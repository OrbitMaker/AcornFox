package edgeadapter

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// TestRealProxy runs the adapter with the real proxy program. It is skipped
// unless ACORNFOX_EDGE_IT=1 and ACORNFOX_EDGE_PROXY names the proxy binary
// (the caddy file of the entry package, or of the upstream release archive).
// It needs no Docker and no root; it uses free loopback ports and temporary
// directories only, so it may run on any Linux machine. The gate does not run
// it; run it by hand:
//
//	go test -c -o edgeadapter.test ./internal/edgeadapter/
//	ACORNFOX_EDGE_IT=1 ACORNFOX_EDGE_PROXY=/path/to/caddy ./edgeadapter.test -test.run RealProxy -test.v
//
// It checks: a configuration applies and serves (plain port and a domain
// with the proxy's internal CA, HTTP redirected to HTTPS); the in-container
// health check passes from this process's own sockets; after a restart the
// adapter serves every route again from the saved configuration within 5
// seconds without any Apply; a request running when the stop signal comes
// still completes.
func TestRealProxy(t *testing.T) {
	if os.Getenv("ACORNFOX_EDGE_IT") != "1" {
		t.Skip("set ACORNFOX_EDGE_IT=1 and ACORNFOX_EDGE_PROXY to run the adapter with the real proxy")
	}
	bin := os.Getenv("ACORNFOX_EDGE_PROXY")
	if bin == "" {
		t.Fatal("ACORNFOX_EDGE_PROXY not set")
	}
	const body = "hello through the entry proxy"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			time.Sleep(3 * time.Second)
		}
		io.WriteString(w, body)
	}))
	defer upstream.Close()

	e := newRunEnv(t)
	data := t.TempDir()
	e.opts.ProxyBin = bin
	e.opts.ProxyEnv = []string{"XDG_DATA_HOME=" + data, "XDG_CONFIG_HOME=" + data, "HOME=" + data, "PATH=/usr/bin:/bin"}
	e.opts.Grace = 5 * time.Second
	ports := freeTCPPorts(t, 3)
	appPort, httpPort, httpsPort := ports[0], ports[1], ports[2]
	const domain = "it.acornfox.test"
	cfg := edge.Config{
		Routes: []edge.Route{
			{App: "web", PublicPort: appPort, Listen: "127.0.0.1", Upstream: upstream.Listener.Addr().String(), Domains: []string{domain}},
		},
		HTTPS: edge.HTTPS{HTTPPort: httpPort, HTTPSPort: httpsPort, Issuer: edge.IssuerInternal},
	}

	start := func() (context.CancelFunc, chan error) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, e.opts) }()
		return cancel, done
	}
	stop := func(cancel context.CancelFunc, done chan error) {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("Run did not stop")
		}
	}

	// 1. First start, apply, serve.
	cancel, done := start()
	waitHealthy(t, e.client)
	v, err := e.client.Apply(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got, err := e.client.Version(context.Background()); err != nil || got != v {
		t.Fatalf("Version = %q %v, want %q", got, err, v)
	}
	appURL := fmt.Sprintf("http://127.0.0.1:%d/", appPort)
	if got := mustGet(t, http.DefaultClient, appURL); got != body {
		t.Fatalf("app port body %q", got)
	}
	root := filepath.Join(data, "caddy", "pki", "authorities", "local", "root.crt")
	https := httpsClientTo(t, root, domain, fmt.Sprintf("127.0.0.1:%d", httpsPort))
	if got := waitGet(t, https, fmt.Sprintf("https://%s:%d/", domain, httpsPort), 30*time.Second); got != body {
		t.Fatalf("https body %q", got)
	}
	noFollow := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, n, fmt.Sprintf("127.0.0.1:%d", httpPort))
		}}}
	resp, err := noFollow.Get(fmt.Sprintf("http://%s:%d/x", domain, httpPort))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("http on the domain = %d, want 308", resp.StatusCode)
	}
	rep := Check(context.Background(), CheckOptions{StateDir: e.opts.StateDir, RuntimeDir: e.opts.RuntimeDir, Want: v})
	if !rep.OK || rep.Version != v || len(rep.Ports) != 3 {
		t.Fatalf("health check %+v", rep)
	}
	if h := e.client.Health(context.Background()); !h.OK() {
		t.Fatalf("Health %+v", h)
	}

	// 2. A request running when the stop signal comes completes.
	slow := make(chan string, 1)
	go func() {
		resp, err := http.Get(appURL + "slow")
		if err != nil {
			slow <- "error: " + err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		slow <- string(b)
	}()
	time.Sleep(500 * time.Millisecond)
	stop(cancel, done)
	if got := <-slow; got != body {
		t.Fatalf("request running at stop: %q", got)
	}
	if _, err := http.Get(appURL); err == nil {
		t.Fatal("app port still answers after stop")
	}

	// 3. Restart: the saved configuration serves again by itself.
	t0 := time.Now()
	cancel, done = start()
	got := waitGet(t, http.DefaultClient, appURL, 5*time.Second)
	t.Logf("routes back %.2f s after the restart (no Apply)", time.Since(t0).Seconds())
	if got != body {
		t.Fatalf("after restart %q", got)
	}
	if got, err := e.client.Version(context.Background()); err != nil || got != v {
		t.Fatalf("Version after restart = %q %v, want %q", got, err, v)
	}
	stop(cancel, done)
}

func freeTCPPorts(t *testing.T, n int) []int {
	t.Helper()
	var out []int
	var lns []net.Listener
	for range n {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		lns = append(lns, ln)
		out = append(out, ln.Addr().(*net.TCPAddr).Port)
	}
	for _, ln := range lns {
		ln.Close()
	}
	return out
}

func mustGet(t *testing.T, c *http.Client, url string) string {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func waitGet(t *testing.T, c *http.Client, url string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	var last error
	for time.Now().Before(deadline) {
		resp, err := c.Get(url)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return string(b)
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("GET %s never succeeded within %s: %v", url, within, last)
	return ""
}

// httpsClientTo dials fixedAddr and trusts the proxy's local CA root (it
// appears once the first internal certificate is issued).
func httpsClientTo(t *testing.T, rootPath, serverName, fixedAddr string) *http.Client {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var pem []byte
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(rootPath); err == nil && len(b) > 0 {
			pem = b
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatalf("no local CA root at %s", rootPath)
	}
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, n, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, n, fixedAddr)
		},
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: serverName},
	}}
}
