package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Reading an app package on the client (the app center installs from it).

// maxPackageFile bounds one file inside a package.
const maxPackageFile = 4 << 20

// ReadPackage returns the files of an app package (apps/<id>/<version>.tar.gz
// bytes), keyed by their name inside <id>/. Anything outside <id>/, a
// non-regular entry or an oversize file is an error.
func ReadPackage(id string, b []byte) (map[string][]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(zr)
	out := map[string][]byte{}
	prefix := id + "/"
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg || len(h.Name) <= len(prefix) || h.Name[:len(prefix)] != prefix {
			return nil, fmt.Errorf("应用包里有不该有的条目：%s", h.Name)
		}
		if h.Size > maxPackageFile {
			return nil, fmt.Errorf("应用包里的 %s 太大", h.Name)
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxPackageFile+1))
		if err != nil {
			return nil, err
		}
		out[h.Name[len(prefix):]] = data
	}
	return out, nil
}

// ParseManifest decodes manifest.yaml bytes.
func ParseManifest(b []byte) (*Manifest, error) {
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// LoadPackage returns the bytes of pkg, checked against its sha256 and size:
// from the built-in snapshot when it carries the same package, else from
// pkgDir (a verified copy saved earlier), else downloaded with FetchPackage
// into pkgDir. With a nil ctxFetch nothing is written to disk.
func LoadPackage(ctxFetch func(dest string) error, snapshot fs.FS, pkgDir string, pkg PackageRef) ([]byte, error) {
	ok := func(b []byte) bool {
		sum := sha256.Sum256(b)
		return int64(len(b)) == pkg.Size && hex.EncodeToString(sum[:]) == pkg.SHA256
	}
	if !packagePathRe.MatchString(pkg.Path) || !sha256Re.MatchString(pkg.SHA256) {
		return nil, newErr(CodePkgPath, nil, "应用包引用不对："+pkg.Path)
	}
	if snapshot != nil {
		if b, err := fs.ReadFile(snapshot, pkg.Path); err == nil && ok(b) {
			return b, nil
		}
	}
	if pkgDir == "" {
		return nil, newErr(CodePkgWrite, nil, "没有设置应用包目录")
	}
	dest := filepath.Join(pkgDir, filepath.FromSlash(pkg.Path))
	if b, err := os.ReadFile(dest); err == nil && ok(b) {
		return b, nil
	}
	// Without a fetcher the call is read-only (the catalog list's icons):
	// it must not create directories, e.g. under a private catalog whose
	// cache was just removed.
	if ctxFetch == nil {
		return nil, newErr(CodePkgAllFailed, nil, "本机没有这个应用包："+pkg.Path)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, newErr(CodePkgWrite, err, "建不了应用包目录")
	}
	if err := ctxFetch(dest); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(dest)
	if err != nil || !ok(b) {
		return nil, newErr(CodePkgMismatch, err, "应用包校验不过："+pkg.Path)
	}
	return b, nil
}
