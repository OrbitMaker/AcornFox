package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/apiserver"
	"github.com/acornfox/acornfox/internal/publicconsole"
	"github.com/acornfox/acornfox/internal/state"
)

// wiringRunner and wiringKicker satisfy the API's required dependencies;
// the LAN console routes never call them.
type wiringRunner struct{ apiserver.Runner }
type wiringKicker struct{}

func (wiringKicker) Kick(string) {}

// The settings page (设置 → 访问) reads the LAN console through the browser
// console. Every entry must see it: the trusted socket, the loopback console
// (18800, reached over the SSH tunnel) and the LAN console itself (18802),
// which wraps the browser console. v0.5.0 copied the API config for the
// browser console before the LAN console was set, so both browser entries
// answered 404 "此版本不支持局域网控制台" while the CLI worked. The console's
// own tests use a mock API and could not see it.
func TestEveryConsoleEntrySeesTheLANConsole(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := state.Open(state.Config{Path: filepath.Join(dir, "acornfox.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	uploads := filepath.Join(dir, "uploads")
	if err := os.MkdirAll(uploads, 0o700); err != nil {
		t.Fatal(err)
	}
	lanStore := publicconsole.NewFileStore(filepath.Join(dir, "lan-console.json"))
	if err := lanStore.Save(&publicconsole.Config{EnabledAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	e := wireConsoles(apiserver.Config{
		Store: st, Kicker: wiringKicker{}, Runner: wiringRunner{}, UploadDir: uploads, DataDir: dir,
	}, nil, publicconsole.LANConfig{
		Store: lanStore, Sessions: st, Host: func() string { return "192.168.31.20" },
	})

	status := func(entry string, w *httptest.ResponseRecorder, wantSet bool) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: GET /v1/lan-console = %d %s", entry, w.Code, w.Body.String())
		}
		var s publicconsole.LANStatus
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatalf("%s: %v %s", entry, err, w.Body.String())
		}
		if !s.Enabled || s.Port != publicconsole.DefaultLANPort || s.PasswordSet != wantSet {
			t.Fatalf("%s: status = %+v", entry, s)
		}
	}

	// Trusted entry (the unix socket: acornfox lan-console status).
	w := httptest.NewRecorder()
	e.trusted.ServeHTTP(w, httptest.NewRequest("GET", "http://unix/v1/lan-console", nil))
	status("trusted", w, false)

	// Browser console on 18800, logged in with a session from acornfox open.
	tok, err := st.CreateConsoleToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, secret, _, err := st.RedeemConsoleToken(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/lan-console", "/v1/lan-console/devices"} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:18800"+path, nil)
		r.AddCookie(&http.Cookie{Name: "af_session", Value: secret})
		w := httptest.NewRecorder()
		e.console.ServeHTTP(w, r)
		if path == "/v1/lan-console" {
			status("console 18800", w, false)
		} else if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"devices"`) {
			t.Fatalf("console 18800: GET %s = %d %s", path, w.Code, w.Body.String())
		}
	}

	// LAN console on 18802: the first visitor from the local network sets the
	// password, which logs them in; then the same reads go through the LAN
	// gateway to the browser console.
	const host = "192.168.31.20:18802"
	cookies := map[string]string{}
	do := func(method, path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		var r *http.Request
		if form != nil {
			r = httptest.NewRequest(method, "http://"+host+path, strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", "http://"+host)
		} else {
			r = httptest.NewRequest(method, "http://"+host+path, nil)
		}
		r.Host = host
		r.RemoteAddr = "192.168.31.50:5000"
		for k, v := range cookies {
			r.AddCookie(&http.Cookie{Name: k, Value: v})
		}
		w := httptest.NewRecorder()
		e.lan.Handler().ServeHTTP(w, r)
		for _, c := range w.Result().Cookies() {
			cookies[c.Name] = c.Value
		}
		return w
	}
	pw := "lan-console-test-1"
	if w := do("POST", "/setup", url.Values{"password": {pw}, "confirm": {pw}}); w.Code != http.StatusSeeOther {
		t.Fatalf("LAN 18802: setting the password = %d %s", w.Code, w.Body.String())
	}
	status("LAN 18802", do("GET", "/v1/lan-console", nil), true)
	if w := do("GET", "/v1/lan-console/devices", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"devices"`) {
		t.Fatalf("LAN 18802: GET /v1/lan-console/devices = %d %s", w.Code, w.Body.String())
	}
	// A browser still may not open or close the LAN console (trusted only).
	if w := do("DELETE", "/v1/lan-console", url.Values{}); w.Code == http.StatusOK {
		t.Fatalf("LAN 18802: DELETE /v1/lan-console must stay closed to a browser, got %d", w.Code)
	}
	if cfg, _ := lanStore.Load(); cfg == nil {
		t.Fatal("the LAN console must still be open")
	}
}

// Before wireConsoles points it at the console, the forwarder answers 503
// instead of panicking.
func TestConsoleForwarderBeforeSet(t *testing.T) {
	var f consoleForwarder
	w := httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", w.Code)
	}
	f.set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	w = httptest.NewRecorder()
	f.ServeHTTP(w, httptest.NewRequest("GET", "http://127.0.0.1/", nil))
	if w.Code != http.StatusTeapot {
		t.Fatalf("got %d", w.Code)
	}
}
