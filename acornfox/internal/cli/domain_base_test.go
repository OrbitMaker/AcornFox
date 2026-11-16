package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/apiserver"
	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/platformdomain"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// pdRunner answers the app views' container lookups: no containers.
type pdRunner struct{ apiserver.Runner }

func (pdRunner) ListContainers(context.Context, string) ([]runner.ContainerInfo, error) {
	return nil, errors.New("no runner in this test")
}

// pdDNS answers every name with ips (nil: not found).
type pdDNS struct{ ips []string }

func (d pdDNS) LookupIPs(_ context.Context, _, host string) ([]string, error) {
	if d.ips == nil {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return d.ips, nil
}

// newPDHarness runs `acornfox domain base` against the real API server and
// client, with the checks' network replaced.
func newPDHarness(t *testing.T, publicHost string, dns pdDNS) (*harness, *state.Store) {
	t.Helper()
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := platformdomain.New(platformdomain.Config{
		Path:        filepath.Join(dir, platformdomain.FileName),
		Machine:     platformdomain.Machine{PublicHost: publicHost},
		Resolver:    dns,
		Dial:        func(context.Context, string) error { return nil },
		CertProbe:   func(context.Context, string) error { return nil },
		ActiveHosts: func(context.Context) []string { return []string{"hello.apps.example.com"} },
	})
	ts := httptest.NewServer(apiserver.New(apiserver.Config{Store: db, Kicker: noKicker{}, Runner: pdRunner{}, UploadDir: dir,
		PublicHost: publicHost, PlatformDomain: svc}))
	t.Cleanup(ts.Close)
	h := newHarness(t)
	h.addTarget("dev", ts.URL)
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) { return client.Connect(ctx, tgt) }
	return h, db
}

func TestDomainBaseCommands(t *testing.T) {
	h, db := newPDHarness(t, "203.0.113.10", pdDNS{ips: []string{"203.0.113.10"}})
	ctx := context.Background()
	db.EnsureApp(ctx, "hello")
	db.UpdateApp(ctx, "hello", func(a *state.App) error { a.Exposure = state.ExposurePublic; return nil })

	// Nothing set: says how to start.
	code, out, _ := h.run("domain", "base")
	if code != exitOK || !strings.Contains(out, "还没有设平台域名") || !strings.Contains(out, "指向 203.0.113.10") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("--json", "domain", "base")
	if m := decodeJSON(t, out); code != exitOK || m["ok"] != true || m["state"] != "unset" || m["supported"] != true {
		t.Fatalf("%d %s", code, out)
	}

	// A plan changes nothing.
	code, out, _ = h.run("domain", "base", "set", "apps.example.com", "--plan")
	if code != exitOK || !strings.Contains(out, "什么都没改") || !strings.Contains(out, "https://hello.apps.example.com") || !strings.Contains(out, "ICP 备案") {
		t.Fatalf("%d %s", code, out)
	}
	if _, out, _ = h.run("--json", "domain", "base"); decodeJSON(t, out)["domain"] != "" {
		t.Fatalf("plan wrote: %s", out)
	}

	// Set.
	code, out, _ = h.run("domain", "base", "set", "apps.example.com")
	if code != exitOK || !strings.Contains(out, "平台域名已设为 apps.example.com") || !strings.Contains(out, "ICP 备案") || !strings.Contains(out, "✓ 域名指过来了") || !strings.Contains(out, "hello：hello.apps.example.com") {
		t.Fatalf("%d %s", code, out)
	}
	// The app's address follows.
	code, out, _ = h.run("--json", "apps")
	if code != exitOK || !strings.Contains(out, "https://hello.apps.example.com") {
		t.Fatalf("%d %s", code, out)
	}

	// Replacing without --yes is refused with PDOMAIN-07 and says how.
	code, out, _ = h.run("--json", "domain", "base", "set", "web.example.org")
	if code == exitOK || !strings.Contains(out, "PDOMAIN-07") || !strings.Contains(out, "--yes") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("domain", "base", "set", "web.example.org", "--plan")
	if code != exitOK || !strings.Contains(out, "要用户同意") || !strings.Contains(out, "https://hello.web.example.org") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("--json", "domain", "base", "set", "web.example.org", "--yes")
	if m := decodeJSON(t, out); code != exitOK || m["domain"] != "web.example.org" {
		t.Fatalf("%d %s", code, out)
	}

	// Clearing needs --yes too.
	code, out, _ = h.run("--json", "domain", "base", "clear")
	if code == exitOK || !strings.Contains(out, "PDOMAIN-07") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("domain", "base", "clear", "--yes")
	if code != exitOK || !strings.Contains(out, "已清掉") {
		t.Fatalf("%d %s", code, out)
	}

	// Usage.
	if code, _, _ := h.run("domain", "base", "set"); code != exitUsage {
		t.Fatalf("set without a domain: %d", code)
	}
	if code, _, _ := h.run("domain", "base", "clear", "x.example.com"); code != exitUsage {
		t.Fatalf("clear with a domain: %d", code)
	}
	if code, _, _ := h.run("domain", "base", "nope"); code != exitUsage {
		t.Fatalf("unknown: %d", code)
	}
	if code, out, _ := h.run("domain", "base", "--help"); code != exitOK || !strings.Contains(out, "平台域名") || !strings.Contains(out, "例子") {
		t.Fatalf("%d %s", code, out)
	}
	// Not a domain name: PDOMAIN-05.
	code, out, _ = h.run("--json", "domain", "base", "set", "https://apps.example.com")
	if code == exitOK || !strings.Contains(out, "PDOMAIN-05") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestDomainBaseProblemsAndLAN(t *testing.T) {
	h, _ := newPDHarness(t, "203.0.113.10", pdDNS{ips: []string{"198.51.100.7"}})
	code, out, _ := h.run("domain", "base", "set", "apps.example.com")
	if code != exitOK || !strings.Contains(out, "问题（PDOMAIN-01）") || !strings.Contains(out, "现在指向 198.51.100.7，应该指向 203.0.113.10") || !strings.Contains(out, "acornfox faults PDOMAIN-01") {
		t.Fatalf("%d %s", code, out)
	}

	h, _ = newPDHarness(t, "192.168.1.20", pdDNS{})
	code, out, _ = h.run("domain", "base")
	if code != exitOK || !strings.Contains(out, "这台机器在局域网里") || !strings.Contains(out, "PDOMAIN-02") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("--json", "domain", "base", "set", "apps.example.com")
	if code == exitOK || !strings.Contains(out, "PDOMAIN-02") {
		t.Fatalf("%d %s", code, out)
	}
}

// An older server has no platform domain routes: server_too_old.
func TestDomainBaseOldServer(t *testing.T) {
	h := newHarness(t)
	ts := httptest.NewServer(http.NotFoundHandler()) // plain 404 for everything
	t.Cleanup(ts.Close)
	h.addTarget("dev", ts.URL)
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) { return client.Connect(ctx, tgt) }
	code, out, _ := h.run("--json", "domain", "base")
	if code == exitOK || !strings.Contains(out, client.CodeServerTooOld) {
		t.Fatalf("%d %s", code, out)
	}
}
