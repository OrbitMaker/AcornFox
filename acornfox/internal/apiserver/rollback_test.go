package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// realServer runs the API on the real SQLite store, so the rollback checks
// are exactly the ones the transaction makes.
type realServer struct {
	t    *testing.T
	h    http.Handler
	st   *state.Store
	kick *fakeKicker
	run  *fakeRunner
	logs *deploylog.Store
	n    int
}

func newRealServer(t *testing.T) *realServer {
	t.Helper()
	dir := t.TempDir()
	st, err := state.Open(state.Config{Path: filepath.Join(dir, "acornfox.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logs, err := deploylog.Open(filepath.Join(dir, "logs", "deployments"))
	if err != nil {
		t.Fatal(err)
	}
	rs := &realServer{t: t, st: st, kick: &fakeKicker{}, run: newFakeRunner(), logs: logs}
	rs.h = New(Config{
		Store: st, Kicker: rs.kick, Runner: rs.run, UploadDir: t.TempDir(),
		PublicHost: "192.168.1.10", HostMetrics: fakeHostSized(8192, 4), DeployLogs: logs,
	})
	if _, _, err := st.EnsureApp(context.Background(), "web"); err != nil {
		t.Fatal(err)
	}
	return rs
}

// add creates the next deployment of web. image "" means no image; status
// live makes it the current deployment (retiring the previous live one) and
// records the go-live event 63 seconds after creation. reuse > 0 makes it an
// image-source deployment re-using that version (reason redeploy).
func (rs *realServer) add(image, status string, reuse int) state.Deployment {
	rs.t.Helper()
	ctx := context.Background()
	rs.n++
	in := state.NewDeployment{App: "web", SourceKind: state.SourceUpload, SourceRef: "/u", SourceDigest: fmt.Sprintf("d%d", rs.n)}
	if reuse > 0 {
		in = state.NewDeployment{App: "web", SourceKind: state.SourceImage, SourceRef: image, SourceDigest: image,
			BypassDedup: true, BasedOnSeq: reuse, OriginKind: state.SourceUpload, Reason: state.ReasonRedeploy}
	}
	d, _, err := rs.st.CreateDeployment(ctx, in)
	if err != nil {
		rs.t.Fatal(err)
	}
	next := status
	if status == state.StatusLive {
		next = state.StatusRouting
	}
	if status != state.StatusQueued {
		if _, err := rs.st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error {
			x.Status, x.ImageID = next, image
			if status == state.StatusFailed {
				x.Diagnosis = &state.Diagnosis{Stage: "health", Code: "container_exited", Message: "容器启动后退出（退出码 1）"}
			}
			return nil
		}); err != nil {
			rs.t.Fatal(err)
		}
	}
	if status == state.StatusLive {
		if err := rs.st.SetCurrentDeployment(ctx, "web", d.ID); err != nil {
			rs.t.Fatal(err)
		}
		if err := rs.st.AddEvent(ctx, state.Event{App: "web", DeploymentID: d.ID, At: d.CreatedAt.Add(63 * time.Second), Stage: "deploy", Message: "已上线 http://x"}); err != nil {
			rs.t.Fatal(err)
		}
	}
	d, err = rs.st.GetDeployment(ctx, d.ID)
	if err != nil {
		rs.t.Fatal(err)
	}
	return d
}

// images marks which image IDs are on this machine.
func (rs *realServer) images(ids ...string) {
	infos := make([]runner.ImageInfo, 0, len(ids))
	for _, id := range ids {
		infos = append(infos, runner.ImageInfo{ID: id, App: "web"})
	}
	rs.run.images = map[string][]runner.ImageInfo{"web": infos}
}

func (rs *realServer) post(path, body string, headers map[string]string) (int, map[string]any) {
	rs.t.Helper()
	var b []byte
	if body != "" {
		b = []byte(body)
	}
	w := do(rs.t, rs.h, http.MethodPost, path, b, headers)
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		rs.t.Fatalf("%s: not JSON: %s", path, w.Body)
	}
	return w.Code, m
}

func (rs *realServer) get(path string) (int, map[string]any) {
	rs.t.Helper()
	w := do(rs.t, rs.h, http.MethodGet, path, nil, nil)
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		rs.t.Fatalf("%s: not JSON: %s", path, w.Body)
	}
	return w.Code, m
}

func (rs *realServer) count() int {
	deps, err := rs.st.ListDeployments(context.Background(), "web", 0)
	if err != nil {
		rs.t.Fatal(err)
	}
	return len(deps)
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

// histFill is how many extra distinct images history() puts between v2 and
// v3, so that v1 is just outside the kept window for any KeepVersions.
const histFill = state.KeepVersions - 3

// hv maps a version number of the history below, as written for a window of
// three, to the real version number.
func hv(n int) int {
	if n <= 2 {
		return n
	}
	return n + histFill
}

// hvf is hv as a JSON number.
func hvf(n int) float64 { return float64(hv(n)) }

// fillImages are the images of the histFill versions, oldest first.
func fillImages() []string {
	out := make([]string, 0, histFill)
	for i := 1; i <= histFill; i++ {
		out = append(out, fmt.Sprintf("sha256:fill-%d", i))
	}
	return out
}

// history builds: v1 A retired, v2 B retired, histFill distinct retired
// images, v3 C retired, v4 D retired, v5 E failed, v6 superseded, v7 C
// retired (redeploy of v3), v8 F live; real numbers come from hv.
// Kept window (live F counts as seen): C (v7), D (v4), the fill images and
// B (v2), KeepVersions in all; A is gone.
func (rs *realServer) history() {
	rs.add("sha256:a", state.StatusLive, 0)
	rs.add("sha256:b", state.StatusLive, 0)
	for _, img := range fillImages() {
		rs.add(img, state.StatusLive, 0)
	}
	rs.add("sha256:c", state.StatusLive, 0)
	rs.add("sha256:d", state.StatusLive, 0)
	rs.add("sha256:e", state.StatusFailed, 0)
	rs.add("", state.StatusSuperseded, 0)
	rs.add("sha256:c", state.StatusLive, hv(3))
	rs.add("sha256:f", state.StatusLive, 0)
}

func TestRollbackToVersionErrors(t *testing.T) {
	rs := newRealServer(t)
	rs.history()
	rs.images(append(fillImages(), "sha256:a", "sha256:b", "sha256:c", "sha256:d", "sha256:e", "sha256:f")...)
	before := rs.count()
	// Newest first: v7, v4, the fill versions, v2.
	alts := []int{hv(7), hv(4)}
	for seq := hv(3) - 1; seq > 2; seq-- {
		alts = append(alts, seq)
	}
	alts = append(alts, 2)
	altText := make([]string, len(alts))
	altJSON := make([]any, len(alts))
	for i, seq := range alts {
		altText[i], altJSON[i] = fmt.Sprint(seq), float64(seq)
	}

	cases := []struct {
		path   string
		body   string
		status int
		code   string
		msg    string
	}{
		{"/v1/apps/web/versions/99/rollback", "", 404, state.RollbackVersionNotFound, "没有第 99 版"},
		{fmt.Sprintf("/v1/apps/web/versions/%d/rollback", hv(8)), "", 409, state.RollbackVersionIsLive, fmt.Sprintf("第 %d 版就是正在服务的版本", hv(8))},
		{fmt.Sprintf("/v1/apps/web/versions/%d/rollback", hv(5)), "", 409, state.RollbackVersionNotRollbackable, fmt.Sprintf("第 %d 版没有成功上线过，不能回退到它", hv(5))},
		{fmt.Sprintf("/v1/apps/web/versions/%d/rollback", hv(6)), "", 409, state.RollbackVersionNotRollbackable, ""},
		{"/v1/apps/web/versions/1/rollback", "", 409, state.RollbackVersionImageGone,
			fmt.Sprintf("第 1 版太旧，已经不能一键回退（只保留最近 %d 个版本）。可以回到第 %s 版", state.KeepVersions, strings.Join(altText, "、"))},
		{"/v1/apps/web/versions/abc/rollback", "", 400, "invalid_version", ""},
		{"/v1/apps/web/versions/0/rollback", "", 400, "invalid_version", ""},
		{"/v1/apps/web/versions/-2/rollback", "", 400, "invalid_version", ""},
		{"/v1/apps/web/versions/4/rollback", `{"to":2}`, 400, "invalid_version", ""},
		{"/v1/apps/web/versions/4/rollback", `not json`, 400, "invalid_version", ""},
		{"/v1/apps/web/rollback", `{"cancel_pending":true}{}`, 400, "invalid_version", ""},
		{"/v1/apps/nope/versions/4/rollback", "", 404, "not_found", ""},
		{"/v1/apps/Bad_Name/versions/4/rollback", "", 400, "invalid_app", ""},
	}
	for _, c := range cases {
		status, m := rs.post(c.path, c.body, nil)
		if status != c.status || errCode(m) != c.code {
			t.Fatalf("%s %q: %d %v, want %d %s", c.path, c.body, status, m, c.status, c.code)
		}
		if c.msg != "" {
			if got := m["error"].(map[string]any)["message"]; got != c.msg {
				t.Fatalf("%s: message %q, want %q", c.path, got, c.msg)
			}
		}
	}
	// The structured alternatives come with the image_gone refusal.
	_, m := rs.post("/v1/apps/web/versions/1/rollback", "", nil)
	e := m["error"].(map[string]any)
	if !reflect.DeepEqual(e["alternatives"], altJSON) || e["seq"] != 1.0 || e["live_seq"] != hvf(8) {
		t.Fatalf("error body = %v", e)
	}
	if rs.count() != before || len(rs.kick.kicks) != 0 {
		t.Fatalf("a refusal created a deployment or kicked: %d -> %d, kicks %v", before, rs.count(), rs.kick.kicks)
	}

	// An image removed by hand is caught before 202, from the runner's list.
	rs.images("sha256:c", "sha256:d", "sha256:f")
	status, m := rs.post("/v1/apps/web/versions/2/rollback", "", nil)
	if status != 409 || errCode(m) != state.RollbackVersionImageGone {
		t.Fatalf("hand-removed image: %d %v", status, m)
	}
	if rs.count() != before {
		t.Fatal("image_gone created a deployment")
	}
}

func TestRollbackToOlderDuplicateAndPending(t *testing.T) {
	rs := newRealServer(t)
	rs.history()
	rs.images("sha256:b", "sha256:c", "sha256:d", "sha256:f")

	// v3 has the same image as v7: no button, but --to 3 is allowed and is
	// recorded as based on 3.
	v3 := fmt.Sprintf("/v1/apps/web/versions/%d/rollback", hv(3))
	status, m := rs.post(v3, "", nil)
	if status != http.StatusAccepted || m["based_on_seq"] != hvf(3) || m["seq"] != hvf(9) || m["rollback_target_seq"] != hvf(3) ||
		m["source_ref"] != "sha256:c" || m["reason"] != state.ReasonRollback || m["status_text"] != "排队中" ||
		m["source_text"] != fmt.Sprintf("上传 · 回退（基于第 %d 版）", hv(3)) {
		t.Fatalf("rollback to 3: %d %v", status, m)
	}
	if _, ok := m["superseded_seq"]; ok {
		t.Fatalf("superseded_seq without a pending deployment: %v", m)
	}
	if len(rs.kick.kicks) != 1 || rs.kick.kicks[0] != "web" {
		t.Fatalf("kicks = %v", rs.kick.kicks)
	}

	// Clicking the same rollback again returns the queued one.
	status, m = rs.post(v3, "", nil)
	if status != http.StatusOK || m["seq"] != hvf(9) || rs.count() != hv(9) {
		t.Fatalf("second click: %d %v count=%d", status, m, rs.count())
	}

	// Another version while v9 is pending: refused, then cancelled on request.
	v4 := fmt.Sprintf("/v1/apps/web/versions/%d/rollback", hv(4))
	status, m = rs.post(v4, "", nil)
	if status != 409 || errCode(m) != state.RollbackDeployInProgress || m["error"].(map[string]any)["message"] != fmt.Sprintf("第 %d 版正在部署，回退会取消它", hv(9)) {
		t.Fatalf("in progress: %d %v", status, m)
	}
	status, m = rs.post(v4, `{"cancel_pending":true}`, nil)
	if status != http.StatusAccepted || m["superseded_seq"] != hvf(9) || m["based_on_seq"] != hvf(4) || m["seq"] != hvf(10) {
		t.Fatalf("cancel pending: %d %v", status, m)
	}
}

func TestRollbackDefaultTargetAndIdempotency(t *testing.T) {
	rs := newRealServer(t)
	rs.history()
	rs.images("sha256:b", "sha256:c", "sha256:d", "sha256:f")

	key := map[string]string{"Idempotency-Key": "k-1"}
	status, m := rs.post("/v1/apps/web/rollback", "", key)
	if status != http.StatusAccepted || m["based_on_seq"] != hvf(7) || m["rollback_target_seq"] != hvf(7) {
		t.Fatalf("default rollback: %d %v", status, m)
	}
	id := m["id"]
	for _, body := range []string{"", "{}"} {
		status, m = rs.post("/v1/apps/web/rollback", body, key)
		if status != http.StatusOK || m["id"] != id {
			t.Fatalf("same key again (%q): %d %v", body, status, m)
		}
	}
	if rs.count() != hv(9) {
		t.Fatalf("count = %d, want %d", rs.count(), hv(9))
	}
}

func TestRollbackSameAsLiveAndRunnerDown(t *testing.T) {
	rs := newRealServer(t)
	rs.add("sha256:a", state.StatusLive, 0)
	rs.add("sha256:b", state.StatusLive, 0)
	rs.add("sha256:a", state.StatusLive, 1) // a rollback to v1 is serving
	// The runner cannot list images: only the kept window decides.
	rs.run.imagesErr = errRunnerDown

	status, m := rs.post("/v1/apps/web/versions/1/rollback", "", nil)
	if status != 409 || errCode(m) != state.RollbackVersionSameAsLive ||
		m["error"].(map[string]any)["message"] != "第 1 版和正在服务的第 3 版是同一个程序，不需要回退" {
		t.Fatalf("same as live: %d %v", status, m)
	}
	status, m = rs.post("/v1/apps/web/rollback", "", nil)
	if status != http.StatusAccepted || m["based_on_seq"] != 2.0 {
		t.Fatalf("default goes to v2: %d %v", status, m)
	}
}

func TestDeploymentListView(t *testing.T) {
	rs := newRealServer(t)
	rs.history()
	rs.images(append(fillImages(), "sha256:b", "sha256:c", "sha256:d", "sha256:f")...)
	if err := rs.logs.Write("web", deploymentBySeq(t, rs, hv(4)).ID, deploylog.KindBuild, []byte("Step 1/2\n"), deploylog.NewRedactor()); err != nil {
		t.Fatal(err)
	}

	status, m := rs.get("/v1/apps/web/deployments?limit=20")
	if status != 200 || m["rollback_target_seq"] != hvf(7) || m["image_check"] != "ok" {
		t.Fatalf("list: %d %v", status, m)
	}
	bySeq := map[float64]map[string]any{}
	for _, raw := range m["deployments"].([]any) {
		d := raw.(map[string]any)
		bySeq[d["seq"].(float64)] = d
	}
	want := []struct {
		seq                                     float64
		status, source, blocked, rbText, failed string
		can                                     bool
		same                                    float64
	}{
		{hvf(8), "正在服务", "上传", state.BlockedLive, "", "", false, 0},
		{hvf(7), "已被新版本替换", fmt.Sprintf("上传 · 重新部署（基于第 %d 版）", hv(3)), "", "可回退", "", true, 0},
		{hvf(6), "已被更新的提交取代", "上传", state.BlockedSuperseded, "", "", false, 0},
		{hvf(5), "部署失败", "上传", state.BlockedFailed, "", "停在健康检查：容器启动后退出（退出码 1）", false, 0},
		{hvf(4), "已被新版本替换", "上传", "", "可回退", "", true, 0},
		{hvf(3), "已被新版本替换", "上传", state.BlockedOlderDuplicate, fmt.Sprintf("与第 %d 版是同一个程序", hv(7)), "", false, hvf(7)},
		{2, "已被新版本替换", "上传", "", "可回退", "", true, 0},
		{1, "已被新版本替换", "上传", state.BlockedImageGone, "太旧，不能一键回退", "", false, 0},
	}
	// The fill versions sit inside the window, between v2 and v3.
	for seq := 3; seq < hv(3); seq++ {
		want = append(want, struct {
			seq                                     float64
			status, source, blocked, rbText, failed string
			can                                     bool
			same                                    float64
		}{float64(seq), "已被新版本替换", "上传", "", "可回退", "", true, 0})
	}
	for _, w := range want {
		d := bySeq[w.seq]
		if d == nil {
			t.Fatalf("version %v missing", w.seq)
		}
		rbText, _ := d["rollback_text"].(string)
		failed, _ := d["failure_text"].(string)
		if d["status_text"] != w.status || d["source_text"] != w.source || d["rollback_blocked"] != w.blocked ||
			rbText != w.rbText || failed != w.failed || d["can_rollback"] != w.can || d["same_image_as"] != w.same {
			t.Fatalf("version %v = %v", w.seq, d)
		}
		// Every stored field is still there.
		for _, k := range []string{"id", "app", "source_kind", "source_digest", "status", "attempts", "created_at"} {
			if _, ok := d[k]; !ok {
				t.Fatalf("version %v lost field %s", w.seq, k)
			}
		}
	}
	// Durations come from the go-live event (63 s after creation); the
	// failed one has no finish time here, the superseded one never has one.
	if bySeq[hvf(8)]["duration_seconds"] != 63.0 || bySeq[hvf(4)]["duration_seconds"] != 63.0 || bySeq[hvf(6)]["duration_seconds"] != nil {
		t.Fatalf("durations: v8=%v v4=%v v6=%v", bySeq[hvf(8)]["duration_seconds"], bySeq[hvf(4)]["duration_seconds"], bySeq[hvf(6)]["duration_seconds"])
	}
	logs := func(seq float64) map[string]any { return bySeq[seq]["logs"].(map[string]any) }
	if logs(hvf(8))["run"] != true || logs(hvf(8))["build"] != false || logs(hvf(4))["build"] != true || logs(hvf(4))["run"] != false || logs(2)["build"] != false {
		t.Fatalf("logs: v8=%v v4=%v v2=%v", logs(hvf(8)), logs(hvf(4)), logs(2))
	}

	// The app view and the deployment detail use the same view.
	_, app := rs.get("/v1/apps/web")
	live := app["live"].(map[string]any)
	if app["rollback_target_seq"] != hvf(7) || live["status_text"] != "正在服务" || live["duration_seconds"] != 63.0 ||
		app["latest"].(map[string]any)["seq"] != hvf(8) {
		t.Fatalf("app view: %v", app)
	}
	_, list := rs.get("/v1/apps")
	if list["apps"].([]any)[0].(map[string]any)["rollback_target_seq"] != hvf(7) {
		t.Fatalf("apps list: %v", list)
	}
	_, detail := rs.get("/v1/deployments/" + deploymentBySeq(t, rs, hv(7)).ID)
	dep := detail["deployment"].(map[string]any)
	if dep["can_rollback"] != true || dep["rollback_text"] != "可回退" || dep["duration_seconds"] != 63.0 {
		t.Fatalf("detail: %v", dep)
	}

	// Runner down: the list still answers, marked unknown.
	rs.run.imagesErr = errRunnerDown
	if _, m := rs.get("/v1/apps/web/deployments"); m["image_check"] != "unknown" || m["rollback_target_seq"] != hvf(7) {
		t.Fatalf("runner down: %v", m)
	}
}

func deploymentBySeq(t *testing.T, rs *realServer, seq int) state.Deployment {
	t.Helper()
	deps, err := rs.st.ListDeployments(context.Background(), "web", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range deps {
		if d.Seq == seq {
			return d
		}
	}
	t.Fatalf("no version %d", seq)
	return state.Deployment{}
}

func TestStatusFeatures(t *testing.T) {
	h, _, _, _, _ := newTestServer(t)
	w := do(t, h, http.MethodGet, "/v1/status", nil, nil)
	var body struct {
		APIVersion int      `json:"api_version"`
		Features   []string `json:"features"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.APIVersion != 1 || !reflect.DeepEqual(body.Features, []string{"rollback_to", "deploy_logs", "deploy_timing"}) {
		t.Fatalf("status = %s", w.Body)
	}
}
