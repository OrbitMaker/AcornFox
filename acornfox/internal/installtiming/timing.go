// Package installtiming says how long each step of an app center install
// normally takes and when a step counts as stuck, and turns an install
// task's step times into a verdict that can be acted on without reasoning:
// whether the task is over, whether the running step is slow or stuck, how
// long every step has taken, one sentence to tell the user, what to do next
// (with the exact command) and the lines that must reach the user word for
// word.
//
// The server works the verdict out by its own clock for every task it
// returns (GET /v1/catalog/install and GET /v1/catalog/install/{task}, field
// "timing"; never stored). `acornfox install status` prints it and, with
// --wait, waits for the task to end first. A server older than this package
// sends no verdict: the command then works it out itself with this package,
// by the computer's own clock (Verdict.Clock says which).
//
// The step times below are the single source: the fault entry CAT-INST-30,
// the Skill and the docs point to the fields (timing.step.normal_seconds,
// stuck_after_seconds …), they do not repeat the numbers.
package installtiming

import (
	"fmt"
	"strings"
	"time"
)

// CodeStuck is the fault code of a running step past its stuck threshold. It
// is a warning carried in the verdict, not an error: the task goes on, and
// the engine's own time limits end it (CAT-INST-15, -16, -17, the task's
// 60 minutes).
const CodeStuck = "CAT-INST-30"

// NotRevokedPrefix begins the line an install puts in apps[].carried when it
// could not revoke a token an earlier app of the name had in a local model
// gateway. Such a line is a must-tell (the token has no quota and no expiry,
// and nothing will try again).
const NotRevokedPrefix = "没能吊销"

// Task statuses and the running step status, as the engine writes them.
const (
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	stepRunning     = "running"
)

// Limit is how long one step normally takes.
type Limit struct {
	Normal     time.Duration // the slowest normal run measured, rounded up: past it the step is "slow"
	StuckAfter time.Duration // past this the running step counts as stuck (CAT-INST-30)
	Typical    string        // the normal time in words, for the user
	Source     string        // where the numbers come from
}

// Limits are the steps' times, by step name (the names of the engine's
// steps). Measured on test VMs (2 cores, 4 GB, Ubuntu 24.04) in 2026-11 with
// the published New API and Hermes manifests.
var Limits = map[string]Limit{
	"check": {
		Normal: 10 * time.Second, StuckAfter: time.Minute,
		Typical: "不到 1 秒",
		Source:  "只查本机的数据库：测试机上 New API、Hermes 各装过十余次，每次不到 1 秒",
	},
	"create": {
		Normal: 20 * time.Second, StuckAfter: 2 * time.Minute,
		Typical: "几秒（要在本机 New API 里发令牌时多几秒）",
		Source:  "测试机上每次不到 2 秒；要在本机 New API 里发令牌时，登录和建令牌各一次请求，每次最多等 30 秒（安装引擎的 HTTP 超时），两次加余量算 2 分钟",
	},
	// image without download progress: the image is here already, or it is
	// pulled from the public sources (no progress to show).
	"image": {
		Normal: 5 * time.Minute, StuckAfter: 12 * time.Minute,
		Typical: "本机已有时几秒；从公共源拉看不到进度，要看网速：小镜像几十秒到几分钟，1 GB 左右的要将近 10 分钟",
		Source:  "测试机实测（出口在境外，经国内镜像站）：本机已有不到 1 秒；New API（约 80 MB）从公共源 126 秒、422 秒，Hermes（约 1.1 GB）561 秒。从公共源拉一次最多等 10 分钟（运行器的拉取时限），到时判失败（CAT-INST-12），所以过了 12 分钟还在这一步、又没有下载进度就不对了（下载网关没成功、改从公共源拉时，前面还有试网关的时间）",
	},
	// image.gateway: the image archive downloaded through the download
	// gateway, with progress (download, then load).
	"image.gateway": {
		Normal: 20 * time.Minute, StuckAfter: 40 * time.Minute,
		Typical: "经下载网关下载有进度和剩余时间：1 GB 左右的镜像包网速好时一两分钟（下载加导入），网速慢的要十几分钟",
		Source:  "测试机实测：Hermes（约 1.1 GB）经下载网关下载约 40 秒，导入 46～75 秒。下载没有总时限，靠 60 秒收不到数据就断开接着下，所以按 1 MB/s 的慢网算 20 分钟以内正常、40 分钟算卡住（整次安装 60 分钟还没完会被取消）；两分钟一个字节都没收到也算卡住（DownloadStallStuck）",
	},
	"files": {
		Normal: 10 * time.Second, StuckAfter: time.Minute,
		Typical: "不到 1 秒",
		Source:  "往数据卷里写一两个小配置文件：测试机上每次不到 1 秒",
	},
	"start": {
		Normal: time.Minute, StuckAfter: 5 * time.Minute,
		Typical: "几秒到半分钟",
		Source:  "测试机实测：New API 6 秒，Hermes 21 秒，接旧数据重装 10～15 秒；安装引擎等 15 分钟还没上线才判失败（CAT-INST-15），过了 5 分钟已远超平常",
	},
	"health": {
		Normal: time.Minute, StuckAfter: 6 * time.Minute,
		Typical: "几秒到十几秒",
		Source:  "测试机实测：New API 4 秒，Hermes 10 秒；清单里最长的等待是 Hermes 的 300 秒（timeout_s），到时没通过就判失败（CAT-INST-16），过了 6 分钟还在这一步就不对了",
	},
	"init": {
		Normal: 20 * time.Second, StuckAfter: 4 * time.Minute,
		Typical: "几秒",
		Source:  "测试机上不到 1 秒（New API 还要关自助注册，也在 1 秒内）；初始化请求连不上时最多试 5 次、每次最多等 30 秒，约 160 秒后判失败（CAT-INST-17），过了 4 分钟就不对了",
	},
	"reuse": {
		Normal: 5 * time.Second, StuckAfter: time.Minute,
		Typical: "不到 1 秒",
		Source:  "配方里已装的应用直接用，只记一笔：不到 1 秒",
	},
}

// unknownStep is used for a step name this version does not know (a newer
// server's).
var unknownStep = Limit{Normal: time.Minute, StuckAfter: 10 * time.Minute, Typical: "（这一版的命令行不认识这一步）",
	Source: "不认识的步骤按 1 分钟算慢、10 分钟算卡住"}

// Downloading an image archive through the download gateway reports how long
// no byte has come in (progress.stalled_seconds). The engine drops a try that
// receives nothing for 60 seconds and resumes it 2 to 30 seconds later; no
// byte for two minutes means resuming does not help.
const (
	DownloadStallSlow  = 30 * time.Second
	DownloadStallStuck = 2 * time.Minute
)

// LimitFor returns the times of step s: the image step has two sets, by
// whether the image comes through the download gateway (it reports progress
// while running; once done its message says so) or not.
func LimitFor(s Step) Limit {
	name := s.Name
	if name == "image" && (s.Progress != nil || strings.HasPrefix(s.Message, GatewayDone)) {
		name = "image.gateway"
	}
	if l, ok := Limits[name]; ok {
		return l
	}
	return unknownStep
}

// GatewayDone begins the image step's message when the image came through
// the download gateway.
const GatewayDone = "从下载网关下载"

// Task is what a verdict is worked out from (the server's TaskView and the
// client's InstallTask are turned into it).
type Task struct {
	ID          string
	Status      string // running | succeeded | failed
	CreatedAt   time.Time
	FinishedAt  *time.Time
	Steps       []Step
	Apps        []App
	FaultCode   string
	FaultMsg    string
	KeepVolumes bool // keep_volumes_on_failure
}

// Step is one step of one app.
type Step struct {
	App, Name, Label, Status, Message string
	StartedAt, FinishedAt             *time.Time
	Progress                          *Progress
}

// Progress is how far a running step is (the image archive downloaded
// through the download gateway). The engine's StepProgress is this type.
type Progress struct {
	Phase       string `json:"phase"` // download | load (docker load after the download)
	DoneBytes   int64  `json:"done_bytes"`
	TotalBytes  int64  `json:"total_bytes,omitempty"`
	BytesPerSec int64  `json:"bytes_per_sec,omitempty"`
	ETASeconds  int64  `json:"eta_seconds,omitempty"`
	// StalledSeconds is how long no byte has come in (download phase).
	StalledSeconds int64 `json:"stalled_seconds,omitempty"`
}

// App is one app of the task, for the lines to pass on.
type App struct {
	Name    string
	Carried []string
	// TokenProvider is the local model gateway (New API) the app has a token
	// in with no quota and no expiry (issued by this task or taken over from
	// the old app of the name); "" when none.
	TokenProvider string
}

// Verdict is the task's state worked out from its step times.
type Verdict struct {
	Now            time.Time `json:"now"`             // the clock it was worked out by
	Clock          string    `json:"clock"`           // server (the server's clock) or client (an older server sent none)
	ElapsedSeconds int       `json:"elapsed_seconds"` // since the task began (until it ended)
	// Done: succeeded or failed, the outcome is final. Until then nothing
	// about the result may be said.
	Done bool `json:"done"`
	// Slow: the running step is past its normal time. Stuck: past its stuck
	// threshold (Code is CAT-INST-30). Both false once done.
	Slow       bool       `json:"slow"`
	Stuck      bool       `json:"stuck"`
	Code       string     `json:"code,omitempty"` // CAT-INST-30 when stuck; the fault's code when failed
	TotalSteps int        `json:"total_steps"`
	Step       *StepTime  `json:"step,omitempty"` // the running step; absent when done or between steps
	Steps      []StepTime `json:"steps"`          // every step that has begun, in order
	// Say is one or two sentences to tell the user as they are. Next is what
	// to do next, NextCommand the command to run for it ("" when none).
	Say         string `json:"say"`
	Next        string `json:"next"`
	NextCommand string `json:"next_command,omitempty"`
	// MustTell are lines to pass on to the user word for word (only once
	// succeeded): an old token that could not be revoked, a token with no
	// quota and no expiry.
	MustTell []string `json:"must_tell,omitempty"`
}

// StepTime is how long one step has taken against its normal time.
type StepTime struct {
	App               string `json:"app"`
	Name              string `json:"name"`
	Label             string `json:"label"`
	Status            string `json:"status"`
	Index             int    `json:"index"` // its place among all the task's steps, from 1
	ElapsedSeconds    int    `json:"elapsed_seconds"`
	NormalSeconds     int    `json:"normal_seconds"`
	StuckAfterSeconds int    `json:"stuck_after_seconds"`
	Typical           string `json:"typical"`
	Slow              bool   `json:"slow"`
	Stuck             bool   `json:"stuck"` // only a running step is ever stuck
	StalledSeconds    int64  `json:"stalled_seconds,omitempty"`
	// Look is a read-only command to see why the running step is slow.
	Look string `json:"look,omitempty"`
}

// What to do next, by state. They speak to whoever reads the output (the AI
// workbench), and refer to the other fields by name.
const (
	NextRunning   = "还没装完，结果还不知道：只把 say 告诉用户，不要说“卡住”“装不好”“容器没在运行”“没有令牌”这类结论，也不要自己估计还要多久；接着运行 next_command 再等，直到 done 是 true"
	NextStuck     = "这一步比平常慢很多（CAT-INST-30），但安装还没有失败：先运行 next_command 看原因（只读，不用问用户），把 say 和看到的要点告诉用户，再问用户是接着等还是不装了；完整的排查步骤用 acornfox faults CAT-INST-30 --json 看。不要自己删除、重装或重启服务"
	NextSucceeded = "装好了：把 first_login 告诉用户（密码不要复述），再说怎么打开和 user_steps；must_tell 里的每一句都要照原话告诉用户，一句都不能漏；接上旧数据重装的，再把 apps[].carried、kept_files、file_backups 告诉用户"
	NextFailed    = "装失败了：先运行 next_command 看这个编号怎么处理，照着做；处理好原因后重新安装"
)

// WaitCommand is the command that waits on task id.
func WaitCommand(id string) string {
	return "acornfox --json install status " + id + " --wait"
}

// TokenNote is the line for an app that has a token with no quota and no
// expiry in provider (a local New API).
func TokenNote(app, provider string) string {
	return app + " 用的是 " + provider + " 里一把不限额、不过期的令牌；要控制花费，可在 " + provider + " 后台的“令牌”页给它设上限。"
}

// Assess works out the verdict of t at now; clock is "server" or "client".
func Assess(t Task, now time.Time, clock string) Verdict {
	v := Verdict{Now: now.UTC(), Clock: clock, TotalSteps: len(t.Steps), Steps: []StepTime{}}
	end := now
	done := t.Status == StatusSucceeded || t.Status == StatusFailed
	if done && t.FinishedAt != nil {
		end = *t.FinishedAt
	}
	if !t.CreatedAt.IsZero() {
		v.ElapsedSeconds = seconds(end.Sub(t.CreatedAt))
	}
	for i, s := range t.Steps {
		// A step that has not begun has no time; a running one without a
		// start time (an old record) counts from 0.
		if s.StartedAt == nil && s.Status != stepRunning {
			continue
		}
		st := stepTime(s, i+1, now, !done)
		v.Steps = append(v.Steps, st)
		if !done && s.Status == stepRunning && v.Step == nil {
			cur := st
			v.Step = &cur
		}
	}
	switch t.Status {
	case StatusSucceeded:
		v.Done = true
		v.Say = "已装好，整次用了 " + durText(v.ElapsedSeconds) + "。"
		v.Next = NextSucceeded
		v.MustTell = mustTell(t.Apps)
	case StatusFailed:
		v.Done = true
		v.Code = t.FaultCode
		v.Say = "安装失败"
		if t.FaultCode != "" {
			v.Say += "：" + t.FaultCode + " " + t.FaultMsg
		}
		if t.KeepVolumes {
			v.Say += "。这次建出来的应用已清理，数据卷按要求留着供排查。"
		} else {
			v.Say += "。这次建出来的应用和数据卷已清理。"
		}
		v.Next = NextFailed
		if t.FaultCode != "" {
			v.NextCommand = "acornfox faults " + t.FaultCode + " --json"
		}
	default:
		v.Next, v.NextCommand = NextRunning, WaitCommand(t.ID)
		cur := v.Step
		if cur == nil {
			v.Say = "还在装：正在准备下一步，整次已用 " + durText(v.ElapsedSeconds) + "。"
			break
		}
		v.Slow, v.Stuck = cur.Slow, cur.Stuck
		msg := ""
		for _, s := range t.Steps {
			if s.App == cur.App && s.Name == cur.Name && s.Message != "" {
				msg = "：" + s.Message
			}
		}
		v.Say = fmt.Sprintf("还在装：%s 正在“%s”（第 %d/%d 步），这一步已用 %s%s。整次已用 %s。",
			cur.App, cur.Label, cur.Index, v.TotalSteps, durText(cur.ElapsedSeconds), msg, durText(v.ElapsedSeconds))
		switch {
		case cur.Stuck:
			v.Code = CodeStuck
			v.Say += "这一步比平常（" + cur.Typical + "）慢很多，可能卡住了，但安装还没有失败。"
			v.Next, v.NextCommand = NextStuck, cur.Look
		case cur.Slow:
			v.Say += "这一步比平常（" + cur.Typical + "）慢一些，还在进行。"
		}
	}
	return v
}

func stepTime(s Step, index int, now time.Time, live bool) StepTime {
	lim := LimitFor(s)
	end := now
	if s.FinishedAt != nil {
		end = *s.FinishedAt
	}
	var el time.Duration
	if s.StartedAt != nil {
		el = end.Sub(*s.StartedAt)
	}
	st := StepTime{App: s.App, Name: s.Name, Label: s.Label, Status: s.Status, Index: index,
		ElapsedSeconds: seconds(el), NormalSeconds: seconds(lim.Normal), StuckAfterSeconds: seconds(lim.StuckAfter),
		Typical: lim.Typical, Slow: el > lim.Normal}
	if s.Status != stepRunning || !live {
		return st
	}
	st.Stuck = el > lim.StuckAfter
	if p := s.Progress; s.Name == "image" && p != nil && p.Phase == "download" {
		stall := time.Duration(p.StalledSeconds) * time.Second
		st.StalledSeconds = p.StalledSeconds
		st.Slow = st.Slow || stall >= DownloadStallSlow
		st.Stuck = st.Stuck || stall >= DownloadStallStuck
	}
	st.Look = look(s)
	return st
}

// look is a read-only command to see why step s is slow.
func look(s Step) string {
	switch s.Name {
	case "start", "health", "init":
		return "acornfox logs " + s.App + " --tail 200 --json"
	}
	return "acornfox doctor --json"
}

func mustTell(apps []App) []string {
	var out []string
	for _, a := range apps {
		for _, c := range a.Carried {
			if strings.HasPrefix(c, NotRevokedPrefix) {
				out = append(out, c)
			}
		}
		if a.TokenProvider != "" {
			out = append(out, TokenNote(a.Name, a.TokenProvider))
		}
	}
	return out
}

func seconds(d time.Duration) int {
	if d < 0 {
		return 0
	}
	return int(d / time.Second)
}

// durText writes seconds the way the rest of the output does: "45 秒",
// "3 分 5 秒", "1 小时 2 分".
func durText(s int) string {
	switch {
	case s < 60:
		return fmt.Sprintf("%d 秒", s)
	case s < 3600:
		if s%60 == 0 {
			return fmt.Sprintf("%d 分钟", s/60)
		}
		return fmt.Sprintf("%d 分 %d 秒", s/60, s%60)
	}
	return fmt.Sprintf("%d 小时 %d 分", s/3600, (s%3600)/60)
}
