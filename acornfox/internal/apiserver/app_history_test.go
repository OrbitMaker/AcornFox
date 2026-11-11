package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/runner"
)

func appHistoryServer(t *testing.T) (http.Handler, *AppHistory, *metricRunner, *fakeStore, *time.Time) {
	t.Helper()
	_, store, run := metricServer(t)
	now := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	hist := NewAppHistory(store, run)
	hist.now = func() time.Time { return now }
	h := New(Config{Store: store, Kicker: &fakeKicker{}, Runner: run, UploadDir: t.TempDir(), AppHistory: hist})
	return h, hist, run, store, &now
}

func appHistoryGet(t *testing.T, h http.Handler, path string) (int, appHistoryView) {
	t.Helper()
	w := do(t, h, "GET", path, nil, nil)
	var v appHistoryView
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
	}
	return w.Code, v
}

func TestAppHistoryWithoutRecorder(t *testing.T) {
	h, _, _ := metricServer(t)
	code, v := appHistoryGet(t, h, "/v1/metrics/apps/shop/history")
	if code != 200 || v.App != "shop" || len(v.Ranges) != 0 || len(v.Points) != 0 || v.Since != nil {
		t.Fatalf("no history: %d %+v", code, v)
	}
}

func TestAppHistoryRangesAndAverages(t *testing.T) {
	h, hist, run, _, now := appHistoryServer(t)
	start := *now
	// 3 hours of 30-second samples; memory climbs by 1 MB each sample.
	for i := 0; i < 360; i++ {
		run.stats.MemoryUsageMB = float64(i + 1)
		hist.sample(context.Background())
		*now = now.Add(hostHistoryInterval)
	}
	code, v := appHistoryGet(t, h, "/v1/metrics/apps/shop/history?range=1h")
	if code != 200 || strings.Join(v.Ranges, ",") != "1h,24h" || v.StepSeconds != 30 {
		t.Fatalf("1h: %d %+v", code, v)
	}
	if len(v.Points) != 119 && len(v.Points) != 120 {
		t.Fatalf("1h should hold about 120 raw samples, got %d", len(v.Points))
	}
	last := v.Points[len(v.Points)-1]
	if last.MemoryUsageMB == nil || *last.MemoryUsageMB != 360 || last.CPUPercent == nil || *last.CPUPercent != 12.5 {
		t.Fatalf("last 1h point = %+v", last)
	}
	if last.MemoryLimitMB != nil {
		t.Fatalf("no limit on the container: memory_limit_mb must be left out, got %v", *last.MemoryLimitMB)
	}
	if v.Since == nil || !v.Since.Equal(start) {
		t.Fatalf("since = %v, want %v", v.Since, start)
	}
	_, d := appHistoryGet(t, h, "/v1/metrics/apps/shop/history?range=24h")
	if d.StepSeconds != 300 || len(d.Points) != 36 {
		t.Fatalf("24h: step %d, %d points (want 36 five-minute averages)", d.StepSeconds, len(d.Points))
	}
	// First 5-minute step holds samples 1..10 MB: average 5.5.
	if m := d.Points[0].MemoryUsageMB; m == nil || *m != 5.5 {
		t.Fatalf("first 24h average = %v, want 5.5", m)
	}
}

func TestAppHistoryErrors(t *testing.T) {
	h, _, _, _, _ := appHistoryServer(t)
	for path, want := range map[string]int{
		"/v1/metrics/apps/shop/history?range=7d": 400,
		"/v1/metrics/apps/Bad_Name/history":      400,
		"/v1/metrics/apps/nosuch/history":        404,
	} {
		if code, _ := appHistoryGet(t, h, path); code != want {
			t.Fatalf("%s: got %d, want %d", path, code, want)
		}
	}
}

func TestAppHistoryStoppedLeavesGapAndCPUWarmup(t *testing.T) {
	h, hist, run, _, now := appHistoryServer(t)
	run.stats.CPUAvailable = false
	run.stats.MemoryLimitMB = 512
	hist.sample(context.Background())
	*now = now.Add(hostHistoryInterval)
	run.statsErr = runner.ErrStopped
	hist.sample(context.Background())
	*now = now.Add(hostHistoryInterval)
	run.statsErr = errors.New("docker hiccup")
	hist.sample(context.Background())
	_, v := appHistoryGet(t, h, "/v1/metrics/apps/shop/history")
	if len(v.Points) != 1 {
		t.Fatalf("stopped or unreadable passes must record nothing, got %d points", len(v.Points))
	}
	p := v.Points[0]
	if p.CPUPercent != nil || p.MemoryLimitMB == nil || *p.MemoryLimitMB != 512 {
		t.Fatalf("warming-up CPU must be left out and the limit kept: %+v", p)
	}
}

func TestAppHistoryDropsDeletedAppAndKeepsOneDay(t *testing.T) {
	_, hist, _, store, now := appHistoryServer(t)
	for i := 0; i < hostHistoryCap+10; i++ {
		hist.sample(context.Background())
		*now = now.Add(hostHistoryInterval)
	}
	pts, since := hist.points("shop", time.Time{})
	if len(pts) != hostHistoryCap || !since.Equal(pts[0].T) || !pts[0].T.Before(pts[len(pts)-1].T) {
		t.Fatalf("ring should hold exactly one day in order, got %d", len(pts))
	}
	if err := store.DeleteApp(context.Background(), "shop", false); err != nil {
		t.Fatal(err)
	}
	hist.sample(context.Background())
	if pts, _ := hist.points("shop", time.Time{}); len(pts) != 0 {
		t.Fatalf("a deleted app's history must be dropped, %d points left", len(pts))
	}
}
