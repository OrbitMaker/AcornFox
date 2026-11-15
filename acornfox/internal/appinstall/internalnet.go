package appinstall

// Apps that do not answer HTTP (a database, a cache) and apps reached over
// the app network only.
//
// Access "internal" (exposure.default: internal in the manifest): the app
// is recorded with state.App.Internal. Its port is not published on the
// host and it gets no entry route (the reconciler passes Internal to the
// runner); its address is empty (platform.app.url "", no URL, no open hint,
// nothing to open) and its access cannot be changed afterwards. Other apps
// reach it the way apps reach each other: by its name on its app network
// (self.host, the network alias the runner gives every app container, also
// valid inside the app's own container) and the endpoint's port
// (self.port). self.internal_url is an http:// address: it is not worked out
// for a tcp endpoint.
//
// Health of the primary endpoint, as the manifest says it, both at install
// (healthy) and for every new container of the app (the reconciler, from
// state.App.HealthCheck):
//   - path: a GET, as before;
//   - exec: the command run as written in the app's own container through
//     the runner (runner.ExecAPI); exit code 0, and the trimmed stdout equal
//     to expect_stdout when one is written, means up;
//   - tcp: the port accepts a connection.
// This machine connects to the published loopback port (an app that is not
// internal and publishes none is not up), or, for an internal app, to the
// container's address on its app network (runner.ProbeAddr; why that needs
// no extra permission is said there).

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/runner"
	"github.com/acornfox/acornfox/internal/state"
)

// internalManifest reports whether m's app is reached over the app network
// only.
func internalManifest(m *catalog.Manifest) bool {
	return m != nil && m.Exposure.Default == catalog.ExposureInternal
}

// stateHealthCheck is the reconciler's health check for an endpoint whose
// manifest check is not a GET (exec or tcp); nil for a path check (or none).
func stateHealthCheck(ep *catalog.Endpoint) *state.HealthCheck {
	if ep == nil || ep.Health == nil {
		return nil
	}
	h := ep.Health
	timeout := h.TimeoutS
	if timeout < 0 || timeout > state.MaxHealthTimeoutS {
		timeout = 0
	}
	switch {
	case len(h.Exec) > 0:
		return &state.HealthCheck{Exec: append([]string(nil), h.Exec...), ExpectStdout: h.ExpectStdout, TimeoutS: timeout}
	case h.TCP:
		return &state.HealthCheck{TCP: true, TimeoutS: timeout}
	}
	return nil
}

// usesExecHealth reports whether some endpoint of m checks its health with
// a command run in the container.
func usesExecHealth(m *catalog.Manifest) bool {
	for _, ep := range m.Endpoints {
		if ep.Health != nil && len(ep.Health.Exec) > 0 {
			return true
		}
	}
	return false
}

// liveContainer is the running container of the app's current deployment.
func (e *Engine) liveContainer(ctx context.Context, a state.App) (runner.ContainerInfo, error) {
	cs, err := e.cfg.Runner.ListContainers(ctx, a.Name)
	if err != nil {
		return runner.ContainerInfo{}, err
	}
	for _, c := range cs {
		if c.DeploymentID == a.CurrentDeployment && c.Running {
			return c, nil
		}
	}
	return runner.ContainerInfo{}, errors.New("没有在运行的容器")
}

// checkHealthOnce runs the primary endpoint's health check h of app once on
// its live container: nil when it passed.
func (e *Engine) checkHealthOnce(ctx context.Context, a state.App, ep *catalog.Endpoint, h catalog.Health) error {
	c, err := e.liveContainer(ctx, a)
	if err != nil {
		return err
	}
	if len(h.Exec) > 0 {
		api, _ := e.cfg.Runner.(runner.ExecAPI)
		return runner.ProbeExec(ctx, api, a.Name, c.Name, h.Exec, h.ExpectStdout)
	}
	port := 0
	if ep != nil {
		port = ep.Port
	}
	addr := runner.ProbeAddr(c, port, a.Internal)
	if addr == "" {
		if !a.Internal {
			return errors.New("容器还没有映射端口")
		}
		return errors.New("容器还没有地址")
	}
	if h.TCP {
		return e.tcpProbe(ctx, addr)
	}
	code, body, err := e.httpJSON(ctx, http.MethodGet, "http://"+addr+h.Path, nil)
	switch {
	case err != nil:
		return err
	case code < 200 || code >= 300:
		return fmt.Errorf("HTTP %d", code)
	case len(h.ExpectJSON) > 0 && !matchJSON(h.ExpectJSON, body):
		return errors.New("应答内容和预期不符")
	}
	return nil
}

// tcpProbe is Config.TCPProbe or runner.ProbeTCP.
func (e *Engine) tcpProbe(ctx context.Context, addr string) error {
	if e.cfg.TCPProbe != nil {
		return e.cfg.TCPProbe(ctx, addr)
	}
	return runner.ProbeTCP(ctx, addr)
}

// healthWhat names a health check for messages: the path, or what it is.
func healthWhat(h catalog.Health) string {
	switch {
	case len(h.Exec) > 0:
		return "（在容器里执行 " + h.Exec[0] + "）"
	case h.TCP:
		return "（端口连得上）"
	}
	return h.Path
}
