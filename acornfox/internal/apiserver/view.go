package apiserver

import (
	"fmt"

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
	Exposure          string `json:"exposure"`          // effective: local | lan | public | afx_cloud
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
	OpenHint      *openHint          `json:"open_hint,omitempty"`
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
	// Catalog the app was installed from: "public" or the private catalog
	// id, its name, and whether that private catalog was disconnected (the
	// app keeps running but cannot be installed again or upgraded from it).
	CatalogOrigin       string `json:"catalog_origin,omitempty"`
	CatalogOriginName   string `json:"catalog_origin_name,omitempty"`
	CatalogDisconnected bool   `json:"catalog_disconnected,omitempty"`
	// GatewayTokenFrom names the local model gateway app (New API) that
	// issued this app a token when it was installed. The token has no quota
	// and no expiry; the console and the CLI say so. Detail view only.
	GatewayTokenFrom string `json:"gateway_token_from,omitempty"`
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
	// LANURL is where the app opens after switching it to lan exposure.
	LANURL string `json:"lan_url,omitempty"`
	// HasLogin: the app asks for its own login (a catalog app whose
	// manifest gives first-login credentials). Apps without it are never
	// offered the LAN switch here.
	HasLogin bool `json:"has_login"`
	// CanLAN: offer "let computers on the LAN open it" (LANAddress set and
	// HasLogin).
	CanLAN    bool   `json:"can_lan"`
	Recommend string `json:"recommend"` // lan | tunnel
	// Command opens the app through an SSH tunnel, run on the user's own
	// computer.
	Command string `json:"command"`
}

// appURL builds the public URL for an app: http://<public-host>:<PublicPort>.
func appURL(publicHost string, publicPort int) string {
	if publicHost == "" || publicPort == 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", publicHost, publicPort)
}
