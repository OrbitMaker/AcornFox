package macvm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Why the daemon dials through /usr/bin/nc and not with its own sockets:
// since macOS 15 a program may only talk to addresses on the local network
// (the virtual machine's 192.168.64.x included) when the user allowed that
// program in "Privacy & Security → Local Network". A program started by
// launchd has no window to ask in, so macOS refuses silently ("no route to
// host"; measured on macOS 26.4 with our own acornfox: refused;
// the very same address through /usr/bin/nc or /usr/bin/ssh started by that
// program: fine, Apple's own programs are not restricted). The daemon
// therefore opens each connection to the machine with `nc` and passes the
// bytes through its standard input and output.

// ncPath is Apple's nc (ACORNFOX_MAC_NC changes it: tests).
func ncPath() string {
	if v := os.Getenv("ACORNFOX_MAC_NC"); v != "" {
		return v
	}
	return "/usr/bin/nc"
}

// NCDial connects to ip:port through nc. It returns once nc says the
// connection is made, or an error when it is not (the machine or the service
// in it is not up yet).
func NCDial(ctx context.Context, ip string, port int) (net.Conn, error) {
	cmd := exec.Command(ncPath(), "-v", "-G", "3", ip, strconv.Itoa(port))
	// Our own pipes, not StdinPipe/StdoutPipe: cmd.Wait closes those as soon as
	// nc exits, so the last bytes nc wrote could be cut off and a read at the
	// end saw "file already closed" instead of io.EOF. With os.Pipe the read
	// side is ours and ends with io.EOF once nc's copy of the write side is gone.
	inR, in, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	out, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		in.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout = inR, outW
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		inR.Close()
		in.Close()
		out.Close()
		outW.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		inR.Close()
		in.Close()
		out.Close()
		outW.Close()
		return nil, err
	}
	// nc has its own copies now.
	inR.Close()
	outW.Close()
	c := &ncConn{cmd: cmd, in: in, out: out, remote: net.JoinHostPort(ip, strconv.Itoa(port)), done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(c.done) }()
	ready := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(errPipe)
		var last string
		sent := false
		for sc.Scan() {
			line := sc.Text()
			last = line
			if !sent && strings.Contains(line, "succeeded") {
				sent = true
				ready <- nil
			}
		}
		if !sent {
			if last == "" {
				last = "nc 没有给出任何结果就退出了"
			}
			ready <- errors.New(last)
		}
	}()
	select {
	case err := <-ready:
		if err != nil {
			c.Close()
			return nil, err
		}
		return c, nil
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	case <-time.After(10 * time.Second):
		c.Close()
		return nil, fmt.Errorf("连 %s 等了 10 秒没有结果", c.remote)
	}
}

// ncConn is a net.Conn over a nc process.
type ncConn struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    io.ReadCloser
	remote string
	done   chan struct{}
	once   sync.Once
	wonce  sync.Once
}

func (c *ncConn) Read(p []byte) (int, error)  { return c.out.Read(p) }
func (c *ncConn) Write(p []byte) (int, error) { return c.in.Write(p) }

// CloseWrite says nothing more is coming; nc is given a moment to deliver
// what it still has and is then ended (macOS's nc has no option to end by
// itself when its input ends).
func (c *ncConn) CloseWrite() error {
	c.wonce.Do(func() {
		_ = c.in.Close()
		go func() {
			select {
			case <-c.done:
			case <-time.After(3 * time.Second):
				_ = c.cmd.Process.Kill()
			}
		}()
	})
	return nil
}

func (c *ncConn) Close() error {
	c.once.Do(func() {
		_ = c.in.Close()
		_ = c.cmd.Process.Kill()
		_ = c.out.Close()
	})
	return nil
}

type ncAddr string

func (a ncAddr) Network() string { return "tcp" }
func (a ncAddr) String() string  { return string(a) }

func (c *ncConn) LocalAddr() net.Addr                { return ncAddr("nc") }
func (c *ncConn) RemoteAddr() net.Addr               { return ncAddr(c.remote) }
func (c *ncConn) SetDeadline(t time.Time) error      { return nil }
func (c *ncConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *ncConn) SetWriteDeadline(t time.Time) error { return nil }
