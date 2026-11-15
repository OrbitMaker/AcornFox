package apiserver

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// GET /v1/apps/{app} of an app installed with a manifest of the former
// format carries legacy_note (one sentence for the user, which the console
// shows as it is); an app of the current format has no such field.
func TestAppDetailLegacyNote(t *testing.T) {
	b, err := catalog.BuildFor(filepath.Join("..", "catalog", "testdata", "services"), 5, catalog.PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	snap := fstest.MapFS{catalog.IndexFile: {Data: b.Index}, catalog.VersionFile: {Data: []byte("5\n")}}
	for p, data := range b.Packages {
		snap[p] = &fstest.MapFile{Data: data}
	}
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	eng := appinstall.New(appinstall.Config{Store: db, Runner: refusingCatalogRunner{}, DataDir: dir,
		Catalog: appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog"), Snapshot: snap, Sources: []string{"http://127.0.0.1:1"}})})
	t.Cleanup(eng.Close)
	h := New(Config{Store: db, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})

	ctx := context.Background()
	manifests := map[string]string{
		"old-app": "schema: 2\nid: old-app\n",
		"new-app": "schema: 3\nid: new-app\n",
	}
	for app, text := range manifests {
		if _, _, err := db.EnsureApp(ctx, app); err != nil {
			t.Fatal(err)
		}
		if _, err := db.UpdateApp(ctx, app, func(a *state.App) error { a.CatalogID, a.CatalogVersion = app, "v1"; return nil }); err != nil {
			t.Fatal(err)
		}
		if err := db.SetCatalogManifest(ctx, state.CatalogManifest{App: app, CatalogID: app, CatalogVersion: "v1",
			Manifest: text, SHA256: state.ManifestSHA256(text), Source: state.ManifestFromInstall}); err != nil {
			t.Fatal(err)
		}
	}
	w := do(t, h, http.MethodGet, "/v1/apps/old-app", nil, nil)
	if m := decodeObj(t, w.Body.String()); w.Code != http.StatusOK || m["legacy_note"] != appinstall.LegacyAppNote {
		t.Fatalf("old-app %d: legacy_note %v", w.Code, m["legacy_note"])
	}
	w = do(t, h, http.MethodGet, "/v1/apps/new-app", nil, nil)
	if m := decodeObj(t, w.Body.String()); w.Code != http.StatusOK {
		t.Fatalf("new-app %d", w.Code)
	} else if _, has := m["legacy_note"]; has {
		t.Fatalf("new-app has legacy_note %v", m["legacy_note"])
	}
	// The list stays as it was: no note there.
	w = do(t, h, http.MethodGet, "/v1/apps", nil, nil)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "legacy_note") {
		t.Fatalf("list %d %s", w.Code, w.Body.String())
	}
}
