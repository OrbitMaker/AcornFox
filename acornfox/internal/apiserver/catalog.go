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
//	                                     private catalogs (each with its source), serial, last sync;
//	                                     like GET /v1/apps it starts a background sync when the last good
//	                                     one is older than 30 minutes (appinstall.Catalog.TouchStale)
//	GET  /v1/catalog/apps/{id}           detail and install form (no hidden params); ?catalog=public|cat-…
//	GET  /v1/catalog/skills/{kind}/{id}  the operating manual of an app or a solution (kind app, recipe or
//	                                     solution): {id, kind, has_skill, version, checked, size, text, code, note};
//	                                     no manual is has_skill false with a note (200), an unknown id CAT-INST-01 (404).
//	                                     The list and the detail only say whether there is one ("skill" in "show")
//	POST /v1/catalog/sync                sync now (public, then private catalogs); with ?if_stale=1 only when the
//	                                     last good sync is older than 30 minutes (and the last attempt older than
//	                                     5 minutes): the answer is the sync status plus "ran" (did it sync now)
//	POST /v1/catalog/connect             connect a private catalog with a code {"code"} (W10)
//	GET  /v1/catalog/sources             public and private catalogs: serial, last sync, disconnected
//	GET  /v1/catalog/platform-vars       platform variables apps can use, with this machine's values
//	POST /v1/catalog/plan                what an install request (same body) would do, read-only:
//	                                     the apps asked for in install order, every need and what it
//	                                     is matched with, the required parameters still empty, and
//	                                     every problem (always 200; can_install says)
//	POST /v1/catalog/install             start an install task (202); "platform" changes user_can_set variables;
//	                                     "use" names the service of a need
//	GET  /v1/catalog/install             recent tasks
//	GET  /v1/catalog/install/{task}      one task's progress; both carry "timing", the verdict on
//	                                     the step times by this server's clock (done, slow, stuck,
//	                                     say, next_command, must_tell: internal/installtiming)
//	GET  /v1/apps/{app}/first-login      first-login info; ?reveal=true shows secrets
//	GET  /v1/apps/{app}/vars             the variables the app was installed with (read-only);
//	                                     ?reveal=true shows secrets in full
//
// Errors carry the CAT-INST-* or CAT-CONN-* code:
// {"error":{"code","message","hint","task_id","gateway_code","retry_after","needs_platform"}}.
// needs_platform (the AcornFox version to upgrade to) is there exactly when
// upgrading is the cure: CAT-INST-08, and the CAT-INST-29 of a manifest that
// asks for a newer platform.
// The connection code is never logged: it only travels in the request body.
// All routes sit on the trusted socket and on the console entry, which
// requires a logged-in session; nothing here is reachable anonymously.

func (s *server) registerCatalog(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/catalog", s.catalogList)
	mux.HandleFunc("GET /v1/catalog/apps/{id}", s.catalogApp)
	mux.HandleFunc("GET /v1/catalog/skills/{kind}/{id}", s.catalogSkill)
	mux.HandleFunc("POST /v1/catalog/sync", s.catalogSync)
	mux.HandleFunc("POST /v1/catalog/connect", s.catalogConnect)
	mux.HandleFunc("GET /v1/catalog/sources", s.catalogSources)
	mux.HandleFunc("GET /v1/catalog/platform-vars", s.catalogPlatformVars)
	mux.HandleFunc("POST /v1/catalog/plan", s.catalogPlan)
	mux.HandleFunc("POST /v1/catalog/install", s.catalogInstall)
	mux.HandleFunc("GET /v1/catalog/install", s.catalogTasks)
	mux.HandleFunc("GET /v1/catalog/install/{task}", s.catalogTask)
	mux.HandleFunc("GET /v1/apps/{app}/first-login", s.appFirstLogin)
	mux.HandleFunc("GET /v1/apps/{app}/vars", s.appVars)
	s.registerReapply(mux)
	s.registerUpgrade(mux)
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
		Where:   s.where,
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
		// NeedsPlatform: upgrade AcornFox to this version first.
		NeedsPlatform string `json:"needs_platform,omitempty"`
	} `json:"error"`
}

func writeFault(w http.ResponseWriter, err error) {
	f := appinstall.AsFault(err)
	var b catalogErrorBody
	b.Error.Code, b.Error.Message, b.Error.Hint, b.Error.TaskID, b.Error.Diagnosis = f.Code, f.Message, f.Hint, f.TaskID, f.Diagnosis
	b.Error.GatewayCode, b.Error.RetryAfter = f.GatewayCode, f.RetryAfter
	b.Error.NeedsPlatform = f.NeedsPlatform
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
	// Someone opened the app center: the catalog may be old, bring it up to
	// date behind the page (never holds this answer).
	s.appCenter.Catalog().TouchStale()
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

func (s *server) catalogSkill(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.Skill(r.Context(), r.PathValue("kind"), r.PathValue("id"))
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
	if r.URL.Query().Get("if_stale") != "" && r.URL.Query().Get("if_stale") != "0" {
		st, ran := s.appCenter.Catalog().SyncIfStale(r.Context())
		writeJSON(w, http.StatusOK, struct {
			appinstall.SyncStatus
			Ran bool `json:"ran"`
		}{st, ran})
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

// catalogPlan answers what an install request would do; it creates nothing.
// Problems are in the answer (can_install false), not an error status.
func (s *server) catalogPlan(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var req appinstall.Request
	if err := decodeJSON(r, &req); err != nil {
		writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "请求体不是合法 JSON"})
		return
	}
	req.App, req.Recipe, req.Name, req.Catalog = strings.TrimSpace(req.App), strings.TrimSpace(req.Recipe), strings.TrimSpace(req.Name), strings.TrimSpace(req.Catalog)
	v, err := s.appCenter.Plan(r.Context(), req)
	if err != nil {
		writeFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
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
	writeJSON(w, http.StatusOK, s.taskWithOpenHints(r.Context(), v))
}

// taskOut is one install task as GET /v1/catalog/install/{task} gives it:
// once it has succeeded, every app of it that only opens on this server
// carries open_hint, the same as GET /v1/apps gives for it, so whoever
// reads the result can say how to open it without another call.
type taskOut struct {
	appinstall.TaskView
	Apps []taskAppOut `json:"apps"`
}

type taskAppOut struct {
	appinstall.AppResult
	OpenHint *openHint `json:"open_hint,omitempty"`
}

// taskWithOpenHints is v with the open hints of its apps (succeeded only;
// worked out now, never stored with the task: the app's exposure may have
// changed since).
func (s *server) taskWithOpenHints(ctx context.Context, v appinstall.TaskView) taskOut {
	out := taskOut{TaskView: v, Apps: make([]taskAppOut, 0, len(v.Apps))}
	for _, a := range v.Apps {
		x := taskAppOut{AppResult: a}
		if v.Status == state.TaskSucceeded {
			if app, err := s.store.GetApp(ctx, a.App); err == nil {
				x.OpenHint = s.localOpenHint(ctx, app)
			}
		}
		out.Apps = append(out.Apps, x)
	}
	return out
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

// appVars lists the variables an app was installed with. It sits on the same
// entries as first-login (the trusted socket and the logged-in console) and
// shows secrets in full only with ?reveal=true.
func (s *server) appVars(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	reveal := r.URL.Query().Get("reveal") == "true"
	v, err := s.appCenter.AppVars(r.Context(), r.PathValue("app"), reveal)
	if err != nil {
		writeFault(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, v)
}
