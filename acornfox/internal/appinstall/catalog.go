package appinstall

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
)

// Catalog is the server's copy of the app catalog: the verified cache under
// <data>/catalog, or the snapshot built into the program. It syncs
//
//   - once when the server starts, then every SyncInterval (6 hours) with a
//     random jitter of a tenth of it either way, so machines do not all ask
//     the official site at the same minute;
//   - in the background when someone opens "my apps" or the app center
//     (GET /v1/apps, GET /v1/catalog) and the last good sync is older than
//     StaleAfter (30 minutes): TouchStale, never holds the page;
//   - when `apps outdated` asks (POST /v1/catalog/sync?if_stale=1: the same
//     test, but the answer waits for the sync);
//   - on POST /v1/catalog/sync (always).
//
// A failed sync is not tried again at once: the periodic one waits for its
// next turn like any other, and the "opened a page" ones wait at least
// RetryGap (5 minutes) after the last attempt, good or not, so a page that
// polls cannot turn a dead network into a storm of requests.
//
// Settings (defaults in the Default* constants; the environment of
// acornfox-server overrides them, a value that does not parse or is out of
// range is ignored): ACORNFOX_CATALOG_SYNC_INTERVAL, ACORNFOX_CATALOG_STALE_AFTER,
// ACORNFOX_CATALOG_RETRY_GAP, all Go durations such as 6h, 30m, 90s.
//
// While the program has no trusted public key (catalog.TrustedKeys empty, the
// state until the release signing key exists) it never contacts a source:
// the status says so and only the built-in snapshot is used.
type Catalog struct {
	cfg CatalogConfig

	mu     sync.Mutex
	status SyncStatus

	pmu sync.Mutex // serializes changes to the private catalogs (private.go)

	// afterSync is called once a sync (daily, at start, or asked for) is
	// over: the engine looks for new versions of the apps installed.
	afterSync func(ctx context.Context)

	icons sync.Map // package sha256 -> listIcon (icons.go)

	// Cadence (see the type comment), set by NewCatalog.
	interval, staleAfter, retryGap time.Duration

	// Guarded by mu: when the last good sync that really contacted a source
	// ended, when the last attempt of any kind ended, whether a sync started
	// by TouchStale or SyncIfStale is running, and the context of Run (the
	// life of the server) the background syncs belong to.
	lastOK, lastTry time.Time
	touching        bool
	runCtx          context.Context
	// serving: Run (the server's periodic sync) has started. A page only
	// starts a background sync then; tests and command-line-only uses that
	// never run the loop get no syncs behind their back.
	serving bool
}

// The cadence of the catalog sync and the environment variables that
// override it.
const (
	DefaultSyncInterval = 6 * time.Hour
	DefaultStaleAfter   = 30 * time.Minute
	DefaultRetryGap     = 5 * time.Minute

	SyncIntervalEnv = "ACORNFOX_CATALOG_SYNC_INTERVAL"
	StaleAfterEnv   = "ACORNFOX_CATALOG_STALE_AFTER"
	RetryGapEnv     = "ACORNFOX_CATALOG_RETRY_GAP"

	// syncJitterDiv: the periodic sync is early or late by up to
	// interval/syncJitterDiv (6 hours: up to 36 minutes either way).
	syncJitterDiv = 10
	// backgroundSyncLimit bounds one sync started by a page or the CLI.
	backgroundSyncLimit = 3 * time.Minute
)

// durationSetting reads a duration from the environment, within [min, max];
// def when it is unset, does not parse or is out of range.
func durationSetting(env string, def, min, max time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(env))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < min || d > max {
		return def
	}
	return d
}

// CatalogConfig configures NewCatalog.
type CatalogConfig struct {
	CacheDir string              // verified index cache and downloaded packages (<data>/catalog)
	Snapshot fs.FS               // nil => catalog.Snapshot()
	Sources  []string            // nil => catalog.DefaultSources("")
	Keys     []ed25519.PublicKey // nil => catalog.TrustedKeys
	HTTP     *http.Client        // nil => http.DefaultClient
	Now      func() time.Time
	// Gateway is the download gateway for private catalogs (W10) and their
	// packages (W2); nil => no private catalogs (connect is CAT-CONN-02).
	Gateway CatalogGateway
	// Cadence; 0 => the environment variable, else the default (see Catalog).
	SyncInterval, StaleAfter, RetryGap time.Duration
}

// SyncStatus is the last sync result, shown by GET /v1/catalog.
type SyncStatus struct {
	At       *time.Time        `json:"at,omitempty"`   // when the last attempt ended; nil = never
	OK       bool              `json:"ok"`             // the last attempt did not fail
	Skipped  bool              `json:"skipped"`        // no trusted key: nothing contacted
	Updated  bool              `json:"updated"`        // a newer index was taken
	Code     string            `json:"code,omitempty"` // CAT-SYNC-* when not OK or skipped
	Message  string            `json:"message"`
	Source   string            `json:"source,omitempty"`
	Attempts []catalog.Attempt `json:"attempts,omitempty"`
	Next     *time.Time        `json:"next,omitempty"` // next scheduled sync
	// Private is the private catalogs' part of the last sync (nil: never ran).
	Private *PrivateSyncStatus `json:"private,omitempty"`
	// Show is where the display content (show.json) stands. After a sync it
	// is what that sync made of it; GET /v1/catalog fills it with what is in
	// use now (Catalog.Show). Nil: the server does not say (never synced, or
	// every source failed).
	Show *catalog.ShowState `json:"show,omitempty"`
	// Skills is where the operating manuals of apps and solutions stand
	// (catalog.SkillsState): after a sync what that sync made of them; GET
	// /v1/catalog fills it with what is usable now. Nil as for Show.
	Skills *catalog.SkillsState `json:"skills,omitempty"`
}

// NewCatalog builds the holder; it does not sync.
func NewCatalog(cfg CatalogConfig) *Catalog {
	if cfg.Snapshot == nil {
		cfg.Snapshot = catalog.Snapshot()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	c := &Catalog{cfg: cfg}
	c.interval, c.staleAfter, c.retryGap = cfg.SyncInterval, cfg.StaleAfter, cfg.RetryGap
	if c.interval <= 0 {
		c.interval = durationSetting(SyncIntervalEnv, DefaultSyncInterval, time.Minute, 7*24*time.Hour)
	}
	if c.staleAfter <= 0 {
		c.staleAfter = durationSetting(StaleAfterEnv, DefaultStaleAfter, time.Second, 7*24*time.Hour)
	}
	if c.retryGap <= 0 {
		c.retryGap = durationSetting(RetryGapEnv, DefaultRetryGap, time.Second, 24*time.Hour)
	}
	c.runCtx = context.Background()
	c.status = c.loadStatus()
	// What the last run of the server knew counts: a restart does not make
	// the catalog look unsynced.
	if c.status.At != nil {
		c.lastTry = *c.status.At
		if c.status.OK && !c.status.Skipped {
			c.lastOK = *c.status.At
		}
	}
	return c
}

func (c *Catalog) keys() []ed25519.PublicKey {
	if c.cfg.Keys != nil {
		return c.cfg.Keys
	}
	return catalog.TrustedKeys
}

func (c *Catalog) syncConfig() catalog.SyncConfig {
	return catalog.SyncConfig{
		Sources:     c.cfg.Sources,
		CacheDir:    filepath.Join(c.cfg.CacheDir, "index"),
		TrustedKeys: c.keys(),
		HTTP:        c.cfg.HTTP,
		Snapshot:    c.cfg.Snapshot,
	}
}

// Index returns the catalog in use and where it came from (cache|snapshot).
func (c *Catalog) Index() (*catalog.Index, string, error) {
	idx, origin, err := catalog.LoadWith(filepath.Join(c.cfg.CacheDir, "index"), c.cfg.Snapshot, c.keys())
	if err != nil {
		return nil, "", fault(CodePackage, err, "目录读不了：%v", err)
	}
	return idx, origin, nil
}

// Show returns the display content of the public catalog in use and where it
// stands: the verified show of the cache version in use (or the snapshot's),
// checked on every read like the index. Without one the pages use the
// manifests' words: the state is none, or failed (with its CAT-SHOW code)
// when the last sync met a show it could not use. Private catalogs have no
// display content here.
func (c *Catalog) Show() (*catalog.Show, catalog.ShowState) {
	idx, origin, err := catalog.LoadWith(filepath.Join(c.cfg.CacheDir, "index"), c.cfg.Snapshot, c.keys())
	if err != nil {
		return nil, catalog.ShowState{State: catalog.ShowNone}
	}
	s, st := catalog.LoadShow(filepath.Join(c.cfg.CacheDir, "index"), c.cfg.Snapshot, c.keys(), idx, origin)
	if s == nil && st.State == catalog.ShowNone {
		if last := c.Status().Show; last != nil && last.State == catalog.ShowFailed {
			return nil, *last
		}
	}
	return s, st
}

// Skills says which operating manuals of the public catalog's apps and
// solutions are usable on this machine (keyed "app/<id>", "recipe/<id>": the
// file is in the cache and its size and sha256 are the ones the signed
// display content lists) and where the manuals stand overall. A listed
// manual that is not usable is a failure: with the code the last sync gave
// it, else CAT-SKILL-01 (not fetched yet). Nothing is downloaded.
func (c *Catalog) Skills() (map[string]catalog.SkillInfo, catalog.SkillsState) {
	show, _ := c.Show()
	if show == nil {
		return map[string]catalog.SkillInfo{}, catalog.SkillsState{State: catalog.SkillsNone}
	}
	_, origin, err := c.Index()
	if err != nil {
		return map[string]catalog.SkillInfo{}, catalog.SkillsState{State: catalog.SkillsNone}
	}
	var last []catalog.SkillFailure
	if st := c.Status().Skills; st != nil {
		last = st.Failed
	}
	return catalog.VerifySkills(catalog.SkillFS(filepath.Join(c.cfg.CacheDir, "index"), c.cfg.Snapshot, origin), show, last)
}

// Skill reads the operating manual of the public catalog's app or solution:
// kind is catalog.SkillKindApp or SkillKindRecipe. nil, nil when the catalog
// lists none; the error is the CAT-SKILL-* of a listed manual that is not
// usable here.
func (c *Catalog) Skill(kind, id string) (*catalog.SkillDoc, error) {
	show, _ := c.Show()
	if show == nil {
		return nil, nil
	}
	_, origin, err := c.Index()
	if err != nil {
		return nil, nil
	}
	return catalog.ReadSkill(catalog.SkillFS(filepath.Join(c.cfg.CacheDir, "index"), c.cfg.Snapshot, origin), show, kind, id)
}

// Status returns the last sync result.
func (c *Catalog) Status() SyncStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

// Sync runs one sync now (the public catalog, then the private catalogs
// when any was connected) and records the result. OK is false when either
// part failed; the public part's fields stay at the top level.
func (c *Catalog) Sync(ctx context.Context) SyncStatus {
	st := c.syncPublic(ctx)
	ps := c.syncPrivate(ctx)
	st.Private = &ps
	if !ps.OK {
		st.OK = false
		if st.Code == "" || st.Skipped {
			st.Code = ps.Code
		}
	}
	if !ps.OK || ps.Catalogs > 0 || len(ps.Disconnected) > 0 {
		st.Message += "；" + ps.Message
	}
	c.mu.Lock()
	st.Next = c.status.Next
	c.status = st
	if st.At != nil {
		c.lastTry = *st.At
		if st.OK && !st.Skipped {
			c.lastOK = *st.At
		}
	}
	hook := c.afterSync
	c.mu.Unlock()
	c.saveStatus(st)
	if hook != nil {
		hook(ctx)
	}
	return st
}

// SetAfterSync sets what is called after every sync, whatever it found (the
// index in use may have changed even when the sync failed, e.g. a private
// catalog's). nil: nothing.
func (c *Catalog) SetAfterSync(fn func(ctx context.Context)) {
	c.mu.Lock()
	c.afterSync = fn
	c.mu.Unlock()
}

func (c *Catalog) syncPublic(ctx context.Context) SyncStatus {
	now := c.cfg.Now()
	st := SyncStatus{At: &now}
	if len(c.keys()) == 0 {
		st.OK, st.Skipped = true, true
		st.Code = catalog.CodeSyncNoKeys
		st.Message = "程序里还没有受信公钥，不联网同步，只用内置目录"
	} else {
		res, err := catalog.Sync(ctx, c.syncConfig())
		end := c.cfg.Now()
		st.At = &end
		st.Updated, st.Source, st.Attempts = res.Updated, res.Source, res.Attempts
		if res.Show.State != "" {
			show := res.Show
			st.Show = &show
		}
		if res.Skills.State != "" {
			sk := res.Skills
			st.Skills = &sk
		}
		switch {
		case err != nil:
			st.Code = catalog.CodeOf(err)
			st.Message = err.Error()
		case res.Updated:
			st.OK = true
			st.Message = "已更新到新目录"
		case res.RemoteSerial > 0 && res.RemoteSerial < res.Serial:
			st.OK = true
			st.Message = fmt.Sprintf("没换目录：来源 %s 的序号是 %d，比这台机器在用的 %d 还旧（来源落后了，或是测试发布点序号没调大）", res.Source, res.RemoteSerial, res.Serial)
		default:
			st.OK = true
			st.Message = "目录已是最新"
		}
	}
	return st
}

// Run syncs once now and then every interval (with jitter) until ctx ends.
func (c *Catalog) Run(ctx context.Context) {
	c.mu.Lock()
	c.runCtx = ctx
	c.serving = true
	c.mu.Unlock()
	for {
		// The periodic sync takes the same right as a page's: no two at once.
		c.mu.Lock()
		c.touching = true
		c.mu.Unlock()
		c.Sync(ctx)
		c.releaseStale()
		next := c.cfg.Now().Add(nextSyncWait(c.interval))
		c.mu.Lock()
		c.status.Next = &next
		c.mu.Unlock()
		t := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// nextSyncWait is how long the periodic sync waits after one: the interval
// plus or minus a random part of up to a tenth of it.
func nextSyncWait(interval time.Duration) time.Duration {
	j := interval / syncJitterDiv
	if j <= 0 {
		return interval
	}
	return interval - j + randomDuration(2*j)
}

// StaleAfter is how old the last good sync may be before a page or `apps
// outdated` starts one.
func (c *Catalog) StaleAfter() time.Duration { return c.staleAfter }

// Interval is the wait between two periodic syncs (before the jitter).
func (c *Catalog) Interval() time.Duration { return c.interval }

// claimStale decides whether a sync should start for someone who looked at
// the catalog just now, and when it should, takes the right to do it (one at
// a time). It says no without a trusted key (nothing would be contacted),
// while another such sync runs, when the last good one is younger than
// StaleAfter, and when the last attempt, good or not, is younger than
// RetryGap.
func (c *Catalog) claimStale() bool {
	if c == nil || len(c.keys()) == 0 {
		return false
	}
	now := c.cfg.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.touching {
		return false
	}
	if !c.lastOK.IsZero() && now.Sub(c.lastOK) < c.staleAfter {
		return false
	}
	if !c.lastTry.IsZero() && now.Sub(c.lastTry) < c.retryGap {
		return false
	}
	c.touching = true
	return true
}

func (c *Catalog) releaseStale() {
	c.mu.Lock()
	c.touching = false
	c.mu.Unlock()
}

// Stale reports whether TouchStale would start a sync now (read-only).
func (c *Catalog) Stale() bool {
	if !c.claimStale() {
		return false
	}
	c.releaseStale()
	return true
}

// TouchStale starts a sync in the background when the last good one is older
// than StaleAfter (see claimStale) and says whether it did. It returns at
// once: whoever opened the page gets the page as it is, and the next look
// has the news. The sync belongs to the life of the server (Run's context)
// and ends after three minutes at most.
func (c *Catalog) TouchStale() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	serving := c.serving
	c.mu.Unlock()
	if !serving || !c.claimStale() {
		return false
	}
	c.mu.Lock()
	base := c.runCtx
	c.mu.Unlock()
	go func() {
		defer c.releaseStale()
		ctx, cancel := context.WithTimeout(base, backgroundSyncLimit)
		defer cancel()
		c.Sync(ctx)
	}()
	return true
}

// SyncIfStale is TouchStale that waits: it syncs now when the last good sync
// is older than StaleAfter (and the attempt before it is older than
// RetryGap) and returns the result with ran true; otherwise it returns the
// last result with ran false.
func (c *Catalog) SyncIfStale(ctx context.Context) (st SyncStatus, ran bool) {
	if !c.claimStale() {
		return c.Status(), false
	}
	defer c.releaseStale()
	return c.Sync(ctx), true
}

func randomDuration(max time.Duration) time.Duration {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return max / 2
	}
	return time.Duration(binary.BigEndian.Uint64(b[:]) % uint64(max))
}

func (c *Catalog) statusPath() string { return filepath.Join(c.cfg.CacheDir, "sync-status.json") }

func (c *Catalog) loadStatus() SyncStatus {
	var st SyncStatus
	b, err := os.ReadFile(c.statusPath())
	if err != nil || json.Unmarshal(b, &st) != nil {
		return SyncStatus{Message: "还没有同步过"}
	}
	st.Next = nil
	return st
}

func (c *Catalog) saveStatus(st SyncStatus) {
	if c.cfg.CacheDir == "" {
		return
	}
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.MkdirAll(c.cfg.CacheDir, 0o755)
	tmp := c.statusPath() + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, c.statusPath())
	}
}

// App is one app of a catalog in use with its manifest and package files.
type App struct {
	Index    catalog.IndexApp
	Manifest *catalog.Manifest
	Files    map[string][]byte
	// Origin is "" for the public catalog, else the private catalog id.
	Origin     string
	OriginName string
}

// Source returns the app's catalog as the API names it.
func (a *App) Source() SourceRef { return sourceRef(a.Origin, a.OriginName) }

func sourceRef(origin, name string) SourceRef {
	if origin == "" {
		return SourceRef{ID: PublicSource, Name: "公共目录"}
	}
	return SourceRef{ID: origin, Name: name}
}

// catalogView is one catalog in use: the public one (origin "") or a
// connected private one.
type catalogView struct {
	origin, name string
	idx          *catalog.Index
}

// views returns the public catalog followed by the connected private ones
// (by id). The public one is left out only when it cannot be read at all.
func (c *Catalog) views() ([]catalogView, error) {
	var out []catalogView
	idx, _, err := c.Index()
	if err == nil {
		out = append(out, catalogView{idx: idx, name: "公共目录"})
	}
	for _, p := range c.privateIndexes() {
		out = append(out, catalogView{origin: p.src.ID, name: p.src.Name, idx: p.idx})
	}
	if len(out) == 0 {
		return nil, err
	}
	return out, nil
}

// selectViews narrows views to the catalog named by want: "" every catalog
// (public first), "public" the public one, else that private catalog
// (CAT-INST-25 when it was disconnected, CAT-INST-01 when not connected).
func (c *Catalog) selectViews(want string) ([]catalogView, error) {
	want = strings.TrimSpace(want)
	if want != "" && want != PublicSource {
		if !catalog.ValidCatalogID(want) {
			return nil, fault(CodeBadParams, nil, "目录要写 public 或私有目录编号（cat-…），现在是 %q", want)
		}
		if name, disc := c.OriginState(want); disc {
			if findSource(c.loadSources(), want) < 0 {
				return nil, fault(CodeUnknown, nil, "这台机器没有连接私有目录 %s（acornfox catalog sources 看已连接的）", want)
			}
			return nil, fault(CodeDisconnected, nil, "私有目录 %s（%s）已断开，不能再从它安装或升级", name, want)
		}
	}
	all, err := c.views()
	if err != nil {
		return nil, fault(CodePackage, err, "目录读不了：%v", err)
	}
	if want == "" {
		return all, nil
	}
	origin := want
	if want == PublicSource {
		origin = ""
	}
	for _, v := range all {
		if v.origin == origin {
			return []catalogView{v}, nil
		}
	}
	if origin == "" {
		return nil, fault(CodePackage, nil, "公共目录读不了")
	}
	return nil, fault(CodeUnknown, nil, "私有目录 %s 还没有同步到可用的索引（acornfox catalog sync 后再试）", want)
}

// App returns app id from the catalog in use (public first, then the
// private ones), reading (and if needed downloading and verifying) its
// package.
func (c *Catalog) App(ctx context.Context, id string) (*App, error) {
	return c.AppIn(ctx, "", id)
}

// AppIn is App restricted to one catalog ("" any, "public", or a private
// catalog id).
func (c *Catalog) AppIn(ctx context.Context, from, id string) (*App, error) {
	v, ia, err := c.find(from, id)
	if err != nil {
		return nil, err
	}
	return c.loadApp(ctx, v, ia)
}

// find returns app id's entry in the index of catalog from ("" any, public
// first) and the catalog it is in. It reads the index only: nothing is
// downloaded, so what the index says (min_platform) can be checked before
// the app package is fetched.
func (c *Catalog) find(from, id string) (catalogView, catalog.IndexApp, error) {
	vs, err := c.selectViews(from)
	if err != nil {
		return catalogView{}, catalog.IndexApp{}, err
	}
	for _, v := range vs {
		for i := range v.idx.Apps {
			if v.idx.Apps[i].ID == id {
				return v, v.idx.Apps[i], nil
			}
		}
	}
	if from != "" && from != PublicSource {
		return catalogView{}, catalog.IndexApp{}, fault(CodeUnknown, nil, "私有目录 %s 里没有应用 %s", from, id)
	}
	if f := nothingToInstall(vs, "应用 "+id); f != nil {
		return catalogView{}, catalog.IndexApp{}, f
	}
	return catalogView{}, catalog.IndexApp{}, fault(CodeUnknown, nil, "目录里没有应用 %s", id)
}

// nothingToInstall is the CAT-INST-01 for what (an app or a recipe) that is
// not in catalogs vs when none of them has an app this platform could
// install (catalog.NothingToInstall): it says so instead of "not in the
// catalog, sync it", which cannot help then. nil when some app could be
// installed.
func nothingToInstall(vs []catalogView, what string) *Fault {
	for _, v := range vs {
		if v.idx != nil && catalog.HasInstallable(v.idx.Apps) {
			return nil
		}
	}
	f := fault(CodeUnknown, nil, "%s（目录里也没有%s）", catalog.NothingToInstall, what)
	f.Hint = catalog.NothingToInstallHint
	return f
}

func (c *Catalog) loadApp(ctx context.Context, v catalogView, ia catalog.IndexApp) (*App, error) {
	files, err := c.loadPackage(ctx, v, ia)
	if err != nil {
		return nil, err
	}
	return parseApp(v, ia, files)
}

// loadPackage reads (and if needed downloads and verifies) the package of ia
// and returns its files, manifest.yaml among them. The manifest is not
// decoded: an install checks its raw bytes first (Engine.checkPackage).
func (c *Catalog) loadPackage(ctx context.Context, v catalogView, ia catalog.IndexApp) (map[string][]byte, error) {
	id := ia.ID
	var b []byte
	var err error
	if v.origin == "" {
		fetch := func(dest string) error {
			return catalog.FetchPackage(ctx, c.syncConfig(), ia.Package, dest)
		}
		if len(c.keys()) == 0 {
			fetch = nil // no trusted catalog source: only the snapshot's packages exist
		}
		b, err = catalog.LoadPackage(fetch, c.cfg.Snapshot, filepath.Join(c.cfg.CacheDir, "packages"), ia.Package)
	} else {
		b, err = catalog.LoadPackage(c.privatePackageFetch(ctx, v.origin, ia), nil, c.privatePackageDir(v.origin), ia.Package)
	}
	if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			return nil, f
		}
		return nil, fault(CodePackage, err, "应用 %s 的安装包读不了或校验不过", id)
	}
	files, err := catalog.ReadPackage(id, b)
	if err != nil {
		return nil, fault(CodePackage, err, "应用 %s 的安装包格式不对", id)
	}
	if _, ok := files[manifestFile]; !ok {
		return nil, fault(CodePackage, nil, "应用 %s 的安装包里没有 manifest.yaml", id)
	}
	return files, nil
}

// parseApp decodes the manifest of a loaded package and checks it is the
// app and version the index named.
func parseApp(v catalogView, ia catalog.IndexApp, files map[string][]byte) (*App, error) {
	m, err := catalog.ParseManifest(files[manifestFile])
	if err != nil || m.ID != ia.ID || m.Release.Version != ia.Version {
		return nil, fault(CodePackage, err, "应用 %s 的清单和目录对不上", ia.ID)
	}
	return &App{Index: ia, Manifest: m, Files: files, Origin: v.origin, OriginName: v.name}, nil
}

// Recipe returns recipe id from the catalog in use (public first).
func (c *Catalog) Recipe(id string) (*catalog.Recipe, error) {
	r, _, err := c.RecipeIn("", id)
	return r, err
}

// RecipeIn returns recipe id from one catalog ("" any, "public", or a private
// catalog id) and the origin it came from ("" public). A recipe's apps come
// from the same catalog. Recipes that need a newer platform are found too
// (the index lists them apart, see catalog.Index.AllRecipes): the caller
// checks MinPlatform before installing.
func (c *Catalog) RecipeIn(from, id string) (*catalog.Recipe, string, error) {
	vs, err := c.selectViews(from)
	if err != nil {
		return nil, "", err
	}
	for _, v := range vs {
		for _, r := range v.idx.AllRecipes() {
			if r.ID == id {
				return &r, v.origin, nil
			}
		}
	}
	if from == "" || from == PublicSource {
		if f := nothingToInstall(vs, "配方 "+id); f != nil {
			return nil, "", f
		}
	}
	return nil, "", fault(CodeUnknown, nil, "目录里没有配方 %s", id)
}

// manifestFile is the manifest's name inside an app package.
const manifestFile = "manifest.yaml"

// localManifest returns the manifest.yaml bytes of catalog app id at version
// from catalog origin ("" public), using only what is already on this
// machine: the index in use and the built-in snapshot's index name the
// package (path, sha256, size), and the package is read from the snapshot or
// the package cache and checked against them. Nothing is downloaded; false
// when no index here lists that version or its package is not here.
func (c *Catalog) localManifest(origin, id, version string) ([]byte, bool) {
	var indexes []*catalog.Index
	var snapshot fs.FS
	pkgDir := ""
	if origin == "" {
		if idx, _, err := c.Index(); err == nil {
			indexes = append(indexes, idx)
		}
		// The snapshot may still list a version the synced index moved past.
		if idx, _, err := catalog.LoadWith("", c.cfg.Snapshot, nil); err == nil {
			indexes = append(indexes, idx)
		}
		snapshot, pkgDir = c.cfg.Snapshot, filepath.Join(c.cfg.CacheDir, "packages")
	} else {
		if !catalog.ValidCatalogID(origin) {
			return nil, false
		}
		if idx := catalog.LoadIndex(c.privateIndexDir(origin), origin, c.keys()); idx != nil {
			indexes = append(indexes, idx)
		}
		pkgDir = c.privatePackageDir(origin)
	}
	for _, idx := range indexes {
		for _, ia := range idx.Apps {
			if ia.ID != id || ia.Version != version {
				continue
			}
			b, err := catalog.LoadPackage(nil, snapshot, pkgDir, ia.Package)
			if err != nil {
				continue
			}
			files, err := catalog.ReadPackage(id, b)
			if err != nil {
				continue
			}
			raw, ok := files[manifestFile]
			if !ok {
				continue
			}
			if m, err := catalog.ParseManifest(raw); err != nil || m.ID != id || m.Release.Version != version {
				continue
			}
			return raw, true
		}
	}
	return nil, false
}
