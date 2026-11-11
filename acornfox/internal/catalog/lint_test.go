package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyTree copies the good catalog into a temp dir so a test can break it.
func copyTree(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("testdata", "good")
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func edit(t *testing.T, path, old, new string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s 里没有 %q", path, old)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func codes(r Report) map[string]int {
	m := map[string]int{}
	for _, p := range r.Problems {
		m[p.Code]++
	}
	return m
}

func TestLintGoodCatalog(t *testing.T) {
	r, err := Lint(filepath.Join("testdata", "good"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Errors() != 0 {
		t.Fatalf("好的目录不该有错误：%+v", r.Problems)
	}
	// Hermes pins its gateway artifact's checksum, so nothing to warn about.
	if len(r.Problems) != 0 {
		t.Fatalf("好的目录不该有提醒：%+v", r.Problems)
	}
	if strings.Join(r.Apps, ",") != "hermes-agent,new-api" {
		t.Fatalf("apps = %v", r.Apps)
	}
	if len(r.Recipes) != 3 {
		t.Fatalf("recipes = %v", r.Recipes)
	}
}

func TestLintFindsProblems(t *testing.T) {
	cases := []struct {
		name string
		file string
		old  string
		new  string
		code string
	}{
		{"schema: latest tag", "apps/new-api/manifest.yaml", "tag: v1.0.0-rc.48", "tag: latest", CodeSchema},
		{"schema: unknown field", "apps/new-api/manifest.yaml", "maintainer: acornfox", "maintainer: acornfox\nhostscript: rm -rf /", CodeSchema},
		{"schema: bad digest", "apps/new-api/manifest.yaml", "digest: sha256:51401a46", "digest: sha256:zz401a46", CodeSchema},
		{"id differs", "apps/new-api/manifest.yaml", "id: new-api", "id: newapi", CodeID},
		{"icon missing", "apps/new-api/manifest.yaml", "icon: icon.svg", "icon: missing.svg", CodeIcon},
		{"agpl without source", "apps/new-api/manifest.yaml", "  source_code: https://github.com/QuantumNous/new-api\n", "", CodeLicense},
		{"env refers to missing param", "apps/new-api/manifest.yaml", "from: params.session_secret", "from: params.nope", CodeReference},
		{"init refers to missing param", "apps/new-api/manifest.yaml", `username: "{{params.admin_username}}"`, `username: "{{params.who}}"`, CodeReference},
		{"hidden without generate", "apps/new-api/manifest.yaml", "    generate: { length: 64, charset: hex }\n", "", CodeSecret},
		{"health missing", "apps/new-api/manifest.yaml", "    health:\n      path: /api/status\n      expect_json: { success: true }\n      timeout_s: 60\n", "", CodeEndpoint},
		{"exposure public warns", "apps/new-api/manifest.yaml", "default: local", "default: public", CodeExposure},
		{"schema: bad artifact sha256", "apps/hermes-agent/manifest.yaml", "artifact_sha256: d356bba314af272039743d82123bf57a38e67696d69558a6871c4b999700df45", "artifact_sha256: ZZ", CodeSchema},
		{"artifact without checksum warns", "apps/hermes-agent/manifest.yaml", "      artifact_sha256: d356bba314af272039743d82123bf57a38e67696d69558a6871c4b999700df45\n      artifact_size: 1120227840\n", "", CodeArtifact},
		{"homepage javascript:", "apps/new-api/manifest.yaml", "homepage: https://github.com/QuantumNous/new-api", "homepage: \"javascript:alert(1)\"", CodeURL},
		{"source_code data:", "apps/new-api/manifest.yaml", "source_code: https://github.com/QuantumNous/new-api", "source_code: \"data:text/html,x\"", CodeURL},
		{"app video javascript:", "apps/new-api/manifest.yaml", "delivery:\n  tier: auto\n", "delivery:\n  tier: auto\n  video: \"javascript:alert(1)\"\n", CodeURL},
		{"recipe video javascript:", "recipes/new-api/recipe.yaml", "  tier: auto\n", "  tier: auto\n  video: \"javascript:alert(1)\"\n", CodeURL},
		{"env refers to unknown platform var", "apps/new-api/manifest.yaml", "from: platform.tz", "from: platform.timezone", CodePlatformVar},
		{"file refers to unknown platform var", "apps/hermes-agent/manifest.yaml", `default: "{{params.llm.model}}"`, `default: "{{platform.llm.model.default}}"`, CodePlatformVar},
		{"platform key used directly", "apps/hermes-agent/manifest.yaml", `api_key: "{{params.llm.api_key}}"`, `api_key: "{{platform.llm.api_key}}"`, CodePlatformVar},
		{"schema: env from unknown kind", "apps/new-api/manifest.yaml", "from: platform.tz", "from: host.tz", CodeSchema},
		{"recipe unknown app", "recipes/new-api/recipe.yaml", "app: new-api", "app: no-such-app", CodeRecipe},
		{"link to missing use", "recipes/hermes-with-new-api/recipe.yaml", "uses: llm_gateway", "uses: nothing", CodeRecipe},
		{"steps without user_steps", "recipes/new-api/recipe.yaml", "delivery:\n  tier: auto\n  minutes: 1\n  memory_mb: 512\n  user_steps:\n    - 登录后新建一个渠道，填上游的 Key 和模型，点“测试”\n", "delivery:\n  tier: steps\n", CodeUserSteps},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyTree(t)
			edit(t, filepath.Join(dir, c.file), c.old, c.new)
			r, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			if codes(r)[c.code] == 0 {
				t.Fatalf("没报 %s：%+v", c.code, r.Problems)
			}
		})
	}
}

func TestLintUnpackableFiles(t *testing.T) {
	dir := copyTree(t)
	app := filepath.Join(dir, "apps", "new-api")
	if err := os.WriteFile(filepath.Join(app, "说明.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, ".hidden-说明"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("a", maxPackedName)
	if err := os.WriteFile(filepath.Join(app, long), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("icon.svg", filepath.Join(app, "link.svg")); err != nil {
		t.Fatal(err)
	}
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n := codes(r)[CodeFileName]; n != 3 {
		t.Fatalf("要报 3 个 %s（中文名、过长、符号链接；点开头的不打包不报），得到 %d：%+v", CodeFileName, n, r.Problems)
	}
	if _, err := Build(dir, 1); CodeOf(err) != CodeBuildLint {
		t.Fatalf("打包要被 lint 拦下：%v", err)
	}
}

func TestLintEmptyDir(t *testing.T) {
	r, err := Lint(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if codes(r)[CodeEmpty] != 1 {
		t.Fatalf("空目录要报 %s：%+v", CodeEmpty, r.Problems)
	}
}

// catalogRepoSchemaDir finds the catalog repository's schema/ directory: the
// directory named by ACORNFOX_CATALOG_REPO, else a checkout called "catalog"
// next to this repository, else one next to the Go module (for a test
// machine that only has the module directory).
func catalogRepoSchemaDir() (string, bool) {
	var candidates []string
	if dir := os.Getenv("ACORNFOX_CATALOG_REPO"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "schema"))
	}
	candidates = append(candidates,
		filepath.Join("..", "..", "..", "..", "catalog", "schema"),
		filepath.Join("..", "..", "..", "catalog", "schema"))
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c, true
		}
	}
	return "", false
}

func TestEmbeddedSchemasMatchCatalogCopy(t *testing.T) {
	// The catalog repository keeps a copy of schema/ for editors and CI.
	// When it is checked out nearby, the copies must match byte for byte.
	dir, ok := catalogRepoSchemaDir()
	if !ok {
		t.Skip("附近没有目录仓库（可用环境变量 ACORNFOX_CATALOG_REPO 指定），跳过")
	}
	for _, f := range []string{"manifest.schema.json", "recipe.schema.json", "gaps.schema.json"} {
		b2, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("目录仓库 %s 里没有 %s：格式定义改了要同步过去（%v）", dir, f, err)
		}
		b1, _ := schemaFS.ReadFile("schema/" + f)
		if string(b1) != string(b2) {
			t.Fatalf("%s 和目录仓库 %s 里的副本不一致", f, dir)
		}
	}
}

func TestPlatformRefs(t *testing.T) {
	m := &Manifest{
		Env:   []EnvVar{{Key: "TZ", From: "platform.tz"}, {Key: "X", From: "params.x"}, {Key: "P", From: "platform.mirror.pypi"}},
		Files: []File{{Template: "url: {{ platform.app.url }}\nk: {{params.llm.api_key}}\nm: {{platform.mirror.pypi}}"}},
		Init:  []InitStep{{HTTP: InitHTTP{JSON: map[string]any{"a": []any{"{{platform.region}}"}, "b": "{{params.y}}"}}}},
	}
	got := strings.Join(PlatformRefs(m), ",")
	if got != "app.url,mirror.pypi,region,tz" {
		t.Fatalf("refs %s", got)
	}
}

func TestPlatformVarTable(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range PlatformVars {
		if seen[v.Name] {
			t.Fatalf("duplicate %s", v.Name)
		}
		seen[v.Name] = true
		if v.Label == "" || v.Standalone == "" || v.Cloud == "" {
			t.Fatalf("%s: label and both sources are required", v.Name)
		}
		if v.Secret && v.UserCanSet {
			t.Fatalf("%s: a secret the user can change", v.Name)
		}
		if v.Since != BasePlatform {
			// The first batch shipped with the first release. A later variable
			// carries the version that first has it; then relax this line.
			t.Fatalf("%s: since %q, want %s", v.Name, v.Since, BasePlatform)
		}
	}
	// the first batch of platform variables
	for _, n := range []string{PVLLMBaseURL, PVLLMAPIKey, PVLLMModelChat, PVTZ, PVRegion, PVMirrorPyPI, PVMirrorNPM, PVAppURL} {
		if !seen[n] {
			t.Fatalf("missing %s", n)
		}
	}
	if len(PlatformVars) != 8 {
		t.Fatalf("%d variables: update the docs when adding one", len(PlatformVars))
	}
}
