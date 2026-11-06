//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

var procFreeConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("FreeConsole")

// consoleFiles keeps the original console *os.File values reachable after
// detachConsole replaces them. Otherwise the garbage collector runs their
// finalizers and closes handle values that Windows has reused after
// FreeConsole: seen as "GetQueuedCompletionStatusEx failed (errno=735)
// fatal error: netpoll failed" (the runtime's I/O completion port was closed)
// a few seconds after the logon task started the server.
var consoleFiles []*os.File

// detachConsole sends output to the log file and lets go of the console
// window the logon task opened, so "acornfox serve --background" keeps
// running without a window.
func detachConsole(f *os.File) {
	consoleFiles = append(consoleFiles, os.Stdin, os.Stdout, os.Stderr)
	os.Stdout, os.Stderr = f, f
	_, _, _ = procFreeConsole.Call()
}
