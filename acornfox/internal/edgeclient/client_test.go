package edgeclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acornfox/acornfox/internal/edge"
)

const token = "0123456789abcdef0123456789abcdef"

// fakeAdapter speaks the protocol of edge/wire.go.
type fakeAdapter struct {
	t         *testing.T
	applied   []edge.Config
	challenge *edge.ChallengeRequest
	health    edge.Health
	open      bool // answer without the token too (exposed)
	status    int  // force this status on every authenticated call
}

func (f *fakeAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !f.open && r.Header.Get(edge.TokenHeader) != token {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(edge.ErrorReply{Error: "要令牌"})
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
		_ = json.NewEncoder(w).Encode(edge.ErrorReply{Error: "坏了"})
		return
	}
	switch {
	case r.Method == http.MethodPut && r.URL.Path == edge.PathConfig:
		var cfg edge.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			f.t.Fatal(err)
		}
		f.applied = append(f.applied, cfg)
		_ = json.NewEncoder(w).Encode(edge.VersionReply{Version: cfg.Version()})
	case r.Method == http.MethodGet && r.URL.Path == edge.PathVersion:
		v := ""
		if n := len(f.applied); n > 0 {
			v = f.applied[n-1].Version()
		}
		_ = json.NewEncoder(w).Encode(edge.VersionReply{Version: v})
	case r.Method == http.MethodPut && r.URL.Path == edge.PathChallenge:
		var c edge.ChallengeRequest
		_ = json.NewDecoder(r.Body).Decode(&c)
		f.challenge = &c
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodDelete && r.URL.Path == edge.PathChallenge:
		f.challenge = nil
		w.WriteHeader(http.StatusNoContent)
	case r.Method == http.MethodGet && r.URL.Path == edge.PathHealth:
		_ = json.NewEncoder(w).Encode(f.health)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func start(t *testing.T, f *fakeAdapter) *Driver {
	f.t = t
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return New(srv.URL, token)
}

func TestApplyVersionChallenge(t *testing.T) {
	f := &fakeAdapter{health: edge.Health{State: edge.StateRunning}}
	d := start(t, f)
	ctx := context.Background()
	cfg := edge.Config{Routes: []edge.Route{{App: "web", PublicPort: 18810, Upstream: "127.0.0.1:32768", Domains: []string{"a.example.com"}}}}
	v, err := d.Apply(ctx, cfg)
	if err != nil || v != cfg.Version() {
		t.Fatalf("Apply = %q %v", v, err)
	}
	if len(f.applied) != 1 || f.applied[0].Routes[0].Upstream != "127.0.0.1:32768" {
		t.Fatalf("applied %+v", f.applied)
	}
	if got, err := d.Version(ctx); err != nil || got != v {
		t.Fatalf("Version = %q %v", got, err)
	}
	if err := d.SetChallenge(ctx, "panel.example.com", "/.well-known/x", "tok"); err != nil || f.challenge == nil || f.challenge.Body != "tok" {
		t.Fatalf("SetChallenge %v %+v", err, f.challenge)
	}
	if err := d.ClearChallenge(ctx); err != nil || f.challenge != nil {
		t.Fatalf("ClearChallenge %v", err)
	}
}

func TestErrorsCarryTheAdaptersReason(t *testing.T) {
	f := &fakeAdapter{status: http.StatusBadGateway}
	d := start(t, f)
	_, err := d.Apply(context.Background(), edge.Config{})
	if err == nil || !strings.Contains(err.Error(), "坏了") || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
	// A wrong token is named as such.
	d.token = "ffffffffffffffffffffffffffffffff"
	f.status = 0
	if _, err := d.Version(context.Background()); err == nil || !strings.Contains(err.Error(), "令牌") {
		t.Fatalf("err = %v", err)
	}
}

func TestHealth(t *testing.T) {
	ctx := context.Background()
	// Healthy: refuses without the token, answers with it.
	f := &fakeAdapter{health: edge.Health{State: edge.StateRunning}}
	if h := start(t, f).Health(ctx); !h.OK() {
		t.Fatalf("health = %+v", h)
	}
	// The adapter's own view passes through (starting, error with reason).
	f = &fakeAdapter{health: edge.Health{State: edge.StateStarting, Reason: "正在启动"}}
	if h := start(t, f).Health(ctx); h.State != edge.StateStarting || h.Reason != "正在启动" {
		t.Fatalf("health = %+v", h)
	}
	// Answers without the token: exposed.
	f = &fakeAdapter{open: true, health: edge.Health{State: edge.StateRunning}}
	if h := start(t, f).Health(ctx); h.Cause != edge.CauseExposed {
		t.Fatalf("health = %+v", h)
	}
	// Wrong token: auth.
	f = &fakeAdapter{health: edge.Health{State: edge.StateRunning}}
	d := start(t, f)
	d.token = "ffffffffffffffffffffffffffffffff"
	if h := d.Health(ctx); h.Cause != edge.CauseAuth {
		t.Fatalf("health = %+v", h)
	}
	// Nothing listens: unreachable.
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if h := New(url, token).Health(ctx); h.Cause != edge.CauseUnreachable {
		t.Fatalf("health = %+v", h)
	}
}

// The token travels in the header only, never in the URL or the body.
func TestTokenOnlyInHeader(t *testing.T) {
	var leaked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(r.URL.String(), token) || strings.Contains(string(b), token) {
			leaked = true
		}
		_ = json.NewEncoder(w).Encode(edge.VersionReply{Version: "v1-x"})
	}))
	defer srv.Close()
	d := New(srv.URL, token)
	_, _ = d.Apply(context.Background(), edge.Config{})
	_, _ = d.Version(context.Background())
	if leaked {
		t.Fatal("token leaked outside the header")
	}
}
