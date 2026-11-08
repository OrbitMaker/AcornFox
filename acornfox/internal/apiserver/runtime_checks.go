package apiserver

// GET /v1/runtime/checks: the runtime page of the console. acornfox-server
// checks what it can see from inside the service, with the same fault codes
// as `acornfox doctor` (internal/setup Faults; the console joins each code to
// its title and next step through GET /v1/setup/faults):
//
//   - the service itself (it answered, so it runs) and the runner / Docker
//     (SETUP-SVC-01 on Linux, SETUP-RT-03 elsewhere),
//   - the gateway (Caddy) container (SETUP-EDGE-01),
//   - the runtime setup when it stopped (its own code),
//   - Linux only: free disk space of the data directory and Docker's data
//     root (SETUP-DISK-01), the ports AcornFox needs (SETUP-PORT-01..05) and
//     the clock (SETUP-CLOCK-01: network time sync, and the difference from
//     the download gateway's Date when the gateway is on; at most 3 seconds
//     each, skipped without network).
//
// It is read-only and takes a few hundred milliseconds. The service runs as
// an unprivileged user, so it cannot see who holds a port another user's
// process listens on: those are reported as SETUP-PORT-05 with the advice to
// run `sudo acornfox doctor`, never guessed.

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/reconcile"
	"github.com/acornfox/acornfox/internal/setup"
	"github.com/acornfox/acornfox/internal/state"
)

// runtimeCheck is one line of the report (same shape as `acornfox doctor --json`).
// Check names the check by its fault code, passed items included.
type runtimeCheck struct {
	OK    bool   `json:"ok"`
	Level string `json:"level"` // ok | warn | fail
	Text  string `json:"text"`
	Code  string `json:"code,omitempty"`
	Check string `json:"check,omitempty"`
}

// checkReport collects checks; it is a hostcheck.Reporter.
type checkReport struct {
	OK    bool
	Items []runtimeCheck
}

func (r *checkReport) Add(ok bool, code, format string, a ...any) {
	lvl, check := "ok", code
	if !ok {
		lvl, r.OK = "fail", false
	} else {
		code = ""
	}
	r.Items = append(r.Items, runtimeCheck{OK: ok, Level: lvl, Text: fmt.Sprintf(format, a...), Code: code, Check: check})
}

func (r *checkReport) Warn(code, format string, a ...any) {
	r.Items = append(r.Items, runtimeCheck{OK: true, Level: "warn", Text: fmt.Sprintf(format, a...), Code: code, Check: code})
}

// DownloadSources says where this server downloads from, in the order tried.
type DownloadSources struct {
	China        bool     `json:"china"`                   // mainland China: domestic sources first
	RegionSource string   `json:"region_source"`           // where China came from: install.env | flag | detected | default
	DockerHub    []string `json:"docker_hub"`              // registries a Docker Hub image is pulled from, in order
	GHCR         []string `json:"ghcr,omitempty"`          // mirrors tried for ghcr.io images before ghcr.io itself
	Gateway      string   `json:"gateway,omitempty"`       // the AcornFox download gateway ("" = off)
	DownloadBase string   `json:"download_base,omitempty"` // our own download source for runtime files
}

type runtimeChecksView struct {
	OS        string           `json:"os"`
	CheckedAt time.Time        `json:"checked_at"`
	OK        bool             `json:"ok"`
	Items     []runtimeCheck   `json:"items"`
	DataDir   string           `json:"data_dir,omitempty"`
	Docker    string           `json:"docker_version,omitempty"`
	Sources   *DownloadSources `json:"sources,omitempty"`
}

// runtimeGOOS is runtime.GOOS; tests switch it.
var runtimeGOOS = runtime.GOOS

// runtimeProcRoot and the disk functions are switched by tests.
var (
	runtimeProcRoot = "/proc"
	runtimeFree     = hostcheck.FreeBytes
	runtimeDevice   = hostcheck.Device
	runtimeIsRoot   = func() bool { return os.Geteuid() == 0 }
	// runtimeClockProbe builds the clock check's readers for the gateway
	// address ("" when the gateway is off: no time comparison).
	runtimeClockProbe = hostcheck.DefaultClockProbe
)

func (s *server) getRuntimeChecks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	rep := &checkReport{OK: true}
	v := runtimeChecksView{OS: runtimeGOOS, CheckedAt: time.Now().UTC(), DataDir: s.dataDir}

	apps, appsErr := s.store.ListApps(ctx)
	rep.Add(true, "SETUP-SVC-01", "后台服务：acornfox-server 运行中（%d 个应用）", len(apps))

	linux := runtimeGOOS == "linux"
	dockerRoot := ""
	if ping, err := s.runner.Ping(ctx); err != nil {
		if linux {
			rep.Add(false, "SETUP-SVC-01", "Docker：执行器 acornfox-runner 没有应答（%v）。运行 sudo systemctl restart acornfox-runner acornfox-server，再运行 sudo acornfox doctor", err)
		} else {
			rep.Add(false, "SETUP-RT-03", "Docker：连不上（%v）", err)
		}
	} else {
		v.Docker = ping.ServerVersion
		dockerRoot = ping.DockerRootDir
		rep.Add(true, "SETUP-RT-03", "Docker：%s 运行中（由执行器访问）", strings.TrimSpace(ping.ServerVersion))
	}

	edgeOK := false
	if s.edgeStatus != nil {
		switch st, msg := s.edgeStatus(); st {
		case reconcile.EdgeStateOff:
		case reconcile.EdgeStateRunning:
			edgeOK = true
			rep.Add(true, "SETUP-EDGE-01", "网关（Caddy）：运行中")
		case reconcile.EdgeStatePreparing:
			rep.Warn("SETUP-EDGE-01", "网关（Caddy）：正在准备（第一次启动要下载镜像）")
		default:
			if msg == "" {
				msg = "没有运行"
			}
			rep.Add(false, "SETUP-EDGE-01", "网关（Caddy）：%s", msg)
		}
	}

	if s.setup != nil {
		st := s.setup.Status()
		if st.State == setup.StateFailed && st.Error != nil {
			rep.Add(false, st.Error.Code, "准备运行环境停在“%s”：%s", st.Error.Step, st.Error.Message)
		}
	}

	if linux && s.dataDir != "" {
		hostcheck.CheckDisk(rep, s.dataDir, dockerRoot, runtimeFree, runtimeDevice)
	}
	if linux && s.checkPorts.Admin != 0 {
		use := hostcheck.PortUse{Own: map[int]bool{s.checkPorts.Admin: true}}
		if appsErr == nil {
			use.Known = true
			for _, a := range apps {
				if a.PublicPort > 0 && a.Desired == state.DesiredRunning && a.CurrentDeployment != "" {
					use.Own[a.PublicPort] = true
				}
			}
			if s.lanConsole != nil {
				if lc, err := s.lanConsole.Status(ctx); err == nil && lc.Enabled {
					use.Own[s.checkPorts.LAN] = true
				}
			}
			if s.publicConsole != nil {
				if pc, err := s.publicConsole.Status(ctx); err == nil && (pc.Enabled || pc.Verifying != "") {
					use.Users = append(use.Users, "公网控制台 "+pc.Domain)
				} else if err != nil {
					use.Known = false
				}
			}
			if ds, err := s.store.ListDomains(ctx, ""); err == nil {
				for _, d := range ds {
					use.Users = append(use.Users, "域名 "+d.Name)
				}
			} else {
				use.Known = false
			}
			// The gateway listens on 80/443 for bound domains and the
			// public console; while it runs those are AcornFox's own.
			if edgeOK && len(use.Users) > 0 {
				use.Own[80], use.Own[443] = true, true
			}
		}
		hostcheck.CheckPorts(rep, runtimeProcRoot, nil, runtimeIsRoot(), use, s.checkPorts)
	}
	if linux {
		gw := ""
		if s.sources != nil {
			gw = s.sources().Gateway
		}
		hostcheck.CheckClock(ctx, rep, runtimeClockProbe(gw))
	}

	v.OK, v.Items = rep.OK, rep.Items
	if s.sources != nil {
		src := s.sources()
		v.Sources = &src
	}
	writeJSON(w, http.StatusOK, v)
}
