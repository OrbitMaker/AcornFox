package platform

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMetadataAnswers(t *testing.T) {
	// Any HTTP answer counts, 401 included (AWS IMDSv2 without a token).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if a, s := metadataAnswers(context.Background(), srv.URL+"/"); !a || !s {
		t.Fatalf("answering server: answered=%v sure=%v", a, s)
	}
	// A refused connection: nothing there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	if a, s := metadataAnswers(context.Background(), "http://"+addr+"/"); a || !s {
		t.Fatalf("refused: answered=%v sure=%v", a, s)
	}
	// A bad URL is not a definite answer.
	if a, s := metadataAnswers(context.Background(), "http://[::1"); a || s {
		t.Fatalf("bad url: answered=%v sure=%v", a, s)
	}
}

func waitProbe(t *testing.T, p *CloudProbe) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		p.mu.Lock()
		running := p.running
		p.mu.Unlock()
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("probe did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCloudProbe(t *testing.T) {
	now := time.Unix(1000, 0)
	mk := func(answered, sure bool, calls *int) *CloudProbe {
		return &CloudProbe{probe: func(context.Context) (bool, bool) { *calls++; return answered, sure },
			retry: 30 * time.Second, now: func() time.Time { return now }}
	}

	var n int
	cloud := mk(true, true, &n)
	if !cloud.OnCloud() {
		t.Fatal("unknown must count as cloud (safe side)")
	}
	waitProbe(t, cloud)
	if !cloud.OnCloud() {
		t.Fatal("metadata answered: cloud")
	}

	n = 0
	home := mk(false, true, &n)
	home.Start()
	waitProbe(t, home)
	if home.OnCloud() {
		t.Fatal("timeout or refused: not a cloud server")
	}
	if n != 1 {
		t.Fatalf("probed %d times, want 1 (a definite result is kept)", n)
	}

	n = 0
	unsure := mk(false, false, &n)
	unsure.Start()
	waitProbe(t, unsure)
	if !unsure.OnCloud() {
		t.Fatal("unsure result stays on the safe side")
	}
	if n != 1 {
		t.Fatalf("retried before 30 seconds: %d probes", n)
	}
	now = now.Add(31 * time.Second)
	unsure.OnCloud()
	waitProbe(t, unsure)
	if n != 2 {
		t.Fatalf("not retried after 30 seconds: %d probes", n)
	}
}
