package main

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// --json before a command handled in this package reaches that command, so
// "acornfox --json doctor" is "acornfox doctor --json" instead of an unknown
// command from internal/cli.
func TestMainCommandArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		cmd  string
		rest []string
	}{
		{[]string{"--json", "doctor"}, "doctor", []string{"--json"}},
		{[]string{"doctor", "--json"}, "doctor", []string{"--json"}},
		{[]string{"-json", "identity", "show"}, "identity", []string{"show", "--json"}},
		{[]string{"--json", "identity", "show", "--json"}, "identity", []string{"show", "--json"}},
		{[]string{"uninstall", "--purge"}, "uninstall", []string{"--purge"}},
		{[]string{"--json", "identity"}, "identity", []string{"--json"}},
		{[]string{"--json", "status"}, "", nil},
		{[]string{"status", "doctor"}, "", nil},
		{nil, "", nil},
	} {
		cmd, rest := mainCommandArgs(c.args)
		if cmd != c.cmd || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("%v: got %q %v, want %q %v", c.args, cmd, rest, c.cmd, c.rest)
		}
	}
}

func runJSON(t *testing.T, args ...string) (map[string]any, int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	rc := run(args, strings.NewReader(""), &out, &errb)
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("%v: not JSON (rc %d): %q %q", args, rc, out.String(), errb.String())
	}
	return m, rc, errb.String()
}

func TestLeadingJSONReachesMainCommands(t *testing.T) {
	dir := t.TempDir()
	m, rc, _ := runJSON(t, "--json", "identity", "show", "--data-dir", dir)
	if rc != 0 || m["ok"] != true || m["exists"] != false {
		t.Fatalf("identity: rc %d %v", rc, m)
	}
	// version answers JSON with --json on either side.
	for _, args := range [][]string{{"--json", "version"}, {"version", "--json"}} {
		m, rc, _ = runJSON(t, args...)
		if rc != 0 || m["ok"] != true || m["version"] == nil {
			t.Fatalf("%v: rc %d %v", args, rc, m)
		}
	}
	// Commands without JSON output say so (and do nothing).
	for _, args := range [][]string{{"--json", "uninstall"}, {"uninstall", "--json", "--yes"}} {
		m, rc, _ = runJSON(t, args...)
		diag, _ := m["diagnosis"].(map[string]any)
		if rc != 2 || m["ok"] != false || diag == nil || !strings.Contains(diag["message"].(string), "没有 JSON 输出") {
			t.Fatalf("%v: rc %d %v", args, rc, m)
		}
	}
	// --json before identity's subcommand, or identity without one: JSON,
	// never "未知子命令：--json".
	m, rc, _ = runJSON(t, "identity", "--json", "show", "--data-dir", dir)
	if rc != 0 || m["ok"] != true || m["exists"] != false {
		t.Fatalf("identity --json show: rc %d %v", rc, m)
	}
	for _, args := range [][]string{{"--json", "identity"}, {"identity", "--json"}, {"-json", "identity", "--json"}} {
		m, rc, _ = runJSON(t, args...)
		diag, _ := m["diagnosis"].(map[string]any)
		if rc != 2 || m["ok"] != false || diag == nil || diag["hint"] != "acornfox identity show --json" {
			t.Fatalf("%v: rc %d %v", args, rc, m)
		}
	}
	// Written after --target, identity points at a command that works.
	m, rc, _ = runJSON(t, "--target", "x", "--json", "identity")
	diag, _ := m["diagnosis"].(map[string]any)
	if rc != 2 || diag == nil || !strings.Contains(diag["message"].(string), "acornfox identity show --json") {
		t.Fatalf("identity after --target: rc %d %v", rc, m)
	}
	// Written after --target, doctor gets a clear message, not "unknown command".
	m, rc, _ = runJSON(t, "--json", "--target", "x", "doctor")
	diag, _ = m["diagnosis"].(map[string]any)
	if rc != 2 || diag == nil || !strings.Contains(diag["message"].(string), "acornfox doctor --json") {
		t.Fatalf("doctor after --target: rc %d %v", rc, m)
	}
}

// Passed items carry the check's fault code in "check" (code stays only on
// items that need attention), so a caller can find the clock line by
// SETUP-CLOCK-01 whatever its result.
func TestDoctorItemsCarryCheck(t *testing.T) {
	r := doctorReport{OK: true}
	r.add(true, "SETUP-CLOCK-01", "时钟：已同步")
	r.add(false, "SETUP-DISK-01", "磁盘：不够")
	r.warn("SETUP-PORT-05", "端口：看不到")
	r.add(true, "", "开机自启：已设置")
	b, _ := json.Marshal(r)
	var got struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"ok": true, "level": "ok", "text": "时钟：已同步", "check": "SETUP-CLOCK-01"},
		{"ok": false, "level": "fail", "text": "磁盘：不够", "code": "SETUP-DISK-01", "check": "SETUP-DISK-01"},
		{"ok": true, "level": "warn", "text": "端口：看不到", "code": "SETUP-PORT-05", "check": "SETUP-PORT-05"},
		{"ok": true, "level": "ok", "text": "开机自启：已设置"},
	}
	if !reflect.DeepEqual(got.Items, want) {
		t.Fatalf("items:\n%v\nwant\n%v", got.Items, want)
	}
	var text bytes.Buffer
	r.print(&text)
	if strings.Contains(text.String(), "已同步（SETUP-CLOCK-01）") || !strings.Contains(text.String(), "磁盘：不够（SETUP-DISK-01）") {
		t.Fatalf("text output changed: %q", text.String())
	}
}

// doctor -h describes the JSON items as they are, check included.
func TestDoctorHelpNamesCheck(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := runDoctor([]string{"-h"}, &out, &errb); rc != 2 {
		t.Fatalf("rc %d", rc)
	}
	help := out.String() + errb.String()
	if !strings.Contains(help, `"code","check"`) || !strings.Contains(help, "SETUP-CLOCK-01") {
		t.Fatalf("help: %q", help)
	}
}
