package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// updateFake 是带“平台新版本提醒”的假服务器：status 里带 platform_update，也有 GET 和“现在查一次”。
type updateFake struct {
	*fakeAPI
	st      client.PlatformUpdate
	after   client.PlatformUpdate
	checked int
	err     error
}

func (u *updateFake) PlatformUpdate(context.Context) (client.PlatformUpdate, error) {
	return u.st, u.err
}

func (u *updateFake) CheckPlatformUpdate(context.Context) (client.PlatformUpdate, error) {
	u.checked++
	if u.err != nil {
		return client.PlatformUpdate{}, u.err
	}
	u.st = u.after
	return u.st, nil
}

func newerOnline() client.PlatformUpdate {
	return client.PlatformUpdate{
		Enabled: true, Current: "0.7.0", Latest: "0.7.1", Available: true, OK: true, Source: "gitee",
		CheckedAt: "2026-11-25T10:00:00Z", NotesURL: "https://gitee.com/VIP13390/AcornFox/releases/tag/v0.7.1",
		UpgradeCommand: "curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade",
		UpgradeWhere:   "在这台服务器上运行", HostKind: "linux", Code: "PLAT-UPD-01",
	}
}

func newUpdateHarness(t *testing.T, st client.PlatformUpdate) (*harness, *updateFake) {
	t.Helper()
	h, _ := statusHarness(t, func(string) bool { return true })
	uf := &updateFake{fakeAPI: h.api, st: st, after: st}
	h.api.statusFn = func(context.Context) (client.Status, error) {
		cp := uf.st
		s := client.Status{APIVersion: client.APIVersion, Runner: "ok", Version: cp.Current}
		s.PlatformUpdate = &cp
		s.LatestVersion = cp.Latest
		return s, nil
	}
	h.connect = func(context.Context, client.Target) (client.API, error) { return uf, nil }
	return h, uf
}

func TestUpdateShowsNewVersion(t *testing.T) {
	h, _ := newUpdateHarness(t, newerOnline())
	code, out, errOut := h.run("update")
	if code != exitOK {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	for _, w := range []string{"AcornFox 有新版本 0.7.1", "现在是 0.7.0", "Gitee", "发行说明：https://gitee.com/VIP13390/AcornFox/releases/tag/v0.7.1", "升级要先问用户", "get.sh | sudo bash -s -- upgrade"} {
		if !strings.Contains(out, w) {
			t.Errorf("输出里要有 %q：\n%s", w, out)
		}
	}
}

func TestUpdateJSONHasTheSameObject(t *testing.T) {
	h, _ := newUpdateHarness(t, newerOnline())
	code, out, _ := h.run("--json", "update")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, out)
	}
	m := decodeJSON(t, out)
	pu, _ := m["platform_update"].(map[string]any)
	if m["ok"] != true || m["latest_version"] != "0.7.1" || pu["available"] != true || pu["latest"] != "0.7.1" || pu["current"] != "0.7.0" ||
		pu["source"] != "gitee" || pu["host_kind"] != "linux" || pu["code"] != "PLAT-UPD-01" || pu["ok"] != true || pu["upgrade_command"] == "" {
		t.Fatalf("--json 的内容不对：%s", out)
	}
}

func TestUpdateAlreadyLatest(t *testing.T) {
	st := newerOnline()
	st.Current, st.Available, st.Code = "0.7.1", false, ""
	h, _ := newUpdateHarness(t, st)
	_, out, _ := h.run("update")
	if !strings.Contains(out, "已是最新") || strings.Contains(out, "get.sh") {
		t.Fatalf("已是最新时不给升级命令：%s", out)
	}
}

func TestUpdateNeverChecked(t *testing.T) {
	h, _ := newUpdateHarness(t, client.PlatformUpdate{Enabled: true, Current: "0.7.0", HostKind: "linux"})
	_, out, _ := h.run("update")
	if !strings.Contains(out, "还没查过") || !strings.Contains(out, "acornfox update check") {
		t.Fatalf("没查过要说明：%s", out)
	}
}

func TestUpdateFailing(t *testing.T) {
	st := client.PlatformUpdate{Enabled: true, Current: "0.7.0", HostKind: "linux", CheckedAt: "2026-11-25T10:00:00Z", Failures: 3, Error: "gitee：连不上；github：连不上"}
	h, _ := newUpdateHarness(t, st)
	_, out, _ := h.run("update")
	if !strings.Contains(out, "没能查到最新版本") || !strings.Contains(out, "连不上") {
		t.Fatalf("失败要写原因：%s", out)
	}
	st.Latest, st.Available, st.OK, st.Unreachable, st.LastOKAt, st.Source = "0.7.0", false, false, true, "2026-11-20T10:00:00Z", "github"
	h, _ = newUpdateHarness(t, st)
	_, out, _ = h.run("update")
	for _, w := range []string{"已是最新", "最近 3 次查询没成功", "好几天查不到最新版本", "PLAT-UPD-02"} {
		if !strings.Contains(out, w) {
			t.Errorf("要有 %q：\n%s", w, out)
		}
	}
}

func TestUpdateDisabled(t *testing.T) {
	h, _ := newUpdateHarness(t, client.PlatformUpdate{Enabled: false, Current: "0.7.0", HostKind: "linux"})
	_, out, _ := h.run("update")
	if !strings.Contains(out, "已关闭") || !strings.Contains(out, "ACORNFOX_UPDATE_CHECK=off") {
		t.Fatalf("关掉时要说明：%s", out)
	}
}

func TestUpdateCheckAsksTheServer(t *testing.T) {
	h, uf := newUpdateHarness(t, client.PlatformUpdate{Enabled: true, Current: "0.7.0", HostKind: "linux"})
	uf.after = newerOnline()
	code, out, _ := h.run("--json", "update", "check")
	if code != exitOK || uf.checked != 1 {
		t.Fatalf("exit %d，查了 %d 次：%s", code, uf.checked, out)
	}
	if m := decodeJSON(t, out); m["latest_version"] != "0.7.1" {
		t.Fatalf("check 要给新结果：%s", out)
	}
	// 只看结果不会让服务器去查。
	if _, _, _ = h.run("update"); uf.checked != 1 {
		t.Fatalf("update 只读，不该触发查询：查了 %d 次", uf.checked)
	}
}

func TestUpdateUsageErrors(t *testing.T) {
	h, _ := newUpdateHarness(t, newerOnline())
	if code, _, _ := h.run("update", "now"); code != exitUsage {
		t.Errorf("不认识的子命令要退出码 2，得到 %d", code)
	}
	if code, _, _ := h.run("update", "check", "extra"); code != exitUsage {
		t.Errorf("多余的参数要退出码 2，得到 %d", code)
	}
}

func TestUpdateOnOldServer(t *testing.T) {
	// 旧服务器（没有这个接口，也没实现 PlatformUpdateAPI）：server_too_old。
	h := newHarness(t)
	h.addTarget("dev", "http://dev")
	code, out, errOut := h.run("--json", "update")
	if code == exitOK || !strings.Contains(out+errOut, "server_too_old") {
		t.Fatalf("旧服务器要 server_too_old：exit %d %s %s", code, out, errOut)
	}
}

func TestStatusCarriesPlatformUpdate(t *testing.T) {
	h, _ := newUpdateHarness(t, newerOnline())
	code, out, _ := h.run("--json", "status", "hello")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, out)
	}
	m := decodeJSON(t, out)
	pu, _ := m["platform_update"].(map[string]any)
	if m["latest_version"] != "0.7.1" || pu["available"] != true || pu["upgrade_command"] == "" {
		t.Fatalf("status --json 要带 platform_update：%s", out)
	}
	if m["app"] != "hello" {
		t.Fatalf("应用状态不该被挤掉：%s", out)
	}
	// 部署编号那条路也带。
	_, out, _ = h.run("--json", "status", "3f2a9c0d1b7e")
	if decodeJSON(t, out)["latest_version"] != "0.7.1" {
		t.Fatalf("部署状态也要带：%s", out)
	}
	// 人看的输出：最后多一行提醒。
	_, out, _ = h.run("status", "hello")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "AcornFox 有新版本 0.7.1") || !strings.Contains(last, "acornfox update") {
		t.Fatalf("status 最后一行要提醒有新版本：%s", out)
	}
}

func TestStatusQuietWhenLatestOrOld(t *testing.T) {
	st := newerOnline()
	st.Current, st.Available = "0.7.1", false
	h, _ := newUpdateHarness(t, st)
	_, out, _ := h.run("status", "hello")
	if strings.Contains(out, "新版本") {
		t.Fatalf("已是最新不该提醒：%s", out)
	}
	// 旧服务器的 status 里没有 platform_update：JSON 里也没有这个字段。
	h2, _ := statusHarness(t, func(string) bool { return true })
	_, out, _ = h2.run("--json", "status", "hello")
	if _, has := decodeJSON(t, out)["platform_update"]; has {
		t.Fatalf("旧服务器不该有 platform_update：%s", out)
	}
}

func TestPlatformUpdateLinesUsesLocalTime(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	lines := strings.Join(platformUpdateLines(newerOnline(), loc), "\n")
	if !strings.Contains(lines, "2026-11-25 18:00") {
		t.Fatalf("时间要换成本地时间：%s", lines)
	}
}
