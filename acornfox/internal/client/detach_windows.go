//go:build windows

package client

import (
	"os"
	"syscall"
)

// Process creation flags (winbase.h).
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detachAttr starts a detached tunnel with no console and in a process
// group of its own, so closing the window that ran the command does not end
// it.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}

// processAlive reports whether process pid can be opened (it exists).
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
