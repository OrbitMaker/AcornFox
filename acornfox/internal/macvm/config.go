// Package macvm is AcornFox on a Mac: the same Linux program as on a server,
// running in a Debian virtual machine that Apple's `container` (container
// machine) opens. The Mac itself only runs Apple's `container`, a launchd
// agent (acornfox mac daemon) and the small acornfox command.
//
// The daemon (daemon.go) listens on the Mac for the ports the virtual machine
// serves and passes each connection on to it. It starts the machine when
// someone connects, stops it when nobody did for a while (the only way to give
// its memory back to macOS), and follows the machine's address, which changes
// at every start. The installer (install.go) puts everything in place; the
// commands are in cmd/acornfox/mac.go (acornfox mac install|status|doctor|
// start|stop|set|uninstall).
//
// Nothing here is Mac-only code: Apple's tools are run through the Runner
// interface, so the logic is tested on Linux with a fake one.
package macvm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Defaults of the Mac install (the one table; docs and the Skill point here).
const (
	DefaultVM      = "acornfox"         // name of the virtual machine; ACORNFOX_MAC_VM changes it (tests)
	DefaultSSHPort = 18808              // Mac port (127.0.0.1 only) that leads to the machine's sshd
	DefaultIdleMin = 30                 // minutes without any access before the machine is stopped; 0 = never
	DefaultLabel   = "com.acornfox.mac" // launchd label of the daemon
	BaseImage      = "acornfox-base:debian13"
	LANConsolePort = 18802 // the password-protected console of the machine
	AppPortMin     = 18810
	AppPortMax     = 18899
	// MemoryBigGB is the machine's memory on a Mac with 16 GB or more; MemorySmallGB below that.
	MemoryBigGB   = 6
	MemorySmallGB = 4
	DefaultCPUs   = 4
	// MinFreeDiskGB is the free space the installer wants before it downloads anything.
	MinFreeDiskGB = 12
	// MinMacOSMajor: Apple's container needs macOS 26.
	MinMacOSMajor = 26
)

// Managed says whether the daemon looks after a machine port: the LAN console
// and the apps' public ports.
func Managed(port int) bool {
	return port == LANConsolePort || (port >= AppPortMin && port <= AppPortMax)
}

// Paths are the places the Mac side keeps things, all under one root folder:
// ~/Library/Application Support/AcornFox/mac (ACORNFOX_MAC_HOME changes it).
type Paths struct{ Root string }

// DefaultRoot is the state folder.
func DefaultRoot() string {
	if v := strings.TrimSpace(os.Getenv("ACORNFOX_MAC_HOME")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	return filepath.Join(home, "Library", "Application Support", "AcornFox", "mac")
}

func (p Paths) join(parts ...string) string {
	return filepath.Join(append([]string{p.Root}, parts...)...)
}

// Config is mac.json.
func (p Paths) Config() string { return p.join("mac.json") }

// Ports is ports.json (the machine's ports the daemon forwards, last known).
func (p Paths) Ports() string { return p.join("ports.json") }

// Bin is the folder of the copy of the program launchd runs.
func (p Paths) Bin() string { return p.join("bin") }

// Program is the acornfox program launchd runs.
func (p Paths) Program() string { return p.join("bin", "acornfox") }

// SSHDir holds the key and known_hosts used for the machine.
func (p Paths) SSHDir() string { return p.join("ssh") }

// SSHKey is the private key of the Mac's account on the machine.
func (p Paths) SSHKey() string { return p.join("ssh", "id_ed25519") }

// KnownHosts is the known_hosts file the acornfox target uses.
func (p Paths) KnownHosts() string { return p.join("ssh", "known_hosts") }

// Logs is the folder of the logs.
func (p Paths) Logs() string { return p.join("logs") }

// DaemonLog is the daemon's log.
func (p Paths) DaemonLog() string { return p.join("logs", "daemon.log") }

// InstallLog is the installer's operation log.
func (p Paths) InstallLog() string { return p.join("logs", "install.log") }

// Downloads is where downloaded files are kept (a finished, checked file is reused).
func (p Paths) Downloads() string { return p.join("downloads") }

// Work is a scratch folder for the installer.
func (p Paths) Work() string { return p.join("work") }

// Config is mac.json: the settings and what the installer made.
type Config struct {
	VM          string `json:"vm"`
	CPUs        int    `json:"cpus"`
	MemoryGB    int    `json:"memory_gb"`
	IdleMinutes int    `json:"idle_minutes"`
	LAN         bool   `json:"lan"`
	SSHPort     int    `json:"ssh_port"`
	Label       string `json:"label"`
	// ContainerBin is the path of Apple's container program found at install time.
	ContainerBin string `json:"container_bin,omitempty"`
	PublicHost   string `json:"public_host,omitempty"`
	Version      string `json:"version,omitempty"`
	InstalledAt  string `json:"installed_at,omitempty"`
	// Created lists what the installer made, so that uninstall removes
	// exactly that and nothing else.
	Created Created `json:"created"`
}

// Created is what the installer made on this Mac.
type Created struct {
	VM        bool   `json:"vm,omitempty"`
	BaseImage bool   `json:"base_image,omitempty"`
	Plist     string `json:"plist,omitempty"`
	CLI       string `json:"cli,omitempty"`
	Target    string `json:"target,omitempty"`
}

// DefaultConfig is the config of a new install on a Mac with totalMem bytes of memory.
func DefaultConfig(totalMem uint64) Config {
	vm := DefaultVM
	if v := strings.TrimSpace(os.Getenv("ACORNFOX_MAC_VM")); v != "" {
		vm = v
	}
	label := DefaultLabel
	if v := strings.TrimSpace(os.Getenv("ACORNFOX_MAC_LABEL")); v != "" {
		label = v
	}
	return Config{
		VM: vm, CPUs: CPUsFor(runtime.NumCPU()), MemoryGB: MemoryFor(totalMem), IdleMinutes: DefaultIdleMin,
		LAN: true, SSHPort: DefaultSSHPort, Label: label,
	}
}

// MemoryFor is the machine's memory (GB) for a Mac with total bytes: 6 GB on
// 16 GB and more, 4 GB below (the machine only gives memory
// back when it stops, so the limit is also what it can take).
func MemoryFor(total uint64) int {
	const gib = 1 << 30
	if total >= 16*gib { // hw.memsize of a "16 GB" Mac is exactly 16 GiB
		return MemoryBigGB
	}
	return MemorySmallGB
}

// CPUsFor is the machine's virtual CPUs: 4, or fewer when the Mac has fewer.
func CPUsFor(n int) int {
	if n >= DefaultCPUs {
		return DefaultCPUs
	}
	if n < 2 {
		return 2
	}
	return n
}

// ErrNotInstalled: there is no mac.json.
var ErrNotInstalled = errors.New("这台 Mac 上没有装 AcornFox（找不到 mac.json）")

// LoadConfig reads mac.json.
func LoadConfig(p Paths) (Config, error) {
	b, err := os.ReadFile(p.Config())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, ErrNotInstalled
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	c.fill()
	return c, nil
}

// fill puts defaults into fields an older file does not have.
func (c *Config) fill() {
	if c.VM == "" {
		c.VM = DefaultVM
	}
	if c.SSHPort == 0 {
		c.SSHPort = DefaultSSHPort
	}
	if c.Label == "" {
		c.Label = DefaultLabel
	}
	if c.CPUs == 0 {
		c.CPUs = DefaultCPUs
	}
	if c.MemoryGB == 0 {
		c.MemoryGB = MemorySmallGB
	}
}

// Save writes mac.json (0600), atomically.
func (c Config) Save(p Paths) error {
	if c.InstalledAt == "" {
		c.InstalledAt = time.Now().Format(time.RFC3339)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(p.Config(), append(b, '\n'), 0o600)
}

func writeFileAtomic(path string, b []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// PortSpec is one machine port the daemon forwards. LAN: other devices on the
// local network may connect too (the app is not "only this computer").
type PortSpec struct {
	Port int  `json:"port"`
	LAN  bool `json:"lan"`
}

// LoadPorts reads ports.json; a missing file is the default (the LAN console).
func LoadPorts(p Paths) []PortSpec {
	b, err := os.ReadFile(p.Ports())
	if err != nil {
		return []PortSpec{{Port: LANConsolePort, LAN: true}}
	}
	var out []PortSpec
	if json.Unmarshal(b, &out) != nil {
		return []PortSpec{{Port: LANConsolePort, LAN: true}}
	}
	var keep []PortSpec
	for _, s := range out {
		if Managed(s.Port) {
			keep = append(keep, s)
		}
	}
	return keep
}

// SavePorts writes ports.json.
func SavePorts(p Paths, ports []PortSpec) error {
	b, err := json.Marshal(ports)
	if err != nil {
		return err
	}
	return writeFileAtomic(p.Ports(), append(b, '\n'), 0o600)
}
