package main

// LAN relay for runtimes whose Docker cannot bind this computer's LAN
// address: the built-in runtime on Windows (Docker inside WSL 2, NAT mode)
// and Colima on macOS (forwards 127.0.0.1 only). The edge then publishes
// each app's LAN listener on 127.0.0.1:P+offset (reconcile/edge.go), and this
// relay, running in acornfox serve itself, listens on <LAN address>:P and
// copies bytes to 127.0.0.1:P+offset. "Local only" apps have no listener on
// P+offset, so a LAN connection to them is refused, as on Linux.
//
// The relay starts only after the edge reported the LAN address unbindable
// (Reconciler.LANUnbindable) and follows LAN address changes. It is plain TCP
// in both directions (half-close kept), so WebSocket and long-lived
// connections pass unchanged.

import (
	"context"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

type lanForwarder struct {
	lanHost          func() string
	active           func(host string) bool
	portMin, portMax int
	offset           int
	logf             func(format string, args ...any)

	syncMu    sync.Mutex // one sync at a time (Run and the setup check)
	mu        sync.Mutex
	host      string
	listeners []net.Listener
	failed    []int
}

// lanForwardInterval is how often the relay checks the LAN address.
var lanForwardInterval = 10 * time.Second

// Run keeps the relay in line with the LAN address until ctx ends.
func (f *lanForwarder) Run(ctx context.Context) {
	t := time.NewTicker(lanForwardInterval)
	defer t.Stop()
	for {
		f.sync()
		select {
		case <-ctx.Done():
			f.stop()
			return
		case <-t.C:
		}
	}
}

// sync starts, moves or stops the listeners to match the LAN address. Run
// calls it periodically; the setup check calls it before reading Status.
func (f *lanForwarder) sync() {
	f.syncMu.Lock()
	defer f.syncMu.Unlock()
	host := f.lanHost()
	want := host != "" && f.active(host)
	f.mu.Lock()
	cur := f.host
	f.mu.Unlock()
	switch {
	case want && host != cur:
		f.stop()
		f.start(host)
	case !want && cur != "":
		f.stop()
	}
}

func (f *lanForwarder) start(host string) {
	var lns []net.Listener
	var failed []int
	for p := f.portMin; p <= f.portMax; p++ {
		ln, err := net.Listen("tcp4", net.JoinHostPort(host, strconv.Itoa(p)))
		if err != nil {
			failed = append(failed, p)
			continue
		}
		lns = append(lns, ln)
		go f.serve(ln, "127.0.0.1:"+strconv.Itoa(p+f.offset))
	}
	f.mu.Lock()
	f.host, f.listeners, f.failed = host, lns, failed
	f.mu.Unlock()
	f.logf("lan relay: %s:%d-%d -> 127.0.0.1:+%d (%d listening, %d failed %v)", host, f.portMin, f.portMax, f.offset, len(lns), len(failed), failed)
}

func (f *lanForwarder) stop() {
	f.mu.Lock()
	lns := f.listeners
	f.host, f.listeners, f.failed = "", nil, nil
	f.mu.Unlock()
	for _, ln := range lns {
		_ = ln.Close()
	}
}

func (f *lanForwarder) serve(ln net.Listener, target string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go relay(c, target)
	}
}

// relay copies both directions between c and target, passing half-closes on.
func relay(c net.Conn, target string) {
	defer c.Close()
	u, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return // nothing listens there: a "local only" app or no app
	}
	defer u.Close()
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(u, c)
	go pipe(c, u)
	<-done
	<-done
}

// Status reports the relay: the LAN address it listens on ("" when off), how
// many app ports listen, and the ports that could not be opened.
func (f *lanForwarder) Status() (host string, listening int, failed []int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.host, len(f.listeners), append([]int(nil), f.failed...)
}

// Serving reports whether the relay listens on host.
func (f *lanForwarder) Serving(host string) bool {
	h, n, _ := f.Status()
	return host != "" && h == host && n > 0
}
