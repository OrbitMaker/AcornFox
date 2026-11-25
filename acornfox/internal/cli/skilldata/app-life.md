# 已装应用的日常：看状态和日志、停启、回退、备份和恢复、内存

**什么时候读这一册**：用户问“应用怎么样了”“起不来”“打不开”“看日志”“我自己装的容器能看到吗”；要停、启动、重启、回退到上一版、删掉应用；问“占多少内存”或要限制内存和 CPU；要备份、恢复数据、升级前留一份；对用户说“好了、能打开了”之前要确认应用真的能打开。装应用读 `app-center.md`，升级和改设置读 `app-change.md`。

## 状态与诊断

```bash
acornfox status --json                 # 应用状态：url、observed_state、current_deployment
acornfox --app 应用名 status --json    # 指定应用（v0.5 起也可写 status 应用名；写 12 位部署编号时看那一次部署的进度）
acornfox apps --json                   # 这台服务器上的所有应用
acornfox diagnose --json               # 本机最近一次部署的状态、事件和诊断
acornfox diagnose DEPLOYMENT_ID --json # 指定部署
acornfox logs --json [--tail 200]      # 容器日志
acornfox versions --json --app APP                    # 版本记录：来源、用时、失败原因、能否回退
acornfox logs --json --app APP --version N [--build]  # 某一版的运行日志或构建日志
```

从应用中心装的应用，`status` 里还有它用的服务（v0.6.0 起）：人读输出“它用的服务”下每个需要一行（需要的名字、服务类型、必须还是可选、接的是哪条服务、那条服务现在的状态）；`--json` 里是 `services`，每条有 `use`、`kind`、`kind_label`、`required`、`service`（没接上是空）、`status`、`status_label`。`status` 不是 `ok` 的，照入口规则运行 `acornfox faults CAT-SVC-08` 处理（提供它的应用停着：先问用户是不是有意停的，不是就 `acornfox start 那个应用`）。

### 确认应用能打开（只读，不用问用户）

装好、升级、退回、恢复、改设置之后，对用户说“好了、能打开了”之前，照这三步确认，**不要只凭命令返回成功就说好**：

1. `acornfox --json status 应用名`：`observed_state` 要是 `running`，`current_deployment` 要有值（一个 12 位的部署编号）。`observed_state` 是 `unavailable` 说明这台机器读不了容器状态；是 `stopped`、`missing`，或 `current_deployment` 是空的，都是还没好：先看 `acornfox --json diagnose` 和 `acornfox logs 应用名`，再回到对应一节处理。
2. 从服务器自己探一下它的地址（只读）：用 `status` 里的 `url`（`local_only` 为 `true` 的就是 `127.0.0.1` 那个地址）。情况甲、丁直接运行，情况乙、丙经 `ssh 用户@地址 '…'` 运行：

   ```bash
   curl -s -o /dev/null -m 5 -w '%{http_code}' 地址
   ```

   返回 200～399，或 401、403（它在应答，只是要登录）：能打开。`000`（连不上）或 5xx：还没好，先等十几秒再探一次，仍不行看 `acornfox logs 应用名`；刚升级、恢复的应用启动要几十秒，别一次不通就下结论。
3. `internal_only` 为 `true` 的应用（数据库、缓存）没有地址，只看第 1 步。

这一步探的是服务器自己；用户电脑浏览器能不能打开（防火墙、安全组、只本机的要用 `acornfox open`）另算，告诉用户自己点开试一下，不要替他判断。

从应用中心装的应用，如果有它的操作手册（见 `app-center.md` 的“从应用中心装应用”开头），手册的“装好以后怎么用起来”里有一小段“怎么确认真的能用”（应用特有的检查，例如某个页面能打开、某条命令有返回）：上面三步做完后再照它做一遍，都通过了才对用户说“好了”；手册里的检查和这三步冲突时，以这三步为准。

`versions` 的每一版有 `status_text`、`source_text`、`duration_seconds`、`failure_text`、`can_rollback`、`rollback_text`，直接转述给用户，不要自己换算；顶层 `rollback_target` 是不指定版本时会回到的版本：除正在服务的版本外，最新一个还能一键回退的版本，不一定是“当前版本减一”；为 0 表示没有可回退的，告诉用户只能重新部署代码。部署失败时线上仍是原来那一版（`status` 为 `live` 的那条）。

部署状态：`queued` → `building` → `starting` → `checking` → `routing` → `live`；失败为 `failed`，被新提交取代为 `superseded`。

## 内存与 CPU 上限

```bash
acornfox app set --memory 768 --cpu 1.5 --json   # 内存至少 256 MB，CPU 至少 0.5 核；立即生效，应用不重启
acornfox limits --json                           # 整机最多分配给应用多少、各应用和数据库各占多少
acornfox limits --memory 26G --cpu 8 --json      # 改整机上限；auto 恢复自动（改之前先问用户）
```

- 所有应用和数据库的内存上限加起来不能超过整机上限；新建应用（默认 256 MB）、加数据库、调大内存时都会检查。整机不到 2 GB 时，上限是整机内存的 2 倍（允许超分）。
- CPU 只要求单个应用的上限不超过整机上限，总和可以超过。

## 生命周期（停、启、重启、回退、删除）

```bash
acornfox --app 应用名 stop --json      # 等到容器真的停下才返回
acornfox --app 应用名 start --json     # 等到观察状态是 running 才返回
acornfox --app 应用名 restart --json   # 同上
acornfox rollback --json --app APP [--to N]   # 回到上一个可回退的版本，或第 N 版（只换程序，不动数据）
acornfox stats APP --json       # 该应用的 CPU、内存、网络（不带应用名则是整台服务器）
acornfox stats APP --history 1h --json   # v0.5.1 起：该应用近 1 小时的 CPU、内存（24h 是近 24 小时）；只读。历史只存在内存里，服务重启后从头记，返回的 ranges 为空说明服务端版本旧、没有历史
acornfox domain add example.com --json
acornfox domain list --json
```

`start`、`restart` 返回 `"ok":true` 并且带 `"running":true` 时，应用已经在运行，不用再用 `apps` 确认。输出里没有 `running` 字段（v0.4.x 不等就返回）时，隔几秒用 `acornfox --json apps` 确认状态是 `running`。失败时看 `diagnosis.code`：`start_timeout` 是等了 60 秒还没起来，按 `hint` 运行 `acornfox logs 应用名` 找原因；`no_deployment` 是应用还没有上线过的版本，先部署；`cancelled` 是等待被打断，请求已经提交，用 `status` 看状态，不用再提交一次。不写 `--app` 时管的是当前目录对应的应用。v0.5 起应用名也可以直接写在命令后面（`acornfox stop 应用名 --json`），旧版本报“不接受参数”时改用 `--app`。

`acornfox --json delete 应用名 [--volumes]` 和 `remove … --volumes` 会删除应用或数据，执行前必须得到用户明确同意。一定写上应用名（不写时删的是当前目录对应的应用）；不加 `--volumes` 时数据卷保留，以后同名重装会被 `CAT-INST-24` 拦下，用户要接着用旧数据时加 `--reuse-data`。接着数据库应用提供的服务（`source` 是 `app`）的应用，加 `--volumes` 时提供方里给它开的库也一起删，不加时库和账号都留着；接的是用户自己登记的数据库时，那边的库不会被动（见 `app-center.md` 的“必须先问用户的”里“连数据一起删掉应用”那一条）。

删除被拒、`diagnosis.code` 是 `app_in_use`：别的应用接着它提供的服务（例如数据库应用还有应用在用它；`message` 写着是哪些应用、哪条服务）。不要硬删，也不要自己先删那些应用；把 `message` 转述给用户，问他是先删那些应用（连数据删时会收回发给它们的凭据），还是不删了，用户同意后按顺序删。保不保留数据都一样：只要还有装着的应用接着它，就删不了。

回退只换程序，不重新构建；数据库、数据目录和环境变量保持现在的样子，回退前把这一点告诉用户。如果后来的版本改过数据库结构，旧版本可能起不来，也要提醒。“重新部署那一版的代码”是用那一版的代码（Git 提交或本地目录）再 `deploy` 一次，会生成新的版本号。要回到哪一版，先 `acornfox versions --json` 看 `can_rollback` 为 true 的版本。回退出错时 `diagnosis.code`（`server_too_old`、`deploy_in_progress`、`version_image_gone`、`version_not_found`、`version_not_rollbackable`、`logs_not_kept` 等）照入口“遇到故障编号怎么办”运行 `acornfox faults 诊断码 --json`。几件要记住的：

- `server_too_old`：服务器上的 AcornFox 版本较旧，不支持这个功能。停下，告诉用户先升级服务器（见 `maintain.md`）；不要改用不带 `--to` 的 `rollback` 凑合，那会回到别的版本。
- `deploy_in_progress`：有一版正在部署，回退会取消它。问用户是否取消；用户同意后加 `--cancel-pending` 再执行。取消的那一版不会上线；回退上线前原来的版本继续服务。
- `version_image_gone`：那一版的程序已不在服务器上（`versions` 里它的 `rollback_blocked` 为 `image_gone`），改用 `can_rollback` 为 true 的版本，或重新部署那一版的代码。
- `version_is_live`、`version_same_as_live`：已经是这一版的程序，不需要回退；`version_not_rollbackable`：那一版没有成功上线过，换一个可回退的版本。
- `logs_not_kept`：那一版的日志没有保存（功能上线前部署的，或已超出保留范围），转述 `message`。

## 备份和恢复应用数据

用户说“备份一下 X”“把数据存一份”“升级前先留一份”“出问题了想退回去”“把数据恢复到昨天那样”时用这一节。备份 = 应用的全部数据（数据目录里的文件，附加数据库的数据也算）加上它的变量和设置，存在**这台服务器**上（数据目录下的 `app-backups/应用名/`）；恢复 = 用一份备份覆盖应用现在的数据。备份不是迁移：只能恢复到同一台服务器上的同名应用。每个应用手动备份留最近 5 份，旧的自动清掉。**自动另存的**（升级前快照 `pre-upgrade`、恢复前另存 `pre-restore`；升级后“退回”用的也是恢复，退回前另存的就是升级后的新数据）**至少留 7 天、每种最新 3 份也留着**：7 天内的不会被别的操作（备份、恢复、再升级）按份数清掉；只有备份目录放不下这次要做的备份时，才会提前清最旧的自动另存的（不清最新的一份、不清正在用来恢复的那份），清掉的找不回来，`--plan` 里会提前写明。

**最低路径：做一份备份（照着做）**

1. 运行 `acornfox --json backup create 应用名 --plan`，什么都不会动，只看：
   - `plan.will_prune_for_space` 不是空的：磁盘放不下，但可以先清掉这几份自动另存的旧备份再做（`plan.enough` 已经是 `true`）。把编号告诉用户，清掉找不回来，问同意了再做。
   - `plan.enough` 是 `false`：磁盘放不下，清自动另存的旧备份也不够（`BKP-01`）。把 `plan.say` 里的数字告诉用户，停下；用户要清理，列出 `acornfox backup list` 的旧备份请他选，**删哪份由用户定**。
   - `plan.will_stop` 是 `true`：先告诉用户“备份时应用会暂停一小会儿，打完包自动继续运行”。
   - 返回 `BKP-13`：这个应用没有数据可备份，照 `message` 转述。
2. 用户要做就运行 `acornfox --json backup create 应用名 [--note 备注]`。默认等到做完（最多 30 分钟）；数据很大时加 `--no-wait`，再用 `acornfox --json backup task 任务编号` 隔几秒查一次，直到 `status` 不是 `running`（轮询到条件满足就走，不写固定秒数的等待）。
3. 成功看 `result.backup_id`（备份编号）、`result.bytes`、`result.downtime_ms`（应用暂停了多久）。告诉用户：做好了、编号是什么、多大、应用已经继续运行。`result.pruned` 不是空的：这是这次**顺带清掉的旧备份编号**（超出份数的旧手动备份，或磁盘放不下时提前清的自动另存的旧备份），都找不回来，原样告诉用户清了哪几份；`--plan` 里的 `will_prune`（做完按份数清）、`will_prune_for_space`（放不下时先清）事先写明了，计划里有 `will_prune_for_space` 的，第 1 步就要告诉用户。`status` 是 `failed` 时看 `fault.code`：`BKP-03` 这个应用正忙（别的备份或恢复在做，或正在部署），等它做完再来；`BKP-02` 读数据出错，应用已经照原样重新启动，`acornfox status 应用名` 确认在运行后重试一次；其他编号 `acornfox faults 编号 --json`。

**最低路径：恢复（会覆盖现在的数据，必须先问用户）**

1. `acornfox --json backup list 应用名` 列出备份（新的在前：编号、种类 `manual` 手动 / `pre-upgrade` 升级前 / `pre-restore` 恢复前、时间、大小、当时的版本）。用户说“最新的”可以用编号 `latest`，但先告诉用户它是哪个时间的。
2. `acornfox --json backup restore 应用名 编号 --plan` 只看会怎样，**什么都不会动**：
   - `plan.intact` 是 `false`：这份备份文件不齐或被改过（`BKP-05`），不能用，换一份。
   - `plan.version_available` 是 `false`：备份时那个版本的程序已不在这台机器上（`BKP-09`）。把 `plan.version_then` 和 `plan.version_now` 告诉用户，问要不要在现在的版本上用这份数据（可能起不来，失败会自动退回）；同意才在下面加 `--allow-current-version`。
   - `plan.will_prune_for_space` 不是空的：备份目录放不下恢复前那份另存，要先清掉这几份自动另存的旧备份（`plan.enough` 已经是 `true`；不会清正在用来恢复的这份）。第 3 步问的时候把编号一起说，它们清了找不回来。
   - `plan.enough` 是 `false`：磁盘不够（`BKP-01`），把数字告诉用户，停下。
   - `plan.will_prune` 不是空的：恢复做完后会按份数清掉这几份旧的恢复前另存（都超过 7 天又不在最新 3 份里）。
3. **停下问用户**（恢复会丢数据，一律要问），一句话说清：“恢复会把应用 X 现在的数据换成 <备份时间> 的那份，那之后新写的数据会没有；恢复期间应用会停一阵；恢复前我会先把现在的数据另存一份（至少留 7 天），恢复失败会自动退回。要恢复吗？”用户明确说要，才继续。**不要自己加 `--yes`。**
4. `acornfox --json backup restore 应用名 编号 --yes`（等到做完；慢就加 `--no-wait` 再轮询 `backup task`）。成功后告诉用户：已恢复到哪份、现在运行的是哪个版本（`result.version`）、恢复前的数据另存在 `result.pre_restore_id`（想撤销这次恢复就用它再恢复一次，至少留 7 天，不想要了问过用户再删）；`result.pruned` 不是空的就把清掉的编号原样告诉用户（是计划里写过的；平时是空的，因为 7 天内的自动另存不会被清）；再照“确认应用能打开”确认应用真的能打开。`result.warnings` 里带 `BKP-15` 的每条原话转述：这是备份之后才接上这个应用服务的别的应用，账号在恢复回来的数据里没有了，它们要重新接上服务（按 `app-center.md` 的“从应用中心装应用”里的办法，涉及删除和重装先问用户）。
5. 失败时：`fault.code` 是 `BKP-07` —— 恢复没成功，数据已经自动退回到恢复之前的样子，应用在运行；把失败原因（`fault.message`）告诉用户，问要不要换一份备份再试。是 `BKP-08` —— 恢复没成功而且自动退回也没成功：**停下，不要再动这个应用，不要自己重试**，把任务的完整 JSON（`backup task 任务编号 --json`）和 `result.pre_restore_id` 交给用户，请他找负责的人。
6. 删一份备份：只在用户明确要求时，`acornfox --json backup rm 应用名 编号 --yes`（删了找不回来，先说清是哪一份）。

**可以发挥的地方：** 解释备份里有什么（应用的数据和变量，不含别的应用的数据）；建议升级、改配置、重要操作之前先备份；告诉用户备份占多大（`acornfox backup list`，不带应用名看整个备份目录和磁盘）；备份目录的问题运行 `acornfox doctor` 看（`BKP-14` 写不进去、`BKP-10` 有半截的备份）。

**必须先问用户的：** 恢复（上面第 3 步）、删备份、清理磁盘时删什么、`--allow-current-version`。应用被删除后它的备份还留着（`backup list 应用名` 能看到），恢复要先重新安装一个同名应用。

## 这台机器上不是从 AcornFox 装的容器（只看，v0.7.1 起）

用户问“我自己装的容器能看到吗”“这台机器上还有别的什么在跑”“有没有不是 AcornFox 装的容器”“内存是不是被别的东西占了”时用这一节。指用户自己 `docker run`、docker compose 或别的面板装的容器；AcornFox 自己的应用、数据库和入口代理不在其中。**只能看，不能管**：没有启动、停止、删除、接管它们的命令，也不要用 `docker` 命令去动它们，用户要处理请他自己来。

**最低路径（只读，不用问用户）：**

1. `acornfox --json other-containers`（情况乙、丙、丁、戊同前面的写法；`--target 服务器名` 看某台）。`containers` 是列表，每个有 `name`、`image`、`state`（`running` 在跑，`exited`、`dead` 停了，`paused` 暂停，`restarting` 在反复重启）、`ports`（对外的端口，`host_network` 为 `true` 的用本机的网络、没有端口可列）、在跑的有 `memory_usage_mb` 和 `cpu_percent`；`running`、`stopped`、`memory_mb`、`cpu_percent` 是合计。`containers` 是空的就是没有，照实说。
2. 对用户说：有几个、各自在跑还是停了、占了多少内存、用了哪些端口；这些不是 AcornFox 管的。用户在控制台“我的应用”最下面能看到同样的一组（默认收起，只看）。
3. 报 `MACHINE-04`：Docker 没回应，或服务器上的后台执行器是旧版本，照 `acornfox faults MACHINE-04` 做；这只影响这一块，应用不受影响。服务器太旧没有这个命令（`server_too_old`）：告诉用户先升级 AcornFox。

**要说清的限制：** Windows 上 AcornFox 跑在 WSL 里、Mac 上跑在苹果 container 开的虚拟机里，只能看到那个环境里的容器，**看不到** Windows 或 Mac 本机 Docker Desktop 里的；用户问起时照实说，不要说成“这台电脑上没有别的容器”。`acornfox machine` 里也有一份只有名字、在跑还是停了、端口的 `other_containers`，要用量用上面的命令。

**可以发挥的地方：** 用大白话解释哪个容器占得多；端口和 AcornFox 的应用冲突时（装应用报端口被占用），帮用户对一下是不是它们占的。**必须先问用户的：** 无（只读）；用户想停或删这些容器时，告诉他这不在 AcornFox 的管理范围内，请他自己用 docker 处理。

## 可以发挥的地方

- 用大白话解释 `status`、`diagnose`、日志里的内容，判断是代码问题还是配置问题；告诉用户这个应用占多少内存、哪个应用占得多。
- 解释备份里有什么（应用的数据和变量，不含别的应用的数据）；建议升级、改配置、重要操作之前先备份；告诉用户备份占多大；备份目录的问题运行 `acornfox doctor` 看。

## 必须先问用户的

- 删除应用或数据（`delete`、`remove … --volumes`）：一定写上应用名，执行前必须得到用户明确同意；别的应用接着它提供的服务（`app_in_use`）时不要硬删，也不要自己先删那些应用。
- 回退（只换程序，数据库、数据目录和环境变量保持现在的样子，回退前把这一点告诉用户；后来的版本改过数据库结构时旧版本可能起不来）；`deploy_in_progress` 要取消正在部署的那一版。
- 调整整机内存和 CPU 上限（`acornfox limits`）、调大某个应用的内存让别的应用挤不下：先问。
- 恢复（会覆盖现在的数据，一律先问，不要自己加 `--yes`）、删备份、清理磁盘时删什么、`--allow-current-version`。
- 启动用户停掉的应用：先问是不是有意停的。
