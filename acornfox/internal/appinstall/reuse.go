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
//   - The services its needs were plugged into: the bindings stayed with its
//     data (connect.go); a need is matched as usual, the service it used
//     before winning among several, and an old credential is used again
//     when the provider's check steps still pass it.
//   - Config files (overwrite: install) that exist in the old data: kept as
//     they are when what the platform writes into them is the same as for the
//     old app (the user's and the app's own edits stay). When it changed (a
//     new credential or address, another service, a changed template), the
//     file is written anew and the old one is first copied aside
//     (<path>.bak-<UTC time>), so nothing is lost; a file that cannot be
//     copied is kept. Without a kept record nothing can be compared and an
//     existing file is kept. A file an earlier install on the same data
//     rewrote before it failed is written anew too (rewrittenSince): it is
//     that attempt's, not the old app's.
//
// Each of these says what it did in the progress (the create and files
// steps) and in the task's apps[]: carried, kept_files, file_backups.
//
// Once the install succeeds the kept record goes (dropKept). A successful
// install that does not reuse data drops a stale kept record of its name
// the same way (its data is gone, or the install would have been refused
// with CAT-INST-24).

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/state"
)

// restoreKept puts back, for a reuse_data install, what was kept when the
// old app of this name was deleted with its data: every parameter the user
// did not give now gets its old value, and the platform variables the user
// had changed come back unless sets changes them again. Old data of another catalog app
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
	p.carried = append(p.carried, e.restorePlatformSets(old, vals, sets)...)
	p.carried = append(p.carried, legacyNotes(p.name, old, true)...)

	p.kept = Values(old)
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
func (e *Engine) restorePlatformSets(old map[string]any, vals Values, sets map[string]string) []string {
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
		if e.checkPlatformSets(map[string]string{n: v}) != nil {
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

// fileChange compares what the platform writes into config file f this time
// (content) with what it wrote for the old app: the old manifest's template
// for the same file (this one's when the old manifest is not saved), filled
// with the kept values. changed lists the references whose value differs (or
// "模板" when only the template text did). ok is false when it cannot be
// told: no kept record, the old manifest has no such file (an existing file
// there is not the platform's), or the old template does not render.
//
// The items of the services (uses.<need>.<item>) are never kept with the
// parameters. A need plugged in again into the same service with the same
// credential (p.reusedUses) gave the old app what it gives now; one plugged
// into the same service with a new credential gave it the old credential
// (p.oldUses); any other need's items may differ from what the old file
// holds in ways nothing can tell (another service), so a reference to them
// counts as changed.
func (p *planned) fileChange(f catalog.File, content string) (changed []string, ok bool) {
	if p.kept == nil {
		return nil, false
	}
	old := Values{}
	for k, v := range p.kept {
		old[k] = v
	}
	cur, _ := p.vals[usesKey].(map[string]map[string]string)
	oldUses := map[string]map[string]string{}
	unknown := map[string]bool{}
	for need, items := range cur {
		switch {
		case p.reusedUses[need]:
			oldUses[need] = items
		case p.oldUses[need] != nil:
			oldUses[need] = p.oldUses[need]
		default:
			// Rendered with this install's items, so that the rest of the
			// file can be compared; its references count as changed.
			oldUses[need], unknown[need] = items, true
		}
	}
	old[usesKey] = oldUses
	var renewed []string
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
	seen := map[string]bool{}
	for _, r := range append(catalog.TemplateRefs(tmpl), catalog.TemplateRefs(f.Template)...) {
		if name, isUse := strings.CutPrefix(r, "uses."); isUse && !seen[r] {
			if need, _, _ := strings.Cut(name, "."); unknown[need] {
				renewed = append(renewed, r)
				seen[r] = true
			}
		}
	}
	oldContent, err := old.render(tmpl, fileEscape(f.Path))
	if err != nil {
		return nil, false
	}
	if oldContent == content && len(renewed) == 0 {
		return nil, true
	}
	changed = append(changed, renewed...)
	for _, r := range append(catalog.TemplateRefs(tmpl), catalog.TemplateRefs(f.Template)...) {
		if seen[r] {
			continue
		}
		seen[r] = true
		ov, oerr := old.lookup(r)
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

// dropKept removes the kept record of app, once an install of that name has
// succeeded (the app has its own saved parameters now). What the old app's
// needs were plugged into is not in it: those bindings are the registry's
// (connect.go).
func (e *Engine) dropKept(ctx context.Context, app string) {
	if err := e.cfg.Store.DeleteKeptCatalogSecrets(ctx, app); err != nil && !errors.Is(err, state.ErrNotFound) {
		e.cfg.Log.Warn("drop kept catalog secrets", "app", app, "err", err)
	}
}

// reuseTarget fills in req.App (and req.Catalog, when not given) for a
// reuse_data request that names only the app to bring back (req.Name): the
// catalog app it was and the catalog it came from are in what was kept when
// it was deleted with its data. That works the same for an app installed
// with a manifest of a former format: the catalog app id is kept for every
// app. Which version is installed is decided as for any install (the one in
// the catalog now, or req.Version). Without a kept record (deleted by a
// version that kept none, or never from the app center) the app cannot be
// told: CAT-INST-02 says so and how to name it.
func (e *Engine) reuseTarget(ctx context.Context, req *Request) error {
	name := req.Name
	if !state.ValidAppName(name) {
		return fault(CodeBadParams, nil, "应用名 %s 不合法：小写字母开头，只用小写字母、数字和 -，2～40 个字符", name)
	}
	k, err := e.cfg.Store.GetKeptCatalogSecrets(ctx, name)
	if err != nil && !errors.Is(err, state.ErrNotFound) {
		return fault(CodeInternal, err, "读保留的记录失败")
	}
	if err != nil || k.CatalogID == "" {
		return fault(CodeBadParams, nil, "这台机器上没有 %s 删除时保留下来的记录，分不出它原来是应用中心里的哪个应用：在 install 后面写上它在应用中心里的名字（acornfox catalog list 看），例如 acornfox install 应用 --name %s --reuse-data", name, name)
	}
	req.App = k.CatalogID
	if strings.TrimSpace(req.Catalog) == "" {
		req.Catalog = PublicSource
		if k.CatalogOrigin != "" {
			req.Catalog = k.CatalogOrigin
		}
	}
	return nil
}

// ReuseCommand is the command that brings app back on the data it left
// behind (deleted with its data kept): the platform finds which catalog app
// it was from what was kept.
func ReuseCommand(app string) string {
	return "acornfox install --name " + app + " --reuse-data"
}
