package cli

import (
	"context"
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/acornfox/acornfox/internal/catalog"
	"github.com/acornfox/acornfox/internal/client"
)

const catalogUsage = `catalog 子命令（给目录维护者用）：
  lint 目录      检查应用目录（apps/、recipes/）是否合格，并算出每个应用、配方最低要哪个平台版本。只读；有错误时退出码 1
  vars 目录 [--app 应用编号]
                 变量报告：每份清单用到哪些变量，各归哪一类（系统给、平台生成、有默认值、可选、要选模型来源、必须用户填）。只读
  gaps 目录      核对缺变量登记（目录根下的 gaps.yaml）：平台已经有了的变量，提示哪几条可以关了。只读
  build 目录 --out 输出目录 --serial N --key 私钥文件 [--catalog 目录编号] [--prev 上次的发布目录] [--apply]
                 校验后打包签名，生成 version.txt、index.json、index.json.sig、apps/<id>/<版本>.tar.gz。
                 默认试跑：只打包签名、检查序号，列出要写的文件，不写；加 --apply 才写输出目录
  keygen --out 私钥文件
                 生成 ed25519 签名钥匙，写私钥（0600），打印公钥。文件已存在就拒绝，不覆盖
  verify 输出目录 [--pubkey 公钥]...
                 只读校验发布目录：签名、序号、每个应用包的 sha256。不给 --pubkey 用程序内置的公钥
每个子命令加 -h 看详细说明；加 --json 输出机器可读结果。
例子：
  acornfox catalog lint .
  acornfox --json catalog lint ~/acornfox-catalog
  acornfox catalog vars . --app new-api
  acornfox catalog gaps .
  acornfox catalog keygen --out ~/.acornfox-catalog-key.pem
  acornfox catalog build . --out dist --serial 12 --key ~/.acornfox-catalog-key.pem --apply
  acornfox catalog verify dist --pubkey <keygen 打印的公钥>`

const catalogBuildUsage = `用法：acornfox catalog build 目录 --out 输出目录 --serial N --key 私钥文件 [--catalog 目录编号] [--prev 上次的发布目录] [--apply]

用途：先按 lint 校验目录，有错误就停下、什么都不写；通过后打包签名：
  apps/<id>/<版本>.tar.gz   每个应用目录里的文件（确定性打包，同样输入字节相同）
  index.json                所有应用和配方的摘要，以及每个包的路径、sha256、大小
  index.json.sig            对 index.json 的 ed25519 签名（base64url）
  version.txt               发布序号 N（十进制）
上传时按这个顺序，version.txt 最后。
和上次发布比较（输出目录里已有的文件，以及 --prev 指向的目录）：
  - 序号比上次小，或和上次相同但 index.json 不同，拒绝（CAT-BUILD-04）：客户端只认更大的序号；
  - 上次已发布的应用包路径换了内容，拒绝（CAT-BUILD-05）：改了应用目录就要升 release.version。
  所以要么每次都输出到上次的发布目录，要么每次输出到空目录并用 --prev 指向上次发布的那一份（例如从官网下载的
  version.txt 和 index.json）。两样都没有时，没法检查，请自己确认序号比线上大。
副作用：默认试跑，不写任何文件（检查照做，有问题照样报错）。加 --apply 才写输出目录：
  已存在且相同的文件不重写，重复运行安全。不会删除输出目录里的旧版本应用包。
参数：
  --out 目录     输出目录（必填）
  --serial N     发布序号，正整数，每次发布要比上次大（必填）
  --key 文件     keygen 生成的私钥，权限要是 0600（必填；Windows 上不能用，请在 WSL、Linux 或 Mac 上签名）
  --catalog 编号 把索引绑定到一个目录：公共目录写 public，私有目录写它的编号（cat-…）。编号写进 index.json、
                 一起签名；客户端只把写着 public 的当公共目录、写着 cat-x 的当私有目录 cat-x，
                 防止拿一个目录的签名索引冒充另一个。不写时索引里没有这一项（旧格式，两边都收）
  --prev 目录    上次发布的目录（至少有 version.txt、index.json），只读，用来检查序号和应用包
  --apply        真的写输出目录
例子：
  acornfox catalog build . --out dist --serial 12 --key ~/.acornfox-catalog-key.pem
  acornfox catalog build . --out dist --serial 12 --key ~/.acornfox-catalog-key.pem --apply
  acornfox --json catalog build . --out new --serial 13 --key key.pem --prev last-published --apply
  acornfox catalog build . --out private-out --serial 3 --key key.pem --catalog cat-demo --apply
出错编号：CAT-BUILD-01（目录有错误）、CAT-BUILD-02（输入不对）、CAT-BUILD-03（写不进去）、
  CAT-BUILD-04（序号没比上次大）、CAT-BUILD-05（已发布的包换了内容）、CAT-KEY-01/02（私钥读不出、权限过宽或在 Windows 上）`

const catalogKeygenUsage = `用法：acornfox catalog keygen --out 私钥文件

用途：生成一把 ed25519 目录签名钥匙。私钥写到 --out（PEM PKCS#8，权限 0600），公钥（base64url）打印出来，
  交给发布流程编进程序的受信公钥列表。
副作用：新建一个文件。文件已存在就拒绝（CAT-KEY-03），绝不覆盖。私钥不要提交到任何仓库。
  Windows 上查不了文件权限，拒绝运行（CAT-KEY-02），请在 WSL、Linux 或 Mac 上生成。
例子：
  acornfox catalog keygen --out ~/.acornfox-catalog-key.pem
  acornfox --json catalog keygen --out key.pem`

const catalogVerifyUsage = `用法：acornfox catalog verify 输出目录 [--pubkey 公钥]...

用途：只读校验一个发布目录：index.json 的签名、version.txt 和 index.json 的序号一致、每个应用包的 sha256 和大小。
  --pubkey 可以给多次（换钥匙期间新旧都认）；不给就用程序内置的受信公钥。
副作用：无，只读。
例子：
  acornfox catalog verify dist --pubkey <keygen 打印的公钥>
出错编号：CAT-VER-01～04、CAT-KEY-04（公钥格式不对）`

const catalogLintUsage = `用法：acornfox catalog lint 目录

用途：检查一个应用目录仓库（apps/<编号>/manifest.yaml、recipes/<编号>/recipe.yaml）是否合格：格式、互相引用、
  许可证、默认安全设置等。每个问题带编号（CAT-LINT-01～18、20、21；19 是 catalog gaps 的）。
  同时按清单实际用到的字段、取值和平台变量，算出每个应用、配方最低要哪个平台版本：
  - 清单的 lifecycle.min_platform 写得比算出来的低，报 CAT-LINT-17，并列出是哪几处写法抬高的；
  - 配方的 min_platform 可以不写（打包时自动算）；写了又写低了，同样报 CAT-LINT-17；
  - 标成全自动（delivery.tier: auto）的应用有必须用户填的参数，报 CAT-LINT-18。
  镜像包按架构写（release.images[].artifacts.amd64、.arm64，0.5.1 起）时还查：
  - 新旧两种写法同时写、写了 resources.platforms 里没有的架构、两种架构写了同一个镜像包，报 CAT-LINT-20；
  - platforms 里有、却没有镜像包的架构，提醒 CAT-LINT-21（那种机器从公共源拉；私有目录的应用在那种机器上装不了）。
  有错误（不含提醒）时打不了包。
副作用：无，只读。有错误时退出码 1。
输出（--json）：ok、apps、recipes、problems（code、level、file、path、message），
  以及 min_platform：{"apps":{编号:{declared 写的, computed 算出来的, reasons 抬高它的写法}}, "recipes":{…}}。
例子：
  acornfox catalog lint .
  acornfox --json catalog lint ~/acornfox-catalog`

const catalogVarsUsage = `用法：acornfox catalog vars 目录 [--app 应用编号]

用途：应用上架前检查用的变量报告，让看清单的人知道这个应用装的时候要不要用户动手。目录是应用目录仓库的根目录
  （含 apps/）。每份清单一张表：它引用的每个系统变量、声明的每个参数各一行，归六类之一：
    系统给 system            引用的是平台变量（platform.名字），平台按装在哪台机器自动填
    平台生成 generated       参数带 generate，由平台生成（密码、密钥）
    有默认值 default         参数带 default（空字符串不算：安装时空值当没填）
    可选 optional            不必填，也没有默认值
    要选模型来源 llm_source  llm_provider 类型的参数：用户选用哪里的大模型
    必须用户填 user_required 必填，又没有默认值、不能生成、不是模型来源
  另外给出：算出来的最低平台版本；环境变量里写死的、看起来像网址、时区、密钥的值（只是提醒，不算错）；
  报告指纹 fingerprint：对“变量名、层、类别”算的 sha256。变量的名字或类别变了指纹才变，
  只改介绍文字、图标不变。把它记下来，下次就能判断“变量有没有变、要不要重新看一遍”。
  标成全自动（delivery.tier: auto）的应用，必须用户填的数量要是 0，否则 catalog lint 报 CAT-LINT-18。
副作用：无，只读。清单读不了或不符合格式的应用没有报告，问题列在 problems 里，退出码 1；目录不存在也是退出码 1。
参数：
  --app 编号   只出这一个应用的报告（apps/ 下的目录名）；目录里没有这个应用时退出码 1
输出（--json）：{"ok":true,"apps":[{app, version, file, delivery_tier, min_platform, vars:[{name, layer, class, …}],
  counts, hardcoded, fingerprint}], "problems":[]}
例子：
  acornfox catalog vars .
  acornfox catalog vars . --app hermes-agent
  acornfox --json catalog vars ~/acornfox-catalog --app new-api`

const catalogGapsUsage = `用法：acornfox catalog gaps 目录

用途：核对缺变量登记。起草清单时发现平台还没有要用的变量（或清单格式缺一种写法），就在目录仓库根目录的
  gaps.yaml 登记一条，应用照常往下走（那一项先写死、让用户做一步，或先不配）。这个命令逐条核对：
  - 想要的是 platform.名字，而平台变量表里已经有了、这条还没标 done：提示“可以关了：变量已有，请改清单”；
  - 登记的应用已不在目录里：提示。
  gaps.yaml 的格式见目录仓库 schema/gaps.schema.json：每条有 id（GAP-0001 这样）、app、upstream_key（上游配置项名字）、
  want（想要的变量或写法）、layer（platform、param、format）、handling（hardcoded、user_step、omitted）、
  doc_url（上游文档地址）、date（登记日期）、status（open、planned、done；done 时要写 done_in 平台版本）、note（可选）。
副作用：无，只读。目录在、gaps.yaml 不存在算“没有登记”，退出码 0；格式不对报 CAT-LINT-19，退出码 1；
  目录本身不存在报 CAT-LINT-01，退出码 1。
  有提示（notices）不算错，退出码仍是 0。
输出（--json）：{"ok":true,"registered":true,"file":"gaps.yaml","gaps":[…],"by_status":{"open":N,"planned":N,"done":N},
  "notices":[{id, app, kind(can_close|app_missing), message}],"problems":[]}
例子：
  acornfox catalog gaps .
  acornfox --json catalog gaps ~/acornfox-catalog`

// cmdCatalog handles `acornfox catalog …`: list, info and sync ask the
// server (install.go); lint, vars, gaps, build, keygen and verify work on
// local files.
func (a *app) cmdCatalog(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.out.usageError("%s\n\n%s", catalogBrowseUsage, catalogUsage)
	}
	switch args[0] {
	case "lint":
		return a.cmdCatalogLint(args[1:])
	case "vars":
		return a.cmdCatalogVars(args[1:])
	case "gaps":
		return a.cmdCatalogGaps(args[1:])
	case "build":
		return a.cmdCatalogBuild(args[1:])
	case "keygen":
		return a.cmdCatalogKeygen(args[1:])
	case "verify":
		return a.cmdCatalogVerify(args[1:])
	case "list":
		return a.cmdCatalogList(ctx, args[1:])
	case "info":
		return a.cmdCatalogInfo(ctx, args[1:])
	case "sync":
		return a.cmdCatalogSync(ctx, args[1:])
	case "connect":
		return a.cmdCatalogConnect(ctx, args[1:])
	case "sources":
		return a.cmdCatalogSources(ctx, args[1:])
	case "platform-vars":
		return a.cmdCatalogPlatformVars(ctx, args[1:])
	case "-h", "--help", "help":
		fmt.Fprintln(a.out.stdout, catalogBrowseUsage)
		fmt.Fprintln(a.out.stdout)
		fmt.Fprintln(a.out.stdout, catalogUsage)
		return exitOK
	default:
		return a.out.usageError("未知的 catalog 子命令：%s\n%s", args[0], catalogUsage)
	}
}

// catalogFlags makes a FlagSet whose -h prints the Chinese help to stdout.
func (a *app) catalogFlags(name, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.out.stderr)
	fs.Usage = func() { fmt.Fprintln(a.out.stdout, usage) }
	return fs
}

// flagExit maps a parse error to an exit code (-h is not an error).
func flagExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	return exitUsage
}

// catalogFail reports a catalog error with its CAT-* code (exit 1).
func (a *app) catalogFail(err error) int {
	d := client.Diagnosis{Stage: "catalog", Code: catalog.CodeOf(err), Message: err.Error()}
	if d.Code == "" {
		d.Code = "unknown"
	}
	for _, f := range catalog.Faults {
		if f.Code == d.Code {
			d.Hint = f.Action
		}
	}
	var ce *catalog.Error
	if errors.As(err, &ce) && len(ce.Problems) > 0 {
		var lines []string
		for _, p := range ce.Problems {
			lines = append(lines, fmt.Sprintf("%s %s %s：%s", p.Code, p.File, p.Path, p.Message))
		}
		d.LogExcerpt = strings.Join(lines, "\n")
	}
	if a.out.json {
		a.out.emitDiagnosisJSON(d)
	} else {
		a.out.printDiagnosis(d)
	}
	return exitOpFailed
}

func (a *app) cmdCatalogLint(args []string) int {
	fs := a.catalogFlags("catalog lint", catalogLintUsage)
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(rest) != 1 {
		return a.out.usageError("用法：catalog lint 目录")
	}
	rep, err := catalog.Lint(rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	errs := rep.Errors()
	if a.out.json {
		a.out.encodeRedacted(a.out.stdout, map[string]any{
			"ok": errs == 0, "apps": rep.Apps, "recipes": rep.Recipes, "problems": rep.Problems,
			"min_platform": rep.MinPlatform,
		})
	} else {
		a.printProblems(rep.Problems)
		fmt.Fprintf(a.out.stdout, "检查了 %d 个应用、%d 个配方：%d 个错误，%d 个提醒\n",
			len(rep.Apps), len(rep.Recipes), errs, len(rep.Problems)-errs)
	}
	if errs > 0 {
		return exitOpFailed
	}
	return exitOK
}

// printProblems lists lint problems for people.
func (a *app) printProblems(problems []catalog.Problem) {
	for _, p := range problems {
		mark := "✗"
		if p.Level == catalog.LevelWarning {
			mark = "!"
		}
		loc := p.File
		if p.Path != "" {
			loc += " " + p.Path
		}
		fmt.Fprintf(a.out.stdout, "%s %s %s：%s\n", mark, p.Code, loc, p.Message)
	}
}

func (a *app) cmdCatalogVars(args []string) int {
	fs := a.catalogFlags("catalog vars", catalogVarsUsage)
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(rest) != 1 {
		return a.out.usageError("用法：catalog vars 目录 [--app 应用编号]")
	}
	// --app is the CLI's global flag (already taken out of args): here it
	// names the catalog app to report on.
	res, err := catalog.CatalogVars(rest[0], a.flagApp)
	if err != nil {
		return a.out.fail(err)
	}
	ok := len(res.Problems) == 0
	if a.out.json {
		a.out.encodeRedacted(a.out.stdout, map[string]any{"ok": ok, "apps": res.Apps, "problems": res.Problems})
	} else {
		a.printProblems(res.Problems)
		for _, r := range res.Apps {
			a.printVarReport(r)
		}
		if len(res.Apps) == 0 && ok {
			fmt.Fprintln(a.out.stdout, "这个目录里没有应用（apps/ 下没有清单）")
		}
	}
	if !ok {
		return exitOpFailed
	}
	return exitOK
}

func (a *app) printVarReport(r catalog.VarReport) {
	w := a.out.stdout
	tier := map[string]string{"auto": "全自动", "steps": "要用户做几步"}[r.DeliveryTier]
	if tier == "" {
		tier = "没写"
	}
	fmt.Fprintf(w, "%s %s（%s）\n", r.App, r.Version, r.File)
	fmt.Fprintf(w, "  交付方式：%s；最低平台版本：算出来 %s，清单写的 %s\n", tier, r.MinPlatform.Computed, r.MinPlatform.Declared)
	for _, q := range r.MinPlatform.Reasons {
		fmt.Fprintf(w, "    抬高版本的写法：%s（%s，%s 起）\n", q.Where, q.Feature, q.Since)
	}
	var counts []string
	for _, c := range catalog.VarClasses {
		counts = append(counts, fmt.Sprintf("%s %d", catalog.VarClassLabels[c], r.Counts[c]))
	}
	fmt.Fprintf(w, "  变量 %d 个：%s\n", len(r.Vars), strings.Join(counts, "，"))
	for _, v := range r.Vars {
		line := fmt.Sprintf("  [%s] %s", catalog.VarClassLabels[v.Class], v.Name)
		if v.Label != "" {
			line += "（" + v.Label + "）"
		}
		if v.Known != nil && !*v.Known {
			line += "（平台变量表里没有这个名字）"
		}
		if len(v.UsedIn) > 0 {
			line += "，用在 " + strings.Join(v.UsedIn, "、")
		} else {
			line += "，清单里没有地方引用它"
		}
		fmt.Fprintln(w, line)
	}
	if r.DeliveryTier == "auto" && r.Counts[catalog.ClassUserRequired] > 0 {
		fmt.Fprintf(w, "  ✗ 标成全自动，但有 %d 个变量必须用户填（catalog lint 会报 %s）\n", r.Counts[catalog.ClassUserRequired], catalog.CodeAutoTier)
	}
	for _, h := range r.Hardcoded {
		fmt.Fprintf(w, "  ! 提醒 %s = %s：%s\n", h.Where, h.Value, h.Message)
	}
	fmt.Fprintf(w, "  指纹：%s\n", r.Fingerprint)
}

func (a *app) cmdCatalogGaps(args []string) int {
	fs := a.catalogFlags("catalog gaps", catalogGapsUsage)
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(rest) != 1 {
		return a.out.usageError("用法：catalog gaps 目录")
	}
	rep, err := catalog.CheckGaps(rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	ok := len(rep.Problems) == 0
	if a.out.json {
		a.out.encodeRedacted(a.out.stdout, map[string]any{"ok": ok, "registered": rep.Registered, "file": rep.File,
			"gaps": rep.Gaps, "by_status": rep.ByStatus, "notices": rep.Notices, "problems": rep.Problems})
	} else {
		w := a.out.stdout
		switch {
		case !ok && !rep.Registered: // the catalog directory itself is not there
			a.printProblems(rep.Problems)
		case !ok:
			a.printProblems(rep.Problems)
			fmt.Fprintf(w, "%s 格式不对，%d 个错误，没有核对\n", rep.File, len(rep.Problems))
		case !rep.Registered:
			fmt.Fprintf(w, "没有登记：目录里没有 %s\n", rep.File)
		default:
			for _, n := range rep.Notices {
				fmt.Fprintf(w, "! %s %s：%s\n", n.ID, n.App, n.Message)
			}
			fmt.Fprintf(w, "登记了 %d 条：待定 %d，已排期 %d，已有 %d；%d 条提示\n", len(rep.Gaps),
				rep.ByStatus["open"], rep.ByStatus["planned"], rep.ByStatus["done"], len(rep.Notices))
		}
	}
	if !ok {
		return exitOpFailed
	}
	return exitOK
}

func (a *app) cmdCatalogBuild(args []string) int {
	fs := a.catalogFlags("catalog build", catalogBuildUsage)
	out := fs.String("out", "", "输出目录")
	serial := fs.Int64("serial", 0, "发布序号")
	keyFile := fs.String("key", "", "私钥文件")
	prev := fs.String("prev", "", "上次的发布目录")
	catalogID := fs.String("catalog", "", "目录编号：public 或 cat-…")
	apply := fs.Bool("apply", false, "真的写输出目录")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(rest) != 1 || *out == "" || *keyFile == "" || *serial == 0 {
		return a.out.usageError("用法：catalog build 目录 --out 输出目录 --serial N --key 私钥文件 [--catalog 目录编号] [--prev 上次的发布目录] [--apply]")
	}
	dryRun := !*apply
	key, err := catalog.LoadSigningKey(*keyFile)
	if err != nil {
		return a.catalogFail(err)
	}
	b, err := catalog.BuildFor(rest[0], *serial, *catalogID)
	if err != nil {
		return a.catalogFail(err)
	}
	b.Sign(key)
	if *prev != "" {
		if err := b.CheckPrev(*prev); err != nil {
			return a.catalogFail(err)
		}
	}
	files := b.Files()
	if dryRun {
		if err := b.CheckPrev(*out); err != nil {
			return a.catalogFail(err)
		}
	} else if files, err = b.Write(*out); err != nil {
		return a.catalogFail(err)
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"dry_run": dryRun, "out": *out, "serial": *serial, "catalog": *catalogID, "prev": *prev,
			"public_key": catalog.EncodePublicKey(key.Public().(ed25519.PublicKey)), "files": files})
		return exitOK
	}
	verb := "写了"
	if dryRun {
		verb = "试跑（加 --apply 才写），会写"
	}
	fmt.Fprintf(a.out.stdout, "%s %d 个文件到 %s（序号 %d）：\n", verb, len(files), *out, *serial)
	for _, f := range files {
		fmt.Fprintf(a.out.stdout, "  %s  %s  %d 字节\n", f.SHA256, f.Path, f.Size)
	}
	fmt.Fprintf(a.out.stdout, "签名公钥：%s\n", catalog.EncodePublicKey(key.Public().(ed25519.PublicKey)))
	return exitOK
}

func (a *app) cmdCatalogKeygen(args []string) int {
	fs := a.catalogFlags("catalog keygen", catalogKeygenUsage)
	out := fs.String("out", "", "私钥文件")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(rest) != 0 || *out == "" {
		return a.out.usageError("用法：catalog keygen --out 私钥文件")
	}
	pub, err := catalog.GenerateKeyFile(*out)
	if err != nil {
		return a.catalogFail(err)
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"key_file": *out, "public_key": catalog.EncodePublicKey(pub)})
		return exitOK
	}
	fmt.Fprintf(a.out.stdout, "私钥已写到 %s（权限 0600，不要提交到任何仓库）\n公钥：%s\n", *out, catalog.EncodePublicKey(pub))
	return exitOK
}

// pubkeyList collects repeated --pubkey flags.
type pubkeyList []string

func (p *pubkeyList) String() string     { return strings.Join(*p, ",") }
func (p *pubkeyList) Set(v string) error { *p = append(*p, v); return nil }

func (a *app) cmdCatalogVerify(args []string) int {
	fs := a.catalogFlags("catalog verify", catalogVerifyUsage)
	var pubs pubkeyList
	fs.Var(&pubs, "pubkey", "受信公钥（可多次）")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return flagExit(err)
	}
	if len(rest) != 1 {
		return a.out.usageError("用法：catalog verify 输出目录 [--pubkey 公钥]...")
	}
	keys := catalog.TrustedKeys
	if len(pubs) > 0 {
		keys = nil
		for _, s := range pubs {
			k, err := catalog.ParsePublicKey(s)
			if err != nil {
				return a.catalogFail(err)
			}
			keys = append(keys, k)
		}
	}
	res, err := catalog.VerifyDir(rest[0], keys)
	if err != nil {
		return a.catalogFail(err)
	}
	if a.out.json {
		a.out.emitJSON(map[string]any{"serial": res.Serial, "apps": res.Apps, "recipes": res.Recipes, "packages": res.Packages})
		return exitOK
	}
	fmt.Fprintf(a.out.stdout, "校验通过：序号 %d，%d 个应用、%d 个配方，%d 个应用包都对得上\n", res.Serial, res.Apps, res.Recipes, res.Packages)
	return exitOK
}
