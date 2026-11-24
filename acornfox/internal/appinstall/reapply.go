package appinstall

// Changing an installed app's settings and applying them again ("重新生效").
//
// What it does: given an app installed from the app center, work out every
// value it should have now (its own parameters with the changes asked for,
// the platform variables as the platform gives them now, the items of the
// services it is plugged into as the registry has them now), render the
// manifest again with them (environment variables, the start command, the
// config files that are not "install only"), and, when something changed,
// write what changed, recreate the app's container from the same image
// (the data volumes are not touched), wait for it to be healthy and run the
// init steps that are due. If anything fails after the first write, every
// write is undone and the app's container is recreated as it was.
//
// Who uses it: `acornfox apps set|reapply|affected`, the console's pages,
// the API (PATCH /v1/apps/{app}/vars, POST /v1/apps/{app}/reapply, GET
// /v1/platform/affected), and the places that change what an app's values
// depend on (an app's exposure or domain; a service's values). The upgrade
// of an app (a new manifest with the values it has) uses ReapplyRequest.
// Upgrade.
//
// Rules (all in code, none left to whoever calls):
//
//   - A parameter is checked exactly as at install (resolveOneParam), and
//     only one the manifest lets change (editable) can be changed.
//   - Values that were not asked to change keep what the app has: generated
//     passwords and keys are never made again.
//   - The init steps of a manifest run at one of three moments (catalog.
//     Step.When): after_healthy (install only, never here), on_reapply (every
//     time an apply is run) and on_change (when something it watches
//     changed). Steps must be written so that running them again is safe.
//   - A config file written with overwrite: install or never is not touched.
//   - Restarting the app is never done unless something it reads changed (or
//     force is asked), and the callers that change the app's address ask the
//     user first (APP-APPLY-05).
//   - Failure puts back: environment, command, config files, the container
//     (APP-APPLY-03); when putting back fails too it says so (APP-APPLY-04).
//
// Side effects: writes the app's environment variables, start command,
// config files in its data volume, its saved parameters; creates a
// deployment (a new container from the same image, so a new version number
// appears in the app's history, reason "reapply"); runs commands in the app
// or sends requests to it for the init steps. Never touches data volumes
// except the config files named above, never deletes anything.
//
// Verify: `acornfox apps reapply APP --plan` (read-only) shows what would
// change; after an apply `acornfox apps vars APP` shows the values and no
// APP-VARS-01; `acornfox install status TASK` shows the steps.
// Stop / roll back: an apply that fails rolls itself back; an apply that
// succeeded is undone by applying the former values (acornfox apps set APP
// name=value). A server that stops in the middle puts back at its next start
// (recoverReapply, from the journal kept with the app's saved parameters).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/acornfox/acornfox/internal/appbackup"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// KindReapply is the Kind of an install task that applies settings again.
const KindReapply = "reapply"

// Why an apply is run (ReapplyRequest.Why, recorded in the task).
const (
	WhySet      = "set"      // the user changed settings
	WhyReapply  = "reapply"  // the user asked to apply again
	WhyAddress  = "address"  // the app's exposure or domain changed
	WhyPlatform = "platform" // a platform variable changed
	WhyService  = "service"  // a service the app uses changed
	WhyUpgrade  = "upgrade"  // a new version of the app
)

const (
	// usesFPKey is where the saved parameters keep, for each item of each
	// service the app is plugged into, a fingerprint of the value the app
	// was last given (never the value: the binding is the one place a
	// credential is kept). It lets "this service changed" be told later.
	usesFPKey = "@svc_fp"
	// undoKey is the journal of an apply under way.
	undoKey = "@undo"
)

// ReapplyRequest is what to apply to one app.
type ReapplyRequest struct {
	App string `json:"app"`
	// Params are parameters to change ("model" or "params.model": the new
	// value, as text, a number or a boolean).
	Params map[string]any `json:"params,omitempty"`
	// Platform sets platform variables for this app only (user_can_set
	// ones, e.g. {"tz": "Asia/Tokyo"}; names with or without "platform.").
	Platform map[string]string `json:"platform,omitempty"`
	// ResetPlatform drops the app's own setting of these platform variables:
	// it follows the machine again.
	ResetPlatform []string `json:"reset_platform,omitempty"`
	// ConfirmRestart: the user has agreed that the app is restarted. Without
	// it an apply that restarts the app is refused (APP-APPLY-05) and
	// changes nothing: the confirmation is a rule of the code, not of
	// whoever asks. Plans never need it.
	ConfirmRestart bool `json:"confirm_restart,omitempty"`
	// Force applies even when nothing changed: the app is restarted and
	// the on_reapply steps run.
	Force bool `json:"force,omitempty"`
	// Why says what started it (Why*); recorded in the task.
	Why string `json:"why,omitempty"`
	// AssumeWhere (plan only, set by the server): the exposure and domains
	// the app will have once the change being asked about is made.
	AssumeWhere *platformvar.Where `json:"-"`
	// Upgrade (Go callers only): apply a new version of the app: its new
	// manifest, rendered with the values the app has (parameters carried by
	// name, new ones from their default or generated, gone ones dropped),
	// with its new image. The caller has the image on this machine and
	// leaves the catalog version and saved manifest to this function.
	Upgrade *ReapplyUpgrade `json:"-"`
}

// ReapplyUpgrade is the new version an apply puts the app on.
type ReapplyUpgrade struct {
	App *App // the new package (manifest and its files)
	// ImageID is the new image's ID on this machine. A plan does not need
	// it; the upgrade task fills it in when it has the image (fetched
	// before the app is touched, taken again under the app's lock).
	ImageID string
	// SnapshotID is the pre-upgrade snapshot the app was given (upgrade
	// task only): a failure after the new container was made goes back to
	// it. "" none (the app has no data to keep).
	SnapshotID string
}

// ReapplyVar is one value that is changing.
type ReapplyVar struct {
	// Name is params.<key>, platform.<name> or uses.<need>.<item>.
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	// Old is what the app has now ("" when unknown), New what it gets;
	// secrets are shown as Masked and the last four characters.
	Old    string `json:"old"`
	New    string `json:"new"`
	Secret bool   `json:"secret,omitempty"`
	// From says where the change comes from: user (asked for now), platform
	// (the machine or the app's address changed), service (a service the
	// app uses changed), upgrade.
	From string `json:"from"`
}

// ReapplyStepPlan is an init step group that will run.
type ReapplyStepPlan struct {
	When  string   `json:"when"`            // on_reapply | on_change
	Names []string `json:"names"`           // the steps, in order
	Watch []string `json:"watch,omitempty"` // on_change: what it watches
}

// ReapplyPlan is what an apply would do (read-only; POST …/reapply with
// plan true). Nothing in it holds a secret.
type ReapplyPlan struct {
	App string `json:"app"`
	// Needed: anything at all would change (or force was asked).
	Needed bool `json:"needed"`
	// Restart: the app's container is recreated (a few seconds to a few
	// minutes of interruption; its data is not touched).
	Restart bool         `json:"restart"`
	Vars    []ReapplyVar `json:"vars"`
	// Env are the environment variables that are written (names only), Gone
	// the ones the manifest no longer writes now, Command whether the start
	// command changes, Files the config files that are written again.
	Env     []string          `json:"env"`
	Gone    []string          `json:"env_removed"`
	Command bool              `json:"command"`
	Files   []string          `json:"files"`
	Steps   []ReapplyStepPlan `json:"steps"`
	Notes   []string          `json:"notes"`
	// Summary is the one sentence to tell the user.
	Summary string `json:"summary"`
	// Dependents are apps whose settings fall behind once this one is
	// applied (they use a service this app provides and its values
	// change); they are told, not changed.
	Dependents []string `json:"dependents,omitempty"`
}

// ReapplyStarted is the answer of StartReapply: the plan and, when
// something is to be done, the task that does it.
type ReapplyStarted struct {
	Plan ReapplyPlan `json:"plan"`
	Task *TaskView   `json:"task,omitempty"`
}

// reapplyFile is a config file to write again.
type reapplyFile struct {
	file   catalog.File
	vf     runner.VolumeFile
	volume string
	mount  string
}

// reapplyGroup is an init step group that runs after the apply.
type reapplyGroup struct {
	g     catalog.InitGroup
	steps []catalog.Step
}

// reapplyJob is everything worked out for one apply.
type reapplyJob struct {
	req    ReapplyRequest
	app    state.App
	oldM   *catalog.Manifest // the manifest the app has now
	m      *catalog.Manifest // the manifest rendered now (the new one when upgrading)
	newRaw string            // upgrade: the new manifest as published
	saved  map[string]any    // the saved parameters as read
	old    Values            // what the app was given
	vals   Values            // what it gets
	oldPl  map[string]string
	newPl  map[string]string
	origin map[string]string // the parameters' origins after the change
	secret func(ref string) bool

	vars    []ReapplyVar
	changed map[string]bool // references that changed and that the manifest reads

	envSet []state.EnvVar
	envDel []string
	cmd    []string
	cmdNew bool
	files  []reapplyFile
	groups []reapplyGroup

	// given: parameters the new version adds that the request gave a value
	// for (upgrade).
	given map[string]bool

	restart bool
	fp      map[string]string // fingerprints of the services' items now
	notes   []string
	// the live deployment before the apply
	liveID, liveImage, liveOrigin string
	liveSeq                       int
}

// ReapplyPlan works out what an apply would do. Nothing is written.
func (e *Engine) ReapplyPlan(ctx context.Context, req ReapplyRequest) (ReapplyPlan, error) {
	job, err := e.reapplyAnalyze(ctx, req)
	if err != nil {
		return ReapplyPlan{}, err
	}
	return job.plan(ctx, e), nil
}

// StartReapply works out what to do and, when anything is to be done (or
// req.Force), starts the task that does it (202). With nothing to do the
// plan comes back without a task.
func (e *Engine) StartReapply(ctx context.Context, req ReapplyRequest) (ReapplyStarted, error) {
	if err := e.settleUndo(ctx, req.App); err != nil {
		return ReapplyStarted{}, err
	}
	job, err := e.reapplyAnalyze(ctx, req)
	if err != nil {
		return ReapplyStarted{}, err
	}
	plan := job.plan(ctx, e)
	if !plan.Needed {
		return ReapplyStarted{Plan: plan}, nil
	}
	if plan.Restart && !req.ConfirmRestart && req.Upgrade == nil {
		f := fault(CodeApplyConfirm, nil, "%s。要用户同意才做：同意后加上 --yes（接口里 confirm_restart: true）再来；什么都还没改", plan.Summary)
		return ReapplyStarted{Plan: plan}, f
	}
	id := newID()
	e.mu.Lock()
	if other, busy := e.running[req.App]; busy {
		e.mu.Unlock()
		f := fault(CodeApplyCannot, nil, "%s 正在安装或重新生效（任务 %s），等它结束再来", req.App, other)
		f.TaskID = other
		return ReapplyStarted{}, f
	}
	e.running[req.App] = id
	e.mine[id] = true
	e.mu.Unlock()

	tr := &taskRun{e: e, id: id, view: TaskView{ID: id, Kind: KindReapply, Target: req.App, Catalog: PublicSource, Status: state.TaskRunning}}
	if job.app.CatalogOrigin != "" {
		tr.view.Catalog = job.app.CatalogOrigin
	}
	tr.view.Apps = []AppResult{{App: req.App, CatalogID: job.m.ID, Version: job.m.Release.Version}}
	for _, s := range reapplyStepLabels {
		tr.view.Steps = append(tr.view.Steps, StepView{App: req.App, Name: s.name, Label: s.label, Status: StepPending})
	}
	data, _ := json.Marshal(tr.view)
	// The table of tasks only knows the kinds app and recipe (a CHECK of the
	// first migration): the record says "app" and its data says what it
	// really is (taskViewOf reads that back as the view's Kind).
	if _, err := e.cfg.Store.CreateInstallTask(ctx, state.InstallTask{ID: id, Kind: "app", Target: req.App, Status: state.TaskRunning, Data: string(data)}); err != nil {
		e.mu.Lock()
		delete(e.running, req.App)
		e.mu.Unlock()
		return ReapplyStarted{}, fault(CodeInternal, err, "记录任务失败")
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
		tr.reapply(tctx, job)
	}()
	v := tr.snapshot()
	return ReapplyStarted{Plan: plan, Task: &v}, nil
}

// Reapplying reports the running task that applies (or installs) app, "" if
// none: the API refuses to delete an app meanwhile.
func (e *Engine) Reapplying(app string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running[app]
}

// reapplyStepLabels are the steps of a reapply task, in order. The names
// are those of an install where the step is the same thing.
var reapplyStepLabels = []struct{ name, label string }{
	{"check", "检查"},
	{"files", "写设置"},
	{"start", "重启"},
	{"health", "健康检查"},
	{"init", "应用里的步骤"},
	{"save", "记下新设置"},
	{"revert", "退回"},
}

// ---- analysis ----

func (e *Engine) reapplyAnalyze(ctx context.Context, req ReapplyRequest) (*reapplyJob, error) {
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
		return nil, fault(CodeApplyCannot, nil, "%s 不是从应用中心装的，没有应用中心的设置可以重新生效（它的环境变量用 acornfox env set 改，改完 acornfox redeploy）", req.App)
	}
	if err := e.busy(req.App); err != nil {
		var f *Fault
		if errors.As(err, &f) {
			g := fault(CodeApplyCannot, nil, "%s 正在安装或重新生效（任务 %s），等它结束再来", req.App, f.TaskID)
			g.TaskID = f.TaskID
			return nil, g
		}
		return nil, err
	}
	if a.CurrentDeployment == "" {
		return nil, fault(CodeApplyCannot, nil, "%s 还没有上线过，没有可以重新生效的东西", req.App)
	}
	oldM, legacy := e.installedWith(ctx, req.App)
	switch {
	case legacy:
		return nil, fault(CodeApplyCannot, nil, "%s 是用旧格式的清单装的，不能重新生效：算不出每一项该是什么（要用新格式重新上架的版本删了重装）", req.App)
	case oldM == nil:
		return nil, fault(CodeApplyCannot, nil, "%s 装的时候没保存清单（或保存的清单坏了），不能重新生效：算不出每一项该是什么（acornfox apps vars %s 看说明）", req.App, req.App)
	}
	saved, err := e.savedParams(ctx, req.App)
	if errors.Is(err, state.ErrNotFound) {
		return nil, fault(CodeApplyCannot, nil, "%s 还没有保存的安装参数：它可能还在安装（acornfox install status 看进度）", req.App)
	}
	if err != nil {
		return nil, fault(CodeInternal, err, "保存的参数解不开（数据目录里的 secrets/catalog.key 换过？）")
	}
	j := &reapplyJob{req: req, app: a, oldM: oldM, m: oldM, saved: saved, changed: map[string]bool{}}
	if u, has := saved[undoKey]; has && u != nil {
		j.notes = append(j.notes, "上一次重新生效没有收尾（退回没成功）：真正开始时会先再退回一次")
	}
	j.old = Values(saved)
	if up := req.Upgrade; up != nil {
		if up.App == nil || up.App.Manifest == nil {
			return nil, fault(CodeInternal, nil, "升级要给新的清单")
		}
		j.m, j.newRaw = up.App.Manifest, string(up.App.Files[manifestFile])
	}
	live, err := e.cfg.Store.GetDeployment(ctx, a.CurrentDeployment)
	if err != nil {
		return nil, fault(CodeInternal, err, "读 %s 现在的版本失败", req.App)
	}
	j.liveID, j.liveImage, j.liveSeq = live.ID, live.ImageID, live.Seq
	j.liveOrigin = live.OriginKind
	if j.liveOrigin == "" {
		j.liveOrigin = live.SourceKind
	}
	if j.liveImage == "" {
		return nil, fault(CodeApplyCannot, nil, "%s 现在的版本没有镜像记录，不能重新生效", req.App)
	}
	j.vals = Values{}
	for k, v := range saved {
		j.vals[k] = v
	}
	if err := j.params(e); err != nil {
		return nil, err
	}
	if err := j.platform(ctx, e); err != nil {
		return nil, err
	}
	if err := j.uses(ctx, e); err != nil {
		return nil, err
	}
	if err := j.diff(ctx, e); err != nil {
		return nil, err
	}
	return j, nil
}

// params puts the parameters asked for into j.vals, checked as at install,
// and (upgrading) carries the saved ones over to the new manifest.
func (j *reapplyJob) params(e *Engine) error {
	m := j.m
	byKey := map[string]catalog.Param{}
	for _, p := range m.Params {
		byKey[p.Key] = p
	}
	j.origin = map[string]string{}
	if o, ok := stringMap(j.saved[originsKey]); ok {
		for k, v := range o {
			j.origin[k] = v
		}
	}
	if j.req.Upgrade != nil {
		// Carry by name; a parameter the new manifest adds gets what an
		// install would give it (default, or generated); one it dropped
		// goes.
		for _, p := range j.oldM.Params {
			if _, kept := byKey[p.Key]; !kept {
				delete(j.vals, p.Key)
				delete(j.origin, p.Key)
			}
		}
		for _, p := range m.Params {
			if _, had := j.old[p.Key]; had {
				continue
			}
			_, isGiven := j.req.Params[p.Key]
			if !isGiven {
				_, isGiven = j.req.Params["params."+p.Key]
			}
			if isGiven && !p.Hidden {
				// The new version's own setting, asked for in this request:
				// the loop below checks and sets it (whatever its editable
				// says: it is given for the first time).
				if j.given == nil {
					j.given = map[string]bool{}
				}
				j.given[p.Key] = true
				continue
			}
			s, empty, err := resolveOneParam(p, nil, false)
			if err != nil {
				return err
			}
			if empty && p.Required {
				return fault(CodeBadParams, nil, "新版本多了必填参数 %s（%s），要先给它一个值：升级时加 --set %s=值", p.Key, p.Label, p.Key)
			}
			j.vals[p.Key] = s
			j.origin[p.Key] = OriginDefault
			if p.Generate != nil && s != "" && p.Default == nil {
				j.origin[p.Key] = OriginGenerated
			}
		}
	}
	keys := make([]string, 0, len(j.req.Params))
	for k := range j.req.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		key := strings.TrimPrefix(k, "params.")
		p, ok := byKey[key]
		if !ok {
			return unknownParam(m, key)
		}
		if p.Hidden {
			return fault(CodeBadParams, nil, "参数 %s 由平台生成，不能填", key)
		}
		switch {
		case j.given[key]:
			// a setting the new version adds: nothing to be "changed" yet
		case p.Editable == "never":
			return fault(CodeApplyEdit, nil, "参数 %s（%s）装好以后不能改：清单写了 editable: never（改了应用会坏）", key, labelOr(p))
		case p.Editable == "reinstall":
			return fault(CodeApplyEdit, nil, "参数 %s（%s）要重装才能改：清单写了 editable: reinstall。问用户同意后保留数据删掉应用，再 acornfox install 应用 --reuse-data --set %s=新值", key, labelOr(p), key)
		}
		s, empty, err := resolveOneParam(p, j.req.Params[k], j.req.Params[k] != nil)
		if err != nil {
			return err
		}
		if empty && p.Required {
			return fault(CodeApplyEdit, nil, "参数 %s（%s）是必填项，不能清空", key, labelOr(p))
		}
		j.vals[key] = s
		j.origin[key] = OriginUser
	}
	return nil
}

func labelOr(p catalog.Param) string {
	if p.Label != "" {
		return p.Label
	}
	return p.Key
}

// platform works out the platform variables the app gets now.
func (j *reapplyJob) platform(ctx context.Context, e *Engine) error {
	req, a := j.req, j.app
	j.oldPl = j.old.platformMap()
	userOld := j.old.platformUser()
	_, haveUser := stringMap(j.saved[platformUserKey])
	sets := map[string]string{}
	for k, v := range userOld {
		sets[k] = v
	}
	given := map[string]string{}
	for k, v := range req.Platform {
		given[strings.TrimPrefix(k, "platform.")] = v
	}
	if err := e.checkPlatformSets(given); err != nil {
		return err
	}
	for k, v := range given {
		sets[k] = strings.TrimSpace(v)
	}
	for _, k := range req.ResetPlatform {
		delete(sets, strings.TrimPrefix(k, "platform."))
	}
	if !haveUser && len(given) == 0 && len(req.ResetPlatform) == 0 {
		// An app saved before the user's own changes were told apart: its
		// platform values stay what they were at install.
		j.newPl = map[string]string{}
		for k, v := range j.oldPl {
			j.newPl[k] = v
		}
		j.notes = append(j.notes, "这个应用装的时候还不分“用户改的”和“平台算的”，平台变量保持装时的值")
		j.vals[platformKey] = j.newPl
		return nil
	}
	app := e.appContext(ctx, a, sets)
	if w := req.AssumeWhere; w != nil && !a.Internal {
		app.Where = *w
	}
	pv, err := e.platformValues(ctx, app)
	if err != nil {
		return err
	}
	j.newPl = pv
	j.vals[platformKey] = pv
	j.vals[platformUserKey] = platformUserSets(sets)
	return nil
}

// uses rebuilds the items of the services the app is plugged into.
func (j *reapplyJob) uses(ctx context.Context, e *Engine) error {
	items, secret, err := e.currentUses(ctx, j.app.Name, j.m)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		j.vals[usesKey] = items
		j.vals[usesSecretKey] = secret
	} else {
		delete(j.vals, usesKey)
		delete(j.vals, usesSecretKey)
	}
	j.fp = usesFingerprint(items)
	return nil
}

// currentUses are the items of every service app is plugged into now: the
// service's own values (defaults filled in), plus, for a service another
// app provides, what that app hands each consumer from its credential. It
// reads the registry only: the providing app need not be running.
func (e *Engine) currentUses(ctx context.Context, app string, m *catalog.Manifest) (map[string]map[string]string, map[string]bool, error) {
	if len(m.Uses) == 0 {
		return nil, nil, nil
	}
	reg, err := e.Registry()
	if err != nil {
		return nil, nil, fault(CodeInternal, err, "读不了服务登记表")
	}
	kinds := e.planKinds()
	items := map[string]map[string]string{}
	secrets := map[string]bool{}
	for _, u := range m.Uses {
		b, err := reg.GetBinding(ctx, app, u.Name)
		if errors.Is(err, state.ErrNotFound) {
			if u.Optional {
				continue
			}
			return nil, nil, fault(CodeApplyCannot, nil, "%s 的“%s”没有接上服务（acornfox services list 看有哪些）", app, u.Name)
		}
		if err != nil {
			return nil, nil, fault(CodeInternal, err, "读 %s 接的服务失败", app)
		}
		sv, err := reg.GetService(ctx, b.Service)
		if err != nil {
			return nil, nil, fault(CodeBadUse, err, "%s 的“%s”接的服务 %s 读不到了（acornfox services list 看）", app, u.Name, b.Service)
		}
		vals, sec := kindItems(kinds[u.Kind], sv.Values)
		if sv.Source == state.ServiceFromApp {
			if pm := e.appManifest(ctx, sv.App); pm != nil && sv.ProvideIndex >= 0 && sv.ProvideIndex < len(pm.Provides) && pm.Provides[sv.ProvideIndex].Kind == sv.Kind {
				pv := pm.Provides[sv.ProvideIndex]
				issuedSecret := issuedSecretKeys(pv, pm, kinds[sv.Kind])
				for item, src := range pv.Values {
					if src.Secret || (src.Kind() == catalog.ValueIssued && issuedSecret[src.Issued]) {
						sec[item] = true
					}
					for _, k := range src.IssuedRefs() {
						if issuedSecret[k] {
							sec[item] = true
						}
					}
				}
				for item, v := range e.consumerItems(ctx, sv, pv, b.Issued) {
					vals[item] = v
				}
			}
		}
		items[u.Name] = vals
		for k := range sec {
			secrets[u.Name+"."+k] = true
		}
	}
	return items, secrets, nil
}

// usesFingerprint is a short fingerprint of every item of every service.
func usesFingerprint(items map[string]map[string]string) map[string]string {
	out := map[string]string{}
	for need, m := range items {
		for item, v := range m {
			sum := sha256.Sum256([]byte(need + "." + item + "\x00" + v))
			out[need+"."+item] = hex.EncodeToString(sum[:8])
		}
	}
	return out
}

// refKey is the part of a reference that names a value: params.<key>,
// platform.<name> or uses.<need>.<item>.
func refKey(ref string) string {
	kind, rest, ok := strings.Cut(ref, ".")
	if !ok {
		return ref
	}
	switch kind {
	case "params":
		k, _, _ := strings.Cut(rest, ".")
		return "params." + k
	case "uses":
		need, after, _ := strings.Cut(rest, ".")
		item, _, _ := strings.Cut(after, ".")
		return "uses." + need + "." + item
	}
	return ref
}

// diff works out what changes, and everything that is to be written.
func (j *reapplyJob) diff(ctx context.Context, e *Engine) error {
	m, a := j.m, j.app
	p := &planned{app: &App{Manifest: m}, name: a.Name, vals: j.vals}
	j.secret = p.secretRefs()

	// Which platform variables the manifest reads at all.
	usedPlat := map[string]bool{}
	for _, n := range catalog.PlatformRefs(m) {
		usedPlat[n] = true
	}
	// parameters
	label := map[string]string{}
	for _, prm := range m.Params {
		label[prm.Key] = prm.Label
		ov, had := j.old[prm.Key].(string)
		nv, _ := j.vals[prm.Key].(string)
		if _, isObj := j.old[prm.Key].(map[string]any); isObj {
			continue
		}
		if had && ov == nv {
			continue
		}
		if !had && nv == "" {
			continue
		}
		j.addVar("params."+prm.Key, prm.Label, ov, nv, j.secret("params."+prm.Key) || prm.Type == "secret", "user", true)
	}
	// platform variables
	names := map[string]bool{}
	for n := range j.oldPl {
		names[n] = true
	}
	for n := range j.newPl {
		names[n] = true
	}
	for _, n := range sortedSet(names) {
		ov, nv := j.oldPl[n], j.newPl[n]
		if ov == nv {
			continue
		}
		from := "platform"
		if _, asked := j.req.Platform[n]; asked {
			from = "user"
		} else if _, asked := j.req.Platform["platform."+n]; asked {
			from = "user"
		}
		pv, _ := catalog.LookupPlatformVar(n)
		j.addVar("platform."+n, pv.Label, ov, nv, pv.Secret, from, usedPlat[n])
	}
	// the services' items: against the fingerprints kept at the last apply
	oldFP, haveFP := stringMap(j.saved[usesFPKey])
	items, _ := j.vals[usesKey].(map[string]map[string]string)
	for _, key := range sortedSet(keysOf(j.fp)) {
		need, item, _ := strings.Cut(key, ".")
		ref := "uses." + key
		// An item nobody in the manifest reads does not matter.
		if !manifestUsesItem(m, ref) || (haveFP && oldFP[key] == j.fp[key]) {
			continue
		}
		old := ""
		if !haveFP {
			old = "（装时没记，当作可能变了）"
		} else if _, had := oldFP[key]; !had {
			old = "（新接上的）"
		} else {
			old = "（已变）"
		}
		j.addVar(ref, "", old, items[need][item], j.vals.useSecret(ref), "service", true)
	}
	if !haveFP && len(j.fp) > 0 {
		j.notes = append(j.notes, "这个应用装的时候没有记接上的服务给了它什么，这次当作可能变了，用到服务的配置会重写一遍")
	}

	// environment variables: rendered now against what the store has
	rendered, err := renderEnv(m, j.vals, j.secret)
	if err != nil {
		return err
	}
	cur, err := e.cfg.Store.ListEnv(ctx, a.Name)
	if err != nil {
		return fault(CodeInternal, err, "读环境变量失败")
	}
	have := map[string]state.EnvVar{}
	for _, ev := range cur {
		have[ev.Key] = ev
	}
	want := map[string]bool{}
	for _, ev := range rendered {
		want[ev.Key] = true
		if c, ok := have[ev.Key]; !ok || c.Value != ev.Value || c.Secret != ev.Secret {
			ev.App = a.Name
			j.envSet = append(j.envSet, ev)
		}
	}
	managed := map[string]bool{}
	for _, ev := range m.Env {
		managed[ev.Key] = true
	}
	for _, k := range sortedSet(managed) {
		if _, ok := have[k]; ok && !want[k] {
			j.envDel = append(j.envDel, k)
		}
	}
	sort.Slice(j.envSet, func(x, y int) bool { return j.envSet[x].Key < j.envSet[y].Key })
	// the start command
	cmd, err := renderArgs(m.Container.Command, j.vals, j.secret)
	if err != nil {
		return err
	}
	j.cmd = cmd
	j.cmdNew = !slices.Equal(cmd, a.Command)

	// config files: those the platform owns (no overwrite word), when what
	// they read changed
	if err := j.collectFiles(ctx, e); err != nil {
		return err
	}

	// init steps that are due: those that watch something that changed, and
	// those of every apply when there is an apply (something changed, or
	// force was asked, or this is an upgrade).
	changedAny := len(j.vars) > 0 || len(j.envSet)+len(j.envDel) > 0 || j.cmdNew || len(j.files) > 0 || j.req.Force || j.req.Upgrade != nil
	var due []reapplyGroup
	for _, g := range catalog.InitGroups(m.Init) {
		if g.When != catalog.StepOnChange {
			continue
		}
		hit := false
		for _, w := range g.Watch {
			if j.changed[refKey(w)] {
				hit = true
			}
		}
		if hit {
			due = append(due, reapplyGroup{g: g})
			changedAny = true
		}
	}
	for _, g := range catalog.InitGroups(m.Init) {
		if g.When == catalog.StepOnReapply && changedAny {
			due = append(due, reapplyGroup{g: g})
		}
	}
	// in the order the manifest writes them
	sort.SliceStable(due, func(x, y int) bool { return due[x].g.Index[0] < due[y].g.Index[0] })
	for _, rg := range due {
		for _, at := range rg.g.Index {
			rg.steps = append(rg.steps, initActionStep(m.Init[at]))
		}
		j.groups = append(j.groups, rg)
	}

	j.restart = len(j.envSet)+len(j.envDel) > 0 || j.cmdNew || len(j.files) > 0 || j.req.Force || j.req.Upgrade != nil
	if a.Desired == state.DesiredStopped {
		if len(j.groups) > 0 {
			return fault(CodeApplyCannot, nil, "%s 停着，而这次改动要在应用里做步骤（%s），停着的应用里做不了：先问用户，同意后 acornfox start %s，再来", a.Name, groupNames(j.groups), a.Name)
		}
		if j.restart {
			j.notes = append(j.notes, "应用停着：新设置写好了，下次启动就用上，这次不重启")
		}
		j.restart = false
	}
	return nil
}

func manifestUsesItem(m *catalog.Manifest, ref string) bool {
	need, item, _ := strings.Cut(strings.TrimPrefix(ref, "uses."), ".")
	return catalog.UsedItems(m, need)[item]
}

func keysOf(m map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range m {
		out[k] = true
	}
	return out
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// groupNames names the groups for a message: "每次重新生效后的步骤" and the
// like.
func groupNames(gs []reapplyGroup) string {
	var out []string
	for _, g := range gs {
		switch g.g.When {
		case catalog.StepOnReapply:
			out = append(out, "每次重新生效后的步骤")
		default:
			out = append(out, "某项设置变了后的步骤")
		}
	}
	return strings.Join(out, "、")
}

// addVar records one changed value; changed marks it (when the manifest
// reads it) as a reason for files and steps to follow.
func (j *reapplyJob) addVar(name, label, old, now string, secret bool, from string, reads bool) {
	if !reads {
		return
	}
	if secret {
		old, now = maskSecretText(old), maskSecretText(now)
	}
	j.vars = append(j.vars, ReapplyVar{Name: name, Label: label, Old: old, New: now, Secret: secret, From: from})
	j.changed[name] = true
}

func maskSecretText(v string) string {
	if v == "" || strings.HasPrefix(v, "（") {
		return v
	}
	return maskSecret(v)
}

// collectFiles lists the config files to write again.
func (j *reapplyJob) collectFiles(ctx context.Context, e *Engine) error {
	m := j.m
	var vols []state.Volume
	loaded := false
	byName := map[string]struct{ volume, mount string }{}
	load := func() error {
		if loaded {
			return nil
		}
		var err error
		if vols, err = e.cfg.Store.ListVolumes(ctx, j.app.Name); err != nil {
			return fault(CodeInternal, err, "读数据卷失败")
		}
		for _, mv := range m.Volumes {
			for _, sv := range vols {
				if sv.Path == mv.Path {
					byName[mv.Name] = struct{ volume, mount string }{sv.VolumeName, sv.Path}
				}
			}
		}
		loaded = true
		return nil
	}
	for _, f := range m.Files {
		if f.Overwrite != "" {
			continue // install-only and keep-what-is-there files are not touched
		}
		if f.If != "" {
			ok, err := j.vals.holds(f.If)
			if err != nil {
				return renderFault(CodeFiles, err, "配置文件 %s 的 if", f.Path)
			}
			if !ok {
				continue
			}
		}
		affected := j.req.Force || j.req.Upgrade != nil
		for _, r := range catalog.TemplateRefs(f.Template) {
			if j.changed[refKey(r)] {
				affected = true
			}
		}
		if f.If != "" {
			if c, err := catalog.ParseCondition(f.If); err == nil && j.changed[refKey(c.Ref)] {
				affected = true
			}
		}
		if !affected {
			continue
		}
		if err := load(); err != nil {
			return err
		}
		tg, ok := byName[f.Volume]
		if !ok {
			return fault(CodeFiles, nil, "配置文件 %s 指向的数据卷 %s 不存在", f.Path, f.Volume)
		}
		content, err := j.vals.render(f.Template, fileEscape(f.Path))
		if err != nil {
			return renderFault(CodeFiles, err, "配置文件 %s 的模板", f.Path)
		}
		vf, err := volumeFile(f, content)
		if err != nil {
			return fault(CodeFiles, err, "配置文件 %s：%v", f.Path, err)
		}
		j.files = append(j.files, reapplyFile{file: f, vf: vf, volume: tg.volume, mount: tg.mount})
	}
	return nil
}

// plan is the job as the plan the user sees.
func (j *reapplyJob) plan(ctx context.Context, e *Engine) ReapplyPlan {
	p := ReapplyPlan{App: j.app.Name, Vars: append([]ReapplyVar{}, j.vars...), Env: []string{}, Gone: []string{}, Files: []string{}, Steps: []ReapplyStepPlan{}, Notes: append([]string{}, j.notes...)}
	for _, ev := range j.envSet {
		p.Env = append(p.Env, ev.Key)
	}
	p.Gone = append(p.Gone, j.envDel...)
	p.Command = j.cmdNew
	for _, f := range j.files {
		p.Files = append(p.Files, f.vf.Path)
	}
	for _, g := range j.groups {
		sp := ReapplyStepPlan{When: g.g.When, Watch: g.g.Watch}
		for _, at := range g.g.Index {
			name := j.m.Init[at].Name
			if name == "" {
				name = fmt.Sprintf("第 %d 步", at+1)
			}
			sp.Names = append(sp.Names, name)
		}
		p.Steps = append(p.Steps, sp)
	}
	p.Restart = j.restart
	p.Needed = j.restart || len(j.groups) > 0 || len(j.vars) > 0 || len(j.envSet)+len(j.envDel) > 0 || j.cmdNew || j.req.Force
	p.Dependents = j.dependents(ctx, e)
	p.Summary = j.summary(p)
	if !p.Needed {
		p.Notes = append(p.Notes, "现在的设置和应用里用的一致，没有要改的")
	}
	return p
}

// summary says what the apply does, in one sentence.
func (j *reapplyJob) summary(p ReapplyPlan) string {
	if !p.Needed {
		return j.app.Name + " 的设置没有变化，什么都不用做"
	}
	var parts []string
	if n := len(p.Vars); n > 0 {
		parts = append(parts, fmt.Sprintf("%d 项设置变了", n))
	}
	if n := len(p.Env) + len(p.Gone); n > 0 {
		parts = append(parts, fmt.Sprintf("改写 %d 个环境变量", n))
	}
	if n := len(p.Files); n > 0 {
		parts = append(parts, fmt.Sprintf("重写 %d 个配置文件", n))
	}
	if p.Command {
		parts = append(parts, "改启动命令")
	}
	if n := len(p.Steps); n > 0 {
		parts = append(parts, "在应用里做重新生效的步骤")
	}
	s := j.app.Name + "：" + strings.Join(parts, "，")
	if p.Restart {
		s += "；会重启 " + j.app.Name + "（数据不动，中间有一小段时间用不了，失败会自动退回）"
	} else {
		s += "；不用重启"
	}
	return s
}

// dependents are the apps that use a service this app provides when that
// service's values would change by this apply.
func (j *reapplyJob) dependents(ctx context.Context, e *Engine) []string {
	m := j.m
	if len(m.Provides) == 0 {
		return nil
	}
	reg, err := e.Registry()
	if err != nil {
		return nil
	}
	var out []string
	for i, pv := range m.Provides {
		name, err := e.providedService(ctx, reg, j.app.Name, i)
		if err != nil {
			continue
		}
		sv, err := reg.GetService(ctx, name)
		if err != nil {
			continue
		}
		values, err := provideValues(m, pv, j.app.Name, j.vals)
		if err != nil {
			continue
		}
		differs := false
		for k, v := range values {
			if sv.Values[k] != v {
				differs = true
			}
		}
		if !differs {
			continue
		}
		bs, err := reg.ListBindings(ctx, state.BindingFilter{Service: name})
		if err != nil {
			continue
		}
		for _, b := range bs {
			if b.App != j.app.Name && !slices.Contains(out, b.App) {
				out = append(out, b.App)
			}
		}
	}
	sort.Strings(out)
	return out
}

// ---- execution ----

// undoRecord is the journal of an apply: what is needed to put the app back.
// It is kept, encrypted, with the app's saved parameters (undoKey) from the
// first write until the apply has ended.
type undoRecord struct {
	Task string `json:"task"`
	// Env: every environment variable the apply wrote or removed, as it was
	// (Had false: it did not exist).
	Env     []undoEnv `json:"env"`
	Command []string  `json:"command"`
	// The version that was live: the container to go back to.
	LiveID    string `json:"live_id"`
	LiveImage string `json:"live_image"`
	LiveSeq   int    `json:"live_seq"`
	LiveKind  string `json:"live_kind"`
	// Files: the config files the apply wrote, rendered again with the old
	// values to put back (path, volume, mount, mode, owner).
	Files []undoFile `json:"files"`
	// Desired: the app was running (a stopped app is put back without a
	// container).
	Redeploy bool `json:"redeploy"`
	// An upgrade: the pre-upgrade snapshot taken before anything was
	// changed ("" none) and the fields of the app record that follow the
	// manifest, as they were.
	Snapshot string   `json:"snapshot,omitempty"`
	App      *undoApp `json:"app,omitempty"`
}

// undoApp is what an upgrade changes in the app record besides the command
// (which the journal keeps already).
type undoApp struct {
	Port        int                `json:"port"`
	HealthPath  string             `json:"health_path"`
	HealthCheck *state.HealthCheck `json:"health_check,omitempty"`
	PidsLimit   int                `json:"pids_limit"`
}

type undoEnv struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
	Had    bool   `json:"had"`
}

type undoFile struct {
	Volume  string `json:"volume"`
	Mount   string `json:"mount"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Mode    int    `json:"mode"`
	UID     int    `json:"uid"`
	GID     int    `json:"gid"`
}

// journal builds the undo record before anything is written. The files are
// rendered with the values the app had (and the services as they are now:
// what a config file held of a service's values that changed since cannot be
// known).
func (j *reapplyJob) journal(ctx context.Context, e *Engine, taskID string) (*undoRecord, error) {
	u := &undoRecord{Task: taskID, Command: j.app.Command, LiveID: j.liveID, LiveImage: j.liveImage, LiveSeq: j.liveSeq, LiveKind: j.liveOrigin, Redeploy: j.restart}
	if up := j.req.Upgrade; up != nil {
		u.Snapshot = up.SnapshotID
		u.App = &undoApp{Port: j.app.Port, HealthPath: j.app.HealthPath, HealthCheck: j.app.HealthCheck, PidsLimit: j.app.PidsLimit}
	}
	cur, err := e.cfg.Store.ListEnv(ctx, j.app.Name)
	if err != nil {
		return nil, fault(CodeInternal, err, "读环境变量失败")
	}
	have := map[string]state.EnvVar{}
	for _, ev := range cur {
		have[ev.Key] = ev
	}
	touched := map[string]bool{}
	for _, ev := range j.envSet {
		touched[ev.Key] = true
	}
	for _, k := range j.envDel {
		touched[k] = true
	}
	for _, k := range sortedSet(touched) {
		ev, ok := have[k]
		u.Env = append(u.Env, undoEnv{Key: k, Value: ev.Value, Secret: ev.Secret, Had: ok})
	}
	if len(j.files) > 0 {
		old := Values{}
		for k, v := range j.old {
			old[k] = v
		}
		if items, ok := j.vals[usesKey]; ok {
			old[usesKey], old[usesSecretKey] = items, j.vals[usesSecretKey]
		}
		if j.req.Upgrade != nil {
			return u, nil // an upgrade's files are those of the old manifest: see revert
		}
		for _, f := range j.files {
			content, err := old.render(f.file.Template, fileEscape(f.file.Path))
			if err != nil {
				continue // cannot be put back from here: left as written
			}
			vf, err := volumeFile(f.file, content)
			if err != nil {
				continue
			}
			u.Files = append(u.Files, undoFile{Volume: f.volume, Mount: f.mount, Path: vf.Path, Content: content, Mode: vf.Mode, UID: vf.UID, GID: vf.GID})
		}
	}
	return u, nil
}

func (e *Engine) writeSaved(ctx context.Context, app string, mod func(map[string]any)) error {
	cs, err := e.cfg.Store.GetCatalogSecrets(ctx, app)
	if err != nil {
		return err
	}
	plain, err := e.box.open(cs.ParamsEnc)
	if err != nil {
		return err
	}
	var vals map[string]any
	if err := json.Unmarshal(plain, &vals); err != nil {
		return err
	}
	mod(vals)
	b, err := json.Marshal(vals)
	if err != nil {
		return err
	}
	enc, err := e.box.seal(b)
	if err != nil {
		return err
	}
	cs.ParamsEnc = enc
	return e.cfg.Store.SetCatalogSecrets(ctx, cs)
}

func (e *Engine) saveUndo(ctx context.Context, app string, u *undoRecord) error {
	return e.writeSaved(ctx, app, func(v map[string]any) {
		if u == nil {
			delete(v, undoKey)
			return
		}
		raw, _ := json.Marshal(u)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		v[undoKey] = m
	})
}

func (e *Engine) readUndo(ctx context.Context, app string) (*undoRecord, error) {
	saved, err := e.savedParams(ctx, app)
	if err != nil {
		return nil, err
	}
	raw, ok := saved[undoKey]
	if !ok || raw == nil {
		return nil, nil
	}
	b, _ := json.Marshal(raw)
	var u undoRecord
	if err := json.Unmarshal(b, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// reapply runs the apply of job as the task t.
func (t *taskRun) reapply(ctx context.Context, job *reapplyJob) {
	e, app := t.e, job.app.Name
	t.step(ctx, app, "check", StepRunning, "")
	u, err := job.journal(ctx, e, t.id)
	if err != nil {
		t.finish(ctx, t.failStep(ctx, app, "check", AsFault(err)))
		return
	}
	if err := e.saveUndo(ctx, app, u); err != nil {
		t.finish(ctx, t.failStep(ctx, app, "check", fault(CodeInternal, err, "写重新生效的记录失败")))
		return
	}
	t.step(ctx, app, "check", StepDone, job.summary(job.plan(ctx, e)))
	if f := t.reapplyBody(ctx, job, u); f != nil {
		t.finish(ctx, f)
		return
	}
	t.step(ctx, app, "revert", StepSkipped, "没出错，不用退回")
	t.finish(ctx, nil)
}

// failStep marks the step failed and returns f (with the step's message).
func (t *taskRun) failStep(ctx context.Context, app, name string, f *Fault) *Fault {
	t.step(ctx, app, name, StepFailed, f.Code+" "+f.Message)
	return f
}

// reapplyBody does the writes, the restart and the steps; on a failure it
// puts everything back and returns the fault the task ends with.
func (t *taskRun) reapplyBody(ctx context.Context, job *reapplyJob, u *undoRecord) *Fault {
	e, app, m := t.e, job.app.Name, job.m
	hooks := e.h()
	cat, _ := e.cfg.Runner.(runner.CatalogAPI)
	var depID string
	fail := func(step string, cause *Fault) *Fault {
		t.step(ctx, app, step, StepFailed, cause.Code+" "+cause.Message)
		if job.req.Upgrade != nil {
			return t.undoUpgrade(ctx, job, u, depID, cause)
		}
		return t.undoReapply(ctx, job, u, depID, cause)
	}

	// 1. write what changed, then recreate the container
	t.step(ctx, app, "files", StepRunning, "")
	err := hooks.withApp(ctx, app, func() error {
		if up := job.req.Upgrade; up != nil {
			// The new version's image: taken again here, under the app's
			// lock, so image GC cannot take it between being fetched and
			// being used (a local copy is adopted in a moment; a copy GC
			// removed in the meantime is fetched again), and the fields of
			// the app record that follow the manifest.
			id, _, err := t.image(ctx, &planned{app: up.App, name: app}, cat)
			if err != nil {
				return AsFault(err)
			}
			up.ImageID = id
			if err := e.upgradeRecord(ctx, app, m); err != nil {
				return err
			}
		}
		for _, ev := range job.envSet {
			if err := e.cfg.Store.SetEnv(ctx, ev); err != nil {
				return fault(CodeInternal, err, "写环境变量 %s 失败", ev.Key)
			}
		}
		for _, k := range job.envDel {
			if err := e.cfg.Store.DeleteEnv(ctx, app, k); err != nil && !errors.Is(err, state.ErrNotFound) {
				return fault(CodeInternal, err, "去掉环境变量 %s 失败", k)
			}
		}
		if job.cmdNew {
			if _, err := e.cfg.Store.UpdateApp(ctx, app, func(a *state.App) error { a.Command = job.cmd; return nil }); err != nil {
				return fault(CodeInternal, err, "保存启动命令失败")
			}
		}
		if err := e.writeReapplyFiles(ctx, app, cat, job.files, job.imageForFiles()); err != nil {
			return err
		}
		if !job.restart {
			return nil
		}
		reason := state.ReasonReapply
		image := job.liveImage
		if up := job.req.Upgrade; up != nil {
			image = up.ImageID
		}
		d, _, err := e.cfg.Store.CreateDeployment(ctx, state.NewDeployment{
			App: app, SourceKind: state.SourceImage, SourceRef: image, SourceDigest: image,
			BypassDedup: true, RequestKey: "reapply-" + t.id, BasedOnSeq: job.liveSeq, OriginKind: job.liveOrigin, Reason: reason,
		})
		if err != nil {
			return fault(CodeInternal, err, "建新版本失败")
		}
		depID = d.ID
		return nil
	})
	if err != nil {
		return fail("files", AsFault(err))
	}
	msg := fmt.Sprintf("改了 %d 个环境变量", len(job.envSet)+len(job.envDel))
	if len(job.files) > 0 {
		msg += fmt.Sprintf("，重写了 %d 个配置文件", len(job.files))
	}
	if job.cmdNew {
		msg += "，改了启动命令"
	}
	t.step(ctx, app, "files", StepDone, msg)

	var cur state.App
	base := ""
	if job.restart {
		t.step(ctx, app, "start", StepRunning, "")
		if hooks.Kick != nil {
			hooks.Kick(app)
		}
		var werr error
		cur, werr = t.waitLive(ctx, app, depID)
		if werr != nil {
			return fail("start", AsFault(werr))
		}
		t.step(ctx, app, "start", StepDone, "新的容器已上线")
		t.step(ctx, app, "health", StepRunning, "")
		var herr error
		base, herr = t.healthy(ctx, &planned{app: &App{Manifest: m}, name: app}, cur)
		if herr != nil {
			return fail("health", AsFault(herr))
		}
		t.step(ctx, app, "health", StepDone, "")
	} else {
		t.step(ctx, app, "start", StepSkipped, "不用重启")
		t.step(ctx, app, "health", StepSkipped, "")
		cur = job.app
		if len(job.groups) > 0 {
			if a2, err := e.cfg.Store.GetApp(ctx, app); err == nil {
				cur = a2
			}
			if b, err := e.hostBase(ctx, cur); err == nil {
				base = b
			}
		}
	}

	// 2. the init steps that are due
	if len(job.groups) == 0 {
		t.step(ctx, app, "init", StepSkipped, "没有要做的步骤")
	} else {
		t.step(ctx, app, "init", StepRunning, "")
		for _, g := range job.groups {
			env := ActionEnv{Lookup: job.vals.lookup, SecretRef: job.secret, Group: GroupInit}
			if stepsRunCommands(g.steps) {
				env.Exec = ExecTarget{App: app, Container: runner.ContainerName(app, cur.CurrentDeployment)}
			}
			if _, aerr := e.actions().run(ctx, base, g.steps, env); aerr != nil {
				return fail("init", initFault(aerr))
			}
		}
		t.step(ctx, app, "init", StepDone, groupNames(job.groups))
	}

	// 3. remember it
	t.step(ctx, app, "save", StepRunning, "")
	if err := e.finishReapply(ctx, job); err != nil {
		return fail("save", AsFault(err))
	}
	t.step(ctx, app, "save", StepDone, "")
	t.setApp(app, func(r *AppResult) {
		r.Services = nil
		r.Affected = job.dependents(ctx, e)
		r.Version = m.Release.Version
	})
	return nil
}

// upgradeRecord puts the fields of the app record that follow the manifest
// (the port and the health check, the process limit) on the new version's.
// The command is written with the other changed values (cmdNew).
func (e *Engine) upgradeRecord(ctx context.Context, app string, m *catalog.Manifest) error {
	ep := primaryEndpoint(m)
	health := "/"
	if ep != nil && ep.Health != nil && ep.Health.Path != "" {
		health = ep.Health.Path
	}
	if _, err := e.cfg.Store.UpdateApp(ctx, app, func(a *state.App) error {
		if ep != nil {
			a.Port = ep.Port
		}
		a.HealthPath = health
		a.HealthCheck = stateHealthCheck(ep)
		a.PidsLimit = m.Container.PidsLimit
		return nil
	}); err != nil {
		return fault(CodeInternal, err, "保存应用设置失败")
	}
	return nil
}

// imageForFiles is the image config files are written with: the one that is
// live (a fresh volume is populated from it as the app sees it).
func (j *reapplyJob) imageForFiles() string {
	if up := j.req.Upgrade; up != nil {
		return up.ImageID
	}
	return j.liveImage
}

// writeReapplyFiles writes the files, a volume at a time.
func (e *Engine) writeReapplyFiles(ctx context.Context, app string, cat runner.CatalogAPI, files []reapplyFile, image string) error {
	if len(files) == 0 {
		return nil
	}
	if cat == nil {
		return fault(CodeInternal, nil, "运行器不支持写配置文件，请升级 AcornFox")
	}
	type target struct{ volume, mount string }
	groups := map[target][]runner.VolumeFile{}
	var order []target
	for _, f := range files {
		tg := target{f.volume, f.mount}
		if _, seen := groups[tg]; !seen {
			order = append(order, tg)
		}
		f.vf.Overwrite = true
		groups[tg] = append(groups[tg], f.vf)
	}
	for _, tg := range order {
		if _, err := cat.WriteVolumeFiles(ctx, runner.WriteFilesRequest{App: app, Volume: tg.volume, MountPath: tg.mount, Image: image, Files: groups[tg]}); err != nil {
			return fault(CodeFiles, err, "写配置文件失败：%v", err)
		}
	}
	return nil
}

// finishReapply saves what the app has now: its parameters (with the new
// fingerprints of the services' items), the first-login fields that follow
// the changed values, the manifest and version when upgrading, and the
// values of the services it provides.
func (e *Engine) finishReapply(ctx context.Context, job *reapplyJob) error {
	app, m := job.app.Name, job.m
	next := map[string]any{}
	for k, v := range job.vals {
		next[k] = v
	}
	delete(next, usesKey)
	delete(next, usesSecretKey)
	delete(next, undoKey)
	next[originsKey] = job.origin
	if len(job.fp) > 0 {
		next[usesFPKey] = job.fp
	} else {
		delete(next, usesFPKey)
	}
	pb, err := json.Marshal(next)
	if err != nil {
		return err
	}
	pe, err := e.box.seal(pb)
	if err != nil {
		return err
	}
	cs, err := e.cfg.Store.GetCatalogSecrets(ctx, app)
	if err != nil {
		return err
	}
	// first-login fields: those that read a value that changed are worked
	// out again, the others stay as they are (a field the app's own data
	// holds is not the platform's to know).
	if plain, err := e.box.open(cs.FirstLoginEnc); err == nil {
		var fl FirstLogin
		if json.Unmarshal(plain, &fl) == nil && len(fl.Fields) == len(m.FirstLogin) {
			for i, lf := range m.FirstLogin {
				refs := catalog.TemplateRefs(lf.Value)
				if lf.From != "" {
					refs = []string{lf.From}
				}
				for _, r := range refs {
					if job.changed[refKey(r)] {
						if f2, ferr := loginFields(&catalog.Manifest{FirstLogin: []catalog.LoginField{lf}}, job.vals); ferr == nil && len(f2) == 1 {
							fl.Fields[i] = f2[0]
						}
						break
					}
				}
			}
			fl.URL = e.address(ctx, job.app)
			if b, err := json.Marshal(fl); err == nil {
				if enc, err := e.box.seal(b); err == nil {
					cs.FirstLoginEnc = enc
				}
			}
		}
	}
	cs.ParamsEnc = pe
	if up := job.req.Upgrade; up != nil {
		text := job.newRaw
		if err := e.cfg.Store.SetCatalogManifest(ctx, state.CatalogManifest{
			App: app, CatalogID: m.ID, CatalogVersion: m.Release.Version, CatalogOrigin: up.App.Origin,
			Manifest: text, SHA256: state.ManifestSHA256(text), Source: state.ManifestFromInstall,
		}); err != nil {
			return fmt.Errorf("保存清单失败：%w", err)
		}
		if _, err := e.cfg.Store.UpdateApp(ctx, app, func(a *state.App) error {
			a.CatalogVersion = m.Release.Version
			a.PidsLimit = m.Container.PidsLimit
			return nil
		}); err != nil {
			return err
		}
	}
	if err := e.cfg.Store.SetCatalogSecrets(ctx, cs); err != nil {
		return err
	}
	e.updateProvides(ctx, job)
	return nil
}

// updateProvides writes the values of the services the app provides again
// (those written as templates: they may read the parameters just changed).
// What the user filled in by hand is kept. A failure is logged only: the
// apply itself is done, and the next one tries again.
func (e *Engine) updateProvides(ctx context.Context, job *reapplyJob) {
	m, app := job.m, job.app.Name
	if len(m.Provides) == 0 {
		return
	}
	reg, err := e.Registry()
	if err != nil {
		return
	}
	for i, pv := range m.Provides {
		name, err := e.providedService(ctx, reg, app, i)
		if err != nil {
			continue
		}
		values, err := provideValues(m, pv, app, job.vals)
		if err != nil {
			e.cfg.Log.Warn("reapply: provided values", "app", app, "service", name, "err", err)
			continue
		}
		if _, err := reg.UpdateService(ctx, name, func(sv *Service) error {
			merged := map[string]string{}
			for k, v := range sv.Values {
				merged[k] = v
			}
			for k, v := range values {
				merged[k] = v
			}
			sv.Values = merged
			return nil
		}); err != nil {
			e.cfg.Log.Warn("reapply: update service", "app", app, "service", name, "err", err)
		}
	}
}

// undoReapply puts the app back as it was before the apply and returns the
// fault the task ends with: APP-APPLY-03 when it worked (the cause is in
// the message), APP-APPLY-04 when it did not.
func (t *taskRun) undoReapply(ctx context.Context, job *reapplyJob, u *undoRecord, depID string, cause *Fault) *Fault {
	e, app := t.e, job.app.Name
	ctx = context.WithoutCancel(ctx)
	t.step(ctx, app, "revert", StepRunning, "")
	if err := e.putBack(ctx, app, job.m, job.oldM, u, depID, job.req.Upgrade != nil, job); err != nil {
		f := fault(CodeApplyUndone, err, "重新生效没成功（%s %s），退回原来的设置时又出了错：%v。应用可能停在新旧设置混在一起的状态", cause.Code, cause.Message, err)
		f.Diagnosis = cause.Diagnosis
		t.step(ctx, app, "revert", StepFailed, f.Code+" "+err.Error())
		return f
	}
	t.step(ctx, app, "revert", StepDone, "已退回原来的设置，应用照原来的在运行")
	f := fault(CodeApplyFailed, nil, "重新生效没成功，已经退回原来的设置：%s %s", cause.Code, cause.Message)
	f.Diagnosis = cause.Diagnosis
	return f
}

// putBack restores the store, the config files and the container from the
// journal u. depID is the deployment the apply created ("" none).
func (e *Engine) putBack(ctx context.Context, app string, m, oldM *catalog.Manifest, u *undoRecord, depID string, upgrading bool, job *reapplyJob) error {
	var errs []string
	note := func(err error, what string) {
		if err != nil {
			errs = append(errs, what+"："+err.Error())
		}
	}
	for _, ue := range u.Env {
		if ue.Had {
			note(e.cfg.Store.SetEnv(ctx, state.EnvVar{App: app, Key: ue.Key, Value: ue.Value, Secret: ue.Secret}), "环境变量 "+ue.Key)
		} else if err := e.cfg.Store.DeleteEnv(ctx, app, ue.Key); err != nil && !errors.Is(err, state.ErrNotFound) {
			note(err, "环境变量 "+ue.Key)
		}
	}
	_, err := e.cfg.Store.UpdateApp(ctx, app, func(a *state.App) error {
		a.Command = u.Command
		if u.App != nil {
			a.Port, a.HealthPath, a.HealthCheck, a.PidsLimit = u.App.Port, u.App.HealthPath, u.App.HealthCheck, u.App.PidsLimit
		}
		return nil
	})
	note(err, "启动命令")
	if len(u.Files) > 0 {
		cat, _ := e.cfg.Runner.(runner.CatalogAPI)
		var files []reapplyFile
		for _, f := range u.Files {
			files = append(files, reapplyFile{vf: runner.VolumeFile{Path: f.Path, Content: []byte(f.Content), Mode: f.Mode, UID: f.UID, GID: f.GID, Overwrite: true}, volume: f.Volume, mount: f.Mount})
		}
		note(e.writeReapplyFiles(ctx, app, cat, files, u.LiveImage), "配置文件")
	}
	if upgrading {
		// The old manifest's files go back from the old manifest: render
		// them with what the app had.
		note(e.putBackManifestFiles(ctx, app, oldM, job, u), "旧版本的配置文件")
	}
	if depID != "" {
		// A container made from the new settings exists or is on its way:
		// make another from the old, which supersedes it. Not needed when
		// the new one failed and the old one is still the live one.
		a, gerr := e.cfg.Store.GetApp(ctx, app)
		d, derr := e.cfg.Store.GetDeployment(ctx, depID)
		stillOld := gerr == nil && a.CurrentDeployment == u.LiveID && derr == nil && (d.Status == state.StatusFailed || d.Status == state.StatusSuperseded)
		if !stillOld {
			nd, _, err := e.cfg.Store.CreateDeployment(ctx, state.NewDeployment{
				App: app, SourceKind: state.SourceImage, SourceRef: u.LiveImage, SourceDigest: u.LiveImage,
				BypassDedup: true, RequestKey: "reapply-" + u.Task + "-undo", BasedOnSeq: u.LiveSeq, OriginKind: u.LiveKind, Reason: state.ReasonReapply,
			})
			if err != nil {
				note(err, "重新建旧版本")
			} else {
				if h := e.h(); h.Kick != nil {
					h.Kick(app)
				}
				tr := &taskRun{e: e}
				if _, werr := tr.waitLive(ctx, app, nd.ID); werr != nil {
					note(werr, "旧版本上线")
				}
			}
		}
	}
	if len(errs) == 0 {
		note(e.saveUndo(ctx, app, nil), "清掉重新生效的记录")
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "；"))
	}
	return nil
}

// putBackManifestFiles is for an upgrade that fails: the files of the old
// manifest are rendered again with the old values.
func (e *Engine) putBackManifestFiles(ctx context.Context, app string, oldM *catalog.Manifest, job *reapplyJob, u *undoRecord) error {
	if oldM == nil || job == nil {
		return nil
	}
	old := Values{}
	for k, v := range job.old {
		old[k] = v
	}
	if items, ok := job.vals[usesKey]; ok {
		old[usesKey], old[usesSecretKey] = items, job.vals[usesSecretKey]
	}
	oj := &reapplyJob{app: job.app, m: oldM, vals: old, changed: map[string]bool{}, req: ReapplyRequest{Force: true}}
	if err := oj.collectFiles(ctx, e); err != nil {
		return err
	}
	cat, _ := e.cfg.Runner.(runner.CatalogAPI)
	return e.writeReapplyFiles(ctx, app, cat, oj.files, u.LiveImage)
}

// settleUndo puts back an apply that left its journal behind because it
// could not be undone the first time (nothing runs it again by itself). The
// journal is the app's: a running install or apply is left alone.
func (e *Engine) settleUndo(ctx context.Context, app string) error {
	if !state.ValidAppName(app) || e.busy(app) != nil {
		return nil
	}
	u, err := e.readUndo(ctx, app)
	if err != nil || u == nil {
		return nil
	}
	depID := ""
	if u.Redeploy {
		depID = "interrupted"
	}
	if err := e.putBackRecovered(ctx, app, u, depID); err != nil {
		return fault(CodeApplyUndone, err, "%s 上一次重新生效没成功，退回原来的设置时出了错，这次再退回也没成功：%v", app, err)
	}
	return nil
}

// recoverReapply puts back an apply the server stopped in the middle of: the
// journal is read from the app's saved parameters; the old environment,
// command and files are written and the container is made again from the
// old image.
func (e *Engine) recoverReapply(ctx context.Context, t state.InstallTask, v TaskView) {
	app := t.Target
	tr := &taskRun{e: e, id: t.ID, view: v}
	u, err := e.readUndo(ctx, app)
	if err != nil || u == nil {
		if v.Kind == KindUpgrade {
			tr.finish(ctx, fault(CodeUpgFailed, nil, "升级途中服务重启了，还没有动应用（没有要退回的记录），应用保持现在的样子；新版本还在，可以再升级一次"))
			return
		}
		tr.finish(ctx, fault(CodeApplyFailed, nil, "重新生效途中服务重启了，没有找到要退回的记录，应用保持现在的样子"))
		return
	}
	depID := ""
	if u.Redeploy {
		depID = "interrupted" // not a deployment id: forces the container to be made again
	}
	if v.Kind == KindUpgrade {
		e.recoverUpgrade(ctx, tr, app, u, depID)
		return
	}
	if err := e.putBackRecovered(ctx, app, u, depID); err != nil {
		tr.finish(ctx, fault(CodeApplyUndone, err, "重新生效途中服务重启了，退回原来的设置时出了错：%v", err))
		return
	}
	tr.finish(ctx, fault(CodeApplyFailed, nil, "重新生效途中服务重启了，已经退回原来的设置"))
}

// recoverUpgrade puts back an upgrade the server stopped in the middle of:
// from the pre-upgrade snapshot when the upgrade took one (the new version
// may have run and changed the data), else from the journal alone.
func (e *Engine) recoverUpgrade(ctx context.Context, tr *taskRun, app string, u *undoRecord, depID string) {
	var notes []string
	if u.Snapshot != "" {
		if snaps := e.snapshots(); snaps != nil {
			_, err := snaps.RestoreSnapshot(ctx, app, u.Snapshot, appbackup.RestoreOptions{})
			if err == nil {
				_ = e.saveUndo(ctx, app, nil)
				e.appEvent(ctx, app, "升级途中服务重启了，已经用升级前的快照退回")
				tr.finish(ctx, fault(CodeUpgFailed, nil, "升级途中服务重启了，已经用升级前的快照退回到升级前（数据和设置都是升级前的）"))
				return
			}
			notes = append(notes, "用快照退回没成功："+err.Error())
		}
	}
	if err := e.putBackRecovered(ctx, app, u, depID); err != nil {
		notes = append(notes, err.Error())
		tr.finish(ctx, fault(CodeUpgUndone, err, "升级途中服务重启了，退回到升级前时出了错：%s", strings.Join(notes, "；")))
		return
	}
	if len(notes) > 0 {
		tr.finish(ctx, fault(CodeUpgUndone, nil, "升级途中服务重启了，程序和设置已退回，但数据没能用快照退回，新版本可能已经改过它：%s", strings.Join(notes, "；")))
		return
	}
	tr.finish(ctx, fault(CodeUpgFailed, nil, "升级途中服务重启了，已经退回到升级前的设置和程序"))
}

// putBackRecovered is putBack after a restart: there is no deployment of the
// apply to look at, the container is made again from the old image when the
// apply had restarted the app.
func (e *Engine) putBackRecovered(ctx context.Context, app string, u *undoRecord, depID string) error {
	// A deployment id that is no id makes putBack create the old version
	// again (GetDeployment fails, so the old one is not taken as still live).
	return e.putBack(ctx, app, nil, nil, u, depID, false, nil)
}
