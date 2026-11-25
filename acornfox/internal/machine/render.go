package machine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CacheFilePrefix and CacheFileExt make the name of the Markdown part kept in
// the Skill directory: machine-<registered server name>.md.
const (
	CacheFilePrefix = "machine-"
	CacheFileExt    = ".md"
)

// CacheFileName is the file name for the server registered under name. Only
// letters, digits, "-", "_" and "." are kept (a registered name may have
// other characters; the file name must not go outside the Skill directory).
func CacheFileName(server string) string {
	return CacheFilePrefix + safeName(server) + CacheFileExt
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._\p{Han}-]+`)

func safeName(s string) string {
	s = strings.Trim(unsafeName.ReplaceAllString(s, "_"), "._")
	if s == "" {
		return "server"
	}
	return s
}

var stateText = map[string]string{
	"running":     "在运行",
	"stopped":     "已停止",
	"not_running": "应该在运行但没有容器（可能在启动或出错了，acornfox status 应用名 看原因）",
	"unknown":     "状态读不出来",
}

var statusText = map[string]string{
	"ok":               "可用",
	"incomplete":       "还差必填项",
	"provider_stopped": "提供它的应用停着",
	"provider_gone":    "提供它的应用已删",
	"not_ready":        "还不能用（提供它的应用还没准备好）",
}

// otherLine is one unmanaged container: its name, whether it runs, the ports
// it publishes.
func otherLine(c OtherContainer) string {
	state := "停了"
	if c.Running {
		state = "在跑"
	}
	line := c.Name + "：" + state
	if len(c.Ports) > 0 {
		ps := make([]string, len(c.Ports))
		for i, p := range c.Ports {
			ps[i] = strconv.Itoa(p)
		}
		line += "，端口 " + strings.Join(ps, "、")
	}
	return line
}

func lookupText(m map[string]string, k string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return k
}

func manualText(m Manual, id string, solution bool) string {
	if !m.Has {
		return "没有专门的手册，照通用 Skill 做"
	}
	cmd := "acornfox --json catalog skill " + id
	if solution {
		cmd += " --recipe"
	}
	if m.Version != "" {
		return fmt.Sprintf("有手册（对 %s 版验过）：%s", m.Version, cmd)
	}
	return "有手册：" + cmd
}

// Markdown is the part kept next to the general Skill for one server: a
// readable copy of the view with a header that says when it was made, for
// which server and by which program version, and when to ask again. It is a
// cache: the answer of `acornfox --json machine` is the truth.
func Markdown(v View, server string) string {
	var b strings.Builder
	name := server
	if name == "" {
		name = v.Machine.Name
	}
	flag := ""
	if server != "" {
		flag = " --target " + server
	}
	fmt.Fprintf(&b, "<!-- 生成时间：%s；服务器：%s；AcornFox %s；这是缓存，以 acornfox%s --json machine 的结果为准 -->\n", v.GeneratedAt, name, v.Machine.Version, flag)
	fmt.Fprintf(&b, "# 服务器 %s 现在能做什么\n\n", name)
	fmt.Fprintf(&b, "生成时间 %s。开始做事前先看这一页；超过一天，或者用户刚在页面上动过应用，先运行 `acornfox%s --json machine` 重新取。\n", v.GeneratedAt, flag)
	fmt.Fprintf(&b, "这里没有密码、Key、令牌的值，也不会有；要用到先问用户。\n\n")

	fmt.Fprintf(&b, "## 已装的应用（%d 个）\n\n", len(v.Apps))
	if len(v.Apps) == 0 {
		b.WriteString("还没有装应用。\n\n")
	}
	for _, a := range v.Apps {
		title := a.Name
		if title == "" {
			title = a.Instance
		}
		fmt.Fprintf(&b, "- **%s**（编号 %s", title, a.ID)
		if a.Instance != a.ID {
			fmt.Fprintf(&b, "，这台机器上叫 %s", a.Instance)
		}
		fmt.Fprintf(&b, "）：%s", lookupText(stateText, a.State))
		if a.Source == "deploy" {
			b.WriteString("；自己部署的项目")
		}
		if a.Version != "" {
			fmt.Fprintf(&b, "；版本 %s", a.Version)
		}
		b.WriteString("\n")
		if a.Tagline != "" {
			fmt.Fprintf(&b, "  - 做什么：%s\n", a.Tagline)
		}
		switch {
		case a.Exposure == "internal":
			b.WriteString("  - 打开：没有页面，只有别的应用从内部网络用它\n")
		case a.URL != "":
			fmt.Fprintf(&b, "  - 打开：%s（对外方式：%s）\n", a.URL, a.Exposure)
		}
		if a.Source == "catalog" {
			fmt.Fprintf(&b, "  - 手册：%s\n", manualText(a.Manual, a.ID, false))
		}
		if len(a.Provides) > 0 {
			fmt.Fprintf(&b, "  - 提供的服务：%s\n", strings.Join(a.Provides, "、"))
		}
		for _, u := range a.Uses {
			need := "可选"
			if u.Required {
				need = "必需"
			}
			plugged := "没接上"
			if u.Service != "" {
				plugged = "接的是服务 " + u.Service
			}
			fmt.Fprintf(&b, "  - 用到的服务：%s（%s，%s）：%s\n", u.Name, u.Kind, need, plugged)
		}
	}
	b.WriteString("\n")

	if len(v.Solutions) > 0 {
		fmt.Fprintf(&b, "## 已装的解决方案（%d 个）\n\n", len(v.Solutions))
		for _, s := range v.Solutions {
			fmt.Fprintf(&b, "- **%s**（编号 %s）：包含 %s；%s\n", s.Name, s.ID, strings.Join(s.Apps, "、"), manualText(s.Manual, s.ID, true))
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## 服务（%d 条）\n\n", len(v.Services))
	if len(v.Services) == 0 {
		b.WriteString("这台机器上还没有服务。\n\n")
	}
	for _, s := range v.Services {
		src := "应用 " + s.Provider + " 提供"
		if s.RegisteredByUser {
			src = "用户自己登记"
		}
		used := "没有应用在用"
		if len(s.UsedBy) > 0 {
			used = "在用它的：" + strings.Join(s.UsedBy, "、")
		}
		fmt.Fprintf(&b, "- %s（类型 %s，登记名 %s）：%s；%s；%s\n", s.Name, s.Type, s.Service, src, lookupText(statusText, s.Status), used)
	}
	b.WriteString("\n")

	if len(v.OtherContainers) > 0 {
		fmt.Fprintf(&b, "## 这台机器上不归 AcornFox 管的容器（%d 个）\n\n", len(v.OtherContainers))
		b.WriteString("用户自己装的（docker run、compose 或别的面板）。只能看，不能管；要看用了多少内存：`acornfox --json other-containers`。\n\n")
		for _, c := range v.OtherContainers {
			fmt.Fprintf(&b, "- %s\n", otherLine(c))
		}
		b.WriteString("\n")
	}

	b.WriteString("## 平台本身能做什么\n\n")
	yes := func(ok bool) string {
		if ok {
			return "能"
		}
		return "这个版本没有"
	}
	fmt.Fprintf(&b, "- 备份和恢复应用数据：%s\n- 升级应用、退回升级：%s\n- 改了设置让应用跟上（apps reapply）：%s\n", yes(v.Platform.Backup), yes(v.Platform.Upgrade), yes(v.Platform.Reapply))
	fmt.Fprintf(&b, "- 这个程序所有命令和参数：`acornfox --json capabilities`\n")
	for _, w := range v.Warnings {
		fmt.Fprintf(&b, "\n> 注意（%s）：%s 没有算全：%s\n", w.Code, w.Part, w.Message)
	}
	return b.String()
}

// Text is the short readable form `acornfox machine` prints.
func Text(v View) string {
	var b strings.Builder
	fmt.Fprintf(&b, "服务器 %s（AcornFox %s，%s）\n", v.Machine.Name, v.Machine.Version, map[string]string{"cn": "国内网络", "global": "海外网络"}[v.Machine.Region])
	fmt.Fprintf(&b, "应用 %d 个：\n", len(v.Apps))
	for _, a := range v.Apps {
		line := fmt.Sprintf("  %s  %s", a.Instance, lookupText(stateText, a.State))
		if a.URL != "" {
			line += "  " + a.URL
		}
		if a.Manual.Has {
			line += "  有手册"
		}
		b.WriteString(line + "\n")
	}
	if len(v.Solutions) > 0 {
		fmt.Fprintf(&b, "解决方案 %d 个：", len(v.Solutions))
		names := make([]string, 0, len(v.Solutions))
		for _, s := range v.Solutions {
			names = append(names, s.Name)
		}
		b.WriteString(strings.Join(names, "、") + "\n")
	}
	fmt.Fprintf(&b, "服务 %d 条：\n", len(v.Services))
	for _, s := range v.Services {
		fmt.Fprintf(&b, "  %s（%s）  %s\n", s.Service, s.Name, lookupText(statusText, s.Status))
	}
	if len(v.OtherContainers) > 0 {
		fmt.Fprintf(&b, "不归 AcornFox 管的容器 %d 个（只看）：\n", len(v.OtherContainers))
		for _, c := range v.OtherContainers {
			fmt.Fprintf(&b, "  %s\n", otherLine(c))
		}
	}
	for _, w := range v.Warnings {
		fmt.Fprintf(&b, "注意（%s）：%s\n", w.Code, w.Message)
	}
	b.WriteString("要完整的看 --json。\n")
	return b.String()
}
