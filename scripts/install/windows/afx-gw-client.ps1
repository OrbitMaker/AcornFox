# afx-gw-client.ps1 —— AcornFox 下载网关的 PowerShell 客户端（Windows PowerShell 5.1，.NET Framework，不装模块）
#
# 做什么：生成机器钥匙（ECDSA P-256，系统自带 CNG），算公钥和安装编号，按网关契约签名，
#         向下载网关登记（W1）、要临时下载地址（W2）；还能把钥匙导出成 SEC1 PEM，交给装进 WSL 的 Linux 版 acornfox。
#
# 两种用法：
#   1) 点引入（dot-source）进安装脚本，直接调函数（真执行，不试跑）：
#        . .\afx-gw-client.ps1
#        $k = New-AfxMachineKey                       # 默认 %LOCALAPPDATA%\AcornFox\identity\machine-key.blob
#        $r = Request-AfxDownload -Gateway https://dl.acornfox.com -FileId wsl-x64-3.0.3
#        if ($r.ok) { $url = $r.url } else { <换下一个下载来源> }
#   2) 单独运行（写操作默认试跑，加 --apply 才执行）：
#        powershell -NoProfile -ExecutionPolicy Bypass -File afx-gw-client.ps1 --help
#
# 函数不抛异常给调用方：网关和网络的问题都返回 ok=$false 的对象（带 code，GW- 开头）。
# 退出码（单独运行时）：0 成功；1 网关拒绝；2 参数不对（GW-CLI-05）；3 连不上网关（GW-CLI-03）；
#                       4 本机钥匙出错（GW-CLI-01/02）；5 网关返回的不是预期的 JSON（GW-CLI-04）。
# 任何失败都只说明“网关这个来源不可用”，安装脚本应该换下一个下载来源，不能因此装不上。
#
# 环境变量：
#   AFX_GW_TIMEOUT          每次请求的超时秒数，默认 10
#   AFX_GW_CLIENT_VERSION   登记时上报的客户端版本，默认 0.4.0
#   AFX_GW_VIA              登记时上报的来源，默认 install-ps1
#   AFX_GW_MAX_RETRY_AFTER  遇到 GW-LIM-02 最多等几秒再重试，默认 30
#   AFX_GW_LOG              操作记录文件；设了就每次登记、要地址追加一行（不记下载地址）。
#                           单独运行时默认 %LOCALAPPDATA%\AcornFox\logs\gw-client.log
#
# 注意：本文件含中文，必须存成带 BOM 的 UTF-8，否则英文 Windows 上的 PowerShell 5.1 会解析错。
# 详细说明、测试方法见同目录 README.md。

$script:AfxGwEcs2Magic = 0x32534345   # BCRYPT_ECDSA_PRIVATE_P256_MAGIC（"ECS2"）
$script:AfxGwEcs1Magic = 0x31534345   # BCRYPT_ECDSA_PUBLIC_P256_MAGIC（"ECS1"）

# ---------- 编码小工具 ----------

function ConvertTo-AfxBase64Url([byte[]]$Bytes) {
    return [Convert]::ToBase64String($Bytes).TrimEnd('=').Replace('+', '-').Replace('/', '_')
}

function ConvertFrom-AfxBase64Url([string]$Text) {
    $s = $Text.Replace('-', '+').Replace('_', '/')
    switch ($s.Length % 4) { 2 { $s += '==' } 3 { $s += '=' } 1 { throw 'base64url 长度不对' } }
    return [Convert]::FromBase64String($s)
}

function ConvertTo-AfxHex([byte[]]$Bytes) {
    return ([BitConverter]::ToString($Bytes)).Replace('-', '').ToLowerInvariant()
}

function ConvertTo-AfxBase32Lower([byte[]]$Bytes) {
    $a = 'abcdefghijklmnopqrstuvwxyz234567'
    $sb = New-Object System.Text.StringBuilder
    $buf = 0; $bits = 0
    foreach ($b in $Bytes) {
        $buf = ($buf -shl 8) -bor [int]$b; $bits += 8
        while ($bits -ge 5) { $bits -= 5; [void]$sb.Append($a[($buf -shr $bits) -band 31]) }
        $buf = $buf -band ((1 -shl $bits) - 1)
    }
    if ($bits -gt 0) { [void]$sb.Append($a[($buf -shl (5 - $bits)) -band 31]) }
    return $sb.ToString()
}

function Get-AfxSha256([byte[]]$Bytes) {
    $h = [System.Security.Cryptography.SHA256]::Create()
    try { return $h.ComputeHash($Bytes) } finally { $h.Dispose() }
}

function Join-AfxBytes {
    $ms = New-Object System.IO.MemoryStream
    foreach ($part in $args) { $b = [byte[]]$part; $ms.Write($b, 0, $b.Length) }
    return , $ms.ToArray()
}

function Get-AfxProp($Obj, [string]$Name) {
    if ($null -eq $Obj) { return $null }
    $p = $Obj.PSObject.Properties[$Name]
    if ($null -eq $p) { return $null }
    return $p.Value
}

function New-AfxGwError([string]$Code, [string]$Message, [int]$Http = 0) {
    return [pscustomobject]@{ ok = $false; code = $Code; message = $Message; http_status = $Http }
}

function Write-AfxGwLog([string]$Line) {
    $f = $env:AFX_GW_LOG
    if ([string]::IsNullOrEmpty($f)) { return }
    try {
        $d = Split-Path -Parent $f
        if ($d -and -not (Test-Path -LiteralPath $d)) { [void](New-Item -ItemType Directory -Path $d -Force) }
        $t = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
        [IO.File]::AppendAllText($f, "$t $Line`n", (New-Object System.Text.UTF8Encoding($false)))
    } catch { }
}

# ---------- 钥匙 ----------

function Get-AfxDefaultKeyPath {
    return (Join-Path $env:LOCALAPPDATA 'AcornFox\identity\machine-key.blob')
}

# 只给当前用户完全控制，去掉继承（文件或目录）
function Protect-AfxPath([string]$Path, [switch]$Directory) {
    $sid = [System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value
    $grant = if ($Directory) { "*${sid}:(OI)(CI)(F)" } else { "*${sid}:(F)" }
    $null = & icacls.exe $Path /inheritance:r /grant:r $grant 2>&1
    if ($LASTEXITCODE -ne 0) { throw "icacls 设置权限失败（$Path，退出码 $LASTEXITCODE）" }
}

# 拼一个 EccPrivateBlob（ECS2 + 32 + X + Y + d），测试和导入用
function New-AfxPrivateBlob([byte[]]$X, [byte[]]$Y, [byte[]]$D) {
    if ($X.Length -ne 32 -or $Y.Length -ne 32 -or $D.Length -ne 32) { throw 'X、Y、d 都要 32 字节' }
    return , (Join-AfxBytes ([BitConverter]::GetBytes([int]$script:AfxGwEcs2Magic)) ([BitConverter]::GetBytes([int]32)) $X $Y $D)
}

# 读钥匙文件，检查格式，返回 @{ Blob; X; Y; D }
function Read-AfxKeyBlob([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) { throw "GW-CLI-02 钥匙文件不存在：$Path" }
    $b = [IO.File]::ReadAllBytes($Path)
    if ($b.Length -ne 104 -or [BitConverter]::ToInt32($b, 0) -ne $script:AfxGwEcs2Magic -or [BitConverter]::ToInt32($b, 4) -ne 32) {
        throw "GW-CLI-02 钥匙文件不是 P-256 的 EccPrivateBlob：$Path"
    }
    $x = New-Object byte[] 32; $y = New-Object byte[] 32; $d = New-Object byte[] 32
    [Array]::Copy($b, 8, $x, 0, 32); [Array]::Copy($b, 40, $y, 0, 32); [Array]::Copy($b, 72, $d, 0, 32)
    return @{ Blob = $b; X = $x; Y = $y; D = $d }
}

function Import-AfxCngKey([byte[]]$Blob) {
    return [System.Security.Cryptography.CngKey]::Import($Blob, [System.Security.Cryptography.CngKeyBlobFormat]::EccPrivateBlob)
}

# 由钥匙文件得到公钥（经 CNG 导出 EccPublicBlob，确认钥匙能被系统加载）
function Get-AfxPublicKey([string]$KeyPath = (Get-AfxDefaultKeyPath)) {
    $k = Read-AfxKeyBlob $KeyPath
    $key = Import-AfxCngKey $k.Blob
    try {
        $pub = $key.Export([System.Security.Cryptography.CngKeyBlobFormat]::EccPublicBlob)
    } finally { $key.Dispose() }
    if ($pub.Length -ne 72 -or [BitConverter]::ToInt32($pub, 0) -ne $script:AfxGwEcs1Magic) { throw 'GW-CLI-02 CNG 导出的公钥格式不对' }
    $point = New-Object byte[] 65
    $point[0] = 4
    [Array]::Copy($pub, 8, $point, 1, 64)
    return (ConvertTo-AfxBase64Url $point)
}

function Get-AfxInstallId([string]$PublicKey) {
    $raw = ConvertFrom-AfxBase64Url $PublicKey
    if ($raw.Length -ne 65 -or $raw[0] -ne 4) { throw 'GW-CLI-02 公钥格式不对：要 65 字节的未压缩点' }
    return 'afx-' + (ConvertTo-AfxBase32Lower (Get-AfxSha256 $raw)).Substring(0, 20)
}

# 生成机器钥匙并存成 EccPrivateBlob 文件（只给当前用户）。文件已存在就不覆盖（幂等）。
function New-AfxMachineKey([string]$Path = (Get-AfxDefaultKeyPath)) {
    $created = $false
    if (-not (Test-Path -LiteralPath $Path)) {
        $dir = Split-Path -Parent $Path
        if (-not (Test-Path -LiteralPath $dir)) {
            [void](New-Item -ItemType Directory -Path $dir -Force)
            Protect-AfxPath $dir -Directory
        }
        $p = New-Object System.Security.Cryptography.CngKeyCreationParameters
        $p.ExportPolicy = [System.Security.Cryptography.CngExportPolicies]::AllowPlaintextExport
        # 名字必须是真正的 null（[NullString]::Value）：PowerShell 会把 $null 转成空串 ""，那样会建出一把名为 "" 的持久钥匙
        $key = [System.Security.Cryptography.CngKey]::Create([System.Security.Cryptography.CngAlgorithm]::ECDsaP256, [NullString]::Value, $p)
        if (-not $key.IsEphemeral) { $key.Delete(); throw 'GW-CLI-02 CNG 建出的不是临时钥匙' }
        try { $blob = $key.Export([System.Security.Cryptography.CngKeyBlobFormat]::EccPrivateBlob) } finally { $key.Dispose() }
        $tmp = "$Path.tmp.$PID"
        [IO.File]::WriteAllBytes($tmp, $blob)
        Protect-AfxPath $tmp
        Move-Item -LiteralPath $tmp -Destination $Path
        $created = $true
        Write-AfxGwLog "keygen path=$Path"
    }
    $pub = Get-AfxPublicKey $Path
    return [pscustomobject]@{ ok = $true; path = $Path; pubkey = $pub; install_id = (Get-AfxInstallId $pub); created = $created }
}

# 导出 SEC1 PEM（-----BEGIN EC PRIVATE KEY-----），换行用 LF，不带 BOM。
# 给出 -OutFile 就写文件（只给当前用户）并返回路径；否则返回 PEM 文本。
function Export-AfxMachineKeyPem([string]$KeyPath = (Get-AfxDefaultKeyPath), [string]$OutFile = '') {
    $k = Read-AfxKeyBlob $KeyPath
    # ECPrivateKey ::= SEQUENCE { version 1, privateKey OCTET STRING(32), [0] prime256v1, [1] BIT STRING(04||X||Y) }
    $der = Join-AfxBytes ([byte[]](0x30, 0x77, 0x02, 0x01, 0x01, 0x04, 0x20)) $k.D `
        ([byte[]](0xa0, 0x0a, 0x06, 0x08, 0x2a, 0x86, 0x48, 0xce, 0x3d, 0x03, 0x01, 0x07, 0xa1, 0x44, 0x03, 0x42, 0x00, 0x04)) $k.X $k.Y
    $b64 = [Convert]::ToBase64String($der)
    $sb = New-Object System.Text.StringBuilder
    [void]$sb.Append("-----BEGIN EC PRIVATE KEY-----`n")
    for ($i = 0; $i -lt $b64.Length; $i += 64) { [void]$sb.Append($b64.Substring($i, [Math]::Min(64, $b64.Length - $i))).Append("`n") }
    [void]$sb.Append("-----END EC PRIVATE KEY-----`n")
    $pem = $sb.ToString()
    if ([string]::IsNullOrEmpty($OutFile)) { return $pem }
    [IO.File]::WriteAllText($OutFile, $pem, (New-Object System.Text.ASCIIEncoding))
    Protect-AfxPath $OutFile
    return $OutFile
}

# ---------- 签名 ----------

function New-AfxNonce {
    $b = New-Object byte[] 16
    $r = New-Object System.Security.Cryptography.RNGCryptoServiceProvider
    try { $r.GetBytes($b) } finally { $r.Dispose() }
    return (ConvertTo-AfxBase64Url $b)
}

# 签名串（UTF-8，\n 连接，末尾无换行）；$Body 为 $null 或空数组时按“没有请求体”
function Get-AfxCanonical([string]$Method, [string]$Path, [long]$Timestamp, [string]$Nonce, [byte[]]$Body) {
    if ($null -eq $Body) { $Body = New-Object byte[] 0 }
    $bh = ConvertTo-AfxHex (Get-AfxSha256 $Body)
    return ('AFX-GW-1', $Method.ToUpperInvariant(), $Path, $Timestamp.ToString([Globalization.CultureInfo]::InvariantCulture), $Nonce, $bh) -join "`n"
}

# 返回 r||s（64 字节，IEEE P1363）的 base64url；ECDsaCng.SignData 出来就是 r||s
function New-AfxSignature([string]$KeyPath = (Get-AfxDefaultKeyPath), [string]$Method, [string]$Path, [long]$Timestamp, [string]$Nonce, [byte[]]$Body) {
    $k = Read-AfxKeyBlob $KeyPath
    $data = [Text.Encoding]::UTF8.GetBytes((Get-AfxCanonical $Method $Path $Timestamp $Nonce $Body))
    $key = Import-AfxCngKey $k.Blob
    $e = New-Object System.Security.Cryptography.ECDsaCng($key)
    try {
        $e.HashAlgorithm = [System.Security.Cryptography.CngAlgorithm]::Sha256
        $s = $e.SignData($data)
    } finally { $e.Dispose(); $key.Dispose() }
    if ($s.Length -ne 64) { throw "GW-CLI-02 签名长度不对（$($s.Length)）" }
    return (ConvertTo-AfxBase64Url $s)
}

# ---------- 请求 ----------

function Read-AfxResponseBytes($Stream) {
    $ms = New-Object System.IO.MemoryStream
    try { $Stream.CopyTo($ms) } finally { $Stream.Dispose() }
    return [Text.Encoding]::UTF8.GetString($ms.ToArray())
}

function ConvertFrom-AfxGwBody([string]$Text, [int]$Http) {
    $t = $Text.Trim()
    if (-not $t.StartsWith('{')) { return (New-AfxGwError 'GW-CLI-04' "网关返回的不是 JSON（HTTP $Http）" $Http) }
    try { $o = $t | ConvertFrom-Json } catch { return (New-AfxGwError 'GW-CLI-04' "网关返回的 JSON 解析不了（HTTP $Http）" $Http) }
    if ($null -eq (Get-AfxProp $o 'ok')) { return (New-AfxGwError 'GW-CLI-04' "网关返回里没有 ok 字段（HTTP $Http）" $Http) }
    if ($Http -ne 200 -and (Get-AfxProp $o 'ok') -eq $true) { $o.ok = $false }
    $o | Add-Member -NotePropertyName http_status -NotePropertyValue $Http -Force
    return $o
}

function Invoke-AfxGwHttp([string]$Method, [string]$Uri, [hashtable]$Headers, [byte[]]$Body) {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    $timeout = 10
    if ($env:AFX_GW_TIMEOUT -match '^\d+$') { $timeout = [int]$env:AFX_GW_TIMEOUT }
    $old = $ProgressPreference
    $ProgressPreference = 'SilentlyContinue'
    try {
        $p = @{ Uri = $Uri; Method = $Method; UseBasicParsing = $true; TimeoutSec = $timeout; ErrorAction = 'Stop' }
        if ($Headers) { $p.Headers = $Headers }
        if ($null -ne $Body) { $p.Body = $Body; $p.ContentType = 'application/json' }
        $r = Invoke-WebRequest @p
        return (ConvertFrom-AfxGwBody ([Text.Encoding]::UTF8.GetString($r.RawContentStream.ToArray())) ([int]$r.StatusCode))
    } catch [System.Net.WebException] {
        $err = $_
        $resp = $err.Exception.Response
        if ($null -eq $resp) { return (New-AfxGwError 'GW-CLI-03' "连不上下载网关或超时（$Uri）：$($err.Exception.Message)") }
        $code = [int]$resp.StatusCode
        # PS 5.1 抛错前已经读过响应体：先倒回流的开头按 UTF-8 读；读不到再用 ErrorDetails（编码可能不对，但 code 是 ASCII）
        $text = ''
        try {
            $st = $resp.GetResponseStream()
            if ($st.CanSeek) { $st.Position = 0 }
            $text = Read-AfxResponseBytes $st
        } catch { $text = '' }
        if ([string]::IsNullOrWhiteSpace($text) -and $null -ne $err.ErrorDetails) { $text = [string]$err.ErrorDetails.Message }
        return (ConvertFrom-AfxGwBody $text $code)
    } catch {
        return (New-AfxGwError 'GW-CLI-03' "请求下载网关失败（$Uri）：$($_.Exception.Message)")
    } finally { $ProgressPreference = $old }
}

# 签名并 POST；请求体按 UTF-8 字节发送，签名用同一份字节
function Invoke-AfxGwPost([string]$Gateway, [string]$KeyPath, [string]$Path, [string]$BodyText) {
    try {
        $body = [Text.Encoding]::UTF8.GetBytes($BodyText)
        $pub = Get-AfxPublicKey $KeyPath
        $id = Get-AfxInstallId $pub
        $ts = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
        $n = New-AfxNonce
        $s = New-AfxSignature -KeyPath $KeyPath -Method 'POST' -Path $Path -Timestamp $ts -Nonce $n -Body $body
    } catch {
        return (New-AfxGwError 'GW-CLI-02' "本机钥匙出错：$($_.Exception.Message)")
    }
    $h = @{ 'X-AFX-Install' = $id; 'X-AFX-Timestamp' = "$ts"; 'X-AFX-Nonce' = $n; 'X-AFX-Signature' = $s }
    return (Invoke-AfxGwHttp 'POST' ($Gateway.TrimEnd('/') + $Path) $h $body)
}

function Get-AfxRegisterBody([string]$KeyPath = (Get-AfxDefaultKeyPath)) {
    $pub = Get-AfxPublicKey $KeyPath
    $arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    $arch = if ($arch -eq 'ARM64') { 'arm64' } else { 'amd64' }
    $ver = if ($env:AFX_GW_CLIENT_VERSION) { $env:AFX_GW_CLIENT_VERSION -replace '[^A-Za-z0-9._+-]', '' } else { '0.4.0' }
    $via = if ($env:AFX_GW_VIA) { $env:AFX_GW_VIA -replace '[^A-Za-z0-9._-]', '' } else { 'install-ps1' }
    return ('{{"pubkey":"{0}","client":{{"version":"{1}","platform":"windows","arch":"{2}","via":"{3}"}}}}' -f $pub, $ver, $arch, $via)
}

function Get-AfxIssueBody([string]$FileId, [string]$Purpose = 'runtime') {
    if ($FileId -notmatch '^[A-Za-z0-9._-]{1,128}$') { throw 'GW-CLI-05 文件 ID 只能有字母、数字和 . _ -' }
    if ($Purpose -notin @('runtime', 'app', 'upgrade')) { throw 'GW-CLI-05 用途只能是 runtime、app、upgrade' }
    return ('{{"file_id":"{0}","purpose":"{1}","app":null,"account_token":null}}' -f $FileId, $Purpose)
}

# W1 登记。返回网关的 JSON 对象（多一个 http_status），出错时 ok=$false、code 为 GW- 编号。
function Register-AfxGateway([string]$Gateway, [string]$KeyPath = (Get-AfxDefaultKeyPath)) {
    if ([string]::IsNullOrEmpty($Gateway)) { return (New-AfxGwError 'GW-CLI-05' '用法：Register-AfxGateway -Gateway <网关地址> [-KeyPath <钥匙文件>]') }
    try { $body = Get-AfxRegisterBody $KeyPath } catch { return (New-AfxGwError 'GW-CLI-02' "本机钥匙出错：$($_.Exception.Message)") }
    $r = Invoke-AfxGwPost $Gateway $KeyPath '/v1/register' $body
    Write-AfxGwLog ("register gw=$Gateway http=$(Get-AfxProp $r 'http_status') result=" + $(if ($r.ok) { 'ok' } else { Get-AfxProp $r 'code' }))
    return $r
}

# W2 要临时下载地址。返回网关的 JSON 对象（ok、url、expires_at、sha256、size、quota……，多一个 http_status）。
# 按契约：GW-AUTH-02（没登记）先登记再重试一次；GW-LIM-02 等 retry_after 秒（不超过上限）重试一次。
# 客户端一律用自己写死的官方 SHA256 核对下载内容，返回里的 sha256 只用来比对。
function Request-AfxDownload([string]$Gateway, [string]$KeyPath = (Get-AfxDefaultKeyPath), [string]$FileId, [string]$Purpose = 'runtime') {
    if ([string]::IsNullOrEmpty($Gateway) -or [string]::IsNullOrEmpty($FileId)) {
        return (New-AfxGwError 'GW-CLI-05' '用法：Request-AfxDownload -Gateway <网关地址> -FileId <文件ID> [-KeyPath <钥匙文件>] [-Purpose runtime]')
    }
    try { $body = Get-AfxIssueBody $FileId $Purpose } catch { return (New-AfxGwError 'GW-CLI-05' $_.Exception.Message) }
    $maxWait = 30
    if ($env:AFX_GW_MAX_RETRY_AFTER -match '^\d+$') { $maxWait = [int]$env:AFX_GW_MAX_RETRY_AFTER }
    $didReg = $false; $didWait = $false; $tries = 0
    while ($true) {
        $tries++
        $r = Invoke-AfxGwPost $Gateway $KeyPath '/v1/issue' $body
        $code = Get-AfxProp $r 'code'
        if ($code -eq 'GW-AUTH-02' -and -not $didReg) {
            $didReg = $true
            if ((Register-AfxGateway $Gateway $KeyPath).ok) { continue }
        } elseif ($code -eq 'GW-LIM-02' -and -not $didWait) {
            $didWait = $true
            $ra = 1
            $v = Get-AfxProp $r 'retry_after'
            if ($null -ne $v) { $ra = [int][Math]::Ceiling([double]$v) }
            if ($ra -le $maxWait) { Start-Sleep -Seconds $ra; continue }
        }
        break
    }
    Write-AfxGwLog ("issue gw=$Gateway file=$FileId http=$(Get-AfxProp $r 'http_status') result=" + $(if ($r.ok) { 'ok' } else { $code }) + " tries=$tries")
    return $r
}

# GET /v1/health（不签名）
function Test-AfxGateway([string]$Gateway) {
    if ([string]::IsNullOrEmpty($Gateway)) { return (New-AfxGwError 'GW-CLI-05' '用法：Test-AfxGateway -Gateway <网关地址>') }
    return (Invoke-AfxGwHttp 'GET' ($Gateway.TrimEnd('/') + '/v1/health') $null $null)
}

# ---------- 单独运行时的命令行 ----------

function Show-AfxGwUsage {
    [Console]::Out.WriteLine(@'
afx-gw-client.ps1：AcornFox 下载网关的 PowerShell 客户端（Windows PowerShell 5.1）

用法：powershell -NoProfile -ExecutionPolicy Bypass -File afx-gw-client.ps1 <命令> [参数] [--json] [--apply]
钥匙文件写 default 表示默认位置 %LOCALAPPDATA%\AcornFox\identity\machine-key.blob。

只读命令（没有副作用）：
  pubkey <钥匙文件>                       输出公钥（65 字节未压缩点的 base64url）
  install-id <公钥>                       由公钥算安装编号（afx-...）
  health <网关地址>                       查网关是否在签发

写操作（默认试跑，只显示会做什么；加 --apply 才执行）：
  keygen <钥匙文件>                       生成 P-256 钥匙（只给当前用户）；文件已存在就不覆盖
  export-pem <钥匙文件> <PEM 文件>        导出 SEC1 PEM，交给 WSL 里的 Linux 版 acornfox
  register <网关地址> <钥匙文件>          向网关登记这台机器（W1）
  issue <网关地址> <钥匙文件> <文件ID> [runtime|app|upgrade]
                                          要一个临时下载地址（W2，会占用当天次数）

选项：--json 输出一行 JSON；--apply 真的执行写操作；--help 显示本说明。

在安装脚本里：先点引入本文件，再调 New-AfxMachineKey、Request-AfxDownload、Export-AfxMachineKeyPem（真执行，无试跑）。
退出码：0 成功；1 网关拒绝；2 参数不对；3 连不上网关；4 本机钥匙出错；5 网关返回格式不对。
错误编号：网关的 GW-xxx 见网关接口契约；本机的是 GW-CLI-01～05（见 README.md）。
'@)
}

function Get-AfxGwExitCode($r) {
    if ($r.ok) { return 0 }
    switch (Get-AfxProp $r 'code') {
        'GW-CLI-05' { return 2 }
        'GW-CLI-03' { return 3 }
        'GW-CLI-01' { return 4 }
        'GW-CLI-02' { return 4 }
        'GW-CLI-04' { return 5 }
        default { return 1 }
    }
}

function Invoke-AfxGwCli([string[]]$Argv) {
    $json = $false; $apply = $false; $pos = @()
    foreach ($a in $Argv) {
        switch ($a) {
            '--json' { $json = $true }
            '--apply' { $apply = $true }
            { $_ -in @('-h', '--help', 'help', '-?', '/?') } { Show-AfxGwUsage; return 0 }
            default { $pos += $a }
        }
    }
    if ($pos.Count -eq 0) { Show-AfxGwUsage; return 2 }
    if (-not $env:AFX_GW_LOG) { $env:AFX_GW_LOG = Join-Path $env:LOCALAPPDATA 'AcornFox\logs\gw-client.log' }
    $cmd = $pos[0]
    $rest = @($pos | Select-Object -Skip 1)
    function KeyArg($i) { if ($rest.Count -le $i -or $rest[$i] -eq 'default') { return (Get-AfxDefaultKeyPath) } return $rest[$i] }
    try {
        switch ($cmd) {
            'pubkey' { $r = [pscustomobject]@{ ok = $true; pubkey = (Get-AfxPublicKey (KeyArg 0)) } }
            'install-id' { if ($rest.Count -lt 1) { throw 'GW-CLI-05 用法：install-id <公钥>' }; $r = [pscustomobject]@{ ok = $true; install_id = (Get-AfxInstallId $rest[0]) } }
            'health' { if ($rest.Count -lt 1) { throw 'GW-CLI-05 用法：health <网关地址>' }; $r = Test-AfxGateway $rest[0] }
            'keygen' {
                $p = KeyArg 0
                if ($apply) { $r = New-AfxMachineKey $p }
                elseif (Test-Path -LiteralPath $p) { $r = [pscustomobject]@{ ok = $true; dry_run = $true; exists = $true; path = $p; message = '已存在，--apply 也不会覆盖' } }
                else { $r = [pscustomobject]@{ ok = $true; dry_run = $true; exists = $false; path = $p; message = '会生成 P-256 钥匙，加 --apply 执行' } }
            }
            'export-pem' {
                if ($rest.Count -lt 2) { throw 'GW-CLI-05 用法：export-pem <钥匙文件> <PEM 文件>' }
                $kp = KeyArg 0; $null = Read-AfxKeyBlob $kp
                if ($apply) { $r = [pscustomobject]@{ ok = $true; path = (Export-AfxMachineKeyPem $kp $rest[1]); format = 'sec1' } }
                else { $r = [pscustomobject]@{ ok = $true; dry_run = $true; path = $rest[1]; message = '会导出 SEC1 PEM（含私钥），加 --apply 执行' } }
            }
            { $_ -in @('register', 'issue') } {
                if ($rest.Count -lt 1) { throw 'GW-CLI-05 缺网关地址' }
                $kp = KeyArg 1
                if ($cmd -eq 'issue' -and $rest.Count -lt 3) { throw 'GW-CLI-05 用法：issue <网关地址> <钥匙文件> <文件ID> [用途]' }
                $purpose = if ($rest.Count -ge 4) { $rest[3] } else { 'runtime' }
                if (-not $apply) {
                    $body = if ($cmd -eq 'register') { Get-AfxRegisterBody $kp } else { Get-AfxIssueBody $rest[2] $purpose }
                    $r = [pscustomobject]@{ ok = $true; dry_run = $true; url = ($rest[0].TrimEnd('/') + "/v1/$cmd"); install_id = (Get-AfxInstallId (Get-AfxPublicKey $kp)); body = $body }
                } elseif ($cmd -eq 'register') { $r = Register-AfxGateway $rest[0] $kp }
                else { $r = Request-AfxDownload -Gateway $rest[0] -KeyPath $kp -FileId $rest[2] -Purpose $purpose }
            }
            default { throw "GW-CLI-05 不认识的命令：$cmd（看 --help）" }
        }
    } catch {
        $m = $_.Exception.Message
        $c = if ($m -match '^(GW-CLI-0\d) (.*)$') { $Matches[1] } else { 'GW-CLI-02' }
        if ($m -match '^GW-CLI-0\d (.*)$') { $m = $Matches[1] }
        $r = New-AfxGwError $c $m
    }
    # 直接写控制台，不进函数的返回值（返回值只留退出码）
    if ($json) { [Console]::Out.WriteLine(($r | ConvertTo-Json -Compress -Depth 5)) }
    else { [Console]::Out.WriteLine((($r | Format-List | Out-String).Trim())) }
    return (Get-AfxGwExitCode $r)
}

# 被点引入（. .\afx-gw-client.ps1）时只定义函数；用 -File 直接运行时才走命令行。
# 把本文件内容嵌进别的脚本（Invoke-Expression 等）时，先设 $env:AFX_GW_NO_CLI = '1' 更稳妥。
if ($MyInvocation.InvocationName -ne '.' -and $MyInvocation.MyCommand.CommandType -eq 'ExternalScript' -and $env:AFX_GW_NO_CLI -ne '1') {
    try { [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false) } catch { }
    $rc = Invoke-AfxGwCli ([string[]]$args)
    exit $rc
}
