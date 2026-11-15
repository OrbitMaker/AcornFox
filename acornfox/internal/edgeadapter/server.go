// Package edgeadapter is the adapter inside the entry proxy container
// (cmd/afx-edge). It is the container's first process: it starts the
// upstream proxy software (Caddy) and supervises it, serves the management
// protocol of edge/wire.go on its own endpoint, translates each configuration
// into the proxy's settings (package caddyroute), keeps the last applied
// configuration on the settings volume and applies it again whenever the
// container starts, without waiting for the acornfox program.
//
// The acornfox program never links this package (guard test in package
// edge); it reaches the adapter through edgeclient only.
package edgeadapter

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// Retries of the startup load of the stored configuration.
var (
	loadAttempts   = 3
	loadRetryPause = 300 * time.Millisecond
)

// maxBody bounds a request body (a configuration of maxRoutes routes is far
// below it).
const maxBody = 4 << 20

// Server serves the management protocol in front of a backend (the proxy
// driver). Apply and the startup load are serialized; a configuration is
// saved only after the proxy accepted it, so the stored one always loads.
type Server struct {
	backend edge.Proxy
	token   string
	store   *Store
	log     *slog.Logger
	// exposed reports an unprotected management endpoint of the proxy
	// software itself (its default admin port); nil = no such check.
	exposed func() bool

	applyMu sync.Mutex

	mu       sync.Mutex
	starting bool
	loadErr  string
}

// NewServer returns a server that starts in the "starting" state until Load
// has run.
func NewServer(backend edge.Proxy, token string, store *Store, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Server{backend: backend, token: token, store: store, log: log, starting: true}
}

// Load applies the stored configuration (if any) and ends the starting state.
// A configuration that no longer applies (for example a port another program
// took meanwhile) is reported by Health until the next successful Apply.
func (s *Server) Load(ctx context.Context) {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	cfg, ok, err := s.store.Load()
	msg := ""
	switch {
	case err != nil:
		msg = "保存的设置读不出来：" + err.Error()
	case ok:
		// A proxy that has only just come up may still drop the first
		// request; try a few times before reporting.
		var aerr error
		for i := 0; i < loadAttempts; i++ {
			if i > 0 {
				select {
				case <-ctx.Done():
				case <-time.After(loadRetryPause):
				}
			}
			if _, aerr = s.backend.Apply(ctx, cfg); aerr == nil || ctx.Err() != nil {
				break
			}
			s.log.Warn("stored configuration did not apply, retrying", "attempt", i+1, "err", aerr)
		}
		if aerr != nil {
			msg = "上次保存的设置没能装上：" + aerr.Error()
		} else {
			s.log.Info("applied the stored configuration", "version", cfg.Version(), "routes", len(cfg.Routes))
		}
	default:
		s.log.Info("no stored configuration yet; waiting for the first one")
	}
	if msg != "" {
		s.log.Error("startup configuration", "err", msg)
	}
	s.mu.Lock()
	s.starting, s.loadErr = false, msg
	s.mu.Unlock()
}

func (s *Server) isStarting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starting
}

// ServeHTTP implements the protocol of edge/wire.go.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get(edge.TokenHeader)
	if s.token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
		writeErr(w, http.StatusUnauthorized, "要令牌")
		return
	}
	switch {
	case r.URL.Path == edge.PathConfig && r.Method == http.MethodPut:
		s.handleApply(w, r)
	case r.URL.Path == edge.PathVersion && r.Method == http.MethodGet:
		s.handleVersion(w, r)
	case r.URL.Path == edge.PathChallenge && r.Method == http.MethodPut:
		s.handleSetChallenge(w, r)
	case r.URL.Path == edge.PathChallenge && r.Method == http.MethodDelete:
		if err := s.backend.ClearChallenge(r.Context()); err != nil {
			writeErr(w, http.StatusBadGateway, "清除验证内容失败："+err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == edge.PathHealth && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, s.Health(r.Context()))
	default:
		writeErr(w, http.StatusNotFound, "没有这个接口")
	}
}

func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	var cfg edge.Config
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		writeErr(w, http.StatusBadRequest, "设置格式不对："+err.Error())
		return
	}
	if err := Validate(cfg); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Apply waits for the startup load (same lock), so a configuration the
	// program sends right after the container started wins over the stored
	// one.
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	v, err := s.backend.Apply(r.Context(), cfg)
	if err != nil {
		s.log.Error("apply", "err", err)
		writeErr(w, http.StatusBadGateway, "入口代理没有装上这份设置："+err.Error())
		return
	}
	if err := s.store.Save(cfg); err != nil {
		// The routes are live; only the restart copy is missing. Say so: the
		// program applies again when Version shows a restart lost them.
		s.log.Error("save configuration", "err", err)
		writeErr(w, http.StatusInternalServerError, "设置已生效，但没能保存到设置卷："+err.Error())
		return
	}
	s.mu.Lock()
	s.loadErr = ""
	s.mu.Unlock()
	s.log.Info("applied", "version", v, "routes", len(cfg.Routes))
	writeJSON(w, http.StatusOK, edge.VersionReply{Version: v})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if s.isStarting() {
		writeErr(w, http.StatusServiceUnavailable, "入口代理正在启动")
		return
	}
	v, err := s.backend.Version(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "读不到入口代理现在的设置："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, edge.VersionReply{Version: v})
}

func (s *Server) handleSetChallenge(w http.ResponseWriter, r *http.Request) {
	var req edge.ChallengeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "格式不对："+err.Error())
		return
	}
	if !validDomain(req.Host) || len(req.Path) == 0 || req.Path[0] != '/' || len(req.Path) > 512 {
		writeErr(w, http.StatusBadRequest, "验证内容的域名或路径不合法")
		return
	}
	if err := s.backend.SetChallenge(r.Context(), req.Host, req.Path, req.Body); err != nil {
		writeErr(w, http.StatusBadGateway, "放验证内容失败："+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Health is the adapter's own view: starting until the stored configuration
// was loaded, an error when the proxy's management socket does not answer,
// when the proxy's default management port is open, or when the stored
// configuration did not apply at startup.
func (s *Server) Health(ctx context.Context) edge.Health {
	s.mu.Lock()
	starting, loadErr := s.starting, s.loadErr
	s.mu.Unlock()
	if starting {
		return edge.Health{State: edge.StateStarting, Reason: "入口代理正在启动"}
	}
	if h := s.backend.Health(ctx); !h.OK() {
		return edge.Health{State: edge.StateError, Cause: edge.CauseOther, Reason: "入口代理里的代理程序没有应答：" + h.Reason}
	}
	if s.exposed != nil && s.exposed() {
		return edge.Health{State: edge.StateError, Cause: edge.CauseExposed, Reason: "代理程序的默认管理口在这台机器上开着"}
	}
	if loadErr != "" {
		return edge.Health{State: edge.StateError, Cause: edge.CauseOther, Reason: loadErr}
	}
	return edge.Health{State: edge.StateRunning}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, edge.ErrorReply{Error: msg})
}

// errStopped marks a supervised proxy that exited on its own.
var errStopped = errors.New("代理程序自己退出了")
