package appinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// The database-shaped test material (internal/catalog/testdata/sqlsvc),
// both manifests as written, through the whole flow: the provider (a tcp
// primary endpoint, reached over the app network only, its health checked
// by a command in its own container) is installed and its init command
// runs in its container; a consumer is installed and the provider's issue
// commands make it a database and an account (issue_vars: names from the
// issue number, a generated password); the consumer gets its own items and
// a connection string worked out from what it was issued; its init command
// runs in its own container; installed again on its data, the old account
// passes the check command and is used again; deleted with its data, the
// account is taken back. The commands run in a fake database (fakeSQL)
// behind a fake runner.

const sqlsvcDir = "../catalog/testdata/sqlsvc"

func sqlManifest(t testing.TB, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sqlsvcDir, "apps", id, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// isHealthProbe reports whether req is the provider's health check (its
// primary endpoint's health.exec), not one of its steps.
func isHealthProbe(req runner.ExecRequest) bool {
	return len(req.Command) > 0 && req.Command[0] == "demo-sql-ping"
}

// fakeSQL plays the database the material's commands talk to. It checks
// the admin password the provider's commands get in their env, keeps roles
// and databases, and answers like the made-up demo-sql tool.
type fakeSQL struct {
	mu           sync.Mutex
	st           *state.Store
	admin        string
	roles        map[string]*sqlRole
	nextOID      int
	failDescribe bool
	migrated     []string // consumer init: the user it connected as
	wrongTarget  []string
	pings        int // health checks run in the provider's container
}

type sqlRole struct {
	password, database, oid string
	login                   bool
}

var (
	createRe = regexp.MustCompile(`CREATE ROLE "([^"]+)" LOGIN PASSWORD '([^']+)';\s*CREATE DATABASE "([^"]+)" OWNER "([^"]+)";`)
	lockRe   = regexp.MustCompile(`ALTER ROLE "([^"]+)" NOLOGIN;`)
)

func argAfter(cmd []string, flag string) string {
	for i := 0; i+1 < len(cmd); i++ {
		if cmd[i] == flag {
			return cmd[i+1]
		}
	}
	return ""
}

func (d *fakeSQL) exec(req runner.ExecRequest) (runner.ExecResult, error) {
	// The runner's own boundary: the app's current deployment, running.
	a, err := d.st.GetApp(context.Background(), req.App)
	if err != nil || req.Name != runner.ContainerName(a.Name, a.CurrentDeployment) {
		d.mu.Lock()
		d.wrongTarget = append(d.wrongTarget, req.Name)
		d.mu.Unlock()
		return runner.ExecResult{}, runner.ErrNotFound
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	res := func(code int, stdout, stderr string) (runner.ExecResult, error) {
		return runner.ExecResult{ExitCode: code, Stdout: []byte(stdout), Stderr: []byte(stderr)}, nil
	}
	stdin := string(req.Stdin)
	if isHealthProbe(req) {
		// The provider's health.exec, as the material writes it.
		if req.App != "demo-sql" || strings.Join(req.Command, " ") != "demo-sql-ping --host 127.0.0.1 --port 5000" || req.User != "" || len(req.Env) != 0 || len(req.Stdin) != 0 {
			return res(1, "", "bad health check call")
		}
		d.pings++
		return res(0, "", "")
	}
	if req.App != "demo-sql" { // a consumer (any name it was installed as)
		if req.Command[0] != "demo-app" || req.Env["DB_PASSWORD"] == "" {
			return res(1, "", "bad migrate call")
		}
		d.migrated = append(d.migrated, argAfter(req.Command, "--user"))
		return res(0, "migrated\n", "")
	}
	switch {
	case strings.Contains(stdin, "statement_log"):
		d.admin = req.Env["DEMO_SQL_PASSWORD"]
		return res(0, "", "")
	case strings.Contains(stdin, "CREATE ROLE"):
		if req.Env["DEMO_SQL_PASSWORD"] != d.admin || d.admin == "" {
			return res(1, "", "FATAL: password authentication failed")
		}
		m := createRe.FindStringSubmatch(stdin)
		if m == nil {
			return res(1, "", "ERROR: syntax error")
		}
		if _, have := d.roles[m[1]]; have {
			// Like a real database: the failing statement is echoed.
			return res(1, "", "ERROR: role exists\nSTATEMENT: "+stdin)
		}
		d.nextOID++
		d.roles[m[1]] = &sqlRole{password: m[2], database: m[3], login: true, oid: fmt.Sprint(16383 + d.nextOID)}
		return res(0, "CREATE ROLE\nCREATE DATABASE\n", "")
	case argAfter(req.Command, "--database") != "" && req.Command[len(req.Command)-1] == "--describe":
		db := argAfter(req.Command, "--database")
		for name, r := range d.roles {
			if r.database == db {
				if d.failDescribe {
					return res(1, "", "describe failed for "+name+" with password "+r.password)
				}
				return res(0, `{"data": {"oid": `+r.oid+`}}`+"\n", "")
			}
		}
		return res(1, "", "no such database")
	case strings.Contains(stdin, "NOLOGIN"):
		m := lockRe.FindStringSubmatch(stdin)
		r, have := d.roles[m[1]]
		if !have {
			return res(3, "", "ERROR: role does not exist")
		}
		r.login = false
		return res(0, "ALTER ROLE\n", "")
	case req.Command[len(req.Command)-1] == "--ping":
		r, have := d.roles[argAfter(req.Command, "--user")]
		if !have || !r.login || r.password != req.Env["DEMO_SQL_PASSWORD"] || r.database != argAfter(req.Command, "--database") || r.oid != argAfter(req.Command, "--oid") {
			return res(2, "", "connection refused")
		}
		return res(0, "PONG\n", "")
	}
	return res(127, "", "unknown command")
}

func (d *fakeSQL) role(name string) (sqlRole, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.roles[name]
	if !ok {
		return sqlRole{}, false
	}
	return *r, true
}

func (d *fakeSQL) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.roles)
}

type sqlEnv struct {
	*env
	db   *fakeSQL
	ex   *execRunner
	logs *lockedBuffer
}

func setupSQL(t *testing.T) *sqlEnv {
	t.Helper()
	manifests := []string{sqlManifest(t, "demo-sql"), sqlManifest(t, "demo-sql-user")}
	logs := &lockedBuffer{}
	db := &fakeSQL{roles: map[string]*sqlRole{}}
	ex := &execRunner{do: db.exec}
	v, _ := setupSnapshot(t, bundleFS(typedBundle(t, sqlsvcDir, nil, manifests...)), func(c *Config) {
		c.Version = "0.6.0"
		c.Log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		c.Runner = &execWorld{fakeWorld: c.Runner.(*fakeWorld), execRunner: ex}
	})
	db.st = v.w.st
	// Only the consumer answers HTTP (its health path): the provider is
	// reached over the app network only and checked by a command.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	t.Cleanup(srv.Close)
	v.w.mu.Lock()
	v.w.servers["demo-sql-user"] = srv
	v.w.mu.Unlock()
	return &sqlEnv{env: v, db: db, ex: ex, logs: logs}
}

// stepRequests are the commands the steps ran (init, issue, revoke, check),
// without the provider's health checks.
func (v *sqlEnv) stepRequests() []runner.ExecRequest {
	var out []runner.ExecRequest
	for _, r := range v.ex.requests() {
		if !isHealthProbe(r) {
			out = append(out, r)
		}
	}
	return out
}

func TestExecFlowIssueCheckRevoke(t *testing.T) {
	v := setupSQL(t)
	ctx := context.Background()
	reg, err := v.e.Registry()
	if err != nil {
		t.Fatal(err)
	}

	// The provider: an internal app, its health checked by the command the
	// manifest writes, then its init command ran in its own container.
	ptv := v.installOK(t, Request{App: "demo-sql"})
	prov, _ := v.w.st.GetApp(ctx, "demo-sql")
	if !prov.Internal || prov.HealthCheck == nil || len(prov.HealthCheck.Exec) == 0 || ptv.Apps[0].URL != "" || !ptv.Apps[0].Internal {
		t.Fatalf("provider %+v health %+v result %+v", prov, prov.HealthCheck, ptv.Apps[0])
	}
	v.db.mu.Lock()
	pings := v.db.pings
	v.db.mu.Unlock()
	if pings == 0 {
		t.Fatal("the provider's health.exec never ran")
	}
	reqs := v.stepRequests()
	if len(reqs) != 1 || reqs[0].Name != runner.ContainerName("demo-sql", prov.CurrentDeployment) || reqs[0].User != "demo" || v.db.admin == "" || len(v.db.admin) != 32 {
		t.Fatalf("provider init %v (admin %d)", reqs, len(v.db.admin))
	}
	sv, err := reg.GetService(ctx, "demo-sql")
	if err != nil || sv.Kind != "demo_sql" || sv.Values["host"] != "demo-sql" || sv.Values["port"] != "5000" || len(sv.Values) != 2 {
		t.Fatalf("service %+v %v", sv, err)
	}
	if h := v.e.serviceHealth(ctx, sv, v.e.planKinds()); !h.Usable() {
		t.Fatalf("health %+v: the items issued per consumer count as missing", h)
	}

	// The consumer: a role and a database of its own, named by the issue.
	tv := v.installOK(t, Request{App: "demo-sql-user"})
	b, err := reg.GetBinding(ctx, "demo-sql-user", "db")
	if err != nil || b.Service != "demo-sql" {
		t.Fatalf("binding %+v %v", b, err)
	}
	id := b.Issued[catalog.IssueIDKey]
	user, pass, dbName := b.Issued["username"], b.Issued["password"], b.Issued["database"]
	if len(id) != 8 || user != "af_"+id || dbName != "af_"+id || len(pass) != 32 || b.Issued["database_oid"] != "16384" {
		t.Fatalf("issued %v", b.Issued)
	}
	if _, kept := b.Issued["server_line"]; kept || len(b.Issued) != 5 {
		t.Fatalf("credential keeps what nothing reads: %v", b.Issued)
	}
	if r, ok := v.db.role(user); !ok || r.password != pass || r.database != dbName || !r.login {
		t.Fatalf("role %+v %v", r, ok)
	}
	envs := envVars(t, v.env, "demo-sql-user")
	wantURL := "demo-sql://" + user + ":" + pass + "@demo-sql:5000/" + dbName
	if envs["DB_HOST"].Value != "demo-sql" || envs["DB_PORT"].Value != "5000" || envs["DB_NAME"].Value != dbName || envs["DB_USER"].Value != user ||
		envs["DB_PASSWORD"].Value != pass || !envs["DB_PASSWORD"].Secret || envs["DB_URL"].Value != wantURL || !envs["DB_URL"].Secret {
		t.Fatalf("env %+v", envs)
	}
	cons, _ := v.w.st.GetApp(ctx, "demo-sql-user")
	if strings.Join(v.db.migrated, ",") != user || strings.Join(cons.Links, ",") != "demo-sql" {
		t.Fatalf("consumer init %v links %v", v.db.migrated, cons.Links)
	}
	for _, r := range v.ex.requests() {
		app, _ := v.w.st.GetApp(ctx, r.App)
		if r.Name != runner.ContainerName(r.App, app.CurrentDeployment) {
			t.Fatalf("ran in %s", r.Name)
		}
		for _, arg := range append(r.Command, r.User) {
			if strings.Contains(arg, pass) || strings.Contains(arg, v.db.admin) {
				t.Fatalf("a secret in the arguments: %q", r.Command)
			}
		}
	}
	// Not in the task, the views, the log.
	raw, _ := v.w.st.GetInstallTask(ctx, tv.ID)
	views, err := v.e.Services(ctx)
	if err != nil {
		t.Fatal(err)
	}
	viewsJSON, _ := json.Marshal(views)
	for what, text := range map[string]string{"task": raw.Data, "services": string(viewsJSON), "log": v.logs.String()} {
		for _, secret := range []string{pass, v.db.admin} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s holds a secret", what)
			}
		}
	}
	for _, view := range views.Services {
		if view.Name == "demo-sql" && strings.Join(view.IssuedPerConsumer, ",") != "database,password,url,username" {
			t.Fatalf("per consumer %v", view.IssuedPerConsumer)
		}
	}

	// Installed again on its data: the old account passes the check
	// command and is used again; no new role.
	if err := v.w.delete(ctx, "demo-sql-user", false); err != nil {
		t.Fatal(err)
	}
	v.installOK(t, Request{App: "demo-sql-user", ReuseData: true})
	again, _ := reg.GetBinding(ctx, "demo-sql-user", "db")
	if v.db.count() != 1 || again.Issued["password"] != pass {
		t.Fatalf("old account not used again: %d roles, %v", v.db.count(), again.Issued)
	}

	// Deleted with its data: the account is locked (revoke).
	if err := v.w.delete(ctx, "demo-sql-user", true); err != nil {
		t.Fatal(err)
	}
	if ws := v.e.ReleaseServices(ctx, "demo-sql-user", true); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if r, _ := v.db.role(user); r.login {
		t.Fatal("account not taken back")
	}
	// Taking back what is gone already ends as a success (finish_if exit 3).
	v.db.mu.Lock()
	delete(v.db.roles, user)
	v.db.mu.Unlock()
	pc, err := v.e.providerOf(ctx, sv)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.e.runRevoke(ctx, pc, "demo-sql-user", b.Issued); err != nil {
		t.Fatalf("revoke of a gone role: %v", err)
	}
	if len(v.db.wrongTarget) != 0 {
		t.Fatalf("commands aimed elsewhere: %v", v.db.wrongTarget)
	}
}

// The issue commands fail half-way (the role and database are made, the
// describe command fails): the install fails with CAT-INST-34 naming the
// step and the exit code, the password the database repeats is masked, and
// the revoke command, run with the names the issue_vars gave, takes back
// what was made.
func TestExecFlowIssueHalfWay(t *testing.T) {
	v := setupSQL(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-sql"})
	v.db.failDescribe = true
	tv := v.install(t, Request{App: "demo-sql-user"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeIssue {
		t.Fatalf("task %+v", tv.Fault)
	}
	msg := tv.Fault.Message
	if !strings.Contains(msg, "第 2 步（describe）") || !strings.Contains(msg, "命令的退出码是 1") || !strings.Contains(msg, "with password ***") {
		t.Fatalf("message %q", msg)
	}
	v.db.mu.Lock()
	var made []sqlRole
	for _, r := range v.db.roles {
		made = append(made, *r)
	}
	v.db.mu.Unlock()
	if len(made) != 1 || made[0].login {
		t.Fatalf("what was made half-way was not taken back: %+v", made)
	}
	if strings.Contains(msg, made[0].password) || strings.Contains(v.logs.String(), made[0].password) {
		t.Fatal("the password shows")
	}
	if ws := carriedRevoke(tv, "demo-sql-user"); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if bs, _ := v.reg(t).ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
}

// Two consumers of one database provider (the same manifest, installed
// under two names): each is issued its own number, account, password and
// database, and its connection string is worked out from its own
// credential only. Deleting one with its data takes back its account; the
// other's still passes the check command.
func TestExecFlowTwoConsumers(t *testing.T) {
	v := setupSQL(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-sql"})
	v.installOK(t, Request{App: "demo-sql-user"})
	v.installOK(t, Request{App: "demo-sql-user", Name: "demo-sql-user-b"})
	reg := v.reg(t)
	a, err := reg.GetBinding(ctx, "demo-sql-user", "db")
	if err != nil {
		t.Fatal(err)
	}
	b, err := reg.GetBinding(ctx, "demo-sql-user-b", "db")
	if err != nil || b.Service != "demo-sql" {
		t.Fatalf("binding %+v %v", b, err)
	}
	for _, k := range []string{catalog.IssueIDKey, "username", "password", "database", "database_oid"} {
		if a.Issued[k] == "" || a.Issued[k] == b.Issued[k] {
			t.Fatalf("%s: %q and %q", k, a.Issued[k], b.Issued[k])
		}
	}
	urls := map[string]string{}
	for app, iss := range map[string]map[string]string{"demo-sql-user": a.Issued, "demo-sql-user-b": b.Issued} {
		envs := envVars(t, v.env, app)
		want := "demo-sql://" + iss["username"] + ":" + iss["password"] + "@demo-sql:5000/" + iss["database"]
		if envs["DB_URL"].Value != want || envs["DB_PASSWORD"].Value != iss["password"] || envs["DB_USER"].Value != iss["username"] {
			t.Fatalf("%s env %+v", app, envs)
		}
		urls[app] = want
	}
	if strings.Contains(urls["demo-sql-user"], b.Issued["password"]) || strings.Contains(urls["demo-sql-user-b"], a.Issued["password"]) {
		t.Fatal("a connection string holds the other consumer's password")
	}
	if v.db.count() != 2 || len(v.db.migrated) != 2 || v.db.migrated[0] == v.db.migrated[1] {
		t.Fatalf("%d roles, migrated as %v", v.db.count(), v.db.migrated)
	}

	if err := v.w.delete(ctx, "demo-sql-user-b", true); err != nil {
		t.Fatal(err)
	}
	if ws := v.e.ReleaseServices(ctx, "demo-sql-user-b", true); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if r, _ := v.db.role(b.Issued["username"]); r.login {
		t.Fatal("the deleted consumer's account was not taken back")
	}
	if r, _ := v.db.role(a.Issued["username"]); !r.login || r.password != a.Issued["password"] {
		t.Fatalf("the other consumer's account was touched: %+v", r)
	}
	sv, err := reg.GetService(ctx, "demo-sql")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := v.e.providerOf(ctx, sv)
	if err != nil {
		t.Fatal(err)
	}
	if !v.e.stillGood(ctx, pc, "demo-sql-user", a.Issued) {
		t.Fatal("the other consumer's account no longer passes the check")
	}
	if _, err := reg.GetBinding(ctx, "demo-sql-user", "db"); err != nil {
		t.Fatalf("the other consumer's binding: %v", err)
	}
}

// The issue steps fail before any of them ran (the provider's container is
// not running now): nothing was made, so the revoke steps are not run and
// there is no "something may be left" warning. A failure after the first
// command ran (TestExecFlowIssueHalfWay) is still taken back.
func TestExecFlowIssueNothingRan(t *testing.T) {
	v := setupSQL(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-sql"})
	before := len(v.stepRequests())
	v.ex.mu.Lock()
	inner := v.ex.do
	v.ex.do = func(req runner.ExecRequest) (runner.ExecResult, error) {
		if req.App == "demo-sql" {
			return runner.ExecResult{}, runner.ErrNotFound
		}
		return inner(req)
	}
	v.ex.mu.Unlock()
	tv := v.install(t, Request{App: "demo-sql-user"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeIssue {
		t.Fatalf("task %+v", tv.Fault)
	}
	if !strings.Contains(tv.Fault.Message, "容器没在运行") {
		t.Fatalf("message %q", tv.Fault.Message)
	}
	// Only the first issue command was tried: no revoke command after it.
	var provider int
	for _, r := range v.stepRequests()[before:] {
		if r.App == "demo-sql" {
			provider++
		}
	}
	if provider != 1 {
		t.Fatalf("%d commands sent to the provider, want the one issue step that failed", provider)
	}
	if ws := carriedRevoke(tv, "demo-sql-user"); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if bs, _ := v.reg(t).ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
}

// An issue cut short by a restart: what the steps saved is lost, the
// issue_vars written as templates (the role's name) are worked out again
// for the revoke steps.
func TestExecRevokeInterrupted(t *testing.T) {
	v := setupSQL(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-sql"})
	reg := v.reg(t)
	v.db.mu.Lock()
	v.db.roles["af_0badc0de"] = &sqlRole{password: "x-0001", database: "af_0badc0de", login: true}
	v.db.mu.Unlock()
	if err := v.e.revokeInterrupted(ctx, reg, "demo-sql-user", IssuingRef{Use: "db", Service: "demo-sql", Provider: "demo-sql", IssueID: "0badc0de"}); err != nil {
		t.Fatal(err)
	}
	if r, _ := v.db.role("af_0badc0de"); r.login {
		t.Fatal("not taken back")
	}
}

func (v *sqlEnv) installOK(t *testing.T, req Request) TaskView {
	t.Helper()
	tv := v.install(t, req)
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("install %+v: %+v", req, tv.Fault)
	}
	return tv
}

func (v *sqlEnv) reg(t *testing.T) *Registry {
	t.Helper()
	r, err := v.e.Registry()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
