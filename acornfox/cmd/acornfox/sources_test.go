package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/platform"
	"github.com/acornfox/acornfox/internal/runner"
)

// sourcesEnv points install.env and daemon.json at temporary files.
func sourcesEnv(t *testing.T, env, daemon string) {
	t.Helper()
	if platform.GOOS != "linux" {
		t.Skip("install.env is the Linux server's")
	}
	dir := t.TempDir()
	oldEnv, oldDaemon := linuxInstallEnv, daemonConfigFile
	linuxInstallEnv, daemonConfigFile = filepath.Join(dir, "install.env"), filepath.Join(dir, "daemon.json")
	t.Cleanup(func() { linuxInstallEnv, daemonConfigFile = oldEnv, oldDaemon })
	os.WriteFile(linuxInstallEnv, []byte(env), 0o644)
	if daemon != "" {
		os.WriteFile(daemonConfigFile, []byte(daemon), 0o644)
	}
	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "")
	os.Unsetenv("ACORNFOX_REGISTRY_MIRRORS")
}

func fakeSourcesProbe(t *testing.T, res map[string]runner.ProbeResult) {
	old := sourcesProbe
	sourcesProbe = func(ctx context.Context, srcs []runner.ImageSource, ref, arch string) map[string]runner.ProbeResult {
		out := map[string]runner.ProbeResult{}
		for _, s := range srcs {
			r, ok := res[s.Host]
			if !ok {
				r = runner.ProbeResult{Status: runner.ProbeOK, MBps: 9}
			}
			r.Host = s.Host
			out[s.Host] = r
		}
		return out
	}
	t.Cleanup(func() { sourcesProbe = old })
}

func TestSourcesCommand(t *testing.T) {
	sourcesEnv(t, "ACORNFOX_IN_CHINA=true\nACORNFOX_CLOUD=tencent\n", `{"registry-mirrors":["https://abc.mirror.aliyuncs.com"]}`)
	var out, errb bytes.Buffer
	if rc := runSources([]string{"--json"}, &out, &errb); rc != 0 {
		t.Fatalf("rc %d %s", rc, errb.String())
	}
	var got struct {
		OK        bool                           `json:"ok"`
		China     bool                           `json:"china"`
		DockerHub []struct{ Host, Layer string } `json:"docker_hub"`
		GHCR      []struct{ Host string }        `json:"ghcr"`
		Skipped   []string                       `json:"upstream_skipped"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || !got.OK || !got.China {
		t.Fatalf("%v %s", err, out.String())
	}
	if got.DockerHub[0].Host != "abc.mirror.aliyuncs.com" || got.DockerHub[0].Layer != "user" ||
		got.DockerHub[1].Host != runner.TencentRegistryMirror || got.DockerHub[1].Layer != "cloud" ||
		got.GHCR[0].Host != "ghcr.nju.edu.cn" {
		t.Fatalf("order %s", out.String())
	}
	// Docker Hub and ghcr.io themselves are blocked in China: not in the
	// lists, said so in the output.
	for _, h := range got.DockerHub {
		if h.Host == runner.DockerHubRegistry {
			t.Fatalf("Docker Hub listed for China: %s", out.String())
		}
	}
	if len(got.GHCR) != 1 || len(got.Skipped) != 2 || got.Skipped[0] != runner.DockerHubRegistry || got.Skipped[1] != runner.GHCRRegistry {
		t.Fatalf("upstream_skipped %s", out.String())
	}
	out.Reset()
	if rc := runSources(nil, &out, &errb); rc != 0 || !strings.Contains(out.String(), "不用 "+runner.DockerHubRegistry+" 本身：国内被屏蔽") ||
		!strings.Contains(out.String(), "ACORNFOX_REGISTRY_MIRRORS") {
		t.Fatalf("text: %s", out.String())
	}

	fakeSourcesProbe(t, map[string]runner.ProbeResult{
		"docker.1panel.live":   {Status: runner.ProbeDenied, Detail: "拒绝访问（403）"},
		"docker.m.daocloud.io": {Status: runner.ProbeSlow, MBps: 0.16, Detail: "慢（0.16 MB/s，镜像层被转到 production.cloudfront.docker.com）"},
	})
	out.Reset()
	if rc := runSources([]string{"--probe"}, &out, &errb); rc != 0 {
		t.Fatal(rc)
	}
	for _, want := range []string{"国内", "docker.1panel.live", "不能用，会跳过：拒绝访问（403）", "很慢，会排到后面", "会跳过：docker.1panel.live",
		"拉镜像时的先后：abc.mirror.aliyuncs.com → " + runner.TencentRegistryMirror} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("lacks %q:\n%s", want, out.String())
		}
	}

	old := sourcesDaemonMirrors
	sourcesDaemonMirrors = func(ctx context.Context, cloud string) []string {
		if cloud != "tencent" {
			t.Errorf("cloud %q", cloud)
		}
		return []string{"mirror.ccs.tencentyun.com", "docker.m.daocloud.io"}
	}
	t.Cleanup(func() { sourcesDaemonMirrors = old })
	out.Reset()
	runSources([]string{"--daemon-mirrors"}, &out, &errb)
	if out.String() != "https://mirror.ccs.tencentyun.com\nhttps://docker.m.daocloud.io\n" {
		t.Fatalf("daemon mirrors %q", out.String())
	}
	if rc := runSources([]string{"--bogus"}, &out, &errb); rc != 2 {
		t.Fatalf("bad flag rc %d", rc)
	}
}

func TestSourcesDaemonMirrorsAbroad(t *testing.T) {
	sourcesEnv(t, "ACORNFOX_IN_CHINA=false\n", "")
	var out, errb bytes.Buffer
	runSources([]string{"--daemon-mirrors"}, &out, &errb)
	if out.Len() != 0 {
		t.Fatalf("abroad: nothing for daemon.json, got %q", out.String())
	}
}

func TestDoctorRegistry(t *testing.T) {
	sourcesEnv(t, "ACORNFOX_IN_CHINA=true\n", "")
	fakeSourcesProbe(t, map[string]runner.ProbeResult{
		"docker.m.daocloud.io":   {Status: runner.ProbeUnreachable, Detail: "连不上"},
		"docker.1panel.live":     {Status: runner.ProbeDenied, Detail: "拒绝访问（403）"},
		"dockerproxy.net":        {Status: runner.ProbeOK, MBps: 1.9},
		runner.DockerHubRegistry: {Status: runner.ProbeUnreachable, Detail: "连不上"},
	})
	r := doctorReport{OK: true}
	doctorRegistry(context.Background(), &r, t.TempDir())
	if !r.OK || len(r.Items) != 2 || !strings.Contains(r.Items[0].Text, "现在先用 dockerproxy.net（别人公开的国内副本，1.9 MB/s）") ||
		r.Items[1].Code != "SETUP-NET-03" || !strings.Contains(r.Items[1].Text, "docker.1panel.live 拒绝访问（403）（跳过）") {
		t.Fatalf("%+v", r.Items)
	}
	for _, it := range r.Items {
		if strings.Contains(it.Text, runner.DockerHubRegistry) {
			t.Fatalf("China: Docker Hub is not probed or reported: %+v", r.Items)
		}
	}

	fakeSourcesProbe(t, map[string]runner.ProbeResult{
		"docker.m.daocloud.io":   {Status: runner.ProbeUnreachable, Detail: "连不上"},
		"docker.1panel.live":     {Status: runner.ProbeUnreachable, Detail: "连不上"},
		"dockerproxy.net":        {Status: runner.ProbeUnreachable, Detail: "连不上"},
		runner.DockerHubRegistry: {Status: runner.ProbeUnreachable, Detail: "连不上"},
	})
	r = doctorReport{OK: true}
	doctorRegistry(context.Background(), &r, t.TempDir())
	if r.OK || r.Items[0].Code != "SETUP-NET-01" {
		t.Fatalf("none usable: %+v", r.Items)
	}
}

func TestDoctorDaemonMirrors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "daemon.json")
	os.WriteFile(p, []byte(`{"registry-mirrors":["https://docker.m.daocloud.io","https://docker.1panel.live"]}`), 0o644)
	old := doctorPing
	doctorPing = func(ctx context.Context, host string) runner.ProbeResult {
		if host == "docker.1panel.live" {
			return runner.ProbeResult{Host: host, Status: runner.ProbeDenied, Detail: "拒绝访问（403）"}
		}
		return runner.ProbeResult{Host: host, Status: runner.ProbeOK}
	}
	t.Cleanup(func() { doctorPing = old })
	r := doctorReport{OK: true}
	doctorDaemonMirrors(context.Background(), &r, p, map[string]runner.ProbeResult{"docker.m.daocloud.io": {Status: runner.ProbeSlow}})
	if !r.OK || len(r.Items) != 1 || r.Items[0].Level != "warn" || r.Items[0].Code != "SETUP-NET-03" ||
		!strings.Contains(r.Items[0].Text, "docker.1panel.live 拒绝访问（403）") {
		t.Fatalf("%+v", r.Items)
	}
	r = doctorReport{OK: true}
	doctorDaemonMirrors(context.Background(), &r, filepath.Join(t.TempDir(), "none"), nil)
	if len(r.Items) != 0 {
		t.Fatalf("no daemon.json: nothing to say, got %+v", r.Items)
	}
}

// Abroad the runner pulls from Docker Hub first and does not probe, so the
// doctor probes only Docker Hub, and the mirrors only when Docker Hub is not
// usable; it never says a mirror is "skipped" there.
func TestDoctorRegistryAbroad(t *testing.T) {
	sourcesEnv(t, "ACORNFOX_IN_CHINA=false\n", "")
	var probed []string
	old := sourcesProbe
	answer := map[string]runner.ProbeResult{}
	sourcesProbe = func(ctx context.Context, srcs []runner.ImageSource, ref, arch string) map[string]runner.ProbeResult {
		out := map[string]runner.ProbeResult{}
		for _, s := range srcs {
			probed = append(probed, s.Host)
			r, ok := answer[s.Host]
			if !ok {
				r = runner.ProbeResult{Status: runner.ProbeOK, MBps: 9}
			}
			r.Host = s.Host
			out[s.Host] = r
		}
		return out
	}
	t.Cleanup(func() { sourcesProbe = old })

	r := doctorReport{OK: true}
	doctorRegistry(context.Background(), &r, t.TempDir())
	if strings.Join(probed, ",") != runner.DockerHubRegistry {
		t.Fatalf("Docker Hub usable: only it is probed, got %v", probed)
	}
	if !r.OK || len(r.Items) != 1 || !strings.Contains(r.Items[0].Text, "境外，先用 "+runner.DockerHubRegistry) {
		t.Fatalf("%+v", r.Items)
	}

	probed = nil
	answer = map[string]runner.ProbeResult{
		runner.DockerHubRegistry: {Status: runner.ProbeUnreachable, Detail: "连不上"},
		"docker.m.daocloud.io":   {Status: runner.ProbeDenied, Detail: "拒绝访问（403）"},
	}
	r = doctorReport{OK: true}
	doctorRegistry(context.Background(), &r, t.TempDir())
	if len(probed) < 2 || probed[0] != runner.DockerHubRegistry {
		t.Fatalf("Docker Hub not usable: the mirrors are probed after it, got %v", probed)
	}
	if !r.OK || len(r.Items) != 2 || !strings.Contains(r.Items[0].Text, "现在能用的是 docker.1panel.live") ||
		r.Items[1].Code != "SETUP-NET-03" || strings.Contains(r.Items[1].Text, "跳过") {
		t.Fatalf("%+v", r.Items)
	}

	answer = map[string]runner.ProbeResult{}
	for _, h := range append(runner.DefaultRegistryMirrors, runner.DockerHubRegistry) {
		answer[h] = runner.ProbeResult{Status: runner.ProbeUnreachable, Detail: "连不上"}
	}
	r = doctorReport{OK: true}
	doctorRegistry(context.Background(), &r, t.TempDir())
	if r.OK || r.Items[0].Code != "SETUP-NET-01" {
		t.Fatalf("none usable abroad: %+v", r.Items)
	}
}

// Abroad `sources --probe` reports the pull order, not a probe ranking.
func TestSourcesProbeAbroadKeepsPullOrder(t *testing.T) {
	sourcesEnv(t, "ACORNFOX_IN_CHINA=false\n", "")
	fakeSourcesProbe(t, map[string]runner.ProbeResult{
		runner.DockerHubRegistry: {Status: runner.ProbeSlow, MBps: 0.3, Detail: "慢（0.30 MB/s）"},
		"docker.1panel.live":     {Status: runner.ProbeDenied, Detail: "拒绝访问（403）"},
	})
	var out, errb bytes.Buffer
	if rc := runSources([]string{"--probe", "--json"}, &out, &errb); rc != 0 {
		t.Fatalf("rc %d %s", rc, errb.String())
	}
	var got struct {
		Probe struct {
			Order   []string `json:"order"`
			Skipped []string `json:"skipped"`
		} `json:"probe"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Probe.Order) == 0 || got.Probe.Order[0] != runner.DockerHubRegistry || len(got.Probe.Skipped) != 0 {
		t.Fatalf("abroad order %+v", got.Probe)
	}
}
