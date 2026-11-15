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
	if len(apps) != 2 || len(recipes) != 3 || na["declared"] != "0.6.0" || na["computed"] != "0.6.0" || na["reasons"] == nil {
		t.Fatalf("min_platform: %s", out)
	}
	if r, _ := recipes["hermes-with-new-api"].(map[string]any); r["declared"] != "" || r["computed"] != "0.6.0" {
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

// The two service tables for people: service types by their label from the
// catalog, the three ways an item is obtained, and none of the manifest's
// field names.
func TestCatalogVarsCommandServices(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join("..", "catalog", "testdata", "services")
	code, out, _ := h.run("catalog", "vars", dir)
	if code != exitOK {
		t.Fatalf("exit %d out %s", code, out)
	}
	for _, want := range []string{
		"demo-consumer v1.0.0",
		"它需要什么服务（名字 | 服务类型 | 必须还是可选 | 没有会怎样）：",
		"    demo | 样例令牌 | 必须 | 装不上：这台机器上要先有一条能用的（没有就登记一条已有的）\n",
		"    mail | 邮箱（发信） | 可选 | 能用，但发不了通知邮件\n",
		"它能提供什么服务（服务类型 | 每一项怎么来）：",
		"    样例令牌 | token：给每个使用方单独发；url：平台算出来\n",
		"      平台代做的步骤：发凭据 有，收回 有，检查旧凭据 有\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("人看的输出里没有 %q：\n%s", want, out)
		}
	}
	// Each app has both tables, "没有" when empty.
	if strings.Count(out, "它需要什么服务") != 2 || strings.Count(out, "它能提供什么服务") != 2 || strings.Count(out, "它能提供什么服务：没有") != 1 || strings.Count(out, "它需要什么服务：没有") != 1 {
		t.Fatalf("两个应用各有两张表：\n%s", out)
	}
	for _, word := range []string{"kind", "uses", "provides", "computed", "issued"} {
		for _, line := range strings.Split(out, "\n") {
			// Variable rows name references such as uses.demo.url; the
			// service tables must not use the manifest's words.
			if strings.Contains(line, " | ") && strings.Contains(line, word) {
				t.Fatalf("服务表里出现了 %q：%s", word, line)
			}
		}
	}
	// A dedicated use, an ask item and a service type the catalog does
	// not define (shown by its name).
	cp := t.TempDir()
	for _, id := range []string{"demo-consumer", "demo-provider"} {
		b, err := os.ReadFile(filepath.Join(dir, "apps", id, "manifest.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		src = strings.Replace(src, "  - { name: demo, kind: demo_token }", "  - { name: demo, kind: demo_token, dedicated: true }", 1)
		src = strings.Replace(src, "token: { issued: token }", "token: { ask: 在应用里新建一个令牌，贴进来, secret: true }", 1)
		src = strings.Replace(src, "kind: smtp, optional: true", "kind: fax, optional: true", 1)
		if err := os.MkdirAll(filepath.Join(cp, "apps", id), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cp, "apps", id, "manifest.yaml"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out, _ = h.run("catalog", "vars", cp)
	for _, want := range []string{
		"    demo | demo_token | 必须，只接没有别的应用在用的服务（dedicated：接上以后别的应用不能再接） |",
		"    mail | fax | 可选 | 能用，但发不了通知邮件\n",
		"    demo_token | token：要用户做一步（在应用里新建一个令牌，贴进来）；url：平台算出来\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("exit %d：人看的输出里没有 %q：\n%s", code, want, out)
		}
	}
	code, out, _ = h.run("--json", "catalog", "vars", cp, "--app", "demo-provider")
	if code != exitOK || !strings.Contains(out, `"uses":[],"provides":[{"kind":"demo_token","values":[{"key":"token","how":"ask"},{"key":"url","how":"computed"}],"has_issue":true,"has_revoke":true,"has_check":true}]`) ||
		strings.Contains(out, "在应用里新建") {
		t.Fatalf("exit %d out %s", code, out)
	}
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
	for _, f := range []string{"app", "version", "file", "delivery_tier", "min_platform", "vars", "counts", "hardcoded", "fingerprint",
		"uses", "provides", "fingerprint_version"} {
		if _, ok := hermes[f]; !ok {
			t.Fatalf("报告缺字段 %s：%s", f, out)
		}
	}
	counts := hermes["counts"].(map[string]any)
	if hermes["app"] != "hermes-agent" || len(counts) != 5 || counts["system"] != float64(5) || counts["user_required"] != float64(1) ||
		hermes["fingerprint"] != "993efab5344ede8eac44b3cbc465d2c7c230125e16c8bc63bbccad489e6a930c" || hermes["fingerprint_version"] != float64(2) {
		t.Fatalf("hermes-agent：%s", out)
	}
	if _, ok := counts["llm_source"]; ok {
		t.Fatalf("a class format 3 does not have: %s", out)
	}
	// Both service lists are always there; Hermes needs one service and
	// offers none, New API offers one, its items obtained three ways.
	if uses := hermes["uses"].([]any); len(uses) != 1 || len(hermes["provides"].([]any)) != 0 ||
		!strings.Contains(out, `"uses":[{"name":"llm","kind":"llm_gateway","optional":false,"without":"","dedicated":false}]`) {
		t.Fatalf("hermes-agent 的服务：%s", out)
	}
	if !strings.Contains(out, `"uses":[],"provides":[{"kind":"llm_gateway","values":[{"key":"api_key","how":"issued"},{"key":"base_url","how":"computed"},{"key":"model","how":"ask"}],"has_issue":true,"has_revoke":true,"has_check":true}]`) {
		t.Fatalf("new-api 的服务：%s", out)
	}
	row := hermes["vars"].([]any)[0].(map[string]any)
	if row["name"] != "params.dashboard_password" || row["layer"] != "custom" || row["class"] != "generated" || row["used_in"] == nil {
		t.Fatalf("第一行：%v", row)
	}

	// --app picks one, before or after the directory.
	for _, args := range [][]string{{"--json", "catalog", "vars", good, "--app", "new-api"}, {"--json", "catalog", "vars", "--app", "new-api", good}} {
		code, out, _ = h.run(args...)
		one, _ := decodeJSON(t, out)["apps"].([]any)
		if code != exitOK || len(one) != 1 || one[0].(map[string]any)["fingerprint"] != "33a0170464fcfa372c3e774386d62da8d057e6cb720659b304aba23e9e87517a" {
			t.Fatalf("%v: exit %d out %s", args, code, out)
		}
	}
	code, out, _ = h.run("catalog", "vars", good, "--app", "hermes-agent")
	for _, want := range []string{"hermes-agent v0.23.0", "交付方式：要用户做几步", "最低平台版本：算出来 0.6.0，清单写的 0.6.0", "系统给 5", "必须用户填 1",
		"[必须用户填] params.model（模型名），用在 /files/config.yaml",
		"[系统给] platform.tz（时区），用在 /env/TZ", "[系统给] uses.llm.base_url，用在 /files/config.yaml", "[平台生成] params.dashboard_password",
		"它需要什么服务（名字 | 服务类型 | 必须还是可选 | 没有会怎样）：", "llm | 大模型接口 | 必须 | 装不上：这台机器上要先有一条能用的（没有就登记一条已有的）",
		"它能提供什么服务：没有", "指纹：993efab5", "（第 2 版）"} {
		if !strings.Contains(out, want) {
			t.Fatalf("人看的输出里没有 %q：\n%s", want, out)
		}
	}
	if code != exitOK || strings.Contains(out, "new-api") || strings.Contains(out, "模型来源") || strings.Contains(out, "旧写法") {
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
	if code, out, _ := h.run("catalog", "vars", "-h"); code != exitOK || !strings.Contains(out, "副作用：无，只读") || !strings.Contains(out, "fingerprint") || !strings.Contains(out, "例子") ||
		!strings.Contains(out, "它需要什么服务") || !strings.Contains(out, "它能提供什么服务") || !strings.Contains(out, "fingerprint_version") {
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
