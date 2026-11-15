package caddyroute

import (
	"context"
	"fmt"
)

// verifyServer is the temporary plain-HTTP server that answers the domain
// ownership challenge of the public console. Its name deliberately
// lacks the "af-" prefix so Sync never deletes it mid-verification.
const verifyServer = "acornfox-verify"

// NewVerifier returns a driver used for its challenge methods only (see
// New). The challenge listens on https.HTTPPort (default 80). One driver
// serves both: the program wires a single New.
func NewVerifier(admin string, https HTTPSConfig, opts ...Option) *Driver {
	return New(admin, https, opts...)
}

// SetChallenge implements edge.Challenger. The server listens only on the HTTP port,
// so Caddy's automatic HTTPS leaves it alone; when the shared domains server
// exists Caddy adds its HTTP->HTTPS redirects behind our terminal route.
func (c *Driver) SetChallenge(ctx context.Context, host, path, body string) error {
	if err := c.ensureServersPath(ctx); err != nil {
		return err
	}
	srv := map[string]any{
		"listen": []string{fmt.Sprintf(":%d", c.httpsConf().HTTPPort)},
		"routes": []any{map[string]any{
			"match": []any{map[string]any{"host": []string{host}, "path": []string{path}}},
			"handle": []any{map[string]any{
				"handler":     "static_response",
				"status_code": 200,
				"body":        body,
				"headers": map[string][]string{
					"Content-Type":  {"text/plain; charset=utf-8"},
					"Cache-Control": {"no-store"},
				},
			}},
			"terminal": true,
		}},
	}
	return c.putJSON(ctx, "/config/apps/http/servers/"+verifyServer, srv)
}

// ClearChallenge implements edge.Challenger.
func (c *Driver) ClearChallenge(ctx context.Context) error {
	servers, err := c.getServers(ctx)
	if err != nil {
		return err
	}
	if _, ok := servers[verifyServer]; !ok {
		return nil
	}
	return c.deleteServer(ctx, verifyServer)
}
