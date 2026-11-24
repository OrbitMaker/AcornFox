package cli

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/acornfox/acornfox/internal/client"
)

// skillFS holds the AcornFox Skill: the entry SKILL.md (with the
// name/description frontmatter) and the 分册 (installation, deploying, the
// app center, ...), one Markdown file each. skills/acornfox/ at the
// repository root is a copy of this directory for readers browsing the
// repo (a test keeps the two identical). What `skill install` and `skill
// print` write is SkillFileText(): the file's text plus the program's version.
//
//go:embed skilldata/*.md
var skillFS embed.FS

// skillEntry is the entry file of the Skill; the other files are read through
// it ("用户说什么 → 读哪一册").
const skillEntry = "SKILL.md"

// skillContent is the entry file as embedded (without a version comment).
var skillContent string

func init() {
	b, err := skillFS.ReadFile("skilldata/" + skillEntry)
	if err != nil {
		panic("skilldata/" + skillEntry + " is not embedded: " + err.Error())
	}
	skillContent = string(b)
}

// skillName is the directory the Skill is installed under.
const skillName = "acornfox"

// SkillFile is one file of the Skill as embedded (no version comment yet).
type SkillFile struct {
	Name string
	Text string
}

// SkillFiles returns every file of the Skill: SKILL.md first, then the 分册
// sorted by name.
func SkillFiles() []SkillFile {
	entries, err := fs.ReadDir(skillFS, "skilldata")
	if err != nil {
		panic("skilldata is not embedded: " + err.Error())
	}
	var rest []SkillFile
	var entry SkillFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, rerr := skillFS.ReadFile("skilldata/" + e.Name())
		if rerr != nil {
			panic(rerr)
		}
		f := SkillFile{Name: e.Name(), Text: string(b)}
		if e.Name() == skillEntry {
			entry = f
			continue
		}
		rest = append(rest, f)
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	return append([]SkillFile{entry}, rest...)
}

// skillFileNames returns the names of the files of the Skill (SKILL.md first).
func skillFileNames() []string {
	files := SkillFiles()
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	return names
}

// cmdSkill handles the `acornfox skill` subcommands.
func (a *app) cmdSkill(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.out.usageError("skill 需要子命令：install / print / refresh")
	}
	switch args[0] {
	case "install":
		return a.cmdSkillInstall(ctx, args[1:])
	case "refresh":
		return a.cmdSkillRefresh(ctx, args[1:])
	case "print":
		return a.cmdSkillPrint(args[1:])
	default:
		return a.out.usageError("未知的 skill 子命令：%s", args[0])
	}
}

const skillPrintUsage = `用法：acornfox skill print [--all | --file 文件名]

用途：把 Skill 的内容打印出来，给其他工具用，或在服务器上取同版本的 Skill（情况丙）。Skill 分入口和分册：
      默认打印入口 SKILL.md；--all 打印全部文件（每个文件前有一行 <!-- 文件：名字 -->）；--file 打印一册，例如 --file install-linux.md。
      每个文件头带这个命令行的版本号。
副作用：只读，不连服务器，不写任何文件。
退出码：0 正常；2 参数不对（--file 的名字不在 Skill 里时错误里列出全部文件名）。
例子：
  acornfox skill print > SKILL.md
  acornfox skill print --file install-linux.md
  acornfox skill print --all`

// cmdSkillPrint implements `skill print [--all | --file NAME]`.
func (a *app) cmdSkillPrint(args []string) int {
	fs := a.catalogFlags("skill print", skillPrintUsage)
	all := fs.Bool("all", false, "打印全部文件")
	file := fs.String("file", "", "打印指定的一册，例如 install-linux.md")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 || (*all && *file != "") {
		return a.out.usageError("用法：skill print [--all | --file 文件名]")
	}
	switch {
	case *all:
		for _, f := range SkillFiles() {
			fmt.Fprintf(a.out.stdout, "<!-- 文件：%s -->\n", f.Name)
			fmt.Fprint(a.out.stdout, skillTextFor(f.Text, Version))
			if !strings.HasSuffix(f.Text, "\n") {
				fmt.Fprintln(a.out.stdout)
			}
		}
	case *file != "":
		text, ok := SkillFileText(normalizeSkillFileName(*file))
		if !ok {
			return a.out.usageError("Skill 里没有 %q；文件有：%s", *file, strings.Join(skillFileNames(), "、"))
		}
		fmt.Fprint(a.out.stdout, text)
	default:
		fmt.Fprint(a.out.stdout, SkillText())
	}
	return exitOK
}

// normalizeSkillFileName lets `--file install-linux` and `--file skill` mean
// the files install-linux.md and SKILL.md.
func normalizeSkillFileName(name string) string {
	name = strings.TrimSpace(name)
	if strings.EqualFold(name, "skill") || strings.EqualFold(name, "skill.md") {
		return skillEntry
	}
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	return name
}

// workbench is an AI workbench whose skills directory we install into.
type workbench struct {
	Name      string
	SkillsDir string // parent of <skillName>/SKILL.md
}

const skillInstallUsage = `用法：acornfox skill install [--dir DIR]

用途：把 AcornFox Skill（入口 SKILL.md 加全部分册）装到 AI 工作台：Claude Code（~/.claude/skills/acornfox/）、Codex（~/.codex/skills/acornfox/）；
      --dir 指定别的 skills 目录（写到 DIR/acornfox/）。每个文件头带这个命令行的版本号，acornfox doctor 逐个核对。
      同时给每台登记的服务器写一份“本机能力”分册 machine-服务器名.md。
前提：电脑上有 ~/.claude 或 ~/.codex（没有的用 --dir，或用 skill print 取内容）。
副作用：写 Skill 目录里的这些文件；目录里旧版本留下的、这一版已经没有的分册（带我们写的版本号的 .md）会删掉，machine-*.md 和别的文件不动；
      写、重装都不出错。服务器连不上只提醒，Skill 照样装好。
输出（--json）：{"ok":true,"installed":[{"workbench","path","files":N,"removed":["旧文件"]}],"failed":0,"skill_version","machines":{...}}
退出码：0 至少装进了一个工作台；1 全都没装成；2 参数不对。
例子：
  acornfox skill install
  acornfox skill install --dir ~/my-tool/skills`

// cmdSkillInstall implements `skill install [--dir DIR]`. Without --dir it
// installs into every detected workbench that loads SKILL.md directories;
// with --dir it writes DIR/acornfox/ for any other tool. Next to the Skill
// files it writes the part "what this server can do now" for each
// registered server (machine.go); a server that cannot be reached is only
// reported, the Skill is installed all the same.
func (a *app) cmdSkillInstall(ctx context.Context, args []string) int {
	fs := a.catalogFlags("skill install", skillInstallUsage)
	dir := fs.String("dir", "", "安装到指定的 skills 目录（写入 DIR/acornfox/）")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(rest) != 0 {
		return a.out.usageError("用法：skill install [--dir DIR]")
	}

	var targets []workbench
	if *dir != "" {
		targets = []workbench{{Name: "自定义目录", SkillsDir: *dir}}
	} else {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return a.out.usageError("无法确定用户主目录：%v", herr)
		}
		targets = detectWorkbenches(home)
	}
	if len(targets) == 0 {
		return a.out.usageError("未检测到支持的 AI 工作台（Claude Code：~/.claude，Codex：~/.codex）；其他工具用 skill install --dir 指定 skills 目录，或用 skill print 输出内容")
	}

	var installed []map[string]any
	var skillDirs []string
	failed := 0
	for _, wb := range targets {
		res, ierr := installSkill(wb.SkillsDir)
		if ierr != nil {
			failed++
			if !a.out.json {
				a.out.humanErr("✗ %s：%v", wb.Name, ierr)
			}
			continue
		}
		installed = append(installed, map[string]any{"workbench": wb.Name, "path": res.EntryPath, "files": res.Files, "removed": nonNilStrings(res.Removed)})
		skillDirs = append(skillDirs, filepath.Dir(res.EntryPath))
		if !a.out.json {
			a.out.human("✓ %s：已安装到 %s（入口加 %d 册）", wb.Name, filepath.Dir(res.EntryPath), res.Files-1)
			for _, r := range res.Removed {
				a.out.human("  已删除旧版本留下的文件：%s", r)
			}
		}
	}

	if len(installed) == 0 {
		if a.out.json {
			a.out.emitDiagnosisJSON(client.Diagnosis{Stage: "skill", Code: "install_failed", Message: "Skill 安装失败"})
		}
		return exitOpFailed
	}
	parts, partsFailed, _ := a.refreshMachineParts(ctx, skillDirs, nil)
	if a.out.json {
		a.out.emitJSON(map[string]any{"installed": installed, "failed": failed, "skill_version": Version,
			"machines": map[string]any{"refreshed": nonNil(parts), "failed": nonNil(partsFailed)}})
		return exitOK
	}
	for _, f := range partsFailed {
		a.out.humanErr("✗ 服务器 %s 的“本机能力”分册没写成：%s（%s）；以后用 acornfox skill refresh 补", f.Server, f.Message, f.Code)
	}
	if len(parts) > 0 {
		a.out.human("已写“本机能力”分册：每台登记的服务器一份（machine-服务器名.md），AI 工作台开始做事前会读；之后装、删、升级应用会自动刷新。")
	}
	a.out.human("在 AI 工作台中说“用 acornfox 部署这个项目”即可。")
	return exitOK
}

// detectWorkbenches returns the workbenches present under home. A workbench
// counts as present when its config directory exists; its skills directory is
// created on install.
func detectWorkbenches(home string) []workbench {
	candidates := []struct {
		name, configDir string
	}{
		{"Claude Code", ".claude"},
		{"Codex", ".codex"},
	}
	var out []workbench
	for _, c := range candidates {
		cfg := filepath.Join(home, c.configDir)
		if fi, err := os.Stat(cfg); err == nil && fi.IsDir() {
			out = append(out, workbench{Name: c.name, SkillsDir: filepath.Join(cfg, "skills")})
		}
	}
	return out
}

// installResult is what writing the Skill into one directory did.
type installResult struct {
	EntryPath string   // …/acornfox/SKILL.md
	Files     int      // files written (the entry included)
	Removed   []string // old files of an earlier version that were deleted
}

// installSkill writes every file of the Skill into skillsDir/acornfox/ (the
// 分册 first, SKILL.md last, so a reader that opens the entry always finds the
// 分册 it names) and deletes what an earlier version left there: a .md file
// that carries our version comment and is no longer part of the Skill. The
// machine-*.md copies and files that are not ours are not touched.
func installSkill(skillsDir string) (installResult, error) {
	dir := filepath.Join(skillsDir, skillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return installResult{}, fmt.Errorf("创建目录失败：%w", err)
	}
	files := SkillFiles()
	have := map[string]bool{}
	for _, f := range files {
		have[f.Name] = true
	}
	ordered := append(append([]SkillFile{}, files[1:]...), files[0])
	res := installResult{EntryPath: filepath.Join(dir, skillEntry)}
	for _, f := range ordered {
		path := filepath.Join(dir, f.Name)
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(skillTextFor(f.Text, Version)), 0o644); err != nil {
			return res, fmt.Errorf("写入文件 %s 失败：%w", f.Name, err)
		}
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			return res, fmt.Errorf("写入文件 %s 失败：%w", f.Name, err)
		}
		res.Files++
	}
	res.Removed = removeStaleSkillFiles(dir, have)
	return res, nil
}

// removeStaleSkillFiles deletes the .md files of dir that an earlier version
// of the Skill wrote (their version comment says so) and that this version
// does not have. have holds the names of the files of this version.
func removeStaleSkillFiles(dir string, have map[string]bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var removed []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || have[name] || strings.HasPrefix(name, "machine-") {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil || parseSkillVersion(string(data)) == "" {
			continue
		}
		if os.Remove(filepath.Join(dir, name)) == nil {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	return removed
}
