package gatewayclient

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConnectRegistersAndSendsCode(t *testing.T) {
	g := newFake()
	var got map[string]any
	g.catalog = func(path string, body []byte) (int, any) {
		_ = json.Unmarshal(body, &got)
		return 200, map[string]any{"ok": true, "catalog": map[string]any{"id": "cat-demo", "name": "演示"}, "subject": "machine:x"}
	}
	srv := httptest.NewServer(g)
	defer srv.Close()
	c := newClient(t, srv.URL)
	conn, err := c.Connect(context.Background(), "AFXC-AAAA-BBBB-CCCC-DDDD")
	if err != nil || conn.CatalogID != "cat-demo" || conn.Name != "演示" {
		t.Fatalf("connect: %+v %v", conn, err)
	}
	if g.registers != 1 {
		t.Fatalf("没登记过的机器应该先自动登记一次：%d", g.registers)
	}
	if got["code"] != "AFXC-AAAA-BBBB-CCCC-DDDD" {
		t.Fatalf("请求体：%v", got)
	}
	if v, ok := got["account_token"]; !ok || v != nil {
		t.Fatalf("account_token 要传 null：%v", got)
	}
}

func TestConnectErrorsAreNotRetried(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{{"GW-CAT-01", 404}, {"GW-CAT-02", 410}, {"GW-CAT-03", 403}, {"GW-CAT-04", 429}} {
		g := newFake()
		calls := 0
		g.catalog = func(path string, body []byte) (int, any) {
			calls++
			return tc.status, map[string]any{"ok": false, "code": tc.code, "message": "x", "retry_after": 1200}
		}
		srv := httptest.NewServer(g)
		c := newClient(t, srv.URL)
		c.sleep = func(context.Context, time.Duration) error { t.Fatal("不该等"); return nil }
		_, err := c.Connect(context.Background(), "AFXC-AAAA-BBBB-CCCC-DDDD")
		srv.Close()
		if CodeOf(err) != tc.code || calls != 1 {
			t.Fatalf("%s: %v（调了 %d 次）", tc.code, err, calls)
		}
		var ge *Error
		if tc.code == "GW-CAT-04" && (!errorsAs(err, &ge) || ge.RetryAfter != 1200*time.Second) {
			t.Fatalf("retry_after 没带出来：%v", err)
		}
	}
}

func errorsAs(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func TestCatalogIndex(t *testing.T) {
	g := newFake()
	var got map[string]any
	g.catalog = func(path string, body []byte) (int, any) {
		if path != "/v1/catalog/index" {
			return 404, map[string]any{"ok": false, "code": "GW-REQ-01"}
		}
		_ = json.Unmarshal(body, &got)
		return 200, map[string]any{"ok": true,
			"catalogs": []any{
				map[string]any{"id": "cat-a", "name": "甲", "serial": 3, "index": `{"schema":1}`, "sig": "c2ln"},
				map[string]any{"id": "cat-b", "serial": 7},
			},
			"revoked": []any{"cat-c"}}
	}
	srv := httptest.NewServer(g)
	defer srv.Close()
	c := newClient(t, srv.URL)
	ans, err := c.CatalogIndex(context.Background(), map[string]int64{"cat-b": 7, "cat-c": 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(ans.Catalogs) != 2 || !ans.Catalogs[0].Full() || ans.Catalogs[1].Full() || ans.Revoked[0] != "cat-c" {
		t.Fatalf("应答：%+v", ans)
	}
	known, _ := got["known"].(map[string]any)
	if known["cat-b"] != float64(7) || known["cat-c"] != float64(1) {
		t.Fatalf("known：%v", got)
	}
}

func TestCatalogIndexRejectsBadAnswers(t *testing.T) {
	for _, bad := range []any{
		map[string]any{"ok": true, "catalogs": []any{map[string]any{"id": "../x", "serial": 1}}, "revoked": []any{}},
		map[string]any{"ok": true, "catalogs": []any{map[string]any{"id": "cat-a", "serial": 0}}, "revoked": []any{}},
		map[string]any{"ok": true, "catalogs": []any{map[string]any{"id": "cat-a", "serial": 1, "index": "{}"}}, "revoked": []any{}},
		map[string]any{"ok": true, "catalogs": []any{}, "revoked": []any{"x/y"}},
	} {
		g := newFake()
		g.catalog = func(string, []byte) (int, any) { return 200, bad }
		srv := httptest.NewServer(g)
		_, err := newClient(t, srv.URL).CatalogIndex(context.Background(), nil)
		srv.Close()
		if CodeOf(err) != "GW-RESP-01" {
			t.Fatalf("%v: %v", bad, err)
		}
	}
}

func TestIssueAppNamesTheCatalog(t *testing.T) {
	g := newFake()
	srv := httptest.NewServer(g)
	defer srv.Close()
	c := newClient(t, srv.URL)
	if _, err := c.IssueApp(context.Background(), "catpkg-new-api-v1", AppRef{Catalog: "cat-a", App: "new-api"}); err != nil {
		t.Fatal(err)
	}
	app, _ := g.lastIssue["app"].(map[string]any)
	if g.lastIssue["purpose"] != "app" || app["catalog"] != "cat-a" || app["app"] != "new-api" || g.lastIssue["file_id"] != "catpkg-new-api-v1" {
		t.Fatalf("请求体：%v", g.lastIssue)
	}
	// A public file still sends "app": null.
	if _, err := c.Issue(context.Background(), "x", "runtime"); err != nil {
		t.Fatal(err)
	}
	if v, ok := g.lastIssue["app"]; !ok || v != nil {
		t.Fatalf("公共文件的 app 要是 null：%v", g.lastIssue)
	}
}

func TestBigIndexAnswer(t *testing.T) {
	g := newFake()
	big := strings.Repeat("x", 200<<10) // larger than the 64 KB limit of the small answers
	g.catalog = func(string, []byte) (int, any) {
		return 200, map[string]any{"ok": true, "catalogs": []any{map[string]any{"id": "cat-a", "serial": 1, "index": big, "sig": "s"}}, "revoked": []any{}}
	}
	srv := httptest.NewServer(g)
	defer srv.Close()
	ans, err := newClient(t, srv.URL).CatalogIndex(context.Background(), nil)
	if err != nil || len(ans.Catalogs[0].Index) != len(big) {
		t.Fatalf("大索引：%v", err)
	}
}

func TestCleanCatalogName(t *testing.T) {
	long := strings.Repeat("目", 150)
	for in, want := range map[string]string{
		"演示客户专用":                       "演示客户专用",
		"  a \t b\n\nc  ":              "a b c",
		"red\x1b[31m!\x07":             "red[31m!",
		"abc‮def":                      "abcdef", // right-to-left override
		"\x1b\x00​":                    "",
		long:                           strings.Repeat("目", 100),
		"x" + strings.Repeat(" ", 200): "x",
	} {
		if got := CleanCatalogName(in); got != want {
			t.Errorf("CleanCatalogName(%q) = %q, want %q", in, got, want)
		}
	}
}
