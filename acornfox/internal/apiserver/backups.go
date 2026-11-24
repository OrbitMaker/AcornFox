package apiserver

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/acornfox/acornfox/internal/appbackup"
	"github.com/acornfox/acornfox/internal/state"
)

// App data backups and restores (package appbackup).
//
//	GET    /v1/backups                              the backup directory: space, per app counts, checks (doctor reads it)
//	GET    /v1/backups/tasks/{id}                   one task: steps, fault, result
//	GET    /v1/apps/{app}/backups                   the app's backups (newest first), the tasks on it, how many are kept
//	POST   /v1/apps/{app}/backups                   {"kind","note","plan"}: 202 {"task"} and the work goes on; "plan": true only says what it would do (200 {"plan"})
//	GET    /v1/apps/{app}/backups/{id}[?verify=true] one backup's list; verify also checks every file's sha256
//	POST   /v1/apps/{app}/backups/{id}/restore      {"confirm","plan","allow_current_version"}: 202 {"task"}; without "confirm" BKP-06 (the data would be overwritten); "plan": 200 {"plan"}
//	DELETE /v1/apps/{app}/backups/{id}?confirm=true remove the files (BKP-06 without confirm); {"deleted": false} when there was none
//
// Errors: {"error":{"code","message","hint"}} with a BKP-* code. A server
// without backups (no manager, or a runner that cannot read volumes) answers
// 404 not_found / BKP-11.

func (s *server) registerBackups(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/backups", s.backupsOverview)
	mux.HandleFunc("GET /v1/backups/tasks/{id}", s.backupTask)
	mux.HandleFunc("GET /v1/apps/{app}/backups", s.listBackups)
	mux.HandleFunc("POST /v1/apps/{app}/backups", s.createBackup)
	mux.HandleFunc("GET /v1/apps/{app}/backups/{id}", s.getBackup)
	mux.HandleFunc("POST /v1/apps/{app}/backups/{id}/restore", s.restoreBackup)
	mux.HandleFunc("DELETE /v1/apps/{app}/backups/{id}", s.deleteBackup)
}

func (s *server) backupsOff(w http.ResponseWriter) bool {
	if s.backups == nil {
		writeError(w, http.StatusNotFound, "not_found", "该服务端不支持应用数据备份")
		return true
	}
	return false
}

func (s *server) writeBackupError(w http.ResponseWriter, err error) {
	var be *appbackup.Error
	if errors.As(err, &be) {
		var b serviceErrorBody
		b.Error.Code, b.Error.Message, b.Error.Hint = be.Code, be.Message, be.Hint
		writeJSON(w, be.Status, b)
		return
	}
	if errors.Is(err, state.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "对象不存在")
		return
	}
	s.log.Error("backup", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "内部错误")
}

func (s *server) backupApp(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.backupsOff(w) {
		return "", false
	}
	app := r.PathValue("app")
	if !state.ValidAppName(app) {
		writeError(w, http.StatusBadRequest, "invalid_app", "应用名不合法")
		return "", false
	}
	return app, true
}

func (s *server) backupsOverview(w http.ResponseWriter, r *http.Request) {
	if s.backupsOff(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.backups.Overview(r.Context()))
}

func (s *server) backupTask(w http.ResponseWriter, r *http.Request) {
	if s.backupsOff(w) {
		return
	}
	id := r.PathValue("id")
	if !appbackup.ValidTaskID(id) {
		writeError(w, http.StatusBadRequest, "invalid_task", "任务编号不合法")
		return
	}
	t, err := s.backups.Task(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "没有这个任务（服务重启过，或太久以前的）")
		return
	}
	writeJSON(w, http.StatusOK, t)
}

type backupList struct {
	App       string               `json:"app"`
	Installed bool                 `json:"installed"`
	Backups   []appbackup.Meta     `json:"backups"`
	Tasks     []appbackup.TaskView `json:"tasks"`
	Keep      appbackup.KeepCounts `json:"keep"`
	Busy      string               `json:"busy,omitempty"`
}

func (s *server) listBackups(w http.ResponseWriter, r *http.Request) {
	app, ok := s.backupApp(w, r)
	if !ok {
		return
	}
	list, err := s.backups.List(app)
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	if list == nil {
		list = []appbackup.Meta{}
	}
	_, gerr := s.store.GetApp(r.Context(), app)
	tasks := s.backups.Tasks(app)
	if len(tasks) > 10 {
		tasks = tasks[:10]
	}
	if tasks == nil {
		tasks = []appbackup.TaskView{}
	}
	ov := s.backups.Overview(r.Context())
	writeJSON(w, http.StatusOK, backupList{App: app, Installed: gerr == nil, Backups: list, Tasks: tasks, Keep: ov.Keep, Busy: s.backups.Busy(app)})
}

type createBackupRequest struct {
	Kind string `json:"kind"`
	Note string `json:"note"`
	Plan bool   `json:"plan"`
}

func (s *server) createBackup(w http.ResponseWriter, r *http.Request) {
	app, ok := s.backupApp(w, r)
	if !ok {
		return
	}
	var req createBackupRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "请求体不是合法的 JSON")
		return
	}
	o := appbackup.BackupOptions{Kind: req.Kind, Note: req.Note}
	if req.Plan {
		plan, err := s.backups.PlanBackup(r.Context(), app, o)
		if err != nil {
			s.writeBackupError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
		return
	}
	t, err := s.backups.StartBackup(r.Context(), app, o)
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task": t})
}

func (s *server) getBackup(w http.ResponseWriter, r *http.Request) {
	app, ok := s.backupApp(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	plan, err := s.backups.PlanRestore(r.Context(), app, id, r.URL.Query().Get("verify") == "true")
	if err != nil {
		// A backup of an app that is gone can still be looked at.
		var be *appbackup.Error
		if errors.As(err, &be) && be.Code == appbackup.CodeNoApp {
			if meta, merr := s.backups.Get(app, id); merr == nil {
				writeJSON(w, http.StatusOK, map[string]any{"backup": meta, "installed": false})
				return
			}
		}
		s.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backup": plan.Backup, "installed": true, "restore_plan": plan})
}

type restoreRequest struct {
	Confirm             bool `json:"confirm"`
	Plan                bool `json:"plan"`
	AllowCurrentVersion bool `json:"allow_current_version"`
}

func (s *server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	app, ok := s.backupApp(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var req restoreRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "请求体不是合法的 JSON")
		return
	}
	if req.Plan {
		plan, err := s.backups.PlanRestore(r.Context(), app, id, true)
		if err != nil {
			s.writeBackupError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plan": plan})
		return
	}
	t, err := s.backups.StartRestore(r.Context(), app, id, appbackup.RestoreOptions{Confirm: req.Confirm, AllowCurrentVersion: req.AllowCurrentVersion})
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task": t})
}

func (s *server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	app, ok := s.backupApp(w, r)
	if !ok {
		return
	}
	raw := r.URL.Query().Get("confirm")
	if raw != "" && raw != "true" && raw != "false" {
		writeError(w, http.StatusBadRequest, "invalid", "confirm 必须为 true 或 false")
		return
	}
	deleted, err := s.backups.Delete(r.Context(), app, r.PathValue("id"), raw == "true")
	if err != nil {
		s.writeBackupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app": app, "id": r.PathValue("id"), "deleted": deleted})
}

// backupBusyText is what stops an app from being deleted while a backup or a
// restore runs on it ("" when none does).
func (s *server) backupBusyText(app string) string {
	if s.backups == nil {
		return ""
	}
	if t := s.backups.Busy(app); t != "" {
		return "应用 " + app + " 正在备份或恢复（任务 " + t + "），等它做完再删除"
	}
	return ""
}

// backupsLeftNote tells the user that a deleted app's backups stay on the machine
// ("" when there are none).
func (s *server) backupsLeftNote(app string) string {
	if s.backups == nil {
		return ""
	}
	list, err := s.backups.List(app)
	if err != nil || len(list) == 0 {
		return ""
	}
	return "应用 " + app + " 的 " + strconv.Itoa(len(list)) + " 份备份还留在这台机器上（不随应用删除）；要清掉：acornfox backup rm " + app + " 备份编号 --yes"
}
