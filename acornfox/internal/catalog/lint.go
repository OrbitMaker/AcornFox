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
	CodeReference   = "CAT-LINT-06" // refers to a param, volume, endpoint or use that does not exist
	CodeSecret      = "CAT-LINT-07" // hidden param without generate: nobody could fill it in
	CodeEndpoint    = "CAT-LINT-08" // no single primary endpoint with a health check
	CodeRecipe      = "CAT-LINT-09" // recipe names an app or link that does not exist or does not fit
	CodeExposure    = "CAT-LINT-10" // default exposure is not local (warning: needs review)
	CodeUserSteps   = "CAT-LINT-11" // delivery.tier steps without user_steps
	CodeEmpty       = "CAT-LINT-12" // no apps/ and no recipes/ found
	CodeFileName    = "CAT-LINT-13" // a file in an app dir cannot go into the package: non-ASCII name, path too long, symlink
	CodeArtifact    = "CAT-LINT-14" // image artifact without artifact_sha256 (warning: the gateway will not be used for it)
	CodeURL         = "CAT-LINT-15" // a link shown to users (homepage, source_code, delivery.video) is not an http(s) URL
	CodePlatformVar = "CAT-LINT-16" // refers to a platform variable that does not exist, or to one only an llm_provider parameter may use
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

// templateRef matches {{params.x}} style references inside init bodies and
// file templates: group 1 is the kind (params, platform…), group 2 the rest
// of the path ("x", "mirror.pypi").
var templateRef = regexp.MustCompile(`\{\{\s*([a-z]+)\.([a-z0-9_]+(?:\.[a-z0-9_]+)*)\s*\}\}`)

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
	rep := Report{Apps: []string{}, Recipes: []string{}, Problems: []Problem{},
		MinPlatform: MinPlatformReport{Apps: map[string]MinPlatformInfo{}, Recipes: map[string]MinPlatformInfo{}}}
	add := func(code, level, file, path, format string, args ...any) {
		rep.Problems = append(rep.Problems, Problem{Code: code, Level: level, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}

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
		checkManifest(dir, rel, sub, m, add)
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
	if len(rep.Apps) == 0 && len(rep.Recipes) == 0 && len(rep.Problems) == 0 {
		add(CodeEmpty, LevelError, ".", "", "没找到 apps/<id>/manifest.yaml 或 recipes/<id>/recipe.yaml，确认传入的是目录仓库的根目录")
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

func checkManifest(dir, rel, sub string, m Manifest, add addFunc) {
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
	primaries := 0
	for _, e := range m.Endpoints {
		endpoints[e.Name] = true
		if e.Primary || len(m.Endpoints) == 1 {
			primaries++
			if e.Health == nil {
				add(CodeEndpoint, LevelError, rel, "/endpoints/"+e.Name, "主入口 %s 没有 health：装好与否、升级成功与否都靠它判断", e.Name)
			}
		}
	}
	if primaries != 1 {
		add(CodeEndpoint, LevelError, rel, "/endpoints", "要有且只有一个主入口（primary: true），现在有 %d 个", primaries)
	}
	uses := map[string]Use{}
	for _, u := range m.Uses {
		uses[u.Name] = u
		if u.ForParam != "" {
			if p, ok := params[u.ForParam]; !ok || p.Type != "llm_provider" {
				add(CodeReference, LevelError, rel, "/uses/"+u.Name, "for_param 指向 %q，它不存在或不是 llm_provider 参数", u.ForParam)
			}
		}
	}
	checkRef := func(path, ref string) {
		kind, full, ok := strings.Cut(ref, ".")
		if !ok {
			return
		}
		name, _, _ := strings.Cut(full, ".")
		switch kind {
		case "platform":
			pv, ok := LookupPlatformVar(full)
			switch {
			case !ok:
				add(CodePlatformVar, LevelError, rel, path, "没有平台变量 platform.%s（平台变量只能用程序里那张表上的名字，见 acornfox catalog platform-vars）", full)
			case pv.ViaParam:
				add(CodePlatformVar, LevelError, rel, path, "platform.%s 不能直接引用：用一个 llm_provider 参数，模板里写 params.<参数>.base_url / api_key，用户选平台时自动填它", full)
			}
		case "params":
			if _, ok := params[name]; !ok {
				add(CodeReference, LevelError, rel, path, "引用了不存在的参数 params.%s", name)
			}
		case "uses":
			if _, ok := uses[name]; !ok {
				add(CodeReference, LevelError, rel, path, "引用了不存在的 uses.%s", name)
			}
		case "volumes":
			if !volumes[name] {
				add(CodeReference, LevelError, rel, path, "引用了不存在的数据卷 volumes.%s", name)
			}
		case "requires":
			add(CodeReference, LevelError, rel, path, "首发还不支持 requires（独立附加服务），不能引用 requires.%s", name)
		}
	}
	for _, e := range m.Env {
		if e.From != "" {
			checkRef("/env/"+e.Key, e.From)
		}
	}
	for _, f := range m.FirstLogin {
		if f.From != "" {
			checkRef("/first_login/"+f.Label, f.From)
		}
	}
	for _, d := range m.Uninstall.Data {
		checkRef("/uninstall/data", d)
	}
	for _, f := range m.Files {
		if !volumes[f.Volume] {
			add(CodeReference, LevelError, rel, "/files/"+f.Path, "写配置文件用的数据卷 %q 不存在", f.Volume)
		}
		for _, r := range templateRef.FindAllStringSubmatch(f.Template, -1) {
			checkRef("/files/"+f.Path, r[1]+"."+r[2])
		}
	}
	for _, s := range m.Init {
		b, _ := json.Marshal(s.HTTP.JSON)
		for _, r := range templateRef.FindAllStringSubmatch(string(b), -1) {
			checkRef("/init/"+s.Name, r[1]+"."+r[2])
		}
	}
	for _, p := range m.Provides {
		if !endpoints[p.Endpoint] {
			add(CodeReference, LevelError, rel, "/provides", "provides 指向不存在的入口 %q", p.Endpoint)
		}
	}
	for i, img := range m.Release.Images {
		if img.Artifact != "" && img.ArtifactSHA256 == "" {
			add(CodeArtifact, LevelWarning, rel, fmt.Sprintf("/release/images/%d", i), "镜像 %s 填了 artifact 但没填 artifact_sha256：安装时不会走下载网关，只能从公共源拉", img.Name)
		}
		checkArchArtifacts(rel, fmt.Sprintf("/release/images/%d", i), img, m.Resources.Platforms, add)
	}
	if d := m.Exposure.Default; d != "" && d != DefaultExposure {
		add(CodeExposure, LevelWarning, rel, "/exposure/default", "默认访问方式是 %s 不是 local：上架审核时要确认它有登录保护", d)
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
	for _, a := range r.Apps {
		inRecipe[a.App] = true
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
			add(CodeRecipe, LevelError, rel, "/links", "%s 的清单里没有名为 %s 的 uses", l.To, l.Uses)
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
