#!/usr/bin/env bash
# 把 Windows 安装脚本拼成发行版里的那一份：
#   - 把 install-windows.ps1 里的 #@@AFX_GW_CLIENT@@ 一行换成 windows/afx-gw-client.ps1 的内容（下载网关的 PowerShell 客户端，
#     唯一来源在 oss 的 scripts/install/windows/；用户拿到的是一个自包含的文件）；
#   - 可选：把 $ScriptVersion = 'dev' 写成指定版本（发行版脚本只装自己这个版本）；
#   - 输出一定带 BOM 的 UTF-8（英文 Windows 上的 PowerShell 5.1 读无 BOM 的中文会解析错）。
# 用法：bash assemble.sh <输出文件> [版本，例如 v0.7.0]
# 验证：输出文件里没有 #@@AFX_GW_CLIENT@@ 这一行、有 function Request-AfxDownload；带版本时有 $ScriptVersion = 'v0.7.0'。
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
OUT="${1:?用法: $0 <输出文件> [版本]}"
VER="${2:-}"
SRC="$HERE/../install-windows.ps1"
CLIENT="$HERE/afx-gw-client.ps1"
python3 - "$SRC" "$CLIENT" "$OUT" "$VER" <<'PY'
import sys
src, client, out, ver = sys.argv[1:5]
s = open(src, encoding="utf-8-sig").read().replace("\r\n", "\n")
c = open(client, encoding="utf-8-sig").read().replace("\r\n", "\n")
marker = "#@@AFX_GW_CLIENT@@"
if s.count(marker) != 1:
    sys.exit("install-windows.ps1 里要恰好有一行 %s，现在有 %d 行" % (marker, s.count(marker)))
s = s.replace(marker, "#region 下载网关客户端（来自 scripts/install/windows/afx-gw-client.ps1）\n" + c + "\n#endregion")
if ver:
    old = "$ScriptVersion = 'dev'"
    if s.count(old) != 1:
        sys.exit("install-windows.ps1 里要恰好有一行 %s" % old)
    s = s.replace(old, "$ScriptVersion = '%s'" % ver)
open(out, "wb").write(b"\xef\xbb\xbf" + s.replace("\n", "\r\n").encode("utf-8"))
PY
grep -q '#@@AFX_GW_CLIENT@@' "$OUT" && { echo "没拼进去" >&2; exit 1; }
grep -q 'function Request-AfxDownload' "$OUT" || { echo "没有网关客户端" >&2; exit 1; }
if [ -n "$VER" ]; then grep -q "^\$ScriptVersion = '$VER'" <(tr -d '\r' < "$OUT") || { echo "版本戳没写进去" >&2; exit 1; }; fi
echo "已生成 $OUT"
