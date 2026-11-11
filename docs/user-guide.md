# AcornFox 单机版使用手册

适用版本：0.5.0 起。写着“v0.5.1 起”的要 0.5.1 以上的 `acornfox`；其中要连服务器的命令，服务器也要是 0.5.1（`install status --wait` 例外，连 0.5.0 的服务器也能用；`acornfox faults` 不连服务器）。

> 支持的平台：
>
> - **Linux 服务器**：正式支持。AcornFox 服务和命令行都装在服务器上。
> - **Mac**：可以装命令行（CLI），当客户端经 SSH 管理服务器，见“Mac 电脑：当命令行客户端”。
> - **Windows**：这一版不提供程序。用 SSH 登录服务器，在服务器上运行 `acornfox`，见“Windows 电脑：经 SSH 在服务器上操作”。
> - 把 AcornFox 服务装在 Mac、Windows 电脑本机运行：这一版不提供。

## 先了解三件事

1. **一个程序管全部。** 服务器上装的是一个 `acornfox` 程序。它常驻后台，通过 Docker 管理你的应用；命令行、网页控制台和 AI 工作台都通过它操作。
2. **应用都跑在 Docker 里。** 应用的数据放在 Docker 数据卷里，不占你的用户目录。网址入口由 Caddy 负责，Caddy 也是一个容器（名字叫 `acornfox-caddy`），由 acornfox 自动管理。
3. **默认谁能打开。**

| | 默认谁能打开 |
| --- | --- |
| 自己部署的应用（`acornfox deploy`） | 所有网卡：`http://服务器地址:端口`，可以绑域名并自动签发 HTTPS 证书 |
| 从应用中心装的应用 | 只有服务器本机（`http://127.0.0.1:端口`）。从自己电脑打开的办法见“应用中心 → 怎么打开只本机的应用” |
| 控制台 | 只有服务器本机；在你自己电脑上用 `acornfox open` 经 SSH 打开 |
| 局域网打开控制台 | 局域网里的服务器第一次安装时默认打开（v0.5.1 起，端口 18802），第一次打开时在浏览器里设密码；云主机开不了；其余情况默认关闭，可以手动打开 |

每个应用都可以单独改“暴露方式”：

| 暴露方式 | 意思 | 怎么设 |
| --- | --- | --- |
| `local` | 只有服务器本机能打开（从应用中心装的应用默认是这个） | `acornfox app set --app 应用名 --expose local` |
| `lan` | 局域网能打开。Linux 服务器上同样是在所有网卡上开放这个端口，公网能不能访问取决于防火墙和安全组 | `acornfox app set --app 应用名 --expose lan` |
| `public` | 所有网卡，可以绑域名（自己部署的应用默认是这个） | `acornfox app set --app 应用名 --expose public` |
| `default` | 恢复系统默认值。Linux 服务器上就是 `public`，从应用中心装的应用改成 `default` 也会变成所有网卡能打开；想改回只本机请用 `local` | `acornfox app set --app 应用名 --expose default` |

---

## Linux 服务器

支持 Ubuntu 22.04、24.04 和 Debian 12、13（x86_64），推荐 Ubuntu 24.04 或 Debian 13。这四个版本都实测过全新安装、自检和部署；从 v0.3.x 升级目前只在 Ubuntu 24.04 上实测过。arm64 还没测过。需要 root 或 sudo，建议 2 核 2 GB 以上。

**买国内云服务器时，系统请选 Ubuntu 24.04（或 Debian 13）。** 阿里云默认的 Alibaba Cloud Linux、腾讯云默认的 OpenCloudOS / TencentOS、火山引擎默认的 veLinux，以及 CentOS、Rocky 等系统装不上，安装脚本会停下并提示“……不能安装 AcornFox”（故障编号 `INSTALL-OS-01`）。已经买了的，可以在云厂商控制台“更换操作系统”，换成 Ubuntu 或 Debian（会清空系统盘）。

端口要求（安装脚本会先检查，被占时停下并说明是哪个程序占的）：

- 18800、18801、18808 是 AcornFox 自己用的本机端口，18810-18899 是应用端口，都必须空着，不能跳过（故障编号 `INSTALL-PORT-01`、`INSTALL-PORT-03`）。
- 18802 是局域网控制台的端口，被占时安装照常完成，只是不打开局域网控制台。
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
- 服务器在家里或办公室的局域网里（私网地址、不是云主机），并且是第一次安装时，装完会打开局域网控制台，屏幕上给出地址 `http://服务器局域网地址:18802`（v0.5.1 起），见下文“局域网访问”。不想打开就加 `--no-lan-console`：`curl … | sudo bash -s -- --no-lan-console`。

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

浏览器会自动打开控制台。控制台不对外开放，是经 SSH 转发到你电脑上的。控制台的主色在“设置 → 通用”里可以换（中性、蓝、绿、橙，记在当前浏览器里；v0.5.1 起）。

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

### 资源使用

```bash
acornfox stats                     # 整台服务器的 CPU、内存
acornfox stats web                 # 一个应用现在的 CPU、内存
acornfox stats web --history 1h    # 这个应用近 1 小时（或 24h）的 CPU、内存（v0.5.1 起）
```

- 应用的历史每 30 秒记一次，只存在内存里，最多 24 小时；后台服务重启（包括升级）后从头记，应用停着时不记。控制台“我的应用”里，应用概览最上面的内存、CPU 两张小图就是它。

### 局域网访问

自己部署的应用默认在所有网卡上可以打开：`http://服务器地址:端口`。只想让服务器本机访问的话（在部署它的目录里运行，或加 `--app 应用名`）：

```bash
acornfox app set --expose local
```

从应用中心装的应用默认只本机能打开，从自己电脑打开的办法见下文“应用中心 → 怎么打开只本机的应用”。

**局域网控制台**：让同一局域网里的电脑、手机用浏览器打开这台服务器的控制台（端口 18802）。下面是 v0.5.1 起的做法：

- 服务器在局域网里（私网地址、不是云主机）并且是第一次安装时，安装脚本默认已经打开它。没打开的（升级上来的、卸载时保留了数据又装回来的、安装时加了 `--no-lan-console` 的），在服务器上运行（用 `uninstall.sh --purge` 连数据删掉后再装，算第一次安装，会默认打开）：

  ```bash
  acornfox lan-console enable
  ```

- 第一次用浏览器打开 `http://服务器局域网地址:18802` 时，自己设一个密码（至少 8 位）。谁先打开谁设，设好以后别的电脑只能登录；所以装好后请尽快自己先打开设上。开着但还没设密码时，`acornfox doctor` 会提醒（`LAN-CONSOLE-03`）。
- 登录时可以勾“记住这台设备 90 天”，最多记 20 台，再多就挤掉最久没用的那台。控制台“设置”里能看到密码是什么时间、从哪个地址设的，也能移除记住的设备。
- 只认局域网里来的访问（10.x、172.16.x～172.31.x、192.168.x、100.64.x～100.127.x、169.254.x、IPv6 的局域网地址和本机），别的来源一律拒绝（`LAN-CONSOLE-02`）。云主机和公网服务器开不了（`LAN-CONSOLE-01`），请用 `acornfox open`；要从公网管理，用 `acornfox public-console enable 域名`，它带 HTTPS。
- 局域网里是明文 HTTP，只在可信的网络里用。

服务器上的命令（都可以加 `--json`）：

| 命令 | 做什么 |
| --- | --- |
| `acornfox lan-console status` | 开没开、地址、密码设了没有（什么时间、从哪个地址设的）、记住了几台设备。只读 |
| `acornfox lan-console enable` | 打开；已经开着就什么都不变 |
| `acornfox lan-console disable` | 关闭；密码、登录状态和记住的设备全部清掉 |
| `acornfox lan-console reset-password` | 清掉密码，所有登录和记住的设备一起作废，下一个从局域网打开的人重新设。忘了密码，或者 `status` 里设密码的时间、地址不是你，就用它 |
| `acornfox lan-console devices` | 列出记住的设备。只读 |
| `acornfox lan-console forget 编号` | 让一台记住的设备失效（它当前的登录也立刻失效）；`forget --all` 是全部 |

v0.5.0 时 `enable` 会生成一个密码；用那个版本打开的局域网控制台和它的密码，升级后照常能用。以前在云主机上打开过、从公网地址访问的，升级后会被拒绝，请改用 `acornfox open`。页面上还不能改密码，要改就 `reset-password` 后在浏览器里重设。

### 升级

中国大陆服务器：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/upgrade.sh | sudo bash
```

其他地区：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh | sudo bash
```

通过管道运行（包括 AI 工作台代为执行）就视为同意升级，脚本开头会先列出要做的事。已经是最新版本时直接退出，不停服务、不重装，重复运行也没关系。只有要重装同一版本时才加 `-y`（`| sudo bash -s -- -y`）。

- 先下载新版本并校验 SHA256；下载失败不会动现有安装。
- 然后停止 AcornFox 服务，备份程序和数据库（放在 `/var/lib/acornfox/backups/`，保留最近 5 份），再执行数据库迁移。
- 数据库迁移失败、新版本的服务起不来、或者升级中途出错（包括被 Ctrl+C 打断、SSH 断开）时，脚本用这份备份自动恢复到升级前：程序和数据库都换回原来的，服务照常运行，等于没升上。脚本最后打印 `故障编号: UPGRADE-FAIL-…` 和失败原因，可以稍后再试一次。恢复的过程不响应 Ctrl+C，请等它结束；这段过程另记在 `/var/log/acornfox-upgrade.log`，SSH 断开、看不到结果时，重新连上后用 `sudo tail -n 40 /var/log/acornfox-upgrade.log` 查看。
- 新版本的服务起来之后，其他问题（例如 Caddy 容器没起来）只提示，不再恢复到升级前。
- 应用容器在升级过程中一直运行。升级期间命令行和控制台会暂时连不上。
- 从 v0.3.x 升级时，原来装在系统里的 Caddy 会被停用，证书复制到数据卷，之后由 Caddy 容器接管 80、443 和应用端口。**换过去的那一下应用网址会打不开几秒**（实测约 2～3 秒），请避开访问高峰。应用和数据库本身不受影响。
- 请同时升级你电脑（Linux、Mac）上的 CLI（重新运行一次安装 CLI 的命令）；Windows 用户在服务器上运行的命令行随升级一起换了，不用另外升级。新版 CLI 连到没升级的服务器时，`rollback --to`、`versions`、`logs --version` 会提示“服务器上的 AcornFox 版本较旧”，不会执行。
- 先升级服务器，再升级电脑上的 CLI。应用中心的命令（`catalog`、`install 应用`）要服务器也是 0.5.0 以上才能用；`apps vars`、`stats --history` 要服务器是 0.5.1 以上；`install status --wait` 连 0.5.0 的服务器也能用；`faults` 不连服务器。
- 升级不会打开局域网控制台；原来开着的照常能用。
- 从 0.4.x 升级到 0.5.0 后，控制台换成新版（`acornfox open` 打开的就是新版），旧控制台已经去掉，旧地址 `/legacy/` 会跳到新控制台。

### 只能升级

AcornFox 本身只能升级，不能回到旧版本：

- 升级脚本没有 `--rollback`，用了会被拒绝（`故障编号: UPGRADE-ROLLBACK-01`）。
- `--version` 指定比现在旧的版本会被拒绝，加 `-y` 也一样（`故障编号: UPGRADE-DOWN-01`）。用 `install.sh` 在已经装好的服务器上装更旧的版本也会被拒绝（`故障编号: INSTALL-DOWN-01`）；卸载时保留了数据的，再装也不能装比这份数据旧的版本（同一个编号）。
- 这几种情况都在停服务之前停下，什么都不改。

升级没成功时脚本已经自动恢复到升级前（见上一节），不需要手动处理。升级后发现问题，请等有更新的版本时再升级，或者把问题和 `acornfox doctor` 的输出反馈给我们。

这里说的是 AcornFox 平台本身。**应用自己的版本照常可以回退**：`acornfox versions 应用名` 看有哪些版本，`acornfox rollback 应用名` 回到上一版（只换应用的程序，不动数据），和平台升级无关。

升级失败时的几个编号：

| 编号 | 意思 | 怎么办 |
| --- | --- | --- |
| `UPGRADE-BACKUP-01` | 停服务后备份程序或数据库没做完（常见是磁盘满了），没有升级，原来的服务已重新启动 | `df -h /var/lib/acornfox` 看空间，腾出空间后重新运行升级命令 |
| `UPGRADE-FAIL-01` | 新版本的数据库迁移失败，已恢复到升级前 | 稍后再试；再失败就把输出反馈给我们 |
| `UPGRADE-FAIL-02` | 新版本的服务起不来，已恢复到升级前 | 同上；输出里带了新版本起不来时的日志 |
| `UPGRADE-FAIL-03` | 升级中途出错或被打断，已恢复到升级前 | 重新运行升级命令 |
| `UPGRADE-RESTORE-01` | 恢复到升级前时文件没能换回去（这时不启动服务），或者已经换回去但服务还是起不来 | 应用容器没停，一般还能打开；把 `journalctl -u acornfox-server -n 50 --no-pager` 和 `journalctl -u acornfox-runner -n 50 --no-pager` 的输出反馈给我们。不要删 `/var/lib/acornfox/backups/` 里的备份，也不要重装 |

### 自检和故障编号

在服务器上运行：

```bash
sudo acornfox doctor
```

它检查 Docker、后台服务、网址入口（Caddy 容器）、磁盘空间、端口和时钟；v0.5.1 起还看下载网关能不能连上（`SETUP-GW-01`）和应用目录上次同步的情况（`SETUP-SYNC-01`，超过两天没同步会提醒）。每个问题带故障编号，加 `--json` 输出给 AI 工作台读。控制台的“运行环境”页是同一套检查。下载网关、应用目录这两项只给警告：网关不通时公共目录的应用照常能装（镜像改从公共源拉，会慢一些），目录旧一点也能装。下载网关一项要用 sudo 运行才查：普通用户读不到数据目录里的网关设置，看不出网关有没有被关掉，就不联系网关。

在任何地方看到的故障编号（安装和升级脚本打印的“故障编号: …”、命令出错时打印的“故障编号：…”、`doctor` 每项后面括号里的、命令加 `--json` 时输出里的 `code`、控制台“运行环境”页上的），都可以查它的完整说明（v0.5.1 起）：

```bash
acornfox faults CAT-INST-27      # 一条：是什么、可能的原因和怎么判断、按什么顺序查、怎么处理、什么时候要先问你、怎么确认好了
acornfox faults                  # 全部编号
acornfox faults --group SETUP    # 只看一组，组名在全部列表的最后
```

- 处理办法分三级：0 级只读；1 级可以直接做（不删数据、不花钱、不对外开放、做了能撤回）；2 级要你先同意。
- 这个命令只读，不连服务器，Mac 上的命令行也能查；目录编在程序里，和这个 `acornfox` 的版本一致。全文见 [故障目录](faults.md)。
- 查不到的编号（例如比你的程序新）按兜底条目 `ANY-00` 处理：先照出错输出里的 message 和 hint 做；还不行就把那条命令加 `--json` 的完整输出反馈给我们。

### 卸载

先把卸载脚本下载到服务器上（国内用 `https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/uninstall.sh`）：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/uninstall.sh -o uninstall.sh
```

```bash
sudo bash uninstall.sh
```

脚本会要求输入 `yes` 确认。默认保留数据（`/var/lib/acornfox` 和数据卷），重新安装后应用会恢复。保留数据时会记下这份数据的版本号，之后重新安装不能装比它旧的版本（旧程序读不了新数据库，`故障编号: INSTALL-DOWN-01`）。要全部删除（不可恢复）：

```bash
sudo bash uninstall.sh --purge
```

加 `-y` 不再询问确认。Docker 本身不会被卸载。

---

## 应用中心

应用中心是一份预先配好的应用目录。从目录装应用不用写 Dockerfile，也不用自己想密码：AcornFox 生成密码和密钥，下载镜像、启动、做健康检查和初始化，最后给出首次登录信息。应用用的都是上游原版镜像，AcornFox 不改它们的代码。

### 能装什么

| 应用 | 目录里的名字 | 是什么 | 内存 | 磁盘剩余空间 | 大约多久 |
| --- | --- | --- | --- | --- | --- |
| New API | `new-api` | 大模型接口网关：把多家大模型合成一个 OpenAI 兼容的接口，统一管理上游 Key、令牌和额度（上游项目 QuantumNous/new-api，AGPL-3.0） | 最少 256 MB，建议 512 MB | 至少 512 MB | 1 分钟左右 |
| Hermes 智能体 | `hermes-agent` | 常驻的 AI 智能体，可以接飞书、钉钉、企业微信等做机器人（上游项目 Nous Research 的 Hermes Agent，MIT） | 最少 1 GB，建议 2 GB | 至少 6 GB | 3～4 分钟 |

配方是一组一起装好的应用：

| 配方 | 装什么 |
| --- | --- |
| `new-api` | 只装 New API |
| `hermes-agent` | 只装 Hermes 智能体 |
| `hermes-with-new-api` | 两个一起装并连起来：Hermes 经本机 New API 用大模型，你只要在 New API 里加一次上游 Key |

- 两个应用都是单容器，装好后默认只有服务器本机能打开。
- Hermes 要求大模型的上下文不少于 64K，否则启动不了。
- 装之前会检查内存和磁盘剩余空间，不够时停下（`CAT-INST-05`、`CAT-INST-27`），不会装一半。
- 目录里的应用或配方要求比这台服务器更新的 AcornFox 时，列表和详情里标“要先升级到 x.y.z”并给出升级命令，安装按钮是灰的（v0.5.1 起；命令行 `--json` 里是 `needs_platform`）。先按上文“升级”升级服务器再装。
- 镜像包按服务器的 CPU 架构取（v0.5.1 起）：目录里没有这台机器架构的镜像包时，改从公共镜像源拉，进度里写明原因。

### 从页面装

1. 在你自己的电脑上运行 `acornfox open` 打开控制台，点左侧“应用中心”。
2. 点一个应用或配方，看详情：它是什么、装好后还要你做的几步、对应的命令。
3. 点“安装”（配方是“开始”）。Hermes 要选大模型来源（见下文“Hermes 的大模型从哪来”）。“更多设置”和“平台自动填的”一般不用改。
4. 弹窗里会显示每一步的进度，装好后显示首次登录信息和怎么打开。

### 从命令行装

在服务器上，或者在登记了服务器的电脑上运行：

```bash
acornfox catalog list                    # 目录里的应用和配方
acornfox catalog info hermes-agent       # 应用详情和安装时能填的参数
acornfox install new-api                 # 装 New API，等到装好或失败
acornfox install --recipe hermes-with-new-api   # 一起装 New API 和 Hermes 并连起来
```

- `install` 默认等到装好或失败才返回，期间每 10 秒打印一次进度；默认最多等 30 分钟（`--timeout` 可改），等不到也不会取消安装。
- 加 `--no-wait` 只提交，输出任务编号，之后用 `acornfox install status 任务编号` 查进度；加 `--wait` 会等到装好或失败再返回（默认最多等 90 秒，`--timeout 秒` 可以改，最多 600；没装完就再运行一次）。输出里写着装到了哪一步、每一步用了多久，比平常慢很多时会提示（编号 `CAT-INST-30`）。
- 应用名默认就是目录里的名字（`new-api`、`hermes-agent`），`--name` 可以换一个。
- 目录每天会自动同步一次；想马上拿到最新目录，运行 `acornfox catalog sync`。在线目录取不到时用程序自带的目录，照样能装。

### Hermes 的大模型从哪来

| 来源 | 写法 | 说明 |
| --- | --- | --- |
| 本机 New API | `--llm local` | 本机已经装了在运行的 New API 时，不写也默认用它。AcornFox 在 New API 里建一把令牌交给 Hermes |
| 自己的 Key | `--llm own --set llm.base_url=… --set llm.api_key=… --set llm.model=…` | 三项都要给，例如 `--set llm.base_url=https://api.example.com/v1` |
| 平台 Key | — | 这一版还不能用，选了会报 `CAT-INST-09` |

```bash
acornfox install hermes-agent --llm local
```

- 配方 `hermes-with-new-api` 固定让 Hermes 用本机 New API；要用自己的 Key，单独装 `hermes-agent --llm own`。
- 用本机 New API 时，Hermes 里配的模型名默认是 `deepseek-chat`。New API 渠道里的模型名不一样时，安装时加 `--set platform.llm.model.chat=模型名`（页面上在“平台自动填的”里改“默认对话模型”），也可以装好后在 Hermes 控制台里改。

### 安装进度和下载网关

每一步（检查、建应用、下载镜像、写配置、启动、健康检查、初始化）都有状态和说明，页面上实时显示，命令行用 `acornfox install status 任务编号` 查。

- Hermes 的镜像约 1.1 GB，只在 Docker Hub 上，国内直接拉很慢。AcornFox 先从下载网关 `dl.acornfox.com` 要一个临时下载地址来下载我们存好的镜像包，下载时显示已下多少、总共多少、速度和预计剩余时间，下完显示“正在导入镜像”。
- 下载中途断开、超时或地址过期时，从已下的位置接着下；没有进展时最多重试 5 次（总共最多试 30 次）才放弃。下完按目录里签名的校验值和大小核对，对不上就删掉、不导入。
- 下载网关没成功时（关掉了、连不上、次数用完等），改从公共镜像源拉，进度里写明网关为什么没成功。
- New API 的镜像约 78 MB，直接从公共镜像源拉（国内走镜像加速）。
- 下载网关会联系我们的服务器，具体发了什么、怎么关，见下文“隐私和联网说明”。

### 平台变量

时区、下载源、默认模型这类底层设置不写死在应用里，安装时由平台按这台机器填进去，叫“平台变量”。查看这台机器上的值：

```bash
acornfox catalog platform-vars
```

| 名字 | 是什么 | 能不能改 |
| --- | --- | --- |
| `tz` | 时区，取服务器本机时区，读不到时为 UTC | 能改 |
| `llm.model.chat` | 默认对话模型，用本机 New API 又没指定模型时用它 | 能改 |
| `mirror.pypi` | Python 包下载源：国内用国内镜像（在阿里云、腾讯云上用该厂商的公开镜像），海外用官方源 | 能改 |
| `mirror.npm` | Node 包下载源，规则同上 | 能改 |
| `region` | 网络区域（`cn` 或 `global`），按安装时的判断 | 不能改 |
| `app.url` | 这个应用的访问地址，按装好时的对外方式算 | 不能改 |
| `llm.base_url`、`llm.api_key` | 平台模型接口地址和平台 Key | 这一版没有值 |

能改的在安装时改，只对这次装的应用生效：

```bash
acornfox install new-api --set platform.tz=Asia/Tokyo
```

页面上在安装弹窗的“平台自动填的”里改。改法不对报 `CAT-INST-28`。

### 装好以后：首次登录

| 应用 | 账号 | 密码 |
| --- | --- | --- |
| New API | `admin` | 安装时自动生成 |
| Hermes 智能体 | `admin` | 安装时自动生成 |

- 密码默认遮成 `******`。页面上在“我的应用”里点开应用，“首次登录”里点显示；命令行用安装时的任务编号：`acornfox install status 任务编号 --show-secrets`。
- New API 第一次打开时谁先打开谁当管理员，所以 AcornFox 在装好后立刻替你完成了初始化，并关掉了自助注册（只有管理员账号能登录）。安装进度的“初始化”一步如果写着“没能关闭自助注册”，请登录 New API，在“系统设置”里关掉“允许新用户注册”。
- 装好后还要你做的几步会列在最后，例如 New API 要在“渠道”里新建一个渠道，填上游的 Key 和模型，点“测试”。在 New API 里加上游渠道会产生模型费用，由上游按你的用量收取。

### 查看一个应用是用什么设置装的

v0.5.1 起，从应用中心装的应用可以查当时用的设置：

```bash
acornfox apps vars new-api             # 每个变量的值、是谁给的、能不能改
acornfox apps vars hermes-agent --reveal   # 连密码和 Key 一起显示全
```

- 每个变量一行：现在的值；值从哪来（你填的、配方预填的、清单默认值、平台生成的、平台按这台机器算的、你用 `--set platform.名字=值` 改的、接上旧数据时恢复的）；清单写的能不能改。
- 密码和 Key 默认只显示后 4 位；`--reveal` 显示全，注意别贴到公开的地方。
- 只能看，还不能改。v0.5.0 装的应用那时不记“值从哪来”，照实写“装时的值”。

### 怎么打开只本机的应用

从应用中心装的应用默认只有服务器本机能打开，地址是 `http://127.0.0.1:端口`，在你自己电脑上点不开。**不用去安全组放行端口**：应用只听本机地址，放行了也打不开。有两种办法：

**办法一：服务器在你的局域网里（例如家里或公司的一台 Linux 机器）。** 页面上应用的“怎么打开”里有“让局域网里的电脑能打开”按钮，点了会先说明影响，确认后马上生效。之后用应用卡片上的“打开”按钮，或在浏览器里输入 `http://服务器的局域网地址:端口` 打开，用应用自己的账号密码登录。

- 这会在服务器的所有网卡上开放这个端口。服务器如果还有公网地址、防火墙或安全组又放行了这个端口，公网也能访问到。
- 命令行的等价写法：`acornfox app set --app new-api --expose lan`；改回只本机：`acornfox app set --app new-api --expose local`。
- 云服务器、或者认不出是局域网的服务器，页面上不显示这个按钮，请用办法二。

**办法二：公网服务器，或者不想开放端口。** 在你自己的电脑上运行：

```bash
acornfox open new-api
```

它像 `acornfox open` 打开控制台一样，开一条 SSH 隧道把应用转到你电脑的一个本地端口，再打开浏览器；看完按 Ctrl+C 关闭。前提是你电脑上装了 CLI，并用 `acornfox target add 名称 --ssh 用户@服务器地址` 登记了服务器。

- Windows 电脑、或者电脑上没有 CLI：在自己电脑的终端运行 `ssh -N -L 端口:127.0.0.1:端口 用户@服务器地址` 并保持不关，然后在浏览器打开 `http://127.0.0.1:端口`（端口是应用地址里的端口，`acornfox apps` 能看到）。
- `OPEN-APP-01`：应用还没有访问端口，用 `acornfox status --app 应用名` 看部署状态。
- `OPEN-APP-02`：这台服务器是用网址（局域网控制台或公网控制台）登记的，开不了隧道。用 `--ssh` 再登记一次，或者改成局域网。

要让公网直接访问（`--expose public` 或绑定域名）请慎重：New API 里存着你的上游 Key，Hermes 的控制台能执行命令。一定要用 HTTPS 和强密码。

### Hermes 接本机 New API 和令牌额度

用本机 New API 时（`--llm local` 或配方 `hermes-with-new-api`），AcornFox 在 New API 里建一把令牌交给 Hermes，令牌名是 `acornfox-应用名`（例如 `acornfox-hermes-agent`）。两个容器在同一个容器网络里直接相连，不经过公网。

- **这把令牌不限额、不过期。** 要控制花费，登录 New API，在“令牌”页给它设额度上限。
- New API 还没加上游渠道时，Hermes 调模型会返回 503“没有可用渠道”，这是正常的，加好渠道就行。
- 连数据一起删除 Hermes（`--volumes`）时会顺带吊销这把令牌。New API 当时没在运行、没吊销成的，删除命令会给出提示，请到 New API 的“令牌”页手动删掉。只删应用、保留数据时（v0.5.1 起），令牌跟着数据留下（Hermes 的配置文件里存着它），以后接上旧数据重装时接着用；重装时改用别的大模型，旧令牌在装好后吊销。那时 New API 停着、没吊销成的，安装进度和命令行结果里会写“没能吊销以前在 … 里给 … 的令牌”，并说出这把令牌的名字和结尾 4 位，请在 New API 运行时到“令牌”页删掉它（这次新发的那把别删）。
- Hermes 还连着 New API 时，不能删 New API（`app_in_use`），要先删 Hermes。
- Hermes 上游的程序启动时会访问 `nousresearch.github.io`（取它的能力目录），国内网络打不开，不影响对话。

### 删除和接上旧数据重装

删除应用默认保留数据卷：

- 页面：“我的应用”里点开应用 →“高级”→“删除”。默认勾着“保留数据卷”；去掉勾选要输入应用名确认，连数据一起删。
- 命令行：一定写上应用名。

```bash
acornfox delete hermes-agent            # 删除应用，保留数据
acornfox delete hermes-agent --volumes  # 连数据一起删，不能恢复
```

保留了数据的应用，以后用同一个名字再装时会停下（`CAT-INST-24`），因为默认不让新装的应用悄悄接上旧数据。这时二选一：

- 要接着用旧数据：页面上点“接上旧数据重装”并确认；命令行在原来的安装命令后面加 `--reuse-data`，例如 `acornfox install new-api --reuse-data`（装的时候用了 `--name` 的，要用同一个名字）。接上的旧数据即使这次安装失败也不会被删。旧数据是别的应用留下的，会被拒绝。

接上旧数据时，这次没写的设置都按以前的来（v0.5.1 起）：

| 设置 | 怎么处理 |
| --- | --- |
| 账号密码、密钥 | 和以前一样 |
| 以前用 `--set platform.名字=值` 改过的平台变量（例如时区） | 恢复成以前改的值；这次又写了 `--set` 的，以这次为准。v0.5.0 装的应用分不出哪些是你改过的，要这次再写一遍 |
| 大模型来源 | 沿用原来的：自己的 Key 照旧；本机 New API 还在运行就接着用，连原来那把令牌也接着用；那个 New API 停着，会停下让你先启动它（`CAT-INST-10`，`acornfox start new-api` 后再装）；本机没有它了，会停下让你重新选（`CAT-INST-02`）。这次写了 `--llm` 的以这次为准（还是选原来那个本机 New API 时，原来那把令牌照样接着用） |
| 数据里已有的配置文件（例如 Hermes 的 `config.yaml`） | 不重写，你和应用自己改过的内容都留着。只有平台要往里填的内容变了（例如这次换了大模型，或者原来那把令牌已经没有了）才按这次的设置重写，重写前把旧文件另存为 `config.yaml.bak-时间`，要的内容可以从里面抄回来。上一次接旧数据没装好、配置文件已经被那次改写过的，这次也按这次的设置重写（旧文件同样另存） |
| 对外方式 | 回到默认的只本机，以前开放过局域网的要再开一次 |

沿用了什么，安装进度的“建应用”“写配置”两步里写着，命令行装好后也会列出来（`--json` 时在任务的 `apps[].carried`、`kept_files`、`file_backups` 里）。
- 不要旧数据：用 `--name` 换个名字装；或者确定不要了，按错误信息里的卷名用 `docker volume rm` 删掉旧卷后再装。

### 要升级应用怎么办

这一版没有“从目录升级到新版本”的功能。目录里有了新版本，想换上去：

1. `acornfox catalog sync` 拿到最新目录，`acornfox catalog info 应用` 看版本。
2. `acornfox delete 应用名` 删除应用，默认保留数据。用本机 New API 的 Hermes，要换 New API 时先删 Hermes。
3. 用原来的安装命令加 `--reuse-data` 重装，接上旧数据。设置按上一节的表沿用。

新版本能不能读旧数据由应用自己决定，重要的数据请先自己备份。新版本的清单如果改了配置文件的模板，配置文件会按新模板重写，旧文件另存为 `文件名.bak-时间`。

### 常见故障编号

安装失败时，这次新建的应用和数据卷会自动清理（以前留下的数据不会动），处理好原因后直接重装即可。服务器返回的错误里带编号、原因和建议。

| 编号 | 意思 | 怎么办 |
| --- | --- | --- |
| `CAT-INST-03` | 已经有同名应用 | `--name` 换个名字；要重装先确认后删掉旧的 |
| `CAT-INST-04` | 同一个应用正在装 | 不要重复提交，用错误里的任务编号查进度 |
| `CAT-INST-05` | 整机分配给应用的内存不够 | 调小别的应用（`acornfox app set --memory`）或调大整机上限（`acornfox limits --memory`） |
| `CAT-INST-08` | 这个应用要更新版本的 AcornFox | 错误里的 `needs_platform` 是要升到的版本；先升级服务器（见上文“升级”），再装 |
| `CAT-INST-09` | 平台 Key 这一版不能用 | 改用 `--llm local` 或 `--llm own` |
| `CAT-INST-10` | 要用本机 New API，但它没装、停着或没在运行 | 错误里说“装着但停着”的，`acornfox start new-api`（名字按错误里的）后再装，或者改用 `--llm own`；本机还没有 New API 才 `acornfox install new-api`，或装配方 `hermes-with-new-api` |
| `CAT-INST-11` | 本机 New API 发不出令牌 | `acornfox status --app new-api` 看它在不在运行；管理员密码改过的话改用 `--llm own`；错误里有 HTTP 429 是 New API 限制了登录次数（同一来源 20 分钟 20 次），等 20 分钟再装，不要反复重试 |
| `CAT-INST-12` | 镜像从所有来源都没拿到 | 稍后重试；`sudo acornfox doctor` 看网络和镜像加速 |
| `CAT-INST-15` | 容器没能启动或上线 | 看错误里的部署诊断；`acornfox logs 应用名` 看日志 |
| `CAT-INST-16` | 健康检查在时限内没通过 | `acornfox logs 应用名` 看日志；Hermes 还要确认模型上下文不少于 64K、内存够用 |
| `CAT-INST-17` | 装好后的初始化调用失败 | 看错误信息；这次的应用已清理，直接重装 |
| `CAT-INST-18` | 安装途中 AcornFox 服务重启了 | 已清理，重新安装 |
| `CAT-INST-24` | 以前同名应用的数据卷还在 | 见上文“删除和接上旧数据重装” |
| `CAT-INST-27` | 磁盘剩余空间不够 | 错误信息里写了要多少、剩多少；删掉不用的应用或镜像，或者扩容磁盘；`sudo acornfox doctor` 看各分区 |
| `CAT-INST-28` | 改平台变量不对 | `acornfox catalog platform-vars` 看哪些能改 |
| `CAT-INST-29` | 应用包里有这个版本不认识的写法（v0.5.1 起），什么都没建 | 错误里有 `needs_platform` 的，先升级 AcornFox；没有的是目录本身有问题，升级也没用，过一会儿 `acornfox catalog sync` 后再试 |
| `CAT-INST-30` | 某一步比平常慢很多，还没失败（v0.5.1 起，是提醒不是错误） | 照输出里的 `next_command` 看原因；接着用 `acornfox install status 任务编号 --wait` 等 |

完整列表见 [能力清单](capabilities.md#应用中心安装故障编号)。v0.5.1 起，任何故障编号（安装脚本、`acornfox doctor`、应用中心、部署诊断里的）都可以用 `acornfox faults 编号` 看完整说明：可能的原因和怎么判断、按什么顺序查、哪些可以直接做、哪些要你先同意；`acornfox faults` 列出全部编号。这个命令只读，不连服务器，见上文“自检和故障编号”。

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
3. 之后 `acornfox open`、`acornfox deploy`、`acornfox add postgres` 等命令都在 Mac 上运行，作用在服务器上。部署时把当前目录的项目打包传到服务器构建。`acornfox faults 编号` 查故障编号的说明，不连服务器，在 Mac 上也能用。

升级服务器后，在 Mac 上重新运行一次安装 CLI 的命令，让两边同版本。

**这一版不提供在 Mac 本机运行 AcornFox（应用跑在这台 Mac 上）。在 Mac 上运行不带应用名的 `acornfox install`、或 `acornfox doctor`，只会提示还不能用（故障编号 `SETUP-MAC-01`）。**

---

## Windows 电脑：经 SSH 在服务器上操作

本版本没有 Windows 版程序（在 Git Bash 里运行 `install-cli.sh` 只会说明这一点并退出）。按上面“Linux 服务器”装好 AcornFox 后，服务器上已经有 `acornfox` 命令，用 SSH 登录服务器运行它就行。Windows 10、11 自带 `ssh` 和 `scp`，在 PowerShell 里就能用；让 AI 工作台按 Skill 代你操作时，请装 Git for Windows，工作台在 Git Bash 里执行。

这一版不提供把 AcornFox 服务装在 Windows 电脑本机运行。

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

## 隐私和联网说明

AcornFox 没有使用统计，也没有崩溃上报。下面写清楚它什么时候会联系 AcornFox 的服务器、发了什么、怎么关，以及安装和运行时还会访问哪些地址。

### 什么时候会联系 AcornFox 的服务器

一共三种情况，都发生在服务器上的 AcornFox 服务里（你电脑上的命令行只经 SSH 连你登记的服务器）：

**1. 下载网关 `dl.acornfox.com`：从应用中心装我们存有镜像包的应用时（这一版是 Hermes 智能体）。**

- 第一次要用时，AcornFox 在本机随机生成一把钥匙（ECDSA P-256），存在 `/var/lib/acornfox/identity/`；由公钥算出一个安装编号（形如 `afx-` 加 20 个字母数字）。钥匙是随机的，不读任何硬件信息。
- 第一次向网关登记时发送：公钥、AcornFox 程序版本、系统（`linux`）、CPU 架构（`amd64` 或 `arm64`），以及“由 acornfox 程序发出”的标记。
- 之后每次要下载地址时发送：安装编号、时间戳、一个随机数、用这把钥匙算的签名，以及要下载哪个文件（文件编号）和用途。
- 网关只返回一个临时下载地址，文件本身从这个地址下载（文件放在我们租用的存储服务上）。网关会记下这台机器在什么时候要了哪个文件，用来限制每台机器每天的次数。
- 和任何网络请求一样，网关能看到这台服务器的公网 IP。

**2. 应用目录：每天同步一次。**

- 服务启动时同步一次，之后每天在一个随机时刻同步一次；运行 `acornfox catalog sync` 时也会同步。看应用详情或安装应用时，本机还没有这个应用的应用包（几 KB 的清单和图标）就下载一份。
- 地址先是 `https://acornfox.com/catalog`，打不开时改读 Gitee 上的备用副本（`https://gitee.com/VIP13390/acornfox-catalog/raw/published`）。两处是同一份带签名的文件，签名对不上的不会用。
- 这些是普通的下载请求，不带安装编号、不带签名。对方能看到这台服务器的公网 IP。

**3. 时钟和下载网关检查：用 sudo（或 root）运行 `acornfox doctor` 时（普通用户运行时读不到网关设置，不发，v0.5.1 起；自己在终端里设了 `ACORNFOX_GATEWAY` 的，按它写的地址发，设为空值的不发）；控制台的首页和“运行环境”页开着时，约每分钟一次；打开“设置”页时一次。**

- 下载网关要求请求的时间和它相差不超过 300 秒，自检还要看网关能不能连上（故障编号 `SETUP-GW-01`）。所以每次自检向网关地址发一个不带内容的 HEAD 请求，只读应答的状态码和 `Date`（服务器时间），最多等 5 秒。这个请求不带安装编号、不带签名。
- 时钟不准（故障编号 `SETUP-CLOCK-01`）时，按提示运行 `sudo timedatectl set-ntp true` 打开网络对时。AcornFox 不会改系统时间。

### 不收集什么

- 不读、不发硬件信息（网卡地址、序列号、主机名等）。
- 不发你的应用列表、应用名、项目代码、环境变量、日志、密码和域名。
- 下载网关只知道这台机器在什么时候下载了我们存放的哪些文件。

查看这台机器的安装编号和公钥：`sudo acornfox identity show`。想让网关把它当成一台新机器，运行重置再重启服务（服务只在启动时读钥匙）：

```bash
sudo acornfox identity reset --yes   # 不加 --yes 只说明会做什么
sudo systemctl restart acornfox-server
```

用 sudo 运行时，新钥匙文件的属主会改回数据目录的属主（后台服务的账号），服务照样读得到。旧钥匙留在同目录的 `machine-key.pem.prev`。

### 怎么关

**关掉下载网关。** 在服务器上写一个 `/var/lib/acornfox/network.json`（这个文件原来没有；已经有的话在里面加 `"gateway": "off"` 这一项），把它交给后台服务的账号，然后重启服务：

```bash
echo '{"gateway": "off"}' | sudo tee /var/lib/acornfox/network.json
sudo chown acornfox /var/lib/acornfox/network.json
sudo systemctl restart acornfox-server
```

第二行不能省：root 的 umask 是 077 之类时，`tee` 建出来的文件只有 root 能读，后台服务读不到它，就会照旧用网关，也不报错。

后台服务（包括控制台各页的自检）和 `sudo acornfox doctor` 都认这个文件。不加 sudo 运行 `acornfox doctor` 时，普通用户读不到数据目录里的这个文件，看不出网关关没关，这一项就不查、也不联系网关（v0.5.1 起；终端里设了 `ACORNFOX_GATEWAY` 的按它），要查用 `sudo acornfox doctor`。要恢复，删掉这个文件（或这一项）再重启服务。也可以给后台服务设环境变量 `ACORNFOX_GATEWAY` 为空值（`sudo systemctl edit acornfox-server`，写入 `[Service]` 和 `Environment=ACORNFOX_GATEWAY=` 两行，再重启服务）；它优先于 `network.json`。但用 `systemctl edit` 设置的环境变量只在后台服务里有，你在终端里运行 `sudo acornfox doctor` 时没有，doctor 仍会向网关发一次 HEAD，所以推荐用 `network.json`。

关掉之后：

- 不再联系下载网关，也不会生成机器钥匙（以前生成的还留在 `/var/lib/acornfox/identity/`）。
- Hermes 的镜像改从公共镜像源拉（Docker Hub，国内走镜像加速），会慢很多：国内网络可能要十几分钟，有时拉不下来（`CAT-INST-12`）。New API 本来就从公共镜像源拉，不受影响。
- 自检不再向网关发请求：时钟检查只看系统有没有开网络对时、是否已同步，不再和网关比对；“下载网关”一项显示已关掉。

**应用目录的来源。** 这一版没有关掉目录同步的开关。可以用环境变量 `ACORNFOX_CATALOG_SOURCES` 换成你自己放的一份副本（多个地址用逗号分隔，按顺序试；`sudo systemctl edit acornfox-server` 写入 `Environment=ACORNFOX_CATALOG_SOURCES=https://…`）。副本里的文件必须是原样复制的，签名对不上的不会用。所有来源都取不到时，用程序自带的目录，应用中心照样能用。

### 安装和运行时还会访问哪些地址

**安装、升级脚本（在服务器上）**

| 做什么 | 访问哪里 | 什么时候 |
| --- | --- | --- |
| 判断在哪家云、哪个地域，取公网 IP | 阿里云、腾讯云、火山引擎、华为云的实例元数据接口 | 每次安装、升级；只在云主机内部能访问，不在云上时很快超时跳过 |
| 判断是否在中国大陆 | `ip-api.com` | 认不出是哪家云时 |
| 查最新版本号、下载程序和脚本 | 中国大陆：先 Gitee 发行版，失败再 GitHub；其他地区：GitHub | 安装、升级 |
| GitHub 下载加速 | `ghfast.top`、`gh-proxy.com`，或你用 `ACORNFOX_GITHUB_PROXY` 指定的地址 | 只在中国大陆、直连 GitHub 失败或太慢时 |
| 系统软件源（APT） | 云厂商内网源；中国大陆还有 `mirrors.huaweicloud.com` | 系统原来的源指向 Ubuntu、Debian 官方地址时才换；其他源原样保留 |
| Docker 安装包 | 云厂商内网源；中国大陆依次是华为、腾讯、阿里、南京大学、清华镜像站；其他地区 `download.docker.com` | 安装 Docker 时（`--skip-docker` 跳过） |
| Caddy 安装包 | 中国大陆先从 Gitee 发行版取，再 GitHub 上的 Caddy 官方发行版；校验值写在脚本里 | 安装时，本机还没有 Caddy 镜像的话 |

**运行时（服务器上的 AcornFox 服务）**

| 做什么 | 访问哪里 | 什么时候 |
| --- | --- | --- |
| 拉取应用和数据库镜像 | Docker Hub；镜像加速 `docker.m.daocloud.io`、`docker.1panel.live`，腾讯云服务器还有腾讯云内网加速；ghcr.io 的镜像在中国大陆走 `ghcr.nju.edu.cn` | 部署、加数据库、从应用中心装应用时；后台服务每次启动自检时探测一次（`GET /v2/`，前面的镜像加速都不通时才探测 Docker Hub） |
| 下载应用中心的镜像包 | 下载网关给的临时地址 | 见上文 |
| 同步应用目录 | `acornfox.com`、`gitee.com` | 见上文 |
| 拉取代码 | 你在 `deploy --git` 里给的仓库地址 | 用 Git 仓库部署时 |
| 构建 | 你的 Dockerfile 里写的软件包源（npm、pip 等） | 构建镜像时 |
| 申请 HTTPS 证书 | Let's Encrypt | 绑定域名、开公网控制台时 |
| 取公网 IP | 云厂商元数据接口；取不到再问 `4.ipw.cn`、`ipv4.icanhazip.com`（IPv6 是 `6.ipw.cn`、`ipv6.icanhazip.com`） | 只在开公网控制台时 |

**应用自己**

应用装好后自己访问哪些地址由应用决定。已知的有：

- New API 访问你在渠道里填的上游模型接口。
- Hermes 访问你配的模型接口；平台把 `mirror.pypi`、`mirror.npm` 作为下载源环境变量交给它，它装插件、技能时实际访问哪里由 Hermes 自己决定；启动时访问 `nousresearch.github.io`（国内打不开，不影响对话）。

**命令行（在你自己的电脑上）**

- 安装命令行：中国大陆先 Gitee 发行版，再 GitHub 和上面的下载加速；判断是否在中国大陆时会访问云厂商元数据接口和 `ip-api.com`。
- 管理服务器：只通过 SSH 连你登记的服务器。

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

