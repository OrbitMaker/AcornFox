package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// A parameter key has no dot: "x.y" is parameter y of app x of a recipe,
// kept as one key; there is no nested form any more.
func TestParseSets(t *testing.T) {
	got, plat, err := parseSets([]string{"admin_username=root", "api_key=sk-a=b", "demo-app.model=m2", "memo.admin_username=x", "platform.tz=Asia/Tokyo", "platform.mirror.pypi=https://m.example/simple"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"admin_username":      "root",
		"api_key":             "sk-a=b",
		"demo-app.model":      "m2",
		"memo.admin_username": "x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	wantPlat := map[string]string{"tz": "Asia/Tokyo", "mirror.pypi": "https://m.example/simple"}
	if !reflect.DeepEqual(plat, wantPlat) {
		t.Fatalf("platform %#v", plat)
	}
	for _, bad := range []string{"noequals", "=v", "a.b.c=v", "platform.=v", ".a=v", "a.=v", "demo-app.llm.model=m"} {
		if _, _, err := parseSets([]string{bad}); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// A finished install tells how to open a local-only app from the user's own
// computer (only for the apps this task made). Nothing is said about a
// credential's quota: that is the providing app's business.
func TestReportTaskHints(t *testing.T) {
	var task client.InstallTask
	raw := `{"id":"t1","status":"succeeded","apps":[
		{"app":"provider-app","version":"v1","reused":true,"url":"http://127.0.0.1:18810"},
		{"app":"consumer-app","version":"v2","url":"http://127.0.0.1:18811","bindings":[{"use":"model","service":"provider-app","issued":true}]}]}`
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	a := &app{out: &outputWriter{stdout: &out, stderr: &errBuf}}
	if code := a.reportTask(context.Background(), nil, task, false, nil); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	s := out.String()
	if !strings.Contains(s, "acornfox open consumer-app") || strings.Contains(s, "acornfox open provider-app") {
		t.Fatalf("open hint (only for the app this task made): %s", s)
	}
	if strings.Contains(s, "令牌") || strings.Contains(s, "设上限") {
		t.Fatalf("no note about a token's quota: %s", s)
	}
}

// An install on old data lists what it carried over; notes of a fresh
// install are not called carried over.
func TestReportTaskCarried(t *testing.T) {
	var task client.InstallTask
	raw := `{"id":"t2","status":"succeeded","apps":[
		{"app":"consumer-app","version":"v2","url":"http://127.0.0.1:18811","reused_volumes":["af-consumer-app-1"],
		 "carried":["沿用以前改过的平台变量：platform.tz=Asia/Tokyo","consumer-app 的“model” 接着用以前 provider-app 发给它的凭据（检查过还能用）"]},
		{"app":"tzapp","version":"v1","url":"http://127.0.0.1:18812","carried":["CAT-INST-35 tzapp 以前接的服务 old（“mail”）这次没有接上，已经断开"]}]}`
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	a := &app{out: &outputWriter{stdout: &out, stderr: &errBuf}}
	if code := a.reportTask(context.Background(), nil, task, false, nil); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	s := out.String()
	for _, want := range []string{"consumer-app 接上了旧数据，沿用的设置：", "  - 沿用以前改过的平台变量：platform.tz=Asia/Tokyo", "  - consumer-app 的“model” 接着用以前 provider-app 发给它的凭据（检查过还能用）", "tzapp 另外：", "  - CAT-INST-35 tzapp 以前接的服务 old（“mail”）这次没有接上，已经断开"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "tzapp 接上了旧数据") {
		t.Fatalf("a fresh install called carried over:\n%s", s)
	}
}

// A finished install carries each app's open_hint (the same as apps gives)
// and what the provider of a service it was plugged into wrote for the user
// (apps[].tell): --json passes both through and puts the sentence in
// must_tell, next says to go by open_hint; the human output says how to
// open it the same way as apps does, and lists the sentence.
func TestReportTaskOpenHintAndTell(t *testing.T) {
	const ask = "在提供方里加好上游后，填一个要用的模型名"
	raw := `{"id":"t3","status":"succeeded","created_at":"2026-11-13T00:00:00Z","finished_at":"2026-11-13T00:00:18Z","apps":[
		{"app":"consumer-app","version":"v2","url":"http://127.0.0.1:18811","tell":["` + ask + `"],
		 "open_hint":{"lan_address":"192.168.1.20","lan_url_after_expose":"http://192.168.1.20:18811","has_login":true,"can_lan":true,"recommend":"lan","command":"acornfox open consumer-app",
		  "ask":"要不要让同一局域网里的电脑都能打开 consumer-app？","ask_yes_command":"acornfox --json app set --app consumer-app --expose lan",
		  "tunnel_command":"ssh -N -L 18811:127.0.0.1:18811 用户@服务器地址","tunnel_url":"http://127.0.0.1:18811"}}],
		"first_login":[{"app":"consumer-app","url":"http://127.0.0.1:18811","url_note":"这个地址只有服务器本机能打开；从你自己电脑打开的办法见“怎么打开”","fields":[{"label":"账号","value":"admin"},{"label":"密码","value":"******","secret":true}]}]}`
	var task client.InstallTask
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatal(err)
	}
	task.Raw = json.RawMessage(raw)
	if task.Apps[0].OpenHint == nil || task.Apps[0].OpenHint.Recommend != "lan" || len(task.Apps[0].Tell) != 1 {
		t.Fatalf("decode %+v", task.Apps[0])
	}
	var out, errBuf bytes.Buffer
	a := &app{out: &outputWriter{stdout: &out, stderr: &errBuf, json: true}}
	if code := a.reportTask(context.Background(), nil, task, false, nil); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Next               string   `json:"next"`
		ShowSecretsCommand string   `json:"show_secrets_command"`
		MustTell           []string `json:"must_tell"`
		Apps               []struct {
			OpenHint map[string]any `json:"open_hint"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	if len(got.MustTell) != 1 || got.MustTell[0] != ask || len(got.Apps) != 1 || got.Apps[0].OpenHint["recommend"] != "lan" ||
		got.Apps[0].OpenHint["command"] != "acornfox open consumer-app" || got.Apps[0].OpenHint["ask_yes_command"] == nil ||
		!strings.Contains(got.Next, "open_hint") || got.ShowSecretsCommand != "acornfox install status t3 --show-secrets" ||
		strings.Contains(got.Next, "--show-secrets") {
		t.Fatalf("json %s", out.String())
	}

	out.Reset()
	a = &app{out: &outputWriter{stdout: &out, stderr: &errBuf}}
	a.reportTask(context.Background(), nil, task, false, nil)
	s := out.String()
	for _, want := range []string{"怎么打开：consumer-app 只在服务器本机能打开", "acornfox open consumer-app", "--expose lan", "改好以后才能用 http://192.168.1.20:18811 打开（现在还打不开）",
		"ssh -N -L 18811:127.0.0.1:18811", "地址：http://127.0.0.1:18811（这个地址只有服务器本机能打开", "在服务器上运行：\n  acornfox install status t3 --show-secrets\n",
		"接上的服务那边还要你做的：", "  - " + ask} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

// The plan shows, under a need that is plugged in, what the provider wrote
// for the user.
func TestPlanShowsTell(t *testing.T) {
	h, f := installHarness(t)
	f.planRaw = `{"can_install":true,"say":"可以安装 demo。需要的服务：大模型接口 接 gw。","steps":[{"app":"demo","name":"demo","action":"install","because":""}],
		"apps":[{"id":"demo","name":"demo","version":"v1","needs":[{"use":"llm","kind":"llm_gateway","kind_label":"大模型接口","required":true,"status":"matched","service":"gw",
		"pending_from":"","candidates":[],"without":"","how":[],"tell":["在 gw 里加好上游后，填一个要用的模型名"]}],"params_missing":[],"provides":[]}],"problems":[]}`
	code, out, errOut := h.run("install", "demo", "--plan")
	if code != exitOK || !strings.Contains(out, "大模型接口（llm）：已接上，用服务 gw") || !strings.Contains(out, "提供它的应用要你做的：在 gw 里加好上游后，填一个要用的模型名") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	code, out, _ = h.run("--json", "install", "demo", "--plan")
	if code != exitOK || !strings.Contains(out, `"tell"`) {
		t.Fatalf("json %s", out)
	}
}

// install list --plan: the server answers 200 with CAT-INST-01 among the
// problems (not an error); the hint on how install tasks are looked at goes
// before that problem's hint, in --json and in the human output. Another
// name keeps the server's hint.
func TestPlanTaskWordHint(t *testing.T) {
	h, f := installHarness(t)
	f.planRaw = `{"can_install":false,"say":"装不了。","steps":[],"apps":[],"problems":[{"code":"CAT-INST-01","message":"目录里没有应用 list","hint":"acornfox catalog list 看有哪些应用"}]}`
	code, out, errOut := h.run("--json", "install", "list", "--plan")
	if code != exitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	probs, _ := decodeJSON(t, out)["problems"].([]any)
	if len(probs) != 1 || probs[0].(map[string]any)["hint"] != taskWordHint+"；acornfox catalog list 看有哪些应用" {
		t.Fatalf("json %s", out)
	}
	code, out, _ = h.run("install", "list", "--plan")
	if code != exitOK || !strings.Contains(out, "装不了：CAT-INST-01 目录里没有应用 list") || !strings.Contains(out, "acornfox install status 任务号") {
		t.Fatalf("human %s", out)
	}
	f.planRaw = `{"can_install":false,"say":"装不了。","steps":[],"apps":[],"problems":[{"code":"CAT-INST-01","message":"目录里没有应用 nope","hint":"原来的提示"}]}`
	_, out, _ = h.run("--json", "install", "nope", "--plan")
	if probs, _ := decodeJSON(t, out)["problems"].([]any); len(probs) != 1 || probs[0].(map[string]any)["hint"] != "原来的提示" {
		t.Fatalf("other app: %s", out)
	}
}
