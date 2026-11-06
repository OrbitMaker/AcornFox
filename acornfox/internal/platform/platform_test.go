package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	osuser "os/user"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

// withEnv swaps the package hooks for one test.
func withEnv(t *testing.T, goos string, env map[string]string, home string) {
	t.Helper()
	oldGOOS, oldEnv, oldHome, oldStat := GOOS, Env, UserHome, StatFile
	GOOS = goos
	Env = func(k string) string { return env[k] }
	UserHome = func() (string, error) { return home, nil }
	StatFile = os.Stat
	t.Cleanup(func() { GOOS, Env, UserHome, StatFile = oldGOOS, oldEnv, oldHome, oldStat })
}

func TestDefaultPaths(t *testing.T) {
	withEnv(t, "linux", nil, "/home/u")
	if p := DefaultPaths(); p.DataDir != "/var/lib/acornfox" || p.APISocket != "/run/acornfox/api.sock" {
		t.Fatalf("linux = %+v", p)
	}
	withEnv(t, "darwin", nil, "/Users/u")
	if p := DefaultPaths(); p.DataDir != "/Users/u/Library/Application Support/AcornFox" || p.APISocket != p.DataDir+"/api.sock" || p.BinDir != p.DataDir+"/bin" {
		t.Fatalf("darwin = %+v", p)
	}
	withEnv(t, "windows", map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`}, `C:\Users\u`)
	if p := DefaultPaths(); !strings.HasSuffix(p.DataDir, "AcornFox") || !strings.HasPrefix(p.DataDir, `C:\Users\u\AppData\Local`) {
		t.Fatalf("windows = %+v", p)
	}
	withEnv(t, "darwin", map[string]string{"ACORNFOX_DATA_DIR": "/tmp/af"}, "/Users/u")
	if p := DefaultPaths(); p.DataDir != "/tmp/af" {
		t.Fatalf("override = %+v", p)
	}
}

func TestDefaultExposure(t *testing.T) {
	withEnv(t, "linux", nil, "/h")
	if DefaultExposure() != "public" || DesktopMode() {
		t.Fatal("linux must default to public")
	}
	withEnv(t, "darwin", nil, "/h")
	if DefaultExposure() != "lan" || !DesktopMode() {
		t.Fatal("macOS must default to lan")
	}
}

func writeContext(t *testing.T, dockerDir, name, host string) {
	t.Helper()
	sum := sha256.Sum256([]byte(name))
	dir := filepath.Join(dockerDir, "contexts", "meta", hex.EncodeToString(sum[:]))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	meta := `{"Name":"` + name + `","Endpoints":{"docker":{"Host":"` + host + `"}}}`
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDockerOrder(t *testing.T) {
	home := t.TempDir()
	dockerDir := filepath.Join(home, ".docker")
	writeContext(t, dockerDir, "colima", "unix://"+home+"/.colima/default/docker.sock")
	if err := os.WriteFile(filepath.Join(dockerDir, "config.json"), []byte(`{"currentContext":"colima"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. DOCKER_HOST wins.
	withEnv(t, "darwin", map[string]string{"DOCKER_HOST": "tcp://10.0.0.1:2375"}, home)
	if ep := ResolveDocker(); ep.Host != "tcp://10.0.0.1:2375" || ep.Source != "DOCKER_HOST" {
		t.Fatalf("DOCKER_HOST: %+v", ep)
	}
	// 2. The current context.
	withEnv(t, "darwin", nil, home)
	if ep := ResolveDocker(); ep.Source != "context colima" || ep.Runtime != "Colima" {
		t.Fatalf("context: %+v", ep)
	}
	// DOCKER_CONTEXT=default falls through to the platform default.
	withEnv(t, "darwin", map[string]string{"DOCKER_CONTEXT": "default"}, home)
	if ep := ResolveDocker(); ep.Source != "default" {
		t.Fatalf("default context: %+v", ep)
	}
}

func TestDarwinDefaultPicksExistingSocket(t *testing.T) {
	home := t.TempDir()
	withEnv(t, "darwin", nil, home)
	orb := filepath.Join(home, ".orbstack", "run", "docker.sock")
	StatFile = func(p string) (os.FileInfo, error) {
		if p == orb {
			return nil, nil
		}
		return nil, errors.New("missing")
	}
	if ep := ResolveDocker(); ep.Host != "unix://"+orb || ep.Runtime != "OrbStack" {
		t.Fatalf("got %+v", ep)
	}
	StatFile = func(string) (os.FileInfo, error) { return nil, errors.New("missing") }
	if ep := ResolveDocker(); ep.Runtime != "Docker Desktop" {
		t.Fatalf("nothing found must point at Docker Desktop: %+v", ep)
	}
}

func TestWindowsAndLinuxDefaults(t *testing.T) {
	withEnv(t, "windows", nil, t.TempDir())
	if ep := ResolveDocker(); ep.Host != "npipe:////./pipe/docker_engine" || ep.Runtime != "Docker Desktop" {
		t.Fatalf("windows: %+v", ep)
	}
	withEnv(t, "linux", nil, t.TempDir())
	if ep := ResolveDocker(); ep.Host != "unix:///var/run/docker.sock" {
		t.Fatalf("linux: %+v", ep)
	}
}

func TestRuntimeFromInfo(t *testing.T) {
	cases := []struct{ os, name, want string }{
		{"Docker Desktop", "docker-desktop", "Docker Desktop"},
		{"OrbStack", "orbstack", "OrbStack"},
		{"Ubuntu 24.04 LTS", "colima", "Colima"},
		{"Ubuntu 24.04 LTS", "host", "Docker"},
	}
	for _, c := range cases {
		if got := RuntimeFromInfo("Docker", c.os, c.name); got != c.want {
			t.Errorf("%q/%q = %q, want %q", c.os, c.name, got, c.want)
		}
	}
}

func TestLaunchAgentPlist(t *testing.T) {
	p := LaunchAgentPlist(AutostartEntry{Binary: "/Users/u/Library/Application Support/AcornFox/bin/acornfox", Args: []string{"serve", "--background"}, LogFile: "/tmp/a&b.log"})
	for _, want := range []string{"<string>com.acornfox.serve</string>", "<string>serve</string>", "<string>--background</string>", "<key>RunAtLoad</key>", "<key>SuccessfulExit</key>", "/tmp/a&amp;b.log"} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q", want)
		}
	}
}

func TestWindowsTaskXML(t *testing.T) {
	x := WindowsTaskXML(AutostartEntry{Binary: `C:\Users\u\AppData\Local\AcornFox\bin\acornfox.exe`, Args: []string{"serve", "--background"}}, `PC\u`)
	for _, want := range []string{"<LogonTrigger>", "<UserId>PC\\u</UserId>", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>", "<Arguments>serve --background</Arguments>", "<RunLevel>LeastPrivilege</RunLevel>", "<RestartOnFailure>"} {
		if !strings.Contains(x, want) {
			t.Errorf("task xml missing %q", want)
		}
	}
	b := utf16File("ab\n")
	if b[0] != 0xFF || b[1] != 0xFE {
		t.Fatal("missing UTF-16LE byte order mark")
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
	}
	if string(utf16.Decode(u)) != "ab\r\n" {
		t.Fatalf("decoded %q", string(utf16.Decode(u)))
	}
}

func TestAutostartLinuxRefused(t *testing.T) {
	withEnv(t, "linux", nil, t.TempDir())
	if err := InstallAutostart(AutostartEntry{}); !errors.Is(err, ErrAutostartLinux) {
		t.Fatalf("got %v", err)
	}
}

func TestInstallLaunchAgentCommands(t *testing.T) {
	home := t.TempDir()
	withEnv(t, "darwin", nil, home)
	var calls []string
	old := RunCommand
	RunCommand = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	t.Cleanup(func() { RunCommand = old })
	e := AutostartEntry{Binary: "/b/acornfox", Args: []string{"serve"}, LogFile: filepath.Join(home, "logs", "a.log")}
	if err := InstallAutostart(e); err != nil {
		t.Fatal(err)
	}
	if !AutostartInstalled() {
		t.Fatal("plist not written")
	}
	if len(calls) != 2 || !strings.HasPrefix(calls[0], "launchctl bootout") || !strings.HasPrefix(calls[1], "launchctl bootstrap") {
		t.Fatalf("calls = %v", calls)
	}
	if err := RemoveAutostart(); err != nil || AutostartInstalled() {
		t.Fatalf("remove: %v", err)
	}
}

func TestNetworkFile(t *testing.T) {
	dir := t.TempDir()
	if n := LoadNetwork(dir); !n.China || n.Source != "default" {
		t.Fatalf("missing file must default to China: %+v", n)
	}
	if err := SaveNetwork(dir, Network{China: false, Source: "flag"}); err != nil {
		t.Fatal(err)
	}
	if n := LoadNetwork(dir); n.China || n.Source != "flag" {
		t.Fatalf("round trip = %+v", n)
	}
	env := filepath.Join(dir, "install.env")
	os.WriteFile(env, []byte("ACORNFOX_IN_CHINA=true\n"), 0o600)
	if c, ok := LinuxInstallChina(env); !c || !ok {
		t.Fatal("install.env true not read")
	}
	os.WriteFile(env, []byte("ACORNFOX_IN_CHINA=false\n"), 0o600)
	if c, ok := LinuxInstallChina(env); c || !ok {
		t.Fatal("install.env false not read")
	}
	if _, ok := LinuxInstallChina(filepath.Join(dir, "missing")); ok {
		t.Fatal("missing file must report !ok")
	}
}

// The logon task runs as the user's SID; without one, a WORKGROUP domain is
// replaced by the computer name (schtasks cannot map WORKGROUP\name).
func TestWindowsTaskUser(t *testing.T) {
	old := currentUser
	t.Cleanup(func() { currentUser = old })
	currentUser = func() (*osuser.User, error) { return &osuser.User{Uid: "S-1-5-21-1-2-3-1001"}, nil }
	withEnv(t, "windows", map[string]string{"USERNAME": "codex", "USERDOMAIN": "WORKGROUP"}, t.TempDir())
	if u, _ := windowsTaskUser(); u != "S-1-5-21-1-2-3-1001" {
		t.Fatalf("got %q", u)
	}
	currentUser = func() (*osuser.User, error) { return nil, errors.New("no user") }
	withEnv(t, "windows", map[string]string{"USERNAME": "codex", "USERDOMAIN": "WORKGROUP", "COMPUTERNAME": "DEVBOX"}, t.TempDir())
	if u, _ := windowsTaskUser(); u != `DEVBOX\codex` {
		t.Fatalf("got %q", u)
	}
	withEnv(t, "windows", map[string]string{"USERNAME": "codex", "USERDOMAIN": "CORP"}, t.TempDir())
	if u, _ := windowsTaskUser(); u != `CORP\codex` {
		t.Fatalf("got %q", u)
	}
}

// VPN, proxy TUN and virtual adapters are not LAN addresses.
func TestLANCandidate(t *testing.T) {
	for _, c := range []struct {
		ip, name string
		want     bool
	}{
		{"192.168.31.20", "Ethernet", true},
		{"192.168.31.20", "en0", true},
		{"172.30.0.2", "Wi-Fi", true},
		{"172.19.0.1", "sing-box-tun", false},
		{"172.19.176.1", "vEthernet (WSL (Hyper-V firewall))", false},
		{"10.8.0.2", "utun3", false},
		{"198.18.0.1", "Meta", false},
		{"100.101.1.2", "eth1", false},
		{"8.8.8.8", "eth0", false},
	} {
		if got := lanCandidate(net.ParseIP(c.ip), c.name); got != c.want {
			t.Errorf("%s on %s: got %v", c.ip, c.name, got)
		}
	}
}

func TestLinuxInstallCloud(t *testing.T) {
	dir := t.TempDir()
	for content, want := range map[string]string{
		"ACORNFOX_IN_CHINA=true\nACORNFOX_CLOUD=tencent\n": "tencent",
		"ACORNFOX_CLOUD=volcengine\n":                      "volcengine",
		"ACORNFOX_CLOUD=\"huawei\"\n":                      "huawei",
		"ACORNFOX_CLOUD=Aliyun\n":                          "aliyun",
		"ACORNFOX_CLOUD=\n":                                "",
		"ACORNFOX_IN_CHINA=true\n":                         "",
	} {
		p := filepath.Join(dir, "install.env")
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := LinuxInstallCloud(p); got != want {
			t.Fatalf("%q: got %q want %q", content, got, want)
		}
	}
	if got := LinuxInstallCloud(filepath.Join(dir, "missing")); got != "" {
		t.Fatalf("missing file: %q", got)
	}
}
