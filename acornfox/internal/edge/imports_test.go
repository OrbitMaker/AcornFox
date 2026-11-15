package edge

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/acornfox/acornfox"

// adapterPkg is the adapter inside the entry proxy container, with the
// translation into the proxy's settings below it (adapterPkg/caddyroute).
const adapterPkg = modulePath + "/internal/edgeadapter"

// adapterHome are the only places that may import the adapter: its own
// packages and its command.
var adapterHome = []string{"internal/edgeadapter", "cmd/afx-edge"}

func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	return root
}

func inAdapterHome(rel string) bool {
	for _, h := range adapterHome {
		if rel == h || strings.HasPrefix(rel, h+"/") {
			return true
		}
	}
	return false
}

// TestOnlyTheAdapterImportsTheAdapter keeps the reconciler, the public
// console, the API server and the rest of the program on edge.Proxy: no Go
// file outside the adapter's own packages and its command imports the adapter
// or the translation into the proxy's settings, tests included.
func TestOnlyTheAdapterImportsTheAdapter(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "testdata", ".git":
				return filepath.SkipDir
			}
			if inAdapterHome(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == adapterPkg || strings.HasPrefix(p, adapterPkg+"/") {
				t.Errorf("%s imports %s; the acornfox program talks to the entry proxy through edge.Proxy (package edgeclient) only", rel, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestProgramDoesNotLinkTheAdapter checks the whole dependency graph of the
// acornfox program: neither the adapter nor the translation into the proxy's
// settings is linked into it, directly or through another package.
func TestProgramDoesNotLinkTheAdapter(t *testing.T) {
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		if goBin, err = exec.LookPath("go"); err != nil {
			t.Skip("go tool not found")
		}
	}
	cmd := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", "./cmd/acornfox")
	cmd.Dir = moduleRoot(t)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("go list -deps ./cmd/acornfox: %v\n%s", err, stderr)
	}
	pkgs := strings.Fields(string(out))
	if len(pkgs) < 20 {
		t.Fatalf("go list returned only %d packages: %q", len(pkgs), out)
	}
	sawEdge := false
	for _, p := range pkgs {
		if p == adapterPkg || strings.HasPrefix(p, adapterPkg+"/") || strings.HasSuffix(p, "/caddyroute") {
			t.Errorf("the acornfox program links %s", p)
		}
		if p == modulePath+"/internal/edgeclient" {
			sawEdge = true
		}
	}
	if !sawEdge {
		t.Errorf("the acornfox program should reach the entry proxy through internal/edgeclient; deps: %v", pkgs)
	}
}
