package appinstall

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// The test material for how values are written (catalog/testdata/values):
// one app writing every way of putting a value into an app. Its README
// lists what each value should come out as; these tests check exactly that.
const valuesDir = "../catalog/testdata/values"

func valuesManifest(t testing.TB) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(valuesDir, "apps", "demo-values", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// setupValues is setup with the catalog holding manifest (demo-values) and
// its service type, and a fake of the app that answers its health check.
func setupValues(t *testing.T, manifest string, china bool) *env {
	t.Helper()
	v := setupWith(t, func(cfg *Config) {
		b := withServiceTypes(t, fixtureBundle(t, 7, "", []fixApp{{manifest: manifest}}, []catalog.Recipe{oneAppRecipe("demo-values")}, nil), valuesDir)
		cfg.Catalog = NewCatalog(CatalogConfig{CacheDir: filepath.Join(t.TempDir(), "catalog"), Snapshot: bundleFS(b), Sources: []string{"http://127.0.0.1:1"}})
		cfg.Platform = PlatformEnv{China: china}
	})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { _, _ = rw.Write([]byte("<h1>ok</h1>")) }))
	t.Cleanup(srv.Close)
	v.w.mu.Lock()
	v.w.servers["demo-values"] = srv
	v.w.mu.Unlock()
	return v
}

func appEnvVars(t *testing.T, v *env, app string) map[string]state.EnvVar {
	t.Helper()
	list, err := v.w.st.ListEnv(context.Background(), app)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]state.EnvVar{}
	for _, e := range list {
		out[e.Key] = e
	}
	return out
}

func TestInstallWritesValuesAsTheManifestSays(t *testing.T) {
	for _, china := range []bool{false, true} {
		t.Run("china="+strconv.FormatBool(china), func(t *testing.T) {
			v := setupValues(t, valuesManifest(t), china)
			tv := v.install(t, Request{App: "demo-values"})
			if tv.Status != state.TaskSucceeded {
				t.Fatalf("task %+v %+v", tv, tv.Fault)
			}
			ctx := context.Background()
			a, err := v.w.st.GetApp(ctx, "demo-values")
			if err != nil {
				t.Fatal(err)
			}
			url := "http://127.0.0.1:" + strconv.Itoa(a.PublicPort)
			// One item, one argument: the blank stays inside it.
			if got := strings.Join(a.Command, "|"); got != "httpd|-f|-p|8080|-h|/www|-r|DEMO VALUES" {
				t.Fatalf("command %q", got)
			}
			region, isCN, mode := "GLOBAL", "no", "abroad"
			if china {
				region, isCN, mode = "CN", "yes", "domestic"
			}
			// What the platform gives for the PyPI mirror on a machine in
			// mainland China, asked through the same interface the engine uses.
			cnVals, err := platformvar.NewStandalone(platformvar.Machine{China: true}).Values(ctx, platformvar.App{})
			if err != nil || cnVals[catalog.PVMirrorPyPI] == "" {
				t.Fatalf("platform values: %v %v", cnVals, err)
			}
			cnPyPI := cnVals[catalog.PVMirrorPyPI]
			env := appEnvVars(t, v, "demo-values")
			want := map[string]string{
				"APP_URL_SLASH":   url + "/",
				"REGION_UPPER":    region,
				"REGION_IS_CN":    isCN,
				"MODE":            mode,
				"FLAG_ON":         "on",
				"GREETING_B64":    "c2F5ICJoaSIgXCBiYWNr",
				"GREETING_URL":    "say%20%22hi%22%20%5C%20back",
				"GREETING_SHA256": "79f203fa9524fb6efceca47ecfd5f65c3cf03e6e1c493e4517b3fe31dd530c2d",
				"GREETING_SHELL":  `'say "hi" \ back'`,
				"GREETING_SNAKE":  `say_"hi"_\_back`,
				"MAIL_HOST":       "none",
				"LITERAL_BRACES":  `{{params.greeting}} and {{ $json.x }}`,
			}
			if china {
				want["PIP_ONLY_IN_CN"] = cnPyPI
			}
			for k, w := range want {
				if env[k].Value != w || env[k].Secret {
					t.Errorf("%s = %+v，要 %q", k, env[k], w)
				}
			}
			dsn := env["ADMIN_DSN"]
			if !dsn.Secret || !strings.HasPrefix(dsn.Value, "admin:") || !strings.HasSuffix(dsn.Value, "@db") || len(dsn.Value) != len("admin:@db")+20 {
				t.Errorf("ADMIN_DSN %+v", dsn)
			}
			if len(env) != len(want)+1 {
				t.Errorf("多了或少了环境变量：%d 条，要 %d：%v", len(env), len(want)+1, env)
			}
			vol := tv.Apps[0].Volumes[0]
			files := map[string]string{
				"index.html":    "<h1>demo values</h1>\n",
				"settings.json": `{"greeting": "say \"hi\" \\ back", "raw": "say \"hi\" \\ back", "region": "` + strings.ToLower(region) + `"}` + "\n",
				"settings.yaml": "greeting: \"say \\\"hi\\\" \\\\ back\"\nraw: \"say \\\"hi\\\" \\\\ back\"\n",
			}
			if china {
				files["cn-only.txt"] = "mirror=" + cnPyPI + "\n"
			}
			for p, w := range files {
				if got, ok := v.w.volFile(vol, p); !ok || got != w {
					t.Errorf("%s = %q，要 %q", p, got, w)
				}
			}
			for _, p := range []string{"mail.txt", "cn-only.txt"} {
				if _, want := files[p]; !want {
					if _, ok := v.w.volFile(vol, p); ok {
						t.Errorf("%s 的 if 不成立，不该写", p)
					}
				}
			}
			fl, err := v.e.FirstLogin(ctx, "demo-values", false)
			if err != nil || len(fl.Fields) != 2 || fl.Fields[0].Value != url+"/index.html" || !fl.Fields[1].Secret {
				t.Fatalf("first login %+v %v", fl, err)
			}
			// apps vars hands out the values as written, a secret one by
			// its fingerprint.
			vars, err := v.e.AppVars(ctx, "demo-values", false)
			if err != nil {
				t.Fatal(err)
			}
			byKey := map[string]AppEnvVar{}
			for _, e := range vars.Env {
				byKey[e.Key] = e
			}
			if e := byKey["MODE"]; e.Value != mode || e.Secret || e.SHA256 != "" || !e.FromManifest {
				t.Errorf("apps vars MODE %+v", e)
			}
			if e := byKey["ADMIN_DSN"]; !e.Secret || !e.Masked || e.Value == dsn.Value || e.SHA256 != hexSum(dsn.Value) {
				t.Errorf("apps vars ADMIN_DSN %+v", e)
			}
		})
	}
}

// A flag set to false leaves its entry out; a value for the other case of a
// key is never written.
func TestInstallValuesIfFalse(t *testing.T) {
	v := setupValues(t, valuesManifest(t), false)
	tv := v.install(t, Request{App: "demo-values", Params: map[string]any{"flag": false}})
	if tv.Status != state.TaskSucceeded {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	env := appEnvVars(t, v, "demo-values")
	if _, ok := env["FLAG_ON"]; ok {
		t.Fatalf("FLAG_ON %+v", env["FLAG_ON"])
	}
	if env["MODE"].Value != "abroad" {
		t.Fatalf("MODE %+v", env["MODE"])
	}
}

// A map that matches nothing fails the install with CAT-INST-39 (the value
// is not in the message) and the install is cleaned up.
func TestInstallValueThatCannotBeWorkedOut(t *testing.T) {
	m := strings.Replace(valuesManifest(t), `map "cn=yes" "*=no"`, `map "cn=yes"`, 1)
	v := setupValues(t, m, false)
	tv := v.install(t, Request{App: "demo-values"})
	if tv.Status != state.TaskFailed || tv.Fault == nil || tv.Fault.Code != CodeRender || !strings.Contains(tv.Fault.Message, "REGION_IS_CN") || strings.Contains(tv.Fault.Message, "global") {
		t.Fatalf("task %+v %+v", tv, tv.Fault)
	}
	if _, err := v.w.st.GetApp(context.Background(), "demo-values"); err == nil {
		t.Fatal("a failed install left the app")
	}
}

// What cannot be read is refused before anything is created (CAT-INST-39).
func TestInstallRefusesUnreadableValues(t *testing.T) {
	cases := map[string][2]string{
		"env value":     {`"{{platform.app.url}}/" }`, `"{{platform.app.url/" }`},
		"env if":        {`if: params.flag }`, `if: "params.flag > 1" }`},
		"start command": {`"{{params.realm | upper}}"]`, `'{{params.realm | upper "x"}}']`},
		"file if":       {`if: uses.mail.host`, `if: "uses.mail.host =="`},
		"first login":   {`"{{platform.app.url}}/index.html" }`, `"{{platform.app.url | nope}}" }`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := strings.Replace(valuesManifest(t), c[0], c[1], 1)
			if m == valuesManifest(t) {
				t.Fatalf("没改到：%s", c[0])
			}
			v := setupValues(t, m, false)
			_, err := v.e.StartInstall(context.Background(), Request{App: "demo-values"})
			if code(err) != CodeRender {
				t.Fatalf("%v", err)
			}
			nothingCreated(t, v)
		})
	}
}

// Two entries of one key that both hold, and a secret in the start command,
// are refused by the engine too (lint refuses them before: CAT-LINT-49, 50).
func TestRenderEnvAndArgsGuards(t *testing.T) {
	m := &catalog.Manifest{Env: []catalog.EnvVar{
		{Key: "A", Value: "1", If: "params.on"},
		{Key: "A", Value: "2", If: "params.on != false"},
	}}
	vals := Values{"on": "true", "pw": "secret-value"}
	if _, err := renderEnv(m, vals, nil); code(err) != CodeRender {
		t.Fatalf("both hold: %v", err)
	}
	m.Env[1].If = "params.on == false"
	got, err := renderEnv(m, vals, nil)
	if err != nil || len(got) != 1 || got[0].Value != "1" {
		t.Fatalf("%+v %v", got, err)
	}
	secret := func(ref string) bool { return ref == "params.pw" }
	if _, err := renderArgs([]string{"run", "--pw={{params.pw}}"}, vals, secret); code(err) != CodeRender || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("secret in the command: %v", err)
	}
	if args, err := renderArgs([]string{"run", "{{params.on | upper}}"}, vals, secret); err != nil || strings.Join(args, " ") != "run TRUE" {
		t.Fatalf("%v %v", args, err)
	}
	// A value made from a secret is kept secret.
	m = &catalog.Manifest{Env: []catalog.EnvVar{{Key: "DSN", Value: "u:{{params.pw}}@h"}, {Key: "OPT", Value: "{{params.none}}", Optional: true}}}
	got, err = renderEnv(m, Values{"pw": "x", "none": ""}, secret)
	if err != nil || len(got) != 1 || !got[0].Secret {
		t.Fatalf("%+v %v", got, err)
	}
}

func hexSum(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
