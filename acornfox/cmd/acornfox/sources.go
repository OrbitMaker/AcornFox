package main

// `acornfox sources`: where this computer pulls Docker Hub and ghcr.io
// images from (the table in internal/runner/sources.go), optionally probed
// now. Read-only; runs in this binary without the server.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/runner"
)

const sourcesHelp = `用法：acornfox sources [--probe] [--image 镜像] [--json]
      acornfox sources --daemon-mirrors

看这台机器拉 Docker Hub、ghcr.io 镜像时用哪些下载源、按什么先后。下载源分层：
  我们自己的源（预留，现在还没有）→ 所在云的内网加速 → 别人公开的国内副本 → 上游自己（Docker Hub、ghcr.io）
国内的机器拉镜像前会先对每个源实测一次：拒绝访问、没有这个镜像、限流、连不上的跳过，慢的排到后面；
拉的过程中太慢也会换下一个源。国内的机器不用上游自己（Docker Hub、ghcr.io 在国内被屏蔽，不测它也不去它那里拉；
镜像源都不行时报 CAT-INST-12）；境外的机器先用上游自己。

  --probe            现在实测一遍：每个源取一次镜像清单和一段镜像层（每个源最多下 16 MB，整个最多约 10 秒），
                     写出能不能用、多快、镜像层实际从哪个地址下，以及拉镜像时会用的先后
  --image 镜像        --probe 时用这个镜像测（默认是常用的公共镜像 ` + runner.ProbeImage + `）
  --daemon-mirrors   只打印该写进 Docker 的 /etc/docker/daemon.json 里 registry-mirrors 的地址（一行一个，
                     已去掉连不上和拒绝访问的；安装脚本用它）。只在国内的机器上有内容
  --json             输出 JSON

没有副作用：只读配置、发几个只读的下载请求，不改任何设置。
网络区域（国内还是境外）Linux 服务器看 /etc/acornfox/install.env，Mac、Windows 看数据目录里的 network.json。
ACORNFOX_REGISTRY_MIRRORS、ACORNFOX_GHCR_MIRRORS（逗号分隔）可以整个换掉 Docker Hub、ghcr.io 的镜像源；
国内的机器能直连上游自己时（例如有代理），把 registry-1.docker.io、ghcr.io 写进去，它才会被测、被用；
/etc/docker/daemon.json 里不在表里的加速地址（例如安装时 --registry-mirror 给的）算你自己配的，排在最前。

例子：
  acornfox sources
  acornfox sources --probe
  acornfox sources --probe --image ghcr.io/组织/镜像:标签 --json

JSON：{"ok":true,"china":bool,"region_source":"…","docker_hub":[{"host","layer","layer_label","name"}],"ghcr":[…],
       "self":[],"upstream_skipped":["registry-1.docker.io"],"gateway":"…","probe":{"image":"…","results":[{"host","layer","status","mbps","final_host","detail"}],
       "order":["…"],"skipped":["…"]}}
     --daemon-mirrors --json：{"ok":true,"china":bool,"mirrors":["https://…"]}
`

// sourcesProbe and sourcesDaemonMirrors are replaced in tests.
var (
	sourcesProbe         = runner.ProbeSources
	sourcesDaemonMirrors = runner.DaemonMirrors
)

// daemonConfigFile is the Docker daemon configuration read for the user's
// own registry mirrors (tests point it elsewhere).
var daemonConfigFile = runner.DaemonConfigPath

type sourceOut struct {
	Host       string `json:"host"`
	Layer      string `json:"layer"`
	LayerLabel string `json:"layer_label"`
	Name       string `json:"name"`
	// LastResort: used only when the other sources cannot be used or were
	// given up as too slow, whatever the probe measured.
	LastResort bool `json:"last_resort,omitempty"`
}

type probeOut struct {
	Host      string  `json:"host"`
	Layer     string  `json:"layer"`
	Status    string  `json:"status"`
	MBps      float64 `json:"mbps"`
	FinalHost string  `json:"final_host,omitempty"`
	Detail    string  `json:"detail,omitempty"`
}

// sourcesRegistry is the pull order this computer's runner uses.
func sourcesRegistry(dataDir string) (runner.Registry, platform.Network) {
	n := setupNetwork(dataDir)
	if n.Source != "install.env" && !platform.DesktopMode() && platform.GOOS == "linux" {
		n = platform.Network{Source: "default"}
	}
	return runner.Registry{China: n.China, Mirrors: n.Mirrors}, n
}

func runSources(args []string, stdout, stderr io.Writer) int {
	var asJSON, probe, daemon bool
	image := runner.ProbeImage
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(stdout, sourcesHelp)
			return 0
		case a == "--json" || a == "-json":
			asJSON = true
		case a == "--probe" || a == "-probe":
			probe = true
		case a == "--daemon-mirrors" || a == "-daemon-mirrors":
			daemon = true
		case a == "--image" || a == "-image":
			if i+1 >= len(args) {
				return sourcesUsage(stdout, stderr, asJSON || hasJSONFlag(args), "--image 后面要写镜像，例如 镜像名:标签")
			}
			i++
			image = args[i]
		case strings.HasPrefix(a, "--image="):
			image = strings.TrimPrefix(a, "--image=")
		default:
			return sourcesUsage(stdout, stderr, asJSON || hasJSONFlag(args), "不认识的参数 "+a)
		}
	}
	if daemon && probe {
		return sourcesUsage(stdout, stderr, asJSON, "--daemon-mirrors 不和 --probe 一起用")
	}
	dataDir := platform.DefaultPaths().DataDir
	reg, n := sourcesRegistry(dataDir)
	ctx := context.Background()
	if daemon {
		var hosts []string
		if n.China {
			cloud := ""
			if n.Source == "install.env" {
				cloud = platform.LinuxInstallCloud(linuxInstallEnv)
			}
			hosts = sourcesDaemonMirrors(ctx, cloud)
		}
		urls := make([]string, 0, len(hosts))
		for _, h := range hosts {
			urls = append(urls, "https://"+h)
		}
		if asJSON {
			b, _ := json.Marshal(map[string]any{"ok": true, "china": n.China, "mirrors": urls})
			fmt.Fprintln(stdout, string(b))
			return 0
		}
		for _, u := range urls {
			fmt.Fprintln(stdout, u)
		}
		return 0
	}
	hub, ghcr := reg.Sources(), reg.GHCRSources()
	// In mainland China Docker Hub and ghcr.io themselves are blocked: the
	// runner neither probes nor pulls from them (runner.Registry.SkipsUpstream).
	upstreamSkipped := []string{}
	if reg.SkipsUpstream() {
		upstreamSkipped = append(upstreamSkipped, runner.DockerHubRegistry)
	}
	if reg.SkipsGHCRUpstream() {
		upstreamSkipped = append(upstreamSkipped, runner.GHCRRegistry)
	}
	out := map[string]any{"ok": true, "china": n.China, "region_source": n.Source,
		"docker_hub": describeSources(hub), "ghcr": describeSources(ghcr), "self": []sourceOut{},
		"upstream_skipped": upstreamSkipped, "gateway": gatewayBase(dataDir)}
	var results []probeOut
	var order, skipped []string
	if probe {
		srcs := hub
		if strings.HasPrefix(image, runner.GHCRRegistry+"/") {
			srcs = ghcr
		}
		t0 := time.Now()
		res := sourcesProbe(ctx, srcs, image, "")
		ord, skip := runner.RankSources(srcs, res)
		if !n.China {
			// Abroad the runner does not reorder by the probe: Docker Hub
			// first, the mirrors after it only when it stalls or fails.
			ord, skip = srcs, nil
		}
		for _, s := range srcs {
			r := res[s.Host]
			results = append(results, probeOut{Host: s.Host, Layer: string(s.Layer), Status: r.Status, MBps: r.MBps, FinalHost: r.FinalHost, Detail: r.Detail})
		}
		for _, s := range ord {
			order = append(order, s.Host)
		}
		for _, s := range skip {
			skipped = append(skipped, s.Host)
		}
		out["probe"] = map[string]any{"image": image, "seconds": int(time.Since(t0).Seconds() + 0.5), "results": results, "order": order, "skipped": skipped}
	}
	if asJSON {
		b, _ := json.Marshal(out)
		fmt.Fprintln(stdout, string(b))
		return 0
	}
	region := map[bool]string{true: "国内（拉镜像前先实测各个源）", false: "境外（先用上游自己）"}[n.China]
	fmt.Fprintf(stdout, "网络区域：%s；这个判断来自 %s\n", region, regionSourceText(n.Source))
	printSources(stdout, "Docker Hub 镜像的下载源（实测之前的先后）：", hub)
	printSources(stdout, "ghcr.io 镜像的下载源（实测之前的先后）：", ghcr)
	for _, h := range upstreamSkipped {
		env := "ACORNFOX_REGISTRY_MIRRORS"
		if h == runner.GHCRRegistry {
			env = "ACORNFOX_GHCR_MIRRORS"
		}
		fmt.Fprintf(stdout, "不用 %s 本身：国内被屏蔽，拉镜像时不测它也不去它那里拉（这台机器能直连它的话，把它写进环境变量 %s）\n", h, env)
	}
	fmt.Fprintln(stdout, "我们自己的源：还没有（预留）")
	if gw := gatewayBase(dataDir); gw != "" {
		fmt.Fprintf(stdout, "下载网关：%s（应用中心里写了镜像包的应用先从这里取，取不到才用上面的源）\n", gw)
	} else {
		fmt.Fprintln(stdout, "下载网关：已关掉")
	}
	if probe {
		fmt.Fprintf(stdout, "\n现在实测（用镜像 %s）：\n", image)
		for _, r := range results {
			fmt.Fprintf(stdout, "  %-28s %s\n", r.Host, probeText(r, n.China))
		}
		if n.China {
			fmt.Fprintf(stdout, "拉镜像时的先后：%s\n", strings.Join(order, " → "))
		} else {
			fmt.Fprintf(stdout, "拉镜像时的先后（境外不按实测重排，前一个没有进展、太慢或失败才换下一个）：%s\n", strings.Join(order, " → "))
		}
		if len(skipped) > 0 {
			fmt.Fprintf(stdout, "会跳过：%s\n", strings.Join(skipped, "、"))
		}
	} else {
		fmt.Fprintln(stdout, "\n要看每个源现在通不通、多快：acornfox sources --probe")
	}
	return 0
}

func regionSourceText(s string) string {
	switch s {
	case "install.env":
		return "安装时的判断（/etc/acornfox/install.env）"
	case "flag":
		return "准备运行环境时指定的（network.json）"
	case "detected":
		return "准备运行环境时自动判断的（network.json）"
	}
	return "默认值"
}

func describeSources(srcs []runner.ImageSource) []sourceOut {
	out := make([]sourceOut, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, sourceOut{Host: s.Host, Layer: string(s.Layer), LayerLabel: s.Layer.Label(), Name: s.Name, LastResort: s.LastResort})
	}
	return out
}

func printSources(w io.Writer, title string, srcs []runner.ImageSource) {
	fmt.Fprintln(w, title)
	for i, s := range srcs {
		note := ""
		if s.LastResort {
			note = "；兜底：别的源都不能用或拉的时候太慢才用它，实测再快也排最后"
		}
		fmt.Fprintf(w, "  %d. %-28s %s（%s%s）\n", i+1, s.Host, s.Name, s.Layer.Label(), note)
	}
}

// probeText is one probe result for people. Only in mainland China does the
// pull skip or reorder sources by the probe; abroad it is only a fact.
func probeText(r probeOut, china bool) string {
	switch r.Status {
	case runner.ProbeOK:
		s := fmt.Sprintf("能用，%.1f MB/s", r.MBps)
		if r.FinalHost != "" && r.FinalHost != r.Host {
			s += "（镜像层由 " + r.FinalHost + " 提供）"
		}
		return s
	case runner.ProbeSlow:
		if !china {
			return r.Detail
		}
		return "很慢，会排到后面：" + r.Detail
	case "":
		return "没测到"
	}
	if !china {
		return "不能用：" + r.Detail
	}
	return "不能用，会跳过：" + r.Detail
}

func sourcesUsage(stdout, stderr io.Writer, asJSON bool, msg string) int {
	if asJSON {
		b, _ := json.Marshal(map[string]any{"ok": false, "diagnosis": client.Diagnosis{Stage: "usage", Code: "usage", Message: msg, Hint: "acornfox sources --help 看用法"}})
		fmt.Fprintln(stdout, string(b))
		return 2
	}
	fmt.Fprintln(stderr, msg+"（acornfox sources --help 看用法）")
	return 2
}
