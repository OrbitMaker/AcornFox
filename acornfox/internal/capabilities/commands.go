// Package capabilities builds `acornfox capabilities --json`: what this
// program can do, worked out from the program itself and never written by
// hand into a document. It is read by the operations system (to check an app
// manual against the platform that will run it), by the general Skill's own
// gate, and by an AI workbench that wants to know what the platform offers
// before it writes a command.
//
// The parts, and where each one comes from:
//
//	commands           the command table in this file. The gate test
//	                   (cmd/acornfox/capabilities_gate_test.go) reads the
//	                   source of both dispatch tables (internal/cli/cli.go
//	                   and cmd/acornfox/main.go) and the subcommand switches
//	                   of every command, and fails when a command that can be
//	                   run is missing here, or when the flags written here are
//	                   not the ones the command's FlagSet declares.
//	faults             internal/faults.Catalog (every code, sorted)
//	platform_vars      internal/catalog.PlatformVars ("platform.<name>")
//	service_types      the service types of the built-in catalog snapshot
//	manifest_features  internal/catalog.Features
//	skill_patterns     the "<!-- 套路: 名字 -->" markers in the general Skill
//
// Nothing here connects to a server, reads the user's data or changes
// anything.
package capabilities

import "strings"

// SinceNext is the platform version the commands marked as new here first
// ship with: the version the next release carries, the same one the
// manifest features of the reapply work are marked with. A command with no
// "since" in the output was already there before this list existed (0.6.3
// and earlier). When the next release gets another number, change it here
// (a test checks it is not lower than any manifest feature's version).
const SinceNext = "0.7.0"

// Command is one runnable command or subcommand.
type Command struct {
	// Path is the words after "acornfox", e.g. "apps upgrade".
	Path string `json:"path"`
	// Flags are the command's own flags with their dashes ("--plan", "-f"),
	// and "--json" when the command can answer in JSON. The global flags
	// --target and --app (every server command takes them) are not listed.
	Flags []string `json:"flags"`
	// Writes: the command changes something (a server's apps or settings,
	// this computer's files). Read-only commands and trial runs (--plan)
	// still say true when the command can write at all: Flags says how to
	// make it a trial run.
	Writes bool `json:"writes"`
	// Since is the version it first appeared in, only for commands added
	// after 0.6.3.
	Since string `json:"since,omitempty"`
	// Local: it runs on this computer and does not connect to a server.
	Local bool `json:"local,omitempty"`
	// Internal: part of how AcornFox starts and installs itself; nobody
	// types it in a day-to-day task.
	Internal bool `json:"internal,omitempty"`
	// Aliases are other spellings of the last word of Path that the command
	// takes ("services rm" for "services remove").
	Aliases []string `json:"aliases,omitempty"`
}

// c builds one table row: path, flags as one space-separated string, writes,
// then options: new, local, internal, alias:a,b.
func c(path, flags string, writes bool, opts ...string) Command {
	cmd := Command{Path: path, Flags: []string{}, Writes: writes}
	if flags != "" {
		cmd.Flags = strings.Fields(flags)
	}
	for _, o := range opts {
		switch o {
		case "new":
			cmd.Since = SinceNext
		case "local":
			cmd.Local = true
		case "internal":
			cmd.Internal = true
		default:
			if a, ok := strings.CutPrefix(o, "alias:"); ok {
				cmd.Aliases = append(cmd.Aliases, strings.Split(a, ",")...)
				continue
			}
			panic("capabilities: unknown option " + o)
		}
	}
	return cmd
}

// Commands is the table, in the order of `acornfox help`.
//
// To add a command: add its row here (the gate test fails until you do),
// register it in docs/capabilities.md and write its --help in Chinese. Mark
// it "new" while it is only in the version being developed.
var Commands = []Command{
	// This computer.
	c("version", "--edge --json", false, "local"),
	c("doctor", "--json", false, "local"),
	c("identity show", "--data-dir --json", false, "local"),
	c("identity reset", "--data-dir --yes --apply --json", true, "local"),
	c("faults", "--group --markdown --json", false, "local"),
	c("sources", "--probe --image --daemon-mirrors --json", false, "local"),
	c("capabilities", "--json", false, "local", "new"),
	// Mac: the whole Linux edition in a virtual machine (internal/macvm).
	c("mac install", "--plan --json --yes --china --global --no-open --no-lan --memory --cpus --idle --public-host --version --release-base --local-package --gateway --install-dir", true, "local", "new"),
	c("mac upgrade", "--plan --json --yes --china --global --version --release-base --local-package --gateway", true, "local", "new"),
	c("mac uninstall", "--plan --json --yes -y --purge", true, "local", "new"),
	c("mac status", "--json", false, "local", "new"),
	c("mac doctor", "--wake --json", false, "local", "new"),
	c("mac start", "--json", true, "local", "new"),
	c("mac stop", "--json", true, "local", "new"),
	c("mac set", "--memory --cpus --idle --public-host --lan --json", true, "local", "new"),
	c("mac daemon", "", true, "local", "internal", "new"),
	c("mac pack-base", "--json", true, "local", "internal", "new"),
	c("install", "--china --global --no-setup --rollback --set --recipe --catalog --name --version --no-wait --timeout --show-secrets --keep-data-on-failure --reuse-data --plan --expose --use --json", true),
	c("uninstall", "--purge --yes", true, "local"),

	// Servers this computer manages.
	c("target add", "--ssh --port --identity --url --ssh-option --json", true, "local"),
	c("target list", "--json", false, "local"),
	c("target remove", "--json", true, "local"),
	c("target use", "--json", true, "local"),
	c("open", "--no-browser --port --detach --stop --json", false),
	c("lan-console status", "--json", false),
	c("lan-console enable", "--reset-password --json", true),
	c("lan-console disable", "--json", true),
	c("lan-console reset-password", "--json", true),
	c("lan-console devices", "--json", false),
	c("lan-console forget", "--all --json", true),
	c("public-console enable", "--local --wait --reset-password --json", true),
	c("public-console disable", "--local --json", true),
	c("public-console status", "--local --json", false),
	c("setup", "--runtime --mirror --resume --pause --status --json", true),

	// Deploying a project.
	c("deploy", "--image --git --ref --port --health-path --no-wait --timeout --json", true),
	c("redeploy", "--no-wait --timeout --json", true),
	c("rollback", "--to --cancel-pending --no-wait --timeout --json", true),
	c("versions", "--limit --json", false),
	c("status", "--json", false),
	c("apps", "--json", false),
	c("diagnose", "--json", false),

	// Running apps.
	c("stop", "--no-wait --json", true),
	c("start", "--no-wait --json", true),
	c("restart", "--no-wait --json", true),
	c("delete", "--volumes --json", true),
	c("logs", "--tail --follow -f --since --version --build --json", false),
	c("stats", "--history --json", false),
	c("limits", "--memory --cpu --json", true),

	// Settings.
	c("env set", "--secret --json", true),
	c("env unset", "--json", true),
	c("env list", "--json", false),
	c("volume add", "--json", true),
	c("volume list", "--json", false),
	c("app set", "--memory --cpu --port --health-path --expose --yes --no-reapply --json", true),
	c("domain add", "--yes --no-reapply --json", true),
	c("domain remove", "--yes --no-reapply --json", true),
	c("domain list", "--json", false),
	c("domain base", "--plan --yes --json", true),
	c("add", "--json", true),
	c("remove", "--volumes --json", true),
	c("addons", "--json", false),

	// App center.
	c("catalog list", "--json", false),
	c("catalog info", "--catalog --json", false),
	c("catalog show", "--recipe --json", false, "new"),
	c("catalog skill", "--recipe --json", false, "new"),
	c("catalog sync", "--json", true),
	c("catalog connect", "--json", true),
	c("catalog sources", "--json", false),
	c("catalog platform-vars", "--offline --json", false),
	c("catalog lint", "--json", false, "local"),
	c("catalog vars", "--json", false, "local"),
	c("catalog gaps", "--json", false, "local"),
	c("catalog build", "--out --serial --key --prev --catalog --apply --json", true, "local"),
	c("catalog keygen", "--out --json", true, "local"),
	c("catalog verify", "--pubkey --json", false, "local"),
	c("catalog show-sign", "--key --index --apply --json", true, "local"),
	c("install status", "--show-secrets --wait --timeout --json", false),
	c("apps vars", "--reveal --json", false),
	c("apps set", "--plan --yes --force --no-wait --reset --timeout --json", true, "new"),
	c("apps reapply", "--plan --yes --force --no-wait --all --var --timeout --json", true, "new"),
	c("apps affected", "--var --json", false, "new"),
	c("apps outdated", "--json", false, "new"),
	c("apps upgrade", "--plan --yes --no-wait --to --set --timeout --json", true, "new"),
	c("apps rollback", "--plan --yes --no-wait --timeout --json", true, "new"),
	c("services kinds", "--json", false),
	c("services list", "--json", false, "alias:ls"),
	c("services show", "--json", false),
	c("services add", "--name --set --json", true),
	c("services set", "--set --json", true),
	c("services test", "--json", false),
	c("services remove", "--yes --json", true, "alias:rm"),

	// Backups.
	c("backup list", "--json", false, "new", "alias:ls"),
	c("backup create", "--note --plan --no-wait --timeout --json", true, "new", "alias:new"),
	c("backup show", "--verify --json", false, "new"),
	c("backup restore", "--plan --yes --allow-current-version --no-wait --timeout --json", true, "new"),
	c("backup rm", "--yes --json", true, "new", "alias:remove,delete"),
	c("backup task", "--json", false, "new"),

	// AI workbench.
	c("skill install", "--dir --json", true),
	c("skill print", "--all --file", false),
	c("skill refresh", "--dir --json", true, "new"),
	c("machine", "--json", false, "new"),

	// How AcornFox starts and installs itself.
	c("server", "--listen --data-dir --listen-group --console-listen --runner-socket --runner-uid --edge --edge-admin-port --public-host --public-port-min --public-port-max --http-port --https-port --https-issuer --https-ca-file --acme-email --public-console-listen --lan-console-port --wsl --default-exposure", true, "local", "internal"),
	c("serve", "--data-dir --console-listen --edge --edge-admin-port --public-port-min --public-port-max --default-exposure --lan-console-port --background", true, "local", "internal"),
	c("runner", "--socket --server-uid --socket-gid --upload-dir", true, "local", "internal"),
	c("proxy", "--socket", false, "local", "internal"),
	c("init", "--data-dir", true, "local", "internal"),
	c("migrate", "--data-dir", true, "local", "internal"),
	c("admin-token", "", true, "local", "internal"),
}

// Find returns the row for path.
func Find(path string) (Command, bool) {
	for _, cmd := range Commands {
		if cmd.Path == path {
			return cmd, true
		}
	}
	return Command{}, false
}
