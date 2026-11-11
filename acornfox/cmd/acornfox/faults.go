package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/faults"
)

const faultsHelp = `用法：acornfox faults [--json]                列出全部故障编号：编号、等级、标题、下一步
      acornfox faults 编号 [--json]           看一条：是什么、现象、可能原因和怎么判定、排查顺序、
                                              处置（分级）、什么时候必须先问用户、怎么确认恢复
      acornfox faults --group 组 [--json]     只列一组，组名见列表最后（例如 SETUP、INST、DEPLOY）
      acornfox faults --markdown              输出故障目录全文（就是仓库里的 docs/faults.md）

只读：不连服务器、不改任何东西。目录编在程序里，和这个 acornfox 的版本一致。

编号从哪里来：安装、升级脚本打印的“故障编号: …”；acornfox doctor 每项后面括号里的；
命令加 --json 时输出里的 code、diagnosis.code、gateway_code；控制台“运行环境”页上的。
大写的编号（例如 CAT-INST-24）不分大小写；小写的诊断码（例如 build_failed）照原样写。

处置分三级：0 级只读；1 级可以直接做（不删数据、不花钱、不对外开放、做了能撤回）；
2 级必须先问用户，用户同意后才做。拿不准就按 2 级。

JSON：
  列表      {"ok":true,"faults":[…],"groups":[…],"tiers":{…},"levels":{…},"evidence":"…"}
  一条      {"ok":true,"fault":{"code","group","title","level","explain","next","auto","symptoms",
            "causes":[{"text","check","auto"}],"steps","actions":[{"text","tier"}],"ask","verify",…}}
  没有这个编号  {"ok":false,"diagnosis":{"stage":"faults","code":"fault_unknown",…},"fallback":{兜底条目 ANY-00}}

退出码：0 正常；1 目录里没有这个编号（照兜底条目 ANY-00 做：先把出错输出里的 message 和 hint 原样告诉用户，
只照做改参数、重试；别的只读，收集证据交给用户）；2 用法不对。

例子：
  acornfox faults
  acornfox faults CAT-INST-27
  acornfox faults SETUP-GW-01 --json
  acornfox faults --group SETUP
`

// runFaults implements `acornfox faults`: the fault catalog compiled into
// this binary (internal/faults). It never contacts a server.
func runFaults(args []string, stdout, stderr io.Writer) int {
	var asJSON, markdown bool
	var group string
	var codes []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(stdout, faultsHelp)
			return 0
		case a == "--json" || a == "-json" || a == "--json=true" || a == "-json=true":
			asJSON = true
		case a == "--markdown" || a == "-markdown":
			markdown = true
		case a == "--group" || a == "-group":
			if i+1 >= len(args) {
				return faultsUsage(stdout, stderr, asJSON || hasJSONFlag(args), "--group 后面要写组名")
			}
			i++
			group = strings.ToUpper(args[i])
		case strings.HasPrefix(a, "--group=") || strings.HasPrefix(a, "-group="):
			group = strings.ToUpper(a[strings.Index(a, "=")+1:])
		case strings.HasPrefix(a, "-"):
			return faultsUsage(stdout, stderr, asJSON || hasJSONFlag(args), "不认识的参数 "+a)
		default:
			codes = append(codes, a)
		}
	}
	switch {
	case len(codes) > 1:
		return faultsUsage(stdout, stderr, asJSON, "一次只看一个编号")
	case markdown && (asJSON || len(codes) > 0 || group != ""):
		return faultsUsage(stdout, stderr, asJSON, "--markdown 不和编号、--group、--json 一起用")
	case len(codes) == 1 && group != "":
		return faultsUsage(stdout, stderr, asJSON, "写了编号就不用 --group")
	case markdown:
		fmt.Fprint(stdout, faults.Markdown())
		return 0
	case len(codes) == 1:
		return showFault(stdout, stderr, codes[0], asJSON)
	}
	list := faults.Catalog
	if group != "" {
		if faults.GroupName(group) == group {
			return faultsUsage(stdout, stderr, asJSON, "没有这个组 "+group+"；组名见 acornfox faults 列表最后")
		}
		list = nil
		for _, f := range faults.Catalog {
			if f.Group == group {
				list = append(list, f)
			}
		}
	}
	if asJSON {
		return faultsJSON(stdout, map[string]any{"ok": true, "faults": list, "groups": faults.Groups,
			"tiers": faults.TierName, "levels": faults.LevelName, "evidence": faults.Evidence}, 0)
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "编号\t等级\t标题\t下一步")
	for _, f := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.Code, f.Level, f.Title, oneLine(f.Next, 60))
	}
	_ = tw.Flush()
	fmt.Fprintf(stdout, "\n共 %d 条。看一条的完整说明：acornfox faults 编号；只看一组：acornfox faults --group 组\n组：", len(list))
	for i, g := range faults.Groups {
		if i > 0 {
			fmt.Fprint(stdout, "、")
		}
		fmt.Fprintf(stdout, "%s（%s）", g.Key, g.Name)
	}
	fmt.Fprintln(stdout)
	return 0
}

func showFault(stdout, stderr io.Writer, code string, asJSON bool) int {
	f, ok := faults.Find(code)
	if !ok {
		msg := "故障目录里没有 " + code
		hint := "运行 acornfox faults 看全部编号；对不上任何条目时照兜底条目 " + faults.FallbackCode + " 做：先把出错那条输出里的 message 和 hint 原样告诉用户，只照做改参数、重试；别的只读、不动任何东西，收集证据交给用户"
		if asJSON {
			return faultsJSON(stdout, map[string]any{"ok": false, "diagnosis": client.Diagnosis{
				Stage: "faults", Code: "fault_unknown", Message: msg, Hint: hint,
			}, "fallback": f}, 1)
		}
		fmt.Fprintln(stderr, msg+"。"+hint)
		fmt.Fprint(stderr, "\n"+faults.Detail(f))
		return 1
	}
	if asJSON {
		return faultsJSON(stdout, map[string]any{"ok": true, "fault": f}, 0)
	}
	fmt.Fprint(stdout, faults.Detail(f))
	return 0
}

func faultsUsage(stdout, stderr io.Writer, asJSON bool, msg string) int {
	if asJSON {
		return faultsJSON(stdout, map[string]any{"ok": false, "diagnosis": client.Diagnosis{
			Stage: "usage", Code: "usage", Message: msg, Hint: "acornfox faults --help",
		}}, 2)
	}
	fmt.Fprintln(stderr, msg+"（acornfox faults --help 看用法）")
	return 2
}

func faultsJSON(w io.Writer, v any, rc int) int {
	b, _ := json.Marshal(v)
	fmt.Fprintln(w, string(b))
	return rc
}

// oneLine shortens s to at most n characters for the list.
func oneLine(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
