package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// The services test catalog (catalog/testdata/services): three service types
// (demo_token, llm_gateway, smtp), an app that provides demo_token and one
// that needs services. The built-in snapshot has no service types, so these
// tests never rely on it.
func servicesSetup(t *testing.T) *env {
	t.Helper()
	b, err := catalog.BuildFor(filepath.Join("..", "catalog", "testdata", "services"), 5, catalog.PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := setupSnapshot(t, bundleFS(b), nil)
	return v
}

// gwManifest is a saved manifest of an app providing llm_gateway: an address
// worked out by the platform, a key issued per consumer and a model the user
// fills in.
const gwManifest = `id: gw
schema: 3
provides:
  - kind: llm_gateway
    endpoint: web
    values:
      base_url: "{{self.internal_url}}/v1"
      api_key: { issued: key, secret: true }
      model: { ask: 在应用里选一个模型，把名字填进来 }
`

const issuedKey = "sk-issued-key-0001"

// providerApp makes app name (catalog id id) installed and live, its saved
// manifest and its service, as an install would leave them, and plugs
// consumer's need "llm" into it with an issued key.
func providerApp(t *testing.T, v *env, name, id string, port int, consumer string) {
	t.Helper()
	ctx := context.Background()
	st := v.w.st
	if _, _, err := st.EnsureApp(ctx, name); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateApp(ctx, name, func(a *state.App) error { a.CatalogID, a.CatalogVersion, a.Port = id, "v1", port; return nil }); err != nil {
		t.Fatal(err)
	}
	d, _, err := st.CreateDeployment(ctx, state.NewDeployment{App: name, SourceKind: state.SourceImage, SourceRef: "img", SourceDigest: "d-" + name})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetCurrentDeployment(ctx, name, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCatalogManifest(ctx, state.CatalogManifest{App: name, CatalogID: id, CatalogVersion: "v1", Manifest: gwManifest,
		SHA256: state.ManifestSHA256(gwManifest), Source: state.ManifestFromInstall}); err != nil {
		t.Fatal(err)
	}
	reg := registry(t, v)
	if _, err := reg.CreateService(ctx, Service{Name: name, Kind: "llm_gateway", Source: state.ServiceFromApp, App: name,
		Values: map[string]string{"base_url": "http://" + name + ":" + strconv.Itoa(port) + "/v1"}}); err != nil {
		t.Fatal(err)
	}
	if consumer != "" {
		if _, err := reg.SetBinding(ctx, Binding{App: consumer, UseName: "llm", Service: name, Issued: map[string]string{"key": issuedKey, "issue_id": "0a1b2c3d"}}); err != nil {
			t.Fatal(err)
		}
	}
}

func registry(t *testing.T, v *env) *Registry {
	t.Helper()
	reg, err := v.e.Registry()
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

func faultCode(err error) string {
	var f *Fault
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestServiceKindsFromCatalog(t *testing.T) {
	v := servicesSetup(t)
	kinds, err := v.e.ServiceKinds(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 3 || kinds[0].Kind != "demo_token" || kinds[1].Kind != "llm_gateway" || kinds[2].Kind != "smtp" {
		t.Fatalf("kinds %+v", kinds)
	}
	demo, smtp := kinds[0], kinds[2]
	if demo.HasTest {
		t.Fatalf("demo_token %+v", demo)
	}
	if !smtp.HasTest {
		t.Fatalf("smtp %+v", smtp)
	}
	port := smtp.Fields[1]
	if port.Key != "port" || port.Type != "int" || port.Default != "587" || port.Options == nil {
		t.Fatalf("port %+v", port)
	}
	tls := smtp.Fields[5]
	if tls.Type != "enum" || tls.Default != "starttls" || strings.Join(tls.Options, ",") != "starttls,tls,none" {
		t.Fatalf("tls %+v", tls)
	}
	if pw := smtp.Fields[3]; !pw.Secret || pw.Required {
		t.Fatalf("password %+v", pw)
	}
	// A catalog that defines no service type: an empty list, not null.
	plain, _ := setupSnapshot(t, bundleFS(fixtureBundle(t, 1, "", []fixApp{{manifest: goodManifest(t, "new-api")}}, nil, nil)), nil)
	kinds, err = plain.e.ServiceKinds(context.Background())
	if err != nil || kinds == nil || len(kinds) != 0 || mustJSON(t, kinds) != "[]" {
		t.Fatalf("no types: %v %v", kinds, err)
	}
}

func TestAddServiceRules(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	add := func(name, kind string, vals map[string]string) (ServiceView, error) {
		return v.e.AddService(ctx, AddServiceRequest{Name: name, Kind: kind, Values: vals})
	}
	sv, err := add("我的邮箱", "smtp", map[string]string{"host": " mail.example.com ", "password": "pw-secret-9z8y", "username": "me"})
	if err != nil {
		t.Fatal(err)
	}
	if sv.Name != "我的邮箱" || sv.Source != "user" || sv.App != "" || sv.Status != ServiceOK || sv.KindLabel != "邮箱（发信）" || len(sv.Missing) != 0 {
		t.Fatalf("view %+v", sv)
	}
	got := map[string]ServiceValueView{}
	for _, x := range sv.Values {
		got[x.Key] = x
	}
	if got["host"].Value != "mail.example.com" || got["port"].Value != "587" || got["tls"].Value != "starttls" ||
		got["password"].Value != Masked+"9z8y" || !got["password"].Secret || got["from"].Set || got["from"].Value != "" {
		t.Fatalf("values %+v", got)
	}
	stored, _ := registry(t, v).GetService(ctx, "我的邮箱")
	if stored.Values["password"] != "pw-secret-9z8y" || stored.Values["port"] != "587" {
		t.Fatalf("stored %+v", stored.Values)
	}

	cases := []struct {
		name, kind string
		vals       map[string]string
		code, says string
	}{
		{"x1", "redis", nil, CodeSvcUnknownKind, "没有 redis 这种服务类型"},
		{"x2", "", nil, CodeSvcBadValue, "要写服务类型"},
		{"", "smtp", map[string]string{"host": "h.example.com"}, CodeSvcBadValue, "起个名字"},
		{"kinds", "smtp", map[string]string{"host": "h.example.com"}, CodeSvcBadValue, "保留的词"},
		{"有 空格", "smtp", map[string]string{"host": "h.example.com"}, CodeSvcBadValue, "不行"},
		{"x3", "smtp", map[string]string{}, CodeSvcBadValue, "还差必填项：服务器地址（host）"},
		{"x4", "smtp", map[string]string{"host": "h.example.com", "nope": "1"}, CodeSvcBadValue, "没有这些项：nope"},
		{"x5", "smtp", map[string]string{"host": "h.example.com", "port": "abc"}, CodeSvcBadValue, "要是整数"},
		{"x6", "smtp", map[string]string{"host": "h.example.com", "tls": "ssl"}, CodeSvcBadValue, "只能是 starttls、tls、none 之一"},
		{"x7", "llm_gateway", map[string]string{"base_url": "api.example.com", "api_key": "k-123456789"}, CodeSvcBadValue, "http:// 或 https://"},
		{"x8", "smtp", map[string]string{"host": "a\nb"}, CodeSvcBadValue, "只能是一行"},
		{"我的邮箱", "smtp", map[string]string{"host": "h.example.com"}, CodeSvcNameTaken, "已经有叫 我的邮箱 的服务"},
		// Addresses a container cannot reach: an item of type url, and the
		// host the kind's test connects to (smtp's host is a plain string).
		{"x9", "llm_gateway", map[string]string{"base_url": "http://127.0.0.1:3000/v1", "api_key": "k-123456789"}, CodeSvcLoopback, "接口地址（base_url） 写的是 127.0.0.1"},
		{"x10", "llm_gateway", map[string]string{"base_url": "http://localhost/v1", "api_key": "k-123456789"}, CodeSvcLoopback, "localhost"},
		{"x11", "llm_gateway", map[string]string{"base_url": "http://[::1]:8080/v1", "api_key": "k-123456789"}, CodeSvcLoopback, "::1"},
		{"x12", "smtp", map[string]string{"host": "127.0.0.1"}, CodeSvcLoopback, "服务器地址（host） 写的是 127.0.0.1"},
		{"x13", "smtp", map[string]string{"host": "localhost:25"}, CodeSvcLoopback, "localhost"},
		{"x14", "demo_token", map[string]string{"url": "http://127.0.0.2/x", "token": "t-123456789"}, CodeSvcLoopback, "127.0.0.2"},
	}
	for _, c := range cases {
		_, err := add(c.name, c.kind, c.vals)
		if faultCode(err) != c.code || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s/%s: want %s %q, got %v", c.name, c.kind, c.code, c.says, err)
		}
		if f := AsFault(err); f.Hint == "" || f.HTTPStatus() < 400 {
			t.Errorf("%s: fault %+v", c.name, f)
		}
	}
	// A secret in a bad value is not quoted back.
	_, err = add("x15", "llm_gateway", map[string]string{"base_url": "https://api.example.com/v1", "api_key": "k-123456789\x01"})
	if faultCode(err) != CodeSvcBadValue || strings.Contains(err.Error(), "k-123456789") {
		t.Fatalf("secret quoted: %v", err)
	}
	// Not loopback: a LAN address, a host name, a url-typed value elsewhere.
	if _, err := add("lan", "llm_gateway", map[string]string{"base_url": "http://192.168.1.20:3000/v1", "api_key": "k-123456789"}); err != nil {
		t.Fatal(err)
	}
	// Booleans and integers are stored in one form.
	if s, err := cleanServiceValue(catalog.ServiceField{Key: "b", Type: catalog.FieldBool}, " Yes "); err != nil || s != "true" {
		t.Fatalf("bool %q %v", s, err)
	}
	if s, err := cleanServiceValue(catalog.ServiceField{Key: "n", Type: catalog.FieldInt}, "+0465"); err != nil || s != "465" {
		t.Fatalf("int %q %v", s, err)
	}
	if _, err := cleanServiceValue(catalog.ServiceField{Key: "s", Type: catalog.FieldString}, strings.Repeat("长", maxServiceValue+1)); err == nil {
		t.Fatal("too long accepted")
	}
}

func TestServiceValuesDecode(t *testing.T) {
	var r AddServiceRequest
	if err := json.Unmarshal([]byte(`{"name":"m","kind":"smtp","values":{"host":"h","port":465,"tls":null,"x":true}}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Values["port"] != "465" || r.Values["tls"] != "" || r.Values["x"] != "true" || r.Values["host"] != "h" {
		t.Fatalf("%+v", r.Values)
	}
	err := json.Unmarshal([]byte(`{"values":{"password":["s3cret-value"]}}`), &r)
	var ve *ServiceValuesError
	if !errors.As(err, &ve) || strings.Contains(err.Error(), "s3cret") || !strings.Contains(BadServiceBody(err).Message, "password") {
		t.Fatalf("list value: %v", err)
	}
	if f := BadServiceBody(errors.New("unexpected EOF")); f.Code != CodeSvcBadValue || strings.Contains(f.Message, "EOF") {
		t.Fatalf("%+v", f)
	}
}

func TestServicesListShowsStatusAndUsers(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	providerApp(t, v, "gw", "gw", 3000, "hermes")
	reg := registry(t, v)
	if _, err := reg.CreateService(ctx, Service{Name: "半截", Kind: "smtp", Source: state.ServiceFromUser, Values: map[string]string{"port": "25"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.CreateService(ctx, Service{Name: "旧的", Kind: "pop3", Source: state.ServiceFromUser, Values: map[string]string{"host": "pop.example.com", "secret_word": "very-secret-1"}}); err != nil {
		t.Fatal(err)
	}
	list, err := v.e.Services(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Services) != 3 || list.Unreadable != nil {
		t.Fatalf("%+v", list)
	}
	by := map[string]ServiceView{}
	for _, s := range list.Services {
		by[s.Name] = s
	}
	gw := by["gw"]
	if gw.Source != "app" || gw.App != "gw" || gw.Status != ServiceOK || strings.Join(gw.IssuedPerConsumer, ",") != "api_key" ||
		len(gw.UsedBy) != 1 || gw.UsedBy[0] != (ServiceUse{App: "hermes", Use: "llm", Gone: true}) {
		t.Fatalf("gw %+v", gw)
	}
	// The issued key is no value of the service; the model is the user's to fill in.
	if len(gw.Values) != 2 || gw.Values[0].Key != "base_url" || gw.Values[0].Value != "http://gw:3000/v1" ||
		gw.Values[1].Key != "model" || gw.Values[1].Set || gw.Values[1].Ask != "在应用里选一个模型，把名字填进来" {
		t.Fatalf("gw values %+v", gw.Values)
	}
	if h := by["半截"]; h.Status != ServiceIncomplete || strings.Join(h.Missing, ",") != "host" || h.StatusLabel == "" || h.UsedBy == nil || len(h.UsedBy) != 0 {
		t.Fatalf("incomplete %+v", h)
	}
	// A kind the catalog no longer defines: every value masked.
	old := by["旧的"]
	if old.KindLabel != "pop3" || old.Status != ServiceOK || len(old.Values) != 2 || old.Values[0].Value != Masked+".com" || !old.Values[1].Secret {
		t.Fatalf("unknown kind %+v", old)
	}
	text := mustJSON(t, list)
	for _, secret := range []string{issuedKey, "very-secret-1", "pop.example.com"} {
		if strings.Contains(text, secret) {
			t.Fatalf("%s in %s", secret, text)
		}
	}
	// The provider stopped, then gone.
	if _, err := v.w.st.UpdateApp(ctx, "gw", func(a *state.App) error { a.Desired = state.DesiredStopped; return nil }); err != nil {
		t.Fatal(err)
	}
	if s, _ := v.e.Service(ctx, "gw"); s.Status != ServiceProviderStopped {
		t.Fatalf("stopped %+v", s.ServiceHealth)
	}
	if err := v.w.st.DeleteApp(ctx, "gw", false); err != nil {
		t.Fatal(err)
	}
	if s, _ := v.e.Service(ctx, "gw"); s.Status != ServiceProviderGone {
		t.Fatalf("gone %+v", s.ServiceHealth)
	}
	if _, err := v.e.Service(ctx, "nope"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// An empty registry lists nothing, not null.
	empty := servicesSetup(t)
	l, err := empty.e.Services(ctx)
	if err != nil || mustJSON(t, l) != `{"services":[]}` {
		t.Fatalf("%s %v", mustJSON(t, l), err)
	}
}

func TestPatchServiceRules(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	if _, err := v.e.AddService(ctx, AddServiceRequest{Name: "mail", Kind: "smtp", Values: map[string]string{"host": "mail.example.com", "password": "pw-secret-9z8y"}}); err != nil {
		t.Fatal(err)
	}
	reg := registry(t, v)
	if _, err := reg.SetBinding(ctx, Binding{App: "memos", UseName: "mail", Service: "mail"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetBinding(ctx, Binding{App: "blog", UseName: "smtp", Service: "mail"}); err != nil {
		t.Fatal(err)
	}
	p, err := v.e.PatchService(ctx, "mail", PatchServiceRequest{Values: ServiceValues{"port": "465", "tls": "tls", "password": "new-secret-4455", "username": "me"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Notes) != 1 || p.Notes[0] != "有 2 个应用在用它，重新应用设置后才生效：blog、memos" {
		t.Fatalf("notes %q", p.Notes)
	}
	s, _ := reg.GetService(ctx, "mail")
	if s.Values["port"] != "465" || s.Values["tls"] != "tls" || s.Values["password"] != "new-secret-4455" || s.Values["host"] != "mail.example.com" {
		t.Fatalf("%+v", s.Values)
	}
	if strings.Contains(mustJSON(t, p), "new-secret-4455") {
		t.Fatal("secret in the answer")
	}
	// Clearing: a default comes back, an optional item goes, a required one stays.
	p, err = v.e.PatchService(ctx, "mail", PatchServiceRequest{Values: ServiceValues{"port": "", "username": ""}})
	if err != nil {
		t.Fatal(err)
	}
	s, _ = reg.GetService(ctx, "mail")
	if s.Values["port"] != "587" || s.Values["username"] != "" || len(p.Notes) != 2 || !strings.Contains(p.Notes[1], "回到默认值 587") {
		t.Fatalf("%+v %q", s.Values, p.Notes)
	}
	for _, c := range []struct {
		vals ServiceValues
		code string
		says string
	}{
		{ServiceValues{"host": ""}, CodeSvcBadValue, "必填项，不能清掉"},
		{ServiceValues{}, CodeSvcBadValue, "至少写一项"},
		{ServiceValues{"nope": "1"}, CodeSvcBadValue, "没有这些项：nope"},
		{ServiceValues{"port": "x"}, CodeSvcBadValue, "要是整数"},
		{ServiceValues{"host": "127.0.0.1"}, CodeSvcLoopback, "127.0.0.1"},
	} {
		if _, err := v.e.PatchService(ctx, "mail", PatchServiceRequest{Values: c.vals}); faultCode(err) != c.code || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%v: %v", c.vals, err)
		}
	}
	if s2, _ := reg.GetService(ctx, "mail"); s2.Values["host"] != "mail.example.com" {
		t.Fatal("a refused change was written")
	}
	if _, err := v.e.PatchService(ctx, "nope", PatchServiceRequest{Values: ServiceValues{"a": "b"}}); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	// No user: no note.
	if _, err := v.e.AddService(ctx, AddServiceRequest{Name: "lone", Kind: "smtp", Values: map[string]string{"host": "h.example.com"}}); err != nil {
		t.Fatal(err)
	}
	p, err = v.e.PatchService(ctx, "lone", PatchServiceRequest{Values: ServiceValues{"from": "a@example.com"}})
	if err != nil || p.Notes == nil || len(p.Notes) != 0 || !strings.Contains(mustJSON(t, p), `"notes":[]`) {
		t.Fatalf("%+v %v", p.Notes, err)
	}
}

func TestPatchAppServiceOnlyAskItems(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	providerApp(t, v, "gw", "gw", 3000, "hermes")
	p, err := v.e.PatchService(ctx, "gw", PatchServiceRequest{Values: ServiceValues{"model": " deepseek-chat "}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Values[1].Value != "deepseek-chat" || !p.Values[1].Set || len(p.Notes) != 1 || !strings.Contains(p.Notes[0], "hermes") {
		t.Fatalf("%+v %q", p.Values, p.Notes)
	}
	for _, vals := range []ServiceValues{{"base_url": "http://other:1/v1"}, {"api_key": "sk-mine-123456"}} {
		_, err := v.e.PatchService(ctx, "gw", PatchServiceRequest{Values: vals})
		if faultCode(err) != CodeSvcFromApp || !strings.Contains(err.Error(), "只有要你在应用里做一步再填进来的这几项能填：model") || strings.Contains(err.Error(), "sk-mine") {
			t.Fatalf("%v: %v", vals, err)
		}
	}
	if _, err := v.e.PatchService(ctx, "gw", PatchServiceRequest{Values: ServiceValues{"zzz": "1"}}); faultCode(err) != CodeSvcBadValue {
		t.Fatalf("unknown item: %v", err)
	}
	// Clearing an ask item is allowed.
	if p, err = v.e.PatchService(ctx, "gw", PatchServiceRequest{Values: ServiceValues{"model": ""}}); err != nil || p.Values[1].Set {
		t.Fatalf("%+v %v", p.Values, err)
	}
	if err := v.w.st.DeleteApp(ctx, "gw", false); err != nil {
		t.Fatal(err)
	}
	if _, err := v.e.PatchService(ctx, "gw", PatchServiceRequest{Values: ServiceValues{"model": "x"}}); faultCode(err) != CodeSvcFromApp || !strings.Contains(err.Error(), "已经删了") {
		t.Fatalf("gone: %v", err)
	}
}

func TestRemoveServiceRules(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	providerApp(t, v, "gw", "gw", 3000, "")
	if _, err := v.e.AddService(ctx, AddServiceRequest{Name: "mail", Kind: "smtp", Values: map[string]string{"host": "mail.example.com"}}); err != nil {
		t.Fatal(err)
	}
	reg := registry(t, v)
	if _, _, err := v.w.st.EnsureApp(ctx, "memos"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetBinding(ctx, Binding{App: "memos", UseName: "mail", Service: "mail"}); err != nil {
		t.Fatal(err)
	}
	_, err := v.e.RemoveService(ctx, "mail")
	var inUse *ServiceInUseFault
	if !errors.As(err, &inUse) || faultCode(err) != CodeSvcInUse || len(inUse.UsedBy) != 1 || inUse.UsedBy[0] != (ServiceUse{App: "memos", Use: "mail"}) ||
		!strings.Contains(err.Error(), "memos（mail）") || AsFault(err).HTTPStatus() != http.StatusConflict {
		t.Fatalf("in use: %v", err)
	}
	if _, err := reg.GetService(ctx, "mail"); err != nil {
		t.Fatal("removed although in use")
	}
	if _, err := reg.DeleteBinding(ctx, "memos", "mail"); err != nil {
		t.Fatal(err)
	}
	if res, err := v.e.RemoveService(ctx, "mail"); err != nil || res.Name != "mail" || len(res.Notes) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := v.e.RemoveService(ctx, "mail"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("twice: %v", err)
	}
	// An app's service goes with the app; once the app is gone it can go.
	if _, err := v.e.RemoveService(ctx, "gw"); faultCode(err) != CodeSvcFromApp || !strings.Contains(err.Error(), "acornfox delete gw") {
		t.Fatalf("app's: %v", err)
	}
	// Its provider deleted with its data kept, and only apps deleted the
	// same way still plugged into it: they do not keep it; their bindings go
	// with it, and that is said. An installed app does keep it.
	if _, err := reg.SetBinding(ctx, Binding{App: "old-consumer", UseName: "llm", Service: "gw", Issued: map[string]string{"key": "k-1234567890", "issue_id": "0a1b2c3d"}}); err != nil {
		t.Fatal(err)
	}
	if err := v.w.st.DeleteApp(ctx, "gw", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.w.st.EnsureApp(ctx, "live-consumer"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetBinding(ctx, Binding{App: "live-consumer", UseName: "llm", Service: "gw"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.e.RemoveService(ctx, "gw"); faultCode(err) != CodeSvcInUse || !strings.Contains(err.Error(), "live-consumer") || strings.Contains(err.Error(), "old-consumer") {
		t.Fatalf("an installed consumer keeps it: %v", err)
	}
	if _, err := reg.GetBinding(ctx, "old-consumer", "llm"); err != nil {
		t.Fatal("a refused removal dropped a binding")
	}
	if _, err := reg.DeleteBinding(ctx, "live-consumer", "llm"); err != nil {
		t.Fatal(err)
	}
	// The token given to old-consumer is still in gw's kept data: the user
	// hears (CAT-INST-35) what to remove there, by its number, never its
	// value.
	res, err := v.e.RemoveService(ctx, "gw")
	if err != nil || len(res.Notes) != 2 || !strings.Contains(res.Notes[0], "old-consumer") || !strings.Contains(res.Notes[0], "重新接服务") ||
		!strings.HasPrefix(res.Notes[1], CodeRevoke+" ") || !strings.Contains(res.Notes[1], "old-consumer") || !strings.Contains(res.Notes[1], "0a1b2c3d") ||
		!strings.Contains(res.Notes[1], "留下的数据") || strings.Contains(strings.Join(res.Notes, "；"), "k-1234567890") {
		t.Fatalf("gone provider, gone consumer: %+v %v", res, err)
	}
	if _, err := reg.GetBinding(ctx, "old-consumer", "llm"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("the gone consumer's binding stayed: %v", err)
	}
	if _, err := reg.GetService(ctx, "gw"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("the service stayed: %v", err)
	}
}

// listenPort returns a port with something listening, and one with nothing.
func listenPorts(t *testing.T) (open, closed int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	l2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed = l2.Addr().(*net.TCPAddr).Port
	l2.Close()
	return l.Addr().(*net.TCPAddr).Port, closed
}

func TestTestServiceTCPAndHTTP(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	reg := registry(t, v)
	open, closed := listenPorts(t)
	// The registry is written directly: these listen on this machine, which
	// registration refuses.
	mk := func(name, kind string, vals map[string]string) {
		t.Helper()
		if _, err := reg.CreateService(ctx, Service{Name: name, Kind: kind, Source: state.ServiceFromUser, Values: vals}); err != nil {
			t.Fatal(err)
		}
	}
	mk("up", "smtp", map[string]string{"host": "127.0.0.1", "port": strconv.Itoa(open), "password": "pw-secret-9z8y"})
	mk("down", "smtp", map[string]string{"host": "127.0.0.1", "port": strconv.Itoa(closed)})
	r, err := v.e.TestService(ctx, "up")
	if err != nil || !r.OK || r.Code != "" || r.Target != "127.0.0.1:"+strconv.Itoa(open) || r.Detail != "连上了 127.0.0.1:"+strconv.Itoa(open) {
		t.Fatalf("up: %+v %v", r, err)
	}
	r, _ = v.e.TestService(ctx, "down")
	if r.OK || r.Skipped || r.Code != CodeSvcTestFailed || !strings.Contains(r.Detail, "拒绝连接") || !strings.Contains(r.Detail, strconv.Itoa(closed)) {
		t.Fatalf("down: %+v", r)
	}

	key := "sk-user-key-7788"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)
	mk("llm", "llm_gateway", map[string]string{"base_url": srv.URL + "/v1", "api_key": key})
	mk("llm-bad", "llm_gateway", map[string]string{"base_url": srv.URL + "/v1?token=" + key, "api_key": "sk-wrong-key-0000"})
	r, _ = v.e.TestService(ctx, "llm")
	if !r.OK || r.Detail != "GET "+srv.URL+"/v1/models 回了 200" {
		t.Fatalf("http: %+v", r)
	}
	r, _ = v.e.TestService(ctx, "llm-bad")
	if r.OK || r.Code != CodeSvcTestFailed || !strings.Contains(r.Detail, "回了 401，要的是 200") {
		t.Fatalf("http 401: %+v", r)
	}
	for _, s := range []string{key, "sk-wrong-key-0000"} {
		if strings.Contains(mustJSON(t, r), s) {
			t.Fatalf("secret %s in %+v", s, r)
		}
	}
	// No test in the kind; an item without a value.
	mk("demo", "demo_token", map[string]string{"url": "http://x.example.com", "token": "t-1234567890"})
	r, _ = v.e.TestService(ctx, "demo")
	if r.OK || !r.Skipped || r.Code != "" || !strings.Contains(r.Detail, "这种服务类型没有试连的办法") {
		t.Fatalf("no test: %+v", r)
	}
	mk("nohost", "smtp", map[string]string{"port": "25"})
	r, _ = v.e.TestService(ctx, "nohost")
	if r.OK || !r.Skipped || !strings.Contains(r.Detail, "服务器地址（host） 还没有值") {
		t.Fatalf("no host: %+v", r)
	}
	if _, err := v.e.TestService(ctx, "nope"); !errors.Is(err, state.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

// A service an app provides is reached through the port its container
// publishes on this machine, with what the first consumer was issued.
func TestTestAppServiceThroughPublishedPort(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Header.Get("Authorization") == "Bearer "+issuedKey {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	v.w.servers["gw"] = srv
	providerApp(t, v, "gw", "gw", 3000, "hermes")
	r, err := v.e.TestService(ctx, "gw")
	hp := strconv.Itoa(v.w.hostPort("gw"))
	if err != nil || !r.OK || r.Target != "http://127.0.0.1:"+hp+"/v1/models" || !strings.Contains(r.Detail, "容器里的地址是 gw:3000") || strings.Contains(mustJSON(t, r), issuedKey) {
		t.Fatalf("%+v %v", r, err)
	}
	// Nobody uses it yet: the key is issued per consumer, nothing to try with.
	if _, err := registry(t, v).DeleteBinding(ctx, "hermes", "llm"); err != nil {
		t.Fatal(err)
	}
	r, _ = v.e.TestService(ctx, "gw")
	if r.OK || !r.Skipped || !strings.Contains(r.Detail, "单独发的，现在还没有应用在用它") || !strings.Contains(r.Detail, "acornfox status gw") {
		t.Fatalf("no consumer: %+v", r)
	}
	// Stopped: say so and point at the app.
	if _, err := v.w.st.UpdateApp(ctx, "gw", func(a *state.App) error { a.Desired = state.DesiredStopped; return nil }); err != nil {
		t.Fatal(err)
	}
	r, _ = v.e.TestService(ctx, "gw")
	if r.OK || !r.Skipped || !strings.Contains(r.Detail, "这条服务由应用 gw 提供，那个应用停着：请在应用详情里看它的状态") {
		t.Fatalf("stopped: %+v", r)
	}
	// Running but not published here (no host port): cannot be reached.
	if _, err := v.w.st.UpdateApp(ctx, "gw", func(a *state.App) error { a.Desired = state.DesiredRunning; return nil }); err != nil {
		t.Fatal(err)
	}
	delete(v.w.servers, "gw")
	r, _ = v.e.TestService(ctx, "gw")
	if r.OK || !r.Skipped || !strings.Contains(r.Detail, "请在应用详情里看它的状态") {
		t.Fatalf("not published: %+v", r)
	}
}

func TestServiceFaultsJoinTheTable(t *testing.T) {
	for _, code := range []string{CodeSvcUnknownKind, CodeSvcBadValue, CodeSvcNameTaken, CodeSvcInUse, CodeSvcFromApp, CodeSvcTestFailed, CodeSvcLoopback} {
		f := fault(code, nil, "x")
		if f.Hint == "" || f.HTTPStatus() == http.StatusInternalServerError {
			t.Errorf("%s: %+v %d", code, f, f.HTTPStatus())
		}
	}
	if s := fault(CodeSvcNameTaken, nil, "x").HTTPStatus(); s != http.StatusConflict {
		t.Fatal(s)
	}
	if s := fault(CodeBadParams, nil, "x").HTTPStatus(); s != http.StatusBadRequest {
		t.Fatal("other codes unchanged", s)
	}
}

func TestLoopbackHost(t *testing.T) {
	for h, want := range map[string]bool{"127.0.0.1": true, "127.1.2.3": true, "localhost": true, "LOCALHOST.": true, "a.localhost": true, "::1": true, "[::1]": true,
		"0.0.0.0": true, "192.168.1.2": false, "mail.example.com": false, "10.0.0.1": false, "": false, "host.docker.internal": false,
		// The shorthand numeric forms resolvers accept, and the usual names.
		"127.1": true, "0x7f000001": true, "0X7F.1": true, "2130706433": true, "0": true, "0177.0.0.1": true, "::ffff:127.0.0.1": true,
		"localhost.localdomain": true, "ip6-localhost": true, "ip6-loopback": true, "localhost6": true,
		"10.1": false, "3232235778": false, "0x0a000001": false, "1.2.3.4.5": false, "08.0.0.1": false, "0x": false, "127.0.0.256": false} {
		if loopbackHost(h) != want {
			t.Errorf("%q: want %v", h, want)
		}
	}
}

func TestAddrHost(t *testing.T) {
	for in, want := range map[string]string{"mail.example.com": "mail.example.com", "mail.example.com:25": "mail.example.com", " 10.0.0.2 ": "10.0.0.2",
		"[fd00::5]": "fd00::5", "[fd00::5]:25": "fd00::5", "fd00::5": "fd00::5", "http://127.0.0.1": "127.0.0.1", "smtp://mail.example.com:465": "mail.example.com"} {
		if got := addrHost(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// Every loopback spelling is refused, in a url item and in the host the
// kind's test connects to.
func TestAddServiceRefusesLoopbackSpellings(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	for i, u := range []string{"http://127.1:8080/v1", "http://0x7f000001/v1", "http://2130706433/v1", "http://0/v1", "http://0177.0.0.1/v1",
		"http://localhost.localdomain/v1", "http://ip6-localhost/v1", "http://[::ffff:127.0.0.1]/v1"} {
		_, err := v.e.AddService(ctx, AddServiceRequest{Name: "llm" + strconv.Itoa(i), Kind: "llm_gateway", Values: map[string]string{"base_url": u, "api_key": "sk-1234567890"}})
		if faultCode(err) != CodeSvcLoopback {
			t.Errorf("%s: %v", u, err)
		}
	}
	for i, h := range []string{"127.1", "0", "2130706433", "http://127.0.0.1", "localhost:25", "[::1]"} {
		_, err := v.e.AddService(ctx, AddServiceRequest{Name: "mail" + strconv.Itoa(i), Kind: "smtp", Values: map[string]string{"host": h}})
		if faultCode(err) != CodeSvcLoopback {
			t.Errorf("host %s: %v", h, err)
		}
	}
	if _, err := v.e.AddService(ctx, AddServiceRequest{Name: "lan", Kind: "smtp", Values: map[string]string{"host": "[fd00::5]"}}); err != nil {
		t.Fatalf("a LAN v6 address: %v", err)
	}
}

// An item the providing app marks secret is secret even when the kind does
// not mark it: a malformed value is not quoted back, and it is shown masked.
func TestPatchAppServiceSecretByProvider(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	providerApp(t, v, "gw", "gw", 3000, "")
	m := strings.Replace(gwManifest, `base_url: "{{self.internal_url}}/v1"`, `base_url: { ask: 把应用里看到的地址填进来, secret: true }`, 1)
	if err := v.w.st.SetCatalogManifest(ctx, state.CatalogManifest{App: "gw", CatalogID: "gw", CatalogVersion: "v1", Manifest: m,
		SHA256: state.ManifestSHA256(m), Source: state.ManifestFromInstall}); err != nil {
		t.Fatal(err)
	}
	const bad = "not-a-url-SUPERSECRET-9911"
	_, err := v.e.PatchService(ctx, "gw", PatchServiceRequest{Values: ServiceValues{"base_url": bad}})
	if faultCode(err) != CodeSvcBadValue || strings.Contains(err.Error(), "SUPERSECRET") || !strings.Contains(err.Error(), Masked+"9911") {
		t.Fatalf("bad value: %v", err)
	}
	if f := AsFault(err); f == nil || strings.Contains(mustJSON(t, f), "SUPERSECRET") {
		t.Fatalf("fault carries the value: %+v", f)
	}
	p, err := v.e.PatchService(ctx, "gw", PatchServiceRequest{Values: ServiceValues{"base_url": "http://gw:3000/v1-SUPERSECRET-9911"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mustJSON(t, p), "SUPERSECRET") || !p.Values[0].Secret {
		t.Fatalf("shown: %+v", p.Values)
	}
}

// When the providing app's saved manifest cannot be read, nothing says which
// items it issues per consumer: they are not reported missing, and every
// value is shown masked.
func TestAppServiceWithoutReadableManifest(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	providerApp(t, v, "gw", "gw", 3000, "")
	const junk = "provides: [unclosed"
	if err := v.w.st.SetCatalogManifest(ctx, state.CatalogManifest{App: "gw", CatalogID: "gw", CatalogVersion: "v1", Manifest: junk,
		SHA256: state.ManifestSHA256(junk), Source: state.ManifestFromInstall}); err != nil {
		t.Fatal(err)
	}
	sv, err := v.e.Service(ctx, "gw")
	if err != nil {
		t.Fatal(err)
	}
	if sv.Status != ServiceOK || len(sv.Missing) != 0 || sv.Values[0].Key != "base_url" || !sv.Values[0].Secret || strings.Contains(sv.Values[0].Value, "gw:3000") {
		t.Fatalf("%+v", sv)
	}
}

// What the first consumer was issued is scrubbed from a try's answer like
// the service's own secrets.
func TestTestAppServiceScrubsIssuedValues(t *testing.T) {
	v := servicesSetup(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	t.Cleanup(srv.Close)
	v.w.servers["gw"] = srv
	providerApp(t, v, "gw", "gw", 3000, "hermes")
	// A path that happens to carry the issued value.
	if _, err := registry(t, v).UpdateService(ctx, "gw", func(s *Service) error {
		s.Values["base_url"] = "http://gw:3000/" + issuedKey + "/v1"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r, err := v.e.TestService(ctx, "gw")
	if err != nil || r.OK || r.Code != CodeSvcTestFailed || strings.Contains(mustJSON(t, r), issuedKey) || !strings.Contains(r.Detail, Masked+"0001") {
		t.Fatalf("%+v %v", r, err)
	}
}
