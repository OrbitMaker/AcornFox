package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// fakeSkillAPI answers GET /v1/catalog/skills/{kind}/{id}: manuals for the
// app "memos" and the solution "office"; "quiet" is an app with none;
// "stale" is listed but not usable here.
type fakeSkillAPI struct {
	*fakeVarsAPI
	asked []string
}

func (f *fakeSkillAPI) CatalogSkill(_ context.Context, kind, id string) (client.Skill, error) {
	f.asked = append(f.asked, kind+"/"+id)
	switch kind + "/" + id {
	case "app/memos":
		return client.Skill{ID: "memos", Kind: "app", HasSkill: true, Version: "0.31.0", Checked: "2026-11-24", Size: 40, Text: "# Memos\n\n## 装之前\n先问用户要不要对外开放。\n"}, nil
	case "recipe/office":
		return client.Skill{ID: "office", Kind: "recipe", HasSkill: true, Version: "3", Checked: "2026-11-23", Size: 30, Text: "# 办公套装\n\n先装 A 再装 B。\n"}, nil
	case "app/quiet":
		return client.Skill{ID: "quiet", Kind: "app", Note: "这个没有专门的操作手册，照通用 Skill 做"}, nil
	case "app/stale":
		return client.Skill{ID: "stale", Kind: "app", Code: "CAT-SKILL-01", Note: "目录里有这份操作手册，但这台机器上现在用不了；先运行 acornfox catalog sync"}, nil
	}
	return client.Skill{}, &client.Error{Status: 404, Diag: client.Diagnosis{Stage: "server", Code: "CAT-INST-01", Message: "目录里没有 " + id}}
}

func skillHarness(t *testing.T) (*harness, *fakeSkillAPI) {
	h, v := varsHarness(t)
	f := &fakeSkillAPI{fakeVarsAPI: v}
	h.connect = func(context.Context, client.Target) (client.API, error) { return f, nil }
	return h, f
}

func TestCatalogSkillCommand(t *testing.T) {
	h, f := skillHarness(t)

	code, out, errOut := h.run("catalog", "skill", "memos")
	if code != exitOK {
		t.Fatalf("exit %d %s", code, errOut)
	}
	for _, want := range []string{"应用 memos 的操作手册（对版本 0.31.0 验过，2026-11-24 通过沙箱验证）", "## 装之前", "先问用户要不要对外开放。"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	_, out, _ = h.run("--json", "catalog", "skill", "memos")
	m := decodeJSON(t, out)
	if m["ok"] != true || m["id"] != "memos" || m["kind"] != "app" || m["has_skill"] != true || m["version"] != "0.31.0" || m["checked"] != "2026-11-24" ||
		!strings.Contains(m["text"].(string), "## 装之前") {
		t.Fatalf("json %s", out)
	}
	// --json after the command works too.
	_, out, _ = h.run("catalog", "skill", "memos", "--json")
	if m := decodeJSON(t, out); m["id"] != "memos" || m["has_skill"] != true {
		t.Fatalf("--json after: %s", out)
	}

	// A solution: --recipe asks for the recipe only; without it an id that is
	// no app falls back to the solution of that id.
	f.asked = nil
	_, out, _ = h.run("--json", "catalog", "skill", "office", "--recipe")
	if m := decodeJSON(t, out); m["kind"] != "recipe" || m["has_skill"] != true || len(f.asked) != 1 || f.asked[0] != "recipe/office" {
		t.Fatalf("recipe: %s asked %v", out, f.asked)
	}
	f.asked = nil
	_, out, _ = h.run("--json", "catalog", "skill", "office")
	if m := decodeJSON(t, out); m["kind"] != "recipe" || m["has_skill"] != true || len(f.asked) != 2 || f.asked[0] != "app/office" {
		t.Fatalf("fallback: %s asked %v", out, f.asked)
	}

	// No manual: said plainly, exit 0, empty text.
	code, out, _ = h.run("catalog", "skill", "quiet")
	if code != exitOK || !strings.Contains(out, "应用 quiet：这个没有专门的操作手册，照通用 Skill 做") {
		t.Fatalf("quiet: %d %s", code, out)
	}
	code, out, _ = h.run("--json", "catalog", "skill", "quiet")
	if m := decodeJSON(t, out); code != exitOK || m["ok"] != true || m["has_skill"] != false || m["text"] != "" || m["note"] == "" {
		t.Fatalf("quiet json: %d %s", code, out)
	}
	// Listed but not usable here: still exit 0, with the code.
	code, out, _ = h.run("catalog", "skill", "stale")
	if code != exitOK || !strings.Contains(out, "编号 CAT-SKILL-01") {
		t.Fatalf("stale: %d %s", code, out)
	}
	_, out, _ = h.run("--json", "catalog", "skill", "stale")
	if m := decodeJSON(t, out); m["has_skill"] != false || m["code"] != "CAT-SKILL-01" {
		t.Fatalf("stale json: %s", out)
	}

	// Unknown id: exit 4 and not_found; --recipe too.
	for _, args := range [][]string{{"--json", "catalog", "skill", "nope"}, {"--json", "catalog", "skill", "nope", "--recipe"}} {
		code, out, _ = h.run(args...)
		if code != exitServerError || !strings.Contains(out, "not_found") {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	if code, _, _ = h.run("catalog", "skill"); code != exitUsage {
		t.Fatalf("no id: %d", code)
	}
	if code, out, _ = h.run("catalog", "skill", "-h"); code != exitOK || !strings.Contains(out, "只读") || !strings.Contains(out, "CAT-SKILL-01") || !strings.Contains(out, "例子：") || !strings.Contains(out, "退出码") {
		t.Fatalf("help: %d %s", code, out)
	}
	if code, out, _ = h.run("catalog", "-h"); code != exitOK || !strings.Contains(out, "catalog skill 应用或解决方案") {
		t.Fatalf("catalog help: %d %s", code, out)
	}
}

// A server (fake) that does not have the call: asked politely to upgrade.
func TestCatalogSkillOldServer(t *testing.T) {
	h, _ := varsHarness(t) // fakeVarsAPI has no CatalogSkill
	code, out, _ := h.run("--json", "catalog", "skill", "memos")
	if code != exitServerError || !strings.Contains(out, "升级 AcornFox") {
		t.Fatalf("%d %s", code, out)
	}
}
