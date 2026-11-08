package apiserver

// Tests of the read-only endpoints behind the console's home dashboard,
// runtime page and settings page: GET /v1/metrics/host/history,
// GET /v1/runtime/checks, the read-only LAN and public console status on the console
// entry and the version in GET /v1/status.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/reconcile"
	"github.com/acornfox/acornfox/internal/state"
)

// seqHost returns successive views; CPU climbs by 1 each call.
type seqHost struct{ n float64 }

func (h *seqHost) Host(context.Context) HostView {
	h.n++
	cpu := h.n
	used, total := uint64(h.n*100), uint64(1000)
	return HostView{Available: true, CPUPercent: &cpu, MemoryUsed: &used, MemoryTotal: &total}
}

func newTestHistory(start time.Time) (*HostHistory, *time.Time) {
	now := start
	h := NewHostHistory(&seqHost{})
	h.now = func() time.Time { return now }
	return h, &now
}

func historyGet(t *testing.T, h http.Handler, q string) (int, hostHistoryView) {
	t.Helper()
	rec := do(t, h, "GET", "/v1/metrics/host/history"+q, nil, nil)
	var v hostHistoryView
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	return rec.Code, v
}

func TestHostHistoryWithoutRecorder(t *testing.T) {
	h, _, _, _, _ := newTestServer(t)
	code, v := historyGet(t, h, "")
	if code != 200 || len(v.Ranges) != 0 || len(v.Points) != 0 || v.Since != nil {
		t.Fatalf("no history: code %d view %+v", code, v)
	}
	if code, _ := historyGet(t, h, "?range=7d"); code != 400 {
		t.Fatalf("7d must be refused (nothing kept on disk), got %d", code)
	}
}

func TestHostHistoryRangesAndAverages(t *testing.T) {
	start := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	hist, now := newTestHistory(start)
	// 3 hours of 30-second samples.
	for i := 0; i < 360; i++ {
		hist.sample(context.Background())
		*now = now.Add(hostHistoryInterval)
	}
	cfg, _, _ := newTestConfig(t)
	cfg.HostHistory = hist
	h := New(cfg)

	code, v := historyGet(t, h, "?range=1h")
	if code != 200 || strings.Join(v.Ranges, ",") != "1h,24h" || v.StepSeconds != 30 {
		t.Fatalf("1h: %d %+v", code, v)
	}
	if len(v.Points) != 119 && len(v.Points) != 120 {
		t.Fatalf("1h should hold about 120 raw samples, got %d", len(v.Points))
	}
	last := v.Points[len(v.Points)-1]
	if last.CPUPercent == nil || *last.CPUPercent != 360 || last.MemoryTotal == nil {
		t.Fatalf("last 1h point = %+v", last)
	}
	if v.Since == nil || !v.Since.Equal(start) {
		t.Fatalf("since = %v, want %v", v.Since, start)
	}

	_, d := historyGet(t, h, "?range=24h")
	if d.StepSeconds != 300 || len(d.Points) != 36 {
		t.Fatalf("24h: step %d, %d points (want 36 five-minute averages)", d.StepSeconds, len(d.Points))
	}
	// The first 5 minutes hold samples 1..10: average 5.5.
	if p := d.Points[0]; p.CPUPercent == nil || *p.CPUPercent != 5.5 || p.MemoryUsed == nil || *p.MemoryUsed != 550 {
		t.Fatalf("first 24h bucket = %+v", p)
	}
}

func TestHostHistoryRingKeepsOneDay(t *testing.T) {
	start := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	hist, now := newTestHistory(start)
	for i := 0; i < hostHistoryCap+10; i++ {
		hist.sample(context.Background())
		*now = now.Add(hostHistoryInterval)
	}
	if hist.n != hostHistoryCap {
		t.Fatalf("ring holds %d, want %d", hist.n, hostHistoryCap)
	}
	if want := start.Add(10 * hostHistoryInterval); !hist.since.Equal(want) {
		t.Fatalf("since = %v, want %v (oldest kept sample)", hist.since, want)
	}
	pts := hist.points(time.Time{})
	if len(pts) != hostHistoryCap || !pts[0].T.Equal(hist.since) || !pts[0].T.Before(pts[len(pts)-1].T) {
		t.Fatalf("points out of order or wrong length: %d", len(pts))
	}
}

func TestHostHistorySkipsUnavailable(t *testing.T) {
	h := NewHostHistory(fakeHostProvider{view: HostView{Available: false}})
	h.sample(context.Background())
	if h.n != 0 {
		t.Fatalf("unavailable snapshots must not be recorded")
	}
}

// --- runtime checks ---

type fakeLANConsole struct{ enabled bool }

func (f *fakeLANConsole) Status(context.Context) (LANConsoleStatus, error) {
	return LANConsoleStatus{Enabled: f.enabled, Port: 18802, URL: "http://192.168.1.10:18802"}, nil
}
func (f *fakeLANConsole) Enable(context.Context, bool) (LANConsoleStatus, string, error) {
	return LANConsoleStatus{}, "", errors.New("not in tests")
}
func (f *fakeLANConsole) Disable(context.Context) error { return nil }

const runtimeTCPHdr = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

func runtimeTCPRow(local, inode string) string {
	return "   0: " + local + " 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 " + inode + " 1 0000000000000000 100 0 0 10 0\n"
}

// withRuntimeEnv switches the OS, /proc root and disk readers for one test.
func withRuntimeEnv(t *testing.T, goos, procRows string, free uint64) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(runtimeTCPHdr+procRows), 0o644); err != nil {
		t.Fatal(err)
	}
	oldOS, oldRoot, oldFree, oldDev, oldRoot2, oldClock := runtimeGOOS, runtimeProcRoot, runtimeFree, runtimeDevice, runtimeIsRoot, runtimeClockProbe
	runtimeGOOS, runtimeProcRoot = goos, root
	runtimeFree = func(string) (uint64, error) { return free, nil }
	runtimeDevice = func(string) uint64 { return 7 }
	runtimeIsRoot = func() bool { return false }
	// The clock: synchronized, no time comparison (no timedatectl, no network).
	runtimeClockProbe = func(string) hostcheck.ClockProbe {
		return hostcheck.ClockProbe{Sync: func(context.Context) (hostcheck.SyncStatus, error) {
			return hostcheck.SyncStatus{SyncKnown: true, Synchronized: true}, nil
		}}
	}
	t.Cleanup(func() {
		runtimeGOOS, runtimeProcRoot, runtimeFree, runtimeDevice, runtimeIsRoot, runtimeClockProbe = oldOS, oldRoot, oldFree, oldDev, oldRoot2, oldClock
	})
}

// The clock line: compared with the download gateway named in the download
// sources; ten minutes off fails with SETUP-CLOCK-01.
func TestRuntimeChecksClock(t *testing.T) {
	withRuntimeEnv(t, "linux", "", 50<<30)
	var asked []string
	runtimeClockProbe = func(gw string) hostcheck.ClockProbe {
		asked = append(asked, gw)
		return hostcheck.ClockProbe{
			Sync: func(context.Context) (hostcheck.SyncStatus, error) {
				return hostcheck.SyncStatus{NTPKnown: true, NTP: false, SyncKnown: true}, nil
			},
			Reference: func(context.Context) (time.Time, error) { return time.Now().Add(10 * time.Minute), nil },
		}
	}
	cfg, _, _ := newTestConfig(t)
	cfg.DataDir = "/var/lib/acornfox"
	cfg.Sources = func() DownloadSources { return DownloadSources{Gateway: "https://dl.example.invalid"} }
	v := runtimeGet(t, New(cfg))
	it := itemWith(v, "时钟：")
	if it == nil || it.Code != "SETUP-CLOCK-01" || it.Check != "SETUP-CLOCK-01" || it.Level != "fail" || v.OK || !strings.Contains(it.Text, "timedatectl set-ntp true") {
		t.Fatalf("clock item %+v (all %+v)", it, v.Items)
	}
	if len(asked) != 1 || asked[0] != "https://dl.example.invalid" {
		t.Fatalf("reference asked for %v", asked)
	}
	// Desktop: no clock line.
	runtimeGOOS = "darwin"
	if it := itemWith(runtimeGet(t, New(cfg)), "时钟"); it != nil {
		t.Fatalf("desktop clock line %+v", it)
	}
}

func runtimeGet(t *testing.T, h http.Handler) runtimeChecksView {
	t.Helper()
	rec := do(t, h, "GET", "/v1/runtime/checks", nil, nil)
	if rec.Code != 200 {
		t.Fatalf("runtime checks = %d %s", rec.Code, rec.Body.String())
	}
	var v runtimeChecksView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func itemWith(v runtimeChecksView, prefix string) *runtimeCheck {
	for i := range v.Items {
		if strings.HasPrefix(v.Items[i].Text, prefix) {
			return &v.Items[i]
		}
	}
	return nil
}

func TestRuntimeChecksLinuxHealthy(t *testing.T) {
	// 18808 (admin) and an app's port are listening; owners are unknown to
	// an unprivileged service but are AcornFox's own.
	cfg, store, _ := newTestConfig(t)
	ctx := context.Background()
	app, _, err := store.EnsureApp(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.UpdateApp(ctx, "web", func(a *state.App) error {
		a.Desired, a.CurrentDeployment = state.DesiredRunning, "d1"
		return nil
	})
	withRuntimeEnv(t, "linux", runtimeTCPRow("0100007F:4978", "1")+runtimeTCPRow(fmt.Sprintf("00000000:%04X", app.PublicPort), "2"), 50<<30)
	cfg.DataDir = "/var/lib/acornfox"
	cfg.EdgeStatus = func() (string, string) { return reconcile.EdgeStateRunning, "" }
	cfg.CheckPorts = hostcheck.Ports{Admin: 18808, LAN: 18802, AppMin: 18810, AppMax: 18899}
	cfg.LANConsole = &fakeLANConsole{}
	cfg.Sources = func() DownloadSources {
		return DownloadSources{China: true, RegionSource: "install.env", DockerHub: []string{"docker.m.daocloud.io", "registry-1.docker.io"}}
	}
	cfg.Runner.(*fakeRunner).pingResp.DockerRootDir = "/var/lib/docker"
	v := runtimeGet(t, New(cfg))
	if !v.OK || v.OS != "linux" || v.DataDir != "/var/lib/acornfox" {
		t.Fatalf("view = %+v", v)
	}
	for _, p := range []string{"后台服务", "Docker", "网关（Caddy）：运行中", "磁盘：数据目录 /var/lib/acornfox、Docker 数据目录 /var/lib/docker 所在分区剩余 50.0 GB", "端口："} {
		it := itemWith(v, p)
		if it == nil || it.Level != "ok" {
			t.Fatalf("missing ok item %q in %+v", p, v.Items)
		}
	}
	// Passed items name their check (code stays empty, as before).
	for p, check := range map[string]string{"后台服务": "SETUP-SVC-01", "Docker": "SETUP-RT-03", "网关（Caddy）": "SETUP-EDGE-01", "磁盘：": "SETUP-DISK-01", "端口：": "SETUP-PORT-01"} {
		if it := itemWith(v, p); it.Check != check || it.Code != "" {
			t.Fatalf("item %q: check %q code %q", p, it.Check, it.Code)
		}
	}
	if !strings.Contains(itemWith(v, "端口：").Text, fmt.Sprintf("18808、%d 是 AcornFox 自己在用", app.PublicPort)) {
		t.Fatalf("own ports not recognized: %s", itemWith(v, "端口：").Text)
	}
	if v.Sources == nil || v.Sources.DockerHub[0] != "docker.m.daocloud.io" {
		t.Fatalf("sources = %+v", v.Sources)
	}
}

func TestRuntimeChecksFaults(t *testing.T) {
	// 2 GB free, an unknown listener in the app range, runner down, gateway broken.
	withRuntimeEnv(t, "linux", runtimeTCPRow("00000000:4990", "9"), 2<<30)
	cfg, _, _ := newTestConfig(t)
	cfg.DataDir = "/var/lib/acornfox"
	cfg.Runner.(*fakeRunner).pingErr = errors.New("dial unix: connection refused")
	cfg.EdgeStatus = func() (string, string) { return reconcile.EdgeStateError, "Caddy 容器启动失败" }
	cfg.CheckPorts = hostcheck.Ports{Admin: 18808, LAN: 18802, AppMin: 18810, AppMax: 18899}
	v := runtimeGet(t, New(cfg))
	if v.OK {
		t.Fatalf("faults must make ok false: %+v", v)
	}
	want := map[string]string{"Docker：": "SETUP-SVC-01", "网关（Caddy）：": "SETUP-EDGE-01", "磁盘：": "SETUP-DISK-01", "端口：18832": "SETUP-PORT-05"}
	for p, code := range want {
		it := itemWith(v, p)
		if it == nil || it.Code != code {
			t.Fatalf("item %q: got %+v, want code %s (all: %+v)", p, it, code, v.Items)
		}
	}
	if !strings.Contains(itemWith(v, "端口：18832").Text, "sudo acornfox doctor") {
		t.Fatalf("unknown port must point to sudo acornfox doctor")
	}
}

// The disk check uses the data root Docker reports, like doctor does, so a
// Docker data root moved to another disk is checked on that disk.
func TestRuntimeChecksDiskUsesDockerRoot(t *testing.T) {
	withRuntimeEnv(t, "linux", "", 50<<30)
	dockerRoot := t.TempDir() // a data root moved to another disk
	runtimeDevice = func(p string) uint64 {
		if p == dockerRoot {
			return 9
		}
		return 7
	}
	runtimeFree = func(p string) (uint64, error) {
		if p == dockerRoot {
			return 1 << 30, nil
		}
		return 50 << 30, nil
	}
	cfg, _, _ := newTestConfig(t)
	cfg.DataDir = "/var/lib/acornfox"
	cfg.CheckPorts = hostcheck.Ports{Admin: 18808, LAN: 18802, AppMin: 18810, AppMax: 18899}
	cfg.Runner.(*fakeRunner).pingResp.DockerRootDir = dockerRoot
	v := runtimeGet(t, New(cfg))
	if it := itemWith(v, "磁盘：数据目录 /var/lib/acornfox 所在分区剩余 50.0 GB"); it == nil || it.Level != "ok" {
		t.Fatalf("data dir line: %+v", v.Items)
	}
	it := itemWith(v, "磁盘：Docker 数据目录 "+dockerRoot+" 所在分区只剩 1.0 GB")
	if it == nil || it.Code != "SETUP-DISK-01" || it.Level != "fail" {
		t.Fatalf("docker root on its own disk not checked: %+v", v.Items)
	}
	if strings.Contains(fmt.Sprint(v.Items), "/var/lib/docker") {
		t.Fatalf("must not name the default location when Docker reports another: %+v", v.Items)
	}

	// When Docker does not report its data root, the default is checked and
	// the line says it is only the default location.
	runtimeDevice = func(string) uint64 { return 7 }
	cfg.Runner.(*fakeRunner).pingResp.DockerRootDir = ""
	v = runtimeGet(t, New(cfg))
	if it := itemWith(v, "磁盘：数据目录 /var/lib/acornfox、Docker 数据目录（Docker 没告知，按默认位置） /var/lib/docker"); it == nil {
		t.Fatalf("default location must be marked: %+v", v.Items)
	}
}

func TestRuntimeChecksDesktopSkipsHostChecks(t *testing.T) {
	withRuntimeEnv(t, "darwin", runtimeTCPRow("00000000:4990", "9"), 1<<30)
	cfg, _, _ := newTestConfig(t)
	cfg.DataDir = "/Users/x/acornfox"
	cfg.CheckPorts = hostcheck.Ports{Admin: 18808, LAN: 18802, AppMin: 18810, AppMax: 18899}
	cfg.Runner.(*fakeRunner).pingErr = errors.New("no docker")
	v := runtimeGet(t, New(cfg))
	if itemWith(v, "磁盘") != nil || itemWith(v, "端口") != nil {
		t.Fatalf("disk and port checks are Linux-only: %+v", v.Items)
	}
	if it := itemWith(v, "Docker"); it == nil || it.Code != "SETUP-RT-03" {
		t.Fatalf("desktop docker fault = %+v", it)
	}
}

func TestStatusReportsVersion(t *testing.T) {
	cfg, _, _ := newTestConfig(t)
	cfg.Version = "0.5.0-dev.1"
	rec := do(t, New(cfg), "GET", "/v1/status", nil, nil)
	if !strings.Contains(rec.Body.String(), `"version":"0.5.0-dev.1"`) {
		t.Fatalf("status = %s", rec.Body.String())
	}
}

// The console entry shows whether the console is open on the LAN or the
// internet, but opening or closing stays on the trusted entry.
func TestConsoleEntryConsoleStatusReadOnly(t *testing.T) {
	cfg, _, _ := newTestConfig(t)
	cfg.LANConsole = &fakeLANConsole{enabled: true}
	cfg.PublicConsole = &fakePublicConsole{}
	trusted, console := New(cfg), NewConsole(cfg)
	cookie, csrf := login(t, trusted, console)
	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = "127.0.0.1:12345"
		req.AddCookie(cookie)
		req.Header.Set(csrfHeader, csrf)
		rec := httptest.NewRecorder()
		console.ServeHTTP(rec, req)
		return rec
	}
	if rec := call("GET", "/v1/lan-console"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("GET lan-console on console entry = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call("GET", "/v1/public-console"); rec.Code != 200 {
		t.Fatalf("GET public-console on console entry = %d", rec.Code)
	}
	for _, m := range [][2]string{{"POST", "/v1/lan-console"}, {"DELETE", "/v1/lan-console"}, {"POST", "/v1/public-console/enable"}, {"DELETE", "/v1/public-console"}} {
		if rec := call(m[0], m[1]); rec.Code == 200 || rec.Code == 201 {
			t.Fatalf("%s %s must not work on the console entry (got %d)", m[0], m[1], rec.Code)
		}
	}
}
