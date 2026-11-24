# 在这台 Mac 上安装 AcornFox（情况戊）

**什么时候读这一册**：用户的电脑是苹果芯片的 Mac（M1 及以后）、macOS 26 或更新，想把 AcornFox 装在这台 Mac 上。Intel 的 Mac（`MAC-ARCH-01`）、macOS 低于 26（`MAC-OS-01`）装不了：照实说，改走情况乙（装在一台 Linux 服务器上，这台 Mac 只当命令行客户端）。

**它是什么**：Mac 本机只有三样东西：苹果的 `container`（苹果签名的安装包）、一个常驻小程序（登录后由 launchd 自动运行）、`acornfox` 命令。AcornFox 装在 `container machine` 开的一台 Debian 13 虚拟机里（名字 `acornfox`，里面是和 Linux 服务器同一个程序、同一个 `install.sh`，Docker 和应用都在里面）。要知道的几件事（回答用户的问题时用）：

- **内存**：虚拟机内存上限默认 6 GB（Mac 内存 16 GB 以上）或 4 GB。它用过多少内存页，Mac 上就占多少，**只有停掉虚拟机才还给 macOS**。所以常驻程序在**闲置 30 分钟没人访问**时停虚拟机，有人访问（浏览器、`acornfox` 命令、应用的端口）再自动叫起，**第一次访问要等 2～3 秒**。虚拟机停着时里面的应用也停着（应用里的定时任务不会跑）；要一直开着：`acornfox mac set --idle off`。
- **怎么访问**：控制台用浏览器开 `http://127.0.0.1:18802/`（第一次打开设密码）；`acornfox` 命令经 Mac 上本机的转发端口（127.0.0.1:18808）用 ssh 连虚拟机，登记的服务器叫 `local`；应用端口在 Mac 上是同一个号（18810 起），“只本机”的应用只在 127.0.0.1，其余的局域网里的电脑、手机也能开（`acornfox mac set --lan off` 全部关掉）。不需要 `acornfox open`。
- **虚拟机的地址每次开机都变**：不用管，常驻程序会跟上；应用地址里写的是 Mac 的局域网地址，Mac 换了地址（路由器重新分配）时 `acornfox mac doctor` 报 `MAC-NET-01`，运行 `acornfox mac set --public-host auto`。
- **只有 arm64 的镜像能跑**：有的应用只提供 x86（amd64）镜像，在这里起不来。
- 虚拟机不挂 Mac 的家目录（Mac 上的文件它读不到），所以 `acornfox deploy` 在 Mac 的项目目录里运行，文件由命令行传过去。

**不要在 Mac 上运行 `acornfox install`、`acornfox setup`**（它们是已经关掉的旧的本机模式，会报 `SETUP-MAC-01`）；装在 Mac 本机只用这一册里的命令。Intel 的 Mac、macOS 低于 26 装不了本机：这台 Mac 只装命令行、经 SSH 管理一台 Linux 服务器（情况乙，读 `install-linux.md` 和 `connect-server.md`）。

## 最低路径（固定步骤）

1. **只读检查**（什么都不改，退出码 0 就是能装）：在用户的“终端”里（普通用户，**不要加 sudo**）运行下面的安装命令，把 `bash` 换成 `bash -s -- install --plan`（可再加 `--json`，那时标准输出只有最后一行 JSON）。
2. **先问用户、得到明确同意**，把三件事说给他听：① 第一次装、这台 Mac 上还没有苹果的 `container` 时，要用户**自己输一次这台 Mac 的登录密码**（装苹果签名的安装包要管理员权限；输入时屏幕不显示字符，是正常的）——**你不要代输**，也不要想办法绕过；② 虚拟机要占 4～6 GB 内存上限、磁盘 12 GB 以上，下载约 1 GB（苹果的 container 170 MB；没有默认内核时再加 676 MB；Debian 镜像约 100 MB；虚拟机里装 Docker 等几百 MB）；③ 闲置 30 分钟自动停虚拟机、局域网默认能打开（不想要加 `--no-lan`、`--idle off`）。
3. **安装命令**（国内用第一条，海外用第二条）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash
```

   要带参数：`… | bash -s -- install --idle 60 --no-open`（`acornfox mac --help` 看全部）。
4. **看退出码**：`0` 成功；`5` 要管理员密码而这里没有终端（或密码没输成）——屏幕上有一行 `sudo installer -pkg … -target /`，**请用户自己在“终端”里运行它**，装好后再运行一次安装命令（已做的步骤会跳过）；`1` 看屏幕最后的故障编号，运行 `acornfox faults MAC-…` 查（没装好、`acornfox` 命令还没有时，直接看屏幕上的原因和安装记录 `~/Library/Application Support/AcornFox/mac/logs/install.log`）。
5. **验收**：`acornfox mac status --json` 里 `ok` 是 `true`；`acornfox mac doctor`（先查 Mac 一侧，再查虚拟机里的，每项带故障编号）没有失败项；`acornfox --json apps` 有回答。控制台第一次打开要设密码，由用户自己设。
6. **装 Skill**：运行 `acornfox skill install`（入口和全部分册一起，装进用户 Mac 上已经有的 AI 工作台：有 `~/.claude` 才装 Claude Code，有 `~/.codex` 才装 Codex）。
7. **之后**：`acornfox apps`、`acornfox install 应用名`、`acornfox deploy` 等和 Linux 服务器上完全一样（用哪一册看入口的对照表；默认服务器是 `local`；用户原来还登记了别的服务器且默认是别的，安装结果会写出来，要让命令默认管这台 Mac：`acornfox target use local`）。

## 日常（用户问“占内存”“怎么停”“怎么一直开着”）

| 要做什么 | 命令 |
| --- | --- |
| 看状态（不会叫起虚拟机） | `acornfox mac status`（`--json`） |
| 现在把内存还给 macOS | `acornfox mac stop`（有人访问会自动叫起） |
| 现在叫起虚拟机 | `acornfox mac start` |
| 改闲置多久停 / 一直开着 | `acornfox mac set --idle 60`、`acornfox mac set --idle off` |
| 改内存上限、CPU 数 | `acornfox mac set --memory 8`、`--cpus 2`（下次启动虚拟机时生效；要现在生效再 `acornfox mac stop`） |
| 局域网能不能打开 | `acornfox mac set --lan on`、`--lan off` |
| Mac 的局域网地址变了 | `acornfox mac set --public-host auto` |
| 逐项检查 | `acornfox mac doctor`（`--json`；`--wake` 先叫起虚拟机再查里面） |
| 升级 | 把安装命令末尾换成 `bash -s -- upgrade`；先加 `--plan` 看 |
| 看虚拟机里的安装记录 | `container machine run -n acornfox --root -i -- tail -60 /root/acornfox-install.log`（把命令写进文件再运行最稳：`container machine run` 会丢第一个命令的参数，用标准输入给脚本） |

## 卸载（固定步骤）

1. **先问用户**：只卸程序、**保留数据**（虚拟机和里面的应用、数据留着并停掉，以后重装接得上；默认就是这个），还是**连数据一起删**（不可恢复）。删数据必须用户明确说出来，不要替他选。
2. `acornfox mac uninstall --plan` 看计划，用户同意后运行 `acornfox mac uninstall`；要删数据的才加 `--purge --yes`（没有 `--yes` 报 `MAC-UNINST-01`，什么都不删）。
3. 卸载只拿掉本安装放的东西：launchd 的常驻程序、`acornfox` 命令里登记的 `local`、虚拟机（`--purge` 才删）、载入的镜像和本机记录（`--purge` 才删）。**苹果的 `container` 本身不卸**（用户可能还在用它）；`acornfox` 命令文件默认留着（它也能管别的服务器）。
4. **验收**：保留数据时 `container machine ls` 里还有 `acornfox` 且是 stopped，`launchctl print gui/$(id -u)/com.acornfox.mac` 报错（已卸载）；`--purge` 时 `container machine ls` 里没有它，`~/Library/Application Support/AcornFox/mac` 没了。

## 出错时

拿到 `MAC-…` 故障编号：照入口“遇到故障编号怎么办”运行 `acornfox faults 编号`（没装好、`acornfox` 命令还没有时，直接看屏幕上的原因和安装记录 `~/Library/Application Support/AcornFox/mac/logs/install.log`）。几件要提前知道的：

- 检查没过（macOS 低于 26、Intel 的 Mac、磁盘不够 12 GB）什么都没装：把原因告诉用户，这几项都要用户自己处理（升级系统、Intel 的 Mac 改走情况乙、清磁盘）。
- 要管理员密码没输成（`MAC-ADMIN-01`）：请用户自己在终端里运行屏幕上给的 `sudo installer …`，再重新运行安装命令。
- 下载失败或校验不过（`MAC-DL-01`、`MAC-DL-02`）：确认能上网、关代理、再运行，已下的部分会接着下，**不要跳过校验**；换源加 `--china` 或 `--global`。
- 建不了虚拟机、起不来、里面的准备没成（`MAC-VM-01`、`MAC-VM-02`、`MAC-PROV-01`）：`acornfox mac doctor --wake`、`container machine logs acornfox`；同名虚拟机不是本安装建的，不要删，问用户。
- 虚拟机里装 AcornFox 没成（`MAC-LINUX-01`）：看屏幕上带出的 `INSTALL-…` 等编号，`acornfox faults 那个编号`。
- 常驻程序没运行、端口被占、命令连不上虚拟机（`MAC-LAUNCH-01`、`MAC-PORT-01`、`MAC-SSH-01`）：`acornfox mac status`、`acornfox mac doctor`，重新运行安装命令只补缺的部分。
- Mac 的局域网地址变了（`MAC-NET-01`）：`acornfox mac set --public-host auto`。

## 可以发挥的地方

- 向用户解释每一步在做什么、会装什么、为什么要输密码和为什么虚拟机会自动停；回答“占多少内存”“能不能一直开着”“别的设备怎么打开”。
- 用户说“慢”：先看是不是第一次访问在叫起虚拟机（日志 `~/Library/Application Support/AcornFox/mac/logs/daemon.log` 里有“叫起虚拟机”），再看下载（国内第一次拉镜像慢，经下载网关分段多路已经在加速）。
- 应用要跑得久（定时任务、机器人）：建议 `acornfox mac set --idle off`，并说明内存会一直占着。

## 必须先问用户的

- 输管理员密码、`sudo installer …`：只能用户自己做。
- `acornfox mac uninstall --purge --yes`（删数据）；`acornfox mac set --memory`、`--cpus`（影响占用）；`container system stop`（会停掉用户别的 container 容器）；`container machine delete acornfox`（等于删数据）。
- 改 Mac 的防火墙、网络、代理、DNS：**不要碰**；安全软件弹窗请用户选“允许”。
