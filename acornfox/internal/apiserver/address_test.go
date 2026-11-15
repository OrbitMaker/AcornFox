package apiserver

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// The address the views show is the platform variables' one: by exposure
// over http, then https on the domain bound first once there is one; the
// install engine is told the same (where). A given addresser (the app
// center's provider) is the one used.
func TestAppViewAddress(t *testing.T) {
	st := newFakeStore()
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	st.clock = func() time.Time { tick = tick.Add(time.Minute); return tick }
	cfg := Config{Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(), PublicHost: "203.0.113.5",
		DefaultExposure: state.ExposureLocal, HostMetrics: fakeHostSized(8192, 4)}
	h := New(cfg)
	s := newServer(cfg)
	ctx := context.Background()
	a, _, _ := st.EnsureApp(ctx, "web")
	port := strconv.Itoa(a.PublicPort)
	url := func() string {
		t.Helper()
		var v appView
		w := do(t, h, "GET", "/v1/apps/web", nil, nil)
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.URL
	}
	if got := url(); got != "http://127.0.0.1:"+port {
		t.Fatalf("local by default %q", got)
	}
	if _, err := st.UpdateApp(ctx, "web", func(x *state.App) error { x.Exposure = state.ExposureLAN; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := url(); got != "http://203.0.113.5:"+port {
		t.Fatalf("lan %q", got)
	}
	// Bound first: z.example.com (the name sorts last).
	for _, d := range []string{"z.example.com", "a.example.com"} {
		if _, _, err := st.AddDomain(ctx, "web", d); err != nil {
			t.Fatal(err)
		}
	}
	if got := url(); got != "https://z.example.com" {
		t.Fatalf("domain %q", got)
	}
	cur, _ := st.GetApp(ctx, "web")
	if w := s.where(ctx, cur); w.Exposure != state.ExposureLAN || w.Port != a.PublicPort || len(w.Domains) != 2 || w.Domains[0] != "z.example.com" {
		t.Fatalf("where %+v", w)
	}
	// Only this machine, with a domain: the address is the domain's, which
	// opens from anywhere, so no "only on the server" hint goes with it.
	if _, err := st.UpdateApp(ctx, "web", func(x *state.App) error { x.Exposure = state.ExposureLocal; return nil }); err != nil {
		t.Fatal(err)
	}
	var lv appView
	if err := json.Unmarshal(do(t, h, "GET", "/v1/apps/web", nil, nil).Body.Bytes(), &lv); err != nil {
		t.Fatal(err)
	}
	if lv.URL != "https://z.example.com" || lv.OpenHint != nil || !lv.LocalOnly {
		t.Fatalf("local with a domain: url %q hint %+v local_only %v", lv.URL, lv.OpenHint, lv.LocalOnly)
	}
	if _, err := st.UpdateApp(ctx, "web", func(x *state.App) error { x.Exposure = state.ExposureLAN; return nil }); err != nil {
		t.Fatal(err)
	}

	// The app center's provider, when given, decides.
	cfg.PlatformVars = platformvar.NewStandalone(platformvar.Machine{Host: platformvar.Host{PublicHost: "192.0.2.7", HTTPSPort: 8443}})
	h = New(cfg)
	if got := url(); got != "https://z.example.com:8443" {
		t.Fatalf("given provider %q", got)
	}
}
