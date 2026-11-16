package cli

// `acornfox open --detach` and `acornfox open --stop`: the console tunnel of
// an SSH target kept in the background, for an AI workbench (which runs
// one command at a time and cannot press Ctrl+C) or a user who does not
// want to keep a terminal open.
//
// --detach starts ssh in a session of its own (client.OpenDetachedTunnel),
// opens the browser and returns at once; the tunnel stays up after this
// command, its terminal or the workbench ends, until `open --stop`, the
// connection drops or the computer restarts. One tunnel per target: run
// again while it is up, it is reused (a new one-time login link, the same
// port). The record of it is <UserConfigDir>/acornfox/open-tunnels.json
// (target name → pid, local port, when it started), 0600.
//
// --stop ends the background tunnel of the target and drops the record;
// none up is not an error (run twice, same result). A pid is ended only
// while it still forwards the recorded port (client.TunnelAlive), so a pid
// the system reused is never killed.
//
// A target reached by URL (LAN or public console) or this computer's own
// AcornFox needs no tunnel: --detach opens the browser and returns.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

const openTunnelsFileName = "open-tunnels.json"

// detachedRecord is one background console tunnel.
type detachedRecord struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Started string `json:"started"`
}

type openTunnelsFile struct {
	Tunnels map[string]detachedRecord `json:"tunnels"`
}

func openTunnelsPath(configDir string) (string, error) {
	dir, _, err := configPaths(configDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, openTunnelsFileName), nil
}

// loadOpenTunnels reads the record; a missing or unreadable one is empty
// (the worst case is a tunnel not reused, never a wrong pid killed).
func loadOpenTunnels(configDir string) openTunnelsFile {
	f := openTunnelsFile{Tunnels: map[string]detachedRecord{}}
	p, err := openTunnelsPath(configDir)
	if err != nil {
		return f
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return f
	}
	if json.Unmarshal(b, &f) != nil || f.Tunnels == nil {
		f.Tunnels = map[string]detachedRecord{}
	}
	return f
}

func saveOpenTunnels(configDir string, f openTunnelsFile) error {
	p, err := openTunnelsPath(configDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// detachedConsoleTunnel returns the background console tunnel of t: the
// recorded one when it is still up (reused true) and on the port asked for
// (port 0: any), otherwise a new one, recorded. A recorded one up on
// another port than asked is stopped first.
func (a *app) detachedConsoleTunnel(ctx context.Context, t client.Target, port int) (*client.DetachedTunnel, bool, error) {
	recs := loadOpenTunnels(a.configDir)
	if r, ok := recs.Tunnels[t.Name]; ok && a.tunnelAliveFn()(r.PID, r.Port) {
		if port == 0 || port == r.Port {
			return &client.DetachedTunnel{LocalURL: "http://127.0.0.1:" + strconv.Itoa(r.Port), PID: r.PID}, true, nil
		}
		_ = a.stopPIDFn()(r.PID)
	}
	localPort := port
	if localPort == 0 {
		p, err := a.pickFreePort()
		if err != nil {
			return nil, false, &client.Error{Diag: client.Diagnosis{Stage: "connect", Code: "connect_failed", Message: "无法分配本地端口：" + err.Error()}}
		}
		localPort = p
	}
	dt, err := a.openDetachedFn()(ctx, t, localPort)
	if err != nil {
		return nil, false, err
	}
	recs.Tunnels[t.Name] = detachedRecord{PID: dt.PID, Port: localPort, Started: time.Now().UTC().Format(time.RFC3339)}
	if err := saveOpenTunnels(a.configDir, recs); err != nil && !a.out.json {
		a.out.humanErr("没记下后台隧道（%v）：acornfox open --stop 关不掉它，要关就结束进程 %d", err, dt.PID)
	}
	return dt, false, nil
}

// openStopCommand is the command that ends the background tunnel of the
// target this run used.
func (a *app) openStopCommand() string {
	if a.flagTarget != "" {
		return "acornfox --target " + a.flagTarget + " open --stop"
	}
	return "acornfox open --stop"
}

// cmdOpenStop implements `open --stop`.
func (a *app) cmdOpenStop() int {
	t, _, err := a.resolveTarget()
	if err != nil {
		return a.out.usageError("%s", err.Error())
	}
	recs := loadOpenTunnels(a.configDir)
	stopped := []int{}
	if r, ok := recs.Tunnels[t.Name]; ok {
		if a.tunnelAliveFn()(r.PID, r.Port) {
			if err := a.stopPIDFn()(r.PID); err != nil {
				return a.out.fail(&client.Error{Diag: client.Diagnosis{Stage: "connect", Code: "connect_failed",
					Message: "关不掉后台隧道（进程 " + strconv.Itoa(r.PID) + "）：" + err.Error()}})
			}
			stopped = append(stopped, r.PID)
		}
		delete(recs.Tunnels, t.Name)
		if err := saveOpenTunnels(a.configDir, recs); err != nil && !errors.Is(err, os.ErrNotExist) && !a.out.json {
			a.out.humanErr("隧道已关，但记录没改成（%v）", err)
		}
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"target": t.Name, "stopped": stopped})
		return exitOK
	}
	if len(stopped) == 0 {
		a.out.human("%s 没有开着的后台隧道。", t.Name)
	} else {
		a.out.human("已关闭 %s 的后台隧道（进程 %d）。", t.Name, stopped[0])
	}
	return exitOK
}

// openDetachedFn returns the injected detached tunnel starter or the real one.
func (a *app) openDetachedFn() func(ctx context.Context, t client.Target, localPort int) (*client.DetachedTunnel, error) {
	if a.openDetached != nil {
		return a.openDetached
	}
	return client.OpenDetachedConsoleTunnel
}

// tunnelAliveFn returns the injected liveness check or the real one.
func (a *app) tunnelAliveFn() func(pid, port int) bool {
	if a.tunnelAlive != nil {
		return a.tunnelAlive
	}
	return client.TunnelAlive
}

// stopPIDFn returns the injected process stopper or the real one.
func (a *app) stopPIDFn() func(pid int) error {
	if a.stopPID != nil {
		return a.stopPID
	}
	return client.StopProcess
}

// reportDetached reports `open --detach` and returns at once: whatever
// tunnel there is stays up in the background (dt; nil when the target needs
// none). The one-time login link is printed only when the browser did not
// open.
func (a *app) reportDetached(baseURL, loginURL string, browserOpened bool, dt *client.DetachedTunnel, reused bool) int {
	note := "浏览器里是 AcornFox 控制台。"
	if dt != nil {
		note = "隧道在后台开着，不用一直开着终端；电脑重启、断网后要重新运行 acornfox open --detach。用完关掉运行 " + a.openStopCommand() + "。"
	}
	if a.out.json {
		out := map[string]any{"url": baseURL, "browser_opened": browserOpened, "note": note}
		if dt != nil {
			out["pid"] = dt.PID
			out["reused"] = reused
			out["stop_command"] = a.openStopCommand()
		}
		if !browserOpened {
			out["login_url"] = loginURL
			out["login_url_note"] = "一次性、60 秒内有效；过期就再运行一次 acornfox open --detach"
		}
		a.out.emitJSON(out)
		return exitOK
	}
	if browserOpened {
		a.out.human("已在浏览器打开控制台：%s", baseURL)
	} else {
		a.out.human("没有打开浏览器。请在浏览器打开下面的登录链接（一次性，60 秒内有效）：")
		a.out.human("%s", loginURL)
	}
	if dt != nil {
		if reused {
			a.out.human("沿用已经开着的后台隧道（进程 %d）。", dt.PID)
		} else {
			a.out.human("隧道已在后台开着（进程 %d）。", dt.PID)
		}
	}
	a.out.human("%s", note)
	return exitOK
}
