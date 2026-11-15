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
#   只装命令行（Mac 或 Linux 电脑上，不用 sudo）： … | bash -s -- cli
#   指定版本（含预发布版）： … | sudo bash -s -- --version v0.6.0
#   其余参数原样交给那个版本的脚本，例如： … | sudo bash -s -- --china --no-lan-console
#   已经下载到本地：bash get.sh [install|upgrade|cli|uninstall] [--version vX.Y.Z] [参数…]；bash get.sh --help
#
# 选源：加 --china 先用 Gitee，加 --global 先用 GitHub；都不加时先看这台机器以前安装时记下的判断，
#   没有就试一下 GitHub 通不通。一个源取不到会自动换另一个。
# 校验：下载的脚本要和同一版本 SHA256SUMS 里的值一致，不一致就停下。v0.6.0 之前的版本没有给脚本提供校验值，会提示一句照常继续。
# 退出码：交给那个版本的脚本之后就是它的退出码；引导自己出错时是 1（用法不对是 2），并打印故障编号：
#   INSTALL-GET-01 查不到最新正式版号   INSTALL-GET-02 下载失败（两个源都试过）   INSTALL-GET-03 校验不过   INSTALL-GET-04 用法不对
# 副作用：只在临时目录里放下载的脚本和校验文件，退出时删掉。真正的改动都由那个版本的脚本做。
# 环境变量：ACORNFOX_REPO、ACORNFOX_GITEE_REPO（换仓库）；
#   ACORNFOX_RELEASE_BASE（测试或自建镜像用：设了就只从这里取，目录结构是
#   <地址>/releases/download/<版本>/<文件>，<地址>/latest 里一行写最新正式版号）。
# 出错先看哪里：屏幕上的故障编号和它上面那几行；能连上哪个源：curl -I https://gitee.com、curl -I https://github.com。
# 兼容 macOS 自带的 bash 3.2（装命令行时会在 Mac 上运行）。

set -euo pipefail

GITHUB_REPO="${ACORNFOX_REPO:-OrbitMaker/AcornFox}"
GITEE_REPO="${ACORNFOX_GITEE_REPO-VIP13390/AcornFox}"
BASE="${ACORNFOX_RELEASE_BASE:-}"
# 从这个版本起，发行版的 SHA256SUMS 覆盖全部文件（含脚本）；更早的版本只覆盖程序文件
CHECKSUM_SINCE="0.6.0"

say() { echo "[引导] $*"; }
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
VERB=""; VERSION=""; PREFER=""; VERSION_GIVEN=0
PASS=()
while [ $# -gt 0 ]; do
    case "$1" in
        install|upgrade|cli|uninstall)
            if [ -z "$VERB" ]; then VERB="$1"; else PASS+=("$1"); fi;;
        --version) [ $# -ge 2 ] || fail 2 INSTALL-GET-04 "--version 后面要跟版本号，例如 --version v0.6.0"; VERSION="$2"; VERSION_GIVEN=1; shift;;
        --version=*) VERSION="${1#*=}"; VERSION_GIVEN=1;;
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
case "$VERB" in
    install) SCRIPT=install.sh;;
    upgrade) SCRIPT=upgrade.sh;;
    cli) SCRIPT=install-cli.sh;;
    uninstall) SCRIPT=uninstall.sh;;
esac
if [ -n "$VERSION" ]; then
    case "$VERSION" in v[0-9]*) ;; [0-9]*) VERSION="v$VERSION";; *) fail 2 INSTALL-GET-04 "版本号写成 v0.6.0 这样：$VERSION";; esac
    printf '%s' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$' || fail 2 INSTALL-GET-04 "版本号写成 v0.6.0 或 v0.6.0-rc.1 这样：$VERSION"
fi
command -v curl >/dev/null 2>&1 || fail 1 INSTALL-GET-02 "这台机器上没有 curl" "先装上：apt-get install -y curl（Mac 自带）"

TMP="$(mktemp -d "${TMPDIR:-/tmp}/acornfox-get.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

# ---------- 选源 ----------
# 结果在 ORDER 里：先试哪个源、再试哪个源
pick_order() {
    if [ -n "$BASE" ]; then ORDER="base"; return; fi
    if [ -z "$GITEE_REPO" ]; then ORDER="github"; return; fi
    if [ -z "$PREFER" ] && [ -r /etc/acornfox/install.env ]; then
        if grep -q '^ACORNFOX_IN_CHINA=true' /etc/acornfox/install.env 2>/dev/null; then PREFER=gitee
        elif grep -q '^ACORNFOX_IN_CHINA=false' /etc/acornfox/install.env 2>/dev/null; then PREFER=github; fi
    fi
    if [ -z "$PREFER" ]; then
        # 都没说：GitHub 5 秒内连得上就先用它，否则先用 Gitee
        if curl -fsI -m 5 -o /dev/null "https://github.com/${GITHUB_REPO}/releases/latest" 2>/dev/null; then PREFER=github; else PREFER=gitee; fi
    fi
    if [ "$PREFER" = gitee ]; then ORDER="gitee github"; else ORDER="github gitee"; fi
}
asset_url() {  # 源 版本 文件
    case "$1" in
        base) echo "${BASE%/}/releases/download/$2/$3";;
        gitee) echo "https://gitee.com/${GITEE_REPO}/releases/download/$2/$3";;
        github) echo "https://github.com/${GITHUB_REPO}/releases/download/$2/$3";;
    esac
}
# 下载某个版本的一个文件：版本 文件 存到哪。按 ORDER 依次试，成功返回 0，并把用的源记在 USED
USED=""
fetch_asset() {
    local src
    for src in $ORDER; do
        if curl -fsSL --connect-timeout 10 --max-time 300 --retry 1 -o "$3" "$(asset_url "$src" "$1" "$2")" 2>/dev/null && [ -s "$3" ]; then
            USED="$src"; return 0
        fi
    done
    return 1
}

# ---------- 定版本 ----------
latest_formal() {
    local src tag
    for src in $ORDER; do
        tag=""
        case "$src" in
            base) tag="$(curl -fsSL -m 15 "${BASE%/}/latest" 2>/dev/null | head -1 | tr -d '[:space:]' || true)";;
            gitee)
                # 不用 Gitee 的 releases/latest：它不保证跳过预发布版。按时间倒序取列表，跳过带“-”的标签
                tag="$(curl -fsS -m 15 "https://gitee.com/api/v5/repos/${GITEE_REPO}/releases?page=1&per_page=20&direction=desc" 2>/dev/null \
                    | grep -o '"tag_name":"[^"]*"' | cut -d'"' -f4 | grep -v '[-]' | head -1 || true)";;
            github)
                # releases/latest 会跳到最新正式版的页面，从跳转后的地址里取版本号（不用 api.github.com，它有次数限制）
                tag="$(curl -fsSLI -m 15 -o /dev/null -w '%{url_effective}' "https://github.com/${GITHUB_REPO}/releases/latest" 2>/dev/null \
                    | sed -n 's#.*/releases/tag/\(v[^/?]*\).*#\1#p' || true)";;
        esac
        if printf '%s' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then echo "$tag"; return 0; fi
    done
    return 1
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
    if [ "$VERB" = uninstall ] && [ -x /usr/local/bin/acornfox ]; then
        # 卸载用“现在装着的那个版本”自带的卸载脚本
        cur="$({ /usr/local/bin/acornfox version 2>/dev/null || true; } | awk '{print $2}')"
        if printf '%s' "$cur" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$'; then VERSION="v$cur"; fi
    fi
    if [ -z "$VERSION" ]; then
        VERSION="$(latest_formal)" || fail 1 INSTALL-GET-01 "查不到最新的正式版号（$(echo $ORDER | sed 's/ /、/g') 都试过）" \
            "先看能不能连上：curl -I https://gitee.com 和 curl -I https://github.com" \
            "也可以直接指定版本：在命令后面加 -s -- --version v0.6.0 这样的参数"
        say "最新正式版：$VERSION"
    fi
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
case "$VERB" in
    install|upgrade) set -- ${PASS[@]+"${PASS[@]}"} --version "$VERSION";;
    cli) set -- "$VERSION" ${PASS[@]+"${PASS[@]}"};;
    uninstall) set -- ${PASS[@]+"${PASS[@]}"};;
esac
say "交给 $VERSION 的 $SCRIPT"
# 把临时目录留给它用完再删：exec 之后本脚本的退出清理不再执行，所以脚本拷到一个单独的临时文件里再执行
RUN="$(mktemp "${TMPDIR:-/tmp}/acornfox-${SCRIPT%.sh}.XXXXXX")"
cp "$TMP/$SCRIPT" "$RUN"
rm -rf "$TMP"; trap - EXIT
export ACORNFOX_REPO="$GITHUB_REPO" ACORNFOX_GITEE_REPO="$GITEE_REPO" ACORNFOX_VIA_GET=1
# 版本是引导替用户选的（没写 --version）：升级脚本据此在“当前版本不比它旧”时说不用升级，而不是当成降级
[ "$VERSION_GIVEN" = 1 ] || export ACORNFOX_GET_DEFAULTED=1
exec bash "$RUN" "$@"
