# AcornFox 单机版

让 AI 工作台里的一句“帮我部署”变成一个可访问的网址。本目录是单机版的全部代码。

## 当前状态

Linux 服务器（Ubuntu 24.04/22.04/20.04、Debian 13/12/11）用 `scripts/install/install.sh` 安装，server 与 runner 分两个账号运行；macOS 和 Windows 用 `acornfox install` 安装，单进程 `acornfox serve` 在后台运行。安装和使用见 [快速开始](../docs/quick-start.md)，各项能力见 [能力清单](../docs/capabilities.md)。

## 运行时

```text
用户电脑：acornfox CLI + Skill ──SSH──┐
                                     ▼
acornfox server（账号 acornfox）  SQLite、调和器；可信 API 用 Unix socket，控制台仅监听回环地址
        │ Unix socket（internal/peer，按对端 UID 校验）
acornfox runner（账号 acornfox-exec，docker 组）  唯一操作 Docker 的程序，不保存状态
        │
      dockerd                 入口容器 acornfox-edge：适配程序 afx-edge（1 号进程）管上游原版 Caddy；
                              server 只经 internal/edge 接口和它的管理口说话，整份交设置
```

- 状态记在 AcornFox（SQLite），运行交给 Docker，访问交给入口容器（里面是 Caddy）。调和而不是编排：每轮对比期望与实际，只做必要动作；资源名固定（`af-<应用>-<部署ID>`），重复执行即幂等。
- server 或 runner 任一崩溃重启，已部署应用不受影响，未完成的部署自动接着做完。
- 只处理带 `acornfox.managed=1` 标签的 Docker 对象。

## 目录

```text
cmd/acornfox               经 SSH 的 CLI；服务器子命令 server、runner、proxy
cmd/afx-edge               入口容器里的适配程序（单独编译，随入口镜像包发布，不链进 acornfox）
internal/
  state                    SQLite 期望状态：应用、部署、环境变量、数据卷、附加服务、事件、管理员认证表
  runner                   Docker SDK 实现、peer socket 上的 HTTP 服务与客户端
  reconcile                调和器：推进部署、收敛、崩溃恢复、回收
  edge                     入口代理接口（主程序这边只认它）和适配程序的管理协议
  edgeclient               主程序的入口驱动：经适配程序的管理口说话
  edgeadapter              适配程序：拉起并管 Caddy、存设置、容器里的健康检查
  edgeadapter/caddyroute   把入口设置翻译成 Caddy 的设置（只在入口容器里运行）
  catalog                  应用目录：清单格式、检查工具（catalog lint）、平台变量表、值的写法（template.go，全平台唯一的模板引擎）
  appinstall               从应用中心装应用：计划、匹配服务、步骤、首次登录、apps vars
  platformvar              平台变量的唯一取值接口（单机版一份实现；应用地址的算法也在这里）
  apiserver                Unix socket 可信 API；控制台会话、Host 检查与 CSRF
  console                  内嵌原生 JS/CSS 工作台
  cli client pack          跨平台命令、SSH 传输与目录打包
  peer                     Unix socket 边界（按对端 UID 校验）
  auth                     管理员密码与会话（N3 控制台登录复用）
  importers/dockerfile     Dockerfile 安全检查规则（待接入）
  hostmetrics containermetrics dockermetrics   主机与容器指标（N4 接入）
  contracts domain foundation application/contracts compatibility   旧 CLI 与指标仍依赖的类型，N2 后按需精简
```

## 构建与测试

在 Linux 上构建与测试（`internal/runner` 的部分测试依赖 Linux）：

```bash
GOTOOLCHAIN=go1.25.13 go build ./...
GOTOOLCHAIN=go1.25.13 go test -race ./...
ACORNFOX_DOCKER_IT=1 go test -race -run Docker ./internal/runner/   # 需要本机 Docker，且这台机器没装 AcornFox
# 适配程序配真 Caddy（不要 Docker、不要 root，只用回环上的空闲端口和临时目录；闸门不跑）：
go test -c -o edgeadapter.test ./internal/edgeadapter/
ACORNFOX_EDGE_IT=1 ACORNFOX_EDGE_PROXY=/path/to/caddy ./edgeadapter.test -test.run RealProxy -test.v
```

## 已知问题

- 停止应用最多延迟一轮巡检（约 10 秒）才生效，N4 改为立即执行。
- 正式安装时 server 与 runner 分属两个账号，上传目录需对 `acornfox-ipc` 组可读（N5）。
- `internal/containermetrics` 的定时测试在机器繁忙时偶发失败，需要改为不依赖真实时间。
