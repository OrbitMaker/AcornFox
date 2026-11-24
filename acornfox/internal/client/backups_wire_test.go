package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The wire shape of the backup routes (the server side is internal/apiserver
// and appbackup; this pins what the client reads from them).
func TestBackupWireShapes(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.RequestURI() {
		case "POST /v1/apps/notes/backups":
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"task":{"id":"0123456789ab","app":"notes","op":"backup","kind":"manual","status":"running","steps":[{"key":"pack","text":"打包数据","status":"running"}],"started_at":"2026-11-24T10:00:00Z"}}`)
		case "GET /v1/backups/tasks/0123456789ab":
			_, _ = io.WriteString(w, `{"id":"0123456789ab","app":"notes","op":"restore","status":"failed","steps":[],"fault":{"code":"BKP-07","message":"m","hint":"h"},"result":{"backup_id":"20261124-100000-aaaa","pre_restore_id":"20261124-100100-bbbb","rolled_back":true,"warnings":["BKP-15：x"]},"started_at":"2026-11-24T10:00:00Z"}`)
		case "GET /v1/apps/notes/backups":
			_, _ = io.WriteString(w, `{"app":"notes","installed":true,"backups":[{"id":"20261124-100000-aaaa","app":"notes","kind":"pre-upgrade","created_at":"2026-11-24T10:00:00Z","was_running":true,"downtime_ms":1500,"version":{"seq":3,"catalog_version":"1.2"},"volumes":[{"role":"app","path":"/data","volume":"af-notes-1","file":"vol-0.tar.gz","bytes":10,"raw_bytes":100,"entries":4,"sha256":"ab"}],"bytes":12,"raw_bytes":100}],"tasks":[],"keep":{"manual":5,"pre_upgrade":1,"pre_restore":1}}`)
		case "POST /v1/apps/notes/backups/20261124-100000-aaaa/restore":
			_, _ = io.WriteString(w, `{"plan":{"app":"notes","say":"会覆盖","enough":true,"intact":true,"verified":true,"version_now":"1.3","version_then":"1.2","changes_version":true,"version_available":true,"needs_confirm":true,"backup":{"id":"20261124-100000-aaaa","app":"notes","kind":"manual","created_at":"2026-11-24T10:00:00Z","volumes":[]}}}`)
		case "DELETE /v1/apps/notes/backups/20261124-100000-aaaa?confirm=true":
			_, _ = io.WriteString(w, `{"app":"notes","id":"20261124-100000-aaaa","deleted":true}`)
		case "GET /v1/backups":
			_, _ = io.WriteString(w, `{"dir":"/d/backups","writable":true,"free_bytes":1000,"free_known":true,"total_bytes":12,"keep":{"manual":5,"pre_upgrade":1,"pre_restore":1},"apps":[{"app":"notes","installed":false,"count":1,"bytes":12}],"findings":[{"code":"BKP-14","severity":"ok","text":"可写"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `404 page not found`)
		}
	}))
	defer ts.Close()
	c := newDirectClient(t, ts)
	ctx := context.Background()

	a, err := c.CreateBackup(ctx, "notes", "manual", "", false)
	if err != nil || a.Task == nil || a.Task.ID != "0123456789ab" || a.Task.Done() || a.Task.Steps[0].Key != "pack" {
		t.Fatalf("create: %v %+v", err, a)
	}
	tk, err := c.BackupTask(ctx, "0123456789ab")
	if err != nil || !tk.Done() || tk.Fault == nil || tk.Fault.Code != "BKP-07" || tk.Result == nil || !tk.Result.RolledBack || tk.Result.PreRestoreID == "" || len(tk.Result.Warnings) != 1 {
		t.Fatalf("task: %v %+v", err, tk)
	}
	l, err := c.AppBackups(ctx, "notes")
	if err != nil || len(l.Backups) != 1 || l.Backups[0].Kind != "pre-upgrade" || l.Backups[0].Version.Seq != 3 || l.Backups[0].Volumes[0].SHA256 != "ab" || l.Keep.Manual != 5 {
		t.Fatalf("list: %v %+v", err, l)
	}
	r, err := c.RestoreBackup(ctx, "notes", "20261124-100000-aaaa", false, true, false)
	if err != nil || r.Plan == nil || !r.Plan.ChangesVersion || !r.Plan.NeedsConfirm || r.Plan.Backup == nil {
		t.Fatalf("restore plan: %v %+v", err, r)
	}
	d, err := c.DeleteBackup(ctx, "notes", "20261124-100000-aaaa", true)
	if err != nil || !d.Deleted {
		t.Fatalf("delete: %v %+v", err, d)
	}
	o, err := c.Backups(ctx)
	if err != nil || !o.Writable || len(o.Apps) != 1 || o.Apps[0].Installed || len(o.Findings) != 1 {
		t.Fatalf("overview: %v %+v", err, o)
	}
	// An older server's plain 404 is server_too_old.
	_, err = c.AppBackups(ctx, "other")
	var ce *Error
	if err == nil || !errors.As(err, &ce) || ce.Diag.Code != CodeServerTooOld {
		t.Fatalf("old server: %v", err)
	}
}
