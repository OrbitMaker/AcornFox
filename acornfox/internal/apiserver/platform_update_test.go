package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/acornfox/acornfox/internal/platformupdate"
)

// fakeUpdater 是假的“平台新版本”检查：Status 给现有的，CheckNow 换成查到的新结果并数次数。
type fakeUpdater struct {
	st      platformupdate.Status
	after   platformupdate.Status
	checked int
}

func (f *fakeUpdater) Status() platformupdate.Status { return f.st }
func (f *fakeUpdater) CheckNow(context.Context) platformupdate.Status {
	f.checked++
	f.st = f.after
	return f.st
}

func newUpdateServer(t *testing.T, u PlatformUpdater) http.Handler {
	t.Helper()
	return New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(),
		PublicHost: "192.168.1.10", HostMetrics: fakeHostSized(8192, 4), Version: "0.7.0", PlatformUpdate: u})
}

func TestStatusCarriesPlatformUpdate(t *testing.T) {
	u := &fakeUpdater{st: platformupdate.Status{
		Enabled: true, Current: "0.7.0", Latest: "0.7.1", Available: true, OK: true, Source: "gitee",
		CheckedAt: "2026-11-25T10:00:00Z", NotesURL: "https://gitee.com/x/releases/tag/v0.7.1",
		UpgradeCommand: "curl -fsSL https://x | sudo bash -s -- upgrade", HostKind: "linux", Code: platformupdate.CodeAvailable,
	}}
	h := newUpdateServer(t, u)
	w := do(t, h, "GET", "/v1/status", nil, nil)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var body struct {
		Version        string                `json:"version"`
		LatestVersion  string                `json:"latest_version"`
		PlatformUpdate platformupdate.Status `json:"platform_update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.LatestVersion != "0.7.1" || !body.PlatformUpdate.Available || body.PlatformUpdate.Current != "0.7.0" ||
		body.PlatformUpdate.Source != "gitee" || body.PlatformUpdate.UpgradeCommand == "" || body.PlatformUpdate.Code != "PLAT-UPD-01" {
		t.Fatalf("status 里的新版本信息不对：%s", w.Body)
	}
	// GET /v1/platform/update 是同一个对象。
	w = do(t, h, "GET", "/v1/platform/update", nil, nil)
	var got platformupdate.Status
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got != body.PlatformUpdate {
		t.Fatalf("GET /v1/platform/update 要和 status 里的一样：%d %s", w.Code, w.Body)
	}
}

func TestStatusWithoutLatestHasNoLatestVersion(t *testing.T) {
	u := &fakeUpdater{st: platformupdate.Status{Enabled: true, Current: "0.7.0", HostKind: "linux"}}
	w := do(t, newUpdateServer(t, u), "GET", "/v1/status", nil, nil)
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if _, has := raw["latest_version"]; has {
		t.Fatalf("还没查到时不该有 latest_version：%s", w.Body)
	}
	if _, has := raw["platform_update"]; !has {
		t.Fatalf("没查到也要有 platform_update（enabled、current）：%s", w.Body)
	}
}

func TestStatusWithoutUpdaterHasNone(t *testing.T) {
	h, _, _, _, _ := newTestServer(t)
	w := do(t, h, "GET", "/v1/status", nil, nil)
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if _, has := raw["platform_update"]; has {
		t.Fatalf("没接检查的服务不该有 platform_update：%s", w.Body)
	}
	if w := do(t, h, "GET", "/v1/platform/update", nil, nil); w.Code != http.StatusNotFound {
		t.Fatalf("没接检查的服务 GET /v1/platform/update 应该 404，得到 %d", w.Code)
	}
	if w := do(t, h, "POST", "/v1/platform/update/check", nil, nil); w.Code != http.StatusNotFound {
		t.Fatalf("没接检查的服务 POST …/check 应该 404，得到 %d", w.Code)
	}
}

func TestPlatformUpdateCheckNow(t *testing.T) {
	u := &fakeUpdater{
		st:    platformupdate.Status{Enabled: true, Current: "0.7.0", HostKind: "linux"},
		after: platformupdate.Status{Enabled: true, Current: "0.7.0", Latest: "0.7.1", Available: true, OK: true, HostKind: "linux"},
	}
	h := newUpdateServer(t, u)
	w := do(t, h, "POST", "/v1/platform/update/check", nil, nil)
	var got platformupdate.Status
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Latest != "0.7.1" || !got.Available || u.checked != 1 {
		t.Fatalf("现在查一次：%d %s（查了 %d 次）", w.Code, w.Body, u.checked)
	}
	// 查完以后 status 里是新结果。
	w = do(t, h, "GET", "/v1/status", nil, nil)
	var body struct {
		LatestVersion string `json:"latest_version"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.LatestVersion != "0.7.1" {
		t.Fatalf("查完 status 里要是新结果：%s", w.Body)
	}
}
