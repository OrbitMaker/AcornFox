//go:build !windows

package publicconsole

import (
	"context"
	"errors"
	"testing"
	"time"
)

type httpsGetter struct {
	err  error
	urls []string
}

func (g *httpsGetter) Get(_ context.Context, u string) (int, []byte, error) {
	g.urls = append(g.urls, u)
	if g.err != nil {
		return 0, nil, g.err
	}
	return 200, []byte("<title>login</title>"), nil
}

// CheckHTTPS is what the console page polls after opening: off => nothing and
// no probe; right after opening a TLS failure is "certificate on its way";
// the same failure after the wait is "failed"; a valid answer is ok at any
// time; anything else is not a certificate verdict.
func TestManagerCheckHTTPS(t *testing.T) {
	ctx := context.Background()
	m, _, _, _, _ := newManager(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m.cfg.Now = func() time.Time { return now }
	g := &httpsGetter{err: errors.New("remote error: tls: internal error")}
	m.cfg.Getter = g

	if d := m.CheckHTTPS(ctx); d.Code != "" || len(g.urls) != 0 {
		t.Fatalf("off = %+v, probes %v", d, g.urls)
	}

	info, err := m.StartVerify(ctx, "console.apps.example.com")
	if err != nil {
		t.Fatalf("StartVerify: %v", err)
	}
	if _, err := m.Enable(ctx, info.Domain, info.VerifyID, false); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	if d := m.CheckHTTPS(ctx); d.Code != CodeCertPending || d.Hint == "" {
		t.Fatalf("just opened = %+v", d)
	}
	if len(g.urls) != 1 || g.urls[0] != "https://console.apps.example.com/login" {
		t.Fatalf("probed %v", g.urls)
	}
	now = now.Add(certWaitLimit - time.Second)
	if d := m.CheckHTTPS(ctx); d.Code != CodeCertPending {
		t.Fatalf("within the wait = %+v", d)
	}
	now = now.Add(2 * time.Second)
	if d := m.CheckHTTPS(ctx); d.Code != CodeCertFailed || d.Hint == "" {
		t.Fatalf("after the wait = %+v", d)
	}
	g.err = errors.New("dial tcp 203.0.113.10:443: connect: no route to host")
	if d := m.CheckHTTPS(ctx); d.OK() || d.Code == CodeCertPending || d.Code == CodeCertFailed {
		t.Fatalf("cannot connect to itself must not be a certificate verdict: %+v", d)
	}
	g.err = nil
	if d := m.CheckHTTPS(ctx); !d.OK() {
		t.Fatalf("certificate ready = %+v", d)
	}

	if _, err := m.Disable(ctx); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	n := len(g.urls)
	if d := m.CheckHTTPS(ctx); d.Code != "" || len(g.urls) != n {
		t.Fatalf("closed = %+v, probes %d -> %d", d, n, len(g.urls))
	}
}
