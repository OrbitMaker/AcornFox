package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// rollbackHints adds the next step to server refusals of a rollback; the
// server message already says what is wrong.
var rollbackHints = map[string]string{
	"deploy_in_progress":       "确认要取消，就加上 --cancel-pending 再执行",
	"version_image_gone":       "用 acornfox versions 查看可回退的版本，或重新部署那一版的代码",
	"version_not_rollbackable": "用 acornfox versions 查看可回退的版本",
	"version_not_found":        "用 acornfox versions 查看有哪些版本",
	"no_rollback_target":       "用 acornfox versions 查看版本记录",
}

// cmdRollback implements `rollback [APP] [--to N] [--cancel-pending]
// [--no-wait] [--timeout D]` and waits to a terminal status.
func (a *app) cmdRollback(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("rollback", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	var (
		to            = fs.Int("to", 0, "回退到第 N 版（acornfox versions 查看哪些版本可回退）；不带时回到上一个可回退的版本")
		cancelPending = fs.Bool("cancel-pending", false, "有部署正在进行时，取消它再回退")
		noWait        = fs.Bool("no-wait", false, "提交后不等待终态")
		timeout       = fs.Duration("timeout", 20*time.Minute, "等待终态的超时时间")
	)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	toSet := flagWasSet(fs, "to")
	if toSet && *to < 1 {
		return a.out.usageError("--to 必须是正整数（第几版）")
	}
	res, err := a.resolveAppArg("rollback", pos)
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	if toSet {
		if code, ok := a.requireFeature(ctx, api, client.FeatureRollbackTo, "回退到指定版本"); !ok {
			return code
		}
	}
	result, serr := api.Rollback(ctx, res.app, client.RollbackOptions{ToSeq: *to, CancelPending: *cancelPending})
	if serr != nil {
		return a.failServer(serr, rollbackHints)
	}
	dep := result.Deployment
	a.rememberDeployment(res.targetName, res.app, dep.ID)
	// Safety net for a server that ignored the version: never report a
	// rollback to some other version as success. A deployment the server
	// already had (a queued rollback to the same program, recorded as based
	// on another version of that program) is not a mismatch.
	if toSet && dep.BasedOnSeq != *to && result.Created {
		return a.failDiag(client.Diagnosis{
			Stage:   "deploy",
			Code:    "rollback_target_mismatch",
			Message: fmt.Sprintf("服务器回到了第 %d 版，不是第 %d 版", dep.BasedOnSeq, *to),
			Hint:    "先在服务器上升级 AcornFox；用 acornfox versions 查看现在的版本记录",
		}, exitOpFailed)
	}
	if !a.out.json {
		a.out.human("回退到第 %d 版（生成第 %d 版），不重新构建。", dep.BasedOnSeq, dep.Seq)
		a.out.human("回退只换程序，数据库、数据目录和环境变量保持现在的样子。")
		if !result.Created {
			a.out.human("同一个回退已经在进行（第 %d 版），继续等待它。", dep.Seq)
		}
		if result.SupersededSeq > 0 {
			a.out.human("正在进行的第 %d 版部署会被取消。", result.SupersededSeq)
		}
	}
	if *noWait {
		return a.reportRollback(ctx, api, res.app, result, nil)
	}
	start := time.Now()
	final, code, ok := a.pollDeployment(ctx, api, dep, *timeout)
	if !ok {
		return code
	}
	result.Deployment = final
	return a.reportRollback(ctx, api, res.app, result, &start)
}

// reportRollback renders the outcome of a rollback. Failures are reported
// like any deployment; success names the new version and the one it is
// based on.
func (a *app) reportRollback(ctx context.Context, api client.API, appName string, result client.RollbackResult, start *time.Time) int {
	dep := result.Deployment
	if dep.Status == "failed" || dep.Status == "superseded" {
		return a.reportDeployment(ctx, api, appName, dep, start)
	}
	var url, lanURL string
	if dep.Status == "live" {
		if app, err := api.App(ctx, appName); err == nil {
			url, lanURL = app.URL, app.LANURL
		}
	}
	if a.out.json {
		warnings := make([]map[string]any, 0, len(dep.Warnings))
		for _, w := range dep.Warnings {
			warnings = append(warnings, map[string]any{"stage": w.Stage, "code": w.Code, "message": w.Message, "hint": w.Hint})
		}
		fields := map[string]any{
			"app":              dep.App,
			"deployment_id":    dep.ID,
			"version":          dep.Seq,
			"based_on_version": dep.BasedOnSeq,
			"status":           dep.Status,
			"url":              url,
			"lan_url":          lanURL,
			"warnings":         warnings,
			"seconds":          0,
		}
		if !result.Created {
			fields["already_in_progress"] = true
		}
		if result.SupersededSeq > 0 {
			fields["superseded_version"] = result.SupersededSeq
		}
		if start != nil {
			fields["seconds"] = int(time.Since(*start).Seconds())
		}
		a.out.emitJSON(fields)
		return exitOK
	}
	for _, w := range dep.Warnings {
		a.out.human("警告：%s", w.Message)
		if w.Hint != "" {
			a.out.human("  建议：%s", w.Hint)
		}
	}
	if dep.Status != "live" {
		a.out.human("已提交回退：第 %d 版（基于第 %d 版），未等待终态。可用 acornfox status %s 查看进度。", dep.Seq, dep.BasedOnSeq, dep.ID)
		return exitOK
	}
	a.out.human("回退成功：第 %d 版（基于第 %d 版）正在服务。", dep.Seq, dep.BasedOnSeq)
	if url != "" {
		a.out.human("网址：%s", url)
	}
	if lanURL != "" {
		a.out.human("局域网地址：%s", lanURL)
	}
	return exitOK
}

// cmdRedeploy implements `redeploy [APP] [--no-wait] [--timeout D]`: it
// redeploys the current version and, like deploy, waits to a terminal status.
// With --no-wait it prints the deployment ID exactly as before waiting existed.
func (a *app) cmdRedeploy(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("redeploy", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	var (
		noWait  = fs.Bool("no-wait", false, "提交后不等待终态")
		timeout = fs.Duration("timeout", 20*time.Minute, "等待终态的超时时间")
	)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	res, err := a.resolveAppArg("redeploy", pos)
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	deploymentID, serr := api.Redeploy(ctx, res.app)
	if serr != nil {
		return a.out.fail(serr)
	}
	a.rememberDeployment(res.targetName, res.app, deploymentID)
	if *noWait {
		if a.out.json {
			a.out.emitJSON(map[string]any{"deployment_id": deploymentID})
			return exitOK
		}
		a.out.human("已触发重新部署，部署 ID: %s", deploymentID)
		a.out.human("可使用 acornfox status %s 查看进度和诊断", deploymentID)
		return exitOK
	}

	if !a.out.json {
		a.out.human("已触发重新部署，部署 ID: %s", deploymentID)
	}
	start := time.Now()
	// The redeploy route returns only the ID; the first poll fills in the rest.
	final, code, ok := a.pollDeployment(ctx, api, client.Deployment{ID: deploymentID, App: res.app}, *timeout)
	if !ok {
		return code
	}
	if final.App == "" {
		final.App = res.app
	}
	return a.reportDeploymentWith(ctx, api, res.app, final, &start, map[string]any{"status": final.Status})
}

// requireFeature checks GET /v1/status before a command that needs a newer
// server. A missing feature is reported as server_too_old (exit 1) without
// sending the request. ok=false means the error was already reported.
func (a *app) requireFeature(ctx context.Context, api client.API, feature, what string) (int, bool) {
	features, err := api.Features(ctx)
	if err != nil {
		return a.out.fail(err), false
	}
	if !client.HasFeature(features, feature) {
		return a.failDiag(client.ServerTooOld(what).Diag, exitOpFailed), false
	}
	return exitOK, true
}

// failServer reports a request error. server_too_old exits 1 (an operation
// that cannot be done here, not a server fault); other server codes get the
// hint from hints when the server gave none.
func (a *app) failServer(err error, hints map[string]string) int {
	var ce *client.Error
	if errors.As(err, &ce) {
		diag := ce.Diag
		if diag.Code == client.CodeServerTooOld {
			return a.failDiag(diag, exitOpFailed)
		}
		if h, ok := hints[diag.Code]; ok && diag.Hint == "" && diag.Stage == "server" {
			diag.Hint = h
			return a.out.fail(&client.Error{Status: ce.Status, Diag: diag})
		}
	}
	return a.out.fail(err)
}
