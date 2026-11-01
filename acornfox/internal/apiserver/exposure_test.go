package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

func TestPatchExposure(t *testing.T) {
	h, st, k, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")

	w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"local"}`), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", w.Code, w.Body.String())
	}
	if a, _ := st.GetApp(ctx, "web"); a.Exposure != state.ExposureLocal {
		t.Fatalf("exposure not stored: %+v", a)
	}
	if len(k.kicks) == 0 {
		t.Fatal("an exposure change must kick the app so routes follow")
	}
	var body struct {
		App appView `json:"app"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.App.Exposure != "local" {
		t.Fatalf("view exposure = %q", body.App.Exposure)
	}

	if w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"internet"}`), nil); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown exposure status = %d", w.Code)
	}
	if w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"afx_cloud"}`), nil); w.Code != http.StatusNotImplemented {
		t.Fatalf("afx_cloud must be reserved (501), got %d", w.Code)
	}
	// "" resets to the platform default (public on Linux).
	if w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":""}`), nil); w.Code != http.StatusOK {
		t.Fatalf("reset status = %d", w.Code)
	}
	w = do(t, h, "GET", "/v1/apps/web", nil, nil)
	var v appView
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if v.Exposure != "public" || v.LANURL != "" {
		t.Fatalf("linux default view = %+v", v)
	}
}

func TestDesktopURLs(t *testing.T) {
	st := newFakeStore()
	h := New(Config{
		Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(),
		PublicHost: "localhost", HostMetrics: fakeHostSized(8192, 4),
		DefaultExposure: "lan", LANHost: func() string { return "192.168.31.20" },
	})
	ctx := context.Background()
	a, _, _ := st.EnsureApp(ctx, "web")
	w := do(t, h, "GET", "/v1/apps/web", nil, nil)
	var v appView
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	want := "http://192.168.31.20:" + itoa(a.PublicPort)
	if v.Exposure != "lan" || v.URL != "http://localhost:"+itoa(a.PublicPort) || v.LANURL != want {
		t.Fatalf("view = %+v, want lan with localhost and %s", v, want)
	}
	do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"local"}`), nil)
	w = do(t, h, "GET", "/v1/apps/web", nil, nil)
	v = appView{}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	if v.LANURL != "" {
		t.Fatalf("local exposure must not show a LAN address: %+v", v)
	}
}

func TestLANConsoleEndpointsAbsentWithoutManager(t *testing.T) {
	h, _, _, _, _ := newTestServer(t)
	if w := do(t, h, "GET", "/v1/lan-console", nil, nil); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d", w.Code)
	}
}
