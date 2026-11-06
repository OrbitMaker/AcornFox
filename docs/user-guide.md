# AcornFox 单机版使用手册

适用版本：0.4.0 起。

> 支持的平台：
>
> - **Linux 服务器**：正式支持。AcornFox 服务和命令行都装在服务器上。
> - **Mac**：可以装命令行（CLI），当客户端经 SSH 管理服务器，见“Mac 电脑：当命令行客户端”。
> - **Windows**：本版本不提供程序。用 SSH 登录服务器，在服务器上运行 `acornfox`，见“Windows 电脑：经 SSH 在服务器上操作”。
> - 把 AcornFox 服务装在 Mac、Windows 电脑本机运行：**即将支持**。

## 先了解三件事

1. **一个程序管全部。** 服务器上装的是一个 `acornfox` 程序。它常驻后台，通过 Docker 管理你的应用；命令行、网页控制台和 AI 工作台都通过它操作。
2. **应用都跑在 Docker 里。** 应用的数据放在 Docker 数据卷里，不占你的用户目录。网址入口由 Caddy 负责，Caddy 也是一个容器（名字叫 `acornfox-caddy`），由 acornfox 自动管理。
3. **默认谁能打开。**

| | Linux 服务器 | Mac、Windows 电脑本机（即将支持） |
| --- | --- | --- |
| 应用 | 服务器地址 + 端口，可绑域名并自动签发 HTTPS 证书 | 本机 `http://localhost:端口`，局域网 `http://电脑的局域网地址:端口` |
| 控制台 | 只在服务器本机；用 `acornfox open` 经 SSH 打开 | 只在本机；`acornfox open` 直接打开 |
| 局域网打开控制台 | 手动开启，要密码 | 手动开启，要密码 |

每个应用都可以单独改“暴露方式”：

| 暴露方式 | 意思 | 怎么设 |
| --- | --- | --- |
| `local` | 只有这台机器能打开 | `acornfox app set --expose local` |
| `lan` | 这台机器和局域网都能打开（Mac、Windows 的默认值） | `acornfox app set --expose lan` |
| `public` | 所有网卡，可以绑域名（Linux 的默认值） | `acornfox app set --expose public` |
| `default` | 恢复本系统的默认值 | `acornfox app set --expose default` |

还有一种“经 AFX 云”（afx_cloud）的方式，目前只预留了名字，还不能用。

---

## Linux 服务器

支持 Ubuntu 22.04、24.04 和 Debian 12、13（x86_64），推荐 Ubuntu 24.04 或 Debian 13。这四个版本都实测过全新安装、自检和部署；从 v0.3.x 升级和回滚目前只在 Ubuntu 24.04 上实测过。arm64 还没测过。需要 root 或 sudo，建议 2 核 2 GB 以上。

**买国内云服务器时，系统请选 Ubuntu 24.04（或 Debian 13）。** 阿里云默认的 Alibaba Cloud Linux、腾讯云默认的 OpenCloudOS / TencentOS、火山引擎默认的 veLinux，以及 CentOS、Rocky 等系统装不上，安装脚本会停下并提示“……不能安装 AcornFox”（故障编号 `INSTALL-OS-01`）。已经买了的，可以在云厂商控制台“更换操作系统”，换成 Ubuntu 或 Debian（会清空系统盘）。

端口要求（安装脚本会先检查，被占时停下并说明是哪个程序占的）：

- 18800、18801、18808 是 AcornFox 自己用的本机端口，18810-18899 是应用端口，都必须空着，不能跳过（故障编号 `INSTALL-PORT-01`、`INSTALL-PORT-03`）。
- 80、443 最好空着。已经被宝塔、1Panel、自己装的 Nginx 占用时，脚本会停下（故障编号 `INSTALL-PORT-02`），可以二选一：停掉占用的程序再装；或者不停，加 `--allow-busy-80-443` 和它们共存。共存时应用照常用 `http://服务器IP:端口` 打开，但不能在 AcornFox 里绑定域名、开 HTTPS 或开公网控制台；要用域名，就在原来的面板里把域名反向代理到 `127.0.0.1:应用端口`。

### 安装

中国大陆服务器：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install.sh | sudo bash
```

其他地区：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install.sh | sudo bash
```

- 脚本会装好 Docker、acornfox，并做好 Caddy 镜像（`acornfox/caddy:2.8.4`，不需要访问镜像仓库）。
- 要多久：云服务器上通常 3～10 分钟；家用宽带、海外网络差、或者国内服务器连境外时，可能要 15～40 分钟，主要花在更新软件源和下载 Docker 上。“安装 Docker...”这一步会好几分钟没有新输出，是在下载，不是卡住。
- 装完会显示服务状态，其中包括 `Caddy 容器 acornfox-caddy: running`，以及管理员令牌（也保存在 `/root/.acornfox-token`）。
- 下载源自动选：服务器在阿里云、腾讯云、火山引擎上时（不论哪个地域），系统软件源和 Docker 安装包先用这家云的内网源，连不上再换下一个。中国大陆的服务器（包括华为云）再依次用华为等国内公共镜像站，并配好 Docker 镜像加速（腾讯云大陆地域优先用它的 Docker Hub 内网加速）。香港、新加坡、东京等境外地域按海外处理：从 GitHub 下载，不配国内镜像站和 Docker 镜像加速，Docker 安装包在内网源连不上时用官方源。判断错了可以加参数指定：`curl … | sudo bash -s -- --china` 或 `--global`。
- 云服务器要在安全组放行入方向 TCP 18810-18899（应用端口）；要绑域名的话，再放行 80 和 443。安装脚本最后也会提示。

### 首次打开控制台

在你自己的电脑（Linux 或 Mac）上先装命令行工具。Windows 本版本没有命令行程序，跳过这一节，看下文“Windows 电脑：经 SSH 在服务器上操作”。中国大陆：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install-cli.sh | bash
```

其他地区：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install-cli.sh | bash
```

再添加服务器。第一次连接前，先在同一台电脑上用 `ssh root@服务器地址` 登录一次：它会显示服务器指纹（`SHA256:…`）并问是否继续，核对后输入 `yes`。核对方法：在云厂商控制台的“远程连接”（网页终端）里运行 `for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf "$f"; done`，比对 `ED25519` 那一行。不确认指纹，下面的命令会报“服务器主机指纹未确认”（`host_key_unknown`）。

```bash
acornfox target add my-server --ssh root@服务器地址
```

- SSH 用户不是 root 时，先在服务器上执行一次 `sudo usermod -aG acornfox-users 用户名`。
- 端口不是 22 加 `--port 端口`，用指定私钥加 `--identity 私钥路径`。
- 服务器要经跳板机才能进时，在 `~/.ssh/config` 里写一个 `Host` 别名（`HostName`、`User`、`ProxyJump 跳板机用户@跳板机地址`），`--ssh` 后面写这个别名。第一次用 `ssh 别名` 登录时会先后确认跳板机和服务器两个指纹。

```bash
acornfox open
```

浏览器会自动打开控制台。控制台不对外开放，是经 SSH 转发到你电脑上的。

### 部署应用

在项目目录里：

```bash
acornfox deploy
```

也可以部署现成镜像，或者公开的 Git 仓库：

```bash
acornfox deploy --image nginx:1.27-alpine --port 80
```

```bash
acornfox deploy --git https://gitee.com/你的账号/你的仓库.git
```

部署成功后会显示网址。

改了环境变量或设置、代码没变时，用 `acornfox redeploy` 按当前程序重新部署一次，让改动生效。它和 `deploy` 一样等到上线或失败才返回；加 `--no-wait` 只提交、不等待。

### 加数据库

```bash
acornfox add postgres
```

`mysql`、`redis` 的用法一样。连接信息会以 `DATABASE_URL`、`REDIS_URL` 环境变量自动交给应用，应用会自动重新部署一次。同一个应用只能有一个 `DATABASE_URL`，也就是 PostgreSQL 和 MySQL 只能二选一。

### 版本记录、回退和旧版本日志

```bash
acornfox versions --app web            # 每一版的状态、来源、用时、失败原因，哪些可以回退
acornfox rollback --to 7 --app web     # 回退到第 7 版
acornfox rollback --app web            # 回退到上一个可回退的版本
acornfox logs --version 6 --app web    # 第 6 版的运行日志；加 --build 看构建日志
```

- 回退只换程序，不重新构建。数据库、数据目录和环境变量保持现在的样子，不会回到那一版时的状态。
- 新版本通过健康检查后才切换流量；失败时当前版本继续服务。
- 服务器保留除当前版本外最近 5 个不同的程序，可以回到其中任何一版；更早的版本只保留记录和日志，`versions` 里显示“太旧，不能一键回退”。
- 有部署正在进行时，回退会提示先取消它；确认要取消就加 `--cancel-pending`。
- 每个应用保留最近 20 次部署的日志（运行日志最多 1 MB，构建日志最多 512 KB），当前版本、能回退的版本和最近一次失败的一直保留。升级到这个版本之前部署的版本没有保存日志。
- 日志里的密钥值、网址里的账号密码会被替换成 `******`。写死在 Dockerfile 里的密钥只能按这类格式兜底，不保证都能识别，密钥请用 `acornfox env set KEY=VALUE --secret`。
- 命令要写成 `acornfox rollback --to 7 --app web`（应用用 `--app` 指定）。旧版本的 CLI 不认识 `--to`，会直接报错，不会回退到别的版本。

### 局域网访问

应用默认在所有网卡上可以打开：`http://服务器地址:端口`。只想让服务器本机访问的话：

```bash
acornfox app set --expose local
```

在局域网打开控制台（要密码）：

```bash
acornfox lan-console enable
```

命令会显示地址（`http://服务器地址:18802`）和一次性显示的密码。用完关闭：

```bash
acornfox lan-console disable
```

公网服务器上不建议开局域网控制台；要从公网管理，请用 `acornfox public-console enable 域名`，它带 HTTPS。

### 升级

中国大陆服务器：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/upgrade.sh | sudo bash
```

其他地区：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh | sudo bash
```

通过管道运行（包括 AI 工作台代为执行）就视为同意升级，脚本开头会先列出要做的事。已经是最新版本时直接退出，不停服务、不重装，重复运行也没关系。只有要重装同一版本时才加 `-y`（`| sudo bash -s -- -y`）：重装也会新建一份备份，之后 `--rollback` 回到的是重装前的同一版本，不再是更早的版本。

- 先下载新版本并校验 SHA256；下载失败不会动现有安装。
- 然后停止 AcornFox 服务，备份程序和数据库（放在 `/var/lib/acornfox/backups/`，保留最近 5 份），再执行数据库迁移。
- 数据库迁移失败、或者新版本的服务启动不了时，脚本自动换回原来的程序和数据库。其他问题（例如 Caddy 容器没起来）只提示，不自动回退，需要时按下面的“回滚”手动回退。
- 应用容器在升级过程中一直运行。升级期间命令行和控制台会暂时连不上。
- 从 v0.3.x 升级时，原来装在系统里的 Caddy 会被停用，证书复制到数据卷，之后由 Caddy 容器接管 80、443 和应用端口。**换过去的那一下应用网址会打不开几秒**（实测约 2～3 秒），请避开访问高峰。应用和数据库本身不受影响。
- 请同时升级你电脑（Linux、Mac）上的 CLI（重新运行一次安装 CLI 的命令）；Windows 用户在服务器上运行的命令行随升级一起换了，不用另外升级。新版 CLI 连到没升级的服务器时，`rollback --to`、`versions`、`logs --version` 会提示“服务器上的 AcornFox 版本较旧”，不会执行。

### 回滚

回到最近一次升级前的版本。先把升级脚本下载到服务器上，再运行（国内把地址换成上面 Gitee 的 `upgrade.sh` 地址）：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh -o upgrade.sh
```

```bash
sudo bash upgrade.sh --rollback
```

程序和数据库都回到升级前的版本。如果是回滚到 v0.3.x，脚本会删掉 Caddy 容器，并重新启用系统里的 Caddy，换的那一下应用网址同样会打不开几秒（实测约 3 秒）。

**回滚前要知道：**

- 回滚会把控制面数据库（应用列表、版本记录、环境变量、域名等设置）恢复到升级前的样子。**升级之后新建的应用、设的环境变量、绑的域名都会从列表里消失**；回到 v0.3.x 时，它不认识的应用容器也会被删掉。
- 这些应用的数据卷**不会删**，还留在磁盘上（`docker volume ls` 能看到，名字以 `af-应用名-` 开头），可以重新部署后手工把数据找回来。
- 升级之前就有的应用、数据卷和数据库不受影响。
- 要回到 v0.3.x，只能用 `--rollback` 回到升级前的那份备份。**不支持用 `--version v0.3.x` 降级**：旧版本的程序不认识新版本的数据库，会失败。

回滚按顺序找这次升级的备份：先看 `/tmp/acornfox_last_backup`，再看 `/var/lib/acornfox/backups/.last`，所以服务器重启过也能直接回滚。要回到更早的某一份备份时，先看有哪些（目录名是升级时间，里面有 `version.txt`），再指定要用的那一份：

```bash
ls /var/lib/acornfox/backups/
```

```bash
echo /var/lib/acornfox/backups/20261105_103000 | sudo tee /tmp/acornfox_last_backup
```

把 `20261105_103000` 换成上一条命令列出的目录名。

### 卸载

先把卸载脚本下载到服务器上（国内用 `https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/uninstall.sh`）：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/uninstall.sh -o uninstall.sh
```

```bash
sudo bash uninstall.sh
```

脚本会要求输入 `yes` 确认。默认保留数据（`/var/lib/acornfox` 和数据卷），重新安装后应用会恢复。要全部删除（不可恢复）：

```bash
sudo bash uninstall.sh --purge
```

加 `-y` 不再询问确认。Docker 本身不会被卸载。

---

## Mac 电脑：当命令行客户端

本版本的 Mac 程序只当客户端：装在你的 Mac 上，经 SSH 管理 Linux 服务器上的 AcornFox。需要 macOS 12 以上，苹果芯片或 Intel 都可以，不需要 Docker，也不需要管理员密码。

做法和上面“Linux 服务器 → 首次打开控制台”一样：

1. 装命令行工具（中国大陆用 Gitee 地址，其他地区用 GitHub 地址）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install-cli.sh | bash
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install-cli.sh | bash
```

2. 先用 `ssh 用户@服务器地址` 登录一次、核对指纹，再添加服务器：`acornfox target add my-server --ssh 用户@服务器地址`。
3. 之后 `acornfox open`、`acornfox deploy`、`acornfox add postgres` 等命令都在 Mac 上运行，作用在服务器上。部署时把当前目录的项目打包传到服务器构建。

升级服务器后，在 Mac 上重新运行一次安装 CLI 的命令，让两边同版本。

**在 Mac 本机运行 AcornFox（`acornfox install`、`acornfox setup`，应用跑在这台 Mac 上）即将支持，本版本请不要使用。**

---

## Windows 电脑：经 SSH 在服务器上操作

本版本没有 Windows 版程序（在 Git Bash 里运行 `install-cli.sh` 只会说明这一点并退出）。按上面“Linux 服务器”装好 AcornFox 后，服务器上已经有 `acornfox` 命令，用 SSH 登录服务器运行它就行。Windows 10、11 自带 `ssh` 和 `scp`，在 PowerShell 里就能用；让 AI 工作台按 Skill 代你操作时，请装 Git for Windows，工作台在 Git Bash 里执行。

**把 AcornFox 服务装在 Windows 电脑本机运行，即将支持。**

### 第一次：登记

1. 确认能用密钥免密登录服务器（第一次连接会让你核对服务器指纹，核对方法见上文“首次打开控制台”）：

```bash
ssh 用户@服务器地址 'echo ok'
```

2. SSH 用户不是 root 时，把它加进 `acornfox-users` 组（只做一次；要输 sudo 密码时在这里输入）：

```bash
ssh -t 用户@服务器地址 'sudo usermod -aG acornfox-users $(id -un)'
```

3. 在服务器上登记本机服务：

```bash
ssh 用户@服务器地址 'acornfox target add local --url unix:///run/acornfox/api.sock'
```

看到“已添加服务器 local，连接测试通过”就好了。加组后新开的 SSH 连接立即生效，不用重新登录服务器。

### 之后怎么运行命令

两种写法，效果一样：

- 登录到服务器上再敲命令：先 `ssh 用户@服务器地址`，之后和“Linux 服务器”一节一样直接运行 `acornfox …`。
- 一条一条经 SSH 运行（AI 工作台这样做）：`ssh 用户@服务器地址 'acornfox status'`。经 SSH 运行时当前目录是服务器上的家目录；要对某个应用操作，先 `cd` 到部署它的目录，或者加 `--app 应用名`，例如 `ssh 用户@服务器地址 'cd ~/acornfox-apps/myapp && acornfox logs'`。

### 部署

现成镜像、公开 Git 仓库和 Linux 上一样，不用传文件：

```bash
ssh 用户@服务器地址 'mkdir -p ~/acornfox-hello && cd ~/acornfox-hello && acornfox deploy --image nginx:1.27-alpine --port 80 --app hello'
```

你电脑上文件夹里的项目（根目录有 Dockerfile）：先把文件夹传到服务器，再在服务器上那个目录里部署。下面以 `C:\Users\me\myapp` 为例，传上去后服务器上的目录是 `~/acornfox-apps/myapp`：

```bash
ssh 用户@服务器地址 'mkdir -p ~/acornfox-apps'
scp -r C:\Users\me\myapp 用户@服务器地址:acornfox-apps/
ssh 用户@服务器地址 'cd ~/acornfox-apps/myapp && acornfox deploy'
```

- 在 Git Bash 里，文件夹路径写成 `/c/Users/me/myapp`。
- SSH 端口不是 22 时，`ssh` 加 `-p 端口`，`scp` 加大写的 `-P 端口`。
- 改了代码要重新部署：再传一次、再 `deploy` 一次。电脑上删掉的文件，服务器上那份不会跟着删。
- 文件夹里有很大的依赖目录（例如 `node_modules`）时，传之前先删掉它，或者用 Git 把代码推到仓库，在服务器上用 `--git` 部署。

### 打开控制台

在一个终端里运行下面这条并保持不关（它把服务器的控制台端口转到你电脑上）：

```bash
ssh -L 18800:127.0.0.1:18800 用户@服务器地址
```

再开一个终端运行：

```bash
ssh 用户@服务器地址 'acornfox open --no-browser'
```

在浏览器里打开它输出的登录链接（一次性，60 秒内有效，过期就再运行一次）。

### 升级

升级服务器（见“Linux 服务器 → 升级”）时，服务器上的命令行一起升级，Windows 电脑上不用做什么。

---

## 常见问题

**镜像下载很慢或卡住。**
国内网络会直接走国内镜像源：Docker Hub 上的镜像依次用 `docker.m.daocloud.io`、`docker.1panel.live`，GitHub 容器镜像（`ghcr.io`）用 `ghcr.nju.edu.cn`。任何一个源 45 秒没有进展，就换下一个源。想指定镜像源，可以在启动前设置环境变量 `ACORNFOX_REGISTRY_MIRRORS`（Docker Hub）或 `ACORNFOX_GHCR_MIRRORS`（ghcr.io），多个用逗号分隔，设为空表示不用镜像源。Linux 服务器上，安装脚本选国内时已经给 Docker 配好了国内镜像源。

**部署时报“整机分配给应用的内存不够”。**
所有应用和数据库的内存上限加起来，不能超过“整机上限”。新应用默认 256 MB，PostgreSQL、MySQL 各 512 MB，Redis 256 MB。整机不到 2 GB 时，上限是整机内存的 2 倍（允许超分，因为上限只是封顶，实际用不了这么多）；2 GB 及以上，上限是整机内存减去 max(1 GB, 15%)。看现在分了多少：

```bash
acornfox limits
```

可以把整机上限调大（小机器最多到整机的 2 倍），或者把应用的内存调小：

```bash
acornfox limits --memory 1800
```

```bash
acornfox app set --memory 256
```

**80 或 443 端口被占用（Linux）。**
安装脚本停下，故障编号 `INSTALL-PORT-02`。Caddy 容器用宿主网络，默认要占用 80、443 和应用端口（18810-18899）。机器上已有 Nginx、宝塔面板等网站服务的话，二选一：停掉它们再安装；或者加 `--allow-busy-80-443` 共存（`curl … | sudo bash -s -- --allow-busy-80-443`），共存时不能在 AcornFox 里绑定域名、开 HTTPS 和公网控制台，域名继续在原来的面板里反向代理到 `127.0.0.1:应用端口`。18800、18801、18808 和 18810-18899 被占（`INSTALL-PORT-01`、`INSTALL-PORT-03`）不能跳过，要先让出来。查看谁占了端口：`sudo ss -ltnp | grep -E ':(80|443|188[0-9][0-9]) '`。

**安装时提示“不能安装 AcornFox”或“不在支持列表”（故障编号 INSTALL-OS-01、INSTALL-OS-02）。**

- `INSTALL-OS-01`：系统不是 Ubuntu/Debian，常见于国内云的默认系统 Alibaba Cloud Linux、OpenCloudOS、TencentOS、veLinux，以及 CentOS 等。到云厂商控制台把系统换成 Ubuntu 24.04 或 Debian 13（会清空系统盘），再重新安装。
- `INSTALL-OS-02`：是 Ubuntu/Debian，但版本不在支持列表（例如 Ubuntu 20.04、Debian 11）。建议换成 Ubuntu 24.04 或 Debian 13。一定要在这个版本上试，可以加 `--force-os`：`curl … | sudo bash -s -- --force-os`，可能遇到问题，自行承担风险。

**提示“连不上本机的 acornfox 服务”。**
后台程序没在运行。在服务器上运行 `sudo acornfox doctor` 检查，按提示处理；一般 `sudo systemctl restart acornfox-runner acornfox-server` 就能重新启动它。

**想看 Caddy 的日志。**

```bash
docker logs acornfox-caddy
```

**后台程序的日志在哪。**
在服务器上：`sudo journalctl -u acornfox-server`（构建和运行容器的部分在 `sudo journalctl -u acornfox-runner`）。

**Caddy 的管理接口安全吗。**
Caddy 的管理接口只开在容器内部。acornfox 通过本机 127.0.0.1:18808 上的一个小网关访问它，网关要求带令牌（令牌存在数据目录的 `caddy-admin.token`，只有 acornfox 能读）。没有令牌的请求一律返回 403。

---

