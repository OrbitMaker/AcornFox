package client

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// A detached tunnel comes up, is reported alive while it forwards its port,
// and is gone after StopProcess; what ssh wrote is not left behind.
func TestOpenDetachedTunnel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-ssh via re-exec assumes unix-style process control")
	}
	withFakeSSH(t)
	t.Setenv("FAKE_SSH_MODE", "listen")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	port := freeTCPPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dt, err := OpenDetachedConsoleTunnel(ctx, Target{SSH: "devbox"}, port)
	if err != nil {
		t.Fatalf("OpenDetachedTunnel: %v", err)
	}
	t.Cleanup(func() { _ = StopProcess(dt.PID) })
	if dt.PID <= 0 || dt.LocalURL != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Fatalf("tunnel %+v", dt)
	}
	if !TunnelAlive(dt.PID, port) {
		t.Fatal("not alive right after it came up")
	}
	if TunnelAlive(dt.PID, freeTCPPort(t)) {
		t.Fatal("alive on a port it does not forward")
	}
	if logs, _ := filepath.Glob(filepath.Join(tmp, "acornfox-tunnel-*.log")); len(logs) != 0 {
		t.Fatalf("ssh output left behind: %v", logs)
	}
	if err := StopProcess(dt.PID); err != nil {
		t.Fatalf("StopProcess: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for TunnelAlive(dt.PID, port) {
		if time.Now().After(deadline) {
			t.Fatal("still alive after StopProcess")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := StopProcess(dt.PID); err != nil {
		t.Fatalf("StopProcess of a gone process: %v", err)
	}
}

// ssh failing before the forward is up: the classified connect error, and
// no temporary file left.
func TestOpenDetachedTunnelFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake-ssh via re-exec assumes unix-style process control")
	}
	withFakeSSH(t)
	t.Setenv("FAKE_SSH_MODE", "stderr")
	t.Setenv("FAKE_SSH_STDERR", "ssh: connect to host x port 22: Connection refused")
	t.Setenv("FAKE_SSH_EXIT", "255")
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	_, err := OpenDetachedConsoleTunnel(context.Background(), Target{SSH: "devbox"}, freeTCPPort(t))
	var e *Error
	if !asError(err, &e) || e.Diag.Code != codeHostUnreachable {
		t.Fatalf("want host_unreachable, got %v", err)
	}
	if logs, _ := filepath.Glob(filepath.Join(tmp, "acornfox-tunnel-*.log")); len(logs) != 0 {
		t.Fatalf("ssh output left behind: %v", logs)
	}
	if _, err := OpenDetachedTunnel(context.Background(), Target{SSH: "u@h"}, 40001, 0); err == nil {
		t.Error("port 0 accepted")
	}
	if TunnelAlive(0, 1) || TunnelAlive(os.Getpid(), 0) {
		t.Error("alive without a pid or a port")
	}
}
