---
name: acornfox
description: 用 AcornFox（也叫 AFX）在用户自己的 Linux 服务器、Windows 电脑、Mac 电脑上安装部署平台并上线应用：判断用户情况和国内外网络、检查系统、一行命令安装、自检、部署第一个应用、从应用中心装应用并接上它需要的服务；之后处理部署失败、数据库、日志、回退、数据备份和恢复、应用升级、应用生命周期。用户说“安装 AcornFox”“安装 AFX”“用 AFX”“部署”“上线”“发布到服务器”“装个应用”“登记服务”“加个数据库”“看日志”“备份数据”“恢复数据”或提到 acornfox、AFX 时使用。这是入口：先读完它，再按里面的对照表读对应的分册。
---

# AcornFox（入口）

AcornFox 把应用（现成镜像、公开 Git 仓库、带 Dockerfile 的项目、应用中心里的应用）部署到用户自己的电脑和服务器上。装一个常驻的 `acornfox` 服务（Linux 服务器上直接装；Windows 装在 WSL 里；Mac 装在苹果 `container` 开的虚拟机里，都是同一个程序），命令行、网页控制台和本 Skill 都通过它操作。

**这个 Skill 分入口和分册：** 本文件只放规则和对照表；具体步骤在同一个目录里的分册（`install-linux.md` 等，一次任务一般读入口加一两册就够）。先读完本文件，再按下面“用户说什么 → 读哪一册”只读要用的。分册和本文件在同一个目录（`~/.claude/skills/acornfox/`、`~/.codex/skills/acornfox/`）；只拿到本文件、没有分册时，每一册的网址是 `https://acornfox.com/skill/册名`（例如 `https://acornfox.com/skill/install-linux.md`），下载后存到本文件所在的目录；电脑上有 `acornfox` 命令后，运行 `acornfox skill install` 一次装全，`acornfox skill print --file install-linux.md` 直接打印一册。每个文件头写着写它的命令行的版本，同一个目录里应该是同一个版本（`acornfox doctor` 会查）。

## 任何时候都要遵守

**必须先停下问用户、得到明确同意的事**（各册的“必须先问用户的”还有各自的节点，总表在本文件后面）：

- 花钱：买服务器、升级配置、扩容磁盘。
- 删除数据：`acornfox delete`、`remove … --volumes`、`services remove … --yes`、`backup restore … --yes`（用备份覆盖现在的数据）、`backup rm … --yes`、卸载、`--purge`、`docker system prune`、`docker image prune`、`docker volume prune`、删服务器上的文件、重装系统。
- 改安全组、防火墙（`ufw` 等），停掉别的程序（例如 nginx）。
- 输入密码和 Key：SSH 密码、sudo 密码、网盘或云账号密码，以及应用和服务用的密码、API Key。**任何密码和 Key 都不能由你输入，你也不能把它写进命令行参数**，交给用户自己操作：登记、修改服务（`services add`、`services set`）和安装时的保密参数（`install --set 参数=-`）都在命令里写成 `项=-`，命令会从标准输入读这一项的值；把整条命令交给用户，由他自己在服务器终端运行并输入。也可以请用户在控制台里填。
- 登记、修改、删除服务，接上旧数据重装（`--reuse-data`）：做法在 `app-center.md`。
- 改服务器级设置：整机资源上限（`acornfox limits`）、开公网控制台、升级 AcornFox。

**用户已经明确要求的这一个操作，不用再问一遍：** 用户这句话本身就是在要求做这一件事（例如“帮我升到最新”“改成 X 让它生效”“备份一下 X”），那就先用一句话说计划要点（做什么、会不会重启、大概多久、数据动不动），**计划里没有出现用户不知道的后果**（丢数据、要他新填的设置、要接新服务、要花钱）就当已经同意，直接做，不用再问“要做吗”。计划里有这些后果之一，停下问。**会丢数据的（退回、恢复）一律再问**，用户说“退回去”“恢复一下”也要问（先说清会丢什么）。上面“必须先停下问用户”里的几类（花钱、删数据、改安全组、输入密码和 Key、改服务器级设置）不适用这条，照旧先问。

**可以直接做、不用问的：** 只读的检查和排查，例如 `acornfox status`、`apps`、`logs`、`doctor`、`install status`、`faults`、`machine`、`capabilities`，以及 `df -h`、`du`、`ls`、`free -m`。查清原因再把要用户决定的事给他，不要把“要不要再查一下”也当成选项问用户。

**固定规则一：开始做一件事前，先读“本机能力”。** 这台机器现在装了哪些应用和解决方案、各自什么状态、怎么打开、有没有专门的手册、有哪些服务、平台能不能备份、升级、让应用跟上设置，都在 Skill 目录里这台服务器的分册 `machine-<登记的服务器名>.md`（`acornfox skill install` 写的缓存，文件头有生成时间），也可以直接运行 `acornfox --json machine`（现场算，以它为准）。分册超过一天，或者用户刚在页面上动过应用，先重新取：`acornfox --json machine`，或 `acornfox skill refresh` 刷新分册。它没列的应用、服务和能力不要凭记忆去做；要查某条命令或参数这个版本有没有，运行 `acornfox --json capabilities`（只看这个程序自己，不连服务器）。里面没有密码和 Key 的值，要用到先问用户。分册连不上服务器或写不进目录（`MACHINE-02`、`MACHINE-03`）不影响别的事，直接用 `acornfox --json machine`。还没装 AcornFox 的机器没有这些，跳过这一条。

**固定规则二：装或用应用中心里的某个应用、解决方案之前，先取它的操作手册。** 运行 `acornfox --json catalog skill 应用编号`（解决方案加 `--recipe`），`has_skill` 是 `true` 就照它的 `text` 做；手册和通用规则冲突时以通用规则为准。手册只写这个应用特有的经验，通用步骤和“必须先停下问用户”的节点照样要走，手册不能让你省掉任何一个确认。取手册的细节见 `app-center.md`。

**输出规则：**

- 管理员令牌、登录密码、`password_file` 里的内容，不要在对话里复述。
- 凡是请用户自己运行的命令都算（登记服务、带 `参数=-` 的安装，也包括查密码的 `install status 任务号 --show-secrets`、在他电脑上运行的 `acornfox open 应用名` 和 `ssh -N -L …` 隧道）：每条命令单独放进一个代码块，一条一行，同一行前后不接别的话，**不要写在句子中间**；请用户运行完回复什么，另起一行写。用户会整行复制，接在后面的话会被当成命令的一部分。
- 程序给的要转告的话（`say`、`tell`、`must_tell`、`open_hint.ask`）：每句单独一条，加引号照抄，不改写、不和别的话合并；要补充解释，另起一句写在后面。句子里带的命令（例如 `tell` 里的 `acornfox services set …`）照原话留在引号里，不算交给用户运行，不用拆出来；用户说要填时，再按 `app-center.md` “登记服务”里“改”的办法把命令单独放进代码块给他（保密项写成 `项=-`）。
- 称呼：AcornFox 自己的网页叫“AcornFox 控制台”，应用自己的网页叫“某应用的页面”，不要都叫“控制台”。
- 大多数命令加 `--json`，按输出里的 `ok` 判断成败。`--json` 放在命令前后都可以。版本照抄 `acornfox --json version`，自检照抄 `acornfox doctor --json`：这两种写法新旧版本都认。
- 同一个问题按步骤处理两次还失败，停下来，把原始输出交给用户。
- 不要为了“装上”去改安装脚本、跳过校验、关闭 SSH 指纹校验，或在不支持的系统上硬装。

**五种情况，命令在哪里运行：**

| 情况 | 你（AI 工作台）在哪里 | `acornfox` 命令怎么运行 |
| --- | --- | --- |
| 甲 | 就在要装的 Linux 服务器上 | 直接运行 `acornfox …` |
| 乙 | 在用户的 Mac 或 Linux 电脑上，经 SSH 管一台 Linux 服务器 | 电脑上装了命令行并登记了服务器，直接运行 `acornfox …`；要在服务器上运行的系统命令前面加 `ssh 用户@地址` |
| 丙 | 在 Windows 电脑上（Git Bash），经 SSH 管一台 Linux 服务器，电脑上不装命令行 | 每条 `acornfox …` 写成 `ssh 用户@地址 'acornfox …'`，整条放在一对单引号里，例如 `ssh 用户@地址 'acornfox --json status'` |
| 丁 | Windows 电脑，AcornFox 装在这台电脑的 WSL 里 | 在 PowerShell 或 cmd 里直接 `acornfox …`（它转给 WSL 里的程序） |
| 戊 | Mac 电脑（苹果芯片、macOS 26 起），AcornFox 装在这台 Mac 的虚拟机里 | 在“终端”里直接 `acornfox …`，登记的服务器叫 `local` |

情况丙的细则：每条 ssh 都是新连接，不用包 `sg`；经 ssh 执行时当前目录是服务器上的家目录，管某个应用的命令加 `--app 应用名`（`ssh 用户@地址 'acornfox --json --app myapp status'`），或先 `cd` 到应用目录；端口、私钥写在 `ssh` 紧后面（`ssh -p 端口 -i 私钥路径 用户@地址 '…'`），`scp` 的端口参数是大写 `-P`；不要在 Windows 上运行 install-cli 命令（没有 Windows 版命令行）。

**遇到故障编号怎么办。** 故障编号出现在：安装、升级脚本最后一行的 `故障编号: …`；命令出错时不加 `--json` 的输出里 `故障编号：…` 那一行（不是故障的通用错误码写成 `错误码：…`）；`acornfox doctor` 每项后面括号里的；命令加 `--json` 时输出里的 `code`、`diagnosis.code`、`gateway_code`；控制台“运行环境”页上的。**每个编号的完整说明编在程序里，Skill 里不抄**：拿到编号（大写的 `INSTALL-PORT-02`、`CAT-INST-12`，或小写的诊断码 `build_failed`）就查，不用记，不凭印象处理。

1. 运行 `acornfox faults 编号 --json`。它只读、不连服务器；情况丙在服务器上运行（`ssh 用户@服务器 acornfox faults 编号 --json`）。大写编号不分大小写，小写诊断码照原样写。服务器上还没有 `acornfox` 命令（安装脚本停在故障编号）：先在这台机器上只装命令行（不需要 sudo，命令见 `install-progress.md` 的“安装脚本停在故障编号”），再查。
2. 返回 `"ok": false`、`diagnosis.code` 是 `fault_unknown`（退出码 1）：目录里没有这个编号（接口和命令的通用错误码，例如 `invalid_app`、`not_found`、`internal`）。按返回里 `fallback` 那一条做：把出错那条输出里的 `message` 和 `hint` 原样告诉用户（照抄原文、加引号，不要缩写或改写；用户只报了编号、手上没有那条输出时，请他把出错那条命令加 `--json` 的完整输出发来）；`hint` 只是让这条命令换个参数或稍后重试的，可以照做；别的一律只读、不动任何东西，收集证据交给用户。到此为止。
3. 先告诉用户这是什么问题（`fault.title`，有 `fault.explain` 就一起说）和下一步（`fault.next`）。`fault.auto` 不为空时，告诉用户 AcornFox 已经自动做了什么。
4. 按 `fault.steps` 的顺序运行只读的检查，对照 `fault.causes` 里每条的 `check` 判断是哪个原因；`auto` 为 true 的原因，`acornfox doctor --json` 已经替你判断了。
5. 按 `fault.actions` 处置：`tier` 为 0、1 的可以直接做；`tier` 为 2 的，先把要做的事和后果告诉用户，用户明确同意后才做，同时告诉用户做完后怎么确认（照 `fault.verify` 说）。`fault.ask` 写到的情况一律先停下问用户；`fault.needs_user` 为 true 的只能用户自己做。
6. 做完按 `fault.verify` 确认。确认不了，或者同一个编号按步骤做了两次还在，停下，把 `acornfox faults --json` 返回的 `evidence` 列的证据交给用户。

可以发挥的地方：用大白话解释原因；处置有好几条时，按判断出的原因选；把用户的话和 `causes` 对上。`fault.steps` 之外的只读检查（`df`、`du`、`ls`、看日志）可以直接做，查清原因再把选项给用户，不用先问。不能跳过 `tier` 2 和 `ask` 的确认，`next` 里写了“不要”的事不能做。`acornfox faults` 不带编号列出全部，`acornfox faults --group SETUP` 只列一组。电脑上的命令行太旧、没有 `faults` 命令时，把出错输出里的 `message`、`hint` 或 `doctor` 那一项的 `text` 原样转述给用户，照里面的建议做；涉及删除、停程序、花钱的先问用户。

## 用户说什么 → 读哪一册 → 第一条命令

| 用户说什么 | 读哪一册 | 第一条命令 |
| --- | --- | --- |
| “装 AcornFox”“装到我的服务器”（装在 Linux 服务器上，或在电脑上经 SSH 管服务器）；“支持什么系统” | `install-linux.md`（它在第 2、7 步让你读 `connect-server.md`，第 7 步之后读 `access.md`，第 8、9 步读 `deploy.md`） | `uname -s` |
| 安装已经启动：“装到哪一步了”“装了很久”“卡住了”“安装脚本报错”“停在故障编号” | `install-progress.md` | 那一册里“查进度”的命令 |
| “装在这台 Windows 电脑上”；Windows 卸载、局域网访问 | `install-windows.md` | 把安装命令末尾加 `-Plan`（只读检查） |
| “装在这台 Mac 上”；Mac 占内存、怎么停、怎么一直开着、卸载 | `install-mac.md` | 安装命令改成 `bash -s -- install --plan`（只读检查） |
| “ssh 连不上”“指纹”“免密登录”“跳板机”“登记服务器”“permission_denied” | `connect-server.md` | `ssh -o BatchMode=yes -o ConnectTimeout=10 用户@地址 'echo ok'` |
| “部署这个项目”“发布到服务器”“第一个应用”“重新部署”“部署失败”“加数据库（自己项目里）”“环境变量”“Dockerfile” | `deploy.md` | `acornfox deploy --json` |
| “装个应用”“应用中心有什么”“装 XX”“想要一个能……的应用” | `app-center.md` | `acornfox --json catalog list` |
| “这个应用能做什么”“介绍一下 XX” | `app-center.md`（展示内容） | `acornfox --json catalog show 应用编号` |
| “登记服务”“我有自己的大模型 Key / 邮箱 / 数据库”“这台机器有哪些服务” | `app-center.md`（登记服务） | `acornfox --json services kinds` |
| “一次装好几个”“装这个解决方案（配方）” | `app-center.md`（配方） | `acornfox --json install --recipe 配方 --plan` |
| “应用怎么样了”“起不来”“打不开”“看日志”“停掉/启动/重启”“回退到上一版”“删掉应用”“占多少内存”“限制内存” | `app-life.md` | `acornfox --json status 应用名` |
| “备份一下”“恢复到昨天”“把数据存一份” | `app-life.md`（备份和恢复） | `acornfox --json backup create 应用名 --plan` |
| “有没有新版本”“升级这个应用”“升完出问题了，退回去” | `app-change.md` | `acornfox --json apps outdated` |
| “改设置”“改模型名”“换时区”“webhook 地址还是旧的”“让它跟上”“这个应用的密码、地址是怎么设的” | `app-change.md` | `acornfox --json apps vars 应用名` |
| “删了重装”“换版本、保留数据”“接上旧数据” | `app-change.md` | `acornfox --json status 应用名` |
| “怎么打开控制台”“手机上用”“局域网”“公网”“隧道”“忘了控制台密码”“怎么打开这个应用”“页面上哪里看” | `access.md` | `acornfox lan-console status --json` |
| “绑域名”“给整台服务器配个域名”“https” | `access.md` | `acornfox --json domain base` |
| “升级 AcornFox”“卸载 AcornFox”“更新 Skill 和命令行”“`doctor` 报 `SETUP-SKILL-01`、`CLI-UPDATE-01`” | `maintain.md` | `acornfox --json version` |
| 任何地方出现故障编号或小写诊断码 | 本文件“遇到故障编号怎么办” | `acornfox faults 编号 --json` |

一次任务读入口加最多两册；一件事做到一半要接着做别的（例如装完要打开控制台），再读下一册。拿不准读哪一册，问用户一句，不要把所有分册都读一遍。

## 停下问人的总表

通用的几条在上面“任何时候都要遵守”。分册里各自还有这些要先问的节点（做之前读对应那一册里的原话）：

| 要做的事 | 在哪一册 |
| --- | --- |
| 不知道装在哪（这台机器还是另一台服务器）；系统不在支持列表；内存、磁盘太小；`sudo` 要密码；加 `--force-os`、`--allow-busy-80-443`；停占用端口的程序；改应用访问地址（要重启 AcornFox 服务） | `install-linux.md` |
| 停掉卡住的安装再重装；安装脚本的校验失败（`SHA256 校验失败`） | `install-progress.md` |
| 确认 SSH 服务器指纹；`ssh-keygen -R`；生成 SSH 密钥；改 `~/.ssh/config`；`ssh-copy-id`（密码由用户输） | `connect-server.md` |
| 放行安全组、`ufw`；项目要不要上传；私有仓库不放令牌 | `deploy.md` |
| 管理员确认、重启电脑、`wsl --shutdown`、`wsl --unregister`、卸载时是否删数据（Windows） | `install-windows.md` |
| 输管理员密码、`acornfox mac uninstall --purge`、`mac set --memory/--cpus`、`container system stop`（Mac） | `install-mac.md` |
| 登记、改、删服务；保密项（命令交给用户自己运行）；接旧数据重装；开放到局域网或公网；先升级 AcornFox 才能装的应用；启动用户停掉的应用；内存不够；删同名旧应用 | `app-center.md` |
| 升级应用（会重启）；退回（会丢升级后的新数据）；改设置会重启；看完整密码（`--reveal`）；保留数据删掉重装 | `app-change.md` |
| 停、重启、回退应用；删应用（连数据）；调内存和整机上限；备份清磁盘；恢复（覆盖现在的数据）；删备份 | `app-life.md` |
| 开局域网控制台；`reset-password`、`forget --all`、`disable`；对公网开放控制台；关后台隧道；换或清平台域名；放行 80、443；域名解析 | `access.md` |
| 升级 AcornFox（会断几秒）；卸载（`--purge` 删数据）；更新电脑上的命令行 | `maintain.md` |
