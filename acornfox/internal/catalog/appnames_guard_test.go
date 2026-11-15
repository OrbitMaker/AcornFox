package catalog

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode"

	"gopkg.in/yaml.v3"
)

// The platform knows no app: no app's name may be written in the program
// (its Go code, tests and test material aside) or in the format definitions.
// What an app does is in its manifest; the platform only runs what manifests
// say. Help texts and examples use neutral names (demo-app).
//
// The names checked are those of the apps of the published catalog, of the
// catalog repository when it is at hand (ACORNFOX_CATALOG_REPO, or a
// checkout next to this one), and of every app in the test material (ids
// and display names), whatever is added there later.
//
// A name is matched by its words, however they are written: "new-api" is
// also found as "New API", "new_api", "NewAPI", "newApiClient" and
// "newapi", but not inside another word ("renewApi").

// publishedApps are the apps of the published catalog (outside this module),
// with their display names.
var publishedApps = []string{"new-api", "hermes-agent", "New API", "Hermes"}

// appNamesUnder reads the ids and display names of every app manifest under
// root (only those inside a testdata directory when testdataOnly).
func appNamesUnder(t *testing.T, root string, testdataOnly bool) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "manifest.yaml" || testdataOnly && !strings.Contains(filepath.ToSlash(path), "/testdata/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var m struct {
			ID  string `yaml:"id"`
			App struct {
				Name string `yaml:"name"`
			} `yaml:"app"`
		}
		if yaml.Unmarshal(raw, &m) != nil {
			return nil // a deliberately broken manifest
		}
		out = append(out, m.ID, m.App.Name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// catalogRepoApps is the apps directory of the catalog repository at hand,
// if any.
func catalogRepoApps() (string, bool) {
	dir, ok := catalogRepoSchemaDir()
	if !ok {
		return "", false
	}
	apps := filepath.Join(filepath.Dir(dir), "apps")
	st, err := os.Stat(apps)
	return apps, err == nil && st.IsDir()
}

// nameWords splits text into lower-case words: runs of letters and digits,
// also split where camel case starts a new word ("newApiClient", "NewAPI"
// and "APIClient" are three, two and two words).
func nameWords(text string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	rs := []rune(text)
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(cur) > 0 && unicode.IsUpper(r) {
			prev := cur[len(cur)-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || unicode.IsUpper(prev) && nextLower {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

// appName is one name to look for: its words, and the words run together
// ("newapi"), which a line may write as one word.
type appName struct {
	text   string
	words  []string
	joined string
}

func appNames(raw []string) []appName {
	seen := map[string]bool{}
	var out []appName
	for _, n := range raw {
		words := nameWords(n)
		joined := strings.Join(words, "")
		if len([]rune(joined)) < 3 || seen[joined] {
			continue
		}
		seen[joined] = true
		out = append(out, appName{text: strings.TrimSpace(n), words: words, joined: joined})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].joined < out[j].joined })
	return out
}

// inLine reports whether line names n. The words of a name are only taken
// together when nothing but blanks, "-" and "_" (or a change of case) is
// between them: "New(apiCfg)" is a call and an argument, not a name.
func (n appName) inLine(line string) bool {
	for _, part := range strings.FieldsFunc(line, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r) && r != '-' && r != '_'
	}) {
		if n.mentions(nameWords(part)) {
			return true
		}
	}
	return false
}

// mentions reports whether the words contain the name.
func (n appName) mentions(line []string) bool {
	for i := range line {
		if line[i] == n.joined {
			return true
		}
		if i+len(n.words) <= len(line) {
			match := true
			for j, w := range n.words {
				if line[i+j] != w {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

func TestNameWords(t *testing.T) {
	n := appNames([]string{"new-api", "New API", "Hermes 智能体"})
	if len(n) != 2 {
		t.Fatalf("names %+v", n)
	}
	newAPI := n[1]
	for _, line := range []string{`id == "NewAPI"`, "new_api", "newapi", "a newApiClient", "NEW-API", "New API 的", "// new api"} {
		if !newAPI.inLine(line) {
			t.Errorf("%q not found in %q", newAPI.text, line)
		}
	}
	for _, line := range []string{"renewApi", "news api", "newer-api", "api new", "apiserver.New(apiCfg)"} {
		if newAPI.inLine(line) {
			t.Errorf("%q found in %q", newAPI.text, line)
		}
	}
	if !n[0].inLine("Hermes智能体") || !n[0].inLine("装 hermes 智能体") {
		t.Errorf("%q not found", n[0].text)
	}
}

// The one exception, by file: the
// built-in "add a database to a user's project" (acornfox add
// postgres|mysql|redis and the console's data tab) predates the rule and
// writes the database kinds it knows into the program. It is kept as it is
// for now; a later work item moves it onto the plug-in apps of the catalog
// (the existing add-on containers and volumes taken over where they are,
// nothing moved), and then this table goes. Until then, in these files and
// nowhere else, the database kinds in builtinAddonWords are let through;
// every other app name is caught there as anywhere. A file that no longer
// writes any of them must leave the table (TestBuiltinAddonException).
var builtinAddonFiles = []string{
	"cmd/acornfox/desktop.go",
	"internal/apiserver/addons.go",
	"internal/apiserver/deployment_view.go",
	"internal/apiserver/resources.go",
	"internal/cli/addon.go",
	"internal/cli/cli.go",
	"internal/deploylog/redact.go",
	"internal/reconcile/addons.go",
	"internal/reconcile/deploylogs.go",
	"internal/runner/addon.go",
	"internal/state/addons.go",
	"internal/state/types.go",
}

// builtinAddonWords are the names let through in builtinAddonFiles, written
// as appName.joined (the words of a name run together, lower case): the
// database kinds the built-in add-ons know, as ids and as display names.
var builtinAddonWords = map[string]bool{"postgres": true, "postgresql": true, "mysql": true, "redis": true}

// exempt reports whether name n may be written in the program file rel.
func exempt(rel string, n appName) bool {
	if !builtinAddonWords[n.joined] {
		return false
	}
	for _, f := range builtinAddonFiles {
		if f == rel {
			return true
		}
	}
	return false
}

// programRoot is the module root (where cmd/ and internal/ are).
func programRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// programFiles lists the files to scan, relative to root: the program's Go
// code (not tests, not test material, not the built-in catalog snapshot,
// which is data) and the format definitions.
func programFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "node_modules", ".git", "web":
				return filepath.SkipDir
			}
			if rel == "internal/catalog/snapshot" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") ||
			strings.HasPrefix(rel, "internal/catalog/schema/") && strings.HasSuffix(rel, ".json") {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 50 {
		t.Fatalf("only %d files found under %s: wrong root?", len(files), root)
	}
	return files
}

// nameHit is one line of a program file that writes an app name.
type nameHit struct {
	file string
	line int
	name appName
}

// programHits returns every line of files (under root) that writes one of
// names, the exception left in.
func programHits(t *testing.T, root string, files []string, names []appName) []nameHit {
	t.Helper()
	var out []nameHit
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for _, n := range names {
				if n.inLine(line) {
					out = append(out, nameHit{rel, i + 1, n})
				}
			}
		}
	}
	return out
}

func TestProgramNamesNoApp(t *testing.T) {
	root := programRoot(t)
	raw := append(append([]string(nil), publishedApps...), appNamesUnder(t, root, true)...)
	if dir, ok := catalogRepoApps(); ok {
		repo := appNamesUnder(t, dir, false)
		if len(repo) == 0 {
			t.Fatalf("no app manifest under %s", dir)
		}
		raw = append(raw, repo...)
	}
	names := appNames(raw)
	if len(names) < 4 {
		t.Fatalf("too few app names to look for: %+v", names)
	}
	files := programFiles(t, root)
	// The format definitions and the feature table are among them: what a
	// manifest may write names no app either.
	scanned := strings.Join(files, "\n") + "\n"
	for _, must := range []string{"internal/catalog/features.go", "internal/catalog/platformvars.go", "internal/catalog/schema/manifest.schema.json",
		"internal/catalog/schema/recipe.schema.json", "internal/catalog/schema/service-type.schema.json"} {
		if !strings.Contains(scanned, must+"\n") {
			t.Fatalf("%s is not scanned", must)
		}
	}
	for _, h := range programHits(t, root, files, names) {
		if exempt(h.file, h.name) {
			continue
		}
		t.Errorf("%s:%d 写了应用名 %q：平台不认识任何具体的应用，改成通用的说法或中性的例子（demo-app）", h.file, h.line, h.name.text)
	}
}

// The exception is as narrow as it says. Taken as app names (as they will
// be once the catalog lists such apps, by id, display name or major
// version), the database kinds are written in the files of the table and in
// no other file of the program; there, nothing but the registered words
// passes, and every file of the table still writes one of them.
func TestBuiltinAddonException(t *testing.T) {
	root := programRoot(t)
	files := programFiles(t, root)
	names := appNames([]string{"postgres", "PostgreSQL", "Postgres", "postgresql", "mysql", "MySQL", "redis", "Redis",
		"postgres-16", "PostgreSQL 17", "mysql-8", "MySQL 8.4", "redis-7", "Redis 7"})
	for w := range builtinAddonWords {
		found := false
		for _, n := range names {
			found = found || n.joined == w
		}
		if !found {
			t.Errorf("例外表里的词 %q 不是这几个数据库的名字", w)
		}
	}
	inTable := map[string]bool{}
	for _, f := range builtinAddonFiles {
		inTable[f] = true
		if !strings.Contains(strings.Join(files, "\n")+"\n", f+"\n") {
			t.Errorf("例外表里的 %s 不在被扫描的文件里：文件改名或删了就从表里去掉", f)
		}
	}
	hit := map[string]bool{}
	for _, h := range programHits(t, root, files, names) {
		switch {
		case !inTable[h.file]:
			t.Errorf("%s:%d 写了 %q：只有现有内置数据库的那几个文件可以写数据库的名字，新代码要走通用规则", h.file, h.line, h.name.text)
		case !exempt(h.file, h.name):
			t.Errorf("%s:%d 写了 %q：例外只放过表里登记的词", h.file, h.line, h.name.text)
		default:
			hit[h.file] = true
		}
	}
	for _, f := range builtinAddonFiles {
		if !hit[f] {
			t.Errorf("%s 已经不写这几个数据库的名字了：从例外表 builtinAddonFiles 里去掉", f)
		}
	}
	// Any other app name is caught in those files as anywhere.
	other := appNames([]string{"new-api", "New API"})[0]
	pg := appNames([]string{"postgres"})[0]
	if exempt("internal/runner/addon.go", other) || !exempt("internal/runner/addon.go", pg) || exempt("internal/runner/exec.go", pg) {
		t.Fatal("exempt 放过了不该放的")
	}
}
