package main

// Linux server checks of `acornfox doctor` besides Docker: whether the
// acornfox-server service answers, free disk space of the data directory and
// Docker's data root, the ports AcornFox needs, the clock, the download
// gateway and the app catalog sync (fault codes SETUP-SVC-01, SETUP-DISK-01,
// SETUP-PORT-01..05, SETUP-CLOCK-01, SETUP-GW-01 and SETUP-SYNC-01; the full
// entries are in internal/faults, `acornfox faults <code>`).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	dockerclient "github.com/moby/moby/client"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/publicconsole"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Ports of a Linux server, matching the defaults of `acornfox server`
// (-public-port-min/-max, the edge admin port and the LAN console port).
const (
	linuxAppPortMin = 18810
	linuxAppPortMax = 18899
)

// doctorGatewayProbes builds the readers of the clock check and the download
// gateway check for the gateway's address ("" when the gateway is off); the
// two share one HEAD request to the gateway. Tests replace it.
var doctorGatewayProbes = hostcheck.DefaultGatewayProbes

// doctorCatalogStatus asks acornfox-server for the app catalog's sync
// status; ok is false when it cannot be asked (tests replace it).
var doctorCatalogStatus = queryCatalogSync

// defaultDockerRoot is used when Docker does not answer.
const defaultDockerRoot = hostcheck.DefaultDockerRoot

// serviceProbe is one answer (or error) of acornfox-server's GET /v1/status,
// asked once per doctor run and shared by the Docker, service and port checks.
type serviceProbe struct {
	st  client.Status
	err error
}

// probeService asks acornfox-server for its status.
func probeService(ctx context.Context) serviceProbe {
	st, err := waitAPI(ctx, platform.DefaultPaths().APISocket, 3*time.Second)
	return serviceProbe{st: st, err: err}
}

// healthy reports whether the server answered and its runner reaches Docker.
func (s serviceProbe) healthy() bool { return s.err == nil && s.st.Runner == "ok" }

// dockerViaService handles a Linux user who may not open Docker's socket.
// acornfox-runner talks to Docker for everyone; a user in the acornfox-users
// group only needs the API socket. When Docker refused this user for lack of
// permission but the server answers with a working runner, Docker is reported
// as fine and dockerViaService returns true. Otherwise it adds nothing.
func dockerViaService(r *doctorReport, dockerErr error, svc serviceProbe) bool {
	if dockerErr == nil || !socketPermissionDenied(dockerErr) || !svc.healthy() {
		return false
	}
	ver := ""
	if v := strings.TrimSpace(svc.st.DockerVersion); v != "" {
		ver = "（Docker " + v + "）"
	}
	r.add(true, "SETUP-RT-03", "Docker：由后台服务访问 Docker%s，当前用户不需要 Docker 权限", ver)
	return true
}

// linuxHostChecks adds the service, disk and port checks. dc is nil when
// Docker did not answer; svc is the server's status from probeService.
func linuxHostChecks(ctx context.Context, r *doctorReport, dc *dockerclient.Client, dockerRoot string, svc serviceProbe) {
	socket := platform.DefaultPaths().APISocket
	use := serverPortUse{}
	if doctorService(r, func() (client.Status, error) { return svc.st, svc.err }) {
		use = queryServerPortUse(ctx, socket, svc.healthy())
	}
	doctorDisk(r, platform.DefaultPaths().DataDir, dockerRoot, hostcheck.FreeBytes, hostcheck.Device)
	doctorPorts(r, "/proc", portCheckContainers(ctx, dc), os.Geteuid() == 0, use)
	doctorClockAndGateway(ctx, r, platform.DefaultPaths().DataDir)
	if svc.err == nil {
		doctorCatalog(ctx, r, socket)
	}
	doctorLANConsole(r, use.LANNoPassword)
}

// doctorClockAndGateway checks the clock and the download gateway this
// server uses (gatewayBase: the same address as the app center) with one
// HEAD request to the gateway, at most 5 seconds.
//
// A user who may not read the data directory (not root, not the service
// account) cannot see whether network.json turned the gateway off. doctor
// then does not contact the gateway at all: the clock is judged by the
// system's time sync alone, and the gateway line says to run it with sudo.
// Turning the gateway off must stop every self check from contacting it.
func doctorClockAndGateway(ctx context.Context, r *doctorReport, dataDir string) {
	if !gatewaySettingReadable(dataDir) {
		clock, _ := doctorGatewayProbes("")
		hostcheck.CheckClock(ctx, r, clock)
		r.add(true, "SETUP-GW-01", "下载网关：当前用户读不到这台服务器的网关设置（数据目录里的 network.json，可能已经关掉），这一项没查，也不联系网关；要查用 sudo acornfox doctor")
		return
	}
	clock, gw := doctorGatewayProbes(gatewayBase(dataDir))
	hostcheck.CheckClock(ctx, r, clock)
	hostcheck.CheckGateway(ctx, r, gw)
}

// gatewaySettingReadable reports whether this process can tell the gateway
// setting: ACORNFOX_GATEWAY is set, or network.json can be read, or it is
// known not to exist. Only a permission error leaves it unknown.
func gatewaySettingReadable(dataDir string) bool {
	if _, ok := os.LookupEnv("ACORNFOX_GATEWAY"); ok {
		return true
	}
	_, err := os.ReadFile(platform.NetworkFile(dataDir))
	return !errors.Is(err, fs.ErrPermission)
}

// doctorCatalog reports the app catalog's last sync as acornfox-server
// recorded it. Nothing is added when the server cannot be asked (its own
// line already says so) or has no app center.
func doctorCatalog(ctx context.Context, r *doctorReport, socket string) {
	s, ok := doctorCatalogStatus(ctx, socket)
	if !ok {
		return
	}
	hostcheck.CheckCatalogSync(r, s, time.Now())
}

// queryCatalogSync reads GET /v1/catalog of acornfox-server.
func queryCatalogSync(ctx context.Context, socket string) (hostcheck.CatalogSync, bool) {
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := client.Connect(qctx, client.Target{URL: unixURL(socket)})
	if err != nil {
		return hostcheck.CatalogSync{}, false
	}
	defer c.Close()
	ac, ok := any(c).(client.AppCenterAPI)
	if !ok {
		return hostcheck.CatalogSync{}, false
	}
	l, err := ac.Catalog(qctx)
	if err != nil {
		return hostcheck.CatalogSync{}, false
	}
	return hostcheck.CatalogSync{
		At: l.Sync.At, OK: l.Sync.OK, Skipped: l.Sync.Skipped, Code: l.Sync.Code, Message: l.Sync.Message,
		Source: l.Sync.Source, Serial: l.Serial, Origin: l.Origin,
	}, true
}

// doctorLANConsole warns while the LAN console is open without a password:
// the first visitor from the local network sets it, so it should not stay
// that way. noPassword is the address to open ("" = nothing to report).
func doctorLANConsole(r *doctorReport, noPassword string) {
	if noPassword == "" {
		return
	}
	r.warn("LAN-CONSOLE-03", "局域网控制台：开着（%s）但还没设密码，局域网里谁先打开谁设。现在就用局域网里的电脑打开它设一个；不用就运行 acornfox lan-console disable", noPassword)
}

// doctorService checks that acornfox-server answers on its API socket and that
// its runner reaches Docker. Users outside the acornfox-users group cannot open
// the socket; that is reported as a hint, not as the service being down. It
// returns whether the server answered.
func doctorService(r *doctorReport, status func() (client.Status, error)) bool {
	st, err := status()
	switch {
	case err == nil && st.Runner == "ok":
		r.add(true, "SETUP-SVC-01", "后台服务：acornfox-server 运行中（%d 个应用，执行器 %s）", st.Apps, st.Runner)
	case err == nil:
		r.add(false, "SETUP-SVC-01", "后台服务：acornfox-server 在运行，但执行器 acornfox-runner 没有应答（状态 %s）。Docker 正常的话，运行 sudo systemctl restart acornfox-runner acornfox-server，再运行 sudo acornfox doctor；还不行看 journalctl -u acornfox-runner -n 50", st.Runner)
	case socketPermissionDenied(err):
		r.warn("SETUP-SVC-01", "后台服务：没有权限连接 acornfox-server，看不出它是否在运行（用 sudo acornfox doctor 再查）")
		return false
	default:
		r.add(false, "SETUP-SVC-01", "后台服务：acornfox-server 没有应答。先运行 sudo systemctl restart acornfox-runner acornfox-server，再运行 sudo acornfox doctor；还不行看 journalctl -u acornfox-server -n 50")
		return false
	}
	return true
}

// serverPortUse is what acornfox-server says about the ports doctor checks.
// Users says what in AcornFox needs ports 80 and 443: only bound domains
// and the public console listen there. Apps are reached on their own port, so
// a server sharing 80/443 with another web server works without them.
type serverPortUse struct {
	Known bool     // false when the 80/443 users could not be asked
	Users []string // what needs 80/443, e.g. "域名 a.example.com"
	// Own are the ports AcornFox itself listens on while the service is
	// healthy: the gateway admin port, the apps' public ports and the LAN
	// console port when it is open. A user who is not root cannot see who
	// holds a port; these are then counted as AcornFox's own.
	Own map[int]bool
	// LANNoPassword is the LAN console's address when it is open and nobody
	// has set a password yet ("" otherwise): whoever opens it first sets it.
	LANNoPassword string
}

// queryServerPortUse asks acornfox-server for bound domains, the public
// console, the apps' ports and the LAN console. Own is filled only when the
// service is healthy. Any error while asking about 80/443 leaves Known false.
func queryServerPortUse(ctx context.Context, socket string, healthy bool) serverPortUse {
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := client.Connect(qctx, client.Target{URL: unixURL(socket)})
	if err != nil {
		return serverPortUse{}
	}
	defer c.Close()
	out := serverPortUse{}
	apps, appsErr := c.Apps(qctx)
	if healthy && appsErr == nil {
		lanPort := 0
		if lc, err := c.LANConsoleStatus(qctx); err == nil && lc.Enabled {
			lanPort = lc.Port
			if lanPort == 0 {
				lanPort = publicconsole.DefaultLANPort
			}
			if !lc.PasswordSet {
				out.LANNoPassword = lc.URL
				if out.LANNoPassword == "" {
					out.LANNoPassword = fmt.Sprintf("端口 %d", lanPort)
				}
			}
		}
		own := ownServerPorts(apps, lanPort)
		out.Own = own
	}
	if appsErr != nil {
		return out
	}
	pc, err := c.PublicConsoleStatus(qctx)
	if err != nil {
		return out
	}
	var users []string
	if pc.Enabled || pc.Verifying != "" {
		users = append(users, "公网控制台 "+pc.Domain)
	}
	for _, a := range apps {
		doms, err := c.Domains(qctx, a.Name)
		if err != nil {
			return out
		}
		for _, d := range doms {
			users = append(users, "域名 "+d.Name)
		}
	}
	out.Known, out.Users = true, users
	return out
}

// ownServerPorts lists the ports AcornFox listens on itself: the gateway
// admin port, the LAN console port when it is open (lanPort > 0) and the
// public port of each app the gateway serves. The gateway serves an app only
// while it should run and has a live deployment (the reconciler's routes
// skip stopped apps and apps that never went live); a stopped app's port is
// free, so whoever holds it is someone else.
func ownServerPorts(apps []client.App, lanPort int) map[int]bool {
	own := map[int]bool{defaultEdgeAdminPort: true}
	if lanPort > 0 {
		own[lanPort] = true
	}
	for _, a := range apps {
		if a.PublicPort > 0 && appServed(a) {
			own[a.PublicPort] = true
		}
	}
	return own
}

// appServed reports whether the gateway listens on the app's public port.
func appServed(a client.App) bool {
	return a.Desired == state.DesiredRunning && a.CurrentDeployment != ""
}

// socketPermissionDenied reports whether connecting to the API socket was
// refused by file permissions rather than nobody listening.
func socketPermissionDenied(err error) bool {
	var ce *client.Error
	if errors.As(err, &ce) && ce.Diag.Code == "permission_denied" {
		return true
	}
	return errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied")
}

// doctorDisk checks the partitions holding the data directory and Docker's
// data root (hostcheck.CheckDisk, shared with the console's runtime page).
func doctorDisk(r *doctorReport, dataDir, dockerRoot string, free func(string) (uint64, error), device func(string) uint64) {
	hostcheck.CheckDisk(r, dataDir, dockerRoot, free, device)
}

// portCheckContainers lists the running containers and marks AcornFox's own.
// It returns nil (unknown) when Docker is not available.
func portCheckContainers(ctx context.Context, dc *dockerclient.Client) []hostcheck.Container {
	if dc == nil {
		return nil
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := dc.ContainerList(lctx, dockerclient.ContainerListOptions{})
	if err != nil {
		return nil
	}
	out := make([]hostcheck.Container, 0, len(res.Items))
	for _, c := range res.Items {
		hc := hostcheck.Container{
			ID:   c.ID,
			Ours: c.Labels[runner.LabelManaged] == "1" || c.Labels[runner.LabelEdge] == "1",
		}
		if len(c.Names) > 0 {
			hc.Name = strings.TrimPrefix(c.Names[0], "/")
		} else if len(c.ID) >= 12 {
			hc.Name = c.ID[:12]
		}
		for _, p := range c.Ports {
			if p.PublicPort != 0 {
				hc.Ports = append(hc.Ports, int(p.PublicPort))
			}
		}
		out = append(out, hc)
	}
	return out
}

// doctorPorts reports needed ports held by someone other than AcornFox
// (hostcheck.CheckPorts, shared with the console's runtime page).
func doctorPorts(r *doctorReport, procRoot string, containers []hostcheck.Container, root bool, use serverPortUse) {
	hostcheck.CheckPorts(r, procRoot, containers, root, hostcheck.PortUse{Known: use.Known, Users: use.Users, Own: use.Own},
		hostcheck.Ports{Admin: defaultEdgeAdminPort, LAN: publicconsole.DefaultLANPort, AppMin: linuxAppPortMin, AppMax: linuxAppPortMax})
}
