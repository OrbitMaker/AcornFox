package cli

import (
	"strings"
	"testing"
)

// install --expose sends the exposure (install and plan); a value that is
// none of local, lan, public is refused before anything is sent; the help
// says what it does and that it is a write to ask about.
func TestInstallExposeFlag(t *testing.T) {
	h, f := installHarness(t)
	code, out, errOut := h.runStdin("", "install", "demo", "--expose", "lan", "--no-wait")
	if code != exitOK || len(f.got) != 1 || f.got[0].Expose != "lan" {
		t.Fatalf("exit %d %+v\n%s\n%s", code, f.got, out, errOut)
	}
	if code, _, _ := h.runStdin("", "install", "demo", "--expose", "public", "--plan"); code != exitOK || len(f.planned) != 1 || f.planned[0].Expose != "public" {
		t.Fatalf("plan: exit %d %+v", code, f.planned)
	}
	if code, _, _ := h.runStdin("", "install", "demo", "--no-wait"); code != exitOK || f.got[1].Expose != "" {
		t.Fatalf("without: %+v", f.got)
	}
	for _, bad := range []string{"LAN", "internal", "afx_cloud", "all"} {
		if code, _, errOut := h.runStdin("", "install", "demo", "--expose", bad, "--no-wait"); code != exitUsage || !strings.Contains(errOut, "--expose 只能是") {
			t.Fatalf("%s: exit %d %s", bad, code, errOut)
		}
	}
	if len(f.got) != 2 {
		t.Fatalf("a refused exposure was sent: %+v", f.got)
	}
	if _, out, _ := h.run("install", "-h"); !strings.Contains(out, "--expose 对外方式") || !strings.Contains(out, "要先问用户") || !strings.Contains(out, "APP-VARS-01") {
		t.Fatalf("help:\n%s", out)
	}
}
