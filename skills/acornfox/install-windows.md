# 在这台 Windows 电脑上安装 AcornFox（情况丁）

**什么时候读这一册**：用户的电脑是 Windows 10 22H2 或 Windows 11（64 位 Intel 或 AMD），想把 AcornFox 装在这台电脑上，管理这台电脑上的应用。ARM 版 Windows 暂时不行（`WIN-ARCH-01`）。

**它是什么**：Windows 上不放我们自己的 exe，只用微软自带的 `wsl.exe`、PowerShell、计划任务和 `netsh`。AcornFox 装在 WSL 2 里一个叫 AcornFox 的 Debian 13 里（和 Linux 服务器上同一个程序、同一个 `install.sh`）；Windows 上多一个小命令 `acornfox`（`%LOCALAPPDATA%\AcornFox\bin`，已加进用户 PATH），它把命令转给 WSL 里的程序，`C:\…` 路径自动换成 `/mnt/c/…`，当前目录一起带过去。控制台在浏览器打开 `http://localhost:18800/`（局域网控制台 `http://localhost:18802/`）。

## 最低路径（固定步骤）

1. **只读检查**（什么都不改，退出码 0 就是能装）：在用户的 PowerShell 里运行下面“安装命令”，末尾加 `-Plan`（可再加 `-Json`，那时标准输出只有最后一行 JSON）。它会检查 Windows 版本、虚拟化、公司策略、磁盘，并打印将要做的事；检查不过时带故障编号 `WIN-…` 停下。`-Plan` 什么都不改，连操作记录文件也不写。
2. **先问用户、得到明确同意**，把下面三件事说给他听：① 第一次装会弹**一次管理员确认**（窗口写 Windows PowerShell、发布者 Microsoft Windows），要他自己点“是”（电脑上已有 WSL 2 就不弹也不重启）；② 电脑上没有 WSL 时要**重启一次电脑**，重启后登录会自动弹出窗口接着装；③ 默认让局域网里的手机、电脑也能打开应用，不想要加 `-LocalOnly`。下载约 350 MB（WSL 安装包）加 85 MB（Debian 镜像），装 Docker 等再几百 MB，一共要 10 GB 以上空闲。
3. **安装命令**（普通 PowerShell，**不要**“以管理员身份运行”；国内用第一条，海外用第二条，不确定就先试第二条，它会自动退到另一个源）：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol=3072; & ([scriptblock]::Create([Text.Encoding]::UTF8.GetString((New-Object Net.WebClient).DownloadData('https://gitee.com/VIP13390/AcornFox/raw/main/get.ps1')).TrimStart([char]0xFEFF)))"
```

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol=3072; & ([scriptblock]::Create([Text.Encoding]::UTF8.GetString((New-Object Net.WebClient).DownloadData('https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.ps1')).TrimStart([char]0xFEFF)))"
```

   要带参数就接在最后一个 `)` 后面，例如 `… ) -Plan`、`… ) -LocalOnly`、`… ) -Action upgrade`。
4. **看退出码**：`0` 成功；`3` 要重启——告诉用户保存工作后自己重启（**你不要替他重启**），重启后登录会自动接着装，也可以重新运行同一条命令；`5` 管理员确认被取消，什么都没装，问用户要不要再来一次；`1` 看屏幕最后的故障编号，运行 `acornfox faults WIN-…` 查（没装好、`acornfox` 命令还没有时，把安装命令末尾换成 `-Action doctor` 逐项看）。
5. **验收**：在**新开**的 PowerShell 里运行 `acornfox windows status`，每一项都是“通过”（加 `--json` 时标准输出只有一行 JSON：`{ok, action, exit_code, steps[], faults[]}`，没有别的文字）；再运行 `acornfox doctor`（先查 Windows 一侧，再查 WSL 里的 AcornFox）。控制台第一次打开要设密码（局域网控制台 `http://localhost:18802/`），由用户自己设。
6. **装 Skill**：运行 `acornfox skill install`（入口和全部分册一起写进 Skill 目录）。它只装进用户电脑上**已经有**的 AI 工作台：有 `~\.claude` 目录才装 Claude Code，有 `~\.codex` 目录才装 Codex（只用了其中一个就只装一份，这是对的，不是漏装）；两个都没有会退出码 2 并说明，这时问用户用的是哪个工作台，用 `--dir 那个工作台的 skills 目录` 指定。
7. **之后**：`acornfox apps`、`acornfox install 应用名`、`acornfox deploy` 等和 Linux 服务器上完全一样（用哪一册看入口的对照表）。部署本机文件夹：先 `cd` 到那个文件夹再运行 `acornfox deploy`，不用传文件。不需要 `acornfox open`（浏览器直接开 `http://localhost:端口`）。局域网里的设备用 `http://这台电脑的局域网地址:端口`，局域网地址在 `acornfox windows status` 的“局域网访问”那一行。

## 卸载（固定步骤）

1. **先问用户**：只卸程序、**保留数据**（应用和数据留在 WSL 的 AcornFox 里，以后重装接得上；默认就是这个），还是**连数据一起删**（不可恢复）。删数据必须用户明确说出来，不要替他选。
2. `acornfox windows uninstall --plan` 看计划（什么都不改），用户同意后运行 `acornfox windows uninstall`；要删数据的才加 `--purge --yes`。会弹**一次**管理员确认，要用户自己点“是”。
3. **看退出码**：
   - `0` 且最后一句是“已卸载（保留数据）”：WSL 里的服务和入口容器都已停掉，Windows 上的计划任务、防火墙和转发规则、PATH、`acornfox` 命令都拿掉了（`bin` 目录由后台进程过大约 10 秒删，别在这十秒里就去检查）；发行版 AcornFox 和里面的数据、下载过的镜像、本机钥匙、`%LOCALAPPDATA%\AcornFox\logs` 里的记录留着。
   - `1` 带 `WIN-UNINST-02`：**WSL 里没卸干净**（屏幕会说哪一步没成），Windows 这一侧**一点没动**，`acornfox` 命令还在，应用数据没动。不会说“已卸载”。先看 `%LOCALAPPDATA%\AcornFox\logs\uninstall-wsl.log` 最后几行和它报的编号，确认这台电脑能上网（卸载要联网取卸载脚本），再**用屏幕上给的完整路径命令重新运行**（`& "%LOCALAPPDATA%\AcornFox\bin\acornfox.cmd" windows uninstall`，重复运行不出错）；同一处失败两次就停下问用户，**不要**改用 `wsl --unregister` 硬删（它会连数据一起删）。
   - `1` 带 `WIN-UNINST-03`（只在 `--purge` 时）：注销发行版没成功，数据还在；先问用户，再按条目处理（`acornfox faults WIN-UNINST-03`）。
   - `1` 带 `WIN-UNINST-01`：没有终端又加了 `--purge` 却没加 `--yes`，什么都没删。
4. **验收**（保留数据）：`wsl -d AcornFox -u root -- systemctl is-active acornfox-server` 不是 `active`；`wsl -d AcornFox -u root -- docker ps` 里没有 acornfox 的容器；`http://localhost:18802/` 打不开；`wsl -l -v` 里还有 AcornFox。`--purge` 的验收是 `wsl -l -v` 里没有 AcornFox，`%LOCALAPPDATA%\AcornFox` 目录没了（也是过大约 10 秒才删完）。
5. **卸载成功以后**新开的 PowerShell 里就**没有 `acornfox` 命令了**，这是正常的，不是坏了：要重装就再运行一遍“安装命令”（会接上留下的旧数据，应用还在）；要连数据一起删，把安装命令末尾换成 `-Action uninstall -Purge -Yes`（先问用户）。

## 常用命令

| 要做什么 | 命令（在 Windows 的 PowerShell 或 cmd 里） |
| --- | --- |
| 看 Windows 一侧每一项好不好 | `acornfox windows status`（`--json` 给程序读）；`acornfox windows doctor` 同样内容 |
| 先看会做什么再升级 | 安装命令末尾加 `-Action upgrade -Plan`；确认后去掉 `-Plan` |
| 打开 / 关闭局域网访问 | `acornfox windows lan-on`、`acornfox windows lan-off`（要管理员确认一次）；加 `--plan` 先看 |
| 卸载（保留数据） | `acornfox windows uninstall`；先 `--plan`；失败时看 `%LOCALAPPDATA%\AcornFox\logs\uninstall-wsl.log` |
| 看 WSL 里的安装记录 | `wsl -d AcornFox -u root -- tail -n 60 /root/acornfox-install.log` |

## 出错时

拿到 `WIN-…` 故障编号：照入口“遇到故障编号怎么办”运行 `acornfox faults 编号`。几件要提前知道的：

- 检查没过（系统版本、ARM 版 Windows、公司策略、虚拟化、磁盘）什么都没装：把原因告诉用户，这几项都要用户自己处理（升级系统、联系 IT、开 BIOS 虚拟化、清磁盘）。
- 安装没成功、`acornfox` 命令还没有时：把安装命令末尾换成 `-Action doctor` 逐项看；`WIN-LINUX-01`（WSL 里的安装没成功）看 `wsl -d AcornFox -u root -- tail -n 60 /root/acornfox-install.log` 最后打印的 `INSTALL-…` 等编号，按那个编号查。
- 下载或校验失败（`WIN-DL-01`、`WIN-DL-02`）：确认能上网、关代理、再运行，已下的部分会保留，**不要跳过校验**。
- 管理员确认被取消、要重启（`WIN-ADMIN-01`、`WIN-REBOOT-01`）：问用户再运行一次；重启请用户自己做，重启后自动接着装。
- 卸载没做完（`WIN-UNINST-02`、`WIN-UNINST-03`）：按上面“卸载（固定步骤）”第 3 步，用屏幕上给的完整路径命令重试，两次不行停下问用户。
- 开机自启、局域网、命令、控制台打不开：先 `acornfox windows doctor`，按失败那一项的编号查。

## 可以发挥的地方

- 向用户解释每一步在做什么、为什么要管理员确认和重启，回答“会不会装东西到我电脑上”（装的东西都记在 `%LOCALAPPDATA%\AcornFox\windows-install.json`，卸载时按它拿掉；WSL 功能本身不卸载）。
- 复杂的 PowerShell 命令（带引号、管道）要传给 WSL 里的程序时，先写成文件：把命令写进 `.sh` 文件，再 `wsl -d AcornFox -u root -- bash /mnt/c/…/文件.sh`；PowerShell 5.1 会改写传给原生程序的双引号。
- 局域网访问的几个说法：只本机的应用不会被转出去；公共场所的网络（Windows 里叫“公用网络”）防火墙会挡，不要替用户改成“专用”（`WIN-LAN-03`）。

## 必须先问用户的

- 管理员确认和输入管理员密码：只能用户自己点、自己输。
- 重启电脑；`wsl --shutdown`（会关掉用户别的 WSL 发行版）；`wsl --update`；`wsl --unregister AcornFox`。
- 卸载：见上面“卸载（固定步骤）”。`acornfox windows uninstall`（默认保留数据）先 `--plan`，用户同意再运行；`--purge --yes` 连数据一起删，**必须先问用户并说清楚会删什么**；`WIN-UNINST-02` 重试之前也要确认能上网，两次不行就停下问用户。
- 改 BIOS、公司策略、Windows 防火墙、网络类型、`.wslconfig`；安全软件（360、火绒）弹窗请用户选“允许”。
