package macvm

import (
	"context"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// Host is what the installer asks about this Mac. RealHost answers for the
// real one; tests give their own.
type Host interface {
	GOOS() string
	Arch() string
	// MacOSVersion is the product version ("26.4").
	MacOSVersion(ctx context.Context) string
	MemBytes(ctx context.Context) uint64
	FreeDiskGB(path string) int
	// LANIP is this Mac's address on the local network ("" when there is none).
	LANIP() string
	// HasTTY says whether a person can type in a terminal (sudo can ask for a password).
	HasTTY() bool
	// UserConfigTargets is the path of the acornfox command's targets.json.
	TargetsFile() string
}

// RealHost is the Mac the program runs on.
type RealHost struct{ R Runner }

func (h RealHost) runner() Runner {
	if h.R == nil {
		return ExecRunner{}
	}
	return h.R
}

// GOOS implements Host.
func (RealHost) GOOS() string { return runtime.GOOS }

// Arch implements Host.
func (RealHost) Arch() string { return runtime.GOARCH }

// MacOSVersion implements Host.
func (h RealHost) MacOSVersion(ctx context.Context) string {
	res, err := Capture(ctx, h.runner(), nil, "sw_vers", "-productVersion")
	if err != nil || res.Code != 0 {
		return ""
	}
	return strings.TrimSpace(res.Stdout)
}

// MemBytes implements Host.
func (h RealHost) MemBytes(ctx context.Context) uint64 {
	res, err := Capture(ctx, h.runner(), nil, "sysctl", "-n", "hw.memsize")
	if err != nil || res.Code != 0 {
		return 0
	}
	n, _ := strconv.ParseUint(strings.TrimSpace(res.Stdout), 10, 64)
	return n
}

// LANIP implements Host: the first private IPv4 address of a physical-looking
// interface (not the virtual networks of Apple's container, Docker or VPNs).
func (RealHost) LANIP() string {
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	best := ""
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := i.Name
		if !strings.HasPrefix(name, "en") {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || !(ip.IsPrivate()) {
				continue
			}
			if strings.HasPrefix(ip.String(), "192.168.64.") { // Apple's container network
				continue
			}
			if best == "" || name == "en0" {
				best = ip.String()
			}
		}
	}
	return best
}

// HasTTY implements Host.
func (RealHost) HasTTY() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// TargetsFile implements Host.
func (RealHost) TargetsFile() string {
	base := os.Getenv("ACORNFOX_CONFIG_DIR")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return base + "/acornfox/targets.json"
}
