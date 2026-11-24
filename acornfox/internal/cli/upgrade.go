package cli

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// `acornfox apps outdated|upgrade|rollback`: see which apps from the app
// center have a newer version, upgrade one (with a snapshot taken first and
// an automatic way back), and go back to before the upgrade. The server
// decides everything (POST /v1/apps/{app}/upgrade and …/rollback:
// internal/appinstall/upgrade.go); the CLI only asks, shows the plan and
// waits for the task.

// outdatedSyncWait is how long `apps outdated` waits for the sync it starts.
const outdatedSyncWait = 60 * time.Second

const appsOutdatedUsage = `用法：acornfox apps outdated

用途：看哪些从应用中心装的应用，应用中心里有比现在装的更新的版本（并且这台机器能装）；也列出有新版本但装不了的
  应用和原因（比如要先升级 AcornFox），以及哪些应用留着升级前的快照、能退回。目录由后台服务每 6 小时（上下抖一点）
  同步一次，有人打开控制台的应用中心或“我的应用”时，距上次成功同步超过 30 分钟也会在后台同步；这条命令一样：先看
  距上次成功同步是否超过 30 分钟，超过就先同步一次（最多等 60 秒，同步不成功不影响结果，按上次同步到的目录算，并说明）。
  刚改过目录想不管多久都马上同步，用 acornfox catalog sync。间隔和 30 分钟能用后台服务的环境变量改（ACORNFOX_CATALOG_SYNC_INTERVAL、ACORNFOX_CATALOG_STALE_AFTER），见用户文档“应用目录同步的节奏”。
副作用：可能联网同步一次应用目录（写本机的目录缓存、对有新版本的应用记一条“有新版本”的事件）；不动任何应用。
输出（--json）：{"ok":true,"count":有新版本的个数,"catalog_sync":{"ran":这次是否同步了,"ok":最近一次同步是否成功,"code":失败编号,
  "message":"…","at":最近一次同步的时间},"apps":[{"app","catalog_id","current","available","can_upgrade",
  "blocked","blocked_code","snapshot":{id,version,created_at,size_mb}|null,"can_rollback"}]}
退出码：0 正常；3 连不上服务器；4 服务器拒绝。
下一步：acornfox apps upgrade 应用 --plan 看会怎么升（只读）。升级会重启应用，要用户同意。
例子：
  acornfox apps outdated
  acornfox --json apps outdated`

const appsUpgradeUsage = `用法：acornfox apps upgrade 应用 [--to 版本] [--set 设置名=值]... [--plan] [--yes] [--no-wait] [--timeout 秒]

用途：把一个从应用中心装的应用升到应用中心里现在的新版本。只在用户点头之后做，平台从不自己升级。
流程：先算出会做什么（--plan 只做这一步，什么都不动）：新版本这台机器能不能装、新版本要的东西（必填的新设置、必须的新
  服务、内存）有没有、磁盘够不够留快照；缺的会停下告诉你要什么，平台不替用户去装别的应用。然后：下载新版本（应用
  照常运行）→ 把应用的数据存一份快照在本机（打包时应用会暂停一阵，每个应用只留最近一份）→ 换上新版本重启 → 健康检查。
  任何一步没成功都会自动退回到升级前（APP-UPG-05，数据和设置都用快照恢复），退回也出错报 APP-UPG-06（要人处理）。
  升级成功后快照还留着，出问题可以 acornfox apps rollback 应用 退回到升级前。
副作用：下载镜像；暂停应用打包数据，写 备份目录/应用名/ 下的快照；改应用的环境变量、启动命令、配置文件和端口，重建
  容器（版本历史里多一个“重新生效”），做清单里写的“重新生效后的步骤”，记下新版本；写应用的事件。不碰别的应用。
参数：
  --to 版本        升到哪个版本（目录里每个应用只有一个最新版本，不写就是它）
  --set 设置名=值  新版本多出来的必填设置的值（密码类写 设置名=-，从标准输入读一行）；可写几次
  --plan           只说会做什么，不动任何东西
  --yes            用户同意重启这个应用（升级必须加）
  --no-wait        只提交，输出任务编号；之后 acornfox install status 任务编号 --wait
  --timeout 秒     等多久（默认 600，最多 3600；大数据的快照要的时间长就加大）
输出（--json）：{"ok":true,"plan":{app,from,to,can_upgrade,blocked,blocked_code,restart,snapshot,snapshot_mb,disk_ok,
  vars_added[],vars_removed[],uses_changed[],notes[],summary,settings},"task":{…}}；等完以后是任务的结果
  （同 install status --json，apps[].snapshot_id 是留的快照）。
退出码：0 正常；1 升级没成功（已退回，看 diagnosis）；2 参数不对；4 服务器拒绝（APP-UPG-01～04、BKP-01 等）。
例子：
  acornfox apps upgrade demo-app --plan
  acornfox apps upgrade demo-app --yes
  acornfox apps upgrade demo-app --set api_key=- --yes`

const appsRollbackUsage = `用法：acornfox apps rollback 应用 [--plan] [--yes] [--no-wait] [--timeout 秒]

用途：把一个升过级的应用退回到升级前：用升级前留的快照里的数据、设置和程序版本，换掉现在的。升级之后写进应用的新数据会
  没有，所以要用户先同意。
流程：先 --plan 看（这个应用有没有升级前的快照、是不是比现在老的版本、快照和那个版本的程序还在不在、磁盘够不够）。
  退回前先把现在的数据另存一份（备份类型“恢复前”，要找回升级后的新数据可以恢复它），再换回快照，等应用跑起来。退回
  没成功会自动回到退回前的样子（APP-UPG-09）；回来也出错报 APP-UPG-10（要人处理）。
副作用：暂停并重启应用；覆盖应用的数据卷；写一份“恢复前”的备份；改应用的环境变量、设置记录、程序版本。
参数：
  --plan        只说会做什么，不动任何东西
  --yes         用户同意丢掉升级后写进应用的新数据（退回必须加）
  --no-wait     只提交，输出任务编号
  --timeout 秒  等多久（默认 600，最多 3600）
输出（--json）：{"ok":true,"plan":{app,from,to,snapshot_id,loses_data,restart,can_rollback,blocked,blocked_code,
  disk_ok,notes[],summary},"task":{…}}。
退出码：0 正常；1 退回没成功；2 参数不对；4 服务器拒绝（APP-UPG-07、08、BKP-xx）。
例子：
  acornfox apps rollback demo-app --plan
  acornfox apps rollback demo-app --yes`

// upgradeAPI is the connection's upgrade operations.
func (a *app) upgradeAPI(ctx context.Context) (client.API, client.AppUpgradeAPI, client.AppCenterAPI, int) {
	t, _, err := a.resolveTarget()
	if err != nil {
		return nil, nil, nil, a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return nil, nil, nil, a.out.fail(derr)
	}
	up, ok1 := api.(client.AppUpgradeAPI)
	ac, ok2 := api.(client.AppCenterAPI)
	if !ok1 || !ok2 {
		api.Close()
		return nil, nil, nil, a.out.fail(&client.Error{Diag: client.Diagnosis{Stage: "server", Code: client.CodeServerTooOld, Message: "这个连接不支持升级应用", Hint: "升级 AcornFox"}})
	}
	return api, up, ac, -1
}

// cmdAppsOutdated implements `apps outdated`.
func (a *app) cmdAppsOutdated(ctx context.Context, args []string) int {
	fs := a.catalogFlags("apps outdated", appsOutdatedUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		return a.out.usageError("用法：acornfox apps outdated")
	}
	t, _, rerr := a.resolveTarget()
	if rerr != nil {
		return a.out.usageError("%s", rerr.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()
	// The catalog may be older than half an hour: sync first (the server
	// decides; a failure here is told, never fatal: the answer is then what
	// the catalog last synced says).
	var catSync *client.CatalogSync
	if ac, ok := api.(client.AppCenterAPI); ok {
		sctx, cancel := context.WithTimeout(ctx, outdatedSyncWait)
		if st, serr := ac.CatalogSyncIfStale(sctx); serr == nil {
			catSync = &st
		}
		cancel()
	}
	apps, serr := api.Apps(ctx)
	if serr != nil {
		return a.out.fail(serr)
	}
	type row struct {
		App         string              `json:"app"`
		CatalogID   string              `json:"catalog_id"`
		Current     string              `json:"current"`
		Available   string              `json:"available"`
		CanUpgrade  bool                `json:"can_upgrade"`
		Blocked     string              `json:"blocked"`
		BlockedCode string              `json:"blocked_code"`
		Snapshot    *client.AppSnapshot `json:"snapshot"`
		CanRollback bool                `json:"can_rollback"`
	}
	rows := []row{}
	count := 0
	for _, x := range apps {
		u := x.Upgrade
		if u == nil || (u.Available == "" && u.Blocked == "" && !u.CanRollback) {
			continue
		}
		rows = append(rows, row{App: x.Name, CatalogID: x.CatalogID, Current: x.CatalogVersion, Available: u.Available, CanUpgrade: u.CanUpgrade,
			Blocked: u.Blocked, BlockedCode: u.BlockedCode, Snapshot: u.Snapshot, CanRollback: u.CanRollback})
		if u.Available != "" {
			count++
		}
	}
	if a.out.json {
		out := map[string]any{"ok": true, "count": count, "apps": rows}
		if catSync != nil {
			out["catalog_sync"] = map[string]any{"ran": catSync.Ran, "ok": catSync.OK, "code": catSync.Code, "message": catSync.Message, "at": catSync.At}
		}
		a.out.emitJSON(out)
		return exitOK
	}
	if catSync != nil && !catSync.OK && catSync.Code != "" {
		a.out.human("注意：应用目录最近一次同步没成功（%s %s），下面按上次同步到的目录算，可能不是最新的。过几分钟再试，或看 acornfox catalog sync --json。",
			catSync.Code, catSync.Message)
	}
	if len(rows) == 0 {
		a.out.human("没有应用有新版本，也没有留着升级前快照的应用。")
		return exitOK
	}
	for _, r := range rows {
		switch {
		case r.Available != "":
			a.out.human("%s：现在 %s，应用中心有 %s", r.App, textOr(r.Current, "（不知道）"), r.Available)
			if r.Blocked != "" {
				a.out.human("  现在不能升级：%s %s", r.BlockedCode, r.Blocked)
			} else {
				a.out.human("  先看会怎么升（只读）：acornfox apps upgrade %s --plan；升级会重启它，要用户同意", r.App)
			}
		case r.Blocked != "":
			a.out.human("%s：现在 %s；%s %s", r.App, textOr(r.Current, "（不知道）"), r.BlockedCode, r.Blocked)
		default:
			a.out.human("%s：现在 %s", r.App, textOr(r.Current, "（不知道）"))
		}
		if r.Snapshot != nil {
			if r.CanRollback {
				a.out.human("  留着升级前（%s）的快照 %s，约 %d MB：退回 acornfox apps rollback %s --plan（会丢升级后的新数据）", textOr(r.Snapshot.Version, "旧版本"), r.Snapshot.ID, r.Snapshot.SizeMB, r.App)
			} else {
				a.out.human("  留着快照 %s，但现在不能用来退回", r.Snapshot.ID)
			}
		}
	}
	return exitOK
}

// upgradePlanLines prints an upgrade plan for people.
func (a *app) upgradePlanLines(p client.UpgradePlan) {
	a.out.human("%s", p.Summary)
	for _, s := range p.VarsAdded {
		a.out.human("  新增设置：%s", s)
	}
	for _, s := range p.VarsRemoved {
		a.out.human("  去掉设置：%s", s)
	}
	for _, s := range p.UsesChanged {
		a.out.human("  服务：%s", s)
	}
	for _, n := range p.Notes {
		a.out.human("  注意：%s", n)
	}
	if p.Settings != nil && len(p.Settings.Vars) > 0 {
		for _, v := range p.Settings.Vars {
			head := v.Name
			if v.Label != "" {
				head += "（" + v.Label + "）"
			}
			a.out.human("  设置 %s：%s → %s", head, textOr(v.Old, "（空）"), textOr(v.New, "（空）"))
		}
	}
	if p.Snapshot {
		a.out.human("  快照：约 %d MB，磁盘%s", p.SnapshotMB, map[bool]string{true: "够", false: "不够"}[p.DiskOK])
	}
}

// upgradeFail reports an upgrade or rollback refusal with the plan that came
// with it.
func (a *app) upgradeFail(err error, rollback bool) int {
	ce, ok := err.(*client.Error)
	if !ok || len(ce.Plan) == 0 {
		return a.out.fail(err)
	}
	if a.out.json {
		code, d := exitCodeForError(err)
		a.out.encodeRedacted(a.out.stdout, map[string]any{"ok": false, "plan": json.RawMessage(ce.Plan), "diagnosis": map[string]any{
			"stage": d.Stage, "code": d.Code, "message": a.out.redact(d.Message), "hint": a.out.redact(d.Hint)}})
		return code
	}
	if rollback {
		var p client.RollbackPlan
		_ = json.Unmarshal(ce.Plan, &p)
		a.out.human("%s", p.Summary)
	} else {
		var p client.UpgradePlan
		_ = json.Unmarshal(ce.Plan, &p)
		a.upgradePlanLines(p)
	}
	return a.out.fail(err)
}

// cmdAppsUpgrade implements `apps upgrade`.
func (a *app) cmdAppsUpgrade(ctx context.Context, args []string) int {
	fs := a.catalogFlags("apps upgrade", appsUpgradeUsage)
	plan := fs.Bool("plan", false, "只说会做什么")
	yes := fs.Bool("yes", false, "用户同意重启")
	noWait := fs.Bool("no-wait", false, "只提交")
	to := fs.String("to", "", "升到哪个版本")
	var sets setFlag
	fs.Var(&sets, "set", "新版本多出来的设置，名=值")
	var limit waitFlag
	fs.Var(&limit, "timeout", "等多久，秒")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	appName, ok := a.applyApp(pos, "acornfox apps upgrade 应用 [--plan] [--yes]")
	if !ok {
		return exitUsage
	}
	params, plat, perr := parseSets([]string(sets))
	if perr != nil {
		return a.out.usageError("%s", perr.Error())
	}
	if len(plat) > 0 {
		return a.out.usageError("升级时不能改平台变量（platform.…）：升完用 acornfox apps set 改")
	}
	req := client.AppUpgradeRequest{Plan: *plan, To: *to, ConfirmRestart: *yes}
	var in *stdinLines
	for k, v := range params {
		if s, _ := v.(string); s == "-" {
			if in == nil {
				in = a.stdinLines()
			}
			got, rerr := in.read(k)
			if rerr != nil {
				return a.out.usageError("%s（值写成 %s=- 时从标准输入给一行）", rerr.Error(), k)
			}
			a.out.addSecret(got)
			v = got
		}
		if req.Set == nil {
			req.Set = map[string]any{}
		}
		req.Set[k] = v
	}
	api, up, ac, code := a.upgradeAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	v, err := up.AppUpgrade(ctx, appName, req)
	if err != nil {
		return a.upgradeFail(err, false)
	}
	if *plan {
		if a.out.json {
			a.emitRaw(v.Raw, nil)
			return exitOK
		}
		a.upgradePlanLines(v.Plan)
		if v.Plan.CanUpgrade {
			a.out.human("做的话加 --yes（会重启 %s，要用户同意）。", v.Plan.App)
		}
		return exitOK
	}
	if v.Task == nil {
		if a.out.json {
			a.emitRaw(v.Raw, nil)
		} else {
			a.out.human("%s", v.Plan.Summary)
		}
		return exitOK
	}
	if !a.out.json {
		a.upgradePlanLines(v.Plan)
	}
	if *noWait {
		if a.out.json {
			a.emitRaw(v.Raw, map[string]any{"task_id": v.Task.ID, "next_command": "acornfox --json install status " + v.Task.ID + " --wait"})
		} else {
			a.out.human("已提交任务 %s。等它做完：acornfox install status %s --wait", v.Task.ID, v.Task.ID)
		}
		return exitOK
	}
	return a.waitInstall(ctx, ac, v.Task.ID, applyTimeout(&limit), false)
}

// cmdAppsRollback implements `apps rollback`.
func (a *app) cmdAppsRollback(ctx context.Context, args []string) int {
	fs := a.catalogFlags("apps rollback", appsRollbackUsage)
	plan := fs.Bool("plan", false, "只说会做什么")
	yes := fs.Bool("yes", false, "用户同意丢掉升级后的新数据")
	noWait := fs.Bool("no-wait", false, "只提交")
	var limit waitFlag
	fs.Var(&limit, "timeout", "等多久，秒")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	appName, ok := a.applyApp(pos, "acornfox apps rollback 应用 [--plan] [--yes]")
	if !ok {
		return exitUsage
	}
	api, up, ac, code := a.upgradeAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	v, err := up.AppRollback(ctx, appName, client.AppRollbackRequest{Plan: *plan, Confirm: *yes})
	if err != nil {
		return a.upgradeFail(err, true)
	}
	if *plan {
		if a.out.json {
			a.emitRaw(v.Raw, nil)
			return exitOK
		}
		a.out.human("%s", v.Plan.Summary)
		for _, n := range v.Plan.Notes {
			a.out.human("  注意：%s", n)
		}
		if v.Plan.CanRollback {
			a.out.human("做的话加 --yes（升级后写进 %s 的新数据会没有，要用户同意）。", v.Plan.App)
		}
		return exitOK
	}
	if v.Task == nil {
		if a.out.json {
			a.emitRaw(v.Raw, nil)
		} else {
			a.out.human("%s", v.Plan.Summary)
		}
		return exitOK
	}
	if !a.out.json {
		a.out.human("%s", v.Plan.Summary)
	}
	if *noWait {
		if a.out.json {
			a.emitRaw(v.Raw, map[string]any{"task_id": v.Task.ID, "next_command": "acornfox --json install status " + v.Task.ID + " --wait"})
		} else {
			a.out.human("已提交任务 %s。等它做完：acornfox install status %s --wait", v.Task.ID, v.Task.ID)
		}
		return exitOK
	}
	return a.waitInstall(ctx, ac, v.Task.ID, applyTimeout(&limit), false)
}

// reportUpgradeTask prints a finished (or running) upgrade or rollback task
// for people.
func (a *app) reportUpgradeTask(t client.InstallTask) {
	what := "升级"
	if t.Kind == "rollback" {
		what = "退回"
	}
	switch t.Status {
	case "running":
		a.out.human("任务 %s 进行中（%s）", t.ID, t.Step)
		a.out.human("接着等：acornfox install status %s --wait", t.ID)
	case "succeeded":
		for _, x := range t.Apps {
			if t.Kind == "rollback" {
				a.out.human("已退回：%s 现在是 %s（升级后写进去的新数据没有了）。", x.App, textOr(x.Version, "升级前的版本"))
			} else {
				a.out.human("已升级：%s %s → %s。", x.App, textOr(x.From, "旧版本"), textOr(x.Version, "新版本"))
				if x.SnapshotID != "" {
					a.out.human("  升级前的快照 %s 还留着：出问题可以 acornfox apps rollback %s --plan（退回会丢升级后的新数据）", x.SnapshotID, x.App)
				}
			}
			for _, n := range x.Notes {
				a.out.human("  - %s", n)
			}
			if len(x.Affected) > 0 {
				a.out.human("用 %s 提供的服务的应用：%s，它们的设置现在落在后面了：acornfox apps affected 看", x.App, strings.Join(x.Affected, "、"))
			}
		}
	case "failed":
		if t.Fault != nil {
			a.out.humanErr("%s没成功：%s %s", what, t.Fault.Code, t.Fault.Message)
			if t.Fault.Hint != "" {
				a.out.humanErr("建议：%s", t.Fault.Hint)
			}
		}
		a.out.humanErr("把以上内容交给 AI 处理。")
	}
}
