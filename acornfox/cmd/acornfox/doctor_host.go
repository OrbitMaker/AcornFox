package main

// Linux server checks of `acornfox doctor` besides Docker: whether the
// acornfox-server service answers, free disk space of the data directory and
// Docker's data root, the ports AcornFox needs, the clock, the download
// gateway, the app catalog sync, the entry proxy (doctor_edge.go) and the
// services the installed apps are plugged into (fault codes SETUP-SVC-01, EDGE-01..04, SETUP-DISK-01, SETUP-PORT-01..05,
// SETUP-CLOCK-01, SETUP-GW-01, SETUP-SYNC-01, CAT-SVC-08 and BKP-01, BKP-08, BKP-10, BKP-14; the full
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
	"github.com/acornfox/acornfox/internal/platformupdate"
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

// doctorServices asks acornfox-server for the service registry and the
// installed apps; ok is false when it cannot be asked or is too old to have
// services (tests replace it).
var doctorServices = queryServices

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
	doctorEdge(ctx, r, dc, platform.DefaultPaths().DataDir)
	doctorDisk(r, platform.DefaultPaths().DataDir, dockerRoot, hostcheck.FreeBytes, hostcheck.Device)
	doctorPorts(r, "/proc", portCheckContainers(ctx, dc), os.Geteuid() == 0, use)
	doctorClockAndGateway(ctx, r, platform.DefaultPaths().DataDir)
	probed := doctorRegistry(ctx, r, platform.DefaultPaths().DataDir)
	doctorDaemonMirrors(ctx, r, daemonConfigFile, probed)
	if svc.err == nil {
		doctorCatalog(ctx, r, socket)
		doctorAppServices(ctx, r, socket)
		doctorAppsBehind(ctx, r, socket)
		doctorAppsOutdated(ctx, r, socket)
		doctorPlatformDomain(ctx, r, socket)
		doctorPlatformUpdate(r, svc.st.PlatformUpdate)
		doctorBackups(ctx, r, socket)
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
	hostcheck.CheckCatalogShow(r, s)
	hostcheck.CheckCatalogSkills(r, s)
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
	cs := hostcheck.CatalogSync{
		At: l.Sync.At, OK: l.Sync.OK, Skipped: l.Sync.Skipped, Code: l.Sync.Code, Message: l.Sync.Message,
		Source: l.Sync.Source, Serial: l.Serial, Origin: l.Origin,
	}
	if sh := l.Sync.Show; sh != nil {
		cs.Show = hostcheck.ShowInUse{State: sh.State, Code: sh.Code, Message: sh.Message, Serial: sh.Serial}
	}
	if sk := l.Sync.Skills; sk != nil {
		cs.Skills = hostcheck.SkillsInUse{State: sk.State, Count: sk.Count, Code: sk.Code, Message: sk.Message}
	}
	return cs, true
}

// doctorAppServices reports the services the installed apps are plugged
// into (hostcheck.CheckAppServices). Nothing is added when the server cannot
// be asked or is older than services.
func doctorAppServices(ctx context.Context, r *doctorReport, socket string) {
	q, ok := doctorServices(ctx, socket)
	if !ok {
		return
	}
	if q.err != nil {
		r.warn(hostcheck.CodeServiceUnusable, "应用用的服务：读不了服务登记表（%v）", q.err)
		return
	}
	hostcheck.CheckAppServices(r, q.services, q.unreadable, q.installed)
}

// doctorAppsBehind warns about each app from the app center whose settings
// are behind (the platform's values or the services it is plugged into
// changed since it was applied: APP-VARS-01). Read-only; nothing is added
// when the server cannot be asked or is older than reapply.
func doctorAppsBehind(ctx context.Context, r *doctorReport, socket string) {
	qctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	c, err := client.Connect(qctx, client.Target{URL: unixURL(socket)})
	if err != nil {
		return
	}
	defer c.Close()
	ap, ok := any(c).(client.AppApplyAPI)
	if !ok {
		return
	}
	v, err := ap.PlatformAffected(qctx, "")
	if err != nil {
		return
	}
	for _, a := range v.Apps {
		if a.Blocked != "" || !a.Differs {
			continue
		}
		var names []string
		for _, x := range a.Vars {
			names = append(names, x.Name)
		}
		restart := ""
		if a.Restart {
			restart = "，会重启它"
		}
		r.warn("APP-VARS-01", "应用 %s 的设置落在后面了（%s 和现在的不一样）：acornfox apps reapply %s --plan 看会改什么%s，用户同意后加 --yes", a.App, strings.Join(names, "、"), a.App, restart)
	}
}

// doctorAppsOutdated says which apps from the app center have a newer version
// (not a fault: a line to look at; the user decides when to upgrade) and
// which have a snapshot from before an upgrade kept (APP-UPG-*). Read-only;
// nothing is added when the server cannot be asked or is older than upgrades.
func doctorAppsOutdated(ctx context.Context, r *doctorReport, socket string) {
	qctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	c, err := client.Connect(qctx, client.Target{URL: unixURL(socket)})
	if err != nil {
		return
	}
	defer c.Close()
	apps, err := c.Apps(qctx)
	if err != nil {
		return
	}
	for _, a := range apps {
		u := a.Upgrade
		if u == nil {
			continue
		}
		switch {
		case u.Available != "" && u.CanUpgrade:
			r.add(true, "APP-UPG-01", "应用 %s 有新版本 %s（现在 %s）：acornfox apps upgrade %s --plan 看会怎么升，升级要用户同意", a.Name, u.Available, a.CatalogVersion, a.Name)
		case u.Available != "":
			r.add(true, "APP-UPG-01", "应用 %s 有新版本 %s（现在 %s），但现在不能升级：%s %s", a.Name, u.Available, a.CatalogVersion, u.BlockedCode, u.Blocked)
		}
		if u.CanRollback && u.Snapshot != nil {
			r.add(true, "APP-UPG-07", "应用 %s 留着升级前（%s）的快照 %s（约 %d MB），出问题可以 acornfox apps rollback %s --plan 退回", a.Name, u.Snapshot.Version, u.Snapshot.ID, u.Snapshot.SizeMB, a.Name)
		}
	}
}

// doctorPlatformUpdate says when a newer AcornFox is out (a hint, not a
// fault: the user decides when to upgrade) and when the latest version could
// not be read for days (PLAT-UPD-02). pu is what GET /v1/status carried;
// nothing is added when the server has no such check or it is switched off.
func doctorPlatformUpdate(r *doctorReport, pu *client.PlatformUpdate) {
	if pu == nil || !pu.Enabled {
		return
	}
	if pu.Available {
		r.warn(platformupdate.CodeAvailable, "AcornFox 有新版本 %s（现在 %s）：acornfox update 看发行说明和升级命令，升级要用户同意", pu.Latest, pu.Current)
	}
	if pu.Unreachable {
		r.warn(platformupdate.CodeUnreachable, "好几天查不到 AcornFox 的最新版本（%s）：acornfox update check 现在查一次，看 acornfox faults %s", pu.Error, platformupdate.CodeUnreachable)
	}
}

// servicesQuery is the service registry as acornfox-server answered it.
type servicesQuery struct {
	services   []hostcheck.ServiceInUse
	unreadable []string
	installed  func(app string) bool // nil: the apps could not be listed
	err        error                 // the server answered with an error
}

// queryServices reads GET /v1/services and GET /v1/apps of acornfox-server.
// ok is false when the server cannot be reached or is older than services
// (server_too_old); err is set when it answered with an error.
func queryServices(ctx context.Context, socket string) (servicesQuery, bool) {
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := client.Connect(qctx, client.Target{URL: unixURL(socket)})
	if err != nil {
		return servicesQuery{}, false
	}
	defer c.Close()
	sa, ok := any(c).(client.ServicesAPI)
	if !ok {
		return servicesQuery{}, false
	}
	l, err := sa.Services(qctx)
	if err != nil {
		// Only an answer of the server is reported; not reaching it (the
		// service line says so already) or a server older than services
		// adds nothing.
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Diag.Stage != "server" || ce.Diag.Code == client.CodeServerTooOld {
			return servicesQuery{}, false
		}
		return servicesQuery{err: err}, true
	}
	q := servicesQuery{unreadable: l.Unreadable}
	if apps, err := c.Apps(qctx); err == nil {
		names := make(map[string]bool, len(apps))
		for _, a := range apps {
			names[a.Name] = true
		}
		q.installed = func(app string) bool { return names[app] }
	}
	for _, sv := range l.Services {
		in := hostcheck.ServiceInUse{Name: sv.Name, Kind: sv.Kind, KindLabel: sv.KindLabel, Status: sv.Status, StatusLabel: sv.StatusLabel, Missing: sv.Missing}
		if sv.Source == state.ServiceFromApp {
			in.Provider = sv.App
		}
		in.ReadySay = sv.ReadySay
		for _, u := range sv.UsedBy {
			in.Users = append(in.Users, hostcheck.ServiceUser{App: u.App, Use: u.Use})
		}
		q.services = append(q.services, in)
	}
	return q, true
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

// doctorRegistry probes the Docker Hub sources this server pulls from (the
// table in internal/runner/sources.go, in the runner's order) the way a pull
// does: the manifest of the probe image (runner.ProbeImage) and a sample of its
// largest layer, at most about 10 seconds. It says which source and layer
// is used first and how fast; sources that refuse, do not answer or are slow
// are a SETUP-NET-03 notice (the pull skips them or puts them last). None
// usable is SETUP-NET-01.
func doctorRegistry(ctx context.Context, r *doctorReport, dataDir string) map[string]runner.ProbeResult {
	reg, _ := sourcesRegistry(dataDir)
	srcs := reg.Sources()
	if !reg.China {
		return doctorRegistryAbroad(ctx, r, srcs)
	}
	res := sourcesProbe(ctx, srcs, runner.ProbeImage, "")
	order, skipped := runner.RankSources(srcs, res)
	var bad []string
	usable := false
	for _, s := range srcs {
		p := res[s.Host]
		switch p.Status {
		case runner.ProbeOK:
			usable = true
		case runner.ProbeSlow:
			usable = true
			bad = append(bad, s.Host+" "+p.Detail+"（排到后面）")
		default:
			bad = append(bad, s.Host+" "+p.Detail+"（跳过）")
		}
	}
	if !usable {
		r.add(false, "SETUP-NET-01", "镜像下载源：都不能用：%s；拉镜像时还会按顺序都试一遍。先查网络和代理，acornfox sources --probe 再看", strings.Join(bad, "，"))
		return res
	}
	first := order[0]
	p := res[first.Host]
	speed := fmt.Sprintf("，%.1f MB/s", p.MBps)
	if p.Status == runner.ProbeSlow {
		speed = fmt.Sprintf("，但也慢：%.2f MB/s，大镜像要等很久", p.MBps)
	}
	r.add(true, "SETUP-NET-01", "镜像下载源：现在先用 %s（%s%s）", first.Host, first.Layer.Label(), speed)
	if len(bad) > 0 || len(skipped) > 0 {
		r.warn("SETUP-NET-03", "镜像下载源：%s；拉镜像时会自动跳过或排到后面（acornfox sources --probe 看详情）", strings.Join(bad, "，"))
	}
	return res
}

// doctorRegistryAbroad is the registry check outside mainland China. There
// the runner does not probe: it pulls from Docker Hub first and only moves
// to the mirrors after it, in order, when Docker Hub stalls, is too slow or
// fails. So only Docker Hub is probed; the mirrors only when Docker Hub is
// not usable (and a mirror that refuses is then no "skip", only a fact).
func doctorRegistryAbroad(ctx context.Context, r *doctorReport, srcs []runner.ImageSource) map[string]runner.ProbeResult {
	if len(srcs) == 0 {
		return nil
	}
	first := srcs[0]
	res := sourcesProbe(ctx, srcs[:1], runner.ProbeImage, "")
	p := res[first.Host]
	switch p.Status {
	case runner.ProbeOK:
		r.add(true, "SETUP-NET-01", "镜像下载源：境外，先用 %s（%s，%.1f MB/s）", first.Host, first.Layer.Label(), p.MBps)
		return res
	case runner.ProbeSlow:
		r.add(true, "SETUP-NET-01", "镜像下载源：境外，先用 %s（%s，但也慢：%.2f MB/s，大镜像要等很久）", first.Host, first.Layer.Label(), p.MBps)
		return res
	}
	rest := srcs[1:]
	if len(rest) > 0 {
		for h, v := range sourcesProbe(ctx, rest, runner.ProbeImage, "") {
			res[h] = v
		}
	}
	for _, s := range rest {
		if q := res[s.Host]; q.Usable() {
			r.add(true, "SETUP-NET-01", "镜像下载源：境外，先用的 %s %s；拉镜像时它没有进展或太慢会换下一个源，现在能用的是 %s（%.1f MB/s）",
				first.Host, p.Detail, s.Host, q.MBps)
			r.warn("SETUP-NET-03", "镜像下载源：%s %s，每次拉镜像要先等它一会儿才换源（acornfox sources --probe 看详情）", first.Host, p.Detail)
			return res
		}
	}
	var bad []string
	for _, s := range srcs {
		bad = append(bad, s.Host+" "+res[s.Host].Detail)
	}
	r.add(false, "SETUP-NET-01", "镜像下载源：都不能用：%s；拉镜像时还会按顺序都试一遍。先查网络和代理，acornfox sources --probe 再看", strings.Join(bad, "，"))
	return res
}

// doctorPing asks a registry's /v2/ (tests replace it).
var doctorPing = runner.PingRegistry

// doctorDaemonMirrors checks the registry-mirrors of the Docker daemon's
// daemon.json: Docker uses them when it pulls by the original name (a
// project's build, Docker Hub as the last source). An address that refuses
// or does not answer is a SETUP-NET-03 notice; nothing is changed. A host the
// registry check just probed is judged by that probe (one answer per host).
func doctorDaemonMirrors(ctx context.Context, r *doctorReport, path string, probed map[string]runner.ProbeResult) {
	hosts := runner.DaemonMirrorHosts(path)
	if len(hosts) == 0 {
		return
	}
	var bad []string
	for _, h := range hosts {
		p, ok := probed[h]
		if !ok {
			p = doctorPing(ctx, h)
		}
		if p.Status != runner.ProbeOK && p.Status != runner.ProbeSlow {
			bad = append(bad, h+" "+p.Detail)
		}
	}
	if len(bad) == 0 {
		r.add(true, "SETUP-NET-03", "Docker 的镜像加速配置（%s）：%d 个地址都能连上", path, len(hosts))
		return
	}
	r.warn("SETUP-NET-03", "Docker 的镜像加速配置（%s）里有不能用的地址：%s。自己 docker pull 和构建项目时 Docker 会先等它；可以从 registry-mirrors 里去掉再重启 Docker（改 Docker 配置，先问用户）", path, strings.Join(bad, "，"))
}
