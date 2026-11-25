package apiserver

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/acornfox/acornfox/internal/runner"
)

// The containers on this machine that AcornFox does not manage (the user
// started them with docker run, docker compose or another panel), read-only:
//
//	GET /v1/host/containers[?fresh=1]
//	    {observed_at, containers[{id, name, image, state, running, created,
//	     ports[{host, container, proto}], host_network, stats_available,
//	     cpu_available, cpu_percent, memory_usage_mb}], running, stopped,
//	     memory_mb, cpu_percent, truncated}
//
// Nothing here starts, stops, changes or removes a container, and there is no
// such route. The platform's own containers (apps, add-ons, helpers, the
// entry proxy) never appear. memory_mb and cpu_percent add up the running
// ones whose usage was read; the console's home chart counts them as one
// "other containers" slice so the totals match. When Docker cannot be read
// the answer is 503 with the fault code MACHINE-04 (and 404 from a server
// older than this route). Worked out at the moment it is asked, kept for a
// few seconds so the pages that poll it do not each cost a Docker call per
// container; fresh=1 (the command line sends it) skips the kept one, so a
// container started a second ago is there.

// othersCacheTTL is how long one listing is reused.
const othersCacheTTL = 5 * time.Second

// codeOthersUnavailable is the fault code of a listing that could not be read.
const codeOthersUnavailable = "MACHINE-04"

type otherContainersView struct {
	ObservedAt string                  `json:"observed_at"`
	Containers []runner.OtherContainer `json:"containers"`
	Running    int                     `json:"running"`
	Stopped    int                     `json:"stopped"`
	MemoryMB   float64                 `json:"memory_mb"`
	CPUPercent float64                 `json:"cpu_percent"`
	Truncated  bool                    `json:"truncated,omitempty"`
}

// othersCache keeps the last listing for each of the two ways to ask (with
// and without usage).
type othersCache struct {
	mu    sync.Mutex
	at    [2]time.Time
	views [2]otherContainersView
}

func (c *othersCache) idx(stats bool) int {
	if stats {
		return 1
	}
	return 0
}

func (s *server) registerOthers(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/host/containers", s.getOtherContainers)
}

func (s *server) getOtherContainers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	v, err := s.otherContainers(r.Context(), true, r.URL.Query().Get("fresh") == "1")
	if err != nil {
		s.log.Debug("other containers", "err", err)
		msg := "读不到这台机器上其他容器：Docker 没有回应，应用不受影响"
		if errors.Is(err, runner.ErrUnsupported) {
			msg = "读不到这台机器上其他容器：后台执行器还是旧版本，升级后重启就好"
		}
		writeError(w, http.StatusServiceUnavailable, codeOthersUnavailable, msg)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// otherContainers lists the containers AcornFox does not manage. withStats
// also reads memory and CPU of the running ones; fresh skips the kept listing
// (the new one is kept). A runner without the capability (a test fake) is
// runner.ErrUnsupported.
func (s *server) otherContainers(ctx context.Context, withStats, fresh bool) (otherContainersView, error) {
	lister, ok := s.runner.(runner.OtherContainerLister)
	if !ok {
		return otherContainersView{}, runner.ErrUnsupported
	}
	i := s.others.idx(withStats)
	s.others.mu.Lock()
	if !fresh && !s.others.at[i].IsZero() && time.Since(s.others.at[i]) < othersCacheTTL {
		v := s.others.views[i]
		s.others.mu.Unlock()
		return v, nil
	}
	s.others.mu.Unlock()

	res, err := lister.ListOtherContainers(ctx, runner.OtherContainersRequest{Stats: withStats})
	if err != nil {
		return otherContainersView{}, err
	}
	v := otherContainersView{
		ObservedAt: time.Now().UTC().Format(time.RFC3339),
		Containers: res.Containers,
		Truncated:  res.Truncated,
	}
	if v.Containers == nil {
		v.Containers = []runner.OtherContainer{}
	}
	for _, c := range v.Containers {
		if c.Running {
			v.Running++
		} else {
			v.Stopped++
		}
		if c.StatsAvailable {
			v.MemoryMB += c.MemoryUsageMB
			if c.CPUAvailable {
				v.CPUPercent += c.CPUPercent
			}
		}
	}
	v.MemoryMB, v.CPUPercent = round1(v.MemoryMB), round1(v.CPUPercent)
	s.others.mu.Lock()
	s.others.at[i], s.others.views[i] = time.Now(), v
	s.others.mu.Unlock()
	return v, nil
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }
