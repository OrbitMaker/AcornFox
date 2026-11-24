package macvm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Daemon is the small resident program on the Mac (acornfox mac daemon,
// started by launchd at login):
//
//   - it listens on the Mac for every port the virtual machine serves and
//     passes each connection on to the machine's address: the machine's own
//     sshd (Mac port Config.SSHPort, 127.0.0.1 only; the acornfox command
//     line uses it), the LAN console 18802 and the apps' ports 18810-18899.
//     Apps that open "only on this computer" are listened for on 127.0.0.1;
//     the others on all interfaces (when Config.LAN), so other devices in the
//     house can open them;
//   - a connection to a stopped machine starts it and waits (the first access
//     takes a few seconds), so nobody has to start anything;
//   - when nothing was accessed for Config.IdleMinutes the machine is stopped,
//     because that is the only way to give its memory back to macOS; it is not
//     stopped while an app is being installed;
//   - every probe interval it looks inside the running machine for the ports
//     it serves and the apps' "only this computer" flag, and keeps the last
//     known list in ports.json, so a stopped machine still has its ports
//     listened on;
//   - the machine's address changes at every start; the daemon asks again each
//     time.
type Daemon struct {
	Paths Paths
	// Cfg returns the current settings (read again at each tick, so
	// "acornfox mac set" takes effect without a restart).
	Cfg func() Config
	VM  VM
	Log func(format string, args ...any)

	// Dial opens a connection to a port of the machine. Default: TCP to ip:port.
	Dial func(ctx context.Context, ip string, port int) (net.Conn, error)
	// Listen opens a listener. Default: net.Listen("tcp", addr).
	Listen func(addr string) (net.Listener, error)
	Now    func() time.Time

	WakeTimeout time.Duration // how long a connection waits for a sleeping machine (default 2 minutes)
	ReadyWait   time.Duration // how long a wake waits for AcornFox in the machine (default 1 minute)
	ProbeEvery  time.Duration // default 15 seconds
	IdleEvery   time.Duration // how often idleness is checked (default 20 seconds)

	mu     sync.Mutex
	lns    map[int]*fwdListener
	apps   []PortSpec // last known ports of the apps and the LAN console
	ip     string
	run    bool
	busy   bool
	wakeMu sync.Mutex // one start or stop at a time
	last   atomic.Int64
	// stopped counts the stops the daemon made because of idleness; wakes counts starts it made because of access.
	stopped atomic.Int64
	wakes   atomic.Int64
}

type fwdListener struct {
	port   int // Mac port
	vmPort int
	lan    bool
	ln     net.Listener
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(format, args...)
	}
}

func (d *Daemon) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Daemon) touch() { d.last.Store(d.now().UnixNano()) }

func (d *Daemon) idleFor() time.Duration {
	l := d.last.Load()
	if l == 0 {
		return 0
	}
	return d.now().Sub(time.Unix(0, l))
}

func dur(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// Stats says how many times the daemon woke and stopped the machine.
func (d *Daemon) Stats() (wakes, stops int64) { return d.wakes.Load(), d.stopped.Load() }

// Run runs until ctx ends.
func (d *Daemon) Run(ctx context.Context) error {
	d.mu.Lock()
	d.lns = map[int]*fwdListener{}
	d.apps = LoadPorts(d.Paths)
	d.mu.Unlock()
	d.touch()
	d.applyPorts()
	d.logf("转发程序启动：虚拟机 %s，闲置 %d 分钟后停机", d.Cfg().VM, d.Cfg().IdleMinutes)
	d.refresh(ctx)
	probe := time.NewTicker(dur(d.ProbeEvery, 15*time.Second))
	idle := time.NewTicker(dur(d.IdleEvery, 20*time.Second))
	defer probe.Stop()
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			d.closeAll()
			return ctx.Err()
		case <-probe.C:
			d.refresh(ctx)
		case <-idle.C:
			d.checkIdle(ctx)
		}
	}
}

func (d *Daemon) closeAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for p, l := range d.lns {
		_ = l.ln.Close()
		delete(d.lns, p)
	}
}

// wantedForwards is the full list: sshd of the machine first, then the apps.
func (d *Daemon) wantedForwards() []fwdListener {
	cfg := d.Cfg()
	out := []fwdListener{{port: cfg.SSHPort, vmPort: 22, lan: false}}
	for _, a := range d.apps {
		if a.Port == cfg.SSHPort {
			continue
		}
		out = append(out, fwdListener{port: a.Port, vmPort: a.Port, lan: a.LAN && cfg.LAN})
	}
	return out
}

// applyPorts makes the listeners match the wanted list.
func (d *Daemon) applyPorts() {
	d.mu.Lock()
	defer d.mu.Unlock()
	want := map[int]fwdListener{}
	for _, w := range d.wantedForwards() {
		want[w.port] = w
	}
	for p, l := range d.lns {
		w, ok := want[p]
		if !ok || w.lan != l.lan {
			_ = l.ln.Close()
			delete(d.lns, p)
			d.logf("不再转发 %d", p)
		}
	}
	ports := make([]int, 0, len(want))
	for p := range want {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	for _, p := range ports {
		if _, ok := d.lns[p]; ok {
			continue
		}
		w := want[p]
		host := "127.0.0.1"
		if w.lan {
			host = "0.0.0.0"
		}
		addr := net.JoinHostPort(host, strconv.Itoa(p))
		listen := d.Listen
		if listen == nil {
			listen = func(a string) (net.Listener, error) { return net.Listen("tcp", a) }
		}
		ln, err := listen(addr)
		if err != nil {
			d.logf("MAC-PORT-01 Mac 上 %s 已被占用或不让监听（%v），这个端口不转发", addr, err)
			continue
		}
		fl := &fwdListener{port: p, vmPort: w.vmPort, lan: w.lan, ln: ln}
		d.lns[p] = fl
		d.logf("转发 %s -> 虚拟机 %d", addr, w.vmPort)
		go d.accept(fl)
	}
}

// Listening returns the Mac ports being listened on (for tests and status).
func (d *Daemon) Listening() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]int, 0, len(d.lns))
	for p := range d.lns {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

func (d *Daemon) accept(fl *fwdListener) {
	for {
		c, err := fl.ln.Accept()
		if err != nil {
			return
		}
		go d.handle(c, fl)
	}
}

// ensureUp returns the machine's address, starting the machine when it is not
// running.
func (d *Daemon) ensureUp(ctx context.Context) (string, error) {
	d.wakeMu.Lock()
	defer d.wakeMu.Unlock()
	d.mu.Lock()
	run, ip := d.run, d.ip
	d.mu.Unlock()
	if run && ip != "" {
		return ip, nil
	}
	st, err := d.VM.Status(ctx)
	if err != nil {
		return "", err
	}
	if st.Running && st.IP != "" {
		d.setState(st.Running, st.IP)
		return st.IP, nil
	}
	if !st.Exists {
		return "", errors.New("虚拟机不存在（卸载过？用 acornfox mac doctor 看）")
	}
	d.logf("有人访问，叫起虚拟机")
	t0 := d.now()
	if err := d.VM.Start(ctx); err != nil {
		return "", err
	}
	st, err = d.VM.Status(ctx)
	if err != nil {
		return "", err
	}
	if !st.Running || st.IP == "" {
		return "", errors.New("虚拟机起来了但拿不到地址")
	}
	d.wakes.Add(1)
	d.setState(true, st.IP)
	d.touch()
	d.logf("虚拟机已起来（%s，用时 %.1f 秒），等里面的 AcornFox 就绪", st.IP, d.now().Sub(t0).Seconds())
	// sshd answers a second or two after the start, AcornFox a few seconds later:
	// hold the first connections until it does (up to a minute, then let them try).
	deadline := time.Now().Add(dur(d.ReadyWait, time.Minute))
	for !d.VM.Ready(ctx) {
		if time.Now().After(deadline) || ctx.Err() != nil {
			d.logf("等了一分钟里面的 AcornFox 还没就绪，先放连接过去试")
			break
		}
		if err := sleepCtx(ctx, time.Second); err != nil {
			break
		}
	}
	d.touch()
	d.logf("虚拟机和里面的 AcornFox 都就绪（共 %.1f 秒）", d.now().Sub(t0).Seconds())
	return st.IP, nil
}

func (d *Daemon) setState(run bool, ip string) {
	d.mu.Lock()
	d.run, d.ip = run, ip
	d.mu.Unlock()
}

// forget drops the cached address: the next access asks again.
func (d *Daemon) forget() { d.setState(false, "") }

func (d *Daemon) dial(ctx context.Context, ip string, port int) (net.Conn, error) {
	if d.Dial != nil {
		return d.Dial(ctx, ip, port)
	}
	if runtime.GOOS == "darwin" {
		return NCDial(ctx, ip, port) // see ncdial.go: our own sockets may not reach the machine
	}
	var nd net.Dialer
	nd.Timeout = 5 * time.Second
	return nd.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
}

func (d *Daemon) handle(c net.Conn, fl *fwdListener) {
	defer c.Close()
	d.touch()
	wake := dur(d.WakeTimeout, 2*time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), wake)
	defer cancel()
	var backend net.Conn
	lastLogged := ""
	for {
		ip, err := d.ensureUp(ctx)
		if err == nil {
			backend, err = d.dial(ctx, ip, fl.vmPort)
			if err == nil {
				break
			}
			// The address may have changed, or the service inside is still starting.
			d.forget()
		}
		if err != nil && err.Error() != lastLogged {
			lastLogged = err.Error()
			d.logf("端口 %d：连虚拟机没成功，再试（%v）", fl.port, err)
		}
		if ctx.Err() != nil {
			d.logf("等虚拟机超时（端口 %d）：%v", fl.port, err)
			return
		}
		if err := sleepCtx(ctx, 500*time.Millisecond); err != nil {
			return
		}
	}
	defer backend.Close()
	d.touch()
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, &touchReader{r: src, d: d})
		if tc, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}
	go cp(backend, c)
	go cp(c, backend)
	<-done
	<-done
}

type touchReader struct {
	r io.Reader
	d *Daemon
}

func (t *touchReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.d.touch()
	}
	return n, err
}

// refresh looks inside the running machine and updates the ports.
func (d *Daemon) refresh(ctx context.Context) {
	st, err := d.VM.Status(ctx)
	if err != nil {
		d.logf("看虚拟机状态出错：%v", err)
		return
	}
	d.mu.Lock()
	wasRun, wasIP := d.run, d.ip
	d.mu.Unlock()
	if !st.Running {
		if wasRun {
			d.logf("虚拟机已经停了")
		}
		d.setState(false, "")
		return
	}
	if st.IP != "" && (st.IP != wasIP || !wasRun) {
		if wasIP != "" && st.IP != wasIP {
			d.logf("虚拟机地址变了：%s -> %s", wasIP, st.IP)
		}
		d.setState(true, st.IP)
	}
	pctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	p, err := d.VM.Probe(pctx)
	if err != nil {
		d.logf("看虚拟机里的端口出错（保留原来的）：%v", err)
		d.applyPorts() // a changed setting (LAN on or off) still takes effect
		return
	}
	d.mu.Lock()
	d.busy = p.Busy
	old := d.apps
	d.mu.Unlock()
	want := WantedPorts(p, old)
	if !samePorts(want, old) {
		d.mu.Lock()
		d.apps = want
		d.mu.Unlock()
		if err := SavePorts(d.Paths, want); err != nil {
			d.logf("ports.json 写不了：%v", err)
		}
		d.applyPorts()
	} else {
		// The settings may have changed (LAN on or off): make the listeners match.
		d.applyPorts()
	}
}

func samePorts(a, b []PortSpec) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[PortSpec]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}

// checkIdle stops the machine when nothing was accessed for the idle time.
func (d *Daemon) checkIdle(ctx context.Context) {
	cfg := d.Cfg()
	if cfg.IdleMinutes <= 0 {
		return
	}
	d.mu.Lock()
	run, busy := d.run, d.busy
	d.mu.Unlock()
	if !run {
		return
	}
	if d.idleFor() < time.Duration(cfg.IdleMinutes)*time.Minute {
		return
	}
	if busy {
		// Look again: the busy flag is from the last probe.
		pctx, cancel := context.WithTimeout(ctx, 40*time.Second)
		p, err := d.VM.Probe(pctx)
		cancel()
		if err == nil && p.Busy {
			d.logf("闲置已超过 %d 分钟，但虚拟机里还有应用在安装或升级，先不停", cfg.IdleMinutes)
			return
		}
	}
	d.wakeMu.Lock()
	defer d.wakeMu.Unlock()
	// Someone may have come in while the lock was taken.
	if d.idleFor() < time.Duration(cfg.IdleMinutes)*time.Minute {
		return
	}
	d.logf("闲置 %d 分钟没人访问，停虚拟机（把内存还给 macOS；有人访问会自动叫起）", cfg.IdleMinutes)
	t0 := d.now()
	if err := d.VM.Stop(ctx); err != nil {
		d.logf("停虚拟机出错：%v", err)
		return
	}
	d.setState(false, "")
	d.stopped.Add(1)
	d.logf("虚拟机已停（用时 %.1f 秒）", d.now().Sub(t0).Seconds())
}

// String is a one-line summary for logs.
func (d *Daemon) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fmt.Sprintf("running=%v ip=%s ports=%d", d.run, d.ip, len(d.lns))
}
