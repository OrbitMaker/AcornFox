package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const testDigest = "sha256:" + "1111111111111111111111111111111111111111111111111111111111111111"

// fakeRegistry is a registry that serves one image (an index with an amd64
// and an arm64 manifest, two layers) and sends blob requests to blob.
type fakeRegistry struct {
	status   int    // /v2/ answer other than the 401 challenge (0: challenge)
	manifest int    // manifest status (0: 200)
	blob     string // base URL blobs are redirected to ("" serve here)
	hits     []string
}

func (f *fakeRegistry) handler(srv **httptest.Server, serveBlob http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.hits = append(f.hits, r.URL.Path)
		switch {
		case r.URL.Path == "/v2/":
			if f.status != 0 {
				w.WriteHeader(f.status)
				return
			}
			w.Header().Set("Www-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test"`, (*srv).URL))
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/token":
			if r.URL.Query().Get("scope") != "repository:library/app:pull" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "t0k"})
		case strings.Contains(r.URL.Path, "/manifests/"):
			if r.Header.Get("Authorization") != "Bearer t0k" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if f.manifest != 0 {
				w.WriteHeader(f.manifest)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/manifests/"+testDigest) {
				fmt.Fprint(w, `{"manifests":[{"digest":"sha256:arm","platform":{"os":"linux","architecture":"arm64"}},{"digest":"sha256:amd","platform":{"os":"linux","architecture":"amd64"}}]}`)
				return
			}
			fmt.Fprint(w, `{"layers":[{"digest":"sha256:small","size":10},{"digest":"sha256:big","size":99999999}]}`)
		case strings.HasSuffix(r.URL.Path, "/blobs/sha256:big"):
			if f.blob != "" {
				http.Redirect(w, r, f.blob+"/cdn/big", http.StatusTemporaryRedirect)
				return
			}
			serveBlob(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// fastBlob sends the sample at once; slowBlob 8 KB every 50 ms (160 KB/s).
func fastBlob(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusPartialContent)
	buf := make([]byte, 64<<10)
	for range 64 {
		if _, err := w.Write(buf); err != nil {
			return
		}
	}
}

func slowBlob(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusPartialContent)
	buf := make([]byte, 8<<10)
	for {
		if _, err := w.Write(buf); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			return
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func newRegistry(t *testing.T, f *fakeRegistry, blob http.HandlerFunc) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(f.handler(&srv, blob))
	t.Cleanup(srv.Close)
	return srv
}

func hostOf(s *httptest.Server) string { u, _ := url.Parse(s.URL); return u.Host }

func shortProbe(t *testing.T, client *http.Client) {
	t.Helper()
	oldTotal, oldTime, oldBytes, oldReport, oldClient := probeTotal, probeSampleTime, probeSampleBytes, probeReportSampleBytes, probeClient
	probeTotal, probeSampleTime, probeSampleBytes, probeReportSampleBytes = 3*time.Second, 700*time.Millisecond, 2<<20, 2<<20
	probeClient = func() *http.Client { return client }
	t.Cleanup(func() {
		probeTotal, probeSampleTime, probeSampleBytes, probeReportSampleBytes, probeClient = oldTotal, oldTime, oldBytes, oldReport, oldClient
	})
}

// The probe tells the five kinds of source apart and RankSources orders
// them: the fast ones in table order, the slow one after them, the others
// left out.
func TestProbeAndRank(t *testing.T) {
	fast := newRegistry(t, &fakeRegistry{}, fastBlob)
	fast2 := newRegistry(t, &fakeRegistry{}, fastBlob)
	cdn := httptest.NewTLSServer(http.HandlerFunc(slowBlob))
	t.Cleanup(cdn.Close)
	slow := newRegistry(t, &fakeRegistry{blob: cdn.URL}, nil)
	denied := newRegistry(t, &fakeRegistry{status: http.StatusForbidden}, nil)
	missing := newRegistry(t, &fakeRegistry{manifest: http.StatusNotFound}, nil)
	limited := newRegistry(t, &fakeRegistry{status: http.StatusTooManyRequests}, nil)
	gone := httptest.NewTLSServer(http.NotFoundHandler())
	goneHost := hostOf(gone)
	gone.Close()
	shortProbe(t, fast.Client())

	srcs := []ImageSource{
		{Host: hostOf(slow), Layer: LayerPublic},
		{Host: hostOf(denied), Layer: LayerPublic},
		{Host: hostOf(fast), Layer: LayerPublic},
		{Host: hostOf(missing), Layer: LayerPublic},
		{Host: hostOf(limited), Layer: LayerPublic},
		{Host: goneHost, Layer: LayerPublic},
		{Host: hostOf(fast2), Layer: LayerUpstream},
	}
	t0 := time.Now()
	res := ProbeSources(context.Background(), srcs, "app@"+testDigest, "amd64")
	if el := time.Since(t0); el > probeTotal+time.Second {
		t.Fatalf("probe took %v, limit %v", el, probeTotal)
	}
	want := map[string]string{
		hostOf(slow): ProbeSlow, hostOf(denied): ProbeDenied, hostOf(fast): ProbeOK, hostOf(missing): ProbeNotFound,
		hostOf(limited): ProbeLimited, goneHost: ProbeUnreachable, hostOf(fast2): ProbeOK,
	}
	for h, st := range want {
		if res[h].Status != st {
			t.Errorf("%s: status %q (%s), want %q", h, res[h].Status, res[h].Detail, st)
		}
	}
	if r := res[hostOf(slow)]; r.FinalHost != hostOf(cdn) || r.MBps <= 0 || r.MBps >= probeFastMBps || !strings.Contains(r.Detail, "镜像层被转到 "+hostOf(cdn)) {
		t.Errorf("slow source: %+v", r)
	}
	if r := res[hostOf(denied)]; r.Detail != "拒绝访问（403）" {
		t.Errorf("denied detail: %q", r.Detail)
	}
	order, skipped := RankSources(srcs, res)
	var got []string
	for _, s := range order {
		got = append(got, s.Host)
	}
	if strings.Join(got, ",") != strings.Join([]string{hostOf(fast), hostOf(fast2), hostOf(slow)}, ",") {
		t.Errorf("order %v", got)
	}
	if len(skipped) != 4 {
		t.Errorf("skipped %v", skipped)
	}
}

func TestRankSourcesNoneUsable(t *testing.T) {
	srcs := []ImageSource{{Host: "a"}, {Host: "b"}}
	order, skipped := RankSources(srcs, map[string]ProbeResult{"a": {Status: ProbeDenied}, "b": {Status: ProbeUnreachable}})
	if len(order) != 2 || order[0].Host != "a" || skipped != nil {
		t.Fatalf("all bad: every source in table order: %v %v", order, skipped)
	}
	order, _ = RankSources(srcs, map[string]ProbeResult{"a": {Status: ProbeSlow, MBps: 0.1}, "b": {Status: ProbeSlow, MBps: 0.5}})
	if order[0].Host != "b" {
		t.Fatalf("slow ones by speed: %v", order)
	}
}

func TestSpeedWatch(t *testing.T) {
	defer func(g, w float64) { slowSwitchKBps, slowFloorKBps = g, w }(slowSwitchKBps, slowFloorKBps)
	t0 := time.Unix(1000, 0)
	feed := func(kbps float64, secs int) *speedWatch {
		w := &speedWatch{start: t0}
		for s := 1; s <= secs; s++ {
			w.add(t0.Add(time.Duration(s)*time.Second), int64(kbps*1024*float64(s)))
		}
		return w
	}
	at := func(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
	// 40 KB/s with a 2 MB/s source next: switch, but only after the grace.
	w := feed(40, 200)
	if give, _ := w.verdict(at(60), true, pullWatch{hasNext: true, nextMBps: 2}); give {
		t.Fatal("inside the grace period")
	}
	if give, kbps := w.verdict(at(200), true, pullWatch{hasNext: true, nextMBps: 2}); !give || kbps < 39 || kbps > 41 {
		t.Fatalf("40 KB/s with a faster next: give=%v kbps=%v", give, kbps)
	}
	// No faster source known: 40 KB/s keeps going.
	if give, _ := w.verdict(at(200), true, pullWatch{hasNext: true}); give {
		t.Fatal("moving at 40 KB/s without a faster source must go on")
	}
	if give, _ := w.verdict(at(200), true, pullWatch{}); give {
		t.Fatal("last source at 40 KB/s must go on")
	}
	// Below the floor: give up even without another source.
	if give, _ := feed(10, 200).verdict(at(200), true, pullWatch{}); !give {
		t.Fatal("10 KB/s is below the floor")
	}
	// Extracting (nothing pending) is never slow.
	if give, _ := feed(1, 200).verdict(at(200), false, pullWatch{}); give {
		t.Fatal("no layer pending")
	}
	// Only the last window counts: fast for 150 s, then nothing for 130 s.
	w = feed(500, 150)
	if give, _ := w.verdict(at(280), true, pullWatch{}); !give {
		t.Fatal("nothing came in the last window")
	}
	if len(w.samples) > 160 {
		t.Fatalf("samples not trimmed: %d", len(w.samples))
	}
}

// fakePulls makes a Docker whose probe and per-source pull are scripted.
func fakePulls(china bool, probes map[string]ProbeResult, pulls map[string]error) (*Docker, *[]string) {
	var tried []string
	d := &Docker{}
	d.SetRegistry(Registry{China: china, Mirrors: []string{"docker.m.daocloud.io", "docker.1panel.live", "dockerproxy.net"}})
	d.probeHook = func(ctx context.Context, srcs []ImageSource, ref, arch string) map[string]ProbeResult {
		out := map[string]ProbeResult{}
		for _, s := range srcs {
			if p, ok := probes[s.Host]; ok {
				p.Host = s.Host
				out[s.Host] = p
			}
		}
		return out
	}
	d.pullHook = func(ctx context.Context, ref string, pw pullWatch) (pullStats, error) {
		tried = append(tried, strings.SplitN(ref, "/", 2)[0])
		for host, err := range pulls {
			if strings.HasPrefix(ref, host+"/") || (host == DockerHubRegistry && !strings.Contains(strings.SplitN(ref, "/", 2)[0], ".")) {
				if errors.Is(err, errPullSlow) {
					return pullStats{Bytes: 12 << 20, Total: 319 << 20, Seconds: 200, AvgKBps: 40}, err
				}
				return pullStats{Seconds: 1}, err
			}
		}
		return pullStats{Seconds: 7}, nil
	}
	return d, &tried
}

const pinned = "example/big-app@" + testDigest

// Fault injection on the order: the first source does not answer (the probe
// skips it, the pull goes straight to the next). The last-resort source
// stays behind a usable one even when that one measured slow and it fast.
func TestPullSkipsBrokenAndSlowSources(t *testing.T) {
	d, tried := fakePulls(true, map[string]ProbeResult{
		"docker.m.daocloud.io": {Status: ProbeUnreachable, Detail: "连不上"},
		"docker.1panel.live":   {Status: ProbeSlow, MBps: 0.2, Detail: "慢（0.20 MB/s）"},
		"dockerproxy.net":      {Status: ProbeOK, MBps: 5},
		DockerHubRegistry:      {Status: ProbeUnreachable, Detail: "连不上"},
	}, nil)
	local, used, err := d.pullFallback(context.Background(), pinned, nil)
	if err != nil || used.Host != "docker.1panel.live" || local != "docker.1panel.live/example/big-app@"+testDigest {
		t.Fatalf("local=%q used=%+v err=%v", local, used, err)
	}
	if strings.Join(*tried, ",") != "docker.1panel.live" {
		t.Fatalf("tried %v", *tried)
	}
}

// The last-resort source is pulled from when the others cannot be used, and
// after them when they fail during the pull.
func TestPullLastResortSource(t *testing.T) {
	d, tried := fakePulls(true, map[string]ProbeResult{
		"docker.m.daocloud.io": {Status: ProbeUnreachable, Detail: "连不上"},
		"docker.1panel.live":   {Status: ProbeDenied, HTTPCode: 403, Detail: "拒绝访问（403）"},
		"dockerproxy.net":      {Status: ProbeSlow, MBps: 0.3, Detail: "慢（0.30 MB/s）"},
		DockerHubRegistry:      {Status: ProbeUnreachable, Detail: "连不上"},
	}, nil)
	_, used, err := d.pullFallback(context.Background(), pinned, nil)
	if err != nil || used.Host != "dockerproxy.net" || strings.Join(*tried, ",") != "dockerproxy.net" {
		t.Fatalf("only the last resort is usable: used=%+v tried=%v err=%v", used, *tried, err)
	}

	d, tried = fakePulls(true, map[string]ProbeResult{
		"docker.m.daocloud.io": {Status: ProbeSlow, MBps: 0.5, Detail: "慢（0.50 MB/s）"},
		"docker.1panel.live":   {Status: ProbeSlow, MBps: 0.9, Detail: "慢（0.90 MB/s）"},
		"dockerproxy.net":      {Status: ProbeOK, MBps: 8},
		DockerHubRegistry:      {Status: ProbeUnreachable, Detail: "连不上"},
	}, map[string]error{
		"docker.1panel.live":   errPullSlow,
		"docker.m.daocloud.io": errPullStalled,
	})
	_, used, err = d.pullFallback(context.Background(), pinned, nil)
	if err != nil || used.Host != "dockerproxy.net" {
		t.Fatalf("used=%+v err=%v", used, err)
	}
	if strings.Join(*tried, ",") != "docker.1panel.live,docker.m.daocloud.io,dockerproxy.net" {
		t.Fatalf("the others first (the slow ones by speed), the last resort after them: %v", *tried)
	}
}

func TestRankSourcesLastResort(t *testing.T) {
	srcs := []ImageSource{{Host: "a"}, {Host: "last", LastResort: true}, {Host: "b"}, {Host: "up"}}
	hosts := func(l []ImageSource) string {
		var h []string
		for _, s := range l {
			h = append(h, s.Host)
		}
		return strings.Join(h, ",")
	}
	// Fastest by the probe, still last; a source without a result too.
	order, skipped := RankSources(srcs, map[string]ProbeResult{
		"a": {Status: ProbeSlow, MBps: 0.2}, "last": {Status: ProbeOK, MBps: 9}, "b": {Status: ProbeOK, MBps: 3}, "up": {Status: ProbeSlow, MBps: 0.1}})
	if hosts(order) != "b,a,up,last" || len(skipped) != 0 {
		t.Fatalf("order %s skipped %v", hosts(order), skipped)
	}
	order, _ = RankSources(srcs, map[string]ProbeResult{"a": {Status: ProbeOK, MBps: 3}})
	if hosts(order) != "a,b,up,last" {
		t.Fatalf("no result for it: %s", hosts(order))
	}
	// Not usable: skipped like any other.
	order, skipped = RankSources(srcs, map[string]ProbeResult{
		"a": {Status: ProbeOK, MBps: 3}, "last": {Status: ProbeDenied}, "b": {Status: ProbeUnreachable}, "up": {Status: ProbeUnreachable}})
	if hosts(order) != "a" || hosts(skipped) != "last,b,up" {
		t.Fatalf("order %s skipped %s", hosts(order), hosts(skipped))
	}
	// The only usable one: used.
	order, _ = RankSources(srcs, map[string]ProbeResult{
		"a": {Status: ProbeDenied}, "last": {Status: ProbeSlow, MBps: 0.1}, "b": {Status: ProbeUnreachable}, "up": {Status: ProbeUnreachable}})
	if hosts(order) != "last" {
		t.Fatalf("only usable: %s", hosts(order))
	}
	// The table itself marks dockerproxy.net and nothing else.
	for _, s := range append(DockerHubSourceTable(), GHCRSourceTable()...) {
		if s.LastResort != (s.Host == "dockerproxy.net") {
			t.Errorf("%s: LastResort=%v", s.Host, s.LastResort)
		}
	}
}

// A pull that fails or is too slow on every source reports each one.
func TestPullErrorReport(t *testing.T) {
	d, tried := fakePulls(true, map[string]ProbeResult{
		"docker.m.daocloud.io": {Status: ProbeOK, MBps: 3, FinalHost: "production.cloudfront.docker.com"},
		"docker.1panel.live":   {Status: ProbeDenied, HTTPCode: 403, Detail: "拒绝访问（403）"},
		"dockerproxy.net":      {Status: ProbeSlow, MBps: 0.1, Detail: "慢（0.10 MB/s）"},
		DockerHubRegistry:      {Status: ProbeUnreachable, Detail: "连不上"},
	}, map[string]error{
		"docker.m.daocloud.io": errPullSlow,
		"dockerproxy.net":      errPullStalled,
	})
	_, _, err := d.pullFallback(context.Background(), pinned, nil)
	var pe *PullError
	if !errors.As(err, &pe) {
		t.Fatalf("err %T %v", err, err)
	}
	if strings.Join(*tried, ",") != "docker.m.daocloud.io,dockerproxy.net" {
		t.Fatalf("tried %v", *tried)
	}
	f := classifyPull(context.Background(), err)
	if f.Code != "pull_timeout" || len(f.Sources) != 4 || !strings.Contains(f.Hint, "--registry-mirror") {
		t.Fatalf("failure %+v", f)
	}
	for _, want := range []string{"试了 2 个源", "docker.m.daocloud.io 太慢（最近 2 分钟平均 40 KB/s，已下 12 MB/319 MB，镜像层被转到 production.cloudfront.docker.com）",
		"docker.1panel.live 拒绝访问（403），探测时已跳过", "dockerproxy.net 45 秒没有进展"} {
		if !strings.Contains(f.Message, want) {
			t.Errorf("message lacks %q:\n%s", want, f.Message)
		}
	}
	if f.Sources[0].Outcome != "skipped" || f.Sources[0].Host != "docker.1panel.live" {
		t.Errorf("first entry %+v", f.Sources[0])
	}
	if !IsPullStalled(err) {
		t.Error("IsPullStalled must see the stalled source")
	}
}

func TestPullNotFoundEverywhere(t *testing.T) {
	nf := errors.New("manifest unknown")
	d, _ := fakePulls(true, map[string]ProbeResult{
		"docker.m.daocloud.io": {Status: ProbeNotFound, Detail: "没有这个镜像（404）"},
		"docker.1panel.live":   {Status: ProbeNotFound, Detail: "没有这个镜像（404）"},
		"dockerproxy.net":      {Status: ProbeOK, MBps: 3},
		DockerHubRegistry:      {Status: ProbeOK, MBps: 3},
	}, map[string]error{"dockerproxy.net": nf, DockerHubRegistry: nf})
	_, _, err := d.pullFallback(context.Background(), pinned, nil)
	if f := classifyPull(context.Background(), err); f.Code != "image_not_found" {
		t.Fatalf("%+v", f)
	}
}

// Outside China Docker Hub goes first and its "no such image" is final.
func TestPullGlobalOrder(t *testing.T) {
	d, tried := fakePulls(false, nil, map[string]error{DockerHubRegistry: errors.New("manifest unknown")})
	_, _, err := d.pullFallback(context.Background(), pinned, nil)
	if err == nil || strings.Join(*tried, ",") != "example" {
		t.Fatalf("tried %v err %v", *tried, err)
	}
	d, tried = fakePulls(false, nil, map[string]error{DockerHubRegistry: errPullStalled})
	_, used, err := d.pullFallback(context.Background(), pinned, nil)
	if err != nil || used.Host != "docker.m.daocloud.io" || strings.Join(*tried, ",") != "example,docker.m.daocloud.io" {
		t.Fatalf("tried %v used %v err %v", *tried, used, err)
	}
}

func TestPullDeadline(t *testing.T) {
	d, _ := fakePulls(true, map[string]ProbeResult{}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	d.pullHook = func(ctx context.Context, ref string, pw pullWatch) (pullStats, error) {
		<-ctx.Done()
		return pullStats{Bytes: 180 << 20, Total: 319 << 20}, ctx.Err()
	}
	_, _, err := d.pullFallback(ctx, pinned, nil)
	f := classifyPull(ctx, err)
	if f.Code != "pull_timeout" || !strings.Contains(f.Message, "拉取镜像超时") || !strings.Contains(f.Message, "已下 180 MB/319 MB") {
		t.Fatalf("%+v", f)
	}
}

// The caller's own deadline (an app install's 60 minutes) comes before the
// image's 50: the pull stops a little before it and the failure says it was
// the caller's limit, how long the image was pulled and how far each source
// got; it does not claim the image's 50 minutes ran out.
func TestPullCallerDeadline(t *testing.T) {
	old := pullCallerMargin
	pullCallerMargin = 50 * time.Millisecond
	t.Cleanup(func() { pullCallerMargin = old })
	d, _ := fakePulls(true, map[string]ProbeResult{}, nil)
	d.pullHook = func(ctx context.Context, ref string, pw pullWatch) (pullStats, error) {
		<-ctx.Done()
		return pullStats{Bytes: 188 << 20, Total: 304 << 20, Seconds: 0.2}, ctx.Err()
	}
	caller, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	ctx, cancel2 := pullContext(caller)
	defer cancel2()
	t0 := time.Now()
	_, _, err := d.pullFallback(ctx, pinned, nil)
	if el := time.Since(t0); el > 230*time.Millisecond {
		t.Fatalf("stopped after %v, the caller gives up at 250ms", el)
	}
	if caller.Err() != nil {
		t.Fatal("the caller's context ran out before the pull stopped")
	}
	f := classifyPull(ctx, err)
	if f.Reason != ReasonCallerDeadline || f.Code != "pull_timeout" {
		t.Fatalf("%+v", f)
	}
	for _, want := range []string{"调用方给的时间先用完了", "已下 188 MB/304 MB", "试了 1 个源"} {
		if !strings.Contains(f.Message, want) {
			t.Fatalf("message lacks %q: %s", want, f.Message)
		}
	}
	if strings.Contains(f.Message, "50 分钟") || strings.Contains(f.Message, "总时限") {
		t.Fatalf("message blames the image's own limit: %s", f.Message)
	}
	last := f.Sources[len(f.Sources)-1]
	if last.Outcome != "cut_off" {
		t.Fatalf("sources %+v", f.Sources)
	}
	if !strings.Contains(SourcesSummary(f.Sources), "调用方给的时间先用完了，停下") {
		t.Fatalf("summary %s", SourcesSummary(f.Sources))
	}

	// The image's own limit: said so, with its cause.
	ctx3, cancel3 := context.WithDeadlineCause(context.Background(), time.Now().Add(20*time.Millisecond), errPullMaxTotal)
	defer cancel3()
	_, _, err = d.pullFallback(ctx3, pinned, nil)
	if f := classifyPull(ctx3, err); f.Reason != ReasonPullDeadline || !strings.Contains(f.Message, "50 分钟还没拉完") {
		t.Fatalf("%+v", f)
	}

	// The caller went away: cancelled, not a timeout.
	ctx4, cancel4 := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel4() }()
	_, _, err = d.pullFallback(ctx4, pinned, nil)
	if f := classifyPull(ctx4, err); f.Reason != ReasonCancelled || !strings.Contains(f.Message, "被取消") {
		t.Fatalf("%+v", f)
	}

	// No caller deadline, or a far one: the image's 50 minutes bound it.
	for _, c := range []context.Context{context.Background()} {
		pc, cl := pullContext(c)
		dl, _ := pc.Deadline()
		cl()
		if d := time.Until(dl); d < pullMaxTotal-time.Minute || d > pullMaxTotal {
			t.Fatalf("deadline in %v", d)
		}
	}
	far, cl := context.WithTimeout(context.Background(), 3*time.Hour)
	defer cl()
	pc, cl2 := pullContext(far)
	defer cl2()
	if dl, _ := pc.Deadline(); time.Until(dl) > pullMaxTotal {
		t.Fatalf("a far caller deadline must not lift the image's limit: %v", time.Until(dl))
	}
}

func TestProbeCache(t *testing.T) {
	calls := 0
	d := &Docker{probeHook: func(ctx context.Context, srcs []ImageSource, ref, arch string) map[string]ProbeResult {
		calls++
		out := map[string]ProbeResult{}
		for _, s := range srcs {
			out[s.Host] = ProbeResult{Host: s.Host, Status: ProbeOK}
		}
		return out
	}}
	srcs := []ImageSource{{Host: "a"}, {Host: "b"}}
	d.probe(context.Background(), srcs, "x/y@"+testDigest, "")
	d.probe(context.Background(), srcs, "x/y:2", "")
	if calls != 1 {
		t.Fatalf("same repository within the TTL: %d probes", calls)
	}
	d.forgetProbe("a", "x/y:1")
	d.probe(context.Background(), srcs, "x/y:1", "")
	if calls != 2 {
		t.Fatalf("a forgotten source is probed again: %d", calls)
	}
}

func TestDaemonAndUserMirrors(t *testing.T) {
	os.Unsetenv("ACORNFOX_REGISTRY_MIRRORS")
	p := filepath.Join(t.TempDir(), "daemon.json")
	os.WriteFile(p, []byte(`{"registry-mirrors":["https://abc.mirror.aliyuncs.com/","https://docker.m.daocloud.io"],"log-driver":"json-file"}`), 0o644)
	hosts := DaemonMirrorHosts(p)
	if strings.Join(hosts, ",") != "abc.mirror.aliyuncs.com,docker.m.daocloud.io" {
		t.Fatalf("hosts %v", hosts)
	}
	got := WithUserMirrors(nil, hosts)
	if got[0] != "abc.mirror.aliyuncs.com" || !slices.Equal(got[1:], DefaultRegistryMirrors) {
		t.Fatalf("user mirror first, then the table: %v", got)
	}
	if src := (Registry{China: true, Mirrors: got}).Sources(); src[0].Layer != LayerUser || src[1].Layer != LayerPublic || src[len(src)-1].Layer != LayerUpstream {
		t.Fatalf("layers %+v", src)
	}
	if DaemonMirrorHosts(filepath.Join(t.TempDir(), "none")) != nil {
		t.Fatal("missing file")
	}
	t.Setenv("ACORNFOX_REGISTRY_MIRRORS", "x.example")
	if got := WithUserMirrors(nil, hosts); got != nil {
		t.Fatalf("the variable wins: %v", got)
	}
}

// The table is the one list: the defaults, the Tencent order and the ghcr.io
// mirrors come from it, and every layer but self has a source.
func TestSourceTable(t *testing.T) {
	os.Unsetenv("ACORNFOX_REGISTRY_MIRRORS")
	os.Unsetenv("ACORNFOX_GHCR_MIRRORS")
	if !slices.Equal(DefaultRegistryMirrors, []string{"docker.m.daocloud.io", "docker.1panel.live", "dockerproxy.net"}) {
		t.Fatalf("defaults %v", DefaultRegistryMirrors)
	}
	if got := CloudRegistryMirrors("tencent"); got[0] != TencentRegistryMirror {
		t.Fatalf("tencent %v", got)
	}
	g := Registry{China: true}.GHCRSources()
	if g[0].Host != "ghcr.nju.edu.cn" || g[1].Host != GHCRRegistry {
		t.Fatalf("ghcr china %+v", g)
	}
	if g := (Registry{}).GHCRSources(); g[0].Host != GHCRRegistry {
		t.Fatalf("ghcr global %+v", g)
	}
	for _, s := range DockerHubSourceTable() {
		if s.Layer.Label() == string(s.Layer) || s.Name == "" {
			t.Errorf("%+v has no label or name", s)
		}
	}
}

// A pull's probe stops once the order's head is settled: the first source
// in the table answered fast, so a source that does not answer is not
// waited for (it keeps its place behind). A source before the fast one that
// is still running is waited for; doctor's full probe waits for all.
func TestProbeStopsEarly(t *testing.T) {
	fast := newRegistry(t, &fakeRegistry{}, fastBlob)
	hang := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	t.Cleanup(hang.Close)
	shortProbe(t, fast.Client())
	f, h := ImageSource{Host: hostOf(fast), Layer: LayerPublic}, ImageSource{Host: hostOf(hang), Layer: LayerUpstream}

	t0 := time.Now()
	res := probeSources(context.Background(), []ImageSource{f, h}, nil, "app@"+testDigest, "amd64", true, probeSampleBytes)
	if el := time.Since(t0); el > time.Second {
		t.Fatalf("early probe took %v", el)
	}
	if res[f.Host].Status != ProbeOK {
		t.Fatalf("fast: %+v", res[f.Host])
	}
	if _, ok := res[h.Host]; ok {
		t.Fatalf("a source cut short must have no result: %+v", res[h.Host])
	}
	order, skipped := RankSources([]ImageSource{f, h}, res)
	if len(order) != 2 || order[0].Host != f.Host || len(skipped) != 0 {
		t.Fatalf("order %v skipped %v", order, skipped)
	}

	// The hanging one first in the table: waited for to the limit.
	t0 = time.Now()
	res = probeSources(context.Background(), []ImageSource{h, f}, nil, "app@"+testDigest, "amd64", true, probeSampleBytes)
	if el := time.Since(t0); el < probeTotal-200*time.Millisecond {
		t.Fatalf("stopped after %v although the first source had not answered", el)
	}
	if res[h.Host].Status != ProbeUnreachable || res[f.Host].Status != ProbeOK {
		t.Fatalf("%+v", res)
	}

	// A known (cached) fast first source: nothing is probed.
	d := &Docker{}
	d.probeCache = map[string]cachedProbe{f.Host + "|library/app": {r: ProbeResult{Host: f.Host, Status: ProbeOK, Detail: "cached"}, at: time.Now()}}
	t0 = time.Now()
	res = d.probe(context.Background(), []ImageSource{f, h}, "app@"+testDigest, "amd64")
	if el := time.Since(t0); el > 200*time.Millisecond {
		t.Fatalf("probe with a settled head took %v", el)
	}
	if _, ok := res[h.Host]; ok || res[f.Host].Detail != "cached" {
		t.Fatalf("%+v", res)
	}

	// doctor and sources --probe: every source.
	t0 = time.Now()
	res = ProbeSources(context.Background(), []ImageSource{f, h}, "app@"+testDigest, "amd64")
	if el := time.Since(t0); el < probeTotal-200*time.Millisecond || res[h.Host].Status != ProbeUnreachable {
		t.Fatalf("full probe: %v %+v", el, res)
	}
}

// A sample that speeds up (a slow start, then fast): its speed is the later
// half's, not the average from the first byte.
func TestLaterHalfSpeed(t *testing.T) {
	t0 := time.Now()
	var marks []speedSample
	var b int64
	for i := 1; i <= 20; i++ { // 1 s at 0.5 MB/s, then 1 s at 4 MB/s
		rate := 0.5
		if i > 10 {
			rate = 4
		}
		b += int64(rate * (1 << 20) / 10)
		marks = append(marks, speedSample{t0.Add(time.Duration(i) * 100 * time.Millisecond), b})
	}
	v, ok := laterHalfMBps(marks)
	if !ok || v < 3.5 || v > 4.5 {
		t.Fatalf("later half %.2f MB/s (%v)", v, ok)
	}
	if _, ok := laterHalfMBps(marks[:3]); ok {
		t.Fatal("three reads are too few to tell")
	}
}
