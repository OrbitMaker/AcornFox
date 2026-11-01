package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

func TestParseResourceCaps(t *testing.T) {
	for in, want := range map[string]int{"auto": 0, "26G": 26624, "1.5gb": 1536, "2048": 2048, "512M": 512} {
		if got, err := parseMemoryCap(in); err != nil || got != want {
			t.Errorf("parseMemoryCap(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for in, want := range map[string]int{"auto": 0, "8": 8000, "2.5": 2500} {
		if got, err := parseCPUCap(in); err != nil || got != want {
			t.Errorf("parseCPUCap(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "-1", "lots"} {
		if _, err := parseMemoryCap(bad); err == nil {
			t.Errorf("parseMemoryCap(%q) accepted", bad)
		}
	}
}

func TestLimitsShowsAndSetsCaps(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	view := func() client.Resources {
		var r client.Resources
		r.Memory.HostMB, r.Memory.CapMB, r.Memory.Auto, r.Memory.AllocatedMB = 32020, 27217, true, 1536
		r.CPU.HostMilli, r.CPU.CapMilli, r.CPU.Auto = 12000, 12000, true
		r.Items = append(r.Items, struct {
			App      string `json:"app"`
			Kind     string `json:"kind"`
			MemoryMB int    `json:"memory_mb"`
			CPUMilli int    `json:"cpu_milli"`
		}{"hello", "app", 512, 1000})
		return r
	}
	h.api.resourcesFn = func(context.Context) (client.Resources, error) { return view(), nil }
	var got client.ResourceCaps
	h.api.setCapsFn = func(_ context.Context, caps client.ResourceCaps) (client.Resources, error) {
		got = caps
		return view(), nil
	}

	code, out, errOut := h.run("limits")
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"已分配 1.5 GB / 上限 26.6 GB（自动）", "还可分配 25.1 GB", "单个应用最多 12.0 核", "hello"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}

	if code, _, errOut := h.run("limits", "--memory", "20G", "--cpu", "auto"); code != exitOK {
		t.Fatalf("set exit %d, stderr %q", code, errOut)
	}
	if got.MemoryMB == nil || *got.MemoryMB != 20480 || got.CPUMilli == nil || *got.CPUMilli != 0 {
		t.Fatalf("caps sent = %+v", got)
	}
}

func TestAppSetMinimumsAndAppliedNow(t *testing.T) {
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	if code, _, _ := h.run("--app", "shop", "app", "set", "--memory", "128"); code != exitUsage {
		t.Fatalf("128 MB exit %d, want usage error", code)
	}
	if code, _, _ := h.run("--app", "shop", "app", "set", "--cpu", "0.25"); code != exitUsage {
		t.Fatalf("0.25 core exit %d, want usage error", code)
	}
	h.api.updateAppFn = func(_ context.Context, app string, s client.AppSettings) (client.AppUpdate, error) {
		return client.AppUpdate{App: client.App{Name: app, MemoryMB: *s.MemoryMB, CPUMilli: 1000}, AppliedNow: true}, nil
	}
	code, out, errOut := h.run("--app", "shop", "app", "set", "--memory", "768")
	if code != exitOK || !strings.Contains(out, "已更新 shop：内存上限 768 MB") || !strings.Contains(out, "已立即生效，应用没有重启") {
		t.Fatalf("exit %d out %q err %q", code, out, errOut)
	}
}
