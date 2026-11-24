package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// `acornfox apps set|reapply|affected`: change an installed app's settings
// and apply them again, and see which apps have fallen behind. The server
// decides everything (PATCH /v1/apps/{app}/vars, POST /v1/apps/{app}/reapply,
// GET /v1/platform/affected: internal/appinstall/reapply.go); the CLI only
// asks, shows the plan and waits for the task.

const appsSetUsage = `用法：acornfox apps set 应用 名=值 [名=值]... [--reset platform.名]... [--plan] [--yes] [--force]
                              [--no-wait] [--timeout 秒]

用途：改一个从应用中心装的应用的设置，并让它生效：重新算它的环境变量、启动命令和配置文件，有变化就把它的容器
  用同一个镜像重建（重启，数据不动），健康检查通过后做清单里写的“重新生效后的步骤”。哪一项能改看
  acornfox apps vars 应用（“能不能改”一列、can_edit）。
名=值：  应用自己的设置写 参数名=新值（密码类的值写 参数名=- 从标准输入读一行，不会出现在命令行和日志里）；
         只改这个应用的平台变量写 platform.名字=新值（例如 platform.tz=Asia/Tokyo，acornfox catalog platform-vars
         看哪些能改）。--reset platform.名字 去掉这个应用自己的设置，回到跟着本机。
流程：先算出会改什么（--plan 只做这一步，什么都不改）。会重启应用的，要用户同意后加 --yes 才做（不加 --yes 报
  APP-APPLY-05，什么都没改）。做的时候写日志式的进度，失败会自动退回原来的环境变量、配置文件和容器
  （APP-APPLY-03），退回也出错报 APP-APPLY-04（要人处理）。
副作用：写应用的环境变量、启动命令、数据卷里的配置文件（清单里写了 overwrite: install 或 never 的不碰），重建
  容器（应用的版本历史里多一个“改设置后重新生效”）。不删数据。重复运行没问题：没有变化就什么都不做。
参数：
  --plan        只说会改什么、会不会重启，不改任何东西
  --yes         用户同意重启这个应用
  --force       设置没变也重新生效一次（重启，重做“每次重新生效后的步骤”）
  --reset 名    去掉这个应用自己的平台变量设置（可以写几次）
  --no-wait     只提交，输出任务编号；之后 acornfox install status 任务编号 --wait
  --timeout 秒  等多久（默认 600，最多 3600）
输出（--json）：{"ok":true,"changed":…,"plan":{app,needed,restart,vars[],env[],env_removed[],command,files[],steps[],
  notes[],summary,dependents[]},"task":{…}}；等完以后是任务的结果（同 install status --json）。
退出码：0 正常；1 重新生效失败（已退回，看 diagnosis）；2 参数不对；4 服务器拒绝（APP-APPLY-01、02、05 等）。
例子：
  acornfox apps set demo-app model=qwen-plus --plan
  acornfox apps set demo-app model=qwen-plus --yes
  acornfox apps set demo-app platform.tz=Asia/Tokyo --yes
  acornfox --json apps set demo-app admin_password=- --yes`

const appsReapplyUsage = `用法：acornfox apps reapply 应用 [--plan] [--yes] [--force] [--no-wait] [--timeout 秒]
      acornfox apps reapply --all [--var 变量] [--plan] [--yes] [--timeout 秒]

用途：让应用用上现在的值：平台变量变了（换了时区、下载源，设了平台域名）、接上的服务变了（提供方换了密码）、
  应用的对外方式或域名变了，应用里用的还是旧的（acornfox apps vars 里 APP-VARS-01）。流程、副作用、
  失败自动退回都和 acornfox apps set 一样（acornfox apps set -h）。
  --all：把 acornfox apps affected 列出来的应用一个一个重新生效（前一个成功才做下一个，第一个失败就停）；
  --var 只做用到这个变量的那几个。
参数：
  --plan       只说会改什么，不改任何东西
  --yes        用户同意重启这些应用（会重启的应用必须加）
  --force      没有变化也重新生效一次
  --all        对所有落在后面的应用
  --var 变量   platform.名字、uses.需要名.项 或 service:服务名（配合 --all，或单独看谁用它用 apps affected）
  --no-wait    只提交（--all 不能用）
  --timeout 秒 每个应用最多等多久（默认 600，最多 3600）
输出（--json）：同 acornfox apps set；--all 时是 {"ok":…,"apps":[每个应用的结果]}。
退出码：同 acornfox apps set。
例子：
  acornfox apps reapply demo-app --plan
  acornfox apps reapply demo-app --yes
  acornfox apps reapply --all --plan
  acornfox apps reapply --all --var platform.tz --yes`

const appsAffectedUsage = `用法：acornfox apps affected [--var 变量]

用途：看哪些应用的设置落在后面了（和现在平台算出来的、接上的服务给的不一样），每个应用哪几项不一样、重新生效会不会
  重启它。平台变量变了、服务变了、设了平台域名之后先看这个，再决定要不要一个个重新生效
  （acornfox apps reapply 应用，会重启应用，先问用户）。
  加 --var 看哪些应用用到这个变量（不管是不是已经不一样），换之前就知道会碰到谁：
  platform.tz、uses.需要名.项、service:服务名。
副作用：无，只读。
输出（--json）：{"ok":true,"var":…,"apps":[{app,catalog_id,uses,differs,vars[],restart,blocked,summary}],"notes":[…]}
退出码：0 正常；2 参数不对；3 连不上服务器；4 服务器拒绝。
例子：
  acornfox apps affected
  acornfox apps affected --var platform.tz
  acornfox --json apps affected`

// applyAPI is the connection's apply operations.
func (a *app) applyAPI(ctx context.Context) (client.API, client.AppApplyAPI, client.AppCenterAPI, int) {
	t, _, err := a.resolveTarget()
	if err != nil {
		return nil, nil, nil, a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return nil, nil, nil, a.out.fail(derr)
	}
	ap, ok1 := api.(client.AppApplyAPI)
	ac, ok2 := api.(client.AppCenterAPI)
	if !ok1 || !ok2 {
		api.Close()
		return nil, nil, nil, a.out.fail(&client.Error{Diag: client.Diagnosis{Stage: "server", Code: client.CodeServerTooOld, Message: "这个连接不支持改设置和重新生效", Hint: "升级 AcornFox"}})
	}
	return api, ap, ac, -1
}

// applyApp is the app of a command: the positional argument or --app.
func (a *app) applyApp(pos []string, usage string) (string, bool) {
	name := a.flagApp
	switch {
	case len(pos) > 0 && name != "" && name != pos[0]:
		a.out.usageError("应用名给了两个不一样的：%s 和 --app %s", pos[0], name)
		return "", false
	case len(pos) > 0:
		name = pos[0]
	case name == "":
		a.out.usageError("要给应用名：%s（acornfox apps 看有哪些）", usage)
		return "", false
	}
	app, ok := normalizeAppName(name)
	if !ok {
		a.out.usageError("应用名 %q 不合法；只能用小写字母、数字和连字符", name)
		return "", false
	}
	return app, true
}

// planLines prints a plan for people.
func (a *app) planLines(p client.ApplyPlan) {
	a.out.human("%s", p.Summary)
	for _, v := range p.Vars {
		who := map[string]string{"user": "你改的", "platform": "平台上变了", "service": "接的服务变了", "upgrade": "新版本"}[v.From]
		if who != "" {
			who = "（" + who + "）"
		}
		head := v.Name
		if v.Label != "" {
			head += "（" + v.Label + "）"
		}
		a.out.human("  %s：%s → %s%s", head, textOr(v.Old, "（空）"), textOr(v.New, "（空）"), who)
	}
	if len(p.Env) > 0 {
		a.out.human("  改写的环境变量：%s", strings.Join(p.Env, "、"))
	}
	if len(p.EnvRemoved) > 0 {
		a.out.human("  去掉的环境变量：%s", strings.Join(p.EnvRemoved, "、"))
	}
	if p.Command {
		a.out.human("  启动命令会变")
	}
	if len(p.Files) > 0 {
		a.out.human("  重写的配置文件：%s", strings.Join(p.Files, "、"))
	}
	for _, s := range p.Steps {
		what := "每次重新生效后"
		if s.When == "on_change" {
			what = "看的设置（" + strings.Join(s.Watch, "、") + "）变了后"
		}
		a.out.human("  在应用里做的步骤（%s）：%s", what, strings.Join(s.Names, "、"))
	}
	if len(p.Dependents) > 0 {
		a.out.human("  用它提供的服务的应用：%s——它们之后会落在后面，用 acornfox apps affected 看、acornfox apps reapply 一个个对上", strings.Join(p.Dependents, "、"))
	}
	for _, n := range p.Notes {
		a.out.human("  注：%s", n)
	}
}

func textOr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// applyFail reports a refusal of an apply; the plan an APP-APPLY-05 brings
// is shown with it.
func (a *app) applyFail(err error) int {
	ce, ok := err.(*client.Error)
	if !ok || len(ce.Plan) == 0 {
		return a.out.fail(err)
	}
	var plan client.ApplyPlan
	_ = json.Unmarshal(ce.Plan, &plan)
	if a.out.json {
		code, d := exitCodeForError(err)
		a.out.encodeRedacted(a.out.stdout, map[string]any{"ok": false, "plan": json.RawMessage(ce.Plan), "diagnosis": map[string]any{
			"stage": d.Stage, "code": d.Code, "message": a.out.redact(d.Message), "hint": a.out.redact(d.Hint)}})
		return code
	}
	a.planLines(plan)
	return a.out.fail(err)
}

// answerApply shows what an apply answered and waits for its task. plan: it
// was only a plan.
func (a *app) answerApply(ctx context.Context, ac client.AppCenterAPI, v client.AppApply, plan, noWait bool, timeout time.Duration) int {
	if plan {
		if a.out.json {
			a.emitRaw(v.Raw, nil)
			return exitOK
		}
		a.planLines(v.Plan)
		if v.Plan.Needed {
			if v.Plan.Restart {
				a.out.human("做的话加 --yes（会重启 %s，要用户同意）。", v.Plan.App)
			} else {
				a.out.human("做的话去掉 --plan（不用重启）。")
			}
		}
		return exitOK
	}
	if v.Task == nil {
		if a.out.json {
			a.emitRaw(v.Raw, nil)
			return exitOK
		}
		a.out.human("%s", v.Plan.Summary)
		return exitOK
	}
	if !a.out.json {
		a.planLines(v.Plan)
	}
	if noWait {
		if a.out.json {
			a.emitRaw(v.Raw, map[string]any{"task_id": v.Task.ID, "next_command": "acornfox --json install status " + v.Task.ID + " --wait"})
		} else {
			a.out.human("已提交任务 %s。等它做完：acornfox install status %s --wait", v.Task.ID, v.Task.ID)
		}
		return exitOK
	}
	return a.waitInstall(ctx, ac, v.Task.ID, timeout, false)
}

// timeoutFlag is --timeout of set and reapply.
func applyTimeout(w *waitFlag) time.Duration {
	if !w.set {
		return 600 * time.Second
	}
	return min(w.d, 3600*time.Second)
}

// cmdAppsSet implements `apps set`.
func (a *app) cmdAppsSet(ctx context.Context, args []string) int {
	fs := a.catalogFlags("apps set", appsSetUsage)
	plan := fs.Bool("plan", false, "只说会改什么")
	yes := fs.Bool("yes", false, "用户同意重启")
	force := fs.Bool("force", false, "没变也重新生效一次")
	noWait := fs.Bool("no-wait", false, "只提交")
	var reset setFlag
	fs.Var(&reset, "reset", "去掉这个应用自己的平台变量设置")
	var limit waitFlag
	fs.Var(&limit, "timeout", "等多久，秒")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	usage := "acornfox apps set 应用 名=值 [--plan] [--yes]"
	if len(pos) == 0 && a.flagApp == "" {
		return a.out.usageError("要给应用名和要改的设置：%s", usage)
	}
	// The app is the first positional when it has no "=": acornfox apps set app a=b.
	var name string
	var sets []string
	for i, p := range pos {
		if i == 0 && !strings.Contains(p, "=") {
			name = p
			continue
		}
		sets = append(sets, p)
	}
	appName, ok := a.applyApp(nonEmpty(name), usage)
	if !ok {
		return exitUsage
	}
	if len(sets) == 0 && len(reset) == 0 {
		return a.out.usageError("要改哪一项？写 名=值（%s）", usage)
	}
	params, plat, err := parseSets(sets)
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	req := client.AppSetRequest{Set: map[string]any{}, Plan: *plan, Force: *force, ConfirmRestart: *yes, Reset: []string(reset)}
	var in *stdinLines
	for k, v := range params {
		if s, _ := v.(string); s == "-" {
			// The value comes from standard input: a password never goes in
			// the command line or the history.
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
		req.Set[k] = v
	}
	for k, v := range plat {
		req.Set["platform."+k] = v
	}
	api, ap, ac, code := a.applyAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	v, err := ap.AppSet(ctx, appName, req)
	if err != nil {
		return a.applyFail(err)
	}
	return a.answerApply(ctx, ac, v, *plan, *noWait, applyTimeout(&limit))
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// cmdAppsReapply implements `apps reapply`.
func (a *app) cmdAppsReapply(ctx context.Context, args []string) int {
	fs := a.catalogFlags("apps reapply", appsReapplyUsage)
	plan := fs.Bool("plan", false, "只说会改什么")
	yes := fs.Bool("yes", false, "用户同意重启")
	force := fs.Bool("force", false, "没变也重新生效一次")
	noWait := fs.Bool("no-wait", false, "只提交")
	all := fs.Bool("all", false, "所有落在后面的应用")
	varName := fs.String("var", "", "只做用到这个变量的")
	var limit waitFlag
	fs.Var(&limit, "timeout", "等多久，秒")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	api, ap, ac, code := a.applyAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	if *all {
		if len(pos) > 0 || a.flagApp != "" {
			return a.out.usageError("--all 不能再指定应用")
		}
		if *noWait {
			return a.out.usageError("--all 要一个做完再做下一个，不能 --no-wait")
		}
		return a.reapplyAll(ctx, ap, ac, *varName, *plan, *yes, *force, applyTimeout(&limit))
	}
	if *varName != "" {
		return a.out.usageError("--var 要和 --all 一起用；看谁用到这个变量用 acornfox apps affected --var 变量")
	}
	appName, ok := a.applyApp(pos, "acornfox apps reapply 应用 [--plan] [--yes]")
	if !ok {
		return exitUsage
	}
	v, err := ap.AppReapply(ctx, appName, client.AppReapplyRequest{Plan: *plan, Force: *force, ConfirmRestart: *yes})
	if err != nil {
		return a.applyFail(err)
	}
	return a.answerApply(ctx, ac, v, *plan, *noWait, applyTimeout(&limit))
}

// reapplyAll applies, one after the other, the apps that are behind.
func (a *app) reapplyAll(ctx context.Context, ap client.AppApplyAPI, ac client.AppCenterAPI, name string, plan, yes, force bool, timeout time.Duration) int {
	list, err := ap.PlatformAffected(ctx, name)
	if err != nil {
		return a.out.fail(err)
	}
	var todo []client.AffectedApp
	for _, x := range list.Apps {
		if x.Blocked != "" {
			continue
		}
		if name != "" && !x.Differs && !force {
			continue
		}
		todo = append(todo, x)
	}
	if len(todo) == 0 {
		if a.out.json {
			a.out.emitJSON(map[string]any{"ok": true, "apps": []any{}, "notes": append([]string{"没有要重新生效的应用"}, list.Notes...)})
		} else {
			a.out.human("没有要重新生效的应用。")
		}
		return exitOK
	}
	if !plan && !yes {
		for _, x := range todo {
			a.out.human("%s", x.Summary)
		}
		return a.out.fail(&client.Error{Status: 409, Diag: client.Diagnosis{Stage: "server", Code: "APP-APPLY-05",
			Message: fmt.Sprintf("要一个个重新生效 %d 个应用，其中会重启的要用户同意；先 acornfox apps reapply --all --plan 看会改什么，同意后加 --yes", len(todo)),
			Hint:    "把上面每个应用会改什么告诉用户，同意后加 --yes 再运行"}})
	}
	var results []any
	worst := exitOK
	for _, x := range todo {
		v, err := ap.AppReapply(ctx, x.App, client.AppReapplyRequest{Plan: plan, Force: force, ConfirmRestart: yes})
		if err != nil {
			worst = a.applyFail(err)
			results = append(results, map[string]any{"app": x.App, "ok": false, "error": err.Error()})
			break
		}
		if !a.out.json {
			a.out.human("—— %s ——", x.App)
		}
		if plan || v.Task == nil {
			rc := a.answerApply(ctx, ac, v, plan, false, timeout)
			results = append(results, json.RawMessage(v.Raw))
			if rc != exitOK {
				worst = rc
				break
			}
			continue
		}
		rc := a.waitReapplyQuiet(ctx, ac, v, timeout)
		results = append(results, map[string]any{"app": x.App, "ok": rc == exitOK, "task_id": v.Task.ID})
		if rc != exitOK {
			worst = rc
			break
		}
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"ok": worst == exitOK, "apps": results})
	}
	return worst
}

// waitReapplyQuiet waits for a task and prints its result for people, the
// JSON of --all being the one list at the end.
func (a *app) waitReapplyQuiet(ctx context.Context, ac client.AppCenterAPI, v client.AppApply, timeout time.Duration) int {
	saved := a.out.json
	a.out.json = false
	defer func() { a.out.json = saved }()
	a.planLines(v.Plan)
	return a.waitInstall(ctx, ac, v.Task.ID, timeout, false)
}

// cmdAppsAffected implements `apps affected`.
func (a *app) cmdAppsAffected(ctx context.Context, args []string) int {
	fs := a.catalogFlags("apps affected", appsAffectedUsage)
	varName := fs.String("var", "", "看用到这个变量的应用")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		return a.out.usageError("用法：acornfox apps affected [--var 变量]")
	}
	api, ap, _, code := a.applyAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	v, err := ap.PlatformAffected(ctx, *varName)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(v.Raw, nil)
		return exitOK
	}
	for _, x := range v.Apps {
		switch {
		case x.Blocked != "":
			a.out.human("%s：现在不能重新生效：%s", x.App, x.Blocked)
		case x.Differs:
			a.out.human("%s", x.Summary)
			for _, vv := range x.Vars {
				a.out.human("  %s：%s → %s", vv.Name, textOr(vv.Old, "（空）"), textOr(vv.New, "（空）"))
			}
			a.out.human("  让它跟上：acornfox apps reapply %s --plan（先看），用户同意后 acornfox apps reapply %s --yes", x.App, x.App)
		default:
			a.out.human("%s：用到 %s，现在的值和应用里用的一致", x.App, *varName)
		}
	}
	for _, n := range v.Notes {
		a.out.human("%s", n)
	}
	return exitOK
}

// reportReapply prints a finished reapply task for people.
func (a *app) reportReapply(t client.InstallTask) {
	switch t.Status {
	case "running":
		a.out.human("任务 %s 进行中（%s）", t.ID, t.Step)
		a.out.human("接着等：acornfox install status %s --wait", t.ID)
	case "succeeded":
		a.out.human("已重新生效。")
		for _, x := range t.Apps {
			if len(x.Affected) > 0 {
				a.out.human("用 %s 提供的服务的应用：%s，它们的设置现在落在后面了：acornfox apps affected 看，acornfox apps reapply 应用 一个个对上（会重启它们，先问用户）", x.App, strings.Join(x.Affected, "、"))
			}
		}
	case "failed":
		if t.Fault != nil {
			a.out.humanErr("重新生效没成功：%s %s", t.Fault.Code, t.Fault.Message)
			if t.Fault.Hint != "" {
				a.out.humanErr("建议：%s", t.Fault.Hint)
			}
		}
		a.out.humanErr("把以上内容交给 AI 处理。")
	}
}

// followApply waits for the apply that followed a change of an app's address
// and prints how it went (nil: nothing followed). With json, out is the
// answer of the command that made the change and the task's result goes in
// it under "reapply".
func (a *app) followApply(ctx context.Context, api client.API, f *client.ApplyFollow, out map[string]any) int {
	if f == nil {
		return exitOK
	}
	if f.Task == nil {
		if f.Note != "" {
			if a.out.json {
				out["reapply_note"] = f.Note
				a.out.emitJSON(out)
			} else {
				a.out.human("%s", f.Note)
			}
		}
		return exitOK
	}
	ac, ok := api.(client.AppCenterAPI)
	if !ok {
		return exitOK
	}
	if !a.out.json {
		a.planLines(f.Plan)
		return a.waitInstall(ctx, ac, f.Task.ID, 600*time.Second, false)
	}
	saved := a.out.stdout
	var buf strings.Builder
	a.out.stdout = &buf
	code := a.waitInstall(ctx, ac, f.Task.ID, 600*time.Second, false)
	a.out.stdout = saved
	var res map[string]any
	_ = json.Unmarshal([]byte(buf.String()), &res)
	out["reapply_result"] = res
	a.out.emitJSON(out)
	return code
}
