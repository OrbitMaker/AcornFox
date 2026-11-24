package appinstall

// The choice between the download gateway and the image sources
// (pickpath.go): the faster goes first, the other is the back-up.

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/gatewayclient"
)

// bigGateway issues one big file served by a limitedServer.
type bigGateway struct {
	url   string
	body  []byte
	err   error
	calls atomic.Int32
}

func (g *bigGateway) Issue(ctx context.Context, fileID, purpose string) (gatewayclient.Issued, error) {
	g.calls.Add(1)
	if g.err != nil {
		return gatewayclient.Issued{}, g.err
	}
	return gatewayclient.Issued{FileID: fileID, URL: g.url, SHA256: sha256hex(g.body), Size: int64(len(g.body))}, nil
}

// pickRun is the image step of Hermes with a 3 MB archive on the gateway
// (served by s), the segmented download set to 4 connections and 1 MB
// segments, a probe window of one second and mirrors that probe as given.
func pickRun(t *testing.T, s *limitedServer, gwErr error, mirror MirrorSpeed) (*env, *bigGateway, func() (string, string, error)) {
	t.Helper()
	t.Setenv("ACORNFOX_DL_SEGMENT_MB", "1")
	t.Setenv("ACORNFOX_DL_START_CONNS", "4")
	t.Setenv("ACORNFOX_DL_MAX_CONNS", "4")
	t.Setenv("ACORNFOX_DL_PROBE_SECONDS", "1")
	oldPoll := pickPoll
	pickPoll = 50 * time.Millisecond
	t.Cleanup(func() { pickPoll = oldPoll })
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	body := s.body
	v, _, _, run := archImageRun(t, "amd64", func(img *catalog.Image) {
		img.Artifact, img.ArtifactSHA256, img.ArtifactSize = "", "", 0
		img.Artifacts = map[string]catalog.ArchArtifact{"amd64": {File: "big", SHA256: sha256hex(body), Size: int64(len(body))}}
	})
	gw := &bigGateway{url: srv.URL + "/big?sig=secret-signature", body: body, err: gwErr}
	v.e.cfg.Gateway = gw
	v.e.cfg.MirrorProbe = func(ctx context.Context, ref, arch string) MirrorSpeed { return mirror }
	return v, gw, run
}

func dlCacheFiles(v *env) []string {
	es, _ := os.ReadDir(filepath.Join(v.e.cfg.UploadDir, "dl-cache"))
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// The gateway is faster than the mirrors: it brings the image, nothing is
// pulled from the sources, the message says what was measured, and the
// downloaded file is the whole archive (as the runner is asked to load it).
func TestPickGatewayWhenFaster(t *testing.T) {
	body := segBody(3 << 20)
	v, _, run := pickRun(t, &limitedServer{body: body, perConn: 1 << 20}, nil, MirrorSpeed{Source: "mirror.example", MBps: 0.001, Usable: true})
	_, msg, err := run()
	if err != nil || !strings.HasPrefix(msg, "从下载网关下载") || !strings.Contains(msg, "测速") {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
	if len(v.w.loads) != 1 || len(v.w.pulled) != 0 {
		t.Fatalf("loads=%d pulled=%v", len(v.w.loads), v.w.pulled)
	}
	if files := dlCacheFiles(v); len(files) != 0 {
		t.Fatalf("cache left behind: %v", files)
	}
}

// The mirrors are faster: the pull from the sources is made, the gateway's
// download was stopped, and what it had is not left behind.
func TestPickMirrorWhenFaster(t *testing.T) {
	body := segBody(3 << 20)
	s := &limitedServer{body: body, perConn: 64 << 10}
	v, _, run := pickRun(t, s, nil, MirrorSpeed{Source: "mirror.example", MBps: 50, Usable: true})
	_, msg, err := run()
	if err != nil || !strings.Contains(msg, "测速") || !strings.Contains(msg, "mirror.example") {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
	if len(v.w.pulled) != 1 || len(v.w.loads) != 0 {
		t.Fatalf("pulled=%v loads=%d", v.w.pulled, len(v.w.loads))
	}
	if files := dlCacheFiles(v); len(files) != 0 {
		t.Fatalf("cache left behind: %v", files)
	}
}

// The gateway cannot give an address (the cloud drive is down): no waiting
// for the window, the mirrors do it.
func TestPickMirrorWhenGatewayCannotIssue(t *testing.T) {
	s := &limitedServer{body: segBody(3 << 20)}
	v, gw, run := pickRun(t, s, errors.New("GW-BE-01 网盘出错"), MirrorSpeed{Source: "mirror.example", MBps: 1, Usable: true})
	_, msg, err := run()
	if err != nil {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
	if gw.calls.Load() != 1 || len(v.w.pulled) != 1 || len(v.w.loads) != 0 {
		t.Fatalf("calls=%d pulled=%v loads=%d", gw.calls.Load(), v.w.pulled, len(v.w.loads))
	}
}

// The gateway fails and the probe finds no usable mirror: the pull from the
// sources goes on anyway (the runner knows its sources better), and the
// gateway's reason is in the message.
func TestPickGatewayFailsMirrorsNotUsable(t *testing.T) {
	s := &limitedServer{body: segBody(3 << 20)}
	v, _, run := pickRun(t, s, errors.New("GW-OFF-01 停发"), MirrorSpeed{})
	_, msg, err := run()
	if err != nil || len(v.w.pulled) != 1 || len(v.w.loads) != 0 {
		t.Fatalf("msg=%q err=%v pulled=%v", msg, err, v.w.pulled)
	}
	if !strings.Contains(msg, "GW-OFF-01") {
		t.Fatalf("the gateway's reason is missing from %q", msg)
	}
}

// The mirrors were chosen but the pull fails: the gateway is the back-up and
// goes on from what it had.
func TestPickGatewayIsBackUpWhenMirrorPullFails(t *testing.T) {
	body := segBody(3 << 20)
	s := &limitedServer{body: body, perConn: 256 << 10}
	v, _, run := pickRun(t, s, nil, MirrorSpeed{Source: "mirror.example", MBps: 50, Usable: true})
	v.w.failPull = true
	_, msg, err := run()
	if err != nil || !strings.HasPrefix(msg, "从下载网关下载") || !strings.Contains(msg, "镜像站没拉成") {
		t.Fatalf("msg=%q err=%v", msg, err)
	}
	if len(v.w.pulled) != 1 || len(v.w.loads) != 1 {
		t.Fatalf("pulled=%v loads=%d", v.w.pulled, len(v.w.loads))
	}
}

// Both fail: CAT-INST-12, with the gateway's reason in it.
func TestPickBothFail(t *testing.T) {
	s := &limitedServer{body: segBody(3 << 20)}
	v, _, run := pickRun(t, s, errors.New("GW-BE-01 网盘出错"), MirrorSpeed{Source: "mirror.example", MBps: 1, Usable: true})
	v.w.failPull = true
	_, _, err := run()
	f := AsFault(err)
	if f == nil || f.Code != CodePull || !strings.Contains(f.Message, "GW-BE-01") {
		t.Fatalf("err=%v", err)
	}
}

// Without a mirror probe nothing changes: the gateway first.
func TestPickOffWithoutProbe(t *testing.T) {
	body := segBody(3 << 20)
	v, _, run := pickRun(t, &limitedServer{body: body}, nil, MirrorSpeed{})
	v.e.cfg.MirrorProbe = nil
	_, msg, err := run()
	if err != nil || msg != "从下载网关下载" || len(v.w.loads) != 1 || len(v.w.pulled) != 0 {
		t.Fatalf("msg=%q err=%v pulled=%v loads=%d", msg, err, v.w.pulled, len(v.w.loads))
	}
}

// The gateway's later-half speed is read from the byte counts and their
// times (no clock: the samples carry their own times).
func TestLaterRate(t *testing.T) {
	t0 := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	at := func(ms int) time.Time { return t0.Add(time.Duration(ms) * time.Millisecond) }
	var samples []pickSample
	// 10 KB/s for the first 2 s, then 1 MB/s for 2 s; sampled every 100 ms
	var n int64
	for ms := 0; ms <= 4000; ms += 100 {
		if ms > 0 {
			if ms <= 2000 {
				n += 1 << 10
			} else {
				n += (1 << 20) / 10
			}
		}
		samples = append(samples, pickSample{at(ms), n})
	}
	got := laterRate(samples, at(0), at(4000))
	if got < 1000<<10 || got > 1100<<10 {
		t.Fatalf("later half rate %.0f, want 1 MB/s", got)
	}
	if laterRate(nil, at(0), at(4000)) != 0 || laterRate(samples, time.Time{}, at(4000)) != 0 {
		t.Fatal("no samples or no start must be 0")
	}
}

// A job that fails before the window ends is reported as failed.
func TestMeasureGatewayFailure(t *testing.T) {
	j := &gwJob{st: &dlStatus{}, done: make(chan struct{}), cancel: func() {}}
	j.err = errors.New("boom")
	close(j.done)
	if _, err := measureGateway(context.Background(), j, time.Second); err == nil {
		t.Fatal("want the error")
	}
}
