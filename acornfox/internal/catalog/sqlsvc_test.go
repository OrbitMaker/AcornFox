package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// sqlsvcDir is the test catalog of a database-shaped service: a provider
// whose primary endpoint is tcp and internal, that hands out per consumer
// what commands run in its container make (issue_vars, exec steps), and a
// consumer that takes each item.
var sqlsvcDir = filepath.Join("testdata", "sqlsvc")

const (
	sqlProvider = "apps/demo-sql/manifest.yaml"
	sqlConsumer = "apps/demo-sql-user/manifest.yaml"
)

// copySqlsvc copies the database-shaped test catalog so a test can break it.
func copySqlsvc(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(sqlsvcDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(sqlsvcDir, p)
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

func readSqlsvc(t *testing.T, rel string) *Manifest {
	t.Helper()
	m, err := ParseManifest(mustRead(t, filepath.Join(sqlsvcDir, filepath.FromSlash(rel))))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The material uses every new way of writing of the first round and is
// clean: no error, no warning, the lowest platform version is BasePlatform.
// Everything it writes is carried out now: command steps, issue_vars and
// issued.* in a value template (the step executor), the tcp endpoints, the
// exec and tcp health checks and the internal exposure (internalnet.go). So
// lint has no CAT-LINT-26 warning and PendingFeatures is empty for both
// manifests. (The second round, sqlsolo, is carried out as well.)
func TestLintDatabaseShapedCatalog(t *testing.T) {
	r, err := Lint(sqlsvcDir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Errors() != 0 || strings.Join(r.Apps, ",") != "demo-sql,demo-sql-user" || strings.Join(r.ServiceTypes, ",") != "demo_sql" {
		t.Fatalf("%v %v %+v", r.Apps, r.ServiceTypes, r.Problems)
	}
	if len(r.Problems) != 0 {
		t.Fatalf("不该有任何提醒：%+v", r.Problems)
	}
	for _, id := range r.Apps {
		if info := r.MinPlatform.Apps[id]; info.Computed != BasePlatform || len(info.Reasons) != 0 {
			t.Errorf("%s：新写法都从 %s 起有：%+v", id, BasePlatform, info)
		}
	}
	if _, err := Build(sqlsvcDir, 1); err != nil {
		t.Fatalf("能打包：%v", err)
	}
	for _, file := range []string{sqlConsumer, sqlProvider} {
		if p := PendingFeatures(readSqlsvc(t, file)); len(p) != 0 {
			t.Fatalf("%s：%+v", file, p)
		}
	}
	good := readServicesManifest(t, "demo-provider")
	m, err := ParseManifest(good)
	if err != nil || len(PendingFeatures(m)) != 0 || len(PendingFeatures(nil)) != 0 {
		t.Fatalf("现在能执行的清单不该有：%v %+v", err, PendingFeatures(m))
	}
}

// The new fields decode into the structs the engine will read.
func TestParseDatabaseShapedManifest(t *testing.T) {
	m := readSqlsvc(t, sqlProvider)
	db, stats := m.Endpoints[0], m.Endpoints[1]
	if db.Protocol != ProtocolTCP || db.Health == nil || strings.Join(db.Health.Exec, " ") != "demo-sql-ping --host 127.0.0.1 --port 5000" ||
		db.Health.Path != "" || db.Health.TCP || db.Health.TimeoutS != 120 {
		t.Fatalf("db %+v %+v", db, db.Health)
	}
	if stats.Protocol != ProtocolTCP || stats.Health == nil || !stats.Health.TCP || len(stats.Health.Exec) != 0 {
		t.Fatalf("stats %+v %+v", stats, stats.Health)
	}
	if m.Exposure.Default != ExposureInternal {
		t.Fatalf("exposure %+v", m.Exposure)
	}
	p := m.Provides[0]
	if strings.Join(p.IssueVarKeys(), ",") != "database,password,username" {
		t.Fatalf("issue_vars %+v", p.IssueVars)
	}
	if v := p.IssueVars["database"]; v.Template != "af_{{issue.id}}" || v.Generated() {
		t.Fatalf("database %+v", v)
	}
	if v := p.IssueVars["password"]; !v.Generated() || v.Generate.Length != 32 || v.Generate.Charset != "alnum" || !v.Secret || v.Template != "" {
		t.Fatalf("password %+v", v)
	}
	if got := strings.Join(p.IssuedKeys(), ","); got != "database,database_oid,password,server_line,username" {
		t.Fatalf("IssuedKeys %s", got)
	}
	secretParams := func(ref string) bool { return ref == "params.admin_password" }
	for k, want := range map[string]bool{"password": true, "database": false, "username": false, "database_oid": false, "server_line": false, "nope": false} {
		if got := p.IssuedSecret(k, secretParams, nil); got != want {
			t.Errorf("IssuedSecret(%s) = %v", k, got)
		}
	}
	url := p.Values["url"]
	if url.Kind() != ValueTemplate || !url.Secret || !url.PerConsumer() || strings.Join(url.IssuedRefs(), ",") != "username,password,database" {
		t.Fatalf("url %+v %v", url, url.IssuedRefs())
	}
	if host := p.Values["host"]; host.Kind() != ValueTemplate || host.Secret || host.PerConsumer() || host.IssuedRefs() != nil {
		t.Fatalf("host %+v", host)
	}
	if pw := p.Values["password"]; pw.Kind() != ValueIssued || !pw.PerConsumer() || pw.IssuedRefs() != nil {
		t.Fatalf("password %+v", pw)
	}

	create := p.Issue[0].Exec
	if create == nil || create.User != "demo" || create.Env["DEMO_SQL_PASSWORD"] != "{{params.admin_password}}" ||
		!strings.Contains(create.Stdin, `PASSWORD '{{issue.password}}'`) || create.Timeout() != 60 ||
		!create.ExitOK(0) || create.ExitOK(1) || strings.Join(create.EnvKeys(), ",") != "DEMO_SQL_PASSWORD" {
		t.Fatalf("create %+v", create)
	}
	describe := p.Issue[1]
	if describe.Exec.Timeout() != DefaultExecTimeoutS || describe.Exec.Save["database_oid"].Path != "data.oid" ||
		!describe.Exec.Save["server_line"].FromStdout() || describe.Exec.Save["database_oid"].FromStdout() ||
		strings.Join(describe.SaveKeys(), ",") != "database_oid,server_line" || len(describe.HTTP.Save) != 0 {
		t.Fatalf("describe %+v", describe.Exec)
	}
	lock := p.Revoke[0]
	if !lock.FinishIfWritten() || !reflect.DeepEqual(lock.Exec.FinishIf.Exit, []int{3}) || !lock.Exec.ExitOK(0) || lock.Exec.ExitOK(3) {
		t.Fatalf("lock %+v", lock.Exec)
	}
	if p.Check[0].FinishIfWritten() || (Step{HTTP: StepHTTP{FinishIf: &StepFinishIf{Status: []int{404}}}}).FinishIfWritten() != true {
		t.Fatal("FinishIfWritten")
	}
	if MaxExecTimeoutS != 300 || MaxExecOutput != 64<<10 {
		t.Fatal("bounds")
	}

	// Through JSON and back (the engine stores manifests as JSON): the forms
	// stay as written, secret marks included.
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"database":"af_{{issue.id}}"`, `"password":{"generate":{"length":32,"charset":"alnum"},"secret":true}`,
		`"url":{"template":"demo-sql://`, `"host":"{{self.host}}"`, `"server_line":{"stdout":true}`, `"finish_if":{"exit":[3]}`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON 里没有 %s：%s", want, b)
		}
	}
	var back Provide
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back, p) {
		t.Fatalf("来回一趟变了：%v\n%+v\n%+v", err, back, p)
	}
	// Decoding is strict: a misspelt key of an issue variable is an error.
	var iv IssueVar
	if err := json.Unmarshal([]byte(`{"generate":{"length":20},"secert":true}`), &iv); err == nil {
		t.Fatal("拼错的字段要报错")
	}
	if _, err := ParseManifest([]byte("provides:\n  - kind: x\n    issue_vars:\n      a: { generate: { length: 20 }, secert: true }\n")); err == nil {
		t.Fatal("YAML 里拼错的字段要报错")
	}
	if _, err := ParseManifest([]byte("provides:\n  - kind: x\n    issue_vars:\n      a: [1]\n")); err == nil {
		t.Fatal("列表不是 issue_vars 的写法")
	}
}

// The checks of the new ways of writing, one broken catalog per case. All of
// them are errors: the catalog does not build.
func TestLintDatabaseShapedProblems(t *testing.T) {
	const create = "          command: [demo-sql, --admin, --on-error-stop]\n          env: { DEMO_SQL_PASSWORD: \"{{params.admin_password}}\" }\n          stdin: |\n            CREATE ROLE"
	cases := []struct {
		name, file, old, new, want, path string
	}{
		// CAT-LINT-35: no secret in the argument list.
		{"secret param in a command", sqlProvider, "      command: [demo-sql, --admin, --on-error-stop]\n      env:", "      command: [demo-sql, --admin, --password, \"{{params.admin_password}}\"]\n      env:", CodeExecSecret, "/init/quiet-log/exec/command/3"},
		{"generated issue var in a command", sqlProvider, create, "          command: [demo-sql, --admin, --new-password, \"{{issue.password}}\"]\n          env: { DEMO_SQL_PASSWORD: \"{{params.admin_password}}\" }\n          stdin: |\n            CREATE ROLE", CodeExecSecret, "/provides/0/issue/create/exec/command/3"},
		{"issued secret in a check command", sqlProvider, "--oid, \"{{issued.database_oid}}\", --ping]", "--oid, \"{{issued.database_oid}}\", --password, \"{{issued.password}}\"]", CodeExecSecret, "/provides/0/check/connect/exec/command/10"},
		{"secret param as the user", sqlProvider, "      user: demo\n      command: [demo-sql, --admin, --on-error-stop]\n      env:", "      user: \"{{params.admin_password}}\"\n      command: [demo-sql, --admin, --on-error-stop]\n      env:", CodeExecSecret, "/init/quiet-log/exec/user"},
		{"secret item of a service in a command", sqlConsumer, "--user, \"{{uses.db.username}}\"]", "--password, \"{{uses.db.password}}\"]", CodeExecSecret, "/init/migrate/exec/command/5"},
		{"secret save not used in a command", sqlProvider, "            server_line: { stdout: true }\n", "            server_line: { stdout: true, secret: true }\n", "", ""}, // no use of it in a command: fine
		// An issue variable made from a secret is secret, here and once issued.
		{"secret template issue var in a command", sqlProvider, "      username: \"af_{{issue.id}}\"\n", "      username: \"af_{{params.admin_password}}\"\n", CodeExecSecret, "/provides/0/check/connect/exec/command/4"},

		// CAT-LINT-36: one item is one argument.
		{"line break in a command item", sqlProvider, "--describe]", "\"--describe\\nmore\"]", CodeExecNewline, "/provides/0/issue/describe/exec/command/5"},
		{"line break in the user", sqlProvider, "      user: demo\n      command: [demo-sql, --admin, --on-error-stop]\n      env:", "      user: \"demo\\nroot\"\n      command: [demo-sql, --admin, --on-error-stop]\n      env:", CodeExecNewline, "/init/quiet-log/exec/user"},
		{"line break in a health check's exec", sqlProvider, "--port, \"5000\"]", "--port, \"5000\\n\"]", CodeExecNewline, "/endpoints/db/health/exec/4"},

		// CAT-LINT-40: an item made from a secret value says so.
		{"secret param in an unmarked value", sqlProvider, "      host: \"{{self.host}}\"\n", "      host: \"{{self.host}}-{{params.admin_password}}\"\n", CodeValueSecret, "/provides/0/values/host"},
		{"secret issued value in an unmarked value", sqlProvider, "      database: { issued: database }\n", "      database: \"{{issued.password}}\"\n", CodeValueSecret, "/provides/0/values/database"},
		{"secret param in a value marked secret", sqlProvider, "      host: \"{{self.host}}\"\n", "      host: { template: \"{{self.host}}-{{params.admin_password}}\", secret: true }\n", "", ""},
		{"secret value in an item the service type marks secret", sqlProvider, "      url: { template: \"demo-sql://{{issued.username}}:{{issued.password}}@{{self.host}}:{{self.port}}/{{issued.database}}\", secret: true }\n", "      url: \"demo-sql://{{issued.username}}:{{issued.password}}@{{self.host}}:{{self.port}}/{{issued.database}}\"\n", "", ""},
		{"plain issued value in an unmarked value", sqlProvider, "      database: { issued: database }\n", "      database: \"{{issued.database}}\"\n", "", ""},

		// CAT-LINT-37: issue_vars that cannot work.
		{"issue var named id", sqlProvider, "      database: \"af_{{issue.id}}\"\n", "      database: \"af_{{issue.id}}\"\n      id: x\n", CodeIssueVars, "/provides/0/issue_vars/id"},
		{"issue var named issue_id", sqlProvider, "      database: \"af_{{issue.id}}\"\n", "      database: \"af_{{issue.id}}\"\n      issue_id: x\n", CodeIssueVars, "/provides/0/issue_vars/issue_id"},
		{"save shares a key with an issue var", sqlProvider, "            database_oid: data.oid\n", "            database_oid: data.oid\n            username: data.owner\n", CodeIssueVars, "/provides/0/issue/describe/exec/save/username"},

		// CAT-LINT-24: references to issue variables and issued values.
		{"issue.<key> not defined", sqlProvider, "OWNER \"{{issue.username}}\"", "OWNER \"{{issue.owner}}\"", CodeStepRef, "/provides/0/issue/create"},
		{"issue.<key> in revoke", sqlProvider, "ALTER ROLE \"{{issued.username}}\"", "ALTER ROLE \"{{issue.username}}\"", CodeStepRef, "/provides/0/revoke/lock"},
		{"issued.<key> in a value template not issued", sqlProvider, "/{{issued.database}}\", secret: true }", "/{{issued.schema}}\", secret: true }", CodeStepRef, "/provides/0/values/url"},
		{"issued.<key> in init", sqlProvider, "        SET statement_log = 'off';\n", "        SET statement_log = '{{issued.username}}';\n", CodeStepRef, "/init/quiet-log"},
		{"issue var refers to another one", sqlProvider, "      username: \"af_{{issue.id}}\"\n", "      username: \"af_{{issue.database}}\"\n", CodeStepRef, "/provides/0/issue_vars/username"},
		{"issue var refers to saved", sqlProvider, "      username: \"af_{{issue.id}}\"\n", "      username: \"af_{{saved.x}}\"\n", CodeStepRef, "/provides/0/issue_vars/username"},
		{"issue var refers to self", sqlProvider, "      username: \"af_{{issue.id}}\"\n", "      username: \"af_{{self.host}}\"\n", CodeStepRef, "/provides/0/issue_vars/username"},
		{"saved in an exec env before it is saved", sqlProvider, "          env: { DEMO_SQL_PASSWORD: \"{{issued.password}}\" }", "          env: { DEMO_SQL_PASSWORD: \"{{saved.password}}\" }", CodeStepRef, "/provides/0/check/connect"},
		{"a misspelt reference in stdin", sqlProvider, "        SET statement_log = 'off';\n", "        SET statement_log = '{{Params.off}}';\n", CodeTemplate, "/init/quiet-log"},

		// CAT-LINT-30: finish_if in revoke only, exec ones as well.
		{"exec finish_if in issue", sqlProvider, "          timeout_s: 60\n", "          timeout_s: 60\n          finish_if: { exit: [3] }\n", CodeFinishIf, "/provides/0/issue/create/exec/finish_if"},

		// CAT-LINT-38: endpoints, health checks and exposure fit.
		{"path health check on a tcp endpoint", sqlProvider, "    health: { tcp: true }", "    health: { path: /stats }", CodeEndpointKind, "/endpoints/stats/health/path"},
		{"tcp primary endpoint, not internal", sqlProvider, "  default: internal\n", "  default: local\n", CodeEndpointKind, "/exposure/default"},
		{"tcp primary endpoint, exposure left out", sqlProvider, "exposure:\n  default: internal\n", "", CodeEndpointKind, "/exposure/default"},
		{"self.internal_url for a tcp endpoint", sqlProvider, "      host: \"{{self.host}}\"\n", "      host: \"{{self.internal_url}}\"\n", CodeEndpointKind, "/provides/0/values/host"},
		{"template in a health check's exec", sqlProvider, "--port, \"5000\"]", "--port, \"5000\", --password, \"{{params.admin_password}}\"]", CodeEndpointKind, "/endpoints/db/health/exec/6"},

		// CAT-LINT-39: what the platform or the user cannot reach.
		{"first_login on an internal app", sqlProvider, "\nprovides:\n", "\nfirst_login:\n  - { label: 密码, from: params.admin_password, secret: true }\n\nprovides:\n", CodeUnreachable, "/first_login/密码"},
		{"http step on an internal app", sqlProvider, "    check:\n", "    check:\n      - name: http-ping\n        http: { method: GET, path: /ping }\n", CodeUnreachable, "/provides/0/check/http-ping/http"},
		{"http init step on a tcp primary app", sqlProvider, "\ninit:\n", "\ninit:\n  - name: hello\n    when: after_healthy\n    http: { method: GET, path: /hello }\n", CodeUnreachable, "/init/hello/http"},

		// CAT-LINT-02: what the schema refuses.
		{"timeout above 300", sqlProvider, "          timeout_s: 60\n", "          timeout_s: 301\n", CodeSchema, ""},
		{"timeout of 0", sqlProvider, "          timeout_s: 60\n", "          timeout_s: 0\n", CodeSchema, ""},
		{"exit code above 255", sqlProvider, "          expect_exit: [0]\n          finish_if", "          expect_exit: [256]\n          finish_if", CodeSchema, ""},
		{"empty expect_exit", sqlProvider, "          expect_exit: [0]\n          finish_if", "          expect_exit: []\n          finish_if", CodeSchema, ""},
		{"stdout written false", sqlProvider, "server_line: { stdout: true }", "server_line: { stdout: false }", CodeSchema, ""},
		{"stdout with a path", sqlProvider, "server_line: { stdout: true }", "server_line: { stdout: true, path: data.line }", CodeSchema, ""},
		{"stdout with a list", sqlProvider, "server_line: { stdout: true }", "server_line: { stdout: true, list: data.items, field: id }", CodeSchema, ""},
		{"stdout in an http step's save", sqlConsumer, "\ninit:\n", "\ninit:\n  - name: hello\n    when: after_healthy\n    http: { method: GET, path: /hello, save: { line: { stdout: true } } }\n", CodeSchema, ""},
		{"finish_if with a status on an exec step", sqlProvider, "finish_if: { exit: [3] }", "finish_if: { exit: [3], status: [404] }", CodeSchema, ""},
		{"finish_if without exit", sqlProvider, "finish_if: { exit: [3] }", "finish_if: {}", CodeSchema, ""},
		{"env name that is no name", sqlProvider, "      env: { DEMO_SQL_PASSWORD: \"{{params.admin_password}}\" }\n      stdin: |\n        SET", "      env: { 1PASSWORD: \"{{params.admin_password}}\" }\n      stdin: |\n        SET", CodeSchema, ""},
		{"generated issue var too short", sqlProvider, "{ generate: { length: 32, charset: alnum }, secret: true }", "{ generate: { length: 10 }, secret: true }", CodeSchema, ""},
		{"generated issue var, unknown charset", sqlProvider, "{ generate: { length: 32, charset: alnum }, secret: true }", "{ generate: { length: 32, charset: base64 }, secret: true }", CodeSchema, ""},
		{"generated issue var marked not secret", sqlProvider, "{ generate: { length: 32, charset: alnum }, secret: true }", "{ generate: { length: 32 }, secret: false }", CodeSchema, ""},
		{"issue var with secret only", sqlProvider, "{ generate: { length: 32, charset: alnum }, secret: true }", "{ secret: true }", CodeSchema, ""},
		{"issue var that is empty", sqlProvider, "      username: \"af_{{issue.id}}\"\n", "      username: \"\"\n", CodeSchema, ""},
		{"empty issue_vars", sqlProvider, "    issue_vars:\n      database: \"af_{{issue.id}}\"\n      username: \"af_{{issue.id}}\"\n      password: { generate: { length: 32, charset: alnum }, secret: true }\n", "    issue_vars: {}\n", CodeSchema, ""},
		{"health with exec and tcp", sqlProvider, "    health: { tcp: true }", "    health: { tcp: true, exec: [ping] }", CodeSchema, ""},
		{"health with nothing to check", sqlProvider, "    health: { tcp: true }", "    health: { timeout_s: 30 }", CodeSchema, ""},
		{"health tcp written false", sqlProvider, "    health: { tcp: true }", "    health: { tcp: false }", CodeSchema, ""},
		{"expect_json without path", sqlProvider, "    health: { tcp: true }", "    health: { tcp: true, expect_json: { ok: true } }", CodeSchema, ""},
		{"unknown protocol", sqlProvider, "    port: 5001\n    protocol: tcp\n", "    port: 5001\n    protocol: udp\n", CodeSchema, ""},
		{"unknown exposure", sqlProvider, "  default: internal\n", "  default: private\n", CodeSchema, ""},
		{"value with template and issued", sqlProvider, "{ template: \"demo-sql://", "{ issued: url, template: \"demo-sql://", CodeSchema, ""},
		{"value with an empty template", sqlProvider, "      host: \"{{self.host}}\"\n", "      host: { template: \"\" }\n", CodeSchema, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copySqlsvc(t)
			edit(t, filepath.Join(dir, filepath.FromSlash(c.file)), c.old, c.new)
			r, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				if r.Errors() != 0 {
					t.Fatalf("不该报错：%+v", r.Problems)
				}
				return
			}
			hits := problemsWith(r, c.want)
			if len(hits) == 0 {
				t.Fatalf("没报 %s：%+v", c.want, r.Problems)
			}
			found := c.path == ""
			for _, p := range hits {
				if p.Level != LevelError || p.Message == "" || p.File != c.file {
					t.Errorf("%+v", p)
				}
				found = found || p.Path == c.path
			}
			if !found {
				t.Errorf("没在 %s 报 %s：%+v", c.path, c.want, hits)
			}
			if _, err := Build(dir, 1); CodeOf(err) != CodeBuildLint {
				t.Fatalf("有错误时不能打包：%v", err)
			}
		})
	}
}

// CAT-LINT-35 knows every way a value becomes secret, also those that take
// several places of the manifest: a key secret through the item that hands
// it out, a secret save read by a later step.
func TestLintExecSecretAcrossPlaces(t *testing.T) {
	type change struct{ old, new string }
	const checkTail = "--oid, \"{{issued.database_oid}}\", --ping]"
	cases := []struct {
		name    string
		changes []change
		want    string // "" for no error
		path    string
	}{
		{"password from a template, secret through its item", []change{
			{"{ generate: { length: 32, charset: alnum }, secret: true }", "\"p{{issue.id}}\""},
			{checkTail, "--oid, \"{{issued.database_oid}}\", --password, \"{{issued.password}}\"]"},
		}, CodeExecSecret, "/provides/0/check/connect/exec/command/10"},
		{"stdout save not marked, secret in the service type", []change{
			{"      password: { issued: password, secret: true }\n", "      password: { issued: server_line }\n"},
			{checkTail, "--oid, \"{{issued.database_oid}}\", --line, \"{{issued.server_line}}\"]"},
		}, CodeExecSecret, "/provides/0/check/connect/exec/command/10"},
		{"stdout save not marked, the item marked secret", []change{
			{"      username: { issued: username }\n", "      username: { issued: server_line, secret: true }\n"},
			{"ALTER ROLE \"{{issued.username}}\" NOLOGIN;", "ALTER ROLE \"{{issued.server_line}}\" NOLOGIN;"},
			{"      - name: lock\n        exec:\n          user: demo\n          command: [demo-sql, --admin, --on-error-stop]", "      - name: lock\n        exec:\n          user: demo\n          command: [demo-sql, --admin, --role, \"{{issued.server_line}}\"]"},
		}, CodeExecSecret, "/provides/0/revoke/lock/exec/command/3"},
		{"stdout save not marked, the item plain", []change{
			{"      username: { issued: username }\n", "      username: { issued: server_line }\n"},
			{"      - name: lock\n        exec:\n          user: demo\n          command: [demo-sql, --admin, --on-error-stop]", "      - name: lock\n        exec:\n          user: demo\n          command: [demo-sql, --admin, --role, \"{{issued.server_line}}\"]"},
		}, "", ""},
		{"secret save read by a later step's command", []change{
			{"            server_line: { stdout: true }\n", "            server_line: { stdout: true, secret: true }\n"},
			{"    revoke:\n", "      - name: tag\n        exec:\n          command: [demo-sql, --tag, \"{{saved.server_line}}\"]\n    revoke:\n"},
		}, CodeExecSecret, "/provides/0/issue/tag/exec/command/2"},
		{"plain save read by a later step's command", []change{
			{"    revoke:\n", "      - name: tag\n        exec:\n          command: [demo-sql, --tag, \"{{saved.server_line}}\"]\n    revoke:\n"},
		}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copySqlsvc(t)
			path := filepath.Join(dir, filepath.FromSlash(sqlProvider))
			for _, ch := range c.changes {
				edit(t, path, ch.old, ch.new)
			}
			r, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			if c.want == "" {
				if r.Errors() != 0 {
					t.Fatalf("不该报错：%+v", r.Problems)
				}
				return
			}
			hits := problemsWith(r, c.want)
			found := false
			for _, p := range hits {
				found = found || (p.Path == c.path && p.Level == LevelError)
			}
			if !found {
				t.Fatalf("没在 %s 报 %s：%+v", c.path, c.want, r.Problems)
			}
			if r.Errors() != len(hits) {
				t.Errorf("只该有 %s：%+v", c.want, r.Problems)
			}
		})
	}
}

// Which issued keys are secret is one rule (IssuedSecretKeys), the one the
// platform goes by: besides generated issue variables and saves marked
// secret, a key behind an item { issued: k } that is marked secret, or that
// the service type marks secret, is secret too.
func TestIssuedSecretKeys(t *testing.T) {
	types, _, problems := ReadServiceTypes(sqlsvcDir)
	if len(types) != 1 || len(problems) != 0 {
		t.Fatalf("%+v %+v", types, problems)
	}
	kind := &types[0]
	keys := func(p Provide, st *ServiceType) string {
		var out []string
		for k := range p.IssuedSecretKeys(nil, st) {
			out = append(out, k)
		}
		sort.Strings(out)
		return strings.Join(out, ",")
	}
	base := readSqlsvc(t, sqlProvider).Provides[0]
	if got := keys(base, kind); got != "password" {
		t.Fatalf("material: %s", got)
	}
	// A password made from a template, not generated: still secret through
	// the item that hands it out, by its mark or by the service type.
	p := base
	p.IssueVars = map[string]IssueVar{"database": base.IssueVars["database"], "username": base.IssueVars["username"], "password": {Template: "p{{issue.id}}"}}
	if got := keys(p, nil); got != "password" {
		t.Fatalf("marked item: %s", got)
	}
	p.Values = map[string]ValueSource{}
	for k, v := range base.Values {
		p.Values[k] = v
	}
	p.Values["password"] = ValueSource{Issued: "password"}
	if got := keys(p, kind); got != "password" {
		t.Fatalf("item secret in the service type: %s", got)
	}
	if got := keys(p, nil); got != "" {
		t.Fatalf("nothing marks it: %s", got)
	}
	// An exec save marked secret counts, as an http one does.
	q := base
	q.Issue = append([]Step(nil), base.Issue...)
	d := *base.Issue[1].Exec
	d.Save = map[string]SaveValue{"database_oid": d.Save["database_oid"], "server_line": {Stdout: true, Secret: true}}
	q.Issue[1] = Step{Name: base.Issue[1].Name, Exec: &d}
	if got := keys(q, kind); got != "password,server_line" {
		t.Fatalf("exec save: %s", got)
	}
	// A template item marked secret does not make the keys it is made of
	// secret: the account name in a connection string stays plain.
	if base.IssuedSecret("username", nil, kind) {
		t.Fatal("username")
	}
}

// issue_vars without issue steps never come to be: CAT-LINT-37, reported
// once at the issue_vars (not again at every value that reads them).
func TestLintIssueVarsWithoutIssue(t *testing.T) {
	dir := copySqlsvc(t)
	path := filepath.Join(dir, filepath.FromSlash(sqlProvider))
	text := string(mustRead(t, path))
	from, to := strings.Index(text, "    issue:\n"), strings.Index(text, "\ndelivery:")
	if from < 0 || to < from {
		t.Fatal("the steps are not in the material")
	}
	if err := os.WriteFile(path, []byte(text[:from]+text[to:]), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _ := Lint(dir)
	if hits := problemsWith(r, CodeIssueVars); len(hits) != 1 || hits[0].Path != "/provides/0/issue_vars" {
		t.Fatalf("%+v", r.Problems)
	}
	if r.Errors() != 1 {
		t.Fatalf("只该有这一个错误：%+v", r.Problems)
	}
}

// What a step refers to in its env, stdin and saves counts wherever the
// references of a manifest are listed: the variable report, the platform
// variables an install needs.
func TestExecStepRefsCount(t *testing.T) {
	m := readSqlsvc(t, sqlProvider)
	var refs []string
	for _, r := range manifestRefs(m) {
		refs = append(refs, r.Ref+"@"+r.Where)
	}
	for _, want := range []string{
		"params.admin_password@/init/quiet-log",
		"params.admin_password@/provides/0/issue/create",
		"issue.password@/provides/0/issue/create",
		"consumer.name@/provides/0/issue/create",
		"issue.id@/provides/0/issue_vars/database",
		"issued.password@/provides/0/values/url",
		"issued.password@/provides/0/check/connect",
	} {
		found := false
		for _, r := range refs {
			found = found || r == want
		}
		if !found {
			t.Errorf("没列出 %s：%v", want, refs)
		}
	}
	rows := ClassifyVars(m)
	for _, row := range rows {
		if row.Name == "params.admin_password" {
			sort.Strings(row.UsedIn)
			if strings.Join(row.UsedIn, ",") != "/env/DEMO_SQL_ADMIN_PASSWORD,/init/quiet-log,/provides/0/issue/create,/provides/0/issue/describe,/provides/0/revoke/lock" {
				t.Errorf("admin_password 用在 %v", row.UsedIn)
			}
		}
	}
	// A value template that refers to issued.<key> is handed out per
	// consumer in the report, as { issued: key } is.
	how := map[string]string{}
	for _, v := range ServiceProvides(m)[0].Values {
		how[v.Key] = v.How
	}
	want := map[string]string{"host": HowComputed, "port": HowComputed, "database": HowIssued, "username": HowIssued, "password": HowIssued, "url": HowIssued}
	if !reflect.DeepEqual(how, want) {
		t.Fatalf("how %v", how)
	}
}
