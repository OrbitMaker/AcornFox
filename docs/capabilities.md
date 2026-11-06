# 单机版能力清单

更新：2026-11-05。

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
| 网页控制台 | 打开控制台 | `POST /v1/console/tokens` | `open` | 只读 |
| 控制台公网访问 | 用一个域名开放控制台（默认不开放） | `/v1/public-console`、`/verify`、`/enable` | `public-console enable/disable/status` | 会改数据 |
| 部署 | 把当前目录、镜像或 Git 仓库部署成应用 | `POST /v1/apps/{app}/deployments` | `deploy` | 会改数据 |
| 重新部署、回退 | 让配置生效（和部署一样默认等到上线或失败）；回到上一个可回退的版本，或指定的第 N 版。回退只换程序，不重新构建，数据库、数据目录和环境变量不变；保留除当前版本外最近 5 个不同的程序。有部署正在进行时要明确取消（`cancel_pending`） | `POST /v1/apps/{app}/redeploy`、`POST /v1/apps/{app}/rollback`、`POST /v1/apps/{app}/versions/{seq}/rollback`（请求体可空或 `{"cancel_pending":true}`，支持 `Idempotency-Key`） | `redeploy [APP] [--no-wait] [--timeout 20m]`、`rollback [APP] [--to N] [--cancel-pending]` | 会改数据 |
| 版本记录 | 每一版的状态、来源、用时、失败原因、能否回退（服务端算好，页面和 CLI 直接显示），以及不指定版本时会回到第几版 | `GET /v1/apps/{app}/deployments?limit=N`（`rollback_target_seq`、`image_check`）；应用视图的 `rollback_target_seq`；`GET /v1/status` 的 `features` | `versions [APP] [--limit N]` | 只读 |
| 应用状态 | 应用在不在跑、地址是什么 | `GET /v1/apps`、`GET /v1/apps/{app}`、`GET /v1/status` | `status`、`apps` | 只读 |
| 部署诊断 | 最近一次部署为什么失败、怎么办 | `GET /v1/deployments/{id}`、`GET /v1/apps/{app}/deployments` | `diagnose` | 只读 |
| 启停 | 停止、启动、重启应用 | `POST /v1/apps/{app}/stop`、`/start`、`/restart` | `stop`、`start`、`restart` | 会改数据 |
| 删除应用 | 删除应用，默认保留数据 | `DELETE /v1/apps/{app}` | `delete [--volumes]` | 会删数据（带 `--volumes` 时） |
| 日志 | 应用最近的日志，可以跟随；也能看某一版的运行日志（容器删除前保存，最多 1 MB）和构建日志（最多 512 KB）。每个应用保留最近 20 次部署的日志，当前版本、能回退的版本和最近一次失败的一直保留；返回前按当前密钥规则再脱敏一遍 | `GET /v1/apps/{app}/logs`、`/log-batch`、`GET /v1/deployments/{id}/logs?kind=run\|build&tail=N` | `logs [APP] [-f]`、`logs [APP] --version N [--build]` | 只读 |
| 资源使用 | 应用或整台服务器的 CPU、内存、磁盘 | `GET /v1/metrics/host`、`GET /v1/metrics/apps/{app}`、`GET /v1/host` | `stats` | 只读 |
| 资源上限 | 各应用的内存和 CPU 上限，整机最多分给应用多少（2 GB 以下的机器内存允许超分 2 倍，应用默认 256 MB） | `GET/PUT /v1/resources`、`PATCH /v1/apps/{app}` | `limits`、`app set` | 会改数据 |
| 环境变量 | 设置、删除、列出 | `PUT/DELETE /v1/apps/{app}/env/{key}` | `env set/unset/list` | 会改数据 |
| 数据目录 | 给应用加持久化目录 | `POST /v1/apps/{app}/volumes` | `volume add` | 会改数据 |
| 域名与 HTTPS | 绑定域名并自动申请证书 | `GET/POST /v1/apps/{app}/domains`、`DELETE …/{name}` | `domain add/remove/list` | 会改数据 |
| 数据库与缓存 | 加或移除 PostgreSQL、MySQL、Redis | `GET/POST /v1/apps/{app}/addons`、`DELETE …/{kind}` | `add`、`remove`、`addons` | 会改数据；移除会删数据 |
| Skill 安装 | 把 AcornFox Skill 装到 AI 工作台 | 不走接口 | `skill install/print` | 会改本地文件 |
| 暴露方式 | 应用只本机、局域网还是所有网卡加域名能打开（afx_cloud 只占位）；Windows 自带运行环境的局域网访问由后台服务转发 | `PATCH /v1/apps/{app}` 的 `exposure`；应用视图的 `exposure`、`lan_url` | `app set --expose local/lan/public/default` | 会改数据 |
| 局域网控制台 | 在局域网打开控制台，要密码登录（默认关闭） | `GET/POST/DELETE /v1/lan-console`（仅可信入口） | `lan-console status/enable/disable` | 会改数据 |
| 本机安装（Mac、Windows）（v0.4.0 未开放） | 在这台电脑上安装主程序、升级、回滚并后台运行；装完接着准备运行环境（不需要先装 Docker） | 不走接口（写开机自启、复制程序） | `install [--rollback] [--no-setup]` | 会改本地文件和开机自启 |
| 准备运行环境（v0.4.0 未开放） | 四步：主程序、运行环境（已有 Docker 直接用，没有就装自带的：Mac 用 Colima，Windows 用 WSL 2 + Docker Engine）、网关组件、自检；断点续做，失败带故障编号；控制台显示同样的页面 | `GET /v1/setup`、`GET /v1/setup/events?since=&wait=`、`POST /v1/setup/start`、`/resume`、`/pause` | `setup [--runtime builtin/docker-desktop/existing] [--mirror 地址] [--confirm-admin \| --local-only] [--resume] [--pause] [--status]`，`--json` 时每行一个事件 | 会下载并安装运行环境；Windows 打开 WSL 2、加局域网防火墙规则要用户本人确认管理员弹窗 |
| 准备运行环境的故障目录 | 每个 `SETUP-*` 编号是什么、可能原因、怎么办 | `GET /v1/setup/faults` | —（`doctor` 输出里带编号） | 只读 |
| 本机卸载（Mac、Windows） | 停止并移除；默认保留数据 | 不走接口（直接调 Docker） | `uninstall [--purge] [--yes]` | `--purge` 会删数据 |
| 本机自检 | 运行环境准备到哪一步、Docker 在不在、后台服务、网关、局域网能不能打开（含 Colima 只绑 127.0.0.1 的检测）；没通过的项带故障编号 | 不走接口（另读 `GET /v1/setup`） | `doctor [--json]` | 只读 |
| 本机服务（v0.4.0 未开放） | Mac、Windows 上的常驻进程（由开机自启调用） | — | `serve` | — |
| 机器身份（下载网关） | 这台机器在 AcornFox 下载网关上的安装编号和公钥；重新生成钥匙。钥匙是随机生成的 ECDSA P-256，不读硬件，放在 `<数据目录>/identity/`。出错编号 `ID-KEY-01`（钥匙读不出）、`ID-KEY-02`（新钥匙存不下） | 不走接口（直接读写数据目录） | `identity show [--json]`、`identity reset [--yes] [--json]`（不带 `--yes` 只试跑） | `show` 只读；`reset --yes` 会改本地文件（旧钥匙留在 `.prev`，可撤销） |
| 从下载网关下载运行环境文件（v0.4.0 未开放） | 准备运行环境时，对网关上有的文件（WSL 安装包，Mac 的 Colima、Lima、虚拟机镜像预留编号）先向网关要临时地址（W1 登记、W2 要地址，请求用机器钥匙签名），排在 `--mirror`/自有下载源之后、国内镜像和官方地址之前。网关出任何错（`GW-*`）、连不上、校验值对不上都只算这个来源失败；地址过期（401/403/410）重新要，最多 2 次，`.part` 接着下。界面只显示“AcornFox 下载网关” | 不走 acornfox 接口（下载器内部调用网关的 `/v1/register`、`/v1/issue`） | 跟随 `setup`；网关地址 `ACORNFOX_GATEWAY`（空值关闭）或 `network.json` 的 `gateway`（`off` 关闭），默认 `https://dl.acornfox.com` | 会联系网关并下载文件；发送安装编号、公钥、文件编号、客户端版本和平台 |

## 已知缺口

- v0.4.0 只发 Linux 服务端和 Linux、Mac 命令行：表中标“v0.4.0 未开放”的本机安装、准备运行环境、本机服务、经网关下载运行环境，在 Mac 上返回 `SETUP-MAC-01`（`install`、`serve`、`doctor` 被拦下），Windows 不发程序；`uninstall` 仍可用，给早先在 Mac 上试装过的人清理开机自启；代码保留，用 `-tags acornfox_maclocal` 编译可打开 Mac 本机模式。

- `--json` 输出还没有 `schema_version` 字段；加之前要考虑已发布版本的兼容性。新命令遇到旧服务器时靠 `GET /v1/status` 的 `features` 判断，缺了就报 `server_too_old`（退出码 1）。
- 控制台还没有用上版本记录的新字段、指定版本回退和旧版本日志（接口已有，页面待做）。
- 还没有机器可读的命令清单（相当于 `commands --json`），也没有测试检查本清单和路由是否一致。
- 应用中心只有占位页：还没有 `catalog update`、`catalog lint`，也没有从应用目录安装的接口。
