// Package appinstall installs apps from the app catalog in one go: create the
// app, its environment and data volumes, write config files into a volume,
// fetch the image pinned by digest, start it, check its health, run its init
// calls, and keep its first-login fields (encrypted). One install is a task
// with steps that can be queried; when a step fails, what this task created
// is removed again.
//
// Operating notes (who picks this up next):
//
//   - Used by: the API server (GET/POST /v1/catalog…, GET /v1/apps/{app}/first-login),
//     hence the console, `acornfox catalog …`, `acornfox install …` and the Skill.
//   - Needs: the runner's app-center operations (runner.CatalogAPI); the
//     catalog (built-in snapshot, verified cache under <data>/catalog);
//     the download gateway only for large images that name an artifact.
//   - Side effects: creates apps, volumes, images and containers; writes
//     <data>/secrets/catalog.key on first use; an install that uses the local
//     model gateway creates a token in that gateway.
//   - Verify: `acornfox install status <task>`; GET /v1/catalog/install/{task};
//     `acornfox status <app>`.
//   - Stop / roll back: a failed install cleans up after itself (or keeps the
//     volumes with keep_volumes_on_failure); a finished install is removed
//     with `acornfox delete <app> [--volumes]`.
//   - First look: the task's fault code (CAT-INST-*, faults.go) and, for
//     CAT-INST-15/16, the deployment diagnosis and `acornfox logs <app>`.
package appinstall

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Store is what the engine needs from *state.Store.
type Store interface {
	GetApp(ctx context.Context, name string) (state.App, error)
	ListApps(ctx context.Context) ([]state.App, error)
	UpdateApp(ctx context.Context, name string, fn func(*state.App) error) (state.App, error)
	SetEnv(ctx context.Context, v state.EnvVar) error
	AddVolume(ctx context.Context, app, path string, auto bool) (state.Volume, bool, error)
	ListVolumes(ctx context.Context, app string) ([]state.Volume, error)
	CreateDeployment(ctx context.Context, in state.NewDeployment) (state.Deployment, bool, error)
	GetDeployment(ctx context.Context, id string) (state.Deployment, error)

	CreateInstallTask(ctx context.Context, t state.InstallTask) (state.InstallTask, error)
	UpdateInstallTask(ctx context.Context, id string, fn func(*state.InstallTask) error) (state.InstallTask, error)
	GetInstallTask(ctx context.Context, id string) (state.InstallTask, error)
	ListInstallTasks(ctx context.Context, status string, limit int) ([]state.InstallTask, error)
	SetCatalogSecrets(ctx context.Context, cs state.CatalogSecrets) error
	GetCatalogSecrets(ctx context.Context, app string) (state.CatalogSecrets, error)
	GetKeptCatalogSecrets(ctx context.Context, app string) (state.KeptCatalogSecrets, error)
	DeleteKeptCatalogSecrets(ctx context.Context, app string) error
}

// Runner is what the engine needs from the runner; it must also implement
// runner.CatalogAPI (checked at install time, CAT-INST-20 otherwise).
type Runner interface {
	PullImage(ctx context.Context, app, deploymentID, ref string) (runner.PullResponse, error)
	EnsureVolume(ctx context.Context, app, name string) error
	ListContainers(ctx context.Context, app string) ([]runner.ContainerInfo, error)
	RemoveImage(ctx context.Context, app, ref string) error
	// ListVolumes lists Docker volumes labeled for app (leftovers of an app
	// of the same name are refused, CAT-INST-24).
	ListVolumes(ctx context.Context, app string) ([]runner.VolumeInfo, error)
	// RemoveVolume removes one volume a failed install created.
	RemoveVolume(ctx context.Context, app, name string) error
}

// Gateway issues a download address for one file (W2). *gatewayclient.Client
// satisfies it; nil means no gateway.
type Gateway interface {
	Issue(ctx context.Context, fileID, purpose string) (gatewayclient.Issued, error)
}

// RoomError is what Hooks.Reserve returns when the memory does not fit.
type RoomError struct {
	NeedMB, CapMB, AllocatedMB int
}

func (e *RoomError) Error() string {
	return "memory cap exceeded"
}

// Hooks are the API server's operations the engine reuses, set with SetHooks.
type Hooks struct {
	// Reserve creates the app with memMB under the server-wide memory cap
	// (*RoomError when it does not fit; state.ErrConflict when it exists or
	// no port is left).
	Reserve func(ctx context.Context, name string, memMB int) (state.App, error)
	// Delete removes an app: containers, network, record, and the volumes
	// when deleteVolumes. A failed install's cleanup passes false and removes
	// only the volumes it created itself.
	Delete func(ctx context.Context, name string, deleteVolumes bool) error
	// WithApp serializes fn with the app's reconcile rounds.
	WithApp func(ctx context.Context, app string, fn func() error) error
	// Kick starts a reconcile round.
	Kick func(app string)
	// URL is the address the app is opened at.
	URL func(ctx context.Context, a state.App) string
}

// Config configures New.
type Config struct {
	Store     Store
	Runner    Runner
	Catalog   *Catalog
	DataDir   string  // <data>/secrets/catalog.key
	UploadDir string  // downloaded image archives (the runner reads them)
	Gateway   Gateway // nil: large images come from public sources only
	Version   string  // this AcornFox's version, for min_platform
	// Platform is what the platform variables are filled from.
	Platform PlatformEnv
	// DiskFree reports the free space an install can use; nil (or an error)
	// skips the disk check.
	DiskFree func(ctx context.Context) (DiskSpace, error)
	Arch     string // Docker host CPU (amd64|arm64); "" => runtime.GOARCH
	HTTP     *http.Client
	Log      *slog.Logger

	DeployTimeout time.Duration // wait for the deployment to go live (0: 15 min)
	Poll          time.Duration // progress polling (0: 1 s)
}

// Engine runs install tasks.
type Engine struct {
	cfg     Config
	box     *secretBox
	adapter *newAPIAdapter

	mu      sync.Mutex
	hooks   Hooks
	running map[string]string // app name -> task id
	mine    map[string]bool   // ids of tasks this process started (recover skips them)
	base    context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// New builds an engine. Call SetHooks before Start.
func New(cfg Config) *Engine {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Arch == "" {
		cfg.Arch = runtime.GOARCH
	}
	if cfg.DeployTimeout <= 0 {
		cfg.DeployTimeout = 15 * time.Minute
	}
	if cfg.Poll <= 0 {
		cfg.Poll = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		cfg: cfg, box: newSecretBox(cfg.DataDir), adapter: &newAPIAdapter{http: cfg.HTTP},
		running: map[string]string{}, mine: map[string]bool{}, base: ctx, cancel: cancel,
	}
}

// SetHooks wires the API server's operations.
func (e *Engine) SetHooks(h Hooks) {
	e.mu.Lock()
	e.hooks = h
	e.mu.Unlock()
}

func (e *Engine) h() Hooks {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hooks
}

// Catalog returns the catalog holder.
func (e *Engine) Catalog() *Catalog { return e.cfg.Catalog }

// Start fails and cleans up tasks a previous run left running, then keeps the
// catalog synced until ctx ends.
func (e *Engine) Start(ctx context.Context) {
	e.recover(ctx)
	if e.cfg.Catalog != nil {
		go e.cfg.Catalog.Run(ctx)
	}
}

// Close cancels running tasks and waits for them.
func (e *Engine) Close() {
	e.cancel()
	e.wg.Wait()
}

// Request is POST /v1/catalog/install.
type Request struct {
	App    string `json:"app,omitempty"`
	Recipe string `json:"recipe,omitempty"`
	// Catalog picks the catalog: "" the public one first, then the private
	// ones by id; "public"; or a private catalog id ("cat-…").
	Catalog string         `json:"catalog,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	// Platform changes platform variables marked user_can_set for every app
	// of this request, e.g. {"tz": "Asia/Tokyo"} (CLI --set platform.tz=…).
	Platform             map[string]string `json:"platform,omitempty"`
	Name                 string            `json:"name,omitempty"`
	Version              string            `json:"version,omitempty"`
	KeepVolumesOnFailure bool              `json:"keep_volumes_on_failure,omitempty"`
	// ReuseData installs on top of the data volumes an earlier app of the
	// same name left behind (deleted with its data kept) instead of refusing
	// with CAT-INST-24. Those volumes are never removed by a failed install;
	// init steps are skipped (the old data has been initialized) and the
	// first-login values init would have written say they are the old ones.
	ReuseData bool `json:"reuse_data,omitempty"`
}

// planned is one app of a task.
type planned struct {
	app    *App
	name   string
	reuse  bool
	source string // catalog | recipe:<id>
	user   map[string]any
	vals   Values
	link   string // provider app name this app's llm param is linked to
	// reuseData: the request allows taking over old data volumes;
	// reusedVolumes are the ones actually taken over (set by create).
	reuseData     bool
	reusedVolumes []string
	// restored: the parameters kept when the old app was deleted with its
	// data were put back (same passwords and keys as the data); initStale:
	// no kept record and init said the data was initialized already.
	restored  bool
	initStale bool
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

var versionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// versionAtLeast compares the numeric part of have to min ("0.5.0-dev.3"
// counts as 0.5.0). An unparsable have is too old; an unparsable min passes.
func versionAtLeast(have, min string) bool {
	m := versionRe.FindStringSubmatch(min)
	if m == nil {
		return true
	}
	h := versionRe.FindStringSubmatch(have)
	if h == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		a, _ := strconv.Atoi(h[i])
		b, _ := strconv.Atoi(m[i])
		if a != b {
			return a > b
		}
	}
	return true
}

func (e *Engine) archSupported(platforms []string) bool {
	if len(platforms) == 0 {
		return true
	}
	for _, p := range platforms {
		if p == "linux/"+e.cfg.Arch {
			return true
		}
	}
	return false
}

// checkManifest rejects what this version cannot install.
func (e *Engine) checkManifest(a *App) error {
	m := a.Manifest
	if len(m.Release.Images) != 1 {
		return fault(CodeUnsupported, nil, "%s 有 %d 个镜像，这个版本只能装单容器应用", m.ID, len(m.Release.Images))
	}
	if primaryEndpoint(m) == nil {
		return fault(CodeUnsupported, nil, "%s 的清单没有入口端口", m.ID)
	}
	for _, ev := range m.Env {
		if ev.From != "" && !strings.HasPrefix(ev.From, "params.") && !strings.HasPrefix(ev.From, "platform.") {
			return fault(CodeUnsupported, nil, "%s 的环境变量 %s 引用了 %s，这个版本不支持", m.ID, ev.Key, ev.From)
		}
	}
	// Platform variables this binary does not have (a newer catalog may use
	// variables added later) are refused here, before anything is created or
	// downloaded, rather than when the env or a file is rendered.
	for _, name := range catalog.PlatformRefs(m) {
		pv, ok := catalog.LookupPlatformVar(name)
		if !ok {
			return fault(CodeUnsupported, nil, "%s 用到平台变量 %s，这个版本的 AcornFox 还没有，请先升级 AcornFox", m.ID, name)
		}
		if pv.ViaParam {
			return fault(CodeUnsupported, nil, "%s 的清单直接引用了平台变量 %s（只能经大模型参数使用），这个版本装不了", m.ID, name)
		}
	}
	if !versionAtLeast(e.cfg.Version, m.Lifecycle.MinPlatform) {
		return fault(CodeTooOld, nil, "%s 要求 AcornFox %s 以上，这台是 %s", m.ID, m.Lifecycle.MinPlatform, e.cfg.Version)
	}
	if !e.archSupported(m.Resources.Platforms) {
		return fault(CodeArch, nil, "%s 不支持这台机器的 CPU（linux/%s），只支持 %s", m.ID, e.cfg.Arch, strings.Join(m.Resources.Platforms, "、"))
	}
	return nil
}

func primaryEndpoint(m *catalog.Manifest) *catalog.Endpoint {
	for i := range m.Endpoints {
		if m.Endpoints[i].Primary {
			return &m.Endpoints[i]
		}
	}
	if len(m.Endpoints) > 0 {
		return &m.Endpoints[0]
	}
	return nil
}

// installedCatalogApps lists apps installed from the catalog, by catalog id.
func (e *Engine) installedCatalogApps(ctx context.Context) (map[string][]state.App, error) {
	apps, err := e.cfg.Store.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string][]state.App{}
	for _, a := range apps {
		if a.CatalogID != "" {
			out[a.CatalogID] = append(out[a.CatalogID], a)
		}
	}
	return out, nil
}

// gatewayProviders returns installed, live apps that provide llm_gateway
// (according to any catalog in use).
func (e *Engine) gatewayProviders(ctx context.Context) ([]state.App, error) {
	vs, err := e.cfg.Catalog.views()
	if err != nil {
		return nil, err
	}
	provides := map[string]bool{}
	for _, v := range vs {
		for _, a := range v.idx.Apps {
			for _, p := range a.Provides {
				if p == "llm_gateway" {
					provides[a.ID] = true
				}
			}
		}
	}
	apps, err := e.cfg.Store.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	var out []state.App
	for _, a := range apps {
		if provides[a.CatalogID] && a.CurrentDeployment != "" {
			out = append(out, a)
		}
	}
	return out, nil
}

// splitParams gives each planned app its own user values. Keys may carry an
// "<app id>." prefix (recipes); an unprefixed key goes to every app that
// has that parameter. A key that matches nothing is CAT-INST-02.
func splitParams(plan []*planned, user map[string]any) error {
	for _, p := range plan {
		p.user = map[string]any{}
	}
	keys := make([]string, 0, len(user))
	for k := range user {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		used := false
		for _, p := range plan {
			id := p.app.Manifest.ID
			if strings.HasPrefix(k, id+".") {
				p.user[strings.TrimPrefix(k, id+".")] = user[k]
				used = true
				continue
			}
			if !strings.Contains(k, ".") {
				for _, prm := range p.app.Manifest.Params {
					if prm.Key == k {
						p.user[k] = user[k]
						used = true
					}
				}
			}
		}
		if !used {
			return fault(CodeBadParams, nil, "没有哪个应用有参数 %s", k)
		}
	}
	return nil
}

// plan resolves what a request will install, synchronously: everything that
// can be refused before a task starts is refused here.
func (e *Engine) plan(ctx context.Context, req Request) (kind, target string, plan []*planned, userSteps []string, err error) {
	if (req.App == "") == (req.Recipe == "") {
		return "", "", nil, nil, fault(CodeBadParams, nil, "要给 app 或 recipe 其中一个")
	}

	installed, err := e.installedCatalogApps(ctx)
	if err != nil {
		return "", "", nil, nil, fault(CodeInternal, err, "读应用列表失败")
	}
	if req.App != "" {
		a, err := e.cfg.Catalog.AppIn(ctx, req.Catalog, req.App)
		if err != nil {
			return "", "", nil, nil, e.disconnectedHint(err, "", installed[req.App])
		}
		if req.Version != "" && req.Version != a.Index.Version {
			return "", "", nil, nil, fault(CodeUnknown, nil, "目录里 %s 只有版本 %s", req.App, a.Index.Version)
		}
		name := req.Name
		if name == "" {
			name = req.App
		}
		if !state.ValidAppName(name) {
			return "", "", nil, nil, fault(CodeBadParams, nil, "应用名 %s 不合法：小写字母开头，只用小写字母、数字和 -，2～40 个字符", name)
		}
		plan = []*planned{{app: a, name: name, source: "catalog"}}
		kind, target, userSteps = "app", req.App, a.Manifest.Delivery.UserSteps
	} else {
		if req.Name != "" || req.Version != "" {
			return "", "", nil, nil, fault(CodeBadParams, nil, "配方不能指定 name 或 version")
		}
		r, origin, err := e.cfg.Catalog.RecipeIn(req.Catalog, req.Recipe)
		if err != nil {
			var fromRecipe []state.App
			for _, have := range installed {
				for _, a := range have {
					if a.CatalogSource == "recipe:"+req.Recipe {
						fromRecipe = append(fromRecipe, a)
					}
				}
			}
			return "", "", nil, nil, e.disconnectedHint(err, "配方 "+req.Recipe, fromRecipe)
		}
		from := origin
		if from == "" {
			from = PublicSource
		}
		byID := map[string]*planned{}
		for _, ra := range r.Apps {
			a, err := e.cfg.Catalog.AppIn(ctx, from, ra.App)
			if err != nil {
				return "", "", nil, nil, err
			}
			p := &planned{app: a, name: ra.App, source: "recipe:" + r.ID}
			reuse := ra.ReuseInstalled == nil || *ra.ReuseInstalled
			if have := installed[ra.App]; reuse && len(have) > 0 {
				p.name, p.reuse = have[0].Name, true
				// Still being installed by another task: not usable yet.
				if err := e.busy(p.name); err != nil {
					return "", "", nil, nil, err
				}
			}
			plan = append(plan, p)
			byID[ra.App] = p
		}
		for _, l := range r.Links {
			from, to := byID[l.From], byID[l.To]
			if from == nil || to == nil || l.Uses != "llm_gateway" {
				return "", "", nil, nil, fault(CodeUnsupported, nil, "配方 %s 的连接 %s→%s 这个版本不支持", r.ID, l.From, l.To)
			}
			to.link = from.name
		}
		kind, target, userSteps = "recipe", req.Recipe, r.Delivery.UserSteps
		if err := splitParams(plan, req.Params); err != nil {
			return "", "", nil, nil, err
		}
		for i, ra := range r.Apps {
			for k, v := range ra.Preset {
				if _, ok := plan[i].user[k]; !ok {
					plan[i].user[k] = v
				}
			}
		}
	}
	if req.App != "" {
		plan[0].user = req.Params
		if plan[0].user == nil {
			plan[0].user = map[string]any{}
		}
	}

	providers, err := e.gatewayProviders(ctx)
	if err != nil {
		return "", "", nil, nil, AsFault(err)
	}
	needMB, archive := 0, int64(0)
	for _, p := range plan {
		if p.reuse {
			continue
		}
		if err := e.checkManifest(p.app); err != nil {
			return "", "", nil, nil, err
		}
		// A running install of this name has already created its app
		// record: report it as busy (with its task), not as a name clash
		// whose advice (delete the old one) would break the running task.
		if err := e.busy(p.name); err != nil {
			return "", "", nil, nil, err
		}
		if _, err := e.cfg.Store.GetApp(ctx, p.name); err == nil {
			return "", "", nil, nil, fault(CodeNameTaken, nil, "已经有叫 %s 的应用", p.name)
		} else if !errors.Is(err, state.ErrNotFound) {
			return "", "", nil, nil, fault(CodeInternal, err, "读应用失败")
		}
		p.reuseData = req.ReuseData
		if len(p.app.Manifest.Volumes) > 0 && !p.reuseData {
			if err := e.checkOldVolumes(ctx, p.name); err != nil {
				return "", "", nil, nil, err
			}
		}
		vals, err := resolveParams(p.app.Manifest, p.user)
		if err != nil {
			return "", "", nil, nil, err
		}
		vals[platformKey] = e.platformValues(req.Platform)
		if p.reuseData {
			if err := e.restoreKept(ctx, p, vals); err != nil {
				return "", "", nil, nil, err
			}
		}
		if err := e.resolveLLM(p, vals, providers); err != nil {
			return "", "", nil, nil, err
		}
		p.vals = vals
		needMB += p.app.Manifest.Resources.DiskMB.Min
		if e.cfg.Gateway != nil {
			for _, img := range p.app.Manifest.Release.Images {
				if img.Artifact != "" && img.ArtifactSHA256 != "" {
					archive += img.ArtifactSize
				}
			}
		}
	}
	if err := e.checkDisk(ctx, needMB, archive); err != nil {
		return "", "", nil, nil, err
	}
	return kind, target, plan, userSteps, nil
}

// DiskSpace is the free space an install can use: the partition Docker
// keeps images on, and the one gateway image archives are downloaded to
// (the upload directory) before they are loaded.
type DiskSpace struct {
	DockerFree uint64
	DockerPath string
	UploadFree uint64 // 0 with UploadPath "" when unknown (not checked)
	UploadPath string
	Same       bool // both directories are on one partition
}

// restoreKept puts back, for a reuse_data install, the parameters kept when
// the old app of this name was deleted with its data: every parameter the
// user did not give now (passwords, secrets, the admin name) gets its old
// value, so the app finds its data initialized with the same ones. Old data
// of another catalog app is refused (CAT-INST-24). No kept record (deleted by
// an older version, or never from the catalog) is not an error: the install
// then goes on with new values and finds out at init.
func (e *Engine) restoreKept(ctx context.Context, p *planned, vals Values) error {
	k, err := e.cfg.Store.GetKeptCatalogSecrets(ctx, p.name)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fault(CodeInternal, err, "读保留的参数失败")
	}
	if k.CatalogID != p.app.Manifest.ID {
		return fault(CodeOldVolumes, nil, "留下的数据是 %s 的，不能接到 %s 上；用 --name 换个名字装", k.CatalogID, p.app.Manifest.ID)
	}
	plain, err := e.box.open(k.ParamsEnc)
	if err != nil {
		return fault(CodeInternal, err, "保留的参数解不开（数据目录里的 secrets/catalog.key 换过？）")
	}
	var old map[string]any
	if err := json.Unmarshal(plain, &old); err != nil {
		return fault(CodeInternal, err, "保留的参数格式不对")
	}
	for _, prm := range p.app.Manifest.Params {
		if prm.Type == "llm_provider" {
			continue // chosen again for this install
		}
		if _, given := p.user[prm.Key]; given {
			continue
		}
		if s, ok := old[prm.Key].(string); ok {
			vals[prm.Key] = s
		}
	}
	p.restored = true
	return nil
}

// checkDisk refuses an install that does not fit (CAT-INST-27). needMB is
// the apps' disk_mb.min (which counts the archive, the unpacked image and
// the first data together); archive is the bytes of the gateway image
// archives this install may download. On one partition needMB must fit; on
// two, the Docker partition must hold needMB and the upload partition the
// archives. When the free space cannot be read the install goes ahead: a
// full disk then fails at the download or import step with its own code.
func (e *Engine) checkDisk(ctx context.Context, needMB int, archive int64) error {
	if needMB <= 0 || e.cfg.DiskFree == nil {
		return nil
	}
	ds, err := e.cfg.DiskFree(ctx)
	if err != nil {
		e.cfg.Log.Warn("app center: free disk unknown, not checked", "err", err)
		return nil
	}
	if ds.DockerFree < uint64(needMB)<<20 {
		return fault(CodeNoDisk, nil, "这次要装的应用至少要 %d MB 磁盘，Docker 存镜像的 %s 所在分区只剩 %d MB", needMB, ds.DockerPath, ds.DockerFree>>20)
	}
	if !ds.Same && ds.UploadPath != "" && archive > 0 && ds.UploadFree < uint64(archive) {
		return fault(CodeNoDisk, nil, "要先下载 %d MB 的镜像包到 %s，这个分区只剩 %d MB", (archive+(1<<20)-1)>>20, ds.UploadPath, ds.UploadFree>>20)
	}
	return nil
}

// disconnectedHint turns "not in any catalog" into CAT-INST-25 when the app
// (or, with what set, the recipe: have are then the apps it installed) is
// installed here from a private catalog that was disconnected, so the user
// learns why it is gone instead of "no such app".
func (e *Engine) disconnectedHint(err error, what string, have []state.App) error {
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeUnknown {
		return err
	}
	for _, a := range have {
		if a.CatalogOrigin == "" {
			continue
		}
		if name, disc := e.cfg.Catalog.OriginState(a.CatalogOrigin); disc {
			if what != "" {
				return fault(CodeDisconnected, nil, "%s 来自私有目录 %s（%s），这个目录已断开，不能再装；用它装好的 %s 等照常运行", what, name, a.CatalogOrigin, a.Name)
			}
			return fault(CodeDisconnected, nil, "%s 来自私有目录 %s（%s），这个目录已断开，不能再装或升级；已装的 %s 照常运行", a.CatalogID, name, a.CatalogOrigin, a.Name)
		}
	}
	return err
}

// busy is CAT-INST-04 (with the running task) when app is being installed.
func (e *Engine) busy(app string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if other, ok := e.running[app]; ok {
		f := fault(CodeBusy, nil, "%s 正在安装（任务 %s）", app, other)
		f.TaskID = other
		return f
	}
	return nil
}

// resolveLLM fills the llm_provider parameters: the recipe link, the user's
// choice, or the local gateway when one is installed.
func (e *Engine) resolveLLM(p *planned, vals Values, providers []state.App) error {
	for _, prm := range p.app.Manifest.Params {
		if prm.Type != "llm_provider" {
			continue
		}
		c, _ := vals[prm.Key].(*LLMChoice)
		wantModel := ""
		if p.link != "" && e.usesLinkFor(p.app.Manifest, prm.Key) {
			model := ""
			if c != nil {
				// The recipe wires this parameter to its gateway: a different
				// explicit choice is refused rather than silently dropped.
				if (c.Source != "" && c.Source != LLMLocalGateway) || (c.App != "" && c.App != p.link) || c.BaseURL != "" || c.APIKey != "" {
					return fault(CodeBadParams, nil, "这个配方固定让 %s 用本机的 %s（参数 %s 只能改 model）；要用自己的 Key，请单独装 %s 并加 --llm own", p.app.Manifest.ID, p.link, prm.Key, p.app.Manifest.ID)
				}
				model = c.Model
			}
			c = &LLMChoice{Source: LLMLocalGateway, App: p.link, Model: model}
		}
		if c != nil && c.Source == "" {
			if c.BaseURL != "" || c.APIKey != "" {
				return fault(CodeBadParams, nil, "大模型参数 %s 要写 source（own、local_gateway、platform）", prm.Key)
			}
			wantModel = c.Model
			c = nil
		}
		if c == nil {
			if prm.LLM != nil && prm.LLM.LocalGateway && len(providers) == 1 {
				c = &LLMChoice{Source: LLMLocalGateway, App: providers[0].Name}
			} else if prm.Required {
				return fault(CodeBadParams, nil, "要选大模型（%s）：--llm local 用本机 New API，或 --llm own 填自己的 base_url、api_key、model", prm.Key)
			} else {
				continue
			}
		}
		switch c.Source {
		case LLMPlatform:
			return fault(CodePlatformLLM, nil, "平台 Key 即将开放，现在请用本机 New API 或自己的 Key")
		case LLMLocalGateway:
			if prm.LLM != nil && !prm.LLM.LocalGateway {
				return fault(CodeBadParams, nil, "%s 不支持用本机网关", p.app.Manifest.ID)
			}
			if c.App == "" {
				switch len(providers) {
				case 0:
					return fault(CodeNoGateway, nil, "本机还没有在运行的模型网关（New API）")
				case 1:
					c.App = providers[0].Name
				default:
					names := []string{}
					for _, pa := range providers {
						names = append(names, pa.Name)
					}
					return fault(CodeBadParams, nil, "本机有几个模型网关（%s），用 llm.app 指定一个", strings.Join(names, "、"))
				}
			} else if c.App != p.link {
				found := false
				for _, pa := range providers {
					if pa.Name == c.App {
						found = true
					}
				}
				if !found {
					return fault(CodeNoGateway, nil, "本机没有在运行的模型网关 %s", c.App)
				}
			}
			if c.Model == "" {
				c.Model = wantModel
			}
			if c.Model == "" {
				c.Model = vals.platformMap()[catalog.PVLLMModelChat]
			}
			if c.Model == "" {
				c.Model = DefaultGatewayModel
			}
		}
		vals[prm.Key] = c
	}
	return nil
}

func (e *Engine) usesLinkFor(m *catalog.Manifest, key string) bool {
	for _, u := range m.Uses {
		if u.Kind == "llm_gateway" && u.ForParam == key {
			return true
		}
	}
	return false
}

// StartInstall checks req, records a task and runs it in the background.
func (e *Engine) StartInstall(ctx context.Context, req Request) (TaskView, error) {
	// A bad platform change is the caller's mistake: report it first.
	if err := checkPlatformSets(req.Platform); err != nil {
		return TaskView{}, err
	}
	if e.cfg.Catalog == nil {
		return TaskView{}, fault(CodeInternal, nil, "这台服务器没有启用应用中心")
	}
	if _, ok := e.cfg.Runner.(runner.CatalogAPI); !ok {
		return TaskView{}, fault(CodeInternal, nil, "运行器不支持从目录安装，请升级 AcornFox")
	}
	kind, target, plan, userSteps, err := e.plan(ctx, req)
	if err != nil {
		return TaskView{}, err
	}
	id := newID()
	e.mu.Lock()
	for _, p := range plan {
		if p.reuse {
			continue
		}
		if other, busy := e.running[p.name]; busy {
			e.mu.Unlock()
			f := fault(CodeBusy, nil, "%s 正在安装（任务 %s）", p.name, other)
			f.TaskID = other
			return TaskView{}, f
		}
	}
	for _, p := range plan {
		if !p.reuse {
			e.running[p.name] = id
		}
	}
	e.mine[id] = true
	e.mu.Unlock()

	tr := &taskRun{e: e, id: id, view: TaskView{ID: id, Kind: kind, Target: target, Catalog: PublicSource, Status: state.TaskRunning, UserSteps: userSteps}}
	// A recipe's apps all come from the recipe's catalog.
	if len(plan) > 0 && plan[0].app.Origin != "" {
		tr.view.Catalog = plan[0].app.Origin
	}
	for _, p := range plan {
		tr.view.Apps = append(tr.view.Apps, AppResult{App: p.name, CatalogID: p.app.Manifest.ID, Version: p.app.Index.Version, Reused: p.reuse})
		tr.view.Steps = append(tr.view.Steps, stepsFor(p)...)
	}
	data, _ := json.Marshal(tr.view)
	if _, err := e.cfg.Store.CreateInstallTask(ctx, state.InstallTask{ID: id, Kind: kind, Target: target, Status: state.TaskRunning, Data: string(data)}); err != nil {
		e.release(plan)
		return TaskView{}, fault(CodeInternal, err, "记录安装任务失败")
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer e.release(plan)
		tctx, cancel := context.WithTimeout(e.base, 60*time.Minute)
		defer cancel()
		tr.run(tctx, plan, req.KeepVolumesOnFailure)
	}()
	tv := tr.snapshot()
	return tv, nil
}

func (e *Engine) release(plan []*planned) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range plan {
		if !p.reuse {
			delete(e.running, p.name)
		}
	}
}

// Task returns a task's progress.
func (e *Engine) Task(ctx context.Context, id string) (TaskView, error) {
	if !state.ValidTaskID(id) {
		return TaskView{}, fault(CodeTaskNotFound, nil, "没有安装任务 %s", id)
	}
	t, err := e.cfg.Store.GetInstallTask(ctx, id)
	if errors.Is(err, state.ErrNotFound) {
		return TaskView{}, fault(CodeTaskNotFound, nil, "没有安装任务 %s", id)
	}
	if err != nil {
		return TaskView{}, fault(CodeInternal, err, "读安装任务失败")
	}
	return taskViewOf(t), nil
}

// Tasks lists recent tasks, newest first.
func (e *Engine) Tasks(ctx context.Context, limit int) ([]TaskView, error) {
	ts, err := e.cfg.Store.ListInstallTasks(ctx, "", limit)
	if err != nil {
		return nil, fault(CodeInternal, err, "读安装任务失败")
	}
	out := make([]TaskView, 0, len(ts))
	for _, t := range ts {
		out = append(out, taskViewOf(t))
	}
	return out, nil
}

func taskViewOf(t state.InstallTask) TaskView {
	var v TaskView
	_ = json.Unmarshal([]byte(t.Data), &v)
	v.ID, v.Kind, v.Target, v.Status = t.ID, t.Kind, t.Target, t.Status
	v.CreatedAt, v.UpdatedAt = t.CreatedAt, t.UpdatedAt
	if t.Status != state.TaskRunning {
		u := t.UpdatedAt
		v.FinishedAt = &u
	}
	return v
}

// recover fails tasks that were running when the server stopped and removes
// the apps they had created.
func (e *Engine) recover(ctx context.Context) {
	ts, err := e.cfg.Store.ListInstallTasks(ctx, state.TaskRunning, 500)
	if err != nil {
		e.cfg.Log.Error("list running install tasks", "err", err)
		return
	}
	for _, t := range ts {
		// A task of this process (accepted after the API began serving,
		// while recover was still running) is not a leftover.
		if e.ownsTask(t.ID) {
			continue
		}
		v := taskViewOf(t)
		tr := &taskRun{e: e, id: t.ID, view: v}
		// What the task created is in its record (volumes, image tag, gateway
		// token), so this removes exactly that.
		tr.cleanup(ctx, false)
		tr.finish(ctx, fault(CodeInterrupted, nil, "安装途中服务重启了，已清理这次装了一半的应用"))
	}
}

// ownsTask reports whether task id was started by this process.
func (e *Engine) ownsTask(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mine[id]
}
