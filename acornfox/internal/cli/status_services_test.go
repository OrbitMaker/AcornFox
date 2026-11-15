package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// status APP lists the services the app uses: the --json field is the
// services of GET /v1/apps/{app} as they came, the human output one line per
// need.
func TestStatusShowsServices(t *testing.T) {
	const appJSON = `{"name":"agent","desired":"running","observed_state":"running","services":[
		{"use":"llm","kind":"llm_gateway","kind_label":"大模型接口","required":true,"service":"gw","status":"provider_stopped","status_label":"提供它的应用停着"},
		{"use":"mail","kind":"smtp","kind_label":"邮箱（发信）","required":false,"service":"","status":"not_connected","status_label":"没接上"},
		{"use":"search","kind":"search","kind_label":"搜索","required":true,"service":"s1","status":"ok","status_label":"可用"}]}`
	var want client.App
	if err := json.Unmarshal([]byte(appJSON), &want); err != nil || len(want.Services) != 3 {
		t.Fatalf("decode: %v %+v", err, want.Services)
	}
	h, _ := statusHarness(t, func(string) bool { return true })
	h.api.appFn = func(context.Context, string) (client.App, error) { return want, nil }

	code, out, _ := h.run("--json", "status", "agent")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Services []map[string]any `json:"services"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Services) != 3 {
		t.Fatalf("output %q", out)
	}
	var api struct {
		Services []map[string]any `json:"services"`
	}
	_ = json.Unmarshal([]byte(appJSON), &api)
	for i := range api.Services {
		for k, v := range api.Services[i] {
			if got.Services[i][k] != v {
				t.Fatalf("services[%d].%s = %v, the server said %v", i, k, got.Services[i][k], v)
			}
		}
		if len(got.Services[i]) != len(api.Services[i]) {
			t.Fatalf("services[%d] = %v, the server said %v", i, got.Services[i], api.Services[i])
		}
	}

	code, out, _ = h.run("status", "agent")
	if code != exitOK {
		t.Fatalf("exit %d", code)
	}
	for _, w := range []string{
		"它用的服务：",
		"llm（大模型接口，必须）→ gw：提供它的应用停着（acornfox services show gw 看原因）",
		"mail（邮箱（发信），可选）：没接上",
		"search（搜索，必须）→ s1：可用",
	} {
		if !strings.Contains(out, w) {
			t.Fatalf("human output lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "s1：可用（") {
		t.Fatalf("a usable service needs no hint:\n%s", out)
	}

	// An app without services: an empty list, and no line.
	h.api.appFn = func(context.Context, string) (client.App, error) { return client.App{Name: "web"}, nil }
	_, out, _ = h.run("--json", "status", "web")
	if !strings.Contains(out, `"services":[]`) && !strings.Contains(out, `"services": []`) {
		t.Fatalf("services must be an empty list: %s", out)
	}
	if _, out, _ = h.run("status", "web"); strings.Contains(out, "它用的服务") {
		t.Fatalf("no services line expected:\n%s", out)
	}
}

// status APP of an app installed with a manifest of the former format says
// the server's legacy_note as it is (human and --json); other apps say
// nothing about it.
func TestStatusShowsLegacyNote(t *testing.T) {
	const note = "这个应用是用旧格式的清单装的：照常运行"
	h, _ := statusHarness(t, func(string) bool { return true })
	h.api.appFn = func(context.Context, string) (client.App, error) {
		return client.App{Name: "old", Desired: "running", LegacyNote: note}, nil
	}
	if code, out, _ := h.run("status", "old"); code != exitOK || !strings.Contains(out, "说明："+note) {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	_, out, _ := h.run("--json", "status", "old")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["legacy_note"] != note {
		t.Fatalf("json %s", out)
	}
	h.api.appFn = func(context.Context, string) (client.App, error) { return client.App{Name: "web"}, nil }
	if _, out, _ := h.run("status", "web"); strings.Contains(out, "说明：") {
		t.Fatalf("a note for an app of the current format:\n%s", out)
	}
	if _, out, _ := h.run("--json", "status", "web"); strings.Contains(out, "legacy_note") {
		t.Fatalf("legacy_note for an app of the current format: %s", out)
	}
}

// status APP carries the app's open_hint as apps does, and says what its
// port is: the port inside the container (kept as port, also
// container_port, with port_note), never the address it opens at.
func TestStatusOpenHintAndPort(t *testing.T) {
	const appJSON = `{"name":"agent","desired":"running","observed_state":"running","port":9119,"url":"http://127.0.0.1:18811","local_only":true,
		"open_hint":{"lan_address":"192.168.1.20","lan_url_after_expose":"http://192.168.1.20:18811","has_login":true,"can_lan":true,"recommend":"lan","command":"acornfox open agent",
		"ask":"要不要让同一局域网里的电脑都能打开 agent？","ask_yes_command":"acornfox --json app set --app agent --expose lan",
		"tunnel_command":"ssh -N -L 18811:127.0.0.1:18811 用户@服务器地址","tunnel_url":"http://127.0.0.1:18811"}}`
	var app client.App
	if err := json.Unmarshal([]byte(appJSON), &app); err != nil {
		t.Fatal(err)
	}
	h, _ := statusHarness(t, func(string) bool { return true })
	h.api.appFn = func(context.Context, string) (client.App, error) { return app, nil }
	code, out, _ := h.run("--json", "status", "agent")
	var got struct {
		Port          int            `json:"port"`
		ContainerPort int            `json:"container_port"`
		PortNote      string         `json:"port_note"`
		URL           string         `json:"url"`
		OpenHint      map[string]any `json:"open_hint"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || code != exitOK {
		t.Fatalf("%d %v %s", code, err, out)
	}
	if got.Port != 9119 || got.ContainerPort != 9119 || !strings.Contains(got.PortNote, "不是访问地址") || got.URL != "http://127.0.0.1:18811" ||
		got.OpenHint["recommend"] != "lan" || got.OpenHint["lan_url_after_expose"] != "http://192.168.1.20:18811" || got.OpenHint["lan_url"] != nil ||
		got.OpenHint["command"] != "acornfox open agent" || got.OpenHint["ask_yes_command"] != "acornfox --json app set --app agent --expose lan" ||
		got.OpenHint["tunnel_command"] != "ssh -N -L 18811:127.0.0.1:18811 用户@服务器地址" || got.OpenHint["tunnel_url"] != "http://127.0.0.1:18811" ||
		got.OpenHint["ask"] != "要不要让同一局域网里的电脑都能打开 agent？" {
		t.Fatalf("json %s", out)
	}
	_, out, _ = h.run("status", "agent")
	for _, w := range []string{"容器内端口：9119", "不是访问地址", "网址：http://127.0.0.1:18811（只在服务器本机能打开）", "怎么打开：", "acornfox open agent", "--expose lan",
		"改好以后才能用 http://192.168.1.20:18811 打开（现在还打不开）"} {
		if !strings.Contains(out, w) {
			t.Fatalf("human output lacks %q:\n%s", w, out)
		}
	}
	// An app that opens from elsewhere: no open_hint, no line about it.
	h.api.appFn = func(context.Context, string) (client.App, error) {
		return client.App{Name: "web", Port: 8080, URL: "http://203.0.113.10:18810"}, nil
	}
	if _, out, _ = h.run("--json", "status", "web"); strings.Contains(out, `"open_hint":`) {
		t.Fatalf("open_hint without a hint: %s", out)
	}
	if _, out, _ = h.run("status", "web"); strings.Contains(out, "怎么打开") || !strings.Contains(out, "容器内端口：8080") {
		t.Fatalf("human %s", out)
	}
}
