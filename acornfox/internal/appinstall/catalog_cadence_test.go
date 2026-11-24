package appinstall

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

// fakeClock is a clock tests move by hand; the background syncs read it too.
type fakeClock struct{ ns atomic.Int64 }

func newFakeClock() *fakeClock {
	c := &fakeClock{}
	c.ns.Store(time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC).UnixNano())
	return c
}
func (c *fakeClock) Now() time.Time          { return time.Unix(0, c.ns.Load()).UTC() }
func (c *fakeClock) Advance(d time.Duration) { c.ns.Add(int64(d)) }

func (c *Catalog) touchingNow() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.touching
}

func cadenceCatalog(t *testing.T, cacheDir string, clk *fakeClock, source string) (*Catalog, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return cadenceCatalogWith(t, cacheDir, clk, source, pub, priv), priv
}

func cadenceCatalogWith(t *testing.T, cacheDir string, clk *fakeClock, source string, pub ed25519.PublicKey, _ ed25519.PrivateKey) *Catalog {
	t.Helper()
	c := NewCatalog(CatalogConfig{CacheDir: cacheDir, Snapshot: fstest.MapFS{}, Keys: []ed25519.PublicKey{pub},
		Sources: []string{source}, Now: clk.Now})
	c.serving = true // as when the server's sync loop runs
	return c
}

// Without the server's sync loop a page starts no background sync.
func TestTouchStaleOnlyWhileServing(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)
	c := NewCatalog(CatalogConfig{CacheDir: t.TempDir(), Snapshot: fstest.MapFS{}, Keys: []ed25519.PublicKey{pub},
		Sources: []string{"http://127.0.0.1:9/catalog"}, Now: newFakeClock().Now})
	if c.TouchStale() {
		t.Fatal("定时同步没在跑时，打开页面不该在后台同步")
	}
}

func TestCadenceDefaultsAndEnvironment(t *testing.T) {
	c := NewCatalog(CatalogConfig{CacheDir: t.TempDir(), Snapshot: fstest.MapFS{}})
	if c.Interval() != 6*time.Hour || c.StaleAfter() != 30*time.Minute || c.retryGap != 5*time.Minute {
		t.Fatalf("默认值不对：%v %v %v", c.Interval(), c.StaleAfter(), c.retryGap)
	}
	t.Setenv(SyncIntervalEnv, "2h")
	t.Setenv(StaleAfterEnv, "90s")
	t.Setenv(RetryGapEnv, "10s")
	c = NewCatalog(CatalogConfig{CacheDir: t.TempDir(), Snapshot: fstest.MapFS{}})
	if c.Interval() != 2*time.Hour || c.StaleAfter() != 90*time.Second || c.retryGap != 10*time.Second {
		t.Fatalf("环境变量没生效：%v %v %v", c.Interval(), c.StaleAfter(), c.retryGap)
	}
	// Nonsense and out-of-range values fall back to the defaults.
	t.Setenv(SyncIntervalEnv, "soon")
	t.Setenv(StaleAfterEnv, "-5m")
	t.Setenv(RetryGapEnv, "9999h")
	c = NewCatalog(CatalogConfig{CacheDir: t.TempDir(), Snapshot: fstest.MapFS{}})
	if c.Interval() != 6*time.Hour || c.StaleAfter() != 30*time.Minute || c.retryGap != 5*time.Minute {
		t.Fatalf("坏值要用默认：%v %v %v", c.Interval(), c.StaleAfter(), c.retryGap)
	}
	// The config wins over the environment.
	c = NewCatalog(CatalogConfig{CacheDir: t.TempDir(), Snapshot: fstest.MapFS{}, SyncInterval: time.Hour})
	if c.Interval() != time.Hour {
		t.Fatalf("配置里的值要优先：%v", c.Interval())
	}
}

func TestNextSyncWaitJitter(t *testing.T) {
	lo, hi := 6*time.Hour-36*time.Minute, 6*time.Hour+36*time.Minute
	seen := map[time.Duration]bool{}
	for i := 0; i < 500; i++ {
		d := nextSyncWait(6 * time.Hour)
		if d < lo || d >= hi {
			t.Fatalf("等待 %v 不在 [%v, %v)", d, lo, hi)
		}
		seen[d] = true
	}
	if len(seen) < 100 {
		t.Fatalf("抖动不随机：500 次只有 %d 个不同的值", len(seen))
	}
	if d := nextSyncWait(time.Nanosecond); d != time.Nanosecond {
		t.Fatalf("太短的间隔不抖动：%v", d)
	}
}

func TestSyncIfStaleAndRetryGap(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)
	srv := showSource(t, 7, priv, 0, nil)
	clk := newFakeClock()
	ctx := context.Background()
	c := cadenceCatalogWith(t, t.TempDir(), clk, srv.URL+"/catalog", pub, priv)

	// Never synced: the first look syncs.
	st, ran := c.SyncIfStale(ctx)
	if !ran || !st.OK {
		t.Fatalf("第一次要同步：ran=%v %+v", ran, st)
	}
	// 10 minutes later: still fresh.
	clk.Advance(10 * time.Minute)
	if _, ran := c.SyncIfStale(ctx); ran {
		t.Fatal("10 分钟前才成功同步过，不该再同步")
	}
	if c.Stale() {
		t.Fatal("Stale 应该是 false")
	}
	// 31 minutes after the sync: stale.
	clk.Advance(21 * time.Minute)
	if !c.Stale() {
		t.Fatal("超过 30 分钟，应该算旧了")
	}
	if _, ran := c.SyncIfStale(ctx); !ran {
		t.Fatal("超过 30 分钟，要同步")
	}
}

func TestSyncIfStaleBacksOffAfterFailure(t *testing.T) {
	clk := newFakeClock()
	ctx := context.Background()
	c, _ := cadenceCatalog(t, t.TempDir(), clk, "http://127.0.0.1:1")

	st, ran := c.SyncIfStale(ctx)
	if !ran || st.OK {
		t.Fatalf("连不上的来源：要试一次且不成功：ran=%v %+v", ran, st)
	}
	// Failed: the next look within 5 minutes does not try again, however
	// stale the catalog is.
	clk.Advance(2 * time.Minute)
	if _, ran := c.SyncIfStale(ctx); ran {
		t.Fatal("失败后 5 分钟内不该再试")
	}
	if c.TouchStale() {
		t.Fatal("失败后 5 分钟内页面也不该再触发")
	}
	clk.Advance(4 * time.Minute)
	if _, ran := c.SyncIfStale(ctx); !ran {
		t.Fatal("过了 5 分钟要再试")
	}
}

func TestTouchStaleRunsInBackgroundOnce(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)
	srv := showSource(t, 7, priv, 0, nil)
	clk := newFakeClock()
	c := cadenceCatalogWith(t, t.TempDir(), clk, srv.URL+"/catalog", pub, priv)

	if !c.TouchStale() {
		t.Fatal("没同步过，打开页面要触发一次")
	}
	if c.TouchStale() {
		t.Fatal("上一次还没做完，不该再起一个")
	}
	deadline := time.Now().Add(10 * time.Second)
	for c.Status().At == nil {
		if time.Now().After(deadline) {
			t.Fatal("后台同步没有结果")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := c.Status(); !st.OK || !st.Updated {
		t.Fatalf("后台同步的结果：%+v", st)
	}
	for c.touchingNow() {
		if time.Now().After(deadline) {
			t.Fatal("后台同步没有收尾")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c.TouchStale() {
		t.Fatal("刚成功同步过，不该再触发")
	}
	clk.Advance(31 * time.Minute)
	if !c.TouchStale() {
		t.Fatal("过了 30 分钟，打开页面要再触发")
	}
	// Let it finish before the temporary directory goes.
	for c.touchingNow() {
		if time.Now().After(deadline) {
			t.Fatal("第二次后台同步没有收尾")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStaleRemembersAcrossRestartAndNeedsKeys(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	pub := priv.Public().(ed25519.PublicKey)
	srv := showSource(t, 7, priv, 0, nil)
	clk := newFakeClock()
	dir := t.TempDir()
	c := cadenceCatalogWith(t, dir, clk, srv.URL+"/catalog", pub, priv)
	if st := c.Sync(context.Background()); !st.OK {
		t.Fatalf("sync %+v", st)
	}
	// The server restarts 10 minutes later: what was synced still counts.
	clk.Advance(10 * time.Minute)
	c2 := cadenceCatalogWith(t, dir, clk, srv.URL+"/catalog", pub, priv)
	if c2.Stale() {
		t.Fatal("重启后 10 分钟，不该算旧")
	}
	clk.Advance(30 * time.Minute)
	if !c2.Stale() {
		t.Fatal("重启后记得上次同步的时间，过了 30 分钟要算旧")
	}
	// No trusted key: nothing would be contacted, so nothing is started.
	off := NewCatalog(CatalogConfig{CacheDir: t.TempDir(), Snapshot: fstest.MapFS{}, Keys: []ed25519.PublicKey{}, Now: clk.Now})
	if off.Stale() || off.TouchStale() {
		t.Fatal("没有受信公钥不该触发同步")
	}
}
