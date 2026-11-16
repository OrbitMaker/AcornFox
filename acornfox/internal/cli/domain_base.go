package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/acornfox/acornfox/internal/client"
)

const domainBaseUsage = `用法：acornfox domain base [set 域名 [--plan] [--yes] | clear [--plan] [--yes]]   [--json]

平台域名：给整台服务器配一个域名（例如 apps.example.com），访问方式是“公网”的应用自动多一个地址
https://应用名.平台域名，带 HTTPS、不用记端口，证书自动申请。应用自己绑的域名优先；原来带端口的地址照常能用。
只本机、局域网、只在内部网络的应用不受影响。局域网里的机器（没有公网地址）用不了。

  domain base             看现在的平台域名、要在域名服务商那里加的那条记录、三项检查（域名指过来没有、
                          外面能不能连上 80 和 443、证书），以及每个应用在它下面的名字。只读（会马上重新检查一次）
  domain base set 域名     设平台域名。写操作：入口代理给公网应用加上新名字并申请证书。
                          已经有平台域名、要换成别的：会让所有应用在旧域名下的地址打不开，要用户同意后加 --yes
  domain base clear       清掉平台域名。写操作：所有应用在它下面的地址打不开，回到带端口的地址；要用户同意后加 --yes
  --plan                  只看会怎样（每个应用的地址怎么变），什么都不改
  --yes                   用户已经同意换掉或清掉现在的平台域名

先在域名服务商那里加一条记录：名字 *.子域（例如 *.apps）、类型 A、指向这台服务器的公网地址
（acornfox domain base 会给出完整的一条），加好以后再 set。中国大陆的服务器，域名要先备案。

例子：
  acornfox domain base --json
  acornfox domain base set apps.example.com --plan
  acornfox domain base set apps.example.com
  acornfox domain base set web.example.org --yes
  acornfox domain base clear --plan
`

// cmdDomainBase implements `domain base [set NAME | clear] [--plan] [--yes]`.
func (a *app) cmdDomainBase(ctx context.Context, args []string) int {
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := a.catalogFlags("domain base", domainBaseUsage)
	plan := fs.Bool("plan", false, "只看会怎样，什么都不改")
	yes := fs.Bool("yes", false, "用户已经同意换掉或清掉现在的平台域名")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	switch sub {
	case "", "show", "check":
		if len(rest) != 0 || *plan || *yes {
			return a.out.usageError("domain base 只看现状，不带别的参数（设：domain base set 域名；清：domain base clear）")
		}
	case "set":
		if len(rest) != 1 {
			return a.out.usageError("用法：acornfox domain base set 域名 [--plan] [--yes]，例如 acornfox domain base set apps.example.com")
		}
	case "clear":
		if len(rest) != 0 {
			return a.out.usageError("domain base clear 不带域名：清掉的是现在那一个")
		}
	case "help":
		fmt.Fprint(a.out.stdout, domainBaseUsage)
		return exitOK
	default:
		return a.out.usageError("不认识的子命令 %q。完整用法：acornfox domain base --help", sub)
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
	pd, ok := api.(client.PlatformDomainAPI)
	if !ok {
		return a.out.fail(client.ServerTooOld("平台域名"))
	}
	var v client.PlatformDomain
	switch sub {
	case "set":
		v, err = pd.SetPlatformDomain(ctx, rest[0], *yes, *plan)
	case "clear":
		v, err = pd.ClearPlatformDomain(ctx, *yes, *plan)
	default:
		v, err = pd.CheckPlatformDomain(ctx)
	}
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		var m map[string]any
		if json.Unmarshal(v.Raw, &m) != nil || m == nil {
			m = map[string]any{}
		}
		a.out.emitJSON(m)
		return exitOK
	}
	switch {
	case v.Plan != nil:
		a.domainBasePlanHuman(v)
	case sub == "set":
		a.out.human("平台域名已设为 %s。", v.Domain)
		a.out.human("%s", icpNotice)
		a.domainBaseHuman(v)
	case sub == "clear":
		a.out.human("平台域名已清掉。应用回到原来的地址（自己绑的域名不受影响）。")
	default:
		a.domainBaseHuman(v)
	}
	return exitOK
}

func (a *app) domainBasePlanHuman(v client.PlatformDomain) {
	p := v.Plan
	a.out.human("只是看看，什么都没改。")
	a.out.human("%s。", strings.TrimSuffix(p.Say, "。"))
	if p.Change == "set" || p.Change == "replace" {
		a.out.human("%s", icpNotice)
	}
	for _, app := range v.Apps {
		if app.URLAfter != "" && app.URLAfter != app.URL {
			a.out.human("  %s：%s → %s", app.App, app.URL, app.URLAfter)
		}
	}
	if p.NeedsConfirm {
		a.out.human("这一步要用户同意。同意后去掉 --plan、加 --yes 再运行。")
	}
}

func (a *app) domainBaseHuman(v client.PlatformDomain) {
	if !v.Supported && v.Unsupported != nil {
		a.out.human("%s（%s）。", v.Unsupported.Text, v.Unsupported.Code)
		if v.Unsupported.Next != "" {
			a.out.human("  %s", v.Unsupported.Next)
		}
		return
	}
	if v.State == "unset" || v.Domain == "" {
		a.out.human("还没有设平台域名。配一个以后，开放到公网的应用会多一个地址 https://应用名.域名，不用记端口。")
		a.out.human("第一步：到你买域名的地方加一条记录：名字 *.子域（例如 *.apps）、类型 %s、指向 %s。", v.Record.Type, v.Record.Value)
		a.out.human("第二步：加好以后运行 acornfox domain base set 域名（例如 apps.example.com）。")
		return
	}
	state := map[string]string{"ok": "正常", "problem": "有问题"}[v.State]
	a.out.human("平台域名：%s（%s）", v.Domain, state)
	a.out.human("要加的记录：名字 %s、类型 %s、指向 %s（完整写法 %s）", v.Record.Name, v.Record.Type, v.Record.Value, v.Record.Full)
	a.out.human("检查：")
	for _, c := range v.Checks {
		mark := "✗"
		switch {
		case c.OK:
			mark = "✓"
		case c.Waiting:
			mark = "…"
		}
		if c.Code != "" {
			a.out.human("  %s %s（%s）", mark, c.Text, c.Code)
		} else {
			a.out.human("  %s %s", mark, c.Text)
		}
	}
	if p := v.Problem; p != nil {
		a.out.human("问题（%s）：%s", p.Code, p.Text)
		if p.Detail != "" {
			a.out.human("  详情：%s", p.Detail)
		}
		if p.Next != "" {
			a.out.human("  怎么办：%s", p.Next)
		}
		a.out.human("  说明：acornfox faults %s", p.Code)
	}
	if len(v.Apps) > 0 {
		a.out.human("应用：")
		for _, app := range v.Apps {
			switch {
			case app.Active:
				a.out.human("  %s：%s（地址 %s）", app.App, app.Host, app.URL)
			case app.Host != "":
				a.out.human("  %s：%s 没生效：%s", app.App, app.Host, app.Note)
			default:
				a.out.human("  %s：%s", app.App, app.Note)
			}
		}
	}
}
