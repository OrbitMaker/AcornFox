// Package console embeds the AcornFox web console. Files are embedded at
// build time and never read from disk, so the console cannot be tampered
// with at runtime.
//
// FS is the console (React, built from web/console into ui/). The server
// serves it at "/". ui/ is a build output committed to the repository so
// that building acornfox needs no Node; build-info.json in it records the
// hash of the sources it was built from, and the tests check it against
// web/console.
package console

import (
	"embed"
	"io/fs"
)

//go:embed ui
var embedded embed.FS

// FS is the console's file tree rooted at ui/: index.html at "/",
// assets/... next to it.
var FS fs.FS

func init() {
	s, err := fs.Sub(embedded, "ui")
	if err != nil {
		// The embed directive guarantees the directory exists; a failure
		// here is a build/programming error, not a runtime condition.
		panic("console: embed sub FS ui: " + err.Error())
	}
	FS = s
}
