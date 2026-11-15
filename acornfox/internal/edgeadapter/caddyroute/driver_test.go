package caddyroute

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acornfox/acornfox/internal/edge"
)

// Apply writes the routes and returns the version Version reads back from
// Caddy; losing Caddy's config (a restart) changes Version; the challenge
// server survives Apply.
func TestApplyVersionRoundTrip(t *testing.T) {
	f := startFakeCaddy(t, nil)
	d := New(f.socket, HTTPSConfig{})
	ctx := context.Background()

	cfg := edge.Config{
		Routes: []edge.Route{
			{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:32768", Domains: []string{"b.example.com", "a.example.com"}},
			{App: "admin", PublicPort: 18811, Listen: "127.0.0.1", Upstream: "127.0.0.1:32769"},
			{App: "notes", PublicPort: 18812, LANPort: 28812, Upstream: "af-notes-x:80"},
			{App: "_public-console", Upstream: "127.0.0.1:18801", Domains: []string{"panel.example.com"}},
		},
		HTTPS: edge.HTTPS{HTTPPort: 18080, HTTPSPort: 18443, Issuer: edge.IssuerInternal},
	}
	if err := d.SetChallenge(ctx, "panel.example.com", "/.well-known/x", "tok"); err != nil {
		t.Fatal(err)
	}
	v, err := d.Apply(ctx, cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if v != cfg.Version() {
		t.Fatalf("Apply returned %s, want %s", v, cfg.Version())
	}
	got, err := d.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != v {
		t.Fatalf("Version after Apply = %s, want %s (Caddy servers: %v)", got, v, f.servers())
	}
	if _, ok := f.servers()[verifyServer]; !ok {
		t.Fatal("Apply removed the challenge server")
	}
	// The HTTPS settings of the configuration were taken over.
	if srv := f.servers()[domainsServer].(map[string]any); srv["listen"].([]any)[0] != ":18443" {
		t.Fatalf("domains server listen = %v", srv["listen"])
	}

	// Caddy restarted with an empty config: the version no longer matches.
	f.mu.Lock()
	f.config = map[string]any{}
	f.mu.Unlock()
	got, err = d.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got == v {
		t.Fatal("Version must change once Caddy lost its configuration")
	}
	if _, err := d.Apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.Version(ctx); got != v {
		t.Fatalf("Version after re-Apply = %s, want %s", got, v)
	}
}

func TestVersionErrorWhenUnreachable(t *testing.T) {
	d := New("/nonexistent/admin.sock", HTTPSConfig{})
	if _, err := d.Version(context.Background()); err == nil {
		t.Fatal("Version must fail when Caddy cannot be reached")
	}
}

// Health through the token gateway: refused without the token, accepted with
// it, Caddy's own admin port closed.
func TestHealthGateway(t *testing.T) {
	f := startFakeCaddy(t, nil)
	open := false // the gateway answers without the token
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !open && r.Header.Get(TokenHeader) != "s3cret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		f.handle(w, r)
	}))
	defer gw.Close()
	ctx := context.Background()

	d := New(gw.URL, HTTPSConfig{}, WithToken("s3cret"))
	adminOpen := false
	d.defaultAdminPortOpen = func() bool { return adminOpen }
	if h := d.Health(ctx); !h.OK() {
		t.Fatalf("healthy gateway reported %+v", h)
	}

	adminOpen = true
	if h := d.Health(ctx); h.OK() || h.Cause != edge.CauseExposed {
		t.Fatalf("open default admin port: %+v", h)
	}
	adminOpen = false

	open = true
	if h := d.Health(ctx); h.OK() || h.Cause != edge.CauseExposed {
		t.Fatalf("gateway without token check: %+v", h)
	}
	open = false

	bad := New(gw.URL, HTTPSConfig{}, WithToken("wrong"))
	bad.defaultAdminPortOpen = func() bool { return false }
	if h := bad.Health(ctx); h.OK() || h.Cause != edge.CauseAuth {
		t.Fatalf("wrong token: %+v", h)
	}

	gw.Close()
	if h := d.Health(ctx); h.OK() || h.Cause != edge.CauseUnreachable {
		t.Fatalf("closed gateway: %+v", h)
	}
}

// On a unix socket (the v0.3 host Caddy) there is no token gateway.
func TestHealthUnixSocket(t *testing.T) {
	f := startFakeCaddy(t, nil)
	d := New(f.socket, HTTPSConfig{})
	d.defaultAdminPortOpen = func() bool { return true } // not checked without a token
	if h := d.Health(context.Background()); !h.OK() {
		t.Fatalf("unix socket: %+v", h)
	}
	if h := New("/nonexistent/admin.sock", HTTPSConfig{}).Health(context.Background()); h.OK() || h.Cause != edge.CauseUnreachable {
		t.Fatalf("missing socket: %+v", h)
	}
}
