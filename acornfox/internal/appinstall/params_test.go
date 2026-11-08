package appinstall

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
		{Key: "llm", Type: "llm_provider"},
	}}
}

func TestResolveParams(t *testing.T) {
	v, err := resolveParams(manifest(), map[string]any{"pass": "my-own-pass", "port": 8080.0, "llm": map[string]any{"source": "local"}})
	if err != nil {
		t.Fatal(err)
	}
	if v["user"] != "admin" || v["pass"] != "my-own-pass" || len(v["hidden"].(string)) != 32 || v["port"] != "8080" || v["need"] != "d" {
		t.Fatalf("values %+v", v)
	}
	if c := v["llm"].(*LLMChoice); c.Source != LLMLocalGateway {
		t.Fatalf("llm %+v", c)
	}
	for _, bad := range []map[string]any{
		{"hidden": "x"}, {"nope": "x"}, {"user": "Admin"}, {"pass": "short"}, {"port": "1.5"},
		{"user": "a\nb"}, {"llm": map[string]any{"source": "cloud"}},
	} {
		if _, err := resolveParams(manifest(), bad); code(err) != CodeBadParams {
			t.Errorf("%v accepted: %v", bad, err)
		}
	}
}

func TestRenderTemplates(t *testing.T) {
	v := Values{"a": `pa"ss\word`, "llm": &LLMChoice{BaseURL: "http://new-api:3000/v1", APIKey: "sk-1", Model: "m"}}
	out, err := v.render(`k: "{{params.a}}"`+"\n"+`u: "{{ params.llm.base_url }}"`, fileEscape("config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if out != `k: "pa\"ss\\word"`+"\n"+`u: "http://new-api:3000/v1"` {
		t.Fatalf("yaml:\n%s", out)
	}
	if raw, _ := v.render("{{params.a}}", fileEscape("env.txt")); raw != `pa"ss\word` {
		t.Fatalf("raw %q", raw)
	}
	for _, bad := range []string{"{{params.zzz}}", "{{params.a.b}}", "{{params.llm}}", "{{params.llm.nope}}", "{{env.X}}"} {
		if _, err := v.render(bad, nil); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	j, err := v.renderJSON(map[string]any{"username": "{{params.a}}", "SelfUseModeEnabled": true, "n": 3, "list": []any{"{{params.llm.model}}"}})
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

func TestNewestTokenID(t *testing.T) {
	if id := newestTokenID(json.RawMessage(`{"items":[{"id":3,"name":"a"},{"id":7,"name":"a"},{"id":9,"name":"b"}]}`), "a"); id != 7 {
		t.Fatalf("paged %d", id)
	}
	if id := newestTokenID(json.RawMessage(`[{"id":2,"name":"a"}]`), "a"); id != 2 {
		t.Fatalf("array %d", id)
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

func TestNextDaily(t *testing.T) {
	loc := time.FixedZone("x", 8*3600)
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, loc)
	if n := nextDaily(now, 11*time.Hour); !n.Equal(time.Date(2026, 11, 6, 11, 0, 0, 0, loc)) {
		t.Fatalf("today %v", n)
	}
	if n := nextDaily(now, 9*time.Hour); !n.Equal(time.Date(2026, 11, 7, 9, 0, 0, 0, loc)) {
		t.Fatalf("tomorrow %v", n)
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
