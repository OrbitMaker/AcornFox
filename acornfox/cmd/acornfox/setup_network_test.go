package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/runner"
)

// The Linux server's self checks follow install.env, not the missing
// network.json (which would mean China): an overseas server upgraded from
// v0.3.1 must not be told the domestic mirror is unreachable.
func TestSetupNetworkFollowsInstallEnv(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "install.env")
	old := linuxInstallEnv
	linuxInstallEnv = env
	t.Cleanup(func() { linuxInstallEnv = old })
	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "")
	os.Unsetenv("ACORNFOX_REGISTRY_MIRRORS")

	dataDir := t.TempDir()

	// No install.env: network.json decides (missing means China).
	if n := setupNetwork(dataDir); !n.China || n.Source != "default" {
		t.Fatalf("no install.env: got %+v, want China from default", n)
	}

	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(env, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("ACORNFOX_IN_CHINA=false\n")
	if n := setupNetwork(dataDir); n.China || len(n.Mirrors) != 0 {
		t.Fatalf("overseas: got %+v, want global", n)
	}

	write("ACORNFOX_IN_CHINA=true\nACORNFOX_CLOUD=tencent\nACORNFOX_CLOUD_REGION=ap-nanjing\n")
	n := setupNetwork(dataDir)
	if !n.China || len(n.Mirrors) == 0 || n.Mirrors[0] != runner.TencentRegistryMirror {
		t.Fatalf("tencent mainland: got %+v, want Tencent mirror first", n)
	}

	// install.env wins over a network.json left behind.
	write("ACORNFOX_IN_CHINA=false\n")
	if err := platform.SaveNetwork(dataDir, platform.Network{China: true}); err != nil {
		t.Fatal(err)
	}
	if n := setupNetwork(dataDir); n.China {
		t.Fatalf("install.env should win over network.json: got %+v", n)
	}
}

// The app center, doctor's clock check and the settings/runtime pages take
// the download gateway's address from gatewayBase, so they agree: with or
// without install.env, network.json's "gateway": "off" turns it off for all
// of them, and ACORNFOX_GATEWAY wins.
func TestGatewayBaseSharedByDoctorAndPages(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "install.env")
	old, oldProbe := linuxInstallEnv, doctorGatewayProbes
	linuxInstallEnv = env
	t.Cleanup(func() { linuxInstallEnv, doctorGatewayProbes = old, oldProbe })
	t.Setenv("ACORNFOX_GATEWAY", "")
	os.Unsetenv("ACORNFOX_GATEWAY")
	dataDir := t.TempDir()

	probed := []string{}
	doctorGatewayProbes = func(gw string) (hostcheck.ClockProbe, hostcheck.GatewayProbe) {
		probed = append(probed, gw)
		return hostcheck.ClockProbe{Sync: func(context.Context) (hostcheck.SyncStatus, error) {
			return hostcheck.SyncStatus{SyncKnown: true, Synchronized: true}, nil
		}}, hostcheck.GatewayProbe{Base: gw}
	}
	check := func(name, want string) {
		t.Helper()
		probed = probed[:0]
		r := doctorReport{OK: true}
		doctorClockAndGateway(context.Background(), &r, dataDir)
		if len(probed) != 1 || probed[0] != want {
			t.Fatalf("%s: doctor probed %q, want %q", name, probed, want)
		}
		if got := gatewayBase(dataDir); got != want {
			t.Fatalf("%s: gatewayBase %q, want %q", name, got, want)
		}
		if got := downloadSources(dataDir).Gateway; got != want {
			t.Fatalf("%s: settings page shows %q, want %q", name, got, want)
		}
	}

	// No install.env, no network.json: the default gateway.
	check("default", platform.DefaultGateway)
	// No install.env, network.json turns it off.
	if err := platform.SaveNetwork(dataDir, platform.Network{China: true, Gateway: "off"}); err != nil {
		t.Fatal(err)
	}
	check("network.json off", "")
	// install.env decides the region; network.json still turns the gateway off.
	if err := os.WriteFile(env, []byte("ACORNFOX_IN_CHINA=true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check("install.env + network.json off", "")
	// install.env with no network.json: the default gateway.
	if err := os.Remove(platform.NetworkFile(dataDir)); err != nil {
		t.Fatal(err)
	}
	check("install.env default", platform.DefaultGateway)
	// ACORNFOX_GATEWAY wins.
	t.Setenv("ACORNFOX_GATEWAY", "https://gw.example.test")
	check("env", "https://gw.example.test")
}

// A user who may not read the data directory (doctor without sudo) cannot see
// whether network.json turned the gateway off, so doctor must not contact
// it: the clock is judged by time sync alone and the gateway line says to use
// sudo. Seen on a test machine: with the gateway turned off, `acornfox
// doctor` run by a member of acornfox-users still sent its HEAD.
func TestDoctorWithoutAccessToTheSettingDoesNotContactTheGateway(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file; run as a normal user")
	}
	old, oldProbe := linuxInstallEnv, doctorGatewayProbes
	linuxInstallEnv = filepath.Join(t.TempDir(), "install.env")
	t.Cleanup(func() { linuxInstallEnv, doctorGatewayProbes = old, oldProbe })
	t.Setenv("ACORNFOX_GATEWAY", "")
	os.Unsetenv("ACORNFOX_GATEWAY")
	dataDir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := platform.SaveNetwork(dataDir, platform.Network{Gateway: "off"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dataDir, 0o100); err != nil { // like /var/lib/acornfox to a stranger: no read
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o700) })
	if err := os.Chmod(platform.NetworkFile(dataDir), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(platform.NetworkFile(dataDir), 0o600) })

	var probed []string
	doctorGatewayProbes = func(gw string) (hostcheck.ClockProbe, hostcheck.GatewayProbe) {
		probed = append(probed, gw)
		return hostcheck.ClockProbe{Sync: func(context.Context) (hostcheck.SyncStatus, error) {
			return hostcheck.SyncStatus{SyncKnown: true, Synchronized: true}, nil
		}}, hostcheck.GatewayProbe{Base: gw}
	}
	if gatewaySettingReadable(dataDir) {
		t.Fatal("the setting must count as unreadable")
	}
	r := doctorReport{OK: true}
	doctorClockAndGateway(context.Background(), &r, dataDir)
	if len(probed) != 1 || probed[0] != "" {
		t.Fatalf("doctor must not be given a gateway to contact: %q", probed)
	}
	if len(r.Items) != 2 || r.Items[0].Check != "SETUP-CLOCK-01" || r.Items[1].Check != "SETUP-GW-01" ||
		!r.Items[1].OK || !strings.Contains(r.Items[1].Text, "sudo acornfox doctor") {
		t.Fatalf("items %+v", r.Items)
	}

	// ACORNFOX_GATEWAY set: known without the file.
	t.Setenv("ACORNFOX_GATEWAY", "")
	if !gatewaySettingReadable(dataDir) {
		t.Fatal("ACORNFOX_GATEWAY decides by itself")
	}
	os.Unsetenv("ACORNFOX_GATEWAY")
	// No file at all (readable directory): the default gateway, contacted.
	_ = os.Chmod(dataDir, 0o700)
	_ = os.Remove(platform.NetworkFile(dataDir))
	if !gatewaySettingReadable(dataDir) {
		t.Fatal("a missing network.json is known: the default gateway")
	}
}
