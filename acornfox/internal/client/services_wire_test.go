package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/client"
)

// A server older than the service routes answers with its router's plain
// 404 (or 405): every call says the server is too old, in words.
func TestServicesOnOldServer(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			_, _ = w.Write([]byte(`{"api_version":1,"runner":"ok"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer old.Close()
	ctx := context.Background()
	c, err := client.Connect(ctx, client.Target{URL: old.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var sa client.ServicesAPI = c
	calls := map[string]func() error{
		"kinds": func() error { _, err := sa.ServiceKinds(ctx); return err },
		"list":  func() error { _, err := sa.Services(ctx); return err },
		"show":  func() error { _, err := sa.Service(ctx, "我的邮箱"); return err },
		"add": func() error {
			_, err := sa.AddService(ctx, client.AddServiceRequest{Name: "m", Kind: "smtp"})
			return err
		},
		"set":    func() error { _, err := sa.PatchService(ctx, "m", map[string]string{"port": "25"}); return err },
		"test":   func() error { _, err := sa.TestService(ctx, "m"); return err },
		"remove": func() error { _, err := sa.RemoveService(ctx, "m"); return err },
	}
	for name, call := range calls {
		var ce *client.Error
		if err := call(); !errors.As(err, &ce) || ce.Diag.Code != client.CodeServerTooOld || !strings.Contains(ce.Diag.Message, "不支持“服务”") || ce.Diag.Hint == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The new server's own answers pass through: its not_found and CAT-SVC
// errors are not "too old", names are escaped, values travel as text.
func TestServicesWire(t *testing.T) {
	var got struct {
		method, path string
		body         map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.body = r.Method, r.URL.EscapedPath(), nil
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			_ = json.Unmarshal(b, &got.body)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/status":
			_, _ = w.Write([]byte(`{"api_version":1,"runner":"ok"}`))
		case r.URL.Path == "/v1/services/nope":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"这台机器上没有叫 nope 的服务"}}`))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"CAT-SVC-04","message":"还有应用在用 m，不能删：memos（mail）","hint":"先删应用","used_by":[{"app":"memos","use":"mail"}]}}`))
		case r.Method == http.MethodPatch:
			_, _ = w.Write([]byte(`{"name":"我的 邮箱","kind":"smtp","status":"ok","values":[{"key":"port","value":"25","set":true}],"used_by":[{"app":"memos","use":"mail"}],"notes":["有 1 个应用在用它，重新应用设置后才生效：memos"]}`))
		case strings.HasSuffix(r.URL.Path, "/test"):
			_, _ = w.Write([]byte(`{"ok":false,"detail":"连不上 mail.example.com:25：拒绝连接","code":"CAT-SVC-06","target":"mail.example.com:25"}`))
		default:
			_, _ = w.Write([]byte(`{"services":[]}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	c, err := client.Connect(ctx, client.Target{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	var ce *client.Error
	if _, err := c.Service(ctx, "nope"); !errors.As(err, &ce) || ce.Diag.Code != "not_found" || ce.Status != http.StatusNotFound {
		t.Fatalf("not found: %v", err)
	}
	if _, err := c.RemoveService(ctx, "m"); !errors.As(err, &ce) || ce.Diag.Code != "CAT-SVC-04" || ce.Diag.Hint != "先删应用" {
		t.Fatalf("in use: %v", err)
	}
	s, err := c.PatchService(ctx, "我的 邮箱", map[string]string{"port": "25"})
	if err != nil || got.method != http.MethodPatch || got.path != "/v1/services/%E6%88%91%E7%9A%84%20%E9%82%AE%E7%AE%B1" ||
		got.body["values"].(map[string]any)["port"] != "25" || len(s.Notes) != 1 || len(s.UsedBy) != 1 || !strings.Contains(string(s.Raw), `"notes"`) {
		t.Fatalf("patch: %+v %v %+v", s, err, got)
	}
	r, err := c.TestService(ctx, "m")
	if err != nil || r.OK || r.Code != "CAT-SVC-06" || r.Target != "mail.example.com:25" || got.method != http.MethodPost {
		t.Fatalf("test: %+v %v", r, err)
	}
	if _, err := c.AddService(ctx, client.AddServiceRequest{Name: "m", Kind: "smtp"}); err != nil || got.body["values"] == nil {
		t.Fatalf("add sends values as an object even when empty: %v %+v", err, got.body)
	}
	l, err := c.Services(ctx)
	if err != nil || l.Services == nil && string(l.Raw) != `{"services":[]}` {
		t.Fatalf("list: %+v %v", l, err)
	}
}

// DELETE answered with 204 No Content (nothing to say): a success with no
// notes, not an error from decoding the empty body.
func TestRemoveServiceNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/status" {
			_, _ = w.Write([]byte(`{"api_version":1,"runner":"ok"}`))
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	ctx := context.Background()
	c, err := client.Connect(ctx, client.Target{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	notes, err := c.RemoveService(ctx, "m")
	if err != nil || notes != nil {
		t.Fatalf("204: %v %v", notes, err)
	}
}
