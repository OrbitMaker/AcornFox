package apiserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// Fault injection for CAT-SVC-08: an app is plugged into a service another
// app provides; stopping the provider makes the runtime page (the same check
// as `acornfox doctor`) warn about it, naming the app, its need, the service
// and how to bring it back. Starting the provider again clears it.
func TestRuntimeChecksWarnWhenProviderStopped(t *testing.T) {
	withRuntimeEnv(t, "darwin", "", 50<<30)
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
	setDesired := func(app, desired string) {
		t.Helper()
		if _, err := db.UpdateApp(ctx, app, func(a *state.App) error { a.Desired = desired; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, app := range []string{"gw", "agent"} {
		if _, _, err := db.EnsureApp(ctx, app); err != nil {
			t.Fatal(err)
		}
		setDesired(app, state.DesiredRunning)
	}
	reg, err := eng.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateService(ctx, appinstall.Service{Name: "gw", Kind: "llm_gateway", Source: state.ServiceFromApp, App: "gw",
		Values: map[string]string{"base_url": "http://gw:3000/v1", "api_key": "sk-test-0000"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetBinding(ctx, appinstall.Binding{App: "agent", UseName: "llm", Service: "gw"}); err != nil {
		t.Fatal(err)
	}

	it := itemWith(runtimeGet(t, h), "应用用的服务：")
	if it == nil || it.Level != "ok" || it.Check != "CAT-SVC-08" || !strings.Contains(it.Text, "1 个应用接着的服务都可用") {
		t.Fatalf("provider running: %+v", it)
	}

	// Inject: the provider is stopped.
	setDesired("gw", state.DesiredStopped)
	v := runtimeGet(t, h)
	it = itemWith(v, "应用用的服务：")
	if it == nil || it.Level != "warn" || it.Code != "CAT-SVC-08" || !v.OK {
		t.Fatalf("provider stopped: %+v (all %+v)", it, v.Items)
	}
	for _, want := range []string{"应用 agent 的需要 llm 接的服务 gw（大模型接口）", "“提供它的应用停着”", "acornfox start gw"} {
		if !strings.Contains(it.Text, want) {
			t.Fatalf("%q lacks %q", it.Text, want)
		}
	}
	if strings.Contains(it.Text, "sk-test") {
		t.Fatalf("a secret leaked: %q", it.Text)
	}

	// Recovered: started again, the line passes.
	setDesired("gw", state.DesiredRunning)
	if it := itemWith(runtimeGet(t, h), "应用用的服务："); it == nil || it.Level != "ok" {
		t.Fatalf("after start: %+v", it)
	}
}
