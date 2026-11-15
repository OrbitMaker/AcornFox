package caddyroute

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/edge"
)

// Base is the part of Caddy's configuration that does not come from an
// edge.Config: where its own admin API listens (a unix socket inside the
// container) and how long it lets running requests finish when it stops or
// replaces a server. The adapter owns the whole Caddy configuration, so
// Replace writes Base together with the routes every time.
type Base struct {
	AdminSocket string        // path of the admin unix socket
	Grace       time.Duration // must stay below Docker's 10 second stop timeout
}

// Render returns Caddy's whole configuration for cfg: the admin API on the
// unix socket with nothing persisted, the per-app servers, the shared
// domains server with its TLS policy (same shapes as Sync writes, so Current
// reads them back the same way), the HTTP and HTTPS ports, the grace period,
// no attempt to install the local CA into the container's trust store (the
// image has no tools for it), and the servers in keep (the domain challenge)
// as they are.
func Render(base Base, cfg edge.Config, keep map[string]json.RawMessage) ([]byte, error) {
	d := &Driver{https: withPorts(cfg.HTTPS)}
	return d.render(base, cfg.Routes, keep)
}

func (c *Driver) render(base Base, routes []Route, keep map[string]json.RawMessage) ([]byte, error) {
	servers := map[string]any{}
	for name, raw := range keep {
		servers[name] = raw
	}
	for _, r := range routes {
		if r.PublicPort == 0 {
			continue
		}
		servers[serverPrefix+r.App] = desiredServer(r)
	}
	domains := collectDomains(routes)
	if len(domains) > 0 {
		servers[domainsServer] = c.desiredDomainsServer(routes)
	}
	h := c.httpsConf()
	httpApp := map[string]any{
		"grace_period": base.Grace.String(),
		"http_port":    h.HTTPPort,
		"https_port":   h.HTTPSPort,
		"servers":      servers,
	}
	apps := map[string]any{
		"http": httpApp,
		"pki": map[string]any{
			"certificate_authorities": map[string]any{
				"local": map[string]any{"install_trust": false},
			},
		},
	}
	if p := c.desiredTLSPolicy(domains); p != nil {
		apps["tls"] = map[string]any{"automation": map[string]any{"policies": []tlsAutomationPolicy{*p}}}
	}
	full := map[string]any{
		"admin": map[string]any{
			"listen":  "unix/" + base.AdminSocket,
			"origins": []string{"caddy"},
			"config":  map[string]any{"persist": false},
		},
		"apps": apps,
	}
	return json.MarshalIndent(full, "", "  ")
}

// Replace loads cfg as Caddy's whole configuration in one step (POST /load):
// Caddy either takes all of it or keeps what it had, so a refused
// configuration (a port another program holds) changes nothing, and servers,
// certificates policy and ports never exist half-written. Unlike Sync it does
// not keep foreign servers: inside the entry container nothing else writes to
// Caddy. The domain challenge server, if set, is kept.
func (c *Driver) Replace(ctx context.Context, base Base, cfg edge.Config) error {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	c.mu.Lock()
	c.https = withPorts(cfg.HTTPS)
	c.mu.Unlock()
	keep, err := c.rawServers(ctx, verifyServer)
	if err != nil {
		return err
	}
	body, err := c.render(base, cfg.Routes, keep)
	if err != nil {
		return err
	}
	resp, status, err := c.do(ctx, http.MethodPost, "/load", body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("caddy load: status %d: %s", status, strings.TrimSpace(string(resp)))
	}
	return nil
}

// rawServers returns the named servers exactly as Caddy has them.
func (c *Driver) rawServers(ctx context.Context, names ...string) (map[string]json.RawMessage, error) {
	body, status, err := c.do(ctx, http.MethodGet, "/config/apps/http/servers", nil)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	if pathMissing(status, body) || !okBody(status, body) {
		if status >= 200 && status < 300 || pathMissing(status, body) {
			return out, nil
		}
		return nil, fmt.Errorf("caddy get servers: status %d: %s", status, strings.TrimSpace(string(body)))
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(body, &all); err != nil {
		return nil, fmt.Errorf("caddy decode servers: %w", err)
	}
	for _, n := range names {
		if raw, ok := all[n]; ok && !isNullOrEmpty(raw) {
			out[n] = raw
		}
	}
	return out, nil
}
