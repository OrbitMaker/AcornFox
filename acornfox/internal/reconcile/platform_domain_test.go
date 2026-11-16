package reconcile

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// platformBase is a settable platform domain for the tests.
type platformBase struct {
	mu sync.Mutex
	d  string
}

func (p *platformBase) get(context.Context) string { p.mu.Lock(); defer p.mu.Unlock(); return p.d }
func (p *platformBase) set(d string)               { p.mu.Lock(); p.d = d; p.mu.Unlock() }

// A public app's route carries <app>.<platform domain>; local, lan and
// internal apps' do not; an app's own domains come first and stay; the name
// goes when the app is no longer public or the platform domain changes.
func TestPlatformDomainOnRoutes(t *testing.T) {
	rec, st, er, rt := newEdgeHarness(t, runner.EdgeModeBridge, state.ExposureLAN)
	base := &platformBase{}
	rec.cfg.PlatformDomain = base.get
	liveEdgeApp(st, er, "hello", state.ExposurePublic, 18810)
	liveEdgeApp(st, er, "notes", state.ExposureLAN, 18811)
	liveEdgeApp(st, er, "mine", "", 18812) // the default: lan here
	liveEdgeApp(st, er, "local", state.ExposureLocal, 18813)
	liveEdgeApp(st, er, "own", state.ExposurePublic, 18814)
	st.putDomain(state.Domain{App: "own", Name: "own.example.org", Status: state.DomainReady})
	ctx := context.Background()

	// No platform domain: exactly as before.
	rec.syncRoutesIfChanged(ctx)
	for name, r := range rt.routes {
		if name == "own" {
			if !reflect.DeepEqual(r.Domains, []string{"own.example.org"}) {
				t.Fatalf("own: %v", r.Domains)
			}
			continue
		}
		if len(r.Domains) != 0 {
			t.Fatalf("%s carries %v without a platform domain", name, r.Domains)
		}
	}
	if hosts := rec.PlatformHosts(ctx); len(hosts) != 0 {
		t.Fatalf("hosts %v", hosts)
	}

	base.set("apps.example.com")
	rec.syncRoutesIfChanged(ctx)
	if got := rt.routes["hello"].Domains; !reflect.DeepEqual(got, []string{"hello.apps.example.com"}) {
		t.Fatalf("hello: %v", got)
	}
	if got := rt.routes["own"].Domains; !reflect.DeepEqual(got, []string{"own.example.org", "own.apps.example.com"}) {
		t.Fatalf("own: %v", got)
	}
	for _, name := range []string{"notes", "mine", "local"} {
		if d := rt.routes[name].Domains; len(d) != 0 {
			t.Fatalf("%s is not public but carries %v", name, d)
		}
	}
	if hosts := rec.PlatformHosts(ctx); !reflect.DeepEqual(hosts, []string{"hello.apps.example.com", "own.apps.example.com"}) {
		t.Fatalf("hosts %v", hosts)
	}

	// Back to lan: the name is gone from the next configuration.
	a := st.getApp("hello")
	a.Exposure = state.ExposureLAN
	st.putApp(a)
	rec.syncRoutesIfChanged(ctx)
	if d := rt.routes["hello"].Domains; len(d) != 0 {
		t.Fatalf("hello after lan: %v", d)
	}

	// Another platform domain replaces the names; clearing it drops them.
	base.set("web.example.net")
	rec.syncRoutesIfChanged(ctx)
	if got := rt.routes["own"].Domains; !reflect.DeepEqual(got, []string{"own.example.org", "own.web.example.net"}) {
		t.Fatalf("own: %v", got)
	}
	base.set("")
	rec.syncRoutesIfChanged(ctx)
	if got := rt.routes["own"].Domains; !reflect.DeepEqual(got, []string{"own.example.org"}) {
		t.Fatalf("own after clear: %v", got)
	}
}

// A name that is already a domain of another app, or the public console's,
// is not put on a second route; an internal app never gets one.
func TestPlatformDomainSkipsTakenNames(t *testing.T) {
	rec, st, er, rt := newEdgeHarness(t, runner.EdgeModeHost, state.ExposurePublic)
	rec.cfg.PlatformDomain = func(context.Context) string { return "apps.example.com" }
	rec.cfg.ExtraRoutes = func(context.Context) ([]edge.Route, error) {
		return []edge.Route{{App: "acornfox-console", Upstream: "127.0.0.1:18803", Domains: []string{"console.apps.example.com"}}}, nil
	}
	liveEdgeApp(st, er, "hello", state.ExposurePublic, 18810)
	liveEdgeApp(st, er, "other", state.ExposurePublic, 18811)
	liveEdgeApp(st, er, "console", state.ExposurePublic, 18812)
	db := baseApp("db")
	db.Internal = true
	db.PublicPort = 18813
	db.CurrentDeployment = "aaaaaaaaaaaa"
	st.putApp(db)
	st.putDomain(state.Domain{App: "other", Name: "hello.apps.example.com", Status: state.DomainReady})
	ctx := context.Background()
	routes, err := rec.desiredRoutes(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	byApp := map[string]edge.Route{}
	for _, r := range routes {
		byApp[r.App] = r
	}
	if d := byApp["hello"].Domains; len(d) != 0 {
		t.Fatalf("hello's name is bound to other: %v", d)
	}
	if d := byApp["other"].Domains; !reflect.DeepEqual(d, []string{"hello.apps.example.com", "other.apps.example.com"}) {
		t.Fatalf("other: %v", d)
	}
	if d := byApp["console"].Domains; len(d) != 0 {
		t.Fatalf("console's name is the public console's: %v", d)
	}
	if r, ok := byApp["db"]; ok && len(r.Domains) != 0 {
		t.Fatalf("internal app: %v", r.Domains)
	}
	_ = rt
}
