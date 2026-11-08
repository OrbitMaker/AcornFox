package appinstall

// Resumable download of a gateway file (image archive or private app
// package).
//
// The gateway hands out a signed address that is only valid for a short
// time (at most 15 minutes). A large image archive can take longer than that
// on a slow line, and a connection can break half way. downloadChecked
// therefore:
//
//   - keeps the bytes it already has and asks for the rest with a Range
//     header when it tries again;
//   - asks the gateway for a new address (W2 again, through dlSource.Reissue)
//     when the old one is refused (401, 403, 404, 410) or has expired;
//     a broken connection or a 5xx with an address that is still valid
//     retries the same address, so the machine's daily gateway quota is not
//     spent on every hiccup;
//   - starts over from byte 0 (and counts it as a retry) when the server
//     ignores Range and answers 200;
//   - gives up after downloadMaxRetries failed tries in a row (a try that
//     brought new bytes resets that count; downloadMaxAttempts caps the
//     total), waiting longer between tries;
//   - aborts a try that receives nothing for downloadIdleTimeout;
//   - checks the whole file against the catalog's sha256 and size at the end
//     and deletes it when either is wrong (no retry: a second copy of a wrong
//     file would be just as wrong).
//
// On any failure the partial file is removed. Download addresses never
// appear in the errors (scrubURL).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/acornfox/acornfox/internal/gatewayclient"
)

// Tunables of downloadChecked (variables so tests can shorten them).
var (
	downloadMaxRetries  = 5                // failed tries in a row before giving up
	downloadMaxAttempts = 30               // tries in all, however much each one brought
	downloadIdleTimeout = 60 * time.Second // a try that receives nothing this long is aborted
	// downloadBackoff is the wait before the n-th retry (n from 1).
	downloadBackoff = func(n int) time.Duration {
		d := time.Duration(1<<uint(n)) * time.Second // 2s, 4s, 8s, 16s, 32s…
		if d > 30*time.Second {
			d = 30 * time.Second
		}
		return d
	}
)

// expirySlack: an address that expires within this long is not used for a
// new try; a fresh one is asked for instead.
const expirySlack = 30 * time.Second

// maxAddressLife is longer than any address the gateway hands out (at most
// 15 minutes): a remaining validity beyond it means this machine's clock and
// the gateway's disagree by more than is known.
const maxAddressLife = 20 * time.Minute

// clockOffsetter is a gateway client that knows how far the gateway's clock
// is from this machine's (gatewayclient.Client, learned from a GW-AUTH-01).
type clockOffsetter interface {
	ClockOffset() time.Duration
}

// localExpiry turns an expiry time given by the gateway's clock into this
// machine's clock (offset is the gateway's time minus this machine's), so
// that "about to expire" is judged by how long the address has left, not by
// a clock that may be off. When the remaining validity still looks wrong
// (already gone, or longer than the gateway ever grants), the expiry is
// treated as unknown: only a refused address (401, 403, 404, 410) then makes
// the download ask for a new one, so a wrong clock cannot spend the
// machine's daily gateway quota on every retry.
func localExpiry(exp time.Time, offset time.Duration, now time.Time) time.Time {
	if exp.IsZero() {
		return exp
	}
	exp = exp.Add(-offset)
	if rem := exp.Sub(now); rem < expirySlack || rem > maxAddressLife {
		return time.Time{}
	}
	return exp
}

// dlSource is where a file is downloaded from.
type dlSource struct {
	URL string
	// ExpiresAt is when the address expires by this machine's clock (see
	// localExpiry); zero: unknown.
	ExpiresAt time.Time
	// Reissue asks the gateway for a new address of the same file. nil: no
	// new address can be had, a refused address ends the download.
	Reissue func(ctx context.Context) (url string, expires time.Time, err error)
}

// dlStatus is a download's progress, read by the progress reporter.
type dlStatus struct {
	done    atomic.Int64 // bytes of the file so far; never goes down
	resumes atomic.Int32 // retries after a broken or refused try
}

// retryable is a failed try that may be tried again.
type retryable struct {
	err      error
	reissue  bool // the address was refused: ask for a new one
	restart  bool // the bytes so far are no good: start over from 0
	progress bool // the try brought new bytes
}

func (r *retryable) Error() string { return r.err.Error() }

// downloadChecked downloads src to dest (0644 so the runner can read it)
// and checks it against wantSHA (required) and wantSize (when > 0). st may
// be nil.
func downloadChecked(ctx context.Context, hc *http.Client, src dlSource, dest, wantSHA string, wantSize int64, st *dlStatus) (err error) {
	if wantSHA == "" {
		return errors.New("没有校验值，不下载")
	}
	if st == nil {
		st = &dlStatus{}
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	dl := *hc
	dl.Timeout = 0 // large files: bounded by ctx and the idle timeout
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		if f != nil {
			_ = f.Close()
		}
		if err != nil {
			_ = os.Remove(dest)
		}
	}()
	d := &download{hc: &dl, f: f, h: sha256.New(), st: st, want: wantSize, src: src}
	if err := d.run(ctx); err != nil {
		return err
	}
	cerr := f.Close()
	f = nil
	if cerr != nil {
		return cerr
	}
	if wantSize > 0 && d.off != wantSize {
		return fault(CodeImageDigest, nil, "下载的大小和目录里的不一致（%d，应为 %d）", d.off, wantSize)
	}
	if hex.EncodeToString(d.h.Sum(nil)) != strings.ToLower(wantSHA) {
		return fault(CodeImageDigest, nil, "下载的文件 sha256 和目录里的不一致")
	}
	return os.Chmod(dest, 0o644)
}

type download struct {
	hc   *http.Client
	f    *os.File
	h    hash.Hash
	st   *dlStatus
	want int64 // expected size, 0: unknown
	src  dlSource
	off  int64 // bytes in f (and in h)
	// target is the size the server announced (or want); 0: unknown.
	target int64
}

func (d *download) run(ctx context.Context) error {
	failed, attempts := 0, 0
	needAddress := false
	for {
		attempts++
		var done bool
		var err error
		if needAddress || (!d.src.ExpiresAt.IsZero() && time.Until(d.src.ExpiresAt) < expirySlack && attempts > 1) {
			if d.src.Reissue == nil {
				return errors.New("下载地址已失效，也没法重新要地址")
			}
			u, exp, rerr := d.src.Reissue(ctx)
			switch {
			case rerr == nil:
				d.src.URL, d.src.ExpiresAt, needAddress = u, exp, false
			case gatewayclient.CodeOf(rerr) == "GW-NET-01":
				// The gateway was briefly unreachable: try again later.
				err = &retryable{err: rerr, reissue: true}
			default:
				return rerr
			}
		}
		if err == nil {
			done, err = d.try(ctx)
			if done {
				return nil
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var r *retryable
		if !errors.As(err, &r) {
			return err
		}
		if r.restart {
			if err := d.reset(); err != nil {
				return err
			}
		}
		needAddress = r.reissue
		if r.progress {
			failed = 0
		}
		failed++
		if failed > downloadMaxRetries || attempts >= downloadMaxAttempts {
			return fmt.Errorf("下载试了 %d 次都没下完，最后一次：%w", attempts, r.err)
		}
		d.st.resumes.Add(1)
		t := time.NewTimer(downloadBackoff(failed))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// reset drops the bytes so far (the progress shown does not go back).
func (d *download) reset() error {
	if err := d.f.Truncate(0); err != nil {
		return err
	}
	if _, err := d.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	d.h.Reset()
	d.off = 0
	return nil
}

// try makes one request from d.off. done reports the whole file is in.
func (d *download) try(ctx context.Context) (done bool, err error) {
	actx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	idle := time.AfterFunc(downloadIdleTimeout, func() { stalled.Store(true); cancel() })
	defer idle.Stop()
	req, err := http.NewRequestWithContext(actx, http.MethodGet, d.src.URL, nil)
	if err != nil {
		return false, errors.New("下载地址不对")
	}
	if d.off > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(d.off, 10)+"-")
	}
	resp, err := d.hc.Do(req)
	if err != nil {
		if stalled.Load() {
			err = fmt.Errorf("%s 内没有应答", downloadIdleTimeout)
		}
		return false, &retryable{err: scrubURL(err)}
	}
	defer resp.Body.Close()
	switch st := resp.StatusCode; {
	case st == http.StatusOK:
		if d.off > 0 {
			// The server ignored Range: this answer is the whole file.
			if err := d.reset(); err != nil {
				return false, err
			}
		}
		if resp.ContentLength > 0 {
			d.target = resp.ContentLength
		}
	case st == http.StatusPartialContent:
		start, total, ok := contentRange(resp.Header.Get("Content-Range"))
		if !ok || start != d.off {
			return false, &retryable{err: fmt.Errorf("续传的起点不对（Content-Range %q）", resp.Header.Get("Content-Range")), restart: true}
		}
		if total > 0 {
			d.target = total
		}
	case st == http.StatusRequestedRangeNotSatisfiable:
		if d.want > 0 && d.off == d.want {
			return true, nil
		}
		return false, &retryable{err: errors.New("下载返回 HTTP 416"), restart: true}
	case st == http.StatusUnauthorized || st == http.StatusForbidden || st == http.StatusNotFound || st == http.StatusGone:
		// The signed address expired or was withdrawn.
		return false, &retryable{err: fmt.Errorf("下载返回 HTTP %d（地址过期或失效）", st), reissue: true}
	case st == http.StatusRequestTimeout || st == http.StatusTooManyRequests || st >= 500:
		return false, &retryable{err: fmt.Errorf("下载返回 HTTP %d", st)}
	default:
		return false, fmt.Errorf("下载返回 HTTP %d", st)
	}
	if d.want > 0 {
		if d.target > 0 && d.target != d.want {
			return false, fault(CodeImageDigest, nil, "下载地址给的文件大小和目录里的不一致（%d，应为 %d）", d.target, d.want)
		}
		d.target = d.want
	}
	before := d.off
	var body io.Reader = resp.Body
	if d.target > 0 {
		body = io.LimitReader(resp.Body, d.target-d.off+1)
	}
	buf := make([]byte, 256<<10)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			idle.Reset(downloadIdleTimeout)
			if _, werr := d.f.Write(buf[:n]); werr != nil {
				return false, werr // disk full or the like: not worth retrying
			}
			d.h.Write(buf[:n])
			d.off += int64(n)
			if d.off > d.st.done.Load() {
				d.st.done.Store(d.off)
			}
			if d.target > 0 && d.off > d.target {
				return false, fault(CodeImageDigest, nil, "下载的文件比目录里写的大")
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if stalled.Load() {
				rerr = fmt.Errorf("%s 内没有收到数据", downloadIdleTimeout)
			}
			return false, &retryable{err: scrubURL(rerr), progress: d.off > before}
		}
	}
	if d.target > 0 && d.off < d.target {
		return false, &retryable{err: fmt.Errorf("连接提前结束（%d / %d 字节）", d.off, d.target), progress: d.off > before}
	}
	return true, nil
}

// contentRange parses "bytes <start>-<end>/<total>" (total may be "*").
func contentRange(v string) (start, total int64, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(v), "bytes ")
	if !found {
		return 0, 0, false
	}
	rng, tot, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, false
	}
	s, _, found := strings.Cut(rng, "-")
	if !found {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}
	if tot = strings.TrimSpace(tot); tot != "*" {
		if total, err = strconv.ParseInt(tot, 10, 64); err != nil {
			return 0, 0, false
		}
	}
	return start, total, true
}
