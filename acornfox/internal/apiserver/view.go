package apiserver

import (
	"fmt"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// deploymentView is the deployment as returned right after it was submitted.
// A new deployment is still pending, so it needs no rollback plan, event
// times or logs; see deploymentViewer for the full view.
func deploymentView(d state.Deployment) deploymentOut {
	return (&deploymentViewer{}).view(d)
}

// envKeyView describes one environment variable without ever exposing a secret
// value. Non-secret values are returned; secret values are omitted entirely.
type envKeyView struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret"`
	Value  string `json:"value,omitempty"` // empty and omitted when Secret is true
}

// makeEnvView converts store env vars into the redacted wire form. A secret's
// value is never copied into the result, so it cannot leak through JSON.
func makeEnvView(vars []state.EnvVar) []envKeyView {
	out := make([]envKeyView, 0, len(vars))
	for _, v := range vars {
		item := envKeyView{Key: v.Key, Secret: v.Secret}
		if !v.Secret {
			item.Value = v.Value
		}
		out = append(out, item)
	}
	return out
}

// observedContainer is the best-effort observed state of an app's live
// container. When the runner is unavailable the whole app view carries
// Observed=nil and ObservedState="unavailable".
type observedContainer struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	Running      bool   `json:"running"`
	RestartCount int    `json:"restart_count"`
	HostPort     int    `json:"host_port"`
	StartedAt    string `json:"started_at,omitempty"`
}

func makeObserved(c runner.ContainerInfo) observedContainer {
	return observedContainer{
		Name:         c.Name,
		State:        c.State,
		Running:      c.Running,
		RestartCount: c.RestartCount,
		HostPort:     c.HostPort,
		StartedAt:    c.StartedAt,
	}
}

// appView is the app list/detail wire form.
type appView struct {
	Name              string `json:"name"`
	Desired           string `json:"desired"`
	Port              int    `json:"port"`
	HealthPath        string `json:"health_path"`
	PublicPort        int    `json:"public_port"`
	CurrentDeployment string `json:"current_deployment"`
	MemoryMB          int    `json:"memory_mb"`
	CPUMilli          int    `json:"cpu_milli"`
	URL               string `json:"url"`
	LANURL            string `json:"lan_url,omitempty"` // http://<LAN address>:<port> when reachable from the local network
	Exposure          string `json:"exposure"`          // effective: local | lan | public | afx_cloud | internal
	// ExposureIsDefault: the app has no exposure of its own and follows the
	// platform default (DefaultExposure); PATCH exposure "" goes back to it.
	ExposureIsDefault bool   `json:"exposure_is_default,omitempty"`
	DefaultExposure   string `json:"default_exposure"` // the platform default an app without its own exposure gets
	// LocalOnly: the app only opens on this server (exposure local); URL is
	// then the loopback address, so nobody is sent to open a firewall port
	// for an app that is not listening outside.
	LocalOnly bool `json:"local_only,omitempty"`
	// OpenHint tells a local-only app on a Linux server how the user opens
	// it from their own computer (the loopback URL does not open there).
	OpenHint *openHint `json:"open_hint,omitempty"`
	// InternalOnly: the app is reached over the app network only (exposure
	// internal, a database or a cache installed from the app catalog): it
	// has no URL, no open hint, nothing to open, and its access cannot be
	// changed. InternalNote says so in words, for people and for the AI.
	InternalOnly  bool               `json:"internal_only,omitempty"`
	InternalNote  string             `json:"internal_note,omitempty"`
	ObservedState string             `json:"observed_state"` // running | stopped | missing | unavailable
	Observed      *observedContainer `json:"observed,omitempty"`
	Live          *deploymentOut     `json:"live,omitempty"`
	Latest        *deploymentOut     `json:"latest,omitempty"` // newest deployment (may be pending or failed while Live keeps serving)
	// LiveSeq is the version number ("第 N 版") of the deployment serving
	// now, 0 when none. The list carries it without the full Live view.
	LiveSeq           int            `json:"live_seq,omitempty"`
	RollbackTargetSeq int            `json:"rollback_target_seq"` // version a rollback without --to goes to; 0 = none
	Env               []envKeyView   `json:"env,omitempty"`
	Volumes           []state.Volume `json:"volumes,omitempty"`
	Domains           []state.Domain `json:"domains,omitempty"`
	Addons            []addonView    `json:"addons,omitempty"`
	CatalogID         string         `json:"catalog_id,omitempty"`      // installed from the app catalog
	CatalogVersion    string         `json:"catalog_version,omitempty"` // catalog release.version
	// CatalogUpdate is the newer version of the app the catalog has and this
	// machine can install ("" none); Upgrade is the rest of what an upgrade
	// and a going back to before it need to show (nil for an app that is
	// not from the app center). See upgrade.go.
	CatalogUpdate string                  `json:"catalog_update,omitempty"`
	Upgrade       *appinstall.UpgradeInfo `json:"upgrade,omitempty"`
	// Catalog the app was installed from: "public" or the private catalog
	// id, its name, and whether that private catalog was disconnected (the
	// app keeps running but cannot be installed again or upgraded from it).
	CatalogOrigin       string `json:"catalog_origin,omitempty"`
	CatalogOriginName   string `json:"catalog_origin_name,omitempty"`
	CatalogDisconnected bool   `json:"catalog_disconnected,omitempty"`
	// Services are the app's needs (written the new way in its manifest)
	// and the service each is plugged into ("" when none). Detail view only;
	// left out for apps without such needs.
	Services []appinstall.AppServiceUse `json:"services,omitempty"`
	// LegacyNote: for an app installed with a manifest of the former
	// format, one sentence for the user (it runs as before; the new
	// features need it installed again). Detail view only; left out for
	// any other app.
	LegacyNote string `json:"legacy_note,omitempty"`
	// PlatformDomain is the app's name under the machine's platform domain:
	// whether it opens at it now, and why not (detail view only; left out
	// without a platform domain and for an internal app).
	PlatformDomain *appPlatformDomain `json:"platform_domain,omitempty"`
}

// openHint is how to open a local-only app from the user's own computer.
// Recommend is "lan" when the server has a LAN address and the app has its
// own login (switching it to lan exposure is then offered with a
// confirmation), otherwise "tunnel" (acornfox open on the user's computer,
// an SSH tunnel like the console's).
type openHint struct {
	// LANAddress is the server's private IPv4 on the user's local network;
	// "" on a cloud or other public server.
	LANAddress string `json:"lan_address,omitempty"`
	// LANURL is only set when the app already opens on the LAN, so the
	// address works now. open_hint is only given to apps that do not, so it
	// stays empty here; the address of an exposed app is the view's lan_url.
	LANURL string `json:"lan_url,omitempty"`
	// LANURLAfterExpose is where the app would open after switching it to
	// lan exposure (CanLAN only). It does not open now.
	LANURLAfterExpose string `json:"lan_url_after_expose,omitempty"`
	// Ask is the question to put to the user as it is, word for word, when
	// Recommend is "lan" and the app is not exposed yet: whether to let the
	// computers of the LAN open it, with what that means. AskYesCommand is
	// what the AI runs once the user agrees.
	Ask           string `json:"ask,omitempty"`
	AskYesCommand string `json:"ask_yes_command,omitempty"`
	// HasLogin: the app asks for its own login (a catalog app whose
	// manifest gives first-login credentials). Apps without it are never
	// offered the LAN switch here.
	HasLogin bool `json:"has_login"`
	// CanLAN: offer "let computers on the LAN open it" (LANAddress set and
	// HasLogin).
	CanLAN    bool   `json:"can_lan"`
	Recommend string `json:"recommend"` // lan | tunnel
	// Command opens the app through an SSH tunnel, run on the user's own
	// computer. CommandWhere says so to the user, word for word: run on
	// the server, it only prints the server's own loopback address.
	Command      string `json:"command"`
	CommandWhere string `json:"command_where"`
	// TunnelCommand is the same tunnel without the acornfox command line
	// on the user's computer: plain ssh, the real port filled in; the user
	// puts their own login and the server's address in place of
	// "用户@服务器地址". TunnelURL is what they then open in the browser.
	// TunnelWhere says where it runs, like CommandWhere.
	TunnelCommand string `json:"tunnel_command,omitempty"`
	TunnelWhere   string `json:"tunnel_where,omitempty"`
	TunnelURL     string `json:"tunnel_url,omitempty"`
}

// Where an open hint's two commands run, said to the user as they are.
const (
	openCommandWhere = "在你自己电脑的终端运行（电脑上要装了 acornfox 命令行，并用 --ssh 登记过这台服务器）；在服务器上运行没有用"
	tunnelWhere      = "在你自己电脑的终端运行，保持不关"
)

// appURL builds the public URL for an app: http://<public-host>:<PublicPort>.
func appURL(publicHost string, publicPort int) string {
	if publicHost == "" || publicPort == 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", publicHost, publicPort)
}
