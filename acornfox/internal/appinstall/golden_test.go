package appinstall

// Golden samples of what the engine writes into the two published apps.
//
// The files under testdata/golden/ record, for the New API and Hermes
// manifests exactly as they were published (catalog serial 3, taken from
// internal/catalog/testdata/published-3), what an install with fixed inputs
// produces: the app's settings, every environment variable, every config
// file with its content, mode and owner, the body of every init request,
// and the first-login fields. The test compares them byte for byte.
//
// These samples stand for the behaviour of apps that are already published
// and installed on users' machines. A published package can never be
// changed, so the engine has to keep producing the same bytes from it. If
// this test fails, the engine's output for a published app changed: treat
// that as a breaking change and find out why, do not refresh the samples to
// make the test pass. Refresh them only when the change is intended and has
// been checked against the real apps (a changed escape in config.yaml or in
// the setup request locks users out), and say so in the commit:
//
//	go test ./internal/appinstall -run TestGoldenPublishedApps -update
//
// What is fixed: the manifests (published-3), the request, the machine's
// platform settings, and the "random" passwords and keys (secretRand is a
// counter-mode SHA-256 stream; math/big's way of drawing from it is part of
// the sample, so a Go release that changes crypto/rand.Int would show up
// here as changed passwords and nothing else).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
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

// publishedSnapshot is the published catalog (serial 3) rebuilt from its
// source; the rebuilt index must be the published one, byte for byte, so the
// packages in it are the published packages.
func publishedSnapshot(t testing.TB) fs.FS {
	t.Helper()
	root := filepath.Join("..", "catalog", "testdata", "published-3")
	want, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := catalog.Build(filepath.Join(root, "src"), 3)
	if err != nil {
		t.Fatalf("the published catalog no longer builds: %v", err)
	}
	if !bytes.Equal(b.Index, want) {
		t.Fatal("the published catalog no longer builds to the published index.json")
	}
	return bundleFS(b)
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

func TestGoldenPublishedApps(t *testing.T) {
	snap := publishedSnapshot(t)
	scenarios := []struct {
		name     string
		platform PlatformEnv
		req      Request
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
			// Hermes with the user's own key; the key and the model hold a
			// quote and a backslash, which config.yaml must escape exactly
			// once inside the template's own quotes.
			name:     "hermes-own-key",
			platform: PlatformEnv{China: true, Cloud: "tencent", TZ: "Asia/Tokyo"},
			req: Request{App: "hermes-agent", Params: map[string]any{"llm": map[string]any{
				"source": "own", "base_url": "https://api.example.com/v1", "api_key": `sk-"quoted"\back`, "model": `qwen-max:"v1"`}}},
		},
		{
			// The recipe: New API, then Hermes wired to it with a token
			// issued in it and the platform's default model.
			name:     "recipe-hermes-with-new-api",
			platform: PlatformEnv{China: true, TZ: "Asia/Shanghai", ChatModel: "qwen-plus"},
			req:      Request{Recipe: "hermes-with-new-api"},
		},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			saved := secretRand
			secretRand = &fixedRand{seed: sc.name}
			t.Cleanup(func() { secretRand = saved })

			v, hits := setupSnapshot(t, snap, func(c *Config) { c.Platform = sc.platform })
			v.e.SetHooks(Hooks{Reserve: v.w.reserve, Delete: v.w.delete, WithApp: v.w.withApp, Kick: v.w.kick,
				URL: func(ctx context.Context, a state.App) string { return "http://server.example:8080/" + a.Name }})
			ctx := context.Background()

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
				srv := httptest.NewServer(rec.wrap(v.newapi.handler()))
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

			doc := goldenDoc{Scenario: sc.name, Request: sc.req, Platform: sc.platform}
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

// The published packages pass the strict manifest check and every other
// check an install makes before it creates anything, both as rebuilt from
// the published source and as built into the program.
func TestPublishedPackagesPassStrictCheck(t *testing.T) {
	for name, snap := range map[string]fs.FS{"published-3": publishedSnapshot(t), "built-in snapshot": catalog.Snapshot()} {
		v, _ := setupSnapshot(t, snap, nil)
		idx, _, err := v.e.Catalog().Index()
		if err != nil {
			t.Fatal(err)
		}
		if len(idx.Apps) < 2 {
			t.Fatalf("%s: %d apps", name, len(idx.Apps))
		}
		for _, ia := range idx.Apps {
			a, err := v.e.appToInstall(context.Background(), PublicSource, ia.ID)
			if err != nil {
				t.Errorf("%s: %s %s is refused: %v", name, ia.ID, ia.Version, err)
				continue
			}
			if err := v.e.checkManifest(a); err != nil {
				t.Errorf("%s: %s %s: %v", name, ia.ID, ia.Version, err)
			}
			if need := v.e.needsPlatform(ia.MinPlatform, a.Manifest.Lifecycle.MinPlatform); need != "" {
				t.Errorf("%s: %s asks for %s, which 0.5.0 is not", name, ia.ID, need)
			}
		}
		l, err := v.e.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range l.Apps {
			if a.NeedsPlatform != "" {
				t.Errorf("%s: %s is marked needs_platform %q on 0.5.0", name, a.ID, a.NeedsPlatform)
			}
		}
		for _, r := range l.Recipes {
			if r.NeedsPlatform != "" {
				t.Errorf("%s: recipe %s is marked needs_platform %q on 0.5.0", name, r.ID, r.NeedsPlatform)
			}
		}
	}
}
