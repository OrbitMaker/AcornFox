//go:build !acornfox_testkeys

package main

import "crypto/ed25519"

// testCatalogKeys is empty in every normal build: only the program's own
// trusted keys verify catalogs. See testkeys_tag.go.
func testCatalogKeys() []ed25519.PublicKey { return nil }

// testAppArch is "" in every normal build: the app center uses this
// machine's architecture. See testkeys_tag.go.
func testAppArch() string { return "" }
