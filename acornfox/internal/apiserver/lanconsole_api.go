package apiserver

import (
	"context"
	"net/http"

	"github.com/acornfox/acornfox/internal/publicconsole"
)

// LANConsole opens the console on the local network (three-platform
// architecture: the console listens on loopback only by default; opening it
// on the LAN is a manual step and requires a password login).
type LANConsole interface {
	Status(ctx context.Context) (LANConsoleStatus, error)
	// Enable starts the LAN listener and returns the generated password once.
	// reset generates a new password even when already enabled.
	Enable(ctx context.Context, reset bool) (LANConsoleStatus, string, error)
	Disable(ctx context.Context) error
}

// LANConsoleStatus is the wire shape of GET /v1/lan-console.
type LANConsoleStatus = publicconsole.LANStatus

var _ LANConsole = (*publicconsole.LANManager)(nil)

func (s *server) getLANConsole(w http.ResponseWriter, r *http.Request) {
	if s.lanConsole == nil {
		writeError(w, http.StatusNotFound, "not_found", "此版本不支持局域网控制台")
		return
	}
	st, err := s.lanConsole.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *server) postLANConsole(w http.ResponseWriter, r *http.Request) {
	if s.lanConsole == nil {
		writeError(w, http.StatusNotFound, "not_found", "此版本不支持局域网控制台")
		return
	}
	var body struct {
		ResetPassword bool `json:"reset_password"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", "请求体不是合法 JSON")
			return
		}
	}
	st, password, err := s.lanConsole.Enable(r.Context(), body.ResetPassword)
	if err != nil {
		writeError(w, http.StatusConflict, "lan_console_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": st, "password": password})
}

func (s *server) deleteLANConsole(w http.ResponseWriter, r *http.Request) {
	if s.lanConsole == nil {
		writeError(w, http.StatusNotFound, "not_found", "此版本不支持局域网控制台")
		return
	}
	if err := s.lanConsole.Disable(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
