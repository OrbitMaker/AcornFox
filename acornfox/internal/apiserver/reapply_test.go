package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

// The routes of internal/appinstall/reapply.go: what they refuse, and that
// an app that is not from the app center is changed exactly as before. The
// whole apply (render, restart, put back) is in appinstall's tests, with the
// engine and its fake world; here is only the HTTP end.

// reapplyServer is the trusted handler over a real store that the app center
// shares (the services test catalog is enough: no app is installed).
func reapplyServer(t *testing.T) (http.Handler, *state.Store) {
	t.Helper()
	_, eng, db := servicesServer(t)
	dir := t.TempDir()
	h := New(Config{Store: db, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})
	return h, db
}

func TestReapplyRoutesRefuse(t *testing.T) {
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
		{"no such app", "PATCH", "/v1/apps/nothing/vars", `{"set":{"a":"b"}}`, 404, "CAT-INST-22"},
		{"nothing to change", "PATCH", "/v1/apps/mine/vars", `{}`, 400, "CAT-INST-02"},
		{"not json", "PATCH", "/v1/apps/mine/vars", `{`, 400, "CAT-INST-02"},
		{"a platform value that is not text", "PATCH", "/v1/apps/mine/vars", `{"set":{"platform.tz":5}}`, 400, "CAT-INST-28"},
		{"not from the app center", "PATCH", "/v1/apps/mine/vars", `{"set":{"a":"b"}}`, 409, "APP-APPLY-01"},
		{"reapply: not from the app center", "POST", "/v1/apps/mine/reapply", `{}`, 409, "APP-APPLY-01"},
		{"plan: not from the app center", "POST", "/v1/apps/mine/reapply", `{"plan":true}`, 409, "APP-APPLY-01"},
		{"assume_exposure needs plan", "POST", "/v1/apps/mine/reapply", `{"assume_exposure":"lan"}`, 400, "CAT-INST-02"},
		{"assume_exposure that is none", "POST", "/v1/apps/mine/reapply", `{"plan":true,"assume_exposure":"internet"}`, 400, "CAT-INST-02"},
		{"affected: a variable written wrong", "GET", "/v1/platform/affected?var=tz", ``, 400, "CAT-INST-02"},
		{"affected: no such platform variable", "GET", "/v1/platform/affected?var=platform.nope", ``, 400, "CAT-INST-28"},
	} {
		var body []byte
		if c.body != "" {
			body = []byte(c.body)
		}
		w := do(t, h, c.method, c.path, body, jsonHeader)
		if w.Code != c.status || errCodeOf(w.Body.String()) != c.code {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body.String())
		}
	}
	// the one that is allowed, with nothing behind
	w := do(t, h, "GET", "/v1/platform/affected", nil, nil)
	m := decodeObj(t, w.Body.String())
	if w.Code != http.StatusOK || m["apps"] == nil || len(m["apps"].([]any)) != 0 || len(m["notes"].([]any)) != 1 {
		t.Fatalf("affected %d %s", w.Code, w.Body.String())
	}
}

// An app that is not from the app center: the exposure and the domains
// change as they always did, no plan is asked for and no confirmation is
// wanted; a word the API does not know is refused.
func TestExposureOfAnOwnAppIsAsBefore(t *testing.T) {
	h, db := reapplyServer(t)
	ctx := context.Background()
	if _, _, err := db.EnsureApp(ctx, "mine"); err != nil {
		t.Fatal(err)
	}
	w := do(t, h, "PATCH", "/v1/apps/mine", []byte(`{"exposure":"lan"}`), jsonHeader)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"reapply"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if a, _ := db.GetApp(ctx, "mine"); a.Exposure != state.ExposureLAN {
		t.Fatalf("exposure %q", a.Exposure)
	}
	if w := do(t, h, "PATCH", "/v1/apps/mine", []byte(`{"exposure":"local","reapply":"later"}`), jsonHeader); w.Code != http.StatusBadRequest || errCodeOf(w.Body.String()) != "invalid_reapply" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if a, _ := db.GetApp(ctx, "mine"); a.Exposure != state.ExposureLAN {
		t.Fatalf("a refused request changed it: %q", a.Exposure)
	}
	w = do(t, h, "POST", "/v1/apps/mine/domains", []byte(`{"name":"a.example.com","reapply":"skip"}`), jsonHeader)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"reapply":{`) {
		t.Fatalf("domain %d %s", w.Code, w.Body.String())
	}
	if w := do(t, h, "POST", "/v1/apps/mine/domains", []byte(`{"name":"b.example.com","reapply":"later"}`), jsonHeader); w.Code != http.StatusBadRequest {
		t.Fatalf("domain bad reapply %d", w.Code)
	}
	if w := do(t, h, "DELETE", "/v1/apps/mine/domains/a.example.com?reapply=skip", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("domain remove %d %s", w.Code, w.Body.String())
	}
}

func errCodeOf(body string) string {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(body), &m)
	if e, ok := m["error"].(map[string]any); ok {
		c, _ := e["code"].(string)
		return c
	}
	c, _ := m["code"].(string)
	return c
}
