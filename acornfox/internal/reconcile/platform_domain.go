package reconcile

import (
	"context"
	"sort"

	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// Platform domain: one domain for the whole machine (Config.PlatformDomain).
// A public app's route carries <app>.<platform domain> next to the domains
// the app bound itself, so the entry proxy serves it over HTTPS and gets its
// certificate exactly as for those (edge contract 4: one certificate per
// name, HTTP-01 on port 80). Who gets a name is platformvar.PlatformHost,
// the same rule the app's address follows. A name that is already a domain
// some app bound, or the public console's, is left off (two routes cannot
// share a name). Turning an app back to local or lan, changing or clearing
// the platform domain drops the name from the next configuration; the
// certificates already issued stay in the entry proxy's volume.

// platformHosts returns, for one route computation, the platform name each
// app's route carries ("" for none). Without a platform domain every app
// gets "".
func (r *Reconciler) platformHosts(ctx context.Context) func(a state.App) string {
	base := ""
	if r.cfg.PlatformDomain != nil {
		base = r.cfg.PlatformDomain(ctx)
	}
	if base == "" {
		return func(state.App) string { return "" }
	}
	taken := r.takenNames(ctx)
	return func(a state.App) string {
		h := platformvar.PlatformHost(a.Name, a.Internal, r.exposureOf(a), base)
		if h == "" || taken[h] {
			return ""
		}
		return h
	}
}

// takenNames are the names already on a route of their own: every domain an
// app bound and the extra routes' (the public console).
func (r *Reconciler) takenNames(ctx context.Context) map[string]bool {
	taken := map[string]bool{}
	if ds, err := r.cfg.Store.ListDomains(ctx, ""); err == nil {
		for _, d := range ds {
			taken[d.Name] = true
		}
	} else {
		r.log.Error("list domains", "err", err)
	}
	if r.cfg.ExtraRoutes != nil {
		if extra, err := r.cfg.ExtraRoutes(ctx); err == nil {
			for _, rt := range extra {
				for _, d := range rt.Domains {
					taken[d] = true
				}
			}
		}
	}
	return taken
}

// PlatformHosts are the names under the platform domain on the routes of
// the apps running now (the platform domain's ports and certificate checks
// look at these). Empty without a platform domain or while the runner
// cannot be asked.
func (r *Reconciler) PlatformHosts(ctx context.Context) []string {
	base := ""
	if r.cfg.PlatformDomain != nil {
		base = r.cfg.PlatformDomain(ctx)
	}
	if base == "" {
		return nil
	}
	if _, err := r.cfg.Runner.Ping(ctx); err != nil {
		return nil
	}
	routes, err := r.observedRoutes(ctx)
	if err != nil {
		return nil
	}
	var out []string
	for _, rt := range routes {
		for _, d := range rt.Domains {
			if d == rt.App+"."+base {
				out = append(out, d)
			}
		}
	}
	sort.Strings(out)
	return out
}
