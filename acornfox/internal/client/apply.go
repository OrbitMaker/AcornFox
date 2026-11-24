package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Changing an installed app's settings and applying them again (server side:
// internal/appinstall/reapply.go). Separate interfaces so existing test
// fakes keep compiling.

// AppApplyAPI is implemented by *Client.
type AppApplyAPI interface {
	// AppSet changes settings and applies them (PATCH /v1/apps/{app}/vars);
	// with req.Plan it only says what would happen.
	AppSet(ctx context.Context, app string, req AppSetRequest) (AppApply, error)
	// AppReapply applies the settings again (POST /v1/apps/{app}/reapply).
	AppReapply(ctx context.Context, app string, req AppReapplyRequest) (AppApply, error)
	// PlatformAffected lists the apps that are behind, or read a variable
	// (GET /v1/platform/affected?var=).
	PlatformAffected(ctx context.Context, name string) (Affected, error)
}

// DomainApplyAPI is implemented by *Client: adding and removing an app's
// domain with the answer to what that does to the app's own settings.
type DomainApplyAPI interface {
	AddDomainApply(ctx context.Context, app, name string, o ApplyOptions) (Domain, *ApplyFollow, error)
	RemoveDomainApply(ctx context.Context, app, name string, o ApplyOptions) (*ApplyFollow, error)
}

// ApplyOptions says how a change of an app's address follows into the app's
// own settings: ConfirmRestart (the user agreed that the app restarts) or
// NoReapply (leave its settings as they are).
type ApplyOptions struct {
	ConfirmRestart bool
	NoReapply      bool
}

// AppSetRequest is PATCH /v1/apps/{app}/vars.
type AppSetRequest struct {
	Set            map[string]any `json:"set,omitempty"`
	Reset          []string       `json:"reset,omitempty"`
	Plan           bool           `json:"plan,omitempty"`
	Force          bool           `json:"force,omitempty"`
	ConfirmRestart bool           `json:"confirm_restart,omitempty"`
}

// AppReapplyRequest is POST /v1/apps/{app}/reapply.
type AppReapplyRequest struct {
	Plan           bool   `json:"plan,omitempty"`
	Force          bool   `json:"force,omitempty"`
	ConfirmRestart bool   `json:"confirm_restart,omitempty"`
	AssumeExposure string `json:"assume_exposure,omitempty"`
}

// ApplyVar is one value that changes.
type ApplyVar struct {
	Name   string `json:"name"`
	Label  string `json:"label,omitempty"`
	Old    string `json:"old"`
	New    string `json:"new"`
	Secret bool   `json:"secret,omitempty"`
	From   string `json:"from"`
}

// ApplyStep is a group of init steps that run.
type ApplyStep struct {
	When  string   `json:"when"`
	Names []string `json:"names"`
	Watch []string `json:"watch,omitempty"`
}

// ApplyPlan mirrors appinstall.ReapplyPlan.
type ApplyPlan struct {
	App        string      `json:"app"`
	Needed     bool        `json:"needed"`
	Restart    bool        `json:"restart"`
	Vars       []ApplyVar  `json:"vars"`
	Env        []string    `json:"env"`
	EnvRemoved []string    `json:"env_removed"`
	Command    bool        `json:"command"`
	Files      []string    `json:"files"`
	Steps      []ApplyStep `json:"steps"`
	Notes      []string    `json:"notes"`
	Summary    string      `json:"summary"`
	Dependents []string    `json:"dependents,omitempty"`
}

// AppApply is the answer of AppSet and AppReapply. Task is set when
// something is being done (an install task of kind reapply).
type AppApply struct {
	Changed bool            `json:"changed"`
	Plan    ApplyPlan       `json:"plan"`
	Task    *InstallTask    `json:"task,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

// ApplyFollow is what came of the apply that follows an address change.
type ApplyFollow struct {
	Plan ApplyPlan    `json:"plan"`
	Task *InstallTask `json:"task,omitempty"`
	Note string       `json:"note,omitempty"`
}

// AffectedApp mirrors appinstall.AffectedApp.
type AffectedApp struct {
	App       string     `json:"app"`
	CatalogID string     `json:"catalog_id"`
	Uses      bool       `json:"uses"`
	Differs   bool       `json:"differs"`
	Vars      []ApplyVar `json:"vars"`
	Restart   bool       `json:"restart"`
	Blocked   string     `json:"blocked,omitempty"`
	Summary   string     `json:"summary"`
}

// Affected mirrors GET /v1/platform/affected.
type Affected struct {
	Var   string          `json:"var"`
	Apps  []AffectedApp   `json:"apps"`
	Notes []string        `json:"notes"`
	Raw   json.RawMessage `json:"-"`
}

// AppSet calls PATCH /v1/apps/{app}/vars.
func (c *Client) AppSet(ctx context.Context, app string, req AppSetRequest) (AppApply, error) {
	var v AppApply
	raw, err := c.getRaw(ctx, http.MethodPatch, "/v1/apps/"+url.PathEscape(app)+"/vars", req, &v)
	v.Raw = raw
	return v, err
}

// AppReapply calls POST /v1/apps/{app}/reapply.
func (c *Client) AppReapply(ctx context.Context, app string, req AppReapplyRequest) (AppApply, error) {
	var v AppApply
	raw, err := c.getRaw(ctx, http.MethodPost, "/v1/apps/"+url.PathEscape(app)+"/reapply", req, &v)
	v.Raw = raw
	return v, err
}

// PlatformAffected calls GET /v1/platform/affected.
func (c *Client) PlatformAffected(ctx context.Context, name string) (Affected, error) {
	var v Affected
	p := "/v1/platform/affected"
	if name != "" {
		p += "?var=" + url.QueryEscape(name)
	}
	raw, err := c.getRaw(ctx, http.MethodGet, p, nil, &v)
	v.Raw = raw
	return v, err
}

// AddDomainApply is AddDomain that also says how the app's own settings
// follow (POST /v1/apps/{app}/domains with confirm_restart or reapply skip).
func (c *Client) AddDomainApply(ctx context.Context, app, name string, o ApplyOptions) (Domain, *ApplyFollow, error) {
	path := "/v1/apps/" + url.PathEscape(app) + "/domains"
	body := map[string]any{"name": name}
	if o.ConfirmRestart {
		body["confirm_restart"] = true
	}
	if o.NoReapply {
		body["reapply"] = "skip"
	}
	var resp struct {
		Domain   Domain       `json:"domain"`
		Warnings []Diagnosis  `json:"warnings"`
		Reapply  *ApplyFollow `json:"reapply"`
	}
	if _, err := c.doJSON(ctx, http.MethodPost, path, body, &resp); err != nil {
		return Domain{}, nil, err
	}
	d := resp.Domain
	if len(resp.Warnings) > 0 {
		d.Warnings = resp.Warnings
	}
	return d, resp.Reapply, nil
}

// RemoveDomainApply is RemoveDomain that also says how the app's own
// settings follow.
func (c *Client) RemoveDomainApply(ctx context.Context, app, name string, o ApplyOptions) (*ApplyFollow, error) {
	q := url.Values{}
	if o.ConfirmRestart {
		q.Set("confirm_restart", "true")
	}
	if o.NoReapply {
		q.Set("reapply", "skip")
	}
	path := "/v1/apps/" + url.PathEscape(app) + "/domains/" + url.PathEscape(name)
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var resp struct {
		Reapply *ApplyFollow `json:"reapply"`
	}
	if _, err := c.doJSON(ctx, http.MethodDelete, path, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Reapply, nil
}

var (
	_ AppApplyAPI    = (*Client)(nil)
	_ DomainApplyAPI = (*Client)(nil)
)
