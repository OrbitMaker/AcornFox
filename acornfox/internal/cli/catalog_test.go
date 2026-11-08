package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogLintCommand(t *testing.T) {
	h := newHarness(t)
	good := filepath.Join("..", "catalog", "testdata", "good")
	code, out, _ := h.run("catalog", "lint", good)
	if code != exitOK || !strings.Contains(out, "0 个错误") {
		t.Fatalf("good: code=%d out=%s", code, out)
	}
	code, out, _ = h.run("--json", "catalog", "lint", good)
	var v struct {
		OK       bool              `json:"ok"`
		Apps     []string          `json:"apps"`
		Problems []json.RawMessage `json:"problems"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil || code != exitOK || !v.OK || len(v.Apps) != 2 {
		t.Fatalf("json: code=%d err=%v out=%s", code, err, out)
	}
	code, out, _ = h.run("--json", "catalog", "lint", t.TempDir())
	if code != exitOpFailed || !strings.Contains(out, "CAT-LINT-12") {
		t.Fatalf("empty: code=%d out=%s", code, out)
	}
	if code, _, _ := h.run("catalog", "lint"); code != exitUsage {
		t.Fatalf("no dir: code=%d", code)
	}
	if code, _, _ := h.run("catalog", "nope"); code != exitUsage {
		t.Fatalf("unknown sub: code=%d", code)
	}
}

func TestCatalogKeygenBuildVerify(t *testing.T) {
	h := newHarness(t)
	good := filepath.Join("..", "catalog", "testdata", "good")
	dir := t.TempDir()
	key := filepath.Join(dir, "key.pem")

	code, out, _ := h.run("--json", "catalog", "keygen", "--out", key)
	var kg struct {
		OK        bool   `json:"ok"`
		PublicKey string `json:"public_key"`
	}
	if err := json.Unmarshal([]byte(out), &kg); err != nil || code != exitOK || kg.PublicKey == "" {
		t.Fatalf("keygen: code=%d out=%s", code, out)
	}
	before, _ := os.ReadFile(key)
	code, out, _ = h.run("--json", "catalog", "keygen", "--out", key)
	if code != exitOpFailed || !strings.Contains(out, "CAT-KEY-03") {
		t.Fatalf("keygen 不该覆盖：code=%d out=%s", code, out)
	}
	if after, _ := os.ReadFile(key); string(after) != string(before) {
		t.Fatal("私钥被覆盖了")
	}

	dist := filepath.Join(dir, "dist")
	code, out, _ = h.run("--json", "catalog", "build", good, "--out", dist, "--serial", "7", "--key", key)
	if code != exitOK || !strings.Contains(out, `"dry_run":true`) || !strings.Contains(out, "index.json.sig") {
		t.Fatalf("dry-run: code=%d out=%s", code, out)
	}
	if _, err := os.Stat(dist); !os.IsNotExist(err) {
		t.Fatal("试跑写了文件")
	}
	code, out, _ = h.run("--json", "catalog", "build", good, "--out", dist, "--serial", "7", "--key", key, "--apply")
	var bd struct {
		OK    bool `json:"ok"`
		Files []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &bd); err != nil || code != exitOK || len(bd.Files) != 5 || bd.Files[4].Path != "version.txt" {
		t.Fatalf("build: code=%d out=%s", code, out)
	}

	code, out, _ = h.run("catalog", "verify", dist, "--pubkey", kg.PublicKey)
	if code != exitOK || !strings.Contains(out, "校验通过：序号 7") {
		t.Fatalf("verify: code=%d out=%s", code, out)
	}
	// Serial going down is refused, in a dry run as well as with --apply.
	for _, extra := range [][]string{nil, {"--apply"}} {
		args := append([]string{"--json", "catalog", "build", good, "--out", dist, "--serial", "6", "--key", key}, extra...)
		code, out, _ = h.run(args...)
		if code != exitOpFailed || !strings.Contains(out, "CAT-BUILD-04") {
			t.Fatalf("序号变小 %v：code=%d out=%s", extra, code, out)
		}
	}
	// A clean output directory checked against the last publish with --prev.
	code, out, _ = h.run("--json", "catalog", "build", good, "--out", filepath.Join(dir, "fresh"), "--serial", "5", "--key", key, "--prev", dist)
	if code != exitOpFailed || !strings.Contains(out, "CAT-BUILD-04") {
		t.Fatalf("--prev：code=%d out=%s", code, out)
	}
	if v, _ := os.ReadFile(filepath.Join(dist, "version.txt")); string(v) != "7\n" {
		t.Fatalf("version.txt = %q", v)
	}
	_, otherOut, _ := h.run("--json", "catalog", "keygen", "--out", filepath.Join(dir, "other.pem"))
	var other struct {
		PublicKey string `json:"public_key"`
	}
	_ = json.Unmarshal([]byte(otherOut), &other)
	code, out, _ = h.run("--json", "catalog", "verify", dist, "--pubkey", other.PublicKey)
	if code != exitOpFailed || !strings.Contains(out, "CAT-VER-02") {
		t.Fatalf("verify 错钥匙：code=%d out=%s", code, out)
	}
	code, _, _ = h.run("catalog", "verify", dist, "--pubkey", other.PublicKey, "--pubkey", kg.PublicKey)
	if code != exitOK {
		t.Fatalf("verify 多把公钥：code=%d", code)
	}

	if err := os.Chmod(key, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ = h.run("--json", "catalog", "build", good, "--out", dist, "--serial", "8", "--key", key)
	if code != exitOpFailed || !strings.Contains(out, "CAT-KEY-02") {
		t.Fatalf("权限过宽的私钥：code=%d out=%s", code, out)
	}

	for _, sub := range []string{"build", "keygen", "verify"} {
		code, out, _ := h.run("catalog", sub, "-h")
		if code != exitOK || !strings.Contains(out, "用途") || !strings.Contains(out, "副作用") {
			t.Fatalf("%s -h: code=%d out=%s", sub, code, out)
		}
	}
	if code, _, _ := h.run("catalog", "build", good); code != exitUsage {
		t.Fatalf("build 缺参数：code=%d", code)
	}
}
