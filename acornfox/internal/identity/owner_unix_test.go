//go:build !windows

package identity

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// As root, a reset leaves the key owned by the data directory's owner so the
// background service (which runs as that user) can still read it.
func TestResetAsRootKeepsDataDirOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	data := t.TempDir()
	if err := os.Chown(data, 12345, 12345); err != nil {
		t.Fatal(err)
	}
	if _, err := Reset(data); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{Dir(data), KeyPath(data), filepath.Join(Dir(data), infoName)} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st := fi.Sys().(*syscall.Stat_t); st.Uid != 12345 {
			t.Fatalf("%s owned by %d", p, st.Uid)
		}
	}
}
