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
| INSTALL-PORT-01 | P2 | AcornFox 自己要用的端口（18800、18801、18808）被别的程序占用 | 把脚本列出的程序和 PID 转述给用户，问用户能不能停掉或改端口；处理后重跑 |
| INSTALL-PORT-02 | P3 | 80 或 443 被别的程序占用（常见：宝塔、1Panel、nginx） | 问用户二选一：停掉原来的程序后重跑，或者加 -s -- --allow-busy-80-443 共存（不能在 AcornFox 里绑域名、开 HTTPS 和公网控制台） |
| INSTALL-PORT-03 | P2 | 应用端口段 18810-18899 里有端口被别的程序占用 | 转述占用的程序，问用户能不能停掉或改端口；处理后重跑 |
| INSTALL-CLOUD-01 | P3 | --cloud 或 ACORNFOX_CLOUD_OVERRIDE 的值不认识 | 这是测试用的参数，去掉后重跑 |
| INSTALL-DOWN-01 | P3 | 服务器上已有更新版本的 AcornFox（或它的数据），要装的版本比它旧 | 什么都没改。不要换参数重试；要换新版本就用升级命令。确实要装旧版本只能先卸载并删除数据（--purge，不可恢复），必须用户明确同意 |
| UPGRADE-DOWN-01 | P3 | 升级时指定了比现在旧的版本 | 服务没停、什么都没改。AcornFox 只能升级：去掉 --version 重新运行升级命令；不要加 -y 或换旧版本号重试 |
| UPGRADE-ROLLBACK-01 | P3 | 用了 --rollback：平台不能回到旧版本 | 服务没停、什么都没改。升级没成功时脚本当时已自动恢复；应用自己的版本用 acornfox rollback 应用名 回退 |
| UPGRADE-BACKUP-01 | P2 | 停服务后备份没做完，没有升级 | 没有升级，原来的服务已重新启动。df -h /var/lib/acornfox 看空间，转述给用户；腾出空间后再升级 |
| UPGRADE-FAIL-01 | P2 | 新版本的数据库迁移失败，已自动恢复到升级前 | 没有升上，服务照常运行。告诉用户现在仍是原来的版本；先问用户要不要再试一次，再失败就停下，把输出反馈给维护者 |
| UPGRADE-FAIL-02 | P2 | 新版本的服务起不来，已自动恢复到升级前 | 没有升上，服务照常运行。先问用户要不要再试一次，再失败就停下，把输出和 journalctl -u acornfox-server -n 50 反馈给维护者 |
| UPGRADE-FAIL-03 | P2 | 升级中途出错或被中断（包括 SSH 断开），已自动恢复到升级前 | 没有升上，服务照常运行。可以再运行一次升级命令 |
| UPGRADE-RESTORE-01 | P1 | 升级失败后没能恢复到升级前（要人处理） | 停下。把输出原样给用户，请用户把 journalctl -u acornfox-server -n 50 和 -u acornfox-runner -n 50 反馈给维护者；不要删备份、不要重装、不要自己拷备份里的文件 |
| UNINSTALL-TTY-01 | P3 | 卸载脚本没有终端、没法询问确认，又没加 -y | 什么都没改。请用户在自己的终端里运行卸载命令；只有用户明确要你代为卸载时才加 -y |
| SETUP-NET-01 | P2 | 下载源都连不上 | 确认能上网后点“重试”（或运行 acornfox setup --resume）；国内网络可以换下载源：acornfox setup --resume --mirror <地址> |
| SETUP-NET-02 | P2 | 下载 45 秒没有进展，所有下载源都试过了 | 已下载的部分会保留，点“重试”从断点接着下；一直不行就换下载源或关掉代理再试 |
| SETUP-SUM-01 | P2 | 下载的文件校验不通过 | 已删掉坏文件，点“重试”会重新下载；换一个下载源更稳妥。反复出现请把诊断信息发给我们 |
| SETUP-RT-01 | P2 | Docker 装了但没有运行，需要你选用哪个运行环境 | 在页面上选“用 AcornFox 自带的运行环境”或“打开 Docker Desktop”；终端：acornfox setup --runtime builtin |
| SETUP-RT-02 | P1 | 自带的运行环境没能启动 | 点“重试”；还不行运行 acornfox doctor 看具体哪一项，再把诊断信息发给我们 |
| SETUP-RT-03 | P1 | 连不上 Docker | Linux 服务器用 install.sh 安装会自动装好 Docker；已经装了就 sudo systemctl start docker 后重试 |
| SETUP-RT-04 | P2 | Docker Desktop 没有按时启动 | 看一眼 Docker Desktop 的窗口，处理完它的提示后点“重试”；或者改用 AcornFox 自带的运行环境 |
| SETUP-WIN-01 | P2 | 需要管理员确认才能打开 WSL 2 | 点“继续”，在 Windows 弹出的管理员确认里选“是” |
| SETUP-WIN-02 | P2 | 需要重启电脑 | 保存好手上的工作后重启电脑；重启后 AcornFox 会自动接着装 |
| SETUP-WIN-03 | P2 | 这台电脑不能运行 WSL 2 虚拟机 | 在 BIOS 里打开虚拟化后重试；虚拟机里运行的话，要在宿主机上为它打开嵌套虚拟化 |
| SETUP-WIN-04 | P3 | 管理员确认被取消或失败 | 点“继续”再试一次，在弹窗里选“是”；不是管理员账号的话，请管理员登录后操作 |
| SETUP-WIN-05 | P2 | WSL 没有把端口转发到本机 | 把 .wslconfig 里的 localhostForwarding 改回 true 或删掉这一行，运行 wsl --shutdown，再运行 acornfox setup --resume |
| SETUP-EDGE-01 | P1 | 网关组件没有准备好 | 点“重试”；端口被占时运行 acornfox doctor 看是哪个端口，关掉占用的程序后再试 |
| SETUP-CHK-01 | P2 | 自检没有通过 | 按没通过那一项的说明处理后点“重试” |
| SETUP-CHK-02 | P1 | Docker 不能创建和删除容器 | 清理 Docker 空间（docker system prune）或重启运行环境后重试 |
| SETUP-CHK-03 | P1 | 网关管理口不安全或不应答 | 运行 acornfox doctor 查看；关掉占用 18808 的程序后重试 |
| SETUP-CHK-04 | P3 | 局域网打不开 | 只在本机用可以忽略。要在局域网打开：Windows 上运行 acornfox setup --confirm-admin 加防火墙规则；网络是公用的话，到 设置 → 网络 把它改成“专用网络”；然后重试 |
| SETUP-CHK-05 | P3 | 局域网转发没有启动 | 运行 acornfox doctor 看是哪些端口被占；关掉占用的程序，过 10 秒再看 |
| SETUP-SVC-01 | P1 | 后台服务 acornfox-server 没有应答 | 运行 sudo systemctl restart acornfox-runner acornfox-server，再运行 sudo acornfox doctor；还不行就把 journalctl -u acornfox-server -n 50 的输出交给用户 |
| SETUP-DISK-01 | P2 | 磁盘剩余空间不够（少于 10 GB 警告，少于 3 GB 不通过） | doctor 会写出是数据目录还是 Docker 数据目录所在的分区。先运行 docker system df 看占用，确认不需要后 docker system prune 清理（会删掉停掉的容器和没用的镜像，先问用户）；还不够就在云厂商控制台扩容磁盘 |
| SETUP-PORT-01 | P2 | 端口 80 或 443 被别的程序占用 | 和宝塔、1Panel、nginx 等共存时（安装用了 --allow-busy-80-443）不用处理，doctor 只给警告：应用照常用 http://服务器IP:端口 访问，不要停用户在用的程序；要用域名就在原来的面板里反向代理到 127.0.0.1:应用端口。doctor 判不通过，说明 AcornFox 里绑定了域名或开了公网控制台：先问用户占用的程序还要不要。不要了才停掉它（例如 sudo systemctl disable --now nginx），再运行 sudo systemctl restart acornfox-server；要保留就删掉 AcornFox 里绑定的域名（acornfox domain remove 域名）或关掉公网控制台（acornfox public-console disable） |
| SETUP-PORT-02 | P1 | 网关管理口 18808 被别的程序占用 | 按 doctor 写出的 PID 确认是什么程序，停掉后运行 sudo systemctl restart acornfox-server；应用的网址和域名都依赖这个端口 |
| SETUP-PORT-03 | P2 | 应用端口段 18810–18899 里有端口被别的程序占用 | 应用的端口在创建时分好、以后不变，分到被占端口的应用会一直上线失败（删掉重建往往还会分到同一个端口）。按 doctor 写出的端口和 PID 找到占用的程序，先问用户能不能停掉；能停就停掉后重新部署，不能停就把情况告诉用户：这些端口上的应用上线不了 |
| SETUP-PORT-04 | P3 | 局域网控制台端口 18802 被别的程序占用 | 不开局域网控制台可以忽略；要开的话先停掉占用的程序再运行 acornfox lan-console enable |
| SETUP-PORT-05 | P3 | 没有权限查看是哪个程序占用了端口 | 普通用户运行时出现是正常的：后台服务正常时，AcornFox 自己的端口（18808、应用端口、开着的局域网控制台端口）已经认出，不在这里；剩下的要确认就用 sudo acornfox doctor 再查一次 |
| SETUP-CLOCK-01 | P2 | 本机时钟不准，或者没开网络对时 | 先问用户，再运行 sudo timedatectl set-ntp true 打开网络对时，过一两分钟用 timedatectl 看 System clock synchronized 是否为 yes，然后再运行 acornfox doctor；一直同步不上的云主机，按云厂商文档把 chrony 或 systemd-timesyncd 改成内网时间服务器。时钟和下载网关差 300 秒以上时，下载网关会拒绝这台机器（GW-AUTH-01）：应用中心的镜像改从公共源拉，私有目录连不上；AcornFox 会按网关时间自动重试一次，但还是要把时钟校准 |
| SETUP-MAC-01 | P3 | Mac 版只有命令行客户端 | Mac 版只有命令行客户端，不能在 Mac 本机运行 AcornFox 服务；请把 AcornFox 装在一台 Linux 服务器上，用这台 Mac 上的命令行管理（acornfox target add …）。1. 在一台 Linux 服务器（amd64 或 arm64）上按使用文档用 install.sh 安装 AcornFox；2. 在这台 Mac 上运行 acornfox target add 名称 --ssh 用户@服务器地址；3. 之后 acornfox deploy、status、logs 等命令都作用在这台服务器上。检查服务器用 ssh 用户@服务器地址 sudo acornfox doctor |
| SETUP-GEN-01 | P2 | 准备运行环境时出错 | 点“重试”；还不行运行 acornfox doctor，把诊断信息发给我们 |
| SETUP-SYNC-01 | P3 | 应用目录上次同步失败，或者很久没同步 | 运行 acornfox catalog sync 看这次的结果；同步不上不影响已装的应用，安装时用手上的目录（缓存或程序内置的） |
| SETUP-GW-01 | P3 | 下载网关连不上或应答不对 | 公共目录的应用照常能装（镜像改从公共源拉，会慢一些），私有目录连不上、装不了。先看网络和代理，再看 ACORNFOX_GATEWAY 或 network.json 里的 gateway 有没有写错 |
| LAN-CONSOLE-01 | P3 | 这台服务器不能开局域网控制台 | 云主机、公网服务器进控制台：在自己电脑上运行 acornfox open（经 SSH 隧道）。确实在自家或办公室局域网里：等十几秒再运行 acornfox lan-console enable；还不行就看 acornfox doctor，把结果发给我们。不要为了开它去改安全组或防火墙 |
| LAN-CONSOLE-02 | P3 | 局域网控制台拒绝了不在局域网里的访问 | 这是保护，不用修。自己要从外面进控制台就用 acornfox open（经 SSH 隧道）。日志里反复出现陌生地址时，去路由器上把 18802 的端口映射删掉 |
| LAN-CONSOLE-03 | P2 | 局域网控制台开着但还没设密码 | 现在就用局域网里的电脑打开 acornfox lan-console status 给的地址设一个密码（至少 8 位），局域网里谁先打开谁设。不用局域网控制台就运行 acornfox lan-console disable。设好后 status 会显示是什么时间、从哪个地址设的；不是用户自己就运行 acornfox lan-console reset-password 重设 |
| lan_console_off | P3 | 局域网控制台没开，没有密码可清 | 不用重设密码：局域网控制台是关着的。用户要用它时先问用户，再 acornfox lan-console enable（对局域网开放 18802），然后请用户打开给出的地址设密码 |
| ssh_missing | P2 | 本机找不到 ssh 命令 | 安装 OpenSSH 客户端（Windows：设置 → 可选功能） |
| host_unreachable | P2 | 连不上服务器 | 检查地址、端口、安全组是否放行 22 |
| auth_failed | P2 | SSH 认证失败 | 先在终端里 `ssh user@host` 确认能免密登录（`ssh-copy-id`） |
| host_key_unknown | P3 | 服务器主机指纹未确认 | 先手动 `ssh user@host` 一次确认主机指纹 |
| host_key_changed | P1 | 服务器主机指纹与本机记录不一致 | 若确认服务器重装过或 IP 换了机器，运行 `ssh-keygen -R 主机` 删除旧记录，再手动 `ssh user@host` 确认新指纹；否则可能是中间人攻击，不要继续 |
| acornfox_missing | P2 | 服务器上没装 AcornFox | 服务器上尚未安装 AcornFox |
| permission_denied | P2 | 无权访问 acornfox 服务 | 把该用户加入 `acornfox-users` 组后重新登录 |
| server_down | P1 | acornfox 服务未运行 | 在服务器上执行 `systemctl status acornfox-server` |
| local_server_down | P1 | 连不上本机的 acornfox 服务 | Linux 服务器上执行 `systemctl status acornfox-server` 和 `acornfox doctor` 检查；Mac、Windows 本机运行 AcornFox 服务还没发布，请把 AcornFox 装到 Linux 服务器上，再用 `acornfox target add 名字 --ssh 用户@地址` 连接 |
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
| pull_timeout | P2 | 拉取镜像超时 | 国内服务器拉取公共镜像常超时，请为 Docker 配置镜像加速后重试 |
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
| route_failed | P1 | 路由持续同步失败超过 5 分钟 | 请检查 Caddy admin 是否可用 |
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
| caddy_unavailable | P2 | 网关（Caddy）没准备好，设不了域名验证 | sudo acornfox doctor 看网关和端口两项，按里面的编号处理后再运行同一条命令 |
| verify_required | P3 | 还没完成域名验证就要开通公网控制台 | 重新运行 acornfox public-console enable 域名 --json：它先验证域名，通过后才开通 |
| OPEN-APP-01 | P3 | 应用还没有访问端口 | 运行 acornfox status 应用名 看状态；部署成功后再打开 |
| OPEN-APP-02 | P3 | 经网址登记的服务器上，只本机的应用打不开 | 用 acornfox target add 名称 --ssh 用户@主机 登记这台服务器后再运行 acornfox open 应用名；或经用户同意改成局域网：acornfox app set --app 应用名 --expose lan |
| install_failed | P3 | Skill 一个也没装上 | 不加 --json 再运行一次 acornfox skill install，看每个工作台后面写的原因；也可以 acornfox skill install --dir 一个能写的 skills 目录，或 acornfox skill print 把内容交给工作台 |
| CAT-INST-01 | P3 | 目录里没有这个应用、配方或版本 | acornfox catalog list 看有哪些；目录太旧就 acornfox catalog sync |
| CAT-INST-02 | P3 | 安装参数不对 | 按 message 改 --set；acornfox catalog info <应用> 看参数。message 说原来用的本机网关本机没有了时，问用户要用哪个：--llm local（本机在运行的 New API）或 --llm own（自己的 Key）。网关只是停着报的是 CAT-INST-10，不是这个 |
| CAT-INST-03 | P3 | 已经有同名的应用 | 用 --name 换个名字；要重装先由用户确认后 acornfox delete 旧的 |
| CAT-INST-04 | P3 | 同一个应用正在安装 | 不要重复点安装；acornfox install status <task_id> 看进度 |
| CAT-INST-05 | P2 | 整机分配给应用的内存不够 | 问用户：调小别的应用（acornfox app set --memory）或调大整机上限（acornfox limits --memory） |
| CAT-INST-06 | P3 | CPU 架构不在应用支持的列表里 | 换一台机器，或等应用出这个架构的版本 |
| CAT-INST-07 | P2 | 访问端口用完了 | 删掉不用的应用后重试 |
| CAT-INST-08 | P3 | AcornFox 版本比应用要求的旧 | 先升级 AcornFox 再装（错误里的 needs_platform 是要升到的版本） |
| CAT-INST-09 | P3 | 平台 Key 还没开放 | 改用 --llm local（本机 New API）或 --llm own（自己的 Key，要用户提供） |
| CAT-INST-10 | P3 | 选了本机网关，但本机没有在运行的 New API | 看 message：说“装着但停着”的，先问用户，同意后 acornfox start <网关名>，acornfox status <网关名> 显示在运行后再装（不要再装一个 New API，同名的已经装着）；说“容器却没起来”的，acornfox status <网关名> 看它怎么了；本机还没有 New API 才 acornfox install new-api，或装配方 hermes-with-new-api。不想用本机网关就改 --llm own（自己的 Key，要用户提供） |
| CAT-INST-11 | P2 | 本机 New API 发不出令牌 | 看 New API 是否在运行（acornfox status new-api）；管理员密码被改过时改用 --llm own；message 里有 HTTP 429 是 New API 限制了登录和取令牌的次数（同一来源 20 分钟 20 次），等 20 分钟再装，不要反复重试 |
| CAT-INST-12 | P2 | 镜像从所有来源都没拿到 | 看 attempts；国内网络确认已配镜像加速；稍后重试 |
| CAT-INST-13 | P2 | 网关给的镜像包和目录里签名的校验值对不上 | 公共目录的应用自动换公共源，都不对就等目录更新；私有目录的应用直接停下，不去公共源找，等目录更新或联系给码的人。不要手动绕过 |
| CAT-INST-14 | P2 | 往数据卷里写配置文件失败 | 看 message；检查磁盘空间和 Docker 状态（acornfox doctor） |
| CAT-INST-15 | P2 | 容器没能启动或上线 | 看 diagnosis；acornfox logs <应用> 看日志 |
| CAT-INST-16 | P2 | 健康检查在时限内没通过 | acornfox logs <应用> 看日志；内存太小时调大后重装 |
| CAT-INST-17 | P2 | 装好后的初始化调用失败 | 看 message；acornfox logs <应用>；这次的应用已清理，可以直接重装 |
| CAT-INST-18 | P3 | 安装途中服务重启了，这次安装已作废并清理 | 重新安装 |
| CAT-INST-19 | P2 | 目录或应用包读不了、校验不过 | acornfox catalog sync 后重试；都不行就重装 AcornFox |
| CAT-INST-20 | P2 | 内部错误：运行器版本旧、数据库或 Docker 出错 | acornfox doctor；升级 AcornFox 后重试 |
| CAT-INST-21 | P3 | 没有这个安装任务 | 检查任务编号 |
| CAT-INST-22 | P3 | 这个应用不是从应用中心装的 | — |
| CAT-INST-23 | P3 | 清单用到了这个版本还不支持的写法 | 升级 AcornFox |
| CAT-INST-24 | P2 | 还留着以前同名应用的数据卷 | 停下问用户：旧数据还要不要。要接着用就加 --reuse-data 重装（页面上是“接上旧数据重装”；删除时保留过参数的，账号密码、改过的平台变量、大模型来源和配置文件都按以前的，这次写了的以这次为准）；不要旧数据就用 --name 换个名字装，或者由用户执行 message 里卷名对应的 docker volume rm 后再装 |
| CAT-INST-25 | P3 | 应用或配方来自已断开的私有目录 | 告诉用户联系给连接码的人；拿到新连接码后 acornfox catalog connect 新码，再装 |
| CAT-INST-26 | P2 | 私有目录的文件没从下载网关取到 | 看 gateway_code：GW-CAT-* 先 acornfox catalog sync 看目录是否已断开，还连着就请给码的人检查目录；GW-AUTH-01 先校准本机时间（acornfox doctor 看 SETUP-CLOCK-01）；GW-NET-01 或下载中断就检查网络后重装；网关被关掉时问用户是否打开；message 说清单没写镜像包、或没有这台机器架构（例如 linux/arm64）的镜像包时，告诉用户联系给码的人补上 |
| CAT-INST-27 | P2 | Docker 存镜像的分区剩余空间不够 | 问用户：删掉不用的应用或镜像（acornfox delete、docker image prune 由用户确认后执行），或扩容磁盘；acornfox doctor 看各分区剩余空间 |
| CAT-INST-28 | P3 | 改平台变量不对 | acornfox catalog platform-vars 看有哪些、哪些能改；按 message 改 --set platform.名字=值 |
| CAT-INST-29 | P3 | 应用包里的清单有这个版本不认识的写法 | 错误里带 needs_platform 的：先升级 AcornFox 到那个版本再装。不带的：是这份目录有问题，不是操作不对，过一会儿 acornfox catalog sync 后再试，还不行就把 message 反馈给我们；不要想办法绕过 |
| CAT-INST-30 | P3 | 安装的某一步比平常慢很多（还没失败） | 先运行 next_command（就是 timing.step.look：下载镜像等步骤是 acornfox doctor --json，启动、健康检查、初始化是 acornfox logs 应用 --tail 200 --json）看原因，只读、不用问用户；把 say 和看到的要点告诉用户，问用户接着等还是不装了；不要自己删除、重装或重启服务 |
| CAT-CONN-01 | P3 | 连接码格式不对 | 让用户照原样再抄一遍；0、1、8 会当成 O、I、B |
| CAT-CONN-02 | P3 | 这台服务器关掉了下载网关 | 问用户是否打开下载网关（去掉 ACORNFOX_GATEWAY=off 或 network.json 里的 "gateway": "off"） |
| CAT-CONN-03 | P3 | 连接码不存在、抄错了，或已被别的机器用过 | 让用户核对连接码；还不行就找给码的人要新码。不要反复试 |
| CAT-CONN-04 | P3 | 连接码已过期 | 找给码的人要新码 |
| CAT-CONN-05 | P3 | 这个私有目录已停用 | 告诉用户联系给码的人 |
| CAT-CONN-06 | P3 | 这一小时试连接码的次数用完了 | 等 retry_after 秒后再试，不要循环重试 |
| CAT-CONN-07 | P2 | 连不上下载网关、网关拒绝，或本机钥匙读不出 | GW-AUTH-01 先校准本机时间；GW-NET-01 看网络和代理；ID-KEY 看 acornfox identity show；稍后再试 |
| CAT-CONN-08 | P2 | 网关已经授权，但本机没记下来 | 检查数据目录权限和磁盘空间后，用同一个连接码再连一次（同一台机器重复用不算新的一次） |
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
| CAT-LINT-08 | P3 | 没有唯一一个带健康检查的主端点 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-09 | P3 | 配方引用的应用或连接不存在或对不上 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-10 | P3 | 默认对外方式不是只本机（要审核） | 这是警告：确认这个应用默认对局域网或公网开放是有意的，并写清理由 |
| CAT-LINT-11 | P3 | 交付级别要用户做事，但没写 user_steps | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-12 | P3 | 目录里没有 apps/ 也没有 recipes/ | 确认 lint 的目录参数指向目录仓库根目录 |
| CAT-LINT-13 | P3 | 应用目录里有文件不能放进应用包 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-14 | P3 | 镜像包没写 artifact_sha256（不会经下载网关） | 这是警告：要经下载网关发镜像包就补 artifact_sha256 和 artifact_size |
| CAT-LINT-15 | P3 | 给用户看的链接不是 http(s) 地址 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-16 | P3 | 引用了不存在的平台变量，或只有大模型参数能用的变量 | acornfox catalog platform-vars 看有哪些；按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-17 | P3 | 声明的最低平台版本比用到的写法要求的低 | 把 min_platform 改成 lint 算出的版本（或删掉让它自动算） |
| CAT-LINT-18 | P3 | 交付级别是 auto，但有参数要用户填 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-19 | P3 | 缺变量登记表 gaps.yaml 读不了或格式不对 | 按输出改 gaps.yaml，再运行 acornfox catalog gaps |
| CAT-LINT-20 | P3 | 按架构写的镜像包互相矛盾 | 按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误 |
| CAT-LINT-21 | P3 | 按架构写了镜像包，但有的架构没有（提醒） | 要在那个架构上走下载网关，就补上它的镜像包；不补的话那个架构从公共镜像源取，私有目录的应用在那个架构上装不了 |
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

### INSTALL-PORT-01 AcornFox 自己要用的端口（18800、18801、18808）被别的程序占用

- 分组：安装脚本（install.sh）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：把脚本列出的程序和 PID 转述给用户，问用户能不能停掉或改端口；处理后重跑
- 现象：安装或升级脚本列出占用端口的程序，打印“故障编号: INSTALL-PORT-01”

可能原因（逐条判定）：

1. 别的程序或容器正好用了 18800、18801 或 18808 —— 判定：sudo ss -ltnp | grep -E ':(18800|18801|18808) '，看 PID 对应的程序（ps -fp PID）
2. 以前装过的 AcornFox 进程没退干净（不是用安装脚本装的、或手动启动的） —— 判定：ps -fp PID 显示是 acornfox

排查顺序：

1. sudo ss -ltnp | grep -E ':(18800|18801|18808) '
2. ps -fp 占用的 PID

处置：

- ［0 级 只读］转述占用的程序和 PID
- ［2 级 先问用户］停掉或改掉占用端口的程序

- 必须先停下问用户：停任何程序之前都要问用户
- 确认恢复：ss 里这三个端口没有别的程序，重跑安装或升级走过端口检查
- 演练方法：在测试机上 python3 -m http.server 18808 后运行 install.sh

### INSTALL-PORT-02 80 或 443 被别的程序占用（常见：宝塔、1Panel、nginx）

- 分组：安装脚本（install.sh）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：问用户二选一：停掉原来的程序后重跑，或者加 -s -- --allow-busy-80-443 共存（不能在 AcornFox 里绑域名、开 HTTPS 和公网控制台）
- 现象：安装或升级脚本打印“80/443 已被别的程序占用”或“要开通公网控制台，但 80/443 被别的程序占用”和“故障编号: INSTALL-PORT-02”

可能原因（逐条判定）：

1. 服务器上原来装了宝塔、1Panel、nginx、Apache 或宿主机上的 Caddy —— 判定：sudo ss -ltnp | grep -E ':(80|443) '，ps -fp PID
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

## 升级和卸载脚本

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

### SETUP-WIN-01 需要管理员确认才能打开 WSL 2

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：点“继续”，在 Windows 弹出的管理员确认里选“是”
- 这一项要用户自己确认或操作
- 现象：准备运行环境等你确认，带 SETUP-WIN-01

可能原因（逐条判定）：

1. 这台电脑没有开 WSL 2 功能 —— 判定：wsl --status 说没有安装

排查顺序：

1. 请用户点继续

处置：

- ［2 级 先问用户］用户在管理员弹窗里选“是”

- 必须先停下问用户：总是：只有用户本人能确认
- 确认恢复：准备运行环境进入下一步

### SETUP-WIN-02 需要重启电脑

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：保存好手上的工作后重启电脑；重启后 AcornFox 会自动接着装
- 这一项要用户自己确认或操作
- 现象：准备运行环境等你重启，带 SETUP-WIN-02

可能原因（逐条判定）：

1. 刚打开 WSL 2 / 虚拟机平台功能，Windows 要重启才生效 —— 判定：刚做完 SETUP-WIN-01

排查顺序：

1. 请用户保存工作后重启

处置：

- ［2 级 先问用户］用户自己重启

- 必须先停下问用户：总是
- 确认恢复：重启后准备运行环境接着走

### SETUP-WIN-03 这台电脑不能运行 WSL 2 虚拟机

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：在 BIOS 里打开虚拟化后重试；虚拟机里运行的话，要在宿主机上为它打开嵌套虚拟化
- 这一项要用户自己确认或操作
- 现象：准备运行环境报 SETUP-WIN-03

可能原因（逐条判定）：

1. BIOS/UEFI 里关了虚拟化（Intel VT-x / AMD-V） —— 判定：任务管理器 → 性能 → CPU 里“虚拟化”是“已禁用”
2. 这台 Windows 本身是虚拟机且没开嵌套虚拟化 —— 判定：systeminfo 显示是虚拟机
3. Windows 版本太旧（需要 Windows 10 2004 以上或 Windows 11） —— 判定：winver 看版本

排查顺序：

1. 看任务管理器里的虚拟化
2. winver

处置：

- ［2 级 先问用户］用户进 BIOS 打开虚拟化

- 必须先停下问用户：总是
- 确认恢复：重试后运行环境能启动

### SETUP-WIN-04 管理员确认被取消或失败

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：点“继续”再试一次，在弹窗里选“是”；不是管理员账号的话，请管理员登录后操作
- 这一项要用户自己确认或操作
- 现象：准备运行环境报 SETUP-WIN-04

可能原因（逐条判定）：

1. 在管理员确认里选了“否” —— 判定：问用户刚才点了什么
2. 当前账号不是管理员 —— 判定：设置 → 账户里看账户类型

排查顺序：

1. 问用户

处置：

- ［2 级 先问用户］用户再点一次“继续”

- 必须先停下问用户：总是
- 确认恢复：准备运行环境进入下一步

### SETUP-WIN-05 WSL 没有把端口转发到本机

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：把 .wslconfig 里的 localhostForwarding 改回 true 或删掉这一行，运行 wsl --shutdown，再运行 acornfox setup --resume
- 现象：准备运行环境报 SETUP-WIN-05；应用在本机打不开

可能原因（逐条判定）：

1. WSL 的 localhost 转发被关掉了（%USERPROFILE%\.wslconfig 里写了 localhostForwarding=false） —— 判定：type %USERPROFILE%\.wslconfig
2. WSL 网络服务出错 —— 判定：wsl --shutdown 后重试能过

排查顺序：

1. 看 .wslconfig

处置：

- ［2 级 先问用户］改 .wslconfig（用户的配置文件）
- ［2 级 先问用户］wsl --shutdown 后 acornfox setup --resume

- 必须先停下问用户：改用户的配置文件、关掉 WSL 之前
- 确认恢复：自检通过

### SETUP-EDGE-01 网关组件没有准备好

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：点“重试”；端口被占时运行 acornfox doctor 看是哪个端口，关掉占用的程序后再试
- 现象：doctor 或运行环境页的“网关（Caddy）”一项不通过；应用网址打不开

可能原因（逐条判定）：

1. Caddy 镜像没下载下来（看上一条下载记录） —— 判定：sudo docker image ls | grep caddy 为空
2. 端口 18808 或 18810–18859 被别的程序占了［doctor 自动判定］ —— 判定：doctor 的端口一项（SETUP-PORT-02、SETUP-PORT-03）
3. Docker 里有旧的 acornfox-caddy 容器卡住 —— 判定：sudo docker ps -a --filter name=acornfox-caddy 状态不是 Up

排查顺序：

1. sudo acornfox doctor
2. sudo docker logs --tail 50 acornfox-caddy

处置：

- ［1 级 可以直接做］sudo systemctl restart acornfox-server（会重建网关容器，应用容器不停）
- ［2 级 先问用户］停掉占用端口的程序

- 必须先停下问用户：要停别的程序时；重启后仍不行时带上证据包
- 确认恢复：doctor 的网关一项通过，应用网址能打开
- 相关编号：SETUP-PORT-02、SETUP-PORT-03

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
- 下一步：运行 acornfox doctor 查看；关掉占用 18808 的程序后重试
- 现象：自检“网关管理口”一项没通过

可能原因（逐条判定）：

1. 18808 端口被别的程序占用［doctor 自动判定］ —— 判定：doctor 的 SETUP-PORT-02
2. Caddy 容器没有运行 —— 判定：sudo docker ps --filter name=acornfox-caddy

排查顺序：

1. acornfox doctor

处置：

- ［2 级 先问用户］停掉占用 18808 的程序

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

1. 服务器上原来装了 nginx、Apache、宝塔、1Panel 或宿主机上的 Caddy［doctor 自动判定］ —— 判定：sudo ss -ltnp | grep -E ':(80|443) '
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

### SETUP-PORT-02 网关管理口 18808 被别的程序占用

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P1（应用或服务停了，马上处理）
- 下一步：按 doctor 写出的 PID 确认是什么程序，停掉后运行 sudo systemctl restart acornfox-server；应用的网址和域名都依赖这个端口
- 现象：doctor 的端口一项不通过，写“网关管理口 …”；应用网址、域名配置不上

可能原因（逐条判定）：

1. 别的程序或容器占了 18808［doctor 自动判定］ —— 判定：doctor 写出的 PID：ps -fp PID
2. 旧版留下的进程还没退出 —— 判定：ps -fp PID 是旧的 acornfox 或 caddy

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
- 下一步：普通用户运行时出现是正常的：后台服务正常时，AcornFox 自己的端口（18808、应用端口、开着的局域网控制台端口）已经认出，不在这里；剩下的要确认就用 sudo acornfox doctor 再查一次
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

### SETUP-MAC-01 Mac 版只有命令行客户端

- 分组：准备运行环境和本机自检（doctor）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：Mac 版只有命令行客户端，不能在 Mac 本机运行 AcornFox 服务；请把 AcornFox 装在一台 Linux 服务器上，用这台 Mac 上的命令行管理（acornfox target add …）。1. 在一台 Linux 服务器（amd64 或 arm64）上按使用文档用 install.sh 安装 AcornFox；2. 在这台 Mac 上运行 acornfox target add 名称 --ssh 用户@服务器地址；3. 之后 acornfox deploy、status、logs 等命令都作用在这台服务器上。检查服务器用 ssh 用户@服务器地址 sudo acornfox doctor
- 这一项要用户自己确认或操作
- 现象：在 Mac 上运行 acornfox install、serve、doctor 时报 SETUP-MAC-01

可能原因（逐条判定）：

1. 这个版本的 Mac 版只当命令行客户端，用来管理 Linux 服务器上的 AcornFox；在 Mac 本机安装和运行（install、serve，以及对本机的 setup、doctor）还没有开放 —— 判定：uname 是 Darwin

排查顺序：

1. 问用户有没有 Linux 服务器

处置：

- ［2 级 先问用户］在 Linux 服务器上安装后，用 acornfox target add 登记

- 必须先停下问用户：要用户准备服务器（可能花钱）时
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
- 是什么：后台服务启动时同步一次应用目录，之后每天同步一次。doctor 和控制台“运行环境”页读的是后台服务记下的上次结果：什么时候、成没成、从哪个来源、在用第几号目录。
- 下一步：运行 acornfox catalog sync 看这次的结果；同步不上不影响已装的应用，安装时用手上的目录（缓存或程序内置的）
- AcornFox 已经自动做的：所有来源都失败时继续用手上的目录；下次定时同步会再试
- 现象：doctor 或运行环境页的“应用目录”一项警告（SETUP-SYNC-01）；应用中心看不到新上架的应用或新版本

可能原因（逐条判定）：

1. 这台服务器连不上目录的来源（官网和 Gitee 备用都不通，或被代理、防火墙拦了）［doctor 自动判定］ —— 判定：acornfox catalog sync --json 的 attempts 里每个来源都是 CAT-SYNC-02
2. 来源返回的内容验不过签名或版本不对（来源正在发布、镜像太旧） —— 判定：attempts 里是 CAT-SYNC-04、CAT-SYNC-05、CAT-SYNC-06
3. 后台服务很久没运行（停过两天以上），定时同步没跑［doctor 自动判定］ —— 判定：doctor 写“上次同步是 … 前”，systemctl status acornfox-server 看启动时间
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
- 下一步：Linux 服务器上执行 `systemctl status acornfox-server` 和 `acornfox doctor` 检查；Mac、Windows 本机运行 AcornFox 服务还没发布，请把 AcornFox 装到 Linux 服务器上，再用 `acornfox target add 名字 --ssh 用户@地址` 连接
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
- 下一步：国内服务器拉取公共镜像常超时，请为 Docker 配置镜像加速后重试
- 现象：code pull_timeout

可能原因（逐条判定）：

1. 国内服务器直连 Docker Hub —— 判定：cat /etc/docker/daemon.json 没有 registry-mirrors

排查顺序：

1. cat /etc/docker/daemon.json

处置：

- ［2 级 先问用户］配置镜像加速（改 Docker 配置）

- 必须先停下问用户：改 Docker 配置之前
- 确认恢复：部署到 live

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
- 下一步：请检查 Caddy admin 是否可用
- 现象：diagnosis.code route_failed

可能原因（逐条判定）：

1. 网关（Caddy）没在运行或管理口被占［doctor 自动判定］ —— 判定：sudo acornfox doctor 的网关和端口两项

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 SETUP-EDGE-01 处理

- 必须先停下问用户：要停别的程序时
- 确认恢复：部署到 live
- 相关编号：SETUP-EDGE-01、SETUP-PORT-02

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
3. 申请次数超了证书机构的限额 —— 判定：sudo docker logs acornfox-caddy 里有 rate limit

排查顺序：

1. sudo docker logs --tail 50 acornfox-caddy

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

### caddy_unavailable 网关（Caddy）没准备好，设不了域名验证

- 分组：域名和公网控制台
- 默认等级：P2（功能用不了，尽快处理）
- 下一步：sudo acornfox doctor 看网关和端口两项，按里面的编号处理后再运行同一条命令
- 现象：acornfox public-console enable 返回 code caddy_unavailable（HTTP 502）

可能原因（逐条判定）：

1. 网关容器没在运行或还在准备（第一次启动要下载镜像）［doctor 自动判定］ —— 判定：doctor 的网关一项（SETUP-EDGE-01）
2. Caddy 的管理口被别的程序占了［doctor 自动判定］ —— 判定：doctor 的端口一项（SETUP-PORT-02）

排查顺序：

1. sudo acornfox doctor

处置：

- ［1 级 可以直接做］按 SETUP-EDGE-01 处理
- ［1 级 可以直接做］网关还在准备时等一两分钟再试

- 必须先停下问用户：要停别的程序时
- 确认恢复：public-console enable 不再报 caddy_unavailable
- 相关编号：SETUP-EDGE-01、SETUP-PORT-02

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

排查顺序：

1. acornfox catalog list
2. acornfox catalog sync
3. acornfox catalog sources

处置：

- ［1 级 可以直接做］改对名字或版本后重装
- ［1 级 可以直接做］acornfox catalog sync

- 必须先停下问用户：—（不用问）
- 确认恢复：acornfox catalog info 应用 能返回

### CAT-INST-02 安装参数不对

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：参数不对：没有这个参数、这个参数由平台生成不能填、格式不符或缺了必填项；接上旧数据重装时，原来用的本机模型网关已经不在本机了（删掉了），也报这个
- 下一步：按 message 改 --set；acornfox catalog info <应用> 看参数。message 说原来用的本机网关本机没有了时，问用户要用哪个：--llm local（本机在运行的 New API）或 --llm own（自己的 Key）。网关只是停着报的是 CAT-INST-10，不是这个
- 现象：安装返回 CAT-INST-02（HTTP 400），message 写了是哪个参数

可能原因（逐条判定）：

1. --set 写了清单里没有的参数 —— 判定：acornfox catalog info 应用 --json 的参数表里没有它
2. 填了由平台生成的参数（密码、密钥） —— 判定：参数表里它是 hidden、generate
3. 值的格式不对，或缺了必填项（含没选大模型来源 --llm） —— 判定：message 写的那个参数

排查顺序：

1. 读 message
2. acornfox catalog info 应用

处置：

- ［1 级 可以直接做］按 message 改 --set 后重装

- 必须先停下问用户：必填项要用户提供的内容（例如自己的模型 Key）时
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

### CAT-INST-09 平台 Key 还没开放

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：平台 Key 还没开放
- 下一步：改用 --llm local（本机 New API）或 --llm own（自己的 Key，要用户提供）
- 现象：安装时选了平台 Key，返回 CAT-INST-09（HTTP 400）

可能原因（逐条判定）：

1. 这个版本还没有平台发的模型 Key —— 判定：安装用了 --llm platform

排查顺序：

1. 问用户想用本机 New API 还是自己的 Key

处置：

- ［1 级 可以直接做］改 --llm local
- ［2 级 先问用户］改 --llm own（要用户提供 Key）

- 必须先停下问用户：要用户提供自己的 Key 时
- 确认恢复：安装任务开始

### CAT-INST-10 选了本机网关，但本机没有在运行的 New API

- 分组：应用中心安装（CAT-INST）
- 默认等级：P3（提示或用法问题，不急）
- 是什么：要用本机的模型网关（选了 --llm local，或接上旧数据时原来用的就是它），但它没装、停着或没在运行
- 下一步：看 message：说“装着但停着”的，先问用户，同意后 acornfox start <网关名>，acornfox status <网关名> 显示在运行后再装（不要再装一个 New API，同名的已经装着）；说“容器却没起来”的，acornfox status <网关名> 看它怎么了；本机还没有 New API 才 acornfox install new-api，或装配方 hermes-with-new-api。不想用本机网关就改 --llm own（自己的 Key，要用户提供）
- 现象：安装返回 CAT-INST-10（HTTP 409）

可能原因（逐条判定）：

1. 还没装 New API —— 判定：acornfox apps 里没有从应用中心装的 new-api
2. New API 装了但停着（acornfox stop 过） —— 判定：message 写“装着但停着”；acornfox status new-api 不是 running
3. New API 应该在运行，容器却没起来 —— 判定：message 写“容器却没起来”；acornfox status new-api 和 acornfox logs new-api 看它怎么了

排查顺序：

1. acornfox apps
2. acornfox status new-api
3. acornfox logs new-api

处置：

- ［1 级 可以直接做］acornfox start new-api
- ［2 级 先问用户］装 New API 或配方

- 必须先停下问用户：要多装一个应用时
- 确认恢复：acornfox status new-api 是 running，重装成功

### CAT-INST-11 本机 New API 发不出令牌

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：本机 New API 发不出令牌（登录或令牌接口失败）
- 下一步：看 New API 是否在运行（acornfox status new-api）；管理员密码被改过时改用 --llm own；message 里有 HTTP 429 是 New API 限制了登录和取令牌的次数（同一来源 20 分钟 20 次），等 20 分钟再装，不要反复重试
- 现象：安装进度停在“向本机 New API 要令牌”，返回 CAT-INST-11

可能原因（逐条判定）：

1. New API 没在运行或还没就绪 —— 判定：acornfox status new-api
2. New API 的管理员密码在它自己的后台被改过，AcornFox 记的密码登录不了 —— 判定：acornfox logs new-api 里有登录失败

排查顺序：

1. acornfox status new-api
2. acornfox logs new-api --tail 50

处置：

- ［1 级 可以直接做］等 New API 起来后重装
- ［2 级 先问用户］改用 --llm own

- 必须先停下问用户：改用自己的 Key 时
- 确认恢复：重装成功

### CAT-INST-12 镜像从所有来源都没拿到

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：镜像从所有来源都没拿到
- 下一步：看 attempts；国内网络确认已配镜像加速；稍后重试
- AcornFox 已经自动做的：已依次试过下载网关和各个公共镜像源
- 现象：安装进度停在“下载镜像”，返回 CAT-INST-12，带 attempts

可能原因（逐条判定）：

1. 国内服务器拉 Docker Hub 的镜像，镜像加速没配或都不通 —— 判定：attempts 里每个镜像源都失败；cat /etc/docker/daemon.json
2. 网络暂时不通 —— 判定：稍后重试能过
3. 下载网关没用上（网关不通或时钟不准）［doctor 自动判定］ —— 判定：doctor 的下载网关（SETUP-GW-01）和时钟（SETUP-CLOCK-01）两项

排查顺序：

1. 读 attempts
2. sudo acornfox doctor

处置：

- ［1 级 可以直接做］稍后重装
- ［2 级 先问用户］改 Docker 镜像加速配置

- 必须先停下问用户：要改 Docker 配置时
- 确认恢复：重装成功
- 相关编号：SETUP-GW-01、SETUP-CLOCK-01、pull_failed

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
- 是什么：装好后的初始化调用失败（没收到应答的不会重发，因为它可能已经生效）
- 下一步：看 message；acornfox logs <应用>；这次的应用已清理，可以直接重装
- AcornFox 已经自动做的：这次装的应用已清理
- 现象：安装进度停在初始化那一步，返回 CAT-INST-17

可能原因（逐条判定）：

1. 应用的接口返回了错误 —— 判定：message 里的 HTTP 状态
2. 请求没送到（应用还没准备好） —— 判定：message 说没收到应答

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
3. 数据库出错 —— 判定：journalctl -u acornfox-server -n 50 --no-pager

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
- 是什么：清单用到了这个版本还不支持的写法（多个镜像、自带依赖）
- 下一步：升级 AcornFox
- 现象：安装返回 CAT-INST-23（HTTP 400）

可能原因（逐条判定）：

1. 应用是多容器或带依赖服务的，这个版本还装不了 —— 判定：acornfox catalog info 应用 --json 的组件不止一个

排查顺序：

1. acornfox --json version

处置：

- ［2 级 先问用户］升级 AcornFox

- 必须先停下问用户：升级之前
- 确认恢复：升级后能装
- 相关编号：CAT-INST-29

### CAT-INST-24 还留着以前同名应用的数据卷

- 分组：应用中心安装（CAT-INST）
- 默认等级：P2（功能用不了，尽快处理）
- 是什么：这台机器上还留着以前同名应用的数据卷（删应用时保留了数据），默认不让新装悄悄接上旧数据，所以拒绝
- 下一步：停下问用户：旧数据还要不要。要接着用就加 --reuse-data 重装（页面上是“接上旧数据重装”；删除时保留过参数的，账号密码、改过的平台变量、大模型来源和配置文件都按以前的，这次写了的以这次为准）；不要旧数据就用 --name 换个名字装，或者由用户执行 message 里卷名对应的 docker volume rm 后再装
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
- 是什么：安装任务还在进行，但正在做的那一步用的时间超过了它的卡住门槛（各步骤的正常时长和门槛写在程序里，install status 输出的 timing.step 有 normal_seconds、stuck_after_seconds）。这是提醒，不是失败：任务没有停，到了 AcornFox 自己的时限会判失败并清理（从公共源拉镜像 10 分钟、启动 15 分钟、健康检查按清单的 timeout_s、整次 60 分钟）
- 下一步：先运行 next_command（就是 timing.step.look：下载镜像等步骤是 acornfox doctor --json，启动、健康检查、初始化是 acornfox logs 应用 --tail 200 --json）看原因，只读、不用问用户；把 say 和看到的要点告诉用户，问用户接着等还是不装了；不要自己删除、重装或重启服务
- AcornFox 已经自动做的：安装没有停，到了时限会自己判失败并清理这次建出来的东西
- 现象：acornfox --json install status 任务 --wait 返回 stuck: true，timing.code 是 CAT-INST-30，status 仍是 running；控制台安装进度停在同一步很久

可能原因（逐条判定）：

1. 下载镜像：从公共源拉大镜像，网速慢或镜像站限速（这一步没有 progress） —— 判定：timing.step.name 是 image、steps 里这一步没有 progress；acornfox doctor --json 看镜像源一项
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

## 应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）

### CAT-SYNC-01 程序里没有受信公钥，不同步目录

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

### CAT-PKG-01 没有来源给出和目录相符的应用包

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

- 分组：应用目录同步和读取（CAT-SYNC、CAT-LOAD、CAT-PKG）
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

排查顺序：

1. 对照格式定义改那个字段

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
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-06，写了是哪个引用

可能原因（逐条判定）：

1. {{params.*}}、volumes、endpoints、uses 名字写错或漏声明 —— 判定：lint 输出的引用

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

### CAT-LINT-08 没有唯一一个带健康检查的主端点

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-08

可能原因（逐条判定）：

1. 没有主端点、有多个主端点，或主端点没写健康检查 —— 判定：清单的 endpoints

排查顺序：

1. 留一个 primary 并写 health

处置：

- ［1 级 可以直接做］改清单

- 必须先停下问用户：—（不用问）
- 确认恢复：lint 没有错误

### CAT-LINT-09 配方引用的应用或连接不存在或对不上

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-09

可能原因（逐条判定）：

1. 配方里的应用 id、版本或连接名写错 —— 判定：lint 输出的配方和引用

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

### CAT-LINT-12 目录里没有 apps/ 也没有 recipes/

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

### CAT-LINT-16 引用了不存在的平台变量，或只有大模型参数能用的变量

- 分组：打包、签名、检查目录（维护目录的人用：CAT-LINT、CAT-BUILD、CAT-KEY、CAT-VER）
- 默认等级：P3（提示或用法问题，不急）
- 下一步：acornfox catalog platform-vars 看有哪些；按输出里的文件和字段改清单或配方，再运行 acornfox catalog lint 直到没有错误
- 现象：lint 报 CAT-LINT-16

可能原因（逐条判定）：

1. {{platform.*}} 名字写错，或在非 llm_provider 参数里用了大模型变量 —— 判定：lint 输出的引用

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
