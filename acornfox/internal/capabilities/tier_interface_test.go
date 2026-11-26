package capabilities

// 核心和外围之间的接口核对（测试提速第 14 条：外围改了不用上机去试“参数对不对、文件名对不对”，由闸门自动核对）。
//
// 核心：Linux 上跑的程序，加 scripts/install/install.sh、upgrade.sh、uninstall.sh、get.sh、scripts/build-release.sh。
// 外围：把核心放进环境里的那一层——
//
//	Windows：get.ps1、scripts/install/install-windows.ps1（在 WSL 里调核心）
//	Mac：scripts/install/install-mac.sh、internal/macvm（在苹果 container 开的虚拟机里调核心）
//
// 这里核对三件事（都只读源码，不连网、不起进程）：
//
//  1. 外围调用安装、升级、卸载脚本（直接调，或经 get.sh 调）时写的每个参数，那个脚本的参数解析里真的有；
//  2. 外围从发行版下载的每个文件名，scripts/build-release.sh 真的会产出；
//  3. 外围在虚拟机或 WSL 里调用的 acornfox 子命令（和它们的参数），都在本包的命令表（Commands）里。
//
// 每一项提取都带“至少找到什么”的断言：脚本或代码改了写法、提取不到东西时，测试会失败并说清楚要改哪里，
// 而不是悄悄少查。TestTierInterfaceCatchesBreakage 在内存里把真实源码改坏几处，证明检查真的会报错。
//
// 加新的外围调用时：照着真实写法写，让这里的提取能认出来；认不出来就改这里的正则（并在 mustFind 里补上）。

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ifaceSources 是核对用到的全部源码文本（测试里按需在内存里改坏再核对）。
type ifaceSources struct {
	installSh, upgradeSh, uninstallSh, getSh, buildRelease string
	getPs1, installWin, installMac                         string
	macvmGo                                                string // internal/macvm 里所有非测试 .go 文件拼在一起
	layoutGo                                               string // internal/edge/layout.go（入口镜像版本的唯一来源）
	cmds                                                   []Command
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "scripts", "install", "install.sh")); err != nil {
		t.Fatalf("找不到仓库根目录（%s 里没有 scripts/install/install.sh）：%v", root, err)
	}
	return root
}

func readText(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.TrimPrefix(string(b), "\xef\xbb\xbf")
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func loadIfaceSources(t *testing.T) ifaceSources {
	t.Helper()
	root := repoRoot(t)
	var macvm strings.Builder
	dir := filepath.Join(root, "acornfox", "internal", "macvm")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		macvm.WriteString(readText(t, root, "acornfox/internal/macvm/"+n))
		macvm.WriteString("\n")
	}
	return ifaceSources{
		installSh:    readText(t, root, "scripts/install/install.sh"),
		upgradeSh:    readText(t, root, "scripts/install/upgrade.sh"),
		uninstallSh:  readText(t, root, "scripts/install/uninstall.sh"),
		getSh:        readText(t, root, "get.sh"),
		buildRelease: readText(t, root, "scripts/build-release.sh"),
		getPs1:       readText(t, root, "get.ps1"),
		installWin:   readText(t, root, "scripts/install/install-windows.ps1"),
		installMac:   readText(t, root, "scripts/install/install-mac.sh"),
		macvmGo:      macvm.String(),
		layoutGo:     readText(t, root, "acornfox/internal/edge/layout.go"),
		cmds:         Commands,
	}
}

// ---------------------------------------------------------------- 小工具

func set(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range items {
		m[s] = true
	}
	return m
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// between 返回从 startRe 第一次出现到其后 endRe 第一次出现（含）的那段文字；找不到返回空串。
func between(src, startRe, endRe string) string {
	s := regexp.MustCompile(startRe).FindStringIndex(src)
	if s == nil {
		return ""
	}
	e := regexp.MustCompile(endRe).FindStringIndex(src[s[1]:])
	if e == nil {
		return ""
	}
	return src[s[0] : s[1]+e[1]]
}

// enclosingFunc 返回包含 marker 的那个 shell 函数（从“名字() {”到行首的“}”）；找不到返回空串。
func enclosingFunc(src, marker string) string {
	idx := strings.Index(src, marker)
	if idx < 0 {
		return ""
	}
	starts := regexp.MustCompile(`(?m)^[a-z_][a-z0-9_]*\(\) \{`).FindAllStringIndex(src[:idx], -1)
	if len(starts) == 0 {
		return ""
	}
	start := starts[len(starts)-1][0]
	e := regexp.MustCompile(`(?m)^\}`).FindStringIndex(src[idx:])
	if e == nil {
		return ""
	}
	return src[start : idx+e[1]]
}

var shLabelRE = regexp.MustCompile(`(?m)^[ \t]*(-[-A-Za-z0-9=*|]*)\)`)

// shFlags 读 shell 参数解析（case 分支）里的所有选项写法：“-y|--yes)”“--version=*)”都算。
func shFlags(region string) map[string]bool {
	out := map[string]bool{}
	for _, m := range shLabelRE.FindAllStringSubmatch(region, -1) {
		for _, f := range strings.Split(m[1], "|") {
			f = strings.TrimSuffix(f, "=*")
			if len(f) >= 2 && strings.HasPrefix(f, "-") {
				out[f] = true
			}
		}
	}
	return out
}

// dashTokens 取一段命令文字里以“-”开头的词（不含变量 $x）。
func dashTokens(s string) []string {
	var out []string
	for _, f := range strings.Fields(s) {
		f = strings.Trim(f, `'"`)
		if len(f) >= 2 && strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "$") {
			out = append(out, f)
		}
	}
	return out
}

func union(ms ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, m := range ms {
		for k := range m {
			out[k] = true
		}
	}
	return out
}

// ---------------------------------------------------------------- 核对本体

// checkInterfaces 做全部核对，返回问题清单（空 = 全部对得上）。
func checkInterfaces(s ifaceSources) []string {
	var p []string
	add := func(format string, a ...any) { p = append(p, fmt.Sprintf(format, a...)) }

	// ---- 核心脚本认哪些参数（读它们自己的参数解析）
	installFlags := shFlags(enclosingFunc(s.installSh, "--mac-vm)"))
	upgradeFlags := shFlags(enclosingFunc(s.upgradeSh, "--rollback)"))
	uninstallFlags := shFlags(between(s.uninstallSh, `for a in "\$@"; do`, `(?m)^done`))
	getFlags := shFlags(between(s.getSh, `(?m)^while \[ \$# -gt 0 \]; do`, `(?m)^done`))
	macShRegion := between(s.installMac, `(?m)^while \[ \$# -gt 0 \]; do`, `(?m)^done`)
	macShFlags := shFlags(macShRegion)
	for _, c := range []struct {
		name  string
		flags map[string]bool
		must  string
	}{
		{"install.sh", installFlags, "--binary"},
		{"upgrade.sh", upgradeFlags, "--binary"},
		{"uninstall.sh", uninstallFlags, "--purge"},
		{"get.sh", getFlags, "--version"},
		{"install-mac.sh", macShFlags, "--version"},
	} {
		if !c.flags[c.must] {
			add("读不到 %s 的参数解析（应该至少有 %s，读到的是 %v）：脚本的参数解析写法变了，要改 tier_interface_test.go 里的提取", c.name, c.must, keys(c.flags))
		}
	}
	verbScript := map[string]map[string]bool{"install": installFlags, "upgrade": upgradeFlags}

	checkSubset := func(who, target string, flags []string, accept ...map[string]bool) {
		ok := union(accept...)
		for _, f := range flags {
			if !ok[f] {
				add("%s 用了参数 %s，但 %s 的参数解析里没有它（认的有：%v）", who, f, target, keys(ok))
			}
		}
	}

	// ---- Windows：在 WSL 里调 install.sh / upgrade.sh / uninstall.sh（直接调或经 get.sh）
	extraRE := regexp.MustCompile(`\$extra = \$\(if \(\$Verb -eq 'install'\) \{ "([^"]*)" \} else \{ "([^"]*)" \}\)`)
	var winRegion []string
	for _, m := range regexp.MustCompile(`return @\('(--[a-z-]+)'\)`).FindAllStringSubmatch(
		between(s.installWin, `function Get-ForceRegionArgs(?:\([^)]*\))? \{`, `(?m)^\}`), -1) {
		winRegion = append(winRegion, m[1])
	}
	if len(winRegion) == 0 {
		add("install-windows.ps1：读不到 Get-ForceRegionArgs 里的 --china、--global（写法变了，要改 tier_interface_test.go）")
	}
	winExtra := map[string][]string{}
	if m := extraRE.FindStringSubmatch(s.installWin); m == nil {
		add("install-windows.ps1：找不到“$extra = $(if ($Verb -eq 'install') …”那一行（写法变了，要改 tier_interface_test.go）")
	} else {
		for i, verb := range []string{"install", "upgrade"} {
			fl := dashTokens(m[i+1])
			if strings.Contains(m[i+1], "$region") {
				fl = append(fl, winRegion...)
			}
			winExtra[verb] = fl
		}
		// 装的时候才有的选项接在后面：$extra = "$extra --from $From"（来源标记）
		for _, am := range regexp.MustCompile(`\$extra = "\$extra([^"]*)"`).FindAllStringSubmatch(s.installWin, -1) {
			winExtra["install"] = append(winExtra["install"], dashTokens(am[1])...)
		}
	}
	localRE := regexp.MustCompile(`\$run = "bash '\$pkg/\$Verb\.sh'([^"]*)"`)
	viaGetRE := regexp.MustCompile(`\$run = "bash '" \+ \(ConvertTo-WslPath \$gs\) \+ "' \$Verb([^"]*)"`)
	lm, gm := localRE.FindStringSubmatch(s.installWin), viaGetRE.FindStringSubmatch(s.installWin)
	if lm == nil {
		add("install-windows.ps1：找不到直接运行 $pkg/$Verb.sh 的那一行（写法变了，要改 tier_interface_test.go）")
	}
	if gm == nil {
		add("install-windows.ps1：找不到经 get.sh 运行 $Verb 的那一行（写法变了，要改 tier_interface_test.go）")
	}
	for _, verb := range []string{"install", "upgrade"} {
		if lm != nil {
			fl := dashTokens(lm[1])
			if strings.Contains(lm[1], "$extra") {
				fl = append(fl, winExtra[verb]...)
			}
			checkSubset("Windows（install-windows.ps1，本地安装包）"+verb, verb+".sh", fl, verbScript[verb])
		}
		if gm != nil {
			fl := dashTokens(gm[1])
			if strings.Contains(gm[1], "$extra") {
				fl = append(fl, winExtra[verb]...)
			}
			checkSubset("Windows（install-windows.ps1，经 get.sh）"+verb, "get.sh 和 "+verb+".sh", fl, getFlags, verbScript[verb])
		}
	}
	purge := regexp.MustCompile(`\$flag = \$\(if \(\$Purge\) \{ '(--[a-z-]+)' \}`).FindStringSubmatch(s.installWin)
	unLocal := regexp.MustCompile(`\$run = "bash '\$pkg/uninstall\.sh' \$flag([^"]*)"`).FindStringSubmatch(s.installWin)
	unGet := regexp.MustCompile(`\$run = "bash '" \+ \(ConvertTo-WslPath \$gs\) \+ "' uninstall \$flag([^"]*)"`).FindStringSubmatch(s.installWin)
	if purge == nil || unLocal == nil || unGet == nil {
		add("install-windows.ps1：找不到卸载那几行（$flag = …、直接运行 uninstall.sh、经 get.sh 运行 uninstall），写法变了，要改 tier_interface_test.go")
	} else {
		checkSubset("Windows（install-windows.ps1，本地安装包）卸载", "uninstall.sh", append([]string{purge[1]}, dashTokens(unLocal[1])...), uninstallFlags)
		checkSubset("Windows（install-windows.ps1，经 get.sh）卸载", "get.sh 和 uninstall.sh", append([]string{purge[1]}, dashTokens(unGet[1])...), getFlags, uninstallFlags)
	}

	// ---- Mac：internal/macvm 在虚拟机里直接运行 install.sh / upgrade.sh
	var macRegion []string
	for _, m := range regexp.MustCompile(`region = "([^"]*)"`).FindAllStringSubmatch(s.macvmGo, -1) {
		macRegion = append(macRegion, dashTokens(m[1])...)
	}
	if len(macRegion) == 0 {
		add("internal/macvm/vmsteps.go：读不到 region = \" --china\" 这类赋值（写法变了，要改 tier_interface_test.go）")
	}
	foundMacRun := set()
	for _, m := range regexp.MustCompile(`(?m)run = ("bash \./(install|upgrade)\.sh[^\n]*)$`).FindAllStringSubmatch(s.macvmGo, -1) {
		var fl []string
		for _, lit := range regexp.MustCompile(`"([^"]*)"`).FindAllStringSubmatch(m[1], -1) {
			fl = append(fl, dashTokens(lit[1])...)
		}
		if strings.Contains(m[1], "region") {
			fl = append(fl, macRegion...)
		}
		if m[2] == "install" {
			// 装的时候才有的选项写在后面的 run += "…" 里（例如来源标记 --from）
			for _, add := range regexp.MustCompile(`(?m)run \+= ("[^"\n]*")`).FindAllStringSubmatch(s.macvmGo, -1) {
				fl = append(fl, dashTokens(add[1])...)
			}
		}
		foundMacRun[m[2]] = true
		checkSubset("Mac（internal/macvm 在虚拟机里）"+m[2], m[2]+".sh", fl, verbScript[m[2]])
	}
	for _, verb := range []string{"install", "upgrade"} {
		if !foundMacRun[verb] {
			add("internal/macvm：找不到在虚拟机里运行 ./%s.sh 的那一行（写法变了，要改 tier_interface_test.go）", verb)
		}
	}

	// ---- get.sh 把哪些动作交给 install-mac.sh，install-mac.sh 认不认
	if m := regexp.MustCompile(`case "\$VERB" in ([a-z|]+)\) SCRIPT=install-mac\.sh`).FindStringSubmatch(s.getSh); m == nil {
		add("get.sh：找不到“case \"$VERB\" in install|upgrade|uninstall) SCRIPT=install-mac.sh”（写法变了，要改 tier_interface_test.go）")
	} else {
		macVerbs := set()
		for _, mm := range regexp.MustCompile(`(?m)^[ \t]*([a-z]+(?:\|[a-z]+)*)\)`).FindAllStringSubmatch(macShRegion, -1) {
			for _, v := range strings.Split(mm[1], "|") {
				macVerbs[v] = true
			}
		}
		for _, v := range strings.Split(m[1], "|") {
			if !macVerbs[v] {
				add("get.sh 把动作 %s 交给 install-mac.sh，但 install-mac.sh 的参数解析里没有这个动作（认的有：%v）", v, keys(macVerbs))
			}
		}
	}

	// ---- 外围调用的 acornfox 子命令都在命令表里
	type acCall struct {
		src          string
		words, flags []string
	}
	var calls []acCall
	parseItems := func(src string, items []string) {
		var words, flags []string
		seenWord := false
		afterFlag := false
		for _, it := range items {
			switch {
			case strings.HasPrefix(it, "-"):
				flags = append(flags, strings.SplitN(it, "=", 2)[0])
				if seenWord {
					afterFlag = true
				}
			case afterFlag: // 选项的值
			default:
				words = append(words, it)
				seenWord = true
			}
		}
		calls = append(calls, acCall{src, words, flags})
	}
	// Windows：'/usr/local/bin/acornfox', 'skill', 'print' 和 sh -c 字符串里的 /usr/local/bin/acornfox apps --json
	for _, m := range regexp.MustCompile(`'/usr/local/bin/acornfox',\s*((?:'[^']*'(?:,\s*)?)+)`).FindAllStringSubmatch(s.installWin, -1) {
		var items []string
		for _, q := range regexp.MustCompile(`'([^']*)'`).FindAllStringSubmatch(m[1], -1) {
			items = append(items, q[1])
		}
		parseItems("Windows（install-windows.ps1）", items)
	}
	shCallRE := regexp.MustCompile(`/usr/local/bin/acornfox((?: +[a-z][a-z-]*| +--[a-z][a-z-]*)+)`)
	for _, m := range shCallRE.FindAllStringSubmatch(s.installWin, -1) {
		parseItems("Windows（install-windows.ps1）", strings.Fields(m[1]))
	}
	// Mac：虚拟机里的命令、Mac 上 exec 的 acornfox、launchd 常驻程序、install-mac.sh
	for _, m := range shCallRE.FindAllStringSubmatch(s.macvmGo, -1) {
		parseItems("Mac（internal/macvm）", strings.Fields(m[1]))
	}
	litRE := regexp.MustCompile(`"([^"]*)"`)
	for _, m := range regexp.MustCompile(`Capture\(ctx, in\.R, nil, (?:exe|in\.Paths\.Program\(\)), ((?:"[^"]*"(?:, )?)+)\)`).FindAllStringSubmatch(s.macvmGo, -1) {
		var items []string
		for _, q := range litRE.FindAllStringSubmatch(m[1], -1) {
			items = append(items, q[1])
		}
		parseItems("Mac（internal/macvm）", items)
	}
	for _, m := range regexp.MustCompile(`args := \[\]string\{("target", "add"[^}]*)\}`).FindAllStringSubmatch(s.macvmGo, -1) {
		var items []string
		for _, q := range litRE.FindAllStringSubmatch(m[1], -1) {
			items = append(items, q[1])
		}
		parseItems("Mac（internal/macvm）", items)
	}
	if regexp.MustCompile(`<string>mac</string>\s*<string>daemon</string>`).MatchString(s.macvmGo) {
		parseItems("Mac（internal/macvm 的 launchd 启动项）", []string{"mac", "daemon"})
	}
	var macShVerbs []string
	if m := regexp.MustCompile(`case "\$VERB" in ([a-z|]+)\) SCRIPT=install-mac\.sh`).FindStringSubmatch(s.getSh); m != nil {
		macShVerbs = strings.Split(m[1], "|")
	}
	if m := regexp.MustCompile(`"\$BIN" mac "\$VERB"([^\n]*)`).FindStringSubmatch(s.installMac); m != nil {
		for _, v := range macShVerbs {
			parseItems("Mac（install-mac.sh）", append([]string{"mac", v}, dashTokens(m[1])...))
		}
	}
	if regexp.MustCompile(`acornfox" mac uninstall`).MatchString(s.installMac) {
		parseItems("Mac（install-mac.sh 卸载时用已装的程序）", []string{"mac", "uninstall"})
	}

	// 每一端至少要提取到这些（提取变少了说明写法变了，不能悄悄少查）
	mustFind := map[string][]string{
		"Windows": {"version", "apps", "skill print"},
		"Mac":     {"version", "apps", "target add", "target remove", "target list", "mac daemon", "mac install", "mac upgrade", "mac uninstall"},
	}
	found := map[string]map[string]bool{"Windows": {}, "Mac": {}}
	tableByPath := map[string]Command{}
	for _, c := range s.cmds {
		tableByPath[c.Path] = c
	}
	macShared := union()
	for _, v := range []string{"mac install", "mac upgrade", "mac uninstall"} {
		macShared = union(macShared, set(tableByPath[v].Flags...))
	}
	for _, c := range calls {
		side := "Windows"
		if strings.HasPrefix(c.src, "Mac") {
			side = "Mac"
		}
		var row Command
		path := ""
		for k := len(c.words); k >= 1; k-- {
			cand := strings.Join(c.words[:k], " ")
			if r, ok := tableByPath[cand]; ok {
				row, path = r, cand
				break
			}
		}
		if path == "" {
			add("%s 调用了 acornfox %s，但命令表（internal/capabilities）里没有这条命令", c.src, strings.Join(c.words, " "))
			continue
		}
		found[side][path] = true
		allowed := set(row.Flags...)
		allowed["--target"], allowed["--app"] = true, true // 全局选项，命令表不逐条写
		if strings.HasPrefix(path, "mac ") {
			// mac install、upgrade、uninstall 共用一套选项（cmd/acornfox/mac.go 的 macFlags），命令表里 uninstall 一行没列全
			allowed = union(allowed, macShared)
		}
		for _, f := range c.flags {
			if !allowed[f] {
				add("%s 调用 acornfox %s 时用了参数 %s，命令表里这条命令没有它（有的：%v）", c.src, path, f, row.Flags)
			}
		}
	}
	for side, paths := range mustFind {
		for _, want := range paths {
			if !found[side][want] {
				add("没有从%s一侧的源码里提取到对 acornfox %s 的调用：写法变了就改 tier_interface_test.go 的提取；这个调用真的去掉了就改 mustFind", side, want)
			}
		}
	}

	// ---- 发布文件名：外围下载的，build-release.sh 都要产出
	produced, prob := producedFiles(s.buildRelease, s.layoutGo)
	p = append(p, prob...)
	type use struct{ name, who string }
	var uses []use
	collect := func(who string, re *regexp.Regexp, text string, group int) {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			uses = append(uses, use{m[group], who})
		}
	}
	collect("get.ps1", regexp.MustCompile(`Get-AssetUrl \$src \$Version '([^']+)'`), s.getPs1, 1)
	collect("install-windows.ps1（从发行版取）", regexp.MustCompile(`\(\$b \+ '([^']+)'\)`), s.installWin, 1)
	collect("install-windows.ps1（本地安装包）", regexp.MustCompile(`\$pkg/([A-Za-z0-9_.-]+)`), s.installWin, 1)
	collect("install-windows.ps1（本地安装包）", regexp.MustCompile(`\$LocalPackage '([A-Za-z0-9_.-]+)'`), s.installWin, 1)
	if strings.Contains(s.installWin, `$pkg/$Verb.sh`) {
		uses = append(uses, use{"install.sh", "install-windows.ps1（本地安装包）"}, use{"upgrade.sh", "install-windows.ps1（本地安装包）"})
	}
	collect("install-mac.sh", regexp.MustCompile(`\bfetch (acornfox_[a-z0-9_]+|SHA256SUMS) `), s.installMac, 1)
	collect("get.sh", regexp.MustCompile(`SCRIPT=([a-z-]+\.sh)`), s.getSh, 1)
	collect("get.sh", regexp.MustCompile(`fetch_asset "\$VERSION" (SHA256SUMS)`), s.getSh, 1)
	// 用户的一行命令直接读发行版里的入口脚本：get.sh、get.ps1 的开头说明里写的 releases/latest/download/<文件>
	collect("get.sh 的一行命令", regexp.MustCompile(`releases/latest/download/([A-Za-z0-9_.-]+)`), s.getSh, 1)
	collect("get.ps1 的一行命令", regexp.MustCompile(`releases/latest/download/([A-Za-z0-9_.-]+)`), s.getPs1, 1)
	for _, m := range regexp.MustCompile(`need := \[\]string\{([^}]*)\}`).FindAllStringSubmatch(s.macvmGo, -1) {
		for _, q := range litRE.FindAllStringSubmatch(m[1], -1) {
			uses = append(uses, use{q[1], "internal/macvm（下载）"})
		}
	}
	collect("internal/macvm（下载）", regexp.MustCompile(`need = append\(need, "([^"]+)"\)`), s.macvmGo, 1)
	collect("internal/macvm（下载）", regexp.MustCompile(`releaseURLs\(ver, "([^"]+)"\)`), s.macvmGo, 1)
	for _, w := range []string{"install.sh", "upgrade.sh", "acornfox_linux_arm64", "SHA256SUMS"} {
		ok := false
		for _, u := range uses {
			if u.who == "internal/macvm（下载）" && u.name == w {
				ok = true
			}
		}
		if !ok {
			add("没有从 internal/macvm 里提取到对发布文件 %s 的下载（写法变了，要改 tier_interface_test.go）", w)
		}
	}
	// 其余各文件各至少要提取到一些
	whoNeeds := map[string]string{"get.ps1": "install-windows.ps1", "install-mac.sh": "acornfox_darwin_arm64", "get.sh": "install-mac.sh",
		"get.sh 的一行命令": "get.sh", "get.ps1 的一行命令": "get.ps1"}
	for who, name := range whoNeeds {
		ok := false
		for _, u := range uses {
			if u.who == who && u.name == name {
				ok = true
			}
		}
		if !ok {
			add("没有从 %s 里提取到对发布文件 %s 的下载（写法变了，要改 tier_interface_test.go）", who, name)
		}
	}
	for _, u := range uses {
		if !produced[u.name] {
			add("%s 要下载发布文件 %s，但 scripts/build-release.sh 不会产出它（会产出的有：%v）", u.who, u.name, keys(produced))
		}
	}
	// 入口包：macvm 用一条正则在 SHA256SUMS 里找，必须真能匹配 build-release.sh 产出的入口包名
	if m := regexp.MustCompile("edgeNameRE = regexp\\.MustCompile\\(`([^`]+)`\\)").FindStringSubmatch(s.macvmGo); m == nil {
		add("internal/macvm/vmsteps.go：找不到 edgeNameRE（写法变了，要改 tier_interface_test.go）")
	} else if re, err := regexp.Compile(m[1]); err != nil {
		add("edgeNameRE 不是合法的正则：%v", err)
	} else {
		hit := false
		for name := range produced {
			if re.MatchString(name) {
				hit = true
			}
		}
		if !hit {
			add("internal/macvm 用来在 SHA256SUMS 里找入口包的正则 %s，匹配不到 build-release.sh 产出的任何文件（产出的有：%v）", m[1], keys(produced))
		}
	}
	// 整体再扫一遍：外围源码里写到的所有程序文件名（acornfox_<系统>_<架构>）都得是会产出的
	progRE := regexp.MustCompile(`acornfox_(?:linux|darwin|windows)_(?:amd64|arm64)(?:\.exe)?`)
	for who, text := range map[string]string{"get.ps1": s.getPs1, "install-windows.ps1": s.installWin, "install-mac.sh": s.installMac, "internal/macvm": s.macvmGo} {
		for _, name := range progRE.FindAllString(text, -1) {
			if !produced[name] {
				add("%s 里写到的程序文件 %s，scripts/build-release.sh 不会产出（产出的有：%v）", who, name, keys(produced))
			}
		}
	}
	return dedupe(p)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// producedFiles 读 scripts/build-release.sh，算出它会放进发行版的文件名（入口包名里的版本取 layout.go 的 ImageVersion）。
func producedFiles(buildRelease, layoutGo string) (map[string]bool, []string) {
	var p []string
	out := map[string]bool{}
	m := regexp.MustCompile(`(?m)^TARGETS="\$\{ACORNFOX_RELEASE_TARGETS:-([^}]*)\}"`).FindStringSubmatch(buildRelease)
	if m == nil {
		p = append(p, "scripts/build-release.sh：读不到默认的编译平台（TARGETS=\"${ACORNFOX_RELEASE_TARGETS:-…}\"），写法变了，要改 tier_interface_test.go")
	} else {
		for _, t := range strings.Fields(m[1]) {
			goos, arch, ok := strings.Cut(t, "/")
			if !ok {
				continue
			}
			name := "acornfox_" + goos + "_" + arch
			if goos == "windows" {
				name += ".exe"
			}
			out[name] = true
		}
	}
	nCopied := 0
	for _, line := range regexp.MustCompile(`(?m)^cp .*$`).FindAllString(buildRelease, -1) {
		for _, mm := range regexp.MustCompile(`\$ROOT/scripts/install/([A-Za-z0-9_.-]+)`).FindAllStringSubmatch(line, -1) {
			out[mm[1]] = true
			nCopied++
		}
		for _, mm := range regexp.MustCompile(`\$ROOT/(get\.(?:sh|ps1))"`).FindAllStringSubmatch(line, -1) {
			out[mm[1]] = true
			nCopied++
		}
	}
	if nCopied == 0 {
		p = append(p, "scripts/build-release.sh：读不到 cp 复制进发行版的脚本（写法变了，要改 tier_interface_test.go）")
	}
	if m := regexp.MustCompile(`assemble\.sh"? "\$DIST/([A-Za-z0-9_.-]+)"`).FindStringSubmatch(buildRelease); m == nil {
		p = append(p, "scripts/build-release.sh：读不到 assemble.sh 生成的 install-windows.ps1（写法变了，要改 tier_interface_test.go）")
	} else {
		out[m[1]] = true
	}
	if strings.Contains(buildRelease, "> SHA256SUMS") {
		out["SHA256SUMS"] = true
	} else {
		p = append(p, "scripts/build-release.sh：读不到生成 SHA256SUMS 的那一行（写法变了，要改 tier_interface_test.go）")
	}
	ev := regexp.MustCompile(`ImageVersion\s*=\s*"([0-9.]+-[0-9]+)"`).FindStringSubmatch(layoutGo)
	arches := regexp.MustCompile(`(?m)^for arch in ([a-z0-9 ]+); do`).FindStringSubmatch(buildRelease)
	pkg := regexp.MustCompile(`pkg="(acornfox-edge_\$\{EDGE_VERSION\}_linux_\$\{arch\}\.tar\.gz)"`).FindStringSubmatch(buildRelease)
	switch {
	case ev == nil:
		p = append(p, "internal/edge/layout.go：读不到 ImageVersion（写法变了，要改 tier_interface_test.go）")
	case arches == nil || pkg == nil:
		p = append(p, "scripts/build-release.sh：读不到入口镜像包的命名（pkg=\"acornfox-edge_${EDGE_VERSION}_linux_${arch}.tar.gz\" 和 for arch in …），写法变了，要改 tier_interface_test.go")
	default:
		for _, a := range strings.Fields(arches[1]) {
			out["acornfox-edge_"+ev[1]+"_linux_"+a+".tar.gz"] = true
		}
	}
	return out, p
}

// ---------------------------------------------------------------- 测试

func TestTierInterfaces(t *testing.T) {
	probs := checkInterfaces(loadIfaceSources(t))
	for _, p := range probs {
		t.Error(p)
	}
}

// replaceOnce 在文本里把 old 换成 new；old 不在文本里就让测试失败（防止改坏用例悄悄失效）。
func replaceOnce(t *testing.T, text, old, new string) string {
	t.Helper()
	if strings.Count(text, old) < 1 {
		t.Fatalf("用例失效：源码里已经没有 %q，要改这条改坏用例", old)
	}
	return strings.Replace(text, old, new, 1)
}

// TestTierInterfaceCatchesBreakage 在内存里把真实源码改坏一处，核对 checkInterfaces 真的会报出来。
func TestTierInterfaceCatchesBreakage(t *testing.T) {
	base := loadIfaceSources(t)
	if probs := checkInterfaces(base); len(probs) > 0 {
		t.Skipf("真实源码本身就有问题（见 TestTierInterfaces），先修那个：%v", probs)
	}
	dropCmd := func(path string) []Command {
		var out []Command
		for _, c := range base.cmds {
			if c.Path != path {
				out = append(out, c)
			}
		}
		if len(out) == len(base.cmds) {
			t.Fatalf("用例失效：命令表里没有 %q", path)
		}
		return out
	}
	cases := []struct {
		name   string
		mutate func(s *ifaceSources)
		want   string // 问题清单里必须出现的字样
	}{
		{"install.sh 不再认 --public-host（Windows、Mac 都在用）", func(s *ifaceSources) {
			s.installSh = replaceOnce(t, s.installSh, "--public-host)", "--public-hostx)")
		}, "--public-host"},
		{"install.sh 不再认 --mac-vm（Mac 在用）", func(s *ifaceSources) {
			s.installSh = replaceOnce(t, s.installSh, "--mac-vm)", "--mac-vmx)")
		}, "--mac-vm"},
		{"upgrade.sh 不再认 --edge-package（Mac 在用）", func(s *ifaceSources) {
			s.upgradeSh = replaceOnce(t, s.upgradeSh, "--edge-package)", "--edge-packagex)")
		}, "--edge-package"},
		{"uninstall.sh 不再认 --purge（Windows 在用）", func(s *ifaceSources) {
			s.uninstallSh = replaceOnce(t, s.uninstallSh, "--purge)", "--purgex)")
		}, "--purge"},
		{"build-release.sh 不再带 install-mac.sh", func(s *ifaceSources) {
			s.buildRelease = replaceOnce(t, s.buildRelease, ` "$ROOT/scripts/install/install-mac.sh"`, "")
		}, "install-mac.sh"},
		{"build-release.sh 不再编 darwin/arm64（Mac 要下这个程序）", func(s *ifaceSources) {
			s.buildRelease = replaceOnce(t, s.buildRelease, " darwin/arm64}", "}")
		}, "acornfox_darwin_arm64"},
		{"build-release.sh 不再带 get.ps1（Windows 的一行命令要读它）", func(s *ifaceSources) {
			s.buildRelease = replaceOnce(t, s.buildRelease, `cp "$ROOT/get.ps1"`, `cp "$ROOT/get-x.ps1"`)
		}, "get.ps1"},
		{"入口包改了命名（build-release.sh 一侧）", func(s *ifaceSources) {
			s.buildRelease = replaceOnce(t, s.buildRelease, `pkg="acornfox-edge_${EDGE_VERSION}_linux_${arch}.tar.gz"`, `pkg="acornfox-edge_${EDGE_VERSION}_${arch}.tar.gz"`)
		}, "入口镜像包的命名"},
		{"macvm 找入口包的正则和产出的名字对不上", func(s *ifaceSources) {
			s.macvmGo = replaceOnce(t, s.macvmGo, `_linux_arm64\.tar\.gz`+"`", `_linux_arm65\.tar\.gz`+"`")
		}, "匹配不到"},
		{"macvm 要下载的程序文件名写错", func(s *ifaceSources) {
			s.macvmGo = strings.ReplaceAll(s.macvmGo, "acornfox_linux_arm64", "acornfox_linux_arm65")
		}, "acornfox_linux_arm65"},
		{"命令表去掉 mac daemon（launchd 常驻程序在用）", func(s *ifaceSources) {
			s.cmds = dropCmd("mac daemon")
		}, "mac daemon"},
		{"命令表去掉 target add（Mac 登记 local 在用）", func(s *ifaceSources) {
			s.cmds = dropCmd("target add")
		}, "target add"},
		{"命令表去掉 skill print（Windows 装 Skill 在用）", func(s *ifaceSources) {
			s.cmds = dropCmd("skill print")
		}, "skill print"},
		{"Windows 调的子命令写错", func(s *ifaceSources) {
			s.installWin = replaceOnce(t, s.installWin, `'skill', 'print'`, `'skill', 'prnit'`)
		}, "prnit"},
		{"Windows 给 install.sh 多传一个它不认的参数", func(s *ifaceSources) {
			s.installWin = replaceOnce(t, s.installWin, `"--wsl --public-host $PublicHost $region"`, `"--wsl --public-host $PublicHost --no-such-flag $region"`)
		}, "--no-such-flag"},
		{"Mac 给 install.sh 多传一个它不认的参数", func(s *ifaceSources) {
			s.macvmGo = replaceOnce(t, s.macvmGo, `" --mac-vm --public-host "`, `" --mac-vm --no-such-flag --public-host "`)
		}, "--no-such-flag"},
		{"install-mac.sh 给 acornfox mac 传一个命令表里没有的参数", func(s *ifaceSources) {
			s.installMac = replaceOnce(t, s.installMac, `"$BIN" mac "$VERB" --version "$VERSION"`, `"$BIN" mac "$VERB" --no-such-flag --version "$VERSION"`)
		}, "--no-such-flag"},
		{"get.sh 把一个 install-mac.sh 不认的动作交给它", func(s *ifaceSources) {
			s.getSh = replaceOnce(t, s.getSh, `install|upgrade|uninstall) SCRIPT=install-mac.sh`, `install|upgrade|uninstall|doctor) SCRIPT=install-mac.sh`)
		}, "doctor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := base
			c.mutate(&s)
			probs := checkInterfaces(s)
			if len(probs) == 0 {
				t.Fatalf("改坏了却没有报任何问题")
			}
			joined := strings.Join(probs, "\n")
			if !strings.Contains(joined, c.want) {
				t.Fatalf("报了问题，但没提到 %q：\n%s", c.want, joined)
			}
		})
	}
}
