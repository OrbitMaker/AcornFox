package main

// Wiring of the runtime setup (internal/setup) into the server: how the
// runtime is detected, how the gateway is prepared and which self checks run.
// "acornfox serve" (macOS, Windows) can install the built-in runtime; the
// Linux server only detects Docker through the runner (install.sh installs
// Docker and checks the entry proxy image).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"

	"github.com/acornfox/acornfox/internal/apiserver"
	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/reconcile"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/setup"
)

// setupDeps is what both platforms' setup wiring needs.
type setupDeps struct {
	DataDir    string
	Console    string
	Options    serverOptions
	Runner     serverRunner
	Docker     *runner.Docker // nil on the Linux server (Docker is behind the runner)
	Runtime    setup.RuntimeProvider
	LANEnabled func() bool
	Log        io.Writer
	Relay      *lanForwarder // nil unless the LAN relay is configured
	// Proxy is the entry proxy (startServer sets it); the self checks ask
	// it whether it works instead of probing a particular proxy.
	Proxy edge.Proxy
}

func newSetupManager(d setupDeps, rec *reconcile.Reconciler) *setup.Manager {
	var logMu sync.Mutex
	logf := func(format string, args ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		fmt.Fprintf(d.Log, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
	}
	return setup.New(setup.Config{
		DataDir: d.DataDir,
		Version: cli.Version,
		Network: func() string {
			if setupNetwork(d.DataDir).China {
				return "china"
			}
			return "global"
		},
		Console: d.Console,
		Runtime: d.Runtime,
		Edge:    edgePreparer(d, rec),
		Checks:  setupChecks(d, rec),
		OnRuntime: func(det setup.Detection) {
			if d.Docker == nil || det.Host == "" || det.Host == "runner" {
				return
			}
			c, err := dockerclient.New(dockerclient.FromEnv, dockerclient.WithHost(det.Host))
			if err != nil {
				logf("setup: docker client %s: %v", det.Host, err)
				return
			}
			old := d.Docker.SetClient(c)
			logf("setup: Docker 改用 %s（%s %s）", det.Host, det.Name, det.Version)
			if old != nil {
				go func() { time.Sleep(time.Minute); _ = old.Close() }()
			}
		},
		Logf: logf,
	})
}

// edgeWait bounds the wait for the entry proxy container in the setup.
var edgeWait = 2 * time.Minute

// edgePreparer makes sure the entry proxy image is there and waits until the
// reconciler has the entry proxy container running. On macOS and Windows a
// missing image is loaded from the release's entry package (the same file
// the Linux install script loads, from the same place as the program,
// checked against the release's SHA256SUMS); it never comes from an image
// registry. On the Linux server (d.Docker nil) the scripts load it.
func edgePreparer(d setupDeps, rec *reconcile.Reconciler) setup.EdgePreparer {
	return func(ctx context.Context, r setup.Reporter) (string, error) {
		if d.Docker != nil && !d.Docker.EdgeImagePresent(ctx) {
			if err := importEdgePackage(ctx, d, r); err != nil {
				r.Item(setup.Item{ID: "edge-image", Title: "入口镜像", State: setup.StateFailed})
				var f *setup.Fault
				if errors.As(err, &f) {
					return "", f
				}
				return "", setup.Faultf("SETUP-EDGE-01", err, "入口镜像没有准备好（故障编号 %s）", edge.FaultImageMissing)
			}
		}
		r.Item(setup.Item{ID: "edge", Title: "网关容器", State: setup.StateRunning})
		deadline := time.Now().Add(edgeWait)
		for !rec.EnsureEdge(ctx) {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if time.Now().After(deadline) {
				_, msg := rec.EdgeStatus()
				return "", setup.Faultf("SETUP-EDGE-01", errors.New(msg), "网关容器 %d 分钟内没有运行起来", int(edgeWait.Minutes()))
			}
			time.Sleep(2 * time.Second)
		}
		r.Item(setup.Item{ID: "edge", Title: "网关容器", State: setup.StateDone, Detail: "运行中"})
		return "入口 " + edge.ImageVersion + " · 运行中", nil
	}
}

// setupChecks returns the self checks shown in the last step.
func setupChecks(d setupDeps, rec *reconcile.Reconciler) func(context.Context) []setup.Check {
	return func(ctx context.Context) []setup.Check {
		var out []setup.Check
		add := func(c setup.Check) { out = append(out, c) }

		// 1. Docker works.
		dockerOK, detail := checkDocker(ctx, d)
		add(setup.Check{ID: "docker", Title: "Docker 能创建和删除容器", OK: dockerOK, Required: true, Detail: detail, Code: code(dockerOK, "SETUP-CHK-02")})

		if d.Options.EdgeMode != "" {
			// 2. The proxy's management endpoint answers only with the token
			// (the proxy's own Health decides what that takes).
			adminOK, detail := checkEdgeAdmin(ctx, d)
			adminCode := "SETUP-CHK-03"
			if !adminOK && platform.GOOS == "windows" {
				// The edge runs but its 127.0.0.1 port does not reach Windows:
				// WSL's localhost forwarding is off or broken.
				if st, _ := rec.EdgeStatus(); st == reconcile.EdgeStateRunning {
					if r, ok := platform.LoadBuiltinRuntime(d.DataDir); ok && r.Kind == setup.RuntimeBuiltin {
						adminCode = "SETUP-WIN-05"
						detail = "网关在运行，但 Windows 上连不上 127.0.0.1:" + strconv.Itoa(d.Options.EdgeAdminPort) + "（WSL 没有把端口转发到本机）"
					}
				}
			}
			add(setup.Check{ID: "edge_admin", Title: "网关管理口只在本机、要令牌", OK: adminOK, Required: true, Detail: detail, Code: code(adminOK, adminCode)})
			// 3. The app port range is published (bridge) / the edge runs (host).
			portsOK, detail := checkPorts(ctx, d, rec)
			add(setup.Check{ID: "ports", Title: "应用端口已预留", OK: portsOK, Required: true,
				Detail: fmt.Sprintf("%d – %d%s", d.Options.PublicPortMin, d.Options.PublicPortMax, detail), Code: code(portsOK, "SETUP-EDGE-01")})
		}

		// 4. LAN access (not required: a computer without a LAN still works locally).
		lanOK, detail := checkLAN(ctx, d, rec)
		add(setup.Check{ID: "lan", Title: "局域网能打开", OK: lanOK, Detail: detail, Code: code(lanOK, "SETUP-CHK-04")})

		// 5. The console listens on loopback only (validated at start); the LAN
		// console is off unless the user enabled it (password protected).
		consoleDetail := d.Options.ConsoleListen
		if d.LANEnabled != nil && d.LANEnabled() {
			consoleDetail += "；局域网控制台已开启（要密码）"
		}
		add(setup.Check{ID: "console", Title: "控制台只在本机打开", OK: true, Required: true, Detail: consoleDetail})

		// 6. The image source answers (not required: apps may use local images).
		regOK, detail := checkRegistry(ctx, d.DataDir)
		add(setup.Check{ID: "registry", Title: "镜像下载源可用", OK: regOK, Detail: detail, Code: code(regOK, "SETUP-NET-01")})
		return out
	}
}

func code(ok bool, c string) string {
	if ok {
		return ""
	}
	return c
}

func checkDocker(ctx context.Context, d setupDeps) (bool, string) {
	if d.Docker == nil {
		// Linux: Docker is behind the runner.
		if _, err := d.Runner.Ping(ctx); err != nil {
			return false, "执行器连不上 Docker：" + err.Error()
		}
		if _, err := d.Runner.ListContainers(ctx, ""); err != nil {
			return false, err.Error()
		}
		return true, "通过执行器"
	}
	c := d.Docker.Client()
	// Any image already on this computer will do (nothing is downloaded):
	// the edge container's, or else the first local image.
	img, err := d.Docker.LocalImageForCheck(ctx)
	if err != nil {
		return false, "找不到可以用来试建容器的本地镜像：" + err.Error()
	}
	name := "acornfox-selfcheck-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	// The container is never started; an explicit command keeps the create
	// working with images that declare none (Docker refuses those).
	created, err := c.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{Name: name, Config: &container.Config{Image: img, Cmd: []string{"true"}}})
	if err != nil {
		return false, "创建测试容器失败：" + err.Error()
	}
	if _, err := c.ContainerRemove(ctx, created.ID, dockerclient.ContainerRemoveOptions{Force: true}); err != nil {
		return false, "删除测试容器失败：" + err.Error()
	}
	return true, ""
}

// edgeHealthTimeout bounds one Health call of the self checks.
var edgeHealthTimeout = 8 * time.Second

func checkEdgeAdmin(ctx context.Context, d setupDeps) (bool, string) {
	if d.Proxy == nil {
		return false, "网关没有接上"
	}
	hctx, cancel := context.WithTimeout(ctx, edgeHealthTimeout)
	defer cancel()
	if h := d.Proxy.Health(hctx); !h.OK() {
		return false, h.Reason
	}
	return true, "127.0.0.1:" + strconv.Itoa(d.Options.EdgeAdminPort)
}

func checkPorts(ctx context.Context, d setupDeps, rec *reconcile.Reconciler) (bool, string) {
	if st, msg := rec.EdgeStatus(); st != reconcile.EdgeStateRunning {
		return false, "（网关：" + st + " " + msg + "）"
	}
	if d.Docker == nil || d.Options.EdgeMode != runner.EdgeModeBridge {
		return true, ""
	}
	res, err := d.Docker.Client().ContainerInspect(ctx, runner.EdgeContainerName, dockerclient.ContainerInspectOptions{})
	if err != nil || res.Container.NetworkSettings == nil {
		return false, "（读不到网关容器的端口）"
	}
	have := map[int]bool{}
	for _, bs := range res.Container.NetworkSettings.Ports {
		for _, b := range bs {
			if p, err := strconv.Atoi(b.HostPort); err == nil {
				have[p] = true
			}
		}
	}
	for p := d.Options.PublicPortMin; p <= d.Options.PublicPortMax; p++ {
		if !have[p] {
			return false, fmt.Sprintf("（%d 没有映射）", p)
		}
	}
	return true, ""
}

func checkLAN(ctx context.Context, d setupDeps, rec *reconcile.Reconciler) (bool, string) {
	lan := ""
	if d.Options.LANHost != nil {
		lan = d.Options.LANHost()
	}
	if lan == "" || lan == "localhost" {
		return false, "没取到局域网地址，应用只能在本机打开"
	}
	if rec.LANUnbindable(lan) {
		// Docker cannot bind the LAN address; acornfox serve relays it.
		if d.Relay == nil {
			return false, "运行环境绑定不了局域网地址 " + lan + "，应用只能在本机打开"
		}
		d.Relay.sync() // the edge may have just fallen back: start listening now, not in up to 10 s
		host, n, failed := d.Relay.Status()
		switch {
		case host != lan || n == 0:
			return false, "局域网转发还没有在 " + lan + " 上启动（10 秒内会自动重试）"
		case len(failed) > 0:
			return false, fmt.Sprintf("局域网转发有 %d 个端口被占用：%v", len(failed), failed)
		}
		if !platform.FirewallRulePresent() {
			return false, lan + "：没有 AcornFox 防火墙规则（选了“只在本机用”就可以忽略；要打开运行 acornfox setup --confirm-admin）"
		}
		if platform.NetworkCategory(lan) == "Public" {
			return false, lan + "：当前网络是“公用网络”，防火墙规则只在专用网络生效；到 设置 → 网络 改成“专用网络”"
		}
		return true, lan + "（由 AcornFox 转发）"
	}
	if d.Docker == nil {
		return true, lan
	}
	port := edgeLANPort(ctx, d.Docker.Client())
	if port == 0 {
		return true, lan
	}
	local, remote := dialOK("127.0.0.1", port), dialOK(lan, port)
	switch {
	case remote:
		return true, lan
	case !local:
		// Nothing listens on that port yet (no app there): nothing to tell.
		return true, lan + "（还没有应用在局域网上打开，部署后可用 acornfox doctor 复查）"
	}
	return false, fmt.Sprintf("%s:%d 连不上，本机能连（防火墙或运行环境只绑定了 127.0.0.1）", lan, port)
}

// linuxInstallEnv is where install.sh records the Linux server's network
// (ACORNFOX_IN_CHINA, ACORNFOX_CLOUD); tests point it elsewhere.
var linuxInstallEnv = "/etc/acornfox/install.env"

// setupNetwork is the network the setup reports and the registry check
// tests. The Linux server keeps install.sh's choice in install.env and never
// writes network.json (whose absence would otherwise mean China), and its
// runner pulls by install.env too; macOS and Windows keep it in network.json.
func setupNetwork(dataDir string) platform.Network {
	if china, ok := platform.LinuxInstallChina(linuxInstallEnv); ok {
		n := platform.Network{China: china, Source: "install.env"}
		if china {
			n.Mirrors = runner.WithUserMirrors(runner.CloudRegistryMirrors(platform.LinuxInstallCloud(linuxInstallEnv)), runner.DaemonMirrorHosts(daemonConfigFile))
		}
		return n
	}
	return platform.LoadNetwork(dataDir)
}

// gatewayBase is the download gateway's address ("" when it is off) used by
// the app center, by doctor's clock check and shown by the settings and
// runtime pages, so the three always agree. ACORNFOX_GATEWAY wins; otherwise
// network.json's "gateway" ("off" turns it off) is honoured on every
// platform, also on a Linux server whose region comes from install.env
// (install.env says nothing about the gateway).
func gatewayBase(dataDir string) string {
	n := setupNetwork(dataDir)
	if n.Source == "install.env" {
		n.Gateway = platform.LoadNetwork(dataDir).Gateway
	}
	return n.EffectiveGateway()
}

// downloadSources is what the console's settings page shows about where this
// server downloads from: the Docker Hub pull order the runner uses, the
// ghcr.io mirrors and the download gateway. On a Linux server without
// install.env the runner keeps its default (not China), so this does too.
func downloadSources(dataDir string) apiserver.DownloadSources {
	n := setupNetwork(dataDir)
	if n.Source != "install.env" && !platform.DesktopMode() && platform.GOOS == "linux" {
		n = platform.Network{Source: "default"}
	}
	reg := runner.Registry{China: n.China, Mirrors: n.Mirrors}
	return apiserver.DownloadSources{
		China: n.China, RegionSource: n.Source, DockerHub: reg.PullSources(), GHCR: runner.GHCRMirrors(),
		Gateway: gatewayBase(dataDir), DownloadBase: n.EffectiveDownloadBase(),
	}
}

// checkRegistry probes the image sources in the order the runner pulls
// Docker Hub images from (runner.Registry.PullSources): the check passes
// when any of them answers, since the runner moves on to the next source
// when one fails. Only when none answers is it SETUP-NET-01.
func checkRegistry(ctx context.Context, dataDir string) (bool, string) {
	n := setupNetwork(dataDir)
	reg := runner.Registry{China: n.China, Mirrors: n.Mirrors}
	return checkRegistrySources(ctx, reg.PullSources(), n.China, probeRegistry)
}

// probeRegistry asks host's registry API root; 200 and 401 mean it answers.
// Tests replace it.
var probeRegistry = func(ctx context.Context, host string) error {
	cl := &http.Client{Timeout: 6 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/v2/", nil)
	if err != nil {
		return err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return errors.New("连不上")
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return nil
	}
	return fmt.Errorf("返回 %d", resp.StatusCode)
}

// registryLabel names an image source for the check's detail.
func registryLabel(host string, china bool) string {
	switch {
	case host == runner.DockerHubRegistry:
		return "Docker Hub"
	case host == runner.TencentRegistryMirror:
		return "腾讯云内网加速"
	case china:
		return "国内镜像"
	}
	return "镜像加速"
}

// checkRegistrySources probes sources concurrently and judges them in
// order: the first one that answers is the one the runner ends up using.
// Docker Hub itself, when it comes after mirrors, is only probed once every
// source before it has failed: in China it is blocked, and asking it on
// every start would be a connection to a blocked address for nothing (the
// runner only goes there when the mirrors fail too).
func checkRegistrySources(ctx context.Context, sources []string, china bool, probe func(context.Context, string) error) (bool, string) {
	if len(sources) == 0 {
		return false, "没有可用的镜像下载源"
	}
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	deferred := func(i int) bool { return i > 0 && sources[i] == runner.DockerHubRegistry }
	results := make([]chan error, len(sources))
	for i, host := range sources {
		results[i] = make(chan error, 1)
		if !deferred(i) {
			go func(ch chan<- error, host string) { ch <- probe(pctx, host) }(results[i], host)
		}
	}
	var failed []string
	for i, host := range sources {
		name := registryLabel(host, china) + "（" + host + "）"
		if deferred(i) {
			// Every source before it failed (or we would have returned).
			results[i] <- probe(pctx, host)
		}
		if err := <-results[i]; err != nil {
			failed = append(failed, name+err.Error())
			continue
		}
		if len(failed) == 0 {
			return true, name
		}
		return true, "用" + name + "；前面的" + strings.Join(failed, "，") + "，拉镜像时会自动跳过"
	}
	return false, "镜像下载源都不通：" + strings.Join(failed, "，")
}

// runnerRuntime is the Linux server's runtime provider: Docker is reached
// through the runner, installed by install.sh; there is nothing to install.
type runnerRuntime struct{ r runner.API }

func (rr runnerRuntime) Detect(ctx context.Context) setup.Detection {
	p, err := rr.r.Ping(ctx)
	if err != nil {
		return setup.Detection{}
	}
	return setup.Detection{Reachable: true, Host: "runner", Kind: "docker", Name: "Docker Engine", Version: p.ServerVersion,
		Found: []setup.Found{{Kind: "docker", Name: "Docker Engine", Version: p.ServerVersion, Running: true}}}
}
func (runnerRuntime) CanBuiltin() bool { return false }
func (runnerRuntime) StartDockerDesktop(context.Context) error {
	return errors.New("Linux 服务器没有 Docker Desktop")
}
func (runnerRuntime) Builtin(context.Context, setup.StartOptions, setup.Reporter) (setup.Detection, error) {
	return setup.Detection{}, setup.Faultf("SETUP-RT-03", nil, "Linux 服务器请用 install.sh 安装 Docker")
}

// importEdgePackage downloads the entry package for this program's version
// and the Docker daemon's architecture and loads it (desktop setup).
func importEdgePackage(ctx context.Context, d setupDeps, r setup.Reporter) error {
	arch := "amd64"
	if info, err := d.Docker.Client().Info(ctx, dockerclient.InfoOptions{}); err == nil {
		switch strings.ToLower(info.Info.Architecture) {
		case "aarch64", "arm64":
			arch = "arm64"
		}
	}
	dl := &setup.Downloader{Dir: filepath.Join(d.DataDir, "runtime", "downloads")}
	path, err := dl.FetchEdgePackage(ctx, cli.Version, arch, setupNetwork(d.DataDir).China, nil, r.Item)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r.Item(setup.Item{ID: "edge-image", Title: "入口镜像", State: setup.StateRunning, Detail: "导入中"})
	if err := d.Docker.ImportEdgeImage(ctx, f); err != nil {
		return err
	}
	r.Item(setup.Item{ID: "edge-image", Title: "入口镜像", State: setup.StateDone, Detail: edge.ImageVersion})
	return nil
}
