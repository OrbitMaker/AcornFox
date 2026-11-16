package appinstall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// The request's exposure: only local, lan, public; refused for an app only
// on the internal network; in a recipe it goes to the apps installed that
// are not internal, and a recipe with none such refuses it.
func TestCheckExpose(t *testing.T) {
	web := func(name string) *planned {
		return &planned{name: name, app: &App{Manifest: &catalog.Manifest{}}}
	}
	db := func(name string) *planned {
		return &planned{name: name, app: &App{Manifest: &catalog.Manifest{Exposure: catalog.Exposure{Default: catalog.ExposureInternal}}}}
	}
	if err := checkExpose("", []*planned{db("db")}, false); err != nil {
		t.Fatalf("none: %v", err)
	}
	for _, bad := range []string{"LAN", "internal", "afx_cloud", "everywhere", " lan"} {
		if err := checkExpose(bad, []*planned{web("w")}, false); code(err) != CodeBadParams {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if err := checkExpose("lan", []*planned{db("db")}, false); code(err) != CodeBadParams || !strings.Contains(err.Error(), "只在内部网络") {
		t.Fatalf("internal: %v", err)
	}
	plan := []*planned{db("db"), web("w"), {name: "old", reuse: true, app: &App{Manifest: &catalog.Manifest{}}}}
	if err := checkExpose("lan", plan, true); err != nil || plan[0].expose != "" || plan[1].expose != "lan" || plan[2].expose != "" {
		t.Fatalf("recipe: %v %q %q %q", err, plan[0].expose, plan[1].expose, plan[2].expose)
	}
	if err := checkExpose("public", []*planned{db("db")}, true); code(err) != CodeBadParams {
		t.Fatalf("recipe of internal apps: %v", err)
	}
	for _, ok := range []string{"local", "lan", "public"} {
		p := web("w")
		if err := checkExpose(ok, []*planned{p}, false); err != nil || p.expose != ok {
			t.Fatalf("%s: %v %q", ok, err, p.expose)
		}
	}
}

// addressManifest is the values test material plus the two variables of
// the address's parts.
func addressManifest(t *testing.T) string {
	t.Helper()
	m := valuesManifest(t)
	line := `  - { key: APP_URL_SLASH, value: "{{platform.app.url}}/" }`
	if !strings.Contains(m, line) {
		t.Fatal("the values test material changed: find the APP_URL_SLASH line again")
	}
	return strings.Replace(m, line, line+`
  - { key: APP_SECURE, value: '{{platform.app.scheme | map "https=true" "http=false"}}' }
  - { key: APP_HOST, value: "{{platform.app.host}}" }`, 1)
}

// setupAddress is setupValues with addressManifest on a server reached at
// 192.0.2.10.
func setupAddress(t *testing.T) *env {
	t.Helper()
	return setupAddressDefault(t, "")
}

// setupAddressDefault is setupAddress on a machine that gives new apps def
// (Config.DefaultExposure).
func setupAddressDefault(t *testing.T, def string) *env {
	t.Helper()
	v := setupWith(t, func(cfg *Config) {
		cfg.DefaultExposure = def
		b := withServiceTypes(t, fixtureBundle(t, 7, "", []fixApp{{manifest: addressManifest(t)}}, []catalog.Recipe{oneAppRecipe("demo-values")}, nil), valuesDir)
		cfg.Catalog = NewCatalog(CatalogConfig{CacheDir: filepath.Join(t.TempDir(), "catalog"), Snapshot: bundleFS(b), Sources: []string{"http://127.0.0.1:1"}})
		cfg.Platform = PlatformEnv{Host: platformvar.Host{PublicHost: "192.0.2.10"}}
	})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _, _ = rw.Write([]byte("<h1>ok</h1>")) }))
	t.Cleanup(srv.Close)
	v.w.mu.Lock()
	v.w.servers["demo-values"] = srv
	v.w.mu.Unlock()
	return v
}

// install --expose lan: the app is created opened to the LAN, and the
// variables about its address are the LAN address from the start (what
// status shows); a refused exposure creates nothing.
func TestInstallExpose(t *testing.T) {
	v := setupAddress(t)
	ctx := context.Background()
	if _, err := v.e.StartInstall(ctx, Request{App: "demo-values", Expose: "everywhere"}); code(err) != CodeBadParams {
		t.Fatalf("bad exposure: %v", err)
	}
	if _, err := v.w.st.GetApp(ctx, "demo-values"); err == nil {
		t.Fatal("a refused exposure created the app")
	}
	tv := v.install(t, Request{App: "demo-values", Expose: "lan"})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	a, err := v.w.st.GetApp(ctx, "demo-values")
	if err != nil || a.Exposure != state.ExposureLAN {
		t.Fatalf("app %+v %v", a, err)
	}
	url := "http://192.0.2.10:" + strconv.Itoa(a.PublicPort)
	env := appEnvVars(t, v, "demo-values")
	if env["APP_URL_SLASH"].Value != url+"/" || env["APP_SECURE"].Value != "false" || env["APP_HOST"].Value != "192.0.2.10" {
		t.Fatalf("env %+v %+v %+v", env["APP_URL_SLASH"], env["APP_SECURE"], env["APP_HOST"])
	}
	saved, err := v.e.savedParams(ctx, "demo-values")
	if err != nil {
		t.Fatal(err)
	}
	p := Values(saved).platformMap()
	if p["app.url"] != url || p["app.scheme"] != "http" || p["app.host"] != "192.0.2.10" {
		t.Fatalf("saved %v", p)
	}
	if len(tv.Apps) != 1 || tv.Apps[0].URL != url {
		t.Fatalf("result %+v", tv.Apps)
	}
	// Nothing differs right after the install.
	vars, err := v.e.AppVars(ctx, "demo-values", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range vars.Vars {
		if x.Differs || x.Code != "" {
			t.Fatalf("differs after the install: %+v", x)
		}
	}
}

// apps vars: after the exposure changed (app set --expose) or a domain was
// bound, the rows the manifest uses give the value now next to the one of
// the install time, with APP-VARS-01; what the app was given is not
// rewritten.
func TestAppVarsValueNow(t *testing.T) {
	v := setupAddress(t)
	ctx := context.Background()
	if tv := v.install(t, Request{App: "demo-values"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv.Fault)
	}
	a, _ := v.w.st.GetApp(ctx, "demo-values")
	local := "http://127.0.0.1:" + strconv.Itoa(a.PublicPort)
	row := func(vars AppVars, name string) AppVar {
		t.Helper()
		for _, x := range vars.Vars {
			if x.Name == name {
				return x
			}
		}
		t.Fatalf("no row %s in %+v", name, vars.Vars)
		return AppVar{}
	}
	vars, err := v.e.AppVars(ctx, "demo-values", false)
	if err != nil {
		t.Fatal(err)
	}
	if r := row(vars, "platform.app.url"); r.Value != local || r.ValueNow == nil || *r.ValueNow != local || r.Differs {
		t.Fatalf("before %+v", r)
	}

	// Opened to the LAN afterwards (what app set --expose lan does).
	if _, err := v.w.st.UpdateApp(ctx, "demo-values", func(x *state.App) error { x.Exposure = state.ExposureLAN; return nil }); err != nil {
		t.Fatal(err)
	}
	vars, err = v.e.AppVars(ctx, "demo-values", false)
	if err != nil {
		t.Fatal(err)
	}
	lan := "http://192.0.2.10:" + strconv.Itoa(a.PublicPort)
	r := row(vars, "platform.app.url")
	if r.Value != local || r.ValueNow == nil || *r.ValueNow != lan || !r.Differs || r.Code != CodeVarsStale || r.Fix == "" {
		t.Fatalf("after lan %+v", r)
	}
	if h := row(vars, "platform.app.host"); !h.Differs || *h.ValueNow != "192.0.2.10" {
		t.Fatalf("host %+v", h)
	}
	if s := row(vars, "platform.app.scheme"); s.Differs || *s.ValueNow != "http" {
		t.Fatalf("scheme %+v", s)
	}
	if tz := row(vars, "platform.region"); tz.Differs {
		t.Fatalf("region %+v", tz)
	}
	note := false
	for _, n := range vars.Notes {
		if strings.HasPrefix(n, CodeVarsStale+" ") && strings.Contains(n, "platform.app.url") {
			note = true
		}
	}
	if !note {
		t.Fatalf("notes %v", vars.Notes)
	}
	if got := appEnvVars(t, v, "demo-values")["APP_URL_SLASH"].Value; got != local+"/" {
		t.Fatalf("the app's env was rewritten: %s", got)
	}

	// A domain bound: https, the domain, by the server's say.
	h := v.e.h()
	h.Where = func(ctx context.Context, a state.App) platformvar.Where {
		return platformvar.Where{Exposure: state.ExposureLAN, Port: a.PublicPort, Domains: []string{"demo.example.com"}}
	}
	v.e.SetHooks(h)
	vars, _ = v.e.AppVars(ctx, "demo-values", false)
	if u, s := row(vars, "platform.app.url"), row(vars, "platform.app.scheme"); *u.ValueNow != "https://demo.example.com" || !s.Differs || *s.ValueNow != "https" {
		t.Fatalf("domain %+v %+v", u, s)
	}
	fl, err := v.e.FirstLogin(ctx, "demo-values", false)
	if err == nil && fl.URL != "https://demo.example.com" {
		t.Fatalf("first login url %q", fl.URL)
	}
}

// What an app gets without --expose: a manifest that says local or nothing
// (the catalog's standard) follows what this machine gives new apps (lan on
// a LAN server installed fresh, local otherwise, local when unset); one that
// says internal, lan or public keeps it.
func TestManifestExposure(t *testing.T) {
	m := func(d string) *catalog.Manifest { return &catalog.Manifest{Exposure: catalog.Exposure{Default: d}} }
	for _, c := range []struct{ machine, manifest, want string }{
		{"", "", "local"}, {"", "local", "local"}, {"", "lan", "lan"}, {"", "internal", "internal"},
		{"lan", "", "lan"}, {"lan", "local", "lan"}, {"lan", "internal", "internal"}, {"lan", "public", "public"},
		{"local", "", "local"}, {"local", "lan", "lan"},
		// Anything else the machine says is not followed: an app from the
		// catalog is never opened to everything without the user choosing.
		{"public", "", "local"}, {"bogus", "local", "local"},
	} {
		e := &Engine{cfg: Config{DefaultExposure: c.machine}}
		if got := e.manifestExposure(m(c.manifest)); got != c.want {
			t.Errorf("machine %q manifest %q: %q, want %q", c.machine, c.manifest, got, c.want)
		}
	}
	if got := (&Engine{cfg: Config{DefaultExposure: "lan"}}).manifestExposure(nil); got != "lan" {
		t.Errorf("no manifest: %q", got)
	}
}

// On a LAN server installed fresh (DefaultExposure lan) an app installed
// without --expose is opened to the LAN, its address is the LAN address
// from the start, and the plan says so; --expose still wins.
func TestInstallMachineDefaultLAN(t *testing.T) {
	ctx := context.Background()
	v := setupAddressDefault(t, state.ExposureLAN)
	plan, err := v.e.Plan(ctx, Request{App: "demo-values"})
	if err != nil || len(plan.Apps) != 1 || plan.Apps[0].Exposure != state.ExposureLAN {
		t.Fatalf("plan %+v %v", plan.Apps, err)
	}
	if plan, err := v.e.Plan(ctx, Request{App: "demo-values", Expose: "local"}); err != nil || plan.Apps[0].Exposure != state.ExposureLocal {
		t.Fatalf("plan with --expose local %+v %v", plan.Apps, err)
	}
	tv := v.install(t, Request{App: "demo-values"})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	a, err := v.w.st.GetApp(ctx, "demo-values")
	if err != nil || a.Exposure != state.ExposureLAN {
		t.Fatalf("app %+v %v", a, err)
	}
	url := "http://192.0.2.10:" + strconv.Itoa(a.PublicPort)
	if len(tv.Apps) != 1 || tv.Apps[0].URL != url {
		t.Fatalf("result %+v", tv.Apps)
	}
	if env := appEnvVars(t, v, "demo-values"); env["APP_URL_SLASH"].Value != url+"/" {
		t.Fatalf("env %+v", env["APP_URL_SLASH"])
	}
	d, err := v.e.Detail(ctx, "demo-values")
	if err != nil || d.Exposure != state.ExposureLAN {
		t.Fatalf("detail %q %v", d.Exposure, err)
	}

	// --expose local on the same machine: only this server.
	w := setupAddressDefault(t, state.ExposureLAN)
	if tv := w.install(t, Request{App: "demo-values", Expose: "local"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv.Fault)
	}
	if a, err := w.w.st.GetApp(ctx, "demo-values"); err != nil || a.Exposure != state.ExposureLocal {
		t.Fatalf("app %+v %v", a, err)
	}
}

// A machine that gives nothing (installed before, a cloud server, a
// desktop): the app stays only on this server, as before.
func TestInstallMachineDefaultUnset(t *testing.T) {
	ctx := context.Background()
	v := setupAddressDefault(t, "")
	plan, err := v.e.Plan(ctx, Request{App: "demo-values"})
	if err != nil || len(plan.Apps) != 1 || plan.Apps[0].Exposure != state.ExposureLocal {
		t.Fatalf("plan %+v %v", plan.Apps, err)
	}
	if tv := v.install(t, Request{App: "demo-values"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv.Fault)
	}
	if a, err := v.w.st.GetApp(ctx, "demo-values"); err != nil || a.Exposure != state.ExposureLocal {
		t.Fatalf("app %+v %v", a, err)
	}
}
