package caddyroute

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// Replace loads the whole configuration in one request; Version reads the
// same version back; the challenge server survives; the TLS policy, ports,
// admin socket and grace period are part of the same load.
func TestReplaceLoadsEverythingAtOnce(t *testing.T) {
	f := startFakeCaddy(t, nil)
	d := New(f.socket, HTTPSConfig{})
	ctx := context.Background()
	base := Base{AdminSocket: "/tmp/afx-edge/proxy-admin.sock", Grace: 8 * time.Second}
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
	if err := d.Replace(ctx, base, cfg); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if f.loads != 1 {
		t.Fatalf("loads = %d, want one load for the whole configuration", f.loads)
	}
	if v, err := d.Version(ctx); err != nil || v != cfg.Version() {
		t.Fatalf("Version = %q %v, want %q", v, err, cfg.Version())
	}
	srv := f.servers()
	vs, ok := srv[verifyServer].(map[string]any)
	if !ok || vs["routes"] == nil {
		t.Fatalf("the challenge server was not kept: %v", srv[verifyServer])
	}
	b, _ := json.Marshal(vs)
	if !jsonHas(b, `"body":"tok"`) {
		t.Fatalf("challenge body lost: %s", b)
	}
	if p := f.afPolicy(); p == nil {
		t.Fatal("the TLS policy is not part of the load")
	}
	httpApp := f.config["apps"].(map[string]any)["http"].(map[string]any)
	if httpApp["grace_period"] != "8s" || httpApp["https_port"].(float64) != 18443 || httpApp["http_port"].(float64) != 18080 {
		t.Fatalf("http app = %v", httpApp)
	}
	admin := f.config["admin"].(map[string]any)
	if admin["listen"] != "unix//tmp/afx-edge/proxy-admin.sock" || admin["config"].(map[string]any)["persist"] != false {
		t.Fatalf("admin = %v", admin)
	}

	// A smaller configuration replaces everything: no stale server, no policy.
	small := edge.Config{Routes: []edge.Route{{App: "admin", PublicPort: 18811, Upstream: "127.0.0.1:32769"}}}
	if err := d.Replace(ctx, base, small); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.servers()["af-web"]; ok {
		t.Fatal("af-web survived a replace without it")
	}
	if _, ok := f.servers()[domainsServer]; ok {
		t.Fatal("the domains server survived a replace without domains")
	}
	if f.afPolicy() != nil {
		t.Fatal("the TLS policy survived a replace without domains")
	}
	if v, _ := d.Version(ctx); v != small.Version() {
		t.Fatalf("Version = %q, want %q", v, small.Version())
	}
}

// ACME without an account email owns no policy (Caddy's default issuers), as
// with Sync.
func TestRenderACMEWithoutEmail(t *testing.T) {
	b, err := Render(Base{AdminSocket: "/s", Grace: time.Second}, edge.Config{
		Routes: []edge.Route{{App: "web", PublicPort: 18810, Upstream: "x:1", Domains: []string{"a.example.com"}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if jsonHas(b, `"tls"`) {
		t.Fatalf("unexpected TLS app: %s", b)
	}
	b, _ = Render(Base{AdminSocket: "/s", Grace: time.Second}, edge.Config{
		Routes: []edge.Route{{App: "web", PublicPort: 18810, Upstream: "x:1", Domains: []string{"a.example.com"}}},
		HTTPS:  edge.HTTPS{Email: "ops@example.com"},
	}, nil)
	if !jsonHas(b, `"email": "ops@example.com"`) {
		t.Fatalf("ACME email policy missing: %s", b)
	}
}

func jsonHas(b []byte, s string) bool { return strings.Contains(string(b), s) }
