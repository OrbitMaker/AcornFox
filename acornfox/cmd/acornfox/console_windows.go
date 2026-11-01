//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

var procFreeConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")

// detachConsole sends output to the log file and lets go of the console
// window the logon task opened, so "acornfox serve --background" keeps
// running without a window.
func detachConsole(f *os.File) {
	os.Stdout, os.Stderr = f, f
	_, _, _ = procFreeConsole.Call()
}
