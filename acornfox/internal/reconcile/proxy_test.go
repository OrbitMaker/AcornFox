package reconcile

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// The reconciler hands the proxy the whole configuration (app routes, extra
// routes, HTTPS settings), re-applies only when the version in effect
// differs, and re-applies after the proxy lost its configuration.
func TestProxyConfigVersionAndExtraRoutes(t *testing.T) {
	st, rt := newStore(), newRouter()
	er := &edgeRunner{fakeRunner: newRunner()}
	extra := []edge.Route{{App: "_console", Upstream: "127.0.0.1:18801", Domains: []string{"panel.example.com"}}}
	var extraErr error
	rec := New(Config{Store: st, Runner: er, Proxy: rt, UploadDir: t.TempDir(),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Tick: time.Hour,
		HTTPS:       edge.HTTPS{HTTPPort: 8080, HTTPSPort: 8443, Issuer: edge.IssuerInternal},
		ExtraRoutes: func(context.Context) ([]edge.Route, error) { return extra, extraErr }})
	liveEdgeApp(st, er, "web", "", 18810)
	ctx := context.Background()

	rec.syncRoutesIfChanged(ctx) // first round: always applies
	if rt.syncs != 1 || rt.routes["web"].Upstream != "127.0.0.1:32768" || rt.routes["_console"].Domains[0] != "panel.example.com" {
		t.Fatalf("first apply: syncs=%d routes=%+v", rt.syncs, rt.routes)
	}
	if rt.https.HTTPSPort != 8443 || rt.https.Issuer != edge.IssuerInternal {
		t.Fatalf("HTTPS settings not handed over: %+v", rt.https)
	}

	rec.firstRun = false
	rec.syncRoutesIfChanged(ctx)
	if rt.syncs != 1 {
		t.Fatalf("unchanged configuration was applied again (syncs=%d)", rt.syncs)
	}

	// The proxy lost its configuration (restart): applied again.
	rt.mu.Lock()
	rt.routes = map[string]edge.Route{}
	rt.mu.Unlock()
	rec.syncRoutesIfChanged(ctx)
	if rt.syncs != 2 || rt.routes["_console"].App == "" {
		t.Fatalf("lost configuration not restored: syncs=%d routes=%+v", rt.syncs, rt.routes)
	}

	// The extra route went away (console turned off): applied without it.
	extra = nil
	rec.syncRoutesIfChanged(ctx)
	if rt.syncs != 3 {
		t.Fatalf("removed extra route not applied (syncs=%d)", rt.syncs)
	}
	if _, ok := rt.routes["_console"]; ok {
		t.Fatalf("stale extra route kept: %+v", rt.routes)
	}

	// An extra-route error fails the hand-over and keeps the proxy as it was.
	extraErr = errors.New("broken config")
	extra = []edge.Route{{App: "_console", Upstream: "127.0.0.1:18801", Domains: []string{"other.example.com"}}}
	if err := rec.syncRoutes(ctx, nil); err == nil {
		t.Fatal("syncRoutes must fail when the extra routes cannot be read")
	}
	if rt.syncs != 3 {
		t.Fatalf("proxy changed despite the error (syncs=%d)", rt.syncs)
	}
}
