// Package hostcheck holds the Linux server checks "acornfox doctor" runs
// besides Docker: free disk space, the ports AcornFox needs and the clock
// (clock.go).
//
// Disk: the data directory and Docker's data root are checked separately
// (they are often, but not always, the same partition). Below WarnFreeBytes
// doctor warns, below FailFreeBytes it fails. Both report fault SETUP-DISK-01.
//
// Ports: listening TCP sockets are read from /proc/net/tcp and tcp6 of the
// host network namespace and matched to their process through /proc/<pid>/fd.
// A listener counts as AcornFox's own when the process is acornfox itself,
// runs inside an AcornFox container (matched by container ID in
// /proc/<pid>/cgroup), or is the docker-proxy of a port an AcornFox container
// publishes. Without root, other users' processes cannot be matched; those
// listeners are reported as Unknown instead of guessed.
//
// Everything reads under a configurable proc root so tests use a fake tree.
package hostcheck

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Disk thresholds. 10 GB is the minimum free space the runtime setup already
// asks for; below 3 GB an image pull or a build is likely to fail outright.
const (
	WarnFreeBytes uint64 = 10 << 30
	FailFreeBytes uint64 = 3 << 30
)

// DiskLevel grades free space: "ok", "warn" or "fail".
func DiskLevel(free uint64) string {
	switch {
	case free < FailFreeBytes:
		return "fail"
	case free < WarnFreeBytes:
		return "warn"
	}
	return "ok"
}

// FormatBytes prints a size as GB with one decimal (MB under 1 GB).
func FormatBytes(b uint64) string {
	if b < 1<<30 {
		return fmt.Sprintf("%d MB", b>>20)
	}
	return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
}

// ExistingDir returns path or its nearest existing parent (the data directory
// may not exist yet before install).
func ExistingDir(path string) string {
	p := filepath.Clean(path)
	for {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// Listener is one listening TCP socket.
type Listener struct {
	Addr  netip.Addr
	Port  int
	Inode string
}

// ReadListeners returns the listening TCP sockets in <procRoot>/net/tcp and
// tcp6. A missing tcp6 file (IPv6 disabled) is not an error.
func ReadListeners(procRoot string) ([]Listener, error) {
	var out []Listener
	for i, name := range []string{"tcp", "tcp6"} {
		b, err := os.ReadFile(filepath.Join(procRoot, "net", name))
		if err != nil {
			if i == 1 && os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		out = append(out, parseProcNetTCP(b)...)
	}
	return out, nil
}

// parseProcNetTCP parses the /proc/net/tcp{,6} format and keeps sockets in
// LISTEN state (0A).
func parseProcNetTCP(b []byte) []Listener {
	var out []Listener
	sc := bufio.NewScanner(bytes.NewReader(b))
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
		host, port, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		p, err := strconv.ParseUint(port, 16, 16)
		if err != nil {
			continue
		}
		addr, ok := parseHexAddr(host)
		if !ok {
			continue
		}
		out = append(out, Listener{Addr: addr, Port: int(p), Inode: f[9]})
	}
	return out
}

// parseHexAddr decodes the kernel's address: 4 or 16 bytes written as 32-bit
// words in host (little-endian on the platforms we ship) byte order.
func parseHexAddr(s string) (netip.Addr, bool) {
	raw, err := hex.DecodeString(s)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.Addr{}, false
	}
	for i := 0; i < len(raw); i += 4 {
		raw[i], raw[i+1], raw[i+2], raw[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	if len(raw) == 4 {
		return netip.AddrFrom4([4]byte(raw)), true
	}
	return netip.AddrFrom16([16]byte(raw)).Unmap(), true
}

// Proc is a process holding a socket.
type Proc struct {
	PID    int
	Comm   string
	Cgroup string
}

// SocketOwners maps socket inodes to the process holding them. It also
// returns how many processes' file descriptors could not be read (not root).
func SocketOwners(procRoot string) (map[string]Proc, int) {
	owners := map[string]Proc{}
	unreadable := 0
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return owners, 0
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		dir := filepath.Join(procRoot, e.Name())
		fds, err := os.ReadDir(filepath.Join(dir, "fd"))
		if err != nil {
			if os.IsPermission(err) {
				unreadable++
			}
			continue
		}
		var p *Proc
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(dir, "fd", fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := strings.CutPrefix(link, "socket:[")
			if !ok {
				continue
			}
			inode = strings.TrimSuffix(inode, "]")
			if _, seen := owners[inode]; seen {
				continue
			}
			if p == nil {
				comm, _ := os.ReadFile(filepath.Join(dir, "comm"))
				cg, _ := os.ReadFile(filepath.Join(dir, "cgroup"))
				p = &Proc{PID: pid, Comm: strings.TrimSpace(string(comm)), Cgroup: string(cg)}
			}
			owners[inode] = *p
		}
	}
	return owners, unreadable
}

// Container is a running Docker container, as far as the port check needs.
type Container struct {
	ID    string // full 64-character ID
	Name  string
	Ours  bool  // carries an AcornFox label
	Ports []int // host ports it publishes
}

// Owner says who holds a port.
type Owner struct {
	Ours    bool
	Unknown bool   // the process could not be identified (not root)
	Desc    string // "nginx（PID 812）", "容器 web-1（进程 caddy）"
}

// Classify decides who holds listener l. containers nil means the container
// list is not available (Docker did not answer): a containerised owner is
// then reported as Unknown rather than as someone else's.
func Classify(l Listener, owners map[string]Proc, containers []Container) Owner {
	p, ok := owners[l.Inode]
	if !ok {
		return Owner{Unknown: true, Desc: "没有权限查看的进程"}
	}
	if p.Comm == "acornfox" {
		return Owner{Ours: true, Desc: fmt.Sprintf("acornfox（PID %d）", p.PID)}
	}
	for _, c := range containers {
		if c.ID != "" && strings.Contains(p.Cgroup, c.ID) {
			return Owner{Ours: c.Ours, Desc: fmt.Sprintf("容器 %s（进程 %s，PID %d）", c.Name, p.Comm, p.PID)}
		}
	}
	if p.Comm == "docker-proxy" {
		for _, c := range containers {
			for _, port := range c.Ports {
				if port == l.Port {
					return Owner{Ours: c.Ours, Desc: fmt.Sprintf("容器 %s（docker-proxy，PID %d）", c.Name, p.PID)}
				}
			}
		}
		return Owner{Unknown: containers == nil, Desc: fmt.Sprintf("某个 Docker 容器（docker-proxy，PID %d）", p.PID)}
	}
	if strings.Contains(p.Cgroup, "docker") || strings.Contains(p.Cgroup, "containerd") {
		return Owner{Unknown: containers == nil, Desc: fmt.Sprintf("容器里的进程 %s（PID %d）", p.Comm, p.PID)}
	}
	return Owner{Desc: fmt.Sprintf("%s（PID %d）", p.Comm, p.PID)}
}

// Conflict is a needed port held by someone other than AcornFox (or by a
// process doctor could not identify).
type Conflict struct {
	Port    int
	Addr    string
	Owner   string
	Unknown bool
}

// Text is "18820 被 node（PID 9）占用", with the bind address after the
// port when it is not a wildcard.
func (c Conflict) Text() string {
	if c.Addr != "" {
		return fmt.Sprintf("%d（%s）被 %s占用", c.Port, c.Addr, c.Owner)
	}
	return fmt.Sprintf("%d 被 %s占用", c.Port, c.Owner)
}

// Conflicts returns, sorted by port, the listeners on ports wanted reports
// true for that are not AcornFox's own. One entry per port.
func Conflicts(listeners []Listener, owners map[string]Proc, containers []Container, wanted func(port int) bool) []Conflict {
	byPort := map[int]Conflict{}
	for _, l := range listeners {
		if !wanted(l.Port) {
			continue
		}
		o := Classify(l, owners, containers)
		if o.Ours {
			continue
		}
		if prev, seen := byPort[l.Port]; seen && !prev.Unknown {
			continue // keep the identified owner over an unknown one
		}
		addr := ""
		if !l.Addr.IsUnspecified() {
			addr = l.Addr.String()
		}
		byPort[l.Port] = Conflict{Port: l.Port, Addr: addr, Owner: o.Desc, Unknown: o.Unknown}
	}
	out := make([]Conflict, 0, len(byPort))
	for _, c := range byPort {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// JoinConflicts prints at most max conflicts and "等，共 N 个端口" when there
// are more.
func JoinConflicts(cs []Conflict, max int) string {
	var parts []string
	for i, c := range cs {
		if i == max {
			parts = append(parts, fmt.Sprintf("等，共 %d 个端口", len(cs)))
			break
		}
		parts = append(parts, c.Text())
	}
	return strings.Join(parts, "；")
}
