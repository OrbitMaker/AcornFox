package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/setup"
)

// Exit codes of `setup` beyond the shared ones.
const (
	exitSetupNeedsUser = 5 // waiting for the user (a choice, an admin confirmation, a reboot)
	exitSetupPaused    = 6 // paused
)

// cmdSetup implements `setup`: prepare this computer's runtime (Docker, the
// gateway, the self checks). The web console shows the same steps; both use
// /v1/setup. Without flags it starts (or attaches to) the setup and follows
// it until it is done, fails or needs the user.
//
//	setup [--runtime builtin|docker-desktop|existing] [--mirror URL]
//	      [--confirm-admin] [--resume] [--pause] [--status] [--json]
//
// With --json every event is printed as one JSON line (the console reads the
// same events); the last line is the done or error event.
func (a *app) cmdSetup(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	runtime := fs.String("runtime", "", "运行环境：builtin（AcornFox 自带，没有界面）、docker-desktop（打开已装的 Docker Desktop）、existing（用已经在运行的 Docker）")
	mirror := fs.String("mirror", "", "先从这个下载源下载运行环境文件（地址前缀，文件在 <地址>/runtime/<文件名>）")
	confirmAdmin := fs.Bool("confirm-admin", false, "Windows：同意弹出管理员确认（打开 WSL 2、加局域网防火墙规则要用）")
	localOnly := fs.Bool("local-only", false, "Windows：不加局域网防火墙规则，应用只在本机打开（以后可用 --confirm-admin 再打开）")
	resume := fs.Bool("resume", false, "从上次停下的地方接着做（失败、暂停或重启电脑之后）")
	pause := fs.Bool("pause", false, "暂停（已下载的部分会保留）")
	status := fs.Bool("status", false, "只看当前状态，不开始")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(rest) != 0 {
		return a.out.usageError("用法：setup [--runtime builtin|docker-desktop|existing] [--mirror 地址] [--confirm-admin | --local-only] [--resume] [--pause] [--status]")
	}
	switch *runtime {
	case "", setup.RuntimeBuiltin, setup.RuntimeDockerDesktop, setup.RuntimeExisting:
	default:
		return a.out.usageError("--runtime 只能是 builtin、docker-desktop 或 existing")
	}
	t, _, terr := a.resolveTarget()
	if terr != nil {
		return a.out.usageError("%s", terr.Error())
	}
	if setup.MacLocalBlocked() && strings.HasPrefix(t.URL, "unix://") {
		// A Mac's own server (left by an earlier test install): the Mac
		// local mode is off in this release (internal/setup/maclocal.go).
		// Setting up a remote server is not affected.
		return a.macLocalStopped()
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()
	sa, ok := api.(client.SetupAPI)
	if !ok {
		return a.out.usageError("当前连接方式不支持运行环境准备")
	}

	st, err := sa.SetupStatus(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	if *status {
		return a.setupReport(st)
	}
	if *pause {
		paused, err := sa.SetupPause(ctx)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"paused": paused})
		} else if paused {
			a.out.human("已暂停；已下载的部分会保留。接着做：acornfox setup --resume")
		} else {
			a.out.human("现在没有在准备运行环境")
		}
		return exitOK
	}

	if *confirmAdmin && *localOnly {
		return a.out.usageError("--confirm-admin 和 --local-only 只能选一个")
	}
	opts := setup.StartOptions{Runtime: *runtime, Mirror: *mirror, ConfirmAdmin: *confirmAdmin, LocalOnly: *localOnly}
	since := st.Seq
	explicit := *runtime != "" || *mirror != "" || *confirmAdmin || *localOnly
	switch {
	case st.State == setup.StateRunning:
		// Attach to the run in progress.
	case explicit:
		st, err = sa.SetupStart(ctx, opts)
	case *resume || st.State == setup.StateFailed || st.State == setup.StatePaused || st.State == setup.StateIdle:
		st, err = sa.SetupResume(ctx)
	case st.State == setup.StateNeedsUser:
		return a.setupReport(st)
	case st.State == setup.StateDone:
		return a.setupReport(st)
	default:
		st, err = sa.SetupResume(ctx)
	}
	if err != nil {
		return a.out.fail(err)
	}
	if !a.out.json {
		a.out.human("AcornFox %s · 准备运行环境 · 网络：%s", st.Version, map[string]string{"china": "国内", "global": "海外"}[st.Network])
	}
	return a.setupFollow(ctx, sa, since)
}

// macLocalStopped reports SETUP-MAC-01 the way acornfox install does on a
// Mac: {"ok":false,"diagnosis":{"stage":"local","code":"SETUP-MAC-01",...}}
// in JSON mode, exit code 2.
func (a *app) macLocalStopped() int {
	d := client.Diagnosis{Stage: "local", Code: setup.MacLocalCode, Message: setup.MacLocalMessage, Hint: setup.MacLocalHint}
	if a.out.json {
		a.out.emitDiagnosisJSON(d)
	} else {
		a.out.humanErr("%s", d.Message)
		a.out.humanErr("故障编号 %s", d.Code)
		a.out.humanErr("怎么办 %s", d.Hint)
	}
	return exitUsage
}

// setupFollow prints events until the setup stops.
func (a *app) setupFollow(ctx context.Context, sa client.SetupAPI, since int64) int {
	lastItem := map[string]time.Time{}
	for {
		evs, resync, err := sa.SetupEvents(ctx, since, 20)
		if err != nil {
			return a.out.fail(err)
		}
		if resync {
			st, err := sa.SetupStatus(ctx)
			if err != nil {
				return a.out.fail(err)
			}
			since = st.Seq
			continue
		}
		for _, e := range evs {
			since = e.Seq
			if a.out.json {
				a.out.encodeRedacted(a.out.stdout, e)
			} else {
				a.setupHuman(e, lastItem)
			}
			switch e.Event {
			case "done":
				st, _ := sa.SetupStatus(ctx)
				if !a.out.json {
					a.setupDoneHuman(st)
				}
				return exitOK
			case "error":
				if !a.out.json && e.Error != nil {
					a.setupErrorHuman(*e.Error)
				}
				return exitOpFailed
			case "needs_user":
				if !a.out.json && e.Need != nil {
					a.setupNeedHuman(*e.Need)
				}
				return exitSetupNeedsUser
			case "paused":
				if !a.out.json {
					a.out.human("已暂停。接着做：acornfox setup --resume")
				}
				return exitSetupPaused
			}
		}
	}
}

var stepNo = map[string]int{setup.StepBinary: 1, setup.StepRuntime: 2, setup.StepEdge: 3, setup.StepChecks: 4}
var stepTitle = map[string]string{setup.StepBinary: "主程序", setup.StepRuntime: "运行环境", setup.StepEdge: "网关组件", setup.StepChecks: "自检"}

func (a *app) setupHuman(e setup.Event, lastItem map[string]time.Time) {
	switch e.Event {
	case "step":
		mark := map[string]string{setup.StateRunning: "●", setup.StateDone: "✓", setup.StateFailed: "✗", setup.StateNeedsUser: "●", setup.StatePaused: "‖"}[e.State]
		if mark == "" || (e.State == setup.StateRunning && e.Detail == "") {
			return
		}
		a.out.human("%s %d/4 %-8s %s", mark, stepNo[e.Step], stepTitle[e.Step], e.Detail)
	case "progress":
		it := e.Item
		if it == nil {
			return
		}
		if it.State == setup.StateDone {
			size := ""
			if it.TotalBytes > 0 {
				size = humanMB(it.TotalBytes)
			}
			a.out.human("      ✓ %s %s %s", it.Title, size, it.Detail)
			return
		}
		// Running items: at most one line every 5 seconds per item.
		if t, ok := lastItem[it.ID]; ok && time.Since(t) < 5*time.Second {
			return
		}
		lastItem[it.ID] = time.Now()
		line := "      … " + it.Title
		if it.TotalBytes > 0 && it.DoneBytes > 0 {
			line += fmt.Sprintf(" %s/%s", humanMB(it.DoneBytes), humanMB(it.TotalBytes))
		}
		if it.SpeedBps > 0 {
			line += fmt.Sprintf(" · %.1f MB/s", float64(it.SpeedBps)/(1<<20))
		}
		if it.ETASeconds > 0 {
			line += fmt.Sprintf(" · 还要约 %d 秒", it.ETASeconds)
		}
		if it.Source != "" {
			line += " · 源 " + it.Source
		}
		if it.Detail != "" {
			line += " · " + it.Detail
		}
		a.out.human("%s", line)
	case "check":
		if c := e.Check; c != nil {
			mark := "✓"
			if !c.OK {
				mark = "!"
				if c.Required {
					mark = "✗"
				}
			}
			detail := c.Detail
			if detail != "" {
				detail = "  " + detail
			}
			a.out.human("      %s %s%s", mark, c.Title, detail)
		}
	}
}

func (a *app) setupDoneHuman(st setup.Status) {
	took := ""
	if st.StartedAt != nil && st.FinishedAt != nil {
		took = fmt.Sprintf("用时 %d 秒。", int(st.FinishedAt.Sub(*st.StartedAt).Seconds()))
	}
	a.out.human("")
	a.out.human("可以部署应用了。%s", took)
	a.out.human("  控制台   acornfox open")
	a.out.human("  部署项目 acornfox deploy")
}

func (a *app) setupErrorHuman(f setup.Failure) {
	a.out.human("✗ %s：%s", stepTitle[f.Step], f.Message)
	a.out.human("      故障编号 %s", f.Code)
	if f.KeptBytes > 0 {
		a.out.human("      已下载的 %s 会保留", humanMB(f.KeptBytes))
	}
	if f.Next != "" {
		a.out.human("      怎么办 %s", f.Next)
	}
	a.out.human("      重试   acornfox setup --resume")
	a.out.human("      诊断   acornfox doctor")
}

func (a *app) setupNeedHuman(n setup.Need) {
	a.out.human("● 需要你：%s", n.Message)
	for i, s := range n.Steps {
		a.out.human("      %d. %s", i+1, s)
	}
	switch n.Kind {
	case setup.NeedChooseRuntime:
		for _, o := range n.Options {
			rec := ""
			if o.Recommended {
				rec = "（推荐）"
			}
			a.out.human("      acornfox setup --runtime %-15s %s%s", o.ID, o.Title, rec)
		}
	case setup.NeedAdmin:
		a.out.human("      同意后运行：acornfox setup --confirm-admin")
		for _, o := range n.Options {
			if o.ID == "local-only" {
				a.out.human("      只在本机用：acornfox setup --local-only")
			}
		}
	case setup.NeedReboot:
		a.out.human("      重启电脑后会自动接着装；也可以重启后运行 acornfox setup --resume")
	}
}

// setupReport prints the current status without changing it.
func (a *app) setupReport(st setup.Status) int {
	if a.out.json {
		a.out.emitJSON(map[string]any{"setup": st})
	} else {
		for _, s := range st.Steps {
			mark := map[string]string{setup.StateDone: "✓", setup.StateFailed: "✗", setup.StateNeedsUser: "●", setup.StateRunning: "●", setup.StatePaused: "‖"}[s.State]
			if mark == "" {
				mark = " "
			}
			a.out.human("%s %d/4 %-8s %s", mark, stepNo[s.ID], s.Title, s.Detail)
		}
		for _, c := range st.Checks {
			mark := "✓"
			if !c.OK {
				mark = "!"
			}
			a.out.human("      %s %s %s", mark, c.Title, c.Detail)
		}
		switch {
		case st.Error != nil:
			a.setupErrorHuman(*st.Error)
		case st.Need != nil:
			a.setupNeedHuman(*st.Need)
		case st.State == setup.StateDone:
			a.out.human("运行环境已就绪（%s）。", strings.TrimSpace(st.RuntimeDetail))
		}
	}
	switch st.State {
	case setup.StateDone:
		return exitOK
	case setup.StateNeedsUser:
		return exitSetupNeedsUser
	case setup.StatePaused:
		return exitSetupPaused
	case setup.StateFailed:
		return exitOpFailed
	}
	return exitOK
}

func humanMB(b int64) string {
	return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
}
