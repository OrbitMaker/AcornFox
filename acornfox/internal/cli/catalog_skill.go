package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/acornfox/acornfox/internal/client"
)

const catalogSkillUsage = `用法：acornfox catalog skill 应用或解决方案编号 [--recipe] [--json]

用途：取应用中心里某个应用（或解决方案）的“操作手册”：一份 Markdown，写这个应用特有的经验——装之前要问用户什么、
  装的时候要注意什么、装好以后怎么用起来（说给用户听的话）、怎么确认真的能用、常见问题、升级要注意什么、哪些地方要停下问用户。
  手册由经营系统写好、在沙箱里验过，随签名的目录发到这台服务器（同步时核对 sha256 和大小，对不上的不用）。
  给 AI 工作台用：装或用应用中心里的某个应用、解决方案之前先运行这条，有手册就照它做；
  手册和通用 Skill 的规则冲突时，以通用 Skill 为准。
  没有手册、或这台服务器还没取到可用的手册时，说明情况并退出码 0：照通用 Skill 做就行，不算出错。
参数：
  应用或解决方案编号   先按应用找，找不到再按解决方案（配方）找
  --recipe             只按解决方案（配方）找（应用和解决方案编号相同时用）
  --json               机器可读（也可以写在 catalog 前面：acornfox --json catalog skill …）
副作用：无，只读（读这台服务器已经同步下来的目录，不联网；离线也能用）。
输出（--json）：{"ok":true,"id":…,"kind":"app|recipe","has_skill":true|false,"version":"这份手册对哪个版本验过",
  "checked":"通过沙箱验证的日期","size":字节数,"text":"手册全文","code":"CAT-SKILL-… 手册列了但这台机器上用不了时才有","note":"没有手册时的一句话"}。
退出码：0 取到了，或没有手册；2 参数不对；3 连不上服务器；4 目录里没有这个编号。
出错编号：CAT-SKILL-01 列了手册但没取到、02 取到的对不上已丢弃、03 超过 32 KB（都不挡安装；acornfox faults 编号 看怎么办）。
例子：
  acornfox --json catalog skill demo-app
  acornfox --json catalog skill my-solution --recipe`

func (a *app) cmdCatalogSkill(ctx context.Context, args []string) int {
	fs := a.catalogFlags("catalog skill", catalogSkillUsage)
	onlyRecipe := fs.Bool("recipe", false, "只按解决方案（配方）找")
	asJSON := fs.Bool("json", false, "机器可读输出")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if *asJSON {
		a.out.json = true
	}
	if len(pos) != 1 {
		return a.out.usageError("用法：acornfox catalog skill 应用或解决方案编号 [--recipe]")
	}
	api, _, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	sk, ok := api.(client.SkillAPI)
	if !ok {
		return a.out.fail(&client.Error{Diag: client.Diagnosis{Stage: "server", Code: client.CodeServerTooOld, Message: "这个连接不支持操作手册", Hint: "升级 AcornFox"}})
	}
	id := pos[0]
	var v client.Skill
	kinds := []string{"app", "recipe"}
	if *onlyRecipe {
		kinds = []string{"recipe"}
	}
	for i, kind := range kinds {
		v, err = sk.CatalogSkill(ctx, kind, id)
		if err == nil {
			break
		}
		var ce *client.Error
		if errors.As(err, &ce) && ce.Status == 404 && ce.Diag.Code != "CAT-INST-01" {
			// A server that does not have the route (older than this command).
			return a.out.fail(&client.Error{Diag: client.Diagnosis{Stage: "server", Code: client.CodeServerTooOld, Message: "这台服务器还不认识操作手册", Hint: "升级 AcornFox"}})
		}
		// Not an app of that id: try a solution (unless --recipe was said).
		if i < len(kinds)-1 && errors.As(err, &ce) && ce.Diag.Code == "CAT-INST-01" {
			continue
		}
		if errors.As(err, &ce) && ce.Diag.Code == "CAT-INST-01" {
			return a.failDiag(client.Diagnosis{
				Stage: "catalog", Code: "not_found",
				Message: fmt.Sprintf("目录里没有应用或解决方案 %s", id),
				Hint:    "acornfox catalog list 看有哪些；目录太旧就 acornfox catalog sync",
			}, exitServerError)
		}
		return a.out.fail(err)
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"id": v.ID, "kind": v.Kind, "has_skill": v.HasSkill, "version": v.Version, "checked": v.Checked,
			"size": v.Size, "text": v.Text, "code": v.Code, "note": v.Note})
		return exitOK
	}
	what := "应用"
	if v.Kind == "recipe" {
		what = "解决方案"
	}
	if !v.HasSkill {
		a.out.human("%s %s：%s", what, v.ID, v.Note)
		if v.Code != "" {
			a.out.human("编号 %s（acornfox faults %s 看怎么办）", v.Code, v.Code)
		}
		return exitOK
	}
	head := fmt.Sprintf("%s %s 的操作手册", what, v.ID)
	var facts []string
	if v.Version != "" {
		facts = append(facts, "对版本 "+v.Version+" 验过")
	}
	if v.Checked != "" {
		facts = append(facts, v.Checked+" 通过沙箱验证")
	}
	if len(facts) > 0 {
		head += "（" + strings.Join(facts, "，") + "）"
	}
	a.out.human("%s", head)
	a.out.human("")
	a.out.human("%s", strings.TrimRight(v.Text, "\n"))
	return exitOK
}
