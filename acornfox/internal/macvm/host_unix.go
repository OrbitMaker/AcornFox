//go:build !windows

package macvm

import "golang.org/x/sys/unix"

// FreeDiskGB implements Host: the free space (GB) of the volume path is on.
func (RealHost) FreeDiskGB(path string) int {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return -1
	}
	return int(uint64(st.Bavail) * uint64(st.Bsize) >> 30)
}
