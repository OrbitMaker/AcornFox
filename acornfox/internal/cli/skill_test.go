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
	for _, cmd := range []string{"acornfox deploy --json", "acornfox diagnose --json", "acornfox add postgres"} {
		if !strings.Contains(skillContent, cmd) {
			t.Fatalf("SKILL.md missing %q", cmd)
		}
	}
}

func TestSkillInstallDir(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	code, out, errOut := h.run("--json", "skill", "install", "--dir", dir)
	if code != exitOK {
		t.Fatalf("exit = %d stderr=%s", code, errOut)
	}
	if m := decodeJSON(t, out); m["ok"] != true {
		t.Fatalf("ok != true: %v", m)
	}
	data, err := os.ReadFile(filepath.Join(dir, "acornfox", "SKILL.md"))
	if err != nil || string(data) != skillContent {
		t.Fatalf("SKILL.md not written: %v", err)
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

// The repository copy must stay identical to the embedded Skill.
func TestSkillRepoCopyInSync(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "skills", "acornfox", "SKILL.md"))
	if err != nil {
		t.Skipf("repo copy not available: %v", err)
	}
	if string(data) != skillContent {
		t.Fatalf("skills/acornfox/SKILL.md differs from internal/cli/skilldata/SKILL.md")
	}
}

// Only `services add` and `services set` read an item written as `item=-`
// from standard input (parseItems); every other command, `install --set`
// included, takes the "-" as the value itself. A command example in the
// Skill or the user docs that writes `=-` anywhere else would have a user
// type a password that never gets read, and the app installed with "-" as
// its password.
func TestStdinItemsOnlyOnServiceCommands(t *testing.T) {
	docs := map[string]string{"internal/cli/skilldata/SKILL.md": skillContent}
	root := filepath.Join("..", "..", "..")
	rels := []string{"skills/acornfox/SKILL.md", "README.md", "README.en.md", "docs/user-guide.md", "docs/quick-start.md",
		"docs/capabilities.md", "docs/faults.md", "docs/release-notes/v0.6.0.md"}
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
		if !strings.Contains(skillContent, must) {
			t.Errorf("SKILL.md no longer says %q", must)
		}
	}
}
