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
//     <data>/secrets/catalog.key on first use; an app that provides a
//     service is registered in the service registry; an app whose need is
//     plugged into a service another app provides gets a credential the
//     providing app makes for it (the provider's own steps, connect.go).
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
	"fmt"
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
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Store is what the engine needs from *state.Store.
type Store interface {
	GetApp(ctx context.Context, name string) (state.App, error)
	ListApps(ctx context.Context) ([]state.App, error)
	UpdateApp(ctx context.Context, name string, fn func(*state.App) error) (state.App, error)
	SetEnv(ctx context.Context, v state.EnvVar) error
	DeleteEnv(ctx context.Context, app, key string) error
	ListEnv(ctx context.Context, app string) ([]state.EnvVar, error)
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
	// The manifest each catalog app was installed with: Set replaces (an
	// install), Add only fills an empty slot (the backfill).
	SetCatalogManifest(ctx context.Context, m state.CatalogManifest) error
	AddCatalogManifest(ctx context.Context, m state.CatalogManifest) (bool, error)
	GetCatalogManifest(ctx context.Context, app string) (state.CatalogManifest, error)
	// AddEvent writes a line in an app's history (a new version found, an
	// upgrade done or put back).
	AddEvent(ctx context.Context, e state.Event) error
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
	// Where is where the app can be reached now: its exposure (the
	// server's default filled in), its port and the domains bound to it.
	// The provider works the app's address out of it (app.url, the
	// address in install results and first-login details, apps vars).
	// nil: the app's own record (its exposure, its port), no domains.
	Where func(ctx context.Context, a state.App) platformvar.Where
}

// Config configures New.
type Config struct {
	Store     Store
	Runner    Runner
	Catalog   *Catalog
	DataDir   string  // <data>/secrets/catalog.key
	UploadDir string  // downloaded image archives (the runner reads them)
	Gateway   Gateway // nil: large images come from public sources only
	// MirrorProbe measures how fast the image sources (mirrors) bring ref
	// (repository@digest) for arch; with it, an image that names an archive
	// goes to whichever is faster, the download gateway or the sources, and
	// the other is the back-up (pickpath.go). nil: the gateway first, the
	// sources only when it fails.
	MirrorProbe func(ctx context.Context, ref, arch string) MirrorSpeed
	Version     string // this AcornFox's version, for min_platform
	// PlatformVars is where the platform variables come from; nil is the
	// standalone implementation filled from Platform (this machine's
	// settings).
	PlatformVars platformvar.Provider
	Platform     PlatformEnv
	// DiskFree reports the free space an install can use; nil (or an error)
	// skips the disk check.
	DiskFree func(ctx context.Context) (DiskSpace, error)
	Arch     string // Docker host CPU (amd64|arm64); "" => runtime.GOARCH
	HTTP     *http.Client
	Log      *slog.Logger

	// DefaultExposure is what this machine gives a new app when neither the
	// request (--expose) nor its manifest chooses something other than the
	// catalog's standard (local): lan on a LAN server installed fresh, local
	// otherwise. "" keeps catalog.DefaultExposure, so machines installed
	// before and desktops go on as they did. See expose.go.
	DefaultExposure string

	DeployTimeout time.Duration // wait for the deployment to go live (0: 15 min)
	Poll          time.Duration // progress polling (0: 1 s)
	// TCPProbe connects once to addr for a tcp health check (nil:
	// runner.ProbeTCP); tests replace it.
	TCPProbe func(ctx context.Context, addr string) error
}

// Engine runs install tasks.
type Engine struct {
	cfg Config
	box *secretBox

	mu    sync.Mutex
	hooks Hooks
	snaps Snapshots // the app backups (the upgrade's snapshot); nil: none
	// notifyMu guards the file of the versions already told (upgrade.go).
	notifyMu sync.Mutex
	running  map[string]string // app name -> task id
	mine     map[string]bool   // ids of tasks this process started (recover skips them)
	base     context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
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
		cfg: cfg, box: newSecretBox(cfg.DataDir),
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

// Start fails and cleans up tasks a previous run left running, starts keeping
// the catalog synced until ctx ends, and then gives apps installed before
// manifests were saved theirs from what is on this machine (see
// BackfillManifests; a failure there is only logged). The server calls it in
// its own goroutine, so none of this delays serving.
func (e *Engine) Start(ctx context.Context) {
	e.recover(ctx)
	if e.cfg.Catalog != nil {
		// A new version of an installed app is told once after each sync.
		e.cfg.Catalog.SetAfterSync(e.noteOutdated)
		go e.cfg.Catalog.Run(ctx)
		e.BackfillManifests(ctx)
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
	Platform map[string]string `json:"platform,omitempty"`
	// Use names the service a need of an app is connected to: {"<need>":
	// "<service>"} for the app of a one-app request, {"<app>.<need>": …}
	// always (CLI --use). See needs.go.
	Use                  map[string]string `json:"use,omitempty"`
	Name                 string            `json:"name,omitempty"`
	Version              string            `json:"version,omitempty"`
	KeepVolumesOnFailure bool              `json:"keep_volumes_on_failure,omitempty"`
	// ReuseData installs on top of the data volumes an earlier app of the
	// same name left behind (deleted with its data kept) instead of refusing
	// with CAT-INST-24. Those volumes are never removed by a failed install;
	// init steps are skipped (the old data has been initialized) and the
	// first-login values init would have written say they are the old ones.
	// What the old app was set up with comes back unless this request says
	// otherwise (see reuse.go): its parameters, the platform variables the
	// user had changed, the services its needs were plugged into (with the
	// credentials, when they still pass the provider's check), and its
	// config files in the data. With ReuseData and Name, App may be left
	// out: it is then the catalog app that app was (and Catalog, when not
	// given, the catalog it came from), read from what was kept when it was
	// deleted with its data (reuseTarget).
	ReuseData bool `json:"reuse_data,omitempty"`
	// Expose is the exposure the apps get instead of their manifest's
	// default: local, lan or public ("" the manifest's). See expose.go.
	Expose string `json:"expose,omitempty"`
}

// planned is one app of a task.
type planned struct {
	app  *App
	name string
	// expose is the exposure the request gives it ("" the manifest's).
	expose string
	reuse  bool
	source string // catalog | recipe:<id>
	user   map[string]any
	vals   Values
	// reuseData: the request allows taking over old data volumes;
	// reusedVolumes are the ones actually taken over (set by create).
	reuseData     bool
	reusedVolumes []string
	// restored: the parameters kept when the old app was deleted with its
	// data were put back (same passwords and keys as the data); initStale:
	// no kept record and init said the data was initialized already.
	restored  bool
	initStale bool
	// What a reuse_data install carries over (reuse.go): kept is the old
	// app's saved parameters (nil without a kept record), keptManifest the
	// manifest it was installed with (nil when not saved). carried: one line
	// per thing carried over, shown in the create step. rewritten: config
	// files (by path) an earlier install on the same old data rewrote before
	// it failed (see rewrittenSince): what is in the data is not the old
	// app's any more.
	kept         Values
	keptManifest *catalog.Manifest
	carried      []string
	rewritten    map[string]bool
	// preset: parameter keys whose value came from the recipe, not the user.
	// origins: where each parameter's value came from (Origin*), saved with
	// the parameters so it can be shown later.
	preset  map[string]bool
	origins map[string]string
	// Services (needs written the new way, see needs.go): what each need was
	// matched with; wired, the recipe's links (by need name) of a need to
	// the app of the same recipe whose service it is plugged into;
	// needsMatched, its needs were matched (a plan skips that for an app
	// whose manifest it refused already).
	needs        []*needMatch
	wired        map[string]*planned
	needsMatched bool
	// reusedUses: the needs whose binding (and credential) from before this
	// install was used again as it was. oldUses: for a need plugged into the
	// same service as before with a new credential, the items the old app
	// was given (its old credential). The items of any other need may differ
	// from what the old app's config files hold in ways nothing can tell
	// (fileChange).
	reusedUses map[string]bool
	oldUses    map[string]map[string]string
}

func (p *planned) setOrigin(key, origin string) {
	if p.origins == nil {
		p.origins = map[string]string{}
	}
	p.origins[key] = origin
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

// needsPlatform returns the highest of the given min_platform values when
// this AcornFox is older than it (without a leading "v"), else "". Values it
// cannot read are skipped, as versionAtLeast lets them pass.
func (e *Engine) needsPlatform(mins ...string) string {
	need := ""
	for _, m := range mins {
		if !versionRe.MatchString(m) {
			continue
		}
		if need == "" || catalog.CompareVersions(m, need) > 0 {
			need = m
		}
	}
	if need == "" || versionAtLeast(e.cfg.Version, need) {
		return ""
	}
	return strings.TrimPrefix(need, "v")
}

// tooOld is CAT-INST-08 for what (an app id or "配方 <id>") that needs
// platform version need.
func (e *Engine) tooOld(what, need string) *Fault {
	f := fault(CodeTooOld, nil, "%s 要求 AcornFox %s 以上，这台是 %s，请先升级 AcornFox 再装", what, need, e.cfg.Version)
	f.NeedsPlatform = need
	return f
}

// appToInstall returns app id of catalog from, checked as far as it can be
// before anything is created: the index's min_platform first (CAT-INST-08,
// before the app package is even downloaded; an entry published in a former
// manifest format is CAT-INST-36 there too), then, with the package
// downloaded and verified, the manifest's format version (CAT-INST-36 for a
// former one), its raw bytes against the built-in schema (CAT-INST-29), and
// only then is the manifest decoded. In that order a manifest this program
// cannot decode at all (a former format, a known field that changed type in
// a later format) is reported as what it is, not as a broken package. A
// private catalog's app goes through the same checks.
func (e *Engine) appToInstall(ctx context.Context, from, id string) (*App, error) {
	v, ia, err := e.cfg.Catalog.find(from, id)
	if err != nil {
		return nil, err
	}
	if need := e.needsPlatform(ia.MinPlatform); need != "" {
		return nil, e.tooOld(ia.ID, need)
	}
	if catalog.FormerIndexApp(ia) {
		return nil, formerApp(ia, 0, false)
	}
	files, err := e.cfg.Catalog.loadPackage(ctx, v, ia)
	if err != nil {
		return nil, err
	}
	if n, former := catalog.FormerManifest(files[manifestFile]); former {
		return nil, formerApp(ia, n, true)
	}
	if err := e.checkPackage(ia, files[manifestFile]); err != nil {
		return nil, err
	}
	return parseApp(v, ia, files)
}

// formerApp is CAT-INST-36 for an app published in a manifest format before
// the current one (n, when known from the manifest itself): this platform
// installs none of it and converts none of it. The app has to be published
// again in the current format; nothing the user can do here changes that.
// fetched: the package was downloaded before this was found out (the
// catalog's index did not say so).
func formerApp(ia catalog.IndexApp, n int, fetched bool) error {
	which := "旧格式的清单"
	if n > 0 {
		which = fmt.Sprintf("旧格式（第 %d 版）的清单", n)
	}
	done := "什么都没建、没下载"
	if fetched {
		done = "应用包已经下载到本机缓存，但什么都没建"
	}
	return fault(CodeLegacyManifest, nil, "%s %s 是%s，这一版平台装不了：要等这个应用按新格式重新上架。不是你的操作不对；%s", ia.ID, ia.Version, which, done)
}

// checkPackage refuses an app package whose manifest uses a field, a value
// or a format version this program does not know (CAT-INST-29): installing
// it anyway would silently leave out what the unknown part asks for. The
// check is the schema the catalog tools use, on the manifest's own bytes, so
// fields the schema has but the engine does not read still pass. When the
// manifest asks for a newer platform the cure is an upgrade (NeedsPlatform is
// set); otherwise the catalog itself is wrong and the user can only wait.
// The two cases carry different hints, so that nobody upgrades for nothing
// or waits for a catalog fix that an upgrade would have made unnecessary.
//
// It works on the raw bytes alone, before the manifest is decoded: the
// platform version the manifest asks for is read leniently from them.
func (e *Engine) checkPackage(ia catalog.IndexApp, manifest []byte) error {
	ps := catalog.ValidateManifest(manifest)
	if len(ps) == 0 {
		return nil
	}
	var what []string
	for i, p := range ps {
		if i == 3 {
			what = append(what, fmt.Sprintf("等 %d 处", len(ps)))
			break
		}
		at := p.Path
		if at == "" {
			at = "/"
		}
		what = append(what, at+" "+p.Message)
	}
	detail := strings.Join(what, "；")
	id, ver := ia.ID, ia.Version
	if need := e.needsPlatform(ia.MinPlatform, catalog.DeclaredMinPlatform(manifest)); need != "" {
		f := fault(CodeBadManifest, nil, "%s %s 的清单用了这个版本的 AcornFox 不认识的写法，它要求 AcornFox %s 以上，这台是 %s，请先升级 AcornFox 再装（%s）", id, ver, need, e.cfg.Version, detail)
		f.NeedsPlatform = need
		f.Hint = badManifestUpgradeHint(need)
		return f
	}
	f := fault(CodeBadManifest, nil, "%s %s 的清单过不了检查，没有安装。这是这份目录有问题，不是你的操作不对：稍后再试，还不行请把这段话反馈给我们（%s）", id, ver, detail)
	f.Hint = hintBadManifestCatalog
	return f
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
	// "Upgrade first" comes before "this version cannot do X": a manifest for
	// a newer platform fails the checks below for that very reason.
	if need := e.needsPlatform(m.Lifecycle.MinPlatform); need != "" {
		return e.tooOld(m.ID, need)
	}
	if len(m.Release.Images) != 1 {
		return fault(CodeUnsupported, nil, "%s 有 %d 个镜像，这个版本只能装单容器应用", m.ID, len(m.Release.Images))
	}
	if primaryEndpoint(m) == nil {
		return fault(CodeUnsupported, nil, "%s 的清单没有入口端口", m.ID)
	}
	needs := map[string]bool{}
	for _, u := range m.Uses {
		needs[u.Name] = true
	}
	for _, ev := range m.Env {
		refs := catalog.TemplateRefs(ev.Value)
		if ev.From != "" {
			refs = []string{ev.From}
		}
		if c, err := catalog.ParseCondition(ev.If); ev.If != "" && err == nil {
			refs = append(refs, c.Ref)
		}
		for _, ref := range refs {
			if strings.HasPrefix(ref, "params.") || strings.HasPrefix(ref, "platform.") {
				continue
			}
			if rest, ok := strings.CutPrefix(ref, "uses."); ok {
				if name, _, _ := strings.Cut(rest, "."); needs[name] {
					continue
				}
			}
			return fault(CodeUnsupported, nil, "%s 的环境变量 %s 引用了 %s，这个版本不支持", m.ID, ev.Key, ref)
		}
	}
	// A value written so that it cannot be read (lint refuses it,
	// CAT-LINT-46…48) stops the install before anything is created.
	if err := unreadableValue(m); err != nil {
		return err
	}
	// What the format accepts but this version does not carry out yet
	// (catalog.PendingFeatures): refused before anything is created, never
	// run as if it were not written.
	if p := catalog.PendingFeatures(m); len(p) > 0 {
		return fault(CodeUnsupported, nil, "%s 的清单用了%s（%s），这一版 AcornFox 还不执行，这个应用现在装不了；等支持它的 AcornFox 版本发布后升级再装", m.ID, p[0].What, p[0].Where)
	}
	// Command steps (its init, or what it provides) and a health check run in
	// the container need a runner that can run commands in containers:
	// refused before anything is created rather than when the first one
	// runs. (A runner reached over its socket may still turn out to be too
	// old then: ActionRunnerOld.)
	if _, ok := e.cfg.Runner.(runner.ExecAPI); !ok && (manifestRunsCommands(m) || usesExecHealth(m)) {
		return fault(CodeInternal, nil, "%s 的清单要在它的容器里执行命令（命令步骤或健康检查），这台机器的运行器（runner）太旧，不能执行：升级 AcornFox 后重试", m.ID)
	}
	// Platform variables this binary does not have (a newer catalog may use
	// variables added later) are refused here, before anything is created or
	// downloaded, rather than when the env or a file is rendered.
	for _, name := range catalog.PlatformRefs(m) {
		if _, ok := catalog.LookupPlatformVar(name); !ok {
			return fault(CodeUnsupported, nil, "%s 用到平台变量 %s，这个版本的 AcornFox 还没有，请先升级 AcornFox", m.ID, name)
		}
		if !e.platformAvailable(name) {
			return fault(CodeUnsupported, nil, "%s 用到平台变量 %s，它在这台机器上没有值，这个版本装不了", m.ID, name)
		}
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
// can be refused before a task starts is refused here (the first problem).
func (e *Engine) plan(ctx context.Context, req Request) (kind, target string, plan []*planned, userSteps []string, err error) {
	a, err := e.assess(ctx, req, false)
	if err != nil {
		return "", "", nil, nil, err
	}
	return a.kind, a.target, a.plan, a.userSteps, nil
}

// assess checks a request and works out what it installs, in order. With
// collect false (an install) it stops at the first problem, which it
// returns; with collect true (a plan, see Plan) it goes on where it can and
// records every problem in the assessment. A problem that leaves nothing to
// look at (no such app or recipe, a malformed request) is returned in both
// modes.
func (e *Engine) assess(ctx context.Context, req Request, collect bool) (*assessment, error) {
	if req.App == "" && req.Recipe == "" && req.ReuseData && req.Name != "" {
		if err := e.reuseTarget(ctx, &req); err != nil {
			return nil, err
		}
	}
	a := &assessment{req: req, collect: collect, missing: map[*planned][]ParamMissing{}, usedPicks: map[string]bool{}, kinds: e.planKinds()}
	if (req.App == "") == (req.Recipe == "") {
		return nil, fault(CodeBadParams, nil, "要给 app 或 recipe 其中一个")
	}

	installed, err := e.installedCatalogApps(ctx)
	if err != nil {
		return nil, fault(CodeInternal, err, "读应用列表失败")
	}
	if req.App != "" {
		app, err := e.appToInstall(ctx, req.Catalog, req.App)
		if err != nil {
			return nil, e.disconnectedHint(err, "", installed[req.App])
		}
		if req.Version != "" && req.Version != app.Index.Version {
			return nil, fault(CodeUnknown, nil, "目录里 %s 只有版本 %s", req.App, app.Index.Version)
		}
		name := req.Name
		if name == "" {
			name = req.App
		}
		if !state.ValidAppName(name) {
			return nil, fault(CodeBadParams, nil, "应用名 %s 不合法：小写字母开头，只用小写字母、数字和 -，2～40 个字符", name)
		}
		a.plan = []*planned{{app: app, name: name, source: "catalog"}}
		a.kind, a.target, a.userSteps, a.title = "app", req.App, app.Manifest.Delivery.UserSteps, app.Manifest.App.Name
	} else {
		if req.Name != "" || req.Version != "" {
			return nil, fault(CodeBadParams, nil, "配方不能指定 name 或 version")
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
			return nil, e.disconnectedHint(err, "配方 "+req.Recipe, fromRecipe)
		}
		// The recipe's own minimum (it already counts its apps') before any
		// of its apps is looked at.
		if need := e.needsPlatform(r.MinPlatform); need != "" {
			return nil, e.tooOld("配方 "+r.ID, need)
		}
		from := origin
		if from == "" {
			from = PublicSource
		}
		byID := map[string]*planned{}
		for _, ra := range r.Apps {
			reuse := ra.ReuseInstalled == nil || *ra.ReuseInstalled
			have := installed[ra.App]
			// An app that is already installed is used as it is; every app
			// this task will install is checked here, before the task starts.
			load := e.appToInstall
			if reuse && len(have) > 0 {
				load = e.cfg.Catalog.AppIn
			}
			app, err := load(ctx, from, ra.App)
			if err != nil {
				return nil, err
			}
			p := &planned{app: app, name: ra.App, source: "recipe:" + r.ID}
			if reuse && len(have) > 0 {
				p.name, p.reuse = have[0].Name, true
				// Still being installed by another task: not usable yet.
				if err := e.busy(p.name); err != nil {
					return nil, err
				}
			}
			a.plan = append(a.plan, p)
			byID[ra.App] = p
		}
		order := map[string]int{}
		for i, ra := range r.Apps {
			if _, seen := order[ra.App]; !seen {
				order[ra.App] = i
			}
		}
		for _, l := range r.Links {
			from, to := byID[l.From], byID[l.To]
			if from == nil || to == nil {
				return nil, fault(CodeUnsupported, nil, "配方 %s 的连接 %s→%s 这个版本不支持", r.ID, l.From, l.To)
			}
			// The apps are installed in the recipe's order and a later one
			// is connected to what an earlier one registers (catalog lint
			// refuses the other way round, CAT-LINT-09).
			if order[l.From] >= order[l.To] {
				return nil, fault(CodeUnsupported, nil, "配方 %s 的连接 %s→%s：提供服务的 %s 排在 %s 后面，按顺序装时还接不上它", r.ID, l.From, l.To, l.From, l.To)
			}
			// "uses" names a need of the "to" app: it is plugged into the
			// service of its kind the "from" app provides (matchWired).
			var use *catalog.Use
			for i, u := range to.app.Manifest.Uses {
				if u.Name == l.Uses {
					use = &to.app.Manifest.Uses[i]
				}
			}
			if use == nil {
				return nil, fault(CodeUnsupported, nil, "配方 %s 的连接 %s→%s 写的需要 %q，%s 的清单里没有这个需要", r.ID, l.From, l.To, l.Uses, l.To)
			}
			if to.wired == nil {
				to.wired = map[string]*planned{}
			}
			to.wired[use.Name] = from
		}
		a.kind, a.target, a.userSteps, a.title = "recipe", req.Recipe, r.Delivery.UserSteps, r.Title
		if err := splitParams(a.plan, req.Params); err != nil {
			return nil, err
		}
		for i, ra := range r.Apps {
			for k, v := range ra.Preset {
				if _, ok := a.plan[i].user[k]; !ok {
					a.plan[i].user[k] = v
					if a.plan[i].preset == nil {
						a.plan[i].preset = map[string]bool{}
					}
					a.plan[i].preset[k] = true
				}
			}
		}
	}
	if req.App != "" {
		a.plan[0].user = req.Params
		if a.plan[0].user == nil {
			a.plan[0].user = map[string]any{}
		}
	}
	if err := checkExpose(req.Expose, a.plan, req.Recipe != ""); err != nil {
		return nil, err
	}

	needMB, archive := 0, int64(0)
	for _, p := range a.plan {
		if p.reuse {
			continue
		}
		if err := e.checkManifest(p.app); err != nil {
			if a.fail(err) {
				return nil, err
			}
			continue
		}
		// A running install of this name has already created its app
		// record: report it as busy (with its task), not as a name clash
		// whose advice (delete the old one) would break the running task.
		if err := e.busy(p.name); err != nil {
			if a.fail(err) {
				return nil, err
			}
		} else if _, err := e.cfg.Store.GetApp(ctx, p.name); err == nil {
			if f := fault(CodeNameTaken, nil, "已经有叫 %s 的应用", p.name); a.fail(f) {
				return nil, f
			}
		} else if !errors.Is(err, state.ErrNotFound) {
			if f := fault(CodeInternal, err, "读应用失败"); a.fail(f) {
				return nil, f
			}
		}
		p.reuseData = req.ReuseData
		if len(p.app.Manifest.Volumes) > 0 && !p.reuseData {
			if err := e.checkOldVolumes(ctx, p.name); err != nil && a.fail(err) {
				return nil, err
			}
		}
		// On old data a required parameter not given now may come back from
		// what the old app was set up with (restoreKept): it is missing only
		// when it does not.
		vals, missing, err := resolveParamsOpt(p.app.Manifest, p.user, collect || p.reuseData)
		if err != nil && a.fail(err) {
			return nil, err
		}
		// A plan goes on after a key that is no parameter of the app (vals
		// is then still worked out without it): what is missing is listed
		// next to it.
		if vals != nil {
			// The platform variables before the app exists (app.url comes
			// once it is created: taskRun.create asks the provider again).
			pvals, err := e.platformValues(ctx, platformvar.App{Name: p.name, Sets: req.Platform})
			if err != nil && a.fail(err) {
				return nil, err
			}
			if pvals == nil {
				pvals = map[string]string{}
			}
			vals[platformKey] = pvals
			// Only what the user changed in this request, next to the merged
			// values above: later the two tell "the user chose this" from "the
			// platform worked this out at install time".
			vals[platformUserKey] = platformUserSets(req.Platform)
			p.origins = paramOrigins(p.app.Manifest, p.user, p.preset)
			vals[originsKey] = p.origins
			if p.reuseData {
				if err := e.restoreKept(ctx, p, vals, req.Platform); err != nil && a.fail(err) {
					return nil, err
				}
			}
			if missing = stillMissing(missing, vals); len(missing) > 0 {
				if !collect {
					return nil, fault(CodeBadParams, nil, "缺少必填参数 %s（%s）", missing[0].Key, missing[0].Label)
				}
				a.missing[p] = missing
			}
			p.vals = vals
		}
		if err := e.matchNeeds(ctx, a, p); err != nil {
			return nil, err
		}
		needMB += p.app.Manifest.Resources.DiskMB.Min
		if e.cfg.Gateway != nil {
			for _, img := range p.app.Manifest.Release.Images {
				// Only this machine's architecture's archive is downloaded.
				if art, ok := img.ArtifactFor(e.cfg.Arch); ok {
					archive += art.Size
				}
			}
		}
	}
	if collect {
		e.planChecks(ctx, a)
	}
	picks := make([]string, 0, len(req.Use))
	for k := range req.Use {
		picks = append(picks, k)
	}
	sort.Strings(picks)
	for _, k := range picks {
		if a.usedPicks[k] {
			continue
		}
		if f := a.badPick(k); f != nil && a.fail(f) {
			return nil, f
		}
	}
	if err := e.checkDisk(ctx, needMB, archive); err != nil && a.fail(err) {
		return nil, err
	}
	return a, nil
}

// stillMissing is the required parameters of missing that vals has no
// value for.
func stillMissing(missing []ParamMissing, vals Values) []ParamMissing {
	var out []ParamMissing
	for _, m := range missing {
		if s, _ := vals[m.Key].(string); s == "" {
			out = append(out, m)
		}
	}
	return out
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

// StartInstall checks req, records a task and runs it in the background.
func (e *Engine) StartInstall(ctx context.Context, req Request) (TaskView, error) {
	// A bad platform change is the caller's mistake: report it first.
	if err := e.checkPlatformSets(req.Platform); err != nil {
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
		tr.view.Apps = append(tr.view.Apps, AppResult{App: p.name, CatalogID: p.app.Manifest.ID, Version: p.app.Index.Version, Reused: p.reuse, Tell: p.tell()})
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
		// The task's 60 minutes do not count the pulls from the public
		// sources, which have their own limit (clock.go).
		tctx, cancel := taskContext(e.base, installTaskLimit)
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
	return withTiming(e.currentLogins(ctx, taskViewOf(t)), time.Now()), nil
}

// currentLogins puts the app's address of now in each first_login of a
// succeeded task (the record keeps the one of the moment it was installed;
// the app may have been opened to the local network since) and works out
// url_note again, the way FirstLogin does. An app that is gone keeps the
// address of the record.
func (e *Engine) currentLogins(ctx context.Context, v TaskView) TaskView {
	if v.Status != state.TaskSucceeded || len(v.FirstLogin) == 0 {
		return v
	}
	logins := make([]FirstLogin, len(v.FirstLogin))
	copy(logins, v.FirstLogin)
	for i := range logins {
		if a, err := e.cfg.Store.GetApp(ctx, logins[i].App); err == nil {
			logins[i].URL = e.address(ctx, a)
		}
		logins[i].URLNote = ""
		if loopbackURL(logins[i].URL) {
			logins[i].URLNote = LoopbackURLNote
		}
	}
	v.FirstLogin = logins
	return v
}

// Tasks lists recent tasks, newest first.
func (e *Engine) Tasks(ctx context.Context, limit int) ([]TaskView, error) {
	ts, err := e.cfg.Store.ListInstallTasks(ctx, "", limit)
	if err != nil {
		return nil, fault(CodeInternal, err, "读安装任务失败")
	}
	out := make([]TaskView, 0, len(ts))
	now := time.Now()
	for _, t := range ts {
		out = append(out, withTiming(e.currentLogins(ctx, taskViewOf(t)), now))
	}
	return out, nil
}

func taskViewOf(t state.InstallTask) TaskView {
	var v TaskView
	_ = json.Unmarshal([]byte(t.Data), &v)
	kind := t.Kind
	if isOpKind(v.Kind) {
		kind = v.Kind // the table's kind is "app": see StartReapply
	}
	v.ID, v.Kind, v.Target, v.Status = t.ID, kind, t.Target, t.Status
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
		if v.Kind == KindRollback {
			e.recoverRollback(ctx, t, v)
			continue
		}
		if v.Kind == KindReapply || v.Kind == KindUpgrade {
			// Not an install: nothing was created; what the apply changed
			// is put back from its journal.
			e.recoverReapply(ctx, t, v)
			continue
		}
		tr := &taskRun{e: e, id: t.ID, view: v}
		// What the task created is in its record (volumes, image tag,
		// services registered, credentials issued), so this removes
		// exactly that.
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

// unreadableValue is CAT-INST-39 for the first template or condition of m's
// env entries, start command, config files and first-login values that
// cannot be read (catalog.ParseTemplate, ParseCondition).
func unreadableValue(m *catalog.Manifest) error {
	check := func(where, text string, cond bool) error {
		if text == "" {
			return nil
		}
		var err error
		if cond {
			_, err = catalog.ParseCondition(text)
		} else {
			_, err = catalog.ParseTemplate(text)
		}
		if err != nil {
			return fault(CodeRender, err, "%s 的清单里 %s 写得读不了：%v", m.ID, where, err)
		}
		return nil
	}
	for _, ev := range m.Env {
		if err := check("环境变量 "+ev.Key+" 的 if", ev.If, true); err != nil {
			return err
		}
		if err := check("环境变量 "+ev.Key, ev.Value, false); err != nil {
			return err
		}
	}
	for i, a := range m.Container.Command {
		if err := check(fmt.Sprintf("启动命令的第 %d 项", i+1), a, false); err != nil {
			return err
		}
	}
	for _, f := range m.Files {
		if err := check("配置文件 "+f.Path+" 的 if", f.If, true); err != nil {
			return err
		}
		if err := check("配置文件 "+f.Path, f.Template, false); err != nil {
			return err
		}
	}
	for _, lf := range m.FirstLogin {
		if lf.From == "" {
			if err := check("首次登录信息 "+lf.Label, lf.Value, false); err != nil {
				return err
			}
		}
	}
	return nil
}
