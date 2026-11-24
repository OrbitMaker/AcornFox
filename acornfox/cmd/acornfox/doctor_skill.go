package main

// doctor 的一项：这台电脑上已装的 AcornFox Skill 和这个命令行是不是同一个版本（SETUP-SKILL-01）。
// 只读：看 ~/.claude/skills/acornfox/ 和 ~/.codex/skills/acornfox/ 里入口 SKILL.md 和每个分册文件头里的版本号，
// 缺哪一册也报；没装 Skill 的工作台不报。不一致只是提醒（warn），不让 doctor 变成不通过。

import (
	"os"
	"strings"

	"github.com/acornfox/acornfox/internal/cli"
)

func skillVersionChecks(r *doctorReport) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return
	}
	found, bad := cli.CheckInstalledSkills(home)
	for _, m := range bad {
		r.warn("SETUP-SKILL-01", "%s", cli.SkillMismatchLine(m))
	}
	if len(found) > 0 && len(bad) == 0 {
		r.add(true, "SETUP-SKILL-01", "Skill：%s 里的 AcornFox Skill 和命令行都是 %s 版", strings.Join(found, "、"), cli.Version)
	}
}
