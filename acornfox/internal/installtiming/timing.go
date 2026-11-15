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
// the engine's own time limits end it (CAT-INST-15, -16, -17, one image's
// pull 50 minutes, the task's 60 minutes not counting the pulls).
const CodeStuck = "CAT-INST-30"

// RevokeCode begins the line an install puts in apps[].carried about a
// credential that was not taken back (a revoke that did not work, an old
// credential that did not pass the check and was replaced): it may still
// work in the app that issued it and nothing will try again, so such a
// line is a must-tell.
const RevokeCode = "CAT-INST-35"

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
// the apps of the published catalog (a small model gateway and a large
// agent app).
var Limits = map[string]Limit{
	"check": {
		Normal: 10 * time.Second, StuckAfter: time.Minute,
		Typical: "不到 1 秒",
		Source:  "只查本机的数据库：测试机上两个应用各装过十余次，每次不到 1 秒",
	},
	"create": {
		Normal: 20 * time.Second, StuckAfter: 2 * time.Minute,
		Typical: "几秒（要让别的应用发凭据时多几秒）",
		Source:  "测试机上每次不到 2 秒；要让提供服务的应用发凭据时，按它清单里的步骤发几个请求（实测一个模型网关发一把令牌 4 个请求，不到 1 秒），每个请求最多等 15 秒，加余量算 2 分钟",
	},
	// image not through the download gateway: the image is here already,
	// or it is pulled from the public sources (progress phase pull: the
	// runner's status of the pull).
	"image": {
		Normal: 15 * time.Minute, StuckAfter: 52 * time.Minute,
		Typical: "本机已有时几秒；从公共源拉要看网速（进度里写着从哪个源拉、拉到多少、多快）：网速好时小镜像几十秒、1 GB 左右的几分钟；家庭宽带等慢网络上 1～2 GB 的镜像要十几到三十几分钟",
		Source:  "测试机实测（2026-11）：本机已有不到 1 秒；约 80 MB 的镜像从公共源 126 秒、422 秒，约 1.1 GB 的 561 秒；家庭宽带上一个大镜像最大的一层（232 MB）0.12～0.64 MB/s，要 6～32 分钟。运行器拉一个镜像先对各个源实测（最多约 10 秒），只要还在往下走就一直等，所有源加起来最多 50 分钟，到时判失败（CAT-INST-12）；拉镜像的时间不算进整次安装的 60 分钟。所以拉了 52 分钟还在拉就不对了（按 progress.pull_seconds 算，下载网关没成功、改从公共源拉时前面试网关的时间不算）",
	},
	// image.gateway: the image archive downloaded through the download
	// gateway, with progress (download, then load).
	"image.gateway": {
		Normal: 20 * time.Minute, StuckAfter: 40 * time.Minute,
		Typical: "经下载网关下载有进度和剩余时间：1 GB 左右的镜像包网速好时一两分钟（下载加导入），网速慢的要十几分钟",
		Source:  "测试机实测：约 1.1 GB 的镜像包经下载网关下载约 40 秒，导入 46～75 秒。下载没有总时限，靠 60 秒收不到数据就断开接着下，所以按 1 MB/s 的慢网算 20 分钟以内正常、40 分钟算卡住（经下载网关下载算在整次安装的 60 分钟里，到时还没完会被取消；从公共源拉镜像的时间才不算）；两分钟一个字节都没收到也算卡住（DownloadStallStuck）",
	},
	"files": {
		Normal: 10 * time.Second, StuckAfter: time.Minute,
		Typical: "不到 1 秒",
		Source:  "往数据卷里写一两个小配置文件：测试机上每次不到 1 秒",
	},
	"start": {
		Normal: time.Minute, StuckAfter: 5 * time.Minute,
		Typical: "几秒到半分钟",
		Source:  "测试机实测：小应用 6 秒，大应用 21 秒，接旧数据重装 10～15 秒；安装引擎等 15 分钟还没上线才判失败（CAT-INST-15），过了 5 分钟已远超平常",
	},
	"health": {
		Normal: time.Minute, StuckAfter: 6 * time.Minute,
		Typical: "几秒到十几秒",
		Source:  "测试机实测：4 秒到 10 秒；目录里的清单写的最长等待是 300 秒（timeout_s），到时没通过就判失败（CAT-INST-16），过了 6 分钟还在这一步就不对了",
	},
	"init": {
		Normal: 20 * time.Second, StuckAfter: 4 * time.Minute,
		Typical: "几秒",
		Source:  "测试机上不到 1 秒（三步请求：建管理员、登录、关自助注册）；初始化请求连不上时最多试 5 次、每次最多等 15 秒，约 160 秒后判失败（CAT-INST-17），过了 4 分钟就不对了",
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
	if name == "image" && ((s.Progress != nil && s.Progress.Phase != PhasePull) || strings.HasPrefix(s.Message, GatewayDone)) {
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
	// HasSecrets: some first-login field of the task is a secret (a
	// password the user looks up with ShowSecretsCommand). An app that lets
	// the user make their own account on first open has none.
	HasSecrets bool
}

// Step is one step of one app.
type Step struct {
	App, Name, Label, Status, Message string
	StartedAt, FinishedAt             *time.Time
	Progress                          *Progress
}

// Progress is how far a running step is: the image archive downloaded
// through the download gateway (phase download, then load), or an image
// pulled from the public sources (phase pull: the runner's status of the
// pull, only what Docker reported). The engine's StepProgress is this type.
type Progress struct {
	Phase       string `json:"phase"` // download | load (docker load after the download) | pull (from the public sources)
	DoneBytes   int64  `json:"done_bytes"`
	TotalBytes  int64  `json:"total_bytes,omitempty"`
	BytesPerSec int64  `json:"bytes_per_sec,omitempty"`
	ETASeconds  int64  `json:"eta_seconds,omitempty"`
	// StalledSeconds is how long no byte has come in (download phase).
	StalledSeconds int64 `json:"stalled_seconds,omitempty"`
	// Pull phase: Source is the image source pulling now (empty while the
	// sources are measured first); Blind that Docker sends no progress now
	// (done_bytes is its last figure, how far the pull is cannot be seen)
	// while the machine's network and disk say it still moves; PullSeconds
	// how long this image has been pulled.
	Source      string `json:"source,omitempty"`
	Blind       bool   `json:"blind,omitempty"`
	PullSeconds int64  `json:"pull_seconds,omitempty"`
}

// PhasePull is Progress.Phase of an image pulled from the public sources.
const PhasePull = "pull"

// App is one app of the task, for the lines to pass on. Tell are the
// sentences the providers of the services it was plugged into wrote for the
// user (the items of theirs to fill in after a step in that app: the
// manifest's provides values written ask), as they are.
type App struct {
	Name    string
	Carried []string
	Tell    []string
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
	// ShowSecretsCommand (succeeded only, and only when a first-login field
	// is a secret) is the command that shows the first-login passwords,
	// with this task's id: the user runs it on the server themselves, so
	// the password never passes through the chat. Absent: there is no
	// password to look up, so nothing tells the user to.
	ShowSecretsCommand string `json:"show_secrets_command,omitempty"`
	// MustTell are lines to pass on to the user word for word (only once
	// succeeded): a credential that was not taken back (RevokeCode), then
	// what the providers of the services the apps were plugged into wrote
	// for the user (App.Tell), each line once.
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
	NextRunning   = "还没装完，结果还不知道：只把 say 告诉用户，不要说“卡住”“装不好”“容器没在运行”“没接上服务”这类结论，也不要自己估计还要多久；接着运行 next_command 再等，直到 done 是 true"
	NextStuck     = "这一步比平常慢很多（CAT-INST-30），但安装还没有失败：先运行 next_command 看原因（只读，不用问用户），把 say 和看到的要点告诉用户，再问用户是接着等还是不装了；完整的排查步骤用 acornfox faults CAT-INST-30 --json 看。不要自己删除、重装或重启服务"
	NextSucceeded = "装好了，给用户发一条收尾消息，按这个顺序写：① 先一句话说装好了、接的是哪条服务（apps[].bindings）；" +
		"② first_login：账号和地址原样说，带 url_note 时把它一起说，这个地址不要当成用户现在就能打开的；密码不复述；有 show_secrets_command 时它就是查密码的命令，单独放一个代码块交给用户自己运行，没有这一项就是这个应用没有密码要查，不要提查密码；" +
		"③ 怎么打开，看 apps[].open_hint：把 command 和 tunnel_command 各放一个代码块给用户，两条都是在用户自己电脑上运行的（见 command_where、tunnel_where），不要说成在服务器上运行；" +
		"lan_url_after_expose 是改好以后的地址，改之前打不开，不要当成现在能用的地址；没有 open_hint 就用 apps[].url；" +
		"apps[].internal 是 true 的应用没有地址、不用打开，只说一句它只在内部网络、是给别的应用用的；" +
		"④ 逐条说 user_steps；⑤ must_tell 每一句照原话说，一句不漏；接上旧数据重装的，再说 apps[].carried、kept_files、file_backups；" +
		"⑥ 最后：open_hint 有 ask 就把 ask 原样放在这条消息的结尾问用户，然后停下等他回答，同意了才运行 ask_yes_command，成功后告诉他可以用 lan_url_after_expose 打开了"
	NextFailed = "装失败了：先运行 next_command 看这个编号怎么处理，照着做；处理好原因后重新安装"
)

// ShowSecretsCommand is the command that shows the first-login passwords of
// task id, for the user to run on the server.
func ShowSecretsCommand(id string) string {
	return "acornfox install status " + id + " --show-secrets"
}

// WaitCommand is the command that waits on task id.
func WaitCommand(id string) string {
	return "acornfox --json install status " + id + " --wait"
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
		if t.ID != "" && t.HasSecrets {
			v.ShowSecretsCommand = ShowSecretsCommand(t.ID)
		}
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
	if p := s.Progress; s.Name == "image" && p != nil && p.Phase == PhasePull && p.PullSeconds > 0 {
		// A pull from the public sources is judged by its own time (the
		// step may have tried the download gateway first).
		st.Stuck = time.Duration(p.PullSeconds)*time.Second > lim.StuckAfter
	}
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
	seen := map[string]bool{}
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, a := range apps {
		for _, c := range a.Carried {
			if strings.HasPrefix(c, RevokeCode+" ") {
				add(c)
			}
		}
	}
	for _, a := range apps {
		for _, s := range a.Tell {
			add(s)
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
