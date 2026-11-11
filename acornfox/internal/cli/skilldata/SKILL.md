---
name: acornfox
description: 用 AcornFox 在用户自己的 Linux 服务器上安装部署平台并上线应用：判断用户情况和国内外网络、检查系统、一行命令安装、自检、部署第一个应用；之后处理部署失败、数据库、日志、回退和应用生命周期。用户说“安装 AcornFox”“部署”“上线”“发布到服务器”“加个数据库”“看日志”或提到 acornfox 时使用。
---

# AcornFox

AcornFox 把应用（现成镜像、公开 Git 仓库、带 Dockerfile 的项目）部署到用户自己的 Linux 服务器上。服务器上装一个常驻的 `acornfox` 服务，命令行、网页控制台和本 Skill 都通过它操作。

支持情况：

| 在哪里运行 | 现状 |
| --- | --- |
| AcornFox 服务装在 Linux 服务器：Ubuntu 24.04、Debian 13（x86_64） | 正式支持，推荐 |
| AcornFox 服务装在 Linux 服务器：Ubuntu 22.04、Debian 12（x86_64） | 支持，已实测安装、自检和部署 |
| AcornFox 服务装在 Linux 服务器，arm64 | 安装脚本允许，但还没实测 |
| Mac 电脑上的命令行（CLI） | 支持，只当客户端：经 SSH 管理 Linux 服务器（情况乙） |
| Windows 电脑 | 本版本没有 Windows 版程序：所有 `acornfox` 命令经 `ssh` 在服务器上运行（情况丙） |
| AcornFox 服务装在 Mac、Windows 电脑本机 | **这一版不提供**，不要尝试 |

Mac、Windows 用户现在的做法：准备一台 Linux 服务器，把 AcornFox 装到服务器上。Mac 电脑只装命令行工具（CLI），通过 SSH 管理服务器；Windows 电脑上什么都不装，AI 工作台经 `ssh` 在服务器上直接运行 `acornfox`（服务器上安装 AcornFox 时已经装好了命令行）。**不要运行 `acornfox install`、`acornfox setup` 去在 Mac 本机装服务端**，这个流程还没发布。

## 任何时候都要遵守

**必须先停下问用户、得到明确同意的事：**

- 花钱：买服务器、升级配置、扩容磁盘。
- 删除数据：`acornfox delete`、`remove … --volumes`、卸载、`--purge`、`docker system prune`、`docker image prune`、`docker volume prune`、删服务器上的文件、重装系统。
- 改安全组、防火墙（`ufw` 等），停掉别的程序（例如 nginx）。
- 输入密码：SSH 密码、sudo 密码、网盘或云账号密码。**任何密码都不能由你输入**，交给用户在他自己的终端里操作。
- 改服务器级设置：整机资源上限（`acornfox limits`）、开公网控制台、升级 AcornFox。

**可以直接做、不用问的：** 只读的检查和排查，例如 `acornfox status`、`apps`、`logs`、`doctor`、`install status`、`faults`，以及 `df -h`、`du`、`ls`、`free -m`。查清原因再把要用户决定的事给他，不要把“要不要再查一下”也当成选项问用户。

**输出规则：**

- 管理员令牌、登录密码、`password_file` 里的内容，不要在对话里复述。
- 大多数命令加 `--json`，按输出里的 `ok` 判断成败。`--json` 放在命令前后都可以。版本照抄 `acornfox --json version`，自检照抄 `acornfox doctor --json`：这两种写法新旧版本都认。
- 同一个问题按表格处理两次还失败，停下来，把原始输出交给用户。

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
| `Darwin`（Mac） | **乙** | 同上。Mac 只当命令行客户端，不在 Mac 本机装服务端 |
| 你自己不在服务器上，而是用 SSH 或工作台的远程执行功能在服务器上跑命令 | **乙** | 同上。判断看的是“你的命令在哪台机器上执行”，不是用户人在哪 |
| `MINGW…`、`MSYS…`、`CYGWIN…`（Windows 的 Git Bash 等） | **丙：AI 工作台在 Windows 电脑上，所有命令经 SSH 在服务器上运行** | Windows 上不装 CLI（本版本没有 Windows 版）。第 2 步连上服务器，之后按第 2 步末尾“情况丙的写法”执行 |
| 找不到 `uname`（Windows PowerShell、cmd） | **丙** | 本 Skill 的命令都是 bash 写法，请在 Git Bash 里执行；用户没装 Git Bash 就请他先装（Git for Windows） |

拿不准是甲还是乙，就问用户一句：“要把 AcornFox 装在这台机器上，还是装到另一台服务器？”

用户是 Mac、Windows，又没有 Linux 服务器：告诉用户“AcornFox 目前要装在 Linux 服务器上，这一版不提供 Mac、Windows 本机运行”，建议买一台云服务器（2 核 2 GB 以上，系统选 Ubuntu 24.04）。**买服务器要花钱，由用户自己决定和操作。** 用户买好后再继续。

### 第 2 步（情况乙、丙）：连上服务器

1. 向用户要 SSH 地址：`用户@地址`，以及端口（不是 22 时）、私钥路径（不是默认私钥时）。不要猜。
2. 测试能否免密码登录：

```bash
ssh -o BatchMode=yes -o ConnectTimeout=10 用户@地址 'echo ok'
```

端口不是 22 时加 `-p 端口`；用指定私钥时加 `-i 私钥路径`。后面每条 ssh 命令都照样加。

从上往下，第一条符合的就是结果：

| 看到 | 下一步 |
| --- | --- |
| `ok` | 继续第 3 步 |
| `REMOTE HOST IDENTIFICATION HAS CHANGED` | 服务器指纹和这台电脑记下的不一样（重装过系统，或者有人冒充）。**停下**问用户：“这台服务器最近重装过系统或换过机器吗？”用户确认重装过，才执行 `ssh-keygen -R 地址`（端口不是 22 时写 `ssh-keygen -R '[地址]:端口'`），再按下面“确认服务器指纹”做；用户说没有，就不要继续 |
| `Host key verification failed` | 这台电脑还没确认过服务器指纹。按下面“确认服务器指纹”做，做完重新测试。**不要用关闭指纹校验的参数绕过**（`StrictHostKeyChecking=no`、`accept-new`、`UserKnownHostsFile=/dev/null` 都不行） |
| `Permission denied` | 服务器只能用密码登录，要先把用户电脑的 SSH 公钥放到服务器上，按下面“配置免密登录”做 |
| `Connection timed out`、`No route to host`、`Connection refused` | 地址或端口不对，或者安全组没放行 22。请用户核对。服务器只能经跳板机进入时，按下面“经跳板机连接”做 |

**确认服务器指纹**（第一次连这台服务器时都要做一次）。指纹是服务器的“身份证号”，确认一次后存进这台电脑的 `~/.ssh/known_hosts`，以后连接会自动核对。两种做法，默认用做法一：

做法一（推荐）：请用户在**你执行命令的同一台电脑、同一个系统账号**上打开终端，运行（端口不是 22 时加 `-p 端口`）：

```bash
ssh 用户@地址
```

告诉用户：会看到 `ED25519 key fingerprint is SHA256:……` 和 `Are you sure you want to continue connecting (yes/no/[fingerprint])?`。核对指纹（方法见下）后输入 `yes` 回车。之后如果要输密码，可以直接按 Ctrl+C 退出；已经登录进去就输入 `exit` 退出。用户说做完了，你重新执行上面的免密登录测试。

做法二（用户希望你代办，或者你的终端不在用户电脑上时）：

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
ssh-keyscan -T 10 地址 2>/dev/null > ~/.ssh/acornfox-hostkeys.tmp
ssh-keygen -lf ~/.ssh/acornfox-hostkeys.tmp
```

端口不是 22 时写 `ssh-keyscan -T 10 -p 端口 地址`。最后一条会输出几行 `256 SHA256:…… 地址 (ED25519)`。没有任何输出就是地址、端口不对或安全组没放行，回到上表。把这几行原样给用户，请他核对；**用户明确说“一致”或“确认接受”之后**才执行：

```bash
cat ~/.ssh/acornfox-hostkeys.tmp >> ~/.ssh/known_hosts && rm ~/.ssh/acornfox-hostkeys.tmp
```

用户不确认就执行 `rm ~/.ssh/acornfox-hostkeys.tmp`，不要写入。

怎么核对指纹（告诉用户）：在云厂商控制台用“远程连接”（网页终端，不经过 SSH）登录服务器，运行 `for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf "$f"; done`，看 `(ED25519)` 那一行的 `SHA256:……` 和上面的是否一模一样。用户不方便核对、决定直接接受也可以，这由用户决定，你不要替他决定。

**经跳板机连接**（只有用户说服务器要先登录另一台“跳板机”才能进时做）：向用户要跳板机的 `用户@地址`（和端口）、目标服务器的内网地址和登录用户。两台都要能用密钥免密登录。告诉用户你要在他电脑的 `~/.ssh/config` 末尾加一段，用户同意后执行（按实际值替换；端口是 22、用默认私钥时可以删掉 `Port`、`IdentityFile` 两行）：

```bash
mkdir -p ~/.ssh && chmod 700 ~/.ssh
cat >> ~/.ssh/config <<'EOF'

Host acornfox-server
    HostName 目标服务器内网地址
    User 目标服务器用户
    Port 22
    IdentityFile ~/.ssh/id_ed25519
    ProxyJump 跳板机用户@跳板机地址:跳板机端口
EOF
chmod 600 ~/.ssh/config
```

之后本 Skill 里所有的 `用户@地址` 都换成 `acornfox-server`，并且不再加 `-p`、`-i`（已写在配置里）。指纹只能用做法一：请用户运行 `ssh acornfox-server`，会先后问两次（跳板机一次、目标服务器一次），都核对后输入 `yes`。跳板机要允许转发（sshd 的 `AllowTcpForwarding`），看到 `administratively prohibited` 或 `channel 0: open failed` 就是没允许，请用户找跳板机的管理员。

**配置免密登录**（只在上表看到 `Permission denied` 时做）：

1. 在用户电脑上执行 `ls ~/.ssh/id_*.pub`。
2. 没有任何输出（或报 `No such file`）：用户电脑上还没有 SSH 密钥。先告诉用户“我要在你电脑上生成一把 SSH 密钥，不需要输密码”，然后执行 `ssh-keygen -t ed25519 -N "" -f ~/.ssh/id_ed25519`。已经有 `.pub` 文件就跳过这一条，**不要覆盖已有的密钥**。
3. 请用户在自己的终端运行 `ssh-copy-id 用户@地址`（端口不是 22 时加 `-p 端口`），它会让用户输入一次服务器密码。**密码由用户自己输，你不能代输。**
4. 重新执行上面的免密登录测试。

| ssh-copy-id 时看到 | 下一步 |
| --- | --- |
| `No identities found` | 用户电脑上没有密钥，回到第 2 条生成 |
| `Permission denied` | 密码不对，或服务器关了密码登录。请用户到云厂商控制台重置密码，或用控制台的“远程连接”把公钥加进服务器的 `~/.ssh/authorized_keys` |
| Windows 上找不到 `ssh-copy-id` | 请用户在 Git Bash 里运行这条命令（Git Bash 自带） |

后文“在服务器上执行”的命令，情况乙、丙一律在开头加 `ssh 用户@地址`，例如 `bash -s <<'EOF'` 写成 `ssh 用户@地址 bash -s <<'EOF'`。

**情况丙的写法**（AI 工作台在 Windows 上）。Windows 电脑上不装 CLI，服务器是唯一装着 `acornfox` 的地方，所以：

- 第 3～6 步和情况乙一样，在“在服务器上执行”的命令开头加 `ssh 用户@地址`。
- 第 7 步起，本 Skill 里的每一条 `acornfox …` 命令都写成 `ssh 用户@地址 '命令'`，整条命令放在一对单引号里，例如 `ssh 用户@地址 'acornfox --json status'`。每条 ssh 都是新连接，不用包 `sg`。
- 经 ssh 执行时，当前目录是服务器上的家目录。管某个应用的命令加 `--app 应用名`，例如 `ssh 用户@地址 'acornfox --json --app myapp status'`；或者先 `cd` 到应用目录（目录里的 `.acornfox` 记着应用名）。这两种写法新旧版本都认。v0.5 起也可以把应用名直接写在命令后面（`acornfox --json status myapp`），旧版本会把它当成部署编号，报 `not_found` 或“不接受参数”时改用 `--app`。
- 不要在 Windows 上运行 install-cli 命令：本版本没有 Windows 版 CLI，脚本只会说明情况并退出。
- 端口、私钥照第 2 步加在命令名紧后面：`ssh -p 端口 -i 私钥路径 用户@地址 '…'`；`scp` 也一样写在 `scp` 紧后面，但端口参数是大写的 `-P 端口`（`scp -P 端口 -i 私钥路径 -r …`）。

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
| `init=` / `container=` | `init=` 不是 `systemd`，或 `container=yes` | **这不是一台独立的服务器**（多半是 AI 工作台自己的云端容器），AcornFox 装不了。告诉用户，改按情况乙：向用户要他自己服务器的 SSH 地址，从第 2 步做 |
| `os=` | `ubuntu 24.04`、`debian 13`、`ubuntu 22.04`、`debian 12` | 支持，继续 |
| `os=` | 其他 Ubuntu、Debian 版本 | 不在支持列表。建议用户在云厂商控制台把系统换成 Ubuntu 24.04。**停下问用户** |
| `os=` | `alinux`、`opencloudos`、`tencentos`、`centos`、`rocky`、`anolis`、`openeuler`、`kylin`、`uos` 等 | **装不上，不要硬装。** 告诉用户：“这台服务器的系统装不了 AcornFox，请在云厂商控制台‘重装系统’（或‘更换操作系统’），选 Ubuntu 24.04。重装会清空系统盘，请先确认上面没有要保留的数据。”由用户自己操作，换好后从第 2 步重来 |
| `arch=` | `x86_64` | 继续 |
| `arch=` | `aarch64` | 能装但没实测过，告诉用户，用户同意再继续 |
| `sudo=` | `root` 或 `ok` | 继续 |
| `sudo=` | `need-password` | 你不能输 sudo 密码。**之后所有要 sudo 的命令**（第 5 步安装、第 6 步自检、第 7 步加组、第 9 步 `ufw`、故障表里的 `journalctl`、`systemctl restart`、`docker logs`）都交给用户在他自己登录服务器的终端里运行，请他把输出贴回来，你再按表格判断。也可以建议用户给这个账号配置免密 sudo，配不配由用户决定 |
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

**下载源由安装脚本自己选，你不用换源，也不要改脚本**：脚本运行时会再判断一次所在网络。在阿里云、腾讯云、火山引擎的服务器上（大陆、境外地域都算），系统软件源和 Docker 安装包先用这家云的内网源；中国大陆的其他服务器用华为等国内镜像站，并配好 Docker 镜像加速；香港和海外服务器用官方源。某个源连不上，脚本会自动换下一个。只有上表写了“加 `--china`”的那种情况要加参数，因为脚本自己也查不到所在地，会按海外处理。

### 第 5 步：安装

#### 5.1 启动安装

安装在后台进行，你每隔一段时间查一次进度，这样不会被 AI 工作台的命令超时打断。在服务器上执行（只选一条，照抄，不要改动）：

国内：

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; else S="sudo -n"; fi
(umask 077; : > ~/acornfox-install.log); chmod 600 ~/acornfox-install.log
echo "started_at=$(date +%s)" >> ~/acornfox-install.log
setsid nohup bash -o pipefail -c "umask 022; curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install.sh | $S bash; echo exit=\$?" >> ~/acornfox-install.log 2>&1 < /dev/null &
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
setsid nohup bash -o pipefail -c "umask 022; curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install.sh | $S bash; echo exit=\$?" >> ~/acornfox-install.log 2>&1 < /dev/null &
echo $! > ~/acornfox-install.pid
echo started
EOF
```

（日志文件只给本人看，因为安装成功时最后会打印管理员令牌；安装本身用正常的文件权限运行。）

要给安装脚本加参数（例如第 4 步说要加 `--china`）时，只把其中的 `| $S bash;` 改成 `| $S bash -s -- 参数;`，例如 `| $S bash -s -- --china;`，其他照抄。

#### 5.2 查进度

在服务器上执行（只读；只显示关键行，不会显示管理员令牌；每次约 50 秒）：

```bash
bash -s <<'EOF'
sleep 30
L=~/acornfox-install.log
grep -E '\[(ERROR|WARN|SUCCESS)\]|故障编号|安装完成|^exit=|^curl:|^sudo:|^bash:' "$L" | tail -n 20
echo "phase=$(grep -F '[INFO]' "$L" | tail -n 1)"
now=$(date +%s); st=$(sed -n 's/^started_at=//p' "$L" | head -n 1)
echo "elapsed_min=$(( (now - ${st:-$now}) / 60 ))"
echo "idle_min=$(( (now - $(stat -c %Y "$L")) / 60 ))"
if kill -0 "$(cat ~/acornfox-install.pid 2>/dev/null)" 2>/dev/null; then echo "running=yes"; else echo "running=no"; fi
rx() { awk 'NR > 2 { sub(/^[ \t]+/, ""); split($0, a, /[: \t]+/); if (a[1] != "lo") s += a[2] } END { printf "%.0f\n", s }' /proc/net/dev; }
a=$(rx); sleep 20; b=$(rx); echo "download_kbps=$(( (b - a) / 20480 ))"
EOF
```

输出的意思：`phase=` 是安装现在走到哪一步（最后一条 `[INFO]` 行）；`elapsed_min=` 是从启动到现在多少分钟；`idle_min=` 是日志多少分钟没有新内容；`running=` 是安装还在不在运行；`download_kbps=` 是这 20 秒服务器的平均下载速度（KB/s）。

从上往下，第一条符合的就是结果：

| 看到 | 下一步 |
| --- | --- |
| `安装完成` **并且** `exit=0` | 成功，去第 6 步。两样必须都有才算成功 |
| `故障编号: INSTALL-…` | 先执行下面“看故障详情”的命令，再按下文“安装故障编号”处理 |
| `[ERROR]` 但没有故障编号 | 按下文“安装时其他报错”处理 |
| `sudo:` 开头的行（例如 `a password is required`、`a terminal is required`） | 后台安装用不了 sudo。改按第 3 步 `sudo=need-password` 的做法：把下面的安装命令交给用户在自己的终端里运行 |
| 有 `exit=`，但没有 `安装完成`，也没有 `[ERROR]`、`故障编号`、`sudo:`（常伴有 `curl:` 开头的行） | 安装脚本本身没下载下来。执行下面“看最后几行”的命令看原因。先核对用的是不是第 4 步判断对应的地址（国内 Gitee、海外 GitHub）；用对了还失败，就换另一个地址（Gitee、GitHub 互换）再启动一次。两个都失败，把“看最后几行”的输出交给用户 |
| 没有 `exit=`，且 `running=no` | 安装被中途打断了（例如服务器重启、进程被杀）。执行“看最后几行”，然后重新启动安装（5.1，重跑是安全的） |
| 没有 `exit=`，`running=yes` | 还在装。按下面“各阶段要多久”判断是不是正常，正常就再查一次 |

**各阶段要多久**（`phase=` 里出现的字样 → 在哪个阶段）。总时间：云服务器上通常 3～10 分钟；家用宽带、海外网络差、国内服务器连境外时可能 15～40 分钟，大部分时间花在第 3、5 阶段：

| 阶段 | `phase=` 里出现 | 通常 | 网络差时 |
| --- | --- | --- | --- |
| 1 检查系统和端口 | `检测到操作系统`、`检查端口占用` | 几秒 | 几秒 |
| 2 判断网络 | `检测服务器所在网络`、`中国大陆地域`、`非中国大陆`、`最新版本` | 10～30 秒 | 1 分钟 |
| 3 更新软件源 | `更新软件包列表`、`配置 APT 使用镜像`、`已停用境外官方源` | 不到 2 分钟 | 10 分钟以上 |
| 3 附：等系统自动更新 | `软件包列表被其他进程…占用，等待释放` | 新开的云服务器常见 | 最多等 10 分钟，脚本会自己继续 |
| 4 装基础工具 | `安装基础依赖` | 不到 1 分钟 | 几分钟 |
| 5 装 Docker | `安装 Docker`、`使用 Docker 源` | 1～3 分钟 | 10～30 分钟以上。**这一段日志不会更新**，只能看 `download_kbps=` |
| 6 Docker 镜像加速 | `配置 Docker 镜像加速` | 几秒 | 几秒 |
| 7 网关组件 | `下载 Caddy`、`从 GitHub 下载 caddy` | 不到 1 分钟 | 几分钟 |
| 8 AcornFox 程序 | `创建系统账号`、`下载 AcornFox` | 不到 1 分钟 | 几分钟 |
| 9 启动服务 | `创建数据目录`、`初始化数据库`、`生成管理员令牌`、`创建 systemd 服务`、`启动 AcornFox 服务` | 不到 1 分钟 | 最多约 3 分钟（等网关容器） |

`phase=` 是 `开始下载: 地址` 时，表示正在下载这个文件（地址里有 `caddy` 是第 7 阶段，有 `acornfox` 是第 8 阶段），下完会出现 `下载完成: 大小，用时 N 秒`。

第 7 阶段出现 `[WARN] 下载没成功（curl 退出码 …）: …caddy…` 不算失败：脚本会自己换下一个来源，见下文“安装时其他报错”。

`phase=` 里出现 `检测到云厂商`、`中国大陆地域`、`非中国大陆`、`手动指定使用中国镜像源（--china）` 这几句，是脚本对网络的判断，和第 4 步不一样也不用管。

**怎么算卡住**（还没有 `exit=`、`running=yes` 时）：

| 看到 | 判断 | 下一步 |
| --- | --- | --- |
| `download_kbps=` 不小于 5 | 正在下载，只是慢 | 继续等，每隔 1～2 分钟查一次。超过 10 分钟就告诉用户“正在下载，网络较慢”，并说出现在的阶段和速度。偶尔一次掉到 0 是正常的波动 |
| `phase=` 是“软件包列表被其他进程…占用” | 在等系统自动更新 | 继续等，最多 10 分钟 |
| 连续 3 次（每次间隔 1 分钟以上）都是 `idle_min=` 不小于 10 **并且** `download_kbps=` 小于 5 | **卡住** | 按下面“卡住了怎么办”处理 |
| `elapsed_min=` 不小于 60 | 太久了 | 告诉用户已经装了多久、现在在哪个阶段、下载速度，问用户继续等还是停下重装 |

**卡住了怎么办**：

1. 看是哪个程序在跑（只读）：

```bash
ps -eo pid,etimes,args --sort=start_time | grep -E 'apt|dpkg|curl|docker|gpg' | grep -v grep | tail -n 15
```

2. 把 `phase=`、下载速度和上面的输出告诉用户，说明“安装卡在 X 阶段”，**问用户是否停下重装**。常见原因：服务器连不上外网或 DNS 不通；国内服务器用了境外源（第 4 步判断错了）。
3. 用户同意后停止安装（第 3 步是 `sudo=need-password` 时交给用户运行）：

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; else S="sudo -n"; fi
p=$(cat ~/acornfox-install.pid)
kids() { local c; for c in $(ps -eo pid=,ppid= | awk -v p="$1" '$2 == p {print $1}'); do kids "$c"; echo "$c"; done; }
all="$(kids "$p") $p"
$S kill -TERM $all 2>/dev/null; sleep 5; $S kill -KILL $all 2>/dev/null
$S dpkg --configure -a >/dev/null 2>&1
echo stopped
ps -eo pid,args | grep -E '[a]pt-get|[d]pkg|[s]udo.* bash' | sed 's/^/left: /'
EOF
```

   `stopped` 后面没有 `left:` 行就是停干净了。还有 `left:` 行时，把这些行交给用户，请他确认后用 `sudo kill -9 PID` 停掉（PID 是每行第一个数字），再执行 `sudo dpkg --configure -a`。

4. 按原因处理（例如第 4 步判断错了就改用另一条命令，或加 `--china`），再从 5.1 重新启动。重跑是安全的，已经装好的部分会跳过。同一阶段卡住两次，停下来，把 `phase=` 和第 1 条的输出交给用户。

#### 5.3 看日志的两条命令

看故障详情（脚本停下时会在 `[ERROR]` 和 `故障编号` 之间列出占用端口的程序、PID 和处理办法；这一段在生成令牌之前，不含令牌）：

```bash
sed -n '/\[ERROR\]/,/故障编号/p' ~/acornfox-install.log | tail -n 30
```

看最后几行（只在没有 `安装完成` 时用；会去掉令牌那两行）：

```bash
sed -e '/管理员令牌/{N;d;}' ~/acornfox-install.log | tail -n 20
```

#### 5.4 用户自己运行安装（`sudo=need-password` 时）

第 3 步是 `sudo=need-password`（或 5.2 看到 `sudo:` 开头的行）时，不要用上面的后台写法，把下面这条（国内用 Gitee 那条）交给用户，请他在自己登录服务器的终端里运行，装完把最后的结果告诉你（**提醒用户不要把“管理员令牌”那一行贴给你**）。告诉用户：通常 3～10 分钟，网络差时可能半小时以上；“安装 Docker”那一步会好几分钟没有新输出，是在下载，不是卡住。用户说看到“安装完成”就去第 6 步；看到“故障编号”就请他把 `[ERROR]` 到“故障编号”那几行贴给你：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install.sh | sudo bash
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install.sh | sudo bash
```

要加参数时写成 `… | sudo bash -s -- 参数`，例如 `-s -- --china`、`-s -- --allow-busy-80-443`。参数说明用 `curl -fsSL 地址 | bash -s -- --help` 查看（不需要 root，不改系统）。安装过程中不会询问，`-y` 加不加都行。安装中途失败时可以重跑同一条命令；**已经装好（第 3 步 `service=active`）的服务器不要再跑安装命令**，要升级用下文“升级”的命令。

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

第 3 步是 `sudo=need-password` 时，下面这段要 sudo 的命令你运行不了（会得到 `doctor_exit=1`、`caddy=missing`，那不是真的故障）。把这段交给用户在自己登录服务器的终端里运行，请他把输出贴回来，你再按下面判断。其他情况在服务器上执行：

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; else S="sudo -n"; fi
$S acornfox doctor --json; echo "doctor_exit=$?"
echo "services=$(systemctl is-active docker acornfox-server acornfox-runner | tr '\n' ' ')"
echo "caddy=$($S docker inspect -f '{{.State.Status}}' acornfox-caddy 2>/dev/null || echo missing)"
echo "gateway=$(curl -s -o /dev/null -w '%{http_code}' -m 3 http://127.0.0.1:18808/)"
EOF
```

全部正常的样子：`doctor` 输出 `"ok":true`、`doctor_exit=0`；`services=active active active`；`caddy=running`；`gateway=403`（403 是对的，表示网关在运行且拒绝了没带令牌的请求）。

- `doctor` 里 `"ok":false` 的项目：按它的 `code` 查下文“自检故障编号”；`warn` 项转述给用户即可，不挡路。
- 输出结构：`{"ok":…,"items":[…]}`，每项有 `ok`、`level`（`ok`、`warn`、`fail`）、`text`、`check`，要处理的项另有 `code`；没有 `status` 字段。
- 要看某一项的结果（例如时钟），按 `check` 找，不要按文字找：每一项都带 `check`，是这项检查的编号，通过的项也有（时钟一项是 `SETUP-CLOCK-01`，端口一项是 `SETUP-PORT-01`）。`code` 只出现在要处理的项上。
- 不是 root 的用户（已在 `acornfox-users` 组里）直接运行 `acornfox doctor` 时，下面三条是正常的，不是故障：Docker 一项写“由后台服务访问 Docker……当前用户不需要 Docker 权限”（Docker 由后台服务访问，普通用户本来就不用连 Docker）；`SETUP-PORT-05` 警告“不是 root，看不到是谁占用”（AcornFox 自己的端口已经认出来了，剩下的端口要确认就用 `sudo acornfox doctor --json`）；`SETUP-GW-01` 那一项通过、写“下载网关：当前用户读不到……这一项没查”（v0.5.1 起，普通用户读不到数据目录里的网关设置）：这一项其实**没查**，不能当成“网关正常”，要知道网关通不通就用 `sudo acornfox doctor --json`。
- `services` 里有不是 `active` 的：执行 `sudo journalctl -u acornfox-server -n 50 --no-pager`（以及 `-u acornfox-runner`），把报错转述给用户。
- `caddy=` 不是 `running` 或 `gateway=` 不是 `403`：执行 `sudo docker logs --tail 50 acornfox-caddy`，多半是端口被占，按 `SETUP-PORT-01`、`SETUP-PORT-02` 处理。

### 第 7 步：登记服务器

**情况甲**（就在服务器上）。先在服务器上执行：

```bash
bash -s <<'EOF'
if [ "$(id -u)" != 0 ] && ! id -nG | tr ' ' '\n' | grep -qx acornfox-users; then if sudo -n usermod -aG acornfox-users "$(id -un)"; then echo "group=added"; else echo "group=need-password"; fi; else echo "group=ok"; fi
EOF
```

为什么要加组：命令行通过 `/run/acornfox/api.sock` 和后台服务说话，只有 root 和 `acornfox-users` 组的成员能连。安装脚本建好了这个组，但不会把你的账号加进去。

加组之后，在当前这个登录会话里执行 `id` 或 `id -nG` 看不到 `acornfox-users` 是**正常的**：新组只在之后新开的登录会话里生效，不是没加上。要核对有没有加上，执行 `getent group acornfox-users`，看末尾有没有你的账号。当前会话按下面 `group=added` 的写法（`sg` 或重新登录）继续。

- `group=ok`（或你是 root）：执行 `acornfox --json target add local --url unix:///run/acornfox/api.sock`，之后直接用 `acornfox` 命令。
- `group=need-password`：请用户在自己登录服务器的终端里运行 `sudo usermod -aG acornfox-users $(id -un)`（`$(id -un)` 是你登录服务器用的账号，用户登录的账号不同时，把它换成你的账号名），然后按 `group=added` 处理。
- `group=added`：组已经加上，但已经打开的会话（包括你现在这个）还不认新组。本次对话里每条 `acornfox` 命令都要包一层 `sg acornfox-users -c "…"`，例如：

```bash
sg acornfox-users -c "acornfox --json target add local --url unix:///run/acornfox/api.sock"
```

  用 `sg` 包着的命令看到下面的情况时，按表处理：

| 看到 | 下一步 |
| --- | --- |
| `"ok":true` | 正常，后面的 `acornfox` 命令都照这样包 |
| `sg: command not found`、`Password: Invalid password.`、其他以 `sg:` 开头的报错，或者仍然是 `"code":"permission_denied"` | 改用 `sudo -n -u "$(id -un)" acornfox …`（还是你自己的账号，sudo 会重新读取组，配置照样存在你自己的主目录），例如 `sudo -n -u "$(id -un)" acornfox --json target add local --url unix:///run/acornfox/api.sock`。后面的 `acornfox` 命令都照这样写 |
| 改用 `sudo -n -u` 后看到 `a password is required` | 你用不了 sudo。请用户退出、重新登录服务器，再重新打开（或重连）AI 工作台；新会话里直接用 `acornfox`，不用包 |

  不要用 `sudo acornfox …`（不带 `-u`）凑合：那样是以 root 身份运行，登记信息会存到 root 的目录里，之后你自己的账号用不了。

  也可以一开始就请用户退出、重新登录服务器后再打开 AI 工作台，之后就不用包了。

  如果你的每条命令其实都是新开一个 SSH 连接执行的（工作台通过 SSH 远程执行），新连接已经带上新组，不用包 `sg`：先直接试 `acornfox --json target add local --url unix:///run/acornfox/api.sock`，返回 `"ok":true` 就照常用；报 `permission_denied` 再按上面的办法包。

**情况乙**（通过 SSH，用户电脑是 Linux 或 Mac）。

1. 用户的电脑上先要有 CLI。执行 `acornfox --json version`（注意 `--json` 在 `version` 前面）：输出 `"ok":true` 就跳过安装；否则在用户电脑上安装（国内、海外按**用户电脑**所在网络选，拿不准就用第 3 步最后两行的 `curl` 在用户电脑上测一次；不需要 sudo）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install-cli.sh | bash
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install-cli.sh | bash
```

   输出提示安装目录不在 PATH 中时，本次对话里用输出给出的完整路径调用（例如 `~/.local/bin/acornfox`），并告诉用户把提示的那行加进 shell 配置。

2. SSH 用户不是 root 时，执行一次 `ssh 用户@地址 'sudo -n usermod -aG acornfox-users $(id -un)'`。第 3 步是 `sudo=need-password` 时，请用户自己登录服务器运行 `sudo usermod -aG acornfox-users $(id -un)`。加组后经 SSH **新开**的连接立即生效：下一条 `target add` 和之后的每条命令都是新连接，不用重新登录，也不用包 `sg`。已经打开着的 SSH 会话（例如用户自己开着的终端）里执行 `id` 看不到新组是正常的，不代表没加上。
3. 登记（`my-server` 可以换成用户喜欢的名字；端口、私钥按需加；经跳板机的写 `--ssh acornfox-server`，不加 `--port`、`--identity`）：

```bash
acornfox target add my-server --ssh 用户@地址 [--port 端口] [--identity 私钥路径] --json
```

成功输出 `"ok":true`。失败按下文“连接故障”处理；最常见的是 `host_key_unknown`，说明第 2 步的“确认服务器指纹”没做，回去做完再登记。

**情况丙**（Windows，经 SSH 在服务器上运行）。服务器上的 `acornfox` 第 5 步已经装好，Windows 电脑上不装。

1. SSH 用户不是 root 时，执行一次（第 3 步是 `sudo=need-password` 时，请用户自己登录服务器运行 `sudo usermod -aG acornfox-users $(id -un)`）：

```bash
ssh 用户@地址 'sudo -n usermod -aG acornfox-users $(id -un)'
```

2. 在服务器上登记它自己（重复执行没关系）：

```bash
ssh 用户@地址 'acornfox --json target add local --url unix:///run/acornfox/api.sock'
```

成功输出 `"ok":true`，之后的 `acornfox` 命令都按第 2 步末尾“情况丙的写法”执行。看到 `"code":"permission_denied"`：第 1 条加组没做成，回去做；加组后新开的 ssh 连接立即生效，不用重新登录，也不用包 `sg`。

### 第 8 步：部署第一个应用

用户有自己的项目（目录里有 Dockerfile）时，在项目目录里执行 `acornfox deploy --json`；没有 Dockerfile 的先按下文“Dockerfile 要点”写一个。情况丙的项目在 Windows 电脑上，要先传到服务器，见下面“情况丙：部署”。

用户没有指定项目时，部署一个示例网页验证整条链路。在一个新建的空目录里执行（部署后当前目录会生成 `.acornfox` 记住应用名，所以不要在用户的项目目录里跑这条）：

```bash
mkdir -p ~/acornfox-hello && cd ~/acornfox-hello && acornfox deploy --json --no-wait --image nginx:1.27-alpine --port 80 --app hello
```

输出里有 `deployment_id`。之后每隔 5 秒执行一次，直到 `status` 是 `live` 或 `failed`（第一次部署要拉镜像，通常 1～5 分钟）：

```bash
acornfox --json status DEPLOYMENT_ID
```

用 `--no-wait` 是因为不等部署做完就返回，不受 AI 工作台单条命令超时的限制。工作台允许单条命令跑 20 分钟以上时，也可以去掉 `--no-wait` 直接等结果，见下文“等部署结果的写法”。

情况甲且第 7 步是 `group=added` 时，`acornfox deploy …` 那一段按第 7 步定下的写法来：包进 `sg acornfox-users -c "…"`，或者写成 `sudo -n -u "$(id -un)" acornfox deploy …`。

**情况丙：部署**（每条都在 Windows 的 Git Bash 里执行，命令都经 ssh 在服务器上运行）。

示例网页：

```bash
ssh 用户@地址 'mkdir -p ~/acornfox-hello && cd ~/acornfox-hello && acornfox deploy --json --no-wait --image nginx:1.27-alpine --port 80 --app hello'
```

```bash
ssh 用户@地址 'acornfox --json status DEPLOYMENT_ID'
```

第二条每隔 5 秒执行一次，直到 `status` 是 `live` 或 `failed`（和上面一样）。`live` 之后取网址：这一条输出里没有 `url`，按应用名查应用状态，用输出里的 `url`：

```bash
ssh 用户@地址 'acornfox --json --app hello status'
```

现成镜像、公开 Git 仓库不用传文件，在服务器上建一个目录直接部署，例如 `ssh 用户@地址 'mkdir -p ~/acornfox-apps/myapp && cd ~/acornfox-apps/myapp && acornfox deploy --json --no-wait --git 仓库地址 --app myapp'`。

用户电脑上文件夹里的项目：先把文件夹传到服务器，再在服务器上那个目录里部署。`项目文件夹` 写 Git Bash 里的路径，例如 Windows 的 `C:\Users\me\myapp` 写成 `/c/Users/me/myapp`；传上去后服务器上的目录名和文件夹名相同（下面的 `myapp`）。

```bash
ssh 用户@地址 'mkdir -p ~/acornfox-apps'
scp -r 项目文件夹 用户@地址:acornfox-apps/
ssh 用户@地址 'cd ~/acornfox-apps/myapp && acornfox deploy --json --no-wait'
```

之后同上：`ssh 用户@地址 'acornfox --json status DEPLOYMENT_ID'` 查到 `live`，再用 `ssh 用户@地址 'acornfox --json --app myapp status'` 取 `url`。

- 容器监听的端口不是 Dockerfile 里 `EXPOSE` 的那个，或者没写 `EXPOSE` 时，在 `deploy` 后面加 `--port 端口`。端口怎么定：nginx、httpd 镜像是 80；Node 看代码里 `listen(` 的端口；Python 看启动命令里的 `--port`；判断不了就问用户，不要猜。
- 文件夹很大（有 `node_modules`、`.git`、构建产物）时，`scp` 会很慢，改用这一条代替上面的 `scp`（`--exclude` 按项目增减）：

```bash
tar -czf - --exclude=node_modules --exclude=.git -C 项目文件夹 . | ssh 用户@地址 'mkdir -p ~/acornfox-apps/myapp && tar -xzf - -C ~/acornfox-apps/myapp'
```

- 改了代码要重新部署：再执行一次传文件的命令和 `deploy`。电脑上删掉的文件，服务器上那份不会跟着删；影响构建时告诉用户。
- 项目在私有 Git 仓库里：不要把用户的 Git 密码或令牌放到服务器上，改用上面传文件夹的做法。

- 成功：`status` 是 `live`。网址用 `acornfox --json status` 输出的 `url`，记下来。
- 失败：`status` 是 `failed`，按输出里的 `diagnosis.code` 查下文“部署诊断码”。

验证能打开：

- 情况乙、丙：在用户电脑上执行 `curl -s -o /dev/null -w '%{http_code}\n' -m 10 网址`。`200` 表示外网能打开；`000` 表示被安全组或防火墙挡住了，去第 9 步。
- 情况甲：在服务器上执行 `curl -s -o /dev/null -w '%{http_code}\n' -m 5 http://127.0.0.1:端口`（端口是 `url` 末尾的数字）。`200` 表示服务正常；外网能不能打开要用户在浏览器里试，打不开就是第 9 步的问题。

### 第 9 步：提醒放行端口

告诉用户（原样转述即可）：

> 应用网址是 `http://服务器地址:端口`，端口从 18810 开始分配（18810–18899）。请到云厂商控制台，在这台服务器的**安全组**里添加入方向规则：TCP 18810-18899，来源 0.0.0.0/0。以后要绑定域名、开 HTTPS，还要放行 TCP 80 和 443。

第 3 步看到 `ufw=Status: active` 时还要说：服务器自带防火墙也开着，需要放行同样的端口；用户同意后执行 `sudo ufw allow 18810:18899/tcp`（要绑域名再加 `sudo ufw allow 80/tcp`、`sudo ufw allow 443/tcp`）。

**安全组、防火墙由用户决定。** 即使你能调用云厂商的命令行，也要先问用户。

### 第 10 步：告诉用户结果

照这个格式说，不要附带令牌和密码：

- AcornFox 已装在 `服务器地址`（系统、国内/海外）。
- 自检：通过（或列出 warn 项）。
- 第一个应用：`网址`（能否打开）。
- 还要用户做的事：安全组放行 18810-18899（以及 80、443）。
- 以后怎么用：在项目目录里说“部署”就行；看网页控制台见下文“网页控制台”。

## 遇到故障编号怎么办（v0.5.1 起）

故障编号出现在：安装、升级脚本最后一行的 `故障编号: …`；命令出错时不加 `--json` 的输出里 `故障编号：…` 那一行（v0.5.1 起；不是故障的通用错误码写成 `错误码：…`）；`acornfox doctor` 每项后面括号里的；命令加 `--json` 时输出里的 `code`、`diagnosis.code`、`gateway_code`；控制台“运行环境”页上的。每个编号的完整说明编在程序里，用 `acornfox faults` 查，不用记。

最低路径（照做，不用推理）：

1. 运行 `acornfox faults 编号 --json`。它只读、不连服务器，Mac 上的命令行也能用；情况丙在服务器上运行（`ssh 用户@服务器 acornfox faults 编号 --json`）。大写编号不分大小写，小写的诊断码（例如 `build_failed`）照原样写。
2. 返回 `"ok": false`、`diagnosis.code` 是 `fault_unknown`（退出码 1）：目录里没有这个编号（接口和命令的通用错误码，例如 `invalid_app`、`not_found`、`internal`，也不在目录里）。按返回里 `fallback` 那一条做：把出错那条输出里的 `message` 和 `hint` 原样告诉用户（照抄原文、加引号，不要缩写或改写；用户只报了编号、手上没有那条输出时，请他把出错那条命令加 `--json` 的完整输出发来）；`hint` 只是让这条命令换个参数或稍后重试的，可以照做；别的一律只读、不动任何东西，收集证据交给用户。到此为止。
3. 先告诉用户这是什么问题（`fault.title`，有 `fault.explain` 就一起说）和下一步（`fault.next`）。`fault.auto` 不为空时，告诉用户 AcornFox 已经自动做了什么。
4. 按 `fault.steps` 的顺序运行只读的检查，对照 `fault.causes` 里每条的 `check` 判断是哪个原因；`auto` 为 true 的原因，`acornfox doctor --json` 已经替你判断了。
5. 按 `fault.actions` 处置：`tier` 为 0、1 的可以直接做；`tier` 为 2 的，先把要做的事和后果告诉用户，用户明确同意后才做，同时告诉用户做完后怎么确认（照 `fault.verify` 说）。`fault.ask` 写到的情况一律先停下问用户；`fault.needs_user` 为 true 的只能用户自己做。
6. 做完按 `fault.verify` 确认。确认不了，或者同一个编号按步骤做了两次还在，停下，把 `acornfox faults --json` 返回的 `evidence` 列的证据交给用户。

可以发挥的地方：用大白话解释原因；处置有好几条时，按判断出的原因选；把用户的话和 `causes` 对上。`fault.steps` 之外的只读检查（`df`、`du`、`ls`、看日志）可以直接做，查清原因再把选项给用户，不用先问。不能跳过 `tier` 2 和 `ask` 的确认，`next` 里写了“不要”的事不能做。

`acornfox faults` 不带编号列出全部，`acornfox faults --group SETUP` 只列一组。电脑上的命令行太旧、没有 `faults` 命令时，按下面各节的表处理。

## 安装故障编号

安装脚本在改动系统之前先检查，不满足时打印 `故障编号: 编号` 并停下，什么都没改。升级脚本也用这些编号；`UPGRADE-` 开头的编号只在升级时出现，见“升级、卸载”一节。

| 编号 | 意思 | 处理 |
| --- | --- | --- |
| `INSTALL-ROOT-01` | 没用 root 或 sudo | 命令里加 `sudo`（`… \| sudo bash`）；没有 sudo 权限请用户换 root 账号 |
| `INSTALL-OS-01` | 系统不是 Ubuntu/Debian（阿里云 Alibaba Cloud Linux、腾讯云 OpenCloudOS/TencentOS、火山 veLinux、CentOS、Rocky 等） | 不能装。转述第 3 步“重装系统”那段话，**停下**，由用户自己重装 |
| `INSTALL-OS-02` | Ubuntu/Debian 版本不在支持列表 | 建议换成 Ubuntu 24.04。用户坚持要试，**他同意后**加 `-s -- --force-os` 重跑 |
| `INSTALL-ARCH-01` | CPU 架构不是 x86_64、arm64 | 不能装，请用户换服务器 |
| `INSTALL-DISK-01` | 数据盘剩余不到 2 GB | 请用户清理或扩容（扩容花钱，用户决定），然后重跑 |
| `INSTALL-PORT-01` | AcornFox 自己要用的 18800、18801、18808 被别的程序占了 | 把脚本列出的程序和 PID 转述给用户，**问用户**能不能停掉或改端口；处理后重跑 |
| `INSTALL-PORT-02` | 80 或 443 被别的程序占了（常见：宝塔、1Panel、nginx） | 问用户二选一：①停掉原来的程序（用户同意后才停）再重跑；②共存：加 `-s -- --allow-busy-80-443` 重跑，应用照常用 `IP:端口` 访问，但不能在 AcornFox 里绑域名、开 HTTPS 和公网控制台，域名继续在原来的面板里反向代理到 `127.0.0.1:应用端口` |
| `INSTALL-PORT-03` | 应用端口段 18810-18899 里有端口被占 | 转述占用的程序，**问用户**能不能停掉或改端口；处理后重跑 |
| `INSTALL-CLOUD-01` | `--cloud` 或 `ACORNFOX_CLOUD_OVERRIDE` 的值不认识 | 这是测试参数，去掉后重跑 |
| `INSTALL-DOWN-01` | 服务器上已经装着更新的 AcornFox，或者留着卸载时保留的、更新版本的数据，要装的版本比它旧。AcornFox 只能升级，不能降级 | 什么都没改。不要换参数重试。要换新版本按“升级、卸载”做升级；用户坚持要装旧版本，只能先卸载并删除数据（`--purge`，不可恢复），这一步必须用户明确同意 |

### 安装时其他报错

这些报错发生在安装中途。`[ERROR]` 那几行处理后重跑同一条安装命令；`[WARN]` 开头的按表处理，多数不用重跑。

| 报错包含 | 处理 |
| --- | --- |
| `数据库初始化失败` | 执行 `grep -B3 '数据库初始化失败' ~/acornfox-install.log`，把这几行交给用户。不要删数据库文件 |
| `更新软件包列表失败`、`安装基础依赖失败` | 网络或软件源的问题。等 1 分钟重跑；还失败执行 `sudo apt-get update` 把输出交给用户 |
| `dpkg was interrupted`、`dpkg --configure -a` | 上次安装被中途停下。执行 `sudo dpkg --configure -a`，再重跑 |
| `Docker 安装失败` | 重跑；还失败请用户先自己装好 Docker，再加 `-s -- --skip-docker` 重跑 |
| `无法获取最新版本号`、`下载 AcornFox 失败`、`下载 Caddy 失败` | 下载不通。国内服务器检查是不是用了 Gitee 地址；判断错了国内外时加 `-s -- --china`（或 `--global`）重跑 |
| `[WARN] 下载没成功（curl 退出码 …）`，地址里有 `caddy`（常见退出码 28，表示超时或太慢） | **正常，不用处理，也不要停下安装**。脚本会接着换下一个来源：中国大陆依次是 Gitee、GitHub 直连、两个 GitHub 加速地址，海外是 GitHub 直连；都慢时最后不限速再试一次。之后出现 `Caddy 镜像已就绪`（或 `Caddy 镜像已存在`）就算这一步成功。所有来源都失败才会报 `下载 Caddy 失败`，按上一行处理 |
| `[WARN] Caddy 官方校验文件取不到，改用 Gitee 镜像上的副本` | 正常，安装包仍按 Caddy 官方的 SHA-512 校验，不用处理 |
| `[WARN] 制作 Caddy 镜像失败` | 安装会继续，服务启动时改从镜像仓库拉 Caddy 镜像。装完做第 6 步自检，`caddy=running` 就是好了 |
| `SHA256 校验失败`、`SHA-512 校验失败` | 重跑一次；还失败**停下**告诉用户（下载的文件可能被篡改），不要跳过校验 |
| `服务启动失败`、`生成管理员令牌失败` | `sudo journalctl -u acornfox-server -n 50 --no-pager`、`sudo journalctl -u acornfox-runner -n 50 --no-pager`，把输出交给用户 |
| `[WARN] Caddy 容器还没就绪` | 做第 6 步自检，按结果处理 |
| `[WARN] /etc/docker/daemon.json 已存在，未自动修改` | 服务器上原来就有 Docker 配置，脚本没加镜像加速；国内拉镜像可能慢，转述给用户 |
| `[WARN] 未从云厂商元数据取到公网 IP`、`[WARN] 无法确定服务器地址` | 部署输出的网址会用脚本猜的本机地址（常是内网地址），或者没有主机名。按 5.5 核对，要改就按下面“改应用访问地址” |

#### 改应用访问地址

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

## 自检故障编号（`acornfox doctor --json`）

| 编号 | 意思 | 处理 |
| --- | --- | --- |
| `SETUP-SVC-01`（fail） | AcornFox 后台服务没有应答 | `sudo systemctl restart acornfox-runner acornfox-server`，等 5 秒再做第 6 步自检；还不行就执行 `sudo journalctl -u acornfox-server -n 50 --no-pager`，把输出交给用户 |
| `SETUP-SVC-01`（warn） | 没有权限连接后台服务，看不出它在不在运行 | 不是故障：用 `sudo acornfox doctor --json` 再查一次 |
| `SETUP-RT-03` | 连不上 Docker | `sudo systemctl status docker --no-pager`；没在运行就 `sudo systemctl start docker` 后再自检。不是 root 时记得用 `sudo acornfox doctor --json`（普通用户连不上 Docker、但后台服务正常时，doctor 不报这一项，而是写“当前用户不需要 Docker 权限”，那是对的） |
| `SETUP-DISK-01` | 磁盘剩余不到 10 GB（warn）或不到 3 GB（fail） | `sudo docker system df` 看占用。清理（`docker system prune`）会删掉停掉的容器和没用的镜像，**先问用户**；还不够请用户扩容 |
| `SETUP-PORT-01` | 80 或 443 被别的程序占用 | 安装时用了 `--allow-busy-80-443`、用户选择共存的，这一项是预期的，告诉用户“绑域名和 HTTPS 用不了”即可。否则**问用户**能否停掉占用的程序，停掉后 `sudo systemctl restart acornfox-server` |
| `SETUP-PORT-02` | 网关管理口 18808 被占用 | 按输出的 PID 确认是什么程序，**问用户**后停掉，再 `sudo systemctl restart acornfox-server` |
| `SETUP-PORT-03` | 应用端口段里有端口被占用 | 分到这些端口的应用会上线失败。**问用户**能否停掉占用的程序 |
| `SETUP-PORT-04`（warn） | 局域网控制台端口 18802 被占用 | 不开局域网控制台就可以忽略 |
| `SETUP-PORT-05`（warn） | 不是 root，看不到端口是谁占的（root 运行时：分不清是不是 AcornFox 自己的） | 普通用户运行时出现是正常的，不挡路；要确认就用 `sudo acornfox doctor --json` 再查一次 |
| `SETUP-EDGE-01` | 网关组件（Caddy）没准备好 | `sudo docker logs --tail 50 acornfox-caddy`；`sudo systemctl restart acornfox-server` 后再自检 |
| `SETUP-CHK-02` | Docker 不能创建和删除容器 | 多半是磁盘满了，按 `SETUP-DISK-01` 处理 |
| `SETUP-CLOCK-01` | 本机时钟不准，或者没开网络对时 | **先问用户**，再运行 `sudo timedatectl set-ntp true`；过一两分钟 `timedatectl` 看 `System clock synchronized: yes`，再 `acornfox doctor`。时钟差太多时下载网关会拒绝这台机器（`GW-AUTH-01`），应用中心的大镜像会改从公共源拉、慢很多 |

表里没有的编号（例如 `SETUP-GW-01` 下载网关连不上、`SETUP-SYNC-01` 应用目录没同步上）：按上面“遇到故障编号怎么办”运行 `acornfox faults 编号 --json`；命令行太旧没有这个命令时，把那一项的 `text` 原样转述给用户，照里面的建议做。涉及删除、停程序、花钱的先问用户。

## 连接故障（`diagnosis.stage` 为 `connect`）

| code | 处理 |
| --- | --- |
| `ssh_missing` | 用户电脑上没有 ssh 命令，请用户装 OpenSSH 客户端 |
| `host_unreachable` | 地址、端口不对，或安全组没放行 22，请用户核对 |
| `auth_failed` | 免密登录不通：回到第 2 步 |
| `host_key_unknown` | 这台电脑还没确认过服务器指纹：按第 2 步“确认服务器指纹”做，做完重新执行刚才的命令 |
| `host_key_changed` | 服务器指纹变了（重装过系统，或者有人冒充）。**停下**问用户是不是重装过；用户确认重装过，才按第 2 步表格里 `REMOTE HOST IDENTIFICATION HAS CHANGED` 那一行处理，否则不要动 known_hosts |
| `acornfox_missing` | 服务器上没装 AcornFox，回到第 3 步 |
| `permission_denied` | 账号不在 `acornfox-users` 组：按第 7 步加组。情况乙、丙加完组后直接重试；情况甲按第 7 步用 `sg`、`sudo -n -u` 或重新登录 |
| `server_down`、`local_server_down` | 服务没在运行：做第 6 步自检 |
| `version_mismatch` | 电脑上的 CLI 和服务器版本对不上：重新运行第 7 步的 install-cli 命令升级 CLI；服务器旧就按“升级”处理（情况丙没有电脑上的 CLI，不会出现） |
| `forwarding_disabled`、`connect_failed` | 转述 `message` 和 `hint` |

## 部署

情况丙：本节和后面各节的 `acornfox` 命令都按第 2 步末尾“情况丙的写法”经 ssh 执行，要部署的项目先按第 8 步“情况丙：部署”传到服务器。

```bash
acornfox deploy --json                      # 部署当前目录（需要根目录有 Dockerfile）
acornfox deploy --json --port 3000          # 指定容器监听端口
acornfox deploy --json --git URL [--ref 分支或提交]
acornfox deploy --json --image nginx:1.27-alpine --port 80 --app my-app
```

`deploy` 默认等到终态才返回（最长 20 分钟，可用 `--timeout 30m` 调整）：

- 成功：`{"ok":true,"url":"http://地址:端口",...}`，把 `url` 告诉用户。
- 失败：`{"ok":false,"diagnosis":{"stage","code","message","log_excerpt","hint"}}`，按下文“部署诊断码”修复后重新部署。

### 等部署结果的写法

`deploy` 要构建或拉镜像，常常要几分钟，最长等 20 分钟。按下面的顺序选一种写法：

1. **首选：先提交、不等，再自己查进度**（不受工作台单条命令超时的限制）。

   ```bash
   acornfox deploy --json --no-wait …          # 立刻返回，记下输出里的 deployment_id
   acornfox --json status DEPLOYMENT_ID        # 每隔 5 秒查一次，直到 status 是 live 或 failed
   ```

   `status` 为 `live`：成功，网址用 `acornfox --json status` 的 `url`。为 `failed`：按输出里的 `diagnosis.code` 查下文“部署诊断码”。为 `superseded`：被更新的部署取代了，用 `acornfox --json status` 看现在的版本。其他值（`queued`、`building`、`starting`、`checking`、`routing`）表示还在进行，继续等。

2. **工作台允许单条命令跑 20 分钟以上时**，可以前台运行、等它自己返回：

   ```bash
   acornfox deploy --json …; echo "exit=$?"
   ```

   `exit=0` 且输出 `"ok":true` 才算成功。

3. **必须放后台时**，只用这一种写法：

   ```bash
   nohup acornfox deploy --json … > out.json 2> err.log < /dev/null &
   wait $!; echo "exit=$?"
   ```

   启动后立刻得到的 `$?` 只表示“已经放到后台”，**不是部署结果**。要等进程结束再读 `out.json`：同一个 shell 里用 `wait $!`；换了一条命令再查时，等 `out.json` 里出现以 `{` 开头的一行（`grep -q '^{' out.json && cat out.json`）才算结束，没出现就过几秒再查。

- **不要**用 `setsid -f acornfox deploy …`、`sg acornfox-users -c "acornfox deploy … &"` 这类立刻返回的包法：命令马上结束，拿不到结果。要用 `sg` 时，每条命令各包一次：`sg acornfox-users -c "cd ~/acornfox-hello && acornfox deploy --json --no-wait …"`，再 `sg acornfox-users -c "acornfox --json status DEPLOYMENT_ID"`。
- 拿到 `"diagnosis":{"code":"cancelled",…}`：只是**等待**被打断了（例如命令超时被工作台结束），部署还在服务器上继续。按 `hint` 运行 `acornfox --json status DEPLOYMENT_ID` 查结果，**不要重新部署**。`code` 为 `timeout` 时同样处理。

用户说“重新部署”“再部署一次”时：本地代码或 Dockerfile 有改动就用 `deploy` 上传新代码；没有改动、只是想让环境变量或设置生效，才用 `redeploy`（它只重建当前版本，不上传本地代码）。拿不准先问用户。`redeploy` 和 `deploy` 一样默认等到终态；`--no-wait` 只返回 `deployment_id`，之后用 `acornfox --json status DEPLOYMENT_ID` 查进度。

首次部署后当前目录会生成 `.acornfox`（只记服务器名和应用名，不含凭据），之后在这个目录里的命令不用再指定 `--app`。

## 状态与诊断

```bash
acornfox status --json                 # 应用状态：url、observed_state、current_deployment
acornfox --app 应用名 status --json    # 指定应用（v0.5 起也可写 status 应用名；写 12 位部署编号时看那一次部署的进度）
acornfox apps --json                   # 这台服务器上的所有应用
acornfox diagnose --json               # 本机最近一次部署的状态、事件和诊断
acornfox diagnose DEPLOYMENT_ID --json # 指定部署
acornfox logs --json [--tail 200]      # 容器日志
acornfox versions --json --app APP                    # 版本记录：来源、用时、失败原因、能否回退
acornfox logs --json --app APP --version N [--build]  # 某一版的运行日志或构建日志
```

`versions` 的每一版有 `status_text`、`source_text`、`duration_seconds`、`failure_text`、`can_rollback`、`rollback_text`，直接转述给用户，不要自己换算；顶层 `rollback_target` 是不指定版本时会回到的版本：除正在服务的版本外，最新一个还能一键回退的版本，不一定是“当前版本减一”；为 0 表示没有可回退的，告诉用户只能重新部署代码。部署失败时线上仍是原来那一版（`status` 为 `live` 的那条）。

部署状态：`queued` → `building` → `starting` → `checking` → `routing` → `live`；失败为 `failed`，被新提交取代为 `superseded`。

## 部署诊断码

| code | 处理 |
| --- | --- |
| `dockerfile_missing` | 按项目类型生成 Dockerfile |
| `too_large` | 补 `.dockerignore`，排除 node_modules、构建产物、大文件 |
| `build_failed` | 读 `log_excerpt`，修依赖或构建命令 |
| `registry_timeout` | 构建时下载依赖超时：服务器在国内时，Dockerfile 里改用国内镜像源（见下文 Dockerfile 要点）后重新部署 |
| `pull_failed` / `pull_timeout` / `image_not_found` | 基础镜像拉不下来：核对镜像名和标签；服务器在国内、又是 `/etc/docker/daemon.json` 原来就有的情况，请用户自己配置 Docker 镜像加速 |
| `port_not_listening` | 程序要监听 `0.0.0.0`（不是 127.0.0.1），端口与 `--port` 一致 |
| `container_exited` / `start_failed` | 看 `log_excerpt` 和 `acornfox logs`，修启动命令或缺失的环境变量 |
| `out_of_memory` | `acornfox app set --memory 1024 --json`，立即生效不用重新部署；如果是部署时就内存不够，改完再重新部署 |
| `memory_cap_exceeded` | 整机分配给应用的内存不够了：先 `acornfox limits --json` 看各应用占用，调小用不完的应用（`app set --memory`），或请用户确认后调大整机上限 `acornfox limits --memory 26G --json` |
| `memory_below_usage` | 内存上限不能低于应用当前实际用量，换一个更大的值 |
| `unpersisted_database`（警告） | 数据写在容器里会丢：`acornfox volume add /app/data --json` |
| `clone_failed` / `clone_timeout` / `repo_not_found` / `ref_not_found` | 核对 Git 地址和分支，仓库必须公开 |
| `context_too_large` | 项目包解压后超过 1 GB：补 `.dockerignore`，排除依赖目录、构建产物和大文件 |
| `runner_response_too_large` | 构建输出过大：减少构建时打印的内容（例如去掉逐个文件的输出）后重新部署 |
| `cancelled` | 只是等待被打断，部署在服务器上继续：按 `hint` 运行 `acornfox --json status DEPLOYMENT_ID` 查结果，不要重新部署（见上文“等部署结果的写法”） |
| `timeout` | 等了 `--timeout` 还没到终态：同样按 `hint` 查 `status`，不要重新部署；常常是构建或拉镜像慢 |
| `image_gone` | 回退要用的那一版程序已不在服务器上：`acornfox versions --json` 换一个 `can_rollback` 为 true 的版本，或重新部署那一版的代码 |

`log_excerpt` 只有最后 40 行。不够判断原因时，用 `acornfox logs --json --app APP --version N --build` 看那一版完整的构建日志，或 `acornfox logs --json --app APP --version N` 看它的运行日志（默认最后 500 行，`--tail` 最多 2000；正在服务的版本最多 1000 行）。N 是失败的版本号，在 `diagnose` 或 `versions` 里。

部署成功、`url` 在服务器上能打开（`curl` 返回 200）但用户的浏览器打不开：几乎都是安全组没放行，按第 9 步提醒用户。

## 配置

```bash
acornfox env set KEY=VALUE [--secret] --json   # 密钥值永不回显
acornfox env unset KEY --json
acornfox env list --json
acornfox volume add /app/data --json           # 绝对路径，容器重建后数据保留
acornfox volume list --json
acornfox app set --port 3000 --memory 512 --health-path /healthz --json
```

改环境变量、数据卷、端口或健康检查路径后执行 `acornfox redeploy --json` 生效。

## 内存与 CPU 上限

```bash
acornfox app set --memory 768 --cpu 1.5 --json   # 内存至少 256 MB，CPU 至少 0.5 核；立即生效，应用不重启
acornfox limits --json                           # 整机最多分配给应用多少、各应用和数据库各占多少
acornfox limits --memory 26G --cpu 8 --json      # 改整机上限；auto 恢复自动（改之前先问用户）
```

- 所有应用和数据库的内存上限加起来不能超过整机上限；新建应用（默认 256 MB）、加数据库、调大内存时都会检查。整机不到 2 GB 时，上限是整机内存的 2 倍（允许超分）。
- CPU 只要求单个应用的上限不超过整机上限，总和可以超过。

## 数据库与缓存

```bash
acornfox add postgres --json   # 也可以是 mysql、redis
acornfox addons --json
acornfox remove postgres --json [--volumes]
```

添加后会自动重新部署并注入连接变量：postgres、mysql 为 `DATABASE_URL`，redis 为 `REDIS_URL`。代码里从环境变量读取连接串，不要写死。`remove … --volumes` 会删数据，先问用户。

## 从应用中心装应用

应用中心的目录里是预先配好的应用（现在有 New API 模型网关、Hermes 智能体）和配方（几个应用一起装好并连起来）。从目录装不用写 Dockerfile，也不用填密码：密码和密钥由 AcornFox 生成，装好后给出首次登录信息。情况丙照样经 ssh 在服务器上运行这些命令。

**最低路径（照着做）：**

1. 看目录：`acornfox --json catalog list`。`apps[].id` 是应用名，`recipes[].id` 是配方名，`apps[].installed` 不为空表示这台服务器已经装过。应用或配方的 `needs_platform` 不为空（v0.5.1 起才有这一项）表示要先升级 AcornFox 才能装，先不要提交安装，按下面“要先升级 AcornFox 才能装的应用”做。同步结果是 `CAT-SYNC-08`（在线目录都取不到）时，用的是程序自带的目录，照样能装，不用处理。
2. 看要填什么：`acornfox --json catalog info 应用`。`params` 里每一项是能填的参数；`generated: true` 的留空会自动生成；`llm` 这一项说明三种大模型来源现在能不能用。`platform` 是平台按这台机器自动填的（时区、下载源、默认模型等），**不用填**。
3. 提交安装，不等：

   ```bash
   acornfox --json install new-api --no-wait                  # 记下输出里的 task_id；next_command 就是第 4 步要运行的命令
   acornfox --json install hermes-agent --llm local --no-wait  # Hermes 接本机 New API（先装好 New API）
   acornfox --json install --recipe hermes-with-new-api --no-wait   # 两个一起装并连起来
   ```

4. 等它装完：`acornfox --json install status TASK_ID --wait`（v0.5.1 起）。它最多等 90 秒，装好或失败就提前返回。看输出最外层这几项，照着做，不用自己判断：
   - `done` 是 `false`：还没装完，结果还不知道。只把 `say` 告诉用户（它写了到了哪一步、这一步和整次各用了多久），然后运行 `next_command`（还是这条命令）接着等，直到 `done` 是 `true`。没到之前**不要**对用户说“卡住了”“装不好”“容器没在运行”“没有令牌”这类结论，也不要自己估计还要多久：时间只照 `say` 里的说，不要说“几分钟”。“启动”一步做完之前，`acornfox status 应用名` 里 `observed_state` 是 `missing`、`current_deployment` 为空，这是正常的，不用另外去查。
   - `stuck` 是 `true`（`timing.code` 是 `CAT-INST-30`）：正在做的那一步比平常慢很多，但安装还没失败。先运行 `next_command`（看日志或自检，只读，不用问用户），把 `say` 和看到的要点告诉用户，再问用户接着等还是不装了；详细步骤按“遇到故障编号怎么办”查 `CAT-INST-30`。不要自己删除、重装或重启服务。`slow` 是 `true` 只是比平常慢一些，照常接着等。
   - `done` 是 `true`：`status` 是 `succeeded` 做第 5 步，`failed` 做第 6 步。
   - 命令报不认识 `--wait`（服务器上的命令行是 v0.5.0）：去掉 `--wait`，每隔 5 秒查一次 `acornfox --json install status TASK_ID`，直到 `status` 是 `succeeded` 或 `failed`；中间同样不下结论。

   `steps` 里是每一步的进度，`timing.steps` 是每一步用了多久、正常多久（`normal_seconds`）。“下载镜像”这一步写着“没有 linux/arm64 的镜像包”“只算 linux/amd64 的”（v0.5.1 起）时，是这台服务器的 CPU 架构在下载网关上没有镜像包，改从公共源拉，会慢一些，不是故障，照常等。
5. 成功：把 `first_login` 告诉用户，再按下面“装好后怎么打开”告诉用户怎么打开。密码默认是 `******`，**不要在对话里复述密码**；告诉用户自己在服务器上运行 `acornfox install status TASK_ID --show-secrets` 查看，或在控制台的应用详情里看。再把 `user_steps`（还要用户做的几步）逐条告诉用户。`must_tell`（v0.5.1 起）里的每一句都要照原话告诉用户，一句都不能漏（没能吊销的旧令牌、不限额不过期的令牌）；应用名、令牌名、令牌结尾照抄，不要换成别的叫法（例如不要把 `new-api` 写成 New API）。任务的 `apps[]` 里带 `gateway_token` 的应用（例如接了本机 New API 的 Hermes）用的是 New API 里一把**不限额、不过期**的令牌，要告诉用户：要控制花费，可在 New API 后台的“令牌”页给它设上限。`gateway_token` 里只有编号和 `provider`，令牌名不在输出里，固定是 `acornfox-应用名`（例如 `acornfox-hermes-agent`）。
6. 失败：看 `fault.code`，按下表处理。失败时这次建出来的应用和数据卷已经自动清理（只清理这次新建的，以前留下的数据不会动），处理好原因后重新安装即可。加了 `--keep-data-on-failure` 时数据卷会留着，任务的 `apps[].volumes` 列出卷名。

### 装好后怎么打开（只本机的应用）

从应用中心装的应用默认只本机能打开（`exposure: local`）：`apps[].url` 是 `http://127.0.0.1:端口`，只在服务器自己身上能用，用户在自己电脑上点不开。**不要**让用户去安全组或防火墙放行端口（应用只听本机地址，放行了也打不开）。

**最低路径（照着做）：**

1. 看对外方式：`acornfox --json apps`。找到这个应用：`local_only: true`（`exposure: local`）才需要往下做；`lan`、`public` 的直接把 `url` 告诉用户。
2. 看 `open_hint`：
   - `recommend` 是 `lan`（服务器在用户的局域网里，应用有自己的登录）：**先问用户**“要不要让同一局域网里的电脑都能打开它？这会在服务器的所有网卡上开放这个端口，服务器如果还有公网地址、安全组又放行了这个端口，公网也能访问到；打开后要用应用自己的账号密码登录”。用户事先已经同意过，执行前也要把这段提醒原样说一遍。用户同意才运行 `acornfox --json app set --app 应用名 --expose lan`，然后把 `open_hint.lan_url` 告诉用户（就是局域网地址加端口）。用户不同意就做第 3 条。
   - `recommend` 是 `tunnel`（公网云服务器，或应用没有自己的登录）：做第 3 条，不提改对外方式。
3. 让用户在**他自己的电脑**上运行 `acornfox open 应用名`（`open_hint.command` 就是这条命令）。它像 `acornfox open` 打开控制台一样开一条 SSH 隧道，自动打开浏览器，看完按 Ctrl+C 关闭。这要求用户电脑上装了 CLI 并按第 7 步用 `--ssh` 登记了服务器（情况乙）。
   - 情况甲、丙（用户电脑上没有 CLI）：请用户在自己电脑的终端运行 `ssh -N -L 端口:127.0.0.1:端口 用户@地址`（端口是 `url` 里的端口）并保持不关，然后在浏览器打开 `http://127.0.0.1:端口`。

`acornfox open 应用名` 出错时：

| 编号 | 怎么办 |
| --- | --- |
| `OPEN-APP-01` | 应用还没有访问端口：`acornfox --json status 应用名` 看部署状态，部署成功后再打开 |
| `OPEN-APP-02` | 这台服务器是用网址（局域网或公网控制台）登记的，开不了隧道：按第 7 步用 `--ssh` 再登记一次；或者先问用户，同意后改成局域网 |
| `diagnosis.stage` 是 `connect` | SSH 连不上或不让转发，按“连接故障”处理 |

**必须先停下问用户的：**

- 用自己的大模型 Key（`--llm own`）：要 `base_url`、`api_key`、`model` 三项，用 `--set llm.base_url=… --set llm.api_key=… --set llm.model=…` 传。分两种情况：
  - 用户已经在对话里给了 Key：可以照用。回答里不要复述 Key（写成 `sk-…`），不要写进任何文件；告诉用户这条命令会留在服务器的命令历史和 sudo 日志里。这把 Key 是用户在上游（Key 的提供方）那里的，要作废就去那里换，不在 New API 里。
  - 用户没给：不要向他要，也不要替他编。把整条安装命令给用户（Key 那里写成 `sk-你的Key`），请他自己在服务器终端运行，再把输出里的 `task_id` 告诉你，你接着按第 4 步等。
- 对外开放：应用默认只本机能打开（`exposure: local`）。New API 管着上游 Key，Hermes 的控制台能执行命令，要开放到局域网或公网（`acornfox app set --expose …`、绑定域名）必须用户明确同意，并提醒走 HTTPS、用强密码。用户只是想自己看一眼时，优先用 `acornfox open 应用名`，不用改对外方式。New API 装好时平台已替用户关掉自助注册（只有管理员账号能登录）；如果安装任务 `init` 步骤的说明里写着“没能关闭自助注册”，开放前先请用户登录 New API，在“系统设置”里关掉“允许新用户注册”。
- 花钱：在 New API 里加上游渠道会产生模型费用，由用户自己在 New API 页面里加。
- 内存不够（`CAT-INST-05`）：调小别的应用或调大整机上限都要用户同意。
- 删掉同名的旧应用（`CAT-INST-03`）：会删数据，先问。
- 应用要更新版本的 AcornFox 才能装（目录里 `needs_platform` 不为空，或安装报错里带 `needs_platform`：`CAT-INST-08` 一定带，`CAT-INST-29` 有的带）：升级 AcornFox 要用户同意，见下面“要先升级 AcornFox 才能装的应用”。
- 以前同名应用留下的数据卷（`CAT-INST-24`）：先问用户旧数据还要不要。要接着用旧数据，就在同一条安装命令后面加 `--reuse-data` 重装（账号密码和以前一样；少数情况平台没留下以前的记录，首次登录会写“沿用旧数据里的”，让用户用自己记得的旧密码）；不要旧数据就用 `--name 新名字` 装，或者确定不要了由用户自己运行 `message` 里卷名对应的 `docker volume rm` 后再装。你不要替用户删。接上旧数据时这次没写的设置按以前的来，见下面“换应用版本、删了再接上旧数据重装”。**对外方式会回到只本机**：以前开放过局域网的，装好后先告诉用户，问要不要照旧开放（提醒照“装好后怎么打开”第 2 条说），同意后再改。
- 用户要改平台自动填的值（例如时区、下载源）：先 `acornfox --json catalog platform-vars` 看 `user_can_set` 是不是 `true`，是才加 `--set platform.名字=值`（只对这次装的应用生效）。`false` 的（网络区域、访问地址、平台 Key）不能改，不要试。
- 连数据一起删掉（`--volumes`）用了本机 New API 的应用（例如 Hermes）时，AcornFox 会顺带吊销给它发的令牌；返回里有 `warnings`（New API 没在跑，没吊销成）时，告诉用户去 New API 的“令牌”页删掉 `acornfox-应用名`。只删应用、保留数据时（v0.5.1 起），令牌跟着数据留下，以后接上旧数据重装时接着用，不用去 New API 里删。

**常见故障编号（完整列表和含义以 `acornfox/internal/appinstall/faults.go` 为准，服务器回的 `hint` 就是处理方法）：**

| 编号 | 意思 | 怎么办 |
| --- | --- | --- |
| `CAT-INST-01` | 目录里没有这个应用、配方或版本 | `acornfox --json catalog list` 看有哪些 |
| `CAT-INST-02` | 参数不对 | 按 `message` 改 `--set`；`acornfox --json catalog info 应用` 看参数。接上旧数据重装时 `message` 说“原来用本机的 … 现在本机没有它了”：停下问用户用哪个大模型（`--llm local` 本机在运行的 New API，或 `--llm own` 自己的 Key），再重新提交 |
| `CAT-INST-03` | 已有同名应用 | `--name 新名字`；要重装先问用户再删旧的 |
| `CAT-INST-04` | 同一个应用正在装 | 不要重复提交；用错误里的 `task_id` 查进度 |
| `CAT-INST-05` | 内存不够 | 问用户：调小别的应用或调大整机上限 |
| `CAT-INST-08` | AcornFox 版本太旧，这个应用要更新的版本才能装 | 停下问用户是否升级 AcornFox（见下面“要先升级 AcornFox 才能装的应用”） |
| `CAT-INST-09` | 平台 Key 即将开放 | 改用 `--llm local`（本机 New API）或 `--llm own`（问用户要 Key） |
| `CAT-INST-10` | 要用的本机 New API 没装、停着或没在运行 | 看 `message`：说“装着但停着”的，**停下问用户**要不要启动它，同意后 `acornfox --json start 网关名`：它会等到运行才返回，输出里 `running: true` 就是已经在运行，直接用原来的命令重新提交；`running` 不是 `true`，或者报 `start_timeout`，再 `acornfox --json status 网关名` 看原因、告诉用户。用户不想启动就问他要不要改用 `--llm own`（自己的 Key）。这种情况**不要** `install new-api`（同名的已经装着，会报 `CAT-INST-03`）。说“容器却没起来”的，`acornfox --json status 网关名` 看它怎么了，把原因告诉用户。本机还没有 New API 时，才先 `acornfox --json install new-api`，或装配方 `hermes-with-new-api` |
| `CAT-INST-11` | 本机 New API 发不出令牌 | `acornfox --json status new-api` 看它在不在跑；管理员密码改过就改用 `--llm own`；`message` 里有 `HTTP 429` 是 New API 限制了登录和取令牌的次数（同一来源 20 分钟 20 次）：告诉用户等 20 分钟再装，不要反复重试 |
| `CAT-INST-12` | 镜像拉不到 | 稍后重试；国内网络看 `sudo acornfox doctor --json` 的“下载网关”一项（不加 sudo 时这一项不查） |
| `CAT-INST-13` | 下载网关给的镜像包和目录里的校验值对不上（没导入） | 会自动换公共源；不要手动绕过 |
| `CAT-INST-15` | 没能上线 | 看 `fault.diagnosis`，按“部署诊断码”处理；`acornfox logs 应用` |
| `CAT-INST-16` | 健康检查不过 | `acornfox logs 应用` 看日志；内存太小时问用户调大 |
| `CAT-INST-17` | 初始化失败 | 看 `message`；这次的应用已清理，重新安装 |
| `CAT-INST-18` | 安装途中服务重启 | 重新安装 |
| `CAT-INST-24` | 以前同名应用的数据卷还在 | 停下问用户（见上面“必须先停下问用户的”） |
| `CAT-INST-25`、`CAT-INST-26` | 和私有目录有关（这一版私有目录还没开放） | 不要自己想办法绕过，把原始输出交给用户 |
| `CAT-INST-27` | 磁盘不够（`message` 里有要多少、剩多少） | 先运行 `acornfox faults CAT-INST-27 --json`，按 `steps` 只读查清是什么占了空间（`df`、`du` 直接做，不用问）。删掉不用的应用、镜像或找出来的大文件，或者扩容磁盘，都先告诉用户是什么、多大，用户确认后才做；你不要替用户删。清出来的空间够了就再装一次，装成功才算好；`doctor` 的磁盘一项剩余不到 10 GB 时仍是警告，不影响这次安装 |
| `CAT-INST-28` | 改平台变量不对（没有这个变量、不能改、格式不对） | 去掉那个 `--set platform.…`；`acornfox --json catalog platform-vars` 看哪些能改 |
| `CAT-INST-29`（v0.5.1 起） | 这个应用里有这个版本的 AcornFox 不认识的写法 | 看错误里有没有 `needs_platform`（`--json` 输出的 `diagnosis.needs_platform`）：有，就和 `CAT-INST-08` 一样处理，它的值是要升到的版本；没有，是目录有问题，不要升级，把 `message` 原话告诉用户，过一段时间再试。不要按 `hint` 里有没有“升级”两个字判断（两种情况的 `hint` 是同一句） |
| `CAT-INST-30`（v0.5.1 起） | 安装还在进行，正在做的那一步比平常慢很多（`install status` 输出 `stuck: true`），还没失败 | 运行输出里的 `next_command`（只读）看原因，把 `say` 和要点告诉用户，问接着等还是不装；不要删除、重装、重启服务 |

**可以发挥的地方：** 用户只说“想要一个能接飞书的 AI 助手”时，可以推荐配方 `hermes-with-new-api` 并解释两步（装好后在 New API 里加上游渠道）。这个配方固定让 Hermes 用本机 New API，要用自己的 Key 就单独装 `hermes-agent --llm own`（配方里给 `--llm own` 会报 `CAT-INST-02`）。New API 没加渠道时，Hermes 调模型会返回 503“没有可用渠道”，这是正常的，提醒用户去加渠道。

### 换应用版本、删了再接上旧数据重装

这一版没有“把已装的应用升级到目录里的新版本”的功能。用户要换应用的版本（或者删掉应用后又想装回来），做法是删掉应用、保留数据，再用原来的安装命令加 `--reuse-data` 装回来。

**最低路径（照着做）：**

1. 删除前先 `acornfox --json status 应用名` 看一眼它现在的状态。和你之前告诉用户的对不上时（例如你说过“卡住”，它其实已经在运行），先把实际情况告诉用户，再问还要不要删。删除要先问用户（会停掉应用）。同意后 `acornfox --json delete 应用名`（**不加** `--volumes`，数据卷保留）。Hermes 还连着 New API 时，要换 New API 先删 Hermes。这个应用用的是本机 New API（例如 Hermes 接本机 New API）时，删除前再 `acornfox --json status new-api` 看一眼：它停着的，先告诉用户并问要不要启动，启动好了再删，免得删了以后装不回来、应用一直不在。
2. 只是删了装回原来的版本：跳过这一步。要换新版本的，`acornfox --json catalog sync` 后 `acornfox --json catalog info 应用` 看目录里的版本。
3. 用原来的安装命令加 `--reuse-data`，不等：`acornfox --json install 应用 --reuse-data --no-wait`（装的时候用了 `--name` 的，用同一个名字）。**不用**把以前的 `--set`、`--llm` 再写一遍：没写的都按以前的来。用户这次明确要改的才写上。
4. 照“从应用中心装应用”第 4 步等它装完（`acornfox --json install status TASK_ID --wait`），直到 `done` 是 `true`。没到之前什么结论都不下，旧令牌的事也一样（见第 5 步）。
5. 成功后先照“从应用中心装应用”第 5 步做（`first_login`、`user_steps`、`must_tell`，再按“装好后怎么打开”从第 1 步 `acornfox --json apps` 做起），然后把任务 `apps[]` 里这几项告诉用户（v0.5.1 起才有；没有这几项是服务器版本旧，设置可能没沿用，照实说）：
   - `carried`：沿用了什么，一条一句，照原话转述（例如“沿用以前改过的平台变量：platform.tz=Asia/Tokyo”“大模型沿用原来的：本机的 new-api”“接着用以前在 new-api 里给它的令牌”）。
   - `kept_files`：数据里原有、没有重写的配置文件（用户和应用自己改过的内容都还在）。
   - `file_backups`：因为平台要填的内容变了（`steps` 里“写配置”一步的 `message` 写着变了什么）而重写的配置文件，`backup` 是旧文件另存的名字（在应用的数据卷里）。告诉用户：以前自己改过的内容在这个旧文件里，需要的话照着抄回去；你不要替用户进容器改。
   - `carried` 里以“没能吊销以前在 … 里给 … 的令牌”开头的一句：**必须原话告诉用户**。那是以前给这个应用的一把不限额、不过期的令牌，New API 当时停着（或连不上）没吊销成，平台以后不会再试；要用户在 New API 运行时到它的“令牌”页按这句话里说的名字和结尾删掉。你不要替用户删。
   - 换了大模型来源（例如这次加了 `--llm own`）时，原来那把令牌怎么样了**只看 `carried`**（v0.5.1 起这句也在 `must_tell` 里）：有“……已吊销”那句才说已吊销；有“没能吊销……”那句就原话转述；任务还没到 `succeeded` 时只说“装好后会告诉你旧令牌的处理结果”。吊销在装好之后、“初始化”一步里才做，结果到 `succeeded` 时才出现。**不要**因为这次 `apps[]` 里没有 `gateway_token`，就说“没有 New API 令牌”“不用去 New API 里管”。
6. 对外方式回到只本机：照“装好后怎么打开”做（先 `acornfox --json apps` 看 `open_hint`）；以前开放过局域网的，先告诉用户，问要不要照旧开放（见上面“必须先停下问用户的”里 `CAT-INST-24` 那条）。

沿用的规矩（判断用，不用念给用户）：

| 设置 | 这次没写 | 这次写了 |
| --- | --- | --- |
| 账号密码、平台生成的密钥 | 和以前一样 | 以这次为准 |
| 以前 `--set platform.名字=值` 改过的平台变量 | 恢复以前改的值。v0.5.0 装的应用分不出来，`carried` 里会说“要这次再写一遍”：问用户以前改过哪些，再加 `--set` 重装 | 以这次为准 |
| 大模型来源 | 自己的 Key 照旧；本机 New API 还在运行就接着用，令牌也接着用原来那把；那个 New API 停着报 `CAT-INST-10`（见上表，问用户后启动它）；本机没有它了报 `CAT-INST-02`（见上表） | 以这次为准：这次选的还是原来那个本机 New API（`--llm local`）时接着用原来那把令牌；换了来源（例如 `--llm own`）时，原来那把在装好后吊销（New API 那时停着就吊销不了，`carried` 里会说）；吊销成没成只看装好后的 `carried`，不要自己推断（见上面第 5 步） |
| 数据里已有的配置文件 | 平台要填的内容没变就不重写；变了（换了大模型、原来的令牌没了、新版本改了模板）就重写，旧文件先另存为 `文件名.bak-时间`；上一次接旧数据没装好、已经改写过的，这次也重写（旧文件再另存一份） | 同左 |

**可以发挥的地方：** 新版本能不能读旧数据由应用自己决定；用户的数据很重要时，建议先让用户自己备份数据卷再换版本。

### 要先升级 AcornFox 才能装的应用（v0.5.1 起）

目录里有的应用或配方要更新版本的 AcornFox 才能装。v0.5.1 起，目录列表会提前标出来；更早的版本没有这个标记，要到安装时才报 `CAT-INST-08`。

**最低路径（照着做）：**

1. 认出来（符合任意一条）：
   - `acornfox --json catalog list` 或 `acornfox --json catalog info 应用` 里，这个应用或配方的 `needs_platform` 不为空。它的值就是要升到的版本，例如 `"0.6.0"`。不带 `--json` 时显示“要先升级到 0.6.0”。
   - 安装返回 `CAT-INST-08`（v0.5.1 起错误里带 `needs_platform`，就是要升到的版本；在 `--json` 输出的 `diagnosis.needs_platform`）。
   - 安装返回 `CAT-INST-29`，并且错误里带 `needs_platform`（`diagnosis.needs_platform` 不为空）。不带的是目录有问题，不算。
2. 不提交安装；已经报错的，不要换参数、换版本号重试。
3. 告诉用户：“这个应用需要更新版本的 AcornFox（要 0.6.0，这台服务器现在是 …）”。现在的版本用 `acornfox --json version` 看。
4. **停下问用户**要不要升级 AcornFox。升级是改服务器级设置，必须用户同意；同意后按“升级、卸载”一节的命令和规矩做（国内、海外二选一，不加 `-y`），不要另找命令。
5. 升级完，重新运行 `acornfox --json catalog list`，确认这个应用的 `needs_platform` 已经是空的，再回到上面的最低路径安装。

**几种情况：**

- 升级后 `needs_platform` 还不为空：最新版的 AcornFox 也还不够新，这个应用要等下一个版本。告诉用户，不要反复升级。
- `CAT-INST-29` 的错误里不带 `needs_platform`：是目录有问题，不是版本的事，升级没用。把原话告诉用户，过一段时间再试；一直不行，请用户反馈给维护者。
- 用户不同意升级：不装这个应用。可以问用户要不要换一个不用升级的（`needs_platform` 为空的）。

### 查一个应用是用什么设置装的（v0.5.1 起）

什么时候用：用户问“这个应用的密码、地址、时区、模型是怎么设的”“当时填的是哪个 Key”，或者排查应用配置（例如怀疑时区、下载源、模型地址不对）时。只对从应用中心装的应用有用。只读，不改任何东西。v0.5.1 之前没有这个命令（报用法错误，退出码 2），告诉用户要 v0.5.1 起才有，不要自己去翻容器。

**最低路径（照着做）：**

1. 运行。应用名必须写，这个命令不会按当前目录猜（`acornfox --json apps` 看有哪些）：

   ```bash
   acornfox --json apps vars 应用名
   ```

2. 先看三处：
   - `from_catalog` 是 `false`：不是从应用中心装的，没有这些变量。把 `notes` 告诉用户；它的环境变量用 `acornfox env list --app 应用名 --json` 看。到此为止。
   - `manifest.source`：`install`（安装时保存的清单）、`backfill`（升级后补存的）都照常往下看；`manifest.saved` 是 `false` 见下面“清单没存”。
   - `notes`：给人看的说明，转述给用户。
3. 读 `vars`，每个变量一行，按用户问的找对应的行。判断用英文取值，说给用户听用同名的 `*_label`（中文）：

   | 列 | 意思 |
   | --- | --- |
   | `name` | `params.名字` 是这个应用自己的参数；`platform.名字` 是平台按这台机器给的（`platform.tz` 时区、`platform.app.url` 访问地址、`platform.llm.model.chat` 默认对话模型、`platform.mirror.pypi` 和 `platform.mirror.npm` 下载源、`platform.region` 网络区域）。大模型参数拆成 `params.名字.source`、`.app`、`.base_url`、`.api_key`、`.model` 几行 |
   | `layer` | 哪一层：`custom` 自定义变量（应用自己的），`system` 系统变量（平台给的） |
   | `class` | 类别：`system` 系统给、`generated` 平台生成、`default` 有默认值、`optional` 可选、`llm_source` 要选模型来源、`user_required` 必须用户填。空表示清单没存，分不出 |
   | `value` | 安装时保存的值。`masked: true` 表示被遮住了 |
   | `secret` | 是不是密码、密钥、Key 这一类 |
   | `origin` | 值从哪来：`user` 用户填的、`recipe` 配方预填的、`default` 清单默认值、`generated` 平台生成的、`platform` 平台算的、`platform_user` 用户安装时用 `--set platform.名字=值` 改的、`restored` 接上旧数据时恢复的、`install_time` 装时的值（旧版本装的，那时不记是谁给的）、`unset` 没填、`old_data` 沿用旧数据里的（平台不知道，`value` 是空） |
   | `editable` | 清单写的能不能改：`never` 不能改、`always` 随时能改、`reinstall` 重装才能改、`install_only` 只能在安装时用 `--set platform.名字=值` 改；空是清单没写或没存。**这只是清单的说法，现在哪一种都还改不了**（见下面） |
   | `used` | 只有系统变量有：这个应用用没用到它。`false` 表示这个值对这个应用不起作用 |

4. 只回答用户问的那几项，不用把整张表念一遍。密码、密钥、Key 默认只显示后 4 位（`******` 加后 4 位；不到 12 个字符的全遮成 `******`）。说到它们时只说“有值、后 4 位是什么、谁给的”。

**必须先停下问用户的：**

- 看完整的密码和 Key（`--reveal`）：**先问用户**，同意了才运行 `acornfox --json apps vars 应用名 --reveal`。看到的密码和 Key **不要在对话里复述**，也不要写进文件、提交信息或别的命令。用户只是想自己看密码时，优先请他自己在服务器上运行 `acornfox apps vars 应用名 --reveal`，或在控制台的应用详情里看。
- 用户要改设置：**现在只能看，不能改**（输出里 `can_change` 一定是 `false`）。照实告诉用户“改设置的功能还没有”。**不要**自己去改容器、进容器改配置文件、改数据卷里的文件，也不要用 `acornfox env set` 绕过：这样改平台不知道，这里显示的值就和实际对不上，以后重装或升级还可能被盖掉。用户坚持要换某一项时，停下说明现在没有稳妥的改法，由用户决定怎么办，不要替他动手。应用自己后台里能改的（例如 New API 的管理员密码）由用户在应用里改；改了以后这里显示的仍是安装时的值。

**几种情况：**

- 清单没存（`manifest.saved` 是 `false`，`notes` 里有“清单没存，分不出类别”）：这个应用是 v0.5.1 之前装的，那时不保存清单，升级后本机也找不到它那一版的应用包，所以补不上。应用照常运行，不用处理。这时 `class` 都是空，自定义变量分不出哪些是密码，**一律按密码遮住**（用户名也会被遮住）。照实告诉用户“这个应用装得早，只能列出保存的值，分不出类别”，不要猜。
- `origin` 是 `install_time`：旧版本装的应用，那时不记每个值是谁给的，分不出哪些是用户改过的。照实说“装时的值”，不要说成“默认值”或“用户填的”。
- `from_catalog` 是 `true` 但 `vars` 是空：应用还在安装，等安装任务结束再查。
- `CAT-INST-22`：没有这个应用，`acornfox --json apps` 看名字。
- `CAT-INST-20`：保存的参数解不开。不要自己想办法，把原始输出交给用户。
- 这里显示的是**安装时保存的值**，不是从容器里现读的。用户怀疑应用里实际的值和它不一样时，照实说明这一点。

### 私有目录

这一版还没开放。用户问起私有目录或给了连接码，就告诉他“私有目录暂未开放”，不要运行 `catalog connect`。

## 生命周期

```bash
acornfox --app 应用名 stop --json      # 等到容器真的停下才返回
acornfox --app 应用名 start --json     # 等到观察状态是 running 才返回
acornfox --app 应用名 restart --json   # 同上
acornfox rollback --json --app APP [--to N]   # 回到上一个可回退的版本，或第 N 版（只换程序，不动数据）
acornfox stats APP --json       # 该应用的 CPU、内存、网络（不带应用名则是整台服务器）
acornfox stats APP --history 1h --json   # v0.5.1 起：该应用近 1 小时的 CPU、内存（24h 是近 24 小时）；只读。历史只存在内存里，服务重启后从头记，返回的 ranges 为空说明服务端版本旧、没有历史
acornfox domain add example.com --json
acornfox domain list --json
```

`start`、`restart` 返回 `"ok":true` 并且带 `"running":true` 时，应用已经在运行，不用再用 `apps` 确认。输出里没有 `running` 字段（v0.4.x 不等就返回）时，隔几秒用 `acornfox --json apps` 确认状态是 `running`。失败时看 `diagnosis.code`：`start_timeout` 是等了 60 秒还没起来，按 `hint` 运行 `acornfox logs 应用名` 找原因；`no_deployment` 是应用还没有上线过的版本，先部署；`cancelled` 是等待被打断，请求已经提交，用 `status` 看状态，不用再提交一次。不写 `--app` 时管的是当前目录对应的应用。v0.5 起应用名也可以直接写在命令后面（`acornfox stop 应用名 --json`），旧版本报“不接受参数”时改用 `--app`。

`acornfox --json delete 应用名 [--volumes]` 和 `remove … --volumes` 会删除应用或数据，执行前必须得到用户明确同意。一定写上应用名（不写时删的是当前目录对应的应用）；不加 `--volumes` 时数据卷保留，以后同名重装会被 `CAT-INST-24` 拦下，用户要接着用旧数据时加 `--reuse-data`。

删除被拒、`diagnosis.code` 是 `app_in_use`：别的应用连在它上面（例如 Hermes 正在用本机的 New API）。不要硬删，也不要自己先删依赖它的应用；把 `message` 转述给用户，问要不要先删 `message` 里那个应用（删它会吊销给它发的令牌），用户同意后按顺序删。

回退只换程序，不重新构建；数据库、数据目录和环境变量保持现在的样子，回退前把这一点告诉用户。如果后来的版本改过数据库结构，旧版本可能起不来，也要提醒。“重新部署那一版的代码”是用那一版的代码（Git 提交或本地目录）再 `deploy` 一次，会生成新的版本号。要回到哪一版，先 `acornfox versions --json` 看 `can_rollback` 为 true 的版本。回退出错时：

| code | 处理 |
| --- | --- |
| `server_too_old` | 服务器上的 AcornFox 版本较旧，不支持这个功能。停下，告诉用户先升级服务器（见“升级、卸载”）；不要改用不带 `--to` 的 `rollback` 凑合，那会回到别的版本 |
| `deploy_in_progress` | 有一版正在部署，回退会取消它。问用户是否取消；用户同意后加 `--cancel-pending` 再执行。取消的那一版不会上线；回退上线前原来的版本继续服务 |
| `version_image_gone` | 那一版的程序已不在服务器上（`versions` 里它的 `rollback_blocked` 为 `image_gone`）。改用 `can_rollback` 为 true 的版本，或重新部署那一版的代码 |
| `version_not_found` | 没有这个版本号，先 `versions` 核对 |
| `version_same_as_live` / `version_is_live` | 已经是这一版的程序，不需要回退 |
| `version_not_rollbackable` | 那一版没有成功上线过（失败或被取代），换一个可回退的版本 |
| `logs_not_kept` | 那一版的日志没有保存（功能上线前部署的，或已超出保留范围），转述 `message` |

## 绑定域名

绑域名前确认三件事：安装时没用 `--allow-busy-80-443`（80、443 归 AcornFox）；安全组放行了 80、443；域名已解析到服务器公网 IP（国内服务器的域名必须已 ICP 备案）。然后在项目目录执行 `acornfox domain add 域名 --json`，证书自动申请。

## 网页控制台

控制台只在服务器本机监听，不对外开放。

- 情况乙：在用户电脑上执行 `acornfox open`，它经 SSH 转发并打开浏览器，用户看完按 Ctrl+C 关闭。
- 情况甲：请用户在**他自己电脑**的终端运行 `ssh -L 18800:127.0.0.1:18800 用户@地址` 并保持不关；然后你在服务器上执行 `acornfox open --no-browser`（不加 `--json`），把输出的登录链接告诉用户在自己电脑的浏览器里打开。链接一次性、60 秒内有效，过期就再执行一次。用户电脑是 Linux、Mac 时，更省事的做法是在他电脑上装 CLI，按情况乙第 7 步登记后用 `acornfox open`。
- 情况丙：同情况甲，请用户在 Windows 的终端（Git Bash 或 PowerShell）运行 `ssh -L 18800:127.0.0.1:18800 用户@地址` 并保持不关；你执行 `ssh 用户@地址 'acornfox open --no-browser'`，把输出的登录链接告诉用户在浏览器里打开。

打开某个应用（不是控制台）：情况乙在用户电脑上运行 `acornfox open 应用名`，同样经 SSH 隧道、按 Ctrl+C 关闭；只本机的应用也能这样打开，不用改对外方式、不用放行安全组。完整步骤见“从应用中心装应用”里的“装好后怎么打开”。

### 公网访问控制台（可选）

只有用户想在手机或别的电脑上直接用网址打开控制台时，才提这个。默认不开放：用户没有明确说要，就不要开。开之前先说清楚：

- 拿到登录密码，就等于拿到这台服务器的 root 权限，因为控制台可以部署任意程序。
- 需要一个用户自己的域名（例如 `panel.example.com`），而且不能是已经给应用用的域名。国内服务器的域名必须已经 ICP 备案。
- 需要在云厂商安全组放行 TCP 80 和 443，并且 80、443 没有被别的程序占用。

用户确认后执行（先只检查一次，看用户还需要做什么）：

```bash
acornfox public-console enable panel.example.com --json --wait 0
```

- `ok:true`：已开通。把 `url` 告诉用户；密码在 `password_file` 指向的文件里（只有用户本人可读），请用户自己打开文件，把密码存进密码管理器后删除该文件。**不要读取或在对话里复述密码。** 如果 `https.code` 不是 `ok`，把 `https.message` 和 `https.hint` 转述给用户。
- `ok:false`：按 `diagnosis.code` 用大白话告诉用户怎么做。处理完后用 `--wait 10m` 再执行，命令会每 15 秒自动重查，通过后自动开通：

| code | 告诉用户 |
| --- | --- |
| `dns_unresolved` | 去域名服务商后台添加解析记录，原样转述 `hint` 里的“主机记录”和“记录值” |
| `dns_mismatch` | 域名指到了别的地址：改成 `hint` 里的记录值，删掉其他记录，关掉 CDN/代理 |
| `port_closed` / `port_unreachable` | 去云厂商控制台的安全组放行 TCP 80 和 443 |
| `icp_blocked` / `connection_reset` | 该域名未备案，国内服务器无法使用；先备案或换一个已备案的域名 |
| `wrong_target` | 域名经过了 CDN/代理，或指向了另一台服务器；关掉 CDN/代理 |
| `dns_error` | 本机查不了 DNS，检查网络后重试 |
| `invalid_domain` / `domain_in_use` / `public_ip_unknown` | 转述 `message` |

其他命令：

```bash
acornfox public-console status --json                         # 是否开启、地址、HTTPS 是否正常
acornfox public-console enable 域名 --reset-password --json    # 忘记密码时重新生成，旧密码和登录会话立即失效
acornfox public-console disable --json                        # 关闭：移除路由，公网登录的会话全部失效
```

### 局域网打开控制台

家里、办公室局域网里的 Linux 服务器专用：局域网里的电脑、手机用浏览器打开 `http://服务器局域网地址:18802`。**云主机、公网服务器开不了**（`LAN-CONSOLE-01`），用上面的 SSH 隧道或公网访问。

v0.5.1 起的规矩（更早的版本是 `enable` 时生成一个密码，只显示一次）：

- 局域网服务器**第一次安装时默认已经打开**（安装时加 `--no-lan-console` 才不开）；重装、升级不会自动打开。
- **第一次打开时在浏览器里设密码**（至少 8 位），局域网里谁先打开谁设。所以装完要提醒用户现在就去设。
- 登录时勾“记住这台设备 90 天”，90 天内不用再输密码。
- 只认局域网来源的访问，别的来源一律 403（`LAN-CONSOLE-02`）。局域网里不加密，只在可信的网络里用。

#### 最低路径（照着做，不用判断）

```bash
acornfox lan-console status --json
```

按返回值转述，不要自己发挥：

| 返回 | 对用户说什么、做什么 |
| --- | --- |
| `enabled: false` | 没开。用户想开，先确认这台机器在他自己的局域网里，再运行 `acornfox lan-console enable --json`（写操作） |
| `enabled: true`，`password_set: false` | 把 `url` 给用户，请他**现在就**用局域网里的电脑打开设密码（`LAN-CONSOLE-03`） |
| `enabled: true`，`password_set: true` | 把 `url` 给用户；把 `password_set_at`、`password_set_from` 念给他，问“是不是你设的”。`password_set_at` 是 UTC 时间（结尾带 `Z`），先换成用户当地时间再念（国内加 8 小时） |
| `generated_password: true` | 旧版本开的，还在用当时生成的密码，照常能用 |

返回里的 `devices` 是现在记住了几台设备，用户问起再说；要看是哪几台用下面的 `devices`。

其他命令：

```bash
acornfox lan-console reset-password --json   # 写操作：清掉密码，所有登录和记住的设备一起作废；之后请用户重新打开 url 设密码
acornfox lan-console devices --json          # 只读：记住的设备（编号、名称、上次使用时间和地址、到期日）
acornfox lan-console forget 设备编号 --json    # 写操作：让一台设备失效（forget --all 是全部）；重复执行不出错
# 用户只说了“那台手机”“那台 iPhone”没给编号：先 devices --json，按 name 找；只有一台对得上就直接 forget 它，
# 对得上的不止一台就把这几台的名称、上次使用时间和地址列给用户，问是哪一台
acornfox lan-console disable --json          # 写操作：关闭，密码、登录、记住的设备全部清掉
```

#### 必须先问用户的

- `enable`：开之前问。云主机上不要尝试，也不要为了开它去改安全组、防火墙或 `--public-host`。
- `reset-password`、`forget --all`、`disable`：会让所有人掉线，先问。例外：用户自己说了“忘了密码”或“设密码的不是我”，这句话就是同意，直接运行 `reset-password`，不用再问一遍；做完用 `status --json` 确认 `password_set` 是 `false`，再请用户现在就打开 `url` 重新设。
- **不要替用户设密码，不要让用户把密码发到对话里。** 密码只在浏览器里输入。

#### 出错先看哪里

| 现象 | 原因和做法 |
| --- | --- |
| `enable` 返回 `LAN-CONSOLE-01` | 云主机或公网服务器，或服务刚启动还没判断出来。局域网机器等十几秒再试一次；云主机改用 `acornfox open` |
| 浏览器里 403，页面写着 `LAN-CONSOLE-02` | 访问的人不在局域网里。从外面进用 `acornfox open` |
| `acornfox doctor` 警告 `LAN-CONSOLE-03` | 开着但没设密码。请用户现在去设，或者 `disable` |
| 浏览器打不开 `url` | 先 `status` 看开没开；再看电脑和服务器是不是同一个局域网、服务器防火墙有没有拦 18802 |

## 升级、卸载

**升级要先问用户**（升级期间命令行和控制台会暂时连不上；从 v0.3.x 升级时应用网址会短暂打不开）。用户同意后在服务器上执行（国内、海外二选一；不要加 `-y`）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/upgrade.sh | sudo bash
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh | sudo bash
```

通过管道运行就算同意升级，不会再询问。已经是最新版本时，脚本打印“已是最新版本 …，不用升级”后直接退出，服务不会停，这是正常的，告诉用户“已是最新版”即可。不要为了“强制升级”加 `-y`：加了会把同一版本重装一遍，白停一次服务。第 3 步是 `sudo=need-password` 时，把命令交给用户运行。

升级成功时最后打印 `升级完成！`（同一版本重装是 `重装完成！`）。升级后，情况乙还要重新运行一次第 7 步的 install-cli 命令，让电脑上的 CLI 和服务器同版本；情况丙不用（服务器上的命令行随升级一起换了）。

**AcornFox 本身只能升级，不能回到旧版本。** 没有 `--rollback`；`--version` 指定比现在旧的版本会被拒绝，加 `-y` 也一样。用户说“回滚 AcornFox”“退回上一版 AcornFox”时，告诉用户：平台不支持回到旧版本；升级没成功时脚本已经自动恢复到升级前；应用自己的版本可以回退（`acornfox rollback 应用名`，见“生命周期”）。不要去拷备份目录里的文件，也不要找旧版本的升级脚本来降级。

升级时 SSH 断开、看不到结果：重新连上后运行 `sudo tail -n 40 /var/log/acornfox-upgrade.log`，脚本自动处理的过程和故障编号写在这里；再运行 `acornfox --json version` 看现在的版本。

升级停下时：

| 看到 | 下一步 |
| --- | --- |
| `故障编号: INSTALL-PORT-01` | 按“安装故障编号”处理 |
| `故障编号: UPGRADE-DOWN-01`（`当前是 …，要装的 … 比它旧`） | 服务没停、什么都没改。AcornFox 只能升级，告诉用户这个版本装不了；不要加 `-y` 或换旧版本号重试。要升级就去掉 `--version` 重新运行上面的命令 |
| `故障编号: UPGRADE-ROLLBACK-01`（`只能升级，不能回到旧版本`） | 服务没停、什么都没改。按上面那段话告诉用户；用户其实是想回退某个应用，用 `acornfox rollback 应用名` |
| `故障编号: UPGRADE-BACKUP-01` | 停服务后备份没做完（常见是磁盘满），没有升级，原来的服务已重新启动。`df -h /var/lib/acornfox` 看空间，转述给用户；用户腾出空间后再升级 |
| `故障编号: UPGRADE-FAIL-01`（迁移失败）、`UPGRADE-FAIL-02`（新版本服务起不来）、`UPGRADE-FAIL-03`（中途出错或被打断，包括 SSH 断开） | 没有升上，脚本已自动恢复到升级前，服务照常运行。告诉用户现在仍是原来的版本（`acornfox --json version`）和输出里的失败原因。`FAIL-03` 可以再运行一次升级命令；`FAIL-01`、`FAIL-02` 先问用户要不要再试一次，再失败就停下，请用户把输出反馈给维护者 |
| `故障编号: UPGRADE-RESTORE-01` | 恢复到升级前时文件没能换回去，或者换回去之后服务还是起不来。应用容器没停，一般还能打开，但控制台和命令行用不了。**停下**，把输出原样给用户，请用户把 `journalctl -u acornfox-server -n 50 --no-pager`、`journalctl -u acornfox-runner -n 50 --no-pager` 的结果反馈给维护者。不要删备份、不要重装、不要自己拷备份里的文件 |

**卸载只在用户明确要求时做。** 默认保留数据；`--purge` 会连数据一起删除且不可恢复，必须用户明确说要删数据。卸载脚本要在终端里输入 `yes` 确认，请用户在自己的终端里运行。经 ssh 执行、没有终端时，脚本报 `UNINSTALL-TTY-01` 退出，什么都没改；只有用户明确要你代为卸载时才加 `-y`（`sudo bash uninstall.sh -y`）。

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/uninstall.sh -o uninstall.sh && sudo bash uninstall.sh
```

国内把地址换成 `https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/uninstall.sh`。

## 更新 Skill

`acornfox skill install` 用与 CLI 同版本的 Skill 覆盖 AI 工作台里的这份文件；`acornfox skill print` 只打印。情况丙（Windows 上没有 CLI）用 `ssh 用户@地址 'acornfox skill print'` 取服务器上同版本的 Skill。

## Dockerfile 要点

- 服务器在国内时，安装依赖一开始就用国内镜像源，否则 npm / pip / Go 下载常会超时：npm 加 `--registry=https://registry.npmmirror.com`，pip 加 `-i https://mirrors.aliyun.com/pypi/simple`，Go 设 `ENV GOPROXY=https://goproxy.cn,direct`。
- 程序监听 `0.0.0.0:$PORT`，`EXPOSE` 与 `--port` 一致。
- 用多阶段构建和 `.dockerignore` 控制体积（上传上限 200 MB）。
- 需要持久化的数据写到固定目录，再用 `volume add` 挂载。

## 可以发挥的地方

下面这些没有固定步骤，可以按用户的情况判断，但不能绕过上面“必须先问用户”的规则：

- 帮用户给项目写 Dockerfile、`.dockerignore`，选基础镜像和启动命令。
- 读构建日志、运行日志，判断是代码问题还是配置问题，改用户的代码。
- 推荐云服务器配置、地域（只做建议，买不买由用户定）。
- 用户的服务器上已经有宝塔、1Panel、nginx 时，解释共存的取舍（`INSTALL-PORT-02` 两种选择）。
- 帮用户规划数据库、数据目录、内存分配。
- 用大白话向用户解释报错，补充表格里没写到的背景。

## 注意

- 国内服务器的域名需要备案；未备案时用 `IP:端口` 访问。
- 不要在命令行或代码里输出密钥值；用 `env set --secret`。
- 不要为了“装上”去改安装脚本、跳过校验、关闭 SSH 指纹校验，或在不支持的系统上硬装。
