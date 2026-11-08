package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// ---- fakes ----

// fakeWorld is the store, the runner and the API server hooks at once. A
// Kick plays the reconciler: the newest queued deployment goes live and its
// container "listens" on the port of the fake app server registered for it.
type fakeWorld struct {
	t  *testing.T
	st *state.Store

	mu          sync.Mutex
	servers     map[string]*httptest.Server // catalog id -> fake app
	failDeploy  map[string]bool
	wrote       []runner.WriteFilesRequest
	failWrite   bool
	adopted     map[string]bool // digest present locally
	pulled      []string
	removed     []string // image refs removed
	deleted     []string // apps deleted (with ",volumes" when volumes)
	room        int      // memory MB left; < 0 = unlimited
	used        int
	withAppHold chan struct{}
	volumes     map[string][]string // app -> Docker volumes labeled for it
	removedVols []string
	loads       []runner.LoadImageRequest
	loadErr     error
	failPull    bool
}

func newWorld(t *testing.T) *fakeWorld {
	st, err := state.Open(state.Config{Path: filepath.Join(t.TempDir(), "acornfox.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &fakeWorld{t: t, st: st, servers: map[string]*httptest.Server{}, failDeploy: map[string]bool{}, adopted: map[string]bool{}, room: -1, volumes: map[string][]string{}}
}

func (w *fakeWorld) hostPort(catalogID string) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	srv := w.servers[catalogID]
	if srv == nil {
		return 0
	}
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	n, _ := strconv.Atoi(p)
	return n
}

// runner side
func (w *fakeWorld) PullImage(ctx context.Context, app, id, ref string) (runner.PullResponse, error) {
	w.mu.Lock()
	w.pulled = append(w.pulled, ref)
	fail := w.failPull
	w.mu.Unlock()
	if fail {
		return runner.PullResponse{OK: false, Failure: &runner.Failure{Stage: "image", Code: "pull_failed", Message: "拉不到"}}, nil
	}
	return runner.PullResponse{OK: true, Image: &runner.ImageInfo{ID: "sha256:" + strings.Repeat("1", 64), App: app}}, nil
}
func (w *fakeWorld) EnsureVolume(ctx context.Context, app, name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, v := range w.volumes[app] {
		if v == name {
			return nil
		}
	}
	w.volumes[app] = append(w.volumes[app], name)
	return nil
}
func (w *fakeWorld) ListVolumes(ctx context.Context, app string) ([]runner.VolumeInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []runner.VolumeInfo
	for _, v := range w.volumes[app] {
		out = append(out, runner.VolumeInfo{Name: v, App: app})
	}
	return out, nil
}
func (w *fakeWorld) RemoveVolume(ctx context.Context, app, name string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.removedVols = append(w.removedVols, name)
	kept := w.volumes[app][:0]
	for _, v := range w.volumes[app] {
		if v != name {
			kept = append(kept, v)
		}
	}
	w.volumes[app] = kept
	return nil
}
func (w *fakeWorld) hasVolume(app, name string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, v := range w.volumes[app] {
		if v == name {
			return true
		}
	}
	return false
}
func (w *fakeWorld) ListContainers(ctx context.Context, app string) ([]runner.ContainerInfo, error) {
	a, err := w.st.GetApp(ctx, app)
	if err != nil || a.CurrentDeployment == "" {
		return nil, nil
	}
	return []runner.ContainerInfo{{Name: runner.ContainerName(app, a.CurrentDeployment), App: app, DeploymentID: a.CurrentDeployment,
		Role: runner.RoleApp, Running: true, HostPort: w.hostPort(a.CatalogID)}}, nil
}
func (w *fakeWorld) RemoveImage(ctx context.Context, app, ref string) error {
	w.mu.Lock()
	w.removed = append(w.removed, ref)
	w.mu.Unlock()
	return nil
}
func (w *fakeWorld) AdoptImage(ctx context.Context, req runner.AdoptImageRequest) (runner.ImageInfo, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.adopted[req.Digest] {
		return runner.ImageInfo{ID: req.Digest, App: req.App}, nil
	}
	return runner.ImageInfo{}, runner.ErrNotFound
}
func (w *fakeWorld) LoadImage(ctx context.Context, req runner.LoadImageRequest) (runner.LoadImageResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.loads = append(w.loads, req)
	if w.loadErr != nil {
		return runner.LoadImageResponse{}, w.loadErr
	}
	return runner.LoadImageResponse{Loaded: []string{"nousresearch/hermes-agent:v0.23.0"},
		Image: &runner.ImageInfo{ID: "sha256:" + strings.Repeat("2", 64), App: req.App, DeploymentID: req.DeploymentID}}, nil
}
func (w *fakeWorld) WriteVolumeFiles(ctx context.Context, req runner.WriteFilesRequest) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failWrite {
		return errors.New("disk full")
	}
	w.wrote = append(w.wrote, req)
	return nil
}

// hooks
func (w *fakeWorld) reserve(ctx context.Context, name string, mem int) (state.App, error) {
	w.mu.Lock()
	if w.room >= 0 && w.used+mem > w.room {
		used := w.used
		w.mu.Unlock()
		return state.App{}, &RoomError{NeedMB: mem, CapMB: w.room, AllocatedMB: used}
	}
	w.used += mem
	w.mu.Unlock()
	a, created, err := w.st.EnsureApp(ctx, name)
	if err != nil {
		return state.App{}, err
	}
	if !created {
		return state.App{}, state.ErrConflict
	}
	return w.st.UpdateApp(ctx, a.Name, func(x *state.App) error { x.MemoryMB = mem; return nil })
}
func (w *fakeWorld) delete(ctx context.Context, name string, vols bool) error {
	w.mu.Lock()
	tag := name
	if vols {
		tag += ",volumes"
	}
	w.deleted = append(w.deleted, tag)
	w.mu.Unlock()
	return w.st.DeleteApp(ctx, name, !vols)
}
func (w *fakeWorld) withApp(ctx context.Context, app string, fn func() error) error {
	w.mu.Lock()
	hold := w.withAppHold
	w.mu.Unlock()
	if hold != nil {
		<-hold
	}
	return fn()
}
func (w *fakeWorld) kick(app string) {
	ctx := context.Background()
	a, err := w.st.GetApp(ctx, app)
	if err != nil {
		return
	}
	ds, _ := w.st.PendingDeployments(ctx, app)
	for _, d := range ds {
		if w.failDeploy[a.CatalogID] {
			_, _ = w.st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error {
				x.Status = state.StatusFailed
				x.Diagnosis = &state.Diagnosis{Stage: "health", Code: "container_exited", Message: "容器启动后退出"}
				return nil
			})
			continue
		}
		_, _ = w.st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error { x.Status = state.StatusRouting; return nil })
		_ = w.st.SetCurrentDeployment(ctx, app, d.ID)
	}
}

// fakeNewAPI is a New API with setup, login and tokens.
type fakeNewAPI struct {
	mu        sync.Mutex
	admin     string
	password  string
	setupBody map[string]any
	tokens    map[int]string
	deleted   []int
	failSetup bool
	setup5xx  bool
	setups    int
	nextID    int
	options   map[string]any // PUT /api/option/ calls (key → value)
	failOpt   bool
}

func (f *fakeNewAPI) handler() http.Handler {
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, data any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "", "data": data})
	}
	no := func(w http.ResponseWriter, msg string) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": msg})
	}
	auth := func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer at-1" && r.Header.Get("New-Api-User") == "1"
	}
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) { ok(w, map[string]any{"version": "x"}) })
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.setups++
		if f.setup5xx {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if f.failSetup || f.admin != "" {
			no(w, "The system is already initialized")
			return
		}
		f.setupBody = b
		f.admin, f.password = b["username"].(string), b["password"].(string)
		ok(w, nil)
	})
	mux.HandleFunc("POST /api/user/login", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		good := b["username"] == f.admin && b["password"] == f.password && f.admin != ""
		f.mu.Unlock()
		if !good {
			no(w, "用户名或密码错误")
			return
		}
		ok(w, map[string]any{"access_token": "at-1", "user": map[string]any{"id": 1}})
	})
	mux.HandleFunc("POST /api/token/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			no(w, "unauthorized")
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.nextID++
		if f.tokens == nil {
			f.tokens = map[int]string{}
		}
		f.tokens[f.nextID] = b["name"].(string)
		f.mu.Unlock()
		ok(w, nil)
	})
	mux.HandleFunc("GET /api/token/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			no(w, "unauthorized")
			return
		}
		f.mu.Lock()
		items := []map[string]any{}
		for id, n := range f.tokens {
			items = append(items, map[string]any{"id": id, "name": n})
		}
		f.mu.Unlock()
		ok(w, map[string]any{"items": items, "total": len(items)})
	})
	mux.HandleFunc("POST /api/token/{id}/key", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			no(w, "unauthorized")
			return
		}
		ok(w, map[string]any{"key": "K" + r.PathValue("id") + strings.Repeat("x", 46)})
	})
	mux.HandleFunc("PUT /api/option/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(r) {
			no(w, "unauthorized")
			return
		}
		var b struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failOpt {
			no(w, "无权进行此操作")
			return
		}
		if f.options == nil {
			f.options = map[string]any{}
		}
		f.options[b.Key] = b.Value
		ok(w, nil)
	})
	mux.HandleFunc("DELETE /api/token/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		f.mu.Lock()
		f.deleted = append(f.deleted, id)
		delete(f.tokens, id)
		f.mu.Unlock()
		ok(w, nil)
	})
	return mux
}

type env struct {
	w      *fakeWorld
	e      *Engine
	newapi *fakeNewAPI
}

func setup(t *testing.T) *env { return setupWith(t, nil) }

// setupWith is setup with the engine's Config changed by mod first.
func setupWith(t *testing.T, mod func(*Config)) *env {
	w := newWorld(t)
	na := &fakeNewAPI{}
	naSrv := httptest.NewServer(na.handler())
	t.Cleanup(naSrv.Close)
	hermes := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = rw.Write([]byte(`{"version":"0.23.0"}`))
	}))
	t.Cleanup(hermes.Close)
	w.servers["new-api"], w.servers["hermes-agent"] = naSrv, hermes

	dir := t.TempDir()
	cfg := Config{
		Store: w.st, Runner: w, Catalog: NewCatalog(CatalogConfig{CacheDir: filepath.Join(dir, "catalog")}),
		DataDir: dir, UploadDir: dir, Version: "0.5.0", Arch: "amd64", Poll: 10 * time.Millisecond, DeployTimeout: 5 * time.Second,
	}
	if mod != nil {
		mod(&cfg)
	}
	e := New(cfg)
	e.SetHooks(Hooks{Reserve: w.reserve, Delete: w.delete, WithApp: w.withApp, Kick: w.kick,
		URL: func(ctx context.Context, a state.App) string { return "http://127.0.0.1:" + strconv.Itoa(a.PublicPort) }})
	t.Cleanup(e.Close)
	return &env{w: w, e: e, newapi: na}
}

func (v *env) install(t *testing.T, req Request) TaskView {
	t.Helper()
	tv, err := v.e.StartInstall(context.Background(), req)
	if err != nil {
		t.Fatalf("StartInstall: %v", err)
	}
	return v.wait(t, tv.ID)
}

func (v *env) wait(t *testing.T, id string) TaskView {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		tv, err := v.e.Task(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if tv.Status != state.TaskRunning {
			return tv
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return TaskView{}
}

func code(err error) string {
	var f *Fault
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}

// ---- tests ----

func TestInstallNewAPI(t *testing.T) {
	v := setup(t)
	tv := v.install(t, Request{App: "new-api"})
	if tv.Status != state.TaskSucceeded || tv.Catalog != PublicSource {
		t.Fatalf("task %+v", tv)
	}
	ctx := context.Background()
	a, err := v.w.st.GetApp(ctx, "new-api")
	if err != nil {
		t.Fatal(err)
	}
	if a.CatalogID != "new-api" || a.CatalogVersion != "v1.0.0-rc.48" || a.Exposure != "local" || a.Port != 3000 || a.HealthPath != "/api/status" || a.MemoryMB != 512 {
		t.Fatalf("app %+v", a)
	}
	envs, _ := v.w.st.ListEnv(ctx, "new-api")
	var session state.EnvVar
	for _, e := range envs {
		if e.Key == "SESSION_SECRET" {
			session = e
		}
	}
	if !session.Secret || len(session.Value) != 64 || strings.Trim(session.Value, "0123456789abcdef") != "" {
		t.Fatalf("SESSION_SECRET %+v", session)
	}
	// init called with the generated password, self-use mode on
	if v.newapi.admin != "admin" || len(v.newapi.password) != 20 || v.newapi.setupBody["SelfUseModeEnabled"] != true || v.newapi.setupBody["confirmPassword"] != v.newapi.password {
		t.Fatalf("setup %+v", v.newapi.setupBody)
	}
	// self-service sign-up is turned off right after setup
	if v.newapi.options["RegisterEnabled"] != false {
		t.Fatalf("options %+v", v.newapi.options)
	}
	if s := stepOf(tv, "new-api", "init"); s == nil || !strings.Contains(s.Message, "已关闭自助注册") {
		t.Fatalf("init step %+v", s)
	}
	if len(v.w.pulled) != 1 || !strings.HasPrefix(v.w.pulled[0], "calciumion/new-api@sha256:51401a46") {
		t.Fatalf("pulled %v", v.w.pulled)
	}
	// first login masked in the task, revealed on request
	if len(tv.FirstLogin) != 1 || tv.FirstLogin[0].Fields[1].Value != Masked || tv.FirstLogin[0].Fields[0].Value != "admin" {
		t.Fatalf("first login %+v", tv.FirstLogin)
	}
	fl, err := v.e.FirstLogin(ctx, "new-api", true)
	if err != nil || fl.Fields[1].Value != v.newapi.password {
		t.Fatalf("reveal %+v %v", fl, err)
	}
	// stored encrypted: the password is not in the database text
	cs, _ := v.w.st.GetCatalogSecrets(ctx, "new-api")
	if strings.Contains(cs.ParamsEnc+cs.FirstLoginEnc, v.newapi.password) || !strings.HasPrefix(cs.ParamsEnc, "v1:") {
		t.Fatal("secrets stored in clear")
	}
	raw, _ := v.w.st.GetInstallTask(ctx, tv.ID)
	if strings.Contains(raw.Data, v.newapi.password) || strings.Contains(raw.Data, session.Value) {
		t.Fatal("task data holds a secret")
	}
	for _, s := range tv.Steps {
		if s.Status == StepFailed || s.Status == StepPending {
			t.Fatalf("step %+v", s)
		}
	}
	// installing it again under the same name is refused
	if _, err := v.e.StartInstall(ctx, Request{App: "new-api"}); code(err) != CodeNameTaken {
		t.Fatalf("again: %v", err)
	}
}

// TestInstallNewAPISignUpNotClosed: turning off sign-up is best effort; the
// install goes on and the init step says how to do it by hand.
func TestInstallNewAPISignUpNotClosed(t *testing.T) {
	v := setup(t)
	v.newapi.failOpt = true
	tv := v.install(t, Request{App: "new-api"})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v", tv)
	}
	s := stepOf(tv, "new-api", "init")
	if s == nil || s.Status != StepDone || !strings.Contains(s.Message, "没能关闭自助注册") || !strings.Contains(s.Message, "允许新用户注册") {
		t.Fatalf("init step %+v", s)
	}
}

// stepOf finds the named step of app in a task.
func stepOf(tv TaskView, app, name string) *StepView {
	for i := range tv.Steps {
		if tv.Steps[i].App == app && tv.Steps[i].Name == name {
			return &tv.Steps[i]
		}
	}
	return nil
}

func TestInstallHermesWithLocalGateway(t *testing.T) {
	v := setup(t)
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("new-api %+v", tv.Fault)
	}
	hermesDigest := "sha256:adb7890916de63245523492be8f68f9d2095fecbec4e2a6dab7960381f0fc002"
	v.w.adopted[hermesDigest] = true // pre-loaded image
	tv := v.install(t, Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{"source": "local_gateway"}}})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("hermes %+v", tv.Fault)
	}
	ctx := context.Background()
	a, _ := v.w.st.GetApp(ctx, "hermes-agent")
	if strings.Join(a.Command, " ") != "gateway run" || a.PidsLimit != 512 || len(a.Links) != 1 || a.Links[0] != "new-api" || a.MemoryMB != 2048 {
		t.Fatalf("app %+v", a)
	}
	if len(v.w.pulled) != 1 {
		t.Fatalf("hermes should be adopted, pulled %v", v.w.pulled)
	}
	if len(v.w.wrote) != 1 {
		t.Fatalf("wrote %d", len(v.w.wrote))
	}
	wr := v.w.wrote[0]
	f := wr.Files[0]
	cfg := string(f.Content)
	if wr.MountPath != "/opt/data" || f.Path != "config.yaml" || f.Mode != 0o600 || f.UID != 10000 || f.GID != 10000 || !f.Overwrite {
		t.Fatalf("file %+v", wr)
	}
	if !strings.Contains(cfg, `base_url: "http://new-api:3000/v1"`) || !strings.Contains(cfg, `api_key: "sk-K1`) ||
		!strings.Contains(cfg, `default: "deepseek-chat"`) || !strings.Contains(cfg, "mode: manual") {
		t.Fatalf("config:\n%s", cfg)
	}
	if len(v.newapi.tokens) != 1 || v.newapi.tokens[1] != "acornfox-hermes-agent" {
		t.Fatalf("tokens %v", v.newapi.tokens)
	}
	envs, _ := v.w.st.ListEnv(ctx, "hermes-agent")
	found := false
	for _, e := range envs {
		if e.Key == "HERMES_DASHBOARD_BASIC_AUTH_PASSWORD" && e.Secret && len(e.Value) == 24 {
			found = true
		}
	}
	if !found {
		t.Fatalf("dashboard password env %+v", envs)
	}
}

func TestRecipeLinksAndReuse(t *testing.T) {
	v := setup(t)
	tv := v.install(t, Request{Recipe: "hermes-with-new-api", Params: map[string]any{"hermes-agent.llm": map[string]any{"model": "qwen-max"}}})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("recipe %+v", tv.Fault)
	}
	if len(tv.Apps) != 2 || tv.Apps[0].App != "new-api" || tv.Apps[1].App != "hermes-agent" || len(tv.FirstLogin) != 2 {
		t.Fatalf("apps %+v", tv.Apps)
	}
	if !strings.Contains(string(v.w.wrote[0].Files[0].Content), `default: "qwen-max"`) {
		t.Fatalf("model not kept:\n%s", v.w.wrote[0].Files[0].Content)
	}
	a, _ := v.w.st.GetApp(context.Background(), "hermes-agent")
	if a.CatalogSource != "recipe:hermes-with-new-api" {
		t.Fatalf("source %q", a.CatalogSource)
	}
	// The single-app recipe now reuses the installed New API.
	tv = v.install(t, Request{Recipe: "new-api"})
	if tv.Status != state.TaskSucceeded || !tv.Apps[0].Reused {
		t.Fatalf("reuse %+v", tv)
	}
}

func TestInitFailureCleansUp(t *testing.T) {
	v := setup(t)
	v.newapi.failSetup = true
	tv := v.install(t, Request{App: "new-api"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeInit {
		t.Fatalf("task %+v", tv)
	}
	if _, err := v.w.st.GetApp(context.Background(), "new-api"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("app left behind: %v", err)
	}
	// The app is deleted without touching volumes by label; the one volume
	// this task created is removed by name.
	if len(v.w.deleted) != 1 || v.w.deleted[0] != "new-api" || len(v.w.removed) != 1 || !strings.HasPrefix(v.w.removed[0], "acornfox/new-api:") {
		t.Fatalf("cleanup deleted=%v removed=%v", v.w.deleted, v.w.removed)
	}
	if len(v.w.removedVols) != 1 || v.w.removedVols[0] != "af-new-api-1" || len(v.w.volumes["new-api"]) != 0 {
		t.Fatalf("volumes removed=%v left=%v", v.w.removedVols, v.w.volumes)
	}
	if v.newapi.setups != 1 {
		t.Fatalf("a refused setup must not be sent again: %d calls", v.newapi.setups)
	}
	if tv.Apps[0].URL != "" {
		t.Fatal("url kept after cleanup")
	}
}

func TestKeepVolumesOnFailure(t *testing.T) {
	v := setup(t)
	v.w.failDeploy["new-api"] = true
	tv := v.install(t, Request{App: "new-api", KeepVolumesOnFailure: true})
	if tv.Fault == nil || tv.Fault.Code != CodeStart || tv.Fault.Diagnosis == nil {
		t.Fatalf("task %+v", tv.Fault)
	}
	if len(v.w.deleted) != 1 || v.w.deleted[0] != "new-api" {
		t.Fatalf("deleted %v", v.w.deleted)
	}
	if len(v.w.removedVols) != 0 || !v.w.hasVolume("new-api", "af-new-api-1") {
		t.Fatalf("volume not kept: removed=%v left=%v", v.w.removedVols, v.w.volumes)
	}
	if !tv.KeepVolumes || len(tv.Apps[0].Volumes) != 1 || tv.Apps[0].Volumes[0] != "af-new-api-1" {
		t.Fatalf("kept volumes not reported: %+v", tv)
	}
}

func TestFilesFailureDeletesGatewayToken(t *testing.T) {
	v := setup(t)
	v.install(t, Request{App: "new-api"})
	v.w.failWrite = true
	tv := v.install(t, Request{App: "hermes-agent", Params: map[string]any{"llm": "local"}})
	if tv.Fault == nil || tv.Fault.Code != CodeFiles {
		t.Fatalf("task %+v", tv.Fault)
	}
	if len(v.newapi.deleted) != 1 || len(v.newapi.tokens) != 0 {
		t.Fatalf("token not removed: deleted=%v tokens=%v", v.newapi.deleted, v.newapi.tokens)
	}
	if _, err := v.w.st.GetApp(context.Background(), "new-api"); err != nil {
		t.Fatal("the reused gateway must stay")
	}
}

func TestRefusals(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	cases := []struct {
		name string
		req  Request
		want string
	}{
		{"unknown", Request{App: "nope"}, CodeUnknown},
		{"version", Request{App: "new-api", Version: "v0.1"}, CodeUnknown},
		{"both", Request{App: "new-api", Recipe: "new-api"}, CodeBadParams},
		{"unknown param", Request{App: "new-api", Params: map[string]any{"x": "1"}}, CodeBadParams},
		{"hidden param", Request{App: "new-api", Params: map[string]any{"session_secret": "1234567890"}}, CodeBadParams},
		{"pattern", Request{App: "new-api", Params: map[string]any{"admin_username": "bad name!"}}, CodeBadParams},
		{"short secret", Request{App: "new-api", Params: map[string]any{"admin_password": "short"}}, CodeBadParams},
		{"bad name", Request{App: "new-api", Name: "Bad"}, CodeBadParams},
		{"platform", Request{App: "hermes-agent", Params: map[string]any{"llm": "platform"}}, CodePlatformLLM},
		{"no gateway", Request{App: "hermes-agent", Params: map[string]any{"llm": "local"}}, CodeNoGateway},
		{"llm missing", Request{App: "hermes-agent"}, CodeBadParams},
		{"own incomplete", Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{"source": "own", "api_key": "k"}}}, CodeBadParams},
		{"own newline", Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{"source": "own", "base_url": "https://a/v1", "api_key": "k\nx", "model": "m"}}}, CodeBadParams},
	}
	for _, c := range cases {
		if _, err := v.e.StartInstall(ctx, c.req); code(err) != c.want {
			t.Errorf("%s: want %s, got %v", c.name, c.want, err)
		}
	}

	old := New(Config{Store: v.w.st, Runner: v.w, Catalog: v.e.cfg.Catalog, DataDir: t.TempDir(), Version: "0.4.9", Arch: "amd64"})
	if _, err := old.StartInstall(ctx, Request{App: "new-api"}); code(err) != CodeTooOld {
		t.Errorf("too old: %v", err)
	}
	arm := New(Config{Store: v.w.st, Runner: v.w, Catalog: v.e.cfg.Catalog, DataDir: t.TempDir(), Version: "0.5.0", Arch: "riscv64"})
	if _, err := arm.StartInstall(ctx, Request{App: "new-api"}); code(err) != CodeArch {
		t.Errorf("arch: %v", err)
	}
}

func TestBusyAndNoMemory(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	v.w.mu.Lock()
	v.w.withAppHold = make(chan struct{})
	v.w.mu.Unlock()
	first, err := v.e.StartInstall(ctx, Request{App: "new-api"})
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the first task has created its app record (it then blocks
	// before the image step): the second request must still be "busy", not
	// "name taken".
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := v.w.st.GetApp(ctx, "new-api"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("app record not created")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// recover must leave a task of this process alone
	v.e.recover(ctx)
	if tv, _ := v.e.Task(ctx, first.ID); tv.Status != state.TaskRunning {
		t.Fatalf("recover touched a live task: %+v", tv)
	}
	_, err = v.e.StartInstall(ctx, Request{App: "new-api"})
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeBusy || f.TaskID != first.ID {
		t.Fatalf("busy: %v", err)
	}
	// a recipe that would reuse the app being installed is busy as well
	if _, err := v.e.StartInstall(ctx, Request{Recipe: "new-api"}); code(err) != CodeBusy {
		t.Fatalf("recipe while installing: %v", err)
	}
	hold := v.w.withAppHold
	v.w.mu.Lock()
	v.w.withAppHold = nil
	v.w.mu.Unlock()
	close(hold)
	if tv := v.wait(t, first.ID); tv.Status != state.TaskSucceeded {
		t.Fatalf("first %+v", tv.Fault)
	}

	v2 := setup(t)
	v2.w.room = 200 // below New API's minimum of 256
	tv := v2.install(t, Request{App: "new-api"})
	if tv.Fault == nil || tv.Fault.Code != CodeNoMemory {
		t.Fatalf("memory %+v", tv.Fault)
	}
	// 300 MB: the recommended 512 does not fit, the minimum 256 does
	v3 := setup(t)
	v3.w.room = 300
	if tv := v3.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded || tv.Apps[0].MemoryMB != 256 {
		t.Fatalf("fallback to min %+v", tv)
	}
}

func TestRecoverInterrupted(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	if _, _, err := v.w.st.EnsureApp(ctx, "new-api"); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(TaskView{Apps: []AppResult{{App: "new-api", Created: true}}})
	if _, err := v.w.st.CreateInstallTask(ctx, state.InstallTask{ID: "0123456789ab", Kind: "app", Target: "new-api", Status: state.TaskRunning, Data: string(data)}); err != nil {
		t.Fatal(err)
	}
	v.e.recover(ctx)
	tv, _ := v.e.Task(ctx, "0123456789ab")
	if tv.Status != state.TaskFailed || tv.Fault.Code != CodeInterrupted {
		t.Fatalf("task %+v", tv)
	}
	if _, err := v.w.st.GetApp(ctx, "new-api"); !errors.Is(err, state.ErrNotFound) {
		t.Fatal("half-installed app kept")
	}
}

func TestListAndDetail(t *testing.T) {
	v := setup(t)
	ctx := context.Background()
	l, err := v.e.List(ctx)
	if err != nil || l.Origin != "snapshot" || len(l.Apps) != 2 || len(l.Recipes) != 3 {
		t.Fatalf("list %+v %v", l, err)
	}
	// The list carries each app's icon from the package built into the
	// program, without downloading anything.
	for _, a := range l.Apps {
		if a.IconType == "" || a.Icon == "" {
			t.Fatalf("list app %s has no icon", a.ID)
		}
	}
	again, _ := v.e.List(ctx)
	if again.Apps[0].Icon != l.Apps[0].Icon {
		t.Fatal("cached list icon differs")
	}
	d, err := v.e.Detail(ctx, "hermes-agent")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range d.Params {
		if p.Key == "dashboard_password" || p.Key == "dashboard_session_secret" {
			t.Fatal("hidden param shown")
		}
	}
	if len(d.Params) != 1 || d.Params[0].LLM == nil || d.Params[0].LLM.Local.Available || d.Params[0].LLM.Platform.Code != CodePlatformLLM {
		t.Fatalf("params %+v", d.Params)
	}
	if !strings.Contains(d.Icon, "<svg") || d.IconType != "svg" || d.Port != 9119 || d.Exposure != "local" {
		t.Fatalf("detail %+v", d)
	}
	if _, err := v.e.Detail(ctx, "nope"); code(err) != CodeUnknown {
		t.Fatalf("unknown: %v", err)
	}
	// The program ships a release key; a test must never reach the real
	// catalog site, so drop the keys for the "no keys" case.
	saved := catalog.TrustedKeys
	catalog.TrustedKeys = nil
	defer func() { catalog.TrustedKeys = saved }()
	st := v.e.Catalog().Sync(ctx)
	if !st.Skipped || st.Code != "CAT-SYNC-01" {
		t.Fatalf("sync without keys %+v", st)
	}
}
