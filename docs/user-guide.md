# AcornFox 单机版使用手册（Linux、Mac、Windows）

适用版本：0.4.0 起。

## 先了解三件事

1. **一个程序管全部。** 三个系统用的都是同一个 `acornfox` 程序。它常驻后台，通过 Docker 管理你的应用。
2. **应用都跑在 Docker 里。** 应用的数据放在 Docker 数据卷里，不占你的用户目录。网址入口由 Caddy 负责，Caddy 也是一个容器（名字叫 `acornfox-caddy`），由 acornfox 自动管理。
3. **默认谁能打开。**

| | Linux 服务器 | Mac、Windows 电脑 |
| --- | --- | --- |
| 应用 | 服务器地址 + 端口，可绑域名并自动签发 HTTPS 证书 | 本机 `http://localhost:端口`，局域网 `http://电脑的局域网地址:端口` |
| 控制台 | 只在服务器本机；用 `acornfox open` 经 SSH 打开 | 只在本机；`acornfox open` 直接打开 |
| 局域网打开控制台 | 手动开启，要密码 | 手动开启，要密码 |

每个应用都可以单独改“暴露方式”：

| 暴露方式 | 意思 | 怎么设 |
| --- | --- | --- |
| `local` | 只有这台机器能打开 | `acornfox app set --expose local` |
| `lan` | 这台机器和局域网都能打开（Mac、Windows 的默认值） | `acornfox app set --expose lan` |
| `public` | 所有网卡，可以绑域名（Linux 的默认值） | `acornfox app set --expose public` |
| `default` | 恢复本系统的默认值 | `acornfox app set --expose default` |

还有一种“经 AFX 云”（afx_cloud）的方式，目前只预留了名字，还不能用。

---

## Linux 服务器

支持 Ubuntu 24.04、22.04、20.04，Debian 13、12、11。需要 root。

### 安装

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/releases/download/最新版本/install.sh | sudo bash
```

- 脚本会装好 Docker、acornfox，并做好 Caddy 镜像（`acornfox/caddy:2.8.4`，不需要访问镜像仓库）。
- 装完会显示服务状态，其中包括 `Caddy 容器 acornfox-caddy: running`。
- 云服务器要在安全组放行 TCP 18810-18899（应用端口）；要绑域名的话，再放行 80 和 443。

### 首次打开控制台

在你自己的电脑上：

```bash
acornfox target add my-server --ssh root@服务器地址
```

```bash
acornfox open
```

浏览器会自动打开控制台。控制台不对外开放，是经 SSH 转发到你电脑上的。

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

### 加数据库

```bash
acornfox add postgres
```

`mysql`、`redis` 的用法一样。连接信息会以 `DATABASE_URL`、`REDIS_URL` 环境变量自动交给应用，应用会自动重新部署一次。同一个应用只能有一个 `DATABASE_URL`，也就是 PostgreSQL 和 MySQL 只能二选一。

### 局域网访问

应用默认在所有网卡上可以打开：`http://服务器地址:端口`。只想让服务器本机访问的话：

```bash
acornfox app set --expose local
```

在局域网打开控制台（要密码）：

```bash
acornfox lan-console enable
```

命令会显示地址（`http://服务器地址:18802`）和一次性显示的密码。用完关闭：

```bash
acornfox lan-console disable
```

公网服务器上不建议开局域网控制台；要从公网管理，请用 `acornfox public-console enable 域名`，它带 HTTPS。

### 升级

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/releases/download/最新版本/upgrade.sh | sudo bash -s -- -y
```

- 升级前会自动备份程序和数据库。
- 从 v0.3.x 升级时，原来装在系统里的 Caddy 会被停用，证书复制到数据卷，之后由 Caddy 容器接管 80、443 和应用端口。应用和数据库不会受影响。

### 回滚

```bash
sudo bash upgrade.sh --rollback
```

程序和数据库都回到升级前的版本。如果是回滚到 v0.3.x，脚本会删掉 Caddy 容器，并重新启用系统里的 Caddy。

### 卸载

```bash
sudo bash uninstall.sh
```

默认保留数据（`/var/lib/acornfox` 和数据卷），重新安装后应用会恢复。要全部删除：

```bash
sudo bash uninstall.sh --purge
```

Docker 本身不会被卸载。

---

## Mac

需要 macOS 12 以上，苹果芯片或 Intel 都可以。

### 先准备 Docker

推荐 **Docker Desktop**（https://www.docker.com/products/docker-desktop/ ）。已经在用 OrbStack 或 Colima 的话不用换，acornfox 会自动找到它们。

acornfox 找 Docker 的顺序：

1. 环境变量 `DOCKER_HOST`
2. 当前的 docker context（`docker context use` 选的那个）
3. 依次找 Docker Desktop、OrbStack、Colima 的默认位置

### 安装

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/releases/download/最新版本/install-cli.sh | bash
```

```bash
acornfox install
```

`acornfox install` 会：

- 检查 Docker 有没有在运行，没运行会告诉你怎么打开；
- 判断网络：在中国大陆的，镜像（Caddy、数据库、应用镜像）直接从国内镜像源拉；在海外的从 Docker Hub 拉。判断不出来时按国内处理。也可以自己指定：`acornfox install --china` 或 `acornfox install --global`。选择记在数据目录的 `network.json`，升级时沿用；
- 准备网关组件：下载 Caddy 镜像（约 50 MB），屏幕上显示进度和用时；
- 把程序放到 `~/Library/Application Support/AcornFox/bin/`；
- 注册登录后自动启动（`~/Library/LaunchAgents/com.acornfox.serve.plist`），不需要管理员密码；
- 添加名为 `local` 的服务器并设为默认；
- 最后自检一遍，包括局域网能不能访问。

### 首次打开控制台

```bash
acornfox open
```

控制台地址是 `http://127.0.0.1:18800`，只有这台 Mac 能打开。

### 部署应用

和 Linux 一样：`acornfox deploy`、`acornfox deploy --image ...`、`acornfox deploy --git ...`。部署 Git 仓库需要电脑上有 git（装过 Xcode 命令行工具就有）。

部署成功后会显示两个网址：

- `http://localhost:18810`：本机
- `http://192.168.x.x:18810`：局域网里的手机、其他电脑可以用

苹果芯片上，只有 x86（amd64）版本的镜像也能装。部署时会提示“靠转译运行，可能较慢”。

### 加数据库

和 Linux 一样：`acornfox add postgres` / `mysql` / `redis`。

### 局域网访问

应用默认就能在局域网打开。第一次有设备访问时，macOS 可能弹窗问是否允许 Docker 接受连接，选“允许”。

只想本机打开：`acornfox app set --expose local`。在局域网打开控制台：`acornfox lan-console enable`。

端口是怎么安排的（Mac、Windows 相同）：

- 应用端口固定在 18810～18859，最多 50 个应用。网关容器 `acornfox-caddy` 第一次启动时就把这 50 个端口全部映射好，映射两份：`127.0.0.1:端口` 给本机用，`局域网地址:端口` 给局域网用。
- 新增、删除应用和改暴露方式，只改网关里的路由，不会重启网关，其他应用不受影响。
- “只本机”的应用，网关不接局域网那一份端口，局域网里的设备连不上。
- 电脑的局域网地址变了（比如换了 Wi-Fi）时，网关会自动重建一次，所有应用断开约 1 秒。
- 取舍：这 50 个端口在局域网上一直开着，由网关把关；没分配给应用的端口、只本机的应用都连不上。
- 为什么不按访问者的地址来区分本机和局域网：Docker Desktop 转发进来的连接看不到真实来源地址（实测都显示为 172.17.0.1），只能靠绑定地址区分。

### 升级

下载新版本后再运行一次：

```bash
acornfox install
```

它会停掉旧版本，备份旧程序和数据库，然后换上新版本。

### 回滚

```bash
acornfox install --rollback
```

程序和数据库都恢复到升级前。

### 卸载

```bash
acornfox uninstall
```

这会停掉后台程序、移除登录自启、删除 AcornFox 的容器和网络。数据（数据库、数据卷）会保留，重新安装后应用会恢复。全部删除用：

```bash
acornfox uninstall --purge
```

`--purge` 会删除数据卷，以及数据目录里 AcornFox 的文件。命令行的服务器列表会保留，其他项目也在用的公共镜像（例如 `postgres:16-alpine`）也不会删。Docker Desktop 本身不会被卸载。

### 网关组件（Caddy）

控制台顶部如果显示“正在准备网关组件”，说明 Caddy 镜像还在下载，完成前应用网址暂时打不开。显示“网关组件启动失败，正在重试”时，运行 `acornfox doctor` 看原因。日志在数据目录的 `logs/acornfox.log`，每次失败和重试都有记录。

### 主机资源怎么看

Mac 上 Docker 跑在一个虚拟机里。控制台“服务器”页显示的是这个虚拟机的核数、内存，以及 AcornFox 应用合计用了多少。要调整虚拟机大小，到 Docker Desktop 的设置 → Resources 里改。

---

## Windows

需要 Windows 10 22H2 或 Windows 11，以及 **Docker Desktop**（WSL 2 后端）。

### 安装

1. 安装并打开 Docker Desktop，等左下角显示 Engine running。
2. 下载 `acornfox_windows_amd64.exe`（Gitee 或 GitHub 发行版），改名为 `acornfox.exe`。
3. 在 PowerShell 里进入下载目录，运行：

```powershell
.\acornfox.exe install
```

它会：

- 把程序放到 `%LOCALAPPDATA%\AcornFox\bin\`，并把这个目录加进用户 PATH；新开的终端里可以直接用 `acornfox`；
- 注册“登录时运行”的计划任务 `AcornFox`（当前用户、普通权限、后台无窗口）；
- 添加名为 `local` 的服务器并设为默认。

### 首次打开控制台、部署、加数据库

和 Mac 一样：`acornfox open`、`acornfox deploy`、`acornfox add postgres`。部署 Git 仓库需要先装 Git for Windows。

### 局域网访问

第一次有局域网设备访问时，Windows 防火墙会问是否允许 Docker Desktop（com.docker.backend）通信，勾选“专用网络”并允许。没弹窗而局域网打不开的话，运行 `acornfox doctor` 看提示。

### 升级、回滚、卸载

和 Mac 相同：`acornfox install`（升级）、`acornfox install --rollback`、`acornfox uninstall [--purge]`。

---

## 常见问题

**`acornfox install` 说没找到 Docker。**
按提示安装或打开 Docker Desktop。用 Colima 的话，先运行 `colima start`。

**Colima：局域网打不开应用。**
Colima 默认只把端口转发到 Mac 本机（127.0.0.1），局域网里别的设备连不上。`acornfox doctor` 会提示“只在 127.0.0.1 能连”。处理办法：

- 改用 Docker Desktop 或 OrbStack，它们会把端口开在所有网卡上；或者
- 让 Colima 把端口转发到所有网卡：编辑 `~/.colima/_lima/_config/override.yaml`，写入下面的内容，然后运行 `colima restart`（这个办法还没实测）：

```yaml
portForwards:
  - guestIP: "0.0.0.0"
    guestPortRange: [1, 65535]
    hostIP: "0.0.0.0"
```

**镜像下载很慢或卡住。**
国内网络会直接走国内镜像源（目前是 `docker.m.daocloud.io`，自建的 `cr.acornfox.com` 建好后会换成它）。任何一个源 45 秒没有进展，就换下一个源。想指定镜像源，可以在启动前设置环境变量 `ACORNFOX_REGISTRY_MIRRORS`，多个用逗号分隔。Linux 服务器上，安装脚本选国内时已经给 Docker 配好了国内镜像源。

**Colima：Docker 没起来。**
`acornfox doctor` 显示“Colima 没有启动”时，运行 `colima start`。Mac 重启后 Colima 不会自动启动，可以用 `brew services start colima` 让它开机启动。

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
Caddy 容器用宿主网络，要占用 80、443 和应用端口。机器上已有 Nginx 等网站服务的话，先停掉它，或者不要绑域名。

**提示“连不上本机的 acornfox 服务”。**
后台程序没在运行。运行 `acornfox doctor` 检查；Mac、Windows 上运行 `acornfox install` 会重新启动它。

**想看 Caddy 的日志。**

```bash
docker logs acornfox-caddy
```

**后台程序的日志在哪。**
Linux：`journalctl -u acornfox-server`。Mac、Windows：数据目录里的 `logs/acornfox.log`。

**Caddy 的管理接口安全吗。**
Caddy 的管理接口只开在容器内部。acornfox 通过本机 127.0.0.1:18808 上的一个小网关访问它，网关要求带令牌（令牌存在数据目录的 `caddy-admin.token`，只有 acornfox 能读）。没有令牌的请求一律返回 403。

---

