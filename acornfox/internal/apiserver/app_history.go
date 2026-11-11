package apiserver

// GET /v1/metrics/apps/{app}/history: one app's CPU and memory over the last
// hour or day, for the small charts on the console's app overview and for
// `acornfox stats APP --history`. Same rules as the host history next door:
// sampled every 30 seconds, kept in memory only (a service restart starts it
// over; "since" says when it starts), raw samples for 1h and 5-minute averages
// for 24h, no 7-day range. A stopped app records nothing, so its chart has a
// gap for that time; a deleted app's history is dropped on the next pass.

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

const (
	// appHistoryMaxApps bounds memory: 64 apps x one day of 30-second samples
	// is about 9 MB at most. Apps past the limit (by name order) get no history.
	appHistoryMaxApps = 64
	// appHistoryPassBudget bounds one sampling pass so a slow Docker never
	// makes passes pile up.
	appHistoryPassBudget = 25 * time.Second
)

// AppPoint is one sample (or one averaged step) of an app's history. CPU is
// left out while the runner is still warming up its first reading; the memory
// limit is left out when the container has none.
type AppPoint struct {
	T             time.Time `json:"t"`
	CPUPercent    *float64  `json:"cpu_percent,omitempty"`
	MemoryUsageMB *float64  `json:"memory_usage_mb,omitempty"`
	MemoryLimitMB *float64  `json:"memory_limit_mb,omitempty"`
}

// AppHistoryStore is the part of the store the app history reads.
type AppHistoryStore interface {
	ListApps(ctx context.Context) ([]state.App, error)
}

// AppHistoryRunner is the part of the runner the app history reads.
type AppHistoryRunner interface {
	ContainerStats(ctx context.Context, app, name string) (runner.StatsResponse, error)
}

type appRing struct {
	buf   []AppPoint // grows to hostHistoryCap, then wraps; oldest at start
	start int
}

func (r *appRing) push(p AppPoint) {
	if len(r.buf) < hostHistoryCap {
		r.buf = append(r.buf, p)
		return
	}
	r.buf[r.start] = p
	r.start = (r.start + 1) % len(r.buf)
}

// AppHistory is the in-memory history of every app's CPU and memory. Build it
// with NewAppHistory, start Run once in the server process and pass it to
// apiserver.Config; the trusted and the console handler read the same one.
type AppHistory struct {
	store    AppHistoryStore
	run      AppHistoryRunner
	interval time.Duration
	now      func() time.Time

	mu    sync.Mutex
	rings map[string]*appRing
}

// NewAppHistory samples the current container of every app in store through run.
func NewAppHistory(store AppHistoryStore, run AppHistoryRunner) *AppHistory {
	return &AppHistory{store: store, run: run, interval: hostHistoryInterval, now: time.Now, rings: map[string]*appRing{}}
}

// Run samples now and then every 30 seconds until ctx is done.
func (h *AppHistory) Run(ctx context.Context) {
	if h == nil || h.store == nil || h.run == nil {
		return
	}
	h.sample(ctx)
	t := time.NewTicker(h.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.sample(ctx)
		}
	}
}

// sample records one point for every app whose current container answers
// stats. An app that is stopped, being replaced or unreadable records nothing.
func (h *AppHistory) sample(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, appHistoryPassBudget)
	defer cancel()
	apps, err := h.store.ListApps(ctx)
	if err != nil {
		return
	}
	alive := make(map[string]bool, len(apps))
	tracked := 0
	for _, a := range apps {
		alive[a.Name] = true
		if a.CurrentDeployment == "" || tracked >= appHistoryMaxApps {
			continue
		}
		tracked++
		st, err := h.run.ContainerStats(ctx, a.Name, runner.ContainerName(a.Name, a.CurrentDeployment))
		if err != nil {
			continue
		}
		p := AppPoint{T: h.now().UTC()}
		mem := st.MemoryUsageMB
		p.MemoryUsageMB = &mem
		if st.CPUAvailable {
			cpu := st.CPUPercent
			p.CPUPercent = &cpu
		}
		if st.MemoryLimitMB > 0 {
			lim := st.MemoryLimitMB
			p.MemoryLimitMB = &lim
		}
		h.mu.Lock()
		r := h.rings[a.Name]
		if r == nil {
			r = &appRing{}
			h.rings[a.Name] = r
		}
		r.push(p)
		h.mu.Unlock()
	}
	h.mu.Lock()
	for name := range h.rings {
		if !alive[name] {
			delete(h.rings, name)
		}
	}
	h.mu.Unlock()
}

// points returns app's samples newer than cutoff, oldest first, and the time
// of the oldest sample kept (zero when there is none).
func (h *AppHistory) points(app string, cutoff time.Time) ([]AppPoint, time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rings[app]
	if r == nil || len(r.buf) == 0 {
		return []AppPoint{}, time.Time{}
	}
	out := make([]AppPoint, 0, len(r.buf))
	for i := 0; i < len(r.buf); i++ {
		p := r.buf[(r.start+i)%len(r.buf)]
		if p.T.After(cutoff) {
			out = append(out, p)
		}
	}
	return out, r.buf[r.start].T
}

// averageApp folds samples into one point per step (aligned to the step), each
// metric averaged over the samples that have it; the limit takes the last value.
func averageApp(pts []AppPoint, step time.Duration) []AppPoint {
	out := []AppPoint{}
	type acc struct {
		t          time.Time
		cpu, mem   float64
		nCPU, nMem int
		limit      *float64
	}
	var cur *acc
	flush := func() {
		if cur == nil {
			return
		}
		p := AppPoint{T: cur.t, MemoryLimitMB: cur.limit}
		if cur.nCPU > 0 {
			v := cur.cpu / float64(cur.nCPU)
			p.CPUPercent = &v
		}
		if cur.nMem > 0 {
			v := cur.mem / float64(cur.nMem)
			p.MemoryUsageMB = &v
		}
		out = append(out, p)
	}
	for _, p := range pts {
		t := p.T.Truncate(step)
		if cur == nil || !cur.t.Equal(t) {
			flush()
			cur = &acc{t: t}
		}
		if p.CPUPercent != nil {
			cur.cpu += *p.CPUPercent
			cur.nCPU++
		}
		if p.MemoryUsageMB != nil {
			cur.mem += *p.MemoryUsageMB
			cur.nMem++
		}
		if p.MemoryLimitMB != nil {
			cur.limit = p.MemoryLimitMB
		}
	}
	flush()
	return out
}

// appHistoryView is the wire shape of GET /v1/metrics/apps/{app}/history.
type appHistoryView struct {
	App         string     `json:"app"`
	Range       string     `json:"range"`
	StepSeconds int        `json:"step_seconds"`
	Ranges      []string   `json:"ranges"`          // ranges this server answers; empty => no history
	Since       *time.Time `json:"since,omitempty"` // oldest sample kept for this app
	Points      []AppPoint `json:"points"`
}

// getAppHistory implements GET /v1/metrics/apps/{app}/history?range=1h|24h
// (default 1h). Read-only. Errors: 400 invalid_app / invalid_range, 404 when
// the app does not exist. Without a history it answers 200 with no ranges.
func (s *server) getAppHistory(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	if !state.ValidAppName(app) {
		writeError(w, http.StatusBadRequest, "invalid_app", "应用名不合法")
		return
	}
	name := r.URL.Query().Get("range")
	if name == "" {
		name = "1h"
	}
	idx := -1
	for i, hr := range hostRanges {
		if hr.Name == name {
			idx = i
		}
	}
	if idx < 0 {
		writeError(w, http.StatusBadRequest, "invalid_range", "range 只能是 1h 或 24h（历史只存在内存里，没有 7 天）")
		return
	}
	if _, err := s.store.GetApp(r.Context(), app); err != nil {
		s.mapStoreError(w, err)
		return
	}
	hr := hostRanges[idx]
	v := appHistoryView{App: app, Range: hr.Name, StepSeconds: int(hr.Step / time.Second), Ranges: []string{}, Points: []AppPoint{}}
	h := s.appHistory
	if h == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	for _, x := range hostRanges {
		v.Ranges = append(v.Ranges, x.Name)
	}
	pts, since := h.points(app, h.now().Add(-hr.Window))
	if hr.Step > h.interval {
		pts = averageApp(pts, hr.Step)
	}
	v.Points = pts
	if !since.IsZero() {
		v.Since = &since
	}
	writeJSON(w, http.StatusOK, v)
}
