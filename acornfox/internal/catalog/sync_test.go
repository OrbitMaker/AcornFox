package catalog

import (
	"context"
	"crypto/ed25519"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// source is a fake catalog server backed by a published directory. Paths in
// override answer with fixed bytes; status forces every answer's code.
type source struct {
	mu       sync.Mutex
	dir      string
	override map[string][]byte
	status   int
	hits     map[string]int
	srv      *httptest.Server
}

func newSource(t *testing.T, dir string) *source {
	s := &source{dir: dir, override: map[string][]byte{}, hits: map[string]int{}}
	fsrv := http.FileServer(http.Dir(dir))
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/catalog/")
		s.mu.Lock()
		s.hits[p]++
		status, body, ok := s.status, s.override[p], false
		if body != nil {
			ok = true
		}
		s.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if ok {
			_, _ = w.Write(body)
			return
		}
		r.URL.Path = "/" + p
		fsrv.ServeHTTP(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *source) url() string { return s.srv.URL + "/catalog" }

func (s *source) count(p string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[p]
}

func cfgFor(cache string, pub ed25519.PublicKey, srcs ...*source) SyncConfig {
	c := SyncConfig{CacheDir: cache, TrustedKeys: []ed25519.PublicKey{pub}, Timeout: 5 * time.Second}
	for _, s := range srcs {
		c.Sources = append(c.Sources, s.url())
	}
	return c
}

func TestSyncFreshThenUnchanged(t *testing.T) {
	pub, priv := testKey(t)
	src := newSource(t, publish(t, copyTree(t), 5, priv))
	cache := filepath.Join(t.TempDir(), "cache")
	cfg := cfgFor(cache, pub, src)

	res, err := Sync(context.Background(), cfg)
	if err != nil || !res.Updated || res.Serial != 5 || res.LocalSerial != 0 || res.Source != src.url() {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	idx, from, err := LoadWith(cache, nil, cfg.TrustedKeys)
	if err != nil || from != FromCache || idx.Serial != 5 {
		t.Fatalf("load: %v %s %v", idx, from, err)
	}

	// Same version: only version.txt is fetched again.
	res, err = Sync(context.Background(), cfg)
	if err != nil || res.Updated || res.Serial != 5 {
		t.Fatalf("second: res=%+v err=%v", res, err)
	}
	if src.count(VersionFile) != 2 || src.count(IndexFile) != 1 || src.count(SigFile) != 1 {
		t.Fatalf("版本没变不该再下载：%v", src.hits)
	}
}

func TestSyncFallsBackToSecondSource(t *testing.T) {
	pub, priv := testKey(t)
	out := publish(t, copyTree(t), 2, priv)
	bad := newSource(t, out)
	bad.status = http.StatusInternalServerError
	good := newSource(t, out)
	cache := t.TempDir()
	res, err := Sync(context.Background(), cfgFor(cache, pub, bad, good))
	if err != nil || !res.Updated || res.Source != good.url() {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(res.Attempts) != 1 || res.Attempts[0].Code != CodeSyncNetwork {
		t.Fatalf("attempts = %+v", res.Attempts)
	}

	// Unreachable source (closed server) also falls through.
	dead := newSource(t, out)
	dead.srv.Close()
	res, err = Sync(context.Background(), cfgFor(t.TempDir(), pub, dead, good))
	if err != nil || res.Source != good.url() || res.Attempts[0].Code != CodeSyncNetwork {
		t.Fatalf("dead: res=%+v err=%v", res, err)
	}
}

func TestSyncBadSignatureNotWritten(t *testing.T) {
	pub, priv := testKey(t)
	_, otherPriv := testKey(t)
	src := newSource(t, publish(t, copyTree(t), 3, otherPriv))
	_ = priv
	cache := filepath.Join(t.TempDir(), "cache")
	res, err := Sync(context.Background(), cfgFor(cache, pub, src))
	if CodeOf(err) != CodeSyncAllFailed || len(res.Attempts) != 1 || res.Attempts[0].Code != CodeSyncSignature {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if _, err := os.Stat(filepath.Join(cache, cacheCurrent)); !os.IsNotExist(err) {
		t.Fatalf("坏签名的目录落盘了：%v", err)
	}

	// Index tampered after signing: same result.
	good := publish(t, copyTree(t), 3, priv)
	b, _ := os.ReadFile(filepath.Join(good, IndexFile))
	src2 := newSource(t, good)
	src2.override[IndexFile] = []byte(strings.Replace(string(b), `"serial": 3`, `"serial": 4`, 1))
	src2.override[VersionFile] = []byte("4\n")
	_, err = Sync(context.Background(), cfgFor(cache, pub, src2))
	if CodeOf(err) != CodeSyncAllFailed || !strings.Contains(err.Error(), CodeSyncSignature) {
		t.Fatalf("改过的 index：%v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, cacheCurrent)); !os.IsNotExist(err) {
		t.Fatal("改过的目录落盘了")
	}
	if left := versionDirs(t, cache); len(left) != 0 {
		t.Fatalf("缓存里留下了 %v", left)
	}
}

func TestSyncRejectsRollback(t *testing.T) {
	pub, priv := testKey(t)
	src := copyTree(t)
	cache := t.TempDir()
	newer := newSource(t, publish(t, src, 6, priv))
	if _, err := Sync(context.Background(), cfgFor(cache, pub, newer)); err != nil {
		t.Fatal(err)
	}

	// An old but validly signed catalog whose version.txt lies about being newer.
	old := newSource(t, publish(t, src, 5, priv))
	old.override[VersionFile] = []byte("9\n")
	res, err := Sync(context.Background(), cfgFor(cache, pub, old))
	if CodeOf(err) != CodeSyncAllFailed || res.Attempts[0].Code != CodeSyncRollback {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// An old catalog that says so: nothing to do, nothing downloaded.
	honest := newSource(t, publish(t, src, 5, priv))
	res, err = Sync(context.Background(), cfgFor(cache, pub, honest))
	if err != nil || res.Updated || res.Serial != 6 || honest.count(IndexFile) != 0 {
		t.Fatalf("res=%+v err=%v hits=%v", res, err, honest.hits)
	}
	idx, _, _ := LoadWith(cache, nil, []ed25519.PublicKey{pub})
	if idx.Serial != 6 {
		t.Fatalf("缓存被回退成 %d", idx.Serial)
	}

	// version.txt newer than the signed index: mid-publish, rejected.
	mid := newSource(t, publish(t, src, 7, priv))
	mid.override[VersionFile] = []byte("8\n")
	res, err = Sync(context.Background(), cfgFor(cache, pub, mid))
	if CodeOf(err) != CodeSyncAllFailed || res.Attempts[0].Code != CodeSyncMismatch {
		t.Fatalf("mid: res=%+v err=%v", res, err)
	}
}

func TestSyncNoKeysAndGarbage(t *testing.T) {
	_, priv := testKey(t)
	out := publish(t, copyTree(t), 1, priv)
	src := newSource(t, out)
	cfg := cfgFor(t.TempDir(), nil, src)
	cfg.TrustedKeys = []ed25519.PublicKey{}
	if _, err := Sync(context.Background(), cfg); CodeOf(err) != CodeSyncNoKeys {
		t.Fatalf("no keys: %v", err)
	}
	if src.count(VersionFile) != 0 {
		t.Fatal("没有公钥时不该联网")
	}
	pub, _ := testKey(t)
	src.override[VersionFile] = []byte("<html>")
	res, err := Sync(context.Background(), cfgFor(t.TempDir(), pub, src))
	if CodeOf(err) != CodeSyncAllFailed || res.Attempts[0].Code != CodeSyncResponse {
		t.Fatalf("garbage: res=%+v err=%v", res, err)
	}
}

func TestSyncSnapshotCountsAsLocal(t *testing.T) {
	pub, priv := testKey(t)
	src := newSource(t, publish(t, copyTree(t), 2, priv))
	snap := fstest.MapFS{IndexFile: {Data: mustIndex(t, 3)}}
	cfg := cfgFor(t.TempDir(), pub, src)
	cfg.Snapshot = snap
	res, err := Sync(context.Background(), cfg)
	if err != nil || res.Updated || res.LocalSerial != 3 || src.count(IndexFile) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func mustIndex(t *testing.T, serial int64) []byte {
	b, err := encodeIndex(Index{Schema: IndexSchema, Serial: serial, Apps: []IndexApp{}, Recipes: []Recipe{}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLoadPrefersNewer(t *testing.T) {
	pub, priv := testKey(t)
	cache := t.TempDir()
	src := newSource(t, publish(t, copyTree(t), 4, priv))
	if _, err := Sync(context.Background(), cfgFor(cache, pub, src)); err != nil {
		t.Fatal(err)
	}
	keys := []ed25519.PublicKey{pub}
	if _, from, _ := LoadWith(cache, fstest.MapFS{IndexFile: {Data: mustIndex(t, 4)}}, keys); from != FromCache {
		t.Fatalf("序号相同用缓存，得到 %s", from)
	}
	if idx, from, _ := LoadWith(cache, fstest.MapFS{IndexFile: {Data: mustIndex(t, 9)}}, keys); from != FromSnapshot || idx.Serial != 9 {
		t.Fatalf("快照更新时用快照，得到 %s", from)
	}
	// Cache edited on disk: ignored.
	p := filepath.Join(currentVersionDir(cache), IndexFile)
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, append(b, ' '), 0o644)
	if _, from, _ := LoadWith(cache, fstest.MapFS{IndexFile: {Data: mustIndex(t, 1)}}, keys); from != FromSnapshot {
		t.Fatalf("被改过的缓存不该用，得到 %s", from)
	}
	if _, _, err := LoadWith(t.TempDir(), nil, keys); CodeOf(err) != CodeLoad {
		t.Fatalf("都没有：%v", err)
	}
}

// versionDirs lists the v<serial> and temporary directories in a cache.
func versionDirs(t *testing.T, cache string) []string {
	t.Helper()
	entries, err := os.ReadDir(cache)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// A write failure while storing a newer index (e.g. a full disk) must leave
// the cache that was there fully usable.
func TestSyncCacheWriteFailureKeepsOld(t *testing.T) {
	pub, priv := testKey(t)
	dir := copyTree(t)
	cache := t.TempDir()
	keys := []ed25519.PublicKey{pub}
	if _, err := Sync(context.Background(), cfgFor(cache, pub, newSource(t, publish(t, dir, 4, priv)))); err != nil {
		t.Fatal(err)
	}
	newer := newSource(t, publish(t, dir, 5, priv))
	for _, failOn := range []string{IndexFile, VersionFile, cacheCurrent} {
		orig := writeCacheFile
		writeCacheFile = func(path string, data []byte) error {
			if filepath.Base(path) == failOn {
				return os.ErrPermission
			}
			return orig(path, data)
		}
		_, err := Sync(context.Background(), cfgFor(cache, pub, newer))
		writeCacheFile = orig
		if CodeOf(err) != CodeSyncCache {
			t.Fatalf("写 %s 失败：%v", failOn, err)
		}
		idx, from, err := LoadWith(cache, nil, keys)
		if err != nil || from != FromCache || idx.Serial != 4 {
			t.Fatalf("写 %s 失败后原缓存要还能用：%v %s %v", failOn, idx, from, err)
		}
	}
	res, err := Sync(context.Background(), cfgFor(cache, pub, newer))
	if err != nil || !res.Updated || res.Serial != 5 {
		t.Fatalf("恢复后再同步：%+v %v", res, err)
	}
	if left := versionDirs(t, cache); len(left) != 2 || left[0] != "v4" || left[1] != "v5" {
		t.Fatalf("缓存里应只留 v4、v5，得到 %v", left)
	}
	newest := newSource(t, publish(t, dir, 6, priv))
	if _, err := Sync(context.Background(), cfgFor(cache, pub, newest)); err != nil {
		t.Fatal(err)
	}
	if left := versionDirs(t, cache); len(left) != 2 || left[0] != "v5" || left[1] != "v6" {
		t.Fatalf("缓存里应只留 v5、v6，得到 %v", left)
	}
}

// Two syncs at once (scheduled and manual) do not interleave: one updates,
// the other sees the cache is current.
func TestSyncConcurrent(t *testing.T) {
	pub, priv := testKey(t)
	src := newSource(t, publish(t, copyTree(t), 3, priv))
	cache := t.TempDir()
	var wg sync.WaitGroup
	results := make([]SyncResult, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = Sync(context.Background(), cfgFor(cache, pub, src))
		}(i)
	}
	wg.Wait()
	updated := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("同步 %d：%v", i, errs[i])
		}
		if results[i].Updated {
			updated++
		}
	}
	if updated != 1 || src.count(IndexFile) != 1 {
		t.Fatalf("应只有一次更新、只下一次 index.json：updated=%d hits=%v", updated, src.hits)
	}
	if idx, from, err := LoadWith(cache, nil, []ed25519.PublicKey{pub}); err != nil || from != FromCache || idx.Serial != 3 {
		t.Fatalf("load: %v %s %v", idx, from, err)
	}
}

func TestSyncBusy(t *testing.T) {
	pub, priv := testKey(t)
	src := newSource(t, publish(t, copyTree(t), 2, priv))
	cache := t.TempDir()
	unlock, err := lockCache(context.Background(), cache)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	orig := syncLockWait
	syncLockWait = 200 * time.Millisecond
	defer func() { syncLockWait = orig }()
	if _, err := Sync(context.Background(), cfgFor(cache, pub, src)); CodeOf(err) != CodeSyncBusy {
		t.Fatalf("锁被占着：%v", err)
	}
	if src.count(VersionFile) != 0 {
		t.Fatal("拿不到锁不该联网")
	}
	if _, err := Sync(context.Background(), cfgFor("", pub, src)); CodeOf(err) != CodeSyncCache {
		t.Fatalf("没有缓存目录：%v", err)
	}
}

func TestFetchPackage(t *testing.T) {
	pub, priv := testKey(t)
	out := publish(t, copyTree(t), 1, priv)
	src := newSource(t, out)
	if _, err := Sync(context.Background(), cfgFor(t.TempDir(), pub, src)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, IndexFile))
	idx, err := ParseIndex(b)
	if err != nil {
		t.Fatal(err)
	}
	pkg := idx.Apps[1].Package
	dest := filepath.Join(t.TempDir(), "pkg.tar.gz")

	tampered := newSource(t, out)
	orig, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(pkg.Path)))
	evil := append([]byte{}, orig...)
	evil[len(evil)-1] ^= 1
	tampered.override[pkg.Path] = evil
	err = FetchPackage(context.Background(), cfgFor("", pub, tampered), pkg, dest)
	if CodeOf(err) != CodePkgAllFailed || !strings.Contains(err.Error(), CodePkgMismatch) {
		t.Fatalf("坏包：%v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("校验失败的包落盘了")
	}
	longer := newSource(t, out)
	longer.override[pkg.Path] = append(append([]byte{}, orig...), 0)
	if err := FetchPackage(context.Background(), cfgFor("", pub, longer), pkg, dest); CodeOf(err) != CodePkgAllFailed {
		t.Fatalf("变长的包：%v", err)
	}

	if err := FetchPackage(context.Background(), cfgFor("", pub, tampered, src), pkg, dest); err != nil {
		t.Fatalf("第二个来源是好的：%v", err)
	}
	got, _ := os.ReadFile(dest)
	if sha256Hex(got) != pkg.SHA256 {
		t.Fatal("存下来的包不对")
	}
	bad := pkg
	bad.Path = "apps/../../etc/passwd"
	if err := FetchPackage(context.Background(), cfgFor("", pub, src), bad, dest); CodeOf(err) != CodePkgPath {
		t.Fatalf("坏路径：%v", err)
	}
}

func TestSnapshotEmbedded(t *testing.T) {
	snap := Snapshot()
	idx, from, err := LoadWith("", snap, nil)
	if err != nil || from != FromSnapshot || len(idx.Apps) == 0 {
		t.Fatalf("内置快照读不出：%v %s", err, from)
	}
	ver, err := fs.ReadFile(snap, VersionFile)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := parseSerial(ver); n != idx.Serial {
		t.Fatalf("快照 version.txt=%q，index serial=%d", ver, idx.Serial)
	}
	for _, a := range idx.Apps {
		b, err := fs.ReadFile(snap, a.Package.Path)
		if err != nil {
			t.Fatal(err)
		}
		if sha256Hex(b) != a.Package.SHA256 || int64(len(b)) != a.Package.Size {
			t.Fatalf("快照里 %s 的 sha256 不对", a.Package.Path)
		}
	}
}

func TestDefaultSources(t *testing.T) {
	if s := DefaultSources(""); len(s) != 1 || s[0] != DefaultSource {
		t.Fatalf("%v", s)
	}
	if s := DefaultSources(" https://gitee.example/catalog "); len(s) != 2 || s[1] != "https://gitee.example/catalog" {
		t.Fatalf("%v", s)
	}
}

func TestPublicSources(t *testing.T) {
	if s := PublicSources(""); len(s) != 2 || s[0] != DefaultSource || s[1] != GiteeMirror {
		t.Fatalf("default: %v", s)
	}
	if s := PublicSources(" https://a.example/c/ , ftp://x, http://b.example/c "); len(s) != 2 || s[0] != "https://a.example/c" || s[1] != "http://b.example/c" {
		t.Fatalf("env: %v", s)
	}
	if s := PublicSources("not-a-url"); len(s) != 2 {
		t.Fatalf("bad env falls back to default: %v", s)
	}
}

// The official site is down: Sync takes the same signed files from the
// mirror (the Gitee copy in production).
func TestSyncFallsBackToMirror(t *testing.T) {
	pub, priv := testKey(t)
	dir := publish(t, copyTree(t), 7, priv)
	down := newSource(t, dir)
	down.status = http.StatusBadGateway
	mirror := newSource(t, dir)
	res, err := Sync(context.Background(), cfgFor(filepath.Join(t.TempDir(), "cache"), pub, down, mirror))
	if err != nil || res.Serial != 7 || res.Source != mirror.url() {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
