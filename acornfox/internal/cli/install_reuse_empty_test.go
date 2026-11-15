package cli

import (
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
)

// An empty line read for 项=- is refused, never taken as "not given" (an
// install would then have the platform generate the secret; services set
// would clear the item): install, services add and services set all say
// "读到的是空值" and send nothing.
func TestEmptyStdinLineIsRefused(t *testing.T) {
	h, f := installHarness(t)
	code, out, errOut := h.runStdin("\n", "install", "demo", "--no-wait", "--set", "admin_password=-")
	if code != exitUsage || len(f.got) != 0 || !strings.Contains(errOut, "读到的是空值") || !strings.Contains(errOut, "去掉 --set admin_password=-") {
		t.Fatalf("install: exit %d sent %v\n%s\n%s", code, f.got, out, errOut)
	}
	// A plain parameter's "-" is its value: nothing is read.
	if code, _, errOut := h.runStdin("\n", "install", "demo", "--no-wait", "--set", "sep=-"); code != exitOK || f.got[0].Params["sep"] != "-" {
		t.Fatalf("plain parameter: exit %d %v %s", code, f.got, errOut)
	}

	s := newSvcHarness(t)
	code, _, errOut = s.runIn("\n", "services", "add", "smtp", "--name", "m", "--set", "host=mail.example.com", "--set", "password=-")
	if code != exitUsage || !strings.Contains(errOut, "password=- 从标准输入读到的是空值") {
		t.Fatalf("services add: exit %d %s", code, errOut)
	}
	if code, out, _ := s.run("--json", "services", "list"); code != exitOK || strings.Contains(out, `"name":"m"`) {
		t.Fatalf("a refused add registered something: %s", out)
	}
	if code, _, errOut := s.runIn("pw-old-1111\n", "services", "add", "smtp", "--name", "mail", "--set", "host=mail.example.com", "--set", "password=-"); code != exitOK {
		t.Fatal(errOut)
	}
	code, _, errOut = s.runIn("\r\n", "services", "set", "mail", "password=-")
	if code != exitUsage || !strings.Contains(errOut, "读到的是空值") || !strings.Contains(errOut, "password=（等号后面什么都不写）") {
		t.Fatalf("services set: exit %d %s", code, errOut)
	}
	if v := s.stored("mail"); v["password"] != "pw-old-1111" {
		t.Fatalf("the item changed: %v", v)
	}
}

// A secret parameter written on the install command line is taken as it is,
// kept out of the output, and the user is told it stays in the shell's
// history (as the services commands do). A plain parameter is not.
func TestInstallSecretOnCommandLine(t *testing.T) {
	h, f := installHarness(t)
	const pw = "pw-cmdline-7788"
	code, out, errOut := h.runStdin("", "install", "demo", "--no-wait", "--set", "admin_password="+pw, "--set", "sep=x")
	if code != exitOK || len(f.got) != 1 || f.got[0].Params["admin_password"] != pw {
		t.Fatalf("exit %d %v\n%s\n%s", code, f.got, out, errOut)
	}
	if !strings.Contains(errOut, "admin_password 是保密参数，写在命令行里会留在命令历史里") || strings.Contains(errOut, "sep 是保密") {
		t.Fatalf("reminder:\n%s", errOut)
	}
	noSecret(t, pw, out, errOut)
	// --plan says it too.
	if _, _, errOut := h.runStdin("", "install", "demo", "--plan", "--set", "admin_password="+pw); !strings.Contains(errOut, "会留在命令历史里") {
		t.Fatalf("plan reminder:\n%s", errOut)
	}
}

// install --name APP --reuse-data without the catalog app sends no app: the
// server finds which one it was. Without --reuse-data (or without --name)
// the app must be named.
func TestInstallReuseByNameOnly(t *testing.T) {
	h, f := installHarness(t)
	code, out, errOut := h.runStdin("", "install", "--name", "my-gw", "--reuse-data", "--no-wait")
	if code != exitOK || len(f.got) != 1 || f.got[0].App != "" || f.got[0].Name != "my-gw" || !f.got[0].ReuseData {
		t.Fatalf("exit %d %+v\n%s\n%s", code, f.got, out, errOut)
	}
	if code, _, _ := h.runStdin("", "install", "--name", "my-gw", "--reuse-data", "--plan"); code != exitOK || len(f.planned) != 1 || f.planned[0].App != "" {
		t.Fatalf("plan: exit %d %+v", code, f.planned)
	}
	for _, args := range [][]string{{"install", "--name", "my-gw", "--no-wait"}, {"install", "--reuse-data", "--no-wait"}} {
		if code, _, errOut := h.runStdin("", args...); code != exitUsage || !strings.Contains(errOut, "--name 应用名 --reuse-data") {
			t.Fatalf("%v: exit %d %s", args, code, errOut)
		}
	}
	if _, out, _ := h.run("install", "-h"); !strings.Contains(out, "acornfox install --name 应用名 --reuse-data") || !strings.Contains(out, "删除时保留下来的记录") {
		t.Fatalf("help:\n%s", out)
	}
}

// The catalog list says the same words as the server when nothing can be
// installed (no app, or apps of a former format only), and does not send the
// user to sync.
func TestCatalogListNothingToInstall(t *testing.T) {
	h, f := varsHarness(t)
	for _, raw := range []string{
		`{"serial":4,"origin":"snapshot","sync":{"ok":true,"message":"目录已是最新"},"apps":[],"recipes":[]}`,
		`{"serial":5,"origin":"cache","sync":{"ok":true,"message":"目录已是最新"},"apps":[{"id":"old-app","version":"v1","name":"旧应用","summary":"x","supported":true,"installed":[],"source":{"id":"public","name":"公共目录"},"memory_mb":{"min":256},"needs_platform":"","former":true,"note":"这是旧格式的清单，这一版装不了，等这个应用按新格式重新上架"}],"recipes":[]}`,
	} {
		f.catalogRaw = raw
		code, out, errOut := h.run("catalog", "list")
		if code != exitOK || !strings.Contains(out, catalog.NothingToInstall) || strings.Contains(out, "catalog sync") || errOut != "" {
			t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
		}
	}
}
