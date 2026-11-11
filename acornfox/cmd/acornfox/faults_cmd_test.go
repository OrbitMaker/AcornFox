package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/faults"
	"github.com/acornfox/acornfox/internal/hostcheck"
)

func runText(args ...string) (string, string, int) {
	var out, errb bytes.Buffer
	rc := run(args, strings.NewReader(""), &out, &errb)
	return out.String(), errb.String(), rc
}

func TestFaultsCommand(t *testing.T) {
	// The list.
	out, _, rc := runText("faults")
	if rc != 0 || !strings.Contains(out, "CAT-INST-24") || !strings.Contains(out, "SETUP-GW-01") || !strings.Contains(out, "acornfox faults 编号") {
		t.Fatalf("list rc %d:\n%.400s", rc, out)
	}
	// One entry, upper-case codes in any case.
	out, _, rc = runText("faults", "cat-inst-27")
	if rc != 0 || !strings.Contains(out, "### CAT-INST-27") || !strings.Contains(out, "排查顺序") || !strings.Contains(out, "必须先停下问用户") {
		t.Fatalf("one rc %d:\n%s", rc, out)
	}
	// --json, written before or after.
	for _, args := range [][]string{{"faults", "SETUP-GW-01", "--json"}, {"--json", "faults", "SETUP-GW-01"}} {
		m, rc, _ := runJSON(t, args...)
		f, _ := m["fault"].(map[string]any)
		if rc != 0 || m["ok"] != true || f["code"] != "SETUP-GW-01" || f["next"] == "" {
			t.Fatalf("%v: rc %d %v", args, rc, m)
		}
	}
	m, rc, _ := runJSON(t, "faults", "--json")
	if list, _ := m["faults"].([]any); rc != 0 || len(list) != len(faults.Catalog) {
		t.Fatalf("json list rc %d len %d", rc, len(list))
	}
	// A group.
	m, rc, _ = runJSON(t, "faults", "--group", "setup", "--json")
	list, _ := m["faults"].([]any)
	if rc != 0 || len(list) == 0 {
		t.Fatalf("group rc %d %v", rc, m)
	}
	for _, x := range list {
		if x.(map[string]any)["group"] != "SETUP" {
			t.Fatalf("group filter leaked %v", x)
		}
	}
	// Unknown code: exit 1, the fallback entry.
	m, rc, _ = runJSON(t, "faults", "NOPE-99", "--json")
	d, _ := m["diagnosis"].(map[string]any)
	fb, _ := m["fallback"].(map[string]any)
	if rc != 1 || m["ok"] != false || d["code"] != "fault_unknown" || fb["code"] != faults.FallbackCode {
		t.Fatalf("unknown rc %d %v", rc, m)
	}
	_, errText, rc := runText("faults", "NOPE-99")
	if rc != 1 || !strings.Contains(errText, "ANY-00") {
		t.Fatalf("unknown text rc %d %q", rc, errText)
	}
	// Markdown is docs/faults.md.
	out, _, rc = runText("faults", "--markdown")
	if rc != 0 || out != faults.Markdown() {
		t.Fatalf("markdown rc %d", rc)
	}
	// Usage errors: exit 2.
	for _, args := range [][]string{{"faults", "A", "B"}, {"faults", "--group", "NOPE"}, {"faults", "--bogus"}, {"faults", "--markdown", "--json"}} {
		if _, _, rc := runText(args...); rc != 2 {
			t.Fatalf("%v: rc %d, want 2", args, rc)
		}
	}
	if m, rc, _ := runJSON(t, "faults", "--bogus", "--json"); rc != 2 || m["ok"] != false {
		t.Fatalf("json usage rc %d %v", rc, m)
	}
	// Help.
	out, _, rc = runText("faults", "--help")
	if rc != 0 || !strings.Contains(out, "只读") || !strings.Contains(out, "退出码") {
		t.Fatalf("help rc %d", rc)
	}
	// After --target it is a usage error from the client layer.
	if _, errText, rc := runText("--target", "x", "faults"); rc == 0 || !strings.Contains(errText, "faults 只查程序里的故障目录") {
		t.Fatalf("--target faults rc %d %q", rc, errText)
	}
}

// doctor's gateway line uses the same gateway address as the app center.
func TestDoctorGatewayUsesGatewayBase(t *testing.T) {
	old := doctorGatewayProbes
	t.Cleanup(func() { doctorGatewayProbes = old })
	t.Setenv("ACORNFOX_GATEWAY", "http://127.0.0.1:9")
	var asked string
	doctorGatewayProbes = func(gw string) (hostcheck.ClockProbe, hostcheck.GatewayProbe) {
		asked = gw
		return hostcheck.ClockProbe{}, hostcheck.GatewayProbe{Base: gw, Health: func(context.Context) (hostcheck.GatewayHealth, error) {
			return hostcheck.GatewayHealth{}, errors.New("connect: connection refused")
		}}
	}
	r := doctorReport{OK: true}
	doctorClockAndGateway(context.Background(), &r, t.TempDir())
	if asked != "http://127.0.0.1:9" || len(r.Items) != 2 || r.Items[0].Check != "SETUP-CLOCK-01" {
		t.Fatalf("asked %q items %+v", asked, r.Items)
	}
	it := r.Items[1]
	if it.Level != "warn" || it.Code != "SETUP-GW-01" || it.Check != "SETUP-GW-01" || !r.OK || !strings.Contains(it.Text, "connection refused") {
		t.Fatalf("item %+v", it)
	}
}

func TestDoctorCatalog(t *testing.T) {
	old := doctorCatalogStatus
	t.Cleanup(func() { doctorCatalogStatus = old })
	at := time.Now().Add(-2 * time.Hour)
	doctorCatalogStatus = func(context.Context, string) (hostcheck.CatalogSync, bool) {
		return hostcheck.CatalogSync{At: &at, OK: true, Source: "https://acornfox.com/catalog", Serial: 3, Origin: "cache"}, true
	}
	r := doctorReport{OK: true}
	doctorCatalog(context.Background(), &r, "/nonexistent.sock")
	if len(r.Items) != 1 || r.Items[0].Check != "SETUP-SYNC-01" || r.Items[0].Level != "ok" || !strings.Contains(r.Items[0].Text, "2 小时前同步过") {
		t.Fatalf("items %+v", r.Items)
	}
	// Cannot ask the server: no line.
	doctorCatalogStatus = func(context.Context, string) (hostcheck.CatalogSync, bool) { return hostcheck.CatalogSync{}, false }
	r = doctorReport{OK: true}
	doctorCatalog(context.Background(), &r, "/nonexistent.sock")
	if len(r.Items) != 0 {
		t.Fatalf("items %+v", r.Items)
	}
	// The real query against a missing socket gives up quietly.
	if _, ok := queryCatalogSync(context.Background(), "/nonexistent/acornfox.sock"); ok {
		t.Fatal("missing socket must not be ok")
	}
}
