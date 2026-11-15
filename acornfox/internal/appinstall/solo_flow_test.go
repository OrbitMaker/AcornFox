package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// The B section's second round, carried out: who may be plugged into a
// service (uses[].dedicated, provides[].max_consumers), deleting a
// consumer's data (purge), stopping when an old credential fails its check
// (on_check_fail: stop), the readiness check (ready) and self.* in the
// provider's steps. Both test materials (internal/catalog/testdata/sqlsvc
// and sqlsolo), their manifests as written, behind a fake runner: demo-sql
// takes any number of consumers, demo-solo-sql one only and has the rest;
// demo-sql-user needs the service, demo-solo-sql-user needs one for itself
// alone. Nothing here names a real app or database.

const sqlsoloDir = "../catalog/testdata/sqlsolo"

func soloManifest(t testing.TB, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sqlsoloDir, "apps", id, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var (
	dropRoleRe = regexp.MustCompile(`DROP ROLE IF EXISTS "([^"]+)"`)
)

// fakeSolo plays the one-consumer database of sqlsolo (any provider app
// whose name starts with demo-solo-sql and is no consumer): its health
// check answers PONG, issue makes a role and a database, revoke locks the
// role, purge drops both, check logs in as the role through the provider's
// own name (self.host), and the readiness check counts the roles.
type fakeSolo struct {
	mu              sync.Mutex
	roles           map[string]*sqlRole
	order           []string // lock:<role>, drop:<role>
	failPurge       bool
	failCreate      bool          // issue makes the role and database, then fails
	stopAfterCreate bool          // once a create worked, the provider's container is gone: later commands find none
	stopped         error         // what a command gets once it is (nil: it is there)
	notReady        bool          // the role count says 0 whatever there is
	readyGate       chan struct{} // closed: the count goes on; nil: no wait
	readyWait       time.Duration // how long a count waits for readyGate
	pingWait        time.Duration // how long a check (--ping) takes before it answers
	readyHosts      []string
	checkHosts      []string
	steps           int // commands other than health checks
}

// soloGone is what a command to the provider gets once stopAfterCreate took
// effect: the container is not there (a stopped one, runner.ErrStopped, is
// the same path after five tries two seconds apart; too slow for here).
var soloGone error = runner.ErrNotFound

func isSoloProvider(app string) bool {
	return strings.HasPrefix(app, "demo-solo-sql") && !strings.Contains(app, "user")
}

func (d *fakeSolo) exec(req runner.ExecRequest) (runner.ExecResult, error) {
	res := func(code int, stdout, stderr string) (runner.ExecResult, error) {
		return runner.ExecResult{ExitCode: code, Stdout: []byte(stdout), Stderr: []byte(stderr)}, nil
	}
	if req.Command[0] == "demo-sql-ping" {
		return res(0, "PONG\n", "")
	}
	last := req.Command[len(req.Command)-1]
	if last == "--ping" {
		d.mu.Lock()
		w := d.pingWait
		d.mu.Unlock()
		time.Sleep(w)
	}
	if last == "--count-roles" {
		d.mu.Lock()
		d.steps++
		d.readyHosts = append(d.readyHosts, argAfter(req.Command, "--host"))
		gate, wait := d.readyGate, d.readyWait
		d.mu.Unlock()
		if gate != nil {
			select {
			case <-gate:
			case <-time.After(wait):
			}
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		n := len(d.roles)
		if d.notReady {
			n = 0
		}
		return res(0, `{"data": {"roles": `+strconv.Itoa(n)+`}}`+"\n", "")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped != nil {
		return runner.ExecResult{}, d.stopped
	}
	d.steps++
	stdin := string(req.Stdin)
	switch {
	case argAfter(req.Command, "--at-least") != "":
		n, _ := strconv.Atoi(argAfter(req.Command, "--count"))
		if n >= 1 {
			return res(0, "", "")
		}
		return res(1, "", "not enough roles: "+strconv.Itoa(n))
	case strings.Contains(stdin, "CREATE ROLE"):
		m := createRe.FindStringSubmatch(stdin)
		if m == nil || req.Env["DEMO_SQL_PASSWORD"] == "" {
			return res(1, "", "ERROR: syntax error")
		}
		if _, have := d.roles[m[1]]; have {
			return res(1, "", "ERROR: role exists")
		}
		d.roles[m[1]] = &sqlRole{password: m[2], database: m[3], login: true}
		if d.failCreate {
			return res(1, "CREATE ROLE\nCREATE DATABASE\n", "ERROR: could not grant")
		}
		if d.stopAfterCreate {
			d.stopped = soloGone
		}
		return res(0, "CREATE ROLE\nCREATE DATABASE\n", "")
	case strings.Contains(stdin, "NOLOGIN"):
		m := lockRe.FindStringSubmatch(stdin)
		d.order = append(d.order, "lock:"+m[1])
		r, have := d.roles[m[1]]
		if !have {
			return res(3, "", "ERROR: role does not exist")
		}
		r.login = false
		return res(0, "ALTER ROLE\n", "")
	case strings.Contains(stdin, "DROP DATABASE"):
		m := dropRoleRe.FindStringSubmatch(stdin)
		d.order = append(d.order, "drop:"+m[1])
		if d.failPurge {
			// Like a real database: the failing statement is echoed.
			return res(1, "", "ERROR: database is being accessed by other users\nSTATEMENT: "+stdin)
		}
		delete(d.roles, m[1])
		return res(0, "DROP DATABASE\nDROP ROLE\n", "")
	case last == "--ping":
		host := argAfter(req.Command, "--host")
		d.checkHosts = append(d.checkHosts, host)
		r, have := d.roles[argAfter(req.Command, "--user")]
		// Like the real one through 127.0.0.1 it would let anything in: the
		// check counts only when it comes in through the provider's name.
		if host != req.App || !have || !r.login || r.password != req.Env["DEMO_SQL_PASSWORD"] || r.database != argAfter(req.Command, "--database") {
			return res(2, "", "FATAL: password authentication failed")
		}
		return res(0, "PONG\n", "")
	}
	return res(127, "", "unknown command")
}

func (d *fakeSolo) role(name string) (sqlRole, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.roles[name]
	if !ok {
		return sqlRole{}, false
	}
	return *r, true
}

// mixedDB routes a command to the fake that plays its app: demo-sql (fakeSQL
// of exec_flow_test.go), a demo-solo-sql provider (fakeSolo), or a consumer.
// createGate, when set, holds every issue command to demo-sql or a
// demo-solo-sql provider until it has been called that many times (or a
// while has passed): two installs then are both past matching and issuing
// at once.
type mixedDB struct {
	sql  *fakeSQL
	solo *fakeSolo

	mu         sync.Mutex
	createGate int
	creates    int
	release    chan struct{}
}

func (m *mixedDB) exec(req runner.ExecRequest) (runner.ExecResult, error) {
	if req.App == "demo-sql" || isSoloProvider(req.App) {
		if strings.Contains(string(req.Stdin), "CREATE ROLE") {
			m.mu.Lock()
			gate, rel := m.createGate, m.release
			if gate > 0 {
				m.creates++
				if m.creates == gate {
					close(rel)
				}
			}
			m.mu.Unlock()
			if gate > 0 {
				select {
				case <-rel:
				case <-time.After(5 * time.Second):
				}
			}
		}
	}
	switch {
	case req.App == "demo-sql":
		return m.sql.exec(req)
	case isSoloProvider(req.App):
		return m.solo.exec(req)
	case req.Command[0] == "demo-app": // demo-sql-user's init
		return runner.ExecResult{Stdout: []byte("migrated\n")}, nil
	}
	return runner.ExecResult{ExitCode: 127, Stderr: []byte("unknown command")}, nil
}

type soloEnv struct {
	*env
	db   *mixedDB
	ex   *execRunner
	logs *lockedBuffer
}

func setupSolo(t *testing.T) *soloEnv {
	t.Helper()
	return setupSoloProvider(t, soloManifest(t, "demo-solo-sql"))
}

// setupSoloProvider is setupSolo with provider as the manifest of
// demo-solo-sql (the test material's, changed by a test).
func setupSoloProvider(t *testing.T, provider string) *soloEnv {
	t.Helper()
	manifests := []string{sqlManifest(t, "demo-sql"), sqlManifest(t, "demo-sql-user"), provider, soloManifest(t, "demo-solo-sql-user")}
	logs := &lockedBuffer{}
	db := &mixedDB{sql: &fakeSQL{roles: map[string]*sqlRole{}}, solo: &fakeSolo{roles: map[string]*sqlRole{}}}
	ex := &execRunner{do: db.exec}
	v, _ := setupSnapshot(t, bundleFS(typedBundle(t, sqlsvcDir, nil, manifests...)), func(c *Config) {
		c.Version = "0.6.0"
		c.Log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		c.Runner = &execWorld{fakeWorld: c.Runner.(*fakeWorld), execRunner: ex}
	})
	db.sql.st = v.w.st
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	t.Cleanup(srv.Close)
	v.w.mu.Lock()
	v.w.servers["demo-sql-user"] = srv
	v.w.servers["demo-solo-sql-user"] = srv
	v.w.mu.Unlock()
	return &soloEnv{env: v, db: db, ex: ex, logs: logs}
}

func (v *soloEnv) installOK(t *testing.T, req Request) TaskView {
	t.Helper()
	tv := v.install(t, req)
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("install %+v: %+v", req, tv.Fault)
	}
	return tv
}

func (v *soloEnv) plan(t *testing.T, req Request) PlanView {
	t.Helper()
	pv, err := v.e.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return pv
}

func (v *soloEnv) service(t *testing.T, name string) ServiceView {
	t.Helper()
	sv, err := v.e.Service(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return sv
}

func (v *soloEnv) binding(t *testing.T, app string) Binding {
	t.Helper()
	reg, err := v.e.Registry()
	if err != nil {
		t.Fatal(err)
	}
	b, err := reg.GetBinding(context.Background(), app, "db")
	if err != nil {
		t.Fatalf("binding of %s: %v", app, err)
	}
	return b
}

// deleteWithData deletes app with its data the way the API does, and
// returns the warnings.
func (v *soloEnv) deleteWithData(t *testing.T, app string) []string {
	t.Helper()
	ctx := context.Background()
	if err := v.w.delete(ctx, app, true); err != nil {
		t.Fatal(err)
	}
	return v.e.ReleaseServices(ctx, app, true)
}

func onlyNeed(t *testing.T, pv PlanView) PlanNeed {
	t.Helper()
	if len(pv.Apps) != 1 || len(pv.Apps[0].Needs) != 1 {
		t.Fatalf("plan %+v", pv)
	}
	return pv.Apps[0].Needs[0]
}

func codes(pv PlanView) []string {
	var out []string
	for _, p := range pv.Problems {
		out = append(out, p.Code)
	}
	return out
}

func startFault(t *testing.T, v *soloEnv, req Request) *Fault {
	t.Helper()
	_, err := v.e.StartInstall(context.Background(), req)
	if err == nil {
		t.Fatalf("install %+v accepted", req)
	}
	return faultOf(t, err)
}

// A dedicated need takes only a service no other app is plugged into, and
// once it holds one nobody else is let in: matched against, said in the
// plan (how) with who is in the way, refused when named with --use
// (CAT-INST-38). An app deleted with its data kept still holds its place.
// The service view says who holds it and that no slot is left.
func TestDedicatedNeed(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-sql"})
	v.installOK(t, Request{App: "demo-sql-user"})

	// Somebody uses it: the dedicated need finds nothing it may take.
	pv := v.plan(t, Request{App: "demo-solo-sql-user"})
	n := onlyNeed(t, pv)
	if pv.CanInstall || strings.Join(codes(pv), ",") != CodeNoService || n.Status != NeedRequiredMissing || !n.Dedicated {
		t.Fatalf("plan %+v", pv)
	}
	if !strings.Contains(strings.Join(n.How, "；"), "服务 demo-sql 已经有应用 demo-sql-user 在用，而这个需要要独占一条没有别的应用在用的服务") ||
		!strings.Contains(pv.Problems[0].Message, "已经有应用 demo-sql-user 在用") {
		t.Fatalf("how %v / %s", n.How, pv.Problems[0].Message)
	}
	if f := startFault(t, v, Request{App: "demo-solo-sql-user"}); f.Code != CodeNoService {
		t.Fatalf("%+v", f)
	}
	if f := startFault(t, v, Request{App: "demo-solo-sql-user", Use: map[string]string{"db": "demo-sql"}}); f.Code != CodeTaken || f.HTTPStatus() != http.StatusConflict ||
		!strings.Contains(f.Message, "指定用服务 demo-sql，但它已经有应用 demo-sql-user 在用") || f.Hint == "" {
		t.Fatalf("%+v", f)
	}
	if sv := v.service(t, "demo-sql"); sv.DedicatedTo != "" || sv.MaxConsumers != 0 || sv.SlotsLeft != nil {
		t.Fatalf("view %+v", sv)
	}

	// The other consumer goes with its data: the dedicated need takes it.
	if ws := v.deleteWithData(t, "demo-sql-user"); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if pv := v.plan(t, Request{App: "demo-solo-sql-user"}); !pv.CanInstall || onlyNeed(t, pv).Service != "demo-sql" {
		t.Fatalf("plan %+v", pv)
	}
	v.installOK(t, Request{App: "demo-solo-sql-user"})
	if b := v.binding(t, "demo-solo-sql-user"); !b.Dedicated || b.Service != "demo-sql" {
		t.Fatalf("binding %+v", b)
	}
	sv := v.service(t, "demo-sql")
	if sv.DedicatedTo != "demo-solo-sql-user" || sv.SlotsLeft == nil || *sv.SlotsLeft != 0 || len(sv.UsedBy) != 1 || !sv.UsedBy[0].Dedicated {
		t.Fatalf("view %+v", sv)
	}
	raw, _ := json.Marshal(sv)
	for _, want := range []string{`"dedicated_to":"demo-solo-sql-user"`, `"slots_left":0`, `"max_consumers":0`, `"dedicated":true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON lacks %s: %s", want, raw)
		}
	}

	// Held: nobody else, not even a need that does not want it alone.
	pv = v.plan(t, Request{App: "demo-sql-user", Name: "demo-sql-user-b"})
	n = onlyNeed(t, pv)
	if pv.CanInstall || n.Status != NeedRequiredMissing || !strings.Contains(strings.Join(n.How, "；"), "服务 demo-sql 已经被应用 demo-solo-sql-user 独占") {
		t.Fatalf("plan %+v", pv)
	}
	if f := startFault(t, v, Request{App: "demo-sql-user", Name: "demo-sql-user-b", Use: map[string]string{"db": "demo-sql"}}); f.Code != CodeTaken ||
		!strings.Contains(f.Message, "已经被应用 demo-solo-sql-user 独占") {
		t.Fatalf("%+v", f)
	}

	// Deleted with its data kept, it still holds its place (it comes back
	// to it); the plan says so. Installed again on its data, it takes it
	// again: its own binding does not count against it.
	if err := v.w.delete(ctx, "demo-solo-sql-user", false); err != nil {
		t.Fatal(err)
	}
	pv = v.plan(t, Request{App: "demo-sql-user", Name: "demo-sql-user-b"})
	if pv.CanInstall || !strings.Contains(strings.Join(onlyNeed(t, pv).How, "；"), "demo-solo-sql-user（已删、数据还留着，装回来时要接回它） 独占") {
		t.Fatalf("plan %+v", pv)
	}
	v.installOK(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	if b := v.binding(t, "demo-solo-sql-user"); !b.Dedicated || b.Service != "demo-sql" {
		t.Fatalf("binding %+v", b)
	}
}

// A service the user registered is under the same rule: a dedicated need
// takes it, and then nobody else does.
func TestDedicatedUserService(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	if _, err := v.e.AddService(ctx, AddServiceRequest{Name: "mydb", Kind: "demo_sql", Values: ServiceValues{
		"host": "db.example.com", "database": "app", "username": "app", "password": "pw-0001-xyz"}}); err != nil {
		t.Fatal(err)
	}
	v.installOK(t, Request{App: "demo-solo-sql-user"})
	if b := v.binding(t, "demo-solo-sql-user"); !b.Dedicated || b.Service != "mydb" {
		t.Fatalf("binding %+v", b)
	}
	if sv := v.service(t, "mydb"); sv.DedicatedTo != "demo-solo-sql-user" || sv.SlotsLeft == nil || *sv.SlotsLeft != 0 {
		t.Fatalf("view %+v", sv)
	}
	if pv := v.plan(t, Request{App: "demo-sql-user"}); pv.CanInstall || !strings.Contains(strings.Join(onlyNeed(t, pv).How, "；"), "服务 mydb 已经被应用 demo-solo-sql-user 独占") {
		t.Fatalf("plan %+v", pv)
	}
}

// Two dedicated needs installed at the same time, both past matching and
// issuing before either has written its binding: the store lets one of
// them write it; the other fails with CAT-INST-38 and its credential is
// taken back.
func TestDedicatedRace(t *testing.T) {
	v := setupSolo(t)
	v.installOK(t, Request{App: "demo-sql"})
	v.db.mu.Lock()
	v.db.createGate, v.db.release = 2, make(chan struct{})
	v.db.mu.Unlock()
	a, err := v.e.StartInstall(context.Background(), Request{App: "demo-solo-sql-user"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.e.StartInstall(context.Background(), Request{App: "demo-solo-sql-user", Name: "demo-solo-sql-user-b"})
	if err != nil {
		t.Fatalf("the second was refused before it issued: %v", err)
	}
	ta, tb := v.wait(t, a.ID), v.wait(t, b.ID)
	won, lost := ta, tb
	if ta.Status != state.TaskSucceeded {
		won, lost = tb, ta
	}
	if won.Status != state.TaskSucceeded || lost.Status != state.TaskFailed || lost.Fault == nil || lost.Fault.Code != CodeTaken ||
		!strings.Contains(lost.Fault.Message, "同时在装的别的应用先接上了") {
		t.Fatalf("won %s %+v / lost %s %+v", won.Status, won.Fault, lost.Status, lost.Fault)
	}
	winner, loser := won.Apps[0].App, lost.Apps[0].App
	reg, _ := v.e.Registry()
	bs, _ := reg.ListBindings(context.Background(), state.BindingFilter{Service: "demo-sql"})
	if len(bs) != 1 || bs[0].App != winner || !bs[0].Dedicated {
		t.Fatalf("bindings %+v", bs)
	}
	// Two roles were made; the loser's is taken back, the winner's works.
	v.db.sql.mu.Lock()
	var locked, open int
	for _, r := range v.db.sql.roles {
		if r.login {
			open++
		} else {
			locked++
		}
	}
	v.db.sql.mu.Unlock()
	if open != 1 || locked != 1 || len(carriedRevoke(lost, loser)) != 0 {
		t.Fatalf("roles open %d locked %d, carried %v", open, locked, lost.Apps)
	}
}

// max_consumers: the one-consumer provider takes one app, then is full;
// the plan says so and --use is refused. The app that has it may be
// installed again on its data (its own binding does not count), and its
// old account passes the check, which connects through the provider's own
// name (self.host in a check step).
func TestMaxConsumers(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	if sv := v.service(t, "demo-solo-sql"); sv.MaxConsumers != 1 || sv.SlotsLeft == nil || *sv.SlotsLeft != 1 || sv.DedicatedTo != "" {
		t.Fatalf("view %+v", sv)
	}
	v.installOK(t, Request{App: "demo-sql-user"})
	sv := v.service(t, "demo-solo-sql")
	if sv.SlotsLeft == nil || *sv.SlotsLeft != 0 {
		t.Fatalf("view %+v", sv)
	}
	pv := v.plan(t, Request{App: "demo-sql-user", Name: "demo-sql-user-b"})
	if pv.CanInstall || !strings.Contains(strings.Join(onlyNeed(t, pv).How, "；"), "服务 demo-solo-sql 已经接满：提供它的应用只让同时接 1 个，现在接着 demo-sql-user") {
		t.Fatalf("plan %+v", pv)
	}
	if f := startFault(t, v, Request{App: "demo-sql-user", Name: "demo-sql-user-b", Use: map[string]string{"db": "demo-solo-sql"}}); f.Code != CodeTaken || !strings.Contains(f.Message, "已经接满") {
		t.Fatalf("%+v", f)
	}
	// A second provider of the kind is free: the second consumer takes it.
	v.installOK(t, Request{App: "demo-sql"})
	if pv := v.plan(t, Request{App: "demo-sql-user", Name: "demo-sql-user-b"}); !pv.CanInstall || onlyNeed(t, pv).Service != "demo-sql" {
		t.Fatalf("plan %+v", pv)
	}

	old := v.binding(t, "demo-sql-user")
	if err := v.w.delete(ctx, "demo-sql-user", false); err != nil {
		t.Fatal(err)
	}
	tv := v.installOK(t, Request{App: "demo-sql-user", ReuseData: true, Use: map[string]string{"db": "demo-solo-sql"}})
	if again := v.binding(t, "demo-sql-user"); again.Service != "demo-solo-sql" || again.Issued["password"] != old.Issued["password"] {
		t.Fatalf("binding %+v", again)
	}
	v.db.solo.mu.Lock()
	hosts := append([]string(nil), v.db.solo.checkHosts...)
	v.db.solo.mu.Unlock()
	if len(hosts) == 0 || hosts[len(hosts)-1] != "demo-solo-sql" {
		t.Fatalf("the check connected through %v", hosts)
	}
	if !strings.Contains(strings.Join(tv.Apps[0].Carried, "；"), "接着用以前 demo-solo-sql 发给它的凭据") {
		t.Fatalf("carried %v", tv.Apps[0].Carried)
	}
}

// Deleted with its data, a consumer's account is taken back (revoke) and
// then its database dropped (purge), in that order. A purge that fails is a
// CAT-INST-35 warning saying what to delete by hand, the database's
// password not in it. Deleted with its data kept, neither runs.
func TestPurgeOnDeleteWithData(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.installOK(t, Request{App: "demo-solo-sql-user"})
	b := v.binding(t, "demo-solo-sql-user")
	user := b.Issued["username"]
	if user == "" || b.Issued["database"] == "" {
		t.Fatalf("issued %v", b.Issued)
	}
	if err := v.w.delete(ctx, "demo-solo-sql-user", false); err != nil {
		t.Fatal(err)
	}
	if ws := v.e.ReleaseServices(ctx, "demo-solo-sql-user", false); len(ws) != 0 {
		t.Fatalf("kept data: %v", ws)
	}
	if r, ok := v.db.solo.role(user); !ok || !r.login || len(v.db.solo.order) != 0 {
		t.Fatalf("kept data touched: %+v %v", r, v.db.solo.order)
	}
	v.installOK(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	if ws := v.deleteWithData(t, "demo-solo-sql-user"); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if _, ok := v.db.solo.role(user); ok || strings.Join(v.db.solo.order, ",") != "lock:"+user+",drop:"+user {
		t.Fatalf("roles %v order %v", v.db.solo.roles, v.db.solo.order)
	}

	// The purge fails: said, with what to delete where; the binding goes.
	v.installOK(t, Request{App: "demo-solo-sql-user"})
	b = v.binding(t, "demo-solo-sql-user")
	v.db.solo.mu.Lock()
	v.db.solo.failPurge = true
	v.db.solo.mu.Unlock()
	ws := v.deleteWithData(t, "demo-solo-sql-user")
	if len(ws) != 1 || !strings.HasPrefix(ws[0], CodeRevoke+" demo-solo-sql-user 连数据一起删了，但没能删掉 demo-solo-sql 里给它建的数据") ||
		!strings.Contains(ws[0], "编号 "+b.Issued[catalog.IssueIDKey]) || !strings.Contains(ws[0], "手工删掉") {
		t.Fatalf("warnings %v", ws)
	}
	if strings.Contains(ws[0], b.Issued["password"]) || strings.Contains(v.logs.String(), b.Issued["password"]) {
		t.Fatal("the password shows")
	}
	reg, _ := v.e.Registry()
	if _, err := reg.GetBinding(ctx, "demo-solo-sql-user", "db"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("binding left: %v", err)
	}
}

// What purge reads of a credential is kept with it, also a key nothing
// else reads.
func TestCredentialKeysPurge(t *testing.T) {
	pv := catalog.Provide{
		Revoke: []catalog.Step{{Exec: &catalog.StepExec{Command: []string{"lock"}, Stdin: "{{issued.username}}"}}},
		Purge:  []catalog.Step{{Exec: &catalog.StepExec{Command: []string{"drop"}, Stdin: "{{issued.volume_tag}}"}}},
	}
	keep := credentialKeys(pv)
	if !keep["volume_tag"] || !keep["username"] || !keep[catalog.IssueIDKey] || keep["session"] {
		t.Fatalf("%v", keep)
	}
}

// on_check_fail: stop. Installed again on its data, the consumer's old
// account fails the check (its password was changed in the provider): the
// install stops with CAT-INST-37, nothing new is issued and the old binding
// stays as it was. Once the account works again, the same install goes
// through on the old account.
func TestCheckFailStops(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.installOK(t, Request{App: "demo-solo-sql-user"})
	old := v.binding(t, "demo-solo-sql-user")
	user, pass := old.Issued["username"], old.Issued["password"]
	if err := v.w.delete(ctx, "demo-solo-sql-user", false); err != nil {
		t.Fatal(err)
	}
	v.db.solo.mu.Lock()
	v.db.solo.roles[user].password = "changed-by-hand"
	v.db.solo.mu.Unlock()

	// The plan checks the old credential first (read-only) and says so.
	pv := v.plan(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	if pv.CanInstall || strings.Join(codes(pv), ",") != CodeCheckStop || !strings.Contains(pv.Problems[0].Message, "照现在的样子装会停下") ||
		strings.Contains(pv.Problems[0].Message, pass) {
		t.Fatalf("plan %+v", pv)
	}

	tv := v.install(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeCheckStop {
		t.Fatalf("task %s %+v", tv.Status, tv.Fault)
	}
	msg := tv.Fault.Message
	for _, want := range []string{"demo-solo-sql-user 的“db” 以前从服务 demo-solo-sql 拿到的凭据这次核对没通过", "第 1 步（connect）", "命令的退出码是 2",
		"不自动换一份", "原来的接入原样留着", "acornfox status demo-solo-sql", "--use db=服务名"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q: %s", want, msg)
		}
	}
	if strings.Contains(msg, pass) || strings.Contains(v.logs.String(), pass) {
		t.Fatal("the password shows")
	}
	if tv.Fault.Hint == "" {
		t.Fatal("no hint")
	}
	again := v.binding(t, "demo-solo-sql-user")
	if again.Service != "demo-solo-sql" || again.Issued["password"] != pass || again.Issued[catalog.IssueIDKey] != old.Issued[catalog.IssueIDKey] {
		t.Fatalf("binding changed: %+v", again)
	}
	v.db.solo.mu.Lock()
	roles := len(v.db.solo.roles)
	v.db.solo.mu.Unlock()
	if roles != 1 {
		t.Fatalf("%d roles: something new was issued", roles)
	}
	reg, _ := v.e.Registry()
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{App: "demo-solo-sql-user"}); len(bs) != 1 || bs[0].UseName != "db" {
		t.Fatalf("bindings %+v", bs)
	}

	v.db.solo.mu.Lock()
	v.db.solo.roles[user].password = pass
	v.db.solo.mu.Unlock()
	if pv := v.plan(t, Request{App: "demo-solo-sql-user", ReuseData: true}); !pv.CanInstall {
		t.Fatalf("plan %+v", pv)
	}
	v.installOK(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	if b := v.binding(t, "demo-solo-sql-user"); b.Issued["password"] != pass {
		t.Fatalf("binding %+v", b)
	}
}

// The readiness check: run when the services are looked at, in a plan and
// once an install has succeeded; never in a stopped provider. Not ready is
// status not_ready with the manifest's sentence and what failed; it does
// not stop matching: the plan's need tells it, and so does the finished
// install (must_tell). self.host in a readiness step is the provider's own
// name.
func TestReadyCheck(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	const say = "样例数据库还没建好管理账号：重装一次这个应用"

	// No role yet: not ready.
	sv := v.service(t, "demo-solo-sql")
	if sv.Status != ServiceNotReady || sv.StatusLabel != "还不能用" || sv.ReadySay != say ||
		!strings.Contains(sv.ReadyDetail, "第 2 步（at-least-one）") || !strings.Contains(sv.ReadyDetail, "not enough roles: 0") {
		t.Fatalf("view %+v", sv)
	}
	list, err := v.e.Services(ctx)
	if err != nil || len(list.Services) != 1 || list.Services[0].Status != ServiceNotReady {
		t.Fatalf("list %+v %v", list, err)
	}
	v.db.solo.mu.Lock()
	hosts := append([]string(nil), v.db.solo.readyHosts...)
	v.db.solo.mu.Unlock()
	if len(hosts) == 0 || hosts[0] != "demo-solo-sql" {
		t.Fatalf("ready ran with host %v", hosts)
	}

	// Matched all the same; the plan tells.
	pv := v.plan(t, Request{App: "demo-sql-user"})
	n := onlyNeed(t, pv)
	want := "服务 demo-solo-sql 现在还不能用：" + say + "。用到它的功能要等它能用了才行（acornfox services show demo-solo-sql 看情况）"
	if !pv.CanInstall || n.Status != NeedMatched || n.Service != "demo-solo-sql" || strings.Join(n.Tell, "|") != want {
		t.Fatalf("plan %+v", pv)
	}

	// Installed: one role now, ready; nothing to tell.
	tv := v.installOK(t, Request{App: "demo-sql-user"})
	if len(tv.Apps[0].Tell) != 0 {
		t.Fatalf("tell %v", tv.Apps[0].Tell)
	}
	if sv := v.service(t, "demo-solo-sql"); sv.Status != ServiceOK || sv.ReadySay != "" {
		t.Fatalf("view %+v", sv)
	}

	// Not ready when the install ends: must_tell.
	v.db.solo.mu.Lock()
	v.db.solo.notReady = true
	v.db.solo.mu.Unlock()
	if err := v.w.delete(ctx, "demo-sql-user", false); err != nil {
		t.Fatal(err)
	}
	tv = v.installOK(t, Request{App: "demo-sql-user", ReuseData: true})
	if strings.Join(tv.Apps[0].Tell, "|") != want {
		t.Fatalf("tell %v", tv.Apps[0].Tell)
	}
	view, err := v.e.Task(ctx, tv.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(view)
	if !strings.Contains(string(raw), "现在还不能用") {
		t.Fatalf("task %s", raw)
	}

	// A stopped provider: said as before, nothing run in it.
	if _, err := v.w.st.UpdateApp(ctx, "demo-solo-sql", func(a *state.App) error { a.Desired = state.DesiredStopped; return nil }); err != nil {
		t.Fatal(err)
	}
	v.db.solo.mu.Lock()
	before := v.db.solo.steps
	v.db.solo.mu.Unlock()
	if sv := v.service(t, "demo-solo-sql"); sv.Status != ServiceProviderStopped || sv.ReadySay != "" {
		t.Fatalf("view %+v", sv)
	}
	v.db.solo.mu.Lock()
	after := v.db.solo.steps
	v.db.solo.mu.Unlock()
	if after != before {
		t.Fatalf("%d commands run in a stopped provider", after-before)
	}
}

// Each service's readiness check has its own short time limit and they run
// side by side: two providers whose checks only go on once both have
// started are both ready (one after the other they would both time out);
// a check that does not end in time is not ready, said so.
func TestReadyChecksSideBySide(t *testing.T) {
	saved := readyTimeout
	t.Cleanup(func() { readyTimeout = saved })
	readyTimeout = 2 * time.Second
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.installOK(t, Request{App: "demo-solo-sql", Name: "demo-solo-sql-2"})
	v.db.solo.mu.Lock()
	v.db.solo.roles["af_seed"] = &sqlRole{password: "x", database: "af_seed", login: true}
	gate := make(chan struct{})
	v.db.solo.readyGate, v.db.solo.readyWait = gate, 5*time.Second
	v.db.solo.mu.Unlock()
	go func() {
		// Open the gate once both checks have started.
		for i := 0; i < 400; i++ {
			v.db.solo.mu.Lock()
			n := len(v.db.solo.readyHosts)
			v.db.solo.mu.Unlock()
			if n >= 2 {
				close(gate)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	list, err := v.e.Services(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list.Services {
		if s.Status != ServiceOK {
			t.Fatalf("%s: %+v", s.Name, s)
		}
	}
	v.db.solo.mu.Lock()
	hosts := strings.Join(v.db.solo.readyHosts, ",")
	v.db.solo.mu.Unlock()
	if hosts != "demo-solo-sql,demo-solo-sql-2" && hosts != "demo-solo-sql-2,demo-solo-sql" {
		t.Fatalf("hosts %s", hosts)
	}

	// Too slow: not ready, and it says the check did not end in time.
	readyTimeout = 200 * time.Millisecond
	v.db.solo.mu.Lock()
	v.db.solo.readyGate, v.db.solo.readyWait = make(chan struct{}), time.Second // never opened
	v.db.solo.mu.Unlock()
	start := time.Now()
	sv := v.service(t, "demo-solo-sql")
	if sv.Status != ServiceNotReady || !strings.Contains(sv.ReadyDetail, "就绪检查在") || !strings.Contains(sv.ReadyDetail, "内没做完") {
		t.Fatalf("view %+v", sv)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %s", d)
	}
}

// self.* resolves in the provider's steps (not only in values): the
// provider's name on the app network and its endpoint's port.
func TestProviderStepsSelf(t *testing.T) {
	m, err := catalog.ParseManifest([]byte(soloManifest(t, "demo-solo-sql")))
	if err != nil {
		t.Fatal(err)
	}
	pc := providerConn{m: m, pv: m.Provides[0], app: state.App{Name: "db-1"}, vals: Values{}}
	for ref, want := range map[string]string{"self.host": "db-1", "self.port": "5000"} {
		if got, err := pc.lookup(ref); err != nil || got != want {
			t.Errorf("%s = %q %v", ref, got, err)
		}
	}
	if _, err := pc.lookup("self.internal_url"); err == nil {
		t.Error("a tcp endpoint has no http:// address")
	}
	if _, err := pc.lookup("self.name"); err == nil || !strings.Contains(err.Error(), "self.host") {
		t.Errorf("%v", err)
	}
	if env := pc.env(GroupReady, "", nil, ""); env.Consumer != nil || env.Group != GroupReady {
		t.Errorf("%+v", env)
	}
	if env := pc.env(GroupCheck, "c1", map[string]string{"x": "y"}, ""); env.Consumer == nil || env.Consumer.Name != "c1" {
		t.Errorf("%+v", env)
	}
}

// A plan's checks run once everything is matched, all side by side: each
// service's readiness check once, however many needs are matched to it, and
// every need matched to a service that is not ready gets the sentence (the
// second one too). Two providers whose checks only go on once both have
// started are both checked in about one time limit, not one each.
func TestPlanChecksOncePerServiceSideBySide(t *testing.T) {
	saved := readyTimeout
	t.Cleanup(func() { readyTimeout = saved })
	readyTimeout = 2 * time.Second
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.installOK(t, Request{App: "demo-solo-sql", Name: "demo-solo-sql-2"})
	v.db.solo.mu.Lock()
	v.db.solo.notReady = true
	v.db.solo.readyHosts = nil
	gate := make(chan struct{})
	v.db.solo.readyGate, v.db.solo.readyWait = gate, 5*time.Second
	v.db.solo.mu.Unlock()
	go func() {
		for i := 0; i < 400; i++ {
			v.db.solo.mu.Lock()
			n := len(v.db.solo.readyHosts)
			v.db.solo.mu.Unlock()
			if n >= 2 {
				close(gate)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	a := &assessment{collect: true, kinds: v.e.planKinds(), usedPicks: map[string]bool{}}
	if err := v.e.loadServices(ctx, a); err != nil {
		t.Fatal(err)
	}
	need := func(svc string) *needMatch {
		return &needMatch{use: catalog.Use{Name: "db", Kind: "demo_sql"}, status: NeedMatched, service: svc}
	}
	p1 := &planned{name: "c1", needs: []*needMatch{need("demo-solo-sql")}}
	p2 := &planned{name: "c2", needs: []*needMatch{need("demo-solo-sql"), need("demo-solo-sql-2")}}
	a.plan = []*planned{p1, p2}
	start := time.Now()
	v.e.planChecks(ctx, a)
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("took %s: the checks ran one after the other", d)
	}
	v.db.solo.mu.Lock()
	hosts := append([]string(nil), v.db.solo.readyHosts...)
	v.db.solo.mu.Unlock()
	if len(hosts) != 2 {
		t.Fatalf("readiness checks ran %d times (%v), want once per service", len(hosts), hosts)
	}
	for _, c := range []struct {
		n   *needMatch
		svc string
	}{{p1.needs[0], "demo-solo-sql"}, {p2.needs[0], "demo-solo-sql"}, {p2.needs[1], "demo-solo-sql-2"}} {
		if len(c.n.tell) != 1 || !strings.HasPrefix(c.n.tell[0], "服务 "+c.svc+" 现在还不能用：") {
			t.Errorf("need on %s: tell %v", c.svc, c.n.tell)
		}
	}
	for name, rr := range a.ready {
		if rr.ready || strings.Contains(rr.detail, "没做完") {
			t.Errorf("%s: %+v (it waited for the other)", name, rr)
		}
	}
	if s := a.service("demo-solo-sql"); s.health.Status != ServiceNotReady {
		t.Errorf("status %s", s.health.Status)
	}
}

// The plan's check of an old credential that does not get done in time is
// no "did not pass": the plan can still install and its need says the
// install checks again; the install, which waits, goes through on the old
// credential.
func TestPrecheckLateIsNoStop(t *testing.T) {
	saved := readyTimeout
	t.Cleanup(func() { readyTimeout = saved })
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.installOK(t, Request{App: "demo-solo-sql-user"})
	old := v.binding(t, "demo-solo-sql-user")
	if err := v.w.delete(ctx, "demo-solo-sql-user", false); err != nil {
		t.Fatal(err)
	}
	readyTimeout = 200 * time.Millisecond
	v.db.solo.mu.Lock()
	v.db.solo.pingWait = time.Second
	v.db.solo.mu.Unlock()
	pv := v.plan(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	n := onlyNeed(t, pv)
	tell := strings.Join(n.Tell, "|")
	if !pv.CanInstall || len(pv.Problems) != 0 || !strings.Contains(tell, "看计划时没能核对完 demo-solo-sql-user 的“db” 以前从服务 demo-solo-sql 拿到的凭据") ||
		!strings.Contains(tell, "装的时候会再核对一次") || !strings.Contains(tell, CodeCheckStop) || strings.Contains(tell, old.Issued["password"]) {
		t.Fatalf("plan %+v", pv)
	}
	v.db.solo.mu.Lock()
	v.db.solo.pingWait = 0
	v.db.solo.mu.Unlock()
	readyTimeout = saved
	v.installOK(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	if b := v.binding(t, "demo-solo-sql-user"); b.Issued["password"] != old.Issued["password"] {
		t.Fatalf("binding %+v", b)
	}
}

// soloRoles is the roles the one-consumer fake database has, and the order
// of what was done to them (lock:<role>, drop:<role>).
func (v *soloEnv) soloRoles() (map[string]sqlRole, string) {
	v.db.solo.mu.Lock()
	defer v.db.solo.mu.Unlock()
	out := map[string]sqlRole{}
	for name, r := range v.db.solo.roles {
		out[name] = *r
	}
	return out, strings.Join(v.db.solo.order, ",")
}

// noPassword fails t when password shows in the task record or the log.
func (v *soloEnv) noPassword(t *testing.T, taskID, password string) {
	t.Helper()
	if password == "" {
		t.Fatal("no password to look for")
	}
	raw, _ := v.w.st.GetInstallTask(context.Background(), taskID)
	if strings.Contains(raw.Data, password) || strings.Contains(v.logs.String(), password) {
		t.Fatal("the password shows")
	}
}

// A fresh install that fails after its database was made: the account is
// taken back and then the database this install made is dropped (purge),
// in that order; nothing is left to tell the user. The task record says
// which credential it issued (bindings[].issue_id).
func TestRollbackPurgesNewData(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.w.failDeploy["demo-solo-sql-user"] = true
	v.db.solo.mu.Lock()
	v.db.solo.order = nil
	v.db.solo.mu.Unlock()
	tv := v.install(t, Request{App: "demo-solo-sql-user"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeStart {
		t.Fatalf("task %s %+v", tv.Status, tv.Fault)
	}
	if len(tv.Apps) != 1 || len(tv.Apps[0].Bindings) != 1 || !tv.Apps[0].Bindings[0].Issued || tv.Apps[0].Bindings[0].IssueID == "" {
		t.Fatalf("task record %+v", tv.Apps)
	}
	id := tv.Apps[0].Bindings[0].IssueID
	roles, order := v.soloRoles()
	if len(roles) != 0 || order != "lock:af_"+id+",drop:af_"+id {
		t.Fatalf("roles %+v, order %s", roles, order)
	}
	if ws := carriedRevoke(tv, "demo-solo-sql-user"); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	reg, _ := v.e.Registry()
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
}

// The purge of a rollback does not work: the rollback goes on (the task is
// the install's failure, the binding goes), and the task says what to
// delete where (CAT-INST-35): the number, no password.
func TestRollbackPurgeFailsSaid(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.w.failDeploy["demo-solo-sql-user"] = true
	v.db.solo.mu.Lock()
	v.db.solo.failPurge = true
	v.db.solo.mu.Unlock()
	tv := v.install(t, Request{App: "demo-solo-sql-user"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeStart {
		t.Fatalf("task %s %+v", tv.Status, tv.Fault)
	}
	roles, _ := v.soloRoles()
	var user string
	var role sqlRole
	for name, r := range roles {
		user, role = name, r
	}
	if user == "" || role.login {
		t.Fatalf("role %s %+v", user, role)
	}
	id := strings.TrimPrefix(user, "af_")
	ws := carriedRevoke(tv, "demo-solo-sql-user")
	if len(ws) != 1 || !strings.HasPrefix(ws[0], CodeRevoke+" demo-solo-sql-user 没装成，平台想删掉 demo-solo-sql 这次给它新建的库或数据") ||
		!strings.Contains(ws[0], "编号 "+id) || !strings.Contains(ws[0], "没删成") || !strings.Contains(ws[0], "手工删掉") ||
		strings.Contains(ws[0], role.password) {
		t.Fatalf("warnings %v", ws)
	}
	reg, _ := v.e.Registry()
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
	v.noPassword(t, tv.ID, role.password)
}

// An install on data kept that fails: the binding it used again (reused)
// is not touched: the account is neither taken back nor dropped, the
// binding stays with the same credential, and an install after that still
// gets the old database.
func TestRollbackLeavesReusedAlone(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.installOK(t, Request{App: "demo-solo-sql-user"})
	old := v.binding(t, "demo-solo-sql-user")
	if err := v.w.delete(ctx, "demo-solo-sql-user", false); err != nil {
		t.Fatal(err)
	}
	v.db.solo.mu.Lock()
	v.db.solo.order = nil
	v.db.solo.mu.Unlock()
	v.w.failDeploy["demo-solo-sql-user"] = true
	tv := v.install(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	if tv.Status != state.TaskFailed {
		t.Fatalf("task %s %+v", tv.Status, tv.Fault)
	}
	if len(tv.Apps) != 1 || len(tv.Apps[0].Bindings) != 1 || !tv.Apps[0].Bindings[0].Reused || tv.Apps[0].Bindings[0].Issued {
		t.Fatalf("task record %+v", tv.Apps)
	}
	roles, order := v.soloRoles()
	r, ok := roles[old.Issued["username"]]
	if !ok || !r.login || len(roles) != 1 || order != "" {
		t.Fatalf("roles %+v, order %q", roles, order)
	}
	if ws := carriedRevoke(tv, "demo-solo-sql-user"); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if b := v.binding(t, "demo-solo-sql-user"); b.Issued["password"] != old.Issued["password"] || b.Issued[catalog.IssueIDKey] != old.Issued[catalog.IssueIDKey] {
		t.Fatalf("binding %+v", b)
	}
	v.w.failDeploy["demo-solo-sql-user"] = false
	v.installOK(t, Request{App: "demo-solo-sql-user", ReuseData: true})
	if b := v.binding(t, "demo-solo-sql-user"); b.Issued["password"] != old.Issued["password"] {
		t.Fatalf("binding %+v", b)
	}
}

// What a rollback purges is only what the task record says this install
// issued, and only while the binding holds that very credential: a record
// without the number (written by an older version), or with another one,
// gets the account taken back and the user told, the database left.
func TestRollbackPurgesOnlyWhatTheRecordSays(t *testing.T) {
	for _, recorded := range []string{"", "0badc0de"} {
		v := setupSolo(t)
		ctx := context.Background()
		v.installOK(t, Request{App: "demo-solo-sql"})
		v.installOK(t, Request{App: "demo-solo-sql-user"})
		b := v.binding(t, "demo-solo-sql-user")
		v.db.solo.mu.Lock()
		v.db.solo.order = nil
		v.db.solo.mu.Unlock()
		tr := &taskRun{e: v.e, id: "0123456789aa", view: TaskView{Apps: []AppResult{{App: "demo-solo-sql-user"}}}}
		tr.releaseCreated(ctx, AppResult{App: "demo-solo-sql-user", Bindings: []BindingRef{{Use: "db", Service: "demo-solo-sql", Issued: true, IssueID: recorded}}})
		roles, order := v.soloRoles()
		user := b.Issued["username"]
		if r, ok := roles[user]; !ok || r.login || order != "lock:"+user {
			t.Fatalf("recorded %q: roles %+v, order %s", recorded, roles, order)
		}
		ws := carriedRevoke(tr.snapshot(), "demo-solo-sql-user")
		if len(ws) != 1 || !strings.Contains(ws[0], "没有删（这次安装的记录里没有写明是哪一份") || !strings.Contains(ws[0], "编号 "+b.Issued[catalog.IssueIDKey]) {
			t.Fatalf("recorded %q: warnings %v", recorded, ws)
		}
	}
}

// Two installs at once want the one place a provider has (max_consumers 1,
// and a need that wants it for itself): both are issued a credential, one
// gets the binding, the other is CAT-INST-38 and its credential is taken
// back and what was made for it dropped; nothing to tell, the winner's
// account works.
func TestRaceLoserPurged(t *testing.T) {
	v := setupSolo(t)
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.db.mu.Lock()
	v.db.createGate, v.db.release = 2, make(chan struct{})
	v.db.mu.Unlock()
	a, err := v.e.StartInstall(context.Background(), Request{App: "demo-solo-sql-user"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.e.StartInstall(context.Background(), Request{App: "demo-solo-sql-user", Name: "demo-solo-sql-user-b"})
	if err != nil {
		t.Fatalf("the second was refused before it issued: %v", err)
	}
	ta, tb := v.wait(t, a.ID), v.wait(t, b.ID)
	won, lost := ta, tb
	if ta.Status != state.TaskSucceeded {
		won, lost = tb, ta
	}
	if won.Status != state.TaskSucceeded || lost.Status != state.TaskFailed || lost.Fault == nil || lost.Fault.Code != CodeTaken {
		t.Fatalf("won %s %+v / lost %s %+v", won.Status, won.Fault, lost.Status, lost.Fault)
	}
	winner, loser := won.Apps[0].App, lost.Apps[0].App
	wb := v.binding(t, winner)
	roles, order := v.soloRoles()
	r, ok := roles[wb.Issued["username"]]
	if len(roles) != 1 || !ok || !r.login {
		t.Fatalf("roles %+v", roles)
	}
	// The loser's role: locked, then dropped.
	var lostRole string
	for _, step := range strings.Split(order, ",") {
		if name, ok := strings.CutPrefix(step, "drop:"); ok {
			lostRole = name
		}
	}
	if lostRole == "" || lostRole == wb.Issued["username"] || order != "lock:"+lostRole+",drop:"+lostRole {
		t.Fatalf("order %s (winner %s)", order, wb.Issued["username"])
	}
	if ws := carriedRevoke(lost, loser); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	reg, _ := v.e.Registry()
	if _, err := reg.GetBinding(context.Background(), loser, "db"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("loser's binding: %v", err)
	}
}

// The issue command fails half-way (the role and database are made): the
// account is taken back and the database dropped, by the names the issue
// variables gave them; the install fails with CAT-INST-34 and nothing is
// left to tell.
func TestHalfIssuePurged(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.db.solo.mu.Lock()
	v.db.solo.failCreate = true
	v.db.solo.order = nil
	v.db.solo.mu.Unlock()
	tv := v.install(t, Request{App: "demo-solo-sql-user"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeIssue {
		t.Fatalf("task %s %+v", tv.Status, tv.Fault)
	}
	roles, order := v.soloRoles()
	if len(roles) != 0 || !strings.HasPrefix(order, "lock:af_") || !strings.HasSuffix(order, ",drop:"+strings.TrimPrefix(strings.Split(order, ",")[0], "lock:")) {
		t.Fatalf("roles %+v, order %s", roles, order)
	}
	if ws := carriedRevoke(tv, "demo-solo-sql-user"); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	reg, _ := v.e.Registry()
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
}

// An issue cut short by a restart: taken back and then purged by the
// number the task recorded (issue variables written as templates are
// worked out again).
func TestInterruptedIssuePurged(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	reg, _ := v.e.Registry()
	v.db.solo.mu.Lock()
	v.db.solo.roles["af_0badc0de"] = &sqlRole{password: "x-0001", database: "af_0badc0de", login: true}
	v.db.solo.order = nil
	v.db.solo.mu.Unlock()
	if err := v.e.revokeInterrupted(ctx, reg, "demo-solo-sql-user", IssuingRef{Use: "db", Service: "demo-solo-sql", Provider: "demo-solo-sql", IssueID: "0badc0de"}); err != nil {
		t.Fatal(err)
	}
	roles, order := v.soloRoles()
	if len(roles) != 0 || order != "lock:af_0badc0de,drop:af_0badc0de" {
		t.Fatalf("roles %+v, order %s", roles, order)
	}
}

// soloByName is the test material's one-consumer provider with its issue
// variables naming the database and the account after the consumer, not
// after the issue's number: what its purge steps would delete may be there
// from before the install (purgeOwnsIssue says no).
func soloByName(t *testing.T) string {
	t.Helper()
	m := soloManifest(t, "demo-solo-sql")
	out := strings.ReplaceAll(m, `"af_{{issue.id}}"`, `"af_{{consumer.name}}"`)
	if out == m || strings.Count(out, "af_{{consumer.name}}") != 2 {
		t.Fatal("the test material's issue_vars are not as this test expects")
	}
	return out
}

// Which purge steps a rollback may run: only those that find what they
// delete by the issue's number.
func TestPurgeOwnsIssue(t *testing.T) {
	step := func(stdin string) catalog.Step {
		return catalog.Step{Name: "drop", Exec: &catalog.StepExec{Command: []string{"demo-sql", "--admin"}, Stdin: stdin}}
	}
	vars := map[string]catalog.IssueVar{
		"database": {Template: "af_{{issue.id}}"},
		"username": {Template: "af_{{issue.id}}"},
		"byname":   {Template: "af_{{consumer.name}}"},
		"password": {Generate: &catalog.Generate{Length: 32, Charset: "alnum"}, Secret: true},
	}
	for _, c := range []struct {
		stdin string
		want  bool
	}{
		{`DROP DATABASE "{{issued.database}}"; DROP ROLE "{{issued.username}}";`, true},
		{`DROP THING "acornfox-{{issued.issue_id}}";`, true},
		{`DROP DATABASE "{{issued.database}}" -- {{issued.password}} {{params.admin_password}} {{self.host}}`, true},
		{`DROP DATABASE "{{issued.byname}}";`, false},
		{`DROP DATABASE "{{issued.database}}"; DROP ROLE "{{issued.byname}}";`, false},
		{`DROP DATABASE "af_{{consumer.name}}";`, false},
		{`DROP DATABASE "{{issued.database}}"; DROP ROLE "{{consumer.name}}";`, false},
		{`DROP TOKEN "{{issued.token_id}}";`, false}, // saved by an issue step: may be found, not made
		{`ROTATE "{{issued.password}}";`, false},     // names nothing
		{`VACUUM;`, false},
	} {
		pv := catalog.Provide{IssueVars: vars, Purge: []catalog.Step{step(c.stdin)}}
		if got := purgeOwnsIssue(pv); got != c.want {
			t.Errorf("%s: %v, want %v", c.stdin, got, c.want)
		}
	}
	if purgeOwnsIssue(catalog.Provide{IssueVars: vars}) {
		t.Error("no purge steps: nothing to own")
	}
}

// A provider whose purge steps find the database by the consumer's name: a
// rollback takes the account back but drops nothing (it may be data from
// before), and tells the user to look.
func TestRollbackKeepsDataNotFoundByNumber(t *testing.T) {
	v := setupSoloProvider(t, soloByName(t))
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.w.failDeploy["demo-solo-sql-user"] = true
	v.db.solo.mu.Lock()
	v.db.solo.order = nil
	v.db.solo.mu.Unlock()
	tv := v.install(t, Request{App: "demo-solo-sql-user"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeStart {
		t.Fatalf("task %s %+v", tv.Status, tv.Fault)
	}
	roles, order := v.soloRoles()
	r, ok := roles["af_demo-solo-sql-user"]
	if !ok || r.login || order != "lock:af_demo-solo-sql-user" {
		t.Fatalf("roles %+v, order %s", roles, order)
	}
	ws := carriedRevoke(tv, "demo-solo-sql-user")
	if len(ws) != 1 || !strings.Contains(ws[0], "没有删（"+leftNotOwned) || !strings.Contains(ws[0], "确认是这次新建的") || strings.Contains(ws[0], r.password) {
		t.Fatalf("warnings %v", ws)
	}
}

// The same provider, and a database of that name is there from before the
// install (the issue step then fails half-way): the rollback must not drop
// it.
func TestHalfIssueKeepsDataFromBefore(t *testing.T) {
	v := setupSoloProvider(t, soloByName(t))
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.db.solo.mu.Lock()
	v.db.solo.roles["af_demo-solo-sql-user"] = &sqlRole{password: "from-before", database: "af_demo-solo-sql-user", login: true}
	v.db.solo.order = nil
	v.db.solo.mu.Unlock()
	tv := v.install(t, Request{App: "demo-solo-sql-user"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeIssue {
		t.Fatalf("task %s %+v", tv.Status, tv.Fault)
	}
	roles, order := v.soloRoles()
	if _, ok := roles["af_demo-solo-sql-user"]; !ok || strings.Contains(order, "drop:") {
		t.Fatalf("data from before dropped: roles %+v, order %s", roles, order)
	}
	ws := carriedRevoke(tv, "demo-solo-sql-user")
	if len(ws) != 1 || !strings.Contains(ws[0], leftNotOwned) {
		t.Fatalf("warnings %v", ws)
	}
}

// An issue cut short by a restart, at a provider whose purge steps find the
// database by the consumer's name: the account is taken back, nothing is
// dropped, and the error says why (the user is told to look).
func TestInterruptedIssueKeepsDataNotFoundByNumber(t *testing.T) {
	v := setupSoloProvider(t, soloByName(t))
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	reg, _ := v.e.Registry()
	v.db.solo.mu.Lock()
	v.db.solo.roles["af_demo-solo-sql-user"] = &sqlRole{password: "from-before", database: "af_demo-solo-sql-user", login: true}
	v.db.solo.order = nil
	v.db.solo.mu.Unlock()
	err := v.e.revokeInterrupted(ctx, reg, "demo-solo-sql-user", IssuingRef{Use: "db", Service: "demo-solo-sql", Provider: "demo-solo-sql", IssueID: "0badc0de"})
	if !errors.Is(err, errPurgeNotOwned) {
		t.Fatalf("err %v", err)
	}
	roles, order := v.soloRoles()
	if _, ok := roles["af_demo-solo-sql-user"]; !ok || order != "lock:af_demo-solo-sql-user" {
		t.Fatalf("roles %+v, order %s", roles, order)
	}
}

// The provider's container goes after it made the database and before the
// rollback (stopped or removed):
// neither taking the account back nor dropping the database can run, the
// rollback still ends (the binding goes, the task is the install's
// failure), and the task says both, with what to delete where.
func TestRollbackProviderStoppedSaid(t *testing.T) {
	v := setupSolo(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-solo-sql"})
	v.w.failDeploy["demo-solo-sql-user"] = true
	v.db.solo.mu.Lock()
	v.db.solo.stopAfterCreate = true
	v.db.solo.mu.Unlock()
	start := time.Now()
	tv := v.install(t, Request{App: "demo-solo-sql-user"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeStart {
		t.Fatalf("task %s %+v", tv.Status, tv.Fault)
	}
	roles, _ := v.soloRoles()
	var user string
	var role sqlRole
	for name, r := range roles {
		user, role = name, r
	}
	if len(roles) != 1 || !role.login {
		t.Fatalf("roles %+v", roles)
	}
	id := strings.TrimPrefix(user, "af_")
	ws := carriedRevoke(tv, "demo-solo-sql-user")
	if len(ws) != 2 || !strings.Contains(strings.Join(ws, "\n"), "平台想删掉 demo-solo-sql 这次给它新建的库或数据（名字里带编号 "+id+"），没删成") {
		t.Fatalf("warnings %v", ws)
	}
	reg, _ := v.e.Registry()
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
	v.noPassword(t, tv.ID, role.password)
	t.Logf("rollback with the provider stopped took %s; warnings:\n%s", time.Since(start).Round(time.Millisecond), strings.Join(ws, "\n"))
}
