package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// The containers on this machine that AcornFox does not manage: the ones the
// user started with docker run, docker compose or another panel. They are
// listed read-only so the console and `acornfox other-containers` can show
// what else is using the machine. Nothing here starts, stops, changes or
// removes a container, and nothing reads more than the allowlisted facts of
// OtherContainer (no environment, command, label, mount or network name).
//
// This is the one runner route that looks at Docker objects without
// LabelManaged=1, on purpose and only to read: the runner still refuses every
// operation on an unmanaged object.

// PathContainersOthers: POST OtherContainersRequest -> OtherContainersResponse.
const PathContainersOthers = "/v1/containers/others"

// Bounds of one listing.
const (
	// OtherContainersMax caps how many containers one answer lists.
	OtherContainersMax = 200
	// otherStatsMax caps how many running containers get a stats sample.
	otherStatsMax = 60
	// otherStatsParallel is how many stats samples are read at once.
	otherStatsParallel = 6
	// otherStatsTimeout bounds one container's stats read.
	otherStatsTimeout = 4 * time.Second
	// otherListTimeout bounds the whole listing on the client side.
	otherListTimeout = 20 * time.Second
)

// OtherPort is one published port of an unmanaged container.
type OtherPort struct {
	Host      int    `json:"host"`      // port on this machine
	Container int    `json:"container"` // port inside the container
	Proto     string `json:"proto"`     // tcp | udp | sctp
}

// OtherContainer is one container AcornFox does not manage.
type OtherContainer struct {
	ID    string `json:"id"` // the first 12 characters
	Name  string `json:"name"`
	Image string `json:"image"` // as the user wrote it (nginx:1.27)
	// State is Docker's: created | running | restarting | paused | exited | dead | removing.
	State   string `json:"state"`
	Running bool   `json:"running"`
	Created int64  `json:"created"` // unix seconds
	// Ports are the ports published on this machine, once each (an address
	// family is not told apart), ascending. HostNetwork: it shares this
	// machine's network, so it publishes nothing and Ports says nothing.
	Ports       []OtherPort `json:"ports"`
	HostNetwork bool        `json:"host_network,omitempty"`
	// StatsAvailable: the usage below was read (running containers, when
	// asked for, up to otherStatsMax of them). CPUAvailable: a CPU figure
	// could be worked out from two samples.
	StatsAvailable bool    `json:"stats_available"`
	CPUAvailable   bool    `json:"cpu_available"`
	CPUPercent     float64 `json:"cpu_percent"`
	MemoryUsageMB  float64 `json:"memory_usage_mb"`
}

// OtherContainersRequest: Stats asks for memory and CPU of the running ones
// (one Docker call each); without it only names, states and ports come back.
type OtherContainersRequest struct {
	Stats bool `json:"stats"`
}

// OtherContainersResponse: Truncated is true when more than OtherContainersMax
// containers exist (the first ones by name are listed).
type OtherContainersResponse struct {
	Containers []OtherContainer `json:"containers"`
	Truncated  bool             `json:"truncated,omitempty"`
}

// OtherContainerLister is optional, like LogTailer: it is not part of API so
// that other API implementations need not provide it. Docker and Client
// implement it; a Server whose API lacks it answers 501 unsupported.
type OtherContainerLister interface {
	ListOtherContainers(ctx context.Context, req OtherContainersRequest) (OtherContainersResponse, error)
}

// isPlatformContainer reports whether a container is one AcornFox itself
// created or manages: an app, an add-on or a helper (LabelManaged=1), or the
// entry proxy and the one it replaced (LabelEdge=1, or its fixed names).
// Everything else is "other".
func isPlatformContainer(labels map[string]string, names []string) bool {
	if labels[LabelManaged] == "1" || labels[LabelEdge] == "1" {
		return true
	}
	for _, n := range names {
		n = strings.TrimPrefix(n, "/")
		if n == EdgeContainerName || strings.HasPrefix(n, EdgeOldPrefix) {
			return true
		}
	}
	return false
}

// otherPorts turns Docker's port list into the published ports, one per
// (host port, container port, protocol), ascending.
func otherPorts(ports []container.PortSummary) []OtherPort {
	type key struct {
		host, cont int
		proto      string
	}
	seen := map[key]bool{}
	out := []OtherPort{}
	for _, p := range ports {
		if p.PublicPort == 0 {
			continue
		}
		k := key{int(p.PublicPort), int(p.PrivatePort), p.Type}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, OtherPort{Host: k.host, Container: k.cont, Proto: k.proto})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		if out[i].Container != out[j].Container {
			return out[i].Container < out[j].Container
		}
		return out[i].Proto < out[j].Proto
	})
	return out
}

// otherFromSummary maps one Docker list entry; ok is false for the platform's
// own containers.
func otherFromSummary(s container.Summary) (OtherContainer, bool) {
	if isPlatformContainer(s.Labels, s.Names) {
		return OtherContainer{}, false
	}
	name := ""
	if len(s.Names) > 0 {
		name = strings.TrimPrefix(s.Names[0], "/")
	}
	id := s.ID
	if len(id) > 12 {
		id = id[:12]
	}
	if name == "" {
		name = id
	}
	state := strings.ToLower(string(s.State))
	return OtherContainer{
		ID:          id,
		Name:        name,
		Image:       s.Image,
		State:       state,
		Running:     state == "running",
		Created:     s.Created,
		Ports:       otherPorts(s.Ports),
		HostNetwork: s.HostConfig.NetworkMode == "host",
	}, true
}

// ListOtherContainers implements OtherContainerLister. It lists every
// container, keeps the ones that are not the platform's, and (when asked)
// reads one stats sample for each running one, a few at a time. A container
// whose stats cannot be read in time is listed without usage; that never
// fails the listing.
func (d *Docker) ListOtherContainers(ctx context.Context, req OtherContainersRequest) (OtherContainersResponse, error) {
	res, err := d.c().ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return OtherContainersResponse{}, err
	}
	list := []OtherContainer{}
	for _, s := range res.Items {
		if c, ok := otherFromSummary(s); ok {
			list = append(list, c)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	out := OtherContainersResponse{Containers: list}
	if len(list) > OtherContainersMax {
		out.Containers, out.Truncated = list[:OtherContainersMax], true
	}
	if !req.Stats {
		return out, nil
	}
	sem := make(chan struct{}, otherStatsParallel)
	var wg sync.WaitGroup
	sampled := 0
	for i := range out.Containers {
		c := &out.Containers[i]
		if !c.Running || sampled >= otherStatsMax {
			continue
		}
		sampled++
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			sctx, cancel := context.WithTimeout(ctx, otherStatsTimeout)
			defer cancel()
			st, ok := d.otherStats(sctx, c.ID)
			if !ok {
				return
			}
			c.StatsAvailable = true
			c.CPUAvailable, c.CPUPercent, c.MemoryUsageMB = st.CPUAvailable, st.CPUPercent, st.MemoryUsageMB
		}()
	}
	wg.Wait()
	return out, nil
}

// otherStats reads one stats sample of a running container by its ID.
func (d *Docker) otherStats(ctx context.Context, id string) (StatsResponse, bool) {
	stats, err := d.c().ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
	if err != nil {
		return StatsResponse{}, false
	}
	defer stats.Body.Close()
	var v container.StatsResponse
	if err := json.NewDecoder(stats.Body).Decode(&v); err != nil {
		return StatsResponse{}, false
	}
	return containerStatsView(v), true
}

// ListOtherContainers implements OtherContainerLister over the socket. A
// runner without the route (an older version) yields an error matching
// ErrUnsupported.
func (c *Client) ListOtherContainers(ctx context.Context, req OtherContainersRequest) (OtherContainersResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, otherListTimeout)
	defer cancel()
	var out OtherContainersResponse
	err := c.call(ctx, c.standard, PathContainersOthers, req, clientLargeBound, &out)
	return out, unsupportedRoute(err)
}

func (s *Server) handleContainersOthers(w http.ResponseWriter, r *http.Request) {
	var req OtherContainersRequest
	if err := decode(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed request body")
		return
	}
	lister, ok := s.api.(OtherContainerLister)
	if !ok {
		writeError(w, http.StatusNotImplemented, "unsupported", "listing other containers not supported")
		return
	}
	res, err := lister.ListOtherContainers(r.Context(), req)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if res.Containers == nil {
		res.Containers = []OtherContainer{}
	}
	writeJSON(w, http.StatusOK, res)
}

var (
	_ OtherContainerLister = (*Client)(nil)
	_ OtherContainerLister = (*Docker)(nil)
)
