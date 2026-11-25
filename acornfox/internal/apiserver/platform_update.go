package apiserver

import (
	"context"
	"net/http"

	"github.com/acornfox/acornfox/internal/platformupdate"
)

// 平台有新版本提醒（W12-3，package platformupdate）：
//
//	GET  /v1/status                带 latest_version（线上最新的正式版，查到过才有）和 platform_update 对象
//	GET  /v1/platform/update       同一个 platform_update 对象（没有别的字段）
//	POST /v1/platform/update/check 现在就去查一次，答同一个对象；几秒内重复点不重复请求（幂等）；只读线上，写数据目录里的 platform-update.json
//
// platform_update 的字段见 platformupdate.Status：enabled、current、latest、available、checked_at、ok、source（gitee 或 github）、
// notes_url、upgrade_command、upgrade_where、host_kind（linux、windows、mac）、last_ok_at、failures、unreachable、error、code
// （有新版本是 PLAT-UPD-01，一直查不到是 PLAT-UPD-02）。只提醒，没有“替我升级”的接口：平台升级要在服务器上运行命令。
// 没装这个功能的旧服务器：GET /v1/status 里没有 platform_update，这两条路由答 404，命令行把它当 server_too_old。
// 路由在受信入口和登录后的控制台上。

// PlatformUpdater is what the routes need of platformupdate.Checker.
type PlatformUpdater interface {
	Status() platformupdate.Status
	CheckNow(ctx context.Context) platformupdate.Status
}

func (s *server) registerPlatformUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/platform/update", s.getPlatformUpdate)
	mux.HandleFunc("POST /v1/platform/update/check", s.checkPlatformUpdate)
}

// addPlatformUpdate adds latest_version and platform_update to the status body.
func (s *server) addPlatformUpdate(body map[string]any) {
	if s.platformUpdate == nil {
		return
	}
	st := s.platformUpdate.Status()
	body["platform_update"] = st
	if st.Latest != "" {
		body["latest_version"] = st.Latest
	}
}

func (s *server) getPlatformUpdate(w http.ResponseWriter, r *http.Request) {
	if s.platformUpdate == nil {
		writeError(w, http.StatusNotFound, "not_found", "这个服务没有“平台新版本提醒”")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.platformUpdate.Status())
}

func (s *server) checkPlatformUpdate(w http.ResponseWriter, r *http.Request) {
	if s.platformUpdate == nil {
		writeError(w, http.StatusNotFound, "not_found", "这个服务没有“平台新版本提醒”")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, s.platformUpdate.CheckNow(r.Context()))
}
