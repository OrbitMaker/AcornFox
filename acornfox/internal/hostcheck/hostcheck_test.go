package hostcheck

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const tcpHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

func tcpLine(local, st, inode string) string {
	return "   0: " + local + " 00000000:0000 " + st + " 00000000:00000000 00:00000000 00000000     0        0 " + inode + " 1 0000000000000000 100 0 0 10 0\n"
}

// fakeProc builds <root>/net/tcp{,6} and /proc/<pid>/{fd,comm,cgroup}.
type fakeProc struct {
	t    *testing.T
	root string
}

func newFakeProc(t *testing.T, tcp, tcp6 string) fakeProc {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(tcpHeader+tcp), 0o644); err != nil {
		t.Fatal(err)
	}
	if tcp6 != "" {
		if err := os.WriteFile(filepath.Join(root, "net", "tcp6"), []byte(tcpHeader+tcp6), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return fakeProc{t: t, root: root}
}

func (f fakeProc) proc(pid, comm, cgroup string, inodes ...string) {
	f.t.Helper()
	dir := filepath.Join(f.root, pid)
	if err := os.MkdirAll(filepath.Join(dir, "fd"), 0o755); err != nil {
		f.t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "cgroup"), []byte(cgroup), 0o644)
	_ = os.Symlink("/dev/null", filepath.Join(dir, "fd", "0"))
	for i, ino := range inodes {
		if err := os.Symlink("socket:["+ino+"]", filepath.Join(dir, "fd", strconv.Itoa(3+i))); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestReadListeners(t *testing.T) {
	f := newFakeProc(t,
		tcpLine("00000000:0050", "0A", "100")+ // 0.0.0.0:80
			tcpLine("0100007F:4978", "0A", "101")+ // 127.0.0.1:18808
			tcpLine("0100007F:4978", "01", "102"), // established: skipped
		tcpLine("00000000000000000000000000000000:01BB", "0A", "103")+ // [::]:443
			tcpLine("00000000000000000000000001000000:4972", "0A", "104")) // [::1]:18802
	ls, err := ReadListeners(f.root)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, l := range ls {
		got = append(got, l.Addr.String()+"|"+itoa(l.Port)+"|"+l.Inode)
	}
	want := "0.0.0.0|80|100 127.0.0.1|18808|101 ::|443|103 ::1|18802|104"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v\nwant %s", got, want)
	}
}

func TestReadListenersNoTCP6(t *testing.T) {
	f := newFakeProc(t, tcpLine("00000000:0050", "0A", "1"), "")
	if ls, err := ReadListeners(f.root); err != nil || len(ls) != 1 {
		t.Fatalf("%v %v", ls, err)
	}
	if _, err := ReadListeners(t.TempDir()); err == nil {
		t.Fatal("missing /proc/net/tcp must be an error")
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

const edgeID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const otherID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestConflictsClassify(t *testing.T) {
	f := newFakeProc(t,
		tcpLine("00000000:0050", "0A", "10")+ // 80 nginx
			tcpLine("00000000:01BB", "0A", "11")+ // 443 our caddy (host network, in edge container cgroup)
			tcpLine("0100007F:4978", "0A", "12")+ // 18808 our caddy
			tcpLine("00000000:4972", "0A", "13")+ // 18802 acornfox server
			tcpLine("00000000:4994", "0A", "14")+ // 18836 docker-proxy of another container
			tcpLine("00000000:4995", "0A", "15")+ // 18837 no owner known
			tcpLine("00000000:4996", "0A", "16")+ // 18838 process in another container
			tcpLine("00000000:1F90", "0A", "17"), // 8080: not a wanted port
		"")
	f.proc("812", "nginx", "0::/system.slice/nginx.service\n", "10")
	f.proc("900", "caddy", "0::/system.slice/docker-"+edgeID+".scope\n", "11", "12")
	f.proc("700", "acornfox", "0::/system.slice/acornfox-server.service\n", "13")
	f.proc("950", "docker-proxy", "0::/system.slice/docker.service\n", "14")
	f.proc("960", "node", "0::/system.slice/docker-"+otherID+".scope\n", "16")
	f.proc("961", "sleep", "0::/user.slice\n", "17")

	owners, unreadable := SocketOwners(f.root)
	if unreadable != 0 {
		t.Fatalf("unreadable %d", unreadable)
	}
	ls, err := ReadListeners(f.root)
	if err != nil {
		t.Fatal(err)
	}
	containers := []Container{
		{ID: edgeID, Name: "acornfox-caddy", Ours: true},
		{ID: otherID, Name: "web-1", Ports: []int{18836}},
	}
	wanted := func(p int) bool { return p == 80 || p == 443 || p == 18808 || p == 18802 || (p >= 18810 && p <= 18899) }
	cs := Conflicts(ls, owners, containers, wanted)
	var got []string
	for _, c := range cs {
		got = append(got, c.Text())
	}
	want := []string{
		"80 被 nginx（PID 812）占用",
		"18836 被 容器 web-1（docker-proxy，PID 950）占用",
		"18837 被 没有权限查看的进程占用",
		"18838 被 容器 web-1（进程 node，PID 960）占用",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !cs[2].Unknown || cs[0].Unknown {
		t.Fatalf("unknown flags %+v", cs)
	}

	// Without the container list (Docker down) a containerised owner is
	// unknown, never blamed on someone else.
	cs = Conflicts(ls, owners, nil, wanted)
	for _, c := range cs {
		if c.Port == 443 || c.Port == 18808 || c.Port == 18836 || c.Port == 18838 {
			if !c.Unknown {
				t.Fatalf("port %d should be unknown without containers: %+v", c.Port, c)
			}
		}
		if c.Port == 18802 {
			t.Fatal("acornfox itself is never a conflict")
		}
	}
	if JoinConflicts(cs, 2) == "" || !strings.Contains(JoinConflicts(cs, 2), "等，共") {
		t.Fatalf("join %q", JoinConflicts(cs, 2))
	}
}

func TestConflictBindAddress(t *testing.T) {
	f := newFakeProc(t, tcpLine("0100007F:0050", "0A", "1"), "")
	f.proc("5", "httpd", "", "1")
	owners, _ := SocketOwners(f.root)
	ls, _ := ReadListeners(f.root)
	cs := Conflicts(ls, owners, []Container{}, func(int) bool { return true })
	if len(cs) != 1 || cs[0].Text() != "80（127.0.0.1）被 httpd（PID 5）占用" {
		t.Fatalf("%+v", cs)
	}
}

func TestSocketOwnersUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	f := newFakeProc(t, tcpLine("00000000:0050", "0A", "1"), "")
	f.proc("5", "nginx", "", "1")
	if err := os.Chmod(filepath.Join(f.root, "5", "fd"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(f.root, "5", "fd"), 0o755) })
	owners, unreadable := SocketOwners(f.root)
	if unreadable != 1 || len(owners) != 0 {
		t.Fatalf("owners %v unreadable %d", owners, unreadable)
	}
}

func TestDiskLevel(t *testing.T) {
	for _, c := range []struct {
		free uint64
		want string
	}{{0, "fail"}, {FailFreeBytes - 1, "fail"}, {FailFreeBytes, "warn"}, {WarnFreeBytes - 1, "warn"}, {WarnFreeBytes, "ok"}, {100 << 30, "ok"}} {
		if got := DiskLevel(c.free); got != c.want {
			t.Fatalf("DiskLevel(%d) = %s, want %s", c.free, got, c.want)
		}
	}
	if FormatBytes(512<<20) != "512 MB" || FormatBytes(35<<30+(1<<29)) != "35.5 GB" {
		t.Fatalf("%s %s", FormatBytes(512<<20), FormatBytes(35<<30+(1<<29)))
	}
}

func TestExistingDir(t *testing.T) {
	d := t.TempDir()
	if got := ExistingDir(filepath.Join(d, "a", "b")); got != d {
		t.Fatalf("%s", got)
	}
}
