package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gapsHead = "# 缺变量登记\nschema: 1\ngaps:\n"

func gapEntry(id, app, want, status, extra string) string {
	return "  - id: " + id + "\n    app: " + app + "\n    upstream_key: HF_ENDPOINT\n    want: " + want +
		"\n    layer: platform\n    handling: hardcoded\n    doc_url: https://example.com/docs/env\n    date: 2026-01-31\n    status: " + status + "\n" + extra
}

func writeGaps(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, GapsFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckGapsNoFile(t *testing.T) {
	rep, err := CheckGaps(copyTree(t))
	if err != nil || rep.Registered || len(rep.Problems) != 0 || rep.Gaps == nil || rep.Notices == nil || rep.File != "gaps.yaml" {
		t.Fatalf("没有文件算没有登记：%+v %v", rep, err)
	}
}

func TestCheckGapsNoSuchDir(t *testing.T) {
	notDir := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(notDir, []byte("schema: 1\ngaps: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{filepath.Join(t.TempDir(), "no-such-dir"), notDir} {
		rep, err := CheckGaps(dir)
		if err != nil || rep.Registered || len(rep.Problems) != 1 || rep.Problems[0].Code != CodeRead || rep.Problems[0].Level != LevelError ||
			!strings.Contains(rep.Problems[0].Message, dir) || rep.Gaps == nil || rep.Notices == nil {
			t.Fatalf("目录 %s 不存在要算问题，不是“没有登记”：%+v %v", dir, rep, err)
		}
	}
	// The directory is there but has no gaps.yaml: still "nothing registered".
	if rep, err := CheckGaps(t.TempDir()); err != nil || rep.Registered || len(rep.Problems) != 0 {
		t.Fatalf("%+v %v", rep, err)
	}
}

func TestCheckGaps(t *testing.T) {
	dir := copyTree(t)
	writeGaps(t, dir, gapsHead+
		gapEntry("GAP-0001", "new-api", "platform.tz", "open", "")+ // exists now → can close
		gapEntry("GAP-0002", "new-api", "platform.mirror.huggingface", "open", "    note: 模型下载源\n")+ // not there yet
		gapEntry("GAP-0003", "hermes-agent", "platform.mirror.npm", "done", "    done_in: \"0.5.0\"\n")+ // already closed
		gapEntry("GAP-0004", "open-webui", "platform.mirror.pypi", "planned", "")+ // exists, and the app is gone
		gapEntry("GAP-0005", "new-api", "\"环境变量的 value 里能写模板\"", "planned", "")) // not a variable
	rep, err := CheckGaps(dir)
	if err != nil || !rep.Registered || len(rep.Problems) != 0 || len(rep.Gaps) != 5 {
		t.Fatalf("%+v %v", rep, err)
	}
	if rep.ByStatus["open"] != 2 || rep.ByStatus["planned"] != 2 || rep.ByStatus["done"] != 1 || len(rep.ByStatus) != 3 {
		t.Fatalf("by_status %+v", rep.ByStatus)
	}
	var got []string
	for _, n := range rep.Notices {
		got = append(got, n.ID+":"+n.App+":"+n.Kind)
		switch n.Kind {
		case GapCanClose:
			if !strings.Contains(n.Message, "可以关了：变量已有，请改清单") {
				t.Errorf("%s 的提示：%s", n.ID, n.Message)
			}
		case GapAppMissing:
			if !strings.Contains(n.Message, "已不在目录里") {
				t.Errorf("%s 的提示：%s", n.ID, n.Message)
			}
		}
	}
	want := "GAP-0001:new-api:can_close,GAP-0004:open-webui:can_close,GAP-0004:open-webui:app_missing"
	if strings.Join(got, ",") != want {
		t.Fatalf("提示 %v，要 %s", got, want)
	}
	g := rep.Gaps[1]
	if g.ID != "GAP-0002" || g.App != "new-api" || g.UpstreamKey != "HF_ENDPOINT" || g.Want != "platform.mirror.huggingface" || g.Layer != "platform" ||
		g.Handling != "hardcoded" || g.DocURL != "https://example.com/docs/env" || g.Date != "2026-01-31" || g.Status != "open" || g.Note != "模型下载源" {
		t.Fatalf("读出来的条目 %+v", g)
	}
	if rep.Gaps[2].DoneIn != "0.5.0" {
		t.Fatalf("done_in %+v", rep.Gaps[2])
	}

	// A variable that arrives later turns the open entry into "can close".
	withPlatformVar(t, PlatformVar{Name: "mirror.huggingface", Label: "模型下载源", Standalone: "x", Cloud: "x", Since: "9.9.9"})
	rep, _ = CheckGaps(dir)
	found := false
	for _, n := range rep.Notices {
		if n.ID == "GAP-0002" && n.Kind == GapCanClose && strings.Contains(n.Message, "9.9.9") {
			found = true
		}
	}
	if !found {
		t.Fatalf("平台补上变量后要提示 GAP-0002 可以关了：%+v", rep.Notices)
	}

	// A quoted date and an empty list are fine.
	writeGaps(t, dir, gapsHead+strings.Replace(gapEntry("GAP-0001", "new-api", "platform.region", "open", ""), "date: 2026-01-31", `date: "2026-01-31"`, 1))
	if rep, _ = CheckGaps(dir); len(rep.Problems) != 0 || rep.Gaps[0].Date != "2026-01-31" {
		t.Fatalf("带引号的日期：%+v", rep)
	}
	writeGaps(t, dir, "# 还没有登记\nschema: 1\ngaps: []\n")
	if rep, _ = CheckGaps(dir); !rep.Registered || len(rep.Problems) != 0 || len(rep.Gaps) != 0 || len(rep.Notices) != 0 {
		t.Fatalf("空列表：%+v", rep)
	}
}

func TestCheckGapsBadFile(t *testing.T) {
	ok := gapEntry("GAP-0001", "new-api", "platform.tz", "open", "")
	cases := map[string]string{
		"不是 YAML":       "schema: 1\ngaps: [",
		"空文件":           "",
		"没有 schema":     "gaps: []\n",
		"schema 不认识":    "schema: 2\ngaps: []\n",
		"多了字段":          gapsHead + ok + "    owner: someone\n",
		"缺 doc_url":     gapsHead + strings.Replace(ok, "    doc_url: https://example.com/docs/env\n", "", 1),
		"layer 取值不对":    gapsHead + strings.Replace(ok, "layer: platform", "layer: system", 1),
		"handling 取值不对": gapsHead + strings.Replace(ok, "handling: hardcoded", "handling: ignored", 1),
		"status 取值不对":   gapsHead + strings.Replace(ok, "status: open", "status: closed", 1),
		"编号格式不对":        gapsHead + strings.Replace(ok, "GAP-0001", "gap-1", 1),
		"日期格式不对":        gapsHead + strings.Replace(ok, "date: 2026-01-31", "date: 11/10", 1),
		"地址不是网址":        gapsHead + strings.Replace(ok, "https://example.com/docs/env", "见上游文档", 1),
		"done 没写版本":     gapsHead + strings.Replace(ok, "status: open", "status: done", 1),
		"done_in 不是版本":  gapsHead + strings.Replace(ok, "status: open", "status: done\n    done_in: next", 1),
		"编号重复":          gapsHead + ok + ok,
	}
	for name, content := range cases {
		dir := copyTree(t)
		writeGaps(t, dir, content)
		rep, err := CheckGaps(dir)
		if err != nil || !rep.Registered || len(rep.Problems) == 0 || len(rep.Notices) != 0 || len(rep.Gaps) != 0 {
			t.Errorf("%s：要有问题、不出提示：%+v %v", name, rep, err)
			continue
		}
		for _, p := range rep.Problems {
			if p.Code != CodeGaps || p.Level != LevelError || p.File != GapsFile || p.Message == "" {
				t.Errorf("%s：问题要是 %s：%+v", name, CodeGaps, p)
			}
		}
	}
}

// gaps.yaml sits in the catalog root and is never published: lint does not
// read it and build output is the same with or without it, even a broken one.
func TestGapsFileStaysOutOfBuild(t *testing.T) {
	dir := copyTree(t)
	before, err := Build(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{gapsHead + gapEntry("GAP-0001", "new-api", "platform.tz", "open", ""), "not: [valid"} {
		writeGaps(t, dir, content)
		r, err := Lint(dir)
		if err != nil || len(r.Problems) != 0 {
			t.Fatalf("lint 不管 gaps.yaml：%+v %v", r.Problems, err)
		}
		after, err := Build(dir, 3)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before.Index, after.Index) || len(before.Packages) != len(after.Packages) {
			t.Fatal("有了 gaps.yaml，index.json 变了")
		}
		for p, b := range before.Packages {
			if !bytes.Equal(b, after.Packages[p]) {
				t.Fatalf("有了 gaps.yaml，%s 变了", p)
			}
		}
		for _, f := range after.Files() {
			if strings.Contains(f.Path, "gaps") {
				t.Fatalf("发布文件里出现了 %s", f.Path)
			}
		}
		if bytes.Contains(after.Index, []byte("GAP-")) {
			t.Fatal("index.json 里出现了登记内容")
		}
	}
}
