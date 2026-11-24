package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// App data backups (GET/POST/DELETE /v1/apps/{app}/backups…, GET /v1/backups,
// GET /v1/backups/tasks/{id}); the server side is package appbackup. A server
// older than these routes answers with its router's plain 404/405: every
// method turns that into server_too_old. The server's own errors (BKP-*)
// pass through unchanged.

// BackupAPI is implemented by *Client (a separate interface so the existing
// test fakes keep compiling).
type BackupAPI interface {
	Backups(ctx context.Context) (BackupOverview, error)                                                       // GET    /v1/backups
	AppBackups(ctx context.Context, app string) (BackupList, error)                                            // GET    /v1/apps/{app}/backups
	CreateBackup(ctx context.Context, app, kind, note string, plan bool) (BackupAnswer, error)                 // POST   /v1/apps/{app}/backups
	BackupDetail(ctx context.Context, app, id string, verify bool) (BackupDetail, error)                       // GET    /v1/apps/{app}/backups/{id}
	RestoreBackup(ctx context.Context, app, id string, confirm, plan, allowCurrent bool) (BackupAnswer, error) // POST   /v1/apps/{app}/backups/{id}/restore
	DeleteBackup(ctx context.Context, app, id string, confirm bool) (BackupDeleted, error)                     // DELETE /v1/apps/{app}/backups/{id}
	BackupTask(ctx context.Context, id string) (BackupTask, error)                                             // GET    /v1/backups/tasks/{id}
}

// BackupMeta is one backup (its meta.json).
type BackupMeta struct {
	ID         string    `json:"id"`
	App        string    `json:"app"`
	Kind       string    `json:"kind"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	Platform   string    `json:"platform,omitempty"`
	WasRunning bool      `json:"was_running"`
	DowntimeMS int64     `json:"downtime_ms"`
	Version    struct {
		DeploymentID   string `json:"deployment_id,omitempty"`
		Seq            int    `json:"seq,omitempty"`
		ImageID        string `json:"image_id,omitempty"`
		CatalogID      string `json:"catalog_id,omitempty"`
		CatalogVersion string `json:"catalog_version,omitempty"`
	} `json:"version"`
	Volumes []struct {
		Role     string `json:"role"`
		Path     string `json:"path,omitempty"`
		Addon    string `json:"addon,omitempty"`
		Volume   string `json:"volume"`
		File     string `json:"file"`
		Bytes    int64  `json:"bytes"`
		RawBytes int64  `json:"raw_bytes"`
		Entries  int    `json:"entries"`
		SHA256   string `json:"sha256"`
	} `json:"volumes"`
	Bytes    int64 `json:"bytes"`
	RawBytes int64 `json:"raw_bytes"`
}

// BackupStep is one step of a task.
type BackupStep struct {
	Key    string `json:"key"`
	Text   string `json:"text"`
	Status string `json:"status"` // pending | running | done | failed | skipped
	Detail string `json:"detail,omitempty"`
}

// BackupTask is a backup or restore task.
type BackupTask struct {
	ID     string       `json:"id"`
	App    string       `json:"app"`
	Op     string       `json:"op"` // backup | restore
	Kind   string       `json:"kind,omitempty"`
	Status string       `json:"status"` // running | succeeded | failed
	Steps  []BackupStep `json:"steps"`
	Fault  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint,omitempty"`
	} `json:"fault,omitempty"`
	Result *struct {
		BackupID     string   `json:"backup_id,omitempty"`
		Kind         string   `json:"kind,omitempty"`
		Bytes        int64    `json:"bytes,omitempty"`
		PreRestoreID string   `json:"pre_restore_id,omitempty"`
		RolledBack   bool     `json:"rolled_back,omitempty"`
		Version      string   `json:"version,omitempty"`
		DowntimeMS   int64    `json:"downtime_ms,omitempty"`
		Pruned       []string `json:"pruned,omitempty"`
		Warnings     []string `json:"warnings,omitempty"`
	} `json:"result,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// Done reports whether the task has ended.
func (t BackupTask) Done() bool { return t.Status == "succeeded" || t.Status == "failed" }

// BackupKeep is how many backups of each kind are kept per app.
type BackupKeep struct {
	Manual     int `json:"manual"`
	PreUpgrade int `json:"pre_upgrade"`
	PreRestore int `json:"pre_restore"`
	// AutoDays: an automatic backup (pre-upgrade, pre-restore) younger than
	// this is not removed to keep a count; 0 from an older server.
	AutoDays int `json:"auto_days,omitempty"`
}

// BackupList mirrors GET /v1/apps/{app}/backups.
type BackupList struct {
	App       string          `json:"app"`
	Installed bool            `json:"installed"`
	Backups   []BackupMeta    `json:"backups"`
	Tasks     []BackupTask    `json:"tasks"`
	Keep      BackupKeep      `json:"keep"`
	Busy      string          `json:"busy,omitempty"`
	Raw       json.RawMessage `json:"-"`
}

// BackupPlanView is the "plan" of a backup or a restore (the fields of the
// one that was asked for are filled).
type BackupPlanView struct {
	App       string   `json:"app"`
	Kind      string   `json:"kind,omitempty"`
	Say       string   `json:"say"`
	Enough    bool     `json:"enough"`
	WillStop  bool     `json:"will_stop"`
	NeedBytes int64    `json:"need_bytes,omitempty"`
	FreeBytes uint64   `json:"free_bytes,omitempty"`
	FreeKnown bool     `json:"free_known,omitempty"`
	Keep      int      `json:"keep,omitempty"`
	WillPrune []string `json:"will_prune,omitempty"`
	// WillPruneForSpace: automatic backups removed first because the disk has
	// no room otherwise (they are named in Say too).
	WillPruneForSpace []string `json:"will_prune_for_space,omitempty"`
	Volumes           []struct {
		Role  string `json:"role"`
		Path  string `json:"path,omitempty"`
		Addon string `json:"addon,omitempty"`
		Bytes int64  `json:"bytes"`
	} `json:"volumes,omitempty"`

	// restore
	Backup           *BackupMeta `json:"backup,omitempty"`
	Intact           bool        `json:"intact,omitempty"`
	Verified         bool        `json:"verified,omitempty"`
	VersionNow       string      `json:"version_now,omitempty"`
	VersionThen      string      `json:"version_then,omitempty"`
	ChangesVersion   bool        `json:"changes_version,omitempty"`
	VersionAvailable bool        `json:"version_available,omitempty"`
	NeedsConfirm     bool        `json:"needs_confirm,omitempty"`
	BackupNeedBytes  int64       `json:"backup_need_bytes,omitempty"`
	DockerNeedBytes  int64       `json:"docker_need_bytes,omitempty"`
	CurrentHasData   bool        `json:"current_has_data,omitempty"`
}

// BackupAnswer is the answer to a backup or restore request: a task (202)
// or, with plan, what it would do (200).
type BackupAnswer struct {
	Task *BackupTask     `json:"task,omitempty"`
	Plan *BackupPlanView `json:"plan,omitempty"`
	Raw  json.RawMessage `json:"-"`
}

// BackupDetail mirrors GET /v1/apps/{app}/backups/{id}.
type BackupDetail struct {
	Backup      BackupMeta      `json:"backup"`
	Installed   bool            `json:"installed"`
	RestorePlan *BackupPlanView `json:"restore_plan,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// BackupDeleted mirrors the answer of DELETE /v1/apps/{app}/backups/{id}.
type BackupDeleted struct {
	App     string          `json:"app"`
	ID      string          `json:"id"`
	Deleted bool            `json:"deleted"`
	Raw     json.RawMessage `json:"-"`
}

// BackupOverview mirrors GET /v1/backups.
type BackupOverview struct {
	Dir        string     `json:"dir"`
	Writable   bool       `json:"writable"`
	FreeBytes  uint64     `json:"free_bytes,omitempty"`
	FreeKnown  bool       `json:"free_known"`
	TotalBytes int64      `json:"total_bytes"`
	Keep       BackupKeep `json:"keep"`
	Apps       []struct {
		App       string     `json:"app"`
		Installed bool       `json:"installed"`
		Count     int        `json:"count"`
		Bytes     int64      `json:"bytes"`
		Latest    *time.Time `json:"latest,omitempty"`
	} `json:"apps"`
	Running  []BackupTask `json:"running,omitempty"`
	Findings []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"` // ok | warn | fail
		Text     string `json:"text"`
	} `json:"findings"`
	Raw json.RawMessage `json:"-"`
}

const backupsWhat = "应用数据备份"

func backupPath(app string, rest ...string) string {
	p := "/v1/apps/" + url.PathEscape(app) + "/backups"
	for _, r := range rest {
		p += "/" + url.PathEscape(r)
	}
	return p
}

// Backups calls GET /v1/backups.
func (c *Client) Backups(ctx context.Context) (BackupOverview, error) {
	var v BackupOverview
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/backups", nil, &v)
	v.Raw = raw
	return v, tooOld(err, backupsWhat)
}

// AppBackups calls GET /v1/apps/{app}/backups.
func (c *Client) AppBackups(ctx context.Context, app string) (BackupList, error) {
	var v BackupList
	raw, err := c.getRaw(ctx, http.MethodGet, backupPath(app), nil, &v)
	v.Raw = raw
	return v, tooOld(err, backupsWhat)
}

// CreateBackup calls POST /v1/apps/{app}/backups; with plan it only asks what
// would happen.
func (c *Client) CreateBackup(ctx context.Context, app, kind, note string, plan bool) (BackupAnswer, error) {
	var v BackupAnswer
	raw, err := c.getRaw(ctx, http.MethodPost, backupPath(app), map[string]any{"kind": kind, "note": note, "plan": plan}, &v)
	v.Raw = raw
	if v.Task != nil {
		v.Task.Raw = raw
	}
	return v, tooOld(err, backupsWhat)
}

// BackupDetail calls GET /v1/apps/{app}/backups/{id}.
func (c *Client) BackupDetail(ctx context.Context, app, id string, verify bool) (BackupDetail, error) {
	var v BackupDetail
	path := backupPath(app, id)
	if verify {
		path += "?verify=true"
	}
	raw, err := c.getRaw(ctx, http.MethodGet, path, nil, &v)
	v.Raw = raw
	return v, tooOld(err, backupsWhat)
}

// RestoreBackup calls POST /v1/apps/{app}/backups/{id}/restore.
func (c *Client) RestoreBackup(ctx context.Context, app, id string, confirm, plan, allowCurrent bool) (BackupAnswer, error) {
	var v BackupAnswer
	raw, err := c.getRaw(ctx, http.MethodPost, backupPath(app, id, "restore"), map[string]any{"confirm": confirm, "plan": plan, "allow_current_version": allowCurrent}, &v)
	v.Raw = raw
	return v, tooOld(err, backupsWhat)
}

// DeleteBackup calls DELETE /v1/apps/{app}/backups/{id}.
func (c *Client) DeleteBackup(ctx context.Context, app, id string, confirm bool) (BackupDeleted, error) {
	var v BackupDeleted
	path := backupPath(app, id)
	if confirm {
		path += "?confirm=true"
	}
	raw, err := c.getRaw(ctx, http.MethodDelete, path, nil, &v)
	v.Raw = raw
	return v, tooOld(err, backupsWhat)
}

// BackupTask calls GET /v1/backups/tasks/{id}.
func (c *Client) BackupTask(ctx context.Context, id string) (BackupTask, error) {
	var v BackupTask
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/backups/tasks/"+url.PathEscape(id), nil, &v)
	v.Raw = raw
	return v, tooOld(err, backupsWhat)
}

var _ BackupAPI = (*Client)(nil)
