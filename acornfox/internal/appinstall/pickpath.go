package appinstall

// Choosing between the download gateway and the image sources (mirrors) for
// an image that names an archive (artifacts).
//
// Both are measured at the same time for a few seconds: the gateway's
// segmented download starts at once (its bytes count: if the gateway wins
// they are already in the file), the mirrors are probed with the sources'
// own probe (runner.ProbeSources through Config.MirrorProbe: the first bytes
// of the image's largest layer). The faster goes first and the other is the
// back-up when it fails:
//
//   - the gateway answered and the mirrors did not (or none is usable): the
//     gateway;
//   - the gateway did not (could not issue an address, failed, brought
//     nothing in time) and a mirror is usable: the mirrors;
//   - both: the mirrors only when their speed times mirrorFactor is more
//     than the gateway's speed (a tie goes to the gateway: its archive is
//     checked against the signed catalog as a whole);
//   - neither: the old order (the gateway's result stands, the pull from the
//     sources goes on after it): the runner knows its sources better than
//     this probe.
//
// Both measure over a window at the same time, so a line that is the limit
// (not the server's per-connection limit) is shared between them and the
// gateway's many connections take the bigger share: on such a line the
// mirrors' number is too low. That is the reason for mirrorFactor.
//
// Parameters (code defaults; environment variables read when an install
// reaches this step):
//
//	ACORNFOX_DL_PROBE_SECONDS   6    the window the gateway's speed is taken over (its later half)
//	ACORNFOX_DL_MIRROR_FACTOR   1.0  the mirror's measured speed is multiplied by this before the comparison
//
// The gateway must show speed within pickMaxWait of the start, else its speed
// is 0. Without Config.MirrorProbe (tests, a machine without the sources'
// probe) none of this happens: the gateway first, the sources when it fails.

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/installtiming"
	"github.com/acornfox/acornfox/internal/runner"
)

// MirrorSpeed is what Config.MirrorProbe reports: the fastest usable image
// source and its speed. Usable false: none of them can be used.
type MirrorSpeed struct {
	Source string  `json:"source"`
	MBps   float64 `json:"mbps"`
	Usable bool    `json:"usable"`
}

// Defaults of the choice (the one table; the doc comment above and
// docs/capabilities.md point here).
const (
	pickDefaultWindow = 6 * time.Second
	pickMaxWait       = 15 * time.Second // the gateway must have shown bytes by then
	pickMirrorWait    = 15 * time.Second // most to wait for the mirror probe after the window
)

// pickPoll is how often the gateway's bytes are sampled (a variable for tests).
var pickPoll = 200 * time.Millisecond

type pickParams struct {
	Window       time.Duration
	MirrorFactor float64
}

func pickParamsFromEnv() pickParams {
	p := pickParams{
		Window:       time.Duration(envInt("ACORNFOX_DL_PROBE_SECONDS", int(pickDefaultWindow/time.Second))) * time.Second,
		MirrorFactor: 1.0,
	}
	if p.Window <= 0 {
		p.Window = pickDefaultWindow
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("ACORNFOX_DL_MIRROR_FACTOR")), 64); err == nil && v > 0 {
		p.MirrorFactor = v
	}
	return p
}

// gwJob is a download from the gateway running in the background.
type gwJob struct {
	st     *dlStatus
	done   chan struct{}
	cancel context.CancelFunc
	dest   string // valid after done
	err    error  // valid after done
}

// startGwJob asks the gateway for art's address and downloads it, in the
// background.
func (t *taskRun) startGwJob(ctx context.Context, p *planned, art catalog.ArchArtifact) *gwJob {
	jctx, cancel := context.WithCancel(ctx)
	j := &gwJob{st: &dlStatus{}, done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(j.done)
		j.dest, j.err = t.gatewayDownload(jctx, p, art, j.st)
	}()
	return j
}

// stop ends the job and removes a finished file; what the segmented
// download keeps in its cache stays.
func (j *gwJob) stop() {
	j.cancel()
	<-j.done
	if j.err == nil && j.dest != "" {
		_ = os.Remove(j.dest)
	}
}

type pickSample struct {
	at time.Time
	n  int64
}

// laterRate is the speed over the later half of the time between startAt and
// now, from the byte counts sampled so far, in bytes per second (0 when it
// cannot be told yet).
func laterRate(samples []pickSample, startAt, now time.Time) float64 {
	if startAt.IsZero() || len(samples) == 0 {
		return 0
	}
	mid := startAt.Add(now.Sub(startAt) / 2)
	from := samples[0]
	for _, s := range samples {
		if s.at.After(mid) {
			break
		}
		from = s
	}
	last := samples[len(samples)-1]
	if dt := last.at.Sub(from.at).Seconds(); dt > 0 {
		return float64(last.n-from.n) / dt
	}
	return 0
}

// measureGateway waits until the gateway's download has run for window
// since its first byte (or finished, failed, or pickMaxWait passed) and
// returns its speed over the later half of that time, in bytes per second.
// A job that failed returns its error; one that finished within the window
// returns the whole file's average speed.
func measureGateway(ctx context.Context, j *gwJob, window time.Duration) (float64, error) {
	tick := time.NewTicker(pickPoll)
	defer tick.Stop()
	begin := time.Now()
	deadline := time.NewTimer(pickMaxWait)
	defer deadline.Stop()
	var samples []pickSample
	var startAt time.Time
	for {
		select {
		case <-j.done:
			if j.err != nil {
				return 0, j.err
			}
			if dt := time.Since(begin).Seconds(); dt > 0 {
				return float64(j.st.total.Load()) / dt, nil
			}
			return 0, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-deadline.C:
			return laterRate(samples, startAt, time.Now()), nil
		case now := <-tick.C:
			n := j.st.done.Load()
			if startAt.IsZero() && n > 0 {
				startAt = now
			}
			samples = append(samples, pickSample{now, n})
			if !startAt.IsZero() && now.Sub(startAt) >= window {
				return laterRate(samples, startAt, now), nil
			}
		}
	}
}

// speedText is "0.9 MB/s".
func speedText(bps float64) string {
	return fmt.Sprintf("%.1f MB/s", bps/(1<<20))
}

// pickSource measures the gateway and the mirrors, brings the image from the
// faster and falls back to the other (see the file comment). handled false:
// nothing was pulled from the sources yet and the caller goes on with the
// pull from them (notes says why the gateway was left); handled true: the
// result (id, msg) or err is final.
func (t *taskRun) pickSource(ctx context.Context, p *planned, cat runner.CatalogAPI, art catalog.ArchArtifact, img catalog.Image, tagID string, notes *[]string) (id, msg string, err error, handled bool) {
	e := t.e
	pp := pickParamsFromEnv()
	t.progress(ctx, p.name, "image", "正在测下载网关和镜像站哪个快（几秒）…", nil)
	mch := make(chan MirrorSpeed, 1)
	mctx, mcancel := context.WithCancel(ctx)
	defer mcancel()
	go func() { mch <- e.cfg.MirrorProbe(mctx, img.Repository+"@"+img.Digest, e.cfg.Arch) }()
	job := t.startGwJob(ctx, p, art)
	gwRate, gwErr := measureGateway(ctx, job, pp.Window)
	if ctx.Err() != nil {
		job.stop()
		return "", "", fault(CodePull, ctx.Err(), "安装被取消"), true
	}
	var mir MirrorSpeed
	select {
	case mir = <-mch:
	case <-time.After(pickMirrorWait):
	case <-ctx.Done():
	}
	mcancel()

	gwOK := gwErr == nil && gwRate > 0
	mirrorFirst := mir.Usable && (!gwOK || mir.MBps*pp.MirrorFactor*(1<<20) > gwRate)
	why := "测速：下载网关 " + speedText(gwRate)
	if n := job.st.conns.Load(); n > 1 && gwOK {
		why += fmt.Sprintf("（%d 路）", n)
	}
	switch {
	case gwErr != nil:
		why = "测速：下载网关没取到数据"
	case !gwOK:
		why += "（还没有数据）"
	}
	if mir.Usable {
		why += "，镜像站 " + mir.Source + " " + speedText(mir.MBps*(1<<20))
	} else {
		why += "，镜像站都不能用"
	}

	if !mirrorFirst {
		if gwErr != nil {
			// The gateway failed and no mirror is usable by the probe: the
			// pull from the sources goes on as before.
			job.stop()
			*notes = append(*notes, "下载网关："+gwErr.Error())
			t.progress(ctx, p.name, "image", "下载网关没成功，改从公共源拉取", nil)
			return "", "", nil, false
		}
		t.progress(ctx, p.name, "image", "用下载网关（"+why+"）", nil)
		stopReport := t.reportDownload(ctx, p.name, 0, job.st)
		select {
		case <-job.done:
		case <-ctx.Done():
			job.stop()
		}
		stopReport()
		if job.err == nil && ctx.Err() == nil {
			defer os.Remove(job.dest)
			lid, lerr := t.loadArchive(ctx, p, cat, tagID, job.dest, job.st.total.Load())
			if lerr == nil {
				return lid, installtiming.GatewayDone + "（" + why + "）", nil, true
			}
			job.err = lerr
		}
		e.cfg.Log.Warn("gateway image", "app", p.name, "artifact", art.File, "err", scrubURL(job.err))
		*notes = append(*notes, "下载网关："+scrubURL(job.err).Error())
		t.progress(ctx, p.name, "image", "下载网关没成功，改从公共源拉取", nil)
		return "", "", nil, false
	}

	// The mirrors first; what the gateway has downloaded stays in the cache
	// in case the mirrors fail.
	job.stop()
	t.progress(ctx, p.name, "image", "从镜像站拉取（"+why+"）…", nil)
	mid, mmsg, merr := t.pullPublic(ctx, p, img, tagID, *notes)
	if merr == nil {
		newDLCacheEntry(dlCacheDir(e.cfg.UploadDir), art.SHA256, job.st.total.Load(), 1).remove()
		return mid, mmsg + "（" + why + "）", nil, true
	}
	if ctx.Err() != nil {
		return "", "", merr, true
	}
	t.progress(ctx, p.name, "image", "镜像站没成功，改用下载网关…", nil)
	gid, gerr := t.fromGateway(ctx, p, cat, art, tagID)
	if gerr == nil {
		return gid, installtiming.GatewayDone + "（镜像站没拉成，改用下载网关；" + why + "）", nil, true
	}
	if f := AsFault(merr); f != nil {
		f.Message += "；下载网关：" + scrubURL(gerr).Error()
	}
	return "", "", merr, true
}
