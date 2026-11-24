# 安装进行中：查进度、卡住怎么办、安装报错

**什么时候读这一册**：已经按 `install-linux.md` 的 5.1 启动了安装（或用户在自己的终端里运行了安装命令），要查装到哪一步、判断是成功、失败还是卡住；安装脚本报错或停在“故障编号”；用户说“装了很久”“卡住了”。装好（`安装完成` 并且 `exit=0`）以后回到 `install-linux.md` 做 5.5 核对地址和第 6 步自检。

## 最低路径

### 5.2 查进度

在服务器上执行（只读；只显示关键行，不会显示管理员令牌；每次约 50 秒）：

```bash
bash -s <<'EOF'
sleep 30
L=~/acornfox-install.log
grep -E '\[(ERROR|WARN|SUCCESS)\]|故障编号|安装完成|^exit=|^curl:|^sudo:|^bash:' "$L" | tail -n 20
echo "phase=$(grep -F '[INFO]' "$L" | tail -n 1)"
now=$(date +%s); st=$(sed -n 's/^started_at=//p' "$L" | head -n 1)
echo "elapsed_min=$(( (now - ${st:-$now}) / 60 ))"
echo "idle_min=$(( (now - $(stat -c %Y "$L")) / 60 ))"
if kill -0 "$(cat ~/acornfox-install.pid 2>/dev/null)" 2>/dev/null; then echo "running=yes"; else echo "running=no"; fi
rx() { awk 'NR > 2 { sub(/^[ \t]+/, ""); split($0, a, /[: \t]+/); if (a[1] != "lo") s += a[2] } END { printf "%.0f\n", s }' /proc/net/dev; }
a=$(rx); sleep 20; b=$(rx); echo "download_kbps=$(( (b - a) / 20480 ))"
EOF
```

输出的意思：`phase=` 是安装现在走到哪一步（最后一条 `[INFO]` 行）；`elapsed_min=` 是从启动到现在多少分钟；`idle_min=` 是日志多少分钟没有新内容；`running=` 是安装还在不在运行；`download_kbps=` 是这 20 秒服务器的平均下载速度（KB/s）。

从上往下，第一条符合的就是结果：

| 看到 | 下一步 |
| --- | --- |
| `安装完成` **并且** `exit=0` | 成功，回到 `install-linux.md` 做 5.5 和第 6 步。两样必须都有才算成功 |
| `故障编号: INSTALL-…` 或 `故障编号: EDGE-…` | 先执行下面“看故障详情”的命令，再按下面“安装脚本停在故障编号”处理 |
| `[ERROR]` 但没有故障编号 | 按下面“安装时其他报错”处理 |
| `sudo:` 开头的行（例如 `a password is required`、`a terminal is required`） | 后台安装用不了 sudo。改按 `install-linux.md` 第 3 步 `sudo=need-password` 的做法：把 `install-linux.md` 5.4 的安装命令交给用户在自己的终端里运行 |
| 有 `exit=`，但没有 `安装完成`，也没有 `[ERROR]`、`故障编号`、`sudo:`（常伴有 `curl:` 开头的行） | 安装脚本本身没下载下来。执行下面“看最后几行”的命令看原因。先核对用的是不是 `install-linux.md` 第 4 步判断对应的地址（国内 Gitee、海外 GitHub）；用对了还失败，就换另一个地址（Gitee、GitHub 互换）再启动一次。两个都失败，把“看最后几行”的输出交给用户 |
| 没有 `exit=`，且 `running=no` | 安装被中途打断了（例如服务器重启、进程被杀）。执行“看最后几行”，然后重新启动安装（`install-linux.md` 5.1，重跑是安全的） |
| 没有 `exit=`，`running=yes` | 还在装。按下面“各阶段要多久”判断是不是正常，正常就再查一次 |

**各阶段要多久**（`phase=` 里出现的字样 → 在哪个阶段）。总时间：云服务器上通常 3～10 分钟；家用宽带、海外网络差、国内服务器连境外时可能 15～40 分钟，大部分时间花在第 3、5 阶段：

| 阶段 | `phase=` 里出现 | 通常 | 网络差时 |
| --- | --- | --- | --- |
| 1 检查系统和端口 | `检测到操作系统`、`检查端口占用` | 几秒 | 几秒 |
| 2 判断网络 | `检测服务器所在网络`、`中国大陆地域`、`非中国大陆`、`最新版本` | 10～30 秒 | 1 分钟 |
| 3 更新软件源 | `更新软件包列表`、`系统软件源实测`、`配置 APT 使用镜像`、`已停用境外官方源` | 不到 2 分钟 | 10 分钟以上 |
| 3 附：等系统自动更新 | `软件包列表被其他进程…占用，等待释放` | 新开的云服务器常见 | 最多等 10 分钟，脚本会自己继续 |
| 4 装基础工具 | `安装基础依赖` | 不到 1 分钟 | 几分钟 |
| 5 装 Docker | `安装 Docker`、`Docker 安装包下载源实测`、`使用 Docker 源` | 测速最多约 15 秒，下载 1～3 分钟 | 10 分钟以上。**下载这一段日志不会更新**，只能看 `download_kbps=` |
| 6 AcornFox 程序 | `创建系统账号`、`下载 AcornFox` | 不到 1 分钟 | 几分钟 |
| 7 入口镜像 | `下载入口包`、`使用本地入口包`、`导入入口镜像`、`入口镜像已就绪` | 不到 1 分钟 | 几分钟 |
| 8 Docker 镜像加速 | `配置 Docker 镜像加速`（装好程序以后才做：加速地址取自程序） | 十几秒以内（要试一下每个加速地址通不通） | 十几秒 |
| 9 启动服务 | `创建数据目录`、`初始化数据库`、`生成管理员令牌`、`创建 systemd 服务`、`启动 AcornFox 服务`、`入口容器 acornfox-edge 已运行` | 不到 1 分钟 | 最多约 2 分钟（等入口容器最多 60 秒） |

`phase=` 是 `开始下载: 地址` 时，表示正在下载这个文件（地址里有 `acornfox-edge_` 是第 7 阶段，别的 `acornfox` 是第 6 阶段），下完会出现 `下载完成: 大小，用时 N 秒`。

第 6、7 阶段出现 `[WARN] 下载没成功（curl 退出码 …）` 不算失败：脚本会自己换下一个来源，见下文“安装时其他报错”。

`phase=` 里出现 `检测到云厂商`、`中国大陆地域`、`非中国大陆`、`手动指定使用中国镜像源（--china）` 这几句，是脚本对网络的判断，和 `install-linux.md` 第 4 步不一样也不用管。

**怎么算卡住**（还没有 `exit=`、`running=yes` 时）：

| 看到 | 判断 | 下一步 |
| --- | --- | --- |
| `download_kbps=` 不小于 5 | 正在下载，只是慢 | 继续等，每隔 1～2 分钟查一次。超过 10 分钟就告诉用户“正在下载，网络较慢”，并说出现在的阶段和速度。偶尔一次掉到 0 是正常的波动 |
| `phase=` 是“软件包列表被其他进程…占用” | 在等系统自动更新 | 继续等，最多 10 分钟 |
| 连续 3 次（每次间隔 1 分钟以上）都是 `idle_min=` 不小于 10 **并且** `download_kbps=` 小于 5 | **卡住** | 按下面“卡住了怎么办”处理 |
| `elapsed_min=` 不小于 60 | 太久了 | 告诉用户已经装了多久、现在在哪个阶段、下载速度，问用户继续等还是停下重装 |

**卡住了怎么办**：

1. 看是哪个程序在跑（只读）：

```bash
ps -eo pid,etimes,args --sort=start_time | grep -E 'apt|dpkg|curl|docker|gpg' | grep -v grep | tail -n 15
```

2. 把 `phase=`、下载速度和上面的输出告诉用户，说明“安装卡在 X 阶段”，**问用户是否停下重装**。常见原因：服务器连不上外网或 DNS 不通；国内服务器用了境外源（`install-linux.md` 第 4 步判断错了）。
3. 用户同意后停止安装（`install-linux.md` 第 3 步是 `sudo=need-password` 时交给用户运行）：

```bash
bash -s <<'EOF'
if [ "$(id -u)" = 0 ]; then S=""; else S="sudo -n"; fi
p=$(cat ~/acornfox-install.pid)
kids() { local c; for c in $(ps -eo pid=,ppid= | awk -v p="$1" '$2 == p {print $1}'); do kids "$c"; echo "$c"; done; }
all="$(kids "$p") $p"
$S kill -TERM $all 2>/dev/null; sleep 5; $S kill -KILL $all 2>/dev/null
$S dpkg --configure -a >/dev/null 2>&1
echo stopped
ps -eo pid,args | grep -E '[a]pt-get|[d]pkg|[s]udo.* bash' | sed 's/^/left: /'
EOF
```

   `stopped` 后面没有 `left:` 行就是停干净了。还有 `left:` 行时，把这些行交给用户，请他确认后用 `sudo kill -9 PID` 停掉（PID 是每行第一个数字），再执行 `sudo dpkg --configure -a`。

4. 按原因处理（例如第 4 步判断错了就改用另一条命令，或加 `--china`），再从 `install-linux.md` 5.1 重新启动。重跑是安全的，已经装好的部分会跳过。同一阶段卡住两次，停下来，把 `phase=` 和第 1 条的输出交给用户。

### 5.3 看日志的两条命令

看故障详情（脚本停下时会在 `[ERROR]` 和 `故障编号` 之间列出占用端口的程序、PID 和处理办法；这一段在生成令牌之前，不含令牌）：

```bash
sed -n '/\[ERROR\]/,/故障编号/p' ~/acornfox-install.log | tail -n 30
```

看最后几行（只在没有 `安装完成` 时用；会去掉令牌那两行）：

```bash
sed -e '/管理员令牌/{N;d;}' ~/acornfox-install.log | tail -n 20
```

## 安装脚本停在故障编号

安装、升级脚本在改动系统之前先检查，不满足时打印 `故障编号: 编号`（`INSTALL-…`、`EDGE-…`）并停下，什么都没改。照入口“遇到故障编号怎么办”运行 `acornfox faults 编号 --json`，不要凭印象处理。

**第一次安装时服务器上还没有 `acornfox` 命令**，查不了编号：先在这台机器上只装命令行（不需要 sudo，装在用户自己的目录，只读、不改系统；国内用第一条，海外用第二条），再查：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash -s -- cli
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash -s -- cli
```

输出提示安装目录不在 PATH 中时，用输出给出的完整路径调用，例如 `~/.local/bin/acornfox faults INSTALL-PORT-02 --json`。这个命令行这时只用来查编号；装服务本身仍照 `install-linux.md` 第 5 步。

## 安装时其他报错

这些报错发生在安装中途。`[ERROR]` 那几行处理后重跑同一条安装命令；`[WARN]` 开头的按表处理，多数不用重跑。

| 报错包含 | 处理 |
| --- | --- |
| `数据库初始化失败` | 执行 `grep -B3 '数据库初始化失败' ~/acornfox-install.log`，把这几行交给用户。不要删数据库文件 |
| `更新软件包列表失败`、`安装基础依赖失败` | 网络或软件源的问题。等 1 分钟重跑；还失败执行 `sudo apt-get update` 把输出交给用户 |
| `dpkg was interrupted`、`dpkg --configure -a` | 上次安装被中途停下。执行 `sudo dpkg --configure -a`，再重跑 |
| `Docker 安装失败` | 重跑；还失败请用户先自己装好 Docker，再加 `-s -- --skip-docker` 重跑 |
| `无法获取最新版本号`、`下载 AcornFox 失败` | 下载不通。国内服务器检查是不是用了 Gitee 地址；判断错了国内外时加 `-s -- --china`（或 `--global`）重跑 |
| `[WARN] 下载没成功（curl 退出码 …）`、`[WARN] Gitee 镜像下载失败，改从 GitHub 下载`（常见退出码 28，表示超时或太慢） | **正常，不用处理，也不要停下安装**。脚本会接着换下一个来源：中国大陆依次是 Gitee、GitHub 直连、两个 GitHub 加速地址，海外是 GitHub 直连；都慢时最后不限速再试一次。下的是入口包时，之后出现 `入口镜像已就绪` 就算这一步成功；所有来源都失败才会报 `下载入口包 … 失败`（`EDGE-02`） |
| `SHA256 校验失败`、`SHA-512 校验失败` | 重跑一次；还失败**停下**告诉用户（下载的文件可能被篡改），不要跳过校验 |
| `服务启动失败`、`生成管理员令牌失败` | `sudo journalctl -u acornfox-server -n 50 --no-pager`、`sudo journalctl -u acornfox-runner -n 50 --no-pager`，把输出交给用户 |
| `[WARN] 入口容器还没就绪` | 做 `install-linux.md` 第 6 步自检，按结果处理 |
| `[WARN] /etc/docker/daemon.json 已存在，未自动修改` | 服务器上原来就有 Docker 配置，脚本没加镜像加速。平台装应用时自己会实测换源，不受影响；只影响用户自己 `docker pull` 和构建项目。转述给用户；`sudo acornfox doctor` 会提醒里面不能用的地址（`SETUP-NET-03`） |
| `[WARN] 没取到镜像加速地址` | 装好程序后取加速地址时，程序里的每个加速地址都连不上 | 不挡安装。平台装应用时自己会实测换源；装完运行 `acornfox sources --probe` 看现在每个源怎样 |
| `[WARN] 未从云厂商元数据取到公网 IP`、`[WARN] 无法确定服务器地址` | 部署输出的网址会用脚本猜的本机地址（常是内网地址），或者没有主机名。按 `install-linux.md` 5.5 核对，要改就按 `install-linux.md` 里的“改应用访问地址” |

## 可以发挥的地方

- 用大白话向用户解释现在在哪个阶段、为什么这一段没有新输出（下载中）、大概还要多久（照“各阶段要多久”）。
- 判断是网络慢还是真卡住：看 `download_kbps`、`idle_min`、`phase`，不要凭一次读数下结论。

## 必须先问用户的

- 停止卡住的安装再重装：先把 `phase=`、下载速度和 `ps` 的输出告诉用户，**用户同意**才停；`left:` 里还剩的进程要用户确认后才杀。
- `sudo=need-password` 时，安装、停止、`dpkg --configure -a` 都交给用户在自己的终端里运行；提醒用户不要把“管理员令牌”那一行贴给你。
- `SHA256 校验失败` 重跑一次还失败：**停下**告诉用户（下载的文件可能被篡改），不要跳过校验。
- 改网络、代理、Docker 配置之前问用户；`INSTALL-OS-01`、`INSTALL-OS-02`、`INSTALL-PORT-02`、`INSTALL-DOWN-01` 这几个编号的处理，见 `install-linux.md` 的“必须先问用户的”。
