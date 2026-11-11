package faults

import (
	"fmt"
	"strings"
)

// Detail is one entry in full, in Chinese; `acornfox faults <code>` and
// docs/faults.md share it.
func Detail(f Fault) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s %s\n\n", f.Code, f.Title)
	fmt.Fprintf(&b, "- 分组：%s\n", GroupName(f.Group))
	if f.Stage != "" {
		fmt.Fprintf(&b, "- 出现在：diagnosis.stage 为 %s\n", f.Stage)
	}
	if f.Origin == "gateway" {
		b.WriteString("- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里\n")
	}
	if f.Reserved {
		b.WriteString("- 预留：现在还没有地方报这个编号\n")
	}
	fmt.Fprintf(&b, "- 默认等级：%s（%s）\n", f.Level, LevelName[f.Level])
	if f.Explain != "" {
		fmt.Fprintf(&b, "- 是什么：%s\n", f.Explain)
	}
	fmt.Fprintf(&b, "- 下一步：%s\n", f.Next)
	if f.Auto != "" {
		fmt.Fprintf(&b, "- AcornFox 已经自动做的：%s\n", f.Auto)
	}
	if f.NeedsUser {
		b.WriteString("- 这一项要用户自己确认或操作\n")
	}
	fmt.Fprintf(&b, "- 现象：%s\n", strings.Join(f.Symptoms, "；"))
	b.WriteString("\n可能原因（逐条判定）：\n\n")
	for i, c := range f.Causes {
		auto := ""
		if c.Auto {
			auto = "［doctor 自动判定］"
		}
		fmt.Fprintf(&b, "%d. %s%s —— 判定：%s\n", i+1, c.Text, auto, c.Check)
	}
	b.WriteString("\n排查顺序：\n\n")
	for i, s := range f.Steps {
		fmt.Fprintf(&b, "%d. %s\n", i+1, s)
	}
	b.WriteString("\n处置：\n\n")
	for _, a := range f.Actions {
		fmt.Fprintf(&b, "- ［%d 级 %s］%s\n", a.Tier, TierName[a.Tier], a.Text)
	}
	fmt.Fprintf(&b, "\n- 必须先停下问用户：%s\n", f.Ask)
	fmt.Fprintf(&b, "- 确认恢复：%s\n", f.Verify)
	if f.Rollback != "" {
		fmt.Fprintf(&b, "- 回滚：%s\n", f.Rollback)
	}
	if len(f.Related) > 0 {
		fmt.Fprintf(&b, "- 相关编号：%s\n", strings.Join(f.Related, "、"))
	}
	if f.Inject != "" {
		fmt.Fprintf(&b, "- 演练方法：%s\n", f.Inject)
	}
	return b.String()
}

// Markdown is the full text of docs/faults.md.
func Markdown() string {
	var b strings.Builder
	b.WriteString("# AcornFox 故障目录\n\n")
	b.WriteString("本文件由 `acornfox faults --markdown > docs/faults.md` 生成，唯一来源是程序里的 `internal/faults`，不要手改。\n")
	b.WriteString("同样的内容在命令行里看：`acornfox faults` 列出全部编号，`acornfox faults <编号>` 看一条（加 `--json` 给程序读）。\n\n")
	b.WriteString("怎么用：看到一个编号（安装脚本打印的 `故障编号: …`、`acornfox doctor` 每项后面括号里的、命令 `--json` 输出里的 `code` 或 `diagnosis.code`、")
	b.WriteString("控制台“运行环境”页上的），先运行 `acornfox faults <编号>`，按“排查顺序”一条条判定，再按“处置”做。\n\n")
	b.WriteString("处置分三级：0 级只读，随时可以做；1 级可以直接做（不删数据、不花钱、不对外开放、做了能撤回）；2 级必须先问用户、用户同意后才做。拿不准就按 2 级。\n\n")
	b.WriteString("等级：P1 应用或服务停了，马上处理；P2 功能用不了，尽快处理；P3 提示或用法问题，不急。\n\n")
	fmt.Fprintf(&b, "要停下问用户、或者要反馈给维护者时，带上：%s。\n\n", Evidence)
	b.WriteString("## 总表\n\n| 编号 | 等级 | 标题 | 下一步 |\n| --- | --- | --- | --- |\n")
	for _, f := range Catalog {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", f.Code, f.Level, cell(f.Title), cell(f.Next))
	}
	last := ""
	for _, f := range Catalog {
		if f.Group != last {
			fmt.Fprintf(&b, "\n## %s\n", GroupName(f.Group))
			last = f.Group
		}
		b.WriteString("\n" + Detail(f))
	}
	return b.String()
}

// cell keeps a table cell on one line.
func cell(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
}
