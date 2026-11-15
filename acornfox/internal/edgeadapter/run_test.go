package edgeadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/edgeclient"
)

// The test binary doubles as a fake proxy program: with AFX_FAKE_PROXY=1 it
// reads the bootstrap file Run wrote, serves an empty management API on the
// unix socket named there, exits 0 on SIGTERM (writing a marker file) and,
// with AFX_FAKE_PROXY_DIE_MS, exits 3 on its own after that long.
func TestMain(m *testing.M) {
	if os.Getenv("AFX_FAKE_PROXY") == "1" {
		os.Exit(fakeProxy(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func fakeProxy(args []string) int {
	if len(args) != 3 || args[0] != "run" || args[1] != "--config" {
		fmt.Fprintln(os.Stderr, "fake proxy: bad args", args)
		return 2
	}
	b, err := os.ReadFile(args[2])
	if err != nil {
		return 2
	}
	var cfg struct {
		Admin struct {
			Listen string `json:"listen"`
		} `json:"admin"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return 2
	}
	sock := strings.TrimPrefix(cfg.Admin.Listen, "unix/")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		return 2
	}
	go func() {
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/config/apps/http/servers" {
				_, _ = w.Write([]byte("{}"))
				return
			}
			_, _ = w.Write([]byte("{}"))
		}))
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	var die <-chan time.Time
	if ms, _ := strconv.Atoi(os.Getenv("AFX_FAKE_PROXY_DIE_MS")); ms > 0 {
		die = time.After(time.Duration(ms) * time.Millisecond)
	}
	select {
	case <-sig:
		if m := os.Getenv("AFX_FAKE_PROXY_MARK"); m != "" {
			_ = os.WriteFile(m, []byte("sigterm"), 0o600)
		}
		return 0
	case <-die:
		return 3
	}
}

type runEnv struct {
	opts   Options
	client *edgeclient.Driver
	mark   string
}

// newRunEnv prepares Options with a short runtime dir (unix socket paths are
// limited to about 100 bytes) and the test binary as the proxy.
func newRunEnv(t *testing.T, extraEnv ...string) runEnv {
	t.Helper()
	rt, err := os.MkdirTemp("/tmp", "afxe-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(rt) })
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	mark := filepath.Join(dir, "mark")
	env := append(os.Environ(), "AFX_FAKE_PROXY=1", "AFX_FAKE_PROXY_MARK="+mark)
	env = append(env, extraEnv...)
	return runEnv{
		opts: Options{AdminListen: addr, TokenFile: tokenFile, StateDir: filepath.Join(dir, "state"), RuntimeDir: rt,
			ProxyBin: self, ProxyEnv: env, StartTimeout: 10 * time.Second, Grace: time.Second},
		client: edgeclient.New("http://"+addr, testToken),
		mark:   mark,
	}
}

func waitHealthy(t *testing.T, c *edgeclient.Driver) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if h := c.Health(context.Background()); h.OK() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("adapter never became healthy: %+v", c.Health(context.Background()))
}

// Run serves the protocol once the proxy answers, writes the run state, and
// on a stop signal stops the proxy with SIGTERM (graceful) and returns nil.
func TestRunStartsAndStopsGracefully(t *testing.T) {
	e := newRunEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, e.opts) }()
	waitHealthy(t, e.client)

	st, err := ReadRunState(e.opts.RuntimeDir)
	if err != nil || st.ProxyPID <= 0 || st.AdapterPID != os.Getpid() || st.AdminListen != e.opts.AdminListen || st.Version != edge.ImageVersion {
		t.Fatalf("run state %+v %v", st, err)
	}
	// A second adapter on the same management endpoint fails before it
	// starts any proxy.
	e2 := e.opts
	e2.RuntimeDir = e.opts.RuntimeDir + "-2"
	t.Cleanup(func() { os.RemoveAll(e2.RuntimeDir) })
	if err := Run(context.Background(), e2); err == nil || !strings.Contains(err.Error(), "管理入口") {
		t.Fatalf("second adapter: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e2.RuntimeDir, bootstrapFile)); err == nil {
		t.Fatal("the second adapter went on after its endpoint was taken")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after stop = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after the stop signal")
	}
	if b, _ := os.ReadFile(e.mark); string(b) != "sigterm" {
		t.Fatal("the proxy was not stopped with SIGTERM")
	}
	if h := e.client.Health(context.Background()); h.Cause != edge.CauseUnreachable {
		t.Fatalf("health after stop = %+v", h)
	}
}

// A proxy that exits on its own ends Run with an error (the container exits
// and Docker restarts it).
func TestRunEndsWhenProxyExits(t *testing.T) {
	e := newRunEnv(t, "AFX_FAKE_PROXY_DIE_MS=500")
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), e.opts) }()
	select {
	case err := <-done:
		if !errors.Is(err, errStopped) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not notice the proxy exited")
	}
}

func TestRunRefusesBadToken(t *testing.T) {
	e := newRunEnv(t)
	if err := os.WriteFile(e.opts.TokenFile, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), e.opts); err == nil || !strings.Contains(err.Error(), "令牌") {
		t.Fatalf("err = %v", err)
	}
}
