package apiserver

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/state"
)

// The plan of an install is read-only and answers 200 whatever it finds:
// what stops the install is in problems, can_install says so.
func TestCatalogPlanRoute(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	eng := appinstall.New(appinstall.Config{Store: db, Runner: refusingCatalogRunner{}, Version: "0.4.9", DataDir: dir, Catalog: goodCatalog(t, dir)})
	defer eng.Close()
	h := New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})

	w := do(t, h, "POST", "/v1/catalog/plan", []byte(`{"app":"new-api"}`), map[string]string{"Content-Type": "application/json"})
	var pv appinstall.PlanView
	if err := json.Unmarshal(w.Body.Bytes(), &pv); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || pv.CanInstall || len(pv.Problems) == 0 || pv.Problems[0].Code != appinstall.CodeTooOld || pv.Say == "" ||
		pv.Steps == nil || pv.Apps == nil {
		t.Fatalf("plan: %d %s", w.Code, w.Body.String())
	}
	w = do(t, h, "POST", "/v1/catalog/plan", []byte(`{nope`), map[string]string{"Content-Type": "application/json"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad body: %d %s", w.Code, w.Body.String())
	}
	// Without the app center the route says so, like the others.
	h2, _, _, _, _ := newTestServer(t)
	w = do(t, h2, "POST", "/v1/catalog/plan", []byte(`{"app":"new-api"}`), nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusNotFound, "app_center_unavailable")
}
