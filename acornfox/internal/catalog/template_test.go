package catalog

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// lookupFrom resolves references from a map; "uses.off.*" has no value
// (ErrNoValue), anything else missing is an error of its own.
func lookupFrom(vals map[string]string) func(string) (string, error) {
	return func(ref string) (string, error) {
		if strings.HasPrefix(ref, "uses.off.") {
			return "", fmt.Errorf("%s：%w", ref, ErrNoValue)
		}
		v, ok := vals[ref]
		if !ok {
			return "", fmt.Errorf("没有 %s", ref)
		}
		return v, nil
	}
}

var tmplVals = map[string]string{
	"params.a":          "Hello World",
	"params.tricky":     `pa"ss\word: {{x}} | é`,
	"params.empty":      "",
	"platform.region":   "cn",
	"platform.app.url":  "http://127.0.0.1:18811",
	"platform.app.mode": "https",
	"uses.db.sslmode":   "require",
}

func render(t *testing.T, tmpl string, escape func(string) string) string {
	t.Helper()
	out, err := RenderTemplate(tmpl, lookupFrom(tmplVals), escape)
	if err != nil {
		t.Fatalf("%q: %v", tmpl, err)
	}
	return out
}

// Every transform of the table, alone and chained.
func TestTransforms(t *testing.T) {
	cases := []struct{ tmpl, want string }{
		{`{{params.a}}`, "Hello World"},
		{`{{ params.a | lower }}`, "hello world"},
		{`{{params.a|upper}}`, "HELLO WORLD"},
		{`{{params.a | replace " " "_"}}`, "Hello_World"},
		{`{{params.a | replace "o" ""}}`, "Hell Wrld"},
		{`{{params.a | b64}}`, "SGVsbG8gV29ybGQ="},
		{`{{params.tricky | urlencode}}`, "pa%22ss%5Cword%3A%20%7B%7Bx%7D%7D%20%7C%20%C3%A9"},
		{`{{params.a | sha256}}`, "a591a6d40bf420404a011733cfb7b190d62c65bf0bcda32b57b277d9ad9f146e"},
		{`{{params.tricky | json}}`, `"pa\"ss\\word: {{x}} | é"`},
		{`{{params.tricky | yaml}}`, `"pa\"ss\\word: {{x}} | é"`},
		{`{{params.tricky | shell}}`, `'pa"ss\word: {{x}} | é'`},
		{`{{params.a | replace "o" "'" | shell}}`, `'Hell'\'' W'\''rld'`},
		{`{{params.empty | default "none"}}`, "none"},
		{`{{params.a | default "none"}}`, "Hello World"},
		{`{{uses.off.host | default "none"}}`, "none"},
		{`{{uses.off.host | upper | default "x"}}`, "x"},
		{`{{platform.app.mode | map "https=true" "http=false"}}`, "true"},
		{`{{uses.db.sslmode | map "disable=false" "*=true"}}`, "true"},
		{`{{platform.region | map "cn=国内" "global=海外"}}`, "国内"},
		{`{{params.a | map "a=b=c" "Hello World=x=y"}}`, "x=y"},
		{`{{params.a | replace "\"" "\\"}}`, "Hello World"},
		{`{{platform.app.url}}/webhook/`, "http://127.0.0.1:18811/webhook/"},
		{`{{platform.app.url}}{{platform.region}}`, "http://127.0.0.1:18811cn"},
	}
	for _, c := range cases {
		if got := render(t, c.tmpl, nil); got != c.want {
			t.Errorf("%s = %q，要 %q", c.tmpl, got, c.want)
		}
	}
	// json and yaml write a quoted value of their own: the file's escape is
	// not applied on top. Without them it is, after the transforms.
	esc := func(s string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) }
	if got := render(t, `a: {{params.tricky | json}} b: "{{params.tricky | upper}}"`, esc); got != `a: "pa\"ss\\word: {{x}} | é" b: "PA\"SS\\WORD: {{X}} | É"` {
		t.Fatalf("escape: %s", got)
	}
}

// Only kind.name is replaced; anything else between double braces is kept
// as written, \{{ writes {{.
func TestTemplateKeepsWhatIsNoReference(t *testing.T) {
	cases := []struct{ tmpl, want string }{
		{`{{ $json.x }} {{ .Values.y }} {{env.X}} {{token}} {{site.title}}`, `{{ $json.x }} {{ .Values.y }} {{env.X}} {{token}} {{site.title}}`},
		{`\{{params.a}} = {{params.a}}`, `{{params.a}} = Hello World`},
		{`\{{ anything }}`, `{{ anything }}`},
		{`back\slash \{ \}}`, `back\slash \{ \}}`},
		{`{{{params.a}}}`, `{Hello World}`},
		{`{one} { {two} } {{}} {{ }}`, `{one} { {two} } {{}} {{ }}`},
		{"line one\n{{params.a}}\n", "line one\nHello World\n"},
		{`{{params.a}}}}`, `Hello World}}`},
	}
	for _, c := range cases {
		if got := render(t, c.tmpl, nil); got != c.want {
			t.Errorf("%s = %q，要 %q", c.tmpl, got, c.want)
		}
	}
	tp := ScanTemplate(`{{ $json.x }} {{params.a}} {{ two words }} {{}} {{env.X}}`)
	if got := strings.Join(tp.Others, ","); got != "$json.x,two words,env.X" {
		t.Fatalf("Others %q", got)
	}
	if got := strings.Join(TemplateRefs(`{{params.a | upper}} {{ uses.db.host }} {{Params.A}} \{{params.b}}`), ","); got != "params.a,uses.db.host" {
		t.Fatalf("TemplateRefs %q", got)
	}
}

// A reference that cannot be read is CAT-LINT-46 (syntax) or 47 (transform);
// what was written is in the message, never a value.
func TestTemplateErrors(t *testing.T) {
	cases := []struct{ tmpl, code string }{
		{`{{params.a`, CodeTemplate},
		{"{{params.a\n}}", CodeTemplate},
		{`{{params.a | }}`, CodeTemplate},
		{`{{params.a | map "x=1}}`, CodeTemplate},
		{`{{params.a | map x=1}}`, CodeTemplate},
		{`{{params.a | map"x=1"}}`, CodeTemplate},
		{`{{params.a | replace "a""b"}}`, CodeTemplate},
		{`{{params.A}}`, CodeTemplate},
		{`{{params.a.}}`, CodeTemplate},
		{`{{params.a b}}`, CodeTemplate},
		{`{{params.a | default "\n"}}`, CodeTemplate},
		{`{{params.a | bcrypt}}`, CodeTransform},
		{`{{params.a | Upper}}`, CodeTemplate},
		{`{{params.a | upper "x"}}`, CodeTransform},
		{`{{params.a | default}}`, CodeTransform},
		{`{{params.a | replace "a"}}`, CodeTransform},
		{`{{params.a | replace "" "b"}}`, CodeTransform},
		{`{{params.a | map}}`, CodeTransform},
		{`{{params.a | map "x"}}`, CodeTransform},
		{`{{params.a | map "x=1" "x=2"}}`, CodeTransform},
	}
	for _, c := range cases {
		_, err := ParseTemplate(c.tmpl)
		var te *TemplateError
		if !errors.As(err, &te) || te.Code != c.code {
			t.Errorf("%q：%v，要 %s", c.tmpl, err, c.code)
			continue
		}
		if !IsRenderError(err) {
			t.Errorf("%q 不算渲染错误", c.tmpl)
		}
	}
	// A lenient scan keeps going: both bad ones are listed, the good one is
	// still a reference.
	tp := ScanTemplate(`{{params.a | nope}} {{params.b}} {{params.c | map}}`)
	if len(tp.Bad) != 2 || len(tp.Exprs()) != 1 || tp.Exprs()[0].Ref != "params.b" {
		t.Fatalf("%+v", tp)
	}
}

// A map that matches nothing and has no "*" fails when rendering, without
// the value in the message; a reference without a value fails unless a
// default covers it; any other lookup error is passed on.
func TestRenderFailures(t *testing.T) {
	tmplVals := map[string]string{"params.secret": "s3cr3t-value"}
	_, err := RenderTemplate(`x={{params.secret | map "a=1" "b=2"}}`, lookupFrom(tmplVals), nil)
	var xe *TransformError
	if !errors.As(err, &xe) || !IsRenderError(err) || strings.Contains(err.Error(), "s3cr3t") || !strings.Contains(err.Error(), "map") {
		t.Fatalf("map: %v", err)
	}
	out, err := RenderTemplate(`a {{uses.off.host}} b`, lookupFrom(tmplVals), nil)
	if !errors.Is(err, ErrNoValue) || IsRenderError(err) || out != "a {{uses.off.host}} b" {
		t.Fatalf("no value: %q %v", out, err)
	}
	if _, err := RenderTemplate(`{{params.nope | default "x"}}`, lookupFrom(tmplVals), nil); err == nil || errors.Is(err, ErrNoValue) {
		t.Fatalf("a missing parameter is no 'no value': %v", err)
	}
}

func TestConditions(t *testing.T) {
	look := lookupFrom(map[string]string{"platform.region": "cn", "params.on": "true", "params.off": "false",
		"params.zero": "0", "params.empty": "", "params.word": "yes"})
	cases := []struct {
		cond string
		want bool
	}{
		{"platform.region == cn", true},
		{"platform.region==cn", true},
		{"  platform.region != cn  ", false},
		{"platform.region == global", false},
		{"platform.region != global", true},
		{"params.on", true},
		{"params.word", true},
		{"params.off", false},
		{"params.zero", false},
		{"params.empty", false},
		{"uses.off.host", false},
		{"uses.off.host == x", false},
		{"uses.off.host != x", false},
	}
	for _, c := range cases {
		cd, err := ParseCondition(c.cond)
		if err != nil {
			t.Fatalf("%q: %v", c.cond, err)
		}
		got, err := cd.Eval(look)
		if err != nil || got != c.want {
			t.Errorf("%q = %v %v，要 %v", c.cond, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "{{platform.region}} == cn", "platform.region == 'cn'", "platform.region == c n", "platform.region == ",
		"region == cn", "platform.region > cn", "platform.region == cn and params.on", "Params.on", "volumes.data", "platform.region === cn"} {
		_, err := ParseCondition(bad)
		var te *TemplateError
		if !errors.As(err, &te) || te.Code != CodeIf {
			t.Errorf("%q：%v，要 CAT-LINT-48", bad, err)
		}
	}
	cd, _ := ParseCondition("params.nope")
	if _, err := cd.Eval(look); err == nil {
		t.Fatal("a reference that does not exist is an error, not false")
	}
}

func TestConditionsExclusive(t *testing.T) {
	c := func(s string) Condition {
		cd, err := ParseCondition(s)
		if err != nil {
			t.Fatal(err)
		}
		return cd
	}
	yes := [][2]string{
		{"platform.app.scheme == https", "platform.app.scheme == http"},
		{"platform.region == cn", "platform.region != cn"},
		{"platform.region != cn", "platform.region == cn"},
	}
	no := [][2]string{
		{"platform.region == cn", "platform.region == cn"},
		{"platform.region != cn", "platform.region != global"},
		{"platform.region == cn", "params.x == global"},
		{"params.on", "params.on == false"},
		{"platform.region == cn", "platform.region != global"},
	}
	for _, p := range yes {
		if !c(p[0]).Exclusive(c(p[1])) {
			t.Errorf("%q 和 %q 应该互斥", p[0], p[1])
		}
	}
	for _, p := range no {
		if c(p[0]).Exclusive(c(p[1])) {
			t.Errorf("%q 和 %q 证明不了互斥", p[0], p[1])
		}
	}
}

// The service type definitions' test templates: bare item names, nothing
// else replaced (the old reading of {{host}}).
func TestFieldTemplates(t *testing.T) {
	vals := map[string]string{"host": "db", "port": "5432", "a.b": "x"}
	got := FillFields("{{host}}:{{ port }} {{a.b}} {{params.x}} {{ two words }} {{{host}}} {{missing}}", func(k string) string { return vals[k] })
	if got != "db:5432 x  {{ two words }} {db} " {
		t.Fatalf("%q", got)
	}
	if refs := strings.Join(FieldRefs("{{host}}:{{port}}/{{ db-name }}"), ","); refs != "host,port" {
		t.Fatalf("%q", refs)
	}
}
