// Package client is the CLI's typed client of the acornfox server API, over
// SSH (system ssh + "acornfox proxy") or a direct URL for development.
package client

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

// APIVersion is the server API version this client speaks.
const APIVersion = 1

// Target is one server entry from targets.json.
type Target struct {
	Name          string `json:"-"`
	SSH           string `json:"ssh,omitempty"`            // user@host or ~/.ssh/config alias
	Port          int    `json:"port,omitempty"`           // 0 = ssh default
	Identity      string `json:"identity,omitempty"`       // private key path, optional
	RemoteCommand string `json:"remote_command,omitempty"` // default "acornfox proxy"
	URL           string `json:"url,omitempty"`            // direct http://127.0.0.1:port or unix:///run/acornfox/api.sock, bypasses SSH
	// Console is the browser console of a local (unix socket) target, e.g.
	// http://127.0.0.1:18800 on macOS and Windows; empty means that default.
	Console string `json:"console,omitempty"`
}

// DefaultLocalConsole is the console of this computer's acornfox.
const DefaultLocalConsole = "http://127.0.0.1:18800"

// Diagnosis mirrors state.Diagnosis on the wire.
type Diagnosis struct {
	Stage      string `json:"stage"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	LogExcerpt string `json:"log_excerpt,omitempty"`
	Hint       string `json:"hint,omitempty"`
	// App center errors that involve the download gateway: its GW- code and,
	// for "too many tries", the seconds to wait.
	GatewayCode string `json:"gateway_code,omitempty"`
	RetryAfter  int    `json:"retry_after,omitempty"`
	// NeedsPlatform is the AcornFox version to upgrade to first (app center:
	// CAT-INST-08, and CAT-INST-29 when an upgrade is the cure).
	NeedsPlatform string `json:"needs_platform,omitempty"`
}

// Error is returned by every Client method on failure. Connect failures have
// Diag.Stage == "connect" (codes in contract section 1); server error replies
// have Status > 0 and Diag.Stage == "server", Diag.Code = the server error code.
type Error struct {
	Status int
	Diag   Diagnosis
	TaskID string // app center: the running install task (CAT-INST-04)
}

func (e *Error) Error() string { return e.Diag.Stage + "/" + e.Diag.Code + ": " + e.Diag.Message }

// Deployment, Event and App are the server's wire views (apiserver). Unknown
// fields are ignored so the server may add fields.
type Deployment struct {
	ID         string      `json:"id"`
	App        string      `json:"app"`
	Seq        int         `json:"seq"`
	SourceKind string      `json:"source_kind"`
	BasedOnSeq int         `json:"based_on_seq,omitempty"`
	OriginKind string      `json:"origin_kind,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Status     string      `json:"status"`
	Diagnosis  *Diagnosis  `json:"diagnosis,omitempty"`
	Warnings   []Diagnosis `json:"warnings,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`

	// Display texts and rollback facts computed by the server (features
	// rollback_to, deploy_logs, deploy_timing); zero from older servers.
	StatusText      string         `json:"status_text,omitempty"`
	SourceText      string         `json:"source_text,omitempty"`
	DurationSeconds *int           `json:"duration_seconds,omitempty"`
	CanRollback     bool           `json:"can_rollback,omitempty"`
	RollbackBlocked string         `json:"rollback_blocked,omitempty"`
	SameImageAs     int            `json:"same_image_as,omitempty"`
	RollbackText    string         `json:"rollback_text,omitempty"`
	FailureText     string         `json:"failure_text,omitempty"`
	Logs            DeploymentLogs `json:"logs"`
}

// DeploymentLogs says which logs of a deployment can be read.
type DeploymentLogs struct {
	Build bool `json:"build"`
	Run   bool `json:"run"`
}

// DeploymentList mirrors GET /v1/apps/{app}/deployments.
type DeploymentList struct {
	Deployments       []Deployment `json:"deployments"`
	RollbackTargetSeq int          `json:"rollback_target_seq"` // where a rollback without a version goes; 0 = none
	ImageCheck        string       `json:"image_check"`         // "ok", or "unknown" when the server could not list its images
}

// RollbackOptions select the version (0 = the newest one that can be rolled
// back to) and whether an in-progress deployment may be cancelled.
type RollbackOptions struct {
	ToSeq         int
	CancelPending bool
}

// RollbackResult is the rollback reply. Created is false when the server
// returned a deployment it had already created for the same request.
type RollbackResult struct {
	Deployment    Deployment
	Created       bool
	TargetSeq     int // the version rolled back to
	SupersededSeq int // the in-progress version that will be cancelled, 0 = none
}

// DeploymentLogLines mirrors GET /v1/deployments/{id}/logs.
type DeploymentLogLines struct {
	DeploymentID string   `json:"deployment_id"`
	Version      int      `json:"version"`
	Kind         string   `json:"kind"` // run | build
	Lines        []string `json:"lines"`
	Truncated    bool     `json:"truncated"`
	Source       string   `json:"source"` // live | archive
}

// Server features (GET /v1/status "features") a newer CLI checks before
// using them, so an older server is reported instead of misbehaving.
const (
	FeatureRollbackTo   = "rollback_to"
	FeatureDeployLogs   = "deploy_logs"
	FeatureDeployTiming = "deploy_timing"
)

// CodeServerTooOld is the client error code for a route the server does not
// have yet (a plain 404/405 that is not this API's JSON error).
const CodeServerTooOld = "server_too_old"

type Event struct {
	ID      int64     `json:"id"`
	At      time.Time `json:"at"`
	Stage   string    `json:"stage"`
	Message string    `json:"message"`
}

type EnvKey struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret"`
	Value  string `json:"value,omitempty"`
}

type Volume struct {
	Path       string `json:"path"`
	VolumeName string `json:"volume_name"`
	Auto       bool   `json:"auto"`
}

type App struct {
	Name              string `json:"name"`
	Desired           string `json:"desired"`
	Port              int    `json:"port"`
	HealthPath        string `json:"health_path"`
	PublicPort        int    `json:"public_port"`
	CurrentDeployment string `json:"current_deployment"`
	MemoryMB          int    `json:"memory_mb"`
	CPUMilli          int    `json:"cpu_milli"`
	URL               string `json:"url"`
	LANURL            string `json:"lan_url,omitempty"`
	Exposure          string `json:"exposure,omitempty"`
	LocalOnly         bool   `json:"local_only,omitempty"` // only opens on the server itself (URL is 127.0.0.1)
	// OpenHint (local-only apps on a Linux server): how the user opens the
	// app from their own computer.
	OpenHint *OpenHint `json:"open_hint,omitempty"`
	// GatewayTokenFrom (detail view): the local model gateway app that
	// issued this app a token with no quota and no expiry.
	GatewayTokenFrom  string      `json:"gateway_token_from,omitempty"`
	ObservedState     string      `json:"observed_state"`
	Live              *Deployment `json:"live,omitempty"`
	Latest            *Deployment `json:"latest,omitempty"`
	RollbackTargetSeq int         `json:"rollback_target_seq,omitempty"`
	Env               []EnvKey    `json:"env,omitempty"`
	Volumes           []Volume    `json:"volumes,omitempty"`
	Domains           []Domain    `json:"domains,omitempty"`
}

// OpenHint is how to open a local-only app from the user's own computer:
// Recommend is "lan" (the server is on the user's LAN and the app has its
// own login: switching it to lan exposure is offered, with the user's
// consent) or "tunnel" (Command, acornfox open APP, on the user's computer).
type OpenHint struct {
	LANAddress string `json:"lan_address,omitempty"`
	LANURL     string `json:"lan_url,omitempty"`
	HasLogin   bool   `json:"has_login"`
	CanLAN     bool   `json:"can_lan"`
	Recommend  string `json:"recommend"`
	Command    string `json:"command"`
}

type Status struct {
	APIVersion    int    `json:"api_version"`
	Runner        string `json:"runner"`
	DockerVersion string `json:"docker_version"`
	Apps          int    `json:"apps"`
	// Features lists capabilities added after api_version 1 (Feature*).
	Features []string `json:"features,omitempty"`
}

// DeployOptions select the source (exactly one of Upload, Image, Git) and
// optional app settings applied before the deployment.
type DeployOptions struct {
	Upload         io.Reader // tar.gz stream
	UploadSize     int64
	Image          string
	Git            string
	Ref            string
	IdempotencyKey string
	Port           int
	HealthPath     string
}

// AppSettings for PATCH /v1/apps/{app}; nil fields are unchanged.
type AppSettings struct {
	MemoryMB   *int    `json:"memory_mb,omitempty"`
	CPUMilli   *int    `json:"cpu_milli,omitempty"`
	Port       *int    `json:"port,omitempty"`
	HealthPath *string `json:"health_path,omitempty"`
	Exposure   *string `json:"exposure,omitempty"`
}

// AppUpdate mirrors PATCH /v1/apps/{app}. AppliedNow is true when new memory /
// CPU limits were applied to the running container without a restart;
// ApplyError explains why they will only take effect on the next deployment.
type AppUpdate struct {
	App        App    `json:"app"`
	AppliedNow bool   `json:"applied_now,omitempty"`
	ApplyError string `json:"apply_error,omitempty"`
}

// Resources mirrors GET/PUT /v1/resources: the server-wide caps and
// what app and add-on containers have been given.
type Resources struct {
	Memory struct {
		HostMB      int  `json:"host_mb"`
		CapMB       int  `json:"cap_mb"`
		Auto        bool `json:"auto"`
		AllocatedMB int  `json:"allocated_mb"`
	} `json:"memory"`
	CPU struct {
		HostMilli   int  `json:"host_milli"`
		CapMilli    int  `json:"cap_milli"`
		Auto        bool `json:"auto"`
		MaxAppMilli int  `json:"max_app_milli"`
	} `json:"cpu"`
	Items []struct {
		App      string `json:"app"`
		Kind     string `json:"kind"`
		MemoryMB int    `json:"memory_mb"`
		CPUMilli int    `json:"cpu_milli"`
	} `json:"items"`
	MinAppMemoryMB int `json:"min_app_memory_mb"`
	MinAppCPUMilli int `json:"min_app_cpu_milli"`
}

// ResourceCaps for PUT /v1/resources; nil fields are unchanged, 0 = automatic.
type ResourceCaps struct {
	MemoryMB *int `json:"memory_mb,omitempty"`
	CPUMilli *int `json:"cpu_milli,omitempty"`
}

// API is what internal/cli depends on. Connect returns a *Client implementing it.
type API interface {
	Status(ctx context.Context) (Status, error)                                                // also verifies APIVersion (connect/version_mismatch)
	Deploy(ctx context.Context, app string, opt DeployOptions) (Deployment, bool, error)       // bool = newly created (false = duplicate)
	Rollback(ctx context.Context, app string, opts RollbackOptions) (RollbackResult, error)    // ToSeq 0: POST /v1/apps/{app}/rollback; else POST /v1/apps/{app}/versions/{seq}/rollback
	ListDeployments(ctx context.Context, app string, limit int) (DeploymentList, error)        // GET /v1/apps/{app}/deployments?limit=N
	DeploymentLogs(ctx context.Context, id, kind string, tail int) (DeploymentLogLines, error) // GET /v1/deployments/{id}/logs?kind=&tail=
	Features(ctx context.Context) ([]string, error)                                            // GET /v1/status "features"
	Redeploy(ctx context.Context, app string) (string, error)                                  // N4.4: POST /v1/apps/{app}/redeploy, returns deployment_id
	Deployment(ctx context.Context, id string, afterEvent int64) (Deployment, []Event, error)
	Apps(ctx context.Context) ([]App, error)
	App(ctx context.Context, app string) (App, error)
	UpdateApp(ctx context.Context, app string, s AppSettings) (AppUpdate, error)
	Logs(ctx context.Context, app string, tail int) ([]string, error)
	HostMetrics(ctx context.Context) (HostMetrics, error)           // N4.3: GET /v1/metrics/host
	AppMetrics(ctx context.Context, app string) (AppMetrics, error) // N4.3: GET /v1/metrics/apps/{app}
	// AppHistory: GET /v1/metrics/apps/{app}/history?range=1h|24h (read-only).
	AppHistory(ctx context.Context, app, rng string) (AppHistory, error)
	SetEnv(ctx context.Context, app, key, value string, secret bool) error
	UnsetEnv(ctx context.Context, app, key string) error
	AddVolume(ctx context.Context, app, path string) error
	Stop(ctx context.Context, app string) error
	Start(ctx context.Context, app string) error
	Restart(ctx context.Context, app string) error                       // N4: POST /v1/apps/{app}/restart
	DeleteApp(ctx context.Context, app string, deleteVolumes bool) error // N4: DELETE /v1/apps/{app}?volumes=(true|false)
	Close() error                                                        // ends the SSH session

	// N3
	ConsoleToken(ctx context.Context) (string, error)                // POST /v1/console/tokens (trusted socket only)
	Domains(ctx context.Context, app string) ([]Domain, error)       // GET  /v1/apps/{app}/domains
	AddDomain(ctx context.Context, app, name string) (Domain, error) // POST /v1/apps/{app}/domains {"name"}
	RemoveDomain(ctx context.Context, app, name string) error        // DELETE /v1/apps/{app}/domains/{name}

	// N4.2 add-ons
	Addons(ctx context.Context, app string) ([]Addon, error)                                    // GET    /v1/apps/{app}/addons
	AddAddon(ctx context.Context, app, kind string) (AddonResult, error)                        // POST   /v1/apps/{app}/addons {"kind"}
	RemoveAddon(ctx context.Context, app, kind string, deleteVolume bool) (AddonRemoval, error) // DELETE /v1/apps/{app}/addons/{kind}[?volumes=true]

	// Server-wide resource caps
	Resources(ctx context.Context) (Resources, error)                          // GET /v1/resources
	SetResourceCaps(ctx context.Context, caps ResourceCaps) (Resources, error) // PUT /v1/resources

	// Public console (trusted socket only)
	PublicConsoleStatus(ctx context.Context) (PublicConsoleStatus, error)                                       // GET    /v1/public-console
	PublicConsoleVerify(ctx context.Context, domain string) (PublicConsoleVerify, error)                        // POST   /v1/public-console/verify
	PublicConsoleEnable(ctx context.Context, domain, verifyID string, reset bool) (PublicConsoleEnabled, error) // POST   /v1/public-console/enable
	PublicConsoleDisable(ctx context.Context) (bool, error)                                                     // DELETE /v1/public-console
}

// PublicConsoleStatus mirrors GET /v1/public-console.
type PublicConsoleStatus struct {
	Enabled   bool       `json:"enabled"`
	Domain    string     `json:"domain,omitempty"`
	URL       string     `json:"url,omitempty"`
	EnabledAt *time.Time `json:"enabled_at,omitempty"`
	Verifying string     `json:"verifying,omitempty"`
}

// PublicConsoleVerify mirrors POST /v1/public-console/verify: the challenge to
// fetch over HTTP and this server's public addresses.
type PublicConsoleVerify struct {
	Domain   string `json:"domain"`
	VerifyID string `json:"verify_id"`
	Path     string `json:"path"`
	Token    string `json:"token"`
	IPs      struct {
		V4     []string `json:"ipv4,omitempty"`
		V6     []string `json:"ipv6,omitempty"`
		Source string   `json:"source,omitempty"`
	} `json:"ips"`
	ExpiresIn int `json:"expires_in"`
}

// PublicConsoleEnabled mirrors POST /v1/public-console/enable. Password is set
// only when a new one was generated, and only in this one reply.
type PublicConsoleEnabled struct {
	Domain         string `json:"domain"`
	URL            string `json:"url"`
	Password       string `json:"password,omitempty"`
	AlreadyEnabled bool   `json:"already_enabled,omitempty"`
}

// Addon mirrors the server's add-on view. Credentials are never sent.
type Addon struct {
	Kind          string    `json:"kind"`
	Image         string    `json:"image"`
	EnvVar        string    `json:"env_var"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	VolumeName    string    `json:"volume_name"`
	ObservedState string    `json:"observed_state"`
	CreatedAt     time.Time `json:"created_at"`
}

// AddonResult is the POST /v1/apps/{app}/addons reply.
type AddonResult struct {
	Reused       bool   `json:"reused,omitempty"`
	Addon        Addon  `json:"addon"`
	DeploymentID string `json:"deployment_id,omitempty"`
	Note         string `json:"note"`
}

// AddonRemoval is the DELETE /v1/apps/{app}/addons/{kind} reply.
type AddonRemoval struct {
	Kind          string `json:"kind"`
	Removed       bool   `json:"removed"`
	DeploymentID  string `json:"deployment_id,omitempty"`
	VolumeDeleted bool   `json:"volume_deleted"`
	VolumeName    string `json:"volume_name"`
}

// HostMetrics mirrors the host metrics response.
type HostMetrics struct {
	MemoryPercent *float64 `json:"memory_percent,omitempty"`
	DiskPercent   *float64 `json:"disk_percent,omitempty"`
	Available     bool     `json:"available"`
	CPUPercent    *float64 `json:"cpu_percent,omitempty"`
	MemoryUsed    *uint64  `json:"memory_used,omitempty"`
	MemoryTotal   *uint64  `json:"memory_total,omitempty"`
	DiskUsed      *uint64  `json:"disk_used,omitempty"`
	DiskTotal     *uint64  `json:"disk_total,omitempty"`
	Load1         *float64 `json:"load1,omitempty"`
	UptimeSeconds *float64 `json:"uptime_seconds,omitempty"`
}

// AppHistoryPoint is one sample (or 5-minute average) of an app's history; a
// metric that was not readable at that time is nil.
type AppHistoryPoint struct {
	T             time.Time `json:"t"`
	CPUPercent    *float64  `json:"cpu_percent,omitempty"`
	MemoryUsageMB *float64  `json:"memory_usage_mb,omitempty"`
	MemoryLimitMB *float64  `json:"memory_limit_mb,omitempty"`
}

// AppHistory mirrors GET /v1/metrics/apps/{app}/history. Ranges is empty when
// the server keeps no history; Since is the oldest sample kept (the history
// lives in memory and restarts with the service).
type AppHistory struct {
	App         string            `json:"app"`
	Range       string            `json:"range"`
	StepSeconds int               `json:"step_seconds"`
	Ranges      []string          `json:"ranges"`
	Since       *time.Time        `json:"since,omitempty"`
	Points      []AppHistoryPoint `json:"points"`
}

// AppMetrics mirrors the app metrics response.
type AppMetrics struct {
	Available     *bool   `json:"available,omitempty"`
	CPUAvailable  *bool   `json:"cpu_available,omitempty"`
	ObservedState string  `json:"observed_state,omitempty"`
	App           string  `json:"app"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryUsageMB float64 `json:"memory_usage_mb"`
	MemoryLimitMB float64 `json:"memory_limit_mb"`
	NetworkRxMB   float64 `json:"network_rx_mb"`
	NetworkTxMB   float64 `json:"network_tx_mb"`
	PIDs          uint64  `json:"pids"`
}

// Domain mirrors state.Domain on the wire.
type Domain struct {
	App       string     `json:"app"`
	Name      string     `json:"name"`
	Status    string     `json:"status"` // pending | ready | failed
	Diagnosis *Diagnosis `json:"diagnosis,omitempty"`
	// Warnings carries non-fatal diagnoses returned by AddDomain (e.g. a
	// dns_mismatch when the name's A/AAAA record does not point at this server).
	// Additive to the state.Domain shape: it only appears on the POST response
	// and is surfaced so the CLI can print it. See N3 contract section 2.
	Warnings []Diagnosis `json:"warnings,omitempty"`
}

// Raw is used for debugging/--json passthrough when needed.
type Raw = json.RawMessage

/*
Implemented in client.go / ssh.go:

	Connect(ctx context.Context, t Target) (*Client, error)
	    // Direct when t.URL != ""; else starts `ssh -T -o BatchMode=yes -o ServerAliveInterval=15
	    // [-p Port] [-i Identity] SSH RemoteCommand` lazily on first request, wraps its
	    // stdin/stdout as a net.Conn for http.Transport (keep-alive, MaxConnsPerHost=1),
	    // and classifies ssh exit/stderr into connect diagnoses (contract table).
	    // Uses exec.LookPath("ssh") -> connect/ssh_missing.
*/
