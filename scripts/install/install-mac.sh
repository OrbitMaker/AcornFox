#!/usr/bin/env bash
# AcornFox 的 Mac 安装脚本：在 Mac（苹果芯片，macOS 26 起）上安装、升级、卸载 AcornFox。
# 用户的一行命令是 get.sh（在 Mac 上它取“那个版本自带的”这份脚本再交给它）；这份脚本不直接给用户用。
#
# 它自己只做一件事：取那个版本的 Mac 程序 acornfox_darwin_arm64、按 SHA256SUMS 校验，然后运行
#   acornfox mac <install|upgrade|uninstall> …
# 真正的工作（检查、装苹果的 container、建 Debian 虚拟机、在里面装 AcornFox、常驻转发程序、开机自启、
# acornfox 命令）都在这个程序里（internal/macvm），所以有 --plan 试跑、--json、故障编号、doctor，
# 装好以后同一个程序就是 acornfox 命令。
#
# 用法（在 Mac 的“终端”里，普通用户身份，不要加 sudo）：
#   国内： curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | bash
#   海外： curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | bash
#   升级： … | bash -s -- upgrade
#   卸载： … | bash -s -- uninstall            （保留虚拟机和里面的数据；连数据一起删加 --purge --yes）
#   试跑： … | bash -s -- install --plan       （只说要做什么，什么都不改）
#   其余参数原样交给 acornfox mac：--china、--global、--memory 6、--idle 30、--no-lan、--no-open、--json、--version vX.Y.Z …
#   （acornfox mac --help 看全部）
# 前提：Apple 芯片的 Mac、macOS 26 或更新、磁盘剩 12 GB 以上、能联网。第一次装且没有苹果的 container 时，会让你输一次这台
#   Mac 的登录密码（装苹果签名的 container 安装包要管理员权限）；这一步 AI 工作台不会代输，由你自己在终端里输。
# 验证：装完 acornfox mac status 显示虚拟机、常驻程序都正常；acornfox mac doctor 逐项检查。
# 停下或回滚：acornfox mac stop（停虚拟机，有人访问自动叫起）；acornfox mac uninstall（卸载，保留数据）；--purge --yes（连数据删掉）。
# 退出码：交给 acornfox mac 之后就是它的退出码（0 成功、1 出错、2 用法不对、5 要管理员密码而这里没有终端）；
#   本脚本自己出错是 1，并打印故障编号：MAC-OS-01（不是 Mac 或系统太旧）、MAC-ARCH-01（Intel 的 Mac）、
#   MAC-DL-02（取不到发行版的程序或校验不过）、MAC-SRC-01（源码版脚本，没有版本号）。
# 副作用：只在临时目录里放下载的程序，结束时删掉；真正的改动都由 acornfox mac 做（它的 --plan 能先看）。
# 环境变量：ACORNFOX_REPO、ACORNFOX_GITEE_REPO（换仓库）、ACORNFOX_RELEASE_BASE（测试或自建镜像：从
#   <地址>/releases/download/<版本>/ 取文件）、ACORNFOX_MAC_HOME（状态目录）、ACORNFOX_INSTALL_DIR（acornfox 命令装到哪）。
# 出错先看哪里：屏幕上的故障编号和它上面几行；~/Library/Application Support/AcornFox/mac/logs/install.log。
# 兼容 macOS 自带的 bash 3.2。

set -euo pipefail

SCRIPT_VERSION="dev"

GITHUB_REPO="${ACORNFOX_REPO:-OrbitMaker/AcornFox}"
GITEE_REPO="${ACORNFOX_GITEE_REPO-VIP13390/AcornFox}"
BASE="${ACORNFOX_RELEASE_BASE:-}"

JSON_MODE=0
say() { if [ "$JSON_MODE" = 1 ]; then echo "[AcornFox] $*" >&2; else echo "[AcornFox] $*"; fi; }
fail() {  # fail 故障编号 说明 [处理办法…]
    local code="$1" msg="$2" line
    shift 2
    echo "[AcornFox] 出错：$msg" >&2
    for line in "$@"; do echo "    $line" >&2; done
    echo "故障编号: ${code}" >&2
    exit 1
}
usage() { awk 'NR>1 && !/^#/ {exit} NR>1' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'; }

# ---------- 参数：第一个动作词归我，其余都交给 acornfox mac ----------
VERB=""; VERSION=""; PREFER=""
PASS=()
while [ $# -gt 0 ]; do
    case "$1" in
        install|upgrade|uninstall)
            if [ -z "$VERB" ]; then VERB="$1"; else PASS+=("$1"); fi;;
        --version) [ $# -ge 2 ] || fail MAC-SRC-01 "--version 后面要跟版本号，例如 --version v0.7.0"; VERSION="$2"; shift;;
        --version=*) VERSION="${1#*=}";;
        --json) JSON_MODE=1; PASS+=("$1");;
        --china) PREFER=gitee; PASS+=("$1");;
        --global) PREFER=github; PASS+=("$1");;
        -h|--help) usage; exit 0;;
        *) PASS+=("$1");;
    esac
    shift
done
[ -n "$VERB" ] || VERB=install

# ---------- 这是不是能装的 Mac ----------
[ "$(uname -s)" = Darwin ] || fail MAC-OS-01 "这不是 Mac。Linux 服务器请用 get.sh 装在服务器上，Windows 请用 get.ps1"
[ "$(uname -m)" = arm64 ] || fail MAC-ARCH-01 "这台 Mac 不是苹果芯片（Intel）。苹果的 container 只在苹果芯片上运行，装不了" \
    "可以把 AcornFox 装在一台 Linux 服务器上，用这台 Mac 上的命令行管理（get.sh cli，然后 acornfox target add）"
[ "$(id -u)" != 0 ] || fail MAC-OS-01 "不要用 sudo 或 root 运行：这会把东西装到 root 的家目录里。用普通用户在“终端”里直接运行" \
    "要管理员密码的那一步，程序会自己在需要的时候请你输"

# ---------- 卸载：优先用已经装着的那份程序，不用联网 ----------
STATE="${ACORNFOX_MAC_HOME:-$HOME/Library/Application Support/AcornFox/mac}"
if [ "$VERB" = uninstall ] && [ -x "$STATE/bin/acornfox" ]; then
    exec "$STATE/bin/acornfox" mac uninstall ${PASS[@]+"${PASS[@]}"}
fi

# ---------- 版本 ----------
if [ -z "$VERSION" ]; then VERSION="$SCRIPT_VERSION"; fi
case "$VERSION" in
    dev) fail MAC-SRC-01 "这是源码里的安装脚本（没有版本号），不能直接用" "用 get.sh 安装，或加 --version vX.Y.Z";;
    v[0-9]*) ;;
    [0-9]*) VERSION="v$VERSION";;
esac
command -v curl >/dev/null 2>&1 || fail MAC-DL-02 "找不到 curl"

# ---------- 选源（和 get.sh 同一套规矩） ----------
if [ -n "$BASE" ]; then ORDER="base"
elif [ -z "$GITEE_REPO" ]; then ORDER="github"
else
    if [ -z "$PREFER" ]; then
        if curl -fsI -m 5 -o /dev/null "https://github.com/${GITHUB_REPO}/releases/latest" 2>/dev/null; then PREFER=github; else PREFER=gitee; fi
    fi
    if [ "$PREFER" = gitee ]; then ORDER="gitee github"; else ORDER="github gitee"; fi
fi
asset_url() {  # 源 文件
    case "$1" in
        base) echo "${BASE%/}/releases/download/$VERSION/$2";;
        gitee) echo "https://gitee.com/${GITEE_REPO}/releases/download/$VERSION/$2";;
        github) echo "https://github.com/${GITHUB_REPO}/releases/download/$VERSION/$2";;
    esac
}
TMP="$(mktemp -d "${TMPDIR:-/tmp}/acornfox-mac.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
fetch() {  # 文件 存到哪：按 ORDER 依次试；Gitee 偶尔中途卡住，每个源试两次
    local src n
    for src in $ORDER; do
        for n in 1 2; do
            if curl -fsSL --connect-timeout 10 --max-time 300 -o "$2" "$(asset_url "$src" "$1")" 2>/dev/null && [ -s "$2" ]; then return 0; fi
        done
    done
    return 1
}
sha256_of() { shasum -a 256 "$1" | awk '{print $1}'; }

# ---------- 取 Mac 程序，校验 ----------
BIN="$TMP/acornfox"
say "取 $VERSION 的 Mac 程序（acornfox_darwin_arm64）"
fetch acornfox_darwin_arm64 "$BIN" || fail MAC-DL-02 "下载 $VERSION 的 acornfox_darwin_arm64 失败（$(echo $ORDER | sed 's/ /、/g') 都试过）" \
    "核对版本号是否存在：https://gitee.com/${GITEE_REPO:-VIP13390/AcornFox}/releases" "网络不通时过一会儿再试，或加 --china、--global 换一个先试的源"
fetch SHA256SUMS "$TMP/SHA256SUMS" || fail MAC-DL-02 "下载 $VERSION 的 SHA256SUMS 失败，没法校验，不运行" "过一会儿再试"
want="$(awk '$2 == "acornfox_darwin_arm64" || $2 == "*acornfox_darwin_arm64" {print $1; exit}' "$TMP/SHA256SUMS")"
[ -n "$want" ] || fail MAC-DL-02 "$VERSION 的 SHA256SUMS 里没有 acornfox_darwin_arm64 这一项，没法校验，不运行" "这个版本的发行版不完整，先反馈"
got="$(sha256_of "$BIN")"
[ "$got" = "$want" ] || fail MAC-DL-02 "acornfox_darwin_arm64 校验不过：发行版记的是 ${want}，下载到的是 ${got}。没有运行它" \
    "多半是下载被截断或被替换。换一个源再试：加 --china 或 --global"
chmod +x "$BIN"
say "程序校验通过，交给 acornfox mac $VERB"

# ---------- 交给它（不用 exec：它跑完要把临时目录删掉；退出码照传） ----------
rc=0
"$BIN" mac "$VERB" --version "$VERSION" ${PASS[@]+"${PASS[@]}"} || rc=$?
exit "$rc"
