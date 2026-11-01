package main

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/acornfox/acornfox/internal/apiserver"
)

// validateTrustedListen rejects anything that is not an absolute unix socket
// path (optionally "unix:"-prefixed). TCP addresses are refused because the
// trusted API is unauthenticated; the browser console uses -console-listen.
func validateTrustedListen(addr string) error {
	if _, ok := stripUnix(addr); ok {
		if err := apiserver.ValidateListen(addr); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("-listen 只接受 unix socket（例如 unix:/run/acornfox/api.sock）；浏览器控制台请使用 -console-listen")
}

// validateConsoleListen accepts an empty value (console disabled) or a loopback
// TCP address (127.0.0.1, ::1, localhost). Non-loopback is rejected so the
// console is never bound to a public interface.
func validateConsoleListen(addr string) error {
	if addr == "" {
		return nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("-console-listen 需要 host:port 形式的回环地址")
	}
	if port == "" {
		return fmt.Errorf("-console-listen 缺少端口")
	}
	switch host {
	case "127.0.0.1", "::1", "localhost":
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("-console-listen 只允许回环地址（127.0.0.1、::1、localhost）")
}

// listenList is a repeatable string flag preserving order.
type listenList []string

func (l *listenList) String() string { return strings.Join(*l, ",") }
func (l *listenList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// lookupGroupID resolves a group name (or numeric gid) to a gid.
func lookupGroupID(name string) (int, error) {
	if g, err := user.LookupGroup(name); err == nil {
		return strconv.Atoi(g.Gid)
	}
	// Allow a numeric group id directly.
	if id, err := strconv.Atoi(name); err == nil {
		return id, nil
	}
	return 0, fmt.Errorf("unknown group %q", name)
}

// listenAPI opens the API listener for a validated address: an absolute path
// (optionally "unix:"-prefixed) yields a unix socket, otherwise a TCP listener.
// For unix sockets it removes any stale socket first, then sets mode 0660 and,
// when gid >= 0, chowns the socket group so only that group may connect.
func listenAPI(addr string, gid int) (net.Listener, error) {
	if p, ok := stripUnix(addr); ok {
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		ln, err := net.Listen("unix", p)
		if err != nil {
			return nil, err
		}
		if gid >= 0 {
			if err := os.Chown(p, -1, gid); err != nil {
				_ = ln.Close()
				return nil, fmt.Errorf("chown socket group: %w", err)
			}
		}
		if err := os.Chmod(p, 0o660); err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("chmod socket: %w", err)
		}
		return ln, nil
	}
	return net.Listen("tcp", addr)
}

func stripUnix(addr string) (string, bool) {
	if len(addr) > 5 && addr[:5] == "unix:" {
		return addr[5:], true
	}
	if len(addr) > 0 && addr[0] == '/' {
		return addr, true
	}
	return "", false
}
