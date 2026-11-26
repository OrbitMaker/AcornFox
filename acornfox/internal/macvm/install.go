package macvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/platform"
)

// Fault is a failure with a fault number (MAC-…; `acornfox faults MAC-…`).
type Fault struct {
	Code   string
	Detail string
	Exit   int // exit code of the command; 0 means 1
}

func (f *Fault) Error() string { return f.Code + " " + f.Detail }

func faultf(code, format string, args ...any) *Fault {
	return &Fault{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// Exit codes of acornfox mac install and friends.
const (
	ExitOK       = 0
	ExitFail     = 1
	ExitUsage    = 2
	ExitAdmin    = 5 // the administrator password is needed and a person has to type it
	StateDone    = "done"
	StateSkip    = "skip"
	StatePlan    = "plan"
	StateFail    = "fail"
	installerTag = "[AcornFox]"
)

// StepResult is one step of an install or uninstall, as it shows in --json.
type StepResult struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// Options are what the command line says.
type Options struct {
	Action      string // install | upgrade | uninstall
	Version     string // release to install (vX.Y.Z); "" = this program's own version
	China       bool
	Global      bool
	ReleaseBase string // test or own mirror: <base>/releases/download/<version>/<file>
	GiteeRepo   string
	GithubRepo  string
	Gateway     string // gateway address; "off" = not used; "" = the default
	Plan        bool
	JSON        bool
	Yes         bool
	Purge       bool
	NoOpen      bool
	NoLAN       bool
	MemoryGB    int // 0 = by the Mac's memory
	CPUs        int
	IdleMinutes int // -1 = not given
	PublicHost  string
	// From is the install source tag (internal/installfrom): passed to the install script in the
	// virtual machine as --from on a fresh install, never on upgrade or uninstall. "" = none;
	// an unusable one is dropped before it gets here (cmd/acornfox/mac.go).
	From string
	// LocalPackage is a folder with install.sh, upgrade.sh, acornfox_linux_arm64 and the
	// edge package: used instead of downloading them (tests).
	LocalPackage string
	InstallDir   string // where the acornfox command is put; "" = the first writable of the usual places
}

// Installer installs, upgrades and removes AcornFox on this Mac.
type Installer struct {
	Paths Paths
	Opt   Options
	R     Runner
	C     *Container
	Host  Host
	HTTP  *http.Client
	Out   io.Writer // what a person reads (standard error with --json)
	Exe   string    // the running acornfox program (it is copied)
	// FetchFn brings an Artifact (nil: the download of fetcher(); tests replace it).
	FetchFn func(ctx context.Context, a Artifact) (string, error)
	Version string // version of this program
	Cfg     Config

	steps  []StepResult
	faults []string
	logFh  *os.File
	noLog  bool // the state folder was deleted: do not write the log into it again
	failed bool // a step already printed what went wrong
	last   time.Time
}

// ---------------------------------------------------------------- plumbing

func (in *Installer) say(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if in.Out != nil {
		fmt.Fprintf(in.Out, "%s %s\n", installerTag, line)
	}
	in.logLine(line)
}

func (in *Installer) warn(format string, args ...any) {
	in.say("注意："+format, args...)
}

func (in *Installer) logLine(line string) {
	if in.Opt.Plan || in.noLog {
		return
	}
	if in.logFh == nil {
		if err := os.MkdirAll(in.Paths.Logs(), 0o700); err != nil {
			return
		}
		f, err := os.OpenFile(in.Paths.InstallLog(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		in.logFh = f
	}
	fmt.Fprintf(in.logFh, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), line)
}

func (in *Installer) client() *http.Client {
	if in.HTTP != nil {
		return in.HTTP
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second}}
}

func (in *Installer) ctr() *Container {
	if in.C == nil {
		bin := in.Cfg.ContainerBin
		if bin == "" {
			bin = findContainer()
		}
		in.C = &Container{Bin: bin, R: in.R}
	}
	return in.C
}

// findContainer is the container program: on PATH, or where Apple's installer puts it.
func findContainer() string {
	for _, p := range []string{"/usr/local/bin/container", "/opt/homebrew/bin/container"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return "container"
}

func (in *Installer) fetcher() *Fetcher {
	gw := platform.Network{Gateway: in.Opt.Gateway}.EffectiveGateway()
	if in.Opt.Gateway == "off" {
		gw = ""
	}
	return &Fetcher{
		Dir: in.Paths.Downloads(), China: in.china(), Gateway: gw, DataDir: in.Paths.Root, Version: in.releaseVersion(), HTTP: in.client(),
		Say: in.say,
		Progress: func(a Artifact, src string, p appinstall.FileProgress) {
			if time.Since(in.last) < 5*time.Second {
				return
			}
			in.last = time.Now()
			if p.Verifying {
				in.say("  %s：下完了，正在核对校验值…", a.Title)
				return
			}
			if p.Total > 0 {
				in.say("  %s：%d / %d MB（%d 路）", a.Title, p.Done>>20, p.Total>>20, p.Conns)
			}
		},
	}
}

func (in *Installer) fetch(ctx context.Context, a Artifact) (string, error) {
	if in.FetchFn != nil {
		return in.FetchFn(ctx, a)
	}
	return in.fetcher().Fetch(ctx, a)
}

// china says whether the mirrors in mainland China go first. --china / --global are the person's word;
// without them the two sources are measured once (see region.go) and the faster goes first.
func (in *Installer) china() bool {
	if in.Opt.China || in.Opt.Global {
		if note := os.Getenv("ACORNFOX_GET_PROBE_NOTE"); note != "" && !chinaCache.noted {
			chinaCache.noted = true
			in.say("%s（get.sh 的测速，已作为 --china / --global 传到这里）", note)
		}
		return in.Opt.China
	}
	if v, ok := in.chinaGuess(); ok {
		return v
	}
	return false
}

var chinaCache struct {
	set     bool
	v       bool
	decided bool // the speed test told which is faster (not "neither brought data")
	noted   bool
}

// chinaGuess measures Gitee and GitHub once per run. When neither brought data, Gitee goes first (and nothing is
// passed on: the install script in the virtual machine decides for itself).
func (in *Installer) chinaGuess() (bool, bool) {
	if chinaCache.set {
		return chinaCache.v, true
	}
	if in.Opt.ReleaseBase != "" || in.Opt.GiteeRepo == "-" {
		return false, false
	}
	giteeURL, githubURL := in.probeURLs()
	r := ProbeSources(context.Background(), in.client(), giteeURL, githubURL, probeWindow())
	in.say("%s", r.Note)
	chinaCache.set = true
	chinaCache.decided = r.Pick != ""
	chinaCache.v = r.Pick != "github"
	return chinaCache.v, true
}

// regionArg is what goes to install.sh / upgrade.sh inside the virtual machine: the person's word, or what the speed test decided.
func (in *Installer) regionArg() string {
	var region string
	if in.Opt.China {
		region = " --china"
	} else if in.Opt.Global {
		region = " --global"
	} else {
		in.china() // measures once if it has not yet
		if chinaCache.decided && chinaCache.v {
			region = " --china"
		} else if chinaCache.decided {
			region = " --global"
		}
	}
	return region
}

func (in *Installer) releaseVersion() string {
	v := in.Opt.Version
	if v == "" {
		v = in.Version
	}
	if v != "" && !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

// step runs fn as one step: it records the result, prints it and stops the run on a failure.
func (in *Installer) step(id, title string, fn func() (state, detail string, err error)) error {
	state, detail, err := fn()
	if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			in.faults = append(in.faults, f.Code)
			detail = f.Code + " " + f.Detail
		} else {
			detail = err.Error()
		}
		in.steps = append(in.steps, StepResult{ID: id, Title: title, State: StateFail, Detail: detail})
		in.say("%s：失败 —— %s", title, detail)
		in.failed = true
		return err
	}
	in.steps = append(in.steps, StepResult{ID: id, Title: title, State: state, Detail: detail})
	switch state {
	case StateSkip:
		in.say("%s：已做过，跳过（%s）", title, detail)
	case StatePlan:
		in.say("（试跑）%s：%s", title, detail)
	default:
		in.say("%s：完成%s", title, ifs(detail != "", "（"+detail+"）", ""))
	}
	return nil
}

func ifs(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

type jsonResult struct {
	OK       bool         `json:"ok"`
	Action   string       `json:"action"`
	ExitCode int          `json:"exit_code"`
	Plan     bool         `json:"plan,omitempty"`
	Steps    []StepResult `json:"steps"`
	Faults   []string     `json:"faults"`
	Console  string       `json:"console_url,omitempty"`
	LAN      string       `json:"lan_url,omitempty"`
	Next     []string     `json:"next,omitempty"`
}

// finish prints the JSON result when asked and returns the exit code.
func (in *Installer) finish(err error, console, lan string, next []string, w io.Writer) int {
	code := ExitOK
	if err != nil {
		code = ExitFail
		var f *Fault
		if errors.As(err, &f) {
			if f.Exit != 0 {
				code = f.Exit
			}
			known := false
			for _, c := range in.faults {
				known = known || c == f.Code
			}
			if !known {
				in.faults = append(in.faults, f.Code)
			}
		}
	}
	if in.logFh != nil {
		in.logLine(fmt.Sprintf("结束，退出码 %d", code))
		_ = in.logFh.Close()
		in.logFh = nil
	}
	if in.Opt.JSON {
		faults := in.faults
		if faults == nil {
			faults = []string{}
		}
		steps := in.steps
		if steps == nil {
			steps = []StepResult{}
		}
		b, _ := json.Marshal(jsonResult{OK: err == nil, Action: in.Opt.Action, ExitCode: code, Plan: in.Opt.Plan, Steps: steps, Faults: faults, Console: console, LAN: lan, Next: next})
		fmt.Fprintln(w, string(b))
	} else if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			if !in.failed {
				fmt.Fprintf(w, "%s %s\n", installerTag, f.Detail)
			}
			fmt.Fprintf(w, "故障编号: %s（acornfox faults %s 看处理办法）\n", f.Code, f.Code)
		}
	}
	return code
}

// ---------------------------------------------------------------- the steps

var versionRE = regexp.MustCompile(`^(\d+)`)

func (in *Installer) stepCheck(ctx context.Context) (string, string, error) {
	h := in.Host
	if h.GOOS() != "darwin" {
		return "", "", faultf("MAC-OS-01", "这不是 Mac（%s）。Linux 服务器用 get.sh 装在服务器上，Windows 用 get.ps1", h.GOOS())
	}
	if h.Arch() != "arm64" {
		return "", "", faultf("MAC-ARCH-01", "这台 Mac 不是苹果芯片（Intel）。苹果的 container 只在苹果芯片上运行，装不了")
	}
	ver := h.MacOSVersion(ctx)
	if m := versionRE.FindStringSubmatch(ver); m != nil {
		if major, _ := strconv.Atoi(m[1]); major < MinMacOSMajor {
			return "", "", faultf("MAC-OS-01", "macOS %s 太旧，苹果的 container 需要 macOS %d 或更新", ver, MinMacOSMajor)
		}
	} else if ver == "" {
		in.warn("读不到 macOS 版本号，先往下做")
	}
	home, _ := os.UserHomeDir()
	free := h.FreeDiskGB(home)
	if free >= 0 && free < MinFreeDiskGB {
		return "", "", faultf("MAC-DISK-01", "磁盘只剩 %d GB，至少要 %d GB（苹果的 container 约 0.5 GB、虚拟机镜像约 1 GB、Docker 和应用另算）", free, MinFreeDiskGB)
	}
	mem := h.MemBytes(ctx)
	return StateDone, fmt.Sprintf("macOS %s，苹果芯片，内存 %d GB，磁盘剩 %d GB；虚拟机给 %d GB 内存、%d 个 CPU", ver, mem>>30, free, in.Cfg.MemoryGB, in.Cfg.CPUs), nil
}

func (in *Installer) stepContainer(ctx context.Context) (string, string, error) {
	c := in.ctr()
	if v, err := c.Version(ctx); err == nil && v != "" {
		in.Cfg.ContainerBin = c.Bin
		return StateSkip, "苹果的 container " + v, nil
	}
	if in.Opt.Plan {
		return StatePlan, "没有装苹果的 container：会下载它的安装包（约 170 MB），然后在终端里用 sudo 装（要你输一次这台 Mac 的登录密码）", nil
	}
	path, err := in.fetch(ctx, ContainerPkg())
	if err != nil {
		return "", "", faultf("MAC-DL-01", "%v", err)
	}
	cmd := fmt.Sprintf("sudo installer -pkg '%s' -target /", path)
	if !in.Host.HasTTY() {
		return "", "", &Fault{Code: "MAC-ADMIN-01", Exit: ExitAdmin, Detail: "安装苹果的 container 要管理员密码，这里没有终端可以输。请你自己在“终端”里运行下面这一行（输入时屏幕不显示字符，这是正常的），装好后再运行一次安装命令：\n    " + cmd}
	}
	in.say("下一步要装苹果的 container，会让你输这台 Mac 的登录密码（输入时屏幕不显示字符，这是正常的）：")
	code, err := in.R.Run(ctx, nil, os.Stdout, os.Stderr, "sudo", "installer", "-pkg", path, "-target", "/")
	if err != nil {
		return "", "", faultf("MAC-CTR-01", "运行 sudo 失败：%v", err)
	}
	if code != 0 {
		return "", "", &Fault{Code: "MAC-ADMIN-01", Exit: ExitAdmin, Detail: fmt.Sprintf("管理员安装没成功（退出码 %d：密码输错、取消，或这个账号不是管理员）。可以自己在终端里运行：\n    %s", code, cmd)}
	}
	c.Bin = findContainer()
	v, err := c.Version(ctx)
	if err != nil {
		return "", "", faultf("MAC-CTR-01", "安装包装完了但还是运行不了 container：%v", err)
	}
	in.Cfg.ContainerBin = c.Bin
	return StateDone, "苹果的 container " + v, nil
}

func (in *Installer) stepSystem(ctx context.Context) (string, string, error) {
	c := in.ctr()
	if c.SystemRunning(ctx) {
		return StateSkip, "container 服务在运行", nil
	}
	if in.Opt.Plan {
		return StatePlan, "会启动 container 服务（container system start）", nil
	}
	if err := c.SystemStart(ctx); err != nil {
		return "", "", faultf("MAC-CTR-01", "container 服务启动不了：%v", err)
	}
	return StateDone, "已启动 container 服务", nil
}

func kernelPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "com.apple.container", "kernels", "default.kernel-arm64")
}

func (in *Installer) stepKernel(ctx context.Context) (string, string, error) {
	if _, err := os.Stat(kernelPath()); err == nil {
		return StateSkip, "已有默认内核", nil
	}
	if in.Opt.Plan {
		return StatePlan, "没有默认内核：会经下载网关下载（约 676 MB，里面是虚拟机用的 Linux 内核）再装上", nil
	}
	path, err := in.fetch(ctx, KataKernel())
	if err != nil {
		return "", "", faultf("MAC-DL-01", "%v", err)
	}
	res, err := Capture(ctx, in.R, nil, in.ctr().bin(), "system", "kernel", "set", "--arch", "arm64", "--tar", path, "--binary", KernelMember, "--force")
	if err != nil {
		return "", "", faultf("MAC-KERNEL-01", "%v", err)
	}
	if res.Code != 0 {
		return "", "", faultf("MAC-KERNEL-01", "container system kernel set 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
	}
	return StateDone, "已装默认内核", nil
}

// hasImage says whether Apple's container has the image (by `image inspect`:
// the init image of the virtual machines is not shown by `image list`).
func (in *Installer) hasImage(ctx context.Context, ref string) bool {
	res, err := Capture(ctx, in.R, nil, in.ctr().bin(), "image", "inspect", ref)
	return err == nil && res.Code == 0
}

var vminitRE = regexp.MustCompile(`(?m)^\[vminit\]\s*\n(?:[^\[\n]*\n)*?image\s*=\s*"([^"]+)"`)

func (in *Installer) vminitRef(ctx context.Context) string {
	res, err := Capture(ctx, in.R, nil, in.ctr().bin(), "system", "property", "list")
	if err == nil && res.Code == 0 {
		if m := vminitRE.FindStringSubmatch(res.Stdout); m != nil {
			return m[1]
		}
	}
	return "ghcr.io/apple/containerization/vminit:0.54.0"
}

func (in *Installer) stepVminit(ctx context.Context) (string, string, error) {
	ref := in.vminitRef(ctx)
	short := ref[strings.LastIndex(ref, "/")+1:] // vminit:0.54.0
	if in.hasImage(ctx, ref) {
		return StateSkip, "已有 " + short, nil
	}
	if in.Opt.Plan {
		return StatePlan, "会拉取虚拟机的初始化镜像 " + short + "（约 70 MB；国内走南京大学的镜像站）", nil
	}
	c := in.ctr().bin()
	rest := strings.TrimPrefix(ref, "ghcr.io/")
	var sources []string
	if in.china() {
		sources = []string{"ghcr.nju.edu.cn/" + rest, ref}
	} else {
		sources = []string{ref, "ghcr.nju.edu.cn/" + rest}
	}
	var last string
	for _, s := range sources {
		in.say("拉取 %s …", s)
		res, err := Capture(ctx, in.R, nil, c, "image", "pull", "--platform", "linux/arm64", "--progress", "none", s)
		if err != nil {
			last = err.Error()
			continue
		}
		if res.Code != 0 {
			last = firstLine(res.Stderr + res.Stdout)
			continue
		}
		if s != ref {
			if r2, err := Capture(ctx, in.R, nil, c, "image", "tag", s, ref); err != nil || r2.Code != 0 {
				last = "改名没成功：" + firstLine(r2.Stderr+r2.Stdout)
				continue
			}
		}
		return StateDone, "已拉取 " + short, nil
	}
	return "", "", faultf("MAC-IMG-01", "虚拟机的初始化镜像 %s 拉不下来（%s）", ref, last)
}

// BasePrebuilt is the finished base image on the download gateway, once it is
// put there (SHA256 is filled in then; until then the gateway is not asked).
func BasePrebuilt() Artifact {
	return Artifact{ID: "base-image", Title: "虚拟机的 Debian 镜像", File: "acornfox-base-debian13-arm64.oci.tar", GatewayID: "acornfox-base-debian13-arm64"}
}

func (in *Installer) stepBaseImage(ctx context.Context) (string, string, error) {
	if in.hasImage(ctx, BaseImage) {
		return StateSkip, "已有 " + BaseImage, nil
	}
	if in.Opt.Plan {
		return StatePlan, "会准备 Debian 13 的虚拟机镜像（约 100 MB：下载根文件系统、打成镜像、载入 container）", nil
	}
	if _, err := os.Stat(in.Paths.Work()); err != nil {
		if err := os.MkdirAll(in.Paths.Work(), 0o700); err != nil {
			return "", "", faultf("MAC-IMG-01", "建不了工作目录：%v", err)
		}
	}
	c := in.ctr().bin()
	load := func(tarPath string) error {
		res, err := Capture(ctx, in.R, nil, c, "image", "load", "--input", tarPath)
		if err != nil {
			return err
		}
		if res.Code != 0 {
			return fmt.Errorf("container image load 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
		}
		return nil
	}
	if pre := BasePrebuilt(); pre.SHA256 != "" {
		if p, err := in.fetch(ctx, pre); err == nil {
			if err := load(p); err == nil {
				return StateDone, "用下载网关上做好的镜像", nil
			}
		}
	}
	rootfs, err := in.fetchRootfs(ctx)
	if err != nil {
		return "", "", faultf("MAC-IMG-01", "%v", err)
	}
	out := filepath.Join(in.Paths.Work(), "acornfox-base-debian13.oci.tar")
	defer os.Remove(out)
	in.say("把根文件系统打成 container 能载入的镜像…")
	layer, err := RootfsLayer(ctx, rootfs)
	if err != nil {
		return "", "", faultf("MAC-IMG-01", "运行 tar 失败：%v", err)
	}
	err = BuildOCI(layer, BaseImage, out)
	layer.Close()
	if err != nil {
		return "", "", faultf("MAC-IMG-01", "%v", err)
	}
	if err := load(out); err != nil {
		return "", "", faultf("MAC-IMG-01", "%v", err)
	}
	in.Cfg.Created.BaseImage = true
	return StateDone, "已载入 " + BaseImage, nil
}

// fetchRootfs downloads the newest Debian 13 arm64 LXC root file system.
func (in *Installer) fetchRootfs(ctx context.Context) (string, error) {
	var reasons []string
	for _, base := range LXCRootfsBases(in.china()) {
		page, err := getText(ctx, in.client(), base)
		if err != nil {
			reasons = append(reasons, hostOf(base)+"："+err.Error())
			continue
		}
		dir := LatestLXCDir(page)
		if dir == "" {
			reasons = append(reasons, hostOf(base)+"：目录里没有镜像")
			continue
		}
		dest := filepath.Join(in.Paths.Downloads(), "debian13-arm64-"+strings.ReplaceAll(dir, ":", "")+"-rootfs.tar.xz")
		if st, err := os.Stat(dest); err == nil && st.Size() > 10<<20 {
			return dest, nil
		}
		in.say("下载 Debian 13 根文件系统（%s，约 90 MB，来源 %s）", dir, hostOf(base))
		if err := simpleGet(ctx, in.client(), base+dir+"/rootfs.tar.xz", dest); err != nil {
			reasons = append(reasons, err.Error())
			continue
		}
		return dest, nil
	}
	return "", fmt.Errorf("Debian 根文件系统下不来：%s", strings.Join(reasons, "；"))
}
