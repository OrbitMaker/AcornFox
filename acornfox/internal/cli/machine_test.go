package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/machine"
)

// machineFake is the fake API with a machine view (the server side of
// `acornfox machine`).
type machineFake struct {
	*fakeAPI
	view  machine.View
	err   error
	calls int
}

func (m *machineFake) Machine(context.Context) (client.MachineView, error) {
	m.calls++
	if m.err != nil {
		return client.MachineView{}, m.err
	}
	return client.MachineView{View: m.view}, nil
}

func sampleMachine() machine.View {
	v := machine.View{
		Schema: 1, GeneratedAt: "2026-11-24T10:00:00Z",
		Machine: machine.Machine{Name: "host-name-on-the-server", Version: "0.6.4", Region: "cn"},
		Apps: []machine.App{{ID: "memos", Instance: "memos", Name: "Memos", Tagline: "随手记笔记", Source: "catalog", State: "running",
			URL: "http://192.168.1.5:18810", Exposure: "lan", Manual: machine.Manual{Has: true, Kind: "app", Version: "0.31.0"}}},
		Platform: machine.Platform{Backup: true, Upgrade: true, Reapply: true},
	}
	machine.Normalize(&v)
	return v
}

func newMachineHarness(t *testing.T) (*harness, *machineFake) {
	t.Helper()
	h := newHarness(t)
	mf := &machineFake{fakeAPI: h.api, view: sampleMachine()}
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) { return mf, nil }
	return h, mf
}

func TestMachineCommandJSONUsesTheRegisteredName(t *testing.T) {
	h, mf := newMachineHarness(t)
	h.addTarget("家里的服务器", "http://127.0.0.1:18800")
	code, out, errOut := h.run("--json", "machine")
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	m := decodeJSON(t, out)
	mc := m["machine"].(map[string]any)
	if m["ok"] != true || m["schema"] != float64(1) || mc["name"] != "家里的服务器" || mc["version"] != "0.6.4" || mc["region"] != "cn" {
		t.Fatalf("%s", out)
	}
	apps := m["apps"].([]any)
	if len(apps) != 1 || apps[0].(map[string]any)["manual"].(map[string]any)["has"] != true {
		t.Fatalf("apps: %s", out)
	}
	for _, k := range []string{"solutions", "services", "warnings"} {
		if l, ok := m[k].([]any); !ok || len(l) != 0 {
			t.Fatalf("%s 要是空列表：%s", k, out)
		}
	}
	if mf.calls != 1 {
		t.Fatalf("只读一次：%d", mf.calls)
	}
	// Human text names the server and the app.
	code, out, _ = h.run("machine")
	if code != exitOK || !strings.Contains(out, "家里的服务器") || !strings.Contains(out, "memos") || !strings.Contains(out, "有手册") {
		t.Fatalf("文字：%d %s", code, out)
	}
}

func TestMachineCommandRefusals(t *testing.T) {
	h := newHarness(t)
	h.addTarget("n7", "http://127.0.0.1:18800")
	// A server whose client has no machine route: "server too old".
	code, out, _ := h.run("--json", "machine")
	if code != exitServerError {
		t.Fatalf("exit %d: %s", code, out)
	}
	if d := decodeJSON(t, out)["diagnosis"].(map[string]any); d["code"] != client.CodeServerTooOld {
		t.Fatalf("%s", out)
	}
	if code, _, _ = h.run("--json", "machine", "extra"); code != exitUsage {
		t.Fatalf("多余参数 exit %d", code)
	}
	// No server registered at all.
	h2 := newHarness(t)
	if code, _, _ = h2.run("--json", "machine"); code != exitUsage {
		t.Fatalf("没有登记服务器 exit %d", code)
	}
}

func TestSkillInstallWritesMachineParts(t *testing.T) {
	h, mf := newMachineHarness(t)
	h.addTarget("n7", "http://127.0.0.1:18800")
	dir := t.TempDir()
	code, out, errOut := h.run("--json", "skill", "install", "--dir", dir)
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	m := decodeJSON(t, out)
	parts := m["machines"].(map[string]any)["refreshed"].([]any)
	if len(parts) != 1 || m["installed"] == nil {
		t.Fatalf("%s", out)
	}
	path := filepath.Join(dir, "acornfox", "machine-n7.md")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.HasPrefix(text, "<!-- 生成时间：2026-11-24T10:00:00Z；服务器：n7；AcornFox 0.6.4；") || !strings.Contains(text, "# 服务器 n7 现在能做什么") ||
		!strings.Contains(text, "http://192.168.1.5:18810") {
		t.Fatalf("分册：%s", text)
	}
	// The part says "n7", the name the user registered, not the server's host name.
	if strings.Contains(text, "host-name-on-the-server") {
		t.Fatalf("分册里要用登记的名字：%s", text)
	}
	if mf.calls != 1 {
		t.Fatalf("calls %d", mf.calls)
	}
	// Human mode says it wrote them.
	if code, out, _ = h.run("skill", "install", "--dir", dir); code != exitOK || !strings.Contains(out, "分册") {
		t.Fatalf("文字：%d %s", code, out)
	}
}

// A server that cannot be reached is only reported; the Skill is installed.
func TestSkillInstallSurvivesAnUnreachableServer(t *testing.T) {
	h := newHarness(t)
	h.addTarget("n7", "http://127.0.0.1:18800")
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) {
		return nil, &client.Error{Diag: client.Diagnosis{Stage: "connect", Code: "host_unreachable", Message: "连不上"}}
	}
	dir := t.TempDir()
	code, out, errOut := h.run("--json", "skill", "install", "--dir", dir)
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	failed := decodeJSON(t, out)["machines"].(map[string]any)["failed"].([]any)
	if len(failed) != 1 || failed[0].(map[string]any)["code"] != machine.CodeNoServer || failed[0].(map[string]any)["server"] != "n7" {
		t.Fatalf("%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "acornfox", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "acornfox", "machine-n7.md")); err == nil {
		t.Fatal("连不上的服务器不该有分册")
	}
	if code, _, errOut = h.run("skill", "install", "--dir", dir); code != exitOK || !strings.Contains(errOut, "MACHINE-03") || !strings.Contains(errOut, "n7") {
		t.Fatalf("文字：%d %s", code, errOut)
	}
}

func TestSkillRefresh(t *testing.T) {
	h, mf := newMachineHarness(t)
	h.addTarget("a", "http://127.0.0.1:18800")
	h.addTarget("b", "http://127.0.0.1:18801")
	dir := t.TempDir()

	// Not installed yet: a usage error that says what to do.
	code, out, _ := h.run("--json", "skill", "refresh", "--dir", dir)
	if code != exitUsage || !strings.Contains(out, "skill install") {
		t.Fatalf("没装 Skill：%d %s", code, out)
	}
	if code, _, _ = h.run("--json", "skill", "install", "--dir", dir); code != exitOK {
		t.Fatal("install")
	}
	sk := filepath.Join(dir, "acornfox")
	for _, n := range []string{"machine-a.md", "machine-b.md"} {
		if _, err := os.Stat(filepath.Join(sk, n)); err != nil {
			t.Fatalf("install 要写 %s：%v", n, err)
		}
	}
	// A file someone else keeps there is left alone; an old part of a server
	// that is no longer registered goes.
	if err := os.WriteFile(filepath.Join(sk, "machine-notes.md"), []byte("我自己的笔记\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tf, _ := loadTargets(h.configDir)
	delete(tf.Targets, "b")
	if err := saveTargets(h.configDir, tf); err != nil {
		t.Fatal(err)
	}
	mf.view.GeneratedAt = "2026-11-25T08:00:00Z"
	code, out, errOut := h.run("--json", "skill", "refresh", "--dir", dir)
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	m := decodeJSON(t, out)
	if len(m["refreshed"].([]any)) != 1 || len(m["removed"].([]any)) != 1 || len(m["failed"].([]any)) != 0 {
		t.Fatalf("%s", out)
	}
	if _, err := os.Stat(filepath.Join(sk, "machine-b.md")); err == nil {
		t.Fatal("不再登记的服务器的旧分册要删")
	}
	if b, err := os.ReadFile(filepath.Join(sk, "machine-notes.md")); err != nil || string(b) != "我自己的笔记\n" {
		t.Fatalf("别人的文件不能动：%v %q", err, b)
	}
	if b, _ := os.ReadFile(filepath.Join(sk, "machine-a.md")); !strings.Contains(string(b), "2026-11-25T08:00:00Z") {
		t.Fatalf("没刷新：%s", b)
	}
	// Repeating changes nothing (no error).
	if code, _, _ = h.run("--json", "skill", "refresh", "--dir", dir); code != exitOK {
		t.Fatalf("重复运行 exit %d", code)
	}
	// One server only: nothing is removed.
	if code, out, _ = h.run("--json", "--target", "a", "skill", "refresh", "--dir", dir); code != exitOK || len(decodeJSON(t, out)["removed"].([]any)) != 0 {
		t.Fatalf("只刷新一台：%d %s", code, out)
	}
	// Every server failing is exit 1 with MACHINE-03; a name that is not registered too.
	mf.err = errors.New("boom")
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) {
		return nil, &client.Error{Diag: client.Diagnosis{Stage: "connect", Code: "host_unreachable", Message: "连不上"}}
	}
	code, out, _ = h.run("--json", "skill", "refresh", "--dir", dir)
	if code != exitOpFailed || decodeJSON(t, out)["failed"].([]any)[0].(map[string]any)["code"] != machine.CodeNoServer {
		t.Fatalf("全都失败：%d %s", code, out)
	}
	if code, out, _ = h.run("--json", "--target", "nope", "skill", "refresh", "--dir", dir); code != exitOpFailed || !strings.Contains(out, "找不到服务器 nope") {
		t.Fatalf("没登记的名字：%d %s", code, out)
	}
}

// A part cannot be written into a place that is not a directory (MACHINE-02).
func TestSkillRefreshWriteFailure(t *testing.T) {
	h, _ := newMachineHarness(t)
	h.addTarget("n7", "http://127.0.0.1:18800")
	dir := t.TempDir()
	if code, _, _ := h.run("--json", "skill", "install", "--dir", dir); code != exitOK {
		t.Fatal("install")
	}
	part := filepath.Join(dir, "acornfox", "machine-n7.md")
	if err := os.Remove(part); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(part, 0o755); err != nil { // a directory where the file goes
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(part, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := h.run("--json", "skill", "refresh", "--dir", dir)
	f := decodeJSON(t, out)["failed"].([]any)
	if code != exitOpFailed || len(f) != 1 || f[0].(map[string]any)["code"] != machine.CodeCacheWrite {
		t.Fatalf("%d %s", code, out)
	}
}

func TestChangesMachine(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		args []string
		want bool
	}{
		{"install", []string{"memos"}, true},
		{"install", []string{"memos", "--plan"}, false},
		{"install", []string{"status", "task-1"}, false},
		{"install", []string{"-h"}, false},
		{"delete", []string{"--volumes"}, true},
		{"deploy", nil, true},
		{"apps", nil, false},
		{"apps", []string{"vars", "memos"}, false},
		{"apps", []string{"upgrade", "memos"}, true},
		{"apps", []string{"upgrade", "memos", "--plan"}, false},
		{"apps", []string{"rollback", "memos"}, true},
		{"apps", []string{"set", "memos", "a=b"}, true},
		{"apps", []string{"reapply", "memos"}, true},
		{"apps", []string{"outdated"}, false},
		{"backup", []string{"restore", "memos", "latest", "--yes"}, true},
		{"backup", []string{"restore", "memos", "latest", "--plan"}, false},
		{"backup", []string{"create", "memos"}, false},
		{"services", []string{"add", "smtp"}, true},
		{"services", []string{"list"}, false},
		{"services", []string{"rm", "x"}, true},
		{"app", []string{"set", "--memory", "512"}, true},
		{"domain", []string{"add", "a.example.com"}, true},
		{"domain", []string{"list"}, false},
		{"status", nil, false},
		{"machine", nil, false},
		{"skill", []string{"refresh"}, false},
	} {
		if got := changesMachine(c.cmd, c.args); got != c.want {
			t.Errorf("changesMachine(%q, %v) = %v, want %v", c.cmd, c.args, got, c.want)
		}
	}
}

// After a command that changed something and succeeded the command line
// writes the part again, silently, into the Skill directories that exist; the
// tests' own config directory switches this off (so a test never writes into
// the real Skill directory).
func TestAutoRefreshMachine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	skill := filepath.Join(home, ".claude", "skills", "acornfox")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A workbench without the Skill installed gets nothing.
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := saveTargets("", &targetsFile{Default: "n7", Targets: map[string]*client.Target{"n7": {URL: "http://127.0.0.1:18800"}}}); err != nil {
		t.Fatal(err)
	}
	mf := &machineFake{fakeAPI: &fakeAPI{}, view: sampleMachine()}
	newApp := func(configDir string) *app {
		return &app{out: &outputWriter{stdout: os.Stderr, stderr: os.Stderr}, configDir: configDir, workDir: t.TempDir(),
			connect: func(ctx context.Context, tgt client.Target) (client.API, error) { return mf, nil }}
	}
	part := filepath.Join(skill, "machine-n7.md")

	newApp(t.TempDir()).autoRefreshMachine(context.Background(), "delete", nil) // a test config dir: off
	if _, err := os.Stat(part); err == nil {
		t.Fatal("测试用的配置目录下不能写分册")
	}
	newApp("").autoRefreshMachine(context.Background(), "status", nil) // not a command that changes things
	if _, err := os.Stat(part); err == nil {
		t.Fatal("只读命令不刷新")
	}
	newApp("").autoRefreshMachine(context.Background(), "install", []string{"memos", "--plan"})
	if _, err := os.Stat(part); err == nil {
		t.Fatal("试跑不刷新")
	}
	newApp("").autoRefreshMachine(context.Background(), "install", []string{"memos"})
	if b, err := os.ReadFile(part); err != nil || !strings.Contains(string(b), "服务器：n7") {
		t.Fatalf("装完要刷新：%v %s", err, b)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "acornfox", "machine-n7.md")); err == nil {
		t.Fatal("没装 Skill 的工作台不写")
	}
	// A server that cannot answer: silent, the old part stays.
	before, _ := os.ReadFile(part)
	mf.err = errors.New("down")
	newApp("").autoRefreshMachine(context.Background(), "delete", nil)
	if after, _ := os.ReadFile(part); string(after) != string(before) {
		t.Fatal("读不到时旧分册不动")
	}
}
