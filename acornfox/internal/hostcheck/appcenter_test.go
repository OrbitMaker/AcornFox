package hostcheck

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCheckCatalogSync(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { x := now.Add(-d); return &x }
	cases := []struct {
		name  string
		s     CatalogSync
		level string
		want  []string
	}{
		{"recent ok", CatalogSync{At: at(3 * time.Hour), OK: true, Source: "https://acornfox.com/catalog", Serial: 3, Origin: "cache"}, "ok",
			[]string{"3 小时前同步过", "来源 https://acornfox.com/catalog", "同步下来的目录（序号 3）"}},
		{"just now", CatalogSync{At: at(10 * time.Second), OK: true, Serial: 3, Origin: "cache"}, "ok", []string{"刚刚同步过"}},
		{"skipped", CatalogSync{At: at(time.Hour), OK: true, Skipped: true, Serial: 2, Origin: "snapshot"}, "ok", []string{"没有受信公钥", "程序内置的目录（序号 2）"}},
		{"never", CatalogSync{Serial: 2, Origin: "snapshot"}, "warn", []string{"还没同步过", "acornfox catalog sync"}},
		{"failed", CatalogSync{At: at(30 * time.Minute), Code: "CAT-SYNC-08", Message: "所有来源都失败，继续用手上的目录", Serial: 3, Origin: "cache"}, "warn",
			[]string{"30 分钟前同步失败", "CAT-SYNC-08 所有来源都失败", "catalog sync --json"}},
		{"failed, code already in message", CatalogSync{At: at(30 * time.Minute), Code: "CAT-SYNC-07", Message: "CAT-SYNC-07 写不进去"}, "warn", []string{"（CAT-SYNC-07 写不进去）"}},
		{"stale", CatalogSync{At: at(80 * time.Hour), OK: true, Serial: 3, Origin: "cache"}, "warn", []string{"上次同步是 3 天前", "超过两天"}},
	}
	for _, c := range cases {
		r := &recorder{}
		CheckCatalogSync(r, c.s, now)
		if len(r.lines) != 1 {
			t.Fatalf("%s: %d lines", c.name, len(r.lines))
		}
		l := r.lines[0]
		if l.level != c.level || l.code != "SETUP-SYNC-01" || !l.ok {
			t.Errorf("%s: %+v", c.name, l)
		}
		for _, w := range c.want {
			if !strings.Contains(l.text, w) {
				t.Errorf("%s: %q lacks %q", c.name, l.text, w)
			}
		}
	}
}

func TestCheckGatewayStates(t *testing.T) {
	now := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		base   string
		health func(context.Context) (GatewayHealth, error)
		level  string
		want   []string
	}{
		{"off", "", nil, "ok", []string{"关掉了"}},
		{"reachable", "https://dl.example", func(context.Context) (GatewayHealth, error) {
			return GatewayHealth{Status: 200, Date: now.Add(5 * time.Second), Latency: 120 * time.Millisecond}, nil
		}, "ok", []string{"能连上 https://dl.example", "120 毫秒", "和本机时钟差约 5 秒"}},
		{"clock far off", "https://dl.example", func(context.Context) (GatewayHealth, error) {
			return GatewayHealth{Status: 200, Date: now.Add(-10 * time.Minute)}, nil
		}, "ok", []string{"差约 600 秒", "SETUP-CLOCK-01"}},
		{"no date", "https://dl.example", func(context.Context) (GatewayHealth, error) {
			return GatewayHealth{Status: 200}, nil
		}, "ok", []string{"能连上"}},
		{"unreachable", "http://127.0.0.1:9", func(context.Context) (GatewayHealth, error) {
			return GatewayHealth{}, errors.New(`Get "http://127.0.0.1:9/v1/health": dial tcp 127.0.0.1:9: connect: connection refused`)
		}, "warn", []string{"连不上 http://127.0.0.1:9（connection refused）", "公共源", "私有目录"}},
		{"wrong answer", "https://example.com", func(context.Context) (GatewayHealth, error) {
			return GatewayHealth{Status: 404}, nil
		}, "warn", []string{"应答不对（HTTP 404）"}},
		{"maintenance", "https://dl.example", func(context.Context) (GatewayHealth, error) {
			return GatewayHealth{Status: 503}, nil
		}, "warn", []string{"应答不对（HTTP 503）", "网关在维护"}},
	}
	for _, c := range cases {
		r := &recorder{}
		CheckGateway(context.Background(), r, GatewayProbe{Base: c.base, Health: c.health, Now: func() time.Time { return now }})
		if len(r.lines) != 1 {
			t.Fatalf("%s: %d lines", c.name, len(r.lines))
		}
		l := r.lines[0]
		if l.level != c.level || l.code != "SETUP-GW-01" || !l.ok {
			t.Errorf("%s: %+v", c.name, l)
		}
		for _, w := range c.want {
			if !strings.Contains(l.text, w) {
				t.Errorf("%s: %q lacks %q", c.name, l.text, w)
			}
		}
	}
}

// The real probe against a test server: one HEAD /v1/health, no body read.
func TestDefaultGatewayProbe(t *testing.T) {
	var path, method string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"version":"0.1.0","issuing":true}`))
	}))
	defer srv.Close()
	p := DefaultGatewayProbe(srv.URL + "/")
	h, err := p.Health(context.Background())
	if err != nil || path != "/v1/health" || method != http.MethodHead {
		t.Fatalf("health %v %s %s", err, method, path)
	}
	if h.Status != 200 || h.Date.IsZero() {
		t.Fatalf("health = %+v", h)
	}
	// Off: no probe at all.
	if p := DefaultGatewayProbe("  "); p.Base != "" || p.Health != nil {
		t.Fatalf("off probe = %+v", p)
	}
	// Nothing listening: an error, which CheckGateway turns into a warning.
	srv3 := httptest.NewServer(http.NotFoundHandler())
	u := srv3.URL
	srv3.Close()
	r := &recorder{}
	CheckGateway(context.Background(), r, DefaultGatewayProbe(u))
	if len(r.lines) != 1 || r.lines[0].level != "warn" || !strings.Contains(r.lines[0].text, "连不上") {
		t.Fatalf("closed gateway: %+v", r.lines)
	}
}

// The clock check and the gateway check share one HEAD request: one request
// for both lines, the clock compared with the answer's Date, and an
// unreachable gateway costs one wait, not two.
func TestDefaultGatewayProbesShareOneRequest(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		// Ten minutes ahead of this machine: the clock line must fail.
		w.Header().Set("Date", time.Now().Add(10*time.Minute).UTC().Format(http.TimeFormat))
	}))
	defer srv.Close()
	clock, gw := DefaultGatewayProbes(srv.URL)
	clock.Sync = func(context.Context) (SyncStatus, error) { return SyncStatus{SyncKnown: true, Synchronized: true}, nil }
	if clock.ReferenceTimeout != GatewayProbeTimeout {
		t.Fatalf("reference timeout %v", clock.ReferenceTimeout)
	}
	r := &recorder{}
	CheckClock(context.Background(), r, clock)
	CheckGateway(context.Background(), r, gw)
	if n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
	if len(r.lines) != 2 || r.lines[0].code != "SETUP-CLOCK-01" || r.lines[0].ok || !strings.Contains(r.lines[0].text, "超过 300 秒") {
		t.Fatalf("clock line %+v", r.lines)
	}
	if g := r.lines[1]; g.code != "SETUP-GW-01" || !g.ok || !strings.Contains(g.text, "能连上") || !strings.Contains(g.text, "太多了") {
		t.Fatalf("gateway line %+v", g)
	}

	// Nothing listening: one failed request, both lines still written.
	srv2 := httptest.NewServer(http.NotFoundHandler())
	u := srv2.URL
	srv2.Close()
	clock, gw = DefaultGatewayProbes(u)
	clock.Sync = func(context.Context) (SyncStatus, error) { return SyncStatus{SyncKnown: true, Synchronized: true}, nil }
	calls := 0
	inner := gw.Health
	gw.Health = func(ctx context.Context) (GatewayHealth, error) { calls++; return inner(ctx) }
	r = &recorder{}
	CheckClock(context.Background(), r, clock)
	CheckGateway(context.Background(), r, gw)
	if len(r.lines) != 2 || r.lines[0].level != "ok" || r.lines[1].level != "warn" || !strings.Contains(r.lines[1].text, "连不上") {
		t.Fatalf("closed gateway lines %+v", r.lines)
	}
	if calls != 1 {
		t.Fatalf("gateway check sent %d requests after the clock check", calls)
	}

	// Off: nothing to compare, nothing sent.
	clock, gw = DefaultGatewayProbes("")
	if clock.Reference != nil || gw.Health != nil || clock.Sync == nil {
		t.Fatalf("off probes %+v %+v", clock, gw)
	}
}

// shareHealth: the first answer, error included, is every caller's answer.
func TestShareHealth(t *testing.T) {
	calls := 0
	get := shareHealth(func(context.Context) (GatewayHealth, error) {
		calls++
		return GatewayHealth{}, errors.New("timeout")
	})
	for i := 0; i < 3; i++ {
		if _, err := get(context.Background()); err == nil {
			t.Fatal("want the first error")
		}
	}
	if calls != 1 {
		t.Fatalf("%d calls", calls)
	}
}
