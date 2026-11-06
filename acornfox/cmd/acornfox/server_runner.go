//go:build !windows

// This file implements the "acornfox server" and "acornfox runner" subcommands
// (N1): it wires state, runner, caddyroute, reconcile and apiserver together.
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

	"github.com/acornfox/acornfox/internal/caddyroute"
	"github.com/acornfox/acornfox/internal/peer"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/runner"
)

// runServer implements `acornfox server` (Linux): it parses flags and starts
// the server with the runner reached over its peer socket. The trusted API is
// served only on unix sockets (N3); the browser console separately on
// -console-listen behind cookie sessions and CSRF.
//
// Caddy (three-platform architecture, 2026-11-01): by default the server runs
// Caddy as a container on the host network (-edge host) and reaches its admin
// API through a token-checked gateway on 127.0.0.1:-edge-admin-port.
// -edge off -caddy-admin /run/acornfox/caddy-admin.sock keeps the v0.3 host
// Caddy (used by upgrade rollback and tests).
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
		edgeMode      = fs_.String("edge", "host", "Caddy 运行方式：host（容器，宿主网络，默认）、off（用 -caddy-admin 指定的宿主机 Caddy）")
		edgeAdminPort = fs_.Int("edge-admin-port", defaultEdgeAdminPort, "Caddy 容器管理网关的本机回环端口")
		caddyAdmin    = fs_.String("caddy-admin", "/run/acornfox/caddy-admin.sock", "宿主机 Caddy 的 admin unix socket（仅 -edge off）")
		publicHost    = fs_.String("public-host", "", "host used in app URLs (e.g. the LAN IP)")
		publicPortMin = fs_.Int("public-port-min", 18810, "应用公开端口范围下限")
		publicPortMax = fs_.Int("public-port-max", 18899, "应用公开端口范围上限")
		httpPort      = fs_.Int("http-port", 80, "HTTP port of the shared af-domains server (ACME HTTP-01 and redirect)")
		httpsPort     = fs_.Int("https-port", 443, "HTTPS port of the shared af-domains server")
		httpsIssuer   = fs_.String("https-issuer", "", "certificate issuer: \"\"/acme = public ACME, internal = Caddy local CA (dev)")
		httpsCAFile   = fs_.String("https-ca-file", "", "path to the CA root used to verify domain certificates (internal issuer)")
		acmeEmail     = fs_.String("acme-email", "", "optional ACME account email")
		publicListen  = fs_.String("public-console-listen", "127.0.0.1:18801", "public console gateway behind Caddy (loopback TCP only; empty disables it)")
		lanPort       = fs_.Int("lan-console-port", 0, "局域网控制台端口（开启后监听 0.0.0.0；默认 18802）")
		exposure      = fs_.String("default-exposure", "public", "应用默认暴露方式：public、lan、local")
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
	switch *edgeMode {
	case runner.EdgeModeHost, edgeModeOff:
	default:
		fmt.Fprintln(stderr, "invalid -edge: 只能是 host 或 off")
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
		HTTPS: caddyroute.HTTPSConfig{
			HTTPPort:  *httpPort,
			HTTPSPort: *httpsPort,
			Issuer:    *httpsIssuer,
			Email:     *acmeEmail,
		},
		HTTPSCAFile:     *httpsCAFile,
		EdgeMode:        *edgeMode,
		EdgeAdminPort:   *edgeAdminPort,
		CaddyAdmin:      *caddyAdmin,
		DefaultExposure: *exposure,
		LANConsolePort:  *lanPort,
		Setup: &setupDeps{
			DataDir: *dataDir,
			Console: "http://" + *consoleListen,
			Runner:  rc,
			Runtime: runnerRuntime{r: rc},
			Log:     stdout,
		},
	}, rc, stdout, stderr)
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
			reg.Mirrors = runner.CloudRegistryMirrors(platform.LinuxInstallCloud("/etc/acornfox/install.env"))
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
