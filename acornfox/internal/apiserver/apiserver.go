// Package apiserver implements the N1 HTTP API of acornfox server.
//
// N1 is unauthenticated and may only listen on 127.0.0.1 or a Unix socket
// (see ValidateListen); authentication and SSH ingress arrive in N2/N3. All
// responses are JSON. Errors use the body {"error":{"code","message"}}.
//
// The server never touches Docker directly: it reads observed container state
// through a Runner (best-effort; runner errors degrade to "unavailable") and
// writes desired state through a Store. Secret environment values are never
// returned by any endpoint.
package apiserver

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/deploylog"
	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// DefaultMaxUploadBytes is the default upload cap when Config.MaxUploadBytes is
// zero: 200 MB.
const DefaultMaxUploadBytes int64 = 200 << 20

// apiVersion is the server API version reported by GET /v1/status. It must
// match client.APIVersion; the client uses it to detect version mismatches.
const apiVersion = 1

// App setting bounds for PATCH /v1/apps/{app} (contract section 4). The
// minimums are 256 MB / 0.5 core;
// the maximums apply only when the host size is unknown, otherwise the
// server-wide caps in resources.go do.
const (
	minMemoryMB = 256
	maxMemoryMB = 16384
	minCPUMilli = 500
	maxCPUMilli = 16000
)

// Store is the subset of *state.Store the API server depends on. The signatures
// match state.Store exactly so the concrete store satisfies this interface.
type Store interface {
	EnsureApp(ctx context.Context, name string) (state.App, bool, error)
	GetApp(ctx context.Context, name string) (state.App, error)
	ListApps(ctx context.Context) ([]state.App, error)
	UpdateApp(ctx context.Context, name string, fn func(*state.App) error) (state.App, error)
	DeleteApp(ctx context.Context, name string, keepVolumes bool) error

	CreateDeployment(ctx context.Context, in state.NewDeployment) (state.Deployment, bool, error)
	GetDeployment(ctx context.Context, id string) (state.Deployment, error)
	ListDeployments(ctx context.Context, app string, limit int) ([]state.Deployment, error)
	UpdateDeployment(ctx context.Context, id string, fn func(*state.Deployment) error) (state.Deployment, error)
	// Rollback: check and create in one transaction; go-live times for durations.
	CreateRollbackDeployment(ctx context.Context, req state.RollbackRequest) (state.RollbackResult, error)
	LiveTimes(ctx context.Context, deploymentIDs []string) (map[string]time.Time, error)

	SetEnv(ctx context.Context, v state.EnvVar) error
	DeleteEnv(ctx context.Context, app, key string) error
	ListEnv(ctx context.Context, app string) ([]state.EnvVar, error)
	AddVolume(ctx context.Context, app, path string, auto bool) (state.Volume, bool, error)
	ListVolumes(ctx context.Context, app string) ([]state.Volume, error)

	// N4 addons
	AddAddon(ctx context.Context, addon state.Addon) (bool, error)
	RemoveAddon(ctx context.Context, app, kind string, deleteVolume bool) error
	ListAddons(ctx context.Context, app string) ([]state.Addon, error)
	ListRemovedAddons(ctx context.Context, app string) ([]state.Addon, error)

	ListEvents(ctx context.Context, app, deploymentID string, afterID int64, limit int) ([]state.Event, error)

	// N3 console sessions and domains.
	CreateConsoleToken(ctx context.Context) (string, error)
	RedeemConsoleToken(ctx context.Context, token string) (state.ConsoleSession, string, string, error)
	TouchConsoleSession(ctx context.Context, sessionSecret string) (state.ConsoleSession, error)
	RevokeConsoleSession(ctx context.Context, sessionSecret string) error

	AddDomain(ctx context.Context, app, name string) (state.Domain, bool, error)
	RemoveDomain(ctx context.Context, app, name string) error
	ListDomains(ctx context.Context, app string) ([]state.Domain, error)

	// Server-wide resource caps.
	GetResourceLimits(ctx context.Context) (state.ResourceLimits, error)
	SetResourceLimits(ctx context.Context, l state.ResourceLimits) error
}

// Kicker triggers one reconcile round for an app. It is non-blocking.
type Kicker interface {
	Kick(app string)
}

// Runner is the subset of runner.API the API server uses to observe live
// container state and daemon health. Mutating calls are limited to explicit
// user actions (restart, deleting an add-on's data); everything else is left
// to the reconciler.
type Runner interface {
	Ping(ctx context.Context) (runner.PingResponse, error)
	ListContainers(ctx context.Context, app string) ([]runner.ContainerInfo, error)
	Logs(ctx context.Context, app, name string, tail int) ([]string, error)
	ContainerStats(ctx context.Context, app, name string) (runner.StatsResponse, error)
	// N4: container lifecycle for restart
	StopContainer(ctx context.Context, app, name string) error
	StartContainer(ctx context.Context, app, name string) (runner.ContainerInfo, error)
	// N4.2: explicit add-on data deletion (DELETE .../addons/{kind}?volumes=true)
	RemoveContainer(ctx context.Context, app, name string) error
	RemoveVolume(ctx context.Context, app, name string) error
	RemoveNetwork(ctx context.Context, app string) error
	ListVolumes(ctx context.Context, app string) ([]runner.VolumeInfo, error)
	// Which images of an app are still on this machine (rollback checks).
	ListImages(ctx context.Context, app string) ([]runner.ImageInfo, error)
	// Apply new app limits to the existing container without a restart.
	UpdateResources(ctx context.Context, req runner.UpdateResourcesRequest) error
}

// Config configures New. Store, Kicker, Runner and UploadDir are required.
type Config struct {
	Store          Store
	Kicker         Kicker
	Runner         Runner
	UploadDir      string       // directory for streamed uploads; must exist and be writable
	PublicHost     string       // host used in app URLs, e.g. the server's LAN IP
	MaxUploadBytes int64        // per-upload cap; 0 => DefaultMaxUploadBytes
	Logger         *slog.Logger // optional; defaults to slog.Default()

	// N3 (console handler): optional dependencies. New() ignores ConsoleFS.
	ConsoleFS   fs.FS        // static console assets; nil => a tiny placeholder page
	HostMetrics HostProvider // host resource snapshot; nil => reads /proc + statfs of DataDir
	Resolver    Resolver     // DNS lookups for domain warnings; nil => net.DefaultResolver
	DataDir     string       // data directory whose filesystem is reported by GET /v1/host

	// Public console management, trusted entry only; nil => 404.
	PublicConsole PublicConsole

	// Three-platform architecture (2026-11-01).
	// DefaultExposure applies to apps whose exposure is "" ("public" on Linux,
	// "lan" on macOS/Windows); empty means "public".
	DefaultExposure string
	// LANHost returns this machine's LAN address for lan_url; nil => no lan_url.
	// Only the desktop server sets it (a local-only app then opens at
	// localhost on the same computer).
	LANHost func() string
	// ServerLAN returns a Linux server's address on the user's local
	// network, or "" when it has none (a cloud or other public server). It
	// only feeds the open hint of local-only apps (open_hint); nil => "".
	ServerLAN func() string
	// LANConsole manages opening the console on the LAN; nil => 404.
	LANConsole LANConsole
	// EdgeStatus reports the Caddy container ("preparing" / "running" /
	// "error" + message); nil or "" state => not reported.
	EdgeStatus func() (state, message string)
	// Setup is the runtime setup (find or install Docker, gateway, checks);
	// nil => the /v1/setup routes answer 404.
	Setup Setup

	// DeployLogs keeps each deployment's build and run logs; nil => no
	// archived logs (GET /v1/deployments/{id}/logs answers logs_not_kept and
	// every deployment's "logs" is all false).
	DeployLogs *deploylog.Store

	// AppCenter installs apps from the catalog; nil => the /v1/catalog
	// routes answer app_center_unavailable.
	AppCenter *appinstall.Engine

	// Console pages (runtime page, home dashboard, settings):
	// HostHistory keeps the last 24 hours of host CPU, memory and disk in
	// memory; nil => GET /v1/metrics/host/history lists no ranges.
	HostHistory *HostHistory
	// CheckPorts are the ports GET /v1/runtime/checks checks on Linux (the
	// gateway admin port, the LAN console port, the apps' range); zero =>
	// no port check.
	CheckPorts hostcheck.Ports
	// Sources reports where images and files are downloaded from (shown on
	// the settings page); nil => not reported.
	Sources func() DownloadSources
	// Version is this program's version, reported by GET /v1/status.
	Version string
}

// HostProvider returns a point-in-time host resource snapshot. It must return
// available=false on any error instead of failing the request.
type HostProvider interface {
	Host(ctx context.Context) HostView
}

// Resolver looks up A/AAAA records for a hostname. net.Resolver satisfies it.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type server struct {
	store          Store
	kicker         Kicker
	runner         Runner
	uploadDir      string
	publicHost     string
	maxUploadBytes int64
	log            *slog.Logger

	// N3
	consoleFS   fs.FS
	hostMetrics HostProvider
	resolver    Resolver
	dataDir     string

	publicConsole PublicConsole

	defaultExposure string
	lanHost         func() string
	serverLAN       func() string
	lanConsole      LANConsole
	edgeStatus      func() (string, string)
	setup           Setup
	deployLogs      *deploylog.Store
	appCenter       *appinstall.Engine
	hostHistory     *HostHistory
	checkPorts      hostcheck.Ports
	sources         func() DownloadSources
	version         string

	// resMu serializes the allocation check and the write that follows it
	// (app limits, add-ons, new apps, server-wide caps), see resources.go.
	resMu sync.Mutex
}

// newServer validates the shared dependencies and builds the server value used
// by both the trusted (New) and console (NewConsole) handlers.
func newServer(cfg Config) *server {
	if cfg.Store == nil {
		panic("apiserver: Config.Store is nil")
	}
	if cfg.Kicker == nil {
		panic("apiserver: Config.Kicker is nil")
	}
	if cfg.Runner == nil {
		panic("apiserver: Config.Runner is nil")
	}
	if cfg.UploadDir == "" {
		panic("apiserver: Config.UploadDir is empty")
	}
	max := cfg.MaxUploadBytes
	if max <= 0 {
		max = DefaultMaxUploadBytes
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	resolver := cfg.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	hostMetrics := cfg.HostMetrics
	if hostMetrics == nil {
		hostMetrics = newProcHostProvider(cfg.DataDir)
	}
	return &server{
		store:          cfg.Store,
		kicker:         cfg.Kicker,
		runner:         cfg.Runner,
		uploadDir:      cfg.UploadDir,
		publicHost:     cfg.PublicHost,
		maxUploadBytes: max,
		log:            log,
		consoleFS:      cfg.ConsoleFS,
		hostMetrics:    hostMetrics,
		resolver:       resolver,
		dataDir:        cfg.DataDir,
		publicConsole:  cfg.PublicConsole,

		defaultExposure: cfg.DefaultExposure,
		lanHost:         cfg.LANHost,
		serverLAN:       cfg.ServerLAN,
		lanConsole:      cfg.LANConsole,
		edgeStatus:      cfg.EdgeStatus,
		setup:           cfg.Setup,
		deployLogs:      cfg.DeployLogs,
		appCenter:       cfg.AppCenter,
		hostHistory:     cfg.HostHistory,
		checkPorts:      cfg.CheckPorts,
		sources:         cfg.Sources,
		version:         cfg.Version,
	}
}

// effectiveExposure resolves "" to the platform default.
func (s *server) effectiveExposure(a state.App) string {
	if a.Exposure != "" {
		return a.Exposure
	}
	if s.defaultExposure != "" {
		return s.defaultExposure
	}
	return state.ExposurePublic
}

// registerV1 installs the shared /v1 API routes used by both handlers.
func (s *server) registerV1(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/apps/{app}/deployments", s.createDeployment)
	mux.HandleFunc("POST /v1/apps/{app}/rollback", s.rollback)
	mux.HandleFunc("POST /v1/apps/{app}/versions/{seq}/rollback", s.rollbackTo)
	mux.HandleFunc("POST /v1/apps/{app}/redeploy", s.redeploy)
	mux.HandleFunc("GET /v1/deployments/{id}", s.getDeployment)
	mux.HandleFunc("GET /v1/deployments/{id}/logs", s.deploymentLogs)
	mux.HandleFunc("GET /v1/apps", s.listApps)
	mux.HandleFunc("GET /v1/apps/{app}", s.getApp)
	mux.HandleFunc("PATCH /v1/apps/{app}", s.patchApp)
	mux.HandleFunc("DELETE /v1/apps/{app}", s.deleteApp)
	mux.HandleFunc("GET /v1/apps/{app}/logs", s.observedLogs)
	mux.HandleFunc("GET /v1/apps/{app}/log-batch", s.appLogBatch)
	mux.HandleFunc("GET /v1/apps/{app}/deployments", s.listDeployments)
	mux.HandleFunc("PUT /v1/apps/{app}/env/{key}", s.putEnv)
	mux.HandleFunc("DELETE /v1/apps/{app}/env/{key}", s.deleteEnv)
	mux.HandleFunc("POST /v1/apps/{app}/volumes", s.addVolume)
	mux.HandleFunc("GET /v1/apps/{app}/domains", s.listDomains)
	mux.HandleFunc("POST /v1/apps/{app}/domains", s.addDomain)
	mux.HandleFunc("DELETE /v1/apps/{app}/domains/{name}", s.removeDomain)
	mux.HandleFunc("GET /v1/apps/{app}/addons", s.listAddons)
	mux.HandleFunc("POST /v1/apps/{app}/addons", s.addAddon)
	mux.HandleFunc("DELETE /v1/apps/{app}/addons/{kind}", s.removeAddon)
	mux.HandleFunc("POST /v1/apps/{app}/stop", s.stopApp)
	mux.HandleFunc("POST /v1/apps/{app}/start", s.startApp)
	mux.HandleFunc("POST /v1/apps/{app}/restart", s.restartApp)
	mux.HandleFunc("GET /v1/metrics/host", s.getHostMetrics)
	mux.HandleFunc("GET /v1/metrics/apps/{app}", s.getAppMetrics)
	mux.HandleFunc("GET /v1/host", s.getHost)
	mux.HandleFunc("GET /v1/status", s.status)
	mux.HandleFunc("GET /v1/resources", s.getResources)
	mux.HandleFunc("PUT /v1/resources", s.putResources)
	mux.HandleFunc("GET /v1/setup", s.getSetup)
	mux.HandleFunc("GET /v1/setup/events", s.getSetupEvents)
	mux.HandleFunc("GET /v1/setup/faults", s.getSetupFaults)
	mux.HandleFunc("POST /v1/setup/start", s.postSetupStart)
	mux.HandleFunc("POST /v1/setup/resume", s.postSetupResume)
	mux.HandleFunc("POST /v1/setup/pause", s.postSetupPause)
	mux.HandleFunc("GET /v1/metrics/host/history", s.getHostHistory)
	mux.HandleFunc("GET /v1/runtime/checks", s.getRuntimeChecks)
	s.registerCatalog(mux)
}

// New builds the trusted HTTP handler (Unix socket / loopback). It serves the
// full /v1 API plus POST /v1/console/tokens, and is authenticated only by the
// socket's group permission. It panics only on a nil required dependency, which
// is a programmer error at wiring time.
func New(cfg Config) http.Handler {
	s := newServer(cfg)
	s.wireAppCenter()
	mux := http.NewServeMux()
	s.registerV1(mux)
	// The trusted entry mints console login tokens; the console entry must not.
	mux.HandleFunc("POST /v1/console/tokens", s.createConsoleToken)
	// Opening the console to the internet is trusted-entry only.
	mux.HandleFunc("GET /v1/public-console", s.publicConsoleStatus)
	mux.HandleFunc("POST /v1/public-console/verify", s.publicConsoleVerify)
	mux.HandleFunc("POST /v1/public-console/enable", s.publicConsoleEnable)
	mux.HandleFunc("DELETE /v1/public-console", s.publicConsoleDisable)
	// Opening the console on the LAN is trusted-entry only too.
	mux.HandleFunc("GET /v1/lan-console", s.getLANConsole)
	mux.HandleFunc("POST /v1/lan-console", s.postLANConsole)
	mux.HandleFunc("DELETE /v1/lan-console", s.deleteLANConsole)
	return mux
}

// mapStoreError translates state sentinel errors into HTTP responses.
func (s *server) mapStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, state.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "对象不存在")
	case errors.Is(err, state.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "状态冲突，操作被拒绝")
	case errors.Is(err, state.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid", "输入不合法")
	default:
		s.log.Error("store error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "内部错误")
	}
}
