package main

// "acornfox serve": the single-process server for macOS and Windows
// (three-platform architecture, 2026-11-01). It runs as the logged-in user,
// calls the runner in-process (no second account, no peer socket), finds
// Docker through internal/platform and runs Caddy as a container on a bridge
// network. The autostart entry written by "acornfox install" runs it with
// --background.
//
// It also runs on Linux (useful for testing the bridge path), but Linux
// installs use the server + runner pair from install.sh.

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	dockerclient "github.com/moby/moby/client"

	"github.com/acornfox/acornfox/internal/caddyroute"
	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/setup"
)

const (
	defaultConsoleListen = "127.0.0.1:18800"
	maxServeLogBytes     = 10 << 20
)

func runServe(args []string, stdout, stderr io.Writer) int {
	paths := platform.DefaultPaths()
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		dataDir       = fs.String("data-dir", paths.DataDir, "数据目录")
		consoleListen = fs.String("console-listen", defaultConsoleListen, "控制台监听地址（只允许本机回环）")
		edgeMode      = fs.String("edge", runner.EdgeModeBridge, "Caddy 容器的网络方式：bridge（默认）或 host（仅 Linux）")
		edgeAdminPort = fs.Int("edge-admin-port", defaultEdgeAdminPort, "Caddy 管理网关的本机端口")
		publicPortMin = fs.Int("public-port-min", 18810, "应用端口范围下限")
		publicPortMax = fs.Int("public-port-max", 18859, "应用端口范围上限（Mac/Windows 预留 50 个，Caddy 容器一次映射好）")
		exposure      = fs.String("default-exposure", platform.DefaultExposure(), "应用默认暴露方式：lan、local、public")
		lanPort       = fs.Int("lan-console-port", 0, "局域网控制台端口（默认 18802）")
		background    = fs.Bool("background", false, "后台运行：输出写入日志文件（开机自启用）")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := validateConsoleListen(*consoleListen); err != nil {
		fmt.Fprintln(stderr, "invalid -console-listen:", err)
		return 2
	}
	switch *edgeMode {
	case runner.EdgeModeBridge, runner.EdgeModeHost:
	default:
		fmt.Fprintln(stderr, "invalid -edge: 只能是 bridge 或 host")
		return 2
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		fmt.Fprintln(stderr, "create data dir:", err)
		return 1
	}
	if *background {
		logPath := filepath.Join(*dataDir, "logs", "acornfox.log")
		f, err := openServeLog(logPath)
		if err != nil {
			fmt.Fprintln(stderr, "open log:", err)
			return 1
		}
		defer f.Close()
		stdout, stderr = f, f
		detachConsole(f)
		// A crash (panic) goes to the log too; otherwise it is lost, because a
		// background process started by the logon task has no console.
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	}

	ep := platform.ResolveDocker()
	fmt.Fprintf(stdout, "%s acornfox serve %s：Docker %s（%s，来源 %s）\n", time.Now().Format(time.RFC3339), cli.Version, ep.Host, ep.Runtime, ep.Source)
	dc, err := dockerclient.New(dockerclient.FromEnv, dockerclient.WithHost(ep.Host))
	if err != nil {
		fmt.Fprintln(stderr, "connect docker:", err)
		return 1
	}
	defer dc.Close()
	docker := runner.NewDocker(dc)
	netcfg := platform.LoadNetwork(*dataDir)
	docker.SetRegistry(runner.Registry{China: netcfg.China, Mirrors: netcfg.Mirrors})
	fmt.Fprintf(stdout, "镜像来源：%s\n", map[bool]string{true: "国内镜像源优先", false: "Docker Hub 优先"}[netcfg.China])

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	socket := filepath.Join(*dataDir, "api.sock")
	return startServer(ctx, serverOptions{
		DataDir:         *dataDir,
		Listens:         []string{"unix:" + socket},
		ListenGID:       -1,
		ConsoleListen:   *consoleListen,
		PublicHost:      "localhost",
		PublicPortMin:   *publicPortMin,
		PublicPortMax:   *publicPortMax,
		HTTPS:           caddyroute.HTTPSConfig{HTTPPort: 80, HTTPSPort: 443},
		EdgeMode:        *edgeMode,
		EdgeAdminPort:   *edgeAdminPort,
		DefaultExposure: *exposure,
		LANHost:         cachedLANAddress(),
		HostMetrics:     newDockerVMHost(docker.Client, docker),
		LANConsolePort:  *lanPort,
		LANRelay:        true,
		Setup: &setupDeps{
			DataDir: *dataDir,
			Console: "http://" + *consoleListen,
			Runner:  docker,
			Docker:  docker,
			Runtime: setup.NewDesktopRuntime(setup.DesktopRuntimeConfig{
				DataDir: *dataDir,
				Version: cli.Version,
				Logf: func(f string, a ...any) {
					fmt.Fprintf(stdout, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(f, a...))
				},
			}),
			Log: stdout,
		},
	}, docker, stdout, stderr)
}

// openServeLog opens the serve log for appending, moving a log larger than
// maxServeLogBytes aside to .1 first.
func openServeLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > maxServeLogBytes {
		_ = os.Rename(path, path+".1")
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

// cachedLANAddress looks the LAN address up at most every 30 seconds (Wi-Fi
// changes, DHCP renewals).
func cachedLANAddress() func() string {
	var (
		mu   sync.Mutex
		at   time.Time
		addr string
	)
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) > 30*time.Second {
			addr, at = platform.LANAddress(), time.Now()
		}
		return addr
	}
}
