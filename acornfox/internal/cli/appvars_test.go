package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// fakeVarsAPI adds the app-variables call and a catalog with entries this
// server is too old for.
type fakeVarsAPI struct {
	*fakePrivateAPI
	gotApp     string
	gotReveal  bool
	raw        string
	err        error
	installErr error
}

const varsAnswer = `{"app":"new-api","from_catalog":true,"catalog_id":"new-api","version":"v1.0.0-rc.48","source":{"id":"public","name":"公共目录"},
"manifest":{"saved":true,"source":"backfill","source_label":"补存的（装的时候还不存清单，后来从本机的目录里找到的）","sha256":"ab"},
"revealed":false,"can_change":false,"vars":[
{"name":"params.admin_password","layer":"custom","layer_label":"自定义变量","class":"generated","class_label":"平台生成","label":"管理员密码","value":"******4567","secret":true,"masked":true,"origin":"install_time","origin_label":"装时的值（那时还不记是谁给的）","editable":"never","editable_label":"不能改"},
{"name":"platform.region","layer":"system","layer_label":"系统变量","class":"system","class_label":"系统给","label":"网络区域（cn 或 global）","value":"cn","secret":false,"masked":false,"origin":"platform","origin_label":"平台算的","editable":"never","editable_label":"不能改","used":false},
{"name":"platform.tz","layer":"system","layer_label":"系统变量","class":"system","class_label":"系统给","label":"时区","value":"Asia/Tokyo","secret":false,"masked":false,"origin":"platform_user","origin_label":"用户改的","editable":"install_only","editable_label":"只能在安装时用 --set platform.名字=值 改","used":true}],
"notes":["改设置的功能还没有：这里只能看，不能改。"]}`

func (f *fakeVarsAPI) AppVars(_ context.Context, app string, reveal bool) (client.AppVars, error) {
	f.gotApp, f.gotReveal = app, reveal
	if f.err != nil {
		return client.AppVars{}, f.err
	}
	raw := json.RawMessage(f.raw)
	var v client.AppVars
	_ = json.Unmarshal(raw, &v)
	v.Raw = raw
	return v, nil
}

func (f *fakeVarsAPI) Catalog(context.Context) (client.CatalogList, error) {
	raw := json.RawMessage(`{"serial":7,"origin":"cache","sync":{"ok":true,"message":"目录已是最新"},
"apps":[{"id":"new-api","version":"v1","name":"New API","summary":"网关","supported":true,"installed":[],"source":{"id":"public","name":"公共目录"},"memory_mb":{"min":256},"needs_platform":""},
{"id":"dify","version":"v2","name":"Dify","summary":"应用平台","supported":true,"installed":[],"source":{"id":"public","name":"公共目录"},"memory_mb":{"min":2048},"needs_platform":"0.6.0"}],
"recipes":[{"id":"dify-stack","title":"Dify 全家桶","summary":"x","apps":[{"app":"dify"}],"source":{"id":"public","name":"公共目录"},"command":"acornfox install --recipe dify-stack","needs_platform":"0.6.0"}]}`)
	var v client.CatalogList
	_ = json.Unmarshal(raw, &v)
	v.Raw = raw
	return v, nil
}

func (f *fakeVarsAPI) CatalogApp(context.Context, string) (client.CatalogApp, error) {
	raw := json.RawMessage(`{"id":"dify","version":"v2","name":"Dify","summary":"应用平台","installed":[],"source":{"id":"public","name":"公共目录"},"command":"acornfox install dify","needs_platform":"0.6.0","params":[],"platform":[]}`)
	var v client.CatalogApp
	_ = json.Unmarshal(raw, &v)
	v.Raw = raw
	return v, nil
}

func (f *fakeVarsAPI) Install(context.Context, client.InstallRequest) (client.InstallTask, error) {
	return client.InstallTask{}, f.installErr
}

func varsHarness(t *testing.T) (*harness, *fakeVarsAPI) {
	h, p := privateHarness(t)
	f := &fakeVarsAPI{fakePrivateAPI: p, raw: varsAnswer}
	h.connect = func(context.Context, client.Target) (client.API, error) { return f, nil }
	return h, f
}

func TestAppsVarsCommand(t *testing.T) {
	h, f := varsHarness(t)
	code, out, errOut := h.run("apps", "vars", "new-api")
	if code != exitOK || f.gotApp != "new-api" || f.gotReveal {
		t.Fatalf("exit %d stderr %s", code, errOut)
	}
	for _, want := range []string{"从应用中心装的 new-api v1.0.0-rc.48", "清单：补存的", "自定义变量：", "系统变量：",
		"params.admin_password（管理员密码） = ******4567", "[平台生成] 装时的值", "platform.tz（时区） = Asia/Tokyo", "用户改的",
		"这个应用的清单没用到它", "改设置的功能还没有"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// --json: the server's answer as is, plus ok.
	code, out, _ = h.run("--json", "apps", "vars", "new-api", "--reveal")
	m := decodeJSON(t, out)
	if code != exitOK || !f.gotReveal || m["ok"] != true || m["can_change"] != false || len(m["vars"].([]any)) != 3 ||
		m["manifest"].(map[string]any)["source"] != "backfill" {
		t.Fatalf("exit %d out %s", code, out)
	}
	// The app may come from the global --app; two different names are refused.
	if code, _, _ := h.run("--app", "new-api", "apps", "vars"); code != exitOK || f.gotApp != "new-api" {
		t.Fatalf("--app: exit %d app %q", code, f.gotApp)
	}
	if code, _, _ := h.run("--app", "other", "apps", "vars", "new-api"); code != exitUsage {
		t.Fatalf("two names: exit %d", code)
	}
	if code, _, _ := h.run("apps", "vars"); code != exitUsage {
		t.Fatalf("no name: exit %d", code)
	}
	if code, out, _ := h.run("apps", "vars", "-h"); code != exitOK || !strings.Contains(out, "只读") || !strings.Contains(out, "acornfox apps vars new-api") || !strings.Contains(out, "--reveal") {
		t.Fatalf("help %d %s", code, out)
	}
	if code, out, _ := h.run("apps", "-h"); code != exitOK || !strings.Contains(out, "apps vars 应用") {
		t.Fatalf("apps help %d %s", code, out)
	}
	if code, _, _ := h.run("apps", "bogus"); code != exitUsage {
		t.Fatalf("apps bogus: exit %d", code)
	}

	// Not installed from the app center: the note, exit 0.
	f.raw = `{"app":"web","from_catalog":false,"catalog_id":"","version":"","source":{"id":"","name":""},"manifest":{"saved":false,"source":"","source_label":""},"revealed":false,"can_change":false,"vars":[],"notes":["web 不是从应用中心装的，没有应用中心的变量。它的环境变量用 acornfox env list --app web 看"]}`
	code, out, _ = h.run("apps", "vars", "web")
	if code != exitOK || !strings.Contains(out, "不是从应用中心装的") {
		t.Fatalf("plain app: exit %d out %s", code, out)
	}
	// No such app: the server's refusal with its code.
	f.err = &client.Error{Status: http.StatusNotFound, Diag: client.Diagnosis{Stage: "server", Code: "CAT-INST-22", Message: "没有叫 nope 的应用"}}
	code, out, _ = h.run("--json", "apps", "vars", "nope")
	if code != exitServerError || !strings.Contains(out, "CAT-INST-22") {
		t.Fatalf("missing app: exit %d out %s", code, out)
	}
}

// catalog list and info say "要先升级到 x.y.z" for entries this server is
// too old for; --json carries needs_platform; a refused install carries it in
// the diagnosis.
func TestCatalogShowsUpgradeFirst(t *testing.T) {
	h, f := varsHarness(t)
	code, out, _ := h.run("catalog", "list")
	if code != exitOK || strings.Count(out, "要先升级到 0.6.0") != 2 {
		t.Fatalf("list: exit %d\n%s", code, out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "new-api") && strings.Contains(line, "要先升级") {
			t.Fatalf("an installable app is marked: %s", line)
		}
	}
	code, out, _ = h.run("--json", "catalog", "list")
	m := decodeJSON(t, out)
	apps := m["apps"].([]any)
	if code != exitOK || apps[0].(map[string]any)["needs_platform"] != "" || apps[1].(map[string]any)["needs_platform"] != "0.6.0" ||
		m["recipes"].([]any)[0].(map[string]any)["needs_platform"] != "0.6.0" {
		t.Fatalf("list json: %s", out)
	}
	code, out, _ = h.run("catalog", "info", "dify")
	if code != exitOK || !strings.Contains(out, "要先升级到 0.6.0") {
		t.Fatalf("info: exit %d\n%s", code, out)
	}
	if _, out, _ = h.run("--json", "catalog", "info", "dify"); decodeJSON(t, out)["needs_platform"] != "0.6.0" {
		t.Fatalf("info json: %s", out)
	}

	f.installErr = &client.Error{Status: http.StatusConflict, Diag: client.Diagnosis{Stage: "server", Code: "CAT-INST-08",
		Message: "dify 要求 AcornFox 0.6.0 以上，这台是 0.5.1，请先升级 AcornFox 再装", Hint: "先升级 AcornFox 再装", NeedsPlatform: "0.6.0"}}
	code, out, _ = h.run("--json", "install", "dify", "--no-wait")
	d, _ := decodeJSON(t, out)["diagnosis"].(map[string]any)
	if code != exitServerError || d["code"] != "CAT-INST-08" || d["needs_platform"] != "0.6.0" {
		t.Fatalf("install: exit %d out %s", code, out)
	}
	// A fault without it has no such key.
	f.installErr = &client.Error{Status: http.StatusConflict, Diag: client.Diagnosis{Stage: "server", Code: "CAT-INST-29", Message: "这份目录有问题"}}
	_, out, _ = h.run("--json", "install", "dify", "--no-wait")
	if d, _ := decodeJSON(t, out)["diagnosis"].(map[string]any); d["code"] != "CAT-INST-29" || d["needs_platform"] != nil {
		t.Fatalf("install: out %s", out)
	}

	// Without --json the two kinds of CAT-INST-29 can be told apart too: the
	// one an upgrade cures says which version, the other never says upgrade.
	_, _, errOut := h.run("install", "dify", "--no-wait")
	if !strings.Contains(errOut, "这份目录有问题") || strings.Contains(errOut, "要先升级到") {
		t.Fatalf("目录有问题的那种不该让人升级：%s", errOut)
	}
	f.installErr = &client.Error{Status: http.StatusConflict, Diag: client.Diagnosis{Stage: "server", Code: "CAT-INST-29",
		Message: "dify 1.0 的清单用了这个版本的 AcornFox 不认识的写法", Hint: "先升级到 0.6.0：这台 AcornFox 版本太旧", NeedsPlatform: "0.6.0"}}
	code, out, errOut = h.run("install", "dify", "--no-wait")
	if code != exitServerError || !strings.Contains(errOut, "要先升级到 0.6.0") || !strings.Contains(errOut, "建议：先升级到 0.6.0") {
		t.Fatalf("要升级的那种，不带 --json 也要说升到哪个版本：exit %d stderr %s", code, errOut)
	}
	// CAT-INST-08 gets the same line.
	f.installErr = &client.Error{Status: http.StatusConflict, Diag: client.Diagnosis{Stage: "server", Code: "CAT-INST-08", Message: "dify 要求 AcornFox 0.6.0 以上", NeedsPlatform: "0.6.0"}}
	if _, _, errOut = h.run("install", "dify", "--no-wait"); !strings.Contains(errOut, "要先升级到 0.6.0") {
		t.Fatalf("CAT-INST-08：%s", errOut)
	}
	// --json keeps its shape: the same keys as before, nothing renamed.
	_, out, _ = h.run("--json", "install", "dify", "--no-wait")
	d, _ = decodeJSON(t, out)["diagnosis"].(map[string]any)
	for _, k := range []string{"stage", "code", "message", "hint", "log_excerpt", "needs_platform"} {
		if _, ok := d[k]; !ok {
			t.Fatalf("--json 的 diagnosis 少了 %s：%s", k, out)
		}
	}
}
