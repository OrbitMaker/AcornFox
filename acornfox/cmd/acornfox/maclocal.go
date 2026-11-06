package main

// The Mac build of this release is a command-line client only (see
// internal/setup/maclocal.go, which also says how to turn the local mode back
// on). The commands that would set up or check the Mac's own runtime stop
// here with fault SETUP-MAC-01; the client commands that talk to a server
// (target, deploy, status, logs ...) are not affected.

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/setup"
)

// macLocalCommands are the commands stopped on a Mac: install puts the
// service on this Mac and prepares its runtime, serve is that service, doctor
// checks it. uninstall stays usable to clean up an earlier test install.
var macLocalCommands = map[string]bool{"install": true, "serve": true, "doctor": true}

// macLocalExit is the exit code of a stopped command; it matches install's
// "use install.sh" answer on Linux (wrong place for this command).
const macLocalExit = 2

// macLocalStop handles a Mac local command when the local mode is off. It
// returns false (and prints nothing) when the command may run. asJSON
// selects the stable JSON shape:
//
//	install, serve: {"ok":false,"diagnosis":{"stage":"local","code":"SETUP-MAC-01","message":...,"hint":...}}
//	                (the same shape as every client command's failure)
//	doctor:         {"ok":false,"items":[{"ok":false,"level":"fail","text":...,"code":"SETUP-MAC-01"}]}
//	                (doctor's own report shape)
func macLocalStop(cmd string, asJSON bool, stdout, stderr io.Writer) (int, bool) {
	if !setup.MacLocalBlocked() || !macLocalCommands[cmd] {
		return 0, false
	}
	if cmd == "doctor" {
		r := doctorReport{OK: true}
		r.add(false, setup.MacLocalCode, "%s。%s", setup.MacLocalMessage, setup.MacLocalHint)
		if asJSON {
			b, _ := json.Marshal(r)
			fmt.Fprintln(stdout, string(b))
		} else {
			r.print(stdout)
		}
		return 1, true
	}
	if asJSON {
		b, _ := json.Marshal(map[string]any{"ok": false, "diagnosis": client.Diagnosis{
			Stage: "local", Code: setup.MacLocalCode, Message: setup.MacLocalMessage, Hint: setup.MacLocalHint,
		}})
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintln(stderr, setup.MacLocalMessage)
		fmt.Fprintln(stderr, "故障编号 "+setup.MacLocalCode)
		fmt.Fprintln(stderr, "怎么办 "+setup.MacLocalHint)
	}
	return macLocalExit, true
}

// hasJSONFlag reports whether args ask for JSON output.
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		switch a {
		case "--json", "-json", "--json=true", "-json=true":
			return true
		}
	}
	return false
}

// leadingCommand returns the first argument after leading --json flags, so
// "acornfox --json install" is stopped like "acornfox install --json".
func leadingCommand(args []string) string {
	for _, a := range args {
		switch a {
		case "--json", "-json", "--json=true", "-json=true":
			continue
		}
		return a
	}
	return ""
}
