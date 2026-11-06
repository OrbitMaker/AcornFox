package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func identityRun(t *testing.T, args ...string) (map[string]any, int) {
	t.Helper()
	var out, errb bytes.Buffer
	rc := run(append([]string{"identity"}, args...), strings.NewReader(""), &out, &errb)
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("not JSON (rc %d): %q %q", rc, out.String(), errb.String())
	}
	return m, rc
}

func TestIdentityCommand(t *testing.T) {
	dir := t.TempDir()
	m, rc := identityRun(t, "show", "--json", "--data-dir", dir)
	if rc != 0 || m["exists"] != false {
		t.Fatalf("show before: %v %d", m, rc)
	}
	// reset without --yes is a dry run and creates nothing.
	m, rc = identityRun(t, "reset", "--json", "--data-dir", dir)
	if rc != 0 || m["dry_run"] != true {
		t.Fatalf("dry run: %v", m)
	}
	m, _ = identityRun(t, "show", "--json", "--data-dir", dir)
	if m["exists"] != false {
		t.Fatal("dry run created a key")
	}
	m, rc = identityRun(t, "reset", "--yes", "--json", "--data-dir", dir)
	first, _ := m["install_id"].(string)
	if rc != 0 || !strings.HasPrefix(first, "afx-") {
		t.Fatalf("reset: %v", m)
	}
	m, _ = identityRun(t, "show", "--json", "--data-dir", dir)
	if m["install_id"] != first || m["public_key"] == "" {
		t.Fatalf("show after: %v", m)
	}
	m, _ = identityRun(t, "reset", "--apply", "--json", "--data-dir", dir)
	if m["install_id"] == first || m["previous_install_id"] != first {
		t.Fatalf("second reset: %v", m)
	}
	var out, errb bytes.Buffer
	if rc := run([]string{"identity", "--help"}, strings.NewReader(""), &out, &errb); rc != 0 || !strings.Contains(out.String(), "安装编号") {
		t.Fatalf("help rc %d", rc)
	}
}
