package apiserver

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/appbackup"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// volumeRunner is the API test runner with volumes that hold one file each.
type volumeRunner struct {
	*fakeRunner
	mu   sync.Mutex
	data map[string]string // volume -> content of its one file
}

func (v *volumeRunner) EnsureVolume(_ context.Context, _, name string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.data[name]; !ok {
		v.data[name] = ""
	}
	return nil
}

func (v *volumeRunner) ExportVolume(_ context.Context, req runner.VolumeDataRequest, w io.Writer) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	tw := tar.NewWriter(w)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "./f.txt", Mode: 0o644, Size: int64(len(v.data[req.Volume]))})
	_, _ = tw.Write([]byte(v.data[req.Volume]))
	return tw.Close()
}

func (v *volumeRunner) ImportVolume(_ context.Context, req runner.VolumeDataRequest, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		_, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		b, _ := io.ReadAll(tr)
		v.mu.Lock()
		v.data[req.Volume] = string(b)
		v.mu.Unlock()
	}
}

func (v *volumeRunner) VolumeSizes(_ context.Context, _ string) ([]runner.VolumeSize, error) {
	return nil, nil
}

// liveKicker makes every pending deployment live, like the reconciler would.
type liveKicker struct {
	st *state.Store
}

func (k *liveKicker) Kick(app string) {
	ctx := context.Background()
	pend, _ := k.st.PendingDeployments(ctx, app)
	for _, d := range pend {
		_, _ = k.st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error { x.ImageID = d.SourceRef; return nil })
		_ = k.st.SetCurrentDeployment(ctx, app, d.ID)
	}
}

func newBackupServer(t *testing.T) (*realServer, *volumeRunner, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := state.Open(state.Config{Path: filepath.Join(dir, "acornfox.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	vr := &volumeRunner{fakeRunner: newFakeRunner(), data: map[string]string{}}
	k := &liveKicker{st: st}
	mgr := appbackup.New(appbackup.Config{Dir: filepath.Join(dir, "backups"), Store: st, Runner: vr, Kick: k.Kick, Poll: time.Millisecond, HealthWait: time.Minute})
	t.Cleanup(mgr.Close)
	rs := &realServer{t: t, st: st, kick: &fakeKicker{}, run: vr.fakeRunner}
	rs.h = New(Config{Store: st, Kicker: k, Runner: vr, UploadDir: t.TempDir(), PublicHost: "192.168.1.10", HostMetrics: fakeHostSized(8192, 4), Backups: mgr})
	ctx := context.Background()
	if _, _, err := st.EnsureApp(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	v, _, err := st.AddVolume(ctx, "web", "/data", false)
	if err != nil {
		t.Fatal(err)
	}
	vr.data[v.VolumeName] = "before"
	rs.add("sha256:img1", state.StatusLive, 0)
	rs.images("sha256:img1")
	return rs, vr, v.VolumeName
}

func (rs *realServer) req(method, path, body string) (int, map[string]any) {
	rs.t.Helper()
	var b []byte
	if body != "" {
		b = []byte(body)
	}
	w := do(rs.t, rs.h, method, path, b, nil)
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		rs.t.Fatalf("%s %s: not JSON: %s", method, path, w.Body)
	}
	return w.Code, m
}

func bkpErrCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func (rs *realServer) waitTask(m map[string]any) map[string]any {
	rs.t.Helper()
	task, _ := m["task"].(map[string]any)
	id, _ := task["id"].(string)
	if id == "" {
		rs.t.Fatalf("no task in %v", m)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		code, got := rs.req(http.MethodGet, "/v1/backups/tasks/"+id, "")
		if code != 200 {
			rs.t.Fatalf("task: %d %v", code, got)
		}
		if got["status"] != "running" {
			return got
		}
		time.Sleep(2 * time.Millisecond)
	}
	rs.t.Fatal("task did not finish")
	return nil
}

func TestBackupRoutesRoundTrip(t *testing.T) {
	rs, vr, vol := newBackupServer(t)

	// Plan first: nothing is made.
	code, m := rs.req("POST", "/v1/apps/web/backups", `{"plan":true}`)
	plan, _ := m["plan"].(map[string]any)
	if code != 200 || plan == nil || plan["enough"] != true || plan["kind"] != "manual" {
		t.Fatalf("plan: %d %v", code, m)
	}
	if code, l := rs.req("GET", "/v1/apps/web/backups", ""); code != 200 || len(l["backups"].([]any)) != 0 || l["installed"] != true {
		t.Fatalf("empty list: %d %v", code, l)
	}

	// A backup is a task.
	code, m = rs.req("POST", "/v1/apps/web/backups", `{"note":"第一份"}`)
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %v", code, m)
	}
	done := rs.waitTask(m)
	if done["status"] != "succeeded" {
		t.Fatalf("backup task: %v", done)
	}
	id := done["result"].(map[string]any)["backup_id"].(string)

	code, l := rs.req("GET", "/v1/apps/web/backups", "")
	list := l["backups"].([]any)
	if code != 200 || len(list) != 1 || list[0].(map[string]any)["id"] != id || list[0].(map[string]any)["note"] != "第一份" {
		t.Fatalf("list: %d %v", code, l)
	}
	if code, d := rs.req("GET", "/v1/apps/web/backups/"+id+"?verify=true", ""); code != 200 || d["restore_plan"].(map[string]any)["verified"] != true {
		t.Fatalf("detail: %d %v", code, d)
	}
	if code, o := rs.req("GET", "/v1/backups", ""); code != 200 || o["writable"] != true || len(o["apps"].([]any)) != 1 {
		t.Fatalf("overview: %d %v", code, o)
	}

	// Restoring needs the user's yes.
	vr.mu.Lock()
	vr.data[vol] = "after"
	vr.mu.Unlock()
	code, m = rs.req("POST", "/v1/apps/web/backups/"+id+"/restore", `{}`)
	if code != http.StatusConflict || bkpErrCode(m) != "BKP-06" {
		t.Fatalf("restore without yes: %d %v", code, m)
	}
	if e := m["error"].(map[string]any); e["hint"] == "" {
		t.Fatalf("no hint: %v", e)
	}
	code, m = rs.req("POST", "/v1/apps/web/backups/"+id+"/restore", `{"plan":true}`)
	if code != 200 || m["plan"].(map[string]any)["needs_confirm"] != true {
		t.Fatalf("restore plan: %d %v", code, m)
	}
	code, m = rs.req("POST", "/v1/apps/web/backups/"+id+"/restore", `{"confirm":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("restore: %d %v", code, m)
	}
	done = rs.waitTask(m)
	if done["status"] != "succeeded" {
		t.Fatalf("restore task: %v", done)
	}
	vr.mu.Lock()
	got := vr.data[vol]
	vr.mu.Unlock()
	if got != "before" {
		t.Fatalf("data after restore: %q", got)
	}

	// Errors carry the code.
	if code, m := rs.req("GET", "/v1/apps/web/backups/20000101-000000-0000", ""); code != 404 || bkpErrCode(m) != "BKP-04" {
		t.Fatalf("missing: %d %v", code, m)
	}
	if code, m := rs.req("POST", "/v1/apps/ghost/backups", `{}`); code != 404 || bkpErrCode(m) != "BKP-12" {
		t.Fatalf("no app: %d %v", code, m)
	}
	if code, m := rs.req("POST", "/v1/apps/web/backups", `{"kind":"bogus"}`); code != 400 || bkpErrCode(m) != "BKP-16" {
		t.Fatalf("bad kind: %d %v", code, m)
	}
	if code, m := rs.req("GET", "/v1/backups/tasks/zzzz", ""); code != 400 {
		t.Fatalf("bad task id: %d %v", code, m)
	}

	// Deleting needs confirm=true and can be repeated.
	if code, m := rs.req("DELETE", "/v1/apps/web/backups/"+id, ""); code != 409 || bkpErrCode(m) != "BKP-06" {
		t.Fatalf("delete without yes: %d %v", code, m)
	}
	if code, m := rs.req("DELETE", "/v1/apps/web/backups/"+id+"?confirm=true", ""); code != 200 || m["deleted"] != true {
		t.Fatalf("delete: %d %v", code, m)
	}
	if code, m := rs.req("DELETE", "/v1/apps/web/backups/"+id+"?confirm=true", ""); code != 200 || m["deleted"] != false {
		t.Fatalf("delete again: %d %v", code, m)
	}
	if code, m := rs.req("DELETE", "/v1/apps/web/backups/"+id+"?confirm=maybe", ""); code != 400 {
		t.Fatalf("bad confirm: %d %v", code, m)
	}
}

func TestDeletedAppKeepsItsBackups(t *testing.T) {
	rs, _, _ := newBackupServer(t)
	_, m := rs.req("POST", "/v1/apps/web/backups", `{}`)
	rs.waitTask(m)
	code, m := rs.req("DELETE", "/v1/apps/web?volumes=true", "")
	if code != 200 {
		t.Fatalf("delete app: %d %v", code, m)
	}
	warns, _ := m["warnings"].([]any)
	found := false
	for _, w := range warns {
		if strings.Contains(w.(string), "备份还留在这台机器上") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no note about the backups: %v", m)
	}
	code, l := rs.req("GET", "/v1/apps/web/backups", "")
	if code != 200 || l["installed"] != false || len(l["backups"].([]any)) != 1 {
		t.Fatalf("list of a deleted app: %d %v", code, l)
	}
}

func TestBackupRoutesOffWithoutManager(t *testing.T) {
	h, _, _, _, _ := newTestServer(t)
	for _, p := range []string{"/v1/backups", "/v1/apps/web/backups", "/v1/backups/tasks/aaaaaaaaaaaa"} {
		w := do(t, h, "GET", p, nil, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: %d %s", p, w.Code, w.Body)
		}
	}
}

func TestRestoreDeploymentReasonText(t *testing.T) {
	if got := reasonText(state.Deployment{Reason: state.ReasonRestore, BasedOnSeq: 2}); got != "恢复备份（基于第 2 版）" {
		t.Fatalf("got %q", got)
	}
}
