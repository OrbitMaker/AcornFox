package cli

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/acornfox/acornfox/internal/apiserver"
	"github.com/acornfox/acornfox/internal/appinstall"
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/client"
	"github.com/acornfox/acornfox/internal/state"
)

// The service commands against the real API server and client, with the
// services test catalog (three service types; the built-in snapshot has two).

// noRunner and noEngineRunner satisfy the server's and the engine's runner
// interfaces; the service routes never reach a container here.
type noRunner struct{ apiserver.Runner }
type noEngineRunner struct{ appinstall.Runner }
type noKicker struct{}

func (noKicker) Kick(string) {}

type svcHarness struct {
	*harness
	eng *appinstall.Engine
	db  *state.Store
	url string
}

func newSvcHarness(t *testing.T) *svcHarness {
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
	eng := appinstall.New(appinstall.Config{Store: db, Runner: noEngineRunner{}, DataDir: dir,
		Catalog: appinstall.NewCatalog(appinstall.CatalogConfig{CacheDir: filepath.Join(dir, "catalog"), Snapshot: snap, Sources: []string{"http://127.0.0.1:1"}})})
	t.Cleanup(eng.Close)
	ts := httptest.NewServer(apiserver.New(apiserver.Config{Store: db, Kicker: noKicker{}, Runner: noRunner{}, UploadDir: dir, AppCenter: eng}))
	t.Cleanup(ts.Close)
	h := newHarness(t)
	h.addTarget("dev", ts.URL)
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) { return client.Connect(ctx, tgt) }
	return &svcHarness{harness: h, eng: eng, db: db, url: ts.URL}
}

// runIn runs the CLI with stdin.
func (h *svcHarness) runIn(stdin string, args ...string) (int, string, string) {
	h.t.Helper()
	var out, errBuf bytes.Buffer
	code := MainWithConnector(context.Background(), args, strings.NewReader(stdin), &out, &errBuf, func(string) string { return "" }, h.connect, h.configDir, h.workDir)
	return code, out.String(), errBuf.String()
}

func (h *svcHarness) stored(name string) map[string]string {
	h.t.Helper()
	reg, err := h.eng.Registry()
	if err != nil {
		h.t.Fatal(err)
	}
	sv, err := reg.GetService(context.Background(), name)
	if err != nil {
		h.t.Fatal(err)
	}
	return sv.Values
}

func noSecret(t *testing.T, secret string, texts ...string) {
	t.Helper()
	for _, s := range texts {
		if strings.Contains(s, secret) {
			t.Fatalf("secret %q in output:\n%s", secret, s)
		}
	}
}

func TestServicesAddReadsSecretsFromStdin(t *testing.T) {
	h := newSvcHarness(t)
	const pw, user = "pw-secret-9z8y", "me@example.com"
	// Two items from standard input, read in the order written.
	code, out, errOut := h.runIn(user+"\n"+pw+"\n", "services", "add", "smtp", "--name", "我的邮箱", "--set", "host=mail.example.com", "--set", "username=-", "--set", "password=-")
	if code != exitOK {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if v := h.stored("我的邮箱"); v["password"] != pw || v["username"] != user || v["port"] != "587" {
		t.Fatalf("stored %v", v)
	}
	noSecret(t, pw, out, errOut)
	for _, want := range []string{"已登记。", "我的邮箱：邮箱（发信），自己登记的", "状态：可用", "密码（password） = " + appinstall.Masked + "9z8y", "acornfox services test 我的邮箱"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	// Not enough lines on standard input: a usage error, nothing sent.
	code, _, errOut = h.runIn("only-one\n", "services", "add", "smtp", "--name", "x", "--set", "host=h.example.com", "--set", "username=-", "--set", "password=-")
	if code != exitUsage || !strings.Contains(errOut, "password=- 要从标准输入读一行") {
		t.Fatalf("exit %d %s", code, errOut)
	}
	if code, out, _ := h.run("--json", "services", "list"); code != exitOK || strings.Contains(out, `"name":"x"`) {
		t.Fatalf("a refused add registered something: %s", out)
	}
	// A secret on the command line is taken, kept out of the output, and the
	// user is told.
	const key = "sk-cmdline-key-5566"
	code, out, errOut = h.runIn("", "services", "add", "llm_gateway", "--name", "上游", "--set", "base_url=https://api.example.com/v1", "--set", "api_key="+key)
	if code != exitOK || !strings.Contains(errOut, "api_key 是保密项，写在命令行里会留在命令历史里") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	noSecret(t, key, out, errOut)
	if h.stored("上游")["api_key"] != key {
		t.Fatal("not stored")
	}
	// --json: the service as the server answers it, plus ok.
	code, out, _ = h.runIn("sk-json-key-1234\n", "--json", "services", "add", "llm_gateway", "--name", "j", "--set", "base_url=https://x.example.com/v1", "--set", "api_key=-")
	m := decodeJSON(t, out)
	if code != exitOK || m["ok"] != true || m["name"] != "j" || m["status"] != "ok" {
		t.Fatalf("json: %d %s", code, out)
	}
	noSecret(t, "sk-json-key-1234", out)
}

func TestServicesRefusalsAndExitCodes(t *testing.T) {
	h := newSvcHarness(t)
	for _, c := range []struct {
		args []string
		exit int
		says string
	}{
		{[]string{"services", "add", "redis", "--name", "r"}, exitServerError, "CAT-SVC-01"},
		{[]string{"services", "add", "smtp", "--name", "m"}, exitServerError, "还差必填项"},
		{[]string{"services", "add", "smtp", "--name", "m", "--set", "host=localhost"}, exitServerError, "CAT-SVC-07"},
		{[]string{"services", "add", "smtp", "--set", "host=h.example.com"}, exitUsage, "--name"},
		{[]string{"services", "add", "smtp", "--name", "m", "--set", "host"}, exitUsage, "项=值"},
		{[]string{"services", "show"}, exitUsage, "services show 名字"},
		{[]string{"services", "show", "nope"}, exitServerError, "没有叫 nope 的服务"},
		{[]string{"services", "set", "nope"}, exitUsage, "要改哪一项"},
		{[]string{"services", "frob"}, exitUsage, "未知的 services 子命令"},
	} {
		code, out, errOut := h.run(c.args...)
		if code != c.exit || !strings.Contains(out+errOut, c.says) {
			t.Errorf("%v: exit %d\n%s%s", c.args, code, out, errOut)
		}
	}
	code, out, _ := h.run("--json", "services", "add", "redis", "--name", "r")
	if d := decodeJSON(t, out); code != exitServerError || d["ok"] != false || d["diagnosis"].(map[string]any)["code"] != "CAT-SVC-01" {
		t.Fatalf("json refusal: %s", out)
	}
	// Help.
	for _, args := range [][]string{{"services"}, {"services", "add", "-h"}, {"services", "remove", "-h"}} {
		code, out, _ := h.run(args...)
		if code != exitOK || !strings.Contains(out, "用法：acornfox services") {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	_, out, _ = h.run("services", "add", "-h")
	for _, want := range []string{"副作用", "退出码", "例子", "项=-", "命令历史"} {
		if !strings.Contains(out, want) {
			t.Errorf("add -h lacks %q", want)
		}
	}
	if _, out, _ := h.run("help"); !strings.Contains(out, "services add 类型") {
		t.Fatal("services not in the help")
	}
}

func TestServicesSetShowListKinds(t *testing.T) {
	h := newSvcHarness(t)
	if code, _, errOut := h.runIn("pw-old-1111\n", "services", "add", "smtp", "--name", "mail", "--set", "host=mail.example.com", "--set", "password=-"); code != exitOK {
		t.Fatal(errOut)
	}
	reg, _ := h.eng.Registry()
	if _, err := reg.SetBinding(context.Background(), appinstall.Binding{App: "memos", UseName: "mail", Service: "mail"}); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := h.runIn("pw-new-2222\n", "services", "set", "mail", "port=465", "password=-")
	if code != exitOK || !strings.Contains(out, "注：有 1 个应用在用它，重新应用设置后才生效：memos") || !strings.Contains(out, "端口（port） = 465") {
		t.Fatalf("set: %d\n%s\n%s", code, out, errOut)
	}
	noSecret(t, "pw-new-2222", out, errOut)
	if v := h.stored("mail"); v["password"] != "pw-new-2222" || v["port"] != "465" {
		t.Fatalf("%v", v)
	}
	code, out, _ = h.run("services", "list")
	if code != exitOK || !strings.Contains(out, "mail  邮箱（发信）  自己登记的  可用  在用它的：memos（mail）") {
		t.Fatalf("list:\n%s", out)
	}
	code, out, _ = h.run("services", "show", "mail")
	if code != exitOK || !strings.Contains(out, "在用它的：memos（mail）") || !strings.Contains(out, "发件人地址（from） 没填") {
		t.Fatalf("show:\n%s", out)
	}
	noSecret(t, "pw-new-2222", out)
	code, out, _ = h.run("--json", "services", "show", "mail")
	if m := decodeJSON(t, out); code != exitOK || m["ok"] != true || m["name"] != "mail" || len(m["used_by"].([]any)) != 1 {
		t.Fatalf("show json: %s", out)
	}
	code, out, _ = h.run("services", "kinds")
	if code != exitOK || !strings.Contains(out, "邮箱（发信）（smtp）") || !strings.Contains(out, "port 端口：整数，默认 587") ||
		strings.Contains(out, "demo-provider") || strings.Contains(out, "能提供它") ||
		!strings.Contains(out, "登记一条：acornfox services add smtp --name 名字") || !strings.Contains(out, "tls 加密方式：从 starttls、tls、none 里选") {
		t.Fatalf("kinds:\n%s", out)
	}
	if code, out, _ = h.run("--json", "services", "kinds"); code != exitOK || len(decodeJSON(t, out)["kinds"].([]any)) != 3 {
		t.Fatalf("kinds json: %s", out)
	}
	// A refused change names the item, not the value.
	code, out, errOut = h.run("services", "set", "mail", "host=127.0.0.1")
	if code != exitServerError || !strings.Contains(errOut, "CAT-SVC-07") || !strings.Contains(errOut, "acornfox faults CAT-SVC-07") {
		t.Fatalf("loopback: %d %s %s", code, out, errOut)
	}
}

func TestServicesTestCommand(t *testing.T) {
	h := newSvcHarness(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
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
	reg, _ := h.eng.Registry()
	if _, err := reg.CreateService(context.Background(), appinstall.Service{Name: "local", Kind: "smtp", Source: state.ServiceFromUser,
		Values: map[string]string{"host": "127.0.0.1", "port": strconv.Itoa(port)}}); err != nil {
		t.Fatal(err)
	}
	code, out, _ := h.run("services", "test", "local")
	if code != exitOK || !strings.Contains(out, "通了：连上了 127.0.0.1:"+strconv.Itoa(port)) {
		t.Fatalf("up: %d %s", code, out)
	}
	l.Close()
	code, out, _ = h.run("services", "test", "local")
	if code != exitOpFailed || !strings.Contains(out, "没通：") || !strings.Contains(out, "拒绝连接") || !strings.Contains(out, "CAT-SVC-06") {
		t.Fatalf("down: %d %s", code, out)
	}
	code, out, _ = h.run("--json", "services", "test", "local")
	if m := decodeJSON(t, out); code != exitOpFailed || m["ok"] != false || m["code"] != "CAT-SVC-06" {
		t.Fatalf("json: %d %s", code, out)
	}
	if _, err := reg.CreateService(context.Background(), appinstall.Service{Name: "demo", Kind: "demo_token", Source: state.ServiceFromUser,
		Values: map[string]string{"url": "http://x.example.com", "token": "t-1234567890"}}); err != nil {
		t.Fatal(err)
	}
	code, out, _ = h.run("services", "test", "demo")
	if code != exitOpFailed || !strings.Contains(out, "试不了：") || !strings.Contains(out, "没有试连的办法") {
		t.Fatalf("no test: %d %s", code, out)
	}
}

func TestServicesRemoveIsADryRunWithoutYes(t *testing.T) {
	h := newSvcHarness(t)
	if code, _, errOut := h.run("services", "add", "smtp", "--name", "mail", "--set", "host=mail.example.com"); code != exitOK {
		t.Fatal(errOut)
	}
	reg, _ := h.eng.Registry()
	ctx := context.Background()
	if _, _, err := h.db.EnsureApp(ctx, "memos"); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.SetBinding(ctx, appinstall.Binding{App: "memos", UseName: "mail", Service: "mail"}); err != nil {
		t.Fatal(err)
	}
	code, out, _ := h.run("services", "remove", "mail")
	if code != exitOK || !strings.Contains(out, "试跑，什么都没删") || !strings.Contains(out, "还有应用在用它：memos（mail）") {
		t.Fatalf("dry run in use: %d %s", code, out)
	}
	code, out, _ = h.run("--json", "services", "remove", "mail")
	if m := decodeJSON(t, out); code != exitOK || m["dry_run"] != true || m["would_remove"] != false || m["blocked"] != "in_use" || len(m["used_by"].([]any)) != 1 {
		t.Fatalf("dry run json: %s", out)
	}
	code, out, errOut := h.run("services", "remove", "mail", "--yes")
	if code != exitServerError || !strings.Contains(errOut, "CAT-SVC-04") || !strings.Contains(errOut, "memos（mail）") {
		t.Fatalf("in use: %d %s %s", code, out, errOut)
	}
	// Deleted with its data kept, memos no longer keeps the service: the
	// dry run and the removal say its binding goes too.
	if err := h.db.DeleteApp(ctx, "memos", true); err != nil {
		t.Fatal(err)
	}
	code, out, _ = h.run("services", "remove", "mail")
	if code != exitOK || !strings.Contains(out, "会删：mail") || !strings.Contains(out, "--yes") || !strings.Contains(out, "以前接它的 memos 已经删了") {
		t.Fatalf("dry run: %d %s", code, out)
	}
	h.stored("mail") // still there
	code, out, _ = h.run("--json", "services", "remove", "mail", "--yes")
	m := decodeJSON(t, out)
	if notes, _ := m["notes"].([]any); code != exitOK || m["removed"] != "mail" || len(notes) != 1 || !strings.Contains(notes[0].(string), "memos") || !strings.Contains(notes[0].(string), "重新接服务") {
		t.Fatalf("remove: %d %s", code, out)
	}
	if _, err := reg.GetBinding(ctx, "memos", "mail"); err == nil {
		t.Fatal("the binding of the deleted app stayed")
	}
	if code, _, errOut := h.run("services", "show", "mail"); code != exitServerError || !strings.Contains(errOut, "not_found") && !strings.Contains(errOut, "没有叫 mail") {
		t.Fatalf("still there: %s", errOut)
	}
}

// A removal the server answers with 204 (nothing to say about apps deleted
// earlier) is a success: no EOF from decoding an empty body.
func TestServicesRemoveNoContent(t *testing.T) {
	h := newSvcHarness(t)
	if code, _, errOut := h.run("services", "add", "smtp", "--name", "mail", "--set", "host=mail.example.com"); code != exitOK {
		t.Fatal(errOut)
	}
	if code, _, errOut := h.run("services", "add", "smtp", "--name", "mail2", "--set", "host=mail2.example.com"); code != exitOK {
		t.Fatal(errOut)
	}
	code, out, errOut := h.run("services", "remove", "mail", "--yes")
	if code != exitOK || !strings.Contains(out, "已删除服务 mail") || errOut != "" {
		t.Fatalf("remove: %d %s %s", code, out, errOut)
	}
	code, out, errOut = h.run("--json", "services", "remove", "mail2", "--yes")
	m := decodeJSON(t, out)
	if notes, ok := m["notes"].([]any); code != exitOK || m["ok"] != true || m["removed"] != "mail2" || !ok || len(notes) != 0 {
		t.Fatalf("remove json: %d %s %s", code, out, errOut)
	}
	if code, _, _ := h.run("services", "show", "mail"); code != exitServerError {
		t.Fatal("mail still there")
	}
}

// Words after a command that it does not take (the rest of a sentence copied
// along with it) are a usage error that names them; a missing type says so.
// Nothing is read from standard input and nothing is registered.
func TestServicesExtraArgs(t *testing.T) {
	h := newSvcHarness(t)
	const key = "sk-abcdef1234567890"
	code, out, errOut := h.runIn(key+"\n", "services", "add", "smtp", "--name", "m", "--set", "host=mail.example.com", "--set", "password=-",
		"运行完请回复‘登记好了’或者把输出贴给我（输出里不会有", "Key）")
	if code != exitUsage || !strings.Contains(errOut, "多了参数：运行完请回复") || !strings.Contains(errOut, "命令要单独一行") || strings.Contains(errOut, "少了类型") {
		t.Fatalf("add with trailing words: %d %s %s", code, out, errOut)
	}
	noSecret(t, key, out, errOut)
	if code, _, _ := h.run("services", "show", "m"); code != exitServerError {
		t.Fatal("registered although refused")
	}
	// A key typed where a word does not belong is not printed back.
	code, _, errOut = h.run("services", "add", "smtp", "--name", "m", "--set", "host=mail.example.com", key)
	if code != exitUsage || !strings.Contains(errOut, "多了参数：") || strings.Contains(errOut, key) || !strings.Contains(errOut, "已遮住") {
		t.Fatalf("secret-like word: %d %s", code, errOut)
	}
	// 项=值 written without --set: only the part before "=" shows, whatever
	// the value looks like, and the error says to put --set in front.
	for _, w := range []string{"api_key=sk-abcdef1234567890", "password=hunter2", "token=" + strings.Repeat("x", 30)} {
		k, v, _ := strings.Cut(w, "=")
		code, out, errOut = h.run("services", "add", "llm_gateway", "--name", "m", w)
		if code != exitUsage || !strings.Contains(errOut, "多了参数："+k+"=******") || !strings.Contains(errOut, "--set 项=值") {
			t.Fatalf("%s: %d %s", w, code, errOut)
		}
		for _, part := range []string{v, v[:4]} {
			if strings.Contains(out+errOut, part) {
				t.Fatalf("%s: value shows in %q", w, out+errOut)
			}
		}
	}
	// A long word of letters only may be a key as well.
	letters := "abcdefghijklmnopqrstuv"
	code, _, errOut = h.run("services", "add", "smtp", "--name", "m", letters)
	if code != exitUsage || strings.Contains(errOut, letters[:8]) || !strings.Contains(errOut, "已遮住") {
		t.Fatalf("letters only: %d %s", code, errOut)
	}
	// Many words: the first few, each cut short, and how many.
	code, _, errOut = h.run("services", "add", "smtp", "--name", "m", "a", "b", "c", "d", "e", strings.Repeat("长", 40))
	if code != exitUsage || !strings.Contains(errOut, "多了参数：a b c …（共 6 个）") {
		t.Fatalf("many: %d %s", code, errOut)
	}
	code, _, errOut = h.run("services", "add", "--name", "m", "--set", "host=mail.example.com")
	if code != exitUsage || !strings.Contains(errOut, "少了类型") {
		t.Fatalf("no type: %d %s", code, errOut)
	}
	code, out, errOut = h.run("--json", "services", "add", "smtp", "--name", "m", "多余的话")
	if code != exitUsage || !strings.Contains(out+errOut, "多了参数：多余的话") {
		t.Fatalf("json: %d %s %s", code, out, errOut)
	}
	// set: words after the name that are no 项=值.
	if code, _, errOut := h.run("services", "add", "smtp", "--name", "m", "--set", "host=mail.example.com"); code != exitOK {
		t.Fatal(errOut)
	}
	code, _, errOut = h.run("services", "set", "m", "port=465", "改好后告诉我")
	if code != exitUsage || !strings.Contains(errOut, "多了参数：改好后告诉我") || !strings.Contains(errOut, "命令要单独一行") {
		t.Fatalf("set with trailing words: %d %s", code, errOut)
	}
	if v := h.stored("m"); v["port"] == "465" {
		t.Fatal("changed although refused")
	}
}

// A server without the service routes: the command says the server is too
// old, it does not show a raw 404.
func TestServicesOnOldServer(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			_, _ = io.WriteString(w, `{"api_version":1,"runner":"ok"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer old.Close()
	h := newHarness(t)
	h.addTarget("old", old.URL)
	h.connect = func(ctx context.Context, tgt client.Target) (client.API, error) { return client.Connect(ctx, tgt) }
	code, _, errOut := h.run("services", "list")
	if code != exitServerError || !strings.Contains(errOut, "版本较旧，不支持“服务”") || !strings.Contains(errOut, "升级") {
		t.Fatalf("%d %s", code, errOut)
	}
	// A connection that has no service calls at all (an old client fake).
	h2 := newHarness(t)
	h2.addTarget("dev", "http://127.0.0.1:1")
	code, _, errOut = h2.run("services", "kinds")
	if code != exitServerError || !strings.Contains(errOut, "不支持“服务”") {
		t.Fatalf("fake: %d %s", code, errOut)
	}
}

// The help of the service commands names no particular app and no catalog
// app that could provide a kind: the platform knows services by kind only.
func TestServicesHelpNamesNoApp(t *testing.T) {
	for _, u := range []string{servicesUsage, servicesKindsUsage, servicesListUsage, servicesShowUsage, servicesAddUsage, servicesSetUsage, servicesTestUsage, servicesRemoveUsage} {
		low := strings.ToLower(u)
		for _, bad := range []string{"new api", "new-api", "newapi", "hermes", "providers", "能提供它"} {
			if strings.Contains(low, bad) {
				t.Errorf("help mentions %q:\n%s", bad, u)
			}
		}
	}
}
