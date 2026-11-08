# acornfox 命令参考

这份参考按用途列出 `acornfox` 的命令，每条一句话加一个例子，和 `acornfox help` 的内容一致。每个命令的全部参数以命令自己的帮助为准：在命令后面加 `-h`，例如 `acornfox install -h`。

同一个 `acornfox` 程序既是命令行，也是服务器上的 AcornFox 服务。Linux 服务器用安装脚本安装，装好后服务器上就有这个命令；Mac、Linux 电脑可以另装一个命令行，经 SSH 管理服务器。安装方法见 [使用手册](../user-guide.md)。

## 全局参数

全局参数可以写在命令前面或后面；`doctor`、`identity` 例外，它们的 `--json` 要写在命令后面（`acornfox doctor --json`）。

| 参数 | 作用 |
| --- | --- |
| `--target 服务器` | 这次用哪台服务器（默认用 `target use` 设的那台） |
| `--app 应用` | 这次操作哪个应用（默认用当前目录对应的应用） |
| `--json` | 输出 JSON：成功是 `{"ok":true, …}`，失败带 `diagnosis`（`code`、`message`、`hint`），方便 AI 工作台读取 |

退出码：`0` 成功；`1` 操作做完了但失败（例如部署失败、安装失败）；`2` 参数不对；`3` 连不上服务器；`4` 服务器拒绝。

有的命令可以直接写应用名，例如 `acornfox logs web`；其余的用 `--app`，例如 `acornfox status --app web`。下面的例子按各命令现在的写法给出。

## 连接服务器

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `target add 名称 --ssh 用户@主机` | 登记一台服务器并测试连接（`--port` 指定 SSH 端口，`--identity` 指定私钥）。第一次连接前先用 `ssh` 登录一次核对服务器指纹 | `acornfox target add my-server --ssh root@203.0.113.10` |
| `target add 名称 --url 地址` | 在服务器本机登记本机的 AcornFox 服务（Windows 用户经 SSH 在服务器上用） | `acornfox target add local --url unix:///run/acornfox/api.sock` |
| `target list` | 列出登记过的服务器 | `acornfox target list` |
| `target use 名称` | 切换默认服务器 | `acornfox target use my-server` |
| `target remove 名称` | 删除登记（不动服务器） | `acornfox target remove my-server` |
| `open` | 打开网页控制台：经 SSH 转发到你的电脑并打开浏览器，按 Ctrl+C 关闭（`--no-browser` 只打印登录链接） | `acornfox open` |

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

## 日志和状态

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `status` | 应用的状态和访问地址；跟一个部署编号时看那次部署 | `acornfox status --app web` |
| `apps` | 列出这台服务器上的所有应用；只本机能打开的标“只本机” | `acornfox apps` |
| `diagnose [部署编号]` | 最近一次（或指定的）部署为什么失败、怎么办 | `acornfox diagnose` |
| `logs [应用]` | 应用最近的日志（`-f` 持续跟随，`--tail N` 行数） | `acornfox logs web -f` |
| `logs [应用] --version N` | 第 N 版的运行日志，加 `--build` 看构建日志 | `acornfox logs web --version 6 --build` |
| `stats [应用]` | 应用的 CPU、内存等资源使用；不带应用名是整台服务器 | `acornfox stats web` |

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
| `lan-console enable` | 在局域网里打开控制台，要密码登录（`disable` 关闭，`status` 查看，`--reset-password` 换密码） | `acornfox lan-console enable` |
| `public-console enable 域名` | 用一个域名开放控制台的公网访问，带 HTTPS（`disable` 关闭，`status` 查看） | `acornfox public-console enable panel.example.com` |
| `open 应用名` | 在你自己的电脑上打开一个应用：经 SSH 隧道转到本地端口，只本机能打开的应用也能这样开，看完按 Ctrl+C 关闭 | `acornfox open new-api` |

## 应用中心

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `catalog list` | 目录里的应用和配方、目录序号、上次同步结果 | `acornfox catalog list` |
| `catalog info 应用` | 应用详情和安装时能填的参数 | `acornfox catalog info hermes-agent` |
| `catalog sync` | 让服务器现在同步一次目录 | `acornfox catalog sync` |
| `catalog platform-vars` | 平台按这台机器自动填给应用的时区、下载源、默认模型等，以及哪些能改 | `acornfox catalog platform-vars` |
| `install 应用` | 从目录装一个应用，默认只本机能打开（`--llm local\|own` 选大模型来源，`--set 参数=值` 填参数，`--no-wait` 只提交，`--reuse-data` 接上以前同名应用留下的数据） | `acornfox install hermes-agent --llm local` |
| `install --recipe 配方` | 装一个配方里的几个应用并连起来 | `acornfox install --recipe hermes-with-new-api` |
| `install status 任务编号` | 查安装进度和首次登录信息（`--show-secrets` 显示密码） | `acornfox install status 3f9a0c1b2d4e --show-secrets` |

装好的应用默认只本机能打开，在你自己的电脑上用 `acornfox open 应用名` 打开。详细说明见 [使用手册的“应用中心”](../user-guide.md#应用中心)。

## 本机服务和自检

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `doctor` | 检查这台服务器：Docker、后台服务、磁盘空间、端口占用、时钟；每个问题带故障编号（`--json` 输出 JSON）。用 sudo 运行看得更全 | `sudo acornfox doctor` |
| `identity show` | 这台机器在下载网关上的安装编号和公钥（还没生成时说明原因，不会生成） | `sudo acornfox identity show` |
| `identity reset` | 重新生成机器钥匙，安装编号随之改变；不加 `--yes` 只说明会做什么。之后 `sudo systemctl restart acornfox-server` 才生效 | `sudo acornfox identity reset --yes` |
| `version` | 版本号 | `acornfox version` |

## Skill

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `skill install` | 把和命令行同版本的 AcornFox Skill 装到 AI 工作台（Claude Code、Codex；`--dir` 指定其他目录） | `acornfox skill install` |
| `skill print` | 把 Skill 内容打印出来，给其他工具用 | `acornfox skill print > SKILL.md` |

## 目录维护（给维护应用目录的人用）

| 命令 | 做什么 | 例子 |
| --- | --- | --- |
| `catalog lint 目录` | 检查应用目录的清单和配方，只读 | `acornfox catalog lint .` |
| `catalog build`、`catalog keygen`、`catalog verify` | 打包签名发布目录、生成签名钥匙、校验发布目录；各自加 `-h` 看用法 | `acornfox catalog verify dist --pubkey 公钥` |
