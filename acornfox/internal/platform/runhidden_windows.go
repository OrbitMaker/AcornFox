//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// RunHidden runs a command without opening a console window (the background
// server has no console; a console program would flash one).
var RunHidden = func(name string, args ...string) ([]byte, error) {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return c.CombinedOutput()
}
