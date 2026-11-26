package main

// acornfox mac …: AcornFox on a Mac. The program itself is the same Linux one as on
// a server; it runs in a Debian virtual machine opened by Apple's `container`
// (container machine). This file is the command line of the Mac side
// (internal/macvm has the logic):
//
//	acornfox mac install | upgrade   put it in place (every step is skipped when it was done before)
//	acornfox mac uninstall [--purge] take it away (default: keep the machine and its data)
//	acornfox mac status | doctor     look (never wakes the machine)
//	acornfox mac start | stop        start the machine now / stop it now (it starts by itself at the next access)
//	acornfox mac set …               memory, CPUs, idle minutes, LAN
//	acornfox mac daemon              the small resident program (started by launchd; not run by hand)
//
// get.sh (on a Mac) fetches this program from the release and runs "acornfox mac install".

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/installfrom"
	"github.com/acornfox/acornfox/internal/macvm"
)

const macHelp = `用法：acornfox mac <动作> [选项]

AcornFox 在这台 Mac 上的样子：和服务器上同一个程序，装在苹果 container 开的一台 Debian 虚拟机里；
Mac 上只有苹果的 container、一个常驻小程序（登录后自动运行，替虚拟机转发端口、闲置时停虚拟机、有人访问再叫起）和 acornfox 命令。

动作：
  install            安装（已经做过的步骤跳过，重复运行不出错）。要管理员密码的一步由你自己输
  upgrade            升级虚拟机里的 AcornFox 和这台 Mac 上的命令（用一行命令 get.sh … | bash -s -- upgrade 运行最方便；直接运行要写 --version）
  uninstall          卸载：默认保留虚拟机和里面的数据（重装能接上）；--purge --yes 连数据一起删
  status [--json]    看状态（不会叫起虚拟机）
  doctor [--json] [--wake]   逐项检查，每项带故障编号；--wake 先叫起虚拟机再检查里面
  start | stop       现在叫起 / 现在停掉虚拟机（停掉能马上把内存还给 macOS；下次有人访问会自动叫起）
  set                改设置：--memory 4|6|8（GB）、--cpus N、--idle 分钟数|off、--lan on|off、--public-host 地址|auto

install / upgrade / uninstall 的选项：
  --plan             试跑：只说要做什么，不下载、不改任何东西
  --json             标准输出只有最后一行 JSON（ok、action、exit_code、steps、faults、console_url）
  --yes              不再询问（--purge 必须加）
  --china | --global 这一次先用国内（Gitee）还是海外（GitHub）下载
  --memory N         虚拟机内存（GB）；不写：16 GB 以上的 Mac 给 6，其余给 4
  --idle M           闲置 M 分钟停虚拟机（默认 30；0 或 off 表示不停）
  --no-lan           局域网里的设备不能打开（默认开，应用设成“只本机”的除外）
  --no-open          装完不打开浏览器
  --version vX.Y.Z   要装的版本（用 get.sh 装时由它填）
  --gateway 地址|off 下载网关（默认 https://dl.acornfox.com；off 不用）
  --release-base 地址  测试或自建镜像：从 <地址>/releases/download/<版本>/ 取文件
  --local-package 目录 测试：用目录里的 install.sh、acornfox_linux_arm64、入口包，不下载
  --from 名字        来源标记（官网链接带来的，例如 bili-1201；只认小写字母、数字、-，最长 32；不合规的当没有，提醒一句、照常安装）：
                     只在安装时交给虚拟机里的安装脚本，装好后向下载网关报一次、只报这个标记；升级、卸载不带；不写就什么都不报

退出码：0 成功；1 出错（屏幕上有故障编号，acornfox faults 编号 看办法）；2 用法不对；5 要管理员密码而这里没有终端，请自己在终端里输。
环境变量：ACORNFOX_MAC_HOME（状态目录，默认 ~/Library/Application Support/AcornFox/mac）、ACORNFOX_MAC_VM（虚拟机名字，默认 acornfox）、
  ACORNFOX_INSTALL_DIR（acornfox 命令装到哪）、ACORNFOX_GATEWAY、ACORNFOX_RELEASE_BASE、ACORNFOX_REPO、ACORNFOX_GITEE_REPO。
例子：
  acornfox mac install --plan
  acornfox mac status --json
  acornfox mac set --idle 60
  acornfox mac uninstall --purge --yes
`

func runMac(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, macHelp)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "install", "upgrade":
		return macInstall(sub, rest, stdout, stderr)
	case "uninstall":
		return macUninstall(rest, stdout, stderr)
	case "status":
		return macStatus(rest, stdout, stderr)
	case "doctor":
		return macDoctor(rest, stdout, stderr)
	case "start", "stop":
		return macStartStop(sub, rest, stdout, stderr)
	case "set":
		return macSet(rest, stdout, stderr)
	case "daemon":
		return macDaemon(stdout, stderr)
	case "pack-base":
		return macPackBase(rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "acornfox mac：不认识的动作 %q（acornfox mac --help 看说明）\n", sub)
	return 2
}

func macPaths() macvm.Paths { return macvm.Paths{Root: macvm.DefaultRoot()} }

func newMacInstaller(opt macvm.Options, human io.Writer) *macvm.Installer {
	r := macvm.ExecRunner{}
	return &macvm.Installer{
		Paths: macPaths(), Opt: opt, R: r, Host: macvm.RealHost{R: r}, Out: human, Exe: macvm.LookExe(), Version: cli.Version,
	}
}

// macCommon are the options install, upgrade and uninstall share.
type macCommon struct {
	opt        macvm.Options
	memory     string
	idle       string
	noLAN      bool
	releaseVer string
	badFrom    string // an unusable --from, dropped by finish
}

func macFlags(name string, c *macCommon, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("mac "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	o := &c.opt
	fs.BoolVar(&o.Plan, "plan", false, "试跑")
	fs.BoolVar(&o.JSON, "json", false, "JSON 输出")
	fs.BoolVar(&o.Yes, "yes", false, "不再询问")
	fs.BoolVar(&o.Yes, "y", false, "不再询问")
	fs.BoolVar(&o.Purge, "purge", false, "连数据一起删")
	fs.BoolVar(&o.China, "china", false, "先用国内源")
	fs.BoolVar(&o.Global, "global", false, "先用海外源")
	fs.BoolVar(&o.NoOpen, "no-open", false, "装完不打开浏览器")
	fs.BoolVar(&o.NoLAN, "no-lan", false, "局域网不能打开")
	fs.StringVar(&c.memory, "memory", "", "虚拟机内存（GB）")
	fs.IntVar(&o.CPUs, "cpus", 0, "虚拟机 CPU 数")
	fs.StringVar(&c.idle, "idle", "", "闲置多少分钟停虚拟机")
	fs.StringVar(&o.PublicHost, "public-host", "", "应用地址里写的这台 Mac 的地址")
	fs.StringVar(&o.Version, "version", "", "要装的版本")
	fs.StringVar(&o.ReleaseBase, "release-base", os.Getenv("ACORNFOX_RELEASE_BASE"), "发布源")
	fs.StringVar(&o.LocalPackage, "local-package", "", "本地安装包目录")
	fs.StringVar(&o.Gateway, "gateway", "", "下载网关")
	fs.StringVar(&o.InstallDir, "install-dir", "", "acornfox 命令装到哪")
	fs.StringVar(&o.From, "from", "", "来源标记")
	return fs
}

func (c *macCommon) finish() error {
	o := &c.opt
	o.IdleMinutes = -1
	if c.idle != "" {
		if c.idle == "off" {
			o.IdleMinutes = 0
		} else if n, err := strconv.Atoi(c.idle); err == nil && n >= 0 {
			o.IdleMinutes = n
		} else {
			return fmt.Errorf("--idle 要写分钟数或 off：%q", c.idle)
		}
	}
	if c.memory != "" {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.ToUpper(c.memory), "G"))
		if err != nil || n < 2 || n > 64 {
			return fmt.Errorf("--memory 要写 GB 数，例如 6：%q", c.memory)
		}
		o.MemoryGB = n
	}
	o.GithubRepo = os.Getenv("ACORNFOX_REPO")
	if v, ok := os.LookupEnv("ACORNFOX_GITEE_REPO"); ok {
		o.GiteeRepo = v
		if v == "" {
			o.GiteeRepo = "-"
		}
	}
	if o.China && o.Global {
		return fmt.Errorf("--china 和 --global 只能写一个")
	}
	if o.From != "" && !installfrom.Valid(o.From) {
		c.badFrom, o.From = o.From, "" // not usable: treated as not given (macInstall says so)
	}
	return nil
}

func macInstall(verb string, args []string, stdout, stderr io.Writer) int {
	c := &macCommon{}
	fs := macFlags(verb, c, stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := c.finish(); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	c.opt.Action = verb
	if c.badFrom != "" && verb == "install" {
		fmt.Fprintf(stderr, "来源标记 --from %s 不合规（只认小写字母、数字、-，最长 32 个字符），已忽略，照常安装\n", c.badFrom)
	}
	if verb != "install" {
		c.opt.From = "" // the source tag belongs to a fresh install only
	}
	if verb == "upgrade" && c.opt.Version == "" && c.opt.LocalPackage == "" {
		fmt.Fprintln(stderr, "升级要从发行版取新版本的程序：用一行命令（它会取最新版、校验后再交给这里）：\n  curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- upgrade\n（海外把地址换成 https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh；先加 --plan 看会做什么）。直接运行 acornfox mac upgrade 要写 --version vX.Y.Z")
		return 2
	}
	human := stdout
	if c.opt.JSON {
		human = stderr
	}
	in := newMacInstaller(c.opt, human)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return in.Install(ctx, stdout)
}

func macUninstall(args []string, stdout, stderr io.Writer) int {
	c := &macCommon{}
	fs := macFlags("uninstall", c, stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	_ = c.finish()
	human := stdout
	if c.opt.JSON {
		human = stderr
	}
	in := newMacInstaller(c.opt, human)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return in.Uninstall(ctx, stdout)
}

func macStatus(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mac status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON 输出")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	in := newMacInstaller(macvm.Options{}, nil)
	st := in.Status(context.Background())
	if *asJSON {
		fmt.Fprintln(stdout, macvm.JSON(st))
	} else {
		macvm.PrintStatus(stdout, st)
	}
	if !st.Installed {
		return 1
	}
	return 0
}

// macVMInstalled says whether this Mac has the AcornFox virtual machine installed.
func macVMInstalled() bool {
	_, err := macvm.LoadConfig(macPaths())
	return err == nil
}

func macDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mac doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON 输出：{ok, items:[{ok, level, text, code}]}")
	wake := fs.Bool("wake", false, "先叫起虚拟机再检查里面")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return macDoctorRun(*asJSON, *wake, stdout)
}

// macDoctorRun is acornfox doctor on a Mac that has the virtual machine: the
// Mac side, the Skill's version, and (when the machine is running, or with
// wake) the machine's own doctor.
func macDoctorRun(asJSON, wake bool, stdout io.Writer) int {
	ctx := context.Background()
	in := newMacInstaller(macvm.Options{}, nil)
	if wake {
		if err := in.Start(ctx); err != nil {
			fmt.Fprintln(stdout, "叫不起虚拟机：", err)
		}
	}
	rep := in.Doctor(ctx, true)
	r := doctorReport{OK: rep.OK}
	for _, it := range rep.Items {
		d := doctorItem{OK: it.OK, Level: it.Level, Text: it.Text, Code: it.Code}
		if it.Level == "ok" {
			d.Code = ""
		}
		r.Items = append(r.Items, d)
	}
	skillVersionChecks(&r)
	if remote, ok := macRemoteDoctor(ctx, in); ok {
		for _, it := range remote.Items {
			it.Text = "（虚拟机里）" + it.Text
			r.Items = append(r.Items, it)
			if !it.OK {
				r.OK = false
			}
		}
	}
	if asJSON {
		b, _ := json.Marshal(r)
		fmt.Fprintln(stdout, string(b))
	} else {
		r.print(stdout)
	}
	if !r.OK {
		return 1
	}
	return 0
}

// macRemoteDoctor runs acornfox doctor inside the machine when it is running.
func macRemoteDoctor(ctx context.Context, in *macvm.Installer) (doctorReport, bool) {
	cfg, err := macvm.LoadConfig(in.Paths)
	if err != nil {
		return doctorReport{}, false
	}
	in.Cfg = cfg
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	res, err := in.RunInVM(cctx, "/usr/local/bin/acornfox doctor --json 2>/dev/null\n")
	if err != nil {
		return doctorReport{}, false
	}
	var rep doctorReport
	if json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &rep) != nil {
		return doctorReport{}, false
	}
	return rep, true
}

func macStartStop(verb string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mac "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON 输出")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	in := newMacInstaller(macvm.Options{}, nil)
	ctx := context.Background()
	var err error
	if verb == "start" {
		err = in.Start(ctx)
	} else {
		err = in.Stop(ctx)
	}
	if err != nil {
		if *asJSON {
			fmt.Fprintln(stdout, macvm.JSON(map[string]any{"ok": false, "error": err.Error()}))
		} else {
			fmt.Fprintln(stderr, "没成：", err)
		}
		return 1
	}
	if *asJSON {
		fmt.Fprintln(stdout, macvm.JSON(map[string]any{"ok": true, "action": verb}))
	} else if verb == "start" {
		fmt.Fprintln(stdout, "虚拟机已起来（不用管它，有人访问时也会自己起来）")
	} else {
		fmt.Fprintln(stdout, "虚拟机已停，内存已还给 macOS；下次有人访问（打开控制台、用 acornfox 命令、打开应用）会自动叫起，第一次要等几秒")
	}
	return 0
}

func macSet(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mac set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON 输出")
	mem := fs.Int("memory", 0, "虚拟机内存（GB）")
	cpus := fs.Int("cpus", 0, "虚拟机 CPU 数")
	idle := fs.String("idle", "", "闲置多少分钟停虚拟机（0 或 off 不停）")
	lan := fs.String("lan", "", "局域网里的设备能不能打开（on 或 off）")
	host := fs.String("public-host", "", "应用地址里写的这台 Mac 的地址（auto 是现在的局域网地址）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var o macvm.SetOptions
	o.MemoryGB, o.CPUs, o.PublicHost = *mem, *cpus, *host
	if *idle != "" {
		n := 0
		if *idle != "off" {
			v, err := strconv.Atoi(*idle)
			if err != nil || v < 0 {
				fmt.Fprintln(stderr, "--idle 要写分钟数或 off")
				return 2
			}
			n = v
		}
		o.Idle = &n
	}
	if *lan != "" {
		if *lan != "on" && *lan != "off" {
			fmt.Fprintln(stderr, "--lan 要写 on 或 off")
			return 2
		}
		b := *lan == "on"
		o.LAN = &b
	}
	if o.MemoryGB == 0 && o.CPUs == 0 && o.Idle == nil && o.LAN == nil && o.PublicHost == "" {
		fmt.Fprintln(stderr, "要改什么？--memory、--cpus、--idle、--lan、--public-host（acornfox mac --help）")
		return 2
	}
	in := newMacInstaller(macvm.Options{}, nil)
	done, err := in.Set(context.Background(), o)
	if err != nil {
		if *asJSON {
			fmt.Fprintln(stdout, macvm.JSON(map[string]any{"ok": false, "error": err.Error(), "done": done}))
		} else {
			fmt.Fprintln(stderr, "没改成：", err)
		}
		return 1
	}
	if *asJSON {
		fmt.Fprintln(stdout, macvm.JSON(map[string]any{"ok": true, "done": done}))
	} else {
		for _, d := range done {
			fmt.Fprintln(stdout, "已改：", d)
		}
	}
	return 0
}

// macPackBase builds the OCI image of the virtual machine's base system from an LXC
// root file system (used to prepare the file that goes on the download gateway).
func macPackBase(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "用法：acornfox mac pack-base <rootfs.tar.xz> <输出的 .oci.tar>（做要放进下载网关的虚拟机基础镜像；只在 Mac 上能运行，用系统自带的 tar）")
		return 2
	}
	ctx := context.Background()
	layer, err := macvm.RootfsLayer(ctx, args[0])
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer layer.Close()
	if err := macvm.BuildOCI(layer, macvm.BaseImage, args[1]); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	sum, _ := macvm.FileSHA256(args[1])
	st, _ := os.Stat(args[1])
	fmt.Fprintf(stdout, "已生成 %s（%d 字节，sha256 %s）\n", args[1], st.Size(), sum)
	return 0
}

// ---------------------------------------------------------------- the daemon

// cfgCache re-reads mac.json when it changed.
type cfgCache struct {
	paths macvm.Paths
	mu    sync.Mutex
	mt    time.Time
	cfg   macvm.Config
}

func (c *cfgCache) get() macvm.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, err := os.Stat(c.paths.Config())
	if err != nil {
		return c.cfg
	}
	if st.ModTime().Equal(c.mt) {
		return c.cfg
	}
	if cfg, err := macvm.LoadConfig(c.paths); err == nil {
		c.cfg, c.mt = cfg, st.ModTime()
	}
	return c.cfg
}

func macDaemon(stdout, stderr io.Writer) int {
	paths := macPaths()
	cc := &cfgCache{paths: paths}
	cfg := cc.get()
	if cfg.VM == "" {
		fmt.Fprintln(stderr, "这台 Mac 上没有装 AcornFox（找不到 mac.json），转发程序不运行")
		return 1
	}
	_ = os.MkdirAll(paths.Logs(), 0o700)
	if st, err := os.Stat(paths.DaemonLog()); err == nil && st.Size() > 1<<20 {
		_ = os.Rename(paths.DaemonLog(), paths.DaemonLog()+".1")
	}
	lf, err := os.OpenFile(paths.DaemonLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		lf = nil
	}
	var logMu sync.Mutex
	logf := func(format string, a ...any) {
		line := time.Now().Format("2006-01-02 15:04:05 ") + fmt.Sprintf(format, a...) + "\n"
		logMu.Lock()
		defer logMu.Unlock()
		if lf != nil {
			_, _ = lf.WriteString(line)
		} else {
			fmt.Fprint(stderr, line)
		}
	}
	c := &macvm.Container{Bin: cfg.ContainerBin}
	d := &macvm.Daemon{
		Paths: paths, Cfg: cc.get, Log: logf,
		VM: &macvm.CtrVM{C: c, Name: cfg.VM, Lock: func() (func(), error) { return macvm.LockVM(paths) }},
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	_ = d.Run(ctx)
	logf("转发程序退出")
	return 0
}
