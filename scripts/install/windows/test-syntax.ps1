# Windows 安装脚本的语法检查（只读）：在 Windows 上用 PowerShell 5.1 运行，不改任何东西。
#
# 做什么：用 PowerShell 自己的解析器检查 get.ps1、install-windows.ps1、afx-gw-client.ps1，以及 install-windows.ps1 里嵌着的
#   四段脚本（管理员那一步、局域网同步、acornfox 命令的 .ps1）有没有语法错误；检查文件带 BOM；检查没有把中文引号写进双引号字符串
#   （PowerShell 把「“」「”」当成双引号，写在双引号字符串里会把字符串提前结束）。
# 什么时候用：改了这几个脚本之后。Mac 和 Linux 上没有 PowerShell，所以要在测试用的 Windows 虚拟机上跑
#   （例如 windows-devbox：把本目录和上一级的两个脚本传过去，运行 powershell -File test-syntax.ps1）。
# 前提：Windows PowerShell 5.1（系统自带）。
# 怎么运行：powershell -NoProfile -ExecutionPolicy Bypass -File test-syntax.ps1 [-Dir 脚本所在目录]
#   -Dir 默认是本文件上一级目录（scripts\install），get.ps1 找 -Dir\..\..\get.ps1，也可以在 -Dir 里放一份。
# 怎么验证：最后一行“通过 N/N”，退出码 0；不对的项打印“不对”和位置。
param([string]$Dir = (Split-Path -Parent $PSScriptRoot))
$ErrorActionPreference = 'Stop'
$pass = 0; $total = 0
function Ok($m) { $script:total++; $script:pass++; Write-Host "对    $m" }
function Bad($m, $d) { $script:total++; Write-Host "不对  $m"; if ($d) { Write-Host "        $d" } }
function Test-Parse([string]$Name, [string]$Text) {
    $errs = $null; $tokens = $null
    [void][System.Management.Automation.Language.Parser]::ParseInput($Text, [ref]$tokens, [ref]$errs)
    if ($errs.Count -eq 0) { Ok "$Name：语法没问题" } else { Bad "$Name：语法有错" (($errs | Select-Object -First 3 | ForEach-Object { "{0}:{1} {2}" -f $_.Extent.StartLineNumber, $_.Extent.StartColumnNumber, $_.Message }) -join ' | ') }
}
$files = @(
    @{ n = 'install-windows.ps1'; p = (Join-Path $Dir 'install-windows.ps1') },
    @{ n = 'afx-gw-client.ps1'; p = (Join-Path $Dir 'windows\afx-gw-client.ps1') },
    @{ n = 'get.ps1'; p = (Join-Path $Dir '..\..\get.ps1') }
)
foreach ($f in $files) {
    if (-not (Test-Path -LiteralPath $f.p)) { Bad "$($f.n)：找不到 $($f.p)" ''; continue }
    $b = [IO.File]::ReadAllBytes($f.p)
    if ($b.Length -ge 3 -and $b[0] -eq 0xEF -and $b[1] -eq 0xBB -and $b[2] -eq 0xBF) { Ok "$($f.n)：带 BOM" } else { Bad "$($f.n)：没有 BOM（英文 Windows 上会读错中文）" '' }
    $text = [Text.Encoding]::UTF8.GetString($b).TrimStart([char]0xFEFF)
    Test-Parse $f.n $text
    # 中文引号不能出现在双引号字符串里
    $errs = $null; $tokens = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseInput($text, [ref]$tokens, [ref]$errs)
    $badq = @($ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.StringConstantExpressionAst] -or $n -is [System.Management.Automation.Language.ExpandableStringExpressionAst] }, $true) |
        Where-Object { $_.Extent.Text -match '[\u201c\u201d]' -and $_.Extent.Text.StartsWith('"') })
    if ($badq.Count -eq 0) { Ok "$($f.n)：双引号字符串里没有中文引号" } else { Bad "$($f.n)：双引号字符串里有中文引号" ("第 " + $badq[0].Extent.StartLineNumber + " 行") }
    if ($f.n -eq 'install-windows.ps1') {
        # 嵌着的脚本：找 $script:xxx = @'…'@ 的内容
        $names = @('SyncScriptText', 'AdminTemplate', 'WrapperPs1', 'WrapperCmd')
        foreach ($n in $names) {
            $m = [regex]::Match($text, '(?s)\$script:' + $n + " = @'\r?\n(.*?)\r?\n'@")
            if (-not $m.Success) { Bad "嵌入的 $n：没找到" ''; continue }
            $body = $m.Groups[1].Value
            if ($n -eq 'WrapperCmd') { if ($body -match 'acornfox\.ps1') { Ok "嵌入的 $n：转给 acornfox.ps1" } else { Bad "嵌入的 $n：内容不对" '' }; continue }
            Test-Parse "嵌入的 $n" ($body.Replace('@@CFG@@', '{}').Replace('@@DISTRO@@', 'AcornFox').Replace('@@SELF@@', 'C:\x.ps1'))
        }
    }
}
Write-Host "通过 $pass/$total"
if ($pass -ne $total) { exit 1 }
