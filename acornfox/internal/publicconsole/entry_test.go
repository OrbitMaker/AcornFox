//go:build !windows

package publicconsole

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// A request that reaches the console through the public gateway carries the
// public entry; its password check is the public console's password, and
// failures share the login page's lockout. Nothing about the password is
// logged.
func TestGatewayTagsPublicEntry(t *testing.T) {
	f, pw := newGateway(t)
	var logs bytes.Buffer
	f.h.(*gateway).cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	w := f.do("POST", "/login", url.Values{"password": {pw}}, "", origin, "198.51.100.1")
	secret := ""
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookie {
			secret = c.Value
		}
	}
	if secret == "" {
		t.Fatalf("login = %d", w.Code)
	}
	if w := f.do("GET", "/v1/apps", nil, secret, "", "198.51.100.1"); w.Code != 200 || f.console == nil {
		t.Fatalf("api = %d", w.Code)
	}
	e := EntryFrom(f.console.Context())
	if e == nil || e.Kind != EntryPublic || e.Client != "198.51.100.1" {
		t.Fatalf("entry = %+v", e)
	}
	if ok, _ := e.CheckPassword(pw); !ok {
		t.Fatal("the public console password must pass")
	}
	for i := 0; i < freeFailures; i++ {
		if ok, wait := e.CheckPassword("wrong-guess-456"); ok || wait != 0 {
			t.Fatalf("wrong password %d = %v %v", i, ok, wait)
		}
	}
	if ok, wait := e.CheckPassword(pw); ok || wait <= 0 {
		t.Fatalf("after %d failures even the right password must wait: %v %v", freeFailures, ok, wait)
	}
	// The same lockout holds for the login page of that address.
	if w := f.do("POST", "/login", url.Values{"password": {pw}}, "", origin, "198.51.100.1"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("login after page failures = %d", w.Code)
	}
	if strings.Contains(logs.String(), "wrong-guess-456") || strings.Contains(logs.String(), pw) {
		t.Fatalf("a password reached the log:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "PUBCON-01") {
		t.Fatalf("a wrong password must be logged with its code:\n%s", logs.String())
	}
}

// Through the LAN gateway the entry is the LAN one and checks the LAN
// console password (the slow hash the user set).
func TestGatewayTagsLANEntry(t *testing.T) {
	f := newLANFixture(t)
	var seen *http.Request
	f.h.(*gateway).cfg.Console = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r })
	c := f.client(t)
	if w := c.do("POST", "/setup", setupForm("lan-secret-123", "lan-secret-123", false)); w.Code != http.StatusSeeOther {
		t.Fatalf("setup = %d", w.Code)
	}
	c.do("GET", "/v1/apps", nil)
	if seen == nil {
		t.Fatal("the console was not reached")
	}
	e := EntryFrom(seen.Context())
	if e == nil || e.Kind != EntryLAN || e.Client != "192.168.31.50" {
		t.Fatalf("entry = %+v", e)
	}
	if ok, _ := e.CheckPassword("lan-secret-123"); !ok {
		t.Fatal("the LAN password must pass")
	}
	if ok, _ := e.CheckPassword("not-it"); ok {
		t.Fatal("a wrong password must not pass")
	}
	if ok, _ := e.CheckPassword(""); ok {
		t.Fatal("an empty password must not pass")
	}
}

// A request with no entry came straight in on loopback; a nil entry never
// passes a password check.
func TestEntryFromPlainContext(t *testing.T) {
	if e := EntryFrom(context.Background()); e != nil {
		t.Fatalf("entry = %+v", e)
	}
	var e *Entry
	if ok, _ := e.CheckPassword("x"); ok {
		t.Fatal("nil entry passed")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-AcornFox-Entry", "lan")
	if EntryFrom(r.Context()) != nil {
		t.Fatal("a header must not make an entry")
	}
}

type hereResolver map[string][]string

func (f hereResolver) LookupIPs(_ context.Context, _, host string) ([]string, error) {
	if ips, ok := f[host]; ok {
		return ips, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
}

type hereGetter struct {
	v   *fakeVerifier
	err error
}

func (g *hereGetter) Get(_ context.Context, u string) (int, []byte, error) {
	if g.err != nil {
		return 0, nil, g.err
	}
	g.v.mu.Lock()
	defer g.v.mu.Unlock()
	if strings.HasSuffix(u, g.v.path) && g.v.body != "" {
		return 200, []byte(g.v.body), nil
	}
	return 404, nil, nil
}

// VerifyHere checks the domain from the server: DNS first, then the HTTP
// challenge; a failed check comes back as the diagnosis and Enable can still
// use the verification once things are fixed.
func TestManagerVerifyHere(t *testing.T) {
	ctx := context.Background()
	m, _, v, _, _ := newManager(t)
	res := hereResolver{}
	m.cfg.Resolver, m.cfg.DNSServers = res, []string{"192.0.2.53"}
	m.cfg.Getter = &hereGetter{v: v}

	if _, d, err := m.VerifyHere(ctx, "console.apps.example.com"); err != nil || d.Code != CodeDNSUnresolved {
		t.Fatalf("no record = %+v %v", d, err)
	}
	res["console.apps.example.com"] = []string{"198.51.100.7"}
	if _, d, _ := m.VerifyHere(ctx, "console.apps.example.com"); d.Code != CodeDNSMismatch || !strings.Contains(d.Hint, "203.0.113.10") {
		t.Fatalf("wrong record = %+v", d)
	}
	res["console.apps.example.com"] = []string{"203.0.113.10"}
	m.cfg.Getter = &hereGetter{v: v, err: errors.New("dial tcp: i/o timeout")}
	if _, d, _ := m.VerifyHere(ctx, "console.apps.example.com"); d.OK() {
		t.Fatalf("an unreachable port 80 must not pass: %+v", d)
	}
	m.cfg.Getter = &hereGetter{v: v}
	info, d, err := m.VerifyHere(ctx, "Console.Apps.Example.com")
	if err != nil || !d.OK() {
		t.Fatalf("all good = %+v %v", d, err)
	}
	r, err := m.Enable(ctx, info.Domain, info.VerifyID, false)
	if err != nil || r.Password == "" || r.Domain != "console.apps.example.com" {
		t.Fatalf("enable after VerifyHere = %+v %v", r, err)
	}
	if _, _, err := m.VerifyHere(ctx, "shop.example.com"); errCode(err) != "domain_in_use" {
		t.Fatalf("an app's domain = %v", err)
	}
}
