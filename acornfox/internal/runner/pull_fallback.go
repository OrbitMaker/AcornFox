package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/distribution/reference"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/acornfox/acornfox/internal/edge"
)

// How Docker Hub images (the add-on databases, and app images such as
// nginx) and ghcr.io images are pulled. The sources and their layers are in
// sources.go (the one list).
//
//   - Region "cn" (users in mainland China use domestic sources): every
//     source is probed first (sources.go), the ones that refuse or do not
//     answer are skipped and the slow ones go last; then the image is pulled
//     from the first source by full reference and tagged back to the
//     original name. Docker Hub and ghcr.io themselves are blocked there:
//     they are neither probed nor pulled from, so an app install makes no
//     connection to a blocked address and a failure on every mirror is
//     reported at once (CAT-INST-12) instead of after a wait for the
//     blocked address. They are used only when nothing else is configured
//     or the user lists them in ACORNFOX_REGISTRY_MIRRORS /
//     ACORNFOX_GHCR_MIRRORS (a machine that can reach them, e.g. through a
//     proxy).
//   - Otherwise: Docker Hub first, the mirrors only when it stalls, is too
//     slow or fails.
//
// While a source pulls, it is abandoned and the next one tried when
//   - no progress message arrives for pullStallTimeout and the machine's
//     network and disk counters did not move either (Docker sends no
//     progress for some pulls; host_activity.go), or
//   - after slowGrace, the last slowWindow brought less than slowSwitchKBps
//     on average and a source after it probed at least twice as fast, or
//   - the last slowWindow brought less than slowFloorKBps on average.
//
// Otherwise the pull goes on as long as it moves, up to pullMaxTotal for the
// image over all sources. ACORNFOX_REGISTRY_MIRRORS overrides the mirror hosts
// (comma separated; an empty value disables mirrors). On Linux the installer
// also writes the Docker daemon's registry-mirrors from the same table, used
// when the daemon pulls by the original name (builds, Docker Hub last).
const pullStallTimeout = 45 * time.Second

// pullStall is the stall window in use (tests shorten it).
var pullStall = pullStallTimeout

// The slow-source rules (see above). Measured 2026-11 on a home broadband
// line: a large public image's 232 MB layer came at 0.12-0.64 MB/s (6-32 minutes), so a
// moving pull must not be cut early; a source below 16 KB/s would need hours
// for such a layer. Variables so tests can shorten them.
var (
	slowGrace      = 90 * time.Second
	slowWindow     = 120 * time.Second
	slowCheckEvery = 30 * time.Second
	slowSwitchKBps = 100.0
	slowFloorKBps  = 16.0
)

// DefaultRegistryMirrors are the public-layer Docker Hub mirrors of the
// table in sources.go, in its order.
var DefaultRegistryMirrors = layerHosts(dockerHubSources, LayerPublic)

// TencentRegistryMirror is Tencent Cloud's internal Docker Hub accelerator;
// it only answers from Tencent Cloud servers, which put it first.
const TencentRegistryMirror = "mirror.ccs.tencentyun.com"

// CloudRegistryMirrors returns the Docker Hub mirrors for a server in
// mainland China on cloud, the vendor the Linux installer detected from the
// instance metadata (ACORNFOX_CLOUD in /etc/acornfox/install.env): the
// table's cloud-layer sources of that vendor, then the public ones. nil
// means "use the default order" (DefaultRegistryMirrors or
// ACORNFOX_REGISTRY_MIRRORS).
//
//   - tencent: the free internal accelerator first, then the defaults.
//   - aliyun, huawei: their Docker Hub accelerators are per-account addresses
//     (one per user's container registry), there is no shared free one to
//     put in code, so the default order stays (install.sh --registry-mirror
//     adds one; see WithUserMirrors).
//   - volcengine: its internal mirror site carries APT and docker-ce
//     packages, not a Docker Hub registry mirror, so the default order stays.
//   - anything else, or empty: the default order.
//
// An explicit ACORNFOX_REGISTRY_MIRRORS always wins over the vendor's mirror.
func CloudRegistryMirrors(cloud string) []string {
	if _, set := os.LookupEnv("ACORNFOX_REGISTRY_MIRRORS"); set {
		return nil
	}
	cloud = strings.ToLower(strings.TrimSpace(cloud))
	var out []string
	for _, s := range dockerHubSources {
		if s.Layer == LayerCloud && slices.Contains(s.Clouds, cloud) {
			out = append(out, s.Host)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return append(out, DefaultRegistryMirrors...)
}

// DefaultGHCRMirrors are the public-layer ghcr.io mirrors of the table.
// ACORNFOX_GHCR_MIRRORS overrides it like ACORNFOX_REGISTRY_MIRRORS.
var DefaultGHCRMirrors = layerHosts(ghcrSources, LayerPublic)

var (
	errPullStalled = errors.New("拉取镜像长时间没有进展")
	errPullSlow    = errors.New("拉取镜像太慢")
)

// IsPullStalled reports whether err comes from a pull that made no progress
// on every source (the setup shows fault SETUP-NET-02 for it).
func IsPullStalled(err error) bool {
	return err != nil && (errors.Is(err, errPullStalled) || errors.Is(err, errPullSlow) ||
		strings.Contains(err.Error(), errPullStalled.Error()))
}

// PullProgress receives the progress of an image pull: source is the
// reference being pulled (a mirror or Docker Hub), done and total are bytes
// summed over the layers seen so far (total grows as layers are announced).
type PullProgress func(source string, done, total int64)

type pullProgressKey struct{}

// WithPullProgress makes pulls under ctx report their progress to fn (the
// setup page shows it for the gateway image).
func WithPullProgress(ctx context.Context, fn PullProgress) context.Context {
	return context.WithValue(ctx, pullProgressKey{}, fn)
}

// Registry selects the pull order.
type Registry struct {
	China   bool     // mirrors first
	Mirrors []string // nil => registryMirrors()
}

// SetRegistry sets how Docker Hub images are pulled.
func (d *Docker) SetRegistry(r Registry) { d.registry = r }

func (d *Docker) mirrors() []string { return d.registry.mirrors() }

func (r Registry) mirrors() []string {
	if r.Mirrors != nil {
		return r.Mirrors
	}
	return registryMirrors()
}

// DockerHubRegistry is Docker Hub's registry host; PullSources names Docker
// Hub itself with it.
const DockerHubRegistry = "registry-1.docker.io"

// PullSources lists the registry hosts a Docker Hub image is pulled from
// under r, before the probe reorders them: in mainland China the mirrors
// only (Docker Hub itself is blocked there, see SkipsUpstream); elsewhere
// Docker Hub first and the mirrors after it. The setup's registry check
// probes them in this order.
func (r Registry) PullSources() []string {
	mirrors := r.mirrors()
	out := make([]string, 0, len(mirrors)+1)
	if r.China && len(mirrors) > 0 {
		return append(out, mirrors...)
	}
	out = append(out, DockerHubRegistry)
	for _, m := range mirrors {
		if m != DockerHubRegistry {
			out = append(out, m)
		}
	}
	return out
}

// SkipsUpstream reports whether Docker Hub itself is left out of the pull
// sources: in mainland China, when there is at least one mirror and the user
// did not list Docker Hub among them. It is blocked there, so asking it only
// costs a wait (and a connection to a blocked address); a machine that can
// reach it names it in ACORNFOX_REGISTRY_MIRRORS.
func (r Registry) SkipsUpstream() bool {
	m := r.mirrors()
	return r.China && len(m) > 0 && !slices.Contains(m, DockerHubRegistry)
}

// SkipsGHCRUpstream is SkipsUpstream for ghcr.io.
func (r Registry) SkipsGHCRUpstream() bool {
	m := ghcrMirrors()
	return r.China && len(m) > 0 && !slices.Contains(m, GHCRRegistry)
}

func registryMirrors() []string {
	return mirrorsFromEnv("ACORNFOX_REGISTRY_MIRRORS", DefaultRegistryMirrors)
}

func ghcrMirrors() []string { return mirrorsFromEnv("ACORNFOX_GHCR_MIRRORS", DefaultGHCRMirrors) }

// GHCRMirrors lists the ghcr.io mirrors in use (the console's settings page
// shows them).
func GHCRMirrors() []string { return ghcrMirrors() }

func mirrorsFromEnv(name string, def []string) []string {
	v, ok := os.LookupEnv(name)
	if !ok {
		return slices.Clone(def)
	}
	var out []string
	for _, m := range strings.Split(v, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// mirrorRef rewrites a Docker Hub reference to the same image on mirror, or
// returns "" for images from other registries.
func mirrorRef(ref, mirror string) string { return rewriteRef(ref, "docker.io", mirror) }

// ghcrMirrorRef does the same for ghcr.io references.
func ghcrMirrorRef(ref, mirror string) string { return rewriteRef(ref, "ghcr.io", mirror) }

func rewriteRef(ref, domain, mirror string) string {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil || reference.Domain(named) != domain {
		return ""
	}
	out := mirror + "/" + reference.Path(named)
	if t, ok := named.(reference.Tagged); ok {
		out += ":" + t.Tag()
	}
	if d, ok := named.(reference.Digested); ok {
		out += "@" + d.Digest().String()
	}
	return out
}

// pullStats is what one source's pull brought.
type pullStats struct {
	Bytes, Total int64
	Seconds      float64
	AvgKBps      float64 // the recent average when the source was given up for slowness
	// Blind: Docker sent no layer progress for a while and the machine's
	// counters showed the pull moving (pullWatched).
	Blind bool
}

// pullWatch tells pullWatched about the sources after this one.
type pullWatch struct {
	hasNext  bool
	nextMBps float64 // the best probe speed among them, 0 when unknown
}

// speedWatch keeps a pull's byte counts over time.
type speedWatch struct {
	start   time.Time
	samples []speedSample
}

type speedSample struct {
	at    time.Time
	bytes int64
}

func (w *speedWatch) add(at time.Time, b int64) {
	w.samples = append(w.samples, speedSample{at, b})
	// Keep one sample older than the window and the ones inside it.
	cut := at.Add(-slowWindow)
	i := 0
	for i+1 < len(w.samples) && !w.samples[i+1].at.After(cut) {
		i++
	}
	if i > 0 {
		w.samples = append(w.samples[:0], w.samples[i:]...)
	}
}

// recentKBps is the average speed over the last slowWindow (or since the
// start when the pull is younger).
func (w *speedWatch) recentKBps(now time.Time) float64 {
	from := now.Add(-slowWindow)
	base := speedSample{w.start, 0}
	cur := int64(0)
	for _, s := range w.samples {
		if !s.at.After(from) {
			base = s
		}
		cur = s.bytes
	}
	el := now.Sub(base.at).Seconds()
	if el <= 0 {
		return 0
	}
	return float64(cur-base.bytes) / 1024 / el
}

// verdict says whether to give the source up now; pending is whether a
// layer is still downloading (extracting does not count).
func (w *speedWatch) verdict(now time.Time, pending bool, pw pullWatch) (bool, float64) {
	if !pending || now.Sub(w.start) < slowGrace {
		return false, 0
	}
	kbps := w.recentKBps(now)
	if pw.hasNext && pw.nextMBps > 0 && kbps < slowSwitchKBps && pw.nextMBps*1024 >= 2*kbps {
		return true, kbps
	}
	if kbps < slowFloorKBps {
		return true, kbps
	}
	return false, kbps
}

// pullWatched pulls ref and gives up when it makes no progress for stall
// (errPullStalled) or the source is too slow (errPullSlow).
//
// Progress is Docker's pull messages. When they stop for stall, the
// machine's network and disk counters (hostActivity) decide: if they moved
// at least slowFloorKBps over that time the pull counts as moving "blind"
// (Docker hides the progress of a pull that continues an image an earlier
// pull left half done; see host_activity.go), and the counters stand in for
// the bytes until Docker's messages come back.
func (d *Docker) pullWatched(ctx context.Context, ref string, platforms []ocispec.Platform, stall time.Duration, pw pullWatch) (pullStats, error) {
	t0 := time.Now()
	var st pullStats
	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	resp, err := d.c().ImagePull(pctx, ref, client.ImagePullOptions{Platforms: platforms})
	if err != nil {
		return st, err
	}
	progress := make(chan struct{}, 1)
	done := make(chan error, 1)
	report, _ := ctx.Value(pullProgressKey{}).(PullProgress)
	tr := trackerFrom(ctx)
	type layer struct {
		cur, total int64
		done       bool
	}
	var mu sync.Mutex
	layers := map[string]layer{}
	watch := &speedWatch{start: t0}
	pending := false
	// blind: Docker's messages stopped and the counters say the pull moves;
	// blindBytes is what the counters added meanwhile (on top of st.Bytes).
	blind := false
	var blindBytes int64
	go func() {
		for msg, err := range resp.JSONMessages(pctx) {
			if err != nil {
				done <- err
				return
			}
			if msg.Error != nil {
				done <- errors.New(msg.Error.Message)
				return
			}
			if msg.ID != "" && layerStatus(msg.Status) {
				mu.Lock()
				blind = false
				l := layers[msg.ID]
				switch {
				case msg.Status == "Downloading" && msg.Progress != nil && msg.Progress.Total > 0:
					l.cur, l.total = msg.Progress.Current, msg.Progress.Total
				case msg.Status == "Download complete" || msg.Status == "Pull complete" || msg.Status == "Already exists" ||
					msg.Status == "Verifying Checksum" || msg.Status == "Extracting":
					l.cur, l.done = l.total, true
				}
				layers[msg.ID] = l
				var cur, total int64
				pending = false
				for _, v := range layers {
					cur, total = cur+v.cur, total+v.total
					if !v.done {
						pending = true
					}
				}
				st.Bytes, st.Total = cur, total
				watch.add(time.Now(), cur+blindBytes)
				mu.Unlock()
				tr.progress(cur, total)
				if report != nil {
					report(ref, cur, total)
				}
			}
			select {
			case progress <- struct{}{}:
			default:
			}
		}
		done <- nil
	}()
	snapshot := func() pullStats {
		mu.Lock()
		defer mu.Unlock()
		s := st
		s.Seconds = time.Since(t0).Seconds()
		return s
	}
	// arm is the counters' reading when the stall timer was last armed (read
	// at most once a second while messages flow): when the timer fires, the
	// counters' rate since then decides. acct is the reading blindBytes was
	// last brought up to.
	type reading struct {
		v  int64
		at time.Time
	}
	read := func(now time.Time) (reading, bool) {
		v, ok := hostActivity()
		return reading{v, now}, ok
	}
	arm, meterOK := read(t0)
	acct := arm
	rearm := func(now time.Time) {
		if now.Sub(arm.at) >= time.Second {
			if r, ok := read(now); ok {
				arm = r
			}
		}
	}
	// account adds the counters' growth since acct to blindBytes.
	account := func(r reading) {
		delta := max(r.v-acct.v, 0)
		acct = r
		mu.Lock()
		blindBytes += delta
		watch.add(r.at, st.Bytes+blindBytes)
		mu.Unlock()
	}
	timer := time.NewTimer(stall)
	defer timer.Stop()
	check := time.NewTicker(slowCheckEvery)
	defer check.Stop()
	for {
		select {
		case err := <-done:
			return snapshot(), err
		case <-progress:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(stall)
			rearm(time.Now())
		case now := <-timer.C:
			if r, ok := read(now); ok && meterOK {
				el := now.Sub(arm.at).Seconds()
				kbps := 0.0
				if el > 0 {
					kbps = float64(max(r.v-arm.v, 0)) / 1024 / el
				}
				if kbps >= slowFloorKBps {
					mu.Lock()
					first := !blind && !st.Blind
					if !blind {
						acct = arm // the quiet time so far counts too
					}
					blind, st.Blind = true, true
					mu.Unlock()
					account(r)
					arm = r
					tr.blind()
					if first {
						pullLogf("拉 %s：Docker %d 秒没发进度消息，本机这段时间的收发和读写约 %.0f KB/s，按还在下处理", ref, int(stall/time.Second), kbps)
					}
					timer.Reset(stall)
					continue
				}
			}
			cancel()
			<-done
			return snapshot(), errPullStalled
		case now := <-check.C:
			mu.Lock()
			isBlind := blind
			mu.Unlock()
			if isBlind {
				if r, ok := read(now); ok {
					account(r)
				}
			}
			mu.Lock()
			give, kbps := watch.verdict(now, pending || blind, pw)
			mu.Unlock()
			if give {
				cancel()
				<-done
				s := snapshot()
				s.AvgKBps = roundTo(kbps, 1)
				return s, errPullSlow
			}
		case <-ctx.Done():
			return snapshot(), ctx.Err()
		}
	}
}

// windowText is slowWindow in words ("2 分钟", "5 秒").
func windowText() string {
	if slowWindow >= time.Minute {
		return fmt.Sprintf("%d 分钟", int(slowWindow/time.Minute))
	}
	return fmt.Sprintf("%d 秒", int(slowWindow/time.Second))
}

// layerStatus reports whether a pull message with this status is about one
// layer (not "Pulling from …", "Digest: …" or "Status: …").
func layerStatus(st string) bool {
	for _, p := range []string{"Pulling fs layer", "Waiting", "Downloading", "Verifying Checksum", "Download complete",
		"Extracting", "Pull complete", "Already exists", "Retrying"} {
		if strings.HasPrefix(st, p) {
			return true
		}
	}
	return false
}

// pullCandidate is one source to try for an image.
type pullCandidate struct {
	src ImageSource
	ref string // the reference to pull from it
}

// candidates lists the sources for ref in table order (pull order before the
// probe). An image of another registry has only itself.
func (d *Docker) candidates(ref string) ([]pullCandidate, string) {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return []pullCandidate{{ImageSource{Host: "", Layer: LayerUpstream}, ref}}, ""
	}
	domain := reference.Domain(named)
	var srcs []ImageSource
	switch domain {
	case "docker.io":
		srcs = d.registry.Sources()
	case GHCRRegistry:
		srcs = d.registry.GHCRSources()
	default:
		return []pullCandidate{{ImageSource{Host: domain, Layer: LayerUpstream, Name: domain}, ref}}, domain
	}
	var out []pullCandidate
	for _, s := range srcs {
		if s.Layer == LayerUpstream {
			out = append(out, pullCandidate{s, ref})
		} else if r := rewriteRef(ref, domain, s.Host); r != "" {
			out = append(out, pullCandidate{s, r})
		}
	}
	return out, domain
}

// pullUsed is the source an image came from.
type pullUsed struct {
	Host    string
	Seconds float64
	Blind   bool // Docker sent no layer progress; the pull was judged by the machine's traffic
}

func platformArch(platforms []ocispec.Platform) string {
	if len(platforms) > 0 && platforms[0].Architecture != "" {
		return platforms[0].Architecture
	}
	return runtime.GOARCH
}

// pullFallback pulls ref from its sources (see above). The caller's context
// bounds the whole attempt. It returns the local reference that holds the
// image (ref, or a mirror's name for an image pinned by digest) and the
// source it came from. A failure on every source is a *PullError.
func (d *Docker) pullFallback(ctx context.Context, ref string, platforms []ocispec.Platform) (string, pullUsed, error) {
	start := time.Now()
	tr := trackerFrom(ctx)
	cands, domain := d.candidates(ref)
	var probes map[string]ProbeResult
	var skipped []ImageSource
	if d.registry.China && len(cands) > 1 {
		tr.probing()
		srcs := make([]ImageSource, len(cands))
		for i, c := range cands {
			srcs[i] = c.src
		}
		t0 := time.Now()
		probes = d.probe(ctx, srcs, ref, platformArch(platforms))
		var order []ImageSource
		order, skipped = RankSources(srcs, probes)
		var parts, hosts []string
		for _, s := range srcs {
			p, probed := probes[s.Host]
			if !probed {
				parts = append(parts, s.Host+" 没测完（排在前面的已经够快）")
				continue
			}
			if p.Status == ProbeOK {
				parts = append(parts, fmt.Sprintf("%s 能用 %.1f MB/s", s.Host, p.MBps))
			} else {
				parts = append(parts, s.Host+" "+p.Status+" "+p.Detail)
			}
		}
		for _, s := range order {
			hosts = append(hosts, s.Host)
		}
		pullLogf("拉 %s 前实测镜像源（%.0f 秒）：%s；顺序：%s", ref, time.Since(t0).Seconds(), strings.Join(parts, "、"), strings.Join(hosts, " → "))
		byHost := map[string]pullCandidate{}
		for _, c := range cands {
			byHost[c.src.Host] = c
		}
		cands = cands[:0:0]
		for _, s := range order {
			cands = append(cands, byHost[s.Host])
		}
	}
	pe := &PullError{Ref: ref}
	if domain == "docker.io" && d.registry.SkipsUpstream() {
		pe.UpstreamSkipped = DockerHubRegistry
	} else if domain == GHCRRegistry && d.registry.SkipsGHCRUpstream() {
		pe.UpstreamSkipped = GHCRRegistry
	}
	for _, s := range skipped {
		pe.Attempts = append(pe.Attempts, attemptFor(s, probes, "skipped", pullStats{}, nil))
	}
	// Docker refuses to tag with a digest, so an image pinned by digest keeps
	// the mirror's name: the digest already proves it is the same image.
	_, pinned := parseDigested(ref)
	for i, c := range cands {
		pw := pullWatch{hasNext: i+1 < len(cands)}
		for _, n := range cands[i+1:] {
			if p, ok := probes[n.src.Host]; ok && p.Usable() && p.MBps > pw.nextMBps {
				pw.nextMBps = p.MBps
			}
		}
		tr.source(c.src.Host, i+1, len(cands))
		st, err := d.pullOne(ctx, c.ref, platforms, pw)
		if err == nil {
			used := pullUsed{Host: c.src.Host, Seconds: st.Seconds, Blind: st.Blind}
			blindNote := ""
			if st.Blind {
				blindNote = "（中间 Docker 没发进度，按本机流量判断它还在下）"
			}
			pullLogf("从 %s 拉好 %s，用了 %.0f 秒%s", c.src.Host, ref, st.Seconds, blindNote)
			if c.ref == ref || pinned {
				return c.ref, used, nil
			}
			if _, err := d.c().ImageTag(ctx, client.ImageTagOptions{Source: c.ref, Target: ref}); err != nil {
				return "", used, err
			}
			// Drop the mirror name; the layers stay under the original tag.
			_, _ = d.c().ImageRemove(ctx, c.ref, client.ImageRemoveOptions{})
			return ref, used, nil
		}
		if noMatchingPlatform(err) {
			return "", pullUsed{}, err
		}
		d.forgetProbe(c.src.Host, ref)
		outcome := attemptOutcome(ctx, err)
		a := attemptFor(c.src, probes, outcome, st, err)
		pullLogf("从 %s 拉 %s 没成：%s", c.src.Host, ref, a.Message)
		tr.gaveUp(a.Message)
		pe.Attempts = append(pe.Attempts, a)
		pe.errs = append(pe.errs, err)
		if ctx.Err() != nil {
			pe.errs = append(pe.errs, ctx.Err())
			pe.Reason = pullEndReason(ctx)
			pe.Seconds = time.Since(start).Seconds()
			pullLogf("拉 %s 停下（%s）：一共拉了 %s", ref, reasonText(pe.Reason), durText(pe.Seconds))
			return "", pullUsed{}, pe
		}
		// Outside China, Docker Hub saying "no such image" is final: the
		// mirrors only copy it.
		if !d.registry.China && c.src.Layer == LayerUpstream && outcome == "not_found" {
			return "", pullUsed{}, pe
		}
	}
	if len(pe.errs) == 0 {
		pe.errs = append(pe.errs, errors.New("没有可用的镜像源"))
	}
	pe.Seconds = time.Since(start).Seconds()
	return "", pullUsed{}, pe
}

// pullLog receives one line per probe and per source tried (the runner's
// journal: journalctl -u acornfox-runner); tests silence it.
var pullLog io.Writer = os.Stderr

func pullLogf(format string, a ...any) {
	if pullLog != nil {
		fmt.Fprintf(pullLog, "acornfox-runner: "+format+"\n", a...)
	}
}

// pullOne pulls one candidate (tests replace it with pullHook).
func (d *Docker) pullOne(ctx context.Context, ref string, platforms []ocispec.Platform, pw pullWatch) (pullStats, error) {
	if d.pullHook != nil {
		return d.pullHook(ctx, ref, pw)
	}
	return d.pullWatched(ctx, ref, platforms, pullStall, pw)
}

// PullError is a pull that failed on every source it tried.
type PullError struct {
	Ref string
	// Attempts are the sources in the order they were considered: the ones
	// the probe skipped first, then the tried ones.
	Attempts []SourceResult
	// Reason is what ended a pull that ran out of time (Failure.Reason);
	// Seconds how long the image was pulled over all sources.
	Reason  string
	Seconds float64
	// UpstreamSkipped is the blocked registry (Docker Hub, ghcr.io) that a
	// pull in mainland China did not ask at all (Registry.SkipsUpstream);
	// the message and hint say so, empty otherwise.
	UpstreamSkipped string
	errs            []error
}

func (e *PullError) Unwrap() []error { return e.errs }

func (e *PullError) Error() string {
	msg := SourcesSummary(e.Attempts)
	if e.UpstreamSkipped != "" {
		msg += "；国内网络没有去试 " + e.UpstreamSkipped + "（它在国内被屏蔽，去了只会白等）"
	}
	return msg
}

// SourcesSummary is the one-line account of a failed pull's sources: how
// many were tried and what each one did.
func SourcesSummary(attempts []SourceResult) string {
	var parts []string
	tried := 0
	for _, a := range attempts {
		parts = append(parts, a.Message)
		if a.Outcome != "skipped" {
			tried++
		}
	}
	return fmt.Sprintf("镜像没拉下来（试了 %d 个源）：%s", tried, strings.Join(parts, "；"))
}

// CallerDeadlineWords is how a pull stopped by its caller's deadline says
// so; a caller that knows what its limit was (an app install) puts its own
// words in their place.
const CallerDeadlineWords = "调用方给的时间先用完了"

// reasonText is a Failure.Reason in words.
func reasonText(r string) string {
	switch r {
	case ReasonCallerDeadline:
		return CallerDeadlineWords
	case ReasonPullDeadline:
		return fmt.Sprintf("到了一个镜像的总时限 %d 分钟", int(pullMaxTotal/time.Minute))
	case ReasonCancelled:
		return "调用方取消了"
	}
	return r
}

// durText is a duration in words: "48 秒", "31.5 分钟".
func durText(sec float64) string {
	if sec < 90 {
		return fmt.Sprintf("%.0f 秒", sec)
	}
	m := sec / 60
	if m == float64(int(m)) {
		return fmt.Sprintf("%d 分钟", int(m))
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", m), ".0") + " 分钟"
}

// deadline reports whether the pull's total time ran out.
func (e *PullError) deadline() bool {
	for _, err := range e.errs {
		if errors.Is(err, context.DeadlineExceeded) {
			return true
		}
	}
	return false
}

func attemptOutcome(ctx context.Context, err error) string {
	msg := ansiPattern.ReplaceAllString(err.Error(), "")
	switch {
	case errors.Is(err, errPullSlow):
		return "slow"
	case errors.Is(err, errPullStalled):
		return "stalled"
	case ctx.Err() != nil:
		switch pullEndReason(ctx) {
		case ReasonCallerDeadline:
			return "cut_off"
		case ReasonCancelled:
			return "cancelled"
		}
		return "timeout"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case pullNotFoundPattern.MatchString(msg):
		return "not_found"
	case pullFailurePattern.MatchString(msg):
		return "unreachable"
	}
	return "failed"
}

// attemptFor describes one source's part in a pull.
func attemptFor(s ImageSource, probes map[string]ProbeResult, outcome string, st pullStats, err error) SourceResult {
	a := SourceResult{Host: s.Host, Layer: string(s.Layer), Name: s.Name, Outcome: outcome,
		Bytes: st.Bytes, Total: st.Total, Seconds: int(st.Seconds + 0.5), AvgKBps: st.AvgKBps}
	p, probed := probes[s.Host]
	if probed {
		a.Probe, a.ProbeMBps, a.FinalHost = p.Status, p.MBps, p.FinalHost
	}
	host := s.Host
	if host == "" {
		host = "镜像仓库"
	}
	moved := ""
	if a.FinalHost != "" && a.FinalHost != s.Host {
		moved = "，镜像层被转到 " + a.FinalHost
	}
	got := ""
	if st.Total > 0 {
		got = fmt.Sprintf("，已下 %s/%s", mb(st.Bytes), mb(st.Total))
	}
	switch outcome {
	case "skipped":
		a.Message = fmt.Sprintf("%s %s，探测时已跳过", host, p.Detail)
	case "slow":
		a.Message = fmt.Sprintf("%s 太慢（最近 %s平均 %.0f KB/s%s%s），换下一个源", host, windowText(), st.AvgKBps, got, moved)
	case "stalled":
		a.Message = fmt.Sprintf("%s %d 秒没有进展（%s%s）", host, int(pullStall/time.Second), strings.TrimPrefix(got, "，"), moved)
		a.Message = strings.ReplaceAll(a.Message, "（）", "")
	case "timeout":
		a.Message = fmt.Sprintf("%s 到了一个镜像的总时限（%d 分钟）还没拉完（%s%s）", host, int(pullMaxTotal/time.Minute), strings.TrimPrefix(got, "，"), moved)
		a.Message = strings.ReplaceAll(a.Message, "（）", "")
	case "cut_off":
		a.Message = fmt.Sprintf("%s 还在拉（这个源拉了 %s%s%s），%s，停下", host, durText(st.Seconds), got, moved, CallerDeadlineWords)
	case "cancelled":
		a.Message = fmt.Sprintf("%s 拉到一半被取消（这个源拉了 %s%s）", host, durText(st.Seconds), got)
	case "not_found":
		a.Message = host + " 没有这个镜像，或拒绝访问"
	case "unreachable":
		a.Message = host + " 连不上（" + shortErr(err) + "）"
	default:
		a.Message = host + " 出错（" + shortErr(err) + "）"
	}
	return a
}

func mb(b int64) string {
	if b < 1<<20 {
		return fmt.Sprintf("%.0f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
}

// parseDigested reports whether ref names its image by digest.
func parseDigested(ref string) (reference.Named, bool) {
	named, err := reference.ParseNormalizedNamed(ref)
	if err != nil {
		return nil, false
	}
	_, ok := named.(reference.Digested)
	return named, ok
}

// EdgeImagePresent reports whether the entry proxy image is on this computer
// (doctor, setup).
func (d *Docker) EdgeImagePresent(ctx context.Context) bool {
	_, err := d.c().ImageInspect(ctx, edge.Image)
	return err == nil
}

// pullErrorFailure turns a pull that failed on every source into the
// user-facing Failure: the message says what each source did, the hint what
// to do about it.
func pullErrorFailure(pe *PullError) *Failure {
	f := &Failure{Stage: "image", Sources: pe.Attempts, Message: pe.Error(), Reason: pe.Reason, PullSeconds: int(pe.Seconds + 0.5)}
	if f.Reason == "" && pe.deadline() {
		f.Reason = ReasonPullDeadline
	}
	var notFound, slow, unreach, tried int
	for _, a := range pe.Attempts {
		switch a.Outcome {
		case "skipped":
			switch a.Probe {
			case ProbeNotFound:
				notFound++
			default:
				unreach++
			}
			continue
		case "not_found":
			notFound++
		case "slow", "stalled", "timeout", "cut_off", "cancelled":
			slow++
		case "unreachable":
			unreach++
		}
		tried++
	}
	switch {
	case f.Reason == ReasonPullDeadline:
		f.Code = "pull_timeout"
		f.Message = fmt.Sprintf("拉取镜像超时：%d 分钟还没拉完。", int(pullMaxTotal/time.Minute)) + f.Message
		f.Hint = slowHint
	case f.Reason == ReasonCallerDeadline:
		f.Code = "pull_timeout"
		f.Message = fmt.Sprintf("拉镜像停下：%s（这个镜像拉了 %s）。", CallerDeadlineWords, durText(pe.Seconds)) + f.Message
		f.Hint = slowHint
	case f.Reason == ReasonCancelled:
		f.Code = "pull_failed"
		f.Message = fmt.Sprintf("拉镜像被取消（这个镜像拉了 %s）。", durText(pe.Seconds)) + f.Message
		f.Hint = "重新安装或部署即可"
	case notFound > 0 && notFound == len(pe.Attempts):
		f.Code = "image_not_found"
		f.Message = "找不到镜像，或镜像加速源不提供该镜像：" + f.Message
		f.Hint = "检查镜像名与标签是否正确；若镜像确实存在，可能是加速源未收录，换用其他加速源或自行构建"
	case slow > 0:
		f.Code = "pull_timeout"
		f.Hint = slowHint
	case unreach > 0:
		f.Code = "pull_timeout"
		f.Hint = "这台机器连不上能用的镜像源：先查网络和代理（sudo acornfox doctor），acornfox sources --probe 看每个源现在怎样；稍后重试" + upstreamHint(pe)
	default:
		f.Code = "pull_failed"
		f.Hint = "检查镜像引用或稍后重试；acornfox sources --probe 看每个源现在怎样" + upstreamHint(pe)
	}
	return f
}

// upstreamHint is the advice for a machine in mainland China whose pull did
// not ask the blocked upstream registry: if the machine does reach it (a
// proxy), name it in the environment variable of its kind.
func upstreamHint(pe *PullError) string {
	switch pe.UpstreamSkipped {
	case "":
		return ""
	case GHCRRegistry:
		return "；国内网络默认不去 ghcr.io（被屏蔽）。这台机器能直连它的话，把 ghcr.io 写进环境变量 ACORNFOX_GHCR_MIRRORS（逗号分隔，放在镜像源后面）"
	}
	return "；国内网络默认不去 Docker Hub（被屏蔽）。这台机器能直连它的话，把 " + DockerHubRegistry + " 写进环境变量 ACORNFOX_REGISTRY_MIRRORS（逗号分隔，放在镜像源后面）"
}

// slowHint is the advice when the sources were reachable but too slow.
const slowHint = "镜像源都很慢：换个时间再装；或给 Docker 加一个自己的镜像加速地址（安装时 --registry-mirror，或设 ACORNFOX_REGISTRY_MIRRORS；阿里云服务器用容器镜像服务控制台里的专属加速地址）；acornfox sources --probe 看每个源现在多快"
