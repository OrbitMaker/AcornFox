package main

// Linux server checks of `acornfox doctor` besides Docker: whether the
// acornfox-server service answers, free disk space of the data directory and
// Docker's data root, and the ports AcornFox needs (fault codes
// SETUP-SVC-01, SETUP-DISK-01 and SETUP-PORT-01..05 in internal/setup).

import (
	"context"
	"errors"
	"fmt"
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

// defaultDockerRoot is used when Docker does not answer.
const defaultDockerRoot = "/var/lib/docker"

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
	r.add(true, "", "Docker：由后台服务访问 Docker%s，当前用户不需要 Docker 权限", ver)
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
}

// doctorService checks that acornfox-server answers on its API socket and that
// its runner reaches Docker. Users outside the acornfox-users group cannot open
// the socket; that is reported as a hint, not as the service being down. It
// returns whether the server answered.
func doctorService(r *doctorReport, status func() (client.Status, error)) bool {
	st, err := status()
	switch {
	case err == nil && st.Runner == "ok":
		r.add(true, "", "后台服务：acornfox-server 运行中（%d 个应用，执行器 %s）", st.Apps, st.Runner)
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
// data root; directories on the same partition are reported together.
func doctorDisk(r *doctorReport, dataDir, dockerRoot string, free func(string) (uint64, error), device func(string) uint64) {
	if dockerRoot == "" {
		dockerRoot = defaultDockerRoot
	}
	type group struct {
		names []string
		path  string
	}
	var groups []*group
	byDev := map[uint64]*group{}
	for _, t := range []struct{ label, path string }{{"数据目录", dataDir}, {"Docker 数据目录", dockerRoot}} {
		p := hostcheck.ExistingDir(t.path)
		name := t.label + " " + t.path
		if dev := device(p); dev != 0 {
			if g, ok := byDev[dev]; ok {
				g.names = append(g.names, name)
				continue
			}
			g := &group{names: []string{name}, path: p}
			byDev[dev] = g
			groups = append(groups, g)
			continue
		}
		groups = append(groups, &group{names: []string{name}, path: p})
	}
	for _, g := range groups {
		where := strings.Join(g.names, "、")
		n, err := free(g.path)
		if err != nil {
			r.warn("", "磁盘：查不到 %s 所在分区的剩余空间（%v）", where, err)
			continue
		}
		switch hostcheck.DiskLevel(n) {
		case "fail":
			r.add(false, "SETUP-DISK-01", "磁盘：%s 所在分区只剩 %s，不到 %s，拉镜像和构建会失败", where, hostcheck.FormatBytes(n), hostcheck.FormatBytes(hostcheck.FailFreeBytes))
		case "warn":
			r.warn("SETUP-DISK-01", "磁盘：%s 所在分区只剩 %s，不到 %s，建议清理或扩容", where, hostcheck.FormatBytes(n), hostcheck.FormatBytes(hostcheck.WarnFreeBytes))
		default:
			r.add(true, "", "磁盘：%s 所在分区剩余 %s", where, hostcheck.FormatBytes(n))
		}
	}
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

// doctorPorts reports needed ports held by someone other than AcornFox.
// Ports 80/443 only fail when a bound domain or the public console needs them;
// otherwise sharing them with another web server is a supported setup.
//
// A user who is not root cannot see other users' processes. Then a port whose
// holder is unknown counts as AcornFox's own when it is in use.Own (the
// service is healthy and that port is one AcornFox listens on); other unknown
// ports are a warning that says to rerun with sudo. As root nothing changes.
func doctorPorts(r *doctorReport, procRoot string, containers []hostcheck.Container, root bool, use serverPortUse) {
	listeners, err := hostcheck.ReadListeners(procRoot)
	if err != nil {
		r.warn("", "端口：读不到正在监听的端口（%v）", err)
		return
	}
	owners, _ := hostcheck.SocketOwners(procRoot)
	adminPort, lanPort := defaultEdgeAdminPort, publicconsole.DefaultLANPort
	all := hostcheck.Conflicts(listeners, owners, containers, func(p int) bool {
		return p == 80 || p == 443 || p == adminPort || p == lanPort || (p >= linuxAppPortMin && p <= linuxAppPortMax)
	})
	var webs, admin, apps, lan, unknown, own []hostcheck.Conflict
	for _, c := range all {
		switch {
		case c.Unknown && !root && use.Own[c.Port]:
			own = append(own, c)
		case c.Unknown:
			unknown = append(unknown, c)
		case c.Port == 80 || c.Port == 443:
			webs = append(webs, c)
		case c.Port == adminPort:
			admin = append(admin, c)
		case c.Port == lanPort:
			lan = append(lan, c)
		default:
			apps = append(apps, c)
		}
	}
	if len(webs) > 0 {
		held := hostcheck.JoinConflicts(webs, 2)
		switch {
		case len(use.Users) > 0:
			r.add(false, "SETUP-PORT-01", "端口：%s；这台服务器上的%s 要用 80/443，现在都打不开", held, strings.Join(use.Users, "、"))
		case use.Known:
			r.warn("SETUP-PORT-01", "端口：%s。没有绑定域名、也没开公网控制台，不影响使用：应用照常用 http://服务器IP:端口 访问，只是绑定域名和 HTTPS 用不了（和宝塔、1Panel、nginx 共存时不用处理）", held)
		default:
			r.warn("SETUP-PORT-01", "端口：%s。连不上 acornfox-server，看不出有没有绑定域名；没绑定的话不影响使用，应用照常用 http://服务器IP:端口 访问，只是绑定域名和 HTTPS 用不了", held)
		}
	}
	if len(admin) > 0 {
		r.add(false, "SETUP-PORT-02", "端口：网关管理口 %s，应用网址和域名都配置不上", hostcheck.JoinConflicts(admin, 1))
	}
	if len(apps) > 0 {
		r.add(false, "SETUP-PORT-03", "端口：应用端口段 %d–%d 里 %s，分到这些端口的应用会上线失败", linuxAppPortMin, linuxAppPortMax, hostcheck.JoinConflicts(apps, 5))
	}
	if len(lan) > 0 {
		r.warn("SETUP-PORT-04", "端口：局域网控制台端口 %s，开不了局域网控制台", hostcheck.JoinConflicts(lan, 1))
	}
	if len(unknown) > 0 {
		ports := make([]string, 0, len(unknown))
		for _, c := range unknown {
			ports = append(ports, fmt.Sprint(c.Port))
		}
		switch {
		case !root:
			r.warn("SETUP-PORT-05", "端口：%s 有程序在监听，不是 root，看不到是谁占用；用 sudo acornfox doctor 可确认", strings.Join(ports, "、"))
		case containers == nil:
			r.warn("SETUP-PORT-05", "端口：%s 有程序在监听，分不清是不是 AcornFox 自己的（可能是容器里的进程，Docker 没有应答所以分不清）", strings.Join(ports, "、"))
		default:
			r.warn("SETUP-PORT-05", "端口：%s 有程序在监听，分不清是不是 AcornFox 自己的（进程可能刚退出，过一会儿再查）", strings.Join(ports, "、"))
		}
	}
	if len(all) == len(own) {
		note := ""
		if len(own) > 0 {
			ports := make([]string, 0, len(own))
			for _, c := range own {
				ports = append(ports, fmt.Sprint(c.Port))
			}
			note = "（" + strings.Join(ports, "、") + " 是 AcornFox 自己在用）"
		}
		r.add(true, "", "端口：80、443、%d、%d、%d–%d 没有被别的程序占用%s", adminPort, lanPort, linuxAppPortMin, linuxAppPortMax, note)
	}
}
