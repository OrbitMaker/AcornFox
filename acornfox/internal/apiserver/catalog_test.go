package apiserver

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// goodCatalog is an app center catalog whose snapshot is the test material
// of the current manifest format (catalog/testdata/good: two apps and their
// recipes); the catalog built into the program has no apps yet. Sync goes
// nowhere.
func goodCatalog(t *testing.T, dir string) *appinstall.Catalog {
	t.Helper()
	return goodCatalogWith(t, dir, nil)
}

// goodCatalogWith is goodCatalog with trusted keys (nil: the program's own).
func goodCatalogWith(t *testing.T, dir string, keys []ed25519.PublicKey) *appinstall.Catalog {
	t.Helper()
	b, err := catalog.BuildFor(filepath.Join("..", "catalog", "testdata", "good"), 5, catalog.PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	snap := fstest.MapFS{catalog.IndexFile: {Data: b.Index}, catalog.VersionFile: {Data: []byte("5\n")}}
	for p, data := range b.Packages {
		snap[p] = &fstest.MapFile{Data: data}
	}
	return appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog"), Snapshot: snap, Keys: keys, Sources: []string{"http://127.0.0.1:1"}})
}

func TestCatalogRoutesWithoutAppCenter(t *testing.T) {
	h, _, _, _, _ := newTestServer(t)
	for _, p := range []string{"/v1/catalog", "/v1/catalog/apps/new-api", "/v1/catalog/install/0123456789ab", "/v1/apps/web/first-login", "/v1/apps/web/vars"} {
		w := do(t, h, "GET", p, nil, nil)
		wantCode(t, w.Code, w.Body.String(), http.StatusNotFound, "app_center_unavailable")
	}
}

func TestReserveCatalogApp(t *testing.T) {
	st := newFakeStore()
	s := newServer(Config{Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(), HostMetrics: fakeHostSized(8192, 4)})
	ctx := context.Background()
	st.EnsureApp(ctx, "web") // 256 MB of the 6964 MB cap

	_, err := s.reserveCatalogApp(ctx, "hermes-agent", 6800)
	var room *appinstall.RoomError
	if !errors.As(err, &room) || room.CapMB != 6964 || room.AllocatedMB != 256 || room.NeedMB != 6800 {
		t.Fatalf("room: %v %+v", err, room)
	}
	if _, err := st.GetApp(ctx, "hermes-agent"); !errors.Is(err, state.ErrNotFound) {
		t.Fatal("app created although it did not fit")
	}
	a, err := s.reserveCatalogApp(ctx, "hermes-agent", 2048)
	if err != nil || a.MemoryMB != 2048 {
		t.Fatalf("reserve: %+v %v", a, err)
	}
	if _, err := s.reserveCatalogApp(ctx, "hermes-agent", 256); !errors.Is(err, state.ErrConflict) {
		t.Fatalf("existing: %v", err)
	}
}

func TestDeleteRefusesLinkedGateway(t *testing.T) {
	h, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "new-api")
	st.EnsureApp(ctx, "hermes-agent")
	st.UpdateApp(ctx, "hermes-agent", func(a *state.App) error { a.Links = []string{"new-api"}; return nil })
	w := do(t, h, "DELETE", "/v1/apps/new-api", nil, nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusConflict, "app_in_use")
	if _, err := st.GetApp(ctx, "new-api"); err != nil {
		t.Fatal("app removed")
	}
	if w := do(t, h, "DELETE", "/v1/apps/hermes-agent", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("delete consumer: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, h, "DELETE", "/v1/apps/new-api", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("delete gateway after: %d %s", w.Code, w.Body.String())
	}
}

func TestPrivateCatalogRoutes(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	eng := appinstall.New(appinstall.Config{Store: db, Catalog: goodCatalog(t, dir), DataDir: dir})
	defer eng.Close()
	st := newFakeStore()
	h := New(Config{Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})

	// No download gateway on this server: connect is CAT-CONN-02 (409).
	w := do(t, h, "POST", "/v1/catalog/connect", []byte(`{"code":"AFXC-AAAA-BBBB-CCCC-DDDD"}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"CAT-CONN-02"`) || strings.Contains(w.Body.String(), "AAAA-BBBB") {
		t.Fatalf("connect: %d %s", w.Code, w.Body.String())
	}
	w = do(t, h, "POST", "/v1/catalog/connect", []byte(`{}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CAT-CONN-01") {
		t.Fatalf("empty code: %d %s", w.Code, w.Body.String())
	}
	w = do(t, h, "GET", "/v1/catalog/sources", nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"public"`) {
		t.Fatalf("sources: %d %s", w.Code, w.Body.String())
	}
	w = do(t, h, "GET", "/v1/catalog/platform-vars", nil, nil)
	// nothing about a model: a model API is a service, not a platform variable
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"mirror.pypi"`) || strings.Contains(w.Body.String(), `"name":"llm.`) ||
		strings.Contains(w.Body.String(), "via_param") || strings.Contains(w.Body.String(), `"issued"`) {
		t.Fatalf("platform-vars: %d %s", w.Code, w.Body.String())
	}
	// every variable says from which platform version on it exists
	if n := strings.Count(w.Body.String(), `"since":"`+catalog.BasePlatform+`"`); n != strings.Count(w.Body.String(), `"name":`) || n == 0 {
		t.Fatalf("platform-vars: %d of the variables carry since: %s", n, w.Body.String())
	}
	// a bad platform change is refused before a task starts (CAT-INST-28, 400)
	w = do(t, h, "POST", "/v1/catalog/install", []byte(`{"app":"new-api","platform":{"region":"global"}}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CAT-INST-28") {
		t.Fatalf("install with bad platform: %d %s", w.Code, w.Body.String())
	}
	// An app installed from a private catalog this machine no longer has
	// shows as disconnected; a public one does not.
	ctx := context.Background()
	st.EnsureApp(ctx, "p1")
	st.UpdateApp(ctx, "p1", func(a *state.App) error { a.CatalogID, a.CatalogOrigin = "new-api", "cat-gone"; return nil })
	st.EnsureApp(ctx, "p2")
	st.UpdateApp(ctx, "p2", func(a *state.App) error { a.CatalogID = "new-api"; return nil })
	w = do(t, h, "GET", "/v1/apps/p1", nil, nil)
	if !strings.Contains(w.Body.String(), `"catalog_origin":"cat-gone"`) || !strings.Contains(w.Body.String(), `"catalog_disconnected":true`) {
		t.Fatalf("p1: %s", w.Body.String())
	}
	w = do(t, h, "GET", "/v1/apps/p2", nil, nil)
	if !strings.Contains(w.Body.String(), `"catalog_origin":"public"`) || strings.Contains(w.Body.String(), "catalog_disconnected") {
		t.Fatalf("p2: %s", w.Body.String())
	}
	// Detail from a catalog that is not connected.
	w = do(t, h, "GET", "/v1/catalog/apps/new-api?catalog=cat-nope", nil, nil)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "CAT-INST-01") {
		t.Fatalf("detail: %d %s", w.Code, w.Body.String())
	}
}

// "Upgrade first" is a field, not a phrase to look for: the catalog list, the
// app detail and the refusal of an install all carry needs_platform. The
// variables of an installed app are served like first-login (never cached);
// an app that did not come from the catalog gets a plain answer, not an error.
func TestNeedsPlatformAndAppVarsRoutes(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Version 0.4.9: older than what the catalog's apps ask for.
	eng := appinstall.New(appinstall.Config{Store: db, Runner: refusingCatalogRunner{}, Version: "0.4.9", DataDir: dir, Catalog: goodCatalog(t, dir)})
	defer eng.Close()
	h := New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})

	w := do(t, h, "GET", "/v1/catalog", nil, nil)
	if w.Code != http.StatusOK || strings.Count(w.Body.String(), `"needs_platform":"0.6.0"`) < 2 || strings.Contains(w.Body.String(), `"needs_platform":""`) ||
		strings.Contains(w.Body.String(), `"former"`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	w = do(t, h, "GET", "/v1/catalog/apps/new-api", nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"needs_platform":"0.6.0"`) {
		t.Fatalf("detail: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"app":"new-api"}`, `{"recipe":"hermes-with-new-api"}`} {
		w = do(t, h, "POST", "/v1/catalog/install", []byte(body), map[string]string{"Content-Type": "application/json"})
		var e catalogErrorBody
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusConflict || e.Error.Code != appinstall.CodeTooOld || e.Error.NeedsPlatform != "0.6.0" || e.Error.Hint == "" {
			t.Fatalf("install %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	// A fault without it does not carry the key at all.
	w = do(t, h, "POST", "/v1/catalog/install", []byte(`{"app":"nope"}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "needs_platform") {
		t.Fatalf("unknown app: %d %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	if _, _, err := db.EnsureApp(ctx, "plain"); err != nil {
		t.Fatal(err)
	}
	w = do(t, h, "GET", "/v1/apps/plain/vars", nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"from_catalog":false`) || !strings.Contains(w.Body.String(), `"vars":[]`) ||
		!strings.Contains(w.Body.String(), "不是从应用中心装的") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("vars of a plain app: %d %s %v", w.Code, w.Body.String(), w.Header())
	}
	w = do(t, h, "GET", "/v1/apps/nope/vars?reveal=true", nil, nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusNotFound, appinstall.CodeNotCatalogApp)
}

// refusingCatalogRunner is a runner for installs that must be refused before
// they reach Docker: every call fails.
type refusingCatalogRunner struct{}

var errRunnerReached = errors.New("the runner was reached")

func (refusingCatalogRunner) PullImage(context.Context, string, string, string) (runner.PullResponse, error) {
	return runner.PullResponse{}, errRunnerReached
}
func (refusingCatalogRunner) EnsureVolume(context.Context, string, string) error {
	return errRunnerReached
}
func (refusingCatalogRunner) ListContainers(context.Context, string) ([]runner.ContainerInfo, error) {
	return nil, errRunnerReached
}
func (refusingCatalogRunner) RemoveImage(context.Context, string, string) error {
	return errRunnerReached
}
func (refusingCatalogRunner) ListVolumes(context.Context, string) ([]runner.VolumeInfo, error) {
	return nil, errRunnerReached
}
func (refusingCatalogRunner) RemoveVolume(context.Context, string, string) error {
	return errRunnerReached
}
func (refusingCatalogRunner) AdoptImage(context.Context, runner.AdoptImageRequest) (runner.ImageInfo, error) {
	return runner.ImageInfo{}, errRunnerReached
}
func (refusingCatalogRunner) LoadImage(context.Context, runner.LoadImageRequest) (runner.LoadImageResponse, error) {
	return runner.LoadImageResponse{}, errRunnerReached
}
func (refusingCatalogRunner) WriteVolumeFiles(context.Context, runner.WriteFilesRequest) (runner.WriteFilesResult, error) {
	return runner.WriteFilesResult{}, errRunnerReached
}

// GET /v1/catalog carries the display content: per app and recipe "show",
// and the whole catalog's industries, home and assist (empty without one).
func TestCatalogListShowFields(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, err := catalog.BuildFor(filepath.Join("..", "catalog", "testdata", "good"), 5, catalog.PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	snap := fstest.MapFS{catalog.IndexFile: {Data: b.Index}, catalog.VersionFile: {Data: []byte("5\n")},
		catalog.ShowFile: {Data: []byte(`{"kind":"catalog-show","schema":1,"serial":5,
"industries":[{"id":"office","name":"通用办公"}],
"apps":[{"id":"hermes-agent","name":"Hermes 智能体","tagline":"会聊天的助手","flag":"hot","color":"#3b5bdb","cover":"https://acornfox.com/catalog/assets/a.png","doc":"# Hermes","order":3,"keywords":["助手"]}],
"solutions":[{"id":"hermes-with-new-api","name":"一套助手","industries":["office"],"order":1}],
"home":{"solutions":["hermes-with-new-api"]},"assist":{"visible":true,"price_cny":99}}`)}}
	for p, data := range b.Packages {
		snap[p] = &fstest.MapFile{Data: data}
	}
	cat := appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog"), Snapshot: snap, Sources: []string{"http://127.0.0.1:1"}})
	eng := appinstall.New(appinstall.Config{Store: db, Catalog: cat, DataDir: dir})
	defer eng.Close()
	h := New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})

	w := do(t, h, "GET", "/v1/catalog", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var v struct {
		Industries []struct{ ID, Name string }  `json:"industries"`
		Home       struct{ Solutions []string } `json:"home"`
		Assist     struct {
			Visible  bool    `json:"visible"`
			PriceCNY float64 `json:"price_cny"`
		} `json:"assist"`
		Sync struct {
			Show struct {
				State  string `json:"state"`
				Serial int64  `json:"serial"`
			} `json:"show"`
		} `json:"sync"`
		Apps []struct {
			ID   string `json:"id"`
			Show *struct {
				Name, Tagline, Flag, Cover, Doc string
				Order                           int
				Keywords                        []string
			} `json:"show"`
		} `json:"apps"`
		Recipes []struct {
			ID   string `json:"id"`
			Show *struct {
				Name       string
				Industries []string
			} `json:"show"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Industries) != 1 || v.Industries[0].ID != "office" || len(v.Home.Solutions) != 1 || !v.Assist.Visible || v.Assist.PriceCNY != 99 ||
		v.Sync.Show.State != "ok" || v.Sync.Show.Serial != 5 {
		t.Fatalf("top level: %s", w.Body.String())
	}
	for _, a := range v.Apps {
		switch a.ID {
		case "hermes-agent":
			if a.Show == nil || a.Show.Tagline != "会聊天的助手" || a.Show.Flag != "hot" || a.Show.Doc != "# Hermes" || a.Show.Order != 3 || a.Show.Cover != "https://acornfox.com/catalog/assets/a.png" {
				t.Errorf("hermes show %+v", a.Show)
			}
		case "new-api":
			if a.Show != nil {
				t.Errorf("new-api show %+v", a.Show)
			}
		}
	}
	found := false
	for _, r := range v.Recipes {
		if r.ID == "hermes-with-new-api" {
			found = r.Show != nil && r.Show.Name == "一套助手" && len(r.Show.Industries) == 1
		}
	}
	if !found {
		t.Fatalf("recipe show: %s", w.Body.String())
	}
}

// Sync on demand: ?if_stale=1 syncs only when the last good sync is older than
// the limit and the last attempt older than the retry gap; opening "my apps"
// or the app center starts one in the background.
func TestCatalogSyncIfStaleAndPageTouch(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	cat := goodCatalogWith(t, dir, []ed25519.PublicKey{pub})
	eng := appinstall.New(appinstall.Config{Store: db, Catalog: cat, DataDir: dir})
	defer eng.Close()
	h := New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})

	post := func() (ran bool, ok bool) {
		w := do(t, h, "POST", "/v1/catalog/sync?if_stale=1", []byte(`{}`), map[string]string{"Content-Type": "application/json"})
		if w.Code != http.StatusOK {
			t.Fatalf("sync: %d %s", w.Code, w.Body.String())
		}
		var v struct {
			Ran bool `json:"ran"`
			OK  bool `json:"ok"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.Ran, v.OK
	}
	// Never synced: it syncs (the only source is unreachable, so it fails).
	if ran, ok := post(); !ran || ok {
		t.Fatalf("第一次要同步且这里连不上：ran=%v ok=%v", ran, ok)
	}
	// The attempt just made holds the next ones back (the retry gap).
	if ran, _ := post(); ran {
		t.Fatal("刚试过，5 分钟内不该再试")
	}
	// Without if_stale it always syncs.
	w := do(t, h, "POST", "/v1/catalog/sync", []byte(`{}`), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"ran"`) {
		t.Fatalf("不带 if_stale 照旧同步：%d %s", w.Code, w.Body.String())
	}
	// The pages answer at once and do not start a sync inside the retry gap.
	before := cat.Status().At
	for _, p := range []string{"/v1/apps", "/v1/catalog"} {
		if w := do(t, h, "GET", p, nil, nil); w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", p, w.Code, w.Body.String())
		}
	}
	if after := cat.Status().At; (before == nil) != (after == nil) || (before != nil && !before.Equal(*after)) {
		t.Fatal("5 分钟内页面不该触发同步")
	}
}

// GET /v1/catalog/skills/{kind}/{id}: the operating manual of an app or a
// solution; the list only says there is one.
func TestCatalogSkillEndpoint(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	b, err := catalog.BuildFor(filepath.Join("..", "catalog", "testdata", "good"), 5, catalog.PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	const body = "# Hermes\n\n## 装之前\n先问用户有没有大模型的 Key。\n"
	sum := sha256.Sum256([]byte(body))
	show := `{"kind":"catalog-show","schema":1,"serial":5,"industries":[],
"apps":[{"id":"hermes-agent","skill":{"path":"skills/app-hermes-agent.md","sha256":"` + hex.EncodeToString(sum[:]) + `","size":` + strconv.Itoa(len(body)) + `,"version":"1.0","checked":"2026-11-24"}}],
"solutions":[],"home":{"solutions":[]},"assist":{"visible":false,"price_cny":0}}`
	snap := fstest.MapFS{catalog.IndexFile: {Data: b.Index}, catalog.VersionFile: {Data: []byte("5\n")},
		catalog.ShowFile:             {Data: []byte(show)},
		"skills/app-hermes-agent.md": {Data: []byte(body)}}
	for p, data := range b.Packages {
		snap[p] = &fstest.MapFile{Data: data}
	}
	cat := appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog"), Snapshot: snap, Sources: []string{"http://127.0.0.1:1"}})
	eng := appinstall.New(appinstall.Config{Store: db, Catalog: cat, DataDir: dir})
	defer eng.Close()
	h := New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})

	w := do(t, h, "GET", "/v1/catalog/skills/app/hermes-agent", nil, nil)
	var v struct {
		ID, Kind, Version, Checked, Text string
		HasSkill                         bool `json:"has_skill"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &v) != nil || !v.HasSkill || v.Text != body || v.Kind != "app" || v.Version != "1.0" || v.Checked != "2026-11-24" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	// An app without a manual: 200 and has_skill false.
	w = do(t, h, "GET", "/v1/catalog/skills/app/new-api", nil, nil)
	v.HasSkill = true
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.HasSkill || v.Text != "" || !strings.Contains(w.Body.String(), "note") {
		t.Fatalf("no manual: %d %s", w.Code, w.Body.String())
	}
	// Unknown id 404 (CAT-INST-01), bad kind 400 (CAT-INST-02).
	if w = do(t, h, "GET", "/v1/catalog/skills/app/nothing-here", nil, nil); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "CAT-INST-01") {
		t.Fatalf("unknown: %d %s", w.Code, w.Body.String())
	}
	if w = do(t, h, "GET", "/v1/catalog/skills/apps/hermes-agent", nil, nil); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "CAT-INST-02") {
		t.Fatalf("bad kind: %d %s", w.Code, w.Body.String())
	}

	// The list says there is one, never gives the text.
	w = do(t, h, "GET", "/v1/catalog", nil, nil)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "先问用户有没有大模型") || !strings.Contains(w.Body.String(), `"skill":{"version":"1.0","checked":"2026-11-24"`) ||
		!strings.Contains(w.Body.String(), `"skills":{"state":"ok","count":1}`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
}
