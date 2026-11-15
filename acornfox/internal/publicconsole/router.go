package publicconsole

import (
	"context"

	"github.com/acornfox/acornfox/internal/edge"
)

// RouteApp names the public console's domain-only route. It is not a valid
// app name (apps never start with "_"), so it cannot collide with an app.
const RouteApp = "_public-console"

// ConfigSource is what the route source reads.
type ConfigSource interface {
	Load() (*Config, error)
}

// Routes returns the public console's routes for the reconciler's
// Config.ExtraRoutes: while the console is on, one domain-only route that
// sends its domain to upstream (the gateway on loopback); none while it is
// off. Because the route is part of every configuration handed to the entry
// proxy, a proxy that lost it (restarted) or still has it after the console
// was turned off reports a different version and gets the configuration
// again on the next round.
func Routes(src ConfigSource, upstream string) func(context.Context) ([]edge.Route, error) {
	return func(context.Context) ([]edge.Route, error) {
		cfg, err := src.Load()
		if err != nil || cfg == nil {
			return nil, err
		}
		return []edge.Route{{App: RouteApp, Upstream: upstream, Domains: []string{cfg.Domain}}}, nil
	}
}
