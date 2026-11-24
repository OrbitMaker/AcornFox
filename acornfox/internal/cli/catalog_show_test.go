package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
)

const showCatalogRaw = `{"serial":7,"origin":"cache","sync":{"ok":true,"message":"目录已是最新","show":{"state":"ok","serial":7}},
"industries":[{"id":"office","name":"通用办公"}],"home":{"solutions":["duo"]},"assist":{"visible":true,"price_cny":199},
"apps":[{"id":"hermes-agent","version":"v1","name":"Hermes Agent","summary":"清单里的一句话","supported":true,"installed":[],"source":{"id":"public","name":"公共目录"},"memory_mb":{"min":256},"needs_platform":"",
"show":{"name":"Hermes 智能体","tagline":"会聊天、会办事的助手","flag":"new","color":"#3b5bdb","cover":"https://acornfox.com/catalog/assets/a.png","doc":"# Hermes\n\n能帮你做这些事……","order":2,"keywords":["智能体","聊天"]}},
{"id":"plain","version":"v1","name":"Plain","summary":"没有展示内容的应用","supported":true,"installed":[],"source":{"id":"public","name":"公共目录"},"memory_mb":{"min":256},"needs_platform":""}],
"recipes":[{"id":"duo","title":"清单里的标题","summary":"清单里的配方说明","apps":[{"app":"hermes-agent"}],"source":{"id":"public","name":"公共目录"},"command":"acornfox install --recipe duo","needs_platform":"",
"show":{"name":"一套助手","tagline":"两个应用接好","industries":["office","ghost"],"doc":"说明"}}]}`

func TestCatalogShowCommand(t *testing.T) {
	h, f := varsHarness(t)
	f.catalogRaw = showCatalogRaw

	code, out, errOut := h.run("catalog", "show", "hermes-agent")
	if code != exitOK {
		t.Fatalf("exit %d %s", code, errOut)
	}
	for _, want := range []string{"Hermes 智能体（hermes-agent）：会聊天、会办事的助手", "角标 新上架", "主色 #3b5bdb",
		"封面 https://acornfox.com/catalog/assets/a.png", "搜索词 智能体、聊天", "排序 2", "# Hermes", "能帮你做这些事"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}

	_, out, _ = h.run("--json", "catalog", "show", "hermes-agent")
	m := decodeJSON(t, out)
	show, _ := m["show"].(map[string]any)
	st, _ := m["show_state"].(map[string]any)
	if m["ok"] != true || m["kind"] != "app" || m["name"] != "Hermes Agent" || m["summary"] != "清单里的一句话" || show["tagline"] != "会聊天、会办事的助手" || show["order"] != float64(2) || st["state"] != "ok" {
		t.Fatalf("json %s", out)
	}

	// An app without display content: the manifest's words, and why.
	code, out, _ = h.run("catalog", "show", "plain")
	if code != exitOK || !strings.Contains(out, "Plain（plain）：没有展示内容的应用") || !strings.Contains(out, "里没有写这一个") {
		t.Fatalf("plain: %d\n%s", code, out)
	}
	_, out, _ = h.run("--json", "catalog", "show", "plain")
	if m := decodeJSON(t, out); m["show"] != nil || m["kind"] != "app" {
		t.Fatalf("plain json %s", out)
	}

	// A solution: the industries come with their names (an unknown one keeps its id).
	_, out, _ = h.run("--json", "catalog", "show", "duo")
	m = decodeJSON(t, out)
	inds, _ := m["industries"].([]any)
	if m["kind"] != "recipe" || len(inds) != 2 || inds[0].(map[string]any)["name"] != "通用办公" || inds[1].(map[string]any)["name"] != "ghost" {
		t.Fatalf("recipe json %s", out)
	}
	code, out, _ = h.run("catalog", "show", "duo", "--recipe")
	if code != exitOK || !strings.Contains(out, "一套助手（duo）：两个应用接好") || !strings.Contains(out, "行业 通用办公、ghost") {
		t.Fatalf("recipe: %d\n%s", code, out)
	}

	// No such id: the server-side refusal shape, exit 4.
	code, out, _ = h.run("--json", "catalog", "show", "nope")
	if code != exitServerError || !strings.Contains(out, "not_found") {
		t.Fatalf("nope: %d %s", code, out)
	}
	if code, _, _ = h.run("catalog", "show"); code != exitUsage {
		t.Fatalf("no id: %d", code)
	}
	if code, out, _ = h.run("catalog", "show", "-h"); code != exitOK || !strings.Contains(out, "只读") || !strings.Contains(out, "CAT-SHOW-01") || !strings.Contains(out, "例子：") {
		t.Fatalf("help: %d %s", code, out)
	}
	if code, out, _ = h.run("catalog", "-h"); code != exitOK || !strings.Contains(out, "catalog show 应用") || !strings.Contains(out, "show-sign") {
		t.Fatalf("catalog help: %d %s", code, out)
	}

	// The show of the catalog could not be used: the manifest's words, and why.
	f.catalogRaw = `{"serial":7,"origin":"cache","sync":{"ok":true,"message":"目录已是最新","show":{"state":"failed","code":"CAT-SHOW-01","message":"CAT-SHOW-01 show.json 的签名验不过"}},
"industries":[],"home":{"solutions":[]},"assist":{"visible":false,"price_cny":0},
"apps":[{"id":"hermes-agent","version":"v1","name":"Hermes Agent","summary":"清单里的一句话","supported":true,"installed":[],"source":{"id":"public","name":"公共目录"},"memory_mb":{"min":256},"needs_platform":""}],"recipes":[]}`
	code, out, _ = h.run("catalog", "show", "hermes-agent")
	if code != exitOK || !strings.Contains(out, "Hermes Agent（hermes-agent）：清单里的一句话") || !strings.Contains(out, "没法用（CAT-SHOW-01") || !strings.Contains(out, "acornfox faults CAT-SHOW-01") {
		t.Fatalf("failed: %d\n%s", code, out)
	}
	// An older server does not say at all: still the manifest's words.
	f.catalogRaw = `{"serial":7,"origin":"cache","sync":{"ok":true,"message":"x"},"apps":[{"id":"hermes-agent","version":"v1","name":"Hermes Agent","summary":"清单里的一句话","supported":true,"installed":[],"source":{"id":"public","name":"公共目录"},"memory_mb":{"min":256},"needs_platform":""}],"recipes":[]}`
	if code, out, _ = h.run("catalog", "show", "hermes-agent"); code != exitOK || !strings.Contains(out, "清单里的一句话") {
		t.Fatalf("old server: %d\n%s", code, out)
	}
}

func TestCatalogShowSignAndVerify(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("私钥权限在 Windows 上查不了")
	}
	h, _ := varsHarness(t)
	dir := t.TempDir()
	key := filepath.Join(dir, "key.pem")
	code, ko, errOut := h.run("--json", "catalog", "keygen", "--out", key)
	if code != exitOK {
		t.Fatalf("keygen %d %s", code, errOut)
	}
	pub := decodeJSON(t, ko)["public_key"].(string)

	// A published directory: index.json at serial 5 and a show.json for it.
	out := filepath.Join(dir, "dist")
	if code, _, errOut := h.run("catalog", "build", filepath.Join("..", "catalog", "testdata", "good"), "--out", out, "--serial", "5", "--key", key, "--apply"); code != exitOK {
		t.Fatalf("build %d %s", code, errOut)
	}
	showPath := filepath.Join(out, catalog.ShowFile)
	good := `{"kind":"catalog-show","schema":1,"serial":5,"industries":[{"id":"office","name":"通用办公"}],"apps":[{"id":"hermes-agent","name":"Hermes","cover":"https://evil.example/a.png"}],"solutions":[],"home":{"solutions":[]},"assist":{"visible":false,"price_cny":0}}`
	if err := os.WriteFile(showPath, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}

	// Trial run writes nothing and says what the program will drop.
	code, so, _ := h.run("--json", "catalog", "show-sign", showPath, "--key", key)
	m := decodeJSON(t, so)
	if code != exitOK || m["dry_run"] != true || m["wrote"] != false || m["serial"] != float64(5) || len(m["warnings"].([]any)) != 1 || m["public_key"] != pub {
		t.Fatalf("dry run: %d %s", code, so)
	}
	if _, err := os.Stat(showPath + ".sig"); !os.IsNotExist(err) {
		t.Fatal("试跑写了签名")
	}
	code, so, _ = h.run("catalog", "show-sign", showPath, "--key", key, "--apply")
	if code != exitOK || !strings.Contains(so, "封面地址") || !strings.Contains(so, "写了") {
		t.Fatalf("apply: %d %s", code, so)
	}
	// Twice: the same signature, not written again.
	_, so, _ = h.run("--json", "catalog", "show-sign", showPath, "--key", key, "--apply")
	if decodeJSON(t, so)["wrote"] != false {
		t.Fatalf("repeat wrote again: %s", so)
	}
	// The whole directory verifies, show included.
	code, vo, _ := h.run("--json", "catalog", "verify", out, "--pubkey", pub)
	if code != exitOK || decodeJSON(t, vo)["show"] != "ok" {
		t.Fatalf("verify: %d %s", code, vo)
	}
	// Edited after signing: verify says CAT-SHOW-01.
	_ = os.WriteFile(showPath, []byte(strings.Replace(good, "Hermes", "Hermes2", 1)), 0o644)
	if code, vo, _ = h.run("--json", "catalog", "verify", out, "--pubkey", pub); code == exitOK || !strings.Contains(vo, "CAT-SHOW-01") {
		t.Fatalf("tampered: %d %s", code, vo)
	}

	// A serial that is not the index's is refused, nothing written.
	_ = os.WriteFile(showPath, []byte(strings.Replace(good, `"serial":5`, `"serial":6`, 1)), 0o644)
	_ = os.Remove(showPath + ".sig")
	code, so, _ = h.run("--json", "catalog", "show-sign", showPath, "--key", key, "--apply")
	if code == exitOK || !strings.Contains(so, "CAT-SHOW-02") {
		t.Fatalf("serial: %d %s", code, so)
	}
	if _, err := os.Stat(showPath + ".sig"); !os.IsNotExist(err) {
		t.Fatal("序号不对还是写了签名")
	}
	// Not a show: refused (CAT-SHOW-03).
	_ = os.WriteFile(showPath, []byte(`{"kind":"ops-overlay"}`), 0o644)
	code, so, _ = h.run("--json", "catalog", "show-sign", showPath, "--key", key, "--apply")
	if code == exitOK || !strings.Contains(so, "CAT-SHOW-03") {
		t.Fatalf("format: %d %s", code, so)
	}
	if code, _, _ := h.run("catalog", "show-sign", showPath); code != exitUsage {
		t.Fatalf("no key: %d", code)
	}
	if code, so, _ := h.run("catalog", "show-sign", "-h"); code != exitOK || !strings.Contains(so, "试跑") || !strings.Contains(so, "例子：") {
		t.Fatalf("help: %d %s", code, so)
	}
}
