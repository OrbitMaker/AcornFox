# 部署用户自己的项目（含第一个示例应用）、环境变量、数据库

**什么时候读这一册**：用户要把自己的项目（目录里有 Dockerfile）、公开 Git 仓库或一个现成镜像部署出来；AcornFox 刚装好，要部署第一个应用验证整条链路（`install-linux.md` 的第 8、9 步在这里）；部署失败要排查；要改自己项目的环境变量、端口、数据卷，或给自己的项目加数据库。从应用中心装应用读 `app-center.md`，不是这一册；已经在运行的应用看状态、日志、停启、回退读 `app-life.md`。

## 最低路径

### 第 8 步：部署第一个应用

用户有自己的项目（目录里有 Dockerfile）时，在项目目录里执行 `acornfox deploy --json`；没有 Dockerfile 的先按下面“Dockerfile 要点”写一个。情况丙的项目在 Windows 电脑上，要先传到服务器，见下面“情况丙：部署”。

用户没有指定项目时，部署一个示例网页验证整条链路。在一个新建的空目录里执行（部署后当前目录会生成 `.acornfox` 记住应用名，所以不要在用户的项目目录里跑这条）：

```bash
mkdir -p ~/acornfox-hello && cd ~/acornfox-hello && acornfox deploy --json --no-wait --image nginx:1.27-alpine --port 80 --app hello
```

输出里有 `deployment_id`。之后每隔 5 秒执行一次，直到 `status` 是 `live` 或 `failed`（第一次部署要拉镜像，通常 1～5 分钟）：

```bash
acornfox --json status DEPLOYMENT_ID
```

用 `--no-wait` 是因为不等部署做完就返回，不受 AI 工作台单条命令超时的限制。工作台允许单条命令跑 20 分钟以上时，也可以去掉 `--no-wait` 直接等结果，见下面“等部署结果的写法”。

情况甲且 `connect-server.md` 第 7 步是 `group=added` 时，`acornfox deploy …` 那一段按那一步定下的写法来：包进 `sg acornfox-users -c "…"`，或者写成 `sudo -n -u "$(id -un)" acornfox deploy …`。

**情况丙：部署**（每条都在 Windows 的 Git Bash 里执行，命令都经 ssh 在服务器上运行）。

示例网页：

```bash
ssh 用户@地址 'mkdir -p ~/acornfox-hello && cd ~/acornfox-hello && acornfox deploy --json --no-wait --image nginx:1.27-alpine --port 80 --app hello'
```

```bash
ssh 用户@地址 'acornfox --json status DEPLOYMENT_ID'
```

第二条每隔 5 秒执行一次，直到 `status` 是 `live` 或 `failed`（和上面一样）。`live` 之后取网址：这一条输出里没有 `url`，按应用名查应用状态，用输出里的 `url`：

```bash
ssh 用户@地址 'acornfox --json --app hello status'
```

现成镜像、公开 Git 仓库不用传文件，在服务器上建一个目录直接部署，例如 `ssh 用户@地址 'mkdir -p ~/acornfox-apps/myapp && cd ~/acornfox-apps/myapp && acornfox deploy --json --no-wait --git 仓库地址 --app myapp'`。

（情况丁不用传：项目就在这台电脑上，在文件夹里直接 `acornfox deploy`。）用户电脑上文件夹里的项目：先把文件夹传到服务器，再在服务器上那个目录里部署。`项目文件夹` 写 Git Bash 里的路径，例如 Windows 的 `C:\Users\me\myapp` 写成 `/c/Users/me/myapp`；传上去后服务器上的目录名和文件夹名相同（下面的 `myapp`）。

```bash
ssh 用户@地址 'mkdir -p ~/acornfox-apps'
scp -r 项目文件夹 用户@地址:acornfox-apps/
ssh 用户@地址 'cd ~/acornfox-apps/myapp && acornfox deploy --json --no-wait'
```

之后同上：`ssh 用户@地址 'acornfox --json status DEPLOYMENT_ID'` 查到 `live`，再用 `ssh 用户@地址 'acornfox --json --app myapp status'` 取 `url`。

- 容器监听的端口不是 Dockerfile 里 `EXPOSE` 的那个，或者没写 `EXPOSE` 时，在 `deploy` 后面加 `--port 端口`。端口怎么定：nginx、httpd 镜像是 80；Node 看代码里 `listen(` 的端口；Python 看启动命令里的 `--port`；判断不了就问用户，不要猜。
- 文件夹很大（有 `node_modules`、`.git`、构建产物）时，`scp` 会很慢，改用这一条代替上面的 `scp`（`--exclude` 按项目增减）：

```bash
tar -czf - --exclude=node_modules --exclude=.git -C 项目文件夹 . | ssh 用户@地址 'mkdir -p ~/acornfox-apps/myapp && tar -xzf - -C ~/acornfox-apps/myapp'
```

- 改了代码要重新部署：再执行一次传文件的命令和 `deploy`。电脑上删掉的文件，服务器上那份不会跟着删；影响构建时告诉用户。
- 项目在私有 Git 仓库里：不要把用户的 Git 密码或令牌放到服务器上，改用上面传文件夹的做法。

- 成功：`status` 是 `live`。网址用 `acornfox --json status` 输出的 `url`，记下来。
- 失败：`status` 是 `failed`，按输出里的 `diagnosis.code` 照下面“部署失败时”处理。

验证能打开：

- 情况乙、丙：在用户电脑上执行 `curl -s -o /dev/null -w '%{http_code}\n' -m 10 网址`。`200` 表示外网能打开；`000` 表示被安全组或防火墙挡住了，去第 9 步。
- 情况甲：在服务器上执行 `curl -s -o /dev/null -w '%{http_code}\n' -m 5 http://127.0.0.1:端口`（端口是 `url` 末尾的数字）。`200` 表示服务正常；外网能不能打开要用户在浏览器里试，打不开就是第 9 步的问题。

### 第 9 步：提醒放行端口

局域网服务器（`access.md` 的“第 7 步之后：打开控制台”里局域网控制台是开着的）：应用默认局域网里就能打开，没有安全组，这一步跳过；服务器自带防火墙开着时（`install-linux.md` 第 3 步看到 `ufw=Status: active`）照下面最后一段放行。其他服务器告诉用户（原样转述即可）：

> 应用网址是 `http://服务器地址:端口`，端口从 18810 开始分配（18810–18899）。请到云厂商控制台，在这台服务器的**安全组**里添加入方向规则：TCP 18810-18899，来源 0.0.0.0/0。以后要绑定域名、开 HTTPS，还要放行 TCP 80 和 443。

`install-linux.md` 第 3 步看到 `ufw=Status: active` 时还要说：服务器自带防火墙也开着，需要放行同样的端口；用户同意后执行 `sudo ufw allow 18810:18899/tcp`（要绑域名再加 `sudo ufw allow 80/tcp`、`sudo ufw allow 443/tcp`）。

**安全组、防火墙由用户决定。** 即使你能调用云厂商的命令行，也要先问用户。

## 之后的部署和重新部署

情况丙：本册的 `acornfox` 命令都按入口“五种情况，命令在哪里运行”里情况丙的写法经 ssh 执行，要部署的项目先按上面第 8 步“情况丙：部署”传到服务器。

```bash
acornfox deploy --json                      # 部署当前目录（需要根目录有 Dockerfile）
acornfox deploy --json --port 3000          # 指定容器监听端口
acornfox deploy --json --git URL [--ref 分支或提交]
acornfox deploy --json --image nginx:1.27-alpine --port 80 --app my-app
```

`deploy` 默认等到终态才返回（最长 20 分钟，可用 `--timeout 30m` 调整）：

- 成功：`{"ok":true,"url":"http://地址:端口",...}`，把 `url` 告诉用户。
- 失败：`{"ok":false,"diagnosis":{"stage","code","message","log_excerpt","hint"}}`，照下面“部署失败时”处理后重新部署。

### 等部署结果的写法

`deploy` 要构建或拉镜像，常常要几分钟，最长等 20 分钟。按下面的顺序选一种写法：

1. **首选：先提交、不等，再自己查进度**（不受工作台单条命令超时的限制）。

   ```bash
   acornfox deploy --json --no-wait …          # 立刻返回，记下输出里的 deployment_id
   acornfox --json status DEPLOYMENT_ID        # 每隔 5 秒查一次，直到 status 是 live 或 failed
   ```

   `status` 为 `live`：成功，网址用 `acornfox --json status` 的 `url`。为 `failed`：按输出里的 `diagnosis.code` 照下面“部署失败时”处理。为 `superseded`：被更新的部署取代了，用 `acornfox --json status` 看现在的版本。其他值（`queued`、`building`、`starting`、`checking`、`routing`）表示还在进行，继续等。

2. **工作台允许单条命令跑 20 分钟以上时**，可以前台运行、等它自己返回：

   ```bash
   acornfox deploy --json …; echo "exit=$?"
   ```

   `exit=0` 且输出 `"ok":true` 才算成功。

3. **必须放后台时**，只用这一种写法：

   ```bash
   nohup acornfox deploy --json … > out.json 2> err.log < /dev/null &
   wait $!; echo "exit=$?"
   ```

   启动后立刻得到的 `$?` 只表示“已经放到后台”，**不是部署结果**。要等进程结束再读 `out.json`：同一个 shell 里用 `wait $!`；换了一条命令再查时，等 `out.json` 里出现以 `{` 开头的一行（`grep -q '^{' out.json && cat out.json`）才算结束，没出现就过几秒再查。

- **不要**用 `setsid -f acornfox deploy …`、`sg acornfox-users -c "acornfox deploy … &"` 这类立刻返回的包法：命令马上结束，拿不到结果。要用 `sg` 时，每条命令各包一次：`sg acornfox-users -c "cd ~/acornfox-hello && acornfox deploy --json --no-wait …"`，再 `sg acornfox-users -c "acornfox --json status DEPLOYMENT_ID"`。
- 拿到 `"diagnosis":{"code":"cancelled",…}`：只是**等待**被打断了（例如命令超时被工作台结束），部署还在服务器上继续。按 `hint` 运行 `acornfox --json status DEPLOYMENT_ID` 查结果，**不要重新部署**。`code` 为 `timeout` 时同样处理。

用户说“重新部署”“再部署一次”时：本地代码或 Dockerfile 有改动就用 `deploy` 上传新代码；没有改动、只是想让环境变量或设置生效，才用 `redeploy`（它只重建当前版本，不上传本地代码）。拿不准先问用户。`redeploy` 和 `deploy` 一样默认等到终态；`--no-wait` 只返回 `deployment_id`，之后用 `acornfox --json status DEPLOYMENT_ID` 查进度。

首次部署后当前目录会生成 `.acornfox`（只记服务器名和应用名，不含凭据），之后在这个目录里的命令不用再指定 `--app`。

## 部署失败时

`status` 是 `failed`、或 `deploy --json` 返回 `"ok":false` 时，`diagnosis.code`（`dockerfile_missing`、`build_failed`、`pull_failed`、`port_not_listening`、`out_of_memory` 这类小写诊断码）照入口“遇到故障编号怎么办”运行 `acornfox faults 诊断码 --json`，按它的 `steps`、`actions` 做。几件要记住的：

- `cancelled`、`timeout` 只是**等待**被打断或超时，部署还在服务器上继续：按 `hint` 运行 `acornfox --json status DEPLOYMENT_ID` 查结果，**不要重新部署**（见上面“等部署结果的写法”）。
- `registry_timeout`（构建时下载依赖超时）：服务器在国内时，Dockerfile 里改用国内镜像源（见下面“Dockerfile 要点”）后重新部署。
- `out_of_memory`：`acornfox app set --memory 1024 --json` 立即生效不用重新部署（部署时就内存不够，改完再重新部署）；整机内存分配不够（`memory_cap_exceeded`）要调整别的应用或整机上限，调整整机上限先问用户，见 `app-life.md`。
- `pull_failed`、`pull_timeout`、`image_not_found`：`message` 里逐个写了每个镜像源怎么了；`image_not_found` 先核对镜像名和标签；写着“太慢”“没有进展”“总时限”的，告诉用户网络慢、换个时间再试；`acornfox --json sources --probe` 看现在每个源多快。

`log_excerpt` 只有最后 40 行。不够判断原因时，用 `acornfox logs --json --app APP --version N --build` 看那一版完整的构建日志，或 `acornfox logs --json --app APP --version N` 看它的运行日志（默认最后 500 行，`--tail` 最多 2000；正在服务的版本最多 1000 行）。N 是失败的版本号，在 `diagnose` 或 `versions` 里。

部署成功、`url` 在服务器上能打开（`curl` 返回 200）但用户的浏览器打不开：几乎都是安全组没放行，按第 9 步提醒用户。

## 配置

```bash
acornfox env set KEY=VALUE [--secret] --json   # 密钥值永不回显
acornfox env unset KEY --json
acornfox env list --json
acornfox volume add /app/data --json           # 绝对路径，容器重建后数据保留
acornfox volume list --json
acornfox app set --port 3000 --memory 512 --health-path /healthz --json
```

改环境变量、数据卷、端口或健康检查路径后执行 `acornfox redeploy --json` 生效。

## 数据库与缓存

```bash
acornfox add postgres --json   # 也可以是 mysql、redis
acornfox addons --json
acornfox remove postgres --json [--volumes]
```

添加后会自动重新部署并注入连接变量：postgres、mysql 为 `DATABASE_URL`，redis 为 `REDIS_URL`。代码里从环境变量读取连接串，不要写死。`remove … --volumes` 会删数据，先问用户。这三条命令只给用户自己部署的项目用；从应用中心装的应用要数据库，按 `app-center.md` 先看计划，**不要**对它用 `acornfox add`。

## Dockerfile 要点

- 服务器在国内时，安装依赖一开始就用国内镜像源，否则 npm / pip / Go 下载常会超时：npm 加 `--registry=https://registry.npmmirror.com`，pip 加 `-i https://mirrors.aliyun.com/pypi/simple`，Go 设 `ENV GOPROXY=https://goproxy.cn,direct`。
- 程序监听 `0.0.0.0:$PORT`，`EXPOSE` 与 `--port` 一致。
- 用多阶段构建和 `.dockerignore` 控制体积（上传上限 200 MB）。
- 需要持久化的数据写到固定目录，再用 `volume add` 挂载。

## 可以发挥的地方

- 帮用户给项目写 Dockerfile、`.dockerignore`，选基础镜像和启动命令。
- 读构建日志、运行日志，判断是代码问题还是配置问题，改用户的代码。
- 帮用户规划数据库、数据目录、内存分配。
- 用大白话向用户解释报错，补充表格里没写到的背景。

## 必须先问用户的

- 放行安全组、防火墙（`ufw`）：即使你能调用云厂商的命令行，也要先问。
- 删除应用或数据（`delete`、`remove … --volumes`）、改整机资源上限（`acornfox limits`）。
- 项目在私有 Git 仓库里：不要把用户的 Git 密码或令牌放到服务器上，改用传文件夹的做法。
- 往应用里放密钥：用 `env set --secret`，不要在命令行或代码里输出密钥值，也不要让用户把密钥发到对话里。
- 国内服务器的域名需要备案；未备案时用 `IP:端口` 访问（绑域名见 `access.md`）。
