//go:build acornfox_testkeys

package main

// Test builds only (go build -tags acornfox_testkeys): catalogs signed with a
// throwaway test key are accepted when that key's public half is given in
// ACORNFOX_TEST_CATALOG_PUBKEYS (base64url, comma separated). A normal build
// does not contain this file, so neither the variable nor any test key has an
// effect there. Used by the end-to-end tests of private catalogs.
//
// The same builds also read ACORNFOX_TEST_APP_ARCH (amd64 or arm64): the CPU
// architecture the app center acts for (which image archive it fetches,
// which apps it can install) instead of this machine's. Used to walk the
// "no archive for this architecture" path on an amd64 test machine.

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

func testAppArch() string {
	switch a := strings.TrimSpace(os.Getenv("ACORNFOX_TEST_APP_ARCH")); a {
	case "", "amd64", "arm64":
		return a
	default:
		fmt.Fprintln(os.Stderr, "ACORNFOX_TEST_APP_ARCH: only amd64 or arm64, ignored:", a)
		return ""
	}
}
