package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

func notFoundErr() error {
	return &client.Error{Diag: client.Diagnosis{Stage: "server", Code: "not_found", Message: "对象不存在"}}
}

// statusCalls records what status asked the server for.
type statusCalls struct {
	apps []string
	deps []string
}

func statusHarness(t *testing.T, appExists func(string) bool) (*harness, *statusCalls) {
	t.Helper()
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	calls := &statusCalls{}
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) {
		calls.apps = append(calls.apps, app)
		if !appExists(app) {
			return client.App{}, notFoundErr()
		}
		return client.App{Name: app, Desired: "running", ObservedState: "running", CurrentDeployment: "0123456789ab"}, nil
	}
	h.api.deploymentFn = func(ctx context.Context, id string, after int64) (client.Deployment, []client.Event, error) {
		calls.deps = append(calls.deps, id)
		if id == "000000000000" {
			return client.Deployment{}, nil, notFoundErr()
		}
		return client.Deployment{ID: id, App: "hello", Seq: 3, Status: "live"}, nil, nil
	}
	return h, calls
}

// status APP shows the app, like logs and rollback take an app name; it is
// what the console and open's OPEN-APP-01 hint tell users to run.
func TestStatusTakesAppName(t *testing.T) {
	h, calls := statusHarness(t, func(string) bool { return true })
	code, out, errOut := h.run("--json", "status", "hello")
	if code != exitOK || len(calls.deps) != 0 || len(calls.apps) != 1 || calls.apps[0] != "hello" {
		t.Fatalf("exit %d calls %+v stderr %q", code, calls, errOut)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil || m["app"] != "hello" || m["observed_state"] != "running" {
		t.Fatalf("output %q", out)
	}
	// Same rules as other commands: a name that normalizes is accepted.
	h, calls = statusHarness(t, func(string) bool { return true })
	if code, _, _ := h.run("status", "Hello"); code != exitOK || calls.apps[0] != "hello" {
		t.Fatalf("normalized name: exit %d calls %+v", code, calls)
	}
}

func TestStatusTakesDeploymentID(t *testing.T) {
	h, calls := statusHarness(t, func(string) bool { return true })
	code, out, _ := h.run("status", "3f2a9c0d1b7e")
	if code != exitOK || len(calls.apps) != 0 || len(calls.deps) != 1 || !strings.Contains(out, "部署 3f2a9c0d1b7e") {
		t.Fatalf("exit %d calls %+v out %q", code, calls, out)
	}
	h, _ = statusHarness(t, func(string) bool { return true })
	code, _, errOut := h.run("status", "000000000000")
	if code != exitServerError || !strings.Contains(errOut, "没有编号为 000000000000 的部署") {
		t.Fatalf("unknown id: exit %d stderr %q", code, errOut)
	}
}

// A 12-hex name that is also a valid app name is looked up as an app first,
// then as a deployment.
func TestStatusAmbiguousArgument(t *testing.T) {
	h, calls := statusHarness(t, func(string) bool { return true })
	if code, out, _ := h.run("status", "deadbeefcafe"); code != exitOK || len(calls.deps) != 0 || !strings.Contains(out, "应用 deadbeefcafe") {
		t.Fatalf("app exists: exit %d calls %+v out %q", code, calls, out)
	}
	h, calls = statusHarness(t, func(string) bool { return false })
	if code, out, _ := h.run("status", "deadbeefcafe"); code != exitOK || len(calls.deps) != 1 || !strings.Contains(out, "部署 deadbeefcafe") {
		t.Fatalf("no such app: exit %d calls %+v out %q", code, calls, out)
	}
	// --app names another app: the argument can only be the deployment.
	h, calls = statusHarness(t, func(string) bool { return true })
	if code, _, _ := h.run("--app", "web", "status", "deadbeefcafe"); code != exitOK || len(calls.apps) != 0 || len(calls.deps) != 1 {
		t.Fatalf("with --app: exit %d calls %+v", code, calls)
	}
}

func TestStatusArgumentErrors(t *testing.T) {
	h, calls := statusHarness(t, func(string) bool { return false })
	code, _, errOut := h.run("status", "hello")
	if code != exitServerError || !strings.Contains(errOut, "没有叫 hello 的应用") || !strings.Contains(errOut, "acornfox apps") || len(calls.deps) != 0 {
		t.Fatalf("missing app: exit %d stderr %q calls %+v", code, errOut, calls)
	}
	h, calls = statusHarness(t, func(string) bool { return true })
	code, out, _ := h.run("--json", "status", "123abc")
	if code != exitUsage || !strings.Contains(out, "既不是应用名，也不是部署编号") || len(calls.apps)+len(calls.deps) != 0 {
		t.Fatalf("neither: exit %d out %q", code, out)
	}
	h, _ = statusHarness(t, func(string) bool { return true })
	if code, _, errOut := h.run("--app", "web", "status", "hello"); code != exitUsage || !strings.Contains(errOut, "不一致") {
		t.Fatalf("mismatch: exit %d stderr %q", code, errOut)
	}
	h, _ = statusHarness(t, func(string) bool { return true })
	if code, _, _ := h.run("status", "a", "b"); code != exitUsage {
		t.Fatalf("two args: exit %d", code)
	}
	h, _ = statusHarness(t, func(string) bool { return true })
	if code, out, _ := h.run("status", "-h"); code != exitOK || !strings.Contains(out, "acornfox status [应用名 | 部署编号]") {
		t.Fatalf("help: exit %d out %q", code, out)
	}
}

// lifecycleHarness serves an app whose observed state follows the given
// sequence, one entry per poll (the last one repeats).
func lifecycleHarness(t *testing.T, states []string, deployment string) (*harness, *[]string, *int) {
	t.Helper()
	shortStopWait(t, 200*time.Millisecond)
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	var acted []string
	h.api.stopFn = func(ctx context.Context, app string) error { acted = append(acted, "stop "+app); return nil }
	h.api.startFn = func(ctx context.Context, app string) error { acted = append(acted, "start "+app); return nil }
	h.api.restartFn = func(ctx context.Context, app string) error { acted = append(acted, "restart "+app); return nil }
	polls := 0
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) {
		s := states[len(states)-1]
		if polls < len(states) {
			s = states[polls]
		}
		polls++
		return client.App{Name: app, ObservedState: s, CurrentDeployment: deployment}, nil
	}
	return h, &acted, &polls
}

// start returns only once the container is observed running, so `apps`
// right after it does not still say stopped.
func TestStartWaitsForRunning(t *testing.T) {
	h, acted, polls := lifecycleHarness(t, []string{"stopped", "stopped", "running"}, "d1")
	code, out, errOut := h.run("start", "shop")
	if code != exitOK || *polls != 3 || len(*acted) != 1 || (*acted)[0] != "start shop" || !strings.Contains(out, "已启动应用 shop（观察状态：running）") {
		t.Fatalf("exit %d polls %d acted %v out %q stderr %q", code, *polls, *acted, out, errOut)
	}
	h, _, _ = lifecycleHarness(t, []string{"stopped", "running"}, "d1")
	code, out, _ = h.run("--json", "--app", "shop", "start")
	if code != exitOK || !strings.Contains(out, `"running":true`) || !strings.Contains(out, `"observed_state":"running"`) || !strings.Contains(out, `"waited":true`) {
		t.Fatalf("json: exit %d out %q", code, out)
	}
}

func TestRestartWaitsForRunning(t *testing.T) {
	h, acted, polls := lifecycleHarness(t, []string{"stopped", "running"}, "d1")
	code, out, _ := h.run("restart", "shop")
	if code != exitOK || *polls != 2 || (*acted)[0] != "restart shop" || !strings.Contains(out, "已重启应用 shop") {
		t.Fatalf("exit %d polls %d acted %v out %q", code, *polls, *acted, out)
	}
}

// restart --json keeps "restarted": true from its earlier output, waiting or
// not, so scripts checking it keep working; the new fields come on top.
func TestRestartKeepsRestartedField(t *testing.T) {
	for _, args := range [][]string{{"--json", "restart", "shop"}, {"--json", "restart", "shop", "--no-wait"}} {
		h, _, _ := lifecycleHarness(t, []string{"stopped", "running"}, "d1")
		code, out, _ := h.run(args...)
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil || code != exitOK || m["ok"] != true || m["restarted"] != true || m["app"] != "shop" {
			t.Fatalf("%v: exit %d out %q", args, code, out)
		}
	}
	h, _, _ := lifecycleHarness(t, []string{"running"}, "d1")
	if _, out, _ := h.run("--json", "start", "shop"); strings.Contains(out, "restarted") {
		t.Fatalf("start must not say restarted: %q", out)
	}
}

// runCtx is harness.run with a caller-supplied context.
func (h *harness) runCtx(ctx context.Context, args ...string) (int, string, string) {
	h.t.Helper()
	var out, errBuf bytes.Buffer
	code := MainWithConnector(ctx, args, io.NopCloser(bytes.NewReader(nil)), &out, &errBuf, func(string) string { return "" }, h.connect, h.configDir, h.workDir)
	return code, out.String(), errBuf.String()
}

// Ctrl-C or a dropped SSH session while start or restart waits, even in the
// middle of a poll request, ends with the "cancelled" diagnosis (the request
// was submitted, do not submit again), not a connection or server error.
func TestStartWaitCancelled(t *testing.T) {
	for _, action := range []string{"start", "restart"} {
		for _, cutPoll := range []int{1, 2} { // cut off on the first poll, or after one answer
			h, _, _ := lifecycleHarness(t, []string{"stopped"}, "d1")
			ctx, cancel := context.WithCancel(context.Background())
			polls := 0
			h.api.appFn = func(c context.Context, app string) (client.App, error) {
				polls++
				if polls == cutPoll {
					cancel()
					return client.App{}, &client.Error{Diag: client.Diagnosis{Stage: "connect", Code: "connect", Message: "连接中断"}}
				}
				return client.App{Name: app, ObservedState: "stopped", CurrentDeployment: "d1"}, nil
			}
			code, out, _ := h.runCtx(ctx, "--json", action, "shop")
			cancel()
			var m map[string]any
			if err := json.Unmarshal([]byte(out), &m); err != nil {
				t.Fatalf("%s/%d: not JSON %q", action, cutPoll, out)
			}
			diag, _ := m["diagnosis"].(map[string]any)
			if code != exitOpFailed || diag == nil || diag["code"] != "cancelled" || !strings.Contains(diag["hint"].(string), "不用再提交一次") {
				t.Fatalf("%s/%d: exit %d out %q", action, cutPoll, code, out)
			}
			want := "观察状态：未知"
			if cutPoll == 2 {
				want = "观察状态：stopped"
			}
			if !strings.Contains(diag["message"].(string), want) {
				t.Fatalf("%s/%d: message %q, want %q", action, cutPoll, diag["message"], want)
			}
		}
	}
	// stop cut off mid-poll says it is still stopping instead of failing.
	h, _, _ := lifecycleHarness(t, []string{"running"}, "d1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.api.appFn = func(c context.Context, app string) (client.App, error) {
		cancel()
		return client.App{}, &client.Error{Diag: client.Diagnosis{Stage: "connect", Code: "connect", Message: "连接中断"}}
	}
	code, out, _ := h.runCtx(ctx, "stop", "shop")
	if code != exitOK || !strings.Contains(out, "仍在停止中（当前状态：未知）") {
		t.Fatalf("stop cancelled: exit %d out %q", code, out)
	}
}

func TestStartTimeoutAndNoDeployment(t *testing.T) {
	h, _, _ := lifecycleHarness(t, []string{"stopped"}, "d1")
	code, out, _ := h.run("--json", "start", "shop")
	if code != exitOpFailed || !strings.Contains(out, `"code":"start_timeout"`) || !strings.Contains(out, "acornfox logs shop") {
		t.Fatalf("timeout: exit %d out %q", code, out)
	}
	h, _, polls := lifecycleHarness(t, []string{"missing"}, "")
	code, _, errOut := h.run("start", "shop")
	if code != exitOpFailed || *polls != 1 || !strings.Contains(errOut, "还没有上线过的版本") {
		t.Fatalf("no deployment: exit %d polls %d stderr %q", code, *polls, errOut)
	}
}

func TestLifecycleNoWait(t *testing.T) {
	for _, action := range []string{"stop", "start", "restart"} {
		h, acted, polls := lifecycleHarness(t, []string{"stopped"}, "d1")
		code, out, _ := h.run("--json", action, "shop", "--no-wait")
		if code != exitOK || *polls != 0 || len(*acted) != 1 || !strings.Contains(out, `"waited":false`) {
			t.Fatalf("%s: exit %d polls %d acted %v out %q", action, code, *polls, *acted, out)
		}
	}
}

// stop, start and restart take the app name like delete and logs; a name
// that differs from --app is refused.
func TestLifecycleAppArgument(t *testing.T) {
	h, acted, _ := lifecycleHarness(t, []string{"stopped"}, "d1")
	if code, out, _ := h.run("stop", "shop"); code != exitOK || (*acted)[0] != "stop shop" || !strings.Contains(out, "已停止应用 shop") {
		t.Fatalf("stop name: exit %d acted %v out %q", code, *acted, out)
	}
	for _, action := range []string{"stop", "start", "restart"} {
		h, acted, _ := lifecycleHarness(t, []string{"running"}, "d1")
		code, _, errOut := h.run("--app", "web", action, "shop")
		if code != exitUsage || len(*acted) != 0 || !strings.Contains(errOut, "不一致") {
			t.Fatalf("%s mismatch: exit %d acted %v stderr %q", action, code, *acted, errOut)
		}
		h, acted, _ = lifecycleHarness(t, []string{"running"}, "d1")
		if code, _, _ := h.run(action, "a", "b"); code != exitUsage || len(*acted) != 0 {
			t.Fatalf("%s two names: exit %d", action, code)
		}
		h, _, _ = lifecycleHarness(t, []string{"running"}, "d1")
		if code, out, _ := h.run(action, "-h"); code != exitOK || !strings.Contains(out, "acornfox "+action+" [应用名] [--no-wait]") {
			t.Fatalf("%s help: exit %d out %q", action, code, out)
		}
	}
	// Without a name the current directory's app is used, as before.
	h, acted, _ = lifecycleHarness(t, []string{"running"}, "d1")
	if code, _, _ := h.run("start"); code != exitOK || (*acted)[0] != "start notes" {
		t.Fatalf("default app: exit %d acted %v", code, *acted)
	}
}
