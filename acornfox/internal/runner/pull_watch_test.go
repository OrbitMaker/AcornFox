package runner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// pullStream is one scripted message of a fake daemon's image pull stream,
// sent after the given pause.
type pullStream struct {
	pause time.Duration
	msg   map[string]any
}

func progressMsg(id, status string, cur, total int64) map[string]any {
	m := map[string]any{"id": id, "status": status}
	if total > 0 {
		m["progressDetail"] = map[string]int64{"current": cur, "total": total}
	}
	return m
}

// fakePullDaemon answers POST /images/create with the scripted stream (and
// stops early when the client goes away).
func fakePullDaemon(t *testing.T, stream []pullStream) *Docker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/images/create") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		enc := json.NewEncoder(w)
		for _, s := range stream {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(s.pause):
			}
			if enc.Encode(s.msg) != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(srv.Close)
	cli, err := client.New(client.WithHost("tcp://"+srv.Listener.Addr().String()), client.WithVersion("1.56"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return NewDocker(cli)
}

func shortSlowRules(t *testing.T) {
	t.Helper()
	g, w, c, sw, fl := slowGrace, slowWindow, slowCheckEvery, slowSwitchKBps, slowFloorKBps
	slowGrace, slowWindow, slowCheckEvery = 300*time.Millisecond, 400*time.Millisecond, 50*time.Millisecond
	slowSwitchKBps, slowFloorKBps = 100, 16
	t.Cleanup(func() { slowGrace, slowWindow, slowCheckEvery, slowSwitchKBps, slowFloorKBps = g, w, c, sw, fl })
	quietHost(t)
}

// quietHost makes the machine's counters stand still (the test machine's own
// traffic must not count as a pull's).
func quietHost(t *testing.T) {
	t.Helper()
	old := hostActivity
	hostActivity = func() (int64, bool) { return 0, true }
	t.Cleanup(func() { hostActivity = old })
}

// trickle is a layer download that moves kbps KB/s for d, in 50 ms steps.
func trickle(id string, kbps float64, d time.Duration, total int64) []pullStream {
	var out []pullStream
	var cur int64
	step := 50 * time.Millisecond
	for el := time.Duration(0); el < d; el += step {
		cur += int64(kbps * 1024 * step.Seconds())
		out = append(out, pullStream{step, progressMsg(id, "Downloading", cur, total)})
	}
	return out
}

// The real watch loop on a real message stream: a layer that keeps moving
// at 40 KB/s is given up only when a source after it probed at least twice
// as fast, and one under the floor is given up even as the last source.
func TestPullWatchedSlowRules(t *testing.T) {
	shortSlowRules(t)
	head := []pullStream{{0, map[string]any{"status": "Pulling from library/app", "id": "1"}}, {0, progressMsg("aaa", "Pulling fs layer", 0, 0)}}
	slow40 := append(append([]pullStream{}, head...), trickle("aaa", 40, 3*time.Second, 300<<20)...)

	d := fakePullDaemon(t, slow40)
	t0 := time.Now()
	st, err := d.pullWatched(context.Background(), "app", nil, 5*time.Second, pullWatch{hasNext: true, nextMBps: 2})
	if !errors.Is(err, errPullSlow) {
		t.Fatalf("40 KB/s with a 2 MB/s source next: err %v", err)
	}
	if el := time.Since(t0); el < slowGrace || el > 2*time.Second {
		t.Fatalf("given up after %v (grace %v)", el, slowGrace)
	}
	if st.Bytes <= 0 || st.Total != 300<<20 || st.AvgKBps < 20 || st.AvgKBps > 60 {
		t.Fatalf("stats %+v", st)
	}

	// Same speed, nothing faster known: the pull goes on to the end.
	short := append(append([]pullStream{}, head...), trickle("aaa", 40, time.Second, 300<<20)...)
	short = append(short, pullStream{0, progressMsg("aaa", "Download complete", 0, 0)}, pullStream{0, progressMsg("aaa", "Pull complete", 0, 0)})
	d = fakePullDaemon(t, short)
	if _, err := d.pullWatched(context.Background(), "app", nil, 5*time.Second, pullWatch{hasNext: true}); err != nil {
		t.Fatalf("40 KB/s without a faster source must go on: %v", err)
	}

	slow5 := append(append([]pullStream{}, head...), trickle("aaa", 5, 3*time.Second, 300<<20)...)
	d = fakePullDaemon(t, slow5)
	if _, err := d.pullWatched(context.Background(), "app", nil, 5*time.Second, pullWatch{}); !errors.Is(err, errPullSlow) {
		t.Fatalf("5 KB/s is under the floor even for the last source: %v", err)
	}
}

// Extracting a big layer brings no bytes but is not slow, and a stream that
// goes quiet is stalled.
func TestPullWatchedExtractAndStall(t *testing.T) {
	shortSlowRules(t)
	s := []pullStream{
		{0, progressMsg("aaa", "Pulling fs layer", 0, 0)},
		{10 * time.Millisecond, progressMsg("aaa", "Downloading", 1<<20, 2<<20)},
		{10 * time.Millisecond, progressMsg("aaa", "Download complete", 0, 0)},
	}
	for range 20 { // 1 s of extracting, far past the grace, no new bytes
		s = append(s, pullStream{50 * time.Millisecond, progressMsg("aaa", "Extracting", 0, 0)})
	}
	s = append(s, pullStream{0, progressMsg("aaa", "Pull complete", 0, 0)})
	d := fakePullDaemon(t, s)
	if _, err := d.pullWatched(context.Background(), "app", nil, 5*time.Second, pullWatch{}); err != nil {
		t.Fatalf("extracting is not slow: %v", err)
	}

	quiet := []pullStream{{0, progressMsg("aaa", "Pulling fs layer", 0, 0)}, {3 * time.Second, progressMsg("aaa", "Downloading", 1, 2)}}
	d = fakePullDaemon(t, quiet)
	t0 := time.Now()
	if _, err := d.pullWatched(context.Background(), "app", nil, 200*time.Millisecond, pullWatch{}); !errors.Is(err, errPullStalled) {
		t.Fatalf("quiet stream: %v", err)
	}
	if el := time.Since(t0); el > 2*time.Second {
		t.Fatalf("stall noticed after %v", el)
	}
}

// movingHost makes the machine's counters grow at kbps KB/s.
func movingHost(t *testing.T, kbps float64) {
	t.Helper()
	old := hostActivity
	t0 := time.Now()
	hostActivity = func() (int64, bool) { return int64(time.Since(t0).Seconds() * kbps * 1024), true }
	t.Cleanup(func() { hostActivity = old })
}

// Docker sends no layer progress at all (a pull that continues an image an
// earlier pull left half done, see host_activity.go): while the machine's
// counters move, the pull is waited for; when they stand still too, it is
// stalled.
func TestPullWatchedBlind(t *testing.T) {
	shortSlowRules(t)
	silent := []pullStream{
		{0, map[string]any{"status": "Pulling from library/app", "id": "1"}},
		{1500 * time.Millisecond, map[string]any{"status": "Digest: sha256:abc"}},
		{0, map[string]any{"status": "Status: Downloaded newer image for app"}},
	}
	movingHost(t, 500)
	d := fakePullDaemon(t, silent)
	st, err := d.pullWatched(context.Background(), "app", nil, 200*time.Millisecond, pullWatch{hasNext: true})
	if err != nil {
		t.Fatalf("moving counters, no messages: %v", err)
	}
	if !st.Blind {
		t.Fatalf("pull not marked blind: %+v", st)
	}

	// Moving, but under the floor: given up (as slow or stalled).
	movingHost(t, 5)
	d = fakePullDaemon(t, silent)
	if _, err := d.pullWatched(context.Background(), "app", nil, 200*time.Millisecond, pullWatch{}); !errors.Is(err, errPullStalled) && !errors.Is(err, errPullSlow) {
		t.Fatalf("counters under the floor: %v", err)
	}

	quietHost(t)
	d = fakePullDaemon(t, silent)
	t0 := time.Now()
	if _, err := d.pullWatched(context.Background(), "app", nil, 200*time.Millisecond, pullWatch{}); !errors.Is(err, errPullStalled) {
		t.Fatalf("quiet counters, no messages: %v", err)
	}
	if el := time.Since(t0); el > time.Second {
		t.Fatalf("stall noticed after %v", el)
	}

	// Counters that cannot be read (not Linux): as before, quiet means stalled.
	old := hostActivity
	hostActivity = func() (int64, bool) { return 0, false }
	defer func() { hostActivity = old }()
	d = fakePullDaemon(t, silent)
	if _, err := d.pullWatched(context.Background(), "app", nil, 200*time.Millisecond, pullWatch{}); !errors.Is(err, errPullStalled) {
		t.Fatalf("no counters, no messages: %v", err)
	}
}
