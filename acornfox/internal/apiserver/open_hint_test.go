package apiserver

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/state"
)

// sealCatalogSecret encrypts plain the way the app center keeps install
// records ("v1:" + base64url(nonce || AES-256-GCM sealed), key in
// <data>/secrets/catalog.key), so a test can lay down what an install leaves
// behind without running one.
func sealCatalogSecret(t *testing.T, dataDir string) func(string) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "secrets", "catalog.key"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	blk, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(blk)
	return func(plain string) string {
		nonce := make([]byte, aead.NonceSize())
		_, _ = rand.Read(nonce)
		return "v1:" + base64.RawURLEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plain), nil))
	}
}

// A catalog app whose install set a first-login password has its own login:
// on a server in the user's LAN its open hint offers the LAN switch
// (can_lan, recommend lan); on a server without a LAN address it is the
// tunnel. A catalog app without such a record is not offered the switch.
// The detail view names the local model gateway an app got its token from;
// the list does not carry it.
func TestCatalogAppOpenHintAndGatewayToken(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seal := sealCatalogSecret(t, dir)
	eng := appinstall.New(appinstall.Config{Store: db, Catalog: appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog")}), DataDir: dir})
	defer eng.Close()
	st := newFakeStore()
	lan := "192.168.1.20"
	h := New(Config{
		Store: st, Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir,
		PublicHost: lan, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng,
		ServerLAN: func() string { return lan },
	})
	ctx := context.Background()
	login := `{"app":"%s","fields":[{"label":"账号","value":"admin"},{"label":"密码","value":"generated-password","secret":true}]}`
	apps := []struct{ name, catalogID, params, firstLogin string }{
		{"new-api", "new-api", `{"admin_username":"admin","admin_password":"generated-password"}`, login},
		{"hermes-agent", "hermes-agent", `{"_acornfox_gateway_tokens":[{"provider":"new-api","id":3}]}`, login},
		{"plain", "some-app", `{}`, `{"app":"plain","fields":[{"label":"地址","value":"x"}]}`},
	}
	ports := map[string]int{}
	for _, a := range apps {
		app, _, err := st.EnsureApp(ctx, a.name)
		if err != nil {
			t.Fatal(err)
		}
		ports[a.name] = app.PublicPort
		st.UpdateApp(ctx, a.name, func(x *state.App) error { x.CatalogID, x.Exposure = a.catalogID, state.ExposureLocal; return nil })
		if _, _, err := db.EnsureApp(ctx, a.name); err != nil {
			t.Fatal(err)
		}
		fl := a.firstLogin
		if fl == login {
			fl = fmt.Sprintf(login, a.name)
		}
		if err := db.SetCatalogSecrets(ctx, state.CatalogSecrets{App: a.name, ParamsEnc: seal(a.params), FirstLoginEnc: seal(fl)}); err != nil {
			t.Fatal(err)
		}
	}
	get := func(name string) appView {
		w := do(t, h, "GET", "/v1/apps/"+name, nil, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("get %s: %d %s", name, w.Code, w.Body.String())
		}
		var v appView
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return v
	}

	hint := get("new-api").OpenHint
	if hint == nil || !hint.HasLogin || !hint.CanLAN || hint.Recommend != "lan" || hint.LANAddress != lan ||
		hint.LANURL != "http://192.168.1.20:"+itoa(ports["new-api"]) || hint.Command != "acornfox open new-api" {
		t.Fatalf("new-api hint %+v", hint)
	}
	if hint := get("plain").OpenHint; hint == nil || hint.HasLogin || hint.CanLAN || hint.Recommend != "tunnel" {
		t.Fatalf("app without its own login: %+v", hint)
	}

	v := get("hermes-agent")
	if v.GatewayTokenFrom != "new-api" {
		t.Fatalf("gateway_token_from = %q", v.GatewayTokenFrom)
	}
	if get("new-api").GatewayTokenFrom != "" {
		t.Fatal("new-api did not get a token from anyone")
	}
	var list struct {
		Apps []appView `json:"apps"`
	}
	_ = json.Unmarshal(do(t, h, "GET", "/v1/apps", nil, nil).Body.Bytes(), &list)
	if len(list.Apps) != 3 {
		t.Fatalf("list %+v", list.Apps)
	}
	for _, a := range list.Apps {
		if a.GatewayTokenFrom != "" {
			t.Fatalf("list carries gateway_token_from: %+v", a)
		}
		if a.Name == "new-api" && (a.OpenHint == nil || !a.OpenHint.CanLAN) {
			t.Fatalf("list hint %+v", a.OpenHint)
		}
	}

	// A server not in the user's LAN (cloud): the tunnel, even with a login.
	lan = ""
	if hint := get("new-api").OpenHint; hint == nil || !hint.HasLogin || hint.CanLAN || hint.Recommend != "tunnel" || hint.LANURL != "" {
		t.Fatalf("cloud hint %+v", hint)
	}
}
