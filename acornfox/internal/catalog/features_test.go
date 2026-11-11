package catalog

import (
	"crypto/ed25519"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// withFeature registers (or overrides) a feature for one test, standing in
// for something a future platform version adds.
func withFeature(t *testing.T, key, since string) {
	t.Helper()
	old, had := featureSince[key]
	featureSince[key] = since
	t.Cleanup(func() {
		if had {
			featureSince[key] = old
		} else {
			delete(featureSince, key)
		}
	})
}

// withPlatformVar adds a platform variable for one test.
func withPlatformVar(t *testing.T, pv PlatformVar) {
	t.Helper()
	n := len(PlatformVars)
	PlatformVars = append(PlatformVars[:n:n], pv)
	platformVarByName[pv.Name] = pv
	t.Cleanup(func() {
		PlatformVars = PlatformVars[:n]
		delete(platformVarByName, pv.Name)
	})
}

// schemaFeatureKeys lists every property path and enum/const value of a
// schema the way the feature table names them. It is written apart from
// walkFeatures on purpose: it walks the schema alone, with no document.
func schemaFeatureKeys(kind string, root map[string]any) []string {
	var out []string
	var walk func(node map[string]any, path string)
	walk = func(node map[string]any, path string) {
		if ref, ok := node["$ref"].(string); ok {
			node = root["$defs"].(map[string]any)[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		}
		if path != "" {
			out = append(out, kind+":"+path)
		}
		if enum, ok := node["enum"].([]any); ok {
			for _, v := range enum {
				s, _ := scalarText(v)
				out = append(out, kind+":"+path+"="+s)
			}
		}
		if c, ok := node["const"]; ok {
			s, _ := scalarText(c)
			out = append(out, kind+":"+path+"="+s)
		}
		if props, ok := node["properties"].(map[string]any); ok {
			for k, sub := range props {
				walk(sub.(map[string]any), path+"/"+k)
			}
		}
		if items, ok := node["items"].(map[string]any); ok {
			walk(items, path+"/*")
		}
	}
	walk(root, "")
	sort.Strings(out)
	return out
}

// missingFeatures returns the schema keys that have no row in the table.
func missingFeatures(kind string, root map[string]any) []string {
	var missing []string
	for _, k := range schemaFeatureKeys(kind, root) {
		if _, ok := featureSince[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing
}

func loadSchemaDoc(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := schemaFS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// Every field and every enum value of the manifest and recipe formats must
// have a row in the feature table, with the platform version that first
// ships it. Whoever adds to a schema and forgets the table is stopped here.
func TestFeatureTableCoversSchemas(t *testing.T) {
	inSchema := map[string]bool{}
	for kind, file := range map[string]string{"manifest": manifestSchemaF, "recipe": recipeSchemaF} {
		root := loadSchemaDoc(t, file)
		if missing := missingFeatures(kind, root); len(missing) > 0 {
			t.Errorf("%s 里这些字段或取值没登记进特性表（features.go 的 featureTable，写上从哪个平台版本起有）：\n  %s", file, strings.Join(missing, "\n  "))
		}
		for _, k := range schemaFeatureKeys(kind, root) {
			inSchema[k] = true
		}
	}
	version := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	seen := map[string]bool{}
	for _, f := range featureTable {
		if seen[f.Key] {
			t.Errorf("特性表里 %s 重复了", f.Key)
		}
		seen[f.Key] = true
		if !inSchema[f.Key] {
			t.Errorf("特性表里的 %s 在格式定义里找不到（字段删了或改名了？）", f.Key)
		}
		if !version.MatchString(f.Since) || CompareVersions(f.Since, BasePlatform) < 0 {
			t.Errorf("%s 的起始版本 %q 不对：要是 x.y.z，且不低于 %s", f.Key, f.Since, BasePlatform)
		}
	}
	if len(Features()) != len(featureTable) {
		t.Errorf("Features() 有 %d 行，表里 %d 行", len(Features()), len(featureTable))
	}
}

// The check above really catches an unregistered addition: a field and an
// enum value added to a copy of the schema are reported.
func TestFeatureTableCatchesUnregistered(t *testing.T) {
	root := loadSchemaDoc(t, manifestSchemaF)
	props := root["properties"].(map[string]any)
	props["sidecars"] = map[string]any{"type": "array", "items": map[string]any{"type": "object",
		"properties": map[string]any{"image": map[string]any{"type": "string"}}}}
	proto := props["endpoints"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["protocol"].(map[string]any)
	proto["enum"] = append(proto["enum"].([]any), "tcp")
	got := strings.Join(missingFeatures("manifest", root), ",")
	want := "manifest:/endpoints/*/protocol=tcp,manifest:/sidecars,manifest:/sidecars/*,manifest:/sidecars/*/image"
	if got != want {
		t.Fatalf("没登记的应该是 %s，得到 %s", want, got)
	}
	rroot := loadSchemaDoc(t, recipeSchemaF)
	rroot["properties"].(map[string]any)["order"] = map[string]any{"enum": []any{"serial", "parallel"}}
	if got := strings.Join(missingFeatures("recipe", rroot), ","); got != "recipe:/order,recipe:/order=parallel,recipe:/order=serial" {
		t.Fatalf("配方：%s", got)
	}
}

func TestEverythingPublishedNeedsOnlyBasePlatform(t *testing.T) {
	for _, f := range Features() {
		if f.Key != "" && f.Since != BasePlatform {
			// Not wrong in itself; it lists what needs more than the base
			// release. The first such rows were the per-architecture image
			// archives (0.5.1); TestLintPerArchClean checks that recipes
			// using them go to recipes_v2. Whoever adds more: check the
			// docs that say which version a feature comes with.
			t.Logf("%s 从 %s 起有", f.Key, f.Since)
		}
	}
	for _, v := range PlatformVars {
		if v.Since == "" {
			t.Fatalf("平台变量 %s 没写 Since", v.Name)
		}
	}
	for _, id := range []string{"new-api", "hermes-agent"} {
		raw, err := os.ReadFile(filepath.Join("testdata", "published-3", "src", "apps", id, manifestName))
		if err != nil {
			t.Fatal(err)
		}
		mp, err := ManifestMinPlatform(raw)
		if err != nil || mp.Version != BasePlatform || len(mp.Reasons) != 0 {
			t.Fatalf("%s：%+v %v", id, mp, err)
		}
	}
}

func TestManifestMinPlatform(t *testing.T) {
	read := func(dir string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, "apps", "new-api", manifestName))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// A field of a future version.
	withFeature(t, "manifest:/container/shm_mb", "9.9.9")
	dir := copyTree(t)
	file := filepath.Join(dir, "apps", "new-api", manifestName)
	edit(t, file, "endpoints:\n", "container:\n  shm_mb: 128\n\nendpoints:\n")
	mp, err := ManifestMinPlatform(read(dir))
	if err != nil || mp.Version != "9.9.9" {
		t.Fatalf("用了 9.9.9 才有的字段，算出来 %+v %v", mp, err)
	}
	if len(mp.Reasons) != 1 || mp.Reasons[0] != (Requirement{Feature: "manifest:/container/shm_mb", Where: "/container/shm_mb", Since: "9.9.9"}) {
		t.Fatalf("原因 %+v", mp.Reasons)
	}

	// Declaring less is CAT-LINT-17, the message names the place, and the
	// catalog cannot be built.
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	var hit *Problem
	for i := range r.Problems {
		if r.Problems[i].Code == CodeMinPlatform && r.Problems[i].File == "apps/new-api/manifest.yaml" {
			hit = &r.Problems[i]
		}
	}
	if hit == nil || hit.Level != LevelError || hit.Path != "/lifecycle/min_platform" ||
		!strings.Contains(hit.Message, "/container/shm_mb") || !strings.Contains(hit.Message, "9.9.9") || !strings.Contains(hit.Message, "0.5.0") {
		t.Fatalf("要报 %s 并写明原因：%+v", CodeMinPlatform, r.Problems)
	}
	if info := r.MinPlatform.Apps["new-api"]; info.Declared != "0.5.0" || info.Computed != "9.9.9" || info.Required() != "9.9.9" || len(info.Reasons) != 1 {
		t.Fatalf("报告里的版本 %+v", info)
	}
	if info := r.MinPlatform.Apps["hermes-agent"]; info.Declared != "0.5.0" || info.Computed != "0.5.0" || len(info.Reasons) != 0 {
		t.Fatalf("没用新写法的应用 %+v", info)
	}
	if _, err := Build(dir, 1); CodeOf(err) != CodeBuildLint {
		t.Fatalf("写低了最低版本不能打包：%v", err)
	}

	// Declaring what it needs (or more) is fine.
	for _, v := range []string{"9.9.9", "10.0.0"} {
		d := copyTree(t)
		f := filepath.Join(d, "apps", "new-api", manifestName)
		edit(t, f, "endpoints:\n", "container:\n  shm_mb: 128\n\nendpoints:\n")
		edit(t, f, `min_platform: "0.5.0"`, `min_platform: "`+v+`"`)
		r, err := Lint(d)
		if err != nil || codes(r)[CodeMinPlatform] != 0 {
			t.Fatalf("写 %s 不该报：%+v %v", v, r.Problems, err)
		}
		if r.MinPlatform.Apps["new-api"].Required() != v {
			t.Fatalf("Required = %s", r.MinPlatform.Apps["new-api"].Required())
		}
	}
}

func TestManifestMinPlatformEnumValueAndPlatformVar(t *testing.T) {
	// An enum value of a future version: only manifests that use that value.
	withFeature(t, "manifest:/params/*/generate/charset=hex", "9.9.9")
	raw, _ := os.ReadFile(filepath.Join("testdata", "good", "apps", "new-api", manifestName))
	mp, err := ManifestMinPlatform(raw)
	if err != nil || mp.Version != "9.9.9" || len(mp.Reasons) != 1 ||
		mp.Reasons[0].Feature != "manifest:/params/*/generate/charset=hex" || mp.Reasons[0].Where != "/params/session_secret/generate/charset" {
		t.Fatalf("取值：%+v %v", mp, err)
	}
	featureSince["manifest:/params/*/generate/charset=hex"] = BasePlatform

	// A platform variable of a future version, used in a file template; a
	// lower one used elsewhere does not show up among the reasons.
	withPlatformVar(t, PlatformVar{Name: "gpu.count", Label: "显卡数", Standalone: "x", Cloud: "x", Since: "9.9.9"})
	withPlatformVar(t, PlatformVar{Name: "gpu.vendor", Label: "显卡厂商", Standalone: "x", Cloud: "x", Since: "9.8.0"})
	dir := copyTree(t)
	file := filepath.Join(dir, "apps", "hermes-agent", manifestName)
	edit(t, file, "        backend: local\n", "        backend: local\n        gpus: \"{{platform.gpu.count}}\"\n")
	edit(t, file, "  - { key: TZ, from: platform.tz }\n", "  - { key: TZ, from: platform.tz }\n  - { key: GPU_VENDOR, from: platform.gpu.vendor }\n")
	raw, _ = os.ReadFile(file)
	mp, err = ManifestMinPlatform(raw)
	if err != nil || mp.Version != "9.9.9" || len(mp.Reasons) != 1 ||
		mp.Reasons[0] != (Requirement{Feature: "platform.gpu.count", Where: "/files/config.yaml", Since: "9.9.9"}) {
		t.Fatalf("平台变量：%+v %v", mp, err)
	}
	r, _ := Lint(dir)
	if codes(r)[CodeMinPlatform] != 1 || codes(r)[CodePlatformVar] != 0 {
		t.Fatalf("%+v", r.Problems)
	}

	// Not YAML: an error, not a version.
	if _, err := ManifestMinPlatform([]byte("a: [")); err == nil {
		t.Fatal("不是 YAML 要报错")
	}
}

func TestRecipeMinPlatform(t *testing.T) {
	// The recipe's own syntax.
	withFeature(t, "recipe:/apps/*/preset", "9.9.9")
	dir := copyTree(t)
	file := filepath.Join(dir, "recipes", "new-api", recipeName)
	edit(t, file, "  - app: new-api\n", "  - app: new-api\n    preset: { admin_username: root }\n")
	raw, _ := os.ReadFile(file)
	mp, err := RecipeMinPlatform(raw, map[string]string{"new-api": "0.5.0"})
	if err != nil || mp.Version != "9.9.9" || len(mp.Reasons) != 1 || mp.Reasons[0] != (Requirement{Feature: "recipe:/apps/*/preset", Where: "/apps/new-api/preset", Since: "9.9.9"}) {
		t.Fatalf("配方自己的写法：%+v %v", mp, err)
	}
	// Left out: no complaint, build works it out. Written too low: CAT-LINT-17.
	r, _ := Lint(dir)
	if codes(r)[CodeMinPlatform] != 0 {
		t.Fatalf("配方没写 min_platform 不该报：%+v", r.Problems)
	}
	if info := r.MinPlatform.Recipes["new-api"]; info.Declared != "" || info.Computed != "9.9.9" || info.Required() != "9.9.9" {
		t.Fatalf("%+v", info)
	}
	edit(t, file, "schema: 1\n", "schema: 1\nmin_platform: \"0.5.0\"\n")
	r, _ = Lint(dir)
	var hit *Problem
	for i := range r.Problems {
		if r.Problems[i].Code == CodeMinPlatform {
			hit = &r.Problems[i]
		}
	}
	if hit == nil || hit.File != "recipes/new-api/recipe.yaml" || hit.Path != "/min_platform" || !strings.Contains(hit.Message, "/apps/new-api/preset") {
		t.Fatalf("配方写低了要报 %s：%+v", CodeMinPlatform, r.Problems)
	}
	edit(t, file, `min_platform: "0.5.0"`, `min_platform: "9.9.9"`)
	if r, _ = Lint(dir); r.Errors() != 0 {
		t.Fatalf("%+v", r.Problems)
	}

	// A member app that needs more lifts the recipe.
	mp, _ = RecipeMinPlatform([]byte("id: x\nschema: 1\napps:\n  - app: a\n  - app: b\n  - app: gone\n"), map[string]string{"a": "0.5.0", "b": "1.2.0"})
	if mp.Version != "1.2.0" || len(mp.Reasons) != 1 || mp.Reasons[0] != (Requirement{Feature: "app:b", Where: "/apps/b", Since: "1.2.0"}) {
		t.Fatalf("成员应用：%+v", mp)
	}
}

// Recipes that need more than the first platform release go to recipes_v2,
// where that release does not look; the others stay where they were.
func TestBuildPutsNewerRecipesInV2(t *testing.T) {
	withFeature(t, "manifest:/container/shm_mb", "9.9.9")
	dir := copyTree(t)
	file := filepath.Join(dir, "apps", "new-api", manifestName)
	edit(t, file, "endpoints:\n", "container:\n  shm_mb: 128\n\nendpoints:\n")
	edit(t, file, `min_platform: "0.5.0"`, `min_platform: "9.9.9"`)
	// One recipe declares a version itself, higher than anything it uses.
	edit(t, filepath.Join(dir, "recipes", "hermes-with-new-api", recipeName), "schema: 1\n", "schema: 1\nmin_platform: \"10.1.0\"\n")
	b, err := Build(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndex(b.Index)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Recipes) != 1 || idx.Recipes[0].ID != "hermes-agent" || idx.Recipes[0].MinPlatform != "" {
		t.Fatalf("recipes = %+v", idx.Recipes)
	}
	if len(idx.RecipesV2) != 2 || idx.RecipesV2[0].ID != "hermes-with-new-api" || idx.RecipesV2[0].MinPlatform != "10.1.0" ||
		idx.RecipesV2[1].ID != "new-api" || idx.RecipesV2[1].MinPlatform != "9.9.9" {
		t.Fatalf("recipes_v2 = %+v", idx.RecipesV2)
	}
	all := idx.AllRecipes()
	if len(all) != 3 || all[0].ID != "hermes-agent" || all[1].ID != "hermes-with-new-api" || all[2].ID != "new-api" {
		t.Fatalf("AllRecipes = %+v", all)
	}
	// What the first release does: plain JSON decoding into the fields it
	// knows, index schema still 1. It sees one recipe and no error.
	var old struct {
		Schema  int `json:"schema"`
		Recipes []struct {
			ID string `json:"id"`
		} `json:"recipes"`
		Apps []struct {
			ID          string `json:"id"`
			MinPlatform string `json:"min_platform"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(b.Index, &old); err != nil || old.Schema != 1 || len(old.Recipes) != 1 || old.Recipes[0].ID != "hermes-agent" {
		t.Fatalf("旧客户端看到的：%+v %v", old, err)
	}
	if len(old.Apps) != 2 || old.Apps[1].MinPlatform != "9.9.9" {
		t.Fatalf("应用的最低版本仍在 apps 里：%+v", old.Apps)
	}
	// Key order in the file: recipes, then recipes_v2.
	if i, j := strings.Index(string(b.Index), `"recipes": [`), strings.Index(string(b.Index), `"recipes_v2": [`); i < 0 || j < i {
		t.Fatalf("index.json 里 recipes_v2 的位置不对")
	}
	// VerifyDir counts both.
	pub, priv := testKey(t)
	b.Sign(priv)
	out := t.TempDir()
	if _, err := b.Write(out); err != nil {
		t.Fatal(err)
	}
	res, err := VerifyDir(out, []ed25519.PublicKey{pub})
	if err != nil || res.Recipes != 3 {
		t.Fatalf("verify: %+v %v", res, err)
	}

	// A recipe that writes the base version stays in recipes, as written.
	dir = copyTree(t)
	edit(t, filepath.Join(dir, "recipes", "new-api", recipeName), "schema: 1\n", "schema: 1\nmin_platform: \"0.5.0\"\n")
	b, err = Build(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	idx, _ = ParseIndex(b.Index)
	if len(idx.Recipes) != 3 || len(idx.RecipesV2) != 0 || idx.Recipes[2].MinPlatform != "0.5.0" || strings.Contains(string(b.Index), "recipes_v2") {
		t.Fatalf("写 0.5.0 的配方：%+v", idx.Recipes)
	}
}

func TestAllRecipesDuplicateID(t *testing.T) {
	idx := &Index{Recipes: []Recipe{{ID: "b", Title: "old"}}, RecipesV2: []Recipe{{ID: "b", Title: "new"}, {ID: "a"}}}
	all := idx.AllRecipes()
	if len(all) != 2 || all[0].ID != "a" || all[1].Title != "old" {
		t.Fatalf("%+v", all)
	}
	if got := (&Index{}).AllRecipes(); got == nil || len(got) != 0 {
		t.Fatalf("空索引要给空列表：%#v", got)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.5.0", "0.5.0", 0}, {"0.5.1", "0.5.0", 1}, {"0.5.0", "0.10.0", -1}, {"1.0.0", "0.99.99", 1},
		{"v0.5.1", "0.5.1", 0}, {"0.5.1-rc.1", "0.5.1", 0}, {"0.5.1-dev.pv7", "0.5.0", 1}, {"", "0.5.0", -1}, {"nonsense", "", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d，要 %d", c.a, c.b, got, c.want)
		}
	}
	if maxVersion("0.5.0", "0.6.0") != "0.6.0" || maxVersion("0.6.0", "") != "0.6.0" || maxVersion("", "0.5.0") != "0.5.0" {
		t.Fatal("maxVersion")
	}
}

func TestValidateManifest(t *testing.T) {
	good, err := os.ReadFile(filepath.Join("testdata", "good", "apps", "new-api", manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if ps := ValidateManifest(good); ps == nil || len(ps) != 0 {
		t.Fatalf("好的清单：%+v", ps)
	}
	cases := []struct {
		name, old, new, code string
	}{
		{"unknown field", "maintainer: acornfox", "maintainer: acornfox\nsidecars: []", CodeSchema},
		{"unknown nested field", "    backup: true", "    backup: true\n    snapshot: daily", CodeSchema},
		{"unknown enum value", "protocol: http", "protocol: tcp", CodeSchema},
		{"unknown format version", "schema: 2", "schema: 3", CodeSchema},
		{"wrong type", "port: 3000", "port: three thousand", CodeSchema},
		{"not yaml", "id: new-api", "id: [new-api", CodeRead},
	}
	for _, c := range cases {
		if !strings.Contains(string(good), c.old) {
			t.Fatalf("%s：清单里没有 %q", c.name, c.old)
		}
		ps := ValidateManifest([]byte(strings.Replace(string(good), c.old, c.new, 1)))
		if len(ps) == 0 || ps[0].Code != c.code || ps[0].Level != LevelError || ps[0].File != manifestName {
			t.Errorf("%s：要报 %s，得到 %+v", c.name, c.code, ps)
		}
	}
	// Schema only: what Lint adds on top (an id that differs from its
	// directory, a missing icon file) is not its business.
	if ps := ValidateManifest([]byte(strings.Replace(string(good), "icon: icon.svg", "icon: other.svg", 1))); len(ps) != 0 {
		t.Fatalf("只按格式定义查：%+v", ps)
	}
	if ps := ValidateManifest(nil); len(ps) == 0 {
		t.Fatal("空内容要有问题")
	}

	recipe, _ := os.ReadFile(filepath.Join("testdata", "good", "recipes", "hermes-with-new-api", recipeName))
	if ps := ValidateRecipe(recipe); len(ps) != 0 {
		t.Fatalf("好的配方：%+v", ps)
	}
	if ps := ValidateRecipe(append([]byte("min_platform: \"0.5.1\"\n"), recipe...)); len(ps) != 0 {
		t.Fatalf("配方可以写 min_platform：%+v", ps)
	}
	if ps := ValidateRecipe(append([]byte("min_platform: latest\nparallel: true\n"), recipe...)); len(ps) == 0 || ps[0].Code != CodeSchema || ps[0].File != recipeName {
		t.Fatalf("坏的配方：%+v", ps)
	}
}

// The manifests inside the packages that were actually published must pass
// the check the install engine runs before installing.
func TestValidateManifestAcceptsPublishedPackages(t *testing.T) {
	snap := Snapshot()
	b, err := fs.ReadFile(snap, IndexFile)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndex(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Apps) < 2 {
		t.Fatalf("快照里只有 %d 个应用", len(idx.Apps))
	}
	for _, a := range idx.Apps {
		pkg, err := fs.ReadFile(snap, a.Package.Path)
		if err != nil {
			t.Fatal(err)
		}
		files, err := ReadPackage(a.ID, pkg)
		if err != nil {
			t.Fatal(err)
		}
		if ps := ValidateManifest(files[manifestName]); len(ps) != 0 {
			t.Errorf("已发布的 %s %s 过不了检查：%+v", a.ID, a.Version, ps)
		}
		mp, err := ManifestMinPlatform(files[manifestName])
		if err != nil || CompareVersions(mp.Version, a.MinPlatform) > 0 {
			t.Errorf("已发布的 %s 写的最低版本 %s 低于算出来的 %s（%v）", a.ID, a.MinPlatform, mp.Version, err)
		}
	}
}

// fullDocument builds a document that writes every field a schema describes:
// every property of every object, one item in every list. An enum field
// takes the value pick names for its path (as the feature table spells
// paths), else its first value. The document need not be valid; it is for
// walking only.
func fullDocument(t *testing.T, root, node map[string]any, path string, pick map[string]string) any {
	t.Helper()
	if ref, ok := node["$ref"].(string); ok {
		def, ok := root["$defs"].(map[string]any)[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		if !ok {
			t.Fatalf("%s：格式定义里的引用 %s 解不开", path, ref)
		}
		node = def
	}
	if props, ok := node["properties"].(map[string]any); ok {
		obj := map[string]any{}
		for k, sub := range props {
			obj[k] = fullDocument(t, root, sub.(map[string]any), path+"/"+k, pick)
		}
		return obj
	}
	if items, ok := node["items"].(map[string]any); ok {
		return []any{fullDocument(t, root, items, path+"/*", pick)}
	}
	if c, ok := node["const"]; ok {
		return c
	}
	if enum, ok := node["enum"].([]any); ok {
		want, picked := pick[path]
		for _, v := range enum {
			if s, _ := scalarText(v); picked && s == want {
				return v
			}
		}
		if picked {
			t.Fatalf("%s 的取值里没有 %q", path, want)
		}
		return enum[0]
	}
	switch node["type"] {
	case "integer", "number":
		return float64(1)
	case "boolean":
		return true
	case "object":
		return map[string]any{"anything": "x"}
	case "array":
		return []any{"x"}
	}
	return "x"
}

// Every row of the feature table must be reachable by the walk the program
// really uses: a manifest (or recipe) that writes the field or value of a row
// which is then lifted to a future version has to come out needing that
// version. A row the walk cannot reach (a field behind a reference it fails
// to follow, say) would let a manifest through with too low a version.
func TestEveryFeatureRowLiftsTheVersion(t *testing.T) {
	files := map[string]string{"manifest": manifestSchemaF, "recipe": recipeSchemaF}
	if len(featureTable) < 200 {
		t.Fatalf("特性表只有 %d 行，表被改短了？", len(featureTable))
	}
	for _, f := range featureTable {
		kind, rest, _ := strings.Cut(f.Key, ":")
		file, ok := files[kind]
		if !ok {
			t.Fatalf("%s：不认识的种类 %q", f.Key, kind)
		}
		root := loadSchemaDoc(t, file)
		pick := map[string]string{}
		if path, value, isValue := strings.Cut(rest, "="); isValue {
			pick[path] = value
		}
		doc := fullDocument(t, root, root, "", pick)

		// Nothing lifted: the full document needs the highest version in
		// the table for its kind (it writes every field).
		uses, err := usedFeatures(kind, file, doc)
		if err != nil {
			t.Fatal(err)
		}
		if mp := minPlatformOf(uses); mp.Version != highestSince(kind) {
			t.Fatalf("%s：没抬高任何一行时算出来是 %s，表里最高是 %s", f.Key, mp.Version, highestSince(kind))
		}

		old := featureSince[f.Key]
		featureSince[f.Key] = "9.9.9"
		uses, err = usedFeatures(kind, file, doc)
		featureSince[f.Key] = old
		if err != nil {
			t.Fatal(err)
		}
		mp := minPlatformOf(uses)
		if mp.Version != "9.9.9" || len(mp.Reasons) != 1 || mp.Reasons[0].Feature != f.Key {
			t.Errorf("把 %s 抬到 9.9.9，写了它的文件算出来却是 %s（原因 %+v）：遍历走不到这一行", f.Key, mp.Version, mp.Reasons)
		}
	}
}

// highestSince is the highest platform version of the table's rows of kind.
func highestSince(kind string) string {
	v := BasePlatform
	for _, f := range featureTable {
		if strings.HasPrefix(f.Key, kind+":") {
			v = maxVersion(v, f.Since)
		}
	}
	return v
}

// The same through the exported entry point, for a field that sits behind a
// "$ref" in the schema (resources.memory_mb is defined under $defs).
func TestMinPlatformSeesFieldsBehindReferences(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "good", "apps", "new-api", manifestName))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"manifest:/resources/memory_mb/min", "manifest:/resources/memory_mb/recommended"} {
		withFeature(t, key, "9.9.9")
		mp, err := ManifestMinPlatform(raw)
		if err != nil || mp.Version != "9.9.9" || len(mp.Reasons) != 1 || mp.Reasons[0].Feature != key ||
			mp.Reasons[0].Where != "/resources/memory_mb/"+key[strings.LastIndex(key, "/")+1:] {
			t.Fatalf("%s：%+v %v", key, mp, err)
		}
		featureSince[key] = BasePlatform
	}
}

func TestResolveRef(t *testing.T) {
	root := map[string]any{"$defs": map[string]any{"minRec": map[string]any{"type": "object"}}}
	plain := map[string]any{"type": "string"}
	if got, err := resolveRef(root, plain); err != nil || got["type"] != "string" {
		t.Fatalf("没有引用的节点原样返回：%v %v", got, err)
	}
	if got, err := resolveRef(root, map[string]any{"$ref": "#/$defs/minRec"}); err != nil || got["type"] != "object" {
		t.Fatalf("本地引用：%v %v", got, err)
	}
	for _, bad := range []any{"#/$defs/nope", "#/$defs/", "#/definitions/minRec", "other.json#/$defs/minRec", "", 7} {
		if got, err := resolveRef(root, map[string]any{"$ref": bad}); err == nil || got != nil {
			t.Errorf("引用 %v 解不开要报错，得到 %v", bad, got)
		}
	}
	if _, err := resolveRef(map[string]any{}, map[string]any{"$ref": "#/$defs/minRec"}); err == nil {
		t.Error("没有 $defs 时要报错")
	}
	// A broken reference stops the whole calculation instead of being
	// counted as "uses nothing".
	broken := map[string]any{"properties": map[string]any{"a": map[string]any{"$ref": "#/$defs/nope"}}}
	var out []Requirement
	if err := walkFeatures("manifest", broken, broken, map[string]any{"a": map[string]any{}}, "", "", &out); err == nil {
		t.Fatal("遍历遇到解不开的引用要报错")
	}
	// The real schemas do use references, and all of them resolve.
	for _, file := range []string{manifestSchemaF, recipeSchemaF} {
		b, _ := schemaFS.ReadFile(file)
		root := loadSchemaDoc(t, file)
		doc := fullDocument(t, root, root, "", nil)
		if _, err := usedFeatures(strings.TrimSuffix(filepath.Base(file), ".schema.json"), file, doc); err != nil {
			t.Fatalf("%s：%v", file, err)
		}
		if file == manifestSchemaF && !strings.Contains(string(b), `"$ref": "#/$defs/minRec"`) {
			t.Fatal("清单格式定义里没有 $ref 了：这条测试要换一个经引用的字段")
		}
	}
}

func TestDeclaredMinPlatform(t *testing.T) {
	good, _ := os.ReadFile(filepath.Join("testdata", "good", "apps", "new-api", manifestName))
	cases := map[string]string{
		string(good): "0.5.0",
		"lifecycle:\n  min_platform: \"9.9.9\"\nendpoints: later-format\nvolumes: {a: 1}\n": "9.9.9", // nothing else has to be decodable
		"lifecycle:\n  status: active\n":  "",
		"lifecycle: soon\n":               "",
		"lifecycle:\n  min_platform: 7\n": "",
		"- a list\n":                      "",
		"a: [":                            "",
		"":                                "",
	}
	for raw, want := range cases {
		if got := DeclaredMinPlatform([]byte(raw)); got != want {
			t.Errorf("DeclaredMinPlatform(%.40q) = %q, want %q", raw, got, want)
		}
	}
}

// The format definitions are pinned by checksum. The other tests notice a new
// field or enum value (it needs a row in the feature table), but not a
// constraint that was loosened: a larger maxItems, a wider pattern, a dropped
// "required", another type allowed. Such a change adds no path, yet a
// manifest that is only valid under the new rule may be installed wrongly by
// a platform that was released with the old one.
func TestFormatDefinitionsArePinned(t *testing.T) {
	pinned := map[string]string{
		manifestSchemaF: "ae2c40e779c603a3b5f45294813a68dd22c1ebaaecddcd9a3cf9c4402e729a01",
		recipeSchemaF:   "9334a4f1487e2c1d8352d547ef538b6830e5ef3b4ad8ef8faf37f5a1a4c671a2",
	}
	for file, want := range pinned {
		b, err := schemaFS.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if got := sha256Hex(b); got != want {
			t.Errorf(`%s has changed: its sha256 is now %s.

Changing a format definition is a compatibility decision, not just an edit.
Before you put the new checksum into this test, do all of the following:

 1. Feature table: every new field and every new enum value needs a row in
    featureTable (features.go) with the platform version that first ships it,
    so that manifests using it get the right minimum platform version.
 2. Older clients: think through what a platform that is already released
    does with a manifest that is only valid under the new definition. This
    matters most when you loosen something (larger maxItems, wider pattern,
    dropped "required", extra type): no test catches that. If an older
    platform would install such a manifest wrongly, the new form must raise
    the minimum platform version (a feature row plus a detector in
    manifestMinPlatform), because lifecycle.min_platform is the only thing
    an older platform checks.
 3. Catalog repository: copy the file to the catalog repository's schema/
    directory and commit it there; the two copies must stay byte-identical
    (TestEmbeddedSchemasMatchCatalogCopy checks when the checkout is nearby).
 4. Published manifests must still validate and build to the same bytes
    (TestValidateManifestAcceptsPublishedPackages, TestBuildMatchesPublishedIndex).`, file, got)
		}
	}
}
