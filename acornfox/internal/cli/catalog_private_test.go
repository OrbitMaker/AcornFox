package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// fakePrivateAPI is the fake server plus the app center and private catalog
// calls the connect/sources commands use.
type fakePrivateAPI struct {
	*fakeAPI
	gotCode string
	err     error
}

func (f *fakePrivateAPI) Catalog(context.Context) (client.CatalogList, error) {
	return client.CatalogList{}, nil
}
func (f *fakePrivateAPI) CatalogApp(context.Context, string) (client.CatalogApp, error) {
	return client.CatalogApp{}, nil
}
func (f *fakePrivateAPI) CatalogSync(context.Context) (client.CatalogSync, error) {
	return client.CatalogSync{}, nil
}
func (f *fakePrivateAPI) Install(context.Context, client.InstallRequest) (client.InstallTask, error) {
	return client.InstallTask{}, nil
}
func (f *fakePrivateAPI) InstallTask(context.Context, string) (client.InstallTask, error) {
	return client.InstallTask{}, nil
}
func (f *fakePrivateAPI) FirstLogin(context.Context, string, bool) (client.FirstLogin, error) {
	return client.FirstLogin{}, nil
}
func (f *fakePrivateAPI) CatalogConnect(ctx context.Context, code string) (client.CatalogConnected, error) {
	f.gotCode = code
	if f.err != nil {
		return client.CatalogConnected{}, f.err
	}
	raw := json.RawMessage(`{"catalog":{"id":"cat-demo","name":"演示"},"again":false,"sync":{"ok":true,"message":"私有目录 1 个"}}`)
	var v client.CatalogConnected
	_ = json.Unmarshal(raw, &v)
	v.Raw = raw
	return v, nil
}
func (f *fakePrivateAPI) CatalogSources(context.Context) (client.CatalogSources, error) {
	raw := json.RawMessage(`{"sources":[{"id":"public","kind":"public","name":"公共目录","serial":1,"apps":2,"recipes":1,"disconnected":false},
{"id":"cat-demo","kind":"private","name":"演示","serial":0,"apps":0,"recipes":0,"code":"CAT-SYNC-11","message":"已断开","disconnected":true,"disconnected_at":"2026-11-06T10:00:00Z"}]}`)
	var v client.CatalogSources
	_ = json.Unmarshal(raw, &v)
	v.Raw = raw
	return v, nil
}
func (f *fakePrivateAPI) CatalogAppFrom(context.Context, string, string) (client.CatalogApp, error) {
	return client.CatalogApp{}, nil
}

func privateHarness(t *testing.T) (*harness, *fakePrivateAPI) {
	h := newHarness(t)
	h.addTarget("prod", "https://203.0.113.10:8443")
	f := &fakePrivateAPI{fakeAPI: h.api}
	h.connect = func(context.Context, client.Target) (client.API, error) { return f, nil }
	return h, f
}

func TestCatalogConnectCommand(t *testing.T) {
	h, f := privateHarness(t)
	code, out, errOut := h.run("--json", "catalog", "connect", "AFXC-AAAA-BBBB-CCCC-DDDD")
	if code != exitOK || f.gotCode != "AFXC-AAAA-BBBB-CCCC-DDDD" {
		t.Fatalf("exit %d stderr %s", code, errOut)
	}
	m := decodeJSON(t, out)
	if m["ok"] != true || m["catalog"].(map[string]any)["id"] != "cat-demo" {
		t.Fatalf("out %s", out)
	}
	// A refused code: exit 4 with the CAT-CONN code; the code itself is not echoed.
	f.err = &client.Error{Status: http.StatusNotFound, Diag: client.Diagnosis{Stage: "server", Code: "CAT-CONN-03",
		Message: "连接码 AFXC-AAAA-BBBB-CCCC-DDDD 不存在（GW-CAT-01）", Hint: "核对连接码"}}
	code, out, _ = h.run("--json", "catalog", "connect", "AFXC-AAAA-BBBB-CCCC-DDDD")
	if code != exitServerError || !strings.Contains(out, "CAT-CONN-03") || strings.Contains(out, "AAAA-BBBB-CCCC-DDDD") {
		t.Fatalf("exit %d out %s", code, out)
	}
	// Too many tries: the gateway code and the seconds to wait are in --json.
	f.err = &client.Error{Status: http.StatusTooManyRequests, Diag: client.Diagnosis{Stage: "server", Code: "CAT-CONN-06",
		Message: "连接码试得太频繁，120 秒后再试（GW-CAT-04）", GatewayCode: "GW-CAT-04", RetryAfter: 120}}
	_, out, _ = h.run("--json", "catalog", "connect", "AFXC-AAAA-BBBB-CCCC-DDDD")
	if d, _ := decodeJSON(t, out)["diagnosis"].(map[string]any); d["gateway_code"] != "GW-CAT-04" || d["retry_after"] != float64(120) {
		t.Fatalf("out %s", out)
	}
	if code, _, _ := h.run("catalog", "connect"); code != exitUsage {
		t.Fatalf("没给码 exit %d", code)
	}
	if code, out, _ := h.run("catalog", "connect", "-h"); code != exitOK || !strings.Contains(out, "一个码只给一台机器用") {
		t.Fatalf("帮助 %d %s", code, out)
	}
}

func TestCatalogSourcesCommand(t *testing.T) {
	h, _ := privateHarness(t)
	code, out, _ := h.run("--json", "catalog", "sources")
	m := decodeJSON(t, out)
	if code != exitOK || m["ok"] != true || len(m["sources"].([]any)) != 2 {
		t.Fatalf("exit %d out %s", code, out)
	}
	code, out, _ = h.run("catalog", "sources")
	if code != exitOK || !strings.Contains(out, "已断开") || !strings.Contains(out, "CAT-SYNC-11") || !strings.Contains(out, "公共目录") {
		t.Fatalf("exit %d out %s", code, out)
	}
}

func (f *fakePrivateAPI) PlatformVars(context.Context) (client.PlatformVars, error) {
	raw := json.RawMessage(`{"vars":[{"name":"tz","label":"时区","standalone":"本机时区","cloud":"账号时区","user_can_set":true,"since":"0.5.0","value":"Asia/Tokyo","available":true},
{"name":"llm.api_key","label":"平台为这个应用发的 Key","standalone":"即将开放","cloud":"每个实例一把","user_can_set":false,"secret":true,"issued":true,"via_param":true,"available":false,"note":"平台 Key 即将开放"}]}`)
	var v client.PlatformVars
	_ = json.Unmarshal(raw, &v)
	v.Raw = raw
	return v, nil
}

func TestCatalogPlatformVarsCommand(t *testing.T) {
	h, _ := privateHarness(t)
	code, out, _ := h.run("--json", "catalog", "platform-vars")
	m := decodeJSON(t, out)
	if code != exitOK || m["ok"] != true || len(m["vars"].([]any)) != 2 {
		t.Fatalf("exit %d out %s", code, out)
	}
	if m["vars"].([]any)[0].(map[string]any)["since"] != "0.5.0" {
		t.Fatalf("--json 要原样带出 since：%s", out)
	}
	code, out, _ = h.run("catalog", "platform-vars")
	if !strings.Contains(out, "AcornFox 0.5.0 起有") {
		t.Fatalf("要显示从哪个版本起有：%s", out)
	}
	if code != exitOK || !strings.Contains(out, "platform.tz  时区：Asia/Tokyo") || !strings.Contains(out, "--set platform.tz=") ||
		!strings.Contains(out, "暂无：平台 Key 即将开放") || !strings.Contains(out, "不能改") {
		t.Fatalf("exit %d out %s", code, out)
	}
}
