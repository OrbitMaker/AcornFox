package apiserver

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

// The routes of internal/appinstall/upgrade.go: what they refuse, what an
// app's view and the status say, and that the rollback of an app to its
// previous deployment answers as before. The upgrade itself (the snapshot,
// the new version, going back) is in appinstall's tests, with the engine and
// its fake world; here is only the HTTP end.

func TestUpgradeRoutesRefuse(t *testing.T) {
	h, db := reapplyServer(t)
	ctx := context.Background()
	if _, _, err := db.EnsureApp(ctx, "mine"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"upgrade: no such app", "POST", "/v1/apps/nothing/upgrade", `{"plan":true}`, 404, "CAT-INST-22"},
		{"upgrade: not json", "POST", "/v1/apps/mine/upgrade", `{`, 400, "CAT-INST-02"},
		{"upgrade: not from the app center", "POST", "/v1/apps/mine/upgrade", `{"plan":true}`, 409, "APP-UPG-01"},
		{"upgrade: not from the app center, for real", "POST", "/v1/apps/mine/upgrade", `{"confirm_restart":true}`, 409, "APP-UPG-01"},
		{"rollback: no such app", "POST", "/v1/apps/nothing/upgrade/rollback", `{"plan":true}`, 404, "CAT-INST-22"},
		{"rollback: not from the app center", "POST", "/v1/apps/mine/upgrade/rollback", `{"plan":true}`, 409, "APP-UPG-07"},
		{"the old route keeps refusing fields it does not know (own project)", "POST", "/v1/apps/mine/rollback", `{"plan":true}`, 400, "invalid_version"},
		{"the old route keeps refusing fields it does not know (own project, confirm)", "POST", "/v1/apps/mine/rollback", `{"confirm":true}`, 400, "invalid_version"},
	} {
		w := do(t, h, c.method, c.path, []byte(c.body), jsonHeader)
		if w.Code != c.status || errCodeOf(w.Body.String()) != c.code {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body.String())
		}
	}
	// an app from the app center answers the upgrade rollback on the old route
	if _, _, err := db.EnsureApp(ctx, "fromcat"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UpdateApp(ctx, "fromcat", func(a *state.App) error { a.CatalogID, a.CatalogVersion = "demo-gone", "1.0.0"; return nil }); err != nil {
		t.Fatal(err)
	}
	if w := do(t, h, "POST", "/v1/apps/fromcat/rollback", []byte(`{"plan":true}`), jsonHeader); w.Code != 200 || !strings.Contains(w.Body.String(), `"blocked_code":"BKP-11"`) {
		t.Errorf("catalog app, old route with plan: %d %s", w.Code, w.Body.String())
	}
	// the rollback to the previous deployment is still that one: an app that
	// was never deployed has none, whatever the body (empty or {})
	for _, body := range []string{``, `{}`, `{"cancel_pending":true}`} {
		w := do(t, h, "POST", "/v1/apps/mine/rollback", []byte(body), jsonHeader)
		if w.Code == http.StatusAccepted || strings.Contains(w.Body.String(), "APP-UPG") {
			t.Errorf("rollback to the previous deployment with %q: %d %s", body, w.Code, w.Body.String())
		}
	}
}

func TestAppViewsCarryTheUpgrade(t *testing.T) {
	h, db := reapplyServer(t)
	ctx := context.Background()
	for _, n := range []string{"mine", "fromcat"} {
		if _, _, err := db.EnsureApp(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.UpdateApp(ctx, "fromcat", func(a *state.App) error {
		a.CatalogID, a.CatalogVersion = "demo-gone", "1.0.0"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// the list: the app from the app center has the upgrade object (nothing
	// newer), the app of the user's own has none
	w := do(t, h, "GET", "/v1/apps", nil, nil)
	m := decodeObj(t, w.Body.String())
	byName := map[string]map[string]any{}
	for _, x := range m["apps"].([]any) {
		a := x.(map[string]any)
		byName[a["name"].(string)] = a
	}
	if _, has := byName["mine"]["upgrade"]; has {
		t.Fatalf("own app has upgrade: %v", byName["mine"])
	}
	up, ok := byName["fromcat"]["upgrade"].(map[string]any)
	if !ok || up["available"] != "" || up["can_upgrade"] != false || up["can_rollback"] != false || up["snapshot"] != nil {
		t.Fatalf("upgrade %v", byName["fromcat"]["upgrade"])
	}
	if _, has := byName["fromcat"]["catalog_update"]; has {
		t.Fatalf("catalog_update with nothing newer: %v", byName["fromcat"])
	}
	// one app
	w = do(t, h, "GET", "/v1/apps/fromcat", nil, nil)
	if one := decodeObj(t, w.Body.String()); one["upgrade"] == nil {
		t.Fatalf("detail %s", w.Body.String())
	}
	// the status says how many apps are behind
	w = do(t, h, "GET", "/v1/status", nil, nil)
	if st := decodeObj(t, w.Body.String()); st["apps_outdated"] != float64(0) {
		t.Fatalf("status %s", w.Body.String())
	}
}

func TestAppEventsRoute(t *testing.T) {
	h, db := reapplyServer(t)
	ctx := context.Background()
	if _, _, err := db.EnsureApp(ctx, "mine"); err != nil {
		t.Fatal(err)
	}
	for _, msg := range []string{"一", "二", "三"} {
		if err := db.AddEvent(ctx, state.Event{App: "mine", Stage: "upgrade", Message: msg}); err != nil {
			t.Fatal(err)
		}
	}
	w := do(t, h, "GET", "/v1/apps/mine/events", nil, nil)
	evs, _ := decodeObj(t, w.Body.String())["events"].([]any)
	if w.Code != http.StatusOK || len(evs) != 3 || evs[0].(map[string]any)["message"] != "一" || evs[0].(map[string]any)["stage"] != "upgrade" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	first := evs[0].(map[string]any)["id"].(float64)
	w = do(t, h, "GET", "/v1/apps/mine/events?limit=1&after="+strconv.Itoa(int(first)), nil, nil)
	evs, _ = decodeObj(t, w.Body.String())["events"].([]any)
	if len(evs) != 1 || evs[0].(map[string]any)["message"] != "二" {
		t.Fatalf("page %s", w.Body.String())
	}
	for _, c := range []struct {
		path   string
		status int
	}{{"/v1/apps/nothing/events", 404}, {"/v1/apps/mine/events?after=x", 400}, {"/v1/apps/mine/events?limit=0", 400}, {"/v1/apps/mine/events?limit=500", 400}} {
		if w := do(t, h, "GET", c.path, nil, nil); w.Code != c.status {
			t.Errorf("%s: %d %s", c.path, w.Code, w.Body.String())
		}
	}
}
