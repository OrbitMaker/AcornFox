package appinstall

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

func varsByName(v AppVars) map[string]AppVar {
	out := map[string]AppVar{}
	for _, x := range v.Vars {
		out[x.Name] = x
	}
	return out
}

func hasNote(v AppVars, part string) bool {
	for _, n := range v.Notes {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

// An install saves the manifest it used, the platform variables the user
// changed (apart from the merged ones) and where each value came from;
// AppVars shows all of it, secrets masked unless revealed.
func TestInstallSavesManifestAndVars(t *testing.T) {
	v := setupWith(t, func(c *Config) { c.Platform = PlatformEnv{China: true, TZ: "Asia/Shanghai"} })
	ctx := context.Background()
	tv := v.install(t, Request{App: "new-api", Params: map[string]any{"admin_username": "root"}, Platform: map[string]string{"tz": " Asia/Tokyo "}})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	want := goodManifest(t, "new-api")
	cm, err := v.w.st.GetCatalogManifest(ctx, "new-api")
	if err != nil || cm.Manifest != want || cm.SHA256 != state.ManifestSHA256(want) || cm.Source != state.ManifestFromInstall ||
		cm.CatalogID != "new-api" || cm.CatalogVersion != "v1.0.0-rc.48" || cm.CatalogOrigin != "" {
		t.Fatalf("saved manifest: %+v %v", cm, err)
	}
	saved, err := v.e.savedParams(ctx, "new-api")
	if err != nil {
		t.Fatal(err)
	}
	if user, ok := stringMap(saved[platformUserKey]); !ok || len(user) != 1 || user["tz"] != "Asia/Tokyo" {
		t.Fatalf("@platform_user %v", saved[platformUserKey])
	}
	// The merged values are saved as before.
	if p := Values(saved).platformMap(); p["tz"] != "Asia/Tokyo" || p["region"] != "cn" || p["app.url"] == "" {
		t.Fatalf("@platform %v", p)
	}

	av, err := v.e.AppVars(ctx, "new-api", false)
	if err != nil {
		t.Fatal(err)
	}
	if !av.FromCatalog || av.CatalogID != "new-api" || av.Version != "v1.0.0-rc.48" || av.Source.ID != PublicSource ||
		!av.Manifest.Saved || av.Manifest.Source != state.ManifestFromInstall || av.Manifest.SHA256 != cm.SHA256 || !av.CanChange || av.Revealed {
		t.Fatalf("head %+v", av)
	}
	if !hasNote(av, "acornfox apps set") || !hasNote(av, "--reveal") {
		t.Fatalf("notes %v", av.Notes)
	}
	rows := varsByName(av)
	for name := range rows {
		if strings.Contains(name, "@") || strings.Contains(name, "_acornfox_") {
			t.Fatalf("internal key listed: %s", name)
		}
	}
	check := func(name, layer, class, origin, editable string, secret bool) AppVar {
		t.Helper()
		r, ok := rows[name]
		if !ok {
			t.Fatalf("no row %s in %v", name, av.Vars)
		}
		if r.Layer != layer || r.Class != class || r.Origin != origin || r.Editable != editable || r.Secret != secret ||
			r.LayerLabel == "" || r.ClassLabel == "" || r.OriginLabel == "" || r.EditableLabel == "" {
			t.Fatalf("row %+v", r)
		}
		return r
	}
	if r := check("params.admin_username", "custom", catalog.ClassDefault, OriginUser, "never", false); r.Value != "root" || r.Masked {
		t.Fatalf("admin_username %+v", r)
	}
	pw := check("params.admin_password", "custom", catalog.ClassGenerated, OriginGenerated, "never", true)
	if !pw.Masked || pw.Value != Masked+v.tokens.password[len(v.tokens.password)-4:] {
		t.Fatalf("password not masked to its last 4: %+v", pw)
	}
	if r := check("params.session_secret", "custom", catalog.ClassGenerated, OriginGenerated, "never", true); !r.Masked || len(r.Value) != len(Masked)+4 {
		t.Fatalf("session_secret %+v", r)
	}
	tz := check("platform.tz", "system", catalog.ClassSystem, OriginPlatformUser, editPerApp, false)
	if tz.Value != "Asia/Tokyo" || tz.Used == nil || !*tz.Used {
		t.Fatalf("tz %+v", tz)
	}
	// Every other platform variable was worked out by the platform; the
	// ones this manifest does not refer to say so.
	others := 0
	for name, r := range rows {
		if r.Layer != "system" || name == "platform.tz" {
			continue
		}
		others++
		if r.Origin != OriginPlatform || r.Used == nil || *r.Used {
			t.Fatalf("platform row %+v", r)
		}
	}
	if others < 4 || rows["platform.region"].Value != "cn" || rows["platform.region"].Editable != editNever {
		t.Fatalf("platform rows: %d %+v", others, rows["platform.region"])
	}
	// Sorted by name.
	for i := 1; i < len(av.Vars); i++ {
		if av.Vars[i-1].Name >= av.Vars[i].Name {
			t.Fatalf("not sorted: %s, %s", av.Vars[i-1].Name, av.Vars[i].Name)
		}
	}
	// Nothing secret in the masked answer.
	b, _ := json.Marshal(av)
	if strings.Contains(string(b), v.tokens.password) {
		t.Fatal("the masked answer holds the password")
	}

	full, err := v.e.AppVars(ctx, "new-api", true)
	if err != nil || !full.Revealed {
		t.Fatal(err)
	}
	if r := varsByName(full)["params.admin_password"]; r.Value != v.tokens.password || r.Masked || !r.Secret {
		t.Fatalf("revealed %+v", r)
	}
	if hasNote(full, "--reveal") {
		t.Fatal("the reveal hint is shown although revealed")
	}
}

// What a service hands an app is not a variable of the app: a consumer's
// rows are its own parameters and the platform variables, never the items
// of the service its need is plugged into (they are in the binding, the
// credential encrypted), and no model name is made up for it.
func TestAppVarsOfConsumer(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	if tv := v.install(t, Request{Recipe: "hermes-with-new-api", Params: hermesModel()}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	av, err := v.e.AppVars(ctx, "hermes-agent", false)
	if err != nil {
		t.Fatal(err)
	}
	rows := varsByName(av)
	for name := range rows {
		if strings.HasPrefix(name, "params.llm") || strings.HasPrefix(name, "uses.") || name == "platform.llm.model.chat" {
			t.Fatalf("a row for what the service hands out: %s", name)
		}
	}
	if r := rows["params.dashboard_password"]; r.Origin != OriginGenerated || !r.Masked {
		t.Fatalf("dashboard_password %+v", r)
	}
	for name, want := range map[string]bool{"platform.mirror.pypi": true, "platform.tz": true, "platform.region": false} {
		if r := rows[name]; r.Used == nil || *r.Used != want {
			t.Fatalf("%s used: %+v", name, r)
		}
	}
	if hasNote(av, "旧格式") {
		t.Fatalf("an app installed the current way called former: %v", av.Notes)
	}
	full, _ := v.e.AppVars(ctx, "hermes-agent", true)
	if b, _ := json.Marshal(full); strings.Contains(string(b), "sk-K1") {
		t.Fatal("the credential shows among the variables")
	}
}

// An app installed with a manifest of the former format: its variables are
// listed (exit 0), a value that is an object is one row per field, all
// masked, and a note says it was installed the former way and needs
// reinstalling for the new features.
func TestAppVarsFormerApp(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	oldInstall(t, v, "old-agent", "hermes-agent", "v0.23.0", map[string]any{
		"dashboard_password": "old-password-0001",
		"llm":                map[string]any{"source": "local_gateway", "app": "gw", "base_url": "http://gw:3000/v1", "api_key": "sk-old-key-000099", "model": "m1"},
		originsKey:           map[string]any{"llm": OriginPlatform},
		platformKey:          map[string]any{"tz": "Asia/Shanghai", "llm.api_key": "sk-platform-000777"},
		platformUserKey:      map[string]any{},
		legacyCredentialsKey: []map[string]any{{"provider": "gw", "id": 4}},
	})
	former := formerManifest(t, "hermes-agent")
	if err := v.w.st.SetCatalogManifest(ctx, state.CatalogManifest{App: "old-agent", CatalogID: "hermes-agent", CatalogVersion: "v0.23.0",
		Manifest: former, SHA256: state.ManifestSHA256(former), Source: state.ManifestFromInstall}); err != nil {
		t.Fatal(err)
	}
	av, err := v.e.AppVars(ctx, "old-agent", false)
	if err != nil || !av.FromCatalog || !hasNote(av, LegacyAppNote) {
		t.Fatalf("%+v %v", av, err)
	}
	// The app's detail says the same sentence (legacy_note); an app of the
	// current format, or none at all, says nothing.
	if n := v.e.LegacyNote(ctx, "old-agent"); n != LegacyAppNote {
		t.Fatalf("legacy note %q", n)
	}
	if n := v.e.LegacyNote(ctx, "no-such-app"); n != "" {
		t.Fatalf("legacy note of an app that is not there: %q", n)
	}
	rows := varsByName(av)
	for _, f := range []string{"source", "app", "base_url", "api_key", "model"} {
		r, ok := rows["params.llm."+f]
		if !ok || !r.Secret || r.Layer != "custom" {
			t.Fatalf("params.llm.%s: %+v (%v)", f, r, ok)
		}
	}
	if r := rows["params.llm.api_key"]; !r.Masked || r.Value != Masked+"0099" {
		t.Fatalf("api_key %+v", r)
	}
	if _, ok := rows["params.llm"]; ok {
		t.Fatal("the object is listed whole as well")
	}
	// A platform variable this version no longer has: shown masked, it
	// may have been a secret one.
	if r := rows["platform.llm.api_key"]; !r.Secret || !r.Masked || r.Value != Masked+"0777" {
		t.Fatalf("platform variable not on the table %+v", r)
	}
	if r := rows["platform.tz"]; r.Secret || r.Value != "Asia/Shanghai" {
		t.Fatalf("platform.tz %+v", r)
	}
	if b, _ := json.Marshal(av); strings.Contains(string(b), "sk-old-key") || strings.Contains(string(b), "sk-platform") || strings.Contains(string(b), "_acornfox_") {
		t.Fatalf("a secret or a record key in the masked answer: %s", b)
	}
	// It takes no part in services, and deleting it reminds the user of the
	// credential another app made for it.
	if uses, err := v.e.AppServices(ctx, "old-agent"); err != nil || uses != nil {
		t.Fatalf("services of a former app: %+v %v", uses, err)
	}
	notes := v.e.LegacyCredentialNotes(ctx, "old-agent")
	if len(notes) != 1 || !strings.HasPrefix(notes[0], CodeRevoke+" ") || !strings.Contains(notes[0], "gw 里") || !strings.Contains(notes[0], "编号 4") ||
		!strings.Contains(notes[0], "acornfox-old-agent") {
		t.Fatalf("notes %q", notes)
	}
	if notes := v.e.LegacyCredentialNotes(ctx, "nobody"); notes != nil {
		t.Fatalf("notes for no app: %q", notes)
	}
}

// A recipe's preset is told from what the user typed.
func TestParamOrigins(t *testing.T) {
	m := manifest()
	got := paramOrigins(m, map[string]any{"pass": "my-own-pass", "port": 8080.0, "user": "fromrecipe", "hidden": ""}, map[string]bool{"user": true})
	want := map[string]string{"user": OriginRecipe, "pass": OriginUser, "hidden": OriginGenerated, "port": OriginUser, "need": OriginDefault}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %q, want %q", k, got[k], w)
		}
	}
	if o := paramOrigins(m, nil, nil); o["port"] != OriginUnset || o["pass"] != OriginGenerated || o["user"] != OriginDefault {
		t.Errorf("nothing given: %v", o)
	}
}

// oldInstall records an app the way a version before saved manifests left
// it: catalog id and version on the app, encrypted parameters without
// origins or the user's platform changes, and no manifest.
func oldInstall(t *testing.T, v *env, name, catalogID, version string, params map[string]any) {
	t.Helper()
	ctx := context.Background()
	if _, _, err := v.w.st.EnsureApp(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := v.w.st.UpdateApp(ctx, name, func(a *state.App) error { a.CatalogID, a.CatalogVersion = catalogID, version; return nil }); err != nil {
		t.Fatal(err)
	}
	pb, _ := json.Marshal(params)
	pe, err := v.e.box.seal(pb)
	if err != nil {
		t.Fatal(err)
	}
	le, _ := v.e.box.seal([]byte(`{"app":"` + name + `","fields":[]}`))
	if err := v.w.st.SetCatalogSecrets(ctx, state.CatalogSecrets{App: name, ParamsEnc: pe, FirstLoginEnc: le}); err != nil {
		t.Fatal(err)
	}
}

// Apps installed before manifests were saved get theirs from the packages on
// this machine, once; an app whose version no local index lists stays
// without one, and its variables are listed without classes, all masked.
// The packages here are those of the catalog published in the former format
// (what such apps were installed with); the backfilled manifest is of that
// format and says so.
func TestBackfillManifests(t *testing.T) {
	v, hits := setupSnapshot(t, bundleFS(fixtureBundle(t, 3, "", []fixApp{{manifest: formerManifest(t, "new-api")}, {manifest: formerManifest(t, "hermes-agent")}}, nil, nil)), nil)
	ctx := context.Background()
	old := map[string]any{
		"admin_username": "admin", "admin_password": "old-password-1234567", "session_secret": strings.Repeat("ab", 32),
		platformKey:          map[string]any{"tz": "Asia/Tokyo", "region": "cn", "app.url": "http://127.0.0.1:18810"},
		legacyCredentialsKey: []legacyCredential{{Provider: "x", ID: 1}},
	}
	oldInstall(t, v, "new-api", "new-api", "v1.0.0-rc.48", old)
	oldInstall(t, v, "gone", "new-api", "v0.0.1", old)         // a version no index here lists
	if _, _, err := v.w.st.EnsureApp(ctx, "web"); err != nil { // not from the catalog
		t.Fatal(err)
	}

	saved, skipped := v.e.BackfillManifests(ctx)
	if saved != 1 || skipped != 1 {
		t.Fatalf("first run: saved %d, skipped %d", saved, skipped)
	}
	if hits.Load() != 0 {
		t.Fatalf("the backfill went to the network %d times", hits.Load())
	}
	cm, err := v.w.st.GetCatalogManifest(ctx, "new-api")
	if err != nil || cm.Source != state.ManifestFromBackfill || cm.Manifest != formerManifest(t, "new-api") || cm.CatalogVersion != "v1.0.0-rc.48" {
		t.Fatalf("backfilled manifest: %+v %v", cm, err)
	}
	if _, err := v.w.st.GetCatalogManifest(ctx, "gone"); err == nil {
		t.Fatal("a manifest was made up for a version that is not here")
	}
	// Again: nothing to save; the missing one is skipped again, harmlessly.
	if saved, skipped := v.e.BackfillManifests(ctx); saved != 0 || skipped != 1 {
		t.Fatalf("second run: saved %d, skipped %d", saved, skipped)
	}
	if cm2, _ := v.w.st.GetCatalogManifest(ctx, "new-api"); !cm2.SavedAt.Equal(cm.SavedAt) {
		t.Fatal("the second run rewrote the manifest")
	}

	// The backfilled app: classes from the manifest, origins honest about
	// what an old record cannot tell.
	av, err := v.e.AppVars(ctx, "new-api", false)
	if err != nil {
		t.Fatal(err)
	}
	if !av.Manifest.Saved || av.Manifest.Source != state.ManifestFromBackfill || !strings.Contains(av.Manifest.SourceLabel, "补存") || !hasNote(av, "装时的值") ||
		!hasNote(av, LegacyAppNote) {
		t.Fatalf("head %+v", av)
	}
	rows := varsByName(av)
	if r := rows["platform.tz"]; r.Origin != OriginInstallTime || r.Value != "Asia/Tokyo" || r.Class != catalog.ClassSystem {
		t.Fatalf("tz %+v", r)
	}
	if r := rows["params.admin_username"]; r.Origin != OriginInstallTime || r.Class != catalog.ClassDefault || r.Value != "admin" {
		t.Fatalf("admin_username %+v", r)
	}
	if r := rows["params.admin_password"]; r.Origin != OriginInstallTime || !r.Masked || r.Value != Masked+"4567" {
		t.Fatalf("admin_password %+v", r)
	}
	// A hidden generated parameter can only have been generated.
	if r := rows["params.session_secret"]; r.Origin != OriginGenerated || !r.Masked {
		t.Fatalf("session_secret %+v", r)
	}

	// No manifest: the saved values only, no classes, every custom value
	// masked, and a note that says why.
	gone, err := v.e.AppVars(ctx, "gone", false)
	if err != nil {
		t.Fatal(err)
	}
	if gone.Manifest.Saved || gone.Manifest.Source != "" || !hasNote(gone, "清单没存，分不出类别") || !hasNote(gone, LegacyAppNote) {
		t.Fatalf("head %+v", gone)
	}
	grows := varsByName(gone)
	if len(grows) != 6 {
		t.Fatalf("rows %v", gone.Vars)
	}
	for name, r := range grows {
		if r.Class != "" || r.ClassLabel != "" || r.Origin != OriginInstallTime {
			t.Fatalf("%s: %+v", name, r)
		}
		if r.Layer == "custom" && (!r.Secret || !r.Masked) {
			t.Fatalf("%s is shown in clear without a manifest: %+v", name, r)
		}
	}
	if r := grows["params.admin_username"]; r.Value != Masked || r.EditableLabel != "清单没存，不知道" {
		t.Fatalf("short value %+v", r)
	}
	if r := grows["platform.tz"]; r.Value != "Asia/Tokyo" || r.Masked {
		t.Fatalf("platform value %+v", r)
	}
	if full, _ := v.e.AppVars(ctx, "gone", true); varsByName(full)["params.admin_username"].Value != "admin" {
		t.Fatal("reveal does not show the value")
	}

	// Not a catalog app: a plain answer, not an error. No such app: 404.
	web, err := v.e.AppVars(ctx, "web", false)
	if err != nil || web.FromCatalog || len(web.Vars) != 0 || !hasNote(web, "不是从应用中心装的") {
		t.Fatalf("plain app: %+v %v", web, err)
	}
	_, err = v.e.AppVars(ctx, "nope", false)
	if f := faultOf(t, err); f.Code != CodeNotCatalogApp || f.HTTPStatus() != 404 {
		t.Fatalf("missing app: %+v", f)
	}
	if _, err := v.e.AppVars(ctx, "Bad Name", true); code(err) != CodeNotCatalogApp {
		t.Fatalf("bad name: %v", err)
	}
}

// Deleting with the data kept keeps the manifest with the parameters; the
// reinstall on that data saves the manifest of this install again and marks
// the values it put back.
func TestReuseDataKeepsManifest(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installAndKeepData(t, v, Request{App: "new-api"})
	want := goodManifest(t, "new-api")
	k, err := v.w.st.GetKeptCatalogSecrets(ctx, "new-api")
	if err != nil || k.Manifest != want || k.ManifestSHA256 != state.ManifestSHA256(want) {
		t.Fatalf("kept manifest: %d bytes, %v", len(k.Manifest), err)
	}
	if tv := v.install(t, Request{App: "new-api", ReuseData: true}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	cm, err := v.w.st.GetCatalogManifest(ctx, "new-api")
	if err != nil || cm.Manifest != want || cm.Source != state.ManifestFromInstall {
		t.Fatalf("manifest after the reuse install: %+v %v", cm.Source, err)
	}
	av, err := v.e.AppVars(ctx, "new-api", false)
	if err != nil {
		t.Fatal(err)
	}
	rows := varsByName(av)
	if rows["params.admin_password"].Origin != OriginRestored || rows["params.session_secret"].Origin != OriginRestored {
		t.Fatalf("restored values: %+v", rows["params.admin_password"])
	}

	// Old data the platform has no parameters for: the values init would
	// have set are the old data's own.
	v2 := setup(t)
	installAndKeepData(t, v2, Request{App: "new-api"})
	if err := v2.w.st.DeleteKeptCatalogSecrets(ctx, "new-api"); err != nil {
		t.Fatal(err)
	}
	if tv := v2.install(t, Request{App: "new-api", ReuseData: true}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	av2, err := v2.e.AppVars(ctx, "new-api", true)
	if err != nil {
		t.Fatal(err)
	}
	if r := varsByName(av2)["params.admin_password"]; r.Origin != OriginOldData || r.Value != "" {
		t.Fatalf("old data's password: %+v", r)
	}
}

func TestMaskSecret(t *testing.T) {
	for in, want := range map[string]string{"": "", "short": Masked, "elevenchars": Masked, "twelve-chars": Masked + "hars", "密码密码密码密码密码密码一二三四": Masked + "一二三四"} {
		if got := maskSecret(in); got != want {
			t.Errorf("maskSecret(%q) = %q, want %q", in, got, want)
		}
	}
}

// An app installed with a manifest of the current format has no legacy
// note (GET /v1/apps/{app} leaves legacy_note out).
func TestLegacyNoteOnlyForFormerApps(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	for _, app := range []string{"new-api", "hermes-agent"} {
		if n := v.e.LegacyNote(ctx, app); n != "" {
			t.Fatalf("%s: legacy note %q", app, n)
		}
	}
}
