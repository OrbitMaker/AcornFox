package apiserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// Changing an installed app's settings and applying them again
// (internal/appinstall/reapply.go):
//
//	PATCH /v1/apps/{app}/vars     body {"set": {"名": 值, "platform.名": 值}, "reset": ["platform.名"],
//	                              "plan": false, "force": false, "confirm_restart": false}
//	                              change settings and apply them; plan true only says what would happen
//	POST  /v1/apps/{app}/reapply  body {"plan", "force", "confirm_restart", "assume_exposure"}: apply the
//	                              settings again (a platform variable or a service changed, an address
//	                              changed); assume_exposure (plan only) is the exposure being thought of
//	GET   /v1/platform/affected   ?var=platform.名 | uses.需要名.项 | service:服务名: the apps that are
//	                              behind, or that read that variable
//
// Answers: a plan is 200 {"plan": …}. A run with nothing to change is 200
// {"changed": false, "plan": …}; otherwise 202 {"changed": true, "plan": …,
// "task": …} (the task is an install task of kind reapply: GET
// /v1/catalog/install/{task}, `acornfox install status`). An apply that
// restarts the app without confirm_restart is 409 APP-APPLY-05 with the plan
// in the body and nothing changed. Errors carry APP-APPLY-* or CAT-INST-*.
// The same entries as first-login and vars: the trusted socket and the
// logged-in console.

func (s *server) registerReapply(mux *http.ServeMux) {
	mux.HandleFunc("PATCH /v1/apps/{app}/vars", s.appVarsPatch)
	mux.HandleFunc("POST /v1/apps/{app}/reapply", s.appReapply)
	mux.HandleFunc("GET /v1/platform/affected", s.platformAffected)
}

type varsPatchRequest struct {
	Set            map[string]any `json:"set"`
	Reset          []string       `json:"reset"`
	Plan           bool           `json:"plan"`
	Force          bool           `json:"force"`
	ConfirmRestart bool           `json:"confirm_restart"`
}

func (s *server) appVarsPatch(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var body varsPatchRequest
	if err := decodeJSON(r, &body); err != nil {
		writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "请求体不是合法 JSON"})
		return
	}
	if len(body.Set) == 0 && len(body.Reset) == 0 {
		writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "要改哪一项？set 里至少写一项（名: 新值；平台变量写成 platform.名字）"})
		return
	}
	req := appinstall.ReapplyRequest{App: r.PathValue("app"), Force: body.Force, ConfirmRestart: body.ConfirmRestart, Why: appinstall.WhySet}
	for k, v := range body.Set {
		if name, ok := strings.CutPrefix(k, "platform."); ok {
			text, isText := v.(string)
			if !isText {
				writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadPlatform, Message: "平台变量 " + k + " 的值要写成文字"})
				return
			}
			if req.Platform == nil {
				req.Platform = map[string]string{}
			}
			req.Platform[name] = text
			continue
		}
		if req.Params == nil {
			req.Params = map[string]any{}
		}
		req.Params[k] = v
	}
	req.ResetPlatform = body.Reset
	s.runReapply(w, r, req, body.Plan)
}

type reapplyRequestBody struct {
	Plan           bool   `json:"plan"`
	Force          bool   `json:"force"`
	ConfirmRestart bool   `json:"confirm_restart"`
	AssumeExposure string `json:"assume_exposure"`
}

func (s *server) appReapply(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	var body reapplyRequestBody
	if err := decodeJSON(r, &body); err != nil {
		writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "请求体不是合法 JSON"})
		return
	}
	req := appinstall.ReapplyRequest{App: r.PathValue("app"), Force: body.Force, ConfirmRestart: body.ConfirmRestart, Why: appinstall.WhyReapply}
	if e := body.AssumeExposure; e != "" {
		if !body.Plan {
			writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "assume_exposure 只在 plan 时用（先看改了对外方式会怎样）；真要改对外方式用 PATCH /v1/apps/{app}"})
			return
		}
		if !state.ValidExposure(e) || e == state.ExposureAFXCloud {
			writeFault(w, &appinstall.Fault{Code: appinstall.CodeBadParams, Message: "assume_exposure 只能是 local、lan 或 public"})
			return
		}
		if a, err := s.store.GetApp(r.Context(), req.App); err == nil && !a.Internal {
			wh := s.where(r.Context(), a)
			wh.Exposure = e
			req.AssumeWhere = &wh
		}
	}
	s.runReapply(w, r, req, body.Plan)
}

// runReapply answers a plan or starts the apply and answers it.
func (s *server) runReapply(w http.ResponseWriter, r *http.Request, req appinstall.ReapplyRequest, plan bool) {
	ctx := r.Context()
	if plan {
		p, err := s.appCenter.ReapplyPlan(ctx, req)
		if err != nil {
			writeFault(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": p})
		return
	}
	started, err := s.appCenter.StartReapply(ctx, req)
	if err != nil {
		writeApplyFault(w, started, err)
		return
	}
	if started.Task == nil {
		writeJSON(w, http.StatusOK, map[string]any{"changed": false, "plan": started.Plan})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"changed": true, "plan": started.Plan, "task": started.Task})
}

// writeApplyFault is writeFault, with the plan next to the error of an apply
// that was refused for want of the user's go-ahead.
func writeApplyFault(w http.ResponseWriter, started appinstall.ReapplyStarted, err error) {
	f := appinstall.AsFault(err)
	if f.Code != appinstall.CodeApplyConfirm {
		writeFault(w, err)
		return
	}
	var b catalogErrorBody
	b.Error.Code, b.Error.Message, b.Error.Hint = f.Code, f.Message, f.Hint
	writeJSON(w, f.HTTPStatus(), map[string]any{"error": b.Error, "plan": started.Plan})
}

func (s *server) platformAffected(w http.ResponseWriter, r *http.Request) {
	if !s.appCenterOn(w) {
		return
	}
	v, err := s.appCenter.Affected(r.Context(), r.URL.Query().Get("var"))
	if err != nil {
		writeFault(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// addressChange is what changing an app's exposure or domains means for the
// app's own settings: the plan of the apply it needs (the values it reads
// that follow its address), asked before anything is changed.
type addressChange struct {
	plan   appinstall.ReapplyPlan
	needed bool // an apply is to follow the change
	ask    bool // it restarts the app, so the user must have agreed
}

// planAddress works out what the change of app cur's address (to where)
// does to the app's settings. An app that is not from the app center, or
// that cannot be applied again, has none: the change is made as before.
func (s *server) planAddress(ctx context.Context, cur state.App, where platformvar.Where) addressChange {
	if s.appCenter == nil || cur.CatalogID == "" || cur.Internal {
		return addressChange{}
	}
	p, err := s.appCenter.ReapplyPlan(ctx, appinstall.ReapplyRequest{App: cur.Name, AssumeWhere: &where, Why: appinstall.WhyAddress})
	if err != nil || !p.Needed {
		return addressChange{}
	}
	return addressChange{plan: p, needed: true, ask: p.Restart}
}

// refuseUnconfirmed answers APP-APPLY-05 when the change restarts the app and
// the request did not say the user agreed.
func (c addressChange) refuseUnconfirmed(w http.ResponseWriter, confirm bool, skip bool) bool {
	if !c.needed || !c.ask || confirm || skip {
		return false
	}
	f := &appinstall.Fault{Code: appinstall.CodeApplyConfirm}
	f.Message = c.plan.Summary + "。要用户同意才做：同意后加上 --yes（接口里 confirm_restart: true）再来，或者不让它跟着变（--no-reapply，接口里 reapply: skip，应用里的地址以后用 acornfox apps reapply 再对）；什么都还没改"
	f.Hint = appinstallHint(f.Code)
	writeApplyFault(w, appinstall.ReapplyStarted{Plan: c.plan}, f)
	return true
}

func appinstallHint(code string) string {
	for _, d := range appinstall.Faults {
		if d.Code == code {
			return d.Action
		}
	}
	return ""
}

// followAddress starts the apply that follows an address change (after it
// has been made) and returns what to put in the answer, nil when none.
func (s *server) followAddress(ctx context.Context, app string, c addressChange, skip bool) map[string]any {
	if !c.needed || skip {
		return nil
	}
	started, err := s.appCenter.StartReapply(ctx, appinstall.ReapplyRequest{App: app, ConfirmRestart: true, Why: appinstall.WhyAddress})
	if err != nil {
		f := appinstall.AsFault(err)
		return map[string]any{"error": map[string]any{"code": f.Code, "message": f.Message}, "plan": c.plan,
			"note": "访问方式已经改了，但应用里的设置没能跟着变：" + f.Message + "。之后用 acornfox apps reapply " + app + " 再对"}
	}
	out := map[string]any{"plan": started.Plan}
	if started.Task != nil {
		out["task"] = started.Task
	}
	return out
}
