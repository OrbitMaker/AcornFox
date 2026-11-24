package appinstall

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/appbackup"
	"github.com/acornfox/acornfox/internal/state"
)

// Upgrading an app from the app center and going back (upgrade.go), with a
// fake world and a fake of the app backups: what the engine decides and
// writes. The backups themselves (the files, the restore) are tested in
// internal/appbackup and on a machine.

// fakeSnaps is the app backups as the upgrade sees them.
type fakeSnaps struct {
	mu          sync.Mutex
	st          *state.Store
	n           int
	latest      map[string]appbackup.Meta
	taken       []string // app of each snapshot taken
	restored    []string // id of each snapshot restored
	noData      bool     // the app has no data directory
	notEnough   bool     // the disk cannot hold a snapshot
	failTake    error
	failRestore error
	afterTake   func()
}

func newFakeSnaps(st *state.Store) *fakeSnaps {
	return &fakeSnaps{st: st, latest: map[string]appbackup.Meta{}}
}

func (f *fakeSnaps) PlanBackup(ctx context.Context, app string, o appbackup.BackupOptions) (appbackup.BackupPlan, error) {
	if f.noData {
		return appbackup.BackupPlan{}, appbackup.Fault(appbackup.CodeNoData, "没有数据卷")
	}
	free := uint64(10 << 30)
	if f.notEnough {
		free = 1 << 20
	}
	return appbackup.BackupPlan{App: app, Kind: o.Kind, Volumes: []appbackup.PlanVol{{Role: "app", Path: "/data", Bytes: 5 << 20}},
		NeedBytes: 6 << 20, FreeBytes: free, FreeKnown: true, Enough: !f.notEnough}, nil
}

func (f *fakeSnaps) SnapshotPreUpgrade(ctx context.Context, app, note string) (appbackup.Meta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failTake != nil {
		return appbackup.Meta{}, f.failTake
	}
	a, err := f.st.GetApp(ctx, app)
	if err != nil {
		return appbackup.Meta{}, err
	}
	f.n++
	m := appbackup.Meta{Format: 1, ID: fmt.Sprintf("20261124-1200%02d-ab%02d", f.n, f.n), App: app, Kind: appbackup.KindPreUpgrade, Note: note,
		CreatedAt: a.UpdatedAt, Bytes: 3 << 20, Version: appbackup.Version{CatalogID: a.CatalogID, CatalogVersion: a.CatalogVersion, ImageID: "sha256:old"}}
	f.latest[app] = m
	f.taken = append(f.taken, app)
	if f.afterTake != nil {
		f.afterTake()
	}
	return m, nil
}

func (f *fakeSnaps) Latest(app, kind string) (appbackup.Meta, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.latest[app]
	return m, ok, nil
}

func (f *fakeSnaps) PlanRestore(ctx context.Context, app, id string, verify bool) (appbackup.RestorePlan, error) {
	return appbackup.RestorePlan{App: app, Intact: true, VersionAvailable: true, Enough: true}, nil
}

func (f *fakeSnaps) RestoreSnapshot(ctx context.Context, app, id string, o appbackup.RestoreOptions) (appbackup.TaskResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.restored = append(f.restored, id)
	if f.failRestore != nil {
		return appbackup.TaskResult{}, f.failRestore
	}
	return appbackup.TaskResult{BackupID: id, PreRestoreID: "20261124-130000-cd01"}, nil
}

// upgradeWorld is a reapply world with a fake of the backups and a way to
// publish a new version of the catalog.
type upgradeWorld struct {
	*reapplyWorld
	snaps *fakeSnaps
}

func upgradeSetup(t *testing.T) *upgradeWorld {
	t.Helper()
	r := reapplySetup(t, reapplyManifest)
	u := &upgradeWorld{reapplyWorld: r, snaps: newFakeSnaps(r.w.st)}
	r.e.SetSnapshots(u.snaps)
	return u
}

// publish makes manifest the catalog's version of its app from now on.
func (u *upgradeWorld) publish(t *testing.T, manifest string) {
	t.Helper()
	b := typedBundle(t, goodDir, nil, manifest)
	u.e.cfg.Catalog = NewCatalog(CatalogConfig{CacheDir: filepath.Join(t.TempDir(), "catalog"), Snapshot: bundleFS(b), Sources: []string{"http://127.0.0.1:1"}})
}

// v2 is reapplyManifest as its next version: a new image, a setting and an
// environment variable that follows it.
func v2(extra ...[2]string) string {
	m := reapplyManifest
	m = strings.Replace(m, "version: v1.0.0", "version: v1.1.0", 1)
	m = strings.Replace(m, "tag: v1.0.0", "tag: v1.1.0", 1)
	m = strings.Replace(m, "5555555555555555555555555555555555555555555555555555555555555555", "6666666666666666666666666666666666666666666666666666666666666666", 1)
	m = strings.Replace(m, "  - { key: pw, type: secret, label: 口令, generate: { length: 20 } }\n", "  - { key: pw, type: secret, label: 口令, generate: { length: 20 } }\n  - { key: extra, type: string, label: 额外设置, default: more }\n", 1)
	m = strings.Replace(m, "  - { key: FAST_ONLY, value: \"1\", if: \"params.mode == fast\" }\n", "  - { key: FAST_ONLY, value: \"1\", if: \"params.mode == fast\" }\n  - { key: EXTRA, value: \"{{params.extra}}\" }\n", 1)
	for _, e := range extra {
		if !strings.Contains(m, e[0]) {
			panic("v2: " + e[0])
		}
		m = strings.Replace(m, e[0], e[1], 1)
	}
	return m
}

func TestAppVersionNewer(t *testing.T) {
	for _, c := range []struct {
		have, cand string
		want       bool
	}{
		{"1.0.0", "1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"1.9.0", "1.10.0", true},
		{"1.10.0", "1.9.0", false},
		{"2.47.5", "2.47.5", false},
		{"v2.47.5", "2.47.5", false},
		{"1.2.0-rc.1", "1.2.0", true},
		{"1.2.0", "1.2.0-rc.1", false},
		{"1.2.0-rc.1", "1.2.0-rc.2", true},
		{"1.2", "1.2.1", true},
		{"1.2.0+build5", "1.2.0", false},
		{"2025.11.3", "2025.12.1", true},
		{"1.0.0", "2", true},
	} {
		if got := appVersionNewer(c.have, c.cand); got != c.want {
			t.Errorf("appVersionNewer(%q, %q) = %v, want %v", c.have, c.cand, got, c.want)
		}
	}
}

// W9-1: a newer version in the catalog shows on the app; one this machine
// cannot install is not offered (and says why); it is told once.
func TestUpgradeInfoAndNotice(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.mustInstall(t, Request{App: "demo-conf"})
	a, _ := u.w.st.GetApp(ctx, "demo-conf")
	if in := u.e.UpgradeInfos(ctx, []state.App{a}, false)["demo-conf"]; in == nil || in.Available != "" || in.Snapshot != nil || in.CanUpgrade {
		t.Fatalf("nothing newer yet: %+v", in)
	}
	u.publish(t, v2())
	in := u.e.UpgradeInfos(ctx, []state.App{a}, true)["demo-conf"]
	if in.Available != "v1.1.0" || !in.CanUpgrade || in.Blocked != "" || in.CanRollback {
		t.Fatalf("info %+v", in)
	}
	if got := u.e.Outdated(ctx); len(got) != 1 || got[0].App != "demo-conf" || got[0].Available != "v1.1.0" || got[0].Current != "v1.0.0" {
		t.Fatalf("outdated %+v", got)
	}
	// told once
	u.e.noteOutdated(ctx)
	u.e.noteOutdated(ctx)
	if evs, _ := u.w.st.ListEvents(ctx, "demo-conf", "", 0, 50); countStage(evs, "upgrade") != 1 {
		t.Fatalf("events %+v", evs)
	}
	// a newer one is told again
	u.publish(t, strings.Replace(v2(), "v1.1.0", "v1.2.0", 2))
	u.e.noteOutdated(ctx)
	if evs, _ := u.w.st.ListEvents(ctx, "demo-conf", "", 0, 50); countStage(evs, "upgrade") != 2 {
		t.Fatalf("events after a newer one %+v", evs)
	}
	// this AcornFox is too old for it: not offered, said why
	old := strings.Replace(v2(), `min_platform: "0.7.0"`, `min_platform: "0.9.0"`, 1)
	u.publish(t, old)
	in = u.e.UpgradeInfos(ctx, []state.App{a}, false)["demo-conf"]
	if in.Available != "" || in.CanUpgrade || in.BlockedCode != CodeTooOld || !strings.Contains(in.Blocked, "0.9.0") {
		t.Fatalf("too old: %+v", in)
	}
	if len(u.e.Outdated(ctx)) != 0 {
		t.Fatal("an app that cannot be upgraded counted as outdated")
	}
	// another CPU
	arm := strings.Replace(v2(), "platforms: [linux/amd64]", "platforms: [linux/arm64]", 1)
	u.publish(t, arm)
	in = u.e.UpgradeInfos(ctx, []state.App{a}, false)["demo-conf"]
	if in.Available != "" || in.BlockedCode != CodeArch {
		t.Fatalf("arch: %+v", in)
	}
}

func countStage(evs []state.Event, stage string) int {
	n := 0
	for _, e := range evs {
		if e.Stage == stage {
			n++
		}
	}
	return n
}

// W9-2: the plan says what would happen and touches nothing; without the
// user's yes nothing is done; with it the snapshot is taken, the new image
// is fetched, the new manifest and version are what the app has, the values
// the user set are kept, and the snapshot can be gone back to (W9-3).
func TestUpgradeAndRollback(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.mustInstall(t, Request{App: "demo-conf"})
	// the user's own setting, and a file the manifest wrote at install only
	u.apply(t, ReapplyRequest{App: "demo-conf", Params: map[string]any{"title": "我的标题"}})
	before := u.currentDeployment(t, "demo-conf")
	u.publish(t, v2())
	u.x.reqs = nil
	u.w.pulled = nil

	plan, err := u.e.UpgradePlan(ctx, UpgradeRequest{App: "demo-conf"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CanUpgrade || plan.From != "v1.0.0" || plan.To != "v1.1.0" || !plan.Restart || !plan.Snapshot || plan.SnapshotMB != 5 || !plan.DiskOK {
		t.Fatalf("plan %+v", plan)
	}
	if len(plan.VarsAdded) != 1 || !strings.Contains(plan.VarsAdded[0], "extra") || len(plan.VarsRemoved) != 0 {
		t.Fatalf("vars %v %v", plan.VarsAdded, plan.VarsRemoved)
	}
	if plan.Settings == nil || !contains1(plan.Settings.Env, "EXTRA") || !strings.Contains(plan.Summary, "v1.0.0 升到 v1.1.0") {
		t.Fatalf("settings %+v summary %q", plan.Settings, plan.Summary)
	}
	if len(u.snaps.taken) != 0 || len(u.w.pulled) != 0 || envMap(t, u.env, "demo-conf")["TITLE"] != "我的标题" {
		t.Fatalf("the plan changed something: %v %v", u.snaps.taken, u.w.pulled)
	}
	// no yes: refused, with the plan
	st, err := u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf"})
	if code(err) != CodeUpgConfirm || st.Plan.To != "v1.1.0" || len(u.snaps.taken) != 0 {
		t.Fatalf("without confirm: %v %+v", err, st.Plan)
	}
	// the only version there is
	if _, err := u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf", To: "v9.9.9", ConfirmRestart: true}); code(err) != CodeUpgCannot {
		t.Fatalf("another version: %v", err)
	}

	st, err = u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf", ConfirmRestart: true})
	if err != nil || st.Task == nil || st.Task.Kind != KindUpgrade {
		t.Fatalf("start: %v %+v", err, st.Task)
	}
	tv := u.wait(t, st.Task.ID)
	if tv.Status != state.TaskSucceeded || tv.Kind != KindUpgrade {
		t.Fatalf("task %+v %+v", tv.Fault, tv.Steps)
	}
	if len(tv.Apps) != 1 || tv.Apps[0].Version != "v1.1.0" || tv.Apps[0].From != "v1.0.0" || tv.Apps[0].SnapshotID == "" {
		t.Fatalf("task apps %+v", tv.Apps)
	}
	if got := stepNames(tv); got != "check,image,snapshot,files,start,health,init,save,revert" {
		t.Fatalf("steps %s", got)
	}
	a, _ := u.w.st.GetApp(ctx, "demo-conf")
	if a.CatalogVersion != "v1.1.0" {
		t.Fatalf("version %s", a.CatalogVersion)
	}
	cm, err := u.w.st.GetCatalogManifest(ctx, "demo-conf")
	if err != nil || cm.CatalogVersion != "v1.1.0" || !strings.Contains(cm.Manifest, "key: EXTRA") {
		t.Fatalf("saved manifest %v %+v", err, cm.CatalogVersion)
	}
	env := envMap(t, u.env, "demo-conf")
	if env["TITLE"] != "我的标题" || env["EXTRA"] != "more" {
		t.Fatalf("env %v", env)
	}
	if got := u.volFileOf(t, "demo-conf", "app.conf"); got != "title=我的标题\ntz=UTC\n" {
		t.Fatalf("app.conf %q", got)
	}
	if !contains1(u.w.pulled, "example/demo-conf@sha256:"+strings.Repeat("6", 64)) {
		t.Fatalf("the new image was not fetched: %v", u.w.pulled)
	}
	after := u.currentDeployment(t, "demo-conf")
	if after.ID == before.ID || after.Seq != before.Seq+1 {
		t.Fatalf("deployments %+v -> %+v", before, after)
	}
	if len(u.snaps.taken) != 1 || len(u.snaps.restored) != 0 {
		t.Fatalf("snapshots taken %v restored %v", u.snaps.taken, u.snaps.restored)
	}
	// the manifest's every-apply step ran once; the first-install one did not
	if cmds := u.cmds(); len(cmds) != 1 || cmds[0] != "demo-conf sync" {
		t.Fatalf("commands %v", cmds)
	}
	if evs, _ := u.w.st.ListEvents(ctx, "demo-conf", "", 0, 50); countStage(evs, "upgrade") != 1 || !strings.Contains(evs[len(evs)-1].Message, "已升级到 v1.1.0") {
		t.Fatalf("events %+v", evs)
	}
	if len(u.e.Outdated(ctx)) != 0 {
		t.Fatal("still outdated after the upgrade")
	}
	// no journal is left
	if saved, _ := u.e.savedParams(ctx, "demo-conf"); saved[undoKey] != nil {
		t.Fatalf("journal left: %v", saved[undoKey])
	}

	// W9-3: the snapshot is there to go back to
	in := u.e.UpgradeInfos(ctx, []state.App{a}, true)["demo-conf"]
	if in.Snapshot == nil || in.Snapshot.Version != "v1.0.0" || !in.CanRollback || in.Available != "" {
		t.Fatalf("info after: %+v", in)
	}
	rp, err := u.e.PlanRollback(ctx, "demo-conf")
	if err != nil || !rp.CanRollback || rp.From != "v1.1.0" || rp.To != "v1.0.0" || !rp.LosesData || rp.SnapshotID != tv.Apps[0].SnapshotID {
		t.Fatalf("rollback plan %v %+v", err, rp)
	}
	rs, err := u.e.StartRollback(ctx, RollbackRequest{App: "demo-conf"})
	if code(err) != CodeRollbackConfirm || rs.Plan.App != "demo-conf" || len(u.snaps.restored) != 0 {
		t.Fatalf("rollback without confirm: %v", err)
	}
	rs, err = u.e.StartRollback(ctx, RollbackRequest{App: "demo-conf", Confirm: true})
	if err != nil || rs.Task == nil || rs.Task.Kind != KindRollback {
		t.Fatalf("rollback: %v", err)
	}
	rv := u.wait(t, rs.Task.ID)
	if rv.Status != state.TaskSucceeded || len(u.snaps.restored) != 1 || u.snaps.restored[0] != tv.Apps[0].SnapshotID {
		t.Fatalf("rollback task %+v restored %v", rv.Fault, u.snaps.restored)
	}
	if len(rv.Apps[0].Notes) == 0 || !strings.Contains(rv.Apps[0].Notes[0], "20261124-130000-cd01") {
		t.Fatalf("notes %v", rv.Apps[0].Notes)
	}
}

func stepNames(tv TaskView) string {
	var names []string
	for _, s := range tv.Steps {
		names = append(names, s.Name)
	}
	return strings.Join(names, ",")
}

// W9-3: an upgrade that does not come up goes back to the snapshot, and the
// task says so.
func TestUpgradeFailureGoesBackToTheSnapshot(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.mustInstall(t, Request{App: "demo-conf"})
	u.publish(t, v2())
	u.healthy.Store(false) // the new version never gets healthy
	st, err := u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf", ConfirmRestart: true})
	if err != nil {
		t.Fatal(err)
	}
	tv := u.wait(t, st.Task.ID)
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeUpgFailed || !strings.Contains(tv.Fault.Message, CodeHealth) {
		t.Fatalf("task %+v", tv.Fault)
	}
	if len(u.snaps.taken) != 1 || len(u.snaps.restored) != 1 || u.snaps.restored[0] != u.snaps.latest["demo-conf"].ID {
		t.Fatalf("snapshots taken %v restored %v", u.snaps.taken, u.snaps.restored)
	}
	// the snapshot's restore is the way back: the journal is gone
	if saved, _ := u.e.savedParams(ctx, "demo-conf"); saved[undoKey] != nil {
		t.Fatalf("journal left: %v", saved[undoKey])
	}
	if len(tv.Apps) != 1 || tv.Apps[0].Version != "v1.0.0" {
		t.Fatalf("apps %+v", tv.Apps)
	}
	// the new version is still on offer
	if got := u.e.Outdated(ctx); len(got) != 1 || got[0].Available != "v1.1.0" {
		t.Fatalf("outdated after a failed upgrade: %+v", got)
	}
}

// When the snapshot cannot be restored the settings and the program are put
// back one by one, and the task says the data may have been changed
// (APP-UPG-06).
func TestUpgradeFailureWithoutARestorableSnapshot(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.mustInstall(t, Request{App: "demo-conf"})
	u.publish(t, v2())
	u.healthy.Store(false)
	u.snaps.failRestore = appbackup.Fault(appbackup.CodeDisk, "磁盘满了")
	st, err := u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf", ConfirmRestart: true})
	if err != nil {
		t.Fatal(err)
	}
	tv := u.wait(t, st.Task.ID)
	if tv.Status != state.TaskFailed || tv.Fault.Code != CodeUpgUndone || !strings.Contains(tv.Fault.Message, "磁盘满了") {
		t.Fatalf("task %+v", tv.Fault)
	}
	// the program and settings are the old ones
	a, _ := u.w.st.GetApp(ctx, "demo-conf")
	if a.CatalogVersion != "v1.0.0" {
		t.Fatalf("version %s", a.CatalogVersion)
	}
	if env := envMap(t, u.env, "demo-conf"); env["EXTRA"] != "" {
		t.Fatalf("env %v", env)
	}
}

// An app without a data directory has no snapshot: a failure puts the
// settings and the program back, and nothing can be gone back to.
func TestUpgradeWithoutData(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.snaps.noData = true
	u.mustInstall(t, Request{App: "demo-conf"})
	u.publish(t, v2())
	plan, err := u.e.UpgradePlan(ctx, UpgradeRequest{App: "demo-conf"})
	if err != nil || !plan.CanUpgrade || plan.Snapshot || !strings.Contains(strings.Join(plan.Notes, " "), "没有存数据的目录") {
		t.Fatalf("plan %v %+v", err, plan)
	}
	u.healthy.Store(false)
	st, _ := u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf", ConfirmRestart: true})
	tv := u.wait(t, st.Task.ID)
	if tv.Fault == nil || tv.Fault.Code != CodeUpgFailed || len(u.snaps.taken) != 0 || len(u.snaps.restored) != 0 {
		t.Fatalf("task %+v taken %v restored %v", tv.Fault, u.snaps.taken, u.snaps.restored)
	}
	a, _ := u.w.st.GetApp(ctx, "demo-conf")
	if a.CatalogVersion != "v1.0.0" || envMap(t, u.env, "demo-conf")["EXTRA"] != "" {
		t.Fatalf("not put back: %s %v", a.CatalogVersion, envMap(t, u.env, "demo-conf"))
	}
	cm, _ := u.w.st.GetCatalogManifest(ctx, "demo-conf")
	if cm.CatalogVersion != "v1.0.0" {
		t.Fatalf("saved manifest is %s", cm.CatalogVersion)
	}
}

// What the new version needs that the app does not have stops the upgrade
// before anything is touched, and says what.
func TestUpgradeBlockedByWhatTheNewVersionNeeds(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.mustInstall(t, Request{App: "demo-conf"})
	blocked := func(t *testing.T, manifest, wantCode, wantText string, mod func(*UpgradeRequest)) UpgradePlan {
		t.Helper()
		u.publish(t, manifest)
		req := UpgradeRequest{App: "demo-conf"}
		if mod != nil {
			mod(&req)
		}
		p, err := u.e.UpgradePlan(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if wantCode == "" {
			if !p.CanUpgrade {
				t.Fatalf("blocked: %s %s", p.BlockedCode, p.Blocked)
			}
			return p
		}
		if p.CanUpgrade || p.BlockedCode != wantCode || !strings.Contains(p.Blocked, wantText) || !strings.Contains(p.Summary, "什么都没动") {
			t.Fatalf("plan %+v", p)
		}
		if _, err := u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf", ConfirmRestart: true}); code(err) != wantCode {
			t.Fatalf("start: %v", err)
		}
		if len(u.snaps.taken) != 0 {
			t.Fatal("a snapshot was taken for an upgrade that cannot go ahead")
		}
		return p
	}
	// a new required setting without a default
	must := v2([2]string{"  - { key: extra, type: string, label: 额外设置, default: more }\n", "  - { key: extra, type: string, label: 额外设置, default: more }\n  - { key: must, type: string, label: 必填新项, required: true }\n"})
	blocked(t, must, CodeUpgNeed, "must", nil)
	// … until the user gives it
	blocked(t, must, "", "", func(r *UpgradeRequest) { r.Params = map[string]any{"must": "给的值"} })
	// more memory than the app has
	mem := strings.Replace(v2(), "memory_mb: { min: 64 }", "memory_mb: { min: 4096 }", 1)
	blocked(t, mem, CodeUpgNeed, "4096 MB", nil)
	// a data directory the app does not have
	vol := strings.Replace(v2(), "  - { name: data, path: /data }\n", "  - { name: data, path: /data }\n  - { name: more, path: /more }\n", 1)
	blocked(t, vol, CodeUpgChanged, "/more", nil)
	// a service it must have
	svc := strings.Replace(v2(), "volumes:\n", "uses:\n  - { name: llm, kind: llm_gateway }\nvolumes:\n", 1)
	svc = strings.Replace(svc, "  - { key: TZ, from: platform.tz }\n", "  - { key: TZ, from: platform.tz }\n  - { key: LLM_URL, from: uses.llm.base_url }\n", 1)
	blocked(t, svc, CodeUpgChanged, "多需要一个服务", nil)
	// the disk cannot hold the snapshot
	u.publish(t, v2())
	u.snaps.notEnough = true
	p, err := u.e.UpgradePlan(ctx, UpgradeRequest{App: "demo-conf"})
	if err != nil || p.CanUpgrade || p.BlockedCode != appbackup.CodeDisk || p.DiskOK {
		t.Fatalf("disk: %v %+v", err, p)
	}
	if _, err := u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf", ConfirmRestart: true}); code(err) != appbackup.CodeDisk {
		t.Fatalf("start on a full disk: %v", err)
	}
	if len(u.snaps.taken) != 0 || u.w.pulled != nil && contains1(u.w.pulled, "example/demo-conf@sha256:"+strings.Repeat("6", 64)) {
		t.Fatalf("something was done: %v %v", u.snaps.taken, u.w.pulled)
	}
}

// A snapshot that could not be made stops the upgrade with the app as it was.
func TestUpgradeStopsWhenTheSnapshotFails(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.mustInstall(t, Request{App: "demo-conf"})
	u.publish(t, v2())
	u.snaps.failTake = appbackup.Fault(appbackup.CodeExport, "读卷失败")
	st, err := u.e.StartUpgrade(ctx, UpgradeRequest{App: "demo-conf", ConfirmRestart: true})
	if err != nil {
		t.Fatal(err)
	}
	tv := u.wait(t, st.Task.ID)
	if tv.Status != state.TaskFailed || tv.Fault.Code != appbackup.CodeExport || !strings.Contains(tv.Fault.Message, "没有动应用") {
		t.Fatalf("task %+v", tv.Fault)
	}
	a, _ := u.w.st.GetApp(ctx, "demo-conf")
	if a.CatalogVersion != "v1.0.0" || envMap(t, u.env, "demo-conf")["EXTRA"] != "" {
		t.Fatal("the app was changed")
	}
}

// Nothing to go back to, or a snapshot of the version running: refused.
func TestRollbackRefusals(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.mustInstall(t, Request{App: "demo-conf"})
	p, err := u.e.PlanRollback(ctx, "demo-conf")
	if err != nil || p.CanRollback || p.BlockedCode != CodeRollbackCannot {
		t.Fatalf("no snapshot: %v %+v", err, p)
	}
	if _, err := u.e.StartRollback(ctx, RollbackRequest{App: "demo-conf", Confirm: true}); code(err) != CodeRollbackCannot {
		t.Fatalf("start: %v", err)
	}
	// a snapshot of the version that runs
	if _, err := u.snaps.SnapshotPreUpgrade(ctx, "demo-conf", ""); err != nil {
		t.Fatal(err)
	}
	p, _ = u.e.PlanRollback(ctx, "demo-conf")
	if p.CanRollback || p.BlockedCode != CodeRollbackCannot || !strings.Contains(p.Blocked, "现在这个版本") {
		t.Fatalf("same version: %+v", p)
	}
	if _, err := u.e.PlanRollback(ctx, "no-such-app"); code(err) != CodeNotCatalogApp {
		t.Fatalf("unknown app: %v", err)
	}
}

// An upgrade the server stopped in the middle of goes back to the snapshot
// when it starts again.
func TestRecoverUpgrade(t *testing.T) {
	u := upgradeSetup(t)
	ctx := context.Background()
	u.mustInstall(t, Request{App: "demo-conf"})
	snap, err := u.snaps.SnapshotPreUpgrade(ctx, "demo-conf", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := u.e.saveUndo(ctx, "demo-conf", &undoRecord{Task: "x", Snapshot: snap.ID, Redeploy: true, LiveID: "d", LiveImage: "i"}); err != nil {
		t.Fatal(err)
	}
	tr := &taskRun{e: u.e, id: "recover1", view: TaskView{ID: "recover1", Kind: KindUpgrade, Target: "demo-conf", Status: state.TaskRunning}}
	u.e.recoverUpgrade(ctx, tr, "demo-conf", &undoRecord{Snapshot: snap.ID, Redeploy: true}, "interrupted")
	if len(u.snaps.restored) != 1 || u.snaps.restored[0] != snap.ID {
		t.Fatalf("restored %v", u.snaps.restored)
	}
	if tr.view.Fault == nil || tr.view.Fault.Code != CodeUpgFailed {
		t.Fatalf("fault %+v", tr.view.Fault)
	}
}
