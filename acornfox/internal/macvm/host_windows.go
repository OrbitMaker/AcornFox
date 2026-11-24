//go:build windows

package macvm

// FreeDiskGB implements Host (not used on Windows).
func (RealHost) FreeDiskGB(path string) int { return -1 }
