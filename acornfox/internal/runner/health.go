package runner

// Health checks of app containers that do not answer HTTP, as the server
// side (the install engine and the reconciler) runs them. A catalog
// manifest says one of three ways for an endpoint: a GET to a path (the
// caller's own HTTP probe), a command run inside the app's own container
// (exec: exit code 0, and the trimmed stdout equal to expect_stdout when one
// is written, means up) or a TCP connect to the endpoint's port (tcp).
//
// Where this machine connects (ProbeAddr): for an app that is opened from
// outside, the loopback port the container publishes, as before (nothing
// when it publishes none: such a container is not up for its users); for an
// internal app, the container's address on its own app network
// (ContainerInfo.IP). An internal app publishes no port: the server process
// reaches the address over Docker's bridge, which
// on a Linux host is routed to the host without any extra permission (no
// Docker exec, no helper container, no host port). It does not work where
// the container network is not routed to the process running the server
// (Docker Desktop on macOS or Windows, rootless Docker); a manifest for such
// an app should check with exec, which goes through Docker.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// HealthExecTimeoutS bounds one exec health check: a check command answers
// quickly or the app is not up yet (the caller retries until its deadline).
const HealthExecTimeoutS = 10

// HealthTCPTimeout bounds one TCP connect of a tcp health check.
const HealthTCPTimeout = 2 * time.Second

// ProbeAddr is the host:port this machine connects to for the container
// port port of ci. An app that is not internal: 127.0.0.1:<HostPort>, the
// one port it publishes, or "" while it publishes none (never its address on
// the app network: a container whose port is not published has no route and
// is not up, as before). An internal app: ci.IP:port on its app network, ""
// while that is not known (not running, or no port).
func ProbeAddr(ci ContainerInfo, port int, internal bool) string {
	if !internal {
		if ci.HostPort > 0 {
			return net.JoinHostPort("127.0.0.1", strconv.Itoa(ci.HostPort))
		}
		return ""
	}
	if ci.IP != "" && port > 0 {
		return net.JoinHostPort(ci.IP, strconv.Itoa(port))
	}
	return ""
}

// ProbeTCP connects to addr once and closes the connection: nil when the
// port accepts a connection within HealthTCPTimeout.
func ProbeTCP(ctx context.Context, addr string) error {
	if addr == "" {
		return errors.New("还不知道容器的地址")
	}
	d := net.Dialer{Timeout: HealthTCPTimeout}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("连不上 %s", addr)
	}
	_ = c.Close()
	return nil
}

// ErrNoExec is returned by ProbeExec when the runner cannot run a command in
// a container (a runner older than the server).
var ErrNoExec = errors.New("执行器不支持在容器里执行命令（执行器版本比服务旧）")

// ProbeExec runs one exec health check of the app container name: command
// as written (an argument vector, no shell), at most HealthExecTimeoutS. It
// is nil when the command exits 0 and, when expectStdout is not "", its
// standard output with surrounding white space removed equals expectStdout.
// The error says why in words without the command's output (a check command
// may print what is not for logs). api nil is ErrNoExec.
func ProbeExec(ctx context.Context, api ExecAPI, app, name string, command []string, expectStdout string) error {
	if api == nil {
		return ErrNoExec
	}
	if len(command) == 0 {
		return errors.New("健康检查没有写命令")
	}
	res, err := api.Exec(ctx, ExecRequest{App: app, Name: name, Command: command, TimeoutS: HealthExecTimeoutS})
	if err != nil {
		if errors.Is(err, ErrUnsupported) {
			return ErrNoExec
		}
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrStopped) {
			return errors.New("容器没在运行")
		}
		var re *RemoteError
		if errors.As(err, &re) && re.Code == CodeExecFailed {
			return fmt.Errorf("检查命令 %s 没能在容器里运行（容器里没有这个程序或用户）", command[0])
		}
		return fmt.Errorf("没能执行检查命令：%v", err)
	}
	switch {
	case res.TimedOut:
		return fmt.Errorf("检查命令 %d 秒内没结束", HealthExecTimeoutS)
	case res.ExitCode != 0:
		return fmt.Errorf("检查命令退出码 %d", res.ExitCode)
	case expectStdout != "" && (res.StdoutTruncated || strings.TrimSpace(string(res.Stdout)) != expectStdout):
		return errors.New("检查命令的输出和预期不符")
	}
	return nil
}
