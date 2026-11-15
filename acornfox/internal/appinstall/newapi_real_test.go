package appinstall

// Against a real New API: the steps of the New API manifest written the new
// way (catalog/testdata/good/apps/new-api) run by the executor, without any
// code of the platform's own for that app.
//
// Skipped unless AFX_REAL_NEWAPI is the base address of a New API container
// that has never been set up (it runs the setup), for example
//
//	docker run -d -p 3000:3000 -e SESSION_SECRET=… -e SQLITE_PATH=/data/one-api.db \
//	  -v /some/empty/dir:/data calciumion/new-api:v1.0.0-rc.48
//	AFX_REAL_NEWAPI=http://<host>:3000 go test ./internal/appinstall -run TestRealNewAPI -v
//
// TestRealNewAPIRateLimit (AFX_REAL_NEWAPI_LIMIT, another fresh container)
// issues tokens until New API refuses, to see how many issues fit in its
// limit for critical calls. It leaves that container unusable for a while:
// give it one of its own.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
)

// realNewAPI is the manifest's steps and a fresh admin password.
type realNewAPI struct {
	base     string
	m        *catalog.Manifest
	pv       catalog.Provide
	password string
	r        actionRunner
	http     *http.Client
}

func newRealNewAPI(t *testing.T, envVar string) *realNewAPI {
	t.Helper()
	base := strings.TrimRight(os.Getenv(envVar), "/")
	if base == "" {
		t.Skipf("%s is not set: no real New API to test against", envVar)
	}
	raw, err := os.ReadFile(filepath.Join("..", "catalog", "testdata", "good", "apps", "new-api", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := catalog.ParseManifest(raw)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(m.Provides) != 1 || m.Provides[0].Kind != "llm_gateway" || len(m.Provides[0].Issue) == 0 {
		t.Fatalf("provides %+v", m.Provides)
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	c := &http.Client{Timeout: 20 * time.Second}
	return &realNewAPI{
		base: base, m: m, pv: m.Provides[0], password: "pw-" + hex.EncodeToString(b), http: c,
		// The executor as the engine sets it up.
		r: actionRunner{http: c, attempts: 5, retryWait: 2 * time.Second, timeout: 15 * time.Second, second: time.Second},
	}
}

func (n *realNewAPI) env() ActionEnv {
	v := Values{"admin_username": "admin", "admin_password": n.password, "session_secret": "unused-here"}
	return ActionEnv{Lookup: v.lookup, SecretRef: secretSettingRefs(n.m)}
}

func (n *realNewAPI) initSteps(ctx context.Context) error {
	_, err := n.r.run(ctx, n.base, initActionSteps(n.m), n.env())
	return err
}

// issue does what the platform does to hand a credential to consumer.
func (n *realNewAPI) issue(ctx context.Context, consumer string) (map[string]SavedValue, error) {
	env := n.env()
	env.Consumer = &ActionConsumer{Name: consumer, ID: "app_" + consumer}
	env.IssueID = newIssueID()
	saved, err := n.r.run(ctx, n.base, n.pv.Issue, env)
	return credential(saved, env.IssueID), err
}

func (n *realNewAPI) withCred(consumer string, cred map[string]SavedValue) ActionEnv {
	env := n.env()
	env.Consumer = &ActionConsumer{Name: consumer, ID: "app_" + consumer}
	env.Issued = cred
	return env
}

func (n *realNewAPI) check(ctx context.Context, consumer string, cred map[string]SavedValue) error {
	_, err := n.r.run(ctx, n.base, n.pv.Check, n.withCred(consumer, cred))
	return err
}

func (n *realNewAPI) revoke(ctx context.Context, consumer string, cred map[string]SavedValue) error {
	_, err := n.r.run(ctx, n.base, n.pv.Revoke, n.withCred(consumer, cred))
	return err
}

// get sends one request of the test's own (not a step) and decodes the JSON.
func (n *realNewAPI) do(t *testing.T, method, path, bearer, body string) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, n.base+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := n.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (n *realNewAPI) registerEnabled(t *testing.T) bool {
	t.Helper()
	code, st := n.do(t, http.MethodGet, "/api/status", "", "")
	data, _ := st["data"].(map[string]any)
	on, ok := data["register_enabled"].(bool)
	if code != 200 || !ok {
		t.Fatalf("/api/status %d %v", code, st)
	}
	return on
}

// modelsWith reports what the model API answers to a key: 200 with an
// object "list" (New API knows the key; with no channel set up the list is
// empty) or 401 (it does not).
func (n *realNewAPI) modelsWith(t *testing.T, key string) (int, string) {
	t.Helper()
	code, out := n.do(t, http.MethodGet, "/v1/models", key, "")
	obj, _ := out["object"].(string)
	return code, obj
}

// noSecretIn fails when an error text holds the admin password or a key.
func noSecretIn(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		return
	}
	for _, s := range secrets {
		if s != "" && strings.Contains(err.Error(), s) {
			t.Fatalf("error text holds a secret: %v", err)
		}
	}
}

// timer logs how long each part takes.
type timer struct {
	t     *testing.T
	start time.Time
	last  time.Time
}

func newTimer(t *testing.T) *timer { now := time.Now(); return &timer{t: t, start: now, last: now} }

func (tm *timer) lap(what string) {
	now := time.Now()
	tm.t.Logf("用时 %-28s %6.2f 秒", what, now.Sub(tm.last).Seconds())
	tm.last = now
}

func TestRealNewAPIStepsFromManifest(t *testing.T) {
	n := newRealNewAPI(t, "AFX_REAL_NEWAPI")
	ctx := context.Background()
	tm := newTimer(t)
	defer func() { t.Logf("用时 合计 %.2f 秒", time.Since(tm.start).Seconds()) }()

	// 1. Init: set up the admin, log in, close self-service sign-up.
	if !n.registerEnabled(t) {
		t.Fatal("sign-up is already off before init: not a fresh New API")
	}
	if err := n.initSteps(ctx); err != nil {
		noSecretIn(t, err, n.password)
		t.Fatalf("init: %v", err)
	}
	tm.lap("初始化（3 步）")
	if n.registerEnabled(t) {
		t.Fatal("after init /api/status still says register_enabled")
	}
	code, reg := n.do(t, http.MethodPost, "/api/user/register", "", `{"username":"stranger1","password":"stranger-pw-123"}`)
	if ok, _ := reg["success"].(bool); code != 200 || ok {
		t.Fatalf("sign-up after init: %d %v", code, reg)
	}
	t.Logf("初始化后注册被拒：HTTP %d，%v", code, reg["message"])
	tm.lap("核对注册已关")

	// 2. Issue to two consumers.
	credA, err := n.issue(ctx, "hermes-agent")
	if err != nil {
		noSecretIn(t, err, n.password)
		t.Fatalf("issue to hermes-agent: %v", err)
	}
	credB, err := n.issue(ctx, "open-webui")
	if err != nil {
		t.Fatalf("issue to open-webui: %v", err)
	}
	tm.lap("发两把令牌")
	for name, c := range map[string]map[string]SavedValue{"hermes-agent": credA, "open-webui": credB} {
		keys := make([]string, 0, len(c))
		for k, v := range c {
			if v.Secret {
				k += "(保密)"
			}
			keys = append(keys, k)
		}
		t.Logf("发给 %s 的凭据存了这些键：%v", name, keys)
		key := c["api_key"].Value
		if !strings.HasPrefix(key, "sk-") || len(key) != 3+48 || !c["api_key"].Secret {
			t.Fatalf("%s: api_key %d characters, secret %v", name, len(key), c["api_key"].Secret)
		}
		if _, err := strconv.Atoi(c["token_id"].Value); err != nil {
			t.Fatalf("%s: token_id %q", name, c["token_id"].Value)
		}
		if len(c[catalog.IssueIDKey].Value) != 8 {
			t.Fatalf("%s: issue id %q", name, c[catalog.IssueIDKey].Value)
		}
	}
	if credA["token_id"].Value == credB["token_id"].Value || credA["api_key"].Value == credB["api_key"].Value {
		t.Fatal("the two consumers got the same token")
	}

	// 3. Check: both still there.
	if err := n.check(ctx, "hermes-agent", credA); err != nil {
		t.Fatalf("check hermes-agent: %v", err)
	}
	if err := n.check(ctx, "open-webui", credB); err != nil {
		t.Fatalf("check open-webui: %v", err)
	}
	tm.lap("核对两把（check）")

	// 4. The keys work on the model API (base_url is <endpoint>/v1).
	for name, c := range map[string]map[string]SavedValue{"hermes-agent": credA, "open-webui": credB} {
		if code, obj := n.modelsWith(t, c["api_key"].Value); code != 200 || obj != "list" {
			t.Fatalf("/v1/models with %s's key: %d %q", name, code, obj)
		}
	}
	if code, _ := n.modelsWith(t, "sk-"+strings.Repeat("0", 48)); code != 401 {
		t.Fatalf("/v1/models with a made-up key: %d, want 401", code)
	}
	tm.lap("用 Key 调 /v1/models")

	// 5. Revoke A: gone, B stays.
	if err := n.revoke(ctx, "hermes-agent", credA); err != nil {
		t.Fatalf("revoke hermes-agent: %v", err)
	}
	tm.lap("收回一把（revoke）")
	if code, _ := n.modelsWith(t, credA["api_key"].Value); code != 401 {
		t.Fatalf("revoked key still works: %d", code)
	}
	if code, obj := n.modelsWith(t, credB["api_key"].Value); code != 200 || obj != "list" {
		t.Fatalf("the other key stopped working: %d %q", code, obj)
	}
	err = n.check(ctx, "hermes-agent", credA)
	ae := asActionError(t, err)
	if ae.Name != "same-token" || ae.Kind != ActionExpect {
		t.Fatalf("check of the revoked token: %+v", ae)
	}
	t.Logf("收回后核对它：%v", err)
	if err := n.check(ctx, "open-webui", credB); err != nil {
		t.Fatalf("check of the other token after the revoke: %v", err)
	}
	tm.lap("收回后核对两把")

	// 6. Revoke A again: the token is not there any more, which counts as
	// taken back (finish_if at same-token: New API answers 200, success
	// false, "record not found"); nothing is deleted.
	if err := n.revoke(ctx, "hermes-agent", credA); err != nil {
		t.Fatalf("second revoke of a token that is gone: %v, want it to count as taken back", err)
	}
	t.Log("对已收回的那把再收回一次：算已收回，没有出错")
	if code, obj := n.modelsWith(t, credB["api_key"].Value); code != 200 || obj != "list" {
		t.Fatalf("the other key stopped working: %d %q", code, obj)
	}
	// A number that is now another consumer's token (a New API set up anew
	// numbers from 1 again): A's credential with B's number. Not deleted.
	cross := map[string]SavedValue{}
	for k, v := range credA {
		cross[k] = v
	}
	cross["token_id"] = credB["token_id"]
	err = n.revoke(ctx, "hermes-agent", cross)
	ae = asActionError(t, err)
	if ae.Name != "same-token" || ae.Kind != ActionExpect {
		t.Fatalf("revoke of a number that is another token now: %+v", ae)
	}
	t.Logf("编号已经是别人的令牌时收回：%v", err)
	if code, obj := n.modelsWith(t, credB["api_key"].Value); code != 200 || obj != "list" {
		t.Fatalf("the other key stopped working: %d %q", code, obj)
	}
	tm.lap("重复收回、编号错位")

	// 7. Names: at most 50 characters in New API. App names here are up to
	// 40 characters, the name written in the manifest is 18 more.
	long32 := "a" + strings.Repeat("b", 30) + "c"
	credL, err := n.issue(ctx, long32)
	if err != nil {
		t.Fatalf("issue to a 32-character app name: %v", err)
	}
	if err := n.revoke(ctx, long32, credL); err != nil {
		t.Fatalf("revoke for the 32-character app name: %v", err)
	}
	long33 := "a" + strings.Repeat("b", 31) + "c"
	_, err = n.issue(ctx, long33)
	ae = asActionError(t, err)
	t.Logf("应用名 33 个字符时发令牌：%v", err)
	if ae.Name != "create" || ae.Kind != ActionExpect {
		t.Fatalf("issue to a 33-character app name: %+v", ae)
	}
	tm.lap("名字长度")
	if code, obj := n.modelsWith(t, credB["api_key"].Value); code != 200 || obj != "list" {
		t.Fatalf("the other key stopped working: %d %q", code, obj)
	}

	// 8. The user turns B's token off in New API. The check steps still pass
	// (it is the same token), but nothing in their outcome says it is off:
	// a pass or a fail is all steps can answer.
	_, login := n.do(t, http.MethodPost, "/api/user/login", "", `{"username":"admin","password":"`+n.password+`"}`)
	data, _ := login["data"].(map[string]any)
	at, _ := data["access_token"].(string)
	req, _ := http.NewRequest(http.MethodPut, n.base+"/api/token/?status_only=true", strings.NewReader(`{"id":`+credB["token_id"].Value+`,"status":2}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+at)
	req.Header.Set("New-Api-User", credB["user_id"].Value)
	resp, err := n.http.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("turning the token off: %v %v", resp, err)
	}
	resp.Body.Close()
	if code, _ := n.modelsWith(t, credB["api_key"].Value); code != 401 {
		t.Fatalf("a token turned off still works: %d", code)
	}
	if err := n.check(ctx, "open-webui", credB); err != nil {
		t.Fatalf("check of a token turned off: %v", err)
	}
	t.Log("令牌被用户停用后：check 照样通过（还是那一把），/v1/models 返回 401；步骤的结果里没有“停用了”这一说")
	tm.lap("停用后核对")
}

// How many issues fit in New API's limit for critical calls, and what the
// steps say once it is reached.
func TestRealNewAPIRateLimit(t *testing.T) {
	n := newRealNewAPI(t, "AFX_REAL_NEWAPI_LIMIT")
	ctx := context.Background()
	start := time.Now()
	if err := n.initSteps(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	var creds []map[string]SavedValue
	var stop error
	for i := 1; i <= 30; i++ {
		c, err := n.issue(ctx, "app"+strconv.Itoa(i))
		if err != nil {
			stop = err
			t.Logf("第 %d 次发令牌没成（用时 %.1f 秒）：%v", i, time.Since(start).Seconds(), err)
			// What it left behind: a token made but not handed out?
			if id := c["token_id"].Value; id != "" {
				t.Logf("  这次已经建了令牌 %s，没取到值", id)
			}
			break
		}
		creds = append(creds, c)
	}
	t.Logf("连续发成了 %d 把（初始化里已经登录过 1 次）", len(creds))
	if stop == nil {
		t.Log("发了 30 次都没被限流")
		return
	}
	// Once limited, check and revoke (both log in) fail as well.
	if len(creds) > 0 {
		err := n.check(ctx, "app1", creds[0])
		t.Logf("被限流后核对：%v", err)
		err = n.revoke(ctx, "app1", creds[0])
		t.Logf("被限流后收回：%v", err)
		// The key itself still works on the model API.
		code, obj := n.modelsWith(t, creds[0]["api_key"].Value)
		t.Logf("被限流后用发下来的 Key 调 /v1/models：HTTP %d %q", code, obj)
	}
}
