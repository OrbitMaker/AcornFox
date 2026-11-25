package faults

// platUpdFaults are the codes of the platform's new-version reminder
// (internal/platformupdate): the server asks Gitee or GitHub for the newest
// stable AcornFox every 6 hours. `acornfox update`, `acornfox status`,
// `acornfox doctor`, the console's "设置 › 这台机器" and the bell show the
// same codes.
var platUpdFaults = []Fault{
	{Code: "PLAT-UPD-01", Group: "UPD", Title: "AcornFox 有新版本可以升级", Level: "P3",
		Explain:  "这不是故障，是一条提醒：服务器查到线上有比现在更新的正式版。现在的版本照常能用，什么时候升由用户定。升级会短暂停一下服务（应用自己的容器不重启），脚本先备份、没成功自动恢复原样。",
		Next:     "运行 acornfox update 看最新版本号、发行说明地址和这台机器该用的那一条升级命令；先把新版本和发行说明告诉用户，问他要不要现在升，同意以后再运行那条命令（Windows、Mac 的命令和 Linux 服务器不一样，以 acornfox update 给的为准）",
		Auto:     "没有；服务器只提醒，不会自己升级",
		Symptoms: []string{"acornfox status 最后一行写“AcornFox 有新版本 …”", "acornfox update 写“AcornFox 有新版本 …”", "acornfox doctor 里一行“AcornFox 有新版本 …”（!）", "控制台“设置 › 这台机器”里升级那一行写“有新版本 …”，铃铛里有一条“AcornFox 有新版本”"},
		Causes: []Cause{
			{Text: "线上发了新的正式版，这台机器还在用旧的", Check: "acornfox update 里 latest 比 current 高", Auto: true},
			{Text: "这是自己编出来的程序（版本号带 -dev）：线上的同一个数字的正式版也会被当成“更新”", Check: "acornfox version 里带 -dev；不用管", Auto: true},
		},
		Steps: []string{"acornfox update（看 latest、notes_url、upgrade_command、upgrade_where）", "把发行说明地址和新版本号告诉用户，问他要不要现在升"},
		Actions: []Action{
			{Text: "acornfox update check 现在就重新查一次（只读线上）", Tier: 0},
			{Text: "照 acornfox update 给的升级命令升级：先用 --plan（Linux、Mac 把 upgrade 换成 upgrade --plan；Windows 末尾加 -Plan）看会做什么，用户同意后再真升", Tier: 2},
		},
		Ask:      "要升级之前：先说新版本号和发行说明地址、会短暂停服务，等用户明确同意（升级由这条命令做，不是这个提醒做）",
		Verify:   "升级后 acornfox version 是新版本号，acornfox update 写“已是最新”",
		Rollback: "升级脚本自己在失败时恢复原样；升级成功后平台不能退回旧版本",
		Related:  []string{"PLAT-UPD-02", "CLI-UPDATE-01"},
		Inject:   "测试机上用环境变量 ACORNFOX_UPDATE_GITHUB_URL、ACORNFOX_UPDATE_GITEE_URL 指向一个本机假的发行版列表（里面放一个比现在高的正式版），重启 acornfox-server，acornfox update check"},
	{Code: "PLAT-UPD-02", Group: "UPD", Title: "好几天查不到 AcornFox 的最新版本", Level: "P3",
		Explain:  "服务器每 6 小时去 Gitee 或 GitHub 查一次最新的正式版，一边不通换另一边。连续 3 天以上两边都没查到，说明这台机器连不上这两个网站，或者发行版列表的返回读不懂。应用和平台照常运行，只是看不到“有新版本”的提醒。",
		Next:     "运行 acornfox update check 现在查一次，看 error 里写的是哪一边、什么原因；确认这台机器能打开 gitee.com 和 github.com（国内机器只通 Gitee 就够）；公司网络有代理的看服务的代理设置；不想要这个提醒就关掉（ACORNFOX_UPDATE_CHECK=off）",
		Auto:     "没有；每 6 小时自动再试，上次查到的结果还留着",
		Symptoms: []string{"acornfox update 写“已经好几天查不到最新版本了”", "acornfox doctor 里一行“好几天查不到 AcornFox 的最新版本”（!）", "控制台“设置 › 这台机器”里升级那一行写“暂时查不到最新版本”"},
		Causes: []Cause{
			{Text: "这台机器连不上 Gitee 也连不上 GitHub（断网、防火墙、DNS）", Check: "acornfox update check --json 里 platform_update.error 写“连不上”；在这台机器上 curl -sI https://gitee.com 和 https://github.com", Auto: true},
			{Text: "对方的接口限流或临时出错（返回 403、429、5xx）", Check: "error 里写“返回 403”等；过几小时再 acornfox update check", Auto: true},
			{Text: "发行版列表里一个正式版都没有，或返回的格式变了", Check: "error 里写“没有正式版”或“返回的内容看不懂”", Auto: true},
			{Text: "服务的环境里把查询地址改成了测试用的地址（ACORNFOX_UPDATE_GITEE_URL、ACORNFOX_UPDATE_GITHUB_URL）", Check: "systemctl show acornfox-server -p Environment 里有这两个变量"},
		},
		Steps: []string{"acornfox update check --json（看 platform_update.error、failures、last_ok_at）", "curl -sI https://gitee.com；curl -sI https://github.com", "systemctl show acornfox-server -p Environment"},
		Actions: []Action{
			{Text: "acornfox update check 再查一次（只读线上）", Tier: 0},
			{Text: "把 error 原样告诉用户；这只影响“有新版本”的提醒，不影响应用", Tier: 0},
			{Text: "用户不想要这个提醒：在服务的环境里设 ACORNFOX_UPDATE_CHECK=off 并重启 acornfox-server", Tier: 2},
		},
		Ask:     "要改服务的环境变量、重启 acornfox-server 之前先问用户",
		Verify:  "acornfox update check 之后 platform_update.ok 是 true，acornfox doctor 里没有这一行",
		Related: []string{"PLAT-UPD-01"},
		Inject:  "测试机上把 ACORNFOX_UPDATE_GITEE_URL、ACORNFOX_UPDATE_GITHUB_URL 都指向 http://127.0.0.1:9/，重启 acornfox-server，acornfox update check 得到“连不上”；要看“好几天”，把数据目录里 platform-update.json 的 first_fail_at 改成 4 天前"},
}
