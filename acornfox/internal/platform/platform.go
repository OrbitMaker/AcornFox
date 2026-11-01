// Package platform is the only place that knows which operating system the
// acornfox process runs on. Everything else is the same on Linux, macOS and
// Windows: one long-running acornfox process manages containers through the
// Docker API. This package covers the four things that differ:
//
//  1. how to reach Docker (DOCKER_HOST, the current docker context, then the
//     platform default socket or named pipe) — docker.go;
//  2. how acornfox starts at boot or login (systemd on Linux, a per-user
//     LaunchAgent on macOS, a logon scheduled task on Windows) — autostart_*.go;
//  3. where files live (Paths below);
//  4. what "host metrics" means (Linux reads /proc; macOS and Windows show
//     the Docker virtual machine as reported by the Docker API) — see
//     DesktopMode and internal/apiserver/host_dockervm.go.
//
// Linux keeps the two-process layout (server + runner under separate
// accounts, installed by install.sh). macOS and Windows run a single
// "acornfox serve" process under the logged-in user.
package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// GOOS is runtime.GOOS; tests override it.
var GOOS = runtime.GOOS

// DesktopMode reports whether acornfox runs as a single per-user process
// (macOS, Windows) instead of the Linux server + runner pair.
func DesktopMode() bool { return GOOS == "darwin" || GOOS == "windows" }

// Paths are the file locations for this platform.
type Paths struct {
	DataDir   string // database, uploads, tokens, logs
	APISocket string // trusted API unix socket (CLI talks to it)
	BinDir    string // desktop only: the copy of acornfox the autostart entry runs
	LogFile   string // desktop only: serve log (Linux uses journald)
}

// Env is the environment lookup used by this package; tests override it.
var Env = os.Getenv

// UserHome is os.UserHomeDir; tests override it.
var UserHome = os.UserHomeDir

// DefaultPaths returns the platform file locations:
//
//	Linux    /var/lib/acornfox, /run/acornfox/api.sock
//	macOS    ~/Library/Application Support/AcornFox
//	Windows  %LOCALAPPDATA%\AcornFox
//
// ACORNFOX_DATA_DIR overrides the data directory on every platform (tests,
// several instances on one machine).
func DefaultPaths() Paths {
	if GOOS == "linux" || !DesktopMode() {
		dir := "/var/lib/acornfox"
		if v := Env("ACORNFOX_DATA_DIR"); v != "" {
			dir = v
		}
		return Paths{DataDir: dir, APISocket: "/run/acornfox/api.sock"}
	}
	dir := Env("ACORNFOX_DATA_DIR")
	if dir == "" {
		dir = desktopDataDir()
	}
	return Paths{
		DataDir:   dir,
		APISocket: filepath.Join(dir, "api.sock"),
		BinDir:    filepath.Join(dir, "bin"),
		LogFile:   filepath.Join(dir, "logs", "acornfox.log"),
	}
}

func desktopDataDir() string {
	if GOOS == "windows" {
		base := Env("LOCALAPPDATA")
		if base == "" {
			if home, err := UserHome(); err == nil {
				base = filepath.Join(home, "AppData", "Local")
			}
		}
		return filepath.Join(base, "AcornFox")
	}
	home, _ := UserHome()
	return filepath.Join(home, "Library", "Application Support", "AcornFox")
}

// BinaryName is the executable file name on this platform.
func BinaryName() string {
	if GOOS == "windows" {
		return "acornfox.exe"
	}
	return "acornfox"
}

// DefaultExposure is how a new app is reachable when the user has not chosen:
// Linux servers serve apps on their public port and domains ("public");
// desktops give localhost plus a LAN address ("lan").
func DefaultExposure() string {
	if DesktopMode() {
		return "lan"
	}
	return "public"
}
