package appinstall

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/cli"
)

// testActions is the executor with a short wait between connection attempts.
func testActions(c *http.Client) actionRunner {
	return actionRunner{http: c, attempts: 5, retryWait: 5 * time.Millisecond, timeout: 5 * time.Second, second: time.Millisecond}
}

// stepsYAML reads steps the way a manifest writes them.
func stepsYAML(t testing.TB, text string) []catalog.Step {
	t.Helper()
	var steps []catalog.Step
	if err := yaml.Unmarshal([]byte(text), &steps); err != nil {
		t.Fatalf("steps: %v", err)
	}
	return steps
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func asActionError(t testing.TB, err error) *ActionError {
	t.Helper()
	var ae *ActionError
	if !errors.As(err, &ae) {
		t.Fatalf("want an *ActionError, got %T %v", err, err)
	}
	return ae
}

// adminEnv is an app with an admin account whose password is a secret
// setting.
func adminEnv(password string) ActionEnv {
	m := &catalog.Manifest{Params: []catalog.Param{{Key: "admin_username", Type: "string"}, {Key: "admin_password", Type: "secret"}}}
	return ActionEnv{
		Lookup:    Values{"admin_username": "admin", "admin_password": password}.lookup,
		SecretRef: secretSettingRefs(m),
	}
}

// ---- the token flow measured on New API, written as steps ----

// fakeTokenApp answers like a model gateway's management API, as measured:
// login gives an access token and a user id, management calls need both as
// headers (the login cookie is not enough), creating a token answers without
// its id, the list is in no particular order, and the value comes from a
// call of its own, without the "sk-" clients send.
type fakeTokenApp struct {
	mu       sync.Mutex
	password string
	tokens   map[int]string // id → name
	nextID   int
	created  []map[string]any
	deleted  []int
	paths    []string
}

// seen is what the app was asked to do so far.
func (f *fakeTokenApp) seen() (paths []string, created []map[string]any, deleted []int, tokens map[int]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tokens = map[int]string{}
	for id, name := range f.tokens {
		tokens[id] = name
	}
	return append([]string(nil), f.paths...), append([]map[string]any(nil), f.created...), append([]int(nil), f.deleted...), tokens
}

func (f *fakeTokenApp) keyOf(id int) string {
	return "K" + strconv.Itoa(id) + strings.Repeat("x", 48-1-len(strconv.Itoa(id)))
}

func (f *fakeTokenApp) handler() http.Handler {
	ok := func(w http.ResponseWriter, data any) {
		reply(w, 200, map[string]any{"success": true, "message": "", "data": data})
	}
	no := func(w http.ResponseWriter, msg string) {
		reply(w, 200, map[string]any{"success": false, "message": msg})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/user/login", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["username"] != "admin" || b["password"] != f.password {
			no(w, "用户名或密码错误")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "s-1", Path: "/"})
		ok(w, map[string]any{"access_token": "at-9", "user": map[string]any{"id": 7}})
	})
	manage := func(fn http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.paths = append(f.paths, r.Method+" "+r.URL.RequestURI())
			f.mu.Unlock()
			if r.Header.Get("Authorization") != "Bearer at-9" || r.Header.Get("New-Api-User") != "7" {
				reply(w, 401, map[string]any{"success": false, "message": "无权进行此操作，未登录且未提供 access token"})
				return
			}
			fn(w, r)
		}
	}
	mux.HandleFunc("POST /api/token/", manage(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.nextID++
		f.tokens[f.nextID] = b["name"].(string)
		f.created = append(f.created, b)
		f.mu.Unlock()
		ok(w, nil)
	}))
	mux.HandleFunc("GET /api/token/", manage(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		ids := make([]int, 0, len(f.tokens))
		for id := range f.tokens {
			ids = append(ids, id)
		}
		// Newest first, as the page shows them: the wanted one is not last.
		sort.Sort(sort.Reverse(sort.IntSlice(ids)))
		items := []map[string]any{}
		for _, id := range ids {
			items = append(items, map[string]any{"id": id, "name": f.tokens[id]})
		}
		f.mu.Unlock()
		ok(w, map[string]any{"items": items, "total": len(items)})
	}))
	mux.HandleFunc("POST /api/token/{id}/key", manage(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		f.mu.Lock()
		_, have := f.tokens[id]
		f.mu.Unlock()
		if !have {
			no(w, "record not found")
			return
		}
		ok(w, map[string]any{"key": f.keyOf(id)})
	}))
	mux.HandleFunc("GET /api/token/{id}", manage(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		f.mu.Lock()
		name, have := f.tokens[id]
		f.mu.Unlock()
		if !have {
			no(w, "record not found")
			return
		}
		key := f.keyOf(id)
		ok(w, map[string]any{"id": id, "name": name, "status": 1, "key": key[:4] + "**********" + key[len(key)-4:]})
	}))
	mux.HandleFunc("DELETE /api/token/{id}", manage(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		f.mu.Lock()
		f.deleted = append(f.deleted, id)
		delete(f.tokens, id)
		f.mu.Unlock()
		ok(w, nil)
	}))
	return mux
}

// The four measured steps: log in, create the token, find it in the list,
// fetch its value.
const tokenIssueSteps = `
- name: login
  http:
    method: POST
    path: /api/user/login
    json: { username: "{{params.admin_username}}", password: "{{params.admin_password}}" }
    expect_json: { success: true }
    save:
      access_token: { path: data.access_token, secret: true }
      user_id: data.user.id
- name: create
  http:
    method: POST
    path: /api/token/
    headers: { Authorization: "Bearer {{saved.access_token}}", New-Api-User: "{{saved.user_id}}" }
    json: { name: "acornfox-{{consumer.name}}", remain_quota: 0, unlimited_quota: true, expired_time: -1 }
    expect_json: { success: true }
- name: find
  http:
    method: GET
    path: /api/token/?p=0&size=100
    headers: { Authorization: "Bearer {{saved.access_token}}", New-Api-User: "{{saved.user_id}}" }
    expect_json: { success: true }
    save:
      token_id:
        list: data.items
        where: { name: "acornfox-{{consumer.name}}" }
        sort_by: id
        pick: last
        field: id
- name: key
  http:
    method: POST
    path: /api/token/{{saved.token_id}}/key
    headers: { Authorization: "Bearer {{saved.access_token}}", New-Api-User: "{{saved.user_id}}" }
    expect_json: { success: true }
    save:
      api_key: { path: data.key, prefix: "sk-", secret: true }
`

// Revoking: log in, see that the number is still this consumer's token,
// delete it.
const tokenRevokeSteps = `
- name: login
  http:
    method: POST
    path: /api/user/login
    json: { username: "{{params.admin_username}}", password: "{{params.admin_password}}" }
    expect_json: { success: true }
    save:
      access_token: { path: data.access_token, secret: true }
      user_id: data.user.id
- name: same-token
  http:
    method: GET
    path: /api/token/{{issued.token_id}}
    headers: { Authorization: "Bearer {{saved.access_token}}", New-Api-User: "{{saved.user_id}}" }
    expect_json: { success: true, data: { id: "{{issued.token_id}}", name: "acornfox-{{consumer.name}}" } }
- name: delete
  http:
    method: DELETE
    path: /api/token/{{issued.token_id}}
    headers: { Authorization: "Bearer {{saved.access_token}}", New-Api-User: "{{saved.user_id}}" }
    expect_json: { success: true }
`

func TestActionTokenFlowWrittenAsSteps(t *testing.T) {
	// Tokens 9 (an earlier one of the same name) and 8 (another app's) exist:
	// the new one gets 10, which sorts before 9 as text and after it as a
	// number.
	app := &fakeTokenApp{password: "pw-0123456789", tokens: map[int]string{8: "acornfox-other", 9: "acornfox-hermes"}, nextID: 9}
	srv := httptest.NewServer(app.handler())
	defer srv.Close()
	ctx := context.Background()
	env := adminEnv(app.password)
	env.Consumer = &ActionConsumer{Name: "hermes", ID: "app_1"}

	issued, err := testActions(srv.Client()).run(ctx, srv.URL, stepsYAML(t, tokenIssueSteps), env)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	want := map[string]SavedValue{
		"access_token": {Value: "at-9", Secret: true},
		"user_id":      {Value: "7"},
		"token_id":     {Value: "10"},
		"api_key":      {Value: "sk-" + app.keyOf(10), Secret: true},
	}
	if !reflect.DeepEqual(issued, want) {
		t.Fatalf("issued\n got %+v\nwant %+v", issued, want)
	}
	if len(issued["api_key"].Value) != 3+48 {
		t.Fatalf("key length %d", len(issued["api_key"].Value))
	}
	// The create request is the one the measured flow sends: numbers and
	// booleans stay what they are.
	wantBody := map[string]any{"name": "acornfox-hermes", "remain_quota": 0.0, "unlimited_quota": true, "expired_time": -1.0}
	paths, created, _, _ := app.seen()
	if len(created) != 1 || !reflect.DeepEqual(created[0], wantBody) {
		t.Fatalf("create body %+v", created)
	}
	wantPaths := []string{"POST /api/token/", "GET /api/token/?p=0&size=100", "POST /api/token/10/key"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("calls %v", paths)
	}

	// Revoke with what issue saved.
	env.Issued = issued
	saved, err := testActions(srv.Client()).run(ctx, srv.URL, stepsYAML(t, tokenRevokeSteps), env)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, _, deleted, tokens := app.seen(); !reflect.DeepEqual(deleted, []int{10}) || tokens[10] != "" || tokens[9] != "acornfox-hermes" {
		t.Fatalf("deleted %v, tokens %v", deleted, tokens)
	}
	if _, has := saved["token_id"]; has || saved["access_token"].Value != "at-9" {
		t.Fatalf("revoke saved %+v", saved)
	}

	// The number now belongs to nothing: the check step says so and nothing
	// is deleted.
	_, err = testActions(srv.Client()).run(ctx, srv.URL, stepsYAML(t, tokenRevokeSteps), env)
	ae := asActionError(t, err)
	if _, _, deleted, _ := app.seen(); ae.Kind != ActionExpect || ae.Step != 2 || ae.Name != "same-token" || ae.AppMessage != "record not found" || len(deleted) != 1 {
		t.Fatalf("revoke of a gone token: %+v (deleted %v)", ae, deleted)
	}

	// A wrong admin password: the error says which step, what the app said,
	// and holds neither the password nor anything else.
	bad := adminEnv("pw-wrong-000")
	bad.Consumer = env.Consumer
	part, err := testActions(srv.Client()).run(ctx, srv.URL, stepsYAML(t, tokenIssueSteps), bad)
	ae = asActionError(t, err)
	if ae.Kind != ActionExpect || ae.Step != 1 || !ae.Answered() || len(part) != 0 {
		t.Fatalf("bad password: %+v saved %v", ae, part)
	}
	if msg := err.Error(); msg != "第 1 步（login）：应用的应答和预期不符（HTTP 200），应用说：用户名或密码错误" {
		t.Fatalf("message %q", msg)
	}
}

// ---- sessions, headers, templates ----

// Login sets a cookie, the next step is let in by it; another group starts
// without it.
func TestActionCookieSession(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "c-42", Path: "/"})
		reply(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("sid")
		if err != nil || c.Value != "c-42" {
			reply(w, 401, map[string]any{"message": "not logged in"})
			return
		}
		reply(w, 200, map[string]any{"name": "admin"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := context.Background()
	both := stepsYAML(t, `
- name: login
  http: { method: POST, path: /login }
- name: me
  http: { method: GET, path: /me, save: { who: name } }
`)
	saved, err := testActions(srv.Client()).run(ctx, srv.URL, both, ActionEnv{})
	if err != nil || saved["who"].Value != "admin" {
		t.Fatalf("with cookie: %v %v", saved, err)
	}
	_, err = testActions(srv.Client()).run(ctx, srv.URL, both[1:], ActionEnv{})
	ae := asActionError(t, err)
	if ae.Kind != ActionStatus || ae.Status != 401 || ae.Step != 1 || ae.AppMessage != "not logged in" {
		t.Fatalf("a new group must start without the cookie: %+v", ae)
	}
	if srv.Client().Jar != nil {
		t.Fatal("the caller's client was given a jar")
	}
}

// Templates are replaced in the path (escaped), headers, json, expect_json
// and save.where.
func TestActionTemplatesEverywhere(t *testing.T) {
	var got struct {
		sync.Mutex
		path, query, header, ctype string
		body                       map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Lock()
		defer got.Unlock()
		got.path, got.query = r.URL.EscapedPath(), r.URL.Query().Get("for")
		got.header, got.ctype = r.Header.Get("X-Tenant"), r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		reply(w, 200, map[string]any{"owner": "a/b?c", "items": []any{
			map[string]any{"owner": "someone", "code": "no"},
			map[string]any{"owner": "a/b?c", "code": "yes"},
		}})
	}))
	defer srv.Close()
	env := ActionEnv{
		Lookup:   Values{"region": "cn", "depth": "3"}.lookup,
		Consumer: &ActionConsumer{Name: "a/b?c", ID: "app_7"},
		Issued:   map[string]SavedValue{"ticket": {Value: "T-1"}},
	}
	steps := stepsYAML(t, `
- http:
    method: PUT
    path: /tenants/{{consumer.name}}/grants/{{issued.ticket}}?for={{consumer.name}}&x=1
    headers: { X-Tenant: "{{params.region}}-{{consumer.id}}" }
    json: { who: "{{consumer.name}}", nested: { list: ["{{params.region}}", 5], depth: "{{params.depth}}" }, keep: 7 }
    expect_json: { owner: "{{consumer.name}}" }
    save:
      code: { list: items, where: { owner: "{{consumer.name}}" }, field: code }
`)
	saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, env)
	if err != nil {
		t.Fatal(err)
	}
	got.Lock()
	defer got.Unlock()
	if got.path != "/tenants/a%2Fb%3Fc/grants/T-1" || got.query != "a/b?c" {
		t.Fatalf("path %q query %q", got.path, got.query)
	}
	if got.header != "cn-app_7" || got.ctype != "application/json" {
		t.Fatalf("header %q content type %q", got.header, got.ctype)
	}
	wantBody := map[string]any{"who": "a/b?c", "nested": map[string]any{"list": []any{"cn", 5.0}, "depth": "3"}, "keep": 7.0}
	if !reflect.DeepEqual(got.body, wantBody) {
		t.Fatalf("body %+v", got.body)
	}
	if saved["code"].Value != "yes" {
		t.Fatalf("saved %+v", saved)
	}
}

// A request without json has no body and no Content-Type; a header of the
// step replaces the default one.
func TestActionBodyAndContentType(t *testing.T) {
	var mu sync.Mutex
	var ctypes []string
	var lengths []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		ctypes, lengths = append(ctypes, r.Header.Get("Content-Type")), append(lengths, r.ContentLength)
		w.WriteHeader(204)
	}))
	defer srv.Close()
	steps := stepsYAML(t, `
- http: { method: POST, path: /a }
- http: { method: PATCH, path: /b, json: {} }
- http: { method: POST, path: /c, json: { a: 1 }, headers: { Content-Type: application/merge-patch+json } }
`)
	if _, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, ActionEnv{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(ctypes, []string{"", "application/json", "application/merge-patch+json"}) || !reflect.DeepEqual(lengths, []int64{0, 2, 7}) {
		t.Fatalf("content types %q lengths %v", ctypes, lengths)
	}
}

// ---- save ----

func saveServer(t *testing.T, answer string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestActionSaveThreeForms(t *testing.T) {
	srv := saveServer(t, `{"data": {
		"access_token": "at-1", "big": 9007199254740993, "ratio": 1.50, "on": true,
		"user": {"id": 12}, "key": "abc", "prefixed": "sk-def",
		"items": [{"id": 3, "name": "x", "tags": ["a", "b"]}, {"id": 4, "name": "y"}]
	}}`)
	steps := stepsYAML(t, `
- http:
    method: GET
    path: /
    save:
      access_token: data.access_token
      user_id: { path: data.user.id }
      api_key: { path: data.key, prefix: "sk-", secret: true }
      kept_prefix: { path: data.prefixed, prefix: "sk-" }
      big: data.big
      ratio: data.ratio
      on: data.on
      by_index: data.items.1.name
      tag: data.items.0.tags.1
      item_id: { list: data.items, where: { name: y }, field: id, secret: true, prefix: "id-" }
`)
	saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, ActionEnv{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]SavedValue{
		"access_token": {Value: "at-1"},
		"user_id":      {Value: "12"},
		"api_key":      {Value: "sk-abc", Secret: true},
		"kept_prefix":  {Value: "sk-def"},
		"big":          {Value: "9007199254740993"}, // past 2^53: the digits as the app wrote them
		"ratio":        {Value: "1.50"},
		"on":           {Value: "true"},
		"by_index":     {Value: "y"},
		"tag":          {Value: "b"},
		"item_id":      {Value: "id-4", Secret: true},
	}
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved\n got %+v\nwant %+v", saved, want)
	}
}

// Picking one item of a list: filter, order, first or last.
func TestActionSaveFromList(t *testing.T) {
	srv := saveServer(t, `{"data": {"items": [
		{"id": 10, "name": "acornfox-web", "label": "b", "at": "2024-03-01"},
		{"id": 2,  "name": "someone-else", "label": "z", "at": "2025-01-01"},
		{"id": 9,  "name": "acornfox-web", "label": "c", "at": "2024-01-01"},
		{"id": 100, "name": "acornfox-web", "label": "a", "at": "2023-12-31"}
	]}}`)
	env := ActionEnv{Consumer: &ActionConsumer{Name: "web"}}
	pick := func(spec string) (string, error) {
		t.Helper()
		steps := stepsYAML(t, "- http:\n    method: GET\n    path: /\n    save:\n      got: "+spec+"\n")
		saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, env)
		return saved["got"].Value, err
	}
	const mine = `where: { name: "acornfox-{{consumer.name}}" }`
	for spec, want := range map[string]string{
		// the newest of this consumer's: ids by value, so 100 after 10 after 9
		`{ list: data.items, ` + mine + `, sort_by: id, pick: last, field: id }`:  "100",
		`{ list: data.items, ` + mine + `, sort_by: id, field: id }`:              "100", // last is the default
		`{ list: data.items, ` + mine + `, sort_by: id, pick: first, field: id }`: "9",
		// text sorts as text
		`{ list: data.items, ` + mine + `, sort_by: at, field: id }`:                 "10",
		`{ list: data.items, ` + mine + `, sort_by: label, pick: first, field: id }`: "100",
		// without sort_by: the order of the answer
		`{ list: data.items, ` + mine + `, field: id }`:              "100",
		`{ list: data.items, ` + mine + `, pick: first, field: id }`: "10",
		// without where: every item
		`{ list: data.items, sort_by: id, pick: first, field: name }`: "someone-else",
		// a number in where matches the number in the answer
		`{ list: data.items, where: { id: 9 }, field: label }`: "c",
	} {
		if got, err := pick(spec); err != nil || got != want {
			t.Errorf("%s\n got %q %v, want %q", spec, got, err, want)
		}
	}
	for spec, wantDetail := range map[string]string{
		`{ list: data.items, where: { name: nobody }, field: id }`: "存 got 时，列表 data.items 里没有符合条件的项",
		`{ list: data.nothing, field: id }`:                        "存 got 时，应答里的 data.nothing 不是列表",
		`{ list: data.items, sort_by: missing, field: id }`:        "存 got 时，列表 data.items 里有的项没有 missing，排不了序",
		`{ list: data.items, field: missing }`:                     "存 got 时，应答里没有 data.items 里挑出那一项的 missing",
		`data.missing`:                                             "存 got 时，应答里没有 data.missing",
		`data.items`:                                               "存 got 时，应答里的 data.items 不是一个值（是对象或列表）",
	} {
		_, err := pick(spec)
		if ae := asActionError(t, err); ae.Kind != ActionSaveFailed || ae.Detail != wantDetail || ae.Status != 200 || ae.Answered() {
			t.Errorf("%s\n got %+v, want %q", spec, ae, wantDetail)
		}
	}
}

// Ids written as text (many apps do, to keep the digits) sort by value like
// numbers, also mixed with numbers; text that is no number sorts as text.
func TestActionSortByNumbersWrittenAsText(t *testing.T) {
	pick := func(answer, spec string) string {
		t.Helper()
		srv := saveServer(t, answer)
		steps := stepsYAML(t, "- http:\n    method: GET\n    path: /\n    save:\n      got: "+spec+"\n")
		saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, ActionEnv{})
		if err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
		return saved["got"].Value
	}
	text := `{"items": [{"id": "10", "n": "a"}, {"id": "9", "n": "b"}, {"id": "100", "n": "c"}]}`
	mixed := `{"items": [{"id": 10, "n": "a"}, {"id": "9", "n": "b"}, {"id": 100, "n": "c"}]}`
	huge := `{"items": [{"id": "9007199254740993", "n": "a"}, {"id": "9007199254740992", "n": "b"}]}`
	names := `{"items": [{"id": "10", "n": "a"}, {"id": "9", "n": "b"}, {"id": "v100", "n": "c"}]}`
	for _, tc := range []struct{ answer, spec, want string }{
		{text, `{ list: items, sort_by: id, field: id }`, "100"},
		{text, `{ list: items, sort_by: id, pick: first, field: id }`, "9"},
		{mixed, `{ list: items, sort_by: id, field: n }`, "c"},
		{mixed, `{ list: items, sort_by: id, pick: first, field: n }`, "b"},
		{huge, `{ list: items, sort_by: id, field: n }`, "a"}, // whole numbers exactly, past 2^53
		{names, `{ list: items, sort_by: id, field: id }`, "v100"},
		{names, `{ list: items, sort_by: id, pick: first, field: id }`, "10"},
	} {
		if got := pick(tc.answer, tc.spec); got != tc.want {
			t.Errorf("%s of %s\n got %q, want %q", tc.spec, tc.answer, got, tc.want)
		}
	}
}

// A step saves all of its values or none; an answer that is not JSON or
// holds an empty value cannot be saved from.
func TestActionSaveFailures(t *testing.T) {
	steps := stepsYAML(t, `
- name: first
  http: { method: GET, path: /, save: { a: a } }
- name: second
  http: { method: GET, path: /, save: { b: a, z: missing } }
`)
	srv := saveServer(t, `{"a": "1"}`)
	saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, ActionEnv{})
	ae := asActionError(t, err)
	if ae.Step != 2 || ae.Name != "second" || ae.Kind != ActionSaveFailed {
		t.Fatalf("%+v", ae)
	}
	if !reflect.DeepEqual(saved, map[string]SavedValue{"a": {Value: "1"}}) {
		t.Fatalf("what was saved before the failure: %+v", saved)
	}
	if msg := err.Error(); msg != "第 2 步（second）：存 z 时，应答里没有 missing（HTTP 200）" {
		t.Fatalf("message %q", msg)
	}
	for answer, want := range map[string]string{
		`<html>hello</html>`: "应答不是 JSON，取不到要存的值",
		`{"a": ""}`:          "存 a 时，应答里的 a 是空的",
		`{"a": null}`:        "存 a 时，应答里没有 a",
	} {
		srv := saveServer(t, answer)
		_, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps[:1], ActionEnv{})
		if ae := asActionError(t, err); ae.Kind != ActionSaveFailed || ae.Detail != want {
			t.Errorf("%s: %+v", answer, ae)
		}
	}
}

// ---- expect_status, expect_json ----

// An id a step saved (or issue saved) is text; in the app's answers it is a
// number. They compare equal whatever the id's size: from a million up a
// float prints as 1e+06, and past 2^53 two ids can be the same float.
func TestActionNumbersComparedAsWritten(t *testing.T) {
	for _, id := range []string{"10", "999999", "1000000", "1234567", "9007199254740993", "-1000000"} {
		answer := `{"data": {"id": ` + id + `, "items": [{"id": 1, "name": "other"}, {"id": ` + id + `, "name": "mine"}]}}`
		srv := saveServer(t, answer)
		env := ActionEnv{Issued: map[string]SavedValue{"token_id": {Value: id}}}
		for name, steps := range map[string]string{
			"expect_json with saved": `
- http: { method: GET, path: /, save: { id: data.id } }
- name: same
  http: { method: GET, path: /, expect_json: { data: { id: "{{saved.id}}" } } }
`,
			"where with saved": `
- http: { method: GET, path: /, save: { id: data.id } }
- http: { method: GET, path: /, save: { name: { list: data.items, where: { id: "{{saved.id}}" }, field: name } } }
`,
			"expect_json with issued": `[{ http: { method: GET, path: /, expect_json: { data: { id: "{{issued.token_id}}" } } } }]`,
			"where with issued":       `[{ http: { method: GET, path: /, save: { name: { list: data.items, where: { id: "{{issued.token_id}}" }, field: name } } } }]`,
			"literal":                 `[{ http: { method: GET, path: /, expect_json: { data: { id: ` + id + ` } }, save: { name: { list: data.items, where: { id: ` + id + ` }, field: name } } } }]`,
		} {
			saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, steps), env)
			if err != nil {
				t.Errorf("id %s, %s: %v", id, name, err)
				continue
			}
			if v, ok := saved["name"]; ok && v.Value != "mine" {
				t.Errorf("id %s, %s: picked %q", id, name, v.Value)
			}
		}
	}

	// The neighbour of an id past 2^53 is another id: "is it still that one"
	// must say no, and a revoke must not pick it from a list.
	srv := saveServer(t, `{"data": {"id": 9007199254740992, "items": [{"id": 9007199254740992, "name": "neighbour"}]}}`)
	env := ActionEnv{Issued: map[string]SavedValue{"token_id": {Value: "9007199254740993"}}}
	for name, tc := range map[string]struct{ steps, kind string }{
		"expect_json":         {`[{ http: { method: GET, path: /, expect_json: { data: { id: "{{issued.token_id}}" } } } }]`, ActionExpect},
		"where":               {`[{ http: { method: GET, path: /, save: { name: { list: data.items, where: { id: "{{issued.token_id}}" }, field: name } } } }]`, ActionSaveFailed},
		"literal expect_json": {`[{ http: { method: GET, path: /, expect_json: { data: { id: 9007199254740993 } } } }]`, ActionExpect},
		"literal where":       {`[{ http: { method: GET, path: /, save: { name: { list: data.items, where: { id: 9007199254740993 }, field: name } } } }]`, ActionSaveFailed},
	} {
		saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, tc.steps), env)
		if err == nil {
			t.Errorf("neighbour, %s: matched (saved %v)", name, saved)
		} else if ae := asActionError(t, err); ae.Kind != tc.kind {
			t.Errorf("neighbour, %s: %v", name, err)
		}
	}
}

// What equals what when a wanted value meets a number of an answer.
func TestAnswerEqual(t *testing.T) {
	num := func(text string) (float64, json.Number) {
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			t.Fatal(err)
		}
		return f, json.Number(text)
	}
	for _, tc := range []struct {
		want   any
		answer string // a JSON number
		equal  bool
	}{
		{"1000000", "1000000", true},
		{"1000000", "1e6", true},
		{"1000000", "1000000.0", true},
		{"1000000", "1000001", false},
		{"1e+06", "1000000", true}, // what a float64 prints as
		{"1.50", "1.5", true},
		{"0", "-0", true},
		{"-5", "5", false},
		{"0.001", "1e-3", true},
		{"9007199254740993", "9007199254740993", true},
		{"9007199254740993", "9007199254740992", false},
		{"123456789012345678901234567890", "123456789012345678901234567890", true},
		{"123456789012345678901234567890", "123456789012345678901234567891", false},
		{"007", "7", false}, // not how a number is written: text
		{" 7", "7", false},
		{"", "0", false},
		{"seven", "7", false},
		{"1e9999999999", "1", false}, // no arithmetic on what cannot be a real exponent
		{7, "7", true},
		{7, "7.0", true},
		{7, "8", false},
		{int64(1000000), "1000000", true},
		{uint64(18446744073709551615), "18446744073709551615", true},
		{uint64(18446744073709551615), "18446744073709551614", false},
		{9007199254740993, "9007199254740992", false},
		{1.5, "1.50", true},
		{0.1, "0.1", true},
		{0.1, "0.1000000000000000055", true}, // the same float64, as before
		{1e6, "1000000", true},
		{true, "1", false},
	} {
		f, n := num(tc.answer)
		if got := answerEqual(tc.want, f, n); got != tc.equal {
			t.Errorf("%#v and %s: equal %v, want %v", tc.want, tc.answer, got, tc.equal)
		}
	}
	// Anything but a number of the answer compares as it always did.
	for _, tc := range []struct {
		want, got any
		equal     bool
	}{
		{"true", true, true}, {true, true, true}, {"x", "x", true}, {"x", "y", false}, {"1", "1", true}, {1, "1", true}, {nil, nil, true}, {"", nil, false},
	} {
		if got := answerEqual(tc.want, tc.got, tc.got); got != tc.equal || got != scalarEqual(tc.want, tc.got) {
			t.Errorf("%#v and %#v: equal %v, want %v", tc.want, tc.got, got, tc.equal)
		}
	}
	// matchAnswer without the exact answer falls back to matchJSON.
	want := map[string]any{"a": map[string]any{"b": 1}, "c": "x"}
	body := map[string]any{"a": map[string]any{"b": float64(1), "z": 2.0}, "c": "x", "d": true}
	if !matchAnswer(want, body, nil) || !matchJSON(want, body) || matchAnswer(want, map[string]any{"a": 1.0, "c": "x"}, nil) || matchAnswer(want, "x", "x") {
		t.Fatal("matchAnswer without the exact answer")
	}
}

func TestActionExpectStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if code >= 300 {
			reply(w, code, map[string]any{"message": "no such thing", "code": code})
			return
		}
		w.WriteHeader(code)
	}))
	defer srv.Close()
	run := func(step string) error {
		_, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, step), ActionEnv{})
		return err
	}
	for _, step := range []string{
		`[{ http: { method: GET, path: /200 } }]`,
		`[{ http: { method: GET, path: /204 } }]`, // without expect_status any 2xx passes
		`[{ http: { method: DELETE, path: /404, expect_status: [200, 404] } }]`,
		`[{ http: { method: GET, path: /404, expect_status: [404], expect_json: { code: 404 } } }]`,
	} {
		if err := run(step); err != nil {
			t.Errorf("%s: %v", step, err)
		}
	}
	err := run(`[{ http: { method: GET, path: /200 } }, { name: gone, http: { method: GET, path: /404 } }]`)
	ae := asActionError(t, err)
	if ae.Kind != ActionStatus || ae.Step != 2 || ae.Name != "gone" || ae.Status != 404 || ae.AppMessage != "no such thing" || !ae.Answered() {
		t.Fatalf("%+v", ae)
	}
	if msg := err.Error(); msg != "第 2 步（gone）：应用返回 HTTP 404，应用说：no such thing" {
		t.Fatalf("message %q", msg)
	}
	// A status that is 2xx but not listed does not pass; a step without a
	// name is told by its number.
	err = run(`[{ http: { method: GET, path: /204, expect_status: [200] } }]`)
	if ae := asActionError(t, err); ae.Kind != ActionStatus || ae.Status != 204 || err.Error() != "第 1 步：应用返回 HTTP 204" {
		t.Fatalf("%+v %q", ae, err.Error())
	}
}

func TestActionExpectJSON(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		reply(w, 200, map[string]any{"success": false, "message": "The system is already initialized", "data": map[string]any{"n": 1}})
	}))
	defer srv.Close()
	run := func(step string) error {
		_, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, step), ActionEnv{})
		return err
	}
	if err := run(`[{ http: { method: POST, path: /, expect_json: { success: false, data: { n: 1 } } } }]`); err != nil {
		t.Fatal(err)
	}
	calls.Store(0)
	err := run(`[{ name: setup, http: { method: POST, path: /, expect_json: { success: true } } }, { http: { method: POST, path: /next } }]`)
	ae := asActionError(t, err)
	if ae.Kind != ActionExpect || ae.Step != 1 || ae.Status != 200 || ae.AppMessage != "The system is already initialized" {
		t.Fatalf("%+v", ae)
	}
	if calls.Load() != 1 {
		t.Fatalf("an answered step was sent %d times, or a later step ran", calls.Load())
	}
}

// ---- connecting again ----

// refuseDials is a client whose first n connection attempts fail the way a
// refused connection does.
func refuseDials(n int32) (*http.Client, *atomic.Int32) {
	var dials atomic.Int32
	tr := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if dials.Add(1) <= n {
			return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connect: connection refused")}
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	return &http.Client{Transport: tr}, &dials
}

func TestActionRetriesOnlyWhenNotConnected(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		reply(w, 200, map[string]any{"ok": true})
	}))
	defer srv.Close()
	steps := stepsYAML(t, `[{ name: setup, http: { method: POST, path: /setup, json: { a: 1 }, expect_json: { ok: true } } }]`)
	ctx := context.Background()

	// Refused twice, then up: sent once.
	c, dials := refuseDials(2)
	if _, err := testActions(c).run(ctx, srv.URL, steps, ActionEnv{}); err != nil {
		t.Fatal(err)
	}
	if dials.Load() != 3 || calls.Load() != 1 {
		t.Fatalf("dials %d, requests %d", dials.Load(), calls.Load())
	}

	// Never up: five attempts, then it gives up.
	calls.Store(0)
	c, dials = refuseDials(1000)
	_, err := testActions(c).run(ctx, srv.URL, steps, ActionEnv{})
	ae := asActionError(t, err)
	if ae.Kind != ActionNotSent || ae.Step != 1 || dials.Load() != 5 || calls.Load() != 0 || ae.Answered() {
		t.Fatalf("%+v dials %d", ae, dials.Load())
	}
	if !strings.Contains(err.Error(), "连不上应用") || strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("message %q", err.Error())
	}

	// A really closed port counts as not connected too.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + ln.Addr().String()
	ln.Close()
	r := testActions(http.DefaultClient)
	r.attempts = 2
	_, err = r.run(ctx, closed, steps, ActionEnv{})
	if ae := asActionError(t, err); ae.Kind != ActionNotSent {
		t.Fatalf("closed port: %+v", ae)
	}

	// Canceled while waiting to connect again.
	c, dials = refuseDials(1000)
	r = testActions(c)
	r.retryWait = time.Minute
	cctx, cancel := context.WithCancel(ctx)
	time.AfterFunc(20*time.Millisecond, cancel)
	_, err = r.run(cctx, srv.URL, steps, ActionEnv{})
	if ae := asActionError(t, err); ae.Kind != ActionCanceled || !errors.Is(err, context.Canceled) || dials.Load() != 1 {
		t.Fatalf("canceled: %+v dials %d", ae, dials.Load())
	}
}

// A request that reached the app and got no answer is not sent again.
func TestActionNotResentOnceSent(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close() // took the request, never answers
		}
	}))
	defer srv.Close()
	// The first step is taken and dropped: one request, no more.
	_, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, `[{ name: setup, http: { method: POST, path: /setup, json: { a: 1 } } }, { http: { method: POST, path: /later } }]`), ActionEnv{})
	ae := asActionError(t, err)
	if ae.Kind != ActionNoAnswer || ae.Step != 1 || ae.Name != "setup" || calls.Load() != 1 || ae.Answered() {
		t.Fatalf("%+v, requests %d", ae, calls.Load())
	}
	if msg := err.Error(); !strings.Contains(msg, "不再重发") || strings.Contains(msg, "127.0.0.1") {
		t.Fatalf("message %q", msg)
	}
}

// A redirect to another address is not followed.
func TestActionStaysOnTheAppsEndpoint(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/away", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/x", http.StatusFound) })
	mux.HandleFunc("/near", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/here", http.StatusFound) })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	mux.HandleFunc("/here", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]any{"at": "here"}) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := context.Background()
	saved, err := testActions(srv.Client()).run(ctx, srv.URL+"/", stepsYAML(t, `[{ http: { method: GET, path: /near, save: { at: at } } }]`), ActionEnv{})
	if err != nil || saved["at"].Value != "here" {
		t.Fatalf("a redirect within the app: %v %v", saved, err)
	}
	// The app did answer: its redirect is the answer, judged by its status.
	_, err = testActions(srv.Client()).run(ctx, srv.URL, stepsYAML(t, `[{ name: out, http: { method: GET, path: /away } }]`), ActionEnv{})
	if ae := asActionError(t, err); ae.Kind != ActionStatus || ae.Status != 302 || !ae.Answered() || elsewhere.Load() != 0 ||
		err.Error() != "第 1 步（out）：应用返回 HTTP 302（应用把请求转到了别的地址，没有跟过去）" ||
		initFault(err).Message != "初始化 out 返回 HTTP 302（应用把请求转到了别的地址，没有跟过去）" {
		t.Fatalf("%+v: %v, requests elsewhere %d", ae, err, elsewhere.Load())
	}
	// A manifest that expects the redirect itself gets it, and it is still not followed.
	if _, err = testActions(srv.Client()).run(ctx, srv.URL, stepsYAML(t, `[{ http: { method: GET, path: /away, expect_status: [302] } }]`), ActionEnv{}); err != nil || elsewhere.Load() != 0 {
		t.Fatalf("expecting the redirect: %v, requests elsewhere %d", err, elsewhere.Load())
	}
	// A redirect that never arrives is an answer too, not a request that got none.
	_, err = testActions(srv.Client()).run(ctx, srv.URL, stepsYAML(t, `[{ http: { method: GET, path: /loop } }]`), ActionEnv{})
	if ae := asActionError(t, err); ae.Kind != ActionStatus || ae.Status != 302 || !strings.Contains(ae.Detail, "转了 10 次") {
		t.Fatalf("%+v", ae)
	}
	// A value cannot move the request off the app either.
	env := ActionEnv{Consumer: &ActionConsumer{Name: "@" + strings.TrimPrefix(other.URL, "http://") + "/x"}}
	if _, err = testActions(srv.Client()).run(ctx, srv.URL, stepsYAML(t, `[{ http: { method: GET, path: "/{{consumer.name}}" } }]`), env); err == nil || elsewhere.Load() != 0 {
		t.Fatalf("err %v, requests elsewhere %d", err, elsewhere.Load())
	}
	if _, err := testActions(srv.Client()).run(ctx, "ftp://127.0.0.1", nil, ActionEnv{}); err == nil {
		t.Fatal("a base that is not http was accepted")
	}
}

// ---- secrets ----

// Secret settings and saved values marked secret never show in an error,
// also when the app's answer repeats them.
func TestActionSecretsHiddenInErrors(t *testing.T) {
	const (
		password = "hunter2-PASSWORD"
		rawKey   = "RAWKEY-0123456789"
		oldKey   = "sk-OLDKEY-abcdef"
		leaked   = "LEAKED-before-saved"
		listed   = "LISTED-in-an-item"
	)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		reply(w, 403, map[string]any{"message": "wrong password " + b["password"] + " for " + b["username"]})
	})
	mux.HandleFunc("POST /key", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"data": map[string]any{"key": rawKey}})
	})
	mux.HandleFunc("POST /use", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 500, map[string]any{"message": "cannot use " + rawKey + " (sk-" + rawKey + ") nor " + oldKey + " as \"" + r.Header.Get("X-Key") + "\""})
	})
	mux.HandleFunc("POST /early", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"success": false, "message": "made " + leaked + " but failed", "data": map[string]any{"key": leaked}})
	})
	mux.HandleFunc("POST /early-list", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"success": false, "message": "made " + listed + " but failed",
			"data": map[string]any{"items": []any{map[string]any{"v": "other", "nested": map[string]any{"deep": []any{listed}}}, map[string]any{"v": listed}}}})
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 500, map[string]any{"message": "admin password is " + password})
	})
	mux.HandleFunc("GET /drop/", func(w http.ResponseWriter, r *http.Request) {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	env := adminEnv(password)
	env.Issued = map[string]SavedValue{"api_key": {Value: oldKey, Secret: true}, "token_id": {Value: "77"}}
	secrets := []string{password, rawKey, oldKey, leaked, listed}
	check := func(name, steps string, kind string, wantIn ...string) {
		t.Helper()
		_, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, steps), env)
		ae := asActionError(t, err)
		if ae.Kind != kind {
			t.Fatalf("%s: kind %s: %v", name, ae.Kind, err)
		}
		for _, text := range []string{err.Error(), ae.AppMessage, ae.Detail, initFault(err).Error(), initFault(err).Message} {
			for _, s := range secrets {
				if strings.Contains(text, s) {
					t.Fatalf("%s: secret %q in %q", name, s, text)
				}
			}
		}
		for _, w := range wantIn {
			if !strings.Contains(err.Error(), w) {
				t.Fatalf("%s: %q not in %q", name, w, err.Error())
			}
		}
	}
	// a secret setting the app repeats; the user name is not secret
	check("setting", `[{ name: login, http: { method: POST, path: /login, json: { username: "{{params.admin_username}}", password: "{{params.admin_password}}" } } }]`,
		ActionStatus, "第 1 步（login）：应用返回 HTTP 403，应用说：wrong password *** for admin")
	// a saved secret, with and without its prefix, and an issued one
	check("saved", `
- http: { method: POST, path: /key, save: { api_key: { path: data.key, prefix: "sk-", secret: true } } }
- name: use
  http: { method: POST, path: /use, headers: { X-Key: "{{saved.api_key}}" } }
`, ActionStatus, "cannot use *** (***) nor *** as \"***\"")
	// a secret in the answer of the very step that fails before saving it
	check("early", `[{ http: { method: POST, path: /early, expect_json: { success: true }, save: { api_key: { path: data.key, secret: true } } } }]`,
		ActionExpect, "made *** but failed")
	check("early list", `[{ http: { method: POST, path: /early-list, expect_json: { success: true }, save: { k: { list: data.items, field: v, secret: true } } } }]`,
		ActionExpect, "made *** but failed")
	// a secret setting only a later step sends, repeated by the app before
	check("setting of a later step", `
- name: status
  http: { method: GET, path: /status }
- http: { method: POST, path: /login, json: { password: "{{params.admin_password}}" } }
`, ActionStatus, "第 1 步（status）：应用返回 HTTP 500，应用说：admin password is ***")
	// a secret save that points one level too high: nothing of the object shows
	check("object", `[{ http: { method: POST, path: /early, save: { api_key: { path: data, secret: true } } } }]`,
		ActionSaveFailed, "made *** but failed")
	check("object in a list", `[{ http: { method: POST, path: /early-list, save: { k: { list: data.items, field: nested, secret: true } } } }]`,
		ActionSaveFailed, "made *** but failed")
	// a secret in the address of a request that failed
	check("address", `[{ http: { method: GET, path: "/drop/{{issued.api_key}}?p={{params.admin_password}}" } }]`, ActionNoAnswer)
	// what a transform makes of a secret (here its b64) is hidden as well
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	secrets = append(secrets, b64(password), b64(oldKey))
	check("transformed setting", `[{ name: login, http: { method: POST, path: /login, json: { username: "{{params.admin_username}}", password: "{{params.admin_password | b64}}" } } }]`,
		ActionStatus, "wrong password *** for admin")
	check("transformed address", `[{ http: { method: GET, path: "/drop/{{issued.api_key | b64}}?p={{params.admin_password | b64}}" } }]`, ActionNoAnswer)

	// A secret value is still returned in full to the caller, marked.
	saved, err := testActions(srv.Client()).run(context.Background(), srv.URL,
		stepsYAML(t, `[{ http: { method: POST, path: /key, save: { api_key: { path: data.key, secret: true } } } }]`), env)
	if err != nil || saved["api_key"] != (SavedValue{Value: rawKey, Secret: true}) {
		t.Fatalf("%+v %v", saved, err)
	}
}

func TestSecretSettingRefs(t *testing.T) {
	m := &catalog.Manifest{
		Params: []catalog.Param{
			{Key: "name", Type: "string"}, {Key: "password", Type: "secret"}, {Key: "pin", Type: "string"},
			{Key: "shown", Type: "string"},
		},
		Env:        []catalog.EnvVar{{Key: "PIN", From: "params.pin", Secret: true}},
		FirstLogin: []catalog.LoginField{{Label: "x", From: "params.shown", Secret: true}},
	}
	secret := secretSettingRefs(m)
	cases := map[string]bool{
		"params.name": false, "params.password": true, "params.pin": true, "params.shown": true,
		"params.password.x": false,
		"platform.nope":     false, "saved.x": false, "consumer.name": false,
	}
	// platform variables: as the table marks them
	for _, pv := range catalog.PlatformVars {
		cases["platform."+pv.Name] = pv.Secret
	}
	for ref, want := range cases {
		if secret(ref) != want {
			t.Errorf("%s: secret %v, want %v", ref, !want, want)
		}
	}
}

// ---- refused before anything is sent ----

func TestActionRefusedBeforeSending(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		reply(w, 200, map[string]any{"id": 1})
	}))
	defer srv.Close()
	first := "- name: first\n  http: { method: POST, path: /create, save: { id: id } }\n"
	issued := map[string]SavedValue{"token_id": {Value: "5"}}
	for name, tc := range map[string]struct {
		steps    string
		consumer bool
		kind     string
		step     int
		detail   string
	}{
		"exec without a runner that runs commands": {first + "- name: grant\n  exec: { command: [\"demo-sql\", \"-c\", \"select 1\"], user: demo, timeout_s: 60 }\n",
			false, ActionRunnerOld, 2, ""},
		"http and exec": {"- http: { method: GET, path: / }\n  exec: { command: [\"true\"] }\n",
			false, ActionBadStep, 1, "一个步骤只能写 http 或 exec 中的一种"},
		"neither": {first + "- name: empty\n", false, ActionBadStep, 2, "步骤里没有写 http 或 exec"},
		"saved key nobody saves": {first + "- name: use\n  http: { method: GET, path: \"/token/{{saved.token_id}}\" }\n",
			false, ActionBadStep, 2, "引用了 saved.token_id，但前面的步骤没有存过它"},
		"saved key saved later": {"- http: { method: GET, path: /, headers: { X: \"{{saved.id}}\" } }\n" + first,
			false, ActionBadStep, 1, "引用了 saved.id，但前面的步骤没有存过它"},
		"saved key of the same step": {"- http: { method: GET, path: /, expect_json: { id: \"{{saved.id}}\" }, save: { id: id } }\n",
			false, ActionBadStep, 1, "引用了 saved.id，但前面的步骤没有存过它"},
		"saved key in where": {first + "- http: { method: GET, path: /, save: { x: { list: items, where: { name: \"{{saved.nope}}\" }, field: id } } }\n",
			false, ActionBadStep, 2, "引用了 saved.nope，但前面的步骤没有存过它"},
		"issued key not issued": {first + "- http: { method: DELETE, path: \"/token/{{issued.other}}\" }\n",
			true, ActionBadStep, 2, "引用了 issued.other，但发凭据时没有存过它"},
		"consumer in init": {first + "- http: { method: POST, path: /, json: { name: \"{{consumer.name}}\" } }\n",
			false, ActionBadStep, 2, "引用了 consumer.name，但这里没有正在接入的应用（初始化步骤里不能用 consumer）"},
		"consumer field": {"- http: { method: POST, path: /, json: { name: \"{{consumer.secret}}\" } }\n",
			true, ActionBadStep, 1, "consumer 只有 name 和 id，没有 consumer.secret"},
		"method":        {first + "- http: { method: HEAD, path: / }\n", false, ActionBadStep, 2, `method 只能是 GET、POST、PUT、PATCH、DELETE，写的是 "HEAD"`},
		"path":          {first + "- http: { method: GET, path: \"http://example.com/x\" }\n", false, ActionBadStep, 2, "path 要以 / 开头，只写路径"},
		"status":        {"- http: { method: GET, path: /, expect_status: [200, 20] }\n", false, ActionBadStep, 1, "expect_status 里的 20 不是 HTTP 状态码"},
		"save both":     {"- http: { method: GET, path: /, save: { x: { path: a, list: b, field: c } } }\n", false, ActionBadStep, 1, "save.x：path 和 list 只能写一种"},
		"save neither":  {"- http: { method: GET, path: /, save: { x: { secret: true } } }\n", false, ActionBadStep, 1, "save.x：要写 path，或者 list 加 field"},
		"save no field": {"- http: { method: GET, path: /, save: { x: { list: items } } }\n", false, ActionBadStep, 1, "save.x：从列表里挑一项时要写 field"},
		"save pick":     {"- http: { method: GET, path: /, save: { x: { list: items, field: id, pick: newest } } }\n", false, ActionBadStep, 1, "save.x：pick 只能是 first 或 last"},
		"save mixed":    {"- http: { method: GET, path: /, save: { x: { path: a, sort_by: id } } }\n", false, ActionBadStep, 1, "save.x：where、sort_by、pick、field 只和 list 一起用"},
		"header name":   {first + "- http: { method: POST, path: /, headers: { \"X Bad\": v } }\n", false, ActionBadStep, 2, `headers 里的 "X Bad" 不能当请求头的名字`},
		"header empty":  {"- http: { method: POST, path: /, headers: { \"\": v } }\n", false, ActionBadStep, 1, `headers 里的 "" 不能当请求头的名字`},
	} {
		env := ActionEnv{Issued: issued}
		if tc.consumer {
			env.Consumer = &ActionConsumer{Name: "web", ID: "app_1"}
		}
		saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, tc.steps), env)
		ae := asActionError(t, err)
		if ae.Kind != tc.kind || ae.Step != tc.step || ae.Detail != tc.detail || ae.Answered() {
			t.Errorf("%s: %+v", name, ae)
		}
		if calls.Load() != 0 || len(saved) != 0 {
			t.Errorf("%s: %d requests were sent before the refusal (saved %v)", name, calls.Load(), saved)
		}
	}
	// The refusal of exec names the step.
	_, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, first+"- name: grant\n  exec: { command: [\"true\"] }\n"), ActionEnv{})
	if msg := err.Error(); msg != "第 2 步（grant）：运行器（runner）太旧，不能在容器里执行命令，命令没有执行：升级 AcornFox 后重试" {
		t.Fatalf("message %q", msg)
	}
}

// A params or platform reference that cannot be resolved stops at its step:
// the steps before it were sent, it was not.
func TestActionUnknownSetting(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		reply(w, 200, map[string]any{})
	}))
	defer srv.Close()
	steps := stepsYAML(t, `
- http: { method: POST, path: /one }
- name: two
  http: { method: POST, path: /two, json: { a: "{{params.nope}}" } }
`)
	_, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, ActionEnv{Lookup: Values{}.lookup})
	ae := asActionError(t, err)
	mu.Lock()
	sent := append([]string(nil), paths...)
	mu.Unlock()
	if ae.Kind != ActionTemplate || ae.Step != 2 || ae.Detail != "没有参数 nope" || !reflect.DeepEqual(sent, []string{"/one"}) {
		t.Fatalf("%+v, sent %v", ae, sent)
	}
	// Without a Lookup nothing but the step values can be referenced.
	_, err = testActions(srv.Client()).run(context.Background(), srv.URL, steps[1:], ActionEnv{})
	if ae := asActionError(t, err); ae.Kind != ActionTemplate || ae.Detail != "不支持的引用 params.nope" {
		t.Fatalf("%+v", ae)
	}
	// A value with a line break cannot go into a header.
	_, err = testActions(srv.Client()).run(context.Background(), srv.URL,
		stepsYAML(t, `[{ http: { method: GET, path: /, headers: { X-A: "{{params.a}}" } } }]`), ActionEnv{Lookup: Values{"a": "x\r\nY: z"}.lookup})
	if ae := asActionError(t, err); ae.Kind != ActionTemplate || !strings.Contains(ae.Detail, "请求头 X-A") {
		t.Fatalf("%+v", ae)
	}
}

// A value cannot take a segment out of a path: the request would go to
// another path than the one written.
func TestActionPathValueCannotDropASegment(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = append(got, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		reply(w, 200, map[string]any{})
	}))
	defer srv.Close()
	sent := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
	run := func(id, step string) error {
		env := ActionEnv{Issued: map[string]SavedValue{"token_id": {Value: id}}, Lookup: Values{"v": id}.lookup}
		_, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, step), env)
		return err
	}
	refused := func(id, step string) {
		t.Helper()
		ae := asActionError(t, run(id, step))
		if ae.Kind != ActionTemplate || ae.Step != 1 || !strings.Contains(ae.Detail, "这一段填进值以后是空的") || ae.Answered() {
			t.Errorf("id %q, %s: %+v", id, step, ae)
		}
	}
	for _, id := range []string{"", ".", ".."} {
		refused(id, `[{ name: revoke, http: { method: DELETE, path: "/api/token/{{issued.token_id}}" } }]`)
		refused(id, `[{ name: revoke, http: { method: DELETE, path: "/api/token/{{issued.token_id}}/x?n=1" } }]`)
		refused(id, `[{ name: revoke, http: { method: DELETE, path: "/{{params.v}}/x" } }]`)
	}
	// two values that make ".." between them
	refused(".", `[{ name: revoke, http: { method: DELETE, path: "/api/{{params.v}}{{issued.token_id}}/x" } }]`)
	if s := sent(); len(s) != 0 {
		t.Fatalf("sent %v", s)
	}
	// Part of a segment, a query value, and what the manifest itself wrote
	// are not the value's doing.
	for _, tc := range []struct{ id, step, sent string }{
		{"", `[{ http: { method: GET, path: "/api/v{{issued.token_id}}/x" } }]`, "GET /api/v/x"},
		{"..", `[{ http: { method: GET, path: "/api/v{{issued.token_id}}/x" } }]`, "GET /api/v../x"},
		{"", `[{ http: { method: GET, path: "/api/x?n={{issued.token_id}}" } }]`, "GET /api/x?n="},
		{"..", `[{ http: { method: GET, path: "/api/x?n={{issued.token_id}}" } }]`, "GET /api/x?n=.."},
		{"7", `[{ http: { method: GET, path: "/api//x/{{issued.token_id}}/" } }]`, "GET /api//x/7/"},
		{"a/../b", `[{ http: { method: GET, path: "/api/{{issued.token_id}}" } }]`, "GET /api/a%2F..%2Fb"},
	} {
		err := run(tc.id, tc.step)
		if s := sent(); err != nil || len(s) != 1 || s[0] != tc.sent {
			t.Errorf("id %q, %s: sent %v, err %v, want %q", tc.id, tc.step, s, err, tc.sent)
		}
	}
}

// ---- the format ----

// The steps read from YAML and from JSON the way the format writes them.
func TestActionStepFormat(t *testing.T) {
	steps := stepsYAML(t, `
- name: login
  http:
    method: POST
    path: /api/user/login
    headers: { X-Foo: "{{saved.token}}" }
    json: { username: "{{params.admin_username}}" }
    expect_status: [200]
    expect_json: { success: true }
    save:
      access_token: data.access_token
      user_id: { path: data.user.id }
      api_key: { path: data.key, prefix: "sk-", secret: true }
      token_id:
        list: data.items
        where: { name: "acornfox-{{consumer.name}}" }
        sort_by: id
        pick: last
        field: id
- exec:
    command: ["psql", "-c", "select 1"]
    user: postgres
    timeout_s: 60
`)
	want := []catalog.Step{
		{Name: "login", HTTP: catalog.StepHTTP{
			Method: "POST", Path: "/api/user/login",
			Headers:      map[string]string{"X-Foo": "{{saved.token}}"},
			JSON:         map[string]any{"username": "{{params.admin_username}}"},
			ExpectStatus: []int{200},
			ExpectJSON:   map[string]any{"success": true},
			Save: map[string]catalog.SaveValue{
				"access_token": {Path: "data.access_token"},
				"user_id":      {Path: "data.user.id"},
				"api_key":      {Path: "data.key", Prefix: "sk-", Secret: true},
				"token_id": {List: "data.items", Where: map[string]any{"name": "acornfox-{{consumer.name}}"},
					SortBy: "id", Pick: "last", Field: "id"},
			},
		}},
		{Exec: &catalog.StepExec{Command: []string{"psql", "-c", "select 1"}, User: "postgres", TimeoutS: 60}},
	}
	if !reflect.DeepEqual(steps, want) {
		t.Fatalf("from YAML\n got %+v\nwant %+v", steps, want)
	}
	// JSON and back: the same steps (the installed manifest is kept as JSON
	// in places).
	b, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	var back []catalog.Step
	if err := json.Unmarshal(b, &back); err != nil || !reflect.DeepEqual(back, want) {
		t.Fatalf("through JSON: %v\n got %+v\n via %s", err, back, b)
	}
	var short map[string]catalog.SaveValue
	if err := json.Unmarshal([]byte(`{"a": "data.id", "b": {"path": "data.key", "secret": true}}`), &short); err != nil ||
		!reflect.DeepEqual(short, map[string]catalog.SaveValue{"a": {Path: "data.id"}, "b": {Path: "data.key", Secret: true}}) {
		t.Fatalf("save from JSON: %+v %v", short, err)
	}
}

// A save written as an object takes its own fields only: a misspelt "secret"
// must not pass for a value that is not one. Nor is a number a path.
func TestActionSaveFormatIsStrict(t *testing.T) {
	for text, want := range map[string]string{
		`[{ http: { method: GET, path: /, save: { x: { path: a, secert: true } } } }]`: "save 里没有 secert 这个字段",
		`[{ http: { method: GET, path: /, save: { x: { path: a, bogus: 1 } } } }]`:     "save 里没有 bogus 这个字段",
		`[{ http: { method: GET, path: /, save: { x: 12 } } }]`:                        "save 的值要写成点路径（文字）或对象，写的是 12",
		`[{ http: { method: GET, path: /, save: { x: true } } }]`:                      "save 的值要写成点路径（文字）或对象，写的是 true",
		`[{ http: { method: GET, path: /, save: { x: [a, b] } } }]`:                    "save 的值要写成点路径（文字）或对象",
	} {
		var steps []catalog.Step
		if err := yaml.Unmarshal([]byte(text), &steps); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n got %v, want %q", text, err, want)
		}
	}
	// a quoted number is a path (an index into a list)
	if steps := stepsYAML(t, `[{ http: { method: GET, path: /, save: { x: "0" } } }]`); steps[0].HTTP.Save["x"].Path != "0" {
		t.Fatalf("%+v", steps[0].HTTP.Save)
	}
	for _, text := range []string{`{"x": {"path": "a", "secert": true}}`, `{"x": 12}`, `{"x": ["a"]}`} {
		var save map[string]catalog.SaveValue
		if err := json.Unmarshal([]byte(text), &save); err == nil {
			t.Errorf("%s was read as %+v", text, save)
		}
	}
}

// ---- init steps through the executor ----

func TestInitActionSteps(t *testing.T) {
	m := &catalog.Manifest{Init: []catalog.InitStep{
		{Name: "setup", When: "after_healthy", HTTP: catalog.InitHTTP{Method: "PUT", Path: "/api/setup",
			JSON: map[string]any{"a": "{{params.x}}"}, ExpectJSON: map[string]any{"success": true}}},
		{Name: "later", When: "after_first_login", HTTP: catalog.InitHTTP{Method: "POST", Path: "/never"}},
		{Name: "no-method", HTTP: catalog.InitHTTP{Path: "/default"}},
	}}
	got := initActionSteps(m)
	want := []catalog.Step{
		{Name: "setup", When: "after_healthy", HTTP: catalog.StepHTTP{Method: "PUT", Path: "/api/setup",
			JSON: map[string]any{"a": "{{params.x}}"}, ExpectJSON: map[string]any{"success": true}}},
		{Name: "no-method", HTTP: catalog.StepHTTP{Method: "POST", Path: "/default"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

// The faults of init steps read as they did before the executor: the page,
// the CLI and the reuse-data path (answered) go by them.
func TestInitFaultTexts(t *testing.T) {
	cause := errors.New("没有参数 x")
	for _, tc := range []struct {
		err      *ActionError
		code     string
		message  string
		answered bool
		wraps    error
	}{
		{&ActionError{Step: 1, Name: "setup", Kind: ActionTemplate, Detail: "没有参数 x", err: cause}, CodeInit, "初始化 setup 的模板：没有参数 x", false, cause},
		{&ActionError{Step: 1, Name: "setup", Kind: ActionCanceled, err: context.Canceled}, CodeInit, "初始化被取消", false, context.Canceled},
		{&ActionError{Step: 1, Name: "setup", Kind: ActionNoAnswer, Detail: "Post 请求失败：EOF"}, CodeInit, "初始化 setup 没有收到应答（Post 请求失败：EOF），不再重发：它可能已经生效", false, nil},
		{&ActionError{Step: 1, Name: "setup", Kind: ActionNotSent, Detail: "Post 请求失败：dial tcp: refused"}, CodeInit, "初始化 setup 失败：Post 请求失败：dial tcp: refused", false, nil},
		{&ActionError{Step: 1, Name: "setup", Kind: ActionStatus, Status: 502, AppMessage: "ignored"}, CodeInit, "初始化 setup 返回 HTTP 502", true, nil},
		{&ActionError{Step: 1, Name: "setup", Kind: ActionExpect, Status: 200, AppMessage: "The system is already initialized"}, CodeInit, "初始化 setup 的应答和预期不符：The system is already initialized", true, nil},
		{&ActionError{Step: 2, Name: "grant", Kind: ActionUnsupported, Detail: "这一版还不支持在容器里执行命令的步骤（exec）"}, CodeUnsupported, "初始化 grant：这一版还不支持在容器里执行命令的步骤（exec）", false, nil},
		{&ActionError{Step: 2, Name: "find", Kind: ActionSaveFailed, Status: 200, Detail: "存 id 时，应答里没有 data.id"}, CodeInit, "初始化 find：存 id 时，应答里没有 data.id", false, nil},
		// Command steps.
		{&ActionError{Step: 1, Name: "quiet", Kind: ActionExit, Exec: true, ExitCode: 3, Detail: "算通过的是 0", Output: "ERROR: role exists"}, CodeInit, "初始化 quiet：命令的退出码是 3（算通过的是 0），标准错误的最后：ERROR: role exists", true, nil},
		{&ActionError{Step: 1, Name: "quiet", Kind: ActionTimeout, Exec: true, ExitCode: -1, Detail: "30 秒"}, CodeInit, "初始化 quiet：命令在 30 秒内没有结束，平台不再等它，也没有重新执行：它可能还在应用的容器里运行（平台停不掉在容器里执行的命令），可能已经部分生效", false, nil},
		{&ActionError{Step: 1, Name: "quiet", Kind: ActionNotRunning, Exec: true, Detail: "这个应用现在没有部署好的容器"}, CodeInit, "初始化 quiet：应用的容器没在运行，命令没有执行（这个应用现在没有部署好的容器）", false, nil},
		{&ActionError{Step: 1, Name: "quiet", Kind: ActionRunnerOld, Exec: true}, CodeInternal, "初始化 quiet：运行器（runner）太旧，不能在容器里执行命令，命令没有执行：升级 AcornFox 后重试", false, nil},
	} {
		f := initFault(tc.err)
		if f.Code != tc.code || f.Message != tc.message || f.answered != tc.answered || !errors.Is(f, tc.wraps) && tc.wraps != nil {
			t.Errorf("%s: %s %q answered=%v", tc.err.Kind, f.Code, f.Message, f.answered)
		}
		if tc.wraps == nil && f.Unwrap() != nil {
			t.Errorf("%s: wraps %v", tc.err.Kind, f.Unwrap())
		}
	}
	if f := initFault(errors.New("boom")); f.Code != CodeInit || !strings.Contains(f.Message, "boom") {
		t.Fatalf("%+v", f)
	}
}

// The install's init steps go through the executor: the published setup
// call is sent once, with the cookie jar and the redirect rule of a group.
func TestRunInitUsesExecutor(t *testing.T) {
	v := setup(t)
	if tv := v.install(t, Request{App: "new-api"}); tv.Fault != nil {
		t.Fatalf("install: %+v", tv.Fault)
	}
	if v.tokens.setups != 1 || v.tokens.setupBody["username"] != "admin" || v.tokens.setupBody["SelfUseModeEnabled"] != true {
		t.Fatalf("setup sent %d times: %+v", v.tokens.setups, v.tokens.setupBody)
	}
	if v.e.cfg.HTTP.Jar != nil || v.e.cfg.HTTP.CheckRedirect != nil {
		t.Fatal("the engine's client was changed by a group of steps")
	}
}

// ---- finish_if, 429 ----

// A step whose answer matches its finish_if ends the group as a success:
// the steps after it are not sent and nothing is saved from it. With status
// and json both written, both must match; an answer that does not match is
// judged as usual.
func TestActionFinishIf(t *testing.T) {
	var paths []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/gone":
			reply(w, 404, map[string]any{"success": false, "message": "record not found"})
		case "/other":
			reply(w, 200, map[string]any{"success": false, "message": "record not found"})
		default:
			reply(w, 200, map[string]any{"success": true, "data": map[string]any{"id": 7}})
		}
	}))
	defer srv.Close()
	run := func(steps string, issued map[string]SavedValue) (map[string]SavedValue, error, []string) {
		mu.Lock()
		paths = nil
		mu.Unlock()
		saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, steps), ActionEnv{Issued: issued})
		mu.Lock()
		defer mu.Unlock()
		return saved, err, append([]string(nil), paths...)
	}
	issued := map[string]SavedValue{"token_id": {Value: "7"}}
	// Gone (404): over, the delete is not sent.
	_, err, sent := run(`
- { name: same, http: { method: GET, path: /gone, finish_if: { status: [404] }, expect_json: { success: true } } }
- { name: delete, http: { method: DELETE, path: "/items/{{issued.token_id}}" } }
`, issued)
	if err != nil || len(sent) != 1 || sent[0] != "GET /gone" {
		t.Fatalf("gone: %v %v", err, sent)
	}
	// json written too, with a template: both must match.
	_, err, sent = run(`
- { http: { method: GET, path: /gone, finish_if: { status: [404], json: { message: "record not found" } } } }
- { http: { method: DELETE, path: /items/7 } }
`, issued)
	if err != nil || len(sent) != 1 {
		t.Fatalf("status and json: %v %v", err, sent)
	}
	// Only json: any status; a 200 that says so ends it as well.
	_, err, sent = run(`
- { http: { method: GET, path: /other, finish_if: { json: { success: false } }, expect_json: { success: true } } }
- { http: { method: DELETE, path: /items/7 } }
`, issued)
	if err != nil || len(sent) != 1 {
		t.Fatalf("json only: %v %v", err, sent)
	}
	// Not matched (the status is not the one written): judged as usual.
	_, err, sent = run(`
- { name: same, http: { method: GET, path: /other, finish_if: { status: [404] }, expect_json: { success: true } } }
- { http: { method: DELETE, path: /items/7 } }
`, issued)
	if ae := asActionError(t, err); ae.Kind != ActionExpect || ae.Name != "same" || len(sent) != 1 {
		t.Fatalf("not matched: %+v %v", ae, sent)
	}
	// Matched answers save nothing; the steps before keep what they saved.
	saved, err, sent := run(`
- { http: { method: GET, path: /items, save: { id: data.id } } }
- { http: { method: GET, path: /gone, finish_if: { status: [404] }, save: { never: data.id } } }
- { http: { method: DELETE, path: "/items/{{saved.id}}" } }
`, nil)
	if err != nil || len(sent) != 2 || saved["id"].Value != "7" || len(saved) != 1 {
		t.Fatalf("saved %+v %v %v", saved, err, sent)
	}
	// An empty finish_if is refused before anything is sent.
	_, err, sent = run(`
- { http: { method: GET, path: /gone, finish_if: {} } }
`, nil)
	if ae := asActionError(t, err); ae.Kind != ActionBadStep || len(sent) != 0 {
		t.Fatalf("empty: %+v %v", ae, sent)
	}
}

// An app that answers 429 limits how often it may be asked: the step fails
// with its own kind and words, and is sent neither again nor on its retry.
// A step that expects 429 passes as written.
func TestAction429IsNotSentAgain(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		reply(w, http.StatusTooManyRequests, map[string]any{"success": false, "message": "too many requests"})
	}))
	defer srv.Close()
	r := testActions(srv.Client())
	r.second = time.Millisecond
	_, err := r.run(context.Background(), srv.URL, stepsYAML(t, `
- { name: list, http: { method: GET, path: /list, retry: { times: 3, delay_s: 1 } } }
`), ActionEnv{})
	ae := asActionError(t, err)
	if ae.Kind != ActionLimited || ae.Status != 429 || ae.Answered() || calls.Load() != 1 ||
		!strings.Contains(ae.Error(), "这个应用限制了请求次数") || !strings.Contains(ae.Error(), "过一会儿再试") {
		t.Fatalf("429: %+v (%v) calls %d", ae, err, calls.Load())
	}
	calls.Store(0)
	if _, err := r.run(context.Background(), srv.URL, stepsYAML(t, `
- { http: { method: POST, path: /x, expect_status: [429] } }
`), ActionEnv{}); err != nil || calls.Load() != 1 {
		t.Fatalf("expected 429: %v calls %d", err, calls.Load())
	}
	// In init, the fault says so too.
	if f := initFault(ae); f.Code != CodeInit || !strings.Contains(f.Message, "限制了请求次数") || f.answered {
		t.Fatalf("init fault %+v", f)
	}
}

// ---- retry ----

// A GET step with retry asks again while the answer does not pass or lacks
// a value to save, at most retry.times more times; a step without retry, or
// whose answer passes at once, is sent once.
func TestActionRetry(t *testing.T) {
	var calls atomic.Int32
	ready := atomic.Int32{} // the request number from which the answer passes
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		switch {
		case n < ready.Load() && r.URL.Path == "/status":
			reply(w, 404, map[string]any{"message": "not yet"})
		case n < ready.Load() && r.URL.Path == "/expect":
			reply(w, 200, map[string]any{"state": "pending"})
		case n < ready.Load():
			reply(w, 200, map[string]any{"data": map[string]any{"items": []any{}}})
		default:
			reply(w, 200, map[string]any{"state": "done", "data": map[string]any{"items": []any{map[string]any{"name": "t", "id": 7}}}})
		}
	}))
	defer srv.Close()
	run := func(step string, from int32) (map[string]SavedValue, error) {
		calls.Store(0)
		ready.Store(from)
		return testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, step), ActionEnv{})
	}
	for _, step := range []string{
		`[{ http: { method: GET, path: /status, retry: { times: 3, delay_s: 1 } } }]`,
		`[{ http: { method: GET, path: /expect, expect_json: { state: done }, retry: { times: 3, delay_s: 1 } } }]`,
		`[{ http: { method: GET, path: /list, save: { id: { list: data.items, where: { name: t }, field: id } }, retry: { times: 3, delay_s: 0 } } }]`,
	} {
		// Passes on the third request: sent three times.
		saved, err := run(step, 3)
		if err != nil || calls.Load() != 3 {
			t.Errorf("%s: %v after %d requests", step, err, calls.Load())
		}
		if strings.Contains(step, "/list") && saved["id"].Value != "7" {
			t.Errorf("%s: saved %v", step, saved)
		}
		// Never passes: 1 + times requests, then the last answer's failure.
		_, err = run(step, 1000)
		ae := asActionError(t, err)
		if calls.Load() != 4 || !answerMayChange(ae.Kind) || ae.Step != 1 {
			t.Errorf("%s: %+v after %d requests", step, ae, calls.Load())
		}
	}
	// Without retry an answer that does not pass is final.
	if _, err := run(`[{ http: { method: GET, path: /status } }]`, 3); err == nil || calls.Load() != 1 {
		t.Fatalf("without retry: %v after %d requests", err, calls.Load())
	}
	// A step after a retried one runs once it passed.
	if _, err := run(`[{ http: { method: GET, path: /status, retry: { times: 2, delay_s: 0 } } }, { http: { method: GET, path: /expect, expect_json: { state: done } } }]`, 2); err != nil || calls.Load() != 3 {
		t.Fatalf("then the next step: %v after %d requests", err, calls.Load())
	}
}

// The wait between two tries can be canceled; a retry does not repeat a
// request that got no answer (it may have taken effect).
func TestActionRetryCanceledAndNoAnswer(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/hang" {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("no hijacker")
				return
			}
			conn, _, _ := hj.Hijack()
			conn.Close() // sent, no answer
			return
		}
		reply(w, 404, map[string]any{})
	}))
	defer srv.Close()
	r := testActions(srv.Client())
	r.second = time.Minute
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	_, err := r.run(ctx, srv.URL, stepsYAML(t, `[{ http: { method: GET, path: /missing, retry: { times: 5, delay_s: 1 } } }]`), ActionEnv{})
	if ae := asActionError(t, err); ae.Kind != ActionCanceled || !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("canceled: %+v after %d requests", ae, calls.Load())
	}
	// A fresh connection for every request: the transport itself may send a
	// GET again on a kept-alive connection that the server dropped.
	calls.Store(0)
	fresh := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	_, err = testActions(fresh).run(context.Background(), srv.URL, stepsYAML(t, `[{ http: { method: GET, path: /hang, retry: { times: 5, delay_s: 0 } } }]`), ActionEnv{})
	if ae := asActionError(t, err); ae.Kind != ActionNoAnswer || calls.Load() != 1 {
		t.Fatalf("no answer: %+v after %d requests", ae, calls.Load())
	}
}

// retry is for GET only, within its bounds; otherwise nothing is sent.
func TestActionRetryRefusedBeforeSending(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		reply(w, 200, map[string]any{})
	}))
	defer srv.Close()
	for step, want := range map[string]string{
		`[{ http: { method: POST, path: /, retry: { times: 2, delay_s: 1 } } }]`:   "retry 只能写在 GET 步骤上，这一步是 POST：再发一遍可能把东西再建一次或再删一次",
		`[{ http: { method: DELETE, path: /, retry: { times: 2, delay_s: 1 } } }]`: "retry 只能写在 GET 步骤上，这一步是 DELETE：再发一遍可能把东西再建一次或再删一次",
		`[{ http: { method: GET, path: /, retry: { times: 0, delay_s: 1 } } }]`:    "retry.times 要在 1 到 10 之间，写的是 0",
		`[{ http: { method: GET, path: /, retry: { times: 11, delay_s: 1 } } }]`:   "retry.times 要在 1 到 10 之间，写的是 11",
		`[{ http: { method: GET, path: /, retry: { times: 1, delay_s: 31 } } }]`:   "retry.delay_s 要在 0 到 30 之间，写的是 31",
		`[{ http: { method: GET, path: /, retry: { times: 1, delay_s: -1 } } }]`:   "retry.delay_s 要在 0 到 30 之间，写的是 -1",
	} {
		_, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, step), ActionEnv{})
		if ae := asActionError(t, err); ae.Kind != ActionBadStep || ae.Detail != want {
			t.Errorf("%s: %+v", step, ae)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("%d requests were sent", calls.Load())
	}
}

// ---- the issue number ----

func TestNewIssueID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		id := newIssueID()
		if len(id) != 8 || strings.Trim(id, "0123456789abcdef") != "" {
			t.Fatalf("issue number %q is not 8 hex digits", id)
		}
		seen[id] = true
	}
	if len(seen) < 19 {
		t.Fatalf("20 draws gave %d different numbers", len(seen))
	}
}

// The issue steps read the number as {{issue.id}}; it is stored with the
// credential, and revoke reads it back as {{issued.issue_id}}.
func TestActionIssueID(t *testing.T) {
	var mu sync.Mutex
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got = append(got, r.Method+" "+r.URL.RequestURI()+" "+fmt.Sprint(body["name"]))
		mu.Unlock()
		reply(w, 200, map[string]any{"data": map[string]any{"key": "k-123"}})
	}))
	defer srv.Close()
	sent := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
	consumer := &ActionConsumer{Name: "web", ID: "app_1"}
	issue := stepsYAML(t, `[{ http: { method: POST, path: /tokens, json: { name: "acornfox-{{consumer.name}}-{{issue.id}}" }, save: { key: { path: data.key, secret: true } } } }]`)
	saved, err := testActions(srv.Client()).run(context.Background(), srv.URL, issue, ActionEnv{Consumer: consumer, IssueID: "0a1b2c3d"})
	if err != nil {
		t.Fatal(err)
	}
	if s := sent(); len(s) != 1 || s[0] != "POST /tokens acornfox-web-0a1b2c3d" {
		t.Fatalf("sent %v", s)
	}
	cred := credential(saved, "0a1b2c3d")
	if !reflect.DeepEqual(cred, map[string]SavedValue{"key": {Value: "k-123", Secret: true}, catalog.IssueIDKey: {Value: "0a1b2c3d"}}) {
		t.Fatalf("credential %v", cred)
	}
	// The platform's number wins over a value a step saved under its key.
	if c := credential(map[string]SavedValue{catalog.IssueIDKey: {Value: "x"}}, "0a1b2c3d"); c[catalog.IssueIDKey].Value != "0a1b2c3d" {
		t.Fatalf("credential %v", c)
	}
	revoke := stepsYAML(t, `[{ http: { method: DELETE, path: "/tokens/acornfox-{{consumer.name}}-{{issued.issue_id}}" } }]`)
	if _, err := testActions(srv.Client()).run(context.Background(), srv.URL, revoke, ActionEnv{Consumer: consumer, Issued: cred}); err != nil {
		t.Fatal(err)
	}
	if s := sent(); len(s) != 1 || s[0] != "DELETE /tokens/acornfox-web-0a1b2c3d <nil>" {
		t.Fatalf("sent %v", s)
	}
	// Outside issue there is no issue.id, and issue has nothing else: refused
	// before anything is sent.
	for step, tc := range map[string]struct {
		env    ActionEnv
		detail string
	}{
		`[{ http: { method: DELETE, path: "/tokens/{{issue.id}}" } }]`: {ActionEnv{Consumer: consumer, Issued: cred},
			"引用了 issue.id，但这里不是发凭据的步骤（只有 issue 的步骤里有；revoke、check 里用 issued.issue_id）"},
		`[{ http: { method: POST, path: /, json: { n: "{{issue.id}}" } } }]`:     {ActionEnv{}, "引用了 issue.id，但这里不是发凭据的步骤（只有 issue 的步骤里有；revoke、check 里用 issued.issue_id）"},
		`[{ http: { method: POST, path: /, json: { n: "{{issue.number}}" } } }]`: {ActionEnv{Consumer: consumer, IssueID: "0a1b2c3d"}, "issue 只有 id，没有 issue.number"},
	} {
		_, err := testActions(srv.Client()).run(context.Background(), srv.URL, stepsYAML(t, step), tc.env)
		if ae := asActionError(t, err); ae.Kind != ActionBadStep || ae.Detail != tc.detail {
			t.Errorf("%s: %+v", step, ae)
		}
	}
	if s := sent(); len(s) != 0 {
		t.Fatalf("sent %v", s)
	}
}

// An exec init step stays an exec step, never sent as an empty request (with
// a runner that cannot run commands it is refused as one: CAT-INST-20). An
// init step is run as the manifest writes it (headers, save, expect_status,
// retry), only a missing method is POST.
func TestInitStepsAsWritten(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/finish" && r.Header.Get("Authorization") != "Bearer t-1" {
			reply(w, 401, map[string]any{})
			return
		}
		reply(w, 201, map[string]any{"data": map[string]any{"token": "t-1"}})
	}))
	defer srv.Close()
	m := &catalog.Manifest{Init: []catalog.Step{
		{When: "after_healthy", HTTP: catalog.StepHTTP{Path: "/setup", ExpectStatus: []int{201}, Save: map[string]catalog.SaveValue{"token": {Path: "data.token"}}}},
		{Name: "finish", When: "after_healthy", HTTP: catalog.StepHTTP{Method: "PATCH", Path: "/finish", Headers: map[string]string{"Authorization": "Bearer {{saved.token}}"}}},
	}}
	steps := initActionSteps(m)
	if steps[0].HTTP.Method != "POST" || steps[1].HTTP.Method != "PATCH" || m.Init[0].HTTP.Method != "" {
		t.Fatalf("%+v (manifest %+v)", steps, m.Init)
	}
	if _, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, ActionEnv{}); err != nil || calls.Load() != 2 {
		t.Fatalf("%v after %d requests", err, calls.Load())
	}
	calls.Store(0)
	m.Init = append(m.Init, catalog.Step{Name: "grant", When: "after_healthy", Exec: &catalog.StepExec{Command: []string{"true"}}})
	steps = initActionSteps(m)
	if !steps[2].IsExec() || steps[2].HTTP.Written() {
		t.Fatalf("exec step became %+v", steps[2])
	}
	_, err := testActions(srv.Client()).run(context.Background(), srv.URL, steps, ActionEnv{})
	if ae := asActionError(t, err); ae.Kind != ActionRunnerOld || ae.Step != 3 || calls.Load() != 0 {
		t.Fatalf("%+v after %d requests", ae, calls.Load())
	}
	if f := initFault(err); f.Code != CodeInternal {
		t.Fatalf("%+v", f)
	}
}

// A manifest with a writing the format accepts and this version does not
// carry out yet (catalog.PendingFeatures), needs no later platform version:
// a build of the version under development installs manifests of the
// format, and refuses this one before anything is created (CAT-INST-23),
// naming what and where. Command steps, issue_vars and issued.* in a value
// template are carried out: accepted with a runner that runs commands,
// refused before anything is created (CAT-INST-20) with one that cannot.
func TestPendingFeaturesRefusedBeforeInstall(t *testing.T) {
	e := &Engine{cfg: Config{Version: cli.Version, Arch: "amd64"}}
	base := "id: x\nschema: 3\nlifecycle: { min_platform: \"0.6.0\" }\nuses:\n  - { name: mail, kind: smtp, optional: true, without: 没有也能用 }\n"
	withExec := base + "init:\n  - when: after_healthy\n    exec: { command: [\"true\"] }\n"
	for raw, want := range map[string]string{base: "", withExec: ""} {
		mp, err := catalog.ManifestMinPlatform([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if got := e.needsPlatform(mp.Version); got != want {
			t.Errorf("%s needs %s; this build (%s) must upgrade to %q, got %q", raw, mp.Version, cli.Version, want, got)
		}
	}
	manifest := func() *catalog.Manifest {
		return &catalog.Manifest{ID: "x", Lifecycle: catalog.Lifecycle{MinPlatform: "0.6.0"}, Resources: catalog.Resources{Platforms: []string{"linux/amd64"}},
			Release: catalog.Release{Images: []catalog.Image{{Name: "app"}}}, Endpoints: []catalog.Endpoint{{Name: "web", Port: 80, Protocol: "http", Primary: true}}}
	}
	if err := e.checkManifest(&App{Manifest: manifest()}); err != nil {
		t.Fatalf("plain manifest refused: %v", err)
	}
	cases := map[string]struct {
		mod   func(*catalog.Manifest)
		where string
	}{
		"max_consumers": {func(m *catalog.Manifest) {
			m.Provides = []catalog.Provide{{Kind: "smtp", MaxConsumers: 1}}
		}, "/provides/0/max_consumers"},
		"purge": {func(m *catalog.Manifest) {
			m.Provides = []catalog.Provide{{Kind: "smtp", Purge: []catalog.Step{{Exec: &catalog.StepExec{Command: []string{"true"}}}}}}
		}, "/provides/0/purge"},
		"on_check_fail stop": {func(m *catalog.Manifest) {
			m.Provides = []catalog.Provide{{Kind: "smtp", OnCheckFail: catalog.OnCheckFailStop}}
		}, "/provides/0/on_check_fail"},
		"ready": {func(m *catalog.Manifest) {
			m.Provides = []catalog.Provide{{Kind: "smtp", Ready: &catalog.ProvideReady{Say: "还没好"}}}
		}, "/provides/0/ready"},
		"dedicated use": {func(m *catalog.Manifest) {
			m.Uses = []catalog.Use{{Name: "db", Kind: "smtp", Dedicated: true}}
		}, "/uses/db/dedicated"},
	}
	// The B section's second round is carried out now: none of these is
	// refused (purge written as a command needs a runner that runs
	// commands, like any command step).
	runsCmds := &Engine{cfg: Config{Version: cli.Version, Arch: "amd64", Runner: &execWorld{fakeWorld: newWorld(t), execRunner: &execRunner{}}}}
	for name, c := range cases {
		m := manifest()
		c.mod(m)
		if p := catalog.PendingFeatures(m); len(p) != 0 {
			t.Errorf("%s: still pending %+v (%s)", name, p, c.where)
		}
		if err := runsCmds.checkManifest(&App{Manifest: m}); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	carried := map[string]func(*catalog.Manifest){
		"exec init step": func(m *catalog.Manifest) {
			m.Init = []catalog.Step{{Name: "grant", When: "after_healthy", Exec: &catalog.StepExec{Command: []string{"true"}}}}
		},
		"exec revoke step": func(m *catalog.Manifest) {
			m.Provides = []catalog.Provide{{Kind: "smtp", Revoke: []catalog.Step{{Exec: &catalog.StepExec{Command: []string{"true"}}}}}}
		},
		"issue_vars and issued in a value template": func(m *catalog.Manifest) {
			m.Provides = []catalog.Provide{{Kind: "smtp", IssueVars: map[string]catalog.IssueVar{"user": {Template: "u{{issue.id}}"}},
				Values: map[string]catalog.ValueSource{"username": {Template: "{{issued.user}}"}}}}
		},
		// Carried out too (internalnet.go): an exec health check runs a
		// command in the container, so it needs the same runner.
		"exec health check": func(m *catalog.Manifest) {
			m.Endpoints[0].Health = &catalog.Health{Exec: []string{"true"}}
		},
	}
	runsCommands := &Engine{cfg: Config{Version: cli.Version, Arch: "amd64", Runner: &execWorld{fakeWorld: newWorld(t), execRunner: &execRunner{}}}}
	for name, mod := range carried {
		m := manifest()
		mod(m)
		if err := runsCommands.checkManifest(&App{Manifest: m}); err != nil {
			t.Errorf("%s refused with a runner that runs commands: %v", name, err)
		}
		err := e.checkManifest(&App{Manifest: m})
		if name == "issue_vars and issued in a value template" {
			if err != nil {
				t.Errorf("%s: no command to run, refused: %v", name, err)
			}
			continue
		}
		if f := faultOf(t, err); f.Code != CodeInternal || !strings.Contains(f.Message, "运行器（runner）太旧") {
			t.Errorf("%s without a runner that runs commands: %+v", name, f)
		}
	}
	// Carried out now (internalnet.go): a tcp primary endpoint reached over
	// the app network only, checked with a TCP connect, is accepted with any
	// runner (no command is run).
	m := manifest()
	m.Endpoints[0].Protocol, m.Endpoints[0].Health, m.Exposure.Default = "tcp", &catalog.Health{TCP: true}, "internal"
	if err := e.checkManifest(&App{Manifest: m}); err != nil {
		t.Errorf("tcp endpoint, tcp health check, internal: %v", err)
	}
}
