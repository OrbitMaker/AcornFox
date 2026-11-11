package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var (
	sumA = strings.Repeat("a", 64)
	sumB = strings.Repeat("b", 64)
)

func TestArtifactFor(t *testing.T) {
	legacy := Image{Artifact: "old-file", ArtifactSHA256: strings.ToUpper(sumA), ArtifactSize: 10}
	both := Image{Artifacts: map[string]ArchArtifact{"amd64": {File: "x-amd64", SHA256: sumA, Size: 1}, "arm64": {File: "x-arm64", SHA256: sumB, Size: 2}}}
	amdOnly := Image{Artifacts: map[string]ArchArtifact{"amd64": {File: "x-amd64", SHA256: sumA, Size: 1}}}
	armOnly := Image{Artifacts: map[string]ArchArtifact{"arm64": {File: "x-arm64", SHA256: sumB, Size: 2}}}
	cases := []struct {
		name, arch string
		img        Image
		file       string // "" = none
	}{
		{"per-arch amd64", "amd64", both, "x-amd64"},
		{"per-arch arm64", "arm64", both, "x-arm64"},
		{"arm64, only amd64 written", "arm64", amdOnly, ""},
		{"amd64, only arm64 written", "amd64", armOnly, ""},
		{"older form on amd64", "amd64", legacy, "old-file"},
		{"older form on arm64", "arm64", legacy, ""},
		{"older form without checksum", "amd64", Image{Artifact: "old-file"}, ""},
		{"nothing written", "amd64", Image{}, ""},
		{"unknown arch", "riscv64", both, ""},
		{"both forms: per-arch wins", "arm64", Image{Artifact: "old-file", ArtifactSHA256: sumA, Artifacts: armOnly.Artifacts}, "x-arm64"},
		{"both forms: no older fallback", "amd64", Image{Artifact: "old-file", ArtifactSHA256: sumA, Artifacts: armOnly.Artifacts}, ""},
	}
	for _, c := range cases {
		a, ok := c.img.ArtifactFor(c.arch)
		if ok != (c.file != "") || a.File != c.file {
			t.Errorf("%s: got %+v ok=%v, want %q", c.name, a, ok, c.file)
		}
		if ok && a.SHA256 != strings.ToLower(a.SHA256) {
			t.Errorf("%s: checksum not lower case: %s", c.name, a.SHA256)
		}
	}
	if a, _ := legacy.ArtifactFor("amd64"); a.SHA256 != sumA || a.Size != 10 {
		t.Fatalf("older form: %+v", a)
	}
	if got := strings.Join(both.ArtifactArchs(), ","); got != "amd64,arm64" {
		t.Fatalf("archs %s", got)
	}
	if got := strings.Join(legacy.ArtifactArchs(), ","); got != "amd64" {
		t.Fatalf("older form archs %s", got)
	}
	if got := (Image{Artifact: "old-file"}).ArtifactArchs(); len(got) != 0 {
		t.Fatalf("older form without checksum archs %v", got)
	}
	if !legacy.NamesArtifact() || !legacy.LegacyArtifact() || !both.NamesArtifact() || both.LegacyArtifact() || (Image{}).NamesArtifact() {
		t.Fatal("NamesArtifact / LegacyArtifact")
	}
}

func TestNoArtifactReason(t *testing.T) {
	amdOnly := Image{Artifacts: map[string]ArchArtifact{"amd64": {File: "x-amd64", SHA256: sumA, Size: 1}}}
	cases := []struct {
		img  Image
		arch string
		want string
	}{
		{amdOnly, "amd64", ""},
		{Image{}, "arm64", ""},
		{amdOnly, "arm64", "清单里只有 linux/amd64 的镜像包，没有 linux/arm64 的"},
		{Image{Artifact: "f", ArtifactSHA256: sumA}, "arm64", "清单里的镜像包是旧写法，只算 linux/amd64 的，这台是 linux/arm64"},
		{Image{Artifact: "f"}, "amd64", "清单没有镜像包的校验值"},
		{Image{Artifact: "f"}, "arm64", "清单没有镜像包的校验值"},
	}
	for _, c := range cases {
		if got := c.img.NoArtifactReason(c.arch); got != c.want {
			t.Errorf("%+v on %s: %q, want %q", c.img, c.arch, got, c.want)
		}
	}
}

// hermesArchives rewrites the good catalog's Hermes image to the
// per-architecture form with the given lines under "artifacts:" and, unless
// keepMin, min_platform 0.5.1. It returns the catalog dir and the file.
func hermesArchives(t *testing.T, lines string, keepMin bool) (string, string) {
	t.Helper()
	dir := copyTree(t)
	file := filepath.Join(dir, "apps", "hermes-agent", manifestName)
	edit(t, file, "      artifact: hermes-agent-v0.23.0-image\n      artifact_sha256: d356bba314af272039743d82123bf57a38e67696d69558a6871c4b999700df45\n      artifact_size: 1120227840\n",
		"      artifacts:\n"+lines)
	if !keepMin {
		edit(t, file, `min_platform: "0.5.0"`, `min_platform: "0.5.1"`)
	}
	return dir, file
}

const (
	amdLine = "        amd64: { file: hermes-agent-v0.23.0-image, sha256: d356bba314af272039743d82123bf57a38e67696d69558a6871c4b999700df45, size: 1120227840 }\n"
	armLine = "        arm64: { file: hermes-agent-v0.23.0-image-arm64, sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef, size: 1000000000 }\n"
)

func lintCodes(t *testing.T, dir string) (Report, map[string]int) {
	t.Helper()
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r, codes(r)
}

// The per-architecture form lints clean with both architectures and
// min_platform 0.5.1, and builds.
func TestLintPerArchClean(t *testing.T) {
	dir, _ := hermesArchives(t, amdLine+armLine, false)
	r, _ := lintCodes(t, dir)
	if len(r.Problems) != 0 {
		t.Fatalf("problems %+v", r.Problems)
	}
	if info := r.MinPlatform.Apps["hermes-agent"]; info.Computed != "0.5.1" || len(info.Reasons) == 0 ||
		!strings.HasPrefix(info.Reasons[0].Feature, "manifest:/release/images/*/artifacts") {
		t.Fatalf("min platform %+v", info)
	}
	b, err := Build(dir, 7)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// The first feature newer than the base release: recipes using Hermes
	// move to recipes_v2 with 0.5.1, so a 0.5.0 client does not list them;
	// the New API recipe stays where it was.
	idx, err := ParseIndex(b.Index)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Recipes) != 1 || idx.Recipes[0].ID != "new-api" || idx.Recipes[0].MinPlatform != "" {
		t.Fatalf("recipes = %+v", idx.Recipes)
	}
	if len(idx.RecipesV2) != 2 || idx.RecipesV2[0].ID != "hermes-agent" || idx.RecipesV2[0].MinPlatform != "0.5.1" ||
		idx.RecipesV2[1].ID != "hermes-with-new-api" || idx.RecipesV2[1].MinPlatform != "0.5.1" {
		t.Fatalf("recipes_v2 = %+v", idx.RecipesV2)
	}
	for _, a := range idx.Apps {
		if want := map[string]string{"hermes-agent": "0.5.1", "new-api": "0.5.0"}[a.ID]; a.MinPlatform != want {
			t.Fatalf("app %s min_platform %q, want %q", a.ID, a.MinPlatform, want)
		}
	}
}

// Writing the new form with min_platform 0.5.0 is CAT-LINT-17 (a 0.5.0
// platform would install it ignoring the archives), and the catalog cannot
// be built.
func TestLintPerArchNeedsMinPlatform(t *testing.T) {
	dir, _ := hermesArchives(t, amdLine+armLine, true)
	r, c := lintCodes(t, dir)
	if c[CodeMinPlatform] != 1 {
		t.Fatalf("want %s: %+v", CodeMinPlatform, r.Problems)
	}
	for _, p := range r.Problems {
		if p.Code == CodeMinPlatform && (!strings.Contains(p.Message, "0.5.1") || !strings.Contains(p.Message, "artifacts")) {
			t.Fatalf("message %q", p.Message)
		}
	}
	if _, err := Build(dir, 7); CodeOf(err) != CodeBuildLint {
		t.Fatalf("build: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "apps", "hermes-agent", manifestName))
	if mp, err := ManifestMinPlatform(raw); err != nil || mp.Version != "0.5.1" {
		t.Fatalf("%+v %v", mp, err)
	}
}

func TestLintPerArchProblems(t *testing.T) {
	cases := []struct {
		name  string
		lines string
		edit  func(t *testing.T, file string)
		code  string
		level string
		in    string // part of the message
	}{
		{name: "both forms", lines: amdLine, code: CodeArchArtifact, level: LevelError, in: "只能用一种",
			edit: func(t *testing.T, f string) {
				edit(t, f, "      artifacts:\n", "      artifact: hermes-agent-v0.23.0-image\n      artifacts:\n")
			}},
		{name: "arch not in platforms", lines: amdLine + armLine, code: CodeArchArtifact, level: LevelError, in: "resources.platforms 里没有 linux/arm64",
			edit: func(t *testing.T, f string) {
				edit(t, f, "platforms: [linux/amd64, linux/arm64]", "platforms: [linux/amd64]")
			}},
		{name: "same file for two archs", lines: amdLine + strings.Replace(armLine, "hermes-agent-v0.23.0-image-arm64", "hermes-agent-v0.23.0-image", 1),
			code: CodeArchArtifact, level: LevelError, in: "同一个网关文件"},
		{name: "same checksum for two archs", lines: amdLine + strings.Replace(armLine, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "d356bba314af272039743d82123bf57a38e67696d69558a6871c4b999700df45", 1),
			code: CodeArchArtifact, level: LevelError, in: "校验值相同"},
		{name: "a platform without archive", lines: amdLine, code: CodeArchMissing, level: LevelWarning, in: "linux/arm64"},
		{name: "unknown arch name", lines: strings.Replace(armLine, "arm64:", "aarch64:", 1), code: CodeSchema, level: LevelError},
		{name: "archive without checksum", lines: "        amd64: { file: f, size: 1 }\n", code: CodeSchema, level: LevelError},
		{name: "empty", lines: "", code: CodeSchema, level: LevelError,
			edit: func(t *testing.T, f string) { edit(t, f, "      artifacts:\n", "      artifacts: {}\n") }},
		{name: "arch written twice", lines: amdLine + amdLine, code: CodeRead, level: LevelError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, file := hermesArchives(t, c.lines, false)
			if c.edit != nil {
				c.edit(t, file)
			}
			r, _ := lintCodes(t, dir)
			var hit *Problem
			for i := range r.Problems {
				if r.Problems[i].Code == c.code {
					hit = &r.Problems[i]
				}
			}
			if hit == nil || hit.Level != c.level || !strings.Contains(hit.Message, c.in) || hit.File != "apps/hermes-agent/manifest.yaml" {
				t.Fatalf("want %s (%s, %q): %+v", c.code, c.level, c.in, r.Problems)
			}
			if c.level == LevelWarning && r.Errors() != 0 {
				t.Fatalf("a warning only, got errors: %+v", r.Problems)
			}
		})
	}
}

// The schema at install time (ValidateManifest) takes the new form and
// still refuses an architecture it does not know.
func TestValidateManifestPerArch(t *testing.T) {
	_, file := hermesArchives(t, amdLine+armLine, false)
	raw, _ := os.ReadFile(file)
	if ps := ValidateManifest(raw); len(ps) != 0 {
		t.Fatalf("%+v", ps)
	}
	bad := strings.Replace(string(raw), "arm64: { file", "riscv64: { file", 1)
	if ps := ValidateManifest([]byte(bad)); len(ps) == 0 {
		t.Fatal("an unknown architecture must not pass")
	}
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := m.Release.Images[0].ArtifactFor("arm64"); !ok || a.File != "hermes-agent-v0.23.0-image-arm64" || a.Size != 1000000000 {
		t.Fatalf("decoded %+v %v", a, ok)
	}
}
