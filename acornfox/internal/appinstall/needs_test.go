package appinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// Services the new way: matching, the plan, recipes that install apps
// together, issuing and taking back credentials. The apps are the test material in
// internal/catalog/testdata/services (demo-provider provides demo_token
// with issue, revoke and check steps; demo-consumer needs it, and an
// optional smtp).

const servicesDir = "../catalog/testdata/services"

func demoManifest(t testing.TB, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(servicesDir, "apps", id, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// servicesBundle is a catalog of the given manifests plus the service type
// definitions of the test material.
func servicesBundle(t testing.TB, recipes []catalog.Recipe, manifests ...string) *catalog.Bundle {
	t.Helper()
	return typedBundle(t, servicesDir, recipes, manifests...)
}

// typedBundle is a catalog of the given manifests and recipes plus the
// service type definitions under dir (a test material directory).
func typedBundle(t testing.TB, dir string, recipes []catalog.Recipe, manifests ...string) *catalog.Bundle {
	t.Helper()
	var apps []fixApp
	for _, m := range manifests {
		apps = append(apps, fixApp{manifest: m})
	}
	return withServiceTypes(t, fixtureBundle(t, 1, "", apps, recipes, nil), dir)
}

// withServiceTypes adds the service type definitions under dir (a test
// material directory) to b's index.
func withServiceTypes(t testing.TB, b *catalog.Bundle, dir string) *catalog.Bundle {
	t.Helper()
	var idx catalog.Index
	if err := json.Unmarshal(b.Index, &idx); err != nil {
		t.Fatal(err)
	}
	types, present, ps := catalog.ReadServiceTypes(dir)
	if !present || len(ps) > 0 || len(types) == 0 {
		t.Fatalf("service types: %v %v", present, ps)
	}
	idx.ServiceTypes = types
	raw, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b.Index = append(raw, '\n')
	if _, err := catalog.ParseIndex(b.Index); err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeDemoProvider is the demo-provider app: setup, a session login and
// tokens (POST and GET /api/tokens, DELETE /api/tokens/{id}, GET
// /api/whoami with a token).
type fakeDemoProvider struct {
	mu         sync.Mutex
	password   string
	tokens     map[int]demoToken
	nextID     int
	deleted    []int
	failMake   bool // POST /api/tokens answers 500
	failDelete bool // DELETE /api/tokens/{id} answers 500
	failLogin  bool // POST /api/login answers 500
}

type demoToken struct{ name, owner, key string }

func (f *fakeDemoProvider) handler() http.Handler {
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, data any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { ok(w, nil) })
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.password = b["password"]
		f.mu.Unlock()
		ok(w, map[string]any{"token": "setup-token-1"})
	})
	mux.HandleFunc("PATCH /api/setup/finish", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer setup-token-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ok(w, nil)
	})
	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		good := f.password != "" && b["password"] == f.password
		fail := f.failLogin
		f.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if !good {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "message": "wrong password"})
			return
		}
		ok(w, map[string]any{"session": "session-secret-value-0001"})
	})
	session := func(r *http.Request) bool { return r.Header.Get("X-Session") == "session-secret-value-0001" }
	mux.HandleFunc("POST /api/tokens", func(w http.ResponseWriter, r *http.Request) {
		if !session(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failMake {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "tokens are off"})
			return
		}
		f.nextID++
		if f.tokens == nil {
			f.tokens = map[int]demoToken{}
		}
		f.tokens[f.nextID] = demoToken{name: b["name"], owner: b["owner"], key: "dt-tokenvalue-" + strconv.Itoa(f.nextID) + "-q8w7e6r5t4y3"}
		ok(w, nil)
	})
	mux.HandleFunc("GET /api/tokens", func(w http.ResponseWriter, r *http.Request) {
		if !session(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.mu.Lock()
		items := []map[string]any{}
		for id, tk := range f.tokens {
			items = append(items, map[string]any{"id": id, "name": tk.name, "key": tk.key})
		}
		f.mu.Unlock()
		ok(w, map[string]any{"items": items})
	})
	mux.HandleFunc("DELETE /api/tokens/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !session(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		id, _ := strconv.Atoi(r.PathValue("id"))
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failDelete {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if _, have := f.tokens[id]; !have {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.tokens, id)
		f.deleted = append(f.deleted, id)
		ok(w, nil)
	})
	mux.HandleFunc("GET /api/whoami", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, tk := range f.tokens {
			if r.Header.Get("Authorization") == "Bearer "+tk.key {
				ok(w, nil)
				return
			}
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	return mux
}

func (f *fakeDemoProvider) tokenList() map[int]demoToken {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[int]demoToken{}
	for k, v := range f.tokens {
		out[k] = v
	}
	return out
}

// lockedBuffer is a log sink tests can read.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

type needsEnv struct {
	*env
	prov *fakeDemoProvider
	log  *lockedBuffer
}

// setupServices is an engine on a catalog of the given manifests (demo
// provider and consumer when none are given), with the fake demo provider
// and a consumer that only answers its health check.
func setupServices(t *testing.T, manifests ...string) *needsEnv {
	t.Helper()
	return setupServicesWith(t, nil, manifests...)
}

// setupServicesWith is setupServices with recipes in the catalog.
func setupServicesWith(t *testing.T, recipes []catalog.Recipe, manifests ...string) *needsEnv {
	t.Helper()
	if len(manifests) == 0 {
		manifests = []string{demoManifest(t, "demo-provider"), demoManifest(t, "demo-consumer")}
	}
	logs := &lockedBuffer{}
	v, _ := setupSnapshot(t, bundleFS(servicesBundle(t, recipes, manifests...)), func(c *Config) {
		c.Version = "0.6.0"
		c.Log = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	})
	prov := &fakeDemoProvider{}
	ps := httptest.NewServer(prov.handler())
	t.Cleanup(ps.Close)
	cs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	t.Cleanup(cs.Close)
	v.w.mu.Lock()
	v.w.servers["demo-provider"], v.w.servers["demo-consumer"] = ps, cs
	v.w.mu.Unlock()
	return &needsEnv{env: v, prov: prov, log: logs}
}

func (v *needsEnv) reg(t *testing.T) *Registry {
	t.Helper()
	r, err := v.e.Registry()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// addMail registers a user's smtp service.
func (v *needsEnv) addMail(t *testing.T, name string) {
	t.Helper()
	if _, err := v.reg(t).CreateService(context.Background(), Service{Name: name, Kind: "smtp", Source: state.ServiceFromUser,
		Values: map[string]string{"host": "smtp." + name + ".example.com", "password": "smtp-password-" + name}}); err != nil {
		t.Fatal(err)
	}
}

func needOf(t *testing.T, pv PlanView, app, use string) PlanNeed {
	t.Helper()
	for _, a := range pv.Apps {
		if a.Name != app {
			continue
		}
		for _, n := range a.Needs {
			if n.Use == use {
				return n
			}
		}
	}
	t.Fatalf("plan has no need %s of %s: %+v", use, app, pv.Apps)
	return PlanNeed{}
}

func (v *needsEnv) plan(t *testing.T, req Request) PlanView {
	t.Helper()
	pv, err := v.e.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return pv
}

// samePlan checks that what the plan says and what the install does agree:
// a plan that cannot install has the install refused with its first
// problem's code; one that can is accepted.
func (v *needsEnv) samePlan(t *testing.T, req Request) (PlanView, TaskView, error) {
	t.Helper()
	pv := v.plan(t, req)
	tv, err := v.e.StartInstall(context.Background(), req)
	switch {
	case pv.CanInstall && err != nil:
		t.Fatalf("plan says it can install, install refused: %v (plan %+v)", err, pv)
	case !pv.CanInstall && err == nil:
		t.Fatalf("plan says it cannot (%+v), install accepted", pv.Problems)
	case !pv.CanInstall && code(err) != pv.Problems[0].Code:
		t.Fatalf("plan's first problem %s, install refused with %v", pv.Problems[0].Code, err)
	}
	if err == nil {
		tv = v.wait(t, tv.ID)
	}
	return pv, tv, err
}

// installOK installs req and fails the test unless it succeeds.
func (v *needsEnv) installOK(t *testing.T, req Request) TaskView {
	t.Helper()
	tv := v.install(t, req)
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("install %+v: %+v", req, tv.Fault)
	}
	return tv
}

// noAppName fails when text names an app of the test catalog.
func noAppName(t *testing.T, what, text string) {
	t.Helper()
	for _, name := range []string{"demo-provider", "demo-consumer", "样例提供方", "样例使用方"} {
		if strings.Contains(text, name) {
			t.Fatalf("%s names the app %s: %s", what, name, text)
		}
	}
}

// TestPlanNeedStates: the four states of a need, the plan and the install
// agreeing on each. Nothing else is ever installed for a need.
func TestPlanNeedStates(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()

	// Nothing here: the required one stops it, the optional mail is
	// skipped; the catalog has an app that provides it, but the platform
	// does not offer it.
	pv, _, err := v.samePlan(t, Request{App: "demo-consumer"})
	if code(err) != CodeNoService || pv.CanInstall || len(pv.Steps) != 1 || pv.Steps[0].Name != "demo-consumer" || len(pv.Apps) != 1 {
		t.Fatalf("plan %+v / %v", pv, err)
	}
	n := needOf(t, pv, "demo-consumer", "demo")
	if n.Status != NeedRequiredMissing || !n.Required || n.KindLabel != "样例令牌" || n.Service != "" || n.PendingFrom != "" ||
		len(n.How) != 1 || n.How[0] != "acornfox services add demo_token --name 名字 --set url=… --set token=-" {
		t.Fatalf("demo need %+v", n)
	}
	if n := needOf(t, pv, "demo-consumer", "mail"); n.Status != NeedOptionalMissing || n.Required || n.Without == "" {
		t.Fatalf("mail need %+v", n)
	}
	// One sentence: register one; no app's name, not even the one being
	// installed.
	noAppName(t, "CAT-INST-31", err.Error())
	noAppName(t, "the plan's problem", pv.Problems[0].Message)
	// The command names every required item; the secret one is read from
	// standard input, and the message says so.
	if !strings.Contains(err.Error(), "acornfox services add demo_token --name 名字 --set url=… --set token=-") || !strings.Contains(err.Error(), "标准输入") {
		t.Fatalf("message %v", err)
	}
	// Registered, it is tried first (services test), then the plan is
	// looked at again: the message and the hint both say so.
	if f := AsFault(err); !strings.Contains(f.Message, "登记好以后先运行 acornfox services test 名字，通了再看一次计划") ||
		!strings.Contains(f.Hint, "先运行 acornfox services test 名字") || !strings.Contains(f.Hint, "通了再看一次计划") {
		t.Fatalf("CAT-INST-31 skips the test: message %q hint %q", f.Message, f.Hint)
	}
	// The command's … is the user's real value: asked first, then the
	// command is given (a weak model otherwise hands the user a placeholder).
	if f := AsFault(err); !strings.Contains(f.Hint, "命令里的 … 要换成用户给的真实值，先问到再给命令") {
		t.Fatalf("CAT-INST-31 hint does not say to ask for the values first: %q", f.Hint)
	}
	nothingCreated(t, v.env)

	// The provider installed on its own registers its service; now the
	// consumer's need is matched.
	v.installOK(t, Request{App: "demo-provider"})
	pv = v.plan(t, Request{App: "demo-consumer"})
	if n := needOf(t, pv, "demo-consumer", "demo"); !pv.CanInstall || n.Status != NeedMatched || n.Service != "demo-provider" || n.PendingFrom != "" {
		t.Fatalf("plan %+v", pv)
	}
	if !strings.Contains(pv.Say, "先跳过") {
		t.Fatalf("say %q", pv.Say)
	}

	// Several mail services: choose.
	v.addMail(t, "mail-a")
	v.addMail(t, "mail-b")
	pv, _, err = v.samePlan(t, Request{App: "demo-consumer"})
	if code(err) != CodeChooseService {
		t.Fatalf("choose: %v", err)
	}
	if n := needOf(t, pv, "demo-consumer", "mail"); n.Status != NeedChoose || strings.Join(n.Candidates, ",") != "mail-a,mail-b" {
		t.Fatalf("mail need %+v", n)
	}
	// A wrong pick, a pick of another kind, a need that does not exist.
	for _, use := range []map[string]string{{"mail": "nope"}, {"mail": "demo-provider"}, {"mail": "mail-a", "post": "mail-a"}} {
		if _, _, err := v.samePlan(t, Request{App: "demo-consumer", Use: use}); code(err) != CodeBadUse {
			t.Fatalf("use %v: %v", use, err)
		}
	}
	// A pick of a service that cannot be used now.
	if _, err := v.w.st.UpdateApp(ctx, "demo-provider", func(a *state.App) error { a.Desired = state.DesiredStopped; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.samePlan(t, Request{App: "demo-consumer", Use: map[string]string{"demo": "demo-provider", "mail": "mail-a"}}); code(err) != CodeBadUse {
		t.Fatalf("stopped provider picked: %v", err)
	}
	// Not picked: the stopped provider is no candidate, the plan says how
	// to make it usable.
	pv = v.plan(t, Request{App: "demo-consumer", Use: map[string]string{"mail": "mail-a"}})
	if n := needOf(t, pv, "demo-consumer", "demo"); n.Status != NeedRequiredMissing || len(n.How) != 2 || !strings.Contains(n.How[0], "acornfox start demo-provider") {
		t.Fatalf("stopped provider: %+v", n)
	}
	if _, err := v.w.st.UpdateApp(ctx, "demo-provider", func(a *state.App) error { a.Desired = state.DesiredRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	if apps, _ := v.w.st.ListApps(ctx); len(apps) != 1 {
		t.Fatalf("a refused install left an app: %+v", apps)
	}

	// Picked: matched; the install goes ahead, one app only.
	req := Request{App: "demo-consumer", Use: map[string]string{"demo-consumer.mail": "mail-b"}}
	pv, tv, err := v.samePlan(t, req)
	if err != nil || tv.Status != state.TaskSucceeded {
		t.Fatalf("install %v %+v", err, tv.Fault)
	}
	if n := needOf(t, pv, "demo-consumer", "mail"); n.Status != NeedMatched || n.Service != "mail-b" {
		t.Fatalf("mail need %+v", n)
	}
	if len(tv.Apps) != 1 || tv.Apps[0].App != "demo-consumer" {
		t.Fatalf("task apps %+v", tv.Apps)
	}
	if b, _ := v.reg(t).GetBinding(ctx, "demo-consumer", "mail"); b.Service != "mail-b" {
		t.Fatalf("mail binding %+v", b)
	}

	// A second consumer: one demo service, mail picked.
	pv = v.plan(t, Request{App: "demo-consumer", Name: "second-consumer", Use: map[string]string{"mail": "mail-a"}})
	if n := needOf(t, pv, "second-consumer", "demo"); !pv.CanInstall || n.Status != NeedMatched || n.Service != "demo-provider" || len(pv.Steps) != 1 {
		t.Fatalf("second plan %+v", pv)
	}
}

// TestPlanTwoProviders: two installed apps provide the kind: the user
// chooses; an app plugged into one before (a binding kept with its data)
// gets that one again.
func TestPlanTwoProviders(t *testing.T) {
	second := strings.Replace(demoManifest(t, "demo-provider"), "id: demo-provider\n", "id: demo-provider-2\n", 1)
	v := setupServices(t, demoManifest(t, "demo-provider"), second, demoManifest(t, "demo-consumer"))
	other := &fakeDemoProvider{}
	srv2 := httptest.NewServer(other.handler())
	t.Cleanup(srv2.Close)
	v.w.mu.Lock()
	v.w.servers["demo-provider-2"] = srv2
	v.w.mu.Unlock()
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-provider"})
	v.installOK(t, Request{App: "demo-provider-2"})

	pv, _, err := v.samePlan(t, Request{App: "demo-consumer"})
	if code(err) != CodeChooseService {
		t.Fatalf("two providers: %v", err)
	}
	if n := needOf(t, pv, "demo-consumer", "demo"); n.Status != NeedChoose || strings.Join(n.Candidates, ",") != "demo-provider,demo-provider-2" {
		t.Fatalf("need %+v", n)
	}
	_, tv, err := v.samePlan(t, Request{App: "demo-consumer", Use: map[string]string{"demo": "demo-provider-2"}})
	if err != nil || tv.Status != state.TaskSucceeded {
		t.Fatalf("install %v %+v", err, tv.Fault)
	}
	if len(v.prov.tokenList()) != 0 || len(other.tokenList()) != 1 {
		t.Fatalf("token issued by the wrong provider: %+v / %+v", v.prov.tokenList(), other.tokenList())
	}
	// Deleted with its data kept, installed again: the one it used before.
	if err := v.w.delete(ctx, "demo-consumer", false); err != nil {
		t.Fatal(err)
	}
	pv = v.plan(t, Request{App: "demo-consumer", ReuseData: true})
	if n := needOf(t, pv, "demo-consumer", "demo"); !pv.CanInstall || n.Status != NeedMatched || n.Service != "demo-provider-2" {
		t.Fatalf("plan on kept data %+v", pv)
	}
}

// TestPlanRequiredMissing: a user's own service of the kind is all it
// takes; the optional mail not connected leaves its variables out.
func TestPlanRequiredMissing(t *testing.T) {
	v := setupServices(t, demoManifest(t, "demo-consumer"))
	if _, err := v.reg(t).CreateService(context.Background(), Service{Name: "my-demo", Kind: "demo_token", Source: state.ServiceFromUser,
		Values: map[string]string{"url": "https://demo.example.com", "token": "user-token-value-9999"}}); err != nil {
		t.Fatal(err)
	}
	pv, tv, err := v.samePlan(t, Request{App: "demo-consumer"})
	if err != nil || tv.Status != state.TaskSucceeded || needOf(t, pv, "demo-consumer", "demo").Service != "my-demo" {
		t.Fatalf("with a user's service: %v %+v %+v", err, pv, tv.Fault)
	}
	envs := envVars(t, v.env, "demo-consumer")
	if envs["DEMO_URL"].Value != "https://demo.example.com" || envs["DEMO_TOKEN"].Value != "user-token-value-9999" || !envs["DEMO_TOKEN"].Secret {
		t.Fatalf("env %+v", envs)
	}
	// The optional mail is not connected: its variables are left out.
	if _, have := envs["SMTP_HOST"]; have {
		t.Fatalf("optional env written: %+v", envs)
	}
	if _, have := envs["SMTP_PORT"]; have {
		t.Fatalf("optional env written: %+v", envs)
	}
	uses, err := v.e.AppServices(context.Background(), "demo-consumer")
	if err != nil || len(uses) != 2 || uses[0].Service != "my-demo" || uses[0].Status != "ok" || uses[1].Service != "" || uses[1].Status != UseNotConnected {
		t.Fatalf("app services %+v %v", uses, err)
	}
	raw, _ := v.w.st.GetInstallTask(context.Background(), tv.ID)
	if strings.Contains(raw.Data, "user-token-value-9999") {
		t.Fatal("the task record holds the user's token")
	}
}

func envVars(t *testing.T, v *env, app string) map[string]state.EnvVar {
	t.Helper()
	envs, err := v.w.st.ListEnv(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]state.EnvVar{}
	for _, e := range envs {
		out[e.Key] = e
	}
	return out
}

// TestInstallIssues: the provider registers its service; the consumer
// installed afterwards is issued a token, gets the items in its env and
// config file and joins the provider's network; secrets show nowhere.
func TestInstallIssues(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()
	v.addMail(t, "mail-a")
	ptv := v.installOK(t, Request{App: "demo-provider"})
	if r := ptv.Apps[0]; strings.Join(r.Services, ",") != "demo-provider" {
		t.Fatalf("provider result %+v", r)
	}
	tv := v.installOK(t, Request{App: "demo-consumer"})
	reg := v.reg(t)
	sv, err := reg.GetService(ctx, "demo-provider")
	if err != nil || sv.Source != state.ServiceFromApp || sv.App != "demo-provider" || sv.Kind != "demo_token" ||
		sv.Values["url"] != "http://demo-provider:8080/api/demo" || len(sv.Values) != 1 {
		t.Fatalf("provider's service %+v %v", sv, err)
	}
	toks := v.prov.tokenList()
	if len(toks) != 1 || toks[1].name != "acornfox-demo-consumer" || toks[1].owner != "demo-consumer" {
		t.Fatalf("tokens %+v", toks)
	}
	token := toks[1].key
	b, err := reg.GetBinding(ctx, "demo-consumer", "demo")
	if err != nil || b.Service != "demo-provider" || b.Issued["token"] != token || b.Issued["token_id"] != "1" || len(b.Issued[catalog.IssueIDKey]) != 8 {
		t.Fatalf("binding %+v %v", b, err)
	}
	if mb, err := reg.GetBinding(ctx, "demo-consumer", "mail"); err != nil || mb.Service != "mail-a" || len(mb.Issued) != 0 {
		t.Fatalf("mail binding %+v %v", mb, err)
	}
	envs := envVars(t, v.env, "demo-consumer")
	if envs["DEMO_URL"].Value != "http://demo-provider:8080/api/demo" || envs["DEMO_TOKEN"].Value != token || !envs["DEMO_TOKEN"].Secret ||
		envs["SMTP_HOST"].Value != "smtp.mail-a.example.com" || envs["SMTP_PORT"].Value != "587" {
		t.Fatalf("env %+v", envs)
	}
	a, _ := v.w.st.GetApp(ctx, "demo-consumer")
	if strings.Join(a.Links, ",") != "demo-provider" {
		t.Fatalf("links %v", a.Links)
	}
	found := false
	for _, w := range v.w.wrote {
		if w.App == "demo-consumer" && len(w.Files) == 1 && string(w.Files[0].Content) == "url = http://demo-provider:8080/api/demo\n" {
			found = true
		}
	}
	if !found {
		t.Fatalf("config file %+v", v.w.wrote)
	}
	if r := tv.Apps[0]; len(r.Bindings) != 2 || !r.Bindings[0].Issued || r.Bindings[0].Service != "demo-provider" || r.Bindings[1].Issued {
		t.Fatalf("consumer result %+v", r)
	}
	uses, _ := v.e.AppServices(ctx, "demo-consumer")
	if len(uses) != 2 || uses[0].Status != "ok" || uses[0].Service != "demo-provider" || uses[1].Service != "mail-a" {
		t.Fatalf("app services %+v", uses)
	}
	// No secret in the task records, the plan, the views, the saved
	// parameters or the log.
	raw, _ := v.w.st.GetInstallTask(ctx, tv.ID)
	praw, _ := v.w.st.GetInstallTask(ctx, ptv.ID)
	saved, _ := v.e.savedParams(ctx, "demo-consumer")
	savedJSON, _ := json.Marshal(saved)
	planJSON, _ := json.Marshal(v.plan(t, Request{App: "demo-consumer", Name: "again", Use: map[string]string{"mail": "mail-a"}}))
	usesJSON, _ := json.Marshal(uses)
	detail, err := v.e.Detail(ctx, "demo-consumer")
	if err != nil {
		t.Fatal(err)
	}
	detailJSON, _ := json.Marshal(detail)
	for what, text := range map[string]string{"task": raw.Data, "provider task": praw.Data, "saved params": string(savedJSON), "plan": string(planJSON),
		"app services": string(usesJSON), "detail": string(detailJSON), "log": v.log.String()} {
		for _, secret := range []string{token, "smtp-password-mail-a", "session-secret-value-0001"} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s holds a secret: %s", what, secret)
			}
		}
	}
	if strings.Contains(string(savedJSON), usesKey) {
		t.Fatalf("saved params keep the services' items: %s", savedJSON)
	}
	// The detail: needs with label and required, provides with label.
	if len(detail.Uses) != 2 || detail.Uses[0].UseName != "demo" || detail.Uses[0].KindLabel != "样例令牌" || !detail.Uses[0].Required ||
		detail.Uses[1].Required || detail.Uses[1].Name != "mail" || len(detail.ProvidesDetail) != 0 {
		t.Fatalf("detail uses %+v / %+v", detail.Uses, detail.ProvidesDetail)
	}
	if pd, _ := v.e.Detail(ctx, "demo-provider"); len(pd.ProvidesDetail) != 1 || pd.ProvidesDetail[0].KindLabel != "样例令牌" {
		t.Fatalf("provider detail %+v", pd.ProvidesDetail)
	}

	// The provider cannot go while the consumer uses it.
	users, err := v.e.ServiceUsers(ctx, "demo-provider")
	if err != nil || len(users) != 1 || users[0].App != "demo-consumer" || !strings.Contains(ServiceUsersText("demo-provider", users), "demo-consumer") {
		t.Fatalf("users %+v %v", users, err)
	}
	// Deleting the consumer with its data takes the token back.
	if err := v.w.delete(ctx, "demo-consumer", true); err != nil {
		t.Fatal(err)
	}
	if ws := v.e.ReleaseServices(ctx, "demo-consumer", true); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if len(v.prov.tokenList()) != 0 || len(v.prov.deleted) != 1 {
		t.Fatalf("token not taken back: %+v", v.prov.tokenList())
	}
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{App: "demo-consumer"}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
	if users, _ := v.e.ServiceUsers(ctx, "demo-provider"); len(users) != 0 {
		t.Fatalf("users left %+v", users)
	}
	// Deleting the provider with its data removes its service.
	if err := v.w.delete(ctx, "demo-provider", true); err != nil {
		t.Fatal(err)
	}
	if ws := v.e.ReleaseServices(ctx, "demo-provider", true); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if _, err := reg.GetService(ctx, "demo-provider"); err == nil {
		t.Fatal("provider's service left")
	}
}

// demoRecipe installs the demo provider, then the consumer; with link the
// consumer's need is wired to the provider by the recipe.
func demoRecipe(id string, link bool) catalog.Recipe {
	r := catalog.Recipe{ID: id, Schema: 1, Title: "样例套装 " + id, Summary: "测试用：提供方和使用方一起装",
		Apps: []catalog.RecipeApp{{App: "demo-provider"}, {App: "demo-consumer"}}, Delivery: catalog.Delivery{Tier: "auto"}}
	if link {
		r.Links = []catalog.Link{{From: "demo-provider", To: "demo-consumer", Uses: "demo"}}
	}
	return r
}

// TestRecipeLinkOrder: a recipe link goes from an app installed earlier to
// one installed later (the later one is connected to what the earlier one
// registers); the other way round is refused before anything is created,
// as catalog lint refuses it (CAT-LINT-09).
func TestRecipeLinkOrder(t *testing.T) {
	r := demoRecipe("demo-backwards", true)
	r.Apps = []catalog.RecipeApp{{App: "demo-consumer"}, {App: "demo-provider"}}
	v := setupServicesWith(t, []catalog.Recipe{r})
	_, err := v.e.StartInstall(context.Background(), Request{Recipe: "demo-backwards"})
	if code(err) != CodeUnsupported || !strings.Contains(err.Error(), "排在") {
		t.Fatalf("backwards link: %v", err)
	}
	nothingCreated(t, v.env)
}

// TestRecipeConnects: a recipe installs the provider and the consumer in
// its order; the plan shows the consumer plugged into the service the
// provider will register (it does not exist yet), and the install does it.
func TestRecipeConnects(t *testing.T) {
	v := setupServicesWith(t, []catalog.Recipe{demoRecipe("demo-pair", true), demoRecipe("demo-pair-auto", false)})
	ctx := context.Background()
	for _, id := range []string{"demo-pair", "demo-pair-auto"} {
		pv := v.plan(t, Request{Recipe: id})
		if !pv.CanInstall || len(pv.Steps) != 2 || pv.Steps[0].Name != "demo-provider" || pv.Steps[1].Name != "demo-consumer" ||
			!strings.Contains(pv.Steps[0].Because, "demo-consumer") || pv.Steps[1].Because != "" {
			t.Fatalf("%s plan %+v", id, pv)
		}
		if n := needOf(t, pv, "demo-consumer", "demo"); n.Status != NeedMatched || n.Service != "demo-provider" || n.PendingFrom != "demo-provider" {
			t.Fatalf("%s need %+v", id, n)
		}
	}
	// A user's own service of the kind as well: without the link that makes
	// two candidates (choose); the link says which.
	if _, err := v.reg(t).CreateService(ctx, Service{Name: "my-demo", Kind: "demo_token", Source: state.ServiceFromUser,
		Values: map[string]string{"url": "https://demo.example.com", "token": "user-token-value-9999"}}); err != nil {
		t.Fatal(err)
	}
	pv, _, err := v.samePlan(t, Request{Recipe: "demo-pair-auto"})
	if n := needOf(t, pv, "demo-consumer", "demo"); code(err) != CodeChooseService || n.Status != NeedChoose || strings.Join(n.Candidates, ",") != "demo-provider,my-demo" {
		t.Fatalf("auto with two: %v %+v", err, n)
	}
	nothingCreated(t, v.env)
	pv, tv, err := v.samePlan(t, Request{Recipe: "demo-pair"})
	if err != nil || tv.Status != state.TaskSucceeded {
		t.Fatalf("recipe install %v %+v", err, tv.Fault)
	}
	if n := needOf(t, pv, "demo-consumer", "demo"); n.Service != "demo-provider" {
		t.Fatalf("need %+v", n)
	}
	if len(tv.Apps) != 2 || tv.Apps[0].App != "demo-provider" || tv.Apps[1].App != "demo-consumer" {
		t.Fatalf("task apps %+v", tv.Apps)
	}
	b, err := v.reg(t).GetBinding(ctx, "demo-consumer", "demo")
	toks := v.prov.tokenList()
	if err != nil || b.Service != "demo-provider" || len(toks) != 1 || b.Issued["token"] != toks[1].key {
		t.Fatalf("binding %+v %v tokens %+v", b, err, toks)
	}
	if envVars(t, v.env, "demo-consumer")["DEMO_TOKEN"].Value != toks[1].key {
		t.Fatal("env without the token")
	}
	// The same recipe again: both installed already, nothing to do but
	// reuse them (a reused app's needs are not matched again).
	pv = v.plan(t, Request{Recipe: "demo-pair"})
	if !pv.CanInstall || pv.Steps[0].Action != "reuse" || pv.Steps[1].Action != "reuse" {
		t.Fatalf("again %+v", pv)
	}
}

// TestFailedInstallTakesBack: the consumer fails after its token was
// issued: the token is taken back, the binding goes; in a recipe the
// provider's service goes with the provider.
func TestFailedInstallTakesBack(t *testing.T) {
	v := setupServicesWith(t, []catalog.Recipe{demoRecipe("demo-pair", true)})
	ctx := context.Background()
	v.w.failDeploy["demo-consumer"] = true
	tv := v.install(t, Request{Recipe: "demo-pair"})
	if tv.Status != state.TaskFailed || tv.Fault.Code != CodeStart {
		t.Fatalf("task %+v", tv.Fault)
	}
	if len(v.prov.deleted) != 1 || len(v.prov.tokenList()) != 0 {
		t.Fatalf("token not taken back: deleted %v left %+v", v.prov.deleted, v.prov.tokenList())
	}
	reg := v.reg(t)
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
	if svs, _ := reg.ListServices(ctx, state.ServiceFilter{}); len(svs) != 0 {
		t.Fatalf("services left %+v", svs)
	}
	if apps, _ := v.w.st.ListApps(ctx); len(apps) != 0 {
		t.Fatalf("apps left %+v", apps)
	}

	// The provider on its own, then the consumer fails: the provider and
	// its service stay, the consumer's token is taken back.
	v.w.failDeploy["demo-consumer"] = false
	v.installOK(t, Request{App: "demo-provider"})
	v.w.failDeploy["demo-consumer"] = true
	if tv := v.install(t, Request{App: "demo-consumer"}); tv.Status != state.TaskFailed {
		t.Fatalf("task %+v", tv)
	}
	if len(v.prov.deleted) != 2 || len(v.prov.tokenList()) != 0 {
		t.Fatalf("token not taken back: deleted %v left %+v", v.prov.deleted, v.prov.tokenList())
	}
	if _, err := reg.GetService(ctx, "demo-provider"); err != nil {
		t.Fatalf("provider's service gone: %v", err)
	}
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
}

// TestIssueFails: the provider will not issue: CAT-INST-34, the consumer's
// install removed, the provider untouched.
func TestIssueFails(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-provider"})
	v.prov.failMake = true
	tv := v.install(t, Request{App: "demo-consumer"})
	if tv.Status != state.TaskFailed || tv.Fault.Code != CodeIssue || !strings.Contains(tv.Fault.Message, "demo-provider") ||
		strings.Contains(tv.Fault.Message, "session-secret-value-0001") {
		t.Fatalf("task %+v", tv.Fault)
	}
	if s := stepOf(tv, "demo-consumer", "create"); s == nil || s.Status != StepFailed {
		t.Fatalf("create step %+v", s)
	}
	if _, err := v.w.st.GetApp(ctx, "demo-consumer"); err == nil {
		t.Fatal("consumer left")
	}
	if bs, _ := v.reg(t).ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
	if _, err := v.reg(t).GetService(ctx, "demo-provider"); err != nil {
		t.Fatalf("provider's service gone: %v", err)
	}
}

// TestRevokeFailsWarns: the provider is stopped when the consumer goes: a
// CAT-INST-35 warning names what to remove, the delete is not stopped.
func TestRevokeFailsWarns(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-provider"})
	v.installOK(t, Request{App: "demo-consumer"})
	if _, err := v.w.st.UpdateApp(ctx, "demo-provider", func(a *state.App) error { a.Desired = state.DesiredStopped; return nil }); err != nil {
		t.Fatal(err)
	}
	b, _ := v.reg(t).GetBinding(ctx, "demo-consumer", "demo")
	if err := v.w.delete(ctx, "demo-consumer", true); err != nil {
		t.Fatal(err)
	}
	ws := v.e.ReleaseServices(ctx, "demo-consumer", true)
	raw, _ := json.Marshal(ws)
	if len(ws) != 1 || !strings.HasPrefix(ws[0], CodeRevoke) || !strings.Contains(ws[0], "demo-provider") || !strings.Contains(ws[0], b.Issued[catalog.IssueIDKey]) ||
		strings.Contains(string(raw), v.prov.tokenList()[1].key) {
		t.Fatalf("warnings %v", ws)
	}
	if bs, _ := v.reg(t).ListBindings(ctx, state.BindingFilter{App: "demo-consumer"}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
}

// TestReuseDataChecksOldCredential: deleted with its data kept, the
// consumer keeps its binding; installed again on that data, the old token
// is used again when the check steps pass, and replaced when they do not.
func TestReuseDataChecksOldCredential(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()
	v.installOK(t, Request{App: "demo-provider"})
	v.installOK(t, Request{App: "demo-consumer"})
	reg := v.reg(t)
	first, _ := reg.GetBinding(ctx, "demo-consumer", "demo")
	if err := v.w.delete(ctx, "demo-consumer", false); err != nil {
		t.Fatal(err)
	}
	if ws := v.e.ReleaseServices(ctx, "demo-consumer", false); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	if _, err := reg.GetBinding(ctx, "demo-consumer", "demo"); err != nil {
		t.Fatalf("binding gone with the data kept: %v", err)
	}
	// A deleted app's kept binding does not count as a user of the provider.
	if users, _ := v.e.ServiceUsers(ctx, "demo-provider"); len(users) != 0 {
		t.Fatalf("a deleted app counted as a user: %+v", users)
	}

	tv := v.installOK(t, Request{App: "demo-consumer", ReuseData: true})
	again, _ := reg.GetBinding(ctx, "demo-consumer", "demo")
	if len(v.prov.tokenList()) != 1 || again.Issued["token"] != first.Issued["token"] {
		t.Fatalf("old token not used again: %+v", v.prov.tokenList())
	}
	if r := tv.Apps[0]; len(r.Bindings) != 1 || !r.Bindings[0].Reused || !strings.Contains(strings.Join(r.Carried, "；"), "接着用以前") {
		t.Fatalf("result %+v", r)
	}

	// The token was removed in the provider meanwhile: a new one is issued.
	if err := v.w.delete(ctx, "demo-consumer", false); err != nil {
		t.Fatal(err)
	}
	v.prov.mu.Lock()
	v.prov.tokens = map[int]demoToken{}
	v.prov.mu.Unlock()
	tv = v.installOK(t, Request{App: "demo-consumer", ReuseData: true})
	third, _ := reg.GetBinding(ctx, "demo-consumer", "demo")
	toks := v.prov.tokenList()
	if len(toks) != 1 || third.Issued["token"] == first.Issued["token"] || third.Issued[catalog.IssueIDKey] == first.Issued[catalog.IssueIDKey] {
		t.Fatalf("no new token: %+v %+v", toks, third.Issued)
	}
	if r := tv.Apps[0]; len(r.Bindings) != 1 || !r.Bindings[0].Issued || r.Bindings[0].Reused {
		t.Fatalf("result %+v", r)
	}
	if envVars(t, v.env, "demo-consumer")["DEMO_TOKEN"].Value != third.Issued["token"] {
		t.Fatal("env holds the old token")
	}

	// The consumer deleted with its data kept, then the provider deleted
	// with its data kept too: nobody installed uses its service, which stays
	// (its provider is gone: not usable) with the kept binding: the token is
	// still in the provider's data. Both come back on their data.
	if err := v.w.delete(ctx, "demo-consumer", false); err != nil {
		t.Fatal(err)
	}
	if err := v.w.delete(ctx, "demo-provider", false); err != nil {
		t.Fatal(err)
	}
	if ws := v.e.ReleaseServices(ctx, "demo-provider", false); len(ws) != 0 {
		t.Fatalf("warnings %v", ws)
	}
	sv, err := reg.GetService(ctx, "demo-provider")
	if err != nil {
		t.Fatalf("service gone with the provider's data kept: %v", err)
	}
	if h := v.e.serviceHealth(ctx, sv, v.e.planKinds()); h.Status != ServiceProviderGone {
		t.Fatalf("service status %+v", h)
	}
	if b, err := reg.GetBinding(ctx, "demo-consumer", "demo"); err != nil || b.Issued["token"] != third.Issued["token"] {
		t.Fatalf("kept binding %+v %v", b, err)
	}
	// Meanwhile a consumer finds nothing it can use, and is told how the
	// service comes back: that app instance installed again on its data, by
	// its name only (a command to copy that names no catalog app: the
	// platform finds which one it was), or one of the user's own registered.
	if n := needOf(t, v.plan(t, Request{App: "demo-consumer", ReuseData: true}), "demo-consumer", "demo"); n.Status != NeedRequiredMissing ||
		len(n.How) != 2 || !strings.Contains(n.How[0], "demo-provider 已经删了、数据还留着") ||
		!strings.HasSuffix(n.How[0], "acornfox install --name demo-provider --reuse-data") || !strings.HasPrefix(n.How[1], "acornfox services add demo_token") {
		t.Fatalf("need with the provider gone %+v", n)
	}
	v.installOK(t, Request{Name: "demo-provider", ReuseData: true}) // what the command above sends
	if svs, _ := reg.ListServices(ctx, state.ServiceFilter{}); len(svs) != 1 || svs[0].Name != "demo-provider" {
		t.Fatalf("services after the provider came back %+v", svs)
	}
	tv = v.installOK(t, Request{App: "demo-consumer", ReuseData: true})
	if b, _ := reg.GetBinding(ctx, "demo-consumer", "demo"); b.Issued["token"] != third.Issued["token"] || len(v.prov.tokenList()) != 1 ||
		len(tv.Apps[0].Bindings) != 1 || !tv.Apps[0].Bindings[0].Reused {
		t.Fatalf("token not used again: %+v / %+v", b.Issued, tv.Apps[0])
	}

	// The consumer deleted with its data kept, the provider with its data:
	// the service goes, and the kept binding with it (the token lived in the
	// provider's data: nothing is left to take back).
	if err := v.w.delete(ctx, "demo-consumer", false); err != nil {
		t.Fatal(err)
	}
	if err := v.w.delete(ctx, "demo-provider", true); err != nil {
		t.Fatal(err)
	}
	// What the delete says: the kept consumer has to be connected anew.
	if ws := v.e.ReleaseServices(ctx, "demo-provider", true); len(ws) != 1 || !strings.Contains(ws[0], "demo-consumer") || !strings.Contains(ws[0], "重新接服务") ||
		strings.HasPrefix(ws[0], CodeRevoke) {
		t.Fatalf("warnings %v", ws)
	}
	if svs, _ := reg.ListServices(ctx, state.ServiceFilter{}); len(svs) != 0 {
		t.Fatalf("services left %+v", svs)
	}
	if bs, _ := reg.ListBindings(ctx, state.BindingFilter{}); len(bs) != 0 {
		t.Fatalf("bindings left %+v", bs)
	}
}

// TestPlanMaterialApps: the provider of the test material needs nothing and
// provides a model API; the consumer needs one, which this machine does not
// have until the provider is installed (the plan says so, and how to
// register one, never which app to install); after that it is matched with
// the provider's service, and the provider itself can no longer be
// installed under its name.
func TestPlanMaterialApps(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	pv, err := v.e.Plan(ctx, Request{App: "hermes-agent", Params: hermesModel()})
	if err != nil {
		t.Fatal(err)
	}
	n := needOf(t, pv, "hermes-agent", "llm")
	if pv.CanInstall || n.Status != NeedRequiredMissing || n.KindLabel != "大模型接口" || len(n.How) != 1 || n.How[0] != "acornfox services add llm_gateway --name 名字 --set base_url=… --set api_key=-" ||
		pv.Problems[0].Code != CodeNoService || strings.Contains(pv.Say, "new-api") || len(pv.Steps) != 1 {
		t.Fatalf("plan %+v", pv)
	}
	pv, _ = v.e.Plan(ctx, Request{App: "new-api"})
	if !pv.CanInstall || len(pv.Steps) != 1 || pv.Steps[0].Action != "install" || len(pv.Apps[0].Needs) != 0 ||
		len(pv.Apps[0].Provides) != 1 || pv.Apps[0].Provides[0].Kind != "llm_gateway" {
		t.Fatalf("plan %+v", pv)
	}
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("install %+v", tv.Fault)
	}
	pv, _ = v.e.Plan(ctx, Request{App: "hermes-agent", Params: hermesModel()})
	if n := needOf(t, pv, "hermes-agent", "llm"); !pv.CanInstall || n.Status != NeedMatched || n.Service != "new-api" {
		t.Fatalf("plan %+v", pv)
	}
	// A problem the form cannot fix is listed.
	pv, _ = v.e.Plan(ctx, Request{App: "new-api"})
	if pv.CanInstall || len(pv.Problems) == 0 || pv.Problems[0].Code != CodeNameTaken {
		t.Fatalf("plan %+v", pv)
	}
}

// A service of the kind that still lacks a secret item: the plan says how
// to fill it in with 项=- (read from standard input), never with the value
// on the command line; the command to register one of the user's own names
// every required item, the secret one as 项=- too.
func TestPlanSecretItemsReadFromStdin(t *testing.T) {
	v := setupServices(t)
	ctx := context.Background()
	if _, _, err := v.w.st.EnsureApp(ctx, "keeper"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.w.st.UpdateApp(ctx, "keeper", func(a *state.App) error { a.Desired = state.DesiredRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := v.reg(t).CreateService(ctx, Service{Name: "keeper", Kind: "demo_token", Source: state.ServiceFromApp, App: "keeper",
		Values: map[string]string{"url": "http://keeper:8080/api"}}); err != nil {
		t.Fatal(err)
	}
	n := needOf(t, v.plan(t, Request{App: "demo-consumer"}), "demo-consumer", "demo")
	if n.Status != NeedRequiredMissing || len(n.How) != 2 ||
		!strings.Contains(n.How[0], "acornfox services set keeper token=-") ||
		n.How[1] != "acornfox services add demo_token --name 名字 --set url=… --set token=-" {
		t.Fatalf("need %+v", n)
	}
}

// askProvider and askConsumer are the demo apps changed to an llm_gateway
// whose model the user fills in after a step in the provider (written ask);
// nothing is issued per consumer.
const providerAsk = "在样例提供方里选一个模型，把名字填进来"

func askApps(t *testing.T) (string, string) {
	t.Helper()
	prov := demoManifest(t, "demo-provider")
	i, j := strings.Index(prov, "provides:\n"), strings.Index(prov, "\ndelivery:")
	if i < 0 || j < i {
		t.Fatal("demo-provider manifest changed shape")
	}
	prov = prov[:i] + `provides:
  - kind: llm_gateway
    endpoint: web
    values:
      base_url: "{{self.internal_url}}/v1"
      api_key: "{{params.admin_password}}"
      model: { ask: ` + providerAsk + ` }
` + prov[j:]
	cons := strings.NewReplacer("kind: demo_token", "kind: llm_gateway", "uses.demo.url", "uses.demo.base_url", "uses.demo.token", "uses.demo.api_key").
		Replace(demoManifest(t, "demo-consumer"))
	cons = strings.Replace(cons, "\nenv:\n", "\nenv:\n  - { key: DEMO_MODEL, from: uses.demo.model }\n", 1)
	if !strings.Contains(cons, "uses.demo.model") {
		t.Fatal("demo-consumer manifest changed shape")
	}
	return prov, cons
}

// providerTell is what the plan and the task say about providerAsk for a
// consumer that uses the item: which service, which item, the provider's
// words, and how to fill it in.
func providerTell(service string) string {
	return "服务 " + service + " 的“默认模型”还没填：" + providerAsk + "。填法：acornfox services set " + service + " model=值"
}

// TestProviderAskIsTold: a need matched to an app's service whose manifest
// asks the user to do a step (values written ask) for an item the consumer
// refers to (uses.<need>.<item>) carries a sentence about it in the plan
// (tell) and, once installed, in the task (apps[].tell) and its must_tell:
// the service, the item's label, the provider's words as they are and the
// command that fills it in; the same in a recipe, from the provider
// installed earlier. An item the consumer does not refer to is not told;
// an item already filled in has nothing to tell; a user's own service
// neither. The words are the provider's manifest's and the kind's: the
// platform names no app.
func TestProviderAskIsTold(t *testing.T) {
	prov, cons := askApps(t)
	quiet := strings.Replace(strings.Replace(cons, "  - { key: DEMO_MODEL, from: uses.demo.model }\n", "", 1), "id: demo-consumer", "id: demo-quiet", 1)
	r := demoRecipe("demo-ask", true)
	v := setupServicesWith(t, []catalog.Recipe{r}, prov, cons, quiet)
	ctx := context.Background()
	tell := providerTell("demo-provider")

	pv := v.plan(t, Request{Recipe: "demo-ask"})
	if n := needOf(t, pv, "demo-consumer", "demo"); !pv.CanInstall || n.Status != NeedMatched || n.PendingFrom != "demo-provider" || strings.Join(n.Tell, "|") != tell {
		t.Fatalf("recipe plan %+v", pv)
	}

	v.installOK(t, Request{App: "demo-provider"})
	pv = v.plan(t, Request{App: "demo-consumer"})
	n := needOf(t, pv, "demo-consumer", "demo")
	if !pv.CanInstall || n.Status != NeedMatched || n.Service != "demo-provider" || strings.Join(n.Tell, "|") != tell {
		t.Fatalf("plan %+v", pv)
	}
	if !strings.Contains(mustJSON(t, pv), `"tell":["`+tell+`"]`) {
		t.Fatalf("plan json %s", mustJSON(t, pv))
	}

	// A consumer that never refers to the item: nothing to tell (the plan
	// and the install match needs, tell included, the same way).
	pv = v.plan(t, Request{App: "demo-quiet"})
	if n := needOf(t, pv, "demo-quiet", "demo"); !pv.CanInstall || n.Status != NeedMatched || n.Tell != nil || strings.Contains(mustJSON(t, pv), `"tell"`) {
		t.Fatalf("a consumer without the item %+v", n)
	}

	tv := v.installOK(t, Request{App: "demo-consumer"})
	if len(tv.Apps) != 1 || strings.Join(tv.Apps[0].Tell, "|") != tell || tv.Timing == nil || strings.Join(tv.Timing.MustTell, "|") != tell {
		t.Fatalf("task %+v timing %+v", tv.Apps, tv.Timing)
	}

	// Once the user filled the item in, the step is done: nothing to tell.
	if _, err := v.e.PatchService(ctx, "demo-provider", PatchServiceRequest{Values: ServiceValues{"model": "qwen-plus"}}); err != nil {
		t.Fatal(err)
	}
	pv = v.plan(t, Request{App: "demo-consumer", Name: "filled-consumer"})
	if n := needOf(t, pv, "filled-consumer", "demo"); n.Status != NeedMatched || n.Tell != nil || strings.Contains(mustJSON(t, pv), `"tell"`) {
		t.Fatalf("filled in %+v", n)
	}

	// A user's own service: no tell, and none in the JSON.
	if _, err := v.reg(t).CreateService(ctx, Service{Name: "my-llm", Kind: "llm_gateway", Source: state.ServiceFromUser,
		Values: map[string]string{"base_url": "https://llm.example.com/v1", "api_key": "user-key-value-9999"}}); err != nil {
		t.Fatal(err)
	}
	pv = v.plan(t, Request{App: "demo-consumer", Name: "second-consumer", Use: map[string]string{"demo": "my-llm"}})
	if n := needOf(t, pv, "second-consumer", "demo"); n.Status != NeedMatched || n.Tell != nil || strings.Contains(mustJSON(t, pv), `"tell"`) {
		t.Fatalf("user's service %+v", n)
	}
}

// TestPlanUnknownParam: a --set key that is no parameter of the app is
// CAT-INST-02 naming the parameters the app has; one written as a
// parameter's label is pointed to its key; and the required parameter that
// was probably meant is still listed in params_missing.
func TestPlanUnknownParam(t *testing.T) {
	m := strings.Replace(demoManifest(t, "demo-consumer"), "\nuses:\n", `
params:
  - key: model
    type: string
    label: 模型名
    required: true
  - key: region_hint
    type: string
    default: demo

uses:
`, 1)
	v := setupServices(t, m)
	if _, err := v.reg(t).CreateService(context.Background(), Service{Name: "my-demo", Kind: "demo_token", Source: state.ServiceFromUser,
		Values: map[string]string{"url": "https://demo.example.com", "token": "user-token-value-9999"}}); err != nil {
		t.Fatal(err)
	}
	pv, _, err := v.samePlan(t, Request{App: "demo-consumer", Params: map[string]any{"模型名": "qwen-plus"}})
	if code(err) != CodeBadParams || pv.CanInstall || len(pv.Problems) != 1 {
		t.Fatalf("label as key: %v %+v", err, pv)
	}
	if msg := pv.Problems[0].Message; !strings.Contains(msg, "参数名是 model（模型名）") || !strings.Contains(msg, "--set model=") {
		t.Fatalf("message %q", msg)
	}
	if len(pv.Apps) != 1 || len(pv.Apps[0].ParamsMissing) != 1 || pv.Apps[0].ParamsMissing[0].Key != "model" {
		t.Fatalf("params_missing %+v", pv.Apps)
	}
	pv = v.plan(t, Request{App: "demo-consumer", Params: map[string]any{"modle": "qwen-plus"}})
	if msg := pv.Problems[0].Message; !strings.Contains(msg, "它的参数有：model（模型名）、region_hint") || len(pv.Apps[0].ParamsMissing) != 1 {
		t.Fatalf("unknown key: %q %+v", msg, pv.Apps[0].ParamsMissing)
	}
	// Given right, nothing is missing.
	pv = v.plan(t, Request{App: "demo-consumer", Params: map[string]any{"model": "qwen-plus"}})
	if !pv.CanInstall || len(pv.Apps[0].ParamsMissing) != 0 {
		t.Fatalf("right: %+v", pv)
	}
}
