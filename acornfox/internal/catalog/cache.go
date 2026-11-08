package catalog

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Local cache of the published catalog (SyncConfig.CacheDir):
//
//	current               one line "v<serial>": the version in use
//	v<serial>/            version.txt, index.json, index.json.sig as published
//	.lock                 held by Sync for its whole run
//
// A new version is written into a fresh temporary directory, renamed to
// v<serial>, and only then does current switch to it (one atomic rename). A
// failure at any step leaves current pointing at the old, complete version.
// Sync holds .lock, so two syncs never interleave; Load takes no lock and
// re-verifies what current points at, falling back to the built-in snapshot.
// Sync keeps the version in use and the one before it (a Load that read
// current just before a switch still finds its files) and removes the rest.
const (
	cacheCurrent = "current"
	cacheLock    = ".lock"
	cacheTmpPfx  = ".tmp-"
)

var cacheVersionRe = regexp.MustCompile(`^v[1-9][0-9]{0,18}$`)

// syncLockWait bounds how long Sync waits for another sync's lock.
var syncLockWait = 2 * time.Minute

// writeCacheFile writes one file of a new cache version. Tests replace it to
// simulate a full disk.
var writeCacheFile = func(path string, data []byte) error {
	return writeAtomic(path, data, 0o644)
}

// lockCache creates dir and takes its .lock, waiting up to syncLockWait (or
// until ctx ends). CAT-SYNC-07 when the lock file cannot be made,
// CAT-SYNC-09 when another holder keeps it.
func lockCache(ctx context.Context, dir string) (func(), error) {
	if dir == "" {
		return nil, newErr(CodeSyncCache, nil, "没有设置缓存目录")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, newErr(CodeSyncCache, err, "建不了缓存目录")
	}
	f, err := os.OpenFile(filepath.Join(dir, cacheLock), os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, newErr(CodeSyncCache, err, "建不了缓存锁文件")
	}
	deadline := time.Now().Add(syncLockWait)
	for {
		busy, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, newErr(CodeSyncCache, err, "锁缓存目录失败")
		}
		if !busy {
			return func() { unlockFile(f); f.Close() }, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, newErr(CodeSyncBusy, nil, "另一个同步正在写缓存目录，等了 "+syncLockWait.String()+" 还没结束")
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, newErr(CodeSyncBusy, ctx.Err(), "等另一个同步结束时被取消")
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// currentVersionDir returns the directory current points at ("" when none or
// malformed).
func currentVersionDir(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, cacheCurrent))
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(b))
	if !cacheVersionRe.MatchString(name) {
		return ""
	}
	return filepath.Join(dir, name)
}

// commitCache writes a verified index as version v<serial> and switches
// current to it. The caller holds the cache lock. If what current points at
// is already as new (it cannot change under the lock, but check anyway),
// nothing is switched.
func commitCache(dir string, serial int64, idx, sig, ver []byte, keys []ed25519.PublicKey) (bool, error) {
	if c := loadCache(dir, keys); c != nil && c.Serial >= serial {
		return false, nil
	}
	tmp := filepath.Join(dir, cacheTmpPfx+randHex())
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return false, err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{{SigFile, sig}, {IndexFile, idx}, {VersionFile, ver}} {
		if err := writeCacheFile(filepath.Join(tmp, f.name), f.data); err != nil {
			_ = os.RemoveAll(tmp)
			return false, err
		}
	}
	name := "v" + strconv.FormatInt(serial, 10)
	final := filepath.Join(dir, name)
	// A leftover from an earlier failed run is never the version in use
	// (current only points at versions older than serial here).
	if err := os.RemoveAll(final); err != nil {
		_ = os.RemoveAll(tmp)
		return false, err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.RemoveAll(tmp)
		return false, err
	}
	prev := filepath.Base(currentVersionDir(dir))
	if err := writeCacheFile(filepath.Join(dir, cacheCurrent), []byte(name+"\n")); err != nil {
		return false, err
	}
	pruneCache(dir, name, prev)
	return true, nil
}

// pruneCache removes cache versions and temporary directories other than
// keep (best effort).
func pruneCache(dir string, keep ...string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || !(cacheVersionRe.MatchString(n) || strings.HasPrefix(n, cacheTmpPfx)) {
			continue
		}
		kept := false
		for _, k := range keep {
			if n == k {
				kept = true
			}
		}
		if !kept {
			_ = os.RemoveAll(filepath.Join(dir, n))
		}
	}
}

func randHex() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// loadCache returns the verified index current points at, or nil.
func loadCache(dir string, keys []ed25519.PublicKey) *Index {
	if dir == "" || len(keys) == 0 {
		return nil
	}
	vdir := currentVersionDir(dir)
	if vdir == "" {
		return nil
	}
	idxB, err := os.ReadFile(filepath.Join(vdir, IndexFile))
	if err != nil {
		return nil
	}
	sig, err := os.ReadFile(filepath.Join(vdir, SigFile))
	if err != nil {
		return nil
	}
	if Verify(idxB, sig, keys) != nil {
		return nil
	}
	idx, err := ParseIndex(idxB)
	if err != nil {
		return nil
	}
	if want := "v" + strconv.FormatInt(idx.Serial, 10); filepath.Base(vdir) != want {
		return nil // current names another serial than the signed one
	}
	return idx
}
