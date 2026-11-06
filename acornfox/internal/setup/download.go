package setup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/gatewayclient"
)

// Artifact is one file the built-in runtime needs. Its checksum and size are
// pinned in manifest_*.go; the file is fetched from the first source that
// works (see Sources) and kept under <data>/runtime/downloads/.
type Artifact struct {
	ID        string   // item ID shown in the setup ("colima", "vm-image")
	Title     string   // "Linux 虚拟机镜像"
	File      string   // file name on disk and on our own source
	SHA256    string   // hex
	Size      int64    // bytes, for progress before the server answers
	GatewayID string   // file ID on the download gateway ("" => not served there)
	Mirrors   []string // full URLs inside mainland China (tried first there)
	Upstream  []string // the original URLs (tried first elsewhere)
}

// GatewayIssuer hands out a short-lived download address for one file
// (*gatewayclient.Client does, through the data directory's machine key).
type GatewayIssuer interface {
	Issue(ctx context.Context, fileID, purpose string) (gatewayclient.Issued, error)
}

// GatewaySourceName is what the setup shows for the gateway source. The
// storage behind it (a cloud drive, object storage) is never shown: its
// addresses are signed and short-lived.
const GatewaySourceName = "AcornFox 下载网关"

// Sources decides the order to try an artifact's sources:
//
//  1. Base (our own download source or --mirror): Base + "/runtime/" + File;
//  2. the download gateway, when Gateway is set and the artifact has a
//     GatewayID (the address is asked for only when its turn comes);
//  3. China: the mirrors, then upstream; elsewhere upstream, then mirrors.
//
// With Gateway nil the order is exactly the one before the gateway existed.
type Sources struct {
	Base    string
	China   bool
	Gateway GatewayIssuer
}

// source is one entry of the list: a fixed address, or the gateway.
type source struct {
	url     string
	gateway bool
}

func (s source) name() string {
	if s.gateway {
		return GatewaySourceName
	}
	return hostOf(s.url)
}

// list is the full order, gateway included.
func (s Sources) list(a Artifact) []source {
	var out []source
	if s.Base != "" {
		out = append(out, source{url: strings.TrimRight(s.Base, "/") + "/runtime/" + a.File})
	}
	if s.Gateway != nil && a.GatewayID != "" {
		out = append(out, source{gateway: true})
	}
	first, second := a.Upstream, a.Mirrors
	if s.China {
		first, second = a.Mirrors, a.Upstream
	}
	for _, u := range first {
		out = append(out, source{url: u})
	}
	for _, u := range second {
		out = append(out, source{url: u})
	}
	return out
}

// URLs lists the fixed addresses in the order they are tried (the gateway,
// which has no fixed address, is left out).
func (s Sources) URLs(a Artifact) []string {
	var out []string
	for _, src := range s.list(a) {
		if !src.gateway {
			out = append(out, src.url)
		}
	}
	return out
}

// Downloader fetches artifacts with resume, stall detection and checksum
// verification.
type Downloader struct {
	Dir     string // where finished and partial files live
	Sources Sources
	Client  *http.Client  // nil => a client without an overall timeout
	Stall   time.Duration // no bytes for this long => next source (default 45s)
}

const defaultStall = 45 * time.Second

func (d *Downloader) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second}}
}

// Fetch returns the path of the verified file, downloading it if needed.
// Progress goes to report (one Item, updated about once a second). A partial
// download is kept as <file>.part and continued next time (HTTP Range).
func (d *Downloader) Fetch(ctx context.Context, a Artifact, report func(Item)) (string, error) {
	if err := os.MkdirAll(d.Dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(d.Dir, a.File)
	it := Item{ID: a.ID, Title: a.Title, State: StateRunning, TotalBytes: a.Size}
	if ok, _ := fileHasSum(dst, a.SHA256); ok {
		it.State, it.DoneBytes, it.Detail = StateDone, a.Size, "已校验"
		report(it)
		return dst, nil
	}
	part := dst + ".part"
	srcs := d.Sources.list(a)
	if len(srcs) == 0 {
		return "", Faultf("SETUP-NET-01", nil, "%s 没有可用的下载地址", a.Title)
	}
	var lastErr error
	stalled, badSum := 0, 0
	for _, src := range srcs {
		it.Source = src.name()
		var err error
		if src.gateway {
			err = d.fetchGateway(ctx, a, part, &it, report)
		} else {
			err = d.fetchOne(ctx, src.url, part, &it, report)
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err != nil {
			lastErr = err
			if errors.Is(err, errStalled) {
				stalled++
			}
			if errors.Is(err, errGatewaySum) {
				badSum++
			}
			report(Item{ID: it.ID, Title: it.Title, State: StateRunning, Source: it.Source, DoneBytes: it.DoneBytes, TotalBytes: it.TotalBytes, Detail: "换下一个下载源：" + err.Error()})
			continue
		}
		ok, err := fileHasSum(part, a.SHA256)
		if err != nil {
			lastErr = err
			continue
		}
		if !ok {
			// A wrong file must not be continued from: start over elsewhere.
			_ = os.Remove(part)
			it.DoneBytes = 0
			badSum++
			lastErr = fmt.Errorf("%s 上的文件校验不通过", it.Source)
			continue
		}
		if err := os.Rename(part, dst); err != nil {
			return "", err
		}
		it.State, it.DoneBytes, it.SpeedBps, it.ETASeconds, it.Detail = StateDone, it.TotalBytes, 0, 0, "已校验"
		report(it)
		return dst, nil
	}
	kept := int64(0)
	if fi, err := os.Stat(part); err == nil {
		kept = fi.Size()
	}
	code := "SETUP-NET-01"
	switch {
	case badSum > 0 && badSum+stalled == len(srcs):
		code = "SETUP-SUM-01"
	case stalled > 0:
		code = "SETUP-NET-02"
	}
	return "", &Fault{Code: code, Message: a.Title + " 下载失败，" + strconv.Itoa(len(srcs)) + " 个下载源都试过了", KeptBytes: kept, Err: lastErr}
}

// maxReissue is how often an address that expired mid-download (401, 403,
// 410) is asked for again.
const maxReissue = 2

var errGatewaySum = errors.New("下载网关登记的校验值和本机写死的不一致")

// fetchGateway asks the gateway for an address when this source's turn
// comes and downloads from it. Any gateway error only fails this source.
func (d *Downloader) fetchGateway(ctx context.Context, a Artifact, part string, it *Item, report func(Item)) error {
	for reissued := 0; ; reissued++ {
		iss, err := d.Sources.Gateway.Issue(ctx, a.GatewayID, "runtime")
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("下载网关没有给出地址：%w", err)
		}
		if iss.SHA256 != "" && !strings.EqualFold(iss.SHA256, a.SHA256) {
			return errGatewaySum
		}
		err = d.fetchOne(ctx, iss.URL, part, it, report)
		var se *httpStatusError
		if errors.As(err, &se) && (se.code == http.StatusUnauthorized || se.code == http.StatusForbidden || se.code == http.StatusGone) && reissued < maxReissue {
			report(Item{ID: it.ID, Title: it.Title, State: StateRunning, Source: it.Source, DoneBytes: it.DoneBytes, TotalBytes: it.TotalBytes, Detail: "下载地址过期，向下载网关重新要一次"})
			continue
		}
		if err != nil {
			return redact(err, iss.URL)
		}
		return nil
	}
}

// redactedError hides the signed address and the storage host from what
// the setup shows and logs, keeping the cause for errors.Is.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

var hostLike = regexp.MustCompile(`(?i)\b[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}(:\d+)?\b`)

func redact(err error, signed string) error {
	if errors.Is(err, errStalled) {
		return err
	}
	inner := err
	var ue *url.Error
	if errors.As(err, &ue) {
		inner = ue.Err
	}
	msg := strings.ReplaceAll(inner.Error(), signed, "签发的地址")
	msg = hostLike.ReplaceAllString(msg, "网盘地址")
	return &redactedError{msg: msg, err: err}
}

// httpStatusError is an unexpected HTTP status from a download source.
type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return "HTTP " + strconv.Itoa(e.code) }

var errStalled = errors.New("45 秒没有进展")

// fetchOne downloads u into part, continuing an existing partial file.
func (d *Downloader) fetchOne(ctx context.Context, u, part string, it *Item, report func(Item)) error {
	stall := d.Stall
	if stall <= 0 {
		stall = defaultStall
	}
	var have int64
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
	}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// A watchdog cancels the request when nothing arrives for stall: no
	// connection, no response headers, or no body bytes.
	progress := make(chan struct{}, 1)
	stalledFlag := make(chan struct{})
	go func() {
		t := time.NewTimer(stall)
		defer t.Stop()
		for {
			select {
			case <-rctx.Done():
				return
			case <-progress:
				if !t.Stop() {
					<-t.C
				}
				t.Reset(stall)
			case <-t.C:
				close(stalledFlag)
				cancel()
				return
			}
		}
	}()
	stalledErr := func(err error) error {
		select {
		case <-stalledFlag:
			return errStalled
		default:
			return err
		}
	}

	req, err := http.NewRequestWithContext(rctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return stalledErr(err)
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusOK:
		have = 0 // the server ignored Range: start over
		flags |= os.O_TRUNC
	case http.StatusRequestedRangeNotSatisfiable:
		// The partial file is already complete (or wrong); let the checksum decide.
		return nil
	default:
		return &httpStatusError{code: resp.StatusCode}
	}
	if resp.ContentLength > 0 {
		it.TotalBytes = have + resp.ContentLength
	}
	f, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 64<<10)
	it.DoneBytes = have
	last, lastBytes := time.Now(), have
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			it.DoneBytes += int64(n)
			select {
			case progress <- struct{}{}:
			default:
			}
			if el := time.Since(last); el >= time.Second {
				it.SpeedBps = int64(float64(it.DoneBytes-lastBytes) / el.Seconds())
				if it.SpeedBps > 0 && it.TotalBytes > it.DoneBytes {
					it.ETASeconds = int((it.TotalBytes - it.DoneBytes) / it.SpeedBps)
				}
				it.Detail = ""
				last, lastBytes = time.Now(), it.DoneBytes
				report(*it)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return stalledErr(rerr)
		}
	}
}

func fileHasSum(path, want string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want), nil
}

func hostOf(u string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return s
}
