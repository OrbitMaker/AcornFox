package main

import "github.com/acornfox/acornfox/internal/console"

// init wires the embedded console into the server at "/". It lives in its
// own file so that server_runner.go can build even if the console package
// is missing during parallel development: removing this file leaves it nil,
// and the console listener falls back to a placeholder page.
func init() {
	consoleFS = console.FS
}
