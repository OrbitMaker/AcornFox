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
		// In mainland China Docker Hub itself is blocked: not in the list.
		{"china defaults", Registry{China: true}, defaults},
		{"china tencent", Registry{China: true, Mirrors: CloudRegistryMirrors("tencent")},
			TencentRegistryMirror + "," + defaults},
		{"china no mirrors", Registry{China: true, Mirrors: []string{}}, DockerHubRegistry},
		// The user names it (a machine that reaches it): it stays where they put it.
		{"china user lists hub", Registry{China: true, Mirrors: []string{"a.example.com", DockerHubRegistry}}, "a.example.com," + DockerHubRegistry},
		{"global hub listed twice", Registry{Mirrors: []string{"a.example.com", DockerHubRegistry}}, DockerHubRegistry + ",a.example.com"},
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
	if got := strings.Join(Registry{China: true}.PullSources(), ","); got != "mirror.example.com" {
		t.Fatalf("env mirrors: got %q", got)
	}
	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "")
	if got := strings.Join(Registry{China: true}.PullSources(), ","); got != DockerHubRegistry {
		t.Fatalf("mirrors disabled: got %q", got)
	}
}

// Docker Hub and ghcr.io are left out in mainland China unless the user names
// them or there is no mirror at all; elsewhere never.
func TestSkipsUpstream(t *testing.T) {
	os.Unsetenv("ACORNFOX_REGISTRY_MIRRORS")
	os.Unsetenv("ACORNFOX_GHCR_MIRRORS")
	cn := Registry{China: true}
	if !cn.SkipsUpstream() || !cn.SkipsGHCRUpstream() {
		t.Fatal("china with the default mirrors skips both")
	}
	if (Registry{}).SkipsUpstream() || (Registry{}).SkipsGHCRUpstream() {
		t.Fatal("abroad skips nothing")
	}
	if (Registry{China: true, Mirrors: []string{}}).SkipsUpstream() {
		t.Fatal("no mirrors: Docker Hub is all there is")
	}
	if (Registry{China: true, Mirrors: []string{"a.example.com", DockerHubRegistry}}).SkipsUpstream() {
		t.Fatal("the user named Docker Hub")
	}
	t.Setenv("ACORNFOX_GHCR_MIRRORS", "ghcr.nju.edu.cn,"+GHCRRegistry)
	if cn.SkipsGHCRUpstream() {
		t.Fatal("the user named ghcr.io")
	}
	if got := hostsOf(cn.GHCRSources()); got != "ghcr.nju.edu.cn,"+GHCRRegistry {
		t.Fatalf("ghcr sources %s", got)
	}
}

func hostsOf(l []ImageSource) string {
	var h []string
	for _, s := range l {
		h = append(h, s.Host)
	}
	return strings.Join(h, ",")
}
