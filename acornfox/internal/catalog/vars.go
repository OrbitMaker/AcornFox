package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// The variable report says, for one manifest, where every value the app is
// configured with comes from: one row per system variable the manifest
// refers to and one per parameter it declares, each in one of five classes.
// A reviewer reads it to see whether installing the app needs the user to do
// anything; "auto" delivery requires that nothing must be filled in by hand
// (CAT-LINT-18).
//
// Layers
//
//	system   handed over by the platform: platform.*, and uses.<name>.<item>
//	         (an item of the service connected to a need). The row is named
//	         after the reference.
//	custom   declared by the manifest under params. The row is "params.<key>".
//
// Classes
//
//	system         a system-layer reference
//	generated      a parameter with generate: the platform makes the value up
//	default        a parameter with a default that is not the empty string
//	               (the install engine treats an empty value as not filled in,
//	               so "default: \"\"" on a required parameter still has to be
//	               filled in by the user)
//	user_required  a required parameter with none of the above
//	optional       any other parameter
//
// The first matching line wins, in this order.
//
// Services
//
// Besides the variables, the report lists the services the app needs (one
// entry per uses[] item, sorted by name) and the services it offers (one per
// provides[] item, sorted by service type), so that a reviewer sees "it needs
// a model gateway, it can offer a mail server" and how each offered item is
// obtained:
//
//	computed  a template the platform works out (self.internal_url, params.*)
//	issued    handed out per consumer by the provider's issue steps
//	ask       the user does a step in the app and fills the value in
//
// Both lists are always there, empty when the manifest has none.
//
// Fingerprint
//
// The fingerprint identifies what a reviewer confirms, so that a confirmation
// given for one report can be tied to it. It is the lowercase hex SHA-256 of
// this JSON text (UTF-8, no whitespace, no trailing newline):
//
//	{"v":2,
//	 "vars":[{"name":"…","layer":"…","class":"…"},…],
//	 "uses":[{"name":"…","kind":"…","optional":false,"without":"…","dedicated":false},…],
//	 "provides":[{"kind":"…","values":[{"key":"…","how":"…"},…],
//	              "has_issue":false,"has_revoke":false,"has_check":false},…]}
//
// (shown on several lines here; the hashed text is one line). Keys are in
// exactly that order and every key is always written. Rows are sorted by
// name, needed services by name, offered services by type (then by their
// order in the manifest), values by key, all in byte order. Strings are
// written the way encoding/json writes them with HTML escaping off: UTF-8 as
// is, with ", \ and control characters escaped (and U+2028, U+2029).
//
// What goes in is exactly what the report's vars (name, layer, class), uses
// and provides lists show the reviewer, nothing else: labels, hints,
// defaults, the app's id and version, the delivery tier, the minimum platform
// version, the "ask" sentence of an offered item and the steps themselves do
// not change it. The "without" sentence of a needed service does: it is what
// the user is told they lose when an optional service is missing, the
// reviewer reads it in the report and confirms it, so rewording it needs a
// new confirmation. Adding, removing or renaming a variable or service, or a
// change of a class, a type, a flag or how an item is obtained, changes it
// too. Stored fingerprints depend on this definition: a change to the
// classes or to this text must bump "v". Version 1 hashed {"v":1,"vars":…}
// only. Version 2 had one more class, for a parameter kind manifest format 3
// no longer has; no manifest of format 3 could be put in it, so taking it out
// changed no fingerprint and "v" stayed 2.

// Layers and classes of a VarRow. They are part of the fingerprint.
const (
	LayerSystem = "system"
	LayerCustom = "custom"

	ClassSystem       = "system"
	ClassGenerated    = "generated"
	ClassDefault      = "default"
	ClassOptional     = "optional"
	ClassUserRequired = "user_required"

	// VarsFingerprintVersion is the "v" inside the fingerprinted text, also
	// reported as fingerprint_version.
	VarsFingerprintVersion = 2

	// How an item of an offered service is obtained (VarProvideValue.How).
	HowComputed = "computed"
	HowIssued   = "issued"
	HowAsk      = "ask"
)

// HowLabels gives each way of obtaining an offered item its Chinese name.
var HowLabels = map[string]string{
	HowComputed: "平台算出来",
	HowIssued:   "给每个使用方单独发",
	HowAsk:      "要用户做一步",
}

// VarClasses lists the five classes in display order.
var VarClasses = []string{ClassSystem, ClassGenerated, ClassDefault, ClassOptional, ClassUserRequired}

// VarClassLabels gives each class its Chinese name.
var VarClassLabels = map[string]string{
	ClassSystem:       "系统给",
	ClassGenerated:    "平台生成",
	ClassDefault:      "有默认值",
	ClassOptional:     "可选",
	ClassUserRequired: "必须用户填",
}

// VarRow is one variable of a manifest. Name, Layer and Class make up the
// fingerprint; the rest is for the reader.
type VarRow struct {
	Name  string `json:"name"`
	Layer string `json:"layer"`
	Class string `json:"class"`
	Label string `json:"label,omitempty"`
	// Type is the parameter type (custom layer only).
	Type     string `json:"type,omitempty"`
	Required bool   `json:"required,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
	Secret   bool   `json:"secret,omitempty"`
	// Since is the platform version a system variable exists from; Known is
	// false when the variable is not in this program's table.
	Since      string `json:"since,omitempty"`
	Known      *bool  `json:"known,omitempty"`
	UserCanSet bool   `json:"user_can_set,omitempty"`
	// UsedIn lists where the manifest refers to the variable (/env/TZ,
	// /files/config.yaml, /init/setup-admin, /first_login/密码).
	UsedIn []string `json:"used_in"`
}

// HardcodedHint points at a value written into the manifest that looks like
// something the platform or a parameter should supply. It is a reminder for
// the author, never an error.
type HardcodedHint struct {
	Where   string `json:"where"`
	Kind    string `json:"kind"` // url, timezone or secret
	Value   string `json:"value"`
	Message string `json:"message"`
}

// VarUse is one service the app needs (a uses[] item). All five fields are
// always written and all are in the fingerprint.
type VarUse struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Optional  bool   `json:"optional"`
	Without   string `json:"without"`
	Dedicated bool   `json:"dedicated"`
}

// VarProvideValue is one item of an offered service and how it is obtained
// (HowComputed, HowIssued or HowAsk).
type VarProvideValue struct {
	Key string `json:"key"`
	How string `json:"how"`
	// Ask is the sentence shown to the user for HowAsk; for people only, not
	// in the fingerprint.
	Ask string `json:"-"`
}

// VarProvide is one service the app offers (a provides[] item). The three
// flags say whether the manifest writes issue, revoke and check steps.
type VarProvide struct {
	Kind      string            `json:"kind"`
	Values    []VarProvideValue `json:"values"`
	HasIssue  bool              `json:"has_issue"`
	HasRevoke bool              `json:"has_revoke"`
	HasCheck  bool              `json:"has_check"`
}

// VarReport is the variable report of one manifest.
type VarReport struct {
	App          string          `json:"app"`
	Version      string          `json:"version"`
	File         string          `json:"file,omitempty"`
	DeliveryTier string          `json:"delivery_tier"`
	MinPlatform  MinPlatformInfo `json:"min_platform"`
	Vars         []VarRow        `json:"vars"`
	// Counts has all five classes, zero included.
	Counts    map[string]int  `json:"counts"`
	Hardcoded []HardcodedHint `json:"hardcoded"`
	// Uses and Provides are never nil: an empty list is written as [], which
	// is how a reader tells a version 2 report from an older one.
	Uses        []VarUse     `json:"uses"`
	Provides    []VarProvide `json:"provides"`
	Fingerprint string       `json:"fingerprint"`
	// FingerprintVersion is the "v" the fingerprint was computed with.
	FingerprintVersion int `json:"fingerprint_version"`
}

// RefUse is one reference a manifest makes ("platform.tz",
// "uses.llm.api_key") and where (/env/TZ, /files/config.yaml).
type RefUse struct {
	Ref   string
	Where string
}

// manifestRefs lists every reference in env (if, "from", value templates),
// the start command, file templates (and their ifs), init steps, first_login
// ("from", value templates) and provided services (value templates and
// steps), in that order.
func manifestRefs(m *Manifest) []RefUse {
	var out []RefUse
	ifRef := func(cond, where string) {
		if c, err := ParseCondition(cond); cond != "" && err == nil {
			out = append(out, RefUse{c.Ref, where})
		}
	}
	for _, e := range m.Env {
		ifRef(e.If, "/env/"+e.Key)
		if e.From != "" {
			out = append(out, RefUse{e.From, "/env/" + e.Key})
		}
		for _, r := range TemplateRefs(e.Value) {
			out = append(out, RefUse{r, "/env/" + e.Key})
		}
	}
	for _, c := range m.Container.Command {
		for _, r := range TemplateRefs(c) {
			out = append(out, RefUse{r, "/container/command"})
		}
	}
	for _, f := range m.Files {
		ifRef(f.If, "/files/"+f.Path)
		for _, r := range TemplateRefs(f.Template) {
			out = append(out, RefUse{r, "/files/" + f.Path})
		}
	}
	for i, s := range m.Init {
		for _, r := range stepRefs(s) {
			out = append(out, RefUse{r, "/init/" + s.Label(i)})
		}
		// What an on_change step watches counts as used: a change of it
		// is what makes the step run.
		for _, r := range s.Watch {
			out = append(out, RefUse{r, "/init/" + s.Label(i) + "/watch"})
		}
	}
	for _, lf := range m.FirstLogin {
		if lf.From != "" {
			out = append(out, RefUse{lf.From, "/first_login/" + lf.Label})
		} else {
			for _, r := range TemplateRefs(lf.Value) {
				out = append(out, RefUse{r, "/first_login/" + lf.Label})
			}
		}
	}
	for i, p := range m.Provides {
		out = append(out, provideRefs(i, p)...)
	}
	return out
}

// UsedItems are the items of need use that m refers to anywhere a reference
// may stand (env "from", file templates, steps, first_login), as
// uses.<use>.<item>: the set of those items.
func UsedItems(m *Manifest, use string) map[string]bool {
	out := map[string]bool{}
	if m == nil {
		return out
	}
	prefix := "uses." + use + "."
	for _, r := range manifestRefs(m) {
		if item, ok := strings.CutPrefix(r.Ref, prefix); ok && item != "" {
			item, _, _ = strings.Cut(item, ".")
			out[item] = true
		}
	}
	return out
}

// systemRefKinds are the reference prefixes that make a system-layer row.
var systemRefKinds = map[string]bool{"platform": true, "uses": true}

// ClassifyVars returns the rows of a manifest, sorted by name: one per system
// variable it refers to and one per parameter it declares.
func ClassifyVars(m *Manifest) []VarRow {
	refs := manifestRefs(m)
	rows := map[string]*VarRow{}
	var names []string
	for _, r := range refs {
		kind, rest, ok := strings.Cut(r.Ref, ".")
		if !ok || !systemRefKinds[kind] {
			continue
		}
		row := rows[r.Ref]
		if row == nil {
			row = &VarRow{Name: r.Ref, Layer: LayerSystem, Class: ClassSystem, UsedIn: []string{}}
			if kind == "platform" {
				pv, known := LookupPlatformVar(rest)
				row.Known = &known
				if known {
					row.Label, row.Since, row.Secret, row.UserCanSet = pv.Label, pv.Since, pv.Secret, pv.UserCanSet
				}
			}
			rows[r.Ref] = row
			names = append(names, r.Ref)
		}
		row.UsedIn = appendOnce(row.UsedIn, r.Where)
	}
	for _, p := range m.Params {
		name := "params." + p.Key
		if rows[name] != nil {
			continue // the schema does not forbid a repeated key; first wins
		}
		row := &VarRow{Name: name, Layer: LayerCustom, Class: paramClass(p), Label: p.Label, Type: p.Type,
			Required: p.Required, Hidden: p.Hidden, Secret: p.Type == "secret", UsedIn: []string{}}
		for _, r := range refs {
			if r.Ref == name || strings.HasPrefix(r.Ref, name+".") {
				row.UsedIn = appendOnce(row.UsedIn, r.Where)
			}
		}
		rows[name] = row
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]VarRow, 0, len(names))
	for _, n := range names {
		out = append(out, *rows[n])
	}
	return out
}

func paramClass(p Param) string {
	switch {
	case p.Generate != nil:
		return ClassGenerated
	case p.Default != nil && p.Default != "":
		return ClassDefault
	case p.Required:
		return ClassUserRequired
	}
	return ClassOptional
}

func appendOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// ServiceUses returns the services m needs, sorted by name (never nil).
func ServiceUses(m *Manifest) []VarUse {
	out := make([]VarUse, 0, len(m.Uses))
	for _, u := range m.Uses {
		out = append(out, VarUse{Name: u.Name, Kind: u.Kind, Optional: u.Optional, Without: u.Without,
			Dedicated: u.Dedicated})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ServiceProvides returns the services m offers, sorted by type (never nil;
// every Values list is never nil either).
func ServiceProvides(m *Manifest) []VarProvide {
	out := make([]VarProvide, 0, len(m.Provides))
	for _, p := range m.Provides {
		vp := VarProvide{Kind: p.Kind, Values: []VarProvideValue{}, HasIssue: len(p.Issue) > 0,
			HasRevoke: len(p.Revoke) > 0, HasCheck: len(p.Check) > 0}
		for _, k := range p.ValueKeys() {
			v := p.Values[k]
			// A template that refers to issued.<key> is handed out per
			// consumer as much as { issued: key } is.
			how := HowComputed
			switch {
			case v.PerConsumer():
				how = HowIssued
			case v.Kind() == ValueAsk:
				how = HowAsk
			}
			vp.Values = append(vp.Values, VarProvideValue{Key: k, How: how, Ask: v.Ask})
		}
		out = append(out, vp)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// VarsFingerprint is the fingerprint of a report's rows, needed services and
// offered services (see the top of this file). The order of the inputs does
// not matter.
func VarsFingerprint(rows []VarRow, uses []VarUse, provides []VarProvide) string {
	sum := sha256.Sum256(varsFingerprintText(rows, uses, provides))
	return hex.EncodeToString(sum[:])
}

// varsFingerprintText is the exact text that gets hashed. The item types are
// spelled out here, apart from the report's, so that a field added to the
// report cannot change stored fingerprints by accident.
func varsFingerprintText(rows []VarRow, uses []VarUse, provides []VarProvide) []byte {
	type item struct {
		Name  string `json:"name"`
		Layer string `json:"layer"`
		Class string `json:"class"`
	}
	type use struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		Optional  bool   `json:"optional"`
		Without   string `json:"without"`
		Dedicated bool   `json:"dedicated"`
	}
	type value struct {
		Key string `json:"key"`
		How string `json:"how"`
	}
	type provide struct {
		Kind      string  `json:"kind"`
		Values    []value `json:"values"`
		HasIssue  bool    `json:"has_issue"`
		HasRevoke bool    `json:"has_revoke"`
		HasCheck  bool    `json:"has_check"`
	}
	doc := struct {
		V        int       `json:"v"`
		Vars     []item    `json:"vars"`
		Uses     []use     `json:"uses"`
		Provides []provide `json:"provides"`
	}{V: VarsFingerprintVersion, Vars: make([]item, 0, len(rows)), Uses: make([]use, 0, len(uses)), Provides: make([]provide, 0, len(provides))}
	for _, r := range rows {
		doc.Vars = append(doc.Vars, item{r.Name, r.Layer, r.Class})
	}
	sort.SliceStable(doc.Vars, func(i, j int) bool { return doc.Vars[i].Name < doc.Vars[j].Name })
	for _, u := range uses {
		doc.Uses = append(doc.Uses, use{u.Name, u.Kind, u.Optional, u.Without, u.Dedicated})
	}
	sort.SliceStable(doc.Uses, func(i, j int) bool { return doc.Uses[i].Name < doc.Uses[j].Name })
	for _, p := range provides {
		vals := make([]value, 0, len(p.Values))
		for _, v := range p.Values {
			vals = append(vals, value{v.Key, v.How})
		}
		sort.SliceStable(vals, func(i, j int) bool { return vals[i].Key < vals[j].Key })
		doc.Provides = append(doc.Provides, provide{p.Kind, vals, p.HasIssue, p.HasRevoke, p.HasCheck})
	}
	sort.SliceStable(doc.Provides, func(i, j int) bool { return doc.Provides[i].Kind < doc.Provides[j].Kind })
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(doc) // strings, bools, ints and lists of them: cannot fail
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// ManifestVarReport builds the variable report of the manifest in raw
// (manifest.yaml bytes). It does not validate the manifest; run
// ValidateManifest first when the bytes are not known to be good.
func ManifestVarReport(raw []byte) (VarReport, error) {
	doc, err := decodeYAMLDoc(raw)
	if err != nil {
		return VarReport{}, err
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return VarReport{}, err
	}
	return varReport(doc, m)
}

func varReport(doc any, m *Manifest) (VarReport, error) {
	mp, err := manifestMinPlatform(doc, m)
	if err != nil {
		return VarReport{}, err
	}
	rows := ClassifyVars(m)
	uses, provides := ServiceUses(m), ServiceProvides(m)
	rep := VarReport{
		App:          m.ID,
		Version:      m.Release.Version,
		DeliveryTier: m.Delivery.Tier,
		MinPlatform:  MinPlatformInfo{Declared: m.Lifecycle.MinPlatform, Computed: mp.Version, Reasons: mp.Reasons},
		Vars:         rows,
		Counts:       map[string]int{},
		Hardcoded:    hardcodedHints(m),
		Uses:         uses,
		Provides:     provides,
		Fingerprint:  VarsFingerprint(rows, uses, provides),

		FingerprintVersion: VarsFingerprintVersion,
	}
	for _, c := range VarClasses {
		rep.Counts[c] = 0
	}
	for _, r := range rows {
		rep.Counts[r.Class]++
	}
	return rep, nil
}

var (
	hintURL = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://\S+$`)
	hintTZ  = regexp.MustCompile(`^(Africa|America|Antarctica|Arctic|Asia|Atlantic|Australia|Europe|Indian|Pacific|Etc)/[A-Za-z0-9_+-]+(/[A-Za-z0-9_+-]+)?$`)
	// A long run of key-like characters holding both letters and digits.
	hintSecret = regexp.MustCompile(`^[A-Za-z0-9+/=_-]{20,}$`)
	hasLetter  = regexp.MustCompile(`[A-Za-z]`)
	hasDigit   = regexp.MustCompile(`[0-9]`)
)

// hardcodedHints looks at the fixed env values of a manifest for ones that
// look like an address, a time zone or a key.
func hardcodedHints(m *Manifest) []HardcodedHint {
	out := []HardcodedHint{}
	for _, e := range m.Env {
		v := strings.TrimSpace(e.Value)
		if e.From != "" || v == "" || len(TemplateRefs(v)) > 0 {
			continue // a value made of references is no fixed value
		}
		where := "/env/" + e.Key
		switch {
		case hintTZ.MatchString(v):
			out = append(out, HardcodedHint{where, "timezone", v, "写死了时区，改成 from: platform.tz，装在哪台机器就用哪台的时区"})
		case hintURL.MatchString(v):
			out = append(out, HardcodedHint{where, "url", v, "写死了一个地址：是下载源、模型接口或应用自己的地址时，改用平台变量（acornfox catalog platform-vars）或做成参数；平台还没有对应变量就在 gaps.yaml 登记"})
		case hintSecret.MatchString(v) && hasLetter.MatchString(v) && hasDigit.MatchString(v):
			out = append(out, HardcodedHint{where, "secret", v[:4] + "…", "看起来像写死的密钥：密钥不要写进清单，做成带 generate 的参数由平台生成，或让用户填"})
		}
	}
	return out
}

// VarsResult is what CatalogVars returns: one report per app (sorted by id)
// and the problems that kept an app out (CAT-LINT-01, CAT-LINT-02).
type VarsResult struct {
	Apps     []VarReport `json:"apps"`
	Problems []Problem   `json:"problems"`
	// KindLabels maps each service type defined under the catalog's
	// service-types/ to its Chinese label, for showing a report to people.
	// Empty when the catalog defines none; a definition with a problem is
	// left out (catalog lint reports it). Not part of any report.
	KindLabels map[string]string `json:"-"`
}

// missingCatalogDir reports a catalog directory that does not exist or is not
// a directory (CAT-LINT-01), so that a mistyped path is not taken for an
// empty catalog.
func missingCatalogDir(dir string) (Problem, bool) {
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return Problem{}, false
	}
	return Problem{Code: CodeRead, Level: LevelError, File: ".",
		Message: fmt.Sprintf("目录 %s 不存在或不是目录，确认传入的是目录仓库的根目录", dir)}, true
}

// CatalogVars builds the variable report of every app in the catalog at dir,
// or only of app when it is not empty, and reads the labels of the catalog's
// service types (KindLabels). A manifest that cannot be read or does
// not match the schema gets no report; what is wrong with it is in Problems,
// and so is an app that is not in the catalog or a dir that does not exist
// (CAT-LINT-01). It only reads.
func CatalogVars(dir, app string) (VarsResult, error) {
	sch, err := compiledSchema(manifestSchemaF)
	if err != nil {
		return VarsResult{}, err
	}
	res := VarsResult{Apps: []VarReport{}, Problems: []Problem{}, KindLabels: map[string]string{}}
	add := func(code, level, file, path, format string, args ...any) {
		res.Problems = append(res.Problems, Problem{Code: code, Level: level, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if p, missing := missingCatalogDir(dir); missing {
		res.Problems = append(res.Problems, p)
		return res, nil
	}
	types, _, _ := ReadServiceTypes(dir)
	for _, t := range types {
		res.KindLabels[t.Kind] = t.Label
	}
	ids := listDirs(filepath.Join(dir, "apps"))
	if app != "" {
		found := false
		for _, id := range ids {
			found = found || id == app
		}
		if !found {
			add(CodeRead, LevelError, filepath.ToSlash(filepath.Join("apps", app, manifestName)), "", "目录里没有应用 %s（apps/ 下没有这个目录）", app)
			return res, nil
		}
		ids = []string{app}
	}
	for _, id := range ids {
		rel := filepath.ToSlash(filepath.Join("apps", id, manifestName))
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			add(CodeRead, LevelError, rel, "", "读不了：%v", err)
			continue
		}
		doc, _, ok := validateBytes(b, rel, sch, add)
		if !ok {
			continue
		}
		m, err := ParseManifest(b)
		if err != nil {
			add(CodeRead, LevelError, rel, "", "字段类型不对：%v", err)
			continue
		}
		rep, err := varReport(doc, m)
		if err != nil {
			return res, err
		}
		rep.File = rel
		res.Apps = append(res.Apps, rep)
	}
	return res, nil
}
