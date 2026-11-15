package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

func internalApp(name string) client.App {
	return client.App{Name: name, PublicPort: 18820, Port: 5000, Exposure: "internal", ObservedState: "running",
		InternalOnly: true, InternalNote: "只在内部网络，给别的应用用"}
}

// acornfox open of an app reached over the app network only says why it
// does not open (OPEN-APP-03), whatever the target: no tunnel, no browser.
func TestOpenInternalApp(t *testing.T) {
	for _, url := range []string{"unix:/run/acornfox/acornfox.sock", "http://192.168.1.20:18802"} {
		h := newHarness(t)
		h.addTarget("t", url)
		h.api.appFn = func(ctx context.Context, app string) (client.App, error) { return internalApp(app), nil }
		cfg := func(a *app) {
			a.openURL = func(u string) error { t.Fatalf("opened %s", u); return nil }
			a.openAppTunnel = func(ctx context.Context, tg client.Target, l, r int) (*client.Tunnel, error) {
				t.Fatal("tunnel")
				return nil, nil
			}
		}
		code, _, errOut := h.runOpen(context.Background(), cfg, "open", "db")
		if code != exitOpFailed || !strings.Contains(errOut, "OPEN-APP-03") || !strings.Contains(errOut, "只在内部网络") || !strings.Contains(errOut, "没有网页") {
			t.Fatalf("%s: exit %d err %s", url, code, errOut)
		}
		code, out, _ := h.runOpen(context.Background(), cfg, "--json", "open", "db")
		m := decodeJSON(t, out)
		d, _ := m["diagnosis"].(map[string]any)
		if code != exitOpFailed || m["ok"] != false || d["code"] != "OPEN-APP-03" || d["hint"] == "" {
			t.Fatalf("%s: json %v", url, m)
		}
	}
}

// status and apps say it is for other apps and give no address.
func TestStatusAndAppsOfInternalApp(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) { return internalApp(app), nil }
	h.api.appsFn = func(ctx context.Context) ([]client.App, error) {
		return []client.App{internalApp("db"), {Name: "web", URL: "http://203.0.113.5:18811", ObservedState: "running", Exposure: "public"}}, nil
	}
	code, out, _ := h.run("--app", "db", "status")
	if code != exitOK || !strings.Contains(out, "只在内部网络，给别的应用用") || !strings.Contains(out, "db:5000") || strings.Contains(out, "网址") || strings.Contains(out, "暴露方式") {
		t.Fatalf("status: %d %s", code, out)
	}
	code, out, _ = h.run("--json", "--app", "db", "status")
	m := decodeJSON(t, out)
	if code != exitOK || m["internal_only"] != true || m["internal_note"] != "只在内部网络，给别的应用用" || m["url"] != "" || m["open_hint"] != nil {
		t.Fatalf("status json: %v", m)
	}
	code, out, _ = h.run("apps")
	if code != exitOK || !strings.Contains(out, "db\trunning\t只在内部网络，给别的应用用") || !strings.Contains(out, "web\trunning\thttp://203.0.113.5:18811") {
		t.Fatalf("apps: %s", out)
	}
	code, out, _ = h.run("--json", "apps")
	m = decodeJSON(t, out)
	list, _ := m["apps"].([]any)
	if code != exitOK || len(list) != 2 {
		t.Fatalf("apps json: %v", m)
	}
	if db := list[0].(map[string]any); db["internal_only"] != true || db["url"] != "" {
		t.Fatalf("apps json db: %v", db)
	}
	if web := list[1].(map[string]any); web["internal_only"] != nil {
		t.Fatalf("apps json web: %v", web)
	}
}
