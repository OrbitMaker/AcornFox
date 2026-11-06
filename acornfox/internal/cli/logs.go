package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// cmdLogs implements logs [APP] [--tail N] [--since RFC3339] [-f] and
// logs [APP] --version N [--build] [--tail N].
// A boundary cursor preserves identical log records; no text-based deduping.
func (a *app) cmdLogs(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	tail := fs.Int("tail", 100, "返回最近 N 行（最多 1000；带 --version 时默认 500、最多 2000）")
	follow := fs.Bool("follow", false, "持续跟随新日志，Ctrl+C 停止")
	fs.BoolVar(follow, "f", false, "持续跟随新日志")
	since := fs.String("since", "", "起始时间（RFC3339）")
	version := fs.Int("version", 0, "看第 N 版的日志（acornfox versions 查看版本号）")
	build := fs.Bool("build", false, "看构建日志（和 --version 一起用）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) > 1 {
		return a.out.usageError("用法：logs [APP] [--tail N] [--since RFC3339] [-f] 或 logs [APP] --version N [--build]")
	}
	if *tail < 1 {
		return a.out.usageError("tail 必须是正整数")
	}
	versionSet := flagWasSet(fs, "version")
	switch {
	case versionSet && *version < 1:
		return a.out.usageError("--version 必须是正整数（第几版）")
	case *build && !versionSet:
		return a.out.usageError("--build 要和 --version 一起用，例如 logs --version 6 --build")
	case versionSet && *since != "":
		return a.out.usageError("--since 不能和 --version 一起用")
	case *build && *follow:
		return a.out.usageError("构建日志不会再更新，不能和 -f 一起用")
	}
	if !versionSet && *tail > 1000 {
		*tail = 1000
	}
	if *since != "" {
		if _, err := time.Parse(time.RFC3339Nano, *since); err != nil {
			return a.out.usageError("since 必须是 RFC3339 时间")
		}
	}
	res, err := a.resolveAppArg("logs", pos)
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	name := res.app
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()
	if versionSet {
		tailParam := 0 // server default
		if flagWasSet(fs, "tail") {
			tailParam = *tail
		}
		dep, code, ok := a.findVersion(ctx, api, name, *version)
		if !ok {
			return code
		}
		if !*follow {
			return a.versionLogs(ctx, api, name, dep, *build, tailParam)
		}
		if dep.Status != "live" {
			return a.out.usageError("第 %d 版已停止运行，日志不会再更新；去掉 -f 查看它保存的日志", *version)
		}
		// Following the serving version is the usual follow.
		*tail = min(*tail, 1000)
	}
	if !*follow && *since == "" {
		lines, err := api.Logs(ctx, name, *tail)
		if err != nil {
			return a.out.fail(err)
		}
		a.printLogBatch(client.LogBatch{Lines: lines})
		return exitOK
	}
	reader, ok := api.(client.LogBatchAPI)
	if !ok {
		return a.out.usageError("当前客户端不支持日志跟随，请升级后重试")
	}
	cursor := ""
	first := true
	for {
		if ctx.Err() != nil {
			return exitOK
		}
		batch, err := reader.LogBatch(ctx, name, *tail, *since, cursor)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return exitOK
			}
			return a.out.fail(err)
		}
		if first || len(batch.Lines) > 0 {
			a.printLogBatch(batch)
		}
		first = false
		if !*follow {
			return exitOK
		}
		cursor = batch.Cursor
		if batch.HasMore {
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return exitOK
		case <-timer.C:
		}
	}
}

func (a *app) printLogBatch(batch client.LogBatch) {
	if a.out.json {
		a.out.emitJSON(map[string]any{"lines": batch.Lines, "cursor": batch.Cursor, "has_more": batch.HasMore, "reset": batch.Reset})
		return
	}
	for _, line := range batch.Lines {
		a.out.human("%s", line)
	}
}

// findVersion looks up version seq of app among its newest 100 deployments,
// after checking that the server keeps deployment logs. ok=false means the
// error was already reported.
func (a *app) findVersion(ctx context.Context, api client.API, appName string, seq int) (client.Deployment, int, bool) {
	if code, ok := a.requireFeature(ctx, api, client.FeatureDeployLogs, "查看某一版的日志"); !ok {
		return client.Deployment{}, code, false
	}
	list, err := api.ListDeployments(ctx, appName, 100)
	if err != nil {
		return client.Deployment{}, a.failServer(err, nil), false
	}
	for _, d := range list.Deployments {
		if d.Seq == seq {
			return d, exitOK, true
		}
	}
	msg := fmt.Sprintf("没有第 %d 版", seq)
	if len(list.Deployments) == 100 {
		msg = fmt.Sprintf("第 %d 版不在最近 100 个版本里，没有它的日志", seq)
	}
	return client.Deployment{}, a.failDiag(client.Diagnosis{
		Stage: "server", Code: "version_not_found", Message: msg, Hint: "用 acornfox versions 查看有哪些版本",
	}, exitOpFailed), false
}

// versionLogs prints the saved run or build log of one version (the run log
// of the serving version comes straight from its container).
func (a *app) versionLogs(ctx context.Context, api client.API, appName string, dep client.Deployment, build bool, tail int) int {
	kind := "run"
	if build {
		kind = "build"
	}
	out, err := api.DeploymentLogs(ctx, dep.ID, kind, tail)
	if err != nil {
		return a.failServer(err, map[string]string{
			"logs_pending":          "等部署结束后再看",
			"redaction_unavailable": "服务器读不到密钥脱敏配置，为了不泄露密钥没有返回日志；稍后重试或运行 acornfox diagnose",
		})
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{
			"app": appName, "version": out.Version, "deployment_id": out.DeploymentID,
			"kind": out.Kind, "source": out.Source, "lines": out.Lines, "truncated": out.Truncated,
		})
		return exitOK
	}
	status := dep.StatusText
	if status == "" {
		status = dep.Status
	}
	switch {
	case kind == "build":
		a.out.human("第 %d 版 · 构建日志", out.Version)
	case out.Source == "live":
		a.out.human("第 %d 版 · 运行日志（%s）", out.Version, status)
	default:
		a.out.human("第 %d 版 · 运行日志（%s，容器删除前保存）", out.Version, status)
	}
	if out.Truncated {
		a.out.human("（前面的日志已省略，可用 --tail 多看一些）")
	}
	if len(out.Lines) == 0 {
		a.out.human("（没有日志）")
	}
	for _, line := range out.Lines {
		a.out.human("%s", localLogLine(line))
	}
	return exitOK
}

// localLogLine shows a leading RFC3339 timestamp (as Docker writes it) in
// local time: "2026-11-03 10:10:31  text". Other lines are unchanged.
func localLogLine(line string) string {
	stamp, rest, ok := strings.Cut(line, " ")
	if !ok || len(stamp) < 20 || stamp[4] != '-' || stamp[10] != 'T' {
		return line
	}
	t, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return line
	}
	return t.In(time.Local).Format("2006-01-02 15:04:05") + "  " + rest
}
