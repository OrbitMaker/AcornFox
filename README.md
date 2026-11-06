<h1>
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="website/assets/brand/lockup-horizontal-dark.svg">
    <img src="website/assets/brand/lockup-horizontal.svg" alt="AcornFox" height="48">
  </picture>
</h1>

**一拨即开** —— 让 AI 一句话部署你的应用

AcornFox 是一个开源的应用部署平台，让你在自己的 Linux 服务器上轻松部署和管理应用。

> 支持的平台：
>
> - **Linux 服务器**：正式支持，AcornFox 服务和命令行都装在服务器上。
> - **Mac**：可以装命令行（CLI），当客户端经 SSH 管理服务器。
> - **Windows**：本版本不提供程序，用 SSH 登录服务器，在服务器上运行 `acornfox`（AI 工作台也是这样做）。
> - 把 AcornFox 服务装在 Mac、Windows 电脑本机运行：即将支持。
>
> 详见 [使用手册](docs/user-guide.md)。

[![License](https://img.shields.io/badge/license-AGPL--3.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8.svg)](https://go.dev)

## ✨ 特性

- 🚀 **一键部署** - 本地目录、Git 仓库或 Docker 镜像
- 🤖 **AI 集成** - 通过 AI 工作台自然语言部署
- 🔍 **智能诊断** - 部署失败时返回结构化诊断，AI 自动修复
- 🗄️ **附加服务** - 一键添加 PostgreSQL/MySQL/Redis
- 📊 **监控指标** - 实时查看日志和资源使用
- 🌐 **自动 HTTPS** - 已备案域名自动申请 SSL 证书
- 🇨🇳 **国内优化** - 镜像加速、超时诊断

## 🎯 适用场景

AcornFox 专为以下场景设计：

- **个人开发者**: 在自己的云服务器上部署个人项目
- **小团队**: 快速部署测试环境和演示应用
- **AI 用户**: 通过 AI 助手自然语言部署应用
- **学习实验**: 学习 Docker、容器编排和应用部署

## 🚀 快速开始

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
| 系统 | 推荐 Ubuntu 24.04 或 Debian 13（x86_64，已实测完整流程）。Ubuntu 22.04、Debian 12 安装脚本也接受，但还没完整实测。arm64 还没测过 |
| 配置 | 建议 2 核 2 GB 以上，需要 root 或 sudo |
| 端口 | 18800、18801、18808（AcornFox 自己用，只听本机）和 18810-18899（应用端口）必须空着，被占时安装脚本会停下，不能跳过。80、443 最好空着；已被宝塔、1Panel、Nginx 占用时可以共存，见下面的说明 |

> **买国内云服务器时，系统请选 Ubuntu 24.04（或 Debian 13）。** 阿里云默认的 Alibaba Cloud Linux、腾讯云默认的 OpenCloudOS / TencentOS、火山引擎默认的 veLinux，以及 CentOS、Rocky 等系统装不上 AcornFox，安装脚本会停下并提示“……不能安装 AcornFox”（故障编号 `INSTALL-OS-01`）。Ubuntu、Debian 的版本不在上面列表里（例如 Ubuntu 20.04、Debian 11）时提示“……不在支持列表”（故障编号 `INSTALL-OS-02`），请换成支持的版本。

> **80、443 已被占用（宝塔、1Panel、自己装的 Nginx）**：安装脚本会停下（故障编号 `INSTALL-PORT-02`）。不想停掉原来的网站，就加 `--allow-busy-80-443` 和它们共存：`curl … | sudo bash -s -- --allow-busy-80-443`。共存时应用照常通过 `http://服务器IP:端口` 访问，但不能在 AcornFox 里绑定域名、开 HTTPS 或开公网控制台；要用域名，就在原来的面板里把域名反向代理到 `127.0.0.1:应用端口`。

### 手动安装

#### 1. 安装服务器端

```bash
# 中国大陆服务器（Gitee 镜像）
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install.sh | sudo bash
# 其他地区
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install.sh | sudo bash
```

安装完成后会显示管理员令牌（也保存在 `/root/.acornfox-token`），请妥善保存。

- 服务器在阿里云、腾讯云、火山引擎上时（不论哪个地域），安装脚本先用这家云的内网软件源（系统软件源和 Docker 安装包），不走公网、不耗流量，连不上再换下一个；在华为云和其他国内服务器上用华为等国内公共镜像站。
- 装完后按脚本最后的提示，在云厂商控制台的安全组放行入方向 TCP 18810-18899（应用端口）；要绑域名再放行 80 和 443。

#### 2. 安装客户端 CLI（Linux、Mac 电脑）

```bash
# 中国大陆
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install-cli.sh | bash
# 其他地区
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install-cli.sh | bash
```

Windows 电脑本版本没有命令行程序，第 2～4 步改为用 SSH 在服务器上运行（服务器上已经装好了 `acornfox`），见 [使用手册](docs/user-guide.md#windows-电脑经-ssh-在服务器上操作)。

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

## 🤖 AI 工作台集成

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

## 📖 核心命令

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
```

## 🏗️ 架构设计

AcornFox 采用简洁的三层架构：

- **AcornFox** - 状态管理和协调
- **Docker** - 容器运行时
- **Caddy** - HTTP 路由和 HTTPS

设计原则：
- **状态与执行分离** - server 无 Docker 权限，runner 只接受类型化请求
- **调和而非编排** - 记录"应该是"，让 runner 自动收敛
- **应用独立性** - 应用由 Docker 管理，AcornFox 崩溃不影响运行中的应用
- **诊断即产品** - 失败时返回结构化诊断，AI 可自动修复

## 🌏 中国大陆特别说明

### 域名与备案

- **已备案域名**: 自动申请 HTTPS 证书，通过 80/443 端口访问
- **未备案域名**: 绑定时会收到提示，但可通过 IP:端口正常访问
- **IP 访问**: 不受备案限制，始终可用

### 网络优化

安装脚本读云厂商的元数据，判断服务器在哪家云、是不是中国大陆地域，然后配置：
- 系统软件源（APT）和 Docker 安装包：阿里云、腾讯云、火山引擎的服务器先用所在云的内网源（不论哪个地域，香港、新加坡等境外地域也用），连不上再换下一个
- 中国大陆服务器（包括华为云）接着依次尝试华为云、腾讯云、阿里云、南京大学、清华镜像站；境外服务器用 Docker 官方源
- Docker 镜像加速（只在中国大陆配置）：腾讯云服务器优先用腾讯云内网加速，其余依次用 DaoCloud、1Panel 镜像；GitHub 容器镜像（ghcr.io）用南京大学镜像
- 香港、新加坡、东京等境外地域按海外处理：从 GitHub 下载，不配国内公共镜像站和 Docker 镜像加速
- Caddy 通过国内下载源获取
- 判断不出是哪家云时，按 IP 归属地判断是否在中国大陆

判断错了可以手动指定（通过管道给脚本传参数）：
```bash
# 强制使用中国镜像源
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install.sh | sudo bash -s -- --china

# 强制使用国际源
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install.sh | sudo bash -s -- --global
```

## 📚 文档

- [使用手册](docs/user-guide.md)
- [常见问题](docs/user-guide.md#常见问题)
- [AI 工作台 Skill](skills/acornfox/SKILL.md)
- [能力清单](docs/capabilities.md)

## 🛠️ 升级

```bash
# 服务器端升级（中国大陆）
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/upgrade.sh | sudo bash
# 服务器端升级（其他地区）
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh | sudo bash

# 客户端升级（Linux、Mac 电脑；中国大陆）
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install-cli.sh | bash
# 客户端升级（Linux、Mac 电脑；其他地区）
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install-cli.sh | bash
```

通过管道运行（包括 AI 工作台代为执行）就视为同意升级，脚本开头会先列出要做的事。已经是最新版本时直接退出，不停服务、不重装，重复运行也没关系。只有要重装同一版本时才加 `-y`（`| sudo bash -s -- -y`）：重装也会新建一份备份，之后 `--rollback` 回到的是重装前的同一版本，不再是更早的版本。

升级过程：
- 先下载新版本并校验 SHA256；下载失败不会动现有安装
- 停止 AcornFox 服务，备份程序和数据库（放在 `/var/lib/acornfox/backups/`，保留最近 5 份），再执行数据库迁移
- 数据库迁移失败、或新版本服务启动失败时，自动换回原来的程序和数据库
- 应用容器在升级过程中一直运行；升级期间命令行和控制台会暂时连不上
- 网址入口（Caddy 容器）需要重建时，应用网址会短暂打不开，例如从 v0.3.x 升级（系统里的 Caddy 会被停用，换成 Caddy 容器）

升级完成后发现问题，可以手动回到升级前的版本（程序和数据库一起回退）。详见 [使用手册](docs/user-guide.md#回滚)。

## 🤝 贡献

欢迎贡献代码、文档或提出建议！

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing`)
5. 创建 Pull Request

请阅读 [贡献指南](CONTRIBUTING.md) 了解详情。

## 📜 许可证

本项目采用 AGPL-3.0 许可证 - 详见 [LICENSE](LICENSE) 文件。

## 🙏 致谢

AcornFox 站在巨人的肩膀上：

- [Docker](https://www.docker.com/) - 容器运行时
- [Caddy](https://caddyserver.com/) - HTTP 服务器和自动 HTTPS
- [SQLite](https://www.sqlite.org/) - 嵌入式数据库
- 所有贡献者和用户

## 📮 联系方式

- 问题反馈: [GitHub Issues](https://github.com/OrbitMaker/AcornFox/issues)
- 文档: https://github.com/OrbitMaker/AcornFox/tree/main/docs

---

**让部署变简单，让 AI 来帮忙** 🦊
