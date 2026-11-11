package cli

import (
	"context"

	"github.com/acornfox/acornfox/internal/client"
)

// `acornfox apps vars APP`: the variables an app installed from the app
// center was configured with. Read-only; the server decides what is shown
// (GET /v1/apps/{app}/vars), the CLI only prints it.

const appsUsage = `用法：acornfox apps
      acornfox apps vars 应用 [--reveal]

apps            列出服务器上的所有应用：名字、状态、地址。只读
apps vars 应用  看一个从应用中心装的应用是用什么设置装的。只读（acornfox apps vars -h 看详情）`

const appsVarsUsage = `用法：acornfox apps vars 应用 [--reveal]

用途：看一个从应用中心装的应用是用什么设置装的。每个变量一行：
  名字       params.名字 是应用自己的参数（自定义变量），platform.名字 是平台给的（系统变量）；
             大模型参数拆成 params.名字.source、base_url、api_key、model 几行
  类别       系统给、平台生成、有默认值、可选、要选模型来源、必须用户填（和 catalog vars 同一套）
  现在的值   安装时保存的值
  值从哪来   用户填的、配方预填的、清单默认值、平台生成的、平台算的、用户改的（--set platform.名字=值）、
             接上旧数据时恢复的；旧版本装的应用那时不记这个，照实写“装时的值”
  能不能改   清单写的（不能改、随时能改、重装才能改）；平台变量只能在安装时改
  还会说明保存的清单是安装时存的、后来补存的，还是没存。清单没存的应用只列保存的值，分不出类别，
  自定义变量一律按密码遮住。
  这一版只能看，不能改：改设置的功能还没有。
副作用：无，只读。密码和 Key 默认只显示后 4 位（不到 12 个字符的全遮）。
参数：
  --reveal   显示完整的密码和 Key（会出现在屏幕和 --json 输出里，注意别贴到公开的地方）
输出（--json）：{"ok":true,"app":…,"from_catalog":true,"catalog_id":…,"version":…,"source":{id,name},
  "manifest":{saved, source(install|backfill|""), source_label, sha256, saved_at},"revealed":false,"can_change":false,
  "vars":[{name, layer(system|custom), layer_label, class, class_label, label, value, secret, masked,
           origin, origin_label, editable, editable_label, used}],"notes":[…]}
  不是从应用中心装的应用：from_catalog 是 false，vars 为空，notes 里说明该看哪里，退出码 0。
退出码：0 正常；2 参数不对；3 连不上服务器；4 服务器拒绝（没有这个应用是 CAT-INST-22）。
例子：
  acornfox apps vars new-api
  acornfox apps vars hermes-agent --reveal
  acornfox --json apps vars new-api`

func (a *app) cmdAppsVars(ctx context.Context, args []string) int {
	fs := a.catalogFlags("apps vars", appsVarsUsage)
	reveal := fs.Bool("reveal", false, "显示完整的密码和 Key")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	// The app is the positional argument or the global --app; never guessed
	// from the current directory (this shows passwords with --reveal).
	name := a.flagApp
	switch {
	case len(pos) > 1:
		return a.out.usageError("用法：acornfox apps vars 应用 [--reveal]")
	case len(pos) == 1 && name != "" && name != pos[0]:
		return a.out.usageError("应用名给了两个不一样的：%s 和 --app %s", pos[0], name)
	case len(pos) == 1:
		name = pos[0]
	case name == "":
		return a.out.usageError("要给应用名：acornfox apps vars 应用（acornfox apps 看有哪些）")
	}
	app, ok := normalizeAppName(name)
	if !ok {
		return a.out.usageError("应用名 %q 不合法；只能用小写字母、数字和连字符", name)
	}
	t, _, terr := a.resolveTarget()
	if terr != nil {
		return a.out.usageError("%s", terr.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()
	va, ok := api.(client.AppVarsAPI)
	if !ok {
		return a.out.fail(&client.Error{Diag: client.Diagnosis{Stage: "server", Code: client.CodeServerTooOld, Message: "这个连接不支持查看应用的变量", Hint: "升级 AcornFox"}})
	}
	v, err := va.AppVars(ctx, app, *reveal)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(v.Raw, nil)
		return exitOK
	}
	if !v.FromCatalog {
		for _, n := range v.Notes {
			a.out.human("%s", n)
		}
		return exitOK
	}
	a.out.human("%s：从应用中心装的 %s %s%s", v.App, v.CatalogID, v.Version, fromText(v.Source))
	a.out.human("清单：%s", v.Manifest.SourceLabel)
	layer := ""
	for _, x := range v.Vars {
		if x.Layer != layer {
			layer = x.Layer
			a.out.human("")
			a.out.human("%s：", x.LayerLabel)
		}
		val := x.Value
		if val == "" {
			val = "（空）"
		}
		head := x.Name
		if x.Label != "" {
			head += "（" + x.Label + "）"
		}
		class := x.ClassLabel
		if class == "" {
			class = "类别不明"
		}
		note := ""
		if x.Used != nil && !*x.Used {
			note = "；这个应用的清单没用到它"
		}
		a.out.human("  %s = %s", head, val)
		a.out.human("      [%s] %s；%s%s", class, x.OriginLabel, x.EditableLabel, note)
	}
	if len(v.Vars) == 0 {
		a.out.human("没有保存的变量。")
	}
	a.out.human("")
	for _, n := range v.Notes {
		a.out.human("注：%s", n)
	}
	return exitOK
}
