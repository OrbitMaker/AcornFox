package appinstall

import (
	"context"
	"errors"

	"github.com/acornfox/acornfox/internal/state"
)

// BackfillManifests saves the manifest of every catalog app that has none
// (apps installed before manifests were saved): by catalog, catalog app id
// and version it looks for the app package in the built-in snapshot and this
// machine's catalog cache, and saves the manifest (source "backfill") when
// the package is there and matches the index's checksum. Nothing is
// downloaded for it; an app whose package is not here is skipped and stays
// without a manifest (its variables are then listed without classes).
//
// Operating notes:
//
//   - When: once at server start (Engine.Start, which the server runs in
//     the background, after interrupted installs were cleaned up). Running
//     it again is harmless: an app that has a manifest is never touched.
//   - Side effects: rows in app_catalog_manifests. No network, no Docker.
//   - Verify: `acornfox apps vars <app>` shows where the manifest came from
//     (manifest.source: install | backfill, or manifest.saved false).
//   - Failure: logged ("app center: manifest backfill …"), never fatal.
//
// It returns how many manifests it saved and how many apps it had to skip.
func (e *Engine) BackfillManifests(ctx context.Context) (saved, skipped int) {
	if e.cfg.Catalog == nil {
		return 0, 0
	}
	apps, err := e.cfg.Store.ListApps(ctx)
	if err != nil {
		e.cfg.Log.Warn("app center: manifest backfill: list apps", "err", err)
		return 0, 0
	}
	for _, a := range apps {
		if ctx.Err() != nil {
			return saved, skipped
		}
		if a.CatalogID == "" {
			continue
		}
		if _, err := e.cfg.Store.GetCatalogManifest(ctx, a.Name); err == nil {
			continue
		} else if !errors.Is(err, state.ErrNotFound) {
			e.cfg.Log.Warn("app center: manifest backfill: read", "app", a.Name, "err", err)
			skipped++
			continue
		}
		raw, ok := e.cfg.Catalog.localManifest(a.CatalogOrigin, a.CatalogID, a.CatalogVersion)
		if !ok {
			e.cfg.Log.Info("app center: manifest backfill: package not on this machine, skipped",
				"app", a.Name, "catalog_id", a.CatalogID, "version", a.CatalogVersion, "catalog", sourceRef(a.CatalogOrigin, "").ID)
			skipped++
			continue
		}
		text := string(raw)
		added, err := e.cfg.Store.AddCatalogManifest(ctx, state.CatalogManifest{
			App: a.Name, CatalogID: a.CatalogID, CatalogVersion: a.CatalogVersion, CatalogOrigin: a.CatalogOrigin,
			Manifest: text, SHA256: state.ManifestSHA256(text), Source: state.ManifestFromBackfill,
		})
		switch {
		case errors.Is(err, state.ErrNotFound):
			// Deleted meanwhile.
		case err != nil:
			e.cfg.Log.Warn("app center: manifest backfill: save", "app", a.Name, "err", err)
			skipped++
		case added:
			e.cfg.Log.Info("app center: manifest backfill: saved",
				"app", a.Name, "catalog_id", a.CatalogID, "version", a.CatalogVersion, "catalog", sourceRef(a.CatalogOrigin, "").ID)
			saved++
		}
	}
	if saved > 0 || skipped > 0 {
		e.cfg.Log.Info("app center: manifest backfill done", "saved", saved, "skipped", skipped)
	}
	return saved, skipped
}
