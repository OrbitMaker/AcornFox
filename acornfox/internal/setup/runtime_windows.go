//go:build windows

package setup

// The Windows built-in runtime: WSL 2 + a minimal Alpine Linux distribution
// ("AcornFox", stored under <data>\runtime\wsl) + Docker Engine from Alpine's
// packages. No Docker Desktop, no windows: every wsl.exe call runs hidden.
// Docker listens inside the distribution on 127.0.0.1:23750, which WSL
// forwards to this computer's 127.0.0.1 only.
//
// Order (each part is skipped when already there, so resuming is cheap):
//
//  1. WSL 2 itself. When `wsl --version` fails, download the WSL package, ask
//     the user to confirm the admin prompt (NeedAdmin), then run one elevated
//     PowerShell that enables VirtualMachinePlatform and
//     Microsoft-Windows-Subsystem-Linux and installs the package. A reboot
//     Windows asks for becomes NeedReboot; the server resumes after it.
//  2. The distribution: download the Alpine minirootfs, `wsl --import`.
//  3. Docker Engine: `apk add docker iptables ip6tables` from the mirror.
//  4. Start dockerd (afx-dockerd) and keep it running (supervisor).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/acornfox/acornfox/internal/platform"
)

const (
	builtinSupported = true
	wslDistro        = "AcornFox"
	wslDockerHost    = "tcp://127.0.0.1:23750"
	wslBuiltinName   = "AcornFox 自带（WSL 2）"
)

type osRuntime struct {
	mu        sync.Mutex
	supervise bool
}

func newOSRuntime() *osRuntime { return &osRuntime{} }

// hidden runs a command without a console window.
func hidden(ctx context.Context, name string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	c.Env = append(os.Environ(), "WSL_UTF8=1")
	return c
}

// wslOut runs wsl.exe and decodes its output (UTF-16LE on older WSL).
func wslOut(ctx context.Context, args ...string) (string, error) {
	b, err := hidden(ctx, "wsl.exe", args...).CombinedOutput()
	return decodeWSL(b), err
}

func decodeWSL(b []byte) string {
	if len(b) >= 2 && len(b)%2 == 0 && strings.Count(string(b), "\x00") >= len(b)/4 {
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return strings.TrimPrefix(string(utf16.Decode(u)), "\ufeff")
	}
	return string(b)
}

func desktopInstalled() (bool, string) {
	for _, dir := range []string{os.Getenv("ProgramFiles"), `C:\Program Files`} {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "Docker", "Docker", "Docker Desktop.exe")); err == nil {
			return true, ""
		}
	}
	return false, ""
}

func openDockerDesktop(ctx context.Context) error {
	for _, dir := range []string{os.Getenv("ProgramFiles"), `C:\Program Files`} {
		p := filepath.Join(dir, "Docker", "Docker", "Docker Desktop.exe")
		if _, err := os.Stat(p); err == nil {
			return exec.Command(p).Start()
		}
	}
	return errors.New("没找到 Docker Desktop")
}

func (d *desktopRuntime) runtimeDir() string { return filepath.Join(d.cfg.DataDir, "runtime") }
func (d *desktopRuntime) rebootMarker() string {
	return filepath.Join(d.runtimeDir(), "reboot-pending")
}

func wslReady(ctx context.Context) bool {
	out, err := wslOut(ctx, "--version")
	return err == nil && strings.Contains(strings.ToLower(out), "wsl")
}

func distroExists(ctx context.Context) bool {
	out, err := wslOut(ctx, "--list", "--quiet")
	if err != nil {
		return false
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.EqualFold(strings.TrimSpace(l), wslDistro) {
			return true
		}
	}
	return false
}

func (d *desktopRuntime) builtinRun(ctx context.Context, opts StartOptions, r Reporter) (Detection, error) {
	if err := os.MkdirAll(d.runtimeDir(), 0o700); err != nil {
		return Detection{}, err
	}
	dl := d.downloader()
	if opts.Mirror != "" {
		dl.Sources.Base = opts.Mirror
	}
	report := func(it Item) { r.Item(it) }

	// 1. WSL 2.
	r.Item(Item{ID: "wsl", Title: "WSL 2", State: StateRunning, Detail: "检查"})
	if !wslReady(ctx) {
		if _, err := os.Stat(d.rebootMarker()); err == nil {
			return Detection{}, &NeedError{Need: Need{Kind: NeedReboot,
				Message: "WSL 2 已经打开，需要重启电脑才生效。重启后 AcornFox 会自动接着装。"}}
		}
		msi, err := dl.Fetch(ctx, wslPackage(), report)
		if err != nil {
			return Detection{}, err
		}
		if !opts.ConfirmAdmin {
			return Detection{}, &NeedError{Need: Need{Kind: NeedAdmin,
				Message: "要先打开 Windows 自带的 WSL 2 功能，AcornFox 的运行环境要用它。",
				Steps: []string{
					"Windows 弹出一次管理员确认，选“是”。",
					"打开 WSL 2 功能，大约 1 分钟。",
					"同时加一条只针对 AcornFox 的防火墙规则，让局域网里的设备能打开你的应用（只在专用网络生效）。",
					"可能要重启电脑。重启后 AcornFox 会自动接着装，不用重新运行。",
					"装 AcornFox 自带的精简 Linux 和 Docker（约 100 MB，没有界面）。",
				}}}
		}
		r.Item(Item{ID: "wsl", Title: "WSL 2", State: StateRunning, Detail: "等你在管理员确认里选“是”"})
		reboot, err := d.enableWSL(ctx, msi, !opts.LocalOnly)
		if err != nil {
			return Detection{}, err
		}
		if reboot || !wslReady(ctx) {
			_ = os.WriteFile(d.rebootMarker(), []byte(time.Now().Format(time.RFC3339)), 0o600)
			return Detection{}, &NeedError{Need: Need{Kind: NeedReboot,
				Message: "WSL 2 已经打开，需要重启电脑才生效。保存好手上的工作后重启；重启后 AcornFox 会自动接着装。"}}
		}
	}
	_ = os.Remove(d.rebootMarker())
	r.Item(Item{ID: "wsl", Title: "WSL 2", State: StateDone, Detail: "已打开"})

	// 2. The distribution.
	if !distroExists(ctx) {
		rootfs, err := dl.Fetch(ctx, alpineRootfs(), report)
		if err != nil {
			return Detection{}, err
		}
		r.Item(Item{ID: "distro", Title: "精简 Linux（Alpine）", State: StateRunning, Detail: "导入"})
		dir := filepath.Join(d.runtimeDir(), "wsl")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Detection{}, err
		}
		out, err := wslOut(ctx, "--import", wslDistro, dir, rootfs, "--version", "2")
		if err != nil {
			return Detection{}, wslImportError(out, err)
		}
	}
	r.Item(Item{ID: "distro", Title: "精简 Linux（Alpine）", State: StateDone})

	// 3. Docker Engine inside it.
	if _, err := wslOut(ctx, "-d", wslDistro, "-u", "root", "--", "test", "-x", "/usr/local/bin/afx-dockerd"); err != nil {
		r.Item(Item{ID: "engine", Title: "Docker Engine", State: StateRunning, Detail: "从软件源安装（约 80 MB）"})
		china := d.cfg.Network().China
		c := hidden(ctx, "wsl.exe", "-d", wslDistro, "-u", "root", "--", "sh", "-s")
		c.Stdin = strings.NewReader(engineScript(china))
		b, err := c.CombinedOutput()
		d.cfg.Logf("setup wsl engine: %s", tail(decodeWSL(b), 2000))
		if err != nil {
			return Detection{}, Faultf("SETUP-NET-01", err, "在自带 Linux 里安装 Docker Engine 失败（%s）", tail(decodeWSL(b), 200))
		}
	}
	r.Item(Item{ID: "engine", Title: "Docker Engine", State: StateDone})

	// 4. Start and keep it running.
	r.Item(Item{ID: "start", Title: "启动运行环境", State: StateRunning})
	d.superviseDockerd()
	v, err := waitDocker(ctx, wslDockerHost, 90*time.Second)
	if err != nil {
		return Detection{}, Faultf("SETUP-RT-02", err, "自带运行环境 90 秒内没有启动好")
	}
	r.Item(Item{ID: "start", Title: "启动运行环境", State: StateDone, Detail: "Docker " + v})

	// 5. LAN access: the firewall rule for the relay in acornfox serve.
	if err := d.ensureFirewall(ctx, opts, r); err != nil {
		return Detection{}, err
	}
	return Detection{Reachable: true, Host: wslDockerHost, Kind: RuntimeBuiltin, Name: wslBuiltinName, Version: v}, nil
}

// elevate runs body (PowerShell that fills the hashtable $r with exit codes)
// once as administrator and returns $r. On a computer with UAC on, Windows
// shows its admin prompt; with UAC off it runs straight away.
func (d *desktopRuntime) elevate(ctx context.Context, body string) (map[string]int, error) {
	result := filepath.Join(d.runtimeDir(), "elevate-result.json")
	_ = os.Remove(result)
	ps1 := filepath.Join(d.runtimeDir(), "elevate.ps1")
	script := "$r = @{}\n" + body + "\n$r | ConvertTo-Json | Set-Content -Encoding ASCII '" + result + "'\n"
	if err := os.WriteFile(ps1, []byte(script), 0o600); err != nil {
		return nil, err
	}
	launch := fmt.Sprintf(`try { Start-Process powershell.exe -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','"%s"'; exit 0 } catch { exit 5 }`, ps1)
	if err := hidden(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", launch).Run(); err != nil {
		return nil, Faultf("SETUP-WIN-04", err, "管理员确认被取消或失败")
	}
	b, err := os.ReadFile(result)
	if err != nil {
		return nil, Faultf("SETUP-WIN-04", err, "管理员步骤没有完成")
	}
	var codes map[string]int
	_ = json.Unmarshal(b, &codes)
	d.cfg.Logf("setup elevate: %s", b)
	return codes, nil
}

// firewallPS adds the LAN firewall rule for the running acornfox.exe and
// records the result as $r.fw (0 = added).
func firewallPS() string {
	exe, _ := os.Executable()
	return "try { " + platform.FirewallAddPS(exe) + "; $r.fw = 0 } catch { $r.fw = 1 }"
}

// enableWSL runs the elevated part once (with the LAN firewall rule unless
// the user chose "this computer only") and reports whether Windows wants a
// reboot.
func (d *desktopRuntime) enableWSL(ctx context.Context, msi string, firewall bool) (bool, error) {
	body := fmt.Sprintf(`dism.exe /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart | Out-Null; $r.vmp = $LASTEXITCODE
dism.exe /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart | Out-Null; $r.wsl = $LASTEXITCODE
$p = Start-Process msiexec.exe -ArgumentList '/i', '"%s"', '/qn', '/norestart' -Wait -PassThru; $r.msi = $p.ExitCode`, msi)
	if firewall {
		body += "\n" + firewallPS()
	}
	codes, err := d.elevate(ctx, body)
	if err != nil {
		return false, err
	}
	reboot := false
	for k, c := range codes {
		switch {
		case c == 0:
		case k == "fw":
			d.cfg.Logf("setup: firewall rule not added (code %d); asked again later", c)
		case c == 3010 || c == 1641:
			reboot = true
		default:
			return false, Faultf("SETUP-WIN-04", nil, "打开 WSL 2 失败（%s 返回 %d）", k, c)
		}
	}
	return reboot, nil
}

// lanNeed asks once for the LAN firewall rule (when WSL was already there,
// so it could not ride along with enabling WSL).
var lanNeed = Need{
	Kind:    NeedAdmin,
	Message: "要让局域网里的手机、电脑打开你的应用，需要管理员确认一次：添加一条只针对 AcornFox 的防火墙规则。",
	Steps: []string{
		"Windows 弹出一次管理员确认，选“是”。",
		"只放行应用端口 18810–18859 和局域网控制台 18802，只在“专用网络”生效，公用网络（比如咖啡馆）不放行。",
		"卸载 AcornFox 时会一起删掉这条规则。",
	},
	Options: []Option{
		{ID: "confirm", Title: "允许局域网访问", Recommended: true, Detail: "局域网里的手机、电脑也能打开你的应用"},
		{ID: "local-only", Title: "只在本机用", Detail: "不加防火墙规则，应用只在这台电脑上能打开；以后可以运行 acornfox setup --confirm-admin 再打开"},
	},
}

// ensureFirewall makes sure the LAN firewall rule exists, unless the user
// chose "this computer only".
func (d *desktopRuntime) ensureFirewall(ctx context.Context, opts StartOptions, r Reporter) error {
	if opts.LocalOnly {
		r.Item(Item{ID: "firewall", Title: "局域网访问", State: StateDone, Detail: "选了只在本机用"})
		return nil
	}
	if platform.FirewallRulePresent() {
		r.Item(Item{ID: "firewall", Title: "局域网访问", State: StateDone, Detail: "防火墙规则已就绪"})
		return nil
	}
	if !opts.ConfirmAdmin {
		return &NeedError{Need: lanNeed}
	}
	r.Item(Item{ID: "firewall", Title: "局域网访问", State: StateRunning, Detail: "等你在管理员确认里选“是”"})
	codes, err := d.elevate(ctx, firewallPS())
	if err != nil {
		return err
	}
	if codes["fw"] != 0 || !platform.FirewallRulePresent() {
		return Faultf("SETUP-WIN-04", nil, "防火墙规则没有加上（返回 %d）", codes["fw"])
	}
	r.Item(Item{ID: "firewall", Title: "局域网访问", State: StateDone, Detail: "防火墙规则已加上"})
	return nil
}

func wslImportError(out string, err error) error {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "0x80370102"), strings.Contains(low, "virtualization"), strings.Contains(out, "虚拟化"):
		return Faultf("SETUP-WIN-03", err, "这台电脑不能运行 WSL 2 虚拟机（%s）", tail(out, 200))
	case strings.Contains(low, "0x8007019e"), strings.Contains(low, "restart"), strings.Contains(out, "重启"):
		return &NeedError{Need: Need{Kind: NeedReboot, Message: "需要重启电脑让 WSL 2 生效；重启后 AcornFox 会自动接着装。"}}
	}
	return Faultf("SETUP-RT-02", err, "导入自带 Linux 失败（%s）", tail(out, 200))
}

// engineScript installs Docker Engine in the Alpine distribution and writes
// afx-dockerd, the start script the supervisor runs.
func engineScript(china bool) string {
	repo := "https://dl-cdn.alpinelinux.org/alpine"
	daemon := `{}`
	if china {
		// Huawei measured fastest on home broadband (where Windows users are).
		repo = "https://mirrors.huaweicloud.com/alpine"
		daemon = `{"registry-mirrors":["https://docker.m.daocloud.io","https://docker.1panel.live"]}`
	}
	return `set -e
printf '%s/v3.21/main\n%s/v3.21/community\n' "` + repo + `" "` + repo + `" > /etc/apk/repositories
apk update
apk add --no-progress docker iptables ip6tables
mkdir -p /etc/docker
printf '%s\n' '` + daemon + `' > /etc/docker/daemon.json
printf '[interop]\nappendWindowsPath=false\n' > /etc/wsl.conf
cat > /usr/local/bin/afx-dockerd <<'EOF'
#!/bin/sh
# AcornFox 自带运行环境：启动 Docker Engine（由 acornfox serve 守护，只听 127.0.0.1）
mountpoint -q /sys/fs/cgroup || mount -t cgroup2 none /sys/fs/cgroup
rm -f /var/run/docker.pid
exec dockerd -H unix:///var/run/docker.sock -H tcp://127.0.0.1:23750 --tls=false
EOF
chmod +x /usr/local/bin/afx-dockerd
`
}

// superviseDockerd keeps afx-dockerd running while the server runs: every
// 10 seconds, when Docker does not answer, it starts it again (hidden).
func (d *desktopRuntime) superviseDockerd() {
	d.os.mu.Lock()
	defer d.os.mu.Unlock()
	if d.os.supervise {
		return
	}
	d.os.supervise = true
	logPath := filepath.Join(d.cfg.DataDir, "logs", "dockerd.log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
	go func() {
		for {
			if _, _, _, err := pingDocker(context.Background(), wslDockerHost); err != nil {
				f, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				c := hidden(context.Background(), "wsl.exe", "-d", wslDistro, "-u", "root", "--", "/usr/local/bin/afx-dockerd")
				if f != nil {
					c.Stdout, c.Stderr = f, f
				}
				if err := c.Start(); err == nil {
					d.cfg.Logf("setup: started dockerd in WSL (pid %d)", c.Process.Pid)
					go func() {
						_ = c.Wait()
						if f != nil {
							f.Close()
						}
					}()
				} else if f != nil {
					f.Close()
				}
			}
			time.Sleep(10 * time.Second)
		}
	}()
}
