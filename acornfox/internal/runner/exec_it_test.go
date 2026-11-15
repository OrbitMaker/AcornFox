package runner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// See the note at the top of docker_it_test.go about where these tests may run.
//
// TestDockerExec runs commands in a real container: stdin and env reach the
// command, the argument vector is not read by a shell, the user is taken,
// output is bounded, a time-out is reported honestly, secrets passed in env
// and stdin are not in any host process's argument list, and a container
// that is not running is refused. Guarded by ACORNFOX_DOCKER_IT=1 like the
// other Docker integration tests.
func TestDockerExec(t *testing.T) {
	d := newDockerIT(t)
	ctx := context.Background()
	// Random names: the dev box is shared, and two runs of this test at the
	// same time must not reuse, stop or remove each other's container.
	app := "afitexec-" + itRandHex(t, 3)
	dep := itRandHex(t, 6)
	otherDep := itRandHex(t, 6)
	for otherDep == dep {
		otherDep = itRandHex(t, 6)
	}
	name := ContainerName(app, dep)
	tag := ImageTag(app, dep)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = d.RemoveContainer(cctx, app, name)
		_ = d.RemoveImage(cctx, app, tag)
		_, _ = d.c().NetworkRemove(cctx, NetworkName(app), client.NetworkRemoveOptions{})
	})
	pull, err := d.PullImage(ctx, app, dep, itBaseImage)
	if err != nil || !pull.OK {
		t.Fatalf("PullImage: %v %+v", err, pull.Failure)
	}
	if _, err := d.EnsureContainer(ctx, EnsureContainerRequest{
		App: app, DeploymentID: dep, Image: tag, Port: 8080, MemoryMB: 64, CPUMilli: 500,
		Command: []string{"sleep", "3600"},
	}); err != nil {
		t.Fatalf("EnsureContainer: %v", err)
	}
	run := func(label string, req ExecRequest) ExecResult {
		t.Helper()
		req.App, req.Name = app, name
		res, err := d.Exec(ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		return res
	}

	// Stdin, env and the exit code; the same through the peer socket.
	secret := "it-env-" + dep + "-value"
	req := ExecRequest{
		Command: []string{"sh", "-c", `cat; printf '%s' "$AF_IT_SECRET" >&2; exit 3`},
		Env:     map[string]string{"AF_IT_SECRET": secret},
		Stdin:   []byte("hello\nworld <&>"),
	}
	res := run("stdin+env", req)
	if res.ExitCode != 3 || string(res.Stdout) != "hello\nworld <&>" || string(res.Stderr) != secret || res.Truncated || res.TimedOut {
		t.Fatalf("stdin+env: %+v stdout=%q stderr=%q", res, res.Stdout, res.Stderr)
	}
	cli := startTestServer(t, NewServer(d, t.TempDir()))
	req.App, req.Name = app, name
	viaSocket, err := cli.Exec(ctx, req)
	if err != nil || viaSocket.ExitCode != 3 || string(viaSocket.Stdout) != "hello\nworld <&>" {
		t.Fatalf("via socket: %+v %v", viaSocket, err)
	}

	// The vector is not read by a shell.
	res = run("no shell", ExecRequest{Command: []string{"echo", "$HOME; id", "`x`"}})
	if res.ExitCode != 0 || string(res.Stdout) != "$HOME; id `x`\n" {
		t.Fatalf("no shell: %q", res.Stdout)
	}

	// The user.
	res = run("user", ExecRequest{Command: []string{"id", "-u"}, User: "nobody"})
	if res.ExitCode != 0 || strings.TrimSpace(string(res.Stdout)) != "65534" {
		t.Fatalf("user: %+v %q", res, res.Stdout)
	}

	// Output bounds: the head of stdout, the tail of stderr.
	res = run("big stdout", ExecRequest{Command: []string{"sh", "-c", "printf A; head -c 100000 /dev/zero"}})
	if res.ExitCode != 0 || len(res.Stdout) != MaxExecOutputBytes || res.Stdout[0] != 'A' || !res.StdoutTruncated || !res.Truncated {
		t.Fatalf("big stdout: %d bytes, %+v", len(res.Stdout), res.StdoutTruncated)
	}
	res = run("big stderr", ExecRequest{Command: []string{"sh", "-c", "head -c 100000 /dev/zero >&2; printf Z >&2"}})
	if len(res.Stderr) != MaxExecOutputBytes || res.Stderr[len(res.Stderr)-1] != 'Z' || !res.StderrTruncated {
		t.Fatalf("big stderr: %d bytes, %+v", len(res.Stderr), res.StderrTruncated)
	}

	// Time-out: reported, and the process is honestly said to be maybe running.
	start := time.Now()
	res = run("timeout", ExecRequest{Command: []string{"sleep", "5"}, TimeoutS: 1})
	if took := time.Since(start); !res.TimedOut || res.ExitCode != -1 || took < time.Second || took > 4*time.Second || !strings.Contains(res.Message, "可能还在") {
		t.Fatalf("timeout: %+v after %v", res, took)
	}

	// Secrets in env and stdin are in no host process's argument list.
	envSecret := "it-argv-env-" + dep
	stdinSecret := "it-argv-stdin-" + dep
	done := make(chan ExecResult, 1)
	go func() {
		r, _ := d.Exec(ctx, ExecRequest{App: app, Name: name, Command: []string{"sh", "-c", "cat >/dev/null; sleep 2"},
			Env: map[string]string{"AF_IT_TOKEN": envSecret}, Stdin: []byte(stdinSecret)})
		done <- r
	}()
	time.Sleep(700 * time.Millisecond)
	if pid, ok := cmdlineContaining(t, envSecret, stdinSecret); ok {
		t.Fatalf("a secret is in the argument list of process %s", pid)
	}
	if r := <-done; r.ExitCode != 0 || r.TimedOut {
		t.Fatalf("argv probe exec: %+v", r)
	}

	// A program that does not exist.
	res, err = d.Exec(ctx, ExecRequest{App: app, Name: name, Command: []string{"no-such-program-afit"}})
	var re *RemoteError
	switch {
	case err == nil && res.ExitCode != 0:
		t.Logf("missing program: exit %d, stdout %q, stderr %q", res.ExitCode, res.Stdout, res.Stderr)
	case errors.As(err, &re) && re.Code == CodeExecFailed:
		t.Logf("missing program: %v", err)
	default:
		t.Fatalf("missing program: %+v %v", res, err)
	}

	// Another deployment of the app, and a stopped container.
	if _, err := d.Exec(ctx, ExecRequest{App: app, Name: ContainerName(app, otherDep), Command: []string{"true"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing container: %v", err)
	}
	if err := d.StopContainer(ctx, app, name); err != nil {
		t.Fatalf("StopContainer: %v", err)
	}
	if _, err := d.Exec(ctx, ExecRequest{App: app, Name: name, Command: []string{"true"}}); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped: %v", err)
	}
}

// itRandHex is n random bytes as lowercase hex (2n characters).
func itRandHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// cmdlineContaining reports a host process whose argument list contains one
// of needles.
func cmdlineContaining(t *testing.T, needles ...string) (string, bool) {
	t.Helper()
	paths, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil || len(paths) == 0 {
		t.Skipf("cannot read /proc: %v", err)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, n := range needles {
			if bytes.Contains(b, []byte(n)) {
				return filepath.Base(filepath.Dir(p)), true
			}
		}
	}
	return "", false
}
