package appinstall

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/platformvar"
	"github.com/acornfox/acornfox/internal/state"
)

// Platform variables: the install engine takes every value from one
// interface, platformvar.Provider (Config.PlatformVars; the standalone
// implementation of this machine when it is nil), and only keeps what it got
// with the app's parameters. Nothing in this package works a value out or
// checks a user's change by itself.

// PlatformEnv is this machine's settings the standalone implementation fills
// the platform variables from. The server builds it from install.env /
// network.json at start.
type PlatformEnv = platformvar.Machine

// platformKey is where Values keep the platform variables. "@" can never
// start a parameter key (the schema allows [a-z][a-z0-9_]*).
const platformKey = "@platform"

// pv is the provider the engine takes platform variables from.
func (e *Engine) pv() platformvar.Provider {
	if e.cfg.PlatformVars != nil {
		return e.cfg.PlatformVars
	}
	return platformvar.NewStandalone(e.cfg.Platform)
}

// PlatformVarProvider is the provider the engine takes platform variables
// and apps' addresses from; the API server shows the apps' addresses with
// the same one.
func (e *Engine) PlatformVarProvider() platformvar.Provider { return e.pv() }

// where is where app a can be reached now (Hooks.Where; without it, its own
// record and no domains).
func (e *Engine) where(ctx context.Context, a state.App) platformvar.Where {
	if h := e.h(); h.Where != nil {
		return h.Where(ctx, a)
	}
	w := platformvar.Where{Exposure: a.Exposure, Port: a.PublicPort}
	if a.Internal {
		w.Exposure = ""
	} else if w.Exposure == "" {
		w.Exposure = catalog.DefaultExposure
	}
	return w
}

// appContext is app a as the provider works its variables out: created,
// where it can be reached now, with the user's changes sets.
func (e *Engine) appContext(ctx context.Context, a state.App, sets map[string]string) platformvar.App {
	return platformvar.App{Name: a.Name, Created: true, Internal: a.Internal, Where: e.where(ctx, a), Sets: sets}
}

// address is the address app a opens at now: what the user is shown (the
// install result, first-login details), the same as app.url would be.
func (e *Engine) address(ctx context.Context, a state.App) string {
	return e.pv().Address(e.appContext(ctx, a, nil)).URL
}

// checkPlatformSets validates the user's changes (CAT-INST-28) with the
// provider's check.
func (e *Engine) checkPlatformSets(sets map[string]string) error {
	if err := e.pv().Check(sets); err != nil {
		return fault(CodeBadPlatform, nil, "%s", err.Error())
	}
	return nil
}

// platformValues is the platform variables of one app as the provider works
// them out, with the user's changes (already checked).
func (e *Engine) platformValues(ctx context.Context, app platformvar.App) (map[string]string, error) {
	vals, err := e.pv().Values(ctx, app)
	if err != nil {
		return nil, fault(CodeInternal, err, "取平台变量的值失败")
	}
	if vals == nil {
		vals = map[string]string{}
	}
	return vals, nil
}

// platformUserKey is where Values keep only the platform variables the user
// changed in the install request (--set platform.<name>=…), next to the
// merged values under platformKey. An app installed before this key existed
// has none: its platform values cannot be told apart and count as "the
// value at install time".
const platformUserKey = "@platform_user"

// platformUserSets is the user's changes as they are saved (already
// checked); never nil, so a saved install with no change is told from an old
// record without the key.
func platformUserSets(sets map[string]string) map[string]string {
	out := make(map[string]string, len(sets))
	for k, v := range sets {
		out[k] = strings.TrimSpace(v)
	}
	return out
}

// platformMap returns the platform variables kept in v (a map[string]any
// after the saved parameters were read back from JSON).
func (v Values) platformMap() map[string]string {
	switch m := v[platformKey].(type) {
	case map[string]string:
		return m
	case map[string]any:
		out := make(map[string]string, len(m))
		for k, x := range m {
			if s, ok := x.(string); ok {
				out[k] = s
			}
		}
		return out
	}
	return nil
}

// platformUser returns the user's changes kept in v (platformUserKey), nil
// when there are none.
func (v Values) platformUser() map[string]string {
	if m, ok := v[platformUserKey].(map[string]string); ok {
		return m
	}
	m, _ := stringMap(v[platformUserKey])
	return m
}

// lookupPlatform resolves platform.<name>.
func (v Values) lookupPlatform(name string) (string, error) {
	pv, ok := catalog.LookupPlatformVar(name)
	if !ok {
		return "", fmt.Errorf("没有平台变量 %s", name)
	}
	s, ok := v.platformMap()[name]
	if !ok {
		return "", fmt.Errorf("平台变量 %s（%s）在这台机器上还没有值", name, pv.Label)
	}
	return s, nil
}

// PlatformVarView is one platform variable as GET /v1/catalog/platform-vars
// and the install form show it: the table entry plus this machine's value.
type PlatformVarView = platformvar.State

// PlatformVars lists the whole table with this machine's values.
func (e *Engine) PlatformVars(ctx context.Context) []PlatformVarView {
	return e.pv().Machine(ctx)
}

// platformAvailable reports whether platform variable name has a value on
// this machine once the app exists.
func (e *Engine) platformAvailable(name string) bool {
	return e.pv().Has(name)
}

// appPlatformViews is what the install form shows for one manifest: the
// variables it uses, as the provider has them here.
func (e *Engine) appPlatformViews(ctx context.Context, m *catalog.Manifest) []PlatformVarView {
	used := map[string]bool{}
	for _, n := range catalog.PlatformRefs(m) {
		used[n] = true
	}
	out := []PlatformVarView{}
	for _, st := range e.pv().Machine(ctx) {
		if used[st.Name] {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
