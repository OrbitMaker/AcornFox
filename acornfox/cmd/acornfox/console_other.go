//go:build !windows

package main

import "os"

// detachConsole is a no-op outside Windows: the LaunchAgent (macOS) or a
// terminal already decides where output goes.
func detachConsole(*os.File) {}
