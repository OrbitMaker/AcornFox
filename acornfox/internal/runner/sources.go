package runner

// The image sources. This file is the one list of where Docker Hub and
// ghcr.io images are pulled from: the runner's pull order, the Docker
// daemon's registry-mirrors that install.sh writes (`acornfox sources
// --daemon-mirrors`), `acornfox sources`, doctor and the console's settings
// page all take it from here; the docs point here instead of copying it.
//
// Sources come in layers, tried in this order in mainland China:
//
//	self      our own registry (none yet; see "Adding the self layer" below)
//	cloud     the cloud vendor's internal accelerator, only on that vendor's servers
//	public    other people's public domestic mirrors of Docker Hub / ghcr.io
//	upstream  Docker Hub / ghcr.io itself (last in China, first elsewhere)
//
// plus "user": addresses the user configured (ACORNFOX_REGISTRY_MIRRORS, or a
// registry-mirrors entry of /etc/docker/daemon.json that is not in the table,
// for example an Aliyun per-account accelerator given to install.sh with
// --registry-mirror), put in front of the table's mirrors.
//
// Before a pull in mainland China every candidate is probed once (probe.go
// style, in this file): /v2/, an anonymous token, the image's manifest by its
// digest, then the first 4 MB of its largest layer. A source that answers 403,
// 404 for the image, 429 or not at all is skipped; one that is slow (for
// example a mirror that sends the big layers to Docker's CDN abroad) goes
// after the fast ones. The probe only orders; whether a pull succeeds is
// still decided by the pull itself (pull_fallback.go).
//
// Adding the self layer (not done yet): add a line
//
//	{Host: "<our registry host>", Layer: LayerSelf, Name: "我们自己的源"}
//
// at the top of dockerHubSources (and ghcrSources when it mirrors ghcr.io).
// If that registry is the download gateway's, the runner first asks the
// gateway for a short-lived registry token signed with this machine's key
// (the same way the app center fetches image archives through the gateway)
// and hands it to Docker as the pull's RegistryAuth; the probe sends the same
// token. A private catalog's app must only be pulled from the self layer,
// matching the rule that a private app's image never goes to public sources.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/distribution/reference"
)

// SourceLayer is one layer of the image source table.
type SourceLayer string

const (
	LayerSelf     SourceLayer = "self"
	LayerCloud    SourceLayer = "cloud"
	LayerPublic   SourceLayer = "public"
	LayerUpstream SourceLayer = "upstream"
	LayerUser     SourceLayer = "user"
)

// Label is the layer's name for people.
func (l SourceLayer) Label() string {
	switch l {
	case LayerSelf:
		return "我们自己的源"
	case LayerCloud:
		return "所在云的内网加速"
	case LayerPublic:
		return "别人公开的国内副本"
	case LayerUpstream:
		return "上游自己"
	case LayerUser:
		return "你自己配的加速地址"
	}
	return string(l)
}

// ImageSource is one registry host images are pulled from.
type ImageSource struct {
	Host  string      `json:"host"`
	Layer SourceLayer `json:"layer"`
	Name  string      `json:"name"`
	// Clouds limits a cloud-layer source to servers of these vendors
	// (ACORNFOX_CLOUD in /etc/acornfox/install.env).
	Clouds []string `json:"clouds,omitempty"`
	// LastResort keeps the source behind every other usable one whatever the
	// probe measured (RankSources): it is used only when the others cannot
	// be used or were given up as too slow during the pull.
	LastResort bool `json:"last_resort,omitempty"`
}

// The Docker Hub table, in the order used in mainland China. Measured
// 2026-11 from a cloud server in mainland China and a home broadband line
// (the 232 MB largest layer of a large public app image): DaoCloud 11 MB/s on the cloud server, but from a
// network it takes for abroad it sends big layers to Docker's own CDN abroad
// (0.16 MB/s); 1Panel 3-4 MB/s on the cloud server, 403 for every request
// from the home line; dockerproxy.net 1.4-2 MB/s on the cloud server.
// docker.1ms.run (429), docker.xuanyuan.me (403), hub.rat.dev (login
// required), docker.nju.edu.cn (403) and mirror.baidubce.com (unreachable)
// did not work and are not listed. The self layer would go first.
// dockerproxy.net is the last resort: who runs it is not published, and the
// same evening it twice slowed to under 100 KB/s near the end of a large
// layer after a small sample had measured it as the fastest.
var dockerHubSources = []ImageSource{
	{Host: TencentRegistryMirror, Layer: LayerCloud, Name: "腾讯云内网加速", Clouds: []string{"tencent"}},
	{Host: "docker.m.daocloud.io", Layer: LayerPublic, Name: "DaoCloud"},
	{Host: "docker.1panel.live", Layer: LayerPublic, Name: "1Panel"},
	{Host: "dockerproxy.net", Layer: LayerPublic, Name: "dockerproxy.net", LastResort: true},
	{Host: DockerHubRegistry, Layer: LayerUpstream, Name: "Docker Hub"},
}

// The ghcr.io table. Nanjing University's mirror measured about 15 MB/s from
// Aliyun and Tencent servers.
var ghcrSources = []ImageSource{
	{Host: "ghcr.nju.edu.cn", Layer: LayerPublic, Name: "南京大学"},
	{Host: GHCRRegistry, Layer: LayerUpstream, Name: "ghcr.io"},
}

// GHCRRegistry is ghcr.io's registry host.
const GHCRRegistry = "ghcr.io"

// DockerHubSourceTable and GHCRSourceTable return copies of the tables.
func DockerHubSourceTable() []ImageSource { return slices.Clone(dockerHubSources) }
func GHCRSourceTable() []ImageSource      { return slices.Clone(ghcrSources) }

func layerHosts(t []ImageSource, layer SourceLayer) []string {
	var out []string
	for _, s := range t {
		if s.Layer == layer {
			out = append(out, s.Host)
		}
	}
	return out
}

// describe returns the table entry for host, or a user-layer entry.
func describe(host string, table []ImageSource) ImageSource {
	for _, s := range table {
		if s.Host == host {
			return s
		}
	}
	return ImageSource{Host: host, Layer: LayerUser, Name: host}
}

// Sources lists the Docker Hub sources in pull order (PullSources) with
// their layer.
func (r Registry) Sources() []ImageSource {
	var out []ImageSource
	for _, h := range r.PullSources() {
		out = append(out, describe(h, dockerHubSources))
	}
	return out
}

// GHCRSources lists the ghcr.io sources in pull order: in mainland China the
// mirrors first and ghcr.io last, elsewhere ghcr.io first.
func (r Registry) GHCRSources() []ImageSource {
	mirrors := ghcrMirrors()
	var hosts []string
	if r.China && len(mirrors) > 0 {
		hosts = append(append(hosts, mirrors...), GHCRRegistry)
	} else {
		hosts = append(append(hosts, GHCRRegistry), mirrors...)
	}
	var out []ImageSource
	for _, h := range hosts {
		out = append(out, describe(h, ghcrSources))
	}
	return out
}

// DaemonMirrorHosts reads the registry-mirrors of a Docker daemon.json and
// returns their hosts ("https://a.example/" -> "a.example"). A missing or
// unreadable file gives nil.
func DaemonMirrorHosts(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Mirrors []string `json:"registry-mirrors"`
	}
	if json.Unmarshal(b, &cfg) != nil {
		return nil
	}
	var out []string
	for _, m := range cfg.Mirrors {
		if h := mirrorHost(m); h != "" && !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

func mirrorHost(m string) string {
	m = strings.TrimSpace(m)
	if !strings.Contains(m, "://") {
		m = "https://" + m
	}
	u, err := url.Parse(m)
	if err != nil {
		return ""
	}
	return u.Host
}

// WithUserMirrors puts the daemon.json mirrors that are not in the Docker
// Hub table in front of mirrors (nil mirrors meaning the default order). It
// returns mirrors unchanged when ACORNFOX_REGISTRY_MIRRORS is set (it wins)
// or there is nothing to add.
func WithUserMirrors(mirrors, daemon []string) []string {
	if _, set := os.LookupEnv("ACORNFOX_REGISTRY_MIRRORS"); set {
		return mirrors
	}
	var extra []string
	for _, h := range daemon {
		if describe(h, dockerHubSources).Layer == LayerUser {
			extra = append(extra, h)
		}
	}
	if len(extra) == 0 {
		return mirrors
	}
	base := mirrors
	if base == nil {
		base = registryMirrors()
	}
	out := slices.Clone(extra)
	for _, h := range base {
		if !slices.Contains(out, h) {
			out = append(out, h)
		}
	}
	return out
}

// DaemonConfigPath is the Linux Docker daemon's configuration file.
const DaemonConfigPath = "/etc/docker/daemon.json"

// DaemonMirrors returns what install.sh writes into daemon.json's
// registry-mirrors (as hosts, best first): the cloud and public layers for
// cloud (user mirrors are added by install.sh itself), without the ones whose
// /v2/ answers 403 or not at all. When that leaves nothing, all of them.
func DaemonMirrors(ctx context.Context, cloud string) []string {
	hosts := CloudRegistryMirrors(cloud)
	if hosts == nil {
		hosts = registryMirrors()
	}
	pctx, cancel := context.WithTimeout(ctx, probeTotal)
	defer cancel()
	ok := make([]bool, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, _, _ := pingRegistry(pctx, probeClient(), h)
			ok[i] = st == ProbeOK
		}()
	}
	wg.Wait()
	var out []string
	for i, h := range hosts {
		if ok[i] {
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		return slices.Clone(hosts)
	}
	return out
}

// Probe statuses.
const (
	ProbeOK          = "ok"          // answered, the sample came fast (or the whole layer came within it)
	ProbeSlow        = "slow"        // answered, but the sample came slowly (or not at all)
	ProbeDenied      = "denied"      // 401 without an anonymous token, or 403
	ProbeNotFound    = "notfound"    // the image's manifest is 404 here
	ProbeLimited     = "limited"     // 429
	ProbeUnreachable = "unreachable" // no connection, a timeout, or not a registry
)

// ProbeResult is one source's probe.
type ProbeResult struct {
	Host   string  `json:"host"`
	Status string  `json:"status"`
	MBps   float64 `json:"mbps"` // after the first byte of the sample
	// FinalHost is the host the layer sample came from after redirects
	// (a mirror may hand big layers to a CDN).
	FinalHost string `json:"final_host,omitempty"`
	HTTPCode  int    `json:"http_code,omitempty"`
	// Complete: the whole largest layer came within the sample.
	Complete bool    `json:"complete,omitempty"`
	Detail   string  `json:"detail,omitempty"`
	Seconds  float64 `json:"seconds"`
}

// Usable reports whether a pull should try the source.
func (p ProbeResult) Usable() bool { return p.Status == ProbeOK || p.Status == ProbeSlow }

// Probe limits. Measured 2026-11: a 1.2 MB layer came at 0.2-0.5 MB/s from
// every source (1-2.5 s to the first byte), so the speed is taken from the
// largest layer and only after its first byte. Variables so tests can shorten
// them.
var (
	probeTotal             = 10 * time.Second // the whole probe of all sources
	probeSampleBytes int64 = 4 << 20          // a pull's probe reads at most this much of the largest layer
	probeSampleTime        = 5 * time.Second  // and at most this long after its first byte
	// probeReportSampleBytes is the sample of doctor and sources --probe
	// (ProbeSources), which report how fast each source is. Measured
	// 2026-11 on a cloud server: a source's speed keeps climbing for the
	// first seconds of a layer (DaoCloud: 4 MB at 0.4-0.8 MB/s, 16 MB at
	// 1.8-3 MB/s, 15 s at 6-7 MB/s), so 4 MB marked every public mirror
	// "slow". The speed is also taken from the later half of the sample.
	probeReportSampleBytes int64 = 16 << 20
	// probeFastMBps: at or above it a source counts as fast and keeps its
	// place in the table; below it sources are ordered by speed.
	probeFastMBps = 2.0
	probeScheme   = "https"
	probeClient   = func() *http.Client { return &http.Client{} }
)

// ProbeSources probes srcs concurrently for ref's image and returns the
// results by host. The whole probe takes at most probeTotal.
func ProbeSources(ctx context.Context, srcs []ImageSource, ref string, arch string) map[string]ProbeResult {
	return probeSources(ctx, srcs, nil, ref, arch, false, probeReportSampleBytes)
}

// probeSources probes the sources of srcs (in table order) that known has
// no result for. With early it stops as soon as the order's head is
// settled: every source before some source is done and that one came back
// ok (RankSources then puts it first whatever the rest do). The sources
// still running are left out of the result (RankSources keeps their place
// after it; a pull tries them in table order). Measured 2026-11 on a cloud
// server: Docker Hub does not answer there, and without this every pull
// waited the full probeTotal for it although the first mirror had answered
// fast within two seconds.
func probeSources(ctx context.Context, srcs []ImageSource, known map[string]ProbeResult, ref, arch string, early bool, sample int64) map[string]ProbeResult {
	path, tagOrDigest := refPath(ref)
	pctx, cancel := context.WithTimeout(ctx, probeTotal)
	defer cancel()
	out := make(map[string]ProbeResult, len(srcs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	stopped := false
	// The results so far, the known ones included (under mu).
	all := maps.Clone(known)
	if all == nil {
		all = map[string]ProbeResult{}
	}
	for _, s := range srcs {
		if _, ok := known[s.Host]; ok {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := probeOne(pctx, probeClient(), s.Host, path, tagOrDigest, arch, sample)
			mu.Lock()
			defer mu.Unlock()
			if stopped {
				return // cut short: the result is the cancellation's, not the source's
			}
			out[s.Host] = r
			all[s.Host] = r
			if early && probeHeadSettled(srcs, all) {
				stopped = true
				cancel()
			}
		}()
	}
	wg.Wait()
	return out
}

// refPath splits ref into its repository path (library/nginx) and tag or
// digest ("latest" when it has neither).
func refPath(ref string) (string, string) {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return ref, "latest"
	}
	ref2 := "latest"
	if t, ok := named.(reference.Tagged); ok {
		ref2 = t.Tag()
	}
	if d, ok := named.(reference.Digested); ok {
		ref2 = d.Digest().String()
	}
	return reference.Path(named), ref2
}

// pingRegistry asks host's /v2/ and returns a probe status, the HTTP code,
// and for a 401 the WWW-Authenticate header.
func pingRegistry(ctx context.Context, hc *http.Client, host string) (string, int, string) {
	resp, err := httpGet(ctx, hc, probeScheme+"://"+host+"/v2/", nil)
	if err != nil {
		return ProbeUnreachable, 0, ""
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return ProbeOK, 200, ""
	case http.StatusUnauthorized:
		return ProbeOK, 401, resp.Header.Get("Www-Authenticate")
	case http.StatusForbidden:
		return ProbeDenied, 403, ""
	case http.StatusTooManyRequests:
		return ProbeLimited, 429, ""
	}
	return ProbeUnreachable, resp.StatusCode, ""
}

func httpGet(ctx context.Context, hc *http.Client, u string, hdr http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	return hc.Do(req)
}

func probeOne(ctx context.Context, hc *http.Client, host, path, ref, arch string, sample int64) (res ProbeResult) {
	t0 := time.Now()
	res.Host = host
	defer func() { res.Seconds = roundTo(time.Since(t0).Seconds(), 1) }()
	st, code, auth := pingRegistry(ctx, hc, host)
	res.HTTPCode = code
	if st != ProbeOK {
		res.Status, res.Detail = st, statusDetail(st, code)
		return res
	}
	hdr := http.Header{}
	if code == http.StatusUnauthorized {
		tok, err := anonymousToken(ctx, hc, auth, path)
		if err != nil {
			res.Status, res.Detail = ProbeDenied, "要登录（匿名取不到令牌）"
			return res
		}
		hdr.Set("Authorization", "Bearer "+tok)
	}
	base := probeScheme + "://" + host + "/v2/" + path
	m, code, err := getManifest(ctx, hc, base, ref, hdr)
	if err != nil || code != http.StatusOK {
		res.HTTPCode = code
		res.Status = codeStatus(code)
		res.Detail = statusDetail(res.Status, code)
		if err != nil && code == 0 {
			res.Detail = "连不上"
		}
		return res
	}
	if len(m.Manifests) > 0 {
		d := pickPlatform(m.Manifests, arch)
		m, code, err = getManifest(ctx, hc, base, d, hdr)
		if err != nil || code != http.StatusOK {
			res.HTTPCode = code
			res.Status = codeStatus(code)
			res.Detail = statusDetail(res.Status, code)
			return res
		}
	}
	var layer string
	var size int64
	for _, l := range m.Layers {
		if l.Size > size {
			layer, size = l.Digest, l.Size
		}
	}
	if layer == "" {
		res.Status, res.Complete, res.Detail = ProbeOK, true, "镜像没有层"
		return res
	}
	sampleLayer(ctx, hc, base+"/blobs/"+layer, hdr, &res, sample)
	return res
}

// sampleLayer reads up to sample bytes of the layer at u (following
// redirects; Go's client drops Authorization when it goes to another host)
// and sets res's status, speed and final host.
func sampleLayer(ctx context.Context, hc *http.Client, u string, hdr http.Header, res *ProbeResult, sample int64) {
	h := hdr.Clone()
	h.Set("Range", fmt.Sprintf("bytes=0-%d", sample-1))
	resp, err := httpGet(ctx, hc, u, h)
	if err != nil {
		res.Status, res.Detail = ProbeSlow, "镜像层取不到数据（"+shortErr(err)+"）"
		if ctx.Err() == nil {
			res.Status, res.Detail = ProbeUnreachable, "镜像层连不上（"+shortErr(err)+"）"
		}
		return
	}
	defer resp.Body.Close()
	if resp.Request != nil && resp.Request.URL != nil {
		res.FinalHost = resp.Request.URL.Host
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		res.HTTPCode = resp.StatusCode
		res.Status = codeStatus(resp.StatusCode)
		res.Detail = "镜像层" + statusDetail(res.Status, resp.StatusCode)
		return
	}
	buf := make([]byte, 64<<10)
	var first, last time.Time
	var total, firstChunk int64
	var stop *time.Timer
	eof := false
	var marks []speedSample // (time, bytes so far) after each read
	for total < sample {
		n, err := resp.Body.Read(buf)
		now := time.Now()
		if n > 0 {
			if first.IsZero() {
				first, firstChunk = now, int64(n)
				body := resp.Body
				stop = time.AfterFunc(probeSampleTime, func() { body.Close() })
			}
			total += int64(n)
			last = now
			marks = append(marks, speedSample{now, total})
		}
		if err != nil {
			eof = errors.Is(err, io.EOF)
			break
		}
	}
	if stop != nil {
		stop.Stop()
	}
	if total == 0 {
		res.Status, res.Detail = ProbeSlow, "镜像层取不到数据"
		return
	}
	el := last.Sub(first).Seconds()
	if el < 0.05 {
		el = 0.05
	}
	after := total - firstChunk
	if after <= 0 {
		after = total
	}
	res.MBps = roundTo(float64(after)/el/(1<<20), 2)
	if v, ok := laterHalfMBps(marks); ok && v > res.MBps {
		res.MBps = roundTo(v, 2)
	}
	res.Complete = eof
	switch {
	case eof || res.MBps >= probeFastMBps:
		res.Status = ProbeOK
	default:
		res.Status = ProbeSlow
		res.Detail = fmt.Sprintf("慢（%.2f MB/s）", res.MBps)
	}
	if res.FinalHost != "" && res.FinalHost != res.Host {
		if res.Detail == "" {
			res.Detail = "镜像层由 " + res.FinalHost + " 提供"
		} else {
			res.Detail = strings.TrimSuffix(res.Detail, "）") + "，镜像层被转到 " + res.FinalHost + "）"
		}
	}
}

// laterHalfMBps is the speed over the later half (by time) of a sample's
// reads, when that half is long enough to tell: a download speeds up over
// its first seconds, and the later half is closer to what a pull gets.
func laterHalfMBps(marks []speedSample) (float64, bool) {
	if len(marks) < 4 {
		return 0, false
	}
	first, last := marks[0], marks[len(marks)-1]
	span := last.at.Sub(first.at)
	if span < 400*time.Millisecond {
		return 0, false
	}
	mid := first.at.Add(span / 2)
	base := first
	for _, m := range marks {
		if m.at.After(mid) {
			break
		}
		base = m
	}
	el := last.at.Sub(base.at).Seconds()
	if el < 0.2 || last.bytes <= base.bytes {
		return 0, false
	}
	return float64(last.bytes-base.bytes) / el / (1 << 20), true
}

func shortErr(err error) string {
	s := err.Error()
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80]) + "…"
	}
	return s
}

func roundTo(v float64, digits int) float64 {
	p := 1.0
	for range digits {
		p *= 10
	}
	return float64(int64(v*p+0.5)) / p
}

func codeStatus(code int) string {
	switch code {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ProbeDenied
	case http.StatusNotFound:
		return ProbeNotFound
	case http.StatusTooManyRequests:
		return ProbeLimited
	}
	return ProbeUnreachable
}

func statusDetail(st string, code int) string {
	switch st {
	case ProbeDenied:
		if code > 0 {
			return fmt.Sprintf("拒绝访问（%d）", code)
		}
		return "拒绝访问"
	case ProbeNotFound:
		return "没有这个镜像（404）"
	case ProbeLimited:
		return "限流（429）"
	case ProbeUnreachable:
		if code > 0 {
			return fmt.Sprintf("返回 %d", code)
		}
		return "连不上"
	}
	return ""
}

type manifestEntry struct {
	Digest   string `json:"digest"`
	Platform struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
	} `json:"platform"`
}

type registryManifest struct {
	Manifests []manifestEntry `json:"manifests"`
	Layers    []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"layers"`
}

func getManifest(ctx context.Context, hc *http.Client, base, ref string, hdr http.Header) (registryManifest, int, error) {
	var m registryManifest
	h := hdr.Clone()
	h.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.docker.distribution.manifest.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
	}, ", "))
	resp, err := httpGet(ctx, hc, base+"/manifests/"+ref, h)
	if err != nil {
		return m, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return m, resp.StatusCode, nil
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&m)
	return m, resp.StatusCode, err
}

func pickPlatform(ms []manifestEntry, arch string) string {
	if arch == "" {
		arch = runtime.GOARCH
	}
	for _, m := range ms {
		if m.Platform.OS == "linux" && m.Platform.Architecture == arch {
			return m.Digest
		}
	}
	return ms[0].Digest
}

// anonymousToken gets a pull token from the Bearer realm in the 401's
// WWW-Authenticate header.
func anonymousToken(ctx context.Context, hc *http.Client, challenge, path string) (string, error) {
	params := parseChallenge(challenge)
	realm := params["realm"]
	if realm == "" {
		return "", errors.New("no realm")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	if s := params["service"]; s != "" {
		q.Set("service", s)
	}
	q.Set("scope", "repository:"+path+":pull")
	u.RawQuery = q.Encode()
	resp, err := httpGet(ctx, hc, u.String(), nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token %d", resp.StatusCode)
	}
	var t struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil {
		return "", err
	}
	if t.Token == "" {
		t.Token = t.AccessToken
	}
	if t.Token == "" {
		return "", errors.New("empty token")
	}
	return t.Token, nil
}

// parseChallenge reads `Bearer realm="…",service="…"`.
func parseChallenge(h string) map[string]string {
	out := map[string]string{}
	h = strings.TrimSpace(h)
	if i := strings.IndexByte(h, ' '); i >= 0 {
		h = h[i+1:]
	}
	for _, part := range strings.Split(h, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			out[strings.ToLower(k)] = strings.Trim(v, `"`)
		}
	}
	return out
}

// RankSources orders srcs by their probe results: the fast ones (Status ok)
// keep the table's order, the slow ones follow from fastest to slowest, the
// ones that answered 403, 404, 429 or not at all are left out (skipped).
// When none is usable all of them are returned in the table's order (the
// probe may be wrong) and skipped is nil. A source without a result keeps
// its place among the fast ones. A LastResort source that is usable goes
// behind all the others, the slow ones too, however fast it measured.
func RankSources(srcs []ImageSource, res map[string]ProbeResult) (order, skipped []ImageSource) {
	var fast, slow, lastFast, lastSlow []ImageSource
	for _, s := range srcs {
		r, ok := res[s.Host]
		switch {
		case !ok || r.Status == ProbeOK:
			if s.LastResort {
				lastFast = append(lastFast, s)
			} else {
				fast = append(fast, s)
			}
		case r.Status == ProbeSlow:
			if s.LastResort {
				lastSlow = append(lastSlow, s)
			} else {
				slow = append(slow, s)
			}
		default:
			skipped = append(skipped, s)
		}
	}
	bySpeed := func(l []ImageSource) {
		sort.SliceStable(l, func(i, j int) bool { return res[l[i].Host].MBps > res[l[j].Host].MBps })
	}
	bySpeed(slow)
	bySpeed(lastSlow)
	order = append(fast, slow...)
	order = append(order, lastFast...)
	order = append(order, lastSlow...)
	if len(order) == 0 {
		return slices.Clone(srcs), nil
	}
	return order, skipped
}

// probeCacheTTL is how long a probe result is reused for the same source and
// image repository (a recipe pulls several images in a row).
var probeCacheTTL = 10 * time.Minute

type cachedProbe struct {
	r  ProbeResult
	at time.Time
}

// probe returns results for srcs and ref, reusing cached ones.
func (d *Docker) probe(ctx context.Context, srcs []ImageSource, ref, arch string) map[string]ProbeResult {
	path, _ := refPath(ref)
	now := time.Now()
	out := map[string]ProbeResult{}
	var need []ImageSource
	d.probeMu.Lock()
	for _, s := range srcs {
		if c, ok := d.probeCache[s.Host+"|"+path]; ok && now.Sub(c.at) < probeCacheTTL {
			out[s.Host] = c.r
		} else {
			need = append(need, s)
		}
	}
	d.probeMu.Unlock()
	if len(need) == 0 {
		return out
	}
	if probeHeadSettled(srcs, out) {
		return out
	}
	fresh := d.probeFn(ctx, srcs, out, ref, arch)
	d.probeMu.Lock()
	if d.probeCache == nil {
		d.probeCache = map[string]cachedProbe{}
	}
	for h, r := range fresh {
		out[h] = r
		d.probeCache[h+"|"+path] = cachedProbe{r: r, at: now}
	}
	d.probeMu.Unlock()
	return out
}

// forgetProbe drops the cached result of host for ref's repository (a pull
// from it just failed).
func (d *Docker) forgetProbe(host, ref string) {
	path, _ := refPath(ref)
	d.probeMu.Lock()
	delete(d.probeCache, host+"|"+path)
	d.probeMu.Unlock()
}

// probeFn probes the sources of srcs that known has no result for (early,
// see probeSources).
func (d *Docker) probeFn(ctx context.Context, srcs []ImageSource, known map[string]ProbeResult, ref, arch string) map[string]ProbeResult {
	if d.probeHook != nil {
		var need []ImageSource
		for _, s := range srcs {
			if _, ok := known[s.Host]; !ok {
				need = append(need, s)
			}
		}
		return d.probeHook(ctx, need, ref, arch)
	}
	return probeSources(ctx, srcs, known, ref, arch, true, probeSampleBytes)
}

// probeHeadSettled reports whether res already settles which source a pull
// tries first: a source that came back ok with every source before it done.
func probeHeadSettled(srcs []ImageSource, res map[string]ProbeResult) bool {
	for _, s := range srcs {
		r, ok := res[s.Host]
		if !ok {
			return false
		}
		if r.Status == ProbeOK {
			return true
		}
	}
	return false
}

// PingRegistry asks only host's /v2/ (no sample): ok (200 or 401), denied
// (403), limited (429) or unreachable, at most probeTotal.
func PingRegistry(ctx context.Context, host string) ProbeResult {
	pctx, cancel := context.WithTimeout(ctx, probeTotal)
	defer cancel()
	st, code, _ := pingRegistry(pctx, probeClient(), host)
	return ProbeResult{Host: host, Status: st, HTTPCode: code, Detail: statusDetail(st, code)}
}
