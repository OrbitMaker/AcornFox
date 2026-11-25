package main

// 平台有新版本提醒的接线（W12-3，internal/platformupdate）：服务启动时建一个 Checker，
// 后台每 6 小时查一次线上最新的正式版；结果经 GET /v1/status 的 platform_update 给页面、命令行和 doctor。
// 环境变量见 internal/platformupdate 的包说明（ACORNFOX_UPDATE_CHECK=off 关掉）。

import (
	"os"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/platformupdate"
)

// procVersionFile is where the kernel's version text is read (tests change it).
var procVersionFile = "/proc/version"

// newPlatformUpdater builds the checker for this server. The region is the
// one the download sources already use (downloadSources), read at most once a
// minute; the host kind (Linux, Windows' WSL, Mac's virtual machine) picks
// the upgrade command.
func newPlatformUpdater(o serverOptions) *platformupdate.Checker {
	var cfg platformupdate.Config
	cfg.FromEnv(os.Getenv)
	cfg.Current = cli.Version
	cfg.DataDir = o.DataDir
	pv, _ := os.ReadFile(procVersionFile)
	cfg.HostKind = platformupdate.DetectHostKind(o.WSL, string(pv), os.Getenv)

	var (
		mu    sync.Mutex
		at    time.Time
		china bool
	)
	cfg.China = func() bool {
		mu.Lock()
		defer mu.Unlock()
		if at.IsZero() || time.Since(at) > time.Minute {
			china, at = downloadSources(o.DataDir).China, time.Now()
		}
		return china
	}
	return platformupdate.New(cfg)
}
