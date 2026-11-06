package runner

import (
	"os"
	"strings"
	"testing"
)

// PullSources is the order pullFallback tries a Docker Hub image in; the
// setup's registry check relies on it matching.
func TestRegistryPullSources(t *testing.T) {
	if v, ok := os.LookupEnv("ACORNFOX_REGISTRY_MIRRORS"); ok {
		os.Unsetenv("ACORNFOX_REGISTRY_MIRRORS")
		t.Cleanup(func() { os.Setenv("ACORNFOX_REGISTRY_MIRRORS", v) })
	}
	defaults := strings.Join(DefaultRegistryMirrors, ",")
	for _, tc := range []struct {
		name string
		reg  Registry
		want string
	}{
		{"china defaults", Registry{China: true}, defaults + "," + DockerHubRegistry},
		{"china tencent", Registry{China: true, Mirrors: CloudRegistryMirrors("tencent")},
			TencentRegistryMirror + "," + defaults + "," + DockerHubRegistry},
		{"china no mirrors", Registry{China: true, Mirrors: []string{}}, DockerHubRegistry},
		{"global defaults", Registry{}, DockerHubRegistry + "," + defaults},
		{"global no mirrors", Registry{Mirrors: []string{}}, DockerHubRegistry},
	} {
		if got := strings.Join(tc.reg.PullSources(), ","); got != tc.want {
			t.Fatalf("%s: PullSources() = %q, want %q", tc.name, got, tc.want)
		}
	}

	// The result must not alias the configured mirrors.
	m := []string{"a.example.com"}
	src := Registry{China: true, Mirrors: m}.PullSources()
	src[0] = "changed"
	if m[0] != "a.example.com" {
		t.Fatal("PullSources aliased Registry.Mirrors")
	}

	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "mirror.example.com")
	if got := strings.Join(Registry{China: true}.PullSources(), ","); got != "mirror.example.com,"+DockerHubRegistry {
		t.Fatalf("env mirrors: got %q", got)
	}
	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "")
	if got := strings.Join(Registry{China: true}.PullSources(), ","); got != DockerHubRegistry {
		t.Fatalf("mirrors disabled: got %q", got)
	}
}
