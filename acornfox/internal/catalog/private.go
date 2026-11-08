package catalog

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"regexp"
	"strconv"
)

// Private catalogs.
//
// A private catalog is published by the operations system and handed out by
// the download gateway only to machines it was opened to (W10). Its index has
// the same format and the same signature as the public one and is verified
// with the same trusted keys; the signed "catalog" field (when present) must
// be its own id, so the public index or another catalog's cannot stand in
// for it, nor it for the public one; this file stores a verified copy of one private
// index in its own cache directory (same layout as the public cache, see
// cache.go) so the public and the private catalogs never mix.
//
// The packages of a private catalog are not on any public site: each one is
// a gateway file named PrivatePackageFileID(app, version) and is fetched
// through W2 with the catalog named in the request.

// catalogIDRe is the gateway's private catalog id: "cat-" plus 1-64 letters,
// digits, "_" or "-". Ids are also used as directory names.
var catalogIDRe = regexp.MustCompile(`^cat-[A-Za-z0-9_-]{1,64}$`)

// ValidCatalogID reports whether id is a well-formed private catalog id.
func ValidCatalogID(id string) bool { return catalogIDRe.MatchString(id) }

// PrivatePackageFileID is the gateway file id of a private catalog's app
// package: "catpkg-<app id>-<release.version>" (fixed by the operations
// system, which registers these files when it publishes a private catalog).
func PrivatePackageFileID(app, version string) string { return "catpkg-" + app + "-" + version }

// StoreIndex verifies a private index (the exact bytes and the base64url
// signature the gateway passed on) with keys and makes it the version in use
// in cacheDir. wantSerial is the serial the gateway announced; the signed one
// must equal it (CAT-SYNC-05). An index that names another catalog than
// catalogID is refused (CAT-SYNC-12). A signed serial lower than the cached one is a
// rollback (CAT-SYNC-06) and is refused; an equal one changes nothing. It
// takes the cache lock for its whole run, like Sync.
func StoreIndex(ctx context.Context, cacheDir, catalogID string, index, sig []byte, wantSerial int64, keys []ed25519.PublicKey) (bool, int64, error) {
	if len(keys) == 0 {
		return false, 0, newErr(CodeSyncNoKeys, nil, "程序里没有受信公钥，不接受私有目录")
	}
	if err := Verify(index, sig, keys); err != nil {
		return false, 0, newErr(CodeSyncSignature, err, "私有目录索引的签名验不过，丢弃")
	}
	idx, err := ParseIndex(index)
	if err != nil {
		return false, 0, newErr(CodeSyncResponse, err, "私有目录索引格式不对")
	}
	if err := CheckCatalog(idx, catalogID); err != nil {
		return false, 0, err
	}
	if wantSerial > 0 && idx.Serial != wantSerial {
		return false, 0, newErr(CodeSyncMismatch, nil, fmt.Sprintf("网关说序号是 %d，签名的索引是 %d", wantSerial, idx.Serial))
	}
	unlock, err := lockCache(ctx, cacheDir)
	if err != nil {
		return false, 0, err
	}
	defer unlock()
	if c := loadCache(cacheDir, keys); c != nil {
		if idx.Serial < c.Serial {
			return false, c.Serial, newErr(CodeSyncRollback, nil, fmt.Sprintf("签名的序号 %d 比本地的 %d 旧", idx.Serial, c.Serial))
		}
		if idx.Serial == c.Serial {
			return false, c.Serial, nil
		}
	}
	ver := []byte(strconv.FormatInt(idx.Serial, 10) + "\n")
	switched, err := commitCache(cacheDir, idx.Serial, index, sig, ver, keys)
	if err != nil {
		return false, 0, newErr(CodeSyncCache, err, "写私有目录缓存失败，手上的目录没动")
	}
	return switched, idx.Serial, nil
}

// LoadIndex returns the verified index cached in cacheDir, or nil when there
// is none (or it no longer verifies with keys, or names another catalog than
// catalogID).
func LoadIndex(cacheDir, catalogID string, keys []ed25519.PublicKey) *Index {
	idx := loadCache(cacheDir, keys)
	if idx == nil || CheckCatalog(idx, catalogID) != nil {
		return nil
	}
	return idx
}
