package edgeadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/edgeclient"
)

const testToken = "0123456789abcdef0123456789abcdef"

// fakeBackend stands in for the proxy driver.
type fakeBackend struct {
	mu        sync.Mutex
	cfg       edge.Config
	applies   int
	applyErr  error
	challenge string
	health    edge.Health
}

func (f *fakeBackend) Apply(_ context.Context, cfg edge.Config) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applyErr != nil {
		return "", f.applyErr
	}
	f.cfg = cfg
	f.applies++
	return cfg.Version(), nil
}

func (f *fakeBackend) Version(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg.Version(), nil
}

func (f *fakeBackend) SetChallenge(_ context.Context, host, path, body string) error {
	f.challenge = host + path + "=" + body
	return nil
}

func (f *fakeBackend) ClearChallenge(context.Context) error { f.challenge = ""; return nil }

func (f *fakeBackend) Health(context.Context) edge.Health {
	if f.health.State == "" {
		return edge.Health{State: edge.StateRunning}
	}
	return f.health
}

// flakyBackend fails the first fails Applies.
type flakyBackend struct {
	*fakeBackend
	fails int
}

func (f *flakyBackend) Apply(ctx context.Context, cfg edge.Config) (string, error) {
	if f.fails > 0 {
		f.fails--
		return "", errors.New("connection reset by peer")
	}
	return f.fakeBackend.Apply(ctx, cfg)
}

func sampleConfig() edge.Config {
	return edge.Config{
		Routes: []edge.Route{
			{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:32768", Domains: []string{"web.example.com"}},
			{App: "admin", PublicPort: 18811, Listen: "127.0.0.1", Upstream: "127.0.0.1:32769"},
			{App: "_public-console", Upstream: "127.0.0.1:18801", Domains: []string{"panel.example.com"}},
		},
		HTTPS: edge.HTTPS{HTTPPort: 18080, HTTPSPort: 18443, Issuer: edge.IssuerInternal},
	}
}

func newTestServer(t *testing.T, b *fakeBackend) (*Server, *edgeclient.Driver, *Store) {
	t.Helper()
	store := NewStore(filepath.Join(t.TempDir(), "state"))
	s := NewServer(b, testToken, store, nil)
	hs := httptest.NewServer(s)
	t.Cleanup(hs.Close)
	return s, edgeclient.New(hs.URL, testToken), store
}

// The program's driver and the adapter's server agree on the protocol: apply,
// version, challenge and health go through, and an applied configuration is
// saved for the next start.
func TestServerProtocolRoundTrip(t *testing.T) {
	b := &fakeBackend{}
	s, c, store := newTestServer(t, b)
	ctx := context.Background()

	// Before the stored configuration was loaded: starting, no version yet.
	if h := c.Health(ctx); h.State != edge.StateStarting {
		t.Fatalf("health before Load = %+v", h)
	}
	if _, err := c.Version(ctx); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("Version while starting = %v", err)
	}
	s.Load(ctx)
	if h := c.Health(ctx); !h.OK() {
		t.Fatalf("health = %+v", h)
	}

	cfg := sampleConfig()
	v, err := c.Apply(ctx, cfg)
	if err != nil || v != cfg.Version() {
		t.Fatalf("Apply = %q %v", v, err)
	}
	if got, err := c.Version(ctx); err != nil || got != v {
		t.Fatalf("Version = %q %v", got, err)
	}
	saved, ok, err := store.Load()
	if err != nil || !ok || saved.Version() != v {
		t.Fatalf("saved %v %v %v", saved, ok, err)
	}
	if err := c.SetChallenge(ctx, "panel.example.com", "/.well-known/acornfox", "abc"); err != nil || b.challenge != "panel.example.com/.well-known/acornfox=abc" {
		t.Fatalf("challenge %v %q", err, b.challenge)
	}
	if err := c.ClearChallenge(ctx); err != nil || b.challenge != "" {
		t.Fatalf("clear %v", err)
	}
}

// Without the token, or with a wrong one, nothing but 401.
func TestServerRequiresToken(t *testing.T) {
	b := &fakeBackend{}
	s, _, _ := newTestServer(t, b)
	s.Load(context.Background())
	for _, tok := range []string{"", "ffffffffffffffffffffffffffffffff", testToken + "x"} {
		for _, req := range []*http.Request{
			httptest.NewRequest(http.MethodPut, edge.PathConfig, strings.NewReader(`{"routes":[]}`)),
			httptest.NewRequest(http.MethodGet, edge.PathVersion, nil),
			httptest.NewRequest(http.MethodGet, edge.PathHealth, nil),
			httptest.NewRequest(http.MethodDelete, edge.PathChallenge, nil),
		} {
			if tok != "" {
				req.Header.Set(edge.TokenHeader, tok)
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s token %q = %d", req.Method, req.URL.Path, tok, w.Code)
			}
		}
	}
	if b.applies != 0 {
		t.Fatal("an unauthenticated request reached the proxy")
	}
}

// A refused configuration is not saved: the stored one always applies.
func TestServerKeepsLastGoodConfiguration(t *testing.T) {
	b := &fakeBackend{}
	s, c, store := newTestServer(t, b)
	ctx := context.Background()
	s.Load(ctx)
	good := sampleConfig()
	if _, err := c.Apply(ctx, good); err != nil {
		t.Fatal(err)
	}
	b.applyErr = errors.New("port 18810 in use")
	bad := sampleConfig()
	bad.Routes[0].PublicPort = 18820
	if _, err := c.Apply(ctx, bad); err == nil || !strings.Contains(err.Error(), "port 18810 in use") {
		t.Fatalf("err = %v", err)
	}
	saved, _, _ := store.Load()
	if saved.Version() != good.Version() {
		t.Fatal("a configuration the proxy refused replaced the saved one")
	}
	// Shape errors never reach the proxy.
	b.applyErr = nil
	before := b.applies
	invalid := sampleConfig()
	invalid.Routes[1].PublicPort = invalid.Routes[0].PublicPort
	if _, err := c.Apply(ctx, invalid); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("err = %v", err)
	}
	if b.applies != before {
		t.Fatal("an invalid configuration reached the proxy")
	}
}

// Unknown fields are refused (a newer program talking to an older adapter
// must not silently lose part of its configuration).
func TestServerRefusesUnknownFields(t *testing.T) {
	s, _, _ := newTestServer(t, &fakeBackend{})
	s.Load(context.Background())
	req := httptest.NewRequest(http.MethodPut, edge.PathConfig, strings.NewReader(`{"routes":[],"https":{},"new_thing":1}`))
	req.Header.Set(edge.TokenHeader, testToken)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
}

// At start the stored configuration is applied before anything else; when
// it no longer applies, Health says so until the next good Apply.
func TestLoadAppliesStoredConfiguration(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	cfg := sampleConfig()
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	b := &fakeBackend{}
	s := NewServer(b, testToken, store, nil)
	s.Load(context.Background())
	if b.applies != 1 || b.cfg.Version() != cfg.Version() {
		t.Fatalf("applies %d cfg %v", b.applies, b.cfg)
	}

	// A failure that clears up (the proxy dropped the first request) is
	// retried.
	flaky := &flakyBackend{fakeBackend: &fakeBackend{}, fails: 1}
	s1 := NewServer(flaky, testToken, store, nil)
	s1.Load(context.Background())
	if h := s1.Health(context.Background()); !h.OK() || flaky.applies != 1 {
		t.Fatalf("health %+v applies %d", h, flaky.applies)
	}

	b2 := &fakeBackend{applyErr: errors.New("address already in use")}
	s2 := NewServer(b2, testToken, store, nil)
	s2.Load(context.Background())
	h := s2.Health(context.Background())
	if h.State != edge.StateError || !strings.Contains(h.Reason, "address already in use") {
		t.Fatalf("health = %+v", h)
	}
	b2.applyErr = nil
	hs := httptest.NewServer(s2)
	defer hs.Close()
	if _, err := edgeclient.New(hs.URL, testToken).Apply(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if h := s2.Health(context.Background()); !h.OK() {
		t.Fatalf("health after a good Apply = %+v", h)
	}
}

func TestHealthReportsExposedDefaultAdmin(t *testing.T) {
	s := NewServer(&fakeBackend{}, testToken, NewStore(t.TempDir()), nil)
	s.Load(context.Background())
	s.exposed = func() bool { return true }
	if h := s.Health(context.Background()); h.Cause != edge.CauseExposed {
		t.Fatalf("health = %+v", h)
	}
}

func TestStoreAtomicAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "s")
	st := NewStore(dir)
	if _, ok, err := st.Load(); ok || err != nil {
		t.Fatalf("empty store: %v %v", ok, err)
	}
	cfg := sampleConfig()
	if err := st.Save(cfg); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(st.Path())
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	b, _ := os.ReadFile(st.Path())
	if !bytes.Contains(b, []byte(cfg.Version())) {
		t.Fatal("the file names the version for people reading it")
	}
	got, ok, err := st.Load()
	if err != nil || !ok || got.Version() != cfg.Version() {
		t.Fatalf("load %v %v %v", got, ok, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftover temp files: %v", entries)
	}
	if err := os.WriteFile(st.Path(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.Load(); err == nil {
		t.Fatal("a broken file must be an error")
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(sampleConfig()); err != nil {
		t.Fatalf("sample rejected: %v", err)
	}
	bad := map[string]func(*edge.Config){
		"app name":         func(c *edge.Config) { c.Routes[0].App = "web/../x" },
		"duplicate app":    func(c *edge.Config) { c.Routes[1].App = "web" },
		"port range":       func(c *edge.Config) { c.Routes[0].PublicPort = 70000 },
		"port clash":       func(c *edge.Config) { c.Routes[1].PublicPort = 18810 },
		"https port clash": func(c *edge.Config) { c.Routes[1].PublicPort = 18443 },
		"lan clash":        func(c *edge.Config) { c.Routes[1].LANPort = 18810 },
		"upstream":         func(c *edge.Config) { c.Routes[0].Upstream = "nowhere" },
		"upstream port":    func(c *edge.Config) { c.Routes[0].Upstream = "127.0.0.1:0" },
		"listen":           func(c *edge.Config) { c.Routes[1].Listen = "localhost" },
		"domain":           func(c *edge.Config) { c.Routes[0].Domains = []string{"a b.example.com"} },
		"domain only lan":  func(c *edge.Config) { c.Routes[2].LANPort = 28801 },
		"nothing":          func(c *edge.Config) { c.Routes[2].Domains = nil },
		"issuer":           func(c *edge.Config) { c.HTTPS.Issuer = "self" },
		"email":            func(c *edge.Config) { c.HTTPS.Email = "a b@example.com" },
	}
	for name, mutate := range bad {
		c := sampleConfig()
		mutate(&c)
		if err := Validate(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestBootstrap(t *testing.T) {
	var cfg map[string]any
	if err := json.Unmarshal(Bootstrap("/tmp/afx-edge/proxy-admin.sock", edge.GracePeriodSeconds*time.Second), &cfg); err != nil {
		t.Fatal(err)
	}
	admin := cfg["admin"].(map[string]any)
	if admin["listen"] != "unix//tmp/afx-edge/proxy-admin.sock" {
		t.Fatalf("admin listen %v (must be a unix socket, never a TCP port)", admin["listen"])
	}
	if admin["config"].(map[string]any)["persist"] != false {
		t.Fatal("the proxy must not persist its own configuration")
	}
	httpApp := cfg["apps"].(map[string]any)["http"].(map[string]any)
	grace, err := time.ParseDuration(httpApp["grace_period"].(string))
	if err != nil || grace <= 0 || grace >= 10*time.Second {
		t.Fatalf("grace period %v must be below Docker's 10 second stop timeout", httpApp["grace_period"])
	}
	if edge.GracePeriodSeconds >= 10 {
		t.Fatal("edge.GracePeriodSeconds must stay below 10")
	}
}

func TestExpectedPortsAndProcParsing(t *testing.T) {
	got := ExpectedPorts(sampleConfig())
	want := []int{18080, 18443, 18810, 18811}
	if len(got) != len(want) {
		t.Fatalf("ports %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ports %v want %v", got, want)
		}
	}
	noDomains := edge.Config{Routes: []edge.Route{{App: "a", PublicPort: 18812, LANPort: 28812, Upstream: "x:1"}}}
	if p := ExpectedPorts(noDomains); len(p) != 2 || p[0] != 18812 || p[1] != 28812 {
		t.Fatalf("ports %v", p)
	}

	// A fake /proc: pid 7 owns the sockets with inodes 100 (LISTEN 18810) and
	// 101 (LISTEN [::]:18811); inode 102 (LISTEN 18820) belongs to someone
	// else; inode 103 is an established connection on 18812.
	proc := t.TempDir()
	base := filepath.Join(proc, "7")
	mustMkdir(t, filepath.Join(base, "fd"))
	mustMkdir(t, filepath.Join(base, "net"))
	for i, target := range []string{"socket:[100]", "socket:[101]", "socket:[103]", "/dev/null"} {
		if err := os.Symlink(target, filepath.Join(base, "fd", string(rune('3'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	header := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	row := func(local, st, inode string) string {
		return "   0: " + local + " 00000000:0000 " + st + " 00000000:00000000 00:00000000 00000000     0        0 " + inode + " 1 0000000000000000 100 0 0 10 0\n"
	}
	tcp := header + row("00000000:497A", "0A", "100") + row("00000000:4984", "0A", "102") + row("0100007F:497C", "01", "103")
	tcp6 := header + row("00000000000000000000000000000000:497B", "0A", "101")
	mustWrite(t, filepath.Join(base, "net", "tcp"), tcp)
	mustWrite(t, filepath.Join(base, "net", "tcp6"), tcp6)
	have, err := ListeningPorts(proc, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !have[18810] || !have[18811] || have[18820] || have[18822] || len(have) != 2 {
		t.Fatalf("listening = %v", have)
	}
	if m := missing([]int{18810, 18811, 18820}, have); len(m) != 1 || m[0] != 18820 {
		t.Fatalf("missing = %v", m)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
