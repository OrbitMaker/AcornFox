// Command acornfox is the AcornFox client and server binary. Client commands
// (deploy, status, env, ...) run in the internal/cli package; server-side
// subcommands (server, runner, proxy) are handled here in package main.
package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	lead := leadingCommand(args)
	if lead == "install" && isCatalogInstall(args) {
		lead = "" // installing an app from the catalog on a server is a client command
	}
	if code, stopped := macLocalStop(lead, hasJSONFlag(args), stdout, stderr); stopped {
		return code
	}
	// "install <app>" goes to internal/cli below; plain "install" installs
	// AcornFox on this computer. A --json written before a command handled
	// here is passed to that command, so "acornfox --json doctor" is
	// "acornfox doctor --json".
	cmd, cmdArgs := mainCommandArgs(args)
	if cmd != "" && !(cmd == "install" && isCatalogInstall(args)) && !(cmd == "version" && hasJSONFlag(args)) {
		if noJSONCommands[cmd] && hasJSONFlag(args) {
			return noJSONOutput(cmd, stdout)
		}
		switch cmd {
		case "server":
			return runServer(cmdArgs, stdout, stderr)
		case "serve":
			return runServe(cmdArgs, stdout, stderr)
		case "install":
			return runInstall(cmdArgs, stdout, stderr)
		case "uninstall":
			return runUninstall(cmdArgs, stdin, stdout, stderr)
		case "doctor":
			return runDoctor(cmdArgs, stdout, stderr)
		case "runner":
			return runRunner(cmdArgs...)
		case "proxy":
			return runProxy(cmdArgs, stdin, stdout, stderr)
		case "version":
			return runVersion(cmdArgs, stdout)
		case "init":
			return runInit(cmdArgs, stdout, stderr)
		case "migrate":
			return runMigrate(cmdArgs, stdout, stderr)
		case "admin-token":
			return runAdminToken(cmdArgs, stdout, stderr)
		case "identity":
			return runIdentity(cmdArgs, stdout, stderr)
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

// mainCommands are the commands this package handles itself; everything
// else goes to internal/cli.
var mainCommands = map[string]bool{
	"server": true, "serve": true, "install": true, "uninstall": true, "doctor": true, "runner": true,
	"proxy": true, "version": true, "init": true, "migrate": true, "admin-token": true, "identity": true,
}

// noJSONCommands are user-facing commands handled here that have no JSON
// output; asked for JSON they say so instead of failing on an unknown flag.
var noJSONCommands = map[string]bool{"install": true, "uninstall": true}

// mainCommandArgs returns the command handled in this package and its
// arguments, or "" when the command belongs to internal/cli. --json flags
// written before the command move to the end of its arguments.
func mainCommandArgs(args []string) (string, []string) {
	cmd := leadingCommand(args)
	if !mainCommands[cmd] {
		return "", nil
	}
	i := 0
	for args[i] != cmd {
		i++
	}
	rest := append([]string{}, args[i+1:]...)
	if i > 0 && !hasJSONFlag(rest) {
		rest = append(rest, "--json")
	}
	return cmd, rest
}

// noJSONOutput answers a JSON request for a command without JSON output.
func noJSONOutput(cmd string, stdout io.Writer) int {
	msg := cmd + " 没有 JSON 输出，去掉 --json 再运行"
	hint := "这个命令要在终端里看着运行"
	if cmd == "install" {
		msg = "install（在这台电脑上安装 AcornFox）没有 JSON 输出，去掉 --json 再运行"
		hint = "Linux 服务器用 install.sh 安装；从应用中心装应用写 acornfox --json install 应用名"
	}
	if cmd == "uninstall" {
		hint = "Linux 服务器用 uninstall.sh 卸载（没有终端时加 -y）"
	}
	b, _ := json.Marshal(map[string]any{"ok": false, "diagnosis": client.Diagnosis{Stage: "usage", Code: "usage", Message: msg, Hint: hint}})
	fmt.Fprintln(stdout, string(b))
	return 2
}

func mustGetwd() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// isCatalogInstall reports whether an "install" command line installs an app
// from the catalog (`install <app>`, `install --recipe …`, `install status …`,
// `--set`, `--llm`…) rather than installing AcornFox itself on this computer
// (`install [--china|--global|--no-setup]`, no positional
// argument). Asking for help (`install -h`) goes to the catalog install too:
// installing apps is what users look for, and its usage ends with a line on
// installing AcornFox itself.
func isCatalogInstall(args []string) bool {
	seen := false
	local := false
	for _, a := range args {
		if !seen {
			if a == "install" {
				seen = true
			}
			continue
		}
		switch {
		case a == "-h", a == "--help", a == "-help":
			return true
		case a == "--china", a == "-china", a == "--global", a == "-global", a == "--no-setup", a == "-no-setup",
			a == "--rollback", a == "-rollback", a == "--json", a == "-json":
			continue
		}
		local = true
	}
	return local
}
