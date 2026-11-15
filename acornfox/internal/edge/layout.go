package edge

import "time"

// The entry proxy image. Its version is "<proxy version>-<adapter revision>":
// it changes only when the proxy software or the adapter changes, so most
// platform upgrades leave the entry container alone. The release build reads
// ImageVersion from this file (scripts/build-release.sh) and packages the
// image as acornfox-edge_<ImageVersion>_linux_<arch>.tar.gz, a root file
// system the install and upgrade scripts load with docker import as
// ImageRepo:ImageVersion.
const (
	ImageRepo    = "acornfox/edge"
	ImageVersion = "2.8.4-1"
	Image        = ImageRepo + ":" + ImageVersion
)

// The layout inside the entry proxy container: the runner creates the
// container with it and the adapter reads it. Both sides use these
// constants only.
const (
	// AdapterBinary is the container's first process (the adapter); it
	// starts and supervises the proxy software.
	AdapterBinary = "/usr/bin/afx-edge"
	// TokenFile holds the management token (32..128 hex characters). The
	// runner copies it into the container before the first start; it never
	// goes on a command line or into the environment.
	TokenFile = "/etc/afx-edge/token"
	// DataMount is the volume with certificates and ACME accounts.
	DataMount = "/data"
	// SettingsMount is the container's own settings volume; the adapter keeps
	// the last configuration it applied under SettingsMount/afx-edge so a
	// restarted container serves every route again without waiting for the
	// acornfox program.
	SettingsMount = "/config"
	StateDir      = SettingsMount + "/afx-edge"
	// StoreFile is the saved configuration (format Stored). The adapter
	// writes it after every accepted Apply; the runner writes it into a new
	// container before its first start, so the container serves every route
	// from the beginning.
	StoreFile = StateDir + "/config.json"
	// BridgeAdminPort is where the adapter's management endpoint listens
	// inside the container in bridge mode (published on 127.0.0.1 of the
	// host); in host mode it listens on 127.0.0.1:<admin port> directly.
	BridgeAdminPort = 2020
	// GracePeriodSeconds bounds how long the proxy lets running requests finish when
	// it stops. It must stay below the 10 seconds Docker waits before killing
	// the container, so the proxy closes its connections itself.
	GracePeriodSeconds = 8
)

// Stored is the format of StoreFile. Version is written for people reading
// the file; readers recompute it from Config.
type Stored struct {
	Version string    `json:"version"`
	SavedAt time.Time `json:"saved_at"`
	Config  Config    `json:"config"`
}

// Fault codes of the entry proxy (entries in internal/faults; the install,
// upgrade and uninstall scripts use the same codes).
const (
	FaultNotRunning   = "EDGE-01" // no entry container is running
	FaultImageMissing = "EDGE-02" // the entry image is missing or could not be loaded
	FaultAdmin        = "EDGE-03" // the management endpoint does not answer or refuses the token
	FaultNotApplied   = "EDGE-04" // the configuration handed over is not the one in effect
	FaultSwitchFailed = "EDGE-05" // switching to a new entry container failed; the old one serves on
	FaultSwitchBack   = "EDGE-06" // undoing an upgrade could not switch back to the old entry container (upgrade script)
)
