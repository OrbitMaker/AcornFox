package publicconsole

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DefaultLANPort is where the console listens on the LAN when opened. The
// loopback console keeps 18800; a second port avoids binding 0.0.0.0 over it.
const DefaultLANPort = 18802

// LANConfig wires LANManager.
type LANConfig struct {
	Store    *FileStore   // lan-console.json in the data directory
	Sessions Sessions     // the console session store
	Console  http.Handler // the native console handler
	Port     int          // 0 => DefaultLANPort
	Host     func() string
	Logger   *slog.Logger
}

// LANManager opens and closes the console on the local network
// (three-platform architecture: off by default, password login, rate-limited
// like the public console). It implements apiserver.LANConsole.
type LANManager struct {
	cfg     LANConfig
	handler http.Handler

	mu  sync.Mutex
	srv *http.Server
}

// NewLANManager returns a manager; call Apply once at startup.
func NewLANManager(cfg LANConfig) *LANManager {
	if cfg.Port == 0 {
		cfg.Port = DefaultLANPort
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &LANManager{cfg: cfg, handler: NewGateway(GatewayConfig{
		Store: cfg.Store, Sessions: cfg.Sessions, Console: cfg.Console, LAN: true, Logger: cfg.Logger,
	})}
}

// Apply starts the listener when the LAN console is enabled and stops it
// otherwise.
func (m *LANManager) Apply() error {
	c, err := m.cfg.Store.Load()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c == nil {
		if m.srv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = m.srv.Shutdown(ctx)
			cancel()
			m.srv = nil
		}
		return nil
	}
	if m.srv != nil {
		return nil
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(m.cfg.Port)))
	if err != nil {
		return fmt.Errorf("局域网控制台监听 %d 端口失败：%w", m.cfg.Port, err)
	}
	srv := &http.Server{Handler: m.handler, ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 64 << 10}
	m.srv = srv
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.cfg.Logger.Error("lan console", "err", err)
		}
	}()
	m.cfg.Logger.Info("lan console listening", "port", m.cfg.Port)
	return nil
}

// Close stops the listener (process shutdown).
func (m *LANManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.srv != nil {
		_ = m.srv.Close()
		m.srv = nil
	}
}

func (m *LANManager) url() string {
	h := ""
	if m.cfg.Host != nil {
		h = m.cfg.Host()
	}
	if h == "" {
		return ""
	}
	return "http://" + net.JoinHostPort(h, strconv.Itoa(m.cfg.Port))
}

// LANStatus is the wire shape of GET /v1/lan-console.
type LANStatus struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url,omitempty"` // http://<LAN address>:<port>
	Port    int    `json:"port"`
}

// Status implements apiserver.LANConsole.
func (m *LANManager) Status(context.Context) (LANStatus, error) {
	c, err := m.cfg.Store.Load()
	if err != nil {
		return LANStatus{}, err
	}
	st := LANStatus{Enabled: c != nil, Port: m.cfg.Port}
	if c != nil {
		st.URL = m.url()
	}
	return st, nil
}

// Enable implements apiserver.LANConsole. The password is returned only when
// it is generated (first enable or reset); enabling again keeps it.
func (m *LANManager) Enable(ctx context.Context, reset bool) (LANStatus, string, error) {
	c, err := m.cfg.Store.Load()
	if err != nil {
		return LANStatus{}, "", err
	}
	password := ""
	if c == nil || reset {
		password, err = GeneratePassword()
		if err != nil {
			return LANStatus{}, "", err
		}
		// A new password ends every session issued under the old one.
		if err := m.cfg.Store.Save(&Config{PasswordSHA256: Digest(password), EnabledAt: time.Now().UTC()}); err != nil {
			return LANStatus{}, "", err
		}
	}
	if err := m.Apply(); err != nil {
		if c == nil {
			_ = m.cfg.Store.Delete()
		}
		return LANStatus{}, "", err
	}
	st, err := m.Status(ctx)
	return st, password, err
}

// Disable implements apiserver.LANConsole.
func (m *LANManager) Disable(context.Context) error {
	if err := m.cfg.Store.Delete(); err != nil {
		return err
	}
	return m.Apply()
}
