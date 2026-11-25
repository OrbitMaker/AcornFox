# 把 AcornFox 装到 Linux 服务器上，到第一个应用上线（情况甲、乙、丙）

**什么时候读这一册**：用户要安装 AcornFox 到一台 Linux 服务器（就是你所在的这台，或者你经 SSH 管的另一台）；问“支持什么系统”；安装卡住、安装脚本报错、自检（`doctor`）不通过；要改应用访问地址。装在这台 Windows 电脑上读 `install-windows.md`，装在这台 Mac 上读 `install-mac.md`。这一册是整个流程的主线：第 2、7 步在 `connect-server.md`，第 7 步之后打开控制台在 `access.md`，第 8、9 步部署第一个应用在 `deploy.md`，做完回到这里的第 10 步。

支持情况：

| 在哪里运行 | 现状 |
| --- | --- |
| AcornFox 服务装在 Linux 服务器：Ubuntu 24.04、Debian 13（x86_64） | 正式支持，推荐 |
| AcornFox 服务装在 Linux 服务器：Ubuntu 22.04、Debian 12（x86_64） | 支持，已实测安装、自检和部署 |
| AcornFox 服务装在 Linux 服务器，arm64 | 安装脚本允许，但还没实测 |
| Mac 电脑上的命令行（CLI），管理别的服务器 | 支持：经 SSH 管理 Linux 服务器（情况乙；`get.sh … | bash -s -- cli` 只装命令行） |
| Windows 10 22H2 / Windows 11 电脑（64 位 Intel 或 AMD）本机 | 支持：AcornFox 装在 WSL 2 里（和 Linux 服务器上同一个程序），Windows 上多一个转发用的小命令 `acornfox`（情况丁，见 `install-windows.md`） |
| Windows 电脑（ARM 版）、管理别的服务器 | ARM 版暂不支持本机；管理别的 Linux 服务器时所有 `acornfox` 命令经 `ssh` 在服务器上运行（情况丙） |
| Mac 电脑（苹果芯片 M1 起、macOS 26 或更新）本机 | 支持：AcornFox 装在苹果 `container` 开的一台 Debian 虚拟机里（和 Linux 服务器上同一个程序），Mac 上多一个常驻小程序和 `acornfox` 命令（情况戊，见 `install-mac.md`）。Intel 的 Mac、macOS 低于 26 装不了本机，走情况乙 |

Mac 用户想把 AcornFox 装在这台 Mac 上：走情况戊（苹果芯片、macOS 26 起）；Intel 的 Mac 或系统太旧，或者想管理一台 Linux 服务器：准备一台 Linux 服务器，把 AcornFox 装到服务器上，Mac 电脑只装命令行工具（CLI）经 SSH 管理它（情况乙）。**不要在 Mac 上运行 `acornfox install`、`acornfox setup`**（它们是已经关掉的旧的本机模式，会报 `SETUP-MAC-01`）；装在 Mac 本机只用 `install-mac.md` 里的命令。Windows 用户想装在自己这台电脑上走情况丁；想管理一台 Linux 服务器走情况丙（AI 工作台经 `ssh` 在服务器上直接运行 `acornfox`）。

## 最低路径：第一次安装到第一个应用上线

照着做，不需要自己推理。每一步写了“看到什么 → 下一步做什么”。

### 第 1 步：判断用户属于哪种情况

在你自己运行的终端里执行：

```bash
uname -s
```

| 输出 | 情况 | 之后怎么做 |
| --- | --- | --- |
| `Linux`，且用户说“就装在这台” | **甲：AI 工作台就在要装的 Linux 服务器上** | 后面所有“在服务器上执行”的命令直接执行。第 3 步还会核对这台是不是真的服务器 |
| `Linux`，但用户说要装到另一台服务器 | **乙：通过 SSH 装到另一台 Linux 服务器** | 后面“在服务器上执行”的命令，前面加 `ssh 用户@地址`（见第 2 步） |
| `Darwin`（Mac），用户要装在**这台 Mac 上** | **戊：装在这台 Mac 的虚拟机里** | 不走第 2～5 步，直接读 `install-mac.md`；装好后第 6 步以后的命令照常用（`acornfox` 命令在“终端”里运行，登记的服务器叫 `local`） |
| `Darwin`（Mac），用户要管理别的服务器 | **乙** | 同上（`connect-server.md`）。这台 Mac 只当命令行客户端 |
| 你自己不在服务器上，而是用 SSH 或工作台的远程执行功能在服务器上跑命令 | **乙** | 同上。判断看的是“你的命令在哪台机器上执行”，不是用户人在哪 |
| `MINGW…`、`MSYS…`、`CYGWIN…`（Windows 的 Git Bash 等），用户要管理别的服务器 | **丙：AI 工作台在 Windows 电脑上，所有命令经 SSH 在服务器上运行** | 第 2 步连上服务器（`connect-server.md`），之后按入口“五种情况，命令在哪里运行”里情况丙的写法执行 |
| 找不到 `uname`（Windows PowerShell、cmd），用户要管理别的服务器 | **丙** | 本 Skill 的命令都是 bash 写法，请在 Git Bash 里执行；用户没装 Git Bash 就请他先装（Git for Windows） |
| Windows（PowerShell、cmd、Git Bash 都算），用户要装在**这台 Windows 电脑上** | **丁：装在这台电脑的 WSL 里** | 不走第 2～5 步，直接读 `install-windows.md`；装好后第 6 步以后的命令照常用（`acornfox` 命令在 PowerShell 或 cmd 里运行） |

拿不准是甲还是乙，就问用户一句：“要把 AcornFox 装在这台机器上，还是装到另一台服务器？”

用户是 Mac（苹果芯片、macOS 26 起）、想装在这台 Mac 上：走情况戊，不用买服务器。用户是 Intel 的 Mac 或系统太旧，又没有 Linux 服务器：告诉用户“这台 Mac 装不了本机版，AcornFox 要装在一台 Linux 服务器上”，建议买一台云服务器（2 核 2 GB 以上，系统选 Ubuntu 24.04）。**买服务器要花钱，由用户自己决定和操作。** 用户买好后再继续。用户是 Windows、想装在自己这台电脑上：走情况丁，不用买服务器。

### 第 2 步（情况乙、丙）：连上服务器

读 `connect-server.md` 的“第 2 步”：向用户要 SSH 地址、测免密登录、确认服务器指纹、经跳板机连接、配置免密登录。做完回到这里的第 3 步。情况甲、丁、戊没有这一步。后面“在服务器上执行”的命令，情况乙、丙一律在开头加 `ssh 用户@地址`（情况丙每条 `acornfox` 命令的写法见入口）。

### 第 3 步：检查服务器

在服务器上执行（只读，不改任何东西）。**整段原样复制执行，不要删减**：每一行后面都会用到，其中云厂商那一行决定后面用不用厂商内网源。输出第一行是 `sudo=`、最后一行是 `github=`；少了哪行，说明没有整段执行，重跑一次。

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; echo "sudo=root"; elif sudo -n true 2>/dev/null; then S="sudo -n"; echo "sudo=ok"; else S="sudo -n"; echo "sudo=need-password"; fi
. /etc/os-release 2>/dev/null; echo "os=${ID:-unknown} ${VERSION_ID:-}"
echo "arch=$(uname -m)"
echo "user=$(id -un)"
echo "mem_mb=$(free -m | awk '/^Mem:/{print $2}')"
echo "disk_free_gb=$(df -BG --output=avail / | tail -1 | tr -dc 0-9)"
if command -v ufw >/dev/null; then echo "ufw=$($S ufw status 2>/dev/null | head -1)"; else echo "ufw=none"; fi
if command -v acornfox >/dev/null; then echo "acornfox=$(acornfox version 2>/dev/null)"; else echo "acornfox=none"; fi
echo "service=$(systemctl is-active acornfox-server 2>/dev/null)"
echo "init=$(ps -p 1 -o comm= 2>/dev/null)"
if [ -f /.dockerenv ] || [ -f /run/.containerenv ]; then echo "container=yes"; else echo "container=no"; fi
r=""
for u in http://100.100.100.200/latest/meta-data/region-id http://metadata.tencentyun.com/latest/meta-data/placement/region http://100.96.0.96/volcstack/latest/region_id; do
  r=$(curl -fs -m 2 "$u" 2>/dev/null | head -c 40); case "$r" in [a-z][a-z]-*) break ;; *) r="" ;; esac
done
[ -n "$r" ] || r=$(curl -fs -m 2 http://169.254.169.254/openstack/latest/meta_data.json 2>/dev/null | grep -o '"region_id" *: *"[^"]*"' | head -1 | cut -d'"' -f4)
case "$r" in
  cn-hongkong*) m=no ;;
  cn-*) m=yes ;;
  ap-hongkong*|ap-taipei*|ap-singapore*|ap-bangkok*|ap-jakarta*|ap-seoul*|ap-tokyo*|ap-mumbai*|ap-manila*|ap-southeast-*|ap-northeast-*|ap-south-*|na-*|eu-*|sa-*|me-*|af-*|us-*) m=no ;;
  ap-*) m=yes ;;
  *) m=unknown ;;
esac
echo "cloud_region=$r"
echo "mainland=$m"
c=""; for i in 1 2; do c=$(curl -s -m 5 'http://ip-api.com/line/?fields=countryCode'); [ -n "$c" ] && break; done
echo "country=$c"
if curl -s -m 6 -o /dev/null https://github.com; then echo "github=ok"; else echo "github=fail"; fi
EOF
```

按输出逐行判断：

| 行 | 值 | 下一步 |
| --- | --- | --- |
| `init=` / `container=` | `init=` 不是 `systemd`，或 `container=yes` | **这不是一台独立的服务器**（多半是 AI 工作台自己的云端容器），AcornFox 装不了。告诉用户，改按情况乙：向用户要他自己服务器的 SSH 地址，从 `connect-server.md` 的第 2 步做 |
| `os=` | `ubuntu 24.04`、`debian 13`、`ubuntu 22.04`、`debian 12` | 支持，继续 |
| `os=` | 其他 Ubuntu、Debian 版本 | 不在支持列表。建议用户在云厂商控制台把系统换成 Ubuntu 24.04。**停下问用户** |
| `os=` | `alinux`、`opencloudos`、`tencentos`、`centos`、`rocky`、`anolis`、`openeuler`、`kylin`、`uos` 等 | **装不上，不要硬装。** 告诉用户：“这台服务器的系统装不了 AcornFox，请在云厂商控制台‘重装系统’（或‘更换操作系统’），选 Ubuntu 24.04。重装会清空系统盘，请先确认上面没有要保留的数据。”由用户自己操作，换好后从 `connect-server.md` 的第 2 步重来 |
| `arch=` | `x86_64` | 继续 |
| `arch=` | `aarch64` | 能装但没实测过，告诉用户，用户同意再继续 |
| `sudo=` | `root` 或 `ok` | 继续 |
| `sudo=` | `need-password` | 你不能输 sudo 密码。**之后所有要 sudo 的命令**（第 5 步安装、第 6 步自检、第 7 步加组、第 9 步 `ufw`、故障处理里的 `journalctl`、`systemctl restart`、`docker logs`）都交给用户在他自己登录服务器的终端里运行，请他把输出贴回来，你再按表格判断。也可以建议用户给这个账号配置免密 sudo，配不配由用户决定 |
| `mem_mb=` | 小于 1800 | 提醒用户：内存小于 2 GB，只适合跑一两个小应用。用户同意再继续 |
| `disk_free_gb=` | 小于 10 | 提醒用户空间偏小，部署几个应用后可能不够；小于 3 时先请用户清理或扩容 |
| `acornfox=` / `service=` | 有版本号且 `service=active` | **已经装过**，跳过第 4、5 步，直接做第 6 步自检 |
| `ufw=` | `Status: active` | 记下来，第 9 步要用（`ufw=none` 或 `Status: inactive` 不用管） |

### 第 4 步：判断国内还是海外

看第 3 步输出的 `mainland=`、`country=` 和 `github=`，从上往下，第一条符合的就是结果：

| 条件 | 结果 |
| --- | --- |
| `mainland=yes`（云厂商中国大陆地域） | **国内** |
| `mainland=no`（云厂商香港或境外地域） | **海外** |
| `country=CN` | **国内** |
| `country=` 为空，且 `github=fail` | **国内，安装时加 `--china`** |
| 其他（`country=HK`、`SG`、`JP`、`US` 等，或 `country=` 为空但 `github=ok`） | **海外** |

几种典型输出对照（香港算海外）：

| 第 3 步输出 | 结论 | 说明 |
| --- | --- | --- |
| `cloud_region=cn-hangzhou`、`mainland=yes` | 国内 | 阿里云杭州 |
| `cloud_region=ap-guangzhou`、`mainland=yes` | 国内 | 腾讯云广州 |
| `cloud_region=cn-hongkong`、`mainland=no` | 海外 | 阿里云、火山引擎香港 |
| `cloud_region=ap-singapore`、`mainland=no` | 海外 | 腾讯云新加坡 |
| `cloud_region=`（空）、`mainland=unknown`、`country=CN` | 国内 | 其他云厂商或自己的机器，在大陆 |
| `cloud_region=`（空）、`mainland=unknown`、`country=HK` | 海外 | 在香港 |
| `cloud_region=`（空）、`mainland=unknown`、`country=US` | 海外 | |
| `mainland=unknown`、`country=`（空）、`github=fail` | 国内，加 `--china` | 查不到所在地，又连不上 GitHub，按国内处理 |
| `mainland=unknown`、`country=`（空）、`github=ok` | 海外 | 查不到所在地，按海外处理 |

这一步的结论只决定两件事：从 Gitee（国内）还是 GitHub（海外）下载安装脚本；要不要加 `--china`。

**没写 `--china`/`--global`、机器上也没有以前的安装记录时，`get.sh` 会自己当场测速**：同时从 Gitee 和 GitHub 取同一个发行版文件的开头（最多 5 秒），谁快先用谁，屏幕上有一行“测速：Gitee x MB/s、GitHub y MB/s，先用 …”，并把这个判断用 `--china`/`--global` 一路交给 `install.sh`（升级时交给 `upgrade.sh`）。国内常见“GitHub 连得上但只有几十 KB/s”，所以不要再用“连不连得上”判断；上表判断错了也不会装得很慢。你自己写了 `--china`/`--global` 就照你写的，不测速。两边都没测通时屏幕上会写明“都没取到数据”，先用 Gitee、不往下传，安装脚本自己再判断（这时看 `install-progress.md` 的网络问题）。

**下载源由安装脚本自己选，你不用换源，也不要改脚本**：脚本运行时会再判断一次所在网络。在阿里云、腾讯云、火山引擎的服务器上（大陆、境外地域都算），系统软件源和 Docker 安装包先用这家云的内网源；中国大陆的其他服务器对几家国内镜像站当场测快慢（日志里有一行“…下载源实测（N 秒）：…；先用 …”，整个测速最多约 15 秒），用能用而且最快的，并配好 Docker 镜像加速；香港和海外服务器用官方源。某个源连不上或很慢，脚本会自动跳过或排到后面。只有上表写了“加 `--china`”的那种情况要加参数，因为脚本自己也查不到所在地，会按海外处理。

### 第 5 步：安装

#### 5.1 启动安装

安装在后台进行，你每隔一段时间查一次进度，这样不会被 AI 工作台的命令超时打断。在服务器上执行（只选一条，照抄，不要改动）：

国内：

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; else S="sudo -n"; fi
(umask 077; : > ~/acornfox-install.log); chmod 600 ~/acornfox-install.log
echo "started_at=$(date +%s)" >> ~/acornfox-install.log
setsid nohup bash -o pipefail -c "umask 022; curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | $S bash; echo exit=\$?" >> ~/acornfox-install.log 2>&1 < /dev/null &
echo $! > ~/acornfox-install.pid
echo started
EOF
```

海外：

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; else S="sudo -n"; fi
(umask 077; : > ~/acornfox-install.log); chmod 600 ~/acornfox-install.log
echo "started_at=$(date +%s)" >> ~/acornfox-install.log
setsid nohup bash -o pipefail -c "umask 022; curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | $S bash; echo exit=\$?" >> ~/acornfox-install.log 2>&1 < /dev/null &
echo $! > ~/acornfox-install.pid
echo started
EOF
```

（日志文件只给本人看，因为安装成功时最后会打印管理员令牌；安装本身用正常的文件权限运行。）

要给安装脚本加参数（例如第 4 步说要加 `--china`）时，只把其中的 `| $S bash;` 改成 `| $S bash -s -- 参数;`，例如 `| $S bash -s -- --china;`，其他照抄。

#### 5.2 查进度、5.3 看日志、安装出错、卡住

启动安装以后，读 `install-progress.md`：它有“查进度”的命令和怎么判断结果、各阶段要多久、怎么算卡住和卡住了怎么办、看日志的两条命令、安装中途的报错和安装脚本停在故障编号时怎么办。看到 `安装完成` 并且 `exit=0` 才算成功，再回到这里做 5.5 和第 6 步。

#### 5.4 用户自己运行安装（`sudo=need-password` 时）

第 3 步是 `sudo=need-password`（或 `install-progress.md` 的 5.2 看到 `sudo:` 开头的行）时，不要用上面的后台写法，把下面这条（国内用 Gitee 那条）交给用户，请他在自己登录服务器的终端里运行，装完把最后的结果告诉你（**提醒用户不要把“管理员令牌”那一行贴给你**）。告诉用户：通常 3～10 分钟，网络差时可能半小时以上；“安装 Docker”那一步会好几分钟没有新输出，是在下载，不是卡住。用户说看到“安装完成”就去第 6 步；看到“故障编号”就请他把 `[ERROR]` 到“故障编号”那几行贴给你（照 `install-progress.md` 处理）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash
```

要加参数时写成 `… | sudo bash -s -- 参数`，例如 `-s -- --china`、`-s -- --allow-busy-80-443`。参数说明用 `curl -fsSL 地址 | bash -s -- --help` 查看（不需要 root，不改系统）。安装过程中不会询问，`-y` 加不加都行。安装中途失败时可以重跑同一条命令；**已经装好（第 3 步 `service=active`）的服务器不要再跑安装命令**，要升级用 `maintain.md` 里的升级命令。

#### 5.5 核对应用访问地址

安装成功后，先核对脚本最后打印的应用访问地址，再去第 6 步。在服务器上执行（只读，不含令牌）：

```bash
grep -E '应用访问地址主机|未从云厂商元数据取到公网 IP|无法确定服务器地址|应用通过 ' ~/acornfox-install.log | tail -n 4
```

用户自己运行安装（5.4）时，请他把安装结果里“应用通过 http://…:端口 访问”那一行和带 `[WARN]` 的行贴给你。

| 看到 | 下一步 |
| --- | --- |
| `应用访问地址主机: 地址（云厂商元数据）` 或 `（手动指定）`，且地址不是下一行说的内网地址 | 正常，去第 6 步 |
| 地址是内网地址（`10.` 开头；`172.16.` 到 `172.31.` 开头；`192.168.` 开头），或者有 `未从云厂商元数据取到公网 IP` | 问用户：服务器只在家里、公司内网里用，这个地址就是对的，去第 6 步。是云服务器、要从外网打开应用时，向用户要服务器的公网 IP（云控制台实例详情里的“公网 IP”或“弹性公网 IP”），按下文“改应用访问地址”改 |
| `无法确定服务器地址`，或者那一行写的是 `http://服务器IP:端口` | 没取到地址，部署输出的网址会没有主机名。向用户要公网 IP，同上一行改 |

### 第 6 步：自检

第 3 步是 `sudo=need-password` 时，下面这段要 sudo 的命令你运行不了（会得到 `doctor_exit=1`、`edge=missing`，那不是真的故障）。把这段交给用户在自己登录服务器的终端里运行，请他把输出贴回来，你再按下面判断。其他情况在服务器上执行：

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; else S="sudo -n"; fi
$S acornfox doctor --json; echo "doctor_exit=$?"
echo "services=$(systemctl is-active docker acornfox-server acornfox-runner | tr '\n' ' ')"
echo "edge=$($S docker inspect -f '{{.State.Status}}' acornfox-edge 2>/dev/null || echo missing)"
echo "edge_health=$($S docker exec acornfox-edge /usr/bin/afx-edge health 2>&1 | head -n 1)"
EOF
```

全部正常的样子：`doctor` 输出 `"ok":true`、`doctor_exit=0`；`services=active active active`；`edge=running`；`edge_health=正常：…`（入口代理在运行，设置已装上、端口都在听）。服务器上的 AcornFox 比 0.6.0 旧时入口容器是旧的名字，`edge=missing` 不算故障，以 `doctor` 的结果为准。

- `doctor` 里 `"ok":false` 的项目：按它的 `code` 照入口“遇到故障编号怎么办”查；`warn` 项转述给用户即可，不挡路。
- 输出结构：`{"ok":…,"items":[…]}`，每项有 `ok`、`level`（`ok`、`warn`、`fail`）、`text`、`check`，要处理的项另有 `code`；没有 `status` 字段。
- 要看某一项的结果（例如时钟），按 `check` 找，不要按文字找：每一项都带 `check`，是这项检查的编号，通过的项也有（时钟一项是 `SETUP-CLOCK-01`，端口一项是 `SETUP-PORT-01`）。`code` 只出现在要处理的项上。
- 不是 root 的用户（已在 `acornfox-users` 组里）直接运行 `acornfox doctor` 时，下面三条是正常的，不是故障：Docker 一项写“由后台服务访问 Docker……当前用户不需要 Docker 权限”（Docker 由后台服务访问，普通用户本来就不用连 Docker）；`SETUP-PORT-05` 警告“不是 root，看不到是谁占用”（AcornFox 自己的端口已经认出来了，剩下的端口要确认就用 `sudo acornfox doctor --json`）；`SETUP-GW-01` 那一项通过、写“下载网关：当前用户读不到……这一项没查”（v0.5.1 起，普通用户读不到数据目录里的网关设置）：这一项其实**没查**，不能当成“网关正常”，要知道网关通不通就用 `sudo acornfox doctor --json`。
- `services` 里有不是 `active` 的：执行 `sudo journalctl -u acornfox-server -n 50 --no-pager`（以及 `-u acornfox-runner`），把报错转述给用户。
- `edge=` 不是 `running`，或 `edge_health=` 不是 `正常` 开头：执行 `sudo docker logs --tail 50 acornfox-edge`，再看 `doctor` 里“入口代理”那一项的 `code`（`EDGE-01`～`EDGE-04`），照入口“遇到故障编号怎么办”查；多半是端口被占（`SETUP-PORT-01`～`SETUP-PORT-03`），同样用 `acornfox faults 编号` 查。

### 第 7 步：登记服务器

读 `connect-server.md` 的“第 7 步：登记服务器”：情况甲把自己的账号加进 `acornfox-users` 组再登记 `local`，情况乙在用户电脑上装命令行再 `target add`，情况丙在服务器上登记它自己。做完回到这里。

### 第 7 步之后：打开控制台（固定一步）

读 `access.md` 的“第 7 步之后：打开控制台”：登记好以后固定做这一步，不用问用户，让用户在浏览器里看到 AcornFox 控制台（用户说过不要打开就跳过）。

### 第 8、9 步：部署第一个应用、提醒放行端口

读 `deploy.md` 的“第 8 步”和“第 9 步”：用户有自己的项目就部署它，没有就部署一个示例网页验证整条链路；局域网服务器跳过放行端口那一步。做完回到这里的第 10 步。

### 第 10 步：告诉用户结果

照这个格式说，不要附带令牌和密码：

- AcornFox 已装在 `服务器地址`（系统、国内/海外）。
- 自检：通过（或列出 warn 项）。
- 控制台：怎么打开的（局域网地址并提醒设密码，或者浏览器已经经隧道打开）。
- 第一个应用：`网址`（能否打开）。
- 还要用户做的事：安全组放行 18810-18899（以及 80、443）。
- 以后怎么用：在项目目录里说“部署”就行；看网页控制台见 `access.md`。

## 改应用访问地址

部署输出的网址用的是安装时定下的地址（写在 `acornfox-server` 服务的 `--public-host` 参数里）。要改成公网 IP 或域名时：向用户要地址，说明要重启 AcornFox 服务（命令行、控制台断几秒，应用容器不停），用户同意后在服务器上执行（把 `公网IP` 换成用户给的地址；第 3 步是 `sudo=need-password` 时交给用户运行）：

```bash
bash -s <<'EOF'
H=公网IP
if [ "$(id -u)" = 0 ]; then S=""; else S="sudo -n"; fi
U=/etc/systemd/system/acornfox-server.service
$S sed -i -E "/^ExecStart=\/usr\/local\/bin\/acornfox server /{s/ --public-host [^ ]+//g; s/\$/ --public-host $H/}" "$U"
$S systemctl daemon-reload && $S systemctl restart acornfox-server
grep '^ExecStart=' "$U"
systemctl is-active acornfox-server
EOF
```

倒数第二行末尾是 `--public-host 公网IP`、最后一行是 `active`，就改好了；已部署应用的网址也跟着变成新地址（`acornfox status --json` 可以看到）。升级 AcornFox 会保留这个参数。

刚装完、还没部署应用时，也可以加 `-s -- --public-host 公网IP` 把同一条安装命令重跑一遍（会重新走一遍安装流程，比上面这段慢）。

## 可以发挥的地方

- 推荐云服务器配置、地域（只做建议，买不买由用户定）。
- 用户的服务器上已经有宝塔、1Panel、nginx 时，解释共存的取舍：停掉原来的程序，或者加 `-s -- --allow-busy-80-443` 共存。
- 用大白话向用户解释报错和每个阶段在做什么，补充表格里没写到的背景。

## 必须先问用户的

- 买服务器、升级配置、扩容磁盘（花钱，用户自己决定和操作）；系统要重装（会清空系统盘，先确认没有要保留的数据）。
- 系统版本不在支持列表（`INSTALL-OS-02`）：建议换成 Ubuntu 24.04；用户坚持要试，**他同意后**才加 `-s -- --force-os` 重跑。系统是阿里云 Alibaba Cloud Linux、腾讯云 OpenCloudOS、CentOS 等（`INSTALL-OS-01`）：不能装，**停下**，由用户自己重装系统。
- 80、443 被别的程序占了（`INSTALL-PORT-02`，常见：宝塔、1Panel、nginx）：问用户二选一，①停掉原来的程序再重跑（用户同意后才停），②加 `-s -- --allow-busy-80-443` 共存（不能在 AcornFox 里绑域名、开 HTTPS 和公网控制台）。AcornFox 自己要用的端口（`INSTALL-PORT-01`、`INSTALL-PORT-03`）被占：把脚本列出的程序和 PID 转述给用户，问能不能停掉。
- 要装的版本比已装的旧（`INSTALL-DOWN-01`）：AcornFox 只能升级、不能降级，什么都没改，不要换参数重试；用户坚持要装旧版本，只能先卸载并删除数据（`--purge`，不可恢复），必须用户明确同意。
- `sudo=need-password`：你不能输 sudo 密码，要 sudo 的命令交给用户在他自己的终端里运行；内存小于 2 GB、磁盘不够、`aarch64`：告诉用户，用户同意再继续。
- 安装卡住要停下重装：先把阶段、下载速度告诉用户，**用户同意**才停。`SHA256 校验失败` 重跑一次还失败：**停下**告诉用户（文件可能被篡改），不要跳过校验。
- 改应用访问地址（要重启 AcornFox 服务，命令行和控制台断几秒）；改安全组、防火墙见 `deploy.md` 第 9 步；改网络、代理、Docker 配置。
