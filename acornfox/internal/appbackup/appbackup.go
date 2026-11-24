// Package appbackup backs up and restores the data of one app, and is the
// base of the snapshot an app upgrade takes before it changes anything
// (W10-1, W10-2; used by W9-2 and W9-3).
//
// A backup is a directory under <data>/app-backups/<app>/<id>/:
//
//	meta.json     what it is: app, kind, time, the version the app ran, one
//	              entry per volume with size and sha256 (listing and
//	              verifying never need the other files)
//	records.json  what the platform knew about the app then: variables
//	              (secret values included, the file is 0600), the saved
//	              install parameters, the manifest, the add-ons and the
//	              service records that concern the app
//	vol-N.tar.gz  one volume's files (tar, gzip), exactly as the runner's
//	              ExportVolume streams them
//
// It is written under <id>.partial and renamed when whole, so a listing
// never shows half a backup. A partial left by a crash is reported by
// doctor (BKP-10) and removed by the next backup of the app.
//
// Kinds and how many are kept per app: manual (default 5, Config.KeepManual),
// pre-upgrade (taken by the upgrade before it changes anything) and
// pre-restore (taken by a restore, or by the rollback after an upgrade,
// before it overwrites anything, so that can itself be undone). The two
// automatic kinds are kept at least 7 days (Config.AutoKeepDays) and at most
// 3 each (Config.KeepPreUpgrade, KeepPreRestore): an automatic backup younger
// than 7 days is never removed to keep a count, only when the disk has no
// room for a new one (the plan names what goes), oldest first; beyond that
// the newest 3 of a kind stay and the older ones go. See retention.go.
//
// Consistency: the app (all its containers, add-ons too) is stopped for the
// copy and started again after it; nothing is read from a running
// container. An app that is stopped already is read as it is and left
// stopped. A backup does not change the app's wanted state: if this server
// dies in the middle, the reconciler starts the app again.
//
// Restore (the data of a backup replaces the current data): it first takes a
// pre-restore backup, removes the app's containers and its volumes, makes the
// volumes again, imports the backup's files, puts the records back (variables,
// parameters, manifest, add-on records), deploys the version the backup was
// taken at (the same image: a new deployment, so the usual health check
// runs) and waits until it is live. When anything on the way fails, the
// pre-restore backup is restored the same way (BKP-07); when that fails too
// the pre-restore backup stays and BKP-08 says so. The service registry is
// never rewritten: only checked afterwards, and what no longer matches is a
// warning (BKP-15).
//
// Long operations are tasks (Task, Tasks): steps with a status, the fault
// code when one failed, the result. They live in memory and are written to
// <data>/app-backups/.tasks/<id>.json; a task found running after a restart is
// marked failed (BKP-10).
//
// Operating notes (who picks this up next):
//
//   - Used by: the API server (/v1/apps/{app}/backups…, /v1/backups,
//     /v1/backups/tasks/{id}), hence `acornfox backup …`, the console and
//     the Skill; the app upgrade (SnapshotPreUpgrade, RestoreSnapshot).
//   - Needs: a runner with runner.VolumeDataAPI (BKP-11 otherwise); free
//     space in <data>/app-backups for a backup and in Docker's data root for a
//     restore (BKP-01 otherwise, nothing is touched).
//   - Side effects: stops and starts the app's containers; a restore removes
//     and recreates its volumes and may deploy another version; files under
//     <data>/app-backups; one line per operation in <data>/app-backups/operations.log
//     and an event on the app.
//   - Verify: `acornfox backup list APP`; a restore says live or what failed;
//     `acornfox doctor` checks the directory and the space.
//   - Stop / roll back: `acornfox backup rm APP ID --yes` removes one backup
//     (only the files); a restore is undone by restoring its pre-restore
//     backup.
//   - First look when something fails: the task's fault code (BKP-*,
//     `acornfox faults BKP-xx`) and <data>/app-backups/operations.log.
//
// Limits: a backup restores on the machine and app it was taken from (the
// credentials in records.json are encrypted with this machine's key);
// deleting an app leaves its backups, but a restore needs the app to exist.
package appbackup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Backup kinds.
const (
	KindManual     = "manual"
	KindPreUpgrade = "pre-upgrade"
	KindPreRestore = "pre-restore"
)

// Defaults.
const (
	DefaultKeepManual     = 5
	DefaultKeepPreUpgrade = 3
	DefaultKeepPreRestore = 3
	// DefaultAutoKeepDays is how long an automatic backup (pre-upgrade,
	// pre-restore) is safe from the count rule.
	DefaultAutoKeepDays = 7
	// DefaultHealthWait is how long a restore waits for the deployment of
	// the restored version to go live.
	DefaultHealthWait = 12 * time.Minute
	// diskMargin is what must be free beyond the data's size.
	diskMargin = 256 << 20
	// FormatVersion is the format of meta.json and records.json.
	FormatVersion = 1
)

// ValidKind reports whether k is a backup kind.
func ValidKind(k string) bool {
	return k == KindManual || k == KindPreUpgrade || k == KindPreRestore
}

// Store is what the manager needs from *state.Store.
type Store interface {
	GetApp(ctx context.Context, name string) (state.App, error)
	ListApps(ctx context.Context) ([]state.App, error)
	UpdateApp(ctx context.Context, name string, fn func(*state.App) error) (state.App, error)
	ListEnv(ctx context.Context, app string) ([]state.EnvVar, error)
	SetEnv(ctx context.Context, v state.EnvVar) error
	DeleteEnv(ctx context.Context, app, key string) error
	ListVolumes(ctx context.Context, app string) ([]state.Volume, error)
	AddVolume(ctx context.Context, app, path string, auto bool) (state.Volume, bool, error)
	ListAddons(ctx context.Context, app string) ([]state.Addon, error)
	AddAddon(ctx context.Context, addon state.Addon) (bool, error)
	RemoveAddon(ctx context.Context, app, kind string, deleteVolume bool) error
	GetCatalogSecrets(ctx context.Context, app string) (state.CatalogSecrets, error)
	SetCatalogSecrets(ctx context.Context, cs state.CatalogSecrets) error
	GetCatalogManifest(ctx context.Context, app string) (state.CatalogManifest, error)
	SetCatalogManifest(ctx context.Context, m state.CatalogManifest) error
	ListServices(ctx context.Context, f state.ServiceFilter) ([]state.Service, error)
	ListServiceBindings(ctx context.Context, f state.BindingFilter) ([]state.ServiceBinding, error)
	GetDeployment(ctx context.Context, id string) (state.Deployment, error)
	ListDeployments(ctx context.Context, app string, limit int) ([]state.Deployment, error)
	PendingDeployments(ctx context.Context, app string) ([]state.Deployment, error)
	CreateDeployment(ctx context.Context, in state.NewDeployment) (state.Deployment, bool, error)
	AddEvent(ctx context.Context, e state.Event) error
}

// Runner is what the manager needs from the runner. Volume data is the
// optional runner.VolumeDataAPI (checked when an operation starts).
type Runner interface {
	ListContainers(ctx context.Context, app string) ([]runner.ContainerInfo, error)
	StopContainer(ctx context.Context, app, name string) error
	StartContainer(ctx context.Context, app, name string) (runner.ContainerInfo, error)
	RemoveContainer(ctx context.Context, app, name string) error
	EnsureVolume(ctx context.Context, app, name string) error
	RemoveVolume(ctx context.Context, app, name string) error
	ListImages(ctx context.Context, app string) ([]runner.ImageInfo, error)
}

// Space is the free space the manager can count on.
type Space struct {
	BackupFree uint64 // the partition holding the backup directory
	DockerFree uint64 // the partition holding Docker's volumes (0: not known, not checked)
}

// Config configures New.
type Config struct {
	Dir     string // <data>/app-backups
	Store   Store
	Runner  Runner
	Version string // this AcornFox's version, recorded in meta.json

	// WithApp serializes fn with the app's reconcile rounds (the reconciler's
	// WithApp); nil runs fn directly (tests).
	WithApp func(ctx context.Context, app string, fn func() error) error
	// Kick starts a reconcile round for the app; nil: nothing.
	Kick func(app string)
	// Free reports the free space; nil (or an error) skips the space check.
	Free func(ctx context.Context) (Space, error)

	KeepManual     int // 0: DefaultKeepManual
	KeepPreUpgrade int // 0: DefaultKeepPreUpgrade
	KeepPreRestore int // 0: DefaultKeepPreRestore
	AutoKeepDays   int // 0: DefaultAutoKeepDays
	HealthWait     time.Duration
	Poll           time.Duration // how often a restore looks at the deployment (0: 1 s)

	Now func() time.Time
	Log *slog.Logger
}

// Manager runs backups and restores.
type Manager struct {
	cfg Config

	mu    sync.Mutex
	busy  map[string]string // app -> task id of what is running on it
	tasks map[string]*taskRun
	order []string // task ids, oldest first
	base  context.Context
	stop  context.CancelFunc
	wg    sync.WaitGroup
}

// New builds a manager and loads the tasks the last run left (a task still
// running there is marked failed).
func New(cfg Config) *Manager {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.KeepManual <= 0 {
		cfg.KeepManual = DefaultKeepManual
	}
	if cfg.KeepPreUpgrade <= 0 {
		cfg.KeepPreUpgrade = DefaultKeepPreUpgrade
	}
	if cfg.KeepPreRestore <= 0 {
		cfg.KeepPreRestore = DefaultKeepPreRestore
	}
	if cfg.AutoKeepDays <= 0 {
		cfg.AutoKeepDays = DefaultAutoKeepDays
	}
	if cfg.HealthWait <= 0 {
		cfg.HealthWait = DefaultHealthWait
	}
	if cfg.Poll <= 0 {
		cfg.Poll = time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{cfg: cfg, busy: map[string]string{}, tasks: map[string]*taskRun{}, base: ctx, stop: cancel}
	m.loadTasks()
	return m
}

// Close stops waiting for running tasks (they are marked failed when the
// server starts again).
func (m *Manager) Close() {
	m.stop()
	m.wg.Wait()
}

// Dir is the backup directory.
func (m *Manager) Dir() string { return m.cfg.Dir }

// Keep is how many backups of kind are kept per app.
func (m *Manager) Keep(kind string) int {
	switch kind {
	case KindPreUpgrade:
		return m.cfg.KeepPreUpgrade
	case KindPreRestore:
		return m.cfg.KeepPreRestore
	}
	return m.cfg.KeepManual
}

func (m *Manager) now() time.Time { return m.cfg.Now() }

// volumeData is the runner's volume data capability.
func (m *Manager) volumeData() (runner.VolumeDataAPI, error) {
	v, ok := m.cfg.Runner.(runner.VolumeDataAPI)
	if !ok {
		return nil, fault(CodeUnsupported, "这台机器上的执行器还不支持备份应用数据")
	}
	return v, nil
}

func (m *Manager) withApp(ctx context.Context, app string, fn func() error) error {
	if m.cfg.WithApp == nil {
		return fn()
	}
	return m.cfg.WithApp(ctx, app, fn)
}

func (m *Manager) kick(app string) {
	if m.cfg.Kick != nil {
		m.cfg.Kick(app)
	}
}

// acquire marks app as busy with task id; BKP-03 when something else runs on it.
func (m *Manager) acquire(app, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if other, ok := m.busy[app]; ok {
		return fault(CodeBusy, fmt.Sprintf("应用 %s 正在做另一件备份或恢复的事（任务 %s）", app, other))
	}
	m.busy[app] = id
	return nil
}

func (m *Manager) release(app, id string) {
	m.mu.Lock()
	if m.busy[app] == id {
		delete(m.busy, app)
	}
	m.mu.Unlock()
}

// end frees the app and then ends the task, in that order: whoever sees the
// task finished can start the next thing on the app at once.
func (m *Manager) end(t *taskRun, err error) {
	m.release(t.v.App, t.v.ID)
	t.finish(err)
}

// Busy reports the task that is running on app ("" when none).
func (m *Manager) Busy(app string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[app]
}

// appDir is the directory of app's backups.
func (m *Manager) appDir(app string) string { return filepath.Join(m.cfg.Dir, app) }

// ensureDir makes the backup directory (0700).
func (m *Manager) ensureDir(app string) error {
	if err := os.MkdirAll(m.appDir(app), 0o700); err != nil {
		return fault(CodeDirNotWritable, fmt.Sprintf("备份目录 %s 建不了：%v", m.cfg.Dir, err))
	}
	return nil
}

// event records a line in the app's history; a failure only goes to the log.
func (m *Manager) event(ctx context.Context, app, msg string) {
	if err := m.cfg.Store.AddEvent(context.WithoutCancel(ctx), state.Event{App: app, Stage: "backup", Message: msg}); err != nil {
		m.cfg.Log.Warn("backup event", "app", app, "err", err)
	}
}

// asFault is errors.As for *Error.
func asFault(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
