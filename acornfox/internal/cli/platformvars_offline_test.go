package cli

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/platformvar"
)

// `catalog platform-vars --offline` prints the whole table from the program
// itself: it contacts no server (there is none configured here and any
// connection fails the test), and its --json shape is pinned, because other
// systems read it instead of the code.
func TestCatalogPlatformVarsOffline(t *testing.T) {
	h := newHarness(t)
	h.connect = func(context.Context, client.Target) (client.API, error) {
		t.Error("--offline connected to a server")
		return nil, errors.New("no server")
	}
	code, out, errOut := h.run("--json", "catalog", "platform-vars", "--offline")
	if code != exitOK {
		t.Fatalf("exit %d out %s err %s", code, out, errOut)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var top []string
	for k := range doc {
		top = append(top, k)
	}
	sort.Strings(top)
	if strings.Join(top, ",") != "offline,ok,platform,vars" {
		t.Fatalf("top keys %v", top)
	}
	if doc["ok"] != true || doc["offline"] != true || doc["platform"] != Version {
		t.Fatalf("doc %s", out)
	}
	vars, _ := doc["vars"].([]any)
	if len(vars) != len(catalog.PlatformVars) {
		t.Fatalf("%d vars, table has %d", len(vars), len(catalog.PlatformVars))
	}
	const required = "cloud,editions,label,name,refresh,since,standalone,user_can_set"
	optional := map[string]bool{"secret": true, "rule": true, "rule_label": true}
	for i, x := range vars {
		m := x.(map[string]any)
		var req []string
		for k := range m {
			if !optional[k] {
				req = append(req, k)
			}
		}
		sort.Strings(req)
		if strings.Join(req, ",") != required {
			t.Fatalf("%v: keys %v, want %s", m["name"], req, required)
		}
		if m["name"] != catalog.PlatformVars[i].Name {
			t.Fatalf("var %d is %v, table order has %s", i, m["name"], catalog.PlatformVars[i].Name)
		}
	}
	// The same as the package's export: one source.
	want, _ := json.Marshal(platformvar.Export(Version).Vars)
	got, _ := json.Marshal(doc["vars"])
	var a, b any
	_ = json.Unmarshal(want, &a)
	_ = json.Unmarshal(got, &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("vars differ from platformvar.Export:\n%s\n%s", got, want)
	}

	code, out, _ = h.run("catalog", "platform-vars", "--offline")
	if code != exitOK || !strings.Contains(out, "没连服务端") || !strings.Contains(out, "platform.tz  时区  [能改：--set platform.tz=…]") ||
		!strings.Contains(out, "装应用时算一次") || !strings.Contains(out, "改的值要是：") {
		t.Fatalf("exit %d out %s", code, out)
	}
	if code, _, _ := h.run("catalog", "platform-vars", "--offline", "x"); code != exitUsage {
		t.Fatalf("a stray argument: exit %d", code)
	}
	// --json after the command works too; a server named next to --offline
	// is refused rather than silently ignored.
	code, out, _ = h.run("catalog", "platform-vars", "--offline", "--json")
	var after map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &after) != nil || after["offline"] != true {
		t.Fatalf("--json after: exit %d out %s", code, out)
	}
	for _, args := range [][]string{
		{"--target", "prod", "catalog", "platform-vars", "--offline"},
		{"--app", "demo", "catalog", "platform-vars", "--offline"},
	} {
		if code, _, _ := h.run(args...); code != exitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}
