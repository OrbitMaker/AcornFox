package appinstall

// Image archives per CPU architecture (catalog/arch.go): the image step
// fetches the archive of this machine's architecture through the gateway;
// with none for it, a public app comes from the public sources by digest and
// a private app is refused. The older single-archive form stands for amd64.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
	"github.com/acornfox/acornfox/internal/state"
)

// archGateway serves several archives by file id and records what was asked.
type archGateway struct {
	mu    sync.Mutex
	asked []string
	files map[string][]byte
	srv   *httptest.Server
}

func newArchGateway(t *testing.T, files map[string][]byte) *archGateway {
	t.Helper()
	g := &archGateway{files: files}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := g.files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *archGateway) Issue(ctx context.Context, fileID, purpose string) (gatewayclient.Issued, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asked = append(g.asked, fileID)
	body, ok := g.files[fileID]
	if purpose != "app" || !ok {
		return gatewayclient.Issued{}, errors.New("GW-FILE-01")
	}
	return gatewayclient.Issued{FileID: fileID, URL: g.srv.URL + "/" + fileID, SHA256: sha256hex(body), Size: int64(len(body))}, nil
}

func (g *archGateway) askedFor() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.asked...)
}

var (
	amd64Archive = []byte("amd64 image archive")
	arm64Archive = []byte("arm64 image archive, a different file")
)

// perArch is the per-architecture form with an archive for each of archs.
func perArch(archs ...string) map[string]catalog.ArchArtifact {
	out := map[string]catalog.ArchArtifact{}
	for _, a := range archs {
		body := map[string][]byte{"amd64": amd64Archive, "arm64": arm64Archive}[a]
		out[a] = catalog.ArchArtifact{File: "hermes-" + a, SHA256: sha256hex(body), Size: int64(len(body))}
	}
	return out
}

// toPerArch rewrites the image to the per-architecture form.
func toPerArch(archs ...string) func(*catalog.Image) {
	return func(img *catalog.Image) {
		img.Artifact, img.ArtifactSHA256, img.ArtifactSize = "", "", 0
		img.Artifacts = perArch(archs...)
	}
}

// archImageRun returns a run of the image step for the public Hermes on a
// machine of arch, its manifest's image changed by mod, with a gateway that
// serves hermes-amd64, hermes-arm64 and (for the older form) a legacy amd64
// archive. The task has its image step running, so its message is visible.
func archImageRun(t *testing.T, arch string, mod func(*catalog.Image)) (*env, *archGateway, *taskRun, func() (string, string, error)) {
	t.Helper()
	v := setup(t)
	gw := newArchGateway(t, map[string][]byte{"hermes-amd64": amd64Archive, "hermes-arm64": arm64Archive, "hermes-agent-v0.23.0-image": amd64Archive})
	v.e.cfg.Gateway, v.e.cfg.Arch = gw, arch
	a, err := v.e.cfg.Catalog.App(context.Background(), "hermes-agent")
	if err != nil {
		t.Fatal(err)
	}
	m := *a.Manifest
	m.Release.Images = append(m.Release.Images[:0:0], m.Release.Images...)
	// The published Hermes, older form, pinned to the legacy archive.
	m.Release.Images[0].ArtifactSHA256, m.Release.Images[0].ArtifactSize = sha256hex(amd64Archive), int64(len(amd64Archive))
	if mod != nil {
		mod(&m.Release.Images[0])
	}
	app := *a
	app.Manifest = &m
	p := &planned{app: &app, name: "hermes-agent"}
	tr := &taskRun{e: v.e, id: "0123456789cd", view: TaskView{ID: "0123456789cd", Status: state.TaskRunning,
		Apps:  []AppResult{{App: "hermes-agent"}},
		Steps: []StepView{{App: "hermes-agent", Name: "image", Label: "下载镜像", Status: StepRunning}},
	}}
	if _, err := v.w.st.CreateInstallTask(context.Background(), state.InstallTask{ID: tr.id, Kind: "app", Target: "hermes-agent", Status: state.TaskRunning, Data: "{}"}); err != nil {
		t.Fatal(err)
	}
	return v, gw, tr, func() (string, string, error) { return tr.image(context.Background(), p, v.w) }
}

// The machine's architecture picks its archive: amd64 gets the amd64 file,
// arm64 the arm64 one; nothing is pulled from public sources.
func TestArchArchiveOfThisMachine(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			v, gw, _, run := archImageRun(t, arch, toPerArch("amd64", "arm64"))
			_, msg, err := run()
			if err != nil || msg != "从下载网关下载" {
				t.Fatalf("msg=%s err=%v", msg, err)
			}
			if got := gw.askedFor(); len(got) != 1 || got[0] != "hermes-"+arch {
				t.Fatalf("asked the gateway for %v, want hermes-%s", got, arch)
			}
			if len(v.w.loads) != 1 || len(v.w.pulled) != 0 {
				t.Fatalf("loads=%d pulled=%v", len(v.w.loads), v.w.pulled)
			}
		})
	}
}

// An arm64 machine and a manifest with only the amd64 archive: the gateway
// is not asked at all, the image comes from the public sources by digest,
// and both the progress and the result say why.
func TestArchNoArchiveForThisMachineUsesPublicSources(t *testing.T) {
	v, gw, tr, run := archImageRun(t, "arm64", toPerArch("amd64"))
	_, msg, err := run()
	if err != nil {
		t.Fatal(err)
	}
	if got := gw.askedFor(); len(got) != 0 {
		t.Fatalf("the gateway must not be asked: %v", got)
	}
	if len(v.w.loads) != 0 || len(v.w.pulled) != 1 || !strings.HasPrefix(v.w.pulled[0], "nousresearch/hermes-agent@sha256:") {
		t.Fatalf("loads=%d pulled=%v", len(v.w.loads), v.w.pulled)
	}
	const why = "清单里只有 linux/amd64 的镜像包，没有 linux/arm64 的"
	if msg != "从公共源拉取（下载网关："+why+"，不走网关）" {
		t.Fatalf("result %q", msg)
	}
	if s := tr.snapshot().Steps[0]; s.Message != why+"，从公共源拉取…" || s.Progress != nil {
		t.Fatalf("progress %+v", s)
	}
}

// The older single-archive form stands for amd64: an amd64 machine fetches
// it through the gateway as before, an arm64 machine (which would load an
// amd64 image) takes the public sources.
func TestArchOlderFormIsAmd64Only(t *testing.T) {
	v, gw, _, run := archImageRun(t, "amd64", nil)
	if _, msg, err := run(); err != nil || msg != "从下载网关下载" || len(v.w.loads) != 1 {
		t.Fatalf("amd64: msg=%s err=%v loads=%d", msg, err, len(v.w.loads))
	}
	if got := gw.askedFor(); len(got) != 1 || got[0] != "hermes-agent-v0.23.0-image" {
		t.Fatalf("amd64 asked %v", got)
	}

	v, gw, tr, run := archImageRun(t, "arm64", nil)
	_, msg, err := run()
	if err != nil || len(gw.askedFor()) != 0 || len(v.w.loads) != 0 || len(v.w.pulled) != 1 {
		t.Fatalf("arm64: msg=%s err=%v asked=%v loads=%d pulled=%v", msg, err, gw.askedFor(), len(v.w.loads), v.w.pulled)
	}
	const why = "清单里的镜像包是旧写法，只算 linux/amd64 的，这台是 linux/arm64"
	if msg != "从公共源拉取（下载网关："+why+"，不走网关）" || tr.snapshot().Steps[0].Message != why+"，从公共源拉取…" {
		t.Fatalf("arm64: result %q, progress %q", msg, tr.snapshot().Steps[0].Message)
	}
}

// A local image with the pinned digest but built for another architecture
// (an amd64 archive an older version loaded on this arm64 machine) is not
// "already here": the adopt request names this machine's architecture, and
// the image is fetched as if it were missing (here: the older form on arm64,
// so the public sources). An image of this architecture, or one that does
// not say, is still taken without any download.
func TestArchLocalImageOfAnotherArchNotAdopted(t *testing.T) {
	v, gw, _, run := archImageRun(t, "arm64", nil)
	v.w.adopted[hermesDigestForTest] = true
	v.w.localArch = map[string]string{hermesDigestForTest: "amd64"}
	_, msg, err := run()
	if err != nil || msg == "本机已有这个镜像" || len(v.w.pulled) != 1 || len(gw.askedFor()) != 0 || len(v.w.loads) != 0 {
		t.Fatalf("amd64 image on arm64: msg=%s err=%v pulled=%v asked=%v loads=%d", msg, err, v.w.pulled, gw.askedFor(), len(v.w.loads))
	}
	if got := v.w.adoptArchs; len(got) != 1 || got[0] != "arm64" {
		t.Fatalf("adopt asked with arch %v", got)
	}
	for _, have := range []string{"arm64", ""} {
		v, gw, _, run := archImageRun(t, "arm64", nil)
		v.w.adopted[hermesDigestForTest] = true
		v.w.localArch = map[string]string{hermesDigestForTest: have}
		if _, msg, err := run(); err != nil || msg != "本机已有这个镜像" || len(v.w.pulled) != 0 || len(gw.askedFor()) != 0 {
			t.Fatalf("local %q image: msg=%s err=%v pulled=%v", have, msg, err, v.w.pulled)
		}
	}
}

// The older form without a checksum keeps its old wording on every
// architecture, and the progress is left alone (as before).
func TestArchOlderFormWithoutChecksumUnchanged(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		v, gw, tr, run := archImageRun(t, arch, func(img *catalog.Image) { img.ArtifactSHA256, img.ArtifactSize = "", 0 })
		_, msg, err := run()
		if err != nil || msg != "从公共源拉取（下载网关：清单没有镜像包的校验值，不走网关）" || len(gw.askedFor()) != 0 || len(v.w.pulled) != 1 {
			t.Fatalf("%s: %s %v asked %v", arch, msg, err, gw.askedFor())
		}
		if s := tr.snapshot().Steps[0]; s.Message != "" {
			t.Fatalf("%s: progress %q", arch, s.Message)
		}
	}
}

// Without a gateway nothing changes: the public sources, no note in the
// result. While pulling, the step says it is pulling (no progress to show).
func TestArchWithoutGatewaySilent(t *testing.T) {
	v, _, tr, run := archImageRun(t, "arm64", toPerArch("amd64"))
	v.e.cfg.Gateway = nil
	if _, msg, err := run(); err != nil || msg != "从公共源拉取" || len(v.w.pulled) != 1 {
		t.Fatalf("%s %v", msg, err)
	}
	if s := tr.snapshot().Steps[0]; s.Message != publicPullNote || s.Progress != nil {
		t.Fatalf("progress %+v", s)
	}
}

// When the per-architecture form is written next to the older one (lint
// refuses that), the per-architecture one decides.
func TestArchBothFormsPerArchWins(t *testing.T) {
	_, gw, _, run := archImageRun(t, "amd64", func(img *catalog.Image) { img.Artifacts = perArch("amd64") })
	if _, msg, err := run(); err != nil || msg != "从下载网关下载" {
		t.Fatalf("%s %v", msg, err)
	}
	if got := gw.askedFor(); len(got) != 1 || got[0] != "hermes-amd64" {
		t.Fatalf("asked %v", got)
	}
}

// A private catalog's app without an archive for this machine's architecture
// is refused (CAT-INST-26): no public pull, no gateway request for another
// architecture's file.
func TestArchPrivateWithoutArchiveRefused(t *testing.T) {
	for name, mod := range map[string]func(*catalog.Image){
		"per-arch, amd64 only": toPerArch("amd64"),
		"older form":           nil,
	} {
		t.Run(name, func(t *testing.T) {
			v, g, run := privateImageRunWith(t, mod)
			v.e.cfg.Arch = "arm64"
			_, _, err := run()
			f, ok := err.(*Fault)
			if !ok || f.Code != CodePrivateDenied || !strings.Contains(f.Message, "linux/arm64") {
				t.Fatalf("got %v", err)
			}
			if len(v.w.pulled) != 0 || len(v.w.loads) != 0 {
				t.Fatalf("pulled=%v loads=%d", v.w.pulled, len(v.w.loads))
			}
			g.mu.Lock()
			defer g.mu.Unlock()
			for _, r := range g.issueReq {
				if fid, _ := r["file_id"].(string); !strings.HasPrefix(fid, "catpkg-") {
					t.Fatalf("the gateway was asked for %v", r)
				}
			}
		})
	}
}

// A private app with an archive for this machine's architecture goes through
// the catalog's gateway as before.
func TestArchPrivatePerArchThroughGateway(t *testing.T) {
	const fid = "hermes-agent-v0.23.0-image"
	v, g, run := privateImageRunWith(t, func(img *catalog.Image) {
		img.Artifacts = map[string]catalog.ArchArtifact{"arm64": {File: fid, SHA256: img.ArtifactSHA256, Size: img.ArtifactSize}}
		img.Artifact, img.ArtifactSHA256, img.ArtifactSize = "", "", 0
	})
	v.e.cfg.Arch = "arm64"
	if _, msg, err := run(); err != nil || msg != "从下载网关下载" {
		t.Fatalf("%s %v", msg, err)
	}
	if req := lastIssue(g, fid); req == nil {
		t.Fatal("the private archive was not asked for")
	}
	if len(v.w.loads) != 1 || len(v.w.pulled) != 0 {
		t.Fatalf("loads=%d pulled=%v", len(v.w.loads), v.w.pulled)
	}
}

// perArchHermes is the published Hermes manifest in the per-architecture
// form: an archive for each of archs (sizes as given, else the test bodies),
// min_platform 0.5.1.
func perArchHermes(t *testing.T, sizes map[string]int64, archs ...string) string {
	t.Helper()
	m := withMinPlatform(t, publishedManifest(t, "hermes-agent"), "0.5.1")
	const old = "      artifact: hermes-agent-v0.23.0-image\n      artifact_sha256: d356bba314af272039743d82123bf57a38e67696d69558a6871c4b999700df45\n      artifact_size: 1120227840\n"
	if !strings.Contains(m, old) {
		t.Fatal("the published Hermes manifest no longer has the archive lines to rewrite")
	}
	var b strings.Builder
	b.WriteString("      artifacts:\n")
	for arch, a := range perArch(archs...) {
		if n, ok := sizes[arch]; ok {
			a.Size = n
		}
		b.WriteString("        " + arch + ": { file: " + a.File + ", sha256: " + a.SHA256 + ", size: " + strconv.FormatInt(a.Size, 10) + " }\n")
	}
	return strings.Replace(m, old, b.String(), 1)
}

var archOwnLLM = map[string]any{"llm": map[string]any{"source": "own", "base_url": "https://api.example.com/v1", "api_key": "sk-test", "model": "m"}}

func imageStep(tv TaskView) StepView {
	for _, s := range tv.Steps {
		if s.Name == "image" {
			return s
		}
	}
	return StepView{}
}

// A whole install from a catalog whose Hermes uses the per-architecture
// form: the strict check accepts it, amd64 loads the amd64 archive from the
// gateway, arm64 (no archive) pulls from the public sources and says why.
func TestArchInstallPerArchManifest(t *testing.T) {
	snap := bundleFS(fixtureBundle(t, 50, catalog.PublicCatalog, []fixApp{{manifest: perArchHermes(t, nil, "amd64")}}, nil, nil))
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			gw := newArchGateway(t, map[string][]byte{"hermes-amd64": amd64Archive})
			v, _ := setupSnapshot(t, snap, func(c *Config) { c.Version, c.Arch, c.Gateway = "0.5.1", arch, gw })
			tv := v.install(t, Request{App: "hermes-agent", Params: archOwnLLM})
			if tv.Status != state.TaskSucceeded {
				t.Fatalf("install: %+v", tv.Fault)
			}
			s := imageStep(tv)
			switch arch {
			case "amd64":
				if got := gw.askedFor(); len(got) != 1 || got[0] != "hermes-amd64" || len(v.w.loads) != 1 || len(v.w.pulled) != 0 || s.Message != "从下载网关下载" {
					t.Fatalf("asked %v loads %d pulled %v step %q", got, len(v.w.loads), v.w.pulled, s.Message)
				}
			case "arm64":
				if got := gw.askedFor(); len(got) != 0 || len(v.w.loads) != 0 || len(v.w.pulled) != 1 ||
					s.Message != "从公共源拉取（下载网关：清单里只有 linux/amd64 的镜像包，没有 linux/arm64 的，不走网关）" {
					t.Fatalf("asked %v loads %d pulled %v step %q", got, len(v.w.loads), v.w.pulled, s.Message)
				}
			}
		})
	}
}

// A platform older than 0.5.1 does not install a manifest in the new form:
// the index says 0.5.1 (CAT-INST-08), before anything is downloaded.
func TestArchPerArchManifestNeedsNewerPlatform(t *testing.T) {
	snap := bundleFS(fixtureBundle(t, 51, catalog.PublicCatalog, []fixApp{{manifest: perArchHermes(t, nil, "amd64")}}, nil, nil))
	v, hits := setupSnapshot(t, snap, func(c *Config) { c.Version = "0.5.0" })
	_, err := v.e.StartInstall(context.Background(), Request{App: "hermes-agent", Params: archOwnLLM})
	if f := faultOf(t, err); f.Code != CodeTooOld || f.NeedsPlatform != "0.5.1" {
		t.Fatalf("fault %+v", f)
	}
	if hits.Load() != 0 {
		t.Fatal("asked for something before the version check")
	}
	nothingCreated(t, v)
}

// The disk check before an install counts only the archive this machine
// downloads: a huge amd64 archive refuses the install on amd64 (CAT-INST-27)
// and does not count on arm64, which downloads none.
func TestArchDiskCountsOnlyThisMachinesArchive(t *testing.T) {
	snap := bundleFS(fixtureBundle(t, 52, catalog.PublicCatalog, []fixApp{{manifest: perArchHermes(t, map[string]int64{"amd64": 50 << 30}, "amd64")}}, nil, nil))
	disk := func(context.Context) (DiskSpace, error) {
		return DiskSpace{DockerFree: 100 << 30, DockerPath: "/var/lib/docker", UploadFree: 10 << 30, UploadPath: "/data/uploads"}, nil
	}
	for arch, wantRefused := range map[string]bool{"amd64": true, "arm64": false} {
		t.Run(arch, func(t *testing.T) {
			gw := newArchGateway(t, nil)
			v, _ := setupSnapshot(t, snap, func(c *Config) { c.Version, c.Arch, c.Gateway, c.DiskFree = "0.5.1", arch, gw, disk })
			tv, err := v.e.StartInstall(context.Background(), Request{App: "hermes-agent", Params: archOwnLLM})
			if wantRefused {
				if f := faultOf(t, err); f.Code != CodeNoDisk || !strings.Contains(f.Message, "镜像包") {
					t.Fatalf("fault %+v", f)
				}
				nothingCreated(t, v)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tv = v.wait(t, tv.ID); tv.Status != state.TaskSucceeded {
				t.Fatalf("install: %+v", tv.Fault)
			}
		})
	}
}
