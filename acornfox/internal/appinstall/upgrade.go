package appinstall

// Upgrading an app from the app center to the newer version of its catalog
// entry, and going back to how it was before the upgrade (W9-1 … W9-3).
//
// What it does:
//
//   - W9-1  After every catalog sync (and whenever the app list is read) the
//     catalog's version of each installed app is compared with the one
//     installed: the same app id, in the catalog the app came from, a
//     version that is newer, that this AcornFox can install (min_platform,
//     CPU architecture, manifest format). UpgradeInfos gives each app the
//     answer (`catalog_update`, `upgrade` in the app's view); a new version
//     found is told once, as an event of the app.
//   - W9-2  The user asks (never by itself): StartUpgrade checks everything
//     that can be refused first (UpgradePlan: nothing was touched, the plan
//     says what is missing), then, as one task: fetches the new image while
//     the app keeps running, takes the pre-upgrade snapshot (the app's data
//     and records, the app is paused while it is copied; refused when the
//     disk cannot hold it), and applies the new manifest with the values the
//     app has (reapply.go with ReapplyRequest.Upgrade: the environment, the
//     start command, the config files the platform owns, a new container
//     from the new image, the health check, the steps of the manifest).
//   - W9-3  Any step that fails after the snapshot goes back to it: the
//     snapshot is restored (data, variables, parameters, manifest, the old
//     program), so the app is as it was before, and the task says so
//     (APP-UPG-05). When even that fails the fault is APP-UPG-06 and the app
//     needs a person. After a successful upgrade the snapshot stays (one per
//     app) and StartRollback goes back to it on the user's word, losing what
//     was written after the upgrade (the data of the moment is kept first as
//     a pre-restore backup).
//
// Rules (all in code, none left to whoever calls):
//
//   - The platform never installs another app or plugs a service for the
//     upgrade: what the new version needs and the app does not have stops it
//     (APP-UPG-02, APP-UPG-03) and says what.
//   - An upgrade restarts the app, so it needs confirm_restart; going back
//     loses data, so it needs confirm. Plans never do.
//   - Nothing in here knows an app: what changes comes from the old and the
//     new manifest alone.
//   - The upgrade never goes ahead without the snapshot, except for an app
//     that has no data directory (nothing to keep): then only the program
//     and the settings are put back on a failure and the user is told there
//     is nothing to go back to afterwards.
//
// Side effects: pulls (or loads) the new image; stops the app for the copy of
// its data and starts it again; writes <backup dir>/<app>/<id> (the
// snapshot, which replaces the earlier one); changes the app's environment,
// start command, config files, port and health check, makes a new deployment
// (the history shows "reapply"); runs the manifest's on_reapply steps; saves
// the new manifest and version; writes events of the app and
// <data>/upgrade-notified.json (which version was told).
//
// Verify: `acornfox apps outdated`, `acornfox apps upgrade APP --plan`
// (read-only), `acornfox install status TASK --wait`, then `acornfox status
// APP` and `acornfox apps vars APP`.
// Stop / roll back: a failed upgrade puts itself back; a finished one is
// undone with `acornfox apps rollback APP` (data written after the upgrade
// is lost). A server that stops in the middle puts the app back at its next
// start (recoverReapply).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/appbackup"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Kinds of the tasks an upgrade makes.
const (
	KindUpgrade  = "upgrade"
	KindRollback = "rollback"
)

// isOpKind reports whether k is the kind of a task that works on an
// installed app (the table of tasks only knows app and recipe: the real kind
// is in the task's data).
func isOpKind(k string) bool {
	return k == KindReapply || k == KindUpgrade || k == KindRollback
}

// eventStage is the stage of the events an upgrade writes.
const eventStage = "upgrade"

// Snapshots is what the engine needs from the app backups (the pre-upgrade
// snapshot and going back to it); *appbackup.Manager is it.
type Snapshots interface {
	PlanBackup(ctx context.Context, app string, o appbackup.BackupOptions) (appbackup.BackupPlan, error)
	SnapshotPreUpgrade(ctx context.Context, app, note string) (appbackup.Meta, error)
	Latest(app, kind string) (appbackup.Meta, bool, error)
	PlanRestore(ctx context.Context, app, id string, verify bool) (appbackup.RestorePlan, error)
	RestoreSnapshot(ctx context.Context, app, id string, o appbackup.RestoreOptions) (appbackup.TaskResult, error)
}

// SetSnapshots wires the app backups. Without them no app can be upgraded.
func (e *Engine) SetSnapshots(s Snapshots) {
	e.mu.Lock()
	e.snaps = s
	e.mu.Unlock()
}

func (e *Engine) snapshots() Snapshots {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snaps
}

// ---- what the views carry (W9-1) ----

// UpgradeInfo is what an installed app's view says about upgrading
// (`upgrade` in GET /v1/apps and /v1/apps/{app}).
type UpgradeInfo struct {
	// Available is the newer version of the app this machine can install, ""
	// when there is none (it is also the view's catalog_update).
	Available string `json:"available"`
	// CanUpgrade: asking for the upgrade is not refused for a reason known
	// without reading the new package (Blocked says what, with its code).
	// What the package asks for is in the plan (POST …/upgrade with plan).
	CanUpgrade  bool     `json:"can_upgrade"`
	Blocked     string   `json:"blocked"`
	BlockedCode string   `json:"blocked_code"`
	Notes       []string `json:"notes"`
	// Snapshot is the one pre-upgrade snapshot kept for the app (nil: none).
	Snapshot *SnapshotInfo `json:"snapshot"`
	// CanRollback: going back to it is possible now.
	CanRollback bool `json:"can_rollback"`
}

// SnapshotInfo is the pre-upgrade snapshot of an app.
type SnapshotInfo struct {
	ID        string    `json:"id"`
	Version   string    `json:"version"` // the app's version when it was taken
	CreatedAt time.Time `json:"created_at"`
	SizeMB    int       `json:"size_mb"`
}

// UpgradeInfos works out the upgrade info of each app of apps that was
// installed from the app center (the others have none), reading the catalog
// once. detail also checks that going back is possible (reads the snapshot's
// files and asks the runner which programs are still here).
func (e *Engine) UpgradeInfos(ctx context.Context, apps []state.App, detail bool) map[string]*UpgradeInfo {
	out := map[string]*UpgradeInfo{}
	var views []catalogView
	loaded := false
	for _, a := range apps {
		if a.CatalogID == "" {
			continue
		}
		if !loaded && e.cfg.Catalog != nil {
			views, _ = e.cfg.Catalog.views()
			loaded = true
		}
		out[a.Name] = e.upgradeInfo(ctx, a, views, detail)
	}
	return out
}

func (e *Engine) upgradeInfo(ctx context.Context, a state.App, views []catalogView, detail bool) *UpgradeInfo {
	info := &UpgradeInfo{Notes: []string{}}
	e.fillSnapshot(ctx, a, info, detail)
	avail, code, msg := e.newerVersion(a, views)
	info.Blocked, info.BlockedCode = msg, code
	if avail == "" {
		return info
	}
	info.Available = avail
	if f := e.upgradeBlocked(ctx, a); f != nil {
		info.Blocked, info.BlockedCode = f.Message, f.Code
	}
	info.CanUpgrade = info.Blocked == ""
	return info
}

// newerVersion says what the catalog an app came from has newer than the app
// (W9-1), reading the index only. avail is the version this machine can
// install ("" none). When the catalog has a newer one this machine cannot
// install, avail is "" and code and msg say why (CAT-INST-08 this AcornFox is
// too old, CAT-INST-06 another CPU, CAT-INST-36 an old format); a private
// catalog that was disconnected is CAT-INST-25. Nothing newer: all empty.
func (e *Engine) newerVersion(a state.App, views []catalogView) (avail, code, msg string) {
	var ia *catalog.IndexApp
	for _, v := range views {
		if v.origin != a.CatalogOrigin {
			continue
		}
		for i := range v.idx.Apps {
			if v.idx.Apps[i].ID == a.CatalogID {
				ia = &v.idx.Apps[i]
			}
		}
	}
	if ia == nil {
		if a.CatalogOrigin != "" && e.cfg.Catalog != nil {
			if name, disc := e.cfg.Catalog.OriginState(a.CatalogOrigin); disc {
				return "", CodeDisconnected, fmt.Sprintf("来自私有目录 %s（%s），这个目录已断开，不能再升级", name, a.CatalogOrigin)
			}
		}
		return "", "", ""
	}
	if a.CatalogVersion == "" || !appVersionNewer(a.CatalogVersion, ia.Version) {
		return "", "", ""
	}
	switch {
	case catalog.FormerIndexApp(*ia):
		return "", CodeLegacyManifest, fmt.Sprintf("目录里的 %s %s 是旧格式的清单，这一版装不了，要等它按新格式重新上架", ia.ID, ia.Version)
	case e.needsPlatform(ia.MinPlatform) != "":
		return "", CodeTooOld, fmt.Sprintf("目录里有新版本 %s，要先把 AcornFox 升级到 %s 才能升级这个应用", ia.Version, e.needsPlatform(ia.MinPlatform))
	case !e.archSupported(ia.Platforms):
		return "", CodeArch, fmt.Sprintf("目录里的新版本 %s 不支持这台机器的 CPU（linux/%s）", ia.Version, e.cfg.Arch)
	}
	return ia.Version, "", ""
}

// upgradeBlocked is the reason that is known without the new package why app
// a cannot be upgraded now: nil when none.
func (e *Engine) upgradeBlocked(ctx context.Context, a state.App) *Fault {
	if f := e.busyFault(a.Name); f != nil {
		return f
	}
	switch {
	case a.CurrentDeployment == "":
		return fault(CodeUpgCannot, nil, "%s 还没有上线过，不能升级", a.Name)
	case a.Desired == state.DesiredStopped:
		return fault(CodeUpgCannot, nil, "%s 停着：先启动它再升级（升级要在应用里做事，还要重启它）", a.Name)
	case e.snapshots() == nil:
		return fault(appbackup.CodeUnsupported, nil, "这台机器不能做升级前的快照，所以不能升级应用")
	}
	if _, legacy := e.installedWith(ctx, a.Name); legacy {
		return fault(CodeUpgCannot, nil, "%s 是用旧格式的清单装的，不能升级：要用新格式重新上架的版本删了重装", a.Name)
	}
	return nil
}

// busyFault is busy as a *Fault (nil when nothing runs on the app).
func (e *Engine) busyFault(app string) *Fault {
	err := e.busy(app)
	if err == nil {
		return nil
	}
	var f *Fault
	if errors.As(err, &f) {
		g := fault(CodeBusy, nil, "%s 正在安装、重新生效或升级（任务 %s），等它结束再来", app, f.TaskID)
		g.TaskID = f.TaskID
		return g
	}
	return fault(CodeInternal, err, "%v", err)
}

// fillSnapshot says which pre-upgrade snapshot app a has and whether going
// back to it is possible.
func (e *Engine) fillSnapshot(ctx context.Context, a state.App, info *UpgradeInfo, detail bool) {
	snaps := e.snapshots()
	if snaps == nil {
		return
	}
	meta, ok, err := snaps.Latest(a.Name, appbackup.KindPreUpgrade)
	if err != nil || !ok {
		return
	}
	info.Snapshot = &SnapshotInfo{ID: meta.ID, Version: meta.Version.CatalogVersion, CreatedAt: meta.CreatedAt, SizeMB: int((meta.Bytes + (1 << 20) - 1) >> 20)}
	if !snapshotIsOlder(a, meta) {
		return
	}
	info.CanRollback = true
	if detail {
		if rp, err := snaps.PlanRestore(ctx, a.Name, meta.ID, false); err != nil || !rp.Intact || !rp.VersionAvailable {
			info.CanRollback = false
			info.Notes = append(info.Notes, "升级前的快照用不了了（文件不齐，或那个版本的程序已经不在这台机器上），不能退回")
		}
	}
}

// snapshotIsOlder reports whether the snapshot is of another version than
// the one the app runs now (a snapshot of the version running is nothing to
// go back to).
func snapshotIsOlder(a state.App, meta appbackup.Meta) bool {
	return meta.Version.CatalogVersion != a.CatalogVersion
}

// OutdatedApp is an installed app with a newer version in its catalog.
type OutdatedApp struct {
	App       string `json:"app"`
	CatalogID string `json:"catalog_id"`
	Current   string `json:"current"`
	Available string `json:"available"`
}

// Outdated lists the apps that have a newer version this machine can install
// (GET /v1/status apps_outdated is its length). It reads the index only: the
// status is read often.
func (e *Engine) Outdated(ctx context.Context) []OutdatedApp {
	apps, err := e.cfg.Store.ListApps(ctx)
	if err != nil || e.cfg.Catalog == nil {
		return nil
	}
	var views []catalogView
	loaded := false
	var out []OutdatedApp
	for _, a := range apps {
		if a.CatalogID == "" {
			continue
		}
		if !loaded {
			views, _ = e.cfg.Catalog.views()
			loaded = true
		}
		if avail, _, _ := e.newerVersion(a, views); avail != "" {
			out = append(out, OutdatedApp{App: a.Name, CatalogID: a.CatalogID, Current: a.CatalogVersion, Available: avail})
		}
	}
	return out
}

// ---- versions ----

// appVersionNewer reports whether version cand is newer than have. The
// numbers are compared piece by piece (1.10.0 is newer than 1.9.0); a
// version with a suffix after "-" (1.2.0-rc.1) is older than the same
// without it; "v" in front and "+build" at the end do not count. Two
// versions that are the same but written differently are not newer.
func appVersionNewer(have, cand string) bool {
	return compareAppVersion(cand, have) > 0
}

func compareAppVersion(a, b string) int {
	ca, pa := splitAppVersion(a)
	cb, pb := splitAppVersion(b)
	n := len(ca)
	if len(cb) > n {
		n = len(cb)
	}
	for i := 0; i < n; i++ {
		x, y := "0", "0"
		if i < len(ca) {
			x = ca[i]
		}
		if i < len(cb) {
			y = cb[i]
		}
		if c := compareVersionPiece(x, y); c != 0 {
			return c
		}
	}
	switch {
	case pa == pb:
		return 0
	case pa == "":
		return 1
	case pb == "":
		return -1
	}
	sa, sb := strings.Split(pa, "."), strings.Split(pb, ".")
	for i := 0; i < len(sa) && i < len(sb); i++ {
		if c := compareVersionPiece(sa[i], sb[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(sa) < len(sb):
		return -1
	case len(sa) > len(sb):
		return 1
	}
	return 0
}

func splitAppVersion(v string) (core []string, pre string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	return strings.Split(v, "."), pre
}

func compareVersionPiece(a, b string) int {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	switch {
	case ea == nil && eb == nil:
		switch {
		case na < nb:
			return -1
		case na > nb:
			return 1
		}
		return 0
	case ea == nil: // a number is older than a word
		return -1
	case eb == nil:
		return 1
	}
	return strings.Compare(a, b)
}

// ---- telling the user a new version is there ----

// notifiedFile is where the versions already told are kept (per app).
func (e *Engine) notifiedFile() string {
	if e.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(e.cfg.DataDir, "upgrade-notified.json")
}

func (e *Engine) readNotified() map[string]string {
	out := map[string]string{}
	if p := e.notifiedFile(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &out)
		}
	}
	return out
}

func (e *Engine) writeNotified(m map[string]string) {
	p := e.notifiedFile()
	if p == "" {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// forgetNotified makes the next look tell the app's new version again (after
// an upgrade or going back).
func (e *Engine) forgetNotified(app string) {
	e.notifyMu.Lock()
	defer e.notifyMu.Unlock()
	m := e.readNotified()
	if _, ok := m[app]; ok {
		delete(m, app)
		e.writeNotified(m)
	}
}

// noteOutdated writes an event of the app (the bell shows it) for each app
// with a newer version that has not been told yet. The same version is told
// once; a newer one is told again. Called after every catalog sync.
func (e *Engine) noteOutdated(ctx context.Context) {
	apps, err := e.cfg.Store.ListApps(ctx)
	if err != nil {
		return
	}
	infos := e.UpgradeInfos(ctx, apps, false)
	e.notifyMu.Lock()
	defer e.notifyMu.Unlock()
	told := e.readNotified()
	changed := false
	for _, a := range apps {
		in := infos[a.Name]
		if in == nil || in.Available == "" || told[a.Name] == in.Available {
			continue
		}
		msg := fmt.Sprintf("应用中心有新版本 %s（现在装的是 %s）：acornfox apps upgrade %s --plan 看会怎么升", in.Available, a.CatalogVersion, a.Name)
		if err := e.cfg.Store.AddEvent(context.WithoutCancel(ctx), state.Event{App: a.Name, DeploymentID: a.CurrentDeployment, Stage: eventStage, Message: msg}); err != nil {
			e.cfg.Log.Warn("upgrade: event", "app", a.Name, "err", err)
			continue
		}
		told[a.Name] = in.Available
		changed = true
	}
	if changed {
		e.writeNotified(told)
	}
}

// appEvent writes a line in the app's history (on the deployment serving
// now, so the deployment's events carry it as well).
func (e *Engine) appEvent(ctx context.Context, app, msg string) {
	dep := ""
	if a, err := e.cfg.Store.GetApp(ctx, app); err == nil {
		dep = a.CurrentDeployment
	}
	if err := e.cfg.Store.AddEvent(context.WithoutCancel(ctx), state.Event{App: app, DeploymentID: dep, Stage: eventStage, Message: msg}); err != nil {
		e.cfg.Log.Warn("upgrade: event", "app", app, "err", err)
	}
}

// ---- the plan (W9-2) ----

// UpgradeRequest is POST /v1/apps/{app}/upgrade.
type UpgradeRequest struct {
	App string `json:"app"`
	// To is the version to upgrade to; "" the catalog's. Only the version
	// the catalog has now can be asked for (the catalog keeps one).
	To string `json:"to,omitempty"`
	// Params gives values for settings the new version adds (and changes
	// the ones the manifest lets change), as `apps set` does.
	Params map[string]any `json:"set,omitempty"`
	// ConfirmRestart: the user has agreed that the app is restarted (and
	// stops for the snapshot). Plans never need it.
	ConfirmRestart bool `json:"confirm_restart,omitempty"`
}

// UpgradePlan is what an upgrade would do (read-only; nothing in it holds a
// secret). A plan that is blocked has CanUpgrade false and says why.
type UpgradePlan struct {
	App  string `json:"app"`
	From string `json:"from"`
	To   string `json:"to"`

	CanUpgrade  bool   `json:"can_upgrade"`
	Blocked     string `json:"blocked,omitempty"`
	BlockedCode string `json:"blocked_code,omitempty"`

	// Restart: the app is restarted (always; its data is kept in the
	// snapshot first).
	Restart bool `json:"restart"`
	// Snapshot: a snapshot is taken first; SnapshotMB is about how big the
	// data is. DiskOK: the disk holds the snapshot and the new image.
	Snapshot   bool `json:"snapshot"`
	SnapshotMB int  `json:"snapshot_mb"`
	ImageMB    int  `json:"image_mb,omitempty"`
	DiskOK     bool `json:"disk_ok"`

	VarsAdded   []string `json:"vars_added"`
	VarsRemoved []string `json:"vars_removed"`
	UsesChanged []string `json:"uses_changed"`
	Notes       []string `json:"notes"`
	// Summary is the one paragraph to tell the user.
	Summary string `json:"summary"`
	// Settings is what changes in the app's own settings (the same plan
	// `apps reapply --plan` gives); nil when the plan is blocked before it
	// could be worked out.
	Settings *ReapplyPlan `json:"settings,omitempty"`
}

// UpgradeStarted is the answer of StartUpgrade.
type UpgradeStarted struct {
	Plan UpgradePlan `json:"plan"`
	Task *TaskView   `json:"task,omitempty"`
}

type upgBlock struct{ code, msg, task string }

// upgradeJob is everything worked out for one upgrade.
type upgradeJob struct {
	req    UpgradeRequest
	app    state.App
	newApp *App
	oldM   *catalog.Manifest
	job    *reapplyJob
	noData bool // the app has no data directory: no snapshot
	plan   UpgradePlan
	blocks []upgBlock
}

func (uj *upgradeJob) block(code, msg string) {
	uj.blocks = append(uj.blocks, upgBlock{code: code, msg: msg})
}

func (uj *upgradeJob) blockFault(f *Fault) {
	uj.blocks = append(uj.blocks, upgBlock{code: f.Code, msg: f.Message, task: f.TaskID})
}

func (uj *upgradeJob) finish() {
	uj.plan.CanUpgrade = len(uj.blocks) == 0
	for i, b := range uj.blocks {
		if i == 0 {
			uj.plan.Blocked, uj.plan.BlockedCode = b.msg, b.code
			continue
		}
		uj.plan.Notes = append(uj.plan.Notes, "还有一处："+b.msg)
	}
}

// UpgradePlan works out what upgrading app (to req.To) would do. Nothing is
// written. What is refused for a reason the user can fix comes back as a
// blocked plan; an error is an app that cannot be asked about at all.
func (e *Engine) UpgradePlan(ctx context.Context, req UpgradeRequest) (UpgradePlan, error) {
	uj, err := e.analyzeUpgrade(ctx, req)
	if err != nil {
		return UpgradePlan{}, err
	}
	return uj.plan, nil
}

// analyzeUpgrade does the whole check; the plan it returns is the plan of
// the job it would start.
func (e *Engine) analyzeUpgrade(ctx context.Context, req UpgradeRequest) (*upgradeJob, error) {
	if e.cfg.Catalog == nil {
		return nil, fault(CodeInternal, nil, "这台服务器没有启用应用中心")
	}
	if !state.ValidAppName(req.App) {
		return nil, fault(CodeNotCatalogApp, nil, "应用名不合法")
	}
	a, err := e.cfg.Store.GetApp(ctx, req.App)
	if errors.Is(err, state.ErrNotFound) {
		return nil, fault(CodeNotCatalogApp, nil, "没有叫 %s 的应用（acornfox apps 看有哪些）", req.App)
	}
	if err != nil {
		return nil, fault(CodeInternal, err, "读应用失败")
	}
	if a.CatalogID == "" {
		return nil, fault(CodeUpgCannot, nil, "%s 不是从应用中心装的，应用中心没有它的新版本可升（自己部署的项目用 acornfox deploy）", req.App)
	}
	from := a.CatalogOrigin
	if from == "" {
		from = PublicSource
	}
	_, ia, err := e.cfg.Catalog.find(from, a.CatalogID)
	if err != nil {
		var f *Fault
		if errors.As(err, &f) && (f.Code == CodeUnknown || f.Code == CodeDisconnected) {
			return nil, fault(CodeUpgCannot, nil, "%s 在目录里已经找不到了（%s），没有新版本可升", req.App, f.Message)
		}
		return nil, err
	}
	if a.CatalogVersion == "" {
		return nil, fault(CodeUpgCannot, nil, "%s 装的是哪个版本没有记录，不能判断有没有新版本", req.App)
	}
	if req.To != "" && req.To != ia.Version {
		return nil, fault(CodeUpgCannot, nil, "目录里 %s 现在只有版本 %s，升不到 %s（目录只留最新的一个版本）", a.CatalogID, ia.Version, req.To)
	}
	if !appVersionNewer(a.CatalogVersion, ia.Version) {
		return nil, fault(CodeUpgCannot, nil, "%s 装的 %s 已经是目录里最新的版本（目录里是 %s），不用升级", req.App, a.CatalogVersion, ia.Version)
	}

	uj := &upgradeJob{req: req, app: a}
	uj.plan = UpgradePlan{App: req.App, From: a.CatalogVersion, To: ia.Version, Restart: true,
		VarsAdded: []string{}, VarsRemoved: []string{}, UsesChanged: []string{}, Notes: []string{}, ImageMB: ia.SizeMB}

	// What stops it without reading the package.
	switch {
	case catalog.FormerIndexApp(ia):
		uj.blockFault(fault(CodeLegacyManifest, nil, "目录里的 %s %s 是旧格式的清单，这一版装不了，要等它按新格式重新上架", ia.ID, ia.Version))
	case e.needsPlatform(ia.MinPlatform) != "":
		uj.blockFault(e.tooOld(ia.ID+" "+ia.Version, e.needsPlatform(ia.MinPlatform)))
	case !e.archSupported(ia.Platforms):
		uj.blockFault(fault(CodeArch, nil, "%s %s 不支持这台机器的 CPU（linux/%s）", ia.ID, ia.Version, e.cfg.Arch))
	}
	if f := e.upgradeBlocked(ctx, a); f != nil {
		uj.blockFault(f)
	}
	oldM, legacy := e.installedWith(ctx, a.Name)
	if oldM == nil && !legacy {
		uj.block(CodeUpgCannot, fmt.Sprintf("%s 装的时候没保存清单（或保存的清单坏了），不能升级：算不出每一项该是什么", a.Name))
	}
	if len(uj.blocks) > 0 {
		return uj.done(), nil
	}
	uj.oldM = oldM

	newApp, err := e.appToInstall(ctx, from, a.CatalogID)
	if err != nil {
		var f *Fault
		if errors.As(err, &f) && blocksUpgrade(f.Code) {
			uj.blockFault(f)
			return uj.done(), nil
		}
		return nil, err
	}
	uj.newApp = newApp
	if err := e.checkManifest(newApp); err != nil {
		f := AsFault(err)
		if !blocksUpgrade(f.Code) {
			return nil, err
		}
		uj.blockFault(f)
		return uj.done(), nil
	}
	uj.compare(a, oldM, newApp.Manifest)
	if len(uj.blocks) == 0 {
		e.analyzeSettings(ctx, uj)
	}
	e.analyzeSnapshot(ctx, uj)
	return uj.done(), nil
}

// done closes the analysis: the plan's verdict and its words.
func (uj *upgradeJob) done() *upgradeJob {
	uj.finish()
	uj.finishText()
	return uj
}

// blocksUpgrade says whether a fault from reading the new package or
// checking its manifest is a reason the upgrade cannot go ahead (a blocked
// plan) as opposed to a failure of the catalog's own reading (an error).
func blocksUpgrade(code string) bool {
	switch code {
	case CodeTooOld, CodeArch, CodeBadManifest, CodeLegacyManifest, CodeUnsupported, CodeRender, CodeDisconnected, CodeNoDisk, CodeBadPlatform:
		return true
	}
	return false
}

// compare finds what differs between the manifest installed (old) and the new
// one, in the ways that decide whether an upgrade in place is possible.
func (uj *upgradeJob) compare(a state.App, old, nw *catalog.Manifest) {
	p := &uj.plan
	// parameters
	oldKeys, newKeys := map[string]catalog.Param{}, map[string]catalog.Param{}
	for _, x := range old.Params {
		oldKeys[x.Key] = x
	}
	for _, x := range nw.Params {
		newKeys[x.Key] = x
	}
	for _, x := range nw.Params {
		if _, had := oldKeys[x.Key]; !had && !x.Hidden {
			p.VarsAdded = append(p.VarsAdded, paramText(x))
		}
	}
	for _, x := range old.Params {
		if _, kept := newKeys[x.Key]; !kept && !x.Hidden {
			p.VarsRemoved = append(p.VarsRemoved, paramText(x))
		}
	}
	// the data directories: nothing is made for a new one
	haveVol := map[string]bool{}
	for _, v := range old.Volumes {
		haveVol[v.Path] = true
	}
	for _, v := range nw.Volumes {
		if !haveVol[v.Path] {
			uj.block(CodeUpgChanged, fmt.Sprintf("新版本 %s 多了数据目录 %s，现在装的应用里没有它，这一版不能原地升级（要保留数据删掉重装：acornfox install %s --reuse-data）", p.To, v.Path, a.CatalogID))
		}
	}
	// what the app provides to others
	if kinds := func(m *catalog.Manifest) string {
		var k []string
		for _, pv := range m.Provides {
			k = append(k, pv.Kind)
		}
		return strings.Join(k, ",")
	}; kinds(old) != kinds(nw) {
		uj.block(CodeUpgChanged, fmt.Sprintf("新版本 %s 提供的服务（%s）和现在的（%s）不一样，别的应用接着它用的服务会对不上，这一版不能原地升级", p.To, textOr1(kinds(nw), "没有"), textOr1(kinds(old), "没有")))
	}
	// the services the app needs
	oldUse := map[string]catalog.Use{}
	for _, u := range old.Uses {
		oldUse[u.Name] = u
	}
	newUse := map[string]bool{}
	for _, u := range nw.Uses {
		newUse[u.Name] = true
		o, had := oldUse[u.Name]
		switch {
		case !had && !u.Optional:
			uj.block(CodeUpgChanged, fmt.Sprintf("新版本 %s 多需要一个服务“%s”（类型 %s）：已经装好的应用不能补接服务，平台也不替你去装别的应用；要用新版本只能保留数据删掉重装（先登记好这个服务）", p.To, u.Name, u.Kind))
		case !had:
			p.UsesChanged = append(p.UsesChanged, fmt.Sprintf("多了可选的服务“%s”（%s）：这次不会接上", u.Name, u.Kind))
		case o.Kind != u.Kind:
			uj.block(CodeUpgChanged, fmt.Sprintf("新版本 %s 里需要“%s”的服务类型从 %s 变成了 %s，现在接的服务对不上，这一版不能原地升级", p.To, u.Name, o.Kind, u.Kind))
		case o.Optional && !u.Optional:
			// still there? the settings check below finds a missing one
		}
	}
	for _, u := range old.Uses {
		if !newUse[u.Name] {
			p.UsesChanged = append(p.UsesChanged, fmt.Sprintf("不再需要服务“%s”（%s）：登记表里的连接留着，用不上了可以以后去掉", u.Name, u.Kind))
		}
	}
	// the app's own record
	if internalManifest(old) != internalManifest(nw) {
		uj.block(CodeUpgChanged, fmt.Sprintf("新版本 %s 改了应用的访问方式（内部应用或不是），这一版不能原地升级", p.To))
	}
	if need := nw.Resources.MemoryMB.Min; need > 0 && need > a.MemoryMB {
		uj.block(CodeUpgNeed, fmt.Sprintf("新版本 %s 至少要 %d MB 内存，现在给 %s 的是 %d MB：问用户同意后先调大（acornfox app set --app %s --memory %d），再升级", p.To, need, a.Name, a.MemoryMB, a.Name, need))
	}
	if ep, was := primaryEndpoint(nw), primaryEndpoint(old); ep != nil && was != nil && ep.Port != was.Port {
		p.Notes = append(p.Notes, fmt.Sprintf("新版本里应用监听的端口从 %d 改成了 %d（对外的访问端口不变）", was.Port, ep.Port))
	}
}

func textOr1(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func paramText(x catalog.Param) string {
	if x.Label != "" {
		return x.Key + "（" + x.Label + "）"
	}
	return x.Key
}

// analyzeSettings works out what the new manifest does to the app's own
// settings (the reapply machinery with the new manifest).
func (e *Engine) analyzeSettings(ctx context.Context, uj *upgradeJob) {
	rj, err := e.reapplyAnalyze(ctx, ReapplyRequest{App: uj.app.Name, Params: uj.req.Params, Why: WhyUpgrade,
		Upgrade: &ReapplyUpgrade{App: uj.newApp}})
	if err != nil {
		f := AsFault(err)
		code := f.Code
		if code == CodeBadParams || code == CodeApplyEdit {
			code = CodeUpgNeed
		}
		uj.block(code, f.Message)
		return
	}
	uj.job = rj
	pl := rj.plan(ctx, e)
	uj.plan.Settings = &pl
	uj.plan.Notes = append(uj.plan.Notes, pl.Notes...)
	if len(pl.Dependents) > 0 {
		uj.plan.Notes = append(uj.plan.Notes, "用 "+uj.app.Name+" 提供的服务的应用（"+strings.Join(pl.Dependents, "、")+"）的设置会落在后面，升完用 acornfox apps affected 看、再一个个重新生效")
	}
}

// analyzeSnapshot works out the snapshot and the disk.
func (e *Engine) analyzeSnapshot(ctx context.Context, uj *upgradeJob) {
	p := &uj.plan
	p.DiskOK = true
	snaps := e.snapshots()
	if snaps == nil {
		uj.block(appbackup.CodeUnsupported, "这台机器不能做升级前的快照，所以不能升级应用")
		p.DiskOK = false
		return
	}
	bp, err := snaps.PlanBackup(ctx, uj.app.Name, appbackup.BackupOptions{Kind: appbackup.KindPreUpgrade})
	var be *appbackup.Error
	switch {
	case errors.As(err, &be) && be.Code == appbackup.CodeNoData:
		uj.noData = true
		p.Notes = append(p.Notes, "这个应用没有存数据的目录，升级前不留快照：升级出问题时程序和设置照样自动退回，但升级以后不能再退回到升级前")
	case err != nil:
		code, msg := appbackup.CodeExport, err.Error()
		if be != nil {
			code, msg = be.Code, be.Message
		}
		uj.block(code, "升级前的快照做不了："+msg)
		p.DiskOK = false
	default:
		p.Snapshot = true
		var known int64
		for _, v := range bp.Volumes {
			if v.Bytes > 0 {
				known += v.Bytes
			}
		}
		p.SnapshotMB = int((known + (1 << 20) - 1) >> 20)
		if !bp.Enough {
			p.DiskOK = false
			uj.block(appbackup.CodeDisk, fmt.Sprintf("磁盘空间不够，不能升级：升级前要把数据存一份快照，需要约 %s，备份目录所在的磁盘只剩 %s。没有动应用", sizeText(int64(bp.NeedBytes)), sizeText(int64(bp.FreeBytes))))
		}
	}
	// the image
	need := uj.plan.ImageMB * 2
	archive := int64(0)
	if e.cfg.Gateway != nil && uj.newApp != nil {
		for _, img := range uj.newApp.Manifest.Release.Images {
			if art, ok := img.ArtifactFor(e.cfg.Arch); ok {
				archive += art.Size
			}
		}
	}
	if err := e.checkDisk(ctx, need, archive); err != nil {
		p.DiskOK = false
		f := AsFault(err)
		uj.block(f.Code, f.Message)
	}
}

func sizeText(b int64) string {
	if b >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
	}
	return fmt.Sprintf("%d MB", (b+(1<<20)-1)>>20)
}

// finishText writes the summary.
func (uj *upgradeJob) finishText() {
	p := &uj.plan
	if !p.CanUpgrade {
		p.Summary = fmt.Sprintf("%s 现在不能从 %s 升到 %s：%s。什么都没动", p.App, p.From, p.To, p.Blocked)
		return
	}
	s := fmt.Sprintf("把 %s 从 %s 升到 %s", p.App, p.From, p.To)
	if p.Snapshot {
		s += fmt.Sprintf("：先把它的数据（约 %d MB）存一份快照在本机，打包时应用会暂停一阵；再换上新版本重启，健康检查通过才算成功", p.SnapshotMB)
	} else {
		s += "：它没有存数据的目录，不留快照；换上新版本重启，健康检查通过才算成功"
	}
	s += "。中间哪一步没成功都会自动退回到升级前"
	if p.Snapshot {
		s += "；升级成功后快照还留着，出问题可以退回到升级前（升级后写进应用的新数据会没有）"
	}
	p.Summary = s
}

// ---- running an upgrade (W9-2, W9-3) ----

// upgradeStepLabels are the steps of an upgrade task, in order.
var upgradeStepLabels = []struct{ name, label string }{
	{"check", "检查"},
	{"image", "下载新版本"},
	{"snapshot", "留升级前快照"},
	{"files", "写设置"},
	{"start", "换上新版本"},
	{"health", "健康检查"},
	{"init", "应用里的步骤"},
	{"save", "记下新版本"},
	{"revert", "退回"},
}

// StartUpgrade checks the request (UpgradePlan) and, when nothing stops it
// and the user has agreed (ConfirmRestart), starts the task that does it
// (202). A refusal comes back as an error with the plan next to it.
func (e *Engine) StartUpgrade(ctx context.Context, req UpgradeRequest) (UpgradeStarted, error) {
	if e.cfg.Catalog == nil {
		return UpgradeStarted{}, fault(CodeInternal, nil, "这台服务器没有启用应用中心")
	}
	if _, ok := e.cfg.Runner.(runner.CatalogAPI); !ok {
		return UpgradeStarted{}, fault(CodeInternal, nil, "运行器不支持从目录安装，请升级 AcornFox")
	}
	if err := e.settleUndo(ctx, req.App); err != nil {
		return UpgradeStarted{}, err
	}
	uj, err := e.analyzeUpgrade(ctx, req)
	if err != nil {
		return UpgradeStarted{}, err
	}
	if !uj.plan.CanUpgrade {
		f := fault(uj.plan.BlockedCode, nil, "%s", uj.plan.Blocked)
		f.TaskID = uj.blocks[0].task
		return UpgradeStarted{Plan: uj.plan}, f
	}
	if !req.ConfirmRestart {
		f := fault(CodeUpgConfirm, nil, "%s。要用户同意才做：同意后加上 --yes（接口里 confirm_restart: true）再来；什么都还没动", uj.plan.Summary)
		return UpgradeStarted{Plan: uj.plan}, f
	}
	id := newID()
	e.mu.Lock()
	if other, busy := e.running[req.App]; busy {
		e.mu.Unlock()
		f := fault(CodeUpgCannot, nil, "%s 正在安装、重新生效或升级（任务 %s），等它结束再来", req.App, other)
		f.TaskID = other
		return UpgradeStarted{}, f
	}
	e.running[req.App] = id
	e.mine[id] = true
	e.mu.Unlock()

	tr := &taskRun{e: e, id: id, view: TaskView{ID: id, Kind: KindUpgrade, Target: req.App, Catalog: PublicSource, Status: state.TaskRunning}}
	if uj.app.CatalogOrigin != "" {
		tr.view.Catalog = uj.app.CatalogOrigin
	}
	tr.view.Apps = []AppResult{{App: req.App, CatalogID: uj.app.CatalogID, Version: uj.plan.To, From: uj.plan.From}}
	for _, s := range upgradeStepLabels {
		tr.view.Steps = append(tr.view.Steps, StepView{App: req.App, Name: s.name, Label: s.label, Status: StepPending})
	}
	if err := e.recordOpTask(ctx, tr); err != nil {
		e.mu.Lock()
		delete(e.running, req.App)
		e.mu.Unlock()
		return UpgradeStarted{}, err
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			e.mu.Lock()
			delete(e.running, req.App)
			e.mu.Unlock()
		}()
		tctx, cancel := taskContext(e.base, installTaskLimit)
		defer cancel()
		tr.upgrade(tctx, uj)
	}()
	v := tr.snapshot()
	return UpgradeStarted{Plan: uj.plan, Task: &v}, nil
}

// recordOpTask writes the record of a new upgrade or rollback task. The
// table of tasks only knows the kinds app and recipe (a CHECK of the first
// migration): the record says "app" and its data says what it really is.
func (e *Engine) recordOpTask(ctx context.Context, tr *taskRun) error {
	data, _ := json.Marshal(tr.view)
	if _, err := e.cfg.Store.CreateInstallTask(ctx, state.InstallTask{ID: tr.id, Kind: "app", Target: tr.view.Target, Status: state.TaskRunning, Data: string(data)}); err != nil {
		return fault(CodeInternal, err, "记录任务失败")
	}
	return nil
}

// upgrade runs the upgrade of uj as the task t.
func (t *taskRun) upgrade(ctx context.Context, uj *upgradeJob) {
	e, job := t.e, uj.job
	app := uj.app.Name
	up := job.req.Upgrade
	cat, _ := e.cfg.Runner.(runner.CatalogAPI)

	t.step(ctx, app, "check", StepDone, uj.plan.Summary)

	// 1. the new image, while the app goes on running.
	t.step(ctx, app, "image", StepRunning, "")
	id, msg, err := t.image(ctx, &planned{app: up.App, name: app}, cat)
	if err != nil {
		t.finish(ctx, t.failStep(ctx, app, "image", AsFault(err)))
		return
	}
	up.ImageID = id
	t.step(ctx, app, "image", StepDone, msg)

	// 2. the snapshot: nothing may be changed without it.
	switch snaps := e.snapshots(); {
	case uj.noData:
		t.step(ctx, app, "snapshot", StepSkipped, "这个应用没有存数据的目录，不留快照")
	case snaps == nil:
		t.finish(ctx, t.failStep(ctx, app, "snapshot", fault(appbackup.CodeUnsupported, nil, "这台机器不能做升级前的快照，没有动应用")))
		return
	default:
		t.step(ctx, app, "snapshot", StepRunning, "")
		resume := pauseClock(ctx) // the copy of a big data directory is not the task's time
		meta, serr := snaps.SnapshotPreUpgrade(ctx, app, fmt.Sprintf("升级到 %s 之前自动留的快照", uj.plan.To))
		resume()
		if serr != nil {
			f := backupFault(serr, "没留成升级前的快照，没有动应用")
			t.finish(ctx, t.failStep(ctx, app, "snapshot", f))
			return
		}
		up.SnapshotID = meta.ID
		t.setApp(app, func(r *AppResult) { r.SnapshotID = meta.ID })
		t.step(ctx, app, "snapshot", StepDone, fmt.Sprintf("快照 %s（%s）", meta.ID, sizeText(meta.Bytes)))
	}

	// 3. the journal, then the apply.
	u, err := job.journal(ctx, e, t.id)
	if err != nil {
		t.finish(ctx, t.upgradeCleanFail(ctx, app, AsFault(err), up))
		return
	}
	if err := e.saveUndo(ctx, app, u); err != nil {
		t.finish(ctx, t.upgradeCleanFail(ctx, app, fault(CodeInternal, err, "写升级的记录失败"), up))
		return
	}
	if f := t.reapplyBody(ctx, job, u); f != nil {
		t.finish(ctx, f)
		return
	}
	t.step(ctx, app, "revert", StepSkipped, "没出错，不用退回")
	e.forgetNotified(app)
	e.appEvent(ctx, app, fmt.Sprintf("已升级到 %s（原来是 %s）；升级前的快照还留着，出问题可以 acornfox apps rollback %s", uj.plan.To, uj.plan.From, app))
	t.finish(ctx, nil)
}

// upgradeCleanFail ends an upgrade that failed before the app was touched
// (after the snapshot, which is left: it is the app's as it is).
func (t *taskRun) upgradeCleanFail(ctx context.Context, app string, f *Fault, up *ReapplyUpgrade) *Fault {
	t.step(ctx, app, "files", StepFailed, f.Code+" "+f.Message)
	return fault(CodeUpgFailed, nil, "升级没成功，应用没有被改动：%s %s", f.Code, f.Message)
}

// backupFault is an appbackup error as a *Fault with its own code and hint.
func backupFault(err error, lead string) *Fault {
	var be *appbackup.Error
	if errors.As(err, &be) {
		return &Fault{Code: be.Code, Message: lead + "：" + be.Message, Hint: be.Hint, err: err}
	}
	return fault(CodeInternal, err, "%s：%v", lead, err)
}

// undoUpgrade puts the app back as it was before the upgrade and returns the
// fault the task ends with: APP-UPG-05 when it worked (the cause's code is
// in the message), APP-UPG-06 when it did not. depID is the deployment the
// upgrade made ("" none yet: nothing of the new version ever ran, so the
// data is untouched and the settings are enough).
func (t *taskRun) undoUpgrade(ctx context.Context, job *reapplyJob, u *undoRecord, depID string, cause *Fault) *Fault {
	e, app := t.e, job.app.Name
	ctx = context.WithoutCancel(ctx)
	t.step(ctx, app, "revert", StepRunning, "")
	from := job.oldM.Release.Version
	var notes []string
	if depID != "" && u.Snapshot != "" {
		if snaps := e.snapshots(); snaps != nil {
			resume := pauseClock(ctx)
			_, err := snaps.RestoreSnapshot(ctx, app, u.Snapshot, appbackup.RestoreOptions{})
			resume()
			if err == nil {
				_ = e.saveUndo(ctx, app, nil)
				t.step(ctx, app, "revert", StepDone, "已用升级前的快照退回：数据、设置和程序版本都是升级前的")
				t.setApp(app, func(r *AppResult) { r.Version = from })
				e.appEvent(ctx, app, fmt.Sprintf("升级没成功，已退回到升级前的 %s：%s %s", from, cause.Code, cause.Message))
				f := fault(CodeUpgFailed, nil, "升级没成功，已经退回到升级前的 %s（数据和设置都是升级前的）：%s %s", from, cause.Code, cause.Message)
				f.Diagnosis = cause.Diagnosis
				return f
			}
			notes = append(notes, "用快照退回没成功："+err.Error())
		}
	}
	// No snapshot to go back to, or nothing of the new version ran: the
	// settings, the files and the container are put back one by one.
	if err := e.putBack(ctx, app, job.m, job.oldM, u, depID, true, job); err != nil {
		notes = append(notes, "退回设置和程序也出了错："+err.Error())
		f := fault(CodeUpgUndone, err, "升级没成功（%s %s），退回到升级前时又出了错：%s。应用可能停在新旧混在一起的状态", cause.Code, cause.Message, strings.Join(notes, "；"))
		f.Diagnosis = cause.Diagnosis
		t.step(ctx, app, "revert", StepFailed, f.Code+" "+strings.Join(notes, "；"))
		return f
	}
	if len(notes) > 0 {
		// The program and settings are back, the data could not be: the new
		// version may have changed it.
		f := fault(CodeUpgUndone, nil, "升级没成功（%s %s），应用的程序和设置已退回，但数据没能用快照退回，新版本可能已经改过它：%s", cause.Code, cause.Message, strings.Join(notes, "；"))
		f.Diagnosis = cause.Diagnosis
		t.step(ctx, app, "revert", StepFailed, f.Code+" "+strings.Join(notes, "；"))
		return f
	}
	t.step(ctx, app, "revert", StepDone, "已退回升级前的设置和程序，应用照原来的在运行")
	t.setApp(app, func(r *AppResult) { r.Version = from })
	e.appEvent(ctx, app, fmt.Sprintf("升级没成功，已退回到升级前的 %s：%s %s", from, cause.Code, cause.Message))
	f := fault(CodeUpgFailed, nil, "升级没成功，已经退回到升级前的 %s：%s %s", from, cause.Code, cause.Message)
	f.Diagnosis = cause.Diagnosis
	return f
}

// ---- going back to before the upgrade (W9-3) ----

// RollbackRequest is POST /v1/apps/{app}/rollback.
type RollbackRequest struct {
	App string `json:"app"`
	// Confirm: the user has agreed that what was written after the upgrade
	// is lost. Plans never need it.
	Confirm bool `json:"confirm,omitempty"`
}

// RollbackPlan is what going back would do (read-only).
type RollbackPlan struct {
	App  string `json:"app"`
	From string `json:"from"` // the version running now
	To   string `json:"to"`   // the version of the snapshot

	CanRollback bool   `json:"can_rollback"`
	Blocked     string `json:"blocked,omitempty"`
	BlockedCode string `json:"blocked_code,omitempty"`

	SnapshotID string    `json:"snapshot_id,omitempty"`
	SnapshotAt time.Time `json:"snapshot_at,omitempty"`
	// LosesData: what was written after the snapshot is lost (the data of
	// the moment is kept first as a pre-restore backup). Restart: the app
	// is restarted.
	LosesData bool     `json:"loses_data"`
	Restart   bool     `json:"restart"`
	DiskOK    bool     `json:"disk_ok"`
	Notes     []string `json:"notes"`
	Summary   string   `json:"summary"`
}

// RollbackStarted is the answer of StartRollback.
type RollbackStarted struct {
	Plan RollbackPlan `json:"plan"`
	Task *TaskView    `json:"task,omitempty"`
}

// PlanRollback says whether and how app goes back to its pre-upgrade
// snapshot. Nothing is touched.
func (e *Engine) PlanRollback(ctx context.Context, app string) (RollbackPlan, error) {
	if !state.ValidAppName(app) {
		return RollbackPlan{}, fault(CodeNotCatalogApp, nil, "应用名不合法")
	}
	a, err := e.cfg.Store.GetApp(ctx, app)
	if errors.Is(err, state.ErrNotFound) {
		return RollbackPlan{}, fault(CodeNotCatalogApp, nil, "没有叫 %s 的应用（acornfox apps 看有哪些）", app)
	}
	if err != nil {
		return RollbackPlan{}, fault(CodeInternal, err, "读应用失败")
	}
	if a.CatalogID == "" {
		return RollbackPlan{}, fault(CodeRollbackCannot, nil, "%s 不是从应用中心装的，没有升级前的快照可退（自己部署的项目用 acornfox rollback）", app)
	}
	p := RollbackPlan{App: app, From: a.CatalogVersion, Restart: true, LosesData: true, DiskOK: true, Notes: []string{}}
	block := func(code, msg string) (RollbackPlan, error) {
		p.Blocked, p.BlockedCode = msg, code
		p.Summary = fmt.Sprintf("%s 现在不能退回到升级前：%s。什么都没动", app, msg)
		return p, nil
	}
	snaps := e.snapshots()
	if snaps == nil {
		return block(appbackup.CodeUnsupported, "这台机器不能做应用数据的备份和恢复")
	}
	meta, ok, err := snaps.Latest(app, appbackup.KindPreUpgrade)
	switch {
	case err != nil:
		return RollbackPlan{}, fault(CodeInternal, err, "读升级前的快照失败")
	case !ok:
		return block(CodeRollbackCannot, "没有升级前的快照（这个应用还没升级过，或快照被删掉了）")
	}
	p.To, p.SnapshotID, p.SnapshotAt = meta.Version.CatalogVersion, meta.ID, meta.CreatedAt
	if !snapshotIsOlder(a, meta) {
		return block(CodeRollbackCannot, fmt.Sprintf("升级前的快照就是现在这个版本（%s）的，没有更早的可以退回", a.CatalogVersion))
	}
	if f := e.busyFault(app); f != nil {
		return block(f.Code, f.Message)
	}
	rp, err := snaps.PlanRestore(ctx, app, meta.ID, true)
	if err != nil {
		code, msg := CodeRollbackCannot, err.Error()
		var be *appbackup.Error
		if errors.As(err, &be) {
			code, msg = be.Code, be.Message
		}
		return block(code, msg)
	}
	switch {
	case !rp.Intact:
		return block(appbackup.CodeCorrupt, fmt.Sprintf("快照 %s 的文件不齐或对不上，不能用来退回", meta.ID))
	case !rp.VersionAvailable:
		return block(appbackup.CodeVersionGone, fmt.Sprintf("升级前那个版本（%s）的程序已经不在这台机器上了，不能退回", p.To))
	case !rp.Enough:
		p.DiskOK = false
		return block(appbackup.CodeDisk, "磁盘空间不够：退回前要先把现在的数据另存一份，再换回快照里的数据")
	}
	p.CanRollback = true
	p.Summary = fmt.Sprintf("把 %s 从 %s 退回到升级前的 %s：用 %s 留的快照里的数据和设置覆盖现在的——升级之后写进应用的新数据会没有（退回前会先把现在的数据另存一份，需要时能找回）；应用会重启一次，中间有一段时间用不了；没成功会自动回到退回前的样子",
		app, p.From, p.To, meta.CreatedAt.Local().Format("2006-01-02 15:04"))
	return p, nil
}

var rollbackStepLabels = []struct{ name, label string }{
	{"check", "检查"},
	{"restore", "换回升级前的数据和版本"},
	{"save", "记下"},
}

// StartRollback checks the request and, when the user agreed (Confirm),
// starts the task that goes back (202).
func (e *Engine) StartRollback(ctx context.Context, req RollbackRequest) (RollbackStarted, error) {
	if err := e.settleUndo(ctx, req.App); err != nil {
		return RollbackStarted{}, err
	}
	p, err := e.PlanRollback(ctx, req.App)
	if err != nil {
		return RollbackStarted{}, err
	}
	if !p.CanRollback {
		f := fault(p.BlockedCode, nil, "%s", p.Blocked)
		return RollbackStarted{Plan: p}, f
	}
	if !req.Confirm {
		f := fault(CodeRollbackConfirm, nil, "%s。要用户同意才做：同意后加上 --yes（接口里 confirm: true）再来；什么都还没动", p.Summary)
		return RollbackStarted{Plan: p}, f
	}
	id := newID()
	e.mu.Lock()
	if other, busy := e.running[req.App]; busy {
		e.mu.Unlock()
		f := fault(CodeRollbackCannot, nil, "%s 正在安装、重新生效或升级（任务 %s），等它结束再来", req.App, other)
		f.TaskID = other
		return RollbackStarted{}, f
	}
	e.running[req.App] = id
	e.mine[id] = true
	e.mu.Unlock()
	a, _ := e.cfg.Store.GetApp(ctx, req.App)
	tr := &taskRun{e: e, id: id, view: TaskView{ID: id, Kind: KindRollback, Target: req.App, Catalog: PublicSource, Status: state.TaskRunning}}
	if a.CatalogOrigin != "" {
		tr.view.Catalog = a.CatalogOrigin
	}
	tr.view.Apps = []AppResult{{App: req.App, CatalogID: a.CatalogID, Version: p.To, From: p.From, SnapshotID: p.SnapshotID}}
	for _, s := range rollbackStepLabels {
		tr.view.Steps = append(tr.view.Steps, StepView{App: req.App, Name: s.name, Label: s.label, Status: StepPending})
	}
	if err := e.recordOpTask(ctx, tr); err != nil {
		e.mu.Lock()
		delete(e.running, req.App)
		e.mu.Unlock()
		return RollbackStarted{}, err
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			e.mu.Lock()
			delete(e.running, req.App)
			e.mu.Unlock()
		}()
		tr.rollback(e.base, p)
	}()
	v := tr.snapshot()
	return RollbackStarted{Plan: p, Task: &v}, nil
}

// rollback runs the rollback of plan p as the task t.
func (t *taskRun) rollback(ctx context.Context, p RollbackPlan) {
	e, app := t.e, p.App
	t.step(ctx, app, "check", StepDone, p.Summary)
	t.step(ctx, app, "restore", StepRunning, "")
	res, err := e.snapshots().RestoreSnapshot(ctx, app, p.SnapshotID, appbackup.RestoreOptions{})
	if err != nil {
		var be *appbackup.Error
		code, msg := CodeRollbackFailed, err.Error()
		if errors.As(err, &be) {
			msg = be.Code + " " + be.Message
			if be.Code == appbackup.CodeRollbackFailed {
				code = CodeRollbackUndone
			}
		}
		var f *Fault
		if code == CodeRollbackUndone {
			f = fault(code, err, "退回没成功，回到退回前的样子时也出了错：%s。退回前另存的数据在备份 %s 里", msg, res.PreRestoreID)
		} else {
			f = fault(code, err, "退回没成功，应用保持退回前的样子：%s", msg)
		}
		t.step(ctx, app, "restore", StepFailed, f.Code+" "+msg)
		t.finish(ctx, f)
		return
	}
	detail := "已换回升级前的数据和版本"
	if res.PreRestoreID != "" {
		detail += "；退回前的数据另存在备份 " + res.PreRestoreID
	}
	t.step(ctx, app, "restore", StepDone, detail)
	t.step(ctx, app, "save", StepRunning, "")
	t.setApp(app, func(r *AppResult) {
		r.Version = p.To
		if res.PreRestoreID != "" {
			r.Notes = append(r.Notes, "退回前的数据另存在备份 "+res.PreRestoreID+"（acornfox backup list "+app+"），要找回升级后写的新数据可以恢复它")
		}
		for _, w := range res.Warnings {
			r.Notes = append(r.Notes, w)
		}
	})
	e.forgetNotified(app)
	e.appEvent(ctx, app, fmt.Sprintf("已退回到升级前的 %s（原来是 %s）", p.To, p.From))
	t.step(ctx, app, "save", StepDone, "")
	// the version offered again, as after any new version
	e.noteOutdated(ctx)
	t.finish(ctx, nil)
}

// recoverRollback ends a rollback task the server stopped in the middle of:
// the restore puts itself back at the next start of the backups (BKP-10),
// so the task only says what is known.
func (e *Engine) recoverRollback(ctx context.Context, t state.InstallTask, v TaskView) {
	tr := &taskRun{e: e, id: t.ID, view: v}
	tr.finish(ctx, fault(CodeRollbackFailed, nil, "退回途中服务重启了：应用有没有退回要看 acornfox backup list %s 和 acornfox status %s；没做完的恢复，启动时已经按备份的规则处理（acornfox doctor 看）", t.Target, t.Target))
}
