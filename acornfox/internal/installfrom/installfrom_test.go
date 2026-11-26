package installfrom

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// harness: a fake clock that only moves when the Reporter sleeps, a Register
// that answers from a script, and a log.
type harness struct {
	t      *testing.T
	dir    string
	now    time.Time
	sleeps []time.Duration
	calls  []string
	errs   []error // answers of the next Register calls; empty = success
	logs   []string
	mu     sync.Mutex
}

func newHarness(t *testing.T) *harness {
	return &harness{t: t, dir: t.TempDir(), now: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)}
}

func (h *harness) marker(content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.dir, MarkerFile), []byte(content), 0o600); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) reporter() *Reporter {
	return New(Config{
		DataDir: h.dir,
		Register: func(ctx context.Context, tag string) error {
			h.calls = append(h.calls, tag)
			if len(h.errs) == 0 {
				return nil
			}
			e := h.errs[0]
			h.errs = h.errs[1:]
			return e
		},
		Now: func() time.Time { return h.now },
		Sleep: func(ctx context.Context, d time.Duration) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			h.sleeps = append(h.sleeps, d)
			h.now = h.now.Add(d)
			return nil
		},
		Logf: func(f string, a ...any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.logs = append(h.logs, strings.TrimSpace(fmt.Sprintf(f, a...)))
		},
	})
}

func (h *harness) logText() string { return strings.Join(h.logs, "\n") }

var errDown = errors.New("GW-NET-01 连不上下载网关")

func TestNoMarkerNeverTouchesTheGateway(t *testing.T) {
	h := newHarness(t)
	h.reporter().Run(context.Background())
	if len(h.calls) != 0 || len(h.sleeps) != 0 {
		t.Fatalf("no marker: nothing may happen, calls=%v sleeps=%v", h.calls, h.sleeps)
	}
	if _, err := os.Stat(filepath.Join(h.dir, StateFile)); err == nil {
		t.Fatal("no marker: no state file either")
	}
	if len(h.logs) != 0 {
		t.Fatalf("no marker: silent, got %v", h.logs)
	}
}

func TestReportsOnceAndNotAgainAfterARestart(t *testing.T) {
	h := newHarness(t)
	h.marker("bili-1201\n")
	h.reporter().Run(context.Background())
	if len(h.calls) != 1 || h.calls[0] != "bili-1201" {
		t.Fatalf("one call with the tag, got %v", h.calls)
	}
	if len(h.sleeps) != 1 || h.sleeps[0] != DefaultFirstDelay {
		t.Fatalf("the first try waits FirstDelay, got %v", h.sleeps)
	}
	st := ReadState(h.dir)
	if st.ReportedAt == "" || st.Tag != "bili-1201" || st.Failures != 0 || !st.Done() {
		t.Fatalf("state after success: %+v", st)
	}
	if fi, err := os.Stat(filepath.Join(h.dir, StateFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode: %v %v", fi, err)
	}
	// "Restart": a new Reporter on the same directory does nothing.
	h2 := &harness{t: t, dir: h.dir, now: h.now}
	h2.reporter().Run(context.Background())
	if len(h2.calls) != 0 || len(h2.sleeps) != 0 {
		t.Fatalf("already reported: no second report, calls=%v", h2.calls)
	}
	// And the marker is never rewritten by this package.
	if b, _ := os.ReadFile(filepath.Join(h.dir, MarkerFile)); string(b) != "bili-1201\n" {
		t.Fatalf("marker changed: %q", b)
	}
}

func TestFailureRetriesWithBackoffThenSucceeds(t *testing.T) {
	h := newHarness(t)
	h.marker("yt-3")
	h.errs = []error{errDown, errDown, errDown}
	h.reporter().Run(context.Background())
	if len(h.calls) != 4 {
		t.Fatalf("3 failures then a success = 4 calls, got %d", len(h.calls))
	}
	want := []time.Duration{DefaultFirstDelay, 5 * time.Minute, 15 * time.Minute, time.Hour}
	if len(h.sleeps) != len(want) {
		t.Fatalf("sleeps %v, want %v", h.sleeps, want)
	}
	for i := range want {
		if h.sleeps[i] != want[i] {
			t.Fatalf("sleeps %v, want %v", h.sleeps, want)
		}
	}
	st := ReadState(h.dir)
	if st.ReportedAt == "" || st.Failures != 0 || st.LastError != "" || st.GaveUpAt != "" {
		t.Fatalf("state after the retries: %+v", st)
	}
	if !strings.Contains(h.logText(), "没成功（第 1 次") || !strings.Contains(h.logText(), "已向下载网关报告") {
		t.Fatalf("log: %s", h.logText())
	}
}

func TestBackoffTable(t *testing.T) {
	want := map[int]time.Duration{1: 5 * time.Minute, 2: 15 * time.Minute, 3: time.Hour, 4: 3 * time.Hour, 5: 6 * time.Hour, 9: 6 * time.Hour}
	for n, d := range want {
		if got := Backoff(n); got != d {
			t.Fatalf("Backoff(%d) = %s, want %s", n, got, d)
		}
	}
}

func TestGivesUpAfterSevenDaysAndStaysQuiet(t *testing.T) {
	h := newHarness(t)
	h.marker("bili-1201")
	h.errs = make([]error, 1000)
	for i := range h.errs {
		h.errs[i] = errDown
	}
	h.reporter().Run(context.Background())
	st := ReadState(h.dir)
	if st.GaveUpAt == "" || st.ReportedAt != "" || !st.Done() {
		t.Fatalf("should have given up: %+v", st)
	}
	// 1 minute + 5m + 15m + 1h + 3h, then every 6 hours: a bit over 7 days of tries, nowhere near 1000.
	if n := len(h.calls); n < 25 || n > 40 {
		t.Fatalf("expected about 30 tries in 7 days, got %d", n)
	}
	first, gave := parse(st.FirstTryAt), parse(st.GaveUpAt)
	if gave.Sub(first) < DefaultGiveUpAfter || gave.Sub(first) > DefaultGiveUpAfter+7*time.Hour {
		t.Fatalf("gave up %s after the first try", gave.Sub(first))
	}
	if st.LastError == "" || !strings.Contains(h.logText(), "放弃") {
		t.Fatalf("the reason and a log line are kept: %+v / %s", st, h.logText())
	}
	// Restart after giving up: nothing more.
	h2 := &harness{t: t, dir: h.dir, now: h.now}
	h2.reporter().Run(context.Background())
	if len(h2.calls) != 0 {
		t.Fatalf("given up for good: %v", h2.calls)
	}
}

// runUntilFirstFailure runs a Reporter whose first try fails and stops it ("the server stops") when it is about to wait for the second.
func runUntilFirstFailure(h *harness) {
	h.errs = []error{errDown}
	ctx, cancel := context.WithCancel(context.Background())
	r := h.reporter()
	inner := r.cfg.Sleep
	r.cfg.Sleep = func(c context.Context, d time.Duration) error {
		if len(h.calls) == 1 {
			cancel()
			return c.Err()
		}
		return inner(c, d)
	}
	r.Run(ctx)
}

func TestRestartInTheMiddleKeepsTheRhythm(t *testing.T) {
	h := newHarness(t)
	h.marker("bili-1201")
	runUntilFirstFailure(h)
	if len(h.calls) != 1 {
		t.Fatalf("one try before the stop, got %d", len(h.calls))
	}
	if st := ReadState(h.dir); st.Failures != 1 || st.Done() {
		t.Fatalf("state: %+v", st)
	}
	// The server comes back 2 minutes later: it waits the 3 minutes that were left, not the first-delay again.
	h2 := &harness{t: t, dir: h.dir, now: h.now.Add(2 * time.Minute)}
	h2.reporter().Run(context.Background())
	if len(h2.sleeps) != 1 || h2.sleeps[0] != 3*time.Minute {
		t.Fatalf("remaining wait should be 3m, got %v", h2.sleeps)
	}
	if len(h2.calls) != 1 || ReadState(h.dir).ReportedAt == "" {
		t.Fatalf("calls=%v state=%+v", h2.calls, ReadState(h.dir))
	}
	// Far later than the backoff: no wait at all.
	h3 := newHarness(t)
	h3.marker("bili-1201")
	runUntilFirstFailure(h3)
	h4 := &harness{t: t, dir: h3.dir, now: h3.now.Add(48 * time.Hour)}
	h4.reporter().Run(context.Background())
	if len(h4.sleeps) != 1 || h4.sleeps[0] != 0 {
		t.Fatalf("overdue retry should not wait, got %v", h4.sleeps)
	}
}

// An old gateway answers the registration with 200 but never says "from_recorded": true. That is not "reported":
// retry like a failure (at most 7 days), and count it as reported only once a gateway that knows the field says so.
func TestAnOldGatewayDoesNotCountAsReported(t *testing.T) {
	h := newHarness(t)
	h.marker("bili-1201")
	h.errs = []error{ErrNotRecorded, ErrNotRecorded}
	h.reporter().Run(context.Background())
	if len(h.calls) != 3 {
		t.Fatalf("two not-recorded answers then a recorded one = 3 calls, got %d", len(h.calls))
	}
	if st := ReadState(h.dir); st.ReportedAt == "" || st.Failures != 0 {
		t.Fatalf("reported only after the recorded answer: %+v", st)
	}
	if !strings.Contains(h.logText(), "没有记下来源标记") {
		t.Fatalf("the log says why: %s", h.logText())
	}
}

func TestAnOldGatewayForSevenDaysGivesUp(t *testing.T) {
	h := newHarness(t)
	h.marker("bili-1201")
	h.errs = make([]error, 1000)
	for i := range h.errs {
		h.errs[i] = ErrNotRecorded
	}
	h.reporter().Run(context.Background())
	st := ReadState(h.dir)
	if st.GaveUpAt == "" || st.ReportedAt != "" || !strings.Contains(st.LastError, "没有记下来源标记") {
		t.Fatalf("an old gateway for 7 days: give up, never 'reported': %+v", st)
	}
}

func TestBadMarkerIsIgnoredWithOneLogLine(t *testing.T) {
	for name, content := range map[string]string{
		"upper case":                   "Bili-1201",
		"underscore":                   "bili_1201",
		"space":                        "bili 1201",
		"too long":                     strings.Repeat("a", 33),
		"empty":                        "",
		"only newline":                 "\n",
		"slash":                        "a/b",
		"non ascii":                    "来源",
		"semicolon":                    "a;rm -rf",
		"second line ok but first bad": "-\x00\n",
	} {
		h := newHarness(t)
		h.marker(content)
		h.reporter().Run(context.Background())
		if len(h.calls) != 0 || len(h.sleeps) != 0 {
			t.Fatalf("%s: a bad tag must not be reported, calls=%v", name, h.calls)
		}
		if !strings.Contains(h.logText(), "不合规") {
			t.Fatalf("%s: one log line says why, got %q", name, h.logText())
		}
		if _, err := os.Stat(filepath.Join(h.dir, StateFile)); err == nil {
			t.Fatalf("%s: no state file for an unusable tag", name)
		}
	}
}

func TestValidTags(t *testing.T) {
	for _, s := range []string{"a", "bili-1201", "yt-3", "0", strings.Repeat("z", 32), "a-b-c"} {
		if !Valid(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "A", "a b", "a_b", strings.Repeat("z", 33), "a\n", "\na", "a.b", "é"} {
		if Valid(s) {
			t.Errorf("%q should not be valid", s)
		}
	}
}

func TestMarkerShapes(t *testing.T) {
	for content, want := range map[string]string{"bili-1201": "bili-1201", "bili-1201\n": "bili-1201", "bili-1201\r\n": "bili-1201", "  bili-1201  \n": "bili-1201", "bili-1201\nignored": "bili-1201"} {
		h := newHarness(t)
		h.marker(content)
		if got, found := ReadMarker(h.dir); !found || got != want {
			t.Errorf("%q: got %q found=%v, want %q", content, got, found, want)
		}
	}
	if _, found := ReadMarker(t.TempDir()); found {
		t.Error("no file: not found")
	}
}

func TestDisabledAndNoGatewayDoNothing(t *testing.T) {
	h := newHarness(t)
	h.marker("bili-1201")
	r := h.reporter()
	r.cfg.Disabled = true
	r.Run(context.Background())
	r2 := h.reporter()
	r2.cfg.Register = nil
	r2.Run(context.Background())
	if len(h.calls) != 0 || len(h.sleeps) != 0 {
		t.Fatalf("calls=%v sleeps=%v", h.calls, h.sleeps)
	}

	var c Config
	c.FromEnv(func(k string) string {
		if k == EnvSwitch {
			return "Off"
		}
		return ""
	})
	if !c.Disabled {
		t.Fatal("ACORNFOX_INSTALL_FROM=off switches it off")
	}
	var d Config
	d.FromEnv(func(string) string { return "" })
	if d.Disabled {
		t.Fatal("unset: on")
	}
}

func TestShutdownIsNotAFailure(t *testing.T) {
	h := newHarness(t)
	h.marker("bili-1201")
	ctx, cancel := context.WithCancel(context.Background())
	r := h.reporter()
	r.cfg.Register = func(c context.Context, tag string) error {
		h.calls = append(h.calls, tag)
		cancel() // the server is stopping while the request is in flight
		return context.Canceled
	}
	r.Run(ctx)
	if st := ReadState(h.dir); st.Failures != 0 || st.Done() || st.FirstTryAt != "" {
		t.Fatalf("a try cut off by shutdown is not counted: %+v", st)
	}
}

func TestChangedMarkerStartsOver(t *testing.T) {
	h := newHarness(t)
	h.marker("old-tag")
	h.reporter().Run(context.Background())
	if ReadState(h.dir).Tag != "old-tag" {
		t.Fatal("setup")
	}
	h.marker("new-tag")
	h2 := &harness{t: t, dir: h.dir, now: h.now}
	h2.reporter().Run(context.Background())
	if len(h2.calls) != 1 || h2.calls[0] != "new-tag" {
		t.Fatalf("a changed marker is reported again, got %v", h2.calls)
	}
}

func TestUnwritableStateDoesNotStopTheReport(t *testing.T) {
	h := newHarness(t)
	h.marker("bili-1201")
	// A directory where the state file should go makes every save fail.
	if err := os.Mkdir(filepath.Join(h.dir, StateFile), 0o700); err != nil {
		t.Fatal(err)
	}
	h.reporter().Run(context.Background())
	if len(h.calls) != 1 {
		t.Fatalf("the report still goes out, got %v", h.calls)
	}
}
