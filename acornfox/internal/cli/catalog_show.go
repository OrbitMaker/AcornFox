package cli

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/client"
)

const catalogShowUsage = `用法：acornfox catalog show 应用或解决方案编号 [--recipe]

用途：看应用中心页面上这个应用（或解决方案）的“展示内容”：名字、一句话、角标、封面、行业、搜索词，和那一篇介绍。
  展示内容由经营系统编辑，随签名的目录一起发到这台服务器，页面上的名字、一句话和介绍就是它；
  用户问“这个应用能做什么”“装好以后能干嘛”时，读这里的介绍再回答，不要凭记忆说。
  没有展示内容（线上目录还没发，或这台服务器没取到、验签没过）时，只给清单里的名字和一句话，并写明原因的故障编号
  （CAT-SHOW-01～04，acornfox faults 编号 看怎么办）；装应用不受影响。
参数：
  应用或解决方案编号   先按应用找，找不到再按解决方案（配方）找
  --recipe             只按解决方案（配方）找（应用和解决方案编号相同时用）
副作用：无，只读（读的是这台服务器已经同步下来的目录，不联网）。
输出（--json）：{"ok":true,"kind":"app|recipe","id":…,"name":清单里的名字,"summary":清单里的一句话,
  "show":{name, tagline, flag, color, cover, doc, order, keywords（解决方案是 industries）, skill}或 null
  （skill 有值表示这个应用有操作手册可用：{version, checked, size}，全文用 acornfox catalog skill 取）,
  "industries":[{id,name}]（解决方案才有：它所在的行业，带名字）,
  "show_state":{"state":"ok|none|failed","code":…,"message":…,"serial":…}}。
  角标 flag：hot 热门、new 新上架、pick 推荐，空是没有。
退出码：0 找到；2 参数不对；3 连不上服务器；4 目录里没有这个编号。
例子：
  acornfox catalog show demo-app
  acornfox --json catalog show demo-app
  acornfox catalog show demo-recipe --recipe`

var flagNames = map[string]string{"hot": "热门", "new": "新上架", "pick": "推荐"}

func (a *app) cmdCatalogShow(ctx context.Context, args []string) int {
	fs := a.catalogFlags("catalog show", catalogShowUsage)
	onlyRecipe := fs.Bool("recipe", false, "只按解决方案（配方）找")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) != 1 {
		return a.out.usageError("用法：acornfox catalog show 应用或解决方案编号")
	}
	api, ac, code := a.appCenterAPI(ctx)
	if api == nil {
		return code
	}
	defer api.Close()
	l, err := ac.Catalog(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	id := pos[0]
	kind, name, summary := "", "", ""
	var item *client.ShowItem
	if !*onlyRecipe {
		for _, x := range l.Apps {
			// The public catalog's app wins over a private one of the same id.
			if x.ID == id && (kind == "" || x.Source.ID == "public") {
				kind, name, summary, item = "app", x.Name, x.Summary, x.Show
			}
		}
	}
	if kind == "" {
		for _, r := range l.Recipes {
			if r.ID == id {
				kind, name, summary, item = "recipe", r.Title, r.Summary, r.Show
				break
			}
		}
	}
	if kind == "" {
		return a.failDiag(client.Diagnosis{
			Stage: "catalog", Code: "not_found",
			Message: fmt.Sprintf("目录里没有应用或解决方案 %s", id),
			Hint:    "acornfox catalog list 看有哪些；目录太旧就 acornfox catalog sync",
		}, exitServerError)
	}
	state := client.ShowState{State: "none"}
	if l.Sync.Show != nil {
		state = *l.Sync.Show
	}
	industries := []client.ShowIndustry{}
	if kind == "recipe" && item != nil {
		for _, in := range item.Industries {
			ind := client.ShowIndustry{ID: in, Name: in}
			for _, known := range l.Industries {
				if known.ID == in {
					ind = known
				}
			}
			industries = append(industries, ind)
		}
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"kind": kind, "id": id, "name": name, "summary": summary, "show": item,
			"industries": industries, "show_state": state})
		return exitOK
	}
	if item == nil {
		a.out.human("%s（%s）：%s", name, id, summary)
		switch state.State {
		case "failed":
			a.out.human("%s", showStateText(state))
		case "ok":
			a.out.human("线上的展示内容（序号 %d）里没有写这一个：页面用的是上面清单里的名字和一句话。", state.Serial)
		default:
			a.out.human("没有展示内容：线上的目录还没有发展示内容，或这台服务器的目录是程序内置的；页面用的是上面清单里的名字和一句话。")
		}
		return exitOK
	}
	title := item.Name
	if title == "" {
		title = name
	}
	tagline := item.Tagline
	if tagline == "" {
		tagline = summary
	}
	a.out.human("%s（%s）：%s", title, id, tagline)
	var facts []string
	if f := flagNames[item.Flag]; f != "" {
		facts = append(facts, "角标 "+f)
	}
	if item.Color != "" {
		facts = append(facts, "主色 "+item.Color)
	}
	if item.Cover != "" {
		facts = append(facts, "封面 "+item.Cover)
	}
	if len(industries) > 0 {
		var ns []string
		for _, in := range industries {
			ns = append(ns, in.Name)
		}
		facts = append(facts, "行业 "+strings.Join(ns, "、"))
	}
	if len(item.Keywords) > 0 {
		facts = append(facts, "搜索词 "+strings.Join(item.Keywords, "、"))
	}
	if item.Order > 0 {
		facts = append(facts, fmt.Sprintf("排序 %d", item.Order))
	}
	if len(facts) > 0 {
		a.out.human("%s", strings.Join(facts, "；"))
	}
	if item.Skill != nil {
		cmd := "acornfox catalog skill " + id
		if kind == "recipe" {
			cmd += " --recipe"
		}
		a.out.human("有操作手册（对版本 %s 验过，%s）：%s", item.Skill.Version, item.Skill.Checked, cmd)
	}
	if strings.TrimSpace(item.Doc) == "" {
		a.out.human("还没有写介绍。")
	} else {
		a.out.human("")
		a.out.human("%s", strings.TrimSpace(item.Doc))
	}
	if state.State == "failed" {
		a.out.human("")
		a.out.human("%s", showStateText(state))
	}
	return exitOK
}

// showStateText says why the display content could not be used (state failed).
func showStateText(s client.ShowState) string {
	msg := strings.TrimSpace(s.Message)
	if s.Code != "" && !strings.HasPrefix(msg, s.Code) {
		msg = s.Code + " " + msg
	}
	return "上次同步到的展示内容没法用（" + msg + "），页面退回用清单里的名字和一句话；acornfox faults " + s.Code + " 看怎么办。"
}

const catalogShowSignUsage = `用法：acornfox catalog show-sign show.json --key 私钥文件 [--index 目录或index.json] [--apply]

用途：给应用中心的展示内容 show.json 签名，写出 show.json.sig（和 show.json 放在同一个目录，发布时一起上传）。
  签的是 "acornfox-catalog-show-v1\n" 加 show.json 的全部字节，用的钥匙和签 index.json 的是同一把（keygen 生成的）。
  签之前先检查：show.json 要是 schema 1 的 catalog-show、编号不重复、每篇介绍不超过 64 KB（CAT-SHOW-03）；
  show.json 里的 serial 要等于同一次发布的 index.json 的 serial（CAT-SHOW-02）——index.json 默认取 show.json 同目录的，
  用 --index 指到别处，同目录没有 index.json 时不核对并提醒。
  程序读到会丢掉的值（不在 https://acornfox.com/catalog/assets/ 下面的封面、不是 hot/new/pick 的角标、不是 #rrggbb 的主色、
  行业列表里没有的行业）会作为提醒列出来，不挡着签名。
副作用：默认试跑，只检查、列出会写的文件，不写。加 --apply 才写 show.json.sig（已存在且相同不重写，重复运行安全）。
  不会改 show.json。Windows 上不能用（私钥权限查不了），请在 WSL、Linux 或 Mac 上签名。
参数：
  --key 文件     keygen 生成的私钥，权限要是 0600（必填）
  --index 路径   同一次发布的 index.json，或放它的目录（可选）
  --apply        真的写 show.json.sig
输出（--json）：{"ok":true,"dry_run":…,"wrote":…,"file":"…/show.json.sig","serial":…,"apps":N,"solutions":N,"warnings":[…],
  "public_key":…}。
例子：
  acornfox catalog show-sign dist/show.json --key ~/.acornfox-catalog-key.pem
  acornfox catalog show-sign dist/show.json --key ~/.acornfox-catalog-key.pem --apply
  acornfox catalog verify dist --pubkey <keygen 打印的公钥>      （签完再核对整个发布目录，含展示内容）
出错编号：CAT-SHOW-02（序号对不上）、CAT-SHOW-03（格式不对）、CAT-KEY-01/02（私钥读不出、权限过宽或在 Windows 上）`

func (a *app) cmdCatalogShowSign(args []string) int {
	fs := a.catalogFlags("catalog show-sign", catalogShowSignUsage)
	keyFile := fs.String("key", "", "私钥文件")
	index := fs.String("index", "", "同一次发布的 index.json 或它所在的目录")
	apply := fs.Bool("apply", false, "真的写 show.json.sig")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(rest) != 1 || *keyFile == "" {
		return a.out.usageError("用法：catalog show-sign show.json --key 私钥文件 [--index 目录或index.json] [--apply]")
	}
	key, err := catalog.LoadSigningKey(*keyFile)
	if err != nil {
		return a.catalogFail(err)
	}
	raw, err := os.ReadFile(rest[0])
	if err != nil {
		return a.catalogFail(&catalog.Error{Code: catalog.CodeShowFormat, Message: "读不了 " + rest[0], Err: err})
	}
	show, warnings, err := catalog.ShowNotes(raw)
	if err != nil {
		return a.catalogFail(err)
	}
	if warnings == nil {
		warnings = []string{}
	}
	idxPath := *index
	if idxPath == "" {
		idxPath = filepath.Dir(rest[0])
	}
	if st, serr := os.Stat(idxPath); serr == nil && st.IsDir() {
		idxPath = filepath.Join(idxPath, catalog.IndexFile)
	}
	idxB, ierr := os.ReadFile(idxPath)
	switch {
	case ierr == nil:
		idx, perr := catalog.ParseIndex(idxB)
		if perr != nil {
			return a.catalogFail(&catalog.Error{Code: catalog.CodeShowSerial, Message: idxPath + " 不是目录索引，没法核对序号", Err: perr})
		}
		if idx.Serial != show.Serial {
			return a.catalogFail(&catalog.Error{Code: catalog.CodeShowSerial, Message: fmt.Sprintf("show.json 的 serial 是 %d，%s 的是 %d", show.Serial, idxPath, idx.Serial)})
		}
	case *index != "":
		return a.catalogFail(&catalog.Error{Code: catalog.CodeShowSerial, Message: "读不了 " + idxPath + "，没法核对序号", Err: ierr})
	default:
		warnings = append(warnings, "同目录没有 index.json，没核对 show.json 的序号和目录的序号一样")
	}
	sigPath := rest[0] + ".sig"
	sig := append(catalog.SignShow(raw, key), '\n')
	pub := catalog.EncodePublicKey(key.Public().(ed25519.PublicKey))
	wrote := false
	if *apply {
		if old, rerr := os.ReadFile(sigPath); rerr != nil || !bytes.Equal(old, sig) {
			if werr := os.WriteFile(sigPath, sig, 0o644); werr != nil {
				return a.catalogFail(&catalog.Error{Code: catalog.CodeShowFormat, Message: "写不了 " + sigPath, Err: werr})
			}
			wrote = true
		}
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"dry_run": !*apply, "wrote": wrote, "file": sigPath, "serial": show.Serial,
			"apps": len(show.Apps), "solutions": len(show.Solutions), "warnings": warnings, "public_key": pub})
		return exitOK
	}
	for _, w := range warnings {
		fmt.Fprintf(a.out.stdout, "! %s\n", w)
	}
	switch {
	case !*apply:
		fmt.Fprintf(a.out.stdout, "试跑（加 --apply 才写）：检查通过，会写 %s（序号 %d，%d 个应用、%d 个解决方案）\n", sigPath, show.Serial, len(show.Apps), len(show.Solutions))
	case wrote:
		fmt.Fprintf(a.out.stdout, "写了 %s（序号 %d，%d 个应用、%d 个解决方案）\n", sigPath, show.Serial, len(show.Apps), len(show.Solutions))
	default:
		fmt.Fprintf(a.out.stdout, "%s 已经是这份 show.json 的签名，没有重写\n", sigPath)
	}
	fmt.Fprintf(a.out.stdout, "签名公钥：%s\n", pub)
	return exitOK
}
