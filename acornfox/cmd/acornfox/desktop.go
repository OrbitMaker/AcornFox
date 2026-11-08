package main

// macOS and Windows lifecycle commands (three-platform architecture):
//
//	acornfox install                 set up or upgrade this computer's acornfox
//	acornfox uninstall [--purge]     remove it (keeps data unless --purge)
//	acornfox doctor                  check Docker, the service and LAN access
//	                                 (Linux: Docker, disk space and ports)
//
// install copies the running binary to the data directory's bin/, keeps the
// previous copy and database as *.prev, registers the
// autostart entry (LaunchAgent / logon task) running "acornfox serve
// --background", waits for the API and adds the "local" target. It does not
// need Docker: preparing the runtime (Docker, the gateway, the self checks)
// is "acornfox setup", which install runs at the end (and the console shows
// the same steps). Linux uses install.sh / upgrade.sh instead.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	dockerclient "github.com/moby/moby/client"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/setup"
)

const (
	localTargetName = "local"
	installWait     = 90 * time.Second
)

var errLinuxInstall = "Linux 服务器请用 install.sh 安装、upgrade.sh 升级（见使用文档）"

// dockerHelp explains how to get Docker running for the detected runtime.
func dockerHelp(ep platform.DockerEndpoint, err error) string {
	switch ep.Runtime {
	case "Colima":
		return "Colima 没有启动。运行 colima start，等它完成后再试。"
	case "OrbStack":
		return "OrbStack 没有运行。打开 OrbStack，等它启动后再试。"
	case "Docker Desktop":
		if platform.GOOS == "windows" {
			return "Docker Desktop 没有运行。从开始菜单打开 Docker Desktop，等左下角显示 Engine running 后再试。"
		}
		return "Docker Desktop 没有运行。打开“应用程序”里的 Docker，等菜单栏图标显示 running 后再试。"
	}
	if platform.GOOS == "windows" {
		return "没找到 Docker。请先安装 Docker Desktop（https://www.docker.com/products/docker-desktop/ ，需要 WSL 2），装好并打开后再试。"
	}
	if platform.GOOS == "darwin" {
		return "没找到 Docker。推荐安装 Docker Desktop（https://www.docker.com/products/docker-desktop/ ）；已经在用 OrbStack 或 Colima 的话先把它打开。装好并启动后再试。"
	}
	return "连不上 Docker：" + err.Error()
}

func dockerConnect(ctx context.Context) (platform.DockerEndpoint, *dockerclient.Client, dockerclient.SystemInfoResult, error) {
	ep := platform.ResolveDocker()
	dc, err := dockerclient.New(dockerclient.FromEnv, dockerclient.WithHost(ep.Host))
	if err != nil {
		return ep, nil, dockerclient.SystemInfoResult{}, err
	}
	pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	info, err := dc.Info(pctx, dockerclient.InfoOptions{})
	if err != nil {
		_ = dc.Close()
		return ep, nil, info, err
	}
	ep.Runtime = platform.RuntimeFromInfo(ep.Runtime, info.Info.OperatingSystem, info.Info.Name)
	return ep, dc, info, nil
}

// waitAPI polls the trusted socket until GET /v1/status answers.
func waitAPI(ctx context.Context, socket string, d time.Duration) (client.Status, error) {
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		c, err := client.Connect(ctx, client.Target{URL: unixURL(socket)})
		if err == nil {
			st, serr := c.Status(ctx)
			_ = c.Close()
			if serr == nil {
				return st, nil
			}
			last = serr
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return client.Status{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if last == nil {
		last = errors.New("超时")
	}
	return client.Status{}, last
}

func unixURL(socket string) string {
	return "unix://" + filepath.ToSlash(func() string {
		if platform.GOOS == "windows" {
			return "/" + socket
		}
		return socket
	}())
}

// copyFile copies src to dst (mode 0755 for binaries), via a temp file.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
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

func sameFile(a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}

// stopService stops the running serve process and waits until it released
// the database (so the binary and database can be replaced).
func stopService(dataDir string) {
	switch platform.GOOS {
	case "darwin":
		_, _ = platform.RunCommand("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+platform.LaunchAgentLabel)
	case "windows":
		_, _ = platform.RunCommand("schtasks", "/End", "/TN", platform.WindowsTaskName)
	}
	// The API socket stops answering once the process is gone.
	sock := filepath.Join(dataDir, "api.sock")
	for i := 0; i < 20; i++ {
		c, err := net.DialTimeout("unix", sock, 300*time.Millisecond)
		if err != nil {
			return
		}
		_ = c.Close()
		time.Sleep(500 * time.Millisecond)
	}
}

func runInstall(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rollback := fs.Bool("rollback", false, "（不支持：AcornFox 只能升级）")
	china := fs.Bool("china", false, "中国大陆网络：镜像直接从国内源拉")
	global := fs.Bool("global", false, "海外网络：镜像从 Docker Hub 拉")
	noSetup := fs.Bool("no-setup", false, "只装主程序，不接着准备运行环境（之后在控制台或用 acornfox setup 准备）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *china && *global {
		fmt.Fprintln(stderr, "--china 和 --global 只能选一个")
		return 2
	}
	if !platform.DesktopMode() {
		fmt.Fprintln(stderr, errLinuxInstall)
		return 2
	}
	ctx := context.Background()
	start := time.Now()
	paths := platform.DefaultPaths()

	// 1. Network: where images and runtime files come from (users in
	// mainland China use domestic sources).
	if err := os.MkdirAll(paths.DataDir, 0o700); err != nil {
		fmt.Fprintln(stderr, "创建目录失败：", err)
		return 1
	}
	netcfg := decideNetwork(ctx, paths.DataDir, *china, *global)
	prevNet := platform.LoadNetwork(paths.DataDir)
	netcfg.DownloadBase, netcfg.Gateway = prevNet.DownloadBase, prevNet.Gateway
	if err := platform.SaveNetwork(paths.DataDir, netcfg); err != nil {
		fmt.Fprintln(stderr, "保存网络设置失败：", err)
		return 1
	}
	fmt.Fprintf(stdout, "网络：%s（%s）\n", map[bool]string{true: "中国大陆，从国内源下载", false: "海外，从官方源下载"}[netcfg.China], netcfg.Source)

	// 2. Put the binary in place (and keep the previous one).
	if err := os.MkdirAll(paths.BinDir, 0o700); err != nil {
		fmt.Fprintln(stderr, "创建目录失败：", err)
		return 1
	}
	bin := filepath.Join(paths.BinDir, platform.BinaryName())
	prev := bin + ".prev"
	db := filepath.Join(paths.DataDir, "acornfox.db")
	if *rollback {
		fmt.Fprintln(stderr, "UPGRADE-ROLLBACK-01：AcornFox 只能升级，不支持回到旧版本。升级没成功时会自动恢复到升级前；应用自己的版本可以用 acornfox rollback 应用名 回退。")
		return 2
	} else {
		self, err := os.Executable()
		if err != nil {
			fmt.Fprintln(stderr, "找不到当前程序：", err)
			return 1
		}
		if !sameFile(self, bin) {
			_, existed := os.Stat(bin)
			stopService(paths.DataDir)
			if existed == nil {
				if err := keepPrevious(bin, prev, db); err != nil {
					fmt.Fprintln(stderr, "备份旧版本失败：", err)
					return 1
				}
			}
			if err := copyFile(self, bin, 0o755); err != nil {
				fmt.Fprintln(stderr, "复制程序失败：", err)
				return 1
			}
		}
	}

	// 3. Autostart entry; this also (re)starts the process.
	if err := platform.InstallAutostart(platform.AutostartEntry{
		Binary:  bin,
		Args:    []string{"serve", "--background"},
		LogFile: paths.LogFile,
	}); err != nil {
		fmt.Fprintln(stderr, "设置开机自启失败：", err)
		return 1
	}

	// 4. Wait for the API.
	st, err := waitAPI(ctx, paths.APISocket, installWait)
	if err != nil {
		fmt.Fprintln(stderr, "服务没有按时启动：", err)
		fmt.Fprintln(stderr, "日志："+paths.LogFile)
		return 1
	}

	// 5. The "local" target for the CLI and AI workbenches; on Windows also
	// put the installed copy on the user's PATH.
	ensureLocalTarget(paths.APISocket, stdout, stderr)
	if changed, err := platform.EnsureUserPath(paths.BinDir); err != nil {
		fmt.Fprintln(stderr, "把程序目录加入 PATH 失败（可以手动加）：", err)
	} else if changed {
		fmt.Fprintln(stdout, "已把 "+paths.BinDir+" 加入用户 PATH（新开的终端里可直接用 acornfox）")
	}

	_ = st
	fmt.Fprintf(stdout, "主程序已安装并在后台运行（用时 %d 秒）\n", int(time.Since(start).Seconds()))
	fmt.Fprintln(stdout, "数据目录："+paths.DataDir)
	if *noSetup {
		fmt.Fprintln(stdout, "下一步：准备运行环境。运行 acornfox setup，或运行 acornfox open 在控制台里点")
		return 0
	}

	// 6. Prepare the runtime, following the same steps the console shows.
	fmt.Fprintln(stdout, "")
	return run([]string{"setup"}, os.Stdin, stdout, stderr)
}

// decideNetwork picks China or global: a flag wins, then an earlier choice
// (upgrades keep it), then detection, and China when detection fails.
func decideNetwork(ctx context.Context, dataDir string, china, global bool) platform.Network {
	switch {
	case china:
		return platform.Network{China: true, Source: "按 --china 指定"}
	case global:
		return platform.Network{China: false, Source: "按 --global 指定"}
	}
	if _, err := os.Stat(platform.NetworkFile(dataDir)); err == nil {
		n := platform.LoadNetwork(dataDir)
		n.Source = "沿用上次的设置"
		return n
	}
	if c, err := platform.DetectChina(ctx); err == nil {
		return platform.Network{China: c, Source: "自动判断"}
	}
	return platform.Network{China: true, Source: "没判断出来，按国内处理；海外用户请加 --global 重新运行"}
}

// keepPrevious saves the installed binary and database as *.prev before an
// upgrade.
func keepPrevious(bin, prev, db string) error {
	if err := copyFile(bin, prev, 0o755); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		src := db + suffix
		if _, err := os.Stat(src); err != nil {
			_ = os.Remove(db + ".prev" + suffix)
			continue
		}
		if err := copyFile(src, db+".prev"+suffix, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// ensureLocalTarget adds (or re-points) the "local" target through the CLI's
// own target command, so the file format stays in one place.
func ensureLocalTarget(socket string, stdout, stderr io.Writer) {
	var out strings.Builder
	code := run([]string{"target", "add", localTargetName, "--url", unixURL(socket)}, strings.NewReader(""), &out, &out)
	if code != 0 {
		// Already present: remove and add again so the URL is current.
		_ = run([]string{"target", "remove", localTargetName}, strings.NewReader(""), io.Discard, io.Discard)
		out.Reset()
		code = run([]string{"target", "add", localTargetName, "--url", unixURL(socket)}, strings.NewReader(""), &out, &out)
	}
	if code != 0 {
		fmt.Fprintln(stderr, "添加 local 服务器失败：", strings.TrimSpace(out.String()))
		return
	}
	_ = run([]string{"target", "use", localTargetName}, strings.NewReader(""), io.Discard, io.Discard)
	fmt.Fprintln(stdout, "已添加服务器 local（本机），并设为默认")
}

func runUninstall(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	purge := fs.Bool("purge", false, "同时删除所有应用数据：数据卷、数据目录里 AcornFox 的文件、AcornFox 构建的镜像（不可恢复；公共镜像如 postgres 保留）")
	yes := fs.Bool("yes", false, "不再询问确认")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !platform.DesktopMode() {
		fmt.Fprintln(stderr, "Linux 服务器请用 install.sh 的卸载说明（见使用文档）")
		return 2
	}
	paths := platform.DefaultPaths()
	if *purge && !*yes {
		fmt.Fprintf(stdout, "将删除所有应用、数据库数据和 %s，不可恢复。输入 yes 确认：", paths.DataDir)
		line, _ := bufio.NewReader(stdin).ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			fmt.Fprintln(stdout, "已取消")
			return 1
		}
	}
	ctx := context.Background()

	// 1. Stop and remove the autostart entry.
	if err := platform.RemoveAutostart(); err != nil {
		fmt.Fprintln(stderr, "移除开机自启失败：", err)
	} else {
		fmt.Fprintln(stdout, "已停止并移除开机自启")
	}
	stopService(paths.DataDir)

	// 2. Containers, networks (and with --purge volumes and images).
	ep, dc, _, err := dockerConnect(ctx)
	if err != nil {
		fmt.Fprintln(stderr, "连不上 Docker，容器没有清理："+dockerHelp(ep, err))
	} else {
		removed := cleanupDocker(ctx, dc, *purge, stdout)
		_ = dc.Close()
		fmt.Fprintf(stdout, "已清理 Docker：%s\n", removed)
	}

	// 3. Windows: the LAN firewall rule (removing it needs an administrator)
	// and, with --purge, the built-in runtime's WSL distribution.
	if platform.GOOS == "windows" {
		if platform.FirewallRulePresent() {
			launch := "try { Start-Process powershell.exe -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '-NoProfile','-Command','" +
				strings.ReplaceAll(platform.FirewallRemovePS(), "'", "''") + "'; exit 0 } catch { exit 5 }"
			_, _ = platform.RunHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", launch)
			if platform.FirewallRulePresent() {
				fmt.Fprintln(stderr, "防火墙规则 "+platform.FirewallRuleName+" 没能删掉（管理员确认被取消？）；可以用管理员 PowerShell 运行："+platform.FirewallRemovePS())
			} else {
				fmt.Fprintln(stdout, "已删除防火墙规则 "+platform.FirewallRuleName)
			}
		}
		if *purge {
			if _, err := platform.RunHidden("wsl.exe", "--unregister", "AcornFox"); err == nil {
				fmt.Fprintln(stdout, "已删除自带运行环境（WSL 发行版 AcornFox）")
			}
		}
	}

	// 4. Files.
	_ = run([]string{"target", "remove", localTargetName}, strings.NewReader(""), io.Discard, io.Discard)
	if *purge {
		if left := purgeDataDir(paths.DataDir); len(left) > 0 {
			// Windows cannot delete the acornfox.exe that is running this
			// command; everything else is gone.
			fmt.Fprintln(stderr, "有些文件没能删掉（正在运行的程序文件删不掉，关掉终端后手动删除即可）：", strings.Join(left, "、"))
		} else {
			fmt.Fprintln(stdout, "已删除 "+paths.DataDir+" 里 AcornFox 的数据")
		}
	} else {
		_ = os.RemoveAll(paths.BinDir)
	}
	_ = platform.RemoveUserPath(paths.BinDir)
	if !*purge {
		fmt.Fprintln(stdout, "已保留数据（"+paths.DataDir+" 和数据卷），重新安装后应用会恢复；要全部删除请加 --purge")
	}
	fmt.Fprintln(stdout, "卸载完成。命令行程序本身（acornfox）可以直接删除。")
	return 0
}

// serverFiles are the entries "acornfox serve" keeps in the data directory.
// On macOS the data directory (…/AcornFox) and the CLI's config directory
// (…/acornfox: targets.json, public console passwords) are the same folder
// on a case-insensitive disk, so purge removes only these and leaves the
// CLI's files.
var serverFiles = []string{"acornfox.db", "acornfox.db-wal", "acornfox.db-shm", "acornfox.db.lock",
	"acornfox.db.prev", "acornfox.db.prev-wal", "acornfox.db.prev-shm",
	"uploads", "logs", "bin", "api.sock", edgeTokenFile, "lan-console.json", "public-console.json"}

// purgeDataDir removes the server's files from dir (and dir itself when it is
// then empty); it returns what could not be removed.
func purgeDataDir(dir string) []string {
	var left []string
	for _, name := range serverFiles {
		p := filepath.Join(dir, name)
		if err := os.RemoveAll(p); err != nil {
			left = append(left, p)
		}
	}
	_ = os.Remove(dir) // only succeeds when empty
	return left
}

// cleanupDocker removes everything AcornFox created in Docker and returns a
// one-line summary. Volumes and images only with purge.
func cleanupDocker(ctx context.Context, dc *dockerclient.Client, purge bool, stdout io.Writer) string {
	nc, nn, nv, ni := 0, 0, 0, 0
	// Containers: apps and add-ons (managed label) and the edge.
	for _, label := range []string{runner.LabelManaged + "=1", runner.LabelEdge + "=1"} {
		res, err := dc.ContainerList(ctx, dockerclient.ContainerListOptions{All: true, Filters: dockerclient.Filters{}.Add("label", label)})
		if err != nil {
			continue
		}
		for _, c := range res.Items {
			if _, err := dc.ContainerRemove(ctx, c.ID, dockerclient.ContainerRemoveOptions{Force: true}); err == nil || cerrdefs.IsNotFound(err) {
				nc++
			}
		}
	}
	for _, label := range []string{runner.LabelManaged + "=1", runner.LabelEdge + "=1"} {
		res, err := dc.NetworkList(ctx, dockerclient.NetworkListOptions{Filters: dockerclient.Filters{}.Add("label", label)})
		if err != nil {
			continue
		}
		for _, n := range res.Items {
			if _, err := dc.NetworkRemove(ctx, n.ID, dockerclient.NetworkRemoveOptions{}); err == nil {
				nn++
			}
		}
	}
	if purge {
		res, err := dc.VolumeList(ctx, dockerclient.VolumeListOptions{})
		if err == nil {
			for _, v := range res.Items {
				if v.Labels[runner.LabelManaged] == "1" || strings.HasPrefix(v.Name, "af-") || v.Name == runner.EdgeDataVolume || v.Name == runner.EdgeConfigVolume {
					if _, err := dc.VolumeRemove(ctx, v.Name, dockerclient.VolumeRemoveOptions{Force: true}); err == nil {
						nv++
					}
				}
			}
		}
		// Only AcornFox's own images (acornfox/<app>:<id> and the local Caddy
		// image). Public images such as postgres:16-alpine stay: other
		// projects on the same Docker may use them.
		imgs, err := dc.ImageList(ctx, dockerclient.ImageListOptions{All: true})
		if err == nil {
			// Remove by name, not by ID: an app image pulled from a registry
			// is the same image as its source tag (acornfox/web:<id> and
			// nginx:1.27-alpine), and removing by ID is refused while the
			// other name exists. Removing our name leaves the source tag.
			for _, im := range imgs.Items {
				for _, t := range im.RepoTags {
					if !strings.HasPrefix(t, "acornfox/") {
						continue
					}
					if _, err := dc.ImageRemove(ctx, t, dockerclient.ImageRemoveOptions{PruneChildren: true}); err == nil {
						ni++
					}
				}
			}
		}
	}
	return fmt.Sprintf("容器 %d 个，网络 %d 个，数据卷 %d 个，镜像 %d 个", nc, nn, nv, ni)
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "输出 JSON：{\"ok\":bool,\"items\":[{\"ok\",\"level\",\"text\",\"code\",\"check\"}],\"setup\":{...}}；check 是这项检查的编号，通过的项也有（例如时钟一项是 SETUP-CLOCK-01），code 只出现在要处理的项上")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	r := doctorRun(context.Background(), false)
	if *asJSON {
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

// doctorItem is one line of doctor's report. Code is a fault code from
// internal/setup (Faults) when the item needs fixing. Check names the check
// by its fault code on every item that has one, passed items included, so a
// caller can find e.g. the clock line by SETUP-CLOCK-01 whatever its result.
type doctorItem struct {
	OK    bool   `json:"ok"`
	Level string `json:"level"` // ok | fail | warn
	Text  string `json:"text"`
	Code  string `json:"code,omitempty"`
	Check string `json:"check,omitempty"`
}

type doctorReport struct {
	OK    bool          `json:"ok"`
	Items []doctorItem  `json:"items"`
	Setup *setup.Status `json:"setup,omitempty"` // the server's runtime setup, when the service answers
}

// add records a passed or failed item. code is the check's fault code; on a
// passed item it is kept only in Check.
func (r *doctorReport) add(ok bool, code, format string, a ...any) {
	lvl, check := "ok", code
	if !ok {
		lvl, r.OK = "fail", false
	} else {
		code = ""
	}
	r.Items = append(r.Items, doctorItem{OK: ok, Level: lvl, Text: fmt.Sprintf(format, a...), Code: code, Check: check})
}

func (r *doctorReport) warn(code, format string, a ...any) {
	r.Items = append(r.Items, doctorItem{OK: true, Level: "warn", Text: fmt.Sprintf(format, a...), Code: code, Check: code})
}

// Add and Warn let the shared checks in internal/hostcheck write into the
// report (hostcheck.Reporter).
func (r *doctorReport) Add(ok bool, code, format string, a ...any) { r.add(ok, code, format, a...) }
func (r *doctorReport) Warn(code, format string, a ...any)         { r.warn(code, format, a...) }

func (r *doctorReport) print(w io.Writer) {
	for _, it := range r.Items {
		mark := map[string]string{"ok": "✓", "fail": "✗", "warn": "!"}[it.Level]
		if it.Code != "" {
			fmt.Fprintf(w, "%s %s（%s）\n", mark, it.Text, it.Code)
		} else {
			fmt.Fprintf(w, "%s %s\n", mark, it.Text)
		}
	}
	if r.Setup != nil && r.Setup.Error != nil {
		fmt.Fprintf(w, "  准备运行环境停在“%s”：%s\n  怎么办：%s\n", r.Setup.Error.Step, r.Setup.Error.Message, r.Setup.Error.Next)
	}
}

// runDoctorChecks prints the report and returns false when something needs
// fixing (used after install).
func runDoctorChecks(ctx context.Context, w io.Writer, afterInstall bool) bool {
	r := doctorRun(ctx, afterInstall)
	r.print(w)
	return r.OK
}

// doctorRun checks this computer's acornfox. afterInstall skips checks
// install has just printed.
func doctorRun(ctx context.Context, afterInstall bool) doctorReport {
	r := doctorReport{OK: true}
	paths := platform.DefaultPaths()

	// The server's own view of the runtime setup comes first: it names the
	// step and the fault code when the setup stopped.
	if platform.DesktopMode() && !afterInstall {
		if c, err := client.Connect(ctx, client.Target{URL: unixURL(paths.APISocket)}); err == nil {
			if sa, ok := any(c).(client.SetupAPI); ok {
				if st, err := sa.SetupStatus(ctx); err == nil {
					r.Setup = &st
					switch st.State {
					case setup.StateDone:
						r.add(true, "", "运行环境：已就绪（%s）", strings.TrimSpace(st.RuntimeDetail))
					case setup.StateFailed:
						code := ""
						if st.Error != nil {
							code = st.Error.Code
						}
						r.add(false, code, "运行环境：准备失败")
					case setup.StateNeedsUser:
						r.add(false, "SETUP-RT-01", "运行环境：等你操作（在控制台或运行 acornfox setup 查看）")
					default:
						r.warn("", "运行环境：%s（运行 acornfox setup 查看进度）", st.State)
					}
				}
			}
			_ = c.Close()
		}
	}

	ep, dc, info, err := dockerConnect(ctx)
	if err != nil {
		// On Linux, ask acornfox-server first: a user in acornfox-users needs
		// no Docker access of their own when the server's runner has it.
		var svc serviceProbe
		if platform.GOOS == "linux" {
			svc = probeService(ctx)
		}
		if platform.GOOS != "linux" || !dockerViaService(&r, err, svc) {
			code := "SETUP-RT-03"
			if ok, _ := dockerDesktopInstalled(); ok && ep.Runtime == "Docker Desktop" {
				code = "SETUP-RT-01"
			}
			r.add(false, code, "Docker：%s", dockerHelp(ep, err))
		}
		if platform.GOOS == "linux" {
			linuxHostChecks(ctx, &r, nil, "", svc)
		}
		return r
	}
	defer dc.Close()
	if !afterInstall {
		r.add(true, "SETUP-RT-03", "Docker：%s %s（%s，来源 %s）", ep.Runtime, info.Info.ServerVersion, ep.Host, ep.Source)
	}
	arch := strings.ToLower(info.Info.Architecture)
	if arch == "aarch64" || arch == "arm64" {
		r.warn("", "苹果芯片 / ARM：只有 amd64 镜像的应用也能装，靠转译运行，可能较慢")
	}
	if !platform.DesktopMode() {
		if platform.GOOS == "linux" {
			linuxHostChecks(ctx, &r, dc, info.Info.DockerRootDir, probeService(ctx))
		}
		return r
	}
	if !afterInstall {
		r.add(platform.AutostartInstalled(), "", "开机自启：%s", map[bool]string{true: "已设置", false: "没有设置（运行 acornfox install）"}[platform.AutostartInstalled()])
		if st, err := waitAPI(ctx, paths.APISocket, 2*time.Second); err == nil {
			r.add(true, "", "后台服务：运行中（%s 个应用，执行器 %s）", strconv.Itoa(st.Apps), st.Runner)
		} else {
			r.add(false, "", "后台服务：没有响应（日志 %s）", paths.LogFile)
		}
	}
	d := runner.NewDocker(dc)
	if d.EdgeImagePresent(ctx) {
		if res, err := dc.ContainerInspect(ctx, runner.EdgeContainerName, dockerclient.ContainerInspectOptions{}); err == nil && res.Container.State != nil && res.Container.State.Running {
			r.add(true, "SETUP-EDGE-01", "网关组件（Caddy）：镜像已就绪，容器运行中")
		} else {
			r.add(false, "SETUP-EDGE-01", "网关组件（Caddy）：镜像已就绪，但容器没有运行（后台服务启动后会自动创建；查看 docker logs %s）", runner.EdgeContainerName)
		}
	} else {
		netcfg := platform.LoadNetwork(paths.DataDir)
		r.add(false, "SETUP-EDGE-01", "网关组件（Caddy）：镜像还没下载好（%s）；运行 acornfox setup --resume 会重新下载", map[bool]string{true: "从国内源拉", false: "从 Docker Hub 拉"}[netcfg.China])
	}
	lan := platform.LANAddress()
	if lan == "" {
		r.warn("SETUP-CHK-04", "没取到局域网地址（没联网？），应用只能用 localhost 打开")
	} else {
		r.add(true, "SETUP-CHK-04", "局域网地址：%s（应用的局域网网址形如 http://%s:18810）", lan, lan)
	}
	// LAN reachability: a published 0.0.0.0 port of the edge must answer on
	// the LAN address too. Colima (and some setups) forward only 127.0.0.1.
	if lan != "" {
		if port := edgeLANPort(ctx, dc); port > 0 {
			local := dialOK("127.0.0.1", port)
			remote := dialOK(lan, port)
			switch {
			case local && remote:
				r.add(true, "SETUP-CHK-04", "局域网访问：%s:%d 能连通", lan, port)
			case local && !remote:
				msg := "局域网访问：端口 %d 只在 127.0.0.1 能连，局域网地址 %s 连不上，别的设备打不开应用"
				if ep.Runtime == "Colima" {
					msg += "。Colima 默认只把端口转发到 127.0.0.1，处理方法见使用手册“Colima”一节"
				} else if platform.GOOS == "windows" {
					msg += "。检查 Windows 防火墙是否放行了 Docker 或 WSL"
				}
				r.add(false, "SETUP-CHK-04", msg, port, lan)
			default:
				r.warn("", "局域网访问：端口 %d 暂时连不上（应用可能还在启动）", port)
			}
		}
	}
	return r
}

// dockerDesktopInstalled reports whether Docker Desktop is installed here.
func dockerDesktopInstalled() (bool, string) {
	switch platform.GOOS {
	case "darwin":
		_, err := os.Stat("/Applications/Docker.app")
		return err == nil, ""
	case "windows":
		_, err := os.Stat(filepath.Join(os.Getenv("ProgramFiles"), "Docker", "Docker", "Docker Desktop.exe"))
		return err == nil, ""
	}
	return false, ""
}

// edgeLANPort returns one port the edge container publishes on 0.0.0.0, or 0.
func edgeLANPort(ctx context.Context, dc *dockerclient.Client) int {
	res, err := dc.ContainerInspect(ctx, runner.EdgeContainerName, dockerclient.ContainerInspectOptions{})
	if err != nil || res.Container.NetworkSettings == nil {
		return 0
	}
	for _, bindings := range res.Container.NetworkSettings.Ports {
		for _, b := range bindings {
			if b.HostIP.IsValid() && b.HostIP.IsLoopback() {
				continue
			}
			if p, err := strconv.Atoi(b.HostPort); err == nil && p != 80 && p != 443 {
				return p
			}
		}
	}
	return 0
}

func dialOK(host string, port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
