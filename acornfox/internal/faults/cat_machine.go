package faults

// machineFaults are the codes of "what this machine can do right now":
// GET /v1/machine, `acornfox machine` and the part machine-<server>.md that
// `acornfox skill install` and `acornfox skill refresh` keep in the Skill
// directory (internal/machine).
var machineFaults = []Fault{
	{Code: "MACHINE-01", Group: "MACHINE", Title: "本机能力有一块没算全", Level: "P3",
		Explain:  "acornfox machine 现场算这台服务器上有什么：应用、解决方案、服务、手册。应用目录读不了、某个应用用到哪些服务读不出来、服务登记表里有几条解不开时，这一块先空着，其余照常给，并在 warnings 里写明是哪一块（part）。答案不完整，但不会是错的。",
		Next:     "运行 acornfox --json machine 看 warnings 里每一项的 part 和 message：manuals 是应用目录读不了（应用名字、一句话、手册没查到），先 acornfox catalog sync；services 是服务登记表的问题，看 acornfox services list；apps 是某个应用的情况读不出来，acornfox status 应用名",
		Auto:     "没有；缺的那一块留空，别的照常",
		Symptoms: []string{"acornfox --json machine 的 warnings 不是空的，里面的 code 是 MACHINE-01", "分册 machine-服务器名.md 末尾有一行“注意（MACHINE-01）”"},
		Causes: []Cause{
			{Text: "应用目录读不了：从没同步过又读不了内置的，或缓存坏了", Check: "warnings 里 part 是 manuals；acornfox catalog sources 和 acornfox doctor 的“应用目录同步”一项", Auto: true},
			{Text: "某个应用的清单读不出来（目录里已经没有它，或它用的是旧格式）", Check: "warnings 里 part 是 apps，message 写着是哪个应用；acornfox apps vars 应用 也读不了", Auto: true},
			{Text: "服务登记表里有几条存的值解不开（数据目录里的加密钥匙换过或丢了）", Check: "warnings 里 part 是 services，message 列着是哪几条；acornfox services list 末尾有同样一行", Auto: true},
			{Text: "服务器上的程序正在升级、重启，应用列表暂时读不了", Check: "稍等一两分钟再运行 acornfox --json machine，warnings 没了就是这个"},
		},
		Steps: []string{"acornfox --json machine（看 warnings）", "acornfox doctor（看应用目录同步、应用用的服务两项）", "按 part 查：manuals → acornfox catalog sources；services → acornfox services list；apps → acornfox status 应用名"},
		Actions: []Action{
			{Text: "acornfox catalog sync 让服务器同步一次应用目录，再运行 acornfox --json machine", Tier: 1},
			{Text: "把 warnings 里的 message 原样告诉用户；缺的那一块当成不知道，不要猜", Tier: 0},
			{Text: "值解不开的服务：不要删，把 doctor 的输出交给维护的人", Tier: 0},
		},
		Ask:     "服务的值解不开时：停下问用户，不动那几条服务",
		Verify:  "acornfox --json machine 的 warnings 是空的",
		Related: []string{"CAT-SVC-08"},
		Inject:  "测试机上登记一条服务，再把数据目录里存服务值用的钥匙文件换成别的内容，运行 acornfox --json machine：warnings 里有 part 是 services 的一项（恢复：把钥匙文件换回来）"},
	{Code: "MACHINE-02", Group: "MACHINE", Title: "“本机能力”分册写不进 Skill 目录", Level: "P3",
		Explain:  "acornfox skill install、acornfox skill refresh 和装、删、升级应用之后的自动刷新，会在 Skill 目录里写 machine-服务器名.md。目录没有写权限、磁盘满了或路径建不了时写不进去；分册只是缓存，写不进不影响任何命令，AI 工作台改用 acornfox --json machine 直接取。",
		Next:     "不加 --json 运行 acornfox skill refresh，看 ✗ 那一行写的路径和原因；Skill 目录以前被 sudo 建过的，请用户确认后改回自己的属主；要马上用，直接运行 acornfox --json machine",
		Auto:     "没有；已装好的 Skill 和别的分册不受影响",
		Symptoms: []string{"acornfox skill refresh 报“写入 … 失败”并带 MACHINE-02", "acornfox --json skill refresh 的 failed 里一项的 code 是 MACHINE-02"},
		Causes: []Cause{
			{Text: "skills 目录没有写权限（例如以前用 sudo 建过）", Check: "ls -ld ~/.claude/skills/acornfox ~/.codex/skills/acornfox，属主是 root"},
			{Text: "磁盘满了", Check: "df -h ~"},
			{Text: "--dir 指的位置建不了目录或不可写", Check: "不加 --json 再运行，看 ✗ 那一行里的路径"},
		},
		Steps: []string{"acornfox skill refresh（不加 --json）", "ls -ld ~/.claude/skills/acornfox", "df -h ~"},
		Actions: []Action{
			{Text: "acornfox --json machine 直接取，不靠分册", Tier: 0},
			{Text: "acornfox skill refresh --dir 一个能写的 skills 目录", Tier: 1},
			{Text: "用 sudo 改 skills 目录的属主", Tier: 2},
		},
		Ask:     "要用 sudo 改文件时",
		Verify:  "acornfox skill refresh 每台服务器后面都是 ✓，Skill 目录里有 machine-服务器名.md",
		Related: []string{"install_failed", "MACHINE-03"},
		Inject:  "在测试机上把 ~/.claude/skills/acornfox 的权限改成只读，运行 acornfox skill refresh（恢复：chmod u+w 回来）"},
	{Code: "MACHINE-03", Group: "MACHINE", Title: "这台服务器的“本机能力”分册没能写：连不上，或服务器上的 AcornFox 太旧", Level: "P3",
		Explain:  "写分册要先连上服务器读 acornfox machine 的结果。服务器连不上，或者它上面的 AcornFox 还没有这个接口（server_too_old）时，那台服务器的分册就没写成，其他服务器照常。skill install 只提醒，skill refresh 在所有服务器都没成时退出码是 1。",
		Next:     "先运行 acornfox --target 服务器名 status 看能不能连上（连不上看里面的连接编号）；能连上但提示服务器版本旧，让用户先升级服务器上的 AcornFox；好了再 acornfox skill refresh",
		Auto:     "没有；这台服务器上一份分册（如果有）不动",
		Symptoms: []string{"acornfox skill install 或 skill refresh 打印“服务器 … 的‘本机能力’分册没写成”并带 MACHINE-03", "acornfox --json skill refresh 的 failed 里一项的 code 是 MACHINE-03"},
		Causes: []Cause{
			{Text: "服务器关机、网络不通或 SSH 登不上", Check: "acornfox --target 服务器名 status 报连接编号（acornfox faults 那个编号）"},
			{Text: "服务器上的 AcornFox 比这个命令行旧，没有 machine 接口", Check: "acornfox --target 服务器名 machine 报 server_too_old"},
			{Text: "登记的服务器名已经不存在（target remove 过）", Check: "acornfox target list 里没有这个名字"},
		},
		Steps: []string{"acornfox target list", "acornfox --target 服务器名 status", "acornfox --target 服务器名 machine"},
		Actions: []Action{
			{Text: "照 status 报的连接编号处理（acornfox faults 编号）", Tier: 0},
			{Text: "连上之后再运行 acornfox skill refresh", Tier: 1},
		},
		Ask:     "要升级服务器上的 AcornFox 时（先问用户）",
		Verify:  "acornfox skill refresh 这台服务器后面是 ✓",
		Related: []string{"MACHINE-02", "CLI-UPDATE-01"},
		Inject:  "在测试机上 acornfox target add 一台连不上的服务器（--url http://127.0.0.1:1），运行 acornfox skill refresh"},
}
