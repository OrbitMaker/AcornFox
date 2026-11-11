package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/faults"
	"github.com/acornfox/acornfox/internal/hostcheck"
	"github.com/acornfox/acornfox/internal/state"
)

func TestFaultsRoutes(t *testing.T) {
	cfg, _, _ := newTestConfig(t)
	h := New(cfg)

	w := do(t, h, "GET", "/v1/faults", nil, nil)
	var list struct {
		Faults []faults.Fault `json:"faults"`
		Groups []struct{ Key, Name string }
		Tiers  map[string]string `json:"tiers"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Faults) != len(faults.Catalog) || len(list.Groups) == 0 || list.Tiers["2"] == "" {
		t.Fatalf("list = %d %.300s", w.Code, w.Body.String())
	}

	w = do(t, h, "GET", "/v1/faults/cat-inst-27", nil, nil)
	var one struct {
		Fault faults.Fault `json:"fault"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &one) != nil || one.Fault.Code != "CAT-INST-27" || len(one.Fault.Causes) == 0 || one.Fault.Causes[0].Check == "" {
		t.Fatalf("one = %d %.300s", w.Code, w.Body.String())
	}
	if w := do(t, h, "GET", "/v1/faults/build_failed", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"stage":"build"`) {
		t.Fatalf("lower-case code = %d %.300s", w.Code, w.Body.String())
	}

	w = do(t, h, "GET", "/v1/faults/NOPE-01", nil, nil)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"code":"fault_unknown"`) || !strings.Contains(w.Body.String(), `"code":"ANY-00"`) {
		t.Fatalf("unknown = %d %.300s", w.Code, w.Body.String())
	}

	// The runtime page's lookup now knows every code, the new doctor checks
	// included, in the same short shape as before.
	w = do(t, h, "GET", "/v1/setup/faults", nil, nil)
	var short struct {
		Faults []struct {
			Code      string   `json:"code"`
			Title     string   `json:"title"`
			Causes    []string `json:"causes"`
			Next      string   `json:"next"`
			NeedsUser bool     `json:"needs_user"`
		} `json:"faults"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &short) != nil {
		t.Fatalf("setup faults = %d", w.Code)
	}
	got := map[string]bool{}
	for _, f := range short.Faults {
		got[f.Code] = f.Title != "" && f.Next != "" && len(f.Causes) > 0
	}
	for _, c := range []string{"SETUP-DISK-01", "SETUP-GW-01", "SETUP-SYNC-01", "CAT-INST-24", "LAN-CONSOLE-03"} {
		if !got[c] {
			t.Errorf("GET /v1/setup/faults lacks %s", c)
		}
	}
}

// The runtime page shows the download gateway (when the sources name one)
// and the app catalog sync (when the app center is on), Linux only.
func TestRuntimeChecksGatewayAndCatalog(t *testing.T) {
	withRuntimeEnv(t, "linux", "", 50<<30)
	var asked []string
	testGatewayProbe = func(gw string) hostcheck.GatewayProbe {
		asked = append(asked, gw)
		return hostcheck.GatewayProbe{Base: gw, Health: func(context.Context) (hostcheck.GatewayHealth, error) {
			return hostcheck.GatewayHealth{}, errors.New("dial tcp: lookup dl.example.invalid: no such host")
		}}
	}
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	eng := appinstall.New(appinstall.Config{Store: db, DataDir: dir,
		Catalog: appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog"), Sources: []string{"http://127.0.0.1:1"}})})
	defer eng.Close()
	cfg, _, _ := newTestConfig(t)
	cfg.DataDir = "/var/lib/acornfox"
	cfg.AppCenter = eng
	cfg.Sources = func() DownloadSources { return DownloadSources{Gateway: "https://dl.example.invalid"} }
	v := runtimeGet(t, New(cfg))

	gw := itemWith(v, "下载网关：")
	if gw == nil || gw.Level != "warn" || gw.Code != "SETUP-GW-01" || gw.Check != "SETUP-GW-01" || !strings.Contains(gw.Text, "no such host") {
		t.Fatalf("gateway item %+v (all %+v)", gw, v.Items)
	}
	if len(asked) != 1 || asked[0] != "https://dl.example.invalid" {
		t.Fatalf("gateway asked for %v", asked)
	}
	cat := itemWith(v, "应用目录：")
	if cat == nil || cat.Level != "warn" || cat.Code != "SETUP-SYNC-01" || !strings.Contains(cat.Text, "还没同步过") || !strings.Contains(cat.Text, "程序内置的目录") {
		t.Fatalf("catalog item %+v (all %+v)", cat, v.Items)
	}
	if !v.OK {
		t.Fatalf("warnings must not fail the page: %+v", v.Items)
	}

	// The gateway switched off: an ok line saying so, nothing asked.
	asked = nil
	cfg.Sources = func() DownloadSources { return DownloadSources{} }
	v = runtimeGet(t, New(cfg))
	if gw := itemWith(v, "下载网关："); gw == nil || gw.Level != "ok" || gw.Check != "SETUP-GW-01" || !strings.Contains(gw.Text, "关掉了") {
		t.Fatalf("gateway off %+v", gw)
	}

	// Desktop: neither line.
	runtimeGOOS = "darwin"
	v = runtimeGet(t, New(cfg))
	if itemWith(v, "下载网关") != nil || itemWith(v, "应用目录") != nil {
		t.Fatalf("desktop lines %+v", v.Items)
	}
}
