package cli

// Skill 的版本号：命令行写 Skill（skill install、skill print）时，在每个文件头带上写它的这个命令行的版本号，
// 一行 HTML 注释：
//
//	<!-- acornfox-skill-version: 0.7.0 -->
//
// 入口 SKILL.md 放在 frontmatter 后面（不动 frontmatter：各家 AI 工作台读 frontmatter 的规则不同，多出来的键可能被拒），
// 分册（没有 frontmatter，第一行是中文标题）放在标题行后面，这样装好以后每个文件第一行仍然是原来的样子。
// 用意：Skill 是用户电脑上 AI 工作台读的文件，服务器升级后它不会自己更新。`acornfox doctor` 对照已装 Skill
// 每个文件里的版本号和这个命令行的版本号，不一致就报 SETUP-SKILL-01。仓库里的文件不带版本号（版本号随发行版变，
// 写的时候才加）。

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// skillVersionPrefix starts the version comment written into every file.
const skillVersionPrefix = "<!-- acornfox-skill-version: "

var skillVersionRE = regexp.MustCompile(`(?m)^<!-- acornfox-skill-version: ([^ >]+) -->\s*$`)

// skillTextFor returns a Skill file with the version comment of ver: after
// the frontmatter when there is one, otherwise after the first line (the
// title), the top of the file for a file that starts with neither. The
// embedded text has none; a text that already carries a version comment is
// returned as it is.
func skillTextFor(content, ver string) string {
	if skillVersionRE.MatchString(content) {
		return content
	}
	comment := skillVersionPrefix + ver + " -->\n"
	if strings.HasPrefix(content, "---\n") {
		const fence = "\n---\n"
		i := strings.Index(content[3:], fence)
		if i < 0 {
			return content
		}
		end := 3 + i + len(fence)
		return content[:end] + comment + content[end:]
	}
	if strings.HasPrefix(content, "# ") {
		if nl := strings.Index(content, "\n"); nl >= 0 {
			return content[:nl+1] + comment + content[nl+1:]
		}
	}
	return comment + content
}

// SkillText is the entry file of the Skill as `acornfox skill install` and
// `skill print` write it: the embedded text with this program's version after
// the frontmatter.
func SkillText() string { return skillTextFor(skillContent, Version) }

// SkillFileText is any file of the Skill as it is written (the name is a
// file name such as "install-linux.md"); ok is false for a name the Skill
// does not have.
func SkillFileText(name string) (string, bool) {
	for _, f := range SkillFiles() {
		if f.Name == name {
			return skillTextFor(f.Text, Version), true
		}
	}
	return "", false
}

// SkillCorpus is the text of every file of the Skill, one after the other
// (no version comments): what the pattern markers ("<!-- 套路: 名字 -->") are
// read from, wherever in the Skill they stand.
func SkillCorpus() string {
	var b strings.Builder
	for _, f := range SkillFiles() {
		b.WriteString(f.Text)
		if !strings.HasSuffix(f.Text, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// parseSkillVersion reads the version a Skill file carries; "" when it has
// none (written by a program from before the version was written).
func parseSkillVersion(text string) string {
	m := skillVersionRE.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return m[1]
}

// versionCore reads the x.y.z at the start of a version ("0.6.4", "v0.6.4",
// "0.6.4-dev", "0.6.9-win.1"); ok is false when there is none.
func versionCore(v string) (x, y, z int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	n := [3]int{}
	for i, p := range parts {
		k, err := strconv.Atoi(p)
		if err != nil || k < 0 {
			return 0, 0, 0, false
		}
		n[i] = k
	}
	return n[0], n[1], n[2], true
}

// versionNewer reports whether a is a higher x.y.z than b. Versions that do
// not parse are never newer (a notice must not guess).
func versionNewer(a, b string) bool {
	ax, ay, az, aok := versionCore(a)
	bx, by, bz, bok := versionCore(b)
	if !aok || !bok {
		return false
	}
	if ax != bx {
		return ax > bx
	}
	if ay != by {
		return ay > by
	}
	return az > bz
}

// SkillMismatch is one installed Skill whose version is not this program's.
type SkillMismatch struct {
	Workbench string // Claude Code, Codex
	Path      string // the entry file (…/acornfox/SKILL.md)
	Installed string // the version the entry file carries; "" for a file without one
	Program   string // this program's version
	// ProgramOlder is true when the Skill is newer than this program (the
	// program is the one to update first).
	ProgramOlder bool
	// Bad names the other files of the Skill (分册) that are missing or carry
	// another version than this program's.
	Bad []string
}

// CheckInstalledSkills looks at the Skills `acornfox skill install` puts under
// home (Claude Code ~/.claude/skills/acornfox, Codex ~/.codex/skills/acornfox)
// and at every file of each: the entry and each 分册 must carry this program's
// version. found names the workbenches that have a Skill (an entry file); bad
// lists those where a file does not carry this program's version or is
// missing. A workbench without an installed Skill is in neither. Read only.
func CheckInstalledSkills(home string) (found []string, bad []SkillMismatch) {
	files := SkillFiles()
	for _, wb := range detectWorkbenches(home) {
		dir := filepath.Join(wb.SkillsDir, skillName)
		path := filepath.Join(dir, skillEntry)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		found = append(found, wb.Name)
		have := parseSkillVersion(string(data))
		var badFiles []string
		for _, f := range files[1:] {
			b, rerr := os.ReadFile(filepath.Join(dir, f.Name))
			if rerr != nil || parseSkillVersion(string(b)) != Version {
				badFiles = append(badFiles, f.Name)
			}
		}
		if have == Version && len(badFiles) == 0 {
			continue
		}
		bad = append(bad, SkillMismatch{
			Workbench: wb.Name, Path: path, Installed: have, Program: Version,
			ProgramOlder: have != "" && versionNewer(have, Version), Bad: badFiles,
		})
	}
	return found, bad
}

// SkillMismatchLine is the one doctor line for a mismatch (without the fault
// code, which doctor adds).
func SkillMismatchLine(m SkillMismatch) string {
	switch {
	case m.Installed == Version:
		return m.Workbench + " 里的 AcornFox Skill 入口是 " + m.Installed + " 版，但分册 " + strings.Join(m.Bad, "、") + " 缺少或不是 " + m.Program + " 版（没装全或没装好）：运行 acornfox skill install 重新装"
	case m.Installed == "":
		return m.Workbench + " 里的 AcornFox Skill 是旧版本写的（没有版本号），这台电脑上的命令行是 " + m.Program + " 版：运行 acornfox skill install 更新"
	case m.ProgramOlder:
		return m.Workbench + " 里的 AcornFox Skill 是 " + m.Installed + " 版，比这台电脑上的命令行（" + m.Program + " 版）新：先更新命令行（运行 get.sh 的 cli 那条命令，见 Skill），再运行 acornfox skill install"
	default:
		return m.Workbench + " 里的 AcornFox Skill 是 " + m.Installed + " 版，这台电脑上的命令行是 " + m.Program + " 版：运行 acornfox skill install 更新"
	}
}
