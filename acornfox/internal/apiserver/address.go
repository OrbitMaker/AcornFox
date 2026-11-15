package apiserver

import (
	"context"
	"sort"

	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// An app's address (appView.URL, what `acornfox status`, `acornfox apps` and
// the console show) is worked out by the platform variables' interface, the
// same as app.url: this server only says where the app can be reached (its
// exposure, port and domains; where below) and hands the install engine the
// same (Hooks.Where). Nothing here builds an address by itself.

// addresserFor is cfg's addresser: the given one (the app center's
// provider, in the server's wiring), else the standalone edition's
// algorithm with this server's options.
func addresserFor(cfg Config) platformvar.Addresser {
	if cfg.PlatformVars != nil {
		return cfg.PlatformVars
	}
	return platformvar.Host{PublicHost: cfg.PublicHost, Desktop: cfg.LANHost != nil}
}

// where is where app a can be reached now: its exposure (the server's
// default filled in; "" for an internal app), its port and the domains bound
// to it, the first bound first.
func (s *server) where(ctx context.Context, a state.App) platformvar.Where {
	w := platformvar.Where{Port: a.PublicPort}
	if a.Internal {
		return w
	}
	w.Exposure = s.effectiveExposure(a)
	if doms, err := s.store.ListDomains(ctx, a.Name); err == nil && len(doms) > 0 {
		sort.SliceStable(doms, func(i, j int) bool { return doms[i].CreatedAt.Before(doms[j].CreatedAt) })
		for _, d := range doms {
			w.Domains = append(w.Domains, d.Name)
		}
	}
	return w
}

// appAddress is the address app a opens at now.
func (s *server) appAddress(ctx context.Context, a state.App) string {
	return s.addresser.Address(platformvar.App{Name: a.Name, Created: true, Internal: a.Internal, Where: s.where(ctx, a)}).URL
}
