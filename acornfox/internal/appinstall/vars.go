package appinstall

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// Looking at the variables one installed app was configured with
// (GET /v1/apps/{app}/vars, `acornfox apps vars <app>`). Read-only.
//
// What it reads: the app's saved parameters (encrypted; they hold the
// platform values under "@platform", the user's platform changes under
// "@platform_user" and each parameter's origin under "@origins") and the
// manifest the app was installed with (app_catalog_manifests). With a
// manifest every row gets the class the catalog's variable report gives it;
// without one only the saved values can be listed, every custom value is
// masked like a secret (nothing says which ones are), and a note says so.
// Changing a value is not implemented: CanChange is always false.

// Where a value came from (AppVar.Origin).
const (
	OriginUser         = "user"          // the user gave it in the install request
	OriginRecipe       = "recipe"        // the recipe preset it (or wired the model source)
	OriginDefault      = "default"       // the manifest's default
	OriginGenerated    = "generated"     // the platform generated it (passwords, keys)
	OriginPlatform     = "platform"      // the platform worked it out for this machine
	OriginPlatformUser = "platform_user" // a platform variable the user changed (--set platform.x=…)
	OriginRestored     = "restored"      // put back from the app deleted earlier with its data kept
	OriginInstallTime  = "install_time"  // installed before origins were recorded: the value at install time
	OriginUnset        = "unset"         // nothing given and no default
	OriginOldData      = "old_data"      // the old data still holds its own value; the platform does not know it
)

// OriginLabels gives each origin its Chinese name.
var OriginLabels = map[string]string{
	OriginUser:         "用户填的",
	OriginRecipe:       "配方预填的",
	OriginDefault:      "清单默认值",
	OriginGenerated:    "平台生成的",
	OriginPlatform:     "平台算的",
	OriginPlatformUser: "用户改的",
	OriginRestored:     "接上旧数据时恢复的",
	OriginInstallTime:  "装时的值（那时还不记是谁给的）",
	OriginUnset:        "没填",
	OriginOldData:      "沿用旧数据里的（平台不知道）",
}

// originsKey is where Values keep each parameter's origin, next to the
// parameters ("@" can never start a parameter key).
const originsKey = "@origins"

// paramOrigins says, for every plain parameter of m, where resolveParams
// takes its value from, given the user's values and which of them the recipe
// preset. llm_provider parameters are filled in by resolveLLM.
func paramOrigins(m *catalog.Manifest, user map[string]any, preset map[string]bool) map[string]string {
	out := map[string]string{}
	for _, p := range m.Params {
		if p.Type == "llm_provider" {
			continue
		}
		s, origin := "", OriginUnset
		if raw, given := user[p.Key]; given && raw != nil {
			s, _ = scalarText(raw)
			origin = OriginUser
			if preset[p.Key] {
				origin = OriginRecipe
			}
		} else if p.Default != nil {
			s, _ = scalarText(p.Default)
			origin = OriginDefault
		}
		if s == "" {
			origin = OriginUnset
			if p.Generate != nil {
				origin = OriginGenerated
			}
		}
		out[p.Key] = origin
	}
	return out
}

// AppVars is GET /v1/apps/{app}/vars.
type AppVars struct {
	App string `json:"app"`
	// FromCatalog is false for an app that was not installed from the app
	// center: it has no such variables (Notes says where to look instead).
	FromCatalog bool        `json:"from_catalog"`
	CatalogID   string      `json:"catalog_id"`
	Version     string      `json:"version"`
	Source      SourceRef   `json:"source"`
	Manifest    ManifestRef `json:"manifest"`
	// Revealed: secrets are shown in full (reveal=true).
	Revealed bool `json:"revealed"`
	// CanChange is false in this version: settings cannot be changed after
	// the install yet.
	CanChange bool     `json:"can_change"`
	Vars      []AppVar `json:"vars"`
	Notes     []string `json:"notes"`
}

// ManifestRef says whether the manifest the app was installed with is saved,
// and how it got there.
type ManifestRef struct {
	Saved       bool       `json:"saved"`
	Source      string     `json:"source"` // install | backfill | "" (not saved)
	SourceLabel string     `json:"source_label"`
	SHA256      string     `json:"sha256,omitempty"`
	SavedAt     *time.Time `json:"saved_at,omitempty"`
}

// AppVar is one variable of an installed app.
type AppVar struct {
	// Name is params.<key> (params.<key>.<field> for a model parameter) or
	// platform.<name>.
	Name       string `json:"name"`
	Layer      string `json:"layer"` // system | custom
	LayerLabel string `json:"layer_label"`
	// Class is the variable report's class (catalog.Class*); "" when the
	// manifest is not saved.
	Class      string `json:"class"`
	ClassLabel string `json:"class_label"`
	Label      string `json:"label,omitempty"`
	Value      string `json:"value"`
	Secret     bool   `json:"secret"`
	// Masked: Value is hidden (all of it, or all but the last 4 characters).
	Masked      bool   `json:"masked"`
	Origin      string `json:"origin"`
	OriginLabel string `json:"origin_label"`
	// Editable is what the manifest says about changing a parameter after
	// the install (never | always | reinstall, "" when it says nothing or is
	// not saved); for a platform variable, install_only when the user may
	// set it at install time, else never.
	Editable      string `json:"editable"`
	EditableLabel string `json:"editable_label"`
	// Used: the manifest refers to this platform variable (only for system
	// rows of an app whose manifest is saved).
	Used *bool `json:"used,omitempty"`
}

const (
	editInstallOnly = "install_only"
	editNever       = "never"
)

var layerLabels = map[string]string{catalog.LayerSystem: "系统变量", catalog.LayerCustom: "自定义变量"}

var manifestSourceLabels = map[string]string{
	state.ManifestFromInstall:  "安装时保存的",
	state.ManifestFromBackfill: "补存的（装的时候还不存清单，后来从本机的目录里找到的）",
}

func editableLabel(e string, haveManifest bool) string {
	switch e {
	case "never":
		return "不能改"
	case "always":
		return "清单写的是随时能改"
	case "reinstall":
		return "清单写的是重装才能改"
	case editInstallOnly:
		return "只能在安装时用 --set platform.名字=值 改"
	}
	if !haveManifest {
		return "清单没存，不知道"
	}
	return "清单没写"
}

// maskSecret hides a secret: all but the last 4 characters, or everything
// when it is too short for 4 characters to be safe to show.
func maskSecret(v string) string {
	if v == "" {
		return ""
	}
	r := []rune(v)
	if len(r) < 12 {
		return Masked
	}
	return Masked + string(r[len(r)-4:])
}

// internalKey reports whether a saved-parameters key is the engine's own
// bookkeeping rather than a variable.
func internalKey(k string) bool {
	return strings.HasPrefix(k, "@") || strings.HasPrefix(k, "_acornfox_")
}

func stringMap(v any) (map[string]string, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(m))
	for k, x := range m {
		if s, ok := x.(string); ok {
			out[k] = s
		}
	}
	return out, true
}

// AppVars lists the variables app was installed with. Secrets are masked
// unless reveal. Only the API server's authenticated entries call it (the
// same ones as FirstLogin).
func (e *Engine) AppVars(ctx context.Context, app string, reveal bool) (AppVars, error) {
	if !state.ValidAppName(app) {
		return AppVars{}, fault(CodeNotCatalogApp, nil, "应用名不合法")
	}
	a, err := e.cfg.Store.GetApp(ctx, app)
	if errors.Is(err, state.ErrNotFound) {
		return AppVars{}, fault(CodeNotCatalogApp, nil, "没有叫 %s 的应用（acornfox apps 看有哪些）", app)
	}
	if err != nil {
		return AppVars{}, fault(CodeInternal, err, "读应用失败")
	}
	out := AppVars{App: app, Vars: []AppVar{}, Notes: []string{}, Revealed: reveal, Source: sourceRef("", "")}
	if a.CatalogID == "" {
		out.Source = SourceRef{}
		out.Notes = append(out.Notes, app+" 不是从应用中心装的，没有应用中心的变量。它的环境变量用 acornfox env list --app "+app+" 看")
		return out, nil
	}
	out.FromCatalog, out.CatalogID, out.Version = true, a.CatalogID, a.CatalogVersion
	name := ""
	if a.CatalogOrigin != "" && e.cfg.Catalog != nil {
		name, _ = e.cfg.Catalog.OriginState(a.CatalogOrigin)
	}
	out.Source = sourceRef(a.CatalogOrigin, name)
	saved, err := e.savedParams(ctx, app)
	if errors.Is(err, state.ErrNotFound) {
		// The parameters are saved as the install's last step.
		out.Manifest.SourceLabel = "没存"
		out.Notes = append(out.Notes, app+" 还没有保存的安装参数：它可能还在安装（acornfox install status 看进度），装好后再看")
		return out, nil
	}
	if err != nil {
		return AppVars{}, fault(CodeInternal, err, "保存的参数解不开（数据目录里的 secrets/catalog.key 换过？）")
	}

	// The manifest the app was installed with, when it is saved and intact.
	var m *catalog.Manifest
	if cm, err := e.cfg.Store.GetCatalogManifest(ctx, app); err == nil {
		if state.ManifestSHA256(cm.Manifest) != cm.SHA256 {
			out.Notes = append(out.Notes, "保存的清单和它的校验值对不上，这次不用它")
		} else if pm, perr := catalog.ParseManifest([]byte(cm.Manifest)); perr != nil {
			out.Notes = append(out.Notes, "保存的清单读不出来，这次不用它")
		} else {
			m = pm
			at := cm.SavedAt
			out.Manifest = ManifestRef{Saved: true, Source: cm.Source, SourceLabel: manifestSourceLabels[cm.Source], SHA256: cm.SHA256, SavedAt: &at}
		}
	} else if !errors.Is(err, state.ErrNotFound) {
		return AppVars{}, fault(CodeInternal, err, "读保存的清单失败")
	}
	if m == nil {
		out.Manifest.SourceLabel = "没存"
		out.Notes = append(out.Notes, "清单没存，分不出类别：这个应用装的时候还不保存清单，本机的目录里也找不到它那一版的应用包。下面只列保存的值，自定义变量分不出哪些是密码，一律按密码遮住（--reveal 显示）")
	}

	origins, haveOrigins := stringMap(saved[originsKey])
	platform := Values(saved).platformMap()
	platformUser, haveUser := stringMap(saved[platformUserKey])
	if !haveOrigins || !haveUser {
		out.Notes = append(out.Notes, "这个应用是旧版本装的，那时不记每个值是谁给的：分不出哪些是用户改的，这些值标成“装时的值”")
	}

	rows := map[string]*AppVar{}
	add := func(v AppVar) {
		v.LayerLabel = layerLabels[v.Layer]
		v.ClassLabel = catalog.VarClassLabels[v.Class]
		v.OriginLabel = OriginLabels[v.Origin]
		v.EditableLabel = editableLabel(v.Editable, m != nil)
		if v.Secret && !reveal && v.Value != "" {
			v.Value, v.Masked = maskSecret(v.Value), true
		}
		rows[v.Name] = &v
	}
	originOf := func(key string) string {
		if !haveOrigins {
			return OriginInstallTime
		}
		if o := origins[key]; o != "" {
			return o
		}
		return OriginInstallTime
	}
	// llmRows adds one row per field of a model parameter.
	llmRows := func(key, class, label, editable string, raw any) {
		b, _ := json.Marshal(raw)
		var c LLMChoice
		if json.Unmarshal(b, &c) != nil {
			return
		}
		src := originOf(key)
		// With the local gateway the address and the key are the platform's
		// doing (the gateway's address inside Docker, a token issued in it).
		issued := src
		if haveOrigins && c.Source == LLMLocalGateway {
			issued = OriginPlatform
		}
		model := src
		if o := origins[key+".model"]; haveOrigins && o != "" {
			model = o
		}
		fields := []struct {
			name, value, origin string
			secret              bool
		}{
			{"source", c.Source, src, false},
			{"app", c.App, src, false},
			{"base_url", c.BaseURL, issued, false},
			{"api_key", c.APIKey, issued, true},
			{"model", c.Model, model, false},
		}
		for _, f := range fields {
			if f.name == "app" && f.value == "" {
				continue // only the local gateway names an app
			}
			add(AppVar{Name: "params." + key + "." + f.name, Layer: catalog.LayerCustom, Class: class, Label: label,
				Value: f.value, Secret: f.secret || m == nil, Origin: f.origin, Editable: editable})
		}
	}

	if m != nil {
		secretRefs := secretParams(m)
		used := map[string]bool{}
		for _, r := range catalog.ClassifyVars(m) {
			if r.Layer == catalog.LayerSystem {
				used[r.Name] = true
				continue
			}
			key := strings.TrimPrefix(r.Name, "params.")
			var prm catalog.Param
			for _, p := range m.Params {
				if p.Key == key {
					prm = p
				}
			}
			raw, have := saved[key]
			if prm.Type == "llm_provider" {
				if have && raw != nil {
					llmRows(key, r.Class, r.Label, prm.Editable, raw)
				} else {
					add(AppVar{Name: r.Name, Layer: r.Layer, Class: r.Class, Label: r.Label, Origin: OriginUnset, Editable: prm.Editable})
				}
				continue
			}
			v := AppVar{Name: r.Name, Layer: r.Layer, Class: r.Class, Label: r.Label, Secret: r.Secret || secretRefs[key], Editable: prm.Editable}
			v.Value, _ = raw.(string)
			switch {
			case !have:
				// Dropped when the install took over old data it could not
				// re-initialize (see saveSecrets).
				v.Origin = OriginOldData
			case haveOrigins:
				v.Origin = originOf(key)
			case prm.Hidden && prm.Generate != nil:
				v.Origin = OriginGenerated // the user can never fill a hidden one
			default:
				v.Origin = OriginInstallTime
			}
			add(v)
		}
		// The default chat model is used without being referred to: a model
		// parameter that can take the local gateway falls back to it.
		for _, p := range m.Params {
			if p.Type == "llm_provider" && p.LLM != nil && p.LLM.LocalGateway {
				used["platform."+catalog.PVLLMModelChat] = true
			}
		}
		names := map[string]bool{}
		for n := range used {
			if rest, ok := strings.CutPrefix(n, "platform."); ok {
				names[rest] = true
			}
		}
		for n := range platform {
			names[n] = true
		}
		for n := range names {
			u := used["platform."+n]
			add(e.platformRow(n, platform, platformUser, haveUser, catalog.ClassSystem, &u))
		}
	} else {
		for k, raw := range saved {
			if internalKey(k) {
				continue
			}
			if _, isMap := raw.(map[string]any); isMap {
				llmRows(k, "", "", "", raw)
				continue
			}
			s, _ := raw.(string)
			add(AppVar{Name: "params." + k, Layer: catalog.LayerCustom, Value: s, Secret: true, Origin: OriginInstallTime})
		}
		for n := range platform {
			add(e.platformRow(n, platform, platformUser, haveUser, "", nil))
		}
	}

	names := make([]string, 0, len(rows))
	for n := range rows {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out.Vars = append(out.Vars, *rows[n])
	}
	out.Notes = append(out.Notes, "改设置的功能还没有：这里只能看，不能改。能改的平台变量只在安装时用 --set platform.名字=值 改")
	if !reveal {
		out.Notes = append(out.Notes, "密码和 Key 只显示后 4 位（不到 12 个字符的全遮）；要看全加 --reveal（接口 ?reveal=true）")
	}
	return out, nil
}

// platformRow is the row of platform variable name.
func (e *Engine) platformRow(name string, platform, user map[string]string, haveUser bool, class string, used *bool) AppVar {
	v := AppVar{Name: "platform." + name, Layer: catalog.LayerSystem, Class: class, Used: used, Editable: editNever}
	if pv, ok := catalog.LookupPlatformVar(name); ok {
		v.Label, v.Secret = pv.Label, pv.Secret
		if pv.UserCanSet {
			v.Editable = editInstallOnly
		}
	}
	val, have := platform[name]
	v.Value = val
	switch {
	case !have:
		v.Origin = OriginUnset
	case !haveUser:
		v.Origin = OriginInstallTime
	default:
		v.Origin = OriginPlatform
		if _, changed := user[name]; changed {
			v.Origin = OriginPlatformUser
		}
	}
	return v
}

// secretParams lists the parameters the manifest treats as secret somewhere
// (an env entry or a first-login field marked secret reads them), besides
// those of type secret.
func secretParams(m *catalog.Manifest) map[string]bool {
	out := map[string]bool{}
	mark := func(from string, secret bool) {
		if name, ok := strings.CutPrefix(from, "params."); ok && secret {
			out[strings.SplitN(name, ".", 2)[0]] = true
		}
	}
	for _, ev := range m.Env {
		mark(ev.From, ev.Secret)
	}
	for _, lf := range m.FirstLogin {
		mark(lf.From, lf.Secret)
	}
	return out
}
