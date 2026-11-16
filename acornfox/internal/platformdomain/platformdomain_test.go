package platformdomain

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDNS answers every name under a domain with the same records.
type fakeDNS struct {
	mu    sync.Mutex
	ips   []string // nil: not found
	fail  bool     // no server answers
	calls int
}

func (f *fakeDNS) LookupIPs(_ context.Context, _, host string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail {
		return nil, errors.New("i/o timeout")
	}
	if f.ips == nil {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return f.ips, nil
}

type rig struct {
	svc     *Service
	dns     *fakeDNS
	now     time.Time
	closed  map[string]bool // addr -> refused
	certErr map[string]error
	hosts   []string
	changes int
	path    string
}

func newRig(t *testing.T, m Machine) *rig {
	t.Helper()
	r := &rig{dns: &fakeDNS{ips: []string{"203.0.113.10"}}, now: time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC),
		closed: map[string]bool{}, certErr: map[string]error{}}
	r.path = filepath.Join(t.TempDir(), FileName)
	r.svc = New(r.config(m))
	return r
}

func (r *rig) config(m Machine) Config {
	return Config{
		Path:     r.path,
		Machine:  m,
		Resolver: r.dns,
		Dial: func(_ context.Context, addr string) error {
			if r.closed[addr] {
				return errors.New("connect: connection refused")
			}
			return nil
		},
		CertProbe: func(_ context.Context, host string) error {
			if err, ok := r.certErr[host]; ok {
				return err
			}
			return nil
		},
		ActiveHosts: func(context.Context) []string { return r.hosts },
		OnChange:    func(context.Context) { r.changes++ },
		Now:         func() time.Time { return r.now },
		Label:       func() string { return "x" },
	}
}

var cloud = Machine{PublicHost: "203.0.113.10"}

func codeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestNormalize(t *testing.T) {
	ok := map[string]string{
		"apps.example.com":    "apps.example.com",
		" Apps.Example.COM. ": "apps.example.com",
		"*.apps.example.com":  "apps.example.com",
		"example.com":         "example.com",
	}
	for in, want := range ok {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q %v want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "https://apps.example.com", "apps.example.com/x", "apps.example.com:443", "203.0.113.10", "[2001:db8::1]", "localhost", "user@apps.example.com", "a_b.example.com"} {
		if _, err := Normalize(in); codeOf(err) != CodeInvalid {
			t.Errorf("%q: want %s, got %v", in, CodeInvalid, err)
		}
	}
}

func TestRecordFor(t *testing.T) {
	cases := []struct {
		base, ip string
		want     Record
	}{
		{"apps.example.com", "203.0.113.10", Record{Name: "*.apps", Type: "A", Value: "203.0.113.10", Full: "*.apps.example.com"}},
		{"example.com", "203.0.113.10", Record{Name: "*", Type: "A", Value: "203.0.113.10", Full: "*.example.com"}},
		{"a.b.example.com.cn", "203.0.113.10", Record{Name: "*.a.b", Type: "A", Value: "203.0.113.10", Full: "*.a.b.example.com.cn"}},
		{"apps.example.com", "2001:db8::1", Record{Name: "*.apps", Type: "AAAA", Value: "2001:db8::1", Full: "*.apps.example.com"}},
		{"", "203.0.113.10", Record{Type: "A", Value: "203.0.113.10"}},
	}
	for _, c := range cases {
		if got := RecordFor(c.base, c.ip); got != c.want {
			t.Errorf("%s %s: got %+v want %+v", c.base, c.ip, got, c.want)
		}
	}
}

func TestUnsetView(t *testing.T) {
	r := newRig(t, cloud)
	v := r.svc.View(context.Background(), false)
	if v.State != StateUnset || !v.Supported || v.Unsupported != nil || v.PublicIP != "203.0.113.10" ||
		v.Record.Type != "A" || v.Record.Value != "203.0.113.10" || v.Record.Name != "" || len(v.Checks) != 0 || v.Problem != nil || v.ConsoleHost != "" {
		t.Fatalf("%+v", v)
	}
	if r.dns.calls != 0 {
		t.Fatal("nothing to look up without a domain")
	}
}

// A machine without a public address cannot use one: the view says so and
// setting one is refused, with PDOMAIN-02.
func TestUnsupportedMachines(t *testing.T) {
	for name, m := range map[string]Machine{
		"lan server":               {PublicHost: "192.168.1.20", ServerLAN: func() string { return "192.168.1.20" }},
		"private":                  {PublicHost: "10.0.0.5"},
		"cgnat":                    {PublicHost: "100.64.1.2"},
		"desktop":                  {PublicHost: "localhost", Desktop: true},
		"lan behind a public host": {PublicHost: "203.0.113.10", ServerLAN: func() string { return "192.168.1.20" }},
		"name, nothing detected":   {PublicHost: "srv.example.com"},
	} {
		r := newRig(t, m)
		v := r.svc.View(context.Background(), true)
		if v.Supported || v.Unsupported == nil || v.Unsupported.Code != CodeNoPublicIP || v.Unsupported.Text != UnsupportedText {
			t.Errorf("%s: %+v", name, v)
		}
		if _, err := r.svc.Set(context.Background(), "apps.example.com", false); codeOf(err) != CodeNoPublicIP {
			t.Errorf("%s: set: %v", name, err)
		}
		if r.svc.Base() != "" || r.changes != 0 {
			t.Errorf("%s: written", name)
		}
	}
}

func TestPublicHostNameUsesDetected(t *testing.T) {
	r := newRig(t, Machine{PublicHost: "srv.example.com"})
	r.svc.cfg.DetectIPs = func(context.Context) []string { return []string{"10.1.1.1", "203.0.113.10"} }
	v := r.svc.View(context.Background(), true)
	if !v.Supported || v.PublicIP != "203.0.113.10" {
		t.Fatalf("%+v", v)
	}
}

func setDomain(t *testing.T, r *rig) View {
	t.Helper()
	v, err := r.svc.Set(context.Background(), "apps.example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func check(t *testing.T, v View, key string) Check {
	t.Helper()
	for _, c := range v.Checks {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("no check %s in %+v", key, v.Checks)
	return Check{}
}

func TestDNSNotPointing(t *testing.T) {
	r := newRig(t, cloud)
	r.dns.ips = nil
	r.hosts = []string{"hello.apps.example.com"}
	v := setDomain(t, r)
	if v.State != StateProblem || v.Problem == nil || v.Problem.Code != CodeDNS || !strings.Contains(v.Problem.Next, "*.apps") || !strings.Contains(v.Problem.Next, "203.0.113.10") {
		t.Fatalf("%+v %+v", v, v.Problem)
	}
	if c := check(t, v, CheckDNS); c.OK || c.Code != CodeDNS {
		t.Fatalf("%+v", c)
	}
	if c := check(t, v, CheckPorts); !c.Waiting || c.Text != "等上一项" {
		t.Fatalf("%+v", c)
	}
	if c := check(t, v, CheckCert); !c.Waiting {
		t.Fatalf("%+v", c)
	}
	if len(v.Checks) != 3 || v.Checks[0].Key != CheckDNS || v.Checks[1].Key != CheckPorts || v.Checks[2].Key != CheckCert {
		t.Fatalf("order %+v", v.Checks)
	}

	// Pointing elsewhere.
	r.dns.ips = []string{"198.51.100.7"}
	v = r.svc.Check(context.Background())
	if v.Problem == nil || v.Problem.Code != CodeDNS || v.Problem.Detail != "现在指向 198.51.100.7，应该指向 203.0.113.10" {
		t.Fatalf("%+v", v.Problem)
	}
	// No DNS server answers.
	r.dns.ips, r.dns.fail = nil, true
	v = r.svc.Check(context.Background())
	if v.Problem == nil || v.Problem.Code != CodeDNS || !strings.Contains(v.Problem.Detail, "没有应答") {
		t.Fatalf("%+v", v.Problem)
	}
}

func TestNoPublicAppYet(t *testing.T) {
	r := newRig(t, cloud)
	v := setDomain(t, r)
	if v.State != StateOK || v.Problem != nil {
		t.Fatalf("%+v", v)
	}
	if c := check(t, v, CheckDNS); !c.OK || c.Text != "域名指过来了" {
		t.Fatalf("%+v", c)
	}
	if c := check(t, v, CheckPorts); !c.Waiting || !strings.Contains(c.Text, "还没有开放到公网的应用") {
		t.Fatalf("%+v", c)
	}
	if c := check(t, v, CheckCert); !c.Waiting {
		t.Fatalf("%+v", c)
	}
	if v.Domain != "apps.example.com" || v.ConsoleHost != "console.apps.example.com" || v.Record.Name != "*.apps" || v.SetAt == nil {
		t.Fatalf("%+v", v)
	}
}

func TestPortsClosed(t *testing.T) {
	r := newRig(t, cloud)
	r.hosts = []string{"hello.apps.example.com", "other.example.org"}
	r.closed["203.0.113.10:443"] = true
	v := setDomain(t, r)
	if v.State != StateProblem || v.Problem.Code != CodePorts || v.Problem.Text != "外面连不上 443" || !strings.Contains(v.Problem.Detail, "203.0.113.10:443") || !strings.Contains(v.Problem.Next, "安全组") {
		t.Fatalf("%+v", v.Problem)
	}
	if c := check(t, v, CheckCert); !c.Waiting || c.Text != "等上一项" {
		t.Fatalf("%+v", c)
	}
	r.closed["203.0.113.10:80"] = true
	v = r.svc.Check(context.Background())
	if v.Problem.Text != "外面连不上 80 和 443" {
		t.Fatalf("%+v", v.Problem)
	}
}

func TestCertPendingThenFailed(t *testing.T) {
	r := newRig(t, cloud)
	r.hosts = []string{"hello.apps.example.com"}
	r.certErr["hello.apps.example.com"] = errors.New("x509: certificate signed by unknown authority")
	v := setDomain(t, r)
	if v.State != StateOK {
		t.Fatalf("still within the budget: %+v %+v", v, v.Problem)
	}
	if c := check(t, v, CheckCert); !c.Waiting || !strings.Contains(c.Text, "正在自动申请") {
		t.Fatalf("%+v", c)
	}
	if c := check(t, v, CheckPorts); !c.OK || c.Text != "外面能连上" {
		t.Fatalf("%+v", c)
	}
	r.now = r.now.Add(11 * time.Minute)
	v = r.svc.Check(context.Background())
	if v.State != StateProblem || v.Problem.Code != CodeCert || !strings.Contains(v.Problem.Detail, "hello.apps.example.com") {
		t.Fatalf("%+v %+v", v, v.Problem)
	}
	delete(r.certErr, "hello.apps.example.com")
	v = r.svc.Check(context.Background())
	if v.State != StateOK || check(t, v, CheckCert).Text != "证书已自动申请" || !check(t, v, CheckCert).OK {
		t.Fatalf("%+v", v)
	}
}

func TestAllPass(t *testing.T) {
	r := newRig(t, cloud)
	r.hosts = []string{"hello.apps.example.com"}
	v := setDomain(t, r)
	if v.State != StateOK || v.Problem != nil {
		t.Fatalf("%+v", v)
	}
	for _, c := range v.Checks {
		if !c.OK || c.Waiting || c.Code != "" {
			t.Fatalf("%+v", c)
		}
	}
}

func TestSetReplaceClear(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, cloud)
	if _, err := r.svc.Set(ctx, "http://apps.example.com", false); codeOf(err) != CodeInvalid {
		t.Fatalf("%v", err)
	}
	p, err := r.svc.PlanSet(ctx, "Apps.Example.com")
	if err != nil || p.Change != "set" || p.NeedsConfirm || p.To != "apps.example.com" {
		t.Fatalf("%+v %v", p, err)
	}
	if r.svc.Base() != "" {
		t.Fatal("plan wrote")
	}
	setDomain(t, r)
	if r.changes != 1 || r.svc.Base() != "apps.example.com" {
		t.Fatalf("changes %d base %q", r.changes, r.svc.Base())
	}
	// The same again: nothing happens.
	setDomain(t, r)
	if r.changes != 1 {
		t.Fatalf("changes %d", r.changes)
	}
	// Replacing needs confirm.
	p, _ = r.svc.PlanSet(ctx, "web.example.org")
	if p.Change != "replace" || !p.NeedsConfirm || p.From != "apps.example.com" {
		t.Fatalf("%+v", p)
	}
	if _, err := r.svc.Set(ctx, "web.example.org", false); codeOf(err) != CodeConfirm {
		t.Fatalf("%v", err)
	}
	if r.svc.Base() != "apps.example.com" || r.changes != 1 {
		t.Fatal("replaced without confirm")
	}
	r.now = r.now.Add(time.Minute)
	v, err := r.svc.Set(ctx, "web.example.org", true)
	if err != nil || v.Domain != "web.example.org" || r.changes != 2 {
		t.Fatalf("%+v %v", v, err)
	}
	// Clearing needs confirm too.
	if p := r.svc.PlanClear(); p.Change != "clear" || !p.NeedsConfirm {
		t.Fatalf("%+v", p)
	}
	if _, err := r.svc.Clear(ctx, false); codeOf(err) != CodeConfirm {
		t.Fatalf("%v", err)
	}
	v, err = r.svc.Clear(ctx, true)
	if err != nil || v.State != StateUnset || v.Domain != "" || r.changes != 3 {
		t.Fatalf("%+v %v", v, err)
	}
	// Clearing nothing is fine and changes nothing.
	if _, err := r.svc.Clear(ctx, false); err != nil || r.changes != 3 {
		t.Fatalf("%v", err)
	}
	// The operation record, newest first, and the file.
	if len(v.Changes) != 3 || v.Changes[0].From != "web.example.org" || v.Changes[0].To != "" || v.Changes[2].To != "apps.example.com" {
		t.Fatalf("%+v", v.Changes)
	}
	st, err := os.Stat(r.path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", st, err)
	}
	// A new service (the server restarted) reads it back.
	if _, err := r.svc.Set(ctx, "apps.example.com", false); err != nil {
		t.Fatal(err)
	}
	again := New(r.config(cloud))
	if again.Base() != "apps.example.com" {
		t.Fatalf("reloaded %q", again.Base())
	}
}

func TestBrokenFile(t *testing.T) {
	r := newRig(t, cloud)
	if err := os.WriteFile(r.path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := New(r.config(cloud))
	v := svc.View(context.Background(), true)
	if v.State != StateProblem || v.Problem == nil || svc.Base() != "" {
		t.Fatalf("%+v", v)
	}
	if _, err := svc.Set(context.Background(), "apps.example.com", false); err != nil {
		t.Fatal(err)
	}
	if v := svc.View(context.Background(), true); v.State == StateProblem && v.Problem.Text == "平台域名的设置文件读不了" {
		t.Fatalf("still broken: %+v", v)
	}
}

func TestViewCache(t *testing.T) {
	r := newRig(t, cloud)
	setDomain(t, r)
	n := r.dns.calls
	r.svc.View(context.Background(), false)
	if r.dns.calls != n {
		t.Fatal("recent checks are reused")
	}
	r.svc.View(context.Background(), true)
	if r.dns.calls == n {
		t.Fatal("force checks again")
	}
	n = r.dns.calls
	r.now = r.now.Add(2 * time.Minute)
	r.svc.View(context.Background(), false)
	if r.dns.calls == n {
		t.Fatal("old checks are not reused")
	}
}

func TestDialReason(t *testing.T) {
	if got := dialReason(errors.New("dial tcp: connect: connection refused")); !strings.Contains(got, "被拒绝") {
		t.Fatal(got)
	}
	if got := dialReason(&net.OpError{Op: "dial", Err: timeoutErr{}}); !strings.Contains(got, "超时") {
		t.Fatal(got)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
