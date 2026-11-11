package containermetrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	appcontracts "github.com/acornfox/acornfox/internal/application/contracts"
	"github.com/acornfox/acornfox/internal/domain"
)

func historySample(cid string, started, at time.Time) appcontracts.ImageMetricsResult {
	cpu, memory := uint64(0), uint64(1024)
	return appcontracts.ImageMetricsResult{State: appcontracts.ImageLifecycleResult{Running: true, VerifiedIdentity: true, ContainerID: cid, ImageID: "sha256:image", ManifestDigest: "sha256:manifest", HostPort: 39001, ContainerPort: 80, ObservedAt: at}, Available: true, SampledAt: at, ProcessStartedAt: started, CPUUsageMillis: &cpu, MemoryUsageBytes: &memory}
}

type phaseStart struct {
	id domain.ID
	at time.Time
}

// phaseClient is a read that never answers: each call reports when it started
// (in virtual time) and then blocks until its deadline or cancellation.
type phaseClient struct {
	clock    *virtualClock
	started  chan phaseStart
	inflight atomic.Int32
	peak     atomic.Int32
}

func (c *phaseClient) ReadManagedImageMetrics(ctx context.Context, q appcontracts.ImageMetricsRequest) (appcontracts.ImageMetricsResult, error) {
	n := c.inflight.Add(1)
	for old := c.peak.Load(); n > old && !c.peak.CompareAndSwap(old, n); old = c.peak.Load() {
	}
	defer c.inflight.Add(-1)
	select {
	case c.started <- phaseStart{id: q.DeploymentID, at: c.clock.Now()}:
	case <-ctx.Done():
		return appcontracts.ImageMetricsResult{}, ctx.Err()
	}
	<-ctx.Done()
	return appcontracts.ImageMetricsResult{}, ctx.Err()
}

// virtualClock drives the sampler in virtual time. Ticks and read deadlines
// happen only when the test says so, so the schedule under test does not
// depend on how fast the machine runs.
type virtualClock struct {
	mu        sync.Mutex
	now       time.Time
	ticks     chan time.Time // unbuffered: a send returns once Run has taken the tick
	intervals []time.Duration
	budgets   []time.Duration
	timers    []virtualTimer
}

type virtualTimer struct {
	due    time.Time
	cancel context.CancelFunc
}

func newVirtualClock(start time.Time) *virtualClock {
	return &virtualClock{now: start, ticks: make(chan time.Time)}
}

func (c *virtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *virtualClock) NewTicker(d time.Duration) (<-chan time.Time, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.intervals = append(c.intervals, d)
	return c.ticks, func() {}
}

func (c *virtualClock) WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.budgets = append(c.budgets, d)
	c.timers = append(c.timers, virtualTimer{due: c.now.Add(d), cancel: cancel})
	return ctx, cancel
}

func (c *virtualClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

// expire ends every read whose deadline is at or before the current virtual
// time and reports how many it ended.
func (c *virtualClock) expire() int {
	c.mu.Lock()
	var due []context.CancelFunc
	kept := c.timers[:0]
	for _, timer := range c.timers {
		if timer.due.After(c.now) {
			kept = append(kept, timer)
		} else {
			due = append(due, timer.cancel)
		}
	}
	c.timers = kept
	c.mu.Unlock()
	for _, cancel := range due {
		cancel()
	}
	return len(due)
}

func (c *virtualClock) recorded() (intervals, budgets []time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.intervals...), append([]time.Duration(nil), c.budgets...)
}

// hangGuard only stops a broken build from hanging the run. No expected state
// waits on real time, so a slow or loaded machine just takes longer.
const hangGuard = 30 * time.Second

func activeReads(s *Sampler) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, id := range s.active {
		if !id.Empty() {
			n++
		}
	}
	return n
}

func waitActiveReads(t *testing.T, s *Sampler, want int) {
	t.Helper()
	guard := time.Now().Add(hangGuard)
	for activeReads(s) != want {
		if time.Now().After(guard) {
			t.Fatalf("in-flight reads stayed at %d, want %d", activeReads(s), want)
		}
		runtime.Gosched()
	}
}

func receiveStart(t *testing.T, client *phaseClient) phaseStart {
	t.Helper()
	select {
	case started := <-client.started:
		return started
	case <-time.After(hangGuard):
		t.Fatal("a dispatched read never started")
		return phaseStart{}
	}
}

func joinSampler(t *testing.T, s *Sampler) {
	t.Helper()
	joined := make(chan struct{})
	go func() { s.wg.Wait(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(hangGuard):
		t.Fatal("in-flight reads did not join after cancellation")
	}
}

func phaseTargets() []appcontracts.ImageMetricsRequest {
	targets := make([]appcontracts.ImageMetricsRequest, 32)
	for i := range targets {
		targets[i] = appcontracts.ImageMetricsRequest{AdminID: "admin", DeploymentID: domain.ID(fmt.Sprintf("dep-phase-%02d", i))}
	}
	return targets
}

// rotateSlowReads runs one launch rotation over 32 targets whose reads always
// run into the read deadline, using the production interval and budget in
// virtual time. When a deadline and a tick fall on the same instant,
// tickFirst lets the tick see both slots still busy (the slow phase the
// freshness budget is sized for); otherwise the slots free up first.
func rotateSlowReads(t *testing.T, tickFirst bool) ([]phaseStart, *phaseClient, *virtualClock) {
	t.Helper()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := newVirtualClock(start)
	targets := phaseTargets()
	client := &phaseClient{clock: clock, started: make(chan phaseStart, len(targets))}
	s := &Sampler{Store: fixedTargetStore{selection: appcontracts.ImageMetricsTargetSelection{Targets: targets}}, History: NewHistory(start), clock: clock}
	s.SetClient(client)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); joinSampler(t, s) }()
	expire := func() {
		before := activeReads(s)
		if n := clock.expire(); n > 0 {
			waitActiveReads(t, s, before-n)
		}
	}
	var launches []phaseStart
	for step := 0; len(launches) < len(targets); step++ {
		if step > 4*len(targets) {
			t.Fatalf("no full launch rotation after %d ticks; reached %d", step, len(launches))
		}
		clock.set(start.Add(time.Duration(step) * sampleInterval))
		if !tickFirst {
			expire()
		}
		before := activeReads(s)
		s.tick(ctx)
		for launched := activeReads(s) - before; launched > 0; launched-- {
			launches = append(launches, receiveStart(t, client))
		}
		if tickFirst {
			expire()
		}
	}
	for i, started := range launches {
		pair := i / 2
		if started.id != targets[2*pair].DeploymentID && started.id != targets[2*pair+1].DeploymentID {
			t.Fatalf("slow tick skipped/duplicated pair %d: launch %d was %s", pair, i, started.id)
		}
		if i%2 == 1 && started.id == launches[i-1].id {
			t.Fatalf("pair %d launched %s twice", pair, started.id)
		}
	}
	return launches, client, clock
}

func TestSamplerThirtyTwoSlowReadsTickPhaseAndCancellationJoin(t *testing.T) {
	pairs := time.Duration(32 / 2)
	for _, tc := range []struct {
		name      string
		tickFirst bool
		rotation  time.Duration
	}{
		// A read ending just after a tick waits one more tick: each pair takes
		// budget + interval, which is what staleAfter is sized for.
		{name: "tick_before_deadline", tickFirst: true, rotation: (pairs - 1) * (readBudget + sampleInterval)},
		// Slots free up first: the next pair starts on the deadline tick.
		{name: "deadline_before_tick", tickFirst: false, rotation: (pairs - 1) * readBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launches, client, clock := rotateSlowReads(t, tc.tickFirst)
			if c := client.peak.Load(); c != 2 {
				t.Fatalf("background read concurrency was %d, want exactly two slots", c)
			}
			_, budgets := clock.recorded()
			for _, budget := range budgets {
				if budget != readBudget {
					t.Fatalf("read deadline %v, want production %v", budget, readBudget)
				}
			}
			elapsed := launches[len(launches)-1].at.Sub(launches[0].at)
			if elapsed != tc.rotation {
				t.Fatalf("32-target slow rotation took %v, want %v", elapsed, tc.rotation)
			}
			if limit := staleAfter(32); elapsed > limit {
				t.Fatalf("slow rotation %v exceeded the %v freshness budget", elapsed, limit)
			}
			t.Logf("32_target_slow_rotation=%s stale_after=%s", elapsed, staleAfter(32))
		})
	}

	t.Run("cancel_joins_inflight_reads", func(t *testing.T) {
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		clock := newVirtualClock(start)
		targets := phaseTargets()
		client := &phaseClient{clock: clock, started: make(chan phaseStart, len(targets))}
		s := &Sampler{Store: fixedTargetStore{selection: appcontracts.ImageMetricsTargetSelection{Targets: targets}}, History: NewHistory(start), clock: clock}
		s.SetClient(client)
		ctx, cancel := context.WithCancel(context.Background())
		done := s.Start(ctx)
		// Run's first tick launches the first pair at once; in virtual time
		// their deadlines never pass, so only cancellation can end them.
		first, second := receiveStart(t, client), receiveStart(t, client)
		if first.id == second.id || (first.id != targets[0].DeploymentID && first.id != targets[1].DeploymentID) || (second.id != targets[0].DeploymentID && second.id != targets[1].DeploymentID) {
			cancel()
			t.Fatalf("first pair changed: %s %s", first.id, second.id)
		}
		// Two more ticks while both slots are busy. The second send returns
		// only after Run has finished the first tick, which must have found
		// no free slot and launched nothing.
		for i := 1; i <= 2; i++ {
			select {
			case clock.ticks <- start.Add(time.Duration(i) * sampleInterval):
			case <-time.After(hangGuard):
				cancel()
				t.Fatal("Run stopped taking ticks from its ticker")
			}
		}
		if extra := len(client.started); extra != 0 {
			cancel()
			t.Fatalf("busy slots queued %d catch-up reads", extra)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(hangGuard):
			t.Fatal("cancel did not join all in-flight sample reads")
		}
		if n := client.inflight.Load(); n != 0 {
			t.Fatalf("Start reported done with %d reads still running", n)
		}
		intervals, budgets := clock.recorded()
		if len(intervals) != 1 || intervals[0] != sampleInterval {
			t.Fatalf("ticker intervals %v, want one production %v ticker", intervals, sampleInterval)
		}
		// The tick still in progress at cancellation may legally launch reads
		// that end at once, so only the first pair's count is fixed.
		if len(budgets) < 2 {
			t.Fatalf("read deadlines %v, want at least the first pair", budgets)
		}
		for _, budget := range budgets {
			if budget != readBudget {
				t.Fatalf("read deadline %v, want production %v", budget, readBudget)
			}
		}
	})
}

func TestHistoryFixedCapacityRotationExpiryAndIndependentCopies(t *testing.T) {
	if size := unsafe.Sizeof(History{}); size > 8<<20 {
		t.Fatalf("fixed retained history exceeds 8 MiB: %d", size)
	} else {
		t.Logf("fixed_history_bytes=%d", size)
	}
	base := time.Now().UTC().Add(-30 * time.Minute)
	h := NewHistory(base.Add(-time.Minute))
	id := domain.ID("dep-history")
	if err := h.SetActiveTargets(appcontracts.ImageMetricsTargetSelection{Targets: []appcontracts.ImageMetricsRequest{{AdminID: "admin", DeploymentID: id}}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= appcontracts.ImageMetricsHistorySamples; i++ {
		at := base.Add(time.Duration(i) * 5 * time.Second)
		if err := h.Record(id, historySample("same-cid", base.Add(-time.Minute), at), at); err != nil {
			t.Fatal(err)
		}
	}
	end := base.Add(30 * time.Minute)
	recent := h.Recent(id, "same-cid", 360, end)
	if len(recent.Samples) != 360 || recent.HistoryStart == nil || !recent.HistoryStart.Equal(base.Add(5*time.Second)) || recent.RecordingStatus != "recording" || recent.StaleAfterSeconds != 30 {
		t.Fatalf("count, expiry or start drifted: %d %+v", len(recent.Samples), recent)
	}
	firstSegment := recent.CurrentSegmentID
	newStart := end.Add(time.Second)
	if err := h.Record(id, historySample("same-cid", newStart, end.Add(5*time.Second)), end.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	recent = h.Recent(id, "same-cid", 360, end.Add(5*time.Second))
	if len(recent.Samples) != 360 || recent.CurrentSegmentID == firstSegment || recent.Samples[len(recent.Samples)-2].SegmentID != firstSegment || recent.SegmentStart == nil || !recent.SegmentStart.Equal(end.Add(5*time.Second)) {
		t.Fatal("same-CID process restart crossed a history segment")
	}
	recent.Samples[len(recent.Samples)-1].MemoryUsageBytes = nil
	if last := h.Recent(id, "same-cid", 1, end.Add(5*time.Second)).Samples[0]; last.MemoryUsageBytes == nil || *last.MemoryUsageBytes != 1024 {
		t.Fatal("caller mutated retained history")
	}
	payload, err := json.Marshal(recent)
	if err != nil || len(payload) > appcontracts.ImageMetricsHistoryJSONBytes {
		t.Fatal("bounded 360-point response exceeded its wire ceiling")
	}
	selection := make([]appcontracts.ImageMetricsRequest, 32)
	selection[0] = appcontracts.ImageMetricsRequest{AdminID: "admin", DeploymentID: id}
	for i := 1; i < len(selection); i++ {
		selection[i] = appcontracts.ImageMetricsRequest{AdminID: "admin", DeploymentID: domain.ID(fmt.Sprintf("dep-other-%02d", i))}
	}
	if err := h.SetActiveTargets(appcontracts.ImageMetricsTargetSelection{Targets: selection}); err != nil {
		t.Fatal(err)
	}
	if staleAfter(32) != 250*time.Second || h.Recent(id, "same-cid", 1, end.Add(85*time.Second)).Stale || !h.Recent(id, "same-cid", 1, end.Add(256*time.Second)).Stale {
		t.Fatal("multi-target scheduling budget falsely marked normal rotation stale")
	}
	notRunningAt := end.Add(10 * time.Second)
	notRunning := appcontracts.ImageMetricsResult{State: appcontracts.ImageLifecycleResult{Running: false, VerifiedIdentity: true, ContainerID: "same-cid", ImageID: "sha256:image", ManifestDigest: "sha256:manifest", HostPort: 39001, ContainerPort: 80, ObservedAt: notRunningAt}, UnavailableReason: "not_running"}
	if err := h.Record(id, notRunning, notRunningAt); err != nil {
		t.Fatal(err)
	}
	stopped := h.Recent(id, "same-cid", 2, notRunningAt)
	if stopped.Samples[1].Available || stopped.Samples[1].SegmentID == stopped.Samples[0].SegmentID || stopped.Samples[1].CPUUsageMillis != nil || stopped.RecordingStatus != "stale" || stopped.Reason != "not_running" || !stopped.Stale {
		t.Fatal("stop was merged into running segment or invented numeric zero")
	}
	stoppedJSON, err := json.Marshal(stopped.Samples[1])
	if err != nil || strings.Contains(string(stoppedJSON), "process_started_at") {
		t.Fatal("unobserved process start leaked as a zero timestamp")
	}
	if expired := h.Recent(id, "same-cid", 2, notRunningAt.Add(31*time.Minute)); expired.RecordingStatus != "stale" || expired.Reason != "sample_expired" || len(expired.Samples) != 0 {
		t.Fatal("expired history was mislabeled as first-sample warm-up")
	}
}

type fixedTargetStore struct {
	selection appcontracts.ImageMetricsTargetSelection
}

func (s fixedTargetStore) ListActiveManagedImageMetricTargets(context.Context, int) (appcontracts.ImageMetricsTargetSelection, error) {
	return s.selection, nil
}

type blockedMetricsClient struct {
	started chan appcontracts.ImageMetricsRequest
	release chan struct{}
}

func (c *blockedMetricsClient) ReadManagedImageMetrics(ctx context.Context, q appcontracts.ImageMetricsRequest) (appcontracts.ImageMetricsResult, error) {
	select {
	case c.started <- q:
	case <-ctx.Done():
		return appcontracts.ImageMetricsResult{}, ctx.Err()
	}
	select {
	case <-c.release:
		return appcontracts.ImageMetricsResult{}, errors.New("fixture read failed")
	case <-ctx.Done():
		return appcontracts.ImageMetricsResult{}, ctx.Err()
	}
}

func TestSamplerThirtyTwoTargetsTwoInflightWithoutCatchup(t *testing.T) {
	targets := make([]appcontracts.ImageMetricsRequest, 32)
	for i := range targets {
		targets[i] = appcontracts.ImageMetricsRequest{AdminID: "admin", DeploymentID: domain.ID(fmt.Sprintf("dep-%02d", i))}
	}
	h := NewHistory(time.Now().UTC())
	client := &blockedMetricsClient{started: make(chan appcontracts.ImageMetricsRequest, 4), release: make(chan struct{})}
	s := &Sampler{Store: fixedTargetStore{selection: appcontracts.ImageMetricsTargetSelection{Targets: targets, Limited: true}}, History: h}
	s.SetClient(client)
	s.tick(context.Background())
	first, second := <-client.started, <-client.started
	if (first.DeploymentID != targets[0].DeploymentID && first.DeploymentID != targets[1].DeploymentID) || (second.DeploymentID != targets[0].DeploymentID && second.DeploymentID != targets[1].DeploymentID) || first.DeploymentID == second.DeploymentID {
		t.Fatal("first bounded target pair changed")
	}
	s.tick(context.Background())
	select {
	case unexpected := <-client.started:
		t.Fatalf("queued a catch-up request while two reads were blocked: %s", unexpected.DeploymentID)
	default:
	}
	close(client.release)
	s.wg.Wait()
	s.tick(context.Background())
	third, fourth := <-client.started, <-client.started
	s.wg.Wait()
	if (third.DeploymentID != targets[2].DeploymentID && third.DeploymentID != targets[3].DeploymentID) || (fourth.DeploymentID != targets[2].DeploymentID && fourth.DeploymentID != targets[3].DeploymentID) || third.DeploymentID == fourth.DeploymentID {
		t.Fatal("slow-read tick skipped a target or broke round-robin order")
	}
	if got := h.Recent(targets[31].DeploymentID, "full-cid", 1, time.Now().UTC()); !got.Scheduled || !got.SelectionLimited || got.RecordingStatus != "warming_up" || got.StaleAfterSeconds != 250 {
		t.Fatalf("32-target warm-up contract drifted: %+v", got)
	}
	replacement := append([]appcontracts.ImageMetricsRequest(nil), targets[1:]...)
	replacement = append(replacement, appcontracts.ImageMetricsRequest{AdminID: "admin", DeploymentID: "dep-32"})
	if err := h.SetActiveTargets(appcontracts.ImageMetricsTargetSelection{Targets: replacement, Limited: true}); err != nil {
		t.Fatal(err)
	}
	if got := h.Recent(targets[0].DeploymentID, "full-cid", 1, time.Now().UTC()); got.Scheduled || got.RecordingStatus != "not_selected" || !got.SelectionLimited {
		t.Fatal("evicted target was still presented as monitored")
	}
}
