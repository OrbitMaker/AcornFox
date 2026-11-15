package caddyroute

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The edge container's admin gateway is plain HTTP on loopback and requires
// the token header; behind it is the same admin API.
func TestTokenGatewayAndListenHost(t *testing.T) {
	f := startFakeCaddy(t, nil)
	var sawToken string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawToken = r.Header.Get(TokenHeader)
		if sawToken != "s3cret" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		f.handle(w, r)
	}))
	defer gw.Close()

	ctx := context.Background()
	if err := New(gw.URL, HTTPSConfig{}).Sync(ctx, []Route{{App: "web", PublicPort: 18810, Upstream: "x:80"}}); err == nil {
		t.Fatal("sync without the token must fail")
	}
	r := New(gw.URL, HTTPSConfig{}, WithToken("s3cret"))
	routes := []Route{
		{App: "web", PublicPort: 18810, Upstream: "af-web-aaaaaaaaaaaa:80"},
		{App: "admin", PublicPort: 18811, Listen: "127.0.0.1", Upstream: "127.0.0.1:32001"},
	}
	if err := r.Sync(ctx, routes); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	listen := f.servers()["af-admin"].(map[string]any)["listen"].([]any)
	if listen[0] != "127.0.0.1:18811" {
		t.Fatalf("listen = %v, want 127.0.0.1:18811", listen)
	}
	cur, err := r.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cur["admin"].Listen != "127.0.0.1" || cur["web"].Listen != "" || cur["web"].Upstream != "af-web-aaaaaaaaaaaa:80" {
		t.Fatalf("current = %+v", cur)
	}

	// Changing only the listen host must rewrite the server.
	routes[1].Listen = ""
	if err := r.Sync(ctx, routes); err != nil {
		t.Fatal(err)
	}
	listen = f.servers()["af-admin"].(map[string]any)["listen"].([]any)
	if listen[0] != ":18811" {
		t.Fatalf("listen after change = %v", listen)
	}
}

func TestParseListenHost(t *testing.T) {
	for in, want := range map[string]string{":18810": "", "127.0.0.1:18810": "127.0.0.1", "tcp/0.0.0.0:80": "0.0.0.0"} {
		if got := parseListenHost([]string{in}); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
