package apiserver

// GET /v1/metrics/host/history: the host's CPU, memory and disk over the last
// hour or day, for the console's home dashboard. The history lives in memory
// only (a restart starts it over; "since" says when it starts) and is sampled
// by its own HostProvider every 30 seconds, so the CPU figure of each point is
// the average of those 30 seconds and requests never change it. There is no
// 7-day range: nothing is kept on disk.

import (
	"context"
	"net/http"
	"sync"
	"time"
)

const (
	hostHistoryInterval = 30 * time.Second
	hostHistoryKeep     = 24 * time.Hour
	// hostHistoryCap is one day of 30-second samples.
	hostHistoryCap = int(hostHistoryKeep / hostHistoryInterval)
)

// hostRanges are the ranges the history answers: the window and the step of
// the returned points (raw samples for 1h, 5-minute averages for 24h).
var hostRanges = []struct {
	Name   string
	Window time.Duration
	Step   time.Duration
}{
	{"1h", time.Hour, hostHistoryInterval},
	{"24h", 24 * time.Hour, 5 * time.Minute},
}

// HostPoint is one sample (or one averaged step) of the host history. A
// metric the provider could not read is left out.
type HostPoint struct {
	T           time.Time `json:"t"`
	CPUPercent  *float64  `json:"cpu_percent,omitempty"`
	MemoryUsed  *uint64   `json:"memory_used,omitempty"`
	MemoryTotal *uint64   `json:"memory_total,omitempty"`
	DiskUsed    *uint64   `json:"disk_used,omitempty"`
	DiskTotal   *uint64   `json:"disk_total,omitempty"`
}

// HostHistory is the in-memory ring of host samples. Build it with
// NewHostHistory, start Run once in the server process and pass it to
// apiserver.Config; both the trusted and the console handler read the same one.
type HostHistory struct {
	src      HostProvider
	interval time.Duration
	now      func() time.Time

	mu    sync.Mutex
	buf   []HostPoint // ring, oldest at start
	start int
	n     int
	since time.Time
}

// NewHostHistory samples src. src should be its own provider instance (the
// /proc provider computes CPU from the previous call).
func NewHostHistory(src HostProvider) *HostHistory {
	return &HostHistory{src: src, interval: hostHistoryInterval, now: time.Now, buf: make([]HostPoint, hostHistoryCap)}
}

// Run samples now and then every 30 seconds until ctx is done.
func (h *HostHistory) Run(ctx context.Context) {
	if h == nil {
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

func (h *HostHistory) sample(ctx context.Context) {
	v := h.src.Host(ctx)
	if !v.Available {
		return
	}
	p := HostPoint{T: h.now().UTC(), CPUPercent: v.CPUPercent, MemoryUsed: v.MemoryUsed, MemoryTotal: v.MemoryTotal, DiskUsed: v.DiskUsed, DiskTotal: v.DiskTotal}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n == 0 && h.since.IsZero() {
		h.since = p.T
	}
	if h.n < len(h.buf) {
		h.buf[(h.start+h.n)%len(h.buf)] = p
		h.n++
		return
	}
	h.buf[h.start] = p
	h.start = (h.start + 1) % len(h.buf)
	h.since = h.buf[h.start].T
}

// points returns the samples newer than cutoff, oldest first.
func (h *HostHistory) points(cutoff time.Time) []HostPoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]HostPoint, 0, h.n)
	for i := 0; i < h.n; i++ {
		p := h.buf[(h.start+i)%len(h.buf)]
		if p.T.After(cutoff) {
			out = append(out, p)
		}
	}
	return out
}

// average folds samples into one point per step (aligned to the step), each
// metric averaged over the samples that have it; totals take the last value.
func average(pts []HostPoint, step time.Duration) []HostPoint {
	out := []HostPoint{}
	type acc struct {
		t                   time.Time
		cpu, mem, disk      float64
		nCPU, nMem, nDisk   int
		memTotal, diskTotal *uint64
	}
	var cur *acc
	flush := func() {
		if cur == nil {
			return
		}
		p := HostPoint{T: cur.t, MemoryTotal: cur.memTotal, DiskTotal: cur.diskTotal}
		if cur.nCPU > 0 {
			v := cur.cpu / float64(cur.nCPU)
			p.CPUPercent = &v
		}
		if cur.nMem > 0 {
			v := uint64(cur.mem / float64(cur.nMem))
			p.MemoryUsed = &v
		}
		if cur.nDisk > 0 {
			v := uint64(cur.disk / float64(cur.nDisk))
			p.DiskUsed = &v
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
		if p.MemoryUsed != nil {
			cur.mem += float64(*p.MemoryUsed)
			cur.nMem++
		}
		if p.DiskUsed != nil {
			cur.disk += float64(*p.DiskUsed)
			cur.nDisk++
		}
		if p.MemoryTotal != nil {
			cur.memTotal = p.MemoryTotal
		}
		if p.DiskTotal != nil {
			cur.diskTotal = p.DiskTotal
		}
	}
	flush()
	return out
}

// hostHistoryView is the wire shape of GET /v1/metrics/host/history.
type hostHistoryView struct {
	Range       string      `json:"range"`
	StepSeconds int         `json:"step_seconds"`
	Ranges      []string    `json:"ranges"`          // ranges this server answers; empty => no history
	Since       *time.Time  `json:"since,omitempty"` // oldest sample kept (history restarts with the service)
	Points      []HostPoint `json:"points"`
}

// getHostHistory implements GET /v1/metrics/host/history?range=1h|24h
// (default 1h). Without a history it answers 200 with no ranges.
func (s *server) getHostHistory(w http.ResponseWriter, r *http.Request) {
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
	hr := hostRanges[idx]
	v := hostHistoryView{Range: hr.Name, StepSeconds: int(hr.Step / time.Second), Ranges: []string{}, Points: []HostPoint{}}
	h := s.hostHistory
	if h == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	for _, x := range hostRanges {
		v.Ranges = append(v.Ranges, x.Name)
	}
	pts := h.points(h.now().Add(-hr.Window))
	if hr.Step > h.interval {
		pts = average(pts, hr.Step)
	}
	v.Points = pts
	h.mu.Lock()
	if h.n > 0 {
		since := h.since
		v.Since = &since
	}
	h.mu.Unlock()
	writeJSON(w, http.StatusOK, v)
}

// NewHostProvider returns a fresh /proc host provider for dataDir (Linux; on
// Windows it reports unavailable). HostHistory needs its own instance.
func NewHostProvider(dataDir string) HostProvider { return newProcHostProvider(dataDir) }
