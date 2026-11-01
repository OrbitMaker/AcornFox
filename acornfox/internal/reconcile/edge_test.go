package reconcile

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// edgeRunner adds the edge capability to fakeRunner and records calls.
type edgeRunner struct {
	*fakeRunner
	mu        sync.Mutex
	specs     []runner.EdgeSpec
	connected []string
	lastKey   string
}

func (e *edgeRunner) EnsureEdge(_ context.Context, s runner.EdgeSpec) (runner.EdgeInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.specs = append(e.specs, s)
	key := specKey(s)
	recreated := key != e.lastKey
	e.lastKey = key
	return runner.EdgeInfo{Running: true, Recreated: recreated}, nil
}

func (e *edgeRunner) ConnectEdge(_ context.Context, app string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.connected = append(e.connected, app)
	return nil
}

func (e *edgeRunner) last() runner.EdgeSpec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.specs[len(e.specs)-1]
}

var testLAN = "192.168.31.46"

func newEdgeHarness(t *testing.T, mode, defaultExposure string) (*Reconciler, *fakeStore, *edgeRunner, *fakeRouter) {
	t.Helper()
	st, rt := newStore(), newRouter()
	er := &edgeRunner{fakeRunner: newRunner()}
	rec := New(Config{
		Store:     st,
		Runner:    er,
		Router:    rt,
		UploadDir: t.TempDir(),
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		Tick:      time.Hour,
		Edge: &EdgeConfig{Mode: mode, AdminPort: 18808, AdminToken: "0123456789abcdef0123456789abcdef",
			PortMin: 18810, PortMax: 18814, LANHost: func() string { return testLAN }},
		DefaultExposure: defaultExposure,
	})
	return rec, st, er, rt
}

func liveEdgeApp(st *fakeStore, er *edgeRunner, name, exposure string, publicPort int) {
	a := baseApp(name)
	a.PublicPort = publicPort
	a.CurrentDeployment = "aaaaaaaaaaaa"
	a.Exposure = exposure
	st.putApp(a)
	cn := runner.ContainerName(name, a.CurrentDeployment)
	er.containers[cn] = runner.ContainerInfo{Name: cn, App: name, DeploymentID: a.CurrentDeployment, Role: runner.RoleApp, Running: true, State: "running", HostPort: 32768 + publicPort - 18810, Port: 80}
}

func TestEdgeBridgeRoutesByContainerName(t *testing.T) {
	rec, st, er, rt := newEdgeHarness(t, runner.EdgeModeBridge, state.ExposureLAN)
	liveEdgeApp(st, er, "web", "", 18810)
	liveEdgeApp(st, er, "notes", state.ExposureLocal, 18811)
	ctx := context.Background()
	rec.syncRoutesIfChanged(ctx)

	got := rt.routes["web"]
	if got.Upstream != "af-web-aaaaaaaaaaaa:80" || got.Listen != "" || got.LANPort != 28810 {
		t.Fatalf("web route = %+v, want upstream by name and a LAN listener on 28810", got)
	}
	if n := rt.routes["notes"]; n.Listen != "" || n.LANPort != 0 {
		t.Fatalf("local-only app must not listen for LAN clients: %+v", n)
	}
	spec := er.last()
	// The whole reserved range, twice: loopback and the LAN address.
	if spec.Mode != runner.EdgeModeBridge || len(spec.Ports) != 10 {
		t.Fatalf("spec = %+v", spec)
	}
	if spec.Ports[0] != (runner.EdgePort{HostIP: "127.0.0.1", Port: 18810}) || spec.Ports[1] != (runner.EdgePort{HostIP: testLAN, Port: 18810, ContainerPort: 28810}) {
		t.Fatalf("ports = %+v", spec.Ports[:2])
	}
	if len(er.connected) != 2 {
		t.Fatalf("connected = %v, want both app networks", er.connected)
	}
}

func TestEdgeHostModeKeepsLoopbackUpstreams(t *testing.T) {
	rec, st, er, rt := newEdgeHarness(t, runner.EdgeModeHost, state.ExposurePublic)
	liveEdgeApp(st, er, "web", "", 18810)
	liveEdgeApp(st, er, "admin", state.ExposureLocal, 18811)
	rec.syncRoutesIfChanged(context.Background())

	if got := rt.routes["web"]; got.Upstream != "127.0.0.1:32768" || got.Listen != "" {
		t.Fatalf("web route = %+v", got)
	}
	if got := rt.routes["admin"]; got.Listen != "127.0.0.1" {
		t.Fatalf("local exposure on Linux must bind 127.0.0.1: %+v", got)
	}
	if spec := er.last(); len(spec.Ports) != 0 || spec.Mode != runner.EdgeModeHost {
		t.Fatalf("host mode spec must publish nothing: %+v", spec)
	}
	if len(er.connected) != 0 {
		t.Fatalf("host mode must not join app networks: %v", er.connected)
	}
}

// Reserved ports: adding/removing apps and changing exposure
// change routes only; the edge container is recreated only when the LAN
// address changes.
func TestEdgeReservedRangeAvoidsRecreation(t *testing.T) {
	rec, st, er, rt := newEdgeHarness(t, runner.EdgeModeBridge, state.ExposureLAN)
	liveEdgeApp(st, er, "web", "", 18810)
	ctx := context.Background()
	rec.syncRoutesIfChanged(ctx)
	rec.mu.Lock()
	rec.firstRun = false
	rec.mu.Unlock()
	calls := len(er.specs)

	a, _ := st.GetApp(ctx, "web")
	a.Exposure = state.ExposureLocal
	st.putApp(a)
	rec.syncRoutesIfChanged(ctx)
	if rt.routes["web"].LANPort != 0 {
		t.Fatalf("exposure local must drop the LAN listener: %+v", rt.routes["web"])
	}
	liveEdgeApp(st, er, "api", "", 18811)
	rec.syncRoutesIfChanged(ctx)
	if rt.routes["api"].LANPort != 28811 {
		t.Fatalf("new app route = %+v", rt.routes["api"])
	}
	if len(er.specs) != calls {
		t.Fatalf("exposure change / new app touched the edge container (%d -> %d EnsureEdge calls)", calls, len(er.specs))
	}

	// A new LAN address is the one reason to recreate.
	testLAN = "192.168.31.99"
	defer func() { testLAN = "192.168.31.46" }()
	rec.syncRoutesIfChanged(ctx)
	if len(er.specs) != calls+1 || er.last().Ports[1].HostIP != "192.168.31.99" {
		t.Fatalf("LAN address change must re-publish: %d calls, last %+v", len(er.specs), er.last().Ports[:2])
	}
}

func TestEdgeDomainsPublishHTTPPorts(t *testing.T) {
	rec, st, er, _ := newEdgeHarness(t, runner.EdgeModeBridge, state.ExposureLAN)
	rec.cfg.HTTPS.HTTPPort, rec.cfg.HTTPS.HTTPSPort = 80, 443
	liveEdgeApp(st, er, "web", "", 18810)
	st.domains["web/a.example.com"] = &state.Domain{App: "web", Name: "a.example.com", Status: "ready"}
	rec.syncRoutesIfChanged(context.Background())
	ports := map[int]string{}
	for _, p := range er.last().Ports {
		ports[p.Port] = p.HostIP
	}
	if ports[80] != "0.0.0.0" || ports[443] != "0.0.0.0" {
		t.Fatalf("domains need 80/443 published: %+v", er.last().Ports)
	}
}

func TestNoEdgeConfigKeepsHostCaddy(t *testing.T) {
	st, rt := newStore(), newRouter()
	er := &edgeRunner{fakeRunner: newRunner()}
	rec := New(Config{Store: st, Runner: er, Router: rt, UploadDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Tick: time.Hour})
	liveEdgeApp(st, er, "web", "", 18810)
	rec.syncRoutesIfChanged(context.Background())
	if len(er.specs) != 0 {
		t.Fatalf("without EdgeConfig the reconciler must not manage a Caddy container")
	}
	if rt.routes["web"].Upstream != "127.0.0.1:32768" {
		t.Fatalf("route = %+v", rt.routes["web"])
	}
}

// concurrentEdge fails when two EnsureEdge calls overlap.
type concurrentEdge struct {
	edgeRunner
	active, maxActive int32
}

func (c *concurrentEdge) EnsureEdge(ctx context.Context, s runner.EdgeSpec) (runner.EdgeInfo, error) {
	c.mu.Lock()
	c.active++
	if c.active > c.maxActive {
		c.maxActive = c.active
	}
	c.mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	c.mu.Lock()
	c.active--
	c.mu.Unlock()
	return c.edgeRunner.EnsureEdge(ctx, s)
}

func TestEnsureEdgeIsSerialized(t *testing.T) {
	st, rt := newStore(), newRouter()
	ce := &concurrentEdge{edgeRunner: edgeRunner{fakeRunner: newRunner()}}
	rec := New(Config{Store: st, Runner: ce, Router: rt, UploadDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Tick: time.Hour,
		Edge: &EdgeConfig{Mode: runner.EdgeModeHost, AdminPort: 18808, AdminToken: "0123456789abcdef0123456789abcdef"}})
	liveEdgeApp(st, &ce.edgeRunner, "web", "", 18810)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); rec.EnsureEdge(context.Background()) }()
	}
	wg.Wait()
	if ce.maxActive != 1 {
		t.Fatalf("EnsureEdge ran %d at once; the edge container would be created twice", ce.maxActive)
	}
}
