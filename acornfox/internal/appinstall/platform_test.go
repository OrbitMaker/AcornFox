package appinstall

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

const hermesDigestForTest = "sha256:adb7890916de63245523492be8f68f9d2095fecbec4e2a6dab7960381f0fc002"

func envMap(t *testing.T, v *env, app string) map[string]string {
	t.Helper()
	envs, err := v.w.st.ListEnv(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range envs {
		out[e.Key] = e.Value
	}
	return out
}

// In China on Aliyun: the apps get the machine's time zone, Aliyun's public
// mirrors and the platform's default model, without the user filling any.
func TestPlatformVarsFillApps(t *testing.T) {
	v := setupWith(t, func(c *Config) {
		c.Platform = PlatformEnv{China: true, Cloud: "aliyun", TZ: "Asia/Tokyo", ChatModel: "qwen-plus"}
	})
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("new-api %+v", tv.Fault)
	}
	if got := envMap(t, v, "new-api")["TZ"]; got != "Asia/Tokyo" {
		t.Fatalf("new-api TZ %q", got)
	}
	v.w.adopted[hermesDigestForTest] = true
	tv := v.install(t, Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{"source": "local_gateway"}}})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("hermes %+v", tv.Fault)
	}
	env := envMap(t, v, "hermes-agent")
	want := map[string]string{
		"TZ":                  "Asia/Tokyo",
		"PIP_INDEX_URL":       "https://mirrors.aliyun.com/pypi/simple/",
		"UV_DEFAULT_INDEX":    "https://mirrors.aliyun.com/pypi/simple/",
		"npm_config_registry": "https://registry.npmmirror.com/",
	}
	for k, w := range want {
		if env[k] != w {
			t.Fatalf("%s = %q, want %q", k, env[k], w)
		}
	}
	cfg := string(v.w.wrote[0].Files[0].Content)
	if !strings.Contains(cfg, `default: "qwen-plus"`) {
		t.Fatalf("default model not from the platform:\n%s", cfg)
	}
	// the user's own model still wins over the platform default
	v2 := setupWith(t, func(c *Config) { c.Platform = PlatformEnv{ChatModel: "qwen-plus"} })
	if tv := v2.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	v2.w.adopted[hermesDigestForTest] = true
	if tv := v2.install(t, Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{"source": "local_gateway", "model": "glm-4"}}}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if cfg := string(v2.w.wrote[0].Files[0].Content); !strings.Contains(cfg, `default: "glm-4"`) {
		t.Fatalf("user model lost:\n%s", cfg)
	}
}

// Outside China (and when this machine names no time zone): official
// registries and UTC. In China off the known clouds: Huawei's mirrors.
func TestPlatformDefaults(t *testing.T) {
	e := &Engine{cfg: Config{}}
	b := e.platformBase()
	if b["region"] != "global" || b["tz"] != "UTC" || b["mirror.pypi"] != officialPyPI || b["mirror.npm"] != officialNPM || b["llm.model.chat"] != DefaultGatewayModel {
		t.Fatalf("global %v", b)
	}
	e.cfg.Platform = PlatformEnv{China: true, Cloud: "volcengine", TZ: "../../etc/passwd"}
	b = e.platformBase()
	if b["region"] != "cn" || !strings.Contains(b["mirror.pypi"], "huaweicloud") || !strings.Contains(b["mirror.npm"], "huaweicloud") || b["tz"] != "UTC" {
		t.Fatalf("cn %v", b)
	}
	if _, ok := b["llm.api_key"]; ok {
		t.Fatal("the platform key must not have a value on the standalone edition yet")
	}
}

// The user changes what may be changed (CAT-INST-28 otherwise), for this
// install only.
func TestPlatformSets(t *testing.T) {
	v := setupWith(t, func(c *Config) { c.Platform = PlatformEnv{China: true, TZ: "Asia/Shanghai"} })
	tv := v.install(t, Request{App: "new-api", Platform: map[string]string{"tz": "Europe/Berlin", "mirror.npm": "https://npm.example.com/"}})
	if tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if got := envMap(t, v, "new-api")["TZ"]; got != "Europe/Berlin" {
		t.Fatalf("TZ %q", got)
	}
	ctx := context.Background()
	bad := []map[string]string{
		{"timezone": "Asia/Tokyo"},               // not in the table
		{"region": "global"},                     // the platform decides
		{"app.url": "https://evil.example"},      // the platform decides
		{"llm.api_key": "sk-x"},                  // the platform decides
		{"mirror.pypi": "ftp://m.example/"},      // not http(s)
		{"mirror.pypi": "https://u:p@m.example"}, // credentials in the address
		{"tz": "../../etc/passwd"},               // not a zone name
		{"tz": "Asia/Tokyo\nX=1"},                // newline
		{"llm.model.chat": "a b"},                // not a model name
		{"tz": "  "},                             // empty
	}
	for _, sets := range bad {
		_, err := v.e.StartInstall(ctx, Request{App: "new-api", Name: "other", Platform: sets})
		if code(err) != CodeBadPlatform {
			t.Errorf("%v: %v", sets, err)
		}
	}
	if _, err := v.w.st.GetApp(ctx, "other"); !errors.Is(err, state.ErrNotFound) {
		t.Fatal("a refused request created an app")
	}
}

// app.url is the address the app opens at, saved with the parameters.
func TestPlatformAppURL(t *testing.T) {
	v := setup(t)
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	ctx := context.Background()
	a, _ := v.w.st.GetApp(ctx, "new-api")
	saved, err := v.e.savedParams(ctx, "new-api")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Values(saved).lookup("platform.app.url")
	if err != nil || got == "" || !strings.HasSuffix(got, ":"+strconv.Itoa(a.PublicPort)) {
		t.Fatalf("app.url %q %v", got, err)
	}
	if _, err := Values(saved).lookup("platform.llm.api_key"); err == nil {
		t.Fatal("platform.llm.api_key resolved directly")
	}
}

// Not enough disk for disk_mb.min is refused before anything is created
// (CAT-INST-27); when the free space cannot be read the install goes on.
func TestDiskCheck(t *testing.T) {
	space := func(ds DiskSpace) func(*Config) {
		return func(c *Config) { c.DiskFree = func(context.Context) (DiskSpace, error) { return ds, nil } }
	}
	ctx := context.Background()
	v := setupWith(t, space(DiskSpace{DockerFree: 100 << 20, DockerPath: "/var/lib/docker", Same: true}))
	_, err := v.e.StartInstall(ctx, Request{App: "new-api"})
	if code(err) != CodeNoDisk || !strings.Contains(err.Error(), "512 MB") || !strings.Contains(err.Error(), "100 MB") {
		t.Fatalf("got %v", err)
	}
	if _, err := v.w.st.GetApp(ctx, "new-api"); !errors.Is(err, state.ErrNotFound) {
		t.Fatal("refused install created the app")
	}
	// a recipe adds up its apps: 512 + 6144 MB
	v2 := setupWith(t, space(DiskSpace{DockerFree: 6000 << 20, DockerPath: "/var/lib/docker", Same: true}))
	if _, err := v2.e.StartInstall(ctx, Request{Recipe: "hermes-with-new-api"}); code(err) != CodeNoDisk || !strings.Contains(err.Error(), "6656 MB") {
		t.Fatalf("recipe: %v", err)
	}
	v3 := setupWith(t, func(c *Config) {
		c.DiskFree = func(context.Context) (DiskSpace, error) { return DiskSpace{}, errors.New("statfs failed") }
	})
	if tv := v3.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("unknown free space should not block: %+v", tv.Fault)
	}
	// Docker on a big data disk, the upload directory on a small system
	// disk: New API downloads no archive, so it installs.
	split := DiskSpace{DockerFree: 80 << 30, DockerPath: "/data/docker", UploadFree: 400 << 20, UploadPath: "/var/lib/acornfox/uploads"}
	v4 := setupWith(t, space(split))
	if tv := v4.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatalf("split disks: %+v", tv.Fault)
	}
}

// With a download gateway the upload partition must hold Hermes' image
// archive (1.12 GB), but not the whole disk_mb.min.
func TestDiskCheckArchive(t *testing.T) {
	ctx := context.Background()
	mk := func(upload uint64) *env {
		return setupWith(t, func(c *Config) {
			c.Gateway = &fakeGateway{}
			c.DiskFree = func(context.Context) (DiskSpace, error) {
				return DiskSpace{DockerFree: 80 << 30, DockerPath: "/data/docker", UploadFree: upload, UploadPath: "/var/lib/acornfox/uploads"}, nil
			}
		})
	}
	v := mk(400 << 20)
	if tv := v.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	_, err := v.e.StartInstall(ctx, Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{"source": "local_gateway"}}})
	if code(err) != CodeNoDisk || !strings.Contains(err.Error(), "1069 MB") || !strings.Contains(err.Error(), "uploads") {
		t.Fatalf("archive: %v", err)
	}
	// 2 GB on the system disk is enough for the archive.
	v2 := mk(2 << 30)
	if tv := v2.install(t, Request{App: "new-api"}); tv.Status != state.TaskSucceeded {
		t.Fatal(tv.Fault)
	}
	if _, err := v2.e.StartInstall(ctx, Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{"source": "local_gateway"}}}); code(err) == CodeNoDisk {
		t.Fatalf("2 GB upload partition refused: %v", err)
	}
}

// A manifest using a platform variable this binary does not have (from a
// newer catalog) is refused before anything is created (CAT-INST-23).
func TestUnknownPlatformVarRefusedEarly(t *testing.T) {
	e := &Engine{cfg: Config{Version: "0.5.0", Arch: "amd64"}}
	base := func() *catalog.Manifest {
		return &catalog.Manifest{ID: "x", Lifecycle: catalog.Lifecycle{MinPlatform: "0.5.0"}, Resources: catalog.Resources{Platforms: []string{"linux/amd64"}},
			Release: catalog.Release{Images: []catalog.Image{{Name: "app"}}}, Endpoints: []catalog.Endpoint{{Name: "web", Port: 80, Primary: true}}}
	}
	cases := map[string]func(*catalog.Manifest){
		"env":  func(m *catalog.Manifest) { m.Env = []catalog.EnvVar{{Key: "HF_ENDPOINT", From: "platform.mirror.hf"}} },
		"file": func(m *catalog.Manifest) { m.Files = []catalog.File{{Template: "x: {{platform.locale}}"}} },
		"init": func(m *catalog.Manifest) {
			m.Init = []catalog.InitStep{{HTTP: catalog.InitHTTP{JSON: map[string]any{"a": "{{platform.owner.email}}"}}}}
		},
		"first_login": func(m *catalog.Manifest) {
			m.FirstLogin = []catalog.LoginField{{Label: "x", From: "platform.app.domain"}}
		},
		"via_param": func(m *catalog.Manifest) { m.Env = []catalog.EnvVar{{Key: "K", From: "platform.llm.api_key"}} },
	}
	for name, mod := range cases {
		m := base()
		mod(m)
		if err := e.checkManifest(&App{Manifest: m}); code(err) != CodeUnsupported {
			t.Errorf("%s: %v", name, err)
		}
	}
	m := base()
	m.Env = []catalog.EnvVar{{Key: "TZ", From: "platform.tz"}}
	if err := e.checkManifest(&App{Manifest: m}); err != nil {
		t.Fatalf("known variable refused: %v", err)
	}
}

func TestTimezoneMustExist(t *testing.T) {
	if err := checkPlatformSets(map[string]string{"tz": "Asia/Tokio"}); code(err) != CodeBadPlatform {
		t.Fatalf("typo accepted: %v", err)
	}
	for _, ok := range []string{"Asia/Tokyo", "UTC", "Etc/UTC", "America/Argentina/Buenos_Aires"} {
		if err := checkPlatformSets(map[string]string{"tz": ok}); err != nil {
			t.Fatalf("%s: %v", ok, err)
		}
	}
	e := &Engine{cfg: Config{Platform: PlatformEnv{TZ: "Asia/Tokio"}}}
	if got := e.platformBase()["tz"]; got != "UTC" {
		t.Fatalf("unknown host zone kept: %s", got)
	}
}

// The detail lists the platform variables an app uses, with this machine's
// values; the full list marks the platform key as not available yet.
func TestPlatformViews(t *testing.T) {
	v := setupWith(t, func(c *Config) { c.Platform = PlatformEnv{China: true, TZ: "Asia/Shanghai"} })
	ctx := context.Background()
	d, err := v.e.Detail(ctx, "hermes-agent")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]PlatformVarView{}
	names := []string{}
	for _, p := range d.Platform {
		got[p.Name] = p
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "llm.model.chat,mirror.npm,mirror.pypi,tz" {
		t.Fatalf("hermes uses %v", names)
	}
	if got["tz"].Value != "Asia/Shanghai" || !got["tz"].UserCanSet || !strings.Contains(got["mirror.pypi"].Value, "huaweicloud") {
		t.Fatalf("views %+v", got)
	}
	all := v.e.PlatformVars(ctx)
	if len(all) != len(catalog.PlatformVars) {
		t.Fatalf("%d vars", len(all))
	}
	for _, p := range all {
		switch p.Name {
		case "llm.api_key", "llm.base_url":
			if p.Available || p.Value != "" {
				t.Fatalf("%s should not be available: %+v", p.Name, p)
			}
		case "region":
			if p.Value != "cn" || p.UserCanSet {
				t.Fatalf("region %+v", p)
			}
		}
	}
}
