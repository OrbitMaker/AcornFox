package appinstall

// Segmented, multi-connection download of a big gateway file (an image
// archive, ~1 GB).
//
// Why: the gateway's cloud-drive backend limits each connection (measured
// 2026-11 from a home line: about 30 KB/s per connection, 32 connections about
// 940 KB/s, one connection of the single-stream download about 30 KB/s), so
// one connection takes an hour for a 1 GB archive and 32 take about 20
// minutes. downloadChecked (download.go) stays the single-connection path for
// small files and for servers that ignore Range.
//
// How:
//
//   - The file is cut into segments of segSize bytes (default 4 MB). Workers
//     take the next segment that is not whole yet and fetch it with a Range
//     request. Each worker keeps its connection (HTTP/2 is switched off for
//     this client: it would put all workers on one connection, which is what
//     is limited).
//   - It starts with StartConns workers (16). After one measuring window with
//     no failures it adds as many again, up to MaxConns (32); after the next
//     window it goes back to StartConns when the speed did not rise by
//     segGainMin or a request failed. Later on, many failures in a window
//     halve the workers (down to segMinConns).
//   - A failed segment is tried again by itself (segMaxRetries times in a row
//     without bringing a byte; bytes already in stay and are not fetched
//     again). A refused address (401, 403, 404, 410) or one about to expire
//     makes exactly one worker ask the gateway for a new one (dlSource.Reissue)
//     while the others wait for it; at most segMaxReissues times.
//   - The bytes go into a cache file (segcache.go) and its record is saved
//     every segPersistEvery, so a stopped download resumes after a restart.
//   - At the end the whole file is checked against the catalog's sha256 and
//     size. A file resumed from the cache that does not match is thrown away
//     and fetched once more from nothing; one that does not match after that
//     is CAT-INST-13. A server that answers a Range request with 200 does not
//     support Range: errNoRange, and the caller falls back to
//     downloadChecked.
//
// Addresses never appear in errors or logs (scrubURL).
//
// Parameters (code defaults; the environment variables are read when a
// download starts; see segParamsFromEnv):
//
//	ACORNFOX_DL_SEGMENTED      1     0 turns segmented downloads off (single connection)
//	ACORNFOX_DL_START_CONNS    16    workers at the start (also the fallback)
//	ACORNFOX_DL_MAX_CONNS      32    most workers
//	ACORNFOX_DL_SEGMENT_MB     4     segment size in MB
//	ACORNFOX_DL_WINDOW_SECONDS 5     length of one speed measuring window

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/acornfox/acornfox/internal/gatewayclient"
)

// Defaults of the segmented download (the one table: the doc comment above and
// docs/capabilities.md point here).
const (
	segDefaultStartConns = 16
	segDefaultMaxConns   = 32
	segDefaultSegSizeMB  = 4
	segDefaultWindow     = 5 * time.Second
	segMinConns          = 4
)

// Tunables that tests shorten.
var (
	segMaxRetries   = 6               // failed tries of one segment in a row (a try that brought bytes resets the count)
	segMaxReissues  = 20              // new addresses asked for during one download
	segPersistEvery = 2 * time.Second // how often the cache record is saved
	segGainMin      = 1.15            // 32 workers must be at least this much faster than 16, or it goes back to 16
	segFailBackoff  = 4               // failures in one window that halve the workers once it has settled
	segIdleTimeout  = downloadIdleTimeout
)

// segParams are the parameters of one download.
type segParams struct {
	Enabled    bool
	StartConns int
	MaxConns   int
	SegSize    int64
	Window     time.Duration
}

// segParamsFromEnv reads the parameters (see the table above).
func segParamsFromEnv() segParams {
	p := segParams{
		Enabled:    envInt("ACORNFOX_DL_SEGMENTED", 1) != 0,
		StartConns: envInt("ACORNFOX_DL_START_CONNS", segDefaultStartConns),
		MaxConns:   envInt("ACORNFOX_DL_MAX_CONNS", segDefaultMaxConns),
		SegSize:    int64(envInt("ACORNFOX_DL_SEGMENT_MB", segDefaultSegSizeMB)) << 20,
		Window:     time.Duration(envInt("ACORNFOX_DL_WINDOW_SECONDS", int(segDefaultWindow/time.Second))) * time.Second,
	}
	return p.normalized()
}

func (p segParams) normalized() segParams {
	if p.StartConns < 1 {
		p.StartConns = segDefaultStartConns
	}
	if p.MaxConns < p.StartConns {
		p.MaxConns = p.StartConns
	}
	if p.SegSize < 64<<10 {
		p.SegSize = segDefaultSegSizeMB << 20
	}
	if p.Window <= 0 {
		p.Window = segDefaultWindow
	}
	return p
}

// envInt is the integer value of an environment variable, def when it is
// missing or not a number.
func envInt(name string, def int) int {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// errNoRange: the server answered a Range request with the whole file.
var errNoRange = errors.New("下载地址不支持分段（Range 请求得到整个文件）")

// errResumedBad: a file resumed from the cache failed its checksum.
var errResumedBad = errors.New("续传的缓存文件校验不过")

// segFatal marks an error that ends the download at once.
type segFatal struct{ err error }

func (e *segFatal) Error() string { return e.err.Error() }
func (e *segFatal) Unwrap() error { return e.err }

// segDownload downloads the file src names to dest through cache and checks
// it against wantSHA and wantSize (both required). errNoRange tells the
// caller to use downloadChecked instead. On any other failure the cache is
// kept (apart from a checksum failure) so that the next try resumes.
func segDownload(ctx context.Context, hc *http.Client, src dlSource, cache *dlCacheEntry, dest, wantSHA string, st *dlStatus, prm segParams) error {
	if wantSHA == "" || cache.size <= 0 {
		return errNoRange
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	prm = prm.normalized()
	release, err := lockDLKey(ctx, cache.key)
	if err != nil {
		return err
	}
	defer release()
	pruneDLCache(cache.dir, time.Now())
	for pass := 0; ; pass++ {
		err := segOnce(ctx, hc, src, cache, dest, wantSHA, st, prm)
		if errors.Is(err, errResumedBad) && pass == 0 {
			cache.remove()
			st.done.Store(0)
			continue
		}
		if errors.Is(err, errResumedBad) {
			err = fault(CodeImageDigest, nil, "下载的文件 sha256 和目录里的不一致")
		}
		return err
	}
}

func segOnce(ctx context.Context, hc *http.Client, src dlSource, cache *dlCacheEntry, dest, wantSHA string, st *dlStatus, prm segParams) error {
	f, got, resumed, err := cache.open()
	if err != nil {
		return fault(CodeSegDL, err, "没法写下载缓存（%s）", cache.dir)
	}
	closed := false
	closeF := func() {
		if !closed {
			closed = true
			_ = f.Close()
		}
	}
	defer closeF()
	st.done.Store(resumed)
	if resumed > 0 {
		st.resumes.Add(1)
	}
	j := &segJob{
		hc: segHTTPClient(hc, prm.MaxConns), f: f, cache: cache, st: st, prm: prm,
		addr: &segAddr{url: src.URL, exp: src.ExpiresAt, reissue: src.Reissue},
		got:  make([]atomic.Int64, len(got)),
	}
	defer j.hc.CloseIdleConnections()
	for i, g := range got {
		j.got[i].Store(g)
		if g < cache.segLen(i) {
			j.pending = append(j.pending, i)
		}
	}
	runErr := j.run(ctx)
	// Whatever happened, what is in the file now is kept in the record.
	_ = f.Sync()
	_ = cache.save(j.snapshot())
	if runErr != nil {
		var ft *Fault
		if errors.Is(runErr, errNoRange) || (errors.As(runErr, &ft) && ft.Code == CodeImageDigest) {
			cache.remove() // nothing worth resuming
		}
		return runErr
	}
	closeF()
	st.phase.Store(phaseVerify)
	sum, err := fileSHA256(ctx, cache.partPath())
	if err != nil {
		return err
	}
	if sum != strings.ToLower(wantSHA) {
		if resumed > 0 {
			return errResumedBad
		}
		cache.remove()
		return fault(CodeImageDigest, nil, "下载的文件 sha256 和目录里的不一致")
	}
	if err := cache.finish(dest); err != nil {
		return fault(CodeSegDL, err, "下载好的文件没法放到下载目录")
	}
	return nil
}

// fileSHA256 is the file's sha256 in lower-case hex.
func fileSHA256(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, rerr := f.Read(buf)
		h.Write(buf[:n])
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// segHTTPClient is a client that opens one connection per request in
// flight: HTTP/2 off, enough idle connections kept for all workers.
func segHTTPClient(hc *http.Client, conns int) *http.Client {
	dl := *hc
	dl.Timeout = 0 // bounded by ctx and the idle timeout
	var tr *http.Transport
	switch t := hc.Transport.(type) {
	case nil:
		if d, ok := http.DefaultTransport.(*http.Transport); ok {
			tr = d.Clone()
		} else {
			tr = &http.Transport{Proxy: http.ProxyFromEnvironment}
		}
	case *http.Transport:
		tr = t.Clone()
	default:
		return &dl // a test double: leave it alone
	}
	tr.MaxIdleConnsPerHost = conns
	tr.MaxConnsPerHost = 0
	tr.ForceAttemptHTTP2 = false
	tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	dl.Transport = tr
	return &dl
}

// segAddr is the download address shared by the workers.
type segAddr struct {
	mu      sync.Mutex
	url     string
	exp     time.Time
	gen     int
	n       int
	reissue func(ctx context.Context) (string, time.Time, error)
}

func (a *segAddr) snapshot() (string, int, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.url, a.gen, a.exp
}

// refresh asks for a new address unless someone already did since gen was
// read. A gateway that is briefly unreachable is a retryable failure.
func (a *segAddr) refresh(ctx context.Context, gen int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.gen != gen {
		return nil
	}
	if a.reissue == nil {
		return &segFatal{errors.New("下载地址已失效，也没法重新要地址")}
	}
	if a.n >= segMaxReissues {
		return &segFatal{fmt.Errorf("下载中途换了 %d 次下载地址还没下完", a.n)}
	}
	a.n++
	u, exp, err := a.reissue(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if codeIsNet(err) {
			return &retryable{err: err}
		}
		return &segFatal{err}
	}
	a.url, a.exp, a.gen = u, exp, a.gen+1
	return nil
}

// segJob is one download in progress.
type segJob struct {
	hc    *http.Client
	f     *os.File
	cache *dlCacheEntry
	st    *dlStatus
	prm   segParams
	addr  *segAddr

	got     []atomic.Int64 // bytes of each segment in the file
	pending []int          // segments that are not whole, in order
	next    atomic.Int64   // index into pending of the next one to hand out

	fetched  atomic.Int64 // bytes fetched in this run
	failures atomic.Int64 // failed tries in this run
	target   atomic.Int32 // workers wanted now

	// Tests only: tick replaces the window timer of the controller (one
	// value per window) and onStep is called after each window with the
	// number of workers wanted (once at the start too, when the baseline is
	// taken), so a test steps the controller by events instead of the clock.
	tick   <-chan time.Time
	onStep func(target int)

	mu       sync.Mutex
	workers  int
	finished bool
	allDone  chan struct{}
	firstErr error
	cancel   context.CancelFunc
}

// snapshot is the bytes each segment has now.
func (j *segJob) snapshot() []int64 {
	out := make([]int64, len(j.got))
	for i := range j.got {
		out[i] = j.got[i].Load()
	}
	return out
}

func (j *segJob) fail(err error) {
	j.mu.Lock()
	if j.firstErr == nil {
		j.firstErr = err
		j.cancel()
	}
	j.mu.Unlock()
}

func (j *segJob) run(ctx context.Context) error {
	if len(j.pending) == 0 {
		return nil
	}
	jctx, cancel := context.WithCancel(ctx)
	defer cancel()
	j.cancel = cancel
	j.allDone = make(chan struct{})
	start := min(j.prm.StartConns, len(j.pending))
	j.target.Store(int32(start))
	for i := 0; i < start; i++ {
		j.spawn(jctx, i)
	}
	ctlDone := make(chan struct{})
	go func() {
		defer close(ctlDone)
		j.control(jctx)
	}()
	persistDone := make(chan struct{})
	go func() {
		defer close(persistDone)
		t := time.NewTicker(segPersistEvery)
		defer t.Stop()
		for {
			select {
			case <-j.allDone:
				return
			case <-t.C:
				if err := j.f.Sync(); err == nil {
					_ = j.cache.save(j.snapshot())
				}
			}
		}
	}()
	<-j.allDone
	cancel()
	<-ctlDone
	<-persistDone
	j.mu.Lock()
	err := j.firstErr
	j.mu.Unlock()
	if err != nil {
		return err
	}
	return ctx.Err()
}

// spawn starts worker number idx (not after the job finished).
func (j *segJob) spawn(ctx context.Context, idx int) {
	j.mu.Lock()
	if j.finished {
		j.mu.Unlock()
		return
	}
	j.workers++
	j.st.conns.Store(int32(j.workers))
	j.mu.Unlock()
	go func() {
		j.worker(ctx, idx)
		j.mu.Lock()
		j.workers--
		j.st.conns.Store(int32(j.workers))
		if j.workers == 0 {
			j.finished = true
			close(j.allDone)
		}
		j.mu.Unlock()
	}()
}

// worker takes segments until none is left, it is cancelled, or the number
// of workers wanted has come down below its own number.
func (j *segJob) worker(ctx context.Context, idx int) {
	for ctx.Err() == nil {
		if int32(idx) >= j.target.Load() {
			return
		}
		k := int(j.next.Add(1)) - 1
		if k >= len(j.pending) {
			return
		}
		if err := j.fetchSegment(ctx, j.pending[k]); err != nil {
			if ctx.Err() == nil {
				j.fail(err)
			}
			return
		}
	}
}

// segController decides how many workers there should be, one measuring
// window at a time (separate from the clock so it can be tested).
type segController struct {
	start, max int
	stage      int // 0: measuring at start; 1: measuring at max; 2: settled
	base       float64
}

// step takes the number of workers wanted now (target), the segments not yet
// handed out (left), the speed over the last window (bytes per second) and
// the failures in it, and returns the number of workers wanted next.
func (c *segController) step(target, left int, rate float64, fails int64) int {
	switch c.stage {
	case 0:
		c.stage = 2
		if fails == 0 && c.max > c.start && left >= 2*c.max {
			c.base = rate
			c.stage = 1
			return c.max
		}
	case 1:
		c.stage = 2
		if fails > 0 || rate < c.base*segGainMin {
			return c.start
		}
	default:
		if int(fails) >= segFailBackoff {
			if n := max(segMinConns, target/2); n < target {
				return n
			}
		}
	}
	return target
}

// control raises the number of workers from StartConns to MaxConns when
// that speeds the download up, and brings it back when it does not or
// requests fail.
func (j *segJob) control(ctx context.Context) {
	c := &segController{start: min(j.prm.StartConns, len(j.pending)), max: min(j.prm.MaxConns, len(j.pending))}
	tick := j.tick
	if tick == nil {
		t := time.NewTicker(j.prm.Window)
		defer t.Stop()
		tick = t.C
	}
	lastBytes, lastFail := j.fetched.Load(), j.failures.Load()
	if j.onStep != nil {
		j.onStep(int(j.target.Load())) // the baseline is taken: a test may now add bytes
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
		}
		b, f := j.fetched.Load(), j.failures.Load()
		rate := float64(b-lastBytes) / j.prm.Window.Seconds()
		fails := f - lastFail
		lastBytes, lastFail = b, f
		before := int(j.target.Load())
		now := c.step(before, len(j.pending)-int(j.next.Load()), rate, fails)
		if now != before {
			j.target.Store(int32(now))
			for i := before; i < now; i++ {
				j.spawn(ctx, i)
			}
		}
		if j.onStep != nil {
			j.onStep(now)
		}
	}
}

// fetchSegment brings segment i to its full length, trying again by itself.
func (j *segJob) fetchSegment(ctx context.Context, i int) error {
	segLen := j.cache.segLen(i)
	failed := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		have := j.got[i].Load()
		if have >= segLen {
			return nil
		}
		gen, err := j.try(ctx, i, have)
		if err == nil {
			continue // the loop checks the segment is whole
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var sf *segFatal
		var r *retryable
		switch {
		case errors.As(err, &sf):
			return sf.err
		case errors.As(err, &r):
		default:
			return err
		}
		j.failures.Add(1)
		if j.got[i].Load() > have {
			failed = 0
		}
		failed++
		if failed > segMaxRetries {
			return fmt.Errorf("第 %d 段（共 %d 段）连着失败 %d 次没下下来，最后一次：%w", i+1, len(j.got), failed-1, r.err)
		}
		j.st.resumes.Add(1)
		if r.reissue {
			if rerr := j.addr.refresh(ctx, gen); rerr != nil {
				var rr *retryable
				if !errors.As(rerr, &rr) {
					return unwrapFatal(rerr)
				}
				// The gateway was unreachable: wait, and the next try is
				// refused again and asks again.
			}
		}
		t := time.NewTimer(downloadBackoff(failed))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

func unwrapFatal(err error) error {
	var sf *segFatal
	if errors.As(err, &sf) {
		return sf.err
	}
	return err
}

// try makes one Range request for what segment i still lacks. gen is the
// generation of the address it used (a refusal asks for a new address only
// if nobody did since).
func (j *segJob) try(ctx context.Context, i int, have int64) (gen int, err error) {
	url, gen, exp := j.addr.snapshot()
	if !exp.IsZero() && time.Until(exp) < expirySlack {
		if err := j.addr.refresh(ctx, gen); err != nil {
			return gen, err
		}
		url, gen, _ = j.addr.snapshot()
	}
	segStart := int64(i) * j.cache.segSize
	from := segStart + have
	to := segStart + j.cache.segLen(i) - 1
	actx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	idle := time.AfterFunc(segIdleTimeout, func() { stalled.Store(true); cancel() })
	defer idle.Stop()
	req, err := http.NewRequestWithContext(actx, http.MethodGet, url, nil)
	if err != nil {
		return gen, &segFatal{errors.New("下载地址不对")}
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-"+strconv.FormatInt(to, 10))
	resp, err := j.hc.Do(req)
	if err != nil {
		if stalled.Load() {
			err = fmt.Errorf("%s 内没有应答", segIdleTimeout)
		}
		return gen, &retryable{err: scrubURL(err)}
	}
	defer resp.Body.Close()
	switch st := resp.StatusCode; {
	case st == http.StatusPartialContent:
		start, total, ok := contentRange(resp.Header.Get("Content-Range"))
		if !ok || start != from {
			return gen, &retryable{err: fmt.Errorf("分段的起点不对（Content-Range %q）", resp.Header.Get("Content-Range"))}
		}
		if total > 0 && total != j.cache.size {
			return gen, &segFatal{fault(CodeImageDigest, nil, "下载地址给的文件大小和目录里的不一致（%d，应为 %d）", total, j.cache.size)}
		}
	case st == http.StatusOK:
		return gen, &segFatal{errNoRange}
	case st == http.StatusUnauthorized || st == http.StatusForbidden || st == http.StatusNotFound || st == http.StatusGone:
		return gen, &retryable{err: fmt.Errorf("下载返回 HTTP %d（地址过期或失效）", st), reissue: true}
	case st == http.StatusRequestTimeout || st == http.StatusTooManyRequests || st >= 500:
		return gen, &retryable{err: fmt.Errorf("下载返回 HTTP %d", st)}
	default:
		return gen, &segFatal{fmt.Errorf("下载返回 HTTP %d", st)}
	}
	buf := segBufs.Get().(*[]byte)
	defer segBufs.Put(buf)
	want := to - from + 1
	var read int64
	for read < want {
		n, rerr := resp.Body.Read((*buf)[:min(int64(len(*buf)), want-read+1)])
		if n > 0 {
			idle.Reset(segIdleTimeout)
			if read+int64(n) > want {
				return gen, &segFatal{fault(CodeImageDigest, nil, "下载的分段比请求的长")}
			}
			if _, werr := j.f.WriteAt((*buf)[:n], from+read); werr != nil {
				return gen, &segFatal{fault(CodeSegDL, werr, "写下载缓存失败（磁盘满了？）")}
			}
			read += int64(n)
			j.got[i].Add(int64(n))
			j.fetched.Add(int64(n))
			j.st.done.Add(int64(n))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return gen, ctx.Err()
			}
			if stalled.Load() {
				rerr = fmt.Errorf("%s 内没有收到数据", segIdleTimeout)
			}
			return gen, &retryable{err: scrubURL(rerr), progress: read > 0}
		}
	}
	if read < want {
		return gen, &retryable{err: fmt.Errorf("连接提前结束（这一段 %d / %d 字节）", read, want), progress: read > 0}
	}
	return gen, nil
}

var segBufs = sync.Pool{New: func() any { b := make([]byte, 128<<10); return &b }}

// codeIsNet: the error is the gateway client's "unreachable".
func codeIsNet(err error) bool { return gatewayclient.CodeOf(err) == "GW-NET-01" }
