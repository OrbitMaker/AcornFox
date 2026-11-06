// Package cli implements the acornfox client command layer: it parses global
// and per-command flags, resolves the selected target and app from the command
// line, .acornfox and targets.json, connects to the server through an injected
// Connector, and renders human (Chinese) or --json output. It depends only on
// the internal/client and internal/pack contracts.
package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/acornfox/acornfox/internal/client"
)

// Version is the AcornFox version reported by `acornfox version` (client and
// server share one binary). Release builds set it with
// -ldflags "-X github.com/acornfox/acornfox/internal/cli.Version=x.y.z".
var Version = "0.2.0-dev"

// Connector opens a client.API for a target. It is injected so the command
// layer stays decoupled from the concurrently implemented client.Connect. main
// wires the default adapter.
type Connector func(ctx context.Context, t client.Target) (client.API, error)

// env is the process environment accessor (os.Getenv-shaped), injected for
// tests.
type env func(string) string

// app is the resolved runtime context shared by all commands.
type app struct {
	out       *outputWriter
	stdin     io.Reader
	connect   Connector
	getenv    env
	configDir string // targets.json base dir; "" means os.UserConfigDir
	workDir   string // directory used for .acornfox search and app-name defaulting

	// selection overrides from global flags
	flagTarget string
	flagApp    string

	// N3 `open` injection points (nil in production → real implementations).
	openTunnel func(ctx context.Context, t client.Target, localPort int) (*client.Tunnel, error)
	openURL    func(url string) error // browser opener
	freePort   func() (int, error)    // pick a free local TCP port
}

// Main is the entry point invoked by cmd/acornfox for every non-server,
// non-runner, non-proxy command. It returns the process exit code.
//
// getenv may be nil (defaults to os.Getenv). The connector must be non-nil.
func Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv env) int {
	return mainWith(ctx, args, stdin, stdout, stderr, getenv, "", workingDir(), nil)
}

// MainWithConnector is like Main but lets callers (tests, main.go) inject the
// Connector, config directory and working directory.
func MainWithConnector(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv env, connect Connector, configDir, workDir string) int {
	return mainWith(ctx, args, stdin, stdout, stderr, getenv, configDir, workDir, connect)
}

func mainWith(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv env, configDir, workDir string, connect Connector) int {
	if getenv == nil {
		getenv = os.Getenv
	}
	a := &app{
		out:       &outputWriter{stdout: stdout, stderr: stderr},
		stdin:     stdin,
		connect:   connect,
		getenv:    getenv,
		configDir: configDir,
		workDir:   workDir,
	}

	rest, err := a.parseGlobal(args)
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	if len(rest) == 0 {
		return a.out.usageError("需要一个子命令；可用：target deploy status diagnose apps versions logs stats env volume app set rollback redeploy stop start restart delete open domain add remove addons lan-console public-console limits skill version install setup uninstall doctor identity serve")
	}

	cmd, cmdArgs := rest[0], rest[1:]
	switch cmd {
	case "help", "-h", "--help":
		_, _ = io.WriteString(stdout, usageText)
		return exitOK
	case "version":
		return a.cmdVersion()
	case "skill":
		return a.cmdSkill(ctx, cmdArgs)
	case "target":
		return a.cmdTarget(ctx, cmdArgs)
	case "deploy":
		return a.cmdDeploy(ctx, cmdArgs)
	case "status":
		return a.cmdStatus(ctx, cmdArgs)
	case "diagnose":
		return a.cmdDiagnose(ctx, cmdArgs)
	case "apps":
		return a.cmdApps(ctx, cmdArgs)
	case "logs":
		return a.cmdLogs(ctx, cmdArgs)
	case "versions":
		return a.cmdVersions(ctx, cmdArgs)
	case "stats":
		return a.cmdStats(ctx, cmdArgs)
	case "env":
		return a.cmdEnv(ctx, cmdArgs)
	case "volume":
		return a.cmdVolume(ctx, cmdArgs)
	case "app":
		return a.cmdApp(ctx, cmdArgs)
	case "rollback":
		return a.cmdRollback(ctx, cmdArgs)
	case "redeploy":
		return a.cmdRedeploy(ctx, cmdArgs)
	case "stop":
		return a.cmdStop(ctx, cmdArgs)
	case "start":
		return a.cmdStart(ctx, cmdArgs)
	case "restart":
		return a.cmdRestart(ctx, cmdArgs)
	case "delete":
		return a.cmdDelete(ctx, cmdArgs)
	case "open":
		return a.cmdOpen(ctx, cmdArgs)
	case "domain":
		return a.cmdDomain(ctx, cmdArgs)
	case "add": // N4.2
		return a.cmdAdd(ctx, cmdArgs)
	case "remove": // N4.2
		return a.cmdRemove(ctx, cmdArgs)
	case "addons": // N4.2
		return a.cmdAddons(ctx, cmdArgs)
	case "lan-console": // three-platform architecture
		return a.cmdLANConsole(ctx, cmdArgs)
	case "setup":
		return a.cmdSetup(ctx, cmdArgs)
	case "public-console":
		return a.cmdPublicConsole(ctx, cmdArgs)
	case "limits":
		return a.cmdLimits(ctx, cmdArgs)
	default:
		return a.out.usageError("未知命令：%s", cmd)
	}
}

// usageText is printed by `acornfox help`. Each command also takes -h for its
// own flags.
const usageText = `用法：acornfox [--target 服务器] [--app 应用] [--json] 命令 [参数]

本机（Mac 本机运行即将支持：Mac 上 install、doctor 和对本机的 setup 先停用，
      请用 target add 管理一台 Linux 服务器；Linux 服务器用 install.sh 安装）
  install                          在本机安装主程序并后台运行（再次运行即升级；--rollback 回到上一版）
  setup                            准备运行环境：Docker、网关组件、自检（页面上同样的四步；--json 逐行事件）
  uninstall [--purge]              卸载（默认保留数据；--purge 全部删除）
  doctor                           检查 Docker、后台服务和局域网访问（Linux：Docker、磁盘空间、端口占用）
  identity show                    这台机器在下载网关上的安装编号和公钥（reset --yes 重新生成）

服务器
  target add 名称 --ssh 用户@主机   添加服务器（list、use、remove 管理已有的）
  open                             打开网页控制台
  lan-console enable               在局域网打开控制台，要密码登录（disable、status）
  public-console enable 域名        开放控制台公网访问（disable、status）

部署
  deploy                           部署当前目录（--image 镜像、--git 仓库）
  redeploy [应用]                  用当前镜像重新部署，让配置生效（默认等到上线或失败；--no-wait 只提交）
  rollback [应用] [--to N]         回到上一个可回退的版本，或第 N 版（只换程序，不动数据；--cancel-pending 取消正在进行的部署）
  versions [应用]                  版本记录：来源、用时、失败原因、能否回退
  status / apps                    查看应用状态 / 列出所有应用
  diagnose                         查看最近一次部署的诊断

运行
  stop / start / restart           停止、启动、重启应用
  delete [--volumes]               删除应用（默认保留数据）
  logs [-f]                        查看日志（--version N 看某一版的运行日志，再加 --build 看构建日志）
  stats [应用]                     资源使用（不带应用名则是整台服务器）
  limits [--memory 26G] [--cpu 8]  整机最多分配给应用的内存和 CPU，及各应用已分配

配置
  env set|unset|list               环境变量
  volume add 路径                  数据目录
  app set                          内存、CPU（立即生效）、端口、健康检查路径、--expose 暴露方式
  domain add|remove|list           域名与 HTTPS
  add|remove postgres|mysql|redis  数据库与缓存；addons 查看已添加的

其他
  skill install                    把 AcornFox Skill 装到 AI 工作台
  version                          版本号

每个命令加 -h 查看它的参数。
`

// parseGlobal extracts the global flags (--json, --target, --app) from args.
// Global flags may appear before or after the subcommand and its arguments.
// It returns the remaining args (subcommand + its flags/args) with the global
// flags removed. Per-command FlagSets then handle the rest.
func (a *app) parseGlobal(args []string) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json" || arg == "-json":
			a.out.json = true
		case arg == "--target" || arg == "-target":
			if i+1 >= len(args) {
				return nil, errNeedValue("--target")
			}
			i++
			a.flagTarget = args[i]
		case strings.HasPrefix(arg, "--target=") || strings.HasPrefix(arg, "-target="):
			a.flagTarget = arg[strings.IndexByte(arg, '=')+1:]
		case arg == "--app" || arg == "-app":
			if i+1 >= len(args) {
				return nil, errNeedValue("--app")
			}
			i++
			a.flagApp = args[i]
		case strings.HasPrefix(arg, "--app=") || strings.HasPrefix(arg, "-app="):
			a.flagApp = arg[strings.IndexByte(arg, '=')+1:]
		default:
			rest = append(rest, arg)
		}
	}
	return rest, nil
}

func workingDir() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
