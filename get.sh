#!/usr/bin/env bash
# AcornFox 引导脚本：安装、升级、装命令行、卸载的唯一入口。
#
# 它自己不装任何东西，只做五件事：选下载源 → 定版本 → 下载“那个版本自带的”脚本和校验文件 → 校验 → 把参数交给它。
# 所以安装脚本和程序永远来自同一个版本，不会出现新脚本配旧程序。
#
# 用法（服务器上）：
#   国内： curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash
#   海外： curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash
#   升级： … | sudo bash -s -- upgrade
#   卸载： … | sudo bash -s -- uninstall            （保留数据；连数据一起删加 --purge）
#   Mac（苹果芯片，macOS 26 起；在“终端”里用普通用户运行，不要加 sudo）： curl -fsSL <get.sh 地址> | bash
#     在 Mac 上 install、upgrade、uninstall 走 Mac 的路：装进苹果 container 开的一台 Debian 虚拟机，里面是和服务器同一个程序
#     （交给那个版本自带的 install-mac.sh，再由它运行 acornfox mac …；--plan 试跑、--json、--idle、--memory 等参数原样交给它）。
#     只想在 Mac 上装命令行管理别处的服务器：见下一行 cli。
#   只装命令行（Mac 或 Linux 电脑上，不用 sudo）： … | bash -s -- cli
#   指定版本（含预发布版）： … | sudo bash -s -- --version v0.6.0
#   其余参数原样交给那个版本的脚本，例如： … | sudo bash -s -- --china --no-lan-console
#   来源标记（官网链接带来的，装好后向下载网关报一次、只报它）： … | sudo bash -s -- --from bili-1201      （Mac：… | bash -s -- --from bili-1201）
#     只认小写字母、数字、-，最长 32；不合规的当没有（提醒一句、照常安装）；只在安装时有用，升级、卸载、装命令行不带；
#     要装的那个版本的脚本还不认它（老版本）时不往下传，照常安装。不写就什么都不报。
#   已经下载到本地：bash get.sh [install|upgrade|cli|uninstall] [--version vX.Y.Z] [参数…]；bash get.sh --help
#
# 选源：加 --china 先用 Gitee，加 --global 先用 GitHub；都不加时先看这台机器以前安装时记下的判断（/etc/acornfox/install.env），
#   没有就当场测速：同时从 Gitee 和 GitHub 各取同一个发行版文件的开头（最多 1 MiB、5 秒），比谁快，快的先用，
#   并把这个判断作为 --china / --global 一路传给 install.sh、upgrade.sh、install-cli.sh（Mac 上传给 install-mac.sh）。
#   屏幕上有一行“测速：Gitee x MB/s、GitHub y MB/s，先用 …”。两边都没测通时先用 Gitee、不往下传（交给下一个脚本自己认）。
#   一个源取不到会自动换另一个。国内常常“GitHub 连得上但只有几十 KB/s”，所以不再用“连不连得上”判断。
# 校验：下载的脚本要和同一版本 SHA256SUMS 里的值一致，不一致就停下。v0.6.0 之前的版本没有给脚本提供校验值，会提示一句照常继续。
# 退出码：交给那个版本的脚本之后就是它的退出码；引导自己出错时是 1（用法不对是 2），并打印故障编号：
#   INSTALL-GET-01 查不到最新正式版号   INSTALL-GET-02 下载失败（两个源都试过）   INSTALL-GET-03 校验不过   INSTALL-GET-04 用法不对
# 副作用：只在临时目录里放下载的脚本和校验文件，退出时删掉。真正的改动都由那个版本的脚本做。
# 环境变量：ACORNFOX_REPO、ACORNFOX_GITEE_REPO（换仓库）；
#   ACORNFOX_RELEASE_BASE（测试或自建镜像用：设了就只从这里取，目录结构是
#   <地址>/releases/download/<版本>/<文件>，<地址>/latest 里一行写最新正式版号）；
#   ACORNFOX_PROBE_SECONDS（测速最多等几秒，默认 5）；ACORNFOX_GITEE_HOST、ACORNFOX_GITHUB_HOST、ACORNFOX_INSTALL_ENV（只给测试：换成假服务器、假的安装登记文件）。
# 出错先看哪里：屏幕上的故障编号和它上面那几行；能连上哪个源：curl -I https://gitee.com、curl -I https://github.com。
# 兼容 macOS 自带的 bash 3.2（装命令行时会在 Mac 上运行）。

set -euo pipefail

GITHUB_REPO="${ACORNFOX_REPO:-OrbitMaker/AcornFox}"
GITEE_REPO="${ACORNFOX_GITEE_REPO-VIP13390/AcornFox}"
BASE="${ACORNFOX_RELEASE_BASE:-}"
# 从这个版本起，发行版的 SHA256SUMS 覆盖全部文件（含脚本）；更早的版本只覆盖程序文件
CHECKSUM_SINCE="0.6.0"

JSON_MODE=0
# 加了 --json（Mac 上的 install-mac.sh 认它）时，引导自己的进度话改走标准错误，标准输出只留最后那一行 JSON
say() { if [ "$JSON_MODE" = 1 ]; then echo "[引导] $*" >&2; else echo "[引导] $*"; fi; }
fail() {  # fail 退出码 故障编号 说明 [处理办法…]
    local rc="$1" code="$2" msg="$3" line
    shift 3
    echo "[引导] 出错：$msg" >&2
    for line in "$@"; do echo "    $line" >&2; done
    echo "故障编号: ${code}" >&2
    exit "$rc"
}
usage() { awk 'NR>1 && !/^#/ {exit} NR>1' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; }

# ---------- 参数 ----------
VERB=""; VERSION=""; PREFER=""; VERSION_GIVEN=0; FROM_RAW=""; FROM_GIVEN=0
PASS=()
while [ $# -gt 0 ]; do
    case "$1" in
        --from)
            # 后面没有值（或紧跟着另一个选项）：当没写，不吞掉下一个选项
            FROM_GIVEN=1
            if [ $# -ge 2 ] && [ "${2#-}" = "$2" ]; then FROM_RAW="$2"; shift; else FROM_RAW=""; fi;;
        --from=*) FROM_GIVEN=1; FROM_RAW="${1#--from=}";;
        install|upgrade|cli|uninstall)
            if [ -z "$VERB" ]; then VERB="$1"; else PASS+=("$1"); fi;;
        --version) [ $# -ge 2 ] || fail 2 INSTALL-GET-04 "--version 后面要跟版本号，例如 --version v0.6.0"; VERSION="$2"; VERSION_GIVEN=1; shift;;
        --version=*) VERSION="${1#*=}"; VERSION_GIVEN=1;;
        --json) JSON_MODE=1; PASS+=("$1");;
        --china) PREFER=gitee; PASS+=("$1");;
        --global) PREFER=github; PASS+=("$1");;
        -h|--help)
            if [ -z "$VERB" ] && [ -f "$0" ]; then usage; exit 0; fi
            PASS+=("$1");;
        *) PASS+=("$1");;
    esac
    shift
done
[ -n "$VERB" ] || VERB=install
# 来源标记：写法和官网统计、下载网关一样（a-z、0-9、-，1～32 个）；不合规的当没有，提醒一句，照常安装
FROM=""
if [ "$FROM_GIVEN" = 1 ]; then
    FROM_RE='^[a-z0-9-]{1,32}$'
    if [[ "$FROM_RAW" =~ $FROM_RE ]]; then FROM="$FROM_RAW"
    else say "来源标记 --from ${FROM_RAW:-（空）} 不合规（只认小写字母、数字、-，最长 32 个字符），已忽略，照常安装"; fi
fi
case "$VERB" in
    install) SCRIPT=install.sh;;
    upgrade) SCRIPT=upgrade.sh;;
    cli) SCRIPT=install-cli.sh;;
    uninstall) SCRIPT=uninstall.sh;;
esac
# Mac 上的安装、升级、卸载走 install-mac.sh（ACORNFOX_GET_OS 只给测试用：假装是别的系统）
OS_NAME="${ACORNFOX_GET_OS:-$(uname -s)}"
MAC=false
if [ "$OS_NAME" = Darwin ]; then
    case "$VERB" in install|upgrade|uninstall) SCRIPT=install-mac.sh; MAC=true;; esac
fi
if [ -n "$VERSION" ]; then
    case "$VERSION" in v[0-9]*) ;; [0-9]*) VERSION="v$VERSION";; *) fail 2 INSTALL-GET-04 "版本号写成 v0.6.0 这样：$VERSION";; esac
    printf '%s' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$' || fail 2 INSTALL-GET-04 "版本号写成 v0.6.0 或 v0.6.0-rc.1 这样：$VERSION"
fi
command -v curl >/dev/null 2>&1 || fail 1 INSTALL-GET-02 "这台机器上没有 curl" "先装上：apt-get install -y curl（Mac 自带）"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/acornfox-get.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

# ---------- 选源 ----------
# >>> 测速选源（get.sh 和 install-mac.sh 里从这一行到“<<<”那一行必须一模一样；scripts/get.test.sh 会逐字核对）
# 没写 --china/--global、也没有安装登记时，同时从 Gitee 和 GitHub 各取同一个发行版文件的开头（最多 1 MiB、最多 PROBE_SECS 秒），
# 比每秒取到多少字节，快的先用。为什么不用“GitHub 连不连得上”：国内常常连得上但极慢（实测 30 KB/s，1 MB/s 以上的 Gitee 反而更快）。
# 为什么按固定时间窗口取而不是只用 Range：Gitee 的发行版文件不认 Range，会一直传，所以到点就停，用“窗口内取到的字节 ÷ 用时”比。
# 结果：PROBE_PICK=gitee|github（空 = 两边都没取到数据）、PROBE_NOTE=给人看的一行。
GITEE_HOST="${ACORNFOX_GITEE_HOST:-https://gitee.com}"      # 只给测试用：换成本机的假服务器
GITHUB_HOST="${ACORNFOX_GITHUB_HOST:-https://github.com}"
PROBE_SECS="${ACORNFOX_PROBE_SECONDS:-5}"
PROBE_PICK=""; PROBE_NOTE=""
probe_url() {  # 源 版本 文件
    case "$1" in
        gitee) echo "${GITEE_HOST}/${GITEE_REPO}/releases/download/$2/$3";;
        github) echo "${GITHUB_HOST}/${GITHUB_REPO}/releases/download/$2/$3";;
    esac
}
probe_start() {  # 源 版本 文件 结果文件：放到后台取开头，结果写成一行“状态码 字节数 用时”
    ( curl -sL -r 0-1048575 --connect-timeout 4 -m "$PROBE_SECS" -o /dev/null -w '%{http_code} %{size_download} %{time_total}' \
        "$(probe_url "$1" "$2" "$3")" > "$4" 2>/dev/null || true ) &
}
probe_bps() {  # 结果文件：打印每秒字节数，没取到数据打 0
    awk 'BEGIN { r = 0 } { if (($1 == 200 || $1 == 206) && $2 > 0 && $3 > 0) r = $2 / $3 } END { printf "%d", r }' "$1" 2>/dev/null || printf 0
}
probe_mb() { awk -v b="$1" 'BEGIN { printf "%.2f", b / 1048576 }'; }
speed_pick() {  # 版本 文件
    local d s g
    d="$(mktemp -d "${TMPDIR:-/tmp}/acornfox-probe.XXXXXX")"
    probe_start gitee "$1" "$2" "$d/gitee"; probe_start github "$1" "$2" "$d/github"; wait
    s="$(probe_bps "$d/gitee")"; g="$(probe_bps "$d/github")"
    # Gitee 偶尔在传输中途卡住，紧接着再连一次几乎都是好的（P-847）：没取到数据的那个再试一次，不要凭一次失败就放弃它
    if [ "$s" = 0 ] || [ "$g" = 0 ]; then
        [ "$s" != 0 ] || probe_start gitee "$1" "$2" "$d/gitee"
        [ "$g" != 0 ] || probe_start github "$1" "$2" "$d/github"
        wait
        s="$(probe_bps "$d/gitee")"; g="$(probe_bps "$d/github")"
    fi
    rm -rf "$d"
    if [ "$s" = 0 ] && [ "$g" = 0 ]; then
        PROBE_PICK=""
        PROBE_NOTE="测速：Gitee、GitHub 都没取到数据（各试了两次），先用 Gitee，取不到再换 GitHub"
    elif [ "$g" = 0 ]; then
        PROBE_PICK=gitee; PROBE_NOTE="测速：Gitee $(probe_mb "$s") MB/s、GitHub 没测通，先用 Gitee"
    elif [ "$s" = 0 ]; then
        PROBE_PICK=github; PROBE_NOTE="测速：Gitee 没测通、GitHub $(probe_mb "$g") MB/s，先用 GitHub"
    elif [ $((s * 10)) -gt $((g * 13)) ]; then
        PROBE_PICK=gitee; PROBE_NOTE="测速：Gitee $(probe_mb "$s") MB/s、GitHub $(probe_mb "$g") MB/s，先用 Gitee"
    else
        # GitHub 更快，或者差不多快（Gitee 不到 1.3 倍）：先用 GitHub，新发布的文件总是先到那里
        PROBE_PICK=github; PROBE_NOTE="测速：Gitee $(probe_mb "$s") MB/s、GitHub $(probe_mb "$g") MB/s，先用 GitHub"
    fi
}
# <<< 测速选源
# 结果在 ORDER 里：先试哪个源、再试哪个源。需要当场测速的（没有任何说法）置 NEED_PROBE=1，等版本定了再测
NEED_PROBE=0
INSTALL_ENV="${ACORNFOX_INSTALL_ENV:-/etc/acornfox/install.env}"   # 只给测试用：换成别的登记文件
pick_order() {
    if [ -n "$BASE" ]; then ORDER="base"; return; fi
    if [ -z "$GITEE_REPO" ]; then ORDER="github"; return; fi
    if [ -z "$PREFER" ] && [ -r "$INSTALL_ENV" ]; then
        if grep -q '^ACORNFOX_IN_CHINA=true' "$INSTALL_ENV" 2>/dev/null; then PREFER=gitee
        elif grep -q '^ACORNFOX_IN_CHINA=false' "$INSTALL_ENV" 2>/dev/null; then PREFER=github; fi
    fi
    if [ -z "$PREFER" ]; then NEED_PROBE=1; PREFER=gitee; fi
    if [ "$PREFER" = gitee ]; then ORDER="gitee github"; else ORDER="github gitee"; fi
}
asset_url() {  # 源 版本 文件
    case "$1" in
        base) echo "${BASE%/}/releases/download/$2/$3";;
        gitee) echo "${GITEE_HOST}/${GITEE_REPO}/releases/download/$2/$3";;
        github) echo "${GITHUB_HOST}/${GITHUB_REPO}/releases/download/$2/$3";;
    esac
}
# 下载某个版本的一个文件：版本 文件 存到哪。按 ORDER 依次试，成功返回 0，并把用的源记在 USED
USED=""
fetch_asset() {
    local src
    for src in $ORDER; do
        if curl -fsSL --connect-timeout 10 --max-time 300 --speed-limit 1024 --speed-time 20 --retry 1 -o "$3" "$(asset_url "$src" "$1" "$2")" 2>/dev/null && [ -s "$3" ]; then
            USED="$src"; return 0
        fi
    done
    return 1
}

# ---------- 定版本 ----------
LATEST_SECS=15
latest_from() {  # 源：打印这个源上的最新正式版号（取不到打印空）
    local tag=""
    case "$1" in
        base) tag="$(curl -fsSL -m "$LATEST_SECS" "${BASE%/}/latest" 2>/dev/null | head -1 | tr -d '[:space:]' || true)";;
        gitee)
            # 不用 Gitee 的 releases/latest：它不保证跳过预发布版。按时间倒序取列表，跳过带“-”的标签
            tag="$(curl -fsS -m "$LATEST_SECS" "${GITEE_HOST}/api/v5/repos/${GITEE_REPO}/releases?page=1&per_page=20&direction=desc" 2>/dev/null \
                | grep -o '"tag_name":"[^"]*"' | cut -d'"' -f4 | grep -v '[-]' | head -1 || true)";;
        github)
            # releases/latest 会跳到最新正式版的页面，从跳转后的地址里取版本号（不用 api.github.com，它有次数限制）
            tag="$(curl -fsSLI -m "$LATEST_SECS" -o /dev/null -w '%{url_effective}' "${GITHUB_HOST}/${GITHUB_REPO}/releases/latest" 2>/dev/null \
                | sed -n 's#.*/releases/tag/\(v[^/?]*\).*#\1#p' || true)";;
    esac
    if printf '%s' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then echo "$tag"; fi
}
latest_formal() {
    local src tag
    for src in $ORDER; do
        tag="$(latest_from "$src")"
        if [ -n "$tag" ]; then echo "$tag"; return 0; fi
    done
    return 1
}
# 还没定用哪个源时查最新版：两个源同时问（最多等 8 秒），两边版本号不一样（Gitee 同步有延迟）取较新的
latest_both() {
    local d v1 v2
    d="$(mktemp -d "${TMPDIR:-/tmp}/acornfox-ver.XXXXXX")"
    ( LATEST_SECS=8; latest_from gitee > "$d/gitee" ) &
    ( LATEST_SECS=8; latest_from github > "$d/github" ) &
    wait
    v1="$(cat "$d/gitee" 2>/dev/null || true)"; v2="$(cat "$d/github" 2>/dev/null || true)"
    rm -rf "$d"
    [ -n "$v1$v2" ] || return 1
    printf '%s\n%s\n' "${v1:-v0.0.0}" "${v2:-v0.0.0}" | sort -t. -k1.2,1n -k2,2n -k3,3n | tail -1
}
# 测速用哪个文件：这台机器上要用的那个程序（只取开头，不会装）
probe_asset() {
    local os arch
    case "$OS_NAME" in Darwin) os=darwin;; *) os=linux;; esac
    case "$(uname -m)" in aarch64|arm64) arch=arm64;; *) arch=amd64;; esac
    echo "acornfox_${os}_${arch}"
}
# 版本号的数字部分不低于 CHECKSUM_SINCE 时返回 0（v0.6.0-rc.1 按 0.6.0 算）
checksums_cover_scripts() {
    local core="${1#v}"; core="${core%%-*}"
    [ "$(printf '%s\n%s\n' "$CHECKSUM_SINCE" "$core" | sort -t. -k1,1n -k2,2n -k3,3n | head -1)" = "$CHECKSUM_SINCE" ]
}
sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi
}

pick_order
if [ -z "$VERSION" ]; then
    if [ "$VERB" = uninstall ] && ! $MAC && [ -x /usr/local/bin/acornfox ]; then
        # 卸载用“现在装着的那个版本”自带的卸载脚本
        cur="$({ /usr/local/bin/acornfox version 2>/dev/null || true; } | awk '{print $2}')"
        if printf '%s' "$cur" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$'; then VERSION="v$cur"; fi
    fi
    if [ -z "$VERSION" ] && [ "$NEED_PROBE" = 1 ]; then
        # 还没有任何说法：两个源一起问最新版号，再拿这个版本的文件测速
        VERSION="$(latest_both)" || fail 1 INSTALL-GET-01 "查不到最新的正式版号（Gitee、GitHub 都试过）" \
            "先看能不能连上：curl -I https://gitee.com 和 curl -I https://github.com" \
            "也可以直接指定版本：在命令后面加 -s -- --version v0.6.0 这样的参数"
        say "最新正式版：$VERSION"
    fi
fi
PROBE_DECIDED=""
if [ "$NEED_PROBE" = 1 ]; then
    speed_pick "${VERSION:-}" "$(probe_asset)"
    say "$PROBE_NOTE"
    if [ -n "$PROBE_PICK" ]; then
        PREFER="$PROBE_PICK"; PROBE_DECIDED="$PROBE_PICK"
    else
        PREFER=gitee
    fi
    if [ "$PREFER" = gitee ]; then ORDER="gitee github"; else ORDER="github gitee"; fi
fi
if [ -z "$VERSION" ]; then
    VERSION="$(latest_formal)" || fail 1 INSTALL-GET-01 "查不到最新的正式版号（$(echo $ORDER | sed 's/ /、/g') 都试过）" \
        "先看能不能连上：curl -I https://gitee.com 和 curl -I https://github.com" \
        "也可以直接指定版本：在命令后面加 -s -- --version v0.6.0 这样的参数"
    say "最新正式版：$VERSION"
fi

# ---------- 下载那个版本自带的脚本，校验 ----------
say "取 $VERSION 自带的 $SCRIPT"
if ! fetch_asset "$VERSION" "$SCRIPT" "$TMP/$SCRIPT"; then
    if [ "$VERB" = uninstall ]; then
        # 现在装着的版本没有对应的发行版（例如开发版）：改用最新正式版的卸载脚本
        v2="$(latest_formal || true)"
        if [ -n "$v2" ] && [ "$v2" != "$VERSION" ] && fetch_asset "$v2" "$SCRIPT" "$TMP/$SCRIPT"; then
            say "$VERSION 没有发行版，改用 $v2 的卸载脚本"; VERSION="$v2"
        else
            fail 1 INSTALL-GET-02 "下载 $VERSION 的 $SCRIPT 失败（$(echo $ORDER | sed 's/ /、/g') 都试过）" "核对版本号是否存在；过一会儿再试"
        fi
    else
        fail 1 INSTALL-GET-02 "下载 $VERSION 的 $SCRIPT 失败（$(echo $ORDER | sed 's/ /、/g') 都试过）" \
            "核对版本号是否存在：https://gitee.com/${GITEE_REPO:-VIP13390/AcornFox}/releases" "网络不通时过一会儿再试"
    fi
fi
SRC_USED="$USED"
want=""
if fetch_asset "$VERSION" SHA256SUMS "$TMP/SHA256SUMS"; then
    want="$(awk -v a="$SCRIPT" '$2 == a || $2 == "*" a {print $1; exit}' "$TMP/SHA256SUMS")"
elif checksums_cover_scripts "$VERSION"; then
    fail 1 INSTALL-GET-02 "下载 $VERSION 的 SHA256SUMS 失败，没法校验 $SCRIPT，不执行" "过一会儿再试"
fi
if [ -n "$want" ]; then
    got="$(sha256_of "$TMP/$SCRIPT")"
    [ "$got" = "$want" ] || fail 1 INSTALL-GET-03 "$SCRIPT 校验不过：发行版记的是 ${want}，下载到的是 ${got}。没有执行它" \
        "多半是下载被截断或被替换。换一个源再试：加 --china 或 --global" "两个源都不过时不要强行安装，先反馈"
    say "$SCRIPT 校验通过（来自 $SRC_USED）"
elif checksums_cover_scripts "$VERSION"; then
    fail 1 INSTALL-GET-03 "$VERSION 的 SHA256SUMS 里没有 $SCRIPT 这一项，没法校验，不执行" "这个版本的发行版不完整，先反馈；也可以换一个版本"
else
    say "提示：$VERSION 的发行版没有给脚本提供校验值（${CHECKSUM_SINCE} 之前的版本都这样），照常继续"
fi

# ---------- 交给它 ----------
# 测速定下来的先后顺序一路传下去（用户自己写了 --china/--global 的、读安装登记的不在这里：前者已在 PASS 里，后者下一个脚本自己会读）；
# 卸载不下载程序，不用传
if [ -n "$PROBE_DECIDED" ]; then
    case "$VERB" in
        install|upgrade|cli) if [ "$PROBE_DECIDED" = gitee ]; then PASS+=("--china"); else PASS+=("--global"); fi;;
    esac
    export ACORNFOX_GET_PROBE_NOTE="$PROBE_NOTE"
fi
# 来源标记：只在安装时往下传；升级、卸载、装命令行不带。老版本的脚本不认 --from（会报未知参数），
# 所以先看要交出去的这份脚本里有没有“AFX-FEATURE: install-from”这一行，没有就不传
if [ -n "$FROM" ]; then
    if [ "$VERB" != install ]; then
        say "来源标记只在安装时用，$VERB 不带它，已忽略"
    elif grep -q 'AFX-FEATURE: install-from' "$TMP/$SCRIPT" 2>/dev/null; then
        PASS+=("--from" "$FROM")
    else
        say "$VERSION 的 $SCRIPT 还不认来源标记（--from），没往下传，照常安装"
    fi
fi
if $MAC; then
    # install-mac.sh 的第一个参数是动作；版本一律交给它（卸载时它会优先用已经装着的程序）
    set -- "$VERB" ${PASS[@]+"${PASS[@]}"} --version "$VERSION"
else
    case "$VERB" in
        install|upgrade) set -- ${PASS[@]+"${PASS[@]}"} --version "$VERSION";;
        cli) set -- "$VERSION" ${PASS[@]+"${PASS[@]}"};;
        uninstall) set -- ${PASS[@]+"${PASS[@]}"};;
    esac
fi
say "交给 $VERSION 的 $SCRIPT"
# 把临时目录留给它用完再删：exec 之后本脚本的退出清理不再执行，所以脚本拷到一个单独的临时文件里再执行
RUN="$(mktemp "${TMPDIR:-/tmp}/acornfox-${SCRIPT%.sh}.XXXXXX")"
cp "$TMP/$SCRIPT" "$RUN"
rm -rf "$TMP"; trap - EXIT
export ACORNFOX_REPO="$GITHUB_REPO" ACORNFOX_GITEE_REPO="$GITEE_REPO" ACORNFOX_VIA_GET=1
# 版本是引导替用户选的（没写 --version）：升级脚本据此在“当前版本不比它旧”时说不用升级，而不是当成降级
[ "$VERSION_GIVEN" = 1 ] || export ACORNFOX_GET_DEFAULTED=1
exec bash "$RUN" "$@"
