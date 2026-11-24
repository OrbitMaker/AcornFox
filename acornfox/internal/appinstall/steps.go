package appinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	neturl "net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/installtiming"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Step names and their labels, in order.
var stepLabels = []struct{ name, label string }{
	{"check", "检查"},
	{"create", "建应用"},
	{"image", "下载镜像"},
	{"files", "写配置"},
	{"start", "启动"},
	{"health", "健康检查"},
	{"init", "初始化"},
}

// Step statuses.
const (
	StepPending = "pending"
	StepRunning = "running"
	StepDone    = "done"
	StepFailed  = "failed"
	StepSkipped = "skipped"
)

// TaskView is an install task as the API returns it. Nothing in it is secret:
// first-login secrets are masked.
type TaskView struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	// Catalog is the catalog the app or recipe came from: "public" or a
	// private catalog id. Tasks recorded before this field have none.
	Catalog string `json:"catalog,omitempty"`
	Status  string `json:"status"` // running | succeeded | failed
	Step    string `json:"step,omitempty"`
	// KeepVolumes: on failure the data volumes this task created were kept
	// (AppResult.Volumes names them).
	KeepVolumes bool         `json:"keep_volumes_on_failure,omitempty"`
	Steps       []StepView   `json:"steps"`
	Apps        []AppResult  `json:"apps"`
	Fault       *Fault       `json:"fault,omitempty"`
	FirstLogin  []FirstLogin `json:"first_login,omitempty"`
	UserSteps   []string     `json:"user_steps,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	FinishedAt  *time.Time   `json:"finished_at,omitempty"`
	// Timing is the verdict on the task's step times by this server's clock
	// (done, slow, stuck, what to say and do next: internal/installtiming).
	// Worked out when the task is read (Task, Tasks); never stored.
	Timing *installtiming.Verdict `json:"timing,omitempty"`
}

// StepView is one step of one app.
type StepView struct {
	App        string     `json:"app"`
	Name       string     `json:"name"`
	Label      string     `json:"label"`
	Status     string     `json:"status"`
	Message    string     `json:"message,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// Progress is set while a running step can measure itself (the image
	// archive downloaded through the download gateway); nil otherwise and
	// once the step is over.
	Progress *StepProgress `json:"progress,omitempty"`
}

// StepProgress is how far a running step is: phase (download | load),
// done_bytes, total_bytes, bytes_per_sec, eta_seconds and, while
// downloading, stalled_seconds (how long no byte has come in).
type StepProgress = installtiming.Progress

// AppResult is one app of a task.
type AppResult struct {
	App       string `json:"app"`
	CatalogID string `json:"catalog_id"`
	Version   string `json:"version"`
	Reused    bool   `json:"reused"`  // already installed, used as is
	Created   bool   `json:"created"` // this task created it (removed again on failure)
	URL       string `json:"url,omitempty"`
	MemoryMB  int    `json:"memory_mb,omitempty"`
	// Internal: the app is reached over the app network only (for other
	// apps); it has no URL and nothing to open.
	Internal bool `json:"internal,omitempty"`
	// What this task made for the app, kept in the task record so a cleanup
	// after a restart still finds it. Only these are removed on failure:
	// never a volume or a credential that existed before.
	Volumes  []string `json:"volumes,omitempty"`   // Docker volumes this task created
	ImageTag string   `json:"image_tag,omitempty"` // acornfox/<app>:<id> tag made for the image
	// ReusedVolumes are old data volumes this install took over (reuse_data);
	// they are never removed by this task.
	ReusedVolumes []string `json:"reused_volumes,omitempty"`
	// What an install on old data (reuse_data) carried over from the old app,
	// see reuse.go. Carried: one line each (parameters, the platform
	// variables the user had changed, the services plugged in again; lines
	// beginning with CAT-INST-35 are credentials not taken back, which the
	// user must hear about). KeptFiles: config files of the old data left as
	// they were. FileBackups: config files written anew because what the
	// platform puts in them changed, and where the old file was copied first.
	Carried []string `json:"carried,omitempty"`
	// Tell: what the providers of the services this app was plugged into
	// wrote for the user (their manifest's provides values written ask), as
	// they are; worked out when the task starts (needs.go, tellOf). They go
	// into the verdict's must_tell once the task has succeeded.
	Tell        []string            `json:"tell,omitempty"`
	KeptFiles   []string            `json:"kept_files,omitempty"`
	FileBackups []runner.FileBackup `json:"file_backups,omitempty"`
	// Services (connect.go): the services this task registered for the app
	// (it provides them) and the needs of the app it plugged in. A failed
	// install takes back the credentials it issued, then removes both.
	Services []string     `json:"services,omitempty"`
	Bindings []BindingRef `json:"bindings,omitempty"`
	// Issuing: the issues this task has started for the app and that have
	// not ended yet, each with its number, recorded before the provider's
	// issue steps run. One still here after the task ended (the server
	// stopped half-way) may have left something in the provider that
	// nothing will take back: the cleanup says so (CAT-INST-35).
	Issuing []IssuingRef `json:"issuing,omitempty"`
	// Replaced: the bindings the app had from before (deleted with its data
	// kept) that this task plugs into something else, moved aside until the
	// task ends: put back on failure, taken back once it has succeeded.
	Replaced []ReplacedRef `json:"replaced,omitempty"`
	// Affected (a reapply task): apps that use a service this app provides
	// whose values this apply changed. They are not changed; their settings
	// are behind now (acornfox apps affected, apps reapply).
	Affected []string `json:"affected_apps,omitempty"`
	// An upgrade or a rollback task: From is the version the app was on
	// before, Version the one it is on (or goes to); SnapshotID the
	// pre-upgrade snapshot taken (upgrade) or used (rollback); Notes what
	// the user is told after it (where the data of the moment was kept).
	From       string   `json:"from,omitempty"`
	SnapshotID string   `json:"snapshot_id,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

// FirstLogin is the first-login information of one app.
type FirstLogin struct {
	App string `json:"app"`
	URL string `json:"url,omitempty"`
	// URLNote is set when URL only opens on the server itself (a loopback
	// address): whoever passes the login on says it to the user as it is
	// (it names no field) and opens the app the way its open_hint says.
	URLNote string       `json:"url_note,omitempty"`
	Fields  []LoginField `json:"fields"`
}

// LoopbackURLNote is FirstLogin.URLNote.
const LoopbackURLNote = "这个地址只有服务器本机能打开；从你自己电脑打开的办法见“怎么打开”"

// loopbackURL reports whether u is an address only the machine itself
// opens: 127.0.0.0/8 or ::1. localhost is left out: a desktop install
// gives it, and there the user sits at that very machine.
func loopbackURL(u string) bool {
	p, err := neturl.Parse(u)
	if err != nil {
		return false
	}
	ip := net.ParseIP(p.Hostname())
	return ip != nil && ip.IsLoopback()
}

// LoginField is one line of first-login information.
type LoginField struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Secret bool   `json:"secret,omitempty"`
}

// Masked is what a secret value shows when not revealed.
const Masked = "******"

// withTiming adds the verdict on v's step times by now (this server's clock).
func withTiming(v TaskView, now time.Time) TaskView {
	t := installtiming.Task{ID: v.ID, Status: v.Status, CreatedAt: v.CreatedAt, FinishedAt: v.FinishedAt, KeepVolumes: v.KeepVolumes, Reapply: v.Kind == KindReapply, Upgrade: v.Kind == KindUpgrade, Rollback: v.Kind == KindRollback}
	if v.Fault != nil {
		t.FaultCode, t.FaultMsg = v.Fault.Code, v.Fault.Message
	}
	for _, s := range v.Steps {
		t.Steps = append(t.Steps, installtiming.Step{App: s.App, Name: s.Name, Label: s.Label, Status: s.Status, Message: s.Message,
			StartedAt: s.StartedAt, FinishedAt: s.FinishedAt, Progress: s.Progress})
	}
	for _, a := range v.Apps {
		t.Apps = append(t.Apps, installtiming.App{Name: a.App, Carried: a.Carried, Tell: a.Tell})
	}
	for _, fl := range v.FirstLogin {
		for _, f := range fl.Fields {
			t.HasSecrets = t.HasSecrets || f.Secret
		}
	}
	vd := installtiming.Assess(t, now, "server")
	v.Timing = &vd
	return v
}

func stepsFor(p *planned) []StepView {
	if p.reuse {
		return []StepView{{App: p.name, Name: "reuse", Label: "复用已装的应用", Status: StepPending}}
	}
	out := make([]StepView, 0, len(stepLabels))
	for _, s := range stepLabels {
		out = append(out, StepView{App: p.name, Name: s.name, Label: s.label, Status: StepPending})
	}
	return out
}

// taskRun is one running task. What it created is recorded in view.Apps
// (Created, Volumes, ImageTag, Services, Bindings, Issuing) and saved with
// every step.
type taskRun struct {
	e    *Engine
	id   string
	mu   sync.Mutex
	view TaskView
}

func (t *taskRun) snapshot() TaskView {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.view
	v.Steps = append([]StepView(nil), t.view.Steps...)
	v.Apps = make([]AppResult, len(t.view.Apps))
	for i, a := range t.view.Apps {
		a.Volumes = append([]string(nil), a.Volumes...)
		a.ReusedVolumes = append([]string(nil), a.ReusedVolumes...)
		a.Carried = append([]string(nil), a.Carried...)
		a.KeptFiles = append([]string(nil), a.KeptFiles...)
		a.FileBackups = append([]runner.FileBackup(nil), a.FileBackups...)
		a.Services = append([]string(nil), a.Services...)
		a.Bindings = append([]BindingRef(nil), a.Bindings...)
		a.Issuing = append([]IssuingRef(nil), a.Issuing...)
		a.Notes = append([]string(nil), a.Notes...)
		v.Apps[i] = a
	}
	return v
}

func (t *taskRun) save(ctx context.Context) {
	v := t.snapshot()
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	if _, err := t.e.cfg.Store.UpdateInstallTask(context.WithoutCancel(ctx), t.id, func(it *state.InstallTask) error {
		it.Data = string(data)
		it.Status = v.Status
		if v.Fault != nil {
			it.FaultCode = v.Fault.Code
		}
		return nil
	}); err != nil {
		t.e.cfg.Log.Error("save install task", "task", t.id, "err", err)
	}
}

// progress updates a running step's message and progress and saves (the
// console and `install status` poll the task).
func (t *taskRun) progress(ctx context.Context, app, name, msg string, pr *StepProgress) {
	t.mu.Lock()
	for i := range t.view.Steps {
		s := &t.view.Steps[i]
		if s.App == app && s.Name == name && s.Status == StepRunning {
			s.Message, s.Progress = msg, pr
		}
	}
	t.mu.Unlock()
	t.save(ctx)
}

// step marks app's step name as status with message and saves.
func (t *taskRun) step(ctx context.Context, app, name, status, msg string) {
	now := time.Now().UTC()
	t.mu.Lock()
	for i := range t.view.Steps {
		s := &t.view.Steps[i]
		if s.App != app || s.Name != name {
			continue
		}
		s.Status = status
		if msg != "" || status != StepRunning {
			s.Message = msg
		}
		if status != StepRunning {
			s.Progress = nil
		}
		switch status {
		case StepRunning:
			s.StartedAt = &now
			t.view.Step = app + "/" + name
		default:
			s.FinishedAt = &now
		}
	}
	t.mu.Unlock()
	t.save(ctx)
}

// noteStep adds note to the message of a step that is over (keeping its
// status) and saves: something found out after the step, shown with it.
func (t *taskRun) noteStep(ctx context.Context, app, name, note string) {
	t.mu.Lock()
	for i := range t.view.Steps {
		s := &t.view.Steps[i]
		if s.App != app || s.Name != name {
			continue
		}
		if s.Message == "" {
			s.Message = note
		} else {
			s.Message += "；" + note
		}
	}
	t.mu.Unlock()
	t.save(ctx)
}

func (t *taskRun) setApp(name string, fn func(*AppResult)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.view.Apps {
		if t.view.Apps[i].App == name {
			fn(&t.view.Apps[i])
		}
	}
}

func (t *taskRun) finish(ctx context.Context, f *Fault) {
	t.mu.Lock()
	if f != nil {
		t.view.Status = state.TaskFailed
		t.view.Fault = f
		for i := range t.view.Steps {
			if t.view.Steps[i].Status == StepPending {
				t.view.Steps[i].Status = StepSkipped
			}
		}
	} else {
		t.view.Status = state.TaskSucceeded
	}
	t.view.Step = ""
	t.mu.Unlock()
	t.save(ctx)
}

func (t *taskRun) run(ctx context.Context, plan []*planned, keepVolumes bool) {
	t.mu.Lock()
	t.view.KeepVolumes = keepVolumes
	t.mu.Unlock()
	var failed *Fault
	for _, p := range plan {
		if p.reuse {
			t.step(ctx, p.name, "reuse", StepDone, "已装，直接用")
			t.addFirstLogin(ctx, p.name)
			continue
		}
		if err := t.installApp(ctx, p); err != nil {
			failed = AsFault(err)
			break
		}
	}
	if failed != nil {
		t.cleanup(ctx, keepVolumes)
	} else {
		// What the apps had from before and do not use any more goes only
		// now: a rollback leaves it as it was. Bindings replaced by this
		// install first, then kept bindings of needs not plugged in.
		for _, p := range plan {
			if !p.reuse {
				t.finishReplaced(ctx, p)
				t.dropStaleBindings(ctx, p)
			}
		}
		// The services the apps were plugged into that are not ready now
		// (their provider's readiness check, ready.go): the user hears it
		// (must_tell); nothing is undone for it.
		for _, p := range plan {
			if !p.reuse {
				t.tellNotReady(ctx, p)
			}
		}
	}
	t.finish(ctx, failed)
}

// fail marks the running step failed and returns f.
func (t *taskRun) fail(ctx context.Context, app, name string, f *Fault) *Fault {
	t.step(ctx, app, name, StepFailed, f.Code+" "+f.Message)
	return f
}

func (t *taskRun) installApp(ctx context.Context, p *planned) error {
	e := t.e
	m := p.app.Manifest
	hooks := e.h()
	if hooks.Reserve == nil || hooks.Delete == nil {
		return fault(CodeInternal, nil, "安装引擎没有接好")
	}
	cat, _ := e.cfg.Runner.(runner.CatalogAPI)

	// check (already done when the task was accepted; the name may have been
	// taken since)
	t.step(ctx, p.name, "check", StepRunning, "")
	if _, err := e.cfg.Store.GetApp(ctx, p.name); err == nil {
		return t.fail(ctx, p.name, "check", fault(CodeNameTaken, nil, "已经有叫 %s 的应用", p.name))
	}
	t.step(ctx, p.name, "check", StepDone, "")

	// create
	t.step(ctx, p.name, "create", StepRunning, "")
	if err := t.create(ctx, p); err != nil {
		// A credential that could not be taken back on the way is for the
		// user to remove: it stays in the task record.
		for _, line := range p.carried {
			if strings.HasPrefix(line, CodeRevoke+" ") {
				t.warnApp(ctx, p.name, line)
			}
		}
		return t.fail(ctx, p.name, "create", AsFault(err))
	}
	if len(p.reusedVolumes) > 0 {
		lines := []string{"接上了旧数据（" + strings.Join(p.reusedVolumes, "、") + "）"}
		if p.kept == nil {
			lines = append(lines, "没有找到删除时保留的设置（不是这台的应用中心删的，或记录丢了）：配置文件沿用旧数据里的，平台变量和服务按这次的")
		}
		p.carried = append(lines, p.carried...)
	}
	t.setApp(p.name, func(r *AppResult) { r.Carried = append([]string(nil), p.carried...) })
	t.step(ctx, p.name, "create", StepDone, strings.Join(p.carried, "；"))

	// image, files and the deployment are serialized with the app's reconcile
	// round, so image GC never sees the image before the deployment refers
	// to it.
	var imageID string
	var dep state.Deployment
	err := hooks.withApp(ctx, p.name, func() error {
		t.step(ctx, p.name, "image", StepRunning, "")
		id, msg, err := t.image(ctx, p, cat)
		if err != nil {
			return t.fail(ctx, p.name, "image", AsFault(err))
		}
		imageID = id
		t.step(ctx, p.name, "image", StepDone, msg)

		t.step(ctx, p.name, "files", StepRunning, "")
		fmsg, err := t.files(ctx, p, cat, imageID)
		if err != nil {
			return t.fail(ctx, p.name, "files", AsFault(err))
		}
		if fmsg == "" {
			t.step(ctx, p.name, "files", StepSkipped, "没有要写的配置文件")
		} else {
			t.step(ctx, p.name, "files", StepDone, fmsg)
		}

		t.step(ctx, p.name, "start", StepRunning, "")
		d, _, err := e.cfg.Store.CreateDeployment(ctx, state.NewDeployment{
			App: p.name, SourceKind: state.SourceImage, SourceRef: imageID, SourceDigest: imageID,
			RequestKey: "catalog-" + t.id,
		})
		if err != nil {
			return t.fail(ctx, p.name, "start", fault(CodeInternal, err, "建部署失败"))
		}
		dep = d
		return nil
	})
	if err != nil {
		return err
	}
	if hooks.Kick != nil {
		hooks.Kick(p.name)
	}
	app, err := t.waitLive(ctx, p.name, dep.ID)
	if err != nil {
		return t.fail(ctx, p.name, "start", AsFault(err))
	}
	url := ""
	if !app.Internal {
		url = e.address(ctx, app)
	}
	t.setApp(p.name, func(r *AppResult) { r.URL = url; r.Internal = app.Internal })
	if app.Internal {
		t.step(ctx, p.name, "start", StepDone, "已上线（只在内部网络，给别的应用用）")
	} else {
		t.step(ctx, p.name, "start", StepDone, "已上线")
	}

	t.step(ctx, p.name, "health", StepRunning, "")
	base, err := t.healthy(ctx, p, app)
	if err != nil {
		return t.fail(ctx, p.name, "health", AsFault(err))
	}
	t.step(ctx, p.name, "health", StepDone, "")

	t.step(ctx, p.name, "init", StepRunning, "")
	if len(m.Init) == 0 {
		t.step(ctx, p.name, "init", StepSkipped, "不需要初始化")
	} else if len(p.reusedVolumes) > 0 && p.restored {
		// The old data was initialized with exactly these (restored)
		// parameters; doing it again fails (a set-up wizard that runs once).
		t.step(ctx, p.name, "init", StepSkipped, "接上了旧数据，不再初始化（账号密码和以前一样）")
	} else {
		err := t.runInit(ctx, p, dep.ID, base)
		var f *Fault
		switch {
		case err == nil:
			t.step(ctx, p.name, "init", StepDone, fmt.Sprintf("%d 步", len(m.Init)))
		case len(p.reusedVolumes) > 0 && errors.As(err, &f) && f.answered:
			// Old data without a kept record (deleted by an older version):
			// the app says no because it was initialized before. Its
			// accounts are the old ones, which the platform does not know.
			p.initStale = true
			t.step(ctx, p.name, "init", StepSkipped, "接上的旧数据已经初始化过（"+f.Message+"），账号密码沿用旧数据里的")
		default:
			return t.fail(ctx, p.name, "init", AsFault(err))
		}
	}
	if err := t.saveSecrets(ctx, p, url); err != nil {
		return fault(CodeInternal, err, "保存首次登录信息失败")
	}
	// What the app provides to other apps (the new way) is registered once
	// it is up and initialized.
	if err := t.registerProvides(ctx, p); err != nil {
		return t.fail(ctx, p.name, "init", AsFault(err))
	}
	// The app has its own saved parameters again: the kept record goes. A
	// fresh install with data volumes passed the old-volumes check, so a
	// kept record of its name is stale (its data is gone).
	if p.reuseData || len(m.Volumes) > 0 {
		e.dropKept(ctx, p.name)
	}
	t.addFirstLogin(ctx, p.name)
	return nil
}

func (h Hooks) withApp(ctx context.Context, app string, fn func() error) error {
	if h.WithApp == nil {
		return fn()
	}
	return h.WithApp(ctx, app, fn)
}

// create reserves memory and creates the app record, then plugs the app's
// needs into their services, and writes env and volumes.
func (t *taskRun) create(ctx context.Context, p *planned) error {
	e, m := t.e, p.app.Manifest
	hooks := e.h()
	mem := m.Resources.MemoryMB.Recommended
	if mem < m.Resources.MemoryMB.Min {
		mem = m.Resources.MemoryMB.Min
	}
	if mem <= 0 {
		mem = state.DefaultMemoryMB
	}
	_, err := hooks.Reserve(ctx, p.name, mem)
	var room *RoomError
	if errors.As(err, &room) && m.Resources.MemoryMB.Min > 0 && m.Resources.MemoryMB.Min < mem {
		mem = m.Resources.MemoryMB.Min
		_, err = hooks.Reserve(ctx, p.name, mem)
	}
	if err != nil {
		if errors.As(err, &room) {
			free := room.CapMB - room.AllocatedMB
			if free < 0 {
				free = 0
			}
			return fault(CodeNoMemory, nil, "%s 至少要 %d MB 内存，整机上限 %d MB 已分配 %d MB，还剩 %d MB", p.name, m.Resources.MemoryMB.Min, room.CapMB, room.AllocatedMB, free)
		}
		if errors.Is(err, state.ErrConflict) && strings.Contains(err.Error(), "exhausted") {
			return fault(CodeNoPort, err, "这台服务器的访问端口用完了")
		}
		if errors.Is(err, state.ErrConflict) {
			return fault(CodeNameTaken, err, "已经有叫 %s 的应用", p.name)
		}
		return fault(CodeInternal, err, "建应用失败")
	}
	t.setApp(p.name, func(r *AppResult) { r.Created = true; r.MemoryMB = mem })
	t.save(ctx)

	// The services the app's needs were matched with: plugged in, the
	// providers' networks joined.
	links, err := t.connectNeeds(ctx, p)
	if err != nil {
		return err
	}

	ep := primaryEndpoint(m)
	exposure := e.manifestExposure(m)
	if p.expose != "" {
		// Chosen in the request (expose.go; never for an internal app).
		exposure = p.expose
	}
	health := "/"
	if ep.Health != nil && ep.Health.Path != "" {
		health = ep.Health.Path
	}
	internal := internalManifest(m)
	if internal {
		// Reached over the app network only: no exposure of its own (see
		// internalnet.go).
		exposure = ""
	}
	saved, err := e.cfg.Store.UpdateApp(ctx, p.name, func(a *state.App) error {
		a.Port = ep.Port
		a.HealthPath = health
		a.Exposure = exposure
		a.Internal = internal
		a.HealthCheck = stateHealthCheck(ep)
		a.Command = m.Container.Command
		a.PidsLimit = m.Container.PidsLimit
		a.Links = links
		a.CatalogID = m.ID
		a.CatalogVersion = m.Release.Version
		a.CatalogSource = p.source
		a.CatalogOrigin = p.app.Origin
		return nil
	})
	if err != nil {
		return fault(CodeInternal, err, "保存应用设置失败")
	}
	// The platform variables now that the app exists, from the provider
	// again: the user's changes (with those restored from old data) go over
	// them, and the variables about the app itself come in (app.url,
	// app.scheme, app.host: the address the app opens at, by its exposure
	// and domains at install time).
	pvals, err := e.platformValues(ctx, e.appContext(ctx, saved, p.vals.platformUser()))
	if err != nil {
		return err
	}
	p.vals[platformKey] = pvals
	if err := t.renderCommand(ctx, p, saved); err != nil {
		return err
	}
	envVals, err := renderEnv(m, p.vals, p.secretRefs())
	if err != nil {
		return err
	}
	for _, ev := range envVals {
		if err := e.cfg.Store.SetEnv(ctx, state.EnvVar{App: p.name, Key: ev.Key, Value: ev.Value, Secret: ev.Secret}); err != nil {
			return fault(CodeInternal, err, "写环境变量 %s 失败", ev.Key)
		}
	}
	old := map[string]bool{}
	if len(m.Volumes) > 0 {
		if !p.reuseData {
			// Checked again here (the request was checked when accepted): a
			// volume left by an earlier app of this name would be picked up
			// by name and then removed by a failed install's cleanup.
			if err := e.checkOldVolumes(ctx, p.name); err != nil {
				return err
			}
		} else {
			vols, err := e.cfg.Runner.ListVolumes(ctx, p.name)
			if err != nil {
				return fault(CodeInternal, err, "读 Docker 数据卷失败")
			}
			for _, v := range vols {
				old[v.Name] = true
			}
		}
	}
	for _, v := range m.Volumes {
		vol, _, err := e.cfg.Store.AddVolume(ctx, p.name, v.Path, false)
		if err != nil {
			return fault(CodeInternal, err, "建数据目录 %s 失败", v.Path)
		}
		if old[vol.VolumeName] {
			// Taken over, not created: a failed install must leave it alone.
			p.reusedVolumes = append(p.reusedVolumes, vol.VolumeName)
			t.setApp(p.name, func(r *AppResult) { r.ReusedVolumes = append(r.ReusedVolumes, vol.VolumeName) })
			t.save(ctx)
			continue
		}
		// Recorded before it is created: removing a volume that was never
		// made is harmless, leaving one behind is not.
		t.setApp(p.name, func(r *AppResult) { r.Volumes = append(r.Volumes, vol.VolumeName) })
		t.save(ctx)
		if err := e.cfg.Runner.EnsureVolume(ctx, p.name, vol.VolumeName); err != nil {
			return fault(CodeInternal, err, "建数据卷失败")
		}
	}
	return nil
}

// checkOldVolumes refuses a name that still has Docker volumes from an app
// deleted earlier with its volumes kept (CAT-INST-24): the new app would
// mount them by name, and its cleanup could remove data the user kept.
func (e *Engine) checkOldVolumes(ctx context.Context, app string) error {
	vols, err := e.cfg.Runner.ListVolumes(ctx, app)
	if err != nil {
		return fault(CodeInternal, err, "读 Docker 数据卷失败")
	}
	if len(vols) == 0 {
		return nil
	}
	names := make([]string, 0, len(vols))
	for _, v := range vols {
		names = append(names, v.Name)
	}
	return fault(CodeOldVolumes, nil, "这台机器上还留着以前叫 %s 的应用的数据卷（%s）", app, strings.Join(names, "、"))
}

func (e *Engine) savedParams(ctx context.Context, app string) (map[string]any, error) {
	cs, err := e.cfg.Store.GetCatalogSecrets(ctx, app)
	if err != nil {
		return nil, err
	}
	plain, err := e.box.open(cs.ParamsEnc)
	if err != nil {
		return nil, err
	}
	var vals map[string]any
	if err := json.Unmarshal(plain, &vals); err != nil {
		return nil, err
	}
	return vals, nil
}

// hostBase is http://127.0.0.1:<port> of the app's live container.
func (e *Engine) hostBase(ctx context.Context, a state.App) (string, error) {
	cs, err := e.cfg.Runner.ListContainers(ctx, a.Name)
	if err != nil {
		return "", err
	}
	for _, c := range cs {
		if c.DeploymentID == a.CurrentDeployment && c.Running && c.HostPort > 0 {
			return "http://127.0.0.1:" + strconv.Itoa(c.HostPort), nil
		}
	}
	return "", errors.New("没有在运行的容器")
}

// image makes the pinned image available as acornfox/<app>:<id> and returns
// its ID: a matching local image, else the gateway archive for this
// machine's architecture (when the manifest names one), else the public
// sources. An image whose archives are all for other architectures (or in
// the older single form, which stands for amd64, on an arm64 machine) is not
// fetched through the gateway: a public app's comes from the public sources
// by digest, a private app's cannot be installed here.
func (t *taskRun) image(ctx context.Context, p *planned, cat runner.CatalogAPI) (string, string, error) {
	e := t.e
	img := p.app.Manifest.Release.Images[0]
	art, haveArt := img.ArtifactFor(e.cfg.Arch)
	tagID := newID()
	tag := runner.ImageTag(p.name, tagID)
	t.setApp(p.name, func(r *AppResult) { r.ImageTag = tag })
	t.save(ctx)
	refs := []string{}
	if img.Tag != "" {
		refs = append(refs, img.Repository+":"+img.Tag)
	}
	refs = append(refs, img.Repository+"@"+img.Digest)
	adopt := func() (string, bool, error) {
		// Arch: a local image with the digest but of another architecture
		// (an amd64 archive an older version loaded on an arm64 machine) is
		// not "already here"; the steps below fetch this machine's one.
		info, err := cat.AdoptImage(ctx, runner.AdoptImageRequest{App: p.name, DeploymentID: tagID, Refs: refs, Digest: img.Digest, Arch: e.cfg.Arch})
		if errors.Is(err, runner.ErrNotFound) {
			return "", false, nil
		}
		if err != nil {
			return "", false, err
		}
		return info.ID, true, nil
	}
	if id, ok, err := adopt(); err != nil {
		return "", "", fault(CodeInternal, err, "查本机镜像失败")
	} else if ok {
		return id, "本机已有这个镜像", nil
	}
	var notes []string
	private := p.app.Origin != ""
	switch {
	case private && !img.NamesArtifact():
		// A private catalog's app is only fetched through the gateway: a
		// manifest without an image archive would make this server ask a
		// public registry for repository@digest, telling a third party the
		// private app's image name. Its author has to add the archive.
		return "", "", fault(CodePrivateDenied, nil, "私有目录 %s 的清单没有写镜像包（artifact），取不到镜像；私有应用的镜像只从下载网关取，不去公共源拉。请给码的人在目录里补上镜像包", p.app.Origin)
	case !img.NamesArtifact():
		// The catalog names no gateway file: the image is a public one,
		// pulled by its pinned digest. The pull reports no progress: say
		// so, or the step looks like it is doing nothing.
		t.progress(ctx, p.name, "image", publicPullNote, nil)
	case private && !t.hasGateway(p):
		// A private catalog's image archive is only handed out by the
		// gateway: asking a public registry for repository@digest would
		// tell a third party the private app's image name.
		return "", "", fault(CodePrivateDenied, nil, "这台服务器关掉了下载网关，取不到私有目录 %s 的镜像包；私有应用的镜像不去公共源找", p.app.Origin)
	case private && img.LegacyArtifact() && img.ArtifactSHA256 == "":
		return "", "", fault(CodePrivateDenied, nil, "私有目录 %s 的清单没有镜像包的校验值，不能下载；私有应用的镜像不去公共源找", p.app.Origin)
	case private && !haveArt:
		// No archive for this machine's architecture: loading another
		// architecture's image would not run, and the public sources are
		// not asked for a private app.
		return "", "", fault(CodePrivateDenied, nil, "私有目录 %s 的应用 %s 没有这台机器能用的镜像包（%s），装不了；私有应用的镜像不去公共源找。请给码的人在目录里补上 linux/%s 的镜像包", p.app.Origin, p.app.Manifest.ID, img.NoArtifactReason(e.cfg.Arch), e.cfg.Arch)
	case !t.hasGateway(p):
		t.progress(ctx, p.name, "image", publicPullNote, nil)
	case !haveArt:
		// Only the catalog's signed checksum may vouch for an archive that
		// is loaded into Docker, and only an archive of this machine's
		// architecture is of use; otherwise the gateway is not used.
		notes = append(notes, "下载网关："+img.NoArtifactReason(e.cfg.Arch)+"，不走网关")
		if img.ArtifactSHA256 != "" || !img.LegacyArtifact() {
			// An architecture without an archive: the pull below can take
			// minutes, say why it is not the (usually faster) gateway.
			t.progress(ctx, p.name, "image", img.NoArtifactReason(e.cfg.Arch)+"，从公共源拉取…", nil)
		}
	default:
		if e.cfg.MirrorProbe != nil && !private {
			// Measure the gateway and the image sources side by side and
			// start with the faster one (pickpath.go).
			if id, msg, err, handled := t.pickSource(ctx, p, cat, art, img, tagID, &notes); handled {
				return id, msg, err
			}
			break
		}
		t.progress(ctx, p.name, "image", "向下载网关要下载地址…", nil)
		id, err := t.fromGateway(ctx, p, cat, art, tagID)
		if err == nil {
			return id, installtiming.GatewayDone, nil
		}
		if private {
			// A private catalog's file is only handed out by the gateway;
			// whatever went wrong (refused, unreachable, retries used up,
			// load failed), it is not looked for on public sources.
			var f *Fault
			if errors.As(err, &f) && (f.Code == CodePrivateDenied || f.Code == CodeImageDigest) {
				return "", "", f
			}
			e.cfg.Log.Warn("gateway image (private)", "app", p.name, "artifact", art.File, "err", err)
			pf := fault(CodePrivateDenied, scrubURL(err), "私有目录 %s 的镜像包没从下载网关取到；私有应用的镜像不去公共源找", p.app.Origin)
			pf.GatewayCode = gatewayclient.CodeOf(err)
			return "", "", pf
		}
		e.cfg.Log.Warn("gateway image", "app", p.name, "artifact", art.File, "err", err)
		notes = append(notes, "下载网关："+err.Error())
		// The download's progress must not stay on screen while the public
		// sources are tried.
		t.progress(ctx, p.name, "image", "下载网关没成功，改从公共源拉取", nil)
	}
	return t.pullPublic(ctx, p, img, tagID, notes)
}

// pullPublic pulls the image by its digest from the public sources through
// the runner and returns its ID. notes are the reasons the download gateway
// was not used (they go into the message).
func (t *taskRun) pullPublic(ctx context.Context, p *planned, img catalog.Image, tagID string, notes []string) (string, string, error) {
	e := t.e
	// The pull does not count toward the task's 60 minutes: it has its own
	// limit (the runner's, and pullStepLimit for its answer; clock.go).
	// Meanwhile the step shows the runner's live status of it.
	resume := pauseClock(ctx)
	pctx, pcancel := context.WithTimeout(ctx, pullStepLimit)
	stopWatch := t.watchPull(pctx, p.name, tagID, imageLabel(img.Repository, img.Tag, img.Digest))
	resp, err := e.cfg.Runner.PullImage(pctx, p.name, tagID, img.Repository+"@"+img.Digest)
	stopWatch()
	pullCtxErr := pctx.Err()
	pcancel()
	resume()
	if err != nil {
		switch {
		case ctx.Err() != nil:
			// The task itself ended (the server is stopping).
			return "", "", fault(CodePull, err, "拉镜像时安装被取消（%s@%s）", img.Repository, shortDigest(img.Digest))
		case pullCtxErr != nil:
			// The runner did not answer within its own limit and the time
			// for the answer (it normally stops the pull at its limit).
			f := fault(CodePull, err, "拉镜像这一步过了 %s 还没结束，运行器也没有回话（一个镜像本来最多拉 %d 分钟）（%s@%s）",
				minutesText(pullStepLimit), int(runner.PullMaxTotal/time.Minute), img.Repository, shortDigest(img.Digest))
			f.Hint = pullNoAnswerHint
			return "", "", f
		}
		return "", "", fault(CodePull, err, "拉镜像失败：%v", err)
	}
	if !resp.OK || resp.Image == nil {
		msg := "拉镜像失败"
		if resp.Failure != nil {
			msg = resp.Failure.Message
		}
		f := fault(CodePull, nil, "%s（%s@%s）", msg, img.Repository, shortDigest(img.Digest))
		if len(notes) > 0 {
			f.Message += "；" + strings.Join(notes, "；")
		}
		if resp.Failure != nil {
			for _, src := range resp.Failure.Sources {
				f.Attempts = append(f.Attempts, catalog.Attempt{Source: src.Host, Code: src.Outcome, Message: src.Message})
			}
			if h := resp.Failure.Hint; h != "" && !strings.Contains(f.Hint, h) {
				f.Hint += "。" + h
			}
		}
		return "", "", f
	}
	msg := "从公共源拉取"
	if resp.Source != "" {
		msg += fmt.Sprintf("（%s，用了 %d 秒）", resp.Source, resp.Seconds)
	}
	if len(notes) > 0 {
		// Say why the gateway was not used: a silent fallback looks like
		// "the gateway is not configured" when it was only unreachable.
		why := strings.Join(notes, "；")
		if r := []rune(why); len(r) > 160 {
			why = string(r[:160]) + "…"
		}
		msg += "（" + why + "）"
	}
	return resp.Image.ID, msg, nil
}

// pullNoAnswerHint is the advice when the runner did not answer a pull in
// time (it stops a pull at its own limit and answers).
const pullNoAnswerHint = "运行器可能卡住或重启过：sudo systemctl status acornfox-runner、journalctl -u acornfox-runner -n 200 看它；好了以后重装。acornfox sources --probe 看每个镜像源现在多快"

// minutesText is a duration in words: "48 秒", "31.5 分钟".
func minutesText(d time.Duration) string {
	sec := d.Seconds()
	if sec < 90 {
		return fmt.Sprintf("%.0f 秒", sec)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", sec/60), ".0") + " 分钟"
}

// publicPullNote is the image step's message when a pull from the public
// sources begins (and the whole time when the runner is too old to say how
// far it is): the runner's live status replaces it (watchPull).
var publicPullNote = fmt.Sprintf("从公共源拉取（一个镜像最多拉 %d 分钟，这段时间不算进整次安装的 %d 分钟）…", int(runner.PullMaxTotal/time.Minute), int(installTaskLimit/time.Minute))

// pullStatusEvery is how often the image step asks the runner how far a pull
// from the public sources is (a variable for tests).
var pullStatusEvery = 3 * time.Second

// watchPull shows the runner's live status of the pull of app's image
// (deployment depID) in the image step until the returned stop is called.
// A runner without the status, or no status yet, leaves the step's message
// as it is.
func (t *taskRun) watchPull(ctx context.Context, app, depID, image string) (stop func()) {
	ps, ok := t.e.cfg.Runner.(runner.PullStatusAPI)
	if !ok {
		return func() {}
	}
	quit, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		tick := time.NewTicker(pullStatusEvery)
		defer tick.Stop()
		for {
			select {
			case <-quit:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				st, err := ps.PullStatus(cctx, app, depID)
				cancel()
				if err != nil {
					continue
				}
				msg, pr := pullStatusText(image, st)
				select {
				case <-quit:
					return
				default:
				}
				t.progress(ctx, app, "image", msg, pr)
			}
		}
	}()
	return func() { close(quit); <-finished }
}

// imageLabel is an image's short name for the user: repository:tag, or the
// repository and the digest's start.
func imageLabel(repo, tag, digest string) string {
	if tag != "" {
		return repo + ":" + tag
	}
	return repo + "@" + shortDigest(digest)
}

// pullStatusText is the image step's message and progress for the runner's
// status st of the pull of image, e.g. "从 docker.m.daocloud.io 拉 demo/app:1.0
// （第 1 个源，共 4 个）：已下 120 MB / 304 MB（39%，最近 2 MB/s），这个镜像已拉 3 分 5 秒".
// It says only what Docker reported: when Docker sends no progress it says
// that how far the pull is cannot be seen.
func pullStatusText(image string, st runner.PullStatus) (string, *StepProgress) {
	el := etaText(int64(st.Seconds))
	if st.Phase == "probe" || st.Source == "" {
		return fmt.Sprintf("从公共源拉 %s：先实测各个镜像源（一般几秒到十几秒），这个镜像已拉 %s", image, el),
			&StepProgress{Phase: "pull", PullSeconds: int64(st.Seconds)}
	}
	pr := &StepProgress{Phase: "pull", DoneBytes: st.Bytes, TotalBytes: st.Total, BytesPerSec: st.BytesPerSec,
		Source: st.Source, Blind: st.Blind, PullSeconds: int64(st.Seconds)}
	where := ""
	if st.Sources > 1 {
		where = fmt.Sprintf("（第 %d 个源，共 %d 个）", st.SourceIndex, st.Sources)
	}
	var how string
	switch {
	case st.Blind:
		pr.BytesPerSec = 0
		how = "Docker 这会儿没报进度，看不到拉到多少"
		if st.Total > 0 {
			how += fmt.Sprintf("（最后报的是 %s / %s）", mbText(st.Bytes), mbText(st.Total))
		}
		how += "；这台机器的网络和磁盘还在动，按还在下处理"
	case st.Total > 0:
		how = fmt.Sprintf("已下 %s / %s（%d%%", mbText(st.Bytes), mbText(st.Total), st.Bytes*100/st.Total)
		if st.BytesPerSec > 0 {
			how += "，最近 " + rateText(st.BytesPerSec)
		} else if st.QuietSeconds >= 10 {
			how += fmt.Sprintf("，%d 秒没有新进度", st.QuietSeconds)
		}
		how += "）"
	default:
		how = "已连上，还没收到进度"
	}
	msg := fmt.Sprintf("从 %s 拉 %s%s：%s，这个镜像已拉 %s", st.Source, image, where, how, el)
	if n := len(st.Earlier); n > 0 {
		before := st.Earlier[n-1]
		if r := []rune(before); len(r) > 120 {
			before = string(r[:120]) + "…"
		}
		if n == 1 {
			msg += "。前一个源：" + before
		} else {
			msg += fmt.Sprintf("。之前换过 %d 个源，上一个：%s", n, before)
		}
	}
	return msg, pr
}

// rateText is a speed: "850 KB/s", "2.3 MB/s".
func rateText(bps int64) string {
	if bps < 1<<20 {
		return fmt.Sprintf("%d KB/s", (bps+512)>>10)
	}
	return fmt.Sprintf("%.1f MB/s", float64(bps)/float64(1<<20))
}

func shortDigest(d string) string {
	if len(d) > 19 {
		return d[:19]
	}
	return d
}

// hasGateway reports whether the gateway can be asked for p's image: the
// catalog's gateway for a private app, the engine's for a public one.
func (t *taskRun) hasGateway(p *planned) bool {
	if p.app.Origin != "" {
		return t.e.cfg.Catalog != nil && t.e.cfg.Catalog.cfg.Gateway != nil
	}
	return t.e.cfg.Gateway != nil
}

// scrubURL drops the address from a *url.Error: a download address issued by
// the gateway is a temporary signed link and must not reach logs, the task
// record or the API.
func scrubURL(err error) error {
	var ue *neturl.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s 请求失败：%w", ue.Op, ue.Err)
	}
	return err
}

// progressInterval is how often a download's progress is saved to the task.
var progressInterval = time.Second

// reportDownload saves the download's progress (done, out of total bytes)
// to the image step every progressInterval until the returned stop is
// called. Speed is the average over the last few seconds. total 0: the size
// the download reports once it knows it (st.total); nothing is saved before
// that (the step still says it is asking the gateway for the address).
func (t *taskRun) reportDownload(ctx context.Context, app string, total int64, st *dlStatus) (stop func()) {
	quit, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		tick := time.NewTicker(progressInterval)
		defer tick.Stop()
		type sample struct {
			at time.Time
			n  int64
		}
		// The bytes already in when the report starts (the gateway's part of
		// the speed test in pickpath.go) are not part of the first speed.
		n0 := st.done.Load()
		window := []sample{{time.Now(), n0}}
		// lastMove: when a byte last came in (stalled_seconds, which tells a
		// stuck download from a slow one).
		lastMove, lastN := time.Now(), n0
		for {
			select {
			case <-quit:
				return
			case <-ctx.Done():
				return
			case now := <-tick.C:
				total := total
				if total <= 0 {
					if total = st.total.Load(); total <= 0 {
						continue
					}
				}
				n := st.done.Load()
				if n != lastN {
					lastMove, lastN = now, n
				}
				window = append(window, sample{now, n})
				if len(window) > 6 {
					window = window[len(window)-6:]
				}
				first := window[0]
				var rate int64
				if dt := now.Sub(first.at).Seconds(); dt > 0 {
					rate = int64(float64(n-first.n) / dt)
				}
				if rate < 0 {
					rate = 0 // a restart from nothing after a failed checksum
				}
				pr := &StepProgress{Phase: "download", DoneBytes: n, TotalBytes: total, BytesPerSec: rate, StalledSeconds: int64(now.Sub(lastMove) / time.Second),
					Conns: int(st.conns.Load())}
				if rate > 0 && total > n {
					pr.ETASeconds = (total - n + rate - 1) / rate
				}
				msg := downloadText(pr)
				if st.phase.Load() == phaseVerify {
					pr.Phase, pr.StalledSeconds, pr.BytesPerSec, pr.ETASeconds = "verify", 0, 0, 0
					t.progress(ctx, app, "image", fmt.Sprintf("已从下载网关下完 %s，正在核对校验值（几秒到几十秒）", mbText(n)), pr)
					continue
				}
				if r := st.resumes.Load(); r > 0 {
					msg += fmt.Sprintf("；中途断开 %d 次，已接着下", r)
				}
				t.progress(ctx, app, "image", msg, pr)
			}
		}
	}()
	return func() { close(quit); <-finished }
}

// downloadText is the image step's message while downloading, e.g.
// "从下载网关下载 312 MB / 1.0 GB（38%，25 MB/s，32 路，还要约 30 秒）".
func downloadText(p *StepProgress) string {
	s := "从下载网关下载 " + mbText(p.DoneBytes)
	if p.TotalBytes > 0 {
		s += " / " + mbText(p.TotalBytes)
	}
	var extra []string
	if p.TotalBytes > 0 {
		extra = append(extra, fmt.Sprintf("%d%%", p.DoneBytes*100/p.TotalBytes))
	}
	if p.BytesPerSec > 0 {
		extra = append(extra, mbText(p.BytesPerSec)+"/s")
	}
	if p.Conns > 1 {
		extra = append(extra, fmt.Sprintf("%d 路", p.Conns))
	}
	if p.ETASeconds > 0 {
		extra = append(extra, "还要约 "+etaText(p.ETASeconds))
	}
	if len(extra) > 0 {
		s += "（" + strings.Join(extra, "，") + "）"
	}
	return s
}

// mbText prints bytes as MB under 1 GB, else GB with one decimal.
func mbText(b int64) string {
	if b < 1<<30 {
		return fmt.Sprintf("%d MB", b>>20)
	}
	return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
}

func etaText(sec int64) string {
	if sec < 60 {
		return fmt.Sprintf("%d 秒", sec)
	}
	return fmt.Sprintf("%d 分 %d 秒", sec/60, sec%60)
}

// files writes the manifest's files into their volumes.
// files writes the manifest's config files into their volumes and returns
// what it says about them ("" when there are none). On a fresh install every
// file is written (overwrite: never keeps one the image already put there).
// Into a volume taken over from an old app (reuse_data) a file that exists
// is kept when what the platform writes into it did not change, and written
// anew after copying the old one aside when it did (see reuse.go).
func (t *taskRun) files(ctx context.Context, p *planned, cat runner.CatalogAPI, imageID string) (string, error) {
	m := p.app.Manifest
	if len(m.Files) == 0 {
		return "", nil
	}
	vols, err := t.e.cfg.Store.ListVolumes(ctx, p.name)
	if err != nil {
		return "", fault(CodeInternal, err, "读数据卷失败")
	}
	type target struct{ volume, mount string }
	byName := map[string]target{}
	for _, mv := range m.Volumes {
		for _, sv := range vols {
			if sv.Path == mv.Path {
				byName[mv.Name] = target{sv.VolumeName, sv.Path}
			}
		}
	}
	reused := map[string]bool{}
	for _, v := range p.reusedVolumes {
		reused[v] = true
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	changes := map[string][]string{} // path -> what changed (a backup was asked for)
	groups := map[target][]runner.VolumeFile{}
	var order []target
	for _, f := range m.Files {
		if f.If != "" {
			ok, err := p.vals.holds(f.If)
			if err != nil {
				return "", renderFault(CodeFiles, err, "配置文件 %s 的 if", f.Path)
			}
			if !ok {
				continue // its condition does not hold: not written
			}
		}
		tg, ok := byName[f.Volume]
		if !ok {
			return "", fault(CodeFiles, nil, "配置文件 %s 指向的数据卷 %s 不存在", f.Path, f.Volume)
		}
		content, err := p.vals.render(f.Template, fileEscape(f.Path))
		if err != nil {
			return "", renderFault(CodeFiles, err, "配置文件 %s 的模板", f.Path)
		}
		vf, err := volumeFile(f, content)
		if err != nil {
			return "", fault(CodeFiles, err, "配置文件 %s：%v", f.Path, err)
		}
		if reused[tg.volume] && vf.Overwrite {
			if p.rewritten[vf.Path] {
				// An earlier install on this data rewrote it, then failed:
				// what is there is that attempt's, not the old app's.
				vf.Backup = vf.Path + ".bak-" + stamp
				changes[vf.Path] = []string{"上次接旧数据没装好时按那次的设置改写过"}
			} else if changed, ok := p.fileChange(f, content); ok && len(changed) > 0 {
				vf.Backup = vf.Path + ".bak-" + stamp
				changes[vf.Path] = changed
			} else {
				vf.Overwrite = false // the old data's own file stays
			}
		}
		if _, seen := groups[tg]; !seen {
			order = append(order, tg)
		}
		groups[tg] = append(groups[tg], vf)
	}
	var res runner.WriteFilesResult
	n := 0
	for _, tg := range order {
		r, err := cat.WriteVolumeFiles(ctx, runner.WriteFilesRequest{App: p.name, Volume: tg.volume, MountPath: tg.mount, Image: imageID, Files: groups[tg]})
		if err != nil {
			return "", fault(CodeFiles, err, "写配置文件失败：%v", err)
		}
		n += len(groups[tg])
		res.Written = append(res.Written, r.Written...)
		res.Kept = append(res.Kept, r.Kept...)
		res.Backups = append(res.Backups, r.Backups...)
	}
	t.setApp(p.name, func(r *AppResult) {
		r.KeptFiles = append([]string(nil), res.Kept...)
		r.FileBackups = append([]runner.FileBackup(nil), res.Backups...)
	})
	return filesMessage(n, len(p.reusedVolumes) > 0, res, changes), nil
}

// filesMessage says what files did. A runner that does not report (an older
// one) leaves every list empty: then only the count is known.
func filesMessage(n int, reuse bool, res runner.WriteFilesResult, changes map[string][]string) string {
	if !reuse || len(res.Written)+len(res.Kept)+len(res.Backups) == 0 {
		return fmt.Sprintf("写了 %d 个文件", n)
	}
	var parts []string
	backedUp := map[string]bool{}
	for _, b := range res.Backups {
		backedUp[b.Path] = true
		parts = append(parts, fmt.Sprintf("%s 里平台填的内容变了（%s），按这次的设置重写了，旧文件另存为 %s", b.Path, strings.Join(changes[b.Path], "、"), b.Backup))
	}
	for _, k := range res.Kept {
		if c, asked := changes[k]; asked {
			parts = append(parts, fmt.Sprintf("%s 里平台填的内容变了（%s），但旧文件不是普通文件或太大、没法另存，没有改它，请自己核对", k, strings.Join(c, "、")))
			continue
		}
		parts = append(parts, "沿用旧的配置文件 "+k)
	}
	fresh := 0
	for _, w := range res.Written {
		if !backedUp[w] {
			fresh++
		}
	}
	if fresh > 0 {
		parts = append(parts, fmt.Sprintf("旧数据里没有的 %d 个按模板写了", fresh))
	}
	return strings.Join(parts, "；")
}

func volumeFile(f catalog.File, content string) (runner.VolumeFile, error) {
	vf := runner.VolumeFile{Path: strings.TrimPrefix(f.Path, "./"), Content: []byte(content), Mode: 0o644, Overwrite: f.Overwrite != "never"}
	if f.Mode != "" {
		mode, err := strconv.ParseUint(f.Mode, 8, 32)
		if err != nil {
			return vf, fmt.Errorf("权限 %s 不对", f.Mode)
		}
		vf.Mode = int(mode)
	}
	if f.Owner != "" {
		u, g, ok := strings.Cut(f.Owner, ":")
		uid, err1 := strconv.Atoi(u)
		gid, err2 := strconv.Atoi(g)
		if !ok || err1 != nil || err2 != nil {
			return vf, fmt.Errorf("属主 %s 不对", f.Owner)
		}
		vf.UID, vf.GID = uid, gid
	}
	return vf, nil
}

// waitLive waits for the deployment to go live.
func (t *taskRun) waitLive(ctx context.Context, app, depID string) (state.App, error) {
	e := t.e
	deadline := time.Now().Add(e.cfg.DeployTimeout)
	for {
		d, err := e.cfg.Store.GetDeployment(ctx, depID)
		if err != nil {
			// The task's limit (or a shutdown) can end the context in the
			// middle of this read: that is the wait being cancelled, not
			// the store failing.
			if ctx.Err() != nil {
				return state.App{}, fault(CodeStart, ctx.Err(), "等 %s 上线时被取消", app)
			}
			return state.App{}, fault(CodeInternal, err, "读部署失败")
		}
		switch d.Status {
		case state.StatusLive:
			a, err := e.cfg.Store.GetApp(ctx, app)
			if err != nil {
				if ctx.Err() != nil {
					return state.App{}, fault(CodeStart, ctx.Err(), "等 %s 上线时被取消", app)
				}
				return state.App{}, fault(CodeInternal, err, "读应用失败")
			}
			return a, nil
		case state.StatusFailed, state.StatusSuperseded, state.StatusRetired:
			f := fault(CodeStart, nil, "%s 没能上线", app)
			if d.Diagnosis != nil {
				f.Message += "：" + d.Diagnosis.Message
				f.Diagnosis = d.Diagnosis
			}
			return state.App{}, f
		}
		if time.Now().After(deadline) {
			return state.App{}, fault(CodeStart, nil, "%s 在 %s 内没上线（状态 %s）", app, e.cfg.DeployTimeout, d.Status)
		}
		select {
		case <-ctx.Done():
			return state.App{}, fault(CodeStart, ctx.Err(), "等 %s 上线时被取消", app)
		case <-time.After(e.cfg.Poll):
		}
	}
}

// healthy checks the primary endpoint's health on the live container, as
// its manifest says (path with expect_json, exec or tcp; see internalnet.go),
// until it passes or timeout_s runs out. It returns the app's base address
// for the init steps sent over HTTP: http://127.0.0.1:<published port>, or
// "" for an app that publishes no port (an internal app).
func (t *taskRun) healthy(ctx context.Context, p *planned, app state.App) (string, error) {
	e := t.e
	ep := primaryEndpoint(p.app.Manifest)
	h := catalog.Health{Path: "/"}
	if ep != nil && ep.Health != nil {
		h = *ep.Health
	}
	timeout := time.Duration(h.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	last := "还没有应答"
	for {
		err := e.checkHealthOnce(ctx, app, ep, h)
		if err == nil {
			if app.Internal {
				return "", nil
			}
			base, err := e.hostBase(ctx, app)
			if err == nil {
				return base, nil
			}
			last = err.Error()
		} else {
			last = err.Error()
			if errors.Is(err, runner.ErrNoExec) {
				return "", fault(CodeInternal, err, "%s 的健康检查要在容器里执行命令：%v，请升级 AcornFox", app.Name, err)
			}
		}
		if time.Now().After(deadline) {
			return "", fault(CodeHealth, nil, "%s 的健康检查 %s 在 %s 内没通过：%s", app.Name, healthWhat(h), timeout, last)
		}
		select {
		case <-ctx.Done():
			return "", fault(CodeHealth, ctx.Err(), "健康检查被取消")
		case <-time.After(e.cfg.Poll):
		}
	}
}

// httpJSON does one request and decodes a JSON answer (nil body when not
// JSON).
func (e *Engine) httpJSON(ctx context.Context, method, url string, body any) (int, any, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.cfg.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var v any
	if json.Unmarshal(raw, &v) != nil {
		v = nil
	}
	return resp.StatusCode, v, nil
}

// runInit runs the after_healthy steps: HTTP calls to the app, commands in
// its own container (the one of its current deployment). Only a call that
// never reached the app (the connection was refused or could not be made;
// a command the runner never took) is retried: an init call such as a
// set-up wizard succeeds only once, so after a timeout or any answer,
// sending it again could turn a done setup into a failure. Any step that
// does not pass fails the install (it is rolled back): an app is never left
// half set up. depID is the deployment that just went live and passed the
// health check: commands run in its container, and only while it is still
// the app's current deployment (otherwise nothing is run).
func (t *taskRun) runInit(ctx context.Context, p *planned, depID, base string) error {
	m := p.app.Manifest
	steps := initActionSteps(m)
	env := ActionEnv{Lookup: p.vals.lookup, SecretRef: p.secretRefs(), Group: GroupInit}
	if stepsRunCommands(steps) {
		a, err := t.e.cfg.Store.GetApp(ctx, p.name)
		if err != nil {
			return fault(CodeInternal, err, "读 %s 的部署失败", p.name)
		}
		if a.CurrentDeployment != depID {
			ae := &ActionError{Kind: ActionNotRunning, Exec: true,
				Detail: "刚通过健康检查的那个部署已经不是这个应用的当前部署了"}
			return fault(CodeInit, nil, "初始化 %s：%s", p.name, ae.account())
		}
		env.Exec = ExecTarget{App: a.Name, Container: runner.ContainerName(a.Name, depID)}
	}
	_, err := t.e.actions().run(ctx, base, steps, env)
	if err != nil {
		return initFault(err)
	}
	return nil
}

// notSent reports whether a request failed before it reached the server.
func notSent(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// saveSecrets keeps the parameters and the first-login fields, encrypted,
// and, in the same step, the manifest the app was installed with (in clear:
// a manifest holds no secrets). The manifest goes first, so an app that has
// saved parameters has its manifest too.
func (t *taskRun) saveSecrets(ctx context.Context, p *planned, url string) error {
	e := t.e
	fields, err := loginFields(p.app.Manifest, p.vals)
	if err != nil {
		return err
	}
	if raw := p.app.Files[manifestFile]; len(raw) > 0 {
		text := string(raw)
		if err := e.cfg.Store.SetCatalogManifest(ctx, state.CatalogManifest{
			App: p.name, CatalogID: p.app.Manifest.ID, CatalogVersion: p.app.Manifest.Release.Version, CatalogOrigin: p.app.Origin,
			Manifest: text, SHA256: state.ManifestSHA256(text), Source: state.ManifestFromInstall,
		}); err != nil {
			return fmt.Errorf("保存清单失败：%w", err)
		}
	}
	saved := map[string]any{}
	for k, v := range p.vals {
		saved[k] = v
	}
	// What the services hand the app is in their bindings, not here; only a
	// fingerprint of each item stays, so that a later change of a service
	// can be told (reapply.go).
	if items, ok := p.vals[usesKey].(map[string]map[string]string); ok && len(items) > 0 {
		saved[usesFPKey] = usesFingerprint(items)
	}
	delete(saved, usesKey)
	delete(saved, usesSecretKey)
	if p.initStale {
		// Init was skipped: what it would have written (an admin password,
		// say) is still the old value in the old data. Do not hand out or
		// keep the new one, which would not work.
		stale := initParams(p.app.Manifest)
		for i, lf := range p.app.Manifest.FirstLogin {
			if name, ok := strings.CutPrefix(lf.From, "params."); ok && stale[strings.SplitN(name, ".", 2)[0]] {
				fields[i] = LoginField{Label: lf.Label, Value: ReusedLoginValue}
			}
		}
		origins := map[string]string{}
		for k, o := range p.origins {
			origins[k] = o
		}
		for k := range stale {
			delete(saved, k)
			origins[k] = OriginOldData
		}
		saved[originsKey] = origins
	}
	pb, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	lb, err := json.Marshal(FirstLogin{App: p.name, URL: url, Fields: fields})
	if err != nil {
		return err
	}
	pe, err := e.box.seal(pb)
	if err != nil {
		return err
	}
	le, err := e.box.seal(lb)
	if err != nil {
		return err
	}
	return e.cfg.Store.SetCatalogSecrets(ctx, state.CatalogSecrets{App: p.name, ParamsEnc: pe, FirstLoginEnc: le})
}

// ReusedLoginValue is the first-login value of a field the old data still
// holds (installed with reuse_data): the platform does not know it.
const ReusedLoginValue = "沿用旧数据里的（重装前的那个）"

// initParams are the parameters the manifest's init steps send to the app
// (and the app then keeps in its data).
func initParams(m *catalog.Manifest) map[string]bool {
	out := map[string]bool{}
	for _, st := range m.Init {
		// What a request sends is its body; what a command is given, all of
		// it (its arguments, env and stdin).
		var sent any = st.HTTP.JSON
		if st.IsExec() {
			sent = stepTexts(st)
		}
		b, _ := json.Marshal(sent)
		for _, r := range catalog.TemplateRefs(string(b)) {
			if name, ok := strings.CutPrefix(r, "params."); ok {
				out[strings.SplitN(name, ".", 2)[0]] = true
			}
		}
	}
	return out
}

func loginFields(m *catalog.Manifest, vals Values) ([]LoginField, error) {
	var out []LoginField
	for _, lf := range m.FirstLogin {
		var v string
		if lf.From != "" {
			s, err := vals.lookup(lf.From)
			if err != nil {
				return nil, err
			}
			v = s
		} else {
			s, err := vals.render(lf.Value, nil)
			if err != nil {
				return nil, renderFault(CodeBadParams, err, "首次登录信息 %s", lf.Label)
			}
			v = s
		}
		out = append(out, LoginField{Label: lf.Label, Value: v, Secret: lf.Secret})
	}
	return out, nil
}

// addFirstLogin adds app's masked first-login fields to the task view.
func (t *taskRun) addFirstLogin(ctx context.Context, app string) {
	fl, err := t.e.FirstLogin(ctx, app, false)
	if err != nil {
		return
	}
	t.mu.Lock()
	t.view.FirstLogin = append(t.view.FirstLogin, fl)
	t.mu.Unlock()
	t.save(ctx)
}

// FirstLogin returns app's first-login fields; secrets are masked unless
// reveal. Only the API server's authenticated entries call it.
func (e *Engine) FirstLogin(ctx context.Context, app string, reveal bool) (FirstLogin, error) {
	if !state.ValidAppName(app) {
		return FirstLogin{}, fault(CodeNotCatalogApp, nil, "应用名不合法")
	}
	cs, err := e.cfg.Store.GetCatalogSecrets(ctx, app)
	if errors.Is(err, state.ErrNotFound) {
		return FirstLogin{}, fault(CodeNotCatalogApp, nil, "%s 不是从应用中心装的，没有首次登录信息", app)
	}
	if err != nil {
		return FirstLogin{}, fault(CodeInternal, err, "读首次登录信息失败")
	}
	plain, err := e.box.open(cs.FirstLoginEnc)
	if err != nil {
		return FirstLogin{}, fault(CodeInternal, err, "解不开首次登录信息（钥匙文件可能换过）")
	}
	var fl FirstLogin
	if err := json.Unmarshal(plain, &fl); err != nil {
		return FirstLogin{}, fault(CodeInternal, err, "首次登录信息格式不对")
	}
	if !reveal {
		for i := range fl.Fields {
			if fl.Fields[i].Secret {
				fl.Fields[i].Value = Masked
			}
		}
	}
	if a, err := e.cfg.Store.GetApp(ctx, app); err == nil {
		fl.URL = e.address(ctx, a)
	}
	fl.URLNote = ""
	if loopbackURL(fl.URL) {
		fl.URLNote = LoopbackURLNote
	}
	// An app without login fields (no web page, e.g. an internal database)
	// answers an empty list, never null: clients walk it without checking.
	if fl.Fields == nil {
		fl.Fields = []LoginField{}
	}
	return fl, nil
}

// cleanup removes what this task created, newest first: the credentials it
// had issued and the services it registered (connect.go), the app
// (containers, network, record; never by volume label), the volumes it
// created unless keepVolumes, and the image tag it made.
func (t *taskRun) cleanup(ctx context.Context, keepVolumes bool) {
	e := t.e
	ctx = context.WithoutCancel(ctx)
	hooks := e.h()
	apps := t.snapshot().Apps
	for i := len(apps) - 1; i >= 0; i-- {
		c := apps[i]
		if !c.Created {
			continue
		}
		t.releaseCreated(ctx, c)
		if hooks.Delete != nil {
			if err := hooks.Delete(ctx, c.App, false); err != nil && !errors.Is(err, state.ErrNotFound) {
				e.cfg.Log.Error("clean up failed install", "app", c.App, "err", err)
			}
		}
		if !keepVolumes {
			for _, v := range c.Volumes {
				if err := e.cfg.Runner.RemoveVolume(ctx, c.App, v); err != nil {
					e.cfg.Log.Error("remove volume of failed install", "app", c.App, "volume", v, "err", err)
				}
			}
		}
		if c.ImageTag != "" {
			_ = e.cfg.Runner.RemoveImage(ctx, c.App, c.ImageTag)
		}
		t.setApp(c.App, func(r *AppResult) {
			r.URL = ""
			if !keepVolumes {
				r.Volumes = nil
			}
		})
	}
}

// renderFault is the fault of a value that could not be worked out:
// CAT-INST-39 when the template, a transform or a condition is what failed
// (catalog.IsRenderError), else code (a reference with no value: the code
// the place always had). what says where; the message never holds a value.
func renderFault(code string, err error, format string, a ...any) *Fault {
	if catalog.IsRenderError(err) {
		code = CodeRender
	}
	return fault(code, err, "%s：%v", fmt.Sprintf(format, a...), err)
}

// renderEnv works out the app's environment variables: each entry whose if
// holds, its from (one reference) or its value (a template, written as it
// renders: no escaping), left out when optional and without a value. Two
// entries of one key that both hold are an error (CAT-INST-39; lint refuses
// what cannot be shown exclusive, CAT-LINT-49). secret says which
// references are secret: an entry made from one is kept secret.
func renderEnv(m *catalog.Manifest, vals Values, secret func(ref string) bool) ([]state.EnvVar, error) {
	var out []state.EnvVar
	seen := map[string]bool{}
	for _, ev := range m.Env {
		if ev.If != "" {
			ok, err := vals.holds(ev.If)
			if err != nil {
				return nil, renderFault(CodeBadParams, err, "环境变量 %s 的 if", ev.Key)
			}
			if !ok {
				continue
			}
		}
		var val string
		isSecret := ev.Secret
		if ev.From != "" {
			v, err := vals.lookup(ev.From)
			if ev.Optional && (errors.Is(err, errNotConnected) || (err == nil && v == "")) {
				continue // no value: the variable is left out
			}
			if err != nil {
				return nil, fault(CodeBadParams, err, "环境变量 %s：%v", ev.Key, err)
			}
			val = v
			isSecret = isSecret || vals.useSecret(ev.From)
		} else {
			v, err := vals.render(ev.Value, nil)
			if ev.Optional && (errors.Is(err, errNotConnected) || (err == nil && v == "")) {
				continue
			}
			if err != nil {
				return nil, renderFault(CodeBadParams, err, "环境变量 %s", ev.Key)
			}
			val = v
			for _, r := range catalog.TemplateRefs(ev.Value) {
				if secret != nil && secret(r) {
					isSecret = true
				}
			}
		}
		if seen[ev.Key] {
			return nil, fault(CodeRender, nil, "环境变量 %s 有两条的 if 同时成立，不知道用哪一条", ev.Key)
		}
		seen[ev.Key] = true
		out = append(out, state.EnvVar{Key: ev.Key, Value: val, Secret: isSecret})
	}
	return out, nil
}

// renderCommand renders the start command, one argument per item, into
// the app's settings (saved holds them as written). No secret may be in it:
// the arguments of a container are visible to every user of the host.
func (t *taskRun) renderCommand(ctx context.Context, p *planned, saved state.App) error {
	m := p.app.Manifest
	cmd, err := renderArgs(m.Container.Command, p.vals, p.secretRefs())
	if err != nil {
		return err
	}
	if slices.Equal(cmd, saved.Command) {
		return nil
	}
	if _, err := t.e.cfg.Store.UpdateApp(ctx, p.name, func(a *state.App) error {
		a.Command = cmd
		return nil
	}); err != nil {
		return fault(CodeInternal, err, "保存应用设置失败")
	}
	return nil
}

// renderArgs renders a command, one argument per item (CAT-LINT-50 refuses
// a secret in it; so does this).
func renderArgs(args []string, vals Values, secret func(ref string) bool) ([]string, error) {
	if len(args) == 0 {
		return args, nil
	}
	out := make([]string, len(args))
	for i, a := range args {
		for _, r := range catalog.TemplateRefs(a) {
			if secret != nil && secret(r) {
				return nil, fault(CodeRender, nil, "启动命令的第 %d 项引用了保密的值 %s：容器的启动参数谁都能看到，保密值要经环境变量给应用", i+1, r)
			}
		}
		v, err := vals.render(a, nil)
		if err != nil {
			return nil, renderFault(CodeBadParams, err, "启动命令的第 %d 项", i+1)
		}
		out[i] = v
	}
	return out, nil
}
