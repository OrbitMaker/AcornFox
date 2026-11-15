package appinstall

// Config files written without a transform come out byte for byte as they
// did before the template engine (catalog/template.go) replaced the
// engine's own {{…}} replacement: the same values in the same places with
// the same escaping (yaml, json: quotes and backslashes). A changed escape
// in a config file locks users out of their apps, so this is checked on
// every config file template of the test material and of the catalog
// repository (when it is at hand), and on templates made to be awkward.
//
// oldRender is the replacement as it was, kept here only to compare with.
// It took every {{…}} without braces or blanks inside for a reference and
// failed on one it could not resolve; templates it failed on (and those
// with \{{, which now writes a literal {{) have nothing to compare.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
)

var oldPlaceholderRe = regexp.MustCompile(`\{\{\s*([^{}\s]+)\s*\}\}`)

func oldRender(tmpl string, lookup func(string) (string, error), escape func(string) string) (string, error) {
	var firstErr error
	out := oldPlaceholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		s, err := lookup(oldPlaceholderRe.FindStringSubmatch(m)[1])
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return m
		}
		if escape != nil {
			s = escape(s)
		}
		return s
	})
	return out, firstErr
}

// awkwardValue is the value every reference gets: quotes, backslashes,
// braces, a pipe, a dollar, non-ASCII, and the name of the reference so
// that two references are told apart.
func awkwardValue(ref string) (string, error) {
	kind, _, _ := strings.Cut(ref, ".")
	if !catalog.RefKinds[kind] {
		return "", fmt.Errorf("没有 %s", ref)
	}
	return `v"` + ref + `"\ {{x}} | $HOME 中文 \\"`, nil
}

// withTransform reports whether a template writes a transform: it is not
// one of those that must render as before.
func withTransform(tmpl string) bool {
	for _, e := range catalog.ScanTemplate(tmpl).Exprs() {
		if len(e.Transforms) > 0 {
			return true
		}
	}
	return false
}

// configTemplates are the files[] of every manifest under dirs, by
// "<manifest>:<path>".
func configTemplates(t *testing.T, dirs ...string) map[string]catalog.File {
	t.Helper()
	out := map[string]catalog.File{}
	for _, dir := range dirs {
		paths, _ := filepath.Glob(filepath.Join(dir, "apps", "*", "manifest.yaml"))
		for _, p := range paths {
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			m, err := catalog.ParseManifest(raw)
			if err != nil {
				continue // another format's: no config files to compare
			}
			for _, f := range m.Files {
				out[p+":"+f.Path] = f
			}
		}
	}
	return out
}

func TestConfigFilesRenderAsBefore(t *testing.T) {
	material, _ := filepath.Glob(filepath.Join("..", "catalog", "testdata", "*"))
	if repo := os.Getenv("ACORNFOX_CATALOG_REPO"); repo != "" {
		material = append(material, repo)
	}
	files := configTemplates(t, material...)
	made := map[string]string{
		"a.yaml":  "k: \"{{params.a}}\"\nu: \"{{ uses.llm.base_url }}\"\nt: '{{platform.tz}}'\nboth: \"{{params.a}}{{params.b}}\"\n",
		"a.yml":   "list:\n  - \"{{params.a}}\"\n  - {{params.b}}\n# {{params.c}} in a comment\n",
		"a.json":  `{"a": "{{params.a}}", "n": {{params.n}}, "nested": {"u": "{{uses.db.url}}"}, "arr": ["{{platform.region}}"]}`,
		"b.JSON":  `{"x":"{{params.x}}"}`,
		"a.toml":  "a = \"{{params.a}}\"\n",
		"a.conf":  "url = {{uses.demo.url}}\n{one} { {two} } {{{params.a}}} {{params.a}}}}\n",
		"a.env":   "A={{params.a}}\nB={{ params.b }}\n",
		"empty":   "",
		"no-refs": "just text with { braces } and }} and {{\n",
	}
	for name, tmpl := range made {
		files["made:"+name] = catalog.File{Path: name, Template: tmpl}
	}
	compared := 0
	for key, f := range files {
		escape := fileEscape(f.Path)
		want, oerr := oldRender(f.Template, awkwardValue, escape)
		if oerr != nil || strings.Contains(f.Template, `\{{`) || withTransform(f.Template) {
			continue
		}
		got, err := catalog.RenderTemplate(f.Template, awkwardValue, escape)
		if err != nil {
			t.Errorf("%s：%v", key, err)
			continue
		}
		if got != want {
			t.Errorf("%s 渲染出来和以前不一样\n以前：%q\n现在：%q", key, want, got)
		}
		compared++
	}
	if compared < len(made)+3 {
		t.Fatalf("只比了 %d 个模板，测试材料里的配置文件没读到？", compared)
	}
	// The engine's own render is that very function.
	v := Values{"a": `pa"ss\word`}
	got, err := v.render(made["a.json"][:20], fileEscape("a.json"))
	if want, _ := catalog.RenderTemplate(made["a.json"][:20], v.lookup, fileEscape("a.json")); err != nil || got != want {
		t.Fatalf("%q %q %v", got, want, err)
	}
}
