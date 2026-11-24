# AcornFox 故障目录

本文件由 `acornfox faults --markdown > docs/faults.md` 生成，唯一来源是程序里的 `internal/faults`，不要手改。
同样的内容在命令行里看：`acornfox faults` 列出全部编号，`acornfox faults <编号>` 看一条（加 `--json` 给程序读）。

怎么用：看到一个编号（安装脚本打印的 `故障编号: …`、`acornfox doctor` 每项后面括号里的、命令 `--json` 输出里的 `code` 或 `diagnosis.code`、控制台“运行环境”页上的），先运行 `acornfox faults <编号>`，按“排查顺序”一条条判定，再按“处置”做。

处置分三级：0 级只读，随时可以做；1 级可以直接做（不删数据、不花钱、不对外开放、做了能撤回）；2 级必须先问用户、用户同意后才做。拿不准就按 2 级。

等级：P1 应用或服务停了，马上处理；P2 功能用不了，尽快处理；P3 提示或用法问题，不急。

要停下问用户、或者要反馈给维护者时，带上：acornfox doctor --json 的输出、出错那条命令加 --json 的完整输出（或安装、升级脚本最后 30 行）、acornfox --json version。

## 总表

| 编号 | 等级 | 标题 | 下一步 |
| --- | --- | --- | --- |
| INSTALL-ROOT-01 | P3 | 没有用 root 或 sudo 运行安装脚本 | 命令里加 sudo：把 \| bash 改成 \| sudo bash（参数照旧）后重新运行 |
| INSTALL-OS-01 | P2 | 系统不是支持的 Ubuntu 或 Debian | 不能装：请用户在云厂商控制台把系统重装成 Ubuntu 24.04（重装会清空系统盘），再运行安装命令 |
| INSTALL-OS-02 | P3 | Ubuntu 或 Debian 的版本不在支持列表 | 建议把系统重装成 Ubuntu 24.04；用户坚持要试，他同意后加 -s -- --force-os 重跑 |
| INSTALL-ARCH-01 | P2 | CPU 架构不是 x86_64 或 arm64 | 不能装，请用户换一台 x86_64 或 arm64 的服务器 |
| INSTALL-DISK-01 | P2 | /var/lib 所在磁盘剩余不到 2 GB | 请用户清理磁盘或扩容系统盘后重新运行（扩容花钱，用户决定） |
| INSTALL-PORT-01 | P2 | AcornFox 自己要用的端口（18800、18801、18807）被别的程序占用 | 把脚本列出的程序和 PID 转述给用户，问用户能不能停掉或改端口；处理后重跑 |
| INSTALL-PORT-02 | P3 | 80 或 443 被别的程序占用（常见：宝塔、1Panel、nginx） | 问用户二选一：停掉原来的程序后重跑，或者加 -s -- --allow-busy-80-443 共存（不能在 AcornFox 里绑域名、开 HTTPS 和公网控制台） |
| INSTALL-PORT-03 | P2 | 应用端口段 18810-18899 里有端口被别的程序占用 | 转述占用的程序，问用户能不能停掉或改端口；处理后重跑 |
| INSTALL-CLOUD-01 | P3 | --cloud 或 ACORNFOX_CLOUD_OVERRIDE 的值不认识 | 这是测试用的参数，去掉后重跑 |
| INSTALL-DOWN-01 | P3 | 服务器上已有更新版本的 AcornFox（或它的数据），要装的版本比它旧 | 什么都没改。不要换参数重试；要换新版本就用升级命令。确实要装旧版本只能先卸载并删除数据（--purge，不可恢复），必须用户明确同意 |
| INSTALL-VER-01 | P3 | 脚本和要装的版本（或 --binary 给的程序）不是同一个版本 | 什么都没改。不要找别的脚本来凑；用一行命令并写明版本，它会取那个版本自带的脚本 |
| INSTALL-SRC-01 | P3 | 直接运行了源码仓库里的安装、升级脚本 | 什么都没改。照报错里给的一行命令重新运行（入口是 get.sh） |
| INSTALL-GET-01 | P2 | 引导脚本查不到最新的正式版号 | 什么都没改。先看能不能连上 gitee.com 和 github.com；也可以在命令后面写明版本 |
| INSTALL-GET-02 | P2 | 引导脚本下载那个版本的脚本或校验文件失败 | 什么都没改。核对版本号是否存在；网络不通就过一会儿再试或换一个源（--china、--global） |
| INSTALL-GET-03 | P1 | 引导脚本下载到的脚本校验不过，没有执行 | 什么都没改、什么都没执行。换一个源再试；两个源都不过时停下反馈，不要绕过校验 |
| INSTALL-GET-04 | P3 | 引导脚本的用法不对 | 什么都没改。版本号写成 v0.6.0 这样；动作只有 install（默认）、upgrade、cli、uninstall |
| INSTALL-DOCKER-01 | P2 | 装 Docker 的软件源都不能用，发行版自带的 docker.io 也没装上 | Docker 没装上，AcornFox 还没装。稍后重试；或用 ACORNFOX_DOCKER_CE_MIRROR=<能用的 docker-ce 源> 指定一个；或先自己装好 Docker 再加 --skip-docker |
| UPGRADE-OLD-01 | P3 | 现在装的是 v0.3.x，不能直接升到这个版本 | 服务没停、什么都没改。先升到 v0.5.1（升级命令加 --version v0.5.1），再运行一次升级命令升到最新版 |
| UPGRADE-DOWN-01 | P3 | 升级时指定了比现在旧的版本 | 服务没停、什么都没改。AcornFox 只能升级：去掉 --version 重新运行升级命令；不要加 -y 或换旧版本号重试 |
| UPGRADE-ROLLBACK-01 | P3 | 用了 --rollback：平台不能回到旧版本 | 服务没停、什么都没改。升级没成功时脚本当时已自动恢复；应用自己的版本用 acornfox rollback 应用名 回退 |
| UPGRADE-BACKUP-01 | P2 | 停服务后备份没做完，没有升级 | 没有升级，原来的服务已重新启动。df -h /var/lib/acornfox 看空间，转述给用户；腾出空间后再升级 |
| UPGRADE-FAIL-01 | P2 | 新版本的数据库迁移失败，已自动恢复到升级前 | 没有升上，服务照常运行。告诉用户现在仍是原来的版本；先问用户要不要再试一次，再失败就停下，把输出反馈给维护者 |
| UPGRADE-FAIL-02 | P2 | 新版本的服务起不来，已自动恢复到升级前 | 没有升上，服务照常运行。先问用户要不要再试一次，再失败就停下，把输出和 journalctl -u acornfox-server -n 50 反馈给维护者 |
| UPGRADE-FAIL-03 | P2 | 升级中途出错或被中断（包括 SSH 断开），已自动恢复到升级前 | 没有升上，服务照常运行。可以再运行一次升级命令 |
| UPGRADE-RESTORE-01 | P1 | 升级失败后没能恢复到升级前（要人处理） | 停下。把输出原样给用户，请用户把 journalctl -u acornfox-server -n 50 和 -u acornfox-runner -n 50 反馈给维护者；不要删备份、不要重装、不要自己拷备份里的文件 |
| UNINSTALL-TTY-01 | P3 | 卸载脚本没有终端、没法询问确认，又没加 -y | 什么都没改。请用户在自己的终端里运行卸载命令；只有用户明确要你代为卸载时才加 -y |
| WIN-OS-01 | P2 | Windows 版本太旧 | 先升级 Windows（Windows 10 22H2 或 Windows 11）再重新运行安装命令 |
| WIN-ARCH-01 | P2 | 这台电脑的 CPU 架构暂时不支持 | ARM 版 Windows 暂时用不了；可以改把 AcornFox 装在一台 Linux 服务器上，用这台电脑通过 SSH 管理 |
| WIN-POLICY-01 | P2 | 公司或学校的策略禁止了 WSL | 请联系管理这台电脑的 IT 人员放开 WSL；自己改注册表可能违反规定，安装脚本不会替你改 |
| WIN-VIRT-01 | P2 | CPU 的虚拟化功能没打开 | 重启电脑进 BIOS/UEFI，打开 Intel VT-x（Intel Virtualization Technology）或 AMD-V（SVM Mode），保存后再运行安装命令 |
| WIN-DISK-01 | P2 | 系统盘剩余空间不够 | 清理系统盘，至少留 10 GB 再运行安装命令（Debian 加 Docker 加几个应用大约要 3～5 GB） |
| WIN-PS-01 | P2 | 缺少必要的系统组件 | 升级 Windows 10 到 22H2 后再运行；需要 PowerShell 5.1 和 System32 里的 curl.exe |
| WIN-SRC-01 | P3 | 这是源码里的脚本，没有绑定版本 | 用引导脚本 get.ps1 安装（它取要装的那个版本自带的 install-windows.ps1）；开发测试时加 -Version 或 -LocalPackage |
| WIN-ADMIN-01 | P3 | 管理员确认被取消，什么都没安装 | 想继续就重新运行安装命令；弹窗上写 Windows PowerShell、发布者 Microsoft Windows 时选“是”。已经下载的文件会留着，不用重下 |
| WIN-ADMIN-02 | P2 | 管理员那一步出错了 | 看 %LOCALAPPDATA%\AcornFox\logs\admin-step.log 里最后几行，再重新运行安装命令（做过的步骤会跳过） |
| WIN-REBOOT-01 | P3 | 需要重启电脑才能继续 | 保存手上的工作后重启电脑；重启后登录，会自动弹出窗口接着装（也可以自己再运行一次同样的安装命令） |
| WIN-DL-01 | P2 | 下载没成功（所有下载来源都试过） | 确认能上网、关掉代理后重新运行；已经下好的部分会保留，不会从头来 |
| WIN-DL-02 | P2 | 取不到这个版本的安装脚本，或校验不过 | 多半是下载被截断或被替换：换一个源再试（加 -China 或 -Global）；两个源都不过时不要强行安装，先反馈 |
| WIN-WSL-01 | P1 | WSL 用不了 | 运行 wsl --status 看原因；多半是还没重启，或虚拟机平台没打开，或 WSL 被升级到一半 |
| WIN-WSL-02 | P2 | 导入 Linux 发行版失败 | 看 install-windows.log 里 wsl --import 的输出；磁盘空间、杀毒软件拦截、文件被占用是常见原因；解决后重新运行安装命令 |
| WIN-WSL-03 | P1 | Linux 发行版里的 systemd 没有起来 | 运行 wsl --shutdown 后重新运行安装命令；还不行看 wsl -d AcornFox -u root -- systemctl status |
| WIN-LINUX-01 | P1 | 在 WSL 里安装 AcornFox 没成功 | 看 WSL 里的 /root/acornfox-install.log 和它最后打印的故障编号（INSTALL-…、EDGE-… 等，acornfox faults 编号 能查）；解决后重新运行安装命令 |
| WIN-START-01 | P2 | 开机自启没登记成功，或 WSL 没设成一直开着 | 重新运行安装命令（要管理员确认一次），或运行 acornfox windows lan-on / lan-off 重新登记；没有 .wslconfig 里的设置时，没人用 WSL 一会儿它就会停 |
| WIN-LAN-01 | P2 | 局域网转发没生效 | 运行 acornfox windows doctor 看是哪一项；360 等安全软件常拦 netsh，弹窗里选“允许操作”；还不行运行 acornfox windows lan-off 再 lan-on 重来 |
| WIN-LAN-02 | P3 | 当前账号不是管理员，局域网访问没打开 | 应用只在这台电脑上能打开。想让局域网里的设备访问：用管理员账号运行 acornfox windows lan-on |
| WIN-LAN-03 | P3 | 这台电脑的网络类型是“公用”，防火墙会挡住局域网里的设备 | 在 Windows 设置里把这个网络改成“专用”（家里或办公室这类可信的网络），再试；公共场所的网络（咖啡店、机场）不要改 |
| WIN-CLI-01 | P3 | acornfox 命令没能写进去 | 也可以直接用 wsl -d AcornFox -u root -- acornfox …；重新运行安装命令会补写；新开一个 PowerShell 窗口让 PATH 生效 |
| WIN-NET-01 | P1 | 控制台打不开（localhost 上没有应答） | 运行 acornfox windows doctor；多半是 WSL 里的服务没起来：wsl -d AcornFox -u root -- systemctl status acornfox-server |
| WIN-UNINST-01 | P3 | 卸载要确认，但这里没有终端可以问 | 确定要卸载就加 -Yes（acornfox windows uninstall --yes）；加 --purge 会连应用数据和发行版一起删，不可恢复，只在用户明确说要全部删掉时加 |
| WIN-UNINST-02 | P2 | WSL 里的 AcornFox 没卸干净，卸载停下了 | 先看 %LOCALAPPDATA%\AcornFox\logs\uninstall-wsl.log 最后几行和它报的编号（INSTALL-GET-…、UNINSTALL-…，acornfox faults 编号 能查）；确认这台电脑能上网后，用屏幕上给的完整路径命令重新运行卸载 |
| WIN-UNINST-03 | P2 | -Purge 没能注销 WSL 发行版，数据还在 | 运行 wsl --shutdown 关掉所有 WSL（会关掉用户别的发行版，先问用户），再用屏幕上给的完整路径命令重新运行卸载 |
| WIN-SKILL-01 | P3 | Windows 用户目录里已装的 AcornFox Skill 和 WSL 里的程序不是同一个版本 | 运行 acornfox skill install 更新（它把 WSL 里程序带的 Skill 装进 Windows 用户目录）；升级 AcornFox 之后都要做一次 |
| MAC-OS-01 | P2 | 这不是 Mac，或 macOS 版本太旧 | Mac 要 macOS 26 或更新才能用苹果的 container；先在“系统设置 → 通用 → 软件更新”升级再重新运行安装命令 |
| MAC-ARCH-01 | P2 | 这台 Mac 不是苹果芯片 | Intel 的 Mac 装不了（苹果的 container 只在苹果芯片上运行）；可以把 AcornFox 装在一台 Linux 服务器上，用这台 Mac 的命令行管理（get.sh cli，再 acornfox target add） |
| MAC-DISK-01 | P2 | 磁盘剩余空间不够 | 清理磁盘，至少留 12 GB 再运行安装命令（苹果的 container 约 0.5 GB、虚拟机镜像约 1 GB，Docker 和应用另算） |
| MAC-DL-01 | P2 | 下载安装需要的大文件失败 | 网络不好：过一会儿重新运行安装命令（已下的部分会接着下）；国内加 --china，海外加 --global 换一个先试的源 |
| MAC-DL-02 | P2 | 取不到这个版本的安装文件 | 核对版本号存在（https://gitee.com/VIP13390/AcornFox/releases）；网络不通时过一会儿再试，或加 --china、--global 换一个先试的源 |
| MAC-SRC-01 | P3 | 这是源码里的脚本或程序，没有版本号 | 用 get.sh 安装（它取带版本号的发行版脚本），或加 --version vX.Y.Z、--local-package 目录 |
| MAC-ADMIN-01 | P2 | 要管理员密码，但没输成 | 装苹果签名的 container 安装包要管理员权限：请你自己在“终端”里运行屏幕上给的那一行 sudo installer …（输入密码时屏幕不显示字符，是正常的），装好后重新运行安装命令 |
| MAC-CTR-01 | P1 | 苹果的 container 装了但用不了 | 运行 container system start 看报错；还不行就重新运行安装命令，或到 https://github.com/apple/container 看有没有已知问题 |
| MAC-KERNEL-01 | P2 | 虚拟机内核没装上 | 重新运行安装命令（会重新下载并装内核）；内核约 676 MB，经下载网关分段下载，慢的话请耐心等 |
| MAC-IMG-01 | P2 | 虚拟机用的镜像准备不好 | 重新运行安装命令（已下的根文件系统会直接用）；还不行带屏幕输出反馈 |
| MAC-VM-01 | P1 | 建不了虚拟机 | 看屏幕上的原因：同名虚拟机已存在就换名字（环境变量 ACORNFOX_MAC_VM）或先自己处理；其余重新运行安装命令 |
| MAC-VM-02 | P1 | 虚拟机起不来或没有应答 | acornfox mac stop 再 acornfox mac start；还不行 container system stop 再 container system start（会停掉你别的容器，先问用户） |
| MAC-PROV-01 | P2 | 虚拟机里的准备没做成 | 重新运行安装命令；软件源连不上时程序会自己换源再试 |
| MAC-LINUX-01 | P1 | 虚拟机里装 AcornFox 没成功 | 屏幕上写了虚拟机里脚本最后报的故障编号（INSTALL-…、EDGE-… 等）：acornfox faults 那个编号看办法；整个输出也记在虚拟机里的 /root/acornfox-install.log |
| MAC-LAUNCH-01 | P1 | 常驻转发程序没在运行 | 重新运行安装命令会装回去；手动：launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.acornfox.mac.plist；看日志 ~/Library/Application Support/AcornFox/mac/logs/daemon.log |
| MAC-PORT-01 | P2 | Mac 上的端口被别的程序占了 | 找出占用的程序（lsof -nP -iTCP:端口 -sTCP:LISTEN）退出它，或换一个 AcornFox 要用的端口；占着的端口转发程序不会转发 |
| MAC-NET-01 | P3 | 这台 Mac 的局域网地址变了 | acornfox mac set --public-host auto（把应用地址里写的地址改成现在的；虚拟机里的 AcornFox 服务会重启一下，应用不停） |
| MAC-SSH-01 | P2 | acornfox 命令连不上虚拟机 | acornfox mac doctor 看原因；多半是转发程序没运行（MAC-LAUNCH-01）或虚拟机刚起来还没好，等十几秒再试 |
| MAC-CLI-01 | P3 | 放不下 acornfox 命令 | 换一个目录：ACORNFOX_INSTALL_DIR=某个可写目录 重新运行安装命令；或自己把 ~/Library/Application Support/AcornFox/mac/bin/acornfox 拷到 PATH 里 |
| MAC-UNINST-01 | P3 | --purge 没有确认 | --purge 会连虚拟机里的应用和数据一起删掉，删了就找不回来；确认要删请加 --yes |
| MAC-UNINST-02 | P2 | 卸载没卸干净 | 看屏幕上哪一步失败：停不掉常驻程序用 launchctl bootout gui/$(id -u)/com.acornfox.mac；停不下虚拟机用 container machine stop acornfox；然后重新运行卸载命令 |
| SETUP-NET-01 | P2 | 下载源都连不上 | 确认能上网后点“重试”（或运行 acornfox setup --resume）；国内网络可以换下载源：acornfox setup --resume --mirror <地址> |
| SETUP-NET-02 | P2 | 下载 45 秒没有进展，所有下载源都试过了 | 已下载的部分会保留，点“重试”从断点接着下；一直不行就换下载源或关掉代理再试 |
| SETUP-SUM-01 | P2 | 下载的文件校验不通过 | 已删掉坏文件，点“重试”会重新下载；换一个下载源更稳妥。反复出现请把诊断信息发给我们 |
| SETUP-RT-01 | P2 | Docker 装了但没有运行，需要你选用哪个运行环境 | 在页面上选“用 AcornFox 自带的运行环境”或“打开 Docker Desktop”；终端：acornfox setup --runtime builtin |
| SETUP-RT-02 | P1 | 自带的运行环境没能启动 | 点“重试”；还不行运行 acornfox doctor 看具体哪一项，再把诊断信息发给我们 |
| SETUP-RT-03 | P1 | 连不上 Docker | Linux 服务器用 install.sh 安装会自动装好 Docker；已经装了就 sudo systemctl start docker 后重试 |
| SETUP-RT-04 | P2 | Docker Desktop 没有按时启动 | 看一眼 Docker Desktop 的窗口，处理完它的提示后点“重试”；或者改用 AcornFox 自带的运行环境 |
| SETUP-EDGE-01 | P1 | 网关组件没有准备好 | 点“重试”；端口被占时运行 acornfox doctor 看是哪个端口，关掉占用的程序后再试 |
| SETUP-CHK-01 | P2 | 自检没有通过 | 按没通过那一项的说明处理后点“重试” |
| SETUP-CHK-02 | P1 | Docker 不能创建和删除容器 | 清理 Docker 空间（docker system prune）或重启运行环境后重试 |
| SETUP-CHK-03 | P1 | 网关管理口不安全或不应答 | 运行 acornfox doctor 查看；关掉占用 18807 的程序后重试 |
| SETUP-CHK-04 | P3 | 局域网打不开 | 只在本机用可以忽略。要在局域网打开：Windows 上运行 acornfox setup --confirm-admin 加防火墙规则；网络是公用的话，到 设置 → 网络 把它改成“专用网络”；然后重试 |
| SETUP-CHK-05 | P3 | 局域网转发没有启动 | 运行 acornfox doctor 看是哪些端口被占；关掉占用的程序，过 10 秒再看 |
| SETUP-SVC-01 | P1 | 后台服务 acornfox-server 没有应答 | 运行 sudo systemctl restart acornfox-runner acornfox-server，再运行 sudo acornfox doctor；还不行就把 journalctl -u acornfox-server -n 50 的输出交给用户 |
| SETUP-DISK-01 | P2 | 磁盘剩余空间不够（少于 10 GB 警告，少于 3 GB 不通过） | doctor 会写出是数据目录还是 Docker 数据目录所在的分区。先运行 docker system df 看占用，确认不需要后 docker system prune 清理（会删掉停掉的容器和没用的镜像，先问用户）；还不够就在云厂商控制台扩容磁盘 |
| SETUP-PORT-01 | P2 | 端口 80 或 443 被别的程序占用 | 和宝塔、1Panel、nginx 等共存时（安装用了 --allow-busy-80-443）不用处理，doctor 只给警告：应用照常用 http://服务器IP:端口 访问，不要停用户在用的程序；要用域名就在原来的面板里反向代理到 127.0.0.1:应用端口。doctor 判不通过，说明 AcornFox 里绑定了域名或开了公网控制台：先问用户占用的程序还要不要。不要了才停掉它（例如 sudo systemctl disable --now nginx），再运行 sudo systemctl restart acornfox-server；要保留就删掉 AcornFox 里绑定的域名（acornfox domain remove 域名）或关掉公网控制台（acornfox public-console disable） |
| SETUP-PORT-02 | P1 | 网关管理口 18807 被别的程序占用 | 按 doctor 写出的 PID 确认是什么程序，停掉后运行 sudo systemctl restart acornfox-server；应用的网址和域名都依赖这个端口 |
| SETUP-PORT-03 | P2 | 应用端口段 18810–18899 里有端口被别的程序占用 | 应用的端口在创建时分好、以后不变，分到被占端口的应用会一直上线失败（删掉重建往往还会分到同一个端口）。按 doctor 写出的端口和 PID 找到占用的程序，先问用户能不能停掉；能停就停掉后重新部署，不能停就把情况告诉用户：这些端口上的应用上线不了 |
| SETUP-PORT-04 | P3 | 局域网控制台端口 18802 被别的程序占用 | 不开局域网控制台可以忽略；要开的话先停掉占用的程序再运行 acornfox lan-console enable |
| SETUP-PORT-05 | P3 | 没有权限查看是哪个程序占用了端口 | 普通用户运行时出现是正常的：后台服务正常时，AcornFox 自己的端口（18807、应用端口、开着的局域网控制台端口）已经认出，不在这里；剩下的要确认就用 sudo acornfox doctor 再查一次 |
| SETUP-CLOCK-01 | P2 | 本机时钟不准，或者没开网络对时 | 先问用户，再运行 sudo timedatectl set-ntp true 打开网络对时，过一两分钟用 timedatectl 看 System clock synchronized 是否为 yes，然后再运行 acornfox doctor；一直同步不上的云主机，按云厂商文档把 chrony 或 systemd-timesyncd 改成内网时间服务器。时钟和下载网关差 300 秒以上时，下载网关会拒绝这台机器（GW-AUTH-01）：应用中心的镜像改从公共源拉，私有目录连不上；AcornFox 会按网关时间自动重试一次，但还是要把时钟校准 |
| SETUP-MAC-01 | P3 | Mac 版的 install、serve 不能直接装本机服务 | Mac 版只有命令行客户端，acornfox install、serve 不能在 Mac 上直接装本机服务；要把 AcornFox 装在这台 Mac 上（苹果芯片、macOS 26 起），运行：curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh \| bash。1. 要装在这台 Mac 上：运行上面那行命令（装进苹果 container 开的一台 Debian 虚拟机），或先运行 acornfox mac install --plan 看会做什么；2. 要管理 Linux 服务器：在服务器上按使用文档用 install.sh 安装 AcornFox，再在这台 Mac 上运行 acornfox target add 名称 --ssh 用户@服务器地址；3. 之后 acornfox deploy、status、logs 等命令都作用在登记的服务器上。检查服务器用 ssh 用户@服务器地址 sudo acornfox doctor |
| SETUP-GEN-01 | P2 | 准备运行环境时出错 | 点“重试”；还不行运行 acornfox doctor，把诊断信息发给我们 |
| SETUP-SYNC-01 | P3 | 应用目录上次同步失败，或者很久没同步 | 运行 acornfox catalog sync 看这次的结果；同步不上不影响已装的应用，安装时用手上的目录（缓存或程序内置的） |
| SETUP-GW-01 | P3 | 下载网关连不上或应答不对 | 公共目录的应用照常能装（镜像改从公共源拉，会慢一些），私有目录连不上、装不了。先看网络和代理，再看 ACORNFOX_GATEWAY 或 network.json 里的 gateway 有没有写错 |
| SETUP-NET-03 | P3 | 有镜像下载源不能用或很慢（拉镜像时已跳过或排到后面），或 Docker 的镜像加速配置里有不能用的地址 | 只是提醒：平台拉应用镜像时会自动跳过不能用的源、把慢的排到后面。要看每个源现在怎样运行 acornfox sources --probe；daemon.json 里不能用的加速地址只影响自己 docker pull 和构建项目，要去掉先问用户 |
| SETUP-SKILL-01 | P3 | 电脑上已装的 AcornFox Skill 和命令行不是同一个版本 | 运行 acornfox skill install 更新 Skill；如果提示 Skill 比命令行新，先更新电脑上的命令行（get.sh 的 cli 那条命令，见 Skill 里 maintain.md 的“更新 Skill 和命令行”），再运行 acornfox skill install |
| EDGE-01 | P1 | 入口代理没有在运行 | 运行 sudo acornfox doctor 看入口代理一项；容器停了就 sudo systemctl restart acornfox-server（会重建或拉起入口容器，应用容器不停） |
| EDGE-02 | P1 | 入口镜像缺失或导入失败 | 升级时报的：服务没停、什么都没改，处理好网络或磁盘后重新运行升级命令。平时报的：重新运行一次升级命令（同一版本加 -y）就会补上入口镜像 |
| EDGE-03 | P1 | 入口代理的管理口连不上或拒绝了令牌 | 运行 sudo acornfox doctor 看入口代理一项；sudo systemctl restart acornfox-server（启动时发现入口容器不接受数据目录里的令牌，会按这个令牌在原地重建入口容器一次，应用网址断几秒） |
| EDGE-04 | P2 | 交给入口代理的设置没有生效 | 运行 sudo docker exec acornfox-edge /usr/bin/afx-edge health 看是哪一项；sudo systemctl restart acornfox-server 让后台服务整份重交一次 |
| EDGE-05 | P2 | 升级时换成新的入口容器没有成功，原来的入口照常服务 | 升级脚本已经自动恢复到升级前（应用照常能打开）。把脚本最后 30 行和 sudo journalctl -u acornfox-server -n 50 --no-pager 的输出交给维护者；处理好原因后可以再运行一次升级命令 |
| EDGE-06 | P1 | 升级没成功、恢复到升级前时没能换回原来的入口容器 | 先把“处理”里的手工步骤（删掉新入口容器、起原来的入口容器）告诉用户，用户同意后照做；做不了就停下，把输出交给维护者 |
| LAN-CONSOLE-01 | P3 | 这台服务器不能开局域网控制台 | 云主机、公网服务器进控制台：在自己电脑上运行 acornfox open（经 SSH 隧道）。确实在自家或办公室局域网里：等十几秒再运行 acornfox lan-console enable；还不行就看 acornfox doctor，把结果发给我们。不要为了开它去改安全组或防火墙 |
| LAN-CONSOLE-02 | P3 | 局域网控制台拒绝了不在局域网里的访问 | 这是保护，不用修。自己要从外面进控制台就用 acornfox open（经 SSH 隧道）。日志里反复出现陌生地址时，去路由器上把 18802 的端口映射删掉 |
| LAN-CONSOLE-03 | P2 | 局域网控制台开着但还没设密码 | 现在就用局域网里的电脑打开 acornfox lan-console status 给的地址设一个密码（至少 8 位），局域网里谁先打开谁设。不用局域网控制台就运行 acornfox lan-console disable。设好后 status 会显示是什么时间、从哪个地址设的；不是用户自己就运行 acornfox lan-console reset-password 重设 |
| lan_console_off | P3 | 局域网控制台没开，没有密码可清 | 不用重设密码：局域网控制台是关着的。用户要用它时先问用户，再 acornfox lan-console enable（对局域网开放 18802），然后请用户打开给出的地址设密码 |
| LAN-CONSOLE-04 | P3 | 忘了局域网控制台的密码 | 在装着 AcornFox 的服务器上（经 SSH）运行 acornfox lan-console reset-password：清掉密码，所有登录状态和记住的设备一起作废；然后在局域网里用浏览器打开控制台地址，重新设一个密码（谁先打开谁设，别拖） |
| ssh_missing | P2 | 本机找不到 ssh 命令 | 安装 OpenSSH 客户端（Windows：设置 → 可选功能） |
| host_unreachable | P2 | 连不上服务器 | 检查地址、端口、安全组是否放行 22 |
| auth_failed | P2 | SSH 认证失败 | 先在终端里 `ssh user@host` 确认能免密登录（`ssh-copy-id`） |
| host_key_unknown | P3 | 服务器主机指纹未确认 | 先手动 `ssh user@host` 一次确认主机指纹 |
| host_key_changed | P1 | 服务器主机指纹与本机记录不一致 | 若确认服务器重装过或 IP 换了机器，运行 `ssh-keygen -R 主机` 删除旧记录，再手动 `ssh user@host` 确认新指纹；否则可能是中间人攻击，不要继续 |
| acornfox_missing | P2 | 服务器上没装 AcornFox | 服务器上尚未安装 AcornFox |
| permission_denied | P2 | 无权访问 acornfox 服务 | 把该用户加入 `acornfox-users` 组后重新登录 |
| server_down | P1 | acornfox 服务未运行 | 在服务器上执行 `systemctl status acornfox-server` |
| local_server_down | P1 | 连不上本机的 acornfox 服务 | Linux 服务器上执行 `systemctl status acornfox-server` 和 `acornfox doctor` 检查；Mac、Windows 本机的 AcornFox 在虚拟机里：Mac 用 `acornfox mac status`、`acornfox mac doctor` 看，Windows 用 `acornfox windows status`；管理别的服务器则先 `acornfox target add 名字 --ssh 用户@地址` 登记 |
| version_mismatch | P3 | 命令行和服务器版本对不上 | 升级 CLI 或服务器 |
| forwarding_disabled | P2 | 服务器禁止了 SSH 端口转发 | 服务器 sshd 需要 `AllowTcpForwarding yes` |
| connect_failed | P2 | SSH 连接失败（其他原因） | 先在终端里手动 `ssh user@host` 排查 |
| server_too_old | P3 | 服务器的 AcornFox 太旧，不支持这个命令 | 升级服务器上的 AcornFox |
| dockerfile_missing | P3 | 项目里没有 Dockerfile | 在项目根目录添加 Dockerfile（写明 EXPOSE 端口）后重新部署 |
| too_large | P3 | 要上传的项目太大 | 检查 .dockerignore，排除依赖、构建产物和大文件 |
| context_too_large | P3 | 项目包解压后超过 1 GB | 在 .dockerignore 里排除依赖目录（如 node_modules）、构建产物、数据和大文件后重新部署 |
| upload_invalid | P3 | 上传的项目包坏了或读不了 | 使用 acornfox deploy 重新上传项目目录 |
| clone_failed | P3 | 拉取 Git 仓库失败 | 检查 Git 地址与网络后重试 |
| clone_timeout | P3 | 拉取 Git 仓库超时 | 国内服务器访问 GitHub 常超时，可改用 Gitee/CNB 镜像，或直接 acornfox deploy 上传本地目录 |
| git_missing | P3 | 服务器上没有 git | 请在服务器上安装 git 后重试 |
| ref_not_found | P3 | 指定的分支或提交不存在 | 检查 --ref 指定的分支名或提交哈希是否存在 |
| auth_required | P3 | 仓库要登录才能拉（私有仓库） | 把代码下载到本地后在项目目录执行 acornfox deploy 上传；私有仓库支持计划在首发之后提供 |
| repo_not_found | P3 | 仓库不存在 | 检查仓库地址是否正确，且仓库是公开的 |
| build_failed | P2 | 镜像构建失败 | 根据日志修改 Dockerfile 或依赖后重新部署 |
| registry_timeout | P3 | 构建时访问软件包源超时 | 构建时访问软件包源超时。服务器在中国大陆时，在 Dockerfile 中改用国内镜像源后重新部署：npm 加 --registry=https://registry.npmmirror.com；pip 加 -i https://mirrors.huaweicloud.com/repository/pypi/simple；Go 设 ENV GOPROXY=https://goproxy.cn,direct；Alpine 用 sed -i 's/dl-cdn.alpinelinux.org/mirrors.huaweicloud.com/g' /etc/apk/repositories |
| base_image_not_found | P3 | 构建时基础镜像拉取失败 | 基础镜像拉取失败：先检查 FROM 里的镜像名；日志里是网络错误或 502、503 时，多半是镜像加速地址暂时不可用，稍后直接重新部署即可，反复失败就在 /etc/docker/daemon.json 换用可用的加速地址（阿里云服务器用控制台“容器镜像服务 → 镜像加速器”里的专属地址） |
| dependency_missing | P3 | 构建缺少依赖 | 构建缺少依赖：把依赖写进依赖清单（如 requirements.txt / package.json）并在 Dockerfile 中安装 |
| runner_response_too_large | P3 | 构建输出过大 | 请减少构建时的输出后重新部署 |
| pull_failed | P2 | 拉取镜像失败 | 检查镜像引用或稍后重试 |
| pull_timeout | P2 | 拉取镜像超时 | 看 message 里每个镜像源各自怎么了（failure.sources 每个源一条）：都很慢就换个时间再装，或给 Docker 加一个自己的镜像加速地址；都连不上先查网络。acornfox sources --probe 看各个源现在多快 |
| image_not_found | P3 | 镜像不存在 | 检查镜像名与标签是否正确；若镜像确实存在，可能是加速源未收录，换用其他加速源或自行构建 |
| image_gone | P3 | 要回退的那一版程序已不在服务器上 | 请重新部署那一版的代码 |
| interrupted | P3 | 部署途中服务重启，这次部署已作废 | 请重新提交部署 |
| start_failed | P2 | 容器启动失败 | 看 log_excerpt 和 acornfox logs，修启动命令或缺失的环境变量 |
| out_of_memory | P2 | 因内存超限被杀掉 | acornfox app set --memory 1024 调大内存上限（立即生效）；部署时就内存不够的，改完再重新部署 |
| container_exited | P2 | 容器启动后退出 | 请检查启动命令与日志 |
| exited | P2 | 附加服务（数据库等）启动后退出或反复重启 | 查看数据库日志摘录；常见原因是数据卷里有不兼容的旧数据 |
| not_ready | P2 | 附加服务在时限内没有就绪 | 查看数据库日志摘录；首次初始化较慢时可直接重新部署 |
| port_not_listening | P2 | 程序没有在约定的端口上监听 | 程序要监听 0.0.0.0（不是 127.0.0.1），端口与 --port 一致 |
| unpersisted_database | P2 | 数据写在容器里，重建会丢（警告） | 先问用户：应用已经在用的先导出容器里的数据，再 acornfox volume add 数据目录、acornfox redeploy（换新容器，容器里的数据不会自动搬过去） |
| route_failed | P1 | 路由持续同步失败超过 5 分钟 | 入口代理没有接受路由；运行 acornfox doctor 查看入口代理的状态 |
| failed | P2 | 部署失败（服务器没给出更具体的原因） | acornfox diagnose 部署编号 看诊断；acornfox logs 应用 --tail 200 看运行日志；acornfox logs 应用 --version N --build 看那一版的构建日志 |
| superseded | P3 | 这次部署被更新的一次取代了 | 查看最新部署：acornfox status |
| cancelled | P3 | 等待被打断（部署在服务器上继续） | 按 hint 运行 acornfox --json status 部署编号 查结果，不要重新部署 |
| timeout | P3 | 等了 --timeout 还没到终态 | 用 acornfox status 部署编号 查看后续状态，或用 --timeout 延长等待；不要重新部署 |
| start_timeout | P2 | start 或 restart 后 60 秒没起来 | acornfox logs 应用名 看日志，找它起不来或反复退出的原因；acornfox status 应用名 看现在的状态 |
| no_deployment | P3 | 应用没有上线过的版本 | 先部署：acornfox deploy，或 acornfox redeploy 应用名；acornfox versions 应用名 看各次部署为什么没上线 |
| rollback_target_mismatch | P2 | 服务器回到的版本不是指定的那一版 | 先在服务器上升级 AcornFox；用 acornfox versions 查看现在的版本记录 |
| deploy_in_progress | P3 | 回退时有部署正在进行 | 确认要取消，就加上 --cancel-pending 再执行 |
| version_not_found | P3 | 没有这一版 | 用 acornfox versions 查看有哪些版本 |
| version_is_live | P3 | 指定的版本就是正在服务的版本 | 不用回退；acornfox versions 看别的版本 |
| version_same_as_live | P3 | 指定版本的程序和正在服务的相同 | 不用回退：那一版和现在用的是同一个程序 |
| version_not_rollbackable | P3 | 这一版不能回退 | 用 acornfox versions 查看可回退的版本 |
| version_image_gone | P3 | 那一版的程序已不在服务器上 | 用 acornfox versions 查看可回退的版本，或重新部署那一版的代码 |
| no_rollback_target | P3 | 没有可以回退的版本 | 用 acornfox versions 查看版本记录 |
| memory_cap_exceeded | P2 | 整机分配给应用的内存不够了 | 先 acornfox limits --json 看各应用占用，调小用不完的应用（app set --memory），或请用户确认后调大整机上限 acornfox limits --memory 26G --json |
| memory_below_usage | P3 | 内存上限不能低于应用当前实际用量 | 内存上限不能低于应用当前实际用量，换一个更大的值 |
| dns_unresolved | P3 | 本机解析不到这个域名 | 解析记录可能还没生效，请稍等几分钟再试。 |
| dns_mismatch | P3 | 域名解析指向了别的地址 | 把域名的 A 记录改成这台服务器的公网 IP |
| dns_error | P3 | 没有 DNS 服务器应答 | 检查服务器的 DNS 设置和网络后再试 |
| port_closed | P2 | 域名的 80 或 443 端口拒绝连接 | 检查 80、443 有没有被别的程序占、AcornFox 网关是否在运行（acornfox doctor） |
| port_unreachable | P2 | 连接域名的端口超时（多半是安全组） | 在云厂商控制台的安全组里放行 80 和 443（入方向） |
| connection_reset | P2 | 连接在握手中途被重置 | 多半是云厂商拦截（未备案）或中间设备：中国大陆服务器先确认域名已备案 |
| icp_blocked | P2 | 访问被云厂商拦截：域名未备案 | 中国大陆服务器的域名须完成 ICP 备案；备案前继续用 http://IP:端口 访问 |
| wrong_target | P2 | 域名能访问，但应答不是这台服务器的 | 检查解析和 CDN：域名要直接指到这台服务器 |
| cert_pending | P3 | HTTPS 证书还没签下来 | 确认域名 A 记录指向本服务器、80 与 443 端口已放行；中国大陆服务器的域名须已完成 ICP 备案。证书一般一两分钟内签下，稍等再看 |
| cert_failed | P2 | 等了很久还是没有证书 | 确认域名 A 记录指向本服务器、80 与 443 端口已放行；中国大陆服务器的域名须已完成 ICP 备案；处理后重新绑定 |
| http_error | P3 | 访问域名失败（其他原因） | 看 message 里的原因；稍后再试 |
| invalid_domain | P3 | 域名写得不对 | 填完整的域名，例如 panel.example.com：小写，至少有一个点，不要带 http://、端口或路径，不能是 IP |
| domain_in_use | P3 | 这个域名已经绑定给了一个应用 | 公网控制台换一个域名（例如 panel.加上原来的域名，要在域名服务商加解析）；要把域名从应用上解绑由用户决定 |
| public_ip_unknown | P2 | 查不到这台服务器的公网 IP | 确认服务器有公网 IP、能访问外网，稍后再运行同一条命令 |
| edge_unavailable | P2 | 网关没准备好，设不了域名验证 | sudo acornfox doctor 看网关和端口两项，按里面的编号处理后再运行同一条命令 |
| verify_required | P3 | 还没完成域名验证就要开通公网控制台 | 重新运行 acornfox public-console enable 域名 --json：它先验证域名，通过后才开通 |
| PUBCON-01 | P3 | 在控制台页面开关公网控制台时，再输一次的控制台密码没输或不对 | 输这次登录用的控制台密码再试；忘了局域网控制台密码就在服务器上运行 acornfox lan-console reset-password 后重设，或者在服务器上用命令 acornfox public-console 开关 |
| PUBCON-02 | P3 | 控制台密码试错次数太多，暂时不让再试 | 按 message 里的分钟数等一会儿再试；等的时候不要反复点，每次失败都会让等的时间变长 |
| PUBCON-03 | P3 | 这台机器在局域网里，开不了公网控制台 | 在局域网里用局域网控制台（http://局域网地址:18802）；人在外面时在自己电脑上运行 acornfox open（经 SSH 隧道） |
| PUBCON-04 | P3 | 从公网控制台登录时，页面上只能关闭公网控制台 | 要换地址，从局域网控制台或用 acornfox open 进控制台再开；或者在服务器上运行 acornfox public-console enable 新域名 |
| PUBCON-05 | P3 | 忘了公网控制台的密码 | 在服务器上（经 SSH）运行 acornfox public-console enable 公网控制台的域名 --reset-password：生成新密码，只显示这一次，旧密码和旧的登录都立刻失效 |
| PDOMAIN-01 | P2 | 平台域名没有指到这台机器 | 到你买域名的地方加（或改）一条记录：名字 *.子域、类型 A、指向这台服务器的公网地址（acornfox domain base 给出完整的一条），等几分钟再检查 |
| PDOMAIN-02 | P3 | 这台机器没有公网地址，用不了平台域名 | 要在外面打开应用，用 acornfox open 应用名 经安全通道打开；要用域名，把应用装到有公网地址的云主机上 |
| PDOMAIN-03 | P2 | 外面连不上这台机器的 80 或 443 | 在云厂商控制台的安全组里放行入方向 TCP 80 和 443（来源 0.0.0.0/0）；服务器自己的防火墙开着的也要放行 |
| PDOMAIN-04 | P2 | 平台域名下的证书没签下来 | 中国大陆的服务器先确认域名已备案；再看入口代理的日志（sudo docker logs --tail 50 acornfox-edge） |
| PDOMAIN-05 | P3 | 平台域名的写法不对 | 只填域名本身，例如 apps.example.com：不带 http://、不带路径和端口，不能是 IP 地址 |
| PDOMAIN-06 | P3 | 某个应用用不了平台给的名字 | 给这个应用单独绑一个自己的域名（acornfox domain add 域名），或者让占着这个名字的一方换一个 |
| PDOMAIN-07 | P3 | 换掉或清掉平台域名要先确认 | 先运行 acornfox domain base set 新域名 --plan（或 clear --plan）把会怎样告诉用户；用户同意后加 --yes 再运行 |
| BKP-01 | P2 | 磁盘空间不够，不能做备份或恢复 | 先清理磁盘（删掉不用的旧备份：acornfox backup rm 应用名 备份编号 --yes），或把备份目录所在的磁盘扩一扩，再重试；没有动过应用 |
| BKP-02 | P2 | 读数据卷或写备份文件时出错 | 看 acornfox logs 和备份目录的 operations.log；应用已经照原样重新启动，重试一次，还不行就把日志给负责的人 |
| BKP-03 | P3 | 这个应用正忙：在备份、恢复或部署 | 等这件事做完再试（acornfox backup task 任务编号 看进度） |
| BKP-04 | P3 | 找不到这份备份 | acornfox backup list 应用名 看现有的备份和编号 |
| BKP-05 | P2 | 备份文件不完整或被改过，不能用来恢复 | 这份备份不能用来恢复，现在的数据没动；换一份备份，或重新做一份 |
| BKP-06 | P3 | 恢复和删除备份都要用户先同意 | 恢复会用备份里的数据覆盖现在的数据：先告诉用户，用户同意后加 --yes 再运行（先 --plan 看会怎样） |
| BKP-07 | P2 | 恢复没成功，数据已经自动退回到恢复之前 | 数据已经退回到恢复之前的样子；看任务里写的失败原因，处理后再试 |
| BKP-08 | P1 | 恢复没成功，自动退回也没成功 | 不要再动这个应用；恢复之前的备份还在（编号见任务结果的 pre_restore_id），马上告诉负责的人 |
| BKP-09 | P2 | 备份时那个版本的程序已经不在这台机器上 | 备份时的程序版本已不在本机；要在现在的版本上用这份数据，加 --allow-current-version（先问用户） |
| BKP-10 | P3 | 备份或恢复被中断，留下半截东西 | 运行 acornfox doctor 看有没有半截的备份；重新做一份备份，残留的会自己清掉 |
| BKP-11 | P3 | 这台机器上的执行器还不支持备份 | 升级 AcornFox 到最新版（acornfox 的执行器要和主程序同版本） |
| BKP-12 | P3 | 这个应用没有装在这台机器上 | 恢复要先有这个应用：先重新安装一个同名应用（acornfox install），再恢复 |
| BKP-13 | P3 | 这个应用没有数据可备份 | 这个应用没有存数据的目录（数据卷），没有东西可备份；先让应用跑起来、产生数据，或确认它的数据目录登记过（acornfox volume add） |
| BKP-14 | P2 | 备份目录写不了 | 检查备份目录的权限和所在磁盘（acornfox doctor 会指出是哪个目录） |
| BKP-15 | P3 | 恢复之后，有些服务连接和账号要重新接 | 照提示的应用逐个重新接上服务（acornfox services 看，装应用时会重新发账号） |
| BKP-16 | P3 | 备份的请求写得不对 | 按提示改请求后重试 |
| OPEN-APP-01 | P3 | 应用还没有访问端口 | 运行 acornfox status 应用名 看状态；部署成功后再打开 |
| OPEN-APP-02 | P3 | 经网址登记的服务器上，只本机的应用打不开 | 用 acornfox target add 名称 --ssh 用户@主机 登记这台服务器后再运行 acornfox open 应用名；或经用户同意改成局域网：acornfox app set --app 应用名 --expose lan |
| OPEN-APP-03 | P3 | 应用只在内部网络，没有网页可以打开 | 不用打开它：它是给别的应用用的（例如数据库、缓存），别的应用按它的名字在内部网络里连它；要用它的应用看 acornfox status 那个应用名 里的服务 |
| install_failed | P3 | Skill 一个也没装上 | 不加 --json 再运行一次 acornfox skill install，看每个工作台后面写的原因；也可以 acornfox skill install --dir 一个能写的 skills 目录，或 acornfox skill print 把内容交给工作台 |
| CLI-UPDATE-01 | P3 | 服务器上的 AcornFox 比这台电脑上的命令行新 | 在这台电脑上更新命令行：国内 curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh \| bash -s -- cli，海外把地址换成 https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh；再运行 acornfox skill install 更新 AI 工作台里的 Skill |
| CAT-INST-01 | P3 | 目录里没有这个应用、配方或版本 | acornfox catalog list 看有哪些；目录太旧就 acornfox catalog sync |
| CAT-INST-02 | P3 | 安装参数不对 | 按 message 改 --set；acornfox catalog info <应用> 看参数。必填项要用户提供内容的（例如要用的模型名），先问用户 |
| CAT-INST-03 | P3 | 已经有同名的应用 | 用 --name 换个名字；要重装先由用户确认后 acornfox delete 旧的 |
| CAT-INST-04 | P3 | 同一个应用正在安装 | 不要重复点安装；acornfox install status <task_id> 看进度 |
| CAT-INST-05 | P2 | 整机分配给应用的内存不够 | 问用户：调小别的应用（acornfox app set --memory）或调大整机上限（acornfox limits --memory） |
| CAT-INST-06 | P3 | CPU 架构不在应用支持的列表里 | 换一台机器，或等应用出这个架构的版本 |
| CAT-INST-07 | P2 | 访问端口用完了 | 删掉不用的应用后重试 |
| CAT-INST-08 | P3 | AcornFox 版本比应用要求的旧 | 先升级 AcornFox 再装（错误里的 needs_platform 是要升到的版本） |
| CAT-INST-09 | P3 | 已不再使用（编号保留） | 这一版不会再出现这个编号。遇到了说明看的是旧版本的输出：按 acornfox faults CAT-INST-31 等新编号处理 |
| CAT-INST-10 | P3 | 已不再使用（编号保留） | 这一版不会再出现这个编号。遇到了说明看的是旧版本的输出：按 acornfox faults CAT-INST-31 等新编号处理 |
| CAT-INST-11 | P3 | 已不再使用（编号保留） | 这一版不会再出现这个编号。遇到了说明看的是旧版本的输出：按 acornfox faults CAT-INST-31 等新编号处理 |
| CAT-INST-12 | P2 | 镜像从所有来源都没拿到 | 看 attempts；国内网络确认已配镜像加速；稍后重试 |
| CAT-INST-13 | P2 | 网关给的镜像包和目录里签名的校验值对不上 | 公共目录的应用自动换公共源，都不对就等目录更新；私有目录的应用直接停下，不去公共源找，等目录更新或联系给码的人。不要手动绕过 |
| CAT-INST-14 | P2 | 往数据卷里写配置文件失败 | 看 message；检查磁盘空间和 Docker 状态（acornfox doctor） |
| CAT-INST-15 | P2 | 容器没能启动或上线 | 看 diagnosis；acornfox logs <应用> 看日志 |
| CAT-INST-16 | P2 | 健康检查在时限内没通过 | acornfox logs <应用> 看日志；内存太小时调大后重装 |
| CAT-INST-17 | P2 | 装好后的初始化调用失败 | 看 message（命令失败的写了退出码和标准错误的最后几行）；acornfox logs <应用>；这次的应用已清理，可以直接重装 |
| CAT-INST-18 | P3 | 安装途中服务重启了，这次安装已作废并清理 | 重新安装 |
| CAT-INST-19 | P2 | 目录或应用包读不了、校验不过 | acornfox catalog sync 后重试；都不行就重装 AcornFox |
| CAT-INST-20 | P2 | 内部错误：运行器版本旧、数据库或 Docker 出错 | acornfox doctor；升级 AcornFox 后重试 |
| CAT-INST-21 | P3 | 没有这个安装任务 | 检查任务编号 |
| CAT-INST-22 | P3 | 这个应用不是从应用中心装的 | — |
| CAT-INST-23 | P3 | 清单用到了这个版本还不支持的写法 | 升级 AcornFox |
| CAT-INST-24 | P2 | 还留着以前同名应用的数据卷 | 停下问用户：旧数据还要不要。要接着用就加 --reuse-data 重装（页面上是“接上旧数据重装”；删除时保留过参数的，账号密码、你填过的设置项、改过的平台变量、接的服务和配置文件都按以前的，这次写了的以这次为准）；不要旧数据就用 --name 换个名字装，或者由用户执行 message 里卷名对应的 docker volume rm 后再装 |
| CAT-INST-25 | P3 | 应用或配方来自已断开的私有目录 | 告诉用户联系给连接码的人；拿到新连接码后 acornfox catalog connect 新码，再装 |
| CAT-INST-26 | P2 | 私有目录的文件没从下载网关取到 | 看 gateway_code：GW-CAT-* 先 acornfox catalog sync 看目录是否已断开，还连着就请给码的人检查目录；GW-AUTH-01 先校准本机时间（acornfox doctor 看 SETUP-CLOCK-01）；GW-NET-01 或下载中断就检查网络后重装；网关被关掉时问用户是否打开；message 说清单没写镜像包、或没有这台机器架构（例如 linux/arm64）的镜像包时，告诉用户联系给码的人补上 |
| CAT-INST-27 | P2 | Docker 存镜像的分区剩余空间不够 | 问用户：删掉不用的应用或镜像（acornfox delete、docker image prune 由用户确认后执行），或扩容磁盘；acornfox doctor 看各分区剩余空间 |
| CAT-INST-28 | P3 | 改平台变量不对 | acornfox catalog platform-vars 看有哪些、哪些能改；按 message 改 --set platform.名字=值 |
| CAT-INST-29 | P3 | 应用包里的清单有这个版本不认识的写法 | 错误里带 needs_platform 的：先升级 AcornFox 到那个版本再装。不带的：是这份目录有问题，不是操作不对，过一会儿 acornfox catalog sync 后再试，还不行就把 message 反馈给我们；不要想办法绕过 |
| CAT-INST-30 | P3 | 安装的某一步比平常慢很多（还没失败） | 先运行 next_command（就是 timing.step.look：下载镜像等步骤是 acornfox doctor --json，启动、健康检查、初始化是 acornfox logs 应用 --tail 200 --json）看原因，只读、不用问用户；把 say 和看到的要点告诉用户，问用户接着等还是不装了；不要自己删除、重装或重启服务 |
| CAT-INST-31 | P3 | 必须用到的服务这台机器上没有 | 你手上已经有这种服务的话（地址、Key 等），照 message 里的命令登记一条（acornfox services add 类型 --name 名字 --set 项=值，保密的项写成 项=-，从标准输入给值；命令里的 … 要换成用户给的真实值，先问到再给命令）；登记好以后先运行 acornfox services test 名字 试一下通不通，通了再看一次计划（acornfox install 应用 --plan），能装了再装；这台机器上有、但现在不能用的（提供它的应用停着、还差必填项），--plan 的 how 里写了怎么让它能用 |
| CAT-INST-32 | P3 | 同一类服务有好几条，要选一条 | 从 message 里列的几条服务里选一条用：安装时加 --use 需要名=服务名 选一条，页面上在“需要的服务”里选 |
| CAT-INST-33 | P3 | 指定的服务不对 | acornfox services list 看有哪些服务和它们的状态；acornfox install 应用 --plan 看应用有哪些需要；改对 --use 再装。提供这条服务的应用停着的，先启动它（acornfox start 应用名） |
| CAT-INST-34 | P2 | 提供服务的应用没能发凭据 | 看 message 是哪个提供方、哪一步（命令失败的写了退出码和标准错误的最后几行）；acornfox status 提供方 看它是否在运行，acornfox logs 提供方 --tail 200 看日志；说限制了请求次数的，过一会儿再装；提供方的管理员密码在应用里改过的话，平台登不进去：请在提供方应用里手工建一个凭据，登记成服务（acornfox services add …）后用 --use 指定 |
| CAT-INST-35 | P3 | 收回凭据没成功（提醒） | 到提醒里写的那个应用里，按写的名字或编号看一下那一项，用不上就手工删掉；那个应用停着的，启动后再删 |
| CAT-INST-36 | P3 | 清单是旧格式的，这一版装不了 | 这个应用要等按新格式重新上架后才能装，现在装不了，不是你的操作不对。已经装着的旧应用照常用；要用需要的服务、自动接上这些新功能，得等新清单上架后删了重装 |
| CAT-INST-37 | P2 | 原来的凭据核对没过，按清单停下了 | 先查提供方：acornfox status 提供方 看它是否在运行，acornfox logs 提供方 --tail 200 看日志；提供方好了再装一次（还是 --reuse-data）。确认原来的数据还在不在（例如用户在提供方里删过库、改过密码就会这样）。只有用户确定不要原来的数据时，才改接别的服务：加 --use 需要名=服务名 再装；不要自己删库、改密码 |
| CAT-INST-38 | P3 | 要接的服务被占着或已经接满 | acornfox services list 看每条服务被谁独占、还能接几个；acornfox install 应用 --plan 看现在能接哪条。有别的同类服务就用 --use 需要名=服务名 指定；没有就登记一条你已有的（acornfox services add …）。占着位置的是删了但数据还留着的应用、用户也确认它不要了：它已经删了，不能直接 delete --volumes，也不要手工 docker volume rm（接入记录还在，位置照样占着）；问过用户后先 acornfox install --name 原来的名字 --reuse-data 装回来，再 acornfox delete 原来的名字 --volumes 连数据一起删，位置才让出来。不要为了腾位置去删别的应用，要删先问用户 |
| CAT-INST-39 | P3 | 清单里的一个值算不出来 | 看 message 是哪一处的哪个 {{…}}（message 里不带取到的值）。是 map 没对上、条件同时成立这类、要靠换一个值解决的（例如换一种对外方式、--set 改设置项；acornfox catalog platform-vars 看本机平台变量的值），问用户后再装；写法读不了、启动命令带保密值的，是目录的问题，把 message 反馈给我们，不要想办法绕过 |
| CAT-INST-40 | P2 | 镜像包经下载网关分段下载没成 | 看 message 是哪一种：写缓存失败先看磁盘空间和权限（acornfox doctor 的 SETUP-DISK-01）；某一段多次下不下来，多半是网络或网盘暂时不通，稍后重装会从缓存接着下，已下的不用重下；缓存在数据目录 uploads/dl-cache，3 天没动会自己清，磁盘紧张时可以由用户确认后删；要退回单连接下载，环境变量 ACORNFOX_DL_SEGMENTED=0 |
| APP-VARS-01 | P3 | 应用装时算的平台变量和现在的不一样 | apps vars 里 value 是装时的值，value_now 是现在算出来的值。不影响用就先不动；要让应用用上现在的值：先 acornfox apps reapply 应用 --plan 看会改什么、会不会重启它，经用户同意后 acornfox apps reapply 应用 --yes（会按现在的值重写它的环境变量和配置文件并重启，数据不动，失败自动退回）；一次看有哪些应用落后了：acornfox apps affected |
| APP-APPLY-01 | P3 | 这个应用现在不能重新生效 | 看 message 是哪一种：正在装或正在重新生效的，用 acornfox install status 任务编号 --wait 等它结束再来；停着的先问用户，同意后 acornfox start 应用，再重新生效；清单没存或是旧格式的，这个应用只能删了重装（保留数据 --reuse-data，要先问用户） |
| APP-APPLY-02 | P3 | 这一项设置装好以后不能改 | 按 message 说的做：不能改的，不要想办法绕过（不要手工 env set）；写了“重装才能改”的，问用户同意后保留数据删掉重装（acornfox delete 应用，再 acornfox install 应用 --reuse-data）；值不对的，改对再试。acornfox apps vars 应用 看每一项能不能改 |
| APP-APPLY-03 | P2 | 重新生效没成功，已经退回原来的设置 | 把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；多半是用户改的值让应用起不来（例如端口、地址写错），改对了再试一次；先用 acornfox apps set … --plan 看会改什么。应用已经退回，不用再处理 |
| APP-APPLY-04 | P1 | 重新生效没成功，退回原来的设置时也出了错 | 这是要人处理的：先把 message 告诉用户，不要再重复运行重新生效，不要改密钥或删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；acornfox apps vars 应用 看环境变量是新的还是旧的；用户同意后可以 acornfox restart 应用，或 acornfox apps reapply 应用 --force 再按保存的设置生效一次 |
| APP-APPLY-05 | P3 | 这次改动会重启应用，要用户先同意 | 把 message 里写的会改什么、会重启哪个应用告诉用户，问他同意后在同一条命令上加 --yes（接口里是 confirm_restart: true）再运行一次；用户不想重启，可以加 --no-reapply（接口里 reapply: skip）只改访问方式，应用里的地址以后用 acornfox apps reapply 再对 |
| APP-UPG-01 | P3 | 这个应用现在不能升级 | 看 message 是哪一种：没有新版本就是已经最新，不用处理；正在装、重新生效或升级的，acornfox install status 任务编号 --wait 等它结束再来；停着的先问用户，同意后 acornfox start 应用再升级；清单没存或是旧格式的，这个应用只能删了重装（保留数据 --reuse-data，要先问用户） |
| APP-UPG-02 | P3 | 新版本要用户先补一样东西 | 按 message 里写的补：缺的设置问到用户给的值，升级时加 --set 设置名=值（密码类写 设置名=-，从标准输入给）；内存不够就问用户，同意后 acornfox app set --app 应用 --memory 数字，再升级。补好以后先 acornfox apps upgrade 应用 --plan 看一遍 |
| APP-UPG-03 | P3 | 新版本这一版不能原地升级 | 把 message 里写的告诉用户：现在这个应用照常用，只是不能升到这个版本。要用新版本，用户同意后保留数据删掉重装（acornfox delete 应用，再 acornfox install 应用 --reuse-data；多了必须的服务的，先登记一条已有的：acornfox services add …）；不同意就先不升，不要想办法绕过 |
| APP-UPG-04 | P3 | 升级会重启应用，要用户先同意 | 把 acornfox apps upgrade 应用 --plan 说的会做什么、要停多久告诉用户，问他同意后在同一条命令上加 --yes（接口里是 confirm_restart: true）再运行一次 |
| APP-UPG-05 | P2 | 升级没成功，已经退回到升级前 | 把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；原因是磁盘不够、网络不通这类的，处理好了再升级一次；应用已经退回，不用再处理，也不要重复升级 |
| APP-UPG-06 | P1 | 升级没成功，退回到升级前时也出了错 | 这是要人处理的：先把 message 告诉用户，不要再重复升级，不要删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；用户同意后用升级前的快照恢复：acornfox backup list 应用 找到类型是升级前的那份，再 acornfox backup restore 应用 备份编号 --yes（先 --plan 看会怎样） |
| APP-UPG-07 | P3 | 没有可以退回的东西 | 看 message 是哪一种：没升级过或已经退回过，就没有可退的，不用处理；快照坏了或程序没了，告诉用户这条路走不通，别的备份用 acornfox backup list 应用 看有没有能用的 |
| APP-UPG-08 | P3 | 退回会丢掉升级后的新数据，要用户先同意 | 把 acornfox apps rollback 应用 --plan 说的告诉用户，特别是“升级后的新数据会没有”，问他同意后加 --yes（接口里是 confirm: true）再运行一次 |
| APP-UPG-09 | P2 | 退回没成功，应用保持退回前的样子 | 把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；处理好了可以再退回一次 |
| APP-UPG-10 | P1 | 退回没成功，回到退回前的样子时也出了错 | 这是要人处理的：先把 message 告诉用户，不要再重复退回，不要删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；acornfox backup list 应用 里有退回前另存的那份（类型是恢复前），问用户同意后用它恢复 |
| CAT-CONN-01 | P3 | 连接码格式不对 | 让用户照原样再抄一遍；0、1、8 会当成 O、I、B |
| CAT-CONN-02 | P3 | 这台服务器关掉了下载网关 | 问用户是否打开下载网关（去掉 ACORNFOX_GATEWAY=off 或 network.json 里的 "gateway": "off"） |
| CAT-CONN-03 | P3 | 连接码不存在、抄错了，或已被别的机器用过 | 让用户核对连接码；还不行就找给码的人要新码。不要反复试 |
| CAT-CONN-04 | P3 | 连接码已过期 | 找给码的人要新码 |
| CAT-CONN-05 | P3 | 这个私有目录已停用 | 告诉用户联系给码的人 |
| CAT-CONN-06 | P3 | 这一小时试连接码的次数用完了 | 等 retry_after 秒后再试，不要循环重试 |
| CAT-CONN-07 | P2 | 连不上下载网关、网关拒绝，或本机钥匙读不出 | GW-AUTH-01 先校准本机时间；GW-NET-01 看网络和代理；ID-KEY 看 acornfox identity show；稍后再试 |
| CAT-CONN-08 | P2 | 网关已经授权，但本机没记下来 | 检查数据目录权限和磁盘空间后，用同一个连接码再连一次（同一台机器重复用不算新的一次） |
| CAT-SVC-01 | P3 | 目录里没有这种服务类型 | acornfox services kinds 看有哪些类型；目录太旧就 acornfox catalog sync 后再试 |
| CAT-SVC-02 | P3 | 服务的名字、项或值不对 | 按 message 改；acornfox services kinds 看这种服务有哪些项、哪些必填、能填什么 |
| CAT-SVC-03 | P3 | 已经有同名的服务 | 换个名字（--name）；要改已有的那条用 acornfox services set |
| CAT-SVC-04 | P3 | 还有应用在用这条服务，不能删 | 错误里列着是哪些应用在用它；要删这条服务，得先删掉这些应用 |
| CAT-SVC-05 | P3 | 这条服务是应用提供的，不能这样改或删 | 要填的项用 acornfox services set 名字 项=值；要它消失就删掉提供它的应用 |
| CAT-SVC-06 | P3 | 试连没通 | 看 detail 写的原因：拒绝连接、超时、找不到地址查地址和端口；回答的状态码不对查 Key 或账号；改值用 acornfox services set |
| CAT-SVC-07 | P3 | 地址写的是本机回环地址 | 服务在这台机器上：改填这台机器在局域网里的地址；服务是 AcornFox 装的应用：不用登记，装好后它会自己出现在服务列表里 |
| CAT-SVC-08 | P3 | 应用用的服务现在不可用 | 按提醒里写的做：提供它的应用停着，问用户后 acornfox start 它；还差哪几项，acornfox services set 服务名 项=值（密码、Key 写成 项=-，让用户自己在终端输入）；提供它的应用已删，问用户是接上旧数据装回来，还是让这些应用改接别的服务 |
| CAT-SVC-09 | P3 | 服务还不能用（就绪检查没通过） | 把提醒里清单写的那句话原样告诉用户，照它说的做（多半要用户在提供它的应用里做一步）；acornfox services show 服务名 看检查哪一步没过（ready_detail）。做完再 acornfox services show 服务名，状态是“可用”就好了 |
| CAT-SYNC-01 | P3 | 程序里没有受信公钥，不同步目录 | 不用处理：只用程序内置的目录；升级到带公钥的版本后会自动联网同步 |
| CAT-SYNC-02 | P3 | 目录来源连不上、超时或返回了非 200 | 检查服务器能不能访问 acornfox.com 和 gitee.com；稍后再 acornfox catalog sync |
| CAT-SYNC-03 | P3 | 目录来源返回的内容读不懂 | 多半是地址指错或中间有代理改了内容；稍后再同步，还不行就反馈 |
| CAT-SYNC-04 | P2 | 目录的签名验不过 | 没有用它，继续用手上的目录。持续出现就停下反馈：来源上的目录可能被篡改 |
| CAT-SYNC-05 | P3 | version.txt 和签名里的序号不一致（来源正在发布） | 等几分钟再 acornfox catalog sync |
| CAT-SYNC-06 | P3 | 来源上的目录不比手上的新（旧镜像或重放） | 不用处理：手上的已经更新；镜像来源过一阵会跟上 |
| CAT-SYNC-07 | P2 | 本机的目录缓存写不进去 | 检查数据目录所在磁盘和权限（acornfox doctor），处理后再 acornfox catalog sync |
| CAT-SYNC-08 | P3 | 所有目录来源都失败，继续用手上的目录 | 看 attempts 里每个来源的编号逐个处理；不影响已装的应用，安装时用手上的目录 |
| CAT-SYNC-09 | P3 | 另一次同步占着目录缓存 | 等一会儿再同步；不要并行跑多个 catalog sync |
| CAT-SYNC-10 | P3 | 向下载网关取私有目录失败 | 手上的私有目录照旧能用；看 doctor 的下载网关和时钟两项，稍后再同步 |
| CAT-SYNC-11 | P3 | 私有目录已断开（授权被撤销或目录停用） | 已装好的应用照常运行，但不能再装、不能升级；告诉用户联系给码的人 |
| CAT-SYNC-12 | P2 | 签名的索引写明属于别的目录 | 没有用它。公共同步时自动换下一个来源；私有目录持续出现就停下反馈 |
| CAT-LOAD-01 | P1 | 目录缓存和程序内置的目录都用不了 | 应用中心打不开。先 acornfox catalog sync；还不行就重装同一版本的 AcornFox（保留数据） |
| CAT-SHOW-01 | P3 | 展示内容（show.json）没有签名，或签名验不过 | 不用处理：应用中心退回用清单里的名字和一句话，装应用不受影响；下一次发布会带新的。持续出现就告诉维护者 |
| CAT-SHOW-02 | P3 | 展示内容的序号和目录的序号对不上 | 不用处理：这一份展示内容没有用，应用中心退回用清单里的名字和一句话；下一次发布会对上 |
| CAT-SHOW-03 | P3 | 展示内容的格式不对 | 不用处理：这一份展示内容没有用，应用中心退回用清单里的名字和一句话；告诉维护者用 acornfox catalog verify 查发布目录 |
| CAT-SHOW-04 | P3 | 取展示内容（show.json）失败 | 不用处理：目录已经同步成功，只是展示内容没取到；下次同步会再取一次 |
| CAT-SKILL-01 | P3 | 应用或解决方案的操作手册没取到 | 不用处理：这份手册暂时没有，应用照常装，AI 工作台照通用 Skill 做；下次同步目录会再取一次 |
| CAT-SKILL-02 | P3 | 操作手册的大小或 sha256 和展示内容里写的对不上 | 不用处理：这份手册已丢弃、没有用，应用照常装，AI 工作台照通用 Skill 做；下次同步会再取；持续出现告诉维护者 |
| CAT-SKILL-03 | P3 | 操作手册超过 32 KB 上限 | 不用处理：这份手册没有用，应用照常装；告诉维护者把它缩短到 32 KB 以内 |
| CAT-PKG-01 | P2 | 没有来源给出和目录相符的应用包 | 稍后 acornfox catalog sync 后重装；还不行就反馈 |
| CAT-PKG-02 | P2 | 下载的应用包校验值或大小和目录不符 | 没有用它；重装一次，持续出现就停下反馈（可能被篡改） |
| CAT-PKG-03 | P2 | 下载的应用包存不下来 | 检查数据目录所在磁盘和权限（acornfox doctor）后重装 |
| CAT-PKG-04 | P2 | 索引里的应用包路径不合规 | 目录本身有问题，不是操作不对；过一阵 acornfox catalog sync 后重试，还不行就反馈 |
| CAT-LINT-01 | P3 | 清单文件缺失、读不了或不是合法的 YAML | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-02 | P3 | 清单不符合格式定义 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-03 | P3 | 清单的 id 和所在目录名不一致 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-04 | P3 | 清单写的图标文件不存在 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-05 | P3 | GPL/AGPL 的应用没写源码地址 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-06 | P3 | 引用了不存在的参数、数据卷、端点或依赖 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-07 | P3 | 隐藏参数没有 generate，没人能填 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-08 | P3 | 主端点不对：没有唯一一个带健康检查的主端点，或服务的步骤指向了别的入口 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-09 | P3 | 配方引用的应用或连接不存在或对不上 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-10 | P3 | 默认对外方式不是只本机（要审核） | 这是警告：确认这个应用默认对局域网或公网开放是有意的，并写清理由 |
| CAT-LINT-11 | P3 | 交付级别要用户做事，但没写 user_steps | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-12 | P3 | 目录里没有 apps/、recipes/，也没有服务类型定义 | 确认 lint 的目录参数指向目录仓库根目录 |
| CAT-LINT-13 | P3 | 应用目录里有文件不能放进应用包 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-14 | P3 | 镜像包没写 artifact_sha256（不会经下载网关） | 这是警告：要经下载网关发镜像包就补 artifact_sha256 和 artifact_size |
| CAT-LINT-15 | P3 | 给用户看的链接不是 http(s) 地址 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-16 | P3 | 引用了不存在的平台变量 | acornfox catalog platform-vars 看有哪些；按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-17 | P3 | 声明的最低平台版本比用到的写法要求的低 | 把 min_platform 改成 lint 算出的版本（或删掉让它自动算） |
| CAT-LINT-18 | P3 | 交付级别是 auto，但有参数要用户填 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-19 | P3 | 缺变量登记表 gaps.yaml 读不了或格式不对 | 按输出改 gaps.yaml，再运行 acornfox catalog gaps |
| CAT-LINT-20 | P3 | 按架构写的镜像包互相矛盾 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-21 | P3 | 按架构写了镜像包，但有的架构没有（提醒） | 要在那个架构上走下载网关，就补上它的镜像包；不补的话那个架构从公共镜像源取，私有目录的应用在那个架构上装不了 |
| CAT-LINT-22 | P3 | 清单里写的服务类型没有定义 | 类型名写错了就改成 service-types/ 下已有的名字；确实是新类型，先加 service-types/<类型>.yaml，再运行 acornfox catalog lint |
| CAT-LINT-23 | P3 | 清单写的项和服务类型定义对不上 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-24 | P3 | 步骤或服务的值里的引用取不到值 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-25 | P3 | 服务类型定义文件有问题 | 按输出里的文件和字段改 service-types/ 下的定义文件，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-26 | P3 | 清单用了现在的平台还不执行的写法（提醒） | 这是提醒，不挡打包：这样的应用现在装不上，先不要上架；要现在就能装，改成现在的平台能执行的写法 |
| CAT-LINT-27 | P3 | 可选的服务没写“没有时会怎样” | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-28 | P3 | 可选的服务的项用在了不能没有值的地方 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-29 | P3 | retry 写在了不是 GET 的步骤上 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-30 | P3 | finish_if 写在了不是 revoke、purge 的步骤上 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-31 | P3 | 只在首次安装时写的配置文件引用了服务给的值 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-32 | P3 | 给每个使用方发凭据，却没写怎么收回 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-33 | P3 | 同一个应用的两条 provides 是同一种服务 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-34 | P3 | 必须的服务写了“没有时会怎样”（提醒） | 这是提醒，不挡打包：按应用真实的情况二选一改掉 |
| CAT-LINT-35 | P3 | exec 步骤把保密值写进了命令参数 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-36 | P3 | 命令参数里有换行 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-37 | P3 | issue_vars 写得用不上或会撞名 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-38 | P3 | 入口、健康检查和访问方式对不上 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-39 | P3 | 写了平台或用户连不到的东西 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-40 | P3 | 服务的一项用了保密值却没标保密 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-41 | P3 | purge 或 on_check_fail 写在了用不上的地方 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-42 | P3 | 就绪检查的步骤引用了使用方或凭据 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-43 | P3 | 就绪检查的 say 是空的或写了模板 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-44 | P3 | 健康检查的 expect_stdout 永远对不上 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-45 | P3 | 就绪检查里有会改东西的请求 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-46 | P3 | 引用的写法读不了 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-47 | P3 | 没有这种变换，或变换的参数不对 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-48 | P3 | if 的写法不对 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-49 | P3 | 同名的环境变量可能同时成立 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-50 | P3 | 启动命令里引用了保密的值 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-51 | P3 | 这个 {{…}} 不是平台的引用，会原样保留（提醒） | 确认这个 {{…}} 是应用自己的写法；是想引用平台的值就改成 {{种类.名字}} |
| CAT-LINT-52 | P3 | on_change 步骤的 watch 里列了不会变的参数 | 按 lint 输出的路径（/init/…/watch）改 watch：去掉 hidden 的参数，改看会变的设置 |
| CAT-BUILD-01 | P3 | 目录有检查错误，什么都没写 | 先 acornfox catalog lint 修完错误再 build |
| CAT-BUILD-02 | P3 | 打包参数或文件不对 | 按输出改：序号要大于 0、版本号要规范、应用目录里不能有符号链接或读不了的文件 |
| CAT-BUILD-03 | P3 | 输出目录写不进去 | 换一个可写的输出目录或腾出磁盘 |
| CAT-BUILD-04 | P3 | 序号比上次发布的小，或相同但内容不同 | 序号只能往上加：用比上次发布大的序号重新 build |
| CAT-BUILD-05 | P2 | 已发布的应用包会被换成别的内容 | 已发布的应用包不能改：内容变了就给应用发一个新版本号 |
| CAT-KEY-01 | P3 | 签名私钥文件缺失、读不了或格式不对 | 确认私钥文件路径，文件要是 ed25519 的 PKCS#8 PEM |
| CAT-KEY-02 | P2 | 签名私钥别人也能读（权限宽于 0600） | chmod 600 私钥文件后重试 |
| CAT-KEY-03 | P3 | keygen 的目标文件已存在，不会覆盖 | 换一个文件名；不要删掉正在用的钥匙 |
| CAT-KEY-04 | P3 | 公钥字符串不是 32 字节的 ed25519 公钥 | 公钥要是 base64url 编码的 32 字节；照 keygen 输出原样复制 |
| CAT-VER-01 | P3 | 要核对的目录缺文件或 index.json 坏了 | 确认目录里有 version.txt、index.json、index.json.sig |
| CAT-VER-02 | P2 | 签名和受信公钥都对不上 | 用了别的钥匙签名，或文件被改过：不要发布，重新 build |
| CAT-VER-03 | P3 | version.txt 和 index.json 里的序号不一致 | 重新 build，不要手改这两个文件 |
| CAT-VER-04 | P2 | 有应用包缺失，或校验值、大小和索引不符 | 重新 build 或补齐应用包后再 verify |
| GW-REQ-01 | P3 | 请求格式不对 | 多半是程序和网关版本对不上；升级 AcornFox。没有机器钥匙时看 acornfox identity show |
| GW-REG-01 | P3 | 这个 IP 今天新登记的机器太多 | 不用处理，明天恢复；应用照常从公共源装 |
| GW-AUTH-01 | P2 | 签名不对、时间戳超出范围或 nonce 重复 | 先校准本机时间：sudo acornfox doctor 看 SETUP-CLOCK-01 |
| GW-AUTH-02 | P3 | 这台机器还没登记 | 不用处理：会自动登记 |
| GW-AUTH-03 | P2 | 这台机器被拉黑 | 应用照常从公共源装；要用网关请联系我们并附上 acornfox identity show 的安装编号 |
| GW-LIM-01 | P3 | 这台机器今天的次数用完了（明天恢复） | 不用处理，明天恢复；今天应用从公共源装 |
| GW-LIM-02 | P3 | 这个 IP 请求太频繁 | 稍后再试 |
| GW-OFF-01 | P3 | 网关停发（全局或这个文件） | 不用处理：应用从公共源装；私有目录的应用要等网关恢复 |
| GW-ART-01 | P3 | 网关上没有这个文件 | 不用处理：应用从公共源装；私有目录的应用请联系给码的人 |
| GW-BE-01 | P3 | 网关后端（网盘）出错 | 稍后再试；应用照常从公共源装 |
| GW-DB-01 | P3 | 网关的数据库出错 | 稍后再试；应用照常从公共源装 |
| GW-NIMP-01 | P3 | 接口预留，还没实现 | 不用处理 |
| GW-CAT-01 | P3 | 连接码不存在、格式不对或已被别的机器用过 | 按 CAT-CONN-03 处理 |
| GW-CAT-02 | P3 | 连接码已过期 | 按 CAT-CONN-04 处理 |
| GW-CAT-03 | P3 | 私有目录已停用 | 按 CAT-CONN-05 处理 |
| GW-CAT-04 | P3 | 连接码试得太频繁 | 按 CAT-CONN-06 处理 |
| GW-CAT-05 | P2 | 不能通过这个私有目录下载这个文件（没授权、已撤销、目录停用或文件不在目录里） | 按 CAT-INST-26 处理：先 acornfox catalog sync 看目录是否已断开 |
| GW-NET-01 | P3 | 连不上网关或超时（本机判断） | 看 doctor 的下载网关一项（SETUP-GW-01）：网络、代理和网关地址 |
| GW-RESP-01 | P3 | 网关的应答看不懂（本机判断） | 多半是网关地址指错了，或中间有代理改了应答：看 doctor 的下载网关一项 |
| ID-KEY-01 | P2 | 机器钥匙读不了或不是 P-256 钥匙 | sudo acornfox identity show 看原因；文件坏了就 sudo acornfox identity reset --yes 重新生成（安装编号会变） |
| ID-KEY-02 | P2 | 新钥匙保存不了 | 检查数据目录的权限和磁盘空间（acornfox doctor）后重试 |
| MACHINE-01 | P3 | 本机能力有一块没算全 | 运行 acornfox --json machine 看 warnings 里每一项的 part 和 message：manuals 是应用目录读不了（应用名字、一句话、手册没查到），先 acornfox catalog sync；services 是服务登记表的问题，看 acornfox services list；apps 是某个应用的情况读不出来，acornfox status 应用名 |
| MACHINE-02 | P3 | “本机能力”分册写不进 Skill 目录 | 不加 --json 运行 acornfox skill refresh，看 ✗ 那一行写的路径和原因；Skill 目录以前被 sudo 建过的，请用户确认后改回自己的属主；要马上用，直接运行 acornfox --json machine |
| MACHINE-03 | P3 | 这台服务器的“本机能力”分册没能写：连不上，或服务器上的 AcornFox 太旧 | 先运行 acornfox --target 服务器名 status 看能不能连上（连不上看里面的连接编号）；能连上但提示服务器版本旧，让用户先升级服务器上的 AcornFox；好了再 acornfox skill refresh |
| ANY-00 | P2 | 目录里找不到对应的情况 | 先把出错那条输出里的 message 和 hint 原样告诉用户；hint 只是让这条命令换个参数或稍后重试的，可以照做；别的一律只读、不动任何东西，收集证据包交给用户（或反馈给维护者） |

## 安装脚本（install.sh）

### INSTALL-ROOT-01 没有用 root 或 sudo 运行安装脚本

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：命令里加 sudo：把 | bash 改成 | sudo bash（参数照旧）后重新运行
- 现象：安装脚本打印“需要 root 权限”和“故障编号: INSTALL-ROOT-01”

可能原因（逐条判定）：

1. 用普通用户直接运行了 curl … | bash —— 判定：id -u 不是 0，命令里没有 sudo
2. 当前用户没有 sudo 权限 —— 判定：sudo -n true 提示要密码或“不在 sudoers 文件中”

排查顺序：

1. 看安装命令里有没有 sudo
2. sudo -n true 看能不能用 sudo

处置：

- ［1 级 可以直接做］加 sudo 重新运行同一条安装命令（参数照旧）
- ［2 级 先问用户］没有 sudo 权限时请用户换 root 账号或请管理员安装

- 必须先停下问用户：当前账号没有 sudo 权限时：请用户换账号，不要尝试别的提权办法
- 确认恢复：安装脚本走过系统检查，打印“检测到操作系统”
- 演练方法：在测试机上用普通用户运行 bash install.sh

### INSTALL-OS-01 系统不是支持的 Ubuntu 或 Debian

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：不能装：请用户在云厂商控制台把系统重装成 Ubuntu 24.04（重装会清空系统盘），再运行安装命令
- 这一项要用户自己确认或操作
- 现象：安装脚本打印“目前只支持 …”和“故障编号: INSTALL-OS-01”

可能原因（逐条判定）：

1. 系统是 Alibaba Cloud Linux、OpenCloudOS、TencentOS、veLinux、CentOS、Rocky 等非 Debian 系 —— 判定：cat /etc/os-release 看 ID 不是 ubuntu 或 debian
2. 系统是 Ubuntu 的衍生版（Linux Mint、Pop!_OS、deepin 等） —— 判定：/etc/os-release 的 ID 是衍生版自己的名字
3. 没有 /etc/os-release（很旧或很特别的系统） —— 判定：ls /etc/os-release 不存在

排查顺序：

1. cat /etc/os-release

处置：

- ［0 级 只读］把脚本打印的说明转述给用户
- ［2 级 先问用户］重装系统（清空系统盘）只能由用户自己决定、自己做

- 必须先停下问用户：总是：重装系统会清空系统盘，必须用户自己确认没有要保留的数据后去做
- 确认恢复：重装后 cat /etc/os-release 显示 Ubuntu 或 Debian 的支持版本，安装脚本走过系统检查
- 演练方法：在 Rocky Linux 测试机上运行 install.sh

### INSTALL-OS-02 Ubuntu 或 Debian 的版本不在支持列表

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：建议把系统重装成 Ubuntu 24.04；用户坚持要试，他同意后加 -s -- --force-os 重跑
- 这一项要用户自己确认或操作
- 现象：安装脚本打印“… 不在支持列表”或“无法取得系统代号”和“故障编号: INSTALL-OS-02”

可能原因（逐条判定）：

1. 版本太旧或太新（例如 Ubuntu 20.04、Debian 11） —— 判定：cat /etc/os-release 看 VERSION_ID
2. /etc/os-release 里没有 VERSION_CODENAME —— 判定：grep VERSION_CODENAME /etc/os-release 为空

排查顺序：

1. cat /etc/os-release 看版本和代号

处置：

- ［0 级 只读］建议用户重装成支持的版本
- ［2 级 先问用户］加 --force-os 在不支持的版本上试装（自行承担风险）

- 必须先停下问用户：用 --force-os 和重装系统都要用户决定
- 确认恢复：安装脚本走过系统检查

### INSTALL-ARCH-01 CPU 架构不是 x86_64 或 arm64

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：不能装，请用户换一台 x86_64 或 arm64 的服务器
- 现象：安装脚本打印“不支持的 CPU 架构”和“故障编号: INSTALL-ARCH-01”

可能原因（逐条判定）：

1. 服务器是 32 位、armv7、龙芯、申威等架构 —— 判定：uname -m 不是 x86_64、aarch64

排查顺序：

1. uname -m

处置：

- ［0 级 只读］转述给用户：这台机器装不了

- 必须先停下问用户：换服务器（可能花钱）由用户决定
- 确认恢复：换机后 uname -m 为 x86_64 或 aarch64

### INSTALL-DISK-01 /var/lib 所在磁盘剩余不到 2 GB

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：请用户清理磁盘或扩容系统盘后重新运行（扩容花钱，用户决定）
- 现象：安装脚本打印“/var/lib 所在磁盘只剩 … MB，至少要 2 GB”和“故障编号: INSTALL-DISK-01”

可能原因（逐条判定）：

1. 系统盘本来就小 —— 判定：df -h /var/lib 看总大小
2. 旧的软件包缓存、日志、别的程序的数据占了空间 —— 判定：sudo du -xh --max-depth=1 /var | sort -h | tail

排查顺序：

1. df -h /var/lib
2. sudo du -xh --max-depth=1 /var | sort -h | tail

处置：

- ［1 级 可以直接做］sudo apt-get clean 清掉软件包缓存
- ［2 级 先问用户］删除用户的文件、扩容磁盘

- 必须先停下问用户：要删文件或扩容时
- 确认恢复：df -h /var/lib 剩余超过 2 GB，安装脚本走过磁盘检查
- 相关编号：SETUP-DISK-01

### INSTALL-PORT-01 AcornFox 自己要用的端口（18800、18801、18807）被别的程序占用

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：把脚本列出的程序和 PID 转述给用户，问用户能不能停掉或改端口；处理后重跑
- 现象：安装或升级脚本列出占用端口的程序，打印“故障编号: INSTALL-PORT-01”

可能原因（逐条判定）：

1. 别的程序或容器正好用了 18800、18801 或 18807 —— 判定：sudo ss -ltnp | grep -E ':(18800|18801|18807) '，看 PID 对应的程序（ps -fp PID）
2. 以前装过的 AcornFox 进程没退干净（不是用安装脚本装的、或手动启动的） —— 判定：ps -fp PID 显示是 acornfox

排查顺序：

1. sudo ss -ltnp | grep -E ':(18800|18801|18807) '
2. ps -fp 占用的 PID

处置：

- ［0 级 只读］转述占用的程序和 PID
- ［2 级 先问用户］停掉或改掉占用端口的程序

- 必须先停下问用户：停任何程序之前都要问用户
- 确认恢复：ss 里这三个端口没有别的程序，重跑安装或升级走过端口检查
- 演练方法：在测试机上 python3 -m http.server 18807 后运行 install.sh

### INSTALL-PORT-02 80 或 443 被别的程序占用（常见：宝塔、1Panel、nginx）

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：问用户二选一：停掉原来的程序后重跑，或者加 -s -- --allow-busy-80-443 共存（不能在 AcornFox 里绑域名、开 HTTPS 和公网控制台）
- 现象：安装或升级脚本打印“80/443 已被别的程序占用”或“要开通公网控制台，但 80/443 被别的程序占用”和“故障编号: INSTALL-PORT-02”

可能原因（逐条判定）：

1. 服务器上原来装了宝塔、1Panel、nginx、Apache 或别的反向代理 —— 判定：sudo ss -ltnp | grep -E ':(80|443) '，ps -fp PID
2. 别的 Docker 容器映射了 80 或 443 —— 判定：docker ps --format '{{.Names}} {{.Ports}}' | grep -E ':(80|443)->'

排查顺序：

1. sudo ss -ltnp | grep -E ':(80|443) '
2. 问用户这个程序还要不要

处置：

- ［2 级 先问用户］加 --allow-busy-80-443 重跑（共存，应用照常用 IP:端口 访问）
- ［2 级 先问用户］停掉占用 80/443 的程序后重跑

- 必须先停下问用户：总是：两条路都要用户选；停程序前必须用户同意
- 确认恢复：重跑后安装完成；共存时 doctor 的 SETUP-PORT-01 是警告（预期）
- 相关编号：SETUP-PORT-01

### INSTALL-PORT-03 应用端口段 18810-18899 里有端口被别的程序占用

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：转述占用的程序，问用户能不能停掉或改端口；处理后重跑
- 现象：安装或升级脚本列出端口段里被占的端口，打印“故障编号: INSTALL-PORT-03”

可能原因（逐条判定）：

1. 别的程序或 Docker 容器正好用了这一段里的端口 —— 判定：sudo ss -ltnp | awk '$4 ~ /:188[1-9][0-9]$/'

排查顺序：

1. sudo ss -ltnp 看是哪几个端口、哪个程序

处置：

- ［0 级 只读］转述占用的程序
- ［2 级 先问用户］停掉或改掉占用的程序

- 必须先停下问用户：停任何程序之前
- 确认恢复：重跑后走过端口检查
- 相关编号：SETUP-PORT-03

### INSTALL-CLOUD-01 --cloud 或 ACORNFOX_CLOUD_OVERRIDE 的值不认识

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：这是测试用的参数，去掉后重跑
- 现象：安装或升级脚本打印“ACORNFOX_CLOUD_OVERRIDE / --cloud 的值不认识”和“故障编号: INSTALL-CLOUD-01”

可能原因（逐条判定）：

1. 手写了 --cloud 或设了 ACORNFOX_CLOUD_OVERRIDE，值不在 aliyun、tencent、huawei、volcengine、none 里 —— 判定：看命令和 env | grep ACORNFOX_CLOUD

排查顺序：

1. 看命令行参数和环境变量

处置：

- ［1 级 可以直接做］去掉 --cloud 和 ACORNFOX_CLOUD_OVERRIDE 后重跑

- 必须先停下问用户：—（不用问）
- 确认恢复：重跑后脚本打印认出的云厂商或“不是已知云厂商”

### INSTALL-DOWN-01 服务器上已有更新版本的 AcornFox（或它的数据），要装的版本比它旧

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：什么都没改。不要换参数重试；要换新版本就用升级命令。确实要装旧版本只能先卸载并删除数据（--purge，不可恢复），必须用户明确同意
- 这一项要用户自己确认或操作
- 现象：安装脚本打印“已经装着 AcornFox …，要装的 … 比它旧”或“留着 AcornFox … 的数据”和“故障编号: INSTALL-DOWN-01”

可能原因（逐条判定）：

1. 用 --version 指定了比现在旧的版本 —— 判定：acornfox --json version 和命令里的 --version 比较
2. 卸载时保留了数据（/var/lib/acornfox），数据是更新版本写的 —— 判定：cat /var/lib/acornfox/installed-version

排查顺序：

1. acornfox --json version
2. cat /var/lib/acornfox/installed-version

处置：

- ［1 级 可以直接做］去掉 --version 装最新版，或用升级脚本升级
- ［2 级 先问用户］uninstall.sh --purge 删除全部数据后装旧版本

- 必须先停下问用户：总是不能降级；要删数据时必须用户明确说“删”
- 确认恢复：acornfox --json version 是想要的版本，应用都在
- 相关编号：UPGRADE-DOWN-01

### INSTALL-VER-01 脚本和要装的版本（或 --binary 给的程序）不是同一个版本

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：什么都没改。不要找别的脚本来凑；用一行命令并写明版本，它会取那个版本自带的脚本
- 现象：安装、升级或装命令行的脚本打印“这份…脚本是 vX 的，只装 vX”和“故障编号: INSTALL-VER-01”

可能原因（逐条判定）：

1. 手里的脚本是从某个发行版下载的，却让它装别的版本 —— 判定：命令里的 --version 和报错里说的脚本版本不一样
2. --binary 给的程序文件和脚本不是同一个发行版里的 —— 判定：./程序文件 version 和报错里说的脚本版本不一样

排查顺序：

1. 看报错里“脚本是哪个版本”“要装哪个版本”
2. 确认想装的到底是哪个版本

处置：

- ［1 级 可以直接做］用一行命令并加 --version 想要的版本（get.sh 会取那个版本自带的脚本）
- ［1 级 可以直接做］离线安装时，脚本和程序文件取自同一个发行版

- 必须先停下问用户：用户没说要哪个版本时先问，不要替用户换版本
- 确认恢复：重新运行后不再报 INSTALL-VER-01，acornfox --json version 是想要的版本
- 相关编号：INSTALL-SRC-01、INSTALL-DOWN-01

### INSTALL-SRC-01 直接运行了源码仓库里的安装、升级脚本

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：什么都没改。照报错里给的一行命令重新运行（入口是 get.sh）
- 现象：脚本打印“这是源码仓库里的…脚本，没有绑定版本，不能直接用来…”和“故障编号: INSTALL-SRC-01”

可能原因（逐条判定）：

1. 用了旧文档里的命令，直接读仓库主分支上的 install.sh、upgrade.sh 或 install-cli.sh —— 判定：命令里的地址是 …/raw/main/scripts/install/…
2. 克隆了仓库后直接运行 scripts/install 下的脚本 —— 判定：脚本所在目录是源码仓库

排查顺序：

1. 看用的是哪条命令

处置：

- ［1 级 可以直接做］改用报错里打印的一行命令（国内 Gitee、海外 GitHub 各一条）

- 必须先停下问用户：不用问，直接换命令
- 确认恢复：新命令开头打印“[引导] 最新正式版：vX”，之后是正常的安装或升级输出
- 相关编号：INSTALL-VER-01、INSTALL-GET-01

### INSTALL-GET-01 引导脚本查不到最新的正式版号

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：什么都没改。先看能不能连上 gitee.com 和 github.com；也可以在命令后面写明版本
- 现象：一行命令打印“[引导] 出错：查不到最新的正式版号”和“故障编号: INSTALL-GET-01”

可能原因（逐条判定）：

1. 这台机器连不上 gitee.com 和 github.com（防火墙、DNS、代理） —— 判定：curl -I https://gitee.com 和 curl -I https://github.com 都不通
2. 两个站点一时都在限流或出故障 —— 判定：过几分钟再试能通

排查顺序：

1. curl -I https://gitee.com
2. curl -I https://github.com
3. getent hosts gitee.com

处置：

- ［0 级 只读］过几分钟重试
- ［1 级 可以直接做］命令后面加 -s -- --version vX.Y.Z 写明版本（不用查最新版）
- ［2 级 先问用户］修这台机器的网络或 DNS

- 必须先停下问用户：改网络、DNS、代理之前问用户
- 确认恢复：重新运行后打印“[引导] 最新正式版：vX”
- 相关编号：INSTALL-GET-02

### INSTALL-GET-02 引导脚本下载那个版本的脚本或校验文件失败

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：什么都没改。核对版本号是否存在；网络不通就过一会儿再试或换一个源（--china、--global）
- 现象：一行命令打印“[引导] 出错：下载 vX 的 … 失败”和“故障编号: INSTALL-GET-02”

可能原因（逐条判定）：

1. 写的版本号不存在 —— 判定：发行版列表里没有这个版本
2. 两个下载源都连不上或中途断了 —— 判定：curl -I https://gitee.com、curl -I https://github.com
3. 这台机器没有 curl —— 判定：command -v curl 没有输出

排查顺序：

1. 核对版本号
2. curl -I https://gitee.com
3. curl -I https://github.com

处置：

- ［0 级 只读］过几分钟重试，或加 --china、--global 换先用的源
- ［1 级 可以直接做］改成存在的版本号
- ［1 级 可以直接做］apt-get install -y curl

- 必须先停下问用户：换版本前问用户要哪个版本
- 确认恢复：重新运行后打印“[引导] 交给 vX 的 …”
- 相关编号：INSTALL-GET-01、INSTALL-GET-03

### INSTALL-GET-03 引导脚本下载到的脚本校验不过，没有执行

- 分组：安装脚本（install.sh）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：什么都没改、什么都没执行。换一个源再试；两个源都不过时停下反馈，不要绕过校验
- 这一项要用户自己确认或操作
- 现象：一行命令打印“[引导] 出错：… 校验不过”或“SHA256SUMS 里没有 … 这一项”和“故障编号: INSTALL-GET-03”

可能原因（逐条判定）：

1. 下载被截断（网络中断、代理改了内容） —— 判定：换一个源（--china 或 --global）后通过
2. 某个下载源上的文件被替换了，或者发行版上传不完整 —— 判定：两个源各下载一次，和发行版页面上的 SHA256SUMS 比

排查顺序：

1. 加 --china 试一次，加 --global 试一次
2. 两个源都不过时记下报错里的两个校验值

处置：

- ［0 级 只读］换一个源重试
- ［0 级 只读］两个源都不过：停下，把报错原样反馈给 AcornFox

- 必须先停下问用户：不要手工下载脚本绕过校验；确实要这样做必须用户明确同意
- 确认恢复：重新运行后打印“[引导] … 校验通过”
- 相关编号：INSTALL-GET-02

### INSTALL-GET-04 引导脚本的用法不对

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：什么都没改。版本号写成 v0.6.0 这样；动作只有 install（默认）、upgrade、cli、uninstall
- 现象：一行命令打印“[引导] 出错：版本号写成…”或“--version 后面要跟版本号”和“故障编号: INSTALL-GET-04”

可能原因（逐条判定）：

1. --version 后面没有跟版本号，或者版本号的格式不对 —— 判定：看命令里 --version 后面的内容

排查顺序：

1. 核对命令

处置：

- ［1 级 可以直接做］改成 --version v0.6.0 这样的写法后重新运行

- 必须先停下问用户：不知道要哪个版本时问用户；不写 --version 就是最新正式版
- 确认恢复：重新运行后不再报 INSTALL-GET-04
- 相关编号：INSTALL-GET-02

### INSTALL-DOCKER-01 装 Docker 的软件源都不能用，发行版自带的 docker.io 也没装上

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：安装脚本装 Docker 前，对候选的几家 docker-ce 软件源（脚本开头的 DOCKER_CE_SOURCES_CN，以及所在云的内网源、ACORNFOX_DOCKER_CE_MIRROR 指定的）同时各取一次索引和一小段安装包测快慢，按“够快的照表里先后、不够快的按实测快慢”排好再逐个试；都不行时退回发行版自带的 docker.io，再不行才停在这里。报错里列出每个源当时的结果。
- 下一步：Docker 没装上，AcornFox 还没装。稍后重试；或用 ACORNFOX_DOCKER_CE_MIRROR=<能用的 docker-ce 源> 指定一个；或先自己装好 Docker 再加 --skip-docker
- AcornFox 已经自动做的：已经按实测快慢依次试过每个能连上的 docker-ce 源，又试过发行版自带的 docker.io
- 现象：安装脚本打印“Docker 安装包下载源实测（…）”一行，之后是“Docker 安装失败”和“故障编号: INSTALL-DOCKER-01”

可能原因（逐条判定）：

1. 这台服务器连不上任何 docker-ce 镜像站（防火墙、DNS、代理） —— 判定：报错里每个源都写“连不上或取不到索引”；curl -I https://mirrors.aliyun.com 不通
2. 各家镜像站刚好在同步 Docker 新版本（索引和包对不上）［doctor 自动判定］ —— 判定：报错里源能连上，但“索引不可用”或装包失败；过半小时重试能过
3. 发行版的软件源本身不通，docker.io 也装不了 —— 判定：apt-get update 报错

排查顺序：

1. 看报错里每个源的结果
2. curl -I https://mirrors.aliyun.com/docker-ce/
3. apt-get update

处置：

- ［1 级 可以直接做］过一会儿重新运行同一条安装命令
- ［1 级 可以直接做］用 ACORNFOX_DOCKER_CE_MIRROR 指定一个能用的 docker-ce 源再装
- ［2 级 先问用户］自己装好 Docker 后加 --skip-docker 再装
- ［2 级 先问用户］改这台机器的网络、DNS 或代理

- 必须先停下问用户：要改网络设置或自己装 Docker 之前
- 确认恢复：重新运行后打印“Docker 安装成功”
- 相关编号：INSTALL-OS-02
- 演练方法：在测试机里把候选镜像站都指到 127.0.0.1（写进 /etc/hosts，连同发行版软件源），再运行安装命令

## 升级和卸载脚本

### UPGRADE-OLD-01 现在装的是 v0.3.x，不能直接升到这个版本

- 分组：升级和卸载脚本
- 默认等级：P3（提示或用法问题，不急）
- 下一步：服务没停、什么都没改。先升到 v0.5.1（升级命令加 --version v0.5.1），再运行一次升级命令升到最新版
- 现象：升级脚本打印“现在装的是 v0.3.x”和“故障编号: UPGRADE-OLD-01”

可能原因（逐条判定）：

1. 这台服务器很久没升级，还是 v0.3.x（网关装在宿主机上的那一代） —— 判定：acornfox version

排查顺序：

1. acornfox version

处置：

- ［1 级 可以直接做］先运行升级命令加 --version v0.5.1，成功后再运行一次不带 --version 的升级命令

- 必须先停下问用户：—（照着做就行；升级前照常告诉用户应用网址会断几秒）
- 确认恢复：acornfox version 是最新版
- 相关编号：UPGRADE-DOWN-01
- 演练方法：在一台装着 v0.3.x 的测试机上运行升级脚本

### UPGRADE-DOWN-01 升级时指定了比现在旧的版本

- 分组：升级和卸载脚本
- 默认等级：P3（提示或用法问题，不急）
- 下一步：服务没停、什么都没改。AcornFox 只能升级：去掉 --version 重新运行升级命令；不要加 -y 或换旧版本号重试
- 现象：升级脚本打印“当前是 …，要装的 … 比它旧”和“故障编号: UPGRADE-DOWN-01”

可能原因（逐条判定）：

1. --version 写了旧版本，或用户想“退回上一版” —— 判定：命令里的 --version 和 acornfox --json version

排查顺序：

1. acornfox --json version

处置：

- ［0 级 只读］告诉用户平台不能降级；要回退的是应用就用 acornfox rollback 应用名

- 必须先停下问用户：用户其实想退回平台版本时：转述“不支持”，不要找旧脚本
- 确认恢复：—（什么都没改）
- 相关编号：UPGRADE-ROLLBACK-01、INSTALL-DOWN-01

### UPGRADE-ROLLBACK-01 用了 --rollback：平台不能回到旧版本

- 分组：升级和卸载脚本
- 默认等级：P3（提示或用法问题，不急）
- 下一步：服务没停、什么都没改。升级没成功时脚本当时已自动恢复；应用自己的版本用 acornfox rollback 应用名 回退
- 现象：升级脚本或 acornfox install --rollback 打印“只能升级，不能回到旧版本”和 UPGRADE-ROLLBACK-01

可能原因（逐条判定）：

1. 以为可以回到上一版平台 —— 判定：命令里有 --rollback

排查顺序：

1. 问清用户想回退的是平台还是某个应用

处置：

- ［2 级 先问用户］应用回退：acornfox versions 应用名 看版本，acornfox rollback 应用名

- 必须先停下问用户：回退应用前问用户回到哪一版
- 确认恢复：—（什么都没改）
- 相关编号：UPGRADE-DOWN-01

### UPGRADE-BACKUP-01 停服务后备份没做完，没有升级

- 分组：升级和卸载脚本
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：没有升级，原来的服务已重新启动。df -h /var/lib/acornfox 看空间，转述给用户；腾出空间后再升级
- AcornFox 已经自动做的：已重新启动原来的服务，程序和数据库都没改
- 现象：升级脚本打印“备份没做完，没有升级”和“故障编号: UPGRADE-BACKUP-01”

可能原因（逐条判定）：

1. 磁盘满了 —— 判定：df -h /var/lib/acornfox 剩余很少
2. 备份目录 /var/lib/acornfox/backups 写不进去（权限、只读文件系统） —— 判定：sudo touch /var/lib/acornfox/backups/x 报错

排查顺序：

1. systemctl is-active acornfox-server acornfox-runner
2. df -h /var/lib/acornfox

处置：

- ［0 级 只读］确认服务已在运行
- ［2 级 先问用户］清理磁盘或扩容

- 必须先停下问用户：清理什么、扩容都由用户决定
- 确认恢复：acornfox doctor 全过，再运行升级命令成功
- 相关编号：SETUP-DISK-01

### UPGRADE-FAIL-01 新版本的数据库迁移失败，已自动恢复到升级前

- 分组：升级和卸载脚本
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：没有升上，服务照常运行。告诉用户现在仍是原来的版本；先问用户要不要再试一次，再失败就停下，把输出反馈给维护者
- AcornFox 已经自动做的：已用升级前的备份换回程序和数据库，服务照常运行，应用容器一直在跑
- 现象：升级脚本打印“数据库迁移失败，没有升上：已自动恢复到升级前”和“故障编号: UPGRADE-FAIL-01”

可能原因（逐条判定）：

1. 新版本的迁移和这台机器上的旧数据不兼容（程序问题） —— 判定：/var/log/acornfox-upgrade.log 里迁移那几行的报错
2. 磁盘满了，迁移写不进去 —— 判定：df -h /var/lib/acornfox

排查顺序：

1. acornfox --json version 确认是原来的版本
2. sudo tail -n 40 /var/log/acornfox-upgrade.log
3. df -h /var/lib/acornfox

处置：

- ［0 级 只读］告诉用户现在的版本和失败原因
- ［2 级 先问用户］再运行一次升级命令

- 必须先停下问用户：再试之前；第二次失败就停下，带上证据包反馈给维护者
- 确认恢复：acornfox --json version 是原来的版本、doctor 全过；或再升级成功
- 回滚：不需要：脚本已自动恢复

### UPGRADE-FAIL-02 新版本的服务起不来，已自动恢复到升级前

- 分组：升级和卸载脚本
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：没有升上，服务照常运行。先问用户要不要再试一次，再失败就停下，把输出和 journalctl -u acornfox-server -n 50 反馈给维护者
- AcornFox 已经自动做的：已用升级前的备份换回程序和数据库，服务照常运行
- 现象：升级脚本打印“服务起不来，没有升上：已自动恢复到升级前”和“故障编号: UPGRADE-FAIL-02”

可能原因（逐条判定）：

1. 新版本在这台机器上启动出错（程序问题） —— 判定：/var/log/acornfox-upgrade.log、journalctl -u acornfox-server -n 50 --no-pager
2. 端口被别的程序抢先占了 —— 判定：日志里有 address already in use

排查顺序：

1. acornfox --json version
2. sudo tail -n 40 /var/log/acornfox-upgrade.log

处置：

- ［0 级 只读］转述失败原因
- ［2 级 先问用户］再运行一次升级命令

- 必须先停下问用户：再试之前；第二次失败就停下反馈
- 确认恢复：doctor 全过
- 回滚：不需要：脚本已自动恢复

### UPGRADE-FAIL-03 升级中途出错或被中断（包括 SSH 断开），已自动恢复到升级前

- 分组：升级和卸载脚本
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：没有升上，服务照常运行。可以再运行一次升级命令
- AcornFox 已经自动做的：已自动恢复到升级前；恢复过程不响应中断
- 现象：升级脚本打印“升级中途出错或被中断”和“故障编号: UPGRADE-FAIL-03”；SSH 断开后重新连上，/var/log/acornfox-upgrade.log 末尾是这个编号

可能原因（逐条判定）：

1. SSH 断开或按了 Ctrl-C —— 判定：/var/log/acornfox-upgrade.log 里写了被中断
2. 下载或替换文件中途出错 —— 判定：upgrade.log 里出错那几行

排查顺序：

1. sudo tail -n 40 /var/log/acornfox-upgrade.log
2. acornfox --json version

处置：

- ［1 级 可以直接做］再运行一次升级命令（用户已同意过升级）

- 必须先停下问用户：第二次还失败时
- 确认恢复：升级完成，acornfox --json version 是新版本
- 回滚：不需要：脚本已自动恢复

### UPGRADE-RESTORE-01 升级失败后没能恢复到升级前（要人处理）

- 分组：升级和卸载脚本
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：停下。把输出原样给用户，请用户把 journalctl -u acornfox-server -n 50 和 -u acornfox-runner -n 50 反馈给维护者；不要删备份、不要重装、不要自己拷备份里的文件
- 这一项要用户自己确认或操作
- 现象：升级脚本打印“找不到本次升级前的备份”“这些文件没能换回去”或“换回升级前的，但服务还是起不来”和“故障编号: UPGRADE-RESTORE-01”；控制台和命令行用不了，应用一般还能打开

可能原因（逐条判定）：

1. 磁盘满，文件换不回去 —— 判定：df -h /var/lib/acornfox /usr/local/bin
2. 备份丢了或记录不对 —— 判定：ls /var/lib/acornfox/backups/
3. 换回旧程序后服务仍起不来 —— 判定：journalctl -u acornfox-server -n 50 --no-pager

排查顺序：

1. df -h /var/lib/acornfox /usr/local/bin
2. journalctl -u acornfox-server -n 50 --no-pager
3. journalctl -u acornfox-runner -n 50 --no-pager

处置：

- ［0 级 只读］收集上面三项的输出
- ［2 级 先问用户］其余一律不动

- 必须先停下问用户：立即：带上证据包和上面的日志
- 确认恢复：维护者确认后 acornfox doctor 全过

### UNINSTALL-TTY-01 卸载脚本没有终端、没法询问确认，又没加 -y

- 分组：升级和卸载脚本
- 默认等级：P3（提示或用法问题，不急）
- 下一步：什么都没改。请用户在自己的终端里运行卸载命令；只有用户明确要你代为卸载时才加 -y
- 这一项要用户自己确认或操作
- 现象：卸载脚本打印“没有终端，没法询问确认”和“故障编号: UNINSTALL-TTY-01”

可能原因（逐条判定）：

1. 经 ssh 执行命令、没有分配终端（例如 AI 工作台代跑） —— 判定：命令是 ssh 主机 'sudo bash uninstall.sh' 这种写法

排查顺序：

1. 确认用户确实要卸载，以及要不要连数据一起删（--purge）

处置：

- ［2 级 先问用户］用户明确同意后加 -y 运行（不带 --purge 时保留数据）

- 必须先停下问用户：总是：卸载只在用户明确要求时做；--purge 会删掉全部数据，不可恢复
- 确认恢复：systemctl status acornfox-server 显示没有这个服务

## Windows 安装（install-windows.ps1）

### WIN-OS-01 Windows 版本太旧

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：先升级 Windows（Windows 10 22H2 或 Windows 11）再重新运行安装命令
- 现象：安装命令在检查这一步停下，带 WIN-OS-01，什么都没装

可能原因（逐条判定）：

1. 系统版本号低于 19045（Windows 10 22H2）［doctor 自动判定］ —— 判定：运行 winver 或 acornfox windows doctor 看“Windows 版本号”

排查顺序：

1. acornfox windows doctor（没装好时用 get.ps1 加 -Action doctor）看版本号
2. 设置 → Windows 更新

处置：

- ［0 级 只读］告诉用户先升级 Windows

- 必须先停下问用户：总是：升级系统要用户自己做
- 确认恢复：重新运行安装命令，检查通过
- 演练方法：只能在老系统上试，没有演练方法

### WIN-ARCH-01 这台电脑的 CPU 架构暂时不支持

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：现在只有 64 位 Intel/AMD 的 WSL 安装包和 Linux 镜像放在下载网关里；ARM 版 Windows 还没有对应的文件，装不了。
- 下一步：ARM 版 Windows 暂时用不了；可以改把 AcornFox 装在一台 Linux 服务器上，用这台电脑通过 SSH 管理
- 现象：安装命令在检查这一步停下，带 WIN-ARCH-01

可能原因（逐条判定）：

1. 这是 ARM 版 Windows（例如 Surface Pro X、Snapdragon 笔记本）［doctor 自动判定］ —— 判定：echo %PROCESSOR_ARCHITECTURE% 显示 ARM64

排查顺序：

1. echo %PROCESSOR_ARCHITECTURE%

处置：

- ［0 级 只读］照实告诉用户暂时不支持，给出装在 Linux 服务器上的办法

- 必须先停下问用户：总是
- 确认恢复：换到 64 位 Intel/AMD 的电脑后检查通过

### WIN-POLICY-01 公司或学校的策略禁止了 WSL

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：请联系管理这台电脑的 IT 人员放开 WSL；自己改注册表可能违反规定，安装脚本不会替你改
- 现象：安装命令在检查这一步停下，带 WIN-POLICY-01；直接运行 wsl 报 ERROR_ACCESS_DISABLED_BY_POLICY

可能原因（逐条判定）：

1. 组策略把 AllowWSL 设成了 0［doctor 自动判定］ —— 判定：reg query HKLM\SOFTWARE\Policies\WSL /v AllowWSL 的值是 0x0

排查顺序：

1. acornfox windows doctor
2. reg query HKLM\SOFTWARE\Policies\WSL

处置：

- ［0 级 只读］把原因和注册表位置告诉用户，请他联系 IT

- 必须先停下问用户：总是：不要替用户改系统策略
- 确认恢复：IT 放开后重新运行安装命令，检查通过
- 演练方法：在测试机上 reg add HKLM\SOFTWARE\Policies\WSL /v AllowWSL /t REG_DWORD /d 0 /f，测完 reg delete 掉

### WIN-VIRT-01 CPU 的虚拟化功能没打开

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：WSL 2 要用 CPU 的虚拟化功能。固件里关着时，Windows 看不到它，装了也跑不起来。虚拟机里运行的 Windows 要在虚拟机设置里打开嵌套虚拟化。
- 下一步：重启电脑进 BIOS/UEFI，打开 Intel VT-x（Intel Virtualization Technology）或 AMD-V（SVM Mode），保存后再运行安装命令
- 现象：安装命令在检查这一步停下，带 WIN-VIRT-01；任务管理器 → 性能 → CPU 里“虚拟化”显示“已禁用”

可能原因（逐条判定）：

1. BIOS/UEFI 里关了虚拟化［doctor 自动判定］ —— 判定：任务管理器 → 性能 → CPU 的“虚拟化”是“已禁用”；或 systeminfo 末尾写“固件中已启用虚拟化: 否”
2. 这台 Windows 本身是虚拟机，宿主机没给它开嵌套虚拟化 —— 判定：systeminfo 里“系统制造商”是 QEMU、VMware、Microsoft Corporation（Hyper-V）等

排查顺序：

1. acornfox windows doctor
2. 任务管理器看“虚拟化”
3. systeminfo

处置：

- ［0 级 只读］告诉用户进 BIOS 打开虚拟化（各品牌选项名不同，常见写法见上）

- 必须先停下问用户：总是：改 BIOS 要用户自己做
- 确认恢复：重新运行安装命令，检查通过

### WIN-DISK-01 系统盘剩余空间不够

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：清理系统盘，至少留 10 GB 再运行安装命令（Debian 加 Docker 加几个应用大约要 3～5 GB）
- 现象：安装命令在检查这一步停下，带 WIN-DISK-01

可能原因（逐条判定）：

1. 系统盘剩余空间小于 10 GB［doctor 自动判定］ —— 判定：acornfox windows doctor 的“系统盘剩 N GB”

排查顺序：

1. acornfox windows doctor
2. 设置 → 系统 → 存储

处置：

- ［0 级 只读］告诉用户清理哪里（下载文件夹、回收站、临时文件）

- 必须先停下问用户：要删用户的文件时
- 确认恢复：剩余空间 10 GB 以上后重新运行安装命令

### WIN-PS-01 缺少必要的系统组件

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：升级 Windows 10 到 22H2 后再运行；需要 PowerShell 5.1 和 System32 里的 curl.exe
- 现象：安装命令在检查这一步停下，带 WIN-PS-01

可能原因（逐条判定）：

1. 没有 curl.exe（Windows 10 1803 以前的系统没有） —— 判定：where curl.exe 没有结果
2. PowerShell 低于 5.1 —— 判定：$PSVersionTable.PSVersion

排查顺序：

1. where curl.exe
2. $PSVersionTable.PSVersion

处置：

- ［0 级 只读］告诉用户升级 Windows

- 必须先停下问用户：总是
- 确认恢复：重新运行安装命令，检查通过

### WIN-SRC-01 这是源码里的脚本，没有绑定版本

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：用引导脚本 get.ps1 安装（它取要装的那个版本自带的 install-windows.ps1）；开发测试时加 -Version 或 -LocalPackage
- 现象：直接运行仓库里的 install-windows.ps1 报 WIN-SRC-01；脚本只装自己这个版本，被要求装别的版本时也报它

可能原因（逐条判定）：

1. 运行的是仓库里的源码（版本戳是 dev） —— 判定：脚本里 $ScriptVersion 是 'dev'
2. -Version 和发行版脚本自带的版本不一致 —— 判定：屏幕上写“这个脚本只装 vX.Y.Z”

排查顺序：

1. 改用 get.ps1 加 -Version 重新运行

处置：

- ［1 级 可以直接做］用 get.ps1 运行同样的命令

- 必须先停下问用户：不用问
- 确认恢复：安装命令不再报 WIN-SRC-01

### WIN-ADMIN-01 管理员确认被取消，什么都没安装

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：想继续就重新运行安装命令；弹窗上写 Windows PowerShell、发布者 Microsoft Windows 时选“是”。已经下载的文件会留着，不用重下
- 现象：安装命令打印“管理员确认被取消”，退出码 5

可能原因（逐条判定）：

1. 在管理员确认里选了“否”或关掉了 —— 判定：问用户刚才点了什么
2. 安全软件拦了提权（360、火绒偶尔会弹窗） —— 判定：屏幕上有没有安全软件的提示
3. 当前账号不是管理员，需要输管理员的密码，输错了或取消了 —— 判定：弹窗里是不是要密码

排查顺序：

1. 问用户
2. 重新运行

处置：

- ［2 级 先问用户］请用户再运行一次并选“是”

- 必须先停下问用户：总是：只有用户本人能点这个确认，输管理员密码也只能他自己输
- 确认恢复：管理员那一步完成，安装继续往下走
- 相关编号：WIN-ADMIN-02

### WIN-ADMIN-02 管理员那一步出错了

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：一次管理员确认里要做的事：打开虚拟机平台、安装 WSL、登记计划任务、防火墙规则。其中一件没做成。
- 下一步：看 %LOCALAPPDATA%\AcornFox\logs\admin-step.log 里最后几行，再重新运行安装命令（做过的步骤会跳过）
- 现象：安装命令打印“管理员那一步出错了”，带 WIN-ADMIN-02；管理员窗口里有红字

可能原因（逐条判定）：

1. dism 或 msiexec 返回了错误码（系统更新没装完、别的安装程序在占用） —— 判定：admin-step.log 里“dism 退出码”“msiexec 退出码”那几行，另有 wsl-msi.log
2. WSL 安装包复制到管理员目录后校验不对（文件被改动或磁盘问题） —— 判定：admin-step.log 里写“校验不对”
3. 安全软件拦了注册计划任务或防火墙规则 —— 判定：admin-step.log 里写“拒绝访问”；安全软件的拦截记录
4. 管理员那一步没有留下结果文件（被安全软件结束了） —— 判定：屏幕上写“没有留下结果文件”

排查顺序：

1. 看 %LOCALAPPDATA%\AcornFox\logs\admin-step.log 最后 20 行
2. 看 wsl-msi.log（msiexec 的详细记录）
3. Windows 更新有没有挂起
4. 重新运行安装命令

处置：

- ［2 级 先问用户］等 Windows 更新装完、重启后重新运行
- ［1 级 可以直接做］重新运行安装命令

- 必须先停下问用户：同一处连续失败两次：带 admin-step.log 和 wsl-msi.log 反馈
- 确认恢复：重新运行后管理员那一步完成
- 相关编号：WIN-ADMIN-01、WIN-REBOOT-01

### WIN-REBOOT-01 需要重启电脑才能继续

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：刚打开 Windows 的“虚拟机平台”功能或装了 WSL，要重启一次才生效。重启后登录会自动弹出窗口接着装。
- 下一步：保存手上的工作后重启电脑；重启后登录，会自动弹出窗口接着装（也可以自己再运行一次同样的安装命令）
- 这一项要用户自己确认或操作
- 现象：安装命令打印“Windows 要重启一次”，退出码 3

可能原因（逐条判定）：

1. 第一次装 WSL：打开虚拟机平台后 Windows 要求重启［doctor 自动判定］ —— 判定：admin-step.log 里 dism 或 msiexec 退出码是 3010（要重启）

排查顺序：

1. 请用户保存工作后重启
2. 重启后看有没有自动弹出安装窗口；没有就重新运行安装命令

处置：

- ［2 级 先问用户］告诉用户重启；不要替用户重启电脑

- 必须先停下问用户：总是：重启要用户自己决定
- 确认恢复：重启后安装自动或手动继续，走完后 acornfox windows status 全部通过
- 相关编号：WIN-ADMIN-02

### WIN-DL-01 下载没成功（所有下载来源都试过）

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：WSL 安装包（约 351 MB）、Linux 镜像（Debian 约 85 MB）先从 AcornFox 下载网关取（分段多路、能续传），不通再用官方地址。每个来源都核对了写死的 SHA256。
- 下一步：确认能上网、关掉代理后重新运行；已经下好的部分会保留，不会从头来
- 现象：安装命令打印“没下成，换下一个来源”，最后带 WIN-DL-01 停下

可能原因（逐条判定）：

1. 这台电脑没有联网，或公司网络拦了下载地址 —— 判定：浏览器能不能打开 https://dl.acornfox.com/v1/health 和 https://github.com
2. 代理或安全软件拦了 curl.exe —— 判定：关掉代理软件后重试能过；日志 %LOCALAPPDATA%\AcornFox\logs\install-windows.log 里“segment … 失败 curl=…”的退出码
3. 下载网关今天的次数用完了或暂停发放（GW-LIM、GW-OFF） —— 判定：日志里 gw-client.log 的结果编号；这时会自动退到官方地址，只是更慢
4. 下载到的文件校验不对（下载源的文件被替换或损坏） —— 判定：日志里写“校验不对”；换一个来源后通过

排查顺序：

1. 看 install-windows.log 里最后的“segment … 失败”和“校验不对”
2. 关代理后重试
3. 加 -NoGateway 直接用官方地址试一次

处置：

- ［1 级 可以直接做］重新运行安装命令（从已下好的段接着下）
- ［1 级 可以直接做］加 -NoGateway 或换网络

- 必须先停下问用户：反复校验不对：停下，可能有人篡改了下载内容，带日志反馈，不要想办法跳过校验
- 确认恢复：下载完成，安装往下走
- 相关编号：WIN-DL-02、GW-LIM-01、GW-OFF-01

### WIN-DL-02 取不到这个版本的安装脚本，或校验不过

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：多半是下载被截断或被替换：换一个源再试（加 -China 或 -Global）；两个源都不过时不要强行安装，先反馈
- 现象：安装命令在“取 get.sh”这一步停下，带 WIN-DL-02

可能原因（逐条判定）：

1. 发行版地址连不上（Gitee、GitHub 都试过了） —— 判定：浏览器打开 https://gitee.com/VIP13390/AcornFox/releases 和 https://github.com/OrbitMaker/AcornFox/releases
2. 这个版本的 get.sh 和 SHA256SUMS 里记的值不一致 —— 判定：install-windows.log 里写“get.sh 校验不过（来自 …）”

排查顺序：

1. 换源重试：加 -China 或 -Global
2. 确认版本号存在

处置：

- ［1 级 可以直接做］重新运行安装命令，换一个源

- 必须先停下问用户：两个源都校验不过：带日志反馈，不要跳过校验
- 确认恢复：get.sh 校验通过，安装继续
- 相关编号：WIN-DL-01、INSTALL-GET-02、INSTALL-GET-03

### WIN-WSL-01 WSL 用不了

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：运行 wsl --status 看原因；多半是还没重启，或虚拟机平台没打开，或 WSL 被升级到一半
- 现象：装完 WSL 后 wsl --version 仍然不行；acornfox windows doctor 的“WSL”一项失败，带 WIN-WSL-01；acornfox 命令报“WSL 里的服务没有应答”

可能原因（逐条判定）：

1. 还没有重启（打开虚拟机平台后必须重启一次）［doctor 自动判定］ —— 判定：管理员那一步的记录里 dism 退出码 3010，之后没重启过
2. “虚拟机平台”功能没打开［doctor 自动判定］ —— 判定：doctor 的“虚拟机平台”写“没打开”
3. WSL 程序没装上或太旧（低于 2.0）［doctor 自动判定］ —— 判定：wsl --version 失败或版本低于 2.0
4. 策略禁止或虚拟化被关 —— 判定：doctor 里 WIN-POLICY-01、WIN-VIRT-01 的两项

排查顺序：

1. acornfox windows doctor
2. wsl --status
3. wsl --update（先问用户）

处置：

- ［2 级 先问用户］没重启的话请用户重启
- ［1 级 可以直接做］重新运行安装命令（会补装 WSL 或升级旧版）

- 必须先停下问用户：要用户重启时；wsl --update 之前（它会升级用户其它发行版用的 WSL）
- 确认恢复：wsl --version 成功，doctor 的“WSL”一项通过
- 相关编号：WIN-REBOOT-01、WIN-VIRT-01、WIN-POLICY-01

### WIN-WSL-02 导入 Linux 发行版失败

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：看 install-windows.log 里 wsl --import 的输出；磁盘空间、杀毒软件拦截、文件被占用是常见原因；解决后重新运行安装命令
- 现象：安装命令在“导入 Linux 发行版”这一步停下，带 WIN-WSL-02；doctor 的“发行版”一项写“没有发行版 AcornFox”

可能原因（逐条判定）：

1. 磁盘空间不够（装在 %LOCALAPPDATA%\AcornFox\wsl，要 2 GB 以上）［doctor 自动判定］ —— 判定：doctor 的系统盘剩余
2. 安全软件拦了 wsl.exe 写磁盘镜像 —— 判定：安全软件的拦截记录
3. 同名发行版没删干净、目录被占用 —— 判定：wsl -l -v 里有 AcornFox 但打不开；%LOCALAPPDATA%\AcornFox\wsl 里有 ext4.vhdx
4. 下载的镜像文件不完整 —— 判定：重新运行时会重新校验，校验不对会重下

排查顺序：

1. 看 install-windows.log 里 wsl --import rc=…
2. wsl -l -v
3. 重新运行

处置：

- ［1 级 可以直接做］重新运行安装命令
- ［2 级 先问用户］wsl --unregister AcornFox（删掉没装好的发行版，里面没有用户数据时才做）

- 必须先停下问用户：要注销发行版之前：先确认里面没有用户的应用和数据
- 确认恢复：wsl -l -v 里有 AcornFox，安装继续
- 相关编号：WIN-DISK-01、WIN-WSL-03

### WIN-WSL-03 Linux 发行版里的 systemd 没有起来

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：AcornFox 在 WSL 里用 systemd 管服务。安装脚本会写好 /etc/wsl.conf（启用 systemd）并重启发行版，等 systemd 起来；这一步等不到就停下。
- 下一步：运行 wsl --shutdown 后重新运行安装命令；还不行看 wsl -d AcornFox -u root -- systemctl status
- 现象：安装命令停在“等 systemd”，带 WIN-WSL-03；doctor 的“systemd”一项失败

可能原因（逐条判定）：

1. /etc/wsl.conf 没写成功或没生效（WSL 要整个关掉再起才读） —— 判定：wsl -d AcornFox -u root -- cat /etc/wsl.conf 里有 systemd=true；wsl --shutdown 后重试能过
2. WSL 版本太旧不支持 systemd —— 判定：wsl --version 低于 0.67.6
3. systemd 起来了但状态是 starting/stopping（有服务卡住） —— 判定：wsl -d AcornFox -u root -- systemctl list-jobs

排查顺序：

1. acornfox windows doctor
2. wsl --shutdown
3. wsl -d AcornFox -u root -- systemctl is-system-running
4. wsl -d AcornFox -u root -- systemctl --failed

处置：

- ［2 级 先问用户］wsl --shutdown 后重新运行安装命令（会关掉用户别的 WSL 发行版，先问用户）
- ［1 级 可以直接做］只关掉我们的：wsl --terminate AcornFox

- 必须先停下问用户：wsl --shutdown 之前：它会关掉用户所有 WSL 发行版
- 确认恢复：systemctl is-system-running 是 running 或 degraded
- 相关编号：WIN-WSL-01

### WIN-LINUX-01 在 WSL 里安装 AcornFox 没成功

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：Windows 这一侧的步骤做完了，在 WSL 里运行的 Linux 安装脚本（install.sh，经 get.sh 取这个版本自带的）没成功，或装好后服务没起来。脚本自己会打印它的故障编号。
- 下一步：看 WSL 里的 /root/acornfox-install.log 和它最后打印的故障编号（INSTALL-…、EDGE-… 等，acornfox faults 编号 能查）；解决后重新运行安装命令
- 现象：安装命令停在“在 AcornFox 里安装 AcornFox”之后，带 WIN-LINUX-01；doctor 的“AcornFox”一项失败

可能原因（逐条判定）：

1. WSL 里下载 Docker、AcornFox 或入口镜像失败（网络慢、代理） —— 判定：/root/acornfox-install.log 里的“下载没成功”和下载源测速那几行
2. Docker 没装上或起不来（WSL 里 iptables、systemd 的问题） —— 判定：wsl -d AcornFox -u root -- systemctl status docker
3. 端口被占（Windows 上的程序占了 18800、18802、18810 这些，WSL 的转发和它们冲突） —— 判定：install-windows.log；netstat -ano | findstr :188
4. 磁盘满了 —— 判定：doctor 的系统盘；wsl -d AcornFox -u root -- df -h /

排查顺序：

1. wsl -d AcornFox -u root -- tail -n 40 /root/acornfox-install.log
2. acornfox faults <日志里最后的编号>
3. acornfox windows doctor
4. 重新运行安装命令（做过的步骤会跳过）

处置：

- ［1 级 可以直接做］重新运行安装命令
- ［1 级 可以直接做］按日志里那个编号的条目处理

- 必须先停下问用户：同一处连续失败两次：带上 /root/acornfox-install.log 最后 40 行和 doctor 的输出反馈
- 确认恢复：acornfox windows doctor 的“AcornFox”一项通过，浏览器能打开控制台
- 相关编号：WIN-WSL-03、WIN-NET-01、INSTALL-DOCKER-01、INSTALL-PORT-03

### WIN-START-01 开机自启没登记成功，或 WSL 没设成一直开着

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：重新运行安装命令（要管理员确认一次），或运行 acornfox windows lan-on / lan-off 重新登记；没有 .wslconfig 里的设置时，没人用 WSL 一会儿它就会停
- 现象：重启电脑后 AcornFox 里的应用打不开，要自己运行一次 acornfox 命令才好；doctor 的“开机自启”或“.wslconfig”一项没通过，带 WIN-START-01

可能原因（逐条判定）：

1. 计划任务 AcornFoxWSL（或 AcornFoxLAN）不在了（被清理软件删了、管理员确认被取消）［doctor 自动判定］ —— 判定：schtasks /query /tn AcornFoxWSL；doctor 的“开机自启”
2. .wslconfig 里没有 instanceIdleTimeout、vmIdleTimeout 两项（被用户删了）［doctor 自动判定］ —— 判定：type %USERPROFILE%\.wslconfig；doctor 的“.wslconfig”
3. 用户把 WSL 的开机任务关掉了 —— 判定：任务计划程序里这个任务被禁用

排查顺序：

1. acornfox windows doctor
2. schtasks /query /tn AcornFoxWSL
3. type %USERPROFILE%\.wslconfig

处置：

- ［2 级 先问用户］重新运行安装命令补登记（要管理员确认一次）

- 必须先停下问用户：要弹管理员确认之前；改用户的 .wslconfig 之前（别的 WSL 发行版也受影响）
- 确认恢复：doctor 这两项通过；重启电脑后应用自己起来
- 相关编号：WIN-ADMIN-01

### WIN-LAN-01 局域网转发没生效

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：WSL 2 默认是 NAT 网络，局域网里的设备连不到它。AcornFoxLAN 任务每 5 秒对一次 WSL 里监听在所有网卡上的端口（局域网能打开的应用、18802 局域网控制台），用 netsh portproxy 把 Windows 的同一个端口转进去，再加 Windows 防火墙规则 AcornFox-LAN-WSL（只在专用、域网络放行）。
- 下一步：运行 acornfox windows doctor 看是哪一项；360 等安全软件常拦 netsh，弹窗里选“允许操作”；还不行运行 acornfox windows lan-off 再 lan-on 重来
- 现象：应用在这台电脑上能打开，局域网里的手机、电脑打不开；doctor 的“局域网访问”一项没通过，带 WIN-LAN-01

可能原因（逐条判定）：

1. AcornFoxLAN 任务没在运行［doctor 自动判定］ —— 判定：schtasks /query /tn AcornFoxLAN；%LOCALAPPDATA%\AcornFox\logs\lan-sync.log 最后几行
2. 安全软件拦了 netsh interface portproxy（360 弹窗默认“阻止”） —— 判定：netsh interface portproxy show v4tov4 里没有 18802；lan-sync.log 没有“转发”那几行
3. 转发规则指向了 WSL 旧的内部地址（WSL 重启后会换）［doctor 自动判定］ —— 判定：doctor 写“有 N 条转到旧地址”；AcornFoxLAN 任务不在运行时才会这样
4. 防火墙规则 AcornFox-LAN-WSL 不在［doctor 自动判定］ —— 判定：Get-NetFirewallRule -Name AcornFox-LAN-WSL
5. 应用是“只本机”（在 WSL 里只监听 127.0.0.1），本来就不会转出去 —— 判定：acornfox apps 里这个应用的访问范围；改成局域网：acornfox app set --app 名字 --expose lan

排查顺序：

1. acornfox windows doctor
2. schtasks /query /tn AcornFoxLAN
3. netsh interface portproxy show v4tov4
4. type %LOCALAPPDATA%\AcornFox\logs\lan-sync.log

处置：

- ［2 级 先问用户］acornfox windows lan-on（重新登记，要管理员确认一次）
- ［2 级 先问用户］请用户在安全软件的弹窗里选“允许”

- 必须先停下问用户：要弹管理员确认或请用户点安全软件的弹窗之前；改防火墙规则之前
- 确认恢复：doctor 的“局域网访问”通过；局域网里另一台设备能打开 http://这台电脑的局域网地址:18802/
- 相关编号：WIN-LAN-02、WIN-LAN-03

### WIN-LAN-02 当前账号不是管理员，局域网访问没打开

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：应用只在这台电脑上能打开。想让局域网里的设备访问：用管理员账号运行 acornfox windows lan-on
- 现象：安装时打印“当前账号不是管理员账号，局域网访问不打开”；局域网里的设备打不开这台电脑上的应用

可能原因（逐条判定）：

1. 安装时用的是普通账号：局域网转发要管理员权限常驻运行（每 5 秒刷新转发规则）［doctor 自动判定］ —— 判定：设置 → 账户里看账户类型；doctor 的“局域网访问”写“没打开”

排查顺序：

1. 问用户这台电脑有没有管理员账号

处置：

- ［0 级 只读］告诉用户这是这一版的限制，给出用管理员账号 lan-on 的办法

- 必须先停下问用户：总是：要输管理员密码只能用户自己输
- 确认恢复：管理员账号运行 acornfox windows lan-on 后 doctor 的“局域网访问”通过
- 相关编号：WIN-LAN-01

### WIN-LAN-03 这台电脑的网络类型是“公用”，防火墙会挡住局域网里的设备

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：在 Windows 设置里把这个网络改成“专用”（家里或办公室这类可信的网络），再试；公共场所的网络（咖啡店、机场）不要改
- 现象：局域网转发都正常，局域网里的设备仍然打不开；doctor 写“网络类型 Public”，带 WIN-LAN-03

可能原因（逐条判定）：

1. 网络类型是“公用网络”，AcornFox 的防火墙规则只在专用和域网络放行（有意不在公用网络放行）［doctor 自动判定］ —— 判定：Get-NetConnectionProfile 的 NetworkCategory 是 Public；doctor 的“局域网访问”

排查顺序：

1. acornfox windows doctor
2. 设置 → 网络和 Internet → 这个网络的属性 → 网络配置文件类型

处置：

- ［0 级 只读］告诉用户在设置里改成“专用网络”

- 必须先停下问用户：总是：改网络类型是用户的安全设置，只能他自己改
- 确认恢复：改完后局域网里的设备能打开应用
- 相关编号：WIN-LAN-01

### WIN-CLI-01 acornfox 命令没能写进去

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：也可以直接用 wsl -d AcornFox -u root -- acornfox …；重新运行安装命令会补写；新开一个 PowerShell 窗口让 PATH 生效
- 现象：在 PowerShell 里运行 acornfox 提示找不到命令；doctor 的“acornfox 命令”一项没通过，带 WIN-CLI-01

可能原因（逐条判定）：

1. 窗口是装之前打开的，PATH 还没刷新 —— 判定：新开一个 PowerShell 窗口运行 acornfox version 能过
2. %LOCALAPPDATA%\AcornFox\bin 里的 acornfox.cmd、acornfox.ps1 被清理软件删了［doctor 自动判定］ —— 判定：dir %LOCALAPPDATA%\AcornFox\bin
3. 用户 PATH 里没有这个目录［doctor 自动判定］ —— 判定：doctor 的“acornfox 命令”

排查顺序：

1. 新开窗口试
2. acornfox windows doctor
3. 重新运行安装命令

处置：

- ［1 级 可以直接做］重新运行安装命令补写（不弹管理员确认）

- 必须先停下问用户：不用问
- 确认恢复：新窗口里 acornfox version 能运行

### WIN-NET-01 控制台打不开（localhost 上没有应答）

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：运行 acornfox windows doctor；多半是 WSL 里的服务没起来：wsl -d AcornFox -u root -- systemctl status acornfox-server
- 现象：浏览器打开 http://localhost:18800/ 或 :18802 没有应答；安装最后一步打开控制台失败，带 WIN-NET-01

可能原因（逐条判定）：

1. WSL 被停了（没人用时 WSL 会停，.wslconfig 的“一直开着”没生效）［doctor 自动判定］ —— 判定：wsl -l -v 里 AcornFox 是 Stopped；doctor 的“发行版”
2. acornfox-server 没起来［doctor 自动判定］ —— 判定：wsl -d AcornFox -u root -- systemctl status acornfox-server；doctor 的“AcornFox”
3. WSL 的 localhost 转发被关了（.wslconfig 里 localhostForwarding=false） —— 判定：type %USERPROFILE%\.wslconfig
4. Windows 上有别的程序占了 18800 或 18802 —— 判定：netstat -ano | findstr :18800

排查顺序：

1. acornfox windows doctor
2. wsl -l -v
3. wsl -d AcornFox -u root -- systemctl status acornfox-server --no-pager
4. type %USERPROFILE%\.wslconfig

处置：

- ［1 级 可以直接做］wsl -d AcornFox --exec /bin/true（把发行版叫起来）后再试
- ［2 级 先问用户］改 .wslconfig 里的 localhostForwarding

- 必须先停下问用户：改用户的 .wslconfig 之前；wsl --shutdown 之前（会关掉用户别的发行版）
- 确认恢复：浏览器能打开 http://localhost:18800/，doctor 全部通过
- 相关编号：WIN-WSL-03、WIN-START-01、WIN-LINUX-01

### WIN-UNINST-01 卸载要确认，但这里没有终端可以问

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：确定要卸载就加 -Yes（acornfox windows uninstall --yes）；加 --purge 会连应用数据和发行版一起删，不可恢复，只在用户明确说要全部删掉时加
- 现象：uninstall -Purge 在没有终端时停下，带 WIN-UNINST-01，什么都没删

可能原因（逐条判定）：

1. -Purge 会删掉全部应用数据，需要确认；没有终端（AI 工作台、计划任务）没法问 —— 判定：命令里有 -Purge 但没有 -Yes

排查顺序：

1. 问用户要不要连数据一起删
2. 要就加 --yes 重新运行

处置：

- ［2 级 先问用户］用户明确确认后加 --yes 重新运行

- 必须先停下问用户：总是：删数据不可恢复，必须先问用户并说清楚会删什么
- 确认恢复：卸载完成；acornfox windows status 显示没有发行版
- 演练方法：在测试机上不带终端运行 acornfox windows uninstall --purge（不加 --yes）

### WIN-UNINST-02 WSL 里的 AcornFox 没卸干净，卸载停下了

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：卸载先在 WSL 里运行本版本自带的 uninstall.sh（经 get.sh 取并校验，要联网），再回头检查 acornfox-server、acornfox-runner 已经停了、没有 AcornFox 的容器（含入口容器）在跑。这两样有一样不成，就停在这里：Windows 这一侧（acornfox 命令、计划任务、局域网转发）原样留着，不会说“已卸载”，应用数据没动。
- 下一步：先看 %LOCALAPPDATA%\AcornFox\logs\uninstall-wsl.log 最后几行和它报的编号（INSTALL-GET-…、UNINSTALL-…，acornfox faults 编号 能查）；确认这台电脑能上网后，用屏幕上给的完整路径命令重新运行卸载
- 现象：acornfox windows uninstall 带 WIN-UNINST-02、退出码 1，说“WSL 里的卸载脚本退出码 …”或“回头检查：服务 … 还在运行”；acornfox 命令还在，WSL 里 acornfox-server 还是 active

可能原因（逐条判定）：

1. 没联网，取不到卸载脚本（INSTALL-GET-01、INSTALL-GET-02、WIN-DL-02） —— 判定：uninstall-wsl.log 里有 INSTALL-GET-…；curl.exe -I https://gitee.com
2. 卸载脚本自己出错（UNINSTALL-… 编号） —— 判定：uninstall-wsl.log 最后的“故障编号:”
3. 脚本报成功但服务或容器还在（docker 停不下来、systemd 没响应） —— 判定：wsl -d AcornFox -u root -- systemctl is-active acornfox-server；wsl -d AcornFox -u root -- docker ps

排查顺序：

1. 读 %LOCALAPPDATA%\AcornFox\logs\uninstall-wsl.log 的最后 30 行
2. acornfox faults <日志里最后的编号>
3. 能上网后重新运行卸载（写完整路径：& "%LOCALAPPDATA%\AcornFox\bin\acornfox.cmd" windows uninstall）

处置：

- ［1 级 可以直接做］确认能上网后重新运行 acornfox windows uninstall（数据不动，重复运行不出错）
- ［0 级 只读］同一处连续失败两次，带上 uninstall-wsl.log 最后 40 行反馈

- 必须先停下问用户：同一处连续失败两次：不要改用别的办法硬删（例如 wsl --unregister 会连数据一起删），先问用户
- 确认恢复：acornfox windows uninstall 说“已卸载（保留数据）”；wsl -d AcornFox -u root -- systemctl is-active acornfox-server 不是 active，docker ps 里没有 acornfox 的容器
- 相关编号：WIN-UNINST-03、WIN-DL-02、INSTALL-GET-02
- 演练方法：在测试机上先断开网络（或把 ACORNFOX_RELEASE_BASE 指到不存在的地址），运行 acornfox windows uninstall --yes

### WIN-UNINST-03 -Purge 没能注销 WSL 发行版，数据还在

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：uninstall -Purge 要先注销 WSL 发行版 AcornFox（连里面的数据一起删），注销没成功就停下：Windows 这一侧原样留着，不会说“已卸载”。
- 下一步：运行 wsl --shutdown 关掉所有 WSL（会关掉用户别的发行版，先问用户），再用屏幕上给的完整路径命令重新运行卸载
- 现象：acornfox windows uninstall --purge --yes 带 WIN-UNINST-03、退出码 1，说“注销发行版没成功”

可能原因（逐条判定）：

1. 发行版还被别的窗口或程序占着（有人开着 wsl -d AcornFox 的终端） —— 判定：wsl --list --running
2. WSL 本身出了问题（服务没响应） —— 判定：wsl --status；wsl --list --verbose

排查顺序：

1. wsl --list --verbose
2. 关掉用户开着的 AcornFox 终端
3. wsl --terminate AcornFox 后重新运行卸载

处置：

- ［2 级 先问用户］wsl --terminate AcornFox（只停这一个发行版）后重新运行卸载
- ［2 级 先问用户］wsl --shutdown（会关掉用户别的发行版）

- 必须先停下问用户：总是：这是删数据的一步，重试前再向用户确认一次；wsl --shutdown 之前先问
- 确认恢复：wsl --list 里没有 AcornFox；acornfox windows uninstall 说“已卸载，连同发行版、数据…都删掉了”
- 相关编号：WIN-UNINST-01、WIN-UNINST-02、WIN-WSL-01

### WIN-SKILL-01 Windows 用户目录里已装的 AcornFox Skill 和 WSL 里的程序不是同一个版本

- 分组：Windows 安装（install-windows.ps1）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：Windows 上的 Skill 装在用户目录（%USERPROFILE%\.claude\skills\acornfox、%USERPROFILE%\.codex\skills\acornfox），程序在 WSL 里；升级后 Skill 不会自己更新。acornfox windows doctor 对照 Skill 文件头里的版本号和 WSL 里程序的版本号，不一样就提醒。意思和 SETUP-SKILL-01 一样，只是 Windows 一侧自己查。只是提醒，不影响应用。
- 下一步：运行 acornfox skill install 更新（它把 WSL 里程序带的 Skill 装进 Windows 用户目录）；升级 AcornFox 之后都要做一次
- AcornFox 已经自动做的：没有；Skill 不会自己更新
- 现象：acornfox windows doctor（或 acornfox doctor 的 Windows 一侧）有一行“注意”带 WIN-SKILL-01

可能原因（逐条判定）：

1. 升级过 AcornFox（acornfox windows upgrade），没有重新装 Skill［doctor 自动判定］ —— 判定：文件头 <!-- acornfox-skill-version: … --> 的版本比 acornfox version 的低
2. Skill 是没有版本号的老版本写的［doctor 自动判定］ —— 判定：SKILL.md 里没有 acornfox-skill-version 那一行

排查顺序：

1. acornfox windows doctor
2. acornfox version
3. acornfox skill install

处置：

- ［1 级 可以直接做］acornfox skill install（只覆盖 Skill 文件）

- 必须先停下问用户：不用问
- 确认恢复：acornfox windows doctor 的“Skill”一项通过
- 相关编号：SETUP-SKILL-01
- 演练方法：在测试机上 acornfox skill install 后把 SKILL.md 里的版本号改成 0.0.1，再运行 acornfox windows doctor

## Mac 安装（acornfox mac，install-mac.sh）

### MAC-OS-01 这不是 Mac，或 macOS 版本太旧

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：Mac 要 macOS 26 或更新才能用苹果的 container；先在“系统设置 → 通用 → 软件更新”升级再重新运行安装命令
- 现象：安装命令在检查这一步停下，带 MAC-OS-01，什么都没装

可能原因（逐条判定）：

1. macOS 版本低于 26［doctor 自动判定］ —— 判定：sw_vers -productVersion 的第一段小于 26；acornfox mac doctor 的“macOS”一项
2. 在 Linux 或别的系统上运行了 Mac 的安装脚本，或用 sudo、root 运行（会装到 root 的家目录里） —— 判定：uname -s 不是 Darwin；id -u 是 0

排查顺序：

1. sw_vers -productVersion
2. acornfox mac doctor（装了以后）

处置：

- ［0 级 只读］告诉用户先升级 macOS（升级要用户自己点）
- ［1 级 可以直接做］用 sudo 运行的：去掉 sudo 重新运行

- 必须先停下问用户：总是：升级系统要用户自己做
- 确认恢复：重新运行安装命令，检查通过

### MAC-ARCH-01 这台 Mac 不是苹果芯片

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：Intel 的 Mac 装不了（苹果的 container 只在苹果芯片上运行）；可以把 AcornFox 装在一台 Linux 服务器上，用这台 Mac 的命令行管理（get.sh cli，再 acornfox target add）
- 现象：安装命令在检查这一步停下，带 MAC-ARCH-01

可能原因（逐条判定）：

1. 这是 Intel 芯片的 Mac［doctor 自动判定］ —— 判定：uname -m 显示 x86_64
2. 终端是用 Rosetta 打开的（uname -m 显示 x86_64，但 sysctl -n machdep.cpu.brand_string 是 Apple） —— 判定：sysctl -n machdep.cpu.brand_string 里有 Apple：关掉“使用 Rosetta 打开”再运行

排查顺序：

1. uname -m
2. sysctl -n machdep.cpu.brand_string

处置：

- ［0 级 只读］照实告诉用户这台 Mac 暂时不支持，给出装在 Linux 服务器上的办法

- 必须先停下问用户：总是
- 确认恢复：换到苹果芯片的 Mac 后检查通过

### MAC-DISK-01 磁盘剩余空间不够

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：清理磁盘，至少留 12 GB 再运行安装命令（苹果的 container 约 0.5 GB、虚拟机镜像约 1 GB，Docker 和应用另算）
- 现象：安装命令在检查这一步停下，带 MAC-DISK-01

可能原因（逐条判定）：

1. 用户家目录所在的磁盘剩余空间小于 12 GB［doctor 自动判定］ —— 判定：df -h ~ 的可用空间；acornfox mac doctor 的“磁盘剩”
2. 虚拟机的磁盘随用随涨，装了很多应用后磁盘快满 —— 判定：container system df 看镜像、卷占了多少

排查顺序：

1. df -h ~
2. container system df

处置：

- ［0 级 只读］告诉用户清理的办法，不要替他删文件

- 必须先停下问用户：总是：删用户的文件要他自己决定
- 确认恢复：重新运行安装命令，检查通过

### MAC-DL-01 下载安装需要的大文件失败

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：网络不好：过一会儿重新运行安装命令（已下的部分会接着下）；国内加 --china，海外加 --global 换一个先试的源
- 现象：安装命令在下载苹果的 container 安装包（约 170 MB）或虚拟机内核（约 676 MB）时停下，带 MAC-DL-01，后面列出每个来源失败的原因

可能原因（逐条判定）：

1. 没联网，或公司网络拦了下载网关和 GitHub［doctor 自动判定］ —— 判定：浏览器能不能打开 https://dl.acornfox.com/v1/health 和 https://github.com
2. 家用宽带每条连接很慢（网关的文件每条连接约 30 KB/s，所以分段多路下载） —— 判定：安装输出里的“N 路”和已下字节数在涨就是慢，不是坏
3. 文件校验不过（下载被截断或被替换）：程序已丢掉重下 —— 判定：失败原因里写“sha256 不一致”

排查顺序：

1. acornfox mac install --plan
2. curl -I https://dl.acornfox.com/v1/health
3. 过一会儿再运行安装命令

处置：

- ［1 级 可以直接做］重新运行安装命令（接着下）
- ［1 级 可以直接做］换源：加 --china 或 --global

- 必须先停下问用户：同一个文件连续三次失败：带安装输出最后 30 行反馈
- 确认恢复：安装命令跑过这一步

### MAC-DL-02 取不到这个版本的安装文件

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：核对版本号存在（https://gitee.com/VIP13390/AcornFox/releases）；网络不通时过一会儿再试，或加 --china、--global 换一个先试的源
- 现象：install-mac.sh 或 acornfox mac install 在下载 acornfox_darwin_arm64、SHA256SUMS、install.sh 等文件时停下，带 MAC-DL-02

可能原因（逐条判定）：

1. 两个源（Gitee、GitHub）都连不上［doctor 自动判定］ —— 判定：curl -I https://gitee.com 和 curl -I https://github.com
2. 版本号写错，或这个版本的发行版不完整（SHA256SUMS 里没有这一项） —— 判定：发行版页面里有没有 acornfox_darwin_arm64、acornfox_linux_arm64、install.sh 和入口包
3. 校验不过：下载被截断或被替换，没有运行它 —— 判定：屏幕上写“校验不过”和两个值

排查顺序：

1. curl -I https://gitee.com
2. curl -I https://github.com
3. 核对版本号

处置：

- ［1 级 可以直接做］重新运行
- ［1 级 可以直接做］换源：加 --china 或 --global

- 必须先停下问用户：两个源都校验不过时不要强行安装：带屏幕输出反馈
- 确认恢复：install-mac.sh 通过校验，进入 acornfox mac install

### MAC-SRC-01 这是源码里的脚本或程序，没有版本号

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：用 get.sh 安装（它取带版本号的发行版脚本），或加 --version vX.Y.Z、--local-package 目录
- 现象：install-mac.sh 或 acornfox mac install 停下，带 MAC-SRC-01

可能原因（逐条判定）：

1. 直接运行了仓库里的 install-mac.sh（版本戳是 dev），或自己编的程序没有写版本号 —— 判定：install-mac.sh 里 SCRIPT_VERSION 是 dev；acornfox version 显示 dev

排查顺序：

1. acornfox version
2. grep SCRIPT_VERSION install-mac.sh

处置：

- ［0 级 只读］改用 get.sh 的一行命令

- 必须先停下问用户：总是
- 确认恢复：用 get.sh 重新运行

### MAC-ADMIN-01 要管理员密码，但没输成

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：装苹果签名的 container 安装包要管理员权限：请你自己在“终端”里运行屏幕上给的那一行 sudo installer …（输入密码时屏幕不显示字符，是正常的），装好后重新运行安装命令
- 这一项要用户自己确认或操作
- 现象：安装命令停在“安装苹果的 container”这一步，退出码 5（没有终端可以输密码）或 1（密码输错、取消、这个账号不是管理员）

可能原因（逐条判定）：

1. 安装命令是 AI 工作台或脚本运行的，没有终端能让人输密码［doctor 自动判定］ —— 判定：屏幕上写“这里没有终端”；退出码是 5
2. 密码输错或取消 —— 判定：sudo 报 Sorry, try again 或 installer 没有运行
3. 这个账号不是管理员 —— 判定：“系统设置 → 用户与群组”里这个账号有没有“允许用户管理这台电脑”

排查顺序：

1. 让用户自己在终端运行屏幕上那一行 sudo installer -pkg … -target /
2. container --version 看装上了没有
3. 重新运行安装命令

处置：

- ［0 级 只读］把那一行命令和原因告诉用户，请他自己在终端里运行

- 必须先停下问用户：总是：AI 工作台不代输管理员密码
- 确认恢复：container --version 有输出后重新运行安装命令，这一步显示已做过

### MAC-CTR-01 苹果的 container 装了但用不了

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：运行 container system start 看报错；还不行就重新运行安装命令，或到 https://github.com/apple/container 看有没有已知问题
- 现象：安装命令在“苹果的 container”或“container 服务”这一步停下，带 MAC-CTR-01；acornfox mac doctor 的 container 一项失败

可能原因（逐条判定）：

1. container 服务没启动（重启电脑后、或被手动停了）［doctor 自动判定］ —— 判定：container system status 不是 running
2. 安装包没装全，container 程序不在 /usr/local/bin —— 判定：which container 没有输出；ls /usr/local/bin/container
3. macOS 太旧或和这个 container 版本不兼容 —— 判定：container system start 的报错里写版本要求

排查顺序：

1. container --version
2. container system status
3. container system start
4. acornfox mac doctor

处置：

- ［1 级 可以直接做］运行 container system start
- ［1 级 可以直接做］重新运行安装命令

- 必须先停下问用户：container system start 报同一个错两次：带报错原文反馈
- 确认恢复：container system status 显示 running

### MAC-KERNEL-01 虚拟机内核没装上

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：重新运行安装命令（会重新下载并装内核）；内核约 676 MB，经下载网关分段下载，慢的话请耐心等
- 现象：安装命令在“虚拟机内核”这一步停下，带 MAC-KERNEL-01；doctor 报没有默认内核

可能原因（逐条判定）：

1. container system kernel set 失败（文件没下全、磁盘满、container 服务没启动）［doctor 自动判定］ —— 判定：屏幕上 container system kernel set 的退出码和第一行报错
2. 默认内核文件被删了 —— 判定：ls ~/Library/Application\ Support/com.apple.container/kernels/ 里没有 default.kernel-arm64

排查顺序：

1. acornfox mac doctor
2. container system status
3. 重新运行安装命令

处置：

- ［1 级 可以直接做］重新运行安装命令

- 必须先停下问用户：同一处连续失败两次：带屏幕输出反馈
- 确认恢复：安装命令跑过“虚拟机内核”这一步

### MAC-IMG-01 虚拟机用的镜像准备不好

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：重新运行安装命令（已下的根文件系统会直接用）；还不行带屏幕输出反馈
- 现象：安装命令在“虚拟机初始化镜像”或“Debian 虚拟机镜像”这一步停下，带 MAC-IMG-01

可能原因（逐条判定）：

1. 拉不下初始化镜像 vminit（ghcr.io 在国内很慢，程序会先试南京大学的镜像站）［doctor 自动判定］ —— 判定：屏幕上“拉取 …”那几行和失败原因
2. Debian 根文件系统下不来（清华、中科大、linuxcontainers 都不通） —— 判定：屏幕上写“Debian 根文件系统下不来”和每个来源的原因
3. 打包或载入镜像失败（磁盘满、系统自带的 tar 不能用） —— 判定：屏幕上 container image load 的退出码和报错

排查顺序：

1. acornfox mac install --plan
2. container image ls
3. df -h ~
4. 重新运行安装命令

处置：

- ［1 级 可以直接做］重新运行安装命令

- 必须先停下问用户：同一处连续失败两次：带屏幕输出反馈
- 确认恢复：container image inspect acornfox-base:debian13 有输出

### MAC-VM-01 建不了虚拟机

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：看屏幕上的原因：同名虚拟机已存在就换名字（环境变量 ACORNFOX_MAC_VM）或先自己处理；其余重新运行安装命令
- 现象：安装命令在“虚拟机”这一步停下，带 MAC-VM-01；acornfox mac doctor 说虚拟机不存在

可能原因（逐条判定）：

1. 已经有一台同名的虚拟机，不是这个安装留下的（为免弄坏你的东西，不动它）［doctor 自动判定］ —— 判定：container machine ls 里有一台叫 acornfox 的，而 ~/Library/Application Support/AcornFox/mac/mac.json 不存在
2. container machine create 失败（内存或磁盘不够、镜像坏了） —— 判定：屏幕上 container machine create 的退出码和报错
3. 虚拟机被卸载或手动删过 —— 判定：container machine ls 里没有它；mac.json 在

排查顺序：

1. container machine ls
2. acornfox mac doctor
3. acornfox mac install --plan

处置：

- ［1 级 可以直接做］重新运行安装命令（虚拟机不存在会重建）

- 必须先停下问用户：要删或改一台不是这个安装建的虚拟机：先问用户
- 确认恢复：container machine ls 里有 acornfox 且 running

### MAC-VM-02 虚拟机起不来或没有应答

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：acornfox mac stop 再 acornfox mac start；还不行 container system stop 再 container system start（会停掉你别的容器，先问用户）
- 现象：acornfox mac start 或有人访问时一直等不到虚拟机；日志里写“虚拟机没有应答”

可能原因（逐条判定）：

1. container 服务没在运行［doctor 自动判定］ —— 判定：container system status
2. 内存不够，Mac 把它挤掉了 —— 判定：活动监视器里内存压力是红的；acornfox mac set --memory 4 调小
3. 虚拟机的系统起不来（磁盘坏了、镜像不对） —— 判定：container machine logs acornfox 看最后几十行

排查顺序：

1. acornfox mac status
2. container machine ls
3. container machine logs acornfox
4. acornfox mac doctor --wake

处置：

- ［1 级 可以直接做］acornfox mac stop 再 acornfox mac start
- ［2 级 先问用户］container system stop 再 start（会停别的容器）

- 必须先停下问用户：重启两次都没起来：带 container machine logs 的最后 50 行反馈
- 确认恢复：acornfox mac status 显示虚拟机在运行并有地址

### MAC-PROV-01 虚拟机里的准备没做成

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：重新运行安装命令；软件源连不上时程序会自己换源再试
- 现象：安装命令在“虚拟机里的准备（软件源、ssh）”这一步停下，带 MAC-PROV-01

可能原因（逐条判定）：

1. 虚拟机里的软件源连不上（国内用阿里云、海外用 Debian 官方）［doctor 自动判定］ —— 判定：屏幕上 apt-get 的报错
2. 读不到 sshd 的主机钥匙（sshd 没装上） —— 判定：屏幕上写“读不到虚拟机 sshd 的主机钥匙”

排查顺序：

1. acornfox mac doctor --wake
2. 重新运行安装命令

处置：

- ［1 级 可以直接做］重新运行安装命令

- 必须先停下问用户：同一处连续失败两次：带屏幕输出反馈
- 确认恢复：安装命令跑过这一步

### MAC-LINUX-01 虚拟机里装 AcornFox 没成功

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：屏幕上写了虚拟机里脚本最后报的故障编号（INSTALL-…、EDGE-… 等）：acornfox faults 那个编号看办法；整个输出也记在虚拟机里的 /root/acornfox-install.log
- 现象：安装命令在“虚拟机里的 AcornFox”这一步停下，带 MAC-LINUX-01，后面常有“脚本报 INSTALL-…”

可能原因（逐条判定）：

1. 虚拟机里装 Docker 或拉入口镜像没成功（和 Linux 服务器上同一个脚本，同样的原因）［doctor 自动判定］ —— 判定：屏幕上最后的故障编号；container machine run -n acornfox --root -- tail -60 /root/acornfox-install.log
2. 文件传进虚拟机后校验不对 —— 判定：屏幕上写“传进虚拟机后校验不对”

排查顺序：

1. 看屏幕上最后的故障编号
2. acornfox faults 那个编号
3. 重新运行安装命令（脚本能重复运行）

处置：

- ［1 级 可以直接做］重新运行安装命令

- 必须先停下问用户：同一个编号连续两次：带那个编号和 /root/acornfox-install.log 最后 60 行反馈
- 确认恢复：acornfox mac doctor --wake 里虚拟机里的服务都正常

### MAC-LAUNCH-01 常驻转发程序没在运行

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：重新运行安装命令会装回去；手动：launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.acornfox.mac.plist；看日志 ~/Library/Application Support/AcornFox/mac/logs/daemon.log
- 现象：控制台、应用、acornfox 命令都连不上，但虚拟机在；acornfox mac status 说常驻程序没加载或 Mac 上的端口没人听

可能原因（逐条判定）：

1. 没有登录（常驻程序是登录后才运行的）或被用户手动卸了［doctor 自动判定］ —— 判定：launchctl print gui/$(id -u)/com.acornfox.mac 失败
2. 程序文件被删或换了位置 —— 判定：ls ~/Library/Application\ Support/AcornFox/mac/bin/acornfox
3. Mac 上的端口被别的程序占了（日志里有 MAC-PORT-01） —— 判定：daemon.log 里 MAC-PORT-01 那几行

排查顺序：

1. acornfox mac status
2. acornfox mac doctor
3. tail -30 ~/Library/Application\ Support/AcornFox/mac/logs/daemon.log
4. 重新运行安装命令

处置：

- ［1 级 可以直接做］重新运行安装命令（只会补装缺的部分）

- 必须先停下问用户：重装后仍然没有：带 daemon.log 最后 30 行反馈
- 确认恢复：acornfox mac status 显示常驻程序已加载、端口在听

### MAC-PORT-01 Mac 上的端口被别的程序占了

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：找出占用的程序（lsof -nP -iTCP:端口 -sTCP:LISTEN）退出它，或换一个 AcornFox 要用的端口；占着的端口转发程序不会转发
- 现象：daemon.log 里写 MAC-PORT-01，对应的应用或控制台在 Mac 上打不开（虚拟机里是好的）

可能原因（逐条判定）：

1. Mac 上别的程序（另一份 AcornFox、开发服务器等）占了 18802、18808 或 18810～18899 里的端口［doctor 自动判定］ —— 判定：lsof -nP -iTCP:18802 -sTCP:LISTEN（换成日志里写的端口）
2. macOS 的防火墙或安全软件不让监听局域网（只影响局域网访问，本机的 127.0.0.1 不受影响） —— 判定：本机用 127.0.0.1 能开、局域网别的设备打不开

排查顺序：

1. tail -30 daemon.log
2. lsof -nP -iTCP:端口 -sTCP:LISTEN

处置：

- ［0 级 只读］告诉用户哪个程序占了端口，请他决定

- 必须先停下问用户：要结束别的程序：先问用户
- 确认恢复：占用的程序退出后，转发程序在 15 秒内自己补上监听

### MAC-NET-01 这台 Mac 的局域网地址变了

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：acornfox mac set --public-host auto（把应用地址里写的地址改成现在的；虚拟机里的 AcornFox 服务会重启一下，应用不停）
- 现象：控制台和应用页上显示的地址（http://192.168.x.x:…）打不开，但把地址换成 127.0.0.1 就能开；acornfox mac doctor 报 MAC-NET-01

可能原因（逐条判定）：

1. 路由器重新分配了这台 Mac 的地址，或换了网络（Wi-Fi 换成网线）［doctor 自动判定］ —— 判定：ifconfig en0 | grep inet 和 acornfox mac status 里的局域网地址不一样

排查顺序：

1. acornfox mac doctor
2. acornfox mac set --public-host auto

处置：

- ［1 级 可以直接做］运行 acornfox mac set --public-host auto

- 必须先停下问用户：总是可以直接做：只改显示用的地址，不动数据和应用
- 确认恢复：acornfox mac doctor 不再报 MAC-NET-01

### MAC-SSH-01 acornfox 命令连不上虚拟机

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：acornfox mac doctor 看原因；多半是转发程序没运行（MAC-LAUNCH-01）或虚拟机刚起来还没好，等十几秒再试
- 现象：acornfox target add local 或 acornfox status 报连接失败（stage connect），或安装最后一步停下

可能原因（逐条判定）：

1. 转发程序没运行，Mac 上 18808 没人听［doctor 自动判定］ —— 判定：acornfox mac status 的“本机转发 ssh 端口”
2. 虚拟机里的 sshd 没起来，或钥匙对不上（虚拟机被重建过） —— 判定：acornfox mac doctor --wake；ssh -F /dev/null -i ~/Library/Application\ Support/AcornFox/mac/ssh/id_ed25519 -o UserKnownHostsFile=~/Library/Application\ Support/AcornFox/mac/ssh/known_hosts -p 18808 root@127.0.0.1 true
3. known_hosts 里的主机钥匙和虚拟机的不一致（重建过虚拟机） —— 判定：ssh 报 Host key verification failed

排查顺序：

1. acornfox mac status
2. acornfox mac doctor --wake
3. 重新运行安装命令（会重新放钥匙、重新登记 local）

处置：

- ［1 级 可以直接做］重新运行安装命令

- 必须先停下问用户：重装后仍连不上：带 acornfox mac doctor --json 的输出反馈
- 确认恢复：acornfox status 有回答

### MAC-CLI-01 放不下 acornfox 命令

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：换一个目录：ACORNFOX_INSTALL_DIR=某个可写目录 重新运行安装命令；或自己把 ~/Library/Application Support/AcornFox/mac/bin/acornfox 拷到 PATH 里
- 现象：安装命令在“acornfox 命令”这一步停下，带 MAC-CLI-01（别的步骤都已经做完，控制台能用）

可能原因（逐条判定）：

1. /usr/local/bin 和 ~/.local/bin 都不能写［doctor 自动判定］ —— 判定：ls -ld /usr/local/bin ~/.local/bin

排查顺序：

1. echo $PATH
2. ls -ld /usr/local/bin ~/.local/bin

处置：

- ［1 级 可以直接做］用 ACORNFOX_INSTALL_DIR 指定别的目录重新运行

- 必须先停下问用户：总是：改用户的 PATH 配置（~/.zshrc）先问
- 确认恢复：which acornfox 有输出

### MAC-UNINST-01 --purge 没有确认

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：--purge 会连虚拟机里的应用和数据一起删掉，删了就找不回来；确认要删请加 --yes
- 现象：acornfox mac uninstall --purge 停下，带 MAC-UNINST-01，什么都没删

可能原因（逐条判定）：

1. 没有加 --yes［doctor 自动判定］ —— 判定：命令里没有 --yes

排查顺序：

1. acornfox mac uninstall --purge --plan（先看会删什么）

处置：

- ［2 级 先问用户］先问用户是否真的要删数据；他同意再加 --yes

- 必须先停下问用户：总是：删数据要用户确认
- 确认恢复：加 --yes 后 acornfox mac status 说没有装

### MAC-UNINST-02 卸载没卸干净

- 分组：Mac 安装（acornfox mac，install-mac.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：看屏幕上哪一步失败：停不掉常驻程序用 launchctl bootout gui/$(id -u)/com.acornfox.mac；停不下虚拟机用 container machine stop acornfox；然后重新运行卸载命令
- 现象：acornfox mac uninstall 停下，带 MAC-UNINST-02

可能原因（逐条判定）：

1. launchctl 或 container 命令失败（服务没响应）［doctor 自动判定］ —— 判定：屏幕上那一步的报错
2. 删目录失败（有程序占着） —— 判定：屏幕上写“删不掉 …”

排查顺序：

1. acornfox mac status
2. launchctl print gui/$(id -u)/com.acornfox.mac
3. container machine ls
4. 重新运行卸载命令

处置：

- ［1 级 可以直接做］重新运行卸载命令（可重复运行）

- 必须先停下问用户：重试两次仍不行：带屏幕输出反馈
- 确认恢复：acornfox mac status 说没有装，或虚拟机已停

## 准备运行环境和本机自检（doctor）

### SETUP-NET-01 下载源都连不上

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：确认能上网后点“重试”（或运行 acornfox setup --resume）；国内网络可以换下载源：acornfox setup --resume --mirror <地址>
- 现象：准备运行环境停在下载那一步，带 SETUP-NET-01；自检“镜像下载源可用”一项没通过

可能原因（逐条判定）：

1. 这台电脑没有联网 —— 判定：浏览器打不开任何网站
2. 开着代理或公司网络拦了下载地址 —— 判定：关掉代理后重试能过
3. DNS 解析失败 —— 判定：nslookup 下载地址没有结果

排查顺序：

1. 确认能上网
2. 关掉代理再试
3. 国内网络换下载源

处置：

- ［1 级 可以直接做］acornfox setup --resume 重试
- ［1 级 可以直接做］换下载源 --mirror

- 必须先停下问用户：网络要改代理或公司网络设置时
- 确认恢复：准备运行环境走过下载那一步

### SETUP-NET-02 下载 45 秒没有进展，所有下载源都试过了

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：已下载的部分会保留，点“重试”从断点接着下；一直不行就换下载源或关掉代理再试
- AcornFox 已经自动做的：已经依次换过全部下载源；已下载的部分留着
- 现象：准备运行环境的下载停住，带 SETUP-NET-02

可能原因（逐条判定）：

1. 网络暂时不通或很慢 —— 判定：过几分钟重试能接着下
2. 代理软件拦截了连接 —— 判定：关掉代理后能下
3. 下载源临时故障 —— 判定：换 --mirror 后能下

排查顺序：

1. 重试一次
2. 关代理
3. 换下载源

处置：

- ［1 级 可以直接做］acornfox setup --resume

- 必须先停下问用户：换了下载源还是不动时，带上证据包
- 确认恢复：下载完成，进入下一步

### SETUP-SUM-01 下载的文件校验不通过

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：已删掉坏文件，点“重试”会重新下载；换一个下载源更稳妥。反复出现请把诊断信息发给我们
- AcornFox 已经自动做的：坏文件已删掉，没有用它
- 现象：准备运行环境报 SETUP-SUM-01

可能原因（逐条判定）：

1. 下载源上的文件被替换或损坏 —— 判定：换下载源后通过
2. 下载过程中数据出错 —— 判定：同一下载源重试一次后通过

排查顺序：

1. 重试一次
2. 换下载源

处置：

- ［1 级 可以直接做］acornfox setup --resume
- ［2 级 先问用户］跳过校验——绝不

- 必须先停下问用户：反复校验失败时：停下，可能有人篡改了下载内容，带上证据包反馈
- 确认恢复：下载并校验通过

### SETUP-RT-01 Docker 装了但没有运行，需要你选用哪个运行环境

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：在页面上选“用 AcornFox 自带的运行环境”或“打开 Docker Desktop”；终端：acornfox setup --runtime builtin
- 这一项要用户自己确认或操作
- 现象：准备运行环境等你选择；doctor 报 SETUP-RT-01

可能原因（逐条判定）：

1. Docker Desktop 没有打开 —— 判定：菜单栏或任务栏没有 Docker 图标

排查顺序：

1. 问用户想用哪一个

处置：

- ［2 级 先问用户］按用户的选择运行 acornfox setup --runtime builtin 或打开 Docker Desktop

- 必须先停下问用户：总是：由用户选
- 确认恢复：doctor 的 Docker 一项通过

### SETUP-RT-02 自带的运行环境没能启动

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：点“重试”；还不行运行 acornfox doctor 看具体哪一项，再把诊断信息发给我们
- 现象：准备运行环境停在运行环境那一步，带 SETUP-RT-02

可能原因（逐条判定）：

1. 虚拟机镜像不完整 —— 判定：重试时会重新校验，再次失败
2. 磁盘空间不够（至少要 10 GB） —— 判定：doctor 的磁盘一项
3. 上次没有正常关闭，虚拟机还占着 —— 判定：重启电脑后重试能过

排查顺序：

1. 重试
2. acornfox doctor

处置：

- ［1 级 可以直接做］acornfox setup --resume

- 必须先停下问用户：重试后仍失败：带上证据包
- 确认恢复：doctor 的 Docker 一项通过

### SETUP-RT-03 连不上 Docker

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：Linux 服务器用 install.sh 安装会自动装好 Docker；已经装了就 sudo systemctl start docker 后重试
- 现象：doctor 的 Docker 一项不通过（SETUP-RT-03）；部署、安装应用都失败

可能原因（逐条判定）：

1. Linux 上 Docker 服务没有运行（systemctl status docker）［doctor 自动判定］ —— 判定：sudo systemctl status docker --no-pager 不是 active
2. 当前用户没有 Docker 权限，而且连不上 acornfox-server（连得上、执行器正常时 doctor 判通过：Docker 由后台服务访问） —— 判定：用 sudo acornfox doctor 再查一次能过
3. DOCKER_HOST 指向了不存在的地址 —— 判定：echo $DOCKER_HOST 不为空

排查顺序：

1. sudo systemctl status docker --no-pager
2. 不是 root 时用 sudo acornfox doctor 再查

处置：

- ［1 级 可以直接做］sudo systemctl start docker
- ［2 级 先问用户］重装或改 Docker 配置

- 必须先停下问用户：Docker 起不来、要改它的配置时
- 确认恢复：sudo acornfox doctor 的 Docker 一项通过
- 相关编号：SETUP-SVC-01
- 演练方法：在测试机上 sudo systemctl stop docker docker.socket 后运行 sudo acornfox doctor

### SETUP-RT-04 Docker Desktop 没有按时启动

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：看一眼 Docker Desktop 的窗口，处理完它的提示后点“重试”；或者改用 AcornFox 自带的运行环境
- 现象：准备运行环境等 Docker Desktop 超时，带 SETUP-RT-04

可能原因（逐条判定）：

1. Docker Desktop 第一次启动要接受协议或登录 —— 判定：Docker Desktop 窗口里有协议或登录页
2. 它正在更新 —— 判定：窗口显示正在更新
3. WSL 2（Windows）没有装好 —— 判定：wsl --status 报错

排查顺序：

1. 看 Docker Desktop 的窗口

处置：

- ［2 级 先问用户］用户处理完 Docker Desktop 的提示后重试

- 必须先停下问用户：要用户在 Docker Desktop 里点同意或登录时
- 确认恢复：doctor 的 Docker 一项通过

### SETUP-EDGE-01 网关组件没有准备好

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：点“重试”；端口被占时运行 acornfox doctor 看是哪个端口，关掉占用的程序后再试
- 现象：doctor 或运行环境页的“网关”一项不通过；应用网址打不开

可能原因（逐条判定）：

1. 入口镜像没准备好（看上一条下载记录；故障编号 EDGE-02） —— 判定：sudo docker image ls acornfox/edge 为空
2. 端口 18807 或 18810–18859 被别的程序占了［doctor 自动判定］ —— 判定：doctor 的端口一项（SETUP-PORT-02、SETUP-PORT-03）
3. Docker 里旧的入口容器卡住 —— 判定：sudo docker ps -a --filter label=acornfox.edge=1 状态不是 Up

排查顺序：

1. sudo acornfox doctor
2. sudo docker logs --tail 50 acornfox-edge

处置：

- ［1 级 可以直接做］sudo systemctl restart acornfox-server（会重建网关容器，应用容器不停）
- ［2 级 先问用户］停掉占用端口的程序

- 必须先停下问用户：要停别的程序时；重启后仍不行时带上证据包
- 确认恢复：doctor 的网关一项通过，应用网址能打开
- 相关编号：SETUP-PORT-02、SETUP-PORT-03、EDGE-01、EDGE-02

### SETUP-CHK-01 自检没有通过

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：按没通过那一项的说明处理后点“重试”
- 现象：准备运行环境的最后一步自检报 SETUP-CHK-01

可能原因（逐条判定）：

1. 见自检列表里没打勾的那一项 —— 判定：acornfox setup --status 或页面上看自检列表

排查顺序：

1. 找到没通过的那一项，按它自己的编号处理

处置：

- ［1 级 可以直接做］按那一项的编号处理后重试

- 必须先停下问用户：那一项要用户操作时
- 确认恢复：自检全部打勾

### SETUP-CHK-02 Docker 不能创建和删除容器

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：清理 Docker 空间（docker system prune）或重启运行环境后重试
- 现象：自检“能创建和删除容器”一项没通过

可能原因（逐条判定）：

1. Docker 磁盘满了 —— 判定：sudo docker system df；df -h
2. Docker 守护进程出错 —— 判定：sudo journalctl -u docker -n 50 --no-pager

排查顺序：

1. df -h 和 sudo docker system df
2. journalctl -u docker

处置：

- ［2 级 先问用户］docker system prune（会删停掉的容器和没用的镜像）
- ［2 级 先问用户］重启 Docker

- 必须先停下问用户：清理或重启 Docker 之前
- 确认恢复：自检通过
- 相关编号：SETUP-DISK-01

### SETUP-CHK-03 网关管理口不安全或不应答

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：运行 acornfox doctor 查看；关掉占用 18807 的程序后重试
- 现象：自检“网关管理口”一项没通过

可能原因（逐条判定）：

1. 18807 端口被别的程序占用［doctor 自动判定］ —— 判定：doctor 的 SETUP-PORT-02
2. 入口容器没有运行 —— 判定：sudo docker ps --filter label=acornfox.edge=1（EDGE-01）

排查顺序：

1. acornfox doctor

处置：

- ［2 级 先问用户］停掉占用 18807 的程序

- 必须先停下问用户：停程序之前
- 确认恢复：自检通过
- 相关编号：SETUP-PORT-02

### SETUP-CHK-04 局域网打不开

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：只在本机用可以忽略。要在局域网打开：Windows 上运行 acornfox setup --confirm-admin 加防火墙规则；网络是公用的话，到 设置 → 网络 把它改成“专用网络”；然后重试
- 现象：doctor 的“局域网访问”或“局域网地址”一项不通过或警告

可能原因（逐条判定）：

1. 没有连接局域网（只有本机能打开，不影响使用） —— 判定：doctor 写“没取到局域网地址”
2. Windows：没有 AcornFox 防火墙规则（准备时选了“只在本机用”，或者管理员确认被取消） —— 判定：Windows 防火墙规则里没有 AcornFox
3. Windows：当前网络是“公用网络”，防火墙规则只在专用网络生效 —— 判定：设置 → 网络 里网络类型
4. 防火墙拦了应用端口 —— 判定：别的设备连 局域网地址:18810 超时
5. 运行环境只绑定了 127.0.0.1 —— 判定：doctor 写“只在 127.0.0.1 能连”

排查顺序：

1. acornfox doctor 看局域网两项

处置：

- ［0 级 只读］只在本机用时忽略
- ［2 级 先问用户］加防火墙规则、改网络类型

- 必须先停下问用户：改防火墙和网络设置之前
- 确认恢复：doctor 的局域网访问一项通过

### SETUP-CHK-05 局域网转发没有启动

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：运行 acornfox doctor 看是哪些端口被占；关掉占用的程序，过 10 秒再看
- AcornFox 已经自动做的：换了网络时 10 秒内自动重试
- 现象：自检“局域网转发”一项没通过

可能原因（逐条判定）：

1. 局域网地址上的应用端口被别的程序占用了 —— 判定：doctor 的端口一项
2. 刚换了网络，转发还没跟上（10 秒内会自动重试） —— 判定：过 10 秒再看就好了

排查顺序：

1. 等 10 秒
2. acornfox doctor

处置：

- ［2 级 先问用户］停掉占用端口的程序

- 必须先停下问用户：停程序之前
- 确认恢复：自检通过

### SETUP-SVC-01 后台服务 acornfox-server 没有应答

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：运行 sudo systemctl restart acornfox-runner acornfox-server，再运行 sudo acornfox doctor；还不行就把 journalctl -u acornfox-server -n 50 的输出交给用户
- 现象：doctor 的后台服务一项不通过；命令行报 server_down 或 local_server_down；控制台打不开

可能原因（逐条判定）：

1. Docker 重启或升级后，acornfox-server、acornfox-runner 跟着停了，没有自动起来［doctor 自动判定］ —— 判定：systemctl is-active acornfox-server acornfox-runner 不是 active
2. acornfox-runner 停了或连不上 Docker（doctor 会写“执行器 acornfox-runner 没有应答”）［doctor 自动判定］ —— 判定：doctor 的后台服务一项写“执行器没有应答”
3. 服务启动失败（看 journalctl -u acornfox-server -n 50） —— 判定：journalctl -u acornfox-server -n 50 --no-pager 里有报错
4. 用的不是 root，也不在 acornfox-users 组里，连不上服务（这时只是警告）［doctor 自动判定］ —— 判定：doctor 写“没有权限连接 acornfox-server”，用 sudo 再查能过

排查顺序：

1. systemctl is-active acornfox-server acornfox-runner docker
2. sudo acornfox doctor
3. journalctl -u acornfox-server -n 50 --no-pager

处置：

- ［1 级 可以直接做］sudo systemctl restart acornfox-runner acornfox-server（命令行、控制台断几秒，应用容器不停）
- ［0 级 只读］重启后还不行：只收集日志

- 必须先停下问用户：重启一次还不行时：带上 journalctl 的输出和证据包
- 确认恢复：sudo acornfox doctor 的后台服务一项通过
- 相关编号：SETUP-RT-03
- 演练方法：在测试机上 sudo systemctl stop acornfox-server 后运行 sudo acornfox doctor

### SETUP-DISK-01 磁盘剩余空间不够（少于 10 GB 警告，少于 3 GB 不通过）

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：doctor 会写出是数据目录还是 Docker 数据目录所在的分区。先运行 docker system df 看占用，确认不需要后 docker system prune 清理（会删掉停掉的容器和没用的镜像，先问用户）；还不够就在云厂商控制台扩容磁盘
- 现象：doctor 或运行环境页的磁盘一项警告或不通过；拉镜像、构建、安装应用失败

可能原因（逐条判定）：

1. 系统盘本来就小（20 GB 的盘装完系统和 Docker 后所剩不多）［doctor 自动判定］ —— 判定：df -h 看总大小
2. 旧的镜像、构建缓存、停掉的容器占了空间 —— 判定：sudo docker system df 的 RECLAIMABLE 一列
3. 应用的数据或日志越写越多 —— 判定：sudo docker system df -v 看数据卷大小

排查顺序：

1. sudo acornfox doctor 看是哪个分区
2. sudo docker system df

处置：

- ［1 级 可以直接做］sudo apt-get clean 清掉软件包缓存
- ［2 级 先问用户］清理旧系统日志（sudo journalctl --vacuum-size=200M，删掉的日志找不回来）
- ［2 级 先问用户］docker system prune（删停掉的容器和没用的镜像）
- ［2 级 先问用户］扩容磁盘（花钱）

- 必须先停下问用户：清理 Docker、删数据、扩容之前
- 确认恢复：doctor 的磁盘一项通过
- 相关编号：CAT-INST-27、INSTALL-DISK-01

### SETUP-PORT-01 端口 80 或 443 被别的程序占用

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：和宝塔、1Panel、nginx 等共存时（安装用了 --allow-busy-80-443）不用处理，doctor 只给警告：应用照常用 http://服务器IP:端口 访问，不要停用户在用的程序；要用域名就在原来的面板里反向代理到 127.0.0.1:应用端口。doctor 判不通过，说明 AcornFox 里绑定了域名或开了公网控制台：先问用户占用的程序还要不要。不要了才停掉它（例如 sudo systemctl disable --now nginx），再运行 sudo systemctl restart acornfox-server；要保留就删掉 AcornFox 里绑定的域名（acornfox domain remove 域名）或关掉公网控制台（acornfox public-console disable）
- 这一项要用户自己确认或操作
- 现象：doctor 的端口一项警告（共存）或不通过（绑了域名、开了公网控制台）

可能原因（逐条判定）：

1. 服务器上原来装了 nginx、Apache、宝塔、1Panel 或别的反向代理［doctor 自动判定］ —— 判定：sudo ss -ltnp | grep -E ':(80|443) '
2. 别的 Docker 容器映射了 80 或 443［doctor 自动判定］ —— 判定：docker ps --format '{{.Names}} {{.Ports}}'

排查顺序：

1. sudo acornfox doctor 看是警告还是不通过
2. 问用户占用的程序还要不要

处置：

- ［0 级 只读］共存时不用处理
- ［2 级 先问用户］停掉占用的程序，或删域名、关公网控制台

- 必须先停下问用户：不通过时总要问用户：留哪一个
- 确认恢复：doctor 的端口一项通过或只剩共存警告
- 相关编号：INSTALL-PORT-02

### SETUP-PORT-02 网关管理口 18807 被别的程序占用

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：按 doctor 写出的 PID 确认是什么程序，停掉后运行 sudo systemctl restart acornfox-server；应用的网址和域名都依赖这个端口
- 现象：doctor 的端口一项不通过，写“网关管理口 …”；应用网址、域名配置不上

可能原因（逐条判定）：

1. 别的程序或容器占了 18807［doctor 自动判定］ —— 判定：doctor 写出的 PID：ps -fp PID
2. 旧版留下的进程还没退出 —— 判定：ps -fp PID 是旧的 acornfox，或旧的入口容器里的进程

排查顺序：

1. sudo acornfox doctor
2. ps -fp PID

处置：

- ［2 级 先问用户］停掉占用的程序
- ［1 级 可以直接做］sudo systemctl restart acornfox-server

- 必须先停下问用户：停程序之前
- 确认恢复：doctor 的端口一项通过
- 相关编号：INSTALL-PORT-01

### SETUP-PORT-03 应用端口段 18810–18899 里有端口被别的程序占用

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：应用的端口在创建时分好、以后不变，分到被占端口的应用会一直上线失败（删掉重建往往还会分到同一个端口）。按 doctor 写出的端口和 PID 找到占用的程序，先问用户能不能停掉；能停就停掉后重新部署，不能停就把情况告诉用户：这些端口上的应用上线不了
- 这一项要用户自己确认或操作
- 现象：doctor 的端口一项不通过，列出应用端口段里被占的端口

可能原因（逐条判定）：

1. 别的程序或 Docker 容器正好用了这一段里的端口［doctor 自动判定］ —— 判定：doctor 写出的端口和 PID

排查顺序：

1. sudo acornfox doctor
2. ps -fp PID

处置：

- ［2 级 先问用户］停掉占用的程序后 acornfox redeploy 应用名

- 必须先停下问用户：总是
- 确认恢复：doctor 的端口一项通过，应用上线
- 相关编号：INSTALL-PORT-03

### SETUP-PORT-04 局域网控制台端口 18802 被别的程序占用

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不开局域网控制台可以忽略；要开的话先停掉占用的程序再运行 acornfox lan-console enable
- 现象：doctor 的端口一项警告“局域网控制台端口 …”

可能原因（逐条判定）：

1. 别的程序或容器占了 18802［doctor 自动判定］ —— 判定：doctor 写出的 PID

排查顺序：

1. 问用户用不用局域网控制台

处置：

- ［0 级 只读］不用就忽略
- ［2 级 先问用户］停掉占用的程序

- 必须先停下问用户：停程序之前
- 确认恢复：doctor 没有这条警告

### SETUP-PORT-05 没有权限查看是哪个程序占用了端口

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：普通用户运行时出现是正常的：后台服务正常时，AcornFox 自己的端口（18807、应用端口、开着的局域网控制台端口）已经认出，不在这里；剩下的要确认就用 sudo acornfox doctor 再查一次
- 现象：doctor 或运行环境页的端口一项警告“… 有程序在监听，… 看不到是谁占用”

可能原因（逐条判定）：

1. doctor 不是用 root 运行的，看不到别的用户的进程［doctor 自动判定］ —— 判定：用 sudo acornfox doctor 再查，这条消失或变成具体的编号

排查顺序：

1. sudo acornfox doctor

处置：

- ［0 级 只读］用 sudo 再查一次

- 必须先停下问用户：—（不用问）
- 确认恢复：sudo acornfox doctor 没有这条，或给出了具体的端口编号

### SETUP-CLOCK-01 本机时钟不准，或者没开网络对时

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：先问用户，再运行 sudo timedatectl set-ntp true 打开网络对时，过一两分钟用 timedatectl 看 System clock synchronized 是否为 yes，然后再运行 acornfox doctor；一直同步不上的云主机，按云厂商文档把 chrony 或 systemd-timesyncd 改成内网时间服务器。时钟和下载网关差 300 秒以上时，下载网关会拒绝这台机器（GW-AUTH-01）：应用中心的镜像改从公共源拉，私有目录连不上；AcornFox 会按网关时间自动重试一次，但还是要把时钟校准
- AcornFox 已经自动做的：签名请求时会按网关的时间自动校正一次（只改签名用的时间，不改系统时间）
- 这一项要用户自己确认或操作
- 现象：doctor 或运行环境页的时钟一项警告或不通过；应用中心安装时进度里写网关拒绝（GW-AUTH-01）；私有目录连不上

可能原因（逐条判定）：

1. 没开网络对时（timedatectl 里 NTP service 是 inactive）［doctor 自动判定］ —— 判定：timedatectl 的 NTP service 是 inactive
2. 开了网络对时但连不上时间服务器（有的云主机屏蔽了外网的时间服务器，要改用云厂商内网的）［doctor 自动判定］ —— 判定：timedatectl 的 System clock synchronized 一直是 no
3. 虚拟机暂停过或宿主机休眠后，时钟没追上［doctor 自动判定］ —— 判定：doctor 写出和下载网关差了多少秒
4. 有人手动改过系统时间 —— 判定：date 和手机上的时间差很多

排查顺序：

1. timedatectl
2. sudo acornfox doctor 看和网关差多少秒

处置：

- ［2 级 先问用户］sudo timedatectl set-ntp true（改系统设置）
- ［2 级 先问用户］改时间服务器配置

- 必须先停下问用户：改系统时间设置之前
- 确认恢复：timedatectl 显示 synchronized: yes，doctor 的时钟一项通过
- 相关编号：GW-AUTH-01
- 演练方法：在测试机上 sudo timedatectl set-ntp false; sudo date -s '+10 min' 后运行 sudo acornfox doctor

### SETUP-MAC-01 Mac 版的 install、serve 不能直接装本机服务

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：Mac 版只有命令行客户端，acornfox install、serve 不能在 Mac 上直接装本机服务；要把 AcornFox 装在这台 Mac 上（苹果芯片、macOS 26 起），运行：curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash。1. 要装在这台 Mac 上：运行上面那行命令（装进苹果 container 开的一台 Debian 虚拟机），或先运行 acornfox mac install --plan 看会做什么；2. 要管理 Linux 服务器：在服务器上按使用文档用 install.sh 安装 AcornFox，再在这台 Mac 上运行 acornfox target add 名称 --ssh 用户@服务器地址；3. 之后 acornfox deploy、status、logs 等命令都作用在登记的服务器上。检查服务器用 ssh 用户@服务器地址 sudo acornfox doctor
- 这一项要用户自己确认或操作
- 现象：在 Mac 上运行 acornfox install、serve 时报 SETUP-MAC-01；还没装 Mac 本机版时运行 acornfox doctor 也报它

可能原因（逐条判定）：

1. acornfox install、serve 是旧的“本机直接装”模式，在 Mac 上已关闭；Mac 本机版是用 get.sh 装进苹果 container 的虚拟机（acornfox mac install） —— 判定：uname 是 Darwin；acornfox mac status 说没有装

排查顺序：

1. 问用户想装在这台 Mac 上还是管理一台 Linux 服务器
2. 装在这台 Mac 上：acornfox mac install --plan（苹果芯片、macOS 26 起）
3. 管理服务器：acornfox target add

处置：

- ［2 级 先问用户］装在这台 Mac 上：运行 get.sh 的一行命令
- ［2 级 先问用户］在 Linux 服务器上安装后，用 acornfox target add 登记

- 必须先停下问用户：要用户准备服务器（可能花钱）时；装在这台 Mac 上要输管理员密码时
- 确认恢复：acornfox --target 服务器 status 能返回

### SETUP-GEN-01 准备运行环境时出错

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：点“重试”；还不行运行 acornfox doctor，把诊断信息发给我们
- 现象：准备运行环境报 SETUP-GEN-01

可能原因（逐条判定）：

1. 见错误信息 —— 判定：acornfox setup --status 看 message

排查顺序：

1. acornfox setup --status
2. acornfox doctor

处置：

- ［1 级 可以直接做］acornfox setup --resume

- 必须先停下问用户：重试仍失败：带上证据包
- 确认恢复：准备运行环境完成

### SETUP-SYNC-01 应用目录上次同步失败，或者很久没同步

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：后台服务启动时同步一次应用目录，之后每 6 小时（上下抖一点）同步一次；有人打开控制台的应用中心或“我的应用”、或运行 acornfox apps outdated 时，距上次成功同步超过 30 分钟也会同步一次。doctor 和控制台“运行环境”页读的是后台服务记下的上次结果：什么时候、成没成、从哪个来源、在用第几号目录。
- 下一步：运行 acornfox catalog sync 看这次的结果；同步不上不影响已装的应用，安装时用手上的目录（缓存或程序内置的）
- AcornFox 已经自动做的：所有来源都失败时继续用手上的目录；下次定时同步会再试
- 现象：doctor 或运行环境页的“应用目录”一项警告（SETUP-SYNC-01）；应用中心看不到新上架的应用或新版本

可能原因（逐条判定）：

1. 这台服务器连不上目录的来源（官网和 Gitee 备用都不通，或被代理、防火墙拦了）［doctor 自动判定］ —— 判定：acornfox catalog sync --json 的 attempts 里每个来源都是 CAT-SYNC-02
2. 来源返回的内容验不过签名或版本不对（来源正在发布、镜像太旧） —— 判定：attempts 里是 CAT-SYNC-04、CAT-SYNC-05、CAT-SYNC-06
3. 后台服务很久没运行（停过一天以上），定时同步没跑［doctor 自动判定］ —— 判定：doctor 写“上次同步是 … 前”，systemctl status acornfox-server 看启动时间
4. 私有目录那部分同步失败（网关不通或授权被撤销） —— 判定：acornfox catalog sources 里私有目录的 code
5. 后台服务刚启动，还没同步过 —— 判定：doctor 写“还没同步过”，过几分钟再看

排查顺序：

1. sudo acornfox doctor 看“应用目录”那一行
2. acornfox catalog sync --json 看每个来源的结果
3. acornfox catalog sources 看私有目录

处置：

- ［1 级 可以直接做］acornfox catalog sync（只读取目录，可重复）
- ［2 级 先问用户］改代理或防火墙让服务器能访问目录来源

- 必须先停下问用户：要改网络设置时
- 确认恢复：acornfox catalog sync 返回“目录已是最新”或“已更新”，doctor 的应用目录一项通过
- 相关编号：CAT-SYNC-02、CAT-SYNC-08、CAT-SYNC-10
- 演练方法：在测试机里把目录来源的域名指到 127.0.0.1（/etc/hosts）后运行 acornfox catalog sync，再运行 sudo acornfox doctor

### SETUP-GW-01 下载网关连不上或应答不对

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：下载网关给应用中心的大镜像和私有目录发临时下载地址。doctor 和控制台“运行环境”页向它的 /v1/health 发一个不带内容的 HEAD 请求（限 5 秒，不带安装编号和签名），看能不能连上、应答是不是 200、和本机时钟差多少。
- 下一步：公共目录的应用照常能装（镜像改从公共源拉，会慢一些），私有目录连不上、装不了。先看网络和代理，再看 ACORNFOX_GATEWAY 或 network.json 里的 gateway 有没有写错
- AcornFox 已经自动做的：安装应用时网关不通会自动跳过网关，改从公共源拉镜像（私有目录的应用除外）
- 现象：doctor 或运行环境页的“下载网关”一项警告（SETUP-GW-01）；应用中心安装进度里写“下载网关失败，改从公共源”；连接私有目录报 CAT-CONN-07；不加 sudo 运行 doctor 时这一项通过、写“读不到这台服务器的网关设置……没查”：不是故障，但也没查网关（普通用户读不到数据目录），要查用 sudo acornfox doctor

可能原因（逐条判定）：

1. 这台服务器连不上网关（网络不通、DNS 解析不了、代理或防火墙拦了）［doctor 自动判定］ —— 判定：doctor 写“连不上 …”和原因；curl -sS -m 5 <网关地址>/v1/health
2. 网关地址写错了（ACORNFOX_GATEWAY 环境变量，或数据目录 network.json 的 gateway）［doctor 自动判定］ —— 判定：doctor 写出的地址不是 https://dl.acornfox.com，也不是你们自己配的网关
3. 地址指到了别的网站，或者中间有代理改了应答［doctor 自动判定］ —— 判定：doctor 写“应答不对（HTTP …）”
4. 网关在维护，或暂停发放 —— 判定：doctor 写“应答不对（HTTP 503）”；curl -sS -m 5 <网关地址>/v1/health 的 issuing 是 false

排查顺序：

1. sudo acornfox doctor 看“下载网关”那一行写的地址和原因
2. curl -sS -m 5 <网关地址>/v1/health（正常是 {"ok":true,…,"issuing":true}）
3. 检查 ACORNFOX_GATEWAY 和 network.json

处置：

- ［0 级 只读］只用公共目录时可以先不处理（应用照常装，慢一些）
- ［2 级 先问用户］改网关地址、代理或防火墙
- ［2 级 先问用户］关掉下载网关：/var/lib/acornfox/network.json 平时没有，没有时 echo '{"gateway": "off"}' | sudo tee /var/lib/acornfox/network.json（已经有的话不要整份覆盖，在里面加 "gateway": "off" 这一项），sudo chown acornfox /var/lib/acornfox/network.json，再 sudo systemctl restart acornfox-server

- 必须先停下问用户：改网络设置、改或关网关地址之前
- 确认恢复：doctor 的下载网关一项通过（写“能连上”）
- 相关编号：GW-NET-01、GW-RESP-01、GW-OFF-01、SETUP-CLOCK-01、CAT-CONN-07
- 演练方法：在测试机里 echo '127.0.0.1 dl.acornfox.com' | sudo tee -a /etc/hosts 后运行 sudo acornfox doctor；或者 sudo ACORNFOX_GATEWAY=http://127.0.0.1:9 acornfox doctor

### SETUP-NET-03 有镜像下载源不能用或很慢（拉镜像时已跳过或排到后面），或 Docker 的镜像加速配置里有不能用的地址

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：镜像下载源分层（我们自己的源、所在云的内网加速、别人公开的国内副本、上游自己），清单在程序里（acornfox sources 能看到）。doctor 对每个源取一次镜像清单和一段镜像层测快慢；另外逐个看 /etc/docker/daemon.json 里 registry-mirrors 的地址通不通。
- 下一步：只是提醒：平台拉应用镜像时会自动跳过不能用的源、把慢的排到后面。要看每个源现在怎样运行 acornfox sources --probe；daemon.json 里不能用的加速地址只影响自己 docker pull 和构建项目，要去掉先问用户
- AcornFox 已经自动做的：拉镜像前会先对每个源实测一次：拒绝访问、没有这个镜像、限流、连不上的跳过，慢的排到后面；拉的过程中太慢会自动换下一个源
- 现象：sudo acornfox doctor 有一行“!”开头、带 SETUP-NET-03 的提醒；acornfox sources --probe 里有源写着拒绝访问、很慢、连不上

可能原因（逐条判定）：

1. 某个公共镜像站对这台机器所在的网络拒绝访问（例如对所有请求回 403）［doctor 自动判定］ —— 判定：acornfox sources --probe 里它写“拒绝访问（403）”
2. 某个公共镜像站把大的镜像层转到境外下载，慢［doctor 自动判定］ —— 判定：acornfox sources --probe 里它写“慢（… MB/s，镜像层被转到 …）”
3. 镜像站临时故障或限流［doctor 自动判定］ —— 判定：过一会儿再 acornfox sources --probe 结果变了
4. /etc/docker/daemon.json 的 registry-mirrors 里有已经不能用的地址（例如老版本装的时候写进去的） —— 判定：cat /etc/docker/daemon.json；doctor 写出是哪个地址

排查顺序：

1. sudo acornfox doctor
2. acornfox sources --probe
3. cat /etc/docker/daemon.json

处置：

- ［0 级 只读］不处理：拉镜像时会自动跳过或排后
- ［2 级 先问用户］从 daemon.json 的 registry-mirrors 里去掉不能用的地址，再 sudo systemctl restart docker（改 Docker 配置，会重启所有容器）
- ［2 级 先问用户］加一个自己的镜像加速地址（ACORNFOX_REGISTRY_MIRRORS，或写进 daemon.json）

- 必须先停下问用户：改 daemon.json、重启 Docker 之前
- 确认恢复：acornfox sources --probe 里排在前面的源写“能用”；doctor 不再有这条提醒
- 相关编号：SETUP-NET-01、CAT-INST-12、pull_timeout
- 演练方法：在测试机里 echo '127.0.0.1 docker.m.daocloud.io' | sudo tee -a /etc/hosts 后运行 sudo acornfox doctor 和 acornfox sources --probe

### SETUP-SKILL-01 电脑上已装的 AcornFox Skill 和命令行不是同一个版本

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：AI 工作台读的 Skill 是这台电脑上的一份文件（~/.claude/skills/acornfox、~/.codex/skills/acornfox），Skill 分入口 SKILL.md 和一批分册（install-linux.md 等），都在同一个目录里；命令行写它们的时候在每个文件头带上自己的版本号。命令行升级后文件不会自己更新，AI 工作台就按旧的做事；doctor 对照每个文件头的版本号和命令行的版本号，不一样、或者缺了哪一册，就提醒。只是提醒，不影响应用和服务器。
- 下一步：运行 acornfox skill install 更新 Skill；如果提示 Skill 比命令行新，先更新电脑上的命令行（get.sh 的 cli 那条命令，见 Skill 里 maintain.md 的“更新 Skill 和命令行”），再运行 acornfox skill install
- AcornFox 已经自动做的：没有；Skill 和命令行都不会自己更新
- 现象：acornfox doctor 有一行“!”开头、带 SETUP-SKILL-01：“… 里的 AcornFox Skill 是 … 版，这台电脑上的命令行是 … 版”；doctor --json 的 items 里 check 为 SETUP-SKILL-01、level 为 warn

可能原因（逐条判定）：

1. 命令行升级过（或服务器升级后重装了命令行），Skill 还是旧的［doctor 自动判定］ —— 判定：head -3 ~/.claude/skills/acornfox/SKILL.md 里的版本号比 acornfox --json version 的低
2. Skill 是没有版本号的老版本写的［doctor 自动判定］ —— 判定：SKILL.md 文件头没有 acornfox-skill-version 那一行
3. 分册没装全：入口是新的，但目录里缺某一册，或某一册不是同一个版本（例如手工拷过文件、安装时被打断）［doctor 自动判定］ —— 判定：doctor 的提醒点名了哪几个分册；ls ~/.claude/skills/acornfox/ 对照 acornfox skill print --all 里的文件名
4. Claude Code 和 Codex 两处只更新了一处［doctor 自动判定］ —— 判定：doctor 的提醒点名是哪个工作台
5. Skill 比命令行新：命令行是旧的（在这台电脑上装命令行之后，Skill 是从别处拷来的或另一个更新的命令行装的）［doctor 自动判定］ —— 判定：doctor 的提醒写“比这台电脑上的命令行新”

排查顺序：

1. acornfox doctor --json 找 check 为 SETUP-SKILL-01 的项
2. acornfox --json version
3. head -3 ~/.claude/skills/acornfox/SKILL.md ~/.codex/skills/acornfox/SKILL.md
4. ls ~/.claude/skills/acornfox/ ~/.codex/skills/acornfox/

处置：

- ［1 级 可以直接做］acornfox skill install（只覆盖 Skill 目录里的文件，旧版本留下的分册会一并删掉）
- ［2 级 先问用户］更新电脑上的命令行（get.sh 的 cli 那条命令，换掉电脑上的程序）

- 必须先停下问用户：要更新命令行之前（它换掉电脑上的程序）
- 确认恢复：acornfox doctor 里 SETUP-SKILL-01 那一行是“都是 … 版”
- 相关编号：CLI-UPDATE-01
- 演练方法：在测试机上 acornfox skill install 后，把 ~/.claude/skills/acornfox/SKILL.md 里的版本号改成 0.0.1，再运行 acornfox doctor

## 入口代理（应用网址和域名前面的入口容器）

### EDGE-01 入口代理没有在运行

- 分组：入口代理（应用网址和域名前面的入口容器）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：所有应用的网址和绑定的域名都经过入口代理（容器 acornfox-edge）。它停了，应用容器还在跑，但从外面都打不开。
- 下一步：运行 sudo acornfox doctor 看入口代理一项；容器停了就 sudo systemctl restart acornfox-server（会重建或拉起入口容器，应用容器不停）
- 现象：doctor 的“入口代理”一项不通过，写“容器 acornfox-edge 没有在运行”；所有应用网址都打不开，应用容器还在运行；后台服务日志里有“入口容器启动失败”

可能原因（逐条判定）：

1. 入口容器被手动停了或删了［doctor 自动判定］ —— 判定：sudo docker ps -a --filter label=acornfox.edge=1 看状态
2. 入口容器起不来（端口被别的程序占了、Docker 出错）［doctor 自动判定］ —— 判定：sudo docker logs --tail 50 acornfox-edge；doctor 的端口一项（SETUP-PORT-02、SETUP-PORT-03）
3. 本机没有入口镜像，建不了容器［doctor 自动判定］ —— 判定：doctor 同时报 EDGE-02

排查顺序：

1. sudo acornfox doctor
2. sudo docker ps -a --filter label=acornfox.edge=1
3. sudo docker logs --tail 50 acornfox-edge
4. sudo journalctl -u acornfox-server -n 50 --no-pager

处置：

- ［1 级 可以直接做］sudo systemctl restart acornfox-server（重建或拉起入口容器，应用容器不停）
- ［2 级 先问用户］停掉占用端口的程序

- 必须先停下问用户：要停别的程序时；重启后 doctor 仍报 EDGE-01 时带上证据包
- 确认恢复：doctor 的入口代理一项通过，应用网址能打开
- 相关编号：EDGE-02、SETUP-PORT-02、SETUP-PORT-03
- 演练方法：在测试机上 sudo docker stop acornfox-edge 后运行 sudo acornfox doctor

### EDGE-02 入口镜像缺失或导入失败

- 分组：入口代理（应用网址和域名前面的入口容器）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：入口镜像 acornfox/edge:<入口版本> 随发行版的入口包（acornfox-edge_<入口版本>_linux_<架构>.tar.gz）发布，安装和升级脚本从和程序同一个地方下载、按 SHA256SUMS 校验后用 docker import 导入，从不从镜像仓库拉。
- 下一步：升级时报的：服务没停、什么都没改，处理好网络或磁盘后重新运行升级命令。平时报的：重新运行一次升级命令（同一版本加 -y）就会补上入口镜像
- AcornFox 已经自动做的：升级脚本在停服务之前就导入入口镜像，导入不成功就停下，不改任何东西
- 现象：安装或升级脚本打印“入口镜像…”和“故障编号: EDGE-02”；doctor 写“本机没有入口镜像”；后台服务日志里有“本机没有入口镜像”

可能原因（逐条判定）：

1. 入口包下载不下来（网络、发行版来源连不上） —— 判定：脚本输出里下载那一行的错误
2. 入口包校验不过（下载损坏或来源被替换） —— 判定：脚本写“SHA256 校验失败”
3. 磁盘满了，docker import 失败 —— 判定：df -h /var/lib/docker
4. 有人删了本机的入口镜像（docker image rm、docker system prune -a）［doctor 自动判定］ —— 判定：sudo docker image ls acornfox/edge 为空

排查顺序：

1. 看脚本最后 30 行
2. sudo docker image ls acornfox/edge
3. df -h /var/lib/docker

处置：

- ［1 级 可以直接做］重新运行同一条升级命令（同一版本加 -y）
- ［2 级 先问用户］清理磁盘

- 必须先停下问用户：要清理磁盘时；网络一直下载不下来时
- 确认恢复：sudo docker image ls acornfox/edge 有这个版本，doctor 的入口代理一项通过
- 相关编号：EDGE-01、INSTALL-GET-03
- 演练方法：升级时 --edge-package 给一个内容不对的文件（或在测试机上 sudo docker image rm acornfox/edge:<入口版本> 后运行 sudo acornfox doctor）

### EDGE-03 入口代理的管理口连不上或拒绝了令牌

- 分组：入口代理（应用网址和域名前面的入口容器）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：后台服务经入口代理自己的管理口（只在本机，127.0.0.1:18807，切换入口时可能是 18806）交设置，每个请求都要带数据目录里的管理令牌（edge-admin.token）。连不上或令牌不对时，应用照旧能打开，但新装的应用、改过的路由和域名都不会生效。
- 下一步：运行 sudo acornfox doctor 看入口代理一项；sudo systemctl restart acornfox-server（启动时发现入口容器不接受数据目录里的令牌，会按这个令牌在原地重建入口容器一次，应用网址断几秒）
- AcornFox 已经自动做的：后台服务每次启动时核对一次：入口容器不接受数据目录里的令牌，就按这个令牌在原地重建它（证书数据卷、设置卷照旧接着用，整份设置重新带上），同一次运行里最多重建一次；重建后仍被拒就不再动容器，只报 EDGE-03。后台服务运行中才出现的令牌不一致不会自己处理，要重启后台服务
- 现象：doctor 的入口代理一项写“管理口没有应答”或“拒绝了令牌”和 EDGE-03；部署停在 routing，报 route_failed

可能原因（逐条判定）：

1. 入口容器里的令牌和数据目录里的不一样（有人改了其中一个）［doctor 自动判定］ —— 判定：doctor 写“拒绝了令牌（401）”
2. 入口容器刚重启还没就绪 —— 判定：过 10 秒再运行 doctor
3. 管理口的端口被别的程序占了，入口容器自己听不上［doctor 自动判定］ —— 判定：doctor 的 SETUP-PORT-02

排查顺序：

1. sudo acornfox doctor
2. sudo docker logs --tail 50 acornfox-edge

处置：

- ［1 级 可以直接做］sudo systemctl restart acornfox-server（启动时令牌对不上会按数据目录里的令牌重建入口容器一次）
- ［2 级 先问用户］停掉占用管理口的程序

- 必须先停下问用户：重启后仍报 EDGE-03 时（重建过一次仍被拒，或管理口一直没有应答）带上证据包；不要自己改令牌文件，也不要删入口容器
- 确认恢复：doctor 的入口代理一项通过；令牌对不上的那种，后台服务日志里有一条“edge recreated with the management token in the data directory”
- 相关编号：EDGE-01、SETUP-PORT-02、route_failed
- 演练方法：在测试机上把另一个令牌拷进入口容器（echo 一串 48 位十六进制 > t; sudo docker cp t acornfox-edge:/etc/afx-edge/token; sudo docker restart acornfox-edge），再运行 sudo acornfox doctor

### EDGE-04 交给入口代理的设置没有生效

- 分组：入口代理（应用网址和域名前面的入口容器）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：入口容器自己的健康检查（afx-edge health）核对：现在生效的设置是不是保存的那一份、该听的端口是不是都在听。不一致时，部分应用或域名可能打不开。
- 下一步：运行 sudo docker exec acornfox-edge /usr/bin/afx-edge health 看是哪一项；sudo systemctl restart acornfox-server 让后台服务整份重交一次
- 现象：doctor 的入口代理一项写“容器自己的健康检查没通过”；运行环境页或状态里入口一项写“接受了设置，但现在生效的是…”

可能原因（逐条判定）：

1. 某个应用端口被别的程序占了，入口代理听不上［doctor 自动判定］ —— 判定：afx-edge health 写“这些端口没有在听”；doctor 的 SETUP-PORT-03
2. 入口代理刚重启，还在装回保存的设置 —— 判定：过 10 秒再看

排查顺序：

1. sudo docker exec acornfox-edge /usr/bin/afx-edge health
2. sudo acornfox doctor
3. sudo docker logs --tail 50 acornfox-edge

处置：

- ［1 级 可以直接做］sudo systemctl restart acornfox-server
- ［2 级 先问用户］停掉占用端口的程序

- 必须先停下问用户：要停别的程序时；重启后仍不行时带上证据包
- 确认恢复：afx-edge health 写“正常”，doctor 的入口代理一项通过
- 相关编号：SETUP-PORT-03、EDGE-03
- 演练方法：在测试机上用 python3 -m http.server 占住一个已分给应用的端口后重启入口容器，再运行 sudo acornfox doctor

### EDGE-05 升级时换成新的入口容器没有成功，原来的入口照常服务

- 分组：入口代理（应用网址和域名前面的入口容器）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：第一次升到新入口版本时，新的入口容器和原来的同时起来，新的健康了才平稳停掉原来的。新容器 30 秒内没通过健康检查，就删掉新容器，原来的入口一直没停、照常服务。
- 下一步：升级脚本已经自动恢复到升级前（应用照常能打开）。把脚本最后 30 行和 sudo journalctl -u acornfox-server -n 50 --no-pager 的输出交给维护者；处理好原因后可以再运行一次升级命令
- AcornFox 已经自动做的：删掉了没通过健康检查的新入口容器，原来的入口容器一直在服务；升级脚本按升级失败自动恢复到升级前
- 现象：升级脚本打印“入口没有换成新的”和“故障编号: EDGE-05”，随后自动恢复到升级前；后台服务日志里有“换成新的入口容器没有成功”

可能原因（逐条判定）：

1. 新入口容器起不来（镜像损坏、内存不够） —— 判定：日志里写“新的入口容器起不来”
2. 新入口不接受现在的设置（某条路由写法它不认） —— 判定：日志里健康检查那一句写“读不懂”或“设置不一样”
3. 某个端口新入口听不上 —— 判定：日志里写“这些端口没有在听”

排查顺序：

1. 看升级脚本最后 30 行
2. sudo journalctl -u acornfox-server -n 80 --no-pager | grep -i edge

处置：

- ［0 级 只读］转述原因
- ［2 级 先问用户］处理好原因后再运行一次升级命令

- 必须先停下问用户：总是：把证据交给维护者，再升级之前问用户
- 确认恢复：sudo docker ps --filter label=acornfox.edge=1 只有原来那一个入口在运行，应用网址能打开
- 相关编号：EDGE-06、UPGRADE-FAIL-02
- 演练方法：升级时 --edge-package 给一个 afx-edge 一启动就退出的入口包

### EDGE-06 升级没成功、恢复到升级前时没能换回原来的入口容器

- 分组：入口代理（应用网址和域名前面的入口容器）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：恢复到升级前要先停掉并删掉新入口容器，再起原来那个停着的入口容器（两者不能同时在跑：旧程序推设置会落到新容器上）。原来的入口起来时是空的，旧程序启动后 30 秒内会把设置补推过去。
- 下一步：先把“处理”里的手工步骤（删掉新入口容器、起原来的入口容器）告诉用户，用户同意后照做；做不了就停下，把输出交给维护者
- 现象：升级脚本恢复时打印“没能换回原来的入口容器”和“故障编号: EDGE-06”；恢复后应用网址打不开

可能原因（逐条判定）：

1. 原来的入口容器被删了（切换记录里的编号找不到） —— 判定：sudo docker ps -a --filter label=acornfox.edge=1
2. 原来的入口容器起不来（端口被别的程序占了） —— 判定：sudo docker logs --tail 50 原来的入口容器

排查顺序：

1. cat /var/lib/acornfox/edge-switch.json 看 old_id（原来的）和 new_id（新的）
2. sudo docker ps -a --filter label=acornfox.edge=1
3. sudo docker rm -f <new_id>，再 sudo docker start <old_id>
4. sudo systemctl restart acornfox-server（旧程序会补推一次设置；等不及 30 秒时这样做）

处置：

- ［2 级 先问用户］按上面的步骤换回原来的入口容器
- ［2 级 先问用户］原来的入口容器已经没有了：sudo docker rm -f <new_id> 后 sudo systemctl restart acornfox-server，旧程序会按旧办法新建一个入口容器（证书数据卷名不变，接着用）

- 必须先停下问用户：换回原来的入口容器之前（要删掉新入口容器）；原来的入口容器已经没有了时；做完仍打不开时带上证据包
- 确认恢复：sudo docker ps --filter label=acornfox.edge=1 只有一个入口在运行，应用网址能打开
- 相关编号：EDGE-05、UPGRADE-RESTORE-01
- 演练方法：升级时注入 EDGE-05 之前先把切换记录里的 old_id 对应的容器删掉（只在测试机上）

## 局域网控制台

### LAN-CONSOLE-01 这台服务器不能开局域网控制台

- 分组：局域网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：云主机、公网服务器进控制台：在自己电脑上运行 acornfox open（经 SSH 隧道）。确实在自家或办公室局域网里：等十几秒再运行 acornfox lan-console enable；还不行就看 acornfox doctor，把结果发给我们。不要为了开它去改安全组或防火墙
- 现象：acornfox lan-console enable 报 LAN-CONSOLE-01

可能原因（逐条判定）：

1. 这是云主机或公网服务器：没有可信的局域网，开了等于把一个只靠密码的入口放到公网上 —— 判定：acornfox lan-console status --json 说是云主机
2. 后台服务刚启动，还没判断出是不是云主机（十几秒内） —— 判定：systemctl status acornfox-server 刚启动不久
3. 应用访问地址（--public-host）填的是公网 IP 或域名，被当成了公网服务器 —— 判定：grep public-host /etc/systemd/system/acornfox-server.service

排查顺序：

1. acornfox lan-console status --json
2. 等十几秒再试一次

处置：

- ［0 级 只读］改用 acornfox open（经 SSH 隧道）
- ［2 级 先问用户］改安全组或防火墙——不要做

- 必须先停下问用户：用户坚持要在公网服务器上开时：说明风险，不要绕过
- 确认恢复：acornfox open 能打开控制台
- 相关编号：OPEN-APP-02

### LAN-CONSOLE-02 局域网控制台拒绝了不在局域网里的访问

- 分组：局域网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：这是保护，不用修。自己要从外面进控制台就用 acornfox open（经 SSH 隧道）。日志里反复出现陌生地址时，去路由器上把 18802 的端口映射删掉
- 现象：浏览器打开 18802 返回 403（LAN-CONSOLE-02）

可能原因（逐条判定）：

1. 访问的人不在这台服务器的局域网里（它只认 10.x、172.16–31.x、192.168.x、100.64–127.x 和本机地址） —— 判定：访问者的地址不在这几段里
2. 路由器把 18802 端口映射到了公网，外面的人在试 —— 判定：journalctl -u acornfox-server 里反复出现陌生公网地址
3. 经过了一层会换掉来源地址的代理或 VPN —— 判定：关掉代理或 VPN 后能打开

排查顺序：

1. 问用户是从哪里访问的

处置：

- ［0 级 只读］改用 acornfox open
- ［2 级 先问用户］删路由器上的端口映射

- 必须先停下问用户：要动路由器时
- 确认恢复：局域网里的电脑能打开登录页

### LAN-CONSOLE-03 局域网控制台开着但还没设密码

- 分组：局域网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：现在就用局域网里的电脑打开 acornfox lan-console status 给的地址设一个密码（至少 8 位），局域网里谁先打开谁设。不用局域网控制台就运行 acornfox lan-console disable。设好后 status 会显示是什么时间、从哪个地址设的；不是用户自己就运行 acornfox lan-console reset-password 重设
- 这一项要用户自己确认或操作
- 现象：doctor 警告“局域网控制台：开着 … 但还没设密码”（LAN-CONSOLE-03）

可能原因（逐条判定）：

1. 刚装完或刚运行过 acornfox lan-console reset-password，还没人去设［doctor 自动判定］ —— 判定：acornfox lan-console status --json 的 password_set 为 false
2. 装的时候默认打开了，但用户不知道 —— 判定：问用户

排查顺序：

1. acornfox lan-console status --json

处置：

- ［2 级 先问用户］请用户马上去设密码
- ［2 级 先问用户］用户不用时 acornfox lan-console disable

- 必须先停下问用户：总是：设密码要用户本人
- 确认恢复：status 的 password_set 为 true，doctor 没有这条警告

### lan_console_off 局域网控制台没开，没有密码可清

- 分组：局域网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用重设密码：局域网控制台是关着的。用户要用它时先问用户，再 acornfox lan-console enable（对局域网开放 18802），然后请用户打开给出的地址设密码
- 现象：acornfox lan-console reset-password 返回 code lan_console_off（HTTP 409）

可能原因（逐条判定）：

1. 局域网控制台本来就关着（安装时用了 --no-lan-console，或运行过 disable，或这是云主机） —— 判定：acornfox lan-console status --json 的 enabled 为 false

排查顺序：

1. acornfox lan-console status --json

处置：

- ［0 级 只读］告诉用户局域网控制台是关着的，不用重设密码
- ［2 级 先问用户］acornfox lan-console enable

- 必须先停下问用户：要打开局域网控制台时（对局域网开放端口）
- 确认恢复：acornfox lan-console status --json 的 enabled 和用户要的一致
- 相关编号：LAN-CONSOLE-01、LAN-CONSOLE-03

### LAN-CONSOLE-04 忘了局域网控制台的密码

- 分组：局域网控制台
- 默认等级：P3（提示或用法问题，不急）
- 是什么：控制台密码只存一份不可逆的摘要，没人（包括 AcornFox）能把它找回来，只能清掉重设。登录页的“忘了密码”不能直接重设：不登录就能重设等于谁都能抢走控制台，所以重设只能在服务器上做。
- 下一步：在装着 AcornFox 的服务器上（经 SSH）运行 acornfox lan-console reset-password：清掉密码，所有登录状态和记住的设备一起作废；然后在局域网里用浏览器打开控制台地址，重新设一个密码（谁先打开谁设，别拖）
- 现象：用户说“忘了控制台密码”“登录不进去”；登录页的“忘了密码”点开后的说明

可能原因（逐条判定）：

1. 忘了自己设的密码 —— 判定：问用户；记住过的设备上可能还没退出（控制台 设置 › 访问 里能看到记住的设备）
2. 密码不是自己设的（谁先打开谁设：开启后没及时设，被局域网里别人设了） —— 判定：acornfox lan-console status --json 的 password_set_at、password_set_from 对不上
3. 输错了（大小写、输入法） —— 判定：让用户在别处输入框里看着打一遍再试

排查顺序：

1. acornfox lan-console status --json 看开没开、密码什么时候从哪设的
2. acornfox lan-console devices --json 看记住了哪几台设备
3. 问用户要不要让所有设备都退出

处置：

- ［2 级 先问用户］acornfox lan-console reset-password（会让所有登录和记住的设备失效；用户说“忘了密码”就是同意）
- ［2 级 先问用户］只想让某一台设备（例如丢了的手机）退出：acornfox lan-console forget 设备编号，不要重设密码

- 必须先停下问用户：用户的原因不是“忘了密码”（例如只是想让丢了的手机退出）时，先问要不要让所有设备都退出；不要替用户设新密码，也不要让用户把密码发到对话里
- 确认恢复：acornfox lan-console status --json 的 password_set 是 false，用户在浏览器里设好后变成 true，用新密码能登录
- 相关编号：LAN-CONSOLE-03、PUBCON-05
- 演练方法：在测试机上设好密码后运行 acornfox lan-console reset-password，再打开登录页看“忘了密码”，用新密码登录

## 连接服务器（diagnosis.stage 为 connect）

### ssh_missing 本机找不到 ssh 命令

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：安装 OpenSSH 客户端（Windows：设置 → 可选功能）
- 现象：命令报 diagnosis.stage connect，code ssh_missing

可能原因（逐条判定）：

1. 电脑上没装 OpenSSH 客户端 —— 判定：ssh -V 报找不到命令

排查顺序：

1. ssh -V

处置：

- ［2 级 先问用户］请用户装 OpenSSH 客户端

- 必须先停下问用户：装软件由用户做
- 确认恢复：ssh -V 有版本号

### host_unreachable 连不上服务器

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：检查地址、端口、安全组是否放行 22
- 现象：code host_unreachable

可能原因（逐条判定）：

1. 地址或端口写错 —— 判定：acornfox target list 看登记的地址
2. 安全组或防火墙没放行 22 —— 判定：ssh -v 用户@主机 卡在 Connecting
3. 服务器关机或网络断了 —— 判定：云厂商控制台看实例状态

排查顺序：

1. acornfox target list
2. ssh -v 用户@主机

处置：

- ［1 级 可以直接做］改登记的地址
- ［2 级 先问用户］改安全组

- 必须先停下问用户：改安全组、开机时
- 确认恢复：ssh 用户@主机 能登录

### auth_failed SSH 认证失败

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：先在终端里 `ssh user@host` 确认能免密登录（`ssh-copy-id`）
- 现象：code auth_failed

可能原因（逐条判定）：

1. 没配免密登录，或用户名、钥匙不对 —— 判定：ssh -o BatchMode=yes 用户@主机 true 失败

排查顺序：

1. ssh -o BatchMode=yes 用户@主机 true

处置：

- ［2 级 先问用户］请用户配好免密登录（ssh-copy-id）

- 必须先停下问用户：要用户的密码或钥匙时
- 确认恢复：ssh -o BatchMode=yes 用户@主机 true 成功

### host_key_unknown 服务器主机指纹未确认

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P3（提示或用法问题，不急）
- 下一步：先手动 `ssh user@host` 一次确认主机指纹
- 现象：code host_key_unknown

可能原因（逐条判定）：

1. 这台电脑第一次连这台服务器 —— 判定：~/.ssh/known_hosts 里没有它

排查顺序：

1. ssh 用户@主机，核对指纹后输入 yes

处置：

- ［2 级 先问用户］请用户核对指纹后确认

- 必须先停下问用户：指纹要用户核对
- 确认恢复：再运行刚才的命令成功

### host_key_changed 服务器主机指纹与本机记录不一致

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：若确认服务器重装过或 IP 换了机器，运行 `ssh-keygen -R 主机` 删除旧记录，再手动 `ssh user@host` 确认新指纹；否则可能是中间人攻击，不要继续
- 这一项要用户自己确认或操作
- 现象：code host_key_changed

可能原因（逐条判定）：

1. 服务器重装过系统或换了机器 —— 判定：问用户
2. 有人冒充这台服务器 —— 判定：用户不知道有重装

排查顺序：

1. 停下问用户是不是重装过

处置：

- ［2 级 先问用户］用户确认重装过才 ssh-keygen -R 主机

- 必须先停下问用户：总是
- 确认恢复：ssh 能连上且指纹是用户确认过的

### acornfox_missing 服务器上没装 AcornFox

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：服务器上尚未安装 AcornFox
- 现象：code acornfox_missing

可能原因（逐条判定）：

1. 这台服务器还没装，或装在了别的用户的 PATH 外 —— 判定：ssh 用户@主机 'command -v acornfox' 为空

排查顺序：

1. ssh 用户@主机 'command -v acornfox'

处置：

- ［2 级 先问用户］按使用文档在服务器上安装

- 必须先停下问用户：安装之前
- 确认恢复：ssh 用户@主机 acornfox --json version 有结果

### permission_denied 无权访问 acornfox 服务

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：把该用户加入 `acornfox-users` 组后重新登录
- 现象：code permission_denied

可能原因（逐条判定）：

1. 登录用户不在 acornfox-users 组 —— 判定：ssh 用户@主机 id 里没有 acornfox-users

排查顺序：

1. ssh 用户@主机 id

处置：

- ［2 级 先问用户］sudo usermod -aG acornfox-users 用户（改系统账号）

- 必须先停下问用户：改账号之前
- 确认恢复：重新登录后命令成功

### server_down acornfox 服务未运行

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：在服务器上执行 `systemctl status acornfox-server`
- 现象：code server_down

可能原因（逐条判定）：

1. acornfox-server 停了或起不来 —— 判定：systemctl is-active acornfox-server

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 SETUP-SVC-01 处理

- 必须先停下问用户：重启后仍不行时
- 确认恢复：命令成功
- 相关编号：SETUP-SVC-01

### local_server_down 连不上本机的 acornfox 服务

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：Linux 服务器上执行 `systemctl status acornfox-server` 和 `acornfox doctor` 检查；Mac、Windows 本机的 AcornFox 在虚拟机里：Mac 用 `acornfox mac status`、`acornfox mac doctor` 看，Windows 用 `acornfox windows status`；管理别的服务器则先 `acornfox target add 名字 --ssh 用户@地址` 登记
- 现象：code local_server_down

可能原因（逐条判定）：

1. 本机的 acornfox-server 没在运行 —— 判定：systemctl is-active acornfox-server
2. 在 Mac、Windows 上没登记服务器就直接运行命令 —— 判定：acornfox target list 为空

排查顺序：

1. acornfox target list
2. Linux 上 sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 SETUP-SVC-01 处理
- ［1 级 可以直接做］登记服务器

- 必须先停下问用户：—（不用问）
- 确认恢复：命令成功
- 相关编号：SETUP-SVC-01

### version_mismatch 命令行和服务器版本对不上

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P3（提示或用法问题，不急）
- 下一步：升级 CLI 或服务器
- 现象：code version_mismatch

可能原因（逐条判定）：

1. 电脑上的命令行和服务器上的 AcornFox 差太多版本 —— 判定：acornfox --json version 和服务器上的比较

排查顺序：

1. 两边都看 acornfox --json version

处置：

- ［1 级 可以直接做］重新运行 install-cli 升级电脑上的命令行
- ［2 级 先问用户］升级服务器

- 必须先停下问用户：升级服务器之前
- 确认恢复：命令成功

### forwarding_disabled 服务器禁止了 SSH 端口转发

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：服务器 sshd 需要 `AllowTcpForwarding yes`
- 现象：code forwarding_disabled；acornfox open 打不开

可能原因（逐条判定）：

1. sshd 配置里 AllowTcpForwarding no —— 判定：sudo sshd -T | grep allowtcpforwarding

排查顺序：

1. sudo sshd -T | grep allowtcpforwarding

处置：

- ［2 级 先问用户］改 sshd 配置（改系统设置）

- 必须先停下问用户：改 sshd 之前
- 确认恢复：acornfox open 能打开

### connect_failed SSH 连接失败（其他原因）

- 分组：连接服务器（diagnosis.stage 为 connect）
- 出现在：diagnosis.stage 为 connect
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：先在终端里手动 `ssh user@host` 排查
- 现象：code connect_failed

可能原因（逐条判定）：

1. 见 log_excerpt 里 ssh 的报错 —— 判定：命令 --json 输出的 diagnosis.log_excerpt

排查顺序：

1. ssh -v 用户@主机

处置：

- ［1 级 可以直接做］按 ssh 的报错处理

- 必须先停下问用户：要改服务器设置时
- 确认恢复：命令成功

### server_too_old 服务器的 AcornFox 太旧，不支持这个命令

- 分组：连接服务器（diagnosis.stage 为 connect）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：升级服务器上的 AcornFox
- 现象：命令返回 code server_too_old，退出码 1

可能原因（逐条判定）：

1. 服务器版本没有这个接口 —— 判定：服务器上 acornfox --json version

排查顺序：

1. acornfox --json version（两边）

处置：

- ［2 级 先问用户］升级服务器

- 必须先停下问用户：升级之前
- 确认恢复：命令成功

## 部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）

### dockerfile_missing 项目里没有 Dockerfile

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 upload
- 默认等级：P3（提示或用法问题，不急）
- 下一步：在项目根目录添加 Dockerfile（写明 EXPOSE 端口）后重新部署
- 现象：deploy 返回 code dockerfile_missing

可能原因（逐条判定）：

1. 项目根目录没有 Dockerfile —— 判定：ls Dockerfile

排查顺序：

1. ls 项目根目录

处置：

- ［1 级 可以直接做］按项目类型写 Dockerfile

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### too_large 要上传的项目太大

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 upload
- 默认等级：P3（提示或用法问题，不急）
- 下一步：检查 .dockerignore，排除依赖、构建产物和大文件
- 现象：deploy 返回 code too_large

可能原因（逐条判定）：

1. node_modules、构建产物、数据文件被一起打包 —— 判定：du -sh * 看哪个目录大

排查顺序：

1. du -sh *

处置：

- ［1 级 可以直接做］补 .dockerignore

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### context_too_large 项目包解压后超过 1 GB

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 upload
- 默认等级：P3（提示或用法问题，不急）
- 下一步：在 .dockerignore 里排除依赖目录（如 node_modules）、构建产物、数据和大文件后重新部署
- 现象：diagnosis.code context_too_large

可能原因（逐条判定）：

1. 上传内容里有大目录 —— 判定：du -sh *

排查顺序：

1. du -sh *

处置：

- ［1 级 可以直接做］补 .dockerignore

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### upload_invalid 上传的项目包坏了或读不了

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 upload
- 默认等级：P3（提示或用法问题，不急）
- 下一步：使用 acornfox deploy 重新上传项目目录
- 现象：diagnosis.code upload_invalid

可能原因（逐条判定）：

1. 上传中断或包格式不对 —— 判定：重新 deploy 能过

排查顺序：

1. 重新 deploy

处置：

- ［1 级 可以直接做］重新 deploy

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### clone_failed 拉取 Git 仓库失败

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 source
- 默认等级：P3（提示或用法问题，不急）
- 下一步：检查 Git 地址与网络后重试
- 现象：deploy --git 返回 code clone_failed

可能原因（逐条判定）：

1. 地址不是有效的 https Git 仓库 —— 判定：message 写“请提供有效的 https Git 仓库地址”
2. 仓库太大 —— 判定：message 写“精简仓库”
3. 网络问题 —— 判定：服务器上 git ls-remote 地址

排查顺序：

1. 读 message 和 hint

处置：

- ［1 级 可以直接做］改地址或改用 acornfox deploy 上传本地目录

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### clone_timeout 拉取 Git 仓库超时

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 source
- 默认等级：P3（提示或用法问题，不急）
- 下一步：国内服务器访问 GitHub 常超时，可改用 Gitee/CNB 镜像，或直接 acornfox deploy 上传本地目录
- 现象：code clone_timeout

可能原因（逐条判定）：

1. 国内服务器访问 GitHub 慢 —— 判定：服务器上 time git ls-remote 地址

排查顺序：

1. 换国内仓库或上传本地目录

处置：

- ［1 级 可以直接做］改用 acornfox deploy 上传

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### git_missing 服务器上没有 git

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 source
- 默认等级：P3（提示或用法问题，不急）
- 下一步：请在服务器上安装 git 后重试
- 现象：code git_missing

可能原因（逐条判定）：

1. 服务器没装 git —— 判定：command -v git 为空

排查顺序：

1. command -v git

处置：

- ［2 级 先问用户］sudo apt-get install -y git（装软件）
- ［1 级 可以直接做］改用 acornfox deploy 上传本地目录

- 必须先停下问用户：装软件之前
- 确认恢复：deploy 进入构建

### ref_not_found 指定的分支或提交不存在

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 source
- 默认等级：P3（提示或用法问题，不急）
- 下一步：检查 --ref 指定的分支名或提交哈希是否存在
- 现象：code ref_not_found

可能原因（逐条判定）：

1. --ref 写错 —— 判定：git ls-remote 地址 看有哪些分支

排查顺序：

1. git ls-remote 地址

处置：

- ［1 级 可以直接做］改 --ref

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### auth_required 仓库要登录才能拉（私有仓库）

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 source
- 默认等级：P3（提示或用法问题，不急）
- 下一步：把代码下载到本地后在项目目录执行 acornfox deploy 上传；私有仓库支持计划在首发之后提供
- 现象：code auth_required

可能原因（逐条判定）：

1. 仓库是私有的 —— 判定：浏览器不登录打不开仓库

排查顺序：

1. 确认仓库是不是公开的

处置：

- ［1 级 可以直接做］改用本地上传

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### repo_not_found 仓库不存在

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 source
- 默认等级：P3（提示或用法问题，不急）
- 下一步：检查仓库地址是否正确，且仓库是公开的
- 现象：code repo_not_found

可能原因（逐条判定）：

1. 地址写错或仓库被删 —— 判定：浏览器打开地址

排查顺序：

1. 核对地址

处置：

- ［1 级 可以直接做］改地址

- 必须先停下问用户：—（不用问）
- 确认恢复：deploy 进入构建

### build_failed 镜像构建失败

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 build
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：根据日志修改 Dockerfile 或依赖后重新部署
- 现象：diagnosis.code build_failed，带 log_excerpt

可能原因（逐条判定）：

1. Dockerfile 写错或构建命令失败 —— 判定：log_excerpt 最后几行
2. 依赖装不上 —— 判定：日志里是包管理器的报错

排查顺序：

1. 读 log_excerpt
2. acornfox logs 应用 --tail 200 看运行日志；acornfox logs 应用 --version N --build 看那一版的构建日志

处置：

- ［1 级 可以直接做］改 Dockerfile 或依赖后重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live
- 回滚：线上仍是原来那一版，不用回滚

### registry_timeout 构建时访问软件包源超时

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 build
- 默认等级：P3（提示或用法问题，不急）
- 下一步：构建时访问软件包源超时。服务器在中国大陆时，在 Dockerfile 中改用国内镜像源后重新部署：npm 加 --registry=https://registry.npmmirror.com；pip 加 -i https://mirrors.huaweicloud.com/repository/pypi/simple；Go 设 ENV GOPROXY=https://goproxy.cn,direct；Alpine 用 sed -i 's/dl-cdn.alpinelinux.org/mirrors.huaweicloud.com/g' /etc/apk/repositories
- 现象：diagnosis.code registry_timeout

可能原因（逐条判定）：

1. 国内服务器访问 npm、PyPI、Go、Alpine 官方源慢 —— 判定：log_excerpt 里是超时

排查顺序：

1. 读 log_excerpt 看是哪个包管理器

处置：

- ［1 级 可以直接做］Dockerfile 里换国内源后重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### base_image_not_found 构建时基础镜像拉取失败

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 build
- 默认等级：P3（提示或用法问题，不急）
- 下一步：基础镜像拉取失败：先检查 FROM 里的镜像名；日志里是网络错误或 502、503 时，多半是镜像加速地址暂时不可用，稍后直接重新部署即可，反复失败就在 /etc/docker/daemon.json 换用可用的加速地址（阿里云服务器用控制台“容器镜像服务 → 镜像加速器”里的专属地址）
- 现象：diagnosis.code base_image_not_found

可能原因（逐条判定）：

1. FROM 的镜像名或标签写错 —— 判定：log_excerpt 里是 not found 或 manifest unknown
2. 镜像加速地址暂时不可用 —— 判定：log_excerpt 里是 502、503、timeout

排查顺序：

1. 读 log_excerpt

处置：

- ［1 级 可以直接做］改 FROM 或稍后重新部署
- ［2 级 先问用户］改 /etc/docker/daemon.json

- 必须先停下问用户：改 Docker 配置之前
- 确认恢复：部署到 live

### dependency_missing 构建缺少依赖

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 build
- 默认等级：P3（提示或用法问题，不急）
- 下一步：构建缺少依赖：把依赖写进依赖清单（如 requirements.txt / package.json）并在 Dockerfile 中安装
- 现象：diagnosis.code dependency_missing

可能原因（逐条判定）：

1. 依赖没写进清单或 Dockerfile 没装 —— 判定：log_excerpt 里是 module not found 一类

排查顺序：

1. 读 log_excerpt

处置：

- ［1 级 可以直接做］补依赖后重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### runner_response_too_large 构建输出过大

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 build
- 默认等级：P3（提示或用法问题，不急）
- 下一步：请减少构建时的输出后重新部署
- 现象：diagnosis.code runner_response_too_large

可能原因（逐条判定）：

1. 构建时逐个打印文件等大量输出 —— 判定：Dockerfile 里有 ls -R、verbose 一类

排查顺序：

1. 看 Dockerfile

处置：

- ［1 级 可以直接做］去掉多余输出后重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### pull_failed 拉取镜像失败

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 image
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：检查镜像引用或稍后重试
- 现象：deploy --image 或附加服务（stage addon）返回 code pull_failed

可能原因（逐条判定）：

1. 镜像名或标签写错 —— 判定：docker pull 同一个引用
2. 国内服务器拉 Docker Hub 不通，镜像加速没配或不可用 —— 判定：cat /etc/docker/daemon.json；log_excerpt 里是网络错误

排查顺序：

1. 读 message 和 log_excerpt

处置：

- ［1 级 可以直接做］改镜像引用或稍后重新部署
- ［2 级 先问用户］配置镜像加速

- 必须先停下问用户：改 Docker 配置之前
- 确认恢复：部署到 live
- 相关编号：CAT-INST-12

### pull_timeout 拉取镜像超时

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 image
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：看 message 里每个镜像源各自怎么了（failure.sources 每个源一条）：都很慢就换个时间再装，或给 Docker 加一个自己的镜像加速地址；都连不上先查网络。acornfox sources --probe 看各个源现在多快
- 现象：code pull_timeout，message 写“镜像没拉下来（试了 N 个源）：…”

可能原因（逐条判定）：

1. 镜像源都很慢（例如把大的镜像层转到境外下载），总时限 50 分钟里拉不完 —— 判定：message 里写了每个源的平均速度和已下多少
2. 镜像源都连不上或拒绝访问 —— 判定：acornfox sources --probe

排查顺序：

1. 读 message
2. acornfox sources --probe
3. cat /etc/docker/daemon.json

处置：

- ［1 级 可以直接做］换个时间重新部署
- ［2 级 先问用户］加一个自己的镜像加速地址（改 Docker 配置）

- 必须先停下问用户：改 Docker 配置之前
- 确认恢复：部署到 live
- 相关编号：SETUP-NET-03、CAT-INST-12

### image_not_found 镜像不存在

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 image
- 默认等级：P3（提示或用法问题，不急）
- 下一步：检查镜像名与标签是否正确；若镜像确实存在，可能是加速源未收录，换用其他加速源或自行构建
- 现象：code image_not_found

可能原因（逐条判定）：

1. 镜像名或标签写错，或加速源没有收录 —— 判定：在能访问 Docker Hub 的地方 docker pull

排查顺序：

1. 核对镜像名

处置：

- ［1 级 可以直接做］改镜像引用

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### image_gone 要回退的那一版程序已不在服务器上

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 image
- 默认等级：P3（提示或用法问题，不急）
- 下一步：请重新部署那一版的代码
- 现象：rollback 或 redeploy 返回 code image_gone

可能原因（逐条判定）：

1. 那一版的镜像超出了保留范围（只留最近 5 个）或被清理了 —— 判定：acornfox versions 应用 --json 看 can_rollback

排查顺序：

1. acornfox versions 应用

处置：

- ［2 级 先问用户］换一个 can_rollback 为 true 的版本，或重新部署那一版的代码

- 必须先停下问用户：回到哪一版问用户
- 确认恢复：部署到 live

### interrupted 部署途中服务重启，这次部署已作废

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 build
- 默认等级：P3（提示或用法问题，不急）
- 下一步：请重新提交部署
- 现象：diagnosis.code interrupted

可能原因（逐条判定）：

1. 部署进行中 acornfox-server 重启了 —— 判定：systemctl status acornfox-server 的启动时间

排查顺序：

1. 确认服务在运行

处置：

- ［1 级 可以直接做］重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### start_failed 容器启动失败

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 start
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：看 log_excerpt 和 acornfox logs，修启动命令或缺失的环境变量
- 现象：diagnosis.code start_failed（stage start 或 addon）

可能原因（逐条判定）：

1. 启动命令写错 —— 判定：log_excerpt
2. 缺环境变量或端口冲突 —— 判定：log_excerpt；acornfox env list

排查顺序：

1. 读 log_excerpt
2. acornfox logs 应用 --tail 200 看运行日志；acornfox logs 应用 --version N --build 看那一版的构建日志

处置：

- ［1 级 可以直接做］改启动命令或环境变量后重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live
- 回滚：线上仍是原来那一版

### out_of_memory 因内存超限被杀掉

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 health
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：acornfox app set --memory 1024 调大内存上限（立即生效）；部署时就内存不够的，改完再重新部署
- 现象：diagnosis.code out_of_memory（stage health 或 addon）

可能原因（逐条判定）：

1. 应用或附加服务的内存上限太小 —— 判定：acornfox stats 应用；acornfox limits

排查顺序：

1. acornfox stats 应用
2. acornfox limits

处置：

- ［1 级 可以直接做］acornfox app set --memory 更大的值
- ［2 级 先问用户］调大整机上限

- 必须先停下问用户：要动整机上限或别的应用时
- 确认恢复：应用稳定运行
- 相关编号：memory_cap_exceeded

### container_exited 容器启动后退出

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 health
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：请检查启动命令与日志
- 现象：diagnosis.code container_exited

可能原因（逐条判定）：

1. 程序启动报错退出 —— 判定：log_excerpt
2. 缺环境变量、连不上数据库 —— 判定：log_excerpt；acornfox env list

排查顺序：

1. 读 log_excerpt
2. acornfox logs 应用 --tail 200 看运行日志；acornfox logs 应用 --version N --build 看那一版的构建日志

处置：

- ［1 级 可以直接做］修启动命令或环境变量后重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### exited 附加服务（数据库等）启动后退出或反复重启

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 addon
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：查看数据库日志摘录；常见原因是数据卷里有不兼容的旧数据
- 现象：diagnosis.stage addon，code exited

可能原因（逐条判定）：

1. 数据卷里有别的版本留下的旧数据 —— 判定：log_excerpt 里是版本不兼容
2. 配置错误 —— 判定：log_excerpt

排查顺序：

1. 读 log_excerpt

处置：

- ［0 级 只读］转述给用户；数据卷没被改动
- ［2 级 先问用户］删数据卷重建

- 必须先停下问用户：动数据卷之前
- 确认恢复：部署到 live

### not_ready 附加服务在时限内没有就绪

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 addon
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：查看数据库日志摘录；首次初始化较慢时可直接重新部署
- 现象：diagnosis.stage addon，code not_ready

可能原因（逐条判定）：

1. 数据库第一次初始化慢 —— 判定：log_excerpt 里还在初始化

排查顺序：

1. 读 log_excerpt

处置：

- ［1 级 可以直接做］重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### port_not_listening 程序没有在约定的端口上监听

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 health
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：程序要监听 0.0.0.0（不是 127.0.0.1），端口与 --port 一致
- 现象：diagnosis.code port_not_listening

可能原因（逐条判定）：

1. 程序只监听了 127.0.0.1 —— 判定：日志里的监听地址
2. 程序监听的端口和应用设置的端口不一样 —— 判定：acornfox status 应用 --json 的端口和日志对比

排查顺序：

1. acornfox logs 应用 --tail 200 看运行日志；acornfox logs 应用 --version N --build 看那一版的构建日志

处置：

- ［1 级 可以直接做］改程序的监听地址，或 acornfox app set --port 后重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### unpersisted_database 数据写在容器里，重建会丢（警告）

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 data
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：先问用户：应用已经在用的先导出容器里的数据，再 acornfox volume add 数据目录、acornfox redeploy（换新容器，容器里的数据不会自动搬过去）
- 现象：部署成功，但 warnings 里有 unpersisted_database

可能原因（逐条判定）：

1. 应用把数据库文件写在容器里，没有挂数据目录 —— 判定：warnings 的 hint 写了目录

排查顺序：

1. 读 warnings
2. 问用户应用是不是已经在用、容器里有没有要留的数据

处置：

- ［0 级 只读］把 warnings 里的目录告诉用户，说明现在的数据在容器里，重建容器就会丢
- ［2 级 先问用户］先导出容器里的数据（sudo docker cp 容器名:目录 ./备份目录），再 acornfox volume add 目录、acornfox redeploy

- 必须先停下问用户：总是先问：redeploy 会换掉现在的容器，新容器挂的是空的数据目录，容器里自上次部署以来写的数据不会自动搬过去。应用已经在用的，先请用户导出数据，用户同意后再做
- 确认恢复：acornfox volume list 有这个目录；redeploy 后 warnings 里没有 unpersisted_database，应用里的数据还在

### route_failed 路由持续同步失败超过 5 分钟

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 route
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：入口代理没有接受路由；运行 acornfox doctor 查看入口代理的状态
- 现象：diagnosis.code route_failed

可能原因（逐条判定）：

1. 入口代理没在运行、管理口被占或拒绝了令牌［doctor 自动判定］ —— 判定：sudo acornfox doctor 的入口代理和端口两项（EDGE-01、EDGE-03、SETUP-PORT-02）

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 doctor 给出的编号处理（EDGE-01、EDGE-03、SETUP-PORT-02）

- 必须先停下问用户：要停别的程序时
- 确认恢复：部署到 live
- 相关编号：EDGE-01、EDGE-03、SETUP-PORT-02

### failed 部署失败（服务器没给出更具体的原因）

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 deploy
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：acornfox diagnose 部署编号 看诊断；acornfox logs 应用 --tail 200 看运行日志；acornfox logs 应用 --version N --build 看那一版的构建日志
- 现象：deploy 返回 code failed

可能原因（逐条判定）：

1. 服务器版本旧，没有给出诊断 —— 判定：acornfox diagnose 部署编号 --json

排查顺序：

1. acornfox diagnose

处置：

- ［1 级 可以直接做］按诊断处理

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### superseded 这次部署被更新的一次取代了

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 deploy
- 默认等级：P3（提示或用法问题，不急）
- 下一步：查看最新部署：acornfox status
- 现象：deploy 返回 code superseded

可能原因（逐条判定）：

1. 在这次结束前又提交了一次部署 —— 判定：acornfox versions 应用

排查顺序：

1. acornfox status

处置：

- ［0 级 只读］看最新那次的结果

- 必须先停下问用户：—（不用问）
- 确认恢复：—

### cancelled 等待被打断（部署在服务器上继续）

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 deploy
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按 hint 运行 acornfox --json status 部署编号 查结果，不要重新部署
- 现象：deploy、redeploy、start 等返回 code cancelled

可能原因（逐条判定）：

1. 按了 Ctrl-C、SSH 断开或 AI 工作台中断了命令 —— 判定：hint 里有部署编号

排查顺序：

1. acornfox --json status 部署编号

处置：

- ［0 级 只读］查状态，不要重复提交

- 必须先停下问用户：—（不用问）
- 确认恢复：status 显示终态

### timeout 等了 --timeout 还没到终态

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 deploy
- 默认等级：P3（提示或用法问题，不急）
- 下一步：用 acornfox status 部署编号 查看后续状态，或用 --timeout 延长等待；不要重新部署
- 现象：code timeout

可能原因（逐条判定）：

1. 构建或拉镜像慢 —— 判定：acornfox --json status 部署编号 还在 building

排查顺序：

1. acornfox --json status 部署编号

处置：

- ［0 级 只读］继续查状态

- 必须先停下问用户：—（不用问）
- 确认恢复：status 显示终态

### start_timeout start 或 restart 后 60 秒没起来

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 start
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：acornfox logs 应用名 看日志，找它起不来或反复退出的原因；acornfox status 应用名 看现在的状态
- 现象：start、restart 返回 code start_timeout，退出码 1

可能原因（逐条判定）：

1. 应用启动慢或反复退出 —— 判定：acornfox logs 应用

排查顺序：

1. acornfox status 应用
2. acornfox logs 应用 --tail 100

处置：

- ［1 级 可以直接做］按日志处理

- 必须先停下问用户：—（不用问）
- 确认恢复：status 是 running

### no_deployment 应用没有上线过的版本

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 start
- 默认等级：P3（提示或用法问题，不急）
- 下一步：先部署：acornfox deploy，或 acornfox redeploy 应用名；acornfox versions 应用名 看各次部署为什么没上线
- 现象：start、restart 返回 code no_deployment

可能原因（逐条判定）：

1. 应用的部署都失败了，或还没部署过 —— 判定：acornfox versions 应用

排查顺序：

1. acornfox versions 应用

处置：

- ［1 级 可以直接做］重新部署

- 必须先停下问用户：—（不用问）
- 确认恢复：部署到 live

### rollback_target_mismatch 服务器回到的版本不是指定的那一版

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 出现在：diagnosis.stage 为 deploy
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：先在服务器上升级 AcornFox；用 acornfox versions 查看现在的版本记录
- 现象：rollback --to N 返回 code rollback_target_mismatch

可能原因（逐条判定）：

1. 服务器版本旧，不认指定版本 —— 判定：服务器上 acornfox --json version

排查顺序：

1. acornfox versions 应用

处置：

- ［2 级 先问用户］升级服务器

- 必须先停下问用户：升级之前；现在线上是哪一版要告诉用户
- 确认恢复：versions 里 live 是想要的那一版

### deploy_in_progress 回退时有部署正在进行

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：确认要取消，就加上 --cancel-pending 再执行
- 现象：rollback 返回 code deploy_in_progress

可能原因（逐条判定）：

1. 这个应用正在部署 —— 判定：acornfox status 应用

排查顺序：

1. acornfox status 应用

处置：

- ［0 级 只读］等部署结束
- ［2 级 先问用户］--cancel-pending 取消正在进行的部署

- 必须先停下问用户：取消部署之前
- 确认恢复：rollback 成功

### version_not_found 没有这一版

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：用 acornfox versions 查看有哪些版本
- 现象：rollback --to N 或 logs --version N 返回 code version_not_found

可能原因（逐条判定）：

1. 版本号写错 —— 判定：acornfox versions 应用

排查顺序：

1. acornfox versions 应用

处置：

- ［0 级 只读］用对的版本号

- 必须先停下问用户：—（不用问）
- 确认恢复：命令成功

### version_is_live 指定的版本就是正在服务的版本

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用回退；acornfox versions 看别的版本
- 现象：rollback --to N 返回 code version_is_live

可能原因（逐条判定）：

1. 要回到的就是现在这一版 —— 判定：versions 里它的 status 是 live

排查顺序：

1. acornfox versions 应用

处置：

- ［0 级 只读］不用处理

- 必须先停下问用户：—（不用问）
- 确认恢复：—

### version_same_as_live 指定版本的程序和正在服务的相同

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用回退：那一版和现在用的是同一个程序
- 现象：rollback --to N 返回 code version_same_as_live

可能原因（逐条判定）：

1. 两次部署用的是同一个镜像 —— 判定：versions 的 rollback_text

排查顺序：

1. acornfox versions 应用

处置：

- ［0 级 只读］不用处理

- 必须先停下问用户：—（不用问）
- 确认恢复：—

### version_not_rollbackable 这一版不能回退

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：用 acornfox versions 查看可回退的版本
- 现象：rollback --to N 返回 code version_not_rollbackable

可能原因（逐条判定）：

1. 那一版没上线过，或有更新的同一程序 —— 判定：versions 的 can_rollback 和 rollback_text

排查顺序：

1. acornfox versions 应用

处置：

- ［2 级 先问用户］换一个 can_rollback 为 true 的版本

- 必须先停下问用户：回到哪一版问用户
- 确认恢复：rollback 成功

### version_image_gone 那一版的程序已不在服务器上

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：用 acornfox versions 查看可回退的版本，或重新部署那一版的代码
- 现象：rollback --to N 返回 code version_image_gone

可能原因（逐条判定）：

1. 镜像超出了保留范围 —— 判定：versions 的 can_rollback 为 false

排查顺序：

1. acornfox versions 应用

处置：

- ［2 级 先问用户］换版本或重新部署那一版的代码

- 必须先停下问用户：问用户
- 确认恢复：rollback 成功
- 相关编号：image_gone

### no_rollback_target 没有可以回退的版本

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：用 acornfox versions 查看版本记录
- 现象：rollback 返回 code no_rollback_target

可能原因（逐条判定）：

1. 除了现在这一版没有别的可回退版本 —— 判定：versions 的 rollback_target 为 0

排查顺序：

1. acornfox versions 应用

处置：

- ［0 级 只读］告诉用户只能重新部署代码

- 必须先停下问用户：—（不用问）
- 确认恢复：—

### memory_cap_exceeded 整机分配给应用的内存不够了

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：先 acornfox limits --json 看各应用占用，调小用不完的应用（app set --memory），或请用户确认后调大整机上限 acornfox limits --memory 26G --json
- 现象：app set --memory、deploy 返回 code memory_cap_exceeded

可能原因（逐条判定）：

1. 各应用分配的内存加起来超过整机上限 —— 判定：acornfox limits --json

排查顺序：

1. acornfox limits --json

处置：

- ［2 级 先问用户］调小别的应用或调大上限

- 必须先停下问用户：总是
- 确认恢复：命令成功
- 相关编号：CAT-INST-05

### memory_below_usage 内存上限不能低于应用当前实际用量

- 分组：部署诊断（deploy、redeploy、start 等返回的 diagnosis.code）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：内存上限不能低于应用当前实际用量，换一个更大的值
- 现象：app set --memory 返回 code memory_below_usage

可能原因（逐条判定）：

1. 设的值比应用现在用的还小 —— 判定：acornfox stats 应用

排查顺序：

1. acornfox stats 应用

处置：

- ［1 级 可以直接做］换更大的值

- 必须先停下问用户：—（不用问）
- 确认恢复：命令成功

## 域名和公网控制台

### dns_unresolved 本机解析不到这个域名

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：解析记录可能还没生效，请稍等几分钟再试。
- 现象：public-console enable 或绑定域名时返回 code dns_unresolved

可能原因（逐条判定）：

1. 刚加的解析还没生效 —— 判定：几分钟后 dig +short 域名 有结果
2. 解析没加或加错了主机记录 —— 判定：域名服务商控制台

排查顺序：

1. dig +short 域名

处置：

- ［1 级 可以直接做］等几分钟再试
- ［2 级 先问用户］在域名服务商加解析

- 必须先停下问用户：要改解析时
- 确认恢复：dig +short 域名 是这台服务器的公网 IP

### dns_mismatch 域名解析指向了别的地址

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：把域名的 A 记录改成这台服务器的公网 IP
- 现象：code dns_mismatch

可能原因（逐条判定）：

1. A 记录写的是别的 IP（旧服务器、CDN） —— 判定：dig +short 域名 和服务器公网 IP 对比

排查顺序：

1. dig +short 域名

处置：

- ［2 级 先问用户］改解析

- 必须先停下问用户：改解析之前
- 确认恢复：dig 结果是这台服务器

### dns_error 没有 DNS 服务器应答

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：检查服务器的 DNS 设置和网络后再试
- 现象：code dns_error

可能原因（逐条判定）：

1. 服务器的 DNS 不通 —— 判定：resolvectl status；dig 任意域名

排查顺序：

1. dig acornfox.com

处置：

- ［1 级 可以直接做］稍后再试
- ［2 级 先问用户］改 DNS 设置

- 必须先停下问用户：改系统 DNS 之前
- 确认恢复：dig 有结果

### port_closed 域名的 80 或 443 端口拒绝连接

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：检查 80、443 有没有被别的程序占、AcornFox 网关是否在运行（acornfox doctor）
- 现象：code port_closed

可能原因（逐条判定）：

1. 网关没在运行［doctor 自动判定］ —— 判定：doctor 的网关一项
2. 80/443 被别的程序占着（共存模式）［doctor 自动判定］ —— 判定：doctor 的 SETUP-PORT-01

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 doctor 的编号处理

- 必须先停下问用户：要停别的程序时
- 确认恢复：再验证通过
- 相关编号：SETUP-PORT-01、SETUP-EDGE-01

### port_unreachable 连接域名的端口超时（多半是安全组）

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：在云厂商控制台的安全组里放行 80 和 443（入方向）
- 现象：code port_unreachable

可能原因（逐条判定）：

1. 安全组或防火墙没放行 80/443 —— 判定：从外网 curl -m 5 http://域名 超时

排查顺序：

1. 问用户安全组有没有放行

处置：

- ［2 级 先问用户］改安全组

- 必须先停下问用户：改安全组之前
- 确认恢复：再验证通过

### connection_reset 连接在握手中途被重置

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：多半是云厂商拦截（未备案）或中间设备：中国大陆服务器先确认域名已备案
- 现象：code connection_reset

可能原因（逐条判定）：

1. 中国大陆服务器的域名没备案，被拦截 —— 判定：备案查询
2. 中间有防火墙重置连接 —— 判定：换网络访问

排查顺序：

1. 确认备案

处置：

- ［0 级 只读］未备案时继续用 http://IP:端口 访问

- 必须先停下问用户：—（不用问）
- 确认恢复：再验证通过

### icp_blocked 访问被云厂商拦截：域名未备案

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：中国大陆服务器的域名须完成 ICP 备案；备案前继续用 http://IP:端口 访问
- 这一项要用户自己确认或操作
- 现象：code icp_blocked

可能原因（逐条判定）：

1. 域名没有备案 —— 判定：浏览器打开域名看到云厂商的拦截页

排查顺序：

1. 问用户备案情况

处置：

- ［2 级 先问用户］备案由用户自己办

- 必须先停下问用户：总是
- 确认恢复：备案后再验证通过

### wrong_target 域名能访问，但应答不是这台服务器的

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：检查解析和 CDN：域名要直接指到这台服务器
- 现象：code wrong_target

可能原因（逐条判定）：

1. 解析到了 CDN 或别的服务器 —— 判定：dig +short 域名

排查顺序：

1. dig +short 域名

处置：

- ［2 级 先问用户］改解析或关 CDN

- 必须先停下问用户：改解析之前
- 确认恢复：再验证通过

### cert_pending HTTPS 证书还没签下来

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：确认域名 A 记录指向本服务器、80 与 443 端口已放行；中国大陆服务器的域名须已完成 ICP 备案。证书一般一两分钟内签下，稍等再看
- 现象：code cert_pending（域名状态或公网控制台验证）

可能原因（逐条判定）：

1. 刚绑定，证书正在申请 —— 判定：一两分钟后再看
2. 80/443 不通或未备案，证书签不下来 —— 判定：doctor 的端口一项；备案

排查顺序：

1. 等两分钟
2. acornfox domain list 看状态

处置：

- ［0 级 只读］等待

- 必须先停下问用户：—（不用问）
- 确认恢复：https://域名 能打开，证书有效
- 相关编号：cert_failed

### cert_failed 等了很久还是没有证书

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：确认域名 A 记录指向本服务器、80 与 443 端口已放行；中国大陆服务器的域名须已完成 ICP 备案；处理后重新绑定
- 现象：code cert_failed

可能原因（逐条判定）：

1. 80 端口不通，证书机构验证不了 —— 判定：从外网 curl http://域名/.well-known/ 超时
2. 未备案被拦截 —— 判定：icp_blocked 的判定
3. 申请次数超了证书机构的限额 —— 判定：sudo docker logs acornfox-edge 里有 rate limit

排查顺序：

1. sudo docker logs --tail 50 acornfox-edge

处置：

- ［2 级 先问用户］改安全组或备案后重新绑定

- 必须先停下问用户：改安全组、解析之前
- 确认恢复：https://域名 能打开

### http_error 访问域名失败（其他原因）

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：看 message 里的原因；稍后再试
- 现象：code http_error

可能原因（逐条判定）：

1. 见 message —— 判定：message 原文

排查顺序：

1. 读 message

处置：

- ［1 级 可以直接做］稍后再试

- 必须先停下问用户：持续失败时带上证据包
- 确认恢复：再验证通过

### invalid_domain 域名写得不对

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：填完整的域名，例如 panel.example.com：小写，至少有一个点，不要带 http://、端口或路径，不能是 IP
- 现象：acornfox public-console enable、acornfox domain add 或 domain remove 返回 code invalid_domain（HTTP 400）

可能原因（逐条判定）：

1. 带了 http://、https://、端口或路径 —— 判定：message 里引号中的域名有 :// 或 / 或 :端口
2. 不是完整的域名（没有点、填了 IP、有空格或中文、超过 253 个字符） —— 判定：看 message 里引号中的域名

排查顺序：

1. 读 message 里的域名
2. 问用户要用的完整域名

处置：

- ［1 级 可以直接做］改成完整的域名后重新运行同一条命令

- 必须先停下问用户：不知道用户要用哪个域名时
- 确认恢复：命令不再报 invalid_domain

### domain_in_use 这个域名已经绑定给了一个应用

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：公网控制台换一个域名（例如 panel.加上原来的域名，要在域名服务商加解析）；要把域名从应用上解绑由用户决定
- 现象：acornfox public-console enable 返回 code domain_in_use（HTTP 409）

可能原因（逐条判定）：

1. 这个域名已经绑定到本机的某个应用，同一个域名不能同时给应用和公网控制台 —— 判定：acornfox domain list 能看到它

排查顺序：

1. acornfox domain list

处置：

- ［0 级 只读］告诉用户这个域名在给哪个应用用
- ［2 级 先问用户］请用户另起一个子域名给公网控制台（要在域名服务商加解析）
- ［2 级 先问用户］把域名从应用上解绑（应用就不能用这个域名访问了）

- 必须先停下问用户：加解析、解绑应用的域名之前
- 确认恢复：public-console enable 不再报 domain_in_use

### public_ip_unknown 查不到这台服务器的公网 IP

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：确认服务器有公网 IP、能访问外网，稍后再运行同一条命令
- 现象：acornfox public-console enable 返回 code public_ip_unknown（HTTP 502）

可能原因（逐条判定）：

1. 服务器没有公网 IP（只在内网，或经 NAT 上网） —— 判定：云厂商控制台看实例有没有公网 IP 或弹性 IP；问用户
2. 出不了外网（安全组出方向、代理、DNS） —— 判定：curl -sS -m 5 -o /dev/null -w '%{http_code}' https://acornfox.com 不是 200
3. 云厂商元数据和公共查询接口一时都没应答 —— 判定：过几分钟再运行就好了

排查顺序：

1. curl -sS -m 5 -o /dev/null -w '%{http_code}' https://acornfox.com
2. 问用户服务器有没有公网 IP

处置：

- ［1 级 可以直接做］稍后再运行同一条命令
- ［2 级 先问用户］给服务器绑公网 IP、改安全组出方向或代理

- 必须先停下问用户：要绑公网 IP（可能花钱）、改安全组或代理时
- 确认恢复：public-console enable 给出了要加的解析记录

### edge_unavailable 网关没准备好，设不了域名验证

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：sudo acornfox doctor 看网关和端口两项，按里面的编号处理后再运行同一条命令
- 现象：acornfox public-console enable 返回 code edge_unavailable（HTTP 502）

可能原因（逐条判定）：

1. 入口容器没在运行或还在准备［doctor 自动判定］ —— 判定：doctor 的入口代理一项（EDGE-01）
2. 网关的管理口被别的程序占了［doctor 自动判定］ —— 判定：doctor 的端口一项（SETUP-PORT-02）

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 doctor 给出的编号处理（EDGE-01、EDGE-03）
- ［1 级 可以直接做］入口还在准备时等一两分钟再试

- 必须先停下问用户：要停别的程序时
- 确认恢复：public-console enable 不再报 edge_unavailable
- 相关编号：EDGE-01、EDGE-03、SETUP-PORT-02

### verify_required 还没完成域名验证就要开通公网控制台

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：重新运行 acornfox public-console enable 域名 --json：它先验证域名，通过后才开通
- 现象：public-console enable 返回 code verify_required（HTTP 409）

可能原因（逐条判定）：

1. 验证过期了，或这期间后台服务重启过 —— 判定：重新运行 enable 会给一个新的验证
2. 直接调了开通接口，没先做验证 —— 判定：看调用顺序：先 POST verify，再 POST enable

排查顺序：

1. acornfox public-console status --json

处置：

- ［2 级 先问用户］重新运行 acornfox public-console enable 域名 --json

- 必须先停下问用户：开公网控制台就是对外开放：用户本来就要开的，那句话就是同意；不是用户要的不要开
- 确认恢复：enable 返回 ok:true

### PUBCON-01 在控制台页面开关公网控制台时，再输一次的控制台密码没输或不对

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 是什么：在“设置 › 访问”里开启或关闭公网控制台，要再输一次这次登录用的那个入口的密码：从局域网控制台登录的输局域网控制台密码，从公网控制台登录的输公网控制台密码。用 acornfox open 经 SSH 隧道进来的不用输。输错的次数和那个入口的登录页算在一起。
- 下一步：输这次登录用的控制台密码再试；忘了局域网控制台密码就在服务器上运行 acornfox lan-console reset-password 后重设，或者在服务器上用命令 acornfox public-console 开关
- 现象：页面开启或关闭公网控制台时提示“控制台密码不对”或“要再输一次现在的控制台密码”；POST /v1/public-console/enable 或 DELETE /v1/public-console 返回 code PUBCON-01（HTTP 403）

可能原因（逐条判定）：

1. 输的是别的密码（例如把公网控制台的密码输进了局域网控制台登录的页面） —— 判定：GET /v1/public-console 的 entry：lan 要局域网控制台密码，public 要公网控制台密码
2. 密码框是空的 —— 判定：message 是“要再输一次现在的控制台密码”
3. 忘了密码 —— 判定：问用户

排查顺序：

1. 看 GET /v1/public-console 的 entry 是哪种登录
2. 请用户输这次登录用的密码再试

处置：

- ［0 级 只读］请用户输对密码再试
- ［2 级 先问用户］在服务器上用 acornfox public-console enable 域名 或 disable 开关（不用密码）
- ［2 级 先问用户］在服务器上运行 acornfox lan-console reset-password 清掉局域网控制台密码后重设（所有记住的设备和登录会话都失效）

- 必须先停下问用户：要在服务器上开公网控制台或清密码之前
- 确认恢复：页面开启或关闭成功
- 相关编号：PUBCON-02

### PUBCON-02 控制台密码试错次数太多，暂时不让再试

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按 message 里的分钟数等一会儿再试；等的时候不要反复点，每次失败都会让等的时间变长
- 现象：页面开启或关闭公网控制台时提示“密码试错次数太多，请 N 分钟后再试”；返回 code PUBCON-02（HTTP 429）

可能原因（逐条判定）：

1. 这个地址连续输错了 5 次以上（和这个入口的登录页算在一起） —— 判定：message 里有要等的分钟数
2. 短时间内各处一共输错了很多次，整个入口暂停 10 分钟 —— 判定：换一台电脑也提示要等

排查顺序：

1. 读 message 里的分钟数

处置：

- ［0 级 只读］等够时间再试
- ［2 级 先问用户］急用时在服务器上用 acornfox public-console 命令开关（不用密码）

- 必须先停下问用户：要在服务器上开公网控制台之前；有人在反复猜密码时告诉用户
- 确认恢复：等过之后输对密码，开启或关闭成功
- 相关编号：PUBCON-01

### PUBCON-03 这台机器在局域网里，开不了公网控制台

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这台机器只有局域网地址，外面的人用域名连不到它，控制台页面上不能开公网控制台。已经开着的可以关。
- 下一步：在局域网里用局域网控制台（http://局域网地址:18802）；人在外面时在自己电脑上运行 acornfox open（经 SSH 隧道）
- 现象：“在外面打开控制台”一行写着“这台机器在局域网里，没有公网地址，开不了公网控制台”；POST /v1/public-console/enable 返回 code PUBCON-03（HTTP 409）

可能原因（逐条判定）：

1. 机器在家里或办公室的局域网里，没有公网地址 —— 判定：GET /v1/public-console 带 unsupported；acornfox doctor 里局域网控制台一项可用

排查顺序：

1. GET /v1/public-console 看 unsupported

处置：

- ［0 级 只读］告诉用户在局域网里用局域网控制台，在外面用 acornfox open

- 必须先停下问用户：用户坚持要从外面直接用网址打开时（要路由器端口转发和域名，风险自担），先说清楚再定
- 确认恢复：用户能用局域网控制台或 acornfox open 进控制台
- 相关编号：LAN-CONSOLE-01

### PUBCON-04 从公网控制台登录时，页面上只能关闭公网控制台

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 是什么：从公网控制台登录进来的页面，能关闭公网控制台，不能开启或换地址：换地址会让正在用的这次登录失效。
- 下一步：要换地址，从局域网控制台或用 acornfox open 进控制台再开；或者在服务器上运行 acornfox public-console enable 新域名
- 现象：POST /v1/public-console/enable 返回 code PUBCON-04（HTTP 403）

可能原因（逐条判定）：

1. 这次是从公网控制台的网址登录的 —— 判定：GET /v1/public-console 的 entry 是 public

排查顺序：

1. GET /v1/public-console 看 entry

处置：

- ［0 级 只读］告诉用户换一种方式进控制台再改
- ［2 级 先问用户］在服务器上运行 acornfox public-console enable 新域名

- 必须先停下问用户：换公网控制台地址之前
- 确认恢复：换了地址后新地址能打开

### PUBCON-05 忘了公网控制台的密码

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 是什么：公网控制台的密码是开启时生成的一串字，只存一份不可逆的摘要，找不回来，只能重新生成。登录页的“忘了密码”不能直接重新生成：不登录就能重设等于谁都能抢走控制台，所以只能在服务器上做。
- 下一步：在服务器上（经 SSH）运行 acornfox public-console enable 公网控制台的域名 --reset-password：生成新密码，只显示这一次，旧密码和旧的登录都立刻失效
- 现象：用户说“忘了公网控制台的密码”；登录页的“忘了密码”点开后的说明

可能原因（逐条判定）：

1. 生成的密码没保存 —— 判定：问用户；命令输出只显示一次，不会再显示
2. 密码被输错或被浏览器自动填错 —— 判定：让用户在别处输入框里看着打一遍再试

排查顺序：

1. acornfox public-console status --json 看域名和开没开
2. 问用户要不要让现在的登录都退出

处置：

- ［2 级 先问用户］acornfox public-console enable 域名 --reset-password（旧密码和旧登录立刻失效；用户说“忘了密码”就是同意）

- 必须先停下问用户：用户的原因不是“忘了密码”时先问；新密码只给用户看一次，不要写进对话记录
- 确认恢复：用新密码能登录公网控制台，旧密码不行
- 相关编号：PUBCON-01、LAN-CONSOLE-04
- 演练方法：在测试机上开启公网控制台后运行 acornfox public-console enable 域名 --reset-password，再用旧密码登录应当失败

### PDOMAIN-01 平台域名没有指到这台机器

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：随便一个 *.平台域名 下的名字，公共 DNS 查出来不是这台服务器的公网地址（或者查不到）。应用照常用原来带端口的地址打开，平台给的名字打不开、证书也签不下来。
- 下一步：到你买域名的地方加（或改）一条记录：名字 *.子域、类型 A、指向这台服务器的公网地址（acornfox domain base 给出完整的一条），等几分钟再检查
- 现象：控制台“设置 › 访问 › 域名”一行红字“还没生效：域名没有指到这台机器”；acornfox domain base 的“问题（PDOMAIN-01）”；doctor 的平台域名一项

可能原因（逐条判定）：

1. 还没有加 *.子域 这条记录，或者刚加、还没生效［doctor 自动判定］ —— 判定：acornfox domain base --json 的 problem.detail 写“还查不到”；dig +short 任意名字.平台域名 没有结果
2. 记录指向了别的地址（旧服务器、CDN、代理）［doctor 自动判定］ —— 判定：problem.detail 写“现在指向 X，应该指向 Y”
3. 只加了某一个名字（例如 hello.apps），没加通配的 *.apps —— 判定：dig +short 随便一个名字.平台域名 没结果，dig +short hello.平台域名 有结果
4. 这台服务器连不上公共 DNS 服务器［doctor 自动判定］ —— 判定：problem.text 是“查不了域名指向哪里”

排查顺序：

1. acornfox --json domain base
2. 看 problem.detail 和 record（要加的那一条）
3. dig +short 随便一个名字.平台域名

处置：

- ［0 级 只读］把 record 里的名字、类型、指向原样告诉用户，请他到域名服务商那里加好
- ［1 级 可以直接做］用户说加好了，等几分钟再运行 acornfox domain base（会重新检查）

- 必须先停下问用户：改解析要用户自己在域名服务商那里做；记录指向别处、开着 CDN 时先问用户能不能改
- 确认恢复：acornfox domain base 的第一项打勾“域名指过来了”
- 相关编号：PDOMAIN-03、PDOMAIN-04
- 演练方法：测试机上设一个没有解析的平台域名：acornfox domain base set nope.example.com

### PDOMAIN-02 这台机器没有公网地址，用不了平台域名

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 是什么：平台域名下的名字要从互联网申请证书、被人访问，局域网里的机器（家里、公司内网，没有公网地址）和 Mac、Windows 电脑这一版用不了。这不是坏了，是这一版不支持。
- 下一步：要在外面打开应用，用 acornfox open 应用名 经安全通道打开；要用域名，把应用装到有公网地址的云主机上
- 现象：控制台“域名”一行写“这台机器在局域网里，没有公网地址，用不了域名”，没有输入框；acornfox domain base set 返回 PDOMAIN-02

可能原因（逐条判定）：

1. 服务器在用户的局域网里（地址是 192.168.x.x、10.x.x.x 这类）［doctor 自动判定］ —— 判定：acornfox --json domain base 的 supported 是 false；acornfox --json status 的地址是私有地址
2. 这是一台 Mac 或 Windows 电脑［doctor 自动判定］ —— 判定：acornfox version 显示的系统
3. 安装时记下的服务器地址（--public-host）是内网地址，其实这台云主机有公网地址 —— 判定：云厂商控制台里这台机器有公网 IP，而 domain base 的 public_ip 是空的

排查顺序：

1. acornfox --json domain base
2. 问用户这台机器是云主机还是家里、公司里的电脑

处置：

- ［0 级 只读］照实告诉用户这一版用不了，给出 acornfox open 应用名 的办法
- ［2 级 先问用户］第三种原因：照 Skill 里 install-linux.md 的“改应用访问地址”那一节改 --public-host（要重启服务，先问用户）

- 必须先停下问用户：要改服务器地址、重启服务时
- 确认恢复：acornfox --json domain base 的 supported 是 true
- 演练方法：开发机（局域网）上运行 acornfox domain base

### PDOMAIN-03 外面连不上这台机器的 80 或 443

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：域名已经指过来了，但连这台服务器公网地址的 80 或 443 端口不通。证书要经 80 端口验证，访问走 443，所以平台给的名字打不开、证书也签不下来。
- 下一步：在云厂商控制台的安全组里放行入方向 TCP 80 和 443（来源 0.0.0.0/0）；服务器自己的防火墙开着的也要放行
- 现象：控制台“域名”一行展开后第二项是“外面连不上 80 / 443”；acornfox domain base 的“问题（PDOMAIN-03）”

可能原因（逐条判定）：

1. 云厂商安全组没有放行 80、443 —— 判定：problem.detail 写“超时”；云厂商控制台看安全组入方向规则
2. 服务器自己的防火墙（ufw、firewalld）没放行 —— 判定：sudo ufw status 或 sudo firewall-cmd --list-all
3. 80、443 被别的程序占着（宝塔、1Panel、nginx），入口代理没在听［doctor 自动判定］ —— 判定：problem.detail 写“被拒绝”；doctor 的 SETUP-PORT-01
4. 入口代理没在运行［doctor 自动判定］ —— 判定：doctor 的入口代理一项（EDGE-01）
5. 这台云主机从自己访问不了自己的公网地址（少数厂商的网络这样设计），其实外面是通的 —— 判定：在别的电脑上 curl -m 5 http://任意名字.平台域名 能连上

排查顺序：

1. acornfox --json domain base
2. sudo acornfox doctor
3. 问用户安全组有没有放行 80、443

处置：

- ［1 级 可以直接做］按 doctor 的编号处理入口代理、端口占用
- ［2 级 先问用户］改安全组、服务器防火墙

- 必须先停下问用户：改安全组、防火墙或停别的程序之前
- 确认恢复：acornfox domain base 的第二项打勾“外面能连上”
- 相关编号：SETUP-PORT-01、EDGE-01、port_unreachable

### PDOMAIN-04 平台域名下的证书没签下来

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：前两项都过了，可是某个应用在平台域名下的名字过了 10 分钟还没有有效的证书。浏览器打开会提示不安全。
- 下一步：中国大陆的服务器先确认域名已备案；再看入口代理的日志（sudo docker logs --tail 50 acornfox-edge）
- 现象：控制台“域名”一行展开后第三项是“证书没签下来”；acornfox domain base 的“问题（PDOMAIN-04）”

可能原因（逐条判定）：

1. 中国大陆服务器的域名没备案，80/443 被云厂商拦截 —— 判定：浏览器打开 http://应用名.平台域名 看到云厂商的拦截页
2. 证书签发方的限额：同一个主域名一周最多 50 张证书（平台域名换来换去、应用很多时会碰到） —— 判定：入口日志里有 rateLimited、too many certificates
3. 域名开了 CDN 或代理，验证请求没到这台机器 —— 判定：dig 结果不是这台服务器的地址
4. 入口代理出了问题［doctor 自动判定］ —— 判定：doctor 的入口代理一项（EDGE-*）

排查顺序：

1. acornfox --json domain base
2. sudo docker logs --tail 50 acornfox-edge
3. 问用户域名有没有备案

处置：

- ［0 级 只读］把日志里和证书有关的几行告诉用户
- ［2 级 先问用户］备案由用户自己办；碰到限额就等一周或给应用绑自己的域名

- 必须先停下问用户：要备案、换域名时
- 确认恢复：acornfox domain base 的第三项打勾“证书已自动申请”
- 相关编号：PDOMAIN-01、PDOMAIN-03、icp_blocked

### PDOMAIN-05 平台域名的写法不对

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 下一步：只填域名本身，例如 apps.example.com：不带 http://、不带路径和端口，不能是 IP 地址
- 现象：acornfox domain base set 或控制台保存时返回 PDOMAIN-05；平台域名的设置文件读不了（problem 写“设置文件读不了”）

可能原因（逐条判定）：

1. 带了 http:// 或 https://、路径、端口 —— 判定：看错误信息里引号中的写法
2. 填的是 IP 地址，或者没有点的名字（localhost） —— 判定：看错误信息
3. 数据目录里的 platform-domain.json 坏了 —— 判定：acornfox --json domain base 的 problem.detail 写“损坏”

排查顺序：

1. 把用户给的写法去掉协议、路径、端口再试

处置：

- ［1 级 可以直接做］按正确写法重新运行 acornfox domain base set 域名（文件坏了也这样做，会覆盖它）

- 必须先停下问用户：拿不准用户要的是哪个域名时
- 确认恢复：acornfox domain base set 成功

### PDOMAIN-06 某个应用用不了平台给的名字

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 是什么：应用名.平台域名 这个名字已经被别的应用自己绑走了，或者是公网控制台的地址，或者加起来超过 253 个字符。这个应用仍用原来的地址，别的应用不受影响。
- 下一步：给这个应用单独绑一个自己的域名（acornfox domain add 域名），或者让占着这个名字的一方换一个
- 现象：应用“设置 › 谁能打开”里平台给的那一行写“已经被…用了”或“太长”；acornfox domain base 的应用列表里这一行带 PDOMAIN-06

可能原因（逐条判定）：

1. 另一个应用自己绑了 这个应用名.平台域名 —— 判定：acornfox domain list（在那个应用的目录里）或看应用详情的 domains
2. 应用名正好是 console，公网控制台开在 console.平台域名 —— 判定：acornfox public-console status
3. 应用名加平台域名太长 —— 判定：数一下长度

排查顺序：

1. acornfox --json domain base 看 apps 里这一行的 note

处置：

- ［0 级 只读］告诉用户原因和两个办法
- ［2 级 先问用户］给应用绑自己的域名或删掉另一个应用绑的域名

- 必须先停下问用户：要改别的应用的域名时
- 确认恢复：应用详情的 platform_domain.active 是 true

### PDOMAIN-07 换掉或清掉平台域名要先确认

- 分组：域名和公网控制台
- 默认等级：P3（提示或用法问题，不急）
- 是什么：已经有平台域名，要换成别的或清掉：所有应用在旧域名下的地址都会打不开（装应用时写进应用里的旧地址也不会跟着变）。所以这一步要用户明确同意，接口不带确认一律拒绝。
- 下一步：先运行 acornfox domain base set 新域名 --plan（或 clear --plan）把会怎样告诉用户；用户同意后加 --yes 再运行
- 这一项要用户自己确认或操作
- 现象：acornfox domain base set 或 clear 返回 PDOMAIN-07

可能原因（逐条判定）：

1. 换掉或清掉时没有带 --yes（接口请求体没带 "confirm": true） —— 判定：看命令有没有 --yes

排查顺序：

1. acornfox domain base set 新域名 --plan

处置：

- ［0 级 只读］把 --plan 的输出告诉用户
- ［2 级 先问用户］用户同意后加 --yes 再运行

- 必须先停下问用户：总是：换掉、清掉平台域名都要用户同意
- 确认恢复：acornfox domain base 显示新的平台域名（或“还没有设平台域名”）

## 应用数据备份和恢复（BKP）

### BKP-01 磁盘空间不够，不能做备份或恢复

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：做备份要在备份目录所在的磁盘上放下一份应用数据的压缩包；恢复还要先给现在的数据另存一份，并在放 Docker 数据的磁盘上放下备份里的数据。备份目录放不下时，如果有自动另存的旧备份（升级前、恢复前），会先清掉最旧的再做（不清最新的一份、不清正在用来恢复的那份；计划里的 will_prune_for_space 和 say 写明清哪几份，任务结果的 pruned 列出清掉的）；清光能清的还不够才拒绝。空间不够时在动应用之前就拒绝，应用没有被暂停。doctor 在备份目录所在磁盘只剩 1 GB 以内时也会提醒。
- 下一步：先清理磁盘（删掉不用的旧备份：acornfox backup rm 应用名 备份编号 --yes），或把备份目录所在的磁盘扩一扩，再重试；没有动过应用
- AcornFox 已经自动做的：没有动应用，也没有留下半截文件
- 现象：acornfox backup create 或 restore 返回 BKP-01（HTTP 507）；acornfox backup create --plan 的“磁盘空间不够：需要约…，只剩…”；acornfox doctor 的“备份目录所在磁盘只剩…”

可能原因（逐条判定）：

1. 备份目录所在的磁盘快满了［doctor 自动判定］ —— 判定：acornfox backup list 看“所在磁盘剩…”；df -h 数据目录（默认 /var/lib/acornfox）
2. 旧备份占了太多空间 —— 判定：acornfox backup list 看总大小和每个应用的份数；手动备份默认每个应用留 5 份；自动另存的（升级前、恢复前）至少留 7 天、每种最新 3 份也留着，清掉最旧的自动另存的也不够才会报这个编号
3. 恢复时放 Docker 数据的磁盘不够 —— 判定：message 写了需要多少、剩多少；df -h /var/lib/docker
4. 应用的数据本身很大 —— 判定：acornfox backup create 应用 --plan 看“数据约…”

排查顺序：

1. acornfox backup create 应用名 --plan
2. acornfox backup list
3. df -h 备份目录和 Docker 数据目录

处置：

- ［0 级 只读］把 --plan 的数字和要删的旧备份告诉用户
- ［2 级 先问用户］用户同意后删旧备份：acornfox backup rm 应用名 编号 --yes
- ［2 级 先问用户］磁盘不够大就请用户换大一点的磁盘（要花钱的事由用户决定）

- 必须先停下问用户：删任何备份之前；要换磁盘之前
- 确认恢复：acornfox backup create 应用名 --plan 的“够不够”是够
- 演练方法：测试机上用大文件把数据目录所在的磁盘填到只剩很少，再运行 acornfox backup create 应用名 --plan

### BKP-02 读数据卷或写备份文件时出错

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：备份在打包数据卷的时候出了错：读数据卷失败、执行器断了、或者写出来的文件读不完整。应用已经照原样重新启动，没有留下半截的备份。
- 下一步：看 acornfox logs 和备份目录的 operations.log；应用已经照原样重新启动，重试一次，还不行就把日志给负责的人
- AcornFox 已经自动做的：应用已经重新启动，半截的备份文件已删掉
- 现象：acornfox backup create 的任务失败，故障编号 BKP-02；恢复时“把备份写回数据卷…出错”（随后会自动退回，见 BKP-07）

可能原因（逐条判定）：

1. 执行器（acornfox-runner）中途停了或重启了［doctor 自动判定］ —— 判定：systemctl status acornfox-runner；acornfox status
2. Docker 出了问题（磁盘满、守护进程重启） —— 判定：acornfox doctor 的 Docker 一项；docker info
3. 容器停不下来或数据卷被别的东西占着 —— 判定：message 写“停不了容器”或“清不掉数据卷”；docker ps -a
4. 备份目录所在的磁盘写满了 —— 判定：df -h 备份目录；这时通常报 BKP-01

排查顺序：

1. acornfox backup task 任务编号 看是哪一步
2. acornfox status 应用名 确认应用在运行
3. tail 备份目录的 operations.log
4. acornfox doctor

处置：

- ［1 级 可以直接做］应用没在运行就 acornfox start 应用名
- ［1 级 可以直接做］重试一次 acornfox backup create 应用名

- 必须先停下问用户：重试两次还是同一个错：带上任务的 JSON 和 operations.log 的最后 30 行问负责的人
- 确认恢复：acornfox backup create 应用名 的任务成功，acornfox backup list 里有新的一份
- 相关编号：BKP-01、BKP-07
- 演练方法：备份进行中在测试机上 sudo systemctl restart acornfox-runner

### BKP-03 这个应用正忙：在备份、恢复或部署

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：同一个应用同一时间只做一件备份或恢复的事，而且正在部署的应用不能备份、恢复，删除应用也要等备份或恢复做完。
- 下一步：等这件事做完再试（acornfox backup task 任务编号 看进度）
- 现象：acornfox backup create、restore、rm 或 delete 返回 BKP-03（HTTP 409）

可能原因（逐条判定）：

1. 上一份备份或一次恢复还没做完［doctor 自动判定］ —— 判定：acornfox backup list 应用名 最后一行“正在做：任务 …”
2. 应用正在部署（装、升级、重新部署） —— 判定：acornfox status 应用名 的部署状态

排查顺序：

1. acornfox backup list 应用名
2. acornfox backup task 任务编号

处置：

- ［0 级 只读］等任务做完（轮询 acornfox backup task，不写固定等待）再重试

- 必须先停下问用户：任务一小时还没结束：带任务 JSON 问负责的人
- 确认恢复：acornfox backup list 应用名 不再有“正在做”
- 演练方法：对同一个应用连着运行两次 acornfox backup create 应用名 --no-wait

### BKP-04 找不到这份备份

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：应用没有这个编号的备份：编号写错了，或者它已经被删掉（超出保留份数时旧的会自动清掉）。
- 下一步：acornfox backup list 应用名 看现有的备份和编号
- 现象：acornfox backup show、restore、rm 返回 BKP-04（HTTP 404）；编号写 latest 但应用还没有备份

可能原因（逐条判定）：

1. 编号写错了（编号形如 20261124-153012-ab12） —— 判定：对照 acornfox backup list 应用名 的第一列
2. 这份备份被按保留份数自动清掉了 —— 判定：operations.log 里有“按保留份数清掉旧备份”这一行
3. 应用名写错了 —— 判定：acornfox backup list 不带应用名看有哪些应用有备份

排查顺序：

1. acornfox backup list
2. acornfox backup list 应用名

处置：

- ［0 级 只读］用 list 里有的编号重试

- 必须先停下问用户：用户要的那一份确实没有了：告诉用户，问要不要用别的一份
- 确认恢复：acornfox backup show 应用名 编号 能看到清单
- 演练方法：acornfox backup show 应用名 20000101-000000-0000

### BKP-05 备份文件不完整或被改过，不能用来恢复

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：备份的清单（meta.json）里记着每个数据文件的大小和校验和，恢复前会逐个核对：缺文件、大小不对、压缩包读不完整、校验和不一致都算。这份备份不会被用来恢复，现在的数据没有动。
- 下一步：这份备份不能用来恢复，现在的数据没动；换一份备份，或重新做一份
- AcornFox 已经自动做的：恢复在动任何东西之前就停下了
- 现象：acornfox backup restore 返回 BKP-05（HTTP 422）；acornfox backup show 应用 编号 --verify 写文件校验不对

可能原因（逐条判定）：

1. 备份文件被人改过、删过，或磁盘出了坏块［doctor 自动判定］ —— 判定：acornfox backup show 应用 编号 --verify 指出是哪个文件
2. 做备份时服务被强行关掉，留下不完整的文件 —— 判定：备份目录里有以 .partial 结尾的目录（doctor 会提醒，BKP-10）
3. 备份目录被手动拷贝或移动过，文件没拷全 —— 判定：对比 meta.json 里每个文件的大小和 ls -l

排查顺序：

1. acornfox backup show 应用 编号 --verify
2. acornfox backup list 应用 看有没有别的备份

处置：

- ［0 级 只读］换一份校验通过的备份重新 --plan 再恢复
- ［1 级 可以直接做］给应用重新做一份备份
- ［2 级 先问用户］删掉坏的这份：acornfox backup rm 应用 编号 --yes

- 必须先停下问用户：删这份坏备份之前；所有备份都坏了、又必须恢复时
- 确认恢复：acornfox backup show 应用 编号 --verify 写“校验和全部核对过”
- 演练方法：sudo 把备份目录里 vol-0.tar.gz 截短一截，再 acornfox backup restore 应用 编号 --yes

### BKP-06 恢复和删除备份都要用户先同意

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：恢复会用备份里的数据覆盖应用现在的数据（备份之后新写的数据会没有），删除备份后文件找不回来，所以接口不带确认一律拒绝，命令行要加 --yes。
- 下一步：恢复会用备份里的数据覆盖现在的数据：先告诉用户，用户同意后加 --yes 再运行（先 --plan 看会怎样）
- 这一项要用户自己确认或操作
- 现象：acornfox backup restore 或 rm 返回 BKP-06（HTTP 409）

可能原因（逐条判定）：

1. 没有带 --yes（接口请求体没带 confirm: true，删除没带 ?confirm=true） —— 判定：看命令有没有 --yes

排查顺序：

1. acornfox backup restore 应用 编号 --plan

处置：

- ［0 级 只读］把 --plan 的输出原样告诉用户
- ［2 级 先问用户］用户明确同意后加 --yes 再运行

- 必须先停下问用户：总是：恢复、删备份都要用户先同意，并说清楚会覆盖现在的数据、备份之后的新数据会没有
- 确认恢复：acornfox backup restore 应用 编号 --yes 的任务开始，最后写“恢复完成”
- 演练方法：acornfox backup restore 应用 编号（不加 --yes）

### BKP-07 恢复没成功，数据已经自动退回到恢复之前

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：恢复在换上备份的数据之后，应用没能起来（健康检查没过，或某一步出错）。恢复前另存的那份备份已经被用来把数据和版本退回到恢复之前的样子，应用在运行。恢复前的那份备份还留着。
- 下一步：数据已经退回到恢复之前的样子；看任务里写的失败原因，处理后再试
- AcornFox 已经自动做的：已用恢复前的备份把数据、变量和版本退回，应用重新启动并通过健康检查
- 现象：acornfox backup restore 的任务失败，故障编号 BKP-07，任务结果 rolled_back 是 true

可能原因（逐条判定）：

1. 备份里的数据和现在要启动的版本对不上，应用起不来 —— 判定：任务的失败原因里写了应用没能起来；acornfox logs 应用名 看启动日志
2. 备份是应用有问题时做的，数据本身就坏了 —— 判定：acornfox backup show 应用 编号 看做备份的时间，对照应用出问题的时间
3. 备份时的程序版本和数据库结构对不上现在的配置 —— 判定：acornfox backup show 应用 编号 的版本和 acornfox versions 应用 对比
4. 磁盘、内存不够，应用启动超时 —— 判定：acornfox doctor；acornfox stats 应用

排查顺序：

1. acornfox backup task 任务编号 看失败的是哪一步和原因
2. acornfox status 应用名 确认应用在运行、数据是恢复前的
3. acornfox logs 应用名

处置：

- ［0 级 只读］把失败原因告诉用户，确认应用现在正常
- ［2 级 先问用户］换一份更早的备份再 --plan、再恢复（要用户同意）

- 必须先停下问用户：要换别的备份重试、或决定不恢复了
- 确认恢复：acornfox status 应用名 是运行中；acornfox backup list 应用名 里能看到“恢复前”那份
- 相关编号：BKP-08、BKP-09
- 演练方法：sudo 编辑备份目录里的 records.json，把 app.health_path 改成 /不存在的路径，再 acornfox backup restore 应用 编号 --yes

### BKP-08 恢复没成功，自动退回也没成功

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：恢复失败后，用恢复前另存的备份退回时又出了错。应用现在的数据处在不确定的状态。恢复前的那份备份（任务结果里的 pre_restore_id）还在，里面是恢复之前的数据。
- 下一步：不要再动这个应用；恢复之前的备份还在（编号见任务结果的 pre_restore_id），马上告诉负责的人
- 这一项要用户自己确认或操作
- 现象：acornfox backup restore 的任务失败，故障编号 BKP-08；acornfox doctor 的“恢复没成功，自动退回也没成功”

可能原因（逐条判定）：

1. 退回时执行器或 Docker 出了问题 —— 判定：acornfox doctor；operations.log 里“自动退回也没成功”后面写的原因
2. 恢复前没能做备份（现在没有数据可备份、或磁盘不够），所以没法退回 —— 判定：任务结果里 pre_restore_id 是空的
3. 退回后的应用还是起不来 —— 判定：任务 JSON 里 rollback 一步的 detail

排查顺序：

1. acornfox backup task 任务编号 --json
2. acornfox backup list 应用名 找“恢复前”那份
3. acornfox doctor

处置：

- ［0 级 只读］把任务 JSON、备份编号和 operations.log 的最后 30 行交给负责的人
- ［2 级 先问用户］负责的人确认后，用 pre_restore_id 那份备份再恢复一次：acornfox backup restore 应用 编号 --yes

- 必须先停下问用户：总是：这是需要人来处理的故障，不要自己反复重试
- 确认恢复：应用恢复运行，数据是用户认可的那一份
- 相关编号：BKP-07
- 演练方法：纸上演练：恢复进行中让 Docker 重启（会同时打断退回）；不在测试机上真做

### BKP-09 备份时那个版本的程序已经不在这台机器上

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：恢复要把应用换回备份时的版本，但那一版的程序镜像已经被清掉了（升级后旧版本只保留最近几版）。直接拿现在的版本去用旧数据可能起不来，所以默认拒绝。
- 下一步：备份时的程序版本已不在本机；要在现在的版本上用这份数据，加 --allow-current-version（先问用户）
- 现象：acornfox backup restore 返回 BKP-09（HTTP 409）；acornfox backup show 应用 编号 写“程序版本已不在本机”

可能原因（逐条判定）：

1. 应用升级过几次，备份时的版本被自动清理了［doctor 自动判定］ —— 判定：acornfox versions 应用名 里没有备份时的那一版
2. 手动删过镜像 —— 判定：docker images 里没有 acornfox/应用名 的那个镜像

排查顺序：

1. acornfox backup show 应用 编号
2. acornfox versions 应用名

处置：

- ［0 级 只读］告诉用户这份备份的版本和现在的版本，问要不要在现在的版本上用这份数据
- ［2 级 先问用户］用户同意后加 --allow-current-version 再恢复

- 必须先停下问用户：要加 --allow-current-version 之前：数据库结构可能和现在的版本对不上，恢复失败会自动退回
- 确认恢复：acornfox backup restore 应用 编号 --plan 的“版本”一项显示可用，或带 --allow-current-version 恢复成功
- 相关编号：BKP-07

### BKP-10 备份或恢复被中断，留下半截东西

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：服务在备份或恢复的中途重启了（或被关掉）。备份会留下以 .partial 结尾的半截目录，它不会出现在备份列表里；任务会被标成失败。恢复被打断时应用可能停在换数据的中途，要看恢复前那份备份。
- 下一步：运行 acornfox doctor 看有没有半截的备份；重新做一份备份，残留的会自己清掉
- AcornFox 已经自动做的：再给这个应用做一次备份时，残留的半截目录会自动删掉；应用的容器由后台程序重新启动
- 现象：acornfox doctor 写“有 N 份没做完的备份”或“被服务重启打断了”；acornfox backup task 写任务失败，故障编号 BKP-10

可能原因（逐条判定）：

1. 备份进行中服务重启、升级或机器重启［doctor 自动判定］ —— 判定：任务的失败原因写“服务重启了，这件备份或恢复中途被打断”
2. 进程被强行杀掉、断电 —— 判定：备份目录下有 .partial 目录；journalctl -u acornfox-server 看有没有异常退出

排查顺序：

1. acornfox doctor
2. acornfox status 应用名 看应用在不在运行
3. acornfox backup list 应用名 看恢复前的那份在不在

处置：

- ［1 级 可以直接做］应用没在运行就 acornfox start 应用名
- ［1 级 可以直接做］给应用重新做一份备份（会清掉半截的）
- ［2 级 先问用户］被打断的是恢复：看应用数据是不是用户要的；不是就用“恢复前”那份备份恢复（要用户同意）

- 必须先停下问用户：被打断的是恢复，而且应用状态不对时
- 确认恢复：acornfox doctor 不再提醒；应用在运行
- 相关编号：BKP-08
- 演练方法：备份进行中 sudo systemctl restart acornfox-server

### BKP-11 这台机器上的执行器还不支持备份

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：备份要用执行器（acornfox-runner）读写数据卷。执行器比主程序旧（升级到一半、或只升级了一部分）时它不认识这些请求。
- 下一步：升级 AcornFox 到最新版（acornfox 的执行器要和主程序同版本）
- 现象：acornfox backup 返回 BKP-11（HTTP 501）；命令行说“服务器上的 AcornFox 版本较旧，不支持应用数据备份”（那是 server_too_old，不是这个编号）

可能原因（逐条判定）：

1. 升级后执行器还没重启，跑着旧版本［doctor 自动判定］ —— 判定：systemctl status acornfox-runner 的启动时间早于升级时间
2. 主程序和执行器版本不一致 —— 判定：acornfox version（服务器上）和 acornfox status 里的版本

排查顺序：

1. acornfox version
2. systemctl status acornfox-runner acornfox-server

处置：

- ［2 级 先问用户］重新运行升级命令（get.sh | sudo bash -s -- upgrade）

- 必须先停下问用户：重启服务之前（会让应用的管理界面短暂不可用）
- 确认恢复：acornfox backup create 应用名 --plan 能返回计划

### BKP-12 这个应用没有装在这台机器上

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：做备份、恢复要有这个应用：应用名写错了，或者应用已经删除。删除应用不会删它的备份，但恢复需要应用存在。
- 下一步：恢复要先有这个应用：先重新安装一个同名应用（acornfox install），再恢复
- 现象：acornfox backup create 或 restore 返回 BKP-12（HTTP 404）；acornfox backup list 写“应用已经不在这台机器上，下面是它留下的备份”

可能原因（逐条判定）：

1. 应用名写错了 —— 判定：acornfox apps 看现有的应用
2. 应用已经被删除（备份还留着） —— 判定：acornfox backup list 应用名 写应用已不在，但有备份

排查顺序：

1. acornfox apps
2. acornfox backup list

处置：

- ［0 级 只读］应用名写错就改正再试
- ［0 级 只读］应用被删了：告诉用户先重新安装同名应用（acornfox install），装好后再恢复

- 必须先停下问用户：重新安装之前（安装会新建一份空数据，恢复会覆盖它）
- 确认恢复：acornfox apps 里有这个应用，acornfox backup restore 应用 编号 --plan 能返回计划
- 演练方法：acornfox backup create 不存在的应用名

### BKP-13 这个应用没有数据可备份

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：应用没有登记数据卷（也没有附加数据库），数据都在容器里，容器一重建就没有，所以没有东西可备份；或者应用还没有跑起来过，没有程序镜像可以打开数据卷。
- 下一步：这个应用没有存数据的目录（数据卷），没有东西可备份；先让应用跑起来、产生数据，或确认它的数据目录登记过（acornfox volume add）
- 现象：acornfox backup create 返回 BKP-13（HTTP 409）

可能原因（逐条判定）：

1. 应用只是个无状态的网页或接口，没有要保存的数据 —— 判定：acornfox apps 看应用详情的数据目录是空的
2. 应用把数据写在了没登记的目录里（重建容器就丢） —— 判定：acornfox diagnose 里有 unpersisted_database 的提醒
3. 应用还没有成功上线过，没有镜像 —— 判定：acornfox versions 应用名 没有可用的版本

排查顺序：

1. acornfox status 应用名
2. acornfox diagnose

处置：

- ［0 级 只读］把原因告诉用户
- ［2 级 先问用户］数据目录没登记：acornfox volume add 路径 后重新部署（要用户同意，会重新部署）

- 必须先停下问用户：要登记数据目录、重新部署之前
- 确认恢复：acornfox backup create 应用名 --plan 列出了数据卷

### BKP-14 备份目录写不了

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：备份存在服务器数据目录下的 app-backups/ 里。这个目录建不了、写不进去（权限、磁盘只读、磁盘错误）时不能做备份。acornfox doctor 每次都会试着写一下；备份目录能写时它也用这个编号打一行“可写”。
- 下一步：检查备份目录的权限和所在磁盘（acornfox doctor 会指出是哪个目录）
- 现象：acornfox backup create 返回 BKP-14；acornfox doctor 的“备份目录 … 写不进去”

可能原因（逐条判定）：

1. 目录的所有者或权限被改过（后台服务的账号写不进去）［doctor 自动判定］ —— 判定：ls -ld 数据目录/backups；后台服务的账号见 systemctl cat acornfox-server
2. 磁盘被挂成只读，或有文件系统错误 —— 判定：mount | grep 数据目录所在磁盘；dmesg | tail
3. 磁盘满了（同时会报 BKP-01） —— 判定：df -h 数据目录

排查顺序：

1. sudo acornfox doctor
2. ls -ld 数据目录/backups
3. df -h 数据目录

处置：

- ［0 级 只读］把目录和磁盘的情况告诉用户
- ［2 级 先问用户］改目录权限、重新挂载磁盘

- 必须先停下问用户：改权限、挂载、动磁盘之前
- 确认恢复：sudo acornfox doctor 的备份目录一行打勾
- 相关编号：BKP-01

### BKP-15 恢复之后，有些服务连接和账号要重新接

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：恢复把应用的数据换回了备份时的样子，服务登记表没有动（里面的账号和连接是和别的应用共用的）。如果这个应用是数据库这类服务的提供方，备份之后才发给别的应用的账号，在恢复回来的数据里已经没有了；备份之后换过的连接也可能和恢复回来的设置对不上。这不算恢复失败，是提醒。
- 下一步：照提示的应用逐个重新接上服务（acornfox services 看，装应用时会重新发账号）
- 现象：acornfox backup restore 完成后的“要留意：”里带 BKP-15；任务结果的 warnings 里带 BKP-15；某个用这个应用做服务的应用恢复后连不上数据库

可能原因（逐条判定）：

1. 备份之后才有应用接上了这个应用提供的服务，它的账号不在备份里［doctor 自动判定］ —— 判定：warnings 里列出了应用和服务名；acornfox services show 服务名 看谁在用
2. 备份之后这个应用提供的服务被删过或重装过 —— 判定：warnings 写“现在不在登记表里”；acornfox services list
3. 备份之后这个应用自己换过连接的服务或账号 —— 判定：warnings 写“连接在备份之后换过”；acornfox services 看它现在用哪条

排查顺序：

1. acornfox backup task 任务编号 看 warnings
2. acornfox services list
3. acornfox status 受影响的应用

处置：

- ［0 级 只读］把受影响的应用列给用户
- ［2 级 先问用户］受影响的应用重新接服务：删掉后重新安装（数据保留则选接旧数据），装的时候平台会重新发账号（要用户同意）

- 必须先停下问用户：要删除或重装任何受影响的应用之前
- 确认恢复：受影响的应用重新连上服务，acornfox status 正常
- 相关编号：CAT-SVC-08

### BKP-16 备份的请求写得不对

- 分组：应用数据备份和恢复（BKP）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：备份编号的写法不对（形如 20261124-153012-ab12），备份种类不是 manual、pre-upgrade、pre-restore 之一，或者备注超过 200 个字。
- 下一步：按提示改请求后重试
- 现象：acornfox backup 返回 BKP-16（HTTP 400）

可能原因（逐条判定）：

1. 编号写错或带了多余的字符 —— 判定：对照 acornfox backup list 应用名 的第一列
2. 备注太长 —— 判定：message 写“备注太长”

排查顺序：

1. 读 message
2. acornfox backup list 应用名

处置：

- ［0 级 只读］照 message 改命令再运行

- 必须先停下问用户：拿不准用户要哪一份备份时
- 确认恢复：命令成功
- 演练方法：acornfox backup show 应用 abc

## 打开应用

### OPEN-APP-01 应用还没有访问端口

- 分组：打开应用
- 默认等级：P3（提示或用法问题，不急）
- 下一步：运行 acornfox status 应用名 看状态；部署成功后再打开
- 现象：acornfox open 应用名 报 OPEN-APP-01

可能原因（逐条判定）：

1. 应用没部署成功，或只在容器网络里 —— 判定：acornfox status 应用名 --json

排查顺序：

1. acornfox status 应用名

处置：

- ［1 级 可以直接做］先让应用部署成功

- 必须先停下问用户：—（不用问）
- 确认恢复：acornfox open 应用名 能打开

### OPEN-APP-02 经网址登记的服务器上，只本机的应用打不开

- 分组：打开应用
- 默认等级：P3（提示或用法问题，不急）
- 下一步：用 acornfox target add 名称 --ssh 用户@主机 登记这台服务器后再运行 acornfox open 应用名；或经用户同意改成局域网：acornfox app set --app 应用名 --expose lan
- 现象：acornfox open 应用名 报 OPEN-APP-02

可能原因（逐条判定）：

1. 服务器是用网址（不是 SSH）登记的，没法开 SSH 隧道 —— 判定：acornfox target list 看登记方式

排查顺序：

1. acornfox target list

处置：

- ［1 级 可以直接做］用 SSH 再登记一次
- ［2 级 先问用户］把应用改成局域网可见

- 必须先停下问用户：改应用对外方式之前
- 确认恢复：acornfox open 应用名 能打开
- 相关编号：LAN-CONSOLE-01

### OPEN-APP-03 应用只在内部网络，没有网页可以打开

- 分组：打开应用
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用打开它：它是给别的应用用的（例如数据库、缓存），别的应用按它的名字在内部网络里连它；要用它的应用看 acornfox status 那个应用名 里的服务
- 现象：acornfox open 应用名 报 OPEN-APP-03；acornfox status 应用名 写着“只在内部网络，给别的应用用”，没有网址

可能原因（逐条判定）：

1. 应用清单把访问方式写成 internal（数据库、缓存这类不说 HTTP 的应用） —— 判定：acornfox status 应用名 --json 里 internal_only 是 true

排查顺序：

1. acornfox status 应用名 --json

处置：

- ［0 级 只读］告诉用户这个应用没有网页，是给别的应用用的

- 必须先停下问用户：—（不用问）
- 确认恢复：用户知道了它不用打开；要用它的应用能正常打开
- 相关编号：OPEN-APP-01
- 演练方法：测试机上装一个访问方式是 internal 的应用（例如数据库），运行 acornfox open 它的名字

## 命令行在这台电脑上的操作（安装 Skill）

### install_failed Skill 一个也没装上

- 分组：命令行在这台电脑上的操作（安装 Skill）
- 出现在：diagnosis.stage 为 skill
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不加 --json 再运行一次 acornfox skill install，看每个工作台后面写的原因；也可以 acornfox skill install --dir 一个能写的 skills 目录，或 acornfox skill print 把内容交给工作台
- 现象：acornfox skill install --json 返回 diagnosis.code install_failed（退出码 1）

可能原因（逐条判定）：

1. skills 目录没有写权限（例如以前用 sudo 建过） —— 判定：ls -ld ~/.claude/skills ~/.codex/skills 属主是 root
2. 磁盘满了 —— 判定：df -h ~
3. --dir 指的位置建不了目录 —— 判定：不加 --json 再运行，看 ✗ 那一行

排查顺序：

1. acornfox skill install（不加 --json）
2. ls -ld ~/.claude/skills ~/.codex/skills

处置：

- ［0 级 只读］acornfox skill print 把内容交给工作台
- ［1 级 可以直接做］acornfox skill install --dir 一个能写的 skills 目录
- ［2 级 先问用户］用 sudo 改 skills 目录的属主

- 必须先停下问用户：要用 sudo 改文件时
- 确认恢复：acornfox skill install --json 的 installed 里有工作台

### CLI-UPDATE-01 服务器上的 AcornFox 比这台电脑上的命令行新

- 分组：命令行在这台电脑上的操作（安装 Skill）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：命令行连上服务器时问一下服务器的版本；服务器的版本号比自己高，就提醒更新。每台电脑每天最多提醒一次，问版本本身一小时最多一次，记在配置目录的 update-notice.json 里。只是提醒：命令照常运行。电脑上的命令行和 Skill 不会跟着服务器升级，所以要自己更新。
- 下一步：在这台电脑上更新命令行：国内 curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli，海外把地址换成 https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh；再运行 acornfox skill install 更新 AI 工作台里的 Skill
- AcornFox 已经自动做的：没有；提醒之后命令照常运行
- 现象：命令的标准错误里有“提醒：服务器 … 上的 AcornFox 是 … 版，这台电脑上的命令行是 … 版，比服务器旧（CLI-UPDATE-01…）”；--json 的结果里有 notices 字段，里面一项的 code 是 CLI-UPDATE-01

可能原因（逐条判定）：

1. 服务器升级了，电脑上的命令行还是装服务器时的版本 —— 判定：acornfox --json version 的 version 比服务器上 ssh 用户@服务器 acornfox --json version 的低
2. 同一台服务器被几台电脑管，这一台没更新 —— 判定：对比各台电脑的 acornfox --json version

排查顺序：

1. acornfox --json version
2. ssh 用户@服务器 acornfox --json version（服务器的版本）

处置：

- ［2 级 先问用户］在这台电脑上运行上面的更新命令（换掉电脑上的命令行程序）
- ［1 级 可以直接做］acornfox skill install（更新 Skill 文件）
- ［0 级 只读］不想看到提醒：设环境变量 ACORNFOX_NO_UPDATE_NOTICE=1

- 必须先停下问用户：要更新电脑上的命令行之前（它换掉电脑上的程序）
- 确认恢复：再运行命令不再出现提醒；acornfox --json version 的版本和服务器的一致
- 相关编号：version_mismatch、SETUP-SKILL-01
- 演练方法：用版本号编低的命令行连测试机（构建时 -ldflags 把 internal/cli.Version 设成 0.0.1），删掉配置目录里的 update-notice.json 后运行 acornfox status

## 应用中心安装（CAT-INST）

### CAT-INST-01 目录里没有这个应用、配方或版本

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：目录里没有这个应用、配方或版本
- 下一步：acornfox catalog list 看有哪些；目录太旧就 acornfox catalog sync
- 现象：acornfox install 应用 或页面安装返回 CAT-INST-01（HTTP 404）

可能原因（逐条判定）：

1. 应用名、配方名或版本号写错 —— 判定：acornfox catalog list --json 里找不到这个 id
2. 手上的目录太旧，还没有这个应用或版本 —— 判定：acornfox catalog sync 后再 catalog list 能找到
3. 应用在私有目录里，但没写 --catalog 或目录已断开 —— 判定：acornfox catalog sources
4. 应用中心里一个能装的应用都没有（刚升级到新格式的平台：程序自带的目录是空的，以前那份旧格式的目录不会再用），这时同步目录也没用 —— 判定：message 以“应用中心现在还没有可以安装的应用”开头；acornfox catalog list 也这样写

排查顺序：

1. acornfox catalog list
2. acornfox catalog sync
3. acornfox catalog sources

处置：

- ［1 级 可以直接做］改对名字或版本后重装
- ［1 级 可以直接做］acornfox catalog sync
- ［0 级 只读］message 说应用中心现在还没有可以安装的应用的：照原样告诉用户，要等应用按新格式重新上架；不用同步、不用重试

- 必须先停下问用户：—（不用问）
- 确认恢复：acornfox catalog info 应用 能返回

### CAT-INST-02 安装参数不对

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：参数不对：没有这个参数、这个参数由平台生成不能填、格式不符或缺了必填项
- 下一步：按 message 改 --set；acornfox catalog info <应用> 看参数。必填项要用户提供内容的（例如要用的模型名），先问用户
- 现象：安装返回 CAT-INST-02（HTTP 400），message 写了是哪个参数

可能原因（逐条判定）：

1. --set 写了清单里没有的参数 —— 判定：acornfox catalog info 应用 --json 的参数表里没有它
2. 填了由平台生成的参数（密码、密钥） —— 判定：参数表里它是 hidden、generate
3. 值的格式不对，或缺了必填项 —— 判定：message 写的那个参数

排查顺序：

1. 读 message
2. acornfox catalog info 应用

处置：

- ［1 级 可以直接做］按 message 改 --set 后重装

- 必须先停下问用户：必填项要用户提供的内容（例如要用的模型名）时
- 确认恢复：安装任务开始（返回 task_id）

### CAT-INST-03 已经有同名的应用

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：已经有同名的应用
- 下一步：用 --name 换个名字；要重装先由用户确认后 acornfox delete 旧的
- 现象：安装返回 CAT-INST-03（HTTP 409）

可能原因（逐条判定）：

1. 这台服务器上已有这个名字的应用 —— 判定：acornfox apps 里有它

排查顺序：

1. acornfox apps

处置：

- ［1 级 可以直接做］--name 换个名字装
- ［2 级 先问用户］删掉旧的再装

- 必须先停下问用户：要删旧应用时
- 确认恢复：安装任务开始
- 相关编号：CAT-INST-24

### CAT-INST-04 同一个应用正在安装

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：同一个应用正在安装
- 下一步：不要重复点安装；acornfox install status <task_id> 看进度
- 现象：安装返回 CAT-INST-04（HTTP 409），带 task_id

可能原因（逐条判定）：

1. 上一次安装还没结束（页面和命令行各点了一次，或重复提交） —— 判定：acornfox install status <task_id> 显示还在进行

排查顺序：

1. acornfox install status <task_id>

处置：

- ［0 级 只读］等那次安装结束

- 必须先停下问用户：—（不用问）
- 确认恢复：那个任务结束（成功或失败）

### CAT-INST-05 整机分配给应用的内存不够

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：整机分配给应用的内存不够
- 下一步：问用户：调小别的应用（acornfox app set --memory）或调大整机上限（acornfox limits --memory）
- 现象：安装返回 CAT-INST-05（HTTP 409）

可能原因（逐条判定）：

1. 已有的应用占满了整机上限 —— 判定：acornfox limits --json 看已分配和上限
2. 整机上限本来就设得小 —— 判定：acornfox limits 看上限，和 free -h 的实际内存比

排查顺序：

1. acornfox limits --json

处置：

- ［2 级 先问用户］调小别的应用的内存
- ［2 级 先问用户］调大整机上限

- 必须先停下问用户：总是：动别的应用或上限都要用户选
- 确认恢复：acornfox limits 有足够余量，重装成功

### CAT-INST-06 CPU 架构不在应用支持的列表里

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这台机器的 CPU 架构不在应用支持的列表里
- 下一步：换一台机器，或等应用出这个架构的版本
- 现象：安装返回 CAT-INST-06（HTTP 409）；应用中心里这个应用标“不支持这台机器”

可能原因（逐条判定）：

1. 应用只出了 amd64（或只出了 arm64） —— 判定：acornfox catalog info 应用 --json 的 platforms，和 uname -m 比

排查顺序：

1. uname -m
2. acornfox catalog info 应用 --json

处置：

- ［0 级 只读］告诉用户这台机器装不了

- 必须先停下问用户：换机器由用户决定
- 确认恢复：—

### CAT-INST-07 访问端口用完了

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：这台服务器的访问端口用完了
- 下一步：删掉不用的应用后重试
- 现象：安装返回 CAT-INST-07（HTTP 409）

可能原因（逐条判定）：

1. 应用端口段 18810–18899 已经分完 —— 判定：acornfox apps 数一下应用个数

排查顺序：

1. acornfox apps

处置：

- ［2 级 先问用户］删掉不用的应用

- 必须先停下问用户：删应用之前
- 确认恢复：重装成功

### CAT-INST-08 AcornFox 版本比应用要求的旧

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：AcornFox 版本比应用要求的旧
- 下一步：先升级 AcornFox 再装（错误里的 needs_platform 是要升到的版本）
- 现象：安装返回 CAT-INST-08，带 needs_platform；应用中心里这个应用标“要先升级”

可能原因（逐条判定）：

1. 应用或配方的 min_platform 比这台 AcornFox 新 —— 判定：acornfox --json version 和 needs_platform 比较

排查顺序：

1. acornfox --json version

处置：

- ［2 级 先问用户］升级 AcornFox（升级期间命令行和控制台会断一会儿）

- 必须先停下问用户：升级之前
- 确认恢复：升级后重装成功
- 相关编号：CAT-INST-29

### CAT-INST-09 已不再使用（编号保留）

- 分组：应用中心安装（CAT-INST）
- 预留：现在还没有地方报这个编号
- 默认等级：P3（提示或用法问题，不急）
- 是什么：已不再使用的编号：以前是平台内置的大模型来源（--llm）出的错。大模型接口现在是一种普通的服务，这一类问题报 CAT-INST-31～35。编号保留，不会再分给别的错误
- 下一步：这一版不会再出现这个编号。遇到了说明看的是旧版本的输出：按 acornfox faults CAT-INST-31 等新编号处理
- 现象：只在旧版本的输出、旧的任务记录里见到

可能原因（逐条判定）：

1. 看的是旧版本留下的输出或任务记录 —— 判定：acornfox --json version 看现在的版本

排查顺序：

1. acornfox faults CAT-INST-31

处置：

- ［0 级 只读］按新编号处理

- 必须先停下问用户：—（不用问）
- 确认恢复：—
- 相关编号：CAT-INST-31、CAT-INST-34

### CAT-INST-10 已不再使用（编号保留）

- 分组：应用中心安装（CAT-INST）
- 预留：现在还没有地方报这个编号
- 默认等级：P3（提示或用法问题，不急）
- 是什么：已不再使用的编号：以前是平台内置的大模型来源（--llm）出的错。大模型接口现在是一种普通的服务，这一类问题报 CAT-INST-31～35。编号保留，不会再分给别的错误
- 下一步：这一版不会再出现这个编号。遇到了说明看的是旧版本的输出：按 acornfox faults CAT-INST-31 等新编号处理
- 现象：只在旧版本的输出、旧的任务记录里见到

可能原因（逐条判定）：

1. 看的是旧版本留下的输出或任务记录 —— 判定：acornfox --json version 看现在的版本

排查顺序：

1. acornfox faults CAT-INST-31

处置：

- ［0 级 只读］按新编号处理

- 必须先停下问用户：—（不用问）
- 确认恢复：—
- 相关编号：CAT-INST-31、CAT-INST-34

### CAT-INST-11 已不再使用（编号保留）

- 分组：应用中心安装（CAT-INST）
- 预留：现在还没有地方报这个编号
- 默认等级：P3（提示或用法问题，不急）
- 是什么：已不再使用的编号：以前是平台内置的大模型来源（--llm）出的错。大模型接口现在是一种普通的服务，这一类问题报 CAT-INST-31～35。编号保留，不会再分给别的错误
- 下一步：这一版不会再出现这个编号。遇到了说明看的是旧版本的输出：按 acornfox faults CAT-INST-31 等新编号处理
- 现象：只在旧版本的输出、旧的任务记录里见到

可能原因（逐条判定）：

1. 看的是旧版本留下的输出或任务记录 —— 判定：acornfox --json version 看现在的版本

排查顺序：

1. acornfox faults CAT-INST-31

处置：

- ［0 级 只读］按新编号处理

- 必须先停下问用户：—（不用问）
- 确认恢复：—
- 相关编号：CAT-INST-31、CAT-INST-34

### CAT-INST-12 镜像从所有来源都没拿到

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：镜像从所有来源都没拿到
- 下一步：看 attempts；国内网络确认已配镜像加速；稍后重试
- AcornFox 已经自动做的：有镜像包的应用先短时间测速，下载网关和镜像站谁快先用谁，失败再换另一条（镜像站里先实测，跳过不能用的、慢的排后，拉的过程中太慢会换下一个）；国内网络不去 Docker Hub、ghcr.io 本身（它们在国内被屏蔽，去了只会白等），message 里写明没去
- 现象：安装进度停在“下载镜像”，返回 CAT-INST-12，带 attempts（每个镜像源一条：source 是源的地址，code 是它怎么了）

可能原因（逐条判定）：

1. 国内服务器拉公共镜像，能用的镜像源都不通或拒绝访问（例如回 403）。国内网络不会再去试 Docker Hub、ghcr.io 本身，所以这时直接报这个编号，不再多等一轮 —— 判定：attempts 里每个源是 skipped、unreachable 或 not_found，message 末尾写“国内网络没有去试 registry-1.docker.io”；acornfox sources --probe。这台机器真能直连 Docker Hub（例如有代理）：把 registry-1.docker.io 写进环境变量 ACORNFOX_REGISTRY_MIRRORS（ghcr.io 写进 ACORNFOX_GHCR_MIRRORS），放在镜像源后面
2. 镜像源都很慢（例如把大的镜像层转到境外下载），一个镜像在总时限 50 分钟里拉不完（拉镜像的时间不算进整次安装的 60 分钟，所以不会因为套装里镜像多、整次安装先到时限而停下） —— 判定：attempts 里是 slow、stalled 或 timeout，message 开头是“拉取镜像超时：50 分钟还没拉完”或写了平均速度和已下多少
3. 运行器在一个镜像的时限过后还没回话（运行器卡住或重启了） —— 判定：message 里写“运行器也没有回话”，没有 attempts；systemctl status acornfox-runner、journalctl -u acornfox-runner
4. 网络暂时不通 —— 判定：稍后重试能过
5. 下载网关没用上（网关不通或时钟不准），message 里带网关那一路的原因［doctor 自动判定］ —— 判定：doctor 的下载网关（SETUP-GW-01）和时钟（SETUP-CLOCK-01）两项
6. 镜像包经网关分段下载没成（缓存写不了、某一段多次失败），镜像站又都不行［doctor 自动判定］ —— 判定：message 里带 CAT-INST-40；按 CAT-INST-40 的条目处理

排查顺序：

1. 读 attempts
2. acornfox sources --probe
3. sudo acornfox doctor

处置：

- ［1 级 可以直接做］稍后重装
- ［2 级 先问用户］改 Docker 镜像加速配置

- 必须先停下问用户：要改 Docker 配置时
- 确认恢复：重装成功
- 相关编号：CAT-INST-40、SETUP-GW-01、SETUP-CLOCK-01、SETUP-NET-03、pull_failed

### CAT-INST-13 网关给的镜像包和目录里签名的校验值对不上

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：下载网关给的镜像包和目录里签名的校验值或大小对不上，没有导入、已删掉
- 下一步：公共目录的应用自动换公共源，都不对就等目录更新；私有目录的应用直接停下，不去公共源找，等目录更新或联系给码的人。不要手动绕过
- AcornFox 已经自动做的：坏的镜像包已删掉、没有导入；公共目录的应用已自动换公共源
- 现象：安装进度里写镜像包校验不过，返回或记下 CAT-INST-13

可能原因（逐条判定）：

1. 网盘上的文件和目录里写的不是同一份（目录发布还没跟上） —— 判定：过一段时间 acornfox catalog sync 后重装能过
2. 下载中途数据出错 —— 判定：重装一次能过
3. 下载内容被篡改 —— 判定：反复出现、换网络也一样

排查顺序：

1. 重装一次
2. acornfox catalog sync 后再装

处置：

- ［1 级 可以直接做］重装一次
- ［2 级 先问用户］绕过校验——绝不

- 必须先停下问用户：反复出现时：停下，带上证据包反馈
- 确认恢复：重装成功

### CAT-INST-14 往数据卷里写配置文件失败

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：往数据卷里写配置文件失败
- 下一步：看 message；检查磁盘空间和 Docker 状态（acornfox doctor）
- 现象：安装进度停在“写配置文件”，返回 CAT-INST-14

可能原因（逐条判定）：

1. 磁盘满了［doctor 自动判定］ —— 判定：doctor 的磁盘一项（SETUP-DISK-01）
2. Docker 出错 —— 判定：doctor 的 Docker 一项

排查顺序：

1. 读 message
2. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 doctor 的结果处理后重装

- 必须先停下问用户：要清理磁盘时
- 确认恢复：重装成功
- 相关编号：SETUP-DISK-01

### CAT-INST-15 容器没能启动或上线

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：容器没能启动或上线
- 下一步：看 diagnosis；acornfox logs <应用> 看日志
- 现象：安装返回 CAT-INST-15，带部署诊断 diagnosis

可能原因（逐条判定）：

1. 见 diagnosis.code（部署诊断码） —— 判定：acornfox faults <diagnosis.code>

排查顺序：

1. 读 diagnosis.code，按部署诊断码的条目处理
2. acornfox logs 应用 --tail 100

处置：

- ［1 级 可以直接做］按部署诊断码处理

- 必须先停下问用户：要改应用设置或删数据时
- 确认恢复：acornfox status 应用 是 running

### CAT-INST-16 健康检查在时限内没通过

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：健康检查在时限内没通过
- 下一步：acornfox logs <应用> 看日志；内存太小时调大后重装
- 现象：安装进度停在“等健康检查”，返回 CAT-INST-16

可能原因（逐条判定）：

1. 应用第一次启动慢（初始化数据库、下载模型） —— 判定：acornfox logs 应用 里还在初始化
2. 内存太小，应用反复重启 —— 判定：acornfox logs 应用 里有 out of memory；acornfox stats 应用
3. 应用启动报错 —— 判定：acornfox logs 应用 里的报错

排查顺序：

1. acornfox logs 应用 --tail 100
2. acornfox stats 应用

处置：

- ［1 级 可以直接做］重装一次
- ［2 级 先问用户］调大内存后重装

- 必须先停下问用户：调内存涉及别的应用时
- 确认恢复：重装后健康检查通过

### CAT-INST-17 装好后的初始化调用失败

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：装好后的初始化调用失败：发给应用的请求，或在应用自己的容器里执行的命令（没收到应答的请求、超时的命令不会重发，因为它可能已经生效）
- 下一步：看 message（命令失败的写了退出码和标准错误的最后几行）；acornfox logs <应用>；这次的应用已清理，可以直接重装
- AcornFox 已经自动做的：这次装的应用已清理
- 现象：安装进度停在初始化那一步，返回 CAT-INST-17

可能原因（逐条判定）：

1. 应用的接口返回了错误 —— 判定：message 里的 HTTP 状态
2. 请求没送到（应用还没准备好） —— 判定：message 说没收到应答
3. 在应用容器里执行的命令没通过：退出码不对、超时（可能还在容器里运行）、输出太多，或容器没在运行 —— 判定：message 写“命令的退出码是 …”（后面是标准错误的最后几行）、“命令在 … 秒内没有结束”、“命令的输出太多”或“应用的容器没在运行”

排查顺序：

1. 读 message
2. 重装一次

处置：

- ［1 级 可以直接做］直接重装

- 必须先停下问用户：重装后还失败：带上证据包
- 确认恢复：重装成功

### CAT-INST-18 安装途中服务重启了，这次安装已作废并清理

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：安装途中服务重启了，这次安装已作废并清理
- 下一步：重新安装
- AcornFox 已经自动做的：这次安装已作废并清理
- 现象：安装任务状态是失败，CAT-INST-18

可能原因（逐条判定）：

1. 安装中 acornfox-server 重启了（升级、手动重启、机器重启） —— 判定：systemctl status acornfox-server 的启动时间晚于安装开始时间

排查顺序：

1. 确认服务已在运行

处置：

- ［1 级 可以直接做］重新安装

- 必须先停下问用户：—（不用问）
- 确认恢复：重装成功

### CAT-INST-19 目录或应用包读不了、校验不过

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：目录或应用包读不了、校验不过
- 下一步：acornfox catalog sync 后重试；都不行就重装 AcornFox
- 现象：安装或打开应用中心返回 CAT-INST-19

可能原因（逐条判定）：

1. 缓存的目录或下载的应用包坏了 —— 判定：acornfox catalog sync 后能过
2. 程序文件坏了 —— 判定：sync 后仍不行

排查顺序：

1. acornfox catalog sync
2. 重装应用

处置：

- ［1 级 可以直接做］acornfox catalog sync
- ［2 级 先问用户］重装 AcornFox（同版本重装保留数据）

- 必须先停下问用户：要重装 AcornFox 时
- 确认恢复：acornfox catalog list 正常，重装应用成功
- 相关编号：CAT-SYNC-08、CAT-PKG-01

### CAT-INST-20 内部错误：运行器版本旧、数据库或 Docker 出错

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：内部错误：运行器版本旧、数据库或 Docker 出错
- 下一步：acornfox doctor；升级 AcornFox 后重试
- 现象：安装返回 CAT-INST-20（HTTP 500）

可能原因（逐条判定）：

1. Docker 出错［doctor 自动判定］ —— 判定：doctor 的 Docker 一项
2. 执行器版本比服务旧（升级没换全） —— 判定：doctor 的后台服务一项
3. 清单里有在应用容器里执行命令的步骤，运行器（runner）太旧，不能执行 —— 判定：message 写“运行器（runner）太旧”
4. 数据库出错 —— 判定：journalctl -u acornfox-server -n 50 --no-pager

排查顺序：

1. sudo acornfox doctor
2. journalctl -u acornfox-server -n 50 --no-pager

处置：

- ［1 级 可以直接做］按 doctor 处理后重试
- ［2 级 先问用户］升级 AcornFox

- 必须先停下问用户：仍不行时带上证据包
- 确认恢复：重装成功

### CAT-INST-21 没有这个安装任务

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：没有这个安装任务
- 下一步：检查任务编号
- 现象：acornfox install status <编号> 返回 CAT-INST-21（HTTP 404）

可能原因（逐条判定）：

1. 编号抄错，或服务重启后旧任务已清掉 —— 判定：acornfox install status --json 列最近的任务

排查顺序：

1. 看最近的任务列表

处置：

- ［0 级 只读］用对的编号再查

- 必须先停下问用户：—（不用问）
- 确认恢复：能查到任务

### CAT-INST-22 这个应用不是从应用中心装的

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这个应用不是从应用中心装的，没有首次登录信息
- 下一步：—
- 现象：查首次登录信息或应用变量返回 CAT-INST-22

可能原因（逐条判定）：

1. 应用是用 acornfox deploy 部署的，不是从应用中心装的 —— 判定：acornfox apps vars 应用 的 from_catalog 为 false

排查顺序：

1. 确认应用是怎么装的

处置：

- ［0 级 只读］告诉用户这个应用没有首次登录信息

- 必须先停下问用户：—（不用问）
- 确认恢复：—

### CAT-INST-23 清单用到了这个版本还不支持的写法

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：清单用到了这个版本还不支持的写法（多个镜像，或格式已经认、这一版还不执行的写法；现在格式里的写法这一版都已执行）
- 下一步：升级 AcornFox
- 现象：安装返回 CAT-INST-23（HTTP 400）

可能原因（逐条判定）：

1. 应用是多容器或带依赖服务的，这个版本还装不了 —— 判定：acornfox catalog info 应用 --json 的组件不止一个
2. 清单用了格式已经认、这一版还不执行的写法（现在没有这样的写法；以后格式先于平台加新写法时会有） —— 判定：message 里写了是哪一种、在清单的哪里；写清单的人用 acornfox catalog lint 看到的是 CAT-LINT-26 提醒

排查顺序：

1. acornfox --json version

处置：

- ［2 级 先问用户］升级 AcornFox

- 必须先停下问用户：升级之前
- 确认恢复：升级后能装
- 相关编号：CAT-INST-29、CAT-LINT-26

### CAT-INST-24 还留着以前同名应用的数据卷

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：这台机器上还留着以前同名应用的数据卷（删应用时保留了数据），默认不让新装悄悄接上旧数据，所以拒绝
- 下一步：停下问用户：旧数据还要不要。要接着用就加 --reuse-data 重装（页面上是“接上旧数据重装”；删除时保留过参数的，账号密码、你填过的设置项、改过的平台变量、接的服务和配置文件都按以前的，这次写了的以这次为准）；不要旧数据就用 --name 换个名字装，或者由用户执行 message 里卷名对应的 docker volume rm 后再装
- 这一项要用户自己确认或操作
- 现象：安装返回 CAT-INST-24（HTTP 409），message 里列出卷名；页面弹出“接上旧数据重装”的确认

可能原因（逐条判定）：

1. 以前装过同名应用，删除时保留了数据（默认就保留） —— 判定：sudo docker volume ls | grep 应用名
2. 数据卷是别的应用留下的，名字撞上了 —— 判定：message 里写旧数据属于哪个应用；不是同一个应用时 --reuse-data 也会被拒

排查顺序：

1. 读 message 里的卷名
2. 问用户旧数据还要不要

处置：

- ［2 级 先问用户］用户要旧数据：加 --reuse-data 重装
- ［2 级 先问用户］用户不要：--name 换名字装
- ［2 级 先问用户］用户明确说删：docker volume rm 卷名（不可恢复）

- 必须先停下问用户：总是：旧数据留不留只能用户定；删卷不可恢复
- 确认恢复：安装成功；接上旧数据时用以前的账号能登录
- 相关编号：CAT-INST-03
- 演练方法：装一个应用，acornfox delete 应用（保留数据），再 acornfox install 同一个应用

### CAT-INST-25 应用或配方来自已断开的私有目录

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这个应用或配方来自已断开的私有目录（授权被撤销或目录被停用）；已装好的照常运行，但不能再装、不能升级
- 下一步：告诉用户联系给连接码的人；拿到新连接码后 acornfox catalog connect 新码，再装
- 现象：安装返回 CAT-INST-25（HTTP 409）；acornfox catalog sources 里那个目录标“已断开”

可能原因（逐条判定）：

1. 给码的人撤销了这台机器的授权或停用了目录 —— 判定：acornfox catalog sources 的 disconnected 为 true，code 是 CAT-SYNC-11

排查顺序：

1. acornfox catalog sources

处置：

- ［0 级 只读］告诉用户联系给码的人
- ［2 级 先问用户］拿到新码后 acornfox catalog connect

- 必须先停下问用户：连接码只能用户去要
- 确认恢复：catalog sources 里目录恢复连接
- 相关编号：CAT-SYNC-11

### CAT-INST-26 私有目录的文件没从下载网关取到

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：私有目录的文件（应用包或镜像）没从下载网关取到：网关不给（这台机器对这个目录没有授权了、目录停用，或文件不在目录里），或者这台服务器关掉了下载网关、连不上网关、本机时钟不准、下载多次都没下完，或者私有目录的清单没写镜像包、没有这台机器 CPU 架构的镜像包。私有应用的文件只走下载网关，不去公共源找
- 下一步：看 gateway_code：GW-CAT-* 先 acornfox catalog sync 看目录是否已断开，还连着就请给码的人检查目录；GW-AUTH-01 先校准本机时间（acornfox doctor 看 SETUP-CLOCK-01）；GW-NET-01 或下载中断就检查网络后重装；网关被关掉时问用户是否打开；message 说清单没写镜像包、或没有这台机器架构（例如 linux/arm64）的镜像包时，告诉用户联系给码的人补上
- 现象：安装私有目录的应用返回 CAT-INST-26（HTTP 403），带 gateway_code

可能原因（逐条判定）：

1. 网关拒绝（授权撤销、目录停用、文件不在目录里） —— 判定：gateway_code 是 GW-CAT-*
2. 本机时钟不准［doctor 自动判定］ —— 判定：gateway_code 是 GW-AUTH-01；doctor 的时钟一项
3. 连不上网关或下载多次中断［doctor 自动判定］ —— 判定：gateway_code 是 GW-NET-01；doctor 的下载网关一项
4. 这台服务器关掉了下载网关［doctor 自动判定］ —— 判定：ACORNFOX_GATEWAY 为空或 network.json 写了 off；doctor 写“已关掉”
5. 私有目录的清单没写镜像包 —— 判定：message 这样写

排查顺序：

1. 读 gateway_code 和 message
2. sudo acornfox doctor 看时钟和下载网关
3. acornfox catalog sync

处置：

- ［1 级 可以直接做］网络修好后重装
- ［2 级 先问用户］校准时钟
- ［2 级 先问用户］打开下载网关

- 必须先停下问用户：改系统时间、打开网关、联系给码的人时
- 确认恢复：重装成功
- 相关编号：GW-CAT-05、GW-AUTH-01、GW-NET-01、SETUP-GW-01、SETUP-CLOCK-01

### CAT-INST-27 Docker 存镜像的分区剩余空间不够

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：Docker 存镜像的分区剩余空间不够这次要装的应用（清单的 disk_mb.min 之和，含下载的镜像包和解开后的镜像）
- 下一步：问用户：删掉不用的应用或镜像（acornfox delete、docker image prune 由用户确认后执行），或扩容磁盘；acornfox doctor 看各分区剩余空间
- 现象：安装在一开始就返回 CAT-INST-27（HTTP 409），message 写了要多少、剩多少，什么都没下载

可能原因（逐条判定）：

1. 系统盘本来就小［doctor 自动判定］ —— 判定：df -h 看 Docker 数据目录所在分区的总大小
2. 旧镜像、停掉的容器、构建缓存占了空间 —— 判定：sudo docker system df 的 RECLAIMABLE
3. 数据目录（下载镜像包的地方）所在分区满了［doctor 自动判定］ —— 判定：doctor 的磁盘一项写的是“数据目录”那个分区
4. 同一分区里有不是 Docker 镜像的东西占空间（容器日志、Docker 目录里别的文件、用户自己的文件或备份；docker system df 不算这些） —— 判定：sudo du -xh --max-depth=1 <分区的挂载点> | sort -h | tail 看哪个目录大；Docker 数据目录（doctor 磁盘一项写着，默认 /var/lib/docker）很大、docker system df 却很小时，再 sudo du -xah --max-depth=2 /var/lib/docker | sort -h | tail 找到具体的文件（-a 连文件一起列）

排查顺序：

1. 读 message：要多少、剩多少
2. sudo acornfox doctor 看是哪个分区
3. sudo docker system df
4. df -h 看那个分区的挂载点和剩余
5. sudo du -xh --max-depth=1 <挂载点> | sort -h | tail 找占空间的目录；Docker 目录大而 docker system df 小时，sudo du -xah --max-depth=2 /var/lib/docker | sort -h | tail 找到具体的文件

处置：

- ［1 级 可以直接做］sudo apt-get clean 清掉软件包缓存
- ［2 级 先问用户］docker image prune 或删掉不用的应用
- ［2 级 先问用户］删掉找出来的大文件或日志（先告诉用户是什么、在哪、多大）
- ［2 级 先问用户］扩容磁盘（花钱）

- 必须先停下问用户：删镜像、删应用、删找出来的文件、扩容之前
- 确认恢复：df -h 剩余超过 message 里要的大小，再装一次成功。doctor 的磁盘一项在剩余不到 10 GB 时仍是警告，这不影响这次安装，不用等它变成 ok
- 相关编号：SETUP-DISK-01
- 演练方法：在测试机上 fallocate -l <大小> /var/lib/docker/fill 占满磁盘后安装一个应用

### CAT-INST-28 改平台变量不对

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：改平台变量不对：没有这个变量、这个变量不能改，或值的格式不对
- 下一步：acornfox catalog platform-vars 看有哪些、哪些能改；按 message 改 --set platform.名字=值
- 现象：安装返回 CAT-INST-28（HTTP 400）

可能原因（逐条判定）：

1. 变量名写错 —— 判定：acornfox catalog platform-vars 里没有它
2. 这个变量不能改 —— 判定：platform-vars 里它的 user_can_set 为 false
3. 值的格式不对（例如时区名不存在） —— 判定：message 写了

排查顺序：

1. acornfox catalog platform-vars

处置：

- ［1 级 可以直接做］改对 --set platform.名字=值 后重装

- 必须先停下问用户：—（不用问）
- 确认恢复：安装任务开始

### CAT-INST-29 应用包里的清单有这个版本不认识的写法

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：应用包里的清单有这个版本的 AcornFox 不认识的写法（字段、取值或格式版本），安装前就拒绝了，什么都没建、没下载镜像
- 下一步：错误里带 needs_platform 的：先升级 AcornFox 到那个版本再装。不带的：是这份目录有问题，不是操作不对，过一会儿 acornfox catalog sync 后再试，还不行就把 message 反馈给我们；不要想办法绕过
- 现象：安装返回 CAT-INST-29（HTTP 409），可能带 needs_platform

可能原因（逐条判定）：

1. 应用用了新版本才有的写法（带 needs_platform） —— 判定：错误里有 needs_platform
2. 目录本身有问题（不带 needs_platform） —— 判定：错误里没有 needs_platform

排查顺序：

1. 看错误里有没有 needs_platform

处置：

- ［2 级 先问用户］升级 AcornFox
- ［1 级 可以直接做］过一会儿 acornfox catalog sync 后重试

- 必须先停下问用户：升级之前；sync 后还不行时反馈
- 确认恢复：重装成功
- 相关编号：CAT-INST-08

### CAT-INST-30 安装的某一步比平常慢很多（还没失败）

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：安装任务还在进行，但正在做的那一步用的时间超过了它的卡住门槛（各步骤的正常时长和门槛写在程序里，install status 输出的 timing.step 有 normal_seconds、stuck_after_seconds）。这是提醒，不是失败：任务没有停，到了 AcornFox 自己的时限会判失败并清理（从公共源拉一个镜像所有源加起来 50 分钟、启动 15 分钟、健康检查按清单的 timeout_s、整次 60 分钟，从公共源拉镜像的时间不算在这 60 分钟里）
- 下一步：先运行 next_command（就是 timing.step.look：下载镜像等步骤是 acornfox doctor --json，启动、健康检查、初始化是 acornfox logs 应用 --tail 200 --json）看原因，只读、不用问用户；把 say 和看到的要点告诉用户，问用户接着等还是不装了；不要自己删除、重装或重启服务
- AcornFox 已经自动做的：安装没有停，到了时限会自己判失败并清理这次建出来的东西
- 现象：acornfox --json install status 任务 --wait 返回 stuck: true，timing.code 是 CAT-INST-30，status 仍是 running；控制台安装进度停在同一步很久

可能原因（逐条判定）：

1. 下载镜像：从公共源拉大镜像，网速慢或镜像站限速 —— 判定：timing.step.name 是 image，这一步的 progress.phase 是 pull（或没有 progress）：message 里写着从哪个源拉、拉到多少、最近多快；progress.pull_seconds 是这个镜像拉了多久（过了 52 分钟才算卡住）；acornfox doctor --json 看镜像源一项
2. 下载镜像：经下载网关下载，一直收不到数据 —— 判定：steps 里下载镜像一步的 progress.stalled_seconds 超过 120；doctor 的下载网关一项
3. 下载镜像：导入镜像慢（磁盘慢或快满） —— 判定：progress.phase 是 load；df -h；doctor 的磁盘一项
4. 启动：容器起不来、反复重启，或镜像大、启动慢 —— 判定：timing.step.name 是 start；acornfox logs 应用 --tail 200 --json 里有报错或反复从头启动
5. 健康检查：应用第一次启动在建库、下载模型等，比平常慢 —— 判定：timing.step.name 是 health；日志还在输出初始化的内容
6. 初始化：应用不回应初始化请求 —— 判定：timing.step.name 是 init；日志
7. 服务器内存或 CPU 被占满，每一步都慢 —— 判定：acornfox stats、free -m、uptime 看负载

排查顺序：

1. acornfox --json install status 任务 --wait 看 timing.step：哪一步、用了多久（elapsed_seconds）、正常多久（normal_seconds、typical）
2. 运行 next_command（timing.step.look）
3. acornfox doctor --json
4. df -h、free -m、uptime

处置：

- ［0 级 只读］接着等：再运行 acornfox --json install status 任务 --wait
- ［0 级 只读］把 say 和看到的原因告诉用户
- ［1 级 可以直接做］任务失败（会自动清理）后按它的编号处理，再重装
- ［2 级 先问用户］停掉占资源的别的程序、删掉应用重来、重启 AcornFox 服务

- 必须先停下问用户：用户要不要接着等；停别的程序、删应用、重启服务、换网络或镜像源之前
- 确认恢复：acornfox --json install status 任务 --wait 返回 done: true：succeeded 就好了，failed 按它的编号处理
- 相关编号：CAT-INST-12、CAT-INST-15、CAT-INST-16、CAT-INST-17、SETUP-DISK-01、SETUP-GW-01
- 演练方法：测试机上装一个镜像本机没有的应用，下载镜像时用 iptables 丢掉到镜像源和下载网关的包，过了卡住门槛后查 install status

### CAT-INST-31 必须用到的服务这台机器上没有

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这个应用必须用到的一种服务（例如大模型接口），这台机器上没有能用的，安装前就停下了，什么都没装。平台不会替用户去装别的应用来提供它
- 下一步：你手上已经有这种服务的话（地址、Key 等），照 message 里的命令登记一条（acornfox services add 类型 --name 名字 --set 项=值，保密的项写成 项=-，从标准输入给值；命令里的 … 要换成用户给的真实值，先问到再给命令）；登记好以后先运行 acornfox services test 名字 试一下通不通，通了再看一次计划（acornfox install 应用 --plan），能装了再装；这台机器上有、但现在不能用的（提供它的应用停着、还差必填项），--plan 的 how 里写了怎么让它能用
- 现象：安装返回 CAT-INST-31（HTTP 409），什么都没装；acornfox install 应用 --plan 里这个需要是 required_missing（必须的，没有），how 里是登记一条的命令

可能原因（逐条判定）：

1. 这台机器上没有这种服务（用户还没登记过，也没有装着提供它的应用） —— 判定：acornfox services list 里没有这个类型的服务
2. 这台机器上有这种服务，但现在不能用（提供它的应用停着，或还差必填项） —— 判定：acornfox services list 看状态；--plan 的 how 里写了怎么让它能用

排查顺序：

1. acornfox install 应用 --plan（只读）看每个需要的状态和 how
2. acornfox services list 看这台机器上有哪些服务、状态

处置：

- ［2 级 先问用户］先问用户手上有没有这种服务；有就照 message 里的命令登记一条（地址、Key 要用户提供；命令里的 … 要换成用户给的真实值，先问到再给命令），登记好以后先运行 acornfox services test 名字，通了再看一次计划、再装
- ［2 级 先问用户］提供方停着的，用户同意后 acornfox start 它，再装

- 必须先停下问用户：总是：登记服务要用户的地址和 Key，启动停着的应用要用户同意
- 确认恢复：acornfox install 应用 --plan 的 can_install 是 true，再装时安装任务开始
- 相关编号：CAT-INST-32、CAT-INST-33
- 演练方法：测试机上不登记任何服务，直接装一个必须用大模型接口的应用

### CAT-INST-32 同一类服务有好几条，要选一条

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这台机器上有好几条同一类型的服务都能用，平台不替用户选，要指定用哪一条
- 下一步：从 message 里列的几条服务里选一条用：安装时加 --use 需要名=服务名 选一条，页面上在“需要的服务”里选
- 现象：安装返回 CAT-INST-32（HTTP 400）；--plan 里这个需要是 choose，candidates 里是能选的服务

可能原因（逐条判定）：

1. 这台机器上同一类型的能用的服务不止一条，这个应用以前也没接过其中哪一条 —— 判定：acornfox services list 里同一类型有几条状态是可用

排查顺序：

1. acornfox install 应用 --plan 看 candidates
2. acornfox services list

处置：

- ［2 级 先问用户］把几条服务列给用户，按用户选的那条在安装时加 --use 需要名=服务名

- 必须先停下问用户：总是：用哪一条服务由用户定
- 确认恢复：安装任务开始
- 相关编号：CAT-INST-33
- 演练方法：测试机上登记两条同类型的服务，再装一个需要这种服务的应用、不加 --use

### CAT-INST-33 指定的服务不对

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：安装时指定的服务（--use）不对：这台机器上没有这个名字的服务、类型不对、现在不能用（例如提供它的应用停着），或者要装的应用里没有这个需要
- 下一步：acornfox services list 看有哪些服务和它们的状态；acornfox install 应用 --plan 看应用有哪些需要；改对 --use 再装。提供这条服务的应用停着的，先启动它（acornfox start 应用名）
- 现象：安装返回 CAT-INST-33（HTTP 400），message 写了是哪个 --use

可能原因（逐条判定）：

1. 服务名写错，或这条服务已经删了 —— 判定：acornfox services list 里没有它
2. 这条服务的类型和应用要的不一样 —— 判定：services list 的类型一栏和 --plan 里这个需要的 kind 比
3. 这条服务现在不能用（提供方停着、还差必填项） —— 判定：services list 的状态不是可用
4. 需要名写错（不是应用清单里的名字），或配方里没写 应用名. —— 判定：--plan 里每个需要的 use 一栏

排查顺序：

1. acornfox services list
2. acornfox install 应用 --plan

处置：

- ［1 级 可以直接做］改对 --use 后重装
- ［2 级 先问用户］用户同意后启动停着的提供方

- 必须先停下问用户：要启动别的应用，或改用别的服务时
- 确认恢复：安装任务开始
- 相关编号：CAT-INST-32
- 演练方法：安装时 --use 写一个不存在的服务名

### CAT-INST-34 提供服务的应用没能发凭据

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：提供服务的应用没能给这个应用发凭据（例如在大模型网关里建令牌、在数据库里建库和账号）：提供方没在运行，或者它清单里写的步骤没走通：发请求的应答不对（message 说“这个应用限制了请求次数”的，是提供方回了 HTTP 429，平台没有自动重发），或者在它容器里执行的命令退出码不对、超时（命令可能还在容器里运行，没有重新执行）、输出太多。这次安装已失败并清理，发了一半的凭据已尽量收回，提供方清单写了删数据步骤（purge）的，这次建了一半的库或数据也已照着删掉；收不回、删不掉的，任务的 apps[].carried 里有一条 CAT-INST-35 提醒
- 下一步：看 message 是哪个提供方、哪一步（命令失败的写了退出码和标准错误的最后几行）；acornfox status 提供方 看它是否在运行，acornfox logs 提供方 --tail 200 看日志；说限制了请求次数的，过一会儿再装；提供方的管理员密码在应用里改过的话，平台登不进去：请在提供方应用里手工建一个凭据，登记成服务（acornfox services add …）后用 --use 指定
- AcornFox 已经自动做的：安装已失败，这次建出来的东西已清理，发了一半的凭据已按提供方清单写的步骤尽量收回，清单写了删数据步骤的也照着删了这次建了一半的库或数据（收不回、删不掉的在任务的 apps[].carried 里有 CAT-INST-35 提醒）
- 现象：安装任务失败，编号 CAT-INST-34，失败在“建应用”那一步；message 写“要 某某 发一个凭据，某某 没给”，括号里是哪一步、应用怎么说的

可能原因（逐条判定）：

1. 提供方停着、没在运行或容器没起来 —— 判定：acornfox status 提供方
2. 提供方的管理员密码在它自己的页面里被改过，平台用安装时的密码登不进去 —— 判定：message 里是登录那一步，应用说密码不对
3. 提供方限制了请求次数（HTTP 429） —— 判定：message 写“这个应用限制了请求次数”
4. 提供方的接口和清单写的不一样（版本变了） —— 判定：message 里的 HTTP 状态码和应用说的话；acornfox logs 提供方
5. 在提供方容器里执行的命令没通过：退出码不对、超时（可能还在容器里运行，没有重新执行）、输出太多，或容器没在运行 —— 判定：message 写“命令的退出码是 …”（后面是标准错误的最后几行）、“命令在 … 秒内没有结束”、“命令的输出太多”或“应用的容器没在运行”；acornfox logs 提供方
6. 运行器（runner）太旧，不能在容器里执行命令（这时报的是 CAT-INST-20） —— 判定：message 写“运行器（runner）太旧”

排查顺序：

1. acornfox status 提供方
2. acornfox logs 提供方 --tail 200
3. 再看一遍 message

处置：

- ［2 级 先问用户］提供方停着的，用户同意后 acornfox start 它，再装
- ［2 级 先问用户］请用户在提供方里手工建凭据并登记成服务，用 --use 指定后重装
- ［1 级 可以直接做］限流的等一会儿再装，不要反复重试

- 必须先停下问用户：启动提供方，或要用户手工建凭据时
- 确认恢复：重装成功；GET /v1/apps/应用 的 services 里这一项的 status 是 ok（页面上“需要的服务”显示可用）
- 相关编号：CAT-INST-35
- 演练方法：测试机上把提供方停掉（或改掉它的管理员密码）后装一个用它的服务的应用

### CAT-INST-35 收回凭据没成功（提醒）

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：发给某个应用的凭据没有收回：收回的步骤没走通，或者接旧数据重装时旧的那份没通过检查、换了一份新的，旧的留在了提供方里，或者应用是用旧格式的清单装的、删除时平台不再替它收回；也可能是应用连数据一起删时，提供方清单里删数据的步骤没走通，给它建的库或数据还留在提供方里；或者安装没成功（或两个安装同时抢一条服务、这个输了）回滚时，平台收回了这次刚发的凭据，再照提供方清单的删数据步骤删这次给它新建的库或数据，没删成；或者这次安装的记录里没写明发的是哪一份凭据（旧版本留下的记录），平台不能确认那份库是这次新建的，所以没有删；或者提供方清单里删数据的步骤不是只按这次发凭据的编号找要删的东西（那可能是装之前就有的数据），回滚时平台也只收回凭据、没有删。这只是提醒：删除、回滚或安装照常完成了，但那个凭据（或那份数据）在提供方应用里可能还在、还能用
- 下一步：到提醒里写的那个应用里，按写的名字或编号看一下那一项，用不上就手工删掉；那个应用停着的，启动后再删
- AcornFox 已经自动做的：删除、安装失败的回滚或安装照常做完了，只是那一个凭据没收回
- 现象：删除应用的输出 warnings 里有一行以 CAT-INST-35 开头；安装任务 apps[].carried 里有一行以 CAT-INST-35 开头（安装失败回滚时、发凭据发到一半失败或安装中断时、接旧数据重装时旧凭据没通过检查换了一份、或断开了这次没接上的服务时）；install status 的 must_tell 里也有它

可能原因（逐条判定）：

1. 提供方停着或已经删了 —— 判定：acornfox status 提供方
2. 提供方的管理员密码被改过，或它的接口和清单写的不一样 —— 判定：提醒括号里应用怎么说的
3. 接旧数据重装时，旧凭据没通过提供方清单写的检查（例如被用户停用了），平台换了一份新的，旧的没删 —— 判定：提醒写“没通过检查，已经换了一份新的”
4. 应用是用旧格式的清单装的，删除时平台不再替它收回 —— 判定：提醒写“是用旧格式的清单装的”
5. 应用连数据一起删时，提供方（数据库这类）清单里删数据的步骤（purge）没走通：提供方停着、命令出错或超时 —— 判定：提醒写“连数据一起删了，但没能删掉 … 里给它建的数据”；acornfox status 提供方
6. 安装没成功回滚了（或两个安装同时抢一条服务、这个输了）：这次刚发的凭据已收回，平台照提供方清单的 purge 删这次给它新建的库或数据，没删成（提供方停着、命令出错或超时） —— 判定：提醒写“没装成，平台想删掉 … 这次给它新建的库或数据…没删成”；acornfox status 提供方
7. 安装没成功回滚了，但这次安装的记录是旧版本写的、没写明发的是哪一份凭据：平台不能确认那份库是这次新建的，只收回了凭据、没有删 —— 判定：提醒写“没装成，… 里给它建的库或数据…没有删（这次安装的记录里没有写明是哪一份”
8. 安装没成功回滚了，但提供方清单里删数据的步骤不是只按这次发凭据的编号找要删的东西（例如按应用名找）：那可能是装之前就有的数据，平台只收回了凭据、没有删 —— 判定：提醒写“没装成，… 里给它建的库或数据…没有删（提供方清单里删数据的步骤不是只按这次发凭据的编号找要删的东西”

排查顺序：

1. 读提醒：哪个提供方、给哪个应用的、编号是多少
2. acornfox status 提供方

处置：

- ［0 级 只读］把提醒告诉用户，请用户在提供方里手工删掉那一项

- 必须先停下问用户：手工删凭据只能用户在提供方应用里做
- 确认恢复：用户在提供方里找不到提醒里写的那一项了
- 相关编号：CAT-INST-34
- 演练方法：装好一对使用方和提供方后，先停掉提供方，再连数据一起删使用方（提供方写了 purge 的，收回和删数据各出一条提醒）

### CAT-INST-36 清单是旧格式的，这一版装不了

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：目录里这个应用还是按旧格式的清单（第 2 版及更早）上架的。这一版平台只装新格式（第 3 版）的清单，旧的不装也不转换，安装前就拒绝了，什么都没建（目录的索引里已经标明是旧格式的，连应用包也不下载；没标明的，应用包会先下载到本机缓存，读了清单才知道）。这不是操作不对。用旧清单装好的应用照常运行，能启停、删除。应用中心的列表里这样的应用标着“这是旧格式的清单，这一版装不了，等这个应用按新格式重新上架”（接口里 former 是 true），点进详情也报这个编号
- 下一步：这个应用要等按新格式重新上架后才能装，现在装不了，不是你的操作不对。已经装着的旧应用照常用；要用需要的服务、自动接上这些新功能，得等新清单上架后删了重装
- 现象：安装、--plan 或看目录里的应用详情返回 CAT-INST-36（HTTP 409），message 写“是旧格式的清单”；acornfox catalog list、应用中心列表里那个应用标着“这是旧格式的清单”；acornfox apps vars 旧应用 的 notes 里写“这个应用是用旧格式的清单装的”

可能原因（逐条判定）：

1. 这台机器拿到的目录里，这个应用还是按旧格式上架的 —— 判定：acornfox --json catalog list 里这个应用 former 是 true；目录索引里它的 min_platform 低于 0.6.0，或应用包里清单的 schema 是 2 或更早
2. 目录太旧，还没同步到重新上架的版本 —— 判定：acornfox catalog sync 后再看

排查顺序：

1. acornfox catalog sync
2. acornfox install 应用 --plan

处置：

- ［1 级 可以直接做］acornfox catalog sync 后再试一次
- ［0 级 只读］把 next 原样告诉用户（要等应用按新格式重新上架）；不要想办法绕过：改目录来源、换旧版本、手工建容器都不行

- 必须先停下问用户：—（不用问）
- 确认恢复：acornfox install 应用 --plan 的 problems 里没有 CAT-INST-36
- 相关编号：CAT-INST-29
- 演练方法：测试机上用一份 schema: 2、min_platform 0.5.0 的清单打一个目录（用旧版本的 acornfox catalog build），装它

### CAT-INST-37 原来的凭据核对没过，按清单停下了

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：接旧数据重装时，这个应用以前从一条服务拿到的凭据（例如数据库的账号和密码）没通过提供方清单里写的核对。提供方的清单写了“核对不过就停下”（存数据的服务这样写：换一份新的凭据就等于接到一个空的新库，原来的数据就接不上了），所以平台没有换新的，这次安装停下并清理了，原来的接入和凭据原样留着
- 下一步：先查提供方：acornfox status 提供方 看它是否在运行，acornfox logs 提供方 --tail 200 看日志；提供方好了再装一次（还是 --reuse-data）。确认原来的数据还在不在（例如用户在提供方里删过库、改过密码就会这样）。只有用户确定不要原来的数据时，才改接别的服务：加 --use 需要名=服务名 再装；不要自己删库、改密码
- AcornFox 已经自动做的：安装已停下并清理，没有发新的凭据，原来的接入和凭据原样留着（下次重装还会先核对它）
- 这一项要用户自己确认或操作
- 现象：安装任务失败，编号 CAT-INST-37，失败在“建应用”那一步；message 写“以前从服务 … 拿到的凭据这次核对没通过”，括号里是核对哪一步、退出码或应答；接旧数据重装前看计划（install … --reuse-data --plan）时 problems 里就有 CAT-INST-37，message 写“照现在的样子装会停下”：计划会先只读地核对一次原来的凭据（只是没在几秒内核对完、提供方没应答的不报这个编号，只在 needs[].tell 里说装的时候会再核对）

可能原因（逐条判定）：

1. 提供方停着、没在运行或刚重启还没好 —— 判定：acornfox status 提供方；message 括号里写“容器没在运行”“连不上”
2. 用户在提供方里删了给这个应用建的库或账号，或者改了它的密码 —— 判定：message 括号里是核对命令的退出码和标准错误（例如密码不对、库不存在）；问用户有没有在提供方里动过
3. 提供方是用别的数据重装过的（原来的数据没了） —— 判定：acornfox status 提供方 看它是不是刚装的；问用户提供方装的时候接没接旧数据
4. 提供方换了清单版本，现在要发的项旧凭据里没有 —— 判定：message 括号里写“缺了提供方现在要发的项”

排查顺序：

1. 读 message：哪条服务、哪个提供方、核对哪一步没过
2. acornfox status 提供方
3. acornfox logs 提供方 --tail 200
4. acornfox services show 服务名
5. 处理后先 acornfox install 应用 --name 应用名 --reuse-data --plan 再核对一次（只读），can_install 是 true 再装

处置：

- ［2 级 先问用户］提供方停着：问用户后 acornfox start 提供方，再用同样的命令重装
- ［1 级 可以直接做］提供方好了以后重装一次（--reuse-data）
- ［2 级 先问用户］用户确定不要原来的数据：问过用户后加 --use 需要名=服务名 改接别的服务再装

- 必须先停下问用户：原来的数据还要不要只能用户决定；改接别的服务、启动提供方之前先问用户；不要自己在提供方里删库、改密码
- 确认恢复：--reuse-data --plan 的 problems 里没有 CAT-INST-37；重装成功，任务 apps[].carried 里写“接着用以前 … 发给它的凭据（检查过还能用）”
- 相关编号：CAT-INST-34、CAT-INST-35
- 演练方法：装好一个写了 on_check_fail: stop 的数据库提供方和一个使用方，保留数据删掉使用方，在提供方里改掉给它建的账号的密码，再 --reuse-data 装回使用方

### CAT-INST-38 要接的服务被占着或已经接满

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：要接的那条服务现在接不了这个应用：别的应用把它独占了（清单写了只接没有别的使用方在用的服务），这个应用要独占它但已经有别的应用在用，或者提供它的应用限制了同时接几个、已经接满。删了但数据还留着的应用也占着它的位置（它装回来时要接回去）。安装前就停下了，或者在接的那一刻发现被同时在装的别的应用先接上了，这次安装已清理（这次刚发的凭据已收回，提供方清单写了删数据步骤的，给它新建的库或数据也已删掉；没删成的在任务的 apps[].carried 里有 CAT-INST-35 提醒）
- 下一步：acornfox services list 看每条服务被谁独占、还能接几个；acornfox install 应用 --plan 看现在能接哪条。有别的同类服务就用 --use 需要名=服务名 指定；没有就登记一条你已有的（acornfox services add …）。占着位置的是删了但数据还留着的应用、用户也确认它不要了：它已经删了，不能直接 delete --volumes，也不要手工 docker volume rm（接入记录还在，位置照样占着）；问过用户后先 acornfox install --name 原来的名字 --reuse-data 装回来，再 acornfox delete 原来的名字 --volumes 连数据一起删，位置才让出来。不要为了腾位置去删别的应用，要删先问用户
- 现象：安装或 --plan 返回 CAT-INST-38（HTTP 409），message 写“已经被应用 … 独占”“已经有应用 … 在用”或“已经接满”；安装任务失败，编号 CAT-INST-38，message 写“可能是同时在装的别的应用先接上了”；acornfox services list 里那条服务写“被应用 … 独占”或“已经接满”

可能原因（逐条判定）：

1. 别的应用的清单写了要独占这条服务（dedicated），已经接上了 —— 判定：acornfox services show 服务名 的“能不能再接”写“被应用 … 独占”；--json 里 dedicated_to 不是空的
2. 这个应用的清单写了要独占（dedicated），这条服务已经有别的应用在用 —— 判定：acornfox install 应用 --plan 里这个需要 dedicated 是 true；services show 的“在用它的”不是空的
3. 提供它的应用限制了同时接几个（max_consumers），已经接满 —— 判定：services show 写“已经接满（最多 N 个）”；--json 里 slots_left 是 0
4. 占着位置的是删了但数据还留着的应用 —— 判定：message 或 services show 里那个应用后面写“已删、数据还留着”；used_by 里它的 gone 是 true
5. 两个应用同时在装，另一个先接上了 —— 判定：message 写“可能是同时在装的别的应用先接上了”；acornfox services show 服务名 看现在是谁接着

排查顺序：

1. acornfox services list
2. acornfox services show 服务名
3. acornfox install 应用 --plan

处置：

- ［1 级 可以直接做］有别的能用的同类服务：加 --use 需要名=服务名 再装
- ［2 级 先问用户］问用户有没有自己已有的这种服务，有就登记一条（acornfox services add …）再装
- ［2 级 先问用户］占着位置的应用确实不要了：问用户后 acornfox delete 应用名 --volumes 连数据一起删
- ［2 级 先问用户］占着位置的是删了但数据还留着的应用、用户确认不要了：问用户后先 acornfox install --name 原来的名字 --reuse-data 装回来，再 acornfox delete 原来的名字 --volumes（不要手工 docker volume rm：接入记录还在，位置照样占着）

- 必须先停下问用户：要删别的应用、登记服务之前都先问用户；平台不会替用户再装一个提供方
- 确认恢复：acornfox install 应用 --plan 的 can_install 是 true，再装成功
- 相关编号：CAT-INST-31、CAT-INST-33
- 演练方法：测试机上装一个数据库提供方和一个写了 dedicated: true 的使用方，再装另一个要这种服务的应用并 --use 指定这条服务

### CAT-INST-39 清单里的一个值算不出来

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：清单里写的一个值算不出来：环境变量、启动命令、配置文件、首次登录信息或初始化步骤里的 {{…}} 写法读不了（检查工具会报 CAT-LINT-46、47），一个变换没做成（例如 map 对照表里没有取到的那个值，又没写 "*=值" 兜底），if 条件读不了，同名的两条环境变量的条件同时成立，或者启动命令引用了保密的值。这是清单的问题，不是操作不对；这次安装已失败并清理
- 下一步：看 message 是哪一处的哪个 {{…}}（message 里不带取到的值）。是 map 没对上、条件同时成立这类、要靠换一个值解决的（例如换一种对外方式、--set 改设置项；acornfox catalog platform-vars 看本机平台变量的值），问用户后再装；写法读不了、启动命令带保密值的，是目录的问题，把 message 反馈给我们，不要想办法绕过
- 现象：安装前就返回 CAT-INST-39，message 写“… 写得读不了”；安装任务失败，编号 CAT-INST-39，message 写是哪个环境变量、配置文件或启动命令的第几项，和那个 {{…}}

可能原因（逐条判定）：

1. map 的对照表里没有取到的那个值，又没写 "*=值" —— 判定：message 里的 {{…}} 有 map，写了“没有对上的项”
2. 清单写法读不了（目录没过检查就发了） —— 判定：message 写“写得读不了”；acornfox catalog lint 目录 报 CAT-LINT-46、47、48
3. 同名的两条环境变量的 if 同时成立 —— 判定：message 写“有两条的 if 同时成立”
4. 启动命令引用了保密的值 —— 判定：message 写“启动命令的第 N 项引用了保密的值”

排查顺序：

1. acornfox install status 任务编号 --json 看 message
2. acornfox catalog platform-vars 看本机平台变量的值
3. 有目录仓库时 acornfox catalog lint 目录

处置：

- ［2 级 先问用户］换一个能对上的值（换对外方式、--set 改设置项）后再装
- ［0 级 只读］清单本身的问题：把 message 反馈给目录维护者，等目录更新

- 必须先停下问用户：换对外方式、改设置项之前先问用户；不要改清单绕过
- 确认恢复：再装成功，acornfox apps vars 应用 --json 的 env 里这一项是预期的值
- 相关编号：CAT-LINT-46、CAT-LINT-47、CAT-LINT-49
- 演练方法：测试机上用一份测试清单，环境变量写 {{platform.region | map "x=1"}}（没有 * 兜底），从本地目录装它

### CAT-INST-40 镜像包经下载网关分段下载没成

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：镜像包经下载网关分段下载没成：下载缓存写不了（磁盘满了或没权限）、某一段连着多次下不下来，或者下载地址反复失效。已经下好的部分留在下载缓存里，下次接着下；公共目录的应用接着换镜像站拉，私有目录的应用停下
- 下一步：看 message 是哪一种：写缓存失败先看磁盘空间和权限（acornfox doctor 的 SETUP-DISK-01）；某一段多次下不下来，多半是网络或网盘暂时不通，稍后重装会从缓存接着下，已下的不用重下；缓存在数据目录 uploads/dl-cache，3 天没动会自己清，磁盘紧张时可以由用户确认后删；要退回单连接下载，环境变量 ACORNFOX_DL_SEGMENTED=0
- AcornFox 已经自动做的：已下好的段留在缓存里，重装接着下；地址过期自动向网关要新的（一次下载最多 20 次）；某一段失败自己重试 6 次；公共目录的应用接着换镜像站拉
- 现象：安装进度的“下载镜像”一步写“下载网关没成功”，message 里带 CAT-INST-40；私有目录的应用安装失败，返回 CAT-INST-26，message 里带这一条的原因

可能原因（逐条判定）：

1. 磁盘满了，或数据目录的 uploads/dl-cache 没有写权限［doctor 自动判定］ —— 判定：message 写“写下载缓存失败”；doctor 的磁盘一项（SETUP-DISK-01）；ls -ld 数据目录/uploads/dl-cache
2. 网络或网盘暂时不通，某一段连着失败 —— 判定：message 写“第 N 段（共 M 段）连着失败”；稍后重装能接着下
3. 下载地址反复失效（本机时钟和网关差得多，或网关反复拒绝）［doctor 自动判定］ —— 判定：message 写“换了 N 次下载地址”；doctor 的时钟一项（SETUP-CLOCK-01）和下载网关一项（SETUP-GW-01）

排查顺序：

1. acornfox install status 任务 --json 看 message
2. sudo acornfox doctor
3. ls -la 数据目录/uploads/dl-cache 看缓存里已经有多少

处置：

- ［1 级 可以直接做］稍后重装（接着下）
- ［2 级 先问用户］磁盘紧张时，用户确认后删 uploads/dl-cache 里的文件

- 必须先停下问用户：删缓存之前先问用户
- 确认恢复：重装成功；安装进度里“下载镜像”一步写“从下载网关下载”并带连接数
- 相关编号：CAT-INST-12、CAT-INST-13、CAT-INST-26、SETUP-DISK-01、SETUP-GW-01、SETUP-CLOCK-01
- 演练方法：测试机上装一个有镜像包的应用，下载到一半用 iptables 丢掉到下载地址的包，过 1 分钟再放开；或把数据目录的 uploads/dl-cache 设成只读

### APP-VARS-01 应用装时算的平台变量和现在的不一样

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：应用装的时候，平台把这个平台变量的值（多半是应用的访问地址 app.url、app.scheme、app.host）算好写进了应用；装好以后改了对外方式、绑了域名，或者这台机器的设置变了，现在算出来的和装时的不一样了。应用里用的还是装时的值（例如应用里的 webhook 地址还是旧的），应用照常运行：要让它用上现在的值，用 acornfox apps reapply 重新生效。这只是提醒，不是出错
- 下一步：apps vars 里 value 是装时的值，value_now 是现在算出来的值。不影响用就先不动；要让应用用上现在的值：先 acornfox apps reapply 应用 --plan 看会改什么、会不会重启它，经用户同意后 acornfox apps reapply 应用 --yes（会按现在的值重写它的环境变量和配置文件并重启，数据不动，失败自动退回）；一次看有哪些应用落后了：acornfox apps affected
- 现象：acornfox apps vars 应用 里某个平台变量下面写着 APP-VARS-01 和现在的值；acornfox --json apps vars 应用 的 vars[] 里有 differs 为 true、code 为 APP-VARS-01 的行，notes 里也有一句

可能原因（逐条判定）：

1. 装好后用 acornfox app set --expose 改了对外方式 —— 判定：acornfox status 应用 --json 的 exposure 和装时不一样（只本机、局域网、公网）
2. 装好后给应用绑了域名 —— 判定：acornfox domain list 里有这个应用的域名；绑了域名的应用地址是 https://域名
3. 这台机器的设置变了（时区、国内或海外、服务器地址） —— 判定：acornfox catalog platform-vars 看本机现在的值

排查顺序：

1. acornfox --json apps vars 应用 看哪几行 differs 为 true（value 是装时的，value_now 是现在的）
2. acornfox status 应用 --json 看现在的对外方式和地址

处置：

- ［0 级 只读］告诉用户哪几项不一样、应用里用的还是装时的值（应用照常运行）
- ［2 级 先问用户］acornfox apps reapply 应用 --plan 看会改什么，用户同意后 acornfox apps reapply 应用 --yes（会重启这个应用，数据不动，失败自动退回）

- 必须先停下问用户：重新生效会重启应用，先问用户；不要自己去改应用的环境变量
- 确认恢复：acornfox apps vars 应用 里这几行不再带 APP-VARS-01
- 相关编号：APP-APPLY-01、APP-APPLY-03
- 演练方法：测试机上装一个只本机的应用，运行 acornfox app set --app 应用 --expose lan --no-reapply，再运行 acornfox apps vars 应用

### APP-APPLY-01 这个应用现在不能重新生效

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这个应用现在不能重新生效：它不是从应用中心装的、装的时候没保存清单或用的是旧格式的清单（那样算不出每一项该是什么），还没有上线，正在装或正在重新生效，或者它停着而这次改动要在应用里做步骤（停着的应用不能在里面做事）。什么都没改
- 下一步：看 message 是哪一种：正在装或正在重新生效的，用 acornfox install status 任务编号 --wait 等它结束再来；停着的先问用户，同意后 acornfox start 应用，再重新生效；清单没存或是旧格式的，这个应用只能删了重装（保留数据 --reuse-data，要先问用户）
- 现象：acornfox apps set 或 acornfox apps reapply 返回 APP-APPLY-01（HTTP 409）；acornfox --json apps reapply 应用 的 error.code 是 APP-APPLY-01

可能原因（逐条判定）：

1. 应用不是从应用中心装的，或装的时候没保存清单、用的是旧格式的清单 —— 判定：acornfox apps vars 应用 的 manifest.saved 为 false，或 notes 里写“旧格式”
2. 应用正在安装或正在重新生效 —— 判定：acornfox install status 任务编号 看任务；error.task_id 是正在跑的那个
3. 应用停着，而这次改动要在应用里做步骤，或者应用还没上线过 —— 判定：acornfox status 应用 --json 的 desired 是 stopped，或没有当前版本

排查顺序：

1. acornfox apps vars 应用 --json
2. acornfox status 应用 --json

处置：

- ［1 级 可以直接做］等任务结束或先启动应用后再试
- ［2 级 先问用户］清单没存或旧格式：只能删了重装（保留数据 --reuse-data）

- 必须先停下问用户：要启动停着的应用、或删了重装时先问用户
- 确认恢复：再运行 acornfox apps reapply 应用 --plan 不再报 APP-APPLY-01
- 相关编号：APP-APPLY-03、CAT-INST-04、CAT-INST-36
- 演练方法：测试机上装一个应用，acornfox stop 应用，运行 acornfox apps set 应用 某项=值（改的是要在应用里做步骤的设置），或对一个自己部署的项目运行 acornfox apps reapply 项目名

### APP-APPLY-02 这一项设置装好以后不能改

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这一项设置装好以后不能改：清单写了 editable: never（改了应用会坏，例如加密密钥）或 reinstall（要重装才能改），或者这是平台生成的值，或者把必填项清空了、值的格式不对。什么都没改
- 下一步：按 message 说的做：不能改的，不要想办法绕过（不要手工 env set）；写了“重装才能改”的，问用户同意后保留数据删掉重装（acornfox delete 应用，再 acornfox install 应用 --reuse-data）；值不对的，改对再试。acornfox apps vars 应用 看每一项能不能改
- 现象：acornfox apps set 应用 名=值 返回 APP-APPLY-02（HTTP 409）；message 里写了是哪一项、清单怎么规定的

可能原因（逐条判定）：

1. 清单给这一项写了 editable: never（改了应用就坏）或 reinstall（要重装） —— 判定：acornfox apps vars 应用 里这一行的“能不能改”
2. 把必填项清空了，或值的格式不对 —— 判定：message 里的要求；acornfox catalog info 应用 看这一项的格式

排查顺序：

1. acornfox apps vars 应用

处置：

- ［1 级 可以直接做］改成合法的值再试
- ［2 级 先问用户］要重装才能改的，用户同意后保留数据删掉重装（acornfox delete 应用，再 acornfox install 应用 --reuse-data）

- 必须先停下问用户：要重装时先问用户（会短暂停掉应用）
- 确认恢复：acornfox apps set 应用 名=值 --plan 不再报错
- 相关编号：CAT-INST-02、APP-APPLY-01
- 演练方法：测试机上装一个应用，对它清单里写了 editable: never 的参数运行 acornfox apps set 应用 名=新值

### APP-APPLY-03 重新生效没成功，已经退回原来的设置

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：重新生效没成功，平台已经把环境变量、启动命令和配置文件、应用的容器都退回了原来的样子，应用照原来的设置在运行，改动没有留下。message 里写了没成功的原因和它的编号（新的容器没起来、健康检查没通过，或清单里“重新生效后做的步骤”没通过）
- 下一步：把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；多半是用户改的值让应用起不来（例如端口、地址写错），改对了再试一次；先用 acornfox apps set … --plan 看会改什么。应用已经退回，不用再处理
- 现象：acornfox apps set 或 apps reapply 等到任务失败，error 或 install status 里是 APP-APPLY-03；应用照原来的设置在运行，apps vars 里的值没变

可能原因（逐条判定）：

1. 新设置让应用起不来或健康检查没通过（例如端口、地址写错） —— 判定：message 里带着 CAT-INST-15、CAT-INST-16 之类的编号，acornfox logs 应用 --tail 200 看日志
2. 清单里“重新生效后做的步骤”没通过 —— 判定：message 里带着 CAT-INST-17 和哪一步
3. 写配置文件失败（磁盘满、Docker 出错） —— 判定：message 里带着 CAT-INST-14；acornfox doctor 看磁盘

排查顺序：

1. acornfox install status 任务编号 --json 看哪一步没过
2. acornfox logs 应用 --tail 200
3. acornfox apps vars 应用 确认值还是原来的

处置：

- ［0 级 只读］告诉用户原因和已经退回，改对设置再试
- ［0 级 只读］不改任何东西，再运行一次 acornfox apps set … --plan 看会改什么

- 必须先停下问用户：原因看不懂或同一个原因反复出现时，带上 install status 的输出问用户
- 确认恢复：acornfox status 应用 正常，acornfox apps vars 应用 的值和改之前一样
- 相关编号：APP-APPLY-04、CAT-INST-15、CAT-INST-16、CAT-INST-17
- 演练方法：测试机上装一个应用，acornfox apps set 应用 某个会让它起不来的设置=错误的值（例如把监听端口改成别的）

### APP-APPLY-04 重新生效没成功，退回原来的设置时也出了错

- 分组：应用中心安装（CAT-INST）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：重新生效没成功，而且退回原来的样子时也出了错：应用可能停在新旧设置混在一起的状态（环境变量、配置文件或容器有的是新的、有的是旧的）。message 里写了两边的原因
- 下一步：这是要人处理的：先把 message 告诉用户，不要再重复运行重新生效，不要改密钥或删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；acornfox apps vars 应用 看环境变量是新的还是旧的；用户同意后可以 acornfox restart 应用，或 acornfox apps reapply 应用 --force 再按保存的设置生效一次
- 这一项要用户自己确认或操作
- 现象：install status 里这个任务失败，编号是 APP-APPLY-04，message 里有两个原因；应用打不开，或环境变量和配置文件对不上

可能原因（逐条判定）：

1. 退回时重启应用也没成功（Docker、磁盘出了问题） —— 判定：acornfox doctor；acornfox logs 应用 --tail 200
2. 退回时写配置文件失败 —— 判定：message 里的第二个原因

排查顺序：

1. acornfox status 应用 --json
2. acornfox apps vars 应用 --json 看环境变量是新的还是旧的
3. acornfox doctor

处置：

- ［0 级 只读］把两边的原因告诉用户
- ［2 级 先问用户］acornfox restart 应用，或 acornfox apps reapply 应用 --force 按保存的设置再生效一次

- 必须先停下问用户：总是：先问用户再重启或再生效，不要改密钥、不要删数据
- 确认恢复：acornfox status 应用 是 running，acornfox apps vars 应用 的值和环境变量对得上
- 相关编号：APP-APPLY-03

### APP-APPLY-05 这次改动会重启应用，要用户先同意

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这次改动会重启这个应用（它的地址或设置变了，应用里用到这些值），请求里没有写明用户同意重启，所以什么都没改。这是确认规则，不是出错
- 下一步：把 message 里写的会改什么、会重启哪个应用告诉用户，问他同意后在同一条命令上加 --yes（接口里是 confirm_restart: true）再运行一次；用户不想重启，可以加 --no-reapply（接口里 reapply: skip）只改访问方式，应用里的地址以后用 acornfox apps reapply 再对
- 现象：acornfox app set --expose 或 domain add 返回 APP-APPLY-05（HTTP 409），应用的访问方式、域名都没变；message 里写了会改什么、会重启哪个应用

可能原因（逐条判定）：

1. 这个应用的清单里用到了应用地址（app.url 这一族）或别的值，改对外方式、域名会让它们变，要重启才用上新的 —— 判定：message 里列的变化

排查顺序：

1. acornfox apps reapply 应用 --plan 看会改什么

处置：

- ［0 级 只读］把会改什么、会重启哪个应用告诉用户
- ［2 级 先问用户］用户同意后加 --yes 再运行一次
- ［2 级 先问用户］用户不想重启：加 --no-reapply 只改访问方式，应用里的地址之后再 acornfox apps reapply

- 必须先停下问用户：总是：会重启应用，先问用户
- 确认恢复：加 --yes 后命令成功，acornfox apps vars 应用 里 APP-VARS-01 的行没有了
- 相关编号：APP-VARS-01、APP-APPLY-03
- 演练方法：测试机上装一个清单里用了 platform.app.url 的应用，运行 acornfox app set --app 应用 --expose lan（不加 --yes）

### APP-UPG-01 这个应用现在不能升级

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这个应用现在不能升级：目录里没有比现在装的更新的版本，它不是从应用中心装的、装的时候没保存清单或用的是旧格式的清单，还没有上线，停着，正在安装、重新生效或升级，或者它来自已断开的私有目录。什么都没改
- 下一步：看 message 是哪一种：没有新版本就是已经最新，不用处理；正在装、重新生效或升级的，acornfox install status 任务编号 --wait 等它结束再来；停着的先问用户，同意后 acornfox start 应用再升级；清单没存或是旧格式的，这个应用只能删了重装（保留数据 --reuse-data，要先问用户）
- 现象：acornfox apps upgrade 应用 返回 APP-UPG-01（HTTP 409），什么都没改；apps outdated 的 blocked_code 是 APP-UPG-01

可能原因（逐条判定）：

1. 目录里没有比现在装的更新的版本 —— 判定：acornfox apps outdated 看这个应用有没有新版本；先 acornfox catalog sync 再看
2. 应用停着、还没上线，或正在安装、重新生效、升级 —— 判定：acornfox status 应用；acornfox install status 看正在跑的任务
3. 不是从应用中心装的，装的时候没保存清单，或用的是旧格式的清单 —— 判定：acornfox apps vars 应用 的说明里写了清单有没有存
4. 应用来自已断开的私有目录 —— 判定：acornfox catalog sources 看目录有没有断开

排查顺序：

1. acornfox apps outdated 看有没有新版本
2. acornfox status 应用 看它在不在运行
3. acornfox apps upgrade 应用 --plan 看 message

处置：

- ［0 级 只读］告诉用户这个应用现在升不了和原因
- ［2 级 先问用户］应用停着：用户同意后 acornfox start 应用，再升级

- 必须先停下问用户：停着的应用要启动、旧格式的应用要删了重装时，先问用户
- 确认恢复：acornfox apps upgrade 应用 --plan 不再报这个编号
- 相关编号：APP-UPG-04、APP-APPLY-01
- 演练方法：测试机上对一个已经是最新版本的应用运行 acornfox apps upgrade 应用 --plan

### APP-UPG-02 新版本要用户先补一样东西

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：新版本要用户先补一样东西才能升级：多了一项必填、又没有默认值的设置，或者要的内存比现在给这个应用的多。升级前就停下了，什么都没改，也没留快照
- 下一步：按 message 里写的补：缺的设置问到用户给的值，升级时加 --set 设置名=值（密码类写 设置名=-，从标准输入给）；内存不够就问用户，同意后 acornfox app set --app 应用 --memory 数字，再升级。补好以后先 acornfox apps upgrade 应用 --plan 看一遍
- 现象：acornfox apps upgrade 应用 --plan 里 can_upgrade 是 false，blocked_code 是 APP-UPG-02；message 里写了缺哪一项设置，或要多少内存

可能原因（逐条判定）：

1. 新版本多了一项必填、又没有默认值的设置 —— 判定：plan 的 vars_added 里列了新增的设置；message 里写了设置名
2. 新版本要的内存比现在给这个应用的多 —— 判定：message 里写了新版本要多少、现在给了多少；acornfox status 应用 看内存

排查顺序：

1. acornfox apps upgrade 应用 --plan --json 看 blocked 和 vars_added
2. 向用户要缺的设置的值，或问他同不同意调大内存

处置：

- ［0 级 只读］告诉用户缺什么
- ［2 级 先问用户］用户给了值：升级时加 --set 设置名=值
- ［2 级 先问用户］用户同意调大内存：acornfox app set --app 应用 --memory 数字

- 必须先停下问用户：设置的值和内存都要先问用户，不要自己编一个值
- 确认恢复：acornfox apps upgrade 应用 --plan 里 can_upgrade 是 true
- 相关编号：APP-UPG-03、CAT-INST-02
- 演练方法：测试机上装一个应用，再换上一个多了必填设置的新版本目录，运行 acornfox apps upgrade 应用 --plan

### APP-UPG-03 新版本这一版不能原地升级

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：新版本和现在装的在这几处不一样，这一版 AcornFox 不能原地升级：多了数据目录、提供的服务变了，或者多需要一个必须的服务（平台不替用户去接别的应用的服务）。升级前就停下了，什么都没改，也没留快照
- 下一步：把 message 里写的告诉用户：现在这个应用照常用，只是不能升到这个版本。要用新版本，用户同意后保留数据删掉重装（acornfox delete 应用，再 acornfox install 应用 --reuse-data；多了必须的服务的，先登记一条已有的：acornfox services add …）；不同意就先不升，不要想办法绕过
- 现象：acornfox apps upgrade 应用 --plan 里 can_upgrade 是 false，blocked_code 是 APP-UPG-03；message 写了新版本和现在的哪里不一样

可能原因（逐条判定）：

1. 新版本多了数据目录 —— 判定：message 里写了目录；plan 的 notes 里也有
2. 新版本提供的服务和现在的不一样 —— 判定：message 里写了两边提供的服务类型
3. 新版本多需要一个必须的服务，或某个服务的类型变了 —— 判定：plan 的 uses_changed 里列了

排查顺序：

1. acornfox apps upgrade 应用 --plan --json 看 blocked 和 uses_changed
2. acornfox services list 看有没有能提供那个服务的

处置：

- ［0 级 只读］告诉用户这个版本现在升不了和原因，应用照常用
- ［2 级 先问用户］用户同意：保留数据删掉重装（acornfox delete 应用，再 acornfox install 应用 --reuse-data）

- 必须先停下问用户：重装会删掉应用再装，总是先问用户；不要为了升级去装别的应用
- 确认恢复：acornfox apps upgrade 应用 --plan 里 can_upgrade 是 true，或重装后 acornfox status 应用 是新版本
- 相关编号：APP-UPG-02、CAT-INST-31
- 演练方法：测试机上装一个应用，再换上一个多了数据目录的新版本目录，运行 acornfox apps upgrade 应用 --plan

### APP-UPG-04 升级会重启应用，要用户先同意

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：升级会重启这个应用（先留一份快照，再换新版本，中间有一段时间用不了），请求里没有写明用户同意，所以什么都没做。这是确认规则，不是出错
- 下一步：把 acornfox apps upgrade 应用 --plan 说的会做什么、要停多久告诉用户，问他同意后在同一条命令上加 --yes（接口里是 confirm_restart: true）再运行一次
- 现象：acornfox apps upgrade 应用 返回 APP-UPG-04（HTTP 409），什么都没做；message 里写了会做什么、会停多久

可能原因（逐条判定）：

1. 升级一定会重启应用（先留快照，再换新版本），请求里没有写明用户同意 —— 判定：message 里是 plan 的 summary

排查顺序：

1. acornfox apps upgrade 应用 --plan 看会做什么

处置：

- ［0 级 只读］把 plan 的 summary 告诉用户
- ［2 级 先问用户］用户同意后加 --yes 再运行一次

- 必须先停下问用户：总是：会重启应用，先问用户
- 确认恢复：加 --yes 后命令开始升级，acornfox install status 任务编号 --wait 看结果
- 相关编号：APP-UPG-05、APP-APPLY-05
- 演练方法：测试机上装一个应用，运行 acornfox apps upgrade 应用（不加 --yes）

### APP-UPG-05 升级没成功，已经退回到升级前

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：升级没成功，平台已经自动退回到升级前：数据、设置、程序版本都恢复成升级前留的快照，应用照原来的版本在运行。message 里写了没成功的原因和它的编号（新版本没拉下来、没留成快照、容器没起来、健康检查没通过，或清单里“重新生效后做的步骤”没通过）
- 下一步：把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；原因是磁盘不够、网络不通这类的，处理好了再升级一次；应用已经退回，不用再处理，也不要重复升级
- 现象：acornfox apps upgrade 等到任务失败，error 或 install status 里是 APP-UPG-05；应用照旧在运行，版本还是升级前的，数据和设置也是升级前的

可能原因（逐条判定）：

1. 新版本的镜像没拉下来（网络、磁盘） —— 判定：message 里带着 CAT-INST-12 之类的编号；acornfox doctor
2. 没能留成升级前的快照（磁盘不够） —— 判定：message 里带着 BKP-01；acornfox backup list 应用 看有没有旧备份可以删
3. 新版本的容器没起来或健康检查没通过 —— 判定：message 里带着 CAT-INST-15、CAT-INST-16；acornfox logs 应用 --tail 200
4. 清单里“重新生效后做的步骤”没通过 —— 判定：message 里带着 CAT-INST-17 和哪一步

排查顺序：

1. acornfox install status 任务编号 --json 看哪一步没过
2. acornfox status 应用 确认版本是升级前的
3. acornfox logs 应用 --tail 200

处置：

- ［0 级 只读］告诉用户原因和已经退回
- ［0 级 只读］原因是磁盘、网络：处理好后再 acornfox apps upgrade 应用 --plan
- ［2 级 先问用户］用户同意后再升级一次

- 必须先停下问用户：同一个原因反复出现时，带上 install status 的输出问用户，不要一直重试
- 确认恢复：acornfox status 应用 正常，acornfox apps outdated 里它还是原来的版本、新版本还在
- 相关编号：APP-UPG-06、BKP-01、CAT-INST-15、CAT-INST-16
- 演练方法：测试机上装一个应用，把目录换成一个健康检查一定不过的新版本，运行 acornfox apps upgrade 应用 --yes

### APP-UPG-06 升级没成功，退回到升级前时也出了错

- 分组：应用中心安装（CAT-INST）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：升级没成功，而且退回到升级前时也出了错：应用可能停在新旧混在一起的状态（程序是旧的、数据可能已经被新版本改过，或设置有的新有的旧）。message 里写了两边的原因，升级前的快照还在
- 下一步：这是要人处理的：先把 message 告诉用户，不要再重复升级，不要删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；用户同意后用升级前的快照恢复：acornfox backup list 应用 找到类型是升级前的那份，再 acornfox backup restore 应用 备份编号 --yes（先 --plan 看会怎样）
- 这一项要用户自己确认或操作
- 现象：install status 里这个任务失败，编号是 APP-UPG-06，message 里有两个原因；应用打不开，或版本、数据和设置对不上

可能原因（逐条判定）：

1. 用快照退回时失败（磁盘不够存恢复前备份、快照文件有问题、Docker 出错） —— 判定：message 里的第二个原因（带 BKP 编号）；acornfox doctor
2. 快照退回不成，改退程序和设置时也失败 —— 判定：message 里写了

排查顺序：

1. acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子
2. acornfox backup list 应用 看升级前的快照还在不在
3. acornfox apps vars 应用 看设置是新的还是旧的

处置：

- ［0 级 只读］先把 message 告诉用户，什么都不要再动
- ［2 级 先问用户］用户同意后用升级前的快照恢复：acornfox backup restore 应用 备份编号 --yes

- 必须先停下问用户：总是：数据可能已经被新版本改过，恢复会覆盖现在的数据，先问用户
- 确认恢复：acornfox status 应用 正常，acornfox apps vars 应用 的值和升级前一样
- 回滚：恢复前会先另存现在的数据（备份类型“恢复前”），恢复错了可以再恢复它
- 相关编号：APP-UPG-05、BKP-08、APP-APPLY-04
- 演练方法：较难造；纸面演练：升级失败的同时让备份目录写不进去

### APP-UPG-07 没有可以退回的东西

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：没有可以退回的东西：这个应用还没升级过（没有升级前的快照），或者快照已经被后来的升级覆盖、被删掉，或者快照就是现在这个版本的，或者快照的文件不齐、那个版本的程序已经不在这台机器上了。什么都没改
- 下一步：看 message 是哪一种：没升级过或已经退回过，就没有可退的，不用处理；快照坏了或程序没了，告诉用户这条路走不通，别的备份用 acornfox backup list 应用 看有没有能用的
- 现象：acornfox apps rollback 应用 返回 APP-UPG-07（HTTP 409），什么都没改

可能原因（逐条判定）：

1. 这个应用还没升级过，或快照已经被后来的升级覆盖、被删掉 —— 判定：acornfox backup list 应用 里没有类型是升级前的备份
2. 快照就是现在这个版本的（已经退回过） —— 判定：acornfox apps outdated 里这个应用的 can_rollback 是 false
3. 快照的文件不齐，或那个版本的程序已经不在这台机器上 —— 判定：acornfox apps rollback 应用 --plan 的 blocked 里写了

排查顺序：

1. acornfox apps rollback 应用 --plan 看 blocked
2. acornfox backup list 应用

处置：

- ［0 级 只读］告诉用户没有可退的和原因

- 必须先停下问用户：没有能用的快照时，别的备份能不能用要问用户
- 确认恢复：acornfox apps rollback 应用 --plan 里 can_rollback 是 true
- 相关编号：APP-UPG-08、BKP-05、BKP-09
- 演练方法：测试机上对一个没升级过的应用运行 acornfox apps rollback 应用 --plan

### APP-UPG-08 退回会丢掉升级后的新数据，要用户先同意

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：退回到升级前会用升级前留的快照覆盖现在的数据：升级之后写进应用的新数据会没有（退回前会先把现在的数据另存一份，需要时能找回），应用会重启一次。请求里没有写明用户同意，所以什么都没做。这是确认规则，不是出错
- 下一步：把 acornfox apps rollback 应用 --plan 说的告诉用户，特别是“升级后的新数据会没有”，问他同意后加 --yes（接口里是 confirm: true）再运行一次
- 现象：acornfox apps rollback 应用 返回 APP-UPG-08（HTTP 409），什么都没做

可能原因（逐条判定）：

1. 退回要用升级前的快照覆盖现在的数据，请求里没有写明用户同意 —— 判定：message 里是 plan 的 summary

排查顺序：

1. acornfox apps rollback 应用 --plan 看会做什么

处置：

- ［0 级 只读］把 plan 的 summary 告诉用户，特别是升级后的新数据会没有
- ［2 级 先问用户］用户同意后加 --yes 再运行一次

- 必须先停下问用户：总是：会丢数据，先问用户
- 确认恢复：加 --yes 后命令开始退回，acornfox install status 任务编号 --wait 看结果
- 相关编号：APP-UPG-07、BKP-06
- 演练方法：测试机上升级一个应用后运行 acornfox apps rollback 应用（不加 --yes）

### APP-UPG-09 退回没成功，应用保持退回前的样子

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：退回没成功，应用已经保持退回前的样子（还是升级后的版本，数据没变）。message 里写了原因和它的编号（磁盘不够、快照有问题、容器没起来……）
- 下一步：把 message 里的原因告诉用户；原因里的编号用 acornfox faults 编号 看怎么办；处理好了可以再退回一次
- 现象：acornfox apps rollback 等到任务失败，error 或 install status 里是 APP-UPG-09；应用还是升级后的版本，数据没变

可能原因（逐条判定）：

1. 磁盘不够存退回前的那份数据 —— 判定：message 里带着 BKP-01
2. 快照有问题，或那个版本的程序不在了 —— 判定：message 里带着 BKP-05、BKP-09
3. 换回旧版本后容器没起来 —— 判定：message 里带着 BKP-07 和原因；acornfox logs 应用 --tail 200

排查顺序：

1. acornfox install status 任务编号 --json 看哪一步没过
2. acornfox status 应用 确认版本还是升级后的

处置：

- ［0 级 只读］告诉用户原因，应用没变
- ［0 级 只读］处理好原因后再 acornfox apps rollback 应用 --plan

- 必须先停下问用户：同一个原因反复出现时带上输出问用户
- 确认恢复：acornfox status 应用 正常；处理好后再退回能成功
- 相关编号：APP-UPG-10、BKP-07、BKP-01
- 演练方法：测试机上升级一个应用，把备份目录所在磁盘填满后运行 acornfox apps rollback 应用 --yes

### APP-UPG-10 退回没成功，回到退回前的样子时也出了错

- 分组：应用中心安装（CAT-INST）
- 默认等级：P1（应用或服务停了，马上处理）
- 是什么：退回没成功，而且回到退回前的样子时也出了错：应用可能停在新旧混在一起的状态。message 里写了两边的原因，退回前另存的那份数据还在
- 下一步：这是要人处理的：先把 message 告诉用户，不要再重复退回，不要删数据；acornfox status 应用 和 acornfox logs 应用 --tail 200 看现在的样子；acornfox backup list 应用 里有退回前另存的那份（类型是恢复前），问用户同意后用它恢复
- 这一项要用户自己确认或操作
- 现象：install status 里这个任务失败，编号是 APP-UPG-10；应用打不开，或数据、版本对不上

可能原因（逐条判定）：

1. 退回途中失败，用退回前另存的那份数据回来时也失败 —— 判定：message 里带着 BKP-08 和另存那份的备份编号

排查顺序：

1. acornfox status 应用 和 acornfox logs 应用 --tail 200
2. acornfox backup list 应用 找类型是恢复前的那份

处置：

- ［0 级 只读］先把 message 告诉用户，什么都不要再动
- ［2 级 先问用户］用户同意后用恢复前那份恢复：acornfox backup restore 应用 备份编号 --yes

- 必须先停下问用户：总是：恢复会覆盖现在的数据，先问用户
- 确认恢复：acornfox status 应用 正常，数据是用户认的那一份
- 相关编号：APP-UPG-09、BKP-08
- 演练方法：较难造；纸面演练

## 连接私有目录（CAT-CONN）

### CAT-CONN-01 连接码格式不对

- 分组：连接私有目录（CAT-CONN）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：连接码格式不对（应是 AFXC- 加 16 个字母数字），没有发给网关
- 下一步：让用户照原样再抄一遍；0、1、8 会当成 O、I、B
- 现象：acornfox catalog connect 返回 CAT-CONN-01（HTTP 400）

可能原因（逐条判定）：

1. 连接码抄错、少了几位或多了空格 —— 判定：数一下：AFXC- 后面是不是 16 位字母数字

排查顺序：

1. 请用户再看一遍连接码

处置：

- ［2 级 先问用户］用用户重新抄的码再连

- 必须先停下问用户：连接码只能用户提供
- 确认恢复：connect 成功

### CAT-CONN-02 这台服务器关掉了下载网关

- 分组：连接私有目录（CAT-CONN）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这台服务器关掉了下载网关，连不了私有目录
- 下一步：问用户是否打开下载网关（去掉 ACORNFOX_GATEWAY=off 或 network.json 里的 "gateway": "off"）
- 现象：acornfox catalog connect 返回 CAT-CONN-02（HTTP 409）

可能原因（逐条判定）：

1. ACORNFOX_GATEWAY 设成了空，或 network.json 的 gateway 是 off［doctor 自动判定］ —— 判定：doctor 的下载网关一项写“已关掉”

排查顺序：

1. sudo acornfox doctor

处置：

- ［2 级 先问用户］打开下载网关（改配置后重启服务）

- 必须先停下问用户：总是
- 确认恢复：doctor 的下载网关一项写“能连上”，connect 成功
- 相关编号：SETUP-GW-01

### CAT-CONN-03 连接码不存在、抄错了，或已被别的机器用过

- 分组：连接私有目录（CAT-CONN）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：连接码不存在、抄错了，或已经被别的机器用过（一码一机）
- 下一步：让用户核对连接码；还不行就找给码的人要新码。不要反复试
- 现象：connect 返回 CAT-CONN-03（HTTP 404），gateway_code 是 GW-CAT-01

可能原因（逐条判定）：

1. 码抄错 —— 判定：和原文逐位对
2. 这个码已经在别的机器上用过 —— 判定：问用户是不是在别的机器上连过

排查顺序：

1. 核对连接码

处置：

- ［2 级 先问用户］用核对过的码再试一次

- 必须先停下问用户：要新码时
- 确认恢复：connect 成功
- 相关编号：GW-CAT-01

### CAT-CONN-04 连接码已过期

- 分组：连接私有目录（CAT-CONN）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：连接码已过期
- 下一步：找给码的人要新码
- 现象：connect 返回 CAT-CONN-04（HTTP 410），gateway_code 是 GW-CAT-02

可能原因（逐条判定）：

1. 码过了有效期 —— 判定：gateway_code 是 GW-CAT-02

排查顺序：

1. 请用户要新码

处置：

- ［2 级 先问用户］拿到新码后再连

- 必须先停下问用户：总是
- 确认恢复：connect 成功
- 相关编号：GW-CAT-02

### CAT-CONN-05 这个私有目录已停用

- 分组：连接私有目录（CAT-CONN）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这个私有目录已停用
- 下一步：告诉用户联系给码的人
- 现象：connect 返回 CAT-CONN-05（HTTP 403），gateway_code 是 GW-CAT-03

可能原因（逐条判定）：

1. 给码的人停用了目录 —— 判定：gateway_code 是 GW-CAT-03

排查顺序：

1. 转述给用户

处置：

- ［0 级 只读］告诉用户联系给码的人

- 必须先停下问用户：—（不用问）
- 确认恢复：—
- 相关编号：GW-CAT-03

### CAT-CONN-06 这一小时试连接码的次数用完了

- 分组：连接私有目录（CAT-CONN）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这台机器这一小时试连接码的次数用完了
- 下一步：等 retry_after 秒后再试，不要循环重试
- 现象：connect 返回 CAT-CONN-06（HTTP 429），带 retry_after

可能原因（逐条判定）：

1. 短时间里试了很多次码 —— 判定：gateway_code 是 GW-CAT-04

排查顺序：

1. 读 retry_after

处置：

- ［1 级 可以直接做］等够秒数再试一次

- 必须先停下问用户：—（不用问）
- 确认恢复：connect 成功
- 相关编号：GW-CAT-04

### CAT-CONN-07 连不上下载网关、网关拒绝，或本机钥匙读不出

- 分组：连接私有目录（CAT-CONN）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：连不上下载网关、网关拒绝（看 gateway_code），或本机钥匙读不出
- 下一步：GW-AUTH-01 先校准本机时间；GW-NET-01 看网络和代理；ID-KEY 看 acornfox identity show；稍后再试
- 现象：connect 返回 CAT-CONN-07（HTTP 502），带 gateway_code

可能原因（逐条判定）：

1. 连不上网关［doctor 自动判定］ —— 判定：gateway_code 是 GW-NET-01；doctor 的下载网关一项
2. 本机时钟不准［doctor 自动判定］ —— 判定：gateway_code 是 GW-AUTH-01；doctor 的时钟一项
3. 本机钥匙读不出 —— 判定：gateway_code 是 ID-KEY；sudo acornfox identity show
4. 网关别的拒绝 —— 判定：gateway_code 的条目：acornfox faults <gateway_code>

排查顺序：

1. 读 gateway_code
2. sudo acornfox doctor
3. sudo acornfox identity show

处置：

- ［1 级 可以直接做］网络好了以后再试
- ［2 级 先问用户］校准时钟

- 必须先停下问用户：改系统设置时
- 确认恢复：connect 成功
- 相关编号：SETUP-GW-01、SETUP-CLOCK-01、ID-KEY-01

### CAT-CONN-08 网关已经授权，但本机没记下来

- 分组：连接私有目录（CAT-CONN）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：网关已经授权，但本机没记下来（数据目录写不进去）
- 下一步：检查数据目录权限和磁盘空间后，用同一个连接码再连一次（同一台机器重复用不算新的一次）
- 现象：connect 返回 CAT-CONN-08（HTTP 500）

可能原因（逐条判定）：

1. 数据目录写不进去（磁盘满、权限） —— 判定：doctor 的磁盘一项；ls -ld /var/lib/acornfox/catalog

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］处理磁盘后用同一个码再连

- 必须先停下问用户：要清理磁盘时
- 确认恢复：catalog sources 里有这个目录
- 相关编号：SETUP-DISK-01

## 服务登记（CAT-SVC）

### CAT-SVC-01 目录里没有这种服务类型

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：目录里没有这种服务类型：类型名写错了，或者这台机器的目录太旧
- 下一步：acornfox services kinds 看有哪些类型；目录太旧就 acornfox catalog sync 后再试
- 现象：acornfox services add 返回 CAT-SVC-01（HTTP 400）；改一条服务时说它的类型现在的目录里没有定义

可能原因（逐条判定）：

1. 类型名写错了（大小写、下划线） —— 判定：acornfox services kinds 的列表里没有写的那个名字
2. 这台机器的应用目录太旧，还没有这种类型 —— 判定：acornfox catalog list 看目录序号和上次同步；acornfox doctor 看目录同步一项
3. 应用目录读不了 —— 判定：message 写“应用目录读不了”；acornfox catalog list 也报 CAT-INST-19

排查顺序：

1. acornfox services kinds
2. acornfox catalog sync
3. 再 acornfox services kinds

处置：

- ［1 级 可以直接做］照 services kinds 列出的名字改类型再登记
- ［1 级 可以直接做］acornfox catalog sync 同步目录

- 必须先停下问用户：目录同步后还是没有用户要的那种类型：告诉用户这一版还不支持，问要不要换一种
- 确认恢复：acornfox services add 成功，services list 里有这一条
- 相关编号：CAT-INST-19

### CAT-SVC-02 服务的名字、项或值不对

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：登记或修改服务时名字、项或值不对：缺了必填项、没有这一项、值的格式不对（数字、true/false、网址、可选值），或者名字不合规矩
- 下一步：按 message 改；acornfox services kinds 看这种服务有哪些项、哪些必填、能填什么
- 现象：acornfox services add 或 set 返回 CAT-SVC-02（HTTP 400），message 写了是哪一项、哪里不对

可能原因（逐条判定）：

1. 缺了必填项 —— 判定：message 写“还差必填项：…”
2. 写了这种服务没有的项 —— 判定：message 写“没有这些项：…（有：…）”
3. 值的格式不对：整数、true 或 false、http:// 开头的网址、可选值之一 —— 判定：message 写“要是整数”“要写 true 或 false”“要是 http:// 或 https:// 开头的网址”“只能是 … 之一”
4. 名字不合规矩：空的、有空格或标点、是保留的词 —— 判定：message 写“名字 … 不行”或“是保留的词”
5. 要清掉的是必填项 —— 判定：message 写“是必填项，不能清掉”
6. 值里有换行或太长 —— 判定：message 写“只能是一行”或“太长”

排查顺序：

1. 读 message
2. acornfox services kinds 看这种服务的项

处置：

- ［1 级 可以直接做］照 message 改参数再执行一次

- 必须先停下问用户：缺的是只有用户知道的值（地址、账号、密码、Key）：问用户要，保密项让用户用 项=- 从标准输入给
- 确认恢复：命令成功，acornfox services show 名字 里是改后的值

### CAT-SVC-03 已经有同名的服务

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这台机器上已经有同名的服务
- 下一步：换个名字（--name）；要改已有的那条用 acornfox services set
- 现象：acornfox services add 返回 CAT-SVC-03（HTTP 409）

可能原因（逐条判定）：

1. 以前登记过同名的 —— 判定：acornfox services list 里有这个名字，来源写“自己登记的”
2. 装好的应用提供的服务用了这个名字（默认就是应用名） —— 判定：acornfox services list 里这个名字的来源写“由应用 … 提供”

排查顺序：

1. acornfox services list

处置：

- ［1 级 可以直接做］换一个名字登记
- ［2 级 先问用户］要改已有那条的值：acornfox services set 名字 项=值

- 必须先停下问用户：已有那条是不是就是用户要的、要不要改它，问用户
- 确认恢复：acornfox services list 里有新的那条

### CAT-SVC-04 还有应用在用这条服务，不能删

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：还有应用在用这条服务，不能删
- 下一步：错误里列着是哪些应用在用它；要删这条服务，得先删掉这些应用
- 现象：acornfox services remove 名字 --yes 或 DELETE /v1/services/{名字} 返回 CAT-SVC-04（HTTP 409），带 used_by；services remove 不加 --yes 试跑时写“现在删不掉：还有应用在用它”

可能原因（逐条判定）：

1. 有应用装的时候接上了这条服务 —— 判定：acornfox services show 名字 的“在用它的”一行；错误里的 used_by

排查顺序：

1. acornfox services show 名字
2. 把 used_by 里的应用告诉用户

处置：

- ［2 级 先问用户］删掉在用它的应用后再删这条服务（acornfox delete 应用）

- 必须先停下问用户：总是：删应用会让它停掉，先问用户
- 确认恢复：acornfox services remove 名字 --yes 成功，services list 里没有这一条

### CAT-SVC-05 这条服务是应用提供的，不能这样改或删

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这条服务是一个应用提供的：只有它要你在应用里做一步再填进来的那几项能填，别的项由应用算出来；它也不能单独删，删掉那个应用它才会消失
- 下一步：要填的项用 acornfox services set 名字 项=值；要它消失就删掉提供它的应用
- 现象：acornfox services set 或 remove 返回 CAT-SVC-05（HTTP 409）

可能原因（逐条判定）：

1. 要改的项是应用算出来的或者按使用方单独发的 —— 判定：acornfox services show 名字：这一项没标“要你做一步”，或者列在“每个使用它的应用单独发”里
2. 想单独删一条应用提供的服务 —— 判定：acornfox services show 名字 的来源写“由应用 … 提供”
3. 提供它的应用已经删了，或者读不到它保存的清单 —— 判定：message 写“已经删了”或“读不到应用 … 保存的清单”；应用删了时这条服务可以直接 remove

排查顺序：

1. acornfox services show 名字
2. 读 message

处置：

- ［2 级 先问用户］只填标“要你做一步”的项：acornfox services set 名字 项=-
- ［2 级 先问用户］删掉提供它的应用（acornfox delete 应用）

- 必须先停下问用户：要填的值只有用户知道（要在应用里操作一步才拿得到）；删应用前也要问用户
- 确认恢复：acornfox services show 名字 的状态是可用

### CAT-SVC-06 试连没通

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：试连没通：连不上那个地址，或者连上了但回答不对
- 下一步：看 detail 写的原因：拒绝连接、超时、找不到地址查地址和端口；回答的状态码不对查 Key 或账号；改值用 acornfox services set
- 现象：acornfox services test 名字 输出“没通：…”、退出码 1；--json 里 ok 是 false、code 是 CAT-SVC-06

可能原因（逐条判定）：

1. 地址或端口写错了 —— 判定：detail 写“拒绝连接”或“找不到这个地址”；acornfox services show 名字 核对地址、端口
2. 那台机器上的服务没开，或者被防火墙挡了 —— 判定：detail 写“拒绝连接”或“超时”；问用户那边的服务开着没有
3. 这台服务器到那个地址的网络不通 —— 判定：detail 写“网络不通”或“超时”；acornfox doctor 看网络
4. Key、账号或密码不对 —— 判定：detail 写“回了 401”或“回了 403”
5. 证书有问题 —— 判定：detail 写“证书校验没过”

排查顺序：

1. 读 detail
2. acornfox services show 名字
3. 改值后再 acornfox services test 名字

处置：

- ［0 级 只读］再试一次（acornfox services test 名字）
- ［2 级 先问用户］照用户给的正确值改：acornfox services set 名字 项=值

- 必须先停下问用户：地址、端口、Key 对不对只有用户知道：把 detail 原样告诉用户，问正确的值
- 确认恢复：acornfox services test 名字 输出“通了”
- 相关编号：CAT-SVC-02

### CAT-SVC-07 地址写的是本机回环地址

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：地址写的是这台机器的本机回环地址（127.0.0.1、localhost、::1）：应用跑在容器里，容器里的这个地址指的是容器自己，连不到这台机器上的服务
- 下一步：服务在这台机器上：改填这台机器在局域网里的地址；服务是 AcornFox 装的应用：不用登记，装好后它会自己出现在服务列表里
- 现象：acornfox services add 或 set 返回 CAT-SVC-07（HTTP 400），message 写了是哪一项

可能原因（逐条判定）：

1. 服务就在这台机器上，用户照习惯写了 127.0.0.1 或 localhost —— 判定：message 里那一项写的是 127.0.0.1、localhost 或 ::1
2. 这个服务是 AcornFox 装的应用提供的，不需要登记 —— 判定：acornfox services list 里已经有它（来源写“由应用 … 提供”）

排查顺序：

1. acornfox services list
2. 问用户服务在哪台机器上

处置：

- ［2 级 先问用户］改填这台机器在局域网里的地址再登记
- ［1 级 可以直接做］已经有应用提供的同类服务：不登记，直接用它

- 必须先停下问用户：这台机器的局域网地址、服务监听在哪个地址，问用户
- 确认恢复：acornfox services add 成功，acornfox services test 名字 通了
- 相关编号：CAT-SVC-06

### CAT-SVC-08 应用用的服务现在不可用

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：acornfox doctor 和控制台“运行环境”页会看服务登记表：已装的应用接着的每一条服务现在能不能用。不能用的（提供它的应用停着或已删，还差必填项没填）各给一条提醒，写明是哪个应用的哪个需要、接的哪条服务、现在什么状态。应用本身照常运行，只是用到这项服务的功能（例如调大模型、发邮件）用不了。这一项只看登记表，不去连那个服务；要试通不通用 acornfox services test
- 下一步：按提醒里写的做：提供它的应用停着，问用户后 acornfox start 它；还差哪几项，acornfox services set 服务名 项=值（密码、Key 写成 项=-，让用户自己在终端输入）；提供它的应用已删，问用户是接上旧数据装回来，还是让这些应用改接别的服务
- 现象：doctor 或运行环境页的“应用用的服务”一项警告（CAT-SVC-08）；acornfox status 应用 的“它用的服务”里有一条不是“可用”；应用里调大模型、发邮件这类功能报错或没反应

可能原因（逐条判定）：

1. 提供这条服务的应用停着（用户停的，或者停了以后没再启动）［doctor 自动判定］ —— 判定：提醒写“提供它的应用停着”；acornfox apps 里那个应用的期望状态是 stopped
2. 提供这条服务的应用删掉了（删的时候保留了数据）［doctor 自动判定］ —— 判定：提醒写“提供它的应用已删”；acornfox apps 里没有那个应用
3. 这条服务还差必填项：用户登记时没填，或者提供它的应用要用户在应用里做一步再填进来［doctor 自动判定］ —— 判定：提醒写“还差 … 没填”；acornfox services show 服务名 看哪几项没值、标“要你做一步”的项写了怎么拿到
4. 登记表里存的值解不开（数据目录里的加密钥匙换过或丢了）［doctor 自动判定］ —— 判定：提醒写“存的值解不开”；acornfox services list 的输出里列着这几条

排查顺序：

1. acornfox doctor 看“应用用的服务”那一行
2. acornfox services list 看每条服务的状态和谁在用
3. acornfox services show 服务名 看还差哪几项
4. acornfox apps 看提供它的应用在不在、停没停

处置：

- ［2 级 先问用户］提供它的应用停着：问用户是不是有意停的，不是就 acornfox start 那个应用
- ［2 级 先问用户］还差必填项：问用户要值，用户自己运行 acornfox services set 服务名 项=- 输入，或者在控制台“服务”页里填
- ［2 级 先问用户］提供它的应用已删：问用户后用 acornfox install --name 原来的名字 --reuse-data 装回来（平台从删除时保留的记录里找到它原来是应用中心里的哪个应用；可以先加 --plan 看一次；报 CAT-INST-02 说找不到记录的，要写上应用中心里的名字：acornfox install 应用 --name 原来的名字 --reuse-data）；或者让用到它的应用改接别的服务（删掉重装时用 --use 指定）
- ［0 级 只读］值解不开：不要删这几条服务，把 doctor 的输出交给维护的人

- 必须先停下问用户：启动、重装应用，填密码和 Key，改接别的服务之前都先问用户；提供它的应用是用户有意停的，就不用处理
- 确认恢复：acornfox doctor 的“应用用的服务”一项通过（写“都可用”）；acornfox services list 里这条服务的状态是“可用”
- 相关编号：CAT-SVC-05、CAT-INST-33
- 演练方法：在测试机上装一个提供服务的应用和一个接着它的应用，acornfox stop 提供方，再运行 acornfox doctor：“应用用的服务”一项警告，写明是哪个应用接的哪条服务、“提供它的应用停着”；acornfox start 提供方后这一项通过

### CAT-SVC-09 服务还不能用（就绪检查没通过）

- 分组：服务登记（CAT-SVC）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：应用提供的服务在运行，但它清单里写的就绪检查（provides 的 ready）现在没通过：例如大模型网关还没配上游、数据库的管理账号还没建好。平台在看服务（acornfox services list、show）、看计划、装完和 doctor 时现跑这个检查（只读，每条最多几秒），结果不存。没通过不挡安装：别的应用照样能接上它，只是用到它的功能要等它好了才能用。提醒里是提供它的应用的清单写的那句话，说缺什么、到哪里去补
- 下一步：把提醒里清单写的那句话原样告诉用户，照它说的做（多半要用户在提供它的应用里做一步）；acornfox services show 服务名 看检查哪一步没过（ready_detail）。做完再 acornfox services show 服务名，状态是“可用”就好了
- 现象：doctor 或运行环境页警告 CAT-SVC-09，写“服务 … 还不能用：…”；acornfox services list 里这条服务的状态是“还不能用：…”；--json 里 status 是 not_ready，ready_say 是原因；装应用的计划（--plan）里这个需要的 tell、装好后的 must_tell 里有“服务 … 现在还不能用：…”

可能原因（逐条判定）：

1. 提供它的应用装好了，但还差用户在它里面做的一步（例如配上游、建管理账号）［doctor 自动判定］ —— 判定：ready_say 写了缺什么；acornfox services show 服务名
2. 提供它的应用刚启动、还没准备好 —— 判定：过一会儿再 acornfox services show 服务名；acornfox logs 提供方 --tail 200
3. 检查超时：提供方很慢或卡住了 —— 判定：ready_detail 写“就绪检查在 … 秒内没做完”；acornfox status 提供方、acornfox logs 提供方

排查顺序：

1. acornfox services show 服务名
2. acornfox status 提供方
3. acornfox logs 提供方 --tail 200

处置：

- ［0 级 只读］把 ready_say 原样告诉用户，请用户在提供它的应用里照着做
- ［0 级 只读］提供方刚启动的，过一会儿再看一次

- 必须先停下问用户：要在提供方应用里做的那一步只能用户做（要登录、要填 Key 等）；重启或重装提供方之前先问用户
- 确认恢复：acornfox services show 服务名 的状态是“可用”；acornfox doctor 不再有 CAT-SVC-09
- 相关编号：CAT-SVC-08
- 演练方法：在测试机上装一个写了 ready 的提供方，让它的就绪检查不过（例如数据库提供方的检查数账号、还没有使用方时是 0 个），运行 acornfox services list 和 acornfox doctor：状态“还不能用”，doctor 警告 CAT-SVC-09；装一个使用方后这一项通过

## 应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）

### CAT-SYNC-01 程序里没有受信公钥，不同步目录

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：只用程序内置的目录；升级到带公钥的版本后会自动联网同步
- 现象：acornfox catalog sync 返回 CAT-SYNC-01（skipped）；doctor 的应用目录一项写“没有受信公钥，只用内置目录”

可能原因（逐条判定）：

1. 这个版本的程序没编进目录签名公钥 —— 判定：catalog sync --json 的 skipped 为 true

排查顺序：

1. acornfox --json version

处置：

- ［0 级 只读］不用处理

- 必须先停下问用户：—（不用问）
- 确认恢复：—

### CAT-SYNC-02 目录来源连不上、超时或返回了非 200

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：检查服务器能不能访问 acornfox.com 和 gitee.com；稍后再 acornfox catalog sync
- AcornFox 已经自动做的：这个来源失败后自动换下一个来源
- 现象：catalog sync --json 的 attempts 里某个来源是 CAT-SYNC-02

可能原因（逐条判定）：

1. 网络不通、DNS 解析不了 —— 判定：curl -sS -m 10 -o /dev/null -w '%{http_code}' https://acornfox.com/catalog/version.txt
2. 代理或防火墙拦了 —— 判定：换一个网络能通
3. ACORNFOX_CATALOG_SOURCES 指向了不存在的地址 —— 判定：systemctl show acornfox-server -p Environment

排查顺序：

1. curl 目录来源的 version.txt

处置：

- ［1 级 可以直接做］稍后再同步
- ［2 级 先问用户］改代理或防火墙

- 必须先停下问用户：要改网络设置时
- 确认恢复：catalog sync 成功
- 相关编号：SETUP-SYNC-01、CAT-SYNC-08

### CAT-SYNC-03 目录来源返回的内容读不懂

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：多半是地址指错或中间有代理改了内容；稍后再同步，还不行就反馈
- AcornFox 已经自动做的：自动换下一个来源，没写进缓存
- 现象：attempts 里某个来源是 CAT-SYNC-03

可能原因（逐条判定）：

1. 来源地址指到了别的网页 —— 判定：curl 来源的 version.txt 不是一个数字
2. 代理或网关改了应答（例如登录页） —— 判定：换网络后正常
3. index.json 格式坏了或超过大小上限 —— 判定：curl 下来看

排查顺序：

1. curl 来源的 version.txt 和 index.json

处置：

- ［1 级 可以直接做］稍后再同步

- 必须先停下问用户：持续出现时带上证据包反馈
- 确认恢复：catalog sync 成功

### CAT-SYNC-04 目录的签名验不过

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：没有用它，继续用手上的目录。持续出现就停下反馈：来源上的目录可能被篡改
- AcornFox 已经自动做的：没写进缓存，自动换下一个来源
- 现象：attempts 里某个来源是 CAT-SYNC-04

可能原因（逐条判定）：

1. 来源上的文件被替换或损坏 —— 判定：换来源后正常
2. 目录用了新钥匙签名，这个版本的程序还不认识 —— 判定：升级 AcornFox 后正常

排查顺序：

1. acornfox catalog sync --json 看每个来源

处置：

- ［0 级 只读］继续用手上的目录
- ［2 级 先问用户］绕过签名——绝不

- 必须先停下问用户：持续出现时：带上证据包反馈
- 确认恢复：catalog sync 成功

### CAT-SYNC-05 version.txt 和签名里的序号不一致（来源正在发布）

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：等几分钟再 acornfox catalog sync
- AcornFox 已经自动做的：自动换下一个来源
- 现象：attempts 里某个来源是 CAT-SYNC-05

可能原因（逐条判定）：

1. 来源正在发布新目录，文件换了一半 —— 判定：几分钟后再同步就好了

排查顺序：

1. 等几分钟

处置：

- ［1 级 可以直接做］稍后再同步

- 必须先停下问用户：—（不用问）
- 确认恢复：catalog sync 成功

### CAT-SYNC-06 来源上的目录不比手上的新（旧镜像或重放）

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：手上的已经更新；镜像来源过一阵会跟上
- AcornFox 已经自动做的：拒绝用更旧的目录，继续用手上的
- 现象：attempts 里某个来源是 CAT-SYNC-06

可能原因（逐条判定）：

1. Gitee 备用来源还没跟上官网，或有人拿旧目录冒充 —— 判定：catalog sources 的 serial 比来源的 version.txt 大

排查顺序：

1. acornfox catalog sources

处置：

- ［0 级 只读］不用处理

- 必须先停下问用户：—（不用问）
- 确认恢复：—

### CAT-SYNC-07 本机的目录缓存写不进去

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：检查数据目录所在磁盘和权限（acornfox doctor），处理后再 acornfox catalog sync
- AcornFox 已经自动做的：旧的缓存原样保留
- 现象：catalog sync 返回 CAT-SYNC-07

可能原因（逐条判定）：

1. 磁盘满了［doctor 自动判定］ —— 判定：doctor 的磁盘一项
2. 数据目录权限被改过 —— 判定：ls -ld /var/lib/acornfox/catalog

排查顺序：

1. sudo acornfox doctor

处置：

- ［2 级 先问用户］清理磁盘

- 必须先停下问用户：清理磁盘之前
- 确认恢复：catalog sync 成功
- 相关编号：SETUP-DISK-01

### CAT-SYNC-08 所有目录来源都失败，继续用手上的目录

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：看 attempts 里每个来源的编号逐个处理；不影响已装的应用，安装时用手上的目录
- AcornFox 已经自动做的：继续用手上的目录（缓存或内置）
- 现象：catalog sync 返回 CAT-SYNC-08，带 attempts；doctor 的应用目录一项警告（SETUP-SYNC-01）

可能原因（逐条判定）：

1. 见 attempts 里每个来源的编号 —— 判定：acornfox catalog sync --json

排查顺序：

1. acornfox catalog sync --json，按 attempts 里的编号查

处置：

- ［1 级 可以直接做］按每个来源的编号处理

- 必须先停下问用户：要改网络设置时
- 确认恢复：catalog sync 成功
- 相关编号：CAT-SYNC-02、SETUP-SYNC-01

### CAT-SYNC-09 另一次同步占着目录缓存

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：等一会儿再同步；不要并行跑多个 catalog sync
- 现象：catalog sync 返回 CAT-SYNC-09

可能原因（逐条判定）：

1. 定时同步和手动同步撞上了，或上一次还没结束 —— 判定：过一分钟再试能过

排查顺序：

1. 等一分钟

处置：

- ［1 级 可以直接做］稍后再同步

- 必须先停下问用户：—（不用问）
- 确认恢复：catalog sync 成功

### CAT-SYNC-10 向下载网关取私有目录失败

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：手上的私有目录照旧能用；看 doctor 的下载网关和时钟两项，稍后再同步
- AcornFox 已经自动做的：手上的私有目录缓存保留
- 现象：catalog sync 的 private 部分是 CAT-SYNC-10

可能原因（逐条判定）：

1. 连不上下载网关［doctor 自动判定］ —— 判定：doctor 的下载网关一项（SETUP-GW-01）
2. 本机时钟不准，网关拒绝［doctor 自动判定］ —— 判定：doctor 的时钟一项（SETUP-CLOCK-01）

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］稍后再同步

- 必须先停下问用户：改系统设置时
- 确认恢复：catalog sources 里私有目录的上次同步时间更新了
- 相关编号：SETUP-GW-01、SETUP-CLOCK-01

### CAT-SYNC-11 私有目录已断开（授权被撤销或目录停用）

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：已装好的应用照常运行，但不能再装、不能升级；告诉用户联系给码的人
- AcornFox 已经自动做的：这个私有目录的缓存已删掉
- 现象：catalog sources 里这个目录标已断开，code 是 CAT-SYNC-11

可能原因（逐条判定）：

1. 给码的人撤销了授权或停用了目录 —— 判定：catalog sources 的 disconnected_at

排查顺序：

1. acornfox catalog sources

处置：

- ［0 级 只读］转述给用户

- 必须先停下问用户：要新连接码时
- 确认恢复：—
- 相关编号：CAT-INST-25

### CAT-SYNC-12 签名的索引写明属于别的目录

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：没有用它。公共同步时自动换下一个来源；私有目录持续出现就停下反馈
- AcornFox 已经自动做的：拒绝用它
- 现象：attempts 或 catalog sources 里是 CAT-SYNC-12

可能原因（逐条判定）：

1. 来源把一个目录的索引当成另一个目录的发了（配置错或被冒充） —— 判定：catalog sync --json 看是哪个来源

排查顺序：

1. acornfox catalog sync --json

处置：

- ［0 级 只读］继续用手上的目录

- 必须先停下问用户：持续出现时带上证据包反馈
- 确认恢复：catalog sync 成功

### CAT-LOAD-01 目录缓存和程序内置的目录都用不了

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：应用中心打不开。先 acornfox catalog sync；还不行就重装同一版本的 AcornFox（保留数据）
- 现象：应用中心列表、安装都报目录读不了

可能原因（逐条判定）：

1. 缓存坏了，而且程序文件也坏了 —— 判定：catalog sync 后仍不行
2. 程序内置的目录签名公钥和目录对不上（程序有问题） —— 判定：同版本别的机器也一样

排查顺序：

1. acornfox catalog sync
2. acornfox --json version

处置：

- ［1 级 可以直接做］acornfox catalog sync
- ［2 级 先问用户］重装同一版本

- 必须先停下问用户：重装之前；带上证据包
- 确认恢复：acornfox catalog list 正常

### CAT-SHOW-01 展示内容（show.json）没有签名，或签名验不过

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：应用中心退回用清单里的名字和一句话，装应用不受影响；下一次发布会带新的。持续出现就告诉维护者
- AcornFox 已经自动做的：没有用这一份展示内容，没写进缓存；目录本身照常同步
- 现象：doctor 的“展示内容”一项警告 CAT-SHOW-01；acornfox catalog show 应用 提示展示内容没法用；应用中心里应用的一句话和介绍是清单里的原话

可能原因（逐条判定）：

1. 发布时没带 show.json.sig，或它被换掉、损坏 —— 判定：catalog sync --json 的 show.code 是 CAT-SHOW-01；换 ACORNFOX_CATALOG_SOURCES 指到另一个来源后正常
2. 发布时签展示内容用的钥匙，这个版本的程序不认识 —— 判定：升级 AcornFox 后正常；catalog verify 发布目录 --pubkey 公钥 报 CAT-SHOW-01

排查顺序：

1. acornfox --json catalog sync
2. sudo acornfox doctor 看“展示内容”那一行
3. acornfox catalog show 应用

处置：

- ［1 级 可以直接做］acornfox catalog sync（只读取目录，可重复）
- ［2 级 先问用户］绕过签名——绝不

- 必须先停下问用户：持续出现时：告诉维护者，带上 catalog sync --json 的输出
- 确认恢复：acornfox catalog sync 之后 doctor 的展示内容一项通过
- 相关编号：CAT-SYNC-04、CAT-SHOW-02、CAT-SHOW-03

### CAT-SHOW-02 展示内容的序号和目录的序号对不上

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：这一份展示内容没有用，应用中心退回用清单里的名字和一句话；下一次发布会对上
- AcornFox 已经自动做的：没有用这一份展示内容，没写进缓存
- 现象：doctor 的“展示内容”一项警告 CAT-SHOW-02

可能原因（逐条判定）：

1. 来源正在发布，show.json 和 index.json 是两次发布的文件 —— 判定：几分钟后 acornfox catalog sync 再看
2. 镜像来源只更新了其中一个文件 —— 判定：catalog sources 对照各来源的 version.txt 和 show.json 里的 serial

排查顺序：

1. 等几分钟后 acornfox catalog sync
2. acornfox --json catalog sync 看 show

处置：

- ［1 级 可以直接做］稍后再同步

- 必须先停下问用户：持续出现时：告诉维护者发布流程把两个文件分开发了
- 确认恢复：acornfox catalog sync 之后 doctor 的展示内容一项通过
- 相关编号：CAT-SYNC-05、CAT-SHOW-01

### CAT-SHOW-03 展示内容的格式不对

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：这一份展示内容没有用，应用中心退回用清单里的名字和一句话；告诉维护者用 acornfox catalog verify 查发布目录
- AcornFox 已经自动做的：没有用这一份展示内容，没写进缓存
- 现象：doctor 的“展示内容”一项警告 CAT-SHOW-03

可能原因（逐条判定）：

1. 不是 schema 1 的 catalog-show 文档，或缺字段 —— 判定：catalog verify 发布目录 报 CAT-SHOW-03，message 写了是哪一处
2. 某个应用或解决方案的介绍超过 64 KB，或编号写了两次 —— 判定：catalog verify 发布目录的 message
3. 这个版本的程序比发布的格式旧，认不了新格式 —— 判定：message 里 schema 大于 1；升级 AcornFox 后正常

排查顺序：

1. acornfox --json catalog sync
2. 维护者：acornfox catalog verify 发布目录

处置：

- ［0 级 只读］不用处理

- 必须先停下问用户：持续出现时：告诉维护者，带上 message
- 确认恢复：acornfox catalog sync 之后 doctor 的展示内容一项通过
- 相关编号：CAT-SHOW-01

### CAT-SHOW-04 取展示内容（show.json）失败

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：目录已经同步成功，只是展示内容没取到；下次同步会再取一次
- AcornFox 已经自动做的：目录照常同步；这一份展示内容没有用，下次同步再试
- 现象：doctor 的“展示内容”一项警告 CAT-SHOW-04

可能原因（逐条判定）：

1. 取 show.json 时网络断了、超时，或来源返回了 200 和 404 以外的状态 —— 判定：curl -sS -m 10 -o /dev/null -w '%{http_code}' https://acornfox.com/catalog/show.json
2. 文件比 8 MB 的上限还大 —— 判定：curl 下来看大小

排查顺序：

1. acornfox catalog sync
2. curl 目录来源的 show.json

处置：

- ［1 级 可以直接做］稍后再同步

- 必须先停下问用户：一直这样时：带上 catalog sync --json 的输出
- 确认恢复：acornfox catalog sync 之后 doctor 的展示内容一项通过
- 相关编号：CAT-SYNC-02

### CAT-SKILL-01 应用或解决方案的操作手册没取到

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：这份手册暂时没有，应用照常装，AI 工作台照通用 Skill 做；下次同步目录会再取一次
- AcornFox 已经自动做的：目录和展示内容照常同步；没取到的手册没有用，之后每次同步都会补取
- 现象：doctor 的“操作手册”一项警告 CAT-SKILL-01；acornfox catalog skill 应用 说有手册但这台机器上用不了；catalog sync --json 的 skills.failed 里有它

可能原因（逐条判定）：

1. 取手册时网络断了、超时，或来源返回了 200 以外的状态 —— 判定：catalog sync --json 的 skills.failed[].message 写了哪一份、什么原因；curl -sS -m 10 -o /dev/null -w '%{http_code}' https://acornfox.com/catalog/skills/app-应用编号.md
2. 来源上没有这个文件（HTTP 404）：发布时只发了展示内容、没发 skills/ 目录 —— 判定：同上，返回 404；告诉维护者用 acornfox catalog verify 发布目录 查
3. 本地缓存里的文件被删了 —— 判定：acornfox catalog sync 之后恢复

排查顺序：

1. acornfox --json catalog sync 看 skills
2. acornfox --json catalog skill 应用编号 看 code 和 note

处置：

- ［1 级 可以直接做］acornfox catalog sync（只读取目录，可重复）

- 必须先停下问用户：一直这样时：告诉维护者，带上 catalog sync --json 的输出
- 确认恢复：acornfox catalog sync 之后 doctor 的操作手册一项通过，acornfox catalog skill 应用编号 能取到全文
- 相关编号：CAT-SKILL-02、CAT-SHOW-04、CAT-SYNC-02

### CAT-SKILL-02 操作手册的大小或 sha256 和展示内容里写的对不上

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：这份手册已丢弃、没有用，应用照常装，AI 工作台照通用 Skill 做；下次同步会再取；持续出现告诉维护者
- AcornFox 已经自动做的：没写进缓存，不会被使用；之后每次同步都会再试
- 现象：doctor 的“操作手册”一项警告 CAT-SKILL-02；catalog sync --json 的 skills.failed 里有它

可能原因（逐条判定）：

1. 来源正在发布，展示内容和手册文件是两次发布的 —— 判定：几分钟后 acornfox catalog sync 再看
2. 手册文件在传输中被改过或截断（代理、镜像） —— 判定：换 ACORNFOX_CATALOG_SOURCES 指到另一个来源后正常
3. 发布目录里的手册和 show.json 登记的不一致，或手册不是文本 —— 判定：维护者：acornfox catalog verify 发布目录 报 CAT-SKILL-02

排查顺序：

1. 等几分钟后 acornfox catalog sync
2. acornfox --json catalog sync 看 skills.failed

处置：

- ［1 级 可以直接做］稍后再同步
- ［2 级 先问用户］绕过校验使用这份手册——绝不

- 必须先停下问用户：持续出现时：告诉维护者，带上 catalog sync --json 的输出
- 确认恢复：acornfox catalog sync 之后 doctor 的操作手册一项通过
- 相关编号：CAT-SKILL-01、CAT-SHOW-02

### CAT-SKILL-03 操作手册超过 32 KB 上限

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：不用处理：这份手册没有用，应用照常装；告诉维护者把它缩短到 32 KB 以内
- AcornFox 已经自动做的：没下载、不使用
- 现象：doctor 的“操作手册”一项警告 CAT-SKILL-03

可能原因（逐条判定）：

1. 展示内容里登记的手册大小超过 32 KB，或取到的文件比它大 —— 判定：catalog sync --json 的 skills.failed[].message；维护者：acornfox catalog verify 发布目录

排查顺序：

1. acornfox --json catalog sync 看 skills.failed

处置：

- ［0 级 只读］不用处理

- 必须先停下问用户：持续出现时：告诉维护者哪一份超长
- 确认恢复：维护者重新发布后，acornfox catalog sync 再看 doctor 的操作手册一项
- 相关编号：CAT-SKILL-02

### CAT-PKG-01 没有来源给出和目录相符的应用包

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：稍后 acornfox catalog sync 后重装；还不行就反馈
- AcornFox 已经自动做的：依次试过每个来源
- 现象：安装时取应用包失败（CAT-INST-19 的原因里是 CAT-PKG-01）

可能原因（逐条判定）：

1. 来源连不上 —— 判定：同 CAT-SYNC-02 的判定
2. 来源上的应用包和签名的索引对不上（正在发布） —— 判定：稍后再试能过

排查顺序：

1. acornfox catalog sync
2. 重装

处置：

- ［1 级 可以直接做］稍后重装

- 必须先停下问用户：持续出现时反馈
- 确认恢复：重装成功
- 相关编号：CAT-INST-19

### CAT-PKG-02 下载的应用包校验值或大小和目录不符

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：没有用它；重装一次，持续出现就停下反馈（可能被篡改）
- AcornFox 已经自动做的：删掉了下载的包，换下一个来源
- 现象：安装时报应用包校验不符

可能原因（逐条判定）：

1. 下载出错或来源上的文件被替换 —— 判定：重装一次后是否还出现

排查顺序：

1. 重装一次

处置：

- ［1 级 可以直接做］重装
- ［2 级 先问用户］绕过校验——绝不

- 必须先停下问用户：持续出现时
- 确认恢复：重装成功

### CAT-PKG-03 下载的应用包存不下来

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：检查数据目录所在磁盘和权限（acornfox doctor）后重装
- 现象：安装时报应用包写不进本地

可能原因（逐条判定）：

1. 磁盘满或权限不对［doctor 自动判定］ —— 判定：doctor 的磁盘一项

排查顺序：

1. sudo acornfox doctor

处置：

- ［2 级 先问用户］清理磁盘

- 必须先停下问用户：清理磁盘之前
- 确认恢复：重装成功
- 相关编号：SETUP-DISK-01

### CAT-PKG-04 索引里的应用包路径不合规

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG、CAT-SHOW、CAT-SKILL）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：目录本身有问题，不是操作不对；过一阵 acornfox catalog sync 后重试，还不行就反馈
- 现象：安装时报应用包路径不对

可能原因（逐条判定）：

1. 目录里的路径不是 apps/<id>/<版本>.tar.gz —— 判定：acornfox catalog info 应用 --json

排查顺序：

1. acornfox catalog sync

处置：

- ［0 级 只读］等目录修正

- 必须先停下问用户：反馈时带上证据包
- 确认恢复：重装成功

## 打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）

### CAT-LINT-01 清单文件缺失、读不了或不是合法的 YAML

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：acornfox catalog lint 报 CAT-LINT-01

可能原因（逐条判定）：

1. 文件名写错、缩进或引号不对 —— 判定：lint 输出的文件和行

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］改文件后再 lint

- 必须先停下问用户：—（维护目录的人自己改）
- 确认恢复：lint 没有错误

### CAT-LINT-02 清单不符合格式定义

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-02，写了是哪个字段

可能原因（逐条判定）：

1. 字段名、类型或取值不在格式定义里 —— 判定：lint 输出的字段路径
2. 清单还是旧格式（schema 写的是 2 或更早）：第 3 版删掉了旧写法（大模型设置项类型 llm_provider、provides 的 adapter 和 base_path、uses 的 for_param） —— 判定：清单的 schema；lint 输出的路径是 /schema

排查顺序：

1. 对照格式定义改那个字段；旧格式的清单按第 3 版重写（需要别的应用的东西写成 uses，提供给别的应用的写成 provides 的 values 和步骤）

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-03 清单的 id 和所在目录名不一致

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-03

可能原因（逐条判定）：

1. 改了 id 没改目录名，或反过来 —— 判定：比较 apps/<目录>/ 和清单里的 id

排查顺序：

1. 统一 id 和目录名

处置：

- ［1 级 可以直接做］改名（已发布过的 id 不能改）

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-04 清单写的图标文件不存在

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-04

可能原因（逐条判定）：

1. 图标文件没放进应用目录或名字写错 —— 判定：ls 应用目录

排查顺序：

1. 放好图标或改名字

处置：

- ［1 级 可以直接做］补图标

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-05 GPL/AGPL 的应用没写源码地址

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-05

可能原因（逐条判定）：

1. 许可证是 GPL 或 AGPL，但缺 app.source_code —— 判定：清单的 license 和 app.source_code

排查顺序：

1. 补上游源码地址

处置：

- ［1 级 可以直接做］补 app.source_code

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-06 引用了不存在的参数、数据卷、端点或依赖

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：平台把配置文件模板里的每个 {{…}} 都当引用去取值，取不到就停在安装那一步，所以不是引用的 {{…}} 也算错：现在没有“原样保留 {{…}}”的写法。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-06，写了是哪个引用

可能原因（逐条判定）：

1. {{params.*}}、volumes、endpoints、uses 名字写错或漏声明 —— 判定：lint 输出的引用
2. 配置文件模板里的 {{…}} 不是引用：大小写不对（{{Params.x}}）、少了种类（{{port}}）、没有这种引用，或者是应用自己的模板语法 —— 判定：lint 输出写了是哪个 {{…}}、这里能引用哪些
3. first_login 的 from 没写成 种类.名字，或把 volumes.* 当值引用了（它只能写在 uninstall.data 里） —— 判定：lint 输出的引用
4. uses 里有两条用了同一个 name —— 判定：lint 输出写了是哪个名字；引用按前一条算

排查顺序：

1. 补声明或改名字

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-07 隐藏参数没有 generate，没人能填

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-07

可能原因（逐条判定）：

1. 参数标了 hidden 却没写怎么生成 —— 判定：那个参数的定义

排查顺序：

1. 加 generate 或去掉 hidden

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-08 主端点不对：没有唯一一个带健康检查的主端点，或服务的步骤指向了别的入口

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：平台从这台机器上连应用，只能经应用映射出来的那一个端口，也就是主端点（primary: true）。所以 provides 写了 issue、revoke 或 check 步骤时，它的 endpoint 只能是主端点或者不写；只给值、没有步骤的 provides 可以指向别的入口（别的应用经应用网络能连到任何端口）。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-08

可能原因（逐条判定）：

1. 没有主端点、有多个主端点，或主端点没写健康检查 —— 判定：清单的 endpoints
2. provides 有步骤，endpoint 却指向了不是主端点的入口 —— 判定：lint 输出的路径 /provides/…/endpoint

排查顺序：

1. 留一个 primary 并写 health；有步骤的 provides 把 endpoint 改成主端点或去掉

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-09 配方引用的应用或连接不存在或对不上

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：配方的 links 每条写 from、to、uses：to 这个应用清单里名叫 uses 的那项需要，接 from 这个应用提供的同类型服务。配方按 apps 的顺序装，后装的应用接先装的应用登记的服务，所以 from 要排在 to 前面。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-09

可能原因（逐条判定）：

1. 配方里的应用 id 或预填的参数名写错 —— 判定：lint 输出的配方和引用
2. links 的 uses 写成了服务类型名，而不是 to 清单里 uses[].name —— 判定：对照 to 应用清单的 uses
3. from 不提供 to 那项需要的服务类型 —— 判定：from 应用清单的 provides
4. from 排在 to 后面 —— 判定：配方 apps 的顺序

排查顺序：

1. 对照应用清单改配方

处置：

- ［1 级 可以直接做］改配方

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-10 默认对外方式不是只本机（要审核）

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：这是警告：确认这个应用默认对局域网或公网开放是有意的，并写清理由
- 现象：lint 警告 CAT-LINT-10

可能原因（逐条判定）：

1. 清单的默认 exposure 不是 local —— 判定：清单的 exposure

排查顺序：

1. 确认是不是有意的

处置：

- ［1 级 可以直接做］改回 local，或在审核时说明理由

- 必须先停下问用户：上架审核时由审核人确认
- 确认恢复：审核通过

### CAT-LINT-11 交付级别要用户做事，但没写 user_steps

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-11

可能原因（逐条判定）：

1. delivery.tier 表示要用户做几步，却没写是哪几步 —— 判定：清单的 delivery

排查顺序：

1. 补 user_steps

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-12 目录里没有 apps/、recipes/，也没有服务类型定义

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：确认 lint 的目录参数指向目录仓库根目录
- 现象：lint 报 CAT-LINT-12

可能原因（逐条判定）：

1. 目录参数写错 —— 判定：ls 那个目录

排查顺序：

1. ls 目录

处置：

- ［1 级 可以直接做］改参数

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 能找到应用

### CAT-LINT-13 应用目录里有文件不能放进应用包

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 或 build 报 CAT-LINT-13

可能原因（逐条判定）：

1. 文件名不是 ASCII、路径太长，或是符号链接 —— 判定：lint 输出的文件

排查顺序：

1. 改名或换成真文件

处置：

- ［1 级 可以直接做］改文件

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-14 镜像包没写 artifact_sha256（不会经下载网关）

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：这是警告：要经下载网关发镜像包就补 artifact_sha256 和 artifact_size
- 现象：lint 警告 CAT-LINT-14

可能原因（逐条判定）：

1. 清单有 artifact 却没有校验值 —— 判定：清单的镜像部分

排查顺序：

1. 算镜像包的 sha256 和大小

处置：

- ［1 级 可以直接做］补上（新版本的应用包，已发布的不改）

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有这条警告

### CAT-LINT-15 给用户看的链接不是 http(s) 地址

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-15

可能原因（逐条判定）：

1. homepage、source_code、delivery.video 写错 —— 判定：lint 输出的字段

排查顺序：

1. 改成 https:// 开头的地址

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-16 引用了不存在的平台变量

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：acornfox catalog platform-vars 看有哪些；按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-16

可能原因（逐条判定）：

1. {{platform.*}} 名字写错 —— 判定：lint 输出的引用
2. 把别的应用给的东西（大模型接口、邮箱）当成了平台变量：它们是服务，要写成 uses，引用 uses.<名>.<项> —— 判定：引用的名字是不是 platform.llm.* 这类

排查顺序：

1. acornfox catalog platform-vars

处置：

- ［1 级 可以直接做］改引用；平台没有的变量登记进 gaps.yaml

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-19

### CAT-LINT-17 声明的最低平台版本比用到的写法要求的低

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：把 min_platform 改成 lint 算出的版本（或删掉让它自动算）
- 现象：lint 报 CAT-LINT-17，写了要的版本

可能原因（逐条判定）：

1. 清单或配方用了新写法，min_platform 没跟着改 —— 判定：lint 输出的需要版本

排查顺序：

1. 改 min_platform

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-INST-29

### CAT-LINT-18 交付级别是 auto，但有参数要用户填

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-18

可能原因（逐条判定）：

1. delivery.tier 是 auto，却有必填、没有默认值的参数 —— 判定：acornfox catalog vars 应用目录 看哪些要用户填

排查顺序：

1. acornfox catalog vars 应用目录

处置：

- ［1 级 可以直接做］补默认值或平台变量，或把交付级别改成要用户做一步

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-19 缺变量登记表 gaps.yaml 读不了或格式不对

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出改 gaps.yaml，再运行 acornfox catalog gaps
- 现象：acornfox catalog gaps 报 CAT-LINT-19

可能原因（逐条判定）：

1. YAML 写错或字段不对 —— 判定：输出的行

排查顺序：

1. acornfox catalog gaps 目录

处置：

- ［1 级 可以直接做］改 gaps.yaml

- 必须先停下问用户：—（不用问）
- 确认恢复：catalog gaps 没有错误

### CAT-LINT-20 按架构写的镜像包互相矛盾

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-20

可能原因（逐条判定）：

1. 同一份清单里旧写法（artifact_sha256、artifact_size）和按架构的写法都写了 —— 判定：清单里两种写法是否同时出现
2. 给 resources.platforms 里没有的架构写了镜像包 —— 判定：对照 resources.platforms
3. 一个镜像包同时写给了两个架构 —— 判定：看各架构的网关文件编号和校验值是否重复

排查顺序：

1. acornfox catalog lint 应用目录
2. 按输出只留一种写法，架构名和 resources.platforms 对上

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-21、CAT-INST-26

### CAT-LINT-21 按架构写了镜像包，但有的架构没有（提醒）

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：要在那个架构上走下载网关，就补上它的镜像包；不补的话那个架构从公共镜像源取，私有目录的应用在那个架构上装不了
- 现象：lint 报 CAT-LINT-21（提醒，不挡打包）

可能原因（逐条判定）：

1. resources.platforms 里写了某个架构，但 artifacts 里没有它的镜像包 —— 判定：对照 resources.platforms 和 artifacts

排查顺序：

1. acornfox catalog lint 应用目录

处置：

- ［1 级 可以直接做］补上缺的架构的镜像包
- ［1 级 可以直接做］接受这个架构走公共源（公共目录的应用）

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 不再提醒，或确认这个架构走公共源
- 相关编号：CAT-LINT-20、CAT-INST-26

### CAT-LINT-22 清单里写的服务类型没有定义

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：清单的 uses（需要什么服务）、provides（提供什么服务）里的 kind 是服务类型的名字，要在目录的 service-types/ 下有同名的定义文件。目录里没有 service-types/ 就是一种类型都没有定义，写了 uses、provides 的清单都报这条。
- 下一步：类型名写错了就改成 service-types/ 下已有的名字；确实是新类型，先加 service-types/<类型>.yaml，再运行 acornfox catalog lint
- 现象：lint 报 CAT-LINT-22，写了是哪份清单的哪一条 uses 或 provides

可能原因（逐条判定）：

1. kind 的名字拼错了 —— 判定：lint 输出里“现有”后面列了目录认识的全部类型，对照着看
2. 新类型还没有加定义文件 —— 判定：ls 目录的 service-types/，没有 <类型>.yaml
3. 目录没有 service-types/ 这个目录 —— 判定：ls 目录根下没有 service-types；lint 输出里“现有”后面写“一个都没有”

排查顺序：

1. acornfox catalog lint 目录
2. ls 目录/service-types

处置：

- ［1 级 可以直接做］改清单里的 kind
- ［1 级 可以直接做］加服务类型定义文件

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有这条错误
- 相关编号：CAT-LINT-25、CAT-LINT-23

### CAT-LINT-23 清单写的项和服务类型定义对不上

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：服务类型定义（service-types/<类型>.yaml 的 fields）写明这种服务有哪几项、哪几项必须有。清单提供这种服务时只能给这几项的值，必须有的项都要写怎么来；清单引用别人的服务时（uses.<名>.<项>）只能引用这几项。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-23，写了是哪一项、这种类型有哪几项

可能原因（逐条判定）：

1. provides 的 values 里写了类型定义里没有的项 —— 判定：对照 service-types/<类型>.yaml 的 fields
2. 类型定义里必须有的项（required: true），values 里没写怎么来，或写成了空的 —— 判定：lint 输出列了缺哪几项
3. env 的 from、配置文件模板里写的 uses.<名>.<项>，项名不在类型定义里 —— 判定：lint 输出的引用和“有：”后面的项名

排查顺序：

1. acornfox catalog lint 目录
2. 看 service-types/<类型>.yaml 的 fields

处置：

- ［1 级 可以直接做］改清单：项名改对，给必须有的项写来路（模板、issued 或 ask）
- ［1 级 可以直接做］这种服务确实该多一项时改服务类型定义

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-22、CAT-LINT-25

### CAT-LINT-24 步骤或服务的值里的引用取不到值

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：步骤里有四种临时值：saved.<键> 是这一组前面的步骤 save 下来的；issued.<键> 只在 provides 的 revoke、check 里有，是当初 issue 步骤 save 下来的，另有平台自动存的 issued.issue_id（那次发凭据的随机编号）；consumer.name、consumer.id 只在 provides 的 issue、revoke、check 里有；issue.id 只在 provides 的 issue 里有，是这次发凭据的随机编号（8 位十六进制），用来给应用里建的东西起一个不会重复的名字。self.internal_url、self.host、self.port 只能写在 provides 的 values 里，而且那一条要写 endpoint。values 里写 { issued: 键 } 时，那个键要是 issue 步骤 save 过的。平台把步骤（path、headers、json、expect_json、save 的 where）和 values 模板里的每个 {{…}} 都当引用去取值，取不到这一步就失败：issue 的步骤要等第一个使用方来装才执行，revoke 的要等卸载才执行，写错了当时看不出来，所以不是引用的 {{…}} 在这里也算错。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-24，写了是哪一步、引用的是什么、现在存下了哪些键或这里能引用哪些

可能原因（逐条判定）：

1. {{…}} 不是引用：大小写不对（{{saved.Session}}）、少了种类（{{session}}）、种类写错（{{save.session}}） —— 判定：lint 输出写了是哪个 {{…}}、这里能引用哪些
2. 引用的 saved.<键> 前面的步骤没有 save 过（键名拼错，或 save 写在了后面的步骤、另一组步骤里） —— 判定：lint 输出末尾“已存下的键”
3. revoke、check 里引用的 issued.<键>，或 values 里 issued 指的键，issue 的步骤里没有 save 过 —— 判定：看那条 provides 的 issue 各步的 save
4. 把只在某处有的值写到了别处：init 里写 consumer.*、issue 里写 issued.*、values 以外写 self.*、环境变量或配置文件里写 saved.*、issue 以外写 issue.id（revoke、check 里要写 issued.issue_id） —— 判定：lint 输出写明了这里能引用哪些
5. issue 的步骤 save 了一个叫 issue_id 的键：这个键名留给平台存发凭据的随机编号 —— 判定：看那条 provides 的 issue 各步的 save
6. 写了 issue.id 以外的 issue.*（只有 issue.id） —— 判定：lint 输出写了是哪个引用
7. values 里用了 self.*，但那条 provides 没写 endpoint —— 判定：那条 provides 有没有 endpoint
8. 在 provides 的步骤或 values 里引用了 uses.*，或把 volumes.* 当值引用了 —— 判定：lint 输出写明了这里能引用哪些

排查顺序：

1. acornfox catalog lint 目录
2. 对照清单里那一组步骤的 save

处置：

- ［1 级 可以直接做］改清单：引用写成 种类.名字（全小写）、键名改对、补上 save，或把引用挪到有这个值的地方

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-25 服务类型定义文件有问题

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：目录的 service-types/ 下一种服务类型一个文件 <类型>.yaml，格式定义是 schema/service-type.schema.json。有问题的定义不进 index.json，目录也打不了包。
- 下一步：按输出里的文件和字段改 service-types/ 下的定义文件，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-25，文件一栏是 service-types/<类型>.yaml

可能原因（逐条判定）：

1. 不是合法的 YAML，或不符合格式定义（缺必填字段、有不认识的字段、test 里 tcp 和 http 都写了） —— 判定：lint 输出的字段路径和原因
2. kind 和文件名不一样 —— 判定：文件名去掉 .yaml 要等于 kind
3. fields 里有重复的 key；type 是 enum 却没写 options，或不是 enum 却写了 options —— 判定：看 fields
4. default 的类型和 type 对不上、不在 options 里、type 是 url 却不是 http(s) 网址，或给保密项写了默认值 —— 判定：看那一项的 default
5. options 里同一个值写了两次 —— 判定：看那一项的 options
6. test 里引用的 {{项}} 在 fields 里没有 —— 判定：对照 test 和 fields
7. service-types/ 下放了不是 .yaml 的文件（例如写成了 .yml） —— 判定：ls 目录/service-types

排查顺序：

1. acornfox catalog lint 目录
2. 对照 schema/service-type.schema.json

处置：

- ［1 级 可以直接做］改定义文件

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-22、CAT-LINT-23

### CAT-LINT-26 清单用了现在的平台还不执行的写法（提醒）

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：格式先定下、平台后执行的写法，清单可以写，但这一版的平台还不执行：用了它们的清单安装前就被拒绝（CAT-INST-23），不会装到一半才失败，也不会当成没写照装。现在没有这样的写法：在容器里执行命令的步骤（exec）、发凭据前先定好的值（issue_vars）、values 模板里引用发给使用方的值（issued.<键>）、不说 HTTP 的入口（protocol: tcp）、health 的 exec 和 tcp、只在内部网络的访问方式（exposure.default: internal）、限制同时接几个使用方（max_consumers）、连数据一起删时删掉使用方数据的步骤（purge）、核对不过时停下（on_check_fail: stop）、就绪检查（ready）、只接没有别的使用方在用的服务（uses 的 dedicated）从 0.6.0 起都已执行，不再提醒。以后格式先于平台加新写法时，这里会写明是哪些。
- 下一步：这是提醒，不挡打包：这样的应用现在装不上，先不要上架；要现在就能装，改成现在的平台能执行的写法
- 现象：lint 提醒 CAT-LINT-26（提醒，不挡打包），写了是清单的哪一处、是哪一种写法

可能原因（逐条判定）：

1. 清单用了格式已经认、这一版平台还不执行的写法（现在没有；以后有时看 lint 输出写的是哪一种） —— 判定：lint 输出的路径和说明

排查顺序：

1. acornfox catalog lint 目录
2. acornfox catalog vars 应用目录 --json 看 min_platform 的 computed 和 reasons

处置：

- ［1 级 可以直接做］改成现在的平台能执行的写法
- ［1 级 可以直接做］留着，等平台支持后再上架

- 必须先停下问用户：要把这样的应用上架之前
- 确认恢复：lint 不再提醒，或确认这个应用暂不上架
- 相关编号：CAT-INST-23

### CAT-LINT-27 可选的服务没写“没有时会怎样”

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：uses 里标了 optional: true 的服务，要用 without 写一句给用户看的话，说明没有这项服务时应用会怎样（例如“能用，但发不了通知邮件”）。安装时缺这项服务，平台把这句话原样给用户看。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-27，写了是哪一条 uses

可能原因（逐条判定）：

1. uses 写了 optional: true，没写 without —— 判定：清单的 uses

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］给那条 uses 补 without（一句话，不超过 80 字）
- ［1 级 可以直接做］这项服务其实必须有时去掉 optional

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-28

### CAT-LINT-28 可选的服务的项用在了不能没有值的地方

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：uses 里标了 optional: true 的服务，安装时可以没接上。它的项只能用在环境变量里，而且那条环境变量（from: uses.<名>.<项>）要同样写 optional: true：没接上时这一条整条不写。不写的话，这项服务没接上时这条环境变量取不到值，安装前就报错，等于把“可选”变成了“必须”。配置文件模板、init 步骤、first_login 里现在没有“没有值时怎么办”的写法，所以不能引用可选服务的项。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-28，写了是哪一条环境变量、哪个配置文件、哪一步，取自哪条 uses

可能原因（逐条判定）：

1. env 的 from 引用了可选的 uses，这一条 env 没写 optional: true —— 判定：清单的 env 和 uses
2. 配置文件模板、init 步骤或 first_login 里引用了可选的 uses 的项 —— 判定：lint 输出的路径（/files/…、/init/…、/first_login/…）
3. 这项服务其实必须有，uses 却标了 optional: true —— 判定：没有这项服务时应用还能不能用

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］给那条 env 加 optional: true
- ［1 级 可以直接做］配置文件、init 步骤、first_login 里的引用改成经 optional 的环境变量传给应用
- ［1 级 可以直接做］这项服务必须有时去掉 uses 的 optional（连同 without）

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-27

### CAT-LINT-29 retry 写在了不是 GET 的步骤上

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：http 步骤可以写 retry: { times, delay_s }：这一步的判断不过（状态码、expect_json）或要存的值还取不到时，等 delay_s 秒再发，最多 times 次。用途是应用刚建完东西、要过一会儿才查得到。只有 GET 能这样重发：POST、PUT、PATCH、DELETE 再发一遍可能把东西再建一次或再删一次。连接没建立时的重发是平台自己的规则，和 retry 无关。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-29，写了是哪一步、它的 method

可能原因（逐条判定）：

1. 在建、改、删东西的那一步上写了 retry —— 判定：lint 输出的 method
2. init 步骤没写 method（按 POST 算）却写了 retry —— 判定：那一步有没有 method

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］去掉那一步的 retry，在它后面加一步 GET 去查刚建的东西，把 retry 写在 GET 那一步上

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-24

### CAT-LINT-30 finish_if 写在了不是 revoke、purge 的步骤上

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：步骤可以写 finish_if（http 步骤写 { status: [404], json: {…} }，exec 步骤写 { exit: [退出码] }）：对上时，这一组步骤到此结束并算成功，后面的步骤不做。它只为“要收回或删掉的东西已经没了”这一种情况：收回一份已经不存在的凭据、删一个已经没了的库不算出错，所以只能写在 provides 的 revoke 和 purge 里。写在 init、issue、check、ready 的步骤上，会让一组步骤没做完就算成功（例如凭据没发出来却当成发好了），所以检查工具拦下。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-30，写了是哪一组的哪一步

可能原因（逐条判定）：

1. 在 init、issue、check 或 ready 的步骤上写了 finish_if —— 判定：lint 输出的路径（/init/…、/provides/…/issue/…、/provides/…/check/…、/provides/…/ready/steps/…）

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］去掉那一步的 finish_if；要容忍某种应答，改用 expect_status、expect_json

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-24

### CAT-LINT-31 只在首次安装时写的配置文件引用了服务给的值

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：配置文件写了 overwrite: never（文件还没有时才写一次），模板里却引用了 uses.<名>.<项>。服务给的值以后可能换：接旧数据重装时旧凭据没通过检查会换一份新的，接的服务换了值也会变；这种文件不会跟着改，应用会一直用旧的值。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-31，写了是哪个配置文件、引用的是哪一项

可能原因（逐条判定）：

1. 要跟着服务变的值写进了 overwrite: never 的配置文件 —— 判定：清单的 files 里那个文件的 overwrite 和 template

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］改成 overwrite: install（每次安装按这次的值写；接旧数据重装时内容变了会先另存旧文件），或者经环境变量传给应用

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-28

### CAT-LINT-32 给每个使用方发凭据，却没写怎么收回

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：provides 写了 issue 步骤，平台就会给每个接它的应用在提供方里建一份凭据。使用方连数据一起删、接旧数据重装时换了服务、安装中途失败时，平台按 revoke 步骤把这份凭据收回；没写 revoke，它会一直留在提供方里还能用，只能靠用户手工去删。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-32，写了是哪一条 provides

可能原因（逐条判定）：

1. provides 只写了 issue，漏了 revoke —— 判定：那条 provides 有没有 revoke

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］补上 revoke 的步骤；要收回的东西已经没了时可以写 finish_if 算收回成功

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-30、CAT-INST-35

### CAT-LINT-33 同一个应用的两条 provides 是同一种服务

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：一个应用每种服务只能提供一条。两条 provides 写了同一个服务类型时，平台登记它们、使用方接哪一条都要看它们在清单里的先后；经营系统比对变量报告的指纹，也会因为两条换了顺序就认成变了，比对不出真正的变化。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-33，写了是第几条 provides、哪个服务类型

可能原因（逐条判定）：

1. 同一种服务想按不同入口或不同用途提供两份 —— 判定：清单的 provides，两条的 kind 一样
2. 复制一条 provides 改了一半，忘了改 kind —— 判定：两条的 values、步骤是不是本该是另一种服务

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］合成一条，或去掉多的那条；本该是另一种服务的改对 kind

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-22

### CAT-LINT-34 必须的服务写了“没有时会怎样”（提醒）

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：uses 的 without 是给可选的服务（optional: true）写的：安装时这项服务没有，平台照装并把这句话给用户看。必须的服务没有时安装会停下、让用户先登记一条，这句话永远不会显示给用户，写了等于白写，还容易让看清单的人以为它可有可无。
- 下一步：这是提醒，不挡打包：按应用真实的情况二选一改掉
- 现象：lint 提醒 CAT-LINT-34（提醒，不挡打包），写了是哪一条 uses

可能原因（逐条判定）：

1. 这项服务其实可有可无，漏写了 optional: true —— 判定：没有这项服务时应用还能不能用
2. 这项服务必须有，without 是多写的 —— 判定：同上

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］可有可无：加 optional: true（再按 CAT-LINT-28 检查它的项用在哪）
- ［1 级 可以直接做］必须有：删掉 without

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 不再提醒
- 相关编号：CAT-LINT-27、CAT-LINT-28

### CAT-LINT-35 exec 步骤把保密值写进了命令参数

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：exec 步骤的 command 和 user 会成为容器里那个进程的参数，这台机器上的任何用户都能在进程列表里看到（容器里的进程在宿主机上也看得到）。密码这类保密值（保密的参数、保密的平台变量、服务里保密的项、步骤 save 时标了 secret 的值、发给使用方的保密值（values 里 { issued: 键 } 指到的项标了 secret，或服务类型里那一项是保密的，这个键就算保密，不管 save 时标没标）、issue_vars 里生成的值）只能经 env（只给这一条命令的环境变量）或 stdin（从标准输入送给命令）传。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-35，写了是哪一步的 command 第几项（或 user）、引用的是哪个值

可能原因（逐条判定）：

1. 建账号、改密码的命令把密码直接写进了参数（例如命令的 -c 后面带着整条语句） —— 判定：lint 输出的路径（…/exec/command/序号）和引用
2. 连接用的密码写成了命令行参数 —— 判定：那个命令能不能从环境变量读密码

排查顺序：

1. acornfox catalog lint 目录
2. 查那个命令的文档：密码能不能从环境变量或标准输入读

处置：

- ［1 级 可以直接做］把带保密值的语句挪进 stdin；命令要的密码放进这一步的 env

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-36、CAT-LINT-40

### CAT-LINT-36 命令参数里有换行

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：exec 步骤的 command 和健康检查的 exec 都是参数数组，不经过命令行解释：一项就是一个参数。一项里有换行时，换行会原样进参数，通常是把几条语句、几行内容错写进了一个参数。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-36，写了是哪一项

可能原因（逐条判定）：

1. 几条语句（例如几条 SQL）写进了 command 的一项 —— 判定：lint 输出的路径
2. YAML 里用 | 写成了多行 —— 判定：清单里那一项的写法

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］多行的内容写进 stdin，从标准输入送给命令；command 每一项只写一个参数

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-35

### CAT-LINT-37 issue_vars 写得用不上或会撞名

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：provides 的 issue_vars 是每次发凭据前由平台先定好的值（模板，或平台随机生成的保密值），issue 的步骤里写 {{issue.<键>}}，发成功后和 save 下来的值一样存进凭据（issued.<键>）。所以：没有 issue 步骤时它永远不会生成；键名不能是 id（{{issue.id}} 是平台的随机编号）和 issue_id（平台自动存的随机编号）；也不能和 issue 步骤 save 的键重名，否则 issued.<键> 分不清是哪一个。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-37，写了是哪一条 provides、哪个键

可能原因（逐条判定）：

1. 写了 issue_vars，没写 issue 的步骤 —— 判定：那条 provides 有没有 issue
2. 键名用了 id 或 issue_id —— 判定：lint 输出的路径（/provides/…/issue_vars/键）
3. issue 的某一步 save 了和 issue_vars 同名的键 —— 判定：lint 输出的路径（…/save/键）

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］补上 issue 的步骤或删掉 issue_vars；撞名的换一个键名

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-24

### CAT-LINT-38 入口、健康检查和访问方式对不上

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：入口有两种：http（网页、接口）和 tcp（不说 HTTP，例如数据库、缓存）。健康检查三选一：path 向入口发 GET，只能用在 http 入口上；exec 在容器里执行一条命令，原样执行、不是模板；tcp: true 只看端口通不通。入口代理只转发 HTTP，所以主入口是 tcp 的应用，访问方式只能是 internal（只在内部网络）。values 模板里的 self.internal_url 是 http:// 开头的地址，tcp 入口要用 self.host、self.port 拼。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-38，写了是哪个入口或哪一项

可能原因（逐条判定）：

1. tcp 入口的健康检查写了 path —— 判定：lint 输出的路径（/endpoints/…/health/path）
2. 主入口是 tcp，exposure.default 没写或不是 internal —— 判定：清单的 endpoints 和 exposure
3. provides 的 endpoint 是 tcp 入口，values 模板里用了 self.internal_url —— 判定：lint 输出的路径（/provides/…/values/项）
4. health.exec 里写了 {{…}} —— 判定：lint 输出的路径（/endpoints/…/health/exec/序号）

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］tcp 入口的健康检查改成 tcp: true 或 exec；主入口是 tcp 的写 exposure: { default: internal }；self.internal_url 改成用 self.host、self.port 拼；health.exec 要的密码从容器环境变量读

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-08、CAT-LINT-39

### CAT-LINT-39 写了平台或用户连不到的东西

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：http 步骤（init，provides 的 issue、revoke、check）由平台从这台机器向应用映射出来的主入口端口发请求。只在内部网络的应用（exposure.default: internal）不映射宿主机端口，主入口是 tcp 的应用不说 HTTP，这两种应用的 http 步骤都发不出去，要改成 exec 步骤（在应用自己的容器里执行命令）。只在内部网络的应用也没有页面可打开，所以不能写 first_login（装好后给用户看的登录地址和账号）。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-39，写了是哪一步或 first_login

可能原因（逐条判定）：

1. 只在内部网络或主入口是 tcp 的应用，init 或 provides 里有 http 步骤 —— 判定：lint 输出的路径（…/http）
2. 只在内部网络的应用写了 first_login —— 判定：清单的 exposure 和 first_login

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］http 步骤改成 exec 步骤；first_login 删掉，别的应用要用的账号密码写进 provides 的 values
- ［1 级 可以直接做］这个应用其实有页面要给用户打开：改回 local 访问方式，主入口用 http

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-38

### CAT-LINT-40 服务的一项用了保密值却没标保密

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：provides 的 values 里，模板能拼进保密的值（保密的参数、保密的平台变量、发给使用方的保密值 issued.<键>）。平台只按写明的保密标记处理这一项：清单里写成 { template: …, secret: true }，或服务类型里这一项标了 secret。两处都没标时，这一项会以明文存进服务登记表，在 acornfox services 和页面上显示出来，密码就露给了看的人和对话。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-40，写了是哪一条 provides 的哪一项、引用的是哪个值

可能原因（逐条判定）：

1. 连接串这类拼出来的项带了密码，写成了普通字符串 —— 判定：lint 输出的路径（/provides/…/values/项）
2. 以为模板引用了保密值，平台会自动按保密处理 —— 判定：清单里那一项是不是写成了对象并带 secret: true

排查顺序：

1. acornfox catalog lint 目录
2. 看服务类型定义里这一项有没有标 secret

处置：

- ［1 级 可以直接做］把这一项写成 { template: "…", secret: true }
- ［1 级 可以直接做］这一项本来就不该带保密值：从模板里去掉那个引用

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-35

### CAT-LINT-41 purge 或 on_check_fail 写在了用不上的地方

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：provides 的 purge（使用方连数据一起删时，删掉给它单独建的数据，例如它的库）和 on_check_fail（接旧数据重装时，当初发给这个使用方的凭据核对不过怎么办）说的都是“给每个使用方单独发的东西”，只有写了 issue 步骤的 provides 才有。on_check_fail 还要有 check 步骤：没有 check 就不会核对，它永远用不上。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-41，写了是哪一条 provides 的 purge 或 on_check_fail

可能原因（逐条判定）：

1. 这条 provides 只给值（大家共用一份），没有 issue 步骤，却写了 purge 或 on_check_fail —— 判定：那条 provides 有没有 issue
2. 写了 on_check_fail，没写 check 步骤 —— 判定：那条 provides 有没有 check

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］删掉用不上的 purge 或 on_check_fail
- ［1 级 可以直接做］确实要给每个使用方单独建库：补上 issue、revoke（以及 check）的步骤

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-32、CAT-LINT-37

### CAT-LINT-42 就绪检查的步骤引用了使用方或凭据

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：provides 的 ready 是就绪检查：判断这条服务现在能不能真用（例如还没配好上游）。它在查服务、看计划、装完时随时会跑，不针对哪个使用方，也不带发给使用方的凭据，所以步骤里只能引用 params.<名>、platform.<名>、saved.<键>（前面的步骤存下的），这条 provides 写了 endpoint 时还有 self.*；不能引用 issued.<键>、consumer.*、issue.*。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-42，写了是 ready 的哪一步、引用的是什么

可能原因（逐条判定）：

1. 照着 check 的步骤抄，带上了 {{issued.<键>}} —— 判定：lint 输出的路径（/provides/…/ready/steps/…）
2. 用了 {{consumer.name}} 或 {{issue.id}} 起名字 —— 判定：那一步的模板

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］改用应用自己的管理账号（params 里的）去查；要的值先在前面的步骤里 save 下来，再用 {{saved.<键>}}

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-24、CAT-LINT-43

### CAT-LINT-43 就绪检查的 say 是空的或写了模板

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：provides 的 ready.say 是服务没就绪时原样给用户看的一句话：缺什么、到哪里去补。平台不替换里面的 {{…}}，所以不能写模板（更不能往里放密码这类值），也不能是空的。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-43，写了是哪一条 provides

可能原因（逐条判定）：

1. say 只写了空白 —— 判定：清单里的 ready.say
2. say 里写了 {{…}}，以为会被替换 —— 判定：清单里的 ready.say

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］把 say 写成一句完整的话，例如“还没有能用的上游：到应用的设置页加一个”

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-42

### CAT-LINT-44 健康检查的 expect_stdout 永远对不上

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：健康检查写 exec 时可以加 expect_stdout：命令的标准输出去掉首尾空白后要等于它才算好了（有的服务没准备好时检查命令的退出码也是 0，例如只看连不连得上）。平台原样比较，不替换 {{…}}；它首尾有空白时，和去掉了首尾空白的输出永远对不上，应用就永远“没好”。expect_stdout 只能和 exec 一起写，和 path、tcp 一起写时格式定义就拒绝（CAT-LINT-02）。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-44，写了是哪个入口的健康检查

可能原因（逐条判定）：

1. expect_stdout 首尾带了空格或换行（例如 YAML 里用 | 写成了多行） —— 判定：lint 输出的路径（/endpoints/…/health/expect_stdout）
2. expect_stdout 里写了 {{…}} —— 判定：清单里那一项

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］写成命令正常时输出的那段文字，去掉首尾空白，不写模板

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-38

### CAT-LINT-45 就绪检查里有会改东西的请求

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：provides 的 ready 是就绪检查，查服务、看计划、装完时都会跑，所以步骤要只读：每跑一次就改一次应用里的东西是不行的。http 步骤写 PUT、PATCH、DELETE 时一定会改东西，检查工具拒绝；GET 是查；POST 不拦，因为很多应用要先用 POST 登录才能查。POST 除了登录不要用来建或改东西，exec 步骤做什么也看不出来，这两样由写清单的人保证只读。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-45，写了是 ready 的哪一步、用的什么方法

可能原因（逐条判定）：

1. 照着 issue 或 revoke 的步骤抄，带上了建、改、删的请求 —— 判定：lint 输出的路径（/provides/…/ready/steps/…/http/method）
2. 想在检查时顺手把应用调成能用的样子 —— 判定：那一步要做的事

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］改成 GET 去查；要登录就留一步 POST 登录，存下会话再 GET；要用户去补的事写进 ready.say

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-42、CAT-LINT-43

### CAT-LINT-46 引用的写法读不了

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：清单里的值用 {{种类.名字}} 引用平台的值，后面可以用竖线接变换，例如 {{uses.db.sslmode | map "disable=false" "*=true"}}。以 params.、platform.、uses. 这些种类开头的 {{…}} 平台都当引用去读：没有配对的 }}、引号不成对、种类后面的名字不是小写字母数字下划线、竖线后面是空的，都读不了，安装时会停下（CAT-INST-39）。种类像是拼错了的（param.、use.、Params.）也报这个编号：它们不会被替换。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-46，写了是哪个文件的哪一处、哪个 {{…}}

可能原因（逐条判定）：

1. 变换的参数没用双引号，或引号不成对 —— 判定：lint 输出里那个 {{…}}：参数要写成 "…"，里面的双引号写成 \"
2. 引用写成了大写或带空格（params.Admin、params .x） —— 判定：那个 {{…}} 的种类和名字
3. 种类拼错了（param.、use.、platforms.） —— 判定：message 写“是不是想写 …”
4. {{ 和 }} 之间换了行，或少写了一个 } —— 判定：清单里那一处的原文

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］照 message 改成 {{种类.名字 | 变换 "参数"}}；要写出字面的 {{ 就写 \{{

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-47、CAT-LINT-51
- 演练方法：在一份测试清单的 env 里写 value: '{{params.admin | default "x}}'（引号不成对），运行 acornfox catalog lint 目录

### CAT-LINT-47 没有这种变换，或变换的参数不对

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：引用后面能接的变换是程序里固定的一张表：default、urlencode、b64、json、yaml、shell、lower、upper、replace、map、sha256。名字不在表里，或参数个数不对（default 一个、replace 两个、map 至少一个且每个写成 "键=值"，键不能重复；其余不带参数），都报这个编号。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-47，写了是哪个 {{…}}、哪个变换

可能原因（逐条判定）：

1. 用了表里没有的变换（例如 bcrypt、toml、trim） —— 判定：message 列了能用的变换
2. map 的参数没写成 "键=值"，或一个键写了两次 —— 判定：那个 {{…}} 的 map 参数
3. default 没写参数，或 replace 只写了一个参数 —— 判定：那个 {{…}} 的参数个数

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］换成表里的变换、改对参数；表里没有要用的变换，在目录的 gaps.yaml 登记一条，先别上架

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-46
- 演练方法：在一份测试清单的 env 里写 value: '{{platform.region | trim}}'，运行 acornfox catalog lint 目录

### CAT-LINT-48 if 的写法不对

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：env 的一条、files 的一个文件可以写 if：成立时才写。只认三种写法：'引用'（有值且不是 false、0、空时成立）、'引用 == 值'、'引用 != 值'；引用不带 {{ }}，值是一个词、不带引号和空格。引用没有值时（例如可选的服务没接上）条件不成立。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-48，写了是哪一条的 if

可能原因（逐条判定）：

1. 引用写成了 {{platform.region}} —— 判定：if 里有没有 {{ }}
2. 写了 and、or、>、in 这类三种写法以外的东西，或值带了引号、空格 —— 判定：if 的原文

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］改成三种写法之一；要按几种情况给不同的值，写几条同名的 env，各带一个 if，或者用一条 env 加 | map 变换

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-49
- 演练方法：在一份测试清单的 env 里给一条加 if: '{{platform.region}} == cn'，运行 acornfox catalog lint 目录

### CAT-LINT-49 同名的环境变量可能同时成立

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：env 里同一个 key 可以写几条，按不同情况给不同的值；平台要能只看 if 就知道它们不会同时成立：每条都要写 if，任意两条要么是同一个引用 == 不同的值，要么是同一个值一条 == 一条 !=。看不出来的就报错：两条同时成立时安装停下（CAT-INST-39）。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-49，写了是哪个环境变量、有几条

可能原因（逐条判定）：

1. 同名的几条里有一条没写 if —— 判定：清单里这个 key 的每一条
2. 两条的 if 引用的不是同一个值，或比较的写法证明不了互斥 —— 判定：两条 if 的原文

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］每条都写 if，用同一个引用的 == 不同值；只是换一个值的，合成一条，用 | map "键=值" 变换

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-48
- 演练方法：在一份测试清单的 env 里写两条同名的环境变量，一条 if: 'platform.region == cn'、一条不写 if，运行 acornfox catalog lint 目录

### CAT-LINT-50 启动命令里引用了保密的值

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：container.command 是容器的启动参数，每一项可以引用 {{params.*}}、{{platform.*}} 等，但不能引用保密的值（保密的设置项、保密的平台变量、服务给的保密项）：容器的启动参数这台机器上的任何用户都能在进程列表里看到，docker inspect 也会显示。保密值经 env 给应用。
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-50，写了是启动命令的第几项、引用的是哪个值

可能原因（逐条判定）：

1. 把密码写进了启动参数（例如 --requirepass {{params.password}}） —— 判定：lint 输出的路径（/container/command/…）

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］把这个值改成经 env 给应用（env 里写 from: params.名字，secret: true）；应用只认命令行参数的，写一个配置文件（files）由应用读

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-35
- 演练方法：在一份测试清单的 container.command 里加一项 '--password={{params.admin_password}}'（admin_password 是 secret 设置项），运行 acornfox catalog lint 目录

### CAT-LINT-51 这个 {{…}} 不是平台的引用，会原样保留（提醒）

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：平台只替换以 params.、platform.、uses.、self.、saved.、issued.、consumer.、issue. 开头的 {{…}}，别的 {{…}}（例如应用自己的模板写法 {{ $json.x }}、{{ .Values }}）原样写进去。这是提醒，不是错误：是应用自己的写法就不用管；本来想引用平台的值，就是写错了，装好后应用里会是一段 {{…}} 文字。
- 下一步：确认这个 {{…}} 是应用自己的写法；是想引用平台的值就改成 {{种类.名字}}
- 现象：lint 给出 CAT-LINT-51 提醒（level 是 warning），写了是哪一处的哪个 {{…}}

可能原因（逐条判定）：

1. 应用自己的配置就用双花括号（模板引擎、工作流表达式） —— 判定：应用的文档
2. 想引用平台的值，种类写错了或没写种类（{{token}}、{{env.X}}） —— 判定：那个 {{…}} 的原文

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］是应用自己的写法就不改；想引用平台的值就改成 {{params.名字}} 这类写法

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有这条提醒，或确认过它是应用自己的写法
- 相关编号：CAT-LINT-46
- 演练方法：在一份测试清单的 files 模板里写一行 {{token}}，运行 acornfox catalog lint 目录（给提醒、不报错）

### CAT-LINT-52 on_change 步骤的 watch 里列了不会变的参数

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：init 步骤写 when: on_change 时，watch 列出“哪几项变了才跑这一步”（只能写 params.<名>、platform.<名>、uses.<需要名>.<项>）。平台生成的参数（hidden）装好以后不会变，列了它这一步永远不会跑。
- 下一步：按 lint 输出的路径（/init/…/watch）改 watch：去掉 hidden 的参数，改看会变的设置
- 现象：lint 给出 CAT-LINT-52 错误，路径是 /init/某一步/watch

可能原因（逐条判定）：

1. watch 里写了 hidden 的参数（平台生成的密码、密钥） —— 判定：lint 输出里的引用，对照清单 params 里它有没有 hidden: true

排查顺序：

1. acornfox catalog lint 目录

处置：

- ［1 级 可以直接做］改清单里这一步的 watch

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误
- 相关编号：CAT-LINT-06
- 演练方法：在一份测试清单的 init 里加一步 when: on_change、watch: [params.某个 hidden 参数]，运行 acornfox catalog lint 目录

### CAT-BUILD-01 目录有检查错误，什么都没写

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：先 acornfox catalog lint 修完错误再 build
- 现象：acornfox catalog build 报 CAT-BUILD-01

可能原因（逐条判定）：

1. lint 有错误 —— 判定：acornfox catalog lint

排查顺序：

1. acornfox catalog lint

处置：

- ［1 级 可以直接做］修完再 build

- 必须先停下问用户：—（不用问）
- 确认恢复：build 成功

### CAT-BUILD-02 打包参数或文件不对

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出改：序号要大于 0、版本号要规范、应用目录里不能有符号链接或读不了的文件
- 现象：build 报 CAT-BUILD-02

可能原因（逐条判定）：

1. 序号小于 1、版本号奇怪、有符号链接或读不了的文件 —— 判定：输出写了是哪一个

排查顺序：

1. 读输出

处置：

- ［1 级 可以直接做］改参数或文件

- 必须先停下问用户：—（不用问）
- 确认恢复：build 成功

### CAT-BUILD-03 输出目录写不进去

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：换一个可写的输出目录或腾出磁盘
- 现象：build 报 CAT-BUILD-03

可能原因（逐条判定）：

1. 权限或磁盘 —— 判定：ls -ld 输出目录；df -h

排查顺序：

1. df -h

处置：

- ［1 级 可以直接做］换目录

- 必须先停下问用户：—（不用问）
- 确认恢复：build 成功

### CAT-BUILD-04 序号比上次发布的小，或相同但内容不同

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：序号只能往上加：用比上次发布大的序号重新 build
- 现象：build 报 CAT-BUILD-04

可能原因（逐条判定）：

1. 序号写小了，或同一序号想发不同的内容 —— 判定：上次发布的 version.txt

排查顺序：

1. 看上次发布的序号

处置：

- ［1 级 可以直接做］换更大的序号

- 必须先停下问用户：—（不用问）
- 确认恢复：build 成功

### CAT-BUILD-05 已发布的应用包会被换成别的内容

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：已发布的应用包不能改：内容变了就给应用发一个新版本号
- 现象：build 报 CAT-BUILD-05

可能原因（逐条判定）：

1. 改了已发布版本的清单或文件 —— 判定：输出写了是哪个应用的哪个版本

排查顺序：

1. 把改动挪到新版本

处置：

- ［1 级 可以直接做］改版本号
- ［2 级 先问用户］覆盖已发布的包——绝不

- 必须先停下问用户：—（不用问）
- 确认恢复：build 成功

### CAT-KEY-01 签名私钥文件缺失、读不了或格式不对

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：确认私钥文件路径，文件要是 ed25519 的 PKCS#8 PEM
- 现象：build、keygen 报 CAT-KEY-01

可能原因（逐条判定）：

1. 路径写错或文件不是那种钥匙 —— 判定：head -1 文件 是 -----BEGIN PRIVATE KEY-----

排查顺序：

1. 检查路径

处置：

- ［1 级 可以直接做］改路径
- ［2 级 先问用户］换钥匙

- 必须先停下问用户：换签名钥匙之前
- 确认恢复：build 成功

### CAT-KEY-02 签名私钥别人也能读（权限宽于 0600）

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：chmod 600 私钥文件后重试
- 现象：build 报 CAT-KEY-02

可能原因（逐条判定）：

1. 私钥文件权限太宽，或在 Windows 上查不了权限 —— 判定：ls -l 私钥文件

排查顺序：

1. ls -l

处置：

- ［1 级 可以直接做］chmod 600

- 必须先停下问用户：怀疑钥匙已泄露时
- 确认恢复：build 成功

### CAT-KEY-03 keygen 的目标文件已存在，不会覆盖

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：换一个文件名；不要删掉正在用的钥匙
- 现象：keygen 报 CAT-KEY-03

可能原因（逐条判定）：

1. 那个位置已有钥匙 —— 判定：ls 目标文件

排查顺序：

1. 确认是不是正在用的钥匙

处置：

- ［1 级 可以直接做］换文件名

- 必须先停下问用户：要换掉现有钥匙时
- 确认恢复：keygen 成功

### CAT-KEY-04 公钥字符串不是 32 字节的 ed25519 公钥

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：公钥要是 base64url 编码的 32 字节；照 keygen 输出原样复制
- 现象：verify 报 CAT-KEY-04

可能原因（逐条判定）：

1. 复制时少了或多了字符 —— 判定：长度和 keygen 输出对比

排查顺序：

1. 重新复制公钥

处置：

- ［1 级 可以直接做］改参数

- 必须先停下问用户：—（不用问）
- 确认恢复：verify 能运行

### CAT-VER-01 要核对的目录缺文件或 index.json 坏了

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：确认目录里有 version.txt、index.json、index.json.sig
- 现象：acornfox catalog verify 报 CAT-VER-01

可能原因（逐条判定）：

1. 目录不完整 —— 判定：ls 目录

排查顺序：

1. ls 目录

处置：

- ［1 级 可以直接做］重新 build

- 必须先停下问用户：—（不用问）
- 确认恢复：verify 通过

### CAT-VER-02 签名和受信公钥都对不上

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：用了别的钥匙签名，或文件被改过：不要发布，重新 build
- 现象：verify 报 CAT-VER-02

可能原因（逐条判定）：

1. 签名钥匙不对或文件被改 —— 判定：用正确的公钥再 verify

排查顺序：

1. 确认用的钥匙

处置：

- ［1 级 可以直接做］重新 build

- 必须先停下问用户：怀疑被改时
- 确认恢复：verify 通过

### CAT-VER-03 version.txt 和 index.json 里的序号不一致

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：重新 build，不要手改这两个文件
- 现象：verify 报 CAT-VER-03

可能原因（逐条判定）：

1. 手改过其中一个 —— 判定：cat version.txt；看 index.json 的 serial

排查顺序：

1. 重新 build

处置：

- ［1 级 可以直接做］重新 build

- 必须先停下问用户：—（不用问）
- 确认恢复：verify 通过

### CAT-VER-04 有应用包缺失，或校验值、大小和索引不符

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：重新 build 或补齐应用包后再 verify
- 现象：verify 报 CAT-VER-04

可能原因（逐条判定）：

1. 拷贝不全或包被改过 —— 判定：verify 输出写了是哪个包

排查顺序：

1. 看是哪个包

处置：

- ［1 级 可以直接做］重新 build

- 必须先停下问用户：—（不用问）
- 确认恢复：verify 通过

## 下载网关返回的编号（GW）

### GW-REQ-01 请求格式不对

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：请求格式不对
- 下一步：多半是程序和网关版本对不上；升级 AcornFox。没有机器钥匙时看 acornfox identity show
- AcornFox 已经自动做的：跳过网关，换下一个下载源
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-REQ-01

可能原因（逐条判定）：

1. 这台机器没有机器钥匙 —— 判定：sudo acornfox identity show

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-REG-01 这个 IP 今天新登记的机器太多

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这个 IP 今天新登记的机器太多
- 下一步：不用处理，明天恢复；应用照常从公共源装
- AcornFox 已经自动做的：跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-REG-01

可能原因（逐条判定）：

1. 同一个出口 IP 下今天有很多台新机器登记（公司网络、测试） —— 判定：问用户是不是在批量装机

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-AUTH-01 签名不对、时间戳超出范围或 nonce 重复

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：签名不对、时间戳超出范围或 nonce 重复
- 下一步：先校准本机时间：sudo acornfox doctor 看 SETUP-CLOCK-01
- AcornFox 已经自动做的：应答带 Date 头且和本机时钟差 60 秒以上时，按网关时间重签一次；还不行就跳过网关，提示里写明时钟差了多少，请校时（timedatectl set-ntp true）
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-AUTH-01

可能原因（逐条判定）：

1. 本机时钟和网关差 300 秒以上［doctor 自动判定］ —— 判定：doctor 的时钟一项写出差多少秒
2. 机器钥匙坏了 —— 判定：sudo acornfox identity show 报 ID-KEY-01

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-AUTH-02 这台机器还没登记

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这台机器还没登记
- 下一步：不用处理：会自动登记
- AcornFox 已经自动做的：自动登记一次再重试；还不行就跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-AUTH-02

可能原因（逐条判定）：

1. 第一次用网关，或网关清过登记 —— 判定：再试一次就好了

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-AUTH-03 这台机器被拉黑

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：这台机器被拉黑
- 下一步：应用照常从公共源装；要用网关请联系我们并附上 acornfox identity show 的安装编号
- AcornFox 已经自动做的：跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-AUTH-03

可能原因（逐条判定）：

1. 这台机器的请求被网关判为滥用 —— 判定：持续返回这个编号

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-LIM-01 这台机器今天的次数用完了（明天恢复）

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这台机器今天的次数用完了（明天恢复）
- 下一步：不用处理，明天恢复；今天应用从公共源装
- AcornFox 已经自动做的：跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-LIM-01

可能原因（逐条判定）：

1. 今天下载次数超过每台机器的上限 —— 判定：今天装了很多次应用

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-LIM-02 这个 IP 请求太频繁

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：这个 IP 请求太频繁
- 下一步：稍后再试
- AcornFox 已经自动做的：等 retry_after 秒（不超过 30 秒）重试一次，还不行就跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-LIM-02

可能原因（逐条判定）：

1. 同一个 IP 短时间请求太多 —— 判定：同一网络下多台机器同时在装

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-OFF-01 网关停发（全局或这个文件）

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：网关停发（全局或这个文件）
- 下一步：不用处理：应用从公共源装；私有目录的应用要等网关恢复
- AcornFox 已经自动做的：跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-OFF-01

可能原因（逐条判定）：

1. 网关维护或这个文件暂停发放 —— 判定：curl -sS -m 5 <网关地址>/v1/health 的 issuing 是 false；只停了这个文件时健康检查照常是 true

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-ART-01 网关上没有这个文件

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：网关上没有这个文件
- 下一步：不用处理：应用从公共源装；私有目录的应用请联系给码的人
- AcornFox 已经自动做的：跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-ART-01

可能原因（逐条判定）：

1. 目录写的文件还没放上网盘 —— 判定：持续出现

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-BE-01 网关后端（网盘）出错

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：网关后端（网盘）出错
- 下一步：稍后再试；应用照常从公共源装
- AcornFox 已经自动做的：跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-BE-01

可能原因（逐条判定）：

1. 网盘临时故障 —— 判定：过一阵恢复

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-DB-01 网关的数据库出错

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：网关的数据库出错（HTTP 500）
- 下一步：稍后再试；应用照常从公共源装
- AcornFox 已经自动做的：跳过网关
- 现象：安装进度或命令输出里 gateway_code 是 GW-DB-01

可能原因（逐条判定）：

1. 网关自己的故障 —— 判定：过一阵恢复

排查顺序：

1. 稍后再试

处置：

- ［1 级 可以直接做］稍后再试

- 必须先停下问用户：持续出现时反馈
- 确认恢复：再装时进度写“经下载网关”

### GW-NIMP-01 接口预留，还没实现

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：接口预留，还没实现
- 下一步：不用处理
- AcornFox 已经自动做的：跳过网关
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-NIMP-01

可能原因（逐条判定）：

1. 程序比网关新，用到了网关还没开放的接口 —— 判定：持续出现

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-CAT-01 连接码不存在、格式不对或已被别的机器用过

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：连接码不存在、格式不对或已被别的机器用过
- 下一步：按 CAT-CONN-03 处理
- AcornFox 已经自动做的：提示用户检查连接码或要新码；不自动重试
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-CAT-01

可能原因（逐条判定）：

1. 码抄错或已被别的机器用过 —— 判定：和原文逐位对

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-CAT-02 连接码已过期

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：连接码已过期
- 下一步：按 CAT-CONN-04 处理
- AcornFox 已经自动做的：提示用户要新码
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-CAT-02

可能原因（逐条判定）：

1. 码过了有效期 —— 判定：—（网关这样说）

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-CAT-03 私有目录已停用

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：私有目录已停用
- 下一步：按 CAT-CONN-05 处理
- AcornFox 已经自动做的：提示用户；已装的应用照常运行
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-CAT-03

可能原因（逐条判定）：

1. 给码的人停用了目录 —— 判定：—（网关这样说）

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-CAT-04 连接码试得太频繁

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P3（提示或用法问题，不急）
- 是什么：连接码试得太频繁
- 下一步：按 CAT-CONN-06 处理
- AcornFox 已经自动做的：等 retry_after 秒后再让用户试；不自动循环
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-CAT-04

可能原因（逐条判定）：

1. 短时间试了很多次 —— 判定：—（网关这样说）

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-CAT-05 不能通过这个私有目录下载这个文件（没授权、已撤销、目录停用或文件不在目录里）

- 分组：下载网关返回的编号（GW）
- 来源：下载网关返回的编号（AcornFox 只是转述），常出现在 gateway_code 里
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：不能通过这个私有目录下载这个文件（没授权、已撤销、目录停用或文件不在目录里）
- 下一步：按 CAT-INST-26 处理：先 acornfox catalog sync 看目录是否已断开
- AcornFox 已经自动做的：不去公共源找；提示重新连接或联系管理员
- 现象：安装进度、catalog connect 或命令 --json 输出里 gateway_code 是 GW-CAT-05

可能原因（逐条判定）：

1. 授权被撤销或目录停用 —— 判定：acornfox catalog sources
2. 文件不在目录里 —— 判定：联系给码的人

排查顺序：

1. 看是哪一步报的（安装、连接私有目录、同步）
2. sudo acornfox doctor 看下载网关和时钟两项

处置：

- ［1 级 可以直接做］按“下一步”做

- 必须先停下问用户：要改系统设置或联系给码的人时
- 确认恢复：再操作时不再出现这个编号
- 相关编号：SETUP-GW-01

### GW-NET-01 连不上网关或超时（本机判断）

- 分组：下载网关返回的编号（GW）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：连不上网关或超时（本机判断）
- 下一步：看 doctor 的下载网关一项（SETUP-GW-01）：网络、代理和网关地址
- AcornFox 已经自动做的：跳过网关；看网络、代理和 ACORNFOX_GATEWAY 的地址
- 现象：安装进度或命令输出里 gateway_code 是 GW-NET-01

可能原因（逐条判定）：

1. 网络、DNS、代理或防火墙问题［doctor 自动判定］ —— 判定：doctor 的下载网关一项写的原因
2. 网关地址写错［doctor 自动判定］ —— 判定：doctor 写出的地址
3. 网关一时回得慢，30 秒内没给出下载地址（偶尔发生，多半过一会儿就好） —— 判定：doctor 的下载网关一项能连上；过几分钟再装一次走的是网关

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 SETUP-GW-01 处理

- 必须先停下问用户：改网络设置时
- 确认恢复：doctor 的下载网关一项通过
- 相关编号：SETUP-GW-01

### GW-RESP-01 网关的应答看不懂（本机判断）

- 分组：下载网关返回的编号（GW）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：网关的应答看不懂（本机判断）
- 下一步：多半是网关地址指错了，或中间有代理改了应答：看 doctor 的下载网关一项
- AcornFox 已经自动做的：跳过网关；多半是地址指错了，或中间有代理改了应答
- 现象：gateway_code 是 GW-RESP-01

可能原因（逐条判定）：

1. 地址指到了别的网站［doctor 自动判定］ —— 判定：doctor 写“应答不对”
2. 代理改了应答 —— 判定：关掉代理后正常

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 SETUP-GW-01 处理

- 必须先停下问用户：改网络设置时
- 确认恢复：doctor 的下载网关一项通过
- 相关编号：SETUP-GW-01

## 本机身份（ID-KEY）

### ID-KEY-01 机器钥匙读不了或不是 P-256 钥匙

- 分组：本机身份（ID-KEY）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：sudo acornfox identity show 看原因；文件坏了就 sudo acornfox identity reset --yes 重新生成（安装编号会变）
- 现象：acornfox identity show 报 ID-KEY-01；连接私有目录报 CAT-CONN-07，gateway_code 是 ID-KEY

可能原因（逐条判定）：

1. 钥匙文件被改坏或权限不对 —— 判定：sudo ls -l /var/lib/acornfox/identity/
2. 不是 root，读不了 —— 判定：用 sudo 再运行能读

排查顺序：

1. sudo acornfox identity show

处置：

- ［0 级 只读］用 sudo 再看
- ［2 级 先问用户］identity reset --yes（安装编号变了，连过的私有目录要重新连）

- 必须先停下问用户：重新生成之前
- 确认恢复：identity show 有安装编号

### ID-KEY-02 新钥匙保存不了

- 分组：本机身份（ID-KEY）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：检查数据目录的权限和磁盘空间（acornfox doctor）后重试
- 现象：acornfox identity reset --yes 报 ID-KEY-02

可能原因（逐条判定）：

1. 磁盘满或权限不对 —— 判定：doctor 的磁盘一项；用 sudo 运行

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］用 sudo 重试

- 必须先停下问用户：要清理磁盘时
- 确认恢复：identity show 有安装编号

## 本机能力和分册（MACHINE）

### MACHINE-01 本机能力有一块没算全

- 分组：本机能力和分册（MACHINE）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：acornfox machine 现场算这台服务器上有什么：应用、解决方案、服务、手册。应用目录读不了、某个应用用到哪些服务读不出来、服务登记表里有几条解不开时，这一块先空着，其余照常给，并在 warnings 里写明是哪一块（part）。答案不完整，但不会是错的。
- 下一步：运行 acornfox --json machine 看 warnings 里每一项的 part 和 message：manuals 是应用目录读不了（应用名字、一句话、手册没查到），先 acornfox catalog sync；services 是服务登记表的问题，看 acornfox services list；apps 是某个应用的情况读不出来，acornfox status 应用名
- AcornFox 已经自动做的：没有；缺的那一块留空，别的照常
- 现象：acornfox --json machine 的 warnings 不是空的，里面的 code 是 MACHINE-01；分册 machine-服务器名.md 末尾有一行“注意（MACHINE-01）”

可能原因（逐条判定）：

1. 应用目录读不了：从没同步过又读不了内置的，或缓存坏了［doctor 自动判定］ —— 判定：warnings 里 part 是 manuals；acornfox catalog sources 和 acornfox doctor 的“应用目录同步”一项
2. 某个应用的清单读不出来（目录里已经没有它，或它用的是旧格式）［doctor 自动判定］ —— 判定：warnings 里 part 是 apps，message 写着是哪个应用；acornfox apps vars 应用 也读不了
3. 服务登记表里有几条存的值解不开（数据目录里的加密钥匙换过或丢了）［doctor 自动判定］ —— 判定：warnings 里 part 是 services，message 列着是哪几条；acornfox services list 末尾有同样一行
4. 服务器上的程序正在升级、重启，应用列表暂时读不了 —— 判定：稍等一两分钟再运行 acornfox --json machine，warnings 没了就是这个

排查顺序：

1. acornfox --json machine（看 warnings）
2. acornfox doctor（看应用目录同步、应用用的服务两项）
3. 按 part 查：manuals → acornfox catalog sources；services → acornfox services list；apps → acornfox status 应用名

处置：

- ［1 级 可以直接做］acornfox catalog sync 让服务器同步一次应用目录，再运行 acornfox --json machine
- ［0 级 只读］把 warnings 里的 message 原样告诉用户；缺的那一块当成不知道，不要猜
- ［0 级 只读］值解不开的服务：不要删，把 doctor 的输出交给维护的人

- 必须先停下问用户：服务的值解不开时：停下问用户，不动那几条服务
- 确认恢复：acornfox --json machine 的 warnings 是空的
- 相关编号：CAT-SVC-08
- 演练方法：测试机上登记一条服务，再把数据目录里存服务值用的钥匙文件换成别的内容，运行 acornfox --json machine：warnings 里有 part 是 services 的一项（恢复：把钥匙文件换回来）

### MACHINE-02 “本机能力”分册写不进 Skill 目录

- 分组：本机能力和分册（MACHINE）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：acornfox skill install、acornfox skill refresh 和装、删、升级应用之后的自动刷新，会在 Skill 目录里写 machine-服务器名.md。目录没有写权限、磁盘满了或路径建不了时写不进去；分册只是缓存，写不进不影响任何命令，AI 工作台改用 acornfox --json machine 直接取。
- 下一步：不加 --json 运行 acornfox skill refresh，看 ✗ 那一行写的路径和原因；Skill 目录以前被 sudo 建过的，请用户确认后改回自己的属主；要马上用，直接运行 acornfox --json machine
- AcornFox 已经自动做的：没有；已装好的 Skill 和别的分册不受影响
- 现象：acornfox skill refresh 报“写入 … 失败”并带 MACHINE-02；acornfox --json skill refresh 的 failed 里一项的 code 是 MACHINE-02

可能原因（逐条判定）：

1. skills 目录没有写权限（例如以前用 sudo 建过） —— 判定：ls -ld ~/.claude/skills/acornfox ~/.codex/skills/acornfox，属主是 root
2. 磁盘满了 —— 判定：df -h ~
3. --dir 指的位置建不了目录或不可写 —— 判定：不加 --json 再运行，看 ✗ 那一行里的路径

排查顺序：

1. acornfox skill refresh（不加 --json）
2. ls -ld ~/.claude/skills/acornfox
3. df -h ~

处置：

- ［0 级 只读］acornfox --json machine 直接取，不靠分册
- ［1 级 可以直接做］acornfox skill refresh --dir 一个能写的 skills 目录
- ［2 级 先问用户］用 sudo 改 skills 目录的属主

- 必须先停下问用户：要用 sudo 改文件时
- 确认恢复：acornfox skill refresh 每台服务器后面都是 ✓，Skill 目录里有 machine-服务器名.md
- 相关编号：install_failed、MACHINE-03
- 演练方法：在测试机上把 ~/.claude/skills/acornfox 的权限改成只读，运行 acornfox skill refresh（恢复：chmod u+w 回来）

### MACHINE-03 这台服务器的“本机能力”分册没能写：连不上，或服务器上的 AcornFox 太旧

- 分组：本机能力和分册（MACHINE）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：写分册要先连上服务器读 acornfox machine 的结果。服务器连不上，或者它上面的 AcornFox 还没有这个接口（server_too_old）时，那台服务器的分册就没写成，其他服务器照常。skill install 只提醒，skill refresh 在所有服务器都没成时退出码是 1。
- 下一步：先运行 acornfox --target 服务器名 status 看能不能连上（连不上看里面的连接编号）；能连上但提示服务器版本旧，让用户先升级服务器上的 AcornFox；好了再 acornfox skill refresh
- AcornFox 已经自动做的：没有；这台服务器上一份分册（如果有）不动
- 现象：acornfox skill install 或 skill refresh 打印“服务器 … 的‘本机能力’分册没写成”并带 MACHINE-03；acornfox --json skill refresh 的 failed 里一项的 code 是 MACHINE-03

可能原因（逐条判定）：

1. 服务器关机、网络不通或 SSH 登不上 —— 判定：acornfox --target 服务器名 status 报连接编号（acornfox faults 那个编号）
2. 服务器上的 AcornFox 比这个命令行旧，没有 machine 接口 —— 判定：acornfox --target 服务器名 machine 报 server_too_old
3. 登记的服务器名已经不存在（target remove 过） —— 判定：acornfox target list 里没有这个名字

排查顺序：

1. acornfox target list
2. acornfox --target 服务器名 status
3. acornfox --target 服务器名 machine

处置：

- ［0 级 只读］照 status 报的连接编号处理（acornfox faults 编号）
- ［1 级 可以直接做］连上之后再运行 acornfox skill refresh

- 必须先停下问用户：要升级服务器上的 AcornFox 时（先问用户）
- 确认恢复：acornfox skill refresh 这台服务器后面是 ✓
- 相关编号：MACHINE-02、CLI-UPDATE-01
- 演练方法：在测试机上 acornfox target add 一台连不上的服务器（--url http://127.0.0.1:1），运行 acornfox skill refresh

## 兜底

### ANY-00 目录里找不到对应的情况

- 分组：兜底
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：先把出错那条输出里的 message 和 hint 原样告诉用户；hint 只是让这条命令换个参数或稍后重试的，可以照做；别的一律只读、不动任何东西，收集证据包交给用户（或反馈给维护者）
- 现象：看到的编号不在目录里，或现象对不上任何条目；接口和命令的通用错误码（例如 invalid_app、not_found、internal、usage）不在目录里，也到这一条

可能原因（逐条判定）：

1. 用法或输入不对（编号多是 invalid_ 开头、not_found、usage） —— 判定：message 说的是参数、名字或格式
2. 新情况，或程序比这份目录新 —— 判定：acornfox --json version；acornfox faults 看全部编号

排查顺序：

1. 读出错那条输出的 message 和 hint（用户只报了编号、手上没有那条输出时，请他把出错那条命令加 --json 的完整输出发来）
2. acornfox faults 看看有没有相近的条目
3. 收集证据包：acornfox doctor --json 的输出、出错那条命令加 --json 的完整输出（或安装、升级脚本最后 30 行）、acornfox --json version

处置：

- ［0 级 只读］把 message 和 hint 原样转述给用户
- ［1 级 可以直接做］按 hint 改这条命令自己的参数，或稍后重试同一条命令
- ［2 级 先问用户］hint 要删数据、停程序、花钱、对外开放或改系统设置的，先问用户
- ［0 级 只读］其余只收集信息，不做任何改动

- 必须先停下问用户：hint 要做的事超出“改参数、重试”时；同一条命令重试两次还不行时，写下现象、怀疑的原因和怎么判定，供补进目录
- 确认恢复：同一条命令不再报这个编号
