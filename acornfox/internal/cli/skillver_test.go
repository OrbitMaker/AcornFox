package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withVersion(t *testing.T, v string) {
	t.Helper()
	old := Version
	Version = v
	t.Cleanup(func() { Version = old })
}

// The version comment goes right after the frontmatter, once; the repository
// copy of the Skill carries none.
func TestSkillTextCarriesVersion(t *testing.T) {
	withVersion(t, "0.7.0")
	if parseSkillVersion(skillContent) != "" {
		t.Fatal("the embedded Skill must not carry a version: it is added when written")
	}
	text := SkillText()
	if !strings.HasPrefix(text, "---\nname: acornfox\n") {
		t.Fatalf("frontmatter must stay first:\n%.200s", text)
	}
	if got := parseSkillVersion(text); got != "0.7.0" {
		t.Fatalf("version = %q", got)
	}
	// (The Skill's own text mentions the comment in a sentence; only a line of
	// its own is the version.)
	if n := len(skillVersionRE.FindAllString(text, -1)); n != 1 {
		t.Fatalf("version comment appears %d times", n)
	}
	i := strings.Index(text, "\n"+skillVersionPrefix) + 1
	if !strings.HasSuffix(text[:i], "\n---\n") {
		t.Fatalf("the comment must follow the closing --- of the frontmatter:\n%.400s", text[:i+60])
	}
	// Writing twice does not stack comments.
	if again := skillTextFor(text, "0.9.9"); again != text {
		t.Fatal("a text that already carries a version must be left alone")
	}
	// Every 分册 carries it too, on the line after its title, so the first
	// line of an installed file is still the title.
	for _, f := range SkillFiles()[1:] {
		if parseSkillVersion(f.Text) != "" {
			t.Fatalf("%s: the embedded 分册 must not carry a version", f.Name)
		}
		got, ok := SkillFileText(f.Name)
		if !ok {
			t.Fatalf("SkillFileText(%q) not found", f.Name)
		}
		lines := strings.SplitN(got, "\n", 3)
		if !strings.HasPrefix(lines[0], "# ") || lines[1] != skillVersionPrefix+"0.7.0 -->" {
			t.Fatalf("%s: title then version comment expected:\n%.200s", f.Name, got)
		}
		if parseSkillVersion(got) != "0.7.0" {
			t.Fatalf("%s: version not read back", f.Name)
		}
	}
	if _, ok := SkillFileText("nope.md"); ok {
		t.Fatal("an unknown file name must not be found")
	}
}

func TestVersionNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.6.4", "0.6.3", true}, {"0.6.3", "0.6.4", false}, {"0.6.3", "0.6.3", false},
		{"0.7.0", "0.6.9", true}, {"1.0.0", "0.9.9", true}, {"0.6.10", "0.6.9", true},
		{"v0.6.4", "0.6.3-dev", true}, {"0.6.3-dev", "0.6.3", false}, {"0.6.9-win.1", "0.6.8", true},
		{"", "0.6.3", false}, {"0.6.3", "", false}, {"abc", "0.6.3", false}, {"0.6", "0.5.0", false},
	}
	for _, c := range cases {
		if got := versionNewer(c.a, c.b); got != c.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func writeSkillFile(t *testing.T, home, bench, name, text string) string {
	t.Helper()
	dir := filepath.Join(home, bench, "skills", skillName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeSkill writes every file of the Skill as this program writes it.
func writeSkill(t *testing.T, home, bench string) {
	t.Helper()
	for _, f := range SkillFiles() {
		text, _ := SkillFileText(f.Name)
		writeSkillFile(t, home, bench, f.Name, text)
	}
}

// doctor's check: matching Skills are fine, an older, a newer, a versionless
// one and a half-written one (a 分册 missing or of another version) are
// reported, a workbench without a Skill is left out.
func TestCheckInstalledSkills(t *testing.T) {
	withVersion(t, "0.7.0")
	home := t.TempDir()
	if found, bad := CheckInstalledSkills(home); len(found) != 0 || len(bad) != 0 {
		t.Fatalf("empty home: %v %v", found, bad)
	}
	// A workbench directory without a Skill is not reported.
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, home, ".claude")
	found, bad := CheckInstalledSkills(home)
	if len(found) != 1 || found[0] != "Claude Code" || len(bad) != 0 {
		t.Fatalf("matching Skill: %v %v", found, bad)
	}

	// The entry alone (what an earlier version, or a half download, leaves).
	for _, c := range []struct {
		name, text string
		older      bool
		contains   string
	}{
		{"older", skillTextFor(skillContent, "0.6.1"), false, "0.6.1 版"},
		{"newer", skillTextFor(skillContent, "0.8.0"), true, "比这台电脑上的命令行"},
		{"versionless", skillContent, false, "没有版本号"},
	} {
		writeSkillFile(t, home, ".codex", skillEntry, c.text)
		_, bad := CheckInstalledSkills(home)
		if len(bad) != 1 || bad[0].Workbench != "Codex" || bad[0].ProgramOlder != c.older {
			t.Fatalf("%s: %+v", c.name, bad)
		}
		if line := SkillMismatchLine(bad[0]); !strings.Contains(line, c.contains) || !strings.Contains(line, "acornfox skill install") {
			t.Fatalf("%s: line = %s", c.name, line)
		}
	}

	// The entry is right, a 分册 is missing or of another version.
	writeSkill(t, home, ".codex")
	if _, bad := CheckInstalledSkills(home); len(bad) != 0 {
		t.Fatalf("a full Skill is fine: %+v", bad)
	}
	if err := os.Remove(filepath.Join(home, ".codex", "skills", skillName, "install-linux.md")); err != nil {
		t.Fatal(err)
	}
	writeSkillFile(t, home, ".codex", "deploy.md", "# 旧的\n<!-- acornfox-skill-version: 0.6.1 -->\n")
	_, bad = CheckInstalledSkills(home)
	if len(bad) != 1 || bad[0].Workbench != "Codex" || bad[0].Installed != "0.7.0" || len(bad[0].Bad) != 2 {
		t.Fatalf("half-written Skill: %+v", bad)
	}
	line := SkillMismatchLine(bad[0])
	for _, want := range []string{"install-linux.md", "deploy.md", "acornfox skill install", "0.7.0"} {
		if !strings.Contains(line, want) {
			t.Fatalf("line misses %q: %s", want, line)
		}
	}
	if strings.Contains(line, "app-center.md") {
		t.Fatalf("a good 分册 must not be listed: %s", line)
	}
}
