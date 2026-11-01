package runner

import "testing"

func TestMirrorRef(t *testing.T) {
	cases := map[string]string{
		"caddy:2.8.4-alpine":         "docker.m.daocloud.io/library/caddy:2.8.4-alpine",
		"postgres:16-alpine":         "docker.m.daocloud.io/library/postgres:16-alpine",
		"bitnami/redis:7":            "docker.m.daocloud.io/bitnami/redis:7",
		"docker.io/library/nginx":    "docker.m.daocloud.io/library/nginx",
		"ghcr.io/org/app:1":          "",
		"registry.example.com/a/b:2": "",
	}
	for in, want := range cases {
		if got := mirrorRef(in, "docker.m.daocloud.io"); got != want {
			t.Errorf("mirrorRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistryMirrorsEnv(t *testing.T) {
	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", " a.example , b.example ")
	if m := registryMirrors(); len(m) != 2 || m[0] != "a.example" || m[1] != "b.example" {
		t.Fatalf("mirrors = %v", m)
	}
	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "")
	if m := registryMirrors(); len(m) != 0 {
		t.Fatalf("empty env must disable the fallback: %v", m)
	}
}
