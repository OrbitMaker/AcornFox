package setup

// Faults is the catalog of setup fault codes (the single source: the
// console, `acornfox setup`, `acornfox doctor` and the Skill all show these
// codes). Each entry says what the user sees, the likely causes in the order
// to check them, and the next step. NeedsUser marks faults only the user can
// clear (a reboot, enabling virtualization, an admin confirmation).
type FaultDef struct {
	Code      string   `json:"code"`
	Title     string   `json:"title"`
	Causes    []string `json:"causes"`
	Next      string   `json:"next"`
	NeedsUser bool     `json:"needs_user"`
}

var Faults = []FaultDef{
	{Code: "SETUP-NET-01", Title: "下载源都连不上",
		Causes: []string{"这台电脑没有联网", "开着代理或公司网络拦了下载地址", "DNS 解析失败"},
		Next:   "确认能上网后点“重试”（或运行 acornfox setup --resume）；国内网络可以换下载源：acornfox setup --resume --mirror <地址>"},
	{Code: "SETUP-NET-02", Title: "下载 45 秒没有进展，所有下载源都试过了",
		Causes: []string{"网络暂时不通或很慢", "代理软件拦截了连接", "下载源临时故障"},
		Next:   "已下载的部分会保留，点“重试”从断点接着下；一直不行就换下载源或关掉代理再试"},
	{Code: "SETUP-SUM-01", Title: "下载的文件校验不通过",
		Causes: []string{"下载源上的文件被替换或损坏", "下载过程中数据出错"},
		Next:   "已删掉坏文件，点“重试”会重新下载；换一个下载源更稳妥。反复出现请把诊断信息发给我们"},
	{Code: "SETUP-RT-01", Title: "Docker 装了但没有运行，需要你选用哪个运行环境",
		Causes: []string{"Docker Desktop 没有打开"},
		Next:   "在页面上选“用 AcornFox 自带的运行环境”或“打开 Docker Desktop”；终端：acornfox setup --runtime builtin", NeedsUser: true},
	{Code: "SETUP-RT-02", Title: "自带的运行环境没能启动",
		Causes: []string{"虚拟机镜像不完整", "磁盘空间不够（至少要 10 GB）", "上次没有正常关闭，虚拟机还占着"},
		Next:   "点“重试”；还不行运行 acornfox doctor 看具体哪一项，再把诊断信息发给我们"},
	{Code: "SETUP-RT-03", Title: "连不上 Docker",
		Causes: []string{"Linux 上 Docker 服务没有运行（systemctl status docker）", "当前用户没有 Docker 权限，而且连不上 acornfox-server（连得上、执行器正常时 doctor 判通过：Docker 由后台服务访问）", "DOCKER_HOST 指向了不存在的地址"},
		Next:   "Linux 服务器用 install.sh 安装会自动装好 Docker；已经装了就 sudo systemctl start docker 后重试"},
	{Code: "SETUP-RT-04", Title: "Docker Desktop 没有按时启动",
		Causes: []string{"Docker Desktop 第一次启动要接受协议或登录", "它正在更新", "WSL 2（Windows）没有装好"},
		Next:   "看一眼 Docker Desktop 的窗口，处理完它的提示后点“重试”；或者改用 AcornFox 自带的运行环境"},
	{Code: "SETUP-WIN-01", Title: "需要管理员确认才能打开 WSL 2",
		Causes: []string{"这台电脑没有开 WSL 2 功能"},
		Next:   "点“继续”，在 Windows 弹出的管理员确认里选“是”", NeedsUser: true},
	{Code: "SETUP-WIN-02", Title: "需要重启电脑",
		Causes: []string{"刚打开 WSL 2 / 虚拟机平台功能，Windows 要重启才生效"},
		Next:   "保存好手上的工作后重启电脑；重启后 AcornFox 会自动接着装", NeedsUser: true},
	{Code: "SETUP-WIN-03", Title: "这台电脑不能运行 WSL 2 虚拟机",
		Causes: []string{"BIOS/UEFI 里关了虚拟化（Intel VT-x / AMD-V）", "这台 Windows 本身是虚拟机且没开嵌套虚拟化", "Windows 版本太旧（需要 Windows 10 2004 以上或 Windows 11）"},
		Next:   "在 BIOS 里打开虚拟化后重试；虚拟机里运行的话，要在宿主机上为它打开嵌套虚拟化", NeedsUser: true},
	{Code: "SETUP-WIN-04", Title: "管理员确认被取消或失败",
		Causes: []string{"在管理员确认里选了“否”", "当前账号不是管理员"},
		Next:   "点“继续”再试一次，在弹窗里选“是”；不是管理员账号的话，请管理员登录后操作", NeedsUser: true},
	{Code: "SETUP-WIN-05", Title: "WSL 没有把端口转发到本机",
		Causes: []string{"WSL 的 localhost 转发被关掉了（%USERPROFILE%\\.wslconfig 里写了 localhostForwarding=false）", "WSL 网络服务出错"},
		Next:   "把 .wslconfig 里的 localhostForwarding 改回 true 或删掉这一行，运行 wsl --shutdown，再运行 acornfox setup --resume"},
	{Code: "SETUP-EDGE-01", Title: "网关组件没有准备好",
		Causes: []string{"Caddy 镜像没下载下来（看上一条下载记录）", "端口 18808 或 18810–18859 被别的程序占了", "Docker 里有旧的 acornfox-caddy 容器卡住"},
		Next:   "点“重试”；端口被占时运行 acornfox doctor 看是哪个端口，关掉占用的程序后再试"},
	{Code: "SETUP-CHK-01", Title: "自检没有通过",
		Causes: []string{"见自检列表里没打勾的那一项"},
		Next:   "按没通过那一项的说明处理后点“重试”"},
	{Code: "SETUP-CHK-02", Title: "Docker 不能创建和删除容器",
		Causes: []string{"Docker 磁盘满了", "Docker 守护进程出错"},
		Next:   "清理 Docker 空间（docker system prune）或重启运行环境后重试"},
	{Code: "SETUP-CHK-03", Title: "网关管理口不安全或不应答",
		Causes: []string{"18808 端口被别的程序占用", "Caddy 容器没有运行"},
		Next:   "运行 acornfox doctor 查看；关掉占用 18808 的程序后重试"},
	{Code: "SETUP-CHK-04", Title: "局域网打不开",
		Causes: []string{"没有连接局域网（只有本机能打开，不影响使用）", "Windows：没有 AcornFox 防火墙规则（准备时选了“只在本机用”，或者管理员确认被取消）", "Windows：当前网络是“公用网络”，防火墙规则只在专用网络生效", "防火墙拦了应用端口", "运行环境只绑定了 127.0.0.1"},
		Next:   "只在本机用可以忽略。要在局域网打开：Windows 上运行 acornfox setup --confirm-admin 加防火墙规则；网络是公用的话，到 设置 → 网络 把它改成“专用网络”；然后重试"},
	{Code: "SETUP-CHK-05", Title: "局域网转发没有启动",
		Causes: []string{"局域网地址上的应用端口被别的程序占用了", "刚换了网络，转发还没跟上（10 秒内会自动重试）"},
		Next:   "运行 acornfox doctor 看是哪些端口被占；关掉占用的程序，过 10 秒再看"},
	// Linux server checks in `acornfox doctor` (service, disk space, ports).
	{Code: "SETUP-SVC-01", Title: "后台服务 acornfox-server 没有应答",
		Causes: []string{"Docker 重启或升级后，acornfox-server、acornfox-runner 跟着停了，没有自动起来", "acornfox-runner 停了或连不上 Docker（doctor 会写“执行器 acornfox-runner 没有应答”）", "服务启动失败（看 journalctl -u acornfox-server -n 50）", "用的不是 root，也不在 acornfox-users 组里，连不上服务（这时只是警告）"},
		Next:   "运行 sudo systemctl restart acornfox-runner acornfox-server，再运行 sudo acornfox doctor；还不行就把 journalctl -u acornfox-server -n 50 的输出交给用户"},
	{Code: "SETUP-DISK-01", Title: "磁盘剩余空间不够（少于 10 GB 警告，少于 3 GB 不通过）",
		Causes: []string{"系统盘本来就小（20 GB 的盘装完系统和 Docker 后所剩不多）", "旧的镜像、构建缓存、停掉的容器占了空间", "应用的数据或日志越写越多"},
		Next:   "doctor 会写出是数据目录还是 Docker 数据目录所在的分区。先运行 docker system df 看占用，确认不需要后 docker system prune 清理（会删掉停掉的容器和没用的镜像，先问用户）；还不够就在云厂商控制台扩容磁盘"},
	{Code: "SETUP-PORT-01", Title: "端口 80 或 443 被别的程序占用",
		Causes: []string{"服务器上原来装了 nginx、Apache、宝塔、1Panel 或宿主机上的 Caddy", "别的 Docker 容器映射了 80 或 443"},
		Next:   "和宝塔、1Panel、nginx 等共存时（安装用了 --allow-busy-80-443）不用处理，doctor 只给警告：应用照常用 http://服务器IP:端口 访问，不要停用户在用的程序；要用域名就在原来的面板里反向代理到 127.0.0.1:应用端口。doctor 判不通过，说明 AcornFox 里绑定了域名或开了公网控制台：先问用户占用的程序还要不要。不要了才停掉它（例如 sudo systemctl disable --now nginx），再运行 sudo systemctl restart acornfox-server；要保留就删掉 AcornFox 里绑定的域名（acornfox domain remove 域名）或关掉公网控制台（acornfox public-console disable）", NeedsUser: true},
	{Code: "SETUP-PORT-02", Title: "网关管理口 18808 被别的程序占用",
		Causes: []string{"别的程序或容器占了 18808", "旧版留下的进程还没退出"},
		Next:   "按 doctor 写出的 PID 确认是什么程序，停掉后运行 sudo systemctl restart acornfox-server；应用的网址和域名都依赖这个端口"},
	{Code: "SETUP-PORT-03", Title: "应用端口段 18810–18899 里有端口被别的程序占用",
		Causes: []string{"别的程序或 Docker 容器正好用了这一段里的端口"},
		Next:   "应用的端口在创建时分好、以后不变，分到被占端口的应用会一直上线失败（删掉重建往往还会分到同一个端口）。按 doctor 写出的端口和 PID 找到占用的程序，先问用户能不能停掉；能停就停掉后重新部署，不能停就把情况告诉用户：这些端口上的应用上线不了", NeedsUser: true},
	{Code: "SETUP-PORT-04", Title: "局域网控制台端口 18802 被别的程序占用",
		Causes: []string{"别的程序或容器占了 18802"},
		Next:   "不开局域网控制台可以忽略；要开的话先停掉占用的程序再运行 acornfox lan-console enable"},
	{Code: "SETUP-PORT-05", Title: "没有权限查看是哪个程序占用了端口",
		Causes: []string{"doctor 不是用 root 运行的，看不到别的用户的进程"},
		Next:   "普通用户运行时出现是正常的：后台服务正常时，AcornFox 自己的端口（18808、应用端口、开着的局域网控制台端口）已经认出，不在这里；剩下的要确认就用 sudo acornfox doctor 再查一次"},
	// The Mac build of this release is a command-line client only
	// (maclocal.go): install, serve and setup/doctor against the Mac itself
	// stop with this code.
	{Code: MacLocalCode, Title: "Mac 本机运行即将支持",
		Causes: []string{"这个版本的 Mac 版只当命令行客户端，用来管理 Linux 服务器上的 AcornFox；在 Mac 本机安装和运行（install、serve，以及对本机的 setup、doctor）还没有开放"},
		Next:   MacLocalMessage + "。" + MacLocalHint, NeedsUser: true},
	{Code: "SETUP-GEN-01", Title: "准备运行环境时出错",
		Causes: []string{"见错误信息"},
		Next:   "点“重试”；还不行运行 acornfox doctor，把诊断信息发给我们"},
}

// FaultByCode looks a code up in Faults.
func FaultByCode(code string) (FaultDef, bool) {
	for _, f := range Faults {
		if f.Code == code {
			return f, true
		}
	}
	return FaultDef{}, false
}
