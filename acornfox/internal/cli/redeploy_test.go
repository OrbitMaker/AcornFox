package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// redeployHarness answers the redeploy route with a fixed ID and the
// deployment route with the given statuses in turn (the last one repeats).
func redeployHarness(t *testing.T, statuses ...client.Deployment) *harness {
	t.Helper()
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	h.api.redeployFn = func(context.Context, string) (string, error) { return "0123456789ab", nil }
	calls := 0
	h.api.deploymentFn = func(_ context.Context, id string, _ int64) (client.Deployment, []client.Event, error) {
		d := statuses[min(calls, len(statuses)-1)]
		calls++
		d.ID = id
		return d, []client.Event{{ID: int64(calls), Stage: d.Status, Message: "进行到 " + d.Status}}, nil
	}
	return h
}

func TestRedeployWaitsLikeDeploy(t *testing.T) {
	h := redeployHarness(t,
		client.Deployment{App: "web", Seq: 4, Status: "starting"},
		client.Deployment{App: "web", Seq: 4, Status: "live", Warnings: []client.Diagnosis{{Stage: "data", Code: "unpersisted_database", Message: "数据写在容器里"}}},
	)
	code, out, errOut := h.run("redeploy", "web")
	if code != exitOK {
		t.Fatalf("code=%d out=%s err=%s", code, out, errOut)
	}
	for _, want := range []string{"已触发重新部署，部署 ID: 0123456789ab", "[starting] 进行到 starting", "警告：数据写在容器里", "部署成功：http://192.168.1.10:12345"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "可使用 acornfox status") {
		t.Fatalf("waiting redeploy should not tell to poll status:\n%s", out)
	}

	code, out, _ = h.run("--json", "redeploy", "web")
	m := decodeJSON(t, out)
	if code != exitOK || m["ok"] != true || m["app"] != "web" || m["deployment_id"] != "0123456789ab" || m["version"] != 4.0 ||
		m["status"] != "live" || m["url"] != "http://192.168.1.10:12345" {
		t.Fatalf("code=%d json=%v", code, m)
	}
	for _, key := range []string{"lan_url", "warnings", "seconds"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("redeploy json missing %q: %v", key, m)
		}
	}
	if ws := m["warnings"].([]any); len(ws) != 1 || ws[0].(map[string]any)["code"] != "unpersisted_database" {
		t.Fatalf("warnings = %v", m["warnings"])
	}
}

func TestRedeployFailureExit1(t *testing.T) {
	h := redeployHarness(t, client.Deployment{App: "web", Seq: 4, Status: "failed",
		Diagnosis: &client.Diagnosis{Stage: "health", Code: "container_exited", Message: "容器启动后退出（退出码 1）"}})
	code, out, _ := h.run("--json", "--app", "web", "redeploy")
	if code != exitOpFailed {
		t.Fatalf("code=%d out=%s", code, out)
	}
	m := decodeJSON(t, out)
	if m["ok"] != false || m["diagnosis"].(map[string]any)["code"] != "container_exited" {
		t.Fatalf("json = %v", m)
	}
	code, _, errOut := h.run("--app", "web", "redeploy")
	if code != exitOpFailed || !strings.Contains(errOut, "容器启动后退出（退出码 1）") {
		t.Fatalf("human: code=%d err=%s", code, errOut)
	}

	h = redeployHarness(t, client.Deployment{App: "web", Seq: 4, Status: "superseded"})
	if code, out, _ := h.run("--json", "--app", "web", "redeploy"); code != exitOpFailed || decodeJSON(t, out)["diagnosis"].(map[string]any)["code"] != "superseded" {
		t.Fatalf("superseded: code=%d out=%s", code, out)
	}
}

// --no-wait keeps the output redeploy had before it waited, byte for byte.
func TestRedeployNoWaitKeepsOldOutput(t *testing.T) {
	h := redeployHarness(t, client.Deployment{Status: "building"})
	polled := false
	h.api.deploymentFn = func(context.Context, string, int64) (client.Deployment, []client.Event, error) {
		polled = true
		return client.Deployment{}, nil, nil
	}
	code, out, errOut := h.run("redeploy", "web", "--no-wait")
	if code != exitOK || out != "已触发重新部署，部署 ID: 0123456789ab\n可使用 acornfox status 0123456789ab 查看进度和诊断\n" || errOut != "" {
		t.Fatalf("human: code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, _ = h.run("--json", "redeploy", "--no-wait", "--app", "web")
	if code != exitOK || strings.TrimSpace(out) != `{"deployment_id":"0123456789ab","ok":true}` {
		t.Fatalf("json: code=%d out=%q", code, out)
	}
	if polled {
		t.Fatal("--no-wait polled the deployment")
	}
}

func TestRedeployTimeout(t *testing.T) {
	h := redeployHarness(t, client.Deployment{App: "web", Seq: 4, Status: "building"})
	code, out, _ := h.run("--json", "redeploy", "web", "--timeout", "1ms")
	if code != exitOpFailed {
		t.Fatalf("code=%d out=%s", code, out)
	}
	d := decodeJSON(t, out)["diagnosis"].(map[string]any)
	if d["code"] != "timeout" || !strings.Contains(d["hint"].(string), "acornfox status 0123456789ab") {
		t.Fatalf("diagnosis = %v", d)
	}
	if code, _, _ := h.run("redeploy", "web", "--timeout", "x"); code != exitUsage {
		t.Fatalf("bad timeout: code=%d", code)
	}
}
