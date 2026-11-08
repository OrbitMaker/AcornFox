package cli

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// runOpen invokes `open` with injected tunnel/opener/port hooks. It returns the
// exit code, stdout and stderr. ctx controls Ctrl+C simulation.
func (h *harness) runOpen(ctx context.Context, cfg func(a *app), args ...string) (int, string, string) {
	h.t.Helper()
	var out, errBuf bytes.Buffer
	a := &app{
		out:       &outputWriter{stdout: &out, stderr: &errBuf},
		stdin:     io.NopCloser(bytes.NewReader(nil)),
		connect:   h.connect,
		getenv:    func(string) string { return "" },
		configDir: h.configDir,
		workDir:   h.workDir,
	}
	if cfg != nil {
		cfg(a)
	}
	rest, err := a.parseGlobal(args)
	if err != nil {
		return exitUsage, out.String(), errBuf.String()
	}
	// rest[0] == "open"
	code := a.cmdOpen(ctx, rest[1:])
	return code, out.String(), errBuf.String()
}

// TestOpenURLModeNoTunnel: a target with URL uses its host:port directly, opens
// the browser and blocks until ctx is cancelled (Ctrl+C) → exit 0.
func TestOpenURLModeNoTunnel(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://127.0.0.1:18800")

	var openedURL string
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "tok-secret-123", nil }

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	code, out, _ := h.runOpen(ctx, func(a *app) {
		a.openURL = func(u string) error { openedURL = u; return nil }
		// A tunnel function that must NOT be called in url mode.
		a.openTunnel = func(ctx context.Context, tg client.Target, p int) (*client.Tunnel, error) {
			t.Fatal("tunnel should not be used in url mode")
			return nil, nil
		}
	}, "open")

	if code != exitOK {
		t.Fatalf("exit = %d want 0", code)
	}
	if !strings.Contains(openedURL, "/console/login?t=tok-secret-123") {
		t.Fatalf("login URL = %q", openedURL)
	}
	// Token must not leak into human output when the browser opened.
	if strings.Contains(out, "tok-secret-123") {
		t.Fatalf("token leaked in output: %s", out)
	}
	if !strings.Contains(out, "http://127.0.0.1:18800") {
		t.Fatalf("base URL missing from output: %s", out)
	}
}

// TestOpenJSONShape: --json prints {"ok":true,"url":...} without the token.
func TestOpenJSONShape(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://127.0.0.1:18800")
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "json-token-xyz", nil }

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	code, out, _ := h.runOpen(ctx, func(a *app) {
		a.openURL = func(u string) error { return nil }
	}, "--json", "open")

	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	m := decodeJSON(t, out)
	if m["ok"] != true {
		t.Fatalf("ok != true: %v", m)
	}
	if m["url"] != "http://127.0.0.1:18800" {
		t.Fatalf("url = %v", m["url"])
	}
	if strings.Contains(out, "json-token-xyz") {
		t.Fatalf("token leaked in json: %s", out)
	}
}

// TestOpenNoBrowserPrintsLink: --no-browser prints the login URL (with token)
// and a note.
func TestOpenNoBrowserPrintsLink(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://127.0.0.1:18800")
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "nb-token-999", nil }

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	opened := false
	code, out, _ := h.runOpen(ctx, func(a *app) {
		a.openURL = func(u string) error { opened = true; return nil }
	}, "open", "--no-browser")

	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if opened {
		t.Fatalf("browser should not open with --no-browser")
	}
	if !strings.Contains(out, "nb-token-999") {
		t.Fatalf("login link with token should be printed: %s", out)
	}
	if !strings.Contains(out, "一次性") {
		t.Fatalf("single-use note missing: %s", out)
	}
}

// TestOpenBrowserFailurePrintsLink: when the opener fails, the login link is
// printed instead.
func TestOpenBrowserFailurePrintsLink(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://127.0.0.1:18800")
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "fail-token-777", nil }

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	code, out, _ := h.runOpen(ctx, func(a *app) {
		a.openURL = func(u string) error { return io.ErrUnexpectedEOF }
	}, "open")

	if code != exitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "fail-token-777") {
		t.Fatalf("fallback link missing: %s", out)
	}
}

// TestOpenTunnelModeSuccess: ssh-mode target uses the injected tunnel; ctx
// cancel ends with exit 0.
func TestOpenTunnelModeSuccess(t *testing.T) {
	h := newHarness(t)
	// SSH-mode target (no URL).
	tf, _ := loadTargets(h.configDir)
	tf.Targets["dev"] = &client.Target{Name: "dev", SSH: "u@h"}
	tf.Default = "dev"
	if err := saveTargets(h.configDir, tf); err != nil {
		t.Fatal(err)
	}

	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "ssh-token-abc", nil }

	var openedURL string
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	var gotPort int
	code, out, _ := h.runOpen(ctx, func(a *app) {
		a.freePort = func() (int, error) { return 45678, nil }
		a.openURL = func(u string) error { openedURL = u; return nil }
		a.openTunnel = func(ctx context.Context, tg client.Target, p int) (*client.Tunnel, error) {
			gotPort = p
			return client.NewLiveTunnelForTest("http://127.0.0.1:45678"), nil
		}
	}, "open")

	if code != exitOK {
		t.Fatalf("exit = %d want 0 out=%s", code, out)
	}
	if gotPort != 45678 {
		t.Fatalf("tunnel port = %d want 45678", gotPort)
	}
	if !strings.Contains(openedURL, "http://127.0.0.1:45678/console/login?t=ssh-token-abc") {
		t.Fatalf("login URL = %q", openedURL)
	}
	if strings.Contains(out, "ssh-token-abc") {
		t.Fatalf("token leaked in human output: %s", out)
	}
}

// TestOpenTunnelDiesConnectExit3: when the tunnel dies with a connect error,
// exit code is 3.
func TestOpenTunnelDiesConnectExit3(t *testing.T) {
	h := newHarness(t)
	tf, _ := loadTargets(h.configDir)
	tf.Targets["dev"] = &client.Target{Name: "dev", SSH: "u@h"}
	tf.Default = "dev"
	if err := saveTargets(h.configDir, tf); err != nil {
		t.Fatal(err)
	}
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "t", nil }

	// A tunnel whose Done is already closed with a connect error.
	tun := client.NewClosedTunnel("http://127.0.0.1:45999", &client.Error{
		Diag: client.Diagnosis{Stage: "connect", Code: "forwarding_disabled", Message: "转发被禁止"},
	})

	code, _, errOut := h.runOpen(context.Background(), func(a *app) {
		a.freePort = func() (int, error) { return 45999, nil }
		a.openURL = func(u string) error { return nil }
		a.openTunnel = func(ctx context.Context, tg client.Target, p int) (*client.Tunnel, error) {
			return tun, nil
		}
	}, "open")

	if code != exitConnect {
		t.Fatalf("exit = %d want %d (stderr=%s)", code, exitConnect, errOut)
	}
	if !strings.Contains(errOut, "转发被禁止") {
		t.Fatalf("diagnosis missing: %s", errOut)
	}
}

// TestOpenTokenErrorPropagates: a failure fetching the token surfaces as the
// client error's exit code.
func TestOpenTokenErrorExit3(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://127.0.0.1:18800")
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) {
		return "", &client.Error{Diag: client.Diagnosis{Stage: "connect", Code: "server_down", Message: "未运行"}}
	}
	code, _, errOut := h.runOpen(context.Background(), func(a *app) {
		a.openURL = func(u string) error { return nil }
	}, "open")
	if code != exitConnect {
		t.Fatalf("exit = %d want %d", code, exitConnect)
	}
	if !strings.Contains(errOut, "未运行") {
		t.Fatalf("error not surfaced: %s", errOut)
	}
}

func sshTarget(t *testing.T, h *harness) {
	t.Helper()
	tf, _ := loadTargets(h.configDir)
	tf.Targets["dev"] = &client.Target{Name: "dev", SSH: "u@h"}
	tf.Default = "dev"
	if err := saveTargets(h.configDir, tf); err != nil {
		t.Fatal(err)
	}
}

// open APP on an ssh target tunnels a free local port to the app's port on
// the server and opens the browser there; Ctrl+C ends it with exit 0.
func TestOpenAppTunnel(t *testing.T) {
	h := newHarness(t)
	sshTarget(t, h)
	var asked string
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) {
		asked = app
		return client.App{Name: app, PublicPort: 18812, Exposure: "local", LocalOnly: true, URL: "http://127.0.0.1:18812"}, nil
	}
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) {
		t.Fatal("open APP must not mint a console token")
		return "", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	var gotLocal, gotRemote int
	var opened string
	code, out, errOut := h.runOpen(ctx, func(a *app) {
		a.freePort = func() (int, error) { return 45001, nil }
		a.openURL = func(u string) error { opened = u; return nil }
		a.openAppTunnel = func(ctx context.Context, tg client.Target, l, r int) (*client.Tunnel, error) {
			gotLocal, gotRemote = l, r
			return client.NewLiveTunnelForTest("http://127.0.0.1:45001"), nil
		}
		a.openTunnel = func(ctx context.Context, tg client.Target, p int) (*client.Tunnel, error) {
			t.Fatal("console tunnel used for an app")
			return nil, nil
		}
	}, "open", "new-api")
	if code != exitOK {
		t.Fatalf("exit %d out=%s err=%s", code, out, errOut)
	}
	if asked != "new-api" || gotLocal != 45001 || gotRemote != 18812 || opened != "http://127.0.0.1:45001" {
		t.Fatalf("asked=%q tunnel %d->%d opened=%q", asked, gotLocal, gotRemote, opened)
	}
	if !strings.Contains(out, "Ctrl+C") || !strings.Contains(out, "new-api") {
		t.Fatalf("output: %s", out)
	}
}

// --port picks the local end; --json reports the address.
func TestOpenAppPortJSON(t *testing.T) {
	h := newHarness(t)
	sshTarget(t, h)
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) {
		return client.App{Name: app, PublicPort: 18810, Exposure: "lan"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	var gotLocal int
	code, out, _ := h.runOpen(ctx, func(a *app) {
		a.openURL = func(u string) error { t.Fatal("--no-browser opened a browser"); return nil }
		a.openAppTunnel = func(ctx context.Context, tg client.Target, l, r int) (*client.Tunnel, error) {
			gotLocal = l
			return client.NewLiveTunnelForTest("http://127.0.0.1:" + itoaTest(l)), nil
		}
	}, "--json", "open", "web", "--port", "9119", "--no-browser")
	if code != exitOK || gotLocal != 9119 {
		t.Fatalf("exit %d local %d out %s", code, gotLocal, out)
	}
	m := decodeJSON(t, out)
	if m["url"] != "http://127.0.0.1:9119" || m["tunnel"] != true || m["app"] != "web" {
		t.Fatalf("json %v", m)
	}
}

// A target added by URL cannot tunnel: a local-only app is OPEN-APP-02; an
// app on the LAN opens at the host the target already reaches.
func TestOpenAppURLTarget(t *testing.T) {
	h := newHarness(t)
	h.addTarget("lan", "http://192.168.1.20:18802")
	exposure := "local"
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) {
		return client.App{Name: app, PublicPort: 18811, Exposure: exposure, LocalOnly: exposure == "local", URL: "http://127.0.0.1:18811"}, nil
	}
	code, out, _ := h.runOpen(context.Background(), func(a *app) {
		a.openURL = func(u string) error { t.Fatal("opened"); return nil }
	}, "--json", "open", "web")
	if code != exitOpFailed {
		t.Fatalf("exit %d", code)
	}
	m := decodeJSON(t, out)
	d, _ := m["diagnosis"].(map[string]any)
	if m["ok"] != false || d["code"] != "OPEN-APP-02" || !strings.Contains(d["hint"].(string), "--ssh") {
		t.Fatalf("json %v", m)
	}

	exposure = "lan"
	var opened string
	code, out, _ = h.runOpen(context.Background(), func(a *app) {
		a.openURL = func(u string) error { opened = u; return nil }
		a.openAppTunnel = func(ctx context.Context, tg client.Target, l, r int) (*client.Tunnel, error) {
			t.Fatal("tunnel on a URL target")
			return nil, nil
		}
	}, "open", "web")
	if code != exitOK || opened != "http://192.168.1.20:18811" {
		t.Fatalf("exit %d opened %q out %s", code, opened, out)
	}
}

// This computer's acornfox opens the loopback address directly; an app
// without a port is OPEN-APP-01; two names are a usage error.
func TestOpenAppLocalAndErrors(t *testing.T) {
	h := newHarness(t)
	h.addTarget("me", "unix:/run/acornfox/acornfox.sock")
	port := 18815
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) {
		return client.App{Name: app, PublicPort: port, Exposure: "local", LocalOnly: true}, nil
	}
	var opened string
	code, _, _ := h.runOpen(context.Background(), func(a *app) {
		a.openURL = func(u string) error { opened = u; return nil }
	}, "open", "web")
	if code != exitOK || opened != "http://127.0.0.1:18815" {
		t.Fatalf("exit %d opened %q", code, opened)
	}
	port = 0
	code, _, errOut := h.runOpen(context.Background(), nil, "open", "web")
	if code != exitOpFailed || !strings.Contains(errOut, "OPEN-APP-01") || !strings.Contains(errOut, "acornfox status web") {
		t.Fatalf("exit %d err %s", code, errOut)
	}
	if code, _, _ := h.runOpen(context.Background(), nil, "open", "a", "b"); code != exitUsage {
		t.Fatalf("two names: exit %d", code)
	}
}

func itoaTest(n int) string { return strconv.Itoa(n) }
