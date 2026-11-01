package cli

import (
	"context"
	"flag"

	"github.com/acornfox/acornfox/internal/client"
)

// cmdLANConsole implements `lan-console status|enable [--reset-password]|disable`.
// The console listens on loopback only by default; enabling opens it on the
// LAN (port 18802, all interfaces) behind a generated password.
func (a *app) cmdLANConsole(ctx context.Context, args []string) int {
	usage := "用法：lan-console status | enable [--reset-password] | disable"
	if len(args) == 0 {
		return a.out.usageError("%s", usage)
	}
	sub := args[0]
	fs := flag.NewFlagSet("lan-console "+sub, flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	reset := fs.Bool("reset-password", false, "重新生成登录密码（旧密码和已登录的会话立即失效）")
	rest, err := parseFlags(fs, args[1:])
	if err != nil {
		return exitUsage
	}
	if len(rest) != 0 {
		return a.out.usageError("%s", usage)
	}
	switch sub {
	case "status", "enable", "disable":
	default:
		return a.out.usageError("%s", usage)
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
			a.out.emitJSON(map[string]any{"enabled": st.Enabled, "url": st.URL, "port": st.Port})
			return exitOK
		}
		if st.Enabled {
			a.out.human("局域网控制台：已开启 %s（要输入密码登录）", st.URL)
		} else {
			a.out.human("局域网控制台：未开启（控制台只在本机打开）。开启：acornfox lan-console enable")
		}
		return exitOK
	case "enable":
		st, password, err := lan.LANConsoleEnable(ctx, *reset)
		if err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"enabled": st.Enabled, "url": st.URL, "port": st.Port, "password": password})
			return exitOK
		}
		a.out.human("局域网控制台已开启：%s", st.URL)
		if password != "" {
			a.out.human("登录密码（只显示这一次，请保存好）：%s", password)
		} else {
			a.out.human("密码没变；忘了的话加 --reset-password 重新生成")
		}
		a.out.human("同一局域网里的人拿到密码就能管理这台电脑上的应用；不用时运行 acornfox lan-console disable 关闭")
		return exitOK
	default:
		if err := lan.LANConsoleDisable(ctx); err != nil {
			return a.out.fail(err)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"enabled": false})
			return exitOK
		}
		a.out.human("局域网控制台已关闭，控制台只在本机打开")
		return exitOK
	}
}
