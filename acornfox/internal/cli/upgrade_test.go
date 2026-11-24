package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// `apps outdated`, `apps upgrade`, `apps rollback` against a fake server
// that answers what internal/apiserver/upgrade.go answers.

type fakeUpgradeAPI struct {
	*fakePrivateAPI
	upgrades  []client.AppUpgradeRequest
	rollbacks []client.AppRollbackRequest
	apps      []string
	plan      string // the upgrade plan the server answers
	rplan     string // the rollback plan
	task      string
	needYes   bool
	blocked   bool
}

const upgradePlanOK = `{"app":"demo-app","from":"1.0.0","to":"1.1.0","can_upgrade":true,"restart":true,"snapshot":true,"snapshot_mb":12,"disk_ok":true,
"vars_added":["extra（额外设置）"],"vars_removed":[],"uses_changed":[],"notes":["新版本里应用监听的端口从 80 改成了 8080"],
"summary":"把 demo-app 从 1.0.0 升到 1.1.0：先把它的数据（约 12 MB）存一份快照在本机，打包时应用会暂停一阵；再换上新版本重启，健康检查通过才算成功。中间哪一步没成功都会自动退回到升级前"}`

const upgradePlanBlocked = `{"app":"demo-app","from":"1.0.0","to":"1.1.0","can_upgrade":false,"blocked":"新版本 1.1.0 多了必填设置 must","blocked_code":"APP-UPG-02","restart":true,"snapshot":false,"snapshot_mb":0,"disk_ok":true,
"vars_added":["must"],"vars_removed":[],"uses_changed":[],"notes":[],"summary":"demo-app 现在不能从 1.0.0 升到 1.1.0：新版本 1.1.0 多了必填设置 must。什么都没动"}`

const upgradeTaskDone = `{"id":"bbbbbbbbbbbb","kind":"upgrade","target":"demo-app","status":"succeeded","created_at":"2026-11-24T04:10:39Z","finished_at":"2026-11-24T04:12:50Z",
"steps":[{"app":"demo-app","name":"check","label":"检查","status":"done"},{"app":"demo-app","name":"image","label":"下载新版本","status":"done"},{"app":"demo-app","name":"snapshot","label":"留升级前快照","status":"done","message":"快照 20261124-120001-ab01（3 MB）"},{"app":"demo-app","name":"revert","label":"退回","status":"skipped","message":"没出错，不用退回"}],
"apps":[{"app":"demo-app","catalog_id":"demo-app","version":"1.1.0","from":"1.0.0","snapshot_id":"20261124-120001-ab01"}]}`

const upgradeTaskFailed = `{"id":"bbbbbbbbbbbb","kind":"upgrade","target":"demo-app","status":"failed","created_at":"2026-11-24T04:10:39Z","finished_at":"2026-11-24T04:12:50Z",
"steps":[{"app":"demo-app","name":"health","label":"健康检查","status":"failed","message":"CAT-INST-16 没通过"},{"app":"demo-app","name":"revert","label":"退回","status":"done"}],
"apps":[{"app":"demo-app","catalog_id":"demo-app","version":"1.0.0","from":"1.0.0"}],
"fault":{"code":"APP-UPG-05","message":"升级没成功，已经退回到升级前的 1.0.0（数据和设置都是升级前的）：CAT-INST-16 没通过","hint":"把原因告诉用户"}}`

const rollbackPlanOK = `{"app":"demo-app","from":"1.1.0","to":"1.0.0","can_rollback":true,"snapshot_id":"20261124-120001-ab01","loses_data":true,"restart":true,"disk_ok":true,"notes":[],
"summary":"把 demo-app 从 1.1.0 退回到升级前的 1.0.0：用 2026-11-24 12:00 留的快照里的数据和设置覆盖现在的——升级之后写进应用的新数据会没有"}`

const rollbackTaskDone = `{"id":"cccccccccccc","kind":"rollback","target":"demo-app","status":"succeeded","created_at":"2026-11-24T04:10:39Z","finished_at":"2026-11-24T04:12:50Z",
"steps":[{"app":"demo-app","name":"check","label":"检查","status":"done"},{"app":"demo-app","name":"restore","label":"换回升级前的数据和版本","status":"done"}],
"apps":[{"app":"demo-app","catalog_id":"demo-app","version":"1.0.0","from":"1.1.0","notes":["退回前的数据另存在备份 20261124-130000-cd01"]}]}`

func (f *fakeUpgradeAPI) AppUpgrade(_ context.Context, app string, req client.AppUpgradeRequest) (client.AppUpgrade, error) {
	f.upgrades = append(f.upgrades, req)
	f.apps = append(f.apps, app)
	raw := f.plan
	var p client.UpgradePlan
	_ = json.Unmarshal([]byte(raw), &p)
	switch {
	case req.Plan:
		v := client.AppUpgrade{Plan: p}
		v.Raw = json.RawMessage(`{"plan":` + raw + `}`)
		return v, nil
	case !p.CanUpgrade:
		return client.AppUpgrade{}, &client.Error{Status: 409, Diag: client.Diagnosis{Stage: "server", Code: p.BlockedCode, Message: p.Blocked}, Plan: json.RawMessage(raw)}
	case !req.ConfirmRestart && f.needYes:
		return client.AppUpgrade{}, &client.Error{Status: 409, Diag: client.Diagnosis{Stage: "server", Code: "APP-UPG-04", Message: p.Summary + "。要用户同意才做"}, Plan: json.RawMessage(raw)}
	}
	var t client.InstallTask
	_ = json.Unmarshal([]byte(f.task), &t)
	v := client.AppUpgrade{Plan: p, Task: &t}
	v.Raw = json.RawMessage(`{"plan":` + raw + `,"task":` + f.task + `}`)
	return v, nil
}

func (f *fakeUpgradeAPI) AppRollback(_ context.Context, app string, req client.AppRollbackRequest) (client.AppRollback, error) {
	f.rollbacks = append(f.rollbacks, req)
	f.apps = append(f.apps, app)
	var p client.RollbackPlan
	_ = json.Unmarshal([]byte(f.rplan), &p)
	switch {
	case req.Plan:
		v := client.AppRollback{Plan: p}
		v.Raw = json.RawMessage(`{"plan":` + f.rplan + `}`)
		return v, nil
	case !req.Confirm && f.needYes:
		return client.AppRollback{}, &client.Error{Status: 409, Diag: client.Diagnosis{Stage: "server", Code: "APP-UPG-08", Message: p.Summary + "。要用户同意才做"}, Plan: json.RawMessage(f.rplan)}
	}
	var t client.InstallTask
	_ = json.Unmarshal([]byte(f.task), &t)
	v := client.AppRollback{Plan: p, Task: &t}
	v.Raw = json.RawMessage(`{"plan":` + f.rplan + `,"task":` + f.task + `}`)
	return v, nil
}

func (f *fakeUpgradeAPI) InstallTask(context.Context, string) (client.InstallTask, error) {
	var t client.InstallTask
	_ = json.Unmarshal([]byte(f.task), &t)
	t.Raw = json.RawMessage(f.task)
	return t, nil
}

func upgradeHarness(t *testing.T, plan, task string) (*harness, *fakeUpgradeAPI) {
	h, p := privateHarness(t)
	f := &fakeUpgradeAPI{fakePrivateAPI: p, plan: plan, rplan: rollbackPlanOK, task: task, needYes: true}
	h.connect = func(context.Context, client.Target) (client.API, error) { return f, nil }
	return h, f
}

func TestAppsOutdated(t *testing.T) {
	h, f := upgradeHarness(t, upgradePlanOK, upgradeTaskDone)
	f.fakeAPI.appsFn = func(context.Context) ([]client.App, error) {
		return []client.App{
			{Name: "plain"},
			{Name: "uptodate", CatalogID: "uptodate", CatalogVersion: "1.0.0", Upgrade: &client.AppUpgradeInfo{}},
			{Name: "demo-app", CatalogID: "demo-app", CatalogVersion: "1.0.0", CatalogUpdate: "1.1.0",
				Upgrade: &client.AppUpgradeInfo{Available: "1.1.0", CanUpgrade: true, Snapshot: &client.AppSnapshot{ID: "20261124-120001-ab01", Version: "0.9.0", CreatedAt: time.Now(), SizeMB: 7}, CanRollback: true}},
			{Name: "old-box", CatalogID: "old-box", CatalogVersion: "2.0.0", Upgrade: &client.AppUpgradeInfo{Blocked: "目录里有新版本 3.0.0，要先把 AcornFox 升级到 0.9.0 才能升级这个应用", BlockedCode: "CAT-INST-08"}},
		}, nil
	}
	code, out, errOut := h.run("apps", "outdated")
	if code != exitOK {
		t.Fatalf("exit %d %s", code, errOut)
	}
	for _, want := range []string{"demo-app：现在 1.0.0，应用中心有 1.1.0", "acornfox apps upgrade demo-app --plan", "留着升级前（0.9.0）的快照 20261124-120001-ab01", "acornfox apps rollback demo-app --plan", "old-box", "CAT-INST-08"} {
		if !strings.Contains(out, want) {
			t.Errorf("没有 %q：\n%s", want, out)
		}
	}
	if strings.Contains(out, "plain") || strings.Contains(out, "uptodate") {
		t.Errorf("列了没新版本的应用：\n%s", out)
	}
	code, out, _ = h.run("--json", "apps", "outdated")
	m := decodeJSON(t, out)
	if code != exitOK || m["ok"] != true || m["count"] != float64(1) || len(m["apps"].([]any)) != 2 {
		t.Fatalf("json %d %s", code, out)
	}
	first := m["apps"].([]any)[0].(map[string]any)
	if first["app"] != "demo-app" || first["available"] != "1.1.0" || first["can_rollback"] != true || first["snapshot"] == nil {
		t.Fatalf("first %v", first)
	}
	// nothing at all
	f.fakeAPI.appsFn = nil
	if code, out, _ := h.run("apps", "outdated"); code != exitOK || !strings.Contains(out, "没有应用有新版本") {
		t.Fatalf("none %d %s", code, out)
	}
}

// `apps outdated` asks the server to sync when the catalog is old, tells how
// that went, and still answers when it did not work.
func TestAppsOutdatedSyncsIfStale(t *testing.T) {
	h, f := upgradeHarness(t, upgradePlanOK, upgradeTaskDone)
	f.fakeAPI.appsFn = func(context.Context) ([]client.App, error) { return nil, nil }

	at := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	f.ifStale = &client.CatalogSync{OK: true, Ran: true, Updated: true, At: &at, Message: "已更新到新目录"}
	code, out, _ := h.run("--json", "apps", "outdated")
	m := decodeJSON(t, out)
	cs, _ := m["catalog_sync"].(map[string]any)
	if code != exitOK || f.ifStaleCalls != 1 || cs == nil || cs["ran"] != true || cs["ok"] != true || cs["at"] == nil {
		t.Fatalf("同步结果要写进 json：%d calls=%d %s", code, f.ifStaleCalls, out)
	}
	if code, out, _ := h.run("apps", "outdated"); code != exitOK || strings.Contains(out, "注意") {
		t.Fatalf("同步成功不该有提醒：%d %s", code, out)
	}

	// The sync did not work: the answer comes from the catalog last synced and says so; the exit code stays 0.
	f.ifStale = &client.CatalogSync{OK: false, Ran: true, Code: "CAT-SYNC-08", Message: "所有来源都失败，继续用手上的目录"}
	code, out, _ = h.run("apps", "outdated")
	if code != exitOK || !strings.Contains(out, "注意：应用目录最近一次同步没成功（CAT-SYNC-08") || !strings.Contains(out, "没有应用有新版本") {
		t.Fatalf("同步失败要说明且照常回答：%d %s", code, out)
	}
	code, out, _ = h.run("--json", "apps", "outdated")
	cs, _ = decodeJSON(t, out)["catalog_sync"].(map[string]any)
	if code != exitOK || cs["ok"] != false || cs["code"] != "CAT-SYNC-08" {
		t.Fatalf("json：%d %s", code, out)
	}

	// Never synced (no code): nothing to warn about.
	f.ifStale = &client.CatalogSync{OK: false, Message: "还没有同步过"}
	if code, out, _ := h.run("apps", "outdated"); code != exitOK || strings.Contains(out, "注意") {
		t.Fatalf("还没同步过不该提醒：%d %s", code, out)
	}
}

func TestAppsUpgradePlanAndNeedsTheUsersYes(t *testing.T) {
	h, f := upgradeHarness(t, upgradePlanOK, upgradeTaskDone)
	code, out, errOut := h.run("apps", "upgrade", "demo-app", "--plan")
	if code != exitOK || len(f.upgrades) != 1 || !f.upgrades[0].Plan || f.upgrades[0].ConfirmRestart || f.apps[0] != "demo-app" {
		t.Fatalf("exit %d upgrades %+v %s", code, f.upgrades, errOut)
	}
	for _, want := range []string{"把 demo-app 从 1.0.0 升到 1.1.0", "新增设置：extra（额外设置）", "注意：新版本里应用监听的端口", "快照：约 12 MB，磁盘够", "做的话加 --yes（会重启 demo-app"} {
		if !strings.Contains(out, want) {
			t.Errorf("没有 %q：\n%s", want, out)
		}
	}
	code, out, errOut = h.run("apps", "upgrade", "demo-app")
	if code != exitServerError || !strings.Contains(errOut, "APP-UPG-04") || !strings.Contains(out, "把 demo-app 从 1.0.0 升到 1.1.0") {
		t.Fatalf("without --yes: %d out %s err %s", code, out, errOut)
	}
	code, out, _ = h.run("--json", "apps", "upgrade", "demo-app")
	if code != exitServerError || !strings.Contains(out, `"code":"APP-UPG-04"`) || !strings.Contains(out, `"plan":`) {
		t.Fatalf("json: %d %s", code, out)
	}
}

func TestAppsUpgradeWithYesWaitsForTheTask(t *testing.T) {
	h, f := upgradeHarness(t, upgradePlanOK, upgradeTaskDone)
	code, out, errOut := h.run("apps", "upgrade", "demo-app", "--to", "1.1.0", "--yes", "--set", "extra=hello")
	last := f.upgrades[len(f.upgrades)-1]
	if code != exitOK || !last.ConfirmRestart || last.To != "1.1.0" || last.Set["extra"] != "hello" {
		t.Fatalf("exit %d %+v stderr %s", code, last, errOut)
	}
	for _, want := range []string{"已升级：demo-app 1.0.0 → 1.1.0", "20261124-120001-ab01", "acornfox apps rollback demo-app --plan"} {
		if !strings.Contains(out, want) {
			t.Errorf("没有 %q：\n%s", want, out)
		}
	}
	if strings.Contains(out, "已装好") || strings.Contains(out, "已重新生效") {
		t.Fatalf("says other words: %s", out)
	}
	code, out, _ = h.run("--json", "apps", "upgrade", "demo-app", "--yes")
	if code != exitOK || !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"say":"已升级`) {
		t.Fatalf("json %d %s", code, out)
	}
	code, out, _ = h.run("--json", "apps", "upgrade", "demo-app", "--yes", "--no-wait")
	if code != exitOK || !strings.Contains(out, `"task_id":"bbbbbbbbbbbb"`) || !strings.Contains(out, "install status bbbbbbbbbbbb --wait") {
		t.Fatalf("no-wait %d %s", code, out)
	}
}

func TestAppsUpgradeFailedTaskAndBlockedPlan(t *testing.T) {
	h, _ := upgradeHarness(t, upgradePlanOK, upgradeTaskFailed)
	code, _, errOut := h.run("apps", "upgrade", "demo-app", "--yes")
	if code != exitOpFailed || !strings.Contains(errOut, "APP-UPG-05") || !strings.Contains(errOut, "升级没成功") || !strings.Contains(errOut, "已经退回到升级前") {
		t.Fatalf("exit %d %s", code, errOut)
	}
	code, out, _ := h.run("--json", "apps", "upgrade", "demo-app", "--yes")
	if code != exitOpFailed || !strings.Contains(out, `"ok":false`) || !strings.Contains(out, "升级没成功") {
		t.Fatalf("json %d %s", code, out)
	}
	h, _ = upgradeHarness(t, upgradePlanBlocked, upgradeTaskDone)
	code, out, errOut = h.run("apps", "upgrade", "demo-app", "--yes")
	if code != exitServerError || !strings.Contains(errOut, "APP-UPG-02") || !strings.Contains(out, "什么都没动") {
		t.Fatalf("blocked: %d out %s err %s", code, out, errOut)
	}
}

func TestAppsUpgradeArguments(t *testing.T) {
	h, f := upgradeHarness(t, upgradePlanOK, upgradeTaskDone)
	if code, _, _ := h.run("apps", "upgrade"); code != exitUsage {
		t.Errorf("no app: exit %d", code)
	}
	if code, _, _ := h.run("apps", "upgrade", "demo-app", "--set", "platform.tz=UTC"); code != exitUsage {
		t.Errorf("platform: exit %d", code)
	}
	if code, out, _ := h.run("apps", "upgrade", "-h"); code != exitOK || !strings.Contains(out, "副作用") || !strings.Contains(out, "APP-UPG-05") || !strings.Contains(out, "acornfox apps upgrade demo-app --plan") {
		t.Fatalf("help %d %s", code, out)
	}
	code, _, errOut := h.runStdin("a-new-key-0000000001\n", "apps", "upgrade", "demo-app", "--set", "key=-", "--plan")
	if code != exitOK || f.upgrades[len(f.upgrades)-1].Set["key"] != "a-new-key-0000000001" {
		t.Fatalf("stdin %d %+v %s", code, f.upgrades, errOut)
	}
}

func TestAppsRollback(t *testing.T) {
	h, f := upgradeHarness(t, upgradePlanOK, rollbackTaskDone)
	code, out, errOut := h.run("apps", "rollback", "demo-app", "--plan")
	if code != exitOK || len(f.rollbacks) != 1 || !f.rollbacks[0].Plan || !strings.Contains(out, "升级之后写进应用的新数据会没有") || !strings.Contains(out, "做的话加 --yes") {
		t.Fatalf("plan %d %s %s", code, out, errOut)
	}
	code, _, errOut = h.run("apps", "rollback", "demo-app")
	if code != exitServerError || !strings.Contains(errOut, "APP-UPG-08") {
		t.Fatalf("without --yes %d %s", code, errOut)
	}
	code, out, _ = h.run("apps", "rollback", "demo-app", "--yes")
	if code != exitOK || !f.rollbacks[len(f.rollbacks)-1].Confirm || !strings.Contains(out, "已退回：demo-app 现在是 1.0.0") || !strings.Contains(out, "20261124-130000-cd01") {
		t.Fatalf("rollback %d %s", code, out)
	}
	code, out, _ = h.run("--json", "apps", "rollback", "demo-app", "--yes")
	if code != exitOK || !strings.Contains(out, `"say":"已退回到升级前`) {
		t.Fatalf("json %d %s", code, out)
	}
	if code, out, _ := h.run("apps", "rollback", "-h"); code != exitOK || !strings.Contains(out, "副作用") || !strings.Contains(out, "APP-UPG-09") {
		t.Fatalf("help %d %s", code, out)
	}
}
