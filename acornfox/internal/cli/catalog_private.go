package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/acornfox/acornfox/internal/client"
)

// Private catalogs: `acornfox catalog connect <code>` and
// `acornfox catalog sources`. The server does the work
// (internal/appinstall/private.go); codes CAT-CONN-* are defined in
// internal/appinstall/faults.go and come back with their hint.

func (a *app) privateCatalogAPI(ctx context.Context) (client.API, client.PrivateCatalogAPI, int) {
	api, _, code := a.appCenterAPI(ctx)
	if api == nil {
		return nil, nil, code
	}
	pc, ok := api.(client.PrivateCatalogAPI)
	if !ok {
		api.Close()
		return nil, nil, a.out.fail(errOldServer())
	}
	return api, pc, -1
}

func (a *app) cmdCatalogConnect(ctx context.Context, args []string) int {
	fs := a.catalogFlags("catalog connect", catalogConnectUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 1 || strings.TrimSpace(pos[0]) == "" {
		return a.out.usageError("用法：acornfox catalog connect 连接码（-h 看详情）")
	}
	code := strings.TrimSpace(pos[0])
	a.out.addSecret(code) // never echo the code back, even inside an error
	api, pc, ec := a.privateCatalogAPI(ctx)
	if api == nil {
		return ec
	}
	defer api.Close()
	v, err := pc.CatalogConnect(ctx, code)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(v.Raw, map[string]any{"ok": true})
		return exitOK
	}
	verb := "已连接"
	if v.Again {
		verb = "已重新连接"
	}
	a.out.human("%s私有目录：%s（%s）", verb, v.Catalog.Name, v.Catalog.ID)
	if v.Sync != nil {
		if v.Sync.OK {
			a.out.human("已同步：%s", v.Sync.Message)
		} else {
			a.out.human("连上了，但这次同步没成功（%s）：%s；稍后 acornfox catalog sync 再试", v.Sync.Code, v.Sync.Message)
		}
	}
	a.out.human("看里面的应用：acornfox catalog list；从它装：acornfox install 应用 --catalog %s", v.Catalog.ID)
	return exitOK
}

func (a *app) cmdCatalogSources(ctx context.Context, args []string) int {
	fs := a.catalogFlags("catalog sources", catalogSourcesUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 0 {
		return a.out.usageError("用法：acornfox catalog sources")
	}
	api, pc, ec := a.privateCatalogAPI(ctx)
	if api == nil {
		return ec
	}
	defer api.Close()
	v, err := pc.CatalogSources(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(v.Raw, map[string]any{"ok": true})
		return exitOK
	}
	for _, s := range v.Sources {
		state := "可用"
		switch {
		case s.Disconnected:
			state = "已断开（" + whenText(s.DisconnectedAt) + "）"
		case s.Serial == 0:
			state = "还没有可用的索引"
		}
		kind := "私有"
		if s.Kind == "public" {
			kind = "公共"
		}
		a.out.human("%s  %s目录 %s：%s，序号 %d，%d 个应用、%d 个配方", s.ID, kind, s.Name, state, s.Serial, s.Apps, s.Recipes)
		line := "    上次同步 " + whenText(s.LastSync)
		if s.Code != "" {
			msg := strings.TrimSpace(strings.TrimPrefix(s.Message, s.Code))
			line += fmt.Sprintf("（%s %s）", s.Code, msg)
		}
		a.out.human("%s", line)
	}
	return exitOK
}

func whenText(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "从没有"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}

func (a *app) cmdCatalogPlatformVars(ctx context.Context, args []string) int {
	fs := a.catalogFlags("catalog platform-vars", catalogPlatformVarsUsage)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 0 {
		return a.out.usageError("用法：acornfox catalog platform-vars")
	}
	api, _, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	pv, ok := api.(client.PlatformVarsAPI)
	if !ok {
		return a.out.fail(errOldServer())
	}
	v, err := pv.PlatformVars(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	if a.out.json {
		a.emitRaw(v.Raw, map[string]any{"ok": true})
		return exitOK
	}
	for _, x := range v.Vars {
		val := x.Value
		switch {
		case x.Secret && val != "":
			val = "******"
		case !x.Available:
			val = "（暂无：" + x.Note + "）"
		case val == "" && x.Note != "":
			val = "（" + x.Note + "）"
		}
		can := "不能改"
		if x.UserCanSet {
			can = "能改：--set platform." + x.Name + "=…"
		}
		a.out.human("platform.%s  %s：%s  [%s]", x.Name, x.Label, val, can)
		a.out.human("    单机版：%s；云版：%s", x.Standalone, x.Cloud)
		if x.Since != "" {
			a.out.human("    AcornFox %s 起有", x.Since)
		}
	}
	return exitOK
}
