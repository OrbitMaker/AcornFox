package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The test material for how values are written (testdata/values): one app
// that writes all of it.
var valuesDir = filepath.Join("testdata", "values")

const valuesManifest = "apps/demo-values/manifest.yaml"

func copyValues(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(valuesDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(valuesDir, p)
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

// The material passes: no error, one warning (the {{ $json.x }} kept on
// purpose), and it needs no more than the format's first platform.
func TestValuesMaterialLints(t *testing.T) {
	r, err := Lint(valuesDir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Errors() != 0 || len(r.Problems) != 1 {
		t.Fatalf("%+v", r.Problems)
	}
	if p := r.Problems[0]; p.Code != CodeNotRef || p.Level != LevelWarning || p.Path != "/env/LITERAL_BRACES/value" || !strings.Contains(p.Message, "$json.x") {
		t.Fatalf("%+v", p)
	}
	if mp := r.MinPlatform.Apps["demo-values"]; mp.Computed != BasePlatform || len(mp.Reasons) != 0 {
		t.Fatalf("min platform %+v", mp)
	}
}

// The variable report and everything built on the references (min
// platform, used items) sees the new places: env values and ifs, the start
// command, file ifs, first-login values.
func TestValuesRefsInTheReport(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(valuesDir, valuesManifest))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]string{}
	for _, v := range ClassifyVars(m) {
		used[v.Name] = strings.Join(v.UsedIn, ",")
	}
	want := map[string]string{
		"params.realm":          "/container/command,/files/index.html",
		"params.greeting":       "/env/GREETING_B64,/env/GREETING_URL,/env/GREETING_SHA256,/env/GREETING_SHELL,/env/GREETING_SNAKE,/files/settings.json,/files/settings.yaml",
		"params.flag":           "/env/FLAG_ON",
		"params.admin_password": "/env/ADMIN_DSN,/first_login/密码",
		"platform.region":       "/env/REGION_UPPER,/env/REGION_IS_CN,/env/PIP_ONLY_IN_CN,/env/MODE,/files/settings.json,/files/cn-only.txt",
		"platform.app.url":      "/env/APP_URL_SLASH,/first_login/地址",
		"platform.mirror.pypi":  "/env/PIP_ONLY_IN_CN,/files/cn-only.txt",
		"uses.mail.host":        "/env/MAIL_HOST,/env/MAIL_ONLY_WHEN_THERE,/files/mail.txt",
	}
	for name, w := range want {
		if used[name] != w {
			t.Errorf("%s 用在 %q，要 %q", name, used[name], w)
		}
	}
	if !UsedItems(m, "mail")["host"] {
		t.Fatal("uses.mail.host 没算进用到的项")
	}
	// {{params.greeting}} after \{{ is no reference.
	for _, r := range manifestRefs(m) {
		if r.Where == "/env/LITERAL_BRACES" {
			t.Fatalf("LITERAL_BRACES 里没有引用：%+v", r)
		}
	}
	// A value made of references is no fixed value to warn about.
	for _, h := range hardcodedHints(m) {
		if h.Where == "/env/APP_URL_SLASH" {
			t.Fatalf("%+v", h)
		}
	}
}

// What lint says about each way of writing a value wrongly: the code, where,
// and that it is an error.
func TestValuesLintProblems(t *testing.T) {
	cases := []struct {
		name, old, new, code, path string
	}{
		{"if of none of the three forms", `if: "platform.region == cn" }`, `if: "platform.region in cn" }`, CodeIf, "/env/PIP_ONLY_IN_CN/if"},
		{"if with braces", `if: params.flag }`, `if: "{{params.flag}}" }`, CodeIf, "/env/FLAG_ON/if"},
		{"if on a platform variable that does not exist", `if: params.flag }`, `if: "platform.nope == x" }`, CodePlatformVar, "/env/FLAG_ON/if"},
		{"if on a parameter that does not exist", `if: params.flag }`, `if: params.nope }`, CodeReference, "/env/FLAG_ON/if"},
		{"a file's if", `if: uses.mail.host`, `if: uses.mail.host ==`, CodeIf, "/files/mail.txt/if"},
		{"one of two entries without if", `{ key: MODE, value: abroad, if: "platform.region != cn" }`, `{ key: MODE, value: abroad }`, CodeEnvExclusive, "/env/MODE"},
		{"two entries both ==", `{ key: MODE, value: abroad, if: "platform.region != cn" }`, `{ key: MODE, value: abroad, if: "platform.region == cn" }`, CodeEnvExclusive, "/env/MODE"},
		{"two entries on different references", `{ key: MODE, value: abroad, if: "platform.region != cn" }`, `{ key: MODE, value: abroad, if: params.flag }`, CodeEnvExclusive, "/env/MODE"},
		{"the same key twice without if", `{ key: FLAG_ON, value: "on", if: params.flag }`, `{ key: FLAG_ON, value: "on" }` + "\n  - { key: FLAG_ON, value: \"off\" }", CodeEnvExclusive, "/env/FLAG_ON"},
		{"an unknown transform", `{{platform.region | upper}}`, `{{platform.region | bcrypt}}`, CodeTransform, "/env/REGION_UPPER/value"},
		{"map without key=value", `map "cn=yes" "*=no"`, `map "cn" "*=no"`, CodeTransform, "/env/REGION_IS_CN/value"},
		{"a reference without its closing braces", `"{{platform.app.url}}/" }`, `"{{platform.app.url/" }`, CodeTemplate, "/env/APP_URL_SLASH/value"},
		{"a misspelt kind in a value", `"{{platform.app.url}}/" }`, `"{{param.realm}}/" }`, CodeTemplate, "/env/APP_URL_SLASH/value"},
		{"a value refers to a parameter that does not exist", `"{{platform.app.url}}/" }`, `"{{params.nope}}/" }`, CodeReference, "/env/APP_URL_SLASH/value"},
		{"a value refers to a platform variable that does not exist", `"{{platform.app.url}}/" }`, `"{{platform.app.nope}}/" }`, CodePlatformVar, "/env/APP_URL_SLASH/value"},
		{"a volume as a value", `"{{platform.app.url}}/" }`, `"{{volumes.www}}/" }`, CodeReference, "/env/APP_URL_SLASH/value"},
		{"an optional service's item without default", `'{{uses.mail.host | default "none"}}'`, `'{{uses.mail.host}}'`, CodeEnvOptional, "/env/MAIL_HOST"},
		{"an optional service's item in a file without if", "    if: uses.mail.host\n", "", CodeEnvOptional, "/files/mail.txt"},
		{"a secret in the start command", `"{{params.realm | upper}}"]`, `"{{params.admin_password}}"]`, CodeCommandSecret, "/container/command/7"},
		{"a bad template in the start command", `"{{params.realm | upper}}"]`, `'{{params.realm | upper "x"}}']`, CodeTransform, "/container/command/7"},
		{"a first-login value refers to a parameter that does not exist", `"{{platform.app.url}}/index.html" }`, `"{{params.nope}}" }`, CodeReference, "/first_login/地址/value"},
		{"cn is gone from the format", "delivery:\n", "cn:\n  env: [{ key: A, value: b }]\ndelivery:\n", CodeSchema, ""},
		{"if must not be empty", `if: params.flag }`, `if: "" }`, CodeSchema, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyValues(t)
			edit(t, filepath.Join(dir, filepath.FromSlash(valuesManifest)), c.old, c.new)
			r, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, p := range problemsWith(r, c.code) {
				if p.Level != LevelError {
					t.Errorf("%s 的级别是 %s", c.code, p.Level)
				}
				found = found || c.path == "" || p.Path == c.path
			}
			if !found {
				t.Fatalf("没在 %s 报 %s：%+v", c.path, c.code, r.Problems)
			}
		})
	}
}

// What covers an optional service's missing value: optional on the env
// entry, a default transform, an if naming the same service (for a file
// too). An if on another service does not.
func TestValuesOptionalServiceCovered(t *testing.T) {
	ok := []struct{ old, new string }{
		{`'{{uses.mail.host | default "none"}}' }`, `'{{uses.mail.host}}', optional: true }`},
		{`'{{uses.mail.host | default "none"}}' }`, `'{{uses.mail.host}}', if: uses.mail.host }`},
		{`{ key: MAIL_ONLY_WHEN_THERE, from: uses.mail.host, optional: true }`, `{ key: MAIL_ONLY_WHEN_THERE, from: uses.mail.host, if: "uses.mail.port != 0" }`},
	}
	for _, c := range ok {
		dir := copyValues(t)
		edit(t, filepath.Join(dir, filepath.FromSlash(valuesManifest)), c.old, c.new)
		r, err := Lint(dir)
		if err != nil || r.Errors() != 0 {
			t.Fatalf("%s：%v %+v", c.new, err, r.Problems)
		}
	}
	dir := copyValues(t)
	edit(t, filepath.Join(dir, filepath.FromSlash(valuesManifest)), "    if: uses.mail.host\n", "    if: params.flag\n")
	r, err := Lint(dir)
	if err != nil || len(problemsWith(r, CodeEnvOptional)) != 1 {
		t.Fatalf("别的条件挡不住可选服务没接上：%v %+v", err, r.Problems)
	}
}
