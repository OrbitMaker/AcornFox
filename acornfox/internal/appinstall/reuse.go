package appinstall

// Installing an app on the data an earlier app of the same name left behind
// (reuse_data: deleted with its data kept, installed again with
// --reuse-data, the way to change an app's version until upgrades exist).
//
// The old app's setup comes back unless this request says otherwise:
//
//   - Parameters (passwords, keys, the admin name): the ones the user does not
//     give now get their old values, so the app finds its data initialized
//     with the same ones (and init is skipped).
//   - Platform variables the user had changed (--set platform.x=…): put back,
//     unless this request sets them again (this request wins). An app
//     installed before those changes were recorded apart from the machine's
//     values cannot tell them apart: the progress says so.
//   - The model source: with none chosen now, the old one when it still works
//     (the user's own key as it was; the same local gateway, while it runs —
//     when it is gone a required source is refused, CAT-INST-02; when it is
//     only stopped, CAT-INST-10 says to start it, before any task). The same
//     local gateway chosen again (or wired by a recipe) gets the old token
//     back when the gateway still has it with the same value (an app deleted
//     with its data kept keeps its token, see the API server's delete), even
//     when the user turned it off there (that is said, not undone);
//     otherwise a new token is issued.
//   - Config files (overwrite: install) that exist in the old data: kept as
//     they are when what the platform writes into them is the same as for the
//     old app (the user's and the app's own edits stay). When it changed (a
//     new model key or address, another source, a changed template), the
//     file is written anew and the old one is first copied aside
//     (<path>.bak-<UTC time>), so nothing is lost; a file that cannot be
//     copied is kept. Without a kept record nothing can be compared and an
//     existing file is kept. A file an earlier install on the same data
//     rewrote before it failed is written anew too (rewrittenSince): it is
//     that attempt's, not the old app's.
//
// Each of these says what it did in the progress (the create and files
// steps) and in the task's apps[]: carried, taken_gateway_token, kept_files,
// file_backups.
//
// Once the install succeeds the kept record goes: tokens of it that were not
// taken over are revoked, after checking the gateway still holds them with
// the same value (a reinstalled gateway may reuse the number for another
// app's token; the check reads the token with its value masked, which New
// API does not count against its limit for critical calls). One that cannot
// be revoked (its gateway stopped, its value not kept, the gateway said no)
// is said in carried and in the init step's message, with how the user
// removes it: it has no quota and no expiry, and nothing tries again. A
// successful install that does not reuse data drops a stale
// kept record of its name the same way (its data is gone, or the install
// would have been refused with CAT-INST-24).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"slices"
	"sort"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/installtiming"
	"github.com/acornfox/acornfox/internal/state"
)

// keptToken is the old app's choice for one llm parameter (its base_url and
// api_key are the old token's) and the token it had in that gateway (nil when
// none was recorded).
type keptToken struct {
	choice LLMChoice
	token  *TokenRef
}

// restoreKept puts back, for a reuse_data install, what was kept when the
// old app of this name was deleted with its data: every parameter the user
// did not give now gets its old value (the model source is chosen in
// resolveLLM, see keptLLM), and the platform variables the user had changed
// come back unless sets changes them again. Old data of another catalog app
// is refused (CAT-INST-24). No kept record (never from the catalog, or lost)
// is not an error: the install then goes on with new values and finds out at
// init.
func (e *Engine) restoreKept(ctx context.Context, p *planned, vals Values, sets map[string]string) error {
	k, err := e.cfg.Store.GetKeptCatalogSecrets(ctx, p.name)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fault(CodeInternal, err, "读保留的参数失败")
	}
	if k.CatalogID != p.app.Manifest.ID {
		return fault(CodeOldVolumes, nil, "留下的数据是 %s 的，不能接到 %s 上；用 --name 换个名字装", k.CatalogID, p.app.Manifest.ID)
	}
	plain, err := e.box.open(k.ParamsEnc)
	if err != nil {
		return fault(CodeInternal, err, "保留的参数解不开（数据目录里的 secrets/catalog.key 换过？）")
	}
	var old map[string]any
	if err := json.Unmarshal(plain, &old); err != nil {
		return fault(CodeInternal, err, "保留的参数格式不对")
	}
	var back []string
	for _, prm := range p.app.Manifest.Params {
		if prm.Type == "llm_provider" {
			continue // chosen in resolveLLM
		}
		if _, given := p.user[prm.Key]; given {
			continue
		}
		if s, ok := old[prm.Key].(string); ok {
			vals[prm.Key] = s
			p.setOrigin(prm.Key, OriginRestored)
			back = append(back, prm.Key)
		}
	}
	if len(back) > 0 {
		p.carried = append(p.carried, "恢复了删除时保留的参数（"+strings.Join(back, "、")+"），账号密码和以前一样")
	}
	p.carried = append(p.carried, restorePlatformSets(old, vals, sets)...)

	p.kept = keptValues(old)
	p.keptTokens = tokenRefs(old[savedTokensKey])
	p.rewritten = e.rewrittenSince(ctx, p.name, k)
	if k.Manifest != "" && state.ManifestSHA256(k.Manifest) == k.ManifestSHA256 {
		if m, err := catalog.ParseManifest([]byte(k.Manifest)); err == nil {
			p.keptManifest = m
		}
	}
	p.restored = true
	return nil
}

// restorePlatformSets puts the platform variables the old app's user had
// changed back into vals (the merged values and the user's changes), except
// those sets changes again, and returns what it says about them.
func restorePlatformSets(old map[string]any, vals Values, sets map[string]string) []string {
	oldUser, ok := stringMap(old[platformUserKey])
	if !ok {
		if _, had := old[platformKey]; had {
			return []string{"以前装它的版本不记哪些平台变量是你改过的，这次平台变量按本机的值；以前用 --set platform.名字=值 改过的，要这次再写一遍"}
		}
		return nil
	}
	merged, _ := vals[platformKey].(map[string]string)
	user, _ := vals[platformUserKey].(map[string]string)
	if merged == nil || user == nil {
		return nil
	}
	names := make([]string, 0, len(oldUser))
	for n := range oldUser {
		names = append(names, n)
	}
	sort.Strings(names)
	var back, again, dropped []string
	for _, n := range names {
		v := oldUser[n]
		if now, set := sets[n]; set {
			if strings.TrimSpace(now) != v {
				again = append(again, "platform."+n)
			}
			continue
		}
		// Only what this version still lets the user set, with a value it
		// still accepts.
		if checkPlatformSets(map[string]string{n: v}) != nil {
			dropped = append(dropped, "platform."+n)
			continue
		}
		merged[n], user[n] = v, v
		if pv, ok := catalog.LookupPlatformVar(n); ok && pv.Secret {
			back = append(back, "platform."+n)
		} else {
			back = append(back, "platform."+n+"="+v)
		}
	}
	var out []string
	if len(back) > 0 {
		out = append(out, "沿用以前改过的平台变量："+strings.Join(back, "、"))
	}
	if len(again) > 0 {
		out = append(out, "这次用 --set 重新写了 "+strings.Join(again, "、")+"，以这次的为准")
	}
	if len(dropped) > 0 {
		out = append(out, "以前改过的 "+strings.Join(dropped, "、")+" 这个版本不能这样改了，这次用平台的值")
	}
	return out
}

// rewrittenSince lists the config files of app that an install on the old
// data kept in k rewrote (copied aside, then written anew) and that then
// failed: the kept record is unchanged by a failed install, but the data now
// holds that attempt's files, so comparing the old app's settings with this
// request's no longer says what is in them. Read from the saved tasks
// started since the data was kept; a task that cannot be read is skipped
// (logged).
func (e *Engine) rewrittenSince(ctx context.Context, app string, k state.KeptCatalogSecrets) map[string]bool {
	tasks, err := e.cfg.Store.ListInstallTasks(ctx, state.TaskFailed, 200)
	if err != nil {
		e.cfg.Log.Warn("app center: read failed install tasks", "app", app, "err", err)
		return nil
	}
	var out map[string]bool
	for _, it := range tasks {
		if it.CreatedAt.Before(k.KeptAt) {
			continue
		}
		var v TaskView
		if err := json.Unmarshal([]byte(it.Data), &v); err != nil {
			e.cfg.Log.Warn("app center: read install task", "task", it.ID, "err", err)
			continue
		}
		for _, a := range v.Apps {
			if a.App != app || len(a.ReusedVolumes) == 0 {
				continue
			}
			for _, b := range a.FileBackups {
				if out == nil {
					out = map[string]bool{}
				}
				out[b.Path] = true
			}
		}
	}
	return out
}

// keptValues makes the old app's saved parameters renderable: a model
// parameter (an object in the JSON) becomes an *LLMChoice again.
func keptValues(old map[string]any) Values {
	out := Values{}
	for k, v := range old {
		if m, ok := v.(map[string]any); ok && !internalKey(k) {
			b, _ := json.Marshal(m)
			var c LLMChoice
			if json.Unmarshal(b, &c) == nil {
				out[k] = &c
				continue
			}
		}
		out[k] = v
	}
	return out
}

// tokenRefs reads the saved gateway tokens (savedTokensKey).
func tokenRefs(raw any) []TokenRef {
	if raw == nil {
		return nil
	}
	b, _ := json.Marshal(raw)
	var out []TokenRef
	_ = json.Unmarshal(b, &out)
	return out
}

// keptChoice is the old app's choice for llm parameter key (nil when none).
func keptChoice(kept Values, key string) *LLMChoice {
	c, _ := kept[key].(*LLMChoice)
	if c == nil || c.Source == "" {
		return nil
	}
	return c
}

// keptLLM decides, for a reuse_data install, what becomes of llm parameter
// prm given the old app's choice. c is this request's choice so far (nil:
// none; wantModel: a model named without a source).
//
//   - Nothing chosen: the old choice comes back (restored is true): the
//     user's own key as it was; the same local gateway while it runs (its
//     old token may be taken over, see takeOverToken). A gateway that is gone
//     is CAT-INST-02 for a required parameter; for an optional one the
//     ordinary choice follows.
//   - The same local gateway chosen again (or wired by a recipe): the old
//     token may be taken over, and the old model is kept when none is named.
//   - Anything else is this request's choice and is returned as it is.
//
// modelKept says the model is the old app's.
func (e *Engine) keptLLM(p *planned, prm catalog.Param, c *LLMChoice, wantModel string, providers []state.App) (out *LLMChoice, restored, modelKept bool, err error) {
	kc := keptChoice(p.kept, prm.Key)
	if kc == nil {
		return c, false, false, nil
	}
	live := func(name string) bool {
		_, ok := providerNamed(providers, name)
		return ok
	}
	model := func(have string) (string, bool) {
		switch {
		case have != "":
			return have, false
		case wantModel != "":
			return wantModel, false
		}
		return kc.Model, kc.Model != ""
	}
	if c == nil {
		switch kc.Source {
		case LLMOwn:
			if kc.BaseURL == "" || kc.APIKey == "" {
				return nil, false, false, nil
			}
			m, mk := model("")
			p.carried = append(p.carried, "大模型沿用原来的：自己的 Key（"+hostOf(kc.BaseURL)+"）")
			return &LLMChoice{Source: LLMOwn, BaseURL: kc.BaseURL, APIKey: kc.APIKey, Model: m}, true, mk, nil
		case LLMLocalGateway:
			if kc.App == "" {
				return nil, false, false, nil
			}
			if !live(kc.App) {
				if prm.Required {
					return nil, false, false, fault(CodeBadParams, nil, "接上旧数据重装：%s 原来用本机的 %s，现在本机没有它了，要重新选大模型（%s）：--llm local 用本机在运行的 New API，或 --llm own 填自己的 base_url、api_key、model", p.app.Manifest.ID, kc.App, prm.Key)
				}
				return nil, false, false, nil
			}
			if pa, _ := providerNamed(providers, kc.App); pa.Desired == state.DesiredStopped {
				// Still installed, only stopped: starting it is the way
				// (CAT-INST-10), not choosing again.
				return nil, false, false, stoppedGateway(kc.App, "接上旧数据重装："+p.app.Manifest.ID+" 原来用本机的 "+kc.App+"，它")
			}
			m, mk := model("")
			p.markTakeover(prm.Key, *kc)
			p.carried = append(p.carried, "大模型沿用原来的：本机的 "+kc.App)
			return &LLMChoice{Source: LLMLocalGateway, App: kc.App, Model: m}, true, mk, nil
		}
		return nil, false, false, nil
	}
	if c.Source == LLMLocalGateway && kc.Source == LLMLocalGateway && kc.App != "" && (c.App == "" || c.App == kc.App) &&
		c.BaseURL == "" && c.APIKey == "" && live(kc.App) {
		o := *c
		o.App = kc.App
		var mk bool
		o.Model, mk = model(c.Model)
		p.markTakeover(prm.Key, *kc)
		return &o, false, mk, nil
	}
	return c, false, false, nil
}

// markTakeover records that llm parameter key may take over the old token in
// kc's gateway.
func (p *planned) markTakeover(key string, kc LLMChoice) {
	if p.takeover == nil {
		p.takeover = map[string]keptToken{}
	}
	tk := keptToken{choice: kc}
	for i := range p.keptTokens {
		if p.keptTokens[i].Provider == kc.App {
			tok := p.keptTokens[i]
			tk.token = &tok
			break
		}
	}
	p.takeover[key] = tk
}

// hostOf is the host of a model address, for messages (never the key).
func hostOf(raw string) string {
	if u, err := neturl.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return "自己填的地址"
}

// takeOverToken points c (local gateway c.App) at the old app's token when
// the gateway still holds it with the same value, and says what it did.
// false: not taken over (a new token is to be issued).
func (t *taskRun) takeOverToken(ctx context.Context, p *planned, c *LLMChoice, tk keptToken) bool {
	e := t.e
	if tk.token == nil || tk.choice.APIKey == "" {
		p.carried = append(p.carried, "以前给它的令牌没有记录，在 "+c.App+" 里新建了一把")
		return false
	}
	g, err := e.gateway(ctx, c.App)
	if err != nil {
		p.carried = append(p.carried, "没能核对以前在 "+c.App+" 里给它的令牌，新建了一把")
		return false
	}
	same, status, err := e.adapter.CheckToken(ctx, g.base, g.user, g.pass, tk.token.ID, tk.choice.APIKey)
	if err != nil {
		e.cfg.Log.Info("app center: old gateway token not checked", "app", p.name, "provider", c.App, "id", tk.token.ID, "err", err)
		p.carried = append(p.carried, "没能核对以前在 "+c.App+" 里给它的令牌，新建了一把")
		return false
	}
	if !same {
		p.carried = append(p.carried, "以前在 "+c.App+" 里给它的令牌已经没有了（v0.5.0 删除应用时会吊销令牌，或者 "+c.App+" 重装过），新建了一把")
		return false
	}
	c.BaseURL, c.APIKey = g.inURL, tk.choice.APIKey
	tok := *tk.token
	p.taken = append(p.taken, tok)
	t.setApp(p.name, func(r *AppResult) { r.TakenToken = &tok })
	t.save(ctx)
	p.carried = append(p.carried, fmt.Sprintf("接着用以前在 %s 里给它的令牌（编号 %d）", c.App, tok.ID))
	if status != tokenStatusEnabled {
		// Taken over as it is: the user may have turned it off on purpose
		// (to stop spending); a new token would quietly undo that.
		p.carried = append(p.carried, fmt.Sprintf("这把令牌在 %s 里现在没有启用（状态 %d），它调不了模型；要用请到 %s 的“令牌”页启用 acornfox-%s", c.App, status, c.App, p.name))
	}
	return true
}

// fileChange compares what the platform writes into config file f this time
// (content) with what it wrote for the old app: the old manifest's template
// for the same file (this one's when the old manifest is not saved), filled
// with the kept values. changed lists the references whose value differs (or
// "模板" when only the template text did). ok is false when it cannot be
// told: no kept record, the old manifest has no such file (an existing file
// there is not the platform's), or the old template does not render.
func (p *planned) fileChange(f catalog.File, content string) (changed []string, ok bool) {
	if p.kept == nil {
		return nil, false
	}
	tmpl := f.Template
	if p.keptManifest != nil {
		found := false
		for _, of := range p.keptManifest.Files {
			if of.Volume == f.Volume && strings.TrimPrefix(of.Path, "./") == strings.TrimPrefix(f.Path, "./") {
				tmpl, found = of.Template, true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	oldContent, err := p.kept.render(tmpl, fileEscape(f.Path))
	if err != nil {
		return nil, false
	}
	if oldContent == content {
		return nil, true
	}
	seen := map[string]bool{}
	for _, r := range append(catalog.TemplateRefs(tmpl), catalog.TemplateRefs(f.Template)...) {
		if seen[r] {
			continue
		}
		seen[r] = true
		ov, oerr := p.kept.lookup(r)
		nv, nerr := p.vals.lookup(r)
		if ov != nv || (oerr == nil) != (nerr == nil) {
			changed = append(changed, r)
		}
	}
	sort.Strings(changed)
	if len(changed) == 0 {
		changed = []string{"模板"}
	}
	return changed, true
}

// notRevoked says that the old app's token tok could not be revoked, why,
// and how the user removes it: it has no quota and no expiry, and the kept
// record goes anyway, so nothing will try again. want is the token's value
// ("" when not known): only its last 4 characters are shown, the way New
// API's token page shows them, to tell it from a token this install made
// under the same name (newHere: it did, in the same gateway).
func notRevoked(app string, tok TokenRef, want, why string, newHere bool) string {
	which, pick := fmt.Sprintf("编号 %d", tok.ID), fmt.Sprintf("编号 %d", tok.ID)
	if v := strings.TrimPrefix(want, "sk-"); len(v) >= 8 {
		which += "，结尾 " + v[len(v)-4:]
		pick = "结尾是 " + v[len(v)-4:]
	}
	// The prefix makes the line a must-tell in the task's verdict.
	s := installtiming.NotRevokedPrefix + fmt.Sprintf("以前在 %s 里给 %s 的令牌（%s）：%s。这把令牌不限额、不过期：%s 运行时，请到它的“令牌”页删掉名字是 acornfox-%s、%s 的那一把",
		tok.Provider, app, which, why, tok.Provider, app, pick)
	if newHere {
		s += "（这次新发给它的那把同名，不要删）"
	}
	return s
}

// gatewayDown says why local gateway name cannot be reached now. gone: it
// was deleted with its data (no app, no volumes left), so its tokens went
// with it and there is nothing to revoke.
func (e *Engine) gatewayDown(ctx context.Context, name string) (why string, gone bool) {
	a, err := e.cfg.Store.GetApp(ctx, name)
	switch {
	case errors.Is(err, state.ErrNotFound):
		if vols, verr := e.cfg.Runner.ListVolumes(ctx, name); verr == nil && len(vols) == 0 {
			return "", true
		}
		return name + " 已经删了，它的数据还留着（以后接上旧数据重装它，这把令牌还在）", false
	case err == nil && a.Desired == state.DesiredStopped:
		return name + " 停着", false
	}
	return name + " 现在没在运行或连不上", false
}

// dropKept removes the kept record of app (once an install of that name has
// succeeded) and revokes the tokens in it that keep does not hold: only
// after checking that the gateway still has each with the value the old app
// was given, since a reinstalled gateway may use the number for another
// app's token. It returns what it says about those tokens: revoked, or not
// revoked (the gateway stopped or gone, its value unknown, the gateway said
// no) with how the user removes it by hand. The record goes in any case
// (the app has its own saved parameters now); a token not revoked is thus
// said once, here, and logged.
//
// keep: the old tokens this install took over; issued: the ones it issued
// (only to tell the user which of two tokens of the same name to remove).
func (e *Engine) dropKept(ctx context.Context, app string, keep, issued []TokenRef) []string {
	k, err := e.cfg.Store.GetKeptCatalogSecrets(ctx, app)
	if errors.Is(err, state.ErrNotFound) {
		return nil
	}
	var out []string
	if err == nil {
		if plain, err := e.box.open(k.ParamsEnc); err == nil {
			var old map[string]any
			if json.Unmarshal(plain, &old) == nil {
				out = e.revokeKeptTokens(ctx, app, keptValues(old), tokenRefs(old[savedTokensKey]), keep, issued)
			}
		}
	}
	if err := e.cfg.Store.DeleteKeptCatalogSecrets(ctx, app); err != nil {
		e.cfg.Log.Warn("drop kept catalog secrets", "app", app, "err", err)
	}
	return out
}

func (e *Engine) revokeKeptTokens(ctx context.Context, app string, kept Values, toks, keep, issued []TokenRef) []string {
	var out []string
	for _, tok := range toks {
		newHere := slices.ContainsFunc(issued, func(r TokenRef) bool { return r.Provider == tok.Provider })
		held := false
		for _, k := range keep {
			if k == tok {
				held = true
			}
		}
		if held {
			continue
		}
		want := ""
		for k, v := range kept {
			if c, ok := v.(*LLMChoice); ok && !internalKey(k) && c.Source == LLMLocalGateway && c.App == tok.Provider {
				want = c.APIKey
			}
		}
		if want == "" {
			// Without its value it cannot be told from another app's token
			// under the same number: not revoked blindly, but said.
			e.cfg.Log.Warn("app center: kept gateway token without its value, not revoked", "app", app, "provider", tok.Provider, "id", tok.ID)
			out = append(out, notRevoked(app, tok, "", "没有记下它的值，没法确认还是不是它", newHere))
			continue
		}
		g, err := e.gateway(ctx, tok.Provider)
		if err != nil {
			why, gone := e.gatewayDown(ctx, tok.Provider)
			if gone {
				e.cfg.Log.Info("app center: kept gateway token gone with its gateway", "app", app, "provider", tok.Provider, "id", tok.ID)
				continue
			}
			e.cfg.Log.Warn("app center: kept gateway token not checked", "app", app, "provider", tok.Provider, "id", tok.ID, "err", err)
			out = append(out, notRevoked(app, tok, want, why, newHere))
			continue
		}
		revoked, err := e.adapter.RevokeIfSame(ctx, g.base, g.user, g.pass, tok.ID, want)
		if err != nil {
			e.cfg.Log.Warn("app center: revoke kept gateway token", "app", app, "provider", tok.Provider, "id", tok.ID, "err", err)
			out = append(out, notRevoked(app, tok, want, tok.Provider+" 没有答应（"+scrubURL(err).Error()+"）", newHere))
			continue
		}
		if revoked {
			out = append(out, fmt.Sprintf("以前在 %s 里给它的令牌（编号 %d）不再用了，已吊销", tok.Provider, tok.ID))
		}
	}
	return out
}
