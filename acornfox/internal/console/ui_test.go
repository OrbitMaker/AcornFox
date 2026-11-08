package console

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The new console is built from web/console (React + Vite) into ui/, and the
// build output is committed. These tests check that the embedded output is
// complete, loads nothing from other origins, needs no inline script or style
// (the console CSP is script-src 'self'; style-src 'self'), and was built
// from the sources that are in the repository now.

func readUI(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(FS, name)
	if err != nil {
		t.Fatalf("read %s from ui: %v", name, err)
	}
	return string(b)
}

var assetRef = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*["']([^"']+)["']`)

func TestUIIndexAndAssets(t *testing.T) {
	html := readUI(t, "index.html")
	refs := assetRef.FindAllStringSubmatch(html, -1)
	if len(refs) < 3 {
		t.Fatalf("index.html references too few files: %v", refs)
	}
	var js, css int
	for _, m := range refs {
		ref := m[1]
		if strings.Contains(ref, "//") || strings.HasPrefix(ref, "data:") {
			t.Errorf("index.html references a resource outside the console: %q", ref)
			continue
		}
		name := strings.TrimPrefix(ref, "/")
		if _, err := fs.Stat(FS, name); err != nil {
			t.Errorf("index.html references %q, not embedded: %v", ref, err)
		}
		switch {
		case strings.HasSuffix(name, ".js"):
			js++
		case strings.HasSuffix(name, ".css"):
			css++
		}
	}
	if js < 2 || css < 1 {
		t.Errorf("index.html should load theme-init.js, the app bundle and a stylesheet: %d js, %d css", js, css)
	}
	for _, name := range []string{"favicon.svg", "theme-init.js", "build-info.json"} {
		if _, err := fs.Stat(FS, name); err != nil {
			t.Errorf("missing %s in ui: %v", name, err)
		}
	}
}

func TestUINoInlineScriptOrStyle(t *testing.T) {
	html := readUI(t, "index.html")
	scriptTag := regexp.MustCompile(`(?is)<script([^>]*)>(.*?)</script>`)
	for _, m := range scriptTag.FindAllStringSubmatch(html, -1) {
		if strings.TrimSpace(m[2]) != "" || !strings.Contains(strings.ToLower(m[1]), "src=") {
			t.Errorf("index.html has an inline script: %q", m[0])
		}
	}
	if regexp.MustCompile(`(?is)<style[\s>]`).MatchString(html) {
		t.Error("index.html must not contain a <style> block")
	}
	if regexp.MustCompile(`(?i)\sstyle\s*=`).MatchString(html) {
		t.Error("index.html must not use inline style attributes")
	}
}

// Everything the browser loads must work under script-src 'self' (no eval or
// new Function) and font-src/default-src 'self' (no fonts from other hosts).
func TestUIAssetsSelfContained(t *testing.T) {
	err := fs.WalkDir(FS, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(p, ".js") && !strings.HasSuffix(p, ".css") {
			return nil
		}
		b, rerr := fs.ReadFile(FS, p)
		if rerr != nil {
			return rerr
		}
		src := string(b)
		if strings.HasSuffix(p, ".js") && regexp.MustCompile(`\bnew Function\s*\(|\beval\s*\(`).MatchString(src) {
			t.Errorf("%s uses eval or new Function (blocked by the console CSP)", p)
		}
		if strings.HasSuffix(p, ".css") {
			for _, m := range regexp.MustCompile(`url\(\s*['"]?([^'")]+)`).FindAllStringSubmatch(src, -1) {
				if strings.Contains(m[1], "//") || strings.HasPrefix(m[1], "data:font") {
					t.Errorf("%s loads %q from outside the console", p, m[1])
				}
			}
			if strings.Contains(src, "fonts.googleapis") || strings.Contains(src, "@import url(http") {
				t.Errorf("%s imports a remote stylesheet", p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// buildInputs mirrors web/console/scripts/source-hash.mjs: the files the
// build depends on, sorted, each as "path\nsha256\n", hashed again.
type buildInputs struct {
	Files   []string `json:"files"`
	Dirs    []string `json:"dirs"`
	Exclude []string `json:"exclude"`
}

func sourceHash(root string) (string, []string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "build-inputs.json"))
	if err != nil {
		return "", nil, err
	}
	var spec buildInputs
	if err := json.Unmarshal(raw, &spec); err != nil {
		return "", nil, err
	}
	set := map[string]bool{}
	for _, f := range spec.Files {
		if st, err := os.Stat(filepath.Join(root, f)); err == nil && !st.IsDir() {
			set[f] = true
		}
	}
	for _, d := range spec.Dirs {
		base := filepath.Join(root, d)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			set[filepath.ToSlash(rel)] = true
			return nil
		})
		if err != nil {
			return "", nil, err
		}
	}
	var ex []*regexp.Regexp
	for _, p := range spec.Exclude {
		ex = append(ex, regexp.MustCompile(p))
	}
	var files []string
	for f := range set {
		skip := false
		for _, re := range ex {
			if re.MatchString(f) {
				skip = true
			}
		}
		if !skip {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return "", nil, err
		}
		sum := sha256.Sum256(b)
		h.Write([]byte(f + "\n" + hex.EncodeToString(sum[:]) + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), files, nil
}

// TestUIBuiltFromCurrentSources fails when web/console changed but ui/ was
// not rebuilt (see web/console/README.md for how to rebuild).
func TestUIBuiltFromCurrentSources(t *testing.T) {
	root := filepath.Join("..", "..", "web", "console")
	if _, err := os.Stat(filepath.Join(root, "build-inputs.json")); err != nil {
		t.Skip("web/console sources not present: " + err.Error())
	}
	want, files, err := sourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 10 {
		t.Fatalf("source hash covers too few files: %v", files)
	}
	var info struct {
		SourceSHA256 string `json:"source_sha256"`
	}
	if err := json.Unmarshal([]byte(readUI(t, "build-info.json")), &info); err != nil {
		t.Fatal(err)
	}
	if info.SourceSHA256 != want {
		t.Fatalf("ui/ was built from other sources (build-info %s, web/console now %s): rebuild it on the dev machine, see web/console/README.md", info.SourceSHA256, want)
	}
}
