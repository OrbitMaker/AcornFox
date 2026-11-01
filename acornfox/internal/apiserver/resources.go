package apiserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// Server-wide resource caps.
//
// Memory is never over-committed: the memory limits of every app and add-on
// container added together must stay within the memory cap. CPU may be
// over-committed: only each container's own CPU limit must stay within the CPU
// cap, because idle CPU time is shared. Stopped apps keep their share, since
// starting them again must not fail for lack of room.
//
// A cap of 0 in the store means automatic: memory = host total minus a system
// reserve of max(1 GB, 15%); on small hosts (under 2 GB) the cap is twice the
// host instead, i.e. memory may be over-committed 2x (the
// limits are ceilings, most apps use far less, and a 1 GB machine could not
// fit a single app otherwise); CPU = all logical cores. When the host size is unknown and nothing is set there is no
// cap (0 in the view) and only the static bounds of PATCH apply.
const (
	systemReserveMinMB   = 1024
	systemReservePercent = 15
	// smallHostMB and smallHostOvercommit: hosts below 2 GB get a memory cap
	// of 2x their size (automatic and the maximum a user can set).
	smallHostMB         = 2048
	smallHostOvercommit = 2
	// memoryUsageMarginMB is kept above a running app's current usage when its
	// limit is lowered in place, so the change does not get it killed at once.
	memoryUsageMarginMB = 32
)

type allocItem struct {
	App      string `json:"app"`
	Kind     string `json:"kind"` // "app", or the add-on kind (postgres / mysql / redis)
	MemoryMB int    `json:"memory_mb"`
	CPUMilli int    `json:"cpu_milli"`
}

type memoryAlloc struct {
	HostMB      int  `json:"host_mb"`      // 0 when unknown
	CapMB       int  `json:"cap_mb"`       // 0 = no cap
	Auto        bool `json:"auto"`         // cap follows the host automatically
	AllocatedMB int  `json:"allocated_mb"` // sum of all container memory limits
}

type cpuAlloc struct {
	HostMilli   int  `json:"host_milli"`    // 0 when unknown
	CapMilli    int  `json:"cap_milli"`     // 0 = no cap
	Auto        bool `json:"auto"`          // cap follows the host automatically
	MaxAppMilli int  `json:"max_app_milli"` // largest single container CPU limit
}

type resourcesView struct {
	Memory         memoryAlloc `json:"memory"`
	CPU            cpuAlloc    `json:"cpu"`
	Items          []allocItem `json:"items"`
	MinAppMemoryMB int         `json:"min_app_memory_mb"`
	MinAppCPUMilli int         `json:"min_app_cpu_milli"`
}

// autoMemoryCapMB is the automatic memory cap for a host of hostMB.
func autoMemoryCapMB(hostMB int) int {
	if hostMB <= 0 {
		return 0
	}
	if hostMB < smallHostMB {
		return hostMB * smallHostOvercommit
	}
	reserve := hostMB * systemReservePercent / 100
	if reserve < systemReserveMinMB {
		reserve = systemReserveMinMB
	}
	capMB := hostMB - reserve
	if capMB < hostMB/2 {
		capMB = hostMB / 2
	}
	return capMB
}

// maxMemoryCapMB is the largest cap a user may set: the host size, or twice
// it on small hosts (same over-commit as the automatic cap).
func maxMemoryCapMB(hostMB int) int {
	if hostMB > 0 && hostMB < smallHostMB {
		return hostMB * smallHostOvercommit
	}
	return hostMB
}

func appMemoryMB(a state.App) int {
	if a.MemoryMB <= 0 {
		return state.DefaultMemoryMB
	}
	return a.MemoryMB
}

func appCPUMilli(a state.App) int {
	if a.CPUMilli <= 0 {
		return state.DefaultCPUMilli
	}
	return a.CPUMilli
}

// resources computes the current allocation against the caps.
func (s *server) resources(ctx context.Context) (resourcesView, error) {
	limits, err := s.store.GetResourceLimits(ctx)
	if err != nil {
		return resourcesView{}, err
	}
	apps, err := s.store.ListApps(ctx)
	if err != nil {
		return resourcesView{}, err
	}
	v := resourcesView{Items: []allocItem{}, MinAppMemoryMB: minMemoryMB, MinAppCPUMilli: minCPUMilli}

	host := s.hostMetrics.Host(ctx)
	if host.MemoryTotal != nil {
		v.Memory.HostMB = int(*host.MemoryTotal >> 20)
	}
	if host.CPUCores != nil {
		v.CPU.HostMilli = *host.CPUCores * 1000
	}
	if limits.MemoryMB > 0 {
		v.Memory.CapMB = limits.MemoryMB
	} else {
		v.Memory.CapMB, v.Memory.Auto = autoMemoryCapMB(v.Memory.HostMB), true
	}
	if limits.CPUMilli > 0 {
		v.CPU.CapMilli = limits.CPUMilli
	} else {
		v.CPU.CapMilli, v.CPU.Auto = v.CPU.HostMilli, true
	}

	add := func(it allocItem) {
		v.Items = append(v.Items, it)
		v.Memory.AllocatedMB += it.MemoryMB
		if it.CPUMilli > v.CPU.MaxAppMilli {
			v.CPU.MaxAppMilli = it.CPUMilli
		}
	}
	for _, a := range apps {
		add(allocItem{App: a.Name, Kind: "app", MemoryMB: appMemoryMB(a), CPUMilli: appCPUMilli(a)})
		addons, err := s.store.ListAddons(ctx, a.Name)
		if err != nil {
			return resourcesView{}, err
		}
		for _, ad := range addons {
			if spec, ok := runner.AddonSpecFor(ad.Kind); ok {
				add(allocItem{App: a.Name, Kind: ad.Kind, MemoryMB: spec.MemoryMB, CPUMilli: spec.CPUMilli})
			}
		}
	}
	return v, nil
}

// memoryRoomError is the 409 body when a change does not fit under the memory
// cap. need is the extra memory the change asks for.
func memoryRoomError(w http.ResponseWriter, v resourcesView, need int, what string) {
	free := v.Memory.CapMB - v.Memory.AllocatedMB
	if free < 0 {
		free = 0
	}
	writeError(w, http.StatusConflict, "memory_cap_exceeded", fmt.Sprintf(
		"整机分配给应用的内存不够：%s需要 %d MB，上限 %d MB 已分配 %d MB，还剩 %d MB。可以调小其他应用的内存，或在服务器页调大整机上限（acornfox limits --memory）",
		what, need, v.Memory.CapMB, v.Memory.AllocatedMB, free))
}

// checkMemoryRoom reports whether need more MB fit under the memory cap and
// writes the 409 when they do not. No cap means it always fits. Callers hold
// s.resMu so the check and their write are not interleaved with another one.
func (s *server) checkMemoryRoom(w http.ResponseWriter, ctx context.Context, need int, what string) bool {
	v, err := s.resources(ctx)
	if err != nil {
		s.mapStoreError(w, err)
		return false
	}
	if v.Memory.CapMB > 0 && v.Memory.AllocatedMB+need > v.Memory.CapMB {
		memoryRoomError(w, v, need, what)
		return false
	}
	return true
}

// checkNewAppRoom is called before a deployment would create app: a new app
// starts with the default limits, which must fit.
func (s *server) checkNewAppRoom(w http.ResponseWriter, ctx context.Context, app string) bool {
	if _, err := s.store.GetApp(ctx, app); err == nil {
		return true
	}
	return s.checkMemoryRoom(w, ctx, state.DefaultMemoryMB, "新应用 "+app+" ")
}

// getResources implements GET /v1/resources.
func (s *server) getResources(w http.ResponseWriter, r *http.Request) {
	v, err := s.resources(r.Context())
	if err != nil {
		s.mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

type putResourcesRequest struct {
	MemoryMB *int `json:"memory_mb,omitempty"` // 0 = automatic; omitted = unchanged
	CPUMilli *int `json:"cpu_milli,omitempty"` // 0 = automatic; omitted = unchanged
}

// putResources implements PUT /v1/resources: set or reset the server-wide caps.
// A cap may not be set below what is already given out (lower app limits
// first) nor above the host.
func (s *server) putResources(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body putResourcesRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "请求体不是合法 JSON")
		return
	}
	s.resMu.Lock()
	defer s.resMu.Unlock()
	v, err := s.resources(ctx)
	if err != nil {
		s.mapStoreError(w, err)
		return
	}
	limits, err := s.store.GetResourceLimits(ctx)
	if err != nil {
		s.mapStoreError(w, err)
		return
	}
	if m := body.MemoryMB; m != nil {
		switch {
		case *m < 0:
			writeError(w, http.StatusBadRequest, "invalid_memory", "内存上限不能为负数；0 表示自动")
			return
		case *m == 0:
		case *m < minMemoryMB:
			writeError(w, http.StatusBadRequest, "invalid_memory", fmt.Sprintf("内存上限至少 %d MB", minMemoryMB))
			return
		case *m < v.Memory.AllocatedMB:
			writeError(w, http.StatusConflict, "cap_below_allocated", fmt.Sprintf(
				"内存上限不能低于已分配的 %d MB；请先调小应用的内存", v.Memory.AllocatedMB))
			return
		case v.Memory.HostMB > 0 && *m > maxMemoryCapMB(v.Memory.HostMB):
			msg := fmt.Sprintf("内存上限不能超过整机内存 %d MB", v.Memory.HostMB)
			if v.Memory.HostMB < smallHostMB {
				msg = fmt.Sprintf("内存上限不能超过整机内存的 %d 倍（%d MB）", smallHostOvercommit, maxMemoryCapMB(v.Memory.HostMB))
			}
			writeError(w, http.StatusBadRequest, "cap_above_host", msg)
			return
		}
		limits.MemoryMB = *m
	}
	if c := body.CPUMilli; c != nil {
		switch {
		case *c < 0:
			writeError(w, http.StatusBadRequest, "invalid_cpu", "CPU 上限不能为负数；0 表示自动")
			return
		case *c == 0:
		case *c < minCPUMilli:
			writeError(w, http.StatusBadRequest, "invalid_cpu", fmt.Sprintf("CPU 上限至少 %.1f 核", float64(minCPUMilli)/1000))
			return
		case *c < v.CPU.MaxAppMilli:
			writeError(w, http.StatusConflict, "cap_below_allocated", fmt.Sprintf(
				"CPU 上限不能低于单个应用已设的 %.1f 核；请先调小那个应用的 CPU", float64(v.CPU.MaxAppMilli)/1000))
			return
		case v.CPU.HostMilli > 0 && *c > v.CPU.HostMilli:
			writeError(w, http.StatusBadRequest, "cap_above_host", fmt.Sprintf("CPU 上限不能超过整机的 %d 核", v.CPU.HostMilli/1000))
			return
		}
		limits.CPUMilli = *c
	}
	if err := s.store.SetResourceLimits(ctx, limits); err != nil {
		s.mapStoreError(w, err)
		return
	}
	v, err = s.resources(ctx)
	if err != nil {
		s.mapStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
