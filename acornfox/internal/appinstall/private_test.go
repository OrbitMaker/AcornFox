package appinstall

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/identity"
	"github.com/acornfox/acornfox/internal/state"
)

// ---- a download gateway that checks requests the way the contract says ----

type fakeCat struct {
	name, status string
	serial       int64
	index, sig   string
	files        map[string]bool
}

// contractGateway implements W1, W2 (with private files) and W10. Every
// request is checked like the real gateway does: headers, timestamp window,
// nonce reuse, install id, and the AFX-GW-1 signature over the raw body.
type contractGateway struct {
	t   *testing.T
	mu  sync.Mutex
	dl  *httptest.Server
	srv *httptest.Server

	keys     map[string]string // install id -> pubkey
	nonces   map[string]bool
	codes    map[string]string // normalized code -> catalog id
	used     map[string]string // normalized code -> install id
	grants   map[string]map[string]bool
	cats     map[string]*fakeCat
	blobs    map[string][]byte // file id -> bytes served by dl
	private  map[string]bool   // file ids that are private
	bodies   []string          // raw bodies seen (to check no code is logged elsewhere)
	indexReq []map[string]any
	issueReq []map[string]any
	down     bool // answer every W10 call with 502
	dlFail   int  // answer every download with this HTTP status (0: serve)
}

func newContractGateway(t *testing.T) *contractGateway {
	g := &contractGateway{t: t, keys: map[string]string{}, nonces: map[string]bool{}, codes: map[string]string{},
		used: map[string]string{}, grants: map[string]map[string]bool{}, cats: map[string]*fakeCat{},
		blobs: map[string][]byte{}, private: map[string]bool{}}
	g.dl = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		b, ok := g.blobs[strings.TrimPrefix(r.URL.Path, "/f/")]
		fail := g.dlFail
		g.mu.Unlock()
		if fail != 0 {
			w.WriteHeader(fail)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	g.srv = httptest.NewServer(g)
	t.Cleanup(func() { g.srv.Close(); g.dl.Close() })
	return g
}

func gwFail(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "code": code, "message": "测试：" + code})
}

func gwOK(w http.ResponseWriter, v map[string]any) {
	v["ok"] = true
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (g *contractGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	iid := r.Header.Get("X-AFX-Install")
	ts, err := strconv.ParseInt(r.Header.Get("X-AFX-Timestamp"), 10, 64)
	nonce, sig := r.Header.Get("X-AFX-Nonce"), r.Header.Get("X-AFX-Signature")
	if r.Method != http.MethodPost || err != nil || len(nonce) != 22 || r.Header.Get("Content-Type") != "application/json" {
		gwFail(w, 400, "GW-REQ-01")
		return
	}
	if d := time.Now().Unix() - ts; d > 300 || d < -300 {
		gwFail(w, 401, "GW-AUTH-01")
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.nonces[iid+nonce] {
		gwFail(w, 401, "GW-AUTH-01")
		return
	}
	g.nonces[iid+nonce] = true
	pubB64 := g.keys[iid]
	if r.URL.Path == "/v1/register" {
		var req struct {
			PubKey string `json:"pubkey"`
		}
		_ = json.Unmarshal(body, &req)
		if want, _ := identity.InstallID(req.PubKey); want != iid {
			gwFail(w, 401, "GW-AUTH-01")
			return
		}
		pubB64 = req.PubKey
	} else if pubB64 == "" {
		gwFail(w, 401, "GW-AUTH-02")
		return
	}
	pub, err := identity.DecodePublicKey(pubB64)
	if err != nil || !identity.Verify(pub, r.Method, r.URL.Path, ts, nonce, body, sig) {
		gwFail(w, 401, "GW-AUTH-01")
		return
	}
	switch r.URL.Path {
	case "/v1/register":
		g.keys[iid] = pubB64
		gwOK(w, map[string]any{"install_id": iid, "limits": map[string]any{"per_day": 20}})
	case "/v1/catalog/connect":
		g.connect(w, iid, body)
	case "/v1/catalog/index":
		g.index(w, iid, body)
	case "/v1/issue":
		g.issue(w, iid, body)
	default:
		gwFail(w, 404, "GW-REQ-01")
	}
}

func (g *contractGateway) connect(w http.ResponseWriter, iid string, body []byte) {
	if g.down {
		gwFail(w, 502, "GW-BE-01")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(body, &req)
	raw, ok := NormalizeCode(req.Code)
	cat := g.codes[raw]
	if !ok || cat == "" || (g.used[raw] != "" && g.used[raw] != iid) {
		gwFail(w, 404, "GW-CAT-01")
		return
	}
	if g.cats[cat].status != "active" {
		gwFail(w, 403, "GW-CAT-03")
		return
	}
	g.used[raw] = iid
	if g.grants[cat] == nil {
		g.grants[cat] = map[string]bool{}
	}
	g.grants[cat][iid] = true
	gwOK(w, map[string]any{"catalog": map[string]any{"id": cat, "name": g.cats[cat].name}, "subject": "machine:" + iid})
}

func (g *contractGateway) index(w http.ResponseWriter, iid string, body []byte) {
	if g.down {
		gwFail(w, 502, "GW-BE-01")
		return
	}
	var req struct {
		Known map[string]int64 `json:"known"`
	}
	_ = json.Unmarshal(body, &req)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	g.indexReq = append(g.indexReq, m)
	out, revoked := []any{}, []any{}
	granted := map[string]bool{}
	for id, c := range g.cats {
		if !g.grants[id][iid] {
			continue
		}
		granted[id] = true
		if c.status != "active" {
			revoked = append(revoked, id)
			continue
		}
		if k, ok := req.Known[id]; ok && k == c.serial {
			out = append(out, map[string]any{"id": id, "serial": c.serial})
			continue
		}
		out = append(out, map[string]any{"id": id, "name": c.name, "serial": c.serial, "index": c.index, "sig": c.sig})
	}
	for id := range req.Known {
		if !granted[id] {
			revoked = append(revoked, id)
		}
	}
	gwOK(w, map[string]any{"catalogs": out, "revoked": revoked})
}

func (g *contractGateway) issue(w http.ResponseWriter, iid string, body []byte) {
	var req struct {
		FileID  string `json:"file_id"`
		Purpose string `json:"purpose"`
		App     *struct {
			Catalog string `json:"catalog"`
			App     string `json:"app"`
		} `json:"app"`
	}
	_ = json.Unmarshal(body, &req)
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	g.issueReq = append(g.issueReq, m)
	if req.Purpose != "app" || req.App == nil || req.App.Catalog == "" {
		if g.private[req.FileID] {
			gwFail(w, 404, "GW-ART-01")
			return
		}
	} else {
		c := g.cats[req.App.Catalog]
		if c == nil || !g.grants[req.App.Catalog][iid] || c.status != "active" || !c.files[req.FileID] {
			gwFail(w, 403, "GW-CAT-05")
			return
		}
	}
	b, ok := g.blobs[req.FileID]
	if !ok {
		gwFail(w, 404, "GW-ART-01")
		return
	}
	gwOK(w, map[string]any{"file_id": req.FileID, "url": g.dl.URL + "/f/" + req.FileID, "expires_at": time.Now().Add(15 * time.Minute).Format(time.RFC3339),
		"sha256": sha256hex(b), "size": len(b), "backend": "fake", "quota": map[string]any{"day_used": 1, "day_limit": 20}})
}

// publish puts a private catalog built from the test catalog (signed with
// priv, bound to id) on the gateway, with its packages as private files
// catpkg-<id>-<ver>.
func (g *contractGateway) publish(t *testing.T, id, name string, serial int64, priv ed25519.PrivateKey) *catalog.Bundle {
	return g.publishAs(t, id, id, name, serial, priv)
}

// publishAs is publish with the index bound to label instead of id ("" for
// an unlabelled index).
func (g *contractGateway) publishAs(t *testing.T, id, label, name string, serial int64, priv ed25519.PrivateKey) *catalog.Bundle {
	t.Helper()
	b, err := catalog.BuildFor(filepath.Join("..", "catalog", "testdata", "good"), serial, label)
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	idx, _ := catalog.ParseIndex(b.Index)
	g.mu.Lock()
	defer g.mu.Unlock()
	c := &fakeCat{name: name, status: "active", serial: serial, index: string(b.Index), sig: string(b.Sig), files: map[string]bool{}}
	for _, a := range idx.Apps {
		fid := catalog.PrivatePackageFileID(a.ID, a.Version)
		c.files[fid], g.private[fid] = true, true
		g.blobs[fid] = b.Packages[a.Package.Path]
	}
	g.cats[id] = c
	return b
}

func (g *contractGateway) makeCode(cat, raw16 string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.codes[raw16] = cat
	return "AFXC-" + raw16[0:4] + "-" + raw16[4:8] + "-" + raw16[8:12] + "-" + raw16[12:16]
}

func (g *contractGateway) revoke(cat string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for k := range g.grants[cat] {
		delete(g.grants[cat], k)
	}
}

// privateSetup is setup() with a catalog that trusts a fresh test key and
// talks to the contract gateway through a real gatewayclient.Client.
func privateSetup(t *testing.T) (*env, *contractGateway, ed25519.PrivateKey, *gatewayclient.Client) {
	t.Helper()
	v := setup(t)
	g := newContractGateway(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	id, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gc := &gatewayclient.Client{BaseURL: g.srv.URL, Identity: id, Info: gatewayclient.DefaultInfo("0.5.0-test")}
	// The public catalog source is unreachable: only the snapshot and the
	// private catalogs are in play.
	v.e.cfg.Catalog = NewCatalog(CatalogConfig{CacheDir: filepath.Join(t.TempDir(), "catalog"), Keys: []ed25519.PublicKey{pub},
		Sources: []string{"http://127.0.0.1:1"}, Gateway: gc})
	return v, g, priv, gc
}

func TestPrivateCatalogLifecycle(t *testing.T) {
	v, g, priv, gc := privateSetup(t)
	ctx := context.Background()
	c := v.e.Catalog()

	// No private catalog yet: a sync does not contact the gateway.
	st := c.Sync(ctx)
	if st.Private == nil || !st.Private.OK || len(g.indexReq) != 0 {
		t.Fatalf("没连过私有目录就不该联系网关：%+v %d", st.Private, len(g.indexReq))
	}

	g.publish(t, "cat-demo", "演示客户", 3, priv)
	code := g.makeCode("cat-demo", "AAAABBBBCCCCDDDD")
	// A mistyped code is refused here and costs no try at the gateway.
	if _, err := c.Connect(ctx, "AFXC-AAAA-BBBB"); code0(err) != CodeConnFormat {
		t.Fatalf("格式不对：%v", err)
	}
	cv, err := c.Connect(ctx, strings.ToLower(code))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if cv.Catalog.ID != "cat-demo" || cv.Catalog.Name != "演示客户" || cv.Sync == nil || !cv.Sync.OK || len(cv.Sync.Updated) != 1 {
		t.Fatalf("connect view %+v %+v", cv, cv.Sync)
	}
	// The same machine may use its code again (a lost answer); the gateway says ok.
	if cv2, err := c.Connect(ctx, code); err != nil || !cv2.Again {
		t.Fatalf("同一台机器重复用：%+v %v", cv2, err)
	}
	// The code is not kept anywhere on disk.
	filepath.Walk(c.cfg.CacheDir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), "AAAABBBBCCCCDDDD") || strings.Contains(string(b), "AAAA-BBBB") {
				t.Errorf("连接码写进了 %s", p)
			}
		}
		return nil
	})

	// Sources: public plus the private one, with its serial.
	srcs := c.Sources()
	if len(srcs) != 2 || srcs[0].ID != PublicSource || srcs[1].ID != "cat-demo" || srcs[1].Serial != 3 || srcs[1].Apps != 2 || srcs[1].Disconnected {
		t.Fatalf("sources %+v", srcs)
	}

	// The list merges both, each app naming its source.
	lv, err := v.e.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	from := map[string]int{}
	for _, a := range lv.Apps {
		from[a.Source.ID]++
		if a.Source.ID == "cat-demo" && (a.Source.Name != "演示客户" || !strings.HasSuffix(a.Command, "--catalog cat-demo")) {
			t.Fatalf("私有应用 %+v", a)
		}
	}
	if from[PublicSource] != 2 || from["cat-demo"] != 2 || len(lv.Sources) != 2 {
		t.Fatalf("合并列表 %v", from)
	}
	for _, r := range lv.Recipes {
		if r.Source.ID == "" {
			t.Fatalf("配方没写来源 %+v", r)
		}
	}

	// Next sync sends the serial it holds and gets no index back.
	st = c.Sync(ctx)
	last := g.indexReq[len(g.indexReq)-1]
	if known, _ := last["known"].(map[string]any); known["cat-demo"] != float64(3) || !st.Private.OK || len(st.Private.Updated) != 0 {
		t.Fatalf("第二次同步 %v %+v", last, st.Private)
	}
	if v, ok := last["account_token"]; !ok || v != nil {
		t.Fatalf("account_token 要传 null：%v", last)
	}

	// Install from the private catalog: the package comes through W2 with
	// the catalog named, file catpkg-<id>-<version>. The test catalog's
	// new-api names no image archive, so its image has to be on this machine
	// already (a private app is never pulled from a public registry).
	adoptPrivateImage(t, v, "cat-demo", "new-api")
	tv := v.install(t, Request{App: "new-api", Catalog: "cat-demo"})
	if tv.Status != state.TaskSucceeded || tv.Catalog != "cat-demo" {
		t.Fatalf("task %+v", tv)
	}
	a, _ := v.w.st.GetApp(ctx, "new-api")
	if a.CatalogOrigin != "cat-demo" {
		t.Fatalf("应用没记下来源：%+v", a)
	}
	var pkgReq map[string]any
	for _, r := range g.issueReq {
		if strings.HasPrefix(r["file_id"].(string), "catpkg-new-api-") {
			pkgReq = r
		}
	}
	app, _ := pkgReq["app"].(map[string]any)
	if pkgReq == nil || pkgReq["purpose"] != "app" || app["catalog"] != "cat-demo" || app["app"] != "new-api" {
		t.Fatalf("应用包请求 %v", pkgReq)
	}
	if name, disc := c.OriginState("cat-demo"); name != "演示客户" || disc {
		t.Fatal("还连着时不该是已断开")
	}
	// The installed app shows under the private entry only.
	lv, _ = v.e.List(ctx)
	for _, x := range lv.Apps {
		if x.ID == "new-api" && (len(x.Installed) == 1) != (x.Source.ID == "cat-demo") {
			t.Fatalf("已安装的归属不对 %+v", x)
		}
	}

	// Revoke: the next sync disconnects it, removes its cache, keeps the app.
	g.revoke("cat-demo")
	st = c.Sync(ctx)
	if len(st.Private.Disconnected) != 1 || st.Private.Disconnected[0] != "cat-demo" {
		t.Fatalf("撤销后同步 %+v", st.Private)
	}
	if _, err := os.Stat(filepath.Join(c.privateDir(), "cat-demo")); !os.IsNotExist(err) {
		t.Fatal("断开后缓存还在")
	}
	srcs = c.Sources()
	if !srcs[1].Disconnected || srcs[1].Code != catalog.CodeSyncDisconnected || srcs[1].Serial != 0 {
		t.Fatalf("断开后 sources %+v", srcs[1])
	}
	if _, disc := c.OriginState("cat-demo"); !disc {
		t.Fatal("已装应用的来源应显示已断开")
	}
	if _, err := v.w.st.GetApp(ctx, "new-api"); err != nil {
		t.Fatal("已装的应用被删了")
	}
	lv, _ = v.e.List(ctx)
	for _, x := range lv.Apps {
		if x.Source.ID == "cat-demo" {
			t.Fatal("断开后还显示它的应用")
		}
	}
	// No new installs from it, by name or by catalog.
	if _, err := v.e.StartInstall(ctx, Request{App: "new-api", Catalog: "cat-demo", Name: "n2"}); code0(err) != CodeDisconnected {
		t.Fatalf("从已断开的目录装：%v", err)
	}
	if _, err := v.e.DetailIn(ctx, "cat-demo", "new-api"); code0(err) != CodeDisconnected {
		t.Fatalf("已断开目录的详情：%v", err)
	}
	// Disconnected catalogs are not sent as known again.
	c.Sync(ctx)
	last = g.indexReq[len(g.indexReq)-1]
	if known, _ := last["known"].(map[string]any); len(known) != 0 {
		t.Fatalf("已断开的不该再报 known：%v", last)
	}

	// A new code connects it again.
	code2 := g.makeCode("cat-demo", "EEEEFFFFGGGGHHHH")
	if cv, err := c.Connect(ctx, code2); err != nil || !cv.Again || !cv.Sync.OK {
		t.Fatalf("重新连接 %+v %v", cv, err)
	}
	if srcs = c.Sources(); srcs[1].Disconnected || srcs[1].Serial != 3 {
		t.Fatalf("重新连接后 %+v", srcs[1])
	}
	_ = gc
}

// adoptPrivateImage marks the image of app in a private catalog as already
// on this machine (the image step then uses it without any download).
func adoptPrivateImage(t *testing.T, v *env, cat, app string) {
	t.Helper()
	a, err := v.e.Catalog().AppIn(context.Background(), cat, app)
	if err != nil {
		t.Fatal(err)
	}
	v.w.mu.Lock()
	v.w.adopted[a.Manifest.Release.Images[0].Digest] = true
	v.w.mu.Unlock()
}

func code0(err error) string {
	if f, ok := err.(*Fault); ok {
		return f.Code
	}
	return ""
}

func TestPrivateCatalogRejectsBadIndex(t *testing.T) {
	v, g, _, _ := privateSetup(t)
	ctx := context.Background()
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	g.publish(t, "cat-evil", "别人签的", 5, other) // not a trusted key
	cv, err := v.e.Catalog().Connect(ctx, g.makeCode("cat-evil", "AAAABBBBCCCCDDDD"))
	if err != nil {
		t.Fatal(err)
	}
	if cv.Sync.OK || cv.Sync.Code != catalog.CodeSyncSignature || len(cv.Sync.Problems) != 1 {
		t.Fatalf("签名不对的索引应被拒：%+v", cv.Sync)
	}
	if s := v.e.Catalog().Sources()[1]; s.Serial != 0 || s.Code != catalog.CodeSyncSignature {
		t.Fatalf("sources %+v", s)
	}
	if _, err := v.e.DetailIn(ctx, "cat-evil", "new-api"); code0(err) != CodeUnknown {
		t.Fatalf("没有可用索引时：%v", err)
	}
}

// The gateway hands out another catalog's validly signed index (or the
// public one) as cat-a's: refused with CAT-SYNC-12; an unlabelled one (built
// before the field existed) is still taken.
func TestPrivateCatalogRefusesOtherCatalogsIndex(t *testing.T) {
	v, g, priv, _ := privateSetup(t)
	ctx := context.Background()
	c := v.e.Catalog()
	g.publishAs(t, "cat-a", "cat-b", "甲", 5, priv)
	cv, err := c.Connect(ctx, g.makeCode("cat-a", "AAAABBBBCCCCDDDD"))
	if err != nil {
		t.Fatal(err)
	}
	if cv.Sync.OK || cv.Sync.Code != catalog.CodeSyncWrongCatalog {
		t.Fatalf("别的目录的索引：%+v", cv.Sync)
	}
	if s := c.Sources()[1]; s.Serial != 0 || s.Code != catalog.CodeSyncWrongCatalog {
		t.Fatalf("sources %+v", s)
	}
	g.publishAs(t, "cat-a", catalog.PublicCatalog, "甲", 6, priv)
	if st := c.Sync(ctx); st.Private.OK || st.Private.Code != catalog.CodeSyncWrongCatalog {
		t.Fatalf("公共目录的索引：%+v", st.Private)
	}
	g.publishAs(t, "cat-a", "", "甲", 7, priv)
	if st := c.Sync(ctx); !st.Private.OK || c.Sources()[1].Serial != 7 {
		t.Fatalf("不带目录编号的旧格式：%+v %+v", st.Private, c.Sources()[1])
	}
}

func TestConnectErrors(t *testing.T) {
	v, g, priv, _ := privateSetup(t)
	ctx := context.Background()
	c := v.e.Catalog()
	if _, err := c.Connect(ctx, "AFXC-ZZZZ-ZZZZ-ZZZZ-ZZZZ"); code0(err) != CodeConnUnknown || err.(*Fault).GatewayCode != "GW-CAT-01" {
		t.Fatalf("不存在的码：%v", err)
	}
	g.publish(t, "cat-off", "停用的", 1, priv)
	g.cats["cat-off"].status = "disabled"
	if _, err := c.Connect(ctx, g.makeCode("cat-off", "AAAABBBBCCCCDDDD")); code0(err) != CodeConnDisabled {
		t.Fatalf("停用的目录：%v", err)
	}
	g.down = true
	g.publish(t, "cat-x", "x", 1, priv)
	if _, err := c.Connect(ctx, g.makeCode("cat-x", "EEEEFFFFGGGGHHHH")); code0(err) != CodeConnGateway || !strings.Contains(err.(*Fault).Message, "GW-BE-01") {
		t.Fatalf("网关出错：%v", err)
	}
	// No gateway configured at all.
	off := NewCatalog(CatalogConfig{CacheDir: t.TempDir()})
	if _, err := off.Connect(ctx, "AFXC-AAAA-BBBB-CCCC-DDDD"); code0(err) != CodeConnNoGateway {
		t.Fatalf("关掉网关：%v", err)
	}
	if (&Fault{Code: CodeConnTooMany}).HTTPStatus() != http.StatusTooManyRequests {
		t.Fatal("CAT-CONN-06 要回 429")
	}
}

func TestPrivateSyncGatewayDownKeepsCache(t *testing.T) {
	v, g, priv, _ := privateSetup(t)
	ctx := context.Background()
	c := v.e.Catalog()
	g.publish(t, "cat-demo", "演示", 2, priv)
	if _, err := c.Connect(ctx, g.makeCode("cat-demo", "AAAABBBBCCCCDDDD")); err != nil {
		t.Fatal(err)
	}
	g.down = true
	st := c.Sync(ctx)
	if st.OK || st.Private.Code != catalog.CodeSyncGateway || st.Private.GatewayCode != "GW-BE-01" {
		t.Fatalf("网关挂了 %+v", st.Private)
	}
	if s := c.Sources()[1]; s.Serial != 2 || s.Disconnected {
		t.Fatalf("网关挂了不该丢缓存 %+v", s)
	}
}

func TestPrivatePackageDeniedHasNoFallback(t *testing.T) {
	v, g, priv, _ := privateSetup(t)
	ctx := context.Background()
	g.publish(t, "cat-demo", "演示", 2, priv)
	if _, err := v.e.Catalog().Connect(ctx, g.makeCode("cat-demo", "AAAABBBBCCCCDDDD")); err != nil {
		t.Fatal(err)
	}
	// The gateway no longer lists the package in this catalog's files.
	for f := range g.cats["cat-demo"].files {
		delete(g.cats["cat-demo"].files, f)
	}
	_, err := v.e.StartInstall(ctx, Request{App: "new-api", Catalog: "cat-demo"})
	if f, ok := err.(*Fault); !ok || f.Code != CodePrivateDenied || f.GatewayCode != "GW-CAT-05" {
		t.Fatalf("网关不给私有应用包：%v", err)
	}
	// Without --catalog the public catalog's new-api is taken first.
	tv := v.install(t, Request{App: "new-api"})
	if a, _ := v.w.st.GetApp(ctx, "new-api"); tv.Status != state.TaskSucceeded || a.CatalogOrigin != "" {
		t.Fatalf("公共目录的 new-api：%+v %+v", tv.Status, a)
	}
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// privateImageRun connects cat-demo (signed with the trusted test key), puts
// an image archive for its hermes-agent on the contract gateway as a private
// file of that catalog, and returns a run of the image step for that app with
// the archive's sha256 and size pinned in the manifest.
func privateImageRun(t *testing.T) (*env, *contractGateway, func() (string, string, error)) {
	t.Helper()
	return privateImageRunWith(t, nil)
}

// privateImageRunWith is privateImageRun with the pinned image changed by
// mod (when not nil) after the archive's checksum and size are filled in.
func privateImageRunWith(t *testing.T, mod func(*catalog.Image)) (*env, *contractGateway, func() (string, string, error)) {
	t.Helper()
	v, g, priv, _ := privateSetup(t)
	ctx := context.Background()
	g.publish(t, "cat-demo", "演示", 2, priv)
	if _, err := v.e.Catalog().Connect(ctx, g.makeCode("cat-demo", "AAAABBBBCCCCDDDD")); err != nil {
		t.Fatal(err)
	}
	const fid = "hermes-agent-v0.23.0-image"
	body := []byte("private image archive bytes")
	g.mu.Lock()
	g.blobs[fid], g.private[fid], g.cats["cat-demo"].files[fid] = body, true, true
	g.mu.Unlock()
	a, err := v.e.Catalog().AppIn(ctx, "cat-demo", "hermes-agent")
	if err != nil || a.Origin != "cat-demo" {
		t.Fatalf("私有目录的 hermes-agent：%+v %v", a, err)
	}
	m := *a.Manifest
	m.Release.Images = append(m.Release.Images[:0:0], m.Release.Images...)
	if m.Release.Images[0].Artifact != fid {
		t.Fatalf("测试目录里的镜像包编号变了：%q", m.Release.Images[0].Artifact)
	}
	m.Release.Images[0].ArtifactSHA256 = sha256hex(body)
	m.Release.Images[0].ArtifactSize = int64(len(body))
	if mod != nil {
		mod(&m.Release.Images[0])
	}
	app := *a
	app.Manifest = &m
	p := &planned{app: &app, name: "hermes-agent"}
	tr := &taskRun{e: v.e, id: "0123456789ab", view: TaskView{Apps: []AppResult{{App: "hermes-agent"}}}}
	if _, err := v.w.st.CreateInstallTask(ctx, state.InstallTask{ID: tr.id, Kind: "app", Target: "hermes-agent", Status: state.TaskRunning, Data: "{}"}); err != nil {
		t.Fatal(err)
	}
	return v, g, func() (string, string, error) { return tr.image(ctx, p, v.w) }
}

func lastIssue(g *contractGateway, fid string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := len(g.issueReq) - 1; i >= 0; i-- {
		if g.issueReq[i]["file_id"] == fid {
			return g.issueReq[i]
		}
	}
	return nil
}

// A private app's image archive is asked for through W2 with the catalog and
// the app named, checked against the signed pin and loaded.
func TestPrivateImageThroughGateway(t *testing.T) {
	v, g, run := privateImageRun(t)
	if v.e.cfg.Gateway != nil {
		t.Fatal("这个测试里不该有公共目录的网关：私有镜像只能走目录自己的网关")
	}
	id, msg, err := run()
	if err != nil || msg != "从下载网关下载" || id == "" {
		t.Fatalf("id=%s msg=%s err=%v", id, msg, err)
	}
	req := lastIssue(g, "hermes-agent-v0.23.0-image")
	app, _ := req["app"].(map[string]any)
	if req == nil || req["purpose"] != "app" || app["catalog"] != "cat-demo" || app["app"] != "hermes-agent" {
		t.Fatalf("私有镜像包的 W2 请求 %v", req)
	}
	if len(v.w.loads) != 1 || len(v.w.pulled) != 0 {
		t.Fatalf("loads=%d pulled=%v", len(v.w.loads), v.w.pulled)
	}
}

// When the gateway refuses the private image (GW-CAT-05), the step fails with
// CAT-INST-26 and never pulls from a public registry.
func TestPrivateImageDeniedHasNoFallback(t *testing.T) {
	v, g, run := privateImageRun(t)
	g.mu.Lock()
	delete(g.cats["cat-demo"].files, "hermes-agent-v0.23.0-image")
	g.mu.Unlock()
	_, _, err := run()
	f, ok := err.(*Fault)
	if !ok || f.Code != CodePrivateDenied || f.GatewayCode != "GW-CAT-05" {
		t.Fatalf("网关不给私有镜像包：%v", err)
	}
	if lastIssue(g, "hermes-agent-v0.23.0-image") == nil {
		t.Fatal("没向网关要过镜像包")
	}
	if len(v.w.pulled) != 0 || len(v.w.loads) != 0 {
		t.Fatalf("不该退回公共源：pulled=%v loads=%d", v.w.pulled, len(v.w.loads))
	}
}

// A recipe that only a disconnected private catalog had: installing it again
// without --catalog is CAT-INST-25 (not "no such recipe"), like an app.
func TestPrivateRecipeDisconnectedHint(t *testing.T) {
	v, g, priv, _ := privateSetup(t)
	ctx := context.Background()
	c := v.e.Catalog()
	// The public catalog has no apps or recipes here.
	c.cfg.Snapshot = fstest.MapFS{catalog.IndexFile: {Data: []byte(`{"schema":1,"serial":1,"apps":[],"recipes":[]}`)}}
	g.publish(t, "cat-demo", "演示", 2, priv)
	if _, err := c.Connect(ctx, g.makeCode("cat-demo", "AAAABBBBCCCCDDDD")); err != nil {
		t.Fatal(err)
	}
	adoptPrivateImage(t, v, "cat-demo", "new-api")
	tv := v.install(t, Request{Recipe: "new-api"})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("从私有目录装配方：%+v", tv)
	}
	if a, _ := v.w.st.GetApp(ctx, "new-api"); a.CatalogOrigin != "cat-demo" || a.CatalogSource != "recipe:new-api" {
		t.Fatalf("配方装的应用 %+v", a)
	}
	g.revoke("cat-demo")
	c.Sync(ctx)
	if _, err := v.e.StartInstall(ctx, Request{Recipe: "new-api"}); code0(err) != CodeDisconnected {
		t.Fatalf("已断开目录的配方：%v", err)
	}
	if _, err := v.e.StartInstall(ctx, Request{Recipe: "no-such-recipe"}); code0(err) != CodeUnknown {
		t.Fatalf("真的没有的配方：%v", err)
	}
}
