package macvm

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRunner answers commands by the first rule whose text is in the command line.
type fakeRunner struct {
	rules []fakeRule
	calls []string
	// hook, when set, answers first: handled false falls through to the rules.
	hook func(line string, n int) (out string, code int, handled bool)
}

type fakeRule struct {
	has  string
	out  string
	code int
}

func (f *fakeRunner) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) (int, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if f.hook != nil {
		if out, code, ok := f.hook(line, len(f.calls)); ok {
			if stdout != nil {
				_, _ = io.WriteString(stdout, out)
			}
			return code, nil
		}
	}
	for _, r := range f.rules {
		if strings.Contains(line, r.has) {
			if stdout != nil {
				_, _ = io.WriteString(stdout, r.out)
			}
			return r.code, nil
		}
	}
	return 0, nil
}

type fakeHost struct {
	goos, arch, ver string
	mem             uint64
	disk            int
	tty             bool
	lan             string
}

func (h fakeHost) GOOS() string                            { return h.goos }
func (h fakeHost) Arch() string                            { return h.arch }
func (h fakeHost) MacOSVersion(ctx context.Context) string { return h.ver }
func (h fakeHost) MemBytes(ctx context.Context) uint64     { return h.mem }
func (h fakeHost) FreeDiskGB(path string) int              { return h.disk }
func (h fakeHost) LANIP() string                           { return h.lan }
func (h fakeHost) HasTTY() bool                            { return h.tty }
func (h fakeHost) TargetsFile() string                     { return "/nonexistent/targets.json" }

func goodHost() fakeHost {
	return fakeHost{goos: "darwin", arch: "arm64", ver: "26.4", mem: 18 << 30, disk: 90, tty: true, lan: "192.168.1.20"}
}

func newInstaller(t *testing.T, h fakeHost, r Runner) (*Installer, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	in := &Installer{Paths: Paths{Root: filepath.Join(t.TempDir(), "mac")}, R: r, Host: h, Out: &out, Exe: "/nonexistent/acornfox", Version: "0.6.97",
		Opt: Options{IdleMinutes: -1}}
	return in, &out
}

func TestCheckStopsEarly(t *testing.T) {
	cases := []struct {
		name string
		h    func(*fakeHost)
		code string
	}{
		{"Intel", func(h *fakeHost) { h.arch = "amd64" }, "MAC-ARCH-01"},
		{"系统太旧", func(h *fakeHost) { h.ver = "15.6" }, "MAC-OS-01"},
		{"不是 Mac", func(h *fakeHost) { h.goos = "linux" }, "MAC-OS-01"},
		{"磁盘不够", func(h *fakeHost) { h.disk = 3 }, "MAC-DISK-01"},
	}
	for _, c := range cases {
		h := goodHost()
		c.h(&h)
		in, _ := newInstaller(t, h, &fakeRunner{})
		in.Cfg = DefaultConfig(h.mem)
		_, _, err := in.stepCheck(context.Background())
		var f *Fault
		if !errors.As(err, &f) || f.Code != c.code {
			t.Errorf("%s：应报 %s，实际 %v", c.name, c.code, err)
		}
	}
	in, _ := newInstaller(t, goodHost(), &fakeRunner{})
	in.Cfg = DefaultConfig(18 << 30)
	if st, detail, err := in.stepCheck(context.Background()); err != nil || st != StateDone || !strings.Contains(detail, "6 GB") {
		t.Errorf("好的 Mac 应通过，并说虚拟机给 6 GB：%v %q %v", st, detail, err)
	}
}

func TestPlanChangesNothing(t *testing.T) {
	// A Mac without Apple's container: the plan says what would happen and touches nothing.
	r := &fakeRunner{rules: []fakeRule{{has: "--version", out: "", code: 127}, {has: "system status", code: 1}, {has: "image inspect", code: 1}, {has: "machine list", out: "[]"}}}
	in, out := newInstaller(t, goodHost(), r)
	in.Opt.Plan, in.Opt.Action, in.Opt.JSON, in.Opt.Version = true, "install", true, "v0.6.97"
	var js bytes.Buffer
	code := in.Install(context.Background(), &js)
	if code != 0 {
		t.Fatalf("试跑应成功，退出码 %d：%s\n%s", code, js.String(), out.String())
	}
	var res jsonResult
	if err := json.Unmarshal(bytes.TrimSpace(js.Bytes()), &res); err != nil {
		t.Fatalf("JSON 读不出：%v\n%s", err, js.String())
	}
	if !res.OK || !res.Plan || len(res.Steps) != 13 {
		t.Fatalf("试跑结果不对：%+v", res)
	}
	for _, s := range res.Steps {
		if s.State != StatePlan && s.State != StateDone && s.State != StateSkip {
			t.Errorf("步骤 %s 状态 %s", s.ID, s.State)
		}
	}
	if _, err := os.Stat(in.Paths.Root); err == nil {
		t.Errorf("试跑不该建任何目录：%s 存在", in.Paths.Root)
	}
	for _, c := range r.calls {
		for _, bad := range []string{"machine create", "machine run", "launchctl", "image load", "image pull", "sudo", "system start", "kernel set"} {
			if strings.Contains(c, bad) {
				t.Errorf("试跑不该运行：%s", c)
			}
		}
	}
	if !strings.Contains(out.String(), "没有装苹果的 container") {
		t.Errorf("应说明会装苹果的 container：\n%s", out.String())
	}
}

func TestContainerNeedsAdmin(t *testing.T) {
	fetch := func(ctx context.Context, a Artifact) (string, error) { return "/downloads/" + a.File, nil }
	// No terminal: stop with exit 5 and the exact command for the person to run.
	r := &fakeRunner{rules: []fakeRule{{has: "--version", code: 127}}}
	h := goodHost()
	h.tty = false
	in, _ := newInstaller(t, h, r)
	in.FetchFn = fetch
	_, _, err := in.stepContainer(context.Background())
	var f *Fault
	if !errors.As(err, &f) || f.Code != "MAC-ADMIN-01" || f.Exit != ExitAdmin || !strings.Contains(f.Detail, "sudo installer -pkg '/downloads/container-1.7.1-installer-signed.pkg' -target /") {
		t.Fatalf("没有终端时应报 MAC-ADMIN-01、退出码 5、给出命令：%v", err)
	}
	for _, c := range r.calls {
		if strings.HasPrefix(c, "sudo") {
			t.Errorf("没有终端不该运行 sudo：%s", c)
		}
	}
	// A terminal, the password is typed wrong (sudo fails): exit 5 again, with the command.
	r2 := &fakeRunner{rules: []fakeRule{{has: "sudo installer", code: 1}, {has: "--version", code: 127}}}
	in2, _ := newInstaller(t, goodHost(), r2)
	in2.FetchFn = fetch
	_, _, err = in2.stepContainer(context.Background())
	if !errors.As(err, &f) || f.Code != "MAC-ADMIN-01" || f.Exit != ExitAdmin {
		t.Fatalf("密码输错应报 MAC-ADMIN-01：%v", err)
	}
	// Already installed: nothing is downloaded, nothing asked.
	r3 := &fakeRunner{rules: []fakeRule{{has: "--version", out: "container CLI version 1.7.1 (build: release)\n"}}}
	in3, _ := newInstaller(t, goodHost(), r3)
	in3.FetchFn = func(ctx context.Context, a Artifact) (string, error) {
		t.Fatal("已装好不该下载")
		return "", nil
	}
	if st, detail, err := in3.stepContainer(context.Background()); err != nil || st != StateSkip || !strings.Contains(detail, "1.7.1") {
		t.Fatalf("已装好应跳过：%v %q %v", st, detail, err)
	}
}

func TestUninstallPurgeNeedsYes(t *testing.T) {
	r := &fakeRunner{}
	in, _ := newInstaller(t, goodHost(), r)
	cfg := DefaultConfig(18 << 30)
	if err := cfg.Save(in.Paths); err != nil {
		t.Fatal(err)
	}
	in.Opt.Purge = true
	var js bytes.Buffer
	in.Opt.JSON = true
	code := in.Uninstall(context.Background(), &js)
	if code != 1 || !strings.Contains(js.String(), "MAC-UNINST-01") {
		t.Fatalf("没有 --yes 的 --purge 应报 MAC-UNINST-01、退出码 1：%d %s", code, js.String())
	}
	for _, c := range r.calls {
		if strings.Contains(c, "delete") || strings.Contains(c, "bootout") {
			t.Errorf("没确认就不该动东西：%s", c)
		}
	}
	if _, err := os.Stat(in.Paths.Config()); err != nil {
		t.Error("什么都不该删")
	}
}

func TestUninstallKeepsData(t *testing.T) {
	list := `[{"id":"acornfox","status":"running","ipAddress":"192.168.64.9"}]`
	r := &fakeRunner{rules: []fakeRule{{has: "machine list", out: list}, {has: "launchctl print", code: 0}}}
	in, _ := newInstaller(t, goodHost(), r)
	cfg := DefaultConfig(18 << 30)
	if err := cfg.Save(in.Paths); err != nil {
		t.Fatal(err)
	}
	in.Opt.Yes = true
	var js bytes.Buffer
	if code := in.Uninstall(context.Background(), &js); code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "machine stop acornfox") || strings.Contains(joined, "machine delete") {
		t.Errorf("保留数据只停不删：\n%s", joined)
	}
	if !strings.Contains(joined, "launchctl bootout") {
		t.Errorf("要把常驻程序卸掉：\n%s", joined)
	}
	if _, err := os.Stat(in.Paths.Config()); err != nil {
		t.Error("保留数据时 mac.json 要留着")
	}
}

func TestPurgeRemovesEverything(t *testing.T) {
	list := `[{"id":"acornfox","status":"stopped"}]`
	r := &fakeRunner{rules: []fakeRule{{has: "machine list", out: list}}}
	in, _ := newInstaller(t, goodHost(), r)
	cfg := DefaultConfig(18 << 30)
	cfg.Created = Created{VM: true, BaseImage: true}
	if err := cfg.Save(in.Paths); err != nil {
		t.Fatal(err)
	}
	in.Opt.Yes, in.Opt.Purge = true, true
	var js bytes.Buffer
	if code := in.Uninstall(context.Background(), &js); code != 0 {
		t.Fatalf("退出码 %d", code)
	}
	joined := strings.Join(r.calls, "\n")
	if !strings.Contains(joined, "machine delete acornfox") || !strings.Contains(joined, "image delete "+BaseImage) {
		t.Errorf("--purge 要删虚拟机和我们载入的镜像：\n%s", joined)
	}
	if _, err := os.Stat(in.Paths.Root); err == nil {
		t.Error("--purge 后状态目录要没有（日志也不能把它再建出来）")
	}
}

func TestVminitRef(t *testing.T) {
	props := "[build]\ncpus = 2\nimage = \"ghcr.io/apple/container-builder-shim/builder:0.13.3\"\n\n[vminit]\nimage = \"ghcr.io/apple/containerization/vminit:0.54.0\"\n"
	in, _ := newInstaller(t, goodHost(), &fakeRunner{rules: []fakeRule{{has: "system property list", out: props}}})
	if got := in.vminitRef(context.Background()); got != "ghcr.io/apple/containerization/vminit:0.54.0" {
		t.Errorf("vminit 的名字读错：%q", got)
	}
}

func TestLatestLXCDir(t *testing.T) {
	page := `<a href="20261116_22%3A14/">x</a> <a href="20261118_05:24/">y</a> <a href="20261117_05:24/">z</a><a href="../">up</a>`
	if got := LatestLXCDir(page); got != "20261118_05:24" {
		t.Errorf("最新的目录读错：%q", got)
	}
	if LatestLXCDir("nothing") != "" {
		t.Error("没有目录时应返回空")
	}
}

func TestBuildOCI(t *testing.T) {
	var layer bytes.Buffer
	tw := tar.NewWriter(&layer)
	_ = tw.WriteHeader(&tar.Header{Name: "etc/hello", Mode: 0o644, Size: 5, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("hello"))
	_ = tw.Close()
	out := filepath.Join(t.TempDir(), "base.oci.tar")
	if err := BuildOCI(bytes.NewReader(layer.Bytes()), BaseImage, out); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	files := map[string][]byte{}
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			files[strings.TrimPrefix(h.Name, "./")] = b
		}
	}
	if string(files["oci-layout"]) != `{"imageLayoutVersion":"1.0.0"}` {
		t.Errorf("oci-layout 不对：%q", files["oci-layout"])
	}
	var idx struct {
		Manifests []struct {
			Digest      string            `json:"digest"`
			Annotations map[string]string `json:"annotations"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(files["index.json"], &idx); err != nil || len(idx.Manifests) != 1 {
		t.Fatalf("index.json 不对：%v %s", err, files["index.json"])
	}
	if idx.Manifests[0].Annotations["org.opencontainers.image.ref.name"] != BaseImage {
		t.Errorf("镜像名不对：%v", idx.Manifests[0].Annotations)
	}
	// Every blob is named by its own sha256, and the manifest points at a layer that is there.
	for name, b := range files {
		if !strings.HasPrefix(name, "blobs/sha256/") {
			continue
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != strings.TrimPrefix(name, "blobs/sha256/") {
			t.Errorf("%s 的名字和内容的 sha256 不一致", name)
		}
	}
	mdigest := strings.TrimPrefix(idx.Manifests[0].Digest, "sha256:")
	var man struct {
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(files["blobs/sha256/"+mdigest], &man); err != nil || len(man.Layers) != 1 {
		t.Fatalf("manifest 不对：%v", err)
	}
	if _, ok := files["blobs/sha256/"+strings.TrimPrefix(man.Layers[0].Digest, "sha256:")]; !ok {
		t.Error("manifest 指的层不在包里")
	}
}

func TestPlistXML(t *testing.T) {
	x := PlistXML("com.acornfox.mac", "/Users/a & b/AcornFox/mac/bin/acornfox", Paths{Root: "/Users/a & b/AcornFox/mac"}, map[string]string{"ACORNFOX_MAC_VM": "x"})
	for _, want := range []string{"<string>com.acornfox.mac</string>", "<string>mac</string>", "<string>daemon</string>", "<key>RunAtLoad</key>", "<key>KeepAlive</key>", "a &amp; b", "ACORNFOX_MAC_HOME", "ACORNFOX_MAC_VM"} {
		if !strings.Contains(x, want) {
			t.Errorf("plist 里缺 %q", want)
		}
	}
	if strings.Contains(x, "a & b") {
		t.Error("路径里的 & 要转义")
	}
}

func TestReleaseURLsOrder(t *testing.T) {
	in, _ := newInstaller(t, goodHost(), &fakeRunner{})
	in.Opt.China = true
	urls := in.releaseURLs("v0.7.0", "install.sh")
	if len(urls) != 2 || !strings.Contains(urls[0], "gitee.com/VIP13390/AcornFox") || !strings.Contains(urls[1], "github.com/OrbitMaker/AcornFox") {
		t.Errorf("国内先 Gitee：%v", urls)
	}
	in.Opt.China, in.Opt.Global = false, true
	chinaCache.set, chinaCache.v = true, false
	urls = in.releaseURLs("v0.7.0", "install.sh")
	if !strings.Contains(urls[0], "github.com") {
		t.Errorf("海外先 GitHub：%v", urls)
	}
	in.Opt.ReleaseBase = "http://127.0.0.1:9/"
	if u := in.releaseURLs("v0.7.0", "SHA256SUMS"); len(u) != 1 || u[0] != "http://127.0.0.1:9/releases/download/v0.7.0/SHA256SUMS" {
		t.Errorf("测试发布源：%v", u)
	}
	in.Opt.ReleaseBase, in.Opt.GiteeRepo = "", "-"
	if u := in.releaseURLs("v0.7.0", "x"); len(u) != 1 || !strings.Contains(u[0], "github.com") {
		t.Errorf("不用 Gitee：%v", u)
	}
}

func TestLineTeeFiltersPackageNoise(t *testing.T) {
	var out bytes.Buffer
	l := &lineTee{w: &out, prefix: "| "}
	_, _ = l.Write([]byte("Unpacking foo (1.0) ...\n[INFO] 安装 Docker...\nSetting up bar\n故障编号: INSTALL-DOCKER-01\npart"))
	_, _ = l.Write([]byte("ial\n"))
	got := out.String()
	if strings.Contains(got, "Unpacking") || strings.Contains(got, "Setting up") || !strings.Contains(got, "[INFO] 安装 Docker") || !strings.Contains(got, "INSTALL-DOCKER-01") {
		t.Errorf("过滤不对：%q", got)
	}
	if !strings.Contains(l.tail(), "Setting up bar") {
		t.Error("被过滤掉的行也要留在尾部，出错时要看")
	}
}

func TestProbeNeverBootsStoppedMachine(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{{has: "machine list", out: `[{"id":"acornfox","status":"stopped"}]`}}}
	v := &CtrVM{C: &Container{R: r}, Name: "acornfox"}
	if _, err := v.Probe(context.Background()); !errors.Is(err, errNotRunning) {
		t.Fatalf("停着的虚拟机不该去看：%v", err)
	}
	if v.Ready(context.Background()) {
		t.Error("停着的虚拟机不算就绪")
	}
	for _, c := range r.calls {
		if strings.Contains(c, "machine run") {
			t.Errorf("看停着的虚拟机不该运行 machine run（它会把机器起来）：%s", c)
		}
	}
}

func TestProbeCutShortIsNotTrusted(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{has: "machine list", out: `[{"id":"acornfox","status":"running","ipAddress":"192.168.64.3"}]`},
		{has: "machine run", out: "==IP==\n192.168.64.3/24\n==TCP==\n"},
	}}
	v := &CtrVM{C: &Container{R: r}, Name: "acornfox"}
	if _, err := v.Probe(context.Background()); err == nil {
		t.Fatal("没看完（缺 END）不能当成结果，否则会把转发的端口弄丢")
	}
}

func TestStartRecoversHalfStartedInstance(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{has: "system status", out: "status              running\n"},
		{has: "machine list", out: `[{"id":"acornfox","status":"running","ipAddress":"192.168.64.4"}]`},
	}}
	killed := 0
	r.hook = func(line string, n int) (string, int, bool) {
		if strings.HasPrefix(line, "pkill ") {
			killed++
			return "", 0, true
		}
		if strings.Contains(line, "machine run") {
			if killed == 0 {
				return `Error: failed to boot container machine (cause: "exists: "hostname(s) already exist: ["acornfox.machine."]"")`, 1, true
			}
			return "afx-up\n", 0, true
		}
		return "", 0, false
	}
	v := &CtrVM{C: &Container{R: r}, Name: "acornfox", WaitReady: 5 * time.Second}
	if err := v.Start(context.Background()); err != nil {
		t.Fatalf("起不来：%v", err)
	}
	joined := strings.Join(r.calls, "\n")
	if killed != 1 || !strings.Contains(joined, "machine stop acornfox") || !strings.Contains(joined, "containers/acornfox-") {
		t.Errorf("应先停机、结束这台机器遗留的运行进程再重试：\n%s", joined)
	}
}

func TestVMLockSerializes(t *testing.T) {
	p := Paths{Root: t.TempDir()}
	unlock, err := LockVM(p)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan struct{})
	go func() {
		u2, err := LockVM(p)
		if err == nil {
			u2()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("锁被占着，另一个不该拿到")
	case <-time.After(500 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("放开以后另一个应当拿到")
	}
}

// The install source tag goes to the install script in the machine as --from, on a fresh install only
// and only when it is usable (it ends up in a shell command line).
func TestLinuxRunCommandPassesTheSourceTagOnlyOnInstall(t *testing.T) {
	inst := linuxRunCommand(false, "edge.tar.gz", " --china", "192.168.1.5", "bili-1201")
	if !strings.HasPrefix(inst, "bash ./install.sh ") || !strings.Contains(inst, " --mac-vm --public-host 192.168.1.5 --china") || !strings.HasSuffix(inst, " --from bili-1201") {
		t.Fatalf("install: %s", inst)
	}
	if got := linuxRunCommand(false, "edge.tar.gz", "", "127.0.0.1", ""); strings.Contains(got, "--from") {
		t.Fatalf("no tag, no --from: %s", got)
	}
	for _, bad := range []string{"Bad", "a b", "a;b", "$(x)", strings.Repeat("a", 33), "a\nb"} {
		if got := linuxRunCommand(false, "edge.tar.gz", "", "127.0.0.1", bad); strings.Contains(got, "--from") {
			t.Fatalf("bad tag %q must not reach the command line: %s", bad, got)
		}
	}
	if up := linuxRunCommand(true, "edge.tar.gz", "", "127.0.0.1", "bili-1201"); strings.Contains(up, "--from") || !strings.HasPrefix(up, "bash ./upgrade.sh ") {
		t.Fatalf("upgrade never takes the tag: %s", up)
	}
}
