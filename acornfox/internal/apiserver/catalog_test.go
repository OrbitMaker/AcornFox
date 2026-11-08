package apiserver

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/state"
)

func TestCatalogRoutesWithoutAppCenter(t *testing.T) {
	h, _, _, _, _ := newTestServer(t)
	for _, p := range []string{"/v1/catalog", "/v1/catalog/apps/new-api", "/v1/catalog/install/0123456789ab", "/v1/apps/web/first-login"} {
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
