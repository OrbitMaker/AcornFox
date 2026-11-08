package catalog

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Client side of the published catalog.
//
// The cache layout and how it is switched atomically are described in
// cache.go. Only a verified index is ever written there, and Load
// re-verifies it, so an edited cache is ignored in favour of the snapshot.

// DefaultSource is the official catalog location.
const DefaultSource = "https://acornfox.com/catalog"

// GiteeMirror is the backup copy of the public catalog: the same signed files
// on the "published" branch of the public catalog repository on Gitee. Every
// file is verified against TrustedKeys, so a mirror can be stale but cannot
// change what is installed.
const GiteeMirror = "https://gitee.com/VIP13390/acornfox-catalog/raw/published"

// SourcesEnv overrides the public catalog sources: comma-separated base URLs
// tried in order (for testing or a self-hosted copy).
const SourcesEnv = "ACORNFOX_CATALOG_SOURCES"

// PublicSources returns the public catalog sources: SourcesEnv when set to at
// least one http(s) URL, else the official site followed by the Gitee mirror.
func PublicSources(env string) []string {
	var out []string
	for _, f := range strings.Split(env, ",") {
		f = strings.TrimRight(strings.TrimSpace(f), "/")
		if strings.HasPrefix(f, "https://") || strings.HasPrefix(f, "http://") {
			out = append(out, f)
		}
	}
	if len(out) > 0 {
		return out
	}
	return DefaultSources(GiteeMirror)
}

// DefaultSources returns the sources Sync tries in order: the official site,
// then the Gitee mirror when one is configured (empty: none).
func DefaultSources(giteeMirror string) []string {
	s := []string{DefaultSource}
	if m := strings.TrimSpace(giteeMirror); m != "" {
		s = append(s, m)
	}
	return s
}

// DefaultTimeout bounds each HTTP request of Sync and FetchPackage.
const DefaultTimeout = 30 * time.Second

// Size limits for what a source may send.
const (
	maxVersionBytes = 64
	maxSigBytes     = 1 << 10
	maxIndexBytes   = 32 << 20
)

// SyncConfig configures Sync and FetchPackage.
type SyncConfig struct {
	// Sources are catalog base URLs tried in order (empty: DefaultSources("")).
	Sources []string
	// CacheDir holds the verified copy. Sync creates it.
	CacheDir string
	// TrustedKeys verify index.json (nil: the package-level TrustedKeys).
	TrustedKeys []ed25519.PublicKey
	// Timeout bounds each request (0: DefaultTimeout).
	Timeout time.Duration
	// HTTP is the client to use (nil: a plain http.Client).
	HTTP *http.Client
	// Snapshot, when set, also counts as "local": a source must be newer than
	// both the cache and the built-in snapshot.
	Snapshot fs.FS
}

func (c SyncConfig) sources() []string {
	if len(c.Sources) > 0 {
		return c.Sources
	}
	return DefaultSources("")
}

func (c SyncConfig) keys() []ed25519.PublicKey {
	if c.TrustedKeys != nil {
		return c.TrustedKeys
	}
	return TrustedKeys
}

func (c SyncConfig) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c SyncConfig) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// SyncResult reports what Sync did.
type SyncResult struct {
	Updated     bool      `json:"updated"`      // a newer index was written to the cache
	Serial      int64     `json:"serial"`       // serial in use after Sync
	LocalSerial int64     `json:"local_serial"` // serial before Sync (0: none)
	Source      string    `json:"source"`       // source that answered ("" when all failed)
	Attempts    []Attempt `json:"attempts"`     // sources that failed before the answer
}

// Sync fetches version.txt from each source in turn. If it is not newer than
// the local copy, Sync stops without downloading anything else. Otherwise it
// downloads index.json and index.json.sig, verifies the signature, requires
// the signed serial to equal version.txt and to be strictly newer than the
// local copy, and then switches the cache to it atomically. Any failure moves
// on to the next source; when all fail it returns CAT-SYNC-08 with the
// attempts. Nothing unverified is ever written. Sync holds the cache lock for
// its whole run (CAT-SYNC-09 when another sync keeps it too long), so a
// scheduled and a manual sync never interleave.
func Sync(ctx context.Context, cfg SyncConfig) (SyncResult, error) {
	keys := cfg.keys()
	if len(keys) == 0 {
		return SyncResult{}, newErr(CodeSyncNoKeys, nil, "程序里没有受信公钥，不同步目录")
	}
	unlock, err := lockCache(ctx, cfg.CacheDir)
	if err != nil {
		return SyncResult{Attempts: []Attempt{}}, err
	}
	defer unlock()
	local := localSerial(cfg.CacheDir, cfg.Snapshot, keys)
	res := SyncResult{LocalSerial: local, Serial: local, Attempts: []Attempt{}}
	for _, src := range cfg.sources() {
		if err := ctx.Err(); err != nil {
			res.Attempts = append(res.Attempts, Attempt{Source: src, Code: CodeSyncNetwork, Message: err.Error()})
			break
		}
		updated, serial, err := syncOne(ctx, cfg, src, local, keys)
		if err != nil {
			e := asError(err, CodeSyncNetwork)
			if e.Code == CodeSyncCache {
				return res, e // local disk problem: another source will not help
			}
			res.Attempts = append(res.Attempts, Attempt{Source: src, Code: e.Code, Message: e.Message})
			continue
		}
		res.Source = src
		res.Updated = updated
		if updated {
			res.Serial = serial
		}
		return res, nil
	}
	return res, &Error{Code: CodeSyncAllFailed, Message: "所有来源都失败，继续用手上的目录", Attempts: res.Attempts}
}

func syncOne(ctx context.Context, cfg SyncConfig, src string, local int64, keys []ed25519.PublicKey) (bool, int64, error) {
	verB, err := get(ctx, cfg, src, VersionFile, maxVersionBytes)
	if err != nil {
		return false, 0, err
	}
	remote, err := parseSerial(verB)
	if err != nil {
		return false, 0, newErr(CodeSyncResponse, err, "version.txt 不是正整数")
	}
	if remote <= local {
		return false, local, nil
	}
	idxB, err := get(ctx, cfg, src, IndexFile, maxIndexBytes)
	if err != nil {
		return false, 0, err
	}
	sig, err := get(ctx, cfg, src, SigFile, maxSigBytes)
	if err != nil {
		return false, 0, err
	}
	if err := Verify(idxB, sig, keys); err != nil {
		return false, 0, newErr(CodeSyncSignature, err, "index.json 的签名验不过，丢弃")
	}
	idx, err := ParseIndex(idxB)
	if err != nil {
		return false, 0, newErr(CodeSyncResponse, err, "index.json 格式不对")
	}
	if err := CheckCatalog(idx, PublicCatalog); err != nil {
		return false, 0, err
	}
	if idx.Serial <= local {
		return false, 0, newErr(CodeSyncRollback, nil, fmt.Sprintf("签名的目录序号 %d 不比本地的 %d 新", idx.Serial, local))
	}
	if idx.Serial != remote {
		return false, 0, newErr(CodeSyncMismatch, nil, fmt.Sprintf("version.txt 是 %d，签名的 index.json 是 %d", remote, idx.Serial))
	}
	switched, err := commitCache(cfg.CacheDir, idx.Serial, idxB, sig, verB, keys)
	if err != nil {
		return false, 0, newErr(CodeSyncCache, err, "写本地缓存失败，手上的目录没动")
	}
	if !switched {
		return false, local, nil
	}
	return true, idx.Serial, nil
}

// get downloads base/name, at most limit bytes.
func get(ctx context.Context, cfg SyncConfig, base, name string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout())
	defer cancel()
	u := strings.TrimRight(base, "/") + "/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, newErr(CodeSyncNetwork, err, "来源地址不对："+u)
	}
	resp, err := cfg.client().Do(req)
	if err != nil {
		return nil, newErr(CodeSyncNetwork, err, "取 "+name+" 失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, newErr(CodeSyncNetwork, nil, fmt.Sprintf("取 %s 返回 HTTP %d", name, resp.StatusCode))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, newErr(CodeSyncNetwork, err, "读 "+name+" 中断")
	}
	if int64(len(b)) > limit {
		return nil, newErr(CodeSyncResponse, nil, fmt.Sprintf("%s 超过 %d 字节", name, limit))
	}
	return b, nil
}

func asError(err error, fallback string) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return newErr(fallback, err, err.Error())
}

var (
	packagePathRe = regexp.MustCompile(`^apps/[a-z][a-z0-9-]{1,39}/[A-Za-z0-9][A-Za-z0-9._+-]{0,63}\.tar\.gz$`)
	sha256Re      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ParseIndex decodes index.json and checks the parts a client relies on:
// schema 1, a positive serial and well-formed package references.
func ParseIndex(b []byte) (*Index, error) {
	var idx Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return nil, err
	}
	// A signed document of another kind (the operations overlay ops.json
	// carries "kind") is never an index, whatever else it holds.
	var head struct {
		Kind *string `json:"kind"`
	}
	if json.Unmarshal(b, &head) == nil && head.Kind != nil {
		return nil, fmt.Errorf("这不是目录索引（kind 是 %q）", *head.Kind)
	}
	if idx.Schema != IndexSchema {
		return nil, fmt.Errorf("index.json 的 schema 是 %d，这个程序只认 %d", idx.Schema, IndexSchema)
	}
	if idx.Serial < 1 {
		return nil, fmt.Errorf("index.json 的 serial 是 %d", idx.Serial)
	}
	if idx.Catalog != "" && idx.Catalog != PublicCatalog && !ValidCatalogID(idx.Catalog) {
		return nil, fmt.Errorf("index.json 的 catalog %q 不对", idx.Catalog)
	}
	for _, a := range idx.Apps {
		if !packagePathRe.MatchString(a.Package.Path) || !sha256Re.MatchString(a.Package.SHA256) || a.Package.Size <= 0 {
			return nil, fmt.Errorf("应用 %s 的 package 不对", a.ID)
		}
	}
	return &idx, nil
}

// CheckCatalog is CAT-SYNC-12 when the signed index names a catalog other
// than want ("public" or a private catalog id). An index that names none
// (built before the field existed) passes.
func CheckCatalog(idx *Index, want string) error {
	if idx.Catalog == "" || idx.Catalog == want {
		return nil
	}
	return newErr(CodeSyncWrongCatalog, nil, fmt.Sprintf("签名的索引属于目录 %s，不是 %s，不用", idx.Catalog, want))
}

// loadSnapshot reads the built-in snapshot. It is part of the program, so it
// is trusted without a signature.
func loadSnapshot(snapshot fs.FS) *Index {
	if snapshot == nil {
		return nil
	}
	b, err := fs.ReadFile(snapshot, IndexFile)
	if err != nil {
		return nil
	}
	idx, err := ParseIndex(b)
	if err != nil {
		return nil
	}
	return idx
}

func localSerial(cacheDir string, snapshot fs.FS, keys []ed25519.PublicKey) int64 {
	var n int64
	if c := loadCache(cacheDir, keys); c != nil {
		n = c.Serial
	}
	if s := loadSnapshot(snapshot); s != nil && s.Serial > n {
		n = s.Serial
	}
	return n
}

// Origins reported by Load.
const (
	FromCache    = "cache"
	FromSnapshot = "snapshot"
)

// Load returns the catalog to show: the verified cache (checked with the
// package-level TrustedKeys) unless the built-in snapshot has a higher serial
// (the program was upgraded since the last sync), then the snapshot.
func Load(cacheDir string, snapshot fs.FS) (*Index, string, error) {
	return LoadWith(cacheDir, snapshot, TrustedKeys)
}

// LoadWith is Load with explicit trusted keys.
func LoadWith(cacheDir string, snapshot fs.FS, keys []ed25519.PublicKey) (*Index, string, error) {
	c := loadCache(cacheDir, keys)
	s := loadSnapshot(snapshot)
	switch {
	case c != nil && (s == nil || c.Serial >= s.Serial):
		return c, FromCache, nil
	case s != nil:
		return s, FromSnapshot, nil
	}
	return nil, "", newErr(CodeLoad, nil, "本地缓存和内置快照都用不了")
}

// FetchPackage downloads pkg from the sources in order and saves it to dest
// only when its size and sha256 match pkg. A mismatch (CAT-PKG-02) or a
// network error moves on to the next source; when all fail it returns
// CAT-PKG-01 with the attempts.
func FetchPackage(ctx context.Context, cfg SyncConfig, pkg PackageRef, dest string) error {
	if !packagePathRe.MatchString(pkg.Path) || !sha256Re.MatchString(pkg.SHA256) || pkg.Size <= 0 {
		return newErr(CodePkgPath, nil, "应用包引用不对："+pkg.Path)
	}
	var attempts []Attempt
	for _, src := range cfg.sources() {
		if err := ctx.Err(); err != nil {
			attempts = append(attempts, Attempt{Source: src, Code: CodeSyncNetwork, Message: err.Error()})
			break
		}
		b, err := get(ctx, cfg, src, pkg.Path, pkg.Size)
		if err != nil {
			e := asError(err, CodeSyncNetwork)
			if e.Code == CodeSyncResponse { // longer than pkg.Size
				e = newErr(CodePkgMismatch, nil, "应用包比目录里写的大")
			}
			attempts = append(attempts, Attempt{Source: src, Code: e.Code, Message: e.Message})
			continue
		}
		sum := sha256.Sum256(b)
		if int64(len(b)) != pkg.Size || !bytes.Equal([]byte(hex.EncodeToString(sum[:])), []byte(pkg.SHA256)) {
			attempts = append(attempts, Attempt{Source: src, Code: CodePkgMismatch, Message: pkg.Path + " 的 sha256 或大小不对，已丢弃"})
			continue
		}
		if err := writeAtomic(dest, b, 0o644); err != nil {
			return newErr(CodePkgWrite, err, "存应用包失败")
		}
		return nil
	}
	return &Error{Code: CodePkgAllFailed, Message: "没有来源给出和目录一致的应用包：" + pkg.Path, Attempts: attempts}
}
