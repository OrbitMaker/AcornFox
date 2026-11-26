# AcornFox 的 Windows 安装脚本（也管升级、卸载、检查）。
#
# 做什么：在这台 Windows 电脑上装好 AcornFox。Windows 上不放我们自己的 exe：只用微软自带的 wsl.exe、
#   PowerShell、计划任务和 netsh；AcornFox 本体是和 Linux 服务器上同一个程序，装在一个叫 AcornFox 的
#   WSL 2 发行版里（默认 Debian 13）。装完 Windows 上多一个小命令 acornfox（实际转到 WSL 里运行）。
# 什么时候用：用户在 Windows 上要用 AcornFox（管理自己电脑上的应用）。一般不直接运行这个文件，
#   用引导脚本 get.ps1（它取「要装的那个版本自带的」这个脚本、核对校验值再交给它），用法见仓库 README 的「Windows」一节。
# 前提：Windows 10 22H2（19045）或 Windows 11，64 位 Intel/AMD（ARM 版暂不支持），CPU 开了虚拟化，
#   系统盘剩余 10 GB 以上，能联网；PowerShell 5.1（系统自带）。第一次装且这台电脑没有 WSL 时，
#   会弹一次「管理员确认」（弹窗上写 Windows PowerShell、发布者 Microsoft Windows）并要重启一次电脑，
#   重启后登录会自动接着装。电脑上已经有 WSL 2 时不弹管理员确认也不重启（想让局域网里的设备能打开应用、
#   或者要登记开机自启时才弹一次）。
#
# 用法：powershell -NoProfile -ExecutionPolicy Bypass -File install-windows.ps1 [动作] [选项]
#   动作（不写就是 install）：
#     install     装：检查 → 装或升级 WSL 2 → 经下载网关下载 WSL 安装包和 Debian 镜像 → 导入发行版 → 在里面装 AcornFox
#                 → 登记开机自启 → 局域网访问 → 写 acornfox 命令 → 打开控制台。每一步做过就跳过，重复运行不出错
#     resume      重启电脑后接着装（登录后自动运行，也可以手工运行；等同 install）
#     upgrade     升级：在 WSL 里运行本版本的升级脚本，刷新 Windows 上的小命令和计划任务
#     uninstall   卸载：默认保留数据。先在 WSL 里卸（本版本自带的 uninstall.sh，要联网；再回头查服务停了、没有容器在跑），
#                 卸不干净就停下（WIN-UNINST-02，退出码 1，Windows 这一侧一点不动、acornfox 命令还在，能重试）；
#                 卸干净了才拿掉 Windows 上的自启、转发、命令，发行版和里面的数据、下载的镜像、本机钥匙、记录留着，重装接得上；
#                 加 -Purge 先注销发行版（失败 WIN-UNINST-03）再连目录、本机钥匙一起删（不可恢复）
#     status      只看：每一部分现在是什么状态（不改任何东西）
#     doctor      只看：逐项检查并给出故障编号（WIN-…，目录见 acornfox faults）（不改任何东西）
#     lan-on / lan-off   打开 / 关闭让局域网里的设备访问（要管理员确认一次）
#     lan-sync    局域网转发的同步循环（由计划任务 AcornFoxLAN 运行，不用手工运行）
#   选项：
#     -Plan            试跑：只说要做什么，不下载、不改任何东西，连操作记录文件也不写（写操作都支持）
#     -Json            标准输出只有最后一行 JSON（ok、action、exit_code、steps、faults），不再打印给人看的话；给 AI 工作台和脚本用
#     -Yes             不再询问（卸载 -Purge 在没有终端时必须加）
#     -China / -Global 这一次先用国内（Gitee）或海外（GitHub）下载 AcornFox 本体；不写就当场测速：同时从 Gitee 和 GitHub 各取同一个发行版文件的开头，
#       比每秒取到多少，快的先用，屏幕上有一行“测速：Gitee x MB/s、GitHub y MB/s，先用 …”，并把这个判断用 --china/--global 交给 WSL 里的 get.sh 和 install.sh
#     -Image debian|ubuntu   用哪个 Linux 发行版镜像（默认 debian，Ubuntu 24.04 可选）
#     -NoGateway       不联系下载网关，WSL 安装包和镜像直接从官方地址下载（也可设环境变量 ACORNFOX_GATEWAY= 空值）
#     -LocalOnly       应用只在这台电脑上打开，不让局域网里的设备访问
#     -NoOpen          装完不自动打开浏览器
#     -Version vX.Y.Z  要装的 AcornFox 版本（带版本戳的发行版脚本不用写）
#     -ReleaseBase 地址   测试或自建镜像：AcornFox 本体和脚本只从 <地址>/releases/download/<版本>/ 取
#     -LocalPackage 目录  测试：用目录里的 install.sh、acornfox_linux_amd64、入口包，不下载
#     -Distro 名字     发行版名字（默认 AcornFox；只在测试时改）
#     -Gateway 地址    下载网关地址（默认 https://dl.acornfox.com；也可设环境变量 ACORNFOX_GATEWAY）
#     -From 名字       来源标记（官网链接带来的，例如 bili-1201；只认小写字母、数字、-，最长 32；不合规的当没有，提醒一句、照常安装）：
#       只在安装（install、resume）时用，交给 WSL 里的 get.sh 的 --from，装好后 AcornFox 向下载网关报一次、只报这个标记；
#       升级、卸载等不带；不写就什么都不报
# 退出码：0 成功；1 出错（屏幕上有故障编号 WIN-…，说明和处理办法见 acornfox faults WIN-xx）；
#   2 用法不对；3 需要重启电脑（重启后登录会自动接着装）；5 管理员确认被取消。
#
# 会改动的地方（卸载时按这个清单拿掉；记录在 %LOCALAPPDATA%\AcornFox\windows-install.json）：
#   WSL 发行版 AcornFox（%LOCALAPPDATA%\AcornFox\wsl）；计划任务 AcornFoxWSL（登录和开机时把 WSL 叫起来）、
#   AcornFoxLAN（局域网转发同步，只在开了局域网访问时有）；Windows 防火墙规则 AcornFox-LAN-WSL；
#   netsh portproxy 规则（只管 18802 和 18810-18899 这些端口）；%UserProfile%\.wslconfig 里的两项
#   （instanceIdleTimeout、vmIdleTimeout，原来没有才加，卸载只拿掉我们加的）；用户 PATH 里加
#   %LOCALAPPDATA%\AcornFox\bin；注册表 RunOnce（重启后接着装，装完自己删）；%ProgramData%\AcornFox（只有管理员能写，
#   放局域网同步脚本和 WSL 安装包的副本）。WSL 功能本身（虚拟机平台、WSL 程序）不卸载：电脑上别的东西可能在用。
# 验证：运行 acornfox doctor（会先查 Windows 这一侧，再查 WSL 里的 AcornFox）；浏览器打开 http://localhost:18800/ 能出登录页；
#   acornfox apps 能列出应用。
# 怎么停下或回滚：装到一半按 Ctrl+C，已经做完的步骤留着，再运行一次会接着做；想退回装之前：
#   运行 uninstall -Purge。管理员确认选「否」：什么都没装，文件下载的部分留着，下次直接用。
# 出错先看哪里：屏幕上的故障编号和上面几行；操作记录 %LOCALAPPDATA%\AcornFox\logs\install-windows.log；
#   管理员那一步的记录 %LOCALAPPDATA%\AcornFox\logs\admin-step.log；WSL 里的安装记录 /root/acornfox-install.log。
#   卸载时 WSL 里的输出 %LOCALAPPDATA%\AcornFox\logs\uninstall-wsl.log。
# 环境变量：ACORNFOX_GATEWAY（下载网关地址，设成空值就不联系网关）、ACORNFOX_RELEASE_BASE（同 -ReleaseBase）、
#   ACORNFOX_PROBE_SECONDS（测速最多等几秒，默认 5）、ACORNFOX_PROBE_VERSION（只给测试：用哪个版本的文件测速，默认就是要装的版本）、
#   ACORNFOX_GITEE_HOST、ACORNFOX_GITHUB_HOST（只给测试：换成假服务器）、ACORNFOX_REPO、ACORNFOX_GITEE_REPO（换发布仓库）、AFX_WIN_DL_PARALLEL（分段下载起始路数，默认 16，最多 32）；
#   AFX_WIN_LIB=1（只给测试用：点引入本脚本时只定义函数、不执行，用来单独测下载这类函数）。
#
# AFX-FEATURE: install-from   （get.ps1 靠这一行知道这份脚本认 -From；老版本没有，get.ps1 就不往下传）
#
# 本文件含中文，必须存成带 BOM 的 UTF-8（英文 Windows 的 PowerShell 5.1 读无 BOM 的中文会解析错）。
# 局域网转发的做法（WSL 2 默认是 NAT 网络，Windows 局域网地址上没人监听，要自己转）：
#   WSL 里 0.0.0.0 上监听的端口（也就是「局域网能打开」的应用和 18802 局域网控制台）由 AcornFoxLAN 任务每 5 秒对一次，
#   用 netsh portproxy 把 Windows 的 0.0.0.0:端口 转到 WSL 的内部地址；只本机的应用在 WSL 里只监听 127.0.0.1，不会被转出去。
#   mirrored 网络模式不用转，但在开发机的嵌套虚拟机里连 localhost 都不通，没法验证，所以没用。

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('install', 'resume', 'upgrade', 'uninstall', 'status', 'doctor', 'lan-on', 'lan-off', 'lan-sync')]
    [string]$Action = 'install',
    [switch]$Purge,
    [switch]$Plan,
    [switch]$Json,
    [switch]$Yes,
    [switch]$China,
    [switch]$Global,
    [ValidateSet('debian', 'ubuntu')]
    [string]$Image = 'debian',
    [switch]$NoGateway,
    [switch]$LocalOnly,
    [switch]$NoOpen,
    [string]$Version = '',
    [string]$ReleaseBase = '',
    [string]$LocalPackage = '',
    [string]$Distro = 'AcornFox',
    [string]$Gateway = '',
    [string]$From = ''
)

# 发行版里这一行在构建时被写成它所属的版本号（scripts/build-release.sh）；源码里是 dev：没有绑定版本，要写 -Version 或 -LocalPackage
$ScriptVersion = 'dev'

Set-StrictMode -Version 2
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072 } catch { }
$env:WSL_UTF8 = '1'

# 来源标记：写法和官网统计、下载网关一样（a-z、0-9、-，1～32 个；-cmatch 区分大小写，\z 不放过结尾的换行）；
# 不合规的当没有，提醒一句，照常安装。只有安装（install、resume）用它，别的动作一律不带
if ($From -and $From -cnotmatch '^[a-z0-9-]{1,32}\z') {
    if (-not $Json) { Write-Host "[AcornFox] 来源标记 -From $From 不合规（只认小写字母、数字、-，最长 32 个字符），已忽略，照常安装" }
    $From = ''
}

# ---------------------------------------------------------------------------------------------------------
# 常量
# ---------------------------------------------------------------------------------------------------------
$script:Root = Join-Path $env:LOCALAPPDATA 'AcornFox'
$script:BinDir = Join-Path $script:Root 'bin'
$script:Work = Join-Path $script:Root 'work'
$script:Logs = Join-Path $script:Root 'logs'
$script:Downloads = Join-Path $script:Root 'downloads'
$script:WslDir = Join-Path $script:Root 'wsl'
$script:StateFile = Join-Path $script:Root 'windows-install.json'
$script:LogFile = Join-Path $script:Logs 'install-windows.log'
$script:AdminLog = Join-Path $script:Logs 'admin-step.log'
$script:AdminResult = Join-Path $script:Work 'admin-result.json'
$script:ProgramDataDir = Join-Path $env:ProgramData 'AcornFox'
$script:SyncScriptPath = Join-Path $script:ProgramDataDir 'bin\afx-lan-sync.ps1'
$script:SelfCopy = Join-Path $script:Root 'afx-win.ps1'
$script:TaskWsl = 'AcornFoxWSL'
$script:TaskLan = 'AcornFoxLAN'
$script:FirewallRule = 'AcornFox-LAN-WSL'
$script:LanPorts = '18802,18810-18899'
$script:MinFreeGB = 10
$script:ConsolePort = 18800
$script:LanConsolePort = 18802
$script:GatewayDefault = 'https://dl.acornfox.com'

# 要下载的大文件：文件 ID、大小和 SHA256 都和下载网关的文件清单（tools/dl-gateway/manifest.tsv）一致，
# 校验值写死在这里，下载来源（网关、官方地址）给的值只用来比对。官方地址是网关不可用时的退路。
$script:Files = @{
    wsl    = @{ Id = 'wsl-x64-3.0.3'; Name = 'wsl.3.0.3.0.x64.msi'; Size = 367575040
        Sha256 = 'ae977286974255bd26bf925f00f40f5692c6b2084155cbc0563c09aea091f211'
        Urls = @('https://github.com/microsoft/WSL/releases/download/3.0.3/wsl.3.0.3.0.x64.msi'); Title = 'WSL 安装包（约 351 MB）' }
    debian = @{ Id = 'debian-13-wsl-x64-1.26.0.0'; Name = 'Debian_WSL_AMD64_v1.26.0.0.wsl'; Size = 89228801
        Sha256 = '5ec7dc68216e75d1d4d4761474e99d8461a98d316537110314b137122a879e0f'
        Urls = @('https://salsa.debian.org/debian/WSL/-/jobs/9606244/artifacts/raw/Debian_WSL_AMD64_v1.26.0.0.wsl'); Title = 'Debian 13 的 WSL 镜像（约 85 MB）' }
    ubuntu = @{ Id = 'ubuntu-24.04.5-wsl-x64'; Name = 'ubuntu-24.04.5-wsl-amd64.wsl'; Size = 388975696
        Sha256 = 'bb415d824822c4b878125729af451a5d18fb13d1cf5cbed9a7393ad64ac6039e'
        Urls = @('https://mirrors.huaweicloud.com/ubuntu-releases/24.04.5/ubuntu-24.04.5-wsl-amd64.wsl',
                 'https://mirrors.aliyun.com/ubuntu-releases/24.04.5/ubuntu-24.04.5-wsl-amd64.wsl',
                 'https://releases.ubuntu.com/24.04.5/ubuntu-24.04.5-wsl-amd64.wsl'); Title = 'Ubuntu 24.04 的 WSL 镜像（约 371 MB）' }
}

# 故障编号 → 一句话说明和处理办法（完整目录是 acornfox faults 里的 WIN-… 条目，那里是唯一来源；这里只放屏幕上要打的短句）
$script:FaultText = @{
    'WIN-OS-01'     = @('Windows 版本太旧（要 Windows 10 22H2 或 Windows 11）', '先升级 Windows 再运行')
    'WIN-ARCH-01'   = @('这台电脑的 CPU 架构暂时不支持（现在只支持 64 位 Intel/AMD）', 'ARM 版 Windows 暂时没有对应的 WSL 安装包和 Linux 镜像；可以改把 AcornFox 装在别的 Linux 服务器上，用 SSH 管理')
    'WIN-POLICY-01' = @('公司或学校的策略禁止了 WSL', '请联系管理这台电脑的 IT 人员放开 WSL；自己改注册表可能违反规定，这里不替你改')
    'WIN-VIRT-01'   = @('CPU 的虚拟化功能没打开', '重启进 BIOS/UEFI，打开 Intel VT-x（Virtualization Technology）或 AMD-V（SVM Mode）后再运行；虚拟机里要在虚拟机设置里打开嵌套虚拟化')
    'WIN-DISK-01'   = @('系统盘剩余空间不够（至少要 10 GB）', '清理磁盘后再运行')
    'WIN-PS-01'     = @('缺少必要的系统组件（PowerShell 5.1 或 curl.exe）', '升级 Windows 10 到 22H2 后再运行')
    'WIN-SRC-01'    = @('这是源码里的脚本，没有绑定版本', '用引导脚本 get.ps1 安装；开发测试时加 -Version 或 -LocalPackage')
    'WIN-ADMIN-01'  = @('管理员确认被取消，什么都没安装', '想继续时重新运行；弹窗上写 Windows PowerShell、发布者 Microsoft Windows 时选「是」')
    'WIN-ADMIN-02'  = @('管理员那一步出错了', '看 %LOCALAPPDATA%\AcornFox\logs\admin-step.log 里最后几行，再重新运行')
    'WIN-REBOOT-01' = @('需要重启电脑才能继续', '保存手上的工作后重启；重启后登录会自动接着装')
    'WIN-DL-01'     = @('下载没成功（所有下载来源都试过）', '确认能上网、关掉代理后重新运行；已经下好的部分会保留')
    'WIN-DL-02'     = @('下载的文件校验不通过，已删掉没有使用', '重新运行会重新下载；反复出现请反馈，不要想办法跳过校验')
    'WIN-WSL-01'    = @('WSL 用不了', '运行 wsl --status 看原因；多半是还没重启，或虚拟机平台没打开')
    'WIN-WSL-02'    = @('导入 Linux 发行版失败', '看操作记录里 wsl --import 的输出；磁盘空间或杀毒软件拦截是常见原因')
    'WIN-WSL-03'    = @('Linux 发行版里的 systemd 没有起来', '运行 wsl --shutdown 后重新运行；还不行看 wsl -d AcornFox -u root -- systemctl status')
    'WIN-LINUX-01'  = @('在 WSL 里安装 AcornFox 没成功', '看 WSL 里的 /root/acornfox-install.log 和它最后打印的故障编号（INSTALL-… 等）')
    'WIN-START-01'  = @('开机自启没登记成功', '这台电脑重启后 AcornFox 不会自己起来；以管理员确认再运行一次 install，或运行 acornfox windows lan-on')
    'WIN-LAN-01'    = @('局域网转发没生效', '运行 doctor 看哪一项；360 等安全软件常拦 netsh，弹窗里选「允许操作」')
    'WIN-LAN-02'    = @('当前账号不是管理员，局域网访问没打开', '应用只在这台电脑上能打开；想让局域网里的设备访问，用管理员账号再运行 install')
    'WIN-LAN-03'    = @('这台电脑的网络类型是「公用」，Windows 防火墙会挡住局域网里的设备', '在 Windows 设置里把这个网络改成「专用」（家里或办公室的可信网络），再试')
    'WIN-CLI-01'    = @('acornfox 命令没能写进去', '看操作记录；也可以直接用 wsl -d AcornFox -u root -- acornfox …')
    'WIN-NET-01'    = @('控制台打不开（localhost 上没有应答）', '运行 doctor；多半是 WSL 里的服务没起来，wsl -d AcornFox -u root -- systemctl status acornfox-server')
    'WIN-SKILL-01'  = @('Windows 用户目录里已装的 AcornFox Skill 和 WSL 里的程序版本不一样', '运行 acornfox skill install 更新（升级 AcornFox 之后都要做一次）')
    'WIN-UNINST-01' = @('卸载要确认，但这里没有终端可以问', '加 -Yes 表示确认；加 -Purge 会连数据一起删，不可恢复')
    'WIN-UNINST-02' = @('WSL 里的 AcornFox 没卸干净（卸载脚本没成功，或服务、容器还在），Windows 这一侧没有动，acornfox 命令还在', '看 %LOCALAPPDATA%\AcornFox\logs\uninstall-wsl.log 最后几行和它报的编号（INSTALL-GET-…、UNINSTALL-…），确认能上网后重新运行卸载（命令写在上面）')
    'WIN-UNINST-03' = @('没能注销 WSL 发行版（-Purge 的最后一步），数据还在', '运行 wsl --shutdown 后重新运行卸载；还不行看 wsl --list --verbose，用户在别处打开着这个发行版时先关掉')
}

# ---------------------------------------------------------------------------------------------------------
# 输出、记录、状态
# ---------------------------------------------------------------------------------------------------------
$script:Steps = New-Object System.Collections.ArrayList
$script:Faults = New-Object System.Collections.ArrayList
$script:ExitCode = 0
$script:NoLog = $false

# -Json 时标准输出只留最后那一行 JSON：给人看的话（Write-Host 打的）全部不输出，内容都在 steps、faults 里。
# 要问用户确认的 Read-Host 不受影响（它自己带提示）。
if ($Json) { function Write-Host { } }

function Write-Log([string]$Text) {
    # -Plan 说“什么都不改”，连操作记录也不写；卸载到最后把整个目录删掉以后也不再写（否则会把刚删的目录又建出来）
    if ($Plan -or $script:NoLog) { return }
    try {
        if (-not (Test-Path -LiteralPath $script:Logs)) { [void](New-Item -ItemType Directory -Path $script:Logs -Force) }
        [IO.File]::AppendAllText($script:LogFile, ("{0} {1}`r`n" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Text), (New-Object Text.UTF8Encoding($true)))
    } catch { }
}
function Say([string]$Text) { Write-Host "[AcornFox] $Text"; Write-Log $Text }
function SayWarn([string]$Text) { Write-Host "[AcornFox] 注意：$Text" -ForegroundColor Yellow; Write-Log "WARN $Text" }
function Add-Step([string]$Id, [string]$State, [string]$Detail = '') {
    [void]$script:Steps.Add([pscustomobject]@{ id = $Id; state = $State; detail = $Detail })
    Write-Log "step $Id $State $Detail"
}

# 打印故障并结束（退出码 1，或指定的码）。Plan 模式下检查类故障也走这里。
function Stop-WithFault([string]$Code, [string]$Detail = '', [int]$Rc = 1) {
    $t = $script:FaultText[$Code]
    if (-not $t) { $t = @($Code, '') }
    [void]$script:Faults.Add([pscustomobject]@{ code = $Code; message = $t[0]; hint = $t[1]; detail = $Detail })
    Write-Host ''
    Write-Host "[AcornFox] 出错：$($t[0])" -ForegroundColor Red
    if ($Detail) { Write-Host "    $Detail" }
    if ($t[1]) { Write-Host "    怎么办：$($t[1])" }
    Write-Host "故障编号: $Code"
    Write-Log "FAULT $Code $Detail"
    $script:ExitCode = $Rc
    throw 'AFX_FAULT'
}

function Read-State {
    if (Test-Path -LiteralPath $script:StateFile) {
        try { return (Get-Content -LiteralPath $script:StateFile -Raw -Encoding UTF8 | ConvertFrom-Json) } catch { }
    }
    return ([pscustomobject]@{})
}
function Get-StateValue($State, [string]$Name, $Default = $null) {
    $p = $State.PSObject.Properties[$Name]
    if ($null -eq $p) { return $Default }
    return $p.Value
}
function Set-StateValue([hashtable]$Values) {
    $s = Read-State
    foreach ($k in $Values.Keys) {
        if ($null -eq $s.PSObject.Properties[$k]) { Add-Member -InputObject $s -NotePropertyName $k -NotePropertyValue $Values[$k] } else { $s.$k = $Values[$k] }
    }
    if (-not (Test-Path -LiteralPath $script:Root)) { [void](New-Item -ItemType Directory -Path $script:Root -Force) }
    [IO.File]::WriteAllText($script:StateFile, ($s | ConvertTo-Json -Depth 6), (New-Object Text.UTF8Encoding($true)))
}

function Ensure-Dirs {
    foreach ($d in @($script:Root, $script:BinDir, $script:Work, $script:Logs, $script:Downloads)) {
        if (-not (Test-Path -LiteralPath $d)) { [void](New-Item -ItemType Directory -Path $d -Force) }
    }
}

# ---------------------------------------------------------------------------------------------------------
# 运行外部程序（不经过 PowerShell 5.1 的参数改写；捕获输出按 UTF-8 解码）
# ---------------------------------------------------------------------------------------------------------
function ConvertTo-WinArg([string]$Arg) {
    if ($Arg.Length -gt 0 -and $Arg -notmatch '[\s"]') { return $Arg }
    $sb = New-Object Text.StringBuilder
    [void]$sb.Append('"')
    $bs = 0
    foreach ($c in $Arg.ToCharArray()) {
        if ($c -eq '\') { $bs++; continue }
        if ($c -eq '"') { [void]$sb.Append('\', $bs * 2 + 1); [void]$sb.Append('"'); $bs = 0; continue }
        if ($bs -gt 0) { [void]$sb.Append('\', $bs); $bs = 0 }
        [void]$sb.Append($c)
    }
    if ($bs -gt 0) { [void]$sb.Append('\', $bs * 2) }
    [void]$sb.Append('"')
    return $sb.ToString()
}

# Invoke-Native 程序 参数数组 [-TimeoutSec 秒] [-Inherit]：返回 @{ ExitCode; Out; Err }。-Inherit 时输出直接接到当前窗口。
function Invoke-Native([string]$File, [string[]]$ArgList = @(), [int]$TimeoutSec = 0, [switch]$Inherit) {
    $psi = New-Object Diagnostics.ProcessStartInfo
    $psi.FileName = $File
    $psi.Arguments = (($ArgList | ForEach-Object { ConvertTo-WinArg $_ }) -join ' ')
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    if (-not $Inherit) {
        $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
        # 标准输入也接上再马上关掉：没装 WSL 的电脑上 wsl.exe 会打印“请安装”并等用户按键，不关掉它会一直卡在桌面窗口里
        $psi.RedirectStandardInput = $true
        $psi.StandardOutputEncoding = [Text.Encoding]::UTF8; $psi.StandardErrorEncoding = [Text.Encoding]::UTF8
    }
    $p = New-Object Diagnostics.Process
    $p.StartInfo = $psi
    [void]$p.Start()
    $out = $null; $err = $null
    if (-not $Inherit) {
        try { $p.StandardInput.Close() } catch { }
        $out = $p.StandardOutput.ReadToEndAsync(); $err = $p.StandardError.ReadToEndAsync()
    }
    if ($TimeoutSec -gt 0) {
        if (-not $p.WaitForExit($TimeoutSec * 1000)) { try { $p.Kill() } catch { }; return @{ ExitCode = 124; Out = ''; Err = "超时（$TimeoutSec 秒）" } }
    } else { $p.WaitForExit() }
    $p.WaitForExit()
    $o = ''; $e = ''
    if (-not $Inherit) { $o = $out.Result; $e = $err.Result }
    return @{ ExitCode = $p.ExitCode; Out = $o; Err = $e }
}

# WSL 里运行（root，不经 shell）：Invoke-Wsl @('命令','参数'…)
function Invoke-Wsl([string[]]$ArgList, [int]$TimeoutSec = 120, [switch]$Inherit) {
    $a = @('-d', $Distro, '-u', 'root', '--exec') + $ArgList
    return (Invoke-Native 'wsl.exe' $a -TimeoutSec $TimeoutSec -Inherit:$Inherit)
}

# Windows 路径 → WSL 里的路径（C:\Users\x → /mnt/c/Users/x）
function ConvertTo-WslPath([string]$Path) {
    $p = $Path -replace '\\', '/'
    if ($p -match '^([A-Za-z]):/?(.*)$') { return ('/mnt/' + $Matches[1].ToLower() + '/' + $Matches[2]) }
    return $p
}

function Format-Size([double]$Bytes) {
    if ($Bytes -ge 1GB) { return ('{0:N1} GB' -f ($Bytes / 1GB)) }
    if ($Bytes -ge 1MB) { return ('{0:N1} MB' -f ($Bytes / 1MB)) }
    return ('{0:N0} KB' -f ($Bytes / 1KB))
}

# ---------------------------------------------------------------------------------------------------------
# 嵌入：下载网关的 PowerShell 客户端（构建时 scripts/build-release.sh 把 windows\afx-gw-client.ps1 的内容放到这里；
# 源码里没有嵌入时，从脚本旁边的 windows\ 目录读）
# ---------------------------------------------------------------------------------------------------------
$env:AFX_GW_NO_CLI = '1'
#@@AFX_GW_CLIENT@@
if (-not (Get-Command Request-AfxDownload -ErrorAction SilentlyContinue)) {
    $gwClient = Join-Path $PSScriptRoot 'windows\afx-gw-client.ps1'
    if (Test-Path -LiteralPath $gwClient) { . $gwClient }
}

# ---------------------------------------------------------------------------------------------------------
# 系统探测（只读）
# ---------------------------------------------------------------------------------------------------------
function Get-WindowsBuild { return [int](Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion').CurrentBuild }

# 当前账号是不是管理员账号（UAC 开着时没提权的管理员账号，组里也有 S-1-5-32-544，只是标成「仅拒绝」）
function Test-AdminAccount {
    # WindowsIdentity.Groups 不列出 UAC 过滤后“仅拒绝”的 Administrators，会把没提权的管理员账号当成普通账号；whoami 两种都列
    try { return [bool]((& whoami.exe /groups /fo csv /nh 2>$null) -match 'S-1-5-32-544') } catch { return $false }
}
function Test-Elevated {
    try { return (New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator) } catch { return $false }
}

function Get-WslVersionText {
    # wsl --version 成功才有版本；旧版 wsl.exe 没有这个参数（输出是别的内容）
    try {
        $r = Invoke-Native 'wsl.exe' @('--version') -TimeoutSec 8
        if ($r.ExitCode -eq 0 -and $r.Out -match 'WSL[^\d]*(\d+\.\d+\.\d+(\.\d+)?)') { return $Matches[1] }
        if ($r.ExitCode -eq 0 -and $r.Out -match '(\d+\.\d+\.\d+(\.\d+)?)') { return $Matches[1] }
    } catch { }
    return ''
}
function Test-VmpEnabled {
    try { return ((Get-CimInstance Win32_OptionalFeature -Filter "Name='VirtualMachinePlatform'" -ErrorAction Stop).InstallState -eq 1) } catch { return $false }
}
# WSL 能用：wsl --version 成功、版本不低于 2.0，并且「虚拟机平台」已打开（装了 WSL 程序但没开这个功能时 wsl --version 照样成功）
function Get-WslReadiness {
    $v = Get-WslVersionText
    $vmp = Test-VmpEnabled
    $old = $false
    if ($v) { try { $old = ([version]($v -replace '^(\d+\.\d+\.\d+).*$', '$1')) -lt [version]'2.0.0' } catch { } }
    return [pscustomobject]@{ version = $v; vmp = $vmp; old = $old; ready = ($v -ne '' -and $vmp -and -not $old) }
}

function Get-DistroList {
    $r = Invoke-Native 'wsl.exe' @('--list', '--quiet') -TimeoutSec 30
    if ($r.ExitCode -ne 0) { return @() }
    return @(($r.Out -replace "`0", '') -split "`r?`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ })
}
function Test-DistroExists { return (@(Get-DistroList) -contains $Distro) }
function Test-DistroRunning {
    $r = Invoke-Native 'wsl.exe' @('--list', '--running', '--quiet') -TimeoutSec 30
    if ($r.ExitCode -ne 0) { return $false }
    return (@(($r.Out -replace "`0", '') -split "`r?`n" | ForEach-Object { $_.Trim() }) -contains $Distro)
}
function Test-AcornFoxInDistro {
    $r = Invoke-Wsl @('/usr/local/bin/acornfox', 'version') -TimeoutSec 30
    return ($r.ExitCode -eq 0)
}
function Get-DistroAcornFoxVersion {
    $r = Invoke-Wsl @('/usr/local/bin/acornfox', 'version') -TimeoutSec 30
    if ($r.ExitCode -eq 0 -and $r.Out -match '(\d+\.\d+\.\d+[-\w.]*)') { return $Matches[1] }
    return ''
}
# WSL 里 eth0 的内部地址（NAT 网络下每次启动会变）
function Get-WslIp {
    $r = Invoke-Wsl @('ip', '-4', '-o', 'addr', 'show', 'dev', 'eth0', 'scope', 'global') -TimeoutSec 20
    if ($r.ExitCode -eq 0 -and $r.Out -match 'inet (\d+\.\d+\.\d+\.\d+)/') { return $Matches[1] }
    return ''
}

# 这台电脑在局域网里的地址：有默认网关、在线、不是虚拟网卡（WSL、Hyper-V、代理的 TUN、VPN、Docker…）的私网 IPv4
function Get-WinLanIp {
    try {
        $bad = 'vEthernet|WSL|sing-box|tun|tap|wintun|vpn|Hyper-V|VMware|VirtualBox|Docker|Clash|Tailscale|ZeroTier|Loopback|Bluetooth'
        foreach ($c in @(Get-NetIPConfiguration -ErrorAction Stop)) {
            if (-not $c.IPv4DefaultGateway -or -not $c.IPv4Address) { continue }
            if ($c.NetAdapter.Status -ne 'Up') { continue }
            if (("$($c.InterfaceAlias) $($c.NetAdapter.InterfaceDescription)") -match $bad) { continue }
            foreach ($a in @($c.IPv4Address)) {
                $ip = $a.IPAddress
                if ($ip -match '^(10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)' -and $ip -notmatch '^(198\.1[89]\.)') { return $ip }
            }
        }
    } catch { }
    return ''
}
# 局域网网卡的网络类型：Public（公用）时 Windows 防火墙会挡住局域网设备
function Get-LanNetworkCategory([string]$Ip) {
    if (-not $Ip) { return '' }
    try {
        $a = (Get-NetIPAddress -IPAddress $Ip -ErrorAction Stop).InterfaceAlias
        return [string](Get-NetConnectionProfile -InterfaceAlias $a -ErrorAction Stop).NetworkCategory
    } catch { return '' }
}

function Get-SystemDriveFreeGB {
    try { return [math]::Round((Get-PSDrive -Name ($env:SystemDrive.TrimEnd(':')) -ErrorAction Stop).Free / 1GB, 1) } catch { return 999 }
}

# 杀毒软件（只做记录，不影响流程）：安全中心的列表加进程名（装了 360 以后火绒会从列表里消失，但进程还在）
function Get-AntivirusNote {
    $names = @()
    try { $names += @(Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct -ErrorAction Stop | ForEach-Object { $_.displayName }) } catch { }
    foreach ($p in @{ 'HipsDaemon' = '火绒'; '360Tray' = '360'; 'ZhuDongFangYu' = '360' }.GetEnumerator()) {
        if (Get-Process -Name $p.Key -ErrorAction SilentlyContinue) { $names += $p.Value }
    }
    return (($names | Select-Object -Unique) -join '、')
}

function Test-ReleaseGatewayEnabled {
    if ($NoGateway) { return $false }
    return ((Get-GatewayUrl) -ne '')
}
function Get-GatewayUrl {
    if ($Gateway) { return $Gateway.TrimEnd('/') }
    $e = [Environment]::GetEnvironmentVariable('ACORNFOX_GATEWAY')
    if ($null -ne $e) { return $e.TrimEnd('/') }   # 设成空值 = 不联系网关
    return $script:GatewayDefault
}

# ---------------------------------------------------------------------------------------------------------
# 检查（下载之前，只读）：不满足就带故障编号停下
# ---------------------------------------------------------------------------------------------------------
function Invoke-Preflight {
    $r = [ordered]@{}
    if ($PSVersionTable.PSVersion -lt [version]'5.1') { Stop-WithFault 'WIN-PS-01' "PowerShell $($PSVersionTable.PSVersion)" }
    $curl = Join-Path $env:SystemRoot 'System32\curl.exe'
    if (-not (Test-Path -LiteralPath $curl)) { Stop-WithFault 'WIN-PS-01' '没有 curl.exe（Windows 10 1803 以上自带）' }
    $build = Get-WindowsBuild
    $r.build = $build
    if ($build -lt 19045) { Stop-WithFault 'WIN-OS-01' "系统版本号 $build（要 19045 以上）" }
    $arch = $env:PROCESSOR_ARCHITEW6432; if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    $r.arch = $arch
    if ($arch -ne 'AMD64') { Stop-WithFault 'WIN-ARCH-01' "处理器架构 $arch" }
    $blocked = ''
    foreach ($k in @('HKLM:\SOFTWARE\Policies\WSL', 'HKLM:\SOFTWARE\Policies\Microsoft\WSL')) {
        $v = $null
        try { $v = (Get-ItemProperty -LiteralPath $k -ErrorAction Stop).AllowWSL } catch { }
        if ($null -ne $v -and [int]$v -eq 0) { $blocked = $k }
    }
    if ($blocked) { Stop-WithFault 'WIN-POLICY-01' "$blocked 的 AllowWSL 是 0" }
    # 虚拟化：固件里没开（VirtualizationFirmwareEnabled 为假）并且系统里也没有 Hypervisor 在运行，才算没开
    $hv = $null; $fw = $null
    try {
        $hv = [bool](Get-CimInstance Win32_ComputerSystem -ErrorAction Stop).HypervisorPresent
        $fw = [bool](Get-CimInstance Win32_Processor -ErrorAction Stop | Select-Object -First 1).VirtualizationFirmwareEnabled
    } catch { }
    $r.virtualization = $(if ($null -eq $hv) { $null } else { ($hv -or $fw) })
    if ($null -ne $hv -and -not $hv -and -not $fw) { Stop-WithFault 'WIN-VIRT-01' 'VirtualizationFirmwareEnabled 和 HypervisorPresent 都是 False' }
    $free = Get-SystemDriveFreeGB
    $r.free_gb = $free
    $need = $script:MinFreeGB
    if ($free -lt $need) { Stop-WithFault 'WIN-DISK-01' "系统盘剩 $free GB，要 $need GB 以上" }
    $r.admin_account = (Test-AdminAccount)
    $r.antivirus = (Get-AntivirusNote)
    $w = Get-WslReadiness
    $r.wsl_version = $w.version; $r.vmp = $w.vmp; $r.wsl_ready = $w.ready
    Write-Log ("preflight " + ($r | ConvertTo-Json -Compress))
    return [pscustomobject]$r
}

# ---------------------------------------------------------------------------------------------------------
# 下载：网关先，官方地址后；分段多路（P-844）、可断点续传（已下好的段留着）、最后核对 SHA256
# ---------------------------------------------------------------------------------------------------------
function Get-FileSha256([string]$Path) {
    $h = [Security.Cryptography.SHA256]::Create()
    $fs = [IO.File]::OpenRead($Path)
    try { return ([BitConverter]::ToString($h.ComputeHash($fs)).Replace('-', '').ToLowerInvariant()) } finally { $fs.Dispose(); $h.Dispose() }
}

# 一个分段：curl.exe 下载 [from, to] 到临时文件。返回 Process（调用方收尾）
function Start-Segment([string]$Url, [long]$From, [long]$To, [string]$Tmp, [long]$MaxBytes) {
    $curl = Join-Path $env:SystemRoot 'System32\curl.exe'
    $a = @('-sS', '-f', '-L', '--connect-timeout', '15', '--max-time', '600', '--speed-limit', '10240', '--speed-time', '30',
        '-r', "$From-$To", '--max-filesize', "$MaxBytes", '-o', $Tmp, $Url)
    $psi = New-Object Diagnostics.ProcessStartInfo
    $psi.FileName = $curl
    $psi.Arguments = (($a | ForEach-Object { ConvertTo-WinArg $_ }) -join ' ')
    $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardError = $true
    $p = New-Object Diagnostics.Process
    $p.StartInfo = $psi
    [void]$p.Start()
    return $p
}

# 分段多路下载。下载地址由 $Source 现取（网关给的地址会过期，失败时重新要）。
# 路数：先 AFX_WIN_DL_PARALLEL（默认 16），速度还涨就加到 32，失败多或不再涨就退回；服务器不认 Range 时退回单路续传。
# 返回 $true/$false；失败时已下好的段留在 <文件>.parts\ 里，下次接着用。
function Get-SourceUrl($Source, [hashtable]$F) {
    if ($Source.Gw) { return (Get-GatewayDownloadUrl $F) }
    return $Source.Url
}

function Invoke-SegmentedDownload($Source, [hashtable]$F, [string]$Out) {
    $Size = [long]$F.Size; $Sha256 = [string]$F.Sha256; $Title = [string]$F.Title
    $segSize = 4MB
    $n = [int][math]::Ceiling($Size / [double]$segSize)
    $partsDir = "$Out.parts"
    [void](New-Item -ItemType Directory -Path $partsDir -Force)
    $start = 16
    if ($env:AFX_WIN_DL_PARALLEL -match '^\d+$' -and [int]$env:AFX_WIN_DL_PARALLEL -gt 0) { $start = [math]::Min(32, [int]$env:AFX_WIN_DL_PARALLEL) }
    $target = $start
    $pending = New-Object System.Collections.Queue
    $doneBytes = [long]0; $doneCount = 0
    $attempts = @{}
    for ($i = 0; $i -lt $n; $i++) {
        $len = [math]::Min([long]$segSize, $Size - [long]$i * $segSize)
        $pf = Join-Path $partsDir ("p{0:D5}" -f $i)
        if ((Test-Path -LiteralPath $pf) -and (Get-Item -LiteralPath $pf).Length -eq $len) { $doneBytes += $len; $doneCount++ } else { $pending.Enqueue($i) }
    }
    if ($doneCount -gt 0) { Write-Log "resume $Title 已有 $doneCount/$n 段" }
    $url = Get-SourceUrl $Source $F
    if (-not $url) { return $false }
    $urlAt = Get-Date
    $running = New-Object System.Collections.ArrayList
    $t0 = Get-Date; $lastShow = Get-Date; $lastAdapt = Get-Date; $adaptBase = [long]0; $stage = 0
    $speedAt16 = 0.0; $startedInStage = 0; $failedInStage = 0; $noRange = $false; $abort = $false
    $interactive = -not [Console]::IsOutputRedirected
    while (($pending.Count -gt 0 -or $running.Count -gt 0) -and -not $abort) {
        # 收尾
        foreach ($w in @($running)) {
            if (-not $w.P.HasExited) { continue }
            [void]$running.Remove($w)
            $len = [math]::Min([long]$segSize, $Size - [long]$w.Idx * $segSize)
            $ok = ($w.P.ExitCode -eq 0 -and (Test-Path -LiteralPath $w.Tmp) -and (Get-Item -LiteralPath $w.Tmp).Length -eq $len)
            if ($ok) {
                Move-Item -LiteralPath $w.Tmp -Destination $w.Part -Force
                $doneBytes += $len; $doneCount++
            } else {
                $rc = $w.P.ExitCode
                try { $e = $w.P.StandardError.ReadToEnd() } catch { $e = '' }
                Write-Log "segment $($w.Idx) 失败 curl=$rc $($e.Trim())"
                Remove-Item -LiteralPath $w.Tmp -Force -ErrorAction SilentlyContinue
                $failedInStage++
                if ($rc -eq 63 -or ($rc -eq 0 -and (Test-Path -LiteralPath $w.Tmp))) { $noRange = $true; $abort = $true; continue }
                $attempts[$w.Idx] = 1 + [int]$(if ($attempts.ContainsKey($w.Idx)) { $attempts[$w.Idx] } else { 0 })
                if ($attempts[$w.Idx] -ge 6) { Write-Log "segment $($w.Idx) 试了 6 次还是不行，放弃这个来源"; $abort = $true; continue }
                $pending.Enqueue($w.Idx)
                # 地址可能过期（HTTP 4xx，curl 退出码 22）：隔 20 秒以上重新要一次
                if ($rc -eq 22 -and ((Get-Date) - $urlAt).TotalSeconds -gt 20) {
                    $nu = Get-SourceUrl $Source $F
                    if ($nu) { $url = $nu; $urlAt = Get-Date }
                }
            }
        }
        if ($abort) { break }
        # 补满
        while ($running.Count -lt $target -and $pending.Count -gt 0) {
            $i = [int]$pending.Dequeue()
            $from = [long]$i * $segSize
            $len = [math]::Min([long]$segSize, $Size - $from)
            $part = Join-Path $partsDir ("p{0:D5}" -f $i)
            $tmp = "$part.tmp"
            Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue
            $p = Start-Segment $url $from ($from + $len - 1) $tmp $len
            [void]$running.Add([pscustomobject]@{ P = $p; Idx = $i; Tmp = $tmp; Part = $part })
            $startedInStage++
        }
        # 调整路数
        $now = Get-Date
        if (($now - $lastAdapt).TotalSeconds -ge 12 -and $pending.Count -gt $target) {
            $inflight = [long]0
            foreach ($w in $running) { if (Test-Path -LiteralPath $w.Tmp) { $inflight += (Get-Item -LiteralPath $w.Tmp).Length } }
            $cur = $doneBytes + $inflight
            $speed = ($cur - $adaptBase) / ($now - $lastAdapt).TotalSeconds
            $failRate = $(if ($startedInStage -gt 0) { $failedInStage / [double]$startedInStage } else { 0 })
            if ($failRate -gt 0.3 -and $target -gt 4) {
                $target = [math]::Max(4, [int]($target / 2)); $stage = 9
                Write-Log "download 失败多（$([int]($failRate*100))%），退到 $target 路"
            } elseif ($stage -eq 0 -and $start -lt 32) {
                $speedAt16 = $speed; $target = 32; $stage = 1
                Write-Log ("download {0} 路速度 {1}/s，试 32 路" -f $start, (Format-Size $speed))
            } elseif ($stage -eq 1) {
                if ($speed -lt $speedAt16 * 1.15) { $target = $start; Write-Log ("download 32 路速度 {0}/s 没有明显变快，退回 {1} 路" -f (Format-Size $speed), $start) }
                else { Write-Log ("download 32 路速度 {0}/s，保持 32 路" -f (Format-Size $speed)) }
                $stage = 2
            }
            $adaptBase = $cur; $lastAdapt = $now; $startedInStage = 0; $failedInStage = 0
        }
        # 进度
        if (($now - $lastShow).TotalSeconds -ge $(if ($interactive) { 2 } else { 15 })) {
            $inflight = [long]0
            foreach ($w in $running) { if (Test-Path -LiteralPath $w.Tmp) { $inflight += (Get-Item -LiteralPath $w.Tmp).Length } }
            $cur = $doneBytes + $inflight
            $sp = $cur / [math]::Max(1.0, ($now - $t0).TotalSeconds)
            $line = ("  {0}：{1} / {2}（{3:N0}%），平均 {4}/s，{5} 路" -f $Title, (Format-Size $cur), (Format-Size $Size), (100.0 * $cur / $Size), (Format-Size $sp), $running.Count)
            if ($interactive) { Write-Host ("`r" + $line.PadRight(100)) -NoNewline } else { Write-Host $line }
            $lastShow = $now
        }
        Start-Sleep -Milliseconds 200
    }
    foreach ($w in @($running)) { try { $w.P.Kill() } catch { } }
    if ($interactive) { Write-Host '' }
    if ($noRange) {
        Write-Log '服务器不支持分段（Range），改单路续传'
        Remove-Item -LiteralPath $partsDir -Recurse -Force -ErrorAction SilentlyContinue
        $u = Get-SourceUrl $Source $F
        if (-not $u) { return $false }
        $r = Invoke-Native (Join-Path $env:SystemRoot 'System32\curl.exe') @('-sS', '-f', '-L', '-C', '-', '--retry', '3', '--connect-timeout', '15', '-o', "$Out.single", $u) -TimeoutSec 7200
        if ($r.ExitCode -ne 0 -or -not (Test-Path -LiteralPath "$Out.single")) { Write-Log "单路下载失败 curl=$($r.ExitCode) $($r.Err.Trim())"; return $false }
        Move-Item -LiteralPath "$Out.single" -Destination "$Out.tmp" -Force
    } else {
        if ($abort -or $pending.Count -gt 0) { return $false }
        # 拼起来
        $fs = [IO.File]::Create("$Out.tmp")
        try {
            for ($i = 0; $i -lt $n; $i++) {
                $b = [IO.File]::ReadAllBytes((Join-Path $partsDir ("p{0:D5}" -f $i)))
                $fs.Write($b, 0, $b.Length)
            }
        } finally { $fs.Dispose() }
    }
    $got = Get-FileSha256 "$Out.tmp"
    if ($got -ne $Sha256.ToLowerInvariant()) {
        Write-Log "校验不对 $Title 要 $Sha256 得到 $got"
        Remove-Item -LiteralPath "$Out.tmp" -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $partsDir -Recurse -Force -ErrorAction SilentlyContinue
        return $false
    }
    Move-Item -LiteralPath "$Out.tmp" -Destination $Out -Force
    Remove-Item -LiteralPath $partsDir -Recurse -Force -ErrorAction SilentlyContinue
    return $true
}

$script:GatewayAnnounced = $false
$script:GatewayLastCode = ''
# 向网关要某个文件的临时下载地址；要不到返回 $null（原因记在日志里，调用方换下一个来源）
function Get-GatewayDownloadUrl([hashtable]$F) {
    if (-not (Test-ReleaseGatewayEnabled)) { return $null }
    $gw = Get-GatewayUrl
    if (-not $script:GatewayAnnounced) {
        Say '会向 AcornFox 下载网关登记一个随机编号（不读硬件、不收用户名），用来领取临时下载地址；不想用可以加 -NoGateway。'
        $script:GatewayAnnounced = $true
    }
    $env:AFX_GW_LOG = Join-Path $script:Logs 'gw-client.log'
    $env:AFX_GW_VIA = 'install-windows'
    $k = New-AfxMachineKey
    if (-not $k.ok) { Write-Log "网关：钥匙出错 $($k.code)"; return $null }
    $r = Request-AfxDownload -Gateway $gw -FileId $F.Id
    if (-not $r.ok) { $script:GatewayLastCode = [string]$r.code; Write-Log "网关：没有给地址 code=$($r.code) http=$($r.http_status)"; return $null }
    $sha = Get-AfxProp $r 'sha256'
    if ($sha -and ([string]$sha).ToLowerInvariant() -ne $F.Sha256) { Write-Log '网关：返回的 sha256 和写死的官方值不一致，不用这个来源'; return $null }
    return [string]$r.url
}

# 本机已经有校验对的文件吗：大小对、SHA256 对才算。算一遍 SHA256 在慢机器上要几十秒（杀毒软件也在扫），所以校验过的记在
# <文件>.ok 里（大小、修改时间、校验值）；文件没变就不再算。管理员那一步会对复制进去的副本再核对一次，不靠这个记录。
function Test-CachedFile([string]$Path, [hashtable]$F) {
    if (-not (Test-Path -LiteralPath $Path)) { return $false }
    $it = Get-Item -LiteralPath $Path
    if ($it.Length -ne $F.Size) { return $false }
    $side = "$Path.ok"
    $stamp = "$($it.Length)|$($it.LastWriteTimeUtc.Ticks)|$($F.Sha256)"
    if ((Test-Path -LiteralPath $side) -and ((Get-Content -LiteralPath $side -Raw).Trim() -eq $stamp)) { return $true }
    if ((Get-FileSha256 $Path) -eq $F.Sha256) { Set-Content -LiteralPath $side -Value $stamp -Encoding ASCII; return $true }
    return $false
}

# 取一个大文件（已经有校验对的就直接用）。返回本地路径；所有来源都不行就带故障编号停下。
function Get-AfxFile([string]$Key) {
    $F = $script:Files[$Key]
    $out = Join-Path $script:Downloads $F.Name
    if (Test-CachedFile $out $F) {
        Say "$($F.Title)：本机已经有校验对的文件，不用下载"
        return $out
    }
    if (Test-Path -LiteralPath $out) { Remove-Item -LiteralPath $out -Force }
    Say "下载 $($F.Title)"
    $sources = @()
    if (Test-ReleaseGatewayEnabled) { $sources += @{ Name = 'AcornFox 下载网关'; Gw = $true } }
    foreach ($u in $F.Urls) { $sources += @{ Name = ([Uri]$u).Host; Gw = $false; Url = $u } }
    $tried = @()
    foreach ($s in $sources) {
        $t0 = Get-Date
        $ok = $false
        try { $ok = Invoke-SegmentedDownload $s $F $out } catch { Write-Log "下载出错 $($s.Name)：$($_.Exception.Message)" }
        $sec = [int]((Get-Date) - $t0).TotalSeconds
        if ($ok) {
            $it = Get-Item -LiteralPath $out
            Set-Content -LiteralPath "$out.ok" -Value "$($it.Length)|$($it.LastWriteTimeUtc.Ticks)|$($F.Sha256)" -Encoding ASCII
            Say ("{0}：下载完成，来源 {1}，用时 {2} 秒" -f $F.Title, $s.Name, $sec)
            Add-Step "download-$Key" 'done' "来源=$($s.Name) 用时=${sec}s"
            return $out
        }
        $tried += $s.Name
        SayWarn ("{0} 没下成（{1} 秒），换下一个来源" -f $s.Name, $sec)
    }
    Stop-WithFault 'WIN-DL-01' ("$($F.Title) 试过：" + ($tried -join '、'))
}

# ---------------------------------------------------------------------------------------------------------
# 局域网转发的同步循环（写到 %ProgramData%\AcornFox\bin\，只有管理员能改；由计划任务 AcornFoxLAN 以管理员权限运行）
# 为什么放在管理员才能写的地方：它每隔几秒以管理员权限运行，普通用户能改的文件不能交给它运行。
# ---------------------------------------------------------------------------------------------------------
$script:SyncScriptText = @'
# AcornFox 局域网转发同步（计划任务 AcornFoxLAN 运行；不用手工运行）。
# 做什么：每 5 秒看一次 WSL 里哪些端口监听在所有网卡（0.0.0.0）上，再问 AcornFox 哪些应用是「只本机」（acornfox apps --json 里
#   local_only 为真；WSL 里的 acornfox-server 带 --wsl 运行，所以只本机的应用也监听所有网卡，不然 Windows 自己用 localhost
#   打开它们会因为 WSL 的转发缺陷打不开），然后用 netsh portproxy 把「局域网能打开」的应用和局域网控制台在 Windows 的
#   0.0.0.0:端口 转到 WSL 的内部地址；只本机的应用没有规则，局域网里的设备连不上，Windows 自己用 localhost 照常打开。
#   只管 18802 和 18810-18899；WSL 的内部地址变了（WSL 重启后会变）就改规则；问不到应用信息时只管 18802，不动别的规则。
# 启动时先拿掉自己管的全部规则，再叫起 WSL：规则要在 WSL 把 localhost 转发建好之后再加，否则 Windows 自己用 localhost 打不开。
# 停下：schtasks /end /tn AcornFoxLAN；彻底去掉：运行 install-windows.ps1 lan-off 或 uninstall。记录：%LOCALAPPDATA%\AcornFox\logs\lan-sync.log
param([string]$Distro = 'AcornFox', [int]$Interval = 5, [switch]$Once)
$ErrorActionPreference = 'Continue'
$env:WSL_UTF8 = '1'
$LogFile = Join-Path $env:LOCALAPPDATA 'AcornFox\logs\lan-sync.log'
function L([string]$m) {
    try {
        $d = Split-Path -Parent $LogFile
        if (-not (Test-Path $d)) { New-Item -ItemType Directory -Path $d -Force | Out-Null }
        if ((Test-Path $LogFile) -and (Get-Item $LogFile).Length -gt 524288) { Remove-Item $LogFile -Force }
        Add-Content -Path $LogFile -Value ("{0} {1}" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $m)
    } catch { }
}
function Test-Managed([int]$p) { return ($p -eq 18802 -or ($p -ge 18810 -and $p -le 18899)) }
function Run([string[]]$a, [int]$sec = 20) {
    $psi = New-Object Diagnostics.ProcessStartInfo
    $psi.FileName = 'wsl.exe'
    $psi.Arguments = (($a | ForEach-Object { if ($_ -match '[\s"]') { '"' + ($_ -replace '"', '\"') + '"' } else { $_ } }) -join ' ')
    $psi.UseShellExecute = $false; $psi.CreateNoWindow = $true
    $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true; $psi.RedirectStandardInput = $true
    $psi.StandardOutputEncoding = [Text.Encoding]::UTF8
    $p = New-Object Diagnostics.Process; $p.StartInfo = $psi
    [void]$p.Start()
    try { $p.StandardInput.Close() } catch { }
    $o = $p.StandardOutput.ReadToEndAsync(); $e = $p.StandardError.ReadToEndAsync()
    if (-not $p.WaitForExit($sec * 1000)) { try { $p.Kill() } catch { }; return '' }
    return $o.Result
}
function Get-Rules {
    $rules = @{}
    $t = (& netsh.exe interface portproxy show v4tov4) -join "`n"
    foreach ($m in [regex]::Matches($t, '(?m)^\s*(\S+)\s+(\d+)\s+(\S+)\s+(\d+)\s*$')) {
        $port = [int]$m.Groups[2].Value
        if ($m.Groups[1].Value -eq '0.0.0.0' -and (Test-Managed $port)) { $rules[$port] = $m.Groups[3].Value }
    }
    return $rules
}
function Remove-Rule([int]$port) { & netsh.exe interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=$port | Out-Null }
function Add-Rule([int]$port, [string]$ip) { & netsh.exe interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=$port connectaddress=$ip connectport=$port | Out-Null }
function Test-Running { return (@(((Run @('--list', '--running', '--quiet')) -replace "`0", '') -split "`r?`n" | ForEach-Object { $_.Trim() }) -contains $Distro) }
# WSL 里监听在所有网卡上的端口（读 /proc/net/tcp、tcp6：状态 0A 是 LISTEN，本地地址全 0 是所有网卡）和 eth0 的地址
function Get-WslState {
    $o = Run @('-d', $Distro, '-u', 'root', '--exec', '/bin/sh', '-c', 'cat /proc/net/tcp /proc/net/tcp6; echo ==IP==; ip -4 -o addr show dev eth0 scope global 2>/dev/null || hostname -I; echo ==APPS==; /usr/local/bin/acornfox apps --json 2>/dev/null')
    if (-not $o) { return $null }
    $parts = $o -split '==IP=='
    $apps = @($parts[1] -split '==APPS==')
    $localPorts = New-Object 'System.Collections.Generic.HashSet[int]'
    $appsOk = $false
    if ($apps.Count -gt 1 -and $apps[1].Trim()) {
        try {
            $j = $apps[1].Trim() | ConvertFrom-Json
            if ($j.ok) {
                $appsOk = $true
                foreach ($a in @($j.apps)) {
                    if ($a.local_only -and "$($a.url)" -match ':(\d+)(/|$)') { [void]$localPorts.Add([int]$Matches[1]) }
                }
            }
        } catch { }
    }
    $ports = New-Object 'System.Collections.Generic.HashSet[int]'
    foreach ($line in ($parts[0] -split "`r?`n")) {
        $f = ($line.Trim() -split '\s+')
        if ($f.Count -lt 4 -or $f[3] -ne '0A') { continue }
        $a = $f[1] -split ':'
        if ($a.Count -eq 2 -and $a[0] -match '^0+$') { [void]$ports.Add([Convert]::ToInt32($a[1], 16)) }
    }
    $ip = ''
    if ($apps.Count -gt 0 -and $apps[0] -match '(\d+\.\d+\.\d+\.\d+)') { $ip = $Matches[1] }
    return [pscustomobject]@{ Ports = $ports; Ip = $ip; LocalPorts = $localPorts; AppsOk = $appsOk }
}
function Sync-Once {
    if (-not (Test-Running)) { return }
    $st = Get-WslState
    if (-not $st -or -not $st.Ip) { return }
    $have = Get-Rules
    foreach ($p in $st.Ports) {
        if (-not (Test-Managed $p)) { continue }
        if ($st.LocalPorts.Contains([int]$p)) { continue }             # 只本机的应用：不转出去
        if (-not $st.AppsOk -and $p -ne 18802) { continue }            # 问不到应用信息：不冒险转别的端口
        if (-not $have.ContainsKey($p) -or $have[$p] -ne $st.Ip) {
            if ($have.ContainsKey($p)) { Remove-Rule $p }
            Add-Rule $p $st.Ip
            L "转发 0.0.0.0:$p -> $($st.Ip):$p"
        }
    }
    foreach ($p in @($have.Keys)) {
        if ($st.LocalPorts.Contains([int]$p)) { Remove-Rule $p; L "去掉转发 $p（这个应用改成了只本机）"; continue }
        if (-not $st.AppsOk -and $p -ne 18802) { continue }
        if (-not $st.Ports.Contains([int]$p)) { Remove-Rule $p; L "去掉转发 $p（WSL 里已经不监听）" }
    }
}
if (-not $Once) {
    # 只留一个同步在运行：旧版本留下的实例和新实例同时跑会互相改规则
    $mutex = New-Object Threading.Mutex($false, 'Global\AcornFoxLanSync')
    $got = $false
    try { $got = $mutex.WaitOne(0) } catch [Threading.AbandonedMutexException] { $got = $true }
    if (-not $got) { L '已经有一个同步在运行，这个退出'; exit 0 }
}
L "启动：先拿掉自己管的转发规则，再叫起 $Distro"
foreach ($p in @((Get-Rules).Keys)) { Remove-Rule $p }
[void](Run @('-d', $Distro, '--exec', '/bin/true') 120)
if ($Once) { Sync-Once; exit 0 }
$errs = 0
while ($true) {
    try { Sync-Once; $errs = 0 } catch { $errs++; if ($errs -le 3) { L "出错：$($_.Exception.Message)" } }
    Start-Sleep -Seconds $Interval
}
'@

# ---------------------------------------------------------------------------------------------------------
# 管理员那一步（一次管理员确认做完）：打开虚拟机平台、装 WSL、登记计划任务、防火墙、局域网转发同步任务；卸载时反过来拿掉
# 以 -EncodedCommand 把压缩后的代码直接交给提权的 PowerShell：没有放在用户能改的地方的脚本文件，提权之前和之后不会被换掉。
# ---------------------------------------------------------------------------------------------------------
$script:AdminTemplate = @'
$ErrorActionPreference = 'Stop'
$cfg = '@@CFG@@' | ConvertFrom-Json
function L([string]$m) {
    Write-Host "[AcornFox] $m"
    try { Add-Content -LiteralPath $cfg.logFile -Value ("{0} {1}" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $m) -Encoding UTF8 } catch { }
}
$res = [ordered]@{ ok = $true; reboot = $false; error = ''; done = @() }
$ports = @(18802) + (18810..18899)
function Remove-Managed {
    $t = (& netsh.exe interface portproxy show v4tov4) -join "`n"
    foreach ($m in [regex]::Matches($t, '(?m)^\s*(\S+)\s+(\d+)\s+(\S+)\s+(\d+)\s*$')) {
        $p = [int]$m.Groups[2].Value
        if ($ports -contains $p) { & netsh.exe interface portproxy delete v4tov4 listenaddress=$($m.Groups[1].Value) listenport=$p | Out-Null }
    }
}
function Lock-Dir([string]$d) {
    [void](New-Item -ItemType Directory -Path $d -Force)
    & icacls.exe $d /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-32-545:(OI)(CI)RX' | Out-Null
}
function Stop-Sync {
    # 注销计划任务不会结束它正在运行的那一次：先停任务，再结束还在跑的同步进程（旧版本的也算）
    Stop-ScheduledTask -TaskName $cfg.taskLan -ErrorAction SilentlyContinue
    foreach ($p in @(Get-CimInstance Win32_Process -Filter "Name = 'powershell.exe'" -ErrorAction SilentlyContinue)) {
        if ($p.CommandLine -match 'afx-lan-sync') { Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue }
    }
}
function New-Triggers {
    $t1 = New-ScheduledTaskTrigger -AtLogOn -User $cfg.user
    $t2 = New-ScheduledTaskTrigger -AtStartup
    $t2.Delay = 'PT20S'
    return @($t1, $t2)
}
try {
    L ("管理员步骤开始：" + ($cfg.todo -join '、'))
    foreach ($job in $cfg.todo) {
        switch ($job) {
            'wsl' {
                if ($cfg.msi) {
                    $dir = Join-Path $env:ProgramData 'AcornFox\installer'
                    Lock-Dir $dir
                    Copy-Item -LiteralPath $cfg.msi -Destination (Join-Path $dir 'wsl.msi') -Force
                    $h = (Get-FileHash -LiteralPath (Join-Path $dir 'wsl.msi') -Algorithm SHA256).Hash.ToLower()
                    if ($h -ne $cfg.msiSha) { throw "WSL 安装包复制后校验不对：$h" }
                    L 'WSL 安装包校验通过（管理员目录里的副本）'
                }
                if ($cfg.enableVmp) {
                    L '打开 Windows 的「虚拟机平台」功能…'
                    & dism.exe /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart | Out-Null
                    $rc = $LASTEXITCODE
                    L "dism 退出码 $rc"
                    if ($rc -eq 3010) { $res.reboot = $true } elseif ($rc -ne 0) { throw "dism 退出码 $rc" }
                }
                if ($cfg.msi) {
                    L '安装 WSL…'
                    $p = Start-Process msiexec.exe -ArgumentList '/i', ('"' + (Join-Path $env:ProgramData 'AcornFox\installer\wsl.msi') + '"'), '/qn', '/norestart', '/l*v', ('"' + $cfg.msiLog + '"') -Wait -PassThru
                    L "msiexec 退出码 $($p.ExitCode)"
                    if ($p.ExitCode -in 3010, 1641) { $res.reboot = $true } elseif ($p.ExitCode -notin 0, 1638) { throw "msiexec 退出码 $($p.ExitCode)" }
                    Remove-Item -LiteralPath (Join-Path $env:ProgramData 'AcornFox\installer\wsl.msi') -Force -ErrorAction SilentlyContinue
                }
                $res.done += 'wsl'
            }
            'tasks' {
                $set = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew
                Stop-Sync
                if ($cfg.lan) {
                    $dir = Join-Path $env:ProgramData 'AcornFox\bin'
                    Lock-Dir (Join-Path $env:ProgramData 'AcornFox')
                    Lock-Dir $dir
                    [IO.File]::WriteAllText((Join-Path $dir 'afx-lan-sync.ps1'), $syncText, (New-Object Text.UTF8Encoding($true)))
                    Unregister-ScheduledTask -TaskName $cfg.taskWsl -Confirm:$false -ErrorAction SilentlyContinue
                    $act = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument ('-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "' + (Join-Path $dir 'afx-lan-sync.ps1') + '" -Distro ' + $cfg.distro)
                    $pr = New-ScheduledTaskPrincipal -UserId $cfg.user -LogonType S4U -RunLevel Highest
                    Register-ScheduledTask -TaskName $cfg.taskLan -Action $act -Trigger (New-Triggers) -Principal $pr -Settings $set -Force | Out-Null
                    Remove-NetFirewallRule -Name $cfg.fwRule -ErrorAction SilentlyContinue
                    New-NetFirewallRule -Name $cfg.fwRule -DisplayName 'AcornFox LAN (WSL)' -Direction Inbound -Protocol TCP -LocalPort 18802,18810-18899 -Action Allow -Profile Private,Domain | Out-Null
                    L '已登记局域网转发任务和防火墙规则（只在专用、域网络生效）'
                    try { Start-ScheduledTask -TaskName $cfg.taskLan } catch { L "马上启动任务没成功：$($_.Exception.Message)" }
                } else {
                    Unregister-ScheduledTask -TaskName $cfg.taskLan -Confirm:$false -ErrorAction SilentlyContinue
                    Remove-Managed
                    Remove-NetFirewallRule -Name $cfg.fwRule -ErrorAction SilentlyContinue
                    $act = New-ScheduledTaskAction -Execute (Join-Path $env:SystemRoot 'System32\wsl.exe') -Argument ('-d ' + $cfg.distro + ' --exec /bin/true')
                    $pr = New-ScheduledTaskPrincipal -UserId $cfg.user -LogonType S4U -RunLevel Limited
                    Register-ScheduledTask -TaskName $cfg.taskWsl -Action $act -Trigger (New-Triggers) -Principal $pr -Settings $set -Force | Out-Null
                    L '已登记开机自启任务（只本机，没有局域网转发）'
                }
                $res.done += 'tasks'
            }
            'remove' {
                Stop-Sync
                Unregister-ScheduledTask -TaskName $cfg.taskLan -Confirm:$false -ErrorAction SilentlyContinue
                Unregister-ScheduledTask -TaskName $cfg.taskWsl -Confirm:$false -ErrorAction SilentlyContinue
                Remove-Managed
                Remove-NetFirewallRule -Name $cfg.fwRule -ErrorAction SilentlyContinue
                Remove-Item -LiteralPath (Join-Path $env:ProgramData 'AcornFox') -Recurse -Force -ErrorAction SilentlyContinue
                L '已拿掉计划任务、转发规则、防火墙规则和管理员目录'
                $res.done += 'remove'
            }
        }
    }
    L '管理员步骤完成'
} catch {
    $res.ok = $false
    $res.error = $_.Exception.Message
    L ('出错：' + $_.Exception.Message)
}
try { [IO.File]::WriteAllText($cfg.resultFile, ($res | ConvertTo-Json -Compress), (New-Object Text.UTF8Encoding($false))) } catch { }
if (-not $res.ok) { Start-Sleep -Seconds 15 }
'@

# 运行管理员那一步。$Todo：'wsl'、'tasks'、'remove'。返回结果对象（ok、reboot、error、done）。
function Invoke-AdminStep([string[]]$Todo, [hashtable]$Extra = @{}) {
    $cfg = [ordered]@{
        todo = $Todo; user = [Security.Principal.WindowsIdentity]::GetCurrent().Name; distro = $Distro; taskWsl = $script:TaskWsl; taskLan = $script:TaskLan
        fwRule = $script:FirewallRule; lan = $false; logFile = $script:AdminLog; resultFile = $script:AdminResult
        msi = ''; msiSha = ''; msiLog = (Join-Path $script:Logs 'wsl-msi.log'); enableVmp = $false
    }
    foreach ($k in $Extra.Keys) { $cfg[$k] = $Extra[$k] }
    # 设置用纯文本放进去（单引号要写成两个），局域网同步脚本放在最前面：压缩才有效果，命令行才放得下（上限约 32000 个字符）
    $cfgText = ($cfg | ConvertTo-Json -Compress).Replace("'", "''")
    $code = "`$syncText = @'`n" + $script:SyncScriptText + "`n'@`n" + $script:AdminTemplate.Replace('@@CFG@@', $cfgText)
    # 压缩后再包一层，命令行才放得下
    $ms = New-Object IO.MemoryStream
    $gz = New-Object IO.Compression.GZipStream($ms, [IO.Compression.CompressionMode]::Compress)
    $b = [Text.Encoding]::UTF8.GetBytes($code); $gz.Write($b, 0, $b.Length); $gz.Close()
    $stub = '$b=[Convert]::FromBase64String(''' + [Convert]::ToBase64String($ms.ToArray()) + ''');$s=New-Object IO.MemoryStream(,$b);$g=New-Object IO.Compression.GZipStream($s,[IO.Compression.CompressionMode]::Decompress);$r=New-Object IO.StreamReader($g,[Text.Encoding]::UTF8);Invoke-Expression $r.ReadToEnd()'
    $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($stub))
    if ($enc.Length -gt 30000) { throw "管理员步骤的命令太长（$($enc.Length)）" }
    Remove-Item -LiteralPath $script:AdminResult -Force -ErrorAction SilentlyContinue
    Say '接下来会弹出一次管理员确认（窗口上写 Windows PowerShell、发布者 Microsoft Windows），请选「是」。'
    try {
        $p = Start-Process -FilePath 'powershell.exe' -Verb RunAs -Wait -PassThru -WindowStyle Normal -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-EncodedCommand', $enc)
    } catch {
        Write-Log "管理员确认没通过：$($_.Exception.Message)"
        Stop-WithFault 'WIN-ADMIN-01' $_.Exception.Message 5
    }
    if (-not (Test-Path -LiteralPath $script:AdminResult)) { Stop-WithFault 'WIN-ADMIN-02' '管理员那一步没有留下结果文件（可能被安全软件拦了）' }
    $res = Get-Content -LiteralPath $script:AdminResult -Raw -Encoding UTF8 | ConvertFrom-Json
    if (-not $res.ok) { Stop-WithFault 'WIN-ADMIN-02' ([string]$res.error) }
    return $res
}

# ---------------------------------------------------------------------------------------------------------
# Windows 上的 acornfox 命令：一个很小的 .cmd 加一个 .ps1，把命令原样转给 WSL 里的程序（不发 exe，不用签名）
# ---------------------------------------------------------------------------------------------------------
$script:WrapperCmd = @'
@echo off
rem AcornFox 命令行（Windows）：转给 WSL 里的 AcornFox 运行。由 install-windows.ps1 写入，升级时会被覆盖，不要手工改。
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0acornfox.ps1" %*
exit /b %ERRORLEVEL%
'@

$script:WrapperPs1 = @'
# AcornFox 命令行（Windows）：把命令转给 WSL 里的 AcornFox 程序运行。
# 由 install-windows.ps1 写入，升级时会被覆盖，不要手工改。
#   acornfox <命令…>              = WSL 里的 acornfox <命令…>（以 root 运行；C:\ 开头的路径自动换成 /mnt/c/…，当前目录一起带过去）
#   acornfox skill install        把 Skill 装到 Windows 用户目录里的 AI 工作台（Claude Code：~\.claude，Codex：~\.codex；--dir 指定别处）
#   acornfox windows <动作>        Windows 这一侧：status、doctor、upgrade、uninstall [--purge] [--yes]、lan-on、lan-off；加 --plan 只看不做，--json 输出 JSON
#   acornfox doctor               先查 Windows 这一侧，再查 WSL 里的 AcornFox（--json 时只输出 WSL 里那份，格式不变）
# 退出码：WSL 里命令的退出码；WSL 没起来是 1。出错先看：acornfox windows doctor。
$ErrorActionPreference = 'Stop'
$Distro = '@@DISTRO@@'
$Self = '@@SELF@@'
$env:WSL_UTF8 = '1'
$argv = @($args)

function ConvertTo-WinArg([string]$Arg) {
    if ($Arg.Length -gt 0 -and $Arg -notmatch '[\s"]') { return $Arg }
    $sb = New-Object Text.StringBuilder
    [void]$sb.Append('"')
    $bs = 0
    foreach ($c in $Arg.ToCharArray()) {
        if ($c -eq '\') { $bs++; continue }
        if ($c -eq '"') { [void]$sb.Append('\', $bs * 2 + 1); [void]$sb.Append('"'); $bs = 0; continue }
        if ($bs -gt 0) { [void]$sb.Append('\', $bs); $bs = 0 }
        [void]$sb.Append($c)
    }
    if ($bs -gt 0) { [void]$sb.Append('\', $bs * 2) }
    [void]$sb.Append('"')
    return $sb.ToString()
}
function ConvertTo-WslPath([string]$p) {
    if ($p -match '^([A-Za-z]):[\\/](.*)$') { return ('/mnt/' + $Matches[1].ToLower() + '/' + ($Matches[2] -replace '\\', '/')) }
    if ($p -match '^([A-Za-z]):$') { return ('/mnt/' + $Matches[1].ToLower()) }
    return $p
}
function Convert-Arg([string]$a) {
    if ($a -match '^([A-Za-z]):[\\/]') { return (ConvertTo-WslPath $a) }
    if ($a -match '^(-{1,2}[A-Za-z][\w-]*=)([A-Za-z]:[\\/].*)$') { return ($Matches[1] + (ConvertTo-WslPath $Matches[2])) }
    return $a
}
function Start-Wsl([string[]]$wslArgs, [switch]$Capture) {
    $psi = New-Object Diagnostics.ProcessStartInfo
    $psi.FileName = 'wsl.exe'
    $psi.Arguments = (($wslArgs | ForEach-Object { ConvertTo-WinArg $_ }) -join ' ')
    $psi.UseShellExecute = $false
    if ($Capture) { $psi.RedirectStandardOutput = $true; $psi.RedirectStandardInput = $true; $psi.StandardOutputEncoding = [Text.Encoding]::UTF8; $psi.CreateNoWindow = $true }
    $p = New-Object Diagnostics.Process
    $p.StartInfo = $psi
    [void]$p.Start()
    $out = $null
    if ($Capture) { try { $p.StandardInput.Close() } catch { }; $out = $p.StandardOutput.ReadToEnd() }
    $p.WaitForExit()
    return @{ ExitCode = $p.ExitCode; Out = $out }
}
# 保证发行版和 acornfox-server 起来了（电脑刚开机或 WSL 被关过后，服务要几秒才起来）
function Wait-Ready {
    $r = Start-Wsl @('-d', $Distro, '-u', 'root', '--exec', '/usr/bin/test', '-S', '/run/acornfox/api.sock') -Capture
    if ($r.ExitCode -eq 0) { return $true }
    [Console]::Error.WriteLine('[AcornFox] 正在启动 AcornFox（WSL 里的服务），请稍等…')
    for ($i = 0; $i -lt 40; $i++) {
        Start-Sleep -Seconds 2
        $r = Start-Wsl @('-d', $Distro, '-u', 'root', '--exec', '/usr/bin/test', '-S', '/run/acornfox/api.sock') -Capture
        if ($r.ExitCode -eq 0) { return $true }
    }
    return $false
}
function Invoke-Windows([string[]]$rest) {
    $map = @{ '--plan' = '-Plan'; '--json' = '-Json'; '--yes' = '-Yes'; '--purge' = '-Purge'; '--no-open' = '-NoOpen'; '--china' = '-China'; '--global' = '-Global' }
    $a = @()
    foreach ($x in $rest) { if ($map.ContainsKey($x)) { $a += $map[$x] } else { $a += $x } }
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $Self @a | Out-Host
    return $LASTEXITCODE
}

if ($argv.Count -ge 1 -and $argv[0] -eq 'windows') {
    if ($argv.Count -lt 2) { [Console]::Error.WriteLine('用法：acornfox windows status|doctor|upgrade|uninstall|lan-on|lan-off [--plan] [--json] [--yes] [--purge]'); exit 2 }
    exit (Invoke-Windows $argv[1..($argv.Count - 1)])
}
if ($argv.Count -ge 1 -and $argv[0] -eq 'uninstall') {
    [Console]::Error.WriteLine('Windows 上卸载用：acornfox windows uninstall（默认保留数据；加 --purge 连数据一起删）')
    exit 2
}
if ($argv.Count -ge 2 -and $argv[0] -eq 'skill' -and $argv[1] -eq 'install') {
    # Skill 内容在 WSL 里的程序里；装到 Windows 用户目录里 AI 工作台的 skills 目录
    $dir = ''; $json = $false
    for ($i = 2; $i -lt $argv.Count; $i++) {
        if ($argv[$i] -eq '--dir' -and $i + 1 -lt $argv.Count) { $dir = $argv[$i + 1]; $i++ }
        elseif ($argv[$i] -like '--dir=*') { $dir = $argv[$i].Substring(6) }
        elseif ($argv[$i] -eq '--json') { $json = $true }
        else { [Console]::Error.WriteLine('用法：acornfox skill install [--dir 目录] [--json]'); exit 2 }
    }
    $c = Start-Wsl @('-d', $Distro, '-u', 'root', '--exec', '/usr/local/bin/acornfox', 'skill', 'print') -Capture
    if ($c.ExitCode -ne 0 -or -not $c.Out) { [Console]::Error.WriteLine('读不到 Skill 内容：WSL 里的 AcornFox 没起来。先运行 acornfox windows doctor'); exit 1 }
    $targets = @()
    if ($dir) { $targets += [pscustomobject]@{ Name = '自定义目录'; Dir = $dir } }
    else {
        foreach ($t in @(@('Claude Code', '.claude'), @('Codex', '.codex'))) {
            $cfg = Join-Path $env:USERPROFILE $t[1]
            if (Test-Path -LiteralPath $cfg) { $targets += [pscustomobject]@{ Name = $t[0]; Dir = (Join-Path $cfg 'skills') } }
        }
    }
    if ($targets.Count -eq 0) {
        [Console]::Error.WriteLine('未检测到支持的 AI 工作台（Claude Code：~\.claude，Codex：~\.codex）；其他工具用 acornfox skill install --dir 指定 skills 目录')
        exit 2
    }
    $done = @(); $failed = 0
    foreach ($t in $targets) {
        try {
            $d = Join-Path $t.Dir 'acornfox'
            [void](New-Item -ItemType Directory -Path $d -Force)
            $path = Join-Path $d 'SKILL.md'
            [IO.File]::WriteAllText($path, $c.Out, (New-Object Text.UTF8Encoding($false)))
            $done += [pscustomobject]@{ workbench = $t.Name; path = $path }
            if (-not $json) { Write-Host "✓ $($t.Name)：已安装到 $path" }
        } catch { $failed++; if (-not $json) { [Console]::Error.WriteLine("✗ $($t.Name)：$($_.Exception.Message)") } }
    }
    if ($json) { Write-Output (ConvertTo-Json @{ installed = $done; failed = $failed } -Compress -Depth 4) }
    elseif ($done.Count -gt 0) { Write-Host '在 AI 工作台中说「用 acornfox 部署这个项目」即可。' }
    if ($done.Count -eq 0) { exit 1 }
    exit 0
}
if ($argv.Count -ge 1 -and $argv[0] -eq 'doctor' -and ($argv -notcontains '--json')) {
    $rc = Invoke-Windows @('doctor')
    Write-Host ''
    Write-Host '---------- WSL 里的 AcornFox ----------'
}

if (-not (Wait-Ready)) {
    [Console]::Error.WriteLine('AcornFox 没能起来（WSL 里的服务没有应答）。运行 acornfox windows doctor 看原因。')
    exit 1
}
$cwd = (Get-Location).ProviderPath
if ($cwd -notmatch '^[A-Za-z]:\\') { $cwd = $env:USERPROFILE }
$wslArgs = @('-d', $Distro, '-u', 'root', '--cd', $cwd, '--exec', '/usr/local/bin/acornfox') + @($argv | ForEach-Object { Convert-Arg ([string]$_) })
$oldEnc = $null
try { $oldEnc = [Console]::OutputEncoding; [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch { }
$r = Start-Wsl $wslArgs
try { if ($oldEnc) { [Console]::OutputEncoding = $oldEnc } } catch { }
exit $r.ExitCode
'@

# ---------------------------------------------------------------------------------------------------------
# 安装的各个步骤（每一步做过就跳过）
# ---------------------------------------------------------------------------------------------------------
function Write-LfFile([string]$Path, [string]$Text) {
    [IO.File]::WriteAllText($Path, ($Text -replace "`r`n", "`n"), (New-Object Text.UTF8Encoding($false)))
}

# 在 WSL 里运行一段 bash 脚本（写成文件再运行，不经 PowerShell 5.1 的参数改写）
function Invoke-WslScript([string]$Name, [string]$Body, [int]$TimeoutSec = 300, [switch]$Inherit) {
    $f = Join-Path $script:Work $Name
    Write-LfFile $f $Body
    return (Invoke-Wsl @('/bin/bash', (ConvertTo-WslPath $f)) -TimeoutSec $TimeoutSec -Inherit:$Inherit)
}

# %UserProfile%\.wslconfig：让 WSL 一直开着（否则最后一个 wsl.exe 退出后空闲一会儿就停，应用就打不开了）。
# 原来有这两项的不动，没有才加，记下加了哪些，卸载时只拿掉我们加的。
function Set-WslConfig {
    $cfg = Join-Path $env:USERPROFILE '.wslconfig'
    $text = ''
    if (Test-Path -LiteralPath $cfg) { $text = [IO.File]::ReadAllText($cfg) }
    $added = @()
    if ($text -notmatch '(?im)^\s*instanceIdleTimeout\s*=') { $added += 'instanceIdleTimeout' }
    if ($text -notmatch '(?im)^\s*vmIdleTimeout\s*=') { $added += 'vmIdleTimeout' }
    if ($added.Count -eq 0) { Add-Step 'wslconfig' 'skip' '已有这两项'; return }
    if ($Plan) { Say "（试跑）会在 $cfg 里加：$($added -join '、')"; return }
    $add = ''
    if ($added -contains 'instanceIdleTimeout') { $add += "`r`n[general]`r`ninstanceIdleTimeout=-1`r`n" }
    if ($added -contains 'vmIdleTimeout') { $add += "`r`n[wsl2]`r`nvmIdleTimeout=-1`r`n" }
    [IO.File]::AppendAllText($cfg, $add, (New-Object Text.ASCIIEncoding))
    Set-StateValue @{ wslconfig_added = $added }
    Add-Step 'wslconfig' 'done' ($added -join ',')
    Say "已在 $cfg 里加了让 WSL 一直开着的设置（$($added -join '、')）"
}

function Import-Distro([string]$ImagePath) {
    if (Test-DistroExists) { Add-Step 'import' 'skip' "$Distro 已经在"; return }
    if ($Plan) { Say "（试跑）会把 $ImagePath 导入成 WSL 发行版 $Distro（装在 $script:WslDir）"; return }
    Say "导入 Linux 发行版 $Distro …"
    [void](New-Item -ItemType Directory -Path $script:WslDir -Force)
    $t0 = Get-Date
    $r = Invoke-Native 'wsl.exe' @('--import', $Distro, $script:WslDir, $ImagePath, '--version', '2') -TimeoutSec 900
    $out = (($r.Out + $r.Err) -replace "`0", '').Trim()
    Write-Log "wsl --import rc=$($r.ExitCode) $out"
    if ($r.ExitCode -ne 0 -or -not (Test-DistroExists)) { Stop-WithFault 'WIN-WSL-02' $out }
    Set-StateValue @{ distro_imported = $true }
    Add-Step 'import' 'done' ("用时 {0}s" -f [int]((Get-Date) - $t0).TotalSeconds)
}

# 让发行版里的 systemd 跑起来、不把 Windows 的 PATH 带进 Linux、默认 root
function Initialize-Distro {
    if ($Plan) { Say '（试跑）会检查并写好发行版里的 /etc/wsl.conf（systemd、不带 Windows 的 PATH），然后等 systemd 起来'; return }
    $r = Invoke-Wsl @('/bin/cat', '/etc/wsl.conf') -TimeoutSec 120
    $conf = $r.Out
    if ($r.ExitCode -ne 0 -or $conf -notmatch '(?im)^\s*systemd\s*=\s*true' -or $conf -notmatch '(?im)^\s*appendWindowsPath\s*=\s*false') {
        $body = @'
[boot]
systemd=true

[interop]
appendWindowsPath=false

[user]
default=root
'@
        $f = Join-Path $script:Work 'wsl.conf'
        Write-LfFile $f $body
        $w = Invoke-Wsl @('/bin/cp', (ConvertTo-WslPath $f), '/etc/wsl.conf') -TimeoutSec 60
        if ($w.ExitCode -ne 0) { Stop-WithFault 'WIN-WSL-03' "写 /etc/wsl.conf 失败：$($w.Err.Trim())" }
        [void](Invoke-Native 'wsl.exe' @('--terminate', $Distro) -TimeoutSec 60)
        Start-Sleep -Seconds 2
        Add-Step 'wsl-conf' 'done' ''
    }
    # 等 systemd 起来（running 或 degraded 都行：Debian 里个别无关服务起不来也算起来了）
    $state = ''
    for ($i = 0; $i -lt 45; $i++) {
        $s = Invoke-Wsl @('/bin/systemctl', 'is-system-running') -TimeoutSec 60
        $state = ($s.Out).Trim()
        if ($state -in 'running', 'degraded') { break }
        Start-Sleep -Seconds 2
    }
    if ($state -notin 'running', 'degraded') { Stop-WithFault 'WIN-WSL-03' "systemctl is-system-running 是「$state」" }
    Add-Step 'systemd' 'done' $state
}

# >>> 测速选源（get.ps1 和 install-windows.ps1 里从这一行到“<<<”那一行必须一模一样；scripts/get.test.sh 会逐字核对）
# 做法和 get.sh 一样：没写 -China/-Global 时，同时从 Gitee 和 GitHub 各取同一个发行版文件的开头（最多 1 MiB、最多 ACORNFOX_PROBE_SECONDS 秒，默认 5），
# 比每秒取到多少字节，快的先用；没取到数据的那个再试一次（Gitee 偶尔中途卡住，P-847）；两边差不多快（Gitee 不到 1.3 倍）先用 GitHub。
# 为什么不用“github.com 连不连得上”：国内常常连得上但极慢。用 Windows 自带的 curl.exe 取（Gitee 的发行版文件不认 Range，所以按固定时间窗口比）。
function Get-ProbeUrl([string]$Src, [string]$Ver, [string]$Name) {
    $gh = $env:ACORNFOX_REPO; if (-not $gh) { $gh = 'OrbitMaker/AcornFox' }
    $ge = [Environment]::GetEnvironmentVariable('ACORNFOX_GITEE_REPO'); if ($null -eq $ge) { $ge = 'VIP13390/AcornFox' }
    $giteeHost = $env:ACORNFOX_GITEE_HOST; if (-not $giteeHost) { $giteeHost = 'https://gitee.com' }      # 只给测试用：换成本机的假服务器
    $githubHost = $env:ACORNFOX_GITHUB_HOST; if (-not $githubHost) { $githubHost = 'https://github.com' }
    if ($Src -eq 'gitee') { return "$giteeHost/$ge/releases/download/$Ver/$Name" }
    return "$githubHost/$gh/releases/download/$Ver/$Name"
}
function Start-ProbeCurl([string[]]$CurlArgs) {
    $curl = Join-Path $env:SystemRoot 'System32\curl.exe'
    if (-not (Test-Path -LiteralPath $curl)) { return $null }
    try {
        $psi = New-Object Diagnostics.ProcessStartInfo
        $psi.FileName = $curl
        $psi.Arguments = (($CurlArgs | ForEach-Object { if ($_ -match '[\s"]') { '"' + ($_ -replace '"', '\"') + '"' } else { $_ } }) -join ' ')
        $psi.UseShellExecute = $false; $psi.RedirectStandardOutput = $true; $psi.CreateNoWindow = $true
        $p = [Diagnostics.Process]::Start($psi)
        return @{ Proc = $p; Task = $p.StandardOutput.ReadToEndAsync() }
    } catch { return $null }
}
function Receive-ProbeCurl($H, [int]$Sec) {
    if (-not $H) { return '' }
    try {
        if (-not $H.Proc.WaitForExit(($Sec + 3) * 1000)) { try { $H.Proc.Kill() } catch { } }
        return [string]$H.Task.Result
    } catch { return '' }
}
function Get-ProbeBps([string]$Out) {  # “状态码 字节数 用时”换算成每秒字节数，没取到数据是 0
    if ($Out -match '^\s*(\d+)\s+(\d+)\s+([0-9.]+)') {
        $inv = [Globalization.CultureInfo]::InvariantCulture
        $size = [double]::Parse($Matches[2], $inv); $sec = [double]::Parse($Matches[3], $inv)
        if (($Matches[1] -eq '200' -or $Matches[1] -eq '206') -and $size -gt 0 -and $sec -gt 0) { return [long][math]::Floor($size / $sec) }
    }
    return [long]0
}
function Select-SourceBySpeed([string]$Ver, [string]$Asset) {  # 返回 @{ Pick = 'gitee'|'github'|''（空 = 两边都没取到数据）; Note = 给人看的一行 }
    $secs = 5; if ($env:ACORNFOX_PROBE_SECONDS -match '^\d+$' -and [int]$env:ACORNFOX_PROBE_SECONDS -gt 0) { $secs = [int]$env:ACORNFOX_PROBE_SECONDS }
    $bps = @{ gitee = [long]0; github = [long]0 }
    $todo = @('gitee', 'github')
    for ($try = 1; $try -le 2 -and $todo.Count -gt 0; $try++) {
        $hs = @{}
        foreach ($s in $todo) {
            $hs[$s] = Start-ProbeCurl @('-sL', '-r', '0-1048575', '--connect-timeout', '4', '-m', "$secs", '-o', 'NUL', '-w', '%{http_code} %{size_download} %{time_total}', (Get-ProbeUrl $s $Ver $Asset))
        }
        foreach ($s in $todo) { $bps[$s] = Get-ProbeBps (Receive-ProbeCurl $hs[$s] $secs) }
        $todo = @($todo | Where-Object { $bps[$_] -eq 0 })
    }
    $d = $bps['gitee']; $g = $bps['github']
    $inv = [Globalization.CultureInfo]::InvariantCulture
    $dm = ([double]$d / 1MB).ToString('0.00', $inv); $gm = ([double]$g / 1MB).ToString('0.00', $inv)
    if ($d -eq 0 -and $g -eq 0) { return @{ Pick = ''; Note = '测速：Gitee、GitHub 都没取到数据（各试了两次），先用 Gitee，取不到再换 GitHub' } }
    if ($g -eq 0) { return @{ Pick = 'gitee'; Note = "测速：Gitee $dm MB/s、GitHub 没测通，先用 Gitee" } }
    if ($d -eq 0) { return @{ Pick = 'github'; Note = "测速：Gitee 没测通、GitHub $gm MB/s，先用 GitHub" } }
    if (($d * 10) -gt ($g * 13)) { return @{ Pick = 'gitee'; Note = "测速：Gitee $dm MB/s、GitHub $gm MB/s，先用 Gitee" } }
    return @{ Pick = 'github'; Note = "测速：Gitee $dm MB/s、GitHub $gm MB/s，先用 GitHub" }   # 更快或差不多快：新发布的文件总是先到 GitHub
}
# <<< 测速选源

# 国内还是海外：-China / -Global 照用户的；都没写就当场测速（一次运行只测一次），结果交给 WSL 里的 get.sh / install.sh（--china / --global）。
# 返回 'china'、'global'，或 ''（没写、两边也都没测通：下载先试 Gitee，不往下传判断）。
# 用发布源（-ReleaseBase）测试时也测：下载来自发布源，但软件源、Docker 镜像的选法还是要看这台电脑实际连哪边快。
$script:RegionPick = $null
$script:ProbeNote = ''
function Get-RegionChoice([string]$Ver) {
    if ($China -or $Global) {
        if ($env:ACORNFOX_GET_PROBE_NOTE -and -not $script:ProbeNote) {
            $script:ProbeNote = $env:ACORNFOX_GET_PROBE_NOTE
            Write-Log ($script:ProbeNote + '（get.ps1 的测速，已作为 -China / -Global 传到这里）')
        }
        if ($China) { return 'china' }; return 'global'
    }
    if ($null -eq $script:RegionPick) {
        $script:RegionPick = ''
        $pv = $env:ACORNFOX_PROBE_VERSION; if (-not $pv) { $pv = $Ver }
        if ($pv) {
            $r = Select-SourceBySpeed $pv 'acornfox_linux_amd64'
            $script:ProbeNote = [string]$r.Note
            Say $script:ProbeNote
            if ($r.Pick -eq 'gitee') { $script:RegionPick = 'china' } elseif ($r.Pick -eq 'github') { $script:RegionPick = 'global' }
        }
    }
    return $script:RegionPick
}
function Get-ForceRegionArgs([string]$Ver) {
    $rc = Get-RegionChoice $Ver
    if ($rc -eq 'china') { return @('--china') }
    if ($rc -eq 'global') { return @('--global') }
    return @()
}

# 要交给 WSL 里脚本的环境变量（发布源地址、换了的发布仓库）：安装、升级、卸载用同一份。
# 不交过去的话，WSL 里的 get.sh 会掉回线上地址：用本地发布源测试、或用自建镜像的人，卸载这一步就会去外网取脚本，慢、还可能失败。
function Get-InsideEnvLines {
    $env1 = ''
    $rb = $ReleaseBase; if (-not $rb) { $rb = $env:ACORNFOX_RELEASE_BASE }
    if ($rb) { $env1 = "export ACORNFOX_RELEASE_BASE='" + $rb + "'`n" }
    if ($script:ProbeNote) { $env1 += "export ACORNFOX_GET_PROBE_NOTE='" + ($script:ProbeNote -replace "'", '') + "'`n" }
    foreach ($v in @('ACORNFOX_REPO', 'ACORNFOX_GITEE_REPO')) {
        $val = [Environment]::GetEnvironmentVariable($v)
        if ($null -ne $val) { $env1 += "export $v='" + $val + "'`n" }
    }
    return $env1
}

# 在 WSL 里运行 AcornFox 的安装或升级脚本（同一个 install.sh / upgrade.sh：经引导脚本 get.sh 取这个版本自带的）。
# $Verb：install 或 upgrade。输出直接接到当前窗口，同时记在 WSL 里的 /root/acornfox-install.log。
function Invoke-InsideRun([string]$Verb, [string]$PublicHost, [string]$Ver) {
    $region = (Get-ForceRegionArgs $Ver) -join ' '
    $china = ((Get-RegionChoice $Ver) -ne 'global')    # 软件源：测速没分出高下（''）时也先用国内镜像，连不上会退回原来的
    $env1 = Get-InsideEnvLines
    # 软件源：国内先换成国内镜像（下面装 curl 才快，install.sh 之后会自己测速选源）；镜像连不上就退回原来的源再试，
    # 最后再试一次镜像。原来的文件备份成 .afxbak，不丢。
    $aptChina = @'
APT_FILES="/etc/apt/sources.list /etc/apt/sources.list.d/*.sources"
use_china_apt() {
    for f in $APT_FILES; do
        [ -f "$f" ] || continue
        [ -f "$f.afxbak" ] || cp -a "$f" "$f.afxbak"
        sed -i -E 's#https?://(deb|security|ftp)\.debian\.org#https://mirrors.aliyun.com#g; s#https?://(archive|security)\.ubuntu\.com#https://mirrors.aliyun.com#g' "$f"
    done
}
use_orig_apt() {
    for f in $APT_FILES; do [ -f "$f.afxbak" ] && cp -a "$f.afxbak" "$f"; done
    return 0
}
'@
    if (-not ($china -and $Verb -eq 'install')) { $aptChina = "APT_FILES=''`nuse_china_apt() { return 0; }`nuse_orig_apt() { return 0; }`n" }
    else { $aptChina += "`nuse_china_apt`n" }
    # 来源标记（-From，已在文件开头校验过：只有 a-z、0-9、-，所以直接拼进命令是安全的）：只在安装时交给 WSL 里的 get.sh --from
    $extra = $(if ($Verb -eq 'install') { "--wsl --public-host $PublicHost $region" } else { "-y $region" }).Trim()
    if ($Verb -eq 'install' -and $From) { $extra = "$extra --from $From" }
    if ($LocalPackage) {
        $pkg = ConvertTo-WslPath (Resolve-Path -LiteralPath $LocalPackage).ProviderPath
        $run = "bash '$pkg/$Verb.sh' --binary '$pkg/acornfox_linux_amd64' $extra"
    } else {
        $gs = Get-GetShOnWindowsSide $Ver
        $run = "bash '" + (ConvertTo-WslPath $gs) + "' $Verb --version $Ver $extra"
    }
    $body = @"
set -u
LOG=/root/acornfox-install.log
exec > >(tee -a "`$LOG") 2>&1
echo "==== `$(date '+%F %T') 从 Windows $Verb AcornFox（$Ver）===="
$env1
$aptChina
export DEBIAN_FRONTEND=noninteractive
need=""
command -v curl >/dev/null 2>&1 || need="`$need curl"
[ -s /etc/ssl/certs/ca-certificates.crt ] || need="`$need ca-certificates"
command -v ip >/dev/null 2>&1 || need="`$need iproute2"
try_apt() { apt-get update -qq && apt-get install -y -qq --no-install-recommends `$need; }
if [ -n "`$need" ]; then
    echo "先装几个基础软件:`$need"
    if ! try_apt; then
        echo "软件源连不上，退回原来的软件源再试一次"
        use_orig_apt
        if ! try_apt; then
            echo "再用国内镜像试一次"
            use_china_apt
            sleep 5
            try_apt || { echo "装不上 `$need（软件源都连不上）：确认这台电脑能上网、关掉代理后重新运行"; exit 1; }
        fi
    fi
fi
$run
rc=`$?
echo "==== $Verb 退出码 `$rc ===="
exit `$rc
"@
    $t0 = Get-Date
    $r = Invoke-WslScript "inside-$Verb.sh" $body 3600 -Inherit:(-not $Json)
    $sec = [int]((Get-Date) - $t0).TotalSeconds
    if ($r.ExitCode -ne 0) {
        # 脚本最后打印的故障编号（INSTALL-…、EDGE-… 等）一起带出来，不用再翻 WSL 里的记录
        $inner = ''
        $t = Invoke-Wsl @('/usr/bin/tail', '-n', '60', '/root/acornfox-install.log') -TimeoutSec 30
        $m = [regex]::Matches($t.Out, '故障编号:\s*([A-Z][A-Z0-9-]+)')
        if ($m.Count -gt 0) { $inner = $m[$m.Count - 1].Groups[1].Value }
        $extra = $(if ($inner) { "；WSL 里的脚本报 $inner（acornfox faults $inner 看处理办法）" } else { '' })
        Stop-WithFault 'WIN-LINUX-01' "$Verb 脚本退出码 $($r.ExitCode)（用时 ${sec}s）$extra"
    }
    return $sec
}

function Install-LinuxSide([string]$PublicHost, [string]$Ver) {
    if ((Test-AcornFoxInDistro) -and ((Invoke-Wsl @('/bin/systemctl', 'is-active', 'acornfox-server') -TimeoutSec 30).Out.Trim() -eq 'active')) {
        Add-Step 'linux-install' 'skip' ("已装 " + (Get-DistroAcornFoxVersion))
        return
    }
    if ($Plan) { Say "（试跑）会在 $Distro 里运行 get.sh install --version $Ver --wsl --public-host $PublicHost$(if ($From) { " --from $From" })，装 Docker 和 AcornFox（要几分钟）"; return }
    Say "在 $Distro 里安装 AcornFox（要装 Docker，几分钟；输出同时记在 WSL 里的 /root/acornfox-install.log）…"
    $sec = Invoke-InsideRun 'install' $PublicHost $Ver
    Add-Step 'linux-install' 'done' "用时=${sec}s"
}

# 在 Windows 这一侧取 get.sh（引导脚本）：从发行版取、核对 SHA256SUMS，放在 Work 里，WSL 里经 /mnt/c 运行
function Get-ReleaseSources([string]$Ver) {
    $list = @()
    $base = $ReleaseBase; if (-not $base) { $base = $env:ACORNFOX_RELEASE_BASE }
    if ($base) { return @($base.TrimEnd('/') + "/releases/download/$Ver/") }
    $gh = $env:ACORNFOX_REPO; if (-not $gh) { $gh = 'OrbitMaker/AcornFox' }
    $ge = [Environment]::GetEnvironmentVariable('ACORNFOX_GITEE_REPO'); if ($null -eq $ge) { $ge = 'VIP13390/AcornFox' }
    $hub = "https://github.com/$gh/releases/download/$Ver/"
    $gitee = $(if ($ge) { "https://gitee.com/$ge/releases/download/$Ver/" } else { '' })
    $china = ((Get-RegionChoice $Ver) -ne 'global')
    if ($china -and $gitee) { $list = @($gitee, $hub) } elseif ($gitee) { $list = @($hub, $gitee) } else { $list = @($hub) }
    return $list
}
function Get-GetShOnWindowsSide([string]$Ver, [switch]$NoFault) {
    $dest = Join-Path $script:Work 'get.sh'
    $sums = $null
    foreach ($b in (Get-ReleaseSources $Ver)) {
        $r1 = Invoke-Native (Join-Path $env:SystemRoot 'System32\curl.exe') @('-fsSL', '--connect-timeout', '15', '--max-time', '120', '-o', $dest, ($b + 'get.sh')) -TimeoutSec 150
        $r2 = Invoke-Native (Join-Path $env:SystemRoot 'System32\curl.exe') @('-fsSL', '--connect-timeout', '15', '--max-time', '120', '-o', "$dest.sums", ($b + 'SHA256SUMS')) -TimeoutSec 150
        if ($r1.ExitCode -ne 0 -or $r2.ExitCode -ne 0) { continue }
        $want = ''
        foreach ($l in (Get-Content -LiteralPath "$dest.sums")) { if ($l -match '^([0-9a-fA-F]{64})\s+\*?get\.sh\s*$') { $want = $Matches[1].ToLower() } }
        if ($want -and (Get-FileSha256 $dest) -eq $want) { Write-Log "get.sh 校验通过（来自 $b）"; return $dest }
        Write-Log "get.sh 校验不过（来自 $b）"
    }
    if ($NoFault) { return $null }
    Stop-WithFault 'WIN-DL-02' "取不到 $Ver 的 get.sh，或校验不过（发行版地址都试过）"
}

# 把 Windows 上的机器钥匙（连过下载网关才有）交给 WSL 里的 acornfox：一台电脑在网关上只算一台机器
function Deliver-MachineKey {
    $key = Get-AfxDefaultKeyPath
    if (-not (Test-Path -LiteralPath $key)) { return }
    if ($Plan) { return }
    $exists = Invoke-Wsl @('/usr/bin/test', '-e', '/var/lib/acornfox/identity/machine-key.pem') -TimeoutSec 30
    if ($exists.ExitCode -eq 0) { Add-Step 'machine-key' 'skip' 'WSL 里已有钥匙，不覆盖'; return }
    $tmp = Join-Path $script:Work 'machine-key.pem'
    try {
        $e = Export-AfxMachineKeyPem -OutFile $tmp
        $b = "set -e`ninstall -d -m 700 -o acornfox -g acornfox /var/lib/acornfox/identity`ninstall -m 600 -o acornfox -g acornfox '" + (ConvertTo-WslPath $tmp) + "' /var/lib/acornfox/identity/machine-key.pem`n"
        $r = Invoke-WslScript 'deliver-key.sh' $b 60
        if ($r.ExitCode -ne 0) { Write-Log "交钥匙没成功 rc=$($r.ExitCode) $($r.Err)"; SayWarn '没能把机器钥匙交给 WSL 里的 AcornFox（不影响使用，网关上会当成两台机器）' }
        else { Add-Step 'machine-key' 'done' '' }
    } finally { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
}

# Windows 上的 acornfox 命令和用户 PATH
function Install-Wrapper {
    if ($Plan) { Say "（试跑）会写 $script:BinDir\acornfox.cmd、acornfox.ps1，把 $script:BinDir 加进用户 PATH，并保存本脚本的副本（afx-win.ps1）"; return }
    [void](New-Item -ItemType Directory -Path $script:BinDir -Force)
    $enc = New-Object Text.UTF8Encoding($true)
    $self = $script:SelfCopy
    if ($PSCommandPath -and (Test-Path -LiteralPath $PSCommandPath) -and ((Resolve-Path -LiteralPath $PSCommandPath).ProviderPath -ne $self)) { Copy-Item -LiteralPath $PSCommandPath -Destination $self -Force }
    [IO.File]::WriteAllText((Join-Path $script:BinDir 'acornfox.cmd'), ($script:WrapperCmd -replace "`r?`n", "`r`n"), (New-Object Text.UTF8Encoding($false)))
    [IO.File]::WriteAllText((Join-Path $script:BinDir 'acornfox.ps1'), $script:WrapperPs1.Replace('@@DISTRO@@', $Distro).Replace('@@SELF@@', $self), $enc)
    $cur = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not (@($cur -split ';') -contains $script:BinDir)) {
        [Environment]::SetEnvironmentVariable('Path', ($(if ($cur) { $cur.TrimEnd(';') + ';' } else { '' }) + $script:BinDir), 'User')
        Set-StateValue @{ path_added = $true }
    }
    if (-not (@($env:Path -split ';') -contains $script:BinDir)) { $env:Path = $env:Path + ';' + $script:BinDir }
    Add-Step 'wrapper' 'done' $script:BinDir
}

# 重启后接着装：登录时运行一次（HKCU RunOnce，不要管理员）
function Set-ResumeAfterReboot {
    $a = @('resume')
    if ($China) { $a += '-China' }; if ($Global) { $a += '-Global' }
    if ($Image -ne 'debian') { $a += @('-Image', $Image) }
    if ($NoGateway) { $a += '-NoGateway' }; if ($LocalOnly) { $a += '-LocalOnly' }; if ($NoOpen) { $a += '-NoOpen' }
    if ($Version) { $a += @('-Version', $Version) }
    if ($ReleaseBase) { $a += @('-ReleaseBase', $ReleaseBase) }
    if ($LocalPackage) { $a += @('-LocalPackage', $LocalPackage) }
    if ($Distro -ne 'AcornFox') { $a += @('-Distro', $Distro) }
    if ($Gateway) { $a += @('-Gateway', $Gateway) }
    if ($From) { $a += @('-From', $From) }
    $src = $script:SelfCopy
    if ($PSCommandPath -and (Test-Path -LiteralPath $PSCommandPath)) { Copy-Item -LiteralPath $PSCommandPath -Destination $src -Force }
    $cmd = 'powershell.exe -NoProfile -ExecutionPolicy Bypass -NoExit -File "' + $src + '" ' + (($a | ForEach-Object { ConvertTo-WinArg $_ }) -join ' ')
    $key = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\RunOnce'
    if (-not (Test-Path $key)) { [void](New-Item -Path $key -Force) }
    Set-ItemProperty -Path $key -Name 'AcornFoxResume' -Value $cmd
    Set-StateValue @{ resume_pending = $true }
    Write-Log "RunOnce: $cmd"
}
function Clear-ResumeAfterReboot {
    Remove-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\RunOnce' -Name 'AcornFoxResume' -ErrorAction SilentlyContinue
}

function Test-HttpOk([string]$Url, [int]$Sec = 10) {
    try { $r = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec $Sec -ErrorAction Stop; return [int]$r.StatusCode }
    catch { if ($_.Exception.Response) { return [int]$_.Exception.Response.StatusCode } else { return 0 } }
}

function Get-TaskInfo([string]$Name) {
    try { return (Get-ScheduledTask -TaskName $Name -ErrorAction Stop) } catch { return $null }
}
function Get-ManagedRules {
    $rules = @{}
    $t = (& netsh.exe interface portproxy show v4tov4 2>$null) -join "`n"
    foreach ($m in [regex]::Matches($t, '(?m)^\s*(\S+)\s+(\d+)\s+(\S+)\s+(\d+)\s*$')) {
        $p = [int]$m.Groups[2].Value
        if ($m.Groups[1].Value -eq '0.0.0.0' -and ($p -eq 18802 -or ($p -ge 18810 -and $p -le 18899))) { $rules[$p] = $m.Groups[3].Value }
    }
    return $rules
}

# ---------------------------------------------------------------------------------------------------------
# 动作
# ---------------------------------------------------------------------------------------------------------
function Resolve-TargetVersion {
    $v = $Version
    if ($v -and $v -notmatch '^v') { $v = "v$v" }
    if ($ScriptVersion -ne 'dev') {
        if ($v -and $v -ne $ScriptVersion) { Stop-WithFault 'WIN-SRC-01' "这个脚本只装 $ScriptVersion，不装 $v；要装别的版本用那个版本自带的脚本（get.ps1 加 -Version）" }
        return $ScriptVersion
    }
    if ($v) { return $v }
    if ($LocalPackage) { return 'dev-local' }
    Stop-WithFault 'WIN-SRC-01'
}

function Show-PlanList([string[]]$Lines) {
    Say '将要做的事：'
    $i = 1
    foreach ($l in $Lines) { Write-Host ("  {0}. {1}" -f $i, $l); Add-Step "plan-$i" 'plan' $l; $i++ }
}

function Open-Console([string]$LanIp, [bool]$Lan) {
    $url = "http://localhost:$($script:ConsolePort)/"
    if ($Lan) {
        $code = Test-HttpOk "http://localhost:$($script:LanConsolePort)/" 8
        if ($code -ge 200 -and $code -lt 400) { $url = "http://localhost:$($script:LanConsolePort)/" }
    }
    if ((Test-HttpOk $url 10) -eq 0) { Stop-WithFault 'WIN-NET-01' "$url 没有应答" }
    Say "控制台：$url"
    if (-not $NoOpen -and [Environment]::UserInteractive -and $env:SESSIONNAME) {
        try { Start-Process $url } catch { Write-Log "打不开浏览器：$($_.Exception.Message)" }
    }
    return $url
}

function Wait-LanForward([string]$LanIp) {
    # 局域网转发由 AcornFoxLAN 任务每 5 秒同步一次；18802（局域网控制台）监听后几秒内应该出现规则
    $ok = $false
    for ($i = 0; $i -lt 30; $i++) {
        $r = Get-ManagedRules
        if ($r.Count -gt 0) { $ok = $true; break }
        Start-Sleep -Seconds 3
    }
    if (-not $ok) {
        SayWarn '局域网转发还没生效（AcornFoxLAN 任务没有加上规则）。应用在这台电脑上能打开，局域网里的设备暂时打不开。运行 acornfox windows doctor 看原因。'
        [void]$script:Faults.Add([pscustomobject]@{ code = 'WIN-LAN-01'; message = $script:FaultText['WIN-LAN-01'][0]; hint = $script:FaultText['WIN-LAN-01'][1]; detail = '' })
        return $false
    }
    $cat = Get-LanNetworkCategory $LanIp
    if ($cat -eq 'Public') {
        SayWarn "这台电脑的网络类型是「公用」，Windows 防火墙只对专用网络放行，局域网里的设备会打不开。$($script:FaultText['WIN-LAN-03'][1])"
        [void]$script:Faults.Add([pscustomobject]@{ code = 'WIN-LAN-03'; message = $script:FaultText['WIN-LAN-03'][0]; hint = $script:FaultText['WIN-LAN-03'][1]; detail = $cat })
    }
    return $true
}

function Invoke-Install {
    if (-not $Plan) { Ensure-Dirs }
    $pre = Invoke-Preflight
    $ver = Resolve-TargetVersion
    $w = Get-WslReadiness
    $lan = -not $LocalOnly
    if ($lan -and -not $pre.admin_account) {
        SayWarn '当前账号不是管理员账号，局域网访问不打开（应用只在这台电脑上能打开）。想让局域网里的设备访问，用管理员账号再运行。'
        [void]$script:Faults.Add([pscustomobject]@{ code = 'WIN-LAN-02'; message = $script:FaultText['WIN-LAN-02'][0]; hint = $script:FaultText['WIN-LAN-02'][1]; detail = '' })
        $lan = $false
    }
    $distroExists = $false
    if ($w.ready) { $distroExists = Test-DistroExists }
    $needWsl = -not $w.ready
    $needImage = -not $distroExists
    $lanIp = Get-WinLanIp
    $publicHost = $(if ($lanIp) { $lanIp } else { '127.0.0.1' })
    $wantTask = $(if ($lan) { $script:TaskLan } else { $script:TaskWsl })
    $needTasks = ($null -eq (Get-TaskInfo $wantTask)) -or ($lan -and ($null -ne (Get-TaskInfo $script:TaskWsl))) -or ((-not $lan) -and ($null -ne (Get-TaskInfo $script:TaskLan)))
    # 局域网同步脚本有新版本（或被改过、不在了）也要重新登记，不然旧的一直在跑
    if ($lan -and -not $needTasks) {
        $have = ''
        if (Test-Path -LiteralPath $script:SyncScriptPath) { $have = (Get-Content -LiteralPath $script:SyncScriptPath -Raw -Encoding UTF8).Trim() }
        if ($have -ne $script:SyncScriptText.Trim()) { $needTasks = $true }
    }
    $needAdmin = $needWsl -or $needTasks
    $F = $script:Files[$Image]

    $lines = @()
    $lines += "检查这台电脑（Windows 版本 $($pre.build)、虚拟化、磁盘剩 $($pre.free_gb) GB）——已通过"
    if ($needWsl) { $lines += "下载 WSL 安装包（$($script:Files.wsl.Title)）；管理员确认一次：打开「虚拟机平台」、安装 WSL；可能要重启一次电脑，重启后登录自动接着装" }
    else { $lines += "WSL 已经能用（版本 $($w.version)），不用装也不用重启" }
    if ($needImage) { $lines += "下载 $($F.Title) 并导入成 WSL 发行版 $Distro（装在 $script:WslDir）" } else { $lines += "发行版 $Distro 已经在，不重新导入" }
    if ($w.ready -and $distroExists -and (Test-AcornFoxInDistro) -and ((Invoke-Wsl @('/bin/systemctl', 'is-active', 'acornfox-server') -TimeoutSec 30).Out.Trim() -eq 'active')) { $lines += "AcornFox 已经装在 $Distro 里（$(Get-DistroAcornFoxVersion)），不重装" }
    else { $lines += "在 $Distro 里安装 AcornFox $ver（Docker 和 AcornFox 本体，要几分钟）；应用访问地址用 $publicHost" }
    if ($needTasks) { $lines += $(if ($lan) { '登记开机自启和局域网转发任务、Windows 防火墙规则（只在专用网络放行 18802、18810-18899）' } else { '登记开机自启任务（只本机，不开局域网访问）' }) + $(if (-not $needWsl) { '；要管理员确认一次' } else { '' }) }
    $lines += '写 acornfox 命令（放在你的用户目录，加进用户 PATH），在 .wslconfig 里让 WSL 一直开着'
    $lines += '打开控制台'
    Show-PlanList $lines
    if ($Plan) {
        Say '（试跑）以上只是计划，什么都没有改。'
        $pl = @{ ok = $true }
        if ($needWsl) { Add-Step 'plan-wsl' 'todo' '装或升级 WSL' }
        return
    }
    Set-StateValue @{ script_version = $ScriptVersion; version = $ver; distro = $Distro; image = $Image; lan = $lan; started = (Get-Date -Format 'o') }
    Write-Log "install ver=$ver wsl=$($w.version) vmp=$($w.vmp) distro=$distroExists lan=$lan host=$publicHost av=$($pre.antivirus)"
    if ($pre.antivirus) { Write-Log "安全软件：$($pre.antivirus)" }

    # 1. 先在用户身份下把要下载的都下好（有进度；点「否」或取消时文件留着，下次直接用）
    $msi = $null
    if ($needWsl) { $msi = Get-AfxFile 'wsl' }
    $img = $null
    if ($needImage) { $img = Get-AfxFile $Image }

    # 2. 管理员那一步（一次确认做完：装 WSL、登记任务、防火墙、局域网转发）
    if ($needAdmin) {
        $todo = @(); $extra = @{ lan = $lan }
        if ($needWsl) { $todo += 'wsl'; $extra.msi = $msi; $extra.msiSha = $script:Files.wsl.Sha256; $extra.enableVmp = (-not $w.vmp) }
        if ($needTasks) { $todo += 'tasks' }
        $res = Invoke-AdminStep $todo $extra
        Add-Step 'admin' 'done' ($res.done -join ',')
        if ($needTasks) { Set-StateValue @{ tasks = $true } }
        if ($res.reboot) {
            Set-ResumeAfterReboot
            Write-Host ''
            Say 'Windows 要重启一次才能用上 WSL。请保存手上的工作后重启电脑；重启后登录，会自动弹出窗口接着装（也可以自己再运行一次同样的命令）。'
            [void]$script:Faults.Add([pscustomobject]@{ code = 'WIN-REBOOT-01'; message = $script:FaultText['WIN-REBOOT-01'][0]; hint = $script:FaultText['WIN-REBOOT-01'][1]; detail = '' })
            Add-Step 'reboot' 'needed' ''
            $script:ExitCode = 3
            return
        }
        $w = Get-WslReadiness
        if (-not $w.ready) { Stop-WithFault 'WIN-WSL-01' "wsl --version 版本=$($w.version) 虚拟机平台=$($w.vmp)" }
    }
    Clear-ResumeAfterReboot

    # 3. WSL 就绪：一直开着的设置 → 导入 → systemd → 装 AcornFox
    Set-WslConfig
    if (-not (Test-DistroExists)) { if (-not $img) { $img = Get-AfxFile $Image }; Import-Distro $img } else { Add-Step 'import' 'skip' "$Distro 已经在" }
    Initialize-Distro
    Install-LinuxSide $publicHost $ver
    Deliver-MachineKey
    Install-Wrapper
    if ($lan) {
        [void](Wait-LanForward $lanIp)
    }
    Set-StateValue @{ installed = $true; installed_at = (Get-Date -Format 'o'); resume_pending = $false }
    $url = Open-Console $lanIp $lan
    Write-Host ''
    Say '装好了。'
    Write-Host "  控制台：$url"
    if ($lan -and $lanIp) { Write-Host "  局域网里的电脑、手机：http://${lanIp}:$($script:LanConsolePort)/（第一次打开时设密码；应用用 http://${lanIp}:端口 打开）" }
    Write-Host '  命令行：在新开的 PowerShell 或命令提示符里运行 acornfox（比如 acornfox apps 看应用）；检查：acornfox doctor'
    Write-Host '  装 Skill 给 AI 工作台：acornfox skill install'
}

function Invoke-Upgrade {
    if (-not $Plan) { Ensure-Dirs }
    $ver = Resolve-TargetVersion
    $w = Get-WslReadiness
    if (-not $w.ready -or -not (Test-DistroExists) -or -not (Test-AcornFoxInDistro)) { Stop-WithFault 'WIN-LINUX-01' '这台电脑上还没有装好 AcornFox，先运行安装' }
    $cur = Get-DistroAcornFoxVersion
    Show-PlanList @("在 $Distro 里升级 AcornFox：现在 $cur → $ver", '刷新 Windows 上的 acornfox 命令和本脚本的副本', '局域网转发任务有新版本时更新（要管理员确认一次）')
    if ($Plan) { Say '（试跑）以上只是计划，什么都没有改。'; return }
    $sec = Invoke-InsideRun 'upgrade' '' $ver
    Add-Step 'linux-upgrade' 'done' "用时=${sec}s"
    Install-Wrapper
    $st = Read-State
    if ((Get-StateValue $st 'lan' $false) -and (Test-Path -LiteralPath $script:SyncScriptPath)) {
        $have = (Get-Content -LiteralPath $script:SyncScriptPath -Raw -Encoding UTF8).Trim()
        if ($have -ne $script:SyncScriptText.Trim()) {
            Say '局域网转发任务有新版本，更新一下。'
            $res = Invoke-AdminStep @('tasks') @{ lan = $true }
            Add-Step 'sync-script' 'done' ($res.done -join ',')
        }
    }
    Set-StateValue @{ version = $ver; upgraded_at = (Get-Date -Format 'o') }
    Say ("升级完成：现在是 " + (Get-DistroAcornFoxVersion))
}

function Remove-WslConfigAdded {
    $st = Read-State
    $added = @(Get-StateValue $st 'wslconfig_added' @())
    $cfg = Join-Path $env:USERPROFILE '.wslconfig'
    if ($added.Count -eq 0 -or -not (Test-Path -LiteralPath $cfg)) { return }
    $lines = @([IO.File]::ReadAllLines($cfg))
    $out = New-Object System.Collections.ArrayList
    foreach ($l in $lines) {
        $drop = $false
        foreach ($k in $added) { if ($l -match "^\s*$k\s*=") { $drop = $true } }
        if (-not $drop) { [void]$out.Add($l) }
    }
    # 拿掉变空的 [general]、[wsl2] 小节（只有空行的）
    $text = ($out -join "`r`n")
    $text = [regex]::Replace($text, '(?m)^\[(general|wsl2)\]\s*(?=^\[|\z)', '')
    $text = $text.Trim()
    if ($text) { [IO.File]::WriteAllText($cfg, $text + "`r`n", (New-Object Text.ASCIIEncoding)) } else { Remove-Item -LiteralPath $cfg -Force }
}

# 过几秒再删几个路径（由另一个隐藏的 PowerShell 做）：命令目录里的 acornfox.cmd 可能就是运行本脚本的那个，
# 立刻删它会让 cmd 在脚本退出后读不到下一行，报“找不到批处理文件”并把退出码变成 1；本脚本自己的副本同理。
# 这个进程用 WMI（Win32_Process.Create）起，不是本进程的子进程：经 ssh 等远程会话运行时，会话一结束子进程会被一起带走，
# 延后的删除就没做成（原来的做法会在 bin、logs 里留下残留）。每个路径最多试 15 次（隔 2 秒），被占着就等一等。
function Remove-LaterPaths([string[]]$Paths) {
    $todo = @($Paths | Where-Object { $_ -and (Test-Path -LiteralPath $_) })
    if ($todo.Count -eq 0) { return }
    $list = ($todo | ForEach-Object { "'" + $_.Replace("'", "''") + "'" }) -join ','
    $code = "Start-Sleep 5; foreach (`$p in @($list)) { for (`$i = 0; `$i -lt 15 -and (Test-Path -LiteralPath `$p); `$i++) { Remove-Item -LiteralPath `$p -Recurse -Force -ErrorAction SilentlyContinue; if (Test-Path -LiteralPath `$p) { Start-Sleep 2 } } }"
    $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($code))
    $started = $false
    try {
        $r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -ErrorAction Stop -Arguments @{ CommandLine = "powershell.exe -NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -EncodedCommand $enc" }
        $started = ($r.ReturnValue -eq 0)
    } catch { }
    if (-not $started) {
        Start-Process -FilePath 'powershell.exe' -ArgumentList @('-NoProfile', '-NonInteractive', '-WindowStyle', 'Hidden', '-ExecutionPolicy', 'Bypass', '-EncodedCommand', $enc) -WindowStyle Hidden | Out-Null
    }
}

# 卸载没做完时告诉用户怎么重来：命令写完整路径（PATH 里的 acornfox 在别处可能找不到）
function Get-UninstallRetryText {
    $cmd = Join-Path $script:BinDir 'acornfox.cmd'
    if (Test-Path -LiteralPath $cmd) { return ("重新运行：& `"$cmd`" windows uninstall" + $(if ($Purge) { ' --purge --yes' } else { '' })) }
    return ('重新运行安装命令，末尾换成 -Action uninstall' + $(if ($Purge) { ' -Purge -Yes' } else { '' }))
}

# 回头查 WSL 里还有没有 AcornFox 的服务、容器在跑（只读）。返回说明的数组，空表示没有。
function Get-InsideLeftovers {
    $b = @'
for u in acornfox-server acornfox-runner; do
    systemctl is-active --quiet "$u" 2>/dev/null && echo "服务 $u 还在运行"
done
if command -v docker >/dev/null 2>&1; then
    n=$( { docker ps -q --filter label=acornfox.managed=1; docker ps -q --filter label=acornfox.edge=1; } 2>/dev/null | sort -u | wc -l)
    [ "${n:-0}" -gt 0 ] && echo "$n 个 AcornFox 的容器还在运行（含入口容器）"
fi
exit 0
'@
    $r = Invoke-WslScript 'inside-uninstall-check.sh' $b 60
    if ($r.ExitCode -ne 0) { return @("没能回头检查 WSL 里的状态（wsl 退出码 $($r.ExitCode)）") }
    return @(($r.Out -split "`r?`n") | ForEach-Object { $_.Trim() } | Where-Object { $_ })
}

# 在 WSL 里卸载 AcornFox 的服务、容器、网络和程序（不带 -Purge 时保留数据）：用现在装着的那个版本自带的 uninstall.sh，
# 经 get.sh 取并校验（-LocalPackage 时用包里的）；输出同时记在 %LOCALAPPDATA%\AcornFox\logs\uninstall-wsl.log。
# 返回空字符串：WSL 一侧卸干净了（脚本退出码 0，并且回头查过服务停了、没有 AcornFox 的容器在跑）；
# 否则返回一句话，说明哪一步没成。
function Invoke-InsideUninstall {
    $wlog = Join-Path $script:Logs 'uninstall-wsl.log'
    $flag = $(if ($Purge) { '--purge' } else { '' })
    if ($LocalPackage -and (Test-Path -LiteralPath (Join-Path $LocalPackage 'uninstall.sh'))) {
        $pkg = ConvertTo-WslPath (Resolve-Path -LiteralPath $LocalPackage).ProviderPath
        $run = "bash '$pkg/uninstall.sh' $flag -y"
    } else {
        $cands = @()
        $cur = Get-DistroAcornFoxVersion
        if ($cur) { $cands += ('v' + $cur) }
        if ($ScriptVersion -ne 'dev' -and $cands -notcontains $ScriptVersion) { $cands += $ScriptVersion }
        $gs = $null
        foreach ($v in $cands) { $gs = Get-GetShOnWindowsSide $v -NoFault; if ($gs) { break } }
        if (-not $gs) { return ("取不到卸载要用的 get.sh（" + ($cands -join '、') + " 的发行版地址都试过，或校验不过），多半是没联网（故障编号 WIN-DL-02）") }
        $run = "bash '" + (ConvertTo-WslPath $gs) + "' uninstall $flag -y"
    }
    $body = "set -u`nexec > >(tee -a '" + (ConvertTo-WslPath $wlog) + "') 2>&1`n" + (Get-InsideEnvLines) +
        "echo `"==== `$(date '+%F %T') 从 Windows 卸载 AcornFox ====`"`n$run`nrc=`$?`necho `"==== 卸载脚本退出码 `$rc ====`"`nsleep 1`nexit `$rc`n"
    $r = Invoke-WslScript 'inside-uninstall.sh' $body 900 -Inherit:(-not $Json)
    $why = @()
    if ($r.ExitCode -ne 0) {
        # 脚本最后打印的故障编号一起带出来，不用再翻记录
        $inner = ''
        if (Test-Path -LiteralPath $wlog) {
            $tail = (@(Get-Content -LiteralPath $wlog -Tail 60 -Encoding UTF8)) -join "`n"
            $m = [regex]::Matches($tail, '故障编号:\s*([A-Z][A-Z0-9-]+)')
            if ($m.Count -gt 0) { $inner = $m[$m.Count - 1].Groups[1].Value }
        }
        $why += ("WSL 里的卸载脚本退出码 $($r.ExitCode)" + $(if ($inner) { "，它报 $inner（acornfox faults $inner 看办法）" } else { '' }))
    }
    $left = @(Get-InsideLeftovers)
    if ($left.Count -gt 0) { $why += ('回头检查：' + ($left -join '；')) }
    return ($why -join '；')
}

function Invoke-Uninstall {
    $st = Read-State
    $distroThere = $false
    if ((Get-WslReadiness).ready) { $distroThere = Test-DistroExists }
    $inDistro = $distroThere -and (Test-AcornFoxInDistro)
    $tasks = ($null -ne (Get-TaskInfo $script:TaskLan)) -or ($null -ne (Get-TaskInfo $script:TaskWsl))
    $rules = (Get-ManagedRules).Count -gt 0
    $fw = $false; try { $fw = [bool](Get-NetFirewallRule -Name $script:FirewallRule -ErrorAction Stop) } catch { }
    $needAdmin = $tasks -or $rules -or $fw -or (Test-Path -LiteralPath $script:ProgramDataDir)
    $lines = @()
    if ($inDistro) { $lines += "在 $Distro 里卸载 AcornFox 的服务和容器（用本版本自带的卸载脚本，要联网下载它；卸不干净就停下，不动 Windows 这一侧）" + $(if ($Purge) { '，连数据一起删' } else { '，保留应用数据' }) }
    if ($needAdmin) { $lines += '拿掉计划任务、局域网转发规则、防火墙规则（要管理员确认一次）' }
    $lines += '拿掉 Windows 上的 acornfox 命令、用户 PATH 里加的那一项、.wslconfig 里加的两项、重启后接着装的登记'
    if ($Purge -and $distroThere) { $lines += "注销 WSL 发行版 $Distro，删掉它的全部数据（不可恢复）" }
    if ($Purge) { $lines += "删掉 $script:Root（含下载的文件、本机钥匙、操作记录）" }
    else { $lines += "保留：发行版 $Distro 和里面的数据、$script:Root 里的下载文件、本机钥匙、操作记录；以后重新安装会接上" }
    $lines += '不动：WSL 功能本身（虚拟机平台、WSL 程序）——电脑上别的东西可能在用；也不动 AI 工作台里装的 Skill（acornfox skill install 装的，不想要就自己删 ~\.claude\skills\acornfox 和 ~\.codex\skills\acornfox）'
    Show-PlanList $lines
    if ($Plan) { Say '（试跑）以上只是计划，什么都没有改。'; return }
    if ($Purge -and -not $Yes) {
        $interactive = [Environment]::UserInteractive -and -not [Console]::IsInputRedirected
        if (-not $interactive) { Stop-WithFault 'WIN-UNINST-01' '-Purge 会连数据一起删，不可恢复；确认要删就加 -Yes' }
        $a = Read-Host '这会删掉 AcornFox 里的全部应用和数据，不可恢复。确认请输入 yes'
        if ($a -ne 'yes') { Say '没有确认，什么都没做。'; return }
    }
    if (-not (Test-Path -LiteralPath $script:Logs)) { Ensure-Dirs }
    Write-Log "uninstall purge=$Purge"
    $retry = Get-UninstallRetryText
    # 先卸 WSL 里的：它没成功就停下，Windows 这一侧（acornfox 命令、计划任务）原样留着，用户还能重试。
    # -Purge 例外：下面要注销整个发行版，里面的东西连同服务、容器一起没有，脚本没成功也往下做。
    if ($inDistro) {
        $why = Invoke-InsideUninstall
        if ($why) {
            if (-not $Purge) { Stop-WithFault 'WIN-UNINST-02' ("$why`n    下一步：$retry") }
            SayWarn "$why；-Purge 下面会注销整个发行版，所以继续"
        } else { Add-Step 'wsl-uninstall' 'done' '' }
    }
    if ($Purge -and $distroThere) {
        $r = Invoke-Native 'wsl.exe' @('--unregister', $Distro) -TimeoutSec 600
        $out = (($r.Out + $r.Err) -replace "`0", '').Trim()
        Write-Log "wsl --unregister rc=$($r.ExitCode) $out"
        if ($r.ExitCode -ne 0) { Stop-WithFault 'WIN-UNINST-03' ("注销发行版没成功：$out`n    下一步：$retry") }
        Add-Step 'wsl-unregister' 'done' ''
        # 在桌面上导入发行版时 WSL 会在开始菜单建一个以发行版命名的文件夹，注销后留下空文件夹；空的才删，别人放了东西的不动
        $sm = Join-Path $env:APPDATA ('Microsoft\Windows\Start Menu\Programs\' + $Distro)
        if ((Test-Path -LiteralPath $sm) -and -not (Get-ChildItem -LiteralPath $sm -Force -ErrorAction SilentlyContinue)) { Remove-Item -LiteralPath $sm -Force -ErrorAction SilentlyContinue }
    }
    if ($needAdmin) {
        $res = Invoke-AdminStep @('remove') @{}
        Add-Step 'admin-remove' 'done' ($res.done -join ',')
    }
    Remove-WslConfigAdded
    Clear-ResumeAfterReboot
    $cur = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($cur -and (@($cur -split ';') -contains $script:BinDir)) {
        [Environment]::SetEnvironmentVariable('Path', ((@($cur -split ';') | Where-Object { $_ -and $_ -ne $script:BinDir }) -join ';'), 'User')
    }
    if ($Purge) {
        # 能马上删的马上删（下载的文件、发行版目录、钥匙、记录）；本脚本自己的副本和转给它的 acornfox.cmd 所在的 bin 可能正在运行，
        # 最后交给另一个进程，过几秒（等它们都退出）再删整个目录。从这里起不再写操作记录（否则会把刚删的 logs 又建出来）
        $script:NoLog = $true
        foreach ($item in @(Get-ChildItem -LiteralPath $script:Root -Force -ErrorAction SilentlyContinue | Where-Object { $_.Name -notin @('bin', 'afx-win.ps1') })) {
            Remove-Item -LiteralPath $item.FullName -Recurse -Force -ErrorAction SilentlyContinue
        }
        Remove-LaterPaths @($script:Root)
        Say '已卸载，连同发行版、数据和 AcornFox 在这台电脑上的文件都删掉了（目录的最后一点过几秒由后台删掉）。'
    } else {
        Set-StateValue @{ installed = $false; uninstalled_at = (Get-Date -Format 'o') }
        # 保留数据：拿掉“程序”这一部分（acornfox 命令、本脚本的副本、临时文件）；发行版和里面的数据（wsl 目录）、
        # 下载的镜像、本机钥匙、操作记录和安装记录留着，重新安装接得上。
        Remove-Item -LiteralPath $script:Work -Recurse -Force -ErrorAction SilentlyContinue
        Remove-LaterPaths @($script:BinDir, $script:SelfCopy)
        Say "已卸载（保留数据）：发行版 $Distro 和里面的应用数据都还在，acornfox 命令已拿掉；重新运行安装命令会接上旧数据。连数据一起删：装上之前的那条安装命令末尾加 -Action uninstall -Purge -Yes。"
    }
}

function Set-LanMode([bool]$On) {
    $pre = Invoke-Preflight
    if ($On -and -not $pre.admin_account) { Stop-WithFault 'WIN-LAN-02' '' }
    Show-PlanList @($(if ($On) { '登记局域网转发任务和防火墙规则（只在专用网络放行 18802、18810-18899）；要管理员确认一次' } else { '拿掉局域网转发任务、转发规则和防火墙规则，改回只登记开机自启；要管理员确认一次' }))
    if ($Plan) { Say '（试跑）以上只是计划，什么都没有改。'; return }
    Ensure-Dirs
    $res = Invoke-AdminStep @('tasks') @{ lan = $On }
    Set-StateValue @{ lan = $On }
    Add-Step 'lan' 'done' ($res.done -join ',')
    if ($On) { [void](Wait-LanForward (Get-WinLanIp)) }
    Say $(if ($On) { '局域网访问已打开。' } else { '局域网访问已关闭，应用只在这台电脑上能打开。' })
}

# 逐项检查（只读）。每项 @{ id; state = ok|warn|fail|info; text; code }
function Get-Checks {
    $c = New-Object System.Collections.ArrayList
    function Add-Check([string]$Id, [string]$State, [string]$Text, [string]$Code = '') { [void]$c.Add([pscustomobject]@{ id = $Id; state = $State; text = $Text; code = $Code }) }
    $build = Get-WindowsBuild
    Add-Check 'windows' $(if ($build -ge 19045) { 'ok' } else { 'fail' }) "Windows 版本号 $build" $(if ($build -lt 19045) { 'WIN-OS-01' } else { '' })
    $arch = $env:PROCESSOR_ARCHITEW6432; if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    Add-Check 'arch' $(if ($arch -eq 'AMD64') { 'ok' } else { 'fail' }) "处理器架构 $arch" $(if ($arch -ne 'AMD64') { 'WIN-ARCH-01' } else { '' })
    $blocked = $false
    foreach ($k in @('HKLM:\SOFTWARE\Policies\WSL', 'HKLM:\SOFTWARE\Policies\Microsoft\WSL')) { try { $v = (Get-ItemProperty -LiteralPath $k -ErrorAction Stop).AllowWSL; if ($null -ne $v -and [int]$v -eq 0) { $blocked = $true } } catch { } }
    Add-Check 'policy' $(if ($blocked) { 'fail' } else { 'ok' }) $(if ($blocked) { '策略禁止 WSL' } else { '没有策略禁止 WSL' }) $(if ($blocked) { 'WIN-POLICY-01' } else { '' })
    $free = Get-SystemDriveFreeGB
    Add-Check 'disk' $(if ($free -ge $script:MinFreeGB) { 'ok' } else { 'warn' }) "系统盘剩 $free GB" $(if ($free -lt $script:MinFreeGB) { 'WIN-DISK-01' } else { '' })
    $w = Get-WslReadiness
    Add-Check 'wsl' $(if ($w.ready) { 'ok' } else { 'fail' }) "WSL 版本 $($w.version)，虚拟机平台 $(if ($w.vmp) { '已打开' } else { '没打开' })" $(if (-not $w.ready) { 'WIN-WSL-01' } else { '' })
    $exists = $false; if ($w.ready) { $exists = Test-DistroExists }
    Add-Check 'distro' $(if ($exists) { 'ok' } else { 'fail' }) $(if ($exists) { "发行版 $Distro 在（$(if (Test-DistroRunning) { '运行中' } else { '没在运行，下面的检查会把它叫起来' })）" } else { "没有发行版 $Distro" }) $(if (-not $exists) { 'WIN-WSL-02' } else { '' })
    if ($exists) {
        $sd = (Invoke-Wsl @('/bin/systemctl', 'is-system-running') -TimeoutSec 90).Out.Trim()
        Add-Check 'systemd' $(if ($sd -in 'running', 'degraded') { 'ok' } else { 'fail' }) "systemd：$sd" $(if ($sd -notin 'running', 'degraded') { 'WIN-WSL-03' } else { '' })
        $v = Get-DistroAcornFoxVersion
        $srv = (Invoke-Wsl @('/bin/systemctl', 'is-active', 'acornfox-server') -TimeoutSec 30).Out.Trim()
        $run = (Invoke-Wsl @('/bin/systemctl', 'is-active', 'acornfox-runner') -TimeoutSec 30).Out.Trim()
        $okA = ($v -ne '' -and $srv -eq 'active' -and $run -eq 'active')
        Add-Check 'acornfox' $(if ($okA) { 'ok' } else { 'fail' }) "AcornFox $v：acornfox-server $srv，acornfox-runner $run" $(if (-not $okA) { 'WIN-LINUX-01' } else { '' })
        $code = Test-HttpOk "http://localhost:$($script:ConsolePort)/" 10
        Add-Check 'console' $(if ($code -gt 0) { 'ok' } else { 'fail' }) "http://localhost:$($script:ConsolePort)/ 返回 $code" $(if ($code -eq 0) { 'WIN-NET-01' } else { '' })
        # Windows 用户目录里已装的 Skill（acornfox skill install 装的）：文件头的版本号要和 WSL 里的程序一致
        $skillFound = @(); $skillBad = @()
        foreach ($sk in @(@('Claude Code', '.claude'), @('Codex', '.codex'))) {
            $skf = Join-Path $env:USERPROFILE ($sk[1] + '\skills\acornfox\SKILL.md')
            if (-not (Test-Path -LiteralPath $skf)) { continue }
            $have = ''
            try { if ([IO.File]::ReadAllText($skf, [Text.Encoding]::UTF8) -match '(?m)^<!-- acornfox-skill-version: (\S+) -->\s*$') { $have = $Matches[1] } } catch { }
            $skillFound += $sk[0]
            if ($v -and $have -ne $v) { $skillBad += ("{0} 里的 Skill 是 {1}，WSL 里的 AcornFox 是 {2}" -f $sk[0], $(if ($have) { "$have 版" } else { '旧版本写的（没有版本号）' }), "$v 版") }
        }
        if ($skillBad.Count -gt 0) { Add-Check 'skill' 'warn' ("Skill 要更新：" + ($skillBad -join '；') + '（运行 acornfox skill install）') 'WIN-SKILL-01' }
        elseif ($skillFound.Count -gt 0) { Add-Check 'skill' 'ok' ("Skill：" + ($skillFound -join '、') + " 里的 AcornFox Skill 和程序都是 $v 版") }
    }
    $cfg = Join-Path $env:USERPROFILE '.wslconfig'
    $okCfg = (Test-Path -LiteralPath $cfg) -and ([IO.File]::ReadAllText($cfg) -match '(?im)^\s*instanceIdleTimeout\s*=\s*-1') -and ([IO.File]::ReadAllText($cfg) -match '(?im)^\s*vmIdleTimeout\s*=\s*-1')
    Add-Check 'wslconfig' $(if ($okCfg) { 'ok' } else { 'warn' }) $(if ($okCfg) { '.wslconfig 里有让 WSL 一直开着的设置' } else { '.wslconfig 里没有让 WSL 一直开着的设置：没人用时 WSL 会停，应用会打不开' }) $(if (-not $okCfg) { 'WIN-START-01' } else { '' })
    $st = Read-State
    $lan = [bool](Get-StateValue $st 'lan' $false)
    $tLan = Get-TaskInfo $script:TaskLan; $tWsl = Get-TaskInfo $script:TaskWsl
    $okT = $(if ($lan) { $null -ne $tLan } else { $null -ne $tWsl })
    Add-Check 'autostart' $(if ($okT) { 'ok' } else { 'fail' }) $(if ($okT) { "开机自启任务在（$(if ($lan) { $script:TaskLan } else { $script:TaskWsl })）" } else { '开机自启任务没有登记：重启电脑后 AcornFox 不会自己起来' }) $(if (-not $okT) { 'WIN-START-01' } else { '' })
    $okW = (Test-Path -LiteralPath (Join-Path $script:BinDir 'acornfox.cmd')) -and (@([Environment]::GetEnvironmentVariable('Path', 'User') -split ';') -contains $script:BinDir)
    Add-Check 'cli' $(if ($okW) { 'ok' } else { 'fail' }) $(if ($okW) { "acornfox 命令在 $script:BinDir，已加进用户 PATH" } else { 'acornfox 命令没有装好或不在 PATH 里' }) $(if (-not $okW) { 'WIN-CLI-01' } else { '' })
    if ($lan) {
        $rules = Get-ManagedRules
        $fw = $false; try { $fw = [bool](Get-NetFirewallRule -Name $script:FirewallRule -ErrorAction Stop) } catch { }
        $ip = Get-WinLanIp
        $cat = Get-LanNetworkCategory $ip
        $wip = ''; if ($exists -and (Test-DistroRunning)) { $wip = Get-WslIp }
        $stale = @($rules.Values | Where-Object { $wip -and $_ -ne $wip }).Count
        $okL = ($fw -and $null -ne $tLan -and $stale -eq 0)
        Add-Check 'lan' $(if ($okL -and $cat -ne 'Public') { 'ok' } elseif ($okL) { 'warn' } else { 'fail' }) ("局域网访问：转发 $($rules.Count) 个端口，防火墙规则 $(if ($fw) { '在' } else { '没有' })，网络类型 $cat，局域网地址 $ip" + $(if ($stale) { "，有 $stale 条转到旧地址" } else { '' })) $(if (-not $okL) { 'WIN-LAN-01' } elseif ($cat -eq 'Public') { 'WIN-LAN-03' } else { '' })
    } else {
        Add-Check 'lan' 'info' '局域网访问：没打开（应用只在这台电脑上能打开；想开：acornfox windows lan-on）'
    }
    Add-Check 'gateway' 'info' $(if (Test-ReleaseGatewayEnabled) { "下载网关：$(Get-GatewayUrl)（出问题时自动退回官方地址）" } else { '下载网关：已关闭' })
    return $c
}

function Invoke-Status {
    $checks = Get-Checks
    $mark = @{ ok = '通过'; warn = '注意'; fail = '失败'; info = '信息' }
    foreach ($x in $checks) {
        $line = ("  [{0}] {1}" -f $mark[$x.state], $x.text)
        if ($x.code) { $line += "（故障编号 $($x.code)）" }
        $color = @{ ok = 'Green'; warn = 'Yellow'; fail = 'Red'; info = 'Gray' }[$x.state]
        Write-Host $line -ForegroundColor $color
        Add-Step $x.id $x.state $x.text
        if ($x.code -and $x.state -in 'warn', 'fail') {
            $t = $script:FaultText[$x.code]
            [void]$script:Faults.Add([pscustomobject]@{ code = $x.code; message = $t[0]; hint = $t[1]; detail = $x.text })
        }
    }
    $bad = @($checks | Where-Object { $_.state -eq 'fail' })
    if ($bad.Count -gt 0) {
        Write-Host ''
        Write-Host '怎么办：'
        foreach ($b in $bad) { Write-Host "  $($b.code)：$($script:FaultText[$b.code][1])" }
        $script:ExitCode = 1
    }
}

function Invoke-LanSyncOnce {
    $f = Join-Path $script:Work 'afx-lan-sync-once.ps1'
    Ensure-Dirs
    [IO.File]::WriteAllText($f, $script:SyncScriptText, (New-Object Text.UTF8Encoding($true)))
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $f -Distro $Distro -Once
}

# ---------------------------------------------------------------------------------------------------------
# 入口
# ---------------------------------------------------------------------------------------------------------
# 只给测试用：环境变量 AFX_WIN_LIB=1 并且是点引入（. .\install-windows.ps1）时，只定义函数、不执行，
# 这样可以单独测下载、检查这些函数（例如在限速的假网关上测分段多路下载）。
if ($env:AFX_WIN_LIB -eq '1' -and $MyInvocation.InvocationName -eq '.') { return }
try {
    switch ($Action) {
        'install'   { Invoke-Install }
        'resume'    { Invoke-Install }
        'upgrade'   { Invoke-Upgrade }
        'uninstall' { Invoke-Uninstall }
        'status'    { Invoke-Status }
        'doctor'    { Invoke-Status }
        'lan-on'    { Set-LanMode $true }
        'lan-off'   { Set-LanMode $false }
        'lan-sync'  { Invoke-LanSyncOnce }
    }
} catch {
    if ($_.Exception.Message -ne 'AFX_FAULT') {
        Write-Host "[AcornFox] 出错：$($_.Exception.Message)" -ForegroundColor Red
        Write-Host '故障编号: WIN-LINUX-01（没有更具体的编号时先看操作记录）'
        Write-Log ("意外出错 " + $_.Exception.ToString())
        $script:ExitCode = 1
    }
}
if ($Json) {
    $out = [ordered]@{ ok = ($script:ExitCode -eq 0); action = $Action; exit_code = $script:ExitCode; steps = @($script:Steps); faults = @($script:Faults) }
    Write-Output ($out | ConvertTo-Json -Compress -Depth 6)
}
exit $script:ExitCode
