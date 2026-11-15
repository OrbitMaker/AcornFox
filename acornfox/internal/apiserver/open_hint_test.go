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
	"strings"
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
// An app installed with a manifest of the former format may have a record
// of a credential another app made for it: the detail view carries nothing
// about it, and deleting the app tells the user where to remove it by hand
// (it is not taken back any more).
func TestCatalogAppOpenHintAndLegacyCredential(t *testing.T) {
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
	port := itoa(ports["new-api"])
	after := "http://192.168.1.20:" + port
	if hint == nil || !hint.HasLogin || !hint.CanLAN || hint.Recommend != "lan" || hint.LANAddress != lan ||
		hint.LANURL != "" || hint.LANURLAfterExpose != after || hint.Command != "acornfox open new-api" ||
		hint.AskYesCommand != "acornfox --json app set --app new-api --expose lan" ||
		hint.TunnelCommand != "ssh -N -L "+port+":127.0.0.1:"+port+" 用户@服务器地址" || hint.TunnelURL != "http://127.0.0.1:"+port {
		t.Fatalf("new-api hint %+v", hint)
	}
	// Both commands run on the user's own computer, and the hint says so
	// in words to pass on (run on the server, open only prints the
	// server's loopback address).
	if hint.CommandWhere != "在你自己电脑的终端运行（电脑上要装了 acornfox 命令行，并用 --ssh 登记过这台服务器）；在服务器上运行没有用" ||
		hint.TunnelWhere != "在你自己电脑的终端运行，保持不关" {
		t.Fatalf("where: %q / %q", hint.CommandWhere, hint.TunnelWhere)
	}
	// The question is the one to put to the user as it is: what opens to
	// whom, the risk, the address it opens at once switched, and that no is
	// an answer too.
	for _, want := range []string{"new-api", "所有网卡", "端口 " + port, "公网也能访问到", "自己的账号密码", after, "SSH 隧道"} {
		if !strings.Contains(hint.Ask, want) {
			t.Fatalf("ask lacks %q: %s", want, hint.Ask)
		}
	}
	if hint := get("plain").OpenHint; hint == nil || hint.HasLogin || hint.CanLAN || hint.Recommend != "tunnel" ||
		hint.LANURLAfterExpose != "" || hint.Ask != "" || hint.AskYesCommand != "" || hint.TunnelCommand == "" {
		t.Fatalf("app without its own login: %+v", hint)
	}

	if w := do(t, h, "GET", "/v1/apps/hermes-agent", nil, nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "gateway_token") {
		t.Fatalf("detail of the former-format app: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Apps []appView `json:"apps"`
	}
	_ = json.Unmarshal(do(t, h, "GET", "/v1/apps", nil, nil).Body.Bytes(), &list)
	if len(list.Apps) != 3 {
		t.Fatalf("list %+v", list.Apps)
	}
	for _, a := range list.Apps {
		if a.Name == "new-api" && (a.OpenHint == nil || !a.OpenHint.CanLAN) {
			t.Fatalf("list hint %+v", a.OpenHint)
		}
	}

	// A finished install task carries the same hint for each of its apps
	// (worked out when it is read); an app that is gone has none, a task
	// still running has none, and the provider's sentences for the user
	// are in must_tell.
	taskData := `{"id":"%s","kind":"app","target":"new-api","status":"%s","steps":[],"apps":[` +
		`{"app":"new-api","catalog_id":"new-api","version":"v1","reused":false,"created":true,"tell":["在提供方里加好上游后，填一个模型名"]},` +
		`{"app":"gone-app","catalog_id":"x","version":"v1","reused":false,"created":true}],"created_at":"2026-11-13T00:00:00Z"}`
	for id, status := range map[string]string{"0123456789ab": state.TaskSucceeded, "0123456789ac": state.TaskRunning} {
		if _, err := db.CreateInstallTask(ctx, state.InstallTask{ID: id, Kind: "app", Target: "new-api", Status: status, Data: fmt.Sprintf(taskData, id, status)}); err != nil {
			t.Fatal(err)
		}
	}
	var task struct {
		Apps []struct {
			App      string    `json:"app"`
			Created  bool      `json:"created"`
			Tell     []string  `json:"tell"`
			OpenHint *openHint `json:"open_hint"`
		} `json:"apps"`
		Timing struct {
			MustTell []string `json:"must_tell"`
		} `json:"timing"`
	}
	w := do(t, h, "GET", "/v1/catalog/install/0123456789ab", nil, nil)
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil || w.Code != http.StatusOK || len(task.Apps) != 2 {
		t.Fatalf("task: %d %s", w.Code, w.Body.String())
	}
	if got, want := task.Apps[0].OpenHint, get("new-api").OpenHint; got == nil || *got != *want || !task.Apps[0].Created || len(task.Apps[0].Tell) != 1 {
		t.Fatalf("task app hint %+v, the app's %+v", task.Apps[0], want)
	}
	if task.Apps[1].OpenHint != nil || strings.Count(w.Body.String(), `"open_hint"`) != 1 {
		t.Fatalf("an app that is gone has a hint: %s", w.Body.String())
	}
	if len(task.Timing.MustTell) != 1 || task.Timing.MustTell[0] != "在提供方里加好上游后，填一个模型名" {
		t.Fatalf("must_tell %q", task.Timing.MustTell)
	}
	if w := do(t, h, "GET", "/v1/catalog/install/0123456789ac", nil, nil); w.Code != http.StatusOK || strings.Contains(w.Body.String(), `"open_hint"`) {
		t.Fatalf("running task: %d %s", w.Code, w.Body.String())
	}

	// A server not in the user's LAN (cloud): the tunnel, even with a login.
	lan = ""
	if hint := get("new-api").OpenHint; hint == nil || !hint.HasLogin || hint.CanLAN || hint.Recommend != "tunnel" || hint.LANURL != "" ||
		hint.LANURLAfterExpose != "" || hint.Ask != "" || hint.AskYesCommand != "" || !strings.HasPrefix(hint.TunnelCommand, "ssh -N -L ") {
		t.Fatalf("cloud hint %+v", hint)
	}

	// Deleted (data kept or not), the former-format app's record says where
	// its credential is; nothing is sent to that app.
	w = do(t, h, "DELETE", "/v1/apps/hermes-agent", nil, nil)
	var del struct {
		Warnings []string `json:"warnings"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &del)
	if w.Code/100 != 2 || len(del.Warnings) != 1 || !strings.HasPrefix(del.Warnings[0], appinstall.CodeRevoke+" ") ||
		!strings.Contains(del.Warnings[0], "旧格式") || !strings.Contains(del.Warnings[0], "new-api 里") || !strings.Contains(del.Warnings[0], "编号 3") {
		t.Fatalf("delete of the former-format app: %d %s", w.Code, w.Body.String())
	}
}
