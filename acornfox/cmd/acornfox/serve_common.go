package main

// Shared startup of the acornfox server, used by "acornfox server" (Linux,
// runner in a separate process) and "acornfox serve" (macOS and Windows,
// runner called in-process). See internal/platform for what differs.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/acornfox/acornfox/internal/apiserver"
	"github.com/acornfox/acornfox/internal/caddyroute"
	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/publicconsole"
	"github.com/acornfox/acornfox/internal/reconcile"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// consoleFS holds the embedded console static assets. It is populated by
// console_wiring.go (init) when the github.com/acornfox/acornfox/internal/console
// package is present; otherwise it stays nil and the console listener serves a
// small placeholder page. The wiring lives in a separate file so the server can
// build before that package exists.
var consoleFS fs.FS

// Edge (Caddy container) defaults.
const (
	edgeModeOff          = "off"
	defaultEdgeAdminPort = 18808
	edgeTokenFile        = "caddy-admin.token"
)

// serverOptions is everything startServer needs; flag parsing (Linux) or the
// platform defaults (desktop) fill it in.
type serverOptions struct {
	DataDir       string
	Listens       []string // trusted API unix sockets
	ListenGID     int      // -1: leave the socket group alone
	ConsoleListen string   // loopback console; "" disables
	PublicListen  string   // public console gateway behind Caddy; "" disables
	PublicHost    string
	PublicPortMin int
	PublicPortMax int
	HTTPS         caddyroute.HTTPSConfig
	HTTPSCAFile   string

	// Caddy: EdgeMode host|bridge runs Caddy as a container managed through
	// the runner (admin gateway on 127.0.0.1:EdgeAdminPort); "off" uses the
	// v0.3 host Caddy at CaddyAdmin (a unix socket).
	EdgeMode      string
	EdgeAdminPort int
	CaddyAdmin    string

	DefaultExposure string
	LANHost         func() string // nil => PublicHost
	// ServerLAN is a Linux server's address on the user's LAN for the open
	// hint of local-only apps ("" on cloud servers); nil => none.
	ServerLAN      func() string
	HostMetrics    apiserver.HostProvider // nil => /proc (Linux)
	LANConsolePort int                    // 0 => publicconsole.DefaultLANPort

	// Setup wires the runtime setup (setup_wiring.go); nil => no /v1/setup.
	Setup *setupDeps
	// CheckPorts turns on the port check of the console's runtime page
	// (the Linux server; GET /v1/runtime/checks).
	CheckPorts bool
	// LANRelay lets acornfox serve relay <LAN>:P to the edge when Docker
	// cannot bind the LAN address (lanforward.go). Desktop only.
	LANRelay bool
}

// readOrCreateToken returns the hex token stored at path, creating it (0600)
// on first use.
func readOrCreateToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); len(t) >= 32 {
			return t, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	t := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}

// startServer opens the state store, wires the Caddy router, reconciler and
// API, serves until ctx is cancelled, and returns the process exit code.
// serverRunner is what the server needs from the runner: the runner API plus
// per-app network cleanup. *runner.Client (Linux) and *runner.Docker
// (desktop, in-process) both implement it.
type serverRunner interface {
	runner.API
	runner.NetworkRemover
}

func startServer(parent context.Context, o serverOptions, runnerAPI serverRunner, stdout, stderr io.Writer) int {
	ctx, stop := context.WithCancel(parent)
	defer stop()
	uploadDir := filepath.Join(o.DataDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o700); err != nil {
		fmt.Fprintln(stderr, "create upload dir:", err)
		return 1
	}

	st, err := state.Open(state.Config{
		Path:          filepath.Join(o.DataDir, "acornfox.db"),
		PublicPortMin: o.PublicPortMin,
		PublicPortMax: o.PublicPortMax,
	})
	if err != nil {
		if errors.Is(err, state.ErrLocked) {
			fmt.Fprintln(stderr, "open state: 另一个 acornfox 进程正在使用", o.DataDir)
		} else {
			fmt.Fprintln(stderr, "open state:", err)
		}
		return 1
	}
	defer st.Close()

	var (
		router   caddyroute.Router
		verifier caddyroute.Verifier
		edge     *reconcile.EdgeConfig
	)
	if o.EdgeMode != "" && o.EdgeMode != edgeModeOff {
		token, err := readOrCreateToken(filepath.Join(o.DataDir, edgeTokenFile))
		if err != nil {
			fmt.Fprintln(stderr, "caddy admin token:", err)
			return 1
		}
		admin := fmt.Sprintf("http://127.0.0.1:%d", o.EdgeAdminPort)
		router = caddyroute.New(admin, o.HTTPS, caddyroute.WithToken(token))
		verifier = caddyroute.NewVerifier(admin, o.HTTPS, caddyroute.WithToken(token))
		edge = &reconcile.EdgeConfig{Mode: o.EdgeMode, AdminPort: o.EdgeAdminPort, AdminToken: token,
			PortMin: o.PublicPortMin, PortMax: o.PublicPortMax, LANHost: o.LANHost}
	} else {
		router = caddyroute.New(o.CaddyAdmin, o.HTTPS)
		verifier = caddyroute.NewVerifier(o.CaddyAdmin, o.HTTPS)
	}

	// The public console's domain route rides along every route
	// sync; its configuration lives in the data directory.
	pcStore := publicconsole.NewFileStore(filepath.Join(o.DataDir, "public-console.json"))
	if o.PublicListen != "" {
		router = publicconsole.WrapRouter(router, pcStore, o.PublicListen)
	}

	// Old-version logs are a convenience: when the directory cannot be
	// opened the server still runs, it just keeps no history.
	deployLogs, err := deploylog.Open(filepath.Join(o.DataDir, "logs", "deployments"))
	if err != nil {
		fmt.Fprintln(stderr, "deployment logs disabled:", err)
		deployLogs = nil
	}

	rec := reconcile.New(reconcile.Config{
		Store:           st,
		DeployLogs:      deployLogs,
		Runner:          runnerAPI,
		Router:          router,
		UploadDir:       uploadDir,
		PublicHost:      o.PublicHost,
		HTTPS:           o.HTTPS,
		HTTPSCAFile:     o.HTTPSCAFile,
		Edge:            edge,
		DefaultExposure: o.DefaultExposure,
	})

	lanHost := o.LANHost
	if lanHost == nil {
		host := o.PublicHost
		lanHost = func() string { return host }
	}
	apiCfg := apiserver.Config{
		Store:           st,
		Kicker:          rec,
		Runner:          runnerAPI,
		UploadDir:       uploadDir,
		PublicHost:      o.PublicHost,
		DataDir:         o.DataDir,
		DeployLogs:      deployLogs,
		HostMetrics:     o.HostMetrics,
		DefaultExposure: o.DefaultExposure,
		Version:         cli.Version,
		Sources:         func() apiserver.DownloadSources { return downloadSources(o.DataDir) },
		ServerLAN:       o.ServerLAN,
	}
	// The home dashboard's host history samples with its own provider (the
	// /proc one computes CPU from its previous call).
	histSrc := o.HostMetrics
	if histSrc == nil {
		histSrc = apiserver.NewHostProvider(o.DataDir)
	}
	hist := apiserver.NewHostHistory(histSrc)
	go hist.Run(ctx)
	apiCfg.HostHistory = hist
	if o.CheckPorts {
		admin, lan := o.EdgeAdminPort, o.LANConsolePort
		if admin == 0 {
			admin = defaultEdgeAdminPort
		}
		if lan == 0 {
			lan = publicconsole.DefaultLANPort
		}
		apiCfg.CheckPorts = hostcheck.Ports{Admin: admin, LAN: lan, AppMin: o.PublicPortMin, AppMax: o.PublicPortMax}
	}
	appCenter := newAppCenter(st, o.DataDir, uploadDir, runnerAPI)
	defer appCenter.Close()
	apiCfg.AppCenter = appCenter
	var relay *lanForwarder
	if o.LANRelay && o.LANHost != nil && edge != nil && o.EdgeMode == runner.EdgeModeBridge {
		relay = &lanForwarder{lanHost: o.LANHost, active: rec.LANUnbindable,
			portMin: o.PublicPortMin, portMax: o.PublicPortMax, offset: reconcile.LANPortOffset,
			logf: func(f string, a ...any) {
				fmt.Fprintf(stdout, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(f, a...))
			}}
		go relay.Run(ctx)
	}
	if o.LANHost != nil {
		// When Docker could not bind the LAN address, apps have a LAN address
		// only while the relay listens on it.
		raw := o.LANHost
		apiCfg.LANHost = func() string {
			h := raw()
			if rec.LANUnbindable(h) && (relay == nil || !relay.Serving(h)) {
				return ""
			}
			return h
		}
	}
	if edge != nil {
		apiCfg.EdgeStatus = rec.EdgeStatus
	}
	var pcManager *publicconsole.Manager
	if o.PublicListen != "" {
		pcManager = publicconsole.NewManager(publicconsole.ManagerConfig{
			Store:    pcStore,
			Verifier: verifier,
			Sessions: st,
			Routes:   rec,
			Domains: func(ctx context.Context) ([]string, error) {
				ds, err := st.ListDomains(ctx, "")
				names := make([]string, 0, len(ds))
				for _, d := range ds {
					names = append(names, d.Name)
				}
				return names, err
			},
		})
		apiCfg.PublicConsole = pcManager
	}

	var lanRef atomic.Pointer[publicconsole.LANManager]
	if o.Setup != nil {
		deps := *o.Setup
		deps.Options = o
		deps.Relay = relay
		deps.LANEnabled = func() bool {
			lan := lanRef.Load()
			if lan == nil {
				return false
			}
			st, err := lan.Status(context.Background())
			return err == nil && st.Enabled
		}
		mgr := newSetupManager(deps, rec)
		apiCfg.Setup = mgr
		go mgr.Boot()
	}
	// The console handler shares the same dependencies plus the embedded
	// assets; the LAN console gateway wraps it.
	consoleCfg := apiCfg
	consoleCfg.ConsoleFS = consoleFS
	consoleHandler := apiserver.NewConsole(consoleCfg)

	lan := publicconsole.NewLANManager(publicconsole.LANConfig{
		Store:    publicconsole.NewFileStore(filepath.Join(o.DataDir, "lan-console.json")),
		Sessions: st,
		Console:  consoleHandler,
		Port:     o.LANConsolePort,
		Host:     lanHost,
	})
	apiCfg.LANConsole = lan
	lanRef.Store(lan)
	trustedHandler := apiserver.New(apiCfg)
	// Hooks are set by apiserver.New: now fail tasks a previous run left
	// running and start the catalog's daily sync.
	go appCenter.Start(ctx)
	defer lan.Close()
	if err := lan.Apply(); err != nil {
		fmt.Fprintln(stderr, "lan console:", err)
	}

	if pcManager != nil {
		pcManager.Cleanup(ctx)
		// Caddy keeps no config across restarts (persist_config off); while the
		// public console is on, put its route back even when no app has a round.
		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if cfg, err := pcStore.Load(); err == nil && cfg != nil {
						rec.RefreshRoutes(ctx)
					}
				}
			}
		}()
	}

	// Start the Caddy container before the first app round, and keep trying
	// while the runner or Docker is not reachable yet.
	if edge != nil {
		go func() {
			for !rec.EnsureEdge(ctx) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
				}
			}
		}()
	}

	// Run the reconciler.
	recDone := make(chan struct{})
	go func() {
		defer close(recDone)
		if err := rec.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintln(stderr, "reconciler stopped:", err)
		}
	}()
	// Open every trusted listener before serving.
	var listeners []net.Listener
	cleanup := func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}
	fail := func(what string, err error) int {
		fmt.Fprintln(stderr, what, err)
		cleanup()
		stop()
		<-recDone
		return 1
	}
	for _, addr := range o.Listens {
		ln, err := listenAPI(addr, o.ListenGID)
		if err != nil {
			return fail("listen:", err)
		}
		listeners = append(listeners, ln)
	}

	trustedSrv := &http.Server{Handler: trustedHandler}
	serveErr := make(chan error, len(listeners)+2)
	for _, ln := range listeners {
		go func(l net.Listener) { serveErr <- trustedSrv.Serve(l) }(ln)
	}
	for _, addr := range o.Listens {
		fmt.Fprintln(stdout, "acornfox server (trusted) listening on", addr)
	}

	// Open the console listener (loopback TCP) when enabled.
	var consoleSrv *http.Server
	if o.ConsoleListen != "" {
		cln, err := net.Listen("tcp", o.ConsoleListen)
		if err != nil {
			return fail("console listen:", err)
		}
		listeners = append(listeners, cln)
		consoleSrv = &http.Server{Handler: consoleHandler}
		go func() { serveErr <- consoleSrv.Serve(cln) }()
		fmt.Fprintln(stdout, "acornfox server (console) listening on", o.ConsoleListen)
	}

	// The public console gateway (loopback; Caddy terminates TLS in front).
	var publicSrv *http.Server
	if pcManager != nil {
		pln, err := net.Listen("tcp", o.PublicListen)
		if err != nil {
			return fail("public console listen:", err)
		}
		listeners = append(listeners, pln)
		publicSrv = &http.Server{
			Handler:           publicconsole.NewGateway(publicconsole.GatewayConfig{Store: pcStore, Sessions: st, Console: consoleHandler}),
			ReadHeaderTimeout: 10 * time.Second,
			MaxHeaderBytes:    64 << 10,
		}
		go func() { serveErr <- publicSrv.Serve(pln) }()
		fmt.Fprintln(stdout, "acornfox server (public console gateway) listening on", o.PublicListen)
	}

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "serve:", err)
		}
	}

	// Graceful shutdown.
	shCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = trustedSrv.Shutdown(shCtx)
	if consoleSrv != nil {
		_ = consoleSrv.Shutdown(shCtx)
	}
	if publicSrv != nil {
		_ = publicSrv.Shutdown(shCtx)
	}
	stop()
	<-recDone
	return 0
}
