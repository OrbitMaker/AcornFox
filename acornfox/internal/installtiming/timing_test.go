package installtiming

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 11, 4, 10, 39, 0, time.UTC)

func at(s int) *time.Time {
	x := t0.Add(time.Duration(s) * time.Second)
	return &x
}

// steps of one app: every step before name is done, name is running since
// started (seconds after t0), the rest pending.
func steps(app, running string, started int) []Step {
	names := []struct{ n, l string }{{"check", "检查"}, {"create", "建应用"}, {"image", "下载镜像"}, {"files", "写配置"}, {"start", "启动"}, {"health", "健康检查"}, {"init", "初始化"}}
	var out []Step
	state := "done"
	for _, x := range names {
		s := Step{App: app, Name: x.n, Label: x.l, Status: state}
		switch {
		case x.n == running:
			s.Status, s.StartedAt = "running", at(started)
			state = "pending"
		case state == "done":
			s.StartedAt, s.FinishedAt = at(0), at(1)
		}
		out = append(out, s)
	}
	return out
}

func TestEveryStepHasTimesAndSource(t *testing.T) {
	for _, name := range []string{"check", "create", "image", "image.gateway", "files", "start", "health", "init", "reuse"} {
		l, ok := Limits[name]
		if !ok {
			t.Fatalf("没有 %s 的时间", name)
		}
		if l.Normal <= 0 || l.StuckAfter <= l.Normal || l.Typical == "" || l.Source == "" {
			t.Errorf("%s：%+v（要有正常时长、更长的卡住门槛、说法和出处）", name, l)
		}
	}
	// The health threshold is past the longest health wait a published
	// manifest allows (Hermes: timeout_s 300), the start one under the
	// engine's 15 minutes, the image one under the task's 60 minutes.
	// A pull from the public sources is cut off at 10 minutes, so its stuck
	// threshold is past that; a gateway download is not.
	if Limits["health"].StuckAfter <= 300*time.Second || Limits["start"].StuckAfter >= 15*time.Minute || Limits["image.gateway"].StuckAfter >= 60*time.Minute ||
		Limits["image"].StuckAfter <= 10*time.Minute || Limits["image"].Normal >= 10*time.Minute {
		t.Fatalf("门槛和引擎自己的时限对不上：%+v", Limits)
	}
}

// The case of the first Haiku walk: 12 seconds into a reinstall on old data
// the task is in "启动". The verdict says it is running normally, the next
// command waits, and nothing is final.
func TestRunningNormal(t *testing.T) {
	task := Task{ID: "5961fd8ff351", Status: StatusRunning, CreatedAt: t0, Steps: steps("hermes-agent", "start", 4)}
	v := Assess(task, t0.Add(12*time.Second), "server")
	if v.Done || v.Stuck || v.Slow || v.Code != "" || v.Step == nil || v.Step.Name != "start" || v.Step.Index != 5 || v.TotalSteps != 7 {
		t.Fatalf("%+v", v)
	}
	if v.ElapsedSeconds != 12 || v.Step.ElapsedSeconds != 8 || v.Step.NormalSeconds != 60 || v.Step.StuckAfterSeconds != 300 {
		t.Fatalf("times %+v / %+v", v, *v.Step)
	}
	if v.NextCommand != "acornfox --json install status 5961fd8ff351 --wait" || v.Next != NextRunning {
		t.Fatalf("next %q %q", v.Next, v.NextCommand)
	}
	want := "还在装：hermes-agent 正在“启动”（第 5/7 步），这一步已用 8 秒。整次已用 12 秒。"
	if v.Say != want {
		t.Fatalf("say %q", v.Say)
	}
	if len(v.Steps) != 5 || v.Steps[0].ElapsedSeconds != 1 || v.Steps[0].Status != "done" || v.Steps[0].Stuck {
		t.Fatalf("steps %+v", v.Steps)
	}
	if v.MustTell != nil {
		t.Fatalf("must_tell before the end: %v", v.MustTell)
	}
}

func TestRunningSlowAndStuck(t *testing.T) {
	task := Task{ID: "t1", Status: StatusRunning, CreatedAt: t0, Steps: steps("new-api", "start", 0)}
	v := Assess(task, t0.Add(90*time.Second), "server")
	if !v.Slow || v.Stuck || v.Code != "" || v.NextCommand != WaitCommand("t1") || !strings.Contains(v.Say, "慢一些，还在进行") {
		t.Fatalf("slow: %+v", v)
	}
	v = Assess(task, t0.Add(301*time.Second), "server")
	if !v.Stuck || v.Code != CodeStuck || v.Next != NextStuck || v.NextCommand != "acornfox logs new-api --tail 200 --json" ||
		!strings.Contains(v.Say, "5 分 1 秒") || !strings.Contains(v.Say, "可能卡住了，但安装还没有失败") {
		t.Fatalf("stuck: %+v", v)
	}
	// The image step looks at doctor, not at logs (there is no container yet).
	task.Steps = steps("new-api", "image", 0)
	if v = Assess(task, t0.Add(7*time.Minute), "server"); !v.Slow || v.Stuck {
		t.Fatalf("public pull at 7 min (a real one took 422 s): %+v", v)
	}
	v = Assess(task, t0.Add(13*time.Minute), "server")
	if !v.Stuck || v.NextCommand != "acornfox doctor --json" || v.Step.StuckAfterSeconds != 720 {
		t.Fatalf("image stuck: %+v", v)
	}
	// Once done, the image step is timed by how it came.
	if l := LimitFor(Step{Name: "image", Message: "从下载网关下载"}); l.StuckAfter != 40*time.Minute {
		t.Fatalf("gateway image done: %+v", l)
	}
}

// A download through the gateway that is moving is not stuck however long it
// takes (within the image limit); no byte for two minutes is.
func TestDownloadStall(t *testing.T) {
	task := Task{ID: "t2", Status: StatusRunning, CreatedAt: t0, Steps: steps("hermes-agent", "image", 0)}
	task.Steps[2].Message = "从下载网关下载 312 MB / 1.0 GB（29%，25 MB/s，还要约 30 秒）"
	task.Steps[2].Progress = &Progress{Phase: "download", DoneBytes: 312 << 20, TotalBytes: 1 << 30, BytesPerSec: 25 << 20, ETASeconds: 30}
	v := Assess(task, t0.Add(15*time.Second), "server")
	if v.Slow || v.Stuck || !strings.Contains(v.Say, "：从下载网关下载 312 MB") {
		t.Fatalf("moving: %+v", v)
	}
	if v = Assess(task, t0.Add(15*time.Minute), "server"); v.Slow || v.Stuck || v.Step.NormalSeconds != 1200 {
		t.Fatalf("moving for 15 min (slow network, within the gateway limits): %+v", v)
	}
	task.Steps[2].Progress.StalledSeconds = 45
	if v = Assess(task, t0.Add(60*time.Second), "server"); !v.Slow || v.Stuck || v.Step.StalledSeconds != 45 {
		t.Fatalf("stalled 45 s: %+v", v)
	}
	task.Steps[2].Progress.StalledSeconds = 121
	if v = Assess(task, t0.Add(150*time.Second), "server"); !v.Stuck || v.Code != CodeStuck {
		t.Fatalf("stalled 2 min: %+v", v)
	}
}

// Succeeded: final, the old token that could not be revoked and the no-quota
// token are must-tells, the other carried lines are not.
func TestSucceededMustTell(t *testing.T) {
	notRevoked := NotRevokedPrefix + "以前在 new-api 里给 hermes-agent 的令牌（编号 1，结尾 rvRH）：new-api 停着。这把令牌不限额、不过期"
	task := Task{ID: "t3", Status: StatusSucceeded, CreatedAt: t0, FinishedAt: at(18), Steps: steps("hermes-agent", "", 0),
		Apps: []App{{Name: "hermes-agent", Carried: []string{"接上了旧数据（af-hermes-agent-1）", notRevoked}},
			{Name: "other", TokenProvider: "new-api"}}}
	v := Assess(task, t0.Add(time.Hour), "server")
	if !v.Done || v.Stuck || v.Slow || v.Step != nil || v.ElapsedSeconds != 18 || v.Say != "已装好，整次用了 18 秒。" || v.NextCommand != "" || v.Next != NextSucceeded {
		t.Fatalf("%+v", v)
	}
	if len(v.MustTell) != 2 || v.MustTell[0] != notRevoked || v.MustTell[1] != TokenNote("other", "new-api") {
		t.Fatalf("must_tell %q", v.MustTell)
	}
}

func TestFailed(t *testing.T) {
	task := Task{ID: "t4", Status: StatusFailed, CreatedAt: t0, FinishedAt: at(30), FaultCode: "CAT-INST-16", FaultMsg: "健康检查没通过",
		Steps: steps("new-api", "", 0)}
	v := Assess(task, t0.Add(time.Hour), "client")
	if !v.Done || v.Code != "CAT-INST-16" || v.NextCommand != "acornfox faults CAT-INST-16 --json" || v.Clock != "client" ||
		v.Say != "安装失败：CAT-INST-16 健康检查没通过。这次建出来的应用和数据卷已清理。" {
		t.Fatalf("%+v", v)
	}
	task.KeepVolumes = true
	if v = Assess(task, t0, "server"); !strings.Contains(v.Say, "数据卷按要求留着") {
		t.Fatalf("kept volumes: %q", v.Say)
	}
}

// A running task between steps, a server clock behind the step's start and a
// step name this version does not know do not break the verdict.
func TestOddCases(t *testing.T) {
	v := Assess(Task{ID: "t5", Status: StatusRunning, CreatedAt: t0, Steps: steps("a", "", 0)}, t0.Add(3*time.Second), "server")
	if v.Done || v.Step != nil || v.NextCommand != WaitCommand("t5") || !strings.Contains(v.Say, "正在准备下一步") {
		t.Fatalf("between steps: %+v", v)
	}
	task := Task{ID: "t6", Status: StatusRunning, CreatedAt: t0, Steps: []Step{{App: "a", Name: "warp", Label: "新步骤", Status: "running", StartedAt: at(10)}}}
	v = Assess(task, t0, "client")
	if v.Step == nil || v.Step.ElapsedSeconds != 0 || v.Step.NormalSeconds != 60 || v.Stuck {
		t.Fatalf("clock behind / unknown step: %+v", v)
	}
	// A running step recorded without a start time still is the current step.
	task.Steps[0].StartedAt = nil
	if v = Assess(task, t0.Add(time.Hour), "server"); v.Step == nil || v.Step.ElapsedSeconds != 0 || v.Stuck {
		t.Fatalf("no start time: %+v", v)
	}
}

func TestDurText(t *testing.T) {
	for s, want := range map[int]string{0: "0 秒", 59: "59 秒", 60: "1 分钟", 125: "2 分 5 秒", 3720: "1 小时 2 分"} {
		if got := durText(s); got != want {
			t.Errorf("%d → %q, want %q", s, got, want)
		}
	}
}
