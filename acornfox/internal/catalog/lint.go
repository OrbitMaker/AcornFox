package catalog

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// Problem codes. They are part of the CLI's --json output; do not renumber.
const (
	CodeRead        = "CAT-LINT-01" // file missing, unreadable or not valid YAML
	CodeSchema      = "CAT-LINT-02" // does not match the schema
	CodeID          = "CAT-LINT-03" // id differs from its directory name
	CodeIcon        = "CAT-LINT-04" // icon file named in the manifest is missing
	CodeLicense     = "CAT-LINT-05" // GPL/AGPL app without app.source_code
	CodeReference   = "CAT-LINT-06" // refers to a param, volume, endpoint or use that does not exist, or to a use without naming an item; a {{…}} in a file template that is not a reference; two uses with one name
	CodeSecret      = "CAT-LINT-07" // hidden param without generate: nobody could fill it in
	CodeEndpoint    = "CAT-LINT-08" // no single primary endpoint with a health check
	CodeRecipe      = "CAT-LINT-09" // recipe names an app or link that does not exist or does not fit (a link whose "from" app is not installed before its "to" app included)
	CodeExposure    = "CAT-LINT-10" // default exposure is not local (warning: needs review)
	CodeUserSteps   = "CAT-LINT-11" // delivery.tier steps without user_steps
	CodeEmpty       = "CAT-LINT-12" // no apps/ and no recipes/ found
	CodeFileName    = "CAT-LINT-13" // a file in an app dir cannot go into the package: non-ASCII name, path too long, symlink
	CodeArtifact    = "CAT-LINT-14" // image artifact without artifact_sha256 (warning: the gateway will not be used for it)
	CodeURL         = "CAT-LINT-15" // a link shown to users (homepage, source_code, delivery.video) is not an http(s) URL
	CodePlatformVar = "CAT-LINT-16" // refers to a platform variable that does not exist
	CodeMinPlatform = "CAT-LINT-17" // declares a lower min_platform than what the manifest or recipe uses needs
	CodeAutoTier    = "CAT-LINT-18" // delivery.tier auto, but some parameter must be filled in by the user
	CodeGaps        = "CAT-LINT-19" // gaps.yaml (missing-variable register) is unreadable or malformed; reported by CheckGaps, not Lint
	LevelError      = "error"
	LevelWarning    = "warning"
	manifestName    = "manifest.yaml"
	recipeName      = "recipe.yaml"
	manifestSchemaF = "schema/manifest.schema.json"
	recipeSchemaF   = "schema/recipe.schema.json"
	gapsSchemaF     = "schema/gaps.schema.json"
)

// Problem codes for the per-architecture image archives (arch.go).
const (
	CodeArchArtifact = "CAT-LINT-20" // artifacts contradict: both forms written, an archive for an architecture not in resources.platforms, or one archive named for two architectures
	CodeArchMissing  = "CAT-LINT-21" // warning: per-architecture form, but an architecture in resources.platforms has no archive (pulled from public sources there; a private catalog's app cannot be installed there)
)

// Problem codes for services: what an app needs (uses), what it offers
// (provides), the steps the platform runs for it and the catalog's service
// type definitions (servicetypes.go, lint_services.go).
const (
	CodeServiceKind   = "CAT-LINT-22" // a uses or provides kind has no definition under service-types/
	CodeServiceFields = "CAT-LINT-23" // does not fit the service type: provides.values gives an item the type does not have, a required item has no source, or uses.<name>.<item> names an item the type does not have
	CodeStepRef       = "CAT-LINT-24" // a step or a provided value refers to something that is not there: saved.<key> not saved by an earlier step, issued.<key> not saved by the issue steps, self, saved, issued, consumer written where they do not exist, or a {{…}} that is no reference at all
	CodeServiceType   = "CAT-LINT-25" // a service type definition file is unreadable, malformed or contradicts itself, (its kind is not its file name, an item twice …)
	CodeNotYet        = "CAT-LINT-26" // warning: something the format accepts that this platform does not carry out yet (PendingFeatures: refused at install)
	CodeUseWithout    = "CAT-LINT-27" // an optional use without "without" (what the user loses when the service is not there)
	CodeEnvOptional   = "CAT-LINT-28" // an item of an optional use is taken where it cannot be left out: an env entry that is not optional itself, a file template, an init step or first_login; the install would stop whenever the service is not there
	CodeStepRetry     = "CAT-LINT-29" // a step's retry on a method other than GET: sending it again could make or delete a thing twice
	CodeFinishIf      = "CAT-LINT-30" // a step's finish_if outside the revoke steps (only taking back may end early as a success)
	CodeFileUses      = "CAT-LINT-31" // a config file written only when it is not there yet (overwrite: never) refers to uses.*: what a service hands out may change when the app is installed again, and such a file would keep the old value
	CodeNoRevoke      = "CAT-LINT-32" // a provides entry hands out a credential per consumer (issue) but cannot take it back (no revoke): deleting a consumer would leave it in the provider
	CodeProvideTwice  = "CAT-LINT-33" // two provides entries of one manifest have the same kind: which one a consumer is plugged into would hang on their order, and so would the variable report's fingerprint
	CodeWithoutUnused = "CAT-LINT-34" // warning: a required use (optional not true) has "without", which is shown only when an optional service is not there: never for this one
	CodeExecSecret    = "CAT-LINT-35" // an exec step's command or user refers to a secret value: the argument list of a process is visible to every user of the host; secrets go in env or stdin
	CodeExecNewline   = "CAT-LINT-36" // an item of an exec step's command, its user or an item of a health check's exec holds a line break: one item is one argument
	CodeIssueVars     = "CAT-LINT-37" // issue_vars that cannot work: written without issue steps, a key named id or issue_id, or a key an issue step saves as well
	CodeEndpointKind  = "CAT-LINT-38" // an endpoint, its health check or the exposure contradict each other: a path health check on a tcp endpoint, a tcp primary endpoint the entry proxy would route, self.internal_url (an http:// address) for a tcp endpoint, a {{…}} in a health check's exec
	CodeUnreachable   = "CAT-LINT-39" // written for an app the platform or the user cannot reach that way: an http step when the app is internal or its primary endpoint is tcp, first_login for an internal app
	CodeValueSecret   = "CAT-LINT-40" // a provides value whose template refers to a secret value (a secret parameter or platform variable, a secret issued value) is not marked secret, and the service type does not mark the item secret: the platform would store and show it as plain text
	CodeNeedsIssue    = "CAT-LINT-41" // purge or on_check_fail on a provides entry without issue steps (nothing is handed out per consumer, so there is nothing of a consumer's to delete or to check), or on_check_fail without check steps
	CodeReadyRef      = "CAT-LINT-42" // a step of a readiness check (ready) refers to issued.*, consumer.* or issue.*: it runs with no consumer and no credential
	CodeReadySay      = "CAT-LINT-43" // ready.say is blank, or holds a {{…}}: it is shown to the user as written, never rendered
	CodeHealthStdout  = "CAT-LINT-44" // a health check's expect_stdout has blanks around it (the output is compared trimmed, so it would never match) or holds a {{…}} (compared as written, no template)
	CodeReadyWrite    = "CAT-LINT-45" // an http step of a readiness check (ready) is a PUT, PATCH or DELETE: the check runs again and again and must not change anything. POST stays allowed (a login is a POST); what an exec step does cannot be told
)

// Problem codes for how a value is written (template.go): references with
// transforms, conditions, the places a template may stand.
const (
	CodeTemplate      = "CAT-LINT-46" // a reference that cannot be read: no closing }}, quotes not in pairs, a reference not written kind.name in lower case, an empty transform; or a {{…}} whose kind looks like a misspelt one (param., use.)
	CodeTransform     = "CAT-LINT-47" // an unknown transform, or one with the wrong arguments (map without "key=value" entries, a key twice, replace without two arguments …)
	CodeIf            = "CAT-LINT-48" // an if that is none of the three forms (ref, ref == word, ref != word), or refers to something that is not there
	CodeEnvExclusive  = "CAT-LINT-49" // two env entries with the same key whose ifs cannot be shown never to hold at once (each needs an if; the same reference compared == with different words, or == and != with the same word)
	CodeCommandSecret = "CAT-LINT-50" // container.command refers to a secret value: the start command's arguments are visible to every user of the host and in docker inspect; secrets go in env
	CodeNotRef        = "CAT-LINT-51" // warning: a {{…}} that is not a reference (no known kind): it is kept as written; fine for an app's own template syntax, a mistake when a reference was meant
	CodeWatch         = "CAT-LINT-52" // an on_change init step's watch lists something that can never change (a hidden parameter) or is no variable (a reference of another kind)
)

// Problem is one finding of Lint.
type Problem struct {
	Code    string `json:"code"`
	Level   string `json:"level"`
	File    string `json:"file"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// Report is the result of Lint.
type Report struct {
	Apps     []string  `json:"apps"`
	Recipes  []string  `json:"recipes"`
	Problems []Problem `json:"problems"`
	// ServiceTypes lists the kinds defined under service-types/ that could
	// be read, sorted. It is empty for a catalog without that directory.
	ServiceTypes []string `json:"service_types"`
	// MinPlatform gives, for every app and recipe that could be read, the
	// platform version it declares and the one worked out from what it uses.
	MinPlatform MinPlatformReport `json:"min_platform"`
}

// MinPlatformReport is Report.MinPlatform: app id / recipe id → versions.
type MinPlatformReport struct {
	Apps    map[string]MinPlatformInfo `json:"apps"`
	Recipes map[string]MinPlatformInfo `json:"recipes"`
}

// MinPlatformInfo compares what a manifest or recipe declares with what it
// needs. Declared is lifecycle.min_platform (a recipe's min_platform, "" when
// it has none); Computed and Reasons are the MinPlatform worked out from the
// file. Declared lower than Computed is CAT-LINT-17.
type MinPlatformInfo struct {
	Declared string        `json:"declared"`
	Computed string        `json:"computed"`
	Reasons  []Requirement `json:"reasons"`
}

// Required is the version that actually applies: the higher of Declared and
// Computed. An author may declare more than the format needs (the app relies
// on a behaviour, not on a field).
func (i MinPlatformInfo) Required() string { return maxVersion(i.Computed, i.Declared) }

// Errors counts problems of level error.
func (r Report) Errors() int {
	n := 0
	for _, p := range r.Problems {
		if p.Level == LevelError {
			n++
		}
	}
	return n
}

var copyleft = regexp.MustCompile(`(?i)\b(A?GPL|LGPL)`)

// Lint checks the catalog rooted at dir (the directory holding apps/ and
// recipes/). It never modifies anything.
func Lint(dir string) (Report, error) {
	mSchema, err := compiledSchema(manifestSchemaF)
	if err != nil {
		return Report{}, err
	}
	rSchema, err := compiledSchema(recipeSchemaF)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Apps: []string{}, Recipes: []string{}, Problems: []Problem{}, ServiceTypes: []string{},
		MinPlatform: MinPlatformReport{Apps: map[string]MinPlatformInfo{}, Recipes: map[string]MinPlatformInfo{}}}
	add := func(code, level, file, path, format string, args ...any) {
		rep.Problems = append(rep.Problems, Problem{Code: code, Level: level, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	svc, typeProblems := loadServiceTypeSet(dir)
	rep.Problems = append(rep.Problems, typeProblems...)
	rep.ServiceTypes = svc.kinds()

	manifests := map[string]Manifest{}
	for _, sub := range listDirs(filepath.Join(dir, "apps")) {
		rel := filepath.ToSlash(filepath.Join("apps", sub, manifestName))
		var m Manifest
		doc, ok := loadAndValidate(filepath.Join(dir, rel), rel, mSchema, &m, add)
		if !ok {
			continue
		}
		rep.Apps = append(rep.Apps, sub)
		manifests[sub] = m
		checkManifest(dir, rel, sub, m, svc, add)
		checkPackable(filepath.Join(dir, "apps", sub), sub, add)
		mp, err := manifestMinPlatform(doc, &m)
		if err != nil {
			return Report{}, err
		}
		info := MinPlatformInfo{Declared: m.Lifecycle.MinPlatform, Computed: mp.Version, Reasons: mp.Reasons}
		rep.MinPlatform.Apps[sub] = info
		checkMinPlatform(rel, "/lifecycle/min_platform", "lifecycle.min_platform", info, add)
	}
	appMin := map[string]string{}
	for id, info := range rep.MinPlatform.Apps {
		appMin[id] = info.Required()
	}
	for _, sub := range listDirs(filepath.Join(dir, "recipes")) {
		rel := filepath.ToSlash(filepath.Join("recipes", sub, recipeName))
		var r Recipe
		doc, ok := loadAndValidate(filepath.Join(dir, rel), rel, rSchema, &r, add)
		if !ok {
			continue
		}
		rep.Recipes = append(rep.Recipes, sub)
		checkRecipe(rel, sub, r, manifests, add)
		mp, err := recipeMinPlatform(doc, &r, appMin)
		if err != nil {
			return Report{}, err
		}
		info := MinPlatformInfo{Declared: r.MinPlatform, Computed: mp.Version, Reasons: mp.Reasons}
		rep.MinPlatform.Recipes[sub] = info
		// A recipe may leave min_platform out: build works it out.
		if r.MinPlatform != "" {
			checkMinPlatform(rel, "/min_platform", "min_platform", info, add)
		}
	}
	// A catalog of service type definitions alone is a catalog with no apps
	// yet (the built-in snapshot can be one), not a wrong directory.
	if len(rep.Apps) == 0 && len(rep.Recipes) == 0 && len(rep.ServiceTypes) == 0 && len(rep.Problems) == 0 {
		add(CodeEmpty, LevelError, ".", "", "没找到 apps/<id>/manifest.yaml、recipes/<id>/recipe.yaml 或 service-types/<类型>.yaml，确认传入的是目录仓库的根目录")
	}
	sort.SliceStable(rep.Problems, func(i, j int) bool { return rep.Problems[i].File < rep.Problems[j].File })
	return rep, nil
}

type addFunc func(code, level, file, path, format string, args ...any)

// maxPackedName is the longest "<id>/<path>" an app package entry may have:
// the USTAR name field, so packages need no PAX headers and stay byte-stable.
const maxPackedName = 100

// checkPackable reports files of an app directory that build could not put
// into the package (it packs every regular file except dotfiles).
func checkPackable(appDir, id string, add addFunc) {
	_ = filepath.WalkDir(appDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == appDir {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(appDir, p)
		rel = filepath.ToSlash(rel)
		file := "apps/" + id + "/" + rel
		switch {
		case d.IsDir():
			return nil
		case !d.Type().IsRegular():
			add(CodeFileName, LevelError, file, "", "不是普通文件（符号链接等），打包时不收，请换成真实文件或删掉")
		case !packableName(rel):
			add(CodeFileName, LevelError, file, "", "文件名只能用英文字母、数字和英文标点（ASCII），不能有中文等字符，请改名")
		case len(id)+1+len(rel) > maxPackedName:
			add(CodeFileName, LevelError, file, "", "包内路径 %s/%s 超过 %d 个字符，请缩短目录或文件名", id, rel, maxPackedName)
		}
		return nil
	})
}

// packableName reports whether rel uses only printable ASCII (no backslash).
func packableName(rel string) bool {
	for _, c := range rel {
		if c < 0x20 || c > 0x7e || c == '\\' {
			return false
		}
	}
	return true
}

// checkMinPlatform is CAT-LINT-17: the declared version is lower than what
// the file uses. The declared version is all an older platform looks at
// before it installs, so it would install something it does not understand.
func checkMinPlatform(rel, ptr, field string, info MinPlatformInfo, add addFunc) {
	if CompareVersions(info.Declared, info.Computed) >= 0 {
		return
	}
	var why []string
	for _, r := range info.Reasons {
		why = append(why, fmt.Sprintf("%s（%s，%s 起）", r.Where, r.Feature, r.Since))
	}
	if len(why) == 0 {
		// Nothing in particular raised it: the format itself starts there.
		add(CodeMinPlatform, LevelError, rel, ptr, "%s 写的是 %s，但这一版格式的清单最低就要 AcornFox %s。把 %s 改成 \"%s\"",
			field, info.Declared, info.Computed, field, info.Computed)
		return
	}
	add(CodeMinPlatform, LevelError, rel, ptr, "%s 写的是 %s，但用到的写法要 AcornFox %s 起才有：%s。把 %s 改成 \"%s\"",
		field, info.Declared, info.Computed, strings.Join(why, "；"), field, info.Computed)
}

var compiledSchemas sync.Map // schema file name → *jsonschema.Schema

// compiledSchema returns an embedded schema compiled once. Lint, the manifest
// check before an install and the gaps check all validate with these.
func compiledSchema(name string) (*jsonschema.Schema, error) {
	if v, ok := compiledSchemas.Load(name); ok {
		return v.(*jsonschema.Schema), nil
	}
	sch, err := compileSchema(name)
	if err != nil {
		return nil, err
	}
	v, _ := compiledSchemas.LoadOrStore(name, sch)
	return v.(*jsonschema.Schema), nil
}

func compileSchema(name string) (*jsonschema.Schema, error) {
	b, err := schemaFS.ReadFile(name)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("内置格式定义 %s 读不出来：%w", name, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		return nil, err
	}
	return c.Compile(name)
}

func listDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// loadAndValidate reads a YAML file, validates it against the schema and
// decodes it into out. It reports problems and returns false when the file
// cannot be used for the cross-file checks. The first result is the document
// as plain JSON values (which fields are written, for the feature table).
func loadAndValidate(path, rel string, sch *jsonschema.Schema, out any, add addFunc) (any, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		add(CodeRead, LevelError, rel, "", "读不了：%v", err)
		return nil, false
	}
	doc, parsed, ok := validateBytes(b, rel, sch, add)
	if !parsed {
		return nil, false
	}
	if err := yaml.Unmarshal(b, out); err != nil {
		add(CodeRead, LevelError, rel, "", "字段类型不对：%v", err)
		return nil, false
	}
	return doc, ok
}

// validateBytes parses YAML bytes and validates them against the schema. It
// returns the document as plain JSON values, whether it could be parsed at
// all, and whether it matches the schema.
func validateBytes(b []byte, rel string, sch *jsonschema.Schema, add addFunc) (doc any, parsed, valid bool) {
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		add(CodeRead, LevelError, rel, "", "不是合法的 YAML：%v", err)
		return nil, false, false
	}
	// Round-trip through JSON so the validator sees JSON types only.
	js, err := json.Marshal(raw)
	if err != nil {
		add(CodeRead, LevelError, rel, "", "转成 JSON 失败（键必须是字符串）：%v", err)
		return nil, false, false
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(js))
	if err != nil {
		add(CodeRead, LevelError, rel, "", "转成 JSON 失败：%v", err)
		return nil, false, false
	}
	if err := json.Unmarshal(js, &doc); err != nil {
		add(CodeRead, LevelError, rel, "", "转成 JSON 失败：%v", err)
		return nil, false, false
	}
	if err := sch.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			for _, leaf := range leaves(ve) {
				add(CodeSchema, LevelError, rel, "/"+strings.Join(leaf.InstanceLocation, "/"), "%s", leafMessage(leaf))
			}
		} else {
			add(CodeSchema, LevelError, rel, "", "%v", err)
		}
		return doc, true, false
	}
	return doc, true, true
}

// decodeYAMLDoc parses YAML bytes into plain JSON values (maps, slices,
// strings, float64, bool).
func decodeYAMLDoc(b []byte) (any, error) {
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	js, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(js, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// ValidateManifest checks manifest.yaml bytes against the built-in manifest
// schema, the very one Lint uses, and returns what is wrong (CAT-LINT-01: not
// readable YAML; CAT-LINT-02: a field, value or format version this program
// does not know). An empty result means the manifest uses nothing this
// program does not understand. It applies the schema only, not Lint's other
// rules, and it never looks at the catalog around the manifest: the install
// engine runs it on a downloaded package before installing.
func ValidateManifest(raw []byte) []Problem {
	return validateWith(raw, manifestName, manifestSchemaF)
}

// ValidateRecipe is ValidateManifest for recipe.yaml bytes.
func ValidateRecipe(raw []byte) []Problem {
	return validateWith(raw, recipeName, recipeSchemaF)
}

func validateWith(raw []byte, file, schemaFile string) []Problem {
	problems := []Problem{}
	add := func(code, level, file, path, format string, args ...any) {
		problems = append(problems, Problem{Code: code, Level: level, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}
	sch, err := compiledSchema(schemaFile)
	if err != nil {
		add(CodeSchema, LevelError, file, "", "%v", err)
		return problems
	}
	validateBytes(raw, file, sch, add)
	return problems
}

func leaves(e *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(e.Causes) == 0 {
		return []*jsonschema.ValidationError{e}
	}
	var out []*jsonschema.ValidationError
	for _, c := range e.Causes {
		out = append(out, leaves(c)...)
	}
	return out
}

func leafMessage(e *jsonschema.ValidationError) string {
	msg := e.Error()
	// e.Error() starts with the instance location; keep only the reason.
	if i := strings.Index(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return msg
}

func checkManifest(dir, rel, sub string, m Manifest, svc *serviceTypeSet, add addFunc) {
	if m.ID != sub {
		add(CodeID, LevelError, rel, "/id", "id 是 %q，但目录名是 %q，两者必须相同", m.ID, sub)
	}
	if m.App.Icon != "" {
		if _, err := os.Stat(filepath.Join(dir, "apps", sub, m.App.Icon)); err != nil {
			add(CodeIcon, LevelError, rel, "/app/icon", "图标文件 apps/%s/%s 不存在", sub, m.App.Icon)
		}
	}
	checkLink(rel, "/app/homepage", m.App.Homepage, add)
	checkLink(rel, "/app/source_code", m.App.SourceCode, add)
	checkLink(rel, "/delivery/video", m.Delivery.Video, add)
	if copyleft.MatchString(m.App.License) && m.App.SourceCode == "" {
		add(CodeLicense, LevelError, rel, "/app/source_code", "许可证是 %s，必须填 app.source_code（源码地址）", m.App.License)
	}

	params := map[string]Param{}
	for _, p := range m.Params {
		params[p.Key] = p
		if p.Hidden && p.Generate == nil {
			add(CodeSecret, LevelError, rel, "/params/"+p.Key, "参数 %s 不出现在表单里（hidden），又没有 generate，没人能给它赋值", p.Key)
		}
	}
	volumes := map[string]bool{}
	for _, v := range m.Volumes {
		volumes[v.Name] = true
	}
	endpoints := map[string]bool{}
	protocols := map[string]string{} // endpoint name → protocol
	primaries, primary := 0, ""
	for _, e := range m.Endpoints {
		endpoints[e.Name] = true
		protocols[e.Name] = e.Protocol
		if e.Primary || len(m.Endpoints) == 1 {
			primaries++
			primary = e.Name
			if e.Health == nil {
				add(CodeEndpoint, LevelError, rel, "/endpoints/"+e.Name, "主入口 %s 没有 health：装好与否、升级成功与否都靠它判断", e.Name)
			}
		}
		checkHealth(rel, e, add)
	}
	if primaries != 1 {
		add(CodeEndpoint, LevelError, rel, "/endpoints", "要有且只有一个主入口（primary: true），现在有 %d 个", primaries)
	}
	exposure := m.Exposure.Default
	if exposure == "" {
		exposure = DefaultExposure
	}
	internal := exposure == ExposureInternal
	// CAT-LINT-38: the entry proxy routes HTTP only, so an app whose primary
	// endpoint does not speak it is reached over the app network only.
	if primaries == 1 && protocols[primary] == ProtocolTCP && !internal {
		add(CodeEndpointKind, LevelError, rel, "/exposure/default", "主入口 %s 是 tcp 入口（不说 HTTP），访问方式却是 %s：入口代理只转发 HTTP，这样的应用打不开。写 exposure: { default: internal }（只在内部网络，别的应用按实例名和端口连它）", primary, exposure)
	}
	// CAT-LINT-39: the platform sends an http step from this server to the
	// port the app publishes for its primary endpoint; an internal app
	// publishes none, and a tcp endpoint does not answer HTTP.
	unreachable := ""
	switch {
	case internal:
		unreachable = "这个应用只在内部网络（exposure.default: internal），不映射宿主机端口，平台从这台机器上连不到它"
	case primaries == 1 && protocols[primary] == ProtocolTCP:
		unreachable = "主入口 " + primary + " 是 tcp 入口，不说 HTTP"
	}
	if internal {
		for _, f := range m.FirstLogin {
			add(CodeUnreachable, LevelError, rel, "/first_login/"+f.Label, "这个应用只在内部网络（exposure.default: internal），没有页面可打开，装好后不会给用户“首次登录”的地址和账号。删掉 first_login；别的应用要用的账号密码写进 provides 的 values")
			break
		}
	}
	uses := map[string]Use{}
	for i, u := range m.Uses {
		if _, twice := uses[u.Name]; twice {
			add(CodeReference, LevelError, rel, fmt.Sprintf("/uses/%d", i), "uses 里有两条都叫 %s：引用 uses.%s.<项> 时分不清是哪一条（现在按前一条算）。给其中一条换个名字", u.Name, u.Name)
		} else {
			uses[u.Name] = u
		}
		svc.checkKind(rel, "/uses/"+u.Name+"/kind", u.Kind, add)
		if u.Optional && u.Without == "" {
			add(CodeUseWithout, LevelError, rel, "/uses/"+u.Name, "uses.%s 是可选的（optional: true），要写 without：一句话告诉用户没有这项服务时会怎样，例如“能用，但发不了通知邮件”", u.Name)
		}
		if !u.Optional && u.Without != "" {
			add(CodeWithoutUnused, LevelWarning, rel, "/uses/"+u.Name+"/without", "uses.%s 是必须的（没写 optional: true），它的 without 不会显示给用户：没有这项服务时安装停下，不会“没有也照装”。它其实可有可无就加 optional: true；必须有就删掉 without", u.Name)
		}
	}
	// Which parameters hold a secret: of type secret, or passed on as a
	// secret (an env entry or a first_login line marked so).
	secretParam := map[string]bool{}
	for _, p := range m.Params {
		if p.Type == "secret" {
			secretParam[p.Key] = true
		}
	}
	markSecret := func(from string, secret bool) {
		if name, ok := strings.CutPrefix(from, "params."); ok && secret {
			secretParam[strings.SplitN(name, ".", 2)[0]] = true
		}
	}
	for _, e := range m.Env {
		markSecret(e.From, e.Secret)
		for _, r := range TemplateRefs(e.Value) {
			markSecret(r, e.Secret)
		}
	}
	for _, f := range m.FirstLogin {
		markSecret(f.From, f.Secret)
	}
	// secretRef reports whether a reference, written where sc says, stands
	// for a secret value.
	secretRef := func(ref string, sc refScope) bool {
		kind, full, _ := strings.Cut(ref, ".")
		name, item, _ := strings.Cut(full, ".")
		switch kind {
		case "params":
			return secretParam[name]
		case "platform":
			pv, ok := LookupPlatformVar(full)
			return ok && pv.Secret
		case "uses":
			u, ok := uses[name]
			if !ok {
				return false
			}
			t, ok := svc.lookup(u.Kind)
			if !ok {
				return false
			}
			f, ok := t.Field(item)
			return ok && f.Secret
		case "saved":
			return sc.savedSecret[full]
		case "issued":
			return sc.issuedSecret[full]
		case "issue":
			return sc.issueSecret[full]
		}
		return false
	}
	// checkRef checks one reference. sc says where it is written: which of
	// the temporary values exist there (none in env, files, first_login).
	checkRef := func(path, ref string, sc refScope) {
		kind, full, ok := strings.Cut(ref, ".")
		if !ok {
			add(sc.unknownCode(), LevelError, rel, path, "%s 不是引用：引用写成 种类.名字。%s", ref, sc.allowed())
			return
		}
		name, item, _ := strings.Cut(full, ".")
		switch kind {
		case "platform":
			if _, ok := LookupPlatformVar(full); !ok {
				add(CodePlatformVar, LevelError, rel, path, "没有平台变量 platform.%s（平台变量只能用程序里那张表上的名字，见 acornfox catalog platform-vars）。别的应用给的东西（大模型接口、邮箱）不是平台变量：写成 uses，引用 uses.<名>.<项>", full)
			}
		case "params":
			if _, ok := params[name]; !ok {
				add(CodeReference, LevelError, rel, path, "引用了不存在的参数 params.%s", name)
			}
		case "uses":
			u, ok := uses[name]
			switch {
			case sc.noUses:
				add(CodeStepRef, LevelError, rel, path, "这里不能引用 uses.%s：%s", full, sc.allowed())
			case !ok:
				add(CodeReference, LevelError, rel, path, "引用了不存在的 uses.%s", name)
			case item == "":
				add(CodeReference, LevelError, rel, path, "uses.%s 要写到项：uses.%s.<项>，项名是服务类型 %s 定义里的 fields[].key", name, name, u.Kind)
			default:
				svc.checkField(rel, path, u, item, add)
				// CAT-LINT-28: an optional service may not be there. An env
				// entry can do without its value (it is left out when
				// optional; env checks that itself, below), and so can a
				// reference with a default transform or what an if naming
				// the same service guards.
				if u.Optional && !sc.env && !sc.optionalOK && sc.guard != name {
					add(CodeEnvOptional, LevelError, rel, path, "这里引用了 uses.%s，这项服务是可选的（optional: true），没接上时这里取不到值，安装前就会报错。可选服务的项可以用在写了 optional: true 的环境变量里（没接上时整条不写），或者给引用加 | default \"值\"（没接上时用这个值），配置文件还可以写 if: 'uses.%s.<项>'（没接上时不写这个文件）；init 步骤、first_login 里只能用 default。这项服务其实必须有，就去掉 uses.%s 的 optional", full, name, name)
				}
			}
		case "self", "saved", "issued", "consumer", "issue":
			// CAT-LINT-42: a readiness check runs with no consumer and no
			// credential.
			if sc.steps == "ready" && (kind == "issued" || kind == "consumer" || kind == "issue") {
				add(CodeReadyRef, LevelError, rel, path, "就绪检查（ready）的步骤里不能引用 %s：它不针对哪个使用方，也不带发给使用方的凭据，查服务、看计划、装完时随时会跑。%s", ref, sc.allowed())
				break
			}
			if msg := sc.check(kind, full); msg != "" {
				add(CodeStepRef, LevelError, rel, path, "%s", msg)
			}
		case "volumes":
			switch {
			case !sc.data:
				add(sc.unknownCode(), LevelError, rel, path, "volumes.%s 只能写在 uninstall.data 里，不能当值引用。%s", full, sc.allowed())
			case !volumes[name]:
				add(CodeReference, LevelError, rel, path, "引用了不存在的数据卷 volumes.%s", name)
			}
		default:
			add(sc.unknownCode(), LevelError, rel, path, "没有 %s 这种引用。%s", ref, sc.allowed())
		}
	}
	// checkTemplate checks every {{…}} of one template the way the install
	// engine reads it (template.go): a reference that cannot be read is an
	// error (CAT-LINT-46, 47), each reference is checked, and what is no
	// reference is kept as written (CAT-LINT-51, a warning: an app's own
	// template syntax, or a reference written wrongly).
	checkTemplate := func(path, tmpl string, sc refScope) {
		t := ScanTemplate(tmpl)
		for _, b := range t.Bad {
			add(b.Code, LevelError, rel, path, "%s", b.Msg)
		}
		for _, e := range t.Exprs() {
			esc := sc
			if e.HasTransform("default") {
				esc.optionalOK = true // no value: the default stands in
			}
			checkRef(path, e.Ref, esc)
		}
		for _, o := range t.Others {
			if kind, rest, ok := strings.Cut(o, "."); ok {
				if kind == "volumes" {
					add(sc.unknownCode(), LevelError, rel, path, "{{%s}}：volumes.%s 只能写在 uninstall.data 里，不能当值引用。%s", o, rest, sc.allowed())
					continue
				}
				if want := nearKind(kind); want != "" {
					add(CodeTemplate, LevelError, rel, path, "{{%s}} 不是引用：没有 %s 这种引用，是不是想写 %s.…？平台只替换 %s 开头的引用，别的 {{…}} 原样写进去", o, kind, want, refKindList)
					continue
				}
			}
			add(CodeNotRef, LevelWarning, rel, path, "{{%s}} 不是平台的引用，会原样保留（应用自己的模板写法可以这样写）。要引用平台的值写成 {{种类.名字}}，种类是 %s；要写出字面的 {{ 就写 \\{{。%s", o, refKindList, sc.allowed())
		}
	}
	// checkIf checks an if (CAT-LINT-48) and the reference it makes. A
	// reference with no value makes it false, so an optional service's
	// item is fine there.
	checkIf := func(path, text string, sc refScope) (Condition, bool) {
		c, err := ParseCondition(text)
		if err != nil {
			add(CodeIf, LevelError, rel, path, "%s", err.Error())
			return Condition{}, false
		}
		sc.optionalOK = true
		checkRef(path, c.Ref, sc)
		return c, true
	}
	envAt := map[string][]int{}
	conds := make([]*Condition, len(m.Env))
	for i, e := range m.Env {
		at := "/env/" + e.Key
		envAt[e.Key] = append(envAt[e.Key], i)
		// An if naming an item of a service leaves the entry out when the
		// service is not there.
		guard := ""
		if e.If != "" {
			if c, ok := checkIf(at+"/if", e.If, refScope{env: true}); ok {
				conds[i] = &c
				guard, _ = refUse(c.Ref)
			}
		}
		if e.From != "" {
			checkRef(at, e.From, refScope{env: true})
			// CAT-LINT-28: the value comes from a service the app may be
			// installed without, so the variable must be one that can be left out.
			if name, ok := refUse(e.From); ok && !e.Optional && uses[name].Optional && guard != name {
				add(CodeEnvOptional, LevelError, rel, at, "环境变量 %s 的值取自 uses.%s，这项服务是可选的（optional: true）：没接上时这一条取不到值，安装前就会报错。给这一条环境变量也写 optional: true（没接上时整条不写）；这项服务其实必须有，就去掉 uses.%s 的 optional", e.Key, name, name)
			}
			continue
		}
		// A value is a template (fixed text is a template without references).
		checkTemplate(at+"/value", e.Value, refScope{env: true})
		if e.Optional {
			continue
		}
		for _, x := range ScanTemplate(e.Value).Exprs() {
			if name, ok := refUse(x.Ref); ok && uses[name].Optional && guard != name && !x.HasTransform("default") {
				add(CodeEnvOptional, LevelError, rel, at, "环境变量 %s 的值里引用了 %s，这项服务是可选的（optional: true）：没接上时取不到值，安装前就会报错。给这一条环境变量写 optional: true（没接上时整条不写），或者给这个引用加 | default \"值\"，或者写 if: 'uses.%s.<项>'（没接上时不写这一条）；这项服务其实必须有，就去掉 uses.%s 的 optional", e.Key, x.Ref, name, name)
				break
			}
		}
	}
	// CAT-LINT-49: entries of one key are written for different cases; that
	// at most one of them holds must show from their ifs alone.
	envKeys := make([]string, 0, len(envAt))
	for k := range envAt {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		idx := envAt[k]
		if len(idx) < 2 {
			continue
		}
	pairs:
		for a := 0; a < len(idx); a++ {
			for b := a + 1; b < len(idx); b++ {
				ca, cb := conds[idx[a]], conds[idx[b]]
				if ca == nil || cb == nil || !ca.Exclusive(*cb) {
					add(CodeEnvExclusive, LevelError, rel, "/env/"+k, "环境变量 %s 写了 %d 条：同名的每一条都要写 if，而且要能看出它们不会同时成立（同一个引用 == 不同的值，例如 'platform.app.scheme == https' 和 'platform.app.scheme == http'；或者同一个值一条 == 一条 !=）。只是按条件换一个值时，也可以只写一条，用 | map \"键=值\" 变换", k, len(idx))
					break pairs
				}
			}
		}
	}
	// The start command: one item is one argument, rendered on its own, and
	// no secret goes in it (CAT-LINT-50).
	for i, c := range m.Container.Command {
		at := "/container/command/" + strconv.Itoa(i)
		checkTemplate(at, c, refScope{})
		for _, r := range TemplateRefs(c) {
			if secretRef(r, refScope{}) {
				add(CodeCommandSecret, LevelError, rel, at, "启动命令引用了保密的值 {{%s}}：容器的启动参数这台机器上的任何用户都能在进程列表里看到，docker inspect 也会显示。保密值经 env 传给应用（env 里写 from: %s），启动命令里不写它", r, r)
				break
			}
		}
	}
	for _, f := range m.FirstLogin {
		switch {
		case f.From != "":
			checkRef("/first_login/"+f.Label, f.From, refScope{})
		case f.Value != "":
			checkTemplate("/first_login/"+f.Label+"/value", f.Value, refScope{})
		}
	}
	for _, d := range m.Uninstall.Data {
		checkRef("/uninstall/data", d, refScope{data: true})
	}
	for _, f := range m.Files {
		at := "/files/" + f.Path
		if !volumes[f.Volume] {
			add(CodeReference, LevelError, rel, at, "写配置文件用的数据卷 %q 不存在", f.Volume)
		}
		fsc := refScope{}
		if f.If != "" {
			if c, ok := checkIf(at+"/if", f.If, refScope{}); ok {
				fsc.guard, _ = refUse(c.Ref)
			}
		}
		checkTemplate(at, f.Template, fsc)
		if f.Overwrite == "never" {
			for _, r := range TemplateRefs(f.Template) {
				if name, ok := refUse(r); ok {
					add(CodeFileUses, LevelError, rel, "/files/"+f.Path, "这个配置文件写了 overwrite: never（只在文件还没有时写一次），模板里却引用了 %s（uses.%s 接的服务给的值）。服务给的值（尤其是按使用方单独发的凭据）以后重装时可能换一份，这个文件不会跟着改，应用会一直用旧的。改成 overwrite: install（每次安装都按这次的值写，接旧数据重装时内容变了会先另存旧文件），或者经环境变量传给应用", r, name)
					break
				}
			}
		}
	}
	// checkExec checks the command and user of an exec step (CAT-LINT-35,
	// 36): one item is one argument, and no secret goes in the argument list.
	checkExec := func(where string, e *StepExec, sc refScope) {
		argv := func(path, text string) {
			if strings.ContainsAny(text, "\r\n") {
				add(CodeExecNewline, LevelError, rel, path, "这一项里有换行：exec 的 command 一项就是一个参数，不经过命令行解释，换行会原样进参数。多行的内容（例如几条 SQL）写进 stdin，从标准输入送给命令")
			}
			for _, r := range TemplateRefs(text) {
				if secretRef(r, sc) {
					add(CodeExecSecret, LevelError, rel, path, "这里引用了保密的值 {{%s}}：exec 的 command、user 会成为进程的参数，这台机器上的任何用户都能在进程列表里看到。保密值经 env（只给这一条命令的环境变量）或 stdin 传给命令", r)
				}
			}
		}
		for i, c := range e.Command {
			argv(where+"/exec/command/"+strconv.Itoa(i), c)
		}
		if e.User != "" {
			argv(where+"/exec/user", e.User)
		}
	}
	// checkSteps checks one list of steps and returns the keys it saves and
	// which of them are secret. A step may use what earlier steps of the
	// same list saved, never its own.
	checkSteps := func(at string, steps []Step, sc refScope, positions ...int) (map[string]bool, map[string]bool) {
		saved, savedSecret := map[string]bool{}, map[string]bool{}
		for i, st := range steps {
			// positions: where each step stands in the manifest's whole
			// list (init steps are checked a group at a time).
			pos := i
			if len(positions) == len(steps) {
				pos = positions[i]
			}
			where := at + "/" + st.Label(pos)
			if r := st.HTTP.Retry; r != nil && !st.IsExec() {
				method := st.HTTP.Method
				if method == "" {
					method = "POST（init 步骤没写 method 时按 POST）"
				}
				if method != "GET" {
					add(CodeStepRetry, LevelError, rel, where+"/http/retry", "retry 只能写在 GET 步骤上，这一步是 %s：判断不过时再发一遍，可能把应用里的东西再建一次或再删一次。要等应用里刚建的东西查得到，就在后面加一步 GET 去查，把 retry 写在那一步上", method)
				}
			}
			kindAt := where + "/http"
			if st.IsExec() {
				kindAt = where + "/exec"
			}
			// CAT-LINT-45: a readiness check runs whenever services are looked
			// at, a plan is made or an install ends, so it must only read.
			// PUT, PATCH and DELETE always change something. POST is let
			// through: many apps log in with a POST before anything can be
			// read. Whether an exec step only reads
			// cannot be told from the manifest.
			if sc.steps == "ready" && !st.IsExec() {
				switch st.HTTP.Method {
				case "PUT", "PATCH", "DELETE":
					add(CodeReadyWrite, LevelError, rel, where+"/http/method", "就绪检查（ready）的步骤要只读，这一步却是 %s：查服务、看计划、装完时都会跑它，每跑一次就改一次应用里的东西。改成 GET 去查；要先登录才能查，登录那一步写 POST 可以", st.HTTP.Method)
				}
			}
			if st.FinishIfWritten() && sc.steps != "revoke" && sc.steps != "purge" {
				add(CodeFinishIf, LevelError, rel, kindAt+"/finish_if", "finish_if 只能写在 provides 的 revoke、purge 步骤里（要收回或删掉的东西已经没了，算成功）。%s 的步骤里用它会让一组步骤没做完就算成功：去掉它", sc.stepsName())
			}
			if !st.IsExec() && unreachable != "" {
				add(CodeUnreachable, LevelError, rel, where+"/http", "这一步是 http 步骤，平台要从这台机器向应用的主入口发请求，可是%s。改成 exec 步骤（在应用自己的容器里执行命令）", unreachable)
			}
			sc.saved, sc.savedSecret = saved, savedSecret
			for _, tmpl := range stepTemplates(st) {
				checkTemplate(where, tmpl, sc)
			}
			if st.IsExec() {
				checkExec(where, st.Exec, sc)
			}
			saves := st.Saves()
			for _, k := range st.SaveKeys() {
				if sc.steps == "issue" && k == IssueIDKey {
					add(CodeStepRef, LevelError, rel, kindAt+"/save/"+k, "save 不能用 %s 这个键名：平台每次发凭据时自动存这个键（这次的随机编号，步骤里写 {{%s}}），revoke、check 里用 {{issued.%s}} 取回。换一个键名", IssueIDKey, IssueIDRef, IssueIDKey)
				}
				if sc.steps == "issue" && sc.issueVars[k] {
					add(CodeIssueVars, LevelError, rel, kindAt+"/save/"+k, "save 的键 %s 和 issue_vars 里的 %s 重名：发成功后两样都存进凭据，issued.%s 分不清是哪一个。给其中一个换个名字", k, k, k)
				}
				saved[k] = true
				if saves[k].Secret {
					savedSecret[k] = true
				}
			}
		}
		return saved, savedSecret
	}
	// The init steps run in groups (InitGroups): the steps of one trigger
	// (and, for on_change, one watch list) share a session, so what a step
	// may use that an earlier one saved is looked for in its own group only.
	for _, g := range InitGroups(m.Init) {
		steps := make([]Step, len(g.Index))
		for i, at := range g.Index {
			steps[i] = m.Init[at]
		}
		checkSteps("/init", steps, refScope{steps: "init"}, g.Index...)
	}
	for i, st := range m.Init {
		if st.Trigger() != StepOnChange {
			continue
		}
		at := "/init/" + st.Label(i) + "/watch"
		for _, ref := range st.Watch {
			kind, rest, _ := strings.Cut(ref, ".")
			switch kind {
			case "params":
				if p, ok := params[rest]; ok && p.Hidden {
					add(CodeWatch, LevelError, rel, at, "watch 里的 %s 是平台生成的参数（hidden），装好以后不会变，这一步永远不会跑：去掉它，或者改看别的", ref)
					continue
				}
			case "platform", "uses":
			default:
				add(CodeWatch, LevelError, rel, at, "watch 里只能写 params.<名>、platform.<名>、uses.<需要名>.<项>，%s 不是", ref)
				continue
			}
			checkRef(at, ref, refScope{optionalOK: true})
		}
	}
	providedAt := map[string]int{}
	for i, p := range m.Provides {
		at := fmt.Sprintf("/provides/%d", i)
		if first, twice := providedAt[p.Kind]; twice && p.Kind != "" {
			add(CodeProvideTwice, LevelError, rel, at+"/kind", "provides 里第 %d 条和第 %d 条都是服务类型 %s：一个应用每种服务只能提供一条（使用方接哪一条、变量报告的指纹都会随先后顺序变）。合成一条，或去掉多的那条", first+1, i+1, p.Kind)
		} else {
			providedAt[p.Kind] = i
		}
		if p.Endpoint != "" && !endpoints[p.Endpoint] {
			add(CodeReference, LevelError, rel, "/provides", "provides 指向不存在的入口 %q", p.Endpoint)
		}
		// The platform sends a service's http steps from this server, which
		// reaches an app through the one port it publishes: the primary
		// endpoint's. Other apps reach any port over the app network, so an
		// entry without http steps (values only, or exec steps, which run in
		// the container) may name another one.
		if hasHTTPSteps(p) && p.Endpoint != "" && endpoints[p.Endpoint] {
			if primaries != 1 || primary != p.Endpoint {
				add(CodeEndpoint, LevelError, rel, at+"/endpoint", "provides 的 issue、revoke、check 里的 http 步骤由平台从这台机器上发出，只能发到主入口（primary: true 的那个），平台连不到入口 %q。把 endpoint 改成主入口，或者不写", p.Endpoint)
			}
		}
		if len(p.Issue) > 0 && len(p.Revoke) == 0 {
			add(CodeNoRevoke, LevelError, rel, at, "provides 写了 issue（给每个使用方发凭据）却没写 revoke：使用方删除时平台收不回发给它的凭据，它会一直留在 %s 里。写上 revoke 的步骤（要收回的东西已经没了时可以用 finish_if 算成功）", m.ID)
		}
		svc.checkKind(rel, at+"/kind", p.Kind, add)
		// The values worked out before an issue (CAT-LINT-37 and the
		// references of their templates).
		issueVars, issueSecret := map[string]bool{}, map[string]bool{}
		if len(p.IssueVars) > 0 && len(p.Issue) == 0 {
			add(CodeIssueVars, LevelError, rel, at+"/issue_vars", "写了 issue_vars 却没写 issue 的步骤：issue_vars 是给 issue 步骤用的（{{issue.<键>}}），没有 issue 就永远不会生成。补上 issue 的步骤，或者删掉 issue_vars")
		}
		for _, k := range p.IssueVarKeys() {
			v, vat := p.IssueVars[k], at+"/issue_vars/"+k
			switch k {
			case "id":
				add(CodeIssueVars, LevelError, rel, vat, "issue_vars 的键不能叫 id：{{%s}} 是平台每次发凭据的随机编号。换一个键名", IssueIDRef)
			case IssueIDKey:
				add(CodeIssueVars, LevelError, rel, vat, "issue_vars 的键不能叫 %s：平台每次发凭据时自动把随机编号存在这个键下（issued.%s）。换一个键名", IssueIDKey, IssueIDKey)
			}
			issueVars[k] = true
			isc := refScope{issueVar: true, consumer: true, noUses: true}
			if v.Generated() {
				issueSecret[k] = true
				continue
			}
			checkTemplate(vat, v.Template, isc)
			for _, r := range TemplateRefs(v.Template) {
				if secretRef(r, isc) {
					issueSecret[k] = true
				}
			}
		}
		issued, issuedSecret := checkSteps(at+"/issue", p.Issue, refScope{steps: "issue", consumer: true, noUses: true, issueVars: issueVars, issueSecret: issueSecret})
		for k := range issueVars {
			issued[k] = true
			if issueSecret[k] {
				issuedSecret[k] = true
			}
		}
		// Which issued keys are secret is one rule (Provide.IssuedSecretKeys),
		// the one the platform goes by: it also counts a key behind an item
		// { issued: k } that is secret by its own mark or by the service type.
		var kindDef *ServiceType
		if t, ok := svc.lookup(p.Kind); ok {
			kindDef = &t
		}
		for k := range p.IssuedSecretKeys(func(r string) bool { return secretRef(r, refScope{issueVar: true, consumer: true, noUses: true}) }, kindDef) {
			issuedSecret[k] = true
		}
		if len(p.Issue) > 0 {
			issued[IssueIDKey] = true // the platform stores the issue number with every credential
		}
		checkSteps(at+"/revoke", p.Revoke, refScope{steps: "revoke", self: p.Endpoint != "", consumer: true, issued: issued, issuedSecret: issuedSecret, noUses: true})
		checkSteps(at+"/purge", p.Purge, refScope{steps: "purge", self: p.Endpoint != "", consumer: true, issued: issued, issuedSecret: issuedSecret, noUses: true})
		checkSteps(at+"/check", p.Check, refScope{steps: "check", self: p.Endpoint != "", consumer: true, issued: issued, issuedSecret: issuedSecret, noUses: true})
		// CAT-LINT-41: deleting a consumer's data, and what follows when its
		// old credential fails the check, are about what was handed out to
		// that consumer.
		if len(p.Issue) == 0 {
			if len(p.Purge) > 0 {
				add(CodeNeedsIssue, LevelError, rel, at+"/purge", "写了 purge 却没写 issue 的步骤：purge 删的是给某个使用方单独建的数据（例如它的库），这条 provides 不给每个使用方单独建东西，使用方连数据一起删时没有它的数据可删。删掉 purge；要给每个使用方单独建库，就补上 issue（还有 revoke）")
			}
			if p.OnCheckFail != "" {
				add(CodeNeedsIssue, LevelError, rel, at+"/on_check_fail", "写了 on_check_fail 却没写 issue 的步骤：它说的是接旧数据重装时，当初发给这个使用方的凭据核对不过怎么办，这条 provides 不给每个使用方单独发凭据。删掉 on_check_fail")
			}
		} else if p.OnCheckFail != "" && len(p.Check) == 0 {
			add(CodeNeedsIssue, LevelError, rel, at+"/on_check_fail", "写了 on_check_fail 却没写 check 的步骤：没有 check 就不会核对旧凭据，on_check_fail 永远用不上。补上 check 的步骤（判断旧凭据还能不能接着用），或者删掉 on_check_fail")
		}
		// The readiness check: steps with no consumer and no credential
		// (CAT-LINT-42), and one sentence for the user, as written (43).
		if rd := p.Ready; rd != nil {
			checkSteps(at+"/ready/steps", rd.Steps, refScope{steps: "ready", self: p.Endpoint != "", noUses: true})
			switch ph := TemplatePlaceholders(rd.Say); {
			case strings.TrimSpace(rd.Say) == "":
				add(CodeReadySay, LevelError, rel, at+"/ready/say", "ready.say 是空的：服务没就绪时平台把这句话原样给用户看，要写清缺什么、到哪里去补，例如“还没有能用的上游：到应用的设置页加一个”")
			case len(ph) > 0:
				add(CodeReadySay, LevelError, rel, at+"/ready/say", "ready.say 里写了 {{%s}}：这句话原样给用户看，不是模板，{{…}} 不会被替换。直接写成文字，不要往里放密码这类值", ph[0])
			}
		}
		for _, k := range p.ValueKeys() {
			v, vat := p.Values[k], at+"/values/"+k
			switch v.Kind() {
			case ValueTemplate:
				vsc := refScope{values: true, self: p.Endpoint != "", issued: issued, issuedSecret: issuedSecret, noUses: true}
				checkTemplate(vat, v.Template, vsc)
				// CAT-LINT-40: an item made from a secret value says so, unless
				// the service type marks the item secret anyway. The platform
				// goes by these marks only: an unmarked item is stored and
				// shown as plain text.
				kindSecretItem := false
				if kindDef != nil {
					if f, ok := kindDef.Field(k); ok && f.Secret {
						kindSecretItem = true
					}
				}
				if !v.Secret && !kindSecretItem {
					for _, r := range TemplateRefs(v.Template) {
						if secretRef(r, vsc) {
							add(CodeValueSecret, LevelError, rel, vat, "%s 的模板引用了保密的值 {{%s}}，这一项却没有标保密：平台只按清单和服务类型里写明的保密标记处理，没标的项会以明文存进服务登记表、在 acornfox services 和页面上显示出来。写成 { template: \"…\", secret: true }", k, r)
							break
						}
					}
				}
				// CAT-LINT-38: self.internal_url is an http:// address.
				if p.Endpoint != "" && protocols[p.Endpoint] == ProtocolTCP {
					for _, r := range TemplateRefs(v.Template) {
						if r == "self.internal_url" {
							add(CodeEndpointKind, LevelError, rel, vat, "self.internal_url 是 http:// 开头的地址，入口 %s 却是 tcp 入口（不说 HTTP）。用 self.host 和 self.port 拼出这种服务自己的地址写法", p.Endpoint)
							break
						}
					}
				}
			case ValueIssued:
				if !issued[v.Issued] {
					add(CodeStepRef, LevelError, rel, vat, "%s 的值取自 issue 步骤存下的 %s，但 issue 的步骤里没有 save 过这个键，issue_vars 里也没有%s", k, v.Issued, savedHint(issued))
				}
			}
		}
		svc.checkProvide(rel, at, p, add)
	}
	for i, img := range m.Release.Images {
		if img.Artifact != "" && img.ArtifactSHA256 == "" {
			add(CodeArtifact, LevelWarning, rel, fmt.Sprintf("/release/images/%d", i), "镜像 %s 填了 artifact 但没填 artifact_sha256：安装时不会走下载网关，只能从公共源拉", img.Name)
		}
		checkArchArtifacts(rel, fmt.Sprintf("/release/images/%d", i), img, m.Resources.Platforms, add)
	}
	if d := m.Exposure.Default; d != "" && d != DefaultExposure && d != ExposureInternal {
		add(CodeExposure, LevelWarning, rel, "/exposure/default", "默认访问方式是 %s 不是 local：上架审核时要确认它有登录保护", d)
	}
	// CAT-LINT-26: what this platform does not carry out yet.
	for _, p := range PendingFeatures(&m) {
		add(CodeNotYet, LevelWarning, rel, p.Where, "%s：格式已经认，但这一版的平台还不执行，现在装这个应用会被拒绝（CAT-INST-23）。等平台支持后再上架", p.What)
	}
	if m.Delivery.Tier == "steps" && len(m.Delivery.UserSteps) == 0 {
		add(CodeUserSteps, LevelError, rel, "/delivery", "交付方式是“几步”，但没写 user_steps")
	}
	if m.Delivery.Tier == "auto" {
		var must []string
		for _, v := range ClassifyVars(&m) {
			if v.Class == ClassUserRequired {
				must = append(must, strings.TrimPrefix(v.Name, "params."))
			}
		}
		if len(must) > 0 {
			add(CodeAutoTier, LevelError, rel, "/delivery/tier", "交付方式是“全自动”（auto），但有 %d 个参数必须用户填：%s。给它们默认值或 generate，或者把 delivery.tier 改成 steps 并写 user_steps",
				len(must), strings.Join(must, "、"))
		}
	}
}

// checkHealth is CAT-LINT-38, 36 and 44 for one endpoint's health check: a
// GET only to an endpoint that speaks HTTP; a command taken as written, one
// argument per item; an expected output that can match.
func checkHealth(rel string, e Endpoint, add addFunc) {
	h := e.Health
	if h == nil {
		return
	}
	at := "/endpoints/" + e.Name + "/health"
	if h.Path != "" && e.Protocol == ProtocolTCP {
		add(CodeEndpointKind, LevelError, rel, at+"/path", "入口 %s 是 tcp 入口（不说 HTTP），健康检查却写了 path（向它发 HTTP 请求）。改成 tcp: true（端口连得上就算好了）或 exec（在容器里执行一条检查命令，退出码 0 算好了）", e.Name)
	}
	for i, a := range h.Exec {
		p := at + "/exec/" + strconv.Itoa(i)
		if strings.ContainsAny(a, "\r\n") {
			add(CodeExecNewline, LevelError, rel, p, "这一项里有换行：health.exec 一项就是一个参数，不经过命令行解释")
		}
		if ph := TemplatePlaceholders(a); len(ph) > 0 {
			add(CodeEndpointKind, LevelError, rel, p, "health.exec 原样执行，不是模板：{{%s}} 不会被替换。检查要用的密码这类值，从容器自己的环境变量读（例如清单 env 里给容器的那一项）", ph[0])
		}
	}
	// CAT-LINT-44: the output is compared trimmed, and as written.
	if x := h.ExpectStdout; x != "" {
		switch ph := TemplatePlaceholders(x); {
		case strings.TrimSpace(x) != x:
			add(CodeHealthStdout, LevelError, rel, at+"/expect_stdout", "expect_stdout 首尾有空白：平台把命令的标准输出去掉首尾空白后再和它比，这样永远对不上，健康检查永远不过。去掉首尾的空格和换行")
		case len(ph) > 0:
			add(CodeHealthStdout, LevelError, rel, at+"/expect_stdout", "expect_stdout 原样比较，不是模板：{{%s}} 不会被替换。写成命令正常时输出的那段文字", ph[0])
		}
	}
}

// hasHTTPSteps reports whether a provided service has an http step (in any
// of its lists, the readiness check's included): one the platform sends
// from this server to the app's published port.
func hasHTTPSteps(p Provide) bool {
	for _, list := range [][]Step{p.Issue, p.Revoke, p.Purge, p.Check, p.ReadySteps()} {
		for _, st := range list {
			if !st.IsExec() {
				return true
			}
		}
	}
	return false
}

// checkArchArtifacts checks the per-architecture image archives of one image
// (arch.go): CAT-LINT-20 for what contradicts, CAT-LINT-21 (warning) for an
// architecture the app runs on that has no archive. Architecture names other
// than amd64 and arm64, and a missing file, sha256 or size, are schema errors.
func checkArchArtifacts(rel, at string, img Image, platforms []string, add addFunc) {
	if len(img.Artifacts) == 0 {
		return
	}
	if img.Artifact != "" || img.ArtifactSHA256 != "" || img.ArtifactSize != 0 {
		add(CodeArchArtifact, LevelError, rel, at, "镜像 %s 同时写了按架构的 artifacts 和旧写法 artifact、artifact_sha256、artifact_size，只能用一种：用 artifacts 时把旧写法的三项删掉", img.Name)
	}
	runs := map[string]bool{}
	for _, p := range platforms {
		runs[strings.TrimPrefix(p, "linux/")] = true
	}
	archs := make([]string, 0, len(img.Artifacts))
	for arch := range img.Artifacts {
		archs = append(archs, arch)
	}
	sort.Strings(archs)
	byFile, bySum := map[string]string{}, map[string]string{}
	for _, arch := range archs {
		a := img.Artifacts[arch]
		where := at + "/artifacts/" + arch
		if !runs[arch] {
			add(CodeArchArtifact, LevelError, rel, where, "镜像 %s 写了 %s 的镜像包，但 resources.platforms 里没有 linux/%s：应用支持这种架构就把 linux/%s 加进 platforms，不支持就删掉这个镜像包", img.Name, arch, arch, arch)
		}
		if other, dup := byFile[a.File]; dup && a.File != "" {
			add(CodeArchArtifact, LevelError, rel, where, "镜像 %s 的 %s 和 %s 写了同一个网关文件 %s：一个镜像包只含一种架构的镜像，每种架构要分别导出（docker save）、分别上传", img.Name, other, arch, a.File)
		} else if other, dup := bySum[strings.ToLower(a.SHA256)]; dup && a.SHA256 != "" {
			add(CodeArchArtifact, LevelError, rel, where, "镜像 %s 的 %s 和 %s 的镜像包校验值相同，是同一个文件：一个镜像包只含一种架构的镜像，每种架构要分别导出、分别上传", img.Name, other, arch)
		}
		byFile[a.File], bySum[strings.ToLower(a.SHA256)] = arch, arch
	}
	for _, p := range platforms {
		arch := strings.TrimPrefix(p, "linux/")
		if _, ok := img.Artifacts[arch]; !ok {
			add(CodeArchMissing, LevelWarning, rel, at+"/artifacts", "镜像 %s 没有 %s 的镜像包：公共目录里，这种机器不走下载网关，从公共源按 digest 拉（较慢）；私有目录里，这种机器装不了这个应用", img.Name, p)
		}
	}
}

func checkRecipe(rel, sub string, r Recipe, manifests map[string]Manifest, add addFunc) {
	if r.ID != sub {
		add(CodeID, LevelError, rel, "/id", "id 是 %q，但目录名是 %q，两者必须相同", r.ID, sub)
	}
	inRecipe := map[string]bool{}
	order := map[string]int{}
	for i, a := range r.Apps {
		inRecipe[a.App] = true
		if _, seen := order[a.App]; !seen {
			order[a.App] = i
		}
		m, ok := manifests[a.App]
		if !ok {
			add(CodeRecipe, LevelError, rel, "/apps/"+a.App, "应用 %s 在 apps/ 里没有清单", a.App)
			continue
		}
		for key := range a.Preset {
			found := false
			for _, p := range m.Params {
				if p.Key == key {
					found = true
				}
			}
			if !found {
				add(CodeRecipe, LevelError, rel, "/apps/"+a.App+"/preset", "预填了 %s 不存在的参数 %s", a.App, key)
			}
		}
	}
	for _, l := range r.Links {
		if !inRecipe[l.From] || !inRecipe[l.To] {
			add(CodeRecipe, LevelError, rel, "/links", "连接 %s → %s 里的应用不在这个配方的 apps 里", l.From, l.To)
			continue
		}
		if order[l.From] >= order[l.To] {
			add(CodeRecipe, LevelError, rel, "/links", "连接 %s → %s：提供服务的 %s 要排在 %s 前面。配方按 apps 的顺序装，后装的应用才能接上先装的应用提供的服务", l.From, l.To, l.From, l.To)
			continue
		}
		from, okF := manifests[l.From]
		to, okT := manifests[l.To]
		if !okF || !okT {
			continue
		}
		var use *Use
		for i := range to.Uses {
			if to.Uses[i].Name == l.Uses {
				use = &to.Uses[i]
			}
		}
		if use == nil {
			add(CodeRecipe, LevelError, rel, "/links", "%s 的清单里没有名为 %s 的需要（links 的 uses 写的是 %s 清单里 uses[].name，不是服务类型名）", l.To, l.Uses, l.To)
			continue
		}
		provided := false
		for _, p := range from.Provides {
			if p.Kind == use.Kind {
				provided = true
			}
		}
		if !provided {
			add(CodeRecipe, LevelError, rel, "/links", "%s 不提供 %s，接不上 %s 的 %s", l.From, use.Kind, l.To, l.Uses)
		}
	}
	if r.Delivery.Tier == "steps" && len(r.Delivery.UserSteps) == 0 {
		add(CodeUserSteps, LevelError, rel, "/delivery", "交付方式是“几步”，但没写 user_steps")
	}
	checkLink(rel, "/delivery/video", r.Delivery.Video, add)
}

// checkLink rejects links the console would show that are not plain http(s)
// URLs with a host: the schema's format: uri also lets javascript: and data:
// through, and these come from a catalog synced over the network.
func checkLink(rel, ptr, u string, add addFunc) {
	if u == "" {
		return
	}
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		add(CodeURL, LevelError, rel, ptr, "链接 %q 不是 http:// 或 https:// 开头的网址", u)
	}
}

// refKindList names the kinds of reference a template can have today.
const refKindList = "params、platform、uses、self、saved、issued、consumer、issue"

// nearKind is the kind of reference kind was probably meant to be (param →
// params, use → uses, Params → params), "" when it is none.
func nearKind(kind string) string {
	k := strings.ToLower(kind)
	for _, c := range []string{k, k + "s", strings.TrimSuffix(k, "s")} {
		if RefKinds[c] && c != "requires" && c != "components" {
			return c
		}
	}
	return ""
}
