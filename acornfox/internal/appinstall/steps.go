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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
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

// StepProgress is how far a running step is.
type StepProgress struct {
	Phase       string `json:"phase"` // download | load (docker load after the download)
	DoneBytes   int64  `json:"done_bytes"`
	TotalBytes  int64  `json:"total_bytes,omitempty"`
	BytesPerSec int64  `json:"bytes_per_sec,omitempty"`
	ETASeconds  int64  `json:"eta_seconds,omitempty"`
}

// AppResult is one app of a task.
type AppResult struct {
	App       string `json:"app"`
	CatalogID string `json:"catalog_id"`
	Version   string `json:"version"`
	Reused    bool   `json:"reused"`  // already installed, used as is
	Created   bool   `json:"created"` // this task created it (removed again on failure)
	URL       string `json:"url,omitempty"`
	MemoryMB  int    `json:"memory_mb,omitempty"`
	// What this task made for the app, kept in the task record so a cleanup
	// after a restart still finds it. Only these are removed on failure:
	// never a volume or token that existed before.
	Volumes      []string  `json:"volumes,omitempty"`       // Docker volumes this task created
	ImageTag     string    `json:"image_tag,omitempty"`     // acornfox/<app>:<id> tag made for the image
	GatewayToken *TokenRef `json:"gateway_token,omitempty"` // token issued in the local model gateway (id only)
	// ReusedVolumes are old data volumes this install took over (reuse_data);
	// they are never removed by this task.
	ReusedVolumes []string `json:"reused_volumes,omitempty"`
}

// TokenRef names a token issued in a local model gateway app (never its value).
type TokenRef struct {
	Provider string `json:"provider"`
	ID       int    `json:"id"`
}

// FirstLogin is the first-login information of one app.
type FirstLogin struct {
	App    string       `json:"app"`
	URL    string       `json:"url,omitempty"`
	Fields []LoginField `json:"fields"`
}

// LoginField is one line of first-login information.
type LoginField struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Secret bool   `json:"secret,omitempty"`
}

// Masked is what a secret value shows when not revealed.
const Masked = "******"

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
// (Created, Volumes, ImageTag, GatewayToken) and saved with every step.
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
		if a.GatewayToken != nil {
			tok := *a.GatewayToken
			a.GatewayToken = &tok
		}
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
		return t.fail(ctx, p.name, "create", AsFault(err))
	}
	t.step(ctx, p.name, "create", StepDone, "")

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
		n, err := t.files(ctx, p, cat, imageID)
		if err != nil {
			return t.fail(ctx, p.name, "files", AsFault(err))
		}
		if n == 0 {
			t.step(ctx, p.name, "files", StepSkipped, "没有要写的配置文件")
		} else {
			t.step(ctx, p.name, "files", StepDone, fmt.Sprintf("写了 %d 个文件", n))
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
	if hooks.URL != nil {
		url = hooks.URL(ctx, app)
	}
	t.setApp(p.name, func(r *AppResult) { r.URL = url })
	t.step(ctx, p.name, "start", StepDone, "已上线")

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
		// parameters; doing it again fails (New API: "already initialized").
		t.step(ctx, p.name, "init", StepSkipped, "接上了旧数据，不再初始化（账号密码和以前一样）")
	} else {
		err := t.runInit(ctx, p, base)
		var f *Fault
		switch {
		case err == nil:
			msg := fmt.Sprintf("%d 步", len(m.Init))
			if note := t.closeSignUp(ctx, p, base); note != "" {
				msg += "；" + note
			}
			t.step(ctx, p.name, "init", StepDone, msg)
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
	if p.reuseData {
		// The app has its own saved parameters again.
		if err := e.cfg.Store.DeleteKeptCatalogSecrets(ctx, p.name); err != nil {
			e.cfg.Log.Warn("drop kept catalog secrets", "app", p.name, "err", err)
		}
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

// create reserves memory and creates the app record, then env, volumes and
// the model gateway link.
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

	// model gateway link: a token in the provider, the provider's network
	var links []string
	for _, prm := range m.Params {
		c, ok := p.vals[prm.Key].(*LLMChoice)
		if !ok || c.Source != LLMLocalGateway {
			continue
		}
		if err := t.linkGateway(ctx, p, c); err != nil {
			return err
		}
		links = append(links, c.App)
	}

	ep := primaryEndpoint(m)
	exposure := m.Exposure.Default
	if exposure == "" {
		exposure = catalog.DefaultExposure
	}
	health := "/"
	if ep.Health != nil && ep.Health.Path != "" {
		health = ep.Health.Path
	}
	saved, err := e.cfg.Store.UpdateApp(ctx, p.name, func(a *state.App) error {
		a.Port = ep.Port
		a.HealthPath = health
		a.Exposure = exposure
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
	// platform.app.url: the address the app is opened at, by its exposure
	// at install time.
	appURL := ""
	if hooks.URL != nil {
		appURL = hooks.URL(ctx, saved)
	}
	p.vals.setPlatform(catalog.PVAppURL, appURL)
	for _, ev := range m.Env {
		val := ev.Value
		if ev.From != "" {
			v, err := p.vals.lookup(ev.From)
			if err != nil {
				return fault(CodeBadParams, err, "环境变量 %s：%v", ev.Key, err)
			}
			val = v
		}
		if err := e.cfg.Store.SetEnv(ctx, state.EnvVar{App: p.name, Key: ev.Key, Value: val, Secret: ev.Secret}); err != nil {
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

// linkGateway issues a token in the provider app for p and points c at the
// provider's address inside Docker.
func (t *taskRun) linkGateway(ctx context.Context, p *planned, c *LLMChoice) error {
	e := t.e
	prov, err := e.cfg.Store.GetApp(ctx, c.App)
	if err != nil || prov.CurrentDeployment == "" {
		return fault(CodeNoGateway, err, "本机的模型网关 %s 没在运行", c.App)
	}
	pa, err := e.cfg.Catalog.App(ctx, prov.CatalogID)
	if err != nil {
		return err
	}
	var pv *catalog.Provide
	for i := range pa.Manifest.Provides {
		if pa.Manifest.Provides[i].Kind == "llm_gateway" {
			pv = &pa.Manifest.Provides[i]
		}
	}
	if pv == nil || pv.Adapter != "new-api" {
		return fault(CodeNoGateway, nil, "%s 不能当模型网关用", c.App)
	}
	user, pass, err := e.gatewayAdmin(ctx, prov.Name)
	if err != nil {
		return fault(CodeGatewayToken, err, "读不到 %s 的管理员账号", prov.Name)
	}
	base, err := e.hostBase(ctx, prov)
	if err != nil {
		return fault(CodeNoGateway, err, "本机的模型网关 %s 没在运行", prov.Name)
	}
	key, id, err := e.adapter.IssueToken(ctx, base, user, pass, "acornfox-"+p.name)
	if id > 0 {
		t.setApp(p.name, func(r *AppResult) { r.GatewayToken = &TokenRef{Provider: prov.Name, ID: id} })
		t.save(ctx)
	}
	if err != nil {
		return fault(CodeGatewayToken, err, "在 %s 里建令牌失败：%v", prov.Name, err)
	}
	port := prov.Port
	for _, ep := range pa.Manifest.Endpoints {
		if ep.Name == pv.Endpoint {
			port = ep.Port
		}
	}
	c.BaseURL = fmt.Sprintf("http://%s:%d%s", prov.Name, port, pv.BasePath)
	c.APIKey = key
	return nil
}

// gatewayAdmin reads the new-api admin account saved when it was installed
// (its parameters admin_username and admin_password).
func (e *Engine) gatewayAdmin(ctx context.Context, app string) (string, string, error) {
	vals, err := e.savedParams(ctx, app)
	if err != nil {
		return "", "", err
	}
	u, _ := vals["admin_username"].(string)
	pw, _ := vals["admin_password"].(string)
	if u == "" || pw == "" {
		return "", "", errors.New("保存的参数里没有管理员账号")
	}
	return u, pw, nil
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
// its ID: a matching local image, else the gateway archive (when the
// manifest names one), else the public sources.
func (t *taskRun) image(ctx context.Context, p *planned, cat runner.CatalogAPI) (string, string, error) {
	e := t.e
	img := p.app.Manifest.Release.Images[0]
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
		info, err := cat.AdoptImage(ctx, runner.AdoptImageRequest{App: p.name, DeploymentID: tagID, Refs: refs, Digest: img.Digest})
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
	case private && img.Artifact == "":
		// A private catalog's app is only fetched through the gateway: a
		// manifest without an image archive would make this server ask a
		// public registry for repository@digest, telling a third party the
		// private app's image name. Its author has to add the archive.
		return "", "", fault(CodePrivateDenied, nil, "私有目录 %s 的清单没有写镜像包（artifact），取不到镜像；私有应用的镜像只从下载网关取，不去公共源拉。请给码的人在目录里补上镜像包", p.app.Origin)
	case img.Artifact == "":
		// The catalog names no gateway file: the image is a public one,
		// pulled by its pinned digest.
	case private && !t.hasGateway(p):
		// A private catalog's image archive is only handed out by the
		// gateway: asking a public registry for repository@digest would
		// tell a third party the private app's image name.
		return "", "", fault(CodePrivateDenied, nil, "这台服务器关掉了下载网关，取不到私有目录 %s 的镜像包；私有应用的镜像不去公共源找", p.app.Origin)
	case private && img.ArtifactSHA256 == "":
		return "", "", fault(CodePrivateDenied, nil, "私有目录 %s 的清单没有镜像包的校验值，不能下载；私有应用的镜像不去公共源找", p.app.Origin)
	case !t.hasGateway(p):
	case img.ArtifactSHA256 == "":
		// Only the catalog's signed checksum may vouch for an archive that
		// is loaded into Docker; without it the gateway is not used.
		notes = append(notes, "下载网关：清单没有镜像包的校验值，不走网关")
	default:
		t.progress(ctx, p.name, "image", "向下载网关要下载地址…", nil)
		id, err := t.fromGateway(ctx, p, cat, img, tagID)
		if err == nil {
			return id, "从下载网关下载", nil
		}
		if private {
			// A private catalog's file is only handed out by the gateway;
			// whatever went wrong (refused, unreachable, retries used up,
			// load failed), it is not looked for on public sources.
			var f *Fault
			if errors.As(err, &f) && (f.Code == CodePrivateDenied || f.Code == CodeImageDigest) {
				return "", "", f
			}
			e.cfg.Log.Warn("gateway image (private)", "app", p.name, "artifact", img.Artifact, "err", err)
			pf := fault(CodePrivateDenied, scrubURL(err), "私有目录 %s 的镜像包没从下载网关取到；私有应用的镜像不去公共源找", p.app.Origin)
			pf.GatewayCode = gatewayclient.CodeOf(err)
			return "", "", pf
		}
		e.cfg.Log.Warn("gateway image", "app", p.name, "artifact", img.Artifact, "err", err)
		notes = append(notes, "下载网关："+err.Error())
		// The download's progress must not stay on screen while the public
		// sources are tried.
		t.progress(ctx, p.name, "image", "下载网关没成功，改从公共源拉取", nil)
	}
	resp, err := e.cfg.Runner.PullImage(ctx, p.name, tagID, img.Repository+"@"+img.Digest)
	if err != nil {
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
		return "", "", f
	}
	msg := "从公共源拉取"
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

// fromGateway downloads the image archive through the download gateway (W2,
// purpose app), checks it against the size and sha256 the signed catalog
// pins for it (artifact_sha256, artifact_size) before anything is loaded, and
// has the runner load it and tag it acornfox/<app>:<tagID>. The checksum the
// gateway reports is only compared: when it differs from the catalog's, the
// file is not even downloaded. Loaded names other than the app's tag are
// dropped by the runner, so nothing is left behind when a later step fails
// (cleanup removes the app's tag, hence the image).
func (t *taskRun) fromGateway(ctx context.Context, p *planned, cat runner.CatalogAPI, img catalog.Image, tagID string) (string, error) {
	e := t.e
	want := strings.ToLower(img.ArtifactSHA256)
	issue := func(ctx context.Context) (gatewayclient.Issued, error) {
		if p.app.Origin != "" {
			// A private catalog's image: W2 names the catalog and the app.
			iss, err := e.cfg.Catalog.cfg.Gateway.IssueApp(ctx, img.Artifact, gatewayclient.AppRef{Catalog: p.app.Origin, App: p.app.Manifest.ID})
			if err != nil {
				return iss, privateIssueFault(err, "镜像包 "+img.Artifact)
			}
			return iss, nil
		}
		iss, err := e.cfg.Gateway.Issue(ctx, img.Artifact, "app")
		return iss, scrubURL(err)
	}
	// offset is the gateway's clock minus this machine's, as the client
	// learned it; expiry times are turned into this machine's clock with it.
	offset := func() time.Duration {
		var g any = e.cfg.Gateway
		if p.app.Origin != "" {
			g = e.cfg.Catalog.cfg.Gateway
		}
		if c, ok := g.(clockOffsetter); ok {
			return c.ClockOffset()
		}
		return 0
	}
	size := img.ArtifactSize
	// check compares what the gateway reports with the signed catalog.
	check := func(iss gatewayclient.Issued) error {
		if iss.SHA256 != "" && strings.ToLower(iss.SHA256) != want {
			return fault(CodeImageDigest, nil, "下载网关给的 %s 校验值和目录里的不一致，没有下载", img.Artifact)
		}
		if size > 0 && iss.Size > 0 && iss.Size != size {
			return fault(CodeImageDigest, nil, "下载网关给的 %s 大小和目录里的不一致，没有下载", img.Artifact)
		}
		return nil
	}
	iss, err := issue(ctx)
	if err != nil {
		return "", err
	}
	if err := check(iss); err != nil {
		return "", err
	}
	if size <= 0 {
		size = iss.Size
	}
	if e.cfg.UploadDir == "" {
		return "", errors.New("没有下载目录")
	}
	dest := filepath.Join(e.cfg.UploadDir, "catalog-"+t.id+"-"+newID()+".tar")
	defer os.Remove(dest)
	src := dlSource{URL: iss.URL, ExpiresAt: localExpiry(iss.ExpiresAt, offset(), time.Now()), Reissue: func(ctx context.Context) (string, time.Time, error) {
		// The signed address expired or the connection broke: ask the
		// gateway again and resume from the bytes already here.
		again, err := issue(ctx)
		if err != nil {
			return "", time.Time{}, err
		}
		if err := check(again); err != nil {
			return "", time.Time{}, err
		}
		return again.URL, localExpiry(again.ExpiresAt, offset(), time.Now()), nil
	}}
	var st dlStatus
	stop := t.reportDownload(ctx, p.name, size, &st)
	err = downloadChecked(ctx, e.cfg.HTTP, src, dest, want, size, &st)
	stop()
	if err != nil {
		return "", scrubURL(err)
	}
	t.progress(ctx, p.name, "image", fmt.Sprintf("已从下载网关下载 %s，正在导入镜像（大镜像要几十秒）", mbText(size)),
		&StepProgress{Phase: "load", DoneBytes: size, TotalBytes: size})
	res, err := cat.LoadImage(ctx, runner.LoadImageRequest{App: p.name, Path: dest, DeploymentID: tagID})
	if err != nil {
		return "", fmt.Errorf("导入镜像失败：%w", err)
	}
	if res.Image == nil || res.Image.ID == "" {
		return "", errors.New("导入镜像后没有拿到镜像（运行器版本太旧？）")
	}
	return res.Image.ID, nil
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
// called. Speed is the average over the last few seconds.
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
		window := []sample{{time.Now(), 0}}
		for {
			select {
			case <-quit:
				return
			case <-ctx.Done():
				return
			case now := <-tick.C:
				n := st.done.Load()
				window = append(window, sample{now, n})
				if len(window) > 6 {
					window = window[len(window)-6:]
				}
				first := window[0]
				var rate int64
				if dt := now.Sub(first.at).Seconds(); dt > 0 {
					rate = int64(float64(n-first.n) / dt)
				}
				pr := &StepProgress{Phase: "download", DoneBytes: n, TotalBytes: total, BytesPerSec: rate}
				if rate > 0 && total > n {
					pr.ETASeconds = (total - n + rate - 1) / rate
				}
				msg := downloadText(pr)
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
// "从下载网关下载 312 MB / 1.0 GB（38%，25 MB/s，还要约 30 秒）".
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
func (t *taskRun) files(ctx context.Context, p *planned, cat runner.CatalogAPI, imageID string) (int, error) {
	m := p.app.Manifest
	if len(m.Files) == 0 {
		return 0, nil
	}
	vols, err := t.e.cfg.Store.ListVolumes(ctx, p.name)
	if err != nil {
		return 0, fault(CodeInternal, err, "读数据卷失败")
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
	groups := map[target][]runner.VolumeFile{}
	var order []target
	for _, f := range m.Files {
		tg, ok := byName[f.Volume]
		if !ok {
			return 0, fault(CodeFiles, nil, "配置文件 %s 指向的数据卷 %s 不存在", f.Path, f.Volume)
		}
		content, err := p.vals.render(f.Template, fileEscape(f.Path))
		if err != nil {
			return 0, fault(CodeFiles, err, "配置文件 %s 的模板：%v", f.Path, err)
		}
		vf, err := volumeFile(f, content)
		if err != nil {
			return 0, fault(CodeFiles, err, "配置文件 %s：%v", f.Path, err)
		}
		if _, seen := groups[tg]; !seen {
			order = append(order, tg)
		}
		groups[tg] = append(groups[tg], vf)
	}
	n := 0
	for _, tg := range order {
		if err := cat.WriteVolumeFiles(ctx, runner.WriteFilesRequest{App: p.name, Volume: tg.volume, MountPath: tg.mount, Image: imageID, Files: groups[tg]}); err != nil {
			return n, fault(CodeFiles, err, "写配置文件失败：%v", err)
		}
		n += len(groups[tg])
	}
	return n, nil
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
			return state.App{}, fault(CodeInternal, err, "读部署失败")
		}
		switch d.Status {
		case state.StatusLive:
			a, err := e.cfg.Store.GetApp(ctx, app)
			if err != nil {
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

// healthy checks the primary endpoint's health (path, expect_json,
// timeout_s) on the live container and returns its base address.
func (t *taskRun) healthy(ctx context.Context, p *planned, app state.App) (string, error) {
	e := t.e
	ep := primaryEndpoint(p.app.Manifest)
	h := catalog.Health{Path: "/"}
	if ep.Health != nil {
		h = *ep.Health
	}
	timeout := time.Duration(h.TimeoutS) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	last := "还没有应答"
	for {
		base, err := e.hostBase(ctx, app)
		if err == nil {
			code, body, err := e.httpJSON(ctx, http.MethodGet, base+h.Path, nil)
			switch {
			case err != nil:
				last = err.Error()
			case code < 200 || code >= 300:
				last = fmt.Sprintf("HTTP %d", code)
			case len(h.ExpectJSON) > 0 && !matchJSON(h.ExpectJSON, body):
				last = "应答内容和预期不符"
			default:
				return base, nil
			}
		} else {
			last = err.Error()
		}
		if time.Now().After(deadline) {
			return "", fault(CodeHealth, nil, "%s 的健康检查 %s 在 %s 内没通过：%s", app.Name, h.Path, timeout, last)
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

// closeSignUp turns off self-service sign-up in a model gateway app set up
// just now (New API allows anyone who opens its page to register by default).
// Best effort: the install goes on when it fails, and the returned note says
// so in the init step and tells the user where to turn it off by hand.
func (t *taskRun) closeSignUp(ctx context.Context, p *planned, base string) string {
	newAPI := false
	for _, pv := range p.app.Manifest.Provides {
		if pv.Kind == "llm_gateway" && pv.Adapter == "new-api" {
			newAPI = true
		}
	}
	if !newAPI {
		return ""
	}
	user, _ := p.vals["admin_username"].(string)
	pass, _ := p.vals["admin_password"].(string)
	err := errors.New("没有管理员账号")
	if user != "" && pass != "" {
		err = t.e.adapter.DisableRegistration(ctx, base, user, pass)
	}
	if err != nil {
		t.e.cfg.Log.Warn("close sign-up", "app", p.name, "err", scrubURL(err))
		return "没能关闭自助注册（" + scrubURL(err).Error() + "），请登录后在“系统设置”里关掉“允许新用户注册”"
	}
	return "已关闭自助注册，只有管理员账号能登录"
}

// runInit runs the after_healthy HTTP calls. Only a call that never reached
// the app (the connection was refused or could not be made) is retried: an
// init call such as New API's setup succeeds only once, so after a timeout or
// any answer, sending it again could turn a done setup into a failure.
func (t *taskRun) runInit(ctx context.Context, p *planned, base string) error {
	for _, st := range p.app.Manifest.Init {
		if st.When != "" && st.When != "after_healthy" {
			continue
		}
		var body any
		if st.HTTP.JSON != nil {
			b, err := p.vals.renderJSON(map[string]any(st.HTTP.JSON))
			if err != nil {
				return fault(CodeInit, err, "初始化 %s 的模板：%v", st.Name, err)
			}
			body = b
		}
		method := st.HTTP.Method
		if method == "" {
			method = http.MethodPost
		}
		var lastErr string
		ok := false
		for attempt := 0; attempt < 5 && !ok; attempt++ {
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return fault(CodeInit, ctx.Err(), "初始化被取消")
				case <-time.After(2 * time.Second):
				}
			}
			code, got, err := t.e.httpJSON(ctx, method, base+st.HTTP.Path, body)
			if err != nil {
				if notSent(err) {
					lastErr = scrubURL(err).Error()
					continue
				}
				return fault(CodeInit, nil, "初始化 %s 没有收到应答（%v），不再重发：它可能已经生效", st.Name, scrubURL(err))
			}
			if code < 200 || code >= 300 {
				f := fault(CodeInit, nil, "初始化 %s 返回 HTTP %d", st.Name, code)
				f.answered = true
				return f
			}
			if len(st.HTTP.ExpectJSON) > 0 && !matchJSON(st.HTTP.ExpectJSON, got) {
				msg := ""
				if m, ok := got.(map[string]any); ok {
					msg, _ = m["message"].(string)
				}
				f := fault(CodeInit, nil, "初始化 %s 的应答和预期不符：%s", st.Name, msg)
				f.answered = true
				return f
			}
			ok = true
		}
		if !ok {
			return fault(CodeInit, nil, "初始化 %s 失败：%s", st.Name, lastErr)
		}
	}
	return nil
}

// notSent reports whether a request failed before it reached the server.
func notSent(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// savedTokensKey holds, next to the parameters, the gateway tokens issued for
// the app (ids only), so deleting the app revokes them.
const savedTokensKey = "_acornfox_gateway_tokens"

// saveSecrets keeps the parameters and the first-login fields, encrypted.
func (t *taskRun) saveSecrets(ctx context.Context, p *planned, url string) error {
	e := t.e
	fields, err := loginFields(p.app.Manifest, p.vals)
	if err != nil {
		return err
	}
	saved := map[string]any{}
	for k, v := range p.vals {
		saved[k] = v
	}
	if p.initStale {
		// Init was skipped: what it would have written (New API's admin
		// password) is still the old value in the old data. Do not hand out
		// or keep the new one, which would not work.
		stale := initParams(p.app.Manifest)
		for i, lf := range p.app.Manifest.FirstLogin {
			if name, ok := strings.CutPrefix(lf.From, "params."); ok && stale[strings.SplitN(name, ".", 2)[0]] {
				fields[i] = LoginField{Label: lf.Label, Value: ReusedLoginValue}
			}
		}
		for k := range stale {
			delete(saved, k)
		}
	}
	t.mu.Lock()
	for _, a := range t.view.Apps {
		if a.App == p.name && a.GatewayToken != nil {
			saved[savedTokensKey] = []TokenRef{*a.GatewayToken}
		}
	}
	t.mu.Unlock()
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
		b, _ := json.Marshal(st.HTTP.JSON)
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
		v := lf.Value
		if lf.From != "" {
			s, err := vals.lookup(lf.From)
			if err != nil {
				return nil, err
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
		if h := e.h(); h.URL != nil {
			fl.URL = h.URL(ctx, a)
		}
	}
	return fl, nil
}

// cleanup removes what this task created, newest first: the gateway token it
// issued, the app (containers, network, record; never by volume label), the
// volumes it created unless keepVolumes, and the image tag it made.
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
		if c.GatewayToken != nil {
			if err := e.revokeToken(ctx, *c.GatewayToken); err != nil {
				e.cfg.Log.Warn("delete gateway token", "app", c.App, "provider", c.GatewayToken.Provider, "err", err)
			}
		}
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

// revokeToken deletes a token issued in a local model gateway app.
func (e *Engine) revokeToken(ctx context.Context, tok TokenRef) error {
	prov, err := e.cfg.Store.GetApp(ctx, tok.Provider)
	if err != nil {
		return fmt.Errorf("模型网关 %s 不在了：%w", tok.Provider, err)
	}
	base, err := e.hostBase(ctx, prov)
	if err != nil {
		return fmt.Errorf("模型网关 %s 没在运行：%w", tok.Provider, err)
	}
	u, pw, err := e.gatewayAdmin(ctx, prov.Name)
	if err != nil {
		return err
	}
	return e.adapter.DeleteToken(ctx, base, u, pw, tok.ID)
}

// GatewayTokens lists the tokens issued in local model gateways for app when
// it was installed (ids only). An app not installed from the catalog, or
// without such tokens, has none. The API server reads them before deleting
// app and revokes them with RevokeGatewayTokens once it is gone.
func (e *Engine) GatewayTokens(ctx context.Context, app string) ([]TokenRef, error) {
	vals, err := e.savedParams(ctx, app)
	if errors.Is(err, state.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读 %s 保存的参数失败：%w", app, err)
	}
	raw, ok := vals[savedTokensKey]
	if !ok {
		return nil, nil
	}
	b, _ := json.Marshal(raw)
	var toks []TokenRef
	if err := json.Unmarshal(b, &toks); err != nil {
		return nil, fmt.Errorf("%s 保存的令牌记录格式不对：%w", app, err)
	}
	return toks, nil
}

// RevokeGatewayTokens deletes app's gateway tokens. The error names those
// that could not be revoked (the gateway is stopped, say): they keep working
// until removed in the gateway by hand.
func (e *Engine) RevokeGatewayTokens(ctx context.Context, app string, toks []TokenRef) error {
	var failed []string
	for _, tok := range toks {
		if err := e.revokeToken(ctx, tok); err != nil {
			e.cfg.Log.Warn("revoke gateway token", "app", app, "provider", tok.Provider, "id", tok.ID, "err", err)
			failed = append(failed, fmt.Sprintf("%s 里的令牌 acornfox-%s（编号 %d）", tok.Provider, app, tok.ID))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("没能吊销 %s，请在模型网关的“令牌”页手动删掉", strings.Join(failed, "、"))
	}
	return nil
}
