package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

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
	eng := appinstall.New(appinstall.Config{Store: db, Catalog: appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog")}), DataDir: dir})
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
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"mirror.pypi"`) || !strings.Contains(w.Body.String(), `"name":"llm.api_key"`) {
		t.Fatalf("platform-vars: %d %s", w.Code, w.Body.String())
	}
	// every variable says from which platform version on it exists
	if n := strings.Count(w.Body.String(), `"since":"0.5.0"`); n != strings.Count(w.Body.String(), `"name":`) || n == 0 {
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
	// Version 0.4.9: older than what the built-in catalog's apps ask for.
	eng := appinstall.New(appinstall.Config{Store: db, Runner: refusingCatalogRunner{}, Version: "0.4.9", DataDir: dir,
		Catalog: appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog"), Sources: []string{"http://127.0.0.1:1"}})})
	defer eng.Close()
	h := New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})

	w := do(t, h, "GET", "/v1/catalog", nil, nil)
	if w.Code != http.StatusOK || strings.Count(w.Body.String(), `"needs_platform":"0.5.0"`) < 2 || strings.Contains(w.Body.String(), `"needs_platform":""`) {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	w = do(t, h, "GET", "/v1/catalog/apps/new-api", nil, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"needs_platform":"0.5.0"`) {
		t.Fatalf("detail: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"app":"new-api"}`, `{"recipe":"hermes-with-new-api"}`} {
		w = do(t, h, "POST", "/v1/catalog/install", []byte(body), map[string]string{"Content-Type": "application/json"})
		var e catalogErrorBody
		if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusConflict || e.Error.Code != appinstall.CodeTooOld || e.Error.NeedsPlatform != "0.5.0" || e.Error.Hint == "" {
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
