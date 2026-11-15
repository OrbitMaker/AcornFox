package catalog

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// servicesDir is the test catalog written the new way: three service types,
// one app that provides a service and one that needs services.
var servicesDir = filepath.Join("testdata", "services")

// copyServices copies the services test catalog so a test can break it.
func copyServices(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(servicesDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(servicesDir, p)
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

func readServicesManifest(t *testing.T, id string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(servicesDir, "apps", id, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// problemsWith returns the problems of r that carry code.
func problemsWith(r Report, code string) []Problem {
	var out []Problem
	for _, p := range r.Problems {
		if p.Code == code {
			out = append(out, p)
		}
	}
	return out
}

// The new fields decode into the structs the engine reads: the three ways to
// write a provided value, the three ways to write a saved value, steps with
// headers and expected status codes, uses with "without", optional env.
func TestParseServiceForms(t *testing.T) {
	m, err := ParseManifest(readServicesManifest(t, "demo-provider"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Provides) != 1 || len(m.Init) != 2 {
		t.Fatalf("provides %d init %d", len(m.Provides), len(m.Init))
	}
	p := m.Provides[0]
	if p.Kind != "demo_token" || p.Endpoint != "web" {
		t.Fatalf("provide %+v", p)
	}
	if got := strings.Join(p.ValueKeys(), ","); got != "token,url" {
		t.Fatalf("value keys %s", got)
	}
	if v := p.Values["url"]; v.Kind() != ValueTemplate || v.Template != "{{self.internal_url}}/api/{{params.region_hint}}" {
		t.Fatalf("模板写法：%+v", v)
	}
	if v := p.Values["token"]; v.Kind() != ValueIssued || v.Issued != "token" || v.Secret {
		t.Fatalf("issued 写法：%+v", v)
	}
	if got := strings.Join(p.IssuedKeys(), ","); got != "session,token,token_id" {
		t.Fatalf("issue 存下的键：%s", got)
	}
	if len(p.Issue) != 3 || len(p.Revoke) != 2 || len(p.Check) != 1 {
		t.Fatalf("步骤数 %d %d %d", len(p.Issue), len(p.Revoke), len(p.Check))
	}

	// Saved values: short form, path form, list form.
	if s := m.Init[0].HTTP.Save["setup_token"]; !reflect.DeepEqual(s, SaveValue{Path: "data.token"}) {
		t.Fatalf("简写：%+v", s)
	}
	if s := p.Issue[0].HTTP.Save["session"]; s.Path != "data.session" || !s.Secret || s.FromList() {
		t.Fatalf("path 写法：%+v", s)
	}
	read := p.Issue[2].HTTP
	if got := strings.Join(read.SaveKeys(), ","); got != "token,token_id" {
		t.Fatalf("save keys %s", got)
	}
	id := read.Save["token_id"]
	if !id.FromList() || id.List != "data.items" || id.SortBy != "id" || id.Pick != "last" || !id.PickLast() || id.Field != "id" ||
		id.Where["name"] != "acornfox-{{consumer.name}}" {
		t.Fatalf("list 写法：%+v", id)
	}
	tok := read.Save["token"]
	if !tok.FromList() || tok.Pick != "" || !tok.PickLast() || tok.Field != "key" || tok.Prefix != "dt-" || !tok.Secret {
		t.Fatalf("list 写法（不写 pick 默认取最后一项）：%+v", tok)
	}
	if (SaveValue{Pick: "first"}).PickLast() {
		t.Fatal("pick: first")
	}

	// Steps: name optional, headers, method, expected status.
	if m.Init[0].Name != "" || m.Init[0].Label(0) != "0" || m.Init[1].Label(1) != "finish-setup" || m.Init[0].When != "after_healthy" || m.Init[0].IsExec() {
		t.Fatalf("init 步骤：%+v", m.Init)
	}
	if h := m.Init[1].HTTP; h.Method != "PATCH" || h.Headers["Authorization"] != "Bearer {{saved.setup_token}}" {
		t.Fatalf("headers：%+v", h)
	}
	if h := m.Init[0].HTTP; !h.StatusOK(200) || !h.StatusOK(201) || h.StatusOK(204) {
		t.Fatalf("expect_status [200, 201]：%+v", h.ExpectStatus)
	}
	if h := m.Init[1].HTTP; !h.StatusOK(204) || h.StatusOK(302) || h.StatusOK(199) {
		t.Fatal("不写 expect_status 时 2xx 都算过")
	}
	if del := p.Revoke[1].HTTP; del.Method != "DELETE" || del.Path != "/api/tokens/{{issued.token_id}}" || !del.StatusOK(404) || del.StatusOK(204) {
		t.Fatalf("revoke：%+v", del)
	}
	if p.Check[0].Name != "" || p.Check[0].When != "" {
		t.Fatalf("check：%+v", p.Check[0])
	}

	// The consumer: uses and optional env.
	c, err := ParseManifest(readServicesManifest(t, "demo-consumer"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Uses) != 2 || c.Uses[0] != (Use{Name: "demo", Kind: "demo_token"}) ||
		c.Uses[1] != (Use{Name: "mail", Kind: "smtp", Optional: true, Without: "能用，但发不了通知邮件"}) {
		t.Fatalf("uses %+v", c.Uses)
	}
	if c.Env[0].Optional || !c.Env[2].Optional || c.Env[2].From != "uses.mail.host" {
		t.Fatalf("env %+v", c.Env)
	}

	// Exec steps, dedicated uses and the "ask" form.
	raw := `
uses:
  - { name: db, kind: postgres, dedicated: true }
provides:
  - kind: llm_gateway
    values:
      api_key: { ask: 在应用里新建一把 Key，贴进来, secret: true }
      model: ""
    issue:
      - exec:
          command: ["psql", "-c", "select 1"]
          user: postgres
          timeout_s: 60
`
	x, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !x.Uses[0].Dedicated {
		t.Fatalf("dedicated：%+v", x.Uses[0])
	}
	if v := x.Provides[0].Values["api_key"]; v.Kind() != ValueAsk || v.Ask != "在应用里新建一把 Key，贴进来" || !v.Secret {
		t.Fatalf("ask 写法：%+v", v)
	}
	if v := x.Provides[0].Values["model"]; v.Kind() != ValueTemplate || v.Template != "" {
		t.Fatalf("空模板：%+v", v)
	}
	st := x.Provides[0].Issue[0]
	if !st.IsExec() || strings.Join(st.Exec.Command, " ") != "psql -c select 1" || st.Exec.User != "postgres" || st.Exec.TimeoutS != 60 || st.Label(0) != "0" {
		t.Fatalf("exec：%+v", st)
	}

	// The two types with their own decoding survive a trip through JSON.
	for name, v := range p.Values {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var back ValueSource
		if err := json.Unmarshal(b, &back); err != nil || back != v {
			t.Fatalf("%s 过一遍 JSON 变了：%s → %+v（%v）", name, b, back, err)
		}
	}
	if b, _ := json.Marshal(p.Values["url"]); string(b) != `"{{self.internal_url}}/api/{{params.region_hint}}"` {
		t.Fatalf("模板写法的 JSON 是一个字符串：%s", b)
	}
	for _, s := range []SaveValue{id, tok, {Path: "a.b"}} {
		b, _ := json.Marshal(s)
		var back SaveValue
		if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back, s) {
			t.Fatalf("SaveValue 过一遍 JSON 变了：%s → %+v（%v）", b, back, err)
		}
	}
	var short SaveValue
	if err := json.Unmarshal([]byte(`"data.key"`), &short); err != nil || short.Path != "data.key" {
		t.Fatalf("JSON 里的简写：%+v %v", short, err)
	}
}

// The services test catalog is clean, and needs the first platform of format
// 3 and nothing more.
func TestLintServicesCatalog(t *testing.T) {
	r, err := Lint(servicesDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 0 {
		t.Fatalf("新写法的样例目录不该有问题：%+v", r.Problems)
	}
	if strings.Join(r.Apps, ",") != "demo-consumer,demo-provider" || strings.Join(r.ServiceTypes, ",") != "demo_token,llm_gateway,smtp" {
		t.Fatalf("apps %v types %v", r.Apps, r.ServiceTypes)
	}
	for _, id := range r.Apps {
		info := r.MinPlatform.Apps[id]
		if info.Declared != BasePlatform || info.Computed != BasePlatform || len(info.Reasons) != 0 {
			t.Fatalf("%s 的最低版本：%+v", id, info)
		}
	}

	// Declared as an older version: CAT-LINT-17, the catalog does not build.
	dir := copyServices(t)
	for _, id := range r.Apps {
		edit(t, filepath.Join(dir, "apps", id, manifestName), `min_platform: "0.6.0"`, `min_platform: "0.5.1"`)
	}
	r, _ = Lint(dir)
	if n := len(problemsWith(r, CodeMinPlatform)); n != 2 || r.Errors() != 2 {
		t.Fatalf("写低了最低版本要报两个 %s：%+v", CodeMinPlatform, r.Problems)
	}
	if _, err := Build(dir, 1); CodeOf(err) != CodeBuildLint {
		t.Fatalf("写低了不能打包：%v", err)
	}

	// The catalog repository's drafts are clean too, and list their one type.
	g, err := Lint(filepath.Join("testdata", "good"))
	if err != nil || len(g.Problems) != 0 || strings.Join(g.ServiceTypes, ",") != "llm_gateway" {
		t.Fatalf("good：%v %+v %v", err, g.Problems, g.ServiceTypes)
	}
}

// The checks about services, one broken catalog per case. want is the code
// that must be reported; warn says it is a warning (the catalog still builds).
func TestLintServiceProblems(t *testing.T) {
	const (
		provider = "apps/demo-provider/manifest.yaml"
		consumer = "apps/demo-consumer/manifest.yaml"
		smtp     = "service-types/smtp.yaml"
		demo     = "service-types/demo_token.yaml"
	)
	long := strings.Repeat("字", 81)
	cases := []struct {
		name, file, old, new, want string
		warn                       bool
	}{
		// CAT-LINT-22: a kind without a definition.
		{"uses an undefined kind", consumer, "{ name: demo, kind: demo_token }", "{ name: demo, kind: demo_tokens }", CodeServiceKind, false},
		{"provides an undefined kind", provider, "  - kind: demo_token\n", "  - kind: redis\n", CodeServiceKind, false},

		// CAT-LINT-23: does not fit the service type.
		{"value the type does not have", provider, "      token: { issued: token }\n", "      token: { issued: token }\n      region: cn\n", CodeServiceFields, false},
		{"required item without a source", provider, "      token: { issued: token }\n", "", CodeServiceFields, false},
		{"uses item the type does not have (env)", consumer, "from: uses.demo.url }", "from: uses.demo.address }", CodeServiceFields, false},
		{"uses item the type does not have (file)", consumer, "url = {{uses.demo.url}}", "url = {{uses.mail.server}}", CodeServiceFields, false},
		{"required item with a blank source", provider, "      token: { issued: token }\n", "      token: \"  \"\n", CodeServiceFields, false},

		// CAT-LINT-24: temporary values that are not there.
		{"saved before any step saved it", provider, "          headers: { X-Session: \"{{saved.session}}\" }\n          json: { name:", "          headers: { X-Session: \"{{saved.sid}}\" }\n          json: { name:", CodeStepRef, false},
		{"a step cannot use its own save", provider, "          json: { password: \"{{params.admin_password}}\" }\n          expect_json: { ok: true }\n", "          json: { password: \"{{saved.session}}\" }\n          expect_json: { ok: true }\n", CodeStepRef, false},
		{"saved of another list", provider, "          headers: { Authorization: \"Bearer {{issued.token}}\" }", "          headers: { Authorization: \"Bearer {{saved.session}}\" }", CodeStepRef, false},
		{"issued key the issue steps do not save", provider, "/api/tokens/{{issued.token_id}}", "/api/tokens/{{issued.id}}", CodeStepRef, false},
		{"issued inside issue", provider, "owner: \"{{consumer.id}}\"", "owner: \"{{issued.token}}\"", CodeStepRef, false},
		{"value names a key the issue steps do not save", provider, "      token: { issued: token }\n", "      token: { issued: api_key }\n", CodeStepRef, false},
		{"consumer inside init", provider, "      json: { password: \"{{params.admin_password}}\" }\n      expect_status: [200, 201]\n", "      json: { password: \"{{consumer.name}}\" }\n      expect_status: [200, 201]\n", CodeStepRef, false},
		{"consumer.<unknown>", provider, "owner: \"{{consumer.id}}\"", "owner: \"{{consumer.owner}}\"", CodeStepRef, false},
		{"issue.id in revoke", provider, "/api/tokens/{{issued.token_id}}", "/api/tokens/{{issue.id}}", CodeStepRef, false},
		{"issue.id in init", provider, "      path: /api/setup/finish\n", "      path: \"/api/setup/{{issue.id}}\"\n", CodeStepRef, false},
		{"issue.id in a provided value", provider, "/api/{{params.region_hint}}\"", "/api/{{issue.id}}\"", CodeStepRef, false},
		{"issue.<unknown>", provider, "owner: \"{{consumer.id}}\"", "owner: \"{{issue.number}}\"", CodeStepRef, false},
		{"issue step saves issue_id", provider, "            session: { path: data.session, secret: true }\n", "            session: { path: data.session, secret: true }\n            issue_id: data.id\n", CodeStepRef, false},
		{"self without endpoint", provider, "    endpoint: web\n    values:", "    values:", CodeStepRef, false},
		{"self.<unknown>", provider, "{{self.internal_url}}/api", "{{self.url}}/api", CodeStepRef, false},
		{"self outside values", provider, "      path: /api/setup/finish\n", "      path: \"/api/setup/{{self.port}}\"\n", CodeStepRef, false},
		{"saved in a file template", consumer, "url = {{uses.demo.url}}", "url = {{saved.token}}", CodeStepRef, false},
		{"uses inside a provided value", provider, "/api/{{params.region_hint}}\"", "/api/{{uses.demo.url}}\"", CodeStepRef, false},
		// … and a {{…}} that is no reference at all: the engine would stop at
		// it, an issue step only when the first consumer is installed, a
		// revoke step only when one is removed.
		{"header: capital in the key", provider, "          headers: { X-Session: \"{{saved.session}}\" }\n          json: { name:", "          headers: { X-Session: \"{{saved.Session}}\" }\n          json: { name:", CodeTemplate, false},
		{"header: no kind", provider, "          headers: { X-Session: \"{{saved.session}}\" }\n          json: { name:", "          headers: { X-Session: \"{{session}}\" }\n          json: { name:", CodeNotRef, true},
		{"header: a kind that does not exist", provider, "          headers: { X-Session: \"{{saved.session}}\" }\n          json: { name:", "          headers: { X-Session: \"{{save.session}}\" }\n          json: { name:", CodeNotRef, true},
		{"header: blanks around a non-reference", provider, "          headers: { X-Session: \"{{saved.session}}\" }\n          json: { name:", "          headers: { X-Session: \"{{ SESSION }}\" }\n          json: { name:", CodeNotRef, true},
		{"revoke path: not a reference", provider, "/api/tokens/{{issued.token_id}}", "/api/tokens/{{token_id}}", CodeNotRef, true},
		{"body: not a reference", provider, "owner: \"{{consumer.id}}\"", "owner: \"{{consumer-id}}\"", CodeNotRef, true},
		{"expect_json: not a reference", provider, "          expect_json: { ok: true }\n", "          expect_json: { ok: true, user: \"{{admin}}\" }\n", CodeNotRef, true},
		{"save where: not a reference", provider, "where: { name: \"acornfox-{{consumer.name}}\" }, field: key", "where: { name: \"acornfox-{{Consumer.Name}}\" }, field: key", CodeTemplate, false},
		{"init body: not a reference", provider, "      json: { password: \"{{params.admin_password}}\" }\n      expect_status: [200, 201]\n", "      json: { password: \"{{admin_password}}\" }\n      expect_status: [200, 201]\n", CodeNotRef, true},
		{"value template: not a reference", provider, "{{self.internal_url}}/api", "{{internal_url}}/api", CodeNotRef, true},
		{"value template: a kind that does not exist", provider, "/api/{{params.region_hint}}\"", "/api/{{param.region_hint}}\"", CodeTemplate, false},
		{"volume as a value in a step", provider, "      path: /api/setup/finish\n", "      path: \"/api/setup/{{volumes.data}}\"\n", CodeStepRef, false},

		// CAT-LINT-25: the definition files.
		{"kind differs from the file name", smtp, "kind: smtp", "kind: mail", CodeServiceType, false},
		{"unknown field in a definition", smtp, "label: 邮箱（发信）", "label: 邮箱（发信）\nicon: mail.svg", CodeServiceType, false},
		{"definition is not YAML", smtp, "schema: 1", "schema: [1", CodeServiceType, false},
		{"enum without options", smtp, "type: enum, options: [starttls, tls, none], default: starttls", "type: enum", CodeServiceType, false},
		{"options on a non-enum field", smtp, "{ key: from, label: 发件人地址, type: string }", "{ key: from, label: 发件人地址, type: string, options: [a] }", CodeServiceType, false},
		{"default not among the options", smtp, "default: starttls", "default: ssl", CodeServiceType, false},
		{"default of the wrong type", smtp, "type: int, default: 587", "type: int, default: \"587\"", CodeServiceType, false},
		{"secret with a default", smtp, "type: string, secret: true }", "type: string, secret: true, default: hunter2 }", CodeServiceType, false},
		{"field written twice", smtp, "{ key: username, label: 账号, type: string }", "{ key: host, label: 账号, type: string }", CodeServiceType, false},
		{"option written twice", smtp, "options: [starttls, tls, none]", "options: [starttls, tls, tls]", CodeServiceType, false},
		{"url default that is no URL", demo, "{ key: url, label: 地址, type: url, required: true }", "{ key: url, label: 地址, type: url, default: not-a-url }", CodeServiceType, false},
		{"hint that is empty", smtp, "{ key: from, label: 发件人地址, type: string }", "{ key: from, label: 发件人地址, type: string, hint: \"\" }", CodeServiceType, false},
		{"test refers to a field that is not there", smtp, "port: \"{{port}}\" }", "port: \"{{smtp_port}}\" }", CodeServiceType, false},
		{"test with both tcp and http", smtp, "  tcp: { host: \"{{host}}\", port: \"{{port}}\" }", "  tcp: { host: \"{{host}}\", port: \"{{port}}\" }\n  http: { url: \"http://{{host}}\" }", CodeServiceType, false},
		{"default_provider is gone from the format", demo, "fields:", "default_provider: demo-provider\nfields:", CodeServiceType, false},

		// CAT-LINT-26: TestDedicatedIsBasePlatformAndPending, TestLintSoloCatalog.

		// CAT-LINT-27: an optional use says what is lost without it.
		{"optional without without", consumer, ", without: 能用，但发不了通知邮件 }", " }", CodeUseWithout, false},

		// CAT-LINT-33: one kind provided twice (which one is used, and the
		// report's fingerprint, would hang on their order).
		{"one kind provided twice", provider, "\ndelivery:\n", "\n  - kind: demo_token\n    values:\n      url: \"http://example.com\"\n      token: { ask: 在应用里建一个令牌填进来 }\n\ndelivery:\n", CodeProvideTwice, false},

		// CAT-LINT-34: "without" on a required use is never shown.
		{"without on a required use", consumer, "{ name: demo, kind: demo_token }", "{ name: demo, kind: demo_token, without: 用不了 }", CodeWithoutUnused, true},
		{"without on a use optional: false", consumer, "{ name: demo, kind: demo_token }", "{ name: demo, kind: demo_token, optional: false, without: 用不了 }", CodeWithoutUnused, true},

		// CAT-LINT-28: a variable taken from an optional use is optional too,
		// and nothing else takes from it.
		{"env from an optional use, not optional", consumer, "{ key: SMTP_HOST, from: uses.mail.host, optional: true }", "{ key: SMTP_HOST, from: uses.mail.host }", CodeEnvOptional, false},
		{"env from an optional use, optional: false", consumer, "{ key: SMTP_PORT, from: uses.mail.port, optional: true }", "{ key: SMTP_PORT, from: uses.mail.port, optional: false }", CodeEnvOptional, false},
		{"file template takes from an optional use", consumer, "url = {{uses.demo.url}}", "url = {{uses.demo.url}}\n      mail = {{uses.mail.host}}", CodeEnvOptional, false},
		{"first_login takes from an optional use", consumer, "delivery:\n", "first_login:\n  - { label: 邮箱, from: uses.mail.from }\n\ndelivery:\n", CodeEnvOptional, false},

		// CAT-LINT-29: retry only on a GET.
		{"retry on a POST", provider, "          json: { name: \"acornfox-{{consumer.name}}\", owner: \"{{consumer.id}}\" }\n", "          json: { name: \"acornfox-{{consumer.name}}\", owner: \"{{consumer.id}}\" }\n          retry: { times: 2, delay_s: 1 }\n", CodeStepRetry, false},
		{"retry on an init PATCH", provider, "      headers: { Authorization: \"Bearer {{saved.setup_token}}\" }\n", "      headers: { Authorization: \"Bearer {{saved.setup_token}}\" }\n      retry: { times: 1, delay_s: 0 }\n", CodeStepRetry, false},

		// CAT-LINT-06: references to things that do not exist.
		{"uses without an item", consumer, "url = {{uses.demo.url}}", "url = {{uses.demo}}", CodeReference, false},
		{"uses that is not declared", consumer, "from: uses.demo.url }", "from: uses.other.url }", CodeReference, false},
		{"provides names a missing endpoint", provider, "    endpoint: web\n    values:", "    endpoint: api\n    values:", CodeReference, false},
		{"step refers to a missing param", provider, "          json: { password: \"{{params.admin_password}}\" }\n          save: { session: data.session }\n", "          json: { password: \"{{params.root_password}}\" }\n          save: { session: data.session }\n", CodeReference, false},
		{"file template: capital in the kind", consumer, "url = {{uses.demo.url}}", "url = {{Uses.demo.url}}", CodeTemplate, false},
		{"file template: no kind", consumer, "url = {{uses.demo.url}}", "url = {{url}}", CodeNotRef, true},
		{"file template: a kind that does not exist", consumer, "url = {{uses.demo.url}}", "url = {{use.demo.url}}", CodeTemplate, false},
		{"file template: a volume as a value", consumer, "url = {{uses.demo.url}}", "url = {{volumes.data}}", CodeReference, false},
		{"first_login from that is no reference", consumer, "delivery:\n", "first_login:\n  - { label: 账号, from: admin }\n\ndelivery:\n", CodeReference, false},
		{"two uses with one name", consumer, "  - { name: demo, kind: demo_token }\n", "  - { name: demo, kind: demo_token }\n  - { name: demo, kind: smtp }\n", CodeReference, false},

		// CAT-LINT-16: platform variables in the new places.
		{"header refers to an unknown platform variable", provider, "headers: { Authorization: \"Bearer {{saved.setup_token}}\" }", "headers: { Authorization: \"Bearer {{saved.setup_token}}\", X-Zone: \"{{platform.zone}}\" }", CodePlatformVar, false},
		{"value refers to an unknown platform variable", provider, "/api/{{params.region_hint}}\"", "/api/{{platform.zone}}\"", CodePlatformVar, false},

		// CAT-LINT-02: what the schema refuses.
		{"value with both issued and ask", provider, "token: { issued: token }", "token: { issued: token, ask: 填一下 }", CodeSchema, false},
		{"value with secret only", provider, "token: { issued: token }", "token: { secret: true }", CodeSchema, false},
		{"value that is an empty template", provider, "      url: \"{{self.internal_url}}/api/{{params.region_hint}}\"\n", "      url: \"\"\n", CodeSchema, false},
		{"save with both path and list", provider, "session: { path: data.session, secret: true }", "session: { path: data.session, list: data.items, field: id }", CodeSchema, false},
		{"list save without field", provider, "where: { name: \"acornfox-{{consumer.name}}\" }, field: key, prefix: \"dt-\", secret: true }", "where: { name: x } }", CodeSchema, false},
		{"save path that is not a dotted path", provider, "setup_token: data.token", "setup_token: \"data[0].token\"", CodeSchema, false},
		{"pick other than first or last", provider, "pick: last", "pick: newest", CodeSchema, false},
		{"step with both http and exec", provider, "      - name: delete\n", "      - name: delete\n        exec: { command: [\"true\"] }\n", CodeSchema, false},
		{"step with neither", provider, "    check:\n", "    check:\n      - name: nothing\n", CodeSchema, false},
		{"unknown method", provider, "method: DELETE", "method: TRACE", CodeSchema, false},
		{"expect_status out of range", provider, "expect_status: [200, 404]", "expect_status: [99]", CodeSchema, false},
		{"exec without command", provider, "      - name: delete\n        http:\n          method: DELETE\n          path: \"/api/tokens/{{issued.token_id}}\"\n          headers: { X-Session: \"{{saved.session}}\" }\n          expect_status: [200, 404]\n", "      - name: delete\n        exec: { user: root }\n", CodeSchema, false},
		{"init step without when", provider, "  - when: after_healthy\n    http:\n      method: POST\n", "  - http:\n      method: POST\n", CodeSchema, false},
		{"when on a provided service's step", provider, "      - name: create\n", "      - name: create\n        when: after_healthy\n", CodeSchema, false},
		{"kind that is not a name", consumer, "kind: demo_token }", "kind: Demo-Token }", CodeSchema, false},
		{"without too long", consumer, "without: 能用，但发不了通知邮件", "without: " + long, CodeSchema, false},
		{"provides without kind", provider, "  - kind: demo_token\n    endpoint: web\n", "  - endpoint: web\n", CodeSchema, false},
		// The former ways of writing a service are gone from the format.
		{"adapter is gone", provider, "    endpoint: web\n    values:", "    endpoint: web\n    adapter: demo\n    values:", CodeSchema, false},
		{"base_path is gone", provider, "    endpoint: web\n    values:", "    endpoint: web\n    base_path: /v1\n    values:", CodeSchema, false},
		{"for_param is gone", consumer, "{ name: demo, kind: demo_token }", "{ name: demo, kind: demo_token, for_param: region_hint }", CodeSchema, false},
		{"env optional that is not a boolean", consumer, "from: uses.mail.host, optional: true", "from: uses.mail.host, optional: maybe", CodeSchema, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyServices(t)
			edit(t, filepath.Join(dir, filepath.FromSlash(c.file)), c.old, c.new)
			r, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			hits := problemsWith(r, c.want)
			if len(hits) == 0 {
				t.Fatalf("没报 %s：%+v", c.want, r.Problems)
			}
			for _, p := range hits {
				if (p.Level == LevelWarning) != c.warn {
					t.Errorf("%s 的级别是 %s", c.want, p.Level)
				}
				if p.File == "" || p.Message == "" {
					t.Errorf("问题要写明文件和原因：%+v", p)
				}
			}
			_, err = Build(dir, 1)
			if c.warn && (err != nil || r.Errors() != 0) {
				t.Fatalf("只是提醒，不该挡住打包：%v %+v", err, r.Problems)
			}
			if !c.warn && CodeOf(err) != CodeBuildLint {
				t.Fatalf("有错误时不能打包：%v", err)
			}
		})
	}
}

// One broken definition is reported once, at the definition, and not again
// for every manifest that names the kind.
func TestLintBrokenDefinitionReportedOnce(t *testing.T) {
	dir := copyServices(t)
	edit(t, filepath.Join(dir, "service-types", "demo_token.yaml"), "label: 样例令牌", "label: 样例令牌\nicon: x.svg")
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 1 || r.Problems[0].Code != CodeServiceType || r.Problems[0].File != "service-types/demo_token.yaml" {
		t.Fatalf("%+v", r.Problems)
	}
	if strings.Join(r.ServiceTypes, ",") != "llm_gateway,smtp" {
		t.Fatalf("读不了的定义不算在内：%v", r.ServiceTypes)
	}
	// Stray files in the directory.
	dir = copyServices(t)
	for name, content := range map[string]string{"redis.yml": "schema: 1\n", "README.md": "说明\n", ".draft.yaml": "x: [\n"} {
		if err := os.WriteFile(filepath.Join(dir, "service-types", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = Lint(dir)
	if len(r.Problems) != 1 || r.Problems[0].Code != CodeServiceType || r.Problems[0].File != "service-types/redis.yml" {
		t.Fatalf("只有 .yml 要报（README.md 和点开头的不管）：%+v", r.Problems)
	}
}

// A catalog without a service-types/ directory defines no service type: a
// manifest that needs or offers a service is refused there (no former
// catalog is exempt any more), the other checks run as always, and a catalog
// whose manifests name no service builds to an index without the key.
func TestLintCatalogWithoutServiceTypes(t *testing.T) {
	dir := copyServices(t)
	if err := os.RemoveAll(filepath.Join(dir, ServiceTypesDir)); err != nil {
		t.Fatal(err)
	}
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	kinds := problemsWith(r, CodeServiceKind)
	if len(kinds) == 0 || r.Errors() != len(kinds) || len(r.Problems) != len(kinds) {
		t.Fatalf("没有定义时每种用到的类型都报错：%+v", r.Problems)
	}
	files := map[string]bool{}
	for _, p := range kinds {
		files[p.File] = true
		if p.Level != LevelError || !strings.Contains(p.Message, "一个都没有") {
			t.Errorf("%+v", p)
		}
	}
	if !files["apps/demo-consumer/manifest.yaml"] || !files["apps/demo-provider/manifest.yaml"] {
		t.Fatalf("两份清单都要报：%+v", kinds)
	}
	if r.ServiceTypes == nil || len(r.ServiceTypes) != 0 {
		t.Fatalf("service_types 要是空列表：%#v", r.ServiceTypes)
	}
	if _, err := Build(dir, 1); CodeOf(err) != CodeBuildLint {
		t.Fatalf("不能打包：%v", err)
	}
	edit(t, filepath.Join(dir, "apps", "demo-provider", manifestName), "/api/tokens/{{issued.token_id}}", "/api/tokens/{{issued.id}}")
	r, _ = Lint(dir)
	if len(problemsWith(r, CodeStepRef)) != 1 {
		t.Fatalf("没有定义也照查步骤里的引用：%+v", r.Problems)
	}

	// An empty service-types/ is the same: every kind is undefined.
	dir = copyTree(t)
	for _, f := range listFiles(t, filepath.Join(dir, ServiceTypesDir)) {
		if err := os.Remove(f); err != nil {
			t.Fatal(err)
		}
	}
	r, _ = Lint(dir)
	if n := len(problemsWith(r, CodeServiceKind)); n != 2 || r.Errors() != 2 {
		t.Fatalf("空的 service-types/：两份清单各报一次，得到 %+v", r.Problems)
	}

	// A catalog whose manifests name no service at all needs no definitions.
	dir = copyServices(t)
	if err := os.RemoveAll(filepath.Join(dir, ServiceTypesDir)); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "apps", "demo-consumer")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "apps", "demo-provider", manifestName)
	raw, _ := os.ReadFile(file)
	start := strings.Index(string(raw), "provides:\n")
	end := strings.Index(string(raw), "delivery:\n")
	if start < 0 || end < start {
		t.Fatal("样例清单的结构变了")
	}
	if err := os.WriteFile(file, []byte(string(raw[:start])+string(raw[end:])), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, _ = Lint(dir); len(r.Problems) != 0 {
		t.Fatalf("没写 uses、provides 的目录不用定义：%+v", r.Problems)
	}
	b, err := Build(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b.Index, []byte("service_types")) {
		t.Fatal("没有 service-types/ 的目录，index.json 里不该出现 service_types")
	}
	idx, err := ParseIndex(b.Index)
	if err != nil || idx.ServiceTypes != nil || len(KnownServiceTypes(idx)) != 0 {
		t.Fatalf("%+v %v", idx.ServiceTypes, err)
	}
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

func TestReadServiceTypes(t *testing.T) {
	types, present, problems := ReadServiceTypes(servicesDir)
	if !present || len(problems) != 0 || len(types) != 3 {
		t.Fatalf("present=%v problems=%+v types=%d", present, problems, len(types))
	}
	if types[0].Kind != "demo_token" || types[1].Kind != "llm_gateway" || types[2].Kind != "smtp" {
		t.Fatalf("要按 kind 排序：%s %s %s", types[0].Kind, types[1].Kind, types[2].Kind)
	}
	smtp := types[2]
	if smtp.Schema != ServiceTypeSchema || smtp.Label != "邮箱（发信）" || smtp.Summary == "" {
		t.Fatalf("%+v", smtp)
	}
	if got := strings.Join(smtp.FieldKeys(), ","); got != "host,port,username,password,from,tls" {
		t.Fatalf("fields %s", got)
	}
	if got := strings.Join(smtp.RequiredKeys(), ","); got != "host" {
		t.Fatalf("required %s", got)
	}
	port, _ := smtp.Field("port")
	if s, ok := port.DefaultText(); port.Type != FieldInt || !ok || s != "587" {
		t.Fatalf("port %+v %q", port, s)
	}
	pw, _ := smtp.Field("password")
	if _, has := pw.DefaultText(); !pw.Secret || has || pw.Required {
		t.Fatalf("password %+v", pw)
	}
	tls, _ := smtp.Field("tls")
	if s, _ := tls.DefaultText(); tls.Type != FieldEnum || strings.Join(tls.Options, ",") != "starttls,tls,none" || s != "starttls" {
		t.Fatalf("tls %+v", tls)
	}
	if _, ok := smtp.Field("nope"); ok {
		t.Fatal("没有的项")
	}
	if smtp.Test == nil || smtp.Test.TCP == nil || smtp.Test.HTTP != nil || smtp.Test.TCP.Host != "{{host}}" || smtp.Test.TCP.Port != "{{port}}" ||
		strings.Join(smtp.TestRefs(), ",") != "host,port" {
		t.Fatalf("test %+v", smtp.Test)
	}
	llm := types[1]
	if llm.Test == nil || llm.Test.HTTP == nil || llm.Test.HTTP.URL != "{{base_url}}/models" || llm.Test.HTTP.Bearer != "{{api_key}}" ||
		len(llm.Test.HTTP.ExpectStatus) != 1 || llm.Test.HTTP.ExpectStatus[0] != 200 || strings.Join(llm.RequiredKeys(), ",") != "base_url,api_key" {
		t.Fatalf("llm_gateway %+v", llm)
	}
	if demo := types[0]; demo.Test != nil || len(demo.TestRefs()) != 0 {
		t.Fatalf("demo_token %+v", demo)
	}

	// No directory: not present, and never nil.
	types, present, problems = ReadServiceTypes(t.TempDir())
	if present || types == nil || len(types) != 0 || problems == nil || len(problems) != 0 {
		t.Fatalf("没有 service-types/ 的目录：%v %#v %#v", present, types, problems)
	}

	// One file checked by itself. A port written as a number is kept as text.
	good := []byte("schema: 1\nkind: imap\nlabel: 收信\nsummary: 收信服务器\nfields:\n  - { key: host, label: 地址, type: string, required: true }\ntest:\n  tcp: { host: \"{{host}}\", port: 993 }\n")
	if ps := ValidateServiceType(good, "imap"); len(ps) != 0 {
		t.Fatalf("%+v", ps)
	}
	if ps := ValidateServiceType(good, "pop3"); len(ps) != 1 || ps[0].Code != CodeServiceType || ps[0].File != "service-types/pop3.yaml" || ps[0].Path != "/kind" {
		t.Fatalf("名字对不上：%+v", ps)
	}
	if ps := ValidateServiceType([]byte("kind: imap\n"), "imap"); len(ps) == 0 || ps[0].Code != CodeServiceType {
		t.Fatalf("缺必填字段：%+v", ps)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ServiceTypesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ServiceTypesDir, "imap.yaml"), good, 0o644); err != nil {
		t.Fatal(err)
	}
	types, present, problems = ReadServiceTypes(dir)
	if !present || len(problems) != 0 || len(types) != 1 || types[0].Test.TCP.Port != "993" {
		t.Fatalf("%v %+v %+v", present, problems, types)
	}
}

// Service types travel inside index.json: built in, signed, synced, cached
// and read back the same.
func TestServiceTypesTravelWithTheIndex(t *testing.T) {
	want, _, _ := ReadServiceTypes(servicesDir)
	wantJSON, _ := json.Marshal(want)

	pub, priv := testKey(t)
	b, err := BuildFor(servicesDir, 7, PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b.Index)
	i, j := strings.Index(text, `"recipes": [`), strings.Index(text, `"service_types": [`)
	if i < 0 || j < i {
		t.Fatalf("index.json 里 service_types 要排在 recipes 后面：\n%s", text)
	}
	again, err := BuildFor(servicesDir, 7, PublicCatalog)
	if err != nil || !bytes.Equal(again.Index, b.Index) {
		t.Fatalf("同样的目录打两次字节要相同：%v", err)
	}
	idx, err := ParseIndex(b.Index)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(idx.ServiceTypes); !bytes.Equal(got, wantJSON) {
		t.Fatalf("打包再解析后的服务类型变了：\n%s\n要：\n%s", got, wantJSON)
	}
	if len(idx.ServiceTypes) != 3 || idx.ServiceTypes[0].Kind != "demo_token" || idx.ServiceTypes[2].Kind != "smtp" {
		t.Fatalf("要按 kind 排序：%+v", idx.ServiceTypes)
	}
	// The index also says what each app needs and offers.
	var consumer, provider IndexApp
	for _, a := range idx.Apps {
		switch a.ID {
		case "demo-consumer":
			consumer = a
		case "demo-provider":
			provider = a
		}
	}
	if len(consumer.Uses) != 2 || consumer.Uses[1] != (Use{Name: "mail", Kind: "smtp", Optional: true, Without: "能用，但发不了通知邮件"}) ||
		len(provider.Provides) != 1 || provider.Provides[0] != "demo_token" || consumer.MinPlatform != BasePlatform {
		t.Fatalf("consumer %+v provider %+v", consumer, provider)
	}

	// Published, verified, synced into a cache and loaded from it.
	b.Sign(priv)
	out := t.TempDir()
	if _, err := b.Write(out); err != nil {
		t.Fatal(err)
	}
	if res, err := VerifyDir(out, []ed25519.PublicKey{pub}); err != nil || res.Apps != 2 {
		t.Fatalf("verify %+v %v", res, err)
	}
	src := newSource(t, out)
	cache := filepath.Join(t.TempDir(), "cache")
	cfg := cfgFor(cache, pub, src)
	if res, err := Sync(context.Background(), cfg); err != nil || !res.Updated || res.Serial != 7 {
		t.Fatalf("sync %+v %v", res, err)
	}
	loaded, from, err := LoadWith(cache, nil, cfg.TrustedKeys)
	if err != nil || from != FromCache {
		t.Fatalf("load %s %v", from, err)
	}
	if got, _ := json.Marshal(loaded.ServiceTypes); !bytes.Equal(got, wantJSON) {
		t.Fatalf("同步、缓存后的服务类型变了：\n%s", got)
	}
	// A private catalog's index goes through StoreIndex and LoadIndex.
	pb, err := BuildFor(servicesDir, 2, "cat-demo")
	if err != nil {
		t.Fatal(err)
	}
	pb.Sign(priv)
	pcache := filepath.Join(t.TempDir(), "cat-demo")
	if _, _, err := StoreIndex(context.Background(), pcache, "cat-demo", pb.Index, pb.Sig, 2, cfg.TrustedKeys); err != nil {
		t.Fatal(err)
	}
	if pidx := LoadIndex(pcache, "cat-demo", cfg.TrustedKeys); pidx == nil || len(pidx.ServiceTypes) != 3 {
		t.Fatalf("私有目录的索引：%+v", pidx)
	}
	// And through a snapshot built into a program.
	snap := fstest.MapFS{IndexFile: {Data: b.Index}}
	sidx, from, err := LoadWith(t.TempDir(), snap, cfg.TrustedKeys)
	if err != nil || from != FromSnapshot || len(sidx.ServiceTypes) != 3 {
		t.Fatalf("快照：%s %v", from, err)
	}

	// Asking the index.
	smtp, ok := loaded.ServiceType("smtp")
	if !ok || smtp.Label != "邮箱（发信）" {
		t.Fatalf("%+v %v", smtp, ok)
	}
	port, _ := smtp.Field("port")
	if s, ok := port.DefaultText(); !ok || s != "587" {
		t.Fatalf("整数默认值从 index.json 读回来：%q %v（%#v）", s, ok, port.Default)
	}
	if _, ok := loaded.ServiceType("redis"); ok {
		t.Fatal("没定义的类型")
	}
	if _, ok := (*Index)(nil).ServiceType("smtp"); ok {
		t.Fatal("nil 索引")
	}
}

func TestKnownServiceTypes(t *testing.T) {
	public := &Index{ServiceTypes: []ServiceType{
		{Schema: 1, Kind: "smtp", Label: "公共目录的"},
		{Schema: 1, Kind: "llm_gateway"},
		{Schema: 2, Kind: "later", Label: "这个程序还不认的格式版本"},
	}}
	private := &Index{ServiceTypes: []ServiceType{
		{Schema: 1, Kind: "smtp", Label: "私有目录的"},
		{Schema: 1, Kind: "acme_ldap"},
	}}
	got := KnownServiceTypes(public, nil, private)
	var kinds []string
	for _, st := range got {
		kinds = append(kinds, st.Kind)
	}
	if strings.Join(kinds, ",") != "acme_ldap,llm_gateway,smtp" || got[2].Label != "公共目录的" {
		t.Fatalf("按 kind 排序、先给的目录优先、跳过不认的格式版本：%+v", got)
	}
	if _, ok := public.ServiceType("later"); ok {
		t.Fatal("不认的格式版本当作没有")
	}
	if got := KnownServiceTypes(); got == nil || len(got) != 0 {
		t.Fatalf("没有目录时给空列表：%#v", got)
	}
	// An index without service types gives an empty list, not nil.
	if got := KnownServiceTypes(&Index{}); got == nil || len(got) != 0 {
		t.Fatalf("没有服务类型的索引：%#v", got)
	}
	// The catalog built into this program has no apps yet, but the service
	// types of the public catalog.
	b, err := fs.ReadFile(Snapshot(), IndexFile)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndex(b)
	if err != nil {
		t.Fatal(err)
	}
	var snap []string
	for _, st := range KnownServiceTypes(idx) {
		snap = append(snap, st.Kind)
	}
	if strings.Join(snap, ",") != "llm_gateway,mysql,postgres,redis,smtp" {
		t.Fatalf("内置快照的服务类型：%v", snap)
	}
}

// Service types never keep an index from being read: a definition this
// program cannot use is left out and the rest of the catalog stays usable,
// so a later catalog (a newer form of the definitions) still syncs.
func TestParseIndexServiceTypes(t *testing.T) {
	index := func(types string) []byte {
		return []byte(`{"schema":1,"serial":4,"apps":[],"recipes":[],"service_types":` + types + `}`)
	}
	const smtp = `{"schema":1,"kind":"smtp","label":"邮箱","summary":"发信","fields":[{"key":"host","label":"地址","type":"string","hint":"问邮箱服务商"}],"test":{"tcp":{"host":"{{host}}","port":"587"}}}`
	cases := map[string]struct {
		types string
		want  string // the kinds that are kept, in order
	}{
		"only good ones":                    {`[` + smtp + `]`, "smtp"},
		"another format version":            {`[` + smtp + `,{"schema":3,"kind":"future","label":"以后的","summary":"x","fields":[{"key":"blob","label":"x","type":"binary"}],"extra":true}]`, "smtp"},
		"another version, another shape":    {`[{"schema":2,"kind":"future","fields":{"host":{"type":"string"}}},` + smtp + `]`, "smtp"},
		"fields in a shape of later":        {`[{"schema":1,"kind":"odd","fields":{"host":{"type":"string"}}},` + smtp + `]`, "smtp"},
		"a port written as a number":        {`[{"schema":1,"kind":"odd","fields":[{"key":"host"}],"test":{"tcp":{"host":"{{host}}","port":587}}},` + smtp + `]`, "smtp"},
		"an unknown field type is kept":     {`[{"schema":1,"kind":"blob","fields":[{"key":"data","type":"binary"}]},` + smtp + `]`, "blob,smtp"},
		"an unknown key is ignored":         {`[{"schema":1,"kind":"extra","fields":[{"key":"a","unit":"ms"}],"icon":"x.svg"}]`, "extra"},
		"kind is not a name":                {`[{"schema":1,"kind":"SMTP","fields":[]},` + smtp + `]`, "smtp"},
		"kind missing":                      {`[{"schema":1,"fields":[]},` + smtp + `]`, "smtp"},
		"kind twice: the first counts":      {`[` + smtp + `,{"schema":1,"kind":"smtp","label":"第二份","fields":[]}]`, "smtp"},
		"field key missing":                 {`[{"schema":1,"kind":"odd","fields":[{"label":"x","type":"string"}]},` + smtp + `]`, "smtp"},
		"field key twice":                   {`[{"schema":1,"kind":"odd","fields":[{"key":"a"},{"key":"a"}]},` + smtp + `]`, "smtp"},
		"an entry that is not an object":    {`["smtp",7,null,` + smtp + `]`, "smtp"},
		"schema missing":                    {`[{"kind":"odd","fields":[]},` + smtp + `]`, "smtp"},
		"schema that is not a number":       {`[{"schema":"1","kind":"odd","fields":[]},` + smtp + `]`, "smtp"},
		"not a list":                        {`{"smtp":{}}`, ""},
		"null":                              {`null`, ""},
		"a list with nothing this can read": {`[{"schema":9,"kind":"future"}]`, ""},
	}
	for name, c := range cases {
		idx, err := ParseIndex(index(c.types))
		if err != nil {
			t.Errorf("%s：服务类型不该让整个索引读不了：%v", name, err)
			continue
		}
		var kinds []string
		for _, st := range idx.ServiceTypes {
			kinds = append(kinds, st.Kind)
		}
		if got := strings.Join(kinds, ","); got != c.want {
			t.Errorf("%s：留下的类型是 %q，要 %q", name, got, c.want)
		}
		if known := KnownServiceTypes(idx); len(known) != len(kinds) {
			t.Errorf("%s：KnownServiceTypes 给了 %d 种，索引里留下 %d 种", name, len(known), len(kinds))
		}
		if st, ok := idx.ServiceType("smtp"); strings.Contains(c.want, "smtp") && (!ok || st.Label != "邮箱" || st.Fields[0].Hint != "问邮箱服务商" || st.Test.TCP.Port != "587") {
			t.Errorf("%s：smtp 读出来是 %+v %v", name, st, ok)
		}
	}
	// The apps of such an index are still there.
	later := `{"schema":1,"serial":4,"apps":[{"id":"demo","name":"样例","version":"v1","package":{"path":"apps/demo/demo-v1.tar.gz","sha256":"` +
		strings.Repeat("a", 64) + `","size":10}}],"recipes":[],"service_types":[{"schema":2,"kind":"future","fields":{}}]}`
	if idx, err := ParseIndex([]byte(later)); err != nil || len(idx.Apps) != 1 || len(idx.ServiceTypes) != 0 {
		t.Fatalf("以后的目录：应用照常，服务类型一个都不认：%v", err)
	}
	if idx, err := ParseIndex([]byte(`{"schema":1,"serial":4,"apps":[],"recipes":[]}`)); err != nil || idx.ServiceTypes != nil {
		t.Fatalf("没有 service_types 的索引照常：%v", err)
	}
	// What the build writes is read back whole: nothing is left out of a
	// catalog made for this program.
	types, _, _ := ReadServiceTypes(servicesDir)
	b, err := Build(servicesDir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if idx, err := ParseIndex(b.Index); err != nil || len(idx.ServiceTypes) != len(types) || len(types) != 3 {
		t.Fatalf("打包出来的服务类型要全部读得回来：%v", err)
	}
}

// The references of the new places are collected with where they are, so the
// variable report and the platform variable list see them.
func TestManifestRefsCoverServices(t *testing.T) {
	m, err := ParseManifest(readServicesManifest(t, "demo-provider"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range manifestRefs(m) {
		got = append(got, r.Ref+"@"+r.Where)
	}
	want := []string{
		"params.admin_password@/init/0",
		"saved.setup_token@/init/finish-setup",
		"self.internal_url@/provides/0/values/url",
		"params.region_hint@/provides/0/values/url",
		"params.admin_password@/provides/0/issue/login",
		"saved.session@/provides/0/issue/create",
		"consumer.name@/provides/0/issue/create",
		"consumer.id@/provides/0/issue/create",
		"saved.session@/provides/0/issue/read",
		"consumer.name@/provides/0/issue/read",
		"consumer.name@/provides/0/issue/read",
		"params.admin_password@/provides/0/revoke/login",
		"issued.token_id@/provides/0/revoke/delete",
		"saved.session@/provides/0/revoke/delete",
		"issued.token@/provides/0/check/0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("引用：\n%s\n要：\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// A platform variable in a header and in a provided value counts.
	m.Init[1].HTTP.Headers["X-TZ"] = "{{platform.tz}}"
	m.Provides[0].Values["url"] = ValueSource{Template: "{{platform.app.url}}"}
	if refs := strings.Join(PlatformRefs(m), ","); refs != "app.url,tz" {
		t.Fatalf("platform refs %s", refs)
	}
	// Rows of the variable report: the parameters are "used in" the steps.
	rows := ClassifyVars(m)
	var pw VarRow
	for _, r := range rows {
		if r.Name == "params.admin_password" {
			pw = r
		}
	}
	if strings.Join(pw.UsedIn, ",") != "/init/0,/provides/0/issue/login,/provides/0/revoke/login" {
		t.Fatalf("admin_password 用在：%v", pw.UsedIn)
	}
}

// TemplatePlaceholders sees every {{…}} with no brace inside and something
// written between the braces (references and everything else): for places
// that are no template, where any of them is a mistake.
func TestTemplatePlaceholders(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"plain text, { one brace }":           "",
		"{{params.a}}":                        "params.a",
		"a {{ params.a }} b {{saved.b}}/c":    "params.a,saved.b",
		"{{Params.A}} {{token}} {{save.x-y}}": "Params.A,token,save.x-y",
		"{{ two words }} {{}} {{ }}":          "two words",
		"{{{params.a}}}":                      "params.a",
		"{{$labels.instance}}":                "$labels.instance",
	}
	for tmpl, want := range cases {
		if got := strings.Join(TemplatePlaceholders(tmpl), ","); got != want {
			t.Errorf("TemplatePlaceholders(%q) = %q，要 %q", tmpl, got, want)
		}
	}
	// TemplateRefs keeps the ones shaped like a reference.
	if got := strings.Join(TemplateRefs("{{Params.A}} {{token}} {{params.a.b}} {{ saved.k }}"), ","); got != "params.a.b,saved.k" {
		t.Fatalf("TemplateRefs %q", got)
	}
}

// Text the engine leaves alone is no error: single braces pass silently, a
// {{…}} that is no reference is kept as written and only warned about
// (CAT-LINT-51). And every kind of reference passes where it exists.
func TestLintLeavesNonPlaceholdersAlone(t *testing.T) {
	dir := copyServices(t)
	edit(t, filepath.Join(dir, "apps", "demo-consumer", manifestName), "      url = {{uses.demo.url}}\n",
		"      url = {{uses.demo.url}}\n      note = {{ not a reference }} {one} { {two} } {{}}\n      tz = {{ platform.tz }}\n")
	edit(t, filepath.Join(dir, "apps", "demo-provider", manifestName), "          expect_json: { ok: true }\n",
		"          expect_json: { ok: true, hint: \"{{ free text }}\", region: \"{{params.region_hint}}\" }\n")
	r, err := Lint(dir)
	if err != nil || r.Errors() != 0 {
		t.Fatalf("%v %+v", err, r.Problems)
	}
	var warned []string
	for _, p := range r.Problems {
		if p.Code != CodeNotRef || p.Level != LevelWarning {
			t.Errorf("只该有 CAT-LINT-51 提醒：%+v", p)
		}
		warned = append(warned, p.Path)
	}
	sort.Strings(warned)
	if strings.Join(warned, ",") != "/files/demo.conf,/provides/0/issue/login" {
		t.Fatalf("提醒的地方：%v", warned)
	}
}

// The message for something that is no reference says what was written and
// what may be written there: a misspelt kind is an error (CAT-LINT-46), a
// kind used where it does not exist is 24 in a step, anything else is kept
// and warned about (CAT-LINT-51).
func TestLintUnknownPlaceholderMessages(t *testing.T) {
	dir := copyServices(t)
	edit(t, filepath.Join(dir, "apps", "demo-provider", manifestName),
		"          headers: { X-Session: \"{{saved.session}}\" }\n          json: { name:", "          headers: { X-Session: \"{{Saved.Session}}\" }\n          json: { name:")
	edit(t, filepath.Join(dir, "apps", "demo-provider", manifestName), "/api/tokens/{{issued.token_id}}", "/api/tokens/{{issue.token_id}}")
	edit(t, filepath.Join(dir, "apps", "demo-consumer", manifestName), "url = {{uses.demo.url}}", "url = {{demo_url}}")
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 3 {
		t.Fatalf("三处各报一条：%+v", r.Problems)
	}
	want := map[string][4]string{ // path → code, level, what was written, what may be written
		"/provides/0/issue/create":  {CodeTemplate, LevelError, "{{Saved.Session}}", "saved"},
		"/provides/0/revoke/delete": {CodeStepRef, LevelError, "issue.token_id", "issued.<键>"},
		"/files/demo.conf":          {CodeNotRef, LevelWarning, "{{demo_url}}", "uses.<名>.<项>"},
	}
	for _, p := range r.Problems {
		w, ok := want[p.Path]
		if !ok || p.Code != w[0] || p.Level != w[1] || !strings.Contains(p.Message, w[2]) || !strings.Contains(p.Message, w[3]) {
			t.Errorf("%+v", p)
		}
	}
}

// A step without a name is pointed at by its position counted from 0, the
// same in a problem's path and in the reasons of the lowest platform version.
func TestStepPositionsCountFromZero(t *testing.T) {
	dir := copyServices(t)
	file := filepath.Join(dir, "apps", "demo-provider", manifestName)
	edit(t, file, "      json: { password: \"{{params.admin_password}}\" }\n      expect_status: [200, 201]\n", "      json: { password: \"{{params.nope}}\" }\n      expect_status: [200, 201]\n")
	edit(t, file, "          headers: { Authorization: \"Bearer {{issued.token}}\" }", "          headers: { Authorization: \"Bearer {{issued.nope}}\" }")
	// A field of the first init step, lifted to a future version, makes a
	// reason that points at that step.
	withFeature(t, "manifest:/init/*/http/expect_status", "9.9.9")
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, p := range r.Problems {
		if p.Code != CodeMinPlatform {
			paths = append(paths, p.Code+"@"+p.Path)
		}
	}
	if got := strings.Join(paths, " "); got != CodeReference+"@/init/0 "+CodeStepRef+"@/provides/0/check/0" {
		t.Fatalf("问题的位置：%s", got)
	}
	found := false
	for _, reason := range r.MinPlatform.Apps["demo-provider"].Reasons {
		found = found || reason.Where == "/init/0" || strings.HasPrefix(reason.Where, "/init/0/")
		if strings.HasPrefix(reason.Where, "/init/1") {
			t.Errorf("第一个 init 步骤被数成了 1：%+v", reason)
		}
	}
	if !found {
		t.Fatalf("最低平台版本的原因里也要按 /init/0 指第一个步骤：%+v", r.MinPlatform.Apps["demo-provider"].Reasons)
	}
	if (Step{}).Label(0) != "0" || (Step{Name: "login"}).Label(3) != "login" {
		t.Fatal("Label")
	}
}

// Two uses with one name: reported once, and references go by the first.
func TestLintUsesNamedTwice(t *testing.T) {
	dir := copyServices(t)
	edit(t, filepath.Join(dir, "apps", "demo-consumer", manifestName), "  - { name: demo, kind: demo_token }\n", "  - { name: demo, kind: demo_token }\n  - { name: demo, kind: smtp }\n")
	r, err := Lint(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Problems) != 1 || r.Problems[0].Code != CodeReference || r.Problems[0].Path != "/uses/1" || !strings.Contains(r.Problems[0].Message, "demo") {
		t.Fatalf("只报名字重复这一条，不连带报项对不上：%+v", r.Problems)
	}
}

// CAT-LINT-28 is about env entries taken from an optional use only.
func TestLintEnvOfOptionalUse(t *testing.T) {
	dir := copyServices(t)
	file := filepath.Join(dir, "apps", "demo-consumer", manifestName)
	// A required use needs no optional; optional on it is allowed as well.
	edit(t, file, "{ key: DEMO_URL, from: uses.demo.url }", "{ key: DEMO_URL, from: uses.demo.url, optional: true }")
	if r, _ := Lint(dir); len(r.Problems) != 0 {
		t.Fatalf("%+v", r.Problems)
	}
	edit(t, file, "{ key: SMTP_HOST, from: uses.mail.host, optional: true }", "{ key: SMTP_HOST, from: uses.mail.host }")
	r, _ := Lint(dir)
	if len(r.Problems) != 1 || r.Problems[0].Code != CodeEnvOptional || r.Problems[0].Path != "/env/SMTP_HOST" ||
		!strings.Contains(r.Problems[0].Message, "uses.mail") || !strings.Contains(r.Problems[0].Message, "optional: true") {
		t.Fatalf("%+v", r.Problems)
	}
	// Once the use is required the entry is fine as it is.
	edit(t, file, "kind: smtp, optional: true, without: 能用，但发不了通知邮件 }", "kind: smtp }")
	edit(t, file, "{ key: SMTP_PORT, from: uses.mail.port, optional: true }", "{ key: SMTP_PORT, from: uses.mail.port }")
	if r, _ := Lint(dir); len(r.Problems) != 0 {
		t.Fatalf("%+v", r.Problems)
	}
}

// A field of a service type may carry a hint; it travels with the index.
func TestServiceFieldHint(t *testing.T) {
	raw := []byte("schema: 1\nkind: imap\nlabel: 收信\nsummary: 收信服务器\nfields:\n  - { key: host, label: 地址, type: string, required: true, hint: 问邮箱服务商要 }\n  - { key: site, label: 网页, type: url, default: \"https://mail.example.com\" }\n")
	if ps := ValidateServiceType(raw, "imap"); len(ps) != 0 {
		t.Fatalf("%+v", ps)
	}
	dir := copyServices(t)
	if err := os.WriteFile(filepath.Join(dir, ServiceTypesDir, "imap.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Build(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := ParseIndex(b.Index)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := idx.ServiceType("imap")
	if !ok || st.Fields[0].Hint != "问邮箱服务商要" || st.Fields[1].Hint != "" {
		t.Fatalf("%+v %v", st, ok)
	}
	if strings.Count(string(b.Index), `"hint"`) != 1 {
		t.Fatal("没写 hint 的项，index.json 里不出现这个键")
	}
}

// Dedicated uses are a matching rule (take only a service no other consumer
// is connected to), part of the format since BasePlatform: they raise no
// version, this platform carries them out (nothing is pending, lint does
// not warn) and the catalog builds, written true or false.
func TestDedicatedIsBasePlatform(t *testing.T) {
	consumer := string(readServicesManifest(t, "demo-consumer"))
	hermes, err := os.ReadFile(filepath.Join("testdata", "good", "apps", "hermes-agent", manifestName))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, file, raw, old, new, where string
	}{
		{"dedicated use", "demo-consumer", consumer, "{ name: demo, kind: demo_token }", "{ name: demo, kind: demo_token, dedicated: true }", ""},
		{"dedicated use (Hermes)", "", string(hermes), "{ name: llm, kind: llm_gateway }", "{ name: llm, kind: llm_gateway, dedicated: true }", ""},
		{"dedicated written false", "demo-consumer", consumer, "{ name: demo, kind: demo_token }", "{ name: demo, kind: demo_token, dedicated: false }", ""},
	}
	for _, c := range cases {
		if !strings.Contains(c.raw, c.old) {
			t.Fatalf("%s：清单里没有 %q", c.name, c.old)
		}
		raw := strings.Replace(c.raw, c.old, c.new, 1)
		mp, err := ManifestMinPlatform([]byte(raw))
		if err != nil || mp.Version != BasePlatform || len(mp.Reasons) != 0 {
			t.Errorf("%s：要 %s，算出来 %+v（%v）", c.name, BasePlatform, mp, err)
		}
		m, err := ParseManifest([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		p := PendingFeatures(m)
		switch {
		case c.where == "" && len(p) != 0:
			t.Errorf("%s：不该有：%+v", c.name, p)
		case c.where != "" && (len(p) != 1 || p[0].Where != c.where || !strings.Contains(p[0].What, "dedicated")):
			t.Errorf("%s：%+v", c.name, p)
		}
		if c.file == "" {
			continue
		}
		dir := copyServices(t)
		edit(t, filepath.Join(dir, "apps", c.file, manifestName), c.old, c.new)
		r, _ := Lint(dir)
		hits := problemsWith(r, CodeNotYet)
		if r.Errors() != 0 || len(problemsWith(r, CodeMinPlatform)) != 0 {
			t.Errorf("%s：不该有错误：%+v", c.name, r.Problems)
		}
		if c.where == "" && len(hits) != 0 || c.where != "" && (len(hits) != 1 || hits[0].Path != c.where || hits[0].Level != LevelWarning || !strings.Contains(hits[0].Message, "CAT-INST-23")) {
			t.Errorf("%s：只该在 %q 提醒 %s：%+v", c.name, c.where, CodeNotYet, r.Problems)
		}
		if _, err := Build(dir, 1); err != nil {
			t.Errorf("%s：只有提醒，能打包：%v", c.name, err)
		}
	}
}

// An exec step is part of the format since BasePlatform: it raises no
// version, and this platform runs it: nothing is pending, lint does not
// warn, and the catalog builds.
func TestExecStepIsBasePlatform(t *testing.T) {
	provider := string(readServicesManifest(t, "demo-provider"))
	whoami := "      - http:\n          method: GET\n          path: /api/whoami\n          headers: { Authorization: \"Bearer {{issued.token}}\" }\n          expect_status: [200]\n"
	cases := []struct{ name, old, new, where string }{
		{"exec step in check", whoami, "      - exec:\n          command: [\"check-token\"]\n          env: { TOKEN: \"{{issued.token}}\" }\n", "/provides/0/check/0/exec"},
		{"exec step in init", "  - name: finish-setup\n    when: after_healthy\n    http:\n      method: PATCH\n      path: /api/setup/finish\n      headers: { Authorization: \"Bearer {{saved.setup_token}}\" }\n", "  - name: finish-setup\n    when: after_healthy\n    exec: { command: [\"finish\"], stdin: \"{{saved.setup_token}}\" }\n", "/init/finish-setup/exec"},
	}
	for _, c := range cases {
		if !strings.Contains(provider, c.old) {
			t.Fatalf("%s：清单里没有 %q", c.name, c.old)
		}
		raw := strings.Replace(provider, c.old, c.new, 1)
		mp, err := ManifestMinPlatform([]byte(raw))
		if err != nil || mp.Version != BasePlatform || len(mp.Reasons) != 0 {
			t.Errorf("%s：要 %s，算出来 %+v（%v）", c.name, BasePlatform, mp, err)
		}
		m, err := ParseManifest([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if p := PendingFeatures(m); len(p) != 0 {
			t.Errorf("%s：%+v", c.name, p)
		}
		dir := copyServices(t)
		edit(t, filepath.Join(dir, "apps", "demo-provider", manifestName), c.old, c.new)
		r, _ := Lint(dir)
		if r.Errors() != 0 || len(problemsWith(r, CodeNotYet)) != 0 {
			t.Errorf("%s：%s 不该再提醒：%+v", c.name, c.where, r.Problems)
		}
		if _, err := Build(dir, 1); err != nil {
			t.Errorf("%s：能打包：%v", c.name, err)
		}
	}
}

// A GET step may retry, and revoke and check read the issue number back as
// issued.issue_id: a clean catalog, needing 0.6.0 and no more.
func TestLintRetryAndIssueNumber(t *testing.T) {
	dir := copyServices(t)
	path := filepath.Join(dir, "apps", "demo-provider", manifestName)
	edit(t, path, "          path: /api/whoami\n", "          path: /api/whoami\n          retry: { times: 3, delay_s: 2 }\n")
	edit(t, path, "owner: \"{{consumer.id}}\" }", "owner: \"{{consumer.id}}\", tag: \"{{issue.id}}\" }")
	edit(t, path, "path: \"/api/tokens/{{issued.token_id}}\"", "path: \"/api/tokens/{{issued.token_id}}?tag={{issued.issue_id}}\"")
	r, err := Lint(dir)
	if err != nil || len(r.Problems) != 0 || r.MinPlatform.Apps["demo-provider"].Computed != v060 {
		t.Fatalf("%v %+v %+v", err, r.Problems, r.MinPlatform.Apps["demo-provider"])
	}
	// The bounds are the schema's.
	for _, retry := range []string{"{ times: 11, delay_s: 1 }", "{ times: 0, delay_s: 1 }", "{ times: 2, delay_s: 31 }", "{ times: 2 }", "{ times: 2, delay_s: 1, backoff: 2 }"} {
		dir := copyServices(t)
		edit(t, filepath.Join(dir, "apps", "demo-provider", manifestName), "          path: /api/whoami\n", "          path: /api/whoami\n          retry: "+retry+"\n")
		if r, _ := Lint(dir); len(problemsWith(r, CodeSchema)) == 0 {
			t.Errorf("retry %s：要报 %s：%+v", retry, CodeSchema, r.Problems)
		}
	}
}

// finish_if ends a revoke list as a success; anywhere else it is refused
// (CAT-LINT-30). A revoke step with it is a clean catalog needing 0.6.0.
func TestLintFinishIfOnlyInRevoke(t *testing.T) {
	dir := copyServices(t)
	path := filepath.Join(dir, "apps", "demo-provider", manifestName)
	edit(t, path, "          expect_status: [200, 404]\n", "          finish_if: { status: [404], json: { gone: \"{{issued.token_id}}\" } }\n")
	r, err := Lint(dir)
	if err != nil || len(r.Problems) != 0 || r.MinPlatform.Apps["demo-provider"].Computed != v060 {
		t.Fatalf("revoke 里写 finish_if 是对的：%v %+v %+v", err, r.Problems, r.MinPlatform.Apps["demo-provider"])
	}
	m, err := ParseManifest(mustRead(t, path))
	if err != nil {
		t.Fatal(err)
	}
	fi := m.Provides[0].Revoke[1].HTTP.FinishIf
	if !fi.Written() || len(fi.Status) != 1 || fi.Status[0] != 404 || fi.JSON["gone"] != "{{issued.token_id}}" {
		t.Fatalf("finish_if %+v", fi)
	}
	for _, c := range []struct{ name, old, new, where string }{
		{"in check", "          path: /api/whoami\n", "          path: /api/whoami\n          finish_if: { status: [401] }\n", "/provides/0/check/0/http/finish_if"},
		{"in issue", "          json: { name: \"acornfox-{{consumer.name}}\", owner: \"{{consumer.id}}\" }\n", "          json: { name: \"acornfox-{{consumer.name}}\", owner: \"{{consumer.id}}\" }\n          finish_if: { status: [409] }\n", "/provides/0/issue/create/http/finish_if"},
		{"in init", "      path: /api/setup/finish\n", "      path: /api/setup/finish\n      finish_if: { json: { done: true } }\n", "/init/finish-setup/http/finish_if"},
	} {
		dir := copyServices(t)
		edit(t, filepath.Join(dir, "apps", "demo-provider", manifestName), c.old, c.new)
		r, _ := Lint(dir)
		hits := problemsWith(r, CodeFinishIf)
		if len(hits) != 1 || hits[0].Path != c.where || hits[0].Level != LevelError {
			t.Errorf("%s：要在 %s 报 %s：%+v", c.name, c.where, CodeFinishIf, r.Problems)
		}
	}
	// It says something: an empty one is the schema's error.
	dir = copyServices(t)
	edit(t, filepath.Join(dir, "apps", "demo-provider", manifestName), "          expect_status: [200, 404]\n", "          finish_if: {}\n")
	if r, _ := Lint(dir); len(problemsWith(r, CodeSchema)) == 0 {
		t.Errorf("空的 finish_if 要报 %s：%+v", CodeSchema, r.Problems)
	}
}

// A config file written only when it is not there yet must not take what a
// service hands out: it would keep the old value (CAT-LINT-31). Written on
// every install, the same template is fine.
func TestLintFileNeverRefersToUses(t *testing.T) {
	dir := copyServices(t)
	path := filepath.Join(dir, "apps", "demo-consumer", manifestName)
	edit(t, path, "    path: demo.conf\n", "    path: demo.conf\n    overwrite: never\n")
	r, _ := Lint(dir)
	hits := problemsWith(r, CodeFileUses)
	if len(hits) != 1 || hits[0].Path != "/files/demo.conf" || !strings.Contains(hits[0].Message, "uses.demo.url") {
		t.Fatalf("%+v", r.Problems)
	}
	dir = copyServices(t)
	edit(t, filepath.Join(dir, "apps", "demo-consumer", manifestName), "    path: demo.conf\n", "    path: demo.conf\n    overwrite: install\n")
	if r, err := Lint(dir); err != nil || len(r.Problems) != 0 {
		t.Fatalf("overwrite: install 可以引用：%v %+v", err, r.Problems)
	}
}

// A provides entry that hands out a credential per consumer must be able to
// take it back (CAT-LINT-32), and its steps can only go to the primary
// endpoint, the one the platform reaches from this server (CAT-LINT-08).
// Without steps, another endpoint is fine: apps reach any port over the
// app network.
func TestLintProvidesRevokeAndEndpoint(t *testing.T) {
	dir := copyServices(t)
	path := filepath.Join(dir, "apps", "demo-provider", manifestName)
	text := string(mustRead(t, path))
	from, to := strings.Index(text, "    revoke:\n"), strings.Index(text, "    check:\n")
	if from < 0 || to < from {
		t.Fatal("the revoke steps are not in the material")
	}
	if err := os.WriteFile(path, []byte(text[:from]+text[to:]), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := Lint(dir)
	if hits := problemsWith(r, CodeNoRevoke); len(hits) != 1 || hits[0].Path != "/provides/0" || hits[0].Level != LevelError || !strings.Contains(hits[0].Message, "revoke") {
		t.Fatalf("issue without revoke: %+v", r.Problems)
	}

	const second = "  - name: admin\n    port: 9000\n    protocol: http\n\nparams:\n"
	dir = copyServices(t)
	path = filepath.Join(dir, "apps", "demo-provider", manifestName)
	edit(t, path, "\nparams:\n", "\n"+second)
	edit(t, path, "    endpoint: web\n", "    endpoint: admin\n")
	r, _ = Lint(dir)
	if hits := problemsWith(r, CodeEndpoint); len(hits) != 1 || hits[0].Path != "/provides/0/endpoint" || !strings.Contains(hits[0].Message, "admin") {
		t.Fatalf("steps to a non-primary endpoint: %+v", r.Problems)
	}
	// The same entry without steps (values only) may name it.
	dir = copyServices(t)
	path = filepath.Join(dir, "apps", "demo-provider", manifestName)
	text = string(mustRead(t, path))
	from, to = strings.Index(text, "    issue:\n"), strings.Index(text, "\ndelivery:")
	if from < 0 || to < from {
		t.Fatal("the steps are not in the material")
	}
	text = text[:from] + text[to:]
	text = strings.Replace(text, "      token: { issued: token }\n", "      token: { ask: 在应用里建一个令牌填进来 }\n", 1)
	text = strings.Replace(text, "\nparams:\n", "\n"+second, 1)
	text = strings.Replace(text, "    endpoint: web\n", "    endpoint: admin\n", 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ = Lint(dir)
	if hits := append(problemsWith(r, CodeEndpoint), problemsWith(r, CodeNoRevoke)...); len(hits) != 0 {
		t.Fatalf("values only, another endpoint: %+v", r.Problems)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// UsedItems are the items of one need a manifest refers to, wherever it
// does (env and file templates in demo-consumer), and nothing of another
// need or of a need it does not have.
func TestUsedItems(t *testing.T) {
	m, err := ParseManifest(readServicesManifest(t, "demo-consumer"))
	if err != nil {
		t.Fatal(err)
	}
	if got := UsedItems(m, "demo"); len(got) != 2 || !got["url"] || !got["token"] {
		t.Fatalf("demo: %v", got)
	}
	if got := UsedItems(m, "mail"); len(got) != 2 || !got["host"] || !got["port"] {
		t.Fatalf("mail: %v", got)
	}
	if got := UsedItems(m, "llm"); len(got) != 0 {
		t.Fatalf("llm: %v", got)
	}
	if got := UsedItems(nil, "demo"); len(got) != 0 {
		t.Fatalf("nil manifest: %v", got)
	}
}
