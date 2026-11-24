package appbackup

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// ReasonRestore is the reason a restore's deployment is recorded with.
const ReasonRestore = state.ReasonRestore

// RestoreOptions are the choices of one restore.
type RestoreOptions struct {
	// Confirm is the user's yes to overwriting the current data (BKP-06
	// without it).
	Confirm bool
	// AllowCurrentVersion goes on with the version the app runs now when the
	// program of the backup's version is gone from this machine (BKP-09
	// without it).
	AllowCurrentVersion bool
}

// RestorePlan is what a restore would do (nothing is touched to make it).
type RestorePlan struct {
	App    string `json:"app"`
	Backup Meta   `json:"backup"`
	// Intact: the files are all there with the right sizes. Verified is true
	// when the sha256 of every file was checked too (the restore itself
	// always does).
	Intact   bool `json:"intact"`
	Verified bool `json:"verified"`

	VersionNow       string `json:"version_now"`
	VersionThen      string `json:"version_then"`
	ChangesVersion   bool   `json:"changes_version"`
	VersionAvailable bool   `json:"version_available"` // the backup's program is on this machine
	NeedsConfirm     bool   `json:"needs_confirm"`
	BackupNeedBytes  int64  `json:"backup_need_bytes"` // for the backup of the current data taken first
	DockerNeedBytes  int64  `json:"docker_need_bytes"`
	Enough           bool   `json:"enough"`
	WillStop         bool   `json:"will_stop"`
	CurrentHasData   bool   `json:"current_has_data"`
	// WillPrune: backups the count rule removes after the restore (never an
	// automatic one younger than 7 days). WillPruneForSpace: automatic
	// backups removed before the restore takes its pre-restore backup,
	// because the backup directory has no room otherwise (never the backup
	// being restored).
	WillPrune         []string `json:"will_prune,omitempty"`
	WillPruneForSpace []string `json:"will_prune_for_space,omitempty"`
	Say               string   `json:"say"`
}

func versionText(v Version) string {
	switch {
	case v.CatalogVersion != "" && v.Seq > 0:
		return fmt.Sprintf("%s（第 %d 版）", v.CatalogVersion, v.Seq)
	case v.CatalogVersion != "":
		return v.CatalogVersion
	case v.Seq > 0:
		return fmt.Sprintf("第 %d 版", v.Seq)
	}
	return "还没上线过"
}

// readRecords reads records.json of a backup.
func (m *Manager) readRecords(app, id string) (Records, error) {
	var rec Records
	b, err := os.ReadFile(filepath.Join(m.backupDir(app, id), recordsFile))
	if err != nil {
		return rec, fault(CodeCorrupt, fmt.Sprintf("备份 %s 缺少 %s：%v", id, recordsFile, err))
	}
	if err := json.Unmarshal(b, &rec); err != nil || rec.Format != FormatVersion {
		return rec, fault(CodeCorrupt, fmt.Sprintf("备份 %s 的 %s 读不了", id, recordsFile))
	}
	return rec, nil
}

// imagePresent reports whether the app has image id on this machine.
func (m *Manager) imagePresent(ctx context.Context, app, id string) bool {
	if id == "" {
		return false
	}
	imgs, err := m.cfg.Runner.ListImages(ctx, app)
	if err != nil {
		return false
	}
	for _, im := range imgs {
		if im.ID == id {
			return true
		}
	}
	return false
}

// PlanRestore says what restoring backup id into app would do and whether
// the space is there. verify also checks every file's sha256 (reads them
// all). It touches nothing.
func (m *Manager) PlanRestore(ctx context.Context, app, id string, verify bool) (RestorePlan, error) {
	if _, err := m.volumeData(); err != nil {
		return RestorePlan{}, err
	}
	meta, err := m.Get(app, id)
	if err != nil {
		return RestorePlan{}, err
	}
	sp, err := m.inspect(ctx, app)
	if err != nil {
		return RestorePlan{}, err
	}
	plan := RestorePlan{App: app, Backup: meta, NeedsConfirm: true, WillStop: sp.Running,
		VersionNow: versionText(sp.Version), VersionThen: versionText(meta.Version), CurrentHasData: len(sp.Volumes) > 0}
	plan.Intact = true
	for _, v := range meta.Volumes {
		st, err := os.Stat(filepath.Join(m.backupDir(app, id), v.File))
		if err != nil || st.Size() != v.Bytes {
			plan.Intact = false
		}
	}
	if verify {
		plan.Verified = true
		if err := m.verify(app, meta); err != nil {
			plan.Intact = false
		}
	}
	plan.VersionAvailable = meta.Version.ImageID == "" || meta.Version.ImageID == sp.Version.ImageID || m.imagePresent(ctx, app, meta.Version.ImageID)
	plan.ChangesVersion = meta.Version.ImageID != "" && meta.Version.ImageID != sp.Version.ImageID && plan.VersionAvailable

	m.sizes(ctx, app, sp.Volumes)
	plan.BackupNeedBytes = needFor(sumKnown(sp.Volumes))
	plan.DockerNeedBytes = needFor(meta.RawBytes)
	plan.Enough = true
	all, _ := m.List(app)
	if plan.CurrentHasData {
		plan.WillPrune = m.pruneIDs(all, KindPreRestore, "", true)
	}
	if m.cfg.Free != nil {
		if free, err := m.cfg.Free(ctx); err == nil {
			if plan.CurrentHasData && int64(free.BackupFree) < plan.BackupNeedBytes {
				if ids, _ := pickSpace(all, plan.BackupNeedBytes, free.BackupFree, id); len(ids) > 0 {
					plan.WillPruneForSpace = ids
				} else {
					plan.Enough = false
				}
			}
			if free.DockerFree > 0 && int64(free.DockerFree) < plan.DockerNeedBytes {
				plan.Enough = false
			}
		}
	}
	plan.Say = fmt.Sprintf("会用 %s 的备份 %s 里的数据覆盖应用 %s 现在的数据", meta.CreatedAt.Local().Format("2006-01-02 15:04"), meta.ID, app)
	if plan.CurrentHasData {
		plan.Say += "；覆盖之前会先把现在的数据另存一份（恢复前备份），恢复失败会自动退回"
	}
	if plan.ChangesVersion {
		plan.Say += fmt.Sprintf("；程序版本也会从 %s 换回 %s", plan.VersionNow, plan.VersionThen)
	}
	if len(plan.WillPruneForSpace) > 0 {
		plan.Say += fmt.Sprintf("；备份目录放不下恢复前的备份，会先清掉自动另存的旧备份 %s（它们找不回来）", strings.Join(plan.WillPruneForSpace, "、"))
	}
	if len(plan.WillPrune) > 0 {
		plan.Say += "；做完后会清掉超出份数的旧恢复前备份 " + strings.Join(plan.WillPrune, "、")
	}
	plan.Say += "；恢复期间应用会停一阵，备份之后新写的数据会没有"
	return plan, nil
}

// StartRestore starts a restore as a task and returns it at once (POST
// /v1/apps/{app}/backups/{id}/restore). What can be refused is refused here
// (BKP-06 without a yes, BKP-01, BKP-05, BKP-09 …) with nothing touched.
func (m *Manager) StartRestore(ctx context.Context, app, id string, o RestoreOptions) (TaskView, error) {
	if err := m.preflightRestore(ctx, app, id, o); err != nil {
		return TaskView{}, err
	}
	t := m.newTask(app, OpRestore, "", restoreSteps())
	if err := m.acquire(app, t.v.ID); err != nil {
		m.dropTask(t)
		return TaskView{}, err
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.end(t, m.runRestore(m.base, t, app, id, o))
	}()
	return t.snapshot(), nil
}

// RestoreSnapshot restores backup id into app and returns when it is done:
// the call the app upgrade makes to go back to its pre-upgrade snapshot
// (the user's yes is the upgrade's own, Confirm is implied). The task
// result says what happened; an error is a *Error with the BKP code.
func (m *Manager) RestoreSnapshot(ctx context.Context, app, id string, o RestoreOptions) (TaskResult, error) {
	o.Confirm = true
	if err := m.preflightRestore(ctx, app, id, o); err != nil {
		return TaskResult{}, err
	}
	t := m.newTask(app, OpRestore, "", restoreSteps())
	if err := m.acquire(app, t.v.ID); err != nil {
		m.dropTask(t)
		return TaskResult{}, err
	}
	err := m.runRestore(ctx, t, app, id, o)
	m.end(t, err)
	v := t.snapshot()
	var res TaskResult
	if v.Result != nil {
		res = *v.Result
	}
	return res, err
}

// preflightRestore is every refusal that needs nothing to be touched.
func (m *Manager) preflightRestore(ctx context.Context, app, id string, o RestoreOptions) error {
	if !o.Confirm {
		return fault(CodeConfirm, "恢复会用备份里的数据覆盖应用现在的数据，需要用户先同意（加 --yes）")
	}
	plan, err := m.PlanRestore(ctx, app, id, false)
	if err != nil {
		return err
	}
	if !plan.Intact {
		return fault(CodeCorrupt, fmt.Sprintf("备份 %s 的文件不齐或大小不对，不能用来恢复", id))
	}
	if !plan.VersionAvailable && !o.AllowCurrentVersion {
		return fault(CodeVersionGone, fmt.Sprintf("备份时应用的版本（%s）的程序已经不在这台机器上了；要在现在的版本（%s）上用这份数据，加 --allow-current-version", plan.VersionThen, plan.VersionNow))
	}
	if !plan.Enough {
		return fault(CodeDisk, fmt.Sprintf("磁盘空间不够，没有动应用：恢复前备份约需 %s，恢复约需 %s 的数据空间", humanBytes(plan.BackupNeedBytes), humanBytes(plan.DockerNeedBytes)))
	}
	return nil
}

// runRestore is the task body of a restore.
func (m *Manager) runRestore(ctx context.Context, t *taskRun, app, id string, o RestoreOptions) error {
	t.step("check", StepRunning, "")
	meta, err := m.Get(app, id)
	if err != nil {
		return err
	}
	if err := m.verify(app, meta); err != nil {
		return err
	}
	rec, err := m.readRecords(app, id)
	if err != nil {
		return err
	}
	sp, err := m.inspect(ctx, app)
	if err != nil {
		return err
	}
	if pend, err := m.cfg.Store.PendingDeployments(ctx, app); err == nil && len(pend) > 0 {
		return fault(CodeBusy, fmt.Sprintf("应用 %s 正在部署中，等它部署完再恢复", app))
	}
	if err := m.ensureDir(app); err != nil {
		return err
	}
	target := meta.Version.ImageID
	if target != "" && target != sp.Version.ImageID && !m.imagePresent(ctx, app, target) {
		if !o.AllowCurrentVersion {
			return fault(CodeVersionGone, "备份时应用的版本的程序已不在这台机器上")
		}
		target = sp.Version.ImageID
	}
	t.step("check", StepDone, "")
	t.setResult(func(r *TaskResult) { r.BackupID = id })

	var pre *Meta
	var preRec Records
	touched := false // the app was stopped for the snapshot, or its data replaced
	wantRunning := sp.App.Desired == state.DesiredRunning
	var depID string
	var evicted []string // automatic backups removed first for lack of room
	err = m.withApp(ctx, app, func() error {
		// 1. the current data first.
		if len(sp.Volumes) > 0 && sp.Image != "" {
			t.step("snapshot", StepRunning, "")
			m.sizes(ctx, app, sp.Volumes)
			evicted = m.evictForSpace(ctx, app, needFor(sumKnown(sp.Volumes)), id)
			if len(evicted) > 0 {
				t.setResult(func(r *TaskResult) { r.Pruned = evicted })
			}
			note := fmt.Sprintf("恢复 %s 之前自动备份", id)
			b, r, _, err := m.copyLocked(ctx, t, app, sp, KindPreRestore, note, true)
			if err != nil {
				t.step("snapshot", StepFailed, err.Error())
				return err
			}
			pre, preRec = &b, r
			touched = true
			t.setResult(func(r *TaskResult) { r.PreRestoreID = b.ID })
			t.step("snapshot", StepDone, b.ID)
		} else {
			t.step("snapshot", StepSkipped, "现在没有数据可备份")
		}
		// 2. the data of the backup.
		touched = true
		t.step("swap", StepRunning, "")
		if err := m.swapLocked(ctx, app, meta, sp, target); err != nil {
			t.step("swap", StepFailed, err.Error())
			return err
		}
		t.step("swap", StepDone, "")
		// 3. the records.
		t.step("records", StepRunning, "")
		warnings, err := m.applyRecords(ctx, app, rec)
		if err != nil {
			t.step("records", StepFailed, err.Error())
			return err
		}
		t.setResult(func(r *TaskResult) { r.Warnings = append(r.Warnings, warnings...) })
		t.step("records", StepDone, "")
		// 4. start the version of the backup.
		if target != "" && (wantRunning || target != sp.Version.ImageID) {
			depID, err = m.deployImage(ctx, app, target, meta.Version.Seq, "restore-"+t.v.ID)
			if err != nil {
				return err
			}
		}
		m.kick(app)
		return nil
	})
	if err == nil && depID != "" {
		t.step("deploy", StepRunning, "")
		err = m.waitLive(ctx, depID)
		if err != nil {
			t.step("deploy", StepFailed, err.Error())
		}
	}
	if err != nil {
		if !touched {
			return err // refused or failed before anything changed
		}
		return m.rollbackRestore(ctx, t, app, id, err, pre, preRec, sp, wantRunning)
	}
	t.step("deploy", StepDone, versionText(meta.Version))
	t.step("verify", StepRunning, "")
	res := t.snapshot().Result
	n := 0
	if res != nil {
		n = len(res.Warnings)
	}
	t.step("verify", StepDone, fmt.Sprintf("%d 条要留意的", n))
	pruned := append(evicted, m.prune(ctx, app, KindPreRestore, "")...)
	t.setResult(func(r *TaskResult) { r.Version = versionText(meta.Version); r.Pruned = pruned })
	m.log(ctx, app, fmt.Sprintf("已恢复备份 %s（恢复前的备份 %s）", id, t.snapshot().Result.PreRestoreID))
	return nil
}

// rollbackRestore puts the data of the pre-restore backup back after a
// restore failed with cause, and says which of BKP-07 and BKP-08 it is.
func (m *Manager) rollbackRestore(ctx context.Context, t *taskRun, app, id string, cause error, pre *Meta, preRec Records, sp appSpec, wantRunning bool) error {
	msg := cause.Error()
	if e, ok := asFault(cause); ok && e.Code != "" {
		msg = e.Code + "：" + e.Message
	}
	m.log(ctx, app, fmt.Sprintf("恢复备份 %s 没成功（%s）", id, msg))
	if pre == nil {
		return fault(CodeRollbackFailed, fmt.Sprintf("恢复没成功（%s），而且恢复前没能做备份，没法自动退回", msg))
	}
	t.addStep(Step{Key: "rollback", Text: "自动退回到恢复之前", Status: StepRunning})
	target := pre.Version.ImageID
	var depID string
	err := m.withApp(ctx, app, func() error {
		if err := m.swapLocked(ctx, app, *pre, sp, target); err != nil {
			return err
		}
		if _, err := m.applyRecords(ctx, app, preRec); err != nil {
			return err
		}
		if target != "" && (wantRunning || target != sp.Version.ImageID) {
			var err error
			depID, err = m.deployImage(ctx, app, target, pre.Version.Seq, "rollback-"+t.v.ID)
			if err != nil {
				return err
			}
		}
		m.kick(app)
		return nil
	})
	if err == nil && depID != "" {
		err = m.waitLive(ctx, depID)
	}
	if err != nil {
		t.step("rollback", StepFailed, err.Error())
		m.log(ctx, app, fmt.Sprintf("自动退回也没成功：%v；恢复前的备份 %s 还在", err, pre.ID))
		return fault(CodeRollbackFailed, fmt.Sprintf("恢复没成功（%s），自动退回也没成功（%v）。恢复之前的数据还在备份 %s 里，先别动这个应用", msg, err, pre.ID))
	}
	t.step("rollback", StepDone, "")
	t.setResult(func(r *TaskResult) { r.RolledBack = true })
	m.log(ctx, app, "已自动退回到恢复之前的数据")
	return fault(CodeRolledBack, fmt.Sprintf("恢复没成功（%s），数据已经退回到恢复之前的样子", msg))
}

// swapLocked replaces the app's volumes with the files of backup b: the
// containers go, then each volume is removed, made again and filled from its
// file. The caller holds the app's lock.
func (m *Manager) swapLocked(ctx context.Context, app string, b Meta, sp appSpec, target string) error {
	vd, err := m.volumeData()
	if err != nil {
		return err
	}
	cs, err := m.cfg.Runner.ListContainers(ctx, app)
	if err != nil {
		return runnerError(err)
	}
	for _, c := range cs {
		if err := m.cfg.Runner.RemoveContainer(ctx, app, c.Name); err != nil {
			return fault(CodeExport, fmt.Sprintf("删不掉容器 %s：%v", c.Name, err))
		}
	}
	image := sp.Image
	if target != "" && m.imagePresent(ctx, app, target) {
		image = target
	}
	if image == "" {
		return fault(CodeNoData, "应用还没有可用的程序镜像，打不开数据卷")
	}
	cur, err := m.cfg.Store.ListVolumes(ctx, app)
	if err != nil {
		return err
	}
	byPath := map[string]string{}
	for _, v := range cur {
		byPath[v.Path] = v.VolumeName
	}
	for _, ve := range b.Volumes {
		req := runner.VolumeDataRequest{App: app}
		switch ve.Role {
		case RoleAddon:
			req.Addon, req.Volume = ve.Addon, runner.AddonVolumeName(app, ve.Addon)
		default:
			name, ok := byPath[ve.Path]
			if !ok {
				v, _, err := m.cfg.Store.AddVolume(ctx, app, ve.Path, false)
				if err != nil {
					return fmt.Errorf("数据目录 %s：%w", ve.Path, err)
				}
				name = v.VolumeName
			}
			req.Volume, req.MountPath, req.Image = name, ve.Path, image
		}
		if err := m.cfg.Runner.RemoveVolume(ctx, app, req.Volume); err != nil {
			return fault(CodeExport, fmt.Sprintf("清不掉数据卷 %s：%v", req.Volume, err))
		}
		if err := m.cfg.Runner.EnsureVolume(ctx, app, req.Volume); err != nil {
			return fault(CodeExport, fmt.Sprintf("建不了数据卷 %s：%v", req.Volume, err))
		}
		if err := m.importFile(ctx, vd, req, filepath.Join(m.backupDir(app, b.ID), ve.File)); err != nil {
			return fault(CodeExport, fmt.Sprintf("把备份写回数据卷 %s 出错：%v", req.Volume, err))
		}
	}
	return nil
}

// importFile streams a volume file (tar.gz) into the volume.
func (m *Manager) importFile(ctx context.Context, vd runner.VolumeDataAPI, req runner.VolumeDataRequest, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return vd.ImportVolume(ctx, req, gz)
}

// deployImage makes the deployment that starts image (the reconciler then
// runs its health check like any deployment's).
func (m *Manager) deployImage(ctx context.Context, app, image string, basedOn int, key string) (string, error) {
	origin := state.SourceImage
	if a, err := m.cfg.Store.GetApp(ctx, app); err == nil && a.CurrentDeployment != "" {
		if d, err := m.cfg.Store.GetDeployment(ctx, a.CurrentDeployment); err == nil {
			if d.OriginKind != "" {
				origin = d.OriginKind
			} else if d.SourceKind != "" {
				origin = d.SourceKind
			}
		}
	}
	dep, _, err := m.cfg.Store.CreateDeployment(ctx, state.NewDeployment{
		App: app, SourceKind: state.SourceImage, SourceRef: image, SourceDigest: image,
		BypassDedup: true, RequestKey: key, BasedOnSeq: basedOn, OriginKind: origin, Reason: ReasonRestore,
	})
	if err != nil {
		return "", fmt.Errorf("建部署：%w", err)
	}
	return dep.ID, nil
}

// waitLive waits for the deployment to go live; failed, superseded or the
// time running out is an error.
func (m *Manager) waitLive(ctx context.Context, depID string) error {
	deadline := m.now().Add(m.cfg.HealthWait)
	for {
		d, err := m.cfg.Store.GetDeployment(ctx, depID)
		if err != nil {
			return fmt.Errorf("读部署状态：%w", err)
		}
		switch d.Status {
		case state.StatusLive, state.StatusRetired:
			return nil
		case state.StatusFailed:
			if d.Diagnosis != nil {
				return fmt.Errorf("应用没能起来：%s（%s）", d.Diagnosis.Message, d.Diagnosis.Code)
			}
			return errors.New("应用没能起来")
		case state.StatusSuperseded:
			return errors.New("部署被别的部署取代了")
		}
		if !m.now().Before(deadline) {
			return fmt.Errorf("等了 %s，应用还没就绪", m.cfg.HealthWait.Round(time.Second))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.cfg.Poll):
		}
	}
}
