package platform

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Autostart names. The macOS LaunchAgent label and the Windows task name are
// fixed so install, upgrade and uninstall always find the same entry.
const (
	LaunchAgentLabel = "com.acornfox.serve"
	WindowsTaskName  = "AcornFox"
)

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
	case "windows":
		return installTask(e)
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
	case "windows":
		_, _ = RunCommand("schtasks", "/End", "/TN", WindowsTaskName)
		out, err := RunCommand("schtasks", "/Delete", "/TN", WindowsTaskName, "/F")
		if err != nil && !strings.Contains(strings.ToLower(string(out)), "cannot find") && !strings.Contains(string(out), "找不到") {
			// schtasks returns an error for a missing task; treat that as done.
			if _, qerr := RunCommand("schtasks", "/Query", "/TN", WindowsTaskName); qerr == nil {
				return fmt.Errorf("删除计划任务失败：%s", strings.TrimSpace(string(out)))
			}
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
	case "windows":
		_, _ = RunCommand("schtasks", "/End", "/TN", WindowsTaskName)
		out, err := RunCommand("schtasks", "/Run", "/TN", WindowsTaskName)
		if err != nil {
			return fmt.Errorf("启动计划任务失败：%s", strings.TrimSpace(string(out)))
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
	case "windows":
		_, err := RunCommand("schtasks", "/Query", "/TN", WindowsTaskName)
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

// WindowsTaskXML renders the logon task. It runs as the current user with
// limited rights, never times out (the schtasks default stops tasks after 72
// hours), restarts after a crash, and keeps running on battery.
func WindowsTaskXML(e AutostartEntry, user string) string {
	quoted := make([]string, 0, len(e.Args))
	for _, a := range e.Args {
		if strings.ContainsAny(a, " \t\"") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		quoted = append(quoted, a)
	}
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>AcornFox：登录后在后台运行，管理本机 Docker 里的应用</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>` + xmlEscape(user) + `</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>` + xmlEscape(user) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowHardTerminate>true</AllowHardTerminate>
    <StartWhenAvailable>true</StartWhenAvailable>
    <IdleSettings>
      <StopOnIdleEnd>false</StopOnIdleEnd>
      <RestartOnIdle>false</RestartOnIdle>
    </IdleSettings>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + xmlEscape(e.Binary) + `</Command>
      <Arguments>` + xmlEscape(strings.Join(quoted, " ")) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// currentUser is os/user.Current; tests override it.
var currentUser = osuser.Current

// windowsTaskUser names the account the logon task runs as. The SID works on
// every machine; DOMAIN\name does not when USERDOMAIN is "WORKGROUP" (an SSH
// session on a workgroup machine: schtasks then fails with "No mapping
// between account names and security IDs").
func windowsTaskUser() (string, error) {
	if u, err := currentUser(); err == nil && strings.HasPrefix(u.Uid, "S-1-") {
		return u.Uid, nil
	}
	name := Env("USERNAME")
	if name == "" {
		return "", errors.New("取不到当前 Windows 用户名（USERNAME）")
	}
	domain := Env("USERDOMAIN")
	if domain == "" || strings.EqualFold(domain, "WORKGROUP") {
		domain = Env("COMPUTERNAME")
	}
	if domain == "" {
		return name, nil
	}
	return domain + `\` + name, nil
}

func installTask(e AutostartEntry) error {
	user, err := windowsTaskUser()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.LogFile), 0o700); err != nil {
		return err
	}
	xmlPath := filepath.Join(filepath.Dir(e.LogFile), "acornfox-task.xml")
	if err := os.WriteFile(xmlPath, utf16File(WindowsTaskXML(e, user)), 0o600); err != nil {
		return err
	}
	defer os.Remove(xmlPath)
	_, _ = RunCommand("schtasks", "/End", "/TN", WindowsTaskName)
	if out, err := RunCommand("schtasks", "/Create", "/TN", WindowsTaskName, "/XML", xmlPath, "/F"); err != nil {
		return fmt.Errorf("创建计划任务失败：%s", strings.TrimSpace(string(out)))
	}
	if out, err := RunCommand("schtasks", "/Run", "/TN", WindowsTaskName); err != nil {
		return fmt.Errorf("启动计划任务失败：%s", strings.TrimSpace(string(out)))
	}
	return nil
}

// utf16File encodes s as UTF-16LE with a byte order mark, which is what
// schtasks /XML expects.
func utf16File(s string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xFE})
	for _, u := range utf16.Encode([]rune(strings.ReplaceAll(s, "\n", "\r\n"))) {
		_ = binary.Write(&buf, binary.LittleEndian, u)
	}
	return buf.Bytes()
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
