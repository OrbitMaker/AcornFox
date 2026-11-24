package reconcile

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// EdgeConfig turns on the edge container (three-platform architecture): the
// reconciler keeps the entry proxy's container running through the runner
// and routes apps through it (Config.Proxy). Nil manages no container (the
// tests that only look at routes).
type EdgeConfig struct {
	Mode       string // runner.EdgeModeHost (Linux, also inside WSL on Windows) | runner.EdgeModeBridge (macOS)
	AdminPort  int
	AdminToken string

	// LocalListenAll (host mode, inside WSL on Windows): apps whose exposure
	// is local listen on all interfaces like the others instead of on
	// 127.0.0.1. Who may reach them is decided by Windows: the portproxy
	// rules install-windows.ps1 keeps cover only the apps that are not local.
	// Why: WSL's localhost forwarding keeps a stale mapping when a port that
	// listened on every interface starts listening on 127.0.0.1 only (the edge
	// reloads without a gap), so a local-only app would stop opening on the
	// Windows computer itself.
	LocalListenAll bool

	// UseAdminPort is called when the runner reports the entry container's
	// management endpoint on another port than the one in use (after a
	// switch between two entry containers): point Config.Proxy there.
	UseAdminPort func(port int)
	// OnSwitch is called once the runner switched from an old entry
	// container to a new one (info.Replaced is the old one's ID): write the
	// switch record. Nil writes none.
	OnSwitch func(info runner.EdgeInfo)

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
	EdgeStateOff       = ""          // no edge container managed
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

// edgeReadyTimeout bounds the wait for a (re)created proxy to answer before
// the configuration is handed over. Tests shorten it.
var edgeReadyTimeout = 20 * time.Second

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
// mode the proxy reaches the container by name on the app network; otherwise
// it uses the container's loopback port. Exposure "local" binds the app's
// port to 127.0.0.1 in host mode; in bridge mode the edge's port publishing
// decides.
func (r *Reconciler) appRoute(a state.App, ci runner.ContainerInfo) (edge.Route, bool) {
	if ci.HostPort == 0 {
		return edge.Route{}, false
	}
	rt := edge.Route{App: a.Name, PublicPort: a.PublicPort, Upstream: fmt.Sprintf("127.0.0.1:%d", ci.HostPort)}
	if r.bridgeMode() && ci.Port > 0 {
		rt.Upstream = fmt.Sprintf("%s:%d", ci.Name, ci.Port)
	}
	if r.bridgeMode() && r.exposureOf(a) != state.ExposureLocal {
		rt.LANPort = a.PublicPort + LANPortOffset
	}
	if !r.bridgeMode() && r.exposureOf(a) == state.ExposureLocal && !(r.cfg.Edge != nil && r.cfg.Edge.LocalListenAll) {
		rt.Listen = "127.0.0.1"
	}
	return rt, true
}

// edgeSpec computes the edge container spec. In bridge mode it publishes the
// whole reserved range on 127.0.0.1 and again on the LAN address (to
// P+LANPortOffset), plus 80/443 once any app has a domain; it does not depend
// on which apps exist or their exposure, so those changes never recreate it.
func (r *Reconciler) edgeSpec(_ context.Context, routes []edge.Route) runner.EdgeSpec {
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
	// Docker cannot bind the LAN address (it runs in a VM without it, e.g.
	// WSL 2 in NAT mode): publish the LAN listeners on 127.0.0.1:P+offset
	// instead; acornfox serve listens on <LAN>:P and relays there
	// (lanforward.go), so "local only" apps stay unreachable from the LAN.
	forward := false
	r.edgeMu.Lock()
	if lan != "" && lan == r.lanUnbindable {
		lan, forward = "", true
	}
	r.edgeMu.Unlock()
	for p := e.PortMin; p <= e.PortMax; p++ {
		spec.Ports = append(spec.Ports, runner.EdgePort{HostIP: "127.0.0.1", Port: p})
		if lan != "" {
			spec.Ports = append(spec.Ports, runner.EdgePort{HostIP: lan, Port: p, ContainerPort: p + LANPortOffset})
		}
		if forward {
			spec.Ports = append(spec.Ports, runner.EdgePort{HostIP: "127.0.0.1", Port: p + LANPortOffset, ContainerPort: p + LANPortOffset})
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

// specLANHost returns the LAN address a spec publishes on, or "".
func specLANHost(s runner.EdgeSpec) string {
	for _, p := range s.Ports {
		if p.ContainerPort != 0 && p.HostIP != "127.0.0.1" && p.HostIP != "0.0.0.0" {
			return p.HostIP
		}
	}
	return ""
}

// LANUnbindable reports whether host is a LAN address Docker could not bind
// (the edge then publishes on 127.0.0.1 only; apps have no LAN address).
func (r *Reconciler) LANUnbindable(host string) bool {
	r.edgeMu.Lock()
	defer r.edgeMu.Unlock()
	return host != "" && host == r.lanUnbindable
}

func specKey(s runner.EdgeSpec) string {
	k := fmt.Sprintf("%s|%d", s.Mode, s.AdminPort)
	for _, p := range s.Ports {
		k += fmt.Sprintf("|%s:%d>%d", p.HostIP, p.Port, p.ContainerPort)
	}
	return k
}

// ensureEdge makes the edge container match routes. It returns true when the
// container was (re)created, in which case the proxy has no configuration yet
// and the caller must hand it over. A no-op without EdgeConfig.
func (r *Reconciler) ensureEdge(ctx context.Context, routes []edge.Route, force bool) (bool, error) {
	return r.ensureEdgeWith(ctx, routes, force, false)
}

// ensureEdgeWith is ensureEdge; recreate asks the runner to rebuild the
// container in place even when it matches the spec (runner.EdgeSpec.Recreate).
func (r *Reconciler) ensureEdgeWith(ctx context.Context, routes []edge.Route, force, recreate bool) (bool, error) {
	m := r.edgeManager()
	if m == nil {
		return false, nil
	}
	spec := r.edgeSpec(ctx, routes)
	spec.Recreate = recreate
	key := specKey(spec)
	// A new container gets the whole configuration in its settings volume
	// before it starts (it serves every route at once), and a switch from
	// an old container needs it.
	if cfg, err := r.proxyConfig(ctx, routes); err == nil {
		spec.Config = &cfg
	} else {
		cfg := edge.Config{Routes: routes, HTTPS: r.cfg.HTTPS}
		spec.Config = &cfg
	}
	// One caller at a time: app rounds and the startup check run
	// concurrently, and two creates of the same container would collide.
	// Hand-overs (applyRoutes) take the same lock, so while the runner
	// switches entry containers nothing is sent to either one.
	r.edgeOpMu.Lock()
	defer r.edgeOpMu.Unlock()
	r.edgeMu.Lock()
	same := r.edgeKey == key
	r.edgeMu.Unlock()
	recreated := false
	if !same || force || recreate {
		if r.edgeKey == "" {
			r.setEdgeState(EdgeStatePreparing, "")
			r.log.Info("preparing edge container")
		}
		info, err := m.EnsureEdge(ctx, spec)
		if err != nil && strings.Contains(err.Error(), "cannot assign requested address") {
			// Docker cannot bind the LAN address (it runs in a VM without it,
			// e.g. WSL 2 in NAT mode): publish on 127.0.0.1 only.
			if lan := specLANHost(spec); lan != "" {
				r.edgeMu.Lock()
				r.lanUnbindable = lan
				r.edgeMu.Unlock()
				r.log.Warn("edge: LAN address not bindable by Docker, apps reachable on this computer only", "lan", lan, "err", err)
				spec = r.edgeSpec(ctx, routes)
				spec.Recreate = recreate
				key = specKey(spec)
				info, err = m.EnsureEdge(ctx, spec)
			}
		}
		if err != nil {
			r.edgeMu.Lock()
			r.edgeFailures++
			n := r.edgeFailures
			r.edgeMu.Unlock()
			r.setEdgeState(EdgeStateError, err.Error())
			r.log.Error("edge container", "attempt", n, "err", err)
			return false, fmt.Errorf("入口容器启动失败：%w", err)
		}
		r.edgeMu.Lock()
		r.edgeFailures = 0
		r.edgeMu.Unlock()
		// A kept container still refuses the token: an EDGE-03 state
		// (recreateEdgeIfTokenRefused) stays until a hand-over takes.
		if st, msg := r.EdgeStatus(); info.Recreated || st != EdgeStateError || !strings.Contains(msg, edge.FaultAdmin) {
			r.setEdgeState(EdgeStateRunning, "")
		}
		if info.AdminPort > 0 && r.cfg.Edge.UseAdminPort != nil {
			r.cfg.Edge.UseAdminPort(info.AdminPort)
		}
		if info.Replaced != "" {
			r.log.Info("edge container switched", "old", info.ReplacedName, "old_id", info.Replaced, "new_id", info.ID, "admin_port", info.AdminPort)
			if info.Note != "" {
				r.log.Warn("edge switch", "note", info.Note)
			}
			if r.cfg.Edge.OnSwitch != nil {
				r.cfg.Edge.OnSwitch(info)
			}
		}
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

// waitEdgeReady waits until a (re)created proxy answers Version, which is
// when it can take a configuration.
func (r *Reconciler) waitEdgeReady(ctx context.Context) {
	deadline := time.Now().Add(edgeReadyTimeout)
	for time.Now().Before(deadline) {
		if _, err := r.cfg.Proxy.Version(ctx); err == nil {
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
// the proxy's management endpoint exists for domain verification and an
// empty proxy still answers. It returns false when the caller should retry (runner or
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
	// A container that refuses the token in the data directory (EDGE-03)
	// is rebuilt in place with it, once per run of the server.
	if err := r.recreateEdgeIfTokenRefused(ctx, routes); err != nil {
		r.log.Error("edge", "err", err)
		return false
	}
	// A new proxy has no configuration: hand it over now rather than on the
	// next round.
	if len(routes) > 0 {
		if err := r.applyRoutes(ctx, routes); err != nil {
			r.log.Error("route sync", "err", err)
		}
	}
	return true
}

// Token refusal (EDGE-03). The entry container reads its management token
// from a file the runner copies in when it creates the container
// (edge.TokenFile); the spec, and so the container's hash label, holds the
// token in the data directory. When someone changes the file inside the
// container (docker cp, a restored backup) the hash still matches and
// EnsureEdge keeps the container, which then refuses every hand-over: apps
// already routed keep working, new routes and domains never take effect.
//
// The rule: when the server starts (Reconciler.EnsureEdge) and the running
// container refuses the token (Health cause edge.CauseAuth), the container
// is rebuilt in place from the token in the data directory: same data and
// settings volumes, the whole configuration copied in again, the app URLs
// gone for the few seconds a recreation takes. At most
// maxEdgeTokenRecreates times per run of the server: when it still refuses
// afterwards nothing touches the container again until the next start, and
// the state says EDGE-03. Only the start does this, not every round, so a
// token that keeps changing never makes the entry container restart over
// and over, and a container changed while the server runs stays as it is
// (doctor reports EDGE-03) until the server is restarted.
const maxEdgeTokenRecreates = 1

// edgeTokenCheckWait bounds how long the start waits for the management
// endpoint to answer at all before deciding whether it refuses the token (a
// container that was just started is still coming up). Tests shorten it.
var edgeTokenCheckWait = 20 * time.Second

// recreateEdgeIfTokenRefused applies the rule above. It returns an error only
// when the recreation itself failed.
func (r *Reconciler) recreateEdgeIfTokenRefused(ctx context.Context, routes []edge.Route) error {
	if r.edgeManager() == nil || r.cfg.Proxy == nil {
		return nil
	}
	h := r.edgeHealthSettled(ctx)
	if h.Cause != edge.CauseAuth {
		return nil
	}
	r.edgeMu.Lock()
	if r.edgeTokenRecreates >= maxEdgeTokenRecreates {
		r.edgeMu.Unlock()
		r.setEdgeState(EdgeStateError, h.Reason)
		r.log.Warn("edge refuses the management token; already recreated once in this run, leaving it", "fault", edge.FaultAdmin)
		return nil
	}
	r.edgeTokenRecreates++
	r.edgeMu.Unlock()
	r.log.Warn("edge refuses the management token in the data directory; recreating it in place", "fault", edge.FaultAdmin)
	if _, err := r.ensureEdgeWith(ctx, routes, true, true); err != nil {
		return err
	}
	if h := r.edgeHealthSettled(ctx); h.Cause == edge.CauseAuth {
		r.setEdgeState(EdgeStateError, h.Reason)
		r.log.Error("edge still refuses the management token after recreating it; not recreating it again in this run", "fault", edge.FaultAdmin)
		return nil
	}
	r.log.Info("edge recreated with the management token in the data directory")
	return nil
}

// edgeHealthSettled asks the proxy's health, waiting up to
// edgeTokenCheckWait while the management endpoint does not answer yet.
func (r *Reconciler) edgeHealthSettled(ctx context.Context) edge.Health {
	deadline := time.Now().Add(edgeTokenCheckWait)
	for {
		h := r.cfg.Proxy.Health(ctx)
		if h.Cause != edge.CauseUnreachable && h.State != edge.StateStarting {
			return h
		}
		if !time.Now().Before(deadline) {
			return h
		}
		select {
		case <-ctx.Done():
			return h
		case <-time.After(300 * time.Millisecond):
		}
	}
}
