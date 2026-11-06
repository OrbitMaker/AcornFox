package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

func intp(v int) *int { return &v }

// The positional app wins over the directory's app and must agree with --app.
func TestRollbackPositionalAppAndConflicts(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	if err := saveProject(h.workDir, &projectFile{Target: "dev", App: "notes"}); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := h.run("--json", "rollback", "web", "--to", "5")
	if code != exitOK || h.api.lastRollbackApp != "web" || h.api.lastRollback.ToSeq != 5 || h.api.lastRollback.CancelPending {
		t.Fatalf("code=%d app=%s opts=%+v out=%s err=%s", code, h.api.lastRollbackApp, h.api.lastRollback, out, errOut)
	}
	if m := decodeJSON(t, out); m["based_on_version"] != 5.0 || m["version"] != 2.0 {
		t.Fatalf("json = %v", m)
	}
	if code, _, _ := h.run("rollback", "web", "--app", "web"); code != exitOK || h.api.lastRollbackApp != "web" {
		t.Fatalf("same app twice: code=%d", code)
	}
	calls := h.api.rollbackCalls
	for _, args := range [][]string{
		{"rollback", "web", "--app", "shop"},
		{"rollback", "web", "shop"},
		{"rollback", "--to", "0"},
		{"rollback", "--to", "-1"},
		{"rollback", "--to", "x"},
		{"redeploy", "web", "shop"},
		{"redeploy", "web", "--app", "shop"},
	} {
		if code, _, _ := h.run(args...); code != exitUsage {
			t.Fatalf("%v: code=%d, want usage", args, code)
		}
	}
	if h.api.rollbackCalls != calls {
		t.Fatal("a usage error reached the server")
	}

	var redeployed string
	h.api.redeployFn = func(_ context.Context, app string) (string, error) { redeployed = app; return "0123456789ab", nil }
	if code, _, _ := h.run("redeploy", "web"); code != exitOK || redeployed != "web" {
		t.Fatalf("redeploy web: code=%d app=%s", code, redeployed)
	}
}

func TestRollbackWaitsAndReportsVersions(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	h.api.rollbackFn = func(_ context.Context, app string, opts client.RollbackOptions) (client.RollbackResult, error) {
		return client.RollbackResult{
			Deployment: client.Deployment{ID: "0123456789ab", App: app, Seq: 9, Status: "queued", BasedOnSeq: 7},
			Created:    true, TargetSeq: 7, SupersededSeq: 8,
		}, nil
	}
	h.api.deploymentFn = func(_ context.Context, id string, after int64) (client.Deployment, []client.Event, error) {
		return client.Deployment{ID: id, App: "web", Seq: 9, Status: "live", BasedOnSeq: 7},
			[]client.Event{{ID: 1, Stage: "health", Message: "健康检查通过"}}, nil
	}
	code, out, errOut := h.run("--app", "web", "rollback", "--to", "7", "--cancel-pending")
	if code != exitOK || !h.api.lastRollback.CancelPending {
		t.Fatalf("code=%d out=%s err=%s", code, out, errOut)
	}
	for _, want := range []string{"回退到第 7 版（生成第 9 版），不重新构建。", "数据库、数据目录和环境变量保持现在的样子", "正在进行的第 8 版部署会被取消", "[health] 健康检查通过", "回退成功：第 9 版（基于第 7 版）正在服务。"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRollbackServerTooOldAndMismatch(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	h.api.featuresFn = func(context.Context) ([]string, error) { return nil, nil }
	code, out, _ := h.run("--json", "--app", "web", "rollback", "--to", "5")
	if code != exitOpFailed || h.api.rollbackCalls != 0 {
		t.Fatalf("old server: code=%d calls=%d", code, h.api.rollbackCalls)
	}
	if d := decodeJSON(t, out)["diagnosis"].(map[string]any); d["code"] != client.CodeServerTooOld {
		t.Fatalf("diagnosis = %v", d)
	}
	// Without --to the old route is still fine on an old server.
	if code, _, _ := h.run("--app", "web", "rollback", "--no-wait"); code != exitOK || h.api.rollbackCalls != 1 {
		t.Fatalf("plain rollback on old server: code=%d", code)
	}
	for _, cmd := range [][]string{{"versions"}, {"logs", "--version", "3"}} {
		code, _, errOut := h.run(append([]string{"--app", "web"}, cmd...)...)
		if code != exitOpFailed || !strings.Contains(errOut, "服务器上的 AcornFox 版本较旧") || !strings.Contains(errOut, "先在服务器上升级") {
			t.Fatalf("%v on old server: code=%d err=%s", cmd, code, errOut)
		}
	}

	// A server error that says server_too_old also exits 1.
	h.api.featuresFn = nil
	h.api.rollbackFn = func(context.Context, string, client.RollbackOptions) (client.RollbackResult, error) {
		return client.RollbackResult{}, client.ServerTooOld("回退到指定版本")
	}
	if code, _, _ := h.run("--app", "web", "rollback", "--to", "5"); code != exitOpFailed {
		t.Fatalf("server_too_old reply: code=%d", code)
	}

	// The server went somewhere else: never reported as success.
	h.api.rollbackFn = func(_ context.Context, app string, _ client.RollbackOptions) (client.RollbackResult, error) {
		return client.RollbackResult{Deployment: client.Deployment{ID: "0123456789ab", App: app, Seq: 9, Status: "queued", BasedOnSeq: 4}, Created: true, TargetSeq: 4}, nil
	}
	code, _, errOut := h.run("--app", "web", "rollback", "--to", "5", "--no-wait")
	if code != exitOpFailed || !strings.Contains(errOut, "服务器回到了第 4 版，不是第 5 版") {
		t.Fatalf("mismatch: code=%d err=%s", code, errOut)
	}
	// An existing queued rollback to the same program is not a mismatch.
	h.api.rollbackFn = func(_ context.Context, app string, _ client.RollbackOptions) (client.RollbackResult, error) {
		return client.RollbackResult{Deployment: client.Deployment{ID: "0123456789ab", App: app, Seq: 9, Status: "queued", BasedOnSeq: 7}, TargetSeq: 7}, nil
	}
	if code, out, _ := h.run("--app", "web", "rollback", "--to", "3", "--no-wait"); code != exitOK || !strings.Contains(out, "同一个回退已经在进行") {
		t.Fatalf("dedup: code=%d out=%s", code, out)
	}
}

func TestRollbackRefusalHints(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	h.api.rollbackFn = func(context.Context, string, client.RollbackOptions) (client.RollbackResult, error) {
		return client.RollbackResult{}, &client.Error{Status: 409, Diag: client.Diagnosis{Stage: "server", Code: "deploy_in_progress", Message: "第 10 版正在部署，回退会取消它"}}
	}
	code, _, errOut := h.run("--app", "web", "rollback", "--to", "7")
	if code != exitServerError || !strings.Contains(errOut, "失败：第 10 版正在部署，回退会取消它") || !strings.Contains(errOut, "建议：确认要取消，就加上 --cancel-pending 再执行") {
		t.Fatalf("code=%d err=%s", code, errOut)
	}
}

func TestVersionsOutput(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	old := nowFunc
	nowFunc = func() time.Time { return now }
	t.Cleanup(func() { nowFunc = old })
	var gotLimit int
	h.api.listDepsFn = func(_ context.Context, app string, limit int) (client.DeploymentList, error) {
		gotLimit = limit
		return client.DeploymentList{RollbackTargetSeq: 7, ImageCheck: "ok", Deployments: []client.Deployment{
			{ID: "aaaaaaaaaaa8", Seq: 8, Status: "live", StatusText: "正在服务", SourceText: "上传 · 回退（基于第 5 版）", CreatedAt: now.Add(-2 * time.Minute), DurationSeconds: intp(9), RollbackBlocked: "live", Logs: client.DeploymentLogs{Run: true}},
			{ID: "aaaaaaaaaaa7", Seq: 7, Status: "retired", StatusText: "已被新版本替换", SourceText: "上传", CreatedAt: now.Add(-10 * time.Minute), DurationSeconds: intp(87), CanRollback: true, RollbackText: "可回退"},
			{ID: "aaaaaaaaaaa6", Seq: 6, Status: "failed", StatusText: "部署失败", SourceText: "上传", CreatedAt: now.Add(-time.Hour), DurationSeconds: intp(42), FailureText: "停在健康检查：容器启动后退出（退出码 1）",
				Diagnosis: &client.Diagnosis{Stage: "health", Code: "container_exited", Message: "容器启动后退出（退出码 1）"}},
			{ID: "aaaaaaaaaaa5", Seq: 5, Status: "retired", StatusText: "已被新版本替换", SourceText: "上传 · 添加 PostgreSQL", CreatedAt: now.AddDate(0, 0, -1).Add(-3 * time.Hour), DurationSeconds: intp(63), RollbackBlocked: "same_as_live", SameImageAs: 8, RollbackText: "与第 8 版是同一个程序"},
			{ID: "aaaaaaaaaaa1", Seq: 1, Status: "retired", StatusText: "已被新版本替换", SourceText: "上传", CreatedAt: time.Date(2026, 11, 1, 17, 12, 0, 0, time.Local), RollbackBlocked: "image_gone", RollbackText: "太旧，不能一键回退"},
		}}, nil
	}
	code, out, errOut := h.run("--app", "web", "versions", "--limit", "30")
	if code != exitOK || gotLimit != 30 {
		t.Fatalf("code=%d limit=%d err=%s", code, gotLimit, errOut)
	}
	lines := strings.Split(out, "\n")
	for _, want := range []string{
		"第 8 版  正在服务        上传 · 回退（基于第 5 版）  2 分钟前     9 秒",
		"第 7 版  已被新版本替换  上传                        10 分钟前    1 分 27 秒  可回退",
		"第 6 版  部署失败        上传                        1 小时前     42 秒       停在健康检查：容器启动后退出（退出码 1）",
		"第 5 版  已被新版本替换  上传 · 添加 PostgreSQL      昨天 09:00   1 分 03 秒  与第 8 版是同一个程序",
		"第 1 版  已被新版本替换  上传                        11-01 17:12  —           太旧，不能一键回退",
		"回退到某一版：acornfox rollback --to 7 --app web",
		"看某一版的日志：acornfox logs --version 6 --app web",
		"回退只换程序，数据库、数据目录和环境变量保持现在的样子。",
	} {
		found := false
		for _, l := range lines {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing line %q in:\n%s", want, out)
		}
	}

	code, out, _ = h.run("--json", "--app", "web", "versions")
	m := decodeJSON(t, out)
	vs := m["versions"].([]any)
	first := vs[0].(map[string]any)
	if code != exitOK || m["rollback_target"] != 7.0 || len(vs) != 5 || first["version"] != 8.0 || first["status_text"] != "正在服务" ||
		first["duration_seconds"] != 9.0 || first["logs"].(map[string]any)["run"] != true {
		t.Fatalf("json = %v", m)
	}
	if f := vs[2].(map[string]any); f["diagnosis"].(map[string]any)["code"] != "container_exited" || f["failure_text"] == nil {
		t.Fatalf("failed version json = %v", f)
	}
	if last := vs[4].(map[string]any); last["duration_seconds"] != nil {
		t.Fatalf("unknown duration should be null: %v", last)
	}
	if code, _, _ := h.run("--app", "web", "versions", "--limit", "0"); code != exitUsage {
		t.Fatalf("limit 0: %d", code)
	}
}

func TestDurationAndTimeText(t *testing.T) {
	cases := map[int]string{0: "0 秒", 59: "59 秒", 60: "1 分 00 秒", 131: "2 分 11 秒", 3725: "1 小时 02 分"}
	for in, want := range cases {
		if got := durationText(intp(in)); got != want {
			t.Fatalf("durationText(%d) = %q, want %q", in, got, want)
		}
	}
	if durationText(nil) != "—" {
		t.Fatal("nil duration")
	}
	now := time.Date(2026, 11, 3, 0, 30, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "刚刚"},
		{now.Add(-25 * time.Minute), "25 分钟前"},
		{now.Add(-2 * time.Hour), "昨天 22:30"},
		{time.Date(2025, 12, 1, 8, 0, 0, 0, time.Local), "2025-12-01"},
	} {
		if got := timeText(c.t, now); got != c.want {
			t.Fatalf("timeText(%v) = %q, want %q", c.t, got, c.want)
		}
	}
	if displayWidth("第 8 版") != 7 || displayWidth("（基于）") != 8 {
		t.Fatalf("width: %d %d", displayWidth("第 8 版"), displayWidth("（基于）"))
	}
}

func TestLogsVersion(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	deps := client.DeploymentList{Deployments: []client.Deployment{
		{ID: "aaaaaaaaaaa8", Seq: 8, Status: "live", StatusText: "正在服务"},
		{ID: "aaaaaaaaaaa6", Seq: 6, Status: "failed", StatusText: "部署失败"},
	}}
	h.api.listDepsFn = func(context.Context, string, int) (client.DeploymentList, error) { return deps, nil }
	var gotID, gotKind string
	var gotTail int
	h.api.depLogsFn = func(_ context.Context, id, kind string, tail int) (client.DeploymentLogLines, error) {
		gotID, gotKind, gotTail = id, kind, tail
		return client.DeploymentLogLines{DeploymentID: id, Version: 6, Kind: kind, Source: "archive",
			Lines: []string{"2026-11-03T02:10:31Z Error: Cannot find module 'express'", "plain"}}, nil
	}
	code, out, errOut := h.run("--app", "web", "logs", "--version", "6")
	if code != exitOK || gotID != "aaaaaaaaaaa6" || gotKind != "run" || gotTail != 0 {
		t.Fatalf("code=%d id=%s kind=%s tail=%d err=%s", code, gotID, gotKind, gotTail, errOut)
	}
	local := time.Date(2026, 11, 3, 2, 10, 31, 0, time.UTC).In(time.Local).Format("2006-01-02 15:04:05")
	if !strings.Contains(out, "第 6 版 · 运行日志（部署失败，容器删除前保存）") || !strings.Contains(out, local+"  Error: Cannot find module 'express'") || !strings.Contains(out, "\nplain\n") {
		t.Fatalf("out=%s", out)
	}

	code, out, _ = h.run("--json", "logs", "web", "--version", "6", "--build", "--tail", "50")
	if m := decodeJSON(t, out); code != exitOK || gotKind != "build" || gotTail != 50 || m["version"] != 6.0 || m["kind"] != "build" || m["source"] != "archive" {
		t.Fatalf("json: code=%d %v", code, m)
	}

	// -f only follows the serving version.
	if code, _, errOut := h.run("--app", "web", "logs", "--version", "6", "-f"); code != exitUsage || !strings.Contains(errOut, "第 6 版已停止运行，日志不会再更新") {
		t.Fatalf("follow old: code=%d err=%s", code, errOut)
	}
	for _, args := range [][]string{
		{"logs", "--build"},
		{"logs", "--version", "0"},
		{"logs", "--version", "6", "--since", "2026-09-30T10:00:00Z"},
		{"logs", "--version", "8", "--build", "-f"},
	} {
		if code, _, _ := h.run(append([]string{"--app", "web"}, args...)...); code != exitUsage {
			t.Fatalf("%v: code=%d", args, code)
		}
	}
	if code, _, errOut := h.run("--app", "web", "logs", "--version", "3"); code != exitOpFailed || !strings.Contains(errOut, "没有第 3 版") {
		t.Fatalf("missing version: code=%d err=%s", code, errOut)
	}
	h.api.depLogsFn = func(context.Context, string, string, int) (client.DeploymentLogLines, error) {
		return client.DeploymentLogLines{}, &client.Error{Status: 404, Diag: client.Diagnosis{Stage: "server", Code: "logs_not_kept", Message: "第 6 版部署时还没有保存日志的功能"}}
	}
	if code, _, errOut := h.run("--app", "web", "logs", "--version", "6"); code != exitServerError || !strings.Contains(errOut, "失败：第 6 版部署时还没有保存日志的功能") {
		t.Fatalf("not kept: code=%d err=%s", code, errOut)
	}
}
