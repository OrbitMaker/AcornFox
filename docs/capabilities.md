# 单机版能力清单

更新：2026-11-01。

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
| 重新部署、回滚 | 让配置生效；回到上一个成功版本 | `POST /v1/apps/{app}/redeploy`、`/rollback` | `redeploy`、`rollback` | 会改数据 |
| 应用状态 | 应用在不在跑、地址是什么 | `GET /v1/apps`、`GET /v1/apps/{app}`、`GET /v1/status` | `status`、`apps` | 只读 |
| 部署诊断 | 最近一次部署为什么失败、怎么办 | `GET /v1/deployments/{id}`、`GET /v1/apps/{app}/deployments` | `diagnose` | 只读 |
| 启停 | 停止、启动、重启应用 | `POST /v1/apps/{app}/stop`、`/start`、`/restart` | `stop`、`start`、`restart` | 会改数据 |
| 删除应用 | 删除应用，默认保留数据 | `DELETE /v1/apps/{app}` | `delete [--volumes]` | 会删数据（带 `--volumes` 时） |
| 日志 | 应用最近的日志，可以跟随 | `GET /v1/apps/{app}/logs`、`/log-batch` | `logs [-f]` | 只读 |
| 资源使用 | 应用或整台服务器的 CPU、内存、磁盘 | `GET /v1/metrics/host`、`GET /v1/metrics/apps/{app}`、`GET /v1/host` | `stats` | 只读 |
| 资源上限 | 各应用的内存和 CPU 上限，整机最多分给应用多少（2 GB 以下的机器内存允许超分 2 倍，应用默认 256 MB） | `GET/PUT /v1/resources`、`PATCH /v1/apps/{app}` | `limits`、`app set` | 会改数据 |
| 环境变量 | 设置、删除、列出 | `PUT/DELETE /v1/apps/{app}/env/{key}` | `env set/unset/list` | 会改数据 |
| 数据目录 | 给应用加持久化目录 | `POST /v1/apps/{app}/volumes` | `volume add` | 会改数据 |
| 域名与 HTTPS | 绑定域名并自动申请证书 | `GET/POST /v1/apps/{app}/domains`、`DELETE …/{name}` | `domain add/remove/list` | 会改数据 |
| 数据库与缓存 | 加或移除 PostgreSQL、MySQL、Redis | `GET/POST /v1/apps/{app}/addons`、`DELETE …/{kind}` | `add`、`remove`、`addons` | 会改数据；移除会删数据 |
| Skill 安装 | 把 AcornFox Skill 装到 AI 工作台 | 不走接口 | `skill install/print` | 会改本地文件 |
| 暴露方式 | 应用只本机、局域网还是所有网卡加域名能打开（afx_cloud 只占位） | `PATCH /v1/apps/{app}` 的 `exposure`；应用视图的 `exposure`、`lan_url` | `app set --expose local/lan/public/default` | 会改数据 |
| 局域网控制台 | 在局域网打开控制台，要密码登录（默认关闭） | `GET/POST/DELETE /v1/lan-console`（仅可信入口） | `lan-console status/enable/disable` | 会改数据 |
| 本机安装（Mac、Windows） | 在这台电脑上安装、升级、回滚并后台运行 | 不走接口（写开机自启、复制程序） | `install [--rollback]` | 会改本地文件和开机自启 |
| 本机卸载（Mac、Windows） | 停止并移除；默认保留数据 | 不走接口（直接调 Docker） | `uninstall [--purge] [--yes]` | `--purge` 会删数据 |
| 本机自检 | Docker 在不在、后台服务、局域网能不能打开（含 Colima 只绑 127.0.0.1 的检测） | 不走接口 | `doctor` | 只读 |
| 本机服务 | Mac、Windows 上的常驻进程（由开机自启调用） | — | `serve` | — |

## 已知缺口

- `--json` 输出还没有 `schema_version` 字段；加之前要考虑已发布版本的兼容性。
- 还没有机器可读的命令清单（相当于 `commands --json`），也没有测试检查本清单和路由是否一致。
- 应用中心只有占位页：还没有 `catalog update`、`catalog lint`，也没有从应用目录安装的接口。
