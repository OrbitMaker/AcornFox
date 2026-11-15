package reconcile

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// execRunner is the fake runner that can also run a command in a container
// (runner.ExecAPI), the way an internal app's health check is run.
type execRunner struct {
	*fakeRunner
	mu    sync.Mutex
	calls []runner.ExecRequest
	fn    func(n int, req runner.ExecRequest) (runner.ExecResult, error)
}

func (e *execRunner) Exec(_ context.Context, req runner.ExecRequest) (runner.ExecResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, req)
	n := len(e.calls)
	e.mu.Unlock()
	return e.fn(n, req)
}

func (e *execRunner) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

// newExecHarness is newHarness with a runner that runs commands (fn answers
// them) and a TCP probe that records where it connects.
func newExecHarness(t *testing.T, fn func(n int, req runner.ExecRequest) (runner.ExecResult, error)) (*harness, *execRunner, *[]string) {
	t.Helper()
	h := newHarness(t)
	er := &execRunner{fakeRunner: h.runner, fn: fn}
	var dialed []string
	var mu sync.Mutex
	h.probe = func(_ context.Context, hp, _ string) error {
		t.Errorf("an app whose check is no GET was sent a GET (%s)", hp)
		return nil
	}
	h.rec = New(Config{
		Store:  h.store,
		Runner: er,
		Proxy:  h.router,
		Probe:  func(ctx context.Context, hp, p string) error { return h.probe(ctx, hp, p) },
		TCPProbe: func(_ context.Context, addr string) error {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			return nil
		},
		UploadDir:     t.TempDir(),
		PublicHost:    "192.168.0.1",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Tick:          time.Hour,
		HealthTimeout: 3 * time.Second,
		Now:           h.clock.Now,
	})
	return h, er, &dialed
}

func internalApp(name string, hc *state.HealthCheck) state.App {
	a := baseApp(name)
	a.Port = 5000
	a.Internal = true
	a.HealthCheck = hc
	return a
}

// An internal app with an exec health check: its container publishes no
// port, the check runs in the new container until it passes, no GET is
// sent, and it goes live without an entry route.
func TestInternalAppExecHealthToLive(t *testing.T) {
	h, er, _ := newExecHarness(t, func(n int, req runner.ExecRequest) (runner.ExecResult, error) {
		if n < 3 {
			return runner.ExecResult{ExitCode: 1}, nil // still starting
		}
		return runner.ExecResult{ExitCode: 0, Stdout: []byte("ready\n")}, nil
	})
	h.store.putApp(internalApp("db", &state.HealthCheck{Exec: []string{"demo-sql-ping", "--host", "127.0.0.1"}, ExpectStdout: "ready"}))
	h.store.putDeployment(queued("db", "aaaaaaaaaaaa", 1))

	d := h.driveToTerminal(t, "db", "aaaaaaaaaaaa", 10)
	if d.Status != state.StatusLive {
		t.Fatalf("want live, got %s (diag=%+v)", d.Status, d.Diagnosis)
	}
	name := runner.ContainerName("db", "aaaaaaaaaaaa")
	reqs := h.runner.requestsFor("aaaaaaaaaaaa")
	if len(reqs) != 1 || !reqs[0].Internal || reqs[0].Port != 5000 {
		t.Fatalf("ensure requests %+v", reqs)
	}
	if c := h.runner.containers[name]; c.HostPort != 0 || c.IP == "" {
		t.Fatalf("container %+v", c)
	}
	if er.callCount() != 3 {
		t.Fatalf("exec calls %d", er.callCount())
	}
	for _, c := range er.calls {
		if c.App != "db" || c.Name != name || strings.Join(c.Command, " ") != "demo-sql-ping --host 127.0.0.1" || c.TimeoutS != runner.HealthExecTimeoutS {
			t.Fatalf("exec request %+v", c)
		}
	}
	if h.router.hasRoute("db") {
		t.Fatal("an internal app gets no entry route")
	}
	if app := h.store.getApp("db"); app.CurrentDeployment != "aaaaaaaaaaaa" {
		t.Fatalf("current deployment %q", app.CurrentDeployment)
	}
	found := false
	for _, m := range h.store.eventMessages() {
		if strings.Contains(m, "http://") && strings.Contains(m, "已上线") {
			t.Errorf("an internal app has no address: %q", m)
		}
		found = found || strings.Contains(m, "只在内部网络")
	}
	if !found {
		t.Fatalf("events %q", h.store.eventMessages())
	}
}

// A tcp health check connects to the container's address on the app network
// (it publishes no port), not to a loopback port.
func TestInternalAppTCPHealth(t *testing.T) {
	h, er, dialed := newExecHarness(t, func(int, runner.ExecRequest) (runner.ExecResult, error) {
		return runner.ExecResult{}, nil
	})
	h.store.putApp(internalApp("cache", &state.HealthCheck{TCP: true}))
	h.store.putDeployment(queued("cache", "bbbbbbbbbbbb", 1))
	d := h.driveToTerminal(t, "cache", "bbbbbbbbbbbb", 10)
	if d.Status != state.StatusLive {
		t.Fatalf("want live, got %s (diag=%+v)", d.Status, d.Diagnosis)
	}
	c := h.runner.containers[runner.ContainerName("cache", "bbbbbbbbbbbb")]
	if len(*dialed) == 0 || (*dialed)[0] != c.IP+":5000" || er.callCount() != 0 {
		t.Fatalf("dialed %q (container %+v), exec calls %d", *dialed, c, er.callCount())
	}
	if h.router.hasRoute("cache") {
		t.Fatal("no entry route")
	}
}

// A check that never passes fails the deployment with the usual code; the
// message says the check command did not pass and why, without its output.
func TestExecHealthTimesOut(t *testing.T) {
	h, _, _ := newExecHarness(t, func(int, runner.ExecRequest) (runner.ExecResult, error) {
		return runner.ExecResult{ExitCode: 3, Stderr: []byte("server starting")}, nil
	})
	h.runner.onList = func() { h.clock.Advance(2 * time.Second) }
	h.store.putApp(internalApp("db", &state.HealthCheck{Exec: []string{"demo-sql-ping"}}))
	h.store.putDeployment(queued("db", "cccccccccccc", 1))
	d := h.driveToTerminal(t, "db", "cccccccccccc", 10)
	if d.Status != state.StatusFailed || d.Diagnosis == nil || d.Diagnosis.Stage != "health" || d.Diagnosis.Code != "port_not_listening" {
		t.Fatalf("%s %+v", d.Status, d.Diagnosis)
	}
	if !strings.Contains(d.Diagnosis.Message, "检查命令") || !strings.Contains(d.Diagnosis.Message, "退出码 3") || strings.Contains(d.Diagnosis.Message, "server starting") {
		t.Fatalf("message %q", d.Diagnosis.Message)
	}
	if h.runner.hasContainer(runner.ContainerName("db", "cccccccccccc")) {
		t.Fatal("the failed container is removed")
	}
}

// The manifest's timeout_s, longer than the reconciler's default, is how
// long a new container may take (a database initializing its data).
func TestHealthCheckTimeoutLongerThanDefault(t *testing.T) {
	h, er, _ := newExecHarness(t, func(n int, req runner.ExecRequest) (runner.ExecResult, error) {
		if n < 3 {
			return runner.ExecResult{ExitCode: 1}, nil
		}
		return runner.ExecResult{}, nil
	})
	// 50 s pass between two checks: past the 3 s default at once, within
	// the app's 120 s.
	h.runner.onList = func() { h.clock.Advance(50 * time.Second) }
	h.store.putApp(internalApp("db", &state.HealthCheck{Exec: []string{"demo-sql-ping"}, TimeoutS: 120}))
	h.store.putDeployment(queued("db", "dddddddddddd", 1))
	d := h.driveToTerminal(t, "db", "dddddddddddd", 10)
	if d.Status != state.StatusLive || er.callCount() != 3 {
		t.Fatalf("%s %+v after %d checks", d.Status, d.Diagnosis, er.callCount())
	}
}

// After a restart of the server (or an upgrade of AcornFox) a missing
// container of an internal app comes back internal: still no published
// port.
func TestRecreateLiveKeepsInternal(t *testing.T) {
	h, _, _ := newExecHarness(t, func(int, runner.ExecRequest) (runner.ExecResult, error) { return runner.ExecResult{}, nil })
	app := internalApp("db", &state.HealthCheck{TCP: true})
	app.CurrentDeployment = "eeeeeeeeeeee"
	h.store.putApp(app)
	h.store.putDeployment(state.Deployment{ID: "eeeeeeeeeeee", App: "db", Seq: 1, SourceKind: state.SourceImage, Status: state.StatusLive, ImageID: "sha256:db-eeeeeeeeeeee"})
	h.runner.images["sha256:db-eeeeeeeeeeee"] = runner.ImageInfo{ID: "sha256:db-eeeeeeeeeeee", App: "db", DeploymentID: "eeeeeeeeeeee"}
	h.rec.round(context.Background(), "db")
	reqs := h.runner.requestsFor("eeeeeeeeeeee")
	if len(reqs) != 1 || !reqs[0].Internal {
		t.Fatalf("recreated with %+v", reqs)
	}
	if c := h.runner.containers[runner.ContainerName("db", "eeeeeeeeeeee")]; c.HostPort != 0 {
		t.Fatalf("container %+v", c)
	}
}

// Without a runner that can run commands, an exec check cannot pass: the
// deployment times out and says the runner is too old.
func TestExecHealthWithoutExecRunner(t *testing.T) {
	h := newHarness(t)
	h.probe = func(context.Context, string, string) error { t.Error("GET sent"); return nil }
	h.runner.onList = func() { h.clock.Advance(2 * time.Second) }
	h.store.putApp(internalApp("db", &state.HealthCheck{Exec: []string{"demo-sql-ping"}}))
	h.store.putDeployment(queued("db", "ffffffffffff", 1))
	d := h.driveToTerminal(t, "db", "ffffffffffff", 10)
	if d.Status != state.StatusFailed || d.Diagnosis == nil || !strings.Contains(d.Diagnosis.Message, "执行器") {
		t.Fatalf("%s %+v", d.Status, d.Diagnosis)
	}
}

// An app that is not internal and whose container publishes no port is not
// up, as before: it is not probed at its address on the app network (that
// would mark it live with an address nobody can open, since it gets no
// route); the deployment fails with port_not_listening.
func TestNotInternalUnpublishedNotHealthy(t *testing.T) {
	h := newHarness(t)
	probed := 0
	h.probe = func(context.Context, string, string) error { probed++; return nil }
	h.runner.onList = func() { h.clock.Advance(2 * time.Second) }
	app := baseApp("web")
	app.Port = 8080
	h.store.putApp(app)
	h.store.putDeployment(queued("web", "unpub0000001", 1))
	name := runner.ContainerName("web", "unpub0000001")
	h.runner.containers[name] = runner.ContainerInfo{ID: "c", Name: name, App: "web", DeploymentID: "unpub0000001", Role: runner.RoleApp, State: "running", Running: true, Port: 8080, IP: "172.30.0.9"}
	d := h.driveToTerminal(t, "web", "unpub0000001", 10)
	if d.Status != state.StatusFailed || d.Diagnosis == nil || d.Diagnosis.Code != "port_not_listening" {
		t.Fatalf("%s %+v", d.Status, d.Diagnosis)
	}
	if probed != 0 {
		t.Fatalf("probed %d times at an address it has no route for", probed)
	}
	if h.router.hasRoute("web") {
		t.Fatal("no route")
	}
}
