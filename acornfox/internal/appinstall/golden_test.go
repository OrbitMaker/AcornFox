package appinstall

// Golden samples of what the engine writes into apps.
//
// The files under testdata/golden/ record, for the test material written
// the current way (internal/catalog/testdata/good: an app that provides a
// model API and issues a token per consumer, an app that needs one, the
// recipe that wires them), what an install with fixed inputs produces: the
// app's settings, every environment variable, every config file with its
// content, mode and owner, the body of every init request, the credential
// kept for each consumer (which keys, never the values) and the first-login
// fields. The test compares them byte for byte.
//
// If this test fails, the engine's output changed: find out why before
// refreshing the samples. Refresh them only when the change is intended (a
// changed escape in a config file or in a set-up request locks users out),
// and say so in the commit:
//
//	go test ./internal/appinstall -run TestGoldenApps -update
//
// What is fixed: the manifests, the request, the machine's platform
// settings, and the "random" passwords and keys (secretRand is a
// counter-mode SHA-256 stream; math/big's way of drawing from it is part of
// the sample, so a Go release that changes crypto/rand.Int would show up
// here as changed passwords and nothing else). The issue numbers are random
// but show nowhere in the samples (the fake app's token values do not
// depend on them).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata/golden from the engine's current output (read the note in golden_test.go first)")

// fixedRand is a deterministic byte stream: SHA-256 of "<seed>/<counter>".
type fixedRand struct {
	seed string
	n    int
	buf  []byte
}

func (r *fixedRand) Read(p []byte) (int, error) {
	for i := range p {
		if len(r.buf) == 0 {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", r.seed, r.n)))
			r.n++
			r.buf = sum[:]
		}
		p[i] = r.buf[0]
		r.buf = r.buf[1:]
	}
	return len(p), nil
}

// formerManifest reads a manifest of the catalog published in the former
// format (schema 2: a setting of the built-in model kind, a provides entry
// whose steps were in platform code), kept as it was published in
// testdata/former. This platform refuses to install it (CAT-INST-36).
func formerManifest(t testing.TB, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "former", id+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type goldenDoc struct {
	Scenario string      `json:"scenario"`
	Request  Request     `json:"request"`
	Platform PlatformEnv `json:"platform"`
	Apps     []goldenApp `json:"apps"`
}

type goldenApp struct {
	App       string `json:"app"`
	CatalogID string `json:"catalog_id"`
	Version   string `json:"version"`
	Settings  struct {
		Port       int      `json:"port"`
		HealthPath string   `json:"health_path"`
		Exposure   string   `json:"exposure"`
		Command    []string `json:"command"`
		PidsLimit  int      `json:"pids_limit"`
		MemoryMB   int      `json:"memory_mb"`
		Links      []string `json:"links"`
	} `json:"settings"`
	Env        []goldenEnv  `json:"env"`
	Files      []goldenFile `json:"files"`
	Init       []goldenCall `json:"init"`
	FirstLogin FirstLogin   `json:"first_login"`
	// Credentials: per need, the keys of the credential kept for it (never
	// the values): only what is used later is kept.
	Credentials map[string][]string `json:"credentials,omitempty"`
}

type goldenEnv struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type goldenFile struct {
	MountPath string `json:"mount_path"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	UID       int    `json:"uid"`
	GID       int    `json:"gid"`
	Overwrite bool   `json:"overwrite"`
	Content   string `json:"content"`
}

type goldenCall struct {
	Method      string `json:"method"`
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	Body        string `json:"body"`
}

// initRecorder keeps the raw body of every request to one of paths.
type initRecorder struct {
	mu    sync.Mutex
	paths map[string]bool
	calls []goldenCall
}

func (r *initRecorder) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.paths[req.URL.Path] {
			body, _ := io.ReadAll(req.Body)
			req.Body = io.NopCloser(bytes.NewReader(body))
			r.mu.Lock()
			r.calls = append(r.calls, goldenCall{Method: req.Method, Path: req.URL.Path, ContentType: req.Header.Get("Content-Type"), Body: string(body)})
			r.mu.Unlock()
		}
		next.ServeHTTP(w, req)
	})
}

func TestGoldenApps(t *testing.T) {
	snap := bundleFS(goodBundle(t))
	scenarios := []struct {
		name     string
		platform PlatformEnv
		req      Request
		// service is a service the user registered before the install.
		service *AddServiceRequest
	}{
		{
			// Nothing given: defaults, generated secrets, a server in China
			// on Aliyun.
			name:     "new-api-defaults",
			platform: PlatformEnv{China: true, Cloud: "aliyun", TZ: "Asia/Shanghai"},
			req:      Request{App: "new-api"},
		},
		{
			// The user's own admin name and a password full of characters
			// that an escape would change: the setup request must carry it
			// as typed, or the user could not log in with it.
			name:     "new-api-user-values",
			platform: PlatformEnv{},
			req: Request{App: "new-api", Name: "gateway",
				Params:   map[string]any{"admin_username": "root_1", "admin_password": `p"a\ss'w{{params.admin_username}}<&>中文`},
				Platform: map[string]string{"tz": "Europe/Berlin"}},
		},
		{
			// The consumer plugged into the user's own registered model API;
			// the key and the model (the consumer's own setting: the
			// service's model is not what it uses) hold a quote and a
			// backslash, which config.yaml must escape exactly once inside
			// the template's own quotes.
			name:     "hermes-own-service",
			platform: PlatformEnv{China: true, Cloud: "tencent", TZ: "Asia/Tokyo"},
			req:      Request{App: "hermes-agent", Params: map[string]any{"model": `qwen-max:"v1"`}},
			service: &AddServiceRequest{Name: "my-upstream", Kind: "llm_gateway", Values: map[string]string{
				"base_url": "https://api.example.com/v1", "api_key": `sk-"quoted"\back`, "model": "not-this-one"}},
		},
		{
			// The recipe: the provider, then the consumer wired to it with a
			// token issued in it; the model is the one the user gave the
			// consumer.
			name:     "recipe-hermes-with-new-api",
			platform: PlatformEnv{China: true, TZ: "Asia/Shanghai"},
			req:      Request{Recipe: "hermes-with-new-api", Params: map[string]any{"hermes-agent.model": "qwen-plus"}},
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			saved := secretRand
			secretRand = &fixedRand{seed: sc.name}
			t.Cleanup(func() { secretRand = saved })

			// The server is reached at server.example and the apps are opened
			// to the LAN: their address is http://server.example:<port>.
			platform := sc.platform
			platform.Host.PublicHost = "server.example"
			v, hits := setupSnapshot(t, snap, func(c *Config) { c.Platform = platform })
			v.e.SetHooks(Hooks{Reserve: v.w.reserve, Delete: v.w.delete, WithApp: v.w.withApp, Kick: v.w.kick,
				Where: func(ctx context.Context, a state.App) platformvar.Where {
					return platformvar.Where{Exposure: state.ExposureLAN, Port: a.PublicPort}
				}})
			ctx := context.Background()
			if sc.service != nil {
				if _, err := v.e.AddService(ctx, *sc.service); err != nil {
					t.Fatal(err)
				}
			}

			// Record the manifest's init requests as they arrive at the app.
			recs := map[string]*initRecorder{}
			for _, id := range []string{"new-api"} {
				a, err := v.e.Catalog().App(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				rec := &initRecorder{paths: map[string]bool{}}
				for _, st := range a.Manifest.Init {
					rec.paths[st.HTTP.Path] = true
				}
				srv := httptest.NewServer(rec.wrap(v.tokens.handler()))
				t.Cleanup(srv.Close)
				v.w.mu.Lock()
				v.w.servers[id] = srv
				v.w.mu.Unlock()
				recs[id] = rec
			}

			tv := v.install(t, sc.req)
			if tv.Status != state.TaskSucceeded {
				t.Fatalf("install failed: %+v", tv.Fault)
			}
			if hits.Load() != 0 {
				t.Fatalf("the install went to the catalog source %d times", hits.Load())
			}

			doc := goldenDoc{Scenario: sc.name, Request: sc.req, Platform: platform}
			for _, r := range tv.Apps {
				a, err := v.w.st.GetApp(ctx, r.App)
				if err != nil {
					t.Fatal(err)
				}
				g := goldenApp{App: r.App, CatalogID: a.CatalogID, Version: a.CatalogVersion, Env: []goldenEnv{}, Files: []goldenFile{}, Init: []goldenCall{}}
				g.Settings.Port, g.Settings.HealthPath, g.Settings.Exposure = a.Port, a.HealthPath, a.Exposure
				g.Settings.Command, g.Settings.PidsLimit, g.Settings.MemoryMB, g.Settings.Links = a.Command, a.PidsLimit, a.MemoryMB, a.Links
				if g.Settings.Command == nil {
					g.Settings.Command = []string{}
				}
				if g.Settings.Links == nil {
					g.Settings.Links = []string{}
				}
				envs, err := v.w.st.ListEnv(ctx, r.App)
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range envs {
					g.Env = append(g.Env, goldenEnv{Key: e.Key, Value: e.Value, Secret: e.Secret})
				}
				sort.Slice(g.Env, func(i, j int) bool { return g.Env[i].Key < g.Env[j].Key })
				v.w.mu.Lock()
				for _, wr := range v.w.wrote {
					if wr.App != r.App {
						continue
					}
					for _, f := range wr.Files {
						g.Files = append(g.Files, goldenFile{MountPath: wr.MountPath, Path: f.Path, Mode: fmt.Sprintf("%04o", f.Mode), UID: f.UID, GID: f.GID,
							Overwrite: f.Overwrite, Content: string(f.Content)})
					}
				}
				v.w.mu.Unlock()
				if rec := recs[a.CatalogID]; rec != nil {
					rec.mu.Lock()
					g.Init = append(g.Init, rec.calls...)
					rec.mu.Unlock()
				}
				fl, err := v.e.FirstLogin(ctx, r.App, true)
				if err != nil {
					t.Fatal(err)
				}
				g.FirstLogin = fl
				reg, err := v.e.Registry()
				if err != nil {
					t.Fatal(err)
				}
				bs, err := reg.ListBindings(ctx, state.BindingFilter{App: r.App})
				if err != nil {
					t.Fatal(err)
				}
				for _, b := range bs {
					if g.Credentials == nil {
						g.Credentials = map[string][]string{}
					}
					keys := []string{}
					for k := range b.Issued {
						keys = append(keys, k)
					}
					sort.Strings(keys)
					g.Credentials[b.UseName+" → "+b.Service] = keys
				}
				doc.Apps = append(doc.Apps, g)
			}

			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			enc.SetIndent("", "  ")
			if err := enc.Encode(doc); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "golden", sc.name+".json")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				t.Logf("rewrote %s", path)
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no golden sample (%v); see the note at the top of golden_test.go", err)
			}
			if !bytes.Equal(buf.Bytes(), want) {
				t.Fatalf("the engine's output for a published app changed (%s).\nThis is a breaking change for installed apps unless proven otherwise; see the note at the top of golden_test.go.\n--- got ---\n%s", path, buf.Bytes())
			}
		})
	}
}

// A manifest of the former format is refused before anything is created or
// downloaded (CAT-INST-36), by the plan as by the install and the catalog
// detail, whatever else is wrong or right with it; nothing of it is
// installed. The catalog list marks such an app, and a recipe holding one.
func TestFormerManifestsRefused(t *testing.T) {
	recipes := []catalog.Recipe{{ID: "both", Schema: 1, Title: "两个", Summary: "两个一起装", Apps: []catalog.RecipeApp{{App: "new-api"}, {App: "hermes-agent"}},
		Delivery: catalog.Delivery{Tier: "auto"}}}
	// The index says 0.5.0 (as it was published): refused without even
	// reading the package, which is not there.
	b := fixtureBundle(t, 1, "", []fixApp{{manifest: formerManifest(t, "new-api"), noPackage: true}, {manifest: formerManifest(t, "hermes-agent"), noPackage: true}}, recipes, nil)
	v, hits := setupSnapshot(t, bundleFS(b), nil)
	ctx := context.Background()
	isFormer := func(what string, err error) {
		t.Helper()
		var f *Fault
		if !errors.As(err, &f) || f.Code != CodeLegacyManifest || f.HTTPStatus() != http.StatusConflict || f.NeedsPlatform != "" ||
			!strings.Contains(f.Message, "旧格式") || !strings.Contains(f.Message, "按新格式重新上架") || strings.Contains(f.Message, "反馈") || f.Hint == "" {
			t.Fatalf("%s: %v", what, err)
		}
	}
	for _, id := range []string{"new-api", "hermes-agent"} {
		_, err := v.e.StartInstall(ctx, Request{App: id})
		isFormer(id, err)
		pv, err := v.e.Plan(ctx, Request{App: id})
		if err != nil || pv.CanInstall || len(pv.Problems) != 1 || pv.Problems[0].Code != CodeLegacyManifest {
			t.Fatalf("%s plan: %+v %v", id, pv, err)
		}
		_, err = v.e.Detail(ctx, id)
		isFormer(id+" detail", err)
	}
	_, err := v.e.StartInstall(ctx, Request{Recipe: "both"})
	isFormer("recipe", err)
	list, err := v.e.List(ctx)
	if err != nil || len(list.Apps) != 2 || len(list.Recipes) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	for _, a := range list.Apps {
		if !a.Former || a.Note != FormerNote || a.NeedsPlatform != "" {
			t.Fatalf("list app %+v", a)
		}
	}
	if r := list.Recipes[0]; !r.Former || r.Note != FormerNote {
		t.Fatalf("list recipe %+v", r)
	}
	if hits.Load() != 0 {
		t.Fatalf("asked the catalog source %d times", hits.Load())
	}
	nothingCreated(t, v)

	// An index that claims the current platform but a package whose
	// manifest is of the former format: refused once the package is read,
	// before its schema is checked (not "the catalog is broken").
	b = fixtureBundle(t, 1, "", []fixApp{{manifest: formerManifest(t, "new-api"), indexMin: catalog.BasePlatform}}, nil, nil)
	v, _ = setupSnapshot(t, bundleFS(b), nil)
	_, err = v.e.StartInstall(ctx, Request{App: "new-api"})
	isFormer("package", err)
	if !strings.Contains(err.Error(), "第 2 版") {
		t.Fatalf("which format: %v", err)
	}
	if list, _ := v.e.List(ctx); list.Apps[0].Former {
		t.Fatal("the list goes by the index, which says the current platform")
	}
	nothingCreated(t, v)
}
