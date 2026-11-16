package appinstall

// Choosing an app's exposure at install time (Request.Expose, CLI
// `install … --expose local|lan|public`, POST /v1/catalog/install
// "expose").
//
// Without it an app gets what this machine gives new apps (Config.
// DefaultExposure: lan on a LAN server installed fresh, local otherwise)
// when its manifest says local or nothing, the catalog's standard; a
// manifest that says internal, lan or public keeps it (manifestExposure).
// With it the app is created with that
// exposure instead, so the platform variables about its address (app.url,
// app.scheme, app.host) are right from the start: changing it afterwards
// (`acornfox app set --expose`) does not rewrite them in this version.
//
// The rules are those of `app set --expose`: local, lan or public (afx_cloud
// is not open yet); an app only on the internal network keeps no exposure
// of its own and refuses one. In a recipe it applies to every app the task
// installs that is not internal (the apps already installed are used as
// they are); a recipe with none such refuses it. Opening an app to the
// network is a write the user must have agreed to (the Skill asks first).
//
// Errors: CAT-INST-02 (CodeBadParams) with the reason in words, before
// anything is created. Side effects: none here; create applies it.

import (
	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// manifestExposure is the exposure an app of manifest m gets when the
// request does not choose one: the manifest's own when it is internal, lan
// or public; otherwise (local or nothing, the catalog's standard) what this
// machine gives new apps, Config.DefaultExposure, or local when that is
// unset. An app installed already keeps the exposure it has.
func (e *Engine) manifestExposure(m *catalog.Manifest) string {
	if m != nil {
		switch d := m.Exposure.Default; d {
		case "", catalog.DefaultExposure:
		default:
			return d
		}
	}
	switch e.cfg.DefaultExposure {
	case state.ExposureLAN, state.ExposureLocal:
		return e.cfg.DefaultExposure
	}
	return catalog.DefaultExposure
}

// checkExpose validates the request's exposure and marks each app of plan
// that gets it. "" changes nothing.
func checkExpose(expose string, plan []*planned, recipe bool) error {
	switch expose {
	case "":
		return nil
	case state.ExposureLocal, state.ExposureLAN, state.ExposurePublic:
	case state.ExposureAFXCloud:
		return fault(CodeBadParams, nil, "--expose afx_cloud（经 AFX 云的公网地址）还没开放，目前可选 local、lan、public")
	default:
		return fault(CodeBadParams, nil, "--expose 只能是 local（只本机）、lan（本机和局域网）或 public（所有网卡 + 域名），不是 %q", expose)
	}
	marked := 0
	for _, p := range plan {
		if p.reuse {
			continue // already installed: used as it is
		}
		if internalManifest(p.app.Manifest) {
			if !recipe {
				return fault(CodeBadParams, nil, "%s 只在内部网络（给别的应用用，没有网页可以打开），不能指定 --expose：去掉它再装", p.name)
			}
			continue
		}
		p.expose = expose
		marked++
	}
	if marked == 0 {
		return fault(CodeBadParams, nil, "这个配方这次要装的应用都只在内部网络（或者已经装好、照原样用），没有哪个能用 --expose：去掉它再装")
	}
	return nil
}
