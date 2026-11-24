package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/acornfox/acornfox/internal/capabilities"
	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/client"
)

const capabilitiesHelp = `用法：acornfox capabilities [--json]

用途：这个 acornfox 程序自己能做什么，一张清单：命令和参数、故障编号、平台变量、服务类型、
      清单格式从哪个版本起支持什么、通用 Skill 里登记的通用做法。
      写应用手册、查手册里的命令是不是真有、AI 工作台想知道“这个版本有没有某条命令”时用它；
      以它为准，不要凭记忆或旧文档猜。
副作用：无，只读；不连服务器、不读用户的数据，只看这个程序自己（版本和这个命令行一致）。
输出（--json）：{"ok":true,"schema":1,"version":"…",
  "commands":[{"path":"apps upgrade","flags":["--to","--plan","--yes","--json"],"writes":true,"since":"0.7.0","local":true,"internal":true}],
  "faults":["APP-UPG-02",…],"platform_vars":["platform.tz",…],"service_types":["llm_gateway",…],
  "manifest_features":[{"name":"manifest:/id","since":"0.6.0"}],"skill_patterns":["llm-key"]}
  commands：path 是 acornfox 后面的词；flags 是这条命令自己的参数（全局的 --target、--app 不列），带 --json 说明它能给 JSON；
  writes 为 true 是这条命令能改东西（要先试跑的写操作加 --plan 或不加 --yes）；since 只有 0.6.3 以后新加的才有，
  没有 since 的是更早就有；local 为 true 是只在这台电脑上做、不连服务器；internal 为 true 是 AcornFox 自己启动和安装用的，日常任务不用。
  service_types：程序内置目录里的服务类型；机器上同步下来的目录可能还有别的，用 acornfox services kinds 看。
  skill_patterns：通用 Skill 里已经写成通用做法的名字；应用手册里再遇到同名的，只许写一句“照通用做法 名字”。
退出码：0 正常；2 用法不对。
例子：
  acornfox capabilities
  acornfox --json capabilities
`

// runCapabilities implements `acornfox capabilities`: the program's own
// capability list (internal/capabilities). It never contacts a server.
func runCapabilities(args []string, stdout, stderr io.Writer) int {
	asJSON := false
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(stdout, capabilitiesHelp)
			return 0
		case a == "--json" || a == "-json" || a == "--json=true" || a == "-json=true":
			asJSON = true
		default:
			if asJSON || hasJSONFlag(args) {
				b, _ := json.Marshal(map[string]any{"ok": false, "diagnosis": client.Diagnosis{
					Stage: "usage", Code: "usage", Message: "capabilities 只认 --json：不认识的参数 " + a, Hint: "acornfox capabilities --help",
				}})
				fmt.Fprintln(stdout, string(b))
			} else {
				fmt.Fprintln(stderr, "capabilities 只认 --json：不认识的参数 "+a+"（acornfox capabilities --help 看用法）")
			}
			return 2
		}
	}
	doc := capabilities.Build(cli.Version, cli.SkillCorpus())
	if asJSON {
		// "ok" first, then the document's own fields in their order.
		b, _ := json.Marshal(doc)
		fmt.Fprintln(stdout, `{"ok":true,`+string(b[1:]))
		return 0
	}
	printCapabilities(stdout, doc)
	return 0
}

func printCapabilities(w io.Writer, d capabilities.Doc) {
	fmt.Fprintf(w, "AcornFox %s 能做的事（--json 看完整清单）\n\n", d.Version)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "命令\t改东西\t新加\t说明\t参数")
	for _, c := range d.Commands {
		if c.Internal {
			continue
		}
		writes, note := "", ""
		if c.Writes {
			writes = "会改"
		}
		if c.Local {
			note = "只在这台电脑"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", c.Path, writes, c.Since, note, strings.Join(c.Flags, " "))
	}
	_ = tw.Flush()
	fmt.Fprintf(w, "\n故障编号 %d 条（acornfox faults 编号 看一条）\n", len(d.Faults))
	fmt.Fprintf(w, "平台变量 %d 个：%s\n", len(d.PlatformVars), strings.Join(d.PlatformVars, "、"))
	fmt.Fprintf(w, "内置的服务类型：%s\n", strings.Join(d.ServiceTypes, "、"))
	fmt.Fprintf(w, "清单格式的功能 %d 项（各从哪个版本起支持，看 --json）\n", len(d.ManifestFeatures))
	if len(d.SkillPatterns) > 0 {
		fmt.Fprintf(w, "通用 Skill 里的通用做法：%s\n", strings.Join(d.SkillPatterns, "、"))
	}
	fmt.Fprintln(w, "没有“新加”一栏内容的命令，0.6.3 以前就有；开发用的内部命令（server、runner 等）没有列在这里，--json 里有。")
}
