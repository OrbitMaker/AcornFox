package appinstall

// Which installed apps have fallen behind: their settings are not what the
// platform would give them now (the machine's time zone or download source
// changed, a platform domain was set, a service they use changed). Read-only
// (GET /v1/platform/affected, `acornfox apps affected`). Applying is the
// user's go-ahead per app (`acornfox apps reapply APP`, or `--all`, which
// does them one after the other and stops at the first that fails).

import (
	"context"
	"sort"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// AffectedApp is one app that is behind (or, with a variable asked for,
// that uses it).
type AffectedApp struct {
	App       string `json:"app"`
	CatalogID string `json:"catalog_id"`
	// Uses: the manifest reads the variable that was asked for (always true
	// without one).
	Uses bool `json:"uses"`
	// Differs: the value the app has is not the one it would get now.
	Differs bool         `json:"differs"`
	Vars    []ReapplyVar `json:"vars"`
	Restart bool         `json:"restart"`
	// Blocked is why it cannot be applied now ("" when it can).
	Blocked string `json:"blocked,omitempty"`
	Summary string `json:"summary"`
}

// Affected is GET /v1/platform/affected.
type Affected struct {
	// Var is the variable asked for ("" none: every app that is behind).
	Var   string        `json:"var"`
	Apps  []AffectedApp `json:"apps"`
	Notes []string      `json:"notes"`
}

// Affected lists the installed catalog apps that are behind. With name (a
// variable: platform.<name>, params.<key> is not meaningful here,
// uses.<need>.<item>, or service:<service name>) it lists the apps that read
// it, whether they differ or not, so that the user sees who a change reaches
// before making it.
func (e *Engine) Affected(ctx context.Context, name string) (Affected, error) {
	out := Affected{Var: name, Apps: []AffectedApp{}, Notes: []string{}}
	want := strings.TrimSpace(name)
	var svcUsers map[string]bool
	switch {
	case want == "":
	case strings.HasPrefix(want, "service:"):
		reg, err := e.Registry()
		if err != nil {
			return out, fault(CodeInternal, err, "读不了服务登记表")
		}
		bs, err := reg.ListBindings(ctx, state.BindingFilter{Service: strings.TrimPrefix(want, "service:")})
		if err != nil {
			return out, fault(CodeInternal, err, "读服务登记表失败")
		}
		svcUsers = map[string]bool{}
		for _, b := range bs {
			svcUsers[b.App] = true
		}
	case strings.HasPrefix(want, "platform."):
		if _, ok := catalog.LookupPlatformVar(strings.TrimPrefix(want, "platform.")); !ok {
			return out, fault(CodeBadPlatform, nil, "没有平台变量 %s（acornfox catalog platform-vars 看有哪些）", want)
		}
	case strings.HasPrefix(want, "uses."):
	default:
		return out, fault(CodeBadParams, nil, "变量要写成 platform.名字、uses.需要名.项 或 service:服务名，不是 %q", want)
	}
	apps, err := e.cfg.Store.ListApps(ctx)
	if err != nil {
		return out, fault(CodeInternal, err, "读应用列表失败")
	}
	for _, a := range apps {
		if a.CatalogID == "" {
			continue
		}
		m := e.appManifest(ctx, a.Name)
		if m == nil {
			continue
		}
		uses := want == ""
		switch {
		case svcUsers != nil:
			uses = svcUsers[a.Name]
		case strings.HasPrefix(want, "platform."):
			for _, n := range catalog.PlatformRefs(m) {
				if "platform."+n == want {
					uses = true
				}
			}
		case strings.HasPrefix(want, "uses."):
			uses = manifestUsesItem(m, want)
		}
		if !uses {
			continue
		}
		row := AffectedApp{App: a.Name, CatalogID: a.CatalogID, Uses: uses, Vars: []ReapplyVar{}}
		plan, perr := e.ReapplyPlan(ctx, ReapplyRequest{App: a.Name})
		if perr != nil {
			var f *Fault
			if want == "" {
				continue // nothing known about it: not listed as behind
			}
			row.Blocked = perr.Error()
			if asF := AsFault(perr); asF != nil {
				f = asF
				row.Blocked = f.Message
			}
			out.Apps = append(out.Apps, row)
			continue
		}
		row.Vars, row.Restart, row.Summary = plan.Vars, plan.Restart, plan.Summary
		if row.Vars == nil {
			row.Vars = []ReapplyVar{}
		}
		for _, v := range plan.Vars {
			if want == "" || v.Name == want || svcUsers != nil || (strings.HasPrefix(want, "uses.") && v.Name == want) {
				row.Differs = true
			}
		}
		if want == "" && !row.Differs {
			continue
		}
		out.Apps = append(out.Apps, row)
	}
	sort.Slice(out.Apps, func(i, j int) bool { return out.Apps[i].App < out.Apps[j].App })
	if len(out.Apps) == 0 {
		if want == "" {
			out.Notes = append(out.Notes, "没有落在后面的应用：每个应用里用的设置都和现在算出来的一致")
		} else {
			out.Notes = append(out.Notes, "没有应用用到 "+want)
		}
	}
	return out, nil
}
