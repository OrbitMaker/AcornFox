package faults

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Code shapes: upper-case codes of the program and the scripts, lower-case
// diagnosis codes, and the fallback.
var (
	upperCode = regexp.MustCompile(`^(CAT-(INST|CONN|LINT|BUILD|KEY|VER|SYNC|LOAD|PKG)|SETUP-[A-Z]+|LAN-CONSOLE|INSTALL-[A-Z]+|UPGRADE-[A-Z]+|UNINSTALL-[A-Z]+|OPEN-APP|ID-KEY|GW-[A-Z]+)-\d{2}$`)
	lowerCode = regexp.MustCompile(`^[a-z][a-z_]*[a-z]$`)
)

// groupOf says which group a code belongs in.
func groupOf(code string) string {
	switch {
	case code == FallbackCode:
		return "ANY"
	case lowerCode.MatchString(code):
		return "lower"
	case strings.HasPrefix(code, "INSTALL-"):
		return "INSTALL"
	case strings.HasPrefix(code, "UPGRADE-"), strings.HasPrefix(code, "UNINSTALL-"):
		return "UPGRADE"
	case strings.HasPrefix(code, "SETUP-"):
		return "SETUP"
	case strings.HasPrefix(code, "LAN-CONSOLE-"):
		return "LAN"
	case strings.HasPrefix(code, "OPEN-APP-"):
		return "OPEN"
	case strings.HasPrefix(code, "CAT-INST-"):
		return "INST"
	case strings.HasPrefix(code, "CAT-CONN-"):
		return "CONN"
	case strings.HasPrefix(code, "CAT-SYNC-"), strings.HasPrefix(code, "CAT-LOAD-"), strings.HasPrefix(code, "CAT-PKG-"):
		return "SYNC"
	case strings.HasPrefix(code, "CAT-"):
		return "PUB"
	case strings.HasPrefix(code, "GW-"):
		return "GW"
	case strings.HasPrefix(code, "ID-KEY-"):
		return "ID"
	}
	return "?"
}

// Every entry is complete: what a weak model needs to handle it without
// guessing.
func TestCatalogComplete(t *testing.T) {
	seen := map[string]bool{}
	groups := map[string]bool{}
	for _, g := range Groups {
		groups[g.Key] = true
	}
	lastGroup := 0
	order := map[string]int{}
	for i, g := range Groups {
		order[g.Key] = i
	}
	for _, f := range Catalog {
		if !upperCode.MatchString(f.Code) && !lowerCode.MatchString(f.Code) && f.Code != FallbackCode {
			t.Errorf("%s：编号格式不对", f.Code)
		}
		want := groupOf(f.Code)
		if want == "lower" {
			switch f.Group {
			case "LINK", "DEPLOY", "DOMAIN", "LAN", "CLI":
			default:
				t.Errorf("%s：小写的诊断码只能在 LINK、DEPLOY、DOMAIN、LAN、CLI 组，现在是 %s", f.Code, f.Group)
			}
		} else if f.Group != want {
			t.Errorf("%s：应在 %s 组，现在是 %s", f.Code, want, f.Group)
		}
		if !groups[f.Group] {
			t.Errorf("%s：没有这个组 %s", f.Code, f.Group)
		}
		if order[f.Group] < lastGroup {
			t.Errorf("%s：条目要按 Groups 的顺序放（%s 出现在后面的组之后）", f.Code, f.Group)
		}
		lastGroup = order[f.Group]
		if seen[f.Code] {
			t.Errorf("%s：重复", f.Code)
		}
		seen[f.Code] = true
		if LevelName[f.Level] == "" {
			t.Errorf("%s：等级只能是 P1、P2、P3", f.Code)
		}
		if f.Title == "" || f.Next == "" || f.Ask == "" || f.Verify == "" || len(f.Symptoms) == 0 || len(f.Causes) == 0 || len(f.Steps) == 0 || len(f.Actions) == 0 {
			t.Errorf("%s：标题、下一步、现象、原因、排查顺序、处置、停下问用户、确认恢复都要写", f.Code)
		}
		for _, c := range f.Causes {
			if c.Text == "" || c.Check == "" {
				t.Errorf("%s：每个原因都要写怎么判定", f.Code)
			}
		}
		for _, a := range f.Actions {
			if a.Tier < 0 || a.Tier > 2 || a.Text == "" {
				t.Errorf("%s：处置要有文字，级别只能是 0、1、2", f.Code)
			}
			// Deleting data, changing system settings, opening to the
			// network or spending money is never "may be done without asking".
			for _, risky := range []string{"volume rm", "--purge", "prune", "rm -rf", "reset --yes", "set-ntp", "usermod", "安全组", "扩容", "vacuum", "删数据卷", "--cancel-pending"} {
				if a.Tier < 2 && strings.Contains(a.Text, risky) {
					t.Errorf("%s：%q 只能是 2 级（先问用户）", f.Code, a.Text)
				}
			}
		}
		if f.Origin != "" && f.Origin != "gateway" {
			t.Errorf("%s：Origin 只能是空或 gateway", f.Code)
		}
		if f.Origin == "gateway" && f.Group != "GW" {
			t.Errorf("%s：网关的编号要在 GW 组", f.Code)
		}
		if f.Stage != "" && !lowerCode.MatchString(f.Code) {
			t.Errorf("%s：只有小写的诊断码写 Stage", f.Code)
		}
	}
	for _, f := range Catalog {
		for _, r := range f.Related {
			if !seen[r] {
				t.Errorf("%s：相关编号 %s 不在目录里", f.Code, r)
			}
			if r == f.Code {
				t.Errorf("%s：相关编号不能是自己", f.Code)
			}
		}
	}
	if Catalog[len(Catalog)-1].Code != FallbackCode {
		t.Errorf("兜底条目 %s 必须在最后", FallbackCode)
	}
}

func TestFind(t *testing.T) {
	if f, ok := Find("cat-inst-24"); !ok || f.Code != "CAT-INST-24" {
		t.Fatalf("upper-case codes match in any case: %v %v", f.Code, ok)
	}
	if f, ok := Find(" Build_Failed "); !ok || f.Code != "build_failed" {
		t.Fatalf("lower-case codes: %v %v", f.Code, ok)
	}
	if f, ok := Find("NOPE-99"); ok || f.Code != FallbackCode {
		t.Fatalf("unknown code must give the fallback: %v %v", f.Code, ok)
	}
}

func TestSetupDefsCoverEverything(t *testing.T) {
	defs := SetupDefs()
	if len(defs) != len(Catalog) {
		t.Fatalf("%d defs for %d entries", len(defs), len(Catalog))
	}
	for i, d := range defs {
		f := Catalog[i]
		if d.Code != f.Code || d.Title != f.Title || d.Next != f.Next || d.NeedsUser != f.NeedsUser || len(d.Causes) != len(f.Causes) {
			t.Fatalf("%s: short shape differs", f.Code)
		}
	}
}

func TestDetailHasEverything(t *testing.T) {
	f, _ := Find("CAT-INST-24")
	d := Detail(f)
	for _, want := range []string{"### CAT-INST-24", "是什么：", "下一步：", "可能原因", "排查顺序", "［2 级 先问用户］", "必须先停下问用户", "确认恢复", "要用户自己确认"} {
		if !strings.Contains(d, want) {
			t.Errorf("detail lacks %q:\n%s", want, d)
		}
	}
	g, _ := Find("GW-AUTH-01")
	if !strings.Contains(Detail(g), "来源：下载网关") || !strings.Contains(Detail(g), "AcornFox 已经自动做的") {
		t.Errorf("gateway entry detail:\n%s", Detail(g))
	}
}

// docs/faults.md is for users and on-call readers and is published with the
// program: no server addresses and no numbered design records. (Names of
// internal documents, machines and people are checked by the scan that runs
// before anything is published, not here.)
var (
	ipAddr    = regexp.MustCompile(`\b(\d{1,3}\.){3}\d{1,3}\b`)
	designRec = regexp.MustCompile(`\bADR-?[0-9]{4}\b`)
)

func TestMarkdownHasNoInternalInfo(t *testing.T) {
	md := Markdown()
	for _, m := range ipAddr.FindAllString(md, -1) {
		switch m {
		case "127.0.0.1", "0.0.0.0":
			continue
		}
		t.Errorf("docs/faults.md 不能写服务器地址：%q（示例地址用域名或 127.0.0.1）", m)
	}
	for _, m := range designRec.FindAllString(md, -1) {
		t.Errorf("docs/faults.md 不能引用设计记录编号：%q", m)
	}
}

// repoRoot is the repository root when the whole repository is here. When
// only the Go module is (some test runs copy just acornfox/), the checks that
// need the rest of the repository skip, saying so: run the tests from a full
// checkout of the repository before merging.
func repoRoot(t *testing.T) string {
	root := filepath.Join("..", "..", "..")
	if _, err := os.Stat(filepath.Join(root, "scripts", "install", "install.sh")); err != nil {
		t.Skipf("只有 Go 模块、没有整个仓库，跳过（docs/faults.md 是否最新没查；合并前要在完整的仓库里跑一次）：%v", err)
	}
	return root
}

const regenerate = "在仓库根目录运行 (cd acornfox && go run ./cmd/acornfox faults --markdown) > docs/faults.md"

func TestFaultsMarkdownUpToDate(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "docs", "faults.md"))
	switch {
	case os.IsNotExist(err):
		t.Errorf("docs/faults.md 不存在：%s 生成。它和程序一起发布（acornfox faults --help 和 Skill 都指向它），复制或发布仓库时不能漏掉", regenerate)
	case err != nil:
		t.Errorf("读不了 docs/faults.md：%v", err)
	case string(raw) != Markdown():
		t.Errorf("docs/faults.md 不是最新的：%s", regenerate)
	}
}
