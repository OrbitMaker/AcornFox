//go:build !linux

package hostcheck

import "errors"

// FreeBytes is only implemented on Linux (the server checks run there).
func FreeBytes(string) (uint64, error) { return 0, errors.New("只在 Linux 上检查磁盘空间") }

// Device is only implemented on Linux.
func Device(string) uint64 { return 0 }
