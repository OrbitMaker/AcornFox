package runner

import (
	"os"
	"strings"
	"testing"
)

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

func TestGHCRMirrorRef(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/open-webui/open-webui:main":                "ghcr.nju.edu.cn/open-webui/open-webui:main",
		"ghcr.io/org/app@sha256:" + strings.Repeat("a", 64): "ghcr.nju.edu.cn/org/app@sha256:" + strings.Repeat("a", 64),
		"caddy:2.8.4-alpine":                                "",
		"registry.example.com/a/b:2":                        "",
	}
	for in, want := range cases {
		if got := ghcrMirrorRef(in, "ghcr.nju.edu.cn"); got != want {
			t.Errorf("ghcrMirrorRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGHCRMirrorsEnv(t *testing.T) {
	t.Setenv("ACORNFOX_GHCR_MIRRORS", "")
	os.Unsetenv("ACORNFOX_GHCR_MIRRORS") // t.Setenv restores the caller's value afterwards
	if m := ghcrMirrors(); len(m) != 1 || m[0] != "ghcr.nju.edu.cn" {
		t.Fatalf("default ghcr mirrors = %v", m)
	}
	t.Setenv("ACORNFOX_GHCR_MIRRORS", "")
	if m := ghcrMirrors(); len(m) != 0 {
		t.Fatalf("empty env must disable ghcr mirrors: %v", m)
	}
}
