package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

// Deleting an app with its data kept and installing it again on that data
// (--reuse-data) brings back what it was set up with: its config files in the
// data, the platform variables the user had changed, and its model source
// with its gateway token. See reuse.go.

const marker = "# changed by the user\n"

func hermesVol(t *testing.T, v *env) string {
	t.Helper()
	vols := v.w.volumes["hermes-agent"]
	if len(vols) != 1 {
		t.Fatalf("hermes volumes %v", vols)
	}
	return vols[0]
}

// keepData deletes app with its data kept, the way the API server does it
// now: the gateway tokens stay with the kept parameters.
func keepData(t *testing.T, v *env, app string) {
	t.Helper()
	if err := v.w.delete(context.Background(), app, false); err != nil {
		t.Fatal(err)
	}
}

func ownLLM() map[string]any {
	return map[string]any{"llm": map[string]any{"source": "own", "base_url": "https://llm.example/v1", "api_key": "sk-own-0123456789", "model": "m1"}}
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

// A fresh install writes config.yaml as before; an install on the old data
// with the same settings keeps the file the user changed, and says so.
func TestReuseKeepsChangedConfigFile(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	v.w.adopted[hermesDigestForTest] = true
	tv := v.install(t, Request{App: "hermes-agent", Params: ownLLM()})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if s := stepOf(tv, "hermes-agent", "files"); s.Message != "写了 1 个文件" || len(tv.Apps[0].KeptFiles) != 0 || len(tv.Apps[0].Carried) != 0 {
		t.Fatalf("fresh install: %+v %+v", s, tv.Apps[0])
	}
	vol := hermesVol(t, v)
	orig, _ := v.w.volFile(vol, "config.yaml")
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
	for _, want := range []string{"接上了旧数据", "恢复了删除时保留的参数", "大模型沿用原来的：自己的 Key（llm.example）"} {
		if !strings.Contains(create, want) || !slices.ContainsFunc(a.Carried, func(c string) bool { return strings.Contains(c, want) }) {
			t.Fatalf("create step %q / carried %v: want %q", create, a.Carried, want)
		}
	}
	if strings.Contains(create, "sk-own") {
		t.Fatal("the key is in the progress")
	}
	// the model source came back as it was, and says where from
	if x := varOf(t, v, "hermes-agent", "params.llm.api_key"); x.Value != "sk-own-0123456789" || x.Origin != OriginRestored {
		t.Fatalf("llm key %+v", x)
	}
	if x := varOf(t, v, "hermes-agent", "params.llm.model"); x.Value != "m1" || x.Origin != OriginRestored {
		t.Fatalf("llm model %+v", x)
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

// Hermes on the local New API: deleted with its data kept, its token stays;
// installed again on the data (without --llm, or with --llm local) it gets
// the same gateway and the same token back, so its config.yaml stays as the
// user left it and no new token is issued. Deleting it with its data then
// revokes that token.
func TestReuseLocalGatewayTakesOverToken(t *testing.T) {
	for _, llm := range []any{nil, "local"} {
		v := setup(t)
		ctx := context.Background()
		first := installHermes(t, v)
		old := *first.Apps[0].GatewayToken
		vol := hermesVol(t, v)
		orig, _ := v.w.volFile(vol, "config.yaml")
		if !strings.Contains(orig, "sk-K1") {
			t.Fatalf("config.yaml without the token:\n%s", orig)
		}
		v.w.editVolFile(vol, "config.yaml", orig+marker)
		keepData(t, v, "hermes-agent")
		if len(v.newapi.deleted) != 0 || len(v.newapi.tokens) != 1 {
			t.Fatalf("token revoked on delete with data kept: %v %v", v.newapi.deleted, v.newapi.tokens)
		}

		req := Request{App: "hermes-agent", ReuseData: true}
		if llm != nil {
			req.Params = map[string]any{"llm": llm}
		}
		tv := v.install(t, req)
		if tv.Status != state.TaskSucceeded {
			t.Fatalf("llm=%v: %+v", llm, tv.Fault)
		}
		a := tv.Apps[0]
		if a.GatewayToken != nil || a.TakenToken == nil || *a.TakenToken != old || v.newapi.nextID != 1 {
			t.Fatalf("llm=%v: token issued again: %+v (tokens made %d)", llm, a, v.newapi.nextID)
		}
		if got, _ := v.w.volFile(vol, "config.yaml"); got != orig+marker {
			t.Fatalf("llm=%v: config.yaml replaced:\n%s", llm, got)
		}
		if !slices.ContainsFunc(a.Carried, func(c string) bool {
			return strings.Contains(c, "接着用以前在 new-api 里给它的令牌（编号 1）")
		}) {
			t.Fatalf("llm=%v: carried %v", llm, a.Carried)
		}
		if llm == nil && !slices.ContainsFunc(a.Carried, func(c string) bool { return c == "大模型沿用原来的：本机的 new-api" }) {
			t.Fatalf("carried %v", a.Carried)
		}
		toks, err := v.e.GatewayTokens(ctx, "hermes-agent")
		if err != nil || !slices.Equal(toks, []TokenRef{old}) {
			t.Fatalf("saved tokens %v %v", toks, err)
		}
		// deleted with its data now: the API server revokes the token
		if err := v.w.delete(ctx, "hermes-agent", true); err != nil {
			t.Fatal(err)
		}
		if err := v.e.RevokeGatewayTokens(ctx, "hermes-agent", toks); err != nil || !slices.Equal(v.newapi.deleted, []int{1}) {
			t.Fatalf("revoke %v %v", err, v.newapi.deleted)
		}
	}
}

// The old gateway is gone: without a model source in the request the
// install is refused (CAT-INST-02) and says which one it was.
func TestReuseGatewayGoneRefused(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	keepData(t, v, "hermes-agent")
	if err := v.w.delete(ctx, "new-api", true); err != nil {
		t.Fatal(err)
	}
	_, err := v.e.StartInstall(ctx, Request{App: "hermes-agent", ReuseData: true})
	if code(err) != CodeBadParams || !strings.Contains(err.Error(), "原来用本机的 new-api") {
		t.Fatalf("got %v", err)
	}
	// choosing one now goes ahead
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Params: ownLLM()})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
}

// What v0.5.0 did on delete (revoke the token) or another model chosen now:
// the platform's part of config.yaml changed, so it is written anew and the
// old file is copied aside first; a token of the old app that is not taken
// over is revoked once the install succeeded.
func TestReuseChangedSettingsRewriteWithBackup(t *testing.T) {
	t.Run("old token revoked", func(t *testing.T) {
		v := setup(t)
		ctx := context.Background()
		installHermes(t, v)
		vol := hermesVol(t, v)
		orig, _ := v.w.volFile(vol, "config.yaml")
		v.w.editVolFile(vol, "config.yaml", orig+marker)
		toks, _ := v.e.GatewayTokens(ctx, "hermes-agent")
		if err := v.e.RevokeGatewayTokens(ctx, "hermes-agent", toks); err != nil { // as v0.5.0 deleted it
			t.Fatal(err)
		}
		keepData(t, v, "hermes-agent")
		tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
		if tv.Status != state.TaskSucceeded {
			t.Fatal(tv.Fault)
		}
		a := tv.Apps[0]
		if a.GatewayToken == nil || a.GatewayToken.ID != 2 || a.TakenToken != nil {
			t.Fatalf("app %+v", a)
		}
		checkBackup(t, v, tv, vol, orig+marker, "params.llm.api_key")
		if !slices.ContainsFunc(a.Carried, func(c string) bool { return strings.Contains(c, "已经没有了") }) {
			t.Fatalf("carried %v", a.Carried)
		}
	})
	t.Run("own key chosen now", func(t *testing.T) {
		v := setup(t)
		installHermes(t, v)
		vol := hermesVol(t, v)
		orig, _ := v.w.volFile(vol, "config.yaml")
		v.w.editVolFile(vol, "config.yaml", orig+marker)
		keepData(t, v, "hermes-agent")
		tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Params: ownLLM()})
		if tv.Status != state.TaskSucceeded {
			t.Fatal(tv.Fault)
		}
		checkBackup(t, v, tv, vol, orig+marker, "params.llm.api_key、params.llm.base_url、params.llm.model")
		if !slices.Equal(v.newapi.deleted, []int{1}) {
			t.Fatalf("the old token not revoked: %v", v.newapi.deleted)
		}
		if !slices.ContainsFunc(tv.Apps[0].Carried, func(c string) bool { return strings.Contains(c, "不再用了，已吊销") }) {
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
// record and revokes its token; a token number the gateway now uses for
// another value (set up anew) is left alone.
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
			v.newapi.mu.Lock()
			v.newapi.salt = "-new"
			v.newapi.mu.Unlock()
		}
		tv := v.install(t, Request{App: "hermes-agent", Params: map[string]any{"llm": "local"}})
		if tv.Status != state.TaskSucceeded {
			t.Fatal(tv.Fault)
		}
		if _, err := v.w.st.GetKeptCatalogSecrets(ctx, "hermes-agent"); err == nil {
			t.Fatal("stale kept record left")
		}
		want := []int{1}
		if reinstalled {
			want = nil
		}
		if !slices.Equal(v.newapi.deleted, want) {
			t.Fatalf("reinstalled=%v: deleted %v, want %v", reinstalled, v.newapi.deleted, want)
		}
	}
}

// Old data without a kept record: an existing config file is kept, and the
// progress says no settings were found.
func TestReuseWithoutKeptRecordKeepsFile(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	v.w.adopted[hermesDigestForTest] = true
	if tv := v.install(t, Request{App: "hermes-agent", Params: ownLLM()}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	vol := hermesVol(t, v)
	orig, _ := v.w.volFile(vol, "config.yaml")
	v.w.editVolFile(vol, "config.yaml", orig+marker)
	keepData(t, v, "hermes-agent")
	if err := v.w.st.DeleteKeptCatalogSecrets(ctx, "hermes-agent"); err != nil {
		t.Fatal(err)
	}
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Params: ownLLM()})
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

// A failed install on old data keeps the kept record and leaves the token
// it took over alone.
func TestReuseFailureKeepsTakenToken(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	keepData(t, v, "hermes-agent")
	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = true
	v.w.mu.Unlock()
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskFailed || tv.Apps[0].TakenToken == nil {
		t.Fatalf("should fail after taking the token over: %+v", tv)
	}
	if len(v.newapi.deleted) != 0 {
		t.Fatalf("the old token was revoked by the failed install: %v", v.newapi.deleted)
	}
	if _, err := v.w.st.GetKeptCatalogSecrets(ctx, "hermes-agent"); err != nil {
		t.Fatal("kept record lost by a failed install")
	}
	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = false
	v.w.mu.Unlock()
	if tv := v.install(t, Request{App: "hermes-agent", ReuseData: true}); tv.Status != state.TaskSucceeded || tv.Apps[0].TakenToken == nil {
		t.Fatalf("retry %+v", tv)
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

// The old token is checked through its masked value (not the key endpoint
// New API limits), and one the user turned off is taken over as it is, with
// a note.
func TestReuseTakesOverDisabledToken(t *testing.T) {
	v := setup(t)
	installHermes(t, v)
	keepData(t, v, "hermes-agent")
	v.newapi.mu.Lock()
	v.newapi.disabled = map[int]bool{1: true}
	calls := v.newapi.keyCalls
	v.newapi.mu.Unlock()
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskSucceeded || tv.Apps[0].TakenToken == nil || tv.Apps[0].GatewayToken != nil {
		t.Fatalf("%+v", tv)
	}
	if v.newapi.keyCalls != calls {
		t.Fatalf("the key endpoint was called %d times", v.newapi.keyCalls-calls)
	}
	if !slices.ContainsFunc(tv.Apps[0].Carried, func(c string) bool { return strings.Contains(c, "现在没有启用（状态 2）") }) {
		t.Fatalf("carried %v", tv.Apps[0].Carried)
	}
}

func TestMaskedTokenMatch(t *testing.T) {
	full := "sk-abcdEFGH0123456789012345678901234567890123wxyz"
	for _, c := range []struct {
		masked string
		want   bool
	}{
		{"abcd**********wxyz", true},
		{"sk-abcd**********wxyz", true},
		{"abce**********wxyz", false},
		{"abcd**********wxya", false},
		{"ab****yz", false}, // too little shown to tell
		{strings.TrimPrefix(full, "sk-"), true},
		{"abcdEFGH", false},
		{"", false},
	} {
		if got := maskedTokenMatch(c.masked, full); got != c.want {
			t.Errorf("%q: %v", c.masked, got)
		}
	}
	if maskedTokenMatch("abcd**********wxyz", "") {
		t.Error("empty full value matched")
	}
}

// setDesired plays acornfox stop / start on app.
func setDesired(t *testing.T, v *env, app, desired string) {
	t.Helper()
	if _, err := v.w.st.UpdateApp(context.Background(), app, func(a *state.App) error { a.Desired = desired; return nil }); err != nil {
		t.Fatal(err)
	}
}

// The old app's gateway is installed but stopped: the install is refused
// before a task starts (CAT-INST-10, "start it"), not CAT-INST-02 ("choose
// again"), and not later at the create step with advice to install another
// New API. The same for --llm local, on old data or not. Started again, the
// install goes ahead and takes the old token over.
func TestReuseGatewayStoppedRefused(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	keepData(t, v, "hermes-agent")
	setDesired(t, v, "new-api", state.DesiredStopped)
	for _, req := range []Request{
		{App: "hermes-agent", ReuseData: true},
		{App: "hermes-agent", ReuseData: true, Params: map[string]any{"llm": "local"}},
		{App: "hermes-agent", Name: "h2", Params: map[string]any{"llm": "local"}},
	} {
		_, err := v.e.StartInstall(ctx, req)
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeNoGateway || !strings.Contains(f.Message, "装着但停着") ||
			!strings.Contains(f.Message, "acornfox start new-api") || !strings.Contains(f.Hint, "acornfox start new-api") ||
			strings.Contains(f.Hint, "acornfox install new-api") {
			t.Fatalf("%+v: got %v (hint %q)", req, err, hintOf(err))
		}
		if req.ReuseData && req.Params == nil && !strings.Contains(f.Message, "hermes-agent 原来用本机的 new-api，它装着但停着") {
			t.Fatalf("message %q", f.Message)
		}
	}
	if tasks, _ := v.w.st.ListInstallTasks(ctx, "", 50); len(tasks) != 2 { // new-api and the first hermes
		t.Fatalf("a refused install made a task: %d tasks", len(tasks))
	}
	setDesired(t, v, "new-api", state.DesiredRunning)
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskSucceeded || tv.Apps[0].TakenToken == nil {
		t.Fatalf("after start: %+v", tv)
	}
}

func hintOf(err error) string {
	var f *Fault
	if errors.As(err, &f) {
		return f.Hint
	}
	return ""
}

// A token of the old app that is not taken over cannot be revoked while its
// gateway is stopped (or deleted with its data kept): the install still
// succeeds and the kept record goes, but the progress (the init step, which
// the page shows) and carried say which token to remove by hand. A gateway
// deleted with its data took its tokens with it: nothing is said.
func TestReuseTokenNotRevokedIsSaid(t *testing.T) {
	for _, c := range []struct {
		name, why string
		down      func(t *testing.T, v *env)
	}{
		{"stopped", "new-api 停着", func(t *testing.T, v *env) { setDesired(t, v, "new-api", state.DesiredStopped) }},
		{"deleted, data kept", "new-api 已经删了，它的数据还留着", func(t *testing.T, v *env) { keepData(t, v, "new-api") }},
		{"deleted with data", "", func(t *testing.T, v *env) {
			t.Helper()
			if err := v.w.delete(context.Background(), "new-api", true); err != nil {
				t.Fatal(err)
			}
			for _, vol := range append([]string(nil), v.w.volumes["new-api"]...) {
				_ = v.w.RemoveVolume(context.Background(), "new-api", vol)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := setup(t)
			ctx := context.Background()
			installHermes(t, v)
			keepData(t, v, "hermes-agent")
			c.down(t, v)
			tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Params: ownLLM()})
			if tv.Status != state.TaskSucceeded {
				t.Fatal(tv.Fault)
			}
			if _, err := v.w.st.GetKeptCatalogSecrets(ctx, "hermes-agent"); err == nil {
				t.Fatal("kept record left")
			}
			if len(v.newapi.deleted) != 0 {
				t.Fatalf("revoked %v", v.newapi.deleted)
			}
			a := tv.Apps[0]
			initStep := stepOf(tv, "hermes-agent", "init")
			if c.why == "" {
				if slices.ContainsFunc(a.Carried, func(s string) bool { return strings.Contains(s, "令牌") && strings.Contains(s, "吊销") }) || strings.Contains(initStep.Message, "吊销") {
					t.Fatalf("said something about a token that is gone: %v / %q", a.Carried, initStep.Message)
				}
				return
			}
			want := "没能吊销以前在 new-api 里给 hermes-agent 的令牌（编号 1，结尾 xxxx）：" + c.why
			if !slices.ContainsFunc(a.Carried, func(s string) bool { return strings.HasPrefix(s, want) }) {
				t.Fatalf("carried %v, want %q", a.Carried, want)
			}
			if !strings.Contains(initStep.Message, want) || !strings.HasSuffix(initStep.Message, "acornfox-hermes-agent、结尾是 xxxx 的那一把") || initStep.Status != StepSkipped {
				t.Fatalf("init step %+v", initStep)
			}
			// The task as read says it is over and hands the line on as a
			// must-tell, word for word.
			if tm := tv.Timing; tm == nil || !tm.Done || tm.Clock != "server" || !slices.ContainsFunc(tm.MustTell, func(s string) bool { return strings.HasPrefix(s, want) }) {
				t.Fatalf("timing %+v", tv.Timing)
			}
		})
	}
}

// A token whose value was not kept cannot be told from another app's under
// the same number: not revoked, said. A new token of the same name was issued
// in the same gateway this time: the note says not to remove that one.
func TestReuseTokenWithoutValueIsSaid(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	installHermes(t, v)
	saved, _ := v.e.savedParams(ctx, "hermes-agent")
	llm, _ := saved["llm"].(map[string]any)
	llm["api_key"] = ""
	resealParams(t, v, "hermes-agent", saved)
	keepData(t, v, "hermes-agent")
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Params: map[string]any{"llm": "local"}})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if len(v.newapi.deleted) != 0 || tv.Apps[0].GatewayToken == nil || tv.Apps[0].GatewayToken.ID != 2 {
		t.Fatalf("revoked without its value %v, or no new token %+v", v.newapi.deleted, tv.Apps[0])
	}
	if !slices.ContainsFunc(tv.Apps[0].Carried, func(s string) bool {
		return strings.HasPrefix(s, "没能吊销以前在 new-api 里给 hermes-agent 的令牌（编号 1）：没有记下它的值") &&
			strings.HasSuffix(s, "acornfox-hermes-agent、编号 1 的那一把（这次新发给它的那把同名，不要删）")
	}) {
		t.Fatalf("carried %v", tv.Apps[0].Carried)
	}
}

// An install on old data that rewrote config.yaml (another model chosen)
// and then failed leaves that attempt's file in the data, while the kept
// record still holds the old settings. Installing again with the old
// settings must not take the file for the old app's: it is written anew
// (and that attempt's copied aside), not "kept as it was".
func TestReuseAfterFailedRewrite(t *testing.T) {
	v := setup(t)
	installHermes(t, v)
	vol := hermesVol(t, v)
	orig, _ := v.w.volFile(vol, "config.yaml")
	v.w.editVolFile(vol, "config.yaml", orig+marker)
	keepData(t, v, "hermes-agent")

	v.w.mu.Lock()
	v.w.failDeploy["hermes-agent"] = true
	v.w.mu.Unlock()
	tv := v.install(t, Request{App: "hermes-agent", ReuseData: true, Params: ownLLM()})
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

	tv = v.install(t, Request{App: "hermes-agent", ReuseData: true})
	if tv.Status != state.TaskSucceeded || tv.Apps[0].TakenToken == nil {
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
	if strings.Contains(got, "sk-own") || !strings.Contains(got, "K1") {
		t.Fatalf("config.yaml should hold the old token again:\n%s", got)
	}
	if msg := stepOf(tv, "hermes-agent", "files").Message; !strings.Contains(msg, "上次接旧数据没装好时按那次的设置改写过") {
		t.Fatalf("files step %q", msg)
	}
}
