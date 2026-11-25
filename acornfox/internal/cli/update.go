package cli

// `acornfox update [check]`：平台（AcornFox 自己）有没有新版本（W12-3，internal/platformupdate）。
// 只提醒、不升级：平台升级要在服务器上运行 get.sh 的升级命令，由用户同意后做。
// 同一份结果也在 `acornfox status` 的最后一行、`acornfox doctor`、控制台“设置 › 这台机器”和铃铛里。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/platformupdate"
)

const updateUsage = `用法：acornfox update [check] [--json]

用途：这台服务器上的 AcornFox 有没有新版本。服务器每 6 小时自己去 Gitee 或 GitHub（国内先 Gitee，其他地区先 GitHub，
      一边不通换另一边）查一次最新的正式版（带“-”的预发布版不算），结果记在服务器上，重启也不丢。
  acornfox update          看上次查到的结果：现在的版本、最新的正式版、有没有新版本、该用哪条命令升级（服务器重启后几分钟内还没查过时会说“还没查过”）
  acornfox update check    现在就去查一次（几秒内重复运行只查一次）
只提醒，不升级：有新版本时，把升级命令和发行说明地址告诉用户，用户同意以后，在服务器上运行那条命令（它会先打印要做的事，
      升级没成功会自动恢复原样）；升级会短暂停服务，先问用户。Windows、Mac 上命令不一样，以这里给的 upgrade_command 为准。
副作用：update 只读；update check 向 Gitee 或 GitHub 发一到两个只读请求，并写服务器数据目录里的 platform-update.json。
关掉：在服务的环境里设 ACORNFOX_UPDATE_CHECK=off（此时 enabled 是 false，不联网）。
输出（--json）：{"ok":true,"latest_version":"线上最新的正式版（查到过才有）","platform_update":{"enabled","current","latest","available",
  "checked_at","ok":上次有没有查到,"source":"gitee|github","notes_url","upgrade_command","upgrade_where","host_kind":"linux|windows|mac",
  "last_ok_at","failures","unreachable","error","code":"PLAT-UPD-01（有新版本）|PLAT-UPD-02（一直查不到）"}}
  platform_update 和 acornfox --json status 里带的是同一个对象。
退出码：0 正常（有没有新版本都是 0）；2 参数不对；3 连不上服务器；4 服务器拒绝（服务器版本太旧没有这个接口是 server_too_old）。
例子：
  acornfox update
  acornfox update check --json
  acornfox --target 家里的服务器 update check`

// updateWhat names the feature in a server_too_old error.
const updateWhat = "平台新版本提醒"

// cmdUpdate implements `update [check]`.
func (a *app) cmdUpdate(ctx context.Context, args []string) int {
	fs := a.catalogFlags("update", updateUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	sub := ""
	if len(pos) > 0 {
		sub, pos = pos[0], pos[1:]
	}
	switch sub {
	case "", "check":
	default:
		return a.out.usageError("不认识的子命令 %q。看结果：acornfox update；现在就查一次：acornfox update check", sub)
	}
	if len(pos) > 0 {
		return a.out.usageError("update 只有 check 一个子命令，不带别的参数")
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
	pu, ok := api.(client.PlatformUpdateAPI)
	if !ok {
		return a.out.fail(client.ServerTooOld(updateWhat))
	}
	var st client.PlatformUpdate
	var cerr error
	if sub == "check" {
		st, cerr = pu.CheckPlatformUpdate(ctx)
	} else {
		st, cerr = pu.PlatformUpdate(ctx)
	}
	if cerr != nil {
		return a.out.fail(cerr)
	}
	if a.out.json {
		a.out.emitJSON(platformUpdateFields(st))
		return exitOK
	}
	for _, l := range platformUpdateLines(st, time.Local) {
		a.out.human("%s", l)
	}
	return exitOK
}

// platformUpdateFields is the answer of `update --json`: the object the
// server gave, as platform_update (as `status --json` carries it), and
// latest_version beside it.
func platformUpdateFields(st client.PlatformUpdate) map[string]any {
	m := map[string]any{"platform_update": st}
	if st.Latest != "" {
		m["latest_version"] = st.Latest
	}
	return m
}

func localTime(rfc string, loc *time.Location) string {
	t, err := time.Parse(time.RFC3339, rfc)
	if err != nil {
		return rfc
	}
	return t.In(loc).Format("2006-01-02 15:04")
}

// platformUpdateLines says the result in words.
func platformUpdateLines(st client.PlatformUpdate, loc *time.Location) []string {
	if !st.Enabled {
		return []string{"新版本提醒已关闭（服务的环境里设了 ACORNFOX_UPDATE_CHECK=off），不会去查最新版本。现在是 " + st.Current + "。"}
	}
	var out []string
	switch {
	case st.Latest == "" && st.CheckedAt == "":
		out = append(out, "还没查过最新版本（服务启动后几分钟会自动查一次）；现在就查：acornfox update check。现在是 "+st.Current+"。")
	case st.Latest == "":
		out = append(out, fmt.Sprintf("没能查到最新版本（%s）。现在是 %s。", st.Error, st.Current))
	case st.Available:
		out = append(out, fmt.Sprintf("AcornFox 有新版本 %s（现在是 %s；%s 查到，来源 %s）。", st.Latest, st.Current, localTime(st.CheckedAt, loc), sourceName(st.Source)))
		if st.NotesURL != "" {
			out = append(out, "发行说明："+st.NotesURL)
		}
		out = append(out, "升级要先问用户，同意以后"+st.UpgradeWhere+"：", "  "+st.UpgradeCommand)
	default:
		out = append(out, fmt.Sprintf("已是最新：现在是 %s，线上最新的正式版是 %s（%s 查的，来源 %s）。", st.Current, st.Latest, localTime(st.CheckedAt, loc), sourceName(st.Source)))
	}
	if st.Latest != "" && st.Failures > 0 {
		out = append(out, fmt.Sprintf("提醒：最近 %d 次查询没成功（%s），上面是 %s 那次查到的。", st.Failures, st.Error, localTime(st.LastOKAt, loc)))
	}
	if st.Unreachable {
		out = append(out, "已经好几天查不到最新版本了（"+platformupdate.CodeUnreachable+"）：acornfox faults "+platformupdate.CodeUnreachable+" 看怎么办。")
	}
	return out
}

func sourceName(s string) string {
	switch s {
	case platformupdate.SourceGitee:
		return "Gitee"
	case platformupdate.SourceGitHub:
		return "GitHub"
	}
	return s
}

// askPlatformUpdate reads the new-version check from the server's status for
// `status` to carry (best effort: three seconds, no error). A server that
// does not have the check leaves a.platformUpd nil.
func (a *app) askPlatformUpdate(ctx context.Context, api client.API) {
	a.platformUpd = nil
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if st, err := api.Status(cctx); err == nil {
		a.platformUpd = st.PlatformUpdate
	}
}

// platformUpdateHint is the line `status` adds when a newer AcornFox is out.
func platformUpdateHint(st *client.PlatformUpdate) string {
	if st == nil || !st.Enabled || !st.Available {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("提醒：AcornFox 有新版本 %s（现在 %s）：acornfox update 看怎么升级（升级前先问用户，%s）", st.Latest, st.Current, platformupdate.CodeAvailable))
}

// addPlatformUpdateFields adds the new-version check to another command's
// JSON fields (`status`): the object as the server gave it, and
// latest_version. Nothing is added when the server has no such check.
func (a *app) addPlatformUpdateFields(fields map[string]any) {
	if a.platformUpd == nil {
		return
	}
	fields["platform_update"] = a.platformUpd
	if a.platformUpd.Latest != "" {
		fields["latest_version"] = a.platformUpd.Latest
	}
}

// humanPlatformUpdate writes the hint line at the end of a human `status`.
func (a *app) humanPlatformUpdate() {
	if h := platformUpdateHint(a.platformUpd); h != "" {
		a.out.human("%s", h)
	}
}
