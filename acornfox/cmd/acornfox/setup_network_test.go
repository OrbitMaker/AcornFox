package main

import (
	"os"
	"path/filepath"
	"testing"

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
