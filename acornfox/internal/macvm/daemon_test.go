package macvm

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeVM is a machine that can be started, stopped and probed.
type fakeVM struct {
	mu      sync.Mutex
	exists  bool
	running bool
	ip      string
	nextIP  int
	starts  int
	stops   int
	probe   Probe
	busy    bool
	startFn func() error
}

func newFakeVM() *fakeVM { return &fakeVM{exists: true, nextIP: 3} }

func (f *fakeVM) Status(ctx context.Context) (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return State{Exists: f.exists, Running: f.running, IP: f.ip}, nil
}

func (f *fakeVM) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startFn != nil {
		if err := f.startFn(); err != nil {
			return err
		}
	}
	f.running = true
	f.ip = "192.168.64." + strconv.Itoa(f.nextIP)
	f.nextIP++
	f.starts++
	return nil
}

func (f *fakeVM) Ready(ctx context.Context) bool { return true }

func (f *fakeVM) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = false
	f.ip = ""
	f.stops++
	return nil
}

func (f *fakeVM) Probe(ctx context.Context) (Probe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.probe
	p.Busy = f.busy
	return p, nil
}

func (f *fakeVM) set(fn func(f *fakeVM)) { f.mu.Lock(); fn(f); f.mu.Unlock() }

// harness wires a Daemon to fake listeners: the "Mac" ports are real
// loopback listeners on random ports, the "machine" ports are echo servers.
type harness struct {
	t        *testing.T
	d        *Daemon
	vm       *fakeVM
	paths    Paths
	mu       sync.Mutex
	macAddr  map[int]string // Mac port -> the real address the daemon listens on
	bindWant map[int]string // Mac port -> the address the daemon asked for
	backends map[int]string // machine port -> real echo server address
	dialIPs  []string
	cfg      Config
	now      time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, vm: newFakeVM(), paths: Paths{Root: t.TempDir()}, macAddr: map[int]string{}, bindWant: map[int]string{}, backends: map[int]string{}, now: time.Unix(1_800_000_000, 0)}
	h.cfg = Config{VM: "acornfox", SSHPort: 18808, IdleMinutes: 30, LAN: true}
	h.d = &Daemon{
		Paths: h.paths,
		Cfg:   func() Config { h.mu.Lock(); defer h.mu.Unlock(); return h.cfg },
		VM:    h.vm,
		Log:   func(f string, a ...any) { t.Logf("daemon: "+f, a...) },
		Now:   func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.now },
		Listen: func(addr string) (net.Listener, error) {
			_, ps, _ := net.SplitHostPort(addr)
			p, _ := strconv.Atoi(ps)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err == nil {
				h.mu.Lock()
				h.macAddr[p] = ln.Addr().String()
				h.bindWant[p] = addr
				h.mu.Unlock()
			}
			return ln, err
		},
		Dial: func(ctx context.Context, ip string, port int) (net.Conn, error) {
			h.mu.Lock()
			addr, ok := h.backends[port]
			h.dialIPs = append(h.dialIPs, ip)
			h.mu.Unlock()
			if !ok {
				return nil, &net.OpError{Op: "dial", Err: io.ErrClosedPipe}
			}
			return net.Dial("tcp", addr)
		},
		WakeTimeout: 5 * time.Second,
		ProbeEvery:  50 * time.Millisecond,
		IdleEvery:   50 * time.Millisecond,
	}
	return h
}

// backend starts an echo server standing in for a machine port.
func (h *harness) backend(port int) {
	h.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { ln.Close() })
	h.mu.Lock()
	h.backends[port] = ln.Addr().String()
	h.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
}

func (h *harness) start() (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = h.d.Run(ctx); close(done) }()
	h.waitFor("daemon listens on ssh port", func() bool { return len(h.d.Listening()) > 0 })
	return func() { cancel(); <-done }
}

func (h *harness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("等不到：%s", what)
}

func (h *harness) advance(d time.Duration) { h.mu.Lock(); h.now = h.now.Add(d); h.mu.Unlock() }

func (h *harness) addr(macPort int) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.macAddr[macPort]
}

func (h *harness) echo(macPort int, msg string) string {
	h.t.Helper()
	a := h.addr(macPort)
	if a == "" {
		h.t.Fatalf("Mac 端口 %d 没有在听", macPort)
	}
	c, err := net.DialTimeout("tcp", a, 3*time.Second)
	if err != nil {
		h.t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		h.t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		h.t.Fatalf("读回应：%v", err)
	}
	return string(buf)
}

func TestWakeOnConnectAndFollowIP(t *testing.T) {
	h := newHarness(t)
	h.backend(22)
	h.vm.set(func(f *fakeVM) { f.running = false })
	stop := h.start()
	defer stop()

	// A stopped machine: the first connection starts it and is served.
	if got := h.echo(18808, "hello"); got != "hello" {
		t.Fatalf("回应不对：%q", got)
	}
	if h.vm.starts != 1 {
		t.Fatalf("应当起一次虚拟机，实际 %d", h.vm.starts)
	}
	// Idle for longer than the limit: it stops.
	h.advance(31 * time.Minute)
	h.waitFor("虚拟机因闲置停下", func() bool { h.vm.mu.Lock(); defer h.vm.mu.Unlock(); return h.vm.stops == 1 })
	// The next access starts it again, with a new address.
	if got := h.echo(18808, "again"); got != "again" {
		t.Fatalf("回应不对：%q", got)
	}
	h.vm.mu.Lock()
	starts := h.vm.starts
	h.vm.mu.Unlock()
	if starts != 2 {
		t.Fatalf("应当起了两次，实际 %d", starts)
	}
	h.mu.Lock()
	ips := append([]string(nil), h.dialIPs...)
	h.mu.Unlock()
	if len(ips) < 2 || ips[0] == ips[len(ips)-1] {
		t.Fatalf("第二次应连新地址：%v", ips)
	}
	w, s := h.d.Stats()
	if w != 2 || s != 1 {
		t.Fatalf("唤醒 %d 次、停 %d 次，应为 2 和 1", w, s)
	}
}

func TestIdleNotWhileBusyOrDisabled(t *testing.T) {
	h := newHarness(t)
	h.backend(22)
	h.vm.set(func(f *fakeVM) { f.running = true; f.ip = "192.168.64.9"; f.busy = true })
	stop := h.start()
	defer stop()
	h.advance(40 * time.Minute)
	time.Sleep(400 * time.Millisecond) // several idle checks
	h.vm.mu.Lock()
	stops := h.vm.stops
	h.vm.mu.Unlock()
	if stops != 0 {
		t.Fatalf("有应用在安装时不该停机，停了 %d 次", stops)
	}
	// The install finished: now it stops.
	h.vm.set(func(f *fakeVM) { f.busy = false })
	h.waitFor("装完后停机", func() bool { h.vm.mu.Lock(); defer h.vm.mu.Unlock(); return h.vm.stops == 1 })

	// Idle off: never stops.
	h.vm.set(func(f *fakeVM) { f.running = true; f.ip = "192.168.64.10" })
	h.mu.Lock()
	h.cfg.IdleMinutes = 0
	h.mu.Unlock()
	h.d.setState(true, "192.168.64.10")
	h.advance(10 * time.Hour)
	time.Sleep(300 * time.Millisecond)
	h.vm.mu.Lock()
	stops = h.vm.stops
	h.vm.mu.Unlock()
	if stops != 1 {
		t.Fatalf("闲置设成 0 不该停机，总共停了 %d 次", stops)
	}
}

func TestTrafficKeepsMachineAwake(t *testing.T) {
	h := newHarness(t)
	h.backend(22)
	h.vm.set(func(f *fakeVM) { f.running = true; f.ip = "192.168.64.9" })
	stop := h.start()
	defer stop()
	h.advance(20 * time.Minute)
	if h.echo(18808, "x") != "x" {
		t.Fatal("没转发")
	}
	h.advance(20 * time.Minute) // 40 minutes since start, but only 20 since the last access
	time.Sleep(300 * time.Millisecond)
	h.vm.mu.Lock()
	stops := h.vm.stops
	h.vm.mu.Unlock()
	if stops != 0 {
		t.Fatalf("20 分钟前刚有人访问过，不该停机，停了 %d 次", stops)
	}
}

func TestPortsFollowTheMachine(t *testing.T) {
	h := newHarness(t)
	h.backend(22)
	h.backend(18802)
	h.backend(18811)
	h.backend(18812)
	h.vm.set(func(f *fakeVM) {
		f.running = true
		f.ip = "192.168.64.9"
		f.probe = Probe{IP: "192.168.64.9", Listening: []int{18802, 18811, 18812}, LocalOnly: []int{18811}, AppsOK: true}
	})
	stop := h.start()
	defer stop()
	h.waitFor("应用端口开始转发", func() bool {
		ls := h.d.Listening()
		return len(ls) == 4
	})
	h.mu.Lock()
	want := map[int]string{18808: "127.0.0.1:18808", 18802: "0.0.0.0:18802", 18811: "127.0.0.1:18811", 18812: "0.0.0.0:18812"}
	for p, w := range want {
		if h.bindWant[p] != w {
			t.Errorf("端口 %d 应监听 %s，实际 %s", p, w, h.bindWant[p])
		}
	}
	h.mu.Unlock()
	if h.echo(18812, "app") != "app" || h.echo(18802, "con") != "con" {
		t.Fatal("应用端口没转发")
	}
	// ports.json has the apps (not the ssh port).
	b, err := os.ReadFile(h.paths.Ports())
	if err != nil || !strings.Contains(string(b), "18812") || strings.Contains(string(b), "18808") {
		t.Fatalf("ports.json 不对：%q %v", b, err)
	}
	// An app goes away: its port stops being forwarded.
	h.vm.set(func(f *fakeVM) {
		f.probe = Probe{IP: "192.168.64.9", Listening: []int{18802}, AppsOK: true}
	})
	h.waitFor("应用没了端口也撤掉", func() bool { return len(h.d.Listening()) == 2 })
	// LAN off: everything is loopback only.
	h.mu.Lock()
	h.cfg.LAN = false
	h.mu.Unlock()
	h.waitFor("关局域网后改成只监听本机", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.bindWant[18802] == "127.0.0.1:18802"
	})
}

func TestStoppedMachineKeepsLastPorts(t *testing.T) {
	h := newHarness(t)
	if err := SavePorts(h.paths, []PortSpec{{Port: 18802, LAN: true}, {Port: 18813, LAN: false}}); err != nil {
		t.Fatal(err)
	}
	h.vm.set(func(f *fakeVM) { f.running = false })
	stop := h.start()
	defer stop()
	h.waitFor("上次记下的端口还在听", func() bool { return len(h.d.Listening()) == 3 })
	// The machine is not started just by the daemon looking around.
	time.Sleep(200 * time.Millisecond)
	h.vm.mu.Lock()
	starts := h.vm.starts
	h.vm.mu.Unlock()
	if starts != 0 {
		t.Fatalf("没人访问不该起虚拟机，起了 %d 次", starts)
	}
}

func TestMissingMachineClosesConnection(t *testing.T) {
	h := newHarness(t)
	h.d.WakeTimeout = 600 * time.Millisecond
	h.vm.set(func(f *fakeVM) { f.exists = false })
	stop := h.start()
	defer stop()
	c, err := net.DialTimeout("tcp", h.addr(18808), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("虚拟机不存在时连接应当被关掉")
	}
}

func TestParseProbe(t *testing.T) {
	out := `==IP==
192.168.64.5/24
==TCP==
  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000:4972 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 11 1 0000000000000000 100 0 0 10 0
   1: 0100007F:4970 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 12 1 0000000000000000 100 0 0 10 0
   2: 00000000:4973 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 13 1 0000000000000000 100 0 0 10 0
   3: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 14 1 0000000000000000 100 0 0 10 0
   4: 00000000:4973 0100007F:9999 01 00000000:00000000 00:00000000 00000000     0        0 15 1 0000000000000000 100 0 0 10 0
==APPS==
{"ok":true,"apps":[{"name":"a","url":"http://127.0.0.1:18811","local_only":true},{"name":"b","url":"http://192.168.1.2:18819"}]}
==TASKS==
{"tasks":[{"id":"x","status":"succeeded"}]}
==END==
`
	p := ParseProbe(out)
	if p.IP != "192.168.64.5" {
		t.Errorf("IP %q", p.IP)
	}
	if len(p.Listening) != 1 || p.Listening[0] != 18802 {
		t.Errorf("监听的受管端口应只有 18802：%v", p.Listening)
	}
	if !p.AppsOK || len(p.LocalOnly) != 1 || p.LocalOnly[0] != 18811 {
		t.Errorf("只本机的应用：%+v", p)
	}
	if p.Busy {
		t.Error("没有在跑的安装任务")
	}
	p2 := ParseProbe(strings.Replace(out, `"status":"succeeded"`, `"status":"running"`, 1))
	if !p2.Busy {
		t.Error("有安装任务在跑，应判为忙")
	}
	// The last command printed no newline (curl): the marker is stuck to its output.
	stuck := strings.Replace(out, "{\"tasks\":[{\"id\":\"x\",\"status\":\"succeeded\"}]}\n==END==\n", "{\"tasks\":[{\"id\":\"x\",\"status\":\"running\"}]}==END==", 1)
	if p4 := ParseProbe(stuck); !p4.Complete || !p4.Busy {
		t.Errorf("结束标记粘在最后一条输出后面也要认：%+v", p4)
	}
	if !p.Complete || ParseProbe("==IP==\n192.168.64.5/24\n==TCP==\n").Complete {
		t.Error("Complete 只在读到结束标记时为真")
	}
	p3 := ParseProbe("==IP==\n==TCP==\n==APPS==\n\n==TASKS==\n==END==\n")
	if p3.AppsOK {
		t.Error("读不到应用列表时 AppsOK 应为 false")
	}
}

func TestWantedPorts(t *testing.T) {
	old := []PortSpec{{Port: 18802, LAN: true}, {Port: 18812, LAN: true}}
	// Apps unreadable: the LAN console is taken from the probe, the other old ports stay.
	got := WantedPorts(Probe{Listening: []int{18802, 18813}}, old)
	if len(got) != 2 || got[0].Port != 18802 || got[1].Port != 18812 {
		t.Errorf("读不到应用时：%+v", got)
	}
	got = WantedPorts(Probe{Listening: []int{18802, 18811}, LocalOnly: []int{18811}, AppsOK: true}, old)
	if len(got) != 2 || got[1].Port != 18811 || got[1].LAN {
		t.Errorf("只本机的应用不该开局域网：%+v", got)
	}
}

func TestMemoryAndConfig(t *testing.T) {
	const gib = uint64(1) << 30
	cases := map[uint64]int{8 * gib: 4, 12 * gib: 4, 16 * gib: 6, 18 * gib: 6, 64 * gib: 6}
	for mem, want := range cases {
		if got := MemoryFor(mem); got != want {
			t.Errorf("%d GiB 内存应给 %d GB，实际 %d", mem/gib, want, got)
		}
	}
	if CPUsFor(11) != 4 || CPUsFor(2) != 2 || CPUsFor(1) != 2 {
		t.Error("CPU 数不对")
	}
	p := Paths{Root: filepath.Join(t.TempDir(), "mac")}
	c := DefaultConfig(18 * gib)
	c.Created.VM = true
	if err := c.Save(p); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(p)
	if err != nil || got.MemoryGB != 6 || !got.Created.VM || got.SSHPort != DefaultSSHPort || !got.LAN || got.IdleMinutes != 30 {
		t.Fatalf("读回的配置不对：%+v %v", got, err)
	}
	if _, err := LoadConfig(Paths{Root: t.TempDir()}); err != ErrNotInstalled {
		t.Fatalf("没装时应报 ErrNotInstalled：%v", err)
	}
}

func TestParseMachines(t *testing.T) {
	list := `[{"configuration":{"id":"acornfox"},"status":"running","networks":[{"address":"192.168.64.7/24","gateway":"192.168.64.1"}]},{"id":"other","status":"stopped"}]`
	ms, err := ParseMachines([]byte(list))
	if err != nil || len(ms) != 2 {
		t.Fatalf("%v %v", ms, err)
	}
	if ms[0].Name != "acornfox" || !ms[0].Running || ms[0].IP != "192.168.64.7" {
		t.Errorf("第一台：%+v", ms[0])
	}
	if ms[1].Running || ms[1].IP != "" {
		t.Errorf("第二台：%+v", ms[1])
	}
	if ms, err := ParseMachines([]byte("[]")); err != nil || len(ms) != 0 {
		t.Errorf("空列表：%v %v", ms, err)
	}
}

func TestNCDial(t *testing.T) {
	dir := t.TempDir()
	okNC := filepath.Join(dir, "nc-ok")
	failNC := filepath.Join(dir, "nc-fail")
	if err := os.WriteFile(okNC, []byte("#!/bin/sh\necho \"Connection to $4 port $5 [tcp/x] succeeded!\" >&2\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failNC, []byte("#!/bin/sh\necho \"nc: connectx to $4 port $5 (tcp) failed: Connection refused\" >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACORNFOX_MAC_NC", okNC)
	c, err := NCDial(context.Background(), "192.168.64.7", 22)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "abc" {
		t.Fatalf("回应不对：%q %v", buf, err)
	}
	if cw, ok := c.(interface{ CloseWrite() error }); !ok || cw.CloseWrite() != nil {
		t.Fatal("要能半关闭")
	}
	// After the input ends, cat ends and the read side sees the end.
	if _, err := c.Read(buf); err != io.EOF {
		t.Fatalf("对端结束后应读到 EOF：%v", err)
	}
	c.Close()

	t.Setenv("ACORNFOX_MAC_NC", failNC)
	if _, err := NCDial(context.Background(), "192.168.64.7", 22); err == nil || !strings.Contains(err.Error(), "Connection refused") {
		t.Fatalf("连不上时应带出 nc 的原因：%v", err)
	}
}
