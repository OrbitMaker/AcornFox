//go:build acornfox_testkeys

package main

// Test builds only (go build -tags acornfox_testkeys): catalogs signed with a
// throwaway test key are accepted when that key's public half is given in
// ACORNFOX_TEST_CATALOG_PUBKEYS (base64url, comma separated). A normal build
// does not contain this file, so neither the variable nor any test key has an
// effect there. Used by the end-to-end tests of private catalogs.

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
)

func testCatalogKeys() []ed25519.PublicKey {
	var out []ed25519.PublicKey
	for _, s := range strings.Split(os.Getenv("ACORNFOX_TEST_CATALOG_PUBKEYS"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		k, err := catalog.ParsePublicKey(s)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ACORNFOX_TEST_CATALOG_PUBKEYS:", err)
			continue
		}
		out = append(out, k)
	}
	return out
}
