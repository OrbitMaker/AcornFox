package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

func TestStatsHostHumanOutput(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	cpu, used, total := 45.2, uint64(2<<30), uint64(8<<30)
	h.api.hostMetricsFn = func(ctx context.Context) (client.HostMetrics, error) {
		return client.HostMetrics{Available: true, CPUPercent: &cpu, MemoryUsed: &used, MemoryTotal: &total}, nil
	}
	code, out, errOut := h.run("stats")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"CPU 使用率：45.20%", "内存使用：2.00 GB / 8.00 GB (25.0%)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestStatsHostUnavailable(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	code, out, _ := h.run("stats")
	if code != exitOK || !strings.Contains(out, "主机指标不可用") {
		t.Fatalf("exit %d, out %q", code, out)
	}
}

// The positional app name must win over the app resolved from .acornfox.
func TestStatsAppUsesPositionalName(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	if err := saveProject(h.workDir, &projectFile{Target: "dev", App: "notes"}); err != nil {
		t.Fatalf("saveProject: %v", err)
	}
	var gotApp string
	h.api.appMetricsFn = func(ctx context.Context, app string) (client.AppMetrics, error) {
		gotApp = app
		return client.AppMetrics{App: app, CPUPercent: 1.5, MemoryUsageMB: 128, MemoryLimitMB: 512}, nil
	}
	code, out, errOut := h.run("stats", "shop")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if gotApp != "shop" {
		t.Fatalf("queried app %q, want shop", gotApp)
	}
	if !strings.Contains(out, "内存使用：128.00 MB / 512.00 MB (25.0%)") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestStatsAppFromAppFlag(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	var gotApp string
	h.api.appMetricsFn = func(ctx context.Context, app string) (client.AppMetrics, error) {
		gotApp = app
		return client.AppMetrics{App: app, CPUPercent: 1.5, MemoryUsageMB: 128, MemoryLimitMB: 512}, nil
	}
	if code, _, errOut := h.run("--app", "shop", "stats"); code != exitOK || gotApp != "shop" {
		t.Fatalf("exit %d app %q stderr %q", code, gotApp, errOut)
	}
}

// A zero memory limit must not divide by zero.
func TestStatsAppNoMemoryLimit(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	h.api.appMetricsFn = func(ctx context.Context, app string) (client.AppMetrics, error) {
		return client.AppMetrics{App: app, MemoryUsageMB: 64}, nil
	}
	_, out, _ := h.run("stats", "shop")
	if !strings.Contains(out, "（无限制）") || strings.Contains(out, "NaN") || strings.Contains(out, "Inf") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestStatsAppJSON(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	h.api.appMetricsFn = func(ctx context.Context, app string) (client.AppMetrics, error) {
		return client.AppMetrics{App: app, CPUPercent: 12.3, MemoryUsageMB: 256, MemoryLimitMB: 512, PIDs: 7}, nil
	}
	code, out, _ := h.run("--json", "stats", "shop")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid json %q: %v", out, err)
	}
	if got["app"] != "shop" || got["cpu_percent"] != 12.3 || got["memory_limit_mb"] != 512.0 {
		t.Fatalf("unexpected json: %v", got)
	}
}

func TestStatsRejectsExtraArgs(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	if code, _, _ := h.run("stats", "a", "b"); code != exitUsage {
		t.Fatalf("exit %d, want usage error", code)
	}
}

func TestStatsAppHistory(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	if err := saveProject(h.workDir, &projectFile{Target: "dev"}); err != nil {
		t.Fatalf("saveProject: %v", err)
	}
	f := func(v float64) *float64 { return &v }
	var gotApp, gotRange string
	h.api.appHistoryFn = func(ctx context.Context, app, rng string) (client.AppHistory, error) {
		gotApp, gotRange = app, rng
		t0 := time.Now().Add(-10 * time.Minute)
		return client.AppHistory{App: app, Range: rng, StepSeconds: 30, Ranges: []string{"1h", "24h"}, Since: &t0, Points: []client.AppHistoryPoint{
			{T: t0, MemoryUsageMB: f(100)},
			{T: t0.Add(30 * time.Second), MemoryUsageMB: f(300), CPUPercent: f(4)},
		}}, nil
	}
	code, out, errOut := h.run("stats", "shop", "--history", "1h")
	if code != exitOK || gotApp != "shop" || gotRange != "1h" {
		t.Fatalf("exit %d app %q range %q stderr %q", code, gotApp, gotRange, errOut)
	}
	for _, want := range []string{"2 个点", "最低 100.0 MB，平均 200.0 MB，最高 300.0 MB", "CPU：平均 4.00%，最高 4.00%", "不满 1 小时"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = h.run("--json", "stats", "shop", "--history", "24h")
	if code != exitOK || gotRange != "24h" || !strings.Contains(out, `"points"`) || !strings.Contains(out, `"memory_usage_mb"`) {
		t.Fatalf("json: exit %d range %q out %s", code, gotRange, out)
	}
}

func TestStatsAppHistoryUsageAndEmpty(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	if err := saveProject(h.workDir, &projectFile{Target: "dev"}); err != nil {
		t.Fatalf("saveProject: %v", err)
	}
	if code, _, errOut := h.run("stats", "shop", "--history", "7d"); code != exitUsage || !strings.Contains(errOut, "1h 或 24h") {
		t.Fatalf("7d: exit %d stderr %q", code, errOut)
	}
	if code, _, errOut := h.run("stats", "--history", "1h"); code != exitUsage || !strings.Contains(errOut, "要带应用名") {
		t.Fatalf("no app: exit %d stderr %q", code, errOut)
	}
	// Old server: no ranges.
	if code, out, _ := h.run("stats", "shop", "--history", "1h"); code != exitOK || !strings.Contains(out, "没有记录应用的历史") {
		t.Fatalf("old server: exit %d out %q", code, out)
	}
	h.api.appHistoryFn = func(ctx context.Context, app, rng string) (client.AppHistory, error) {
		return client.AppHistory{App: app, Range: rng, Ranges: []string{"1h", "24h"}}, nil
	}
	if code, out, _ := h.run("stats", "shop", "--history", "1h"); code != exitOK || !strings.Contains(out, "还没有记录") {
		t.Fatalf("empty: exit %d out %q", code, out)
	}
}
