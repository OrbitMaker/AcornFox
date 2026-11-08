package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/setup"
)

// On a Mac (local mode off in this release) setup against the Mac's own
// server stops with SETUP-MAC-01 before dialing; setup against a remote
// server is not affected. On Linux nothing changes.
func TestSetupMacLocalStopped(t *testing.T) {
	if setup.MacLocalEnabled {
		t.Skip("built with the Mac local mode on")
	}
	old := platform.GOOS
	t.Cleanup(func() { platform.GOOS = old })

	h := newHarness(t)
	dialed := ""
	h.connect = func(_ context.Context, tgt client.Target) (client.API, error) {
		dialed = tgt.URL
		return h.api, nil
	}
	h.addTarget("local", "unix:///Users/me/Library/Application Support/AcornFox/api.sock")
	h.addTarget("srv", "ssh://root@203.0.113.10")

	platform.GOOS = "darwin"
	code, out, _ := h.run("--json", "setup", "--runtime", "builtin")
	if code != exitUsage || dialed != "" {
		t.Fatalf("exit %d dialed %q\n%s", code, dialed, out)
	}
	d := decodeJSON(t, out)["diagnosis"].(map[string]any)
	if d["code"] != "SETUP-MAC-01" || d["stage"] != "local" || !strings.Contains(d["message"].(string), "Mac 版只有命令行客户端") ||
		!strings.Contains(d["hint"].(string), "acornfox target add") {
		t.Fatalf("diagnosis %v", d)
	}
	code, _, errOut := h.run("setup")
	if code != exitUsage || !strings.Contains(errOut, "故障编号 SETUP-MAC-01") || dialed != "" {
		t.Fatalf("human: exit %d %q", code, errOut)
	}

	// A remote server: dialed as before (the fake has no setup API).
	h.run("--target", "srv", "setup", "--status")
	if dialed != "ssh://root@203.0.113.10" {
		t.Fatalf("remote not dialed: %q", dialed)
	}

	// Linux: the local socket is dialed as before.
	platform.GOOS, dialed = "linux", ""
	h.run("--target", "local", "setup", "--status")
	if !strings.HasPrefix(dialed, "unix://") {
		t.Fatalf("linux local not dialed: %q", dialed)
	}
}
