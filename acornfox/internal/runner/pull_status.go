package runner

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"time"
)

// The live status of a pull from the public sources (PullImage), so that a
// caller waiting on the long pull request can say which source is pulling
// and how far it got: the app center's install engine asks for it every few
// seconds and shows it in the install's image step (`acornfox install
// status`, the console).
//
// What it reports is only what Docker told the pull: bytes and their speed
// come from Docker's progress messages. While Docker sends none for the
// layers (it hides them when it continues an image an earlier pull left half
// done, see host_activity.go) Blind is true and Bytes stays at Docker's last
// figure; the machine's own counters only decide that the pull still moves,
// they are not passed off as the pull's bytes.
//
// It is optional (PullStatusAPI): an older runner answers 501 and the caller
// shows its fixed "pulling from the public sources" line as before.
const PathImagePullStatus = "/v1/images/pull/status" // POST PullStatusRequest -> PullStatus (404 when no pull of it is running)

// PullStatusRequest names the pull by the tag it will create (App and
// DeploymentID of the PullImage request).
type PullStatusRequest struct {
	App          string `json:"app"`
	DeploymentID string `json:"deployment_id"`
}

// PullStatus is one running pull.
type PullStatus struct {
	Ref string `json:"ref"`
	// Phase: probe (measuring the sources before pulling) or pull.
	Phase string `json:"phase"`
	// Source is the host being pulled from (empty while probing);
	// SourceIndex its place in the pull order, from 1, of Sources.
	Source      string `json:"source,omitempty"`
	SourceIndex int    `json:"source_index,omitempty"`
	Sources     int    `json:"sources,omitempty"`
	// Bytes and Total are summed over the layers Docker has announced for
	// this source so far (Total grows as layers are announced).
	Bytes int64 `json:"bytes"`
	Total int64 `json:"total,omitempty"`
	// BytesPerSec is the speed over the last pullRateWindow by Docker's
	// messages (it falls when they stop); 0 when not known yet.
	BytesPerSec int64 `json:"bytes_per_sec,omitempty"`
	// Blind: Docker sends no layer progress now and the machine's network
	// and disk counters say the pull still moves (Bytes is Docker's last
	// figure, how far it really is cannot be seen).
	Blind bool `json:"blind,omitempty"`
	// QuietSeconds is how long no progress message came.
	QuietSeconds int `json:"quiet_seconds,omitempty"`
	// Seconds is how long this image has been pulled (all sources, the
	// probe included); SourceSeconds how long from the current source.
	Seconds       int `json:"seconds"`
	SourceSeconds int `json:"source_seconds,omitempty"`
	// LimitSeconds is the most one image is pulled (pullMaxTotal).
	LimitSeconds int `json:"limit_seconds"`
	// Earlier is what each source tried before this one did (the same
	// sentences as a failed pull's per-source messages).
	Earlier []string `json:"earlier,omitempty"`
}

// PullStatusAPI is implemented by *Docker and *Client. ErrNotFound: no pull
// of that tag is running (not begun yet, or over).
type PullStatusAPI interface {
	PullStatus(ctx context.Context, app, deploymentID string) (PullStatus, error)
}

// pullRateWindow is how far back BytesPerSec looks.
const pullRateWindow = 20 * time.Second

// PullMaxTotal is the most one image is pulled over all its sources
// (pullMaxTotal), for callers that bound their own wait on a pull.
const PullMaxTotal = pullMaxTotal

// pullTracker records one pull's status; its methods do nothing on nil (a
// pull nobody asked about: the setup's edge image, tests).
type pullTracker struct {
	mu       sync.Mutex
	st       PullStatus
	start    time.Time
	srcStart time.Time
	lastMsg  time.Time
	samples  []speedSample
}

type pullTrackerKey struct{}

func trackerFrom(ctx context.Context) *pullTracker {
	tr, _ := ctx.Value(pullTrackerKey{}).(*pullTracker)
	return tr
}

// trackPull starts recording the pull that creates tag; the returned func
// ends it.
func (d *Docker) trackPull(ctx context.Context, tag, ref string) (context.Context, func()) {
	now := time.Now()
	tr := &pullTracker{st: PullStatus{Ref: ref, Phase: "pull", LimitSeconds: int(pullMaxTotal / time.Second)}, start: now, srcStart: now, lastMsg: now}
	d.pullsMu.Lock()
	if d.pulls == nil {
		d.pulls = map[string]*pullTracker{}
	}
	d.pulls[tag] = tr
	d.pullsMu.Unlock()
	return context.WithValue(ctx, pullTrackerKey{}, tr), func() {
		d.pullsMu.Lock()
		if d.pulls[tag] == tr {
			delete(d.pulls, tag)
		}
		d.pullsMu.Unlock()
	}
}

// PullStatus implements PullStatusAPI.
func (d *Docker) PullStatus(_ context.Context, app, deploymentID string) (PullStatus, error) {
	d.pullsMu.Lock()
	tr := d.pulls[ImageTag(app, deploymentID)]
	d.pullsMu.Unlock()
	if tr == nil {
		return PullStatus{}, ErrNotFound
	}
	return tr.status(time.Now()), nil
}

func (tr *pullTracker) probing() {
	if tr == nil {
		return
	}
	tr.mu.Lock()
	tr.st.Phase = "probe"
	tr.mu.Unlock()
}

// source: the pull goes on from host, number i (from 1) of n.
func (tr *pullTracker) source(host string, i, n int) {
	if tr == nil {
		return
	}
	now := time.Now()
	tr.mu.Lock()
	tr.st.Phase, tr.st.Source, tr.st.SourceIndex, tr.st.Sources = "pull", host, i, n
	tr.st.Bytes, tr.st.Total, tr.st.Blind = 0, 0, false
	tr.srcStart, tr.lastMsg, tr.samples = now, now, nil
	tr.mu.Unlock()
}

// progress: Docker's messages say done of total bytes for this source.
func (tr *pullTracker) progress(done, total int64) {
	if tr == nil {
		return
	}
	now := time.Now()
	tr.mu.Lock()
	tr.st.Bytes, tr.st.Total, tr.st.Blind = done, total, false
	tr.lastMsg = now
	tr.samples = append(tr.samples, speedSample{now, done})
	cut := now.Add(-pullRateWindow)
	i := 0
	for i+1 < len(tr.samples) && !tr.samples[i+1].at.After(cut) {
		i++
	}
	if i > 0 {
		tr.samples = append(tr.samples[:0], tr.samples[i:]...)
	}
	tr.mu.Unlock()
}

// blind: Docker's messages stopped and the machine's counters say the pull
// moves (pullWatched); a progress message ends it.
func (tr *pullTracker) blind() {
	if tr == nil {
		return
	}
	tr.mu.Lock()
	tr.st.Blind = true
	tr.mu.Unlock()
}

// gaveUp records what the current source did before the next one is tried.
func (tr *pullTracker) gaveUp(msg string) {
	if tr == nil {
		return
	}
	tr.mu.Lock()
	tr.st.Earlier = append(tr.st.Earlier, msg)
	tr.mu.Unlock()
}

func (tr *pullTracker) status(now time.Time) PullStatus {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	s := tr.st
	s.Earlier = slices.Clone(tr.st.Earlier)
	s.Seconds = int(now.Sub(tr.start) / time.Second)
	if s.Phase == "pull" && s.Source != "" {
		s.SourceSeconds = int(now.Sub(tr.srcStart) / time.Second)
		s.QuietSeconds = int(now.Sub(tr.lastMsg) / time.Second)
	}
	// The speed from the newest sample at or before the window's start (or
	// the source's start, at 0 bytes) to now: it falls while no message
	// comes, instead of showing the last burst's speed.
	if len(tr.samples) > 0 && !s.Blind {
		base := speedSample{tr.srcStart, 0}
		from := now.Add(-pullRateWindow)
		for _, x := range tr.samples {
			if !x.at.After(from) {
				base = x
			}
		}
		last := tr.samples[len(tr.samples)-1]
		if el := now.Sub(base.at).Seconds(); el >= 1 && last.bytes > base.bytes {
			s.BytesPerSec = int64(float64(last.bytes-base.bytes) / el)
		}
	}
	return s
}

// ---- Client ----

// PullStatus implements PullStatusAPI.
func (c *Client) PullStatus(ctx context.Context, app, deploymentID string) (PullStatus, error) {
	var out PullStatus
	err := c.call(ctx, c.standard, PathImagePullStatus, PullStatusRequest{App: app, DeploymentID: deploymentID}, clientDefaultBound, &out)
	return out, err
}

// ---- Server ----

func (s *Server) handlePullStatus(w http.ResponseWriter, r *http.Request) {
	var req PullStatusRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	if !validAppName(w, req.App) {
		return
	}
	if !ValidDeploymentID(req.DeploymentID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid deployment id")
		return
	}
	ps, ok := s.api.(PullStatusAPI)
	if !ok {
		writeError(w, http.StatusNotImplemented, "unsupported", "this runner cannot report a pull's progress")
		return
	}
	st, err := ps.PullStatus(r.Context(), req.App, req.DeploymentID)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}
