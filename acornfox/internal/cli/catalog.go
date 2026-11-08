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
  lint 目录      检查应用目录（apps/、recipes/）是否合格。只读；有错误时退出码 1
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

// cmdCatalog handles `acornfox catalog …`: list, info and sync ask the
// server (install.go); lint, build, keygen and verify work on local files.
func (a *app) cmdCatalog(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return a.out.usageError("%s\n\n%s", catalogBrowseUsage, catalogUsage)
	}
	switch args[0] {
	case "lint":
		return a.cmdCatalogLint(args[1:])
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
	fs := a.catalogFlags("catalog lint", catalogUsage)
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
		})
	} else {
		for _, p := range rep.Problems {
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
		fmt.Fprintf(a.out.stdout, "检查了 %d 个应用、%d 个配方：%d 个错误，%d 个提醒\n",
			len(rep.Apps), len(rep.Recipes), errs, len(rep.Problems)-errs)
	}
	if errs > 0 {
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
