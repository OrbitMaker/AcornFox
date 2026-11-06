package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// withDeployLogs gives the harness a deploy log store in a temp dir.
func withDeployLogs(t *testing.T, h *harness) *deploylog.Store {
	t.Helper()
	ls, err := deploylog.Open(filepath.Join(t.TempDir(), "logs", "deployments"))
	if err != nil {
		t.Fatal(err)
	}
	h.rec.cfg.DeployLogs = ls
	return ls
}

func readLog(t *testing.T, ls *deploylog.Store, app, id string, kind deploylog.Kind) string {
	t.Helper()
	lines, _, err := ls.Read(app, id, kind, deploylog.MaxTail)
	if err != nil {
		t.Fatalf("read %s log of %s: %v", kind, id, err)
	}
	return strings.Join(lines, "\n")
}

func hasEvent(h *harness, sub string) bool {
	for _, m := range h.store.eventMessages() {
		if strings.Contains(m, sub) {
			return true
		}
	}
	return false
}

// liveWeb puts app web with a live deployment (seq 1) and its running container.
func liveWeb(h *harness, id string) {
	app := baseApp("web")
	app.CurrentDeployment = id
	h.store.putApp(app)
	img := "sha256:web-" + id
	h.store.putDeployment(state.Deployment{ID: id, App: "web", Seq: 1, SourceKind: state.SourceUpload, Status: state.StatusLive, ImageID: img})
	h.runner.images[img] = runner.ImageInfo{ID: img, App: "web", DeploymentID: id, ExposedPorts: []int{8080}}
	name := runner.ContainerName("web", id)
	h.runner.containers[name] = runner.ContainerInfo{ID: "c-" + id, Name: name, App: "web", DeploymentID: id, Role: runner.RoleApp, State: "running", Running: true, HostPort: 41000}
}

func TestBuildLogSavedForUploadBuilds(t *testing.T) {
	cases := []struct {
		name   string
		resp   runner.BuildResponse
		status string
		want   []string
	}{
		{"success with output", runner.BuildResponse{OK: true, Log: "Step 1/2 : FROM alpine\nStep 2/2 : RUN echo hi\n"}, state.StatusLive, []string{"Step 1/2 : FROM alpine", "Step 2/2 : RUN echo hi"}},
		{"image already existed", runner.BuildResponse{OK: true}, state.StatusLive, []string{buildLogImageExists}},
		{"failure with output", runner.BuildResponse{OK: false, Log: "Step 2/2 : RUN npm ci\nnpm ERR! missing\n", Failure: &runner.Failure{Stage: "build", Code: "build_failed", Message: "构建失败"}}, state.StatusFailed, []string{"npm ERR! missing"}},
		{"rejected upload", runner.BuildResponse{OK: false, Failure: &runner.Failure{Stage: "upload", Code: "dockerfile_missing", Message: "缺少 Dockerfile"}}, state.StatusFailed, []string{"缺少 Dockerfile"}},
		{"old runner failure", runner.BuildResponse{OK: false}, state.StatusFailed, []string{buildLogNoOutput}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			ls := withDeployLogs(t, h)
			h.store.putApp(baseApp("web"))
			h.store.putDeployment(queued("web", "b00000000001", 1))
			h.runner.buildResp["b00000000001"] = tc.resp
			d := h.driveToTerminal(t, "web", "b00000000001", 10)
			if d.Status != tc.status {
				t.Fatalf("status %s, want %s (diag=%+v)", d.Status, tc.status, d.Diagnosis)
			}
			got := readLog(t, ls, "web", "b00000000001", deploylog.KindBuild)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Fatalf("build log %q lacks %q", got, w)
				}
			}
		})
	}
}

func TestBuildLogIsRedacted(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	h.store.env["web"] = []state.EnvVar{{App: "web", Key: "API_KEY", Value: "topsecret123", Secret: true}}
	h.store.putDeployment(queued("web", "b00000000002", 1))
	h.runner.buildResp["b00000000002"] = runner.BuildResponse{OK: true, Log: "echo topsecret123\npip install -i https://user:pa55w0rd@pypi.example.com/simple x\n"}
	if d := h.driveToTerminal(t, "web", "b00000000002", 10); d.Status != state.StatusLive {
		t.Fatalf("want live, got %s", d.Status)
	}
	got := readLog(t, ls, "web", "b00000000002", deploylog.KindBuild)
	if strings.Contains(got, "topsecret123") || strings.Contains(got, "pa55w0rd") {
		t.Fatalf("secret leaked into build log: %q", got)
	}
	if !strings.Contains(got, "https://******@pypi.example.com") {
		t.Fatalf("URL credentials not masked: %q", got)
	}
}

func TestBuildLogNotSavedWithoutRedactionConfig(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	h.store.removedErr = errors.New("db locked")
	h.store.putDeployment(queued("web", "b00000000003", 4))
	h.runner.buildResp["b00000000003"] = runner.BuildResponse{OK: true, Log: "Step 1/1 : FROM alpine\n"}
	if d := h.driveToTerminal(t, "web", "b00000000003", 10); d.Status != state.StatusLive {
		t.Fatalf("a log that cannot be redacted must not block the deployment, got %s", d.Status)
	}
	if b, _ := ls.Has("web", "b00000000003"); b {
		t.Fatal("build log written without redaction config")
	}
	if !hasEvent(h, "第 4 版的日志没有保存：读不到脱敏配置") {
		t.Fatalf("missing event, got %v", h.store.eventMessages())
	}
}

func TestGitBuildLogKeepsGitOutput(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.rec.cfg.Git = &fakeGit{cloneOutput: "Cloning into 'src'...\n"}
	h.store.putApp(baseApp("web"))
	h.store.putDeployment(gitDeployment("web", "a10000000001", 1, "https://example.com/repo.git#main"))
	h.runner.buildResp["a10000000001"] = runner.BuildResponse{OK: true, Log: "Step 1/1 : FROM scratch\n"}
	if d := h.driveToTerminal(t, "web", "a10000000001", 10); d.Status != state.StatusLive {
		t.Fatalf("want live, got %s", d.Status)
	}
	got := readLog(t, ls, "web", "a10000000001", deploylog.KindBuild)
	if !strings.Contains(got, "Cloning into 'src'...\nStep 1/1 : FROM scratch") {
		t.Fatalf("git output must precede the build output: %q", got)
	}
}

func TestGitBuildLogKeepsLast200GitLinesOnFailure(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	var out strings.Builder
	for i := 1; i <= 250; i++ {
		fmt.Fprintf(&out, "line %d\n", i)
	}
	out.WriteString("fatal: repository not found\n")
	h.rec.cfg.Git = &fakeGit{cloneErr: errors.New("exit 128"), cloneOutput: out.String()}
	h.store.putApp(baseApp("web"))
	h.store.putDeployment(gitDeployment("web", "a10000000002", 1, "https://example.com/missing.git"))
	if d := h.driveToTerminal(t, "web", "a10000000002", 5); d.Status != state.StatusFailed {
		t.Fatalf("want failed, got %s", d.Status)
	}
	lines, _, err := ls.Read("web", "a10000000002", deploylog.KindBuild, deploylog.MaxTail)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != gitLogLines || lines[0] != "line 52" || lines[len(lines)-1] != "fatal: repository not found" {
		t.Fatalf("want the last %d git lines, got %d lines starting %q", gitLogLines, len(lines), lines[0])
	}
}

func TestImagePullWritesOneLineAndReuseWritesNone(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	h.store.putDeployment(imageDeployment("web", "c10000000001", 1, "nginx:1.27-alpine"))
	d := h.driveToTerminal(t, "web", "c10000000001", 10)
	if d.Status != state.StatusLive {
		t.Fatalf("want live, got %s", d.Status)
	}
	if got := readLog(t, ls, "web", "c10000000001", deploylog.KindBuild); got != "拉取 nginx:1.27-alpine" {
		t.Fatalf("pull build log = %q", got)
	}

	// A rollback reuses an image by ID: no build log.
	rb := imageDeployment("web", "c10000000002", 2, d.ImageID)
	rb.Reason = state.ReasonRollback
	rb.BasedOnSeq = 1
	h.store.putDeployment(rb)
	if d2 := h.driveToTerminal(t, "web", "c10000000002", 10); d2.Status != state.StatusLive {
		t.Fatalf("rollback want live, got %s (diag=%+v)", d2.Status, d2.Diagnosis)
	}
	if b, _ := ls.Has("web", "c10000000002"); b {
		t.Fatal("a reused image must not get a build log")
	}
}

func TestImageIDSourceMissingFailsImageGone(t *testing.T) {
	h := newHarness(t)
	h.store.putApp(baseApp("web"))
	d := imageDeployment("web", "e10000000001", 5, "sha256:collected")
	d.Reason = state.ReasonRollback
	h.store.putDeployment(d)
	final := h.driveToTerminal(t, "web", "e10000000001", 5)
	if final.Status != state.StatusFailed || final.Diagnosis == nil ||
		final.Diagnosis.Stage != "image" || final.Diagnosis.Code != "image_gone" {
		t.Fatalf("want failed image/image_gone, got %s %+v", final.Status, final.Diagnosis)
	}
	if !strings.Contains(final.Diagnosis.Message+final.Diagnosis.Hint, "请重新部署那一版的代码") {
		t.Fatalf("diagnosis text: %+v", final.Diagnosis)
	}
	h.runner.mu.Lock()
	pulls := len(h.runner.pullReqs)
	h.runner.mu.Unlock()
	if pulls != 0 {
		t.Fatalf("an image ID must never be pulled, got %d pulls", pulls)
	}
}

func TestBuildResponseTooLargeFailsWithoutRetry(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	h.store.putDeployment(queued("web", "d10000000001", 1))
	h.runner.buildErr["d10000000001"] = fmt.Errorf("decode: %w", runner.ErrResponseTooLarge)
	h.rec.round(context.Background(), "web")
	d := h.store.getDeployment("d10000000001")
	if d.Status != state.StatusFailed || d.Diagnosis == nil ||
		d.Diagnosis.Stage != "build" || d.Diagnosis.Code != "runner_response_too_large" {
		t.Fatalf("want failed build/runner_response_too_large after one round, got %s %+v", d.Status, d.Diagnosis)
	}
	if !strings.Contains(d.Diagnosis.Message+d.Diagnosis.Hint, "请减少构建时的输出后重新部署") {
		t.Fatalf("diagnosis text: %+v", d.Diagnosis)
	}
	if d.Attempts != 1 {
		t.Fatalf("attempt must not be given back, attempts=%d", d.Attempts)
	}
	h.rec.round(context.Background(), "web")
	if n := atomic.LoadInt32(&h.runner.buildCount); n != 1 {
		t.Fatalf("want exactly one build, got %d", n)
	}
	if got := readLog(t, ls, "web", "d10000000001", deploylog.KindBuild); !strings.Contains(got, buildLogTooLarge) {
		t.Fatalf("build log = %q", got)
	}
}

func TestRunLogSavedWhenReplacedWithOldSecretsRedacted(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	liveWeb(h, "0d0000000001")
	oldName := runner.ContainerName("web", "0d0000000001")
	// Current configuration: API_KEY changed, OLD_TOKEN removed, one database.
	h.store.env["web"] = []state.EnvVar{
		{App: "web", Key: "API_KEY", Value: "new-key-value", Secret: true},
		{App: "web", Key: "MODE", Value: "production"},
	}
	seedAddon(t, h.store, "web", state.AddonPostgres)
	h.runner.tailEnv[oldName] = []string{
		"API_KEY=old-key-value", "OLD_TOKEN=gone-token-1", "DATABASE_URL=postgres://u:dbpass99@db/x",
		"PATH=/usr/local/bin:/usr/bin", "MODE=production",
	}
	h.runner.tailData[oldName] = []byte(
		"2026-11-03T02:10:29.000000000Z start with old-key-value\n" +
			"2026-11-03T02:10:30.000000000Z token gone-token-1\n" +
			"2026-11-03T02:10:31.000000000Z db postgres://u:dbpass99@db/x mode production\n")

	h.store.putDeployment(queued("web", "0e0000000002", 2))
	if d := h.driveToTerminal(t, "web", "0e0000000002", 10); d.Status != state.StatusLive {
		t.Fatalf("want new live, got %s (diag=%+v)", d.Status, d.Diagnosis)
	}
	if h.runner.hasContainer(oldName) {
		t.Fatal("old container must be removed after its log is saved")
	}
	got := readLog(t, ls, "web", "0d0000000001", deploylog.KindRun)
	for _, leaked := range []string{"old-key-value", "gone-token-1", "dbpass99"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("run log leaks %q: %q", leaked, got)
		}
	}
	if !strings.Contains(got, "2026-11-03T02:10:29.000000000Z start with ******") || !strings.Contains(got, "mode production") {
		t.Fatalf("run log content: %q", got)
	}
}

func TestFailedHealthCheckExcerptComesFromSavedLog(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	h.store.env["web"] = []state.EnvVar{{App: "web", Key: "DB_PASSWORD", Value: "supersecret", Secret: true}}
	d := queued("web", "f00000000001", 6)
	d.Status = state.StatusChecking
	d.ImageID = "sha256:web-f00000000001"
	name := runner.ContainerName("web", "f00000000001")
	h.runner.containers[name] = runner.ContainerInfo{ID: "c", Name: name, App: "web", DeploymentID: "f00000000001", Role: runner.RoleApp, State: "exited", RestartCount: 1, HostPort: 42000}
	var data strings.Builder
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&data, "2026-11-03T02:10:%02d.000000000Z \x1b[31mline %d\x1b[0m\n", i%60, i)
	}
	data.WriteString("2026-11-03T02:11:00.000000000Z Error: password supersecret rejected\n")
	h.runner.tailData[name] = []byte(data.String())
	h.runner.logs[name] = []string{"from the old path"} // must not be used
	h.store.putDeployment(d)

	final := h.driveToTerminal(t, "web", "f00000000001", 5)
	if final.Status != state.StatusFailed || final.Diagnosis.Code != "container_exited" {
		t.Fatalf("want failed container_exited, got %s %+v", final.Status, final.Diagnosis)
	}
	ex := final.Diagnosis.LogExcerpt
	lines := strings.Split(ex, "\n")
	if len(lines) != runner.MaxLogExcerptLine || lines[0] != "line 22" || lines[len(lines)-1] != "Error: password ****** rejected" {
		t.Fatalf("excerpt must be the redacted last 40 lines without timestamps or colors, got %d lines: %q", len(lines), ex)
	}
	if h.runner.hasContainer(name) {
		t.Fatal("failed container must be removed")
	}
	run := readLog(t, ls, "web", "f00000000001", deploylog.KindRun)
	if strings.Contains(run, "supersecret") || !strings.Contains(run, "line 1") {
		t.Fatalf("run log: %q", run)
	}
}

func TestFailureExcerptFallsBackWhenNothingCaptured(t *testing.T) {
	h := newHarness(t)
	withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	d := queued("web", "f00000000002", 1)
	d.Status = state.StatusChecking
	d.ImageID = "sha256:web-f00000000002"
	name := runner.ContainerName("web", "f00000000002")
	h.runner.containers[name] = runner.ContainerInfo{ID: "c", Name: name, App: "web", DeploymentID: "f00000000002", Role: runner.RoleApp, State: "exited", RestartCount: 1, HostPort: 42000}
	h.runner.tailErr[name] = errors.New("socket closed")
	h.runner.logs[name] = []string{"fallback line"}
	h.store.putDeployment(d)

	final := h.driveToTerminal(t, "web", "f00000000002", 5)
	if final.Diagnosis == nil || final.Diagnosis.LogExcerpt != "fallback line" {
		t.Fatalf("want the tailLogs excerpt, got %+v", final.Diagnosis)
	}
	if !hasEvent(h, "第 1 版的运行日志没有保存：读取失败") {
		t.Fatalf("missing event, got %v", h.store.eventMessages())
	}
	if h.runner.hasContainer(name) {
		t.Fatal("container must still be removed")
	}
}

// tailless hides the fake runner's LogTail, like an older runner.
type tailless struct{ runner.API }

func TestRunnerWithoutLogTailStillRemovesAndWarnsOnce(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	var buf bytes.Buffer
	h.rec.log = slog.New(slog.NewTextHandler(&buf, nil))
	h.rec.cfg.Runner = tailless{h.runner}
	liveWeb(h, "0d0000000001")

	h.store.putDeployment(queued("web", "0e0000000002", 2))
	if d := h.driveToTerminal(t, "web", "0e0000000002", 10); d.Status != state.StatusLive {
		t.Fatalf("want live, got %s", d.Status)
	}
	h.store.putDeployment(queued("web", "0e0000000003", 3))
	if d := h.driveToTerminal(t, "web", "0e0000000003", 10); d.Status != state.StatusLive {
		t.Fatalf("want live, got %s", d.Status)
	}
	for _, id := range []string{"0d0000000001", "0e0000000002"} {
		if h.runner.hasContainer(runner.ContainerName("web", id)) {
			t.Fatalf("container of %s not removed", id)
		}
		if _, run := ls.Has("web", id); run {
			t.Fatalf("no run log expected for %s", id)
		}
	}
	if n := strings.Count(buf.String(), "runner 版本较旧，暂不保存运行日志"); n != 1 {
		t.Fatalf("want the warning once, got %d", n)
	}
}

func TestUnsupportedLogTailErrorIsTreatedAsOldRunner(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	liveWeb(h, "0d0000000001")
	h.runner.tailErr[runner.ContainerName("web", "0d0000000001")] = errors.Join(runner.ErrUnsupported, errors.New("404"))
	h.store.putDeployment(queued("web", "0e0000000002", 2))
	if d := h.driveToTerminal(t, "web", "0e0000000002", 10); d.Status != state.StatusLive {
		t.Fatalf("want live, got %s", d.Status)
	}
	if _, run := ls.Has("web", "0d0000000001"); run {
		t.Fatal("no run log expected")
	}
	if hasEvent(h, "运行日志没有保存") {
		t.Fatal("an old runner is not a per-deployment failure event")
	}
}

func TestSupersededDeploymentKeepsItsRunLog(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	// d1 got as far as checking (has a container); d2 supersedes it.
	d1 := queued("web", "500000000001", 1)
	d1.Status = state.StatusChecking
	d1.ImageID = "sha256:web-500000000001"
	name := runner.ContainerName("web", "500000000001")
	h.runner.containers[name] = runner.ContainerInfo{ID: "c", Name: name, App: "web", DeploymentID: "500000000001", Role: runner.RoleApp, State: "running", Running: true, HostPort: 42000}
	h.runner.logs[name] = []string{"booting"}
	h.store.putDeployment(d1)
	h.store.putDeployment(queued("web", "500000000002", 2))
	h.store.putDeployment(queued("web", "500000000003", 3)) // never had a container

	if d := h.driveToTerminal(t, "web", "500000000003", 10); d.Status != state.StatusLive {
		t.Fatalf("want newest live, got %s", d.Status)
	}
	if s := h.store.getDeployment("500000000001").Status; s != state.StatusSuperseded {
		t.Fatalf("d1 want superseded, got %s", s)
	}
	if got := readLog(t, ls, "web", "500000000001", deploylog.KindRun); !strings.Contains(got, "booting") {
		t.Fatalf("superseded run log = %q", got)
	}
	if _, run := ls.Has("web", "500000000002"); run {
		t.Fatal("a deployment without a container has no run log")
	}
	if hasEvent(h, "运行日志没有保存") {
		t.Fatalf("a missing container is not a failure: %v", h.store.eventMessages())
	}
	if h.runner.hasContainer(name) {
		t.Fatal("superseded container must be removed")
	}
}

func TestRouteFailureSavesRunLog(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	d := queued("web", "7e0000000002", 1)
	d.Status = state.StatusRouting
	d.ImageID = "sha256:web-7e0000000002"
	name := runner.ContainerName("web", "7e0000000002")
	h.runner.containers[name] = runner.ContainerInfo{ID: "c", Name: name, App: "web", DeploymentID: "7e0000000002", Role: runner.RoleApp, State: "running", Running: true, HostPort: 42000}
	h.runner.logs[name] = []string{"listening on 8080"}
	h.store.putDeployment(d)
	h.router.mu.Lock()
	h.router.syncErr = fmt.Errorf("caddy down")
	h.router.mu.Unlock()
	h.rec.round(context.Background(), "web")
	h.clock.Advance(6 * time.Minute)
	h.rec.round(context.Background(), "web")
	if final := h.store.getDeployment("7e0000000002"); final.Status != state.StatusFailed {
		t.Fatalf("want failed, got %s", final.Status)
	}
	if got := readLog(t, ls, "web", "7e0000000002", deploylog.KindRun); !strings.Contains(got, "listening on 8080") {
		t.Fatalf("run log = %q", got)
	}
}

func TestRecreateLiveRecordsLostLogs(t *testing.T) {
	h := newHarness(t)
	withDeployLogs(t, h)
	liveWeb(h, "1e0000000001")
	delete(h.runner.containers, runner.ContainerName("web", "1e0000000001"))
	h.rec.round(context.Background(), "web")
	if !h.runner.hasContainer(runner.ContainerName("web", "1e0000000001")) {
		t.Fatal("live container not recreated")
	}
	if !hasEvent(h, "容器丢失，之前的日志没有保存") {
		t.Fatalf("missing event, got %v", h.store.eventMessages())
	}
}

func TestNoDeployLogsKeepsOldBehaviour(t *testing.T) {
	h := newHarness(t)
	liveWeb(h, "0d0000000001")
	h.store.putDeployment(queued("web", "0e0000000002", 2))
	if d := h.driveToTerminal(t, "web", "0e0000000002", 10); d.Status != state.StatusLive {
		t.Fatalf("want live, got %s", d.Status)
	}
	h.runner.mu.Lock()
	calls := len(h.runner.tailCalls)
	h.runner.mu.Unlock()
	if calls != 0 {
		t.Fatalf("LogTail must not be called without DeployLogs, got %d calls", calls)
	}
	if hasEvent(h, "日志没有保存") {
		t.Fatal("no log events without DeployLogs")
	}
}

func TestImageGCKeepsImageOfQueuedRollback(t *testing.T) {
	h := newHarness(t)
	app := baseApp("web")
	// n live; the KeepVersions before it are kept; 1 and 2 are outside.
	n := state.KeepVersions + 3
	app.CurrentDeployment = fmt.Sprintf("%012x", n)
	h.store.putApp(app)
	for seq := 1; seq <= n; seq++ {
		id := fmt.Sprintf("%012x", seq)
		status := state.StatusRetired
		if seq == n {
			status = state.StatusLive
		}
		img := fmt.Sprintf("sha256:image-%d", seq)
		h.store.putDeployment(state.Deployment{ID: id, App: "web", Seq: seq, Status: status, ImageID: img})
		h.runner.images[img] = runner.ImageInfo{ID: img, App: "web", DeploymentID: id}
	}
	// A queued rollback to version 1 (outside the window) names its image only
	// in source_ref; GC must not collect it before the rollback resolves it.
	rb := imageDeployment("web", fmt.Sprintf("%012x", n+1), n+1, "sha256:image-1")
	rb.Reason = state.ReasonRollback
	h.store.putDeployment(rb)
	h.rec.appGC(context.Background(), "web", []state.Deployment{rb})
	if _, ok := h.runner.images["sha256:image-1"]; !ok {
		t.Fatal("image of a queued rollback was collected")
	}
	if _, ok := h.runner.images["sha256:image-2"]; ok {
		t.Fatal("image outside the window must be collected")
	}
}

func TestImageGCCountsDistinctImages(t *testing.T) {
	h := newHarness(t)
	app := baseApp("web")
	// n live; before it KeepVersions distinct images with two records each (a
	// deploy and its redeploy), more records than the window holds.
	n := 2*state.KeepVersions + 1
	app.CurrentDeployment = fmt.Sprintf("%012x", n)
	h.store.putApp(app)
	for seq := 1; seq <= n; seq++ {
		id := fmt.Sprintf("%012x", seq)
		status := state.StatusRetired
		if seq == n {
			status = state.StatusLive
		}
		img := fmt.Sprintf("sha256:img-%d", (seq+1)/2)
		h.store.putDeployment(state.Deployment{ID: id, App: "web", Seq: seq, Status: status, ImageID: img})
		h.runner.images[img] = runner.ImageInfo{ID: img, App: "web", DeploymentID: id}
	}
	h.rec.appGC(context.Background(), "web", nil)
	for i := 1; i <= state.KeepVersions+1; i++ {
		if _, ok := h.runner.images[fmt.Sprintf("sha256:img-%d", i)]; !ok {
			t.Fatalf("image %d must be kept (KeepVersions distinct retired images plus live)", i)
		}
	}
}

func TestPruneAppLogsKeepsProtected(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	app := baseApp("web")
	app.CurrentDeployment = fmt.Sprintf("%012x", 30)
	h.store.putApp(app)
	red := deploylog.NewRedactor()
	// 30 live; 6..29 redeploys of the live image; 5 and 4 the two other kept
	// images, 2 an older record of 4's image; 3 the newest failure, 1 an older one.
	for seq := 1; seq <= 30; seq++ {
		id := fmt.Sprintf("%012x", seq)
		status := state.StatusRetired
		img := "sha256:img-live"
		switch seq {
		case 30:
			status = state.StatusLive
		case 5:
			img = "sha256:img-5"
		case 4, 2:
			img = "sha256:img-old"
		case 3, 1:
			status = state.StatusFailed
			img = fmt.Sprintf("sha256:img-%d", seq)
		}
		h.store.putDeployment(state.Deployment{ID: id, App: "web", Seq: seq, Status: status, ImageID: img})
		if err := ls.Write("web", id, deploylog.KindRun, []byte("x\n"), red); err != nil {
			t.Fatal(err)
		}
	}
	h.rec.pruneAppLogs(context.Background(), "web")
	kept := func(seq int) bool {
		_, run := ls.Has("web", fmt.Sprintf("%012x", seq))
		return run
	}
	for seq := 11; seq <= 30; seq++ {
		if !kept(seq) {
			t.Fatalf("recent version %d pruned", seq)
		}
	}
	for _, seq := range []int{2, 3, 4, 5} {
		if !kept(seq) {
			t.Fatalf("protected version %d pruned (rollback target, older record of a kept image, or newest failure)", seq)
		}
	}
	for _, seq := range []int{1, 6, 7, 8, 9, 10} {
		if kept(seq) {
			t.Fatalf("version %d should have been pruned", seq)
		}
	}
}

func TestGlobalPruneRemovesOrphanAppLogs(t *testing.T) {
	h := newHarness(t)
	ls := withDeployLogs(t, h)
	h.store.putApp(baseApp("web"))
	h.store.putDeployment(state.Deployment{ID: "aaaaaaaaaaaa", App: "web", Seq: 1, Status: state.StatusFailed})
	red := deploylog.NewRedactor()
	for _, app := range []string{"web", "ghost"} {
		if err := ls.Write(app, "aaaaaaaaaaaa", deploylog.KindBuild, []byte("x\n"), red); err != nil {
			t.Fatal(err)
		}
	}
	h.rec.globalGC(context.Background(), []string{"web"})
	if _, err := os.Stat(filepath.Join(ls.Root(), "ghost")); !os.IsNotExist(err) {
		t.Fatalf("orphan app logs must be removed, stat err=%v", err)
	}
	if b, _ := ls.Has("web", "aaaaaaaaaaaa"); !b {
		t.Fatal("logs of an existing app must stay")
	}
}

func TestRunExcerpt(t *testing.T) {
	long := strings.Repeat("é", excerptLineMax) // 2 bytes per rune
	in := "2026-11-03T02:10:31.123456789Z \x1b[1mhello\x1b[0m\r\nno stamp here\n2026-11-03T02:10:32Z " + long + "\n"
	got := strings.Split(runExcerpt(in), "\n")
	if got[0] != "hello" || got[1] != "no stamp here" {
		t.Fatalf("excerpt = %q", got)
	}
	if !strings.HasSuffix(got[2], "…（本行已截断）") || len(got[2]) > excerptLineMax+len("…（本行已截断）") {
		t.Fatalf("long line not capped: %d bytes", len(got[2]))
	}
	if runExcerpt("") != "" || runExcerpt("\n") != "" {
		t.Fatal("empty input must give an empty excerpt")
	}
}
