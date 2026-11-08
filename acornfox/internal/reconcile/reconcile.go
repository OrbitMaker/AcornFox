package reconcile

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/caddyroute"
	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Store is the subset of *state.Store the reconciler depends on. It is
// declared here (rather than importing a concrete type) so the store can be
// implemented concurrently and faked in tests. Signatures match the method
// set documented in internal/state/types.go.
type Store interface {
	// Apps
	GetApp(ctx context.Context, name string) (state.App, error)
	ListApps(ctx context.Context) ([]state.App, error)
	SetCurrentDeployment(ctx context.Context, app, deploymentID string) error

	// Deployments
	GetDeployment(ctx context.Context, id string) (state.Deployment, error)
	PendingDeployments(ctx context.Context, app string) ([]state.Deployment, error)
	UpdateDeployment(ctx context.Context, id string, fn func(*state.Deployment) error) (state.Deployment, error)
	KeptImageDeployments(ctx context.Context, app string) ([]state.Deployment, error)
	// KeptImageIDs is the image set image GC must keep (kept versions plus
	// what pending deployments use or will resolve).
	KeptImageIDs(ctx context.Context, app string) (map[string]struct{}, error)
	// ListDeployments returns deployments newest first; limit <= 0 is all.
	ListDeployments(ctx context.Context, app string, limit int) ([]state.Deployment, error)

	// Env, volumes
	AddVolume(ctx context.Context, app, path string, auto bool) (state.Volume, bool, error)
	ListEnv(ctx context.Context, app string) ([]state.EnvVar, error)
	ListVolumes(ctx context.Context, app string) ([]state.Volume, error)

	// Addons (N4)
	ListAddons(ctx context.Context, app string) ([]state.Addon, error)
	// ListRemovedAddons returns soft-deleted add-ons; their credentials are
	// still redacted from logs.
	ListRemovedAddons(ctx context.Context, app string) ([]state.Addon, error)

	// Events
	AddEvent(ctx context.Context, e state.Event) error

	// Domains (N3)
	ListDomains(ctx context.Context, app string) ([]state.Domain, error)
	SetDomainStatus(ctx context.Context, app, name, status string, diag *state.Diagnosis) error
}

// Config configures a Reconciler. Store, Runner and Router are required.
type Config struct {
	Store         Store
	Runner        runner.API
	Router        caddyroute.Router
	Probe         func(ctx context.Context, hostport, path string) error
	UploadDir     string
	PublicHost    string
	Logger        *slog.Logger
	Tick          time.Duration
	HealthTimeout time.Duration
	Now           func() time.Time
	// Git performs server-side clones for git-source deployments. Nil uses the
	// system git binary (DefaultGit); tests inject a fake.
	Git Git

	// HTTPS configures the shared "af-domains" server maintained by the router.
	HTTPS caddyroute.HTTPSConfig
	// HTTPSCAFile is the PEM root(s) used to verify domain certificates during
	// probing. Empty means use the system roots. For the internal Caddy CA
	// (development), this points at pki/authorities/local/root.crt.
	HTTPSCAFile string
	// DomainProbe performs one TLS handshake against httpsAddr with SNI=name,
	// verifying the presented chain against roots (system roots when nil) and
	// that the certificate covers name. Nil uses DefaultDomainProbe.
	DomainProbe func(ctx context.Context, httpsAddr, name string, roots *x509.CertPool) error
	// DomainPendingBudget overrides state.DomainPendingBudget (tests set it
	// small). Zero uses the default.
	DomainPendingBudget time.Duration

	// Edge runs Caddy as a container managed through the runner (see
	// edge.go); nil keeps the v0.3 host Caddy.
	Edge *EdgeConfig
	// DefaultExposure applies to apps whose exposure is "" ("public" when empty).
	DefaultExposure string

	// DeployLogs keeps each deployment's build log and, before its container
	// is removed, its run log (see deploylogs.go). Nil keeps no logs and
	// changes nothing else.
	DeployLogs *deploylog.Store
}

// Default configuration values.
const (
	DefaultTick          = 30 * time.Second
	DefaultHealthTimeout = 60 * time.Second
	routeFailureBudget   = 5 * time.Minute
	staleUploadAge       = 1 * time.Hour
	pingThrottle         = 1 * time.Minute
	redactMinLen         = 4
	redactMask           = "******"
)

// Reconciler drives every app toward its desired state.
type Reconciler struct {
	cfg Config
	log *slog.Logger

	// per-app scheduling
	mu       sync.Mutex
	workers  map[string]*worker
	firstRun bool // true until the first global round completes (forces a route Sync)

	// edge container: the spec key last applied (edge.go); edgeOpMu
	// serializes EnsureEdge calls
	edgeMu       sync.Mutex
	edgeKey      string
	edgeState    string
	edgeMsg      string
	edgeFailures int
	edgeOpMu     sync.Mutex
	// lanUnbindable is a LAN address Docker could not bind (Docker runs in a
	// VM that does not have it, e.g. WSL 2 in NAT mode); the edge then
	// publishes on 127.0.0.1 only and apps get no LAN address.
	lanUnbindable string

	// runner/unavailable event throttle: app -> last time an event was written
	pingMu      sync.Mutex
	lastPingEvt map[string]time.Time

	// global build semaphore (capacity 1)
	buildSem chan struct{}

	// routing failure tracking: deployment ID -> first failure time
	routeFailMu    sync.Mutex
	routeFailSince map[string]time.Time

	// domain probing CA roots cache (from Config.HTTPSCAFile)
	caOnce sync.Once
	caPool *x509.CertPool
	caErr  error

	// N4.2: poll interval of the add-on readiness gate (tests shorten it)
	addonPoll time.Duration

	// noLogTailOnce limits the "runner cannot return log tails" warning to one.
	noLogTailOnce sync.Once
}

// worker owns one app's serial reconcile goroutine.
type worker struct {
	app   string
	kick  chan struct{} // buffered size 1; coalesces triggers
	token chan struct{} // held by a reconcile round or an explicit API operation
}

// New builds a Reconciler, filling defaults.
func New(cfg Config) *Reconciler {
	if cfg.Tick <= 0 {
		cfg.Tick = DefaultTick
	}
	if cfg.HealthTimeout <= 0 {
		cfg.HealthTimeout = DefaultHealthTimeout
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Probe == nil {
		cfg.Probe = DefaultProbe
	}
	if cfg.DomainProbe == nil {
		cfg.DomainProbe = DefaultDomainProbe
	}
	if cfg.DomainPendingBudget <= 0 {
		cfg.DomainPendingBudget = state.DomainPendingBudget
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Reconciler{
		cfg:            cfg,
		log:            cfg.Logger,
		workers:        make(map[string]*worker),
		firstRun:       true,
		lastPingEvt:    make(map[string]time.Time),
		buildSem:       make(chan struct{}, 1),
		routeFailSince: make(map[string]time.Time),
		addonPoll:      time.Second,
	}
}

// Run blocks until ctx is cancelled. On start and every Tick it kicks all
// known apps and runs a global orphan sweep.
func (r *Reconciler) Run(ctx context.Context) error {
	// initial round
	r.globalRound(ctx)

	t := time.NewTicker(r.cfg.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.shutdown()
			return ctx.Err()
		case <-t.C:
			r.globalRound(ctx)
		}
	}
}

// globalRound kicks every known app and performs the global GC sweep.
func (r *Reconciler) globalRound(ctx context.Context) {
	apps, err := r.cfg.Store.ListApps(ctx)
	if err != nil {
		r.log.Error("list apps", "err", err)
		return
	}
	names := make([]string, 0, len(apps))
	for _, a := range apps {
		names = append(names, a.Name)
		r.Kick(a.Name)
	}
	r.globalGC(ctx, names)

	r.mu.Lock()
	r.firstRun = false
	r.mu.Unlock()
}

// shutdown closes all worker kick channels.
func (r *Reconciler) shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range r.workers {
		close(w.kick)
	}
	r.workers = make(map[string]*worker)
}

// Kick triggers one reconcile round for app; duplicate kicks are coalesced.
// It lazily creates the app's serial goroutine on first use.
func (r *Reconciler) Kick(app string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.workerLocked(app)
	select {
	case w.kick <- struct{}{}:
	default: // already pending; coalesce
	}
}

// workerLocked returns the app's worker. The caller holds r.mu.
func (r *Reconciler) workerLocked(app string) *worker {
	w, ok := r.workers[app]
	if !ok {
		w = &worker{app: app, kick: make(chan struct{}, 1), token: make(chan struct{}, 1)}
		r.workers[app] = w
		go r.workerLoop(w)
	}
	return w
}

// WithApp serializes an explicit operation with this app's reconcile rounds.
// Waiting is cancellable; different apps do not block each other. fn may Kick
// the same app because Kick only queues a round. fn must not call WithApp again
// for the same app.
func (r *Reconciler) WithApp(ctx context.Context, app string, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	w := r.workerLocked(app)
	r.mu.Unlock()
	return w.with(ctx, fn)
}

func (w *worker) with(ctx context.Context, fn func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case w.token <- struct{}{}:
		defer func() { <-w.token }()
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// workerLoop runs one app's rounds strictly serially with explicit API operations.
func (r *Reconciler) workerLoop(w *worker) {
	for range w.kick {
		ctx := context.Background()
		_ = w.with(ctx, func() error {
			r.round(ctx, w.app)
			return nil
		})
	}
}

// round performs one full reconcile pass for a single app.
func (r *Reconciler) round(ctx context.Context, app string) {
	// runner ping gating
	if _, err := r.cfg.Runner.Ping(ctx); err != nil {
		r.throttledPingEvent(ctx, app)
		return
	}

	st := r.cfg.Store
	appRec, err := st.GetApp(ctx, app)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return
		}
		r.log.Error("get app", "app", app, "err", err)
		return
	}
	pending, err := st.PendingDeployments(ctx, app)
	if err != nil {
		r.log.Error("pending deployments", "app", app, "err", err)
		return
	}
	volumes, err := st.ListVolumes(ctx, app)
	if err != nil {
		r.log.Error("list volumes", "app", app, "err", err)
		return
	}
	addons, err := st.ListAddons(ctx, app)
	if err != nil {
		r.log.Error("list addons", "app", app, "err", err)
		return
	}
	// env is the app's own variables plus the add-on connection URLs (as
	// secrets), so they are injected into new containers and redacted from
	// diagnoses like any other secret.
	env, err := r.appEnv(ctx, app, addons)
	if err != nil {
		r.log.Error("list env", "app", app, "err", err)
		return
	}

	// N4.2: add-on containers are ensured before any deployment advances, so a
	// new app container finds its database already running.
	r.reconcileAddons(ctx, appRec, addons)

	// 2.2.2 supersede: keep only the newest pending deployment.
	pending = r.supersede(ctx, app, pending, env)

	// 2.2.3 advance the single pending deployment.
	if len(pending) == 1 {
		d := &pending[0]
		// Drive the deployment through as many stages as possible in this
		// round. Stop when it is terminal, or when a step made no progress
		// (runner unavailable, route retry): the next kick or tick resumes it.
		for step := 0; step < 8; step++ {
			before := d.Status
			r.advance(ctx, appRec, d, env, volumes)
			cur, err := st.GetDeployment(ctx, d.ID)
			if err != nil {
				break
			}
			*d = cur
			if !state.IsPending(d.Status) || d.Status == before {
				break
			}
			if v, err := st.ListVolumes(ctx, app); err == nil {
				volumes = v // the build step may have added image VOLUME paths
			}
		}
		// re-read app since SetCurrentDeployment may have changed it
		if a2, err := st.GetApp(ctx, app); err == nil {
			appRec = a2
		}
	}

	// re-read observed containers after advancing
	containers, err := r.cfg.Runner.ListContainers(ctx, app)
	if err != nil {
		r.log.Error("list containers", "app", app, "err", err)
		return
	}

	// 2.4 converge actual state.
	r.converge(ctx, appRec, containers)

	// route sync on change (always on first round)
	r.syncRoutesIfChanged(ctx)

	// N3: probe custom domains and update their status/diagnosis.
	r.reconcileDomains(ctx, appRec, containers)

	// 2.5 per-app GC.
	r.appGC(ctx, app, pending)
}

// supersede marks all but the newest pending deployment as superseded, removing
// their containers and uploads. Returns the surviving pending slice (0 or 1).
func (r *Reconciler) supersede(ctx context.Context, app string, pending []state.Deployment, env []state.EnvVar) []state.Deployment {
	if len(pending) <= 1 {
		return pending
	}
	// PendingDeployments is oldest first; the last is the newest.
	newest := pending[len(pending)-1]
	for i := 0; i < len(pending)-1; i++ {
		r.markSuperseded(ctx, app, pending[i], env)
	}
	return []state.Deployment{newest}
}

func (r *Reconciler) markSuperseded(ctx context.Context, app string, d state.Deployment, env []state.EnvVar) {
	_, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
		dep.Status = state.StatusSuperseded
		return nil
	})
	if err != nil {
		r.log.Error("supersede", "deployment", d.ID, "err", err)
		return
	}
	r.event(ctx, app, d.ID, "deploy", "被更新的提交取代")
	// remove any container this deployment created, keeping its output
	r.retireContainer(ctx, app, runner.ContainerName(app, d.ID), d, env, nil)
	r.removeUpload(d)
}

// advance drives one pending deployment forward one step per round. Each status
// transition is written to the store before the side effect so a crash can
// resume from the persisted status.
func (r *Reconciler) advance(ctx context.Context, app state.App, d *state.Deployment, env []state.EnvVar, volumes []state.Volume) {
	switch d.Status {
	case state.StatusQueued:
		r.doBuild(ctx, app, d, env)
	case state.StatusBuilding:
		r.resumeBuild(ctx, app, d, env)
	case state.StatusStarting:
		r.doStart(ctx, app, d, env, volumes)
	case state.StatusChecking:
		r.doCheck(ctx, app, d, env)
	case state.StatusRouting:
		r.doRoute(ctx, app, d)
	}
}

// doBuild: queued -> building (+Attempts) -> build.
func (r *Reconciler) doBuild(ctx context.Context, app state.App, d *state.Deployment, env []state.EnvVar) {
	updated, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
		dep.Status = state.StatusBuilding
		dep.Attempts++
		return nil
	})
	if err != nil {
		r.log.Error("to building", "deployment", d.ID, "err", err)
		return
	}
	*d = updated
	r.event(ctx, app.Name, d.ID, "build", buildStartMessage(d.SourceKind))
	r.build(ctx, app, d, env)
}

// resumeBuild handles a deployment found in building after a restart.
func (r *Reconciler) resumeBuild(ctx context.Context, app state.App, d *state.Deployment, env []state.EnvVar) {
	if d.Attempts >= state.MaxBuildAttempts {
		r.fail(ctx, app, d, env, &state.Diagnosis{
			Stage:   "build",
			Code:    "interrupted",
			Message: "构建被中断两次",
			Hint:    "请重新提交部署",
		})
		return
	}
	updated, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
		dep.Attempts++
		return nil
	})
	if err != nil {
		r.log.Error("resume build attempts", "deployment", d.ID, "err", err)
		return
	}
	*d = updated
	// The runner returns an existing image without rebuilding, so a completed
	// build is not redone.
	r.build(ctx, app, d, env)
}

// build performs the source-kind specific build/resolve, then moves to starting.
func (r *Reconciler) build(ctx context.Context, app state.App, d *state.Deployment, env []state.EnvVar) {
	var imageID string
	var imageVolumes []string

	switch d.SourceKind {
	case state.SourceUpload:
		img, ok := r.runBuild(ctx, app, d, env, d.SourceRef, "")
		if !ok {
			return
		}
		if img != nil {
			imageID = img.ID
			imageVolumes = img.Volumes
		}
	case state.SourceGit:
		// Clone the repo into a tar.gz, then build it like an upload. The clone
		// itself is network I/O and is not gated by the build semaphore; the
		// build is.
		tarball, gitOutput, diag, gerr := r.buildGitTarball(ctx, d)
		if gerr != nil {
			// Transient/internal failure: stay in building, retry next round and
			// hand the crash-retry attempt back.
			r.log.Error("git clone", "deployment", d.ID, "err", gerr)
			r.giveBackAttempt(ctx, d)
			return
		}
		if diag != nil {
			text := lastLines(gitOutput, gitLogLines)
			if text == "" {
				text = diag.Message
			}
			r.saveBuildLog(ctx, app.Name, *d, text+"\n")
			r.fail(ctx, app, d, env, diag)
			return
		}
		img, ok := r.runBuild(ctx, app, d, env, tarball, gitOutput)
		if !ok {
			return
		}
		if img != nil {
			imageID = img.ID
			imageVolumes = img.Volumes
		}
	case state.SourceImage:
		// Rollback and already-resolved deployments reference an image present
		// locally: inspect it. A fresh `deploy --image` references an external
		// image that must be pulled first (stage=image).
		reuse := strings.HasPrefix(d.SourceRef, "sha256:")
		info, err := r.cfg.Runner.ImageInspect(ctx, app.Name, d.SourceRef)
		if err == nil {
			imageID = info.ID
			imageVolumes = info.Volumes
			if !reuse && r.cfg.DeployLogs != nil {
				if built, _ := r.cfg.DeployLogs.Has(app.Name, d.ID); !built {
					r.saveBuildLog(ctx, app.Name, *d, fmt.Sprintf(buildLogLocalPattern, d.SourceRef)+"\n")
				}
			}
			break
		}
		if !errors.Is(err, runner.ErrNotFound) {
			r.log.Error("image inspect", "deployment", d.ID, "err", err)
			r.giveBackAttempt(ctx, d)
			return // transient: retry next round
		}
		if reuse {
			// An image ID cannot be pulled: the image this version used was
			// collected or deleted by hand.
			r.fail(ctx, app, d, env, &state.Diagnosis{
				Stage:   "image",
				Code:    "image_gone",
				Message: "这一版的程序已不在本机（可能被自动清理或手动删除）",
				Hint:    "请重新部署那一版的代码",
			})
			return
		}
		resp, perr := r.cfg.Runner.PullImage(ctx, app.Name, d.ID, d.SourceRef)
		if perr != nil {
			r.log.Error("image pull", "deployment", d.ID, "err", perr)
			r.giveBackAttempt(ctx, d)
			return // transient: retry next round
		}
		pullLog := buildLogPullPrefix + d.SourceRef + "\n"
		if !resp.OK && resp.Failure != nil {
			pullLog += resp.Failure.Message + "\n"
			if resp.Failure.LogExcerpt != "" {
				pullLog += strings.TrimRight(resp.Failure.LogExcerpt, "\n") + "\n"
			}
		}
		r.saveBuildLog(ctx, app.Name, *d, pullLog)
		if !resp.OK {
			diag := &state.Diagnosis{Stage: "image", Code: "pull_failed", Message: "拉取镜像失败"}
			if resp.Failure != nil {
				diag.Stage = resp.Failure.Stage
				diag.Code = resp.Failure.Code
				diag.Message = resp.Failure.Message
				diag.LogExcerpt = resp.Failure.LogExcerpt
				diag.Hint = resp.Failure.Hint
			}
			r.fail(ctx, app, d, env, diag)
			return
		}
		if resp.Emulated {
			r.event(ctx, app.Name, d.ID, "image", "这个镜像只有 amd64（x86）版本，苹果芯片上靠转译运行，可能较慢")
		}
		if resp.Image != nil {
			imageID = resp.Image.ID
			imageVolumes = resp.Image.Volumes
		}
	default:
		r.log.Error("unknown source kind", "deployment", d.ID, "kind", d.SourceKind)
		return
	}

	// auto-save each image VOLUME path
	for _, p := range imageVolumes {
		_, created, err := r.cfg.Store.AddVolume(ctx, app.Name, p, true)
		if err != nil {
			r.log.Error("add auto volume", "app", app.Name, "path", p, "err", err)
			continue
		}
		if created {
			r.event(ctx, app.Name, d.ID, "build", "自动保存数据目录 "+p)
		}
	}

	// persist ImageID and move to starting
	updated, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
		dep.ImageID = imageID
		dep.Status = state.StatusStarting
		return nil
	})
	if err != nil {
		r.log.Error("to starting", "deployment", d.ID, "err", err)
		return
	}
	*d = updated
	r.event(ctx, app.Name, d.ID, "build", "镜像就绪，启动容器")
}

// runBuild builds contextPath under the global build semaphore and saves
// the build log (gitOutput first, for git sources). ok is false when the
// round must stop: a transient runner error (the attempt is given back and
// the build retried later) or a terminal failure (the deployment is failed).
func (r *Reconciler) runBuild(ctx context.Context, app state.App, d *state.Deployment, env []state.EnvVar, contextPath, gitOutput string) (*runner.ImageInfo, bool) {
	select {
	case r.buildSem <- struct{}{}:
	case <-ctx.Done():
		return nil, false
	}
	resp, err := r.cfg.Runner.Build(ctx, runner.BuildRequest{
		App:          app.Name,
		DeploymentID: d.ID,
		ContextPath:  contextPath,
	})
	<-r.buildSem
	if errors.Is(err, runner.ErrResponseTooLarge) {
		// Retrying would rebuild and overflow again: fail for good.
		r.log.Error("build", "deployment", d.ID, "err", err)
		r.saveBuildLog(ctx, app.Name, *d, buildLogText(gitOutput, runner.BuildResponse{Failure: &runner.Failure{Message: buildLogTooLarge}}))
		r.fail(ctx, app, d, env, &state.Diagnosis{
			Stage:   "build",
			Code:    "runner_response_too_large",
			Message: "构建输出过大",
			Hint:    "请减少构建时的输出后重新部署",
		})
		return nil, false
	}
	if err != nil {
		r.log.Error("build", "deployment", d.ID, "err", err)
		// Transient runner/transport failure: stay in building and retry on a
		// later round. Attempts only count builds interrupted by a server
		// crash, so give this attempt back.
		r.giveBackAttempt(ctx, d)
		return nil, false
	}
	r.saveBuildLog(ctx, app.Name, *d, buildLogText(gitOutput, resp))
	if !resp.OK {
		diag := &state.Diagnosis{Stage: "build", Code: "build_failed", Message: "构建失败"}
		if resp.Failure != nil {
			diag.Stage = resp.Failure.Stage
			diag.Code = resp.Failure.Code
			diag.Message = resp.Failure.Message
			diag.LogExcerpt = resp.Failure.LogExcerpt
			diag.Hint = resp.Failure.Hint
		}
		r.fail(ctx, app, d, env, diag)
		return nil, false
	}
	return resp.Image, true
}

// giveBackAttempt undoes the Attempts increment of a build that did not run
// to completion for a transient reason.
func (r *Reconciler) giveBackAttempt(ctx context.Context, d *state.Deployment) {
	if u, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
		if dep.Attempts > 0 {
			dep.Attempts--
		}
		return nil
	}); err == nil {
		*d = u
	}
}

// doStart creates and starts the container, then moves to checking.
func (r *Reconciler) doStart(ctx context.Context, app state.App, d *state.Deployment, env []state.EnvVar, volumes []state.Volume) {
	// The new container starts only once every add-on
	// accepts connections; otherwise the deployment fails at stage "addon".
	if addons, err := r.cfg.Store.ListAddons(ctx, app.Name); err != nil {
		r.log.Error("list addons", "app", app.Name, "err", err)
		return // retry next round
	} else if len(addons) > 0 {
		ok, problem := r.waitAddonsReady(ctx, app, addons)
		if problem != nil {
			r.fail(ctx, app, d, env, r.addonDiagnosis(ctx, app, problem, addonSecrets(env, addons)))
			return
		}
		if !ok {
			return // runner unavailable; stay in starting
		}
	}

	// resolve port
	port, declared := r.containerPort(ctx, app, d.ImageID)
	if !declared {
		r.event(ctx, app.Name, d.ID, "start", "未声明 EXPOSE，按 8080 处理")
	}

	// ensure volumes
	mounts := make([]runner.Mount, 0, len(volumes))
	for _, v := range volumes {
		if err := r.cfg.Runner.EnsureVolume(ctx, app.Name, v.VolumeName); err != nil {
			r.log.Error("ensure volume", "app", app.Name, "volume", v.VolumeName, "err", err)
			return // transient; retry next round in starting
		}
		mounts = append(mounts, runner.Mount{Volume: v.VolumeName, Path: v.Path})
	}

	envMap := make(map[string]string, len(env))
	for _, e := range env {
		envMap[e.Key] = e.Value
	}

	image := d.ImageID
	if image == "" {
		image = runner.ImageTag(app.Name, d.ID)
	}

	memMB := app.MemoryMB
	if memMB <= 0 {
		memMB = state.DefaultMemoryMB
	}
	cpu := app.CPUMilli
	if cpu <= 0 {
		cpu = state.DefaultCPUMilli
	}

	_, err := r.cfg.Runner.EnsureContainer(ctx, runner.EnsureContainerRequest{
		App:          app.Name,
		DeploymentID: d.ID,
		Image:        image,
		Port:         port,
		Env:          envMap,
		Mounts:       mounts,
		MemoryMB:     memMB,
		CPUMilli:     cpu,
		Command:      app.Command,
		PidsLimit:    app.PidsLimit,
		Links:        app.Links,
	})
	if err != nil {
		name := runner.ContainerName(app.Name, d.ID)
		r.retireContainer(ctx, app.Name, name, *d, env, func(logs string) {
			r.fail(ctx, app, d, env, &state.Diagnosis{
				Stage:      "start",
				Code:       "start_failed",
				Message:    "容器启动失败",
				LogExcerpt: logs,
			})
		})
		return
	}

	updated, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
		dep.Status = state.StatusChecking
		return nil
	})
	if err != nil {
		r.log.Error("to checking", "deployment", d.ID, "err", err)
		return
	}
	*d = updated
	r.event(ctx, app.Name, d.ID, "start", "容器已启动，等待应用响应")
}

// containerPort is the app's container port: explicit, else the image's first
// EXPOSE, else 8080. declared is false only in the 8080 fallback case.
func (r *Reconciler) containerPort(ctx context.Context, app state.App, imageID string) (port int, declared bool) {
	if app.Port != 0 {
		return app.Port, true
	}
	if info, err := r.cfg.Runner.ImageInspect(ctx, app.Name, imageID); err == nil && len(info.ExposedPorts) > 0 {
		return info.ExposedPorts[0], true
	}
	return 8080, false
}

// doCheck runs the health check loop, then moves to routing (or fails).
func (r *Reconciler) doCheck(ctx context.Context, app state.App, d *state.Deployment, env []state.EnvVar) {
	name := runner.ContainerName(app.Name, d.ID)
	healthPath := app.HealthPath
	if healthPath == "" {
		healthPath = state.DefaultHealthPath
	}

	containerPort, _ := r.containerPort(ctx, app, d.ImageID)
	deadline := r.cfg.Now().Add(r.cfg.HealthTimeout)
	for {
		ci, ok := r.findContainer(ctx, app.Name, name)
		if ok {
			// container-level failure detection
			if ci.OOMKilled {
				r.retireContainer(ctx, app.Name, name, *d, env, func(logs string) {
					r.fail(ctx, app, d, env, &state.Diagnosis{
						Stage:      "health",
						Code:       "out_of_memory",
						Message:    "容器因内存超限被杀死",
						LogExcerpt: logs,
						Hint:       "请调高内存上限（acornfox app set --memory）",
					})
				})
				return
			}
			if ci.RestartCount > 0 || ci.Restarting || (ci.State != "" && ci.State != "running") {
				r.retireContainer(ctx, app.Name, name, *d, env, func(logs string) {
					r.fail(ctx, app, d, env, &state.Diagnosis{
						Stage:      "health",
						Code:       "container_exited",
						Message:    "容器启动后退出或反复重启",
						LogExcerpt: logs,
						Hint:       "请检查启动命令与日志",
					})
				})
				return
			}
			if ci.HostPort > 0 {
				hostport := fmt.Sprintf("127.0.0.1:%d", ci.HostPort)
				if err := r.cfg.Probe(ctx, hostport, healthPath); err == nil {
					goto healthy
				}
			}
		}
		if !r.cfg.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
	// timed out
	r.retireContainer(ctx, app.Name, name, *d, env, func(logs string) {
		r.fail(ctx, app, d, env, &state.Diagnosis{
			Stage:      "health",
			Code:       "port_not_listening",
			Message:    "健康检查超时，端口未监听",
			LogExcerpt: logs,
			Hint:       fmt.Sprintf("请确认应用在容器内监听 0.0.0.0:%d（可用 EXPOSE 声明端口）", containerPort),
		})
	})
	return

healthy:
	// unpersisted database warning (non-fatal)
	if files, err := r.cfg.Runner.Diff(ctx, app.Name, name); err == nil && len(files) > 0 {
		dir := filepath.Dir(files[0])
		warn := state.Diagnosis{
			Stage:   "data",
			Code:    "unpersisted_database",
			Message: "检测到未持久化的数据库文件：" + strings.Join(files, ", "),
			Hint:    "acornfox volume add " + dir,
		}
		_, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
			dep.Warnings = append(dep.Warnings, warn)
			return nil
		})
		if err != nil {
			r.log.Error("append warning", "deployment", d.ID, "err", err)
		}
		r.event(ctx, app.Name, d.ID, "data", warn.Message)
	}

	updated, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
		dep.Status = state.StatusRouting
		return nil
	})
	if err != nil {
		r.log.Error("to routing", "deployment", d.ID, "err", err)
		return
	}
	*d = updated
	r.event(ctx, app.Name, d.ID, "health", "健康检查通过，切换流量")
}

// doRoute switches Caddy to the new container, with retry and a 5-minute budget.
func (r *Reconciler) doRoute(ctx context.Context, app state.App, d *state.Deployment) {
	routes, err := r.desiredRoutes(ctx, app.Name, d)
	if err != nil {
		r.log.Error("compute routes", "app", app.Name, "err", err)
		return
	}
	if err := r.syncRoutes(ctx, routes); err != nil {
		r.log.Error("route sync", "app", app.Name, "deployment", d.ID, "err", err)
		r.event(ctx, app.Name, d.ID, "route", "路由同步失败，稍后重试："+err.Error())
		r.routeFailMu.Lock()
		first, ok := r.routeFailSince[d.ID]
		if !ok {
			first = r.cfg.Now()
			r.routeFailSince[d.ID] = first
		}
		expired := r.cfg.Now().Sub(first) > routeFailureBudget
		r.routeFailMu.Unlock()

		if expired {
			r.routeFailMu.Lock()
			delete(r.routeFailSince, d.ID)
			r.routeFailMu.Unlock()
			r.fail(ctx, app, d, nil, &state.Diagnosis{
				Stage:   "route",
				Code:    "route_failed",
				Message: "路由持续同步失败超过 5 分钟",
				Hint:    "请检查 Caddy admin 是否可用",
			})
			r.retireContainer(ctx, app.Name, runner.ContainerName(app.Name, d.ID), *d, nil, nil)
		}
		return // stay in routing, retry next round
	}
	r.routeFailMu.Lock()
	delete(r.routeFailSince, d.ID)
	r.routeFailMu.Unlock()

	if err := r.cfg.Store.SetCurrentDeployment(ctx, app.Name, d.ID); err != nil {
		r.log.Error("set current deployment", "deployment", d.ID, "err", err)
		return
	}
	r.removeUpload(*d)
	r.event(ctx, app.Name, d.ID, "deploy", fmt.Sprintf("已上线 http://%s:%d", r.cfg.PublicHost, app.PublicPort))
}

// fail marks a deployment failed with a redacted diagnosis and removes upload.
func (r *Reconciler) fail(ctx context.Context, app state.App, d *state.Deployment, env []state.EnvVar, diag *state.Diagnosis) {
	if env == nil {
		env, _ = r.appEnv(ctx, app.Name, nil)
	}
	r.redactDiagnosis(diag, env)
	updated, err := r.cfg.Store.UpdateDeployment(ctx, d.ID, func(dep *state.Deployment) error {
		dep.Status = state.StatusFailed
		dep.Diagnosis = diag
		return nil
	})
	if err != nil {
		r.log.Error("to failed", "deployment", d.ID, "err", err)
		return
	}
	*d = updated
	r.event(ctx, app.Name, d.ID, diag.Stage, diag.Message)
	r.removeUpload(*d)
}

// converge makes actual containers match the expected set for one app (2.4).
func (r *Reconciler) converge(ctx context.Context, app state.App, containers []runner.ContainerInfo) {
	// determine live deployment container name (if any)
	liveName := ""
	if app.CurrentDeployment != "" {
		liveName = runner.ContainerName(app.Name, app.CurrentDeployment)
	}

	// pending deployment currently occupying a container (starting/checking/routing)
	pendingNames := map[string]struct{}{}
	pending, _ := r.cfg.Store.PendingDeployments(ctx, app.Name)
	for _, d := range pending {
		switch d.Status {
		case state.StatusStarting, state.StatusChecking, state.StatusRouting:
			pendingNames[runner.ContainerName(app.Name, d.ID)] = struct{}{}
		}
	}

	// remove unexpected app-role containers
	var liveContainer *runner.ContainerInfo
	for i := range containers {
		c := containers[i]
		if c.Role != runner.RoleApp {
			continue
		}
		expected := (liveName != "" && c.Name == liveName)
		if _, ok := pendingNames[c.Name]; ok {
			expected = true
		}
		if !expected {
			// Keep the output of a known deployment of this app (usually the
			// version just replaced) before its container goes.
			if dep, err := r.cfg.Store.GetDeployment(ctx, c.DeploymentID); err == nil && dep.App == app.Name {
				r.retireContainer(ctx, app.Name, c.Name, dep, nil, nil)
			} else {
				_ = r.cfg.Runner.RemoveContainer(ctx, app.Name, c.Name)
			}
			r.event(ctx, app.Name, "", "converge", "清理旧容器 "+c.Name)
			continue
		}
		if c.Name == liveName {
			liveContainer = &containers[i]
		}
	}

	if liveName == "" {
		return // no live deployment; nothing to keep running
	}

	switch app.Desired {
	case state.DesiredRunning:
		if liveContainer == nil {
			// live container missing: recreate from live deployment's ImageID
			r.recreateLive(ctx, app)
		} else if !liveContainer.Running {
			if _, err := r.cfg.Runner.StartContainer(ctx, app.Name, liveName); err != nil {
				r.log.Error("start live", "app", app.Name, "err", err)
			}
		}
	case state.DesiredStopped:
		if liveContainer != nil && liveContainer.Running {
			if err := r.cfg.Runner.StopContainer(ctx, app.Name, liveName); err != nil {
				r.log.Error("stop live", "app", app.Name, "err", err)
			}
		}
	}
}

// recreateLive re-ensures the live container after it went missing.
func (r *Reconciler) recreateLive(ctx context.Context, app state.App) {
	kept, err := r.cfg.Store.KeptImageDeployments(ctx, app.Name)
	if err != nil {
		r.log.Error("kept deployments", "app", app.Name, "err", err)
		return
	}
	var live *state.Deployment
	for i := range kept {
		if kept[i].ID == app.CurrentDeployment {
			live = &kept[i]
			break
		}
	}
	if live == nil || live.ImageID == "" {
		return
	}
	port, _ := r.containerPort(ctx, app, live.ImageID)
	env, _ := r.appEnv(ctx, app.Name, nil)
	envMap := make(map[string]string, len(env))
	for _, e := range env {
		envMap[e.Key] = e.Value
	}
	volumes, _ := r.cfg.Store.ListVolumes(ctx, app.Name)
	mounts := make([]runner.Mount, 0, len(volumes))
	for _, v := range volumes {
		_ = r.cfg.Runner.EnsureVolume(ctx, app.Name, v.VolumeName)
		mounts = append(mounts, runner.Mount{Volume: v.VolumeName, Path: v.Path})
	}
	memMB := app.MemoryMB
	if memMB <= 0 {
		memMB = state.DefaultMemoryMB
	}
	cpu := app.CPUMilli
	if cpu <= 0 {
		cpu = state.DefaultCPUMilli
	}
	if _, err := r.cfg.Runner.EnsureContainer(ctx, runner.EnsureContainerRequest{
		App:          app.Name,
		DeploymentID: live.ID,
		Image:        live.ImageID,
		Port:         port,
		Env:          envMap,
		Mounts:       mounts,
		MemoryMB:     memMB,
		CPUMilli:     cpu,
		Command:      app.Command,
		PidsLimit:    app.PidsLimit,
		Links:        app.Links,
	}); err != nil {
		r.log.Error("recreate live", "app", app.Name, "err", err)
		return
	}
	r.event(ctx, app.Name, live.ID, "converge", "容器缺失，已重建")
	if r.cfg.DeployLogs != nil {
		r.event(ctx, app.Name, live.ID, "logs", "容器丢失，之前的日志没有保存")
	}
}

// appGC removes images outside the kept set for one app (2.5 per-app part),
// then trims the app's deployment logs.
func (r *Reconciler) appGC(ctx context.Context, app string, pending []state.Deployment) {
	r.gcImages(ctx, app, pending)
	r.pruneAppLogs(ctx, app)
}

// gcImages removes the app's images that KeptImageIDs does not protect.
func (r *Reconciler) gcImages(ctx context.Context, app string, pending []state.Deployment) {
	// Protected by actual image ID, not deployment: a shared image may carry
	// an old deployment's label but still back a kept version, and a queued
	// rollback names its image only by ID in its source.
	keepImageIDs, err := r.cfg.Store.KeptImageIDs(ctx, app)
	if err != nil {
		r.log.Error("kept images", "app", app, "err", err)
		return
	}
	for _, d := range pending {
		if d.ImageID != "" {
			keepImageIDs[d.ImageID] = struct{}{}
		}
	}

	images, err := r.cfg.Runner.ListImages(ctx, app)
	if err != nil {
		r.log.Error("list images", "app", app, "err", err)
		return
	}
	for _, img := range images {
		if _, ok := keepImageIDs[img.ID]; ok {
			continue
		}
		if err := r.cfg.Runner.RemoveImage(ctx, app, img.ID); err != nil {
			// ignore "image still in use" errors
			r.log.Debug("remove image", "app", app, "image", img.ID, "err", err)
		}
	}
}

// globalGC runs once per Tick: orphan containers and stale uploads (2.5 global).
func (r *Reconciler) globalGC(ctx context.Context, appNames []string) {
	known := map[string]struct{}{}
	for _, n := range appNames {
		known[n] = struct{}{}
	}

	// orphan containers: acornfox.app not in ListApps
	all, err := r.cfg.Runner.ListContainers(ctx, "")
	if err != nil {
		r.log.Error("list all containers", "err", err)
	} else {
		for _, c := range all {
			if _, ok := known[c.App]; !ok {
				_ = r.cfg.Runner.RemoveContainer(ctx, c.App, c.Name)
			}
		}
	}

	// stale uploads: not belonging to any pending deployment, mtime > 1h
	r.gcUploads(ctx, appNames)

	// deployment logs of deleted apps, and the total size cap
	r.pruneGlobalLogs(ctx)
}

// gcUploads removes upload files older than 1h that no pending deployment references.
func (r *Reconciler) gcUploads(ctx context.Context, appNames []string) {
	if r.cfg.UploadDir == "" {
		return
	}
	entries, err := os.ReadDir(r.cfg.UploadDir)
	if err != nil {
		return
	}
	// collect referenced upload paths
	referenced := map[string]struct{}{}
	for _, app := range appNames {
		pending, err := r.cfg.Store.PendingDeployments(ctx, app)
		if err != nil {
			continue
		}
		for _, d := range pending {
			if d.SourceKind == state.SourceUpload && d.SourceRef != "" {
				referenced[filepath.Clean(d.SourceRef)] = struct{}{}
			}
		}
	}
	cutoff := r.cfg.Now().Add(-staleUploadAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		full := filepath.Join(r.cfg.UploadDir, e.Name())
		if _, ok := referenced[filepath.Clean(full)]; ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(full)
		}
	}
}

// desiredRoutes computes the full expected route set, substituting the given
// pending deployment's container as this app's upstream when non-nil.
func (r *Reconciler) desiredRoutes(ctx context.Context, thisApp string, override *state.Deployment) ([]caddyroute.Route, error) {
	apps, err := r.cfg.Store.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	var routes []caddyroute.Route
	for _, a := range apps {
		if a.Desired == state.DesiredStopped {
			continue
		}
		var containerName string
		if a.Name == thisApp && override != nil {
			containerName = runner.ContainerName(a.Name, override.ID)
		} else if a.CurrentDeployment != "" {
			containerName = runner.ContainerName(a.Name, a.CurrentDeployment)
		} else {
			continue // no live deployment, no route
		}
		ci, ok := r.findContainer(ctx, a.Name, containerName)
		if !ok {
			continue
		}
		rt, ok := r.appRoute(a, ci)
		if !ok {
			continue
		}
		rt.Domains = r.appDomainNames(ctx, a.Name)
		routes = append(routes, rt)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].App < routes[j].App })
	return routes, nil
}

// RefreshRoutes syncs Caddy routes when they differ from the observed live
// containers. The public console uses it because its route has no
// app worker to trigger a round. Like a round it is skipped while the runner is
// unreachable, so missing container observations never delete app routes.
func (r *Reconciler) RefreshRoutes(ctx context.Context) {
	if _, err := r.cfg.Runner.Ping(ctx); err != nil {
		return
	}
	r.syncRoutesIfChanged(ctx)
}

// syncRoutesIfChanged computes routes from observed live containers and syncs
// only when different from Caddy's current view (or always on the first round).
func (r *Reconciler) syncRoutesIfChanged(ctx context.Context) {
	routes, err := r.observedRoutes(ctx)
	if err != nil {
		r.log.Error("observed routes", "err", err)
		return
	}
	r.mu.Lock()
	first := r.firstRun
	r.mu.Unlock()

	if !first {
		current, err := r.cfg.Router.Current(ctx)
		if err == nil && routesEqual(current, routes) {
			// Routes match; the edge may still need new published ports
			// (an exposure change in bridge mode leaves routes unchanged).
			if recreated, eerr := r.ensureEdge(ctx, routes, false); eerr != nil {
				r.log.Error("edge", "err", eerr)
			} else if recreated {
				if serr := r.cfg.Router.Sync(ctx, routes); serr != nil {
					r.log.Error("route sync", "err", serr)
				}
			}
			return
		}
	}
	if err := r.syncRoutes(ctx, routes); err != nil {
		r.log.Error("route sync", "err", err)
	}
}

// syncRoutes makes sure the edge container (when configured) matches routes
// and pushes routes to Caddy. A failed push with an edge configured re-checks
// the container once (it may have been removed or stopped) and retries.
func (r *Reconciler) syncRoutes(ctx context.Context, routes []caddyroute.Route) error {
	if _, err := r.ensureEdge(ctx, routes, false); err != nil {
		return err
	}
	err := r.cfg.Router.Sync(ctx, routes)
	if err == nil || r.edgeManager() == nil {
		return err
	}
	if _, eerr := r.ensureEdge(ctx, routes, true); eerr != nil {
		return eerr
	}
	return r.cfg.Router.Sync(ctx, routes)
}

// observedRoutes builds routes from live containers of running apps.
func (r *Reconciler) observedRoutes(ctx context.Context) ([]caddyroute.Route, error) {
	apps, err := r.cfg.Store.ListApps(ctx)
	if err != nil {
		return nil, err
	}
	var routes []caddyroute.Route
	for _, a := range apps {
		if a.Desired == state.DesiredStopped || a.CurrentDeployment == "" {
			continue
		}
		name := runner.ContainerName(a.Name, a.CurrentDeployment)
		ci, ok := r.findContainer(ctx, a.Name, name)
		if !ok {
			continue
		}
		rt, ok := r.appRoute(a, ci)
		if !ok {
			continue
		}
		rt.Domains = r.appDomainNames(ctx, a.Name)
		routes = append(routes, rt)
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].App < routes[j].App })
	return routes, nil
}

func (r *Reconciler) observedHostPort(ctx context.Context, app, name string) int {
	if ci, ok := r.findContainer(ctx, app, name); ok {
		return ci.HostPort
	}
	return 0
}

func (r *Reconciler) findContainer(ctx context.Context, app, name string) (runner.ContainerInfo, bool) {
	cs, err := r.cfg.Runner.ListContainers(ctx, app)
	if err != nil {
		return runner.ContainerInfo{}, false
	}
	for _, c := range cs {
		if c.Name == name {
			return c, true
		}
	}
	return runner.ContainerInfo{}, false
}

// tailLogs fetches the last 40 lines of a container's logs, redacted.
func (r *Reconciler) tailLogs(ctx context.Context, app, name string, env []state.EnvVar) string {
	lines, err := r.cfg.Runner.Logs(ctx, app, name, runner.MaxLogExcerptLine)
	if err != nil || len(lines) == 0 {
		return ""
	}
	joined := strings.Join(lines, "\n")
	return r.redactString(joined, env)
}

// throttledPingEvent writes a runner/unavailable event at most once per minute per app.
func (r *Reconciler) throttledPingEvent(ctx context.Context, app string) {
	r.pingMu.Lock()
	last, ok := r.lastPingEvt[app]
	now := r.cfg.Now()
	if ok && now.Sub(last) < pingThrottle {
		r.pingMu.Unlock()
		return
	}
	r.lastPingEvt[app] = now
	r.pingMu.Unlock()
	r.event(ctx, app, "", "runner", "runner 暂不可用，稍后重试")
}

func (r *Reconciler) event(ctx context.Context, app, deploymentID, stage, message string) {
	err := r.cfg.Store.AddEvent(ctx, state.Event{
		App:          app,
		DeploymentID: deploymentID,
		At:           r.cfg.Now(),
		Stage:        stage,
		Message:      message,
	})
	if err != nil {
		r.log.Error("add event", "app", app, "err", err)
	}
}

func (r *Reconciler) removeUpload(d state.Deployment) {
	if d.SourceKind != state.SourceUpload || d.SourceRef == "" {
		return
	}
	_ = os.Remove(d.SourceRef)
}

// redactDiagnosis masks any secret env values (len >= 4) in message/log/hint.
func (r *Reconciler) redactDiagnosis(diag *state.Diagnosis, env []state.EnvVar) {
	if diag == nil {
		return
	}
	diag.Message = r.redactString(diag.Message, env)
	diag.LogExcerpt = r.redactString(diag.LogExcerpt, env)
	diag.Hint = r.redactString(diag.Hint, env)
}

func (r *Reconciler) redactString(s string, env []state.EnvVar) string {
	if s == "" {
		return s
	}
	for _, e := range env {
		if e.Secret && len(e.Value) >= redactMinLen {
			s = strings.ReplaceAll(s, e.Value, redactMask)
		}
	}
	return s
}

func routesEqual(current map[string]caddyroute.Route, want []caddyroute.Route) bool {
	if len(current) != len(want) {
		return false
	}
	for _, rt := range want {
		c, ok := current[rt.App]
		if !ok || !routeEqual(c, rt) {
			return false
		}
	}
	return true
}

// routeEqual compares two routes on the fields the reconciler controls,
// treating Domains as an unordered set.
func routeEqual(a, b caddyroute.Route) bool {
	if a.App != b.App || a.PublicPort != b.PublicPort || a.Upstream != b.Upstream || a.Listen != b.Listen || a.LANPort != b.LANPort {
		return false
	}
	if len(a.Domains) != len(b.Domains) {
		return false
	}
	set := make(map[string]int, len(a.Domains))
	for _, d := range a.Domains {
		set[d]++
	}
	for _, d := range b.Domains {
		set[d]--
		if set[d] < 0 {
			return false
		}
	}
	return true
}

// DefaultProbe performs an HTTP GET with a 2s timeout, no redirects; any
// response with status < 500 is considered healthy (section 2.6).
func DefaultProbe(ctx context.Context, hostport, path string) error {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := "http://" + hostport + path
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext:       (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
			DisableKeepAlives: true,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("probe: status %d", resp.StatusCode)
	}
	return nil
}

func buildStartMessage(kind string) string {
	switch kind {
	case state.SourceGit:
		return "拉取 Git 仓库并构建镜像"
	case state.SourceImage:
		return "准备镜像"
	default:
		return "开始构建镜像"
	}
}
