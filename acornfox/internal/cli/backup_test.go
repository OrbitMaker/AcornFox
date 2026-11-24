package cli

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/apiserver"
	"github.com/acornfox/acornfox/internal/appbackup"
	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// backupRunner is a runner whose one volume holds one file.
type backupRunner struct {
	apiserver.Runner
	mu      sync.Mutex
	content string
	running bool
}

func (b *backupRunner) ListContainers(context.Context, string) ([]runner.ContainerInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return []runner.ContainerInfo{{Name: "af-notes-0123456789ab", Role: runner.RoleApp, Running: b.running}}, nil
}
func (b *backupRunner) StopContainer(context.Context, string, string) error {
	b.mu.Lock()
	b.running = false
	b.mu.Unlock()
	return nil
}
func (b *backupRunner) StartContainer(context.Context, string, string) (runner.ContainerInfo, error) {
	b.mu.Lock()
	b.running = true
	b.mu.Unlock()
	return runner.ContainerInfo{}, nil
}
func (b *backupRunner) RemoveContainer(context.Context, string, string) error { return nil }
func (b *backupRunner) EnsureVolume(context.Context, string, string) error    { return nil }
func (b *backupRunner) RemoveVolume(context.Context, string, string) error    { return nil }
func (b *backupRunner) ListImages(context.Context, string) ([]runner.ImageInfo, error) {
	return []runner.ImageInfo{{ID: "sha256:img1"}}, nil
}
func (b *backupRunner) ExportVolume(_ context.Context, _ runner.VolumeDataRequest, w io.Writer) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	tw := tar.NewWriter(w)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "./f", Mode: 0o644, Size: int64(len(b.content))})
	_, _ = tw.Write([]byte(b.content))
	return tw.Close()
}
func (b *backupRunner) ImportVolume(_ context.Context, _ runner.VolumeDataRequest, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		_, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(tr)
		b.mu.Lock()
		b.content = string(body)
		b.mu.Unlock()
	}
}
func (b *backupRunner) VolumeSizes(context.Context, string) ([]runner.VolumeSize, error) {
	return nil, nil
}

type liveKick struct{ st *state.Store }

func (k liveKick) Kick(app string) {
	ctx := context.Background()
	pend, _ := k.st.PendingDeployments(ctx, app)
	for _, d := range pend {
		_, _ = k.st.UpdateDeployment(ctx, d.ID, func(x *state.Deployment) error { x.ImageID = d.SourceRef; return nil })
		_ = k.st.SetCurrentDeployment(ctx, app, d.ID)
	}
}

func newBackupHarness(t *testing.T) (*harness, *backupRunner) {
	t.Helper()
	old := backupPoll
	backupPoll = time.Millisecond
	t.Cleanup(func() { backupPoll = old })
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	br := &backupRunner{content: "alpha", running: true}
	k := liveKick{st: db}
	mgr := appbackup.New(appbackup.Config{Dir: filepath.Join(dir, "backups"), Store: db, Runner: br, Kick: k.Kick, Poll: time.Millisecond, HealthWait: time.Minute})
	t.Cleanup(mgr.Close)
	ctx := context.Background()
	if _, _, err := db.EnsureApp(ctx, "notes"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.AddVolume(ctx, "notes", "/data", false); err != nil {
		t.Fatal(err)
	}
	d, _, _ := db.CreateDeployment(ctx, state.NewDeployment{App: "notes", SourceKind: state.SourceImage, SourceRef: "sha256:img1", SourceDigest: "sha256:img1"})
	k.Kick("notes")
	_ = d
	ts := httptest.NewServer(apiserver.New(apiserver.Config{Store: db, Kicker: k, Runner: br, UploadDir: dir, PublicHost: "203.0.113.10", Backups: mgr}))
	t.Cleanup(ts.Close)
	h := newHarness(t)
	h.addTarget("dev", ts.URL)
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) { return client.Connect(ctx, tgt) }
	return h, br
}

func TestBackupCommands(t *testing.T) {
	h, br := newBackupHarness(t)

	code, out, _ := h.run("backup", "list", "notes")
	if code != exitOK || !strings.Contains(out, "还没有备份") {
		t.Fatalf("%d %s", code, out)
	}
	// A plan changes nothing.
	code, out, _ = h.run("backup", "create", "notes", "--plan")
	if code != exitOK || !strings.Contains(out, "什么都没动") || !strings.Contains(out, "暂停") {
		t.Fatalf("%d %s", code, out)
	}
	if _, out, _ = h.run("--json", "backup", "list", "notes"); len(decodeJSON(t, out)["backups"].([]any)) != 0 {
		t.Fatalf("plan made a backup: %s", out)
	}

	// Create waits until done and says what it made.
	code, out, _ = h.run("backup", "create", "notes", "--note", "第一份")
	if code != exitOK || !strings.Contains(out, "备份完成") || !strings.Contains(out, "手动") || !strings.Contains(out, "打包数据") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("--json", "backup", "list", "notes")
	m := decodeJSON(t, out)
	list, _ := m["backups"].([]any)
	if code != exitOK || m["ok"] != true || len(list) != 1 {
		t.Fatalf("%d %s", code, out)
	}
	id := list[0].(map[string]any)["id"].(string)
	code, out, _ = h.run("backup", "list", "notes")
	if code != exitOK || !strings.Contains(out, id) || !strings.Contains(out, "第一份") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("backup", "list")
	if code != exitOK || !strings.Contains(out, "notes：1 份") || !strings.Contains(out, "✓ 备份目录") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("backup", "show", "notes", "latest", "--verify")
	if code != exitOK || !strings.Contains(out, id) || !strings.Contains(out, "校验和全部核对过") {
		t.Fatalf("%d %s", code, out)
	}

	// Restoring: without --yes the server refuses and says how.
	br.mu.Lock()
	br.content = "changed"
	br.mu.Unlock()
	code, out, _ = h.run("--json", "backup", "restore", "notes", id)
	if code == exitOK || !strings.Contains(out, "BKP-06") || !strings.Contains(out, "--yes") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("backup", "restore", "notes", "latest", "--plan")
	if code != exitOK || !strings.Contains(out, "什么都没动") || !strings.Contains(out, "要用户同意") {
		t.Fatalf("%d %s", code, out)
	}
	if br.content != "changed" {
		t.Fatal("a plan touched the data")
	}
	code, out, _ = h.run("backup", "restore", "notes", "latest", "--yes")
	if code != exitOK || !strings.Contains(out, "恢复完成") || !strings.Contains(out, "恢复之前的数据另存在备份") {
		t.Fatalf("%d %s", code, out)
	}
	if br.content != "alpha" {
		t.Fatalf("data after restore: %q", br.content)
	}

	// Task output from the JSON of a finished backup.
	code, out, _ = h.run("--json", "backup", "create", "notes", "--no-wait")
	task := decodeJSON(t, out)["task"].(map[string]any)
	if code != exitOK || task["id"] == "" {
		t.Fatalf("%d %s", code, out)
	}
	tid := task["id"].(string)
	var final map[string]any
	for i := 0; i < 2000; i++ {
		_, out, _ = h.run("--json", "backup", "task", tid)
		final = decodeJSON(t, out)
		if final["status"] != "running" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if final["status"] != "succeeded" {
		t.Fatalf("task: %v", final)
	}

	// rm: without --yes it only says what it would do.
	code, out, _ = h.run("backup", "rm", "notes", id)
	if code != exitOK || !strings.Contains(out, "什么都没删") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("backup", "rm", "notes", id, "--yes")
	if code != exitOK || !strings.Contains(out, "已删掉") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("backup", "rm", "notes", id, "--yes")
	if code != exitOK || !strings.Contains(out, "本来就不在") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, _ = h.run("--json", "backup", "show", "notes", "20000101-000000-0000")
	if code == exitOK || !strings.Contains(out, "BKP-04") {
		t.Fatalf("%d %s", code, out)
	}
	code, out, errOut := h.run("backup", "restore", "ghost", "latest", "--yes")
	if code == exitOK || !strings.Contains(errOut, "BKP-04") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	code, _, _ = h.run("backup", "bogus")
	if code != exitUsage {
		t.Fatalf("unknown sub: %d", code)
	}
	code, out, _ = h.run("backup", "--help")
	if code != exitOK || !strings.Contains(out, "restore 应用 编号") {
		t.Fatalf("%d %s", code, out)
	}
}
