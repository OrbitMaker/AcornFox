package publicconsole

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	lanHostT = "192.168.31.20:18802"
	lanPeerT = "192.168.31.50:5000"
)

type lanClient struct {
	t       *testing.T
	h       http.Handler
	cookies map[string]string
	remote  string
	ua      string
}

func (c *lanClient) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	c.t.Helper()
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, "http://"+lanHostT+path, body)
	r.Host = lanHostT
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method != "GET" {
		r.Header.Set("Origin", "http://"+lanHostT)
	}
	if c.ua != "" {
		r.Header.Set("User-Agent", c.ua)
	}
	for k, v := range c.cookies {
		r.AddCookie(&http.Cookie{Name: k, Value: v})
	}
	r.RemoteAddr = c.remote
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	for _, ck := range w.Result().Cookies() {
		if ck.MaxAge < 0 || ck.Value == "" {
			delete(c.cookies, ck.Name)
		} else {
			c.cookies[ck.Name] = ck.Value
		}
	}
	return w
}

type lanFixture struct {
	store    *FileStore
	h        http.Handler
	now      *time.Time
	sessions *fakeSessions
}

func newLANFixture(t *testing.T) *lanFixture {
	t.Helper()
	store := NewFileStore(filepath.Join(t.TempDir(), "lan.json"))
	if err := store.Save(&Config{EnabledAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	f := &lanFixture{store: store, now: &now, sessions: &fakeSessions{}}
	console := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("console")) })
	f.h = NewGateway(GatewayConfig{Store: store, Sessions: f.sessions, Console: console, LAN: true, Now: func() time.Time { return *f.now }})
	return f
}

func (f *lanFixture) client(t *testing.T) *lanClient {
	return &lanClient{t: t, h: f.h, cookies: map[string]string{}, remote: lanPeerT, ua: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/131.0 Safari/537.36"}
}

func setupForm(pw, confirm string, remember bool) url.Values {
	v := url.Values{"password": {pw}, "confirm": {confirm}}
	if remember {
		v.Set("remember", "1")
	}
	return v
}

func TestLANFirstVisitorSetsPassword(t *testing.T) {
	f := newLANFixture(t)
	c := f.client(t)
	if w := c.do("GET", "/", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous = %d %s", w.Code, w.Header().Get("Location"))
	}
	if w := c.do("GET", "/v1/apps", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("the API must stay closed while no password is set, got %d", w.Code)
	}
	page := c.do("GET", "/login", nil).Body.String()
	if !strings.Contains(page, `action="/setup"`) || !strings.Contains(page, "还没有密码") || !strings.Contains(page, "只在可信的网络里用") {
		t.Fatalf("first visit must show the set-password form:\n%s", page)
	}
	for _, bad := range []struct {
		form url.Values
		want string
	}{
		{setupForm("short", "short", false), "至少 8 位"},
		{setupForm("long enough", "different one", false), "不一样"},
		{setupForm(strings.Repeat("x", 129), strings.Repeat("x", 129), false), "太长"},
	} {
		if w := c.do("POST", "/setup", bad.form); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), bad.want) {
			t.Fatalf("setup %q = %d, want 400 with %q", bad.form.Get("password"), w.Code, bad.want)
		}
	}
	if cfg, _ := f.store.Load(); cfg.HasPassword() {
		t.Fatal("a refused password must not be saved")
	}
	// Eight characters count by character, not byte: eight Chinese characters pass.
	if w := c.do("POST", "/setup", setupForm("一二三四五六七八", "一二三四五六七八", false)); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("setup = %d %s", w.Code, w.Body.String())
	}
	cfg, _ := f.store.Load()
	if cfg.PasswordHash == "" || strings.Contains(cfg.PasswordHash, "一二三") || !strings.HasPrefix(cfg.PasswordHash, "pbkdf2-sha256$600000$") || cfg.PasswordSHA256 != "" {
		t.Fatalf("password must be stored as a slow salted hash, got %q", cfg.PasswordHash)
	}
	if cfg.PasswordSetAt == nil || !cfg.PasswordSetAt.Equal(*f.now) || cfg.PasswordSetFrom != "192.168.31.50" {
		t.Fatalf("who set the password must be recorded: %v %q", cfg.PasswordSetAt, cfg.PasswordSetFrom)
	}
	if len(cfg.Devices) != 0 || c.cookies[deviceCookie] != "" {
		t.Fatal("no device may be remembered when the box is not ticked")
	}
	if w := c.do("GET", "/v1/apps", nil); w.Code != 200 {
		t.Fatalf("setting the password must log the visitor in, got %d", w.Code)
	}

	// The second visitor can only log in: the form is gone and POST /setup changes nothing.
	other := f.client(t)
	other.remote = "192.168.31.77:6000"
	if page := other.do("GET", "/login", nil).Body.String(); strings.Contains(page, `action="/setup"`) || !strings.Contains(page, `action="/login"`) {
		t.Fatalf("once set, the page must be a login form:\n%s", page)
	}
	if w := other.do("POST", "/setup", setupForm("hijack attempt", "hijack attempt", false)); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("second setup = %d", w.Code)
	}
	after, _ := f.store.Load()
	if after.PasswordHash != cfg.PasswordHash || after.PasswordSetFrom != "192.168.31.50" {
		t.Fatal("a second visitor must not be able to replace the password")
	}
	if w := other.do("POST", "/login", url.Values{"password": {"hijack attempt"}}); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d", w.Code)
	}
	if w := other.do("POST", "/login", url.Values{"password": {"一二三四五六七八"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("right password = %d", w.Code)
	}
}

func TestLANRememberDeviceNinetyDays(t *testing.T) {
	f := newLANFixture(t)
	c := f.client(t)
	if w := c.do("POST", "/setup", setupForm("correct horse", "correct horse", true)); w.Code != http.StatusSeeOther {
		t.Fatalf("setup = %d", w.Code)
	}
	secret := c.cookies[deviceCookie]
	cfg, _ := f.store.Load()
	if secret == "" || len(cfg.Devices) != 1 {
		t.Fatalf("device must be remembered: cookie %q, %d devices", secret, len(cfg.Devices))
	}
	d := cfg.Devices[0]
	if d.Name != "Mac · Chrome" || d.Digest != Digest(secret) || strings.Contains(d.Digest, secret) || !d.ExpiresAt.Equal(f.now.Add(90*24*time.Hour)) || d.LastIP != "192.168.31.50" {
		t.Fatalf("device = %+v", d)
	}
	if cfg.Sessions[0].DeviceID != d.ID {
		t.Fatal("the session must belong to the remembered device")
	}

	// The session is gone (24 hours passed, or the browser lost it): a page
	// load gets a new one without a password and comes back to the same page.
	renew := func(at time.Duration) *httptest.ResponseRecorder {
		*f.now = time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC).Add(at)
		delete(c.cookies, sessionCookie)
		return c.do("GET", "/#apps", nil)
	}
	if w := renew(89 * 24 * time.Hour); w.Code != http.StatusSeeOther || w.Header().Get("Location") == "/login" || c.cookies[sessionCookie] == "" {
		t.Fatalf("day 89: a remembered device must get a session, got %d -> %s", w.Code, w.Header().Get("Location"))
	}
	if w := c.do("GET", "/v1/apps", nil); w.Code != 200 {
		t.Fatalf("renewed session = %d", w.Code)
	}
	if cfg, _ := f.store.Load(); !cfg.Devices[0].LastUsedAt.Equal(*f.now) {
		t.Fatal("last used must move when the device renews")
	}
	// API calls are not renewed silently: the page holds the old CSRF token.
	delete(c.cookies, sessionCookie)
	if w := c.do("GET", "/v1/apps", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("an API call without a session = %d, want 401 (the page reloads)", w.Code)
	}
	if w := renew(91 * 24 * time.Hour); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("day 91: the device must have expired, got %d -> %s", w.Code, w.Header().Get("Location"))
	}
	if c.cookies[deviceCookie] != "" {
		t.Fatal("an expired device cookie must be cleared")
	}
}

func TestLANRemoveDeviceAndLogoutAndReset(t *testing.T) {
	f := newLANFixture(t)
	m := NewLANManager(LANConfig{Store: f.store, Sessions: f.sessions, Console: http.NotFoundHandler(), Port: freePort(t)})
	ctx := context.Background()
	a, b := f.client(t), f.client(t)
	b.ua = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1"
	a.do("POST", "/setup", setupForm("correct horse", "correct horse", true))
	b.do("POST", "/login", url.Values{"password": {"correct horse"}, "remember": {"1"}})
	ds, _ := m.Devices(ctx)
	if len(ds) != 2 || ds[0].Name != "Mac · Chrome" || ds[1].Name != "iPhone · Safari" {
		t.Fatalf("devices = %+v", ds)
	}

	// Removing A's device ends A at once and leaves B alone.
	if ok, err := m.RemoveDevice(ctx, ds[0].ID); !ok || err != nil {
		t.Fatalf("remove = %v %v", ok, err)
	}
	if ok, _ := m.RemoveDevice(ctx, ds[0].ID); ok {
		t.Fatal("removing twice must report that nothing was there")
	}
	if w := a.do("GET", "/v1/apps", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("a removed device's session must stop working at once, got %d", w.Code)
	}
	if w := a.do("GET", "/", nil); w.Header().Get("Location") != "/login" {
		t.Fatal("a removed device must be asked for the password again")
	}
	if w := b.do("GET", "/v1/apps", nil); w.Code != 200 {
		t.Fatalf("the other device = %d", w.Code)
	}

	// Logging out forgets the device too.
	if w := b.do("POST", "/v1/console/logout", nil); w.Code != 200 {
		t.Fatalf("logout = %d", w.Code)
	}
	if ds, _ := m.Devices(ctx); len(ds) != 0 || b.cookies[deviceCookie] != "" {
		t.Fatalf("logout must forget the device: %d left, cookie %q", len(ds), b.cookies[deviceCookie])
	}

	// reset-password: everything granted under the old password is gone and
	// the next visitor sets a new one.
	b.do("POST", "/login", url.Values{"password": {"correct horse"}, "remember": {"1"}})
	st, err := m.ResetPassword(ctx)
	if err != nil || !st.Enabled || st.PasswordSet || st.Devices != 0 {
		t.Fatalf("reset = %+v %v", st, err)
	}
	if w := b.do("GET", "/", nil); w.Header().Get("Location") != "/login" {
		t.Fatal("reset must end remembered devices")
	}
	if page := b.do("GET", "/login", nil).Body.String(); !strings.Contains(page, `action="/setup"`) {
		t.Fatal("after reset the page must offer to set a password again")
	}
	if n, _ := m.RemoveDevices(ctx); n != 0 {
		t.Fatalf("nothing left to remove, got %d", n)
	}
}

func TestLANOnlyLocalNetworkClients(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.1.9": true, "10.1.2.3": true, "172.16.0.1": true, "172.31.255.254": true, "127.0.0.1": true,
		"169.254.10.1": true, "100.64.0.1": true, "100.127.255.254": true, "fd12:3456::1": true, "fe80::1%eth0": true, "::1": true,
		"::ffff:192.168.1.9": true,
		"172.32.0.1":         false, "100.128.0.1": false, "8.8.8.8": false, "203.0.113.7": false, "2001:db8::1": false,
		"::ffff:8.8.8.8": false, "": false, "not-an-ip": false,
	} {
		if got := LANSource(addr); got != want {
			t.Errorf("LANSource(%q) = %v, want %v", addr, got, want)
		}
	}
	f := newLANFixture(t)
	c := f.client(t)
	c.remote = "203.0.113.7:4444"
	for _, path := range []string{"/", "/login", "/login.css", "/login.js", "/v1/apps"} {
		w := c.do("GET", path, nil)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "LAN-CONSOLE-02") {
			t.Fatalf("GET %s from a public address = %d %s", path, w.Code, w.Body.String())
		}
	}
	if w := c.do("POST", "/setup", setupForm("public attacker", "public attacker", false)); w.Code != http.StatusForbidden {
		t.Fatalf("a public address must not be able to set the password, got %d", w.Code)
	}
	if cfg, _ := f.store.Load(); cfg.HasPassword() {
		t.Fatal("password set from a public address")
	}
}

func TestLANGeneratedPasswordFromOlderVersionStillWorks(t *testing.T) {
	f := newLANFixture(t)
	_ = f.store.Save(&Config{PasswordSHA256: Digest("abcdef-ghjkmn-pqrstu-vwxyzA"), EnabledAt: time.Now()})
	m := NewLANManager(LANConfig{Store: f.store, Sessions: f.sessions, Console: http.NotFoundHandler(), Port: freePort(t)})
	st, _ := m.Status(context.Background())
	if !st.PasswordSet || !st.GeneratedPassword || st.PasswordSetAt != nil {
		t.Fatalf("status of an upgraded LAN console = %+v", st)
	}
	c := f.client(t)
	if page := c.do("GET", "/login", nil).Body.String(); strings.Contains(page, `action="/setup"`) || !strings.Contains(page, "生成的密码") {
		t.Fatalf("an upgraded LAN console must keep asking for the generated password:\n%s", page)
	}
	if w := c.do("POST", "/login", url.Values{"password": {"abcdef-ghjkmn-pqrstu-vwxyzA"}, "remember": {"1"}}); w.Code != http.StatusSeeOther || c.cookies[deviceCookie] == "" {
		t.Fatalf("old password login = %d", w.Code)
	}
}

func TestLANManagerRefusedOnCloudServer(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "lan.json"))
	why := "这台服务器是云主机"
	m := NewLANManager(LANConfig{Store: store, Sessions: &fakeSessions{}, Console: http.NotFoundHandler(), Port: freePort(t), Refuse: func() string { return why }})
	defer m.Close()
	ctx := context.Background()
	_, _, err := m.Enable(ctx, false)
	var pe *Error
	if !errors.As(err, &pe) || pe.Code != "LAN-CONSOLE-01" || pe.Status != http.StatusConflict {
		t.Fatalf("enable on a cloud server = %v", err)
	}
	if st, _ := m.Status(ctx); st.Enabled {
		t.Fatal("a refused enable must leave the LAN console closed")
	}
	if _, err := m.ResetPassword(ctx); !errors.As(err, &pe) || pe.Code != "lan_console_off" {
		t.Fatalf("reset while closed = %v", err)
	}
	// One that an older version already opened stays open (upgrades change nothing).
	_ = store.Save(&Config{PasswordSHA256: Digest("pw"), EnabledAt: time.Now()})
	if st, _, err := m.Enable(ctx, false); err != nil || !st.Enabled {
		t.Fatalf("an already open LAN console must stay usable: %+v %v", st, err)
	}
	why = ""
	_ = m.Disable(ctx)
	if st, _, err := m.Enable(ctx, false); err != nil || !st.Enabled {
		t.Fatalf("enable on a LAN server = %+v %v", st, err)
	}
}

func TestDeviceName(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36 Edg/131.0": "Windows · Edge",
		"Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0":                                "Linux · Firefox",
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/131.0 Mobile Safari/537.36":         "Android · Chrome",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15":     "Mac · Safari",
		"curl/8.5.0": "未知设备",
		"":           "未知设备",
	} {
		if got := DeviceName(ua); got != want {
			t.Errorf("DeviceName(%q) = %q, want %q", ua, got, want)
		}
	}
}

func TestPasswordHash(t *testing.T) {
	h1, _ := HashPassword("correct horse")
	h2, _ := HashPassword("correct horse")
	if h1 == h2 {
		t.Fatal("two hashes of one password must differ (salt)")
	}
	c := &Config{PasswordHash: h1}
	if !c.CheckPassword("correct horse") || c.CheckPassword("correct horsf") || c.CheckPassword("") {
		t.Fatal("hash check")
	}
	if (&Config{}).CheckPassword("") {
		t.Fatal("no password must match nothing, not even the empty string")
	}
	for _, bad := range []string{"", "pbkdf2-sha256$1$AA$AA", "pbkdf2-sha256$600000$!!$AA", "argon2$1$AA$AA", "pbkdf2-sha256$600000$AAAA$"} {
		if verifyPasswordHash(bad, "x") {
			t.Fatalf("malformed hash %q must not verify", bad)
		}
	}
}

// The login page matches the console: same brand, the console's remembered
// theme and accent, and nothing inline (the CSP forbids it).
func TestLoginPageLooksLikeTheConsole(t *testing.T) {
	f := newLANFixture(t)
	c := f.client(t)
	w := c.do("GET", "/login", nil)
	page := w.Body.String()
	for _, want := range []string{"AFX Console", `<script src="/login.js"></script>`, `<link rel="stylesheet" href="/login.css">`, "<h1>设置密码</h1>", `class="fox"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("login page lacks %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "style=") || strings.Contains(page, "<style") || strings.Contains(page, "onclick") {
		t.Fatalf("the login page must not carry inline style or script:\n%s", page)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe") {
		t.Fatalf("CSP = %q", csp)
	}
	js := c.do("GET", "/login.js", nil)
	if js.Code != 200 || !strings.HasPrefix(js.Header().Get("Content-Type"), "text/javascript") || !strings.Contains(js.Body.String(), "afx-accent") || !strings.Contains(js.Body.String(), "afx-theme") {
		t.Fatalf("login.js = %d %s", js.Code, js.Body.String())
	}
	css := c.do("GET", "/login.css", nil).Body.String()
	for _, want := range []string{"--primary:240 5.9% 10%", "[data-accent=blue]", "[data-theme=dark]", "height:32px", "font:13px/1.5"} {
		if !strings.Contains(css, want) {
			t.Fatalf("login.css lacks %q", want)
		}
	}
	// A wrong password shows the boxed error, still without inline style.
	c.do("POST", "/setup", setupForm("correct horse", "correct horse", false))
	other := f.client(t)
	bad := other.do("POST", "/login", url.Values{"password": {"nope nope"}}).Body.String()
	if !strings.Contains(bad, `<p class="err" role="alert">`) || !strings.Contains(bad, "密码不对。") || !strings.Contains(bad, "<h1>登录</h1>") || strings.Contains(bad, "style=") {
		t.Fatalf("error page:\n%s", bad)
	}
}
