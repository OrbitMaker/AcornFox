package reconcile

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/runner"
)

// switchingEdge reports a switch from an old entry container to a new one on
// another management port, the way the runner does after an upgrade.
type switchingEdge struct {
	edgeRunner
	info runner.EdgeInfo
	err  error
}

func (s *switchingEdge) EnsureEdge(ctx context.Context, sp runner.EdgeSpec) (runner.EdgeInfo, error) {
	if _, err := s.edgeRunner.EnsureEdge(ctx, sp); err != nil {
		return runner.EdgeInfo{}, err
	}
	if s.err != nil {
		return runner.EdgeInfo{}, s.err
	}
	return s.info, nil
}

func newSwitchHarness(t *testing.T, se *switchingEdge, onPort func(int), onSwitch func(runner.EdgeInfo)) (*Reconciler, *fakeStore, *fakeRouter) {
	t.Helper()
	st, rt := newStore(), newRouter()
	rec := New(Config{Store: st, Runner: se, Proxy: rt, UploadDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Tick: time.Hour,
		Edge: &EdgeConfig{Mode: runner.EdgeModeHost, AdminPort: 18807, AdminToken: "0123456789abcdef0123456789abcdef",
			UseAdminPort: onPort, OnSwitch: onSwitch}})
	return rec, st, rt
}

// The runner gets the whole configuration (every route) with the spec, so a
// new entry container serves all apps from its first second; a switch is
// reported once and the proxy follows the management port the runner says.
func TestEnsureEdgeHandsConfigAndFollowsSwitch(t *testing.T) {
	se := &switchingEdge{edgeRunner: edgeRunner{fakeRunner: newRunner()},
		info: runner.EdgeInfo{ID: "new", Running: true, Recreated: true, AdminPort: 18806, Replaced: "old", ReplacedName: "old-entry"}}
	var port int
	var switched []runner.EdgeInfo
	rec, st, _ := newSwitchHarness(t, se, func(p int) { port = p }, func(i runner.EdgeInfo) { switched = append(switched, i) })
	liveEdgeApp(st, &se.edgeRunner, "web", "", 18810)
	if !rec.EnsureEdge(context.Background()) {
		t.Fatal("EnsureEdge failed")
	}
	sp := se.last()
	if sp.Config == nil || len(sp.Config.Routes) != 1 || sp.Config.Routes[0].App != "web" || sp.Config.Routes[0].PublicPort != 18810 {
		t.Fatalf("spec config = %+v", sp.Config)
	}
	if port != 18806 {
		t.Fatalf("proxy moved to %d, want 18806", port)
	}
	if len(switched) != 1 || switched[0].Replaced != "old" {
		t.Fatalf("switch records = %+v", switched)
	}
}

// A failed switch (EDGE-05) leaves the edge state in error with the code.
func TestEnsureEdgeSwitchFailureIsReported(t *testing.T) {
	se := &switchingEdge{edgeRunner: edgeRunner{fakeRunner: newRunner()},
		err: errors.New("runner 502 EDGE-05: 换成新的入口容器没有成功（故障编号 EDGE-05）")}
	rec, st, _ := newSwitchHarness(t, se, nil, func(runner.EdgeInfo) { t.Error("no switch record on failure") })
	liveEdgeApp(st, &se.edgeRunner, "web", "", 18810)
	if rec.EnsureEdge(context.Background()) {
		t.Fatal("EnsureEdge reported success")
	}
	if s, msg := rec.EdgeStatus(); s != EdgeStateError || !strings.Contains(msg, edge.FaultSwitchFailed) {
		t.Fatalf("status %q %q", s, msg)
	}
}

// staleRouter accepts a configuration but keeps serving another one.
type staleRouter struct{ *fakeRouter }

func (s staleRouter) Apply(ctx context.Context, cfg edge.Config) (string, error) {
	if _, err := s.fakeRouter.Apply(ctx, cfg); err != nil {
		return "", err
	}
	return "v1-other", nil
}

// A configuration the proxy accepted but does not serve is EDGE-04; a later
// hand-over that takes effect clears it.
func TestApplyNotInEffectIsReported(t *testing.T) {
	st, rt := newStore(), newRouter()
	er := &edgeRunner{fakeRunner: newRunner()}
	stale := staleRouter{rt}
	rec := New(Config{Store: st, Runner: er, Proxy: stale, UploadDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Tick: time.Hour,
		Edge: &EdgeConfig{Mode: runner.EdgeModeHost, AdminPort: 18807, AdminToken: "0123456789abcdef0123456789abcdef"}})
	liveEdgeApp(st, er, "web", "", 18810)
	rec.EnsureEdge(context.Background())
	if s, msg := rec.EdgeStatus(); s != EdgeStateError || !strings.Contains(msg, edge.FaultNotApplied) {
		t.Fatalf("status %q %q", s, msg)
	}
	rec.cfg.Proxy = rt
	if err := rec.applyRoutes(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if s, _ := rec.EdgeStatus(); s != EdgeStateRunning {
		t.Fatalf("status %q after a hand-over that took effect", s)
	}
}
