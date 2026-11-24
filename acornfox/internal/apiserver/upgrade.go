package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/state"
)

// Upgrading an app from the app center and going back to before the upgrade
// (internal/appinstall/upgrade.go):
//
//	GET  /v1/apps, GET /v1/apps/{app}   an app from the app center carries catalog_update (the newer
//	                                    version of it this machine can install; absent when none) and
//	                                    upgrade {available, can_upgrade, blocked, blocked_code, notes,
//	                                    snapshot {id, version, created_at, size_mb} | null, can_rollback}
//	GET  /v1/status                     apps_outdated: how many apps have a newer version
//	GET  /v1/apps/{app}/events          ?after=<id>&limit=<n>: the app's own events, oldest first, {"events":
//	                                    [{id, app, deployment_id, at, stage, message}]}; a new version found,
//	                                    an upgrade done or put back are the ones with stage "upgrade"
//	POST /v1/apps/{app}/upgrade         body {"plan": true, "to": "", "set": {"设置名": 值}, "confirm_restart": false}
//	                                    plan true: 200 {"plan": {from, to, restart, snapshot_mb, disk_ok,
//	                                    vars_added[], vars_removed[], uses_changed[], notes[], summary,
//	                                    can_upgrade, blocked, blocked_code, …}}, nothing is touched.
//	                                    Otherwise 202 {"plan", "task"} (task kind upgrade: GET
//	                                    /v1/catalog/install/{task}); 409 APP-UPG-04 with the plan when
//	                                    confirm_restart is missing; a blocked plan is 409 with its code
//	                                    (and the plan in the body)
//	POST /v1/apps/{app}/upgrade/rollback  body {"plan": true, "confirm": false}: back to the snapshot taken
//	                                    before the upgrade. plan: 200 {"plan": {from, to, snapshot_id,
//	                                    loses_data, …}}; otherwise 202 {"plan", "task"} (kind rollback);
//	                                    409 APP-UPG-08 without confirm (the data written after the upgrade
//	                                    is lost: the answer says so).
//	POST /v1/apps/{app}/rollback        the same for an app from the app center, when the body has a "plan" or a "confirm" key. The route is
//	                                    the one that rolls an app back to its previous deployment (rollback.go,
//	                                    body empty, {} or {"cancel_pending": true}), which stays as it was for the user's own projects:
//	                                    a body without those keys is that one. Clients send the unambiguous
//	                                    …/upgrade/rollback.
//
// Errors carry APP-UPG-*, CAT-INST-* or BKP-* codes. The same entries as
// the other app routes (the trusted socket, the logged-in console).

func (s *server) registerUpgrade(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/apps/{app}/events", s.appEvents)
	mux.HandleFunc("POST /v1/apps/{app}/upgrade", s.appUpgrade)
	mux.HandleFunc("POST /v1/apps/{app}/upgrade/rollback", s.appRollback)
}

type upgradeBody struct {
	Plan           bool           `json:"plan"`
	To             string         `json:"to"`
	Set            map[string]any `json:"set"`
	ConfirmRestart bool           `json:"confirm_restart"`
}

func (s *server) appUpgrade(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var body upgradeBody
	if err := decodeJSON(r, &body); err != nil {
		writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "请求体不是合法 JSON"})
		return
	}
	req := appinstall.UpgradeRequest{App: r.PathValue("app"), To: body.To, Params: body.Set, ConfirmRestart: body.ConfirmRestart}
	ctx := r.Context()
	if body.Plan {
		p, err := s.appCenter.UpgradePlan(ctx, req)
		if err != nil {
			writeFault(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": p})
		return
	}
	started, err := s.appCenter.StartUpgrade(ctx, req)
	if err != nil {
		writeFaultWithPlan(w, err, started.Plan, started.Plan.App != "")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"plan": started.Plan, "task": started.Task})
}

type rollbackBody struct {
	Plan    bool `json:"plan"`
	Confirm bool `json:"confirm"`
}

func (s *server) appRollback(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var body rollbackBody
	if err := decodeJSON(r, &body); err != nil {
		writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "请求体不是合法 JSON"})
		return
	}
	s.runRollback(w, r, body)
}

// upgradeRollbackAsked reports whether the body of POST /v1/apps/{app}/rollback
// asks for the rollback to before the upgrade (it has a "plan" or a
// "confirm" key) and not for the one to the previous deployment; it gives the
// body back to the request so either handler reads it.
func upgradeRollbackAsked(r *http.Request) (rollbackBody, bool) {
	var body rollbackBody
	if r.Body == nil {
		return body, false
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRollbackBody+1))
	if err != nil {
		return body, false
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	var keys map[string]json.RawMessage
	if json.Unmarshal(data, &keys) != nil {
		return body, false
	}
	_, hasPlan := keys["plan"]
	_, hasConfirm := keys["confirm"]
	if !hasPlan && !hasConfirm {
		return body, false
	}
	if json.Unmarshal(data, &body) != nil {
		return body, false
	}
	return body, true
}

func (s *server) runRollback(w http.ResponseWriter, r *http.Request, body rollbackBody) {
	if !s.appCenterOn(w) {
		return
	}
	ctx := r.Context()
	app := r.PathValue("app")
	if body.Plan {
		p, err := s.appCenter.PlanRollback(ctx, app)
		if err != nil {
			writeFault(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": p})
		return
	}
	started, err := s.appCenter.StartRollback(ctx, appinstall.RollbackRequest{App: app, Confirm: body.Confirm})
	if err != nil {
		writeFaultWithPlan(w, err, started.Plan, started.Plan.App != "")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"plan": started.Plan, "task": started.Task})
}

// writeFaultWithPlan is writeFault with the plan next to the error, for a
// refusal that has one (an upgrade that needs the user's yes or is blocked).
func writeFaultWithPlan(w http.ResponseWriter, err error, plan any, has bool) {
	f := appinstall.AsFault(err)
	if !has {
		writeFault(w, err)
		return
	}
	var b catalogErrorBody
	b.Error.Code, b.Error.Message, b.Error.Hint = f.Code, f.Message, f.Hint
	writeJSON(w, f.HTTPStatus(), map[string]any{"error": b.Error, "plan": plan})
}

// fillUpgrade puts what each app of the app center says about upgrading into
// its view (catalog_update, upgrade). detail also checks that going back is
// possible.
func (s *server) fillUpgrade(ctx context.Context, apps []state.App, views []appView, detail bool) {
	if s.appCenter == nil {
		return
	}
	infos := s.appCenter.UpgradeInfos(ctx, apps, detail)
	for i := range views {
		if in := infos[views[i].Name]; in != nil {
			views[i].CatalogUpdate = in.Available
			views[i].Upgrade = in
		}
	}
}

// maxAppEvents bounds one page of an app's events.
const maxAppEvents = 200

func (s *server) appEvents(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !state.ValidAppName(app) {
		writeError(w, http.StatusBadRequest, "invalid_app", "应用名不合法")
		return
	}
	if _, err := s.store.GetApp(r.Context(), app); err != nil {
		s.mapStoreError(w, err)
		return
	}
	after, limit := int64(0), 50
	if v := r.URL.Query().Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_after", "after 要写事件编号（非负整数）")
			return
		}
		after = n
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxAppEvents {
			writeError(w, http.StatusBadRequest, "invalid_limit", "limit 要写 1 到 200")
			return
		}
		limit = n
	}
	evs, err := s.store.ListEvents(r.Context(), app, "", after, limit)
	if err != nil {
		s.mapStoreError(w, err)
		return
	}
	if evs == nil {
		evs = []state.Event{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs})
}
