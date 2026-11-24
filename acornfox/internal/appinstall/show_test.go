package appinstall

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/acornfox/acornfox/internal/catalog"
)

// The display content (catalog show.json) in the app center's views.

func showFor(serial int64) catalog.Show {
	return catalog.Show{
		Kind: catalog.ShowKind, Schema: catalog.ShowSchema, Serial: serial,
		Industries: []catalog.ShowIndustry{{ID: "office", Name: "通用办公"}},
		Apps: []catalog.ShowApp{{ID: "hermes-agent", Name: "Hermes 智能体", Tagline: "会聊天的助手", Flag: "new", Color: "#3b5bdb",
			Cover: catalog.CoverPrefix + "c1.png", Doc: "# Hermes", Order: 2, Keywords: []string{"智能体", "助手"}}},
		Solutions: []catalog.ShowSolution{{ID: "hermes-with-new-api", Name: "一套助手", Tagline: "接好的两个应用", Industries: []string{"office"}, Doc: "说明", Order: 1}},
		Home:      catalog.ShowHome{Solutions: []string{"hermes-with-new-api"}},
		Assist:    catalog.ShowAssist{Visible: true, PriceCNY: 199},
	}
}

func TestListCarriesShowFromSnapshot(t *testing.T) {
	b := goodBundle(t)
	snap := bundleFS(b).(fstest.MapFS)
	raw, _ := json.Marshal(showFor(b.Serial))
	snap[catalog.ShowFile] = &fstest.MapFile{Data: raw}
	v, _ := setupSnapshot(t, snap, nil)
	ctx := context.Background()

	l, err := v.e.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var hermes *CatalogAppView
	for i := range l.Apps {
		if l.Apps[i].ID == "hermes-agent" {
			hermes = &l.Apps[i]
		}
		if l.Apps[i].ID == "new-api" && l.Apps[i].Show != nil {
			t.Errorf("没写展示内容的应用不该有 show：%+v", l.Apps[i].Show)
		}
	}
	if hermes == nil || hermes.Show == nil || hermes.Show.Tagline != "会聊天的助手" || hermes.Show.Flag != "new" || hermes.Show.Cover != catalog.CoverPrefix+"c1.png" || hermes.Show.Doc != "# Hermes" {
		t.Fatalf("hermes %+v", hermes)
	}
	// Search words: the manifest's first, then the display content's, once each.
	seen := map[string]int{}
	for _, k := range hermes.Keywords {
		seen[k]++
	}
	if seen["智能体"] != 1 || seen["助手"] != 1 {
		t.Fatalf("keywords %v", hermes.Keywords)
	}
	var recipe *RecipeView
	for i := range l.Recipes {
		if l.Recipes[i].ID == "hermes-with-new-api" {
			recipe = &l.Recipes[i]
		}
		if l.Recipes[i].ID == "new-api" && l.Recipes[i].Show != nil {
			t.Error("没写展示内容的解决方案不该有 show")
		}
	}
	if recipe == nil || recipe.Show == nil || recipe.Show.Name != "一套助手" || len(recipe.Show.Industries) != 1 {
		t.Fatalf("recipe %+v", recipe)
	}
	if len(l.Industries) != 1 || l.Industries[0].ID != "office" || len(l.Home.Solutions) != 1 || !l.Assist.Visible || l.Assist.PriceCNY != 199 {
		t.Fatalf("top level %+v %+v %+v", l.Industries, l.Home, l.Assist)
	}
	if l.Sync.Show == nil || l.Sync.Show.State != catalog.ShowOK || l.Sync.Show.Serial != b.Serial {
		t.Fatalf("sync.show %+v", l.Sync.Show)
	}
	d, err := v.e.Detail(ctx, "hermes-agent")
	if err != nil || d.Show == nil || d.Show.Name != "Hermes 智能体" {
		t.Fatalf("detail %+v %v", d.Show, err)
	}
	if d, err := v.e.Detail(ctx, "new-api"); err != nil || d.Show != nil {
		t.Fatalf("detail new-api %+v %v", d.Show, err)
	}
}

func TestListWithoutShow(t *testing.T) {
	v := setup(t)
	l, err := v.e.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(l)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if inds, ok := m["industries"].([]any); !ok || len(inds) != 0 {
		t.Fatalf("industries 要是空列表：%s", raw)
	}
	home, _ := m["home"].(map[string]any)
	if sols, ok := home["solutions"].([]any); !ok || len(sols) != 0 {
		t.Fatalf("home.solutions 要是空列表：%v", m["home"])
	}
	if a, _ := m["assist"].(map[string]any); a["visible"] != false || a["price_cny"] != float64(0) {
		t.Fatalf("assist %v", m["assist"])
	}
	for _, a := range l.Apps {
		if a.Show != nil {
			t.Errorf("%s 不该有 show", a.ID)
		}
	}
	if l.Sync.Show == nil || l.Sync.Show.State != catalog.ShowNone {
		t.Fatalf("sync.show %+v", l.Sync.Show)
	}
	if strings.Contains(string(raw), `"show":null`) {
		t.Fatal("没有展示内容时不带 show 字段")
	}
}

// showSource serves a signed catalog directory the way the official site
// does, with show.json and show.json.sig when given.
func showSource(t *testing.T, serial int64, priv ed25519.PrivateKey, showSerial int64, showKey ed25519.PrivateKey) *httptest.Server {
	t.Helper()
	b, err := catalog.BuildFor(goodDir, serial, catalog.PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	b.Sign(priv)
	out := t.TempDir()
	if _, err := b.Write(out); err != nil {
		t.Fatal(err)
	}
	if showKey != nil {
		raw, _ := json.Marshal(showFor(showSerial))
		_ = os.WriteFile(filepath.Join(out, catalog.ShowFile), raw, 0o644)
		_ = os.WriteFile(filepath.Join(out, catalog.ShowSigFile), catalog.SignShow(raw, showKey), 0o644)
	}
	srv := httptest.NewServer(http.StripPrefix("/catalog", http.FileServer(http.Dir(out))))
	t.Cleanup(srv.Close)
	return srv
}

func TestSyncTakesShowAndReportsFailure(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_, other, _ := ed25519.GenerateKey(rand.Reader)
	ctx := context.Background()
	newCatalog := func(srv *httptest.Server) *Catalog {
		return NewCatalog(CatalogConfig{CacheDir: t.TempDir(), Snapshot: fstest.MapFS{}, Keys: []ed25519.PublicKey{pub}, Sources: []string{srv.URL + "/catalog"}})
	}

	// Good: the show is in the status and in use.
	c := newCatalog(showSource(t, 7, priv, 7, priv))
	st := c.Sync(ctx)
	if !st.OK || !st.Updated || st.Show == nil || st.Show.State != catalog.ShowOK {
		t.Fatalf("sync %+v show %+v", st, st.Show)
	}
	if s, ss := c.Show(); s == nil || ss.State != catalog.ShowOK || s.App("hermes-agent") == nil {
		t.Fatalf("show %v %+v", s, ss)
	}

	// Signed by another key: the catalog is taken, the show is not, and the
	// failure is what Show() reports (doctor shows it).
	c = newCatalog(showSource(t, 7, priv, 7, other))
	st = c.Sync(ctx)
	if !st.OK || !st.Updated || st.Show == nil || st.Show.Code != catalog.CodeShowSignature {
		t.Fatalf("bad sig: %+v show %+v", st, st.Show)
	}
	if s, ss := c.Show(); s != nil || ss.State != catalog.ShowFailed || ss.Code != catalog.CodeShowSignature {
		t.Fatalf("bad sig show %v %+v", s, ss)
	}
	if idx, _, err := c.Index(); err != nil || idx.Serial != 7 {
		t.Fatalf("目录要照常用：%v", err)
	}

	// Serial of another publish.
	c = newCatalog(showSource(t, 7, priv, 6, priv))
	st = c.Sync(ctx)
	if st.Show == nil || st.Show.Code != catalog.CodeShowSerial {
		t.Fatalf("serial: %+v", st.Show)
	}

	// No show published: none, not a failure; the status survives a restart.
	c = newCatalog(showSource(t, 7, priv, 0, nil))
	st = c.Sync(ctx)
	if !st.OK || st.Show == nil || st.Show.State != catalog.ShowNone {
		t.Fatalf("absent: %+v", st.Show)
	}
	again := NewCatalog(CatalogConfig{CacheDir: c.cfg.CacheDir, Snapshot: fstest.MapFS{}, Keys: []ed25519.PublicKey{pub}})
	if s := again.Status(); s.Show == nil || s.Show.State != catalog.ShowNone {
		t.Fatalf("saved status %+v", s.Show)
	}
}
