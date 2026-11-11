package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/acornfox/acornfox/internal/installtiming"
)

// App center (GET/POST /v1/catalog…). The methods live outside API so the
// existing test fakes keep compiling; the CLI checks for AppCenterAPI and
// reports an old server otherwise.

// AppCenterAPI is implemented by *Client.
type AppCenterAPI interface {
	Catalog(ctx context.Context) (CatalogList, error)                            // GET  /v1/catalog
	CatalogApp(ctx context.Context, id string) (CatalogApp, error)               // GET  /v1/catalog/apps/{id}
	CatalogSync(ctx context.Context) (CatalogSync, error)                        // POST /v1/catalog/sync
	Install(ctx context.Context, req InstallRequest) (InstallTask, error)        // POST /v1/catalog/install
	InstallTask(ctx context.Context, id string) (InstallTask, error)             // GET  /v1/catalog/install/{task}
	FirstLogin(ctx context.Context, app string, reveal bool) (FirstLogin, error) // GET /v1/apps/{app}/first-login
}

// CatalogSync mirrors the sync status.
type CatalogSync struct {
	At      *time.Time `json:"at,omitempty"`
	OK      bool       `json:"ok"`
	Skipped bool       `json:"skipped"`
	Updated bool       `json:"updated"`
	Code    string     `json:"code,omitempty"`
	Message string     `json:"message"`
	Source  string     `json:"source,omitempty"`
	Next    *time.Time `json:"next,omitempty"`
	// Private is the private catalogs' part, passed through as the server
	// sent it.
	Private json.RawMessage `json:"private,omitempty"`
}

// PrivateCatalogAPI is implemented by *Client: private catalogs (W10) and
// picking the catalog an app comes from. It is separate from AppCenterAPI so
// existing test fakes keep compiling.
type PrivateCatalogAPI interface {
	CatalogConnect(ctx context.Context, code string) (CatalogConnected, error)  // POST /v1/catalog/connect
	CatalogSources(ctx context.Context) (CatalogSources, error)                 // GET  /v1/catalog/sources
	CatalogAppFrom(ctx context.Context, id, catalog string) (CatalogApp, error) // GET  /v1/catalog/apps/{id}?catalog=
}

// PlatformVarsAPI is implemented by *Client: the platform variables apps
// can use (separate interface so existing fakes keep compiling).
type PlatformVarsAPI interface {
	PlatformVars(ctx context.Context) (PlatformVars, error) // GET /v1/catalog/platform-vars
}

// AppVarsAPI is implemented by *Client: the variables an installed catalog
// app was configured with (separate interface so existing fakes keep
// compiling).
type AppVarsAPI interface {
	AppVars(ctx context.Context, app string, reveal bool) (AppVars, error) // GET /v1/apps/{app}/vars
}

// AppVars mirrors GET /v1/apps/{app}/vars (--json prints the server's answer
// as is).
type AppVars struct {
	App         string    `json:"app"`
	FromCatalog bool      `json:"from_catalog"`
	CatalogID   string    `json:"catalog_id"`
	Version     string    `json:"version"`
	Source      SourceRef `json:"source"`
	Manifest    struct {
		Saved       bool   `json:"saved"`
		Source      string `json:"source"`
		SourceLabel string `json:"source_label"`
	} `json:"manifest"`
	Revealed  bool `json:"revealed"`
	CanChange bool `json:"can_change"`
	Vars      []struct {
		Name          string `json:"name"`
		Layer         string `json:"layer"`
		LayerLabel    string `json:"layer_label"`
		Class         string `json:"class"`
		ClassLabel    string `json:"class_label"`
		Label         string `json:"label"`
		Value         string `json:"value"`
		Secret        bool   `json:"secret"`
		Masked        bool   `json:"masked"`
		Origin        string `json:"origin"`
		OriginLabel   string `json:"origin_label"`
		Editable      string `json:"editable"`
		EditableLabel string `json:"editable_label"`
		Used          *bool  `json:"used"`
	} `json:"vars"`
	Notes []string        `json:"notes"`
	Raw   json.RawMessage `json:"-"`
}

// PlatformVars mirrors GET /v1/catalog/platform-vars.
type PlatformVars struct {
	Vars []PlatformVar   `json:"vars"`
	Raw  json.RawMessage `json:"-"`
}

// PlatformVar is one platform variable with this machine's value.
type PlatformVar struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Standalone string `json:"standalone"`
	Cloud      string `json:"cloud"`
	UserCanSet bool   `json:"user_can_set"`
	Secret     bool   `json:"secret,omitempty"`
	Issued     bool   `json:"issued,omitempty"`
	ViaParam   bool   `json:"via_param,omitempty"`
	Since      string `json:"since,omitempty"`
	Value      string `json:"value,omitempty"`
	Available  bool   `json:"available"`
	Note       string `json:"note,omitempty"`
}

// SourceRef names the catalog an app or recipe comes from.
type SourceRef struct {
	ID   string `json:"id"` // "public" or a private catalog id
	Name string `json:"name"`
}

// CatalogConnected mirrors POST /v1/catalog/connect.
type CatalogConnected struct {
	Catalog SourceRef `json:"catalog"`
	Again   bool      `json:"again"`
	Sync    *struct {
		OK      bool   `json:"ok"`
		Code    string `json:"code,omitempty"`
		Message string `json:"message"`
	} `json:"sync,omitempty"`
	Raw json.RawMessage `json:"-"`
}

// CatalogSource is one catalog in GET /v1/catalog/sources.
type CatalogSource struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Name           string     `json:"name"`
	Serial         int64      `json:"serial"`
	Origin         string     `json:"origin,omitempty"`
	Apps           int        `json:"apps"`
	Recipes        int        `json:"recipes"`
	ConnectedAt    *time.Time `json:"connected_at,omitempty"`
	LastSync       *time.Time `json:"last_sync,omitempty"`
	Code           string     `json:"code,omitempty"`
	Message        string     `json:"message,omitempty"`
	Disconnected   bool       `json:"disconnected"`
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"`
}

// CatalogSources mirrors GET /v1/catalog/sources.
type CatalogSources struct {
	Sources []CatalogSource `json:"sources"`
	Raw     json.RawMessage `json:"-"`
}

// CatalogList mirrors GET /v1/catalog.
type CatalogList struct {
	Serial int64       `json:"serial"`
	Origin string      `json:"origin"`
	Sync   CatalogSync `json:"sync"`
	Apps   []struct {
		ID        string    `json:"id"`
		Version   string    `json:"version"`
		Name      string    `json:"name"`
		Summary   string    `json:"summary"`
		Category  string    `json:"category"`
		Supported bool      `json:"supported"`
		Installed []string  `json:"installed"`
		Source    SourceRef `json:"source"`
		Command   string    `json:"command"`
		MemoryMB  struct {
			Min         int `json:"min"`
			Recommended int `json:"recommended"`
		} `json:"memory_mb"`
		// NeedsPlatform: upgrade AcornFox to this version before installing
		// ("" when this server can install it).
		NeedsPlatform string `json:"needs_platform"`
	} `json:"apps"`
	Recipes []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Summary string `json:"summary"`
		Apps    []struct {
			App string `json:"app"`
		} `json:"apps"`
		Source        SourceRef `json:"source"`
		Command       string    `json:"command"`
		NeedsPlatform string    `json:"needs_platform"`
	} `json:"recipes"`
	Raw json.RawMessage `json:"-"`
}

// CatalogApp mirrors GET /v1/catalog/apps/{id} (the parts the CLI prints;
// --json prints the server's answer as is).
type CatalogApp struct {
	ID         string    `json:"id"`
	Version    string    `json:"version"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary"`
	License    string    `json:"license"`
	Homepage   string    `json:"homepage"`
	SourceCode string    `json:"source_code,omitempty"`
	Image      string    `json:"image"`
	Port       int       `json:"port"`
	Exposure   string    `json:"exposure"`
	Supported  bool      `json:"supported"`
	SizeMB     int       `json:"size_mb"`
	Installed  []string  `json:"installed"`
	Source     SourceRef `json:"source"`
	Command    string    `json:"command"`
	// NeedsPlatform as in the list ("" when installable on this server).
	NeedsPlatform string `json:"needs_platform"`
	MemoryMB      struct {
		Min         int `json:"min"`
		Recommended int `json:"recommended"`
	} `json:"memory_mb"`
	Delivery struct {
		Tier      string   `json:"tier"`
		Minutes   int      `json:"minutes"`
		UserSteps []string `json:"user_steps"`
	} `json:"delivery"`
	// Platform is the platform variables the app uses, with this
	// machine's values.
	Platform []PlatformVar `json:"platform"`
	Params   []struct {
		Key       string `json:"key"`
		Type      string `json:"type"`
		Label     string `json:"label"`
		Hint      string `json:"hint"`
		Default   any    `json:"default"`
		Required  bool   `json:"required"`
		Generated bool   `json:"generated"`
		LLM       *struct {
			Own   struct{ Available bool } `json:"own"`
			Local struct {
				Available bool     `json:"available"`
				Message   string   `json:"message"`
				Apps      []string `json:"apps"`
			} `json:"local_gateway"`
			Platform struct {
				Available bool   `json:"available"`
				Message   string `json:"message"`
			} `json:"platform"`
		} `json:"llm"`
	} `json:"params"`
	Raw json.RawMessage `json:"-"`
}

// InstallRequest mirrors POST /v1/catalog/install.
type InstallRequest struct {
	App     string         `json:"app,omitempty"`
	Recipe  string         `json:"recipe,omitempty"`
	Catalog string         `json:"catalog,omitempty"` // "" | public | cat-…
	Params  map[string]any `json:"params,omitempty"`
	// Platform changes platform variables marked user_can_set
	// (--set platform.tz=…), for every app of the request.
	Platform             map[string]string `json:"platform,omitempty"`
	Name                 string            `json:"name,omitempty"`
	Version              string            `json:"version,omitempty"`
	KeepVolumesOnFailure bool              `json:"keep_volumes_on_failure,omitempty"`
	ReuseData            bool              `json:"reuse_data,omitempty"` // take over the data volumes an earlier app of this name left
}

// InstallTask mirrors an install task.
type InstallTask struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	Status      string `json:"status"` // running | succeeded | failed
	Step        string `json:"step,omitempty"`
	KeepVolumes bool   `json:"keep_volumes_on_failure,omitempty"`
	Steps       []struct {
		App        string                  `json:"app"`
		Name       string                  `json:"name"`
		Label      string                  `json:"label"`
		Status     string                  `json:"status"`
		Message    string                  `json:"message,omitempty"`
		StartedAt  *time.Time              `json:"started_at,omitempty"`
		FinishedAt *time.Time              `json:"finished_at,omitempty"`
		Progress   *installtiming.Progress `json:"progress,omitempty"`
	} `json:"steps"`
	Apps []struct {
		App       string   `json:"app"`
		CatalogID string   `json:"catalog_id"`
		Version   string   `json:"version"`
		Reused    bool     `json:"reused"`
		Created   bool     `json:"created"`
		URL       string   `json:"url,omitempty"`
		Volumes   []string `json:"volumes,omitempty"`
		// GatewayToken: this task issued the app a token in a local model
		// gateway (New API); the token has no quota and no expiry.
		GatewayToken *struct {
			Provider string `json:"provider"`
		} `json:"gateway_token,omitempty"`
		// Installed on old data (reuse_data): the old volumes taken over,
		// what was carried over, one line each (also, after any install,
		// what became of tokens an old app of the name left), and the old
		// app's gateway token when it was taken over (also no quota and no
		// expiry).
		ReusedVolumes []string `json:"reused_volumes,omitempty"`
		Carried       []string `json:"carried,omitempty"`
		TakenToken    *struct {
			Provider string `json:"provider"`
		} `json:"taken_gateway_token,omitempty"`
	} `json:"apps"`
	Fault *struct {
		Code          string `json:"code"`
		Message       string `json:"message"`
		Hint          string `json:"hint,omitempty"`
		Diagnosis     any    `json:"diagnosis,omitempty"`
		NeedsPlatform string `json:"needs_platform,omitempty"`
	} `json:"fault,omitempty"`
	FirstLogin []FirstLogin `json:"first_login,omitempty"`
	UserSteps  []string     `json:"user_steps,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
	// Timing is the server's verdict on the step times (v0.5.1 on; nil from
	// an older server).
	Timing *installtiming.Verdict `json:"timing,omitempty"`
	Raw    json.RawMessage        `json:"-"`
}

// FirstLogin mirrors the first-login information of one app.
type FirstLogin struct {
	App    string `json:"app"`
	URL    string `json:"url,omitempty"`
	Fields []struct {
		Label  string `json:"label"`
		Value  string `json:"value"`
		Secret bool   `json:"secret,omitempty"`
	} `json:"fields"`
}

func (c *Client) getRaw(ctx context.Context, method, path string, body any, out any) (json.RawMessage, error) {
	var raw json.RawMessage
	if _, err := c.doJSON(ctx, method, path, body, &raw); err != nil {
		return nil, err
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// Catalog calls GET /v1/catalog.
func (c *Client) Catalog(ctx context.Context) (CatalogList, error) {
	var v CatalogList
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/catalog", nil, &v)
	v.Raw = raw
	return v, err
}

// CatalogApp calls GET /v1/catalog/apps/{id}.
func (c *Client) CatalogApp(ctx context.Context, id string) (CatalogApp, error) {
	var v CatalogApp
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/catalog/apps/"+url.PathEscape(id), nil, &v)
	v.Raw = raw
	return v, err
}

// CatalogSync calls POST /v1/catalog/sync.
func (c *Client) CatalogSync(ctx context.Context) (CatalogSync, error) {
	var v CatalogSync
	_, err := c.doJSON(ctx, http.MethodPost, "/v1/catalog/sync", struct{}{}, &v)
	return v, err
}

// Install calls POST /v1/catalog/install.
func (c *Client) Install(ctx context.Context, req InstallRequest) (InstallTask, error) {
	var v InstallTask
	raw, err := c.getRaw(ctx, http.MethodPost, "/v1/catalog/install", req, &v)
	v.Raw = raw
	return v, err
}

// InstallTask calls GET /v1/catalog/install/{task}.
func (c *Client) InstallTask(ctx context.Context, id string) (InstallTask, error) {
	var v InstallTask
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/catalog/install/"+url.PathEscape(id), nil, &v)
	v.Raw = raw
	return v, err
}

// FirstLogin calls GET /v1/apps/{app}/first-login.
func (c *Client) FirstLogin(ctx context.Context, app string, reveal bool) (FirstLogin, error) {
	var v FirstLogin
	p := "/v1/apps/" + url.PathEscape(app) + "/first-login"
	if reveal {
		p += "?reveal=true"
	}
	_, err := c.doJSON(ctx, http.MethodGet, p, nil, &v)
	return v, err
}

// CatalogConnect calls POST /v1/catalog/connect. The code goes only in the
// request body.
func (c *Client) CatalogConnect(ctx context.Context, code string) (CatalogConnected, error) {
	var v CatalogConnected
	raw, err := c.getRaw(ctx, http.MethodPost, "/v1/catalog/connect", map[string]string{"code": code}, &v)
	v.Raw = raw
	return v, err
}

// CatalogSources calls GET /v1/catalog/sources.
func (c *Client) CatalogSources(ctx context.Context) (CatalogSources, error) {
	var v CatalogSources
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/catalog/sources", nil, &v)
	v.Raw = raw
	return v, err
}

// PlatformVars calls GET /v1/catalog/platform-vars.
func (c *Client) PlatformVars(ctx context.Context) (PlatformVars, error) {
	var v PlatformVars
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/catalog/platform-vars", nil, &v)
	v.Raw = raw
	return v, err
}

// AppVars calls GET /v1/apps/{app}/vars.
func (c *Client) AppVars(ctx context.Context, app string, reveal bool) (AppVars, error) {
	var v AppVars
	p := "/v1/apps/" + url.PathEscape(app) + "/vars"
	if reveal {
		p += "?reveal=true"
	}
	raw, err := c.getRaw(ctx, http.MethodGet, p, nil, &v)
	v.Raw = raw
	return v, err
}

// CatalogAppFrom calls GET /v1/catalog/apps/{id}?catalog=… ("" = any).
func (c *Client) CatalogAppFrom(ctx context.Context, id, catalog string) (CatalogApp, error) {
	if catalog == "" {
		return c.CatalogApp(ctx, id)
	}
	var v CatalogApp
	raw, err := c.getRaw(ctx, http.MethodGet, "/v1/catalog/apps/"+url.PathEscape(id)+"?catalog="+url.QueryEscape(catalog), nil, &v)
	v.Raw = raw
	return v, err
}

var (
	_ AppCenterAPI      = (*Client)(nil)
	_ PrivateCatalogAPI = (*Client)(nil)
	_ AppVarsAPI        = (*Client)(nil)
)
