---
name: acornfox
description: 用 AcornFox（也叫 AFX）在用户自己的 Linux 服务器上安装部署平台并上线应用：判断用户情况和国内外网络、检查系统、一行命令安装、自检、部署第一个应用、从应用中心装应用并接上它需要的服务；之后处理部署失败、数据库、日志、回退和应用生命周期。用户说“安装 AcornFox”“安装 AFX”“用 AFX”“部署”“上线”“发布到服务器”“装个应用”“登记服务”“加个数据库”“看日志”或提到 acornfox、AFX 时使用。
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
- 删除数据：`acornfox delete`、`remove … --volumes`、`services remove … --yes`、卸载、`--purge`、`docker system prune`、`docker image prune`、`docker volume prune`、删服务器上的文件、重装系统。
- 改安全组、防火墙（`ufw` 等），停掉别的程序（例如 nginx）。
- 输入密码和 Key：SSH 密码、sudo 密码、网盘或云账号密码，以及应用和服务用的密码、API Key。**任何密码和 Key 都不能由你输入，你也不能把它写进命令行参数**，交给用户自己操作：登记、修改服务（`services add`、`services set`）和安装时的保密参数（`install --set 参数=-`，v0.6.0 起）都在命令里写成 `项=-`，命令会从标准输入读这一项的值；把整条命令交给用户，由他自己在服务器终端运行并输入。也可以请用户在控制台里填。
- 登记、修改、删除服务（`acornfox services add`、`set`、`remove --yes`；有保密项、命令交给用户自己运行的，先问到值，下一条消息里说清楚和给命令放在一起，用户去运行就算同意，见“登记服务”第 2、3 步），接上旧数据重装（`--reuse-data`）。
- 改服务器级设置：整机资源上限（`acornfox limits`）、开公网控制台、升级 AcornFox。

**可以直接做、不用问的：** 只读的检查和排查，例如 `acornfox status`、`apps`、`logs`、`doctor`、`install status`、`faults`，以及 `df -h`、`du`、`ls`、`free -m`。查清原因再把要用户决定的事给他，不要把“要不要再查一下”也当成选项问用户。

**输出规则：**

- 管理员令牌、登录密码、`password_file` 里的内容，不要在对话里复述。
- 凡是请用户自己运行的命令都算（登记服务、带 `参数=-` 的安装，也包括查密码的 `install status 任务号 --show-secrets`、在他电脑上运行的 `acornfox open 应用名` 和 `ssh -N -L …` 隧道）：每条命令单独放进一个代码块，一条一行，同一行前后不接别的话，**不要写在句子中间**；请用户运行完回复什么，另起一行写。用户会整行复制，接在后面的话会被当成命令的一部分。
- 程序给的要转告的话（`say`、`tell`、`must_tell`、`open_hint.ask`）：每句单独一条，加引号照抄，不改写、不和别的话合并；要补充解释，另起一句写在后面。句子里带的命令（例如 `tell` 里的 `acornfox services set …`）照原话留在引号里，不算交给用户运行，不用拆出来；用户说要填时，再按“登记服务”里“改”的办法把命令单独放进代码块给他（保密项写成 `项=-`）。
- 称呼：AcornFox 自己的网页叫“AcornFox 控制台”，应用自己的网页叫“某应用的页面”，不要都叫“控制台”。
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
| `故障编号: INSTALL-…` 或 `故障编号: EDGE-…` | 先执行下面“看故障详情”的命令，再按下文“安装故障编号”处理 |
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
| 3 更新软件源 | `更新软件包列表`、`系统软件源实测`、`配置 APT 使用镜像`、`已停用境外官方源` | 不到 2 分钟 | 10 分钟以上 |
| 3 附：等系统自动更新 | `软件包列表被其他进程…占用，等待释放` | 新开的云服务器常见 | 最多等 10 分钟，脚本会自己继续 |
| 4 装基础工具 | `安装基础依赖` | 不到 1 分钟 | 几分钟 |
| 5 装 Docker | `安装 Docker`、`Docker 安装包下载源实测`、`使用 Docker 源` | 测速最多约 15 秒，下载 1～3 分钟 | 10 分钟以上。**下载这一段日志不会更新**，只能看 `download_kbps=` |
| 6 AcornFox 程序 | `创建系统账号`、`下载 AcornFox` | 不到 1 分钟 | 几分钟 |
| 7 入口镜像 | `下载入口包`、`使用本地入口包`、`导入入口镜像`、`入口镜像已就绪` | 不到 1 分钟 | 几分钟 |
| 8 Docker 镜像加速 | `配置 Docker 镜像加速`（装好程序以后才做：加速地址取自程序） | 十几秒以内（要试一下每个加速地址通不通） | 十几秒 |
| 9 启动服务 | `创建数据目录`、`初始化数据库`、`生成管理员令牌`、`创建 systemd 服务`、`启动 AcornFox 服务`、`入口容器 acornfox-edge 已运行` | 不到 1 分钟 | 最多约 2 分钟（等入口容器最多 60 秒） |

`phase=` 是 `开始下载: 地址` 时，表示正在下载这个文件（地址里有 `acornfox-edge_` 是第 7 阶段，别的 `acornfox` 是第 6 阶段），下完会出现 `下载完成: 大小，用时 N 秒`。

第 6、7 阶段出现 `[WARN] 下载没成功（curl 退出码 …）` 不算失败：脚本会自己换下一个来源，见下文“安装时其他报错”。

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
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash
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

- `doctor` 里 `"ok":false` 的项目：按它的 `code` 查下文“自检故障编号”；`warn` 项转述给用户即可，不挡路。
- 输出结构：`{"ok":…,"items":[…]}`，每项有 `ok`、`level`（`ok`、`warn`、`fail`）、`text`、`check`，要处理的项另有 `code`；没有 `status` 字段。
- 要看某一项的结果（例如时钟），按 `check` 找，不要按文字找：每一项都带 `check`，是这项检查的编号，通过的项也有（时钟一项是 `SETUP-CLOCK-01`，端口一项是 `SETUP-PORT-01`）。`code` 只出现在要处理的项上。
- 不是 root 的用户（已在 `acornfox-users` 组里）直接运行 `acornfox doctor` 时，下面三条是正常的，不是故障：Docker 一项写“由后台服务访问 Docker……当前用户不需要 Docker 权限”（Docker 由后台服务访问，普通用户本来就不用连 Docker）；`SETUP-PORT-05` 警告“不是 root，看不到是谁占用”（AcornFox 自己的端口已经认出来了，剩下的端口要确认就用 `sudo acornfox doctor --json`）；`SETUP-GW-01` 那一项通过、写“下载网关：当前用户读不到……这一项没查”（v0.5.1 起，普通用户读不到数据目录里的网关设置）：这一项其实**没查**，不能当成“网关正常”，要知道网关通不通就用 `sudo acornfox doctor --json`。
- `services` 里有不是 `active` 的：执行 `sudo journalctl -u acornfox-server -n 50 --no-pager`（以及 `-u acornfox-runner`），把报错转述给用户。
- `edge=` 不是 `running`，或 `edge_health=` 不是 `正常` 开头：执行 `sudo docker logs --tail 50 acornfox-edge`，再看 `doctor` 里“入口代理”那一项的 `code`（`EDGE-01`～`EDGE-04`），按“遇到故障编号怎么办”查；多半是端口被占，按 `SETUP-PORT-01`、`SETUP-PORT-02`、`SETUP-PORT-03` 处理。

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
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli
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

### 第 7 步之后：打开控制台（固定一步）

登记好以后**固定做这一步，不用问用户**：让用户在浏览器里看到 AcornFox 控制台，之后他可以在那里看这台服务器、装应用、改设置。这一步只读，不改服务器上的任何东西。用户说过不要打开，就跳过。

**① 先看局域网控制台开没开**（在服务器上执行；情况乙、丙前面加 `ssh 用户@地址`，照第 2 步的写法；情况甲且第 7 步是 `group=added` 时照第 7 步定下的写法包一层）：

```bash
acornfox lan-console status --json
```

| 看到 | 下一步 |
| --- | --- |
| `enabled: true`（局域网服务器第一次安装时默认就开着） | 做 ②甲 |
| `enabled: false`、报 `LAN-CONSOLE-01`，或者命令不认识 `lan-console` | 做 ②乙 |

**②甲 局域网服务器：直接给地址，不用隧道。** 情况乙（你在用户电脑上）时，先在用户电脑上用浏览器打开 `url`（Mac 执行 `open 地址`，Linux 执行 `xdg-open 地址`；打不开就算了，地址照样给）。然后照抄告诉用户（`<url>` 换成返回里的 `url`）：

- `password_set: false`：“控制台已经能在同一局域网里的电脑、手机上打开：<url>。现在还没有密码，第一次打开时要先设一个（至少 8 位）；局域网里谁先打开谁设，请现在就去设。”
- `password_set: true`：“控制台在同一局域网里的电脑、手机上用 <url> 打开，用设好的密码登录。”

到这里这一步就做完了，不用再开隧道。

**②乙 云主机、公网服务器：经 SSH 隧道打开。** 按第 1 步的情况分：

- **情况乙**（你在用户电脑上，第 7 步用 `--ssh` 登记了服务器）：在用户电脑上运行下面这一条。`--detach` 让隧道在后台开着、命令马上结束；**不要**运行不带 `--detach` 的 `acornfox open`，它会一直占着前台等人按 Ctrl+C，你会卡住。

```bash
acornfox --json open --detach
```

  | 看到 | 下一步 |
  | --- | --- |
  | `"ok":true`，`browser_opened` 是 `true` | 照抄告诉用户：“浏览器已经打开了 AcornFox 控制台（<url>）。连接在后台开着，不用开着终端；电脑重启或断网后要再打开就跟我说。” |
  | `"ok":true`，`browser_opened` 是 `false` | 把 `login_url` 告诉用户，请他在浏览器里打开（一次性，60 秒内有效；过期就再运行一次上面这条命令，给新的链接） |
  | 报不认识 `--detach`（用户电脑上的命令行比 v0.6.2 旧） | 请用户在**他自己电脑**的终端运行下面这一行，它会打开浏览器；用的时候终端保持不关，用完按 Ctrl+C。你不要自己运行它 |
  | `diagnosis.stage` 是 `connect` | 按下文“连接故障”处理，处理好再运行一次 |

  旧命令行时交给用户的那一行：

```bash
acornfox open
```

- **情况甲、丙**（你在服务器上，或者在 Windows 上经 ssh 运行命令）：用户电脑上的隧道你建不了。发一条消息，请用户在**他自己电脑**的终端运行下面这一行并保持不关，开好了回复“好了”（`用户@地址` 换成他登录这台服务器用的）：

```bash
ssh -N -L 18800:127.0.0.1:18800 用户@地址
```

  用户回复后，你在服务器上执行 `acornfox open --no-browser`（情况丙：`ssh 用户@地址 'acornfox open --no-browser'`；都不加 `--json`），把输出的登录链接告诉用户在他电脑的浏览器里打开（一次性，60 秒内有效，过期就再执行一次）。用户电脑是 Linux、Mac 时，也可以按第 7 步情况乙在他电脑上装命令行、登记，以后用 `acornfox open`。

**不用问用户的**：`lan-console status`；`acornfox --json open --detach`（只在用户电脑上开一个本地端口和一条 SSH 连接，不改服务器）；在用户电脑上用浏览器打开地址。

**必须先问用户的**：

- 关掉后台隧道（`acornfox open --stop`，在用户电脑上运行）：用户说不用了才关。
- 局域网控制台没开、用户想在局域网里打开：先确认这台机器在他自己的局域网里，再照下文“局域网打开控制台”开（写操作）。
- 对公网开放控制台：照下文“公网访问控制台”，用户明确要了才做。

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

局域网服务器（“第 7 步之后：打开控制台”里局域网控制台是开着的）：应用默认局域网里就能打开，没有安全组，这一步跳过；服务器自带防火墙开着时（第 3 步看到 `ufw=Status: active`）照下面最后一段放行。其他服务器告诉用户（原样转述即可）：

> 应用网址是 `http://服务器地址:端口`，端口从 18810 开始分配（18810–18899）。请到云厂商控制台，在这台服务器的**安全组**里添加入方向规则：TCP 18810-18899，来源 0.0.0.0/0。以后要绑定域名、开 HTTPS，还要放行 TCP 80 和 443。

第 3 步看到 `ufw=Status: active` 时还要说：服务器自带防火墙也开着，需要放行同样的端口；用户同意后执行 `sudo ufw allow 18810:18899/tcp`（要绑域名再加 `sudo ufw allow 80/tcp`、`sudo ufw allow 443/tcp`）。

**安全组、防火墙由用户决定。** 即使你能调用云厂商的命令行，也要先问用户。

### 第 10 步：告诉用户结果

照这个格式说，不要附带令牌和密码：

- AcornFox 已装在 `服务器地址`（系统、国内/海外）。
- 自检：通过（或列出 warn 项）。
- 控制台：怎么打开的（局域网地址并提醒设密码，或者浏览器已经经隧道打开）。
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
| `INSTALL-PORT-01` | AcornFox 自己要用的 18800、18801、18807 被别的程序占了 | 把脚本列出的程序和 PID 转述给用户，**问用户**能不能停掉或改端口；处理后重跑 |
| `INSTALL-PORT-02` | 80 或 443 被别的程序占了（常见：宝塔、1Panel、nginx） | 问用户二选一：①停掉原来的程序（用户同意后才停）再重跑；②共存：加 `-s -- --allow-busy-80-443` 重跑，应用照常用 `IP:端口` 访问，但不能在 AcornFox 里绑域名、开 HTTPS 和公网控制台，域名继续在原来的面板里反向代理到 `127.0.0.1:应用端口` |
| `INSTALL-PORT-03` | 应用端口段 18810-18899 里有端口被占 | 转述占用的程序，**问用户**能不能停掉或改端口；处理后重跑 |
| `INSTALL-CLOUD-01` | `--cloud` 或 `ACORNFOX_CLOUD_OVERRIDE` 的值不认识 | 这是测试参数，去掉后重跑 |
| `INSTALL-DOWN-01` | 服务器上已经装着更新的 AcornFox，或者留着卸载时保留的、更新版本的数据，要装的版本比它旧。AcornFox 只能升级，不能降级 | 什么都没改。不要换参数重试。要换新版本按“升级、卸载”做升级；用户坚持要装旧版本，只能先卸载并删除数据（`--purge`，不可恢复），这一步必须用户明确同意 |
| `INSTALL-DOCKER-01` | 装 Docker 的软件源都不能用，发行版自带的 docker.io 也没装上（报错里列出每个源的实测结果） | Docker 和 AcornFox 都还没装上。先把报错里“实测结果”那一行转述给用户；过一会儿重跑同一条命令；用户有能用的 docker-ce 源就在 `sudo` 后面加 `ACORNFOX_DOCKER_CE_MIRROR=地址` 重跑；用户想自己装 Docker，装好后加 `-s -- --skip-docker` 重跑。改网络、代理之前先问用户 |

| `EDGE-02` | 入口镜像（入口包）没下载下来、校验不过或导入失败。升级时报的：服务没停、什么都没改 | 把报错那几行转述给用户。下载不了：检查国内外地址用对没有，过一会儿重跑同一条命令；`SHA256 校验失败`：重跑一次，还失败**停下**告诉用户（文件可能被篡改），不要跳过校验；磁盘满了（`df -h /var/lib/docker`）：清理要先问用户 |

### 安装时其他报错

这些报错发生在安装中途。`[ERROR]` 那几行处理后重跑同一条安装命令；`[WARN]` 开头的按表处理，多数不用重跑。

| 报错包含 | 处理 |
| --- | --- |
| `数据库初始化失败` | 执行 `grep -B3 '数据库初始化失败' ~/acornfox-install.log`，把这几行交给用户。不要删数据库文件 |
| `更新软件包列表失败`、`安装基础依赖失败` | 网络或软件源的问题。等 1 分钟重跑；还失败执行 `sudo apt-get update` 把输出交给用户 |
| `dpkg was interrupted`、`dpkg --configure -a` | 上次安装被中途停下。执行 `sudo dpkg --configure -a`，再重跑 |
| `Docker 安装失败` | 重跑；还失败请用户先自己装好 Docker，再加 `-s -- --skip-docker` 重跑 |
| `无法获取最新版本号`、`下载 AcornFox 失败` | 下载不通。国内服务器检查是不是用了 Gitee 地址；判断错了国内外时加 `-s -- --china`（或 `--global`）重跑 |
| `[WARN] 下载没成功（curl 退出码 …）`、`[WARN] Gitee 镜像下载失败，改从 GitHub 下载`（常见退出码 28，表示超时或太慢） | **正常，不用处理，也不要停下安装**。脚本会接着换下一个来源：中国大陆依次是 Gitee、GitHub 直连、两个 GitHub 加速地址，海外是 GitHub 直连；都慢时最后不限速再试一次。下的是入口包时，之后出现 `入口镜像已就绪` 就算这一步成功；所有来源都失败才会报 `下载入口包 … 失败`（`EDGE-02`） |
| `SHA256 校验失败`、`SHA-512 校验失败` | 重跑一次；还失败**停下**告诉用户（下载的文件可能被篡改），不要跳过校验 |
| `服务启动失败`、`生成管理员令牌失败` | `sudo journalctl -u acornfox-server -n 50 --no-pager`、`sudo journalctl -u acornfox-runner -n 50 --no-pager`，把输出交给用户 |
| `[WARN] 入口容器还没就绪` | 做第 6 步自检，按结果处理 |
| `[WARN] /etc/docker/daemon.json 已存在，未自动修改` | 服务器上原来就有 Docker 配置，脚本没加镜像加速。平台装应用时自己会实测换源，不受影响；只影响用户自己 `docker pull` 和构建项目。转述给用户；`sudo acornfox doctor` 会提醒里面不能用的地址（`SETUP-NET-03`） |
| `[WARN] 没取到镜像加速地址` | 装好程序后取加速地址时，程序里的每个加速地址都连不上 | 不挡安装。平台装应用时自己会实测换源；装完运行 `acornfox sources --probe` 看现在每个源怎样 |
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
| `SETUP-PORT-02` | 网关（入口代理）的管理口 18807 被占用 | 按输出的 PID 确认是什么程序，**问用户**后停掉，再 `sudo systemctl restart acornfox-server` |
| `SETUP-PORT-03` | 应用端口段里有端口被占用 | 分到这些端口的应用会上线失败。**问用户**能否停掉占用的程序 |
| `SETUP-PORT-04`（warn） | 局域网控制台端口 18802 被占用 | 不开局域网控制台就可以忽略 |
| `SETUP-PORT-05`（warn） | 不是 root，看不到端口是谁占的（root 运行时：分不清是不是 AcornFox 自己的） | 普通用户运行时出现是正常的，不挡路；要确认就用 `sudo acornfox doctor --json` 再查一次 |
| `SETUP-EDGE-01` | 网关组件（入口代理）没准备好 | `sudo docker logs --tail 50 acornfox-edge`；`sudo systemctl restart acornfox-server` 后再自检 |
| `EDGE-01`～`EDGE-04`（v0.6.0 起，要 sudo 才查） | “入口代理”一项：`EDGE-01` 容器 `acornfox-edge` 没在运行（所有应用网址都打不开）；`EDGE-02` 本机没有入口镜像；`EDGE-03` 管理口不应答或拒绝了令牌（应用照常能打开，新装的应用、改的路由和域名不生效）；`EDGE-04` 它自己的健康检查没过（有端口没在听、设置不是最新的） | 先只读：`sudo docker logs --tail 50 acornfox-edge`、`sudo docker exec acornfox-edge /usr/bin/afx-edge health`，把看到的告诉用户。再运行 `sudo systemctl restart acornfox-server`（会拉起或重建入口容器，应用容器不停；命令行、控制台断几秒），再自检。`EDGE-02` 按“安装故障编号”那一行处理。要停别的程序、清磁盘先问用户；**不要**自己改令牌文件、删入口容器或它的卷。重启后还在：照 `acornfox faults 编号 --json` 做 |
| `SETUP-CHK-02` | Docker 不能创建和删除容器 | 多半是磁盘满了，按 `SETUP-DISK-01` 处理 |
| `CAT-SVC-08`（warn，v0.6.0 起） | 应用用的服务现在不可用：写着哪个应用的哪个需要、接的哪条服务、现在什么状态 | 照这一项写的做：“提供它的应用停着”——问用户是不是有意停的，不是就 `acornfox start 那个应用`；“还差 …”——`acornfox services show 服务名` 看是哪几项，问用户要值，按“登记服务”里“改”的办法填（保密项交给用户）；“提供它的应用已删”——问用户是接上旧数据把它装回来，还是让这些应用改接别的服务。装回来的命令是 `acornfox install --name 原来的应用名 --reuse-data`（原来的应用名就是提醒里写的那个；不用写目录里的名字，平台自己找回它原来是哪个应用；提醒里写的是带目录名字的 `acornfox install 目录里的名字 --name 原来的应用名 --reuse-data` 时，照它写的用也一样）。先在这条命令后面加 `--plan` 看一次（只读），报错说找不到它原来是哪个应用时，问用户当初装的是目录里哪个应用，写上应用编号（`acornfox --json install 应用编号 --name 原来的应用名 --reuse-data --plan`）再看；计划没问题后把整条命令给用户看，同意后才运行；“存的值解不开”——不要删，把输出交给用户。应用本身照常运行，只是用到这项服务的功能用不了 |
| `CAT-SVC-09`（warn，v0.6.0 起） | 服务还不能用：提供它的应用的就绪检查没过，后面是它清单写的原因 | 把原因加引号照抄告诉用户（多半要用户在提供它的应用里做一步），不要自己去那个应用里改。用户说做好了，运行 `acornfox --json services show 服务名`：`status` 是 `ok` 就好了；还是 `not_ready` 就把 `ready_say` 再加引号照抄一次；`ready_detail` 是排查用的，不念给用户，只用来自己看是不是超时或提供方还没起来（超时的过一会儿再看，或按 `acornfox faults CAT-SVC-09 --json` 的步骤查） |
| `SETUP-NET-01`（v0.6.0 起 Linux 服务器上也查） | 镜像下载源都不能用（拒绝访问、没有镜像、限流或连不上） | 先查网络和代理：`acornfox --json sources --probe` 看每个源；改网络、代理之前问用户 |
| `SETUP-NET-03`（warn，v0.6.0 起） | 有的镜像下载源不能用或很慢（拉镜像时会自动跳过或排后），或 `/etc/docker/daemon.json` 里有不能用的加速地址 | 只是提醒，不挡路。用户要看就 `acornfox sources --probe`。daemon.json 里不能用的地址只影响用户自己 `docker pull` 和构建项目；要去掉得改 Docker 配置并重启 Docker（所有容器会重启），**先问用户** |
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

从应用中心装的应用，`status` 里还有它用的服务（v0.6.0 起）：人读输出“它用的服务”下每个需要一行（需要的名字、服务类型、必须还是可选、接的是哪条服务、那条服务现在的状态）；`--json` 里是 `services`，每条有 `use`、`kind`、`kind_label`、`required`、`service`（没接上是空）、`status`、`status_label`。`status` 不是 `ok` 的，按“自检故障编号”里 `CAT-SVC-08` 那一行处理。

`versions` 的每一版有 `status_text`、`source_text`、`duration_seconds`、`failure_text`、`can_rollback`、`rollback_text`，直接转述给用户，不要自己换算；顶层 `rollback_target` 是不指定版本时会回到的版本：除正在服务的版本外，最新一个还能一键回退的版本，不一定是“当前版本减一”；为 0 表示没有可回退的，告诉用户只能重新部署代码。部署失败时线上仍是原来那一版（`status` 为 `live` 的那条）。

部署状态：`queued` → `building` → `starting` → `checking` → `routing` → `live`；失败为 `failed`，被新提交取代为 `superseded`。

## 部署诊断码

| code | 处理 |
| --- | --- |
| `dockerfile_missing` | 按项目类型生成 Dockerfile |
| `too_large` | 补 `.dockerignore`，排除 node_modules、构建产物、大文件 |
| `build_failed` | 读 `log_excerpt`，修依赖或构建命令 |
| `registry_timeout` | 构建时下载依赖超时：服务器在国内时，Dockerfile 里改用国内镜像源（见下文 Dockerfile 要点）后重新部署 |
| `pull_failed` / `pull_timeout` / `image_not_found` | 镜像拉不下来：`message` 里逐个写了每个镜像源怎么了（`failure.sources` 每个源一条）。`image_not_found` 先核对镜像名和标签；写着“太慢”“没有进展”“总时限”的，告诉用户网络慢、换个时间再试，或请用户给 Docker 加一个自己的镜像加速地址（安装时 `--registry-mirror`）；都连不上的先查网络。`acornfox --json sources --probe` 看现在每个源多快 |
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

添加后会自动重新部署并注入连接变量：postgres、mysql 为 `DATABASE_URL`，redis 为 `REDIS_URL`。代码里从环境变量读取连接串，不要写死。`remove … --volumes` 会删数据，先问用户。这三条命令只给用户自己部署的项目用；从应用中心装的应用要数据库，按“从应用中心装应用”先看计划，**不要**对它用 `acornfox add`。

## 从应用中心装应用

应用中心的目录里是预先配好的应用和配方（套装：几个应用一起装好并接上）。从目录装不用写 Dockerfile，也不用填密码：密码和密钥由 AcornFox 生成，装好后给出首次登录信息。情况丙照样经 ssh 在服务器上运行这些命令。

**服务（v0.6.0 起）**：有的应用要用到别的东西才能完整工作，例如要调大模型的应用需要一个“大模型接口”，要发通知的应用需要一个“邮箱（发信）”，要存数据的应用需要一个数据库（例如“PostgreSQL 数据库”）。这些叫**服务**；应用写明了它“需要的服务”（必须的或可选的），也写明了它装好后“能提供的服务”。服务从两处来：这台机器上装好的应用提供的（装好就自动登记），或者用户自己已有、登记进来的（见下面“登记服务”）。装应用时 AcornFox 按服务类型自动接上。**AcornFox 不会替用户去装别的应用来提供服务，也不会提示先装哪个应用**；要几个应用一起装好并接上，装配方（见下面“一次装好几个应用”）。数据库、缓存这类应用（v0.6.0 起）装好后**只在内部网络**：没有网页、没有访问地址，不用打开，它提供的服务给别的应用用；别的应用接上它时，平台照它清单写的自动给那个应用开一个单独的库和账号（有的一个只给一个应用用，例如缓存），不用用户做什么。下文例子里的应用名、服务名只是举例，以 `catalog list` 和 `services list` 实际列出的为准。

**最低路径（照着做）：**

1. 看目录：`acornfox --json catalog list`。`apps[].id` 是应用名，`recipes[].id` 是配方名，`apps[].installed` 不为空表示这台服务器已经装过。应用或配方的 `needs_platform` 不为空表示要先升级 AcornFox 才能装，先不要提交安装，按下面“要先升级 AcornFox 才能装的应用”做。应用或配方的 `former` 是 `true`（`note` 写“这是旧格式的清单，这一版装不了……”）：它还是按旧格式上架的，这一版装不了，要等它按新格式重新上架；告诉用户这不是操作的问题，不要提交安装，不要想办法绕过，也不要劝用户降级 AcornFox（`CAT-INST-36`）。同步结果是 `CAT-SYNC-08`（在线目录都取不到）时，用的是程序自带的目录，照样能装其中的应用，不用处理。
   **应用中心是空的**（`apps` 是空的，或者每一个都是 `former: true`；装应用时也会直接说“应用中心现在还没有可以安装的应用，要等应用按新格式重新上架”）：v0.6.0 换了清单格式，以前上架的应用要按新格式重新上架后才能装。照这句话告诉用户：现在没有可以装的应用，要等应用重新上架，这不是用户或你操作的问题；已经装着的应用照常用。到此为止：不要换别的办法装（不要拉镜像自己部署、不要降级 AcornFox），也不要反复同步。用户想过一阵再看，就请他以后运行 `acornfox catalog sync`（或者等每天的自动同步）后再问你。
2. 看要填什么：`acornfox --json catalog info 应用`。`params` 里每一项是能填的参数；`generated: true` 的留空会自动生成。`platform` 是平台按这台机器自动填的（时区、下载源等），**不用填**。`uses` 是这个应用需要的服务，`provides` 是它装好后能提供的服务。
3. 先看计划（只读，什么都不装，不用问用户）：

   先看下面这条命令输出里每个应用的 `exposure`（v0.6.2 起有；装好后的对外方式）：`lan` 表示装好后同一局域网里的电脑、手机就能打开（局域网里新装的服务器默认这样，是装机时定的），**不用问、不加 `--expose`**，下面这段跳过；`internal` 是数据库、缓存这类，也跳过。是 `local`（只本机，云主机上默认这样），或者没有 `exposure` 这一项（服务器版本旧）时照下面这段做。

   **用户要在局域网里别的电脑（或手机）上用这个应用时**（v0.6.0 起；用户说了“局域网”“别的电脑”“手机上用”这类话）：先问用户这句（引号带上），等用户回答：

   “要不要让同一局域网里的电脑都能打开它？这会在服务器的所有网卡上开放这个端口，服务器如果还有公网地址、安全组又放行了这个端口，公网也能访问到；打开后要用应用自己的账号密码登录”

   用户同意：下面看计划和第 5 步提交都加 `--expose lan`（例如 `acornfox --json install 应用 --expose lan --plan`），装好时应用拿到的地址就是局域网地址，第 7 步 ⑥ 不用再问。用户不同意，或者用户没提局域网：不加，装完照第 7 步做。数据库、缓存这类只在内部网络的应用不加（加了报 `CAT-INST-02`）。服务器上的 AcornFox 比 0.6.0 旧、报不认识 `--expose` 时：去掉它，装完照第 7 步 ⑥ 开放。

   ```bash
   acornfox --json install 应用 --plan
   acornfox --json install 应用 --plan --set 参数名=值 --use 需要名=服务名   # 用户已经给了参数、选了服务时，带上一起看
   ```

   **`--set` 左边写 `catalog info` 里 `params[].key`（例如 `model`），不要写中文的 `label`（例如“模型名”）**：写成 `--set model=qwen-plus`，不是 `--set 模型名=qwen-plus`。写错了报 `CAT-INST-02`，`message` 里列着这个应用的参数名。

   看输出：
   - `can_install` 是 `false`：现在装不了，原因在 `problems` 里（每条有 `code`、`message`）。把缺什么、为什么装不了照 `say` 的意思告诉用户（`say`、`message` 里带的命令不要念给用户，它是给你看的，里面的 `…` 还没填值），再按第 4 步和下面的故障表处理，处理好再看一次计划。`problems` 里是 `CAT-INST-36`（旧格式的清单）时，这个应用要等按新格式重新上架才能装：告诉用户，到此为止，不要换参数、换版本反复试。是 `CAT-INST-37`、`CAT-INST-38` 时按下面故障表那一行做。
   - `apps[].params_missing` 不为空：还有必填参数没给（`key`、`label`）。问用户要值，提交时用 `--set key=值`。密码这类保密的参数（`catalog info` 的 `params` 里 `type` 是 `secret`）**不要问值**，按下面“必须先停下问用户的”第一条处理（命令里写成 `--set 参数=-`，交给用户自己运行）。看计划时 `--set 参数=-` 不读输入，这一项照样列在 `params_missing` 里，不用管它。
   - 应用要的“模型名”这类设置项（例如 `key` 是 `model`、`label` 是“模型名”，`hint` 里写着要用哪种模型）是这个应用自己的必填参数，和它接哪条服务无关：**问用户要**，把 `hint` 原话告诉用户（例如上下文要多大）。**不要替用户编一个**，也不要拿服务里的默认模型、别的应用的设置或你自己知道的模型名顶上；用户说不知道，就请他先到提供大模型接口的地方（他自己的上游，或提供它的应用里）看有哪些模型，想好了再装。
   - `apps[].needs[]`：这个应用需要的每一种服务，按 `status` 做第 4 步。没有 `needs` 的应用直接做第 5 步。
   - 命令报 `server_too_old`（或“这个连接不支持安装前的计划”）：服务器上的 AcornFox 比这份说明旧。告诉用户，问要不要升级 AcornFox（见“升级、卸载”），不要换别的参数硬装。
4. 按每个需要的 `status` 做。说给用户听时用 `kind_label`（服务类型的中文名）；`use` 是这个需要的名字，`--use` 里写它：

   | `status` | 意思 | 怎么做 |
   | --- | --- | --- |
   | `matched` | 已接上，`service` 是接的那条服务。`pending_from` 不为空时，那条服务由同一个配方里先装的那个应用装好后登记 | 不用做什么，直接装（接的是数据库应用提供的服务时，库和账号由平台自动开）。有 `tell` 时每一句单独一条、加引号照抄告诉用户：句子里已经写了是哪条服务的哪一项、怎么填（命令照留在引号里，见“输出规则”），要不要填由用户定；“服务 X 现在还不能用：…”这句也照抄，它不挡安装。没有 `tell` 就不用说 |
   | `optional_missing` | 可选的，这台机器上没有 | 把 `without`（没有它会怎样）原话告诉用户，问用户要不要先登记一条他已有的（这条消息不给命令）。要：按“登记服务”第 2 步起做，登记好以后按“登记服务”第 4 步运行 `acornfox --json services test 名字`，通了再看一次计划。不要：也能装，直接往下做 |
   | `required_missing` | 必须的，这台机器上没有能用的 | 现在不能装。**照计划给的做**，按 `how` 里写的分三种：① 这台机器上有这种服务，提供它的应用停着：`how` 里是启动它的命令，先问用户是不是有意停的，同意后运行；② 有这种服务，还差几项没填：`how` 里是 `services set` 的命令，要填的值先问用户、拿到再给命令，保密项写成 `项=-` 交给用户自己运行（按“登记服务”的规矩）；③ 有这种服务，但提供它的应用已经删了、数据还留着：`how` 里写着用旧数据把哪个应用装回来，命令是 `acornfox install --name 那个应用名 --reuse-data`（应用名照 `how` 里写的），先问用户要不要装回来，同意后照“换应用版本、删了再接上旧数据重装”第 3 步起做（先加 `--plan` 看一次）。做完再看一次计划。`how` 里写着某条服务“被应用 … 独占”“已经有应用 … 在用”或“已经接满”：那条服务现在接不了，把这句原样告诉用户，问他有没有另一条他已有的要登记（按下面的两条消息做）；不要为了腾位置去删别的应用。三种都不是（`how` 里只有 `services add` 的命令）：原样告诉用户缺哪种服务（`kind_label`），分两条消息：第一条只问用户手上有没有现成的、每个必填项的值是什么（例如大模型接口的地址；数据库问地址、端口、库名、账号，PostgreSQL 再问一句要不要加密连接（连云数据库一般要）；保密项如 Key、密码不问值，只说到时由他自己在终端里输入），**不给命令**（用户前面已经给过的值不用再问）；用户给了值，下一条才按“登记服务”第 3 步给填好真实值的命令。登记好以后按“登记服务”第 4 步运行 `acornfox --json services test 名字`，通了再看一次计划。回答里没有值、也没说“没有”（例如只回“好的”）：照“登记服务”第 2 步再问一次。用户说没有就**停下**，告诉用户这个应用现在装不了。问的时候只问有没有现成的和值：**不要**主动提“装别的应用”“装一个数据库应用”或“装配方”这个选项，不要推荐先装哪个应用，也不要自己去装。用户自己提出来要装某个应用（包括数据库应用）时，那是另一次安装，照这里从第 2 步做起 |
   | `choose` | 有好几条能用，`candidates` 是它们的名字 | 运行 `acornfox --json services list`，把每条候选连同来源（`source` 是 `user`：用户自己登记的；`app`：哪个应用提供的）和 `status_label` 一起列给用户选，不替用户推荐。用户选了，带上 `--use 需要名=服务名`（配方里写 `--use 应用名.需要名=服务名`）再看一次计划，确认变成 `matched` |

   `how` 里登记服务的命令（`acornfox services add …`）已经带上了全部必填项：非保密的写成 `项=…`，把 `…` 换成用户给的真实值（值没问到就不给命令；命令里不能留 `…`、“接口地址”“你的地址”这类占位字，也不要让用户自己去替换）；保密的写成 `项=-`，原样保留，照“登记服务”第 3 步把整条命令交给用户自己运行。**不要**把保密项的值写进去。
5. 提交安装，不等（提交会建应用、拉镜像，用户要装才提交）：

   ```bash
   acornfox --json install 应用 --no-wait                      # 记下输出里的 task_id；next_command 就是下一步要运行的命令
   acornfox --json install 应用 --use 需要名=服务名 --no-wait   # 第 4 步用户选过服务时
   acornfox --json install --recipe 配方 --no-wait             # 配方，见“一次装好几个应用”
   ```

   看计划时带的 `--set`、`--use`、`--expose`，提交时原样带上。
6. 等它装完：`acornfox --json install status TASK_ID --wait`（v0.5.1 起）。它最多等 90 秒，装好或失败就提前返回。看输出最外层这几项，照着做，不用自己判断：
   - `done` 是 `false`：还没装完，结果还不知道。只把 `say` 告诉用户（它写了到了哪一步、这一步和整次各用了多久），然后运行 `next_command`（还是这条命令）接着等，直到 `done` 是 `true`。没到之前**不要**对用户说“卡住了”“装不好”“容器没在运行”“没接上服务”这类结论，也不要自己估计还要多久：时间只照 `say` 里的说，不要说“几分钟”。“启动”一步做完之前，`acornfox status 应用名` 里 `observed_state` 是 `missing`、`current_deployment` 为空，这是正常的，不用另外去查。
   - `stuck` 是 `true`（`timing.code` 是 `CAT-INST-30`）：正在做的那一步比平常慢很多，但安装还没失败。先运行 `next_command`（看日志或自检，只读，不用问用户），把 `say` 和看到的要点告诉用户，再问用户接着等还是不装了；详细步骤按“遇到故障编号怎么办”查 `CAT-INST-30`。不要自己删除、重装或重启服务。`slow` 是 `true` 只是比平常慢一些，照常接着等。
   - `done` 是 `true`：`status` 是 `succeeded` 做第 7 步，`failed` 做第 8 步。
   - 命令报不认识 `--wait`（服务器上的命令行是 v0.5.0）：去掉 `--wait`，每隔 5 秒查一次 `acornfox --json install status TASK_ID`，直到 `status` 是 `succeeded` 或 `failed`；中间同样不下结论。

   `steps` 里是每一步的进度，`timing.steps` 是每一步用了多久、正常多久（`normal_seconds`）。“下载镜像”这一步写着“没有 linux/arm64 的镜像包”“只算 linux/amd64 的”时，是这台服务器的 CPU 架构在下载网关上没有镜像包，改从公共源拉，会慢一些，不是故障，照常等。从公共源拉时（v0.6.0 起）这一步写着正在拉哪个镜像、从哪个源（第几个、共几个）、拉到多少、最近多快、这个镜像拉了多久，换过源的写上前一个源怎么了；这些都在 `say` 里，照原话转述就行。拉镜像的时间不算进整次安装的 60 分钟，一个镜像最多拉 50 分钟，所以套装里有几个大镜像、网又慢时，一次安装要一两个小时也是正常的，照常接着等。
7. 成功：给用户发**一条**收尾消息，照下面的样板按顺序写，对用户说“你”，不要写成给别人看的总结。`<…>` 换成安装结果（`install status` 的输出）里的值；某一节没有内容就整节省掉。任务输出的 `next` 是简写，顺序以这里为准：问题永远放在最后。

   **下面的话逐字照抄，只把 `<…>` 换成结果里的值；每条命令都放进 ```` ```bash ```` 代码块（写成单独一行不算）。**

   发之前先做只读的两步（不用问用户）。第一步，接的是哪条服务（① 用）：安装结果里这个应用的 `apps[].bindings` 有值，就用它（`service` 是接的服务），**不用**再运行 `status`；没有 `bindings` 才运行 `acornfox --json status 应用名`，看 `services[].service`。照这条做，不要自己判断能不能省。第二步，它能提供服务（计划里 `apps[].provides` 不为空）时，再运行 `acornfox --json services list`：它登记的服务（`source` 是 `app`）里状态是 `incomplete` 的，运行 `acornfox --json services show 服务名`，`ask` 不为空的项放进 ⑤。

   **① 结果**：一句话说装好了；接了服务的说接的是哪条。

   **② 首次登录**：`first_login` 里的地址和每个不保密的项（例如账号）原样说。地址有 `url_note` 时，地址后面加括号照抄 `url_note`；没有 `url_note`、地址又是 `127.0.0.1` 开头的，固定写“（这个地址只有服务器本机能打开，从你电脑打开的办法见下面“怎么打开”）”。**不复述密码**，照这样写：

      查看密码，在服务器终端运行下面这一行：

      ```bash
      <show_secrets_command>
      ```

      或在 AcornFox 控制台的应用详情里看。

   “查看密码”这一段只在 `first_login` 里有保密的项（值显示成 `******`）时才写；没有保密的项（例如应用让用户第一次打开时自己建账号），整段不写，也不要提查密码。有保密的项、结果里却没有 `show_secrets_command`（服务器版本旧）时，代码块里写 `acornfox install status <任务号> --show-secrets`（任务号换成真的）。

   **③ 怎么打开**：这个应用的 `apps[].internal` 是 `true`（数据库、缓存这类）时，③ 只照抄这一句：“它只在内部网络，是给别的应用用的，没有网页，不用打开。”不给命令，不去 `apps` 里找地址，⑥ 不为它问开放局域网。已经开放到局域网的应用（任务 `apps[]` 里这个应用没有 `open_hint`、`apps[].url` 不是 `127.0.0.1` 开头；局域网里新装的服务器上默认就是这样）：③ 只照抄这一句：“在同一局域网里的电脑、手机上用浏览器打开 <apps[].url>。”不给隧道命令，⑥ 不为它问。任务里有几个应用时，③ 每个应用各写一段，⑥ 只为有 `open_hint.ask` 的应用问。其他应用看任务 `apps[]` 里这个应用的 `open_hint`（没有这一项就运行 `acornfox --json apps` 找），照这样写（两条命令各放一个代码块）：

      <open_hint.command_where>。运行下面这一行会自动打开浏览器，看完按 Ctrl+C 关闭：

      ```bash
      <open_hint.command>
      ```

      没装命令行的话，<open_hint.tunnel_where>：

      ```bash
      <open_hint.tunnel_command>
      ```

      然后用浏览器打开 <open_hint.tunnel_url>（隧道开着时，你电脑上的这个地址就通到服务器上的应用）

   结果里没有 `command_where`、`tunnel_where`（旧版本服务器）时，第一句写“在你自己电脑的终端运行（电脑上要装了 acornfox 命令行，并用 --ssh 登记过这台服务器）；在服务器上运行没有用”，第二句写“在你自己电脑的终端运行，保持不关”。`tunnel_command` 里的“用户@服务器地址”：你知道用户登录这台服务器用的“用户名@地址”，就换进去；不知道，就原样留着，并在这个代码块下面另起一行写“把 用户@服务器地址 换成你登录这台服务器用的”。`apps` 里也没有 `open_hint` 时，把 `apps[].url` 告诉用户。三条禁令：`open_hint.command` 和 `tunnel_command` 都是交给用户在**他自己电脑上**运行的，**不要说成在服务器上运行**（在服务器上运行 `acornfox open` 只会打印服务器本机的地址，用户打不开）；`lan_url_after_expose` 是开放以后的地址，**现在打不开，③ 里不要当成地址给用户**（只在 ⑥ 用户同意、开放成功后才给）；`first_login` 和 `apps[].url` 里 `127.0.0.1` 开头的地址只有服务器本机能打开，提到就说明这一点。`tunnel_url` 不算：它是隧道开着时在用户自己电脑上打开的，不要加这句说明。`open_hint.lan_address` 只是服务器在局域网里的 IP 地址（不是应用的地址），**不要拿它自己拼地址给用户**：应用现在没开放到局域网，拼出来的地址打不开；要给局域网地址时只用程序给好的 `lan_url_after_expose`，而且照上面说的，只在 ⑥ 开放成功后给。

   **④ 还要你做的**：`user_steps` 逐条说，每条照原话说，要补充另起一句。其中写着安装时就该做、这次已经做完的（例如“安装时填要用的模型名”），改成提醒用户核对，不要说成还没做。

   **⑤ 要转告的**：`must_tell` 每句单独一条、加引号照抄，一句都不能漏（句子里的命令照留，见“输出规则”）；发之前查到的 `services show` 里的 `ask` 也一样各列一条（和 `must_tell` 里相同的不重复），用户做完后按“登记服务”里“改”的办法填。要补充解释，另起一句写在后面。不要和 `user_steps`、别的应用的话合并或改写。

   **⑤′ 接旧数据的情况**（只在接旧数据重装时有）：`carried`、`kept_files`、`file_backups`，怎么说见“换应用版本、删了再接上旧数据重装”第 5 步。

   **⑥ 最后的问题**：永远是这条消息的最后一段。`open_hint.ask` 有值时，照这样写（引号带上），发完就停下，等用户这次的回答：

      “<open_hint.ask>”

   **直接问**，不要写成“需要先问用户”“需要你同意后再改”这种转述。不管用户之前说过什么，都照样问、照样等。
   - 用户同意：你运行 `open_hint.ask_yes_command`，`ok` 是 `true` 后，告诉用户现在可以用 `open_hint.lan_url_after_expose` 打开了（`ask` 那句话里也写着这个地址）。不要再去 `status` 里找地址。再运行 `acornfox --json apps vars 应用名`：`vars` 里有 `code` 是 `APP-VARS-01` 的行（应用装的时候用的是只本机的地址），再照抄告诉用户这一句：“这个应用装的时候写进去的地址（例如 webhook 地址）还是只本机的那个，这一版不会跟着改，应用照常能用。要换成局域网地址，得保留数据删掉它再装回来，要的话告诉我。”用户要换，照“换应用版本、删了再接上旧数据重装”做，看计划和提交时加 `--expose lan`。没有这一行就不用说。
   - 用户不同意：不用再做什么，③ 的办法照样能用。

   旧版本服务器（`open_hint` 里没有 `ask`、`tunnel_command`）：③ 的隧道命令写 `ssh -N -L 端口:127.0.0.1:端口 用户@服务器地址`、浏览器打开 `http://127.0.0.1:端口`（端口是 `apps[].url` 里的）；旧版本里开放以后的地址叫 `lan_url`，没开放时也有值，③ 里同样**不能**当成现在能用的地址，⑥ 同意并开放成功后才给它。`recommend` 是 `lan` 时 ⑥ 问这句：“要不要让同一局域网里的电脑都能打开它？这会在服务器的所有网卡上开放这个端口，服务器如果还有公网地址、安全组又放行了这个端口，公网也能访问到；打开后要用应用自己的账号密码登录”，同意后运行 `acornfox --json app set --app 应用名 --expose lan`。
8. 失败：看 `fault.code`，按下表处理。失败时这次建出来的应用和数据卷已经自动清理（只清理这次新建的，以前留下的数据不会动），处理好原因后重新安装即可。加了 `--keep-data-on-failure` 时数据卷会留着，任务的 `apps[].volumes` 列出卷名。失败时平台也已收回这次发给它的凭据，并删掉提供方（数据库这类）里这次给它新建的库（只删这次新建的）。`apps[].carried` 里以 `CAT-INST-35` 开头的那句，加引号原样告诉用户（没收回成、或这次新建的库没删掉，要用户到提供方里手工删；你不要替用户删）。

### 装好后怎么打开（只本机的应用）

云主机上（还有 v0.6.2 之前装的机器），从应用中心装的应用默认只本机能打开（`exposure: local`）：`apps[].url` 是 `http://127.0.0.1:端口`，只在服务器自己身上能用，用户在自己电脑上点不开。局域网里新装的服务器上默认是 `lan`，`apps[].url` 就是局域网地址，直接给用户，不用看这一节。**不要**让用户去安全组或防火墙放行端口（应用只听本机地址，放行了也打不开）。

**最低路径（照着做）：** 运行 `acornfox --json apps`，找到这个应用。`internal_only: true`（数据库、缓存这类，只在内部网络）：告诉用户“它只在内部网络，是给别的应用用的，没有网页，不用打开”，到此为止；`acornfox open`、`app set --expose`、绑域名对它都不适用（报 `OPEN-APP-03` 或 `internal_app`），不要去试。`local_only` 不是 `true`（`exposure` 是 `lan`、`public`，已经开放）：运行 `acornfox --json status 应用名`，把最外层的 `lan_url`（没有就用 `url`）告诉用户。`local_only: true`：照“从应用中心装应用”第 7 步的 ③（怎么打开）和 ⑥（最后的问题）的样板写给用户，两条命令都给，规矩一样。`acornfox open 应用名` 像 `acornfox open` 打开 AcornFox 控制台一样开一条 SSH 隧道，要求用户电脑上装了 CLI 并按最低路径第 7 步（登记服务器）用 `--ssh` 登记了这台服务器。

`acornfox open 应用名` 出错时：

| 编号 | 怎么办 |
| --- | --- |
| `OPEN-APP-01` | 应用还没有访问端口：`acornfox --json status 应用名` 看部署状态，部署成功后再打开 |
| `OPEN-APP-02` | 这台服务器是用网址（局域网或公网控制台）登记的，开不了隧道：按最低路径第 7 步（登记服务器）用 `--ssh` 再登记一次；或者给 `tunnel_command` 那条；或者照上面第 7 步 ⑥ 问用户，同意后改成局域网 |
| `OPEN-APP-03` | 应用只在内部网络（`internal_only: true`，数据库、缓存这类给别的应用用的），没有网页：告诉用户不用打开它，要看的是用它的那个应用 |
| `diagnosis.stage` 是 `connect` | SSH 连不上或不让转发，按“连接故障”处理 |

**必须先停下问用户的：**

- 往任何地方填密码和 Key（安装参数里的密码、服务的保密项、应用里的账号）：**不要由你来填**，不要让用户把密码、Key 发到对话里，你也不要把它写在命令行参数、文件、提交信息里。按是哪一种来做：
  - 服务的保密项（`services add`、`services set`）：把命令给用户，保密项写成 `项=-`，请他自己在服务器终端运行并输入（照“登记服务”第 3 步）；或者请他在 AcornFox 控制台“设置 → 服务”里填。
  - 安装参数里的密码（`params` 里 `type` 是 `secret`）：参数是 `generated: true` 的，先问用户留空让平台生成行不行（装好后用 `install status TASK_ID --show-secrets` 或 AcornFox 控制台看）；用户要自己定密码，把整条安装命令单独放进代码块给他，密码那一项写成 `--set 参数=-`，请他自己在服务器终端运行，命令会读一行当密码（有几个 `参数=-` 就按出现的顺序读几行；读到空行时命令会提示）。也可以请他在 AcornFox 控制台的应用中心里装（安装弹窗里密码是遮着的输入框）。这两种都是用户自己装，装好后请他告诉你应用名，你用 `acornfox --json status 应用名` 接着看。旧版本（v0.5.x）的安装命令不认 `参数=-`，会把一个减号当成密码：那时只能请用户在 AcornFox 控制台里装。
  - 用户已经在对话里给了 Key 或密码：不要复述（写成 `sk-…`），不要写进任何地方，照上面的办法请他自己输入；告诉他发在对话里的 Key 要不要作废换一把由他决定。
- 登记、修改、删除服务（`services add`、`services set`、`services remove --yes`）：先说清要登记或改的是什么、哪些应用在用，用户同意才做。命令由用户自己运行的（有保密项），先问到值（照“登记服务”第 2 步，这一条不给命令），下一条消息里说清楚和给命令放在一起，用户去运行就算同意。
- 接上旧数据重装（`--reuse-data`），见下面 `CAT-INST-24` 那条和“换应用版本、删了再接上旧数据重装”。
- 对外开放：应用装好后的对外方式先按这台机器的默认（看计划里的 `apps[].exposure`：云主机上是只本机 `local`，局域网里新装的服务器上是 `lan`；这是装机时定的，照着装不用再问）。比默认开得更大时，管着 Key 的应用、能执行命令的应用，要开放到局域网或公网（`acornfox app set --expose …`、`install … --expose lan|public`、绑定域名）必须用户明确同意，并提醒走 HTTPS、用强密码。用户只是想自己看一眼时，优先用 `acornfox open 应用名`，不用改对外方式。
- 花钱：在提供大模型接口的应用里加上游渠道、登记别家的大模型 Key，用起来都会产生模型费用，加不加、用哪家由用户自己决定。
- 启动用户停掉的应用（例如提供服务的应用停着）：先问用户是不是有意停的。
- 内存不够（`CAT-INST-05`）：调小别的应用或调大整机上限都要用户同意。
- 删掉同名的旧应用（`CAT-INST-03`）：会删数据，先问。
- 应用要更新版本的 AcornFox 才能装（目录里 `needs_platform` 不为空，或安装报错里带 `needs_platform`：`CAT-INST-08` 一定带，`CAT-INST-29` 有的带）：升级 AcornFox 要用户同意，见下面“要先升级 AcornFox 才能装的应用”。
- 以前同名应用留下的数据卷（`CAT-INST-24`）：先问用户旧数据还要不要。要接着用旧数据，就在同一条安装命令后面加 `--reuse-data` 重装（账号密码和以前一样；少数情况平台没留下以前的记录，首次登录会写“沿用旧数据里的”，让用户用自己记得的旧密码）；不要旧数据就用 `--name 新名字` 装，或者确定不要了由用户自己运行 `message` 里卷名对应的 `docker volume rm` 后再装。你不要替用户删。接上旧数据时这次没写的设置按以前的来，见下面“换应用版本、删了再接上旧数据重装”。**对外方式会回到这台机器的默认**（计划里的 `apps[].exposure`；没有这一项时是只本机）：以前开放过局域网、而默认是 `local` 的，提交之前先告诉用户，问要不要照旧开放（问话照“从应用中心装应用”第 3 步），同意了看计划和提交都加 `--expose lan`；服务器比 0.6.0 旧时装好后照第 7 步 ⑥ 问。
- 用户要改平台自动填的值（例如时区、下载源）：先 `acornfox --json catalog platform-vars` 看 `user_can_set` 是不是 `true`，是才加 `--set platform.名字=值`（只对这次装的应用生效；值要符合 `rule_label` 写的，例如时区写成 `Asia/Shanghai`）。`false` 的（网络区域、访问地址、访问方式、地址里的主机）不能改，不要试；访问地址要对，就在装的时候用 `--expose` 定对外方式。
- 连数据一起删掉应用（`delete 应用名 --volumes`）：别的应用发给它的凭据会被收回；它接的是数据库应用提供的服务时（`services list` 里那条服务的 `source` 是 `app`），提供方里给它开的库和账号也会一起删掉，不能恢复，问用户时把这一点说清（例如“连数据删会把数据库应用里给它开的库也删掉”）；接的是用户自己登记的数据库（`source` 是 `user`）时，那边的库不会被动，不要说会删。返回里有 `warnings`（`CAT-INST-35`，没收回成或库没删掉）时，把那句话原样告诉用户，它写着要到哪个应用里手工删什么；你不要替用户删。只删应用、保留数据时，它接的服务、凭据和库都留着，以后接上旧数据重装时接着用。安装失败、或独占的服务被同时在装的别的应用先接上时，平台会收回这次发的凭据、删掉这次新建的库；没删掉的，任务的 `apps[].carried` 里有一句 `CAT-INST-35`，同样原样告诉用户。

**常见故障编号（完整说明用 `acornfox faults 编号 --json` 查，服务器回的 `hint` 就是处理方法）：**

| 编号 | 意思 | 怎么办 |
| --- | --- | --- |
| `CAT-INST-01` | 目录里没有这个应用、配方或版本 | `acornfox --json catalog list` 看有哪些 |
| `CAT-INST-02` | 参数不对 | 按 `message` 改 `--set`：左边写参数的 `key`，不是中文 `label`（`message` 里列着这个应用的参数；写着“参数名是 …”就照它改）。`acornfox --json catalog info 应用` 看参数 |
| `CAT-INST-03` | 已有同名应用 | `--name 新名字`；要重装先问用户再删旧的 |
| `CAT-INST-04` | 同一个应用正在装 | 不要重复提交；用错误里的 `task_id` 查进度 |
| `CAT-INST-05` | 内存不够 | 问用户：调小别的应用或调大整机上限 |
| `CAT-INST-08` | AcornFox 版本太旧，这个应用要更新的版本才能装 | 停下问用户是否升级 AcornFox（见下面“要先升级 AcornFox 才能装的应用”） |
| `CAT-INST-12` | 镜像拉不到 | 看 `fault.attempts`（每个镜像源一条：`source` 是地址，`code` 是它怎么了）和 `message`，把每个源的情况简单转述给用户。都是 `slow`、`stalled`、`timeout`（太慢、没进展、到了 50 分钟总时限）：告诉用户网络慢，换个时间再装；`message` 开头是“拉取镜像超时：50 分钟还没拉完”：一个镜像拉了 50 分钟还没拉完，把每个源拉到多少告诉用户，建议换个网快的时间再装；`message` 里写“运行器也没有回话”：照 `hint` 只读地看运行器（`systemctl status`、`journalctl`），把看到的告诉用户，重启服务要先问用户；都是 `skipped`、`unreachable`：先查网络，`acornfox --json sources --probe` 看现在每个源；国内网络再看 `sudo acornfox doctor --json` 的“下载网关”一项（不加 sudo 时这一项不查）。拉的时候“下载镜像”这一步写着从哪个源拉、拉到多少、最近多快（Docker 不报进度时写“看不到拉到多少”，照原话说，不要自己估数）；一个镜像最长要等 50 分钟，这段时间不算进整次安装的 60 分钟，不要中途重装 |
| `CAT-INST-13` | 下载网关给的镜像包和目录里的校验值对不上（没导入） | 会自动换公共源；不要手动绕过 |
| `CAT-INST-15` | 没能上线 | 看 `fault.diagnosis`，按“部署诊断码”处理；`acornfox logs 应用` |
| `CAT-INST-16` | 健康检查不过 | `acornfox logs 应用` 看日志；内存太小时问用户调大 |
| `CAT-INST-17` | 初始化失败（应用装好后要做的几步没走通，安装已失败并清理） | 把 `message` 告诉用户；`message` 里说应用限制了请求次数（HTTP 429）时，告诉用户过一会儿再装，不要反复重试 |
| `CAT-INST-18` | 安装途中服务重启 | 重新安装 |
| `CAT-INST-24` | 以前同名应用的数据卷还在 | 停下问用户（见上面“必须先停下问用户的”） |
| `CAT-INST-25`、`CAT-INST-26` | 和私有目录有关（这一版私有目录还没开放） | 不要自己想办法绕过，把原始输出交给用户 |
| `CAT-INST-27` | 磁盘不够（`message` 里有要多少、剩多少） | 先运行 `acornfox faults CAT-INST-27 --json`，按 `steps` 只读查清是什么占了空间（`df`、`du` 直接做，不用问）。删掉不用的应用、镜像或找出来的大文件，或者扩容磁盘，都先告诉用户是什么、多大，用户确认后才做；你不要替用户删。清出来的空间够了就再装一次，装成功才算好；`doctor` 的磁盘一项剩余不到 10 GB 时仍是警告，不影响这次安装 |
| `CAT-INST-28` | 改平台变量不对（没有这个变量、不能改、格式不对） | 去掉那个 `--set platform.…`；`acornfox --json catalog platform-vars` 看哪些能改 |
| `CAT-INST-29` | 这个应用里有这个版本的 AcornFox 不认识的写法 | 看错误里有没有 `needs_platform`（`--json` 输出的 `diagnosis.needs_platform`）：有，就和 `CAT-INST-08` 一样处理，它的值是要升到的版本；没有，是目录有问题，不要升级，把 `message` 原话告诉用户，过一段时间再试。不要按 `hint` 里有没有“升级”两个字判断（两种情况的 `hint` 是同一句） |
| `CAT-INST-30` | 安装还在进行，正在做的那一步比平常慢很多（`install status` 输出 `stuck: true`），还没失败 | 运行输出里的 `next_command`（只读）看原因，把 `say` 和要点告诉用户，问接着等还是不装；不要删除、重装、重启服务 |
| `CAT-INST-31` | 必须用到的服务这台机器上没有能用的 | 按上面第 4 步 `required_missing` 那一行做：问用户有没有现成的、值是什么，问到值再登记（`message` 里有登记的命令），登记好以后按“登记服务”第 4 步运行 `acornfox --json services test 名字`，通了再看一次计划；没有就停下。不要自己装别的应用，也不要主动提装别的应用或配方 |
| `CAT-INST-32` | 同一类服务有好几条，要选一条 | 按上面第 4 步 `choose` 那一行列给用户选，用户选了加 `--use 需要名=服务名` 再看一次计划、再提交 |
| `CAT-INST-33` | `--use` 指定的服务不对（没有这条、类型不对或现在不能用） | `acornfox --json services list` 看名字和状态，按 `message` 改；那条服务不能用就按“登记服务”里的办法让它能用（要问用户的照样先问） |
| `CAT-INST-34` | 提供服务的应用没能发凭据（安装已失败并清理） | `acornfox --json status 提供方应用` 看它在不在跑；把 `message` 告诉用户。`message` 里说应用限制了请求次数（HTTP 429）时，告诉用户过一会儿再装，不要反复重试 |
| `CAT-INST-35` | 收回凭据或删库没成功，或者安装没成功、这次新建的库没删掉（只是提醒，不拦） | 把那句话原样告诉用户：要用户到提供方应用里手工删掉它写的那一份；你不要替用户删 |
| `CAT-INST-36` | 目录里这个应用还是旧格式的清单，这一版装不了（什么都没建） | 告诉用户：要等这个应用按新格式重新上架，不是操作的问题。可以 `acornfox --json catalog sync` 后再看一次计划；还是这个编号就停下。**不要**降级 AcornFox、不要换参数或版本反复试、不要想办法绕过 |
| `CAT-INST-37` | 接旧数据重装时，以前发给它的凭据（例如数据库账号）核对没过；提供方写了核对不过就停下，因为换一份等于接到空库（看计划时在 `problems` 里，或安装失败） | **停下**，把 `message` 里的原因告诉用户，给三个选择让他定：① 先让提供方恢复（`acornfox --json status 提供方` 看它在不在跑，停着的问用户后再启动），好了用同一条命令再装；② 确定不要原来的数据，同意后加 `--use 需要名=服务名` 改接别的服务再装（没有别的同类服务，就按第 4 步 `required_missing` 那一行问用户有没有现成的，登记一条再改接）；③ 先不装，原样留着。①、② 处理好都先加 `--plan` 看一次，`can_install` 是 `true` 再装。不要自己改接别的服务、不要重装提供方、不要去提供方里删库或改密码 |
| `CAT-INST-38` | 要接的服务被别的应用独占、已经接满，或这个应用要独占它但已经有别的应用在用 | 把 `message` 原样告诉用户；运行 `acornfox --json services list` 看还有没有别的同类服务，有就问用户换哪一条（`--use 需要名=服务名`），没有就问他有没有另一条已有的要登记（按“登记服务”做）。不要为了腾位置删别的应用 |
| `CAT-INST-39` | 清单里的一个值算不出来（`{{…}}` 写法读不了、变换没对上、条件同时成立、启动命令引用了保密值）；读不了的什么都没建，其余的安装已失败并清理 | 把 `message` 加引号原样告诉用户，说明这是应用清单的问题，不是用户操作的问题。`message` 里是 `map` 没对上、条件同时成立这类、换一个值就能对上的（例如换对外方式、改一个设置项），问用户后再装；别的不要想办法绕过，请用户反馈给我们 |
| `APP-VARS-01` | `apps vars` 里的提醒：应用装的时候算好的地址（`app.url` 这类）和现在的不一样，多半是装完改了对外方式或绑了域名。应用照常运行，只是应用里写的地址还是旧的 | 不是错误，不用马上处理。用户问起时照“查一个应用是用什么设置装的”里这一条说 |
| `CAT-SVC-01` | 目录里没有这种服务类型 | `acornfox --json services kinds` 看有哪些，改对类型名；目录太旧就 `acornfox --json catalog sync` 后再看；还是没有，告诉用户这一版不支持这种服务 |
| `CAT-SVC-02` | 登记或修改服务时名字、项或值不对（少必填项、没有这一项、格式不对） | 按 `message` 改；`services kinds` 看这种服务有哪些项。缺的是地址、账号、密码、Key 这类只有用户知道的值，问用户要，保密项照“登记服务”第 3 步交给用户自己输入。报错的值是你写的占位字（用户还没给过）：照“登记服务”第 3 步回第 2 步先问到真实的值，不要再发带占位字的命令 |
| `CAT-SVC-03` | 已经有同名的服务 | `services list` 看那一条是不是就是用户要的；换个名字登记，或者问用户后用 `services set` 改那一条 |
| `CAT-SVC-04` | 还有应用在用这条服务，不能删 | 把 `used_by` 告诉用户；删不了，不要为了删它去删应用 |
| `CAT-SVC-05` | 这条服务是应用提供的，不能这样改或删 | 只能填 `ask` 不为空的项；要它消失得删掉提供它的应用（先问用户） |
| `CAT-SVC-06` | 试连没通 | 把 `detail` 原话告诉用户，问正确的地址、端口、Key，按“登记服务”里“改”的办法改了再试 |
| `CAT-SVC-07` | 地址写的是本机回环地址（127.0.0.1、localhost、::1） | 问用户要这台机器在局域网里的地址；这台机器上装的应用提供的服务不用登记，`services list` 里已经有 |
| `CAT-SVC-08` | 应用用的服务现在不可用（自检里的提醒） | 按“自检故障编号”里 `CAT-SVC-08` 那一行做 |
| `CAT-SVC-09` | 服务还不能用（提供它的应用的就绪检查没过） | 按“自检故障编号”里 `CAT-SVC-09` 那一行做 |

**用户自己在写应用清单、运行 `acornfox catalog lint 目录` 时**（v0.6.0 起的值的写法）：清单里的值写成 `{{种类.名字}}`，可以接变换，例如 `{{platform.app.scheme | map "https=true" "*=false"}}`；环境变量的一条、配置文件的一个文件可以写 `if: 'platform.region == cn'` 这类条件。报 `CAT-LINT-46`～`CAT-LINT-50` 是错误：照 `message` 改，改完再 lint 一次，没有错误才算好；不知道怎么改就运行 `acornfox faults 编号 --json` 照 `actions` 做。`CAT-LINT-51` 只是提醒：这个 `{{…}}` 不是平台的写法，会原样写进应用。把它和用户对一下：是应用自己的模板写法（例如 `{{ $json.x }}`）就不用改；是想取平台的值却漏了种类（例如步骤里写成 `{{token}}`，本来该是 `{{saved.token}}`），就要改。

**可以发挥的地方：** 用户只说想要一种功能（例如“想要一个能接飞书的 AI 助手”）时，可以从 `catalog list` 里挑合适的应用或配方推荐，并用计划解释它需要哪些服务、装好后还要用户做哪几步（`user_steps`）。缺服务时只问用户有没有现成的、请他登记，不要主动提装别的应用或配方；用户自己提出来，再当成另一次安装处理。应用里调模型报“没有可用渠道”这类错，多半是提供大模型接口的那个应用里还没加上游，提醒用户去加。

### 登记服务（v0.6.0 起）

什么时候用：计划里有必须的服务没有（`required_missing`、`CAT-INST-31`），或者可选的没有、用户想接上；用户说“我有自己的大模型 Key / 邮箱，登记进来”；用户问这台机器上有哪些服务、谁在用；提供服务的应用装好后还差用户填的项。

**最低路径（照着做）：**

1. 看有哪些服务类型、每种有哪几项（只读）：`acornfox --json services kinds`。`kinds[].kind` 是类型名（登记时写它），`label` 是中文名；`fields[]` 每一项：`key` 项名、`label` 中文名、`required` 必填、`secret` 保密、`default` 默认值（不填就用它）、`options` 只能填这几个值、`hint` 提示。
2. 名字用用户说的（文字和数字，第二个字起也可以用 `-`、`_`、`.`，不能有空格）；用户没说时你取一个，在第 3 步那条消息里写明“名字先叫 X，想换就告诉我”。问用户要每个必填项、他想填的非保密项的值（地址、端口、账号这类）。地址不能写 `127.0.0.1`、`localhost`、`::1`（`CAT-SVC-07`：应用在容器里连不到这台机器的本机地址；服务就在这台机器上时，问用户要这台机器在局域网里的地址）。**保密项（`secret: true`，密码、Key）不要问值**，只说到时由他自己在终端里输入。这一条消息只问、**不给命令**；用户给了值，下一条才做第 3 步。用户在前面的话里已经给过的值不用再问；都给过了就直接做第 3 步。用户的回答里既没有要的值、也没说“没有”（例如只回“好的”）：再问一次，只问这个值、不给命令；第二次还不清楚就停下，告诉用户现在登记不了（为装应用登记的，就说这个应用现在装不了）。
3. 登记：
   - 有保密项：值都问到以后（第 2 步），在**同一条消息**里先说清要登记什么，再给命令；命令里每项都写用户给的真实值，不能留 `…`、“接口地址”“你的地址”这类占位字，也不要让用户自己去替换；用户去运行就算同意，不用先等一句“同意”。保密项写成 `项=-`，命令会按出现的顺序一行一行读用户输入的值。照这个样板发（类型、项名以 `services kinds` 为准）：

     要登记一条“邮箱（发信）”服务，名字叫 my-mail：服务器 mail.example.com，账号 me；密码由你自己在终端里输入，不要发给我。请在服务器终端运行下面这一行：

     ```bash
     acornfox services add smtp --name my-mail --set host=mail.example.com --set username=me --set password=-
     ```

     按提示输入密码，做完回复“登记好了”。

     名字是你取的（用户没说）：把“名字叫 my-mail”换成“名字先叫 my-mail，想换就告诉我”。

     数据库照同一个样板，把用户给的地址、端口、库名、账号写进去，密码写成 `password=-`（下面的值只是例子）。用户说要加密连接，就再加 `--set sslmode=require`：

     ```bash
     acornfox services add postgres --name my-pg --set host=192.168.1.20 --set port=5432 --set database=appdb --set username=app --set password=-
     ```

     ```bash
     acornfox services add mysql --name my-mysql --set host=192.168.1.20 --set port=3306 --set database=appdb --set username=app --set password=-
     ```

     用户不方便用终端时，也可以请他在 AcornFox 控制台“设置 → 服务”里登记。
   - 没有保密项：先把要登记的内容告诉用户，用户同意后你来运行同样的命令（不用 `项=-`）。
   - 报 `CAT-SVC-01`（没有这种类型）、`CAT-SVC-02`（少必填项或值不对）、`CAT-SVC-03`（重名）、`CAT-SVC-07`（本机回环地址）：按 `message` 改，再按这一步重来。`CAT-SVC-02` 说某项的值不对、而那个值是你写的占位字（用户还没给过）：回到第 2 步先问到真实的值再给命令，不要再发带占位字的命令。
   - 用户运行后说报“用法错误”（退出码 2，例如“多了参数”“少了类型”）：多半是复制时多带了字、漏了类型，或命令被拆成了两行。你先运行 `acornfox services add --help`（改的是 `services set` 就看 `acornfox services set --help`）对一遍，再把命令单独放一行重新给用户。**不要**让用户把他运行的整条命令贴回来（里面可能有保密值）。
4. 用户说做完了（不管他贴没贴输出）：直接运行 `acornfox --json services test 名字`（只读，会从服务器去连那个地址，不用问用户），它同时确认登记上了没有：
   - 报 `not_found`（没有这条服务）：没登记上，回到第 3 步重新给命令。
   - `ok` 是 `true`：登记上了，也通了。
   - `ok` 是 `false`、`code` 是 `CAT-SVC-06`：登记上了，但没连通。把 `detail` 原话告诉用户，问正确的值，按下面“改”的办法改了再试。
   - `skipped` 是 `true`：登记上了，这种类型或这条服务现在试不了，照 `detail` 实说，不算通也不算不通。
5. 回到“从应用中心装应用”第 3 步，再看一次计划。

看和管：

- 看（只读）：`acornfox --json services list` 列出这台机器上的服务：`source` 是 `user`（用户自己登记的）或 `app`（由应用提供，`app` 是哪个应用）；`status` 是 `ok` 可用、`incomplete` 还差必填项（`missing` 是哪几项）、`provider_stopped` 提供它的应用停着、`provider_gone` 提供它的应用已删、`not_ready` 还不能用（`ready_say` 是提供它的应用写的原因，用户问到时加引号照抄；它不挡安装）；`used_by` 是哪些应用的哪个需要接着它；`dedicated_to` 不为空是被那个应用独占，`slots_left` 是还能接几个（0 是已经接满，`null` 是不限），这两种别的应用都接不了。`acornfox --json services show 名字` 看一条的每一项：保密项只显示后 4 位，没有显示明文的办法，也不要想办法看明文；`values[].ask` 不为空的项要用户在提供它的应用里做一步再填进来；用户问到这条服务时，把这句话加引号照抄告诉他。
- 改：先问用户。`acornfox services set 名字 项=值`，保密项同样写成 `项=-` 交给用户自己运行（怎么给照上面第 3 步，做完照第 4 步试一下）；`项=`（空）是清掉这一项。应用提供的服务只能填 `ask` 不为空的项（别的报 `CAT-SVC-05`）。输出的 `notes` 里列着在用它的应用、要怎样才生效，原话告诉用户。
- 删：先试跑（不删）：`acornfox --json services remove 名字`，看 `would_remove`、`blocked`、`used_by`，告诉用户会删什么。用户同意后才加 `--yes`（删了不能恢复）。`blocked` 是 `in_use`（`CAT-SVC-04`）：有应用在用，把 `used_by` 告诉用户，删不了，不要为了删它去删应用；`from_app`（`CAT-SVC-05`）：是应用提供的，提供它的应用还装着，不能单独删；连数据删掉那个应用时它一起没了，保留数据删掉时它留着（`provider_gone`），这时才能单独删。

### 一次装好几个应用（配方）

配方（套装）是目录里发布好的一组应用：装哪几个、按什么顺序、谁接谁的服务。`acornfox --json catalog list` 的 `recipes[]` 是配方。要几个应用一起装好并接上，只走配方；不要自己挑几个应用拼起来装。

**最低路径（照着做）：**

1. 看计划：`acornfox --json install --recipe 配方 --plan`。`steps[]` 是安装顺序（`action` 是 `reuse` 的已经装着，直接用）；`because` 写着它装好后登记的服务给后面哪个应用用。
2. 照“从应用中心装应用”第 3、4 步看每个应用的 `params_missing` 和 `needs`。后装的应用接先装的应用提供的服务，在计划里是 `matched`、`pending_from` 写着由谁提供，不用处理；别的需要照第 4 步做。给某个应用的参数写 `--set 应用名.参数=值`，指定服务写 `--use 应用名.需要名=服务名`。
3. 提交：`acornfox --json install --recipe 配方 --no-wait`（看计划时带的 `--set`、`--use` 原样带上）。平台照计划的顺序一个一个装，装好一个就登记它提供的服务，后面的应用自动接上。
4. 等、成功、失败的处理同“从应用中心装应用”第 6～8 步。`user_steps` 里常有“在提供服务的应用里做一步”（例如加上游渠道、填模型名），逐条告诉用户；要填进服务的项按“登记服务”里“改”的办法做（先问用户，保密项交给用户）。

### 换应用版本、删了再接上旧数据重装

这一版没有“把已装的应用升级到目录里的新版本”的功能。用户要换应用的版本（或者删掉应用后又想装回来），做法是删掉应用、保留数据，再用原来的安装命令加 `--reuse-data` 装回来。

**最低路径（照着做）：**

1. 删除前先 `acornfox --json status 应用名` 看一眼它现在的状态和 `services`（它接了哪些服务）。和你之前告诉用户的对不上时（例如你说过“卡住”，它其实已经在运行），先把实际情况告诉用户，再问还要不要删。删除要先问用户（会停掉应用）。同意后 `acornfox --json delete 应用名`（**不加** `--volumes`，数据卷保留）。删除被拒、`diagnosis.code` 是 `app_in_use`：别的应用接着它提供的服务，按下面“生命周期”里那一段做。
2. 只是删了装回原来的版本：跳过这一步。要换新版本的，`acornfox --json catalog sync` 后 `acornfox --json catalog info 应用` 看目录里的版本。
3. 先看计划：`acornfox --json install 应用 --reuse-data --plan`，照“从应用中心装应用”第 3、4 步处理；再提交：`acornfox --json install 应用 --reuse-data --no-wait`（装的时候用了 `--name` 的，用同一个名字）。只记得应用名、不知道它是目录里哪个应用时，可以只写应用名：`acornfox --json install --name 应用名 --reuse-data --plan`（v0.6.0 起，平台按留下的数据找回它原来是哪个应用）。这样报错、说找不到它原来是哪个应用时，问用户当初装的是目录里哪个应用，写上应用编号再看计划：`acornfox --json install 应用编号 --name 应用名 --reuse-data --plan`。**不用**把以前的 `--set`、`--use` 再写一遍：没写的都按以前的来。用户这次明确要改的才写上。
4. 照“从应用中心装应用”第 6 步等它装完（`acornfox --json install status TASK_ID --wait`），直到 `done` 是 `true`。没到之前什么结论都不下。
5. 成功后照“从应用中心装应用”第 7 步发收尾消息；任务 `apps[]` 里下面这几项写在它的 ⑤′（⑥ 的问题之前；没有这几项是服务器版本旧，设置可能没沿用，照实说）：
   - `carried`：沿用了什么，一条一句，照原话转述（例如“沿用以前改过的平台变量：platform.tz=Asia/Tokyo”）。
   - `kept_files`：数据里原有、没有重写的配置文件（用户和应用自己改过的内容都还在）。
   - `file_backups`：因为平台要填的内容变了（`steps` 里“写配置”一步的 `message` 写着变了什么）而重写的配置文件，`backup` 是旧文件另存的名字（在应用的数据卷里）。告诉用户：以前自己改过的内容在这个旧文件里，需要的话照着抄回去；你不要替用户进容器改。
   - 服务和凭据怎么样了**只看 `carried` 和 `must_tell`**：例如“旧的那份没通过检查，已经换了一份”“以前的那份没能收回”这类话原话告诉用户，旧的要不要到提供方应用里删由用户决定。不要自己推断“凭据还是原来那份”或“已经收回了”。
6. 对外方式：不加 `--expose` 时回到这台机器的默认（本节第 3 步计划里的 `apps[].exposure`；没有这一项时是只本机）。本节第 1 步 `status` 里 `exposure` 是 `lan`（以前开放过局域网）、而本节第 3 步看到的计划里是 `local` 的，提交之前问用户要不要照旧开放（问话照“从应用中心装应用”第 3 步），同意了加 `--expose lan` 再看一次计划，提交时也加上。没在本节第 3 步问过的，收尾消息的 ③ 照常写怎么打开，在 ⑥ 的问题前面加一句“以前开放过局域网，这次回到了只本机”，问题照 ⑥ 问。

沿用的规矩（判断用，不用念给用户）：

| 设置 | 这次没写 | 这次写了 |
| --- | --- | --- |
| 账号密码、平台生成的密钥 | 和以前一样 | 以这次为准 |
| 以前 `--set platform.名字=值` 改过的平台变量 | 恢复以前改的值。v0.5.0 装的应用分不出来，`carried` 里会说“要这次再写一遍”：问用户以前改过哪些，再加 `--set` 重装 | 以这次为准 |
| 需要的服务 | 接回原来那条（它现在能用的话）。别的应用发给它的凭据先核对还能不能接着用，不能用就换一份新的，`must_tell` 里会说；数据库这类（换一份等于接到空库）核对不过不换，安装停下（`CAT-INST-37`，按故障表做）；原来那条现在不能用，按计划里的状态处理（第 4 步） | `--use` 以这次为准 |
| 数据里已有的配置文件 | 平台要填的内容没变就不重写；变了（例如接的服务或凭据变了、新版本改了模板）就重写，旧文件先另存为 `文件名.bak-时间`；上一次接旧数据没装好、已经改写过的，这次也重写（旧文件再另存一份） | 同左 |

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

什么时候用：用户问“这个应用的密码、地址、时区、模型名是怎么设的”，或者排查应用配置（例如怀疑时区、下载源不对）时。只对从应用中心装的应用有用。只读，不改任何东西。它接的服务（例如大模型接口的地址和 Key、邮箱）不在这里：先 `acornfox --json status 应用名` 看 `services` 接的是哪条，再 `acornfox --json services show 服务名` 看那条服务的各项（保密项只显示后 4 位）。v0.5.1 之前没有这个命令（报用法错误，退出码 2），告诉用户要 v0.5.1 起才有，不要自己去翻容器。

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
   | `name` | `params.名字` 是这个应用自己的参数；`platform.名字` 是平台按这台机器给的（`platform.tz` 时区、`platform.app.url` 访问地址、`platform.app.scheme` 访问方式 http 或 https、`platform.app.host` 地址里的主机、`platform.mirror.pypi` 和 `platform.mirror.npm` 下载源、`platform.region` 网络区域） |
   | `layer` | 哪一层：`custom` 自定义变量（应用自己的），`system` 平台变量（平台给的） |
   | `class` | 类别：`system` 系统给、`generated` 平台生成、`default` 有默认值、`optional` 可选、`user_required` 必须用户填。空表示清单没存，分不出 |
   | `value` | 安装时保存的值。`masked: true` 表示被遮住了 |
   | `secret` | 是不是密码、密钥、Key 这一类 |
   | `origin` | 值从哪来：`user` 用户填的、`recipe` 配方预填的、`default` 清单默认值、`generated` 平台生成的、`platform` 平台算的、`platform_user` 用户安装时用 `--set platform.名字=值` 改的、`restored` 接上旧数据时恢复的、`install_time` 装时的值（旧版本装的，那时不记是谁给的）、`unset` 没填、`old_data` 沿用旧数据里的（平台不知道，`value` 是空） |
   | `editable` | 清单写的能不能改：`never` 不能改、`always` 随时能改、`reinstall` 重装才能改、`install_only` 只能在安装时用 `--set platform.名字=值` 改；空是清单没写或没存。**这只是清单的说法，现在哪一种都还改不了**（见下面） |
   | `used` | 只有平台变量有：这个应用用没用到它。`false` 表示这个值对这个应用不起作用 |
| `value_now` | 只有平台变量有（v0.6.0 起）：按现在的对外方式、域名和这台机器的设置算出来的值。`value` 是装时算好、应用现在用的 |
| `differs`、`code`、`fix` | `differs` 是 `true`、`code` 是 `APP-VARS-01`：装时的值和现在的不一样，应用里用的还是装时的（多半是装完改了对外方式或绑了域名）；`fix` 是怎么办 |

   它需要的服务接了哪条、现在能不能用，不在这张表里：看 `acornfox --json status 应用名` 的 `services`（见“状态与诊断”）。

4. 只回答用户问的那几项，不用把整张表念一遍。有 `code` 是 `APP-VARS-01` 的行、用户又问到地址（或者问“webhook 地址怎么还是旧的”）时，照这样说：“应用装的时候写进去的 <name> 是 <value>，现在算出来是 <value_now>。这一版改了对外方式或绑了域名以后，应用里的这个地址不会跟着改，应用照常能用。”用户要换成现在的值：因为绑了域名才不一样的（`value_now` 是 `https://` 开头），照实说删了重装也换不了，先不动；别的，问用户后照“换应用版本、删了再接上旧数据重装”做，看计划和提交时加 `--expose 现在的对外方式`（`acornfox --json status 应用名` 的 `exposure`）。**不要**用 `acornfox env set` 或进容器去改。密码、密钥、Key 默认只显示后 4 位（`******` 加后 4 位；不到 12 个字符的全遮成 `******`）。说到它们时只说“有值、后 4 位是什么、谁给的”。

**必须先停下问用户的：**

- 看完整的密码和 Key（`--reveal`）：**先问用户**，同意了才运行 `acornfox --json apps vars 应用名 --reveal`。看到的密码和 Key **不要在对话里复述**，也不要写进文件、提交信息或别的命令。用户只是想自己看密码时，优先请他自己在服务器上运行 `acornfox apps vars 应用名 --reveal`，或在 AcornFox 控制台的应用详情里看。
- 用户要改设置：**现在只能看，不能改**（输出里 `can_change` 一定是 `false`）。照实告诉用户“改设置的功能还没有”。**不要**自己去改容器、进容器改配置文件、改数据卷里的文件，也不要用 `acornfox env set` 绕过：这样改平台不知道，这里显示的值就和实际对不上，以后重装或升级还可能被盖掉。用户坚持要换某一项时，停下说明现在没有稳妥的改法，由用户决定怎么办，不要替他动手。应用自己后台里能改的（例如应用的管理员密码）由用户在应用里改；改了以后这里显示的仍是安装时的值。

**几种情况：**

- 用旧格式的清单装的应用（v0.5.x 时从应用中心装的，`notes` 里写着是旧格式装的；`acornfox --json status 应用名` 的 `legacy_note`、控制台应用详情里也有同样一句说明）：照常运行，能启动、停止、删除，但不接服务（`status` 里没有“它用的服务”，自检也不查它），这一版的新功能（看计划、接服务）用不上。用户要用新功能，只能删掉（保留数据）再从目录装回来，见“换应用版本、删了再接上旧数据重装”：**先问用户**，而且要等目录里这个应用有了新格式的版本（安装时报清单是旧格式，就是还没有，告诉用户，不要反复试）。不急的话不用处理。
- 清单没存（`manifest.saved` 是 `false`，`notes` 里有“清单没存，分不出类别”）：这个应用是 v0.5.1 之前装的，那时不保存清单，升级后本机也找不到它那一版的应用包，所以补不上。应用照常运行，不用处理。这时 `class` 都是空，自定义变量分不出哪些是密码，**一律按密码遮住**（用户名也会被遮住）。照实告诉用户“这个应用装得早，只能列出保存的值，分不出类别”，不要猜。
- `origin` 是 `install_time`：旧版本装的应用，那时不记每个值是谁给的，分不出哪些是用户改过的。照实说“装时的值”，不要说成“默认值”或“用户填的”。
- `from_catalog` 是 `true` 但 `vars` 是空：应用还在安装，等安装任务结束再查。
- `CAT-INST-22`：没有这个应用，`acornfox --json apps` 看名字。
- `CAT-INST-20`：保存的参数解不开。不要自己想办法，把原始输出交给用户。
- 这里显示的是**安装时保存的值**，不是从容器里现读的。用户怀疑应用里实际的值和它不一样时，照实说明这一点。`--json` 里的 `env`（v0.6.0 起）是写进应用的每个环境变量算好以后的值，保密的遮住、只给 `sha256` 指纹，核对用，不念给用户。

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

`acornfox --json delete 应用名 [--volumes]` 和 `remove … --volumes` 会删除应用或数据，执行前必须得到用户明确同意。一定写上应用名（不写时删的是当前目录对应的应用）；不加 `--volumes` 时数据卷保留，以后同名重装会被 `CAT-INST-24` 拦下，用户要接着用旧数据时加 `--reuse-data`。接着数据库应用提供的服务（`source` 是 `app`）的应用，加 `--volumes` 时提供方里给它开的库也一起删，不加时库和账号都留着；接的是用户自己登记的数据库时，那边的库不会被动（见“从应用中心装应用”里“必须先停下问用户的”最后一条）。

删除被拒、`diagnosis.code` 是 `app_in_use`：别的应用接着它提供的服务（例如数据库应用还有应用在用它；`message` 写着是哪些应用、哪条服务）。不要硬删，也不要自己先删那些应用；把 `message` 转述给用户，问他是先删那些应用（连数据删时会收回发给它们的凭据），还是不删了，用户同意后按顺序删。保不保留数据都一样：只要还有装着的应用接着它，就删不了。

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

绑好后（v0.6.0 起）`acornfox --json status 应用名` 里的地址是 `https://域名`，告诉用户用这个地址打开。从应用中心装的应用，装的时候写进应用的地址不会跟着变成域名（`apps vars` 里标 `APP-VARS-01`），用户问起照“查一个应用是用什么设置装的”第 4 步说。

## 平台域名（整台服务器一个域名）

给整台服务器配一个域名（例如 `apps.example.com`），访问方式是“公网”的应用自动多一个地址 `https://应用名.平台域名`，证书自动申请，不用记端口。应用自己绑的域名（上一节）优先；原来带端口的地址照常能用；只本机、局域网、只在内部网络的应用不受影响。设了平台域名**不会**把应用改成公网，改不改仍由用户在每个应用上定（见“对外开放”的规矩）。用户说“给这台服务器配个域名”“所有应用都用域名打开”时用这一节；只想给一个应用绑域名用上一节。

**最低路径（照着做）：**

1. 运行 `acornfox --json domain base`，看现状：
   - `supported` 是 `false`（`unsupported.code` 是 `PDOMAIN-02`）：原样告诉用户 `unsupported.text`，再说“要在外面打开应用，可以用 `acornfox open 应用名`”。到此为止。
   - `state` 是 `unset`：接第 2 步。
   - `domain` 已经有值：用户要换成别的或清掉，跳到下面“必须先问用户的”。
2. 问用户要用哪个域名（要是他自己的、能改解析的域名，通常是一个子域，例如 `apps.example.com`）。拿到后运行 `acornfox --json domain base set 域名 --plan`（只看会怎样，什么都不改），从输出里取 `record`，照这样告诉用户：
   > 请到你买域名的地方加一条记录：名字 `<record.name>`、类型 `<record.type>`、指向 `<record.value>`（完整写法 `<record.full>`）。中国大陆的服务器，这个域名要先备案。加好了告诉我。

   `--plan` 返回 `PDOMAIN-05`：域名写法不对，照 `hint` 请用户只给域名本身。
3. 用户说加好了，运行 `acornfox --json domain base set 域名`。
4. 看输出的 `checks`（`dns` 域名指过来没有、`ports` 外面能不能连上 80 和 443、`cert` 证书）和 `state`：
   - `state` 是 `ok`：告诉用户平台域名配好了，`apps` 里 `active` 是 `true` 的应用现在用 `https://应用名.平台域名` 打开（原样念 `apps[].url`）。有一项 `waiting` 是 `true` 时把它的 `text` 也告诉用户（例如“还没有开放到公网的应用，开放一个以后再检查”“证书正在自动申请，几分钟后再检查”），这不是故障。
   - `state` 是 `problem`：把 `problem.text`、`problem.detail`、`problem.next` 原样告诉用户，按编号处理：

| `problem.code` | 怎么办 |
| --- | --- |
| `PDOMAIN-01` | 记录还没加好、没生效或指错了：照 `problem.next` 请用户改，几分钟后再运行 `acornfox --json domain base` 重新检查 |
| `PDOMAIN-03` | 外面连不上 80 或 443：先 `sudo acornfox doctor` 看入口代理和端口占用；安全组、防火墙**问用户**同意后由用户放行 TCP 80、443 |
| `PDOMAIN-04` | 证书没签下来：大陆服务器先问域名备案了没有；只读看 `sudo docker logs --tail 50 acornfox-edge`，把和证书有关的几行告诉用户 |
| 其他编号 | `acornfox faults 编号 --json`，照它做 |

5. 某个应用在 `apps` 里 `active` 是 `false`：`note` 是“改成公网后自动生效”的，说明它现在不是公网、不用处理（要开放到公网照“对外开放”的规矩先问用户）；`note` 带 `PDOMAIN-06` 的，原样转述，办法是给它单独绑一个域名（上一节）。

**必须先问用户的：**

- **换掉**已有的平台域名、**清掉**平台域名：所有应用在旧域名下的地址都会打不开。先运行 `acornfox --json domain base set 新域名 --plan`（清掉是 `acornfox --json domain base clear --plan`），把 `plan.say` 和每个应用 `url` → `url_after` 的变化告诉用户，用户明确同意后再加 `--yes` 运行。不带 `--yes` 会被拒绝（`PDOMAIN-07`），**不要**自己加 `--yes`。
- 放行安全组、防火墙，改服务器地址。

**可以发挥的地方：** 解释域名、记录、证书是什么；帮用户判断用哪个子域；用户已经给各应用单独绑了域名时，说明两者可以并存。

装的时候写进应用里的地址不会跟着平台域名变（`apps vars` 里标 `APP-VARS-01`），用户问起照“查一个应用是用什么设置装的”第 4 步说。

## 网页控制台

控制台只在服务器本机监听，不对外开放。

- 情况乙：你（AI 工作台）在用户电脑上执行 `acornfox --json open --detach`：它经 SSH 转发、打开浏览器，隧道留在后台，命令马上结束（v0.6.2 起；输出怎么看见“第 7 步之后：打开控制台”）。用完关掉用 `acornfox open --stop`（先问用户）。用户自己在终端里用时是 `acornfox open`，看完按 Ctrl+C 关闭；你不要运行不带 `--detach` 的 `acornfox open`，它会一直占着前台。
- 情况甲：请用户在**他自己电脑**的终端运行 `ssh -L 18800:127.0.0.1:18800 用户@地址` 并保持不关；然后你在服务器上执行 `acornfox open --no-browser`（不加 `--json`），把输出的登录链接告诉用户在自己电脑的浏览器里打开。链接一次性、60 秒内有效，过期就再执行一次。用户电脑是 Linux、Mac 时，更省事的做法是在他电脑上装 CLI，按情况乙第 7 步登记后用 `acornfox open`。
- 情况丙：同情况甲，请用户在 Windows 的终端（Git Bash 或 PowerShell）运行 `ssh -L 18800:127.0.0.1:18800 用户@地址` 并保持不关；你执行 `ssh 用户@地址 'acornfox open --no-browser'`，把输出的登录链接告诉用户在浏览器里打开。

打开某个应用的页面（不是 AcornFox 控制台）：情况乙在用户电脑上运行 `acornfox open 应用名`，同样经 SSH 隧道、按 Ctrl+C 关闭；只本机的应用也能这样打开，不用改对外方式、不用放行安全组。完整步骤见“从应用中心装应用”里的“装好后怎么打开”。

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

**用户自己在控制台页面上开关**（服务器上的命令照旧能用，不用密码）：控制台页面“设置 › 访问 › 在外面打开控制台”里有“开启公网控制台”和“关闭公网控制台”按钮。默认仍然不开；用户没说要开，不要劝他去点。页面上开之前会弹确认框，并要**再输一次这次登录用的控制台密码**（从局域网控制台登录的输局域网控制台密码；从公网控制台登录的输公网控制台密码，而且那里只能关、不能开或换地址；用 `acornfox open` 进来的不用输）。开启成功后新密码只在页面上显示这一次，请用户当场存进密码管理器；**不要让用户把密码发给你**。配了平台域名时，地址框默认填 `console.平台域名`，也可以换成自己的域名。页面检查域名没通过时给的编号和上面表里一样；另外几个编号：

| code | 告诉用户 |
| --- | --- |
| `PUBCON-01` | 密码没输或不对：输这次登录用的那个控制台密码再试 |
| `PUBCON-02` | 输错太多次，按提示等几分钟再试，等的时候别反复点 |
| `PUBCON-03` | 这台机器在局域网里，没有公网地址，开不了；在局域网里用局域网控制台，人在外面用 `acornfox open` |
| `PUBCON-04` | 现在是从公网控制台登录的，只能关闭；要换地址就从局域网控制台或 `acornfox open` 进来再开 |

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

**升级要先问用户**（升级期间命令行和控制台会暂时连不上，公网控制台约 4 秒打不开）。从 0.6.0 之前的版本第一次升上来时会换一次入口代理容器：新建的访问不受影响，但已经连着的长连接、超过 8 秒还没做完的请求和 WebSocket 会断一次，问用户时把这一句也说上，建议避开访问高峰。用户同意后在服务器上执行（国内、海外二选一；不要加 `-y`）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- upgrade
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
| `故障编号: UPGRADE-OLD-01`（`现在装的是 v0.3.x`） | 服务没停、什么都没改。要分两次升：先问用户，同意后运行上面的升级命令并在 `upgrade` 后面加 `--version v0.5.1`（例如 `… \| sudo bash -s -- upgrade --version v0.5.1`），看到 `升级完成！` 后再运行一次不带 `--version` 的升级命令 |
| `故障编号: EDGE-02`（入口镜像…） | 停服务之前就停下了，什么都没改。按“安装故障编号”里 `EDGE-02` 那一行处理，好了再运行一次升级命令 |
| `故障编号: EDGE-05`（`入口没有换成新的`） | 新的入口代理没起来，原来的一直在服务，脚本已自动恢复到升级前，应用照常能打开。告诉用户现在仍是原来的版本，把脚本最后 30 行和 `sudo journalctl -u acornfox-server -n 50 --no-pager` 的输出交给用户反馈给维护者；再升级之前先问用户 |
| `故障编号: EDGE-06`（`没能换回原来的入口容器`） | 恢复到升级前时入口代理没换回来，应用网址可能打不开。**停下**，运行 `acornfox faults EDGE-06 --json`，把它的 `steps` 和输出原样给用户；里面的 `docker rm -f`、`docker start` 要用户同意后才做（或交给用户自己做）。不要删备份、不要重装 |
| `故障编号: UPGRADE-RESTORE-01` | 恢复到升级前时文件没能换回去，或者换回去之后服务还是起不来。应用容器没停，一般还能打开，但控制台和命令行用不了。**停下**，把输出原样给用户，请用户把 `journalctl -u acornfox-server -n 50 --no-pager`、`journalctl -u acornfox-runner -n 50 --no-pager` 的结果反馈给维护者。不要删备份、不要重装、不要自己拷备份里的文件 |

**卸载只在用户明确要求时做。** 默认保留数据；`--purge` 会连数据一起删除且不可恢复，必须用户明确说要删数据。卸载脚本要在终端里输入 `yes` 确认，请用户在自己的终端里运行。经 ssh 执行、没有终端时，脚本报 `UNINSTALL-TTY-01` 退出，什么都没改；只有用户明确要你代为卸载时才加 `-y`（`sudo bash get.sh uninstall -y`）。

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh -o get.sh && sudo bash get.sh uninstall
```

国内把地址换成 `https://gitee.com/VIP13390/AcornFox/raw/main/get.sh`。

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
