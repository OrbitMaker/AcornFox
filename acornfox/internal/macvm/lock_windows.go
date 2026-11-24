//go:build windows

package macvm

// LockVM does nothing on Windows (the Mac side is not used there).
func LockVM(p Paths) (func(), error) { return func() {}, nil }
