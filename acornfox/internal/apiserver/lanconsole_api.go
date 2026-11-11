package apiserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/acornfox/acornfox/internal/publicconsole"
)

// LANConsole opens the console on the local network. The user sets the
// password in the browser on the first visit and can have a browser
// remembered for 90 days; only local-network clients are served. See
// publicconsole.LANManager for the rules.
//
// Which entry may do what (写死在路由里，不靠调用方自觉):
//   - trusted entry (unix socket, i.e. someone with SSH access): everything.
//   - console entry (a logged-in browser): read the status, list remembered
//     devices, remove devices. It cannot open or close the LAN console and
//     cannot clear the password.
type LANConsole interface {
	Status(ctx context.Context) (LANConsoleStatus, error)
	// Enable starts the LAN listener; the returned password is always empty
	// (kept for older clients). reset also clears the password, sessions and
	// remembered devices.
	Enable(ctx context.Context, reset bool) (LANConsoleStatus, string, error)
	Disable(ctx context.Context) error
	// ResetPassword clears the password, sessions and remembered devices of
	// an open LAN console.
	ResetPassword(ctx context.Context) (LANConsoleStatus, error)
	Devices(ctx context.Context) ([]publicconsole.DeviceView, error)
	RemoveDevice(ctx context.Context, id string) (bool, error)
	RemoveDevices(ctx context.Context) (int, error)
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
		s.lanConsoleError(w, err, http.StatusConflict, "lan_console_failed")
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

// lanConsoleError writes a classified publicconsole.Error as it is and
// anything else with the fallback status and code.
func (s *server) lanConsoleError(w http.ResponseWriter, err error, status int, code string) {
	var pe *publicconsole.Error
	if errors.As(err, &pe) {
		writeError(w, pe.Status, pe.Code, pe.Message)
		return
	}
	writeError(w, status, code, err.Error())
}

// postLANConsoleResetPassword implements POST /v1/lan-console/reset-password
// (trusted entry only): clear the password, sessions and remembered devices.
// Idempotent. Errors: 409 lan_console_off when the LAN console is closed.
func (s *server) postLANConsoleResetPassword(w http.ResponseWriter, r *http.Request) {
	if s.lanConsole == nil {
		writeError(w, http.StatusNotFound, "not_found", "此版本不支持局域网控制台")
		return
	}
	st, err := s.lanConsole.ResetPassword(r.Context())
	if err != nil {
		s.lanConsoleError(w, err, http.StatusInternalServerError, "internal")
		return
	}
	s.log.Info("lan console: password cleared")
	writeJSON(w, http.StatusOK, map[string]any{"status": st})
}

// getLANConsoleDevices implements GET /v1/lan-console/devices (read-only).
func (s *server) getLANConsoleDevices(w http.ResponseWriter, r *http.Request) {
	if s.lanConsole == nil {
		writeError(w, http.StatusNotFound, "not_found", "此版本不支持局域网控制台")
		return
	}
	ds, err := s.lanConsole.Devices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": ds})
}

// deleteLANConsoleDevice implements DELETE /v1/lan-console/devices/{id}:
// forget one remembered device and end its sessions. Idempotent: removed
// says whether it existed.
func (s *server) deleteLANConsoleDevice(w http.ResponseWriter, r *http.Request) {
	if s.lanConsole == nil {
		writeError(w, http.StatusNotFound, "not_found", "此版本不支持局域网控制台")
		return
	}
	id := r.PathValue("id")
	if len(id) == 0 || len(id) > 32 {
		writeError(w, http.StatusBadRequest, "invalid_device", "设备编号不合法")
		return
	}
	found, err := s.lanConsole.RemoveDevice(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.log.Info("lan console: remembered device removed", "device", id, "existed", found)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": found})
}

// deleteLANConsoleDevices implements DELETE /v1/lan-console/devices: forget
// every remembered device and end their sessions. Idempotent.
func (s *server) deleteLANConsoleDevices(w http.ResponseWriter, r *http.Request) {
	if s.lanConsole == nil {
		writeError(w, http.StatusNotFound, "not_found", "此版本不支持局域网控制台")
		return
	}
	n, err := s.lanConsole.RemoveDevices(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	s.log.Info("lan console: all remembered devices removed", "count", n)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "removed": n})
}
