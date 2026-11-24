package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Autostart name. The macOS LaunchAgent label is fixed so install, upgrade and
// uninstall always find the same entry. (Windows has no autostart entry of our
// own: AcornFox runs inside WSL and install-windows.ps1 starts WSL.)
const LaunchAgentLabel = "com.acornfox.serve"

// ErrAutostartLinux is returned on Linux, where install.sh writes the systemd
// units for the server and runner.
var ErrAutostartLinux = errors.New("Linux 上由 install.sh 配置 systemd 服务（acornfox-server、acornfox-runner）")

// RunCommand runs an external command and returns its combined output; tests
// override it.
var RunCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// AutostartEntry describes what starts at login: binary + args, with output
// sent to logFile.
type AutostartEntry struct {
	Binary  string
	Args    []string
	LogFile string
}

// InstallAutostart registers entry to start at login and starts it now. It is
// idempotent: an existing entry is replaced (and the running process
// restarted), which is also how upgrades switch to a new binary.
func InstallAutostart(e AutostartEntry) error {
	switch GOOS {
	case "darwin":
		return installLaunchAgent(e)
	default:
		return ErrAutostartLinux
	}
}

// RemoveAutostart stops the running process and removes the login entry.
// Missing is success.
func RemoveAutostart() error {
	switch GOOS {
	case "darwin":
		_, _ = RunCommand("launchctl", "bootout", launchDomain()+"/"+LaunchAgentLabel)
		p, err := LaunchAgentPath()
		if err != nil {
			return err
		}
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	default:
		return ErrAutostartLinux
	}
}

// RestartAutostart restarts the running process through the autostart entry.
func RestartAutostart() error {
	switch GOOS {
	case "darwin":
		out, err := RunCommand("launchctl", "kickstart", "-k", launchDomain()+"/"+LaunchAgentLabel)
		if err != nil {
			return fmt.Errorf("重启 LaunchAgent 失败：%s", strings.TrimSpace(string(out)))
		}
		return nil
	default:
		return ErrAutostartLinux
	}
}

// AutostartInstalled reports whether the login entry exists.
func AutostartInstalled() bool {
	switch GOOS {
	case "darwin":
		p, err := LaunchAgentPath()
		if err != nil {
			return false
		}
		_, err = os.Stat(p)
		return err == nil
	default:
		return false
	}
}

// LaunchAgentPath is ~/Library/LaunchAgents/com.acornfox.serve.plist.
func LaunchAgentPath() (string, error) {
	home, err := UserHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist"), nil
}

func launchDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// LaunchAgentPlist renders the per-user LaunchAgent. KeepAlive restarts the
// process after a crash but not after a clean exit; PATH includes the usual
// Homebrew and Docker locations so git and docker credential helpers resolve.
func LaunchAgentPlist(e AutostartEntry) string {
	var args strings.Builder
	for _, a := range append([]string{e.Binary}, e.Args...) {
		fmt.Fprintf(&args, "\t\t<string>%s</string>\n", xmlEscape(a))
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + LaunchAgentLabel + `</string>
	<key>ProgramArguments</key>
	<array>
` + args.String() + `	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>10</integer>
	<key>ProcessType</key>
	<string>Background</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin:/Applications/Docker.app/Contents/Resources/bin</string>
	</dict>
	<key>StandardOutPath</key>
	<string>` + xmlEscape(e.LogFile) + `</string>
	<key>StandardErrorPath</key>
	<string>` + xmlEscape(e.LogFile) + `</string>
</dict>
</plist>
`
}

func installLaunchAgent(e AutostartEntry) error {
	p, err := LaunchAgentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.LogFile), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(LaunchAgentPlist(e)), 0o644); err != nil {
		return err
	}
	// bootout first so a changed plist (new binary path, upgrade) takes effect.
	_, _ = RunCommand("launchctl", "bootout", launchDomain()+"/"+LaunchAgentLabel)
	var out []byte
	for i := 0; i < 3; i++ {
		// bootout is asynchronous; bootstrap can fail with "Input/output error"
		// for a moment while the old job is still being torn down.
		if out, err = RunCommand("launchctl", "bootstrap", launchDomain(), p); err == nil {
			return nil
		}
		sleep(1)
	}
	return fmt.Errorf("注册 LaunchAgent 失败：%s", strings.TrimSpace(string(out)))
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
