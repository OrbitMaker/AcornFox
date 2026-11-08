package main

import (
	"context"
	"os"
	"path/filepath"
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
	old, oldProbe := linuxInstallEnv, doctorClockProbe
	linuxInstallEnv = env
	t.Cleanup(func() { linuxInstallEnv, doctorClockProbe = old, oldProbe })
	t.Setenv("ACORNFOX_GATEWAY", "")
	os.Unsetenv("ACORNFOX_GATEWAY")
	dataDir := t.TempDir()

	probed := []string{}
	doctorClockProbe = func(gw string) hostcheck.ClockProbe {
		probed = append(probed, gw)
		return hostcheck.ClockProbe{Sync: func(context.Context) (hostcheck.SyncStatus, error) {
			return hostcheck.SyncStatus{SyncKnown: true, Synchronized: true}, nil
		}}
	}
	check := func(name, want string) {
		t.Helper()
		probed = probed[:0]
		r := doctorReport{OK: true}
		doctorClock(context.Background(), &r, dataDir)
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
