package main

// 通用 Skill（入口加分册）里写到的命令和故障编号都是真的（闸门）：
//
//   - 每条写成 `acornfox …` 的命令（行内代码和代码块里），它的子命令在命令表（internal/capabilities）里，
//     它的 --选项是命令表给这条命令登记的（加全局选项 --json、--target、--app）；
//   - 每个写到的故障编号（大写的 INSTALL-…、CAT-…、WIN-… 等）在故障目录里；
//   - Skill 目录（internal/cli/skilldata）和 acornfox capabilities 读到的套路标记对得上。
//
// 布局（行数、链接、标题）在 internal/cli/skill_layout_test.go 里查。

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/capabilities"
	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/faults"
)

// windowsSide are the actions of `acornfox windows …`: the forwarding command on
// Windows (scripts/install/install-windows.ps1), not a command of the Go program.
var windowsSide = map[string]bool{"status": true, "doctor": true, "upgrade": true, "uninstall": true, "lan-on": true, "lan-off": true}

// skillCmdWord is a word of the command path as the Skill writes it.
var skillCmdWord = regexp.MustCompile(`^[a-z][a-z-]*$`)

// acornfoxWord finds the program name as a word of its own: not part of
// "acornfox-users", "acornfox.cmd" or a path ("…/bin/acornfox"). Group 1 is the name.
var acornfoxWord = regexp.MustCompile(`(?:^|[^\w./-])(acornfox)(?:[^\w./-]|$)`)

// codeSegments returns the text the Skill writes as code: the lines of fenced
// blocks and the inline `spans`.
func codeSegments(text string) []string {
	var segs []string
	fence := false
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			fence = !fence
			continue
		}
		if fence {
			segs = append(segs, ln)
			continue
		}
		parts := strings.Split(ln, "`")
		for i := 1; i < len(parts); i += 2 {
			segs = append(segs, parts[i])
		}
	}
	return segs
}

// stopWords end the command: what follows belongs to another program.
var stopWords = map[string]bool{"|": true, "||": true, "&&": true, ";": true, ">": true, ">>": true, "2>": true, "<": true}

func cleanToken(w string) string {
	return strings.Trim(w, "[](){}'\"`,;，。；：）（")
}

// skillCommands returns the `acornfox …` commands of a code segment: the
// words after the program name up to the end of the command.
func skillCommands(seg string) [][]string {
	var out [][]string
	// `container machine run -n acornfox …`, `wsl -d …`: the other program's
	// words, "acornfox" is the name of a virtual machine there.
	if t := strings.TrimSpace(seg); strings.HasPrefix(t, "container ") || strings.HasPrefix(t, "wsl ") {
		return nil
	}
	for _, loc := range acornfoxWord.FindAllStringSubmatchIndex(seg, -1) {
		var words []string
		for _, w := range strings.Fields(seg[loc[3]:]) {
			if stopWords[w] || strings.HasPrefix(w, "$(") || w == "EOF" {
				break
			}
			words = append(words, w)
		}
		out = append(out, words)
	}
	return out
}

type cmdIndex struct {
	byPath map[string]capabilities.Command
	// top is the first words of all paths; group the ones that are only a
	// group (no command of their own, like "target", "mac", "catalog").
	top   map[string]bool
	group map[string]bool
}

func newCmdIndex() cmdIndex {
	ix := cmdIndex{byPath: map[string]capabilities.Command{}, top: map[string]bool{}, group: map[string]bool{}}
	for _, c := range capabilities.Commands {
		ix.byPath[c.Path] = c
		fields := strings.Fields(c.Path)
		ix.top[fields[0]] = true
		if len(fields) > 1 {
			for _, a := range c.Aliases {
				alias := strings.Join(append(append([]string{}, fields[:len(fields)-1]...), a), " ")
				ix.byPath[alias] = c
			}
		}
	}
	for top := range ix.top {
		if _, own := ix.byPath[top]; !own {
			ix.group[top] = true
		}
	}
	return ix
}

var globalFlags = map[string]bool{"--json": true, "--target": true, "--app": true, "--help": true, "-h": true}

// checkCommand checks one `acornfox …` command; it returns what is wrong, or "".
func (ix cmdIndex) checkCommand(words []string) string {
	// Global flags (and the value of --target and --app) may stand in front of
	// the command; the words of the command path are the lower-case ASCII ones.
	var path, rest []string
	i := 0
	for ; i < len(words); i++ {
		w := cleanToken(words[i])
		if w == "" {
			continue
		}
		if w == "--target" || w == "--app" {
			i++
			continue
		}
		if strings.HasPrefix(w, "-") {
			continue
		}
		break
	}
	j := i
	for ; j < len(words) && len(path) < 3; j++ {
		w := cleanToken(words[j])
		if !skillCmdWord.MatchString(w) {
			break
		}
		path = append(path, w)
	}
	rest = words[j:]
	if len(path) == 0 {
		return "" // “acornfox 命令行”“`acornfox` 服务”: prose, or a bare name
	}
	if path[0] == "windows" {
		if len(path) > 1 && !windowsSide[path[1]] {
			return "acornfox windows " + path[1] + "：Windows 一侧没有这个动作（status、doctor、upgrade、uninstall、lan-on、lan-off）"
		}
		return ""
	}
	if !ix.top[path[0]] {
		// A lower-case word right after "acornfox" that is no command: only a
		// mistake when it is where the command goes.
		return "acornfox " + path[0] + "：命令表里没有这个命令"
	}
	// The longest prefix of the words that is a command of the table.
	var cmd capabilities.Command
	found, used := false, 0
	for n := len(path); n >= 1; n-- {
		if c, ok := ix.byPath[strings.Join(path[:n], " ")]; ok {
			cmd, found, used = c, true, n
			break
		}
	}
	if !found {
		if ix.group[path[0]] {
			if len(path) == 1 {
				return "" // “acornfox target”: the group named in prose
			}
			return "acornfox " + strings.Join(path, " ") + "：命令表里没有这条子命令"
		}
		return "acornfox " + strings.Join(path[:1], " ") + "：命令表里没有这条命令"
	}
	if ix.group[path[0]] && used == 1 && len(path) > 1 {
		return "acornfox " + strings.Join(path, " ") + "：命令表里没有这条子命令"
	}
	// The flags written after the command are the ones the table lists for it.
	have := map[string]bool{}
	for _, f := range cmd.Flags {
		have[f] = true
	}
	for _, w := range append(append([]string{}, path[used:]...), rest...) {
		w = cleanToken(w)
		if !strings.HasPrefix(w, "--") {
			continue
		}
		if k := strings.Index(w, "="); k > 0 {
			w = w[:k]
		}
		if globalFlags[w] || have[w] {
			continue
		}
		return "acornfox " + cmd.Path + " 没有选项 " + w + "（命令表登记的：" + strings.Join(cmd.Flags, " ") + "）"
	}
	return ""
}

func TestSkillCommandsAreInTheTable(t *testing.T) {
	ix := newCmdIndex()
	checked := 0
	for _, f := range cli.SkillFiles() {
		for _, seg := range codeSegments(f.Text) {
			for _, words := range skillCommands(seg) {
				if msg := ix.checkCommand(words); msg != "" {
					t.Errorf("%s：`%s` 里写到 %s", f.Name, strings.TrimSpace(seg), msg)
				}
				if len(words) > 0 {
					checked++
				}
			}
		}
	}
	if checked < 200 {
		t.Fatalf("只查到 %d 条命令：检查已经不看任何东西了", checked)
	}
	// The checker itself: a made-up command and a made-up flag are caught.
	if ix.checkCommand(strings.Fields("--json apps upgrade 应用名 --nope")) == "" {
		t.Error("检查器漏了不存在的选项")
	}
	if ix.checkCommand(strings.Fields("frobnicate --json")) == "" {
		t.Error("检查器漏了不存在的命令")
	}
	if ix.checkCommand(strings.Fields("services frob x")) == "" {
		t.Error("检查器漏了不存在的子命令")
	}
	if msg := ix.checkCommand(strings.Fields("--json --app myapp status")); msg != "" {
		t.Errorf("检查器误报：%s", msg)
	}
}

// Every upper-case fault code the Skill writes is in the catalog.
var skillFaultCode = regexp.MustCompile(`\b(?:CAT-(?:INST|CONN|SVC|LINT|BUILD|KEY|VER|SYNC|LOAD|PKG|SHOW|SKILL)|SETUP-[A-Z]+|EDGE|LAN-CONSOLE|INSTALL-[A-Z]+|UPGRADE-[A-Z]+|UNINSTALL-[A-Z]+|WIN-[A-Z]+|MAC-[A-Z]+|OPEN-APP|APP-VARS|APP-APPLY|APP-UPG|ID-KEY|PDOMAIN|PUBCON|BKP|MACHINE|PLAT-UPD|CLI-[A-Z]+|GW-[A-Z]+)-\d{2}\b`)

func TestSkillFaultCodesAreInTheCatalog(t *testing.T) {
	have := map[string]bool{}
	for _, f := range faults.Catalog {
		have[f.Code] = true
	}
	seen := map[string]bool{}
	for _, f := range cli.SkillFiles() {
		for _, code := range skillFaultCode.FindAllString(f.Text, -1) {
			seen[code] = true
			if !have[code] {
				t.Errorf("%s 里写到故障编号 %s，故障目录里没有", f.Name, code)
			}
		}
	}
	if len(seen) < 30 {
		t.Fatalf("只在 Skill 里找到 %d 个故障编号：检查已经不看任何东西了", len(seen))
	}
	// Ranges such as `EDGE-01`～`EDGE-04` name real codes at both ends (the
	// regexp above sees both); nothing else to check.
}

// The pattern markers of the whole Skill are what `acornfox capabilities`
// lists, wherever in the Skill they stand.
func TestSkillPatternsReadFromEveryFile(t *testing.T) {
	doc := capabilities.Build(cli.Version, cli.SkillCorpus())
	if !contains(doc.SkillPatterns, "llm-key") {
		t.Fatalf("套路 llm-key 要能从分册里读到，现在读到 %v", doc.SkillPatterns)
	}
	// llm-key stands in app-center.md, not in the entry.
	for _, f := range cli.SkillFiles() {
		got := capabilities.SkillPatterns(f.Text)
		sort.Strings(got)
		if f.Name == "SKILL.md" && len(got) != 0 {
			t.Errorf("入口里不放套路，现在有 %v", got)
		}
	}
}
