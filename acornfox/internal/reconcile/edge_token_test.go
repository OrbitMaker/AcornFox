package reconcile

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// tokenRouter is a proxy whose entry container may hold another management
// token than the data directory: while refuse is set it answers every call
// with the token refusal the real driver reports (EDGE-03).
type tokenRouter struct {
	*fakeRouter
	mu     sync.Mutex
	refuse bool
}

func (t *tokenRouter) refusing() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.refuse
}

func (t *tokenRouter) set(refuse bool) {
	t.mu.Lock()
	t.refuse = refuse
	t.mu.Unlock()
}

var errTokenRefused = errors.New("入口代理的管理口拒绝了令牌（401，故障编号 " + edge.FaultAdmin + "）")

func (t *tokenRouter) Apply(ctx context.Context, cfg edge.Config) (string, error) {
	if t.refusing() {
		return "", errTokenRefused
	}
	return t.fakeRouter.Apply(ctx, cfg)
}

func (t *tokenRouter) Version(ctx context.Context) (string, error) {
	if t.refusing() {
		return "", errTokenRefused
	}
	return t.fakeRouter.Version(ctx)
}

func (t *tokenRouter) Health(ctx context.Context) edge.Health {
	if t.refusing() {
		return edge.Health{State: edge.StateError, Cause: edge.CauseAuth, Reason: errTokenRefused.Error()}
	}
	return t.fakeRouter.Health(ctx)
}

// newTokenHarness is newEdgeHarness (host mode) with a tokenRouter. fixes
// decides whether a recreation with the data directory's token makes the
// container accept it (true) or the container keeps refusing (false).
func newTokenHarness(t *testing.T, refuse, fixes bool) (*Reconciler, *edgeRunner, *tokenRouter) {
	t.Helper()
	rec, st, er, rt := newEdgeHarness(t, runner.EdgeModeHost, state.ExposurePublic)
	tr := &tokenRouter{fakeRouter: rt, refuse: refuse}
	rec.cfg.Proxy = tr
	er.onEnsure = func(s runner.EdgeSpec) {
		if s.Recreate && fixes {
			tr.set(false)
		}
	}
	liveEdgeApp(st, er, "web", "", 18810)
	// The entry container exists from an earlier run (created with the spec
	// the reconciler computes), so the start keeps it.
	er.lastKey = specKey(rec.edgeSpec(context.Background(), nil))
	// A container that keeps refusing never answers Version: do not wait
	// the full time for it.
	old := edgeReadyTimeout
	edgeReadyTimeout = 50 * time.Millisecond
	t.Cleanup(func() { edgeReadyTimeout = old })
	return rec, er, tr
}

func recreates(er *edgeRunner) int {
	er.mu.Lock()
	defer er.mu.Unlock()
	n := 0
	for _, s := range er.specs {
		if s.Recreate {
			n++
		}
	}
	return n
}

// The server starts and the running entry container refuses the token in
// the data directory: it is rebuilt in place once (with the data directory's
// token and the whole configuration), and the routes are handed over.
func TestEdgeTokenRefusedAtStartRecreatesOnce(t *testing.T) {
	rec, er, tr := newTokenHarness(t, true, true)
	ctx := context.Background()
	if !rec.EnsureEdge(ctx) {
		t.Fatal("EnsureEdge failed")
	}
	if n := recreates(er); n != 1 {
		t.Fatalf("%d recreations, want 1", n)
	}
	last := er.last()
	if !last.Recreate || last.AdminToken != rec.cfg.Edge.AdminToken || last.Config == nil || len(last.Config.Routes) != 1 {
		t.Fatalf("recreation spec = %+v, want the data directory's token and the whole configuration", last)
	}
	if !tr.hasRoute("web") {
		t.Fatal("the routes were not handed over after the recreation")
	}
	if st, msg := rec.EdgeStatus(); st != EdgeStateRunning {
		t.Fatalf("edge state %s %q", st, msg)
	}
	// Later starts and rounds of the same run leave the container alone.
	rec.EnsureEdge(ctx)
	rec.syncRoutesIfChanged(ctx)
	if n := recreates(er); n != 1 {
		t.Fatalf("%d recreations after a second start and a round, want 1", n)
	}
}

// A container that still refuses the token after the one recreation is not
// recreated again in this run: no matter how often the start is retried or
// rounds run, the state says EDGE-03 and nothing touches the container.
func TestEdgeTokenStillRefusedIsNotRecreatedAgain(t *testing.T) {
	rec, er, _ := newTokenHarness(t, true, false)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		rec.EnsureEdge(ctx)
		rec.syncRoutesIfChanged(ctx)
	}
	if n := recreates(er); n != 1 {
		t.Fatalf("%d recreations, want exactly 1", n)
	}
	if st, msg := rec.EdgeStatus(); st != EdgeStateError || !strings.Contains(msg, edge.FaultAdmin) {
		t.Fatalf("edge state %s %q, want error with %s", st, msg, edge.FaultAdmin)
	}
}

// A container that accepts the token is never recreated: not at the start,
// not in rounds. A container that starts refusing while the server runs is
// left as it is too (doctor reports EDGE-03 until the next start).
func TestEdgeTokenAcceptedIsNotRecreated(t *testing.T) {
	rec, er, tr := newTokenHarness(t, false, true)
	ctx := context.Background()
	if !rec.EnsureEdge(ctx) {
		t.Fatal("EnsureEdge failed")
	}
	rec.syncRoutesIfChanged(ctx)
	if n := recreates(er); n != 0 {
		t.Fatalf("%d recreations of a container that accepts the token", n)
	}
	if !tr.hasRoute("web") {
		t.Fatal("routes not handed over")
	}
	tr.set(true)
	rec.mu.Lock()
	rec.firstRun = false
	rec.mu.Unlock()
	rec.syncRoutesIfChanged(ctx)
	rec.syncRoutesIfChanged(ctx)
	if n := recreates(er); n != 0 {
		t.Fatalf("%d recreations from rounds, want none (only the start recreates)", n)
	}
}

// A management endpoint that does not answer yet is waited for; one that
// never answers is not a token refusal and recreates nothing.
func TestEdgeTokenUnreachableIsNotRecreated(t *testing.T) {
	old := edgeTokenCheckWait
	edgeTokenCheckWait = 50 * time.Millisecond
	defer func() { edgeTokenCheckWait = old }()
	rec, er, tr := newTokenHarness(t, false, true)
	rec.cfg.Proxy = unreachableRouter{tr}
	rec.EnsureEdge(context.Background())
	if n := recreates(er); n != 0 {
		t.Fatalf("%d recreations for an endpoint that does not answer", n)
	}
}

type unreachableRouter struct{ *tokenRouter }

func (u unreachableRouter) Health(context.Context) edge.Health {
	return edge.Health{State: edge.StateError, Cause: edge.CauseUnreachable, Reason: "入口代理的管理口没有应答"}
}
