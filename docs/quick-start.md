# 快速开始

在一台全新的 Linux 服务器上安装 AcornFox，部署第一个项目，再从应用中心装一个应用。安装在云服务器上通常 3～10 分钟，家用宽带或网络差时可能要 15～40 分钟。

## 推荐：把一句话发给 AI 工作台

```text
请安装 AcornFox Skill：下载 https://acornfox.com/skill/SKILL.md ，保存为你 skills 目录下的 acornfox/SKILL.md（Claude Code 是 ~/.claude/skills，Codex 是 ~/.codex/skills），然后按这个 Skill 把当前项目部署到我的服务器。
```

AI 先安装 Skill，再由 Skill 分别判断你的电脑和服务器在国内还是海外，选择 Gitee 或 GitHub 下载源，完成下文的全部步骤。下文是手动操作的说明。

## 环境要求

### 服务器端

- **操作系统**: Ubuntu 24.04/22.04 或 Debian 13/12（x86_64；arm64 有程序但还没实测）。推荐 Ubuntu 24.04 或 Debian 13；国内云厂商默认的 Alibaba Cloud Linux、OpenCloudOS 等装不上，买服务器时请选 Ubuntu 或 Debian
- **硬件**: 最低 1 核 1GB，推荐 2 核 2GB+
- **网络**: 公网 IP；安全组放行 22（SSH）、18810-18899（应用端口），绑定域名时再放行 80 和 443
- **权限**: root 或 sudo 权限

### 客户端

- **操作系统**: Linux、macOS 12 以上（苹果芯片或 Intel）
- **工具**: SSH 客户端
- **Windows 电脑**: 本版本没有 Windows 版命令行。电脑上不用装任何东西，用 `ssh` 登录服务器后直接在服务器上运行 `acornfox`（服务器安装时已经装好）；AI 工作台在 Windows 上时，也让它经 `ssh` 在服务器上运行

## 安装步骤

### 1. 安装服务器端

SSH 登录你的服务器：

```bash
ssh user@your-server-ip
```

执行安装命令：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install.sh | sudo bash
# 中国大陆服务器改用 Gitee 镜像：
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install.sh | sudo bash
```

**国内服务器**会自动使用镜像加速，无需额外配置。

安装过程在云服务器上通常 3～10 分钟（家用宽带或网络差时可能要 15～40 分钟），完成后会显示（节选）：

```
✓ AcornFox 安装完成！

服务状态:
  • acornfox-server: active
  • acornfox-runner: active
  • Caddy 容器 acornfox-caddy: running

管理员令牌:
acf_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

下一步（看你或 AI 工作台在哪台机器上操作，二选一）:
  …
```

**重要**：复制并保存管理员令牌，后续需要使用。

### 2. 安装客户端 CLI

在你的本地电脑（macOS 或 Linux）执行：

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install-cli.sh | bash
# 中国大陆改用 Gitee 镜像：
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install-cli.sh | bash
```

**Windows 用户**：跳过这一步和下一步，`ssh user@your-server-ip` 登录服务器，在服务器上执行 `acornfox target add local --url unix:///run/acornfox/api.sock` 登记本机，之后的命令都在服务器上运行。

安装完成后验证：

```bash
acornfox version
```

### 3. 配置服务器连接

添加服务器到 AcornFox：

```bash
acornfox target add my-server --ssh user@your-server-ip
```

参数说明：
- `my-server`: 给服务器起个名字
- `user`: 你的 SSH 用户名
- `your-server-ip`: 服务器 IP 地址

**使用 SSH 密钥**：

```bash
acornfox target add my-server --ssh user@your-server-ip --identity ~/.ssh/id_rsa
```

**配置成功**会显示：

```
已添加服务器 my-server，连接测试通过。
已设为默认服务器。
```

## 部署第一个应用

### 方法 1: 部署本地项目

进入你的项目目录：

```bash
cd /path/to/your/project
```

执行部署：

```bash
acornfox deploy
```

如果项目没有 Dockerfile，AcornFox 会提示你创建。参考：[Dockerfile 模板](#dockerfile-模板)

### 方法 2: 部署 Git 仓库

```bash
acornfox deploy --git https://github.com/username/repo
```

支持的 Git 平台：
- GitHub
- Gitee (国内推荐)
- GitLab
- 任意可通过 HTTPS 匿名克隆的仓库

### 方法 3: 部署 Docker 镜像

```bash
acornfox deploy --image nginx:latest --app my-app
```

## 查看部署状态

`acornfox deploy` 会一直等到部署结束（成功或失败）再返回，期间打印每个阶段的进度。加 `--no-wait` 则提交后立即返回。

**部署状态**：

| 状态 | 含义 |
|------|------|
| `queued` | 排队中 |
| `building` | 构建镜像 |
| `starting` | 启动容器 |
| `checking` | 健康检查，通常 10-30 秒 |
| `routing` | 配置访问地址 |
| `live` | 运行中 |
| `failed` | 失败，见诊断 |
| `superseded` | 被更新的提交取代 |

### 成功部署

```
部署成功：http://your-server-ip:端口
```

之后随时可以用 `acornfox status` 查看应用的访问地址和运行状态。

### 部署失败

`deploy` 失败时会直接打印失败原因、建议和日志片段。之后想再看一次：

```bash
acornfox diagnose          # 本机最近一次部署
acornfox diagnose 部署ID    # 指定部署
```

常见错误和修复方法：[使用手册的常见问题](user-guide.md#常见问题)

## 从应用中心装一个应用

应用中心里是预先配好的应用，不用写 Dockerfile。下面装 New API（大模型接口网关）：

```bash
acornfox catalog list        # 看目录里有哪些应用和配方
acornfox install new-api     # 约 1 分钟，装好后显示首次登录的账号，密码默认遮住
```

要显示密码，用输出里的任务编号运行 `acornfox install status 任务编号 --show-secrets`，或在控制台“我的应用”里点开应用查看。

从应用中心装的应用默认只有服务器本机能打开。在你自己的电脑上运行下面的命令打开它（经 SSH 隧道，不用放行安全组），看完按 Ctrl+C 关闭：

```bash
acornfox open new-api
```

也可以在控制台（`acornfox open`）左侧的“应用中心”里点“安装”。Hermes 智能体、配方、局域网访问和令牌额度见 [使用手册的“应用中心”](user-guide.md#应用中心)。

## Dockerfile 模板

### Node.js (Express/Koa/Next.js)

```dockerfile
FROM node:18-alpine
WORKDIR /app

# 安装依赖
COPY package*.json ./
RUN npm ci --only=production

# 复制代码
COPY . .

# 暴露端口
EXPOSE 3000

# 启动
CMD ["node", "index.js"]
```

**注意**: 修改 `EXPOSE` 和 `CMD` 为你的实际端口和启动命令。

### Python (Flask/Django/FastAPI)

```dockerfile
FROM python:3.11-slim
WORKDIR /app

# 安装依赖
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

# 复制代码
COPY . .

# 暴露端口
EXPOSE 8000

# 启动（根据框架选择）
CMD ["python", "app.py"]
# Django: CMD ["python", "manage.py", "runserver", "0.0.0.0:8000"]
# FastAPI: CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
```

### Go

```dockerfile
# 构建阶段
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN go build -o main .

# 运行阶段
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/main ./
EXPOSE 8080
CMD ["./main"]
```

## 下一步

完成首次部署后，你可以：

1. **管理应用生命周期**
   - 停止/启动/重启应用
   - 查看日志和资源使用
   - 应用回退到上一版本（`acornfox rollback 应用名`）

2. **添加数据库**
   - PostgreSQL
   - MySQL
   - Redis

3. **配置域名**
   - 绑定自定义域名
   - 自动申请 HTTPS 证书

4. **集成 AI 工作台**
   - 安装 Skill
   - 通过自然语言部署

5. **从应用中心装应用**
   - New API、Hermes 智能体
   - 见上面“从应用中心装一个应用”

详细文档：
- [命令参考](acornfox/cli.md)
- [使用手册](user-guide.md)
- [AI 工作台 Skill](../skills/acornfox/SKILL.md)

## 常见问题

### 端口没监听

部署失败、诊断显示 `port_not_listening`（健康检查超时，应用没在声明的端口上监听）时，确认应用在容器里监听 `0.0.0.0` 和哪个端口，然后改成实际端口：

```bash
# 查看各应用的地址和端口
acornfox apps

# 改成应用实际监听的容器端口，再重新部署
acornfox app set --app my-app --port 8081
acornfox redeploy --app my-app
```

### 数据持久化

数据库文件等需要持久化的数据：

```bash
# 添加数据卷
acornfox volume add /app/data --app my-app
acornfox redeploy --app my-app
```

### 国内网络问题

如果 Docker Hub 拉取镜像超时：

```bash
# 服务器已自动配置镜像加速
# 可以手动验证配置：
sudo cat /etc/docker/daemon.json
```

### 需要帮助？

- 查看命令帮助: `acornfox --help`
- 常见问题: [使用手册](user-guide.md#常见问题)
- GitHub Issues: https://github.com/OrbitMaker/AcornFox/issues
