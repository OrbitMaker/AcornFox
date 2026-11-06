// Command acornfox is the AcornFox client and server binary. Client commands
// (deploy, status, env, ...) run in the internal/cli package; server-side
// subcommands (server, runner, proxy) are handled here in package main.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/acornfox/acornfox/internal/cli"
	"github.com/acornfox/acornfox/internal/client"
)

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

// run dispatches the server-side subcommands to their handlers and everything
// else to the CLI command layer.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	// Mac: the local mode is off in this release (maclocal.go).
	if code, stopped := macLocalStop(leadingCommand(args), hasJSONFlag(args), stdout, stderr); stopped {
		return code
	}
	if len(args) > 0 {
		switch args[0] {
		case "server":
			return runServer(args[1:], stdout, stderr)
		case "serve":
			return runServe(args[1:], stdout, stderr)
		case "install":
			return runInstall(args[1:], stdout, stderr)
		case "uninstall":
			return runUninstall(args[1:], stdin, stdout, stderr)
		case "doctor":
			return runDoctor(args[1:], stdout, stderr)
		case "runner":
			return runRunner(args[1:]...)
		case "proxy":
			return runProxy(args[1:], stdin, stdout, stderr)
		case "version":
			return runVersion(args[1:], stdout)
		case "init":
			return runInit(args[1:], stdout, stderr)
		case "migrate":
			return runMigrate(args[1:], stdout, stderr)
		case "admin-token":
			return runAdminToken(args[1:], stdout, stderr)
		case "identity":
			return runIdentity(args[1:], stdout, stderr)
		}
	}
	// The default connector wraps client.Connect so internal/cli stays
	// decoupled from the transport implementation.
	connect := func(ctx context.Context, t client.Target) (client.API, error) {
		return client.Connect(ctx, t)
	}
	ctx, stop := signal.NotifyContext(context.Background(), cliStopSignals(signal.Ignored)...)
	defer stop()
	return cli.MainWithConnector(ctx, args, stdin, stdout, stderr, os.Getenv, connect, "", mustGetwd())
}

// cliStopSignals lists the signals that cancel a client command so it can
// still print its result (in --json mode a diagnosis object) before exiting.
// SIGHUP (terminal closed, SSH session dropped) is included only when it is not
// ignored: under nohup it stays ignored and the command keeps running, because
// registering it would undo nohup.
func cliStopSignals(ignored func(os.Signal) bool) []os.Signal {
	sigs := []os.Signal{os.Interrupt, syscall.SIGTERM}
	if !ignored(syscall.SIGHUP) {
		sigs = append(sigs, syscall.SIGHUP)
	}
	return sigs
}

func mustGetwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}
