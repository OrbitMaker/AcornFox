package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Without a fetcher LoadPackage only reads: a missing package must not
// leave directories behind (the catalog list asks for every app's icon).
func TestLoadPackageWithoutFetchWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private", "cat-x", "packages")
	pkg := PackageRef{Path: "apps/demo/demo-1.0.0.tar.gz", SHA256: strings.Repeat("a", 64), Size: 10}
	if _, err := LoadPackage(nil, nil, dir, pkg); err == nil {
		t.Fatal("missing package must fail")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("read-only load created %s (err %v)", dir, err)
	}
}
