package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/state"
)

// GET /v1/machine: what is installed, how to open it, which services there
// are and who uses them, what the platform can do; and never a secret.
func TestMachineView(t *testing.T) {
	h, db := reapplyServer(t)
	ctx := context.Background()
	const secret = "pw-secret-7q8w9e"
	for _, n := range []string{"dp", "dc", "mine"} {
		if _, _, err := db.EnsureApp(ctx, n); err != nil {
			t.Fatal(err)
		}
	}
	set := func(name, id, source string) {
		if _, err := db.UpdateApp(ctx, name, func(a *state.App) error {
			a.CatalogID, a.CatalogVersion, a.CatalogSource = id, "v1.0.0", source
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	set("dp", "demo-provider", "recipe:demo-pair")
	set("dc", "demo-consumer", "recipe:demo-pair")
	// The manifest each was installed with: what "uses" is read from.
	for app, id := range map[string]string{"dp": "demo-provider", "dc": "demo-consumer"} {
		text, err := os.ReadFile(filepath.Join("..", "catalog", "testdata", "services", "apps", id, "manifest.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetCatalogManifest(ctx, state.CatalogManifest{App: app, CatalogID: id, CatalogVersion: "v1.0.0",
			Manifest: string(text), SHA256: state.ManifestSHA256(string(text)), Source: state.ManifestFromInstall}); err != nil {
			t.Fatal(err)
		}
	}
	// An env value that is a secret on an app, and a user service with a secret item: neither may show.
	if err := db.SetEnv(ctx, state.EnvVar{App: "mine", Key: "API_KEY", Value: secret, Secret: true}); err != nil {
		t.Fatal(err)
	}
	if w := do(t, h, "POST", "/v1/services", []byte(`{"name":"我的邮箱","kind":"smtp","values":{"host":"mail.example.com","password":"`+secret+`"}}`), jsonHeader); w.Code != http.StatusCreated {
		t.Fatalf("add service: %d %s", w.Code, w.Body.String())
	}

	w := do(t, h, "GET", "/v1/machine", nil, nil)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body.String())
	}
	body := w.Body.String()
	for _, banned := range []string{secret, "mail.example.com", "password", "api_key", "API_KEY", "values"} {
		if strings.Contains(body, banned) {
			t.Fatalf("本机能力里不能出现 %q：%s", banned, body)
		}
	}
	if strings.Contains(body, "null") {
		t.Fatalf("不能有 null：%s", body)
	}
	var v struct {
		Schema      int    `json:"schema"`
		GeneratedAt string `json:"generated_at"`
		Machine     struct{ Name, Version, Region string }
		Apps        []struct {
			ID, Instance, Name, Tagline, Source, State, URL, Exposure, Version string
			Manual                                                             struct {
				Has     bool
				Kind    string
				Version string
			}
			Provides []string
			Uses     []struct {
				Name, Kind string
				Required   bool
				Service    string
			}
		}
		Solutions []struct {
			ID, Name string
			Apps     []string
			Manual   struct{ Has bool }
		}
		Services []struct {
			Type, Name, Service, Provider, Status string
			UsedBy                                []string `json:"used_by"`
			RegisteredByUser                      bool     `json:"registered_by_user"`
		}
		Platform struct{ Backup, Upgrade, Reapply bool }
		Warnings []struct{ Code, Part, Message string }
	}
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	if v.Schema != 1 || v.GeneratedAt == "" || v.Machine.Region != "global" || v.Machine.Name == "" {
		t.Fatalf("头：%+v", v)
	}
	if len(v.Apps) != 3 || v.Apps[0].Instance != "dc" || v.Apps[1].Instance != "dp" || v.Apps[2].Instance != "mine" {
		t.Fatalf("应用：%+v", v.Apps)
	}
	dp := v.Apps[1]
	if dp.ID != "demo-provider" || dp.Source != "catalog" || dp.Version != "v1.0.0" || dp.Name != "样例提供方" || dp.Tagline != "提供样例令牌服务，只为测试清单格式" ||
		dp.State != "not_running" || dp.Manual.Has || len(dp.Provides) != 1 || dp.Provides[0] != "demo_token" {
		t.Fatalf("目录里的应用：%+v", dp)
	}
	dc := v.Apps[0]
	if len(dc.Uses) != 2 || dc.Uses[0].Kind != "demo_token" || !dc.Uses[0].Required || dc.Uses[1].Kind != "smtp" || dc.Uses[1].Required {
		t.Fatalf("用到的服务：%+v", dc.Uses)
	}
	if mine := v.Apps[2]; mine.ID != "mine" || mine.Source != "deploy" || mine.Name != "mine" || mine.Version != "" {
		t.Fatalf("自己部署的项目：%+v", mine)
	}
	if len(v.Solutions) != 1 || v.Solutions[0].ID != "demo-pair" || strings.Join(v.Solutions[0].Apps, ",") != "demo-consumer,demo-provider" {
		t.Fatalf("解决方案：%+v", v.Solutions)
	}
	if len(v.Services) != 1 || v.Services[0].Type != "smtp" || v.Services[0].Name != "邮箱（发信）" || v.Services[0].Service != "我的邮箱" ||
		!v.Services[0].RegisteredByUser || v.Services[0].Provider != "" || v.Services[0].Status != "ok" || len(v.Services[0].UsedBy) != 0 {
		t.Fatalf("服务：%+v", v.Services)
	}
	if !v.Platform.Upgrade || !v.Platform.Reapply || v.Platform.Backup {
		t.Fatalf("平台：%+v", v.Platform)
	}
	if len(v.Warnings) != 0 {
		t.Fatalf("不该有 warnings：%+v", v.Warnings)
	}
}

// With nothing installed every list is an empty list, and a server without
// the app center still answers.
func TestMachineViewEmptyAndNoAppCenter(t *testing.T) {
	h, _ := reapplyServer(t)
	w := do(t, h, "GET", "/v1/machine", nil, nil)
	m := decodeObj(t, w.Body.String())
	for _, k := range []string{"apps", "solutions", "services", "warnings"} {
		if l, ok := m[k].([]any); !ok || len(l) != 0 {
			t.Fatalf("%s 要是空列表：%s", k, w.Body.String())
		}
	}
	bare := New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: t.TempDir(), HostMetrics: fakeHostSized(8192, 4), Version: "0.6.4"})
	w = do(t, bare, "GET", "/v1/machine", nil, nil)
	m = decodeObj(t, w.Body.String())
	if w.Code != http.StatusOK || m["platform"].(map[string]any)["upgrade"] != false || m["machine"].(map[string]any)["version"] != "0.6.4" {
		t.Fatalf("没有应用中心：%d %s", w.Code, w.Body.String())
	}
}
