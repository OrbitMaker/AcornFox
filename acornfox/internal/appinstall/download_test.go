package appinstall

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
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
)

// The waits between download retries are seconds long; tests do not wait.
func init() {
	downloadBackoff = func(int) time.Duration { return time.Millisecond }
}

// rangeServer serves body with Range support; script decides per request
// (counted from 1) whether to cut the connection after cut bytes (-1: no
// cut), to answer a status instead (status > 0), or to ignore Range.
type rangeServer struct {
	mu     sync.Mutex
	body   []byte
	n      int
	ranges []string // Range header of each request ("" when none)
	sigs   []string
	script func(n int, sig string) (cut int, status int, ignoreRange bool)
}

func (s *rangeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.n++
	n := s.n
	s.ranges = append(s.ranges, r.Header.Get("Range"))
	sig := r.URL.Query().Get("sig")
	s.sigs = append(s.sigs, sig)
	s.mu.Unlock()
	cut, status, ignore := s.script(n, sig)
	if status > 0 {
		w.WriteHeader(status)
		return
	}
	start := 0
	if rg := r.Header.Get("Range"); rg != "" && !ignore {
		v := strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-")
		start, _ = strconv.Atoi(v)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(s.body)-1, len(s.body)))
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)-start))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.body)))
		w.WriteHeader(http.StatusOK)
	}
	part := s.body[start:]
	if cut >= 0 && cut < len(part) {
		_, _ = w.Write(part[:cut])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // the connection breaks half way
	}
	_, _ = w.Write(part)
}

func (s *rangeServer) requests() (int, []string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n, append([]string(nil), s.ranges...), append([]string(nil), s.sigs...)
}

func testBody() []byte {
	return bytes.Repeat([]byte("0123456789abcdef"), 4096) // 64 KiB
}

func runDownload(t *testing.T, src dlSource, want []byte, pinned []byte) (string, *dlStatus, error) {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "catalog-x.tar")
	st := &dlStatus{}
	err := downloadChecked(context.Background(), http.DefaultClient, src, dest, sha256hex(pinned), int64(len(pinned)), st)
	if err == nil {
		got, rerr := os.ReadFile(dest)
		if rerr != nil || !bytes.Equal(got, want) {
			t.Fatalf("downloaded file differs (%d bytes, %v)", len(got), rerr)
		}
	}
	return dest, st, err
}

// The connection breaks half way: the next try asks for the rest with Range
// on the same (still valid) address, and no new address is asked for.
func TestDownloadResumesAfterDisconnect(t *testing.T) {
	body := testBody()
	s := &rangeServer{body: body, script: func(n int, _ string) (int, int, bool) {
		if n == 1 {
			return 20000, 0, false
		}
		return -1, 0, false
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	reissued := 0
	src := dlSource{URL: srv.URL + "/f?sig=1", ExpiresAt: time.Now().Add(15 * time.Minute),
		Reissue: func(context.Context) (string, time.Time, error) {
			reissued++
			return srv.URL + "/f?sig=2", time.Time{}, nil
		}}
	_, st, err := runDownload(t, src, body, body)
	if err != nil {
		t.Fatal(err)
	}
	n, ranges, _ := s.requests()
	if n != 2 || ranges[0] != "" || !strings.HasPrefix(ranges[1], "bytes=") || ranges[1] == "bytes=0-" {
		t.Fatalf("requests %d ranges %q", n, ranges)
	}
	if reissued != 0 {
		t.Fatalf("a still valid address must be reused, reissued %d", reissued)
	}
	if st.done.Load() != int64(len(body)) || st.resumes.Load() != 1 {
		t.Fatalf("done %d resumes %d", st.done.Load(), st.resumes.Load())
	}
}

// The signed address expires half way (the backend answers 403): the
// gateway is asked for a new address and the download goes on from the
// bytes already here.
func TestDownloadReissuesExpiredAddress(t *testing.T) {
	body := testBody()
	s := &rangeServer{body: body, script: func(n int, sig string) (int, int, bool) {
		switch {
		case sig == "1" && n == 1:
			return 30000, 0, false
		case sig == "1":
			return 0, http.StatusForbidden, false // expired
		}
		return -1, 0, false
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	reissued := 0
	src := dlSource{URL: srv.URL + "/f?sig=1", Reissue: func(context.Context) (string, time.Time, error) {
		reissued++
		return srv.URL + "/f?sig=2", time.Now().Add(15 * time.Minute), nil
	}}
	_, st, err := runDownload(t, src, body, body)
	if err != nil {
		t.Fatal(err)
	}
	_, ranges, sigs := s.requests()
	if reissued != 1 || strings.Join(sigs, ",") != "1,1,2" {
		t.Fatalf("reissued %d sigs %v", reissued, sigs)
	}
	if ranges[2] == "" || ranges[2] == "bytes=0-" {
		t.Fatalf("the new address must resume, ranges %q", ranges)
	}
	if st.done.Load() != int64(len(body)) {
		t.Fatalf("done %d", st.done.Load())
	}
}

// An address that is about to expire is not used for a retry: a new one is
// asked for first.
func TestDownloadReissuesBeforeExpiry(t *testing.T) {
	body := testBody()
	s := &rangeServer{body: body, script: func(n int, _ string) (int, int, bool) {
		if n == 1 {
			return 1000, 0, false
		}
		return -1, 0, false
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	reissued := 0
	src := dlSource{URL: srv.URL + "/f?sig=1", ExpiresAt: time.Now().Add(5 * time.Second),
		Reissue: func(context.Context) (string, time.Time, error) {
			reissued++
			return srv.URL + "/f?sig=2", time.Now().Add(15 * time.Minute), nil
		}}
	if _, _, err := runDownload(t, src, body, body); err != nil {
		t.Fatal(err)
	}
	if _, _, sigs := s.requests(); reissued != 1 || strings.Join(sigs, ",") != "1,2" {
		t.Fatalf("reissued %d sigs %v", reissued, sigs)
	}
}

// The server ignores Range (200 instead of 206): the file is downloaded
// again from byte 0, which counts as a retry; the progress shown never goes
// back.
func TestDownloadRestartsWhenRangeIgnored(t *testing.T) {
	body := testBody()
	s := &rangeServer{body: body, script: func(n int, _ string) (int, int, bool) {
		if n == 1 {
			return 40000, 0, false
		}
		return -1, 0, true
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	_, st, err := runDownload(t, dlSource{URL: srv.URL + "/f"}, body, body)
	if err != nil {
		t.Fatal(err)
	}
	n, ranges, _ := s.requests()
	if n != 2 || ranges[1] == "" {
		t.Fatalf("requests %d ranges %q", n, ranges)
	}
	if st.done.Load() != int64(len(body)) || st.resumes.Load() != 1 {
		t.Fatalf("done %d resumes %d", st.done.Load(), st.resumes.Load())
	}
}

// The whole file does not match the catalog's checksum: it is deleted and
// the fault is CAT-INST-13; a wrong file is not downloaded again.
func TestDownloadChecksumMismatchDeleted(t *testing.T) {
	body := testBody()
	pinned := append([]byte(nil), body...)
	pinned[100] ^= 1
	s := &rangeServer{body: body, script: func(int, string) (int, int, bool) { return -1, 0, false }}
	srv := httptest.NewServer(s)
	defer srv.Close()
	dest, _, err := runDownload(t, dlSource{URL: srv.URL + "/f"}, nil, pinned)
	if f := AsFault(err); f.Code != CodeImageDigest {
		t.Fatalf("got %v", err)
	}
	if _, serr := os.Stat(dest); !os.IsNotExist(serr) {
		t.Fatal("the wrong file must be deleted")
	}
	if n, _, _ := s.requests(); n != 1 {
		t.Fatalf("requests %d", n)
	}
}

// A connection that breaks every time without bringing anything: the
// download stops after downloadMaxRetries retries and the partial file is
// removed.
func TestDownloadGivesUpAfterRetries(t *testing.T) {
	body := testBody()
	s := &rangeServer{body: body, script: func(int, string) (int, int, bool) { return 0, http.StatusBadGateway, false }}
	srv := httptest.NewServer(s)
	defer srv.Close()
	dest, st, err := runDownload(t, dlSource{URL: srv.URL + "/f"}, nil, body)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("试了 %d 次", downloadMaxRetries+1)) {
		t.Fatalf("got %v", err)
	}
	if n, _, _ := s.requests(); n != downloadMaxRetries+1 || st.resumes.Load() != int32(downloadMaxRetries) {
		t.Fatalf("requests %d resumes %d", n, st.resumes.Load())
	}
	if _, serr := os.Stat(dest); !os.IsNotExist(serr) {
		t.Fatal("the partial file must be removed")
	}
}

// A refused address without a way to get a new one ends the download; a
// gateway refusal while asking again is returned as it is.
func TestDownloadReissueErrors(t *testing.T) {
	body := testBody()
	s := &rangeServer{body: body, script: func(int, string) (int, int, bool) { return 0, http.StatusGone, false }}
	srv := httptest.NewServer(s)
	defer srv.Close()
	if _, _, err := runDownload(t, dlSource{URL: srv.URL + "/f"}, nil, body); err == nil || !strings.Contains(err.Error(), "地址已失效") {
		t.Fatalf("no reissue: %v", err)
	}
	refusal := &gatewayclient.Error{Code: "GW-CAT-05", Message: "不给"}
	src := dlSource{URL: srv.URL + "/f", Reissue: func(context.Context) (string, time.Time, error) { return "", time.Time{}, refusal }}
	if _, _, err := runDownload(t, src, nil, body); !errors.Is(err, refusal) {
		t.Fatalf("refusal: %v", err)
	}
}

// A public app: the gateway download keeps failing (retries used up), the
// step falls back to the public sources and says why.
func TestGatewayDownloadRetriesThenFallsBack(t *testing.T) {
	body := []byte("image archive bytes")
	c, run := gwSetup(t, body, body, false)
	c.gw.issued.URL = "http://127.0.0.1:1/f?sig=secret-signature" // nobody listens on port 1
	_, msg, err := run()
	if err != nil || !strings.HasPrefix(msg, "从公共源拉取（下载网关") || !strings.Contains(msg, "试了") {
		t.Fatalf("%s %v", msg, err)
	}
	if strings.Contains(msg, "secret-signature") || len(c.v.w.pulled) != 1 || c.uploadsLeft(t) != 0 {
		t.Fatalf("msg %q pulled %v left %d", msg, c.v.w.pulled, c.uploadsLeft(t))
	}
}

// A private app's image: the download keeps failing; the step stops with
// CAT-INST-26 and never asks a public registry.
func TestPrivateImageDownloadFailsNoFallback(t *testing.T) {
	v, g, run := privateImageRun(t)
	g.mu.Lock()
	g.dlFail = http.StatusBadGateway
	g.mu.Unlock()
	_, _, err := run()
	f, ok := err.(*Fault)
	if !ok || f.Code != CodePrivateDenied || !strings.Contains(f.Error(), "试了") {
		t.Fatalf("got %v", err)
	}
	if len(v.w.pulled) != 0 || len(v.w.loads) != 0 {
		t.Fatalf("no public fallback: pulled=%v loads=%d", v.w.pulled, len(v.w.loads))
	}
}

// A private app's image with the download gateway switched off on this
// server: CAT-INST-26 at once, nothing asked of a public registry.
func TestPrivateImageGatewayOffNoFallback(t *testing.T) {
	v, _, run := privateImageRun(t)
	v.e.Catalog().cfg.Gateway = nil
	_, _, err := run()
	if f, ok := err.(*Fault); !ok || f.Code != CodePrivateDenied {
		t.Fatalf("got %v", err)
	}
	if len(v.w.pulled) != 0 || len(v.w.loads) != 0 {
		t.Fatalf("no public fallback: pulled=%v loads=%d", v.w.pulled, len(v.w.loads))
	}
}

// A private app's image with the gateway unreachable (GW-NET-01): CAT-INST-26
// carrying the gateway code, no public fallback.
func TestPrivateImageGatewayUnreachableNoFallback(t *testing.T) {
	v, _, run := privateImageRun(t)
	gc, ok := v.e.Catalog().cfg.Gateway.(*gatewayclient.Client)
	if !ok {
		t.Fatalf("gateway %T", v.e.Catalog().cfg.Gateway)
	}
	gc.BaseURL = "http://127.0.0.1:1"
	_, _, err := run()
	f, ok := err.(*Fault)
	if !ok || f.Code != CodePrivateDenied || f.GatewayCode != "GW-NET-01" {
		t.Fatalf("got %v", err)
	}
	if len(v.w.pulled) != 0 {
		t.Fatalf("no public fallback: pulled=%v", v.w.pulled)
	}
}

// The gateway's expiry is judged by how long the address has left: the
// learned clock offset turns it into this machine's clock, and an expiry that
// still looks impossible is treated as unknown (only a refusal then asks for
// a new address).
func TestLocalExpiry(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name   string
		exp    time.Time
		offset time.Duration
		want   time.Time
	}{
		{"unknown stays unknown", time.Time{}, 0, time.Time{}},
		{"clocks agree", now.Add(15 * time.Minute), 0, now.Add(15 * time.Minute)},
		// This machine is 20 minutes fast: the gateway's "15 minutes from
		// now" reads as 5 minutes ago here, the offset puts it right.
		{"fast clock corrected", now.Add(-5 * time.Minute), -20 * time.Minute, now.Add(15 * time.Minute)},
		{"slow clock corrected", now.Add(35 * time.Minute), 20 * time.Minute, now.Add(15 * time.Minute)},
		{"already gone without offset: unknown", now.Add(-5 * time.Minute), 0, time.Time{}},
		{"longer than ever granted: unknown", now.Add(2 * time.Hour), 0, time.Time{}},
	} {
		if got := localExpiry(c.exp, c.offset, now); !got.Equal(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// offsetGateway is fakeGateway with a learned clock offset, like a
// gatewayclient.Client after a GW-AUTH-01 retry.
type offsetGateway struct {
	*fakeGateway
	offset time.Duration
}

func (g offsetGateway) ClockOffset() time.Duration { return g.offset }

// This machine's clock is 20 minutes fast (the gateway client corrected its
// signatures): the address the gateway hands out looks expired by the local
// clock, yet a broken connection is resumed on the same address instead of
// asking the gateway again on every retry (each ask counts against the
// machine's daily quota).
func TestGatewayResumeWithFastClockKeepsAddress(t *testing.T) {
	body := testBody()
	s := &rangeServer{body: body, script: func(n int, _ string) (int, int, bool) {
		if n <= 2 {
			return 10000, 0, false
		}
		return -1, 0, false
	}}
	srv := httptest.NewServer(s)
	defer srv.Close()
	c, run := gwSetup(t, body, body, false)
	c.gw.issued.URL = srv.URL + "/f?sig=1"
	// The gateway's clock is 20 minutes behind: its expiry is 5 minutes in
	// this machine's past.
	c.gw.issued.ExpiresAt = time.Now().Add(-5 * time.Minute)
	c.v.e.cfg.Gateway = offsetGateway{fakeGateway: c.gw, offset: -20 * time.Minute}
	if _, msg, err := run(); err != nil || msg != "从下载网关下载" {
		t.Fatalf("%s %v", msg, err)
	}
	if c.gw.calls != 1 {
		t.Fatalf("the gateway was asked %d times, want 1", c.gw.calls)
	}
	if n, _, sigs := s.requests(); n != 3 || strings.Join(sigs, ",") != "1,1,1" {
		t.Fatalf("requests %d sigs %v", n, sigs)
	}

	// Without a learned offset the impossible expiry is ignored the same way.
	s2 := &rangeServer{body: body, script: func(n int, _ string) (int, int, bool) {
		if n == 1 {
			return 10000, 0, false
		}
		return -1, 0, false
	}}
	srv2 := httptest.NewServer(s2)
	defer srv2.Close()
	c2, run2 := gwSetup(t, body, body, false)
	c2.gw.issued.URL = srv2.URL + "/f?sig=1"
	c2.gw.issued.ExpiresAt = time.Now().Add(-5 * time.Minute)
	if _, msg, err := run2(); err != nil || msg != "从下载网关下载" || c2.gw.calls != 1 {
		t.Fatalf("%s %v calls %d", msg, err, c2.gw.calls)
	}
}

// A private catalog's manifest without an image archive: the image is not
// pulled from a public registry by repository@digest (that would tell a
// third party the private image's name); CAT-INST-26.
func TestPrivateImageWithoutArtifactNoPublicPull(t *testing.T) {
	v, _, run := privateImageRunWith(t, func(img *catalog.Image) { img.Artifact, img.ArtifactSHA256, img.ArtifactSize = "", "", 0 })
	_, _, err := run()
	f, ok := err.(*Fault)
	if !ok || f.Code != CodePrivateDenied || !strings.Contains(f.Message, "没有写镜像包") {
		t.Fatalf("got %v", err)
	}
	if len(v.w.pulled) != 0 || len(v.w.loads) != 0 {
		t.Fatalf("no public pull: pulled=%v loads=%d", v.w.pulled, len(v.w.loads))
	}
}
