//go:build !windows

package client

import (
	"errors"
	"syscall"
)

// detachAttr starts a detached tunnel in a session of its own: no
// controlling terminal, out of the process group of whoever ran the
// command, so closing that terminal or workbench does not end it.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// processAlive reports whether process pid exists (signal 0 checks without
// sending anything; EPERM means it exists but belongs to someone else).
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
