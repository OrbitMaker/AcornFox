package caddyroute

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// caddyDefaultAdmin is the address Caddy's built-in admin API listens on
// unless told otherwise. The edge container moves the admin API to a unix
// socket behind the token gateway, so this port must stay closed.
const caddyDefaultAdmin = "127.0.0.1:2019"

// withPorts fills zero HTTP and HTTPS ports with 80/443 and leaves the
// issuer as given ("" and "acme" both mean public ACME to the driver).
func withPorts(h HTTPSConfig) HTTPSConfig {
	if h.HTTPPort == 0 {
		h.HTTPPort = defaultHTTPPort
	}
	if h.HTTPSPort == 0 {
		h.HTTPSPort = defaultHTTPSPort
	}
	return h
}

// DefaultAdminOpen reports whether something answers on Caddy's built-in
// admin port on this network namespace. The adapter starts Caddy with its
// admin API on a unix socket, so this port must stay closed.
func DefaultAdminOpen() bool { return caddyDefaultAdminOpen() }

func caddyDefaultAdminOpen() bool {
	c, err := net.DialTimeout("tcp", caddyDefaultAdmin, 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// Apply implements edge.Proxy: it takes cfg's HTTPS settings and makes the
// AcornFox-owned part of Caddy's configuration exactly cfg.Routes (Sync).
// Caddy keeps nothing across a restart (the edge container runs with
// persist_config off); Version then no longer matches and the caller applies
// again.
func (c *Driver) Apply(ctx context.Context, cfg edge.Config) (string, error) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	c.https = withPorts(cfg.HTTPS)
	c.mu.Unlock()
	if err := c.Sync(ctx, cfg.Routes); err != nil {
		return "", err
	}
	return cfg.Version(), nil
}

// Version implements edge.Proxy. Caddy does not store our version, so the
// driver reads the AcornFox-owned servers back (Current) and computes the
// version of what Caddy really serves, with the HTTPS settings of the last
// Apply (or of New before the first one).
func (c *Driver) Version(ctx context.Context) (string, error) {
	cur, err := c.Current(ctx)
	if err != nil {
		return "", err
	}
	cfg := edge.Config{HTTPS: c.httpsConf(), Routes: make([]edge.Route, 0, len(cur))}
	for _, r := range cur {
		cfg.Routes = append(cfg.Routes, r)
	}
	return cfg.Version(), nil
}

// Health implements edge.Proxy. Through the edge container's admin gateway
// (a token is set) it checks, in this order, that the gateway answers, that
// it refuses a request without the token, that Caddy's built-in admin port is
// closed, and that the token is accepted. On a unix socket (the v0.3 host
// Caddy) only the last check applies.
func (c *Driver) Health(ctx context.Context) edge.Health {
	if c.token != "" {
		status, err := c.statusWithoutToken(ctx)
		if err != nil {
			return edge.Health{State: edge.StateError, Cause: edge.CauseUnreachable, Reason: "管理网关没有应答：" + err.Error()}
		}
		if status != http.StatusForbidden && status != http.StatusUnauthorized {
			return edge.Health{State: edge.StateError, Cause: edge.CauseExposed, Reason: fmt.Sprintf("不带令牌访问返回 %d（应为 403 或 401）", status)}
		}
		if c.defaultAdminPortOpen != nil && c.defaultAdminPortOpen() {
			return edge.Health{State: edge.StateError, Cause: edge.CauseExposed, Reason: "Caddy 默认管理口 2019 开着"}
		}
	}
	body, status, err := c.do(ctx, http.MethodGet, "/config/", nil)
	switch {
	case err != nil:
		return edge.Health{State: edge.StateError, Cause: edge.CauseUnreachable, Reason: "管理口没有应答：" + err.Error()}
	case status == http.StatusForbidden || status == http.StatusUnauthorized:
		return edge.Health{State: edge.StateError, Cause: edge.CauseAuth, Reason: fmt.Sprintf("带令牌访问返回 %d（令牌不对）", status)}
	case status < 200 || status >= 300:
		return edge.Health{State: edge.StateError, Cause: edge.CauseOther, Reason: fmt.Sprintf("管理口返回 %d：%s", status, strings.TrimSpace(string(body)))}
	}
	return edge.Health{State: edge.StateRunning}
}

// statusWithoutToken sends GET /config/ without the token header and returns
// the status code.
func (c *Driver) statusWithoutToken(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/config/", nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
