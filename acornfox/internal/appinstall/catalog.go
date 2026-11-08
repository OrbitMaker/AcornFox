package appinstall

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
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
// <data>/catalog, or the snapshot built into the program. It syncs once when
// the server starts and then once a day at a random time of day (so machines
// do not all ask at the same minute), and on POST /v1/catalog/sync.
//
// While the program has no trusted public key (catalog.TrustedKeys empty, the
// state until the release signing key exists) it never contacts a source:
// the status says so and only the built-in snapshot is used.
type Catalog struct {
	cfg CatalogConfig

	mu     sync.Mutex
	status SyncStatus

	pmu sync.Mutex // serializes changes to the private catalogs (private.go)

	icons sync.Map // package sha256 -> listIcon (icons.go)
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
	c.status = c.loadStatus()
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
	c.mu.Unlock()
	c.saveStatus(st)
	return st
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
		switch {
		case err != nil:
			st.Code = catalog.CodeOf(err)
			st.Message = err.Error()
		case res.Updated:
			st.OK = true
			st.Message = "已更新到新目录"
		default:
			st.OK = true
			st.Message = "目录已是最新"
		}
	}
	return st
}

// Run syncs once now and then daily at a random time of day until ctx ends.
func (c *Catalog) Run(ctx context.Context) {
	offset := randomDuration(24 * time.Hour)
	for {
		c.Sync(ctx)
		next := nextDaily(c.cfg.Now(), offset)
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

// nextDaily is the next time after now that is offset past local midnight.
func nextDaily(now time.Time, offset time.Duration) time.Time {
	y, m, d := now.Date()
	t := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).Add(offset)
	for !t.After(now) {
		t = t.Add(24 * time.Hour)
	}
	return t
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
	vs, err := c.selectViews(from)
	if err != nil {
		return nil, err
	}
	for _, v := range vs {
		for i := range v.idx.Apps {
			if v.idx.Apps[i].ID == id {
				return c.loadApp(ctx, v, v.idx.Apps[i])
			}
		}
	}
	if from != "" && from != PublicSource {
		return nil, fault(CodeUnknown, nil, "私有目录 %s 里没有应用 %s", from, id)
	}
	return nil, fault(CodeUnknown, nil, "目录里没有应用 %s", id)
}

func (c *Catalog) loadApp(ctx context.Context, v catalogView, ia catalog.IndexApp) (*App, error) {
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
	mb, ok := files["manifest.yaml"]
	if !ok {
		return nil, fault(CodePackage, nil, "应用 %s 的安装包里没有 manifest.yaml", id)
	}
	m, err := catalog.ParseManifest(mb)
	if err != nil || m.ID != id || m.Release.Version != ia.Version {
		return nil, fault(CodePackage, err, "应用 %s 的清单和目录对不上", id)
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
// from the same catalog.
func (c *Catalog) RecipeIn(from, id string) (*catalog.Recipe, string, error) {
	vs, err := c.selectViews(from)
	if err != nil {
		return nil, "", err
	}
	for _, v := range vs {
		for i := range v.idx.Recipes {
			if v.idx.Recipes[i].ID == id {
				r := v.idx.Recipes[i]
				return &r, v.origin, nil
			}
		}
	}
	return nil, "", fault(CodeUnknown, nil, "目录里没有配方 %s", id)
}
