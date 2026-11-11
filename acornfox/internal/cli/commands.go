package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

const statusUsage = `用法：acornfox status [应用名 | 部署编号]

用途：查看一个应用的状态（网址、期望状态和观察状态、当前版本、数据卷、域名、环境变量名），
  或者一次部署的进度和诊断。
  不写参数：看 --app 或当前目录对应的应用。
  写应用名：看这个应用；和 --app 一起用时两者要一致。
  写部署编号：看这次部署。部署编号是 deploy、redeploy --no-wait 打印的 12 位编号，只有 0-9 和 a-f。
  应用名恰好也是 12 位 0-9、a-f 时，先按应用名查，没有这个应用再按部署编号查。
副作用：无（只读）。
例子：
  acornfox status hello
  acornfox status 3f2a9c0d1b7e
  acornfox --json status hello`

// deploymentIDRe matches a deployment ID as the server issues it: 12
// lowercase hex characters.
var deploymentIDRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

// cmdStatus implements `status [APP | DEPLOYMENT_ID]`. The argument is an app
// name, the same as for logs and rollback, unless it is written like a
// deployment ID; an ID that is also a valid app name is looked up as an app
// first.
func (a *app) cmdStatus(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	fs.Usage = func() { fmt.Fprintln(a.out.stdout, statusUsage) }
	rest, err := parseFlags(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if len(rest) > 1 {
		return a.out.usageError("status 最多接受一个参数（应用名或部署编号），收到 %d 个：%v", len(rest), rest)
	}
	if len(rest) == 0 {
		res, err := a.resolveTargetAndApp()
		if err != nil {
			return a.out.usageError("%s", err.Error())
		}
		api, derr := a.dial(ctx, res.target)
		if derr != nil {
			return a.out.fail(derr)
		}
		defer api.Close()
		return a.statusApp(ctx, api, res.app)
	}

	arg := rest[0]
	isDep := deploymentIDRe.MatchString(arg)
	appName, appOK := normalizeAppName(arg)
	if !isDep && !appOK {
		return a.out.usageError("%q 既不是应用名，也不是部署编号。应用名只能用小写字母、数字和连字符，以字母开头；"+
			"部署编号是 deploy、redeploy --no-wait 打印的 12 位编号（0-9、a-f）。acornfox apps 列出这台服务器上的应用", arg)
	}
	if appOK && a.flagApp != "" {
		if flagName, fok := normalizeAppName(a.flagApp); !fok || flagName != appName {
			if !isDep {
				return a.out.usageError("应用名 %s 和 --app %s 不一致，只用其中一个", arg, a.flagApp)
			}
			appOK = false // names a different app than --app: it is the deployment ID
		}
	}
	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	if !appOK {
		return a.statusDeployment(ctx, api, arg)
	}
	app, err := api.App(ctx, appName)
	if err != nil {
		if !isNotFound(err) {
			return a.out.fail(err)
		}
		if isDep {
			return a.statusDeployment(ctx, api, arg)
		}
		return a.failDiag(client.Diagnosis{
			Stage:   "status",
			Code:    "not_found",
			Message: fmt.Sprintf("这台服务器上没有叫 %s 的应用", appName),
			Hint:    "acornfox apps 列出这台服务器上的应用；要看一次部署，写 deploy 打印的 12 位部署编号",
		}, exitServerError)
	}
	return a.renderAppStatus(app)
}

// isNotFound reports whether the server answered that the object does not
// exist.
func isNotFound(err error) bool {
	var ce *client.Error
	return errors.As(err, &ce) && ce.Diag.Code == "not_found"
}

func (a *app) statusDeployment(ctx context.Context, api client.API, id string) int {
	dep, events, err := api.Deployment(ctx, id, 0)
	if err != nil {
		if isNotFound(err) {
			return a.failDiag(client.Diagnosis{
				Stage:   "status",
				Code:    "not_found",
				Message: fmt.Sprintf("这台服务器上没有编号为 %s 的部署", id),
				Hint:    "acornfox apps 列出应用；acornfox versions 应用名 列出这个应用的各次部署",
			}, exitServerError)
		}
		return a.out.fail(err)
	}
	if a.out.json {
		evs := make([]map[string]any, 0, len(events))
		for _, ev := range events {
			evs = append(evs, map[string]any{"id": ev.ID, "at": ev.At, "stage": ev.Stage, "message": ev.Message})
		}
		fields := map[string]any{
			"deployment_id": dep.ID,
			"app":           dep.App,
			"version":       dep.Seq,
			"status":        dep.Status,
			"source_kind":   dep.SourceKind,
			"events":        evs,
		}
		if dep.Diagnosis != nil {
			fields["diagnosis"] = map[string]any{
				"stage": dep.Diagnosis.Stage, "code": dep.Diagnosis.Code,
				"message": dep.Diagnosis.Message, "log_excerpt": dep.Diagnosis.LogExcerpt, "hint": dep.Diagnosis.Hint,
			}
		}
		a.out.emitJSON(fields)
		return exitOK
	}
	a.out.human("部署 %s（应用 %s，版本 %d）状态：%s", dep.ID, dep.App, dep.Seq, dep.Status)
	for _, ev := range events {
		a.out.human("[%s] %s", ev.Stage, ev.Message)
	}
	if dep.Diagnosis != nil {
		a.out.printDiagnosis(*dep.Diagnosis)
	}
	return exitOK
}

func (a *app) statusApp(ctx context.Context, api client.API, appName string) int {
	app, err := api.App(ctx, appName)
	if err != nil {
		return a.out.fail(err)
	}
	return a.renderAppStatus(app)
}

// renderAppStatus prints one app's status.
func (a *app) renderAppStatus(app client.App) int {
	envKeys := make([]map[string]any, 0, len(app.Env))
	for _, e := range app.Env {
		// Values for secrets are never returned by the server; do not print
		// any value here regardless.
		envKeys = append(envKeys, map[string]any{"key": e.Key, "secret": e.Secret})
	}
	volumes := make([]string, 0, len(app.Volumes))
	for _, v := range app.Volumes {
		volumes = append(volumes, v.Path)
	}
	domains := make([]map[string]any, 0, len(app.Domains))
	for _, d := range app.Domains {
		domains = append(domains, map[string]any{"name": d.Name, "status": d.Status})
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{
			"app":                app.Name,
			"url":                app.URL,
			"lan_url":            app.LANURL,
			"exposure":           app.Exposure,
			"desired":            app.Desired,
			"observed_state":     app.ObservedState,
			"current_deployment": app.CurrentDeployment,
			"port":               app.Port,
			"memory_mb":          app.MemoryMB,
			"cpu_milli":          app.CPUMilli,
			"health_path":        app.HealthPath,
			"volumes":            volumes,
			"env":                envKeys,
			"domains":            domains,
		})
		return exitOK
	}
	a.out.human("应用 %s", app.Name)
	if app.URL != "" {
		a.out.human("网址：%s", app.URL)
	}
	if app.LANURL != "" {
		a.out.human("局域网地址：%s", app.LANURL)
	}
	if app.Exposure != "" {
		a.out.human("暴露方式：%s", app.Exposure)
	}
	a.out.human("期望状态：%s；观察状态：%s", app.Desired, app.ObservedState)
	a.out.human("当前版本：%s", app.CurrentDeployment)
	if len(volumes) > 0 {
		a.out.human("数据卷：%s", strings.Join(volumes, ", "))
	}
	if len(app.Domains) > 0 {
		for _, d := range app.Domains {
			a.out.human("域名：%s（%s）", d.Name, d.Status)
		}
	}
	if len(app.Env) > 0 {
		keys := make([]string, 0, len(app.Env))
		for _, e := range app.Env {
			if e.Secret {
				keys = append(keys, e.Key+"(密钥)")
			} else {
				keys = append(keys, e.Key)
			}
		}
		a.out.human("环境变量：%s", strings.Join(keys, ", "))
	}
	return exitOK
}

// cmdApps implements `apps`.
func (a *app) cmdApps(ctx context.Context, args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "vars":
			return a.cmdAppsVars(ctx, args[1:])
		case "-h", "--help", "help":
			fmt.Fprintln(a.out.stdout, appsUsage)
			return exitOK
		}
		return a.out.usageError("apps 不接受参数 %s；列出应用用 acornfox apps，看一个应用的变量用 acornfox apps vars 应用", args[0])
	}
	// apps does not need an app name, only a target.
	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	apps, serr := api.Apps(ctx)
	if serr != nil {
		return a.out.fail(serr)
	}
	if a.out.json {
		list := make([]map[string]any, 0, len(apps))
		for _, ap := range apps {
			item := map[string]any{
				"app": ap.Name, "url": ap.URL, "desired": ap.Desired,
				"observed_state": ap.ObservedState, "local_only": ap.LocalOnly,
				"exposure": ap.Exposure,
			}
			if ap.OpenHint != nil {
				item["open_hint"] = ap.OpenHint
			}
			list = append(list, item)
		}
		a.out.emitJSON(map[string]any{"apps": list})
		return exitOK
	}
	if len(apps) == 0 {
		a.out.human("服务器上还没有应用。")
		return exitOK
	}
	for _, ap := range apps {
		note := ""
		if ap.LocalOnly {
			note = "\t" + localOnlyNote(ap.Name, ap.OpenHint)
		}
		a.out.human("%s\t%s\t%s%s", ap.Name, ap.ObservedState, ap.URL, note)
	}
	return exitOK
}

// localOnlyNote tells how to open a local-only app from the user's own
// computer: acornfox open APP there (an SSH tunnel, like the console);
// on a server in the user's LAN, switching to lan exposure is the other
// way, with the user's consent.
func localOnlyNote(name string, h *client.OpenHint) string {
	note := "只本机：在你自己的电脑上运行 acornfox open " + name + " 打开（经 SSH 隧道，不用放行安全组）"
	if h != nil && h.CanLAN {
		note += "；也可以经用户同意改成局域网：acornfox app set --app " + name + " --expose lan，之后用 " + h.LANURL + " 打开"
	}
	return note
}

// cmdEnv implements `env set KEY=VALUE [--secret]` / `env unset KEY` /
// `env list`.
func (a *app) cmdEnv(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.out.usageError("env 需要子命令：set / unset / list")
	}
	sub, rest := args[0], args[1:]
	res, err := a.resolveTargetAndApp()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}

	switch sub {
	case "set":
		fs := flag.NewFlagSet("env set", flag.ContinueOnError)
		fs.SetOutput(a.out.stderr)
		secret := fs.Bool("secret", false, "标记为密钥（值永不回显）")
		pairs, perr := parseFlags(fs, rest)
		if perr != nil {
			return exitUsage
		}
		if len(pairs) != 1 {
			return a.out.usageError("用法：env set KEY=VALUE [--secret]")
		}
		key, value, ok := strings.Cut(pairs[0], "=")
		if !ok || key == "" {
			return a.out.usageError("环境变量格式应为 KEY=VALUE")
		}
		// Register the value as a secret so it is never printed in any output.
		if *secret {
			a.out.addSecret(value)
		}
		api, derr := a.dial(ctx, res.target)
		if derr != nil {
			return a.out.fail(derr)
		}
		defer api.Close()
		if serr := api.SetEnv(ctx, res.app, key, value, *secret); serr != nil {
			return a.out.fail(serr)
		}
		if a.out.json {
			// Never include the value.
			a.out.emitJSON(map[string]any{"key": key, "secret": *secret, "note": "下次部署生效；应用已上线时可运行 acornfox redeploy 立即生效"})
			return exitOK
		}
		a.out.human("已设置 %s，下次部署生效；应用已上线时可运行 acornfox redeploy 立即生效。", key)
		return exitOK

	case "unset":
		if len(rest) != 1 {
			return a.out.usageError("用法：env unset KEY")
		}
		key := rest[0]
		api, derr := a.dial(ctx, res.target)
		if derr != nil {
			return a.out.fail(derr)
		}
		defer api.Close()
		if serr := api.UnsetEnv(ctx, res.app, key); serr != nil {
			return a.out.fail(serr)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"key": key, "note": "下次部署生效；应用已上线时可运行 acornfox redeploy 立即生效"})
			return exitOK
		}
		a.out.human("已删除 %s，下次部署生效；应用已上线时可运行 acornfox redeploy 立即生效。", key)
		return exitOK

	case "list":
		api, derr := a.dial(ctx, res.target)
		if derr != nil {
			return a.out.fail(derr)
		}
		defer api.Close()
		app, serr := api.App(ctx, res.app)
		if serr != nil {
			return a.out.fail(serr)
		}
		if a.out.json {
			keys := make([]map[string]any, 0, len(app.Env))
			for _, e := range app.Env {
				// Never emit secret values.
				item := map[string]any{"key": e.Key, "secret": e.Secret}
				if !e.Secret {
					item["value"] = e.Value
				}
				keys = append(keys, item)
			}
			a.out.emitJSON(map[string]any{"env": keys})
			return exitOK
		}
		if len(app.Env) == 0 {
			a.out.human("还没有环境变量。")
			return exitOK
		}
		for _, e := range app.Env {
			if e.Secret {
				a.out.human("%s=（密钥，已隐藏）", e.Key)
			} else {
				a.out.human("%s=%s", e.Key, e.Value)
			}
		}
		return exitOK

	default:
		return a.out.usageError("未知的 env 子命令：%s", sub)
	}
}

// cmdVolume implements `volume add PATH` / `volume list`.
func (a *app) cmdVolume(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.out.usageError("volume 需要子命令：add / list")
	}
	sub, rest := args[0], args[1:]
	res, err := a.resolveTargetAndApp()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	switch sub {
	case "add":
		if len(rest) != 1 {
			return a.out.usageError("用法：volume add PATH")
		}
		path := rest[0]
		if !strings.HasPrefix(path, "/") {
			return a.out.usageError("卷路径必须是绝对路径")
		}
		if serr := api.AddVolume(ctx, res.app, path); serr != nil {
			return a.out.fail(serr)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"path": path, "note": "下次部署生效"})
			return exitOK
		}
		a.out.human("已添加数据卷 %s，下次部署生效。", path)
		return exitOK

	case "list":
		if len(rest) != 0 {
			return a.out.usageError("volume list 不接受参数")
		}
		app, serr := api.App(ctx, res.app)
		if serr != nil {
			return a.out.fail(serr)
		}
		if a.out.json {
			vols := make([]map[string]any, 0, len(app.Volumes))
			for _, v := range app.Volumes {
				vols = append(vols, map[string]any{"path": v.Path, "auto": v.Auto})
			}
			a.out.emitJSON(map[string]any{"volumes": vols})
			return exitOK
		}
		if len(app.Volumes) == 0 {
			a.out.human("还没有数据卷。")
			return exitOK
		}
		for _, v := range app.Volumes {
			auto := ""
			if v.Auto {
				auto = "（自动）"
			}
			a.out.human("%s%s", v.Path, auto)
		}
		return exitOK

	default:
		return a.out.usageError("未知的 volume 子命令：%s", sub)
	}
}

// cmdApp implements `app set [--memory MB] [--cpu CORES] [--port N]
// [--health-path P] [--expose local|lan|public|default]`.
func (a *app) cmdApp(ctx context.Context, args []string) int {
	if len(args) == 0 || args[0] != "set" {
		return a.out.usageError("用法：app set [--memory MB] [--cpu CORES] [--port N] [--health-path P] [--expose local|lan|public|default]")
	}
	fs := flag.NewFlagSet("app set", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	var (
		memory     = fs.Int("memory", -1, "内存上限 MB（至少 256，不超过整机上限）")
		cpu        = fs.Float64("cpu", -1, "CPU 核数（小数，至少 0.5）")
		port       = fs.Int("port", -1, "容器端口")
		healthPath = fs.String("health-path", "\x00", "健康检查路径")
		expose     = fs.String("expose", "", "暴露方式：local（只本机）、lan（本机和局域网）、public（所有网卡 + 域名）、default（恢复本平台默认）")
	)
	if err := fs.Parse(args[1:]); err != nil {
		return exitUsage
	}

	var s client.AppSettings
	set := false
	if *memory >= 0 {
		// The upper bound is the server-wide cap, checked by the server.
		if *memory < 256 {
			return a.out.usageError("内存至少 256 MB")
		}
		m := *memory
		s.MemoryMB = &m
		set = true
	}
	if *cpu >= 0 {
		milli := int(*cpu*1000 + 0.5)
		if milli < 500 {
			return a.out.usageError("CPU 至少 0.5 核")
		}
		s.CPUMilli = &milli
		set = true
	}
	if *port >= 0 {
		p := *port
		s.Port = &p
		set = true
	}
	if *healthPath != "\x00" {
		hp := *healthPath
		s.HealthPath = &hp
		set = true
	}
	if *expose != "" {
		e := *expose
		switch e {
		case "default":
			e = ""
		case "local", "lan", "public", "afx_cloud":
		default:
			return a.out.usageError("--expose 只能是 local、lan、public 或 default")
		}
		s.Exposure = &e
		set = true
	}
	if !set {
		return a.out.usageError("至少提供一个设置：--memory / --cpu / --port / --health-path / --expose")
	}

	res, err := a.resolveTargetAndApp()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	u, serr := api.UpdateApp(ctx, res.app, s)
	if serr != nil {
		return a.out.fail(serr)
	}
	updated := u.App
	// Memory / CPU limits are applied to the running container in place;
	// port and health path still need a redeploy.
	limits := s.MemoryMB != nil || s.CPUMilli != nil
	others := s.Port != nil || s.HealthPath != nil
	note := "下次部署生效；应用已上线时可运行 acornfox redeploy 立即生效"
	switch {
	case s.Exposure != nil && !limits && !others:
		note = "暴露方式已改为 " + updated.Exposure + "，几秒内生效，不用重新部署"
		if updated.LANURL != "" {
			note += "；局域网地址 " + updated.LANURL
		}
	case limits && u.AppliedNow && !others:
		note = "内存和 CPU 上限已立即生效，应用没有重启"
	case limits && u.AppliedNow:
		note = "内存和 CPU 上限已立即生效；端口和健康检查路径下次部署生效，可运行 acornfox redeploy"
	case u.ApplyError != "":
		note = u.ApplyError
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{
			"app": updated.Name, "memory_mb": updated.MemoryMB, "cpu_milli": updated.CPUMilli,
			"port": updated.Port, "health_path": updated.HealthPath, "applied_now": u.AppliedNow, "note": note,
		})
		return exitOK
	}
	a.out.human("已更新 %s：内存上限 %d MB，CPU 上限 %.1f 核。%s。", updated.Name, updated.MemoryMB, float64(updated.CPUMilli)/1000, note)
	return exitOK
}

// cmdStop implements `stop [APP] [--no-wait]`.
func (a *app) cmdStop(ctx context.Context, args []string) int {
	return a.appLifecycle(ctx, args, "stop")
}

// cmdStart implements `start [APP] [--no-wait]`.
func (a *app) cmdStart(ctx context.Context, args []string) int {
	return a.appLifecycle(ctx, args, "start")
}

// cmdRestart implements `restart [APP] [--no-wait]`.
func (a *app) cmdRestart(ctx context.Context, args []string) int {
	return a.appLifecycle(ctx, args, "restart")
}

const deleteUsage = `用法：acornfox delete [应用名] [--volumes]

用途：删除一个应用：停掉并删掉它的容器、网络和记录。不写应用名时，用 --app 或当前目录对应的应用。
  默认保留数据卷（以后同名重装会被 CAT-INST-24 拦下，先问用户旧数据还要不要）；加 --volumes 连数据一起删，不能恢复。
  从应用中心装的、用了本机 New API 的应用（例如 Hermes）：加 --volumes 时顺带吊销给它发的令牌；
  保留数据时令牌和设置跟着数据留下，以后 install 应用 --reuse-data 接上旧数据时接着用；
  被别的应用连着的模型网关（例如 Hermes 还在用的 New API）拒绝删除，先删用它的应用。
副作用：会删数据，执行前必须得到用户明确同意。
例子：
  acornfox delete hermes-agent
  acornfox delete new-api --volumes
  acornfox --json delete hermes-agent`

// cmdDelete implements `delete [app] [--volumes]`.
func (a *app) cmdDelete(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	fs.Usage = func() { fmt.Fprintln(a.out.stdout, deleteUsage) }
	deleteVolumes := fs.Bool("volumes", false, "同时删除数据卷（默认保留）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	// The app named on the command line wins; without one it is --app or the
	// current directory's app. A name used to be ignored here, so
	// "delete web" in a directory called "api" deleted api.
	res, err := a.resolveAppArg("delete", pos)
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	// The server may warn (a model-gateway token of an app-center app that
	// could not be revoked); older servers and test fakes have no warnings.
	var warnings []string
	if dw, ok := api.(interface {
		DeleteAppWarnings(ctx context.Context, app string, deleteVolumes bool) ([]string, error)
	}); ok {
		w, serr := dw.DeleteAppWarnings(ctx, res.app, *deleteVolumes)
		if serr != nil {
			return a.out.fail(serr)
		}
		warnings = w
	} else if serr := api.DeleteApp(ctx, res.app, *deleteVolumes); serr != nil {
		return a.out.fail(serr)
	}
	if a.out.json {
		body := map[string]any{
			"app":             res.app,
			"deleted":         true,
			"volumes_deleted": *deleteVolumes,
		}
		if len(warnings) > 0 {
			body["warnings"] = warnings
		}
		a.out.emitJSON(body)
		return exitOK
	}
	for _, w := range warnings {
		a.out.humanErr("注意：%s", w)
	}
	if *deleteVolumes {
		a.out.human("已删除应用 %s 及其所有数据卷。", res.app)
	} else {
		a.out.human("已删除应用 %s（数据卷已保留）。", res.app)
	}
	return exitOK
}

// lifecycleUsage is the -h text of stop, start and restart.
func lifecycleUsage(action string) string {
	what := map[string]string{"stop": "停止", "start": "启动", "restart": "重启"}[action]
	wait := "等到容器真的运行起来（观察状态 running）才返回；等了 60 秒还没起来就报 start_timeout 并退出 1，按提示看日志。"
	if action == "stop" {
		wait = "等到容器真的停下（观察状态 stopped）才返回；30 秒还没停下就说明仍在停止中。"
	}
	if action == "restart" {
		wait = "重启后等到容器运行起来（观察状态 running）才返回；60 秒还没起来就报 start_timeout 并退出 1，按提示看日志。"
	}
	return fmt.Sprintf(`用法：acornfox %[1]s [应用名] [--no-wait]

用途：%[2]s一个应用。不写应用名时，用 --app 或当前目录对应的应用；写了应用名又写 --app 时两者要一致。
  %[3]s
  返回后用 acornfox status 应用名 或 acornfox apps 看到的就是新状态。
  --no-wait 只提交请求，不等。
副作用：%[2]s应用的容器，不动数据。
例子：
  acornfox %[1]s hello
  acornfox --json %[1]s hello
  acornfox %[1]s hello --no-wait`, action, what, wait)
}

// appLifecycle runs stop, start or restart and, unless --no-wait, waits until
// the container is observed in the new state, so `apps` right after the
// command already shows it.
func (a *app) appLifecycle(ctx context.Context, args []string, action string) int {
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	fs.Usage = func() { fmt.Fprintln(a.out.stdout, lifecycleUsage(action)) }
	noWait := fs.Bool("no-wait", false, "只提交请求，不等容器状态变化")
	pos, err := parseFlags(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	res, err := a.resolveAppArg(action, pos)
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	var serr error
	switch action {
	case "stop":
		serr = api.Stop(ctx, res.app)
	case "start":
		serr = api.Start(ctx, res.app)
	default:
		serr = api.Restart(ctx, res.app)
	}
	if serr != nil {
		return a.out.fail(serr)
	}
	done := map[string]string{"stop": "已停止", "start": "已启动", "restart": "已重启"}[action]
	what := map[string]string{"stop": "停止", "start": "启动", "restart": "重启"}[action]
	if *noWait {
		if a.out.json {
			body := map[string]any{"app": res.app, "action": action, "waited": false}
			if action == "restart" {
				body["restarted"] = true // kept from the earlier output of restart
			}
			a.out.emitJSON(body)
			return exitOK
		}
		a.out.human("已提交%s请求（没有等）。用 acornfox status %s 看观察状态。", what, res.app)
		return exitOK
	}

	if action == "stop" {
		// The server only records the desired state; the container stops on
		// the next reconcile. Report success once it is actually down.
		observed, stopped, werr := waitStopped(ctx, api, res.app)
		if werr != nil {
			return a.out.fail(werr)
		}
		if a.out.json {
			a.out.emitJSON(map[string]any{"app": res.app, "action": action, "waited": true, "stopped": stopped, "observed_state": observed})
			return exitOK
		}
		if stopped {
			a.out.human("已停止应用 %s。", res.app)
		} else {
			a.out.human("已提交停止请求，应用 %s 仍在停止中（当前状态：%s）。稍后用 acornfox status %s 查看。", res.app, observedOrUnknown(observed), res.app)
		}
		return exitOK
	}

	// start records the desired state and restart restarts the container;
	// either way the observed state catches up a moment later.
	w, werr := waitRunning(ctx, api, res.app)
	if werr != nil {
		return a.out.fail(werr)
	}
	switch {
	case w.running:
		if a.out.json {
			body := map[string]any{"app": res.app, "action": action, "waited": true, "running": true, "observed_state": w.observed}
			if action == "restart" {
				body["restarted"] = true // kept from the earlier output of restart
			}
			a.out.emitJSON(body)
			return exitOK
		}
		a.out.human("%s应用 %s（观察状态：running）。", done, res.app)
		return exitOK
	case w.noDeployment:
		return a.failDiag(client.Diagnosis{
			Stage:   action,
			Code:    "no_deployment",
			Message: fmt.Sprintf("应用 %s 还没有上线过的版本，没有容器可以%s", res.app, what),
			Hint:    fmt.Sprintf("先部署：acornfox deploy，或 acornfox redeploy %s；acornfox versions %s 看各次部署为什么没上线", res.app, res.app),
		}, exitOpFailed)
	case ctx.Err() != nil:
		return a.failDiag(client.Diagnosis{
			Stage:   action,
			Code:    "cancelled",
			Message: fmt.Sprintf("已提交%s请求，等待被打断，还没确认应用 %s 运行起来（观察状态：%s）", what, res.app, observedOrUnknown(w.observed)),
			Hint:    fmt.Sprintf("acornfox status %s 看观察状态，不用再提交一次", res.app),
		}, exitOpFailed)
	default:
		return a.failDiag(client.Diagnosis{
			Stage:   action,
			Code:    "start_timeout",
			Message: fmt.Sprintf("已提交%s请求，但等了 %d 秒应用 %s 还没有运行起来（观察状态：%s）", what, int(startWaitTimeout/time.Second), res.app, w.observed),
			Hint:    fmt.Sprintf("acornfox logs %s 看日志，找它起不来或反复退出的原因；acornfox status %s 看现在的状态", res.app, res.app),
		}, exitOpFailed)
	}
}

// stopPollInterval, stopWaitTimeout and startWaitTimeout bound how long
// `stop` waits for the container to go down and `start`/`restart` for it to
// run; variables so tests can shorten them.
var (
	stopPollInterval = 500 * time.Millisecond
	stopWaitTimeout  = 30 * time.Second
	startWaitTimeout = 60 * time.Second
)

// observedOrUnknown names an observed state for a message; "" means the wait
// was cut off before the first answer.
func observedOrUnknown(s string) string {
	if s == "" {
		return "未知"
	}
	return s
}

// waitStopped polls the app until its observed state is stopped or missing.
// It returns the last observed state and whether the app stopped in time. A
// request cut off by a cancelled ctx (Ctrl-C, SSH dropped) is not an error:
// the stop was already submitted, so the caller reports "still stopping".
func waitStopped(ctx context.Context, api client.API, appName string) (string, bool, error) {
	deadline := time.Now().Add(stopWaitTimeout)
	last := ""
	for {
		app, err := api.App(ctx, appName)
		if err != nil {
			if ctx.Err() != nil {
				return last, false, nil
			}
			return "", false, err
		}
		last = app.ObservedState
		if app.ObservedState == "stopped" || app.ObservedState == "missing" {
			return app.ObservedState, true, nil
		}
		if time.Now().After(deadline) {
			return app.ObservedState, false, nil
		}
		select {
		case <-ctx.Done():
			return app.ObservedState, false, nil
		case <-time.After(stopPollInterval):
		}
	}
}

// runWait is the outcome of waitRunning.
type runWait struct {
	observed     string // last observed state
	running      bool   // observed running in time
	noDeployment bool   // the app has no live version, so nothing can run
}

// waitRunning polls the app until its observed state is running. An app
// without a live deployment has no container to start; that is reported at
// once instead of waiting for the timeout. A request cut off by a cancelled
// ctx returns the last observed state without an error, so the caller reports
// "cancelled" (the request was submitted) rather than a connection failure.
func waitRunning(ctx context.Context, api client.API, appName string) (runWait, error) {
	deadline := time.Now().Add(startWaitTimeout)
	last := ""
	for {
		app, err := api.App(ctx, appName)
		if err != nil {
			if ctx.Err() != nil {
				return runWait{observed: last}, nil
			}
			return runWait{}, err
		}
		last = app.ObservedState
		if app.ObservedState == "running" {
			return runWait{observed: app.ObservedState, running: true}, nil
		}
		if app.CurrentDeployment == "" {
			return runWait{observed: app.ObservedState, noDeployment: true}, nil
		}
		if time.Now().After(deadline) {
			return runWait{observed: app.ObservedState}, nil
		}
		select {
		case <-ctx.Done():
			return runWait{observed: app.ObservedState}, nil
		case <-time.After(stopPollInterval):
		}
	}
}
