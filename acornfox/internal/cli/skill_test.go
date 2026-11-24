package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSkillContentHasFrontmatter(t *testing.T) {
	if !strings.HasPrefix(skillContent, "---\nname: acornfox\ndescription: ") {
		t.Fatalf("SKILL.md must start with name/description frontmatter")
	}
	// The entry says where the rest is; the commands live in the 分册.
	corpus := SkillCorpus()
	for _, cmd := range []string{"acornfox deploy --json", "acornfox diagnose --json", "acornfox add postgres"} {
		if !strings.Contains(corpus, cmd) {
			t.Fatalf("the Skill missing %q", cmd)
		}
	}
	for _, f := range SkillFiles() {
		if f.Name != skillEntry && strings.HasPrefix(f.Text, "---") {
			t.Errorf("%s: a 分册 has no frontmatter (only the entry is a Skill to the workbench)", f.Name)
		}
	}
}

// skill install writes the entry and every 分册, each with the program's
// version, and the files one earlier version left behind are replaced or
// removed: the old single-file Skill, an old 分册 that carries our version
// comment. machine-*.md and files that are not ours stay.
func TestSkillInstallDir(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "acornfox")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// What the version before this one left: the single file, an old 分册 of
	// ours, a user's own note and a machine copy.
	old := map[string]string{
		"SKILL.md":         "---\nname: acornfox\ndescription: old\n---\n<!-- acornfox-skill-version: 0.6.3 -->\nold single-file Skill\n",
		"gone-guide.md":    "# 旧的一册\n<!-- acornfox-skill-version: 0.6.3 -->\nold\n",
		"my-notes.md":      "my own notes\n",
		"machine-home.md":  "<!-- 用户自己放在这里的，不是我们写的 -->\n",
		"machine-gone.txt": "keep\n",
	}
	for n, text := range old {
		if err := os.WriteFile(filepath.Join(skillDir, n), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for round := 1; round <= 2; round++ { // writing twice is not an error
		code, out, errOut := h.run("--json", "skill", "install", "--dir", dir)
		if code != exitOK {
			t.Fatalf("exit = %d stderr=%s", code, errOut)
		}
		m := decodeJSON(t, out)
		if m["ok"] != true || m["skill_version"] != Version {
			t.Fatalf("answer: %v", m)
		}
		inst := m["installed"].([]any)[0].(map[string]any)
		if int(inst["files"].(float64)) != len(SkillFiles()) {
			t.Fatalf("files = %v, want %d", inst["files"], len(SkillFiles()))
		}
		removed, _ := inst["removed"].([]any)
		if round == 1 && (len(removed) != 1 || removed[0] != "gone-guide.md") {
			t.Fatalf("removed = %v", removed)
		}
		if round == 2 && len(removed) != 0 {
			t.Fatalf("second round removed %v", removed)
		}
		for _, f := range SkillFiles() {
			data, err := os.ReadFile(filepath.Join(skillDir, f.Name))
			want, _ := SkillFileText(f.Name)
			if err != nil || string(data) != want {
				t.Fatalf("%s not written: %v", f.Name, err)
			}
			// Every file carries this program's version.
			if v := parseSkillVersion(string(data)); v != Version {
				t.Fatalf("%s: version = %q, want %q", f.Name, v, Version)
			}
		}
	}
	for _, keep := range []string{"my-notes.md", "machine-home.md", "machine-gone.txt"} {
		if _, err := os.Stat(filepath.Join(skillDir, keep)); err != nil {
			t.Errorf("%s must be left alone: %v", keep, err)
		}
	}
	if _, err := os.Stat(filepath.Join(skillDir, "gone-guide.md")); err == nil {
		t.Errorf("the old 分册 that carries our version comment must be removed")
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md.tmp")); err == nil {
		t.Errorf("a temporary file is left behind")
	}
}

// skill print: the entry by default, one 分册 with --file, everything with
// --all; a wrong name is a usage error that lists the names.
func TestSkillPrint(t *testing.T) {
	h := newHarness(t)
	code, out, _ := h.run("skill", "print")
	if code != exitOK || out != SkillText() {
		t.Fatalf("default prints the entry: code=%d", code)
	}
	for _, name := range []string{"install-linux.md", "install-linux"} {
		code, out, _ = h.run("skill", "print", "--file", name)
		want, _ := SkillFileText("install-linux.md")
		if code != exitOK || out != want {
			t.Fatalf("--file %s: code=%d", name, code)
		}
	}
	code, out, _ = h.run("skill", "print", "--file", "skill")
	if code != exitOK || out != SkillText() {
		t.Fatalf("--file skill is the entry")
	}
	code, out, _ = h.run("skill", "print", "--all")
	if code != exitOK {
		t.Fatalf("--all: code=%d", code)
	}
	for _, f := range SkillFiles() {
		if !strings.Contains(out, "<!-- 文件："+f.Name+" -->") {
			t.Errorf("--all misses %s", f.Name)
		}
	}
	if code, _, errOut := h.run("skill", "print", "--file", "nope.md"); code != exitUsage || !strings.Contains(errOut, "install-linux.md") {
		t.Fatalf("unknown file: code=%d stderr=%s", code, errOut)
	}
	if code, _, _ := h.run("skill", "print", "--all", "--file", "deploy.md"); code != exitUsage {
		t.Fatalf("--all with --file is a usage error, got %d", code)
	}
}

func TestDetectWorkbenches(t *testing.T) {
	home := t.TempDir()
	if got := detectWorkbenches(home); len(got) != 0 {
		t.Fatalf("empty home: %v", got)
	}
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := detectWorkbenches(home)
	if len(got) != 1 || got[0].SkillsDir != filepath.Join(home, ".claude", "skills") {
		t.Fatalf("got %v", got)
	}
}

// The repository copy (skills/acornfox/) must stay identical to the embedded
// Skill (internal/cli/skilldata/): the same files, the same text.
func TestSkillRepoCopyInSync(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "skills", "acornfox")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("repo copy not available: %v", err)
	}
	have := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		have[e.Name()] = true
	}
	for _, f := range SkillFiles() {
		data, rerr := os.ReadFile(filepath.Join(dir, f.Name))
		if rerr != nil {
			t.Errorf("skills/acornfox/%s is missing: copy internal/cli/skilldata/%s there", f.Name, f.Name)
			continue
		}
		if string(data) != f.Text {
			t.Errorf("skills/acornfox/%s differs from internal/cli/skilldata/%s", f.Name, f.Name)
		}
		delete(have, f.Name)
	}
	for extra := range have {
		t.Errorf("skills/acornfox/%s is not in internal/cli/skilldata/", extra)
	}
}

// Only `services add` and `services set` read an item written as `item=-`
// from standard input (parseItems); every other command, `install --set`
// included, takes the "-" as the value itself. A command example in the
// Skill or the user docs that writes `=-` anywhere else would have a user
// type a password that never gets read, and the app installed with "-" as
// its password.
func TestStdinItemsOnlyOnServiceCommands(t *testing.T) {
	docs := map[string]string{}
	for _, f := range SkillFiles() {
		docs["internal/cli/skilldata/"+f.Name] = f.Text
	}
	root := filepath.Join("..", "..", "..")
	rels := []string{"README.md", "README.en.md", "docs/user-guide.md", "docs/quick-start.md",
		"docs/capabilities.md", "docs/faults.md", "docs/release-notes/v0.6.0.md"}
	if more, err := filepath.Glob(filepath.Join(root, "skills", "acornfox", "*.md")); err == nil {
		for _, m := range more {
			rel, _ := filepath.Rel(root, m)
			rels = append(rels, filepath.ToSlash(rel))
		}
	}
	if more, err := filepath.Glob(filepath.Join(root, "docs", "acornfox", "*.md")); err == nil {
		for _, m := range more {
			rel, _ := filepath.Rel(root, m)
			rels = append(rels, filepath.ToSlash(rel))
		}
	}
	for _, rel := range rels {
		if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			docs[rel] = string(b)
		}
	}
	stdinItem := regexp.MustCompile(`(^|\s)[^\s=` + "`" + `]+=-(\s|$)`)
	// The commands that read item=- from standard input: registering or
	// changing a service, and install for its secret settings (--set).
	readsStdin := regexp.MustCompile(`acornfox\s+(--json\s+)?(services\s+(add|set)\s|install\s.*--set\s)`)
	checked := 0
	for rel, text := range docs {
		inFence := false
		for i, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inFence = !inFence
				continue
			}
			var cmds []string
			if inFence {
				cmds = append(cmds, line)
			} else {
				parts := strings.Split(line, "`")
				for j := 1; j < len(parts); j += 2 {
					cmds = append(cmds, parts[j])
				}
			}
			for _, c := range cmds {
				if !strings.Contains(c, "acornfox ") || !stdinItem.MatchString(c) {
					continue
				}
				checked++
				if !readsStdin.MatchString(c) {
					t.Errorf("%s:%d: %q writes item=- but only `acornfox services add/set` and `acornfox install --set` read it from standard input", rel, i+1, strings.TrimSpace(c))
				}
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no command example with item=- found: the check no longer looks at anything")
	}
	// The Skill says in words who types the secret: a weak model otherwise
	// writes the value into the command line or the conversation.
	for _, must := range []string{"**任何密码和 Key 都不能由你输入，你也不能把它写进命令行参数**", "保密的写成 `项=-`，原样保留"} {
		if !strings.Contains(SkillCorpus(), must) {
			t.Errorf("the Skill no longer says %q", must)
		}
	}
}
