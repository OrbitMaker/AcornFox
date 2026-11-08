package apiserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/state"
)

// App center: browse the catalog and install from it (internal/appinstall).
//
//	GET  /v1/catalog                     apps and recipes of the public and the connected
//	                                     private catalogs (each with its source), serial, last sync
//	GET  /v1/catalog/apps/{id}           detail and install form (no hidden params); ?catalog=public|cat-…
//	POST /v1/catalog/sync                sync now (public, then private catalogs)
//	POST /v1/catalog/connect             connect a private catalog with a code {"code"} (W10)
//	GET  /v1/catalog/sources             public and private catalogs: serial, last sync, disconnected
//	GET  /v1/catalog/platform-vars       platform variables apps can use, with this machine's values
//	POST /v1/catalog/install             start an install task (202); "platform" changes user_can_set variables
//	GET  /v1/catalog/install             recent tasks
//	GET  /v1/catalog/install/{task}      one task's progress
//	GET  /v1/apps/{app}/first-login      first-login info; ?reveal=true shows secrets
//
// Errors carry the CAT-INST-* or CAT-CONN-* code:
// {"error":{"code","message","hint","task_id","gateway_code","retry_after"}}.
// The connection code is never logged: it only travels in the request body.
// All routes sit on the trusted socket and on the console entry, which
// requires a logged-in session; nothing here is reachable anonymously.

func (s *server) registerCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/catalog", s.catalogList)
	mux.HandleFunc("GET /v1/catalog/apps/{id}", s.catalogApp)
	mux.HandleFunc("POST /v1/catalog/sync", s.catalogSync)
	mux.HandleFunc("POST /v1/catalog/connect", s.catalogConnect)
	mux.HandleFunc("GET /v1/catalog/sources", s.catalogSources)
	mux.HandleFunc("GET /v1/catalog/platform-vars", s.catalogPlatformVars)
	mux.HandleFunc("POST /v1/catalog/install", s.catalogInstall)
	mux.HandleFunc("GET /v1/catalog/install", s.catalogTasks)
	mux.HandleFunc("GET /v1/catalog/install/{task}", s.catalogTask)
	mux.HandleFunc("GET /v1/apps/{app}/first-login", s.appFirstLogin)
}

// wireAppCenter gives the engine the server's own operations.
func (s *server) wireAppCenter() {
	if s.appCenter == nil {
		return
	}
	s.appCenter.SetHooks(appinstall.Hooks{
		Reserve: s.reserveCatalogApp,
		Delete:  s.removeApp,
		WithApp: s.withApp,
		Kick:    s.kicker.Kick,
		URL:     s.catalogAppURL,
	})
}

// reserveCatalogApp creates a new app with memMB under the memory cap, in one
// step under resMu. It refuses an existing app (state.ErrConflict).
func (s *server) reserveCatalogApp(ctx context.Context, name string, memMB int) (state.App, error) {
	s.resMu.Lock()
	defer s.resMu.Unlock()
	if _, err := s.store.GetApp(ctx, name); err == nil {
		return state.App{}, state.ErrConflict
	} else if !errors.Is(err, state.ErrNotFound) {
		return state.App{}, err
	}
	v, err := s.resources(ctx)
	if err != nil {
		return state.App{}, err
	}
	if v.Memory.CapMB > 0 && v.Memory.AllocatedMB+memMB > v.Memory.CapMB {
		return state.App{}, &appinstall.RoomError{NeedMB: memMB, CapMB: v.Memory.CapMB, AllocatedMB: v.Memory.AllocatedMB}
	}
	a, created, err := s.store.EnsureApp(ctx, name)
	if err != nil {
		return state.App{}, err
	}
	if !created {
		return state.App{}, state.ErrConflict
	}
	return s.store.UpdateApp(ctx, a.Name, func(x *state.App) error {
		x.MemoryMB = memMB
		return nil
	})
}

type catalogErrorBody struct {
	Error struct {
		Code        string `json:"code"`
		Message     string `json:"message"`
		Hint        string `json:"hint,omitempty"`
		TaskID      string `json:"task_id,omitempty"`
		GatewayCode string `json:"gateway_code,omitempty"`
		RetryAfter  int    `json:"retry_after,omitempty"`
		Diagnosis   any    `json:"diagnosis,omitempty"`
	} `json:"error"`
}

func writeFault(w http.ResponseWriter, err error) {
	f := appinstall.AsFault(err)
	var b catalogErrorBody
	b.Error.Code, b.Error.Message, b.Error.Hint, b.Error.TaskID, b.Error.Diagnosis = f.Code, f.Message, f.Hint, f.TaskID, f.Diagnosis
	b.Error.GatewayCode, b.Error.RetryAfter = f.GatewayCode, f.RetryAfter
	if f.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(f.RetryAfter))
	}
	writeJSON(w, f.HTTPStatus(), b)
}

func (s *server) appCenterOn(w http.ResponseWriter) bool {
	if s.appCenter == nil {
		writeError(w, http.StatusNotFound, "app_center_unavailable", "这台服务器没有启用应用中心")
		return false
	}
	return true
}

func (s *server) catalogList(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.List(r.Context())
	if err != nil {
		writeFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) catalogApp(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.DetailIn(r.Context(), r.URL.Query().Get("catalog"), r.PathValue("id"))
	if err != nil {
		writeFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) catalogSync(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.appCenter.Catalog().Sync(r.Context()))
}

func (s *server) catalogConnect(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Code) == "" {
		writeFault(w, &appinstall.Fault{Code: appinstall.CodeConnFormat, Message: "请求体要是 {\"code\": \"AFXC-…\"}"})
		return
	}
	v, err := s.appCenter.Catalog().Connect(r.Context(), req.Code)
	if err != nil {
		writeFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) catalogSources(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": s.appCenter.Catalog().Sources()})
}

// catalogPlatformVars lists the platform variables apps can use, with this
// machine's values (read-only; changes go with an install request).
func (s *server) catalogPlatformVars(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vars": s.appCenter.PlatformVars(r.Context())})
}

func (s *server) catalogInstall(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var req appinstall.Request
	if err := decodeJSON(r, &req); err != nil {
		writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "请求体不是合法 JSON"})
		return
	}
	req.App, req.Recipe, req.Name, req.Catalog = strings.TrimSpace(req.App), strings.TrimSpace(req.Recipe), strings.TrimSpace(req.Name), strings.TrimSpace(req.Catalog)
	v, err := s.appCenter.StartInstall(r.Context(), req)
	if err != nil {
		writeFault(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, v)
}

func (s *server) catalogTasks(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.Tasks(r.Context(), 50)
	if err != nil {
		writeFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": v})
}

func (s *server) catalogTask(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.Task(r.Context(), r.PathValue("task"))
	if err != nil {
		writeFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *server) appFirstLogin(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	reveal := r.URL.Query().Get("reveal") == "true"
	v, err := s.appCenter.FirstLogin(r.Context(), r.PathValue("app"), reveal)
	if err != nil {
		writeFault(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}

// catalogAppURL is the address an installed catalog app opens at. An app
// that is only reachable from this machine gets its loopback address, not
// the server's public host (which would not answer).
func (s *server) catalogAppURL(ctx context.Context, a state.App) string {
	return s.buildAppView(ctx, a, false).URL
}
