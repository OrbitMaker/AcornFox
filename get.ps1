# AcornFox 的 Windows 引导脚本：在 Windows 上安装、升级、卸载、检查的唯一入口（和 Linux、Mac 上的 get.sh 对应）。
#
# 它自己不装任何东西，只做五件事：选下载源 → 定版本 → 下载「那个版本自带的」install-windows.ps1 和校验文件 → 校验 → 把参数交给它。
# 所以安装脚本和程序永远来自同一个版本，不会出现新脚本配旧程序。
#
# 用法（在 PowerShell 里，普通用户身份，不要用「以管理员身份运行」）：
#   国内： powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol=3072; & ([scriptblock]::Create([Text.Encoding]::UTF8.GetString((New-Object Net.WebClient).DownloadData('https://gitee.com/VIP13390/AcornFox/raw/main/get.ps1')).TrimStart([char]0xFEFF)))"
#   海外： powershell -NoProfile -ExecutionPolicy Bypass -Command "[Net.ServicePointManager]::SecurityProtocol=3072; & ([scriptblock]::Create([Text.Encoding]::UTF8.GetString((New-Object Net.WebClient).DownloadData('https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.ps1')).TrimStart([char]0xFEFF)))"
#   （这样写是为了按 UTF-8 读文件：直接用 irm | iex 时，英文系统上中文提示可能乱码，但功能不受影响。）
#   要带参数：在最后一个括号后面接参数，例如  … ) -Action upgrade   或   … ) -Action uninstall -Purge -Yes
#   已经下载到本地：powershell -NoProfile -ExecutionPolicy Bypass -File get.ps1 [参数]
#
# 参数（不写 -Action 就是安装）：
#   -Action install|upgrade|uninstall|status|doctor|lan-on|lan-off   做什么（上面这些动作都交给 install-windows.ps1，它的说明里有详细解释）
#   -Version v0.7.0     指定版本（含预发布版）；不写就用最新正式版
#   -China / -Global    先用 Gitee（国内）还是 GitHub（海外）取文件；都不写就先试 GitHub 通不通
#   -ReleaseBase 地址   测试或自建镜像：只从 <地址>/releases/download/<版本>/ 取（<地址>/latest 里一行写最新正式版号）；也可设环境变量 ACORNFOX_RELEASE_BASE
#   其余参数原样交给 install-windows.ps1：-Plan（只说要做什么）、-Json（标准输出只留最后一行 JSON，引导自己的进度话也不打印）、-Yes、-Purge、-Image debian|ubuntu、-NoGateway、
#   -LocalOnly、-NoOpen、-LocalPackage 目录、-Distro 名字、-Gateway 地址
# 校验：下载的 install-windows.ps1 要和同一版本 SHA256SUMS 里的值一致，不一致就停下、不执行。
# 退出码：交给 install-windows.ps1 之后就是它的退出码（0 成功、1 出错、3 要重启、5 管理员确认被取消）；引导自己出错是 1，并打印故障编号：
#   INSTALL-GET-01 查不到最新正式版号   INSTALL-GET-02 下载失败（两个源都试过）   INSTALL-GET-03 校验不过   INSTALL-GET-04 用法不对
# 副作用：只在临时目录里放下载的脚本和校验文件，退出时删掉。真正的改动都由 install-windows.ps1 做。
# 环境变量：ACORNFOX_REPO、ACORNFOX_GITEE_REPO（换仓库）、ACORNFOX_RELEASE_BASE（见上）。
# 出错先看哪里：屏幕上的故障编号和它上面几行；能连上哪个源：在浏览器里打开 https://gitee.com 和 https://github.com 试试。
# 本文件含中文，必须存成带 BOM 的 UTF-8。

[CmdletBinding()]
param(
    [ValidateSet('install', 'resume', 'upgrade', 'uninstall', 'status', 'doctor', 'lan-on', 'lan-off')]
    [string]$Action = 'install',
    [string]$Version = '',
    [switch]$China,
    [switch]$Global,
    [string]$ReleaseBase = '',
    [switch]$Purge,
    [switch]$Plan,
    [switch]$Json,
    [switch]$Yes,
    [ValidateSet('debian', 'ubuntu')]
    [string]$Image = 'debian',
    [switch]$NoGateway,
    [switch]$LocalOnly,
    [switch]$NoOpen,
    [string]$LocalPackage = '',
    [string]$Distro = 'AcornFox',
    [string]$Gateway = ''
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
try { [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072 } catch { }

$GithubRepo = $env:ACORNFOX_REPO; if (-not $GithubRepo) { $GithubRepo = 'OrbitMaker/AcornFox' }
$GiteeRepo = [Environment]::GetEnvironmentVariable('ACORNFOX_GITEE_REPO'); if ($null -eq $GiteeRepo) { $GiteeRepo = 'VIP13390/AcornFox' }
$Base = $ReleaseBase; if (-not $Base) { $Base = $env:ACORNFOX_RELEASE_BASE }

# -Json 时标准输出只留 install-windows.ps1 最后输出的那一行 JSON：引导的进度话不打印（出错的话 Fail 照常打印，那时还没有 JSON）
function Say([string]$t) { if (-not $Json) { Write-Host "[引导] $t" } }
function Fail([int]$Rc, [string]$Code, [string]$Msg, [string[]]$Hints = @()) {
    Write-Host "[引导] 出错：$Msg" -ForegroundColor Red
    foreach ($h in $Hints) { Write-Host "    $h" }
    Write-Host "故障编号: $Code"
    exit $Rc
}
function Test-Reachable([string]$Url, [int]$Sec) {
    try { $r = [Net.WebRequest]::Create($Url); $r.Method = 'HEAD'; $r.Timeout = $Sec * 1000; $x = $r.GetResponse(); $x.Close(); return $true }
    catch [Net.WebException] { return ($null -ne $_.Exception.Response) } catch { return $false }
}
function Get-Bytes([string]$Url, [int]$Sec = 60) {
    $wc = New-Object Net.WebClient
    try { return $wc.DownloadData($Url) } finally { $wc.Dispose() }
}
function Get-Text([string]$Url, [int]$Sec = 20) {
    try { $b = Get-Bytes $Url $Sec; return [Text.Encoding]::UTF8.GetString($b).TrimStart([char]0xFEFF) } catch { return '' }
}

# 选源：结果是按先后排好的源名字（base、gitee、github）
function Get-SourceOrder {
    if ($Base) { return @('base') }
    if (-not $GiteeRepo) { return @('github') }
    $prefer = ''
    if ($China) { $prefer = 'gitee' } elseif ($Global) { $prefer = 'github' }
    if (-not $prefer) { if (Test-Reachable "https://github.com/$GithubRepo/releases/latest" 5) { $prefer = 'github' } else { $prefer = 'gitee' } }
    if ($prefer -eq 'gitee') { return @('gitee', 'github') } else { return @('github', 'gitee') }
}
function Get-AssetUrl([string]$Src, [string]$Ver, [string]$Name) {
    switch ($Src) {
        'base'   { return ($Base.TrimEnd('/') + "/releases/download/$Ver/$Name") }
        'gitee'  { return "https://gitee.com/$GiteeRepo/releases/download/$Ver/$Name" }
        'github' { return "https://github.com/$GithubRepo/releases/download/$Ver/$Name" }
    }
}
# 最新正式版号（不带「-」的预发布标签不算）
function Get-LatestFormal([string[]]$Order) {
    foreach ($src in $Order) {
        $tag = ''
        switch ($src) {
            'base' { $tag = (Get-Text ($Base.TrimEnd('/') + '/latest') 15).Trim() }
            'gitee' {
                $t = Get-Text "https://gitee.com/api/v5/repos/$GiteeRepo/releases?page=1&per_page=20&direction=desc" 15
                foreach ($m in [regex]::Matches($t, '"tag_name":"([^"]*)"')) { if ($m.Groups[1].Value -notmatch '-') { $tag = $m.Groups[1].Value; break } }
            }
            'github' {
                try {
                    $r = [Net.WebRequest]::Create("https://github.com/$GithubRepo/releases/latest"); $r.Method = 'HEAD'; $r.Timeout = 15000; $r.AllowAutoRedirect = $true
                    $x = $r.GetResponse(); $u = $x.ResponseUri.AbsoluteUri; $x.Close()
                    if ($u -match '/releases/tag/(v[^/?]+)') { $tag = $Matches[1] }
                } catch { }
            }
        }
        if ($tag -match '^v\d+\.\d+\.\d+$') { return $tag }
    }
    return ''
}

if ($Version) {
    if ($Version -notmatch '^v') { $Version = "v$Version" }
    if ($Version -notmatch '^v\d+\.\d+\.\d+(-[A-Za-z0-9.]+)?$') { Fail 2 'INSTALL-GET-04' "版本号写成 v0.7.0 或 v0.7.0-rc.1 这样：$Version" }
}

$order = Get-SourceOrder
if (-not $Version) {
    $Version = Get-LatestFormal $order
    if (-not $Version) {
        Fail 1 'INSTALL-GET-01' ("查不到最新的正式版号（" + ($order -join '、') + ' 都试过）') @('先看浏览器能不能打开 https://gitee.com 和 https://github.com', '也可以直接指定版本：在命令后面加 -Version v0.7.0')
    }
    Say "最新正式版：$Version"
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) ('acornfox-get-' + [Guid]::NewGuid().ToString('N').Substring(0, 8))
[void](New-Item -ItemType Directory -Path $tmp)
try {
    $script = Join-Path $tmp 'install-windows.ps1'
    $got = $false; $used = ''
    foreach ($src in $order) {
        try { [IO.File]::WriteAllBytes($script, (Get-Bytes (Get-AssetUrl $src $Version 'install-windows.ps1') 120)); $got = $true; $used = $src; break } catch { }
    }
    if (-not $got) { Fail 1 'INSTALL-GET-02' ("下载 $Version 的 install-windows.ps1 失败（" + ($order -join '、') + ' 都试过）') @("核对版本号是否存在：https://gitee.com/$GiteeRepo/releases", '网络不通时过一会儿再试') }
    $sums = ''
    foreach ($src in @($used) + @($order | Where-Object { $_ -ne $used })) { $sums = Get-Text (Get-AssetUrl $src $Version 'SHA256SUMS') 30; if ($sums) { break } }
    if (-not $sums) { Fail 1 'INSTALL-GET-02' "下载 $Version 的 SHA256SUMS 失败，没法校验 install-windows.ps1，不执行" @('过一会儿再试') }
    $want = ''
    foreach ($l in ($sums -split "`r?`n")) { if ($l -match '^([0-9a-fA-F]{64})\s+\*?install-windows\.ps1\s*$') { $want = $Matches[1].ToLower() } }
    if (-not $want) { Fail 1 'INSTALL-GET-03' "$Version 的 SHA256SUMS 里没有 install-windows.ps1 这一项，没法校验，不执行" @('这个版本的发行版不完整，先反馈；也可以换一个版本') }
    $h = [Security.Cryptography.SHA256]::Create()
    $have = ([BitConverter]::ToString($h.ComputeHash([IO.File]::ReadAllBytes($script))).Replace('-', '')).ToLower()
    if ($have -ne $want) {
        Fail 1 'INSTALL-GET-03' "install-windows.ps1 校验不过：发行版记的是 $want，下载到的是 $have。没有执行它" @('多半是下载被截断或被替换。换一个源再试：加 -China 或 -Global', '两个源都不过时不要强行安装，先反馈')
    }
    Say "install-windows.ps1 校验通过（来自 $used）"

    $a = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $script, $Action)
    if ($Version) { $a += @('-Version', $Version) }
    if ($China) { $a += '-China' }; if ($Global) { $a += '-Global' }
    if ($Base) { $a += @('-ReleaseBase', $Base) }
    if ($Purge) { $a += '-Purge' }; if ($Plan) { $a += '-Plan' }; if ($Json) { $a += '-Json' }; if ($Yes) { $a += '-Yes' }
    if ($Image -ne 'debian') { $a += @('-Image', $Image) }
    if ($NoGateway) { $a += '-NoGateway' }; if ($LocalOnly) { $a += '-LocalOnly' }; if ($NoOpen) { $a += '-NoOpen' }
    if ($LocalPackage) { $a += @('-LocalPackage', $LocalPackage) }
    if ($Distro -ne 'AcornFox') { $a += @('-Distro', $Distro) }
    if ($Gateway) { $a += @('-Gateway', $Gateway) }
    Say "交给 $Version 的 install-windows.ps1（$Action）"
    & powershell.exe @a
    $rc = $LASTEXITCODE
} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
exit $rc
