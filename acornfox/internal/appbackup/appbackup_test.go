package appbackup

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/faults"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// fakeRunner is an in-memory runner: containers per app and volumes holding
// files. It exports volumes as real tar streams and imports them back.
type fakeRunner struct {
	mu         sync.Mutex
	containers map[string][]runner.ContainerInfo
	volumes    map[string]map[string]string // volume -> file -> content
	images     []runner.ImageInfo
	log        []string

	failExport  string // volume name whose export fails
	failImport  string // volume name whose import fails ...
	importFails int    // ... this many times
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{containers: map[string][]runner.ContainerInfo{}, volumes: map[string]map[string]string{}}
}

func (f *fakeRunner) note(format string, a ...any) {
	f.log = append(f.log, fmt.Sprintf(format, a...))
}

func (f *fakeRunner) ListContainers(_ context.Context, app string) ([]runner.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]runner.ContainerInfo(nil), f.containers[app]...), nil
}

func (f *fakeRunner) setRunning(app, name string, running bool) {
	cs := f.containers[app]
	for i := range cs {
		if cs[i].Name == name {
			cs[i].Running = running
		}
	}
}

func (f *fakeRunner) StopContainer(_ context.Context, app, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("stop %s", name)
	f.setRunning(app, name, false)
	return nil
}

func (f *fakeRunner) StartContainer(_ context.Context, app, name string) (runner.ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("start %s", name)
	f.setRunning(app, name, true)
	return runner.ContainerInfo{Name: name, Running: true}, nil
}

func (f *fakeRunner) RemoveContainer(_ context.Context, app, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("rm %s", name)
	var keep []runner.ContainerInfo
	for _, c := range f.containers[app] {
		if c.Name != name {
			keep = append(keep, c)
		}
	}
	f.containers[app] = keep
	return nil
}

func (f *fakeRunner) EnsureVolume(_ context.Context, _, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.volumes[name] == nil {
		f.volumes[name] = map[string]string{}
	}
	return nil
}

func (f *fakeRunner) RemoveVolume(_ context.Context, _, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note("rmvol %s", name)
	delete(f.volumes, name)
	return nil
}

func (f *fakeRunner) ListImages(_ context.Context, _ string) ([]runner.ImageInfo, error) {
	return f.images, nil
}

func (f *fakeRunner) ExportVolume(_ context.Context, req runner.VolumeDataRequest, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failExport != "" && f.failExport == req.Volume {
		return errors.New("docker: export boom")
	}
	files, ok := f.volumes[req.Volume]
	if !ok {
		return errors.New("no such volume")
	}
	tw := tar.NewWriter(w)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755})
	var names []string
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "./" + n, Mode: 0o644, Size: int64(len(files[n]))})
		_, _ = tw.Write([]byte(files[n]))
	}
	return tw.Close()
}

func (f *fakeRunner) ImportVolume(_ context.Context, req runner.VolumeDataRequest, r io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.importFails > 0 && f.failImport == req.Volume {
		f.importFails--
		return errors.New("docker: import boom")
	}
	if f.volumes[req.Volume] == nil {
		f.volumes[req.Volume] = map[string]string{}
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		b, _ := io.ReadAll(tr)
		f.volumes[req.Volume][strings.TrimPrefix(h.Name, "./")] = string(b)
	}
}

func (f *fakeRunner) VolumeSizes(_ context.Context, _ string) ([]runner.VolumeSize, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []runner.VolumeSize
	for name, files := range f.volumes {
		n := int64(0)
		for _, c := range files {
			n += int64(len(c))
		}
		out = append(out, runner.VolumeSize{Name: name, Bytes: n})
	}
	return out, nil
}

// harness is a manager over a real store and the fake runner. Kick makes the
// pending deployments of an app go live (or fail when failDeploys > 0).
type harness struct {
	t   *testing.T
	st  *state.Store
	run *fakeRunner
	m   *Manager
	dir string

	mu          sync.Mutex
	failDeploys int // the next N deployments fail
	free        Space
	freeErr     error
	clock       time.Time
}

func newHarness(t *testing.T, mod func(*Config)) *harness {
	t.Helper()
	dir := t.TempDir()
	st, err := state.Open(state.Config{Path: filepath.Join(dir, "acornfox.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := &harness{t: t, st: st, run: newFakeRunner(), dir: filepath.Join(dir, "backups"), clock: time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC),
		free: Space{BackupFree: 100 << 30, DockerFree: 100 << 30}}
	cfg := Config{
		Dir: h.dir, Store: st, Runner: h.run, Version: "test",
		Kick: h.kick,
		Free: func(context.Context) (Space, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.free, h.freeErr
		},
		Poll: time.Millisecond, HealthWait: time.Minute,
		Now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.clock = h.clock.Add(time.Second)
			return h.clock
		},
	}
	if mod != nil {
		mod(&cfg)
	}
	h.m = New(cfg)
	t.Cleanup(h.m.Close)
	return h
}

// kick goes live with every pending deployment of app (like the reconciler).
func (h *harness) kick(app string) {
	ctx := context.Background()
	pend, err := h.st.PendingDeployments(ctx, app)
	if err != nil {
		return
	}
	for _, d := range pend {
		h.mu.Lock()
		fail := h.failDeploys > 0
		if fail {
			h.failDeploys--
		}
		h.mu.Unlock()
		if fail {
			_, _ = h.st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error {
				x.Status = state.StatusFailed
				x.Diagnosis = &state.Diagnosis{Stage: "health", Code: "port_not_listening", Message: "健康检查超时"}
				return nil
			})
			continue
		}
		_, _ = h.st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error {
			x.ImageID = d.SourceRef
			return nil
		})
		_ = h.st.SetCurrentDeployment(ctx, app, d.ID)
		h.run.mu.Lock()
		h.run.containers[app] = append(h.run.containers[app], runner.ContainerInfo{Name: runner.ContainerName(app, d.ID), Role: runner.RoleApp, Running: true})
		h.run.mu.Unlock()
	}
}

// installApp creates a live app with one data volume at /data.
func (h *harness) installApp(app, image string, files map[string]string) {
	h.t.Helper()
	ctx := context.Background()
	if _, _, err := h.st.EnsureApp(ctx, app); err != nil {
		h.t.Fatal(err)
	}
	v, _, err := h.st.AddVolume(ctx, app, "/data", false)
	if err != nil {
		h.t.Fatal(err)
	}
	h.run.mu.Lock()
	h.run.volumes[v.VolumeName] = map[string]string{}
	for k, c := range files {
		h.run.volumes[v.VolumeName][k] = c
	}
	h.run.images = append(h.run.images, runner.ImageInfo{ID: image, Created: 1})
	h.run.mu.Unlock()
	d, _, err := h.st.CreateDeployment(ctx, state.NewDeployment{App: app, SourceKind: state.SourceImage, SourceRef: image, SourceDigest: image})
	if err != nil {
		h.t.Fatal(err)
	}
	h.kick(app)
	_ = d
}

func (h *harness) vol(app string) string {
	h.t.Helper()
	vs, err := h.st.ListVolumes(context.Background(), app)
	if err != nil || len(vs) == 0 {
		h.t.Fatalf("volumes of %s: %v %v", app, vs, err)
	}
	return vs[0].VolumeName
}

func (h *harness) files(app string) map[string]string {
	h.run.mu.Lock()
	defer h.run.mu.Unlock()
	out := map[string]string{}
	for k, v := range h.run.volumes[h.vol(app)] {
		out[k] = v
	}
	return out
}

func (h *harness) setFiles(app string, files map[string]string) {
	h.run.mu.Lock()
	defer h.run.mu.Unlock()
	h.run.volumes[h.vol(app)] = files
}

func (h *harness) running(app string) int {
	n := 0
	h.run.mu.Lock()
	defer h.run.mu.Unlock()
	for _, c := range h.run.containers[app] {
		if c.Running {
			n++
		}
	}
	return n
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	e, ok := asFault(err)
	if !ok || e.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func waitTask(t *testing.T, m *Manager, id string) TaskView {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		v, err := m.Task(id)
		if err != nil {
			t.Fatal(err)
		}
		if v.Status != TaskRunning {
			return v
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return TaskView{}
}

func TestBackupMakesFilesAndRestartsApp(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a.txt": "alpha", "sub/b.txt": "beta"})
	ctx := context.Background()
	if err := h.st.SetEnv(ctx, state.EnvVar{App: "hello", Key: "TOKEN", Value: "s3cret", Secret: true}); err != nil {
		t.Fatal(err)
	}

	meta, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{Note: "第一份"})
	if err != nil {
		t.Fatal(err)
	}
	if meta.Kind != KindManual || meta.App != "hello" || meta.Note != "第一份" || !meta.WasRunning {
		t.Fatalf("meta: %+v", meta)
	}
	if len(meta.Volumes) != 1 || meta.Volumes[0].Role != RoleApp || meta.Volumes[0].Path != "/data" || meta.Volumes[0].Entries < 3 {
		t.Fatalf("volumes: %+v", meta.Volumes)
	}
	if meta.Version.ImageID != "sha256:img1" || meta.Version.Seq != 1 {
		t.Fatalf("version: %+v", meta.Version)
	}
	// Whole and listed.
	got, err := h.m.Get("hello", meta.ID)
	if err != nil || got.ID != meta.ID {
		t.Fatalf("get: %v %+v", err, got)
	}
	if err := h.m.verify("hello", got); err != nil {
		t.Fatalf("verify: %v", err)
	}
	list, _ := h.m.List("hello")
	if len(list) != 1 {
		t.Fatalf("list: %d", len(list))
	}
	// 0600 files, no partial left, records hold the secret.
	for _, f := range []string{"meta.json", "records.json", "vol-0.tar.gz"} {
		st, err := os.Stat(filepath.Join(h.dir, "hello", meta.ID, f))
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", f, err, st)
		}
	}
	rec, err := h.m.readRecords("hello", meta.ID)
	if err != nil || len(rec.Env) != 1 || rec.Env[0].Value != "s3cret" {
		t.Fatalf("records: %v %+v", err, rec.Env)
	}
	entries, _ := os.ReadDir(filepath.Join(h.dir, "hello"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), partialExt) {
			t.Fatalf("partial left: %s", e.Name())
		}
	}
	// The app was stopped for the copy and runs again.
	if h.running("hello") != 1 {
		t.Fatalf("app not running again: %v", h.run.log)
	}
	log := strings.Join(h.run.log, ",")
	if !strings.Contains(log, "stop ") || !strings.Contains(log, "start ") {
		t.Fatalf("log: %s", log)
	}
}

func TestBackupOfStoppedAppLeavesItStopped(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	h.run.containers["hello"][0].Running = false
	meta, err := h.m.SnapshotBackup(context.Background(), "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if meta.WasRunning || h.running("hello") != 0 {
		t.Fatalf("stopped app was started: %+v %v", meta, h.run.log)
	}
}

func TestRetention(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.KeepManual = 3 })
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	var ids []string
	for i := 0; i < 5; i++ {
		meta, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, meta.ID)
	}
	for i := 0; i < 2; i++ {
		if _, err := h.m.SnapshotPreUpgrade(ctx, "hello", "升级前"); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := h.m.List("hello")
	manual, pre := 0, 0
	for _, b := range all {
		switch b.Kind {
		case KindManual:
			manual++
		case KindPreUpgrade:
			pre++
		}
	}
	// Automatic backups younger than 7 days are not removed to keep a count.
	if manual != 3 || pre != 2 {
		t.Fatalf("manual=%d pre=%d", manual, pre)
	}
	if _, err := h.m.Get("hello", ids[0]); err == nil {
		t.Fatal("the oldest manual backup should be gone")
	}
	if _, err := h.m.Get("hello", ids[4]); err != nil {
		t.Fatal("the newest must stay")
	}
}

func TestRestoreRoundTrip(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a.txt": "alpha", "sub/b.txt": "beta"})
	ctx := context.Background()
	_ = h.st.SetEnv(ctx, state.EnvVar{App: "hello", Key: "MODE", Value: "old"})
	meta, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Break the data and the variables.
	h.setFiles("hello", map[string]string{"a.txt": "BROKEN", "new.txt": "x"})
	_ = h.st.SetEnv(ctx, state.EnvVar{App: "hello", Key: "MODE", Value: "new"})
	_ = h.st.SetEnv(ctx, state.EnvVar{App: "hello", Key: "EXTRA", Value: "1"})

	// Without a yes nothing happens.
	_, err = h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{})
	wantCode(t, err, CodeConfirm)
	if h.files("hello")["a.txt"] != "BROKEN" {
		t.Fatal("refused restore touched the data")
	}

	tv, err := h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTask(t, h.m, tv.ID)
	if done.Status != TaskSucceeded {
		t.Fatalf("restore: %+v %+v", done.Fault, done.Steps)
	}
	files := h.files("hello")
	if files["a.txt"] != "alpha" || files["sub/b.txt"] != "beta" || files["new.txt"] != "" {
		t.Fatalf("files after restore: %v", files)
	}
	env, _ := h.st.ListEnv(ctx, "hello")
	got := map[string]string{}
	for _, e := range env {
		got[e.Key] = e.Value
	}
	if got["MODE"] != "old" || len(got) != 1 {
		t.Fatalf("env after restore: %v", got)
	}
	if done.Result == nil || done.Result.PreRestoreID == "" || done.Result.RolledBack {
		t.Fatalf("result: %+v", done.Result)
	}
	// The data before the restore was kept as a pre-restore backup.
	pre, err := h.m.Get("hello", done.Result.PreRestoreID)
	if err != nil || pre.Kind != KindPreRestore {
		t.Fatalf("pre-restore: %v %+v", err, pre)
	}
	// The app runs again, as a new deployment.
	a, _ := h.st.GetApp(ctx, "hello")
	d, _ := h.st.GetDeployment(ctx, a.CurrentDeployment)
	if d.Reason != ReasonRestore || d.Status != state.StatusLive || h.running("hello") != 1 {
		t.Fatalf("deployment: %+v running=%d", d, h.running("hello"))
	}

	// The restore can itself be undone with its pre-restore backup.
	res, err := h.m.RestoreSnapshot(ctx, "hello", pre.ID, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.RolledBack || h.files("hello")["a.txt"] != "BROKEN" {
		t.Fatalf("undo: %+v %v", res, h.files("hello"))
	}
}

func TestRestoreRefusals(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	meta, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Unknown id, bad id, app not installed.
	_, err = h.m.StartRestore(ctx, "hello", "20000101-000000-0000", RestoreOptions{Confirm: true})
	wantCode(t, err, CodeNotFound)
	_, err = h.m.StartRestore(ctx, "hello", "abc", RestoreOptions{Confirm: true})
	wantCode(t, err, CodeBadRequest)
	_, err = h.m.StartRestore(ctx, "ghost", meta.ID, RestoreOptions{Confirm: true})
	wantCode(t, err, CodeNotFound)

	// A file cut short is not used.
	h.setFiles("hello", map[string]string{"a": "changed"})
	p := filepath.Join(h.dir, "hello", meta.ID, "vol-0.tar.gz")
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, b[:len(b)/2], 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{Confirm: true})
	wantCode(t, err, CodeCorrupt)
	if h.files("hello")["a"] != "changed" {
		t.Fatal("a corrupt backup touched the data")
	}

	// A flipped byte with the right size is caught by the checksum.
	b2 := append([]byte(nil), b...)
	b2[len(b2)/2] ^= 0xff
	if err := os.WriteFile(p, b2, 0o600); err != nil {
		t.Fatal(err)
	}
	m2, _ := h.m.Get("hello", meta.ID)
	wantCode(t, h.m.verify("hello", m2), CodeCorrupt)
}

func TestRestoreRollsBackWhenAppDoesNotComeUp(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "old"})
	ctx := context.Background()
	meta, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h.setFiles("hello", map[string]string{"a": "current"})

	h.mu.Lock()
	h.failDeploys = 1 // the restored version does not come up; the going back does
	h.mu.Unlock()
	tv, err := h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTask(t, h.m, tv.ID)
	if done.Status != TaskFailed || done.Fault == nil || done.Fault.Code != CodeRolledBack {
		t.Fatalf("want BKP-07, got %+v", done)
	}
	if done.Result == nil || !done.Result.RolledBack || done.Result.PreRestoreID == "" {
		t.Fatalf("result: %+v", done.Result)
	}
	if got := h.files("hello")["a"]; got != "current" {
		t.Fatalf("data after going back: %q", got)
	}
	a, _ := h.st.GetApp(ctx, "hello")
	d, _ := h.st.GetDeployment(ctx, a.CurrentDeployment)
	if d.Status != state.StatusLive || h.running("hello") != 1 {
		t.Fatalf("app after going back: %+v running=%d", d, h.running("hello"))
	}
}

func TestRestoreRollbackFailsToo(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "old"})
	ctx := context.Background()
	meta, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	h.mu.Lock()
	h.failDeploys = 2
	h.mu.Unlock()
	tv, err := h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTask(t, h.m, tv.ID)
	if done.Fault == nil || done.Fault.Code != CodeRollbackFailed || done.Result == nil || done.Result.PreRestoreID == "" {
		t.Fatalf("want BKP-08 with the pre-restore id, got %+v", done)
	}
	if _, err := h.m.Get("hello", done.Result.PreRestoreID); err != nil {
		t.Fatal("the pre-restore backup must stay")
	}
}

func TestRestoreFailsWhileSwappingGoesBack(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "old"})
	ctx := context.Background()
	meta, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	h.setFiles("hello", map[string]string{"a": "current"})
	// Writing the backup's files into the volume fails once; going back
	// writes the pre-restore backup's files.
	h.run.failImport, h.run.importFails = h.vol("hello"), 1
	tv, err := h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTask(t, h.m, tv.ID)
	if done.Fault == nil || done.Fault.Code != CodeRolledBack || done.Result == nil || !done.Result.RolledBack {
		t.Fatalf("want BKP-07, got %+v", done)
	}
	if got := h.files("hello")["a"]; got != "current" {
		t.Fatalf("data after going back: %q", got)
	}
	if h.running("hello") != 1 {
		t.Fatalf("app not running after going back: %v", h.run.log)
	}
}

func TestDiskTooSmallTouchesNothing(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": strings.Repeat("x", 1000)})
	h.mu.Lock()
	h.free = Space{BackupFree: 1 << 20, DockerFree: 100 << 30}
	h.mu.Unlock()
	h.run.log = nil
	_, err := h.m.StartBackup(context.Background(), "hello", BackupOptions{})
	wantCode(t, err, CodeDisk)
	plan, err := h.m.PlanBackup(context.Background(), "hello", BackupOptions{})
	if err != nil || plan.Enough || !plan.FreeKnown {
		t.Fatalf("plan: %v %+v", err, plan)
	}
	if len(h.run.log) != 0 || h.running("hello") != 1 {
		t.Fatalf("the app was touched: %v", h.run.log)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "hello")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(h.dir, "hello"))
		if len(entries) != 0 {
			t.Fatalf("files were left: %v", entries)
		}
	}
}

func TestExportFailureRestartsApp(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	h.run.failExport = h.vol("hello")
	_, err := h.m.SnapshotBackup(context.Background(), "hello", BackupOptions{})
	wantCode(t, err, CodeExport)
	if h.running("hello") != 1 {
		t.Fatalf("app left stopped: %v", h.run.log)
	}
	if list, _ := h.m.List("hello"); len(list) != 0 {
		t.Fatalf("a failed backup is listed: %v", list)
	}
	entries, _ := os.ReadDir(filepath.Join(h.dir, "hello"))
	if len(entries) != 0 {
		t.Fatalf("partial left: %v", entries)
	}
	if t0 := h.m.Tasks("hello"); len(t0) == 0 || t0[0].Status != TaskFailed || t0[0].Fault.Code != CodeExport {
		t.Fatalf("task: %+v", t0)
	}
}

func TestBusyApp(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	if err := h.m.acquire("hello", "aaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	_, err := h.m.StartBackup(context.Background(), "hello", BackupOptions{})
	wantCode(t, err, CodeBusy)
	if _, err := h.m.Delete(context.Background(), "hello", "20000101-000000-0000", true); err == nil {
		t.Fatal("delete while busy")
	}
	h.m.release("hello", "aaaaaaaaaaaa")
	if _, err := h.m.StartBackup(context.Background(), "hello", BackupOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestPendingDeploymentRefuses(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	if _, _, err := h.st.CreateDeployment(ctx, state.NewDeployment{App: "hello", SourceKind: state.SourceImage, SourceRef: "sha256:img2", SourceDigest: "sha256:img2", BypassDedup: true}); err != nil {
		t.Fatal(err)
	}
	_, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	wantCode(t, err, CodeBusy)
}

func TestVersionGone(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "old"})
	ctx := context.Background()
	meta, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	// The app moves to another version and the old image is gone.
	if _, _, err := h.st.CreateDeployment(ctx, state.NewDeployment{App: "hello", SourceKind: state.SourceImage, SourceRef: "sha256:img2", SourceDigest: "sha256:img2", BypassDedup: true}); err != nil {
		t.Fatal(err)
	}
	h.kick("hello")
	h.run.mu.Lock()
	h.run.images = []runner.ImageInfo{{ID: "sha256:img2"}}
	h.run.mu.Unlock()

	plan, err := h.m.PlanRestore(ctx, "hello", meta.ID, true)
	if err != nil || plan.VersionAvailable || !plan.Intact || !plan.Verified {
		t.Fatalf("plan: %v %+v", err, plan)
	}
	_, err = h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{Confirm: true})
	wantCode(t, err, CodeVersionGone)

	tv, err := h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{Confirm: true, AllowCurrentVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if done := waitTask(t, h.m, tv.ID); done.Status != TaskSucceeded {
		t.Fatalf("restore on the current version: %+v", done.Fault)
	}
	if h.files("hello")["a"] != "old" {
		t.Fatalf("data: %v", h.files("hello"))
	}
	a, _ := h.st.GetApp(ctx, "hello")
	d, _ := h.st.GetDeployment(ctx, a.CurrentDeployment)
	if d.ImageID != "sha256:img2" {
		t.Fatalf("image: %s", d.ImageID)
	}
}

func TestRestoreSwitchesBackToBackupVersion(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "old"})
	ctx := context.Background()
	meta, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	if _, _, err := h.st.CreateDeployment(ctx, state.NewDeployment{App: "hello", SourceKind: state.SourceImage, SourceRef: "sha256:img2", SourceDigest: "sha256:img2", BypassDedup: true}); err != nil {
		t.Fatal(err)
	}
	h.kick("hello")
	h.run.mu.Lock()
	h.run.images = []runner.ImageInfo{{ID: "sha256:img1"}, {ID: "sha256:img2"}}
	h.run.mu.Unlock()
	plan, _ := h.m.PlanRestore(ctx, "hello", meta.ID, false)
	if !plan.ChangesVersion || !plan.VersionAvailable {
		t.Fatalf("plan: %+v", plan)
	}
	res, err := h.m.RestoreSnapshot(ctx, "hello", meta.ID, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := h.st.GetApp(ctx, "hello")
	d, _ := h.st.GetDeployment(ctx, a.CurrentDeployment)
	if d.ImageID != "sha256:img1" || res.PreRestoreID == "" {
		t.Fatalf("image %s result %+v", d.ImageID, res)
	}
}

func TestAddonVolumesAreBackedUpAndRestored(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	creds := []byte(`{"user":"u","password":"p"}`)
	if _, err := h.st.AddAddon(ctx, state.Addon{App: "hello", Kind: state.AddonPostgres, Image: "postgres:16-alpine", VolumeName: runner.AddonVolumeName("hello", "postgres"), Credentials: creds, EnvVar: "DATABASE_URL"}); err != nil {
		t.Fatal(err)
	}
	av := runner.AddonVolumeName("hello", "postgres")
	h.run.volumes[av] = map[string]string{"PG_VERSION": "16"}
	h.run.containers["hello"] = append(h.run.containers["hello"], runner.ContainerInfo{Name: runner.AddonContainerName("hello", "postgres"), Role: runner.RoleAddon, Running: true})

	meta, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Volumes) != 2 || meta.Volumes[1].Role != RoleAddon || meta.Volumes[1].Addon != "postgres" {
		t.Fatalf("volumes: %+v", meta.Volumes)
	}
	// The app container goes first, the database last; the database starts first.
	log := strings.Join(h.run.log, ",")
	if strings.Index(log, "stop af-hello-") > strings.Index(log, "stop af-hello-addon-postgres") {
		t.Fatalf("stop order: %s", log)
	}
	if strings.Index(log, "start af-hello-addon-postgres") > strings.LastIndex(log, "start af-hello-") {
		t.Fatalf("start order: %s", log)
	}

	h.run.volumes[av] = map[string]string{"PG_VERSION": "BROKEN"}
	// Changed credentials are put back too.
	_ = h.st.RemoveAddon(ctx, "hello", "postgres", true)
	_, _ = h.st.AddAddon(ctx, state.Addon{App: "hello", Kind: state.AddonPostgres, Image: "postgres:16-alpine", VolumeName: av, Credentials: []byte(`{"user":"u","password":"other"}`), EnvVar: "DATABASE_URL"})

	if _, err := h.m.RestoreSnapshot(ctx, "hello", meta.ID, RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if h.run.volumes[av]["PG_VERSION"] != "16" {
		t.Fatalf("addon data: %v", h.run.volumes[av])
	}
	ads, _ := h.st.ListAddons(ctx, "hello")
	if len(ads) != 1 || !bytes.Contains(ads[0].Credentials, []byte(`"p"`)) {
		t.Fatalf("addon record: %+v", ads)
	}
}

func TestServiceCheckAfterRestore(t *testing.T) {
	h := newHarness(t, func(c *Config) { c.Now = nil })
	h.installApp("db", "sha256:img1", map[string]string{"pgdata": "1"})
	h.installApp("web", "sha256:img9", map[string]string{"w": "1"})
	ctx := context.Background()
	if _, err := h.st.CreateService(ctx, state.Service{Name: "db-pg", Kind: "postgres", Source: state.ServiceFromApp, App: "db"}); err != nil {
		t.Fatal(err)
	}
	meta, err := h.m.SnapshotBackup(ctx, "db", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	// web gets its account from db after the backup.
	if _, err := h.st.SetServiceBinding(ctx, state.ServiceBinding{App: "web", UseName: "database", Service: "db-pg", IssuedEnc: "x"}); err != nil {
		t.Fatal(err)
	}
	res, err := h.m.RestoreSnapshot(ctx, "db", meta.ID, RestoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, CodeServiceRecheck) && strings.Contains(w, "web") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings: %v", res.Warnings)
	}
	// The registry itself was not touched.
	bs, _ := h.st.ListServiceBindings(ctx, state.BindingFilter{Service: "db-pg"})
	if len(bs) != 1 {
		t.Fatalf("bindings: %v", bs)
	}
}

func TestDeleteNeedsConfirmAndIsRepeatable(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	meta, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	_, err := h.m.Delete(ctx, "hello", meta.ID, false)
	wantCode(t, err, CodeConfirm)
	if _, err := h.m.Get("hello", meta.ID); err != nil {
		t.Fatal("refused delete removed it")
	}
	deleted, err := h.m.Delete(ctx, "hello", meta.ID, true)
	if err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	deleted, err = h.m.Delete(ctx, "hello", meta.ID, true)
	if err != nil || deleted {
		t.Fatalf("second delete: %v %v", deleted, err)
	}
	_, err = h.m.Delete(ctx, "hello", "bad", true)
	wantCode(t, err, CodeBadRequest)
}

func TestNoDataAndNoApp(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	_, err := h.m.StartBackup(ctx, "ghost", BackupOptions{})
	wantCode(t, err, CodeNoApp)
	if _, _, err := h.st.EnsureApp(ctx, "empty"); err != nil {
		t.Fatal(err)
	}
	_, err = h.m.StartBackup(ctx, "empty", BackupOptions{})
	wantCode(t, err, CodeNoData)
	_, err = h.m.StartBackup(ctx, "empty", BackupOptions{Kind: "bogus"})
	wantCode(t, err, CodeBadRequest)
}

func TestBackupsOfDeletedAppStayListed(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	meta, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	if err := h.st.DeleteApp(ctx, "hello", true); err != nil {
		t.Fatal(err)
	}
	list, _ := h.m.List("hello")
	if len(list) != 1 || list[0].ID != meta.ID {
		t.Fatalf("list: %+v", list)
	}
	_, err := h.m.StartRestore(ctx, "hello", meta.ID, RestoreOptions{Confirm: true})
	wantCode(t, err, CodeNoApp)
	ov := h.m.Overview(ctx)
	if len(ov.Apps) != 1 || ov.Apps[0].Installed || ov.Apps[0].Count != 1 {
		t.Fatalf("overview: %+v", ov.Apps)
	}
}

func TestTasksSurviveRestartAndInterruptedAreFailed(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	tv, err := h.m.StartBackup(context.Background(), "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTask(t, h.m, tv.ID)
	if done.Status != TaskSucceeded || done.Result == nil || done.Result.BackupID == "" {
		t.Fatalf("task: %+v", done)
	}
	// A task file that says running (the server died then).
	run := TaskView{ID: "abcdefabcdef", App: "hello", Op: OpBackup, Status: TaskRunning, StartedAt: time.Now(), Steps: []Step{{Key: "pack", Text: "打包数据", Status: StepRunning}}}
	if err := writeJSONFile(filepath.Join(h.dir, ".tasks", run.ID+".json"), run); err != nil {
		t.Fatal(err)
	}
	m2 := New(Config{Dir: h.dir, Store: h.st, Runner: h.run})
	defer m2.Close()
	v, err := m2.Task("abcdefabcdef")
	if err != nil || v.Status != TaskFailed || v.Fault == nil || v.Fault.Code != CodeInterrupted || v.Steps[0].Status != StepFailed {
		t.Fatalf("interrupted: %v %+v", err, v)
	}
	if v2, err := m2.Task(tv.ID); err != nil || v2.Status != TaskSucceeded {
		t.Fatalf("finished task after restart: %v %+v", err, v2)
	}
	ov := m2.Overview(context.Background())
	found := false
	for _, f := range ov.Findings {
		if f.Code == CodeInterrupted {
			found = true
		}
	}
	if !found {
		t.Fatalf("findings: %+v", ov.Findings)
	}
}

func TestOverviewFindsPartialAndLowSpace(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	if _, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.dir, "hello", "20261101-010101-aaaa.partial"), 0o700); err != nil {
		t.Fatal(err)
	}
	ov := h.m.Overview(ctx)
	if !ov.Writable || len(ov.Apps) != 1 || ov.Apps[0].Count != 1 || ov.TotalBytes <= 0 {
		t.Fatalf("overview: %+v", ov)
	}
	codes := map[string]string{}
	for _, f := range ov.Findings {
		codes[f.Code] = f.Severity
	}
	if codes[CodeInterrupted] != SevWarn || codes[CodeDirNotWritable] != SevOK {
		t.Fatalf("findings: %+v", ov.Findings)
	}
	h.mu.Lock()
	h.free = Space{BackupFree: 100 << 20}
	h.mu.Unlock()
	ov = h.m.Overview(ctx)
	low := false
	for _, f := range ov.Findings {
		if f.Code == CodeDisk && f.Severity == SevWarn {
			low = true
		}
	}
	if !low {
		t.Fatalf("low space not reported: %+v", ov.Findings)
	}
	// The next backup clears the partial.
	h.mu.Lock()
	h.free = Space{BackupFree: 100 << 30}
	h.mu.Unlock()
	if _, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "hello", "20261101-010101-aaaa.partial")); err == nil {
		t.Fatal("partial not removed")
	}
}

func TestScanArchiveRejectsCutFiles(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": strings.Repeat("hello", 1000)})
	meta, err := h.m.SnapshotBackup(context.Background(), "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(h.dir, "hello", meta.ID, "vol-0.tar.gz")
	sum, raw, n, err := scanArchive(p)
	if err != nil || sum != meta.Volumes[0].SHA256 || raw != meta.Volumes[0].RawBytes || n != meta.Volumes[0].Entries {
		t.Fatalf("scan: %v %s %d %d", err, sum, raw, n)
	}
	b, _ := os.ReadFile(p)
	if err := os.WriteFile(p, b[:len(b)-20], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := scanArchive(p); err == nil {
		t.Fatal("a cut file must not scan clean")
	}
}

func TestPlanBackupSaysWhatWouldHappen(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": strings.Repeat("x", 4096)})
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	h.run.log = nil
	plan, err := h.m.PlanBackup(ctx, "hello", BackupOptions{})
	if err != nil || !plan.Enough || !plan.WillStop || plan.Keep != 5 || len(plan.WillPrune) != 1 || len(plan.Volumes) != 1 || plan.Volumes[0].Bytes != 4096 {
		t.Fatalf("plan: %v %+v", err, plan)
	}
	if len(h.run.log) != 0 {
		t.Fatalf("a plan touched the runner: %v", h.run.log)
	}
	if list, _ := h.m.List("hello"); len(list) != 5 {
		t.Fatalf("a plan changed the backups: %d", len(list))
	}
}

func TestExportFaultMapsFullDisk(t *testing.T) {
	v := volSpec{Volume: "af-hello-1"}
	wantCode(t, exportFault(v, &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}), CodeDisk)
	wantCode(t, exportFault(v, &os.PathError{Op: "write", Path: "x", Err: syscall.EIO}), CodeDirNotWritable)
	wantCode(t, exportFault(v, errors.New("docker went away")), CodeExport)
}

func TestRefusedBackupLeavesNoTask(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	if err := h.m.acquire("hello", "aaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.StartBackup(context.Background(), "hello", BackupOptions{}); err == nil {
		t.Fatal("expected BKP-03")
	}
	if got := h.m.Tasks("hello"); len(got) != 0 {
		t.Fatalf("a refused request left tasks: %+v", got)
	}
}

// The one-line next steps that travel with an error are the catalog's.
func TestHintsAreTheCatalogsNextSteps(t *testing.T) {
	for code := range statusOf {
		f, ok := faults.Find(code)
		if !ok || f.Code != code {
			t.Errorf("%s is not in the fault catalog", code)
			continue
		}
		if f.Next != hints[code] {
			t.Errorf("%s: hint %q differs from the catalog's next step %q", code, hints[code], f.Next)
		}
	}
}

func idsOf(t *testing.T, h *harness, app string) map[string]string {
	t.Helper()
	all, err := h.m.List(app)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, b := range all {
		out[b.ID] = b.Kind
	}
	return out
}

// upgrade -> go back (the data of the upgraded app is saved first) -> restore
// another backup: the second restore's own save must not remove the first one,
// or the upgraded data is gone for good.
func TestUpgradeRollbackThenRestoreKeepsUpgradedData(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "v1"})
	ctx := context.Background()
	manual, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pre, err := h.m.SnapshotPreUpgrade(ctx, "hello", "升级前")
	if err != nil {
		t.Fatal(err)
	}
	h.setFiles("hello", map[string]string{"a": "upgraded-data"}) // the app ran upgraded for a while

	// Going back to the pre-upgrade snapshot saves the upgraded data first.
	res, err := h.m.RestoreSnapshot(ctx, "hello", pre.ID, RestoreOptions{})
	if err != nil || res.PreRestoreID == "" {
		t.Fatalf("rollback: %v %+v", err, res)
	}
	saved := res.PreRestoreID
	if len(res.Pruned) != 0 {
		t.Fatalf("pruned on rollback: %v", res.Pruned)
	}
	// Restoring another backup takes another save; the first must stay.
	res2, err := h.m.RestoreSnapshot(ctx, "hello", manual.ID, RestoreOptions{})
	if err != nil || res2.PreRestoreID == "" || res2.PreRestoreID == saved {
		t.Fatalf("restore: %v %+v", err, res2)
	}
	if len(res2.Pruned) != 0 {
		t.Fatalf("the second restore pruned %v: the saved upgraded data would be lost", res2.Pruned)
	}
	got := idsOf(t, h, "hello")
	if got[saved] != KindPreRestore || got[res2.PreRestoreID] != KindPreRestore || got[pre.ID] != KindPreUpgrade {
		t.Fatalf("backups: %v", got)
	}
	// And the upgraded data can still be brought back.
	if _, err := h.m.RestoreSnapshot(ctx, "hello", saved, RestoreOptions{}); err != nil {
		t.Fatal(err)
	}
	if h.files("hello")["a"] != "upgraded-data" {
		t.Fatalf("data: %v", h.files("hello"))
	}
}

// Beyond 7 days the count rule applies: the newest 3 of a kind stay.
func TestAutoBackupsOlderThanAWeekFollowTheCount(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	var old []string
	for i := 0; i < 4; i++ {
		m, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{Kind: KindPreRestore})
		if err != nil {
			t.Fatal(err)
		}
		old = append(old, m.ID)
	}
	if got := idsOf(t, h, "hello"); len(got) != 4 {
		t.Fatalf("within 7 days all four stay: %v", got)
	}
	h.mu.Lock()
	h.clock = h.clock.Add(8 * 24 * time.Hour)
	h.mu.Unlock()
	plan, err := h.m.PlanBackup(ctx, "hello", BackupOptions{Kind: KindPreRestore})
	if err != nil || len(plan.WillPrune) != 2 || plan.WillPrune[0] != old[1] || plan.WillPrune[1] != old[0] {
		t.Fatalf("plan: %v %+v (want the two oldest %v)", err, plan.WillPrune, old[:2])
	}
	if !strings.Contains(plan.Say, old[0]) {
		t.Fatalf("the plan must name what goes: %s", plan.Say)
	}
	m5, err := h.m.SnapshotBackup(ctx, "hello", BackupOptions{Kind: KindPreRestore})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOf(t, h, "hello")
	if len(got) != 3 || got[m5.ID] == "" || got[old[3]] == "" || got[old[2]] == "" || got[old[0]] != "" || got[old[1]] != "" {
		t.Fatalf("after the count rule: %v", got)
	}
}

// A manual backup is never evicted for room; automatic ones go oldest first,
// only when the disk has no room, the plan says which, and the newest
// automatic backup stays. When that is not enough nothing is removed.
func TestDiskShortEvictsOldestAutoBackupsOnly(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	man, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{})
	a, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{Kind: KindPreUpgrade})
	b, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{Kind: KindPreRestore})
	c, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{Kind: KindPreRestore})
	plan, err := h.m.PlanBackup(ctx, "hello", BackupOptions{})
	if err != nil || len(plan.WillPruneForSpace) != 0 {
		t.Fatalf("room enough: %v %+v", err, plan)
	}
	need := plan.NeedBytes

	// Not enough even after removing every candidate (a and b): refused, nothing removed.
	h.mu.Lock()
	h.free = Space{BackupFree: uint64(need - a.Bytes - b.Bytes - 1), DockerFree: 100 << 30}
	h.mu.Unlock()
	plan, _ = h.m.PlanBackup(ctx, "hello", BackupOptions{})
	if plan.Enough || len(plan.WillPruneForSpace) != 0 {
		t.Fatalf("plan with no hope: %+v", plan)
	}
	_, err = h.m.StartBackup(ctx, "hello", BackupOptions{})
	wantCode(t, err, CodeDisk)
	if got := idsOf(t, h, "hello"); len(got) != 4 {
		t.Fatalf("a refused backup removed something: %v", got)
	}

	// Removing the oldest automatic one (a) is enough: the plan says so, the task does it.
	h.mu.Lock()
	h.free = Space{BackupFree: uint64(need - 1), DockerFree: 100 << 30}
	h.mu.Unlock()
	plan, _ = h.m.PlanBackup(ctx, "hello", BackupOptions{})
	if !plan.Enough || len(plan.WillPruneForSpace) != 1 || plan.WillPruneForSpace[0] != a.ID || !strings.Contains(plan.Say, a.ID) {
		t.Fatalf("plan: %+v", plan)
	}
	if got := idsOf(t, h, "hello"); len(got) != 4 {
		t.Fatal("a plan removed something")
	}
	tv, err := h.m.StartBackup(ctx, "hello", BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	done := waitTask(t, h.m, tv.ID)
	if done.Status != TaskSucceeded || done.Result == nil || len(done.Result.Pruned) != 1 || done.Result.Pruned[0] != a.ID {
		t.Fatalf("task: %+v %+v", done.Result, done.Fault)
	}
	got := idsOf(t, h, "hello")
	if got[a.ID] != "" || got[b.ID] == "" || got[c.ID] == "" || got[man.ID] == "" {
		t.Fatalf("after: %v", got)
	}
}

// A restore with no room for its own save evicts old automatic backups, never
// the backup being restored, and its plan names them.
func TestRestorePlanNamesWhatGoesForSpace(t *testing.T) {
	h := newHarness(t, nil)
	h.installApp("hello", "sha256:img1", map[string]string{"a": "1"})
	ctx := context.Background()
	a, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{Kind: KindPreUpgrade})
	b, _ := h.m.SnapshotBackup(ctx, "hello", BackupOptions{Kind: KindPreRestore})
	plan, err := h.m.PlanRestore(ctx, "hello", b.ID, false)
	if err != nil || !plan.Enough || len(plan.WillPruneForSpace) != 0 {
		t.Fatalf("room enough: %v %+v", err, plan)
	}
	h.mu.Lock()
	h.free = Space{BackupFree: uint64(plan.BackupNeedBytes - 1), DockerFree: 100 << 30}
	h.mu.Unlock()
	// Restoring b (the newest automatic): a is the only candidate.
	plan, _ = h.m.PlanRestore(ctx, "hello", b.ID, false)
	if !plan.Enough || len(plan.WillPruneForSpace) != 1 || plan.WillPruneForSpace[0] != a.ID || !strings.Contains(plan.Say, a.ID) {
		t.Fatalf("plan: %+v", plan)
	}
	// Restoring a itself: the only other candidate is the newest, which stays: no room, refused.
	plan, _ = h.m.PlanRestore(ctx, "hello", a.ID, false)
	if plan.Enough || len(plan.WillPruneForSpace) != 0 {
		t.Fatalf("the backup being restored must not be evicted: %+v", plan)
	}
	res, err := h.m.RestoreSnapshot(ctx, "hello", b.ID, RestoreOptions{})
	if err != nil || len(res.Pruned) != 1 || res.Pruned[0] != a.ID {
		t.Fatalf("restore: %v %+v", err, res)
	}
}
