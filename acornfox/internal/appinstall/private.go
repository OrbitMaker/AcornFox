package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
)

// Private catalogs (W10).
//
// The owner of a private catalog makes a connection code for one machine;
// `acornfox catalog connect <code>` (POST /v1/catalog/connect) hands it to
// the download gateway, signed with this machine's key, and the gateway
// grants this machine that catalog. Every sync then also asks the gateway
// which private catalogs this machine may see (only when at least one was
// ever connected, so machines without private catalogs never contact it for
// this), verifies each index with the same trusted keys as the public
// catalog and caches it on its own.
//
// Files under <data>/catalog/private/:
//
//	sources.json          connected catalogs: id, name, when connected, last
//	                      sync and its code, whether disconnected (0644, no
//	                      secrets: the connection code is never stored)
//	<id>/index/           verified index, same layout as the public cache
//	<id>/packages/        app packages fetched through W2
//
// When the gateway lists a catalog as revoked (access withdrawn, or the
// catalog disabled), its directory is removed and the source is marked
// disconnected: its apps disappear from the list; apps already installed
// from it keep running but cannot be installed again or upgraded (they are
// shown with catalog_disconnected). Connecting again with a new code, or the
// gateway listing it again (a disabled catalog enabled again), clears that.
//
// Undo: deleting <data>/catalog/private/ forgets every private catalog on
// this machine (the gateway still holds the grant; connecting with the same
// code again from this machine succeeds).

// PublicSource is how the API names the public catalog. Installed apps store
// "" for it (state.App.CatalogOrigin).
const PublicSource = "public"

// CatalogGateway is the download gateway's private-catalog side.
// *gatewayclient.Client satisfies it.
type CatalogGateway interface {
	Connect(ctx context.Context, code string) (gatewayclient.Connected, error)
	CatalogIndex(ctx context.Context, known map[string]int64) (gatewayclient.PrivateIndex, error)
	IssueApp(ctx context.Context, fileID string, app gatewayclient.AppRef) (gatewayclient.Issued, error)
}

// privateSource is one entry of sources.json.
type privateSource struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	ConnectedAt    time.Time  `json:"connected_at"`
	LastSync       *time.Time `json:"last_sync,omitempty"` // the gateway last answered for it
	Code           string     `json:"code,omitempty"`      // problem of the last sync (CAT-SYNC-*)
	Message        string     `json:"message,omitempty"`
	Disconnected   bool       `json:"disconnected"`
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"`
}

type sourcesFile struct {
	Schema   int             `json:"schema"`
	Catalogs []privateSource `json:"catalogs"`
}

// SourceRef names the catalog an app or recipe comes from.
type SourceRef struct {
	ID   string `json:"id"`   // "public" or the private catalog id
	Name string `json:"name"` // shown as "来自：<name>"
}

// PrivateSyncStatus is the private part of a sync.
type PrivateSyncStatus struct {
	OK           bool     `json:"ok"`
	Code         string   `json:"code,omitempty"` // CAT-SYNC-10 when the gateway call failed
	GatewayCode  string   `json:"gateway_code,omitempty"`
	Message      string   `json:"message"`
	Catalogs     int      `json:"catalogs"`     // connected private catalogs after the sync
	Updated      []string `json:"updated"`      // ids that took a newer index
	Disconnected []string `json:"disconnected"` // ids disconnected by this sync
	Problems     []string `json:"problems"`     // "<id> <CAT-SYNC-*> <message>" per catalog that did not take its index
}

func (c *Catalog) privateDir() string { return filepath.Join(c.cfg.CacheDir, "private") }

func (c *Catalog) sourcesPath() string { return filepath.Join(c.privateDir(), "sources.json") }

func (c *Catalog) privateIndexDir(id string) string {
	return filepath.Join(c.privateDir(), id, "index")
}

func (c *Catalog) privatePackageDir(id string) string {
	return filepath.Join(c.privateDir(), id, "packages")
}

// loadSources reads sources.json (empty when missing; entries with a bad id
// are dropped, so a hand-edited file cannot point outside the directory).
func (c *Catalog) loadSources() []privateSource {
	b, err := os.ReadFile(c.sourcesPath())
	if err != nil {
		return nil
	}
	var f sourcesFile
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	out := f.Catalogs[:0]
	for _, s := range f.Catalogs {
		if catalog.ValidCatalogID(s.ID) {
			if s.Name = gatewayclient.CleanCatalogName(s.Name); s.Name == "" {
				s.Name = s.ID
			}
			out = append(out, s)
		}
	}
	return out
}

func (c *Catalog) saveSources(src []privateSource) error {
	sort.Slice(src, func(i, j int) bool { return src[i].ID < src[j].ID })
	b, err := json.MarshalIndent(sourcesFile{Schema: 1, Catalogs: src}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.privateDir(), 0o755); err != nil {
		return err
	}
	tmp := c.sourcesPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.sourcesPath())
}

func findSource(src []privateSource, id string) int {
	for i := range src {
		if src[i].ID == id {
			return i
		}
	}
	return -1
}

// privateCatalog is a connected private catalog with its verified index.
type privateCatalog struct {
	src privateSource
	idx *catalog.Index
}

// privateIndexes returns the connected private catalogs that have a verified
// index, sorted by id.
func (c *Catalog) privateIndexes() []privateCatalog {
	keys := c.keys()
	var out []privateCatalog
	for _, s := range c.loadSources() {
		if s.Disconnected {
			continue
		}
		if idx := catalog.LoadIndex(c.privateIndexDir(s.ID), s.ID, keys); idx != nil {
			out = append(out, privateCatalog{src: s, idx: idx})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].src.ID < out[j].src.ID })
	return out
}

// OriginState says where an installed app came from: the catalog's name and
// whether it is a private catalog that was disconnected. origin "" is the
// public catalog. An origin no longer in sources.json counts as disconnected.
func (c *Catalog) OriginState(origin string) (name string, disconnected bool) {
	if origin == "" {
		return "公共目录", false
	}
	for _, s := range c.loadSources() {
		if s.ID == origin {
			return s.Name, s.Disconnected
		}
	}
	return origin, true
}

// NormalizeCode applies the gateway's lenient reading of a connection code
// (drop dashes and blanks, upper-case, 0/1/8 read as O/I/B, strip a leading
// AFXC from a 20-character input) and reports whether 16 valid characters
// remain. It lets a mistyped code be refused before it costs one of the
// machine's hourly tries.
func NormalizeCode(s string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '\n' || r == '\r':
			continue
		case r == '0':
			r = 'O'
		case r == '1':
			r = 'I'
		case r == '8':
			r = 'B'
		}
		if !(r >= 'A' && r <= 'Z' || r >= '2' && r <= '7') {
			return "", false
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) == 20 && strings.HasPrefix(out, "AFXC") {
		out = out[4:]
	}
	return out, len(out) == 16
}

// ConnectView is POST /v1/catalog/connect's answer.
type ConnectView struct {
	Catalog SourceRef          `json:"catalog"`
	Again   bool               `json:"again"` // this machine had connected it before
	Sync    *PrivateSyncStatus `json:"sync,omitempty"`
}

// Connect hands a connection code to the gateway and records the catalog it
// grants, then syncs the private catalogs so its apps show at once (a failed
// sync is reported in Sync, the connection stands). The code is neither
// logged nor stored.
func (c *Catalog) Connect(ctx context.Context, code string) (ConnectView, error) {
	raw, ok := NormalizeCode(code)
	if !ok {
		return ConnectView{}, fault(CodeConnFormat, nil, "连接码格式不对：应该是 AFXC- 加 16 个字母数字（每 4 个一组），没有发给网关")
	}
	if c.cfg.Gateway == nil {
		return ConnectView{}, fault(CodeConnNoGateway, nil, "这台服务器关掉了下载网关（ACORNFOX_GATEWAY 或 network.json 的 gateway），连不了私有目录")
	}
	conn, err := c.cfg.Gateway.Connect(ctx, "AFXC-"+raw[0:4]+"-"+raw[4:8]+"-"+raw[8:12]+"-"+raw[12:16])
	if err != nil {
		return ConnectView{}, connectFault(err)
	}
	if conn.Name == "" {
		conn.Name = conn.CatalogID // the gateway's name was empty or unprintable
	}
	c.pmu.Lock()
	src := c.loadSources()
	now := c.cfg.Now().UTC()
	v := ConnectView{Catalog: SourceRef{ID: conn.CatalogID, Name: conn.Name}}
	if i := findSource(src, conn.CatalogID); i >= 0 {
		v.Again = true
		src[i].Name, src[i].Disconnected, src[i].DisconnectedAt = conn.Name, false, nil
		src[i].Code, src[i].Message = "", ""
	} else {
		src = append(src, privateSource{ID: conn.CatalogID, Name: conn.Name, ConnectedAt: now})
	}
	err = c.saveSources(src)
	c.pmu.Unlock()
	if err != nil {
		return v, fault(CodeConnSave, err, "网关已经给这台机器开了私有目录 %s，但本机没记下来；用同一个连接码再连一次即可（同一台机器重复用不算新的一次）", conn.Name)
	}
	ps := c.syncPrivate(ctx)
	v.Sync = &ps
	c.mu.Lock()
	c.status.Private = &ps
	st := c.status
	c.mu.Unlock()
	c.saveStatus(st)
	return v, nil
}

// connectFault maps a gateway error of Connect to CAT-CONN-*.
func connectFault(err error) error {
	gw := gatewayclient.CodeOf(err)
	var f *Fault
	switch gw {
	case "GW-CAT-01":
		f = fault(CodeConnUnknown, err, "连接码不存在、已经被别的机器用过，或抄错了")
	case "GW-CAT-02":
		f = fault(CodeConnExpired, err, "连接码已过期")
	case "GW-CAT-03":
		f = fault(CodeConnDisabled, err, "这个私有目录已停用")
	case "GW-CAT-04":
		f = fault(CodeConnTooMany, err, "连接码试得太频繁（每台机器每小时有次数限制）")
		var ge *gatewayclient.Error
		if errors.As(err, &ge) && ge.RetryAfter > 0 {
			f.RetryAfter = int(ge.RetryAfter / time.Second)
			f.Message += fmt.Sprintf("，%d 秒后再试", f.RetryAfter)
		}
	default:
		code := gw
		if code == "" {
			code = "ID-KEY"
		}
		f = fault(CodeConnGateway, err, "连不上下载网关或网关拒绝（%s）", code)
	}
	f.GatewayCode = gw
	if gw != "" && !strings.Contains(f.Message, gw) {
		f.Message += "（" + gw + "）"
	}
	return f
}

// syncPrivate asks the gateway for the private catalogs and updates the
// local copies. It does nothing (OK, no contact) when no private catalog was
// ever connected.
func (c *Catalog) syncPrivate(ctx context.Context) PrivateSyncStatus {
	c.pmu.Lock()
	defer c.pmu.Unlock()
	st := PrivateSyncStatus{OK: true, Updated: []string{}, Disconnected: []string{}, Problems: []string{}}
	src := c.loadSources()
	if len(src) == 0 {
		st.Message = "没有连接私有目录"
		return st
	}
	if c.cfg.Gateway == nil {
		st.OK, st.Code = false, catalog.CodeSyncGateway
		st.Message = "下载网关已关掉，私有目录没有同步，继续用手上的"
		st.Catalogs = countConnected(src)
		return st
	}
	keys := c.keys()
	known := map[string]int64{}
	for _, s := range src {
		if s.Disconnected {
			continue
		}
		var serial int64
		if idx := catalog.LoadIndex(c.privateIndexDir(s.ID), s.ID, keys); idx != nil {
			serial = idx.Serial
		}
		known[s.ID] = serial
	}
	ans, err := c.cfg.Gateway.CatalogIndex(ctx, known)
	if err != nil {
		st.OK, st.Code, st.GatewayCode = false, catalog.CodeSyncGateway, gatewayclient.CodeOf(err)
		st.Message = "向下载网关取私有目录失败，继续用手上的：" + err.Error()
		st.Catalogs = countConnected(src)
		return st
	}
	now := c.cfg.Now().UTC()
	for _, pc := range ans.Catalogs {
		i := findSource(src, pc.ID)
		if i < 0 {
			// The gateway holds a grant this machine had not recorded (the
			// answer to an earlier connect was lost): take it.
			name := pc.Name
			if name == "" {
				name = pc.ID
			}
			src = append(src, privateSource{ID: pc.ID, Name: name, ConnectedAt: now})
			i = len(src) - 1
		}
		s := &src[i]
		if s.Disconnected {
			s.Disconnected, s.DisconnectedAt = false, nil
		}
		if pc.Name != "" {
			s.Name = pc.Name
		}
		s.LastSync = &now
		s.Code, s.Message = "", ""
		if !pc.Full() {
			continue
		}
		if len(keys) == 0 {
			s.Code, s.Message = catalog.CodeSyncNoKeys, "程序里没有受信公钥，私有目录索引验不了，不用"
			st.Problems = append(st.Problems, s.ID+" "+s.Code+" "+s.Message)
			continue
		}
		updated, _, err := catalog.StoreIndex(ctx, c.privateIndexDir(s.ID), s.ID, []byte(pc.Index), []byte(pc.Sig), pc.Serial, keys)
		if err != nil {
			s.Code, s.Message = catalog.CodeOf(err), err.Error()
			if s.Code == "" {
				s.Code = catalog.CodeSyncCache
			}
			st.Problems = append(st.Problems, s.ID+" "+s.Code+" "+s.Message)
			continue
		}
		if updated {
			st.Updated = append(st.Updated, s.ID)
		}
	}
	for _, id := range ans.Revoked {
		i := findSource(src, id)
		if i < 0 || src[i].Disconnected {
			continue
		}
		s := &src[i]
		s.Disconnected, s.DisconnectedAt, s.LastSync = true, &now, &now
		s.Code, s.Message = catalog.CodeSyncDisconnected, "授权已撤销或目录已停用，已断开"
		_ = os.RemoveAll(filepath.Join(c.privateDir(), s.ID))
		st.Disconnected = append(st.Disconnected, s.ID)
	}
	if err := c.saveSources(src); err != nil {
		st.OK, st.Code = false, catalog.CodeSyncCache
		st.Message = "私有目录的记录写不进去：" + err.Error()
		return st
	}
	st.Catalogs = countConnected(src)
	parts := []string{fmt.Sprintf("私有目录 %d 个", st.Catalogs)}
	if len(st.Updated) > 0 {
		parts = append(parts, "更新了 "+strings.Join(st.Updated, "、"))
	}
	if len(st.Disconnected) > 0 {
		parts = append(parts, "已断开 "+strings.Join(st.Disconnected, "、"))
	}
	if len(st.Problems) > 0 {
		st.OK = false
		st.Code = strings.Fields(st.Problems[0])[1]
		parts = append(parts, fmt.Sprintf("%d 个没能更新", len(st.Problems)))
	}
	st.Message = strings.Join(parts, "；")
	return st
}

func countConnected(src []privateSource) int {
	n := 0
	for _, s := range src {
		if !s.Disconnected {
			n++
		}
	}
	return n
}

// SourceView is one catalog in GET /v1/catalog/sources.
type SourceView struct {
	ID             string     `json:"id"`   // "public" or the private catalog id
	Kind           string     `json:"kind"` // public | private
	Name           string     `json:"name"`
	Serial         int64      `json:"serial"`           // 0: no verified index yet
	Origin         string     `json:"origin,omitempty"` // public: cache | snapshot
	Apps           int        `json:"apps"`
	Recipes        int        `json:"recipes"`
	ConnectedAt    *time.Time `json:"connected_at,omitempty"`
	LastSync       *time.Time `json:"last_sync,omitempty"`
	Code           string     `json:"code,omitempty"`
	Message        string     `json:"message,omitempty"`
	Disconnected   bool       `json:"disconnected"`
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"`
}

// Sources lists the public catalog and every private catalog this machine
// connected (disconnected ones included).
func (c *Catalog) Sources() []SourceView {
	st := c.Status()
	pub := SourceView{ID: PublicSource, Kind: "public", Name: "公共目录", LastSync: st.At, Code: st.Code, Message: st.Message}
	if idx, origin, err := c.Index(); err == nil {
		pub.Serial, pub.Origin, pub.Apps, pub.Recipes = idx.Serial, origin, len(idx.Apps), len(idx.Recipes)
	}
	out := []SourceView{pub}
	keys := c.keys()
	src := c.loadSources()
	sort.Slice(src, func(i, j int) bool { return src[i].ID < src[j].ID })
	for _, s := range src {
		at := s.ConnectedAt
		v := SourceView{ID: s.ID, Kind: "private", Name: s.Name, ConnectedAt: &at, LastSync: s.LastSync,
			Code: s.Code, Message: s.Message, Disconnected: s.Disconnected, DisconnectedAt: s.DisconnectedAt}
		if !s.Disconnected {
			if idx := catalog.LoadIndex(c.privateIndexDir(s.ID), s.ID, keys); idx != nil {
				v.Serial, v.Apps, v.Recipes = idx.Serial, len(idx.Apps), len(idx.Recipes)
			}
		}
		out = append(out, v)
	}
	return out
}

// privatePackageFetch downloads a private app package through W2 (file
// catpkg-<id>-<version>, purpose app, this catalog named) into dest, checked
// against the signed index's sha256 and size. A GW-CAT-* refusal is
// CAT-INST-26: there is no public source to fall back to.
func (c *Catalog) privatePackageFetch(ctx context.Context, origin string, ia catalog.IndexApp) func(dest string) error {
	return func(dest string) error {
		if c.cfg.Gateway == nil {
			return fault(CodePrivateDenied, nil, "下载网关已关掉，取不到私有目录 %s 的应用包", origin)
		}
		fileID := catalog.PrivatePackageFileID(ia.ID, ia.Version)
		iss, err := c.cfg.Gateway.IssueApp(ctx, fileID, gatewayclient.AppRef{Catalog: origin, App: ia.ID})
		if err != nil {
			return privateIssueFault(err, "应用包 "+fileID)
		}
		if iss.SHA256 != "" && !strings.EqualFold(iss.SHA256, ia.Package.SHA256) {
			return fault(CodePackage, nil, "下载网关给的 %s 校验值和目录里的不一致，没有下载", fileID)
		}
		if iss.Size > 0 && iss.Size != ia.Package.Size {
			return fault(CodePackage, nil, "下载网关给的 %s 大小和目录里的不一致，没有下载", fileID)
		}
		src := dlSource{URL: iss.URL, ExpiresAt: iss.ExpiresAt, Reissue: func(ctx context.Context) (string, time.Time, error) {
			again, err := c.cfg.Gateway.IssueApp(ctx, fileID, gatewayclient.AppRef{Catalog: origin, App: ia.ID})
			if err != nil {
				return "", time.Time{}, privateIssueFault(err, "应用包 "+fileID)
			}
			if again.SHA256 != "" && !strings.EqualFold(again.SHA256, ia.Package.SHA256) {
				return "", time.Time{}, fault(CodePackage, nil, "下载网关给的 %s 校验值和目录里的不一致，没有下载", fileID)
			}
			return again.URL, again.ExpiresAt, nil
		}}
		if err := downloadChecked(ctx, c.client(), src, dest, ia.Package.SHA256, ia.Package.Size, nil); err != nil {
			var f *Fault
			if errors.As(err, &f) && (f.Code == CodePrivateDenied || f.Code == CodePackage) {
				return f
			}
			_ = os.Remove(dest)
			return fault(CodePackage, scrubURL(err), "下载 %s 失败", fileID)
		}
		return nil
	}
}

// privateIssueFault maps a W2 error for a private file: GW-CAT-* is
// CAT-INST-26 (no public fallback); anything else is returned as is.
func privateIssueFault(err error, what string) error {
	gw := gatewayclient.CodeOf(err)
	if strings.HasPrefix(gw, "GW-CAT-") || gw == "GW-ART-01" {
		f := fault(CodePrivateDenied, err, "下载网关不给 %s（%s）：这台机器对这个私有目录可能已经没有授权", what, gw)
		f.GatewayCode = gw
		return f
	}
	return scrubURL(err)
}

func (c *Catalog) client() *http.Client {
	if c.cfg.HTTP != nil {
		return c.cfg.HTTP
	}
	return http.DefaultClient
}
