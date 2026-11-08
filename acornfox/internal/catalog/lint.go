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
	LevelError      = "error"
	LevelWarning    = "warning"
	manifestName    = "manifest.yaml"
	recipeName      = "recipe.yaml"
	manifestSchemaF = "schema/manifest.schema.json"
	recipeSchemaF   = "schema/recipe.schema.json"
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
}

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
	mSchema, err := compileSchema(manifestSchemaF)
	if err != nil {
		return Report{}, err
	}
	rSchema, err := compileSchema(recipeSchemaF)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Apps: []string{}, Recipes: []string{}, Problems: []Problem{}}
	add := func(code, level, file, path, format string, args ...any) {
		rep.Problems = append(rep.Problems, Problem{Code: code, Level: level, File: file, Path: path, Message: fmt.Sprintf(format, args...)})
	}

	manifests := map[string]Manifest{}
	for _, sub := range listDirs(filepath.Join(dir, "apps")) {
		rel := filepath.ToSlash(filepath.Join("apps", sub, manifestName))
		var m Manifest
		if !loadAndValidate(filepath.Join(dir, rel), rel, mSchema, &m, add) {
			continue
		}
		rep.Apps = append(rep.Apps, sub)
		manifests[sub] = m
		checkManifest(dir, rel, sub, m, add)
		checkPackable(filepath.Join(dir, "apps", sub), sub, add)
	}
	for _, sub := range listDirs(filepath.Join(dir, "recipes")) {
		rel := filepath.ToSlash(filepath.Join("recipes", sub, recipeName))
		var r Recipe
		if !loadAndValidate(filepath.Join(dir, rel), rel, rSchema, &r, add) {
			continue
		}
		rep.Recipes = append(rep.Recipes, sub)
		checkRecipe(rel, sub, r, manifests, add)
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
// cannot be used for the cross-file checks.
func loadAndValidate(path, rel string, sch *jsonschema.Schema, out any, add addFunc) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		add(CodeRead, LevelError, rel, "", "读不了：%v", err)
		return false
	}
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		add(CodeRead, LevelError, rel, "", "不是合法的 YAML：%v", err)
		return false
	}
	// Round-trip through JSON so the validator sees JSON types only.
	js, err := json.Marshal(raw)
	if err != nil {
		add(CodeRead, LevelError, rel, "", "转成 JSON 失败（键必须是字符串）：%v", err)
		return false
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(js))
	if err != nil {
		add(CodeRead, LevelError, rel, "", "转成 JSON 失败：%v", err)
		return false
	}
	ok := true
	if err := sch.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			for _, leaf := range leaves(ve) {
				add(CodeSchema, LevelError, rel, "/"+strings.Join(leaf.InstanceLocation, "/"), "%s", leafMessage(leaf))
			}
		} else {
			add(CodeSchema, LevelError, rel, "", "%v", err)
		}
		ok = false
	}
	if err := yaml.Unmarshal(b, out); err != nil {
		add(CodeRead, LevelError, rel, "", "字段类型不对：%v", err)
		return false
	}
	return ok
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
	}
	if d := m.Exposure.Default; d != "" && d != DefaultExposure {
		add(CodeExposure, LevelWarning, rel, "/exposure/default", "默认访问方式是 %s 不是 local：上架审核时要确认它有登录保护", d)
	}
	if m.Delivery.Tier == "steps" && len(m.Delivery.UserSteps) == 0 {
		add(CodeUserSteps, LevelError, rel, "/delivery", "交付方式是“几步”，但没写 user_steps")
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
