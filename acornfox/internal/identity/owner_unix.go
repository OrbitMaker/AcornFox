//go:build !windows

package identity

import (
	"os"
	"path/filepath"
	"syscall"
)

// matchOwner gives the identity directory and its files to the owner of the
// data directory. It matters when root runs a command that creates or
// resets the key (sudo acornfox identity reset): the background service runs
// as the data directory's owner and must still be able to read the key.
// It does nothing unless running as root.
func matchOwner(dataDir string) {
	if os.Geteuid() != 0 {
		return
	}
	fi, err := os.Stat(dataDir)
	if err != nil {
		return
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return
	}
	uid, gid := int(st.Uid), int(st.Gid)
	dir := Dir(dataDir)
	_ = os.Lchown(dir, uid, gid)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.Lchown(filepath.Join(dir, e.Name()), uid, gid)
	}
}
