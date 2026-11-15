package apiserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// servicesServer is the trusted handler with an app center whose catalog is
// the services test catalog (three service types; the built-in snapshot has
// two and no apps).
func servicesServer(t *testing.T) (http.Handler, *appinstall.Engine, *state.Store) {
	t.Helper()
	b, err := catalog.BuildFor(filepath.Join("..", "catalog", "testdata", "services"), 5, catalog.PublicCatalog)
	if err != nil {
		t.Fatal(err)
	}
	snap := fstest.MapFS{catalog.IndexFile: {Data: b.Index}, catalog.VersionFile: {Data: []byte("5\n")}}
	for p, data := range b.Packages {
		snap[p] = &fstest.MapFile{Data: data}
	}
	dir := t.TempDir()
	db, err := state.Open(state.Config{Path: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	eng := appinstall.New(appinstall.Config{Store: db, Runner: refusingCatalogRunner{}, DataDir: dir,
		Catalog: appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog"), Snapshot: snap, Sources: []string{"http://127.0.0.1:1"}})})
	t.Cleanup(eng.Close)
	h := New(Config{Store: newFakeStore(), Kicker: &fakeKicker{}, Runner: newFakeRunner(), UploadDir: dir, HostMetrics: fakeHostSized(8192, 4), AppCenter: eng})
	return h, eng, db
}

var jsonHeader = map[string]string{"Content-Type": "application/json"}

func keysOf(m map[string]any) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

func decodeObj(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, body)
	}
	return m
}

// The fields of the interface agreement, field by field. A service carries
// more: issued_per_consumer (the items its app hands each consumer), and
// dedicated_to, max_consumers, slots_left (who holds it, how many more apps
// can be plugged in); ready_say, ready_detail only when it is not_ready.
const (
	kindKeys    = "fields,has_test,kind,label,summary"
	fieldKeys   = "default,hint,key,label,options,required,secret,type"
	serviceKeys = "app,created_at,dedicated_to,issued_per_consumer,kind,kind_label,max_consumers,missing,name,slots_left,source,status,status_label,updated_at,used_by,values"
	valueKeys   = "ask,key,label,secret,set,value"
)

func TestServiceRoutesShapes(t *testing.T) {
	h, eng, db := servicesServer(t)
	ctx := context.Background()
	const secret = "pw-secret-9z8y"
	var bodies []string
	call := func(method, path, body string) (int, string) {
		t.Helper()
		var b []byte
		if body != "" {
			b = []byte(body)
		}
		w := do(t, h, method, path, b, jsonHeader)
		bodies = append(bodies, w.Body.String())
		return w.Code, w.Body.String()
	}

	code, body := call("GET", "/v1/services/kinds", "")
	m := decodeObj(t, body)
	kinds := m["kinds"].([]any)
	if code != http.StatusOK || keysOf(m) != "kinds" || len(kinds) != 3 {
		t.Fatalf("kinds: %d %s", code, body)
	}
	demo := kinds[0].(map[string]any)
	if keysOf(demo) != kindKeys || keysOf(demo["fields"].([]any)[0].(map[string]any)) != fieldKeys {
		t.Fatalf("kind shape: %s", body)
	}
	// A kind says nothing about which catalog apps could provide it: apps do
	// not depend on each other.
	if strings.Contains(body, "providers") || strings.Contains(body, "demo-provider") {
		t.Fatalf("kinds must not name providing apps: %s", body)
	}
	if port := kinds[2].(map[string]any)["fields"].([]any)[1].(map[string]any); port["default"] != "587" || port["type"] != "int" {
		t.Fatalf("defaults are text: %v", port)
	}

	// Register: 201 and the service object; values as text, numbers taken too.
	name := "我的邮箱"
	path := "/v1/services/" + url.PathEscape(name)
	code, body = call("POST", "/v1/services", `{"name":"`+name+`","kind":"smtp","values":{"host":"mail.example.com","port":465,"password":"`+secret+`"}}`)
	sv := decodeObj(t, body)
	if code != http.StatusCreated || keysOf(sv) != serviceKeys || sv["status"] != "ok" || sv["source"] != "user" || sv["app"] != "" || sv["kind_label"] != "邮箱（发信）" {
		t.Fatalf("add: %d %s", code, body)
	}
	vals := sv["values"].([]any)
	for _, x := range vals {
		if keysOf(x.(map[string]any)) != valueKeys {
			t.Fatalf("value shape: %v", x)
		}
	}
	if pw := vals[3].(map[string]any); pw["key"] != "password" || pw["value"] != appinstall.Masked+"9z8y" || pw["secret"] != true || pw["set"] != true {
		t.Fatalf("password: %v", pw)
	}
	if p := vals[1].(map[string]any); p["value"] != "465" {
		t.Fatalf("port: %v", p)
	}
	for _, k := range []string{"missing", "used_by", "issued_per_consumer"} {
		if _, ok := sv[k].([]any); !ok {
			t.Fatalf("%s must be a list, not null: %s", k, body)
		}
	}

	// One service: the same object, not wrapped; the list wraps.
	code, body = call("GET", path, "")
	if code != http.StatusOK || keysOf(decodeObj(t, body)) != serviceKeys {
		t.Fatalf("get: %d %s", code, body)
	}
	code, body = call("GET", "/v1/services", "")
	if l := decodeObj(t, body); code != http.StatusOK || keysOf(l) != "services" || len(l["services"].([]any)) != 1 {
		t.Fatalf("list: %d %s", code, body)
	}

	// Change: the service plus notes, who uses it (an installed app).
	if _, _, err := db.EnsureApp(ctx, "memos"); err != nil {
		t.Fatal(err)
	}
	reg, _ := eng.Registry()
	if _, err := reg.SetBinding(ctx, appinstall.Binding{App: "memos", UseName: "mail", Service: name}); err != nil {
		t.Fatal(err)
	}
	code, body = call("PATCH", path, `{"values":{"tls":"tls","password":"new-secret-4455"}}`)
	pm := decodeObj(t, body)
	if code != http.StatusOK || keysOf(pm) != "app,created_at,dedicated_to,issued_per_consumer,kind,kind_label,max_consumers,missing,name,notes,slots_left,source,status,status_label,updated_at,used_by,values" {
		t.Fatalf("patch: %d %s", code, body)
	}
	if n := pm["notes"].([]any); len(n) != 1 || n[0] != "有 1 个应用在用它，重新应用设置后才生效：memos" {
		t.Fatalf("notes: %v", n)
	}
	if u := pm["used_by"].([]any); len(u) != 1 || keysOf(u[0].(map[string]any)) != "app,use" {
		t.Fatalf("used_by: %v", u)
	}

	// Delete while in use: CAT-SVC-04 with used_by in the error.
	code, body = call("DELETE", path, "")
	e := decodeObj(t, body)["error"].(map[string]any)
	if code != http.StatusConflict || e["code"] != appinstall.CodeSvcInUse || e["hint"] == "" {
		t.Fatalf("delete in use: %d %s", code, body)
	}
	if u := e["used_by"].([]any); len(u) != 1 || u[0].(map[string]any)["app"] != "memos" || u[0].(map[string]any)["use"] != "mail" {
		t.Fatalf("used_by in error: %v", e)
	}
	if _, err := reg.DeleteBinding(ctx, "memos", "mail"); err != nil {
		t.Fatal(err)
	}
	if code, body = call("DELETE", path, ""); code != http.StatusNoContent || body != "" {
		t.Fatalf("delete: %d %s", code, body)
	}
	code, body = call("GET", path, "")
	wantCode(t, code, body, http.StatusNotFound, "not_found")
	code, body = call("POST", path+"/test", "")
	wantCode(t, code, body, http.StatusNotFound, "not_found")

	// Refusals carry their code and status.
	for _, c := range []struct {
		body   string
		status int
		code   string
	}{
		{`{"name":"x","kind":"redis"}`, http.StatusBadRequest, appinstall.CodeSvcUnknownKind},
		{`{"name":"x","kind":"smtp","values":{}}`, http.StatusBadRequest, appinstall.CodeSvcBadValue},
		{`{"name":"x","kind":"smtp","values":{"host":"127.0.0.1"}}`, http.StatusBadRequest, appinstall.CodeSvcLoopback},
		{`{"name":"x","kind":"smtp","values":{"password":{"v":"` + secret + `"}}}`, http.StatusBadRequest, appinstall.CodeSvcBadValue},
		{`not json`, http.StatusBadRequest, appinstall.CodeSvcBadValue},
	} {
		code, body = call("POST", "/v1/services", c.body)
		wantCode(t, code, body, c.status, c.code)
	}
	call("POST", "/v1/services", `{"name":"dup","kind":"smtp","values":{"host":"h.example.com"}}`)
	code, body = call("POST", "/v1/services", `{"name":"dup","kind":"smtp","values":{"host":"h.example.com"}}`)
	wantCode(t, code, body, http.StatusConflict, appinstall.CodeSvcNameTaken)

	for _, b := range bodies {
		for _, s := range []string{secret, "new-secret-4455"} {
			if strings.Contains(b, s) {
				t.Fatalf("a secret went out: %s", b)
			}
		}
	}
}

func TestServiceTestRoute(t *testing.T) {
	h, eng, _ := servicesServer(t)
	ctx := context.Background()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := l.Addr().(*net.TCPAddr).Port
	reg, _ := eng.Registry()
	// Written directly: registration refuses an address on this machine.
	if _, err := reg.CreateService(ctx, appinstall.Service{Name: "local", Kind: "smtp", Source: state.ServiceFromUser,
		Values: map[string]string{"host": "127.0.0.1", "port": strconv.Itoa(port)}}); err != nil {
		t.Fatal(err)
	}
	w := do(t, h, "POST", "/v1/services/local/test", nil, nil)
	r := decodeObj(t, w.Body.String())
	if w.Code != http.StatusOK || r["ok"] != true || r["target"] != "127.0.0.1:"+strconv.Itoa(port) || !strings.HasPrefix(r["detail"].(string), "连上了") {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	if _, ok := r["code"]; ok {
		t.Fatalf("no code when it got through: %s", w.Body.String())
	}
	l.Close()
	w = do(t, h, "POST", "/v1/services/local/test", nil, nil)
	r = decodeObj(t, w.Body.String())
	if w.Code != http.StatusOK || r["ok"] != false || r["code"] != appinstall.CodeSvcTestFailed || !strings.Contains(r["detail"].(string), "拒绝连接") {
		t.Fatalf("test down: %d %s", w.Code, w.Body.String())
	}
}

func TestServiceRoutesWithoutAppCenter(t *testing.T) {
	h, _, _, _, _ := newTestServer(t)
	for _, c := range [][2]string{{"GET", "/v1/services/kinds"}, {"GET", "/v1/services"}, {"GET", "/v1/services/x"}, {"POST", "/v1/services"},
		{"PATCH", "/v1/services/x"}, {"POST", "/v1/services/x/test"}, {"DELETE", "/v1/services/x"}} {
		w := do(t, h, c[0], c[1], nil, nil)
		wantCode(t, w.Code, w.Body.String(), http.StatusNotFound, "app_center_unavailable")
	}
}
