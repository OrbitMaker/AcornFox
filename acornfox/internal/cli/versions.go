package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/acornfox/acornfox/internal/client"
)

// nowFunc is the clock for relative times; tests replace it.
var nowFunc = time.Now

// cmdVersions implements `versions [APP] [--limit N]`: the version history
// with source, time, duration, failure reason and whether each version can
// be rolled back to. All texts come from the server.
func (a *app) cmdVersions(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("versions", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	limit := fs.Int("limit", 20, "最多列出最近几版（1～100）")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	if *limit < 1 || *limit > 100 {
		return a.out.usageError("--limit 必须在 1～100 之间")
	}
	res, err := a.resolveAppArg("versions", pos)
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, res.target)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	if code, ok := a.requireFeature(ctx, api, client.FeatureRollbackTo, "版本记录"); !ok {
		return code
	}
	list, serr := api.ListDeployments(ctx, res.app, *limit)
	if serr != nil {
		return a.failServer(serr, nil)
	}
	if a.out.json {
		items := make([]map[string]any, 0, len(list.Deployments))
		for _, d := range list.Deployments {
			items = append(items, versionJSON(d))
		}
		a.out.emitJSON(map[string]any{
			"app":             res.app,
			"rollback_target": list.RollbackTargetSeq,
			"image_check":     list.ImageCheck,
			"versions":        items,
		})
		return exitOK
	}
	a.printVersions(res.app, list)
	return exitOK
}

// versionJSON is one version in `versions --json`: the API fields, with seq
// named "version" like the other commands.
func versionJSON(d client.Deployment) map[string]any {
	item := map[string]any{
		"version":          d.Seq,
		"deployment_id":    d.ID,
		"status":           d.Status,
		"status_text":      d.StatusText,
		"source_text":      d.SourceText,
		"source_kind":      d.SourceKind,
		"origin_kind":      d.OriginKind,
		"reason":           d.Reason,
		"based_on_seq":     d.BasedOnSeq,
		"created_at":       d.CreatedAt,
		"duration_seconds": d.DurationSeconds,
		"can_rollback":     d.CanRollback,
		"rollback_blocked": d.RollbackBlocked,
		"same_image_as":    d.SameImageAs,
		"rollback_text":    d.RollbackText,
		"logs":             map[string]bool{"build": d.Logs.Build, "run": d.Logs.Run},
	}
	if d.FailureText != "" {
		item["failure_text"] = d.FailureText
	}
	if d.Diagnosis != nil {
		item["diagnosis"] = map[string]any{
			"stage": d.Diagnosis.Stage, "code": d.Diagnosis.Code, "message": d.Diagnosis.Message, "hint": d.Diagnosis.Hint,
		}
	}
	return item
}

func (a *app) printVersions(appName string, list client.DeploymentList) {
	if len(list.Deployments) == 0 {
		a.out.human("还没有部署记录。")
		return
	}
	now := nowFunc()
	rows := [][]string{{"版本", "状态", "来源", "时间", "用时", ""}}
	failedSeq := 0
	for _, d := range list.Deployments {
		status := d.StatusText
		if status == "" {
			status = d.Status
		}
		note := d.RollbackText
		if d.Status == "failed" {
			note = d.FailureText
			if failedSeq == 0 {
				failedSeq = d.Seq
			}
		}
		rows = append(rows, []string{
			fmt.Sprintf("第 %d 版", d.Seq), status, d.SourceText, timeText(d.CreatedAt, now), durationText(d.DurationSeconds), note,
		})
	}
	for _, line := range alignColumns(rows) {
		a.out.human("%s", line)
	}
	a.out.human("")
	if list.ImageCheck == "unknown" {
		a.out.human("暂时无法确认服务器上的程序是否都还在，“可回退”只按保留规则判断。")
	}
	if list.RollbackTargetSeq > 0 {
		a.out.human("回退到某一版：acornfox rollback --to %d --app %s", list.RollbackTargetSeq, appName)
	}
	logSeq := failedSeq
	if logSeq == 0 {
		logSeq = list.Deployments[0].Seq
	}
	a.out.human("看某一版的日志：acornfox logs --version %d --app %s", logSeq, appName)
	a.out.human("回退只换程序，数据库、数据目录和环境变量保持现在的样子。")
}

// alignColumns pads every column but the last to its widest cell, counting
// CJK characters as two columns, and trims trailing spaces.
func alignColumns(rows [][]string) []string {
	if len(rows) == 0 {
		return nil
	}
	cols := len(rows[0])
	widths := make([]int, cols)
	for _, r := range rows {
		for i := 0; i < cols-1 && i < len(r); i++ {
			widths[i] = max(widths[i], displayWidth(r[i]))
		}
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		var b strings.Builder
		for i, cell := range r {
			b.WriteString(cell)
			if i < cols-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-displayWidth(cell)+2))
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

// displayWidth is the terminal width of s: East Asian wide and fullwidth
// characters take two columns.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWide(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isWide(r rune) bool {
	switch {
	case unicode.Is(unicode.Han, r):
		return true
	case r >= 0x1100 && r <= 0x115F, // Hangul Jamo
		r >= 0x2E80 && r <= 0x303E, // CJK radicals, punctuation
		r >= 0x3041 && r <= 0x33FF, // kana, CJK symbols
		r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
		r >= 0xF900 && r <= 0xFAFF, // CJK compatibility
		r >= 0xFE30 && r <= 0xFE4F, // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFF60, // fullwidth forms
		r >= 0xFFE0 && r <= 0xFFE6:
		return true
	}
	return false
}

// timeText is a short local time: "刚刚", "N 分钟前", "N 小时前" (today),
// "昨天 21:04", "11-01 18:22" (this year), else "2025-11-01".
func timeText(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := now.Sub(t)
	lt, ln := t.In(time.Local), now.In(time.Local)
	ty, tm, td := lt.Date()
	ny, nm, nd := ln.Date()
	yy, ym, yd := ln.AddDate(0, 0, -1).Date()
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d/time.Minute))
	case ty == ny && tm == nm && td == nd:
		return fmt.Sprintf("%d 小时前", int(d/time.Hour))
	case ty == yy && tm == ym && td == yd:
		return "昨天 " + lt.Format("15:04")
	case ty == ny:
		return lt.Format("01-02 15:04")
	}
	return lt.Format("2006-01-02")
}

// durationText renders seconds as "9 秒", "1 分 03 秒" or "1 小时 05 分";
// nil (unknown) is "—".
func durationText(secs *int) string {
	if secs == nil {
		return "—"
	}
	s := *secs
	switch {
	case s < 60:
		return fmt.Sprintf("%d 秒", s)
	case s < 3600:
		return fmt.Sprintf("%d 分 %02d 秒", s/60, s%60)
	}
	return fmt.Sprintf("%d 小时 %02d 分", s/3600, s%3600/60)
}
