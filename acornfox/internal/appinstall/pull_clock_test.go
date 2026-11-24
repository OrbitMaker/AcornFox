package appinstall

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/installtiming"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// shortLimits shortens the task's limit (and the wait on one pull) for a test.
func shortLimits(t *testing.T, task, pull time.Duration) {
	t.Helper()
	oldTask, oldPull := installTaskLimit, pullStepLimit
	installTaskLimit, pullStepLimit = task, pull
	t.Cleanup(func() { installTaskLimit, pullStepLimit = oldTask, oldPull })
}

// Pulling from the public sources does not count toward the task's limit: a
// recipe whose two pulls together take three times the limit is installed.
// The limit leaves 2 s for the other steps so a busy test machine does not
// run out of it.
func TestPullTimeNotCountedInTaskLimit(t *testing.T) {
	shortLimits(t, 2*time.Second, time.Minute)
	v := setup(t)
	v.w.mu.Lock()
	v.w.pullDelay = 3 * time.Second
	v.w.mu.Unlock()
	t0 := time.Now()
	tv := v.install(t, Request{Recipe: "hermes-with-new-api", Params: hermesModel()})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("%s %+v", tv.Status, tv.Fault)
	}
	if el := time.Since(t0); el < 6*time.Second || len(v.w.pulled) != 2 {
		t.Fatalf("took %v, pulled %v: the pulls did not run as long as the test needs", el, v.w.pulled)
	}
}

// Every other step still counts: a step that outlasts the limit is cancelled
// with the same fault as when the limit was a plain deadline (the context
// of earlier versions, context.WithTimeout, run side by side).
func TestOtherStepsStillLimited(t *testing.T) {
	// The limit must be long enough for the steps before "start" to finish
	// on a busy machine, and short enough to keep the test quick: the start
	// step waits for it. (When the limit ran out in the middle of a store
	// read, the wait used to report the read as failed; it now reports the
	// cancelled wait whenever the context has ended.)
	shortLimits(t, time.Second, time.Minute)
	run := func() (TaskView, time.Duration) {
		v := setup(t)
		v.w.mu.Lock()
		v.w.noLive = true // the deployment never goes live: the start step waits
		v.w.mu.Unlock()
		t0 := time.Now()
		tv := v.install(t, Request{App: "new-api"})
		return tv, time.Since(t0)
	}
	now, nowTook := run()
	old := taskContext
	taskContext = context.WithTimeout
	before, _ := run()
	taskContext = old
	if now.Status != state.TaskFailed || now.Fault == nil || before.Fault == nil {
		t.Fatalf("now %+v before %+v", now.Fault, before.Fault)
	}
	if now.Fault.Code != CodeStart || now.Fault.Code != before.Fault.Code || now.Fault.Message != before.Fault.Message ||
		now.Fault.Hint != before.Fault.Hint {
		t.Fatalf("now %+v\nbefore %+v", now.Fault, before.Fault)
	}
	if s := stepOf(now, "new-api", "start"); s == nil || s.Status != StepFailed || s.Message != stepOf(before, "new-api", "start").Message {
		t.Fatalf("start step %+v", s)
	}
	if nowTook > 10*time.Second {
		t.Fatalf("the limit ended the task only after %v", nowTook)
	}
}

// The clock stands still while paused, nested pauses included; when it runs
// out the context ends like a deadline's, and so does one derived from it.
func TestTaskClock(t *testing.T) {
	ctx, cancel := newTaskClock(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("a moving limit must not be sent as a deadline")
	}
	child, cc := context.WithCancel(ctx)
	defer cc()
	r1 := pauseClock(ctx)
	r2 := pauseClock(child) // nested, through a derived context
	time.Sleep(400 * time.Millisecond)
	r2()
	if ctx.Err() != nil {
		t.Fatal("the clock ran while paused")
	}
	r1()
	r1() // twice is once
	if left, paused, ok := clockLeft(ctx); !ok || left > 200*time.Millisecond || left < 50*time.Millisecond || paused < 350*time.Millisecond {
		t.Fatalf("left %v paused %v", left, paused)
	}
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the clock did not run out")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) || !errors.Is(child.Err(), context.DeadlineExceeded) {
		t.Fatalf("errors %v %v", ctx.Err(), child.Err())
	}
	pauseClock(context.Background())() // no clock: nothing

	// The parent ending ends the task with the parent's error.
	parent, pc := context.WithCancel(context.Background())
	ctx2, cancel2 := newTaskClock(parent, time.Hour)
	defer cancel2()
	pc()
	<-ctx2.Done()
	if !errors.Is(ctx2.Err(), context.Canceled) {
		t.Fatalf("parent ended: %v", ctx2.Err())
	}
	// The longest a task can take is known.
	if MaxTaskDuration(2) != installTaskLimit+2*pullStepLimit {
		t.Fatal("MaxTaskDuration")
	}
}

// Stopped by one image's limit (the runner's 50 minutes): the fault names the
// image, what each source got to, and what to do.
func TestPullStoppedByImageLimit(t *testing.T) {
	v := setup(t)
	v.w.mu.Lock()
	v.w.failPull = true
	v.w.pullFailure = &runner.Failure{
		Stage: "image", Code: "pull_timeout", Reason: runner.ReasonPullDeadline, PullSeconds: 3000,
		Message: "拉取镜像超时：50 分钟还没拉完。镜像没拉下来（试了 2 个源）：dockerproxy.net 太慢（最近 2 分钟平均 40 KB/s，已下 12 MB/319 MB），换下一个源；registry-1.docker.io 到了一个镜像的总时限（50 分钟）还没拉完（已下 188 MB/304 MB）",
		Hint:    "镜像源都很慢：换个时间再装",
		Sources: []runner.SourceResult{
			{Host: "dockerproxy.net", Outcome: "slow", Message: "dockerproxy.net 太慢（最近 2 分钟平均 40 KB/s，已下 12 MB/319 MB），换下一个源"},
			{Host: "registry-1.docker.io", Outcome: "timeout", Message: "registry-1.docker.io 到了一个镜像的总时限（50 分钟）还没拉完（已下 188 MB/304 MB）"},
		},
	}
	v.w.mu.Unlock()
	tv := v.install(t, Request{App: "new-api"})
	f := tv.Fault
	if tv.Status != state.TaskFailed || f == nil || f.Code != CodePull {
		t.Fatalf("%+v %+v", tv.Status, f)
	}
	a, _ := v.e.cfg.Catalog.App(context.Background(), "new-api")
	for _, want := range []string{a.Manifest.Release.Images[0].Repository + "@", "50 分钟还没拉完", "已下 188 MB/304 MB", "已下 12 MB/319 MB", "试了 2 个源"} {
		if !strings.Contains(f.Message, want) {
			t.Fatalf("message lacks %q: %s", want, f.Message)
		}
	}
	if strings.Contains(f.Message, "整次安装") || !strings.Contains(f.Hint, "换个时间再装") || len(f.Attempts) != 2 || f.Attempts[1].Code != "timeout" {
		t.Fatalf("hint %q attempts %+v message %s", f.Hint, f.Attempts, f.Message)
	}
}

// The runner does not answer within its limit and the time for the answer;
// the task itself ending; a plain transport error.
func TestPullNoAnswer(t *testing.T) {
	shortLimits(t, time.Hour, 100*time.Millisecond)
	v := setup(t)
	v.w.mu.Lock()
	v.w.pullDelay = 5 * time.Second
	v.w.mu.Unlock()
	p, tr := imageStepFor(t, v, "new-api")
	t0 := time.Now()
	_, _, err := tr.image(context.Background(), p, v.w)
	f := AsFault(err)
	if f == nil || f.Code != CodePull || !strings.Contains(f.Message, "运行器也没有回话") || !strings.Contains(f.Message, "50 分钟") ||
		f.Hint != pullNoAnswerHint || time.Since(t0) > 2*time.Second {
		t.Fatalf("%+v", f)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = tr.image(ctx, p, v.w)
	if f := AsFault(err); f == nil || !strings.Contains(f.Message, "拉镜像时安装被取消") {
		t.Fatalf("%+v", f)
	}
	v.w.mu.Lock()
	v.w.pullDelay, v.w.pullErr = 0, runner.ErrUnavailable
	v.w.mu.Unlock()
	_, _, err = tr.image(context.Background(), p, v.w)
	if f := AsFault(err); f == nil || !strings.Contains(f.Message, "拉镜像失败") || !errors.Is(err, runner.ErrUnavailable) {
		t.Fatalf("%+v", err)
	}
}

// statusWorld is the fake world with a runner that reports how far a pull is.
type statusWorld struct {
	*fakeWorld
	mu     sync.Mutex
	status runner.PullStatus
	asked  []string // app/deployment of each status request
}

func (s *statusWorld) PullStatus(_ context.Context, app, dep string) (runner.PullStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, app+"/"+dep)
	if s.status.Phase == "" {
		return runner.PullStatus{}, runner.ErrNotFound
	}
	return s.status, nil
}

// While the image is pulled, the image step says which source pulls, how far
// and how fast Docker says it is (progress phase pull), and counts for the
// timing as a pull, not as a download through the gateway.
func TestPullStatusShownInStep(t *testing.T) {
	old := pullStatusEvery
	pullStatusEvery = 20 * time.Millisecond
	t.Cleanup(func() { pullStatusEvery = old })
	var sw *statusWorld
	v := setupWith(t, func(c *Config) {
		sw = &statusWorld{fakeWorld: c.Runner.(*fakeWorld)}
		c.Runner = sw
	})
	sw.status = runner.PullStatus{Phase: "pull", Source: "dockerproxy.net", SourceIndex: 2, Sources: 3, Bytes: 120 << 20, Total: 304 << 20,
		BytesPerSec: 2 << 20, Seconds: 185, Earlier: []string{"docker.1panel.live 太慢（最近 2 分钟平均 40 KB/s），换下一个源"}}
	v.w.mu.Lock()
	v.w.pullDelay = time.Second
	v.w.mu.Unlock()
	tv, err := v.e.StartInstall(context.Background(), Request{App: "new-api"})
	if err != nil {
		t.Fatal(err)
	}
	var got *StepView
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && got == nil; time.Sleep(10 * time.Millisecond) {
		cur, _ := v.e.Task(context.Background(), tv.ID)
		if s := stepOf(cur, "new-api", "image"); s != nil && s.Progress != nil {
			cp := *s
			got = &cp
		}
	}
	if got == nil {
		t.Fatal("the image step never showed the pull's status")
	}
	for _, want := range []string{"从 dockerproxy.net 拉", "（第 2 个源，共 3 个）", "已下 120 MB / 304 MB（39%，最近 2.0 MB/s）", "这个镜像已拉 3 分 5 秒", "前一个源：docker.1panel.live 太慢"} {
		if !strings.Contains(got.Message, want) {
			t.Fatalf("message lacks %q: %s", want, got.Message)
		}
	}
	p := got.Progress
	if p.Phase != installtiming.PhasePull || p.Source != "dockerproxy.net" || p.DoneBytes != 120<<20 || p.TotalBytes != 304<<20 || p.PullSeconds != 185 {
		t.Fatalf("progress %+v", p)
	}
	if lim := installtiming.LimitFor(installtiming.Step{Name: "image", Progress: p}); lim != installtiming.Limits["image"] {
		t.Fatalf("a pull counted as %+v", lim)
	}
	end := v.wait(t, tv.ID)
	if end.Status != state.TaskSucceeded || stepOf(end, "new-api", "image").Progress != nil {
		t.Fatalf("%+v", end)
	}
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if len(sw.asked) == 0 || !strings.HasPrefix(sw.asked[0], "new-api/") {
		t.Fatalf("asked %v", sw.asked)
	}
}

// The words for the other states: measuring the sources, Docker silent (no
// figure made up), no progress yet.
func TestPullStatusText(t *testing.T) {
	msg, p := pullStatusText("demo/app:1", runner.PullStatus{Phase: "probe", Seconds: 4})
	if !strings.Contains(msg, "先实测各个镜像源") || p.Phase != installtiming.PhasePull || p.DoneBytes != 0 {
		t.Fatalf("%s %+v", msg, p)
	}
	msg, p = pullStatusText("demo/app:1", runner.PullStatus{Phase: "pull", Source: "dockerproxy.net", SourceIndex: 1, Sources: 1,
		Bytes: 80 << 20, Total: 232 << 20, BytesPerSec: 5 << 20, Blind: true, Seconds: 600})
	if !strings.Contains(msg, "Docker 这会儿没报进度，看不到拉到多少（最后报的是 80 MB / 232 MB）") || strings.Contains(msg, "MB/s") ||
		strings.Contains(msg, "第 1 个源") || !p.Blind || p.BytesPerSec != 0 {
		t.Fatalf("%s %+v", msg, p)
	}
	msg, _ = pullStatusText("demo/app:1", runner.PullStatus{Phase: "pull", Source: "docker.m.daocloud.io", SourceIndex: 1, Sources: 4, Seconds: 12})
	if !strings.Contains(msg, "已连上，还没收到进度") {
		t.Fatal(msg)
	}
	msg, _ = pullStatusText("demo/app:1", runner.PullStatus{Phase: "pull", Source: "docker.m.daocloud.io", SourceIndex: 1, Sources: 4,
		Bytes: 10 << 20, Total: 100 << 20, QuietSeconds: 30, Seconds: 90})
	if !strings.Contains(msg, "30 秒没有新进度") {
		t.Fatal(msg)
	}
}

// imageStepFor sets up app's image step alone (no gateway: the public
// sources only).
func imageStepFor(t *testing.T, v *env, app string) (*planned, *taskRun) {
	t.Helper()
	a, err := v.e.cfg.Catalog.App(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	p := &planned{app: a, name: app}
	tr := &taskRun{e: v.e, id: "0123456789ee", view: TaskView{Apps: []AppResult{{App: app}}}}
	if _, err := v.w.st.CreateInstallTask(context.Background(), state.InstallTask{ID: tr.id, Kind: "app", Target: app, Status: state.TaskRunning, Data: "{}"}); err != nil {
		t.Fatal(err)
	}
	return p, tr
}
