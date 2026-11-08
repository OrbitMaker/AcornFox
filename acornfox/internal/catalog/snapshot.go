package catalog

import (
	"embed"
	"io/fs"
)

// The built-in snapshot lets the app center browse the catalog with no
// network. It is the output of Build without a signature (the binary itself
// is the trust anchor). Regenerate it from a catalog checkout with:
//
//	cd acornfox && go generate ./internal/catalog/
//
// or point it at another catalog / serial:
//
//	go run ./internal/catalog/gensnapshot -src ../../catalog -serial 7
//
// See snapshot/README.md.
//
//go:generate go run ./gensnapshot -src testdata/good -serial 1 -out snapshot

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
