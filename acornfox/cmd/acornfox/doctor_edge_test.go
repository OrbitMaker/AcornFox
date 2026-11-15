package main

import "testing"

// The doctor line ends with （EDGE-03） already; the reason from the entry
// client must not repeat the code.
func TestDoctorEdgeReasonNamesCodeOnce(t *testing.T) {
	for in, want := range map[string]string{
		"入口代理的管理口拒绝了令牌（401，故障编号 EDGE-03）":               "入口代理的管理口拒绝了令牌（401）",
		"入口代理的管理口没有应答（故障编号 EDGE-03）：connection refused": "入口代理的管理口没有应答：connection refused",
		"别的原因": "别的原因",
	} {
		if got := withoutCode(in, "EDGE-03"); got != want {
			t.Errorf("withoutCode(%q) = %q, want %q", in, got, want)
		}
	}
}
