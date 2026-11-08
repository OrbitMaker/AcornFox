# 单机版能力清单

更新：2026-11-08。

这份清单回答一个问题：**单机版现在能做什么，每项能力从哪里调用。** 写给接手的人和接入的 AI 工作台看。

## 怎么用

- 每项能力都有 HTTP 接口（`acornfox/internal/apiserver/apiserver.go`）、CLI（`acornfox/internal/cli/`）和 Skill（`skills/acornfox/SKILL.md`）。网页控制台也调同一个接口。
- 命令的参数以 `acornfox help` 和各命令的 `-h` 为准，本文件不重复抄命令细节。
- 加全局参数 `--json` 时，成功输出 `{"ok":true, …}`，失败输出带诊断的 JSON。

## 影响分级

- **只读**：随时可以调用。
- **会改数据**：改应用、配置或服务器上的状态，Skill 里写明什么时候要先问用户。
- **会删数据**：`delete --volumes`、移除数据库这类不可恢复的操作，必须用户明确要求。

## 已有能力

| 能力 | 回答什么问题 / 做什么 | 接口 | 命令 | 影响 |
| --- | --- | --- | --- | --- |
| 服务器管理 | 本机 CLI 连哪台服务器 | 本地配置，不走接口 | `target add/list/use/remove` | 会改本地配置 |
| 网页控制台 | 打开控制台。v0.5 起默认是新控制台（源码 `acornfox/web/console`，产物嵌在 `internal/console/ui`）：首页看板（可编辑网格，布局存在浏览器里）、应用中心（浏览、搜索、配方、安装弹窗、安装进度、首次登录；“连接私有目录”和来源筛选这一版藏起，开关是 `web/console/src/lib/features.ts` 的 `PRIVATE_CATALOG_UI`）、我的应用（列表、只本机的标“只本机”、七个标签页）、运行环境（自检带故障编号）、设置（通用、访问、运行环境、存储、下载源、升级、开发者与 AI；升级只给升级命令，不给回滚）。只本机的应用不给 127.0.0.1 的“打开”链接，改成“怎么打开”（见下面“打开应用”）。云版上线前不显示任何“AFX 云”入口。旧控制台已去掉，`/legacy`、`/legacy/…` 跳到 `/` | `POST /v1/console/tokens`（只受信入口）；页面调的都是本表里的接口 | `open` | 只读（页面上的操作走各自的接口） |
| 控制台公网访问 | 用一个域名开放控制台（默认不开放） | `/v1/public-console`、`/verify`、`/enable`（受信入口；控制台入口只有 `GET /v1/public-console` 看状态） | `public-console enable/disable/status` | 会改数据 |
| 部署 | 把当前目录、镜像或 Git 仓库部署成应用 | `POST /v1/apps/{app}/deployments` | `deploy` | 会改数据 |
| 重新部署、回退 | 让配置生效（和部署一样默认等到上线或失败）；回到上一个可回退的版本，或指定的第 N 版。回退只换程序，不重新构建，数据库、数据目录和环境变量不变；保留除当前版本外最近 5 个不同的程序。有部署正在进行时要明确取消（`cancel_pending`） | `POST /v1/apps/{app}/redeploy`、`POST /v1/apps/{app}/rollback`、`POST /v1/apps/{app}/versions/{seq}/rollback`（请求体可空或 `{"cancel_pending":true}`，支持 `Idempotency-Key`） | `redeploy [APP] [--no-wait] [--timeout 20m]`、`rollback [APP] [--to N] [--cancel-pending]` | 会改数据 |
| 版本记录 | 每一版的状态、来源、用时、失败原因、能否回退（服务端算好，页面和 CLI 直接显示），以及不指定版本时会回到第几版 | `GET /v1/apps/{app}/deployments?limit=N`（`rollback_target_seq`、`image_check`）；应用视图的 `rollback_target_seq`；`GET /v1/status` 的 `features` | `versions [APP] [--limit N]` | 只读 |
| 应用状态 | 应用在不在跑、地址是什么。只本机能打开的应用（`exposure: local`），Linux 上地址是 `http://127.0.0.1:端口`、带 `local_only: true` 和 `open_hint`（怎么从自己电脑打开，见下面“打开应用”），`apps` 标“只本机”并给出 `acornfox open 应用名`。`live_seq` 是正在服务的是第几版（列表里也有，列表不带完整的 `live`；控制台“我的应用”卡片按它写“第 N 版”，没有上线版本时写“第 N 版部署中”“还没有上线的版本”或“还没部署”）。详情里 `gateway_token_from` 是给这个应用发了不限额令牌的本机 New API 的应用名（控制台和 `install` 输出据此提示“可在 New API 后台给令牌设上限”） | `GET /v1/apps`、`GET /v1/apps/{app}`、`GET /v1/status` | `status [应用名 \| 部署编号]`（参数按应用名解析，和 `logs`、`rollback`、`delete` 同一规则；12 位 0-9、a-f 的写法是部署编号，同时也是合法应用名时先按应用查、没有再按部署查；都不像时报用法错误；应用不存在报 `not_found` 并提示 `acornfox apps`）、`apps`、`delete 应用名 [--volumes]`（不写应用名时用当前目录对应的应用） | 只读（`delete` 是写操作，要用户同意） |
| 部署诊断 | 最近一次部署为什么失败、怎么办 | `GET /v1/deployments/{id}`、`GET /v1/apps/{app}/deployments` | `diagnose` | 只读 |
| 启停 | 停止、启动、重启应用。命令行默认等到观察状态变了才返回：`stop` 等到 stopped（30 秒没停下说明仍在停止中），`start`、`restart` 等到 running（60 秒没起来报 `start_timeout`、退出码 1；应用没有上线过的版本报 `no_deployment`；等待被打断报 `cancelled`），所以返回后 `apps` 看到的就是新状态；`--json` 输出带 `waited`、`observed_state`，`restart` 另外照旧带 `"restarted":true`（`--no-wait` 时也带）；等待中按 Ctrl-C 或 SSH 断开（哪怕正在查询）报 `cancelled`，请求已经提交，不用再提交一次 | `POST /v1/apps/{app}/stop`、`/start`、`/restart`；等待时轮询 `GET /v1/apps/{app}` | `stop`、`start`、`restart`，都是 `[应用名] [--no-wait]`（写了应用名又写 `--app` 时两者要一致；`--no-wait` 只提交） | 会改数据 |
| 删除应用 | 删除应用，默认保留数据 | `DELETE /v1/apps/{app}` | `delete [--volumes]` | 会删数据（带 `--volumes` 时） |
| 日志 | 应用最近的日志，可以跟随；也能看某一版的运行日志（容器删除前保存，最多 1 MB）和构建日志（最多 512 KB）。每个应用保留最近 20 次部署的日志，当前版本、能回退的版本和最近一次失败的一直保留；返回前按当前密钥规则再脱敏一遍 | `GET /v1/apps/{app}/logs`、`/log-batch`、`GET /v1/deployments/{id}/logs?kind=run\|build&tail=N` | `logs [APP] [-f]`、`logs [APP] --version N [--build]` | 只读 |
| 资源使用 | 应用或整台服务器的 CPU、内存、磁盘 | `GET /v1/metrics/host`、`GET /v1/metrics/apps/{app}`、`GET /v1/host` | `stats` | 只读 |
| 整机资源历史（v0.5 开发中） | 近 1 小时、24 小时的整机 CPU、内存、磁盘（控制台首页看板用）。后台服务每 30 秒记一次，只存在内存里，最多 24 小时，重启后从头记（`since` 是最早一个点）；1 小时给原始点，24 小时给 5 分钟平均。没有 7 天；没有历史的服务端 `ranges` 为空 | `GET /v1/metrics/host/history?range=1h\|24h`（`range` 不对回 400 `invalid_range`） | —（CLI 用 `stats` 看此刻） | 只读 |
| 运行环境自检（v0.5 开发中） | 后台服务自己能查到的：服务、执行器和 Docker、网关（Caddy）、准备运行环境停在哪；Linux 上加数据目录和 Docker 数据目录所在分区的剩余空间（Docker 数据目录的位置由执行器问 Docker 拿，和 `doctor` 一样；问不到时按默认位置查，结果里写明“按默认位置”）、AcornFox 要用的端口（和 `doctor` 同一份检查代码 `internal/hostcheck`，同一套编号 `SETUP-SVC-01`、`SETUP-RT-03`、`SETUP-EDGE-01`、`SETUP-DISK-01`、`SETUP-PORT-01..05`）、时钟（`SETUP-CLOCK-01`：用 `timedatectl` 看有没有开网络对时、是否已同步，读不到时看 `/run/systemd/timesync/synchronized`；下载网关开着时再用一次 HEAD 请求取网关的 `Date` 比对（网关地址和应用中心用的是同一个：`ACORNFOX_GATEWAY`，否则 `network.json` 的 `gateway`，`off` 关闭；Linux 服务器有 `install.env` 时也认 `network.json` 的 `gateway`），各限 3 秒，没网或关了网关就只看对时状态、不报错；差 300 秒以上不通过（下载网关会拒绝），60 秒以上或没开对时、还没同步上给警告；只读，不改系统时间）。后台服务不是 root，看不到别的用户的程序占了哪个端口，这种只报 `SETUP-PORT-05` 并建议 `sudo acornfox doctor`；另给数据目录、Docker 版本和下载顺序（Docker Hub 镜像源、ghcr 镜像源、下载网关） | `GET /v1/runtime/checks`（每一项带 `check`：这项检查的编号，通过的项也有，和 `doctor --json` 一样）；编号说明 `GET /v1/setup/faults` | `sudo acornfox doctor [--json]`（看得更全） | 只读 |
| 资源上限 | 各应用的内存和 CPU 上限，整机最多分给应用多少（2 GB 以下的机器内存允许超分 2 倍，应用默认 256 MB） | `GET/PUT /v1/resources`、`PATCH /v1/apps/{app}` | `limits`、`app set` | 会改数据 |
| 环境变量 | 设置、删除、列出 | `PUT/DELETE /v1/apps/{app}/env/{key}` | `env set/unset/list` | 会改数据 |
| 数据目录 | 给应用加持久化目录 | `POST /v1/apps/{app}/volumes` | `volume add` | 会改数据 |
| 域名与 HTTPS | 绑定域名并自动申请证书 | `GET/POST /v1/apps/{app}/domains`、`DELETE …/{name}` | `domain add/remove/list` | 会改数据 |
| 数据库与缓存 | 加或移除 PostgreSQL、MySQL、Redis | `GET/POST /v1/apps/{app}/addons`、`DELETE …/{kind}` | `add`、`remove`、`addons` | 会改数据；移除会删数据 |
| Skill 安装 | 把 AcornFox Skill 装到 AI 工作台 | 不走接口 | `skill install/print` | 会改本地文件 |
| 打开应用（v0.5） | 只本机的应用怎么从用户自己的电脑打开。`open_hint`：`lan_address` 是服务器在用户局域网里的私有 IPv4（判断在 `internal/platform` 的 `ServerLANAddress`：安装时认出云厂商、应用地址的主机是公网 IP 或域名、或者云元数据地址 169.254.169.254 有应答（`CloudProbe`，后台服务启动时在后台问一次，没问出结果前也按云服务器算）时都算公网服务器，给空；拿不准就给空），`lan_url` 是改成局域网后的地址，`has_login` 表示应用有自己的登录（从目录装、清单写了首次登录账号密码的才算），两者都有时 `can_lan: true`、`recommend: lan`，否则 `recommend: tunnel`；`command` 总是 `acornfox open 应用名`。控制台据此给“让局域网里的电脑能打开”（确认后调 `PATCH /v1/apps/{app}` 改成 `lan`；确认框写明 Linux 上 `lan` 是在服务器所有网卡上开放这个端口；改完应用不再带 `open_hint`，说明块留在原位显示“已改成局域网，同一局域网的电脑用这个地址打开”和局域网地址（优先用应用视图里现在的地址），以及怎么改回只本机；首次登录卡片的地址也换成应用现在的地址）和命令；命令行 `open 应用名`：经 SSH 登记的服务器开 SSH 隧道到服务器 `127.0.0.1:应用端口`（任何对外方式都行），本机 AcornFox 直接开 `http://127.0.0.1:端口`，经网址登记的服务器只能打开局域网、公网的应用。出错编号 `OPEN-APP-01`（应用还没有访问端口）、`OPEN-APP-02`（经网址登记的服务器上只本机的应用打不开），说明写在 `open -h` 和 Skill | 应用视图的 `open_hint`；改局域网走 `PATCH /v1/apps/{app}` | `open 应用名 [--no-browser] [--port N]`（`--json` 输出 `app`、`url`、`tunnel`；退出码 0 成功、1 `OPEN-APP-*`、3 连不上或隧道建不起来） | 只读（只在用户电脑上开本地端口和 SSH 连接）；改成局域网是写操作，要用户确认 |
| 暴露方式 | 应用只本机、局域网还是所有网卡加域名能打开（afx_cloud 只占位）；Windows 自带运行环境的局域网访问由后台服务转发。应用可以不单独设、跟随平台默认：控制台“高级”里的“跟随默认（现在是…）”发 `exposure: ""`，和命令行 `--expose default` 一样；跟随默认、默认是公网时同样先确认 | `PATCH /v1/apps/{app}` 的 `exposure`（`""` 回到平台默认）；应用视图的 `exposure`（实际生效的）、`exposure_is_default`（没单独设）、`default_exposure`（平台默认）、`lan_url` | `app set --expose local/lan/public/default` | 会改数据 |
| 局域网控制台 | 在局域网打开控制台，要密码登录（默认关闭） | `GET/POST/DELETE /v1/lan-console`（开关只在可信入口；控制台入口只有 `GET` 看状态，设置页用） | `lan-console status/enable/disable` | 会改数据 |
| 平台升级（Linux 服务器） | 把服务端升级到新版本。只能升级，不能回到旧版本：没有 `--rollback`，`--version` 比当前旧的一律拒绝（加 `-y` 也一样），`install.sh` 在已装好的服务器上装更旧的版本也拒绝（卸载时保留了数据的，按 `/var/lib/acornfox/installed-version` 记下的版本判断），都在停服务之前、改动系统之前停下。升级前备份程序和数据库到 `/var/lib/acornfox/backups/`（保留 5 份）；数据库迁移失败、新版本服务起不来、中途出错或被打断（含 SSH 断开）时，用这份备份自动恢复到升级前；恢复过程不响应中断、文件原子替换，过程另记在 `/var/log/acornfox-upgrade.log`。故障编号 `UPGRADE-DOWN-01`、`UPGRADE-ROLLBACK-01`、`UPGRADE-BACKUP-01`、`UPGRADE-FAIL-01`～`03`、`UPGRADE-RESTORE-01`、`INSTALL-DOWN-01`，说明在使用手册“只能升级”一节和 Skill“升级、卸载”。应用自己的版本回退是另一项能力（`rollback`），不受影响 | 不走接口（服务器上的脚本） | `scripts/install/upgrade.sh`（`--version`、`--binary`、`-y`、`--help`） | 会停 AcornFox 服务、换程序、改数据库；应用容器不停 |
| 本机安装（Mac、Windows）（未开放；`install -h` 先显示从应用中心装应用的用法，末尾一句说明本机安装） | 在这台电脑上安装主程序、升级并后台运行（只能升级，没有 `--rollback`，传了报 `UPGRADE-ROLLBACK-01`）；装完接着准备运行环境（不需要先装 Docker） | 不走接口（写开机自启、复制程序） | `install [--no-setup]` | 会改本地文件和开机自启 |
| 准备运行环境（v0.4.0 未开放） | 四步：主程序、运行环境（已有 Docker 直接用，没有就装自带的：Mac 用 Colima，Windows 用 WSL 2 + Docker Engine）、网关组件、自检；断点续做，失败带故障编号；控制台显示同样的页面 | `GET /v1/setup`、`GET /v1/setup/events?since=&wait=`、`POST /v1/setup/start`、`/resume`、`/pause` | `setup [--runtime builtin/docker-desktop/existing] [--mirror 地址] [--confirm-admin \| --local-only] [--resume] [--pause] [--status]`，`--json` 时每行一个事件 | 会下载并安装运行环境；Windows 打开 WSL 2、加局域网防火墙规则要用户本人确认管理员弹窗 |
| 准备运行环境的故障目录 | 每个 `SETUP-*` 编号是什么、可能原因、怎么办 | `GET /v1/setup/faults` | —（`doctor` 输出里带编号） | 只读 |
| 本机卸载（Mac、Windows） | 停止并移除；默认保留数据。没有 JSON 输出：带 `--json` 时只回一条用法错误，什么都不做。Linux 服务器用 `uninstall.sh`：没有终端（经 ssh 执行、没分配终端）又没加 `-y` 时报 `UNINSTALL-TTY-01` 退出 1，什么都没改 | 不走接口（直接调 Docker） | `uninstall [--purge] [--yes]`；Linux `sudo bash uninstall.sh [--purge] [-y]` | `--purge` 会删数据 |
| 本机自检 | 运行环境准备到哪一步、Docker 在不在、后台服务、网关、局域网能不能打开（含 Colima 只绑 127.0.0.1 的检测）；没通过的项带故障编号 `code`；每一项另带 `check`（这项检查的编号，通过的项也有，例如时钟一项 `SETUP-CLOCK-01`、端口一项 `SETUP-PORT-01`），按编号找某一项用它 | 不走接口（另读 `GET /v1/setup`） | `doctor [--json]`（`acornfox --json doctor` 一样；`identity` 也认前置的 `--json`，例如 `acornfox --json identity show`、`acornfox identity --json show`，不带子命令时返回 JSON 用法错误，提示 `acornfox identity show --json`） | 只读 |
| 本机服务（v0.4.0 未开放） | Mac、Windows 上的常驻进程（由开机自启调用） | — | `serve` | — |
| 机器身份（下载网关） | 这台机器在 AcornFox 下载网关上的安装编号和公钥；重新生成钥匙。钥匙是随机生成的 ECDSA P-256，不读硬件，放在 `<数据目录>/identity/`。出错编号 `ID-KEY-01`（钥匙读不出）、`ID-KEY-02`（新钥匙存不下） | 不走接口（直接读写数据目录） | `identity show [--json]`、`identity reset [--yes] [--json]`（不带 `--yes` 只试跑） | `show` 只读；`reset --yes` 会改本地文件（旧钥匙留在 `.prev`，可撤销） |
| 时钟自动校正（下载网关） | 网关要求请求时间和网关时间相差不超过 300 秒。收到 `GW-AUTH-01` 且应答的 `Date` 和本机时钟差 60 秒以上时，按网关时间重签一次请求，之后这个客户端一直用校正后的时间；还被拒就报错，提示里写明“这台机器的时钟和网关差了约 N 秒，请校时（timedatectl set-ntp true）”。只改签名用的时间戳，不改系统时间 | 所有用机器钥匙签名的网关请求（W1、W2、W10） | —（`doctor` 的 `SETUP-CLOCK-01` 单独查时钟） | 只读 |
| 应用目录打包签名（v0.5 开发中） | 把目录仓库校验后打包签名成发布目录：`version.txt`、`index.json`、`index.json.sig`（ed25519，base64url）、`apps/<id>/<版本>.tar.gz`；同样输入字节相同；和上次发布比较（输出目录里已有的或 `--prev` 指向的），序号必须比上次大（同序号只允许内容完全相同的重跑），已发布的包不能换内容。应用目录里的文件名要是 ASCII、不能是符号链接（lint `CAT-LINT-13`）；镜像写了 `artifact` 没写 `artifact_sha256` 时 lint 给警告 `CAT-LINT-14`（安装时不走下载网关）。主页、源码地址、视频链接不是 `http://`、`https://` 网址时 lint 报错 `CAT-LINT-15`（控制台也不会把它们显示成链接）。生成签名钥匙（私钥 0600，已存在不覆盖；Windows 上拒绝）。只读校验发布目录。出错编号 `CAT-BUILD-*`、`CAT-KEY-*`、`CAT-VER-*`，含义和处置以 `acornfox/internal/catalog/codes.go` 为准 | 不走接口（给目录维护者用） | `catalog build 目录 --out 输出目录 --serial N --key 私钥 [--prev 上次的发布目录] [--apply]`、`catalog keygen --out 私钥`、`catalog verify 输出目录 [--pubkey 公钥]...`，都支持 `--json` | `build` 默认试跑不写，加 `--apply` 才写输出目录；`keygen` 新建一个文件；`verify` 只读 |
| 应用目录同步（v0.5 开发中） | 客户端按顺序试来源（默认先官网 `https://acornfox.com/catalog`，不通再读 Gitee 备用 `https://gitee.com/VIP13390/acornfox-catalog/raw/published`（同一份签名文件，照样验签）；环境变量 `ACORNFOX_CATALOG_SOURCES`（逗号分隔）可整体替换），先比 `version.txt`，新了才下目录并验签，签名里的序号必须比本地新，通过后写进缓存的新版本目录、再一次性切换指向（写失败时原缓存不受影响），同步全程持有缓存锁，两个同步不会交错（等不到锁报 `CAT-SYNC-09`）；任何一步失败换下一个来源，全失败报 `CAT-SYNC-08`（带每个来源的编号）。读目录时缓存优先，没有或比内置快照旧就用内置快照。安装时按目录里的 sha256 校验应用包（`CAT-PKG-*`）。程序内置的受信公钥还是空的，正式公钥定下来之前只用内置快照 | 服务器启动时同步一次，之后每天随机时刻一次；`POST /v1/catalog/sync` 手动同步；`GET /v1/catalog` 的 `sync` 是上次结果（受信公钥为空时 `skipped: true`、`CAT-SYNC-01`，不联网） | `catalog sync`、`catalog list` | 联系目录来源并写本地缓存 |
| 浏览应用目录（v0.5 开发中） | 目录里有哪些应用和配方、目录序号、来自缓存还是内置快照、上次同步结果；每个应用在这台服务器上装成了哪个应用名、CPU 支不支持；列表里的图标（`icon`、`icon_type`，只在应用包已在本机时有，列表不下载应用包）；应用详情和安装表单要的参数（不含 `hidden` 参数），大模型参数的三种来源现在能不能用 | `GET /v1/catalog`、`GET /v1/catalog/apps/{id}` | `catalog list`、`catalog info 应用`（都支持 `--json`） | 只读 |
| 从目录安装应用（v0.5 开发中） | 一次装好一个单容器应用或一个配方：检查（版本、CPU 架构、同名、正在装、磁盘剩余空间够不够清单 `disk_mb.min` 之和）→ 生成密码和密钥（`crypto/rand`）→ 在整机内存上限内建应用（先按建议内存，不够退到最少内存），记目录 id、版本、来源 → 环境变量（密钥按密钥存）→ 数据卷 → 把配置文件写进数据卷（带权限和属主，宿主机不跑脚本）→ 镜像（本机已有同摘要的直接用；清单写了 `artifact` 和签名的 `artifact_sha256` 时先经下载网关 W2 `purpose=app` 下载，按目录里的校验值和大小核对后才导入，导入的镜像只挂在应用自己的标签上；下载能续传：中途断开、超时（60 秒收不到数据）或地址过期（401、403、404、410）时带 `Range` 从已下的字节接着下，地址过期或快过期时回网关重新要地址，地址还有效时不重新要（省每天的次数），服务器不支持 `Range` 时从头下，连续失败 5 次（总共最多 30 次）才放弃，间隔 2、4、8、16、30 秒，进度不回退，最后整包核对校验值和大小，不对就删掉；没写校验值的不走网关；公共目录的应用网关没成功时按摘要从公共源拉，沿用现有换源，并写明网关没成功的原因；**私有目录的应用清单写了 `artifact` 时只走网关**：网关关掉、连不上、要不到地址、下载重试用完一律停下报 `CAT-INST-26`，不去公共源按 `repository@digest` 拉，免得把私有应用的镜像名发给第三方；私有目录的清单没写 `artifact` 的也停下报 `CAT-INST-26`（本机已有同摘要的镜像除外），提示目录作者补上镜像包；判断地址快过期时按地址还剩多久算：网关给的到期时间先按客户端学到的时钟偏差换成本机时间，换完仍然不合理（已经过期，或剩下超过 20 分钟）就不按到期时间判断，只在下载被拒（401、403、404、410）时重新要地址，免得本机时钟不准时每次重试都去要地址、用掉每天的次数）→ 启动（默认只本机）→ 健康检查 → 初始化调用（提供 `llm_gateway`、适配器 `new-api` 的应用初始化后再用管理员账号关掉自助注册，失败不影响安装，`init` 步骤的说明里写明并提示手动关）→ 首次登录信息加密保存。配方按顺序装，已装的直接复用；`uses: llm_gateway` 的连接由内置 new-api 适配器在本机 New API 里建令牌、填进参数（`base_url` 用容器网络里的别名 `http://new-api:3000/v1`），两个容器在同一个网络里。任一步失败自动清理这次新建的应用、卷、镜像标签和令牌（只删任务记录里这次建的，按卷名删，不按应用标签删；`keep_volumes_on_failure` 保留卷）；同名的旧数据卷还在时拒绝安装（`CAT-INST-24`）；服务重启时进行中的任务记为 `CAT-INST-18`，按任务记录清理。删除从目录装、用了本机 New API 的应用时，顺带吊销给它发的令牌，没吊销成的在返回和 CLI 里给出 `warnings`。编号见下表 | `POST /v1/catalog/install`（202 返回任务）、`GET /v1/catalog/install`、`GET /v1/catalog/install/{task}` | `install 应用 [--name] [--version] [--set k=v]... [--llm own\|local\|platform] [--no-wait] [--show-secrets] [--keep-data-on-failure]`、`install --recipe 配方 …`、`install status 任务`（都支持 `--json`；退出码 0 装好、1 失败、2 参数、3 连接、4 服务器拒绝） | 会改数据；`--llm local` 会在本机 New API 里建一把令牌；`--llm own` 的 Key 要用户提供 |
| 平台变量（v0.5 开发中） | 应用清单不写死时区、下载源、默认模型这类底层的东西，而是引用平台变量（文件模板、初始化请求里写 `{{platform.名字}}`，环境变量写 `from: platform.名字`），装的时候平台按这台机器填进去。表的唯一来源是 `acornfox/internal/catalog/platformvars.go`：`llm.base_url`、`llm.api_key`（这两个只经大模型参数用，平台 Key 开放前单机版没有值）、`llm.model.chat`（默认对话模型，用本机 New API 又没给模型名时也用它）、`tz`（本机时区，读不到为 UTC）、`region`（`cn`/`global`，同执行器拉镜像的判断）、`mirror.pypi`、`mirror.npm`（国内用华为，阿里云、腾讯云上用该厂商的公开镜像；海外用官方源）、`app.url`（装好时按对外方式算）。标了能改的（`llm.model.chat`、`tz`、`mirror.*`）可以在安装时改，只对这次装的应用生效；改法不对报 `CAT-INST-28`。清单引用表里没有的名字、或直接引用只经参数用的，lint 报 `CAT-LINT-16`。应用详情 `platform` 列出这个应用用到的平台变量和这台机器上的值 | `GET /v1/catalog/platform-vars`、`GET /v1/catalog/apps/{id}` 的 `platform`、`POST /v1/catalog/install` 的 `platform`（只放要改的） | `catalog platform-vars`、`catalog info 应用`、`install 应用 --set platform.名字=值`（都支持 `--json`） | 只读；改值随安装请求 |
| 安装进度（v0.5 开发中） | 安装任务每一步的状态和说明；经下载网关下载镜像包时，“下载镜像”这一步每秒更新 `steps[].progress`（`phase` 是 `download` 或下完后的 `load`、`done_bytes`、`total_bytes`、`bytes_per_sec`、`eta_seconds`）和一句话说明（如“从下载网关下载 312 MB / 1.0 GB（29%，25 MB/s，还要约 30 秒）”，断开续传过的后面加“；中途断开 N 次，已接着下”），这一步结束后清掉；网关没成功、改从公共源拉取时也清掉并说明 | `GET /v1/catalog/install/{task}` | `install status 任务`；`install` 等待时每 10 秒打印一次变化 | 只读 |
| 接上旧数据重装（v0.5 开发中） | 删应用时保留了数据（不加 `--volumes`），同名重装默认报 `CAT-INST-24`；加 `--reuse-data` 就接上旧数据卷：这些卷装失败也不会删。从应用中心装的应用删除时，参数（加密）也另存一份（迁移 0012），接上时原样恢复，所以账号密码、会话密钥和以前一样，跳过初始化，本机网关仍能给别的应用发令牌；旧数据是别的应用留下的则拒绝（`CAT-INST-24`）。没有保留的参数时照常初始化，应用回答“已经初始化过”就算接上了，首次登录里初始化写进去的账号密码写“沿用旧数据里的”。连数据一起删（`--volumes`）时保留的参数也删 | `POST /v1/catalog/install` 的 `reuse_data`；任务 `apps[].reused_volumes` | `install 应用 --reuse-data`；`delete 应用 [--volumes]` | 写操作；要用户同意 |
| 首次登录信息（v0.5 开发中） | 从目录装的应用的账号、密码、地址；密码默认遮成 `******`，`reveal=true` 才显示。安装任务结果里的地址是装好那一刻的，控制台显示时换成应用视图里现在的地址（和“打开”同一来源，只本机时仍标“只本机”）。数据库里只存密文（AES-256-GCM，钥匙 `<数据目录>/secrets/catalog.key`，0600），安装任务记录里不存任何密码 | `GET /v1/apps/{app}/first-login[?reveal=true]`（可信入口和要登录的控制台入口） | `install status 任务 --show-secrets` | 只读（会显示密码） |
| 私有应用目录（W10，v0.5 开发中；这一版不开放：控制台入口藏起，命令行总帮助和 `catalog -h` 不列 `catalog connect`、`catalog sources`，Skill 写“暂未开放”；命令本身保留，内部测试用） | 用别人给的连接码（`AFXC-` 加 16 个字母数字，一码一机）把这台服务器连到一个私有目录：服务器用机器钥匙签名，经下载网关 `POST /v1/catalog/connect` 登记，只记目录编号和名称，连接码不落盘、不写日志（本机先按网关同样的宽松规则检查格式，格式不对不发出去）。连过私有目录后，每次同步在公共目录之后再向网关 `POST /v1/catalog/index` 要这台机器能看的私有目录（带手上每个目录的序号 `known`），每个索引用和公共目录同一套受信公钥验签，按目录分别缓存在 `<数据目录>/catalog/private/<目录编号>/`；签名不对、序号回退、格式不对的不用（每个目录各记编号）；网关列为 `revoked` 的目录删掉缓存、标“已断开”，从它装的应用照常运行，但不能再装、不能升级（应用视图 `catalog_disconnected: true`，安装报 `CAT-INST-25`）。没连过私有目录的机器同步时不联系网关。目录列表合并公共和私有，每个应用、配方带 `source`（`public` 或目录编号和名称）；同一个应用两边都有时默认先用公共目录，`--catalog` 指定。私有应用的应用包（文件编号 `catpkg-<应用 id>-<版本>`）和镜像包只经网关 W2（`purpose: app`、`app: {catalog, app}`）取，按签名索引里的 sha256 和大小核对；网关拒绝（`GW-CAT-*`）时报 `CAT-INST-26`，不退回公共源。程序内置受信公钥为空时私有索引也不会被接受（`CAT-SYNC-01`）。索引可带签名覆盖的 `catalog` 字段（`acornfox catalog build --catalog public|cat-…` 写入）：公共同步只收 `public`，私有目录只收自己的编号，写着别的目录时报 `CAT-SYNC-12`；没有这个字段的旧格式两边都收 | `POST /v1/catalog/connect`、`GET /v1/catalog/sources`、`GET /v1/catalog`（`sources`、每项 `source`）、`GET /v1/catalog/apps/{id}?catalog=`、`POST /v1/catalog/install` 的 `catalog`、应用视图的 `catalog_origin`、`catalog_origin_name`、`catalog_disconnected` | `catalog connect 连接码`、`catalog sources`、`catalog info 应用 --catalog …`、`install 应用 --catalog …`（都支持 `--json`；退出码 0 成功、2 参数、3 连接、4 被拒绝） | `connect` 联系下载网关并写本地记录；`sources` 只读 |
| 从下载网关下载运行环境文件（v0.4.0 未开放） | 准备运行环境时，对网关上有的文件（WSL 安装包，Mac 的 Colima、Lima、虚拟机镜像预留编号）先向网关要临时地址（W1 登记、W2 要地址，请求用机器钥匙签名），排在 `--mirror`/自有下载源之后、国内镜像和官方地址之前。网关出任何错（`GW-*`）、连不上、校验值对不上都只算这个来源失败；地址过期（401/403/410）重新要，最多 2 次，`.part` 接着下。界面只显示“AcornFox 下载网关” | 不走 acornfox 接口（下载器内部调用网关的 `/v1/register`、`/v1/issue`） | 跟随 `setup`；网关地址 `ACORNFOX_GATEWAY`（空值关闭）或 `network.json` 的 `gateway`（`off` 关闭），默认 `https://dl.acornfox.com` | 会联系网关并下载文件；发送安装编号、公钥、文件编号、客户端版本和平台 |

## 已知缺口

- v0.4.0 只发 Linux 服务端和 Linux、Mac 命令行：表中标“v0.4.0 未开放”的本机安装、准备运行环境、本机服务、经网关下载运行环境，在 Mac 上返回 `SETUP-MAC-01`（`install`、`serve`、`doctor` 被拦下），Windows 不发程序；`uninstall` 仍可用，给早先在 Mac 上试装过的人清理开机自启；代码保留，用 `-tags acornfox_maclocal` 编译可打开 Mac 本机模式。

- `--json` 输出还没有 `schema_version` 字段；加之前要考虑已发布版本的兼容性。新命令遇到旧服务器时靠 `GET /v1/status` 的 `features` 判断，缺了就报 `server_too_old`（退出码 1）。
- 新控制台已用上版本记录的新字段、指定版本回退和旧版本日志；旧控制台已去掉。首页看板只放有数据来源的图表：网络流量、磁盘占用分类、7 天和热力图、下载源测速、访问量、出错次数、在线率没有接口，不在可选列表里（见 `acornfox/web/console/README.md`）；设置里没有提醒和账号分区。
- 还没有机器可读的命令清单（相当于 `commands --json`），也没有测试检查本清单和路由是否一致。
- 应用中心：后端、CLI、Skill 已有（从目录安装、浏览、同步），新控制台的页面已有；“找专人部署”的按钮前端已留好（`GET /v1/catalog` 带 `expert` 才显示），但服务端和目录格式还没有这个字段，接到哪里也没定，所以现在不显示；程序内置的受信公钥还是空的，所以只用内置目录快照；多容器应用、升级还没做；私有目录（W10）的后端、CLI、Skill 和控制台都有了（控制台按来源 + id 区分同 id 的公共、私有应用，详情和安装带 `catalog`）。删除被别的应用连着的模型网关（例如 Hermes 还连着 New API 时删 New API）会在一开始被拒（`app_in_use`），要先删用它的应用。镜像经下载网关下发要清单里有 `artifact_sha256`，现在内置目录里的 Hermes 还没有，所以只从公共源拉。

## 应用中心安装故障编号

唯一来源 `acornfox/internal/appinstall/faults.go`，服务器的错误和任务的 `fault` 里带 `code`、`message`、`hint`。

| 编号 | 意思 | 怎么办 |
| --- | --- | --- |
| `CAT-INST-01` | 目录里没有这个应用、配方或版本 | `catalog list` 看有哪些；目录太旧就 `catalog sync` |
| `CAT-INST-02` | 参数不对：没有这个参数、平台生成的参数不能填、格式不符、缺必填（含没选大模型） | 按 `message` 改 `--set`；`catalog info 应用` 看参数 |
| `CAT-INST-03` | 已经有同名应用 | `--name` 换名字；要重装先问用户再 `delete` |
| `CAT-INST-04` | 同一个应用正在装（错误里带 `task_id`） | 不要重复提交，查那个任务 |
| `CAT-INST-05` | 整机分配给应用的内存不够 | 问用户：调小别的应用或调大整机上限 |
| `CAT-INST-06` | CPU 架构不支持 | 换机器 |
| `CAT-INST-07` | 访问端口用完了 | 删掉不用的应用 |
| `CAT-INST-08` | AcornFox 版本比应用要求的旧 | 升级 AcornFox |
| `CAT-INST-09` | 平台 Key 即将开放 | 改用 `--llm local` 或 `--llm own` |
| `CAT-INST-10` | 选了本机网关，但本机没有在运行的 New API | 先装 New API，或装配方 `hermes-with-new-api` |
| `CAT-INST-11` | 本机 New API 发不出令牌 | 看 New API 是否在运行；管理员密码改过就改用 `--llm own` |
| `CAT-INST-12` | 镜像从所有来源都没拿到 | 稍后重试；看网络和镜像加速 |
| `CAT-INST-13` | 下载网关给的镜像包和目录里签名的校验值或大小对不上，没有导入、已删掉 | 公共目录的应用自动换公共源，私有目录的应用停下；不要手动绕过 |
| `CAT-INST-14` | 写配置文件失败 | 看 `message`、磁盘和 Docker（`doctor`） |
| `CAT-INST-15` | 没能上线（带部署诊断） | 按部署诊断码处理；`logs 应用` |
| `CAT-INST-16` | 健康检查没通过 | `logs 应用`；内存太小时调大后重装 |
| `CAT-INST-17` | 初始化调用失败（没收到应答的不重发，因为它可能已经生效） | 看 `message`；这次的应用已清理，重新安装 |
| `CAT-INST-18` | 安装途中服务重启，已作废并清理 | 重新安装 |
| `CAT-INST-19` | 目录或应用包读不了、校验不过 | `catalog sync` 后重试；还不行重装 AcornFox |
| `CAT-INST-20` | 内部错误（运行器版本旧、数据库或 Docker 出错） | `doctor`；升级 AcornFox |
| `CAT-INST-21` | 没有这个安装任务 | 检查任务编号 |
| `CAT-INST-22` | 这个应用不是从应用中心装的，没有首次登录信息 | — |
| `CAT-INST-23` | 清单用到这个版本还不支持的写法（多个镜像、自带依赖） | 升级 AcornFox |
| `CAT-INST-24` | 以前同名应用的数据卷还在（删应用时保留了数据），默认不让新装悄悄接上 | 停下问用户：要接着用旧数据就加 `--reuse-data`（接口 `reuse_data`，页面“接上旧数据重装”）；不要就 `--name` 换名字，或由用户删掉旧卷后再装 |
| `CAT-INST-25` | 应用或配方来自已断开的私有目录（授权被撤销或目录停用）；已装好的照常运行，不能再装、不能升级 | 告诉用户联系给连接码的人；拿到新码后 `catalog connect` |
| `CAT-INST-26` | 私有目录的文件（应用包或镜像）没从下载网关取到：网关不给（`gateway_code` 是 `GW-CAT-*`），或这台服务器关了下载网关、连不上网关（`GW-NET-01`）、本机时钟不准（`GW-AUTH-01`）、下载重试用完，或私有目录的清单没写镜像包（`artifact`） | 不去公共源找；`GW-CAT-*` 先 `catalog sync` 看目录是否已断开；`GW-AUTH-01` 先校时（`doctor` 看 `SETUP-CLOCK-01`）；网络问题修好后重装；清单没写镜像包时联系给码的人补上 |
| `CAT-INST-27` | Docker 存镜像的分区（和下载镜像包的数据目录，取剩得少的）剩余空间不够这次要装的应用（清单 `disk_mb.min` 之和）；读不到剩余空间时不拦 | 问用户：删掉不用的应用或镜像，或扩容磁盘；`doctor` 看各分区 |
| `CAT-INST-28` | 改平台变量不对：没有这个变量、这个变量不能改，或值的格式不对 | `catalog platform-vars` 看哪些能改；改 `--set platform.名字=值` |
| `CAT-CONN-01` | 连接码格式不对（本机检查，没发给网关） | 照原样再抄；0、1、8 会当成 O、I、B |
| `CAT-CONN-02` | 这台服务器关掉了下载网关 | 问用户是否打开（`ACORNFOX_GATEWAY`、`network.json` 的 `gateway`） |
| `CAT-CONN-03` | 码不存在、抄错或已被别的机器用过（`GW-CAT-01`） | 核对；找给码的人要新码，不要反复试 |
| `CAT-CONN-04` | 码已过期（`GW-CAT-02`） | 要新码 |
| `CAT-CONN-05` | 私有目录已停用（`GW-CAT-03`） | 联系给码的人 |
| `CAT-CONN-06` | 这台机器这一小时试码次数用完（`GW-CAT-04`，带 `retry_after`；CLI `--json` 在 `diagnosis.retry_after`） | 等够秒数再试，不循环 |
| `CAT-CONN-07` | 连不上网关、网关拒绝（`gateway_code`；CLI `--json` 在 `diagnosis.gateway_code`）或本机钥匙读不出 | `GW-AUTH-01` 先校准时间；`GW-NET-01` 看网络；`identity show` |
| `CAT-CONN-08` | 网关已授权，但服务器没记下来 | 检查数据目录后用同一个码再连（同一台机器重复用不算新的一次） |

同步私有目录的编号在 `acornfox/internal/catalog/codes.go`：`CAT-SYNC-10` 向网关取私有目录失败（手上的不变）、`CAT-SYNC-11` 目录已断开、`CAT-SYNC-12` 签名的索引写明属于别的目录（公共同步遇到时换下一个来源）；单个私有目录的索引问题沿用 `CAT-SYNC-01/03/04/05/06/07`，写在 `catalog sources` 的 `code` 里。
