//go:build live

package appinstall

// Opt-in measurement against the real download gateway (not part of any
// normal run: it needs the build tag live and the network).
//
//	go test ./internal/appinstall/ -tags live -run TestLiveGatewayThroughput -v -count=1
//
// What it does: registers a throw-away machine key (a temporary data folder)
// with the gateway, asks for the address of the Hermes image archive and, for
// AFX_LIVE_SECONDS seconds (default 30) each, measures (1) one plain GET of
// the file, (2) the segmented download with one connection, and (3) with the
// default 16 up to 32 connections, printing the bytes per second and the
// connections once a second. Nothing is installed; the downloads are cut
// after the seconds (the cache is a temporary folder).
//
// Environment: ACORNFOX_GATEWAY (default https://dl.acornfox.com),
// AFX_LIVE_FILE (default hermes-agent-v0.23.0-image), AFX_LIVE_SECONDS.
// The address never appears in the output.

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/identity"
)

func liveEnv(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func TestLiveGatewayThroughput(t *testing.T) {
	base := liveEnv("ACORNFOX_GATEWAY", "https://dl.acornfox.com")
	file := liveEnv("AFX_LIVE_FILE", "hermes-agent-v0.23.0-image")
	secs, _ := strconv.Atoi(liveEnv("AFX_LIVE_SECONDS", "30"))
	id, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := &gatewayclient.Client{BaseURL: base, Identity: id, Info: gatewayclient.DefaultInfo("live-test")}
	issue := func() (gatewayclient.Issued, error) { return c.Issue(context.Background(), file, "app") }
	iss, err := issue()
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	t.Logf("文件 %s，%.0f MB，后端 %s", file, float64(iss.Size)/(1<<20), iss.Backend)

	t.Run("一路普通下载", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(secs)*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, iss.URL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(scrubURL(err))
		}
		defer resp.Body.Close()
		var n int64
		buf := make([]byte, 256<<10)
		t0 := time.Now()
		last := t0
		for {
			k, err := resp.Body.Read(buf)
			n += int64(k)
			if now := time.Now(); now.Sub(last) >= time.Second {
				t.Logf("+%ds %.2f MB/s（累计）", int(now.Sub(t0).Seconds()), float64(n)/(1<<20)/now.Sub(t0).Seconds())
				last = now
			}
			if err != nil {
				break
			}
		}
		t.Logf("一路普通下载：%.0f 秒下了 %.1f MB，平均 %.2f MB/s", time.Since(t0).Seconds(), float64(n)/(1<<20), float64(n)/(1<<20)/time.Since(t0).Seconds())
	})

	seg := func(t *testing.T, start, max int) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(secs)*time.Second)
		defer cancel()
		dir := t.TempDir()
		prm := segParams{Enabled: true, StartConns: start, MaxConns: max, SegSize: 4 << 20, Window: 5 * time.Second}
		cache := newDLCacheEntry(filepath.Join(dir, "dl-cache"), iss.SHA256, iss.Size, prm.SegSize)
		src := dlSource{URL: iss.URL, ExpiresAt: iss.ExpiresAt, Reissue: func(ctx context.Context) (string, time.Time, error) {
			again, err := c.Issue(ctx, file, "app")
			return again.URL, again.ExpiresAt, err
		}}
		st := &dlStatus{}
		t0 := time.Now()
		done := make(chan error, 1)
		go func() {
			done <- segDownload(ctx, http.DefaultClient, src, cache, filepath.Join(dir, "out.tar"), iss.SHA256, st, prm)
		}()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		var prev int64
		for {
			select {
			case err := <-done:
				n := st.done.Load()
				t.Logf("%d～%d 路：%.0f 秒下了 %.1f MB，平均 %.2f MB/s（%v）", start, max, time.Since(t0).Seconds(), float64(n)/(1<<20), float64(n)/(1<<20)/time.Since(t0).Seconds(), scrubURL(err))
				return
			case now := <-tick.C:
				n := st.done.Load()
				t.Logf("+%ds %.2f MB/s（这一秒） %d 路，重试 %d", int(now.Sub(t0).Seconds()), float64(n-prev)/(1<<20), st.conns.Load(), st.resumes.Load())
				prev = n
			}
		}
	}
	t.Run("分段一路", func(t *testing.T) { seg(t, 1, 1) })
	t.Run("分段十六到三十二路", func(t *testing.T) { seg(t, 16, 32) })
}
