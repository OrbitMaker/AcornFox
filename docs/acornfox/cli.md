# acornfox 命令参考

这份参考按用途列出 `acornfox` 的命令，每条一句话加一个例子，和 `acornfox help` 的内容一致。每个命令的全部参数以命令自己的帮助为准：在命令后面加 `-h`，例如 `acornfox install -h`。

同一个 `acornfox` 程序既是命令行，也是服务器上的 AcornFox 服务。Linux 服务器用安装脚本安装，装好后服务器上就有这个命令；Mac、Linux 电脑可以另装一个命令行，经 SSH 管理服务器。安装方法见 [使用手册](../user-guide.md)。

标着“v0.5.1 起”“v0.6.0 起”的要那个版本以上的 `acornfox`；其中要连服务器的命令，服务器也要是 0.5.1（`install status --wait` 例外，连 0.5.0 的服务器也能用；`faults` 不连服务器）。

## 全局参数

全局参数可以写在命令前面或后面，`acornfox --json doctor` 和 `acornfox doctor --json` 一样。

| 参数 | 作用 |
| --- | --- |
| `--target 服务器` | 这次用哪台服务器（默认用 `target use` 设的那台） |
| `--app 应用` | 这次操作哪个应用（默认用当前目录对应的应用） |
| `--json` | 输出 JSON：成功是 `{"ok":true, …}`，失败带 `diagnosis`（`code`、`message`、`hint`），方便 AI 工作台读取 |

退出码：`0` 成功；`1` 操作做完了但失败（例如部署失败、安装失败）；`2` 参数不对；`3` 连不上服务器；`4` 服务器拒绝。

不加 `--json` 时，出错的输出里有一行 `故障编号：编号（运行 acornfox faults 编号 查处理办法）`；不是故障的通用错误码写成 `错误码：…`（v0.5.1 起）。

有的命令可以直接写应用名，例如 `acornfox logs web`；其余的用 `--app`，例如 `acornfox status --app web`。下面的例子按各命令现在的写法给出。

## 连接服务器

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `target add 名称 --ssh 用户@主机` | 登记一台服务器并测试连接（`--port` 指定 SSH 端口，`--identity` 指定私钥）。第一次连接前先用 `ssh` 登录一次核对服务器指纹 | `acornfox target add my-server --ssh root@203.0.113.10` |
| `target add 名称 --url 地址` | 在服务器本机登记本机的 AcornFox 服务（Windows 用户经 SSH 在服务器上用） | `acornfox target add local --url unix:///run/acornfox/api.sock` |
| `target list` | 列出登记过的服务器 | `acornfox target list` |
| `target use 名称` | 切换默认服务器 | `acornfox target use my-server` |
| `target remove 名称` | 删除登记（不动服务器） | `acornfox target remove my-server` |
| `open` | 打开网页控制台：经 SSH 转发到你的电脑并打开浏览器，按 Ctrl+C 关闭（`--no-browser` 只打印登录链接）。`--detach` 把隧道放到后台、命令马上结束（AI 工作台里用这个），`--stop` 关掉后台隧道 | `acornfox open`、`acornfox open --detach` |

## 部署和版本

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `deploy` | 部署当前目录（根目录要有 Dockerfile），默认等到上线或失败（`--no-wait` 只提交，`--timeout` 改等待时间） | `acornfox deploy` |
| `deploy --image 镜像` | 部署现成的 Docker 镜像（`--port` 指定容器端口） | `acornfox deploy --image nginx:1.27-alpine --port 80 --app hello` |
| `deploy --git 仓库` | 部署公开的 Git 仓库（`--ref` 指定分支或提交） | `acornfox deploy --git https://gitee.com/你的账号/你的仓库.git` |
| `redeploy [应用]` | 按当前程序重新部署一次，让改过的环境变量和设置生效 | `acornfox redeploy web` |
| `versions [应用]` | 版本记录：每一版的来源、用时、失败原因、能不能回退 | `acornfox versions web` |
| `rollback [应用] [--to N]` | 应用回到上一个可回退的版本或第 N 版；只换程序，不动数据库、数据目录和环境变量（`--cancel-pending` 取消正在进行的部署） | `acornfox rollback web --to 7` |

`rollback` 回退的是你的应用，不是 AcornFox 平台。AcornFox 平台本身只能升级，见 [使用手册的“只能升级”](../user-guide.md#只能升级)。

## 备份和恢复

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `backup create [应用] [--note 话] [--plan] [--no-wait]` | 给应用的数据做一份手动备份（应用暂停到打完包，自动继续）；`--plan` 只看会怎样、磁盘够不够 | `acornfox backup create web --note 升级之前` |
| `backup list [应用]` | 列出应用的备份（新的在前：编号、种类、时间、大小、当时的版本）；不带应用名看整个备份目录、各应用份数和磁盘 | `acornfox backup list web` |
| `backup show 应用 编号\|latest [--verify]` | 一份备份的清单；`--verify` 核对每个文件的校验和 | `acornfox backup show web latest --verify` |
| `backup restore 应用 编号\|latest [--plan] [--yes] [--allow-current-version]` | 用备份覆盖应用现在的数据：先自动另存现在的、换数据和变量、按备份时的版本启动，失败自动退回；覆盖数据要用户同意，`--yes` | `acornfox backup restore web latest --yes` |
| `backup rm 应用 编号 [--yes]` | 删一份备份（删了找不回来，不加 `--yes` 只提示） | `acornfox backup rm web 20261124-153012-ab12 --yes` |
| `backup task 任务编号` | 看一次备份或恢复做到哪一步 | `acornfox backup task 0123456789ab` |

都支持 `--json`。故障编号 `BKP-01`～`BKP-16`，`acornfox faults BKP-01` 看原因和处理办法。说明见 [使用手册的“备份和恢复应用数据”](../user-guide.md#备份和恢复应用数据)。

## 日志和状态

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `status` | 应用的状态和访问地址；从应用中心装的应用还列出“它用的服务”（每个需要接的哪条服务、现在能不能用，v0.6.0 起）；跟一个部署编号时看那次部署 | `acornfox status --app web` |
| `apps` | 列出这台服务器上的所有应用；只本机能打开的标“只本机” | `acornfox apps` |
| `diagnose [部署编号]` | 最近一次（或指定的）部署为什么失败、怎么办 | `acornfox diagnose` |
| `logs [应用]` | 应用最近的日志（`-f` 持续跟随，`--tail N` 行数） | `acornfox logs web -f` |
| `logs [应用] --version N` | 第 N 版的运行日志，加 `--build` 看构建日志 | `acornfox logs web --version 6 --build` |
| `stats [应用]` | 应用的 CPU、内存等资源使用；不带应用名是整台服务器 | `acornfox stats web` |
| `stats 应用 --history 1h\|24h` | 这个应用近 1 小时或 24 小时的 CPU、内存（每 30 秒记一次，只存在内存里，后台服务重启后从头记；v0.5.1 起） | `acornfox stats web --history 1h` |

## 运行

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `stop`、`start`、`restart` | 停止、启动、重启应用 | `acornfox restart --app web` |
| `delete 应用名` | 删除应用，默认保留数据卷；加 `--volumes` 连数据一起删，不能恢复。请一定写上应用名 | `acornfox delete web` |
| `limits` | 整台服务器最多分给应用多少内存和 CPU，以及各应用已分配多少（`--memory`、`--cpu` 修改，`auto` 恢复自动） | `acornfox limits --memory 1800` |

## 配置

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `env set KEY=VALUE` | 设环境变量（`--secret` 标记为密钥，值不再显示）；改完用 `redeploy` 生效 | `acornfox env set API_KEY=xxx --secret --app web` |
| `env unset KEY`、`env list` | 删除、列出环境变量 | `acornfox env list --app web` |
| `volume add 路径` | 给应用加数据目录（容器里的绝对路径），下次部署生效；`volume list` 列出 | `acornfox volume add /app/data --app web` |
| `app set` | 改应用的内存（`--memory`，MB）、CPU（`--cpu`，核）、容器端口（`--port`）、健康检查路径（`--health-path`）、暴露方式（`--expose local\|lan\|public\|default`）；内存和 CPU 立即生效 | `acornfox app set --app web --memory 512 --cpu 1` |

## 数据库

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `add postgres`、`add mysql`、`add redis` | 给应用加数据库或缓存，连接信息以 `DATABASE_URL`、`REDIS_URL` 交给应用，应用自动重新部署一次 | `acornfox add postgres --app web` |
| `remove 类型` | 移除数据库或缓存，默认保留数据卷；`--volumes` 连数据一起删 | `acornfox remove redis --app web` |
| `addons` | 列出应用已添加的数据库和缓存 | `acornfox addons --app web` |

## 对外访问和域名

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `domain add 域名` | 给应用绑定域名，自动申请 HTTPS 证书（国内服务器的域名要已备案） | `acornfox domain add app.example.com --app web` |
| `domain remove 域名`、`domain list` | 解绑、列出域名 | `acornfox domain list --app web` |
| `lan-console enable` | 在局域网里打开控制台（端口 18802）：第一次打开时在浏览器里设密码，登录时可勾“记住这台设备 90 天”；只给局域网里的地址用，云主机开不了。局域网服务器第一次安装时默认已打开。`status` 看状态和密码是何时从哪设的，`disable` 关闭，`reset-password` 清掉密码重设（所有登录和记住的设备作废），`devices` 看记住的设备，`forget 编号` 或 `forget --all` 让设备失效；`lan-console --help` 看全 | `acornfox lan-console status` |
| `public-console enable 域名` | 用一个域名开放控制台的公网访问，带 HTTPS（`disable` 关闭，`status` 查看） | `acornfox public-console enable panel.example.com` |
| `open 应用名` | 在你自己的电脑上打开一个应用：经 SSH 隧道转到本地端口，只本机能打开的应用也能这样开，看完按 Ctrl+C 关闭 | `acornfox open web` |

## 应用中心

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `catalog list` | 目录里的应用和配方、目录序号、上次同步结果 | `acornfox catalog list` |
| `catalog info 应用` | 应用详情、安装时能填的参数、需要的服务和能提供的服务 | `acornfox catalog info 应用` |
| `catalog sync` | 让服务器现在同步一次目录 | `acornfox catalog sync` |
| `catalog platform-vars` | 平台按这台机器自动填给应用的时区、下载源、访问地址等，以及哪些能改 | `acornfox catalog platform-vars` |
| `install 应用 --plan` | 只看安装前的计划，什么都不装：它需要的每一种服务接哪条（已接上、可选的没有先跳过、必须的没有、有好几条要选）、还缺哪些必填参数、装不了的原因。必须的没有时给出该运行的命令：登记一条（保密项写成 `项=-`），或者让这台机器上已有、现在用不了的那条能用（启动提供方、填上还差的项、用旧数据把已删的提供方装回来）。配方也能看（`--recipe 配方 --plan`）。只读，v0.6.0 起 | `acornfox install 应用 --plan` |
| `install 应用` | 从目录装一个应用，默认按这台机器给新应用的对外方式（局域网里新装的服务器上局域网能打开，云主机上只本机能打开；`--expose` 可以改）（`--set 参数=值` 填参数，保密的参数写成 `--set 参数=-` 从键盘输入；`--use 需要名=服务名` 指定某个需要接哪条服务，`--no-wait` 只提交，`--reuse-data` 接上以前同名应用留下的数据）。必须的服务没有时停下，告诉你怎么登记一条；不会替你装别的应用。目录里一个可装的应用都没有时，直接说要等应用按新格式重新上架 | `acornfox install 应用` |
| `install --name 应用名 --reuse-data` | 用以前留下的数据把删掉的应用装回来，不用写它是目录里哪个应用：平台按留下的数据找回来。v0.6.0 起 | `acornfox install --name 应用名 --reuse-data --plan` |
| `install --recipe 配方` | 按配方的顺序装几个应用并接上：后装的应用接先装的应用提供的服务 | `acornfox install --recipe 配方` |
| `install status 任务编号` | 查安装进度和首次登录信息（`--show-secrets` 显示密码）。加 `--wait` 等到装好或失败再返回（默认最多 90 秒，`--timeout 秒` 最多 600，没装完再运行一次），输出写明装到哪一步、用了多久、算不算比平常慢（`CAT-INST-30`）；v0.5.1 起 | `acornfox install status 3f9a0c1b2d4e --wait` |
| `apps vars 应用` | 看一个从应用中心装的应用是用什么设置装的：每个变量的值、谁给的、能不能改；密码和 Key 默认只显示后 4 位（`--reveal` 显示全）。用旧格式清单装的应用会说明（`acornfox status 应用` 里也有这句说明）。只读，v0.5.1 起 | `acornfox apps vars 应用` |

局域网里新装的服务器上，装好的应用默认同一局域网里的电脑、手机就能打开（用安装结果里的地址）；云主机上默认只本机能打开，在你自己的电脑上用 `acornfox open 应用名` 打开。详细说明见 [使用手册的“应用中心”](../user-guide.md#应用中心)。

## 服务（v0.6.0 起）

服务是这台机器上能给应用用的东西：装好的应用提供的（装好就自动登记），或者你自己已有、登记进来的（例如自己的邮箱、别家的大模型 Key）。装应用时按服务类型自动接上。密码、Key 这类保密项不要写在命令行里：写成 `项=-`，命令会让你输入；所有输出里保密项只显示后 4 位。装应用时的保密参数同样能写成 `install --set 参数=-`（v0.6.0 起）。

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `services kinds` | 有哪些服务类型、每种有哪几项（哪些必填、哪些保密、默认值）。只读 | `acornfox services kinds` |
| `services list` | 这台机器上的服务：类型、来源（自己登记的或由哪个应用提供）、状态、哪些应用在用。只读 | `acornfox services list` |
| `services show 名字` | 一条服务的每一项；要你在提供它的应用里做一步再填的项会写明怎么做。只读 | `acornfox services show 我的邮箱` |
| `services add 类型 --name 名字 --set 项=值…` | 登记一条你已有的服务；地址不能写本机回环地址（`CAT-SVC-07`） | `acornfox services add smtp --name 我的邮箱 --set host=mail.example.com --set password=-` |
| `services set 名字 项=值…` | 改几项；`项=` 清掉一项。应用提供的服务只能填要你做一步的那几项 | `acornfox services set 我的邮箱 port=465` |
| `services test 名字` | 照这种服务类型的办法试一下通不通（会从服务器去连那个地址），没通时写明原因（`CAT-SVC-06`） | `acornfox services test 我的邮箱` |
| `services remove 名字` | 删一条自己登记的服务；不加 `--yes` 只试跑。有应用在用时删不掉（`CAT-SVC-04`） | `acornfox services remove 我的邮箱 --yes` |

## 本机服务和自检

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `doctor` | 检查这台服务器：Docker、后台服务、磁盘空间、端口占用、时钟，以及下载网关能不能连上、应用目录上次同步的情况（这两项 v0.5.1 起，只给警告）、已装的应用接着的服务现在能不能用（`CAT-SVC-08`，v0.6.0 起，只给警告）；每个问题带故障编号（`--json` 输出 JSON）。用 sudo 运行看得更全 | `sudo acornfox doctor` |
| `faults [编号]` | 故障目录：一个故障编号是什么、可能的原因和怎么判断、按什么顺序查、怎么处理、什么时候要先问你；不带编号列出全部，`--group 组` 只列一组，`--markdown` 输出全文。只读，不连服务器，Mac 上也能用；v0.5.1 起 | `acornfox faults CAT-INST-27` |
| `capabilities [--json]` | 这个 acornfox 程序自己能做什么：每条命令和参数、故障编号、平台变量、内置的服务类型、清单格式各项功能从哪个版本起支持、通用 Skill 里登记的通用做法。只读，不连服务器；查“这个版本有没有某条命令”以它为准。v0.7.0 起 | `acornfox --json capabilities` |
| `identity show` | 这台机器在下载网关上的安装编号和公钥（还没生成时说明原因，不会生成） | `sudo acornfox identity show` |
| `identity reset` | 重新生成机器钥匙，安装编号随之改变；不加 `--yes` 只说明会做什么。之后 `sudo systemctl restart acornfox-server` 才生效 | `sudo acornfox identity reset --yes` |
| `version` | 版本号 | `acornfox version` |

## Mac 本机（装在这台 Mac 上的 AcornFox，苹果芯片、macOS 26 起）

AcornFox 在 Mac 上是苹果 container 开的一台 Debian 虚拟机；下面的命令管 Mac 这一侧。装好之前用一行命令 `curl -fsSL <get.sh 地址> | bash`（见 README）。所有动作支持 `--json`；`install`、`upgrade`、`uninstall` 支持 `--plan`（试跑，什么都不改）。

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `mac install` / `mac upgrade` | 安装、升级（每一步做过就跳过，重复运行不出错）；要管理员密码的一步（装苹果的 container）由你自己在终端里输，没有终端时停下并打印命令，退出码 5。选项：`--idle 分钟\|off`、`--memory GB`、`--cpus N`、`--no-lan`、`--no-open`、`--china`/`--global`、`--version`、`--release-base`、`--local-package` | `acornfox mac install --plan` |
| `mac status` | 虚拟机、常驻程序、转发端口的状态；不会把停着的虚拟机叫起来 | `acornfox mac status --json` |
| `mac doctor` | 逐项检查，每项带故障编号 `MAC-…`；`--wake` 先叫起虚拟机再查里面。`acornfox doctor` 在装了 Mac 本机版的电脑上等于它加 Skill 版本核对加虚拟机里的 doctor | `acornfox mac doctor --json` |
| `mac stop` / `mac start` | 现在停掉（把内存还给 macOS）/ 现在叫起虚拟机；有人访问时也会自动叫起 | `acornfox mac stop` |
| `mac set` | 改设置：`--idle 分钟\|off`、`--lan on\|off`、`--memory GB`、`--cpus N`（下次启动生效）、`--public-host 地址\|auto`（Mac 的局域网地址变了） | `acornfox mac set --idle 60` |
| `mac uninstall` | 卸载；默认保留虚拟机和里面的数据，`--purge --yes` 连数据一起删。苹果的 container 本身不卸 | `acornfox mac uninstall --plan` |
| `mac daemon` | 常驻小程序，由 launchd 在登录后运行，不用手工运行 | — |
| `mac pack-base 根文件系统.tar.xz 输出.oci.tar` | 做虚拟机的基础镜像包（准备放进下载网关的文件）；只在 Mac 上能用 | — |

## Skill

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `skill install` | 把和命令行同版本的 AcornFox Skill（入口 `SKILL.md` 加全部分册）装到 AI 工作台（Claude Code、Codex；`--dir` 指定其他目录）；每个文件头带命令行的版本号；旧版本留下的分册会删掉，`machine-*.md` 保留 | `acornfox skill install` |
| `skill print` | 把 Skill 内容打印出来，给其他工具用：默认打印入口，`--all` 打印全部文件，`--file 文件名` 打印一册（例如 `install-linux.md`） | `acornfox skill print > SKILL.md`、`acornfox skill print --file deploy.md` |
| `skill refresh` | 重新写 Skill 目录里的“本机能力”分册 `machine-服务器名.md`（`skill install` 会给每台登记的服务器写一份；命令行里装、删、升级、退回、恢复应用和改设置之后自己刷新，在页面上动过应用后手动运行）；`--target` 只刷新一台，已经不登记的服务器的旧分册会删掉。里面没有密码和 Key。写不进目录 `MACHINE-02`，连不上服务器 `MACHINE-03`。v0.7.0 起 | `acornfox skill refresh` |
| `machine` | 这台服务器现在能做什么：装了哪些应用和解决方案、状态、怎么打开、有没有手册、有哪些服务和谁在用、平台能备份和升级吗。只读，现场算，没有密码和 Key；AI 工作台开始做事前先看它。v0.7.0 起，服务器也要是 0.7.0 | `acornfox --json machine` |

## 目录维护（给维护应用目录的人用）

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `catalog lint 目录` | 检查应用目录的清单和配方，只读 | `acornfox catalog lint .` |
| `catalog vars 目录` | 列出应用装的时候用到哪些变量、哪些要用户动手、需要和提供哪些服务（`--app` 只看一个），只读 | `acornfox catalog vars . --app 应用` |
| `catalog gaps 目录` | 核对目录里登记的“平台还缺的变量”，只读 | `acornfox catalog gaps .` |
| `catalog build`、`catalog keygen`、`catalog verify` | 打包签名发布目录、生成签名钥匙、校验发布目录；各自加 `-h` 看用法 | `acornfox catalog verify dist --pubkey 公钥` |
