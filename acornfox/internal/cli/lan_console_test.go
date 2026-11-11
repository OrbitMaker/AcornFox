package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

func lanHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	if err := saveProject(h.workDir, &projectFile{Target: "dev"}); err != nil {
		t.Fatalf("saveProject: %v", err)
	}
	return h
}

func TestLANConsoleStatusSaysWhoSetThePassword(t *testing.T) {
	h := lanHarness(t)
	at := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	h.api.lanStatus = client.LANConsoleStatus{Enabled: true, URL: "http://192.168.1.10:18802", Port: 18802, PasswordSet: true, PasswordSetAt: &at, PasswordSetFrom: "192.168.1.23", Devices: 2}
	code, out, errOut := h.run("lan-console", "status")
	if code != exitOK {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	for _, want := range []string{"已开启 http://192.168.1.10:18802", "从 192.168.1.23 设的", "acornfox lan-console reset-password", "记住的设备：2 台"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status lacks %q:\n%s", want, out)
		}
	}
	h.api.lanStatus = client.LANConsoleStatus{Enabled: true, URL: "http://192.168.1.10:18802", Port: 18802}
	if _, out, _ := h.run("lan-console", "status"); !strings.Contains(out, "还没设") || !strings.Contains(out, "LAN-CONSOLE-03") {
		t.Fatalf("an open LAN console without a password must be called out:\n%s", out)
	}
	if _, out, _ := h.run("--json", "lan-console", "status"); !strings.Contains(out, `"password_set":false`) || !strings.Contains(out, `"devices":0`) {
		t.Fatalf("json status = %s", out)
	}
	h.api.lanStatus = client.LANConsoleStatus{Enabled: true, PasswordSet: true, GeneratedPassword: true}
	if _, out, _ := h.run("lan-console", "status"); !strings.Contains(out, "开启时生成的那个") {
		t.Fatalf("a generated password from an older version must be named:\n%s", out)
	}
}

func TestLANConsoleEnableResetDevicesForget(t *testing.T) {
	h := lanHarness(t)
	h.api.lanStatus = client.LANConsoleStatus{Enabled: true, URL: "http://192.168.1.10:18802", Port: 18802}
	if code, out, _ := h.run("lan-console", "enable"); code != exitOK || !strings.Contains(out, "还没有密码：请现在就") || strings.Contains(out, "登录密码（只显示这一次") {
		t.Fatalf("enable = %d\n%s", code, out)
	}
	if code, out, _ := h.run("lan-console", "reset-password"); code != exitOK || !strings.Contains(out, "密码已清掉") || !strings.Contains(out, "http://192.168.1.10:18802") {
		t.Fatalf("reset-password = %d\n%s", code, out)
	}
	h.api.lanDevices = []client.LANConsoleDevice{{ID: "3f9c2a1b7d0e", Name: "Mac · Chrome", LastIP: "192.168.1.23", LastUsedAt: time.Now(), ExpiresAt: time.Now().Add(90 * 24 * time.Hour)}}
	if code, out, _ := h.run("lan-console", "devices"); code != exitOK || !strings.Contains(out, "3f9c2a1b7d0e\tMac · Chrome") {
		t.Fatalf("devices = %d\n%s", code, out)
	}
	h.api.lanRemoved = 1
	if code, out, _ := h.run("lan-console", "forget", "3f9c2a1b7d0e"); code != exitOK || !strings.Contains(out, "已让 1 台设备失效") {
		t.Fatalf("forget = %d\n%s", code, out)
	}
	h.api.lanRemoved = 0
	if code, out, _ := h.run("lan-console", "forget", "gone"); code != exitOK || !strings.Contains(out, "什么都没改") {
		t.Fatalf("forgetting a device that is gone must not fail: %d\n%s", code, out)
	}
	if code, _, _ := h.run("lan-console", "forget", "--all"); code != exitOK {
		t.Fatalf("forget --all = %d", code)
	}
	if got := strings.Join(h.api.lanCalls, "|"); got != "enable reset=false|reset-password|forget 3f9c2a1b7d0e|forget gone|forget " {
		t.Fatalf("calls = %s", got)
	}
}

func TestLANConsoleUsage(t *testing.T) {
	h := lanHarness(t)
	for _, args := range [][]string{{"lan-console"}, {"lan-console", "forget"}, {"lan-console", "forget", "x", "--all"}, {"lan-console", "nope"}, {"lan-console", "status", "extra"}} {
		if code, _, _ := h.run(args...); code != exitUsage {
			t.Fatalf("%v = %d, want a usage error", args, code)
		}
	}
	if len(h.api.lanCalls) != 0 {
		t.Fatalf("usage errors must not call the server: %v", h.api.lanCalls)
	}
	if code, out, _ := h.run("lan-console", "--help"); code != exitOK || !strings.Contains(out, "reset-password") || !strings.Contains(out, "写操作") || !strings.Contains(out, "例子") {
		t.Fatalf("help = %d\n%s", code, out)
	}
}
