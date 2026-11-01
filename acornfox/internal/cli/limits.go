package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/client"
)

// cmdLimits implements `limits [--memory 26G|26624|auto] [--cpu 12|auto]`:
// without flags it shows the server-wide caps and what each app and
// add-on has been given; with flags it changes the caps.
func (a *app) cmdLimits(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("limits", flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	var (
		memory = fs.String("memory", "", "最多分配给应用的内存：如 26G、2048（MB）；auto 恢复自动")
		cpu    = fs.String("cpu", "", "单个应用 CPU 上限的最大值（核）：如 8、2.5；auto 恢复自动")
	)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 0 {
		return a.out.usageError("limits 不接受位置参数；用 --memory、--cpu 修改")
	}
	var caps client.ResourceCaps
	if *memory != "" {
		mb, err := parseMemoryCap(*memory)
		if err != nil {
			return a.out.usageError("%s", err.Error())
		}
		caps.MemoryMB = &mb
	}
	if *cpu != "" {
		milli, err := parseCPUCap(*cpu)
		if err != nil {
			return a.out.usageError("%s", err.Error())
		}
		caps.CPUMilli = &milli
	}

	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	api, derr := a.dial(ctx, t)
	if derr != nil {
		return a.out.fail(derr)
	}
	defer api.Close()

	var r client.Resources
	var serr error
	if caps.MemoryMB != nil || caps.CPUMilli != nil {
		r, serr = api.SetResourceCaps(ctx, caps)
	} else {
		r, serr = api.Resources(ctx)
	}
	if serr != nil {
		return a.out.fail(serr)
	}
	if a.out.json {
		var m map[string]any
		b, _ := json.Marshal(r)
		_ = json.Unmarshal(b, &m)
		a.out.emitJSON(m)
		return exitOK
	}
	auto := func(b bool) string {
		if b {
			return "（自动）"
		}
		return ""
	}
	if r.Memory.CapMB > 0 {
		a.out.human("内存：已分配 %s / 上限 %s%s，整机 %s，还可分配 %s",
			mbText(r.Memory.AllocatedMB), mbText(r.Memory.CapMB), auto(r.Memory.Auto), mbText(r.Memory.HostMB), mbText(max(0, r.Memory.CapMB-r.Memory.AllocatedMB)))
	} else {
		a.out.human("内存：已分配 %s，没有上限（读不到整机内存）", mbText(r.Memory.AllocatedMB))
	}
	if r.CPU.CapMilli > 0 {
		a.out.human("CPU：单个应用最多 %s 核%s，整机 %s 核（CPU 允许超分，空闲时大家共用）",
			coresText(r.CPU.CapMilli), auto(r.CPU.Auto), coresText(r.CPU.HostMilli))
	}
	if len(r.Items) > 0 {
		a.out.human("")
		a.out.human("%s%s%s%s", padWidth("应用", 24), padWidth("类型", 12), padWidth("内存", 10), "CPU")
		for _, it := range r.Items {
			kind := it.Kind
			if kind == "app" {
				kind = "应用"
			}
			a.out.human("%s%s%s%s 核", padWidth(it.App, 24), padWidth(kind, 12), padWidth(mbText(it.MemoryMB), 10), coresText(it.CPUMilli))
		}
	}
	return exitOK
}

// parseMemoryCap accepts "auto", a size with G/GB/M/MB, or plain MB.
func parseMemoryCap(s string) (int, error) {
	v := strings.ToUpper(strings.TrimSpace(s))
	if v == "AUTO" {
		return 0, nil
	}
	unit := 1.0
	switch {
	case strings.HasSuffix(v, "GB"):
		v, unit = strings.TrimSuffix(v, "GB"), 1024
	case strings.HasSuffix(v, "G"):
		v, unit = strings.TrimSuffix(v, "G"), 1024
	case strings.HasSuffix(v, "MB"):
		v = strings.TrimSuffix(v, "MB")
	case strings.HasSuffix(v, "M"):
		v = strings.TrimSuffix(v, "M")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f <= 0 {
		return 0, fmt.Errorf("--memory 需要 auto 或大小，如 26G、2048")
	}
	return int(f*unit + 0.5), nil
}

// parseCPUCap accepts "auto" or a number of cores.
func parseCPUCap(s string) (int, error) {
	v := strings.TrimSpace(strings.ToLower(s))
	if v == "auto" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(v, "核"), 64)
	if err != nil || f <= 0 {
		return 0, fmt.Errorf("--cpu 需要 auto 或核数，如 8、2.5")
	}
	return int(f*1000 + 0.5), nil
}

func mbText(mb int) string {
	if mb >= 1024 {
		return strconv.FormatFloat(float64(mb)/1024, 'f', 1, 64) + " GB"
	}
	return strconv.Itoa(mb) + " MB"
}

func coresText(milli int) string {
	return strconv.FormatFloat(float64(milli)/1000, 'f', 1, 64)
}

// padWidth pads s with spaces to w terminal columns, counting CJK characters as
// two columns so Chinese headers line up with ASCII rows.
func padWidth(s string, w int) string {
	n := 0
	for _, r := range s {
		if r >= 0x2E80 {
			n += 2
		} else {
			n++
		}
	}
	if n >= w {
		return s + " "
	}
	return s + strings.Repeat(" ", w-n)
}
