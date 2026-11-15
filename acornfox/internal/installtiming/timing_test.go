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
	// A pull from the public sources is cut off at 50 minutes over all
	// sources, so its stuck threshold is past that and under the task's 60;
	// a gateway download is not cut off.
	if Limits["health"].StuckAfter <= 300*time.Second || Limits["start"].StuckAfter >= 15*time.Minute || Limits["image.gateway"].StuckAfter >= 60*time.Minute ||
		Limits["image"].StuckAfter <= 50*time.Minute || Limits["image"].StuckAfter >= 60*time.Minute || Limits["image"].Normal >= 20*time.Minute {
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
	if v = Assess(task, t0.Add(7*time.Minute), "server"); v.Slow || v.Stuck {
		t.Fatalf("public pull at 7 min (a real one took 422 s) is normal: %+v", v)
	}
	if v = Assess(task, t0.Add(20*time.Minute), "server"); !v.Slow || v.Stuck {
		t.Fatalf("public pull at 20 min (a home line took up to 32 min) is slow, not stuck: %+v", v)
	}
	v = Assess(task, t0.Add(53*time.Minute), "server")
	if !v.Stuck || v.NextCommand != "acornfox doctor --json" || v.Step.StuckAfterSeconds != 3120 {
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

// A pull from the public sources (progress phase pull) counts as the plain
// image step, not as a download through the gateway, and is judged stuck by
// its own time: the minutes spent on the gateway before it do not count. A
// slow network's long pull (longer than the task's 60 minutes would have
// allowed together with the other steps) is not called stuck.
func TestPublicPull(t *testing.T) {
	task := Task{ID: "t3", Status: StatusRunning, CreatedAt: t0, Steps: steps("demo-app", "image", 0)}
	task.Steps[2].Message = "从 dockerproxy.net 拉 demo/app:1（第 2 个源，共 3 个）：已下 120 MB / 304 MB（39%，最近 85 KB/s），这个镜像已拉 40 分 0 秒"
	task.Steps[2].Progress = &Progress{Phase: PhasePull, DoneBytes: 120 << 20, TotalBytes: 304 << 20, BytesPerSec: 85 << 10, Source: "dockerproxy.net", PullSeconds: 2400}
	if LimitFor(task.Steps[2]) != Limits["image"] {
		t.Fatal("a public pull counted as the gateway's download")
	}
	// The step began 55 minutes ago (the gateway was tried first), the pull 40.
	v := Assess(task, t0.Add(55*time.Minute), "server")
	if v.Stuck || !v.Slow || !strings.Contains(v.Say, "已下 120 MB / 304 MB") || v.Next != NextRunning {
		t.Fatalf("pulling for 40 min: %+v", v)
	}
	task.Steps[2].Progress.PullSeconds = 53 * 60
	if v = Assess(task, t0.Add(55*time.Minute), "server"); !v.Stuck || v.Code != CodeStuck {
		t.Fatalf("pulled 53 min, past one image's limit: %+v", v)
	}
	// Without the pull's own time (an older server) the step's time decides, as before.
	task.Steps[2].Progress = nil
	if v = Assess(task, t0.Add(55*time.Minute), "server"); !v.Stuck {
		t.Fatalf("no progress: %+v", v)
	}
}

// Succeeded: final; a credential not taken back (a line beginning with
// RevokeCode, of any app of the task) is a must-tell, the other carried
// lines are not.
func TestSucceededMustTell(t *testing.T) {
	notTaken := RevokeCode + " consumer-app 的“model”：以前 provider-app 发给它的那份凭据没通过检查，已经换了一份新的；旧的那份（名字或备注里带编号 0a1b2c3d）没有删，要不要删请到 provider-app 里看"
	revokeFailed := RevokeCode + " 没能收回发给 other 的凭据（应用停着）：请到 provider-app 里手工删掉给 other 建的那一项"
	task := Task{ID: "t3", Status: StatusSucceeded, CreatedAt: t0, FinishedAt: at(18), Steps: steps("consumer-app", "", 0), HasSecrets: true,
		Apps: []App{{Name: "consumer-app", Carried: []string{"接上了旧数据（af-consumer-app-1）", notTaken}},
			{Name: "other", Carried: []string{revokeFailed, "沿用以前改过的平台变量：platform.tz=Asia/Tokyo"}}}}
	v := Assess(task, t0.Add(time.Hour), "server")
	if !v.Done || v.Stuck || v.Slow || v.Step != nil || v.ElapsedSeconds != 18 || v.Say != "已装好，整次用了 18 秒。" || v.NextCommand != "" || v.Next != NextSucceeded ||
		v.ShowSecretsCommand != "acornfox install status t3 --show-secrets" {
		t.Fatalf("%+v", v)
	}
	if len(v.MustTell) != 2 || v.MustTell[0] != notTaken || v.MustTell[1] != revokeFailed {
		t.Fatalf("must_tell %q", v.MustTell)
	}
}

// What the providers of the services the apps were plugged into wrote for
// the user (App.Tell) goes into must_tell after the credentials not taken
// back, each sentence once even when two apps use the same service.
func TestSucceededMustTellProviderAsk(t *testing.T) {
	ask := "在提供方应用里加好上游后，填一个要用的模型名"
	revokeFailed := RevokeCode + " 没能收回发给 other 的凭据（应用停着）：请到 provider-app 里手工删掉给 other 建的那一项"
	task := Task{ID: "t5", Status: StatusSucceeded, CreatedAt: t0, FinishedAt: at(10), Steps: steps("a", "", 0),
		Apps: []App{{Name: "a", Tell: []string{ask}}, {Name: "b", Carried: []string{revokeFailed}, Tell: []string{ask, " "}}}}
	v := Assess(task, t0.Add(time.Hour), "server")
	if len(v.MustTell) != 2 || v.MustTell[0] != revokeFailed || v.MustTell[1] != ask {
		t.Fatalf("must_tell %q", v.MustTell)
	}
	// Not before the end.
	task.Status, task.FinishedAt = "running", nil
	if v = Assess(task, t0, "server"); v.MustTell != nil {
		t.Fatalf("must_tell while running: %q", v.MustTell)
	}
}

// The verdict on a finished install says how to tell the user, in order:
// the login (no password; the command that shows it is a field of its own,
// with the real task id), how to open it by open_hint (ask first when there
// is a question; the address after switching does not open before), then
// user_steps and must_tell. Next names fields and the order only: no
// command sits inside its sentences.
func TestNextSucceededSaysOpenHint(t *testing.T) {
	if c := ShowSecretsCommand("5961fd8ff351"); c != "acornfox install status 5961fd8ff351 --show-secrets" {
		t.Errorf("show secrets: %s", c)
	}
	for _, want := range []string{"first_login", "账号和地址", "url_note", "show_secrets_command", "open_hint", "ask", "ask_yes_command",
		"lan_url_after_expose", "打不开", "command", "tunnel_command", "代码块", "apps[].url", "user_steps", "must_tell", "照原话",
		"apps[].bindings", "command_where", "tunnel_where", "用户自己电脑上运行", "不要说成在服务器上运行",
		"apps[].internal", "不用打开", "只在内部网络", "给别的应用用"} {
		if !strings.Contains(NextSucceeded, want) {
			t.Errorf("NextSucceeded lacks %q", want)
		}
	}
	for _, not := range []string{"acornfox ", "--show-secrets", "--expose", "<task_id>", "open_hint.lan_url"} {
		if strings.Contains(NextSucceeded, not) {
			t.Errorf("NextSucceeded has %q in it: %s", not, NextSucceeded)
		}
	}
	// No password among the first-login fields (the app lets the user make
	// their own account): no command, nothing sends the user to look one up.
	plain := Task{ID: "t8", Status: StatusSucceeded, CreatedAt: t0, FinishedAt: at(5), Steps: steps("a", "", 0)}
	if v := Assess(plain, t0, "server"); v.ShowSecretsCommand != "" {
		t.Errorf("command without a secret: %q", v.ShowSecretsCommand)
	}
	plain.HasSecrets = true
	if v := Assess(plain, t0, "server"); v.ShowSecretsCommand != "acornfox install status t8 --show-secrets" {
		t.Errorf("command with a secret: %q", v.ShowSecretsCommand)
	}
	// No task id, no command to fill in.
	task := Task{Status: StatusSucceeded, CreatedAt: t0, FinishedAt: at(5), Steps: steps("a", "", 0), HasSecrets: true}
	if v := Assess(task, t0, "server"); v.ShowSecretsCommand != "" {
		t.Errorf("command without an id: %q", v.ShowSecretsCommand)
	}
	// Only once succeeded.
	task.ID, task.Status, task.FinishedAt = "t9", "running", nil
	if v := Assess(task, t0, "server"); v.ShowSecretsCommand != "" {
		t.Errorf("command while running: %q", v.ShowSecretsCommand)
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
