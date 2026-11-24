package appbackup

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// BackupOptions are the choices of one backup.
type BackupOptions struct {
	Kind string // "" is KindManual
	Note string // free words the user gives it (at most 200 characters)
}

// volSpec is one volume to copy.
type volSpec struct {
	Role   string
	Path   string // app: container path
	Addon  string // addon: kind
	Volume string // Docker volume name
	Size   int64  // bytes Docker says it uses, -1 unknown
}

func (v volSpec) request(image string) runner.VolumeDataRequest {
	if v.Role == RoleAddon {
		return runner.VolumeDataRequest{App: "", Volume: v.Volume, Addon: v.Addon}
	}
	return runner.VolumeDataRequest{Volume: v.Volume, MountPath: v.Path, Image: image}
}

// appSpec is what a backup or a restore works from.
type appSpec struct {
	App     state.App
	Image   string // image of the app that opens the app's volumes
	Version Version
	Volumes []volSpec
	Running bool // some container of the app is running
}

// BackupPlan is what a backup would do (nothing is touched to make it).
type BackupPlan struct {
	App       string    `json:"app"`
	Kind      string    `json:"kind"`
	Volumes   []PlanVol `json:"volumes"`
	NeedBytes int64     `json:"need_bytes"` // space the backup needs (the data's size, compressing only helps)
	FreeBytes uint64    `json:"free_bytes"` // 0 with FreeKnown false
	FreeKnown bool      `json:"free_known"`
	Enough    bool      `json:"enough"`
	WillStop  bool      `json:"will_stop"` // the app is running and is paused for the copy
	Keep      int       `json:"keep"`      // how many of this kind are kept
	// WillPrune: backups the count rule removes after this one is made
	// (never an automatic one younger than 7 days). WillPruneForSpace:
	// automatic backups removed BEFORE it, oldest first, because the disk has
	// no room otherwise (Enough is then true because of them).
	WillPrune         []string `json:"will_prune,omitempty"`
	WillPruneForSpace []string `json:"will_prune_for_space,omitempty"`
	Say               string   `json:"say"`
}

// PlanVol is one volume in a plan.
type PlanVol struct {
	Role  string `json:"role"`
	Path  string `json:"path,omitempty"`
	Addon string `json:"addon,omitempty"`
	Bytes int64  `json:"bytes"` // -1 unknown
}

// inspect reads what app has and runs: volumes (its own and its add-ons'),
// the version, whether it is running.
func (m *Manager) inspect(ctx context.Context, app string) (appSpec, error) {
	st := m.cfg.Store
	a, err := st.GetApp(ctx, app)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return appSpec{}, fault(CodeNoApp, fmt.Sprintf("应用 %s 没有装在这台机器上", app))
		}
		return appSpec{}, err
	}
	sp := appSpec{App: a, Version: Version{CatalogID: a.CatalogID, CatalogVersion: a.CatalogVersion}}
	vols, err := st.ListVolumes(ctx, app)
	if err != nil {
		return appSpec{}, err
	}
	for _, v := range vols {
		sp.Volumes = append(sp.Volumes, volSpec{Role: RoleApp, Path: v.Path, Volume: v.VolumeName, Size: -1})
	}
	addons, err := st.ListAddons(ctx, app)
	if err != nil {
		return appSpec{}, err
	}
	for _, ad := range addons {
		sp.Volumes = append(sp.Volumes, volSpec{Role: RoleAddon, Addon: ad.Kind, Volume: ad.VolumeName, Size: -1})
	}
	if a.CurrentDeployment != "" {
		if d, err := st.GetDeployment(ctx, a.CurrentDeployment); err == nil {
			sp.Version.DeploymentID, sp.Version.Seq, sp.Image, sp.Version.ImageID = d.ID, d.Seq, d.ImageID, d.ImageID
		}
	}
	if sp.Image == "" {
		// Never live: any image of the app opens a volume.
		if imgs, err := m.cfg.Runner.ListImages(ctx, app); err == nil && len(imgs) > 0 {
			sort.Slice(imgs, func(i, j int) bool { return imgs[i].Created > imgs[j].Created })
			sp.Image = imgs[0].ID
		}
	}
	cs, err := m.cfg.Runner.ListContainers(ctx, app)
	if err != nil {
		return appSpec{}, runnerError(err)
	}
	for _, c := range cs {
		if c.Running && (c.Role == runner.RoleApp || c.Role == runner.RoleAddon) {
			sp.Running = true
		}
	}
	return sp, nil
}

// runnerError says the runner could not be reached (or what it said).
func runnerError(err error) error {
	if errors.Is(err, runner.ErrUnavailable) {
		return fault(CodeExport, "执行器连不上（acornfox-runner 没在运行？）")
	}
	return err
}

// sizes fills in the volumes' sizes (best effort: -1 stays when unknown).
func (m *Manager) sizes(ctx context.Context, app string, vols []volSpec) {
	vd, err := m.volumeData()
	if err != nil {
		return
	}
	got, err := vd.VolumeSizes(ctx, app)
	if err != nil {
		return
	}
	by := map[string]int64{}
	for _, s := range got {
		by[s.Name] = s.Bytes
	}
	for i := range vols {
		if b, ok := by[vols[i].Volume]; ok {
			vols[i].Size = b
		}
	}
}

func sumKnown(vols []volSpec) int64 {
	var n int64
	for _, v := range vols {
		if v.Size > 0 {
			n += v.Size
		}
	}
	return n
}

// needFor is the space a copy of vols needs: their size plus a margin of
// 256 MB or 5%, whichever is more.
func needFor(total int64) int64 {
	margin := total / 20
	if margin < diskMargin {
		margin = diskMargin
	}
	return total + margin
}

func humanBytes(n int64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GB", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	}
	return fmt.Sprintf("%d B", n)
}

// PlanBackup says what a backup of app would do and whether the space is
// there. It touches nothing.
func (m *Manager) PlanBackup(ctx context.Context, app string, o BackupOptions) (BackupPlan, error) {
	kind, err := m.kindOf(o)
	if err != nil {
		return BackupPlan{}, err
	}
	if _, err := m.volumeData(); err != nil {
		return BackupPlan{}, err
	}
	sp, err := m.inspect(ctx, app)
	if err != nil {
		return BackupPlan{}, err
	}
	if len(sp.Volumes) == 0 {
		return BackupPlan{}, fault(CodeNoData, fmt.Sprintf("应用 %s 没有数据卷，没有东西可备份", app))
	}
	m.sizes(ctx, app, sp.Volumes)
	plan := BackupPlan{App: app, Kind: kind, WillStop: sp.Running, Keep: m.Keep(kind), NeedBytes: needFor(sumKnown(sp.Volumes))}
	for _, v := range sp.Volumes {
		plan.Volumes = append(plan.Volumes, PlanVol{Role: v.Role, Path: v.Path, Addon: v.Addon, Bytes: v.Size})
	}
	plan.Enough = true
	if m.cfg.Free != nil {
		if sp, err := m.cfg.Free(ctx); err == nil {
			plan.FreeBytes, plan.FreeKnown = sp.BackupFree, true
			plan.Enough = int64(sp.BackupFree) >= plan.NeedBytes
		}
	}
	if all, err := m.List(app); err == nil {
		plan.WillPrune = m.pruneIDs(all, kind, "", true)
		if !plan.Enough && plan.FreeKnown {
			if ids, _ := pickSpace(all, plan.NeedBytes, plan.FreeBytes, ""); len(ids) > 0 {
				plan.WillPruneForSpace = ids
				plan.Enough = true
			}
		}
	}
	plan.Say = fmt.Sprintf("会把应用 %s 的数据（约 %s）打包存到本机", app, humanBytes(sumKnown(sp.Volumes)))
	if sp.Running {
		plan.Say += "；打包时应用会暂停，打包完自动继续运行"
	}
	if len(plan.WillPruneForSpace) > 0 {
		plan.Say += fmt.Sprintf("。磁盘放不下：需要约 %s，只剩 %s，会先清掉自动另存的旧备份 %s（它们找不回来）", humanBytes(plan.NeedBytes), humanBytes(int64(plan.FreeBytes)), strings.Join(plan.WillPruneForSpace, "、"))
	} else if !plan.Enough {
		plan.Say += fmt.Sprintf("。磁盘空间不够：需要约 %s，只剩 %s", humanBytes(plan.NeedBytes), humanBytes(int64(plan.FreeBytes)))
	}
	if len(plan.WillPrune) > 0 {
		plan.Say += "；做完后会清掉超出份数的旧备份 " + strings.Join(plan.WillPrune, "、")
	}
	return plan, nil
}

// diskText is the refusal when the space is not there.
func (p BackupPlan) diskText() string {
	return fmt.Sprintf("磁盘空间不够，没有动应用 %s：做这份备份需要约 %s，备份目录所在的磁盘只剩 %s", p.App, humanBytes(p.NeedBytes), humanBytes(int64(p.FreeBytes)))
}

func (m *Manager) kindOf(o BackupOptions) (string, error) {
	kind := o.Kind
	if kind == "" {
		kind = KindManual
	}
	if !ValidKind(kind) {
		return "", fault(CodeBadRequest, fmt.Sprintf("备份种类 %q 不对（manual、pre-upgrade、pre-restore）", kind))
	}
	if len([]rune(o.Note)) > 200 {
		return "", fault(CodeBadRequest, "备注太长（最多 200 个字）")
	}
	return kind, nil
}

// StartBackup starts a backup of app as a task and returns it at once
// (POST /v1/apps/{app}/backups). The checks that can refuse it run first,
// so a refusal is an error here and not a failed task.
func (m *Manager) StartBackup(ctx context.Context, app string, o BackupOptions) (TaskView, error) {
	plan, err := m.PlanBackup(ctx, app, o)
	if err != nil {
		return TaskView{}, err
	}
	if !plan.Enough {
		return TaskView{}, fault(CodeDisk, plan.diskText())
	}
	t := m.newTask(app, OpBackup, plan.Kind, backupSteps())
	if err := m.acquire(app, t.v.ID); err != nil {
		m.dropTask(t)
		return TaskView{}, err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		_, err := m.runBackup(m.base, t, app, o)
		m.end(t, err)
	}()
	return t.snapshot(), nil
}

// SnapshotBackup takes a backup of app and returns when it is done. Other
// parts of the program use it (the app upgrade takes its pre-upgrade
// snapshot with it, SnapshotPreUpgrade).
func (m *Manager) SnapshotBackup(ctx context.Context, app string, o BackupOptions) (Meta, error) {
	plan, err := m.PlanBackup(ctx, app, o)
	if err != nil {
		return Meta{}, err
	}
	if !plan.Enough {
		return Meta{}, fault(CodeDisk, plan.diskText())
	}
	t := m.newTask(app, OpBackup, plan.Kind, backupSteps())
	if err := m.acquire(app, t.v.ID); err != nil {
		m.dropTask(t)
		return Meta{}, err
	}
	meta, err := m.runBackup(ctx, t, app, o)
	m.end(t, err)
	return meta, err
}

// SnapshotPreUpgrade is the snapshot an upgrade takes before it changes
// anything: a backup of kind pre-upgrade (one is kept per app). The upgrade
// goes on only when it returns without an error; with BKP-01 (not enough
// room) it must not touch the app.
func (m *Manager) SnapshotPreUpgrade(ctx context.Context, app, note string) (Meta, error) {
	return m.SnapshotBackup(ctx, app, BackupOptions{Kind: KindPreUpgrade, Note: note})
}

// runBackup is the task body of a backup.
func (m *Manager) runBackup(ctx context.Context, t *taskRun, app string, o BackupOptions) (Meta, error) {
	kind, _ := m.kindOf(o)
	t.step("check", StepRunning, "")
	if err := m.ensureDir(app); err != nil {
		return Meta{}, err
	}
	for _, p := range m.removePartials(app) {
		m.opLog(app, "清掉上次留下的半截备份 "+p)
	}
	sp, err := m.inspect(ctx, app)
	if err != nil {
		return Meta{}, err
	}
	if len(sp.Volumes) == 0 {
		return Meta{}, fault(CodeNoData, fmt.Sprintf("应用 %s 没有数据卷，没有东西可备份", app))
	}
	if pend, err := m.cfg.Store.PendingDeployments(ctx, app); err == nil && len(pend) > 0 {
		return Meta{}, fault(CodeBusy, fmt.Sprintf("应用 %s 正在部署中，等它部署完再备份", app))
	}
	t.step("check", StepDone, "")

	// 磁盘放不下时先清自动另存的旧份（计划里写过）；够用就一份都不动
	m.sizes(ctx, app, sp.Volumes)
	evicted := m.evictForSpace(ctx, app, needFor(sumKnown(sp.Volumes)), "")

	var meta Meta
	err = m.withApp(ctx, app, func() error {
		var err error
		meta, _, _, err = m.copyLocked(ctx, t, app, sp, kind, o.Note, false)
		return err
	})
	if err != nil {
		return Meta{}, err
	}
	t.step("prune", StepRunning, "")
	pruned := append(evicted, m.prune(ctx, app, kind, meta.ID)...)
	t.step("prune", StepDone, fmt.Sprintf("清掉 %d 份", len(pruned)))
	t.setResult(func(r *TaskResult) {
		r.BackupID, r.Kind, r.Bytes, r.DowntimeMS, r.Pruned = meta.ID, kind, meta.Bytes, meta.DowntimeMS, pruned
	})
	m.log(ctx, app, fmt.Sprintf("备份完成 %s（%s，%s）", meta.ID, kind, humanBytes(meta.Bytes)))
	return meta, nil
}

// copyLocked stops the app, copies its volumes and the records into a new
// backup and (unless leaveStopped) starts the app again. The caller holds
// the app's lock. It returns the containers it stopped (to start them
// later, when leaveStopped) and the records it saved. The app is started again on every failure
// after it was stopped, except that leaveStopped keeps it stopped for the
// caller to deal with.
func (m *Manager) copyLocked(ctx context.Context, t *taskRun, app string, sp appSpec, kind, note string, leaveStopped bool) (meta Meta, rec Records, stopped []runner.ContainerInfo, err error) {
	vd, err := m.volumeData()
	if err != nil {
		return Meta{}, Records{}, nil, err
	}
	id := m.newID()
	final := m.backupDir(app, id)
	tmp := final + partialExt
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return Meta{}, Records{}, nil, fault(CodeDirNotWritable, fmt.Sprintf("备份目录建不了：%v", err))
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmp)
		}
	}()

	rec, err = m.collectRecords(ctx, app)
	if err != nil {
		return Meta{}, Records{}, nil, err
	}

	t.step("stop", StepRunning, "")
	stopped, err = m.stopApp(ctx, app)
	if err != nil {
		t.step("stop", StepFailed, err.Error())
		return Meta{}, Records{}, nil, err
	}
	began := m.now()
	t.step("stop", StepDone, fmt.Sprintf("停了 %d 个容器", len(stopped)))
	ran := stopped // what to start again (stopped is what the returns give the caller)
	startAgain := func() {
		if len(ran) > 0 && !leaveStopped {
			m.startContainers(ctx, app, ran)
		}
	}
	defer func() {
		if err != nil && len(ran) > 0 {
			// Whatever went wrong, the app is not left stopped by this backup.
			m.startContainers(ctx, app, ran)
		}
	}()

	t.step("pack", StepRunning, "")
	meta = Meta{Format: FormatVersion, ID: id, App: app, Kind: kind, Note: note, CreatedAt: m.now(), Platform: m.cfg.Version, WasRunning: len(ran) > 0, Version: sp.Version}
	for i, v := range sp.Volumes {
		if err := m.cfg.Runner.EnsureVolume(ctx, app, v.Volume); err != nil {
			return Meta{}, Records{}, nil, fault(CodeExport, fmt.Sprintf("数据卷 %s 准备不了：%v", v.Volume, err))
		}
		ve, err := m.exportVolume(ctx, vd, app, sp.Image, v, filepath.Join(tmp, fmt.Sprintf("vol-%d.tar.gz", i)))
		if err != nil {
			t.step("pack", StepFailed, err.Error())
			return Meta{}, Records{}, nil, err
		}
		ve.File = fmt.Sprintf("vol-%d.tar.gz", i)
		meta.Volumes = append(meta.Volumes, ve)
		meta.Bytes += ve.Bytes
		meta.RawBytes += ve.RawBytes
		t.step("pack", StepRunning, fmt.Sprintf("已打包 %d/%d 个数据卷（%s）", i+1, len(sp.Volumes), humanBytes(meta.Bytes)))
	}
	t.step("pack", StepDone, humanBytes(meta.Bytes))

	t.step("start", StepRunning, "")
	startAgain()
	if len(ran) > 0 && !leaveStopped {
		meta.DowntimeMS = m.now().Sub(began).Milliseconds()
	}
	if leaveStopped {
		t.step("start", StepSkipped, "")
	} else {
		t.step("start", StepDone, "")
	}

	t.step("save", StepRunning, "")
	if err := writeJSONFile(filepath.Join(tmp, recordsFile), rec); err != nil {
		return Meta{}, Records{}, nil, fault(CodeDirNotWritable, fmt.Sprintf("写不了备份文件：%v", err))
	}
	meta.Bytes += dirBytesOf(tmp, recordsFile)
	if err := writeJSONFile(filepath.Join(tmp, metaFile), meta); err != nil {
		return Meta{}, Records{}, nil, fault(CodeDirNotWritable, fmt.Sprintf("写不了备份清单：%v", err))
	}
	if err := os.Rename(tmp, final); err != nil {
		return Meta{}, Records{}, nil, fault(CodeDirNotWritable, fmt.Sprintf("备份存不下来：%v", err))
	}
	syncDir(filepath.Dir(final))
	t.step("save", StepDone, "")
	return meta, rec, stopped, nil
}

// dirBytesOf is the size of the file name in dir (0 when unreadable).
func dirBytesOf(dir, name string) int64 {
	st, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return 0
	}
	return st.Size()
}

// syncDir makes the rename durable (best effort).
func syncDir(dir string) {
	if f, err := os.Open(dir); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
}

// exportVolume streams one volume through gzip into path and checks what
// was written (BKP-02 on any failure).
func (m *Manager) exportVolume(ctx context.Context, vd runner.VolumeDataAPI, app, image string, v volSpec, path string) (VolumeEntry, error) {
	req := v.request(image)
	req.App = app
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return VolumeEntry{}, fault(CodeDirNotWritable, fmt.Sprintf("备份文件建不了：%v", err))
	}
	gz, _ := gzip.NewWriterLevel(f, gzip.BestSpeed)
	err = vd.ExportVolume(ctx, req, gz)
	if cerr := gz.Close(); err == nil {
		err = cerr
	}
	if serr := f.Sync(); err == nil {
		err = serr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return VolumeEntry{}, exportFault(v, err)
	}
	sum, raw, entries, err := scanArchive(path)
	if err != nil {
		return VolumeEntry{}, fault(CodeExport, fmt.Sprintf("数据卷 %s 打包出来的文件读不完整：%v", v.Volume, err))
	}
	st, err := os.Stat(path)
	if err != nil {
		return VolumeEntry{}, err
	}
	return VolumeEntry{Role: v.Role, Path: v.Path, Addon: v.Addon, Volume: v.Volume, Bytes: st.Size(), RawBytes: raw, Entries: entries, SHA256: sum}, nil
}

func exportFault(v volSpec, err error) error {
	if errors.Is(err, syscall.ENOSPC) {
		return fault(CodeDisk, fmt.Sprintf("写备份文件时磁盘满了：%v", err))
	}
	var pe *os.PathError
	if errors.As(err, &pe) {
		return fault(CodeDirNotWritable, fmt.Sprintf("写备份文件出错（磁盘满了？）：%v", err))
	}
	if errors.Is(err, io.ErrShortWrite) {
		return fault(CodeDisk, fmt.Sprintf("写备份文件时磁盘满了：%v", err))
	}
	return fault(CodeExport, fmt.Sprintf("读数据卷 %s 出错：%v", v.Volume, err))
}

// stopApp stops the app's running containers (the app's, then the
// add-ons') and returns them.
func (m *Manager) stopApp(ctx context.Context, app string) ([]runner.ContainerInfo, error) {
	cs, err := m.cfg.Runner.ListContainers(ctx, app)
	if err != nil {
		return nil, runnerError(err)
	}
	var run []runner.ContainerInfo
	for _, c := range cs {
		if c.Running && (c.Role == runner.RoleApp || c.Role == runner.RoleAddon) {
			run = append(run, c)
		}
	}
	// App containers first, add-ons (databases) last.
	sort.SliceStable(run, func(i, j int) bool { return run[i].Role == runner.RoleApp && run[j].Role != runner.RoleApp })
	var done []runner.ContainerInfo
	for _, c := range run {
		if err := m.cfg.Runner.StopContainer(ctx, app, c.Name); err != nil {
			m.startContainers(ctx, app, done)
			return nil, fault(CodeExport, fmt.Sprintf("停不了容器 %s：%v", c.Name, err))
		}
		done = append(done, c)
	}
	return done, nil
}

// startContainers starts the containers again: add-ons first, then the app.
// A failure is only logged: the reconciler starts what is not running.
func (m *Manager) startContainers(ctx context.Context, app string, cs []runner.ContainerInfo) {
	order := append([]runner.ContainerInfo(nil), cs...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Role == runner.RoleAddon && order[j].Role != runner.RoleAddon })
	sctx := context.WithoutCancel(ctx)
	for _, c := range order {
		if _, err := m.cfg.Runner.StartContainer(sctx, app, c.Name); err != nil {
			m.cfg.Log.Warn("start container after backup", "app", app, "name", c.Name, "err", err)
		}
	}
	m.kick(app)
}
