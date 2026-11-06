package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/runner"
)

// fakeProbe answers for the hosts in up and records every host it was asked.
type fakeProbe struct {
	mu    sync.Mutex
	up    map[string]bool
	asked []string
}

func (f *fakeProbe) probe(_ context.Context, host string) error {
	f.mu.Lock()
	f.asked = append(f.asked, host)
	f.mu.Unlock()
	if f.up[host] {
		return nil
	}
	return errors.New("连不上")
}

func useFakeProbe(t *testing.T, up ...string) *fakeProbe {
	t.Helper()
	f := &fakeProbe{up: map[string]bool{}}
	for _, h := range up {
		f.up[h] = true
	}
	old := probeRegistry
	probeRegistry = f.probe
	t.Cleanup(func() { probeRegistry = old })
	return f
}

func useInstallEnv(t *testing.T, content string) {
	t.Helper()
	env := filepath.Join(t.TempDir(), "install.env")
	if err := os.WriteFile(env, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := linuxInstallEnv
	linuxInstallEnv = env
	t.Cleanup(func() { linuxInstallEnv = old })
}

func unsetRegistryMirrorsEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "")
	os.Unsetenv("ACORNFOX_REGISTRY_MIRRORS")
}

// A Tencent Cloud server whose internal accelerator does not answer still
// pulls through the next mirror, so the check passes and names that mirror.
func TestCheckRegistryTencentFallsBack(t *testing.T) {
	unsetRegistryMirrorsEnv(t)
	useInstallEnv(t, "ACORNFOX_IN_CHINA=true\nACORNFOX_CLOUD=tencent\nACORNFOX_CLOUD_REGION=override\n")
	next := runner.DefaultRegistryMirrors[0]
	f := useFakeProbe(t, next)

	ok, detail := checkRegistry(context.Background(), t.TempDir())
	if !ok {
		t.Fatalf("check failed although %s answers: %s", next, detail)
	}
	if !strings.HasPrefix(detail, "用国内镜像（"+next+"）") || !strings.Contains(detail, runner.TencentRegistryMirror) {
		t.Fatalf("detail should name the mirror in use and the skipped one: %q", detail)
	}
	if !slices.Contains(f.asked, runner.TencentRegistryMirror) {
		t.Fatalf("the Tencent accelerator was not probed: %v", f.asked)
	}
}

// When the first source answers the detail just names it.
func TestCheckRegistryFirstSource(t *testing.T) {
	unsetRegistryMirrorsEnv(t)
	useInstallEnv(t, "ACORNFOX_IN_CHINA=true\nACORNFOX_CLOUD=tencent\n")
	useFakeProbe(t, runner.TencentRegistryMirror, runner.DefaultRegistryMirrors[0])
	ok, detail := checkRegistry(context.Background(), t.TempDir())
	if want := "腾讯云内网加速（" + runner.TencentRegistryMirror + "）"; !ok || detail != want {
		t.Fatalf("got %v %q, want ok %q", ok, detail, want)
	}
}

// Only when no source answers does the check fail (SETUP-NET-01), listing
// every source it tried in the runner's order.
func TestCheckRegistryAllDown(t *testing.T) {
	unsetRegistryMirrorsEnv(t)
	useInstallEnv(t, "ACORNFOX_IN_CHINA=true\nACORNFOX_CLOUD=tencent\n")
	useFakeProbe(t)
	ok, detail := checkRegistry(context.Background(), t.TempDir())
	if ok {
		t.Fatalf("check passed with every source down: %q", detail)
	}
	want := append(append([]string{runner.TencentRegistryMirror}, runner.DefaultRegistryMirrors...), runner.DockerHubRegistry)
	last := -1
	for _, h := range want {
		i := strings.Index(detail, h)
		if i < 0 || i < last {
			t.Fatalf("detail should list %v in order: %q", want, detail)
		}
		last = i
	}
}

// Overseas servers try Docker Hub first, the mirrors after it.
func TestCheckRegistryOverseas(t *testing.T) {
	unsetRegistryMirrorsEnv(t)
	useInstallEnv(t, "ACORNFOX_IN_CHINA=false\n")
	useFakeProbe(t, runner.DockerHubRegistry)
	ok, detail := checkRegistry(context.Background(), t.TempDir())
	if want := "Docker Hub（" + runner.DockerHubRegistry + "）"; !ok || detail != want {
		t.Fatalf("got %v %q, want ok %q", ok, detail, want)
	}

	useFakeProbe(t, runner.DefaultRegistryMirrors[1])
	ok, detail = checkRegistry(context.Background(), t.TempDir())
	if !ok || !strings.HasPrefix(detail, "用镜像加速（"+runner.DefaultRegistryMirrors[1]+"）") {
		t.Fatalf("Docker Hub down, second mirror up: got %v %q", ok, detail)
	}
}

// No source at all fails rather than passing vacuously.
func TestCheckRegistrySourcesEmpty(t *testing.T) {
	if ok, _ := checkRegistrySources(context.Background(), nil, true, (&fakeProbe{}).probe); ok {
		t.Fatal("no sources must fail")
	}
}
