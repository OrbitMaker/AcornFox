package macvm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/appinstall"
)

// ---------------------------------------------------------------- the machine

func (in *Installer) vm() *CtrVM {
	return &CtrVM{C: in.ctr(), Name: in.Cfg.VM, Lock: func() (func(), error) { return LockVM(in.Paths) }}
}

// RunInVM runs a script in the machine when it is running (it never boots a stopped one).
func (in *Installer) RunInVM(ctx context.Context, script string) (Result, error) {
	return in.vm().RunScript(ctx, script)
}

func (in *Installer) stepMachine(ctx context.Context, configExisted bool) (string, string, error) {
	c := in.ctr()
	m, ok, err := c.Find(ctx, in.Cfg.VM)
	if err != nil {
		return "", "", faultf("MAC-VM-01", "看不到虚拟机列表：%v", err)
	}
	if ok {
		if !configExisted && !in.Cfg.Created.VM {
			return "", "", faultf("MAC-VM-01", "已经有一台叫 %s 的虚拟机，但不是这个安装留下的。为免弄坏你的东西，这里不动它；可以换名字（环境变量 ACORNFOX_MAC_VM）或自己先处理掉", in.Cfg.VM)
		}
		if m.Running {
			return StateSkip, "虚拟机在运行（" + m.IP + "）", nil
		}
		if in.Opt.Plan {
			return StatePlan, "虚拟机已停着，会叫起来", nil
		}
		in.say("虚拟机停着，叫起来…")
		if err := in.vm().Start(ctx); err != nil {
			return "", "", faultf("MAC-VM-02", "%v", err)
		}
		return StateDone, "已叫起虚拟机", nil
	}
	if in.Opt.Plan {
		return StatePlan, fmt.Sprintf("会建一台叫 %s 的 Debian 虚拟机（%d 个 CPU、%d GB 内存、不挂 Mac 的家目录）", in.Cfg.VM, in.Cfg.CPUs, in.Cfg.MemoryGB), nil
	}
	in.say("建虚拟机 %s（%d 个 CPU、%d GB 内存，不挂 Mac 的家目录；第一次要解开镜像，约一两分钟）…", in.Cfg.VM, in.Cfg.CPUs, in.Cfg.MemoryGB)
	res, err := Capture(ctx, in.R, nil, c.bin(), "machine", "create", BaseImage, "--name", in.Cfg.VM,
		"--cpus", strconv.Itoa(in.Cfg.CPUs), "--memory", strconv.Itoa(in.Cfg.MemoryGB)+"G", "--home-mount", "none",
		"--user", "dev", "--uid", "1000", "--gid", "1000", "--progress", "none")
	if err != nil {
		return "", "", faultf("MAC-VM-01", "%v", err)
	}
	if res.Code != 0 {
		return "", "", faultf("MAC-VM-01", "container machine create 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
	}
	in.Cfg.Created.VM = true
	if err := in.vm().Start(ctx); err != nil {
		return "", "", faultf("MAC-VM-02", "%v", err)
	}
	return StateDone, "已建好并起来", nil
}

// ---------------------------------------------------------------- inside the machine

const provisionScript = `set -e
export DEBIAN_FRONTEND=noninteractive
# 开机不要多等两分钟（实测：systemd-networkd-wait-online 超时 120 秒，Docker 排在它后面）
systemctl mask systemd-networkd-wait-online.service >/dev/null 2>&1 || true
APT_FILES="/etc/apt/sources.list /etc/apt/sources.list.d/*.sources"
use_china_apt() {
    for f in $APT_FILES; do
        [ -f "$f" ] || continue
        [ -f "$f.afxbak" ] || cp -a "$f" "$f.afxbak"
        sed -i -E 's#https?://(deb|security|ftp)\.debian\.org#https://mirrors.aliyun.com#g' "$f"
    done
}
use_orig_apt() { for f in $APT_FILES; do [ -f "$f.afxbak" ] && cp -a "$f.afxbak" "$f"; done; return 0; }
[ "@CHINA@" = "1" ] && use_china_apt
need=""
command -v curl >/dev/null 2>&1 || need="$need curl"
[ -s /etc/ssl/certs/ca-certificates.crt ] || need="$need ca-certificates"
command -v ip >/dev/null 2>&1 || need="$need iproute2"
[ -x /usr/sbin/sshd ] || need="$need openssh-server"
try_apt() { apt-get update -qq && apt-get install -y -qq --no-install-recommends $need; }
if [ -n "$need" ]; then
    echo "先装几个基础软件:$need"
    if ! try_apt; then
        echo "软件源连不上，退回原来的软件源再试一次"
        use_orig_apt
        if ! try_apt; then
            use_china_apt
            sleep 3
            try_apt || { echo "装不上$need（软件源都连不上）"; exit 1; }
        fi
    fi
fi
systemctl enable --now ssh >/dev/null 2>&1 || systemctl enable --now sshd >/dev/null 2>&1 || true
# 时钟：虚拟机的时钟随 Mac 睡眠会走偏，下载网关要求两边相差不到 5 分钟；装不上也不影响安装
if ! systemctl is-active systemd-timesyncd >/dev/null 2>&1; then
    apt-get install -y -qq --no-install-recommends systemd-timesyncd >/dev/null 2>&1 || true
fi
timedatectl set-ntp true >/dev/null 2>&1 || true
install -d -m 700 /root/.ssh
KEY='@PUBKEY@'
grep -qxF "$KEY" /root/.ssh/authorized_keys 2>/dev/null || echo "$KEY" >> /root/.ssh/authorized_keys
chmod 600 /root/.ssh/authorized_keys
install -d /etc/ssh/sshd_config.d
printf 'PermitRootLogin prohibit-password\nPasswordAuthentication no\n' > /etc/ssh/sshd_config.d/10-acornfox.conf
systemctl reload ssh >/dev/null 2>&1 || systemctl reload sshd >/dev/null 2>&1 || true
[ -s /etc/ssh/ssh_host_ed25519_key.pub ] || ssh-keygen -A >/dev/null 2>&1
echo "AFX-HOSTKEY $(cut -d' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub)"
echo "AFX-PROVISIONED"
`

var hostKeyRE = regexp.MustCompile(`(?m)^AFX-HOSTKEY (ssh-ed25519 \S+)`)

// ensureSSHKey makes the key the Mac logs in to the machine with.
func (in *Installer) ensureSSHKey(ctx context.Context) (string, error) {
	key := in.Paths.SSHKey()
	if _, err := os.Stat(key + ".pub"); err != nil {
		if err := os.MkdirAll(in.Paths.SSHDir(), 0o700); err != nil {
			return "", err
		}
		res, err := Capture(ctx, in.R, nil, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "acornfox-mac", "-f", key)
		if err != nil || res.Code != 0 {
			return "", fmt.Errorf("ssh-keygen 失败：%v %s", err, firstLine(res.Stderr))
		}
	}
	b, err := os.ReadFile(key + ".pub")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (in *Installer) stepProvision(ctx context.Context) (string, string, error) {
	if in.Opt.Plan {
		return StatePlan, "会在虚拟机里屏蔽开机等待、装 curl 和 sshd、放进这台 Mac 的登录钥匙", nil
	}
	pub, err := in.ensureSSHKey(ctx)
	if err != nil {
		return "", "", faultf("MAC-PROV-01", "%v", err)
	}
	script := strings.NewReplacer("@CHINA@", ifs(in.china(), "1", "0"), "@PUBKEY@", pub).Replace(provisionScript)
	var out strings.Builder
	code, err := in.ctr().ScriptStream(ctx, in.Cfg.VM, script, nil, &out, &out)
	if err != nil {
		return "", "", faultf("MAC-PROV-01", "%v", err)
	}
	if code != 0 || !strings.Contains(out.String(), "AFX-PROVISIONED") {
		return "", "", faultf("MAC-PROV-01", "虚拟机里的准备没做完（退出码 %d）：%s", code, lastLines(out.String(), 3))
	}
	m := hostKeyRE.FindStringSubmatch(out.String())
	if m == nil {
		return "", "", faultf("MAC-PROV-01", "读不到虚拟机 sshd 的主机钥匙")
	}
	line := fmt.Sprintf("[127.0.0.1]:%d %s\n", in.Cfg.SSHPort, m[1])
	if err := os.MkdirAll(in.Paths.SSHDir(), 0o700); err != nil {
		return "", "", faultf("MAC-PROV-01", "%v", err)
	}
	if err := os.WriteFile(in.Paths.KnownHosts(), []byte(line), 0o600); err != nil {
		return "", "", faultf("MAC-PROV-01", "%v", err)
	}
	return StateDone, "", nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}

// scriptOut runs a short script in the machine and returns what it printed.
func (in *Installer) scriptOut(ctx context.Context, script string) (string, error) {
	res, err := in.vm().RunScript(ctx, script)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ---------------------------------------------------------------- the release files

type releaseFiles struct {
	Install, Upgrade, Binary, Edge string // local paths
}

var edgeNameRE = regexp.MustCompile(`acornfox-edge_[0-9][0-9.]*-[0-9]+_linux_arm64\.tar\.gz`)

func (in *Installer) releaseURLs(ver, file string) []string {
	if in.Opt.ReleaseBase != "" {
		return []string{strings.TrimRight(in.Opt.ReleaseBase, "/") + "/releases/download/" + ver + "/" + file}
	}
	gh := in.Opt.GithubRepo
	if gh == "" {
		gh = "OrbitMaker/AcornFox"
	}
	hub := "https://github.com/" + gh + "/releases/download/" + ver + "/" + file
	if in.Opt.GiteeRepo == "-" { // ACORNFOX_GITEE_REPO set to an empty value: no Gitee
		return []string{hub}
	}
	ge := in.Opt.GiteeRepo
	if ge == "" {
		ge = "VIP13390/AcornFox"
	}
	gitee := "https://gitee.com/" + ge + "/releases/download/" + ver + "/" + file
	if in.china() {
		return []string{gitee, hub}
	}
	return []string{hub, gitee}
}

// FetchRelease brings the files of one release that go into the machine, checked
// against the release's SHA256SUMS.
func (in *Installer) fetchRelease(ctx context.Context, wantUpgrade bool) (releaseFiles, error) {
	ver := in.releaseVersion()
	if p := in.Opt.LocalPackage; p != "" {
		rf := releaseFiles{Install: filepath.Join(p, "install.sh"), Upgrade: filepath.Join(p, "upgrade.sh"), Binary: filepath.Join(p, "acornfox_linux_arm64")}
		m, _ := filepath.Glob(filepath.Join(p, "acornfox-edge_*_linux_arm64.tar.gz"))
		if len(m) == 1 {
			rf.Edge = m[0]
		}
		for _, f := range []string{rf.Install, rf.Binary, rf.Edge} {
			if f == "" {
				return rf, faultf("MAC-DL-02", "%s 里缺 install.sh、acornfox_linux_arm64 或入口包", p)
			}
			if _, err := os.Stat(f); err != nil {
				return rf, faultf("MAC-DL-02", "%v", err)
			}
		}
		return rf, nil
	}
	if ver == "" || ver == "vdev" {
		return releaseFiles{}, faultf("MAC-SRC-01", "这是没有版本号的源码版，没法决定装哪个版本的程序；用 get.sh 安装，或加 --version vX.Y.Z、--local-package 目录")
	}
	dir := filepath.Join(in.Paths.Downloads(), "release-"+ver)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return releaseFiles{}, err
	}
	var sums string
	for _, u := range in.releaseURLs(ver, "SHA256SUMS") {
		t, err := getText(ctx, in.client(), u)
		if err == nil && strings.TrimSpace(t) != "" {
			sums = t
			break
		}
	}
	if sums == "" {
		return releaseFiles{}, faultf("MAC-DL-02", "取不到 %s 的 SHA256SUMS（Gitee、GitHub 都试过）", ver)
	}
	want := map[string]string{}
	for _, l := range strings.Split(sums, "\n") {
		f := strings.Fields(l)
		if len(f) == 2 {
			want[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	edge := edgeNameRE.FindString(sums)
	need := []string{"install.sh", "acornfox_linux_arm64", edge}
	if wantUpgrade {
		need = append(need, "upgrade.sh")
	}
	paths := map[string]string{}
	for _, name := range need {
		sum := want[name]
		if name == "" || sum == "" {
			return releaseFiles{}, faultf("MAC-DL-02", "%s 的 SHA256SUMS 里没有 %s 这一项，没法校验", ver, name)
		}
		dest := filepath.Join(dir, name)
		paths[name] = dest
		if fileHasSHA(dest, sum) {
			continue
		}
		in.say("下载 %s 的 %s …", ver, name)
		var lastErr error
		ok := false
		for _, u := range in.releaseURLs(ver, name) {
			err := appinstall.DownloadFile(ctx, in.client(), appinstall.FileSource{URL: u}, "", dest, sum, 0, nil)
			if err == nil {
				ok = true
				break
			}
			lastErr = err
			if ctx.Err() != nil {
				return releaseFiles{}, ctx.Err()
			}
			in.say("  这个来源没成（%s），换下一个", firstLine(err.Error()))
		}
		if !ok {
			return releaseFiles{}, faultf("MAC-DL-02", "下载 %s 失败：%v", name, lastErr)
		}
	}
	return releaseFiles{Install: paths["install.sh"], Upgrade: paths["upgrade.sh"], Binary: paths["acornfox_linux_arm64"], Edge: paths[edge]}, nil
}

// pushFile copies a file into the machine (standard input of one shell line:
// the line is "cat > file", the rest of the input is the file) and checks its
// checksum there.
func (in *Installer) pushFile(ctx context.Context, local, remote string) error {
	f, err := os.Open(local)
	if err != nil {
		return err
	}
	defer f.Close()
	sum, err := FileSHA256(local)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("mkdir -p '%s' && cat > '%s.part'\n", filepath.Dir(remote), remote)
	res, err := in.ctr().Script(ctx, in.Cfg.VM, line, f)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("传文件 %s 进虚拟机失败（退出码 %d）：%s", filepath.Base(local), res.Code, firstLine(res.Stderr))
	}
	got, err := in.scriptOut(ctx, fmt.Sprintf("sha256sum '%s.part' | cut -d' ' -f1 && mv '%s.part' '%s'", remote, remote, remote))
	if err != nil {
		return err
	}
	if got != sum {
		return fmt.Errorf("文件 %s 传进虚拟机后校验不对（%s ≠ %s）", filepath.Base(local), got, sum)
	}
	return nil
}

const linuxRunScript = `set -u
LOG=/root/acornfox-install.log
exec > >(tee -a "$LOG") 2>&1
echo "==== $(date '+%F %T') 从 Mac @VERB@ AcornFox（@VERSION@）===="
export DEBIAN_FRONTEND=noninteractive
cd /root/afx-pkg
chmod +x install.sh acornfox_linux_arm64 2>/dev/null
@RUN@
rc=$?
echo "==== @VERB@ 退出码 $rc ===="
exit $rc
`

func (in *Installer) stepLinux(ctx context.Context) (string, string, error) {
	upgrade := in.Opt.Action == "upgrade"
	active := ""
	// Only look inside a machine that is running: a script would boot a stopped one (not in a plan).
	if m, ok, _ := in.ctr().Find(ctx, in.Cfg.VM); ok && m.Running {
		active, _ = in.scriptOut(ctx, "systemctl is-active acornfox-server 2>/dev/null || true")
	}
	if active == "active" && !upgrade {
		v, _ := in.scriptOut(ctx, "/usr/local/bin/acornfox version 2>/dev/null | head -1")
		return StateSkip, "虚拟机里已装好 " + v, nil
	}
	if upgrade && active != "active" {
		upgrade = false // nothing to upgrade: install
	}
	if in.Opt.Plan {
		return StatePlan, ifs(upgrade, "会在虚拟机里升级 AcornFox", "会把 install.sh、程序和入口包放进虚拟机，在里面装 Docker 和 AcornFox（要几分钟）"), nil
	}
	rf, err := in.fetchRelease(ctx, upgrade)
	if err != nil {
		return "", "", err
	}
	in.say("把安装文件放进虚拟机…")
	remote := "/root/afx-pkg/"
	pushes := map[string]string{rf.Install: "install.sh", rf.Binary: "acornfox_linux_arm64", rf.Edge: filepath.Base(rf.Edge)}
	if upgrade {
		pushes[rf.Upgrade] = "upgrade.sh"
	}
	for local, name := range pushes {
		if err := in.pushFile(ctx, local, remote+name); err != nil {
			return "", "", faultf("MAC-LINUX-01", "%v", err)
		}
	}
	region := in.regionArg()
	host := in.Cfg.PublicHost
	if host == "" {
		host = "127.0.0.1"
	}
	var run, verb string
	if upgrade {
		verb = "升级"
		run = "bash ./upgrade.sh -y --binary ./acornfox_linux_arm64 --edge-package ./" + filepath.Base(rf.Edge) + region
	} else {
		verb = "安装"
		run = "bash ./install.sh --binary ./acornfox_linux_arm64 --edge-package ./" + filepath.Base(rf.Edge) + " --mac-vm --public-host " + host + region
	}
	script := strings.NewReplacer("@VERB@", verb, "@VERSION@", in.releaseVersion(), "@RUN@", run).Replace(linuxRunScript)
	in.say("在虚拟机里%s AcornFox（%s；输出同时记在虚拟机里的 /root/acornfox-install.log）…", verb, ifs(upgrade, "应用不停，约十几秒", "要装 Docker，几分钟"))
	t0 := time.Now()
	w := &lineTee{w: in.Out, prefix: "    | "}
	code, err := in.ctr().ScriptStream(ctx, in.Cfg.VM, script, nil, w, w)
	if err != nil {
		return "", "", faultf("MAC-LINUX-01", "%v", err)
	}
	if code != 0 {
		inner := ""
		if m := regexp.MustCompile(`故障编号:\s*([A-Z][A-Z0-9-]+)`).FindAllStringSubmatch(w.tail(), -1); len(m) > 0 {
			inner = m[len(m)-1][1]
		}
		return "", "", faultf("MAC-LINUX-01", "虚拟机里的%s脚本退出码 %d（用时 %d 秒）%s", verb, code, int(time.Since(t0).Seconds()), ifs(inner != "", "；脚本报 "+inner+"（acornfox faults "+inner+" 看处理办法）", ""))
	}
	in.Cfg.Version = in.releaseVersion()
	return StateDone, fmt.Sprintf("用时 %d 秒", int(time.Since(t0).Seconds())), nil
}

// lineTee prints what the machine says, line by line, with a prefix, and keeps the last lines.
type lineTee struct {
	w      io.Writer
	prefix string
	buf    strings.Builder
	keep   []string
}

// installNoise: the lines of the install script that are only the package manager talking
// (they are all in /root/acornfox-install.log in the machine); the script's own lines
// start with a tag such as [INFO], or are a fault number.
var installKeep = regexp.MustCompile(`^(\[(INFO|SUCCESS|WARN|ERROR)\]|故障编号|====|错误|出错|\s+(处理办法|原因))`)

func (l *lineTee) Write(p []byte) (int, error) {
	l.buf.Write(p)
	for {
		s := l.buf.String()
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(s[:i], "\r")
		l.buf.Reset()
		l.buf.WriteString(s[i+1:])
		l.keep = append(l.keep, line)
		if len(l.keep) > 80 {
			l.keep = l.keep[len(l.keep)-80:]
		}
		if l.w != nil && installKeep.MatchString(line) {
			fmt.Fprintf(l.w, "%s%s\n", l.prefix, line)
		}
	}
	return len(p), nil
}

func (l *lineTee) tail() string { return strings.Join(l.keep, "\n") }

// deliverKey puts the Mac's machine key (made when the first file came through the
// gateway) into the machine, so that one Mac counts as one machine to the gateway.
func (in *Installer) deliverKey(ctx context.Context) {
	key := filepath.Join(in.Paths.Root, "identity", "machine-key.pem")
	b, err := os.ReadFile(key)
	if err != nil || in.Opt.Plan {
		return
	}
	have, _ := in.scriptOut(ctx, "test -e /var/lib/acornfox/identity/machine-key.pem && echo yes || echo no")
	if have == "yes" {
		return
	}
	script := "set -e\ninstall -d -m 700 -o acornfox -g acornfox /var/lib/acornfox/identity\ncat > /var/lib/acornfox/identity/machine-key.pem <<'AFXKEYEOF'\n" + string(b) +
		"AFXKEYEOF\nchown acornfox:acornfox /var/lib/acornfox/identity/machine-key.pem\nchmod 600 /var/lib/acornfox/identity/machine-key.pem\n"
	if res, err := in.ctr().Script(ctx, in.Cfg.VM, script, nil); err != nil || res.Code != 0 {
		in.warn("没能把这台 Mac 的机器钥匙交给虚拟机里的 AcornFox（不影响使用，下载网关上会当成两台机器）")
	}
}

// ---------------------------------------------------------------- the Mac side

func copyExecutable(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func (in *Installer) stepDaemon(ctx context.Context, configExisted bool) (string, string, error) {
	label := in.Cfg.Label
	plist := PlistPath(label)
	if in.Opt.Plan {
		return StatePlan, fmt.Sprintf("会放一个常驻小程序（%s，登录后自动运行）：转发 Mac 上的端口到虚拟机，闲置 %d 分钟停虚拟机、有人访问再叫起", label, in.Cfg.IdleMinutes), nil
	}
	if err := copyExecutable(in.Exe, in.Paths.Program()); err != nil {
		return "", "", faultf("MAC-LAUNCH-01", "复制程序失败：%v", err)
	}
	if err := os.MkdirAll(in.Paths.Logs(), 0o700); err != nil {
		return "", "", faultf("MAC-LAUNCH-01", "%v", err)
	}
	if !configExisted {
		in.Cfg.Created.Plist = plist
	}
	if err := in.Cfg.Save(in.Paths); err != nil {
		return "", "", faultf("MAC-LAUNCH-01", "写不了 mac.json：%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return "", "", faultf("MAC-LAUNCH-01", "%v", err)
	}
	xml := PlistXML(label, in.Paths.Program(), in.Paths, daemonEnv())
	if err := os.WriteFile(plist, []byte(xml), 0o644); err != nil {
		return "", "", faultf("MAC-LAUNCH-01", "写不了 %s：%v", plist, err)
	}
	if err := LaunchLoad(ctx, in.R, label, plist); err != nil {
		return "", "", faultf("MAC-LAUNCH-01", "%v", err)
	}
	// Wait until the daemon listens on the ssh port (it is its first job).
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if portInUse(in.Cfg.SSHPort) {
			return StateDone, "已登记并启动 " + label, nil
		}
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return "", "", err
		}
	}
	return "", "", faultf("MAC-PORT-01", "常驻小程序起来了，但 Mac 上的 %d 端口一直没人监听；看 %s 里的原因（多半是端口被别的程序占了）", in.Cfg.SSHPort, in.Paths.DaemonLog())
}

// daemonEnv is the environment the daemon needs on top of its folder: the test overrides.
func daemonEnv() map[string]string {
	env := map[string]string{}
	for _, k := range []string{"ACORNFOX_MAC_VM", "ACORNFOX_CONFIG_DIR"} {
		if v := os.Getenv(k); v != "" {
			env[k] = v
		}
	}
	return env
}

// portInUse says whether something listens on 127.0.0.1:port (it tries to take
// the port itself; it never connects, so it never wakes the machine).
func portInUse(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

func (in *Installer) targetsDefault(ctx context.Context) (name string, hasLocal bool) {
	res, err := Capture(ctx, in.R, nil, in.Paths.Program(), "--json", "target", "list")
	if err != nil || res.Code != 0 {
		return "", false
	}
	var j struct {
		Default string `json:"default"`
		Targets []struct {
			Name string `json:"name"`
		} `json:"targets"`
	}
	if json.Unmarshal([]byte(res.Stdout), &j) != nil {
		return "", false
	}
	for _, t := range j.Targets {
		if t.Name == "local" {
			hasLocal = true
		}
	}
	return j.Default, hasLocal
}

func (in *Installer) stepTarget(ctx context.Context) (string, string, error) {
	if in.Opt.Plan {
		return StatePlan, "会在 acornfox 命令里登记一台叫 local 的服务器（经 ssh 连这台虚拟机），之后 acornfox apps 等命令就管这台 Mac 上的 AcornFox", nil
	}
	args := []string{"target", "add", "local", "--ssh", "root@127.0.0.1", "--port", strconv.Itoa(in.Cfg.SSHPort), "--identity", in.Paths.SSHKey(),
		"--ssh-option", "UserKnownHostsFile=" + in.Paths.KnownHosts(), "--ssh-option", "StrictHostKeyChecking=yes", "--ssh-option", "IdentitiesOnly=yes"}
	res, err := Capture(ctx, in.R, nil, in.Paths.Program(), args...)
	if err != nil {
		return "", "", faultf("MAC-SSH-01", "%v", err)
	}
	if res.Code != 0 {
		return "", "", faultf("MAC-SSH-01", "acornfox target add 退出码 %d：%s", res.Code, firstLine(res.Stderr+res.Stdout))
	}
	in.Cfg.Created.Target = "local"
	def, _ := in.targetsDefault(ctx)
	detail := "已登记 local"
	if def != "" && def != "local" {
		detail += fmt.Sprintf("（你原来的默认服务器是 %s，没改；要让 acornfox 命令默认管这台 Mac：acornfox target use local）", def)
	}
	return StateDone, detail, nil
}

func dirWritable(d string) bool {
	st, err := os.Stat(d)
	if err != nil || !st.IsDir() {
		return false
	}
	f, err := os.CreateTemp(d, ".afx-w-")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// cliDir picks where the acornfox command goes: the folder given, the one an
// acornfox on PATH is in (replaced in place), /usr/local/bin, ~/.local/bin.
func (in *Installer) cliDir() string {
	if in.Opt.InstallDir != "" {
		return in.Opt.InstallDir
	}
	if v := os.Getenv("ACORNFOX_INSTALL_DIR"); v != "" {
		return v
	}
	if p, err := exec.LookPath("acornfox"); err == nil {
		d := filepath.Dir(p)
		if d != in.Paths.Bin() && dirWritable(d) {
			return d
		}
	}
	if dirWritable("/usr/local/bin") {
		return "/usr/local/bin"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

func (in *Installer) stepCLI() (string, string, error) {
	dir := in.cliDir()
	dst := filepath.Join(dir, "acornfox")
	_, statErr := os.Stat(dst)
	if in.Opt.Plan {
		return StatePlan, "会把 acornfox 命令放到 " + dir, nil
	}
	if err := copyExecutable(in.Exe, dst); err != nil {
		return "", "", faultf("MAC-CLI-01", "放不了 %s：%v（可以用环境变量 ACORNFOX_INSTALL_DIR 指定别的目录）", dst, err)
	}
	if statErr != nil { // there was none before: this install made it
		in.Cfg.Created.CLI = dst
	}
	detail := dst
	onPath := false
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == dir {
			onPath = true
		}
	}
	if !onPath {
		detail += "（" + dir + " 不在 PATH 里，要在 ~/.zshrc 加一行：export PATH=\"" + dir + ":$PATH\"）"
	}
	return StateDone, detail, nil
}

func (in *Installer) stepOpen(ctx context.Context) (string, string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/", LANConsolePort)
	if in.Opt.NoOpen {
		return StateSkip, "按你的要求不打开浏览器", nil
	}
	if in.Opt.Plan {
		return StatePlan, "会用浏览器打开 " + url + "（第一次打开时设一个控制台密码）", nil
	}
	if res, err := Capture(ctx, in.R, nil, "open", url); err != nil || res.Code != 0 {
		return StateDone, "没能自动打开浏览器，请自己打开 " + url, nil
	}
	return StateDone, url, nil
}
