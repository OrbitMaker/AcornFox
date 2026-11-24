package appinstall

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
)

func TestGenerateSecret(t *testing.T) {
	a, err := generateSecret(24, "")
	if err != nil || len(a) != 24 || strings.Trim(a, charsetAlnum) != "" {
		t.Fatalf("alnum %q %v", a, err)
	}
	b, _ := generateSecret(24, "")
	if a == b {
		t.Fatal("two secrets equal")
	}
	h, _ := generateSecret(64, "hex")
	if len(h) != 64 || strings.Trim(h, charsetHex) != "" {
		t.Fatalf("hex %q", h)
	}
	if _, err := generateSecret(0, ""); err == nil {
		t.Fatal("zero length accepted")
	}
}

func manifest() *catalog.Manifest {
	return &catalog.Manifest{ID: "x", Params: []catalog.Param{
		{Key: "user", Type: "string", Default: "admin", Pattern: "^[a-z]+$"},
		{Key: "pass", Type: "secret", Generate: &catalog.Generate{Length: 20}},
		{Key: "hidden", Type: "secret", Hidden: true, Generate: &catalog.Generate{Length: 32, Charset: "hex"}},
		{Key: "port", Type: "int"},
		{Key: "need", Type: "string", Required: true, Default: "d"},
	}}
}

func TestResolveParams(t *testing.T) {
	v, err := resolveParams(manifest(), map[string]any{"pass": "my-own-pass", "port": 8080.0})
	if err != nil {
		t.Fatal(err)
	}
	if v["user"] != "admin" || v["pass"] != "my-own-pass" || len(v["hidden"].(string)) != 32 || v["port"] != "8080" || v["need"] != "d" {
		t.Fatalf("values %+v", v)
	}
	// Every value is text: an object (what a setting of the former built-in
	// model kind took) is refused like any other value that is not.
	for _, bad := range []map[string]any{
		{"hidden": "x"}, {"nope": "x"}, {"user": "Admin"}, {"pass": "short"}, {"port": "1.5"},
		{"user": "a\nb"}, {"user": map[string]any{"source": "own"}},
	} {
		if _, err := resolveParams(manifest(), bad); code(err) != CodeBadParams {
			t.Errorf("%v accepted: %v", bad, err)
		}
	}
	// "-" is what a command line writes for "read it from standard input":
	// never installed as a password, and the message says where it goes.
	if _, err := resolveParams(manifest(), map[string]any{"pass": "-"}); code(err) != CodeBadParams || !strings.Contains(err.Error(), "标准输入") {
		t.Errorf("\"-\" as a secret: %v", err)
	}
	if v, err := resolveParams(manifest(), map[string]any{"user": "-"}); code(err) != CodeBadParams || v != nil {
		t.Errorf("\"-\" against the pattern: %v", err) // a plain parameter checks "-" like any value
	}
}

// A key that is no parameter: the message lists the app's parameters (not
// the hidden one); leniently (a plan) the values and what is missing are
// worked out as well.
func TestUnknownParamNamesTheParams(t *testing.T) {
	m := manifest()
	m.Params = append(m.Params, catalog.Param{Key: "model", Type: "string", Label: "模型名", Required: true})
	_, err := resolveParams(m, map[string]any{"nope": "x"})
	if msg := err.Error(); code(err) != CodeBadParams || !strings.Contains(msg, "它的参数有：user、pass、port、need、model（模型名）") || strings.Contains(msg, "hidden") {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := resolveParams(m, map[string]any{"模型名": "qwen-plus"}); code(err) != CodeBadParams || !strings.Contains(err.Error(), "参数名是 model（模型名）") {
		t.Fatalf("label: %v", err)
	}
	vals, missing, err := resolveParamsOpt(m, map[string]any{"模型名": "qwen-plus", "user": "root"}, true)
	if code(err) != CodeBadParams || vals == nil || vals["user"] != "root" || len(missing) != 1 || missing[0].Key != "model" {
		t.Fatalf("lenient: %v %v %+v", err, vals, missing)
	}
	// A hidden parameter is still refused outright.
	if vals, _, err := resolveParamsOpt(m, map[string]any{"hidden": "x"}, true); code(err) != CodeBadParams || vals != nil {
		t.Fatalf("hidden: %v %v", err, vals)
	}
}

func TestRenderTemplates(t *testing.T) {
	v := Values{"a": `pa"ss\word`, usesKey: map[string]map[string]string{"llm": {"base_url": "http://provider-app:3000/v1", "api_key": "sk-1", "model": "m"}}}
	out, err := v.render(`k: "{{params.a}}"`+"\n"+`u: "{{ uses.llm.base_url }}"`, fileEscape("config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if out != `k: "pa\"ss\\word"`+"\n"+`u: "http://provider-app:3000/v1"` {
		t.Fatalf("yaml:\n%s", out)
	}
	if raw, _ := v.render("{{params.a}}", fileEscape("env.txt")); raw != `pa"ss\word` {
		t.Fatalf("raw %q", raw)
	}
	for _, bad := range []string{"{{params.zzz}}", "{{params.a.b}}", "{{uses.llm}}", "{{uses.nope.x}}", "{{params.llm.base_url}}", "{{params.a | nope}}"} {
		if _, err := v.render(bad, nil); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	// What is no reference is kept as written; \{{ writes {{.
	if out, err := v.render(`{{env.X}} {{ $json.a }} \{{params.a}}`, nil); err != nil || out != `{{env.X}} {{ $json.a }} {{params.a}}` {
		t.Errorf("kept: %q %v", out, err)
	}
	// A need that is not connected has no value: a default stands in, a
	// condition on it is false.
	if out, err := v.render(`{{uses.mail.host | default "none"}}`, nil); err != nil || out != "none" {
		t.Errorf("default: %q %v", out, err)
	}
	if ok, err := v.holds("uses.mail.host"); err != nil || ok {
		t.Errorf("holds: %v %v", ok, err)
	}
	if ok, err := v.holds("params.a != x"); err != nil || !ok {
		t.Errorf("holds !=: %v %v", ok, err)
	}
	j, err := v.renderJSON(map[string]any{"username": "{{params.a}}", "SelfUseModeEnabled": true, "n": 3, "list": []any{"{{uses.llm.model}}"}})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(j)
	if string(b) != `{"SelfUseModeEnabled":true,"list":["m"],"n":3,"username":"pa\"ss\\word"}` {
		t.Fatalf("json %s", b)
	}
}

func TestMatchJSON(t *testing.T) {
	var got any
	_ = json.Unmarshal([]byte(`{"success":true,"data":{"n":1,"s":"x"}}`), &got)
	if !matchJSON(map[string]any{"success": true}, got) || !matchJSON(map[string]any{"data": map[string]any{"n": 1}}, got) {
		t.Fatal("should match")
	}
	if matchJSON(map[string]any{"success": false}, got) || matchJSON(map[string]any{"x": 1}, got) || matchJSON(map[string]any{"a": 1}, "str") {
		t.Fatal("should not match")
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, c := range []struct {
		have, min string
		ok        bool
	}{{"0.5.0", "0.5.0", true}, {"0.5.0-dev.3", "0.5.0", true}, {"v0.6.1", "0.5.0", true}, {"0.4.9", "0.5.0", false}, {"0.2.0-dev", "0.5.0", false}, {"dev", "0.5.0", false}, {"0.1.0", "", true}} {
		if versionAtLeast(c.have, c.min) != c.ok {
			t.Errorf("%s >= %s", c.have, c.min)
		}
	}
}

func TestSecretBox(t *testing.T) {
	dir := t.TempDir()
	b := newSecretBox(dir)
	c, err := b.seal([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := newSecretBox(dir).open(c) // a new box reads the same key file
	if err != nil || string(p) != "hello" {
		t.Fatalf("open %q %v", p, err)
	}
	if _, err := newSecretBox(t.TempDir()).open(c); err == nil {
		t.Fatal("another key opened it")
	}
}
