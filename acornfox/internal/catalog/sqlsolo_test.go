package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sqlsoloDir is the test catalog of a database-shaped service for one
// consumer only (the B section's second round of the format): a provider
// with max_consumers, purge, on_check_fail, a readiness check and a health
// check comparing the output, and a consumer whose need is dedicated.
var sqlsoloDir = filepath.Join("testdata", "sqlsolo")

const (
	soloProvider = "apps/demo-solo-sql/manifest.yaml"
	soloConsumer = "apps/demo-solo-sql-user/manifest.yaml"
)

// copySqlsolo copies the test catalog so a test can break it.
func copySqlsolo(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(sqlsoloDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(sqlsoloDir, p)
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

func readSqlsolo(t *testing.T, rel string) *Manifest {
	t.Helper()
	m, err := ParseManifest(mustRead(t, filepath.Join(sqlsoloDir, filepath.FromSlash(rel))))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// The material is clean: no error and no warning (every writing of the
// second round is carried out now, PendingFeatures is empty), and the
// lowest platform version is BasePlatform. self.* is written in the check
// steps (the check connects as the consumer would, through the provider's
// own name on the app network).
func TestLintSoloCatalog(t *testing.T) {
	r, err := Lint(sqlsoloDir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Errors() != 0 || strings.Join(r.Apps, ",") != "demo-solo-sql,demo-solo-sql-user" || strings.Join(r.ServiceTypes, ",") != "demo_sql" {
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
	if _, err := Build(sqlsoloDir, 1); err != nil {
		t.Fatalf("能打包：%v", err)
	}
	for _, file := range []string{soloProvider, soloConsumer} {
		if p := PendingFeatures(readSqlsolo(t, file)); len(p) != 0 {
			t.Errorf("%s：%+v", file, p)
		}
	}
	m := readSqlsolo(t, soloProvider)
	if !strings.Contains(strings.Join(m.Provides[0].Check[0].Exec.Command, " "), "{{self.host}}") {
		t.Fatalf("材料的 check 要连 self.host：%v", m.Provides[0].Check[0].Exec.Command)
	}
}

// The new fields decode into the structs the engine will read, and survive
// JSON (the engine stores manifests as JSON).
func TestParseSoloManifest(t *testing.T) {
	m := readSqlsolo(t, soloProvider)
	h := m.Endpoints[0].Health
	if h == nil || h.ExpectStdout != "PONG" || len(h.Exec) != 5 || h.Path != "" || h.TCP {
		t.Fatalf("health %+v", h)
	}
	p := m.Provides[0]
	if p.MaxConsumers != 1 || p.OnCheckFail != OnCheckFailStop || p.CheckFailAction() != OnCheckFailStop {
		t.Fatalf("max_consumers %d on_check_fail %q", p.MaxConsumers, p.OnCheckFail)
	}
	if (Provide{}).CheckFailAction() != OnCheckFailReissue || (Provide{}).ReadySteps() != nil || (Provide{}).MaxConsumers != 0 {
		t.Fatal("不写时的默认")
	}
	if len(p.Purge) != 1 || p.Purge[0].Name != "drop" || p.Purge[0].Exec == nil || !reflect.DeepEqual(p.Purge[0].Exec.FinishIf.Exit, []int{3}) ||
		p.Purge[0].Exec.Timeout() != 120 || !strings.Contains(p.Purge[0].Exec.Stdin, `DROP DATABASE IF EXISTS "{{issued.database}}"`) {
		t.Fatalf("purge %+v", p.Purge)
	}
	if p.Ready == nil || p.Ready.Say != "样例数据库还没建好管理账号：重装一次这个应用" || len(p.Ready.Steps) != 2 || len(p.ReadySteps()) != 2 ||
		p.Ready.Steps[0].Exec.Save["roles"].Path != "data.roles" || p.Ready.Steps[1].Name != "at-least-one" {
		t.Fatalf("ready %+v", p.Ready)
	}
	// What purge and the readiness check save is not part of a credential.
	if got := strings.Join(p.IssuedKeys(), ","); got != "database,password,username" {
		t.Fatalf("IssuedKeys %s", got)
	}
	u := readSqlsolo(t, soloConsumer).Uses[0]
	if !u.Dedicated || u.Optional {
		t.Fatalf("uses %+v", u)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"expect_stdout":"PONG"`, `"max_consumers":1`, `"on_check_fail":"stop"`, `"purge":[{"name":"drop"`,
		`"ready":{"steps":[{"name":"count"`, `"say":"样例数据库还没建好管理账号：重装一次这个应用"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON 里没有 %s：%s", want, b)
		}
	}
	var back Manifest
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back.Provides, m.Provides) || !reflect.DeepEqual(back.Endpoints, m.Endpoints) {
		t.Fatalf("来回一趟变了：%v\n%+v\n%+v", err, back.Provides, m.Provides)
	}
	// Not written, nothing new shows in the JSON.
	plain, _ := json.Marshal(Provide{Kind: "x"})
	for _, key := range []string{"max_consumers", "purge", "on_check_fail", "ready"} {
		if strings.Contains(string(plain), `"`+key+`"`) {
			t.Errorf("没写的 %s 出现在 JSON 里：%s", key, plain)
		}
	}
}

// What purge and the readiness check refer to counts wherever the
// references of a manifest are listed (the variable report, the platform
// variables an install needs).
func TestSoloStepRefsCount(t *testing.T) {
	var refs []string
	for _, r := range manifestRefs(readSqlsolo(t, soloProvider)) {
		refs = append(refs, r.Ref+"@"+r.Where)
	}
	for _, want := range []string{
		"issued.database@/provides/0/purge/drop",
		"params.admin_password@/provides/0/purge/drop",
		"self.host@/provides/0/ready/steps/count",
		"params.admin_password@/provides/0/ready/steps/count",
		"saved.roles@/provides/0/ready/steps/at-least-one",
	} {
		found := false
		for _, r := range refs {
			found = found || r == want
		}
		if !found {
			t.Errorf("没列出 %s：%v", want, refs)
		}
	}
}

// The checks of the second round, one broken catalog per case: all errors,
// the catalog does not build. Cases with no code are allowed writings.
func TestLintSoloProblems(t *testing.T) {
	text := string(mustRead(t, filepath.Join(sqlsoloDir, filepath.FromSlash(soloProvider))))
	from, to := strings.Index(text, "provides:\n"), strings.Index(text, "\ndelivery:")
	if from < 0 || to < from {
		t.Fatal("the provides block is not in the material")
	}
	block := text[from:to]
	purge := text[strings.Index(text, "    purge:\n"):strings.Index(text, "    check:\n")]
	steps := text[strings.Index(text, "      steps:\n"):to]
	// One shared account for every consumer: nothing is issued per consumer.
	const shared = "provides:\n  - kind: demo_sql\n    endpoint: db\n    values:\n      host: \"{{self.host}}\"\n      database: app\n      username: app\n" +
		"      password: { template: \"{{params.admin_password}}\", secret: true }\n"
	const check = "    check:\n      - name: connect\n        exec:\n          command: [demo-sql, --host, \"{{self.host}}\", --user, \"{{issued.username}}\", --database, \"{{issued.database}}\", --ping]\n" +
		"          env: { DEMO_SQL_PASSWORD: \"{{issued.password}}\" }\n"
	const purgeCmd = "      - name: drop\n        exec:\n          command: [demo-sql, --admin, --on-error-stop]\n"
	cases := []struct {
		name, file, old, new, want, path string
	}{
		// CAT-LINT-41: purge and on_check_fail are about what is issued per
		// consumer; on_check_fail needs check steps as well.
		{"purge without issue", soloProvider, block, shared + "    purge:\n      - exec: { command: [demo-sql, --admin, --drop-all] }\n", CodeNeedsIssue, "/provides/0/purge"},
		{"on_check_fail stop without issue", soloProvider, block, shared + "    on_check_fail: stop\n", CodeNeedsIssue, "/provides/0/on_check_fail"},
		{"on_check_fail reissue without issue", soloProvider, block, shared + "    on_check_fail: reissue\n", CodeNeedsIssue, "/provides/0/on_check_fail"},
		{"on_check_fail without check", soloProvider, check, "", CodeNeedsIssue, "/provides/0/on_check_fail"},
		{"shared account, one consumer, readiness check", soloProvider, block, shared + "    max_consumers: 1\n    ready:\n      say: 还没好\n      steps:\n        - exec: { command: [demo-sql, --admin, --ping] }\n", "", ""},
		{"on_check_fail reissue written", soloProvider, "    on_check_fail: stop\n", "    on_check_fail: reissue\n", "", ""},
		{"no check, no on_check_fail", soloProvider, check + "    on_check_fail: stop\n", "", "", ""},

		// CAT-LINT-42: a readiness check has no consumer and no credential.
		{"issued in a readiness check", soloProvider, "--count-roles]", "--count-roles, --user, \"{{issued.username}}\"]", CodeReadyRef, "/provides/0/ready/steps/count"},
		{"consumer in a readiness check", soloProvider, "--count-roles]", "--count-roles, --for, \"{{consumer.name}}\"]", CodeReadyRef, "/provides/0/ready/steps/count"},
		{"issue.id in a readiness check", soloProvider, "--count-roles]", "--count-roles, --tag, \"{{issue.id}}\"]", CodeReadyRef, "/provides/0/ready/steps/count"},
		{"uses in a readiness check", soloProvider, "--count-roles]", "--count-roles, --tag, \"{{uses.db.host}}\"]", CodeStepRef, "/provides/0/ready/steps/count"},
		{"self in a readiness check without endpoint", soloProvider, "    endpoint: db\n", "", CodeStepRef, "/provides/0/ready/steps/count"},
		{"saved before it is saved in a readiness check", soloProvider, "--count, \"{{saved.roles}}\"]", "--count, \"{{saved.users}}\"]", CodeStepRef, "/provides/0/ready/steps/at-least-one"},
		{"platform variable in a readiness check", soloProvider, "--count-roles]", "--count-roles, --tz, \"{{platform.tz}}\"]", "", ""},

		// CAT-LINT-43: say is a sentence shown as written.
		{"blank say", soloProvider, "      say: 样例数据库还没建好管理账号：重装一次这个应用\n", "      say: \"   \"\n", CodeReadySay, "/provides/0/ready/say"},
		{"template in say", soloProvider, "      say: 样例数据库还没建好管理账号：重装一次这个应用\n", "      say: \"密码是 {{params.admin_password}}\"\n", CodeReadySay, "/provides/0/ready/say"},

		// CAT-LINT-44: expect_stdout is compared trimmed and as written.
		{"blanks around expect_stdout", soloProvider, "      expect_stdout: PONG\n", "      expect_stdout: \" PONG\"\n", CodeHealthStdout, "/endpoints/db/health/expect_stdout"},
		{"line break after expect_stdout", soloProvider, "      expect_stdout: PONG\n", "      expect_stdout: |\n        PONG\n", CodeHealthStdout, "/endpoints/db/health/expect_stdout"},
		{"template in expect_stdout", soloProvider, "      expect_stdout: PONG\n", "      expect_stdout: \"{{params.admin_password}}\"\n", CodeHealthStdout, "/endpoints/db/health/expect_stdout"},
		{"two words in expect_stdout", soloProvider, "      expect_stdout: PONG\n", "      expect_stdout: \"accepting connections\"\n", "", ""},

		// CAT-LINT-24: self.* in revoke, purge and check steps of an entry
		// that names its endpoint (the material's check has it); not without
		// the endpoint, not in issue steps.
		{"self in purge", soloProvider, "DROP ROLE IF EXISTS \"{{issued.username}}\"", "DROP ROLE IF EXISTS \"{{issued.username}}\"; -- {{self.host}}:{{self.port}}", "", ""},
		{"self in revoke", soloProvider, "ALTER ROLE \"{{issued.username}}\" NOLOGIN;", "ALTER ROLE \"{{issued.username}}\" NOLOGIN; -- {{self.host}}", "", ""},
		{"self in check without endpoint", soloProvider, "    endpoint: db\n", "", CodeStepRef, "/provides/0/check/connect"},
		{"self in issue", soloProvider, "CREATE DATABASE \"{{issue.database}}\"", "CREATE DATABASE \"{{issue.database}}\" -- {{self.host}}", CodeStepRef, "/provides/0/issue/create"},
		{"unknown self in check", soloProvider, "--host, \"{{self.host}}\", --user", "--host, \"{{self.name}}\", --user", CodeStepRef, "/provides/0/check/connect"},

		// CAT-LINT-30: finish_if in revoke and purge only (the material has
		// one in purge).
		{"finish_if in a readiness check", soloProvider, "--count, \"{{saved.roles}}\"]", "--count, \"{{saved.roles}}\"]\n            finish_if: { exit: [3] }", CodeFinishIf, "/provides/0/ready/steps/at-least-one/exec/finish_if"},

		// CAT-LINT-24: what purge may refer to.
		{"issue.<key> in purge", soloProvider, "DROP ROLE IF EXISTS \"{{issued.username}}\"", "DROP ROLE IF EXISTS \"{{issue.username}}\"", CodeStepRef, "/provides/0/purge/drop"},
		{"issued.<key> not issued in purge", soloProvider, "DROP ROLE IF EXISTS \"{{issued.username}}\"", "DROP ROLE IF EXISTS \"{{issued.owner}}\"", CodeStepRef, "/provides/0/purge/drop"},
		{"consumer and issue_id in purge", soloProvider, "DROP ROLE IF EXISTS \"{{issued.username}}\"", "DROP ROLE IF EXISTS \"{{issued.username}}\"; -- {{consumer.name}} {{issued.issue_id}}", "", ""},

		// CAT-LINT-35: no secret in the argument list, here as anywhere.
		{"secret param in a readiness command", soloProvider, "--count-roles]", "--count-roles, --password, \"{{params.admin_password}}\"]", CodeExecSecret, "/provides/0/ready/steps/count/exec/command/7"},
		{"issued secret in a purge command", soloProvider, purgeCmd, "      - name: drop\n        exec:\n          command: [demo-sql, --admin, --password, \"{{issued.password}}\"]\n", CodeExecSecret, "/provides/0/purge/drop/exec/command/3"},

		// CAT-LINT-39: no http step for an internal app, in purge and ready too.
		{"http step in purge", soloProvider, purgeCmd, "      - name: web\n        http: { method: DELETE, path: /db }\n" + purgeCmd, CodeUnreachable, "/provides/0/purge/web/http"},
		{"http step in a readiness check", soloProvider, "      steps:\n", "      steps:\n        - name: web\n          http: { method: GET, path: /status }\n", CodeUnreachable, "/provides/0/ready/steps/web/http"},

		// CAT-LINT-02: what the schema refuses.
		{"max_consumers of 0", soloProvider, "    max_consumers: 1\n", "    max_consumers: 0\n", CodeSchema, ""},
		{"max_consumers written as text", soloProvider, "    max_consumers: 1\n", "    max_consumers: \"1\"\n", CodeSchema, ""},
		{"unknown on_check_fail", soloProvider, "    on_check_fail: stop\n", "    on_check_fail: ignore\n", CodeSchema, ""},
		{"empty purge", soloProvider, purge, "    purge: []\n", CodeSchema, ""},
		{"readiness check without say", soloProvider, "      say: 样例数据库还没建好管理账号：重装一次这个应用\n", "", CodeSchema, ""},
		{"readiness check without steps", soloProvider, steps, "      steps: []\n", CodeSchema, ""},
		{"say too long", soloProvider, "      say: 样例数据库还没建好管理账号：重装一次这个应用\n", "      say: " + strings.Repeat("长", 121) + "\n", CodeSchema, ""},
		{"empty expect_stdout", soloProvider, "      expect_stdout: PONG\n", "      expect_stdout: \"\"\n", CodeSchema, ""},
		{"expect_stdout with path", soloConsumer, "      path: /healthz\n", "      path: /healthz\n      expect_stdout: ok\n", CodeSchema, ""},
		{"expect_stdout with tcp", soloProvider, "      exec: [demo-sql-ping, --host, 127.0.0.1, --port, \"5000\"]\n", "      tcp: true\n", CodeSchema, ""},
		{"dedicated written as text", soloConsumer, "dedicated: true }", "dedicated: \"yes\" }", CodeSchema, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copySqlsolo(t)
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

// CAT-LINT-45: the http steps of a readiness check only read. The check is
// written on the HTTP provider of the services material (the sqlsolo
// provider is internal, where no http step is allowed at all). GET reads;
// POST stays allowed, many apps log in with one before anything can be read;
// PUT, PATCH and DELETE always change something.
func TestLintReadyWrites(t *testing.T) {
	const at = "    revoke:\n"
	ready := func(method string) string {
		return "    ready:\n      say: 还没有能用的令牌：到应用的设置页建一个\n      steps:\n" +
			"        - name: login\n          http:\n            method: POST\n            path: /api/login\n" +
			"            json: { password: \"{{params.admin_password}}\" }\n            save: { session: data.session }\n" +
			"        - name: look\n          http:\n            method: " + method + "\n            path: /api/tokens\n" +
			"            headers: { X-Session: \"{{saved.session}}\" }\n"
	}
	for _, c := range []struct{ method, want string }{
		{"GET", ""},
		{"POST", ""},
		{"PUT", CodeReadyWrite},
		{"PATCH", CodeReadyWrite},
		{"DELETE", CodeReadyWrite},
	} {
		t.Run(c.method, func(t *testing.T) {
			dir := copyServices(t)
			file := "apps/demo-provider/manifest.yaml"
			edit(t, filepath.Join(dir, filepath.FromSlash(file)), at, ready(c.method)+at)
			r, err := Lint(dir)
			if err != nil {
				t.Fatal(err)
			}
			hits := problemsWith(r, CodeReadyWrite)
			if c.want == "" {
				if len(hits) != 0 || r.Errors() != 0 {
					t.Fatalf("不该报错：%+v", r.Problems)
				}
				return
			}
			if len(hits) != 1 {
				t.Fatalf("要报一条 %s：%+v", c.want, r.Problems)
			}
			p := hits[0]
			if p.Level != LevelError || p.File != file || p.Path != "/provides/0/ready/steps/look/http/method" || !strings.Contains(p.Message, c.method) {
				t.Fatalf("%+v", p)
			}
		})
	}
}
