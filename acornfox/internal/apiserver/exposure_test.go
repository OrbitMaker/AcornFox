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

// On a Linux server a local-only app shows the loopback address and
// local_only, not the public host (which would not open it); other
// exposures keep the public host.
func TestLocalOnlyAppShowsLoopback(t *testing.T) {
	h, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")
	a, _ := st.GetApp(ctx, "web")
	if w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"local"}`), nil); w.Code != http.StatusOK {
		t.Fatalf("patch %d", w.Code)
	}
	var v appView
	_ = json.Unmarshal(do(t, h, "GET", "/v1/apps/web", nil, nil).Body.Bytes(), &v)
	if a.PublicPort == 0 {
		t.Fatal("test app has no port")
	}
	if v.URL != "http://127.0.0.1:"+itoa(a.PublicPort) || !v.LocalOnly {
		t.Fatalf("local view %+v", v)
	}
	if w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"public"}`), nil); w.Code != http.StatusOK {
		t.Fatalf("patch %d", w.Code)
	}
	v = appView{}
	_ = json.Unmarshal(do(t, h, "GET", "/v1/apps/web", nil, nil).Body.Bytes(), &v)
	if v.LocalOnly || v.URL == "http://127.0.0.1:"+itoa(a.PublicPort) {
		t.Fatalf("public view %+v", v)
	}
}

// A local-only app on a Linux server carries an open hint: the command for
// an SSH tunnel always; the LAN address and the URL after switching when
// the server has one. Without its own login (not from the catalog) the LAN
// switch is not offered. Other exposures have no hint; neither does the
// desktop, where localhost opens on the same computer.
func TestLocalOnlyOpenHint(t *testing.T) {
	st := newFakeStore()
	lan := "192.168.1.20"
	h := New(Config{
		Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(),
		PublicHost: "192.168.1.20", HostMetrics: fakeHostSized(8192, 4),
		ServerLAN: func() string { return lan },
	})
	ctx := context.Background()
	a, _, _ := st.EnsureApp(ctx, "web")
	if w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"local"}`), nil); w.Code != http.StatusOK {
		t.Fatalf("patch %d", w.Code)
	}
	get := func() appView {
		var v appView
		_ = json.Unmarshal(do(t, h, "GET", "/v1/apps/web", nil, nil).Body.Bytes(), &v)
		return v
	}
	v := get()
	hint := v.OpenHint
	if hint == nil || hint.Command != "acornfox open web" || hint.LANAddress != lan ||
		hint.LANURL != "http://192.168.1.20:"+itoa(a.PublicPort) || hint.HasLogin || hint.CanLAN || hint.Recommend != "tunnel" {
		t.Fatalf("hint %+v", hint)
	}
	// A cloud server: no LAN address, the tunnel is the way.
	lan = ""
	if hint := get().OpenHint; hint == nil || hint.LANAddress != "" || hint.LANURL != "" || hint.CanLAN || hint.Recommend != "tunnel" {
		t.Fatalf("cloud hint %+v", hint)
	}
	if w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"lan"}`), nil); w.Code != http.StatusOK {
		t.Fatalf("patch %d", w.Code)
	}
	if v := get(); v.OpenHint != nil {
		t.Fatalf("lan app has a hint: %+v", v.OpenHint)
	}
}

// The app list carries the serving version number and whether the app
// follows the platform default exposure, so the console's app cards need no
// detail request per app.
func TestListCarriesLiveSeqAndDefaultExposure(t *testing.T) {
	h, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")
	d1, _, _ := st.CreateDeployment(ctx, state.NewDeployment{App: "web", SourceKind: state.SourceUpload, SourceRef: "/x", SourceDigest: "d1"})
	st.UpdateDeployment(ctx, d1.ID, func(d *state.Deployment) error { d.Status = state.StatusLive; return nil })
	st.UpdateApp(ctx, "web", func(a *state.App) error { a.CurrentDeployment = d1.ID; return nil })

	list := func() appView {
		t.Helper()
		w := do(t, h, "GET", "/v1/apps", nil, nil)
		var body struct {
			Apps []appView `json:"apps"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Apps) != 1 {
			t.Fatalf("list = %s (%v)", w.Body.String(), err)
		}
		return body.Apps[0]
	}
	v := list()
	if v.LiveSeq != d1.Seq || v.Live != nil {
		t.Fatalf("list live_seq = %d (want %d), live = %+v (list keeps no full live view)", v.LiveSeq, d1.Seq, v.Live)
	}
	if !v.ExposureIsDefault || v.DefaultExposure != state.ExposurePublic || v.Exposure != state.ExposurePublic {
		t.Fatalf("default exposure view = %+v", v)
	}

	// A newer deployment still pending: the old version keeps serving.
	d2, _, _ := st.CreateDeployment(ctx, state.NewDeployment{App: "web", SourceKind: state.SourceUpload, SourceRef: "/y", SourceDigest: "d2"})
	v = list()
	if v.LiveSeq != d1.Seq || v.Latest == nil || v.Latest.Seq != d2.Seq {
		t.Fatalf("pending newer deployment: live_seq = %d, latest = %+v", v.LiveSeq, v.Latest)
	}
	w := do(t, h, "GET", "/v1/apps/web", nil, nil)
	var detail appView
	_ = json.Unmarshal(w.Body.Bytes(), &detail)
	if detail.Live == nil || detail.Live.Seq != d1.Seq || detail.LiveSeq != d1.Seq {
		t.Fatalf("detail live = %+v, live_seq = %d", detail.Live, detail.LiveSeq)
	}

	// Its own exposure: no longer following the default.
	do(t, h, "PATCH", "/v1/apps/web", []byte(`{"exposure":"local"}`), nil)
	if v = list(); v.ExposureIsDefault || v.Exposure != state.ExposureLocal || v.DefaultExposure != state.ExposurePublic {
		t.Fatalf("own exposure view = %+v", v)
	}

	// Never deployed: no live_seq.
	st.EnsureApp(ctx, "empty")
	w = do(t, h, "GET", "/v1/apps/empty", nil, nil)
	var e appView
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if e.LiveSeq != 0 || e.Live != nil {
		t.Fatalf("never deployed: %+v", e)
	}
}
