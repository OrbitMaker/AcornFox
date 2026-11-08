package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

func shortStopWait(t *testing.T, timeout time.Duration) {
	t.Helper()
	oldInterval, oldTimeout, oldStart := stopPollInterval, stopWaitTimeout, startWaitTimeout
	stopPollInterval, stopWaitTimeout, startWaitTimeout = time.Millisecond, timeout, timeout
	t.Cleanup(func() { stopPollInterval, stopWaitTimeout, startWaitTimeout = oldInterval, oldTimeout, oldStart })
}

// `stop` must not report success until the container is observed stopped.
func TestStopWaitsForObservedStopped(t *testing.T) {
	shortStopWait(t, time.Second)
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	stopped := false
	h.api.stopFn = func(ctx context.Context, app string) error { stopped = true; return nil }
	polls := 0
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) {
		polls++
		state := "running"
		if stopped && polls >= 3 {
			state = "stopped"
		}
		return client.App{Name: app, ObservedState: state}, nil
	}
	code, out, errOut := h.run("--app", "shop", "stop")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if polls < 3 || !strings.Contains(out, "已停止应用 shop") {
		t.Fatalf("polls %d, output %q", polls, out)
	}
}

func TestStopTimeoutSaysStillStopping(t *testing.T) {
	shortStopWait(t, 20*time.Millisecond)
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	h.api.appFn = func(ctx context.Context, app string) (client.App, error) {
		return client.App{Name: app, ObservedState: "running"}, nil
	}
	code, out, errOut := h.run("--app", "shop", "stop")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if strings.Contains(out, "已停止应用") || !strings.Contains(out, "仍在停止中（当前状态：running）") {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestHelpPrintsUsage(t *testing.T) {
	for _, arg := range []string{"help", "-h", "--help"} {
		h := newHarness(t)
		code, out, _ := h.run(arg)
		if code != exitOK || !strings.Contains(out, "用法：acornfox") || !strings.Contains(out, "public-console") {
			t.Fatalf("%s: exit %d, output %q", arg, code, out)
		}
	}
}

// apps tells how to open a local-only app: acornfox open on the user's own
// computer; the LAN switch only when the server offers it. --json carries
// the exposure and the open hint.
func TestAppsLocalOnlyNote(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://127.0.0.1:18800")
	h.api.appsFn = func(ctx context.Context) ([]client.App, error) {
		return []client.App{
			{Name: "new-api", URL: "http://127.0.0.1:18810", Exposure: "local", LocalOnly: true,
				OpenHint: &client.OpenHint{LANAddress: "192.168.1.20", LANURL: "http://192.168.1.20:18810", HasLogin: true, CanLAN: true, Recommend: "lan", Command: "acornfox open new-api"}},
			{Name: "web", URL: "http://127.0.0.1:18811", Exposure: "local", LocalOnly: true,
				OpenHint: &client.OpenHint{Recommend: "tunnel", Command: "acornfox open web"}},
		}, nil
	}
	code, out, _ := h.run("apps")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "acornfox open new-api") || !strings.Contains(lines[0], "--expose lan") ||
		!strings.Contains(lines[0], "http://192.168.1.20:18810") || !strings.Contains(lines[1], "acornfox open web") || strings.Contains(lines[1], "--expose") {
		t.Fatalf("output:\n%s", out)
	}
	code, out, _ = h.run("--json", "apps")
	if code != exitOK || !strings.Contains(out, `"exposure":"local"`) || !strings.Contains(out, `"recommend":"lan"`) {
		t.Fatalf("json: %s", out)
	}
}

// The help lists the app center's platform variables and open APP, and no
// longer promises a Mac local mode.
func TestHelpMentionsOpenAppAndPlatformVars(t *testing.T) {
	h := newHarness(t)
	_, out, _ := h.run("help")
	for _, want := range []string{"catalog platform-vars", "open [应用]", "Mac 上只有命令行客户端"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	for _, not := range []string{"即将支持", "catalog connect", "catalog sources"} {
		if strings.Contains(out, not) {
			t.Errorf("help still says %q", not)
		}
	}
	_, _, errOut := h.run()
	if !strings.Contains(errOut, "app set") || strings.Contains(errOut, "、set、") {
		t.Errorf("missing-command list: %s", errOut)
	}
}

func TestInstallHelpShowsAppUsage(t *testing.T) {
	h := newHarness(t)
	for _, args := range [][]string{{"install", "-h"}, {"install", "--china", "-h"}} {
		code, out, _ := h.run(args...)
		if code != exitOK || !strings.Contains(out, "用法：acornfox install 应用") || !strings.Contains(out, "install.sh") {
			t.Fatalf("%v: exit %d out %s", args, code, out)
		}
	}
}
