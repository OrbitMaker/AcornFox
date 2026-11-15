package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// Deleting an app with its data kept and installing it again on that data
// (--reuse-data) brings back what it was set up with: its config files in the
// data, the platform variables the user had changed, and the services its
// needs were plugged into, with the credentials that still pass the
// provider's check. See reuse.go and connect.go.

const marker = "# changed by the user\n"

func hermesVol(t *testing.T, v *env) string {
	t.Helper()
	vols := v.w.volumes["hermes-agent"]
	if len(vols) != 1 {
		t.Fatalf("hermes volumes %v", vols)
	}
	return vols[0]
}

// keepData deletes app with its data kept, the way the API server does it:
// its bindings (and credentials) stay in the registry.
func keepData(t *testing.T, v *env, app string) {
	t.Helper()
	if err := v.w.delete(context.Background(), app, false); err != nil {
		t.Fatal(err)
	}
}

// addOwnService registers the user's own model API (my-upstream).
func addOwnService(t *testing.T, v *env) {
	t.Helper()
	if _, err := v.e.AddService(context.Background(), AddServiceRequest{Name: "my-upstream", Kind: "llm_gateway",
		Values: map[string]string{"base_url": "https://llm.example/v1", "api_key": "sk-own-0123456789", "model": "m1"}}); err != nil {
		t.Fatal(err)
	}
}

func varOf(t *testing.T, v *env, app, name string) AppVar {
	t.Helper()
	av, err := v.e.AppVars(context.Background(), app, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range av.Vars {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("%s has no %s: %+v", app, name, av.Vars)
	return AppVar{}
}

func envOf(t *testing.T, v *env, app, key string) string {
	t.Helper()
	envs, err := v.w.st.ListEnv(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range envs {
		if e.Key == key {
			return e.Value
		}
	}
	return ""
}

func hasLine(lines []string, part string) bool {
	return slices.ContainsFunc(lines, func(c string) bool { return strings.Contains(c, part) })
}

// A fresh install writes config.yaml as before; an install on the old data
// with the same settings keeps the file the user changed, and says so.
func TestReuseKeepsChangedConfigFile(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	addOwnService(t, v)
	v.w.adopted[hermesDigestForTest] = true
	tv := v.install(t, Request{App: "hermes-agent", Params: hermesModel()})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if s := stepOf(tv, "hermes-agent", "files"); s.Message != "写了 1 个文件" || len(tv.Apps[0].KeptFiles) != 0 || len(tv.Apps[0].Carried) != 0 {
		t.Fatalf("fresh install: %+v %+v", s, tv.Apps[0])
	}
	vol := hermesVol(t, v)
	orig, _ := v.w.volFile(vol, "config.yaml")
	if !strings.Contains(orig, "sk-own-0123456789") {
		t.Fatalf("config.yaml:\n%s", orig)
	}
	v.w.editVolFile(vol, "config.yaml", orig+marker)
	keepData(t, v, "hermes-agent")

	tv = v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if got, _ := v.w.volFile(vol, "config.yaml"); got != orig+marker {
		t.Fatalf("config.yaml replaced:\n%s", got)
	}
	if s := stepOf(tv, "hermes-agent", "files"); s.Status != StepDone || s.Message != "沿用旧的配置文件 config.yaml" {
		t.Fatalf("files step %+v", s)
	}
	a := tv.Apps[0]
	if !slices.Equal(a.KeptFiles, []string{"config.yaml"}) || len(a.FileBackups) != 0 {
		t.Fatalf("app %+v", a)
	}
	create := stepOf(tv, "hermes-agent", "create").Message
	for _, want := range []string{"接上了旧数据", "恢复了删除时保留的参数"} {
		if !strings.Contains(create, want) || !hasLine(a.Carried, want) {
			t.Fatalf("create step %q / carried %v: want %q", create, a.Carried, want)
		}
	}
	if strings.Contains(create, "sk-own") {
		t.Fatal("the key is in the progress")
	}
	// plugged into the same service again
	if b, err := registry(t, v).GetBinding(ctx, "hermes-agent", "llm"); err != nil || b.Service != "my-upstream" || len(a.Bindings) != 1 || !a.Bindings[0].Reused {
		t.Fatalf("binding %+v %v (%+v)", b, err, a.Bindings)
	}
	if _, err := v.w.st.GetKeptCatalogSecrets(ctx, "hermes-agent"); err == nil {
		t.Fatal("kept record left behind")
	}
}

// The platform variables the user changed come back; changed again with
// --set in this request, this request wins.
func TestReuseRestoresUserPlatformVars(t *testing.T) {
	v := setup(t)
	if tv := v.install(t, Request{App: "new-api", Name: "tzapp", Platform: map[string]string{"tz": "Asia/Tokyo"}}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	keepData(t, v, "tzapp")
	tv := v.install(t, Request{App: "new-api", Name: "tzapp", ReuseData: true})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if got := envOf(t, v, "tzapp", "TZ"); got != "Asia/Tokyo" {
		t.Fatalf("TZ %q", got)
	}
	if x := varOf(t, v, "tzapp", "platform.tz"); x.Value != "Asia/Tokyo" || x.Origin != OriginPlatformUser {
		t.Fatalf("platform.tz %+v", x)
	}
	if !strings.Contains(stepOf(tv, "tzapp", "create").Message, "沿用以前改过的平台变量：platform.tz=Asia/Tokyo") {
		t.Fatalf("create %+v", stepOf(tv, "tzapp", "create"))
	}

	keepData(t, v, "tzapp")
	tv = v.install(t, Request{App: "new-api", Name: "tzapp", ReuseData: true, Platform: map[string]string{"tz": "Europe/Paris"}})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if got := envOf(t, v, "tzapp", "TZ"); got != "Europe/Paris" {
		t.Fatalf("TZ %q (this request should win)", got)
	}
	if !strings.Contains(stepOf(tv, "tzapp", "create").Message, "这次用 --set 重新写了 platform.tz，以这次的为准") {
		t.Fatalf("create %+v", stepOf(tv, "tzapp", "create"))
	}
}

// An app installed before the user's platform changes were saved apart
// cannot tell them from the machine's values: the progress says to set them
// again.
func TestReuseOldRecordWithoutPlatformUser(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	if tv := v.install(t, Request{App: "new-api", Platform: map[string]string{"tz": "Asia/Tokyo"}}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	// what an older version saved: no @platform_user
	saved, _ := v.e.savedParams(ctx, "new-api")
	delete(saved, platformUserKey)
	resealParams(t, v, "new-api", saved)
	keepData(t, v, "new-api")
	tv := v.install(t, Request{App: "new-api", ReuseData: true})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if got := envOf(t, v, "new-api", "TZ"); got != "UTC" {
		t.Fatalf("TZ %q (an old record cannot say tz was the user's)", got)
	}
	if !strings.Contains(stepOf(tv, "new-api", "create").Message, "要这次再写一遍") {
		t.Fatalf("create %+v", stepOf(tv, "new-api", "create"))
	}
}

// The consumer of a provided service: deleted with its data kept, its
// binding and credential stay; installed again on the data it is plugged
// into the same service, the credential passes the provider's check and is
// used again (no new one is made), so its config.yaml stays as the user
// left it. Deleting it with its data then takes that credential back.
func TestReuseKeepsCredentialThatPassesCheck(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	vol := hermesVol(t, v)
	orig, _ := v.w.volFile(vol, "config.yaml")
	v.w.editVolFile(vol, "config.yaml", orig+marker)
	before, _ := registry(t, v).GetBinding(ctx, "hermes-agent", "llm")
	keepData(t, v, "hermes-agent")
	if v.tokens.tokenCount() != 1 {
		t.Fatalf("the credential went with the delete: %v", v.tokens.tokens)
	}

	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	a := tv.Apps[0]
	if v.tokens.tokenCount() != 1 || len(a.Bindings) != 1 || !a.Bindings[0].Reused || a.Bindings[0].Issued {
		t.Fatalf("tokens %v, bindings %+v", v.tokens.tokens, a.Bindings)
	}
	after, _ := registry(t, v).GetBinding(ctx, "hermes-agent", "llm")
	if after.Issued["token_id"] != before.Issued["token_id"] || after.Issued["issue_id"] != before.Issued["issue_id"] {
		t.Fatalf("credential changed: %v → %v", before.Issued, after.Issued)
	}
	if !hasLine(a.Carried, "接着用以前 new-api 发给它的凭据（检查过还能用）") {
		t.Fatalf("carried %v", a.Carried)
	}
	if got, _ := v.w.volFile(vol, "config.yaml"); got != orig+marker || !slices.Equal(a.KeptFiles, []string{"config.yaml"}) {
		t.Fatalf("config.yaml:\n%s", got)
	}
	// Deleted with its data: taken back.
	if err := v.w.st.DeleteApp(ctx, "hermes-agent", false); err != nil {
		t.Fatal(err)
	}
	if w := v.e.ReleaseServices(ctx, "hermes-agent", true); len(w) != 0 {
		t.Fatalf("warnings %q", w)
	}
	if v.tokens.tokenCount() != 0 {
		t.Fatalf("not taken back: %v", v.tokens.tokens)
	}
}

// The provider deleted with its data: its service is gone, and with it the
// binding the consumer (deleted with its data kept) had to it, which the
// delete says. The consumer installed again on its data needs a model API
// this machine no longer has: refused before anything is created
// (CAT-INST-31), with how to register one.
func TestReuseProviderGoneRefused(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	keepData(t, v, "hermes-agent")
	if err := v.w.delete(ctx, "new-api", true); err != nil {
		t.Fatal(err)
	}
	w := v.e.ReleaseServices(ctx, "new-api", true)
	if len(w) != 1 || !strings.Contains(w[0], "hermes-agent") || !strings.Contains(w[0], "重新接服务") {
		t.Fatalf("warnings %q", w)
	}
	_, err := v.e.StartInstall(ctx, Request{App: "hermes-agent", ReuseData: true})
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeNoService || !strings.Contains(f.Message, "acornfox services add llm_gateway") || strings.Contains(f.Message, "new-api") {
		t.Fatalf("got %v", err)
	}
	if _, err := v.w.st.GetApp(ctx, "hermes-agent"); !errors.Is(err, state.ErrNotFound) {
		t.Fatal("a refused install created the app")
	}
}

// What the old app's config file holds changed: it is written anew and the
// old file is copied aside first, naming what changed.
func TestReuseChangedSettingsRewriteWithBackup(t *testing.T) {
	t.Run("old credential no longer passes the check", func(t *testing.T) {
		v := setup(t)
		installHermes(t, v)
		vol := hermesVol(t, v)
		orig, _ := v.w.volFile(vol, "config.yaml")
		v.w.editVolFile(vol, "config.yaml", orig+marker)
		b, _ := registry(t, v).GetBinding(context.Background(), "hermes-agent", "llm")
		// The user deleted the token in the provider.
		v.tokens.mu.Lock()
		delete(v.tokens.tokens, 1)
		v.tokens.mu.Unlock()
		keepData(t, v, "hermes-agent")
		tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
		if tv.Status != state.TaskSucceeded {
			t.Fatal(tv.Fault)
		}
		a := tv.Apps[0]
		if len(a.Bindings) != 1 || !a.Bindings[0].Issued || v.tokens.tokenCount() != 1 {
			t.Fatalf("app %+v tokens %v", a, v.tokens.tokens)
		}
		// Only the key differs from what the old file was written with.
		checkBackup(t, v, tv, vol, orig+marker, "uses.llm.api_key")
		notes := carriedRevoke(tv, "hermes-agent")
		if len(notes) != 1 || !strings.Contains(notes[0], "没通过检查，已经换了一份新的") || !strings.Contains(notes[0], b.Issued["issue_id"]) || !strings.Contains(notes[0], "new-api 里看") {
			t.Fatalf("carried %v", a.Carried)
		}
		// The credential not taken back is to be told; the item the
		// provider's manifest asks the user to fill in there (its default
		// model) is not, since Hermes does not use it.
		if tm := tv.Timing; tm == nil || len(tm.MustTell) != 1 || tm.MustTell[0] != notes[0] {
			t.Fatalf("must_tell %+v", tv.Timing)
		}
	})
	t.Run("another service chosen now", func(t *testing.T) {
		v := setup(t)
		installHermes(t, v)
		addOwnService(t, v)
		vol := hermesVol(t, v)
		orig, _ := v.w.volFile(vol, "config.yaml")
		v.w.editVolFile(vol, "config.yaml", orig+marker)
		keepData(t, v, "hermes-agent")
		tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Use: map[string]string{"llm": "my-upstream"}})
		if tv.Status != state.TaskSucceeded {
			t.Fatal(tv.Fault)
		}
		checkBackup(t, v, tv, vol, orig+marker, "uses.llm.api_key、uses.llm.base_url")
		if !slices.Equal(v.tokens.deleted, []int{1}) {
			t.Fatalf("the old credential not taken back: %v", v.tokens.deleted)
		}
		if !hasLine(tv.Apps[0].Carried, "已经收回") || len(carriedRevoke(tv, "hermes-agent")) != 0 {
			t.Fatalf("carried %v", tv.Apps[0].Carried)
		}
	})
}

func checkBackup(t *testing.T, v *env, tv TaskView, vol, old, changed string) {
	t.Helper()
	a := tv.Apps[0]
	if len(a.FileBackups) != 1 || a.FileBackups[0].Path != "config.yaml" || !strings.HasPrefix(a.FileBackups[0].Backup, "config.yaml.bak-") || len(a.KeptFiles) != 0 {
		t.Fatalf("app %+v", a)
	}
	if got, _ := v.w.volFile(vol, a.FileBackups[0].Backup); got != old {
		t.Fatalf("backup:\n%s", got)
	}
	if got, _ := v.w.volFile(vol, "config.yaml"); strings.Contains(got, marker) {
		t.Fatalf("config.yaml not written anew:\n%s", got)
	}
	msg := stepOf(tv, "hermes-agent", "files").Message
	if !strings.Contains(msg, "config.yaml 里平台填的内容变了（"+changed+"）") || !strings.Contains(msg, "旧文件另存为 "+a.FileBackups[0].Backup) {
		t.Fatalf("files step %q", msg)
	}
}

// A fresh install of a name whose old data is gone drops the stale kept
// record. Its kept binding is checked like any other: the credential still
// there is used again; a number the provider now uses for something else (it
// was set up anew) does not pass, a new one is issued, and the thing under
// the old number is left alone.
func TestFreshInstallDropsStaleKeptRecord(t *testing.T) {
	for _, reinstalled := range []bool{false, true} {
		v := setup(t)
		ctx := context.Background()
		installHermes(t, v)
		vol := hermesVol(t, v)
		keepData(t, v, "hermes-agent")
		if err := v.w.RemoveVolume(ctx, "hermes-agent", vol); err != nil { // the user removed the data
			t.Fatal(err)
		}
		if reinstalled {
			v.tokens.mu.Lock()
			v.tokens.tokens[1] = "someone-else"
			v.tokens.mu.Unlock()
		}
		tv := v.install(t, Request{App: "hermes-agent", Params: hermesModel()})
		if tv.Status != state.TaskSucceeded {
			t.Fatal(tv.Fault)
		}
		if _, err := v.w.st.GetKeptCatalogSecrets(ctx, "hermes-agent"); err == nil {
			t.Fatal("stale kept record left")
		}
		if len(v.tokens.deleted) != 0 {
			t.Fatalf("reinstalled=%v: deleted %v", reinstalled, v.tokens.deleted)
		}
		want := 1
		if reinstalled {
			want = 2
		}
		if v.tokens.tokenCount() != want || (reinstalled && len(carriedRevoke(tv, "hermes-agent")) != 1) {
			t.Fatalf("reinstalled=%v: tokens %v carried %v", reinstalled, v.tokens.tokens, tv.Apps[0].Carried)
		}
	}
}

// Old data without a kept record: an existing config file is kept, and the
// progress says no settings were found.
func TestReuseWithoutKeptRecordKeepsFile(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	addOwnService(t, v)
	v.w.adopted[hermesDigestForTest] = true
	if tv := v.install(t, Request{App: "hermes-agent", Params: hermesModel()}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	vol := hermesVol(t, v)
	orig, _ := v.w.volFile(vol, "config.yaml")
	v.w.editVolFile(vol, "config.yaml", orig+marker)
	keepData(t, v, "hermes-agent")
	if err := v.w.st.DeleteKeptCatalogSecrets(ctx, "hermes-agent"); err != nil {
		t.Fatal(err)
	}
	// Nothing kept to bring the model back: it is given again.
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Params: hermesModel()})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if got, _ := v.w.volFile(vol, "config.yaml"); got != orig+marker {
		t.Fatalf("config.yaml replaced:\n%s", got)
	}
	if !strings.Contains(stepOf(tv, "hermes-agent", "create").Message, "没有找到删除时保留的设置") {
		t.Fatalf("create %+v", stepOf(tv, "hermes-agent", "create"))
	}
}

// A failed install on old data keeps the kept record, and leaves the old
// binding and its credential, used again by it, alone.
func TestReuseFailureKeepsOldBinding(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	keepData(t, v, "hermes-agent")
	before, _ := registry(t, v).GetBinding(ctx, "hermes-agent", "llm")
	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = true
	v.w.mu.Unlock()
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskFailed || len(tv.Apps[0].Bindings) != 1 || !tv.Apps[0].Bindings[0].Reused {
		t.Fatalf("should fail after plugging in the old credential: %+v", tv)
	}
	if len(v.tokens.deleted) != 0 {
		t.Fatalf("the old credential was taken back by the failed install: %v", v.tokens.deleted)
	}
	if b, err := registry(t, v).GetBinding(ctx, "hermes-agent", "llm"); err != nil || b.Issued["issue_id"] != before.Issued["issue_id"] {
		t.Fatalf("binding %+v %v", b, err)
	}
	if _, err := v.w.st.GetKeptCatalogSecrets(ctx, "hermes-agent"); err != nil {
		t.Fatal("kept record lost by a failed install")
	}
	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = false
	v.w.mu.Unlock()
	if tv := v.install(t, Request{App: "hermes-agent", ReuseData: true}); tv.Status != state.TaskSucceeded || !tv.Apps[0].Bindings[0].Reused || v.tokens.tokenCount() != 1 {
		t.Fatalf("retry %+v", tv)
	}
}

// A required setting the user gave the old app (the model) comes back on
// its data without being given again, and can be changed; a required
// setting neither given nor kept is still refused (CAT-INST-01).
func TestReuseRestoresRequiredSetting(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	keepData(t, v, "hermes-agent")
	if tv := v.install(t, Request{App: "hermes-agent", ReuseData: true}); tv.Status != state.TaskSucceeded {
		t.Fatalf("on its data without the model: %+v", tv.Fault)
	}
	if vals, err := v.e.savedParams(ctx, "hermes-agent"); err != nil || vals["model"] != "demo-model" {
		t.Fatalf("model %v %v", vals["model"], err)
	}
	keepData(t, v, "hermes-agent")
	if tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Params: map[string]any{"model": "other-model"}}); tv.Status != state.TaskSucceeded {
		t.Fatalf("on its data with another model: %+v", tv.Fault)
	}
	if vals, _ := v.e.savedParams(ctx, "hermes-agent"); vals["model"] != "other-model" {
		t.Fatalf("model %v", vals["model"])
	}
	// The kept record lost: nothing to bring the model back.
	keepData(t, v, "hermes-agent")
	if err := v.w.st.DeleteKeptCatalogSecrets(ctx, "hermes-agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.e.StartInstall(ctx, Request{App: "hermes-agent", ReuseData: true}); code(err) != CodeBadParams || !strings.Contains(err.Error(), "model") {
		t.Fatalf("no kept record, no model: %v", err)
	}
	pv, err := v.e.Plan(ctx, Request{App: "hermes-agent", ReuseData: true})
	if err != nil || len(pv.Apps) != 1 || len(pv.Apps[0].ParamsMissing) != 1 || pv.Apps[0].ParamsMissing[0].Key != "model" {
		t.Fatalf("plan %+v %v", pv, err)
	}
}

// The old app on this data was installed with a manifest of the former
// format and got a credential made for it in another app, which a delete
// with the data kept did not mention (an install was meant to take it
// over). Installed on that data with a manifest of the current format, the
// install says that credential is not used any more and is left for the
// user to remove (CAT-INST-35), by its number.
func TestReuseFormerCredentialIsSaid(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	vals, err := v.e.savedParams(ctx, "hermes-agent")
	if err != nil {
		t.Fatal(err)
	}
	vals[legacyCredentialsKey] = []map[string]any{{"provider": "gw", "id": 4}}
	resealParams(t, v, "hermes-agent", vals)
	keepData(t, v, "hermes-agent")
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	notes := carriedRevoke(tv, "hermes-agent")
	if len(notes) != 1 || !strings.Contains(notes[0], "旧格式") || !strings.Contains(notes[0], "gw") || !strings.Contains(notes[0], "编号 4") {
		t.Fatalf("carried %v", tv.Apps[0].Carried)
	}
}

// resealParams replaces app's saved parameters (to play an older version's
// record).
func resealParams(t *testing.T, v *env, app string, vals map[string]any) {
	t.Helper()
	ctx := context.Background()
	cs, err := v.w.st.GetCatalogSecrets(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(vals)
	enc, err := v.e.box.seal(b)
	if err != nil {
		t.Fatal(err)
	}
	cs.ParamsEnc = enc
	if err := v.w.st.SetCatalogSecrets(ctx, cs); err != nil {
		t.Fatal(err)
	}
}

// The check answers one thing: can the old credential go on being used.
// The material's check passes a token the user turned off (still the same
// one; turning it on again is the user's call); a manifest whose check asks
// for it to be on does not pass it: a new one is issued, the old one is left
// where it is (the user may have turned it off on purpose) and the user is
// told to look.
func TestReuseCheckDecidesOnTurnedOff(t *testing.T) {
	strict := goodManifest(t, "new-api")
	const sameToken = `expect_json: { success: true, data: { id: "{{issued.token_id}}", name: "acornfox-{{consumer.name}}-{{issued.issue_id}}" } }`
	idx := strings.LastIndex(strict, sameToken)
	if idx < 0 {
		t.Fatal("the check step to change is not in the material")
	}
	strict = strict[:idx] + `expect_json: { success: true, data: { id: "{{issued.token_id}}", name: "acornfox-{{consumer.name}}-{{issued.issue_id}}", status: 1 } }` + strict[idx+len(sameToken):]
	if _, err := catalog.ParseManifest([]byte(strict)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		manifest string
		renewed  bool
	}{{"the material's check", goodManifest(t, "new-api"), false}, {"a check that asks for it on", strict, true}} {
		t.Run(c.name, func(t *testing.T) {
			b := typedBundle(t, goodDir, nil, c.manifest, goodManifest(t, "hermes-agent"))
			v, _ := setupSnapshot(t, bundleFS(b), nil)
			installHermes(t, v)
			v.tokens.mu.Lock()
			v.tokens.disabled = map[int]bool{1: true}
			v.tokens.mu.Unlock()
			keepData(t, v, "hermes-agent")
			tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
			if tv.Status != state.TaskSucceeded {
				t.Fatal(tv.Fault)
			}
			notes := carriedRevoke(tv, "hermes-agent")
			switch {
			case !c.renewed && (v.tokens.tokenCount() != 1 || len(notes) != 0 || !tv.Apps[0].Bindings[0].Reused):
				t.Fatalf("used again: tokens %v carried %v", v.tokens.tokens, tv.Apps[0].Carried)
			case c.renewed && (v.tokens.tokenCount() != 2 || len(v.tokens.deleted) != 0 || len(notes) != 1 || !strings.Contains(notes[0], "没通过检查")):
				t.Fatalf("renewed: tokens %v deleted %v carried %v", v.tokens.tokens, v.tokens.deleted, tv.Apps[0].Carried)
			}
		})
	}
}

// setDesired plays acornfox stop / start on app.
func setDesired(t *testing.T, v *env, app, desired string) {
	t.Helper()
	if _, err := v.w.st.UpdateApp(context.Background(), app, func(a *state.App) error { a.Desired = desired; return nil }); err != nil {
		t.Fatal(err)
	}
}

// The provider is installed but stopped: its service cannot be used, so an
// install that needs it is refused before a task starts (CAT-INST-31), on
// old data or not, and the refusal says to start it rather than to install
// anything. Started again, the install goes ahead and uses the old
// credential again.
func TestReuseProviderStoppedRefused(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	keepData(t, v, "hermes-agent")
	setDesired(t, v, "new-api", state.DesiredStopped)
	for name, req := range map[string]Request{
		"hermes-agent": {App: "hermes-agent", ReuseData: true},
		"h2":           {App: "hermes-agent", Name: "h2", Params: hermesModel()},
	} {
		_, err := v.e.StartInstall(ctx, req)
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeNoService || !strings.Contains(f.Message, "acornfox start new-api") ||
			strings.Contains(f.Message, "acornfox install") || f.Hint == "" {
			t.Fatalf("%+v: got %v", req, err)
		}
		pv, _ := v.e.Plan(ctx, req)
		n := needOf(t, pv, name, "llm")
		if n.Status != NeedRequiredMissing || !hasLine(n.How, "acornfox start new-api") || !hasLine(n.How, "acornfox services add llm_gateway") {
			t.Fatalf("plan %+v", n)
		}
	}
	if tasks, _ := v.w.st.ListInstallTasks(ctx, "", 50); len(tasks) != 2 { // new-api and the first hermes
		t.Fatalf("a refused install made a task: %d tasks", len(tasks))
	}
	setDesired(t, v, "new-api", state.DesiredRunning)
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskSucceeded || !tv.Apps[0].Bindings[0].Reused || v.tokens.tokenCount() != 1 {
		t.Fatalf("after start: %+v", tv)
	}
}

// Plugged into another service this time while the old provider is stopped:
// the old credential cannot be taken back. The install still succeeds, the
// old binding goes, and carried (a must-tell) says what to remove by hand
// and where, with the issue number.
func TestReuseOldCredentialNotTakenBackIsSaid(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	addOwnService(t, v)
	old, _ := registry(t, v).GetBinding(ctx, "hermes-agent", "llm")
	keepData(t, v, "hermes-agent")
	setDesired(t, v, "new-api", state.DesiredStopped)
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Use: map[string]string{"llm": "my-upstream"}})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	notes := carriedRevoke(tv, "hermes-agent")
	if len(notes) != 1 || !strings.Contains(notes[0], "new-api") || !strings.Contains(notes[0], old.Issued["issue_id"]) {
		t.Fatalf("carried %v", tv.Apps[0].Carried)
	}
	if tm := tv.Timing; tm == nil || len(tm.MustTell) != 1 || tm.MustTell[0] != notes[0] {
		t.Fatalf("must_tell %+v", tv.Timing)
	}
	if b, err := registry(t, v).GetBinding(ctx, "hermes-agent", "llm"); err != nil || b.Service != "my-upstream" {
		t.Fatalf("binding %+v %v", b, err)
	}
	if v.tokens.tokenCount() != 1 || len(v.tokens.deleted) != 0 {
		t.Fatalf("tokens %v deleted %v", v.tokens.tokens, v.tokens.deleted)
	}
}

// An install on old data that rewrote config.yaml (another service) and then
// failed leaves that attempt's file in the data, while the kept record still
// holds the old settings. The next install on the data must not take the
// file for the old app's: it is written anew (and that attempt's copied
// aside), not "kept as it was".
func TestReuseAfterFailedRewrite(t *testing.T) {
	v := setup(t)
	installHermes(t, v)
	addOwnService(t, v)
	vol := hermesVol(t, v)
	orig, _ := v.w.volFile(vol, "config.yaml")
	v.w.editVolFile(vol, "config.yaml", orig+marker)
	keepData(t, v, "hermes-agent")

	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = true
	v.w.mu.Unlock()
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Use: map[string]string{"llm": "my-upstream"}})
	if tv.Status != state.TaskFailed || len(tv.Apps[0].FileBackups) != 1 {
		t.Fatalf("first attempt should rewrite, then fail: %+v", tv)
	}
	failed, _ := v.w.volFile(vol, "config.yaml")
	if !strings.Contains(failed, "sk-own-0123456789") {
		t.Fatalf("first attempt's file:\n%s", failed)
	}
	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = false
	v.w.mu.Unlock()

	tv = v.install(t, Request{App: "hermes-agent", ReuseData: true, Use: map[string]string{"llm": "new-api"}})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("%+v", tv)
	}
	a := tv.Apps[0]
	if len(a.FileBackups) != 1 || len(a.KeptFiles) != 0 {
		t.Fatalf("not rewritten: %+v", a)
	}
	if got, _ := v.w.volFile(vol, a.FileBackups[0].Backup); got != failed {
		t.Fatalf("the failed attempt's file not copied aside:\n%s", got)
	}
	got, _ := v.w.volFile(vol, "config.yaml")
	if strings.Contains(got, "sk-own") || !strings.Contains(got, `base_url: "http://new-api:3000/v1"`) {
		t.Fatalf("config.yaml should hold the provider's again:\n%s", got)
	}
	if msg := stepOf(tv, "hermes-agent", "files").Message; !strings.Contains(msg, "上次接旧数据没装好时按那次的设置改写过") {
		t.Fatalf("files step %q", msg)
	}
}
