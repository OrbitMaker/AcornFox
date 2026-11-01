package publicconsole

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func lanRequest(h http.Handler, method, host, path string, form url.Values, cookie, origin, remote string) *httptest.ResponseRecorder {
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "http://"+host+path, body)
	r.Host = host
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != "" {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.RemoteAddr = remote
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLANGatewayLogin(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "lan.json"))
	if err := store.Save(&Config{PasswordSHA256: Digest("pw")}); err != nil {
		t.Fatal(err)
	}
	var seen *http.Request
	console := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r; _, _ = w.Write([]byte("console")) })
	h := NewGateway(GatewayConfig{Store: store, Sessions: &fakeSessions{}, Console: console, LAN: true})
	host := "192.168.31.20:18802"

	if w := lanRequest(h, "GET", host, "/", nil, "", "", "192.168.31.50:5000"); w.Code != http.StatusSeeOther {
		t.Fatalf("anonymous = %d", w.Code)
	}
	if w := lanRequest(h, "GET", host, "/login", nil, "", "", "192.168.31.50:5000"); !strings.Contains(w.Body.String(), "局域网") || w.Header().Get("Strict-Transport-Security") != "" {
		t.Fatalf("LAN login page must mention the LAN and send no HSTS: %s", w.Body.String())
	}
	if w := lanRequest(h, "POST", host, "/login", url.Values{"password": {"pw"}}, "", "http://evil.example", "192.168.31.50:5000"); w.Code != http.StatusForbidden {
		t.Fatalf("cross origin = %d", w.Code)
	}
	w := lanRequest(h, "POST", host, "/login", url.Values{"password": {"pw"}}, "", "http://"+host, "192.168.31.50:5000")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login = %d %s", w.Code, w.Body.String())
	}
	var secret string
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			secret = c.Value
			if c.Secure {
				t.Fatal("plain-HTTP LAN cookie must not be Secure (the browser would drop it)")
			}
		}
	}
	if w := lanRequest(h, "GET", host, "/v1/apps", nil, secret, "", "192.168.31.50:5000"); w.Code != 200 || seen == nil {
		t.Fatalf("logged-in request = %d", w.Code)
	}
}

func TestLANGatewayRateLimitUsesPeerAddress(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "lan.json"))
	_ = store.Save(&Config{PasswordSHA256: Digest("pw")})
	h := NewGateway(GatewayConfig{Store: store, Sessions: &fakeSessions{}, Console: http.NotFoundHandler(), LAN: true})
	host := "10.0.0.2:18802"
	for i := 0; i < 5; i++ {
		lanRequest(h, "POST", host, "/login", url.Values{"password": {"nope"}}, "", "http://"+host, "10.0.0.9:1000")
	}
	// A spoofed X-Forwarded-For must not reset the counter.
	r := httptest.NewRequest("POST", "http://"+host+"/login", strings.NewReader("password=pw"))
	r.Host = host
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://"+host)
	r.Header.Set("X-Forwarded-For", "10.0.0.77")
	r.RemoteAddr = "10.0.0.9:1001"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("after 5 failures = %d, want 429", w.Code)
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestLANManagerEnableDisable(t *testing.T) {
	port := freePort(t)
	store := NewFileStore(filepath.Join(t.TempDir(), "lan.json"))
	m := NewLANManager(LANConfig{Store: store, Sessions: &fakeSessions{}, Console: http.NotFoundHandler(), Port: port, Host: func() string { return "192.168.31.20" }})
	defer m.Close()
	ctx := context.Background()
	if st, _ := m.Status(ctx); st.Enabled {
		t.Fatal("must start disabled")
	}
	st, pw, err := m.Enable(ctx, false)
	if err != nil || pw == "" || !st.Enabled || st.URL != "http://192.168.31.20:"+strconv.Itoa(port) {
		t.Fatalf("enable = %+v %q %v", st, pw, err)
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/login")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("listener not serving: %v", err)
	}
	resp.Body.Close()
	if _, again, _ := m.Enable(ctx, false); again != "" {
		t.Fatal("enabling again must keep the password and not reveal it")
	}
	if _, reset, _ := m.Enable(ctx, true); reset == "" || reset == pw {
		t.Fatal("reset must generate a new password")
	}
	if err := m.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
		t.Fatal("listener still open after disable")
	}
}
