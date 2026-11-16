package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/publicconsole"
)

// Passwords used below; none may show up in a log line or an error.
const (
	pageLANPassword = "lan-secret-123"
	pageWrongGuess  = "wrong-guess-456"
	pageNewPassword = "NEW-PASS-once-789"
)

type pageFixture struct {
	t       *testing.T
	pc      *fakePublicConsole
	console http.Handler
	trusted http.Handler
	cookie  *http.Cookie
	csrf    string
	logs    *bytes.Buffer
	entry   *publicconsole.Entry // nil: logged in with acornfox open
}

func newPageFixture(t *testing.T, edit func(*Config)) *pageFixture {
	t.Helper()
	cfg, _, _ := newTestConfig(t)
	f := &pageFixture{t: t, pc: &fakePublicConsole{password: pageNewPassword}, logs: &bytes.Buffer{}}
	cfg.PublicConsole = f.pc
	cfg.Logger = slog.New(slog.NewTextHandler(f.logs, nil))
	if edit != nil {
		edit(&cfg)
	}
	f.trusted, f.console = New(cfg), NewConsole(cfg)
	f.cookie, f.csrf = login(t, f.trusted, f.console)
	return f
}

// lanEntry makes the requests look as if they came through the LAN gateway
// with the given password check.
func (f *pageFixture) lanEntry(kind string, check func(client, password string) (bool, time.Duration)) {
	f.entry = publicconsole.NewEntry(kind, "192.168.31.50", check)
}

func (f *pageFixture) call(method, path, body string) *httptest.ResponseRecorder {
	f.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "127.0.0.1:12345"
	req.AddCookie(f.cookie)
	req.Header.Set(csrfHeader, f.csrf)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if f.entry != nil {
		req = req.WithContext(publicconsole.WithEntry(req.Context(), f.entry))
	}
	rec := httptest.NewRecorder()
	f.console.ServeHTTP(rec, req)
	return rec
}

func (f *pageFixture) view() publicConsoleView {
	f.t.Helper()
	rec := f.call("GET", "/v1/public-console", "")
	if rec.Code != 200 {
		f.t.Fatalf("GET = %d %s", rec.Code, rec.Body.String())
	}
	var v publicConsoleView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		f.t.Fatal(err)
	}
	return v
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) (code, msg, hint string) {
	t.Helper()
	var b pubconErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("not an error body: %s", rec.Body.String())
	}
	return b.Error.Code, b.Error.Message, b.Error.Hint
}

func (f *pageFixture) noSecretsLeaked() {
	f.t.Helper()
	for _, s := range []string{pageLANPassword, pageWrongGuess, pageNewPassword} {
		if strings.Contains(f.logs.String(), s) {
			f.t.Fatalf("a password reached the log:\n%s", f.logs.String())
		}
	}
}

// Logged in with acornfox open over SSH: no password to type; open with the
// suggested console.<platform domain>, the new password comes back once,
// close without a body.
func TestPublicConsolePageTunnelLogin(t *testing.T) {
	f := newPageFixture(t, func(c *Config) {
		c.PublicConsoleBase = func(context.Context) string { return "apps.example.com" }
	})
	v := f.view()
	if v.Entry != "tunnel" || !v.CanToggle || v.NeedsPassword || v.SuggestedDomain != "console.apps.example.com" || v.Unsupported != nil || v.Enabled {
		t.Fatalf("view = %+v", v)
	}

	rec := f.call("POST", "/v1/public-console/enable", `{"domain":"console.apps.example.com"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"password":"`+pageNewPassword+`"`) {
		t.Fatalf("enable = %d %s", rec.Code, rec.Body.String())
	}
	if !f.pc.enabled || f.pc.domain != "console.apps.example.com" || f.pc.verified != "console.apps.example.com" {
		t.Fatalf("console state = %+v", f.pc)
	}
	// The password is in that one reply only.
	if body := f.call("GET", "/v1/public-console", "").Body.String(); strings.Contains(body, pageNewPassword) || !strings.Contains(body, `"enabled":true`) {
		t.Fatalf("status after enable = %s", body)
	}
	again := f.call("POST", "/v1/public-console/enable", `{"domain":"console.apps.example.com"}`)
	if again.Code != 200 || strings.Contains(again.Body.String(), pageNewPassword) || !strings.Contains(again.Body.String(), `"already_enabled":true`) {
		t.Fatalf("enable again = %d %s", again.Code, again.Body.String())
	}

	if rec := f.call("DELETE", "/v1/public-console", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"was_enabled":true`) || f.pc.enabled {
		t.Fatalf("disable = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(f.logs.String(), "opened from the console page") || !strings.Contains(f.logs.String(), "closed from the console page") {
		t.Fatalf("opening and closing must be logged:\n%s", f.logs.String())
	}
	f.noSecretsLeaked()
}

// Logged in on the LAN console: the LAN console password is asked again;
// missing or wrong is PUBCON-01 and changes nothing.
func TestPublicConsolePageLANNeedsPassword(t *testing.T) {
	f := newPageFixture(t, nil)
	f.lanEntry(publicconsole.EntryLAN, func(_, pw string) (bool, time.Duration) { return pw == pageLANPassword, 0 })
	v := f.view()
	if v.Entry != "lan" || !v.CanToggle || !v.NeedsPassword || v.SuggestedDomain != "" {
		t.Fatalf("view = %+v", v)
	}

	for _, body := range []string{
		`{"domain":"panel.example.com"}`,
		`{"domain":"panel.example.com","password":"` + pageWrongGuess + `"}`,
	} {
		rec := f.call("POST", "/v1/public-console/enable", body)
		code, msg, hint := errorCode(t, rec)
		if rec.Code != http.StatusForbidden || code != "PUBCON-01" || hint == "" {
			t.Fatalf("enable %s = %d %s", body, rec.Code, rec.Body.String())
		}
		if strings.Contains(msg+hint, pageWrongGuess) {
			t.Fatalf("the typed password came back in the error: %s", rec.Body.String())
		}
		if f.pc.enabled || f.pc.verified != "" {
			t.Fatal("a refused password must not start anything")
		}
	}

	rec := f.call("POST", "/v1/public-console/enable", `{"domain":"panel.example.com","password":"`+pageLANPassword+`"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), pageNewPassword) || !f.pc.enabled {
		t.Fatalf("enable = %d %s", rec.Code, rec.Body.String())
	}

	if rec := f.call("DELETE", "/v1/public-console", `{"password":"`+pageWrongGuess+`"}`); rec.Code != http.StatusForbidden || !f.pc.enabled {
		t.Fatalf("disable with a wrong password = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.call("DELETE", "/v1/public-console", ""); rec.Code != http.StatusForbidden || !f.pc.enabled {
		t.Fatalf("disable without a password = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.call("DELETE", "/v1/public-console", `{"password":"`+pageLANPassword+`"}`); rec.Code != 200 || f.pc.enabled {
		t.Fatalf("disable = %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(f.logs.String(), "entry=lan") || !strings.Contains(f.logs.String(), "192.168.31.50") {
		t.Fatalf("the log must say who did it:\n%s", f.logs.String())
	}
	f.noSecretsLeaked()
}

// Too many wrong passwords: PUBCON-02 with the minutes to wait.
func TestPublicConsolePageLockedOut(t *testing.T) {
	f := newPageFixture(t, nil)
	f.lanEntry(publicconsole.EntryLAN, func(string, string) (bool, time.Duration) { return false, 90 * time.Second })
	rec := f.call("POST", "/v1/public-console/enable", `{"domain":"panel.example.com","password":"`+pageLANPassword+`"}`)
	code, msg, _ := errorCode(t, rec)
	if rec.Code != http.StatusTooManyRequests || code != "PUBCON-02" || !strings.Contains(msg, "2 分钟") || f.pc.enabled {
		t.Fatalf("locked out = %d %s", rec.Code, rec.Body.String())
	}
}

// A machine on a local network cannot open it from the page (PUBCON-03),
// but an open one can still be closed.
func TestPublicConsolePageLANMachine(t *testing.T) {
	f := newPageFixture(t, func(c *Config) { c.ServerLAN = func() string { return "192.168.31.20" } })
	v := f.view()
	if v.Unsupported == nil || v.Unsupported.Code != "PUBCON-03" || v.Unsupported.Text == "" {
		t.Fatalf("view = %+v", v)
	}
	rec := f.call("POST", "/v1/public-console/enable", `{"domain":"panel.example.com"}`)
	if code, _, _ := errorCode(t, rec); rec.Code != http.StatusConflict || code != "PUBCON-03" || f.pc.verified != "" {
		t.Fatalf("enable on a LAN machine = %d %s", rec.Code, rec.Body.String())
	}
	f.pc.enabled, f.pc.domain = true, "panel.example.com"
	if rec := f.call("DELETE", "/v1/public-console", ""); rec.Code != 200 || f.pc.enabled {
		t.Fatalf("closing must still work = %d %s", rec.Code, rec.Body.String())
	}
}

// Logged in on the public console itself: close only (PUBCON-04 for open),
// with the public console password.
func TestPublicConsolePageFromPublicConsole(t *testing.T) {
	f := newPageFixture(t, nil)
	f.pc.enabled, f.pc.domain = true, "panel.example.com"
	f.lanEntry(publicconsole.EntryPublic, func(_, pw string) (bool, time.Duration) { return pw == "public-pass", 0 })
	if v := f.view(); v.Entry != "public" || !v.NeedsPassword || !v.CanToggle {
		t.Fatalf("view = %+v", v)
	}
	rec := f.call("POST", "/v1/public-console/enable", `{"domain":"other.example.com","password":"public-pass"}`)
	if code, _, _ := errorCode(t, rec); rec.Code != http.StatusForbidden || code != "PUBCON-04" || f.pc.domain != "panel.example.com" {
		t.Fatalf("enable from the public console = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.call("DELETE", "/v1/public-console", `{"password":"public-pass"}`); rec.Code != 200 || f.pc.enabled {
		t.Fatalf("close from the public console = %d %s", rec.Code, rec.Body.String())
	}
}

// The domain is checked from the server; a failed check is returned with the
// diagnosis code, message and hint, and nothing is turned on.
func TestPublicConsolePageDomainCheckFails(t *testing.T) {
	f := newPageFixture(t, nil)
	f.pc.here = publicconsole.Diagnosis{Code: publicconsole.CodeDNSMismatch, Message: "域名指到了别处", Hint: "改成 203.0.113.10"}
	rec := f.call("POST", "/v1/public-console/enable", `{"domain":"panel.example.com"}`)
	code, msg, hint := errorCode(t, rec)
	if rec.Code != http.StatusConflict || code != "dns_mismatch" || msg != "域名指到了别处" || hint != "改成 203.0.113.10" || f.pc.enabled {
		t.Fatalf("failed check = %d %s", rec.Code, rec.Body.String())
	}
	if rec := f.call("POST", "/v1/public-console/enable", `{"domain":"http://x"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_domain") {
		t.Fatalf("bad domain = %d %s", rec.Code, rec.Body.String())
	}
}

// The trusted entry (the command line) keeps its old behaviour: no password,
// verify then enable with the verification id; the status says "server".
func TestPublicConsoleTrustedUnchanged(t *testing.T) {
	f := newPageFixture(t, func(c *Config) { c.ServerLAN = func() string { return "192.168.31.20" } })
	rec := doJSON(t, f.trusted, http.MethodGet, "/v1/public-console", "")
	var v publicConsoleView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if rec.Code != 200 || v.Entry != "server" || v.NeedsPassword || !v.CanToggle {
		t.Fatalf("trusted status = %d %s", rec.Code, rec.Body.String())
	}
	// Even on a LAN machine the command line still does what it did.
	if rec := doJSON(t, f.trusted, http.MethodPost, "/v1/public-console/verify", `{"domain":"panel.example.com"}`); rec.Code != 200 {
		t.Fatalf("trusted verify = %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, f.trusted, http.MethodPost, "/v1/public-console/enable", `{"domain":"panel.example.com","verify_id":"v1"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), pageNewPassword) {
		t.Fatalf("trusted enable = %d %s", rec.Code, rec.Body.String())
	}
	if rec := doJSON(t, f.trusted, http.MethodDelete, "/v1/public-console", ""); rec.Code != 200 || f.pc.enabled {
		t.Fatalf("trusted disable = %d %s", rec.Code, rec.Body.String())
	}
}

// Without a session the new handlers are not reachable at all, and the
// console entry still cannot use the command line's verify step.
func TestPublicConsolePageNeedsSession(t *testing.T) {
	f := newPageFixture(t, nil)
	for _, m := range [][2]string{{"POST", "/v1/public-console/enable"}, {"DELETE", "/v1/public-console"}} {
		req := httptest.NewRequest(m[0], m[1], strings.NewReader(`{"domain":"panel.example.com"}`))
		req.Host = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		f.console.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a session = %d", m[0], m[1], rec.Code)
		}
		// A session without the CSRF header is refused too.
		req = httptest.NewRequest(m[0], m[1], strings.NewReader(`{"domain":"panel.example.com"}`))
		req.Host = "127.0.0.1:12345"
		req.AddCookie(f.cookie)
		rec = httptest.NewRecorder()
		f.console.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s without CSRF = %d", m[0], m[1], rec.Code)
		}
	}
	if f.pc.enabled || f.pc.disabled {
		t.Fatal("nothing may change without a session")
	}
}
