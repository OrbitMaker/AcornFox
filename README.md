[English](README.en.md)

<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://acornfox.com/assets/brand/lockup-horizontal-dark.svg">
    <img src="https://acornfox.com/assets/brand/lockup-horizontal.svg" alt="AcornFox" height="48">
  </picture>
</h1>

AcornFox 是一个开源的单机应用部署工具：在你自己的 Linux 服务器上部署和管理应用，可以用命令行、网页控制台，也可以交给 AI 工作台操作。

## 支持的平台

| 平台 | 这一版提供 | 说明 |
| --- | --- | --- |
| Linux 服务器 x86_64 | 服务端 + 命令行 | 正式支持。Ubuntu 22.04、24.04，Debian 12、13 |
| Linux 服务器 arm64 | 服务端 + 命令行 | 安装脚本允许安装，还没有实测 |
| Mac（Apple 芯片，macOS 26 起） | 装在这台 Mac 的虚拟机里 + `acornfox` 命令 | 同一个 Linux 程序装进苹果 `container` 开的一台 Debian 虚拟机，Mac 上只有苹果的 container、一个常驻小程序和 `acornfox` 命令，见下面“Mac 电脑本机安装”。需要管理员密码的只有装苹果 container 那一步，由你自己输 |
| Mac（Intel，或系统低于 macOS 26） | 只有命令行 | 当客户端，经 SSH 管理 Linux 服务器 |
| Windows 10 22H2 / Windows 11（64 位 Intel、AMD） | 装在这台电脑的 WSL 里 + `acornfox` 命令 | 同一个 Linux 程序装进 WSL 2 里一个叫 AcornFox 的 Debian，Windows 上不放我们的 exe，见下面“Windows 电脑本机安装”。ARM 版 Windows 暂时不支持 |

详见 [使用手册](docs/user-guide.md)。

## 能做什么

- **部署**：本地目录（带 Dockerfile）、公开 Git 仓库或现成的 Docker 镜像
- **应用中心**：从预先配好的目录装应用；应用要用的东西（大模型接口、邮箱等）登记成“服务”，装的时候自动接上，见下面“应用中心”
- **AI 工作台**：Claude Code、Codex 等照着 AcornFox Skill 完成安装、部署和排错
- **诊断**：部署失败时返回带编号的诊断和日志片段；任何故障编号都能用 `acornfox faults 编号` 查原因和处理办法
- **数据库**：一条命令加 PostgreSQL、MySQL 或 Redis
- **版本**：每次部署都有记录，可以回退应用到之前的版本
- **HTTPS**：绑定域名后自动申请证书（国内服务器的域名要已备案）
- **国内网络**：自动选用国内软件源和镜像加速

## 快速开始

### 推荐：把一句话发给 AI 工作台

准备一台能 SSH 登录的 Linux 服务器（系统要求见下面的「服务器要求」），在 Claude Code、Codex 等 AI 工作台里发送：

```text
请安装 AcornFox Skill：下载 https://acornfox.com/skill/SKILL.md ，保存为你 skills 目录下的 acornfox/SKILL.md（Claude Code 是 ~/.claude/skills，Codex 是 ~/.codex/skills），然后按这个 Skill 把当前项目部署到我的服务器。
```

AI 会先安装 Skill，再由 Skill 分别判断你的电脑和服务器在国内还是海外，选择 Gitee 或 GitHub 下载源，装好 CLI 与服务端，然后部署。

> **云服务器安全组**：应用通过 `http://服务器IP:端口` 访问，端口在 18810-18899 之间分配。请在云厂商控制台的安全组中放行入方向 TCP 18810-18899；绑定域名启用 HTTPS 还需放行 80 和 443。

### 服务器要求

| 项目 | 要求 |
| --- | --- |
| 系统 | Ubuntu 22.04、24.04，Debian 12、13（x86_64），推荐 Ubuntu 24.04 或 Debian 13。这四个版本都实测过全新安装、自检和部署。arm64 还没测过 |
| 配置 | 建议 2 核 2 GB 以上，需要 root 或 sudo |
| 端口 | 18800、18801、18807（AcornFox 自己用，只听本机）和 18810-18899（应用端口）必须空着，被占时安装脚本会停下，不能跳过。80、443 最好空着；已被宝塔、1Panel、Nginx 占用时可以共存，见下面的说明。18802 是局域网控制台，被占时只是不开 |

> **买国内云服务器时，系统请选 Ubuntu 24.04（或 Debian 13）。** 阿里云默认的 Alibaba Cloud Linux、腾讯云默认的 OpenCloudOS / TencentOS、火山引擎默认的 veLinux，以及 CentOS、Rocky 等系统装不上 AcornFox，安装脚本会停下并提示“……不能安装 AcornFox”（故障编号 `INSTALL-OS-01`）。Ubuntu、Debian 的版本不在上面列表里（例如 Ubuntu 20.04、Debian 11）时提示“……不在支持列表”（故障编号 `INSTALL-OS-02`），请换成支持的版本。

> **80、443 已被占用（宝塔、1Panel、自己装的 Nginx）**：安装脚本会停下（故障编号 `INSTALL-PORT-02`）。不想停掉原来的网站，就加 `--allow-busy-80-443` 和它们共存：`curl … | sudo bash -s -- --allow-busy-80-443`。共存时应用照常通过 `http://服务器IP:端口` 访问，但不能在 AcornFox 里绑定域名、开 HTTPS 或开公网控制台；要用域名，就在原来的面板里把域名反向代理到 `127.0.0.1:应用端口`。

### 手动安装

#### 1. 安装服务器端

```bash
# 中国大陆服务器（Gitee 镜像）
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash
# 其他地区
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash
```

安装完成后会显示管理员令牌（也保存在 `/root/.acornfox-token`），请妥善保存。

服务器在家里或办公室的局域网里（私网地址、不是云主机）时，第一次安装会默认打开局域网控制台（`http://服务器局域网地址:18802`），第一次打开时在浏览器里自己设密码；不想打开就加 `--no-lan-console`：`curl … | sudo bash -s -- --no-lan-console`。云服务器不会打开。

- 服务器在阿里云、腾讯云、火山引擎上时（不论哪个地域），安装脚本先用这家云的内网软件源（系统软件源和 Docker 安装包），不走公网、不耗流量，连不上再换下一个；在华为云和其他国内服务器上用华为等国内公共镜像站。
- 装完后按脚本最后的提示，在云厂商控制台的安全组放行入方向 TCP 18810-18899（应用端口）；要绑域名再放行 80 和 443。

#### 1b. 在这台 Windows 电脑上安装（Windows 10 22H2、Windows 11）

Windows 上只用微软自带的 `wsl.exe`、PowerShell、计划任务和 `netsh`：AcornFox 装在 WSL 2 里一个叫 AcornFox 的 Debian 13（和 Linux 服务器上同一个程序），Windows 上多一个转发用的小命令 `acornfox`。在**普通** PowerShell 里（不要“以管理员身份运行”）运行下面一整行：

```powershell
# 中国大陆（Gitee）
powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol=3072; & ([scriptblock]::Create([Text.Encoding]::UTF8.GetString((New-Object Net.WebClient).DownloadData('https://gitee.com/VIP13390/AcornFox/raw/main/get.ps1')).TrimStart([char]0xFEFF)))"
# 其他地区（GitHub）
powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol=3072; & ([scriptblock]::Create([Text.Encoding]::UTF8.GetString((New-Object Net.WebClient).DownloadData('https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.ps1')).TrimStart([char]0xFEFF)))"
```

- 要先看会做什么：在最后一个 `)` 后面加 ` -Plan`（只检查和说明，什么都不改）。其他参数同理：`-LocalOnly`（应用只在这台电脑上能打开）、`-Image ubuntu`（用 Ubuntu 24.04 代替 Debian）、`-Version v0.7.0`、`-NoGateway`。
- 需要：64 位 Intel/AMD、CPU 开了虚拟化、系统盘剩 10 GB 以上、能上网。检查不过时脚本在下载之前就停下，打印故障编号（`WIN-OS-01`、`WIN-VIRT-01`、`WIN-POLICY-01`、`WIN-DISK-01`……，`acornfox faults WIN-…` 查处理办法）。
- 过程中会弹**一次管理员确认**（窗口写 Windows PowerShell、发布者 Microsoft Windows，选“是”）；电脑上还没有 WSL 时要**重启一次电脑**，重启后登录会自动弹出窗口接着装。电脑上已有 WSL 2 就不用重启。
- 装完自动打开控制台（`http://localhost:18802/`，第一次打开设密码）。在新开的 PowerShell 里运行 `acornfox apps`、`acornfox deploy`、`acornfox install 应用名` 等，和 Linux 服务器上一样；`acornfox skill install` 把 Skill 装进你的 Claude Code、Codex；`acornfox windows status` 看 Windows 这一侧每一项。
- 局域网里的手机、电脑也能打开你的应用（`http://这台电脑的局域网地址:端口`）：脚本登记一个开机自启的小任务，把 WSL 里“局域网能打开”的端口转到 Windows 上，并加防火墙规则（只在专用网络放行）；只想本机用就加 `-LocalOnly`，以后用 `acornfox windows lan-on` / `lan-off` 切换。
- 开机自启：登记计划任务，开机和登录时把 WSL 叫起来；`%UserProfile%\.wslconfig` 里加两行让 WSL 一直开着。
- 升级：同一条命令末尾加 ` -Action upgrade`。卸载：`acornfox windows uninstall`（保留数据）；`acornfox windows uninstall --purge --yes` 连发行版和数据一起删，不可恢复。WSL 功能本身不卸载。
- 手机、别的电脑要连这台 Windows 的话，Windows 的网络类型要是“专用网络”（家里、办公室），“公用网络”防火墙会挡（`WIN-LAN-03`）。

#### 1c. 在这台 Mac 上安装（苹果芯片、macOS 26 起）

AcornFox 装在苹果 `container`（苹果签名）开的一台 Debian 13 虚拟机里，和 Linux 服务器上是同一个程序、同一个 `install.sh`；Mac 上只多三样东西：苹果的 container、一个常驻小程序（登录后自动运行）、`acornfox` 命令。在“终端”里用普通用户（**不要加 sudo**）运行：

```bash
# 中国大陆（Gitee）
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash
# 其他地区（GitHub）
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash
```

- 要先看会做什么：`… | bash -s -- install --plan`（只检查和说明，什么都不下载、不改）；`--json` 让标准输出只有最后一行 JSON。其他参数：`--idle 60`（闲置多少分钟停虚拟机）、`--memory 8`（虚拟机内存 GB）、`--no-lan`、`--no-open`、`--china`/`--global`、`--version vX.Y.Z`。
- 需要：苹果芯片、macOS 26 或更新、磁盘剩 12 GB 以上、能上网。检查不过时在下载之前就停下，打印故障编号（`MAC-OS-01`、`MAC-ARCH-01`、`MAC-DISK-01`，`acornfox faults MAC-…` 查办法）。
- **管理员密码**：只有这台 Mac 上还没有苹果的 container 时要输一次（装苹果签名的安装包）。输入时屏幕不显示字符，这是正常的。AI 工作台不会代你输；它没有终端时，程序打印出那一行 `sudo installer …` 让你自己在终端里运行，装好再重新运行安装命令（退出码 5，`MAC-ADMIN-01`）。
- 下载：苹果的 container 安装包约 170 MB、没有默认内核时再加约 676 MB（先走 AcornFox 下载网关，分段多路、能续传、核对 SHA256，不通再用官方地址），Debian 镜像约 100 MB，虚拟机里装 Docker 等再几百 MB。
- 装完自动打开控制台（`http://127.0.0.1:18802/`，第一次打开设密码）。`acornfox apps`、`acornfox deploy`、`acornfox install 应用名` 等和 Linux 服务器上一样（登记的服务器叫 `local`）；`acornfox skill install` 把 Skill 装进你的 Claude Code、Codex。
- **内存**：虚拟机的内存上限默认 6 GB（Mac 内存 16 GB 以上）或 4 GB；它用过多少 Mac 上就占多少，只有停掉才还给 macOS。所以常驻小程序在**闲置 30 分钟没人访问**时停虚拟机，有人访问（浏览器、命令行、应用端口）再自动叫起，第一次要等 2～3 秒。虚拟机停着时里面的应用也停着；要一直开着：`acornfox mac set --idle off`。现在就想还内存：`acornfox mac stop`。
- 局域网里的手机、电脑也能打开你的应用和控制台（`http://这台 Mac 的局域网地址:端口`；设成“只本机”的应用除外）：常驻小程序把虚拟机的端口转到 Mac 自己的地址上；不想要加 `--no-lan`，以后 `acornfox mac set --lan off`。Mac 的局域网地址变了：`acornfox mac set --public-host auto`。
- 开机自启：常驻小程序是 launchd 的用户级启动项（`~/Library/LaunchAgents/com.acornfox.mac.plist`），登录后自动运行；虚拟机本身不随开机起，有人访问才起。
- 看状态、检查：`acornfox mac status`、`acornfox mac doctor`（先查 Mac 一侧，再查虚拟机里；每项带故障编号）。升级：同一条命令后面接 `bash -s -- upgrade`。卸载：`acornfox mac uninstall`（保留虚拟机和里面的数据，重装接得上）；`acornfox mac uninstall --purge --yes` 连数据一起删，不可恢复。**苹果的 container 本身不卸**（你可能还在用它）。
- 限制：虚拟机是 arm64，只有 x86 镜像的应用跑不了；虚拟机不挂 Mac 的家目录（读不到 Mac 上的文件）；macOS 的“本地网络”隐私限制不让我们自己的常驻程序直接连虚拟机，所以它借苹果自带的 `nc` 转发（对你没有影响，不用点任何允许）。

#### 2. 安装客户端 CLI（Linux、Mac 电脑）

```bash
# 中国大陆
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli
# 其他地区
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli
```

Mac 电脑想装在本机：用上面的 1c（命令行就是 `acornfox mac install` 装好的那个，不用再装 CLI）；只想管理一台 Linux 服务器时才需要这一步。Windows 电脑：装在本机用上面的 1b（命令行在 WSL 里，Windows 上的 `acornfox` 命令自动转过去）；只想管理一台 Linux 服务器时，第 2～4 步改为用 SSH 在服务器上运行（服务器上已经装好了 `acornfox`），见 [使用手册](docs/user-guide.md#windows-电脑经-ssh-在服务器上操作)。

#### 3. 配置服务器连接

```bash
acornfox target add my-server --ssh user@your-server-ip
```

#### 4. 部署你的第一个应用

```bash
cd your-project
acornfox deploy
```

部署后查询状态：

```bash
acornfox status
```

当状态变为 `live` 时，访问返回的 URL 即可。

## 应用中心

应用中心是一份预先配好的应用目录。装的时候不用写 Dockerfile，也不用自己想密码：AcornFox 生成密码，下载镜像、启动、检查、初始化，装好后给出首次登录信息。

目录里有两种东西：**应用**，一个一个单独装；**配方**（套装），几个应用一起装好并接上。目录里有什么以控制台“应用中心”和 `acornfox catalog list` 为准。

**v0.6.0 起只装新格式的应用清单。** 应用目录已按新格式发布，联网的服务器升级后会自动取到（想马上看到就运行 `acornfox catalog sync`）。以前按旧格式上架的应用，要等按新格式重新上架后才能再装；已经装好的应用不受影响，照常运行。程序自带的目录里没有应用：一次都没同步到新目录的服务器（例如连不上外网），装应用时会说“应用中心现在还没有可以安装的应用，要等应用按新格式重新上架”，能联网后运行 `acornfox catalog sync`（或者等自动同步，一般几个小时内）就好。

**服务**（v0.6.0 起）：有的应用要用到别的东西，例如要调大模型的应用需要一个“大模型接口”，要发通知的应用需要一个“邮箱（发信）”。应用写明了它需要哪些服务、装好后能提供哪些服务；这台机器上装好的应用提供的服务会自动登记，你自己已有的（例如自己的邮箱、别家的大模型 Key）用 `acornfox services add` 登记。装应用时按服务类型自动接上；必须的服务没有时停下，告诉你缺什么、怎么登记。AcornFox 不会替你装别的应用来提供服务，几个应用一起装靠配方。

两种装法：

- **页面**：`acornfox open` 打开控制台，左侧“应用中心”，选应用点“安装”，弹窗里能看到每一步的进度和装好后的登录信息。
- **命令行**：

  ```bash
  acornfox catalog list                  # 看目录
  acornfox install 应用 --plan           # 先看计划：需要的服务接哪条、还缺什么（只读）
  acornfox install 应用                  # 装一个应用
  acornfox install --recipe 配方         # 装一个配方
  acornfox services list                 # 这台机器上的服务、谁在用
  ```

**谁能打开。** 服务器在你的局域网里并且是第一次安装时，从应用中心装的应用默认同一局域网里的电脑、手机就能打开，安装结果里给出局域网地址（安装时加 `--default-exposure local` 改成只本机）。云服务器上默认只本机能打开：应用只听服务器本机地址（`http://127.0.0.1:端口`），不用去安全组放行端口。从自己电脑打开只本机的应用：

- 公网服务器：在你自己的电脑上运行 `acornfox open 应用名`，它像打开控制台一样开一条 SSH 隧道，看完按 Ctrl+C 关闭。
- 服务器在你的局域网里：页面上点“让局域网里的电脑能打开”，确认后用局域网地址打开（会在服务器所有网卡上开放这个端口）。

**以前装的应用。** v0.5.x 时从应用中心装的应用用的是旧格式的清单，照常运行、能启停和删除，但不接服务（`acornfox status 应用名` 和控制台应用详情里有一句说明）；要用新功能就等它按新格式重新上架后，删掉（保留数据）再从目录装回来。

**要升级应用怎么办。** 这一版没有应用的原地升级。目录里有新版本时，先 `acornfox delete 应用名`（默认保留数据），再用原来的安装命令加 `--reuse-data` 重装，接上旧数据（不记得它是目录里哪个应用时，`acornfox install --name 应用名 --reuse-data` 也行）。账号密码、以前改过的设置、接的服务和数据里的配置文件都照原来的接上，不用再把以前的参数写一遍；`acornfox apps vars 应用名` 可以看一个应用是用什么设置装的。

详细说明（服务怎么登记、配方、平台变量、故障编号）见 [使用手册的“应用中心”](docs/user-guide.md#应用中心)。

## AI 工作台

Skill 是使用 AcornFox 的入口：首次使用时由它安装 CLI 与服务端，之后负责部署、读诊断并修复、管理数据库和生命周期。

### 安装 Skill

- **还没有 CLI**：把「快速开始」里那句话发给 AI 工作台，AI 会从 `https://acornfox.com/skill/SKILL.md` 下载 Skill。
- **已有 CLI**：`acornfox skill install` 安装与 CLI 同版本的 Skill。

| 工作台 | 方式 |
| --- | --- |
| Claude Code | 自动安装到 `~/.claude/skills/acornfox/` |
| Codex | 自动安装到 `~/.codex/skills/acornfox/` |
| 其他工具 | `acornfox skill install --dir 目录`，或 `acornfox skill print` 输出内容 |

### 使用示例

```
你: 帮我把这个项目部署到我的服务器

AI: 已生成 Dockerfile，开始部署。
    [health] 容器启动后退出或反复重启
    日志片段：Error: DATABASE_URL is not set
AI: 诊断显示缺少数据库，添加 PostgreSQL 并重新部署。
    ✓ 已上线 http://服务器IP:18810
```

## 常用命令

```bash
# 部署
acornfox deploy                              # 部署当前目录
acornfox deploy --git https://github.com/... # 部署 Git 仓库
acornfox deploy --image nginx:latest         # 部署 Docker 镜像

# 状态
acornfox status                              # 查看应用状态
acornfox apps                                # 列出所有应用
acornfox logs --app my-app -f               # 查看实时日志

# 生命周期
acornfox stop/start/restart --app my-app    # 启停应用
acornfox redeploy --app my-app              # 重新部署
acornfox rollback --to 5 --app my-app       # 回退到第 5 版（acornfox versions 查看可回退的版本）
acornfox delete --app my-app                # 删除应用

# 附加服务
acornfox add postgres --app my-app          # 添加 PostgreSQL
acornfox add mysql --app my-app             # 添加 MySQL
acornfox add redis --app my-app             # 添加 Redis

# 域名
acornfox domain add example.com --app my-app # 绑定域名（自动 HTTPS）

# 应用中心
acornfox catalog list                       # 看目录里的应用和配方
acornfox install 应用 --plan                # 先看计划：需要的服务接哪条、还缺什么（只读）
acornfox install 应用                       # 从目录装一个应用
acornfox install status 任务编号 --wait      # 等到装好或失败再返回（最多等 90 秒，没装完再运行一次）
acornfox services add smtp --name 我的邮箱 --set host=mail.example.com --set password=-   # 登记一条你已有的服务（密码从键盘输入）
acornfox services list                      # 这台机器上的服务、状态、谁在用
acornfox apps vars 应用                     # 看一个应用是用什么设置装的
acornfox open 应用                          # 在自己电脑上打开只本机的应用

# 出了问题
acornfox faults CAT-INST-24                 # 一个故障编号是什么意思、怎么处理（只读，不连服务器）
sudo acornfox doctor                        # 在服务器上自检
```

## 架构

AcornFox 由三部分组成：

- **AcornFox** - 状态管理和协调
- **Docker** - 容器运行时
- **Caddy** - HTTP 路由和 HTTPS（跑在入口容器 `acornfox-edge` 里，用上游原版）

设计原则：
- **状态与执行分离** - server 无 Docker 权限，runner 只接受类型化请求
- **调和而非编排** - 记录"应该是"，让 runner 自动收敛
- **应用独立性** - 应用由 Docker 管理，AcornFox 崩溃不影响运行中的应用
- **诊断即产品** - 失败时返回结构化诊断，AI 可自动修复

## 中国大陆说明

### 域名与备案

- **已备案域名**: 自动申请 HTTPS 证书，通过 80/443 端口访问
- **未备案域名**: 绑定时会收到提示，但可通过 IP:端口正常访问
- **IP 访问**: 不受备案限制，始终可用

### 网络优化

安装脚本读云厂商的元数据，判断服务器在哪家云、是不是中国大陆地域，然后配置：
- 系统软件源（APT）和 Docker 安装包：阿里云、腾讯云、火山引擎的服务器先用所在云的内网源（不论哪个地域，香港、新加坡等境外地域也用），连不上再换下一个
- 中国大陆服务器（包括华为云）接着对几家国内镜像站（阿里云、清华、中科大、南大、上海交大、华为云、腾讯云）当场测快慢，用能用而且最快的；境外服务器用 Docker 官方源
- 拉镜像：下载源分层（所在云的内网加速 → 别人公开的国内副本 → Docker Hub、ghcr.io 本身），国内拉之前先实测，不能用的跳过、慢的排后，拉的过程中太慢会换下一个源；国内网络不连 Docker Hub、ghcr.io 本身（它们在国内被屏蔽）。`acornfox sources --probe` 看每个源现在怎样
- 香港、新加坡、东京等境外地域按海外处理：从 GitHub 下载，不配国内公共镜像站和 Docker 镜像加速
- 入口代理的镜像随发行版的入口包从国内下载源（Gitee）获取，不访问镜像仓库
- 判断不出是哪家云时，按 IP 归属地判断是否在中国大陆

判断错了可以手动指定（通过管道给脚本传参数）：
```bash
# 强制使用中国镜像源
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- --china

# 强制使用国际源
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- --global
```

## 文档

- [使用手册](docs/user-guide.md)
- [快速开始](docs/quick-start.md)
- [命令参考](docs/acornfox/cli.md)
- [隐私和联网说明](docs/user-guide.md#隐私和联网说明)
- [常见问题](docs/user-guide.md#常见问题)
- [AI 工作台 Skill](skills/acornfox/SKILL.md)
- [能力清单](docs/capabilities.md)
- [故障目录](docs/faults.md)
- [发行说明](docs/release-notes/)

## 升级

```bash
# 服务器端升级（中国大陆）
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade
# 服务器端升级（其他地区）
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- upgrade

# 客户端升级（Linux、Mac 电脑；中国大陆）
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli
# 客户端升级（Linux、Mac 电脑；其他地区）
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli
```

Windows 电脑本机：把安装那一整行末尾加 ` -Action upgrade`（先加 ` -Plan` 看会做什么）。

通过管道运行（包括 AI 工作台代为执行）就视为同意升级，脚本开头会先列出要做的事。已经是最新版本时直接退出，不停服务、不重装，重复运行也没关系。只有要重装同一版本时才加 `-y`（`| sudo bash -s -- -y`）。

升级过程：
- 先下载新版本并校验 SHA256；下载失败不会动现有安装
- 停止 AcornFox 服务，备份程序和数据库（放在 `/var/lib/acornfox/backups/`，保留最近 5 份），再执行数据库迁移
- 数据库迁移失败、新版本服务起不来、或升级中途出错时，自动恢复到升级前（程序和数据库都换回原来的，等于没升上）
- 应用容器在升级过程中一直运行；升级期间命令行和控制台会暂时连不上
- 换入口代理容器时（例如第一次升到 v0.6.0），新建的访问不受影响，但已经连着的长连接、超过 8 秒还没做完的请求和 WebSocket 会断一次；现在装的是 v0.3.x 的要先升到 v0.5.1
- 从 v0.4.x 升级到 v0.5.0 后，控制台换成新版，旧控制台已经去掉
- 升级不会打开局域网控制台；原来开着的照常能用

AcornFox 本身只能升级，不能回到旧版本：没有手动回滚，指定比现在旧的版本也会被拒绝。应用自己的版本照常可以用 `acornfox rollback 应用名` 回退。详见 [使用手册](docs/user-guide.md#只能升级)。

## 贡献

欢迎贡献代码、文档或提出建议。

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing`)
5. 创建 Pull Request

请阅读 [贡献指南](CONTRIBUTING.md) 了解详情。

## 许可证

本项目采用 AGPL-3.0 许可证 - 详见 [LICENSE](LICENSE) 文件。

## 致谢

AcornFox 用到的主要开源项目：

- [Docker](https://www.docker.com/) - 容器运行时
- [Caddy](https://caddyserver.com/) - HTTP 服务器和自动 HTTPS
- [SQLite](https://www.sqlite.org/) - 嵌入式数据库
- 所有贡献者和用户

## 联系方式

- 问题反馈: [GitHub Issues](https://github.com/OrbitMaker/AcornFox/issues)
- 文档: https://github.com/OrbitMaker/AcornFox/tree/main/docs

