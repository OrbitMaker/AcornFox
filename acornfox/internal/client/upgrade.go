package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

// Upgrading an app from the app center and going back to before the upgrade
// (server side: internal/appinstall/upgrade.go). A separate interface so
// existing test fakes keep compiling.

// AppUpgradeAPI is implemented by *Client.
type AppUpgradeAPI interface {
	// AppUpgrade asks for the upgrade of an app (POST /v1/apps/{app}/upgrade);
	// with req.Plan it only says what would happen.
	AppUpgrade(ctx context.Context, app string, req AppUpgradeRequest) (AppUpgrade, error)
	// AppRollback goes back to the snapshot taken before the upgrade (POST
	// /v1/apps/{app}/upgrade/rollback); with req.Plan it only says what would happen.
	AppRollback(ctx context.Context, app string, req AppRollbackRequest) (AppRollback, error)
}

// AppUpgradeRequest is POST /v1/apps/{app}/upgrade.
type AppUpgradeRequest struct {
	Plan           bool           `json:"plan,omitempty"`
	To             string         `json:"to,omitempty"`
	Set            map[string]any `json:"set,omitempty"`
	ConfirmRestart bool           `json:"confirm_restart,omitempty"`
}

// AppRollbackRequest is POST /v1/apps/{app}/rollback.
type AppRollbackRequest struct {
	Plan    bool `json:"plan"`
	Confirm bool `json:"confirm"`
}

// UpgradePlan mirrors appinstall.UpgradePlan.
type UpgradePlan struct {
	App         string          `json:"app"`
	From        string          `json:"from"`
	To          string          `json:"to"`
	CanUpgrade  bool            `json:"can_upgrade"`
	Blocked     string          `json:"blocked,omitempty"`
	BlockedCode string          `json:"blocked_code,omitempty"`
	Restart     bool            `json:"restart"`
	Snapshot    bool            `json:"snapshot"`
	SnapshotMB  int             `json:"snapshot_mb"`
	ImageMB     int             `json:"image_mb,omitempty"`
	DiskOK      bool            `json:"disk_ok"`
	VarsAdded   []string        `json:"vars_added"`
	VarsRemoved []string        `json:"vars_removed"`
	UsesChanged []string        `json:"uses_changed"`
	Notes       []string        `json:"notes"`
	Summary     string          `json:"summary"`
	Settings    *ApplyPlan      `json:"settings,omitempty"`
	Raw         json.RawMessage `json:"-"`
}

// RollbackPlan mirrors appinstall.RollbackPlan.
type RollbackPlan struct {
	App         string    `json:"app"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	CanRollback bool      `json:"can_rollback"`
	Blocked     string    `json:"blocked,omitempty"`
	BlockedCode string    `json:"blocked_code,omitempty"`
	SnapshotID  string    `json:"snapshot_id,omitempty"`
	SnapshotAt  time.Time `json:"snapshot_at,omitempty"`
	LosesData   bool      `json:"loses_data"`
	Restart     bool      `json:"restart"`
	DiskOK      bool      `json:"disk_ok"`
	Notes       []string  `json:"notes"`
	Summary     string    `json:"summary"`
}

// AppUpgrade is the answer of AppUpgrade: the plan and, when something is
// being done, the task (kind upgrade).
type AppUpgrade struct {
	Plan UpgradePlan     `json:"plan"`
	Task *InstallTask    `json:"task,omitempty"`
	Raw  json.RawMessage `json:"-"`
}

// AppRollback is the answer of AppRollback.
type AppRollback struct {
	Plan RollbackPlan    `json:"plan"`
	Task *InstallTask    `json:"task,omitempty"`
	Raw  json.RawMessage `json:"-"`
}

// AppUpgradeInfo is the `upgrade` of an app in GET /v1/apps.
type AppUpgradeInfo struct {
	Available   string       `json:"available"`
	CanUpgrade  bool         `json:"can_upgrade"`
	Blocked     string       `json:"blocked"`
	BlockedCode string       `json:"blocked_code"`
	Notes       []string     `json:"notes"`
	Snapshot    *AppSnapshot `json:"snapshot"`
	CanRollback bool         `json:"can_rollback"`
}

// AppSnapshot is the pre-upgrade snapshot of an app.
type AppSnapshot struct {
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	SizeMB    int       `json:"size_mb"`
}

// AppUpgrade calls POST /v1/apps/{app}/upgrade.
func (c *Client) AppUpgrade(ctx context.Context, app string, req AppUpgradeRequest) (AppUpgrade, error) {
	var v AppUpgrade
	raw, err := c.getRaw(ctx, http.MethodPost, "/v1/apps/"+url.PathEscape(app)+"/upgrade", req, &v)
	v.Raw = raw
	if v.Plan.Raw == nil {
		var w struct {
			Plan json.RawMessage `json:"plan"`
		}
		if json.Unmarshal(raw, &w) == nil {
			v.Plan.Raw = w.Plan
		}
	}
	return v, err
}

// AppRollback calls POST /v1/apps/{app}/upgrade/rollback (the path
// /v1/apps/{app}/rollback is the one that goes back to the previous
// deployment; with a "plan" or "confirm" in its body it answers this too).
func (c *Client) AppRollback(ctx context.Context, app string, req AppRollbackRequest) (AppRollback, error) {
	var v AppRollback
	raw, err := c.getRaw(ctx, http.MethodPost, "/v1/apps/"+url.PathEscape(app)+"/upgrade/rollback", req, &v)
	v.Raw = raw
	return v, err
}

var _ AppUpgradeAPI = (*Client)(nil)
