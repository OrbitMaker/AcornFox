package reconcile

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/acornfox/acornfox/internal/caddyroute"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// EdgeConfig turns on the edge container (three-platform architecture): the
// reconciler keeps one Caddy container running through the runner and routes
// apps through it. Nil keeps the v0.3 behaviour (a Caddy the installer runs
// on the host).
type EdgeConfig struct {
	Mode       string // runner.EdgeModeHost (Linux) | runner.EdgeModeBridge (macOS, Windows)
	AdminPort  int
	AdminToken string

	// Bridge mode only: the reserved app port range published once
	// and the LAN address the second copy is
	// bound to (nil or "" => local only).
	PortMin, PortMax int
	LANHost          func() string
}

// LANPortOffset maps <LAN address>:P to container port P+LANPortOffset in
// bridge mode, so an app's server can listen for LAN clients separately.
const LANPortOffset = 10000

// Edge states reported by EdgeStatus (GET /v1/status, console banner).
const (
	EdgeStateOff       = ""          // no edge configured (v0.3 host Caddy)
	EdgeStatePreparing = "preparing" // creating the container, maybe downloading the image
	EdgeStateRunning   = "running"
	EdgeStateError     = "error"
)

func (r *Reconciler) setEdgeState(state, msg string) {
	r.edgeMu.Lock()
	r.edgeState, r.edgeMsg = state, msg
	r.edgeMu.Unlock()
}

// EdgeStatus reports the edge container's state and, for errors, why.
func (r *Reconciler) EdgeStatus() (state, message string) {
	if r.cfg.Edge == nil {
		return EdgeStateOff, ""
	}
	r.edgeMu.Lock()
	defer r.edgeMu.Unlock()
	if r.edgeState == "" {
		return EdgeStatePreparing, ""
	}
	return r.edgeState, r.edgeMsg
}

// edgeReadyTimeout bounds the wait for a (re)created Caddy to accept admin
// requests before routes are pushed.
const edgeReadyTimeout = 20 * time.Second

func (r *Reconciler) edgeManager() runner.EdgeManager {
	if r.cfg.Edge == nil {
		return nil
	}
	m, _ := r.cfg.Runner.(runner.EdgeManager)
	return m
}

func (r *Reconciler) bridgeMode() bool {
	return r.cfg.Edge != nil && r.cfg.Edge.Mode == runner.EdgeModeBridge
}

// exposureOf resolves an app's exposure, "" meaning the platform default.
func (r *Reconciler) exposureOf(a state.App) string {
	if a.Exposure != "" {
		return a.Exposure
	}
	if r.cfg.DefaultExposure != "" {
		return r.cfg.DefaultExposure
	}
	return state.ExposurePublic
}

// appRoute builds the route of one app from its live container. In bridge
// mode Caddy reaches the container by name on the app network; otherwise it
// uses the container's loopback port. Exposure "local" binds Caddy to
// 127.0.0.1 in host mode; in bridge mode the edge's port publishing decides.
func (r *Reconciler) appRoute(a state.App, ci runner.ContainerInfo) (caddyroute.Route, bool) {
	if ci.HostPort == 0 {
		return caddyroute.Route{}, false
	}
	rt := caddyroute.Route{App: a.Name, PublicPort: a.PublicPort, Upstream: fmt.Sprintf("127.0.0.1:%d", ci.HostPort)}
	if r.bridgeMode() && ci.Port > 0 {
		rt.Upstream = fmt.Sprintf("%s:%d", ci.Name, ci.Port)
	}
	if r.bridgeMode() && r.exposureOf(a) != state.ExposureLocal {
		rt.LANPort = a.PublicPort + LANPortOffset
	}
	if !r.bridgeMode() && r.exposureOf(a) == state.ExposureLocal {
		rt.Listen = "127.0.0.1"
	}
	return rt, true
}

// edgeSpec computes the edge container spec. In bridge mode it publishes the
// whole reserved range on 127.0.0.1 and again on the LAN address (to
// P+LANPortOffset), plus 80/443 once any app has a domain; it does not depend
// on which apps exist or their exposure, so those changes never recreate it.
func (r *Reconciler) edgeSpec(_ context.Context, routes []caddyroute.Route) runner.EdgeSpec {
	e := r.cfg.Edge
	spec := runner.EdgeSpec{Mode: e.Mode, AdminPort: e.AdminPort, AdminToken: e.AdminToken}
	if spec.Mode != runner.EdgeModeBridge || e.PortMin <= 0 || e.PortMax < e.PortMin {
		return spec
	}
	lan := ""
	if e.LANHost != nil {
		if ip, err := netip.ParseAddr(e.LANHost()); err == nil && ip.Is4() && ip.IsPrivate() {
			lan = ip.String()
		}
	}
	for p := e.PortMin; p <= e.PortMax; p++ {
		spec.Ports = append(spec.Ports, runner.EdgePort{HostIP: "127.0.0.1", Port: p})
		if lan != "" {
			spec.Ports = append(spec.Ports, runner.EdgePort{HostIP: lan, Port: p, ContainerPort: p + LANPortOffset})
		}
	}
	for _, rt := range routes {
		if len(rt.Domains) > 0 {
			for _, p := range []int{r.cfg.HTTPS.HTTPPort, r.cfg.HTTPS.HTTPSPort} {
				if p > 0 {
					spec.Ports = append(spec.Ports, runner.EdgePort{HostIP: "0.0.0.0", Port: p})
				}
			}
			break
		}
	}
	return spec
}

func specKey(s runner.EdgeSpec) string {
	k := fmt.Sprintf("%s|%d", s.Mode, s.AdminPort)
	for _, p := range s.Ports {
		k += fmt.Sprintf("|%s:%d>%d", p.HostIP, p.Port, p.ContainerPort)
	}
	return k
}

// ensureEdge makes the edge container match routes. It returns true when the
// container was (re)created, in which case Caddy has no routes yet and the
// caller must push them. A no-op without EdgeConfig.
func (r *Reconciler) ensureEdge(ctx context.Context, routes []caddyroute.Route, force bool) (bool, error) {
	m := r.edgeManager()
	if m == nil {
		return false, nil
	}
	spec := r.edgeSpec(ctx, routes)
	key := specKey(spec)
	// One caller at a time: app rounds and the startup check run
	// concurrently, and two creates of the same container would collide.
	r.edgeOpMu.Lock()
	defer r.edgeOpMu.Unlock()
	r.edgeMu.Lock()
	same := r.edgeKey == key
	r.edgeMu.Unlock()
	recreated := false
	if !same || force {
		if r.edgeKey == "" {
			r.setEdgeState(EdgeStatePreparing, "")
			r.log.Info("preparing edge container (the first start may download the Caddy image)")
		}
		info, err := m.EnsureEdge(ctx, spec)
		if err != nil {
			r.edgeMu.Lock()
			r.edgeFailures++
			n := r.edgeFailures
			r.edgeMu.Unlock()
			r.setEdgeState(EdgeStateError, err.Error())
			r.log.Error("edge container", "attempt", n, "err", err)
			return false, fmt.Errorf("入口容器（Caddy）启动失败：%w", err)
		}
		r.edgeMu.Lock()
		r.edgeFailures = 0
		r.edgeMu.Unlock()
		r.setEdgeState(EdgeStateRunning, "")
		recreated = info.Recreated
		if recreated {
			r.log.Info("edge container created", "image", info.Image, "ports", len(spec.Ports))
			r.waitEdgeReady(ctx)
		}
		r.edgeMu.Lock()
		r.edgeKey = key
		r.edgeMu.Unlock()
	}
	if r.bridgeMode() {
		for _, rt := range routes {
			if err := m.ConnectEdge(ctx, rt.App); err != nil {
				r.log.Warn("connect edge to app network", "app", rt.App, "err", err)
			}
		}
	}
	return recreated, nil
}

func (r *Reconciler) waitEdgeReady(ctx context.Context) {
	deadline := time.Now().Add(edgeReadyTimeout)
	for time.Now().Before(deadline) {
		if _, err := r.cfg.Router.Current(ctx); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// EnsureEdge starts the edge container at startup, before any app round, so
// the Caddy admin gateway exists for domain verification and an empty server
// still answers. It returns false when the caller should retry (runner or
// Docker not reachable yet); rounds also retry on their own.
func (r *Reconciler) EnsureEdge(ctx context.Context) bool {
	if r.edgeManager() == nil {
		return true
	}
	if _, err := r.cfg.Runner.Ping(ctx); err != nil {
		return false
	}
	routes, err := r.observedRoutes(ctx)
	if err != nil {
		routes = nil
	}
	if _, err := r.ensureEdge(ctx, routes, true); err != nil {
		r.log.Error("edge", "err", err)
		return false
	}
	// A new Caddy has no routes: push them now rather than on the next round.
	if len(routes) > 0 {
		if err := r.cfg.Router.Sync(ctx, routes); err != nil {
			r.log.Error("route sync", "err", err)
		}
	}
	return true
}
