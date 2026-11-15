package catalog

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// Service types are catalog data, not program code. A service type says what
// one kind of service consists of (a model gateway: a base URL, a key, a
// model), which items are secret and how to try the connection. A manifest
// names one under uses[].kind ("I need one") or provides[].kind ("I offer
// one"); the platform matches the two by that name and knows nothing else
// about any kind.
//
// In a catalog repository each type is one file, service-types/<kind>.yaml
// (schema/service-type.schema.json). Build puts them all into index.json
// under "service_types", sorted by kind, so they are signed, synced and
// cached with the index and need no file of their own. A catalog without the
// directory builds an index without the key.
//
// A client asks the index: Index.ServiceType for one kind, KnownServiceTypes
// for the kinds of the catalogs it has. Reading the definitions out of an
// index never fails (ServiceTypeList): a definition this program cannot use
// is left out, so a catalog that later carries types in a newer form still
// syncs and its apps can still be installed.

const (
	// ServiceTypesDir is the directory of the definitions in a catalog.
	ServiceTypesDir = "service-types"
	// ServiceTypeSchema is the format version of a definition this program
	// understands. Definitions of another version are skipped by the client.
	ServiceTypeSchema = 1

	serviceTypeSchemaF = "schema/service-type.schema.json"
	serviceTypeExt     = ".yaml"
)

// Types of a service type's field.
const (
	FieldString = "string"
	FieldInt    = "int"
	FieldBool   = "bool"
	FieldURL    = "url"
	FieldEnum   = "enum"
)

// ServiceType is one definition. Field order is the struct order, so the
// same definitions always produce the same index.json bytes.
type ServiceType struct {
	Schema  int            `yaml:"schema" json:"schema"`
	Kind    string         `yaml:"kind" json:"kind"`
	Label   string         `yaml:"label" json:"label"`
	Summary string         `yaml:"summary" json:"summary"`
	Fields  []ServiceField `yaml:"fields" json:"fields"`
	// Test says how to try a registered service of this type; nil when the
	// type cannot be tried.
	Test *ServiceTest `yaml:"test" json:"test,omitempty"`
}

// ServiceField is one item of a service type. Manifests refer to it as
// uses.<name>.<Key>; a provider gives its value under provides[].values.
type ServiceField struct {
	Key   string `yaml:"key" json:"key"`
	Label string `yaml:"label" json:"label"`
	Type  string `yaml:"type" json:"type"`
	// Required: a service without a value for it is not usable yet.
	Required bool `yaml:"required" json:"required,omitempty"`
	// Secret: stored encrypted, shown as its last four characters only.
	Secret bool `yaml:"secret" json:"secret,omitempty"`
	// Default is the value used when none is given: a string, an integer
	// or a boolean, as the type asks. Read it through DefaultText.
	Default any `yaml:"default" json:"default,omitempty"`
	// Options are the values an enum field may take.
	Options []string `yaml:"options" json:"options,omitempty"`
	// Hint is one more sentence for whoever fills the item in (where to
	// find the value, what form it has); it may be empty.
	Hint string `yaml:"hint" json:"hint,omitempty"`
}

// DefaultText renders Default as text ("587", "true", "starttls"); ok is
// false when the field has no default.
func (f ServiceField) DefaultText() (string, bool) {
	switch v := f.Default.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case float64: // an integer read back from index.json
		return strconv.FormatFloat(v, 'f', -1, 64), true
	}
	return fmt.Sprint(f.Default), true
}

// ServiceTest is how to try a service: exactly one of TCP and HTTP is set.
// Their strings are templates over the type's own fields: {{host}}.
type ServiceTest struct {
	TCP  *ServiceTestTCP  `yaml:"tcp" json:"tcp,omitempty"`
	HTTP *ServiceTestHTTP `yaml:"http" json:"http,omitempty"`
}

// ServiceTestTCP passes when a TCP connection to Host:Port opens. Port is
// text because it is usually a template ("{{port}}"); a number written in the
// definition is kept as its digits.
type ServiceTestTCP struct {
	Host string `yaml:"host" json:"host"`
	Port string `yaml:"port" json:"port"`
}

// ServiceTestHTTP passes when a GET of URL answers with one of ExpectStatus
// (any 2xx when empty). Bearer, when set, is sent as the bearer token.
type ServiceTestHTTP struct {
	URL          string `yaml:"url" json:"url"`
	Bearer       string `yaml:"bearer" json:"bearer,omitempty"`
	ExpectStatus []int  `yaml:"expect_status" json:"expect_status,omitempty"`
}

// Field returns the field called key.
func (t ServiceType) Field(key string) (ServiceField, bool) {
	for _, f := range t.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return ServiceField{}, false
}

// FieldKeys returns the keys of the fields in definition order.
func (t ServiceType) FieldKeys() []string {
	keys := make([]string, 0, len(t.Fields))
	for _, f := range t.Fields {
		keys = append(keys, f.Key)
	}
	return keys
}

// RequiredKeys returns the keys of the required fields in definition order.
func (t ServiceType) RequiredKeys() []string {
	var keys []string
	for _, f := range t.Fields {
		if f.Required {
			keys = append(keys, f.Key)
		}
	}
	return keys
}

// TestRefs returns the field keys the test templates refer to, in order,
// each once.
func (t ServiceType) TestRefs() []string {
	if t.Test == nil {
		return nil
	}
	var texts []string
	if t.Test.TCP != nil {
		texts = append(texts, t.Test.TCP.Host, t.Test.TCP.Port)
	}
	if t.Test.HTTP != nil {
		texts = append(texts, t.Test.HTTP.URL, t.Test.HTTP.Bearer)
	}
	var out []string
	for _, s := range texts {
		for _, k := range FieldRefs(s) {
			out = appendOnce(out, k)
		}
	}
	return out
}

// serviceKindRe is what a service type's name looks like. The {{host}} of
// its test templates is read by FieldRefs (template.go).
var serviceKindRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// ValidServiceKind reports whether s can be the name of a service type.
func ValidServiceKind(s string) bool { return serviceKindRe.MatchString(s) }

// ReadServiceTypes reads the definitions under dir/service-types (dir is the
// root of a catalog repository). present is false when the directory is not
// there: an older catalog, whose manifests' kinds cannot be checked. The
// types come back sorted by kind; a file with a problem (CAT-LINT-25) is left
// out. It only reads.
func ReadServiceTypes(dir string) (types []ServiceType, present bool, problems []Problem) {
	types, problems = []ServiceType{}, []Problem{}
	root := filepath.Join(dir, ServiceTypesDir)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return types, false, problems
	}
	add := func(file, path, format string, args ...any) {
		problems = append(problems, Problem{Code: CodeServiceType, Level: LevelError, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if err != nil {
		add(ServiceTypesDir, "", "读不了：%v", err)
		return types, true, problems
	}
	sch, err := compiledSchema(serviceTypeSchemaF)
	if err != nil {
		add(ServiceTypesDir, "", "%v", err)
		return types, true, problems
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		rel := ServiceTypesDir + "/" + name
		switch {
		case strings.HasPrefix(name, "."), strings.EqualFold(name, "README.md"):
			continue
		case !strings.HasSuffix(name, serviceTypeExt):
			add(rel, "", "%s/ 下只放服务类型定义 <类型>.yaml（和 README.md）：把这个文件改名成 .yaml，或者挪走", ServiceTypesDir)
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			add(rel, "", "读不了：%v", err)
			continue
		}
		t, ps := parseServiceType(b, rel, strings.TrimSuffix(name, serviceTypeExt), sch)
		problems = append(problems, ps...)
		if len(ps) == 0 {
			types = append(types, t)
		}
	}
	sort.SliceStable(types, func(i, j int) bool { return types[i].Kind < types[j].Kind })
	return types, true, problems
}

// ValidateServiceType checks the bytes of one definition file whose name
// without ".yaml" is kind: the schema, then what a schema cannot express. An
// empty result means the definition is good. Every problem is CAT-LINT-25.
func ValidateServiceType(raw []byte, kind string) []Problem {
	rel := ServiceTypesDir + "/" + kind + serviceTypeExt
	sch, err := compiledSchema(serviceTypeSchemaF)
	if err != nil {
		return []Problem{{Code: CodeServiceType, Level: LevelError, File: rel, Message: err.Error()}}
	}
	_, ps := parseServiceType(raw, rel, kind, sch)
	return ps
}

func parseServiceType(raw []byte, rel, wantKind string, sch *jsonschema.Schema) (ServiceType, []Problem) {
	problems := []Problem{}
	// Whatever is wrong with a definition file is CAT-LINT-25, the schema
	// and YAML problems included.
	add := func(_, level, file, path, format string, args ...any) {
		problems = append(problems, Problem{Code: CodeServiceType, Level: level, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	var t ServiceType
	if _, _, ok := validateBytes(raw, rel, sch, add); !ok {
		return t, problems
	}
	if err := yaml.Unmarshal(raw, &t); err != nil {
		add("", LevelError, rel, "", "字段类型不对：%v", err)
		return t, problems
	}
	if t.Kind != wantKind {
		add("", LevelError, rel, "/kind", "kind 是 %q，但文件名是 %s%s，两者必须相同", t.Kind, wantKind, serviceTypeExt)
	}
	seen := map[string]bool{}
	for _, f := range t.Fields {
		at := "/fields/" + f.Key
		if seen[f.Key] {
			add("", LevelError, rel, at, "fields 里 %s 写了两次", f.Key)
		}
		seen[f.Key] = true
		switch {
		case f.Type == FieldEnum && len(f.Options) == 0:
			add("", LevelError, rel, at, "%s 的 type 是 enum，要写 options（能选的值）", f.Key)
		case f.Type != FieldEnum && len(f.Options) > 0:
			add("", LevelError, rel, at, "%s 写了 options，但 type 是 %s 不是 enum", f.Key, f.Type)
		}
		options := map[string]bool{}
		for _, o := range f.Options {
			if options[o] {
				add("", LevelError, rel, at+"/options", "%s 的 options 里 %s 写了两次", f.Key, o)
			}
			options[o] = true
		}
		if msg := defaultMismatch(f); msg != "" {
			add("", LevelError, rel, at+"/default", "%s", msg)
		}
		if f.Secret && f.Default != nil {
			add("", LevelError, rel, at+"/default", "%s 是保密项，不能写默认值：定义文件是公开的", f.Key)
		}
	}
	for _, ref := range t.TestRefs() {
		if !seen[ref] {
			add("", LevelError, rel, "/test", "test 里引用了 {{%s}}，但 fields 里没有这一项", ref)
		}
	}
	return t, problems
}

// defaultMismatch says what is wrong with a field's default, or "".
func defaultMismatch(f ServiceField) string {
	if f.Default == nil {
		return ""
	}
	switch f.Type {
	case FieldInt:
		switch f.Default.(type) {
		case int, int64:
			return ""
		}
		return fmt.Sprintf("%s 的 type 是 int，default 要写整数", f.Key)
	case FieldBool:
		if _, ok := f.Default.(bool); ok {
			return ""
		}
		return fmt.Sprintf("%s 的 type 是 bool，default 要写 true 或 false", f.Key)
	}
	s, ok := f.Default.(string)
	if !ok {
		return fmt.Sprintf("%s 的 type 是 %s，default 要写字符串（数字、true、false 加引号）", f.Key, f.Type)
	}
	if f.Type == FieldURL {
		if u, err := url.Parse(s); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Sprintf("%s 的 type 是 url，default %q 要是 http:// 或 https:// 开头的网址", f.Key, s)
		}
		return ""
	}
	if f.Type == FieldEnum && len(f.Options) > 0 {
		for _, o := range f.Options {
			if o == s {
				return ""
			}
		}
		return fmt.Sprintf("%s 的 default %q 不在 options 里", f.Key, s)
	}
	return ""
}

// ServiceTypeList is Index.ServiceTypes. Reading it from index.json never
// fails, whatever stands under "service_types": the definitions this program
// can use are kept, in the order written, and everything else is left out
// without a word. Left out are
//
//   - a definition of another format version (ServiceTypeSchema), or one
//     that does not decode (a later form of a field, say);
//   - a definition whose kind or field keys are not names, or that names a
//     field twice;
//   - a second definition of a kind (the first counts);
//   - everything, when "service_types" is not a list.
//
// A catalog is published by the build, which puts in only definitions that
// passed the checks (CAT-LINT-25), so nothing is left out of a catalog made
// for this program; what is left out comes from a later one. An app that
// needs a kind this program then does not know is told so when it is
// installed, the rest of the catalog works as before.
type ServiceTypeList []ServiceType

// UnmarshalJSON keeps the usable definitions and never returns an error.
func (l *ServiceTypeList) UnmarshalJSON(b []byte) error {
	out := ServiceTypeList{}
	var raws []json.RawMessage
	if json.Unmarshal(b, &raws) != nil {
		*l = out
		return nil
	}
	seen := map[string]bool{}
	for _, raw := range raws {
		var head struct {
			Schema int `json:"schema"`
		}
		if json.Unmarshal(raw, &head) != nil || head.Schema != ServiceTypeSchema {
			continue
		}
		var t ServiceType
		if json.Unmarshal(raw, &t) != nil || !usableServiceType(t) || seen[t.Kind] {
			continue
		}
		seen[t.Kind] = true
		out = append(out, t)
	}
	*l = out
	return nil
}

// usableServiceType reports whether a definition read from an index has what
// a client relies on: a kind and field keys that are names, each key once.
func usableServiceType(t ServiceType) bool {
	if !ValidServiceKind(t.Kind) {
		return false
	}
	keys := map[string]bool{}
	for _, f := range t.Fields {
		if !ValidServiceKind(f.Key) || keys[f.Key] {
			return false
		}
		keys[f.Key] = true
	}
	return true
}

// ServiceType returns the definition of kind from the index. ok is false
// when the catalog does not define the kind, or defines it in a format
// version this program does not understand.
func (i *Index) ServiceType(kind string) (ServiceType, bool) {
	if i == nil {
		return ServiceType{}, false
	}
	for _, t := range i.ServiceTypes {
		if t.Kind == kind && t.Schema == ServiceTypeSchema {
			return t, true
		}
	}
	return ServiceType{}, false
}

// KnownServiceTypes returns the service types the given catalogs define,
// sorted by kind: pass the public catalog's index first, then those of the
// private catalogs. When several define a kind the first one counts. nil
// indexes are skipped, as are definitions in a format version this program
// does not understand. The result is never nil.
func KnownServiceTypes(indexes ...*Index) []ServiceType {
	out := []ServiceType{}
	seen := map[string]bool{}
	for _, idx := range indexes {
		if idx == nil {
			continue
		}
		for _, t := range idx.ServiceTypes {
			if t.Schema != ServiceTypeSchema || seen[t.Kind] {
				continue
			}
			seen[t.Kind] = true
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Kind < out[b].Kind })
	return out
}
