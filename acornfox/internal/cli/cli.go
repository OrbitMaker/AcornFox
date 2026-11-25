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
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// Version is the AcornFox version reported by `acornfox version` (client and
// server share one binary). Release builds set it with
// -ldflags "-X github.com/acornfox/acornfox/internal/cli.Version=x.y.z"
// (scripts/build-release.sh takes x.y.z from the release tag), so this
// default never reaches a release.
//
// The default is what a build without that flag reports: the version under
// development with a "-dev" suffix. The app center compares only its x.y.z
// part with a manifest's min_platform, so such a build installs what the
// version under development understands. Raise it when the manifest format
// gains a version.
var Version = "0.6.0-dev"

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

	// The notice that the server is newer than this command line
	// (notices.go): asked at most once per process; now is the clock (tests).
	noticeAsked bool
	now         func() time.Time

	// The new-version check of the server `status` is talking to
	// (update.go); nil when it was not asked or the server has none.
	platformUpd *client.PlatformUpdate

	// N3 `open` injection points (nil in production → real implementations).
	openTunnel func(ctx context.Context, t client.Target, localPort int) (*client.Tunnel, error)
	// `open APP`: tunnel to the app's port on the server.
	openAppTunnel func(ctx context.Context, t client.Target, localPort, remotePort int) (*client.Tunnel, error)
	openURL       func(url string) error // browser opener
	freePort      func() (int, error)    // pick a free local TCP port
	// `open --detach` / `--stop` (open_detach.go).
	openDetached func(ctx context.Context, t client.Target, localPort int) (*client.DetachedTunnel, error)
	tunnelAlive  func(pid, port int) bool
	stopPID      func(pid int) error
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

func mainWith(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, getenv env, configDir, workDir string, connect Connector) (code int) {
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
		return a.out.usageError("需要一个子命令；可用：target、deploy、status、diagnose、apps、versions、logs、stats、env、volume、app set、rollback、redeploy、stop、start、restart、delete、open、domain、add、remove、addons、lan-console、public-console、limits、skill、catalog、install、services、backup、machine、other-containers、update、version、doctor、identity、faults、sources、capabilities（acornfox help 看说明）")
	}

	cmd, cmdArgs := rest[0], rest[1:]
	// After a command that changed what is installed or its settings and
	// succeeded: write the "what this server can do now" part again
	// (machine.go; silent, never changes the exit code).
	defer func() {
		if code == exitOK {
			a.autoRefreshMachine(ctx, cmd, cmdArgs)
		}
	}()
	switch cmd {
	case "help", "-h", "--help":
		_, _ = io.WriteString(stdout, usageText)
		return exitOK
	case "version":
		return a.cmdVersion()
	case "skill":
		return a.cmdSkill(ctx, cmdArgs)
	case "catalog":
		return a.cmdCatalog(ctx, cmdArgs)
	case "install":
		return a.cmdInstall(ctx, cmdArgs)
	case "services":
		return a.cmdServices(ctx, cmdArgs)
	case "machine":
		return a.cmdMachine(ctx, cmdArgs)
	case "update":
		return a.cmdUpdate(ctx, cmdArgs)
	case "other-containers":
		return a.cmdOtherContainers(ctx, cmdArgs)
	case "backup":
		return a.cmdBackup(ctx, cmdArgs)
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
	case "doctor", "identity", "uninstall", "faults", "sources", "capabilities":
		// These run in the acornfox binary itself and only look at this
		// computer; they reach here only when written after --target or --app.
		if cmd == "sources" {
			return a.out.usageError("sources 只看这台机器的镜像下载源，不接受 --target、--app；直接运行 acornfox sources（要实测加 --probe，要 JSON 加 --json）")
		}
		if cmd == "capabilities" {
			return a.out.usageError("capabilities 只看这个程序自己能做什么，不连服务器，不接受 --target、--app；直接运行 acornfox capabilities（要 JSON 加 --json）")
		}
		if cmd == "faults" {
			return a.out.usageError("faults 只查程序里的故障目录，不连服务器，不接受 --target、--app；直接运行 acornfox faults 编号（要 JSON 加 --json）")
		}
		if cmd == "uninstall" {
			return a.out.usageError("uninstall 只管这台机器，不接受 --target、--app；直接运行 acornfox uninstall")
		}
		if cmd == "identity" {
			return a.out.usageError("identity 只管这台机器，不接受 --target、--app；直接运行 acornfox identity show（要 JSON 写 acornfox identity show --json）")
		}
		return a.out.usageError("doctor 只管这台机器，不接受 --target、--app；直接运行 acornfox doctor（要 JSON 写 acornfox doctor --json）")
	default:
		return a.out.usageError("未知命令：%s", cmd)
	}
}

// usageText is printed by `acornfox help`. Each command also takes -h for its
// own flags.
const usageText = `用法：acornfox [--target 服务器] [--app 应用] [--json] 命令 [参数]

本机
  Mac 上只有命令行客户端：用 target add 登记一台 Linux 服务器来管理；Linux 服务器用 install.sh 安装、
  upgrade.sh 升级（见使用文档），装好后服务器上也有这个命令行。
  doctor                           检查这台服务器：Docker、后台服务、磁盘空间、端口占用、时钟、下载网关、应用目录同步
                                   （sudo acornfox doctor 看得更全）
  identity show                    这台机器在下载网关上的安装编号和公钥（reset --yes 重新生成）
  faults [编号]                    故障目录：一个故障编号是什么意思、怎么判定、怎么处理（只读，不连服务器）
  sources [--probe]                这台机器拉镜像用哪些下载源、按什么先后；--probe 现在实测每个源通不通、多快（只读）
  capabilities [--json]            这个 acornfox 程序自己能做什么：每条命令和参数、故障编号、平台变量、服务类型、清单格式从哪版起支持什么（只读，不连服务器；
                                   查“这个版本有没有某条命令”以它为准）

服务器
  target add 名称 --ssh 用户@主机   添加服务器（list、use、remove 管理已有的）
  open [应用]                      打开网页控制台；带应用名时在你电脑上打开这个应用（经 SSH 隧道，只本机的应用也能开）
  lan-console enable               在局域网打开控制台，第一次打开时在浏览器里设密码（disable、status、
                                   reset-password 清密码重设（忘了密码就用它，只能在服务器上做）、devices 看记住的设备、
                                   forget 让设备失效；--help 看全）
  public-console enable 域名        开放控制台公网访问（disable、status）；登录后的控制台页面“设置 › 访问”
                                   也能开关，要再输一次控制台密码（acornfox open 进来的不用）

部署
  deploy                           部署当前目录（--image 镜像、--git 仓库）
  redeploy [应用]                  用当前镜像重新部署，让配置生效（默认等到上线或失败；--no-wait 只提交）
  rollback [应用] [--to N]         回到上一个可回退的版本，或第 N 版（只换程序，不动数据；--cancel-pending 取消正在进行的部署）
  versions [应用]                  版本记录：来源、用时、失败原因、能否回退
  status [应用 | 部署编号] / apps   查看应用状态或一次部署的进度 / 列出所有应用
  diagnose                         查看最近一次部署的诊断

运行
  stop / start / restart [应用]    停止、启动、重启应用（等到状态变了才返回；--no-wait 只提交）
  delete [--volumes]               删除应用（默认保留数据）
  logs [-f]                        查看日志（--version N 看某一版的运行日志，再加 --build 看构建日志）
  stats [应用]                     资源使用（不带应用名则是整台服务器）
  stats 应用 --history 1h|24h      这个应用近 1 小时或 24 小时的 CPU、内存（只读）
  limits [--memory 26G] [--cpu 8]  整机最多分配给应用的内存和 CPU，及各应用已分配

配置
  env set|unset|list               环境变量
  volume add 路径                  数据目录
  app set                          内存、CPU（立即生效）、端口、健康检查路径、--expose 暴露方式
  domain add|remove|list           应用自己的域名与 HTTPS
  domain base [set 域名|clear]     整台服务器的平台域名：公网应用自动得到 应用名.平台域名（--plan 只看会怎样）
  add|remove postgres|mysql|redis  数据库与缓存；addons 查看已添加的

应用中心
  catalog list / info 应用 / sync  看目录里的应用和配方、应用详情和参数、让服务器同步目录
  catalog show 应用                应用中心页面上这个应用的展示内容：一句话、角标、介绍（用户问“能做什么”时读它，只读）
  catalog platform-vars            平台按这台机器自动填给应用的时区、下载源等（只读）
  apps vars 应用 [--reveal]         看一个装好的应用是用什么设置装的：每个变量的值、谁给的、能不能改（只读）
  install 应用 [--plan] [--use …]   从目录装一个应用（--recipe 配方 装一组；--plan 只看计划；--no-wait 只提交；acornfox install -h 看详情）
  install status 任务编号 [--wait]  查安装进度、首次登录信息（--wait 等到装好或失败，默认最多 90 秒；
                                   --show-secrets 显示密码）
  装好的应用：局域网里新装的服务器上默认局域网能打开（用安装结果里的地址）；云主机上默认只本机能打开，
                                   在你自己的电脑上运行 acornfox open 应用名
  services list / show 名字         这台机器上能给应用用的服务（应用提供的、自己登记的）：每一项、状态、谁在用（只读）
  services kinds                   有哪些服务类型、每种有哪些项（只读）
  services add 类型 --name 名字 --set 项=值   登记一条自己已有的服务（保密项写 项=-，从标准输入读）
  services set 名字 项=值 / test 名字 / remove 名字 [--yes]   改几项 / 试一下通不通 / 删一条（不加 --yes 只试跑）

数据备份
  backup create [应用] [--plan]     给应用的数据做一份备份（应用暂停几十秒，打完包自动继续；--plan 只看会怎样）
  backup list [应用]                看备份（不带应用名看整个备份目录和磁盘）；show 应用 编号 看一份的清单
  backup restore 应用 编号|latest [--plan] [--yes]   用备份覆盖现在的数据（先自动另存现在的、失败自动退回；要用户同意加 --yes）
  backup rm 应用 编号 [--yes]       删一份备份（删了找不回来，要用户同意）；backup task 任务编号 看进度

其他
  skill install                    把 AcornFox Skill（入口 SKILL.md 加全部分册）装到 AI 工作台（每个文件头带这个命令行的版本号，acornfox doctor 逐个核对；
                                   写、重装都不出错，旧版本留下的分册会删掉）；同时给每台登记的服务器写一份“本机能力”分册 machine-服务器名.md
  skill print [--all | --file 名]  打印 Skill：默认打印入口，--all 全部，--file install-linux.md 一册（只读）
  skill refresh                    重新写“本机能力”分册（装、删、升级应用之后命令行会自动刷新；在页面上动过应用后手动运行）
  machine                          这台服务器现在能做什么：装了哪些应用和解决方案、各自怎么打开、有没有手册、有哪些服务、平台能做什么（只读；
                                   AI 工作台开始做事前先看它，不含任何密码和 Key）
                                   连上服务器时发现服务器比这个命令行新，会提醒一次更新命令行和 Skill（每天最多一次，
                                   写到标准错误；--json 时在结果的 notices 里；ACORNFOX_NO_UPDATE_NOTICE=1 关掉；CLI-UPDATE-01）
  other-containers                 这台服务器上不是从 AcornFox 装的容器（用户自己 docker run 装的）：名字、在跑还是停了、端口、占了多少内存
                                   （只读，只能看不能管；Windows、Mac 上只看得到 AcornFox 自己那套环境里的）
  catalog lint 目录                检查应用目录的清单和配方（只读，给目录维护者用）
  catalog build|keygen|verify      打包签名发布目录、生成签名钥匙、校验发布目录（给目录维护者用）
  update [check]                   AcornFox 自己有没有新版本、该用哪条命令升级（服务器每 6 小时自己查；check 现在就查；只提醒，不升级，只读线上）
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
