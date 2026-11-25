package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/runner"
)

// `acornfox other-containers`: the containers on the server that AcornFox
// does not manage (the user started them with docker run, docker compose or
// another panel), read-only. Same data as GET /v1/host/containers and the
// console's "这台机器上的其他容器". There is no subcommand that changes one.

const otherContainersUsage = `用法：acornfox other-containers [--json]

用途：看这台服务器上不是从 AcornFox 装的容器（用户自己 docker run、docker compose 或别的面板装的）：名字、镜像、
      在跑还是停了、对外的端口、在跑的占了多少内存和 CPU。用户问“我自己装的容器能看到吗”“这台机器上还有别的什么在跑”时用。
      AcornFox 自己的应用、数据库和入口代理不会出现在这里。
      只能看，不能管：没有启动、停止、删除、接管的命令；要动它们请用户用 docker 自己处理。
      Windows、Mac 上程序跑在 AcornFox 自己那套环境里（WSL、Mac 的虚拟机），只能看到里面的容器，
      看不到系统外面 Docker Desktop 里的。
副作用：无，只读；每次都现场读（控制台页面几秒内共用一份）。读的是名字、镜像名、状态、端口和用量，不读环境变量、挂载、命令。
输出（--json）：
  {"ok":true,"observed_at":"…","running":2,"stopped":1,"memory_mb":120.5,"cpu_percent":1.3,"truncated":false,
   "containers":[{"id":"前 12 位","name","image","state":"running|exited|paused|restarting|created|dead","running":true,
                  "created":秒级时间戳,"ports":[{"host":6379,"container":6379,"proto":"tcp"}],"host_network":false,
                  "stats_available":true,"cpu_available":true,"cpu_percent":0.1,"memory_usage_mb":12.3}]}
  memory_mb、cpu_percent 是读到用量的在跑容器加起来的；一台机器列得下的容器最多 200 个（truncated 为 true 时有更多）。
  读不到时报 MACHINE-04（Docker 没回应，或服务器上的后台执行器太旧）：acornfox faults MACHINE-04。
退出码：0 正常（没有其他容器也是 0）；2 参数不对；3 连不上服务器；4 服务器拒绝（读不到容器是 MACHINE-04；服务器版本太旧没有这个接口是 server_too_old）。
例子：
  acornfox other-containers
  acornfox --json other-containers
  acornfox --target 家里的服务器 other-containers`

func (a *app) cmdOtherContainers(ctx context.Context, args []string) int {
	fs := a.catalogFlags("other-containers", otherContainersUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		return a.out.usageError("other-containers 不带参数；看某台服务器用 --target 服务器名")
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
	oa, ok := api.(client.OtherContainersAPI)
	if !ok {
		return a.out.fail(client.ServerTooOld(otherContainersWhat))
	}
	v, oerr := oa.OtherContainers(ctx)
	if oerr != nil {
		return a.out.fail(oerr)
	}
	if a.out.json {
		fields := map[string]any{}
		if len(v.Raw) > 0 {
			_ = json.Unmarshal(v.Raw, &fields)
		}
		a.out.emitJSON(fields)
		return exitOK
	}
	a.out.human("%s", strings.TrimRight(otherContainersText(v), "\n"))
	return exitOK
}

const otherContainersWhat = "这台机器上的其他容器（other-containers）"

// otherContainersText is the readable form.
func otherContainersText(v client.OtherContainersView) string {
	var b strings.Builder
	if len(v.Containers) == 0 {
		b.WriteString("这台机器上没有不是从 AcornFox 装的容器。\n")
		return b.String()
	}
	fmt.Fprintf(&b, "这台机器上不是从 AcornFox 装的容器 %d 个（在跑 %d，停了 %d）。只能看，不能在这里管。\n", len(v.Containers), v.Running, v.Stopped)
	for _, c := range v.Containers {
		fmt.Fprintf(&b, "  %s  %s  %s", c.Name, otherStateText(c), c.Image)
		switch {
		case c.HostNetwork:
			b.WriteString("  用本机的网络")
		case len(c.Ports) > 0:
			b.WriteString("  端口 " + otherPortsText(c.Ports))
		}
		if c.StatsAvailable {
			fmt.Fprintf(&b, "  内存 %.1f MB", c.MemoryUsageMB)
			if c.CPUAvailable {
				fmt.Fprintf(&b, "  CPU %.1f%%", c.CPUPercent)
			}
		}
		b.WriteString("\n")
	}
	if v.Running > 0 {
		fmt.Fprintf(&b, "在跑的合计：内存 %.1f MB，CPU %.1f%%\n", v.MemoryMB, v.CPUPercent)
	}
	if v.Truncated {
		b.WriteString("容器太多，只列了前面一部分。\n")
	}
	b.WriteString("Windows、Mac 上只看得到 AcornFox 自己那套环境里的容器，看不到系统外面 Docker Desktop 里的。\n")
	return b.String()
}

func otherStateText(c runner.OtherContainer) string {
	switch c.State {
	case "running":
		return "在跑"
	case "paused":
		return "暂停"
	case "restarting":
		return "在反复重启"
	case "created":
		return "没启动过"
	}
	return "停了"
}

func otherPortsText(ps []runner.OtherPort) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		s := strconv.Itoa(p.Host)
		if p.Container != p.Host {
			s += "→" + strconv.Itoa(p.Container)
		}
		if p.Proto != "" && p.Proto != "tcp" {
			s += "/" + p.Proto
		}
		out[i] = s
	}
	return strings.Join(out, "、")
}
