package appinstall

// Tests of the segmented download (segdl.go, segcache.go) against a fake
// server that limits the speed of each connection, breaks connections,
// refuses expired addresses and serves a file that does not match.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func init() {
	segPersistEvery = 20 * time.Millisecond
}

// limitedServer serves body with Range support. perConn is the speed of one
// connection in bytes per second (0: unlimited); hook may change what one
// request gets (called with the request number, counted from 1).
type limitedServer struct {
	body    []byte
	perConn int
	hook    func(n int, r *http.Request) (status int, cut int)
	ignore  bool // ignore Range

	n      atomic.Int64
	cur    atomic.Int64
	maxCur atomic.Int64
	served atomic.Int64 // bytes written
	mu     sync.Mutex
	ranges []string
}

func (s *limitedServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := int(s.n.Add(1))
	c := s.cur.Add(1)
	defer s.cur.Add(-1)
	for {
		m := s.maxCur.Load()
		if c <= m || s.maxCur.CompareAndSwap(m, c) {
			break
		}
	}
	s.mu.Lock()
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	s.mu.Unlock()
	cut := -1
	if s.hook != nil {
		var status int
		status, cut = s.hook(n, r)
		if status > 0 {
			w.WriteHeader(status)
			return
		}
	}
	start, end := 0, len(s.body)-1
	if rg := r.Header.Get("Range"); rg != "" && !s.ignore {
		v := strings.TrimPrefix(rg, "bytes=")
		a, b, _ := strings.Cut(v, "-")
		start, _ = strconv.Atoi(a)
		if b != "" {
			end, _ = strconv.Atoi(b)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.body)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)))
		w.WriteHeader(http.StatusOK)
	}
	part := s.body[start : end+1]
	if s.ignore {
		part = s.body
	}
	sent := 0
	for sent < len(part) {
		if r.Context().Err() != nil {
			return // the client went away
		}
		if cut >= 0 && sent >= cut {
			panic(http.ErrAbortHandler)
		}
		chunk := 16 << 10
		if sent+chunk > len(part) {
			chunk = len(part) - sent
		}
		if cut >= 0 && sent+chunk > cut {
			chunk = cut - sent
		}
		_, _ = w.Write(part[sent : sent+chunk])
		w.(http.Flusher).Flush()
		s.served.Add(int64(chunk))
		sent += chunk
		if s.perConn > 0 {
			time.Sleep(time.Duration(float64(chunk) / float64(s.perConn) * float64(time.Second)))
		}
	}
}

func (s *limitedServer) rangeHeaders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ranges...)
}

func segBody(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*131 + i/251) % 251)
	}
	return b
}

func testSegParams(start, max int) segParams {
	return segParams{Enabled: true, StartConns: start, MaxConns: max, SegSize: 64 << 10, Window: 40 * time.Millisecond}
}

type segRun struct {
	dir   string
	cache *dlCacheEntry
	dest  string
	want  string
	st    *dlStatus
}

func newSegRun(t *testing.T, body []byte, prm segParams) *segRun {
	t.Helper()
	dir := t.TempDir()
	want := sha256hex(body)
	return &segRun{dir: dir, want: want, dest: filepath.Join(dir, "out.tar"), st: &dlStatus{},
		cache: newDLCacheEntry(filepath.Join(dir, "dl-cache"), want, int64(len(body)), prm.SegSize)}
}

func (r *segRun) download(ctx context.Context, src dlSource, prm segParams) error {
	return segDownload(ctx, http.DefaultClient, src, r.cache, r.dest, r.want, r.st, prm)
}

func (r *segRun) cacheFiles(t *testing.T) []string {
	t.Helper()
	es, _ := os.ReadDir(filepath.Join(r.dir, "dl-cache"))
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// Several connections at once; the file comes out whole, the cache is gone,
// the number of connections went up to the most the parameters allow.
func TestSegDownloadManyConnections(t *testing.T) {
	// Whether the speed rose is the controller's own test (below, stepped by
	// events); here every window counts as a rise so a loaded machine
	// cannot change what is checked.
	oldGain := segGainMin
	segGainMin = 0
	defer func() { segGainMin = oldGain }()
	body := segBody(4 << 20)
	s := &limitedServer{body: body, perConn: 1 << 20}
	srv := httptest.NewServer(s)
	defer srv.Close()
	prm := testSegParams(4, 8)
	r := newSegRun(t, body, prm)
	if err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(r.dest)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("file differs (%d bytes, %v)", len(got), err)
	}
	if files := r.cacheFiles(t); len(files) != 0 {
		t.Fatalf("cache left behind: %v", files)
	}
	if r.st.done.Load() != int64(len(body)) {
		t.Fatalf("done %d", r.st.done.Load())
	}
	if m := s.maxCur.Load(); m != 8 {
		t.Fatalf("most connections at once %d, want 8 (speed kept rising)", m)
	}
	for _, rg := range s.rangeHeaders() {
		if !strings.HasPrefix(rg, "bytes=") || !strings.Contains(rg, "-") || strings.HasSuffix(rg, "-") {
			t.Fatalf("every request asks for a closed range, got %q", rg)
		}
	}
	if fi, _ := os.Stat(r.dest); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, the runner has to read it", fi.Mode().Perm())
	}
}

// Many connections are used at once (what makes the download faster over a
// server that limits each connection): all the start workers have a request
// in flight at the same time. Counted, not timed.
func TestSegDownloadUsesManyConnectionsAtOnce(t *testing.T) {
	body := segBody(1 << 20)
	s := &limitedServer{body: body, perConn: 512 << 10}
	srv := httptest.NewServer(s)
	defer srv.Close()
	prm := testSegParams(8, 8)
	r := newSegRun(t, body, prm)
	if err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm); err != nil {
		t.Fatal(err)
	}
	if m := s.maxCur.Load(); m != 8 {
		t.Fatalf("most connections at once %d, want 8", m)
	}
}

// The speed does not rise with more connections (the server's whole line is
// the limit): the controller goes back to the start number. The pure step
// function is tested for each stage.
func TestSegControllerSteps(t *testing.T) {
	c := &segController{start: 16, max: 32}
	// first window, no failures, plenty left: go to 32
	if got := c.step(16, 100, 50<<20, 0); got != 32 {
		t.Fatalf("first window: %d, want 32", got)
	}
	// second window, speed rose by 30%: stay at 32
	if got := c.step(32, 100, 65<<20, 0); got != 32 {
		t.Fatalf("rose: %d, want 32", got)
	}
	// later windows: four failures halve it
	if got := c.step(32, 100, 65<<20, 4); got != 16 {
		t.Fatalf("failures: %d, want 16", got)
	}
	// the other way: no rise goes back
	c = &segController{start: 16, max: 32}
	c.step(16, 100, 50<<20, 0)
	if got := c.step(32, 100, 52<<20, 0); got != 16 {
		t.Fatalf("no rise: %d, want 16", got)
	}
	// a failure in the second window also goes back
	c = &segController{start: 16, max: 32}
	c.step(16, 100, 50<<20, 0)
	if got := c.step(32, 100, 90<<20, 1); got != 16 {
		t.Fatalf("failure in second window: %d, want 16", got)
	}
	// a failure in the first window: stays at 16
	c = &segController{start: 16, max: 32}
	if got := c.step(16, 100, 50<<20, 1); got != 16 {
		t.Fatalf("failure in first window: %d, want 16", got)
	}
	// too few segments left to feed 32: stays
	c = &segController{start: 16, max: 32}
	if got := c.step(16, 20, 50<<20, 0); got != 16 {
		t.Fatalf("few left: %d, want 16", got)
	}
	// never below segMinConns
	c = &segController{start: 6, max: 6, stage: 2}
	if got := c.step(6, 100, 1, 9); got != segMinConns {
		t.Fatalf("floor: %d, want %d", got, segMinConns)
	}
}

// The controller as a whole, stepped by events instead of the clock: each
// window is one tick the test sends after setting the bytes and failures of
// that window, and it waits for the controller to finish the window.
func TestSegControlSteppedByEvents(t *testing.T) {
	run := func(t *testing.T, windows []struct {
		bytes int64
		fails int64
	}, want []int) {
		ticks := make(chan time.Time)
		stepped := make(chan int)
		j := &segJob{prm: testSegParams(2, 8), pending: make([]int, 100), tick: ticks,
			onStep: func(target int) { stepped <- target }}
		j.prm.Window = time.Second // the divisor: bytes per window = bytes per second
		j.finished = true          // spawn starts nobody; only the wanted number changes
		j.target.Store(2)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { j.control(ctx); close(done) }()
		<-stepped // the controller has taken its baseline
		for i, w := range windows {
			j.fetched.Add(w.bytes)
			j.failures.Add(w.fails)
			ticks <- time.Time{}
			if got := <-stepped; got != want[i] {
				t.Fatalf("after window %d: %d workers wanted, want %d", i+1, got, want[i])
			}
		}
		cancel()
		<-done
	}
	type win = struct {
		bytes int64
		fails int64
	}
	t.Run("speed does not rise: back to the start number", func(t *testing.T) {
		run(t, []win{{10 << 20, 0}, {10<<20 + 1<<20, 0}, {10 << 20, 0}}, []int{8, 2, 2})
	})
	t.Run("speed rises: stays at the most", func(t *testing.T) {
		run(t, []win{{10 << 20, 0}, {20 << 20, 0}, {20 << 20, 0}}, []int{8, 8, 8})
	})
	t.Run("a failure in the second window: back", func(t *testing.T) {
		run(t, []win{{10 << 20, 0}, {20 << 20, 1}}, []int{8, 2})
	})
	t.Run("many failures later halve it down to the floor", func(t *testing.T) {
		run(t, []win{{10 << 20, 0}, {20 << 20, 0}, {20 << 20, int64(segFailBackoff)}, {20 << 20, int64(segFailBackoff)}, {20 << 20, int64(segFailBackoff)}},
			[]int{8, 8, 4, 4, 4})
	})
}

// A connection that breaks half way: the segment is asked for again from
// where it stopped, the others go on, and the file comes out right.
func TestSegDownloadSegmentRetry(t *testing.T) {
	body := segBody(1 << 20)
	s := &limitedServer{body: body, perConn: 4 << 20, hook: func(n int, r *http.Request) (int, int) {
		if n%5 == 3 {
			return 0, 20 << 10 // the connection breaks after 20 KB
		}
		return 0, -1
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	prm := testSegParams(4, 4)
	r := newSegRun(t, body, prm)
	if err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(r.dest)
	if !bytes.Equal(got, body) {
		t.Fatal("file differs")
	}
	if r.st.resumes.Load() == 0 {
		t.Fatal("no retry counted")
	}
	// a retry asks from where the broken connection stopped, not from the segment start
	partial := false
	for _, rg := range s.rangeHeaders() {
		v := strings.TrimPrefix(rg, "bytes=")
		a, _, _ := strings.Cut(v, "-")
		n, _ := strconv.Atoi(a)
		if n%(64<<10) != 0 {
			partial = true
		}
	}
	if !partial {
		t.Fatalf("no request started inside a segment: %v", s.rangeHeaders())
	}
}

// A segment that keeps failing ends the download, and the bytes already in
// stay in the cache; the next download goes on from them.
func TestSegDownloadResumesFromCache(t *testing.T) {
	body := segBody(2 << 20)
	var failing atomic.Bool
	failing.Store(true)
	s := &limitedServer{body: body, perConn: 8 << 20, hook: func(n int, r *http.Request) (int, int) {
		// segment 20 (bytes 1310720-1376255) always fails while "failing"
		if failing.Load() && strings.HasPrefix(r.Header.Get("Range"), "bytes=1310720-") {
			return http.StatusInternalServerError, -1
		}
		return 0, -1
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	prm := testSegParams(4, 4)
	r := newSegRun(t, body, prm)
	err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm)
	if err == nil || !strings.Contains(err.Error(), "第 21 段") {
		t.Fatalf("want the failing segment named, got %v", err)
	}
	if files := r.cacheFiles(t); len(files) != 2 {
		t.Fatalf("cache %v, want data file and record", files)
	}
	servedBefore := s.served.Load()
	failing.Store(false)
	r.st = &dlStatus{}
	if err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm); err != nil {
		t.Fatal(err)
	}
	again := s.served.Load() - servedBefore
	if again >= int64(len(body)) || again <= 0 {
		t.Fatalf("second download fetched %d bytes of %d: it should only fetch what was missing", again, len(body))
	}
	got, _ := os.ReadFile(r.dest)
	if !bytes.Equal(got, body) {
		t.Fatal("file differs")
	}
	if files := r.cacheFiles(t); len(files) != 0 {
		t.Fatalf("cache left behind: %v", files)
	}
}

// Cancelling (the server stops) keeps the cache too.
func TestSegDownloadCancelKeepsCache(t *testing.T) {
	body := segBody(2 << 20)
	s := &limitedServer{body: body, perConn: 256 << 10}
	srv := httptest.NewServer(s)
	defer srv.Close()
	prm := testSegParams(4, 4)
	r := newSegRun(t, body, prm)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for r.st.done.Load() < 400<<10 {
			time.Sleep(5 * time.Millisecond)
		}
		cancel()
	}()
	err := r.download(ctx, dlSource{URL: srv.URL + "/f"}, prm)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancelled, got %v", err)
	}
	rec, ok := r.cache.load()
	if !ok {
		t.Fatal("no record kept")
	}
	var have int64
	for _, g := range rec.Got {
		have += g
	}
	if have < 300<<10 {
		t.Fatalf("record says %d bytes", have)
	}
	if _, err := os.Stat(r.dest); err == nil {
		t.Fatal("a half file must not be at the destination")
	}
}

// The signed address expires while many workers are downloading: exactly one
// new address is asked for, and the others use it.
func TestSegDownloadReissuesOnce(t *testing.T) {
	body := segBody(1 << 20)
	var expired atomic.Bool
	s := &limitedServer{body: body, perConn: 2 << 20, hook: func(n int, r *http.Request) (int, int) {
		if r.URL.Query().Get("sig") == "1" && n > 6 {
			expired.Store(true)
		}
		if r.URL.Query().Get("sig") == "1" && expired.Load() {
			return http.StatusForbidden, -1
		}
		return 0, -1
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	var reissued atomic.Int32
	src := dlSource{URL: srv.URL + "/f?sig=1", Reissue: func(context.Context) (string, time.Time, error) {
		reissued.Add(1)
		return srv.URL + "/f?sig=2", time.Time{}, nil
	}}
	prm := testSegParams(6, 6)
	r := newSegRun(t, body, prm)
	if err := r.download(context.Background(), src, prm); err != nil {
		t.Fatal(err)
	}
	if reissued.Load() != 1 {
		t.Fatalf("asked the gateway %d times, want once", reissued.Load())
	}
	got, _ := os.ReadFile(r.dest)
	if !bytes.Equal(got, body) {
		t.Fatal("file differs")
	}
}

// An address that is about to expire is replaced before a request is made
// with it.
func TestSegDownloadReissuesBeforeExpiry(t *testing.T) {
	body := segBody(512 << 10)
	s := &limitedServer{body: body}
	srv := httptest.NewServer(s)
	defer srv.Close()
	var reissued atomic.Int32
	src := dlSource{URL: srv.URL + "/f?sig=old", ExpiresAt: time.Now().Add(5 * time.Second),
		Reissue: func(context.Context) (string, time.Time, error) {
			reissued.Add(1)
			return srv.URL + "/f?sig=new", time.Now().Add(15 * time.Minute), nil
		}}
	prm := testSegParams(2, 2)
	r := newSegRun(t, body, prm)
	if err := r.download(context.Background(), src, prm); err != nil {
		t.Fatal(err)
	}
	if reissued.Load() != 1 {
		t.Fatalf("reissued %d times", reissued.Load())
	}
}

// The address never appears in an error.
func TestSegDownloadErrorsHideTheAddress(t *testing.T) {
	body := segBody(512 << 10)
	srv := httptest.NewServer(&limitedServer{body: body})
	url := srv.URL + "/f?sig=very-secret-signature"
	srv.Close() // connection refused
	prm := testSegParams(2, 2)
	r := newSegRun(t, body, prm)
	old := downloadBackoff
	downloadBackoff = func(int) time.Duration { return time.Millisecond }
	defer func() { downloadBackoff = old }()
	err := r.download(context.Background(), dlSource{URL: url}, prm)
	if err == nil {
		t.Fatal("expected a failure")
	}
	if strings.Contains(err.Error(), "very-secret-signature") {
		t.Fatalf("the address is in the error: %v", err)
	}
}

// The file does not match the catalog's checksum: CAT-INST-13, nothing is
// left in the cache, nothing at the destination.
func TestSegDownloadWrongContent(t *testing.T) {
	good := segBody(512 << 10)
	bad := append([]byte(nil), good...)
	bad[300000] ^= 0xff
	srv := httptest.NewServer(&limitedServer{body: bad})
	defer srv.Close()
	prm := testSegParams(4, 4)
	r := newSegRun(t, good, prm)
	err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm)
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeImageDigest {
		t.Fatalf("want CAT-INST-13, got %v", err)
	}
	if files := r.cacheFiles(t); len(files) != 0 {
		t.Fatalf("cache left behind: %v", files)
	}
	if _, err := os.Stat(r.dest); err == nil {
		t.Fatal("a wrong file must not be at the destination")
	}
}

// A size the server announces that differs from the catalog's is refused.
func TestSegDownloadWrongSize(t *testing.T) {
	good := segBody(512 << 10)
	srv := httptest.NewServer(&limitedServer{body: segBody(600 << 10)})
	defer srv.Close()
	prm := testSegParams(2, 2)
	r := newSegRun(t, good, prm)
	err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm)
	var f *Fault
	if !errors.As(err, &f) || f.Code != CodeImageDigest {
		t.Fatalf("want CAT-INST-13, got %v", err)
	}
}

// A cache whose data does not match (a crash between the data and the
// record, or a bad disk): the file is fetched again from nothing, once, and
// then right.
func TestSegDownloadBadCacheStartsOver(t *testing.T) {
	body := segBody(512 << 10)
	srv := httptest.NewServer(&limitedServer{body: body})
	defer srv.Close()
	prm := testSegParams(4, 4)
	r := newSegRun(t, body, prm)
	// seed: the record says the first 3 segments are in, but the data is zeros
	f, got, _, err := r.cache.open()
	if err != nil {
		t.Fatal(err)
	}
	got[0], got[1], got[2] = prm.SegSize, prm.SegSize, prm.SegSize
	_ = f.Sync()
	f.Close()
	if err := r.cache.save(got); err != nil {
		t.Fatal(err)
	}
	if err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(r.dest)
	if !bytes.Equal(data, body) {
		t.Fatal("file differs")
	}
}

// A server that ignores Range gives the whole file for a Range request:
// errNoRange, and nothing is left in the cache.
func TestSegDownloadServerIgnoresRange(t *testing.T) {
	body := segBody(512 << 10)
	srv := httptest.NewServer(&limitedServer{body: body, ignore: true})
	defer srv.Close()
	prm := testSegParams(4, 4)
	r := newSegRun(t, body, prm)
	err := r.download(context.Background(), dlSource{URL: srv.URL + "/f"}, prm)
	if !errors.Is(err, errNoRange) {
		t.Fatalf("want errNoRange, got %v", err)
	}
	if files := r.cacheFiles(t); len(files) != 0 {
		t.Fatalf("cache left behind: %v", files)
	}
}

// A record for another size or segment size is not trusted.
func TestDLCacheIgnoresOtherRecord(t *testing.T) {
	dir := t.TempDir()
	a := newDLCacheEntry(dir, strings.Repeat("a", 64), 1<<20, 64<<10)
	f, got, _, err := a.open()
	if err != nil {
		t.Fatal(err)
	}
	got[0] = 64 << 10
	f.Close()
	if err := a.save(got); err != nil {
		t.Fatal(err)
	}
	same := newDLCacheEntry(dir, strings.Repeat("a", 64), 1<<20, 64<<10)
	f, got2, resumed, err := same.open()
	if err != nil || resumed != 64<<10 || got2[0] != 64<<10 {
		t.Fatalf("resumed %d err %v", resumed, err)
	}
	f.Close()
	other := newDLCacheEntry(dir, strings.Repeat("a", 64), 1<<20, 128<<10)
	f, _, resumed, err = other.open()
	if err != nil || resumed != 0 {
		t.Fatalf("a record with another segment size was used: resumed %d err %v", resumed, err)
	}
	f.Close()
}

// Unfinished entries nobody touched for days are removed.
func TestPruneDLCache(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.part")
	fresh := filepath.Join(dir, "fresh.part")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	when := time.Now().Add(-dlCacheMaxAge - time.Hour)
	_ = os.Chtimes(old, when, when)
	pruneDLCache(dir, time.Now())
	if _, err := os.Stat(old); err == nil {
		t.Fatal("old entry kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("fresh entry removed")
	}
}

// Two downloads of the same file in one process do not use one cache entry at
// once: the second waits.
func TestSegDownloadSameFileTakesTurns(t *testing.T) {
	body := segBody(512 << 10)
	srv := httptest.NewServer(&limitedServer{body: body, perConn: 1 << 20})
	defer srv.Close()
	prm := testSegParams(2, 2)
	dir := t.TempDir()
	want := sha256hex(body)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache := newDLCacheEntry(filepath.Join(dir, "dl-cache"), want, int64(len(body)), prm.SegSize)
			errs[i] = segDownload(context.Background(), http.DefaultClient, dlSource{URL: srv.URL + "/f"}, cache,
				filepath.Join(dir, fmt.Sprintf("out%d.tar", i)), want, &dlStatus{}, prm)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("download %d: %v", i, err)
		}
		got, _ := os.ReadFile(filepath.Join(dir, fmt.Sprintf("out%d.tar", i)))
		if !bytes.Equal(got, body) {
			t.Fatalf("file %d differs", i)
		}
	}
}

// The parameters come from the environment, with the defaults of the table.
func TestSegParamsFromEnv(t *testing.T) {
	p := segParamsFromEnv()
	if !p.Enabled || p.StartConns != 16 || p.MaxConns != 32 || p.SegSize != 4<<20 || p.Window != 5*time.Second {
		t.Fatalf("defaults: %+v", p)
	}
	t.Setenv("ACORNFOX_DL_START_CONNS", "8")
	t.Setenv("ACORNFOX_DL_MAX_CONNS", "4") // below start: raised to start
	t.Setenv("ACORNFOX_DL_SEGMENT_MB", "2")
	t.Setenv("ACORNFOX_DL_WINDOW_SECONDS", "3")
	p = segParamsFromEnv()
	if p.StartConns != 8 || p.MaxConns != 8 || p.SegSize != 2<<20 || p.Window != 3*time.Second {
		t.Fatalf("from env: %+v", p)
	}
	t.Setenv("ACORNFOX_DL_SEGMENTED", "0")
	if segParamsFromEnv().Enabled {
		t.Fatal("ACORNFOX_DL_SEGMENTED=0 must switch it off")
	}
}
