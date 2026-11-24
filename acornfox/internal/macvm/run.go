package macvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Install installs AcornFox on this Mac (Action "install") or upgrades it
// ("upgrade"). Every step is skipped when it was done before, so running it
// again is safe, and with Plan nothing is changed. It returns the exit code
// (0 ok, 1 failed, 5 the administrator password has to be typed by a person).
func (in *Installer) Install(ctx context.Context, w io.Writer) int {
	if in.Opt.Action == "" {
		in.Opt.Action = "install"
	}
	mem := in.Host.MemBytes(ctx)
	cfg, err := LoadConfig(in.Paths)
	existed := err == nil
	if !existed {
		cfg = DefaultConfig(mem)
	}
	in.Cfg = cfg
	if in.Opt.MemoryGB > 0 {
		in.Cfg.MemoryGB = in.Opt.MemoryGB
	}
	if in.Opt.CPUs > 0 {
		in.Cfg.CPUs = in.Opt.CPUs
	}
	if in.Opt.IdleMinutes >= 0 {
		in.Cfg.IdleMinutes = in.Opt.IdleMinutes
	}
	if in.Opt.NoLAN {
		in.Cfg.LAN = false
	}
	switch {
	case in.Opt.PublicHost != "":
		in.Cfg.PublicHost = in.Opt.PublicHost
	case in.Cfg.PublicHost == "":
		in.Cfg.PublicHost = in.Host.LANIP()
	}
	if in.Cfg.PublicHost == "" {
		in.Cfg.PublicHost = "127.0.0.1"
	}
	if in.Opt.Plan {
		in.say("试跑：只说要做什么，不下载、不改任何东西")
	}
	in.logLine(fmt.Sprintf("==== %s %s（版本 %s）====", in.Opt.Action, time.Now().Format(time.RFC3339), in.releaseVersion()))

	type st struct {
		id, title string
		fn        func() (string, string, error)
	}
	steps := []st{
		{"check", "检查这台 Mac", func() (string, string, error) { return in.stepCheck(ctx) }},
		{"container", "苹果的 container", func() (string, string, error) { return in.stepContainer(ctx) }},
		{"system", "container 服务", func() (string, string, error) { return in.stepSystem(ctx) }},
		{"kernel", "虚拟机内核", func() (string, string, error) { return in.stepKernel(ctx) }},
		{"vminit", "虚拟机初始化镜像", func() (string, string, error) { return in.stepVminit(ctx) }},
		{"base-image", "Debian 虚拟机镜像", func() (string, string, error) { return in.stepBaseImage(ctx) }},
		{"machine", "虚拟机", func() (string, string, error) { return in.stepMachine(ctx, existed) }},
		{"provision", "虚拟机里的准备（软件源、ssh）", func() (string, string, error) { return in.stepProvision(ctx) }},
		{"linux", "虚拟机里的 AcornFox", func() (string, string, error) { return in.stepLinux(ctx) }},
		{"daemon", "常驻转发程序", func() (string, string, error) { return in.stepDaemon(ctx, existed) }},
		{"target", "acornfox 命令登记本机", func() (string, string, error) { return in.stepTarget(ctx) }},
		{"cli", "acornfox 命令", func() (string, string, error) { return in.stepCLI() }},
		{"open", "打开控制台", func() (string, string, error) { return in.stepOpen(ctx) }},
	}
	var runErr error
	for _, s := range steps {
		if runErr = in.step(s.id, s.title, s.fn); runErr != nil {
			break
		}
		if s.id == "linux" && !in.Opt.Plan {
			in.deliverKey(ctx)
		}
		if !in.Opt.Plan && (s.id == "machine" || s.id == "linux") {
			_ = in.Cfg.Save(in.Paths) // what was made so far is on record even when a later step fails
		}
	}
	console := fmt.Sprintf("http://127.0.0.1:%d/", LANConsolePort)
	lan := ""
	if in.Cfg.LAN && in.Cfg.PublicHost != "127.0.0.1" {
		lan = fmt.Sprintf("http://%s:%d/", in.Cfg.PublicHost, LANConsolePort)
	}
	var next []string
	if runErr == nil && !in.Opt.Plan {
		in.Cfg.Version = in.releaseVersion()
		_ = in.Cfg.Save(in.Paths)
		next = []string{"浏览器打开 " + console + "（第一次打开时设一个控制台密码）", "让 AI 工作台会用它：acornfox skill install", "看状态：acornfox mac status；有问题：acornfox mac doctor"}
		if !in.Opt.JSON {
			in.summary(console, lan)
		}
	}
	return in.finish(runErr, ifs(runErr == nil, console, ""), lan, next, w)
}

func (in *Installer) summary(console, lan string) {
	idle := ifs(in.Cfg.IdleMinutes > 0, fmt.Sprintf("闲置 %d 分钟没人访问，虚拟机自动停（把内存还给 macOS）；有人访问再自动叫起，第一次要等几秒", in.Cfg.IdleMinutes), "虚拟机一直开着（想省内存：acornfox mac set --idle 30）")
	fmt.Fprintf(in.out(), `
%[1]s AcornFox 已装在这台 Mac 里：一台叫 %[2]s 的 Debian 虚拟机，Docker 和你的应用都在里面。
  • 控制台（浏览器）：%[3]s%[4]s
  • 命令行：acornfox（登记的服务器叫 local）；让 AI 工作台会用它：acornfox skill install
  • %[5]s
  • 管理：acornfox mac status | doctor | stop | start | set | uninstall
  • 日志：%[6]s
`, installerTag, in.Cfg.VM, console, ifs(lan != "", "；局域网里的电脑、手机："+lan, ""), idle, in.Paths.Logs())
}

func (in *Installer) out() io.Writer {
	if in.Out != nil {
		return in.Out
	}
	return io.Discard
}

// Uninstall removes AcornFox from this Mac. By default the virtual machine
// (and the apps and data in it) stays, stopped; with Purge it goes too, with
// everything the install made. Apple's container itself is never removed:
// it may be in use for other things.
func (in *Installer) Uninstall(ctx context.Context, w io.Writer) int {
	in.Opt.Action = "uninstall"
	cfg, err := LoadConfig(in.Paths)
	if err != nil {
		in.say("这台 Mac 上没有装 AcornFox（%v）。没有什么要卸的", err)
		return in.finish(nil, "", "", nil, w)
	}
	in.Cfg = cfg
	if in.Opt.Purge && !in.Opt.Plan && !in.Opt.Yes {
		return in.finish(faultf("MAC-UNINST-01", "--purge 会连虚拟机里的应用和数据一起删掉，删了就找不回来。确认要删请加 --yes"), "", "", nil, w)
	}
	if in.Opt.Plan {
		in.say("试跑：只说要做什么，不改任何东西")
	}
	in.logLine(fmt.Sprintf("==== uninstall purge=%v %s ====", in.Opt.Purge, time.Now().Format(time.RFC3339)))
	label := in.Cfg.Label
	steps := []struct {
		id, title string
		fn        func() (string, string, error)
	}{
		{"daemon", "常驻转发程序", func() (string, string, error) {
			plist := PlistPath(label)
			if in.Opt.Plan {
				return StatePlan, "会停掉并删掉 " + plist, nil
			}
			if err := LaunchUnload(ctx, in.R, label); err != nil {
				return "", "", faultf("MAC-UNINST-02", "停不掉常驻程序：%v", err)
			}
			if err := os.Remove(plist); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", "", faultf("MAC-UNINST-02", "删不掉 %s：%v", plist, err)
			}
			return StateDone, "", nil
		}},
		{"target", "acornfox 命令里登记的 local", func() (string, string, error) {
			if in.Opt.Plan {
				return StatePlan, "会从 acornfox 命令里去掉 local", nil
			}
			exe := in.Exe
			if _, err := os.Stat(in.Paths.Program()); err == nil {
				exe = in.Paths.Program()
			}
			if exe != "" {
				_, _ = Capture(ctx, in.R, nil, exe, "target", "remove", "local")
			}
			return StateDone, "", nil
		}},
		{"vm", "虚拟机", func() (string, string, error) {
			m, ok, err := in.ctr().Find(ctx, in.Cfg.VM)
			if err != nil {
				return "", "", faultf("MAC-UNINST-02", "看不到虚拟机：%v", err)
			}
			if !ok {
				return StateSkip, "没有虚拟机", nil
			}
			if in.Opt.Plan {
				return StatePlan, ifs(in.Opt.Purge, "会停下并删掉虚拟机（连里面的应用和数据）", "会停下虚拟机（数据留着，重装能接上）"), nil
			}
			if m.Running {
				if err := in.ctr().Stop(ctx, in.Cfg.VM); err != nil {
					return "", "", faultf("MAC-UNINST-02", "停不下虚拟机：%v", err)
				}
			}
			if in.Opt.Purge {
				if err := in.ctr().Delete(ctx, in.Cfg.VM); err != nil {
					return "", "", faultf("MAC-UNINST-02", "删不掉虚拟机：%v", err)
				}
				return StateDone, "已删除虚拟机和里面的数据", nil
			}
			return StateDone, "虚拟机已停，数据留着", nil
		}},
	}
	var runErr error
	for _, s := range steps {
		if runErr = in.step(s.id, s.title, s.fn); runErr != nil {
			break
		}
	}
	if runErr == nil && in.Opt.Purge {
		runErr = in.step("files", "镜像、命令和本机记录", func() (string, string, error) {
			if in.Opt.Plan {
				return StatePlan, "会删掉安装时载入的虚拟机镜像、这次装的 acornfox 命令和 " + in.Paths.Root, nil
			}
			if in.Cfg.Created.BaseImage {
				_, _ = Capture(ctx, in.R, nil, in.ctr().bin(), "image", "delete", BaseImage)
			}
			if c := in.Cfg.Created.CLI; c != "" {
				_ = os.Remove(c)
			}
			if in.logFh != nil {
				_ = in.logFh.Close()
				in.logFh = nil
			}
			in.noLog = true
			if err := os.RemoveAll(in.Paths.Root); err != nil {
				return "", "", faultf("MAC-UNINST-02", "删不掉 %s：%v", in.Paths.Root, err)
			}
			return StateDone, "", nil
		})
	}
	if runErr == nil && !in.Opt.Plan {
		in.logFh = nil
		if in.Opt.Purge {
			in.say("已全部卸载。苹果的 container 本身没有卸（你可能还在用它；不用的话运行 container 自带的卸载脚本）")
		} else {
			in.say("已卸载（虚拟机和里面的应用、数据留着，重新运行安装命令就接上）；要连数据一起删：acornfox mac uninstall --purge --yes")
		}
	}
	return in.finish(runErr, "", "", nil, w)
}

// ---------------------------------------------------------------- status and checks

// VMStatus is the machine part of Status.
type VMStatus struct {
	Name     string `json:"name"`
	Exists   bool   `json:"exists"`
	State    string `json:"state"`
	IP       string `json:"ip,omitempty"`
	MemoryGB int    `json:"memory_gb"`
	CPUs     int    `json:"cpus"`
}

// Status is what `acornfox mac status --json` prints.
type Status struct {
	OK          bool       `json:"ok"`
	Installed   bool       `json:"installed"`
	Version     string     `json:"version,omitempty"`
	Container   string     `json:"container,omitempty"`
	VM          VMStatus   `json:"vm"`
	DaemonLoad  bool       `json:"daemon_loaded"`
	Forwarding  bool       `json:"forwarding"`
	IdleMinutes int        `json:"idle_minutes"`
	LAN         bool       `json:"lan"`
	ConsoleURL  string     `json:"console_url,omitempty"`
	LANURL      string     `json:"lan_url,omitempty"`
	Ports       []PortSpec `json:"ports,omitempty"`
	Root        string     `json:"root"`
}

// Status reads the state without changing anything (and without waking the machine).
func (in *Installer) Status(ctx context.Context) Status {
	st := Status{Root: in.Paths.Root}
	cfg, err := LoadConfig(in.Paths)
	if err != nil {
		return st
	}
	in.Cfg = cfg
	st.Installed, st.Version, st.IdleMinutes, st.LAN = true, cfg.Version, cfg.IdleMinutes, cfg.LAN
	st.VM = VMStatus{Name: cfg.VM, State: "unknown", MemoryGB: cfg.MemoryGB, CPUs: cfg.CPUs}
	if v, err := in.ctr().Version(ctx); err == nil {
		st.Container = v
	}
	if m, ok, err := in.ctr().Find(ctx, cfg.VM); err == nil {
		st.VM.Exists = ok
		if ok {
			st.VM.State, st.VM.IP = ifs(m.Running, "running", "stopped"), m.IP
		} else {
			st.VM.State = "absent"
		}
	}
	st.DaemonLoad = LaunchLoaded(ctx, in.R, cfg.Label)
	st.Forwarding = portInUse(cfg.SSHPort)
	st.Ports = LoadPorts(in.Paths)
	st.ConsoleURL = fmt.Sprintf("http://127.0.0.1:%d/", LANConsolePort)
	if cfg.LAN && cfg.PublicHost != "" && cfg.PublicHost != "127.0.0.1" {
		st.LANURL = fmt.Sprintf("http://%s:%d/", cfg.PublicHost, LANConsolePort)
	}
	st.OK = st.VM.Exists && st.DaemonLoad && st.Forwarding
	return st
}

// PrintStatus prints Status for a person.
func PrintStatus(w io.Writer, st Status) {
	if !st.Installed {
		fmt.Fprintln(w, "这台 Mac 上没有装 AcornFox（要装：curl -fsSL <get.sh 地址> | bash，见 README 的 Mac 一节）")
		return
	}
	state := map[string]string{"running": "运行中", "stopped": "已停（省内存；有人访问会自动叫起）", "absent": "不存在", "unknown": "不知道"}[st.VM.State]
	fmt.Fprintf(w, "AcornFox %s（这台 Mac 上）\n", ifs(st.Version != "", st.Version, "版本未记录"))
	fmt.Fprintf(w, "  虚拟机 %s：%s%s（%d 个 CPU、%d GB 内存上限）\n", st.VM.Name, state, ifs(st.VM.IP != "", "，地址 "+st.VM.IP, ""), st.VM.CPUs, st.VM.MemoryGB)
	fmt.Fprintf(w, "  常驻转发程序：%s；本机转发 ssh 端口：%s\n", ifs(st.DaemonLoad, "已加载", "没加载（MAC-LAUNCH-01）"), ifs(st.Forwarding, "在听", "没人听"))
	fmt.Fprintf(w, "  闲置 %s 停虚拟机；局域网：%s\n", ifs(st.IdleMinutes > 0, fmt.Sprintf("%d 分钟", st.IdleMinutes), "不会（一直开着）"), ifs(st.LAN, "开", "关"))
	fmt.Fprintf(w, "  控制台：%s%s\n", st.ConsoleURL, ifs(st.LANURL != "", "；局域网："+st.LANURL, ""))
}

// DoctorItem is one line of the report (the same shape as acornfox doctor).
type DoctorItem struct {
	OK    bool   `json:"ok"`
	Level string `json:"level"` // ok | warn | fail
	Text  string `json:"text"`
	Code  string `json:"code,omitempty"`
}

// DoctorReport is the result of the checks.
type DoctorReport struct {
	OK    bool         `json:"ok"`
	Items []DoctorItem `json:"items"`
}

func (r *DoctorReport) add(level, code, format string, args ...any) {
	it := DoctorItem{OK: level != "fail", Level: level, Text: fmt.Sprintf(format, args...), Code: code}
	r.Items = append(r.Items, it)
	if level == "fail" {
		r.OK = false
	}
}

// Doctor checks the Mac side one by one, each failure with its fault number.
// It only reads (and never wakes the machine); with deep it also looks inside
// the machine when it is running.
func (in *Installer) Doctor(ctx context.Context, deep bool) DoctorReport {
	r := DoctorReport{OK: true}
	h := in.Host
	if h.GOOS() != "darwin" {
		r.add("fail", "MAC-OS-01", "这不是 Mac")
		return r
	}
	if h.Arch() != "arm64" {
		r.add("fail", "MAC-ARCH-01", "不是苹果芯片")
	} else if v := h.MacOSVersion(ctx); v != "" {
		if m := versionRE.FindStringSubmatch(v); m != nil && atoi(m[1]) < MinMacOSMajor {
			r.add("fail", "MAC-OS-01", "macOS %s 太旧，要 %d 或更新", v, MinMacOSMajor)
		} else {
			r.add("ok", "", "macOS %s，苹果芯片", v)
		}
	}
	home, _ := os.UserHomeDir()
	if free := h.FreeDiskGB(home); free >= 0 {
		if free < 5 {
			r.add("fail", "MAC-DISK-01", "磁盘只剩 %d GB", free)
		} else {
			r.add("ok", "", "磁盘剩 %d GB", free)
		}
	}
	cfg, err := LoadConfig(in.Paths)
	if err != nil {
		r.add("fail", "", "这台 Mac 上没有装 AcornFox（找不到 %s）", in.Paths.Config())
		return r
	}
	in.Cfg = cfg
	c := in.ctr()
	cv, err := c.Version(ctx)
	if err != nil {
		r.add("fail", "MAC-CTR-01", "找不到苹果的 container 程序（%v）", err)
		return r
	}
	r.add("ok", "", "苹果的 container %s", cv)
	if !c.SystemRunning(ctx) {
		r.add("warn", "MAC-CTR-01", "container 服务没在运行（有人访问时常驻程序会自己启动它；要手动：container system start）")
	} else {
		r.add("ok", "", "container 服务在运行")
	}
	if _, err := os.Stat(kernelPath()); err != nil {
		r.add("fail", "MAC-KERNEL-01", "没有默认内核（%s）", kernelPath())
	}
	m, ok, err := c.Find(ctx, cfg.VM)
	switch {
	case err != nil:
		r.add("fail", "MAC-VM-01", "看不到虚拟机列表：%v", err)
	case !ok:
		r.add("fail", "MAC-VM-01", "虚拟机 %s 不存在（卸载过？重新运行安装命令）", cfg.VM)
	case m.Running:
		r.add("ok", "", "虚拟机 %s 在运行（%s）", cfg.VM, m.IP)
	default:
		r.add("ok", "", "虚拟机 %s 已停（闲置自动停的；有人访问会自动叫起）", cfg.VM)
	}
	if LaunchLoaded(ctx, in.R, cfg.Label) {
		r.add("ok", "", "常驻转发程序已加载（%s）", cfg.Label)
		if portInUse(cfg.SSHPort) {
			r.add("ok", "", "Mac 上 %d 端口有人在转发", cfg.SSHPort)
		} else {
			r.add("fail", "MAC-LAUNCH-01", "常驻程序加载了，但 Mac 上 %d 端口没人听；看 %s", cfg.SSHPort, in.Paths.DaemonLog())
		}
	} else {
		r.add("fail", "MAC-LAUNCH-01", "常驻转发程序没加载：重新运行安装命令会装回去；手动：launchctl bootstrap gui/$(id -u) %s", PlistPath(cfg.Label))
	}
	if b, err := os.ReadFile(in.Paths.DaemonLog()); err == nil && strings.Contains(lastBytes(b, 6000), "MAC-PORT-01") {
		r.add("warn", "MAC-PORT-01", "转发程序的日志里有端口被占用的记录（%s）", in.Paths.DaemonLog())
	}
	if cfg.PublicHost != "" && cfg.PublicHost != "127.0.0.1" {
		if cur := h.LANIP(); cur != "" && cur != cfg.PublicHost {
			r.add("warn", "MAC-NET-01", "这台 Mac 现在的局域网地址是 %s，应用地址里写的还是 %s（路由器换了地址）：acornfox mac set --public-host auto", cur, cfg.PublicHost)
		}
	}
	if _, err := os.Stat(in.Paths.SSHKey()); err != nil {
		r.add("fail", "MAC-SSH-01", "找不到登录虚拟机用的钥匙（%s）", in.Paths.SSHKey())
	}
	if b, err := os.ReadFile(h.TargetsFile()); err != nil || !strings.Contains(string(b), `"local"`) {
		r.add("warn", "MAC-SSH-01", "acornfox 命令里没有登记 local 这台服务器（acornfox target add …，或重新运行安装命令）")
	} else {
		r.add("ok", "", "acornfox 命令已登记 local")
	}
	if deep && ok && m.Running {
		out, err := in.scriptOut(ctx, `for u in docker acornfox-server acornfox-runner ssh; do printf '%s=%s\n' "$u" "$(systemctl is-active $u 2>/dev/null)"; done`)
		if err != nil {
			r.add("fail", "MAC-VM-02", "虚拟机没有应答：%v", err)
		} else {
			bad := ""
			for _, l := range strings.Split(out, "\n") {
				if strings.HasSuffix(strings.TrimSpace(l), "=active") {
					continue
				}
				bad += l + " "
			}
			if bad != "" {
				r.add("fail", "MAC-LINUX-01", "虚拟机里的服务不正常：%s（里面运行 acornfox doctor 看详细）", strings.TrimSpace(bad))
			} else {
				r.add("ok", "", "虚拟机里 docker、acornfox-server、acornfox-runner、ssh 都在运行")
			}
		}
	}
	return r
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func lastBytes(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

// PrintDoctor prints a report for a person.
func PrintDoctor(w io.Writer, r DoctorReport) {
	for _, it := range r.Items {
		mark := map[string]string{"ok": "✓", "warn": "!", "fail": "✗"}[it.Level]
		fmt.Fprintf(w, "  %s %s%s\n", mark, it.Text, ifs(it.Code != "", "  [故障编号 "+it.Code+"]", ""))
	}
	fmt.Fprintln(w, ifs(r.OK, "没有发现问题", "有问题，按故障编号运行 acornfox faults 编号 看处理办法"))
}

// JSON of a status or report, one line.
func JSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------------------------------------------------------------- start, stop, set

// Start boots the machine now (it would also start by itself at the first access).
func (in *Installer) Start(ctx context.Context) error {
	cfg, err := LoadConfig(in.Paths)
	if err != nil {
		return err
	}
	in.Cfg = cfg
	return in.vm().Start(ctx)
}

// Stop stops the machine now, which gives its memory back to macOS. The
// resident program starts it again at the next access.
func (in *Installer) Stop(ctx context.Context) error {
	cfg, err := LoadConfig(in.Paths)
	if err != nil {
		return err
	}
	in.Cfg = cfg
	m, ok, err := in.ctr().Find(ctx, cfg.VM)
	if err != nil {
		return err
	}
	if !ok || !m.Running {
		return nil
	}
	return in.vm().Stop(ctx)
}

// SetOptions are what `acornfox mac set` changes (nil/0 = leave as is).
type SetOptions struct {
	MemoryGB int
	CPUs     int
	Idle     *int
	LAN      *bool
	// PublicHost is the Mac's address written into the app addresses ("auto" = the
	// address it has now). Changing it restarts the AcornFox service in the machine
	// (the apps keep running).
	PublicHost string
}

// Set changes settings. Idle time and LAN take effect within seconds (the
// resident program reads them again); memory and CPUs are applied to the
// machine and take effect when it starts next.
func (in *Installer) Set(ctx context.Context, o SetOptions) ([]string, error) {
	cfg, err := LoadConfig(in.Paths)
	if err != nil {
		return nil, err
	}
	in.Cfg = cfg
	var done []string
	if o.Idle != nil {
		in.Cfg.IdleMinutes = *o.Idle
		done = append(done, ifs(*o.Idle > 0, fmt.Sprintf("闲置 %d 分钟停虚拟机", *o.Idle), "虚拟机一直开着，不再闲置停机"))
	}
	if o.LAN != nil {
		in.Cfg.LAN = *o.LAN
		done = append(done, ifs(*o.LAN, "局域网里的设备也能打开不是“只本机”的应用和控制台", "只有这台 Mac 自己能打开"))
	}
	if o.PublicHost != "" {
		host := o.PublicHost
		if host == "auto" {
			host = in.Host.LANIP()
			if host == "" {
				host = "127.0.0.1"
			}
		}
		if !strings.ContainsAny(host, " '\"$;&|`\n") && host != "" {
			if err := in.vm().Start(ctx); err != nil {
				return done, err
			}
			script := "set -e\nf=/etc/systemd/system/acornfox-server.service\nsed -i -E 's/(--public-host )[^ ]+/\\1" + host + "/' \"$f\"\nsystemctl daemon-reload\nsystemctl restart acornfox-server\necho afx-host-ok\n"
			res, err := in.ctr().Script(ctx, cfg.VM, script, nil)
			if err != nil || !strings.Contains(res.Stdout, "afx-host-ok") {
				return done, fmt.Errorf("改虚拟机里的服务地址没成功：%v %s", err, firstLine(res.Stderr))
			}
			in.Cfg.PublicHost = host
			done = append(done, "应用地址里写的这台 Mac 的地址改成 "+host+"（虚拟机里的 AcornFox 服务重启了一下，应用没停）")
		} else {
			return done, fmt.Errorf("地址写法不对：%q", host)
		}
	}
	var args []string
	if o.MemoryGB > 0 {
		in.Cfg.MemoryGB = o.MemoryGB
		args = append(args, fmt.Sprintf("memory=%dG", o.MemoryGB))
	}
	if o.CPUs > 0 {
		in.Cfg.CPUs = o.CPUs
		args = append(args, fmt.Sprintf("cpus=%d", o.CPUs))
	}
	if len(args) > 0 {
		res, err := Capture(ctx, in.R, nil, in.ctr().bin(), append([]string{"machine", "set", "--name", cfg.VM}, args...)...)
		if err != nil {
			return done, err
		}
		if res.Code != 0 {
			return done, fmt.Errorf("container machine set 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
		}
		done = append(done, strings.Join(args, "、")+"（下次启动虚拟机时生效；现在要生效：acornfox mac stop，下次访问自动起来）")
	}
	if err := in.Cfg.Save(in.Paths); err != nil {
		return done, err
	}
	return done, nil
}

// LookExe is the path of the running program.
func LookExe() string {
	p, err := os.Executable()
	if err != nil {
		p, _ = exec.LookPath("acornfox")
	}
	return p
}
