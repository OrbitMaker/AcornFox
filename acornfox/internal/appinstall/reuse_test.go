package appinstall

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

func installAndKeepData(t *testing.T, v *env, req Request) []string {
	t.Helper()
	if tv := v.install(t, req); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	name := req.Name
	if name == "" {
		name = req.App
	}
	vols := append([]string(nil), v.w.volumes[name]...)
	if len(vols) == 0 {
		t.Fatal("no volume recorded")
	}
	if err := v.w.delete(context.Background(), name, false); err != nil {
		t.Fatal(err)
	}
	return vols
}

// Deleted with its data kept, an app of the same name is refused
// (CAT-INST-24) unless reuse_data; then the kept parameters come back (same
// admin password, same session key), init is skipped, and a Hermes can still
// be linked to the New API because the platform knows its admin again.
func TestReuseDataRestoresParameters(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	vols := installAndKeepData(t, v, Request{App: "new-api"})
	oldPassword := v.newapi.password
	if _, err := v.e.StartInstall(ctx, Request{App: "new-api"}); code(err) != CodeOldVolumes {
		t.Fatalf("without reuse_data: %v", err)
	}
	tv := v.install(t, Request{App: "new-api", ReuseData: true})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("reuse: %+v", tv.Fault)
	}
	for _, s := range tv.Steps {
		if s.Name == "init" && (s.Status != StepSkipped || !strings.Contains(s.Message, "和以前一样")) {
			t.Fatalf("init %+v", s)
		}
	}
	if len(tv.Apps[0].Volumes) != 0 || !slices.Equal(tv.Apps[0].ReusedVolumes, vols) {
		t.Fatalf("apps %+v (want reused %v)", tv.Apps, vols)
	}
	fl, err := v.e.FirstLogin(ctx, "new-api", true)
	if err != nil || fl.Fields[1].Value != oldPassword || fl.Fields[0].Value != "admin" {
		t.Fatalf("first login %+v %v (want the old password back)", fl.Fields, err)
	}
	if _, err := v.w.st.GetKeptCatalogSecrets(ctx, "new-api"); err == nil {
		t.Fatal("kept parameters not dropped after the reuse install")
	}
	// the reused gateway still issues tokens: the platform knows its admin
	v.w.adopted[hermesDigestForTest] = true
	h := v.install(t, Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{"source": "local_gateway"}}})
	if h.Status != state.TaskSucceeded {
		t.Fatalf("hermes on the reused New API: %+v", h.Fault)
	}
}

// Old data without a kept record (deleted by an older version): init runs;
// New API says it is initialized already, which is accepted, and the
// accounts are reported as the old ones the platform does not know.
func TestReuseDataWithoutKeptRecord(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installAndKeepData(t, v, Request{App: "new-api"})
	if err := v.w.st.DeleteKeptCatalogSecrets(ctx, "new-api"); err != nil {
		t.Fatal(err)
	}
	tv := v.install(t, Request{App: "new-api", ReuseData: true})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("reuse: %+v", tv.Fault)
	}
	for _, s := range tv.Steps {
		if s.Name == "init" && (s.Status != StepSkipped || !strings.Contains(s.Message, "已经初始化过")) {
			t.Fatalf("init %+v", s)
		}
	}
	fl, _ := v.e.FirstLogin(ctx, "new-api", true)
	for _, f := range fl.Fields {
		if f.Value != ReusedLoginValue {
			t.Fatalf("first login %+v", fl.Fields)
		}
	}
	saved, _ := v.e.savedParams(ctx, "new-api")
	if _, ok := saved["admin_password"]; ok {
		t.Fatal("a password that does not work was kept")
	}
}

// Data kept from another catalog app is not taken over.
func TestReuseDataOfAnotherApp(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installAndKeepData(t, v, Request{App: "new-api", Name: "box"})
	v.w.adopted[hermesDigestForTest] = true
	_, err := v.e.StartInstall(ctx, Request{App: "hermes-agent", Name: "box", ReuseData: true, Params: map[string]any{"llm": map[string]any{"source": "own", "base_url": "https://x.example/v1", "api_key": "sk-test-key", "model": "m"}}})
	if code(err) != CodeOldVolumes || !strings.Contains(err.Error(), "new-api") {
		t.Fatalf("got %v", err)
	}
}

// A failed install that took over old volumes leaves them alone.
func TestReuseDataFailureKeepsOldVolumes(t *testing.T) {
	v := setup(t)
	vols := installAndKeepData(t, v, Request{App: "new-api"})
	v.w.mu.Lock()
	v.w.failPull = true
	v.w.mu.Unlock()
	v.w.adopted = map[string]bool{}
	tv := v.install(t, Request{App: "new-api", ReuseData: true})
	if tv.Status != state.TaskFailed {
		t.Fatalf("should fail: %+v", tv)
	}
	for _, rv := range v.w.removedVols {
		if slices.Contains(vols, rv) {
			t.Fatalf("old volume %s removed by the failed install", rv)
		}
	}
	if _, err := v.w.st.GetKeptCatalogSecrets(context.Background(), "new-api"); err != nil {
		t.Fatal("kept parameters lost by a failed reuse install")
	}
}
