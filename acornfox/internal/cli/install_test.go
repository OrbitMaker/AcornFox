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

func TestParseSets(t *testing.T) {
	got, plat, err := parseSets([]string{"admin_username=root", "llm.model=qwen", "llm.api_key=sk-a=b", "hermes-agent.llm.model=m2", "new-api.admin_username=x", "platform.tz=Asia/Tokyo", "platform.mirror.pypi=https://m.example/simple"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"admin_username":         "root",
		"llm":                    map[string]any{"model": "qwen", "api_key": "sk-a=b"},
		"hermes-agent.llm":       map[string]any{"model": "m2"},
		"new-api.admin_username": "x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	wantPlat := map[string]string{"tz": "Asia/Tokyo", "mirror.pypi": "https://m.example/simple"}
	if !reflect.DeepEqual(plat, wantPlat) {
		t.Fatalf("platform %#v", plat)
	}
	for _, bad := range []string{"noequals", "=v", "a.b.c=v", "platform.=v"} {
		if _, _, err := parseSets([]string{bad}); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// A finished install tells how to open a local-only app from the user's own
// computer, and that an app linked to the local New API uses a token with no
// quota.
func TestReportTaskHints(t *testing.T) {
	var task client.InstallTask
	raw := `{"id":"t1","status":"succeeded","apps":[
		{"app":"new-api","version":"v1","reused":true,"url":"http://127.0.0.1:18810"},
		{"app":"hermes-agent","version":"v0.23.0","url":"http://127.0.0.1:18811","gateway_token":{"provider":"new-api","id":3}}]}`
	if err := json.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	a := &app{out: &outputWriter{stdout: &out, stderr: &errBuf}}
	if code := a.reportTask(context.Background(), nil, task, false); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	s := out.String()
	if !strings.Contains(s, "acornfox open hermes-agent") || strings.Contains(s, "acornfox open new-api") {
		t.Fatalf("open hint (only for the app this task made): %s", s)
	}
	if !strings.Contains(s, "hermes-agent 用的是 new-api 里一把不限额的令牌") || !strings.Contains(s, "设上限") {
		t.Fatalf("token note: %s", s)
	}
}
