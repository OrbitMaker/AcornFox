#!/usr/bin/env bash
# install.sh、upgrade.sh 下载发行版文件时“先在 Gitee 重试一次再换 GitHub”，以及 upgrade.sh 的 --china / --global 的不动系统测试。
#
# 做什么：把脚本当函数库读进来（AFX_INSTALL_LIB=1，只定义函数、不执行），用假的 curl_get、github_download 模拟
#   “第一次下到一半卡住（curl 退出码 28）、第二次好了”这类序列，核对：好了就不碰 GitHub、两次都不行才换 GitHub、
#   换源前每次失败都写了一行日志；upgrade.sh 认 --china、--global 并且只影响这一次的下载源、不写 install.env。
# 为什么：P-847——国内机器上 Gitee 能到 2 MB/s，下载偶尔中途卡住被限速判断停下，以前一次失败就退回 GitHub（几十 KB/s）。
# 什么时候用：改了 release_download、gitee_download、upgrade.sh 的参数或下载源判断之后。scripts/get.test.sh 最后会调用它。
# 前提：bash。不需要 root，不联网。
# 怎么运行：bash scripts/install/download.test.sh
# 怎么验证：最后一行“通过 N/N”，退出码 0；不对的项打印“不对”和实际输出。
# 副作用：只在 mktemp 建的临时目录里写文件，退出时删掉。不改系统。
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
pass=0; total=0
ok() { total=$((total + 1)); pass=$((pass + 1)); echo "对    $1"; }
bad() { total=$((total + 1)); echo "不对  $1"; echo "$2" | sed 's/^/        /' | head -20; }
check() { if grep -qF -- "$2" <<<"$3"; then ok "$1"; else bad "$1（没有：$2）" "$3"; fi; }
checkno() { if grep -qF -- "$2" <<<"$3"; then bad "$1（不该有：$2）" "$3"; else ok "$1"; fi; }

# run 脚本 序列：序列是每次 Gitee 下载的 curl 退出码，空格分隔（例如 "28 0"）。输出：每次调用一行，最后一行是 release_download 的返回值
run() {
    SEQ="$2" AFX_INSTALL_LIB=1 bash -c '
        source "$1" >/dev/null 2>&1
        set +e
        IN_CHINA=true; ACORNFOX_GITEE_REPO=x/y; ACORNFOX_RELEASE_BASE=""
        ACORNFOX_REPO=o/r
        n=0; read -r -a SEQ <<<"$SEQ"
        log_info() { :; }
        log_warn() { echo "警告: $*"; }
        curl_get() { echo "gitee尝试 $2"; local rc=${SEQ[$n]:-1}; n=$((n+1)); [ "$rc" = 0 ] && echo data > "$1"; return "$rc"; }
        github_download() { echo "github $1"; return 0; }
        release_download v1.2.3 acornfox_linux_amd64 "'"$T"'/out"
        echo "返回 $?"' _ "$HERE/$1" 2>&1
}

for sc in install.sh upgrade.sh; do
    out=$(run "$sc" "0")
    check "$sc：Gitee 第一次就好：只下一次" "返回 0" "$out"
    checkno "$sc：Gitee 第一次就好：不碰 GitHub" "github " "$out"
    n=$(grep -c '^gitee尝试' <<<"$out"); [ "$n" = 1 ] && ok "$sc：Gitee 第一次就好：只尝试 1 次" || bad "$sc：尝试次数 $n，要 1" "$out"

    out=$(run "$sc" "28 0")
    check "$sc：第一次卡住、第二次好：返回 0" "返回 0" "$out"
    checkno "$sc：第一次卡住、第二次好：不碰 GitHub" "github " "$out"
    n=$(grep -c '^gitee尝试' <<<"$out"); [ "$n" = 2 ] && ok "$sc：第一次卡住、第二次好：在 Gitee 上试了 2 次" || bad "$sc：尝试次数 $n，要 2" "$out"
    check "$sc：失败那次写了日志（含 curl 退出码）" "curl 退出码 28" "$out"
    check "$sc：日志说明在 Gitee 上再试一次" "再试一次" "$out"

    out=$(run "$sc" "28 28")
    check "$sc：两次都卡住：换 GitHub" "github https://github.com/o/r/releases/download/v1.2.3/acornfox_linux_amd64" "$out"
    n=$(grep -c '^gitee尝试' <<<"$out"); [ "$n" = 2 ] && ok "$sc：两次都卡住：Gitee 只试 2 次" || bad "$sc：尝试次数 $n，要 2" "$out"
    check "$sc：两次都卡住：说明换了源" "改从 GitHub 下载" "$out"
    [ ! -e "$T/out" ] && ok "$sc：失败后不留半个文件" || bad "$sc：失败后留下了文件" "$(ls -l "$T/out")"
    rm -f "$T/out"
done

# ---------- upgrade.sh 的 --china / --global（P-837） ----------
U="$HERE/upgrade.sh"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; set +e; parse_args --china -y; echo "china=$FORCE_CHINA global=$FORCE_GLOBAL yes=$ASSUME_YES"' _ "$U" 2>&1)
check "upgrade.sh 认 --china" "china=true global=false yes=true" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; set +e; parse_args --global; echo "china=$FORCE_CHINA global=$FORCE_GLOBAL"' _ "$U" 2>&1)
check "upgrade.sh 认 --global" "china=false global=true" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --nope; echo 到不了这里' _ "$U" 2>&1)
check "upgrade.sh 不认的参数仍然报错，提示里列出 --china" "--china、--global" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; set +e; parse_args --china; NETWORK_DECIDED=1; IN_CHINA=false; setup_github_proxies; echo "china=$IN_CHINA proxies=${#GITHUB_PROXIES[@]}"' _ "$U" 2>&1)
check "--china：记下的区域是海外也改成这一次先用国内源（加两个加速前缀）" "china=true proxies=3" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; set +e; parse_args --global; NETWORK_DECIDED=1; IN_CHINA=true; setup_github_proxies; echo "china=$IN_CHINA proxies=${#GITHUB_PROXIES[@]}"' _ "$U" 2>&1)
check "--global：记下的区域是国内也改成这一次先用海外源（不加速前缀）" "china=false proxies=1" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; set +e; parse_args; NETWORK_DECIDED=1; IN_CHINA=true; setup_github_proxies; echo "china=$IN_CHINA"' _ "$U" 2>&1)
check "都不加：沿用记下的区域" "china=true" "$out"
# 只影响这一次：升级脚本里不会写 install.env 的网络区域
n=$(grep -c 'ACORNFOX_IN_CHINA=' "$U" || true)
out=$(grep -n 'ACORNFOX_IN_CHINA=' "$U" | grep -E '(>|echo|printf|sed -i)' || true)
[ -z "$out" ] && ok "upgrade.sh 没有写 ACORNFOX_IN_CHINA（--china / --global 不改记下的区域）" || bad "upgrade.sh 里有写 ACORNFOX_IN_CHINA 的地方" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; show_help' _ "$U" 2>&1)
check "upgrade.sh 帮助里有 --china、--global" "--global           这一次先从 GitHub" "$out"

echo "通过 $pass/$total"
[ "$pass" = "$total" ]
