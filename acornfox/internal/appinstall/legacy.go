package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// Apps installed with a manifest of the former format (before services were
// written in manifests) keep running: they start, stop and can be deleted
// as before. They are not in the service registry and are never moved into
// it; installing them again with a manifest of the current format is the
// way to get the new behaviour.
//
// Such an app may have got a credential made for it in another app on this
// machine when it was installed: the platform recorded which app made it
// and its number with the app's saved parameters (legacyCredentialsKey).
// Taking such a credential back was done by code this platform no longer
// has, so a delete only reminds the user to remove it by hand
// (LegacyCredentialNotes), and so does an install with a manifest of the
// current format on the data of such an app deleted with its data kept
// (restoreKept). Nothing else reads that record.

// LegacyNote is LegacyAppNote for an app installed with a manifest of the
// former format (installedWith), "" for any other app. GET /v1/apps/{app}
// says it as legacy_note, `acornfox status APP` too.
func (e *Engine) LegacyNote(ctx context.Context, app string) string {
	if _, legacy := e.installedWith(ctx, app); legacy {
		return LegacyAppNote
	}
	return ""
}

// installedWith is the one rule that tells an app installed with a manifest
// of the former format from one of the current format; `apps vars`, the
// legacy note of the app's detail (and so `status`) and the service registry
// all go by it. m is the manifest app was installed with when it is of the
// current format, intact and readable, and app is not a former one (nil
// otherwise). legacy reports an app installed from the app center (it has a
// catalog app id) when
//
//   - the manifest saved for it is of a former format;
//   - its saved parameters are such an app's (legacyValues); or
//   - no manifest is saved for it at all. An install of the current format
//     saves its manifest before its parameters, so only an app installed by
//     a version that did not save manifests (and whose package was not on
//     this machine to backfill it from) has none. An app still being
//     installed here has none yet either and is not called one.
//
// An app not from the app center, or not there, is neither.
func (e *Engine) installedWith(ctx context.Context, app string) (m *catalog.Manifest, legacy bool) {
	a, err := e.cfg.Store.GetApp(ctx, app)
	if err != nil || a.CatalogID == "" {
		return nil, false
	}
	cm, err := e.cfg.Store.GetCatalogManifest(ctx, app)
	switch {
	case err == nil && cm.Manifest != "":
		if _, former := catalog.FormerManifest([]byte(cm.Manifest)); former {
			return nil, true
		}
		if state.ManifestSHA256(cm.Manifest) == cm.SHA256 {
			m, _ = catalog.ParseManifest([]byte(cm.Manifest))
		}
	case err == nil || errors.Is(err, state.ErrNotFound):
		if e.busy(app) == nil {
			return nil, true
		}
	default:
		e.cfg.Log.Warn("read the saved manifest of an app", "app", app, "err", err)
	}
	if saved, err := e.savedParams(ctx, app); err == nil && legacyValues(saved) {
		return nil, true
	}
	return m, false
}

// legacyCredentialsKey is where the saved parameters of an app installed
// with a manifest of the former format record the credentials made for it
// in other apps.
const legacyCredentialsKey = "_acornfox_gateway_tokens"

// legacyCredential is one of them: the app that made it and its number
// there (never its value).
type legacyCredential struct {
	Provider string `json:"provider"`
	ID       int    `json:"id"`
}

// LegacyCredentialNotes says, for an app installed with a manifest of the
// former format, which credentials other apps made for it, and that the user
// removes them by hand: a delete no longer takes them back. Call it before
// the app is deleted (its saved parameters go with it). Nothing for any
// other app.
func (e *Engine) LegacyCredentialNotes(ctx context.Context, app string) []string {
	vals, err := e.savedParams(ctx, app)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		e.cfg.Log.Warn("read the saved parameters of an app being deleted", "app", app, "err", err)
		return nil
	}
	return legacyNotes(app, vals, false)
}

// legacyNotes is what LegacyCredentialNotes says, from the saved (or kept)
// parameters vals of app. reinstall: said for an install with a manifest of
// the current format on the data of such an app (deleted with its data
// kept, when nothing was said because an install was meant to take the
// credential over): it is not used any more, and nothing takes it back.
func legacyNotes(app string, vals map[string]any, reinstall bool) []string {
	raw, ok := vals[legacyCredentialsKey]
	if !ok {
		return nil
	}
	b, _ := json.Marshal(raw)
	var creds []legacyCredential
	if err := json.Unmarshal(b, &creds); err != nil {
		return nil
	}
	var out []string
	for _, c := range creds {
		if c.Provider == "" {
			continue
		}
		if reinstall {
			out = append(out, fmt.Sprintf("%s %s 以前是用旧格式的清单装的，当初 %s 里给它建的那一项（名字 acornfox-%s，编号 %d）这次不再用了，平台也不会替它收回：它还在、还能用，用不上请到 %s 里手工删掉",
				CodeRevoke, app, c.Provider, app, c.ID, c.Provider))
			continue
		}
		out = append(out, fmt.Sprintf("%s %s 是用旧格式的清单装的，删除时平台不再替它收回当初拿到的凭据：%s 里给它建的那一项（名字 acornfox-%s，编号 %d）还在、还能用，用不上请到 %s 里手工删掉",
			CodeRevoke, app, c.Provider, app, c.ID, c.Provider))
	}
	return out
}
