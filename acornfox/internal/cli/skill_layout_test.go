package cli

// 通用 Skill 分成“入口 + 分册”的布局检查（闸门）。规矩：
//
//   - 入口 SKILL.md 只放规则、“用户说什么 → 读哪一册”的对照表和停下问人的总表，不超过 skillEntryMaxLines 行；
//   - 每一册不超过 skillPartMaxLines 行，第一行是中文标题，第一段写“什么时候读这一册”，
//     分最低路径、可以发挥的地方、必须先问用户的三块；
//   - 入口里每个分册都读得到，文件里写到的每个分册名都真有；
//   - 故障编号不抄进 Skill（不出现首列是故障编号的表格行），拿到编号就 `acornfox faults 编号`。
//
// 命令和故障编号是不是真有，在 cmd/acornfox/skill_gate_test.go 里查（要读命令表和故障目录）。

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	skillEntryMaxLines = 260 // 入口约 200 行
	skillPartMaxLines  = 400
)

// mdName finds a Skill file name written as `name.md`.
var mdName = regexp.MustCompile("`([a-z][a-z0-9-]*\\.md)`")

func lineCount(text string) int {
	return strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
}

func TestSkillLayoutLineLimits(t *testing.T) {
	files := SkillFiles()
	if len(files) < 2 || files[0].Name != skillEntry {
		t.Fatalf("the Skill must be the entry plus 分册, got %v", skillFileNames())
	}
	for _, f := range files {
		limit := skillPartMaxLines
		if f.Name == skillEntry {
			limit = skillEntryMaxLines
		}
		if n := lineCount(f.Text); n > limit {
			t.Errorf("%s 有 %d 行，超过上限 %d 行：拆成两册，并在入口的对照表里登记", f.Name, n, limit)
		}
	}
}

func TestSkillLayoutEveryPartHasItsParts(t *testing.T) {
	for _, f := range SkillFiles() {
		if f.Name == skillEntry {
			continue
		}
		lines := strings.Split(f.Text, "\n")
		if !strings.HasPrefix(lines[0], "# ") {
			t.Errorf("%s：第一行要是中文标题（# 开头），现在是 %q", f.Name, lines[0])
		}
		first := ""
		for _, ln := range lines[1:] {
			if strings.TrimSpace(ln) != "" {
				first = ln
				break
			}
		}
		if !strings.HasPrefix(first, "**什么时候读这一册**") {
			t.Errorf("%s：标题后面第一段要以“**什么时候读这一册**”开头，现在是 %.40q", f.Name, first)
		}
		for _, must := range []string{"最低路径", "可以发挥的地方", "必须先问用户的"} {
			if !strings.Contains(f.Text, must) {
				t.Errorf("%s：缺“%s”这一块（最低路径 / 可以发挥的地方 / 必须先问用户的）", f.Name, must)
			}
		}
		if strings.HasPrefix(f.Text, "---") {
			t.Errorf("%s：分册不写 frontmatter（只有入口是工作台读的 Skill）", f.Name)
		}
	}
}

func TestSkillLayoutEntryHasWhatItMust(t *testing.T) {
	for _, must := range []string{
		"## 任何时候都要遵守", "## 用户说什么 → 读哪一册 → 第一条命令", "## 停下问人的总表",
		"遇到故障编号怎么办", "固定规则一", "固定规则二",
		"acornfox --json machine", "acornfox --json catalog skill", "acornfox faults 编号",
		"**任何密码和 Key 都不能由你输入，你也不能把它写进命令行参数**",
	} {
		if !strings.Contains(skillContent, must) {
			t.Errorf("入口缺“%s”", must)
		}
	}
}

// Every file the Skill names exists, and the entry reaches every 分册.
func TestSkillLayoutLinks(t *testing.T) {
	have := map[string]bool{}
	for _, f := range SkillFiles() {
		have[f.Name] = true
	}
	for _, f := range SkillFiles() {
		for _, m := range mdName.FindAllStringSubmatch(f.Text, -1) {
			if !have[m[1]] {
				t.Errorf("%s 里写到分册 %s，Skill 里没有这个文件", f.Name, m[1])
			}
		}
	}
	reach := map[string]bool{skillEntry: true}
	for _, m := range mdName.FindAllStringSubmatch(skillContent, -1) {
		reach[m[1]] = true
	}
	var missing []string
	for _, f := range SkillFiles() {
		if !reach[f.Name] {
			missing = append(missing, f.Name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("入口里没写到这些分册（要在“用户说什么 → 读哪一册”里登记）：%s", strings.Join(missing, "、"))
	}
}

// The fault codes are not copied into the Skill: no table row starts with one.
// (`acornfox faults 编号` is the one place they are explained.)
var faultTableRow = regexp.MustCompile("^\\|\\s*`[A-Z][A-Z]+(-[A-Z]+)*-\\d{2}`")

func TestSkillLayoutNoFaultTables(t *testing.T) {
	for _, f := range SkillFiles() {
		for i, ln := range strings.Split(f.Text, "\n") {
			if faultTableRow.MatchString(ln) {
				t.Errorf("%s:%d 是故障编号表的一行：故障编号不抄进 Skill，写成“照入口规则 acornfox faults 编号”；流程里必须提到的编号用句子写", f.Name, i+1)
			}
		}
	}
}
