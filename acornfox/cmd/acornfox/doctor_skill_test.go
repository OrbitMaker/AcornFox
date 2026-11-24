package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/cli"
)

func writeInstalledSkill(t *testing.T, home, bench, text string) {
	t.Helper()
	dir := filepath.Join(home, bench, "skills", "acornfox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeInstalledSkillAll writes the entry and every 分册 as `skill install` does.
func writeInstalledSkillAll(t *testing.T, home, bench string) {
	t.Helper()
	for _, f := range cli.SkillFiles() {
		text, _ := cli.SkillFileText(f.Name)
		dir := filepath.Join(home, bench, "skills", "acornfox")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func setHome(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// doctor's Skill line: nothing without an installed Skill; a pass line with a
// matching one; a SETUP-SKILL-01 warning (not a failure) with an old one.
func TestDoctorSkillVersionCheck(t *testing.T) {
	home := setHome(t)
	var r doctorReport
	r.OK = true
	skillVersionChecks(&r)
	if len(r.Items) != 0 {
		t.Fatalf("no Skill installed, no line: %+v", r.Items)
	}

	writeInstalledSkillAll(t, home, ".claude")
	r = doctorReport{OK: true}
	skillVersionChecks(&r)
	if len(r.Items) != 1 || !r.Items[0].OK || r.Items[0].Level != "ok" || r.Items[0].Check != "SETUP-SKILL-01" || !strings.Contains(r.Items[0].Text, cli.Version) {
		t.Fatalf("matching Skill: %+v", r.Items)
	}

	writeInstalledSkill(t, home, ".codex", "---\nname: acornfox\ndescription: x\n---\n<!-- acornfox-skill-version: 0.0.1 -->\n")
	r = doctorReport{OK: true}
	skillVersionChecks(&r)
	if !r.OK || len(r.Items) != 1 || r.Items[0].Level != "warn" || r.Items[0].Code != "SETUP-SKILL-01" ||
		!strings.Contains(r.Items[0].Text, "Codex") || !strings.Contains(r.Items[0].Text, "0.0.1") || !strings.Contains(r.Items[0].Text, "acornfox skill install") {
		t.Fatalf("old Skill: %+v", r.Items)
	}

	// The entry is right but a 分册 is missing: also a warning, naming the file.
	if err := os.RemoveAll(filepath.Join(home, ".codex")); err != nil {
		t.Fatal(err)
	}
	writeInstalledSkillAll(t, home, ".codex")
	if err := os.Remove(filepath.Join(home, ".codex", "skills", "acornfox", "app-center.md")); err != nil {
		t.Fatal(err)
	}
	r = doctorReport{OK: true}
	skillVersionChecks(&r)
	if !r.OK || len(r.Items) != 1 || r.Items[0].Level != "warn" || r.Items[0].Code != "SETUP-SKILL-01" ||
		!strings.Contains(r.Items[0].Text, "Codex") || !strings.Contains(r.Items[0].Text, "app-center.md") {
		t.Fatalf("a 分册 missing: %+v", r.Items)
	}
}
