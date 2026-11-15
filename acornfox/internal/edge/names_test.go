package edge

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// proxyNames are the words of the proxy software behind the adapter. The
// acornfox program, the install scripts and the console never name it: the
// program talks to an entry proxy (package edge), and which software runs in
// the entry container is the adapter's business (internal/edgeadapter,
// cmd/afx-edge, the release build). Matched case-insensitively; "caddy"
// also covers "Caddyfile".
var proxyNames = regexp.MustCompile(`(?i)caddy`)

// nameExceptions would list, per file (slash path from the repository root),
// the lines allowed to keep a proxy name, each with why. The target is none;
// TestNameExceptionsDoNotWiden keeps it that way.
var nameExceptions = map[string]string{}

// TestNameExceptionsDoNotWiden: an exception must be argued for in a review,
// not slipped in. The list is empty; adding to it needs this test changed too.
func TestNameExceptionsDoNotWiden(t *testing.T) {
	if len(nameExceptions) != 0 {
		t.Fatalf("nameExceptions must stay empty (found %d): rename the text instead, or change this test in the same review with the reason", len(nameExceptions))
	}
}

func goTool(t *testing.T) string {
	t.Helper()
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := os.Stat(goBin); err != nil {
		if goBin, err = exec.LookPath("go"); err != nil {
			t.Skip("go tool not found")
		}
	}
	return goBin
}

// programGOOS are the systems the acornfox program is built for: files with
// build constraints (the macOS and Windows desktop service) only show up in
// go list for their own GOOS.
var programGOOS = []string{"linux", "darwin", "windows"}

// programGoFiles lists the non-test Go files of every package of this
// module the acornfox program depends on (go list -deps ./cmd/acornfox),
// for each of programGOOS.
func programGoFiles(t *testing.T, root string) []string {
	t.Helper()
	seen := map[string]bool{}
	var files []string
	for _, goos := range programGOOS {
		cmd := exec.Command(goTool(t), "list", "-deps", "-f", `{{if .Module}}{{if eq .Module.Path "`+modulePath+`"}}{{$d := .Dir}}{{range .GoFiles}}{{$d}}/{{.}}
{{end}}{{end}}{{end}}`, "./cmd/acornfox")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOOS="+goos)
		out, err := cmd.Output()
		if err != nil {
			var stderr string
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = string(ee.Stderr)
			}
			t.Fatalf("GOOS=%s go list -deps ./cmd/acornfox: %v\n%s", goos, err, stderr)
		}
		n := 0
		for _, l := range strings.Split(string(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				n++
				if !seen[l] {
					seen[l] = true
					files = append(files, l)
				}
			}
		}
		if n < 50 {
			t.Fatalf("GOOS=%s go list returned only %d files", goos, n)
		}
	}
	return files
}

// scanNames reports every line of path naming the proxy software.
func scanNames(t *testing.T, repo, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(repo, path)
	rel = filepath.ToSlash(rel)
	if _, ok := nameExceptions[rel]; ok {
		return
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for n := 1; sc.Scan(); n++ {
		if line := sc.Text(); proxyNames.MatchString(line) {
			t.Errorf("%s:%d names the proxy software; say \"entry proxy\" (入口代理) instead: %s", rel, n, strings.TrimSpace(line))
		}
	}
}

// TestProgramDoesNotNameTheProxy: no Go file the acornfox program is built
// from, no install script (scripts/install/*.sh) and no console source
// (web/console/src) names the proxy software behind the entry proxy. The
// scripts are outside the Go module: that part skips, with a line saying so,
// when only the module is present.
func TestProgramDoesNotNameTheProxy(t *testing.T) {
	root := moduleRoot(t)
	repo := filepath.Dir(root)
	files := programGoFiles(t, root)
	for _, f := range files {
		scanNames(t, repo, f)
	}

	src := filepath.Join(root, "web", "console", "src")
	var consoleFiles []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		consoleFiles = append(consoleFiles, path)
		return nil
	})
	if err != nil {
		t.Fatalf("console sources: %v", err)
	}
	if len(consoleFiles) < 10 {
		t.Fatalf("found only %d console source files under %s", len(consoleFiles), src)
	}
	for _, f := range consoleFiles {
		scanNames(t, repo, f)
	}

	scripts, _ := filepath.Glob(filepath.Join(repo, "scripts", "install", "*.sh"))
	if len(scripts) == 0 {
		t.Log("scripts/install/*.sh not found (only the Go module is present): the install scripts were not scanned; run in a full checkout before merging")
		return
	}
	sort.Strings(scripts)
	for _, f := range scripts {
		scanNames(t, repo, f)
	}
}
