package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// While a pull runs, PullStatus says which source pulls (its place in the
// order), what the earlier ones did, and how far Docker says it is; after the
// pull there is none.
func TestPullStatusFollowsTheSources(t *testing.T) {
	d, _ := fakePulls(true, map[string]ProbeResult{
		"docker.m.daocloud.io": {Status: ProbeOK, MBps: 5},
		"docker.1panel.live":   {Status: ProbeOK, MBps: 3},
		"dockerproxy.net":      {Status: ProbeOK, MBps: 1},
	}, nil)
	tag := ImageTag("big-app", "0123456789ab")
	ctx, untrack := d.trackPull(context.Background(), tag, pinned)
	seen := make(chan PullStatus, 4)
	d.pullHook = func(ctx context.Context, ref string, pw pullWatch) (pullStats, error) {
		tr := trackerFrom(ctx)
		if strings.HasPrefix(ref, "docker.m.daocloud.io/") {
			tr.progress(40<<20, 300<<20)
			st, _ := d.PullStatus(ctx, "big-app", "0123456789ab")
			seen <- st
			return pullStats{Bytes: 40 << 20, Total: 300 << 20, Seconds: 200, AvgKBps: 40}, errPullSlow
		}
		tr.progress(10<<20, 300<<20)
		tr.blind()
		st, _ := d.PullStatus(ctx, "big-app", "0123456789ab")
		seen <- st
		return pullStats{Seconds: 3}, nil
	}
	if _, _, err := d.pullFallback(ctx, pinned, nil); err != nil {
		t.Fatal(err)
	}
	untrack()
	first, second := <-seen, <-seen
	if first.Phase != "pull" || first.Source != "docker.m.daocloud.io" || first.SourceIndex != 1 || first.Sources != 4 ||
		first.Bytes != 40<<20 || first.Total != 300<<20 || first.Blind || len(first.Earlier) != 0 ||
		first.LimitSeconds != int(pullMaxTotal/time.Second) {
		t.Fatalf("first %+v", first)
	}
	if second.Source != "docker.1panel.live" || second.SourceIndex != 2 || !second.Blind || second.BytesPerSec != 0 ||
		len(second.Earlier) != 1 || !strings.Contains(second.Earlier[0], "docker.m.daocloud.io 太慢") {
		t.Fatalf("second %+v", second)
	}
	if _, err := d.PullStatus(context.Background(), "big-app", "0123456789ab"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after the pull: %v", err)
	}
}

// The speed is over the last pullRateWindow of Docker's messages and falls
// while none come; before any message there is none.
func TestPullStatusSpeed(t *testing.T) {
	tr := &pullTracker{start: time.Now()}
	tr.source("docker.m.daocloud.io", 1, 3)
	now := time.Now()
	if st := tr.status(now); st.BytesPerSec != 0 || st.Bytes != 0 {
		t.Fatalf("no message yet: %+v", st)
	}
	t0 := now.Add(-30 * time.Second)
	tr.mu.Lock()
	tr.srcStart = t0
	tr.samples = []speedSample{{t0.Add(5 * time.Second), 5 << 20}, {now.Add(-20 * time.Second), 10 << 20}, {now.Add(-10 * time.Second), 20 << 20}}
	tr.st.Bytes = 20 << 20
	tr.mu.Unlock()
	st := tr.status(now) // 10 MB over the last 20 s
	if st.BytesPerSec < 500<<10 || st.BytesPerSec > 530<<10 {
		t.Fatalf("speed %d", st.BytesPerSec)
	}
	later := tr.status(now.Add(20 * time.Second)) // nothing came since: the speed falls
	if later.BytesPerSec >= st.BytesPerSec {
		t.Fatalf("speed did not fall: %d then %d", st.BytesPerSec, later.BytesPerSec)
	}
	var nilTracker *pullTracker
	nilTracker.progress(1, 2) // a pull nobody follows
	nilTracker.source("x", 1, 1)
	nilTracker.blind()
	nilTracker.gaveUp("x")
	nilTracker.probing()
}

// Over the runner socket: the status of a running pull, 404 (ErrNotFound)
// for none, and an API without it answers "unsupported".
func TestPullStatusRoundTrip(t *testing.T) {
	d := &Docker{}
	_, cli := newTestServer(t, d)
	if _, err := cli.PullStatus(context.Background(), "big-app", "0123456789ab"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no pull: %v", err)
	}
	ctx, untrack := d.trackPull(context.Background(), ImageTag("big-app", "0123456789ab"), pinned)
	defer untrack()
	trackerFrom(ctx).source("dockerproxy.net", 2, 3)
	trackerFrom(ctx).progress(3<<20, 9<<20)
	st, err := cli.PullStatus(context.Background(), "big-app", "0123456789ab")
	if err != nil || st.Source != "dockerproxy.net" || st.Bytes != 3<<20 || st.Total != 9<<20 || st.Ref != pinned {
		t.Fatalf("%+v %v", st, err)
	}
	_, plain := newTestServer(t, &fakeAPI{})
	if _, err := plain.PullStatus(context.Background(), "big-app", "0123456789ab"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("an API without pull status: %v", err)
	}
	if _, err := cli.PullStatus(context.Background(), "big-app", "bad id"); err == nil {
		t.Fatal("a bad deployment id was taken")
	}
}
