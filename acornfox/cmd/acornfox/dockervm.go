package main

import (
	"context"
	"sync"
	"time"

	dockerclient "github.com/moby/moby/client"

	"github.com/acornfox/acornfox/internal/apiserver"
	"github.com/acornfox/acornfox/internal/runner"
)

// dockerVMHost reports "host" metrics on macOS and Windows, where Docker runs
// in a virtual machine: cores and memory come from docker info (the VM), CPU
// and memory use are the sum over AcornFox's running containers. Disk is not
// reported (the VM disk size is not exposed by the Docker API). Results are
// cached for a few seconds so the console's polling stays cheap.
type dockerVMHost struct {
	cli    func() *dockerclient.Client // follows runner.Docker.SetClient
	runner runner.API

	mu   sync.Mutex
	at   time.Time
	last apiserver.HostView
}

func newDockerVMHost(cli func() *dockerclient.Client, r runner.API) *dockerVMHost {
	return &dockerVMHost{cli: cli, runner: r}
}

const dockerVMCacheFor = 5 * time.Second

func (h *dockerVMHost) Host(ctx context.Context) apiserver.HostView {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.at.IsZero() && time.Since(h.at) < dockerVMCacheFor {
		return h.last
	}
	h.last, h.at = h.sample(ctx), time.Now()
	return h.last
}

func (h *dockerVMHost) sample(ctx context.Context) apiserver.HostView {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	info, err := h.cli().Info(ctx, dockerclient.InfoOptions{})
	if err != nil {
		return apiserver.HostView{Available: false, Source: "docker_vm"}
	}
	cores := info.Info.NCPU
	total := uint64(info.Info.MemTotal)
	v := apiserver.HostView{Available: true, Source: "docker_vm", CPUCores: &cores, MemoryTotal: &total}
	var used uint64
	var cpu float64
	if cs, err := h.runner.ListContainers(ctx, ""); err == nil {
		for _, c := range cs {
			if !c.Running {
				continue
			}
			st, err := h.runner.ContainerStats(ctx, c.App, c.Name)
			if err != nil {
				continue
			}
			used += uint64(st.MemoryUsageMB * (1 << 20))
			if st.CPUAvailable {
				cpu += st.CPUPercent
			}
		}
	}
	v.MemoryUsed = &used
	if total > 0 {
		pct := float64(used) / float64(total) * 100
		v.MemoryPercent = &pct
	}
	if cores > 0 {
		// Docker's per-container CPU percentage is relative to one core.
		pct := cpu / float64(cores)
		v.CPUPercent = &pct
	}
	return v
}
