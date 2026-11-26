#!/usr/bin/env bash
# 安装脚本认“来源标记”（--from / -From）的不动系统测试。
#
# 做什么：核对 install.sh（Linux 安装）、install-mac.sh（Mac）、install-windows.ps1 和 get.ps1（Windows，没有 PowerShell 时只做静态检查）：
#   1. install.sh：读进函数库（AFX_INSTALL_LIB=1），--from 名字 / --from=名字 记进 INSTALL_FROM；不合规（大写、下划线、超长、空、没写值、结尾带换行）
#      的当没有、提醒一句、不吞掉后面的选项；record_install_from 把标记写进文件（0600）、已有就不覆盖、没带标记不写、写不进去只提醒；
#      show_plan 里写明会报；main 里在 create_directories 之后调用；
#   2. install-mac.sh：真运行一遍（用假的 uname/id 和假的发行版站点、假的 Mac 程序），看交给 acornfox mac 的参数里有没有 --from：
#      安装带、升级和卸载不带、不合规不带；
#   3. Windows：get.ps1、install-windows.ps1 里和 --from 有关的几处写法都在（参数、校验写法、只在安装时往下传、重启后接着装也带、
#      老脚本检测用的 AFX-FEATURE 行），并且各处的标记写法完全一样；
#   4. 各处的“标记写法”（a-z、0-9、-，1～32）和 Go 里的 installfrom、官网 site.js 是同一条。
# 什么时候用：改了上面这些脚本里和 --from 有关的部分之后。scripts/get.test.sh 最后会调用它（测试闸门的 oss.scripts）。
# 前提：bash、grep、sed；第 2 项还要 shasum 或 sha256sum、curl（支持 file://），没有就跳过那一项并写明。不需要 root，不联网，不装东西。
# 怎么运行：bash scripts/install/from.test.sh
# 怎么验证：最后一行“通过 N/N”，退出码 0；不对的项打印“不对”和实际输出。
# 副作用：只在 mktemp 建的临时目录里写文件，退出时删掉。
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd); ROOT=$(cd "$HERE/../.." && pwd)
INSTALL="$HERE/install.sh"; MAC="$HERE/install-mac.sh"; WIN="$HERE/install-windows.ps1"; GETPS="$ROOT/get.ps1"; GETSH="$ROOT/get.sh"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
pass=0; total=0
ok() { total=$((total + 1)); pass=$((pass + 1)); echo "对    $1"; }
bad() { total=$((total + 1)); echo "不对  $1"; echo "$2" | sed 's/^/        /' | head -12; }
check() {  # 名称 要有的（grep -F） 实际
    if grep -qF -- "$2" <<<"$3"; then ok "$1"; else bad "$1（没有：$2）" "$3"; fi
}
checkno() {  # 名称 不该有的 实际
    if grep -qF -- "$2" <<<"$3"; then bad "$1（不该有：$2）" "$3"; else ok "$1"; fi
}

# ---------- 1. install.sh ----------
# parse 参数…：读入函数后运行 parse_args，打印 “FROM=… CHINA=…”，再把提醒（标准输出）一起带出来
parse() {
    AFX_INSTALL_LIB=1 bash -c 'source "$1"; shift; parse_args "$@"; echo "FROM=[${INSTALL_FROM}] CHINA=${FORCE_CHINA} NOLAN=${LAN_CONSOLE}"' _ "$INSTALL" "$@" 2>&1
}
check "--from 名字：记下来" "FROM=[bili-1201]" "$(parse --from bili-1201)"
check "--from=名字：记下来" "FROM=[bili-1201]" "$(parse --from=bili-1201)"
check "和别的选项一起写：都认" "FROM=[bili-1201] CHINA=true NOLAN=false" "$(parse --china --from bili-1201 --no-lan-console)"
check "名字是 32 个字符：可以" "FROM=[$(printf 'a%.0s' $(seq 1 32))]" "$(parse --from "$(printf 'a%.0s' $(seq 1 32))")"
check "名字带数字和横线：可以" "FROM=[0-a-9]" "$(parse --from 0-a-9)"
for badtag in "Bili-1201" "bili_1201" "bili 1201" "$(printf 'a%.0s' $(seq 1 33))" "" "a/b" "a;b" '$(x)' "来源" $'ab\ncd' $'ab\n'; do
    out=$(parse --from "$badtag")
    label=$(printf '%s' "$badtag" | tr '\n' '~' | cut -c1-20)
    check "不合规的名字（$label）：当没有" "FROM=[]" "$out"
    check "不合规的名字（$label）：提醒一句、说照常安装" "照常安装" "$out"
done
out=$(parse --from Bad_Tag --china)
check "不合规的名字不影响后面的选项" "FROM=[] CHINA=true" "$out"
out=$(parse --from)
check "--from 后面什么都没有：当没写，不报错" "FROM=[]" "$out"
out=$(parse --from --china)
check "--from 后面紧跟另一个选项：不吞掉它" "FROM=[] CHINA=true" "$out"
check "--from 后面紧跟另一个选项：提醒一句" "不合规" "$out"
out=$(parse --from=)
check "--from= 空值：当没写" "FROM=[]" "$out"
out=$(parse)
check "不写 --from：INSTALL_FROM 是空的" "FROM=[]" "$out"
checkno "不写 --from：没有任何提醒" "来源标记" "$out"

# record_install_from：文件路径用 AFX_INSTALL_FROM_FILE 换成临时的；chown 换成空函数（测试不是 root，也没有 acornfox 账号）
rec() {  # 标记 文件 → 打印输出和退出码
    AFX_INSTALL_LIB=1 AFX_INSTALL_FROM_FILE="$2" bash -c 'source "$1"; chown() { return 0; }; INSTALL_FROM="$2"; record_install_from; echo "rc=$?"' _ "$INSTALL" "$1" 2>&1
}
F="$T/install-from"
out=$(rec bili-1201 "$F"); check "record_install_from：写文件" "rc=0" "$out"
[ "$(cat "$F" 2>/dev/null)" = "bili-1201" ] && ok "文件里就是这一行标记" || bad "文件内容不对" "$(cat "$F" 2>&1)"
mode=$(stat -c '%a' "$F" 2>/dev/null || stat -f '%Lp' "$F" 2>/dev/null)
[ "$mode" = 600 ] && ok "文件权限是 0600" || bad "文件权限是 $mode，要 600" ""
out=$(rec other-tag "$F"); check "已有就不覆盖：说明" "不覆盖" "$out"
[ "$(cat "$F")" = "bili-1201" ] && ok "已有就不覆盖：重装不改最早的来源" || bad "标记被改了" "$(cat "$F")"
[ ! -e "$F.tmp" ] && ok "不留临时文件" || bad "留了临时文件" ""
F2="$T/none-from"
out=$(rec "" "$F2"); check "没带标记：什么都不做" "rc=0" "$out"
[ ! -e "$F2" ] && ok "没带标记：不写文件（也就不会去登记网关）" || bad "没带标记却写了文件" ""
out=$(rec bili-1201 "$T/no-such-dir/install-from"); check "写不进去：只提醒，函数照常返回 0" "rc=0" "$out"
check "写不进去：提醒里说不影响安装" "不影响安装" "$out"

out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; INSTALL_FROM=bili-1201; show_plan' _ "$INSTALL" 2>&1)
check "show_plan：写明会向下载网关报一次、只报标记" "来源标记 bili-1201" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; INSTALL_FROM=""; show_plan' _ "$INSTALL" 2>&1)
checkno "show_plan：没带标记就没有这一条" "来源标记" "$out"
out=$(bash "$INSTALL" --help 2>&1); check "--help 里有 --from 的说明" "--from 名字" "$out"
if awk '/^main\(\) \{/,/^}/' "$INSTALL" | grep -n 'create_directories\|record_install_from' | tr '\n' ' ' | grep -Eq 'create_directories.*record_install_from'; then ok "main：create_directories 之后才 record_install_from"; else bad "main 里 record_install_from 没排在 create_directories 之后" "$(awk '/^main\(\) \{/,/^}/' "$INSTALL" | grep -n 'create_directories\|record_install_from')"; fi

# ---------- 2. install-mac.sh（真运行：假 uname、假 id、假发行版站点、假 Mac 程序） ----------
SHA=""
if command -v shasum >/dev/null 2>&1; then SHA="shasum -a 256"; elif command -v sha256sum >/dev/null 2>&1; then SHA="sha256sum"; fi
if [ -n "$SHA" ] && command -v curl >/dev/null 2>&1; then
    SHIM="$T/shim"; mkdir -p "$SHIM"
    printf '#!/bin/sh\ncase "$1" in -s) echo Darwin;; -m) echo arm64;; *) exec /usr/bin/uname "$@";; esac\n' > "$SHIM/uname"
    printf '#!/bin/sh\ncase "$1" in -u) echo 501;; *) exec /usr/bin/id "$@";; esac\n' > "$SHIM/id"
    # install-mac.sh 用 shasum；Linux 上没有 shasum 时给一个转到 sha256sum 的
    if ! command -v shasum >/dev/null 2>&1; then printf '#!/bin/sh\nshift 2\nexec sha256sum "$@"\n' > "$SHIM/shasum"; fi
    chmod +x "$SHIM"/*
    SITE="$T/site"; D="$SITE/releases/download/v9.9.1"; mkdir -p "$D"
    printf '#!/bin/sh\necho "MAC-PROGRAM args=[$*]"\n' > "$D/acornfox_darwin_arm64"; chmod +x "$D/acornfox_darwin_arm64"
    h=$($SHA "$D/acornfox_darwin_arm64" | awk '{print $1}'); echo "$h  acornfox_darwin_arm64" > "$D/SHA256SUMS"
    mac() { PATH="$SHIM:$PATH" ACORNFOX_RELEASE_BASE="file://$SITE" ACORNFOX_MAC_HOME="$T/machome" bash "$MAC" "$@" 2>&1; }
    out=$(mac install --version v9.9.1 --from bili-1201)
    check "install-mac.sh 安装：--from 交给 acornfox mac" "MAC-PROGRAM args=[mac install --version v9.9.1 --from bili-1201]" "$out"
    out=$(mac --version v9.9.1 --china --from=bili-1201 --idle 60)
    check "install-mac.sh：--from=名字，和别的参数一起" "--china --idle 60 --from bili-1201]" "$out"
    out=$(mac install --version v9.9.1 --from Bad_Tag)
    checkno "install-mac.sh：不合规的不往下传" "--from" "$(grep MAC-PROGRAM <<<"$out")"
    check "install-mac.sh：不合规的提醒一句" "不合规" "$out"
    check "install-mac.sh：不合规时照常安装" "MAC-PROGRAM args=[mac install --version v9.9.1" "$out"
    out=$(mac upgrade --version v9.9.1 --from bili-1201)
    checkno "install-mac.sh 升级：不带 --from" "--from" "$(grep MAC-PROGRAM <<<"$out")"
    check "install-mac.sh 升级：说一句只在安装时用" "只在安装时用" "$out"
    out=$(mac install --version v9.9.1 --from --china)
    check "install-mac.sh：--from 后面紧跟选项不吞掉它" "--china" "$(grep MAC-PROGRAM <<<"$out")"
    checkno "install-mac.sh：--from 没写值就当没有" "--from bili" "$out"
else
    echo "跳过  install-mac.sh 的真运行（没有 shasum/sha256sum 或 curl）"
fi

# ---------- 3. Windows：静态检查（这台机器上没有 PowerShell 也能跑；真运行在 Windows 测试机上） ----------
# 脚本是带 BOM 的 UTF-8，去掉 BOM 再读
wsrc() { sed '1s/^\xEF\xBB\xBF//' "$1"; }
GP=$(wsrc "$GETPS"); WP=$(wsrc "$WIN")
check "get.ps1 有 -From 参数" "[string]\$From = ''" "$GP"
check "install-windows.ps1 有 -From 参数" "[string]\$From = ''" "$WP"
for pair in "get.ps1:$GP" "install-windows.ps1:$WP"; do
    n=${pair%%:*}; body=${pair#*:}
    check "$n：-From 的校验用区分大小写、不放过结尾换行的写法" "-cnotmatch '^[a-z0-9-]{1,32}\\z'" "$body"
done
check "get.ps1：只在 install、resume 时往下传 -From" "@('install', 'resume') -notcontains \$Action" "$GP"
check "get.ps1：往下传前先看 install-windows.ps1 认不认（AFX-FEATURE 行）" "AFX-FEATURE: install-from" "$GP"
check "get.ps1：真的把 -From 交给 install-windows.ps1" "\$a += @('-From', \$From)" "$GP"
check "install-windows.ps1：带 AFX-FEATURE 行（get.ps1 靠它认新旧）" "# AFX-FEATURE: install-from" "$WP"
check "install-windows.ps1：只在安装时把 --from 接给 WSL 里的 get.sh" "if (\$Verb -eq 'install' -and \$From) { \$extra = \"\$extra --from \$From\" }" "$WP"
check "install-windows.ps1：重启后接着装也带 -From" "if (\$From) { \$a += @('-From', \$From) }" "$WP"
for f in "$INSTALL" "$MAC"; do
    grep -q '^# AFX-FEATURE: install-from' "$f" && ok "$(basename "$f")：带 AFX-FEATURE 行（get.sh 靠它认新旧）" || bad "$(basename "$f") 缺 AFX-FEATURE 行" ""
done
# 带 -From 的 get.ps1 / install-windows.ps1 仍是带 BOM 的 UTF-8
for f in "$GETPS" "$WIN"; do
    [ "$(head -c 3 "$f" | od -An -tx1 | tr -d ' \n')" = efbbbf ] && ok "$(basename "$f") 仍带 BOM" || bad "$(basename "$f") 丢了 BOM" ""
done

# ---------- 4. 标记写法各处一样 ----------
RE='[a-z0-9-]{1,32}'
check "install.sh 的写法" "INSTALL_FROM_RE='^$RE\$'" "$(cat "$INSTALL")"
check "install-mac.sh 的写法" "FROM_RE='^$RE\$'" "$(cat "$MAC")"
check "get.sh 的写法" "FROM_RE='^$RE\$'" "$(cat "$GETSH")"
check "Go（internal/installfrom）的写法" "regexp.MustCompile(\`^$RE\$\`)" "$(cat "$ROOT/acornfox/internal/installfrom/installfrom.go")"
check "官网 site.js 的写法" "var FROM_RE = /^$RE\$/" "$(cat "$ROOT/website/assets/site.js")"
check "官网 commands.js 的写法" "/^$RE\$/" "$(cat "$ROOT/website/assets/commands.js")"

echo "通过 $pass/$total"
[ "$pass" = "$total" ]
