package client

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// DetachedTunnel is an `ssh -L` forward started in a session of its own
// (`acornfox open --detach`): it keeps running after the command that
// started it has exited, and after the terminal or AI workbench that ran
// that command closes its process group, until it is stopped (StopProcess,
// `acornfox open --stop`), the connection drops or the computer restarts.
type DetachedTunnel struct {
	// LocalURL is the base URL reachable through the forward, e.g.
	// "http://127.0.0.1:54321". It never contains a token.
	LocalURL string
	// PID is the ssh process.
	PID int
}

// OpenDetachedTunnel starts ssh with the same arguments as OpenTunnelTo, in
// a session of its own with no terminal, and waits until the local port
// accepts a connection (up to tunnelConnectTimeout) or ssh exits. On
// success ssh is left running and its PID returned; on failure ssh is
// stopped and a classified connect *Error returned, as OpenTunnelTo does.
// What ssh writes goes to a temporary file read only to classify a failure
// and removed once the forward is up (or has failed).
func OpenDetachedTunnel(ctx context.Context, t Target, localPort, remotePort int) (*DetachedTunnel, error) {
	if remotePort < 1 || remotePort > 65535 {
		return nil, connectError(codeConnectFailed, "服务器端口不对")
	}
	if t.SSH == "" {
		return nil, connectError(codeConnectFailed, "target 缺少 ssh")
	}
	prog := sshPath
	if prog == "ssh" {
		resolved, err := exec.LookPath("ssh")
		if err != nil {
			return nil, connectError(codeSSHMissing, "")
		}
		prog = resolved
	}
	logFile, err := os.CreateTemp("", "acornfox-tunnel-*.log")
	if err != nil {
		return nil, connectError(codeConnectFailed, "建不了临时文件："+err.Error())
	}
	logPath := logFile.Name()
	removeLog := func() { _ = os.Remove(logPath) }

	cmd := exec.Command(prog, tunnelArgs(t, localPort, remotePort)...)
	cmd.Stderr = logFile // stdin and stdout: the null device
	cmd.SysProcAttr = detachAttr()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		removeLog()
		return nil, connectError(codeConnectFailed, err.Error())
	}
	_ = logFile.Close() // ssh has its own descriptor

	exited := make(chan int, 1)
	go func() {
		_ = cmd.Wait()
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		exited <- code
	}()
	stop := func() {
		_ = cmd.Process.Kill()
		removeLog()
	}

	local := "127.0.0.1:" + strconv.Itoa(localPort)
	deadline := time.Now().Add(tunnelConnectTimeout)
	ticker := time.NewTicker(tunnelPollInterval)
	defer ticker.Stop()
	for {
		if c, err := net.DialTimeout("tcp", local, tunnelPollInterval); err == nil {
			_ = c.Close()
			removeLog()
			return &DetachedTunnel{LocalURL: "http://" + local, PID: cmd.Process.Pid}, nil
		}
		select {
		case code := <-exited:
			b, _ := os.ReadFile(logPath)
			removeLog()
			if len(b) > stderrCap {
				b = b[:stderrCap]
			}
			return nil, connectError(classifySSH(code, string(b)), string(b))
		case <-ctx.Done():
			stop()
			return nil, connectError(codeConnectFailed, ctx.Err().Error())
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			stop()
			return nil, connectError(codeConnectFailed, "隧道端口在 15 秒内未就绪")
		}
	}
}

// TunnelAlive reports whether the detached tunnel pid, forwarding local
// port port, is still up: the process exists and the port accepts a
// connection. Both are checked so a PID the system reused for something
// else is not taken for the tunnel.
func TunnelAlive(pid, port int) bool {
	if pid <= 0 || port <= 0 || !processAlive(pid) {
		return false
	}
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// StopProcess ends process pid (a detached tunnel). A process that is gone
// already is not an error.
func StopProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := p.Kill(); err != nil && processAlive(pid) {
		return err
	}
	return nil
}

// OpenDetachedConsoleTunnel is OpenDetachedTunnel to the server's console
// listener (the port OpenTunnel forwards to).
func OpenDetachedConsoleTunnel(ctx context.Context, t Target, localPort int) (*DetachedTunnel, error) {
	return OpenDetachedTunnel(ctx, t, localPort, consoleRemotePort)
}
