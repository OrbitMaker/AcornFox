package appinstall

import (
	"context"
	"encoding/base64"
	"path"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
)

// CatalogView is GET /v1/catalog: the public catalog and the connected
// private ones merged. Serial and Origin are the public catalog's; each app
// and recipe names its catalog in Source; Sources lists every catalog.
type CatalogView struct {
	Serial  int64            `json:"serial"`
	Origin  string           `json:"origin"` // cache | snapshot
	Sync    SyncStatus       `json:"sync"`
	Sources []SourceView     `json:"sources"`
	Apps    []CatalogAppView `json:"apps"`
	Recipes []RecipeView     `json:"recipes"`
}

// CatalogAppView is one app in the list.
type CatalogAppView struct {
	ID        string           `json:"id"`
	Version   string           `json:"version"`
	Name      string           `json:"name"`
	Summary   string           `json:"summary"`
	Category  string           `json:"category"`
	Keywords  []string         `json:"keywords"`
	Tags      []string         `json:"tags"`
	MemoryMB  catalog.MinRec   `json:"memory_mb"`
	Platforms []string         `json:"platforms"`
	SizeMB    int              `json:"size_mb"`
	Delivery  catalog.Delivery `json:"delivery"`
	Uses      []catalog.Use    `json:"uses"`
	Provides  []string         `json:"provides"`
	Supported bool             `json:"supported"` // runs on this machine's CPU
	Installed []string         `json:"installed"` // app names installed from it (this catalog) here
	Source    SourceRef        `json:"source"`    // the catalog it comes from
	Command   string           `json:"command"`   // CLI to install it
	// Icon and IconType as in AppDetail, only when the app's package is
	// already on this machine (the list never downloads; see icons.go).
	Icon     string `json:"icon,omitempty"`
	IconType string `json:"icon_type,omitempty"`
	// NeedsPlatform is the AcornFox version to upgrade to before this app
	// can be installed here ("0.6.0"); "" when this version can install it.
	// From the index's min_platform, so the list still downloads nothing.
	NeedsPlatform string `json:"needs_platform"`
	// Former: the catalog still lists the app in a manifest format before
	// the current one. This version cannot install it (CAT-INST-36) until
	// it is published again in the current format; Note says so.
	Former bool   `json:"former,omitempty"`
	Note   string `json:"note,omitempty"`
}

// FormerNote is the Note of an app or recipe the catalog still lists in a
// former manifest format.
const FormerNote = "这是旧格式的清单，这一版装不了，等这个应用按新格式重新上架"

// RecipeView is one recipe in the list.
type RecipeView struct {
	catalog.Recipe
	Source  SourceRef `json:"source"`
	Command string    `json:"command"`
	// NeedsPlatform as in CatalogAppView: the highest of the recipe's own
	// min_platform and its apps', when this AcornFox is older than it.
	NeedsPlatform string `json:"needs_platform"`
	// Former and Note as in CatalogAppView: one of its apps is listed in a
	// former manifest format.
	Former bool   `json:"former,omitempty"`
	Note   string `json:"note,omitempty"`
}

// catalogFlag is the --catalog part of an install command ("" for public).
func catalogFlag(origin string) string {
	if origin == "" {
		return ""
	}
	return " --catalog " + origin
}

// List returns the catalogs in use (public first, then the connected
// private ones) with what is installed here.
func (e *Engine) List(ctx context.Context) (CatalogView, error) {
	idx, origin, err := e.cfg.Catalog.Index()
	if err != nil {
		return CatalogView{}, err
	}
	installed, err := e.installedCatalogApps(ctx)
	if err != nil {
		return CatalogView{}, fault(CodeInternal, err, "读应用列表失败")
	}
	v := CatalogView{Serial: idx.Serial, Origin: origin, Sync: e.cfg.Catalog.Status(), Sources: e.cfg.Catalog.Sources(),
		Apps: []CatalogAppView{}, Recipes: []RecipeView{}}
	vs, err := e.cfg.Catalog.views()
	if err != nil {
		return CatalogView{}, fault(CodePackage, err, "目录读不了：%v", err)
	}
	for _, cv := range vs {
		src := sourceRef(cv.origin, cv.name)
		for _, a := range cv.idx.Apps {
			names := []string{}
			for _, s := range installed[a.ID] {
				if s.CatalogOrigin == cv.origin {
					names = append(names, s.Name)
				}
			}
			icon, iconType := e.cfg.Catalog.listIcon(cv, a)
			av := CatalogAppView{
				ID: a.ID, Version: a.Version, Name: a.Name, Summary: a.Summary, Category: a.Category,
				Keywords: a.Keywords, Tags: a.Tags, MemoryMB: a.MemoryMB, Platforms: a.Platforms, SizeMB: a.SizeMB,
				Delivery: a.Delivery, Uses: a.Uses, Provides: a.Provides, Supported: e.archSupported(a.Platforms),
				Installed: names, Source: src, Command: "acornfox install " + a.ID + catalogFlag(cv.origin),
				Icon: icon, IconType: iconType, NeedsPlatform: e.needsPlatform(a.MinPlatform),
			}
			if av.NeedsPlatform == "" && catalog.FormerIndexApp(a) {
				av.Former, av.Note = true, FormerNote
			}
			v.Apps = append(v.Apps, av)
		}
		byID := map[string]catalog.IndexApp{}
		for _, a := range cv.idx.Apps {
			byID[a.ID] = a
		}
		for _, r := range cv.idx.AllRecipes() {
			mins := []string{r.MinPlatform}
			former := false
			for _, ra := range r.Apps {
				mins = append(mins, byID[ra.App].MinPlatform)
				former = former || catalog.FormerIndexApp(byID[ra.App])
			}
			rv := RecipeView{Recipe: r, Source: src, Command: "acornfox install --recipe " + r.ID + catalogFlag(cv.origin),
				NeedsPlatform: e.needsPlatform(mins...)}
			if rv.NeedsPlatform == "" && former {
				rv.Former, rv.Note = true, FormerNote
			}
			v.Recipes = append(v.Recipes, rv)
		}
	}
	return v, nil
}

// AppDetail is GET /v1/catalog/apps/{id}: what the detail page and the
// install form need. Hidden parameters are left out.
type AppDetail struct {
	ID         string           `json:"id"`
	Version    string           `json:"version"`
	Versions   []string         `json:"versions"`
	Name       string           `json:"name"`
	Summary    string           `json:"summary"`
	Category   string           `json:"category"`
	Homepage   string           `json:"homepage"`
	License    string           `json:"license"`
	SourceCode string           `json:"source_code,omitempty"`
	Icon       string           `json:"icon,omitempty"`      // see IconType
	IconType   string           `json:"icon_type,omitempty"` // svg: Icon is SVG text; png: Icon is base64 of the PNG bytes
	MemoryMB   catalog.MinRec   `json:"memory_mb"`
	DiskMB     catalog.MinRec   `json:"disk_mb"`
	Platforms  []string         `json:"platforms"`
	Supported  bool             `json:"supported"`
	SizeMB     int              `json:"size_mb"`
	Image      string           `json:"image"`
	Port       int              `json:"port"`
	Exposure   string           `json:"exposure"`
	Delivery   catalog.Delivery `json:"delivery"`
	Params     []ParamView      `json:"params"`
	// Platform is the platform variables this app uses with this
	// machine's values; the user_can_set ones go in the advanced settings.
	Platform []PlatformVarView `json:"platform"`
	Provides []string          `json:"provides"`
	// Uses is the manifest's needs as before, each with what it is called
	// (use), its kind's label and whether it is required.
	Uses []UseView `json:"uses"`
	// ProvidesDetail is Provides with each kind's label.
	ProvidesDetail []PlanProvide `json:"provides_detail"`
	Installed      []string      `json:"installed"`
	Source         SourceRef     `json:"source"`
	Command        string        `json:"command"`
	// NeedsPlatform as in CatalogAppView ("" when installable here).
	NeedsPlatform string `json:"needs_platform"`
}

// ParamView is one form field.
type ParamView struct {
	Key       string `json:"key"`
	Type      string `json:"type"`
	Label     string `json:"label,omitempty"`
	Hint      string `json:"hint,omitempty"`
	Default   any    `json:"default,omitempty"`
	Options   []any  `json:"options,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Required  bool   `json:"required"`
	Generated bool   `json:"generated"` // left empty, the platform generates it
	Editable  string `json:"editable,omitempty"`
}

// Detail returns one app's detail from the first catalog that has it
// (public first).
func (e *Engine) Detail(ctx context.Context, id string) (AppDetail, error) {
	return e.DetailIn(ctx, "", id)
}

// DetailIn returns one app's detail from catalog from ("" any, "public", or
// a private catalog id).
func (e *Engine) DetailIn(ctx context.Context, from, id string) (AppDetail, error) {
	// An app listed in a former manifest format has no detail this version
	// can show (its manifest does not decode to the current format): say
	// why, before anything is downloaded.
	if _, ia, ferr := e.cfg.Catalog.find(from, id); ferr == nil && e.needsPlatform(ia.MinPlatform) == "" && catalog.FormerIndexApp(ia) {
		return AppDetail{}, formerApp(ia, 0, false)
	}
	a, err := e.cfg.Catalog.AppIn(ctx, from, id)
	if err != nil {
		// A package made for a newer platform may not even be readable here:
		// say "upgrade first", not "the package is broken".
		if v, ia, ferr := e.cfg.Catalog.find(from, id); ferr == nil {
			if need := e.needsPlatform(ia.MinPlatform); need != "" {
				return AppDetail{}, e.tooOld(ia.ID, need)
			}
			// The index did not say, the manifest is of a former format:
			// the same answer as an install gives (CAT-INST-36).
			if files, lerr := e.cfg.Catalog.loadPackage(ctx, v, ia); lerr == nil {
				if n, former := catalog.FormerManifest(files[manifestFile]); former {
					return AppDetail{}, formerApp(ia, n, true)
				}
			}
		}
		return AppDetail{}, err
	}
	m := a.Manifest
	installed, err := e.installedCatalogApps(ctx)
	if err != nil {
		return AppDetail{}, fault(CodeInternal, err, "读应用列表失败")
	}
	kinds := e.planKinds()
	ep := primaryEndpoint(m)
	exposure := m.Exposure.Default
	if exposure == "" {
		exposure = catalog.DefaultExposure
	}
	d := AppDetail{
		ID: m.ID, Version: m.Release.Version, Versions: []string{m.Release.Version}, Name: m.App.Name, Summary: m.App.Summary,
		Category: m.App.Category, Homepage: m.App.Homepage, License: m.App.License, SourceCode: m.App.SourceCode,
		MemoryMB: m.Resources.MemoryMB, DiskMB: m.Resources.DiskMB, Platforms: m.Resources.Platforms,
		Supported: e.archSupported(m.Resources.Platforms), SizeMB: a.Index.SizeMB, Exposure: exposure,
		Delivery: m.Delivery, Provides: a.Index.Provides, Uses: useViews(m, kinds), ProvidesDetail: provideViews(m, kinds), Installed: []string{},
		Command: "acornfox install " + m.ID + catalogFlag(a.Origin), Params: []ParamView{}, Source: a.Source(),
		Platform:      e.appPlatformViews(ctx, m),
		NeedsPlatform: e.needsPlatform(a.Index.MinPlatform, m.Lifecycle.MinPlatform),
	}
	if icon, ok := a.Files[m.App.Icon]; ok && m.App.Icon != "" {
		d.Icon, d.IconType = iconView(m.App.Icon, icon)
	}
	if len(m.Release.Images) > 0 {
		d.Image = m.Release.Images[0].Repository + ":" + m.Release.Images[0].Tag
	}
	if ep != nil {
		d.Port = ep.Port
	}
	for _, s := range installed[m.ID] {
		if s.CatalogOrigin == a.Origin {
			d.Installed = append(d.Installed, s.Name)
		}
	}
	for _, p := range m.Params {
		if p.Hidden {
			continue
		}
		pv := ParamView{Key: p.Key, Type: p.Type, Label: p.Label, Hint: p.Hint, Default: p.Default, Options: p.Options,
			Pattern: p.Pattern, Required: p.Required, Generated: p.Generate != nil, Editable: p.Editable}
		d.Params = append(d.Params, pv)
	}
	return d, nil
}

// iconView returns the icon as the detail view carries it. SVG stays text; PNG
// is base64 so the bytes survive JSON. Any other type is left out and the
// console falls back to the name's initials.
func iconView(name string, b []byte) (icon, typ string) {
	switch strings.ToLower(path.Ext(name)) {
	case ".svg":
		return string(b), "svg"
	case ".png":
		return base64.StdEncoding.EncodeToString(b), "png"
	}
	return "", ""
}
