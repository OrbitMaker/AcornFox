package catalog

import (
	"embed"
	"io/fs"
)

// The built-in snapshot lets the app center browse the catalog with no
// network. It is the output of Build without a signature (the binary itself
// is the trust anchor).
//
// For now it is a catalog with no apps: the service type definitions only
// (snapshotsrc/), at serial 4. This platform installs no app of a former
// manifest format, and no app has been published in the current one yet;
// the serial is above the last catalog published in the former format (3),
// so that one is never synced over it. Before a release it is regenerated
// from the catalog as published again in the current format, at that
// publish's serial, which must be above 4. See snapshot/README.md.
//
// Regenerate it (from this directory, as go generate runs it):
//
//	go run ./gensnapshot -src snapshotsrc -serial 4 -out snapshot
//
//go:generate go run ./gensnapshot -src snapshotsrc -serial 4 -out snapshot

//go:embed snapshot
var snapshotFS embed.FS

// Snapshot returns the built-in catalog snapshot (version.txt, index.json,
// apps/<id>/<version>.tar.gz).
func Snapshot() fs.FS {
	sub, err := fs.Sub(snapshotFS, "snapshot")
	if err != nil {
		panic(err) // the directory is embedded at build time
	}
	return sub
}
