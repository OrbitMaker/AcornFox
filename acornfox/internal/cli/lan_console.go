package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

const lanConsoleUsage = `用法：acornfox lan-console status | enable | disable | reset-password | devices | forget 设备编号|--all   [--json]

局域网控制台：让同一局域网里的电脑、手机用浏览器打开这台服务器的控制台（端口 18802）。
  第一次打开时在浏览器里自己设密码；登录时可以勾“记住这台设备 90 天”。
  只给局域网里的地址用，从公网来的请求一律拒绝；云主机、公网服务器开不了（用 acornfox open）。
  局域网里不加密，只在可信的网络里用。

  status          看开没开、地址、密码设了没有（什么时候、从哪个地址设的）、记住了几台设备。只读
  enable          打开。已经开着就什么都不变。写操作：开始在 18802 端口监听
  disable         关闭。写操作：停止监听，密码、登录状态、记住的设备全部清掉
  reset-password  清掉密码。写操作：所有登录状态和记住的设备一起作废，下一个从局域网打开的人重新设密码。
                  忘了密码，或者 status 里设密码的时间、地址不是你，就用它
  devices         列出记住的设备。只读
  forget 编号     让一台记住的设备失效（它当前的登录也立刻失效）；forget --all 是全部。写操作，可以重复执行

例子：
  acornfox lan-console status --json
  acornfox lan-console reset-password
  acornfox lan-console forget 3f9c2a1b7d0e
`

// cmdLANConsole implements `lan-console status | enable | disable |
// reset-password | devices | forget ID|--all`. See lanConsoleUsage.
func (a *app) cmdLANConsole(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.out.stderr, lanConsoleUsage)
		return exitUsage
	}
	sub := args[0]
	fs := flag.NewFlagSet("lan-console "+sub, flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	fs.Usage = func() { fmt.Fprint(a.out.stderr, lanConsoleUsage) }
	// Kept for scripts written for v0.5.0: enable --reset-password is
	// reset-password after making sure the LAN console is open.
	reset := fs.Bool("reset-password", false, "同 reset-password 子命令（为旧脚本保留）")
	all := fs.Bool("all", false, "forget：全部记住的设备")
	rest, err := parseFlags(fs, args[1:])
	if err != nil {
		return exitUsage
	}
	switch sub {
	case "status", "enable", "disable", "reset-password", "devices":
		if len(rest) != 0 {
			return a.out.usageError("lan-console %s 不带别的参数。完整用法：acornfox lan-console --help", sub)
		}
	case "forget":
		if (len(rest) == 1) == *all {
			return a.out.usageError("lan-console forget 要一个设备编号（acornfox lan-console devices 里看），或者 --all")
		}
	case "-h", "--help", "help":
		fmt.Fprint(a.out.stdout, lanConsoleUsage)
		return exitOK
	default:
		return a.out.usageError("不认识的子命令 %q。完整用法：acornfox lan-console --help", sub)
	}
	t, _, terr := a.resolveTarget()
	if terr != nil {
		return a.out.usageError("%s", terr.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()
	lan, ok := api.(client.LANConsoleAPI)
	if !ok {
		return a.out.usageError("当前连接方式不支持局域网控制台")
	}
	switch sub {
	case "status":
		st, err := lan.LANConsoleStatus(ctx)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.out.emitJSON(lanStatusJSON(st))
			return exitOK
		}
		a.lanStatusHuman(st)
		return exitOK
	case "enable":
		st, password, err := lan.LANConsoleEnable(ctx, *reset)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			m := lanStatusJSON(st)
			m["password"] = password
			a.out.emitJSON(m)
			return exitOK
		}
		a.out.human("局域网控制台已开启：%s", st.URL)
		switch {
		case password != "":
			// An older server still generates the password.
			a.out.human("登录密码（只显示这一次，请保存好）：%s", password)
		case !st.PasswordSet:
			a.out.human("还没有密码：请现在就用局域网里的电脑打开上面的地址设一个。谁先打开谁设，别拖。")
		default:
			a.out.human("密码没变。忘了就运行 acornfox lan-console reset-password，再打开上面的地址重新设")
		}
		a.out.human("只在可信的网络里用（局域网里不加密）；不用时运行 acornfox lan-console disable 关闭")
		return exitOK
	case "disable":
		if err := lan.LANConsoleDisable(ctx); err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"enabled": false})
			return exitOK
		}
		a.out.human("局域网控制台已关闭，控制台只在本机打开；密码、登录状态和记住的设备都已清掉")
		return exitOK
	case "reset-password":
		st, err := lan.LANConsoleResetPassword(ctx)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.out.emitJSON(lanStatusJSON(st))
			return exitOK
		}
		a.out.human("密码已清掉，所有登录状态和记住的设备一起作废。")
		a.out.human("请现在就用局域网里的电脑打开 %s 重新设密码（谁先打开谁设）。", st.URL)
		return exitOK
	case "devices":
		ds, err := lan.LANConsoleDevices(ctx)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"devices": ds})
			return exitOK
		}
		if len(ds) == 0 {
			a.out.human("没有记住的设备（登录时勾“记住这台设备 90 天”才会有）")
			return exitOK
		}
		for _, d := range ds {
			a.out.human("%s\t%s\t上次使用 %s（%s）\t到期 %s", d.ID, d.Name, d.LastUsedAt.Local().Format("01-02 15:04"), d.LastIP, d.ExpiresAt.Local().Format("2006-01-02"))
		}
		a.out.human("让某一台失效：acornfox lan-console forget 设备编号")
		return exitOK
	default: // forget
		id := ""
		if !*all {
			id = rest[0]
		}
		n, err := lan.LANConsoleRemoveDevice(ctx, id)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"removed": n})
			return exitOK
		}
		switch {
		case n > 0:
			a.out.human("已让 %d 台设备失效，它们要重新输密码才能进", n)
		case *all:
			a.out.human("没有记住的设备，什么都没改")
		default:
			a.out.human("没有编号是 %s 的设备（可能已经移除或到期），什么都没改", id)
		}
		return exitOK
	}
}

func lanStatusJSON(st client.LANConsoleStatus) map[string]any {
	return map[string]any{
		"enabled": st.Enabled, "url": st.URL, "port": st.Port,
		"password_set": st.PasswordSet, "password_set_at": st.PasswordSetAt, "password_set_from": st.PasswordSetFrom,
		"generated_password": st.GeneratedPassword, "devices": st.Devices,
	}
}

func (a *app) lanStatusHuman(st client.LANConsoleStatus) {
	if !st.Enabled {
		a.out.human("局域网控制台：未开启（控制台只在本机打开）。开启：acornfox lan-console enable")
		return
	}
	a.out.human("局域网控制台：已开启 %s", st.URL)
	switch {
	case !st.PasswordSet:
		a.out.human("密码：还没设（故障编号 LAN-CONSOLE-03）。请现在就打开上面的地址设一个，谁先打开谁设")
	case st.GeneratedPassword:
		a.out.human("密码：开启时生成的那个（旧版本的做法，照常能用）。想换成自己设的：acornfox lan-console reset-password")
	case st.PasswordSetAt != nil:
		a.out.human("密码：%s 从 %s 设的。不是你设的就运行 acornfox lan-console reset-password", st.PasswordSetAt.Local().Format(time.DateTime), st.PasswordSetFrom)
	default:
		a.out.human("密码：已设")
	}
	a.out.human("记住的设备：%d 台（acornfox lan-console devices 查看）", st.Devices)
}
