package main

// Linux server checks of `acornfox doctor` besides Docker: whether the
// acornfox-server service answers, free disk space of the data directory and
// Docker's data root, the ports AcornFox needs and the clock (fault codes
// SETUP-SVC-01, SETUP-DISK-01, SETUP-PORT-01..05 and SETUP-CLOCK-01 in
// internal/setup).

import (
	"context"
	"errors"
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

// doctorClockProbe builds the clock check's readers for the download
// gateway's address ("" when the gateway is off); tests replace it.
var doctorClockProbe = hostcheck.DefaultClockProbe

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
	doctorClock(ctx, r, platform.DefaultPaths().DataDir)
}

// doctorClock compares the clock with the download gateway's (the one that
// refuses requests more than 300 seconds off), taking the gateway's address
// the same way the app center does (gatewayBase); no gateway, no network:
// only the time sync status is read.
func doctorClock(ctx context.Context, r *doctorReport, dataDir string) {
	hostcheck.CheckClock(ctx, r, doctorClockProbe(gatewayBase(dataDir)))
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
		}
		out.Own = ownServerPorts(apps, lanPort)
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
