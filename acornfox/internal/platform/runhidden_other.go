//go:build !windows

package platform

import "os/exec"

// RunHidden runs a command (no console window to hide outside Windows).
var RunHidden = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}
