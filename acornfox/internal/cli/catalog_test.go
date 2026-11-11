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
	// Each app and recipe comes with the platform version it declares and
	// the one worked out from what it uses.
	mp, _ := decodeJSON(t, out)["min_platform"].(map[string]any)
	apps, _ := mp["apps"].(map[string]any)
	recipes, _ := mp["recipes"].(map[string]any)
	na, _ := apps["new-api"].(map[string]any)
	if len(apps) != 2 || len(recipes) != 3 || na["declared"] != "0.5.0" || na["computed"] != "0.5.0" || na["reasons"] == nil {
		t.Fatalf("min_platform: %s", out)
	}
	if r, _ := recipes["hermes-with-new-api"].(map[string]any); r["declared"] != "" || r["computed"] != "0.5.0" {
		t.Fatalf("recipe min_platform: %s", out)
	}
	if code, out, _ := h.run("catalog", "lint", "-h"); code != exitOK || !strings.Contains(out, "CAT-LINT-17") || !strings.Contains(out, "副作用：无，只读") {
		t.Fatalf("lint 帮助：%d %s", code, out)
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

// copyGoodCatalog copies the catalog package's good test catalog so a test
// can change it.
func copyGoodCatalog(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "catalog", "testdata", "good")
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestCatalogVarsCommand(t *testing.T) {
	h := newHarness(t)
	good := filepath.Join("..", "catalog", "testdata", "good")

	code, out, _ := h.run("--json", "catalog", "vars", good)
	m := decodeJSON(t, out)
	apps, _ := m["apps"].([]any)
	if code != exitOK || m["ok"] != true || len(apps) != 2 || len(m["problems"].([]any)) != 0 {
		t.Fatalf("exit %d out %s", code, out)
	}
	hermes := apps[0].(map[string]any)
	for _, f := range []string{"app", "version", "file", "delivery_tier", "min_platform", "vars", "counts", "hardcoded", "fingerprint"} {
		if _, ok := hermes[f]; !ok {
			t.Fatalf("报告缺字段 %s：%s", f, out)
		}
	}
	counts := hermes["counts"].(map[string]any)
	if hermes["app"] != "hermes-agent" || len(counts) != 6 || counts["llm_source"] != float64(1) || counts["user_required"] != float64(0) ||
		hermes["fingerprint"] != "bd7dfeb61301654ae8628d2b5f37f5ad7f9ff0b0a6c1ec49d461a25102877d3b" {
		t.Fatalf("hermes-agent：%s", out)
	}
	row := hermes["vars"].([]any)[0].(map[string]any)
	if row["name"] != "params.dashboard_password" || row["layer"] != "custom" || row["class"] != "generated" || row["used_in"] == nil {
		t.Fatalf("第一行：%v", row)
	}

	// --app picks one, before or after the directory.
	for _, args := range [][]string{{"--json", "catalog", "vars", good, "--app", "new-api"}, {"--json", "catalog", "vars", "--app", "new-api", good}} {
		code, out, _ = h.run(args...)
		one, _ := decodeJSON(t, out)["apps"].([]any)
		if code != exitOK || len(one) != 1 || one[0].(map[string]any)["fingerprint"] != "42c2264decb6fc511deaf585a3907e5bc2bd462337f6322a56958835dbfc5c61" {
			t.Fatalf("%v: exit %d out %s", args, code, out)
		}
	}
	code, out, _ = h.run("catalog", "vars", good, "--app", "hermes-agent")
	for _, want := range []string{"hermes-agent v0.23.0", "交付方式：全自动", "最低平台版本：算出来 0.5.0，清单写的 0.5.0", "要选模型来源 1", "必须用户填 0",
		"[系统给] platform.tz（时区），用在 /env/TZ", "[要选模型来源] params.llm（大模型），用在 /files/config.yaml", "[平台生成] params.dashboard_password", "指纹：bd7dfeb6"} {
		if !strings.Contains(out, want) {
			t.Fatalf("人看的输出里没有 %q：\n%s", want, out)
		}
	}
	if code != exitOK || strings.Contains(out, "new-api") {
		t.Fatalf("exit %d out %s", code, out)
	}

	// An app that is not there, a broken manifest, wrong usage.
	if code, out, _ := h.run("--json", "catalog", "vars", good, "--app", "nope"); code != exitOpFailed || !strings.Contains(out, "nope") || !strings.Contains(out, "CAT-LINT-01") || decodeJSON(t, out)["ok"] != false {
		t.Fatalf("没有的应用：exit %d out %s", code, out)
	}
	dir := copyGoodCatalog(t)
	file := filepath.Join(dir, "apps", "new-api", "manifest.yaml")
	b, _ := os.ReadFile(file)
	_ = os.WriteFile(file, []byte(strings.Replace(string(b), "maintainer: acornfox", "maintainer: acornfox\nsidecars: []", 1)), 0o644)
	code, out, _ = h.run("--json", "catalog", "vars", dir)
	m = decodeJSON(t, out)
	if code != exitOpFailed || m["ok"] != false || len(m["apps"].([]any)) != 1 || !strings.Contains(out, "CAT-LINT-02") {
		t.Fatalf("坏清单：exit %d out %s", code, out)
	}
	// A hard-coded time zone and a parameter the user must fill in show up
	// for people too.
	_ = os.WriteFile(file, []byte(strings.Replace(strings.Replace(string(b), "{ key: TZ, from: platform.tz }", "{ key: TZ, value: Asia/Shanghai }", 1),
		"\nenv:\n", "  - key: site_name\n    type: string\n    required: true\n\nenv:\n", 1)), 0o644)
	code, out, _ = h.run("catalog", "vars", dir, "--app", "new-api")
	if code != exitOK || !strings.Contains(out, "! 提醒 /env/TZ = Asia/Shanghai") || !strings.Contains(out, "[必须用户填] params.site_name") || !strings.Contains(out, "CAT-LINT-18") {
		t.Fatalf("exit %d out %s", code, out)
	}
	// A directory that does not exist is an error (as it is for lint), not
	// an empty catalog.
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	code, out, _ = h.run("--json", "catalog", "vars", missing)
	m = decodeJSON(t, out)
	if code != exitOpFailed || m["ok"] != false || len(m["apps"].([]any)) != 0 || !strings.Contains(out, "CAT-LINT-01") || !strings.Contains(out, "no-such-dir") {
		t.Fatalf("目录不存在：exit %d out %s", code, out)
	}
	if code, out, _ := h.run("catalog", "vars", missing); code != exitOpFailed || !strings.Contains(out, "不存在") {
		t.Fatalf("目录不存在：exit %d out %s", code, out)
	}
	if code, _, _ := h.run("catalog", "lint", missing); code != exitOpFailed {
		t.Fatalf("lint 对不存在的目录也是退出码 1，得到 %d", code)
	}
	if code, _, _ := h.run("catalog", "vars"); code != exitUsage {
		t.Fatalf("没给目录 exit %d", code)
	}
	if code, out, _ := h.run("catalog", "vars", "-h"); code != exitOK || !strings.Contains(out, "副作用：无，只读") || !strings.Contains(out, "fingerprint") || !strings.Contains(out, "例子") {
		t.Fatalf("帮助：%d %s", code, out)
	}
	if code, out, _ := h.run("catalog", "-h"); code != exitOK || !strings.Contains(out, "vars 目录") || !strings.Contains(out, "gaps 目录") {
		t.Fatalf("catalog 帮助里要列出 vars 和 gaps：%d %s", code, out)
	}
}

func TestCatalogGapsCommand(t *testing.T) {
	h := newHarness(t)
	dir := copyGoodCatalog(t)

	// No register: not an error.
	code, out, _ := h.run("--json", "catalog", "gaps", dir)
	m := decodeJSON(t, out)
	if code != exitOK || m["ok"] != true || m["registered"] != false || len(m["gaps"].([]any)) != 0 || len(m["notices"].([]any)) != 0 || m["file"] != "gaps.yaml" {
		t.Fatalf("没有文件：exit %d out %s", code, out)
	}
	if code, out, _ := h.run("catalog", "gaps", dir); code != exitOK || !strings.Contains(out, "没有登记") {
		t.Fatalf("没有文件：exit %d out %s", code, out)
	}

	entry := func(id, app, want, status string) string {
		return "  - id: " + id + "\n    app: " + app + "\n    upstream_key: HF_ENDPOINT\n    want: " + want +
			"\n    layer: platform\n    handling: hardcoded\n    doc_url: https://example.com/docs\n    date: 2026-01-31\n    status: " + status + "\n"
	}
	gaps := filepath.Join(dir, "gaps.yaml")
	_ = os.WriteFile(gaps, []byte("schema: 1\ngaps:\n"+entry("GAP-0001", "new-api", "platform.tz", "open")+
		entry("GAP-0002", "new-api", "platform.mirror.huggingface", "planned")+entry("GAP-0003", "gone-app", "platform.gpu.count", "open")), 0o644)
	code, out, _ = h.run("--json", "catalog", "gaps", dir)
	m = decodeJSON(t, out)
	notices, _ := m["notices"].([]any)
	by, _ := m["by_status"].(map[string]any)
	if code != exitOK || m["ok"] != true || m["registered"] != true || len(m["gaps"].([]any)) != 3 || len(notices) != 2 ||
		by["open"] != float64(2) || by["planned"] != float64(1) || by["done"] != float64(0) {
		t.Fatalf("exit %d out %s", code, out)
	}
	n0, n1 := notices[0].(map[string]any), notices[1].(map[string]any)
	if n0["id"] != "GAP-0001" || n0["kind"] != "can_close" || !strings.Contains(n0["message"].(string), "可以关了：变量已有，请改清单") ||
		n1["id"] != "GAP-0003" || n1["kind"] != "app_missing" || n1["app"] != "gone-app" {
		t.Fatalf("提示：%s", out)
	}
	code, out, _ = h.run("catalog", "gaps", dir)
	if code != exitOK || !strings.Contains(out, "! GAP-0001 new-api：可以关了") || !strings.Contains(out, "登记了 3 条：待定 2，已排期 1，已有 0；2 条提示") {
		t.Fatalf("exit %d out %s", code, out)
	}

	// A malformed register: CAT-LINT-19, exit 1.
	_ = os.WriteFile(gaps, []byte("schema: 1\ngaps:\n"+strings.Replace(entry("GAP-0001", "new-api", "platform.tz", "open"), "layer: platform", "layer: system", 1)), 0o644)
	code, out, _ = h.run("--json", "catalog", "gaps", dir)
	m = decodeJSON(t, out)
	if code != exitOpFailed || m["ok"] != false || !strings.Contains(out, "CAT-LINT-19") || len(m["notices"].([]any)) != 0 {
		t.Fatalf("格式不对：exit %d out %s", code, out)
	}
	if code, out, _ := h.run("catalog", "gaps", dir); code != exitOpFailed || !strings.Contains(out, "CAT-LINT-19") || !strings.Contains(out, "没有核对") {
		t.Fatalf("格式不对：exit %d out %s", code, out)
	}
	// A directory that does not exist: exit 1, not "nothing registered".
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	code, out, _ = h.run("--json", "catalog", "gaps", missing)
	m = decodeJSON(t, out)
	if code != exitOpFailed || m["ok"] != false || m["registered"] != false || !strings.Contains(out, "CAT-LINT-01") || !strings.Contains(out, "no-such-dir") {
		t.Fatalf("目录不存在：exit %d out %s", code, out)
	}
	if code, out, _ := h.run("catalog", "gaps", missing); code != exitOpFailed || !strings.Contains(out, "不存在") || strings.Contains(out, "没有登记") {
		t.Fatalf("目录不存在：exit %d out %s", code, out)
	}
	if code, _, _ := h.run("catalog", "gaps"); code != exitUsage {
		t.Fatalf("没给目录 exit %d", code)
	}
	if code, out, _ := h.run("catalog", "gaps", "-h"); code != exitOK || !strings.Contains(out, "副作用：无，只读") || !strings.Contains(out, "CAT-LINT-19") || !strings.Contains(out, "例子") {
		t.Fatalf("帮助：%d %s", code, out)
	}
}
