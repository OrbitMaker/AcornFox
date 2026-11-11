package cli

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// cmdStats implements `stats [APP]`: show host or app resource metrics.
func (a *app) cmdStats(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	history := fs.String("history", "", "看这个应用近一段时间的 CPU、内存：1h 或 24h（只读；要带应用名）")
	fs.Usage = func() {
		fmt.Fprint(a.out.stderr, `用法：acornfox stats [应用] [--history 1h|24h] [--json]

看资源使用，只读，不改任何东西。
  acornfox stats                     整台服务器现在的 CPU、内存、磁盘
  acornfox stats shop                应用 shop 现在的 CPU、内存、网络、进程数
  acornfox stats shop --history 1h   shop 近 1 小时的 CPU、内存（每 30 秒一个点）
  acornfox stats shop --history 24h  shop 近 24 小时（每 5 分钟一个平均值）
  加 --json 输出全部的点，字段和控制台概览页的小图用的是同一个接口。

历史只存在后台服务的内存里：最多 24 小时，服务重启（包括升级）后从头记；
应用停着的时候不记，那段时间没有点。
`)
	}
	rest, err := parseFlags(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(rest) > 1 {
		return a.out.usageError("stats 最多接受一个应用名参数")
	}
	if *history != "" {
		if *history != "1h" && *history != "24h" {
			return a.out.usageError("--history 只能是 1h 或 24h（历史只存在内存里，没有更长的）")
		}
		name := a.flagApp
		if len(rest) == 1 {
			name = rest[0]
		}
		if name == "" {
			return a.out.usageError("--history 要带应用名，例如：acornfox stats shop --history 1h（整台服务器的历史在控制台首页看）")
		}
		return a.statsAppHistory(ctx, name, *history)
	}

	// stats --app NAME behaves like stats NAME, as --app does for other commands.
	if len(rest) == 0 && a.flagApp != "" {
		return a.statsApp(ctx, a.flagApp)
	}
	// stats without app name = host metrics
	if len(rest) == 0 {
		return a.statsHost(ctx)
	}

	// stats APP = app metrics
	return a.statsApp(ctx, rest[0])
}

func (a *app) statsHost(ctx context.Context) int {
	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	metrics, serr := api.HostMetrics(ctx)
	if serr != nil {
		return a.out.fail(serr)
	}

	if a.out.json {
		a.out.emitJSON(map[string]any{
			"available":      metrics.Available,
			"cpu_percent":    metrics.CPUPercent,
			"memory_used":    metrics.MemoryUsed,
			"memory_total":   metrics.MemoryTotal,
			"disk_used":      metrics.DiskUsed,
			"disk_total":     metrics.DiskTotal,
			"load1":          metrics.Load1,
			"uptime_seconds": metrics.UptimeSeconds,
		})
		return exitOK
	}

	if !metrics.Available {
		a.out.human("主机指标不可用")
		return exitOK
	}

	a.out.human("=== 主机资源使用情况 ===")
	if metrics.CPUPercent != nil {
		a.out.human("CPU 使用率：%.2f%%", *metrics.CPUPercent)
	}
	if metrics.MemoryUsed != nil && metrics.MemoryTotal != nil {
		usedGB := float64(*metrics.MemoryUsed) / (1024 * 1024 * 1024)
		totalGB := float64(*metrics.MemoryTotal) / (1024 * 1024 * 1024)
		percent := float64(*metrics.MemoryUsed) / float64(*metrics.MemoryTotal) * 100
		a.out.human("内存使用：%.2f GB / %.2f GB (%.1f%%)", usedGB, totalGB, percent)
	}
	if metrics.DiskUsed != nil && metrics.DiskTotal != nil {
		usedGB := float64(*metrics.DiskUsed) / (1024 * 1024 * 1024)
		totalGB := float64(*metrics.DiskTotal) / (1024 * 1024 * 1024)
		percent := float64(*metrics.DiskUsed) / float64(*metrics.DiskTotal) * 100
		a.out.human("磁盘使用：%.2f GB / %.2f GB (%.1f%%)", usedGB, totalGB, percent)
	}
	if metrics.Load1 != nil {
		a.out.human("负载（1分钟）：%.2f", *metrics.Load1)
	}
	if metrics.UptimeSeconds != nil {
		hours := *metrics.UptimeSeconds / 3600
		a.out.human("系统运行时间：%.1f 小时", hours)
	}

	return exitOK
}

func (a *app) statsApp(ctx context.Context, appName string) int {
	// The positional app name always wins over .acornfox / the working dir.
	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}

	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	metrics, serr := api.AppMetrics(ctx, appName)
	if serr != nil {
		return a.out.fail(serr)
	}

	if a.out.json {
		a.out.emitJSON(map[string]any{
			"app":             metrics.App,
			"available":       metrics.Available,
			"observed_state":  metrics.ObservedState,
			"cpu_available":   metrics.CPUAvailable,
			"cpu_percent":     metrics.CPUPercent,
			"memory_usage_mb": metrics.MemoryUsageMB,
			"memory_limit_mb": metrics.MemoryLimitMB,
			"network_rx_mb":   metrics.NetworkRxMB,
			"network_tx_mb":   metrics.NetworkTxMB,
			"pids":            metrics.PIDs,
		})
		return exitOK
	}

	if metrics.Available != nil && !*metrics.Available {
		a.out.human("应用 %s 指标不可用（状态：%s）", metrics.App, metrics.ObservedState)
		return exitOK
	}
	a.out.human("=== 应用 %s 资源使用情况 ===", metrics.App)
	if metrics.CPUAvailable == nil || *metrics.CPUAvailable {
		a.out.human("CPU 使用率：%.2f%%", metrics.CPUPercent)
	} else {
		a.out.human("CPU 使用率：采样中")
	}
	if metrics.MemoryLimitMB > 0 {
		a.out.human("内存使用：%.2f MB / %.2f MB (%.1f%%)",
			metrics.MemoryUsageMB,
			metrics.MemoryLimitMB,
			metrics.MemoryUsageMB/metrics.MemoryLimitMB*100)
	} else {
		a.out.human("内存使用：%.2f MB（无限制）", metrics.MemoryUsageMB)
	}
	a.out.human("网络接收：%.2f MB", metrics.NetworkRxMB)
	a.out.human("网络发送：%.2f MB", metrics.NetworkTxMB)
	a.out.human("进程数：%d", metrics.PIDs)

	return exitOK
}

// statsAppHistory implements `stats APP --history 1h|24h`: a summary of the
// app's CPU and memory over the range, or every point with --json. Read-only.
func (a *app) statsAppHistory(ctx context.Context, appName, rng string) int {
	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	h, herr := api.AppHistory(ctx, appName, rng)
	if herr != nil {
		return a.out.fail(herr)
	}
	if h.Points == nil {
		h.Points = []client.AppHistoryPoint{}
	}
	if h.Ranges == nil {
		h.Ranges = []string{}
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{
			"app": h.App, "range": h.Range, "step_seconds": h.StepSeconds,
			"ranges": h.Ranges, "since": h.Since, "points": h.Points,
		})
		return exitOK
	}
	label := map[string]string{"1h": "近 1 小时", "24h": "近 24 小时"}[rng]
	if len(h.Ranges) == 0 {
		a.out.human("这台服务器没有记录应用的历史（后台服务版本较旧，升级后才有）。现在的数字用：acornfox stats %s", appName)
		return exitOK
	}
	if len(h.Points) == 0 {
		a.out.human("应用 %s %s还没有记录。历史每 30 秒记一次、只存在内存里，服务重启后从头记；应用停着的时候不记。", appName, label)
		return exitOK
	}
	var memMin, memMax, memSum, cpuMax, cpuSum float64
	var nMem, nCPU int
	for _, p := range h.Points {
		if p.MemoryUsageMB != nil {
			v := *p.MemoryUsageMB
			if nMem == 0 || v < memMin {
				memMin = v
			}
			if v > memMax {
				memMax = v
			}
			memSum += v
			nMem++
		}
		if p.CPUPercent != nil {
			if *p.CPUPercent > cpuMax {
				cpuMax = *p.CPUPercent
			}
			cpuSum += *p.CPUPercent
			nCPU++
		}
	}
	first, last := h.Points[0].T.Local(), h.Points[len(h.Points)-1].T.Local()
	a.out.human("=== 应用 %s %s的资源使用 ===", appName, label)
	a.out.human("记录：%d 个点，每 %d 秒一个，%s ～ %s", len(h.Points), h.StepSeconds, first.Format("01-02 15:04"), last.Format("01-02 15:04"))
	if nMem > 0 {
		a.out.human("内存：最低 %.1f MB，平均 %.1f MB，最高 %.1f MB", memMin, memSum/float64(nMem), memMax)
	}
	if nCPU > 0 {
		a.out.human("CPU：平均 %.2f%%，最高 %.2f%%", cpuSum/float64(nCPU), cpuMax)
	}
	if h.Since != nil && h.Since.After(time.Now().Add(-map[string]time.Duration{"1h": time.Hour, "24h": 24 * time.Hour}[rng])) {
		a.out.human("注意：记录从 %s 才开始（后台服务那时启动，或应用那时才装好），不满 %s。", h.Since.Local().Format("01-02 15:04"), label[len("近 "):])
	}
	a.out.human("要全部的点：加 --json")
	return exitOK
}
