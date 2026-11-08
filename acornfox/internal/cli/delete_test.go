package cli

import (
	"context"
	"strings"
	"testing"
)

// The app named on the command line is the one deleted, not the current
// directory's app (it used to be ignored: "delete web" in the "notes"
// project deleted notes).
func TestDeleteTakesTheNamedApp(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	if err := saveProject(h.workDir, &projectFile{Target: "dev", App: "notes"}); err != nil {
		t.Fatal(err)
	}
	var got []string
	var vols []bool
	h.api.deleteAppFn = func(_ context.Context, app string, v bool) error {
		got, vols = append(got, app), append(vols, v)
		return nil
	}
	if code, out, errOut := h.run("--json", "delete", "web"); code != exitOK || len(got) != 1 || got[0] != "web" || vols[0] {
		t.Fatalf("code=%d got=%v out=%s err=%s", code, got, out, errOut)
	}
	if code, _, _ := h.run("delete", "web", "--volumes"); code != exitOK || got[1] != "web" || !vols[1] {
		t.Fatalf("flag after the name: code=%d got=%v vols=%v", code, got, vols)
	}
	if code, _, _ := h.run("delete"); code != exitOK || got[2] != "notes" {
		t.Fatalf("no name falls back to the directory's app: code=%d got=%v", code, got)
	}
	for _, args := range [][]string{{"delete", "web", "shop"}, {"delete", "web", "--app", "shop"}} {
		if code, _, _ := h.run(args...); code != exitUsage {
			t.Fatalf("%v: code=%d, want usage", args, code)
		}
	}
	if len(got) != 3 {
		t.Fatalf("a refused command deleted something: %v", got)
	}
	code, out, _ := h.run("delete", "-h")
	if code != exitOK || !strings.Contains(out, "默认保留数据卷") || !strings.Contains(out, "acornfox delete web") {
		t.Fatalf("help: code=%d out=%s", code, out)
	}
}
