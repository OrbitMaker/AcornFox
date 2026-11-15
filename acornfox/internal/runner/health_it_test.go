package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// TestDockerInternalHealth runs an app container reached over the app
// network only (Internal: no port published on the host) and checks it the
// two ways a manifest may say for an app that does not answer HTTP: a
// command run in the container (exec) and a TCP connect from this machine
// to the container's address on its app network (tcp, see ProbeAddr). It
// also pins that the container reaches itself by its alias (the app name,
// what self.host is): a provider's check steps connect to self.host, not to
// 127.0.0.1. Guarded by ACORNFOX_DOCKER_IT=1 like the other Docker
// integration tests.
func TestDockerInternalHealth(t *testing.T) {
	d := newDockerIT(t)
	ctx := context.Background()
	// Random names: the dev box is shared.
	app := "afithealth-" + itRandHex(t, 3)
	dep := itRandHex(t, 6)
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
	const port = 5432
	ci, err := d.EnsureContainer(ctx, EnsureContainerRequest{
		App: app, DeploymentID: dep, Image: tag, Port: port, MemoryMB: 64, CPUMilli: 500,
		// A TCP server that is no HTTP server (busybox nc, kept listening).
		Command:  []string{"nc", "-lk", "-p", "5432", "-e", "cat"},
		Internal: true,
	})
	if err != nil {
		t.Fatalf("EnsureContainer: %v", err)
	}
	if ci.HostPort != 0 || ci.IP == "" || ci.App != app {
		t.Fatalf("an internal app publishes no port and has an address on its network: %+v", ci)
	}
	inspect, err := d.c().ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if hc := inspect.Container.HostConfig; hc == nil || len(hc.PortBindings) != 0 {
		t.Fatalf("port bindings: %+v", hc)
	}
	listed, err := d.ListContainers(ctx, app)
	if err != nil || len(listed) != 1 || listed[0].IP != ci.IP || listed[0].HostPort != 0 {
		t.Fatalf("ListContainers: %+v %v", listed, err)
	}

	// tcp: from this machine to the container's address on the app network.
	addr := ProbeAddr(ci, port, true)
	if !strings.HasPrefix(addr, ci.IP+":") {
		t.Fatalf("probe address %q", addr)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := ProbeTCP(ctx, addr)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tcp health check never passed: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err := ProbeTCP(ctx, ProbeAddr(ci, port+1, true)); err == nil {
		t.Fatal("a port nothing listens on must not pass")
	}

	// exec: a command in the container; exit 0 is up.
	if err := ProbeExec(ctx, d, app, name, []string{"nc", "-z", "-w", "2", "127.0.0.1", "5432"}, ""); err != nil {
		t.Fatalf("exec health check: %v", err)
	}
	if err := ProbeExec(ctx, d, app, name, []string{"nc", "-z", "-w", "2", "127.0.0.1", "5433"}, ""); err == nil || !strings.Contains(err.Error(), "退出码") {
		t.Fatalf("exec health check of a closed port: %v", err)
	}
	if err := ProbeExec(ctx, d, app, name, []string{"echo", "  PONG  "}, "PONG"); err != nil {
		t.Fatalf("expected output: %v", err)
	}
	if err := ProbeExec(ctx, d, app, name, []string{"echo", "LOADING"}, "PONG"); err == nil {
		t.Fatal("other output must not pass")
	}
	// The same through the peer socket (what the server uses).
	cli := startTestServer(t, NewServer(d, t.TempDir()))
	if err := ProbeExec(ctx, cli, app, name, []string{"nc", "-z", "-w", "2", "127.0.0.1", "5432"}, ""); err != nil {
		t.Fatalf("exec health check through the socket: %v", err)
	}

	// The container reaches itself by its alias on the app network (the app
	// name, which is self.host): what a provider's check step connects to.
	if err := ProbeExec(ctx, d, app, name, []string{"nc", "-z", "-w", "2", app, "5432"}, ""); err != nil {
		t.Fatalf("the container does not reach itself as %s: %v", app, err)
	}
}
