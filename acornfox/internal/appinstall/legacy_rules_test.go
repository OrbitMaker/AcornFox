package appinstall

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// An app from the app center with no manifest saved at all (installed by a
// version that did not save manifests, its package not here to backfill it
// from) and plain saved parameters is one installed the former way: apps
// vars, the app's detail (legacy_note, so `status`) and the service registry
// all say so, by the one rule (installedWith). An app still being installed
// has no manifest yet either and is not called one; an app not from the app
// center is never one.
func TestLegacyWithoutSavedManifest(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	oldInstall(t, v, "na", "new-api", "v1.0.0-rc.48", map[string]any{
		"admin_username": "admin", "admin_password": "old-password-1234567",
	})
	if _, err := v.w.st.GetCatalogManifest(ctx, "na"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("the test app has a manifest: %v", err)
	}
	if n := v.e.LegacyNote(ctx, "na"); n != LegacyAppNote {
		t.Fatalf("legacy note %q", n)
	}
	av, err := v.e.AppVars(ctx, "na", false)
	if err != nil || !hasNote(av, LegacyAppNote) || !hasNote(av, "清单没存") {
		t.Fatalf("apps vars %+v %v", av.Notes, err)
	}
	if m := v.e.appManifest(ctx, "na"); m != nil {
		t.Fatal("a manifest for an app installed the former way")
	}
	if uses, err := v.e.AppServices(ctx, "na"); err != nil || uses != nil {
		t.Fatalf("services of a former app: %+v %v", uses, err)
	}

	// Being installed: not (yet) a former one.
	oldInstall(t, v, "busy", "new-api", "v1.0.0-rc.48", map[string]any{"admin_username": "admin"})
	v.e.mu.Lock()
	v.e.running["busy"] = "t1"
	v.e.mu.Unlock()
	if n := v.e.LegacyNote(ctx, "busy"); n != "" {
		t.Fatalf("an app being installed: legacy note %q", n)
	}
	v.e.mu.Lock()
	delete(v.e.running, "busy")
	v.e.mu.Unlock()
	if n := v.e.LegacyNote(ctx, "busy"); n != LegacyAppNote {
		t.Fatalf("once nobody installs it: legacy note %q", n)
	}

	// Not from the app center.
	if _, _, err := v.w.st.EnsureApp(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if n := v.e.LegacyNote(ctx, "web"); n != "" {
		t.Fatalf("an app not from the app center: legacy note %q", n)
	}
}

// When the catalog in use has no app this platform could install (right
// after an upgrade: the built-in catalog is empty), asking for any app or
// recipe says so, and does not send the user to sync the catalog, which
// cannot help. A catalog of former apps only is the same for an app that is
// not in it (an app that is in it is CAT-INST-36). A catalog with an app to
// install keeps the usual words.
func TestNothingToInstall(t *testing.T) {
	ctx := context.Background()
	nothing := func(what string, err error) {
		t.Helper()
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeUnknown || f.HTTPStatus() != http.StatusNotFound ||
			!strings.HasPrefix(f.Message, catalog.NothingToInstall) || f.Hint != catalog.NothingToInstallHint ||
			strings.Contains(f.Message+f.Hint, "sync") || strings.Contains(f.Message+f.Hint, "同步") {
			t.Fatalf("%s: %+v", what, err)
		}
	}

	empty, _ := setupSnapshot(t, bundleFS(fixtureBundle(t, 4, "", nil, nil, nil)), nil)
	_, err := empty.e.StartInstall(ctx, Request{App: "new-api", Name: "na2"})
	nothing("install", err)
	pv, err := empty.e.Plan(ctx, Request{App: "new-api", Name: "na2"})
	if err != nil || pv.CanInstall || len(pv.Problems) != 1 || pv.Problems[0].Code != CodeUnknown || !strings.HasPrefix(pv.Problems[0].Message, catalog.NothingToInstall) {
		t.Fatalf("plan %+v %v", pv, err)
	}
	_, err = empty.e.StartInstall(ctx, Request{App: "new-api", Catalog: PublicSource})
	nothing("install from the public catalog", err)
	_, err = empty.e.StartInstall(ctx, Request{Recipe: "hermes-with-new-api"})
	nothing("recipe", err)
	_, err = empty.e.Detail(ctx, "new-api")
	nothing("detail", err)
	nothingCreated(t, empty)

	former, _ := setupSnapshot(t, bundleFS(fixtureBundle(t, 4, "", []fixApp{{manifest: formerManifest(t, "new-api"), noPackage: true}}, nil, nil)), nil)
	_, err = former.e.StartInstall(ctx, Request{App: "hermes-agent"})
	nothing("only former apps", err)
	if _, err = former.e.StartInstall(ctx, Request{App: "new-api"}); code(err) != CodeLegacyManifest {
		t.Fatalf("a former app that is there: %v", err)
	}

	v := setup(t)
	_, err = v.e.StartInstall(ctx, Request{App: "no-such-app"})
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeUnknown || strings.Contains(f.Message, catalog.NothingToInstall) || !strings.Contains(f.Message, "目录里没有应用 no-such-app") ||
		!strings.Contains(f.Hint, "acornfox catalog sync") {
		t.Fatalf("an app that is not there, others are: %+v", err)
	}
}

// install --name APP --reuse-data without the catalog app: the platform reads
// which catalog app it was from what was kept when APP was deleted with its
// data. Found: installed as that app, on the old data. Not found: CAT-INST-02
// says so and how to name the app. A kept record of an app installed the
// former way names its catalog app all the same.
func TestReuseByNameOnly(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installAndKeepData(t, v, Request{App: "new-api", Name: "gw"})
	pv, err := v.e.Plan(ctx, Request{Name: "gw", ReuseData: true})
	if err != nil || len(pv.Steps) != 1 || pv.Steps[0].App != "new-api" || pv.Steps[0].Name != "gw" {
		t.Fatalf("plan %+v %v", pv, err)
	}
	tv := v.install(t, Request{Name: "gw", ReuseData: true})
	if tv.Status != state.TaskSucceeded || len(tv.Apps) != 1 || len(tv.Apps[0].ReusedVolumes) == 0 {
		t.Fatalf("install %+v", tv)
	}
	if a, err := v.w.st.GetApp(ctx, "gw"); err != nil || a.CatalogID != "new-api" {
		t.Fatalf("app %+v %v", a, err)
	}

	// Nothing kept under that name.
	for _, name := range []string{"nobody", "web"} {
		if name == "web" {
			if _, _, err := v.w.st.EnsureApp(ctx, "web"); err != nil {
				t.Fatal(err)
			}
			if err := v.w.st.DeleteApp(ctx, "web", true); err != nil {
				t.Fatal(err)
			}
		}
		_, err := v.e.StartInstall(ctx, Request{Name: name, ReuseData: true})
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeBadParams || !strings.Contains(f.Message, "删除时保留下来的记录") ||
			!strings.Contains(f.Message, "acornfox install 应用 --name "+name+" --reuse-data") {
			t.Fatalf("%s: %+v", name, err)
		}
		pv, err := v.e.Plan(ctx, Request{Name: name, ReuseData: true})
		if err != nil || pv.CanInstall || len(pv.Problems) != 1 || pv.Problems[0].Code != CodeBadParams {
			t.Fatalf("%s plan %+v %v", name, pv, err)
		}
	}
	if _, err := v.e.StartInstall(ctx, Request{Name: "gw2"}); code(err) != CodeBadParams {
		t.Fatalf("without reuse_data the app must be named: %v", err)
	}

	// Kept from an app installed the former way.
	oldInstall(t, v, "old-gw", "new-api", "v1.0.0-rc.48", map[string]any{
		"admin_username": "admin", "admin_password": "old-password-1234567",
		legacyCredentialsKey: []map[string]any{{"provider": "x", "id": 2}},
	})
	fm := formerManifest(t, "new-api")
	if err := v.w.st.SetCatalogManifest(ctx, state.CatalogManifest{App: "old-gw", CatalogID: "new-api", CatalogVersion: "v1.0.0-rc.48",
		Manifest: fm, SHA256: state.ManifestSHA256(fm), Source: state.ManifestFromInstall}); err != nil {
		t.Fatal(err)
	}
	if err := v.w.st.DeleteApp(ctx, "old-gw", true); err != nil {
		t.Fatal(err)
	}
	if k, err := v.w.st.GetKeptCatalogSecrets(ctx, "old-gw"); err != nil || k.Manifest != fm {
		t.Fatalf("kept %+v %v", k.CatalogID, err)
	}
	pv, err = v.e.Plan(ctx, Request{Name: "old-gw", ReuseData: true})
	if err != nil || len(pv.Steps) != 1 || pv.Steps[0].App != "new-api" || pv.Steps[0].Name != "old-gw" {
		t.Fatalf("plan of a former app's data %+v %v", pv, err)
	}
	for _, p := range pv.Problems {
		if p.Code == CodeBadParams || p.Code == CodeUnknown {
			t.Fatalf("plan of a former app's data: %+v", p)
		}
	}

	if got := ReuseCommand("gw"); got != "acornfox install --name gw --reuse-data" {
		t.Fatalf("command %q", got)
	}
}
