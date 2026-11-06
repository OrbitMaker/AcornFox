package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// freePort returns a TCP port that is free on 127.0.0.1 right now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// The relay copies both ways (half-close passed on) for a port whose target
// listens, and closes connections to a port whose target does not ("local
// only" app).
func TestLANRelay(t *testing.T) {
	lanPort, lanPort2 := freePort(t), freePort(t)
	for lanPort2 != lanPort+1 { // the relay listens on a contiguous range
		lanPort = freePort(t)
		lanPort2 = lanPort + 1
		if ln, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(lanPort2)); err == nil {
			ln.Close()
			break
		}
	}
	// Echo target behind the first port only: it reads to EOF, then answers.
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	offset := target.Addr().(*net.TCPAddr).Port - lanPort
	go func() {
		for {
			c, err := target.Accept()
			if err != nil {
				return
			}
			b, _ := io.ReadAll(c)
			_, _ = c.Write(append([]byte("echo:"), b...))
			c.Close()
		}
	}()
	f := &lanForwarder{lanHost: func() string { return "127.0.0.1" }, active: func(string) bool { return true },
		portMin: lanPort, portMax: lanPort2, offset: offset, logf: func(string, ...any) {}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for !f.Serving("127.0.0.1") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !f.Serving("127.0.0.1") {
		t.Fatal("relay not listening")
	}

	c, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(lanPort))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Write([]byte("hello"))
	_ = c.(*net.TCPConn).CloseWrite() // half-close must reach the target
	got, _ := io.ReadAll(bufio.NewReader(c))
	c.Close()
	if string(got) != "echo:hello" {
		t.Fatalf("relayed %q", got)
	}

	// No target behind the second port: the connection is closed at once.
	c2, err := net.Dial("tcp4", "127.0.0.1:"+strconv.Itoa(lanPort2))
	if err != nil {
		t.Fatal(err)
	}
	_ = c2.SetReadDeadline(time.Now().Add(7 * time.Second))
	n, rerr := c2.Read(make([]byte, 1))
	c2.Close()
	if n != 0 || rerr == nil {
		t.Fatalf("connection to a local-only port stayed open: n=%d err=%v", n, rerr)
	}

	// Turning the relay off closes the listeners.
	f.active = func(string) bool { return false }
	f.sync()
	if h, n, _ := f.Status(); h != "" || n != 0 {
		t.Fatalf("relay still on: %s %d", h, n)
	}
}
