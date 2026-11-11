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
// refers to and one per parameter it declares, each in one of six classes.
// A reviewer reads it to see whether installing the app needs the user to do
// anything; "auto" delivery requires that nothing must be filled in by hand
// (CAT-LINT-18).
//
// Layers
//
//	system   handed over by the platform: platform.* (later also requires.*,
//	         uses.*, components.*). The row is named after the reference.
//	custom   declared by the manifest under params. The row is "params.<key>".
//
// Classes
//
//	system         a system-layer reference
//	llm_source     a parameter of type llm_provider: the user picks where the
//	               model comes from (counted on its own, never as user_required)
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
// Fingerprint
//
// The fingerprint identifies the classification, so that a confirmation given
// for one report can be tied to it. It is the lowercase hex SHA-256 of this
// JSON text (UTF-8, no whitespace, no trailing newline):
//
//	{"v":1,"vars":[{"name":"…","layer":"…","class":"…"},…]}
//
// with one object per row, keys in exactly that order, rows sorted by name
// (byte order). Names are ASCII by the manifest format, so no escaping ever
// applies. Nothing else goes in: labels, hints, defaults, the app's id and
// version, the delivery tier and the minimum platform version do not change
// it; adding, removing or renaming a variable, or a change of its layer or
// class, does. Stored fingerprints depend on this definition: a change to the
// classes or to this text must bump "v".

// Layers and classes of a VarRow. They are part of the fingerprint.
const (
	LayerSystem = "system"
	LayerCustom = "custom"

	ClassSystem       = "system"
	ClassGenerated    = "generated"
	ClassDefault      = "default"
	ClassOptional     = "optional"
	ClassLLMSource    = "llm_source"
	ClassUserRequired = "user_required"

	// VarsFingerprintVersion is the "v" inside the fingerprinted text.
	VarsFingerprintVersion = 1
)

// VarClasses lists the six classes in display order.
var VarClasses = []string{ClassSystem, ClassGenerated, ClassDefault, ClassOptional, ClassLLMSource, ClassUserRequired}

// VarClassLabels gives each class its Chinese name.
var VarClassLabels = map[string]string{
	ClassSystem:       "系统给",
	ClassGenerated:    "平台生成",
	ClassDefault:      "有默认值",
	ClassOptional:     "可选",
	ClassLLMSource:    "要选模型来源",
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

// VarReport is the variable report of one manifest.
type VarReport struct {
	App          string          `json:"app"`
	Version      string          `json:"version"`
	File         string          `json:"file,omitempty"`
	DeliveryTier string          `json:"delivery_tier"`
	MinPlatform  MinPlatformInfo `json:"min_platform"`
	Vars         []VarRow        `json:"vars"`
	// Counts has all six classes, zero included.
	Counts      map[string]int  `json:"counts"`
	Hardcoded   []HardcodedHint `json:"hardcoded"`
	Fingerprint string          `json:"fingerprint"`
}

// RefUse is one reference a manifest makes ("platform.tz",
// "params.llm.api_key") and where (/env/TZ, /files/config.yaml).
type RefUse struct {
	Ref   string
	Where string
}

// manifestRefs lists every reference in env "from", first_login "from", file
// templates and init bodies, in manifest order.
func manifestRefs(m *Manifest) []RefUse {
	var out []RefUse
	for _, e := range m.Env {
		if e.From != "" {
			out = append(out, RefUse{e.From, "/env/" + e.Key})
		}
	}
	for _, f := range m.Files {
		for _, r := range TemplateRefs(f.Template) {
			out = append(out, RefUse{r, "/files/" + f.Path})
		}
	}
	for _, s := range m.Init {
		for _, r := range jsonRefs(s.HTTP.JSON) {
			out = append(out, RefUse{r, "/init/" + s.Name})
		}
	}
	for _, lf := range m.FirstLogin {
		if lf.From != "" {
			out = append(out, RefUse{lf.From, "/first_login/" + lf.Label})
		}
	}
	return out
}

// systemRefKinds are the reference prefixes that make a system-layer row.
var systemRefKinds = map[string]bool{"platform": true, "requires": true, "uses": true, "components": true}

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
	case p.Type == "llm_provider":
		return ClassLLMSource
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

// VarsFingerprint is the fingerprint of rows (see the top of this file).
func VarsFingerprint(rows []VarRow) string {
	sum := sha256.Sum256(varsFingerprintText(rows))
	return hex.EncodeToString(sum[:])
}

// varsFingerprintText is the exact text that gets hashed.
func varsFingerprintText(rows []VarRow) []byte {
	type item struct {
		Name  string `json:"name"`
		Layer string `json:"layer"`
		Class string `json:"class"`
	}
	doc := struct {
		V    int    `json:"v"`
		Vars []item `json:"vars"`
	}{V: VarsFingerprintVersion, Vars: make([]item, 0, len(rows))}
	for _, r := range rows {
		doc.Vars = append(doc.Vars, item{r.Name, r.Layer, r.Class})
	}
	sort.SliceStable(doc.Vars, func(i, j int) bool { return doc.Vars[i].Name < doc.Vars[j].Name })
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(doc) // strings and ints only: cannot fail
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
	rep := VarReport{
		App:          m.ID,
		Version:      m.Release.Version,
		DeliveryTier: m.Delivery.Tier,
		MinPlatform:  MinPlatformInfo{Declared: m.Lifecycle.MinPlatform, Computed: mp.Version, Reasons: mp.Reasons},
		Vars:         rows,
		Counts:       map[string]int{},
		Hardcoded:    hardcodedHints(m),
		Fingerprint:  VarsFingerprint(rows),
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
		if e.From != "" || v == "" {
			continue
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
// or only of app when it is not empty. A manifest that cannot be read or does
// not match the schema gets no report; what is wrong with it is in Problems,
// and so is an app that is not in the catalog or a dir that does not exist
// (CAT-LINT-01). It only reads.
func CatalogVars(dir, app string) (VarsResult, error) {
	sch, err := compiledSchema(manifestSchemaF)
	if err != nil {
		return VarsResult{}, err
	}
	res := VarsResult{Apps: []VarReport{}, Problems: []Problem{}}
	add := func(code, level, file, path, format string, args ...any) {
		res.Problems = append(res.Problems, Problem{Code: code, Level: level, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	if p, missing := missingCatalogDir(dir); missing {
		res.Problems = append(res.Problems, p)
		return res, nil
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
