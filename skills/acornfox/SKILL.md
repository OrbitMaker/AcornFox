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
| AcornFox 服务装在 Mac、Windows 电脑本机 | **即将支持**，现在不要尝试 |

Mac、Windows 用户现在的做法：准备一台 Linux 服务器，把 AcornFox 装到服务器上。Mac 电脑只装命令行工具（CLI），通过 SSH 管理服务器；Windows 电脑上什么都不装，AI 工作台经 `ssh` 在服务器上直接运行 `acornfox`（服务器上安装 AcornFox 时已经装好了命令行）。**不要运行 `acornfox install`、`acornfox setup` 去在 Mac 本机装服务端**，这个流程还没发布。

## 任何时候都要遵守

**必须先停下问用户、得到明确同意的事：**

- 花钱：买服务器、升级配置、扩容磁盘。
- 删除数据：`acornfox delete`、`remove … --volumes`、卸载、`--purge`、`docker system prune`、重装系统。
- 改安全组、防火墙（`ufw` 等），停掉别的程序（例如 nginx）。
- 输入密码：SSH 密码、sudo 密码、网盘或云账号密码。**任何密码都不能由你输入**，交给用户在他自己的终端里操作。
- 改服务器级设置：整机资源上限（`acornfox limits`）、开公网控制台、升级 AcornFox。

**输出规则：**

- 管理员令牌、登录密码、`password_file` 里的内容，不要在对话里复述。
- 大多数命令加 `--json`，按输出里的 `ok` 判断成败。两个例外写法要照抄：版本用 `acornfox --json version`；自检用 `acornfox doctor --json`（`--json` 放在 doctor 后面）。
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

用户是 Mac、Windows，又没有 Linux 服务器：告诉用户“AcornFox 目前要装在 Linux 服务器上，Mac、Windows 本机运行即将支持”，建议买一台云服务器（2 核 2 GB 以上，系统选 Ubuntu 24.04）。**买服务器要花钱，由用户自己决定和操作。** 用户买好后再继续。

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
- 经 ssh 执行时，当前目录是服务器上的家目录。要在应用目录里执行的命令（目录里的 `.acornfox` 记着应用名）先 `cd`，例如 `ssh 用户@地址 'cd ~/acornfox-apps/myapp && acornfox status --json'`；或者加 `--app 应用名`。
- 不要在 Windows 上运行 install-cli 命令：本版本没有 Windows 版 CLI，脚本只会说明情况并退出。
- 端口、私钥照第 2 步加在命令名紧后面：`ssh -p 端口 -i 私钥路径 用户@地址 '…'`；`scp` 也一样写在 `scp` 紧后面，但端口参数是大写的 `-P 端口`（`scp -P 端口 -i 私钥路径 -r …`）。

### 第 3 步：检查服务器

在服务器上执行（只读，不改任何东西）。**整段原样复制执行，不要删减**：每一行后面都会用到，其中云厂商那一行决定后面用不用厂商内网源。

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; echo "sudo=root"; elif sudo -n true 2>/dev/null; then S="sudo -n"; echo "sudo=ok"; else S="sudo -n"; echo "sudo=need-password"; fi
. /etc/os-release 2>/dev/null; echo "os=${ID:-unknown} ${VERSION_ID:-}"
echo "arch=$(uname -m)"
echo "user=$(id -un)"
echo "mem_mb=$(free -m | awk '/^Mem:/{print $2}')"
echo "disk_free_gb=$(df -BG --output=avail / | tail -1 | tr -dc 0-9)"
command -v ufw >/dev/null && $S ufw status 2>/dev/null | head -1 | sed 's/^/ufw=/'
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
| `ufw=` | `Status: active` | 记下来，第 9 步要用 |

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
- 不是 root 的用户（已在 `acornfox-users` 组里）直接运行 `acornfox doctor` 时，下面两条是正常的，不是故障：Docker 一项写“由后台服务访问 Docker……当前用户不需要 Docker 权限”（Docker 由后台服务访问，普通用户本来就不用连 Docker）；`SETUP-PORT-05` 警告“不是 root，看不到是谁占用”（AcornFox 自己的端口已经认出来了，剩下的端口要确认就用 `sudo acornfox doctor --json`）。
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

第二条每隔 5 秒执行一次，直到 `status` 是 `live` 或 `failed`（和上面一样）。`live` 之后取网址：这一条输出里没有 `url`，要在部署时的目录里查应用状态（经 ssh 不 `cd` 会报 `not_found`），用输出里的 `url`：

```bash
ssh 用户@地址 'cd ~/acornfox-hello && acornfox --json status'
```

现成镜像、公开 Git 仓库不用传文件，在服务器上建一个目录直接部署，例如 `ssh 用户@地址 'mkdir -p ~/acornfox-apps/myapp && cd ~/acornfox-apps/myapp && acornfox deploy --json --no-wait --git 仓库地址 --app myapp'`。

用户电脑上文件夹里的项目：先把文件夹传到服务器，再在服务器上那个目录里部署。`项目文件夹` 写 Git Bash 里的路径，例如 Windows 的 `C:\Users\me\myapp` 写成 `/c/Users/me/myapp`；传上去后服务器上的目录名和文件夹名相同（下面的 `myapp`）。

```bash
ssh 用户@地址 'mkdir -p ~/acornfox-apps'
scp -r 项目文件夹 用户@地址:acornfox-apps/
ssh 用户@地址 'cd ~/acornfox-apps/myapp && acornfox deploy --json --no-wait'
```

之后同上：`ssh 用户@地址 'acornfox --json status DEPLOYMENT_ID'` 查到 `live`，再用 `ssh 用户@地址 'cd ~/acornfox-apps/myapp && acornfox --json status'` 取 `url`。

- 容器监听的端口不是 Dockerfile 里 `EXPOSE` 的那个，或者没写 `EXPOSE` 时，在 `deploy` 后面加 `--port 端口`。
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

## 安装故障编号

安装脚本在改动系统之前先检查，不满足时打印 `故障编号: 编号` 并停下，什么都没改。升级脚本也用这些编号，另外有两个 `UPGRADE-` 开头的编号，只在升级、回滚时出现，同样是停下、什么都没改（服务也没停）。

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
| `UPGRADE-DOWN-01` | 升级时要装的版本比现在旧，且读不了现在的数据库：不支持降级到 v0.3.x（v0.4.0 以前）；v0.4.0 起的版本之间加 `-y` 降级时，在数据库副本上试跑旧版本的迁移没通过 | 不要加 `-y` 或换 `--version` 重试。告诉用户这个版本降不回去；用户是想回到升级前的版本，改用 `--rollback`，按“升级、卸载”里的“回滚”做（先说明副作用，用户同意后执行） |
| `UPGRADE-ROLLBACK-01` | `--rollback` 找不到比现在版本旧的备份，没有回滚 | 现在已经在原来的版本上（上次升级没成功、已自动回到原版本，或者已经回滚过一次），不用再回滚，告诉用户现在的版本（`acornfox --json version`）。输出里列的备份目录**不要自己去恢复**：用户要把数据库倒回某一份备份的状态（会丢掉那之后的改动），先停下问清楚用户 |

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

表里没有的编号：把那一项的 `text` 原样转述给用户，照里面的建议做；涉及删除、停程序、花钱的先问用户。

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

## 生命周期

```bash
acornfox stop --json
acornfox start --json
acornfox restart --json
acornfox rollback --json --app APP [--to N]   # 回到上一个可回退的版本，或第 N 版（只换程序，不动数据）
acornfox stats APP --json       # 该应用的 CPU、内存、网络（不带应用名则是整台服务器）
acornfox domain add example.com --json
acornfox domain list --json
```

`acornfox delete --json [--volumes]` 和 `remove … --volumes` 会删除应用或数据，执行前必须得到用户明确同意。

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

### 局域网打开控制台（可选）

只适合放在家里、公司内网的 Linux 机器。**公网云服务器不要开**，用上面的公网访问（带 HTTPS）。

```bash
acornfox lan-console enable --json   # 返回 url 和 password（密码只显示这一次）
acornfox lan-console status --json
acornfox lan-console disable --json
```

把 `url` 告诉用户；密码请用户自己记下，不要在对话里复述。

## 升级、卸载

**升级要先问用户**（升级期间命令行和控制台会暂时连不上；从 v0.3.x 升级时应用网址会短暂打不开）。用户同意后在服务器上执行（国内、海外二选一；不要加 `-y`）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/upgrade.sh | sudo bash
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh | sudo bash
```

通过管道运行就算同意升级，不会再询问。已经是最新版本时，脚本打印“已是最新版本 …，不用升级”后直接退出，服务不会停，这是正常的，告诉用户“已是最新版”即可。不要为了“强制升级”加 `-y`：加了会把同一版本重装一遍，白停一次服务。第 3 步是 `sudo=need-password` 时，把命令交给用户运行。

升级成功时最后打印 `升级完成！`（用 `-y` 装了更旧的版本时是 `降级完成！`，同一版本重装是 `重装完成！`）。升级后，情况乙还要重新运行一次第 7 步的 install-cli 命令，让电脑上的 CLI 和服务器同版本；情况丙不用（服务器上的命令行随升级一起换了）。

升级停下时：

| 看到 | 下一步 |
| --- | --- |
| `故障编号: INSTALL-PORT-01` | 按“安装故障编号”处理 |
| `故障编号: UPGRADE-DOWN-01`（`当前是 …，不能降级到 …`） | 要装的版本比现在旧，读不了现在的数据库：不支持降级到 v0.3.x；v0.4.0 起的版本之间降级，在数据库副本上试跑旧版本的迁移没通过。服务没停、什么都没改。不要加 `-y` 或换 `--version` 重试；用户想回到升级前的版本，按下面“回滚”做 |
| `早于当前版本 …，通过管道运行时不会自动降级`（没有故障编号） | 指定的版本比现在旧，服务没停。用户想回到升级前的版本，按下面“回滚”做；用户明确要装那个更旧的版本（只限 v0.4.0 起的版本之间），先问用户，同意后在同一条命令里再加 `-y` 重跑（例如 `| sudo bash -s -- --version 版本 -y`），脚本会先在数据库副本上试跑，不通过就报 `UPGRADE-DOWN-01` |

**回滚 AcornFox 本身**（回到升级前的版本）只在用户明确要求时做，先把下面几点告诉用户，得到同意再执行：

- 回滚会把 AcornFox 的数据库恢复到升级前：升级之后新建的应用、设的环境变量、绑的域名都会从列表里消失，回到 v0.3.x 时这些应用的容器也会被删掉；它们的数据卷留在磁盘上，可以手工找回。升级前就有的应用和数据不受影响。
- 从 v0.3.x 升级来的，回滚时应用网址会断几秒。
- 只能回到升级前的那份备份；不要用 `--version` 指定旧版本去降级：降到 v0.3.x 脚本一定拒绝（`UPGRADE-DOWN-01`），v0.4.0 起的版本之间降级也要旧程序认得新数据库才行。

命令（国内把地址换成 Gitee 的 `upgrade.sh` 地址；第 3 步是 `sudo=need-password` 时交给用户运行）：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh -o upgrade.sh && sudo bash upgrade.sh --rollback
```

回滚的结果：

| 看到 | 下一步 |
| --- | --- |
| `回滚完成` | 告诉用户回到了哪个版本（`acornfox --json version`）；情况乙重新运行一次第 7 步的 install-cli 命令（情况丙不用） |
| `故障编号: UPGRADE-ROLLBACK-01`（`没有比当前版本 … 旧的备份，没有回滚`） | 服务没停、什么都没改。现在已经在原来的版本上（上次升级没成功、已自动回到原版本，或者已经回滚过一次），不用再回滚，告诉用户现在的版本。刚用 `-y` 降级过、想回到降级前的版本：不用 `--rollback`，重新运行上面的升级命令。输出里列的备份目录**不要自己去恢复**：用户要把数据库倒回某一份备份的状态（会丢掉那之后的改动），先停下问清楚用户 |
| `找不到备份`（没有故障编号） | 这台服务器没升级过，或备份已被清理，没有可回的版本，告诉用户 |

**卸载只在用户明确要求时做。** 默认保留数据；`--purge` 会连数据一起删除且不可恢复，必须用户明确说要删数据。卸载脚本要在终端里输入 `yes` 确认，请用户在自己的终端里运行：

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
