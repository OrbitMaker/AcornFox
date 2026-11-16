//go:build !windows

// This file implements the "acornfox server" and "acornfox runner" subcommands
// (N1): it wires state, runner, the entry proxy (edge), reconcile and apiserver together.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/moby/moby/client"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/peer"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/runner"
)

// runServer implements `acornfox server` (Linux): it parses flags and starts
// the server with the runner reached over its peer socket. The trusted API is
// served only on unix sockets (N3); the browser console separately on
// -console-listen behind cookie sessions and CSRF.
//
// The entry proxy runs as a container on the host network (-edge host); the
// server reaches the adapter in it on its token-checked management endpoint
// 127.0.0.1:-edge-admin-port.
func runServer(args []string, stdout, stderr io.Writer) int {
	fs_ := flag.NewFlagSet("server", flag.ContinueOnError)
	fs_.SetOutput(stderr)
	var listens listenList
	fs_.Var(&listens, "listen", "trusted API unix socket (repeatable): unix:/path.sock")
	var (
		dataDir       = fs_.String("data-dir", "/var/lib/acornfox", "state directory (holds acornfox.db and uploads/)")
		listenGroup   = fs_.String("listen-group", "", "group owner for unix socket listeners (mode 0660)")
		consoleListen = fs_.String("console-listen", "127.0.0.1:18800", "browser console listener (loopback TCP only; empty disables it)")
		runnerSocket  = fs_.String("runner-socket", "/run/acornfox/runner.sock", "runner peer socket path")
		runnerUID     = fs_.Int("runner-uid", os.Getuid(), "uid the runner process runs as")
		edgeMode      = fs_.String("edge", "host", "入口代理运行方式：host（容器，宿主网络，也是唯一的选项）")
		edgeAdminPort = fs_.Int("edge-admin-port", defaultEdgeAdminPort, "入口代理容器管理口的本机回环端口")
		publicHost    = fs_.String("public-host", "", "host used in app URLs (e.g. the LAN IP)")
		publicPortMin = fs_.Int("public-port-min", 18810, "应用公开端口范围下限")
		publicPortMax = fs_.Int("public-port-max", 18899, "应用公开端口范围上限")
		httpPort      = fs_.Int("http-port", 80, "HTTP port of the shared af-domains server (ACME HTTP-01 and redirect)")
		httpsPort     = fs_.Int("https-port", 443, "HTTPS port of the shared af-domains server")
		httpsIssuer   = fs_.String("https-issuer", "", "certificate issuer: \"\"/acme = public ACME, internal = the entry proxy's local CA (dev)")
		httpsCAFile   = fs_.String("https-ca-file", "", "path to the CA root used to verify domain certificates (internal issuer)")
		acmeEmail     = fs_.String("acme-email", "", "optional ACME account email")
		publicListen  = fs_.String("public-console-listen", "127.0.0.1:18801", "public console gateway behind the entry proxy (loopback TCP only; empty disables it)")
		lanPort       = fs_.Int("lan-console-port", 0, "局域网控制台端口（开启后监听 0.0.0.0；默认 18802）")
		exposure      = fs_.String("default-exposure", "public", "新应用默认的对外方式：public、lan、local。不给时是 public（自己部署的项目），应用中心装的应用是 local；"+
			"给了 lan 或 local 时两者都用它（install.sh 在局域网里新装的服务器上写 lan）")
	)
	if err := fs_.Parse(args); err != nil {
		return 2
	}
	if len(listens) == 0 {
		// Default trusted entry is the unix socket per the N3 contract.
		listens = listenList{"unix:/run/acornfox/api.sock"}
	}

	// -listen accepts unix sockets only. TCP would re-expose the
	// unauthenticated API, so it is rejected with guidance to -console-listen.
	for _, addr := range listens {
		if err := validateTrustedListen(addr); err != nil {
			fmt.Fprintln(stderr, "invalid -listen:", err)
			return 2
		}
	}
	if err := validateConsoleListen(*consoleListen); err != nil {
		fmt.Fprintln(stderr, "invalid -console-listen:", err)
		return 2
	}
	if err := validateConsoleListen(*publicListen); err != nil {
		fmt.Fprintln(stderr, "invalid -public-console-listen:", err)
		return 2
	}
	if *edgeMode != runner.EdgeModeHost {
		fmt.Fprintln(stderr, "invalid -edge: 只能是 host")
		return 2
	}
	switch *exposure {
	case "public", "lan", "local":
	default:
		fmt.Fprintln(stderr, "invalid -default-exposure: 只能是 public、lan、local")
		return 2
	}

	gid := -1
	if *listenGroup != "" {
		g, err := lookupGroupID(*listenGroup)
		if err != nil {
			fmt.Fprintln(stderr, "invalid -listen-group:", err)
			return 2
		}
		gid = g
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	rc := runner.NewClient(*runnerSocket, uint32(*runnerUID))
	return startServer(ctx, serverOptions{
		DataDir:       *dataDir,
		Listens:       listens,
		ListenGID:     gid,
		ConsoleListen: *consoleListen,
		PublicListen:  *publicListen,
		PublicHost:    *publicHost,
		PublicPortMin: *publicPortMin,
		PublicPortMax: *publicPortMax,
		HTTPS: edge.HTTPS{
			HTTPPort:  *httpPort,
			HTTPSPort: *httpsPort,
			Issuer:    *httpsIssuer,
			Email:     *acmeEmail,
		},
		HTTPSCAFile:        *httpsCAFile,
		EdgeMode:           *edgeMode,
		EdgeAdminPort:      *edgeAdminPort,
		DefaultExposure:    *exposure,
		AppDefaultExposure: appCenterExposure(*exposure),
		LANConsolePort:     *lanPort,
		CheckPorts:         true,
		ServerLAN:          serverLANAddress(*publicHost, platform.LinuxInstallCloud("/etc/acornfox/install.env")),
		Setup: &setupDeps{
			DataDir: *dataDir,
			Console: "http://" + *consoleListen,
			Runner:  rc,
			Runtime: runnerRuntime{r: rc},
			Log:     stdout,
		},
	}, rc, stdout, stderr)
}

// appCenterExposure is what a new app from the app center gets on a Linux
// server whose default exposure (--default-exposure) is d: lan or local as
// given; otherwise local. The flag's default, public, is what servers
// installed before it existed run with: their app-center apps stay local
// and an app from the catalog is never opened to every interface and its
// domains without the user choosing it (install --expose public).
func appCenterExposure(d string) string {
	switch d {
	case "lan", "local":
		return d
	}
	return "local"
}

// runRunner implements `acornfox runner` (N1): connect to Docker via client
// environment and serve the runner API on the peer socket, accepting only the
// server uid.
func runRunner(args ...string) int {
	fs := flag.NewFlagSet("runner", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		socket    = fs.String("socket", "/run/acornfox/runner.sock", "peer socket to serve on")
		serverUID = fs.Int("server-uid", os.Getuid(), "uid of the acornfox server allowed to connect")
		socketGID = fs.Int("socket-gid", 0, "group owner of the socket (0 = leave default)")
		uploadDir = fs.String("upload-dir", "/var/lib/acornfox/uploads", "directory holding server uploads; builds may only read files under it")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cli, err := client.New(client.FromEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect docker:", err)
		return 1
	}
	defer cli.Close()

	api := runner.NewDocker(cli)
	// install.sh records whether this server is in mainland China; Docker Hub
	// images are then pulled from the domestic mirror first.
	if china, ok := platform.LinuxInstallChina("/etc/acornfox/install.env"); ok {
		reg := runner.Registry{China: china}
		if china {
			// A cloud vendor's internal accelerator (Tencent Cloud today) only
			// answers inside its network and is by far the fastest there.
			// Mirrors the user gave install.sh (--registry-mirror, in
			// daemon.json but not in the table) go in front.
			reg.Mirrors = runner.WithUserMirrors(runner.CloudRegistryMirrors(platform.LinuxInstallCloud("/etc/acornfox/install.env")),
				runner.DaemonMirrorHosts(runner.DaemonConfigPath))
		}
		api.SetRegistry(reg)
	}
	// The runner does not stage uploads itself; the server writes them under
	// data-dir/uploads which the runner reads by absolute path.
	if !filepath.IsAbs(*uploadDir) {
		fmt.Fprintln(os.Stderr, "-upload-dir must be absolute")
		return 2
	}
	handler := runner.NewServer(api, filepath.Clean(*uploadDir))

	ln, err := peer.Listen(*socket, uint32(*socketGID), uint32(*serverUID))
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen peer socket:", err)
		return 1
	}
	defer ln.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srv := &http.Server{Handler: handler}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	fmt.Fprintln(os.Stdout, "acornfox runner serving on", *socket)

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "serve:", err)
		}
	}
	shCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = srv.Shutdown(shCtx)
	return 0
}

// serverLANAddress returns the Linux server's address on the user's local
// network for the console's open hints, or "" on a cloud or other public
// server (platform.ServerLANAddress decides; the address is looked up at
// most every 30 seconds). Besides the vendor install.sh recorded, the cloud
// metadata address is asked in the background: until it is known not to
// answer, the server counts as a cloud server.
func serverLANAddress(publicHost, cloud string) func() string {
	lan := cachedLANAddress()
	probe := platform.NewCloudProbe()
	probe.Start()
	return func() string { return platform.ServerLANAddress(lan(), publicHost, cloud, probe.OnCloud()) }
}
