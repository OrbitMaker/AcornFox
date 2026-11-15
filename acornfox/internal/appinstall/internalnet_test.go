package appinstall

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// The database-shaped test material (catalog testdata/sqlsvc, sqlsvcDir in
// exec_flow_test.go): provider demo-sql, whose primary endpoint is tcp,
// checked by a command in its own container (health.exec), reached over the
// app network only (internal).

// sqlProviderManifest is demo-sql's manifest as the material has it: its
// exec init step and the provides entry (issue_vars, exec steps, an
// issued.* value template) included, since this version carries all of
// them out. Installing it with no consumer runs its health check and its
// init step (isInitCall tells that one apart); nothing is issued.
func sqlProviderManifest(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(sqlsvcDir, "apps", "demo-sql", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "\ninit:\n") || !strings.Contains(s, "\nprovides:\n") {
		t.Fatal("material changed: demo-sql has no init step or provides entry")
	}
	m, err := catalog.ParseManifest([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	if p := catalog.PendingFeatures(m); len(p) != 0 {
		t.Fatalf("all of it must be carried out by this version: %+v", p)
	}
	return s
}

// isInitCall tells demo-sql's init step (quiet-log: the admin client fed on
// standard input) from its health check.
func isInitCall(req runner.ExecRequest) bool {
	return len(req.Command) > 1 && req.Command[0] == "demo-sql" && req.Command[1] == "--admin"
}

// netWorld is the fake world whose runner can run a command in a container
// (runner.ExecAPI; fn answers, numbered from 1) and gives an internal app's container an address on its
// app network (it publishes no port).
type netWorld struct {
	*fakeWorld
	mu    sync.Mutex
	calls []runner.ExecRequest
	fn    func(n int, req runner.ExecRequest) (runner.ExecResult, error)
}

const fakeAppNetIP = "172.30.0.9"

func (x *netWorld) Exec(_ context.Context, req runner.ExecRequest) (runner.ExecResult, error) {
	x.mu.Lock()
	x.calls = append(x.calls, req)
	n := len(x.calls)
	x.mu.Unlock()
	return x.fn(n, req)
}

func (x *netWorld) ListContainers(ctx context.Context, app string) ([]runner.ContainerInfo, error) {
	cs, err := x.fakeWorld.ListContainers(ctx, app)
	if a, gerr := x.st.GetApp(ctx, app); gerr == nil && a.Internal {
		for i := range cs {
			cs[i].HostPort, cs[i].IP = 0, fakeAppNetIP
		}
	}
	return cs, err
}

func (x *netWorld) execCalls() []runner.ExecRequest {
	x.mu.Lock()
	defer x.mu.Unlock()
	return append([]runner.ExecRequest(nil), x.calls...)
}

// setupInternal is setup with the catalog holding only manifest (demo-sql)
// and a runner that runs commands (fn answers them); dialed records where
// the tcp health check connected.
func setupInternal(t *testing.T, manifest string, fn func(n int, req runner.ExecRequest) (runner.ExecResult, error)) (*env, *netWorld, *[]string) {
	t.Helper()
	var x *netWorld
	var dialed []string
	var mu sync.Mutex
	v := setupWith(t, func(cfg *Config) {
		x = &netWorld{fakeWorld: cfg.Runner.(*fakeWorld), fn: fn}
		cfg.Runner = x
		b := fixtureBundle(t, 7, "", []fixApp{{manifest: manifest}}, []catalog.Recipe{oneAppRecipe("demo-sql")}, nil)
		cfg.Catalog = NewCatalog(CatalogConfig{CacheDir: filepath.Join(t.TempDir(), "catalog"), Snapshot: bundleFS(b), Sources: []string{"http://127.0.0.1:1"}})
		cfg.TCPProbe = func(_ context.Context, addr string) error {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			return nil
		}
	})
	return v, x, &dialed
}

// An app that does not answer HTTP, reached over the app network only,
// installs to ready: its health check runs as a command in its container
// until it passes, nothing is sent over HTTP, and it has no address.
func TestInstallInternalAppExecHealth(t *testing.T) {
	v, x, dialed := setupInternal(t, sqlProviderManifest(t), func(n int, req runner.ExecRequest) (runner.ExecResult, error) {
		if n < 3 {
			return runner.ExecResult{ExitCode: 2}, nil // still initializing
		}
		return runner.ExecResult{}, nil
	})
	tv := v.install(t, Request{App: "demo-sql"})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	r := tv.Apps[0]
	if !r.Internal || r.URL != "" {
		t.Fatalf("result %+v", r)
	}
	if st := stepOf(tv, "demo-sql", "health"); st == nil || st.Status != StepDone {
		t.Fatalf("health step %+v", st)
	}
	// An app with no login fields answers an empty list, never null: the
	// console walks it without checking (JSON "fields":[] not "fields":null).
	fl, err := v.e.FirstLogin(context.Background(), "demo-sql", false)
	if err != nil || fl.Fields == nil || len(fl.Fields) != 0 {
		t.Fatalf("first login of an app without fields: %+v %v", fl, err)
	}
	if b, _ := json.Marshal(fl); !strings.Contains(string(b), `"fields":[]`) {
		t.Fatalf("first login JSON %s", b)
	}
	for _, x := range tv.FirstLogin {
		if x.Fields == nil {
			t.Fatalf("task first_login with null fields: %+v", x)
		}
	}
	a, err := v.w.st.GetApp(context.Background(), "demo-sql")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"demo-sql-ping", "--host", "127.0.0.1", "--port", "5000"}
	if !a.Internal || a.Exposure != "" || a.Port != 5000 || a.HealthCheck == nil || strings.Join(a.HealthCheck.Exec, " ") != strings.Join(want, " ") || a.HealthCheck.TimeoutS != 120 || a.HealthCheck.TCP {
		t.Fatalf("app %+v health %+v", a, a.HealthCheck)
	}
	var calls []runner.ExecRequest
	inits := 0
	for _, c := range x.execCalls() {
		if isInitCall(c) {
			// After the check passed, with the admin password in its own
			// environment, never on the command line.
			if inits++; len(calls) != 3 || c.Env["DEMO_SQL_PASSWORD"] == "" || !strings.Contains(string(c.Stdin), "statement_log") || strings.Contains(strings.Join(c.Command, " "), c.Env["DEMO_SQL_PASSWORD"]) {
				t.Fatalf("init call %+v after %d checks", c, len(calls))
			}
			continue
		}
		calls = append(calls, c)
	}
	if len(calls) != 3 || inits != 1 {
		t.Fatalf("exec calls %+v, init %d", calls, inits)
	}
	for _, c := range calls {
		if c.App != "demo-sql" || c.Name != runner.ContainerName("demo-sql", a.CurrentDeployment) || strings.Join(c.Command, " ") != strings.Join(want, " ") || len(c.Env) != 0 || len(c.Stdin) != 0 {
			t.Fatalf("exec request %+v", c)
		}
	}
	if len(*dialed) != 0 {
		t.Fatalf("an exec check connects nowhere: %q", *dialed)
	}
}

// A tcp health check connects to the container's address on the app
// network (an internal app publishes no port).
func TestInstallInternalAppTCPHealth(t *testing.T) {
	m := strings.Replace(sqlProviderManifest(t), "      exec: [demo-sql-ping, --host, 127.0.0.1, --port, \"5000\"]\n", "      tcp: true\n", 1)
	v, x, dialed := setupInternal(t, m, func(_ int, req runner.ExecRequest) (runner.ExecResult, error) {
		if !isInitCall(req) {
			t.Errorf("a tcp check runs no command: %+v", req)
		}
		return runner.ExecResult{}, nil
	})
	tv := v.install(t, Request{App: "demo-sql"})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	if len(*dialed) == 0 || (*dialed)[0] != fakeAppNetIP+":5000" || len(x.execCalls()) != 1 {
		t.Fatalf("dialed %q", *dialed)
	}
	a, _ := v.w.st.GetApp(context.Background(), "demo-sql")
	if a.HealthCheck == nil || !a.HealthCheck.TCP || len(a.HealthCheck.Exec) != 0 {
		t.Fatalf("health %+v", a.HealthCheck)
	}
}

// A check that never passes fails the install with the health code, saying
// what was checked; the app is rolled back.
func TestInstallInternalAppHealthFails(t *testing.T) {
	m := strings.Replace(sqlProviderManifest(t), "      timeout_s: 120\n", "      timeout_s: 5\n", 1)
	v, _, _ := setupInternal(t, m, func(int, runner.ExecRequest) (runner.ExecResult, error) {
		return runner.ExecResult{ExitCode: 1, Stdout: []byte("not yet")}, nil
	})
	start := time.Now()
	tv := v.install(t, Request{App: "demo-sql"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeHealth {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	if !strings.Contains(tv.Fault.Message, "在容器里执行 demo-sql-ping") || !strings.Contains(tv.Fault.Message, "退出码 1") || strings.Contains(tv.Fault.Message, "not yet") {
		t.Fatalf("message %q", tv.Fault.Message)
	}
	if time.Since(start) > 20*time.Second {
		t.Fatal("timeout_s was not used")
	}
	if _, err := v.w.st.GetApp(context.Background(), "demo-sql"); err == nil {
		t.Fatal("a failed install is rolled back")
	}
}

// Without a runner that can run a command in a container, an app checked
// that way is refused before anything is made.
func TestInstallExecHealthNeedsExecRunner(t *testing.T) {
	v := setupWith(t, func(cfg *Config) {
		b := fixtureBundle(t, 7, "", []fixApp{{manifest: sqlProviderManifest(t)}}, []catalog.Recipe{oneAppRecipe("demo-sql")}, nil)
		cfg.Catalog = NewCatalog(CatalogConfig{CacheDir: filepath.Join(t.TempDir(), "catalog"), Snapshot: bundleFS(b), Sources: []string{"http://127.0.0.1:1"}})
	})
	_, err := v.e.StartInstall(context.Background(), Request{App: "demo-sql"})
	if f := faultOf(t, err); f.Code != CodeInternal || !strings.Contains(f.Message, "运行器（runner）太旧") || !strings.Contains(f.Message, "健康检查") {
		t.Fatalf("%+v", f)
	}
	if apps, _ := v.w.st.ListApps(context.Background()); len(apps) != 0 {
		t.Fatalf("made %+v", apps)
	}
}

// The server-wide memory cap applies to an internal app like to any other.
func TestInstallInternalAppMemoryCap(t *testing.T) {
	v, _, _ := setupInternal(t, sqlProviderManifest(t), func(int, runner.ExecRequest) (runner.ExecResult, error) { return runner.ExecResult{}, nil })
	v.w.room = 32
	tv := v.install(t, Request{App: "demo-sql"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeNoMemory {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
}

// Deleted with its data kept and installed again on that data, it is again
// internal with its health check.
func TestReinstallInternalAppOnKeptData(t *testing.T) {
	v, _, _ := setupInternal(t, sqlProviderManifest(t), func(int, runner.ExecRequest) (runner.ExecResult, error) { return runner.ExecResult{}, nil })
	ctx := context.Background()
	installAndKeepData(t, v, Request{App: "demo-sql"})
	tv := v.install(t, Request{App: "demo-sql", ReuseData: true})
	if tv.Status != state.TaskSucceeded || !tv.Apps[0].Internal || len(tv.Apps[0].ReusedVolumes) == 0 {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	a, _ := v.w.st.GetApp(ctx, "demo-sql")
	if !a.Internal || a.HealthCheck == nil || len(a.HealthCheck.Exec) == 0 {
		t.Fatalf("app %+v", a)
	}
}

// A provider's values for a tcp endpoint: self.host and self.port are the
// app's name and the endpoint's port; self.internal_url (an http://
// address) is not worked out for it.
func TestProvideValuesOfTCPEndpoint(t *testing.T) {
	m := &catalog.Manifest{Endpoints: []catalog.Endpoint{
		{Name: "db", Port: 5000, Protocol: catalog.ProtocolTCP, Primary: true},
		{Name: "web", Port: 8080, Protocol: catalog.ProtocolHTTP},
	}}
	pv := catalog.Provide{Kind: "demo_sql", Endpoint: "db", Values: map[string]catalog.ValueSource{
		"host": {Template: "{{self.host}}"}, "port": {Template: "{{self.port}}"},
	}}
	got, err := provideValues(m, pv, "demo-sql", Values{})
	if err != nil || got["host"] != "demo-sql" || got["port"] != "5000" {
		t.Fatalf("%v %v", got, err)
	}
	pv.Values["url"] = catalog.ValueSource{Template: "{{self.internal_url}}/x"}
	if got, err := provideValues(m, pv, "demo-sql", Values{}); err == nil || strings.Contains(got["url"], "http://") {
		t.Fatalf("a tcp endpoint has no http:// address: %v %v", got, err)
	}
	// An http endpoint keeps it.
	pv.Endpoint = "web"
	if got, err := provideValues(m, pv, "demo-sql", Values{}); err != nil || got["url"] != "http://demo-sql:8080/x" {
		t.Fatalf("%v %v", got, err)
	}
}

// Testing a service an internal app provides goes to the container's
// address on the app network: it publishes no port.
func TestHostRouteOfInternalApp(t *testing.T) {
	r := &hostRoute{app: "demo-sql", name: "demo-sql", port: 5000, ip: fakeAppNetIP}
	if addr, mapped := r.mapAddr("demo-sql", 5000); !mapped || addr != fakeAppNetIP+":5000" {
		t.Fatalf("%s %v", addr, mapped)
	}
	if addr, mapped := r.mapAddr("other", 5000); mapped || addr != "other:5000" {
		t.Fatalf("%s %v", addr, mapped)
	}
	if n := r.note(5000); !strings.Contains(n, "只在内部网络") || !strings.Contains(n, fakeAppNetIP+":5000") {
		t.Fatalf("%s", n)
	}
	// A service on another tcp endpoint of the internal app (not the
	// primary one): every port of the container is reachable at its address
	// on the app network.
	if addr, mapped := r.mapAddr("demo-sql", 5001); !mapped || addr != fakeAppNetIP+":5001" {
		t.Fatalf("other endpoint: %s %v", addr, mapped)
	}
	if n := r.note(5001); !strings.Contains(n, fakeAppNetIP+":5001") || !strings.Contains(n, "demo-sql:5001") {
		t.Fatalf("%s", n)
	}
	pub := &hostRoute{app: "web", name: "web", port: 8080, hostPort: 41000}
	if addr, mapped := pub.mapAddr("web", 8080); !mapped || addr != "127.0.0.1:41000" {
		t.Fatalf("%s %v", addr, mapped)
	}
	// A published app publishes only its primary port.
	if addr, mapped := pub.mapAddr("web", 9000); mapped || addr != "web:9000" {
		t.Fatalf("unpublished port: %s %v", addr, mapped)
	}
	// routeToProvider finds it.
	v, _, _ := setupInternal(t, sqlProviderManifest(t), func(int, runner.ExecRequest) (runner.ExecResult, error) { return runner.ExecResult{}, nil })
	if tv := v.install(t, Request{App: "demo-sql"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	a, _ := v.w.st.GetApp(context.Background(), "demo-sql")
	got, why := v.e.routeToProvider(context.Background(), &svcEnv{apps: map[string]state.App{"demo-sql": a}}, Service{App: "demo-sql"})
	if got == nil || got.ip != fakeAppNetIP || got.port != 5000 || got.hostPort != 0 {
		t.Fatalf("%+v %s", got, why)
	}
}
