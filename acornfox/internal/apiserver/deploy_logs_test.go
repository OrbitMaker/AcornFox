package apiserver

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

func linesOf(t *testing.T, m map[string]any) []string {
	t.Helper()
	raw, _ := m["lines"].([]any)
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, l.(string))
	}
	return out
}

func TestDeploymentLogsArchiveLiveAndRedaction(t *testing.T) {
	rs := newRealServer(t)
	ctx := context.Background()
	old := rs.add("sha256:a", state.StatusLive, 0)
	live := rs.add("sha256:b", state.StatusLive, 0)

	// The archive was written before the secret existed; reading masks it
	// with the current rule, URL credentials included.
	archived := "2026-11-03T02:10:31Z token=supersecret1\n2026-11-03T02:10:32Z pip https://u:p4ss@mirror.example/simple\n"
	if err := rs.logs.Write("web", old.ID, deploylog.KindRun, []byte(archived), deploylog.NewRedactor()); err != nil {
		t.Fatal(err)
	}
	if err := rs.logs.Write("web", old.ID, deploylog.KindBuild, []byte("Step 1/3 : FROM node\nStep 2/3 : RUN npm ci\n"), deploylog.NewRedactor()); err != nil {
		t.Fatal(err)
	}
	if err := rs.st.SetEnv(ctx, state.EnvVar{App: "web", Key: "TOKEN", Value: "supersecret1", Secret: true}); err != nil {
		t.Fatal(err)
	}

	status, m := rs.get("/v1/deployments/" + old.ID + "/logs")
	lines := linesOf(t, m)
	if status != 200 || m["source"] != "archive" || m["kind"] != "run" || m["version"] != 1.0 || m["deployment_id"] != old.ID || len(lines) != 2 {
		t.Fatalf("archive run: %d %v", status, m)
	}
	if strings.Contains(strings.Join(lines, "\n"), "supersecret1") || strings.Contains(strings.Join(lines, "\n"), "p4ss") ||
		!strings.Contains(lines[0], "token=******") || !strings.Contains(lines[1], "https://******@mirror.example") {
		t.Fatalf("not redacted: %q", lines)
	}
	status, m = rs.get("/v1/deployments/" + old.ID + "/logs?kind=build&tail=1")
	if lines := linesOf(t, m); status != 200 || len(lines) != 1 || lines[0] != "Step 2/3 : RUN npm ci" || m["truncated"] != true {
		t.Fatalf("archive build tail=1: %d %v", status, m)
	}

	// The serving version is read from its container.
	rs.run.logLines[runner.ContainerName("web", live.ID)] = []string{"listening", "secret supersecret1"}
	status, m = rs.get("/v1/deployments/" + live.ID + "/logs?tail=10")
	if lines := linesOf(t, m); status != 200 || m["source"] != "live" || len(lines) != 2 || lines[1] != "secret ******" || m["truncated"] != false {
		t.Fatalf("live: %d %v", status, m)
	}
	// Its container gone and nothing archived: not kept.
	rs.run.logsErr = runner.ErrNotFound
	if status, m = rs.get("/v1/deployments/" + live.ID + "/logs"); status != 404 || errCode(m) != "logs_not_kept" {
		t.Fatalf("live container gone: %d %v", status, m)
	}
	rs.run.logsErr = errRunnerDown
	if status, m = rs.get("/v1/deployments/" + live.ID + "/logs"); status != 503 || errCode(m) != "runner_unavailable" {
		t.Fatalf("runner down: %d %v", status, m)
	}
}

func TestDeploymentLogsErrors(t *testing.T) {
	rs := newRealServer(t)
	// Deployed after log keeping started, but nothing was archived.
	before := rs.add("sha256:a", state.StatusLive, 0)
	reuse := rs.add("sha256:a", state.StatusLive, 1)
	queued := rs.add("", state.StatusQueued, 0)

	for _, c := range []struct {
		path   string
		status int
		code   string
		msg    string
	}{
		{"/v1/deployments/" + reuse.ID + "/logs?kind=build", 404, "logs_not_kept", "第 2 版复用第 1 版的程序，没有重新构建"},
		{"/v1/deployments/" + before.ID + "/logs", 404, "logs_not_kept", "第 1 版的日志已超出保留范围，已删除"},
		{"/v1/deployments/" + queued.ID + "/logs?kind=build", 409, "logs_pending", "第 3 版还在构建，构建结束后才能看构建日志"},
		{"/v1/deployments/" + queued.ID + "/logs", 409, "logs_pending", ""},
		{"/v1/deployments/" + before.ID + "/logs?kind=stdout", 400, "invalid_kind", ""},
		{"/v1/deployments/" + before.ID + "/logs?tail=0", 400, "invalid_tail", ""},
		{"/v1/deployments/" + before.ID + "/logs?tail=x", 400, "invalid_tail", ""},
		{"/v1/deployments/NOT-AN-ID/logs", 400, "invalid_deployment", ""},
		{"/v1/deployments/0123456789ab/logs", 404, "not_found", ""},
	} {
		status, m := rs.get(c.path)
		if status != c.status || errCode(m) != c.code {
			t.Fatalf("%s: %d %v, want %d %s", c.path, status, m, c.status, c.code)
		}
		if c.msg != "" && m["error"].(map[string]any)["message"] != c.msg {
			t.Fatalf("%s: message %v, want %q", c.path, m["error"], c.msg)
		}
	}
}

func TestDeploymentLogsBeforeFeature(t *testing.T) {
	// Log keeping started after the deployment: its .since is later.
	_, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")
	d, _, _ := st.CreateDeployment(ctx, state.NewDeployment{App: "web", SourceKind: state.SourceUpload, SourceRef: "/x", SourceDigest: "x"})
	st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error { x.Status = state.StatusRetired; x.ImageID = "sha256:a"; return nil })
	root := filepath.Join(t.TempDir(), "deployments")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	since := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if err := os.WriteFile(filepath.Join(root, ".since"), []byte(since+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logs, err := deploylog.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if !logs.Since().After(d.CreatedAt) {
		t.Fatalf("since %v not after %v", logs.Since(), d.CreatedAt)
	}
	h := New(Config{Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(), HostMetrics: fakeHostSized(8192, 4), DeployLogs: logs})
	w := do(t, h, http.MethodGet, "/v1/deployments/"+d.ID+"/logs", nil, nil)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "第 1 版部署时还没有保存日志的功能") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}

	// Without a log store every log is not kept and no view claims one.
	h = New(Config{Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(), HostMetrics: fakeHostSized(8192, 4)})
	w = do(t, h, http.MethodGet, "/v1/deployments/"+d.ID+"/logs", nil, nil)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "logs_not_kept") {
		t.Fatalf("no store: %d %s", w.Code, w.Body)
	}
	w = do(t, h, http.MethodGet, "/v1/apps/web/deployments", nil, nil)
	if !strings.Contains(w.Body.String(), `"logs":{"build":false,"run":false}`) {
		t.Fatalf("no store view: %s", w.Body)
	}
}

func TestDeploymentLogsFailClosed(t *testing.T) {
	_, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	logs, err := deploylog.Open(filepath.Join(t.TempDir(), "deployments"))
	if err != nil {
		t.Fatal(err)
	}
	h := New(Config{Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(), HostMetrics: fakeHostSized(8192, 4), DeployLogs: logs})
	st.EnsureApp(ctx, "web")
	d, _, _ := st.CreateDeployment(ctx, state.NewDeployment{App: "web", SourceKind: state.SourceUpload, SourceRef: "/x", SourceDigest: "x"})
	st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error { x.Status = state.StatusRetired; return nil })
	if err := logs.Write("web", d.ID, deploylog.KindRun, []byte("hello\n"), deploylog.NewRedactor()); err != nil {
		t.Fatal(err)
	}
	st.addons = map[string]state.Addon{"web/redis": {App: "web", Kind: state.AddonRedis, Credentials: []byte("invalid")}}
	w := do(t, h, http.MethodGet, "/v1/deployments/"+d.ID+"/logs", nil, nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "redaction_unavailable") || strings.Contains(w.Body.String(), "hello") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestDeleteAppRemovesDeploymentLogs(t *testing.T) {
	rs := newRealServer(t)
	d := rs.add("sha256:a", state.StatusLive, 0)
	if err := rs.logs.Write("web", d.ID, deploylog.KindBuild, []byte("x\n"), deploylog.NewRedactor()); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(rs.logs.Root(), "web")
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
	w := do(t, rs.h, http.MethodDelete, "/v1/apps/web", nil, nil)
	if w.Code != 200 {
		t.Fatalf("delete: %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("log dir still there: %v", err)
	}
}
