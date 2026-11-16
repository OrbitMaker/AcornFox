package cli

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// sshTarget makes "dev" an SSH target and the default.
func (h *harness) sshTarget() {
	h.t.Helper()
	tf, _ := loadTargets(h.configDir)
	tf.Targets["dev"] = &client.Target{Name: "dev", SSH: "u@h"}
	tf.Default = "dev"
	if err := saveTargets(h.configDir, tf); err != nil {
		h.t.Fatal(err)
	}
}

// fakeDetach is a detached-tunnel world: started tunnels and which are up.
type fakeDetach struct {
	started []int // local ports
	up      map[int]int
	stopped []int
}

func (f *fakeDetach) install(a *app) {
	if f.up == nil {
		f.up = map[int]int{}
	}
	a.freePort = func() (int, error) { return 45678 + len(f.started), nil }
	a.openDetached = func(ctx context.Context, tg client.Target, p int) (*client.DetachedTunnel, error) {
		f.started = append(f.started, p)
		pid := 9000 + len(f.started)
		f.up[pid] = p
		return &client.DetachedTunnel{LocalURL: "http://127.0.0.1:" + strconv.Itoa(p), PID: pid}, nil
	}
	a.tunnelAlive = func(pid, port int) bool { return f.up[pid] == port && port != 0 }
	a.stopPID = func(pid int) error { f.stopped = append(f.stopped, pid); delete(f.up, pid); return nil }
}

// --detach on an SSH target: the tunnel goes to the background, the browser
// opens the one-time login link through it, and the command returns at once
// (no ctx cancel needed) with what to stop it with; the token is not printed.
func TestOpenDetach(t *testing.T) {
	h := newHarness(t)
	h.sshTarget()
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "det-token-1", nil }
	f := &fakeDetach{}
	var opened string
	code, out, _ := h.runOpen(context.Background(), func(a *app) {
		f.install(a)
		a.openURL = func(u string) error { opened = u; return nil }
		a.openTunnel = func(ctx context.Context, tg client.Target, p int) (*client.Tunnel, error) {
			t.Fatal("a foreground tunnel with --detach")
			return nil, nil
		}
	}, "--json", "open", "--detach")
	if code != exitOK {
		t.Fatalf("exit %d %s", code, out)
	}
	if !strings.Contains(opened, "http://127.0.0.1:45678/console/login?t=det-token-1") {
		t.Fatalf("opened %q", opened)
	}
	m := decodeJSON(t, out)
	if m["url"] != "http://127.0.0.1:45678" || m["pid"] != float64(9001) || m["reused"] != false || m["browser_opened"] != true ||
		m["stop_command"] != "acornfox open --stop" {
		t.Fatalf("json %v", m)
	}
	if _, ok := m["login_url"]; ok || strings.Contains(out, "det-token-1") {
		t.Fatalf("token printed although the browser opened: %s", out)
	}
	recs := loadOpenTunnels(h.configDir)
	if r := recs.Tunnels["dev"]; r.PID != 9001 || r.Port != 45678 || r.Started == "" {
		t.Fatalf("record %+v", recs)
	}

	// Again while it is up: the same tunnel, a new login link.
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "det-token-2", nil }
	code, out, _ = h.runOpen(context.Background(), func(a *app) {
		f.install(a)
		a.openURL = func(u string) error { opened = u; return nil }
	}, "--json", "open", "--detach")
	m = decodeJSON(t, out)
	if code != exitOK || m["reused"] != true || m["pid"] != float64(9001) || len(f.started) != 1 ||
		!strings.Contains(opened, "45678/console/login?t=det-token-2") {
		t.Fatalf("reuse: %d %v started=%v opened=%q", code, m, f.started, opened)
	}

	// --stop ends it and drops the record; again: nothing up, not an error.
	code, out, _ = h.runOpen(context.Background(), f.install, "--json", "open", "--stop")
	m = decodeJSON(t, out)
	if code != exitOK || len(f.stopped) != 1 || f.stopped[0] != 9001 {
		t.Fatalf("stop: %d %v %v", code, m, f.stopped)
	}
	if s, _ := m["stopped"].([]any); len(s) != 1 {
		t.Fatalf("stopped %v", m)
	}
	if _, ok := loadOpenTunnels(h.configDir).Tunnels["dev"]; ok {
		t.Fatal("record kept after --stop")
	}
	code, out, _ = h.runOpen(context.Background(), f.install, "--json", "open", "--stop")
	m = decodeJSON(t, out)
	if s, _ := m["stopped"].([]any); code != exitOK || len(s) != 0 || len(f.stopped) != 1 {
		t.Fatalf("second stop: %d %v %v", code, m, f.stopped)
	}
}

// A recorded tunnel that is gone (or a pid now used by something else, not
// forwarding the port) is neither reused nor killed: a new one starts.
func TestOpenDetachStaleRecord(t *testing.T) {
	h := newHarness(t)
	h.sshTarget()
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "tok", nil }
	if err := saveOpenTunnels(h.configDir, openTunnelsFile{Tunnels: map[string]detachedRecord{"dev": {PID: 4242, Port: 40000}}}); err != nil {
		t.Fatal(err)
	}
	f := &fakeDetach{}
	code, out, _ := h.runOpen(context.Background(), func(a *app) {
		f.install(a)
		a.openURL = func(string) error { return nil }
	}, "--json", "open", "--detach")
	m := decodeJSON(t, out)
	if code != exitOK || m["reused"] != false || len(f.started) != 1 || len(f.stopped) != 0 {
		t.Fatalf("%d %v started=%v stopped=%v", code, m, f.started, f.stopped)
	}
	code, _, _ = h.runOpen(context.Background(), func(a *app) {
		f.install(a)
		f.up = map[int]int{} // the tunnel died meanwhile
	}, "open", "--stop")
	if code != exitOK || len(f.stopped) != 0 {
		t.Fatalf("stop of a dead tunnel killed %v", f.stopped)
	}
}

// When the browser cannot open, --detach gives the one-time login link so
// the AI can hand it to the user.
func TestOpenDetachNoBrowser(t *testing.T) {
	h := newHarness(t)
	h.sshTarget()
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "link-token", nil }
	f := &fakeDetach{}
	code, out, _ := h.runOpen(context.Background(), f.install, "--json", "open", "--detach", "--no-browser")
	m := decodeJSON(t, out)
	if code != exitOK || m["browser_opened"] != false || !strings.Contains(m["login_url"].(string), "45678/console/login?t=link-token") {
		t.Fatalf("%d %v", code, m)
	}
}

// A URL target needs no tunnel: --detach opens the browser and returns
// without waiting for Ctrl+C.
func TestOpenDetachURLTarget(t *testing.T) {
	h := newHarness(t)
	h.addTarget("lan", "http://192.168.1.20:18802")
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "tok", nil }
	code, out, _ := h.runOpen(context.Background(), func(a *app) {
		a.openURL = func(string) error { return nil }
		a.openDetached = func(context.Context, client.Target, int) (*client.DetachedTunnel, error) {
			t.Fatal("a tunnel for a URL target")
			return nil, nil
		}
	}, "--json", "open", "--detach")
	m := decodeJSON(t, out)
	if code != exitOK || m["url"] != "http://192.168.1.20:18802" {
		t.Fatalf("%d %v", code, m)
	}
	if _, ok := m["pid"]; ok {
		t.Fatalf("pid without a tunnel: %v", m)
	}
}

// --detach and --stop are for the console only; a tunnel error comes back
// as the usual connect diagnosis.
func TestOpenDetachRefusals(t *testing.T) {
	h := newHarness(t)
	h.sshTarget()
	if code, out, _ := h.runOpen(context.Background(), nil, "--json", "open", "web", "--detach"); code != exitUsage || !strings.Contains(out, "只用于打开控制台") {
		t.Fatalf("app with --detach: %d %s", code, out)
	}
	if code, _, _ := h.runOpen(context.Background(), nil, "open", "--stop", "--detach"); code != exitUsage {
		t.Fatalf("--stop --detach: %d", code)
	}
	h.api.consoleTokenFn = func(ctx context.Context) (string, error) { return "tok", nil }
	code, out, _ := h.runOpen(context.Background(), func(a *app) {
		a.freePort = func() (int, error) { return 45678, nil }
		a.tunnelAlive = func(int, int) bool { return false }
		a.openDetached = func(context.Context, client.Target, int) (*client.DetachedTunnel, error) {
			return nil, &client.Error{Diag: client.Diagnosis{Stage: "connect", Code: "host_unreachable", Message: "连不上"}}
		}
	}, "--json", "open", "--detach")
	if code != exitConnect || !strings.Contains(out, "host_unreachable") {
		t.Fatalf("tunnel error: %d %s", code, out)
	}
	if _, ok := loadOpenTunnels(h.configDir).Tunnels["dev"]; ok {
		t.Fatal("a failed tunnel was recorded")
	}
}
