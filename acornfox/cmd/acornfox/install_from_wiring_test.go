package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/acornfox/acornfox/internal/installfrom"
)

// registerWithFrom is W1 with client.from; it is "done" only when the gateway says from_recorded.
func TestRegisterWithFromNeedsFromRecorded(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recorded bool
		want     error
	}{{"new gateway", true, nil}, {"old gateway: 200 without from_recorded", false, installfrom.ErrNotRecorded}} {
		var got map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Client map[string]any `json:"client"`
			}
			_ = json.Unmarshal(body, &req)
			got = req.Client
			ans := map[string]any{"ok": true, "install_id": r.Header.Get("X-AFX-Install"), "limits": map[string]any{"per_day": 20}}
			if tc.recorded {
				ans["from_recorded"] = true
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ans)
		}))
		err := registerWithFrom(context.Background(), t.TempDir(), srv.URL, "bili-1201")
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.name, err, tc.want)
		}
		if got["from"] != "bili-1201" || got["via"] != "acornfox" {
			t.Fatalf("%s: the request's client object: %v", tc.name, got)
		}
	}
	if err := registerWithFrom(context.Background(), t.TempDir(), "", "bili-1201"); !errors.Is(err, installfrom.ErrGatewayOff) {
		t.Fatalf("gateway off: %v", err)
	}
	// A gateway that refuses is a failure to retry, not "recorded".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()
	if err := registerWithFrom(context.Background(), t.TempDir(), srv.URL, "bili-1201"); err == nil {
		t.Fatal("a 500 must be an error")
	}
}
