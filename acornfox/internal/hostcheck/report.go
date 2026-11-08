package hostcheck

// The disk, port and clock checks shared by `acornfox doctor` (run on the
// server by a user) and the console's runtime page (GET /v1/runtime/checks,
// run inside acornfox-server). Both write into a Reporter, so the wording and
// the fault codes (SETUP-DISK-01, SETUP-PORT-01..05, SETUP-CLOCK-01 in
// internal/setup) stay the same. The clock check is in clock.go.

import (
	"fmt"
	"strings"
)

// Reporter receives check results. Add with ok=false is a failure carrying a
// fault code; Warn is a warning that may carry one.
type Reporter interface {
	Add(ok bool, code, format string, a ...any)
	Warn(code, format string, a ...any)
}

// DefaultDockerRoot is Docker's data root when Docker does not say.
const DefaultDockerRoot = "/var/lib/docker"

// CheckDisk checks the partitions holding the data directory and Docker's
// data root; directories on the same partition are reported together. An
// empty dockerRoot means Docker did not say where its data root is: the
// default location is checked and the line says so, since Docker may keep
// its data elsewhere.
func CheckDisk(r Reporter, dataDir, dockerRoot string, free func(string) (uint64, error), device func(string) uint64) {
	dockerLabel := "Docker 数据目录"
	if dockerRoot == "" {
		dockerRoot = DefaultDockerRoot
		dockerLabel = "Docker 数据目录（Docker 没告知，按默认位置）"
	}
	type group struct {
		names []string
		path  string
	}
	var groups []*group
	byDev := map[uint64]*group{}
	for _, t := range []struct{ label, path string }{{"数据目录", dataDir}, {dockerLabel, dockerRoot}} {
		p := ExistingDir(t.path)
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
			r.Warn("", "磁盘：查不到 %s 所在分区的剩余空间（%v）", where, err)
			continue
		}
		switch DiskLevel(n) {
		case "fail":
			r.Add(false, "SETUP-DISK-01", "磁盘：%s 所在分区只剩 %s，不到 %s，拉镜像和构建会失败", where, FormatBytes(n), FormatBytes(FailFreeBytes))
		case "warn":
			r.Warn("SETUP-DISK-01", "磁盘：%s 所在分区只剩 %s，不到 %s，建议清理或扩容", where, FormatBytes(n), FormatBytes(WarnFreeBytes))
		default:
			r.Add(true, "SETUP-DISK-01", "磁盘：%s 所在分区剩余 %s", where, FormatBytes(n))
		}
	}
}

// PortUse is what is known about the ports AcornFox itself needs. Users says
// what in AcornFox needs ports 80 and 443 (bound domains, the public
// console); Known is false when that could not be found out. Own are the
// ports AcornFox listens on itself (the gateway admin port, the apps' public
// ports, the LAN console port when open): without root a listener whose
// holder is unknown counts as AcornFox's own when its port is in Own.
type PortUse struct {
	Known bool
	Users []string
	Own   map[int]bool
}

// Ports are the ports checked: the gateway admin port, the LAN console port
// and the range the apps' ports are taken from.
type Ports struct {
	Admin, LAN, AppMin, AppMax int
}

// CheckPorts reports needed ports held by someone other than AcornFox.
// Ports 80/443 only fail when a bound domain or the public console needs
// them; otherwise sharing them with another web server is a supported setup.
// containers nil means Docker could not be asked; root says whether every
// process's sockets can be matched.
func CheckPorts(r Reporter, procRoot string, containers []Container, root bool, use PortUse, p Ports) {
	listeners, err := ReadListeners(procRoot)
	if err != nil {
		r.Warn("", "端口：读不到正在监听的端口（%v）", err)
		return
	}
	owners, _ := SocketOwners(procRoot)
	all := Conflicts(listeners, owners, containers, func(port int) bool {
		return port == 80 || port == 443 || port == p.Admin || port == p.LAN || (port >= p.AppMin && port <= p.AppMax)
	})
	var webs, admin, apps, lan, unknown, own []Conflict
	for _, c := range all {
		switch {
		case c.Unknown && !root && use.Own[c.Port]:
			own = append(own, c)
		case c.Unknown:
			unknown = append(unknown, c)
		case c.Port == 80 || c.Port == 443:
			webs = append(webs, c)
		case c.Port == p.Admin:
			admin = append(admin, c)
		case c.Port == p.LAN:
			lan = append(lan, c)
		default:
			apps = append(apps, c)
		}
	}
	if len(webs) > 0 {
		held := JoinConflicts(webs, 2)
		switch {
		case len(use.Users) > 0:
			r.Add(false, "SETUP-PORT-01", "端口：%s；这台服务器上的%s 要用 80/443，现在都打不开", held, strings.Join(use.Users, "、"))
		case use.Known:
			r.Warn("SETUP-PORT-01", "端口：%s。没有绑定域名、也没开公网控制台，不影响使用：应用照常用 http://服务器IP:端口 访问，只是绑定域名和 HTTPS 用不了（和宝塔、1Panel、nginx 共存时不用处理）", held)
		default:
			r.Warn("SETUP-PORT-01", "端口：%s。连不上 acornfox-server，看不出有没有绑定域名；没绑定的话不影响使用，应用照常用 http://服务器IP:端口 访问，只是绑定域名和 HTTPS 用不了", held)
		}
	}
	if len(admin) > 0 {
		r.Add(false, "SETUP-PORT-02", "端口：网关管理口 %s，应用网址和域名都配置不上", JoinConflicts(admin, 1))
	}
	if len(apps) > 0 {
		r.Add(false, "SETUP-PORT-03", "端口：应用端口段 %d–%d 里 %s，分到这些端口的应用会上线失败", p.AppMin, p.AppMax, JoinConflicts(apps, 5))
	}
	if len(lan) > 0 {
		r.Warn("SETUP-PORT-04", "端口：局域网控制台端口 %s，开不了局域网控制台", JoinConflicts(lan, 1))
	}
	if len(unknown) > 0 {
		ports := make([]string, 0, len(unknown))
		for _, c := range unknown {
			ports = append(ports, fmt.Sprint(c.Port))
		}
		switch {
		case !root:
			r.Warn("SETUP-PORT-05", "端口：%s 有程序在监听，不是 root，看不到是谁占用；用 sudo acornfox doctor 可确认", strings.Join(ports, "、"))
		case containers == nil:
			r.Warn("SETUP-PORT-05", "端口：%s 有程序在监听，分不清是不是 AcornFox 自己的（可能是容器里的进程，Docker 没有应答所以分不清）", strings.Join(ports, "、"))
		default:
			r.Warn("SETUP-PORT-05", "端口：%s 有程序在监听，分不清是不是 AcornFox 自己的（进程可能刚退出，过一会儿再查）", strings.Join(ports, "、"))
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
		r.Add(true, "SETUP-PORT-01", "端口：80、443、%d、%d、%d–%d 没有被别的程序占用%s", p.Admin, p.LAN, p.AppMin, p.AppMax, note)
	}
}
