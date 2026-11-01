//go:build !windows

package platform

// EnsureUserPath is only needed on Windows; install-cli.sh puts acornfox on
// PATH on macOS and Linux.
func EnsureUserPath(string) (bool, error) { return false, nil }

// RemoveUserPath is only needed on Windows.
func RemoveUserPath(string) error { return nil }
