package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

// An app reached over the app network only (a database installed from the
// app catalog) has no address, no open hint and nothing to open, says so,
// and its access cannot be changed. Another app of the same server is not
// affected.
func TestInternalAppView(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  func(*Config)
	}{
		{"linux server, local by default", func(c *Config) {
			c.DefaultExposure = state.ExposureLocal
			c.ServerLAN = func() string { return "192.168.1.20" }
		}},
		{"desktop", func(c *Config) { c.DefaultExposure = "lan"; c.LANHost = func() string { return "192.168.31.20" } }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newFakeStore()
			k := &fakeKicker{}
			cfg := Config{Store: st, Kicker: k, Runner: newFakeRunner(), UploadDir: t.TempDir(), PublicHost: "203.0.113.5", HostMetrics: fakeHostSized(8192, 4)}
			tc.cfg(&cfg)
			h := New(cfg)
			s := newServer(cfg)
			ctx := context.Background()
			st.EnsureApp(ctx, "db")
			st.EnsureApp(ctx, "web")
			if _, err := st.UpdateApp(ctx, "db", func(a *state.App) error {
				a.Internal, a.Port, a.CatalogID = true, 5000, "demo-sql"
				a.HealthCheck = &state.HealthCheck{TCP: true}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var v appView
			w := do(t, h, "GET", "/v1/apps/db", nil, nil)
			_ = json.Unmarshal(w.Body.Bytes(), &v)
			if v.Exposure != state.ExposureInternal || v.ExposureIsDefault || v.URL != "" || v.PublicPort != 0 || v.LANURL != "" || v.OpenHint != nil || v.LocalOnly ||
				!v.InternalOnly || v.InternalNote != "只在内部网络，给别的应用用" {
				t.Fatalf("view %+v", v)
			}
			var list []appView
			w = do(t, h, "GET", "/v1/apps", nil, nil)
			_ = json.Unmarshal(w.Body.Bytes(), &list)
			for _, a := range list {
				if a.Name == "db" && (a.URL != "" || a.PublicPort != 0 || !a.InternalOnly) {
					t.Fatalf("list %+v", a)
				}
				if a.Name == "web" && (a.URL == "" || a.InternalOnly || a.Exposure == state.ExposureInternal) {
					t.Fatalf("other app %+v", a)
				}
			}
			// The install engine's address of it (platform.app.url, the
			// result's url) is empty.
			app, _ := st.GetApp(ctx, "db")
			if u := s.appAddress(ctx, app); u != "" {
				t.Fatalf("catalog app url %q", u)
			}
			kicks := len(k.kicks)
			for _, e := range []string{"lan", "public", "local", ""} {
				w := do(t, h, "PATCH", "/v1/apps/db", []byte(`{"exposure":"`+e+`"}`), nil)
				if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "internal_app") || !strings.Contains(w.Body.String(), "只在内部网络") {
					t.Fatalf("expose %q: %d %s", e, w.Code, w.Body.String())
				}
			}
			if a, _ := st.GetApp(ctx, "db"); a.Exposure != "" || !a.Internal || len(k.kicks) != kicks {
				t.Fatalf("refused change touched the app: %+v", a)
			}
			// No domain either: it has no entry route.
			if w := do(t, h, "POST", "/v1/apps/db/domains", []byte(`{"name":"db.example.com"}`), nil); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "internal_app") || !strings.Contains(w.Body.String(), "域名") {
				t.Fatalf("domain: %d %s", w.Code, w.Body.String())
			}
			if ds, _ := st.ListDomains(ctx, "db"); len(ds) != 0 || len(k.kicks) != kicks {
				t.Fatalf("refused domain was kept: %+v", ds)
			}
			// Other settings still change.
			if w := do(t, h, "PATCH", "/v1/apps/db", []byte(`{"memory_mb":300}`), nil); w.Code != http.StatusOK {
				t.Fatalf("memory: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
