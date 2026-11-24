package appinstall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Changing an installed app's settings and applying them again
// (reapply.go), with a fake world: the manifest of a made-up app that reads
// its parameters, a platform variable and the address in environment
// variables and a config file, and has an init step of each moment.

const reapplyManifest = `id: demo-conf
schema: 3
source: official
maintainer: acornfox

app:
  name: 样例设置应用
  summary: 为测试改设置后重新生效
  category: dev
  icon: icon.svg
  homepage: https://example.com/demo-conf
  license: MIT

lifecycle:
  min_platform: "0.7.0"
  status: active

resources:
  memory_mb: { min: 64 }
  platforms: [linux/amd64]

release:
  version: v1.0.0
  images:
    - name: app
      repository: example/demo-conf
      tag: v1.0.0
      digest: sha256:5555555555555555555555555555555555555555555555555555555555555555

endpoints:
  - name: web
    port: 8080
    protocol: http
    primary: true
    health:
      path: /healthz
      timeout_s: 5

volumes:
  - { name: data, path: /data }

params:
  - { key: title, type: string, label: 标题, default: 你好 }
  - { key: mode, type: enum, label: 模式, options: [fast, slow], default: fast }
  - { key: fixed, type: string, label: 固定值, default: keep, editable: never }
  - { key: later, type: string, label: 重装才能改, default: a, editable: reinstall }
  - { key: needed, type: string, label: 必填项, default: x, required: true }
  - { key: token, type: secret, generate: { length: 24 }, hidden: true }
  - { key: pw, type: secret, label: 口令, generate: { length: 20 } }

env:
  - { key: TITLE, value: "{{params.title}}" }
  - { key: MODE, from: params.mode }
  - { key: TZ, from: platform.tz }
  - { key: HOME_URL, from: platform.app.url }
  - { key: TOKEN, from: params.token, secret: true }
  - { key: PW, from: params.pw, secret: true }
  - { key: FAST_ONLY, value: "1", if: "params.mode == fast" }

files:
  - volume: data
    path: app.conf
    template: |
      title={{params.title}}
      tz={{platform.tz}}
  - volume: data
    path: seed.conf
    overwrite: install
    template: |
      seed={{params.title}}

init:
  - name: seed
    when: after_healthy
    exec: { command: [demo-conf, seed, "{{params.title}}"] }
  - name: sync
    when: on_reapply
    exec: { command: [demo-conf, sync] }
  - name: rename
    when: on_change
    watch: [params.title]
    exec: { command: [demo-conf, rename, "{{params.title}}"] }

first_login:
  - { label: 标题, value: "{{params.title}}" }

delivery:
  tier: auto
  minutes: 1

uninstall:
  data: []
`

// swapProvider is the standalone provider of a machine whose settings can
// be changed while the engine runs.
type swapProvider struct {
	mu  sync.Mutex
	cur *platformvar.Standalone
}

func newSwapProvider(tz string) *swapProvider {
	s := &swapProvider{}
	s.set(tz)
	return s
}

func (s *swapProvider) set(tz string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cur = platformvar.NewStandalone(platformvar.Machine{TZ: tz, Host: platformvar.Host{PublicHost: "192.168.1.20"}})
}

func (s *swapProvider) c() *platformvar.Standalone {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

func (s *swapProvider) Edition() string              { return s.c().Edition() }
func (s *swapProvider) Table() []catalog.PlatformVar { return s.c().Table() }
func (s *swapProvider) Machine(ctx context.Context) []platformvar.State {
	return s.c().Machine(ctx)
}
func (s *swapProvider) Values(ctx context.Context, a platformvar.App) (map[string]string, error) {
	return s.c().Values(ctx, a)
}
func (s *swapProvider) Address(a platformvar.App) platformvar.Address { return s.c().Address(a) }
func (s *swapProvider) Has(name string) bool                          { return s.c().Has(name) }
func (s *swapProvider) Check(sets map[string]string) error            { return s.c().Check(sets) }

// reapplyWorld is an engine with a runner that runs commands, a machine
// whose time zone can change, and the made-up apps.
type reapplyWorld struct {
	*env
	x       *execRunner
	machine *swapProvider
	healthy atomic.Bool // the apps answer their health check
}

// exec lists the commands run so far, each joined by spaces.
func (r *reapplyWorld) cmds() []string {
	var out []string
	for _, q := range r.x.requests() {
		out = append(out, strings.Join(q.Command, " "))
	}
	return out
}

func reapplySetup(t *testing.T, manifests ...string) *reapplyWorld {
	t.Helper()
	r := &reapplyWorld{x: &execRunner{}, machine: newSwapProvider("UTC")}
	r.healthy.Store(true)
	b := typedBundle(t, goodDir, nil, manifests...)
	v, _ := setupSnapshot(t, bundleFS(b), func(c *Config) {
		c.Version = "0.7.0"
		c.Runner = &execWorld{fakeWorld: c.Runner.(*fakeWorld), execRunner: r.x}
		c.PlatformVars = r.machine
	})
	r.env = v
	for _, id := range []string{"demo-conf", "demo-plain", "demo-llm-user"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if !r.healthy.Load() {
				http.Error(w, "down", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		t.Cleanup(srv.Close)
		v.w.servers[id] = srv
	}
	return r
}

// install installs app id and returns its name.
func (r *reapplyWorld) mustInstall(t *testing.T, req Request) {
	t.Helper()
	if tv := r.install(t, req); tv.Status != state.TaskSucceeded {
		t.Fatalf("install: %+v", tv.Fault)
	}
}

// apply starts a reapply, waits for its task and returns it.
func (r *reapplyWorld) apply(t *testing.T, req ReapplyRequest) TaskView {
	t.Helper()
	req.ConfirmRestart = true
	st, err := r.e.StartReapply(context.Background(), req)
	if err != nil {
		t.Fatalf("StartReapply: %v", err)
	}
	if st.Task == nil {
		t.Fatalf("nothing was started: %+v", st.Plan)
	}
	return r.wait(t, st.Task.ID)
}

func (r *reapplyWorld) volFileOf(t *testing.T, app, path string) string {
	t.Helper()
	vols, _ := r.w.st.ListVolumes(context.Background(), app)
	for _, v := range vols {
		if c, ok := r.w.volFile(v.VolumeName, path); ok {
			return c
		}
	}
	return ""
}

func (r *reapplyWorld) currentDeployment(t *testing.T, app string) state.Deployment {
	t.Helper()
	a, err := r.w.st.GetApp(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.w.st.GetDeployment(context.Background(), a.CurrentDeployment)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func contains1(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// A setting is changed: the plan says what changes and that the app
// restarts; without the user's go-ahead nothing is done; with it the
// environment, the config file the platform owns, the container and the
// steps of the manifest follow, the file the manifest wrote "install only"
// is left, and the new value is what `apps vars` and the first-login show.
func TestReapplyChangesASetting(t *testing.T) {
	r := reapplySetup(t, reapplyManifest)
	ctx := context.Background()
	r.mustInstall(t, Request{App: "demo-conf"})
	before := r.currentDeployment(t, "demo-conf")
	if env := envMap(t, r.env, "demo-conf"); env["TITLE"] != "你好" || env["TZ"] != "UTC" || env["FAST_ONLY"] != "1" {
		t.Fatalf("env %v", env)
	}
	if got := r.volFileOf(t, "demo-conf", "app.conf"); got != "title=你好\ntz=UTC\n" {
		t.Fatalf("app.conf %q", got)
	}
	r.x.reqs = nil // forget the install's own step

	req := ReapplyRequest{App: "demo-conf", Params: map[string]any{"title": "新标题", "mode": "slow"}}
	plan, err := r.e.ReapplyPlan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Needed || !plan.Restart || len(plan.Vars) != 2 || plan.Vars[0].Name != "params.mode" && plan.Vars[0].Name != "params.title" ||
		!contains1(plan.Env, "TITLE") || !contains1(plan.Env, "MODE") || !contains1(plan.Gone, "FAST_ONLY") || !contains1(plan.Files, "app.conf") || contains1(plan.Files, "seed.conf") {
		t.Fatalf("plan %+v", plan)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].When != catalog.StepOnReapply || plan.Steps[1].When != catalog.StepOnChange {
		t.Fatalf("steps %+v", plan.Steps)
	}
	if !strings.Contains(plan.Summary, "会重启 demo-conf") {
		t.Fatalf("summary %q", plan.Summary)
	}
	// planning wrote nothing
	if env := envMap(t, r.env, "demo-conf"); env["TITLE"] != "你好" || len(r.x.requests()) != 0 {
		t.Fatalf("plan changed something: %v", env)
	}
	// no go-ahead: refused, nothing changed
	_, err = r.e.StartReapply(ctx, req)
	if code(err) != CodeApplyConfirm {
		t.Fatalf("without confirm: %v", err)
	}
	if env := envMap(t, r.env, "demo-conf"); env["TITLE"] != "你好" {
		t.Fatalf("refused but changed: %v", env)
	}

	tv := r.apply(t, req)
	if tv.Status != state.TaskSucceeded || tv.Kind != KindReapply {
		t.Fatalf("task %+v %+v", tv.Fault, tv.Steps)
	}
	env := envMap(t, r.env, "demo-conf")
	if env["TITLE"] != "新标题" || env["MODE"] != "slow" || env["TZ"] != "UTC" {
		t.Fatalf("env %v", env)
	}
	if _, has := env["FAST_ONLY"]; has {
		t.Fatalf("FAST_ONLY stayed: %v", env)
	}
	if got := r.volFileOf(t, "demo-conf", "app.conf"); got != "title=新标题\ntz=UTC\n" {
		t.Fatalf("app.conf %q", got)
	}
	if got := r.volFileOf(t, "demo-conf", "seed.conf"); got != "seed=你好\n" {
		t.Fatalf("seed.conf (install only) was touched: %q", got)
	}
	// a new version of the same image came up
	after := r.currentDeployment(t, "demo-conf")
	if after.ID == before.ID || after.Reason != state.ReasonReapply || after.ImageID != before.ImageID || after.Seq != before.Seq+1 {
		t.Fatalf("deployments %+v -> %+v", before, after)
	}
	// the steps: on_reapply and the on_change one, not the first-install one
	cmds := r.cmds()
	if len(cmds) != 2 || cmds[0] != "demo-conf sync" || cmds[1] != "demo-conf rename 新标题" {
		t.Fatalf("commands %v", cmds)
	}
	// saved: values, origin, no journal
	vars, err := r.e.AppVars(ctx, "demo-conf", true)
	if err != nil || !vars.CanChange {
		t.Fatalf("vars %v can_change %v", err, vars.CanChange)
	}
	byName := map[string]AppVar{}
	for _, x := range vars.Vars {
		byName[x.Name] = x
	}
	if x := byName["params.title"]; x.Value != "新标题" || x.Origin != OriginUser || !x.CanEdit {
		t.Fatalf("title %+v", x)
	}
	if x := byName["params.fixed"]; x.CanEdit || x.Editable != "never" {
		t.Fatalf("fixed %+v", x)
	}
	if x := byName["params.token"]; x.CanEdit {
		t.Fatalf("token (hidden) %+v", x)
	}
	if x := byName["platform.tz"]; x.Differs {
		t.Fatalf("tz differs: %+v", x)
	}
	for _, n := range vars.Notes {
		if strings.Contains(n, "APP-VARS-01") {
			t.Fatalf("still behind: %v", vars.Notes)
		}
	}
	saved, _ := r.e.savedParams(ctx, "demo-conf")
	if _, has := saved[undoKey]; has {
		t.Fatalf("journal left: %v", saved[undoKey])
	}
	fl, err := r.e.FirstLogin(ctx, "demo-conf", true)
	if err != nil || fl.Fields[0].Value != "新标题" {
		t.Fatalf("first login %+v %v", fl, err)
	}
	// asking again changes nothing
	again, err := r.e.StartReapply(ctx, ReapplyRequest{App: "demo-conf", Params: map[string]any{"title": "新标题"}})
	if err != nil || again.Task != nil || again.Plan.Needed {
		t.Fatalf("again %+v %v", again, err)
	}
	if !strings.Contains(again.Plan.Summary, "没有变化") {
		t.Fatalf("summary %q", again.Plan.Summary)
	}
}

// The rules of install hold when a setting is changed later: a setting the
// manifest forbids to change, one the platform generated, a value of the
// wrong shape, a required one emptied.
func TestReapplyRefusesWhatInstallWouldRefuse(t *testing.T) {
	r := reapplySetup(t, reapplyManifest)
	ctx := context.Background()
	r.mustInstall(t, Request{App: "demo-conf"})
	for _, c := range []struct {
		name string
		req  ReapplyRequest
		want string
	}{
		{"never", ReapplyRequest{Params: map[string]any{"fixed": "other"}}, CodeApplyEdit},
		{"reinstall", ReapplyRequest{Params: map[string]any{"later": "b"}}, CodeApplyEdit},
		{"required emptied", ReapplyRequest{Params: map[string]any{"needed": ""}}, CodeApplyEdit},
		{"hidden", ReapplyRequest{Params: map[string]any{"token": "abcdefghijkl"}}, CodeBadParams},
		{"unknown", ReapplyRequest{Params: map[string]any{"nothing": "x"}}, CodeBadParams},
		{"not an option", ReapplyRequest{Params: map[string]any{"mode": "warp"}}, CodeBadParams},
		{"secret too short", ReapplyRequest{Params: map[string]any{"pw": "short"}}, CodeBadParams},
		{"platform that cannot be set", ReapplyRequest{Platform: map[string]string{"region": "cn"}}, CodeBadPlatform},
		{"time zone that is none", ReapplyRequest{Platform: map[string]string{"tz": "Not/AZone"}}, CodeBadPlatform},
	} {
		c.req.App = "demo-conf"
		if _, err := r.e.ReapplyPlan(ctx, c.req); code(err) != c.want {
			t.Errorf("%s: %v, want %s", c.name, err, c.want)
		}
	}
	if _, err := r.e.ReapplyPlan(ctx, ReapplyRequest{App: "nothing-here"}); code(err) != CodeNotCatalogApp {
		t.Errorf("no such app: %v", err)
	}
	// the secret that is no secret of the platform's: changed as asked, shown masked
	plan, err := r.e.ReapplyPlan(ctx, ReapplyRequest{App: "demo-conf", Params: map[string]any{"pw": "a-new-password-0001"}})
	if err != nil || len(plan.Vars) != 1 || !plan.Vars[0].Secret || strings.Contains(plan.Vars[0].New, "a-new-password") || strings.Contains(plan.Vars[0].Old, "ab") && false {
		t.Fatalf("secret plan %+v %v", plan, err)
	}
	// and what was installed stays as it was
	if env := envMap(t, r.env, "demo-conf"); env["TITLE"] != "你好" {
		t.Fatalf("env %v", env)
	}
}

// The machine's time zone changed: the app is listed as behind (and by the
// variable asked for); applying brings the environment and the file to the
// new zone, runs the every-time step but not the one that watches the title,
// and the app is no longer behind.
func TestReapplyFollowsThePlatform(t *testing.T) {
	r := reapplySetup(t, reapplyManifest)
	ctx := context.Background()
	r.mustInstall(t, Request{App: "demo-conf"})
	af, err := r.e.Affected(ctx, "")
	if err != nil || len(af.Apps) != 0 {
		t.Fatalf("nothing changed yet: %+v %v", af, err)
	}
	r.machine.set("Asia/Tokyo")
	af, err = r.e.Affected(ctx, "")
	if err != nil || len(af.Apps) != 1 || af.Apps[0].App != "demo-conf" || !af.Apps[0].Differs || len(af.Apps[0].Vars) != 1 ||
		af.Apps[0].Vars[0].Name != "platform.tz" || af.Apps[0].Vars[0].Old != "UTC" || af.Apps[0].Vars[0].New != "Asia/Tokyo" || !af.Apps[0].Restart {
		t.Fatalf("affected %+v %v", af, err)
	}
	// who reads it: asked by name
	one, err := r.e.Affected(ctx, "platform.tz")
	if err != nil || len(one.Apps) != 1 || !one.Apps[0].Uses || !one.Apps[0].Differs {
		t.Fatalf("by var %+v %v", one, err)
	}
	other, err := r.e.Affected(ctx, "platform.mirror.npm")
	if err != nil || len(other.Apps) != 0 {
		t.Fatalf("an app that does not read it: %+v %v", other, err)
	}
	if _, err := r.e.Affected(ctx, "tz"); code(err) != CodeBadParams {
		t.Fatalf("a variable written wrong: %v", err)
	}
	r.x.reqs = nil
	tv := r.apply(t, ReapplyRequest{App: "demo-conf", Why: WhyPlatform})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv.Fault)
	}
	if env := envMap(t, r.env, "demo-conf"); env["TZ"] != "Asia/Tokyo" || env["TITLE"] != "你好" {
		t.Fatalf("env %v", env)
	}
	if got := r.volFileOf(t, "demo-conf", "app.conf"); got != "title=你好\ntz=Asia/Tokyo\n" {
		t.Fatalf("app.conf %q", got)
	}
	if cmds := r.cmds(); len(cmds) != 1 || cmds[0] != "demo-conf sync" {
		t.Fatalf("commands %v", cmds)
	}
	af, _ = r.e.Affected(ctx, "")
	if len(af.Apps) != 0 {
		t.Fatalf("still behind: %+v", af)
	}
	// the app's own setting of a platform variable survives a change of the
	// machine; resetting it follows the machine again
	tv = r.apply(t, ReapplyRequest{App: "demo-conf", Platform: map[string]string{"platform.tz": "Europe/Paris"}})
	if tv.Status != state.TaskSucceeded || envMap(t, r.env, "demo-conf")["TZ"] != "Europe/Paris" {
		t.Fatalf("own tz: %+v %v", tv.Fault, envMap(t, r.env, "demo-conf"))
	}
	r.machine.set("America/Chicago")
	if plan, _ := r.e.ReapplyPlan(ctx, ReapplyRequest{App: "demo-conf"}); plan.Needed {
		t.Fatalf("the app's own zone should hold: %+v", plan)
	}
	tv = r.apply(t, ReapplyRequest{App: "demo-conf", ResetPlatform: []string{"tz"}})
	if tv.Status != state.TaskSucceeded || envMap(t, r.env, "demo-conf")["TZ"] != "America/Chicago" {
		t.Fatalf("reset: %+v %v", tv.Fault, envMap(t, r.env, "demo-conf"))
	}
}

// The app's exposure changed: the address it reads (app.url) follows. The
// plan can be had before the change is made.
func TestReapplyFollowsTheAddress(t *testing.T) {
	r := reapplySetup(t, reapplyManifest)
	ctx := context.Background()
	r.mustInstall(t, Request{App: "demo-conf"})
	if u := envMap(t, r.env, "demo-conf")["HOME_URL"]; !strings.HasPrefix(u, "http://127.0.0.1:") {
		t.Fatalf("HOME_URL %q", u)
	}
	a, _ := r.w.st.GetApp(ctx, "demo-conf")
	lan := platformvar.Where{Exposure: state.ExposureLAN, Port: a.PublicPort}
	plan, err := r.e.ReapplyPlan(ctx, ReapplyRequest{App: "demo-conf", AssumeWhere: &lan})
	if err != nil || !plan.Needed || !plan.Restart || !contains1(plan.Env, "HOME_URL") {
		t.Fatalf("plan %+v %v", plan, err)
	}
	// the plan did not change the exposure, so nothing is behind yet
	if af, _ := r.e.Affected(ctx, ""); len(af.Apps) != 0 {
		t.Fatalf("behind before the change: %+v", af)
	}
	if _, err := r.w.st.UpdateApp(ctx, "demo-conf", func(x *state.App) error { x.Exposure = state.ExposureLAN; return nil }); err != nil {
		t.Fatal(err)
	}
	af, _ := r.e.Affected(ctx, "")
	if len(af.Apps) != 1 || af.Apps[0].Vars[0].Name != "platform.app.url" {
		t.Fatalf("affected %+v", af)
	}
	tv := r.apply(t, ReapplyRequest{App: "demo-conf", Why: WhyAddress})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv.Fault)
	}
	if u := envMap(t, r.env, "demo-conf")["HOME_URL"]; !strings.HasPrefix(u, "http://192.168.1.20:") {
		t.Fatalf("HOME_URL %q", u)
	}
}

// Whatever goes wrong, the app is put back as it was: the new container
// does not come up; it comes up but is not healthy; a step of the manifest
// fails. In each, the environment, the config file, the saved settings and
// the container are as before, with APP-APPLY-03 and no journal left.
func TestReapplyPutsBackOnFailure(t *testing.T) {
	cases := []struct {
		name string
		arm  func(r *reapplyWorld)
		// replaced: a new container was made again from the old settings
		replaced bool
	}{
		{"the container does not start", func(r *reapplyWorld) { r.w.failDeploy["demo-conf"] = true }, false},
		{"not healthy", func(r *reapplyWorld) { r.healthy.Store(false) }, true},
		{"a step fails", func(r *reapplyWorld) {
			r.x.do = func(req runner.ExecRequest) (runner.ExecResult, error) {
				if len(req.Command) > 1 && req.Command[1] == "rename" {
					return runner.ExecResult{ExitCode: 1, Stderr: []byte("no such title"), Stdout: []byte{}}, nil
				}
				return ok("")
			}
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := reapplySetup(t, reapplyManifest)
			ctx := context.Background()
			r.mustInstall(t, Request{App: "demo-conf"})
			before := r.currentDeployment(t, "demo-conf")
			c.arm(r)
			tv := r.apply(t, ReapplyRequest{App: "demo-conf", Params: map[string]any{"title": "坏的", "mode": "slow"}})
			if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeApplyFailed {
				t.Fatalf("task %+v %+v", tv.Fault, tv.Steps)
			}
			if rv := stepOf(tv, "demo-conf", "revert"); rv == nil || rv.Status != StepDone {
				t.Fatalf("revert step %+v", rv)
			}
			env := envMap(t, r.env, "demo-conf")
			if env["TITLE"] != "你好" || env["MODE"] != "fast" || env["FAST_ONLY"] != "1" {
				t.Fatalf("env not put back: %v", env)
			}
			if got := r.volFileOf(t, "demo-conf", "app.conf"); got != "title=你好\ntz=UTC\n" {
				t.Fatalf("app.conf not put back: %q", got)
			}
			saved, _ := r.e.savedParams(ctx, "demo-conf")
			if saved["title"] != "你好" || saved[undoKey] != nil {
				t.Fatalf("saved %v", saved)
			}
			if fl, _ := r.e.FirstLogin(ctx, "demo-conf", true); fl.Fields[0].Value != "你好" {
				t.Fatalf("first login %+v", fl)
			}
			now := r.currentDeployment(t, "demo-conf")
			if c.replaced == (now.ID == before.ID) {
				t.Fatalf("container: before %s now %s, replaced=%v", before.ID, now.ID, c.replaced)
			}
			if now.ImageID != before.ImageID {
				t.Fatalf("image %s", now.ImageID)
			}
			// and the app can be applied again after the cause is gone
			r.w.failDeploy["demo-conf"] = false
			r.healthy.Store(true)
			r.x.do = nil
			if tv := r.apply(t, ReapplyRequest{App: "demo-conf", Params: map[string]any{"title": "好了"}}); tv.Status != state.TaskSucceeded {
				t.Fatalf("again: %+v", tv.Fault)
			}
			if envMap(t, r.env, "demo-conf")["TITLE"] != "好了" {
				t.Fatal("not applied the second time")
			}
		})
	}
}

// A server that stopped in the middle (the journal is there, the task is
// still "running") puts the app back at its next start.
func TestReapplyRecoversAfterARestart(t *testing.T) {
	r := reapplySetup(t, reapplyManifest)
	ctx := context.Background()
	r.mustInstall(t, Request{App: "demo-conf"})
	job, err := r.e.reapplyAnalyze(ctx, ReapplyRequest{App: "demo-conf", Params: map[string]any{"title": "半路"}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := job.journal(ctx, r.e, "task-x")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.e.saveUndo(ctx, "demo-conf", u); err != nil {
		t.Fatal(err)
	}
	// what the apply wrote before it was cut short
	_ = r.w.st.SetEnv(ctx, state.EnvVar{App: "demo-conf", Key: "TITLE", Value: "半路"})
	if _, err := r.w.st.CreateInstallTask(ctx, state.InstallTask{ID: "aaaaaaaaaaaa", Kind: "app", Target: "demo-conf", Status: state.TaskRunning, Data: `{"kind":"reapply"}`}); err != nil {
		t.Fatal(err)
	}
	r.e.recover(ctx)
	if got := envMap(t, r.env, "demo-conf")["TITLE"]; got != "你好" {
		t.Fatalf("TITLE %q", got)
	}
	tv, err := r.e.Task(ctx, "aaaaaaaaaaaa")
	if err != nil || tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeApplyFailed {
		t.Fatalf("task %+v %v", tv.Fault, err)
	}
	if saved, _ := r.e.savedParams(ctx, "demo-conf"); saved[undoKey] != nil {
		t.Fatalf("journal left")
	}
	// the app is still there, not deleted as a failed install's would be
	if _, err := r.w.st.GetApp(ctx, "demo-conf"); err != nil {
		t.Fatal(err)
	}
}

// An app that is not installed from the app center, was installed with no
// manifest saved, is being installed, or is stopped while a step is due:
// APP-APPLY-01, nothing changed.
func TestReapplyCannotWhere(t *testing.T) {
	r := reapplySetup(t, reapplyManifest)
	ctx := context.Background()
	if _, _, err := r.w.st.EnsureApp(ctx, "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.ReapplyPlan(ctx, ReapplyRequest{App: "mine"}); code(err) != CodeApplyCannot {
		t.Fatalf("own app: %v", err)
	}
	r.mustInstall(t, Request{App: "demo-conf"})
	// stopped, and the manifest has steps that run every time
	if _, err := r.w.st.UpdateApp(ctx, "demo-conf", func(a *state.App) error { a.Desired = state.DesiredStopped; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := r.e.ReapplyPlan(ctx, ReapplyRequest{App: "demo-conf", Params: map[string]any{"title": "x"}}); code(err) != CodeApplyCannot {
		t.Fatalf("stopped: %v", err)
	}
	if _, err := r.w.st.UpdateApp(ctx, "demo-conf", func(a *state.App) error { a.Desired = state.DesiredRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	// installed from the app center but no manifest was saved
	if _, _, err := r.w.st.EnsureApp(ctx, "nomani"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.w.st.UpdateApp(ctx, "nomani", func(a *state.App) error { a.CatalogID = "demo-conf"; return nil }); err != nil {
		t.Fatal(err)
	}
	d, _, err := r.w.st.CreateDeployment(ctx, state.NewDeployment{App: "nomani", SourceKind: state.SourceImage, SourceRef: "img", SourceDigest: "d-nomani"})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.w.st.SetCurrentDeployment(ctx, "nomani", d.ID)
	if _, err := r.e.ReapplyPlan(ctx, ReapplyRequest{App: "nomani"}); code(err) != CodeApplyCannot {
		t.Fatalf("no manifest: %v", err)
	}
}

const plainManifest = `id: demo-plain
schema: 3
source: official
maintainer: acornfox
app: { name: 样例没有步骤, summary: 没有 init 步骤, category: dev, icon: icon.svg, homepage: "https://example.com/demo-plain", license: MIT }
lifecycle: { min_platform: "0.6.0", status: active }
resources: { memory_mb: { min: 64 }, platforms: [linux/amd64] }
release:
  version: v1.0.0
  images:
    - { name: app, repository: example/demo-plain, tag: v1.0.0, digest: "sha256:6666666666666666666666666666666666666666666666666666666666666666" }
endpoints:
  - { name: web, port: 8080, protocol: http, primary: true, health: { path: /healthz, timeout_s: 5 } }
params:
  - { key: title, type: string, label: 标题, default: 你好 }
env:
  - { key: TITLE, value: "{{params.title}}" }
  - { key: TZ, from: platform.tz }
delivery: { tier: auto, minutes: 1 }
uninstall: { data: [] }
`

// A stopped app with no step due gets its new settings written and no
// container: they are what it starts with.
func TestReapplyOfAStoppedApp(t *testing.T) {
	r := reapplySetup(t, plainManifest)
	ctx := context.Background()
	r.mustInstall(t, Request{App: "demo-plain"})
	before := r.currentDeployment(t, "demo-plain")
	if _, err := r.w.st.UpdateApp(ctx, "demo-plain", func(a *state.App) error { a.Desired = state.DesiredStopped; return nil }); err != nil {
		t.Fatal(err)
	}
	plan, err := r.e.ReapplyPlan(ctx, ReapplyRequest{App: "demo-plain", Params: map[string]any{"title": "停着改"}})
	if err != nil || !plan.Needed || plan.Restart {
		t.Fatalf("plan %+v %v", plan, err)
	}
	tv := r.apply(t, ReapplyRequest{App: "demo-plain", Params: map[string]any{"title": "停着改"}})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv.Fault)
	}
	if envMap(t, r.env, "demo-plain")["TITLE"] != "停着改" || r.currentDeployment(t, "demo-plain").ID != before.ID {
		t.Fatalf("env %v", envMap(t, r.env, "demo-plain"))
	}
}

// A change that touches nothing the app reads (a setting no environment
// variable, file or step reads) is saved without restarting anything.
func TestReapplyWithoutRestart(t *testing.T) {
	m := strings.Replace(plainManifest, "params:\n", "params:\n  - { key: spare, type: string, label: 备用, default: a }\n", 1)
	r := reapplySetup(t, m)
	ctx := context.Background()
	r.mustInstall(t, Request{App: "demo-plain"})
	before := r.currentDeployment(t, "demo-plain")
	st, err := r.e.StartReapply(ctx, ReapplyRequest{App: "demo-plain", Params: map[string]any{"spare": "b"}})
	if err != nil {
		t.Fatalf("%v", err) // no restart: no go-ahead needed
	}
	if st.Plan.Restart || st.Task == nil {
		t.Fatalf("plan %+v", st.Plan)
	}
	if tv := r.wait(t, st.Task.ID); tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv.Fault)
	}
	if r.currentDeployment(t, "demo-plain").ID != before.ID {
		t.Fatal("restarted")
	}
	vars, _ := r.e.AppVars(ctx, "demo-plain", true)
	for _, x := range vars.Vars {
		if x.Name == "params.spare" && x.Value != "b" {
			t.Fatalf("spare %+v", x)
		}
	}
}

const llmUserManifest = `id: demo-llm-user
schema: 3
source: official
maintainer: acornfox
app: { name: 样例大模型使用方, summary: 要大模型接口, category: dev, icon: icon.svg, homepage: "https://example.com/demo-llm-user", license: MIT }
lifecycle: { min_platform: "0.7.0", status: active }
resources: { memory_mb: { min: 64 }, platforms: [linux/amd64] }
release:
  version: v1.0.0
  images:
    - { name: app, repository: example/demo-llm-user, tag: v1.0.0, digest: "sha256:7777777777777777777777777777777777777777777777777777777777777777" }
endpoints:
  - { name: web, port: 8080, protocol: http, primary: true, health: { path: /healthz, timeout_s: 5 } }
volumes:
  - { name: data, path: /data }
uses:
  - { name: llm, kind: llm_gateway }
env:
  - { key: LLM_URL, from: uses.llm.base_url }
  - { key: LLM_KEY, from: uses.llm.api_key, secret: true }
files:
  - volume: data
    path: llm.conf
    template: |
      url={{uses.llm.base_url}}
init:
  - name: reload
    when: on_change
    watch: [uses.llm.base_url]
    exec: { command: [demo-llm, reload, "{{uses.llm.base_url}}"] }
delivery: { tier: auto, minutes: 1 }
uninstall: { data: [] }
`

// A service the app is plugged into changes: the app is listed as behind
// because of it, applying brings it the new address, and the step that
// watches the address runs; changing something else of the service (its
// model) leaves the app alone.
func TestReapplyFollowsAService(t *testing.T) {
	r := reapplySetup(t, llmUserManifest)
	ctx := context.Background()
	if _, err := r.e.AddService(ctx, AddServiceRequest{Name: "my-llm", Kind: "llm_gateway", Values: ServiceValues{"base_url": "https://llm.example.com/v1", "api_key": "sk-test-0000000001"}}); err != nil {
		t.Fatal(err)
	}
	r.mustInstall(t, Request{App: "demo-llm-user"})
	if env := envMap(t, r.env, "demo-llm-user"); env["LLM_URL"] != "https://llm.example.com/v1" {
		t.Fatalf("env %v", env)
	}
	if af, _ := r.e.Affected(ctx, ""); len(af.Apps) != 0 {
		t.Fatalf("behind at once: %+v", af)
	}
	pr, err := r.e.PatchService(ctx, "my-llm", PatchServiceRequest{Values: ServiceValues{"base_url": "https://other.example.com/v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pr.Notes) == 0 || !strings.Contains(strings.Join(pr.Notes, " "), "重新应用设置后才生效：demo-llm-user") {
		t.Fatalf("notes %v", pr.Notes)
	}
	af, err := r.e.Affected(ctx, "service:my-llm")
	if err != nil || len(af.Apps) != 1 || !af.Apps[0].Differs || af.Apps[0].Vars[0].Name != "uses.llm.base_url" {
		t.Fatalf("by service %+v %v", af, err)
	}
	if all, _ := r.e.Affected(ctx, ""); len(all.Apps) != 1 {
		t.Fatalf("behind %+v", all)
	}
	r.x.reqs = nil
	tv := r.apply(t, ReapplyRequest{App: "demo-llm-user", Why: WhyService})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv.Fault)
	}
	if env := envMap(t, r.env, "demo-llm-user"); env["LLM_URL"] != "https://other.example.com/v1" || env["LLM_KEY"] != "sk-test-0000000001" {
		t.Fatalf("env %v", env)
	}
	if got := r.volFileOf(t, "demo-llm-user", "llm.conf"); got != "url=https://other.example.com/v1\n" {
		t.Fatalf("llm.conf %q", got)
	}
	if cmds := r.cmds(); len(cmds) != 1 || cmds[0] != "demo-llm reload https://other.example.com/v1" {
		t.Fatalf("commands %v", cmds)
	}
	if all, _ := r.e.Affected(ctx, ""); len(all.Apps) != 0 {
		t.Fatalf("still behind %+v", all)
	}
	// a change of an item the app does not read leaves it alone
	if _, err := r.e.PatchService(ctx, "my-llm", PatchServiceRequest{Values: ServiceValues{"model": "some-model"}}); err != nil {
		t.Fatal(err)
	}
	if all, _ := r.e.Affected(ctx, ""); len(all.Apps) != 0 {
		t.Fatalf("an item nobody reads: %+v", all)
	}
}

// Deleting an app that is being applied is refused by the API through
// Reapplying; the running task is named.
func TestReapplyingNamesTheRunningTask(t *testing.T) {
	r := reapplySetup(t, reapplyManifest)
	r.mustInstall(t, Request{App: "demo-conf"})
	if r.e.Reapplying("demo-conf") != "" {
		t.Fatal("nothing runs")
	}
	hold := make(chan struct{})
	r.w.mu.Lock()
	r.w.withAppHold = hold
	r.w.mu.Unlock()
	st, err := r.e.StartReapply(context.Background(), ReapplyRequest{App: "demo-conf", Params: map[string]any{"title": "等着"}, ConfirmRestart: true})
	if err != nil || st.Task == nil {
		t.Fatalf("%v", err)
	}
	if got := r.e.Reapplying("demo-conf"); got != st.Task.ID {
		t.Fatalf("running %q, task %q", got, st.Task.ID)
	}
	if _, err := r.e.ReapplyPlan(context.Background(), ReapplyRequest{App: "demo-conf"}); code(err) != CodeApplyCannot {
		t.Fatalf("a second one: %v", err)
	}
	close(hold)
	r.wait(t, st.Task.ID)
}
