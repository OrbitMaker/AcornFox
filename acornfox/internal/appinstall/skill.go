package appinstall

import (
	"context"
	"errors"

	"github.com/acornfox/acornfox/internal/catalog"
)

// SkillView is GET /v1/catalog/skills/{kind}/{id}: the operating manual of an
// app or a solution (catalog/skill.go), or why there is none.
//
// HasSkill false is not an error: the AI workbench then follows the general
// Skill alone. Note says why in one sentence; Code is the CAT-SKILL-* when the
// catalog lists a manual that is not usable on this machine (not fetched yet,
// does not match): then a catalog sync may fetch it.
type SkillView struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // app | recipe
	HasSkill bool   `json:"has_skill"`
	Version  string `json:"version"`
	Checked  string `json:"checked"`
	Size     int64  `json:"size"`
	Text     string `json:"text"`
	Code     string `json:"code,omitempty"`
	Note     string `json:"note,omitempty"`
}

// Skill gives the operating manual of the app or solution id (kind: app,
// recipe or solution). CAT-INST-02 for another kind, CAT-INST-01 when no
// catalog in use has that app or solution. Only the public catalog has
// manuals (they come with its display content); an id of a private catalog
// answers "no manual".
func (e *Engine) Skill(ctx context.Context, kind, id string) (SkillView, error) {
	k := catalog.NormalizeSkillKind(kind)
	if k == "" {
		return SkillView{}, fault(CodeBadParams, nil, "手册的种类要写 app 或 recipe（solution 也行），现在是 %q", kind)
	}
	vs, err := e.cfg.Catalog.views()
	if err != nil {
		return SkillView{}, fault(CodePackage, err, "目录读不了：%v", err)
	}
	found, public := false, false
	for _, v := range vs {
		if k == catalog.SkillKindApp {
			for _, a := range v.idx.Apps {
				if a.ID == id {
					found, public = true, public || v.origin == ""
				}
			}
		} else {
			for _, r := range v.idx.AllRecipes() {
				if r.ID == id {
					found, public = true, public || v.origin == ""
				}
			}
		}
	}
	if !found {
		what := "应用"
		if k == catalog.SkillKindRecipe {
			what = "解决方案"
		}
		return SkillView{}, fault(CodeUnknown, nil, "目录里没有%s %s", what, id)
	}
	out := SkillView{ID: id, Kind: k}
	if !public {
		out.Note = "这个编号只在私有目录里，私有目录没有操作手册，照通用 Skill 做"
		return out, nil
	}
	doc, err := e.cfg.Catalog.Skill(k, id)
	if err != nil {
		var ce *catalog.Error
		if errors.As(err, &ce) {
			out.Code = ce.Code
			out.Note = "目录里有这份操作手册，但这台机器上现在用不了（" + ce.Message + "）；先运行 acornfox catalog sync，还不行就照通用 Skill 做"
		}
		return out, nil
	}
	if doc == nil {
		out.Note = "这个没有专门的操作手册，照通用 Skill 做"
		return out, nil
	}
	out.HasSkill, out.Version, out.Checked, out.Size, out.Text = true, doc.Version, doc.Checked, doc.Size, doc.Text
	return out, nil
}
