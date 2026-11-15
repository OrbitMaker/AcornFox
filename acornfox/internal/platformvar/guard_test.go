package platformvar

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The dependency check: the install engine, the API behind the console's
// pages, the command line and the server's wiring take platform values only
// through this package's interface. In every non-test Go file of the
// module outside this package and internal/catalog (where the table is
// defined and read by lint):
//
//   - no catalog.PlatformVars (the whole table comes from Table, Provider.Table
//     or Provider.Machine) and no catalog.PV… name constant (a branch on one
//     variable's name is this package's business);
//   - no platformvar.NewStandalone except in the server's wiring
//     (cmd/acornfox) and the engine's fallback for an unset provider
//     (internal/appinstall): the API and the command line cannot make a
//     provider of their own;
//   - no string that is one of the standalone edition's values (a mirror
//     address), and no string that is a variable's name ("tz",
//     "platform.tz"): a branch on a name written out is the same as one on
//     its constant.
//
// The console's own code (web/console/src) holds none of those values
// either: it shows what the API gives.
func TestNoPlatformValuesOutsideProvider(t *testing.T) {
	root := moduleRoot(t)
	values := standaloneValues()
	forbidden := withNames(values)
	var bad []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == "internal/platformvar" || rel == "internal/catalog" ||
				d.Name() == "testdata" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), "."):
				if rel != "." {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		bad = append(bad, checkGoFile(t, path, rel, forbidden)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	bad = append(bad, checkConsole(t, root, values)...)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("platform values worked out outside internal/platformvar:\n  %s", strings.Join(bad, "\n  "))
	}
}

// newStandaloneAllowed is where a standalone provider may be made.
var newStandaloneAllowed = map[string]bool{
	"cmd/acornfox":        true,
	"internal/appinstall": true,
}

func checkGoFile(t *testing.T, path, rel string, values map[string]bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	// The names the file imports the two packages under.
	var catName, pvName string
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		local := ""
		if im.Name != nil {
			local = im.Name.Name
		}
		switch p {
		case "github.com/acornfox/acornfox/internal/catalog":
			catName = or(local, "catalog")
		case "github.com/acornfox/acornfox/internal/platformvar":
			pvName = or(local, "platformvar")
		}
	}
	dir := filepath.ToSlash(filepath.Dir(rel))
	var bad []string
	where := func(n ast.Node) string { return rel + ":" + strconv.Itoa(fset.Position(n.Pos()).Line) }
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			id, ok := x.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch {
			case catName != "" && id.Name == catName && (x.Sel.Name == "PlatformVars" || isPVConst(x.Sel.Name)):
				bad = append(bad, where(x)+": catalog."+x.Sel.Name)
			case pvName != "" && id.Name == pvName && x.Sel.Name == "NewStandalone" && !newStandaloneAllowed[dir]:
				bad = append(bad, where(x)+": platformvar.NewStandalone")
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if s, err := strconv.Unquote(x.Value); err == nil && values[s] {
					bad = append(bad, where(x)+": "+s)
				}
			}
		}
		return true
	})
	return bad
}

// checkConsole looks for the standalone edition's values, quoted, in the
// console's own source (not its tests).
func checkConsole(t *testing.T, root string, values map[string]bool) []string {
	t.Helper()
	src := filepath.Join(root, "web", "console", "src")
	if _, err := os.Stat(src); err != nil {
		return nil // no console source in this checkout
	}
	var bad []string
	_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		if !(strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx")) || strings.Contains(name, ".test.") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for v := range values {
			for _, q := range []string{`"`, `'`, "`"} {
				if strings.Contains(string(b), q+v+q) {
					bad = append(bad, filepath.ToSlash(rel)+": "+v)
				}
			}
		}
		return nil
	})
	return bad
}

// isPVConst reports whether name is one of catalog's PV… name constants.
func isPVConst(name string) bool {
	return len(name) > 2 && strings.HasPrefix(name, "PV") && name[2] >= 'A' && name[2] <= 'Z'
}

// standaloneValues is every value the standalone edition gives that is
// not the user's or the host's: the mirror addresses.
func standaloneValues() map[string]bool {
	out := map[string]bool{officialPyPI: true, officialNPM: true}
	for _, m := range []map[string]string{pypiMirrors, npmMirrors} {
		for _, v := range m {
			out[v] = true
		}
	}
	return out
}

// withNames is values plus every variable's name, bare and with the
// platform. prefix (for the Go files; the console's form still checks the
// user's input by name, which the server checks again).
func withNames(values map[string]bool) map[string]bool {
	out := make(map[string]bool, len(values))
	for v := range values {
		out[v] = true
	}
	for _, pv := range Table() {
		out[pv.Name] = true
		out["platform."+pv.Name] = true
	}
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// The check itself finds what it is to find: a file that branches on a
// variable's name (its constant or the name written out), iterates the
// table, makes its own provider or writes a mirror address down.
func TestGuardCatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.go")
	src := `package x

import (
	cat "github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/platformvar"
)

func f() {
	_ = cat.PlatformVars
	_ = cat.PVTZ
	_ = platformvar.NewStandalone(platformvar.Machine{})
	_ = "` + officialPyPI + `"
	_ = "app.url"
	_ = cat.LookupPlatformVar
}
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := checkGoFile(t, path, "internal/apiserver/x.go", withNames(standaloneValues()))
	if len(bad) != 5 {
		t.Fatalf("found %d: %v", len(bad), bad)
	}
	if got := checkGoFile(t, path, "cmd/acornfox/x.go", withNames(standaloneValues())); len(got) != 4 {
		t.Fatalf("wiring: %v", got)
	}
}
