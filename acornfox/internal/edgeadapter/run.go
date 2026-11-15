package edgeadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/edgeadapter/caddyroute"
)

// Defaults of Options inside the container.
const (
	DefaultRuntimeDir = "/tmp/afx-edge"
	DefaultProxyBin   = "/usr/bin/caddy"
	// proxySocket and bootstrapFile live in the runtime directory.
	proxySocket   = "proxy-admin.sock"
	bootstrapFile = "bootstrap.json"
	runFile       = "run.json"
)

// Options configures Run. Zero values take the container defaults.
type Options struct {
	// AdminListen is the adapter's own management endpoint: 127.0.0.1:<port>
	// in host mode, ":2020" in bridge mode. Required.
	AdminListen string
	TokenFile   string // default edge.TokenFile
	StateDir    string // default edge.StateDir
	RuntimeDir  string // default DefaultRuntimeDir
	ProxyBin    string // default DefaultProxyBin
	// StartTimeout bounds the wait for the proxy's management socket.
	StartTimeout time.Duration // default 30s
	// Grace is how long the proxy lets running requests finish on stop.
	Grace time.Duration // default edge.GracePeriodSeconds
	// ProxyEnv overrides the environment of the proxy process (tests);
	// nil = the adapter's own environment.
	ProxyEnv []string
	Stdout   io.Writer // proxy output; default os.Stdout
	Stderr   io.Writer // proxy and adapter logs; default os.Stderr
	// CheckDefaultAdmin turns on the "proxy default admin port is closed"
	// health check (always on in the container; tests on a busy machine
	// turn it off).
	CheckDefaultAdmin bool
}

func (o *Options) defaults() {
	if o.TokenFile == "" {
		o.TokenFile = edge.TokenFile
	}
	if o.StateDir == "" {
		o.StateDir = edge.StateDir
	}
	if o.RuntimeDir == "" {
		o.RuntimeDir = DefaultRuntimeDir
	}
	if o.ProxyBin == "" {
		o.ProxyBin = DefaultProxyBin
	}
	if o.StartTimeout == 0 {
		o.StartTimeout = 30 * time.Second
	}
	if o.Grace == 0 {
		o.Grace = edge.GracePeriodSeconds * time.Second
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
}

// RunState is written to the runtime directory once the proxy runs; the
// health check reads it to know which processes are the ones of this
// container.
type RunState struct {
	AdapterPID  int    `json:"adapter_pid"`
	ProxyPID    int    `json:"proxy_pid"`
	AdminListen string `json:"admin_listen"`
	Version     string `json:"image_version"`
	Started     string `json:"started"`
}

var hexToken = regexp.MustCompile(`^[0-9a-f]{32,128}$`)

// ReadToken reads and checks the management token file.
func ReadToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读不到管理令牌 %s：%w", path, err)
	}
	t := strings.TrimSpace(string(b))
	if !hexToken.MatchString(t) {
		return "", fmt.Errorf("管理令牌 %s 格式不对（要 32～128 位十六进制）", path)
	}
	return t, nil
}

// Bootstrap is the proxy's start configuration: the whole configuration of
// an empty edge.Config (caddyroute.Render), that is its own management API on
// a container-private unix socket (never a TCP port), nothing persisted, the
// grace period and no routes. The stored configuration follows in one load.
func Bootstrap(socket string, grace time.Duration) []byte {
	b, _ := caddyroute.Render(caddyroute.Base{AdminSocket: socket, Grace: grace}, edge.Config{}, nil)
	return b
}

// proxyBackend is the proxy driver as the server's backend: every Apply loads
// the whole configuration in one step (caddyroute.Replace).
type proxyBackend struct {
	*caddyroute.Driver
	base caddyroute.Base
}

func (p proxyBackend) Apply(ctx context.Context, cfg edge.Config) (string, error) {
	if err := p.Replace(ctx, p.base, cfg); err != nil {
		return "", err
	}
	return cfg.Version(), nil
}

// Run is the adapter's main loop. It binds the management endpoint first (a
// second container on the same endpoint fails here, before its proxy takes
// any port), starts the proxy, waits for its management socket, applies the
// stored configuration and serves the protocol until ctx ends (stop signal)
// or the proxy exits. On ctx end it stops the proxy gracefully (its grace
// period) and returns nil; an unexpected proxy exit returns an error so the
// container exits and Docker restarts it.
func Run(ctx context.Context, o Options) error {
	o.defaults()
	log := slog.New(slog.NewTextHandler(o.Stderr, nil)).With("component", "afx-edge")
	if o.AdminListen == "" {
		return errors.New("缺少 --admin-listen")
	}
	token, err := ReadToken(o.TokenFile)
	if err != nil {
		return err
	}
	// Plain listen, no port sharing: two adapters can never both hold one
	// management endpoint, so a request always reaches the container it
	// was meant for.
	ln, err := net.Listen("tcp", o.AdminListen)
	if err != nil {
		return fmt.Errorf("管理入口 %s 打不开（是不是另一个入口容器还在用它）：%w", o.AdminListen, err)
	}
	defer ln.Close()

	if err := os.MkdirAll(o.RuntimeDir, 0o700); err != nil {
		return err
	}
	sock := filepath.Join(o.RuntimeDir, proxySocket)
	_ = os.Remove(sock)
	boot := filepath.Join(o.RuntimeDir, bootstrapFile)
	if err := os.WriteFile(boot, Bootstrap(sock, o.Grace), 0o600); err != nil {
		return err
	}

	cmd := exec.Command(o.ProxyBin, "run", "--config", boot)
	cmd.Stdout, cmd.Stderr = o.Stdout, o.Stderr
	if o.ProxyEnv != nil {
		cmd.Env = o.ProxyEnv
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("代理程序 %s 起不来：%w", o.ProxyBin, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	stopProxy := func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-exited:
		case <-time.After(o.Grace + 2*time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
	}

	driver := caddyroute.New("unix:"+sock, edge.HTTPS{})
	backend := proxyBackend{Driver: driver, base: caddyroute.Base{AdminSocket: sock, Grace: o.Grace}}
	srv := NewServer(backend, token, NewStore(o.StateDir), log)
	if o.CheckDefaultAdmin {
		srv.exposed = caddyroute.DefaultAdminOpen
	}
	hs := &http.Server{Handler: srv, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- hs.Serve(ln) }()
	shutdown := func() {
		sctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = hs.Shutdown(sctx)
		cancel()
	}

	// Wait for the proxy's management socket.
	deadline := time.Now().Add(o.StartTimeout)
	for {
		hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		h := driver.Health(hctx)
		cancel()
		if h.OK() {
			break
		}
		select {
		case err := <-exited:
			shutdown()
			return fmt.Errorf("代理程序启动时退出了：%v", err)
		case <-ctx.Done():
			shutdown()
			stopProxy()
			return nil
		case <-time.After(100 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			shutdown()
			stopProxy()
			return fmt.Errorf("代理程序 %s 内没有应答：%s", o.StartTimeout, h.Reason)
		}
	}
	writeRunState(o, cmd.Process.Pid, log)
	srv.Load(ctx)
	log.Info("ready", "admin", o.AdminListen, "image", edge.ImageVersion)

	select {
	case <-ctx.Done():
		log.Info("stopping: letting running requests finish", "grace", o.Grace.String())
		shutdown()
		stopProxy()
		return nil
	case err := <-exited:
		shutdown()
		return fmt.Errorf("%w：%v", errStopped, err)
	case err := <-served:
		stopProxy()
		return fmt.Errorf("管理入口停了：%w", err)
	}
}

func writeRunState(o Options, proxyPID int, log *slog.Logger) {
	st := RunState{AdapterPID: os.Getpid(), ProxyPID: proxyPID, AdminListen: o.AdminListen, Version: edge.ImageVersion,
		Started: time.Now().UTC().Format(time.RFC3339)}
	b, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(o.RuntimeDir, runFile), b, 0o600); err != nil {
		log.Warn("write run state", "err", err)
	}
}

// ReadRunState reads the run state written by Run.
func ReadRunState(runtimeDir string) (RunState, error) {
	var st RunState
	b, err := os.ReadFile(filepath.Join(runtimeDir, runFile))
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(b, &st)
	return st, err
}
