# 升级和卸载 AcornFox、更新电脑上的 Skill 和命令行

**什么时候读这一册**：用户要升级 AcornFox 到新版本、问“AcornFox 有没有新版本”，或 `acornfox status`、`acornfox doctor` 提醒有新版本（`PLAT-UPD-01`）；卸载 AcornFox；任何命令输出 `CLI-UPDATE-01`（服务器比这台电脑上的命令行新），或 `acornfox doctor` 报 `SETUP-SKILL-01`（已装的 Skill 和命令行不是同一个版本）；升级后要更新电脑上的 Skill 和命令行；问“怎么更新 Skill”。应用自己的升级读 `app-change.md`。

## 有没有新版本（v0.7.1 起）

服务器每 6 小时自己去线上查一次最新的正式版（国内先 Gitee，其他地区先 GitHub，一边不通换另一边；带“-”的预发布版不算），结果记在服务器上。它**只提醒，不会自己升级**。看到提醒的几个地方：

- `acornfox status`（看应用或看一次部署都一样）最后一行写“提醒：AcornFox 有新版本 …”；加 `--json` 时结果里多一个 `platform_update` 对象和 `latest_version`；
- `acornfox --json update`：同一个对象；`acornfox --json update check` 现在就去查一次（只读线上，几秒内重复运行只查一次）；
- `acornfox doctor` 里一行 `PLAT-UPD-01`（有新版本，不是故障）或 `PLAT-UPD-02`（好几天查不到，应用不受影响）；
- 控制台“设置 › 这台机器 › 升级”那一行，和铃铛里的一条“AcornFox 有新版本”（同一个版本只提醒一次）。

最低路径（固定步骤）：

1. 看到有新版本，运行 `acornfox --json update`（要最新结果加 `check`），读 `platform_update`：`latest`（新版本）、`notes_url`（发行说明）、`upgrade_command`（这台机器该用的那一条命令，Linux、Windows、Mac 不一样，已经按国内海外选好）、`upgrade_where`（在哪里运行）。`available` 是 `false` 就不用升。
2. 用一句话告诉用户：现在是 X 版，线上有 Y 版，发行说明在 `notes_url`，升级会让命令行和控制台暂时连不上几秒；**问他要不要现在升**。不问就升、替他挑时间都不行。
3. 用户同意：照 `upgrade_command` 原样在 `upgrade_where` 说的地方运行，再按下面“升级、卸载”看结果和故障编号；想先看会做什么：Linux、Mac 把命令末尾的 `upgrade` 换成 `upgrade --plan`，Windows 在命令里 `-Action upgrade` 后面加 ` -Plan`。
4. 用户说“先不升”：什么都不做，也不要反复提醒；他想看时再运行 `acornfox update`。

`platform_update.enabled` 是 `false`：服务器上关掉了这个提醒（环境变量 `ACORNFOX_UPDATE_CHECK=off`），不会有“有新版本”，用户要升级自己说；不要替他打开。`update check` 报 `platform_update.ok` 是 `false`：照 `acornfox faults PLAT-UPD-02` 查（先看 `error`），它不影响任何应用。

## 升级、卸载

**升级要先问用户**（升级期间命令行和控制台会暂时连不上，公网控制台约 4 秒打不开）。从 0.6.0 之前的版本第一次升上来时会换一次入口代理容器：新建的访问不受影响，但已经连着的长连接、超过 8 秒还没做完的请求和 WebSocket 会断一次，问用户时把这一句也说上，建议避开访问高峰。用户同意后在服务器上执行（国内、海外二选一；不要加 `-y`）：

```bash
curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade
```

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- upgrade
```

通过管道运行就算同意升级，不会再询问。已经是最新版本时，脚本打印“已是最新版本 …，不用升级”后直接退出，服务不会停，这是正常的，告诉用户“已是最新版”即可。不要为了“强制升级”加 `-y`：加了会把同一版本重装一遍，白停一次服务。`install-linux.md` 第 3 步是 `sudo=need-password` 时，把命令交给用户运行。

升级成功时最后打印 `升级完成！`（同一版本重装是 `重装完成！`）。升级后，情况乙还要重新运行一次 `connect-server.md` 第 7 步的 install-cli 命令，让电脑上的 CLI 和服务器同版本，再运行 `acornfox skill install` 更新电脑上的 Skill（见下面“更新 Skill 和命令行”；不做的话命令行会提醒一次，`acornfox doctor` 也会提醒）；情况丙不用（服务器上的命令行随升级一起换了）。

**AcornFox 本身只能升级，不能回到旧版本。** 没有 `--rollback`；`--version` 指定比现在旧的版本会被拒绝，加 `-y` 也一样。用户说“回滚 AcornFox”“退回上一版 AcornFox”时，告诉用户：平台不支持回到旧版本；升级没成功时脚本已经自动恢复到升级前；应用自己的版本可以回退（`acornfox rollback 应用名`，见 `app-life.md` 的“生命周期”）。不要去拷备份目录里的文件，也不要找旧版本的升级脚本来降级。

升级时 SSH 断开、看不到结果：重新连上后运行 `sudo tail -n 40 /var/log/acornfox-upgrade.log`，脚本自动处理的过程和故障编号写在这里；再运行 `acornfox --json version` 看现在的版本。
升级脚本停下时会打印 `故障编号: …`（`UPGRADE-…`、`INSTALL-…`、`EDGE-…`），照入口“遇到故障编号怎么办”运行 `acornfox faults 编号 --json`（服务器上的命令行已经在，直接查）。几件固定的事：

- `UPGRADE-DOWN-01`、`UPGRADE-ROLLBACK-01`：服务没停、什么都没改，AcornFox 只能升级，告诉用户这个版本装不了；不要加 `-y` 或换旧版本号重试，要升级就去掉 `--version` 重新运行上面的命令。
- `UPGRADE-FAIL-01`、`UPGRADE-FAIL-02`、`UPGRADE-FAIL-03`、`UPGRADE-BACKUP-01`：没有升上，脚本已自动恢复到升级前，服务照常运行；告诉用户现在仍是原来的版本（`acornfox --json version`）和失败原因；`FAIL-03` 可以再运行一次，别的先问用户要不要再试一次，再失败就停下，请用户把输出反馈给维护者。
- `UPGRADE-OLD-01`（现在装的是 v0.3.x）：要分两次升，先问用户，同意后在 `upgrade` 后面加 `--version v0.5.1`（例如 `… | sudo bash -s -- upgrade --version v0.5.1`），看到 `升级完成！` 后再运行一次不带 `--version` 的升级命令。
- `EDGE-06`、`UPGRADE-RESTORE-01`（恢复到升级前时没能换回去）：**停下**，运行 `acornfox faults 编号 --json`，把它的 `steps` 和输出原样给用户；里面的 `docker rm -f`、`docker start` 要用户同意后才做；不要删备份、不要重装、不要自己拷备份里的文件。

**卸载只在用户明确要求时做。** 默认保留数据；`--purge` 会连数据一起删除且不可恢复，必须用户明确说要删数据。卸载脚本要在终端里输入 `yes` 确认，请用户在自己的终端里运行。经 ssh 执行、没有终端时，脚本报 `UNINSTALL-TTY-01` 退出，什么都没改；只有用户明确要你代为卸载时才加 `-y`（`sudo bash get.sh uninstall -y`）。

```bash
curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh -o get.sh && sudo bash get.sh uninstall
```

国内把地址换成 `https://gitee.com/VIP13390/AcornFox/raw/main/get.sh`。

## 更新 Skill 和命令行（v0.7.0 起）

Skill 和电脑上的命令行都放在用户自己的电脑上，服务器升级后它们**不会**跟着更新。`acornfox skill install` 用与命令行同版本的整个 Skill（入口 `SKILL.md` 加全部分册）覆盖 AI 工作台里那个目录，并在每个文件头写一行 `<!-- acornfox-skill-version: x.y.z -->`（入口在 frontmatter 后面，分册在第一行标题后面），就是写它的那个命令行的版本号；目录里多出来的旧文件（旧版本留下的分册，老版本只有一个 `SKILL.md` 时也一样）会删掉，`machine-*.md` 本机能力分册保留。`acornfox skill print` 只打印入口（同样带版本号），`--all` 打印全部文件，`--file 册名` 打印一册（例如 `--file install-linux.md`）。情况丙（管理别的服务器、Windows 上没有命令行）用 `ssh 用户@地址 'acornfox skill print'` 取服务器上同版本的入口，`ssh 用户@地址 'acornfox skill print --file 册名'` 取分册，一个个存进 Skill 目录；情况丁在 Windows 上直接运行 `acornfox skill install`，装进 Windows 用户目录。

有两处会提醒你：

- `CLI-UPDATE-01`：任何连服务器的命令都可能带：标准错误里一行“提醒：服务器 … 上的 AcornFox 是 … 版，这台电脑上的命令行是 … 版，比服务器旧”；加了 `--json` 时在结果的 `notices` 里（`code`、`message`、`hint`、`commands` 是要运行的命令）。意思：服务器比这台电脑上的命令行新。每台电脑每天最多一次；命令照常运行。设环境变量 `ACORNFOX_NO_UPDATE_NOTICE=1` 可关掉。
- `SETUP-SKILL-01`（warn）：在用户电脑上运行 `acornfox doctor` 时看到（Windows 本机用 `acornfox doctor`，它会先查 Windows 一侧）。意思：已装的 Skill 和命令行不是同一个版本（入口或某个分册的版本号对不上、缺哪一册，没有版本号的是老版本写的）。

最低路径（看到上面任何一条）：

1. 用一句话告诉用户：服务器是 X 版，这台电脑上的命令行是 Y 版，建议更新。
2. **更新命令行要先问用户**（它换掉电脑上的程序），同意后在**用户电脑上**运行 `commands` 里的命令：国内用 Gitee 那条，海外用 GitHub 那条，和 `connect-server.md` 第 7 步一样；Windows 本机运行 `acornfox windows upgrade`（或安装命令末尾加 `-Action upgrade`）。只有 `SETUP-SKILL-01` 提醒“Skill 比命令行旧”、命令行本来就是新的，跳过这一步。
3. 运行 `acornfox skill install` 更新 Skill（只覆盖 Skill 目录里的文件，可以直接做）。
4. 确认：`acornfox --json version` 的版本和服务器一致；`acornfox doctor` 里 `SETUP-SKILL-01` 那一行是“都是 … 版”。
5. 告诉用户：**本次对话里读到的还是旧 Skill，新开一个对话才会读到新的**。

**本机能力分册（v0.7.0 起）：** `acornfox skill install` 还会给每台登记的服务器各写一份“这台机器现在能做什么”的分册 `machine-服务器名.md`（和 `SKILL.md` 放在同一个目录）。`acornfox skill refresh` 只刷新这些分册（给 `--target 服务器名` 只刷新一台）：命令行里装、删、升级、退回、恢复应用和改设置成功后会自己刷新；用户在页面上动过应用之后，或分册超过一天，运行一次。分册只是缓存：服务器连不上或太旧没有这个接口报 `MACHINE-03`，Skill 目录写不进去报 `MACHINE-02`，都不影响别的事，改用 `acornfox --json machine` 直接取。

可以发挥的地方：解释为什么两处不会自己更新；用户一直不想更新时，说明旧命令行连新服务器能用的范围（出现 `version_mismatch` 才是真连不上，见 `connect-server.md` 的“连接出错”）。

## 日常自检

`acornfox doctor --json`（只读）逐项检查这台机器：每项有 `ok`、`level`（`ok`、`warn`、`fail`）、`text`、`check`，要处理的项另有 `code`（故障编号，照入口规则查）；`warn` 转述给用户即可，不挡路。在用户电脑上运行时它还会检查已装的 Skill 和命令行是不是同一个版本（`SETUP-SKILL-01`）。装在服务器上的完整自检和怎么读它的输出见 `install-linux.md` 第 6 步。

## 可以发挥的地方

- 解释为什么 Skill 和电脑上的命令行不会自己更新；用户一直不想更新时，说明旧命令行连新服务器能用的范围（出现 `version_mismatch` 才是真连不上）。
- 解释升级会发生什么（命令行和控制台暂时连不上、公网控制台约 4 秒打不开）、建议避开访问高峰。

## 必须先问用户的

- 升级 AcornFox（会停服务几秒；从 0.6.0 之前升上来还会换一次入口代理容器，已连着的长连接会断一次）：问了再做，不要加 `-y`；`sudo=need-password` 时把命令交给用户运行。
- 卸载：只在用户明确要求时做；`--purge` 会连数据一起删且不可恢复，必须用户明确说要删数据；卸载脚本要在终端里输入 `yes`，请用户自己运行，只有用户明确要你代为卸载时才加 `-y`。
- 更新电脑上的命令行（它换掉电脑上的程序）：先问；只更新 Skill（`acornfox skill install`）可以直接做。
