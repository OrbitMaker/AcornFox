package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/installtiming"
)

// fakeWaitAPI answers install status with answers in turn (the last one
// repeats) and counts the calls.
type fakeWaitAPI struct {
	*fakePrivateAPI
	answers []string
	calls   int
}

func (f *fakeWaitAPI) InstallTask(context.Context, string) (client.InstallTask, error) {
	raw := f.answers[min(f.calls, len(f.answers)-1)]
	f.calls++
	var v client.InstallTask
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		panic(err)
	}
	v.Raw = json.RawMessage(raw)
	return v, nil
}

func waitHarness(t *testing.T, answers ...string) (*harness, *fakeWaitAPI) {
	h, p := privateHarness(t)
	f := &fakeWaitAPI{fakePrivateAPI: p, answers: answers}
	h.connect = func(context.Context, client.Target) (client.API, error) { return f, nil }
	return h, f
}

// virtualClock makes --wait go by a clock that only moves when it sleeps,
// starting 12 seconds after the sample task began.
func virtualClock(t *testing.T) *time.Time {
	now := time.Date(2026, 11, 11, 4, 10, 51, 0, time.UTC)
	oldNow, oldSleep := waitNow, waitSleep
	waitNow = func() time.Time { return now }
	waitSleep = func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }
	t.Cleanup(func() { waitNow, waitSleep = oldNow, oldSleep })
	return &now
}

const doneSteps = `{"app":"hermes-agent","name":"check","label":"检查","status":"done","started_at":"2026-11-11T04:10:39Z","finished_at":"2026-11-11T04:10:39Z"},
{"app":"hermes-agent","name":"create","label":"建应用","status":"done","started_at":"2026-11-11T04:10:39Z","finished_at":"2026-11-11T04:10:40Z"},
{"app":"hermes-agent","name":"image","label":"下载镜像","status":"done","message":"本机已有这个镜像","started_at":"2026-11-11T04:10:40Z","finished_at":"2026-11-11T04:10:40Z"},
{"app":"hermes-agent","name":"files","label":"写配置","status":"done","started_at":"2026-11-11T04:10:40Z","finished_at":"2026-11-11T04:10:43Z"},`

// A reinstall of Hermes on old data 12 seconds in, in "启动" (what the first
// Haiku walk called stuck). No timing: an older server.
const runningTask = `{"id":"5961fd8ff351","kind":"app","target":"hermes-agent","status":"running","step":"hermes-agent/start","created_at":"2026-11-11T04:10:39Z",
"steps":[` + doneSteps + `
{"app":"hermes-agent","name":"start","label":"启动","status":"running","started_at":"2026-11-11T04:10:43Z"},
{"app":"hermes-agent","name":"health","label":"健康检查","status":"pending"},
{"app":"hermes-agent","name":"init","label":"初始化","status":"pending"}],
"apps":[{"app":"hermes-agent","catalog_id":"hermes-agent","version":"v0.23.0","reused":false,"created":true}]}`

// The same task done: the old token could not be revoked (only said once
// the task is over, in carried).
const succeededTask = `{"id":"5961fd8ff351","kind":"app","target":"hermes-agent","status":"succeeded","created_at":"2026-11-11T04:10:39Z","finished_at":"2026-11-11T04:10:57Z",
"steps":[` + doneSteps + `
{"app":"hermes-agent","name":"start","label":"启动","status":"done","message":"已上线","started_at":"2026-11-11T04:10:43Z","finished_at":"2026-11-11T04:10:55Z"},
{"app":"hermes-agent","name":"health","label":"健康检查","status":"done","started_at":"2026-11-11T04:10:55Z","finished_at":"2026-11-11T04:10:56Z"},
{"app":"hermes-agent","name":"init","label":"初始化","status":"skipped","started_at":"2026-11-11T04:10:56Z","finished_at":"2026-11-11T04:10:57Z"}],
"apps":[{"app":"hermes-agent","catalog_id":"hermes-agent","version":"v0.23.0","reused":false,"created":true,"url":"http://127.0.0.1:18811",
"reused_volumes":["af-hermes-agent-1"],"carried":["接上了旧数据（af-hermes-agent-1）","没能吊销以前在 new-api 里给 hermes-agent 的令牌（编号 1，结尾 rvRH）：new-api 停着。这把令牌不限额、不过期"]}]}`

// --wait asks until the task is over and the output says, without
// judgement left to the reader, that it is done and what must be told.
func TestInstallStatusWaitUntilDone(t *testing.T) {
	virtualClock(t)
	h, f := waitHarness(t, runningTask, runningTask, succeededTask)
	code, out, errOut := h.run("--json", "install", "status", "5961fd8ff351", "--wait")
	if code != exitOK || f.calls != 3 {
		t.Fatalf("exit %d calls %d stderr %s", code, f.calls, errOut)
	}
	m := decodeJSON(t, out)
	if m["ok"] != true || m["done"] != true || m["stuck"] != false || m["timed_out"] != false || m["waited_seconds"] != float64(4) ||
		m["wait_timeout_seconds"] != float64(90) || m["elapsed_seconds"] != float64(18) || m["status"] != "succeeded" {
		t.Fatalf("out %s", out)
	}
	if _, ok := m["next_command"]; ok {
		t.Fatalf("nothing to run once succeeded: %s", out)
	}
	tell := m["must_tell"].([]any)
	if len(tell) != 1 || !strings.HasPrefix(tell[0].(string), "没能吊销以前在 new-api 里给 hermes-agent 的令牌") {
		t.Fatalf("must_tell %v", tell)
	}
	tm := m["timing"].(map[string]any)
	if tm["clock"] != "client" || len(tm["steps"].([]any)) != 7 {
		t.Fatalf("timing worked out here: %v", tm)
	}
}

// Time up while still installing: the state as it is, not a conclusion, and
// the command that waits again.
func TestInstallStatusWaitTimesOut(t *testing.T) {
	virtualClock(t)
	h, f := waitHarness(t, runningTask)
	code, out, _ := h.run("--json", "install", "status", "5961fd8ff351", "--timeout", "10")
	if code != exitOK || f.calls != 6 {
		t.Fatalf("exit %d calls %d", code, f.calls)
	}
	m := decodeJSON(t, out)
	if m["done"] != false || m["stuck"] != false || m["timed_out"] != true || m["waited_seconds"] != float64(10) ||
		m["next_command"] != "acornfox --json install status 5961fd8ff351 --wait" || m["next"] != installtiming.NextRunning {
		t.Fatalf("out %s", out)
	}
	if say := m["say"].(string); !strings.Contains(say, "还在装：hermes-agent 正在“启动”（第 5/7 步），这一步已用 18 秒") || !strings.Contains(say, "整次已用 22 秒") {
		t.Fatalf("say %q", say)
	}
	step := m["timing"].(map[string]any)["step"].(map[string]any)
	if step["name"] != "start" || step["normal_seconds"] != float64(60) || step["stuck_after_seconds"] != float64(300) || step["look"] != "acornfox logs hermes-agent --tail 200 --json" {
		t.Fatalf("step %v", step)
	}
	// Past the stuck threshold: CAT-INST-30 and the read-only look first.
	virtualClock(t)
	h, _ = waitHarness(t, strings.Replace(runningTask, `"started_at":"2026-11-11T04:10:43Z"}`, `"started_at":"2026-11-11T04:00:00Z"}`, 1))
	_, out, _ = h.run("--json", "install", "status", "5961fd8ff351")
	m = decodeJSON(t, out)
	if m["stuck"] != true || m["done"] != false || m["next_command"] != "acornfox logs hermes-agent --tail 200 --json" ||
		m["timing"].(map[string]any)["code"] != "CAT-INST-30" {
		t.Fatalf("stuck: %s", out)
	}
}

func TestInstallStatusWaitFlags(t *testing.T) {
	virtualClock(t)
	h, f := waitHarness(t, runningTask)
	// One look without --wait: no waiting fields, one call.
	code, out, _ := h.run("--json", "install", "status", "5961fd8ff351")
	m := decodeJSON(t, out)
	if code != exitOK || f.calls != 1 || m["done"] != false || m["say"] == "" {
		t.Fatalf("exit %d calls %d out %s", code, f.calls, out)
	}
	if _, ok := m["waited_seconds"]; ok {
		t.Fatalf("waiting fields without --wait: %s", out)
	}
	// More than 600 seconds counts as 600; 2m is two minutes.
	for arg, want := range map[string]float64{"900": 600, "2m": 120, "45s": 45} {
		_, out, _ = h.run("--json", "install", "status", "5961fd8ff351", "--wait", "--timeout", arg)
		if m = decodeJSON(t, out); m["wait_timeout_seconds"] != want {
			t.Fatalf("--timeout %s: %s", arg, out)
		}
	}
	for _, bad := range []string{"abc", "0", "-5"} {
		if code, _, _ := h.run("install", "status", "5961fd8ff351", "--timeout", bad); code != exitUsage {
			t.Fatalf("--timeout %s: exit %d", bad, code)
		}
	}
	if code, out, _ := h.run("install", "-h"); code != exitOK || !strings.Contains(out, "--wait") || !strings.Contains(out, "must_tell") {
		t.Fatalf("help %d %s", code, out)
	}
}

// The server's verdict is used as it is; a failed task points at its code.
func TestInstallStatusUsesServerTiming(t *testing.T) {
	virtualClock(t)
	failed := `{"id":"t9","status":"failed","created_at":"2026-11-11T04:10:39Z","finished_at":"2026-11-11T04:11:09Z","steps":[],"apps":[],
"fault":{"code":"CAT-INST-16","message":"健康检查没通过"},
"timing":{"now":"2026-11-11T04:12:00Z","clock":"server","elapsed_seconds":30,"done":true,"slow":false,"stuck":false,"code":"CAT-INST-16","total_steps":7,"steps":[],
"say":"安装失败：CAT-INST-16 健康检查没通过。这次建出来的应用和数据卷已清理。","next":"x","next_command":"acornfox faults CAT-INST-16 --json"}}`
	h, _ := waitHarness(t, failed)
	code, out, _ := h.run("--json", "install", "status", "t9", "--wait")
	m := decodeJSON(t, out)
	if code != exitOpFailed || m["done"] != true || m["next_command"] != "acornfox faults CAT-INST-16 --json" ||
		m["timing"].(map[string]any)["clock"] != "server" || m["waited_seconds"] != float64(0) || m["timed_out"] != false {
		t.Fatalf("exit %d out %s", code, out)
	}
}

// Human output of a running task: the sentence for the user and how to wait.
func TestInstallStatusHumanRunning(t *testing.T) {
	virtualClock(t)
	h, _ := waitHarness(t, runningTask)
	code, out, _ := h.run("install", "status", "5961fd8ff351")
	if code != exitOK || !strings.Contains(out, "还在装：hermes-agent 正在“启动”") || !strings.Contains(out, "接着等：acornfox install status 5961fd8ff351 --wait") {
		t.Fatalf("exit %d out %s", code, out)
	}
}

// --no-wait hands over the command that waits.
func TestInstallNoWaitNextCommand(t *testing.T) {
	h, _ := waitHarness(t, runningTask)
	code, out, _ := h.run("--json", "install", "new-api", "--no-wait")
	m := decodeJSON(t, out)
	if code != exitOK || m["done"] != false || !strings.HasPrefix(m["next_command"].(string), "acornfox --json install status ") || !strings.HasSuffix(m["next_command"].(string), " --wait") {
		t.Fatalf("exit %d out %s", code, out)
	}
}
