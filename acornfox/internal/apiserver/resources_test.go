package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

func decodeResources(t *testing.T, body []byte) resourcesView {
	t.Helper()
	var v resourcesView
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode resources: %v; body=%s", err, body)
	}
	return v
}

func wantCode(t *testing.T, code int, body string, status int, errCode string) {
	t.Helper()
	if code != status || (errCode != "" && !strings.Contains(body, `"`+errCode+`"`)) {
		t.Fatalf("status = %d body=%s, want %d %s", code, body, status, errCode)
	}
}

func TestAutoMemoryCap(t *testing.T) {
	for _, c := range []struct{ host, want int }{
		{0, 0},
		{961, 1922},    // small host (under 2 GB): 2x over-commit
		{1966, 3932},   // still small
		{2048, 1024},   // 1 GB reserve
		{8192, 6964},   // 15% reserve
		{32020, 27217}, // the 32 GB dev box
	} {
		if got := autoMemoryCapMB(c.host); got != c.want {
			t.Errorf("autoMemoryCapMB(%d) = %d, want %d", c.host, got, c.want)
		}
	}
}

func TestGetResourcesCountsAppsAndAddons(t *testing.T) {
	h, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")
	st.AddAddon(ctx, state.Addon{App: "web", Kind: state.AddonPostgres, EnvVar: "DATABASE_URL"})

	w := do(t, h, "GET", "/v1/resources", nil, nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusOK, "")
	v := decodeResources(t, w.Body.Bytes())
	if v.Memory.HostMB != 8192 || v.Memory.CapMB != 6964 || !v.Memory.Auto || v.Memory.AllocatedMB != 768 {
		t.Fatalf("memory = %+v", v.Memory)
	}
	if v.CPU.HostMilli != 4000 || v.CPU.CapMilli != 4000 || !v.CPU.Auto || v.CPU.MaxAppMilli != 1000 {
		t.Fatalf("cpu = %+v", v.CPU)
	}
	if len(v.Items) != 2 || v.Items[0].Kind != "app" || v.Items[1].Kind != state.AddonPostgres || v.Items[1].MemoryMB != 512 {
		t.Fatalf("items = %+v", v.Items)
	}
	if v.MinAppMemoryMB != 256 || v.MinAppCPUMilli != 500 {
		t.Fatalf("minimums = %d MB / %d milli", v.MinAppMemoryMB, v.MinAppCPUMilli)
	}
}

func TestPutResourcesBounds(t *testing.T) {
	h, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")
	st.UpdateApp(ctx, "web", func(a *state.App) error { a.MemoryMB, a.CPUMilli = 1024, 2000; return nil })

	put := func(body string) (int, string) {
		w := do(t, h, "PUT", "/v1/resources", []byte(body), nil)
		return w.Code, w.Body.String()
	}
	c, b := put(`{"memory_mb":512}`)
	wantCode(t, c, b, http.StatusConflict, "cap_below_allocated")
	c, b = put(`{"memory_mb":9000}`)
	wantCode(t, c, b, http.StatusBadRequest, "cap_above_host")
	c, b = put(`{"memory_mb":100}`)
	wantCode(t, c, b, http.StatusBadRequest, "invalid_memory")
	c, b = put(`{"cpu_milli":1000}`)
	wantCode(t, c, b, http.StatusConflict, "cap_below_allocated")
	c, b = put(`{"cpu_milli":8000}`)
	wantCode(t, c, b, http.StatusBadRequest, "cap_above_host")

	c, b = put(`{"memory_mb":2048,"cpu_milli":3000}`)
	wantCode(t, c, b, http.StatusOK, "")
	v := decodeResources(t, []byte(b))
	if v.Memory.CapMB != 2048 || v.Memory.Auto || v.CPU.CapMilli != 3000 || v.CPU.Auto {
		t.Fatalf("after set: memory %+v cpu %+v", v.Memory, v.CPU)
	}
	// 0 goes back to automatic; an omitted field stays as it is.
	c, b = put(`{"memory_mb":0}`)
	wantCode(t, c, b, http.StatusOK, "")
	v = decodeResources(t, []byte(b))
	if v.Memory.CapMB != 6964 || !v.Memory.Auto || v.CPU.CapMilli != 3000 {
		t.Fatalf("after reset: memory %+v cpu %+v", v.Memory, v.CPU)
	}
}

func TestPatchAppLimitsAgainstCaps(t *testing.T) {
	h, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")
	st.EnsureApp(ctx, "api")
	st.SetResourceLimits(ctx, state.ResourceLimits{MemoryMB: 1536, CPUMilli: 2000})

	patch := func(body string) (int, string) {
		w := do(t, h, "PATCH", "/v1/apps/web", []byte(body), nil)
		return w.Code, w.Body.String()
	}
	c, b := patch(`{"memory_mb":255}`)
	wantCode(t, c, b, http.StatusBadRequest, "invalid_memory")
	c, b = patch(`{"cpu_milli":499}`)
	wantCode(t, c, b, http.StatusBadRequest, "invalid_cpu")
	c, b = patch(`{"cpu_milli":2500}`)
	wantCode(t, c, b, http.StatusBadRequest, "invalid_cpu")
	// web 256 + api 256 under a 1536 cap: web may grow by 1024, not by 1088.
	c, b = patch(`{"memory_mb":1344}`)
	wantCode(t, c, b, http.StatusConflict, "memory_cap_exceeded")
	c, b = patch(`{"memory_mb":1024,"cpu_milli":2000}`)
	wantCode(t, c, b, http.StatusOK, "")
	a, _ := st.GetApp(ctx, "web")
	if a.MemoryMB != 1024 || a.CPUMilli != 2000 {
		t.Fatalf("web = %+v", a)
	}
}

func TestPatchAppAppliesLimitsInPlace(t *testing.T) {
	h, st, _, rn, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")
	st.UpdateApp(ctx, "web", func(a *state.App) error { a.CurrentDeployment, a.MemoryMB = "0123456789ab", 1024; return nil })
	live := runner.ContainerName("web", "0123456789ab")
	rn.stats = map[string]runner.StatsResponse{live: {MemoryUsageMB: 600}}

	// Lowering below current usage plus the margin is refused and changes nothing.
	w := do(t, h, "PATCH", "/v1/apps/web", []byte(`{"memory_mb":512}`), nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusConflict, "memory_below_usage")
	if len(rn.updates) != 0 {
		t.Fatalf("refused patch reached the runner: %+v", rn.updates)
	}

	w = do(t, h, "PATCH", "/v1/apps/web", []byte(`{"memory_mb":768,"cpu_milli":1500}`), nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusOK, "")
	want := runner.UpdateResourcesRequest{App: "web", Name: live, MemoryMB: 768, CPUMilli: 1500}
	if len(rn.updates) != 1 || rn.updates[0] != want {
		t.Fatalf("runner updates = %+v, want %+v", rn.updates, want)
	}
	if !strings.Contains(w.Body.String(), `"applied_now":true`) {
		t.Fatalf("response lacks applied_now: %s", w.Body.String())
	}

	// Port changes never touch the container.
	w = do(t, h, "PATCH", "/v1/apps/web", []byte(`{"port":3000}`), nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusOK, "")
	if len(rn.updates) != 1 {
		t.Fatalf("port patch updated the container: %+v", rn.updates)
	}
}

func TestNewAppAndAddonRespectMemoryCap(t *testing.T) {
	h, st, _, _, _ := newTestServer(t)
	ctx := context.Background()
	st.EnsureApp(ctx, "web")
	st.SetResourceLimits(ctx, state.ResourceLimits{MemoryMB: 500})

	// A new app would take another 256 MB: refused before anything is created.
	w := do(t, h, "POST", "/v1/apps/other/deployments", []byte("tarball"), nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusConflict, "memory_cap_exceeded")
	if _, err := st.GetApp(ctx, "other"); err == nil {
		t.Fatal("refused deployment still created the app")
	}
	w = do(t, h, "POST", "/v1/apps/other/deployments", []byte(`{"image":"nginx:alpine"}`), map[string]string{"Content-Type": "application/json"})
	wantCode(t, w.Code, w.Body.String(), http.StatusConflict, "memory_cap_exceeded")

	// Existing apps can still be redeployed.
	w = do(t, h, "POST", "/v1/apps/web/deployments", []byte("tarball"), nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusAccepted, "")

	// PostgreSQL needs 512 MB, only 244 MB are left.
	w = do(t, h, "POST", "/v1/apps/web/addons", []byte(`{"kind":"postgres"}`), nil)
	wantCode(t, w.Code, w.Body.String(), http.StatusConflict, "memory_cap_exceeded")
	if addons, _ := st.ListAddons(ctx, "web"); len(addons) != 0 {
		t.Fatalf("refused add-on was added: %+v", addons)
	}
}

// Small hosts (under 2 GB) may be over-committed 2x, automatically and when
// the cap is set by hand.
func TestSmallHostOvercommit(t *testing.T) {
	st := newFakeStore()
	h := New(Config{Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(), HostMetrics: fakeHostSized(1024, 2)})
	ctx := context.Background()
	w := do(t, h, "GET", "/v1/resources", nil, nil)
	if v := decodeResources(t, w.Body.Bytes()); v.Memory.CapMB != 2048 || !v.Memory.Auto {
		t.Fatalf("auto cap on 1 GB = %+v, want 2048", v.Memory)
	}
	// Two apps (256 MB each) + PostgreSQL + Redis + MySQL fit on a 1 GB host.
	st.EnsureApp(ctx, "web")
	st.EnsureApp(ctx, "web2")
	for _, k := range []struct{ app, body string }{{"web", `{"kind":"postgres"}`}, {"web", `{"kind":"redis"}`}, {"web2", `{"kind":"mysql"}`}} {
		w := do(t, h, "POST", "/v1/apps/"+k.app+"/addons", []byte(k.body), nil)
		if w.Code >= 300 {
			t.Fatalf("add %s to %s: %d %s", k.body, k.app, w.Code, w.Body.String())
		}
	}
	put := func(body string) int {
		return do(t, h, "PUT", "/v1/resources", []byte(body), nil).Code
	}
	if c := put(`{"memory_mb":2000}`); c != http.StatusOK {
		t.Fatalf("2000 MB on a 1 GB host = %d, want 200", c)
	}
	if c := put(`{"memory_mb":2100}`); c != http.StatusBadRequest {
		t.Fatalf("2100 MB on a 1 GB host = %d, want 400", c)
	}
}
