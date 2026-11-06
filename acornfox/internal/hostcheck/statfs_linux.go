//go:build linux

package hostcheck

import (
	"os"
	"syscall"
)

// FreeBytes is the space an unprivileged writer can still use on the
// filesystem holding path.
func FreeBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// Device identifies the filesystem holding path (0 when unknown), so two
// directories on the same partition are reported once.
func Device(path string) uint64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Dev)
	}
	return 0
}
