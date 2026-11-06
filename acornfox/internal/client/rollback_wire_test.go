package client_test

// End-to-end wire tests for rollback to a version, the version list and
// deployment logs: the real client against the real API server and store.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/apiserver"
	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// imageRunner reports every image as present and serves fixed live logs.
type imageRunner struct {
	wireRunner
	images []runner.ImageInfo
	logs   []string
}

func (r *imageRunner) ListImages(context.Context, string) ([]runner.ImageInfo, error) {
	return r.images, nil
}
func (r *imageRunner) Logs(context.Context, string, string, int) ([]string, error) {
	return r.logs, nil
}

func seed(t *testing.T, st *state.Store, n int, image, status string) state.Deployment {
	t.Helper()
	ctx := context.Background()
	d, _, err := st.CreateDeployment(ctx, state.NewDeployment{App: "web", SourceKind: state.SourceUpload, SourceRef: "/u", SourceDigest: fmt.Sprintf("d%d", n)})
	if err != nil {
		t.Fatal(err)
	}
	next := status
	if status == state.StatusLive {
		next = state.StatusRouting
	}
	if _, err := st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error { x.Status, x.ImageID = next, image; return nil }); err != nil {
		t.Fatal(err)
	}
	if status == state.StatusLive {
		if err := st.SetCurrentDeployment(ctx, "web", d.ID); err != nil {
			t.Fatal(err)
		}
		_ = st.AddEvent(ctx, state.Event{App: "web", DeploymentID: d.ID, At: d.CreatedAt.Add(9 * time.Second), Stage: "deploy", Message: "已上线"})
	}
	return d
}

func TestRollbackVersionsAndLogsWire(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := state.Open(state.Config{Path: filepath.Join(dir, "af.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logs, err := deploylog.Open(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.EnsureApp(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	v1 := seed(t, st, 1, "sha256:a", state.StatusLive)
	seed(t, st, 2, "sha256:b", state.StatusLive)
	seed(t, st, 3, "sha256:c", state.StatusLive)
	if err := logs.Write("web", v1.ID, deploylog.KindRun, []byte("2026-11-03T02:10:31Z hello\n"), deploylog.NewRedactor()); err != nil {
		t.Fatal(err)
	}
	rn := &imageRunner{images: []runner.ImageInfo{{ID: "sha256:a"}, {ID: "sha256:b"}, {ID: "sha256:c"}}, logs: []string{"live line"}}
	ts := httptest.NewServer(apiserver.New(apiserver.Config{
		Store: st, Kicker: nopKicker{}, Runner: rn, UploadDir: t.TempDir(), PublicHost: "127.0.0.1", DeployLogs: logs,
	}))
	defer ts.Close()
	c, err := client.Connect(ctx, client.Target{URL: ts.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	features, err := c.Features(ctx)
	if err != nil || !client.HasFeature(features, client.FeatureRollbackTo) || !client.HasFeature(features, client.FeatureDeployLogs) {
		t.Fatalf("features = %v, %v", features, err)
	}

	list, err := c.ListDeployments(ctx, "web", 20)
	if err != nil || len(list.Deployments) != 3 || list.RollbackTargetSeq != 2 || list.ImageCheck != "ok" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	live := list.Deployments[0]
	if live.StatusText != "正在服务" || live.SourceText != "上传" || live.DurationSeconds == nil || *live.DurationSeconds != 9 ||
		live.RollbackBlocked != "live" || !live.Logs.Run {
		t.Fatalf("live view = %+v", live)
	}
	if d := list.Deployments[2]; !d.CanRollback || d.RollbackText != "可回退" || !d.Logs.Run || d.Logs.Build {
		t.Fatalf("v1 view = %+v", d)
	}

	res, err := c.Rollback(ctx, "web", client.RollbackOptions{ToSeq: 1})
	if err != nil || !res.Created || res.TargetSeq != 1 || res.Deployment.BasedOnSeq != 1 || res.Deployment.Seq != 4 || res.SupersededSeq != 0 {
		t.Fatalf("rollback --to 1 = %+v, %v", res, err)
	}
	// Another version while v4 is queued, then with cancel.
	_, err = c.Rollback(ctx, "web", client.RollbackOptions{ToSeq: 2})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusConflict || ce.Diag.Code != "deploy_in_progress" || ce.Diag.Message != "第 4 版正在部署，回退会取消它" {
		t.Fatalf("in progress: %v", err)
	}
	res, err = c.Rollback(ctx, "web", client.RollbackOptions{ToSeq: 2, CancelPending: true})
	if err != nil || res.SupersededSeq != 4 || res.TargetSeq != 2 {
		t.Fatalf("cancel pending = %+v, %v", res, err)
	}
	// A version that does not exist is the API's own JSON 404, not "too old".
	_, err = c.Rollback(ctx, "web", client.RollbackOptions{ToSeq: 99})
	if !errors.As(err, &ce) || ce.Diag.Code != "version_not_found" {
		t.Fatalf("not found: %v", err)
	}

	got, err := c.DeploymentLogs(ctx, v1.ID, "run", 0)
	if err != nil || got.Source != "archive" || got.Version != 1 || len(got.Lines) != 1 || !strings.HasSuffix(got.Lines[0], "hello") {
		t.Fatalf("logs v1 = %+v, %v", got, err)
	}
	got, err = c.DeploymentLogs(ctx, live.ID, "", 10)
	if err != nil || got.Source != "live" || got.Kind != "run" || got.Lines[0] != "live line" {
		t.Fatalf("logs live = %+v, %v", got, err)
	}
	_, err = c.DeploymentLogs(ctx, v1.ID, "build", 0)
	if !errors.As(err, &ce) || ce.Diag.Code != "logs_not_kept" {
		t.Fatalf("build not kept: %v", err)
	}

	app, err := c.App(ctx, "web")
	if err != nil || app.RollbackTargetSeq == 0 || app.Live == nil || app.Live.StatusText != "正在服务" || app.Latest == nil {
		t.Fatalf("app = %+v, %v", app, err)
	}
}

// An older server's router answers plain-text 404/405 for the new routes.
func TestNewRoutesOnOldServerAreServerTooOld(t *testing.T) {
	var created int
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/status":
			_, _ = w.Write([]byte(`{"api_version":1,"runner":"ok"}`))
		case r.URL.Path == "/v1/apps/web/rollback":
			created++
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"id":"0123456789ab","app":"web","seq":4,"status":"queued","based_on_seq":2}`))
		case strings.HasSuffix(r.URL.Path, "/logs") && r.Method == http.MethodPost:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		default:
			http.NotFound(w, r)
		}
	}))
	defer old.Close()
	ctx := context.Background()
	c, err := client.Connect(ctx, client.Target{URL: old.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if f, err := c.Features(ctx); err != nil || len(f) != 0 {
		t.Fatalf("features = %v, %v", f, err)
	}
	var ce *client.Error
	if _, err := c.Rollback(ctx, "web", client.RollbackOptions{ToSeq: 2}); !errors.As(err, &ce) || ce.Diag.Code != client.CodeServerTooOld || ce.Diag.Hint == "" {
		t.Fatalf("rollback --to on old server: %v", err)
	}
	if _, err := c.DeploymentLogs(ctx, "0123456789ab", "run", 0); !errors.As(err, &ce) || ce.Diag.Code != client.CodeServerTooOld {
		t.Fatalf("logs on old server: %v", err)
	}
	if created != 0 {
		t.Fatalf("old route called %d times", created)
	}
	// The old route still works; the target comes from based_on_seq.
	res, err := c.Rollback(ctx, "web", client.RollbackOptions{})
	if err != nil || res.TargetSeq != 2 || !res.Created || created != 1 {
		t.Fatalf("plain rollback = %+v, %v", res, err)
	}
}
