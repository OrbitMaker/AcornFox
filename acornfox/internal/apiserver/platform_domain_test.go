package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/platformdomain"
	"github.com/acornfox/acornfox/internal/state"
)

// pointsHere answers every name with the machine's address.
type pointsHere struct{ ips []string }

func (p pointsHere) LookupIPs(_ context.Context, _, host string) ([]string, error) {
	if p.ips == nil {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return p.ips, nil
}

func newPDServer(t *testing.T, publicHost string, dns pointsHere) (http.Handler, *fakeStore, *platformdomain.Service, *int) {
	t.Helper()
	st := newFakeStore()
	changes := 0
	svc := platformdomain.New(platformdomain.Config{
		Path:        filepath.Join(t.TempDir(), platformdomain.FileName),
		Machine:     platformdomain.Machine{PublicHost: publicHost},
		Resolver:    dns,
		Dial:        func(context.Context, string) error { return nil },
		CertProbe:   func(context.Context, string) error { return nil },
		ActiveHosts: func(context.Context) []string { return []string{"hello.apps.example.com"} },
		OnChange:    func(context.Context) { changes++ },
	})
	h := New(Config{Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(),
		PublicHost: publicHost, HostMetrics: fakeHostSized(8192, 4), PlatformDomain: svc,
		Resolver: fakeResolver{err: errors.New("no lookups in tests")}})
	return h, st, svc, &changes
}

type pdAnswer struct {
	Domain      string `json:"domain"`
	State       string `json:"state"`
	Supported   bool   `json:"supported"`
	Unsupported *struct {
		Code string `json:"code"`
		Text string `json:"text"`
	} `json:"unsupported"`
	PublicIP string `json:"public_ip"`
	Record   struct {
		Name, Type, Value string
	} `json:"record"`
	Checks []struct {
		Key  string `json:"key"`
		OK   bool   `json:"ok"`
		Text string `json:"text"`
	} `json:"checks"`
	Problem *struct {
		Code, Text, Detail, Next string
	} `json:"problem"`
	ConsoleHost string               `json:"console_host"`
	Apps        []platformDomainApp  `json:"apps"`
	Plan        *platformdomain.Plan `json:"plan"`
}

func decodePD(t *testing.T, w interface{ Bytes() []byte }) pdAnswer {
	t.Helper()
	var a pdAnswer
	if err := json.Unmarshal(w.Bytes(), &a); err != nil {
		t.Fatalf("%v: %s", err, w.Bytes())
	}
	return a
}

func pdErrCode(t *testing.T, body []byte) string {
	t.Helper()
	var b struct {
		Error struct{ Code, Message, Hint string } `json:"error"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return b.Error.Code
}

func TestPlatformDomainRoutes(t *testing.T) {
	h, st, _, changes := newPDServer(t, "203.0.113.10", pointsHere{ips: []string{"203.0.113.10"}})
	ctx := context.Background()
	hello, _, _ := st.EnsureApp(ctx, "hello")
	st.UpdateApp(ctx, "hello", func(a *state.App) error { a.Exposure = state.ExposurePublic; return nil })
	st.EnsureApp(ctx, "notes")
	st.UpdateApp(ctx, "notes", func(a *state.App) error { a.Exposure = state.ExposureLAN; return nil })

	// Not set yet.
	w := do(t, h, "GET", "/v1/platform/domain", nil, nil)
	a := decodePD(t, w.Body)
	if w.Code != 200 || a.State != "unset" || !a.Supported || a.Record.Type != "A" || a.Record.Value != "203.0.113.10" || len(a.Checks) != 0 {
		t.Fatalf("%d %+v", w.Code, a)
	}
	if v := appDetail(t, h, "hello"); v.PlatformDomain != nil || v.URL != "http://203.0.113.10:"+itoa(hello.PublicPort) {
		t.Fatalf("%+v", v)
	}

	// Not a domain name.
	w = do(t, h, "PUT", "/v1/platform/domain", []byte(`{"domain":"https://apps.example.com"}`), nil)
	if w.Code != 400 || pdErrCode(t, w.Body.Bytes()) != platformdomain.CodeInvalid {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	// A plan writes nothing.
	w = do(t, h, "PUT", "/v1/platform/domain", []byte(`{"domain":"apps.example.com","plan":true}`), nil)
	a = decodePD(t, w.Body)
	if w.Code != 200 || a.Plan == nil || a.Plan.Change != "set" || a.Domain != "" || *changes != 0 {
		t.Fatalf("%d %+v", w.Code, a)
	}
	if row := pdRow(t, a, "hello"); row.URLAfter != "https://hello.apps.example.com" {
		t.Fatalf("%+v", row)
	}
	if row := pdRow(t, a, "notes"); !strings.HasPrefix(row.URLAfter, "http://203.0.113.10:") {
		t.Fatalf("%+v", row)
	}

	// Set.
	w = do(t, h, "PUT", "/v1/platform/domain", []byte(`{"domain":"apps.example.com"}`), nil)
	a = decodePD(t, w.Body)
	if w.Code != 200 || a.Domain != "apps.example.com" || a.State != "ok" || len(a.Checks) != 3 || a.Record.Name != "*.apps" || a.ConsoleHost != "console.apps.example.com" || *changes != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if row := pdRow(t, a, "hello"); row.Host != "hello.apps.example.com" || !row.Active || row.URL != "https://hello.apps.example.com" {
		t.Fatalf("%+v", row)
	}
	if row := pdRow(t, a, "notes"); row.Host != "notes.apps.example.com" || row.Active || row.Note != pdNoteNotPublic {
		t.Fatalf("%+v", row)
	}

	// The app views follow: the address and platform_domain.
	v := appDetail(t, h, "hello")
	if v.URL != "https://hello.apps.example.com" || v.PlatformDomain == nil || v.PlatformDomain.Host != "hello.apps.example.com" || !v.PlatformDomain.Active || v.PlatformDomain.Note != "" {
		t.Fatalf("%+v %+v", v, v.PlatformDomain)
	}
	v = appDetail(t, h, "notes")
	if strings.HasPrefix(v.URL, "https://") || v.PlatformDomain == nil || v.PlatformDomain.Active || v.PlatformDomain.Note != "改成公网后自动生效" {
		t.Fatalf("%+v %+v", v, v.PlatformDomain)
	}

	// The app's own domain comes first.
	if w := do(t, h, "POST", "/v1/apps/hello/domains", []byte(`{"name":"hello.mine.org"}`), nil); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	v = appDetail(t, h, "hello")
	if v.URL != "https://hello.mine.org" || !v.PlatformDomain.Active || v.PlatformDomain.Note != pdNoteOwnDomain {
		t.Fatalf("%+v %+v", v, v.PlatformDomain)
	}
	do(t, h, "DELETE", "/v1/apps/hello/domains/hello.mine.org", nil, nil)
	if v := appDetail(t, h, "hello"); v.URL != "https://hello.apps.example.com" {
		t.Fatalf("%+v", v)
	}

	// A name another app bound is not hello's.
	st.EnsureApp(ctx, "other")
	do(t, h, "POST", "/v1/apps/other/domains", []byte(`{"name":"hello.apps.example.com"}`), nil)
	v = appDetail(t, h, "hello")
	if v.URL == "https://hello.apps.example.com" || v.PlatformDomain.Active || !strings.Contains(v.PlatformDomain.Note, "应用 other") || !strings.Contains(v.PlatformDomain.Note, platformdomain.CodeAppName) {
		t.Fatalf("%+v %+v", v, v.PlatformDomain)
	}
	do(t, h, "DELETE", "/v1/apps/other/domains/hello.apps.example.com", nil, nil)

	// Replacing needs confirm.
	w = do(t, h, "PUT", "/v1/platform/domain", []byte(`{"domain":"web.example.org"}`), nil)
	if w.Code != 409 || pdErrCode(t, w.Body.Bytes()) != platformdomain.CodeConfirm {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = do(t, h, "PUT", "/v1/platform/domain", []byte(`{"domain":"web.example.org","plan":true}`), nil)
	if a := decodePD(t, w.Body); a.Plan == nil || !a.Plan.NeedsConfirm || a.Plan.Change != "replace" || pdRow(t, a, "hello").URLAfter != "https://hello.web.example.org" {
		t.Fatalf("%s", w.Body)
	}
	w = do(t, h, "PUT", "/v1/platform/domain", []byte(`{"domain":"web.example.org","confirm":true}`), nil)
	if w.Code != 200 || decodePD(t, w.Body).Domain != "web.example.org" || *changes != 2 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	// Clearing needs confirm; the query form works too.
	w = do(t, h, "DELETE", "/v1/platform/domain", nil, nil)
	if w.Code != 409 || pdErrCode(t, w.Body.Bytes()) != platformdomain.CodeConfirm {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = do(t, h, "DELETE", "/v1/platform/domain?plan=true", nil, nil)
	if a := decodePD(t, w.Body); a.Plan == nil || a.Plan.Change != "clear" || a.Domain != "web.example.org" {
		t.Fatalf("%s", w.Body)
	}
	w = do(t, h, "DELETE", "/v1/platform/domain", []byte(`{"confirm":true}`), nil)
	if w.Code != 200 || decodePD(t, w.Body).State != "unset" || *changes != 3 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if v := appDetail(t, h, "hello"); v.PlatformDomain != nil || strings.HasPrefix(v.URL, "https://") {
		t.Fatalf("%+v", v)
	}
	// Check again.
	if w := do(t, h, "POST", "/v1/platform/domain/check", nil, nil); w.Code != 200 || decodePD(t, w.Body).State != "unset" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestPlatformDomainProblemAndLANMachine(t *testing.T) {
	h, _, _, _ := newPDServer(t, "203.0.113.10", pointsHere{ips: []string{"198.51.100.7"}})
	w := do(t, h, "PUT", "/v1/platform/domain", []byte(`{"domain":"apps.example.com"}`), nil)
	a := decodePD(t, w.Body)
	if w.Code != 200 || a.State != "problem" || a.Problem == nil || a.Problem.Code != platformdomain.CodeDNS ||
		a.Problem.Detail != "现在指向 198.51.100.7，应该指向 203.0.113.10" || a.Checks[0].OK {
		t.Fatalf("%s", w.Body)
	}

	h, _, _, _ = newPDServer(t, "192.168.1.20", pointsHere{})
	w = do(t, h, "GET", "/v1/platform/domain", nil, nil)
	a = decodePD(t, w.Body)
	if a.Supported || a.Unsupported == nil || a.Unsupported.Code != platformdomain.CodeNoPublicIP || a.Unsupported.Text != "这台机器在局域网里，没有公网地址，用不了域名" {
		t.Fatalf("%s", w.Body)
	}
	w = do(t, h, "PUT", "/v1/platform/domain", []byte(`{"domain":"apps.example.com"}`), nil)
	if w.Code != 409 || pdErrCode(t, w.Body.Bytes()) != platformdomain.CodeNoPublicIP {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestPlatformDomainAbsent(t *testing.T) {
	h, st, _, _, _ := newTestServer(t)
	for _, c := range []struct{ method, path string }{{"GET", "/v1/platform/domain"}, {"PUT", "/v1/platform/domain"}, {"DELETE", "/v1/platform/domain"}, {"POST", "/v1/platform/domain/check"}} {
		if w := do(t, h, c.method, c.path, []byte(`{}`), nil); w.Code != 404 {
			t.Fatalf("%s %s: %d", c.method, c.path, w.Code)
		}
	}
	st.EnsureApp(context.Background(), "hello")
	if v := appDetail(t, h, "hello"); v.PlatformDomain != nil {
		t.Fatalf("%+v", v)
	}
}

func TestPlatformDomainInternalApp(t *testing.T) {
	h, st, svc, _ := newPDServer(t, "203.0.113.10", pointsHere{ips: []string{"203.0.113.10"}})
	if _, err := svc.Set(context.Background(), "apps.example.com", false); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st.EnsureApp(ctx, "db")
	st.UpdateApp(ctx, "db", func(a *state.App) error { a.Internal = true; a.Exposure = state.ExposurePublic; return nil })
	if v := appDetail(t, h, "db"); v.PlatformDomain != nil || v.URL != "" {
		t.Fatalf("%+v", v)
	}
}

func pdRow(t *testing.T, a pdAnswer, app string) platformDomainApp {
	t.Helper()
	for _, r := range a.Apps {
		if r.App == app {
			return r
		}
	}
	t.Fatalf("no %s in %+v", app, a.Apps)
	return platformDomainApp{}
}

func appDetail(t *testing.T, h http.Handler, app string) appView {
	t.Helper()
	w := do(t, h, "GET", "/v1/apps/"+app, nil, nil)
	var v appView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || w.Code != 200 {
		t.Fatalf("%d %v %s", w.Code, err, w.Body)
	}
	return v
}
