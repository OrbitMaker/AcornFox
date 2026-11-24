package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// `apps set`, `apps reapply`, `apps affected` and the --yes of the commands
// that change an app's address, against a fake server that answers what
// internal/apiserver/reapply.go answers.

type fakeApplyAPI struct {
	*fakePrivateAPI
	sets      []client.AppSetRequest
	reapplies []client.AppReapplyRequest
	apps      []string
	affected  string
	plan      string // the plan the server answers
	needYes   bool   // an apply that restarts is refused without confirm_restart
	task      string // the task the server answers when something is done
	taskErr   error
	asked     []string // vars asked of affected
	domain    []client.ApplyOptions
	update    []client.AppSettings
}

const restartPlan = `{"app":"demo-app","needed":true,"restart":true,"vars":[{"name":"params.model","label":"模型名","old":"a","new":"b","from":"user"},{"name":"params.key","old":"******1111","new":"******2222","secret":true,"from":"user"}],"env":["MODEL"],"env_removed":[],"command":false,"files":["config.yaml"],"steps":[{"when":"on_change","names":["rename"],"watch":["params.model"]}],"notes":[],"summary":"demo-app：1 项设置变了，改写 1 个环境变量；会重启 demo-app（数据不动，中间有一小段时间用不了，失败会自动退回）","dependents":["other-app"]}`

const quietPlan = `{"app":"demo-app","needed":false,"restart":false,"vars":[],"env":[],"env_removed":[],"command":false,"files":[],"steps":[],"notes":["现在的设置和应用里用的一致，没有要改的"],"summary":"demo-app 的设置没有变化，什么都不用做"}`

const reapplyTaskDone = `{"id":"aaaaaaaaaaaa","kind":"reapply","target":"demo-app","status":"succeeded","created_at":"2026-11-24T04:10:39Z","finished_at":"2026-11-24T04:10:50Z",
"steps":[{"app":"demo-app","name":"check","label":"检查","status":"done"},{"app":"demo-app","name":"files","label":"写设置","status":"done","message":"改了 1 个环境变量"},{"app":"demo-app","name":"start","label":"重启","status":"done"},{"app":"demo-app","name":"revert","label":"退回","status":"skipped","message":"没出错，不用退回"}],
"apps":[{"app":"demo-app","catalog_id":"demo-app","version":"v1","affected_apps":["other-app"]}]}`

const reapplyTaskFailed = `{"id":"aaaaaaaaaaaa","kind":"reapply","target":"demo-app","status":"failed","created_at":"2026-11-24T04:10:39Z","finished_at":"2026-11-24T04:10:50Z",
"steps":[{"app":"demo-app","name":"check","label":"检查","status":"done"},{"app":"demo-app","name":"start","label":"重启","status":"failed","message":"CAT-INST-15 没能上线"},{"app":"demo-app","name":"revert","label":"退回","status":"done"}],
"apps":[{"app":"demo-app","catalog_id":"demo-app","version":"v1"}],
"fault":{"code":"APP-APPLY-03","message":"重新生效没成功，已经退回原来的设置：CAT-INST-15 没能上线","hint":"把原因告诉用户"}}`

func (f *fakeApplyAPI) apply(plan bool, confirm bool, raw string) (client.AppApply, error) {
	var p client.ApplyPlan
	_ = json.Unmarshal([]byte(f.plan), &p)
	var v client.AppApply
	switch {
	case plan || !p.Needed:
		v = client.AppApply{Plan: p}
		v.Raw = json.RawMessage(`{"plan":` + f.plan + `}`)
	case p.Restart && !confirm && f.needYes:
		return client.AppApply{}, &client.Error{Status: 409, Diag: client.Diagnosis{Stage: "server", Code: "APP-APPLY-05",
			Message: p.Summary + "。要用户同意才做：同意后加上 --yes", Hint: "加 --yes"}, Plan: json.RawMessage(f.plan)}
	default:
		var t client.InstallTask
		_ = json.Unmarshal([]byte(f.task), &t)
		v = client.AppApply{Changed: true, Plan: p, Task: &t}
		v.Raw = json.RawMessage(`{"changed":true,"plan":` + f.plan + `,"task":` + f.task + `}`)
	}
	return v, nil
}

func (f *fakeApplyAPI) AppSet(_ context.Context, app string, req client.AppSetRequest) (client.AppApply, error) {
	f.sets = append(f.sets, req)
	f.apps = append(f.apps, app)
	return f.apply(req.Plan, req.ConfirmRestart, "")
}

func (f *fakeApplyAPI) AppReapply(_ context.Context, app string, req client.AppReapplyRequest) (client.AppApply, error) {
	f.reapplies = append(f.reapplies, req)
	f.apps = append(f.apps, app)
	return f.apply(req.Plan, req.ConfirmRestart, "")
}

func (f *fakeApplyAPI) PlatformAffected(_ context.Context, name string) (client.Affected, error) {
	f.asked = append(f.asked, name)
	var v client.Affected
	_ = json.Unmarshal([]byte(f.affected), &v)
	v.Raw = json.RawMessage(f.affected)
	return v, nil
}

func (f *fakeApplyAPI) InstallTask(context.Context, string) (client.InstallTask, error) {
	var t client.InstallTask
	_ = json.Unmarshal([]byte(f.task), &t)
	t.Raw = json.RawMessage(f.task)
	return t, f.taskErr
}

func (f *fakeApplyAPI) UpdateApp(_ context.Context, app string, s client.AppSettings) (client.AppUpdate, error) {
	f.update = append(f.update, s)
	if s.Exposure != nil && !s.ConfirmRestart && s.Reapply == "" && f.needYes {
		return client.AppUpdate{}, &client.Error{Status: 409, Diag: client.Diagnosis{Stage: "server", Code: "APP-APPLY-05", Message: "会重启 demo-app，要用户同意才做"}, Plan: json.RawMessage(f.plan)}
	}
	u := client.AppUpdate{App: client.App{Name: app, Exposure: "lan"}}
	if s.Exposure != nil && s.Reapply == "" {
		var p client.ApplyPlan
		_ = json.Unmarshal([]byte(f.plan), &p)
		var t client.InstallTask
		_ = json.Unmarshal([]byte(f.task), &t)
		u.Reapply = &client.ApplyFollow{Plan: p, Task: &t}
	}
	return u, nil
}

func (f *fakeApplyAPI) AddDomainApply(_ context.Context, app, name string, o client.ApplyOptions) (client.Domain, *client.ApplyFollow, error) {
	f.domain = append(f.domain, o)
	if !o.ConfirmRestart && !o.NoReapply && f.needYes {
		return client.Domain{}, nil, &client.Error{Status: 409, Diag: client.Diagnosis{Stage: "server", Code: "APP-APPLY-05", Message: "会重启 demo-app"}, Plan: json.RawMessage(f.plan)}
	}
	return client.Domain{App: app, Name: name, Status: "pending"}, nil, nil
}

func (f *fakeApplyAPI) RemoveDomainApply(context.Context, string, string, client.ApplyOptions) (*client.ApplyFollow, error) {
	return nil, nil
}

func applyHarness(t *testing.T, plan, task string) (*harness, *fakeApplyAPI) {
	h, p := privateHarness(t)
	f := &fakeApplyAPI{fakePrivateAPI: p, plan: plan, task: task, needYes: true}
	h.connect = func(context.Context, client.Target) (client.API, error) { return f, nil }
	return h, f
}

func TestAppsSetPlanOnly(t *testing.T) {
	h, f := applyHarness(t, restartPlan, reapplyTaskDone)
	code, out, errOut := h.run("apps", "set", "demo-app", "model=b", "--plan")
	if code != exitOK || len(f.sets) != 1 || !f.sets[0].Plan || f.sets[0].ConfirmRestart || f.sets[0].Set["model"] != "b" || f.apps[0] != "demo-app" {
		t.Fatalf("exit %d sets %+v stderr %s", code, f.sets, errOut)
	}
	for _, want := range []string{"会重启 demo-app", "params.model（模型名）：a → b（你改的）", "改写的环境变量：MODEL", "重写的配置文件：config.yaml",
		"看的设置（params.model）变了后", "用它提供的服务的应用：other-app", "做的话加 --yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("没有 %q：\n%s", want, out)
		}
	}
	if code, out, _ := h.run("--json", "apps", "set", "demo-app", "model=b", "--plan"); code != exitOK || !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"restart":true`) {
		t.Fatalf("json %d %s", code, out)
	}
}

func TestAppsSetNeedsTheUsersGoAhead(t *testing.T) {
	h, f := applyHarness(t, restartPlan, reapplyTaskDone)
	code, out, errOut := h.run("apps", "set", "demo-app", "model=b")
	if code != exitServerError || !strings.Contains(errOut, "APP-APPLY-05") || !strings.Contains(out, "会重启 demo-app") {
		t.Fatalf("exit %d out %s err %s", code, out, errOut)
	}
	if len(f.sets) != 1 || f.sets[0].ConfirmRestart {
		t.Fatalf("sets %+v", f.sets)
	}
	code, out, _ = h.run("--json", "apps", "set", "demo-app", "model=b")
	if code != exitServerError || !strings.Contains(out, `"code":"APP-APPLY-05"`) || !strings.Contains(out, `"plan":`) {
		t.Fatalf("json exit %d %s", code, out)
	}
}

func TestAppsSetWithYesWaitsForTheTask(t *testing.T) {
	h, f := applyHarness(t, restartPlan, reapplyTaskDone)
	code, out, errOut := h.run("apps", "set", "demo-app", "model=b", "platform.tz=Asia/Tokyo", "--reset", "platform.mirror.npm", "--yes")
	if code != exitOK || len(f.sets) != 1 || !f.sets[0].ConfirmRestart || f.sets[0].Set["platform.tz"] != "Asia/Tokyo" || len(f.sets[0].Reset) != 1 {
		t.Fatalf("exit %d sets %+v stderr %s", code, f.sets, errOut)
	}
	if !strings.Contains(out, "已重新生效") || !strings.Contains(out, "other-app") || !strings.Contains(out, "acornfox apps reapply") {
		t.Fatalf("out %s", out)
	}
	if strings.Contains(out, "已装好") {
		t.Fatalf("says install words: %s", out)
	}
	code, out, _ = h.run("--json", "apps", "set", "demo-app", "model=b", "--yes")
	if code != exitOK || !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"say":"已重新生效`) {
		t.Fatalf("json %d %s", code, out)
	}
}

func TestAppsSetFailedTaskIsReportedAsUndone(t *testing.T) {
	h, _ := applyHarness(t, restartPlan, reapplyTaskFailed)
	code, _, errOut := h.run("apps", "set", "demo-app", "model=b", "--yes")
	if code != exitOpFailed || !strings.Contains(errOut, "APP-APPLY-03") || !strings.Contains(errOut, "已经退回原来的设置") || strings.Contains(errOut, "已清理") {
		t.Fatalf("exit %d %s", code, errOut)
	}
	code, out, _ := h.run("--json", "apps", "set", "demo-app", "model=b", "--yes")
	if code != exitOpFailed || !strings.Contains(out, `"ok":false`) || !strings.Contains(out, "重新生效没成功") {
		t.Fatalf("json %d %s", code, out)
	}
}

func TestAppsSetNoWaitAndNothingToDo(t *testing.T) {
	h, _ := applyHarness(t, restartPlan, reapplyTaskDone)
	code, out, _ := h.run("--json", "apps", "set", "demo-app", "model=b", "--yes", "--no-wait")
	if code != exitOK || !strings.Contains(out, `"task_id":"aaaaaaaaaaaa"`) || !strings.Contains(out, "install status aaaaaaaaaaaa --wait") {
		t.Fatalf("no-wait %d %s", code, out)
	}
	h, _ = applyHarness(t, quietPlan, reapplyTaskDone)
	code, out, _ = h.run("apps", "set", "demo-app", "model=b")
	if code != exitOK || !strings.Contains(out, "没有变化") {
		t.Fatalf("nothing %d %s", code, out)
	}
}

func TestAppsSetArguments(t *testing.T) {
	h, _ := applyHarness(t, restartPlan, reapplyTaskDone)
	for _, c := range [][]string{
		{"apps", "set"},                   // no app
		{"apps", "set", "demo-app"},       // nothing to change
		{"apps", "set", "demo-app", "=b"}, // no name
	} {
		if code, _, _ := h.run(c...); code != exitUsage {
			t.Errorf("%v: exit %d", c, code)
		}
	}
	if code, out, _ := h.run("apps", "set", "-h"); code != exitOK || !strings.Contains(out, "副作用") || !strings.Contains(out, "--plan") || !strings.Contains(out, "acornfox apps set demo-app model=qwen-plus --plan") {
		t.Fatalf("help %d %s", code, out)
	}
	if code, out, _ := h.run("--app", "demo-app", "apps", "set", "model=b", "--plan"); code != exitOK {
		t.Fatalf("--app %d %s", code, out)
	}
}

func TestAppsSetSecretFromStdin(t *testing.T) {
	h, f := applyHarness(t, restartPlan, reapplyTaskDone)
	code, out, errOut := h.runStdin("a-new-password-0001\n", "apps", "set", "demo-app", "pw=-", "--plan")
	if code != exitOK || f.sets[0].Set["pw"] != "a-new-password-0001" {
		t.Fatalf("exit %d sets %+v stderr %s", code, f.sets, errOut)
	}
	if strings.Contains(out+errOut, "a-new-password-0001") {
		t.Fatalf("the secret is in the output: %s %s", out, errOut)
	}
}

func TestAppsReapplyAndAffected(t *testing.T) {
	h, f := applyHarness(t, restartPlan, reapplyTaskDone)
	f.affected = `{"var":"","apps":[{"app":"demo-app","catalog_id":"demo-app","uses":true,"differs":true,"vars":[{"name":"platform.tz","old":"UTC","new":"Asia/Tokyo","from":"platform"}],"restart":true,"summary":"demo-app：1 项设置变了；会重启 demo-app"},
{"app":"stopped-app","catalog_id":"x","uses":true,"differs":false,"vars":[],"restart":false,"blocked":"停着","summary":""}],"notes":[]}`
	code, out, _ := h.run("apps", "affected")
	if code != exitOK || !strings.Contains(out, "platform.tz：UTC → Asia/Tokyo") || !strings.Contains(out, "stopped-app：现在不能重新生效：停着") || !strings.Contains(out, "acornfox apps reapply demo-app --plan") {
		t.Fatalf("affected %d %s", code, out)
	}
	if code, out, _ := h.run("--json", "apps", "affected", "--var", "platform.tz"); code != exitOK || f.asked[len(f.asked)-1] != "platform.tz" || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("by var %d %s", code, out)
	}
	// one app
	code, _, errOut := h.run("apps", "reapply", "demo-app")
	if code != exitServerError || !strings.Contains(errOut, "APP-APPLY-05") {
		t.Fatalf("without --yes %d %s", code, errOut)
	}
	code, out, _ = h.run("apps", "reapply", "demo-app", "--yes", "--force")
	if code != exitOK || !f.reapplies[len(f.reapplies)-1].ConfirmRestart || !f.reapplies[len(f.reapplies)-1].Force || !strings.Contains(out, "已重新生效") {
		t.Fatalf("reapply %d %s", code, out)
	}
	// all that are behind: the blocked one is left, a go-ahead is needed
	f.reapplies, f.apps = nil, nil
	code, out, errOut = h.run("apps", "reapply", "--all")
	if code != exitServerError || len(f.reapplies) != 0 || !strings.Contains(errOut, "APP-APPLY-05") || !strings.Contains(out, "demo-app") {
		t.Fatalf("--all without --yes %d %s %s %+v", code, out, errOut, f.reapplies)
	}
	code, out, _ = h.run("apps", "reapply", "--all", "--yes")
	if code != exitOK || len(f.apps) != 1 || f.apps[0] != "demo-app" || !strings.Contains(out, "已重新生效") {
		t.Fatalf("--all %d %s %v", code, out, f.apps)
	}
	if code, _, _ := h.run("apps", "reapply", "--all", "demo-app"); code != exitUsage {
		t.Fatalf("--all with an app: %d", code)
	}
	if code, _, _ := h.run("apps", "reapply", "demo-app", "--var", "platform.tz"); code != exitUsage {
		t.Fatalf("--var alone: %d", code)
	}
	if code, out, _ := h.run("apps", "reapply", "-h"); code != exitOK || !strings.Contains(out, "副作用") && !strings.Contains(out, "失败自动退回") {
		t.Fatalf("help %d %s", code, out)
	}
}

func TestExposeAndDomainFollowTheApp(t *testing.T) {
	h, f := applyHarness(t, restartPlan, reapplyTaskDone)
	code, _, errOut := h.run("--app", "demo-app", "app", "set", "--expose", "lan")
	if code != exitServerError || !strings.Contains(errOut, "APP-APPLY-05") {
		t.Fatalf("without --yes %d %s", code, errOut)
	}
	code, out, _ := h.run("--app", "demo-app", "app", "set", "--expose", "lan", "--yes")
	if code != exitOK || !f.update[len(f.update)-1].ConfirmRestart || !strings.Contains(out, "已重新生效") {
		t.Fatalf("with --yes %d %s", code, out)
	}
	code, out, _ = h.run("--app", "demo-app", "app", "set", "--expose", "lan", "--no-reapply")
	if code != exitOK || f.update[len(f.update)-1].Reapply != "skip" || strings.Contains(out, "已重新生效") {
		t.Fatalf("--no-reapply %d %s", code, out)
	}
	if code, _, errOut := h.run("--app", "demo-app", "domain", "add", "a.example.com"); code != exitServerError || !strings.Contains(errOut, "APP-APPLY-05") {
		t.Fatalf("domain without --yes %d %s", code, errOut)
	}
	if code, _, _ := h.run("--app", "demo-app", "domain", "add", "a.example.com", "--yes"); code != exitOK || !f.domain[len(f.domain)-1].ConfirmRestart {
		t.Fatalf("domain --yes %d %+v", code, f.domain)
	}
	if code, _, _ := h.run("--app", "demo-app", "domain", "add", "a.example.com", "--no-reapply"); code != exitOK || !f.domain[len(f.domain)-1].NoReapply {
		t.Fatalf("domain --no-reapply %d %+v", code, f.domain)
	}
}
