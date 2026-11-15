package edgeadapter

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/edgeadapter/caddyroute"
)

// CheckOptions configures Check. Zero values take the container defaults.
type CheckOptions struct {
	StateDir   string // default edge.StateDir
	RuntimeDir string // default DefaultRuntimeDir
	Proc       string // default "/proc"
	// Want, when set, is the configuration version the caller expects (for
	// example the one it wrote into the settings volume before starting the
	// container).
	Want string
}

// Report is the result of Check (afx-edge health --json).
type Report struct {
	OK       bool     `json:"ok"`
	Version  string   `json:"version,omitempty"`  // in effect, read from this container's proxy
	Stored   string   `json:"stored,omitempty"`   // of the saved configuration
	Routes   int      `json:"routes"`             // in the saved configuration
	Ports    []int    `json:"ports,omitempty"`    // that must be listening
	Problems []string `json:"problems,omitempty"` // empty when OK
}

// Check is the container's health check, run inside the container (Docker's
// health check and the acornfox program's switch both exec it). It answers
// from this container only, which matters while an old and a new entry
// container share the host network and the same ports:
//
//  1. the proxy of this container answers on its private management socket;
//  2. the configuration it serves has the version of the saved one (and of
//     Want when given);
//  3. every port of that configuration is listening, by this container's
//     proxy process (its own sockets in /proc, not "something answers on the
//     port", which another container could do);
//  4. the adapter itself listens on its management endpoint.
func Check(ctx context.Context, o CheckOptions) Report {
	if o.StateDir == "" {
		o.StateDir = edge.StateDir
	}
	if o.RuntimeDir == "" {
		o.RuntimeDir = DefaultRuntimeDir
	}
	if o.Proc == "" {
		o.Proc = "/proc"
	}
	var rep Report
	bad := func(format string, a ...any) { rep.Problems = append(rep.Problems, fmt.Sprintf(format, a...)) }

	st, err := ReadRunState(o.RuntimeDir)
	if err != nil {
		bad("适配程序还没有就绪（读不到 %s：%v）", filepath.Join(o.RuntimeDir, runFile), err)
		return rep
	}
	cfg, stored, err := NewStore(o.StateDir).Load()
	if err != nil {
		bad("%v", err)
		return rep
	}
	if stored {
		rep.Stored = cfg.Version()
		rep.Routes = len(cfg.Routes)
	}

	sock := filepath.Join(o.RuntimeDir, proxySocket)
	driver := caddyroute.New("unix:"+sock, cfg.HTTPS)
	if h := driver.Health(ctx); !h.OK() {
		bad("代理程序没有应答：%s", h.Reason)
		return rep
	}
	v, err := driver.Version(ctx)
	if err != nil {
		bad("读不到代理程序现在的设置：%v", err)
		return rep
	}
	rep.Version = v
	if stored && v != rep.Stored {
		bad("代理程序现在的设置（%s）和保存的（%s）不一样", v, rep.Stored)
	}
	if o.Want != "" && v != o.Want {
		bad("代理程序现在的设置（%s）不是要的（%s）", v, o.Want)
	}

	rep.Ports = ExpectedPorts(cfg)
	if len(rep.Ports) > 0 {
		have, err := ListeningPorts(o.Proc, st.ProxyPID)
		if err != nil {
			bad("看不到代理程序在听哪些端口：%v", err)
		} else if miss := missing(rep.Ports, have); len(miss) > 0 {
			bad("这些端口没有在听：%s", joinInts(miss))
		}
	}
	if _, p, err := net.SplitHostPort(st.AdminListen); err == nil {
		if port, _ := strconv.Atoi(p); port > 0 {
			have, err := ListeningPorts(o.Proc, st.AdapterPID)
			if err != nil || !have[port] {
				bad("适配程序没有在听管理入口 %s", st.AdminListen)
			}
		}
	}
	rep.OK = len(rep.Problems) == 0
	return rep
}

// ExpectedPorts are the ports the proxy listens on for cfg: each route's own
// port (and LAN port), and the HTTP and HTTPS ports once any route has a
// domain (the HTTPS listener and the HTTP redirect).
func ExpectedPorts(cfg edge.Config) []int {
	set := map[int]bool{}
	domains := false
	for _, r := range cfg.Routes {
		if r.PublicPort > 0 {
			set[r.PublicPort] = true
		}
		if r.LANPort > 0 {
			set[r.LANPort] = true
		}
		if len(r.Domains) > 0 {
			domains = true
		}
	}
	if domains {
		h := cfg.HTTPS.WithDefaults()
		set[h.HTTPPort] = true
		set[h.HTTPSPort] = true
	}
	out := make([]int, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// ListeningPorts returns the TCP ports process pid listens on: the sockets
// among its open files that /proc/<pid>/net/tcp{,6} lists in state LISTEN.
func ListeningPorts(proc string, pid int) (map[int]bool, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("进程号 %d 不对", pid)
	}
	base := filepath.Join(proc, strconv.Itoa(pid))
	fds, err := os.ReadDir(filepath.Join(base, "fd"))
	if err != nil {
		return nil, err
	}
	inodes := map[string]bool{}
	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(base, "fd", fd.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(link, "socket:[") && strings.HasSuffix(link, "]") {
			inodes[link[len("socket:["):len(link)-1]] = true
		}
	}
	out := map[int]bool{}
	for _, f := range []string{"tcp", "tcp6"} {
		file, err := os.Open(filepath.Join(base, "net", f))
		if err != nil {
			continue
		}
		for port, inode := range parseListen(file) {
			for _, in := range inode {
				if inodes[in] {
					out[port] = true
				}
			}
		}
		file.Close()
	}
	return out, nil
}

// parseListen reads a /proc/net/tcp table and returns, per local port, the
// inodes of the sockets in state LISTEN (0A).
func parseListen(r interface{ Read([]byte) (int, error) }) map[int][]string {
	out := map[int][]string{}
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[3] != "0A" {
			continue
		}
		i := strings.LastIndex(f[1], ":")
		if i < 0 {
			continue
		}
		p, err := strconv.ParseInt(f[1][i+1:], 16, 32)
		if err != nil {
			continue
		}
		out[int(p)] = append(out[int(p)], f[9])
	}
	return out
}

func missing(want []int, have map[int]bool) []int {
	var out []int
	for _, p := range want {
		if !have[p] {
			out = append(out, p)
		}
	}
	return out
}

func joinInts(ps []int) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, "、")
}
