#!/usr/bin/env bash
# shellcheck disable=SC1111  # 中文提示里的全角引号是正文，不是 shell 引号
# AcornFox 安装脚本
# 用途: 在干净的 Linux 系统上一键安装 AcornFox
# 支持: Ubuntu 22.04/24.04, Debian 12/13，x86_64（这四个版本都实测过安装、自检、部署；
#       升级在 Ubuntu 24.04 上实测；推荐 Ubuntu 24.04 或 Debian 13；
#       其他 Ubuntu/Debian 版本要加 --force-os）
# 许可: AGPL-3.0
#
# 用法（国内）: curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/install.sh | sudo bash
# 用法（海外）: curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/install.sh | sudo bash
# 参数用 | sudo bash -s -- --china 这样传。
# 安装过程中不会询问；-y 可以加也可以不加（与 upgrade.sh 一致）。
#
# 可选环境变量 ACORNFOX_DOCKER_CE_MIRROR：自己指定 docker-ce 软件源地址（企业内网镜像，
# 或用 apt 缓存代理时用 http 地址，例如 http://mirrors.huaweicloud.com/docker-ce）。设了就排在
# 所有 docker-ce 候选源最前面，同样先探测能不能用，用不了再按默认顺序换源。传法:
#   curl -fsSL …/install.sh | sudo ACORNFOX_DOCKER_CE_MIRROR=http://… bash
#
# 故障编号（脚本停下时打印“故障编号: XXX”，AI 工作台据此处理）:
#   INSTALL-ROOT-01  没有用 root 或 sudo 运行
#   INSTALL-OS-01    系统不是 Ubuntu/Debian（如 Alibaba Cloud Linux、OpenCloudOS、TencentOS、CentOS、Rocky）
#   INSTALL-OS-02    Ubuntu/Debian 版本不在支持列表（加 --force-os 可继续，自行承担风险）
#   INSTALL-ARCH-01  CPU 架构不是 x86_64 或 arm64
#   INSTALL-DISK-01  数据盘剩余空间不足 2 GB
#   INSTALL-PORT-01  AcornFox 自己要用的本机端口（18800、18801、18808）被别的程序占用，不能继续
#   INSTALL-PORT-02  80 或 443 被别的程序占用（加 --allow-busy-80-443 可继续，但绑定域名、HTTPS、公网控制台不能用）
#   INSTALL-PORT-03  应用端口段 18810-18899 中有端口被别的程序占用，不能继续
#   INSTALL-CLOUD-01 ACORNFOX_CLOUD_OVERRIDE / --cloud 的值不认识
#   INSTALL-DOWN-01  这台服务器上已经装着更新的 AcornFox（或留着卸载时保留的更新版本的数据），要装的版本比它旧
#                    （只能升级，不能降级），什么都没改

set -euo pipefail

# 本次安装的临时文件都放在自己新建的目录里（只有 root 能读写），退出时删掉。
# 不用固定的 /tmp/xxx：别的用户先建了同名文件时，root 也写不进去（fs.protected_regular），会报 curl 23。
AFX_WORK=$(mktemp -d /tmp/acornfox-install.XXXXXX)
trap 'rm -rf "$AFX_WORK"' EXIT

# curl | sudo bash 时 stdin 是脚本本身，不能让 apt/debconf 交互读取它
export DEBIAN_FRONTEND=noninteractive

# 新开的云主机开机后 unattended-upgrades 会占用 dpkg 锁几分钟，等锁释放而不是直接失败。
# DPkg::Lock::Timeout 不管 apt-get update 用的软件包列表锁（/var/lib/apt/lists/lock），
# 这把锁被占时 update 会立刻失败，所以 update 另外每 10 秒重试，最多 10 分钟。
# 输出不是终端（AI 工作台、日志文件）时加 Dpkg::Use-Pty=0：apt 默认给 dpkg 开伪终端，
# dpkg 以为在终端里，会把“(Reading database ... 5%”这样的进度刷成一大串。
APT_TTY_OPTS=()
[ -t 1 ] || APT_TTY_OPTS=(-o Dpkg::Use-Pty=0)

apt_get() {
    if [ "${1:-}" = update ]; then
        local i
        for i in $(seq 1 60); do
            if apt-get -o DPkg::Lock::Timeout=600 -o Acquire::Retries=3 "${APT_TTY_OPTS[@]}" "$@" 2>"$AFX_WORK/apt-update.err"; then
                rm -f "$AFX_WORK/apt-update.err"
                return 0
            fi
            grep -q "Could not get lock" "$AFX_WORK/apt-update.err" || { cat "$AFX_WORK/apt-update.err" >&2; return 1; }
            [ "$i" = 1 ] && echo "[INFO] 软件包列表被其他进程（通常是系统自动更新）占用，等待释放..."
            sleep 10
        done
        cat "$AFX_WORK/apt-update.err" >&2
        return 1
    fi
    apt-get -o DPkg::Lock::Timeout=600 -o Acquire::Retries=3 "${APT_TTY_OPTS[@]}" "$@"
}

# 颜色输出
# 用 $'...' 存真实的转义字符，摘要里的 cat << EOF 才不会原样打印 \033；
# 输出不是终端（管道、日志文件）时不加颜色。
if [ -t 1 ]; then
    RED=$'\033[0;31m'
    GREEN=$'\033[0;32m'
    YELLOW=$'\033[1;33m'
    BLUE=$'\033[0;34m'
    NC=$'\033[0m'
else
    RED='' GREEN='' YELLOW='' BLUE='' NC=''
fi

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 错误处理
error_exit() {
    log_error "$1"
    exit 1
}

# fault_exit 编号 说明 [处理办法...]：带故障编号停下
fault_exit() {
    local code="$1" msg="$2" line
    shift 2
    log_error "$msg"
    for line in "$@"; do
        echo "    $line"
    done
    echo "故障编号: ${code}"
    exit 1
}

# curl_get OUT URL [curl 参数...]：下载文件。输出是终端时显示 curl 自带的进度条；不是终端时
# （AI 工作台、管道、日志文件）不刷进度条，只在开始、结束各打一行。返回 curl 的退出码。
curl_get() {
    local out="$1" url="$2" t0 rc=0 size
    shift 2
    if [ -t 2 ]; then
        curl -fL "$@" -o "$out" "$url" || rc=$?
        return "$rc"
    fi
    log_info "开始下载: ${url}"
    t0=$(date +%s)
    curl -fsSL "$@" -o "$out" "$url" || rc=$?
    if [ "$rc" = 0 ]; then
        size=$(stat -c %s "$out" 2>/dev/null || echo 0)
        log_info "下载完成: $(awk -v b="$size" 'BEGIN { if (b >= 1048576) printf "%.1f MB", b / 1048576; else printf "%.0f KB", b / 1024 }')，用时 $(( $(date +%s) - t0 )) 秒"
    else
        log_warn "下载没成功（curl 退出码 ${rc}）: ${url}"
    fi
    return "$rc"
}

# GitHub 仓库与下载代理
# ACORNFOX_REPO 可覆盖发布仓库；ACORNFOX_GITHUB_PROXY 指定自己的 GitHub 加速前缀
# （形如 https://example.com/，拼接为 前缀+原始地址）。中国大陆先直连，失败再依次试代理。
ACORNFOX_REPO="${ACORNFOX_REPO:-OrbitMaker/AcornFox}"
GITHUB_PROXIES=()

setup_github_proxies() {
    GITHUB_PROXIES=("")
    if [ -n "${ACORNFOX_GITHUB_PROXY:-}" ]; then
        GITHUB_PROXIES+=("${ACORNFOX_GITHUB_PROXY%/}/")
    fi
    if $IN_CHINA; then
        GITHUB_PROXIES+=("https://ghfast.top/" "https://gh-proxy.com/")
    fi
}

# github_download URL OUT：依次直连、走代理下载 GitHub 文件
github_download() {
    local url="$1" out="$2" p
    # 连续 15 秒低于 50KB/s 就放弃当前源换下一个：国内直连 GitHub 常只有十几 KB/s，
    # 不限速时要等满 --max-time 才会轮到代理。所有源都太慢时，最后不限速再试一次。
    for p in "${GITHUB_PROXIES[@]}"; do
        if [ -n "$p" ]; then
            log_info "尝试通过 ${p} 下载"
        fi
        if curl_get "$out" "${p}${url}" --connect-timeout 10 --max-time 600 --speed-limit 51200 --speed-time 15; then
            return 0
        fi
    done
    log_info "所有下载源都较慢，不限速重试一次"
    curl_get "$out" "${GITHUB_PROXIES[0]}${url}" --connect-timeout 10 --max-time 1800 --retry 2
}

# 国内镜像：Gitee 发行版与 GitHub Release 的文件相同（同一份 SHA256SUMS 校验）。
# 中国大陆服务器先从 Gitee 下载，失败再走 GitHub 直连和代理；ACORNFOX_GITEE_REPO 设为空可关闭。
ACORNFOX_GITEE_REPO="${ACORNFOX_GITEE_REPO-VIP13390/AcornFox}"

# 给人照抄的脚本地址（提示和帮助里打印真实地址，不写占位符）
SCRIPT_URL_GITEE="https://gitee.com/${ACORNFOX_GITEE_REPO:-VIP13390/AcornFox}/raw/main/scripts/install"
SCRIPT_URL_GITHUB="https://github.com/${ACORNFOX_REPO}/releases/latest/download"

# release_download VERSION ASSET OUT：下载某个版本的 Release 文件
release_download() {
    local version="$1" asset="$2" out="$3"
    if $IN_CHINA && [ -n "$ACORNFOX_GITEE_REPO" ]; then
        log_info "从 Gitee 镜像下载 ${asset}"
        if curl_get "$out" "https://gitee.com/${ACORNFOX_GITEE_REPO}/releases/download/${version}/${asset}" \
            --connect-timeout 10 --max-time 600 --speed-limit 51200 --speed-time 15; then
            return 0
        fi
        log_warn "Gitee 镜像下载失败，改从 GitHub 下载"
    fi
    github_download "https://github.com/${ACORNFOX_REPO}/releases/download/${version}/${asset}" "$out"
}

# 公开仓库 main 上的脚本可能比最新发行版新（例如 main 已是 0.4.0 的脚本，最新发行版还是 v0.3.1）。
# 要装的版本早于 SCRIPT_MIN_VERSION 时，改用那个版本发行版里附带的同名脚本，参数原样传过去，
# 避免新脚本配旧程序（新脚本会把宿主机 Caddy 换成容器，旧程序不认识）。--binary 时不切换。
SCRIPT_MIN_VERSION="v0.4.0"

# version_lt A B：A 早于 B 时返回 0（vX.Y.Z，按 sort -V 比较；0.4.0-dev.N 视为不早于 0.4.0）
version_lt() {
    [ "$1" = "$2" ] && return 1
    [ "$(printf '%s\n%s\n' "${1#v}" "${2#v}" | sort -V | head -1)" = "${1#v}" ]
}

# version_older T C：T 比 C 旧时返回 0（与 upgrade.sh 中的同名函数一致）。去掉开头的 v 再比；
# 同一版本号时正式版比预发布版新，两个都是预发布版时按 sort -V 比。任一个为空或 unknown 时返回 1
version_older() {
    local t="${1#v}" c="${2#v}"
    case "$t" in ""|unknown) return 1 ;; esac
    case "$c" in ""|unknown) return 1 ;; esac
    [ "$t" = "$c" ] && return 1
    if [ "${t%%-*}" = "${c%%-*}" ]; then
        [ "$t" != "${t%%-*}" ] || return 1
        [ "$c" = "${c%%-*}" ] && return 0
        version_lt "$t" "$c"
    else
        version_lt "${t%%-*}" "${c%%-*}"
    fi
}

# 数据目录里记下这份数据库最后是哪个版本的程序迁移过的（安装、升级成功后写；卸载保留数据时也写）。
# 程序已经卸掉、数据还在时，check_install_downgrade 靠它判断要装的版本是不是比数据旧
INSTALLED_VERSION_FILE="/var/lib/acornfox/installed-version"

# record_installed_version：把 /usr/local/bin/acornfox 的版本号写进 INSTALLED_VERSION_FILE（先写临时文件再换上）。
# 读不出版本号或写不进去时只提示，不影响安装
record_installed_version() {
    local v
    v=$({ /usr/local/bin/acornfox version 2>/dev/null || true; } | awk '{print $2}')
    [ -n "$v" ] || return 0
    if echo "$v" > "${INSTALLED_VERSION_FILE}.tmp" && mv -f "${INSTALLED_VERSION_FILE}.tmp" "$INSTALLED_VERSION_FILE"; then
        chmod 644 "$INSTALLED_VERSION_FILE" || true
    else
        log_warn "没能写入 ${INSTALLED_VERSION_FILE}（不影响使用）"
    fi
    return 0
}

# check_install_downgrade 要装的版本：这台服务器上已经装着 AcornFox，或者留着卸载时保留的数据，
# 而且比要装的新时停下。AcornFox 只能升级，不能降级（旧程序读不了新数据库）。在改动系统之前检查；
# 程序不在时按 INSTALLED_VERSION_FILE 判断；程序和这个文件都没有时不查（没装过，或者是很早的版本留下的数据）。
check_install_downgrade() {
    local t="$1" cur="" from=bin
    if [ -x /usr/local/bin/acornfox ]; then
        cur=$({ /usr/local/bin/acornfox version 2>/dev/null || true; } | awk '{print $2}')
    fi
    if [ -z "$cur" ] && [ -f "$INSTALLED_VERSION_FILE" ]; then
        cur=$(head -n 1 "$INSTALLED_VERSION_FILE" 2>/dev/null | tr -d '[:space:]' || true)
        from=data
    fi
    if [ -z "$cur" ]; then
        if [ -f /var/lib/acornfox/acornfox.db ]; then
            log_warn "这台服务器上留着以前的 AcornFox 数据（/var/lib/acornfox），但没有记下是哪个版本的；要装的版本比它旧时，后面初始化数据库会失败"
        fi
        return 0
    fi
    version_older "$t" "$cur" || return 0
    if [ "$from" = bin ]; then
        fault_exit INSTALL-DOWN-01 "这台服务器上已经装着 AcornFox ${cur}，要装的 ${t} 比它旧。AcornFox 只能升级，不能降级。什么都没改。" \
            "要换到更新的版本，用升级命令（upgrade.sh），不用重新安装。" \
            "确实要装旧版本，只能先卸载并连数据一起删除（uninstall.sh --purge，应用和数据都会删掉、不可恢复）再装。"
    fi
    fault_exit INSTALL-DOWN-01 "这台服务器上留着 AcornFox ${cur} 的数据（卸载时保留的 /var/lib/acornfox），要装的 ${t} 比它旧，旧程序读不了新数据库。AcornFox 只能升级，不能降级。什么都没改。" \
        "要接着用这份数据：装 ${cur} 或更新的版本（去掉 --version 就是装最新版）。" \
        "确实要装旧版本，只能先删掉这份数据（uninstall.sh --purge，应用和数据都会删掉、不可恢复）再装。"
}

# follow_release_script 脚本名 版本 参数...：版本早于 SCRIPT_MIN_VERSION 时下载该版本的同名脚本并交给它执行
follow_release_script() {
    local name="$1" v="$2" s
    shift 2
    [ -n "${ACORNFOX_SCRIPT_FOLLOWED:-}" ] && return 0
    version_lt "$v" "$SCRIPT_MIN_VERSION" || return 0
    log_warn "要装的 ${v} 早于本脚本支持的 ${SCRIPT_MIN_VERSION}，改用 ${v} 发行版自带的 ${name}"
    # 老版本脚本不认识的新参数不往下传（它们只影响本脚本自己的检查）
    local args=()
    while [ $# -gt 0 ]; do
        case "$1" in
            -y|--yes|--force-os|--allow-busy-80-443) shift ;;
            --cloud) shift; [ $# -gt 0 ] && shift ;;
            *) args+=("$1"); shift ;;
        esac
    done
    s=$(mktemp)
    release_download "$v" "$name" "$s" || error_exit "下载 ${v} 的 ${name} 失败；可以直接运行该版本发行版里的 ${name}"
    # 老版本脚本里写死的是改名前的旧仓库名；把现在的仓库名传给它
    export ACORNFOX_REPO ACORNFOX_GITEE_REPO
    ACORNFOX_SCRIPT_FOLLOWED=1 exec bash "$s" ${args[@]+"${args[@]}"} --version "$v"
}

# latest_tag：最新版本号。中国大陆先查 Gitee 发行版 API（Gitee 的 releases/latest
# 页面不跳转到版本页，不能沿用 GitHub 的做法），再走 GitHub 的 releases/latest 跳转
latest_tag() {
    local tag
    if $IN_CHINA && [ -n "$ACORNFOX_GITEE_REPO" ]; then
        # 不用 Gitee 的 releases/latest：它不保证跳过预发布版。按时间倒序取列表，跳过带“-”的
        # 预发布标签（v0.4.0-rc.1 这类），和 GitHub 的 releases/latest 一样只认正式版
        tag=$(curl -fsS -m 15 "https://gitee.com/api/v5/repos/${ACORNFOX_GITEE_REPO}/releases?page=1&per_page=20&direction=desc" 2>/dev/null \
            | grep -o '"tag_name":"[^"]*"' | cut -d'"' -f4 | grep -v '[-]' | head -1 || true)
        if [ -n "$tag" ]; then
            echo "$tag"
            return 0
        fi
    fi
    github_latest_tag
}

# verify_sha256 FILE ASSET VERSION：用同一 Release 的 SHA256SUMS 校验下载文件，不一致则删除并中止。
# 能发现下载损坏和镜像代理被篡改的单个文件；SHA256SUMS 与二进制走同一通道，防不了整个发布源被替换。
verify_sha256() {
    local file="$1" asset="$2" version="$3" sums want got
    sums=$(mktemp)
    if ! release_download "$version" SHA256SUMS "$sums"; then
        rm -f "$sums" "$file"
        error_exit "下载 SHA256SUMS 失败，无法校验 ${asset}"
    fi
    want=$(awk -v a="$asset" '$2 == a || $2 == "*" a {print $1; exit}' "$sums")
    rm -f "$sums"
    if [ -z "$want" ]; then
        rm -f "$file"
        error_exit "SHA256SUMS 中没有 ${asset}"
    fi
    if command -v sha256sum >/dev/null 2>&1; then
        got=$(sha256sum "$file" | awk '{print $1}')
    else
        got=$(shasum -a 256 "$file" | awk '{print $1}')
    fi
    if [ "$got" != "$want" ]; then
        rm -f "$file"
        error_exit "${asset} SHA256 校验失败：期望 ${want}，实际 ${got}"
    fi
    log_success "SHA256 校验通过: ${asset}"
}

# github_latest_tag：通过 releases/latest 的跳转地址取最新版本号（不依赖 api.github.com）
github_latest_tag() {
    local p loc
    for p in "${GITHUB_PROXIES[@]}"; do
        loc=$(curl -fsSIL --connect-timeout 10 --max-time 30 -o /dev/null -w '%{url_effective}' \
            "${p}https://github.com/${ACORNFOX_REPO}/releases/latest" 2>/dev/null || true)
        case "$loc" in
            */releases/tag/*)
                echo "${loc##*/}"
                return 0
                ;;
        esac
    done
    return 1
}

# 检查 root 权限
check_root() {
    if [ "$EUID" -ne 0 ]; then
        fault_exit INSTALL-ROOT-01 "需要 root 权限" \
            "请用 sudo 运行：把刚才那条命令里的 | bash 改成 | sudo bash（参数照旧），例如:" \
            "  国内: curl -fsSL ${SCRIPT_URL_GITEE}/install.sh | sudo bash" \
            "  海外: curl -fsSL ${SCRIPT_URL_GITHUB}/install.sh | sudo bash"
    fi
}

# 检测操作系统：只支持 Ubuntu、Debian。国内云厂商的默认系统（Alibaba Cloud Linux、
# OpenCloudOS、TencentOS）和 CentOS/Rocky 等在任何改动之前就停下，提示重装系统。
SUPPORTED_OS_TEXT="Ubuntu 22.04/24.04 或 Debian 12/13"
# 重装系统时推荐的版本（发布前完整实测过的）
RECOMMENDED_OS_TEXT="Ubuntu 24.04 或 Debian 13"
detect_os() {
    if [ -f /etc/os-release ]; then
        # shellcheck disable=SC1091
        . /etc/os-release
        OS=${ID:-}
        OS_VERSION=${VERSION_ID:-}
        OS_CODENAME=${VERSION_CODENAME:-}
        OS_PRETTY=${PRETTY_NAME:-$OS $OS_VERSION}
    else
        fault_exit INSTALL-OS-01 "无法识别操作系统（没有 /etc/os-release）" \
            "AcornFox 只支持 ${SUPPORTED_OS_TEXT}，请在云厂商控制台把系统重装成其中之一后再安装"
    fi

    log_info "检测到操作系统: ${OS_PRETTY}"

    local supported=false
    case "$OS" in
        ubuntu) [[ "$OS_VERSION" =~ ^(22\.04|24\.04)$ ]] && supported=true ;;
        debian) [[ "$OS_VERSION" =~ ^(12|13)$ ]] && supported=true ;;
        *)
            local hint="这个系统不能安装 AcornFox"
            case "$OS" in
                alinux|aliyun) hint="Alibaba Cloud Linux（阿里云默认系统）不能安装 AcornFox" ;;
                opencloudos|tencentos|tlinux) hint="${OS_PRETTY}（腾讯云默认系统）不能安装 AcornFox" ;;
                veLinux|velinux) hint="veLinux（火山引擎默认系统）不能安装 AcornFox" ;;
                centos|rocky|almalinux|rhel|fedora|anolis|openeuler|kylin|uos) hint="${OS_PRETTY} 不能安装 AcornFox" ;;
            esac
            # Linux Mint、Pop!_OS、deepin 等衍生版的 ID 不是 ubuntu/debian，软件源和代号也不同，同样不支持
            case " ${ID_LIKE:-} " in
                *" ubuntu "*|*" debian "*) hint="${OS_PRETTY} 是 Ubuntu/Debian 的衍生版，不能安装 AcornFox" ;;
            esac
            fault_exit INSTALL-OS-01 "${hint}：目前只支持 ${SUPPORTED_OS_TEXT}" \
                "云服务器：在云厂商控制台“重装系统”（或“更换操作系统”），选择 ${RECOMMENDED_OS_TEXT}，然后重新运行这条安装命令。" \
                "自己的电脑或机房服务器：重新安装 ${RECOMMENDED_OS_TEXT}（Linux Mint、Pop!_OS、deepin 等衍生版不行，--force-os 也不管用）。" \
                "重装系统会清空系统盘，请先确认上面没有要保留的数据。"
            ;;
    esac
    if ! $supported; then
        if $FORCE_OS; then
            log_warn "${OS_PRETTY} 不在支持列表（${SUPPORTED_OS_TEXT}），按 --force-os 继续，可能遇到问题"
        else
            fault_exit INSTALL-OS-02 "${OS_PRETTY} 不在支持列表：目前只支持 ${SUPPORTED_OS_TEXT}" \
                "建议：把系统重装成 ${RECOMMENDED_OS_TEXT}（云服务器在云厂商控制台“重装系统”）。" \
                "一定要在这个版本上试，就加 --force-os 重新运行（自行承担风险）。"
        fi
    fi
    [ -n "$OS_CODENAME" ] || fault_exit INSTALL-OS-02 "无法取得系统代号（/etc/os-release 里没有 VERSION_CODENAME）" \
        "请重装成 ${SUPPORTED_OS_TEXT}"
}

# check_arch：只支持 x86_64、arm64
check_arch() {
    case "$(uname -m)" in
        x86_64|aarch64) ;;
        *) fault_exit INSTALL-ARCH-01 "不支持的 CPU 架构: $(uname -m)（只支持 x86_64 和 arm64）" ;;
    esac
}

# check_disk：Docker 和应用镜像放在 /var/lib，剩余空间太少时装到一半会失败
check_disk() {
    local free_kb
    free_kb=$(df -Pk /var/lib 2>/dev/null | awk 'NR==2 {print $4}')
    [[ "$free_kb" =~ ^[0-9]+$ ]] || return 0
    if [ "$free_kb" -lt 2097152 ]; then
        fault_exit INSTALL-DISK-01 "/var/lib 所在磁盘只剩 $((free_kb / 1024)) MB，至少要 2 GB" \
            "清理磁盘（例如 apt-get clean、删除不用的文件）或扩容系统盘后重新运行"
    elif [ "$free_kb" -lt 10485760 ]; then
        log_warn "/var/lib 所在磁盘只剩 $((free_kb / 1024 / 1024)) GB，部署几个应用后可能不够用"
    fi
}

# ---- 云厂商识别（与 upgrade.sh 中的同名函数保持一致）----
# 结果: CLOUD=aliyun|tencent|huawei|volcengine|空  CLOUD_REGION=地域
#       CLOUD_MAINLAND=true|false|unknown（unknown：地域不在已知列表里，交给 IP 判断）
# 在云主机上（不论是否大陆地域）优先用该云的内网软件源；内网源连不上时退回公网源。
# ACORNFOX_CLOUD_OVERRIDE（install.sh 也可用 --cloud）只用于测试：跳过元数据探测，
# 直接当作该云的大陆地域；值为 none 表示当作不在任何云上。

# meta_get URL：读云厂商元数据（不是该云时 1～2 秒内失败）
meta_get() {
    curl -fs --connect-timeout 1 -m 2 "$1" 2>/dev/null | head -c 20000 || true
}

# region_like 值：像不像地域名（元数据接口出错时可能返回 HTML）
region_like() {
    [[ "$1" =~ ^[a-z]{2,}(-[a-z0-9]+)+$ ]]
}

detect_cloud() {
    CLOUD="" CLOUD_REGION="" CLOUD_MAINLAND=unknown
    local r j
    if [ -n "${ACORNFOX_CLOUD_OVERRIDE:-}" ]; then
        case "$ACORNFOX_CLOUD_OVERRIDE" in
            aliyun|tencent|huawei|volcengine)
                CLOUD="$ACORNFOX_CLOUD_OVERRIDE" CLOUD_REGION=override CLOUD_MAINLAND=true ;;
            none) CLOUD_MAINLAND=unknown ;;
            *) fault_exit INSTALL-CLOUD-01 "ACORNFOX_CLOUD_OVERRIDE / --cloud 的值不认识: ${ACORNFOX_CLOUD_OVERRIDE}" \
                "可用的值: aliyun、tencent、huawei、volcengine、none（只用于测试）" ;;
        esac
        return 0
    fi

    # 阿里云：region-id 形如 cn-hangzhou；cn-hongkong 不是大陆
    r=$(meta_get http://100.100.100.200/latest/meta-data/region-id)
    if region_like "$r"; then
        CLOUD=aliyun CLOUD_REGION="$r"
        case "$r" in
            cn-hongkong) CLOUD_MAINLAND=false ;;
            cn-*) CLOUD_MAINLAND=true ;;
            *) CLOUD_MAINLAND=false ;;
        esac
        return 0
    fi

    # 腾讯云：placement/region 形如 ap-guangzhou；ap-hongkong、ap-singapore 等是境外
    r=$(meta_get http://metadata.tencentyun.com/latest/meta-data/placement/region)
    if region_like "$r"; then
        CLOUD=tencent CLOUD_REGION="$r"
        case "$r" in
            ap-beijing*|ap-shanghai*|ap-guangzhou*|ap-shenzhen*|ap-chengdu*|ap-chongqing*|ap-nanjing*|\
            ap-tianjin*|ap-wuhan*|ap-hangzhou*|ap-changsha*|ap-fuzhou*|ap-jinan*|ap-hefei*|ap-shijiazhuang*|\
            ap-xian*|ap-zhengzhou*|ap-qingyuan*|ap-shenyang*|ap-harbin*|ap-taiyuan*|ap-kunming*|ap-guiyang*|\
            ap-nanning*|ap-nanchang*|ap-xibei*|ap-huhehaote*|ap-lanzhou*|ap-urumqi*|ap-changchun*|ap-qingdao*|\
            ap-xiamen*|ap-ningbo*|ap-suzhou*|ap-dongguan*|ap-foshan*)
                CLOUD_MAINLAND=true ;;
            ap-hongkong*|ap-taipei*|ap-singapore*|ap-bangkok*|ap-jakarta*|ap-seoul*|ap-tokyo*|ap-mumbai*|\
            ap-manila*|na-*|eu-*|sa-*|me-*|af-*)
                CLOUD_MAINLAND=false ;;
            *) CLOUD_MAINLAND=unknown ;;
        esac
        return 0
    fi

    # 火山引擎：region_id 形如 cn-shanghai；cn-hongkong、ap-southeast-1 是境外
    r=$(meta_get http://100.96.0.96/volcstack/latest/region_id)
    if region_like "$r"; then
        CLOUD=volcengine CLOUD_REGION="$r"
        case "$r" in
            cn-hongkong) CLOUD_MAINLAND=false ;;
            cn-*) CLOUD_MAINLAND=true ;;
            *) CLOUD_MAINLAND=false ;;
        esac
        return 0
    fi

    # 华为云：OpenStack 元数据里的 region_id（没有时取可用区去掉末尾字母），大陆地域以 cn- 开头
    j=$(meta_get http://169.254.169.254/openstack/latest/meta_data.json)
    if [ -n "$j" ]; then
        r=$(printf '%s' "$j" | grep -o '"region_id" *: *"[^"]*"' | head -1 | cut -d'"' -f4 || true)
        if [ -z "$r" ]; then
            r=$(printf '%s' "$j" | grep -o '"availability_zone" *: *"[^"]*"' | head -1 | cut -d'"' -f4 | sed 's/[a-z]$//' || true)
        fi
        if region_like "$r"; then
            CLOUD=huawei CLOUD_REGION="$r"
            case "$r" in
                cn-*) CLOUD_MAINLAND=true ;;
                *) CLOUD_MAINLAND=false ;;
            esac
            return 0
        fi
    fi
}

# ip_in_china：按公网 IP 判断是否在中国大陆（ip-api 偶尔要 2 秒以上才响应，放宽超时并试两次）
ip_in_china() {
    local _
    for _ in 1 2; do
        curl -s -m 5 "http://ip-api.com/json/?fields=countryCode" 2>/dev/null | grep -q '"countryCode":"CN"' && return 0
    done
    return 1
}

# detect_location：先认云厂商和地域，认不出再看 IP；--china / --global 最后覆盖
detect_location() {
    log_info "检测服务器所在网络..."
    IN_CHINA=false
    detect_cloud
    if [ -n "$CLOUD" ]; then
        log_info "检测到云厂商: ${CLOUD}（地域 ${CLOUD_REGION}）"
    fi
    case "$CLOUD_MAINLAND" in
        true)
            IN_CHINA=true
            log_info "中国大陆地域"
            ;;
        false)
            log_info "非中国大陆地域"
            ;;
        *)
            if ip_in_china; then
                IN_CHINA=true
                log_info "通过 IP 判断为中国大陆"
            else
                log_info "判断为非中国大陆环境"
            fi
            ;;
    esac
    if ${FORCE_CHINA:-false}; then
        IN_CHINA=true
        log_info "手动指定使用中国镜像源（--china）"
    elif ${FORCE_GLOBAL:-false}; then
        IN_CHINA=false
        log_info "手动指定使用国际源（--global）"
    fi
}

# cloud_apt_base：所在云的内网 APT 源（只有 http；包由发行版签名保证真实性）
cloud_apt_base() {
    case "${CLOUD:-}" in
        aliyun) echo "http://mirrors.cloud.aliyuncs.com" ;;
        tencent) echo "http://mirrors.tencentyun.com" ;;
        volcengine) echo "http://mirrors.ivolces.com" ;;
        *) echo "" ;;
    esac
}

# cloud_docker_source：所在云的内网 docker-ce 源，格式 "软件包地址|GPG 密钥地址"。
# 内网源只有 http，签名密钥从 https 取（各镜像站的 Docker 密钥是同一份）。
cloud_docker_source() {
    case "${CLOUD:-}" in
        aliyun) echo "http://mirrors.cloud.aliyuncs.com/docker-ce|https://mirrors.aliyun.com/docker-ce" ;;
        tencent) echo "http://mirrors.tencentyun.com/docker-ce|https://mirrors.cloud.tencent.com/docker-ce" ;;
        volcengine) echo "http://mirrors.ivolces.com/docker|https://mirrors.huaweicloud.com/docker-ce" ;;
        *) echo "" ;;
    esac
}

# url_ok URL：地址能不能取到（只取前 100 字节，3 秒连不上就算不通）
url_ok() {
    curl -fsS -o /dev/null --connect-timeout 3 -m 10 -r 0-99 "$1" 2>/dev/null
}

# custom_docker_source：用户用 ACORNFOX_DOCKER_CE_MIRROR 指定的 docker-ce 源，格式同上；没设或
# 格式不对时输出空（格式不对只警告，照默认顺序装）。地址是 http 时，签名密钥先试同一地址的
# https 版本（密钥不经 apt 缓存，走 https 更稳妥），取不到再用 http 地址本身；包的真实性由签名保证。
custom_docker_source() {
    local m="${ACORNFOX_DOCKER_CE_MIRROR:-}" k
    [ -n "$m" ] || return 0
    m="${m%/}"
    case "$m" in
        http://*|https://*) ;;
        *)
            log_warn "ACORNFOX_DOCKER_CE_MIRROR 要以 http:// 或 https:// 开头，已忽略: ${m}" >&2
            return 0
            ;;
    esac
    k="$m"
    if [ "${m#http://}" != "$m" ] && url_ok "https://${m#http://}/linux/${OS}/gpg"; then
        k="https://${m#http://}"
    fi
    log_info "ACORNFOX_DOCKER_CE_MIRROR 已设置，先试这个 Docker 源: ${m}" >&2
    echo "${m}|${k}"
}

# ---- 端口预检（与 upgrade.sh 中的同名函数保持一致）----
# AcornFox 用到的端口：
#   18800 控制台、18801 公网控制台网关、18808 Caddy 管理网关（都只听本机，被占服务就起不来）
#   18810-18899 应用端口（Caddy 容器用宿主网络直接监听；被占的端口一旦分给应用，Caddy 整份配置都加载不了）
#   80、443 绑定域名、HTTPS 和公网控制台时才用
#   18802 局域网控制台：局域网服务器全新安装时默认开，其余情况默认不开
# 自己的进程（acornfox、Caddy 容器 acornfox-caddy、v0.3.x 装在宿主机上的 Caddy）占着不算冲突。

# port_listeners：每行 "端口 进程名 PID"（只看 TCP 监听）
port_listeners() {
    ss -H -ltnp 2>/dev/null | awk '{
        n = split($4, a, ":"); port = a[n]; proc = "-"; pid = "-"
        if (match($0, /users:\(\("[^"]*",pid=[0-9]+/)) {
            s = substr($0, RSTART + 9, RLENGTH - 9)
            i = index(s, "\",pid=")
            proc = substr(s, 1, i - 1); pid = substr(s, i + 6)
        }
        print port, proc, pid
    }'
}

# port_is_ours 进程名 PID
port_is_ours() {
    local proc="$1" pid="$2" edge
    case "$proc" in
        acornfox) return 0 ;;
        caddy)
            edge=$(docker inspect -f '{{.Id}}' acornfox-caddy 2>/dev/null || true)
            if [ -n "$edge" ] && grep -q "$edge" "/proc/${pid}/cgroup" 2>/dev/null; then
                return 0
            fi
            # v0.3.x 装在宿主机上的 Caddy：安装和升级时会迁移进容器
            if [ -x /usr/local/bin/acornfox ] && [ "$(systemctl show -p MainPID --value caddy 2>/dev/null)" = "$pid" ]; then
                return 0
            fi
            ;;
    esac
    return 1
}

# port_owner_desc 端口 进程名 PID：给人看的占用者说明
port_owner_desc() {
    local port="$1" proc="$2" pid="$3" who="$2" name
    case "$proc" in
        docker-proxy)
            name=$(docker ps --format '{{.Names}} {{.Ports}}' 2>/dev/null \
                | awk -v p=":${port}->" 'index($0, p) {print $1; exit}' || true)
            who="Docker 容器 ${name:-（docker ps 查看）}"
            case "$name" in *1[Pp]anel*|*openresty*) who="${who}（1Panel）" ;; esac
            ;;
        nginx)
            if [ -d /www/server/nginx ]; then
                who="nginx（宝塔面板）"
            elif [ -d /opt/1panel ]; then
                who="nginx（1Panel）"
            fi
            ;;
        openresty) [ -d /opt/1panel ] && who="openresty（1Panel）" ;;
        BT-Panel|BT-Task|python*) [ -d /www/server/panel ] && who="${proc}（宝塔面板）" ;;
        -) who="未知程序" ;;
    esac
    if [ "$pid" = "-" ]; then
        echo "端口 ${port}: ${who}"
    else
        echo "端口 ${port}: ${who} (PID ${pid})"
    fi
}

# check_ports install|upgrade
check_ports() {
    local mode="$1" port proc pid line
    if ! command -v ss >/dev/null 2>&1; then
        log_warn "没有 ss 命令（iproute2），跳过端口检查"
        return 0
    fi
    log_info "检查端口占用..."
    local core=() web=() apps=() lan="" seen=" "
    while read -r port proc pid; do
        [[ "$port" =~ ^[0-9]+$ ]] || continue
        case "$seen" in *" $port "*) continue ;; esac
        port_is_ours "$proc" "$pid" && continue
        case "$port" in
            18800|18801|18808) core+=("$(port_owner_desc "$port" "$proc" "$pid")") ;;
            80|443) web+=("$(port_owner_desc "$port" "$proc" "$pid")") ;;
            18802) lan="$(port_owner_desc "$port" "$proc" "$pid")" ;;
            *) if [ "$port" -ge 18810 ] && [ "$port" -le 18899 ]; then
                   apps+=("$(port_owner_desc "$port" "$proc" "$pid")")
               else
                   continue
               fi ;;
        esac
        seen="${seen}${port} "
    done < <(port_listeners)

    if [ ${#core[@]} -gt 0 ]; then
        fault_exit INSTALL-PORT-01 "AcornFox 自己要用的本机端口被别的程序占用：" \
            "${core[@]}" \
            "18800（控制台）、18801（公网控制台网关）、18808（网关管理口）被占时 AcornFox 服务起不来，不能跳过。" \
            "处理办法：停掉或改掉占用这些端口的程序（看 PID 对应的服务：ps -fp PID），然后重新运行。"
    fi
    if [ ${#apps[@]} -gt 0 ]; then
        if [ "$mode" = upgrade ]; then
            log_warn "应用端口段 18810-18899 中有端口被别的程序占用（分到这些端口的应用会打不开）："
            for line in "${apps[@]}"; do log_warn "  ${line}"; done
        else
            fault_exit INSTALL-PORT-03 "应用端口段 18810-18899 中有端口被别的程序占用：" \
                "${apps[@]}" \
                "AcornFox 的网关直接监听这些端口；被占的端口一旦分给应用，网关整份配置都会加载失败，所有应用都打不开，所以不能跳过。" \
                "处理办法：把占用这些端口的程序改到别的端口或停掉，然后重新运行。"
        fi
    fi
    if [ ${#web[@]} -gt 0 ]; then
        if [ "$mode" = upgrade ]; then
            log_warn "80/443 被别的程序占用，绑定域名、HTTPS 和公网控制台用不了："
            for line in "${web[@]}"; do log_warn "  ${line}"; done
        elif [ -n "${PUBLIC_CONSOLE_DOMAIN:-}" ]; then
            fault_exit INSTALL-PORT-02 "要开通公网控制台（--public-console），但 80/443 被别的程序占用：" \
                "${web[@]}" \
                "公网控制台要用 80/443 申请 HTTPS 证书，被占时开不了。" \
                "处理办法：停掉占用 80/443 的程序后重新运行；或者先去掉 --public-console 并加 --allow-busy-80-443 安装。"
        elif ${ALLOW_BUSY_WEB:-false}; then
            log_warn "80/443 被别的程序占用，按 --allow-busy-80-443 继续："
            for line in "${web[@]}"; do log_warn "  ${line}"; done
            log_warn "应用照常用 http://服务器IP:端口 访问；在 80/443 空出来之前，不要在 AcornFox 里绑定域名或开公网控制台"
        else
            fault_exit INSTALL-PORT-02 "80/443 已被别的程序占用（常见于宝塔、1Panel 或自己装的 nginx）：" \
                "${web[@]}" \
                "AcornFox 绑定域名、申请 HTTPS 证书和开公网控制台时要用 80/443。" \
                "可以共存：应用通过 http://服务器IP:18810 起的端口访问，不需要 80/443。要共存就加 --allow-busy-80-443 重新运行，" \
                "  做法：把刚才那条安装命令末尾的 | sudo bash 改成 | sudo bash -s -- --allow-busy-80-443（原来已带 -s -- 参数的，把它加在最后），例如:" \
                "  国内: curl -fsSL ${SCRIPT_URL_GITEE}/install.sh | sudo bash -s -- --allow-busy-80-443" \
                "  海外: curl -fsSL ${SCRIPT_URL_GITHUB}/install.sh | sudo bash -s -- --allow-busy-80-443" \
                "  共存时域名请继续在原来的面板（宝塔、1Panel、nginx）里反向代理到 127.0.0.1:应用端口。" \
                "不需要原来的程序时，停掉它（例如 systemctl disable --now nginx）后重新运行。"
        fi
    fi
    if [ -n "$lan" ]; then
        log_warn "局域网控制台端口被占用（${lan}）；局域网控制台这次开不了，要用的话先让出 18802 再运行 acornfox lan-console enable"
    fi
    return 0
}

# 处理命令行参数
parse_args() {
    FORCE_CHINA=false
    FORCE_GLOBAL=false
    SKIP_DOCKER=false
    SKIP_CADDY=false
    ACORNFOX_VERSION="latest"
    LOCAL_BINARY=""
    REGISTRY_MIRRORS=()
    PUBLIC_HOST=""
    PUBLIC_CONSOLE_DOMAIN=""
    FORCE_OS=false
    ALLOW_BUSY_WEB=false
    # 局域网控制台：局域网服务器全新安装时默认打开；--no-lan-console 或 ACORNFOX_LAN_CONSOLE=0 不开
    LAN_CONSOLE=true
    case "${ACORNFOX_LAN_CONSOLE:-}" in 0|false|no|off) LAN_CONSOLE=false ;; esac

    while [[ $# -gt 0 ]]; do
        case $1 in
            --public-console)
                [[ $# -ge 2 ]] || error_exit "--public-console 需要一个域名，例如 panel.example.com"
                PUBLIC_CONSOLE_DOMAIN="$2"
                shift 2
                ;;
            --version)
                [[ $# -ge 2 ]] || error_exit "--version 需要一个版本号，例如 v0.2.0"
                ACORNFOX_VERSION="$2"
                shift 2
                ;;
            --binary)
                [[ $# -ge 2 ]] || error_exit "--binary 需要一个本地文件路径"
                LOCAL_BINARY="$2"
                shift 2
                ;;
            --public-host)
                [[ $# -ge 2 ]] || error_exit "--public-host 需要服务器的公网 IP 或域名"
                PUBLIC_HOST="$2"
                shift 2
                ;;
            --registry-mirror)
                [[ $# -ge 2 ]] || error_exit "--registry-mirror 需要一个地址，例如 https://xxxx.mirror.aliyuncs.com"
                REGISTRY_MIRRORS+=("$2")
                shift 2
                ;;
            --china)
                FORCE_CHINA=true
                shift
                ;;
            --global)
                FORCE_GLOBAL=true
                shift
                ;;
            --skip-docker)
                SKIP_DOCKER=true
                shift
                ;;
            --skip-caddy)
                SKIP_CADDY=true
                shift
                ;;
            -y|--yes)
                # 安装过程中不询问，-y 只是为了和 upgrade.sh 用法一致
                shift
                ;;
            --force-os)
                FORCE_OS=true
                shift
                ;;
            --no-lan-console)
                LAN_CONSOLE=false
                shift
                ;;
            --allow-busy-80-443)
                ALLOW_BUSY_WEB=true
                shift
                ;;
            --cloud)
                [[ $# -ge 2 ]] || error_exit "--cloud 需要一个值：aliyun、tencent、huawei、volcengine 或 none"
                ACORNFOX_CLOUD_OVERRIDE="$2"
                shift 2
                ;;
            -h|--help)
                show_help
                exit 0
                ;;
            *)
                error_exit "未知参数: $1\n使用 --help 查看帮助"
                ;;
        esac
    done
    # --china / --global 在 detect_location 最后生效
}

# 显示帮助
show_help() {
    cat << EOF
AcornFox 安装脚本（Linux 服务器：Ubuntu 22.04/24.04、Debian 12/13）

用法:
    curl -fsSL ${SCRIPT_URL_GITEE}/install.sh | sudo bash     （国内）
    curl -fsSL ${SCRIPT_URL_GITHUB}/install.sh | sudo bash   （海外）
    curl -fsSL <上面任一地址> | sudo bash -s -- [选项]
    sudo bash install.sh [选项]

安装过程中不会询问，运行即开始。开始改动系统之前先检查：系统版本、CPU 架构、
磁盘空间、端口占用；不满足时停下并打印“故障编号”（编号含义见脚本开头的注释）。

选项:
    --version V            安装指定版本（默认 latest）
    --binary PATH          使用本地 acornfox 二进制文件，不下载（用于测试候选版本）
    --public-host HOST     应用访问地址中的主机（公网 IP 或域名），默认自动检测
    --registry-mirror URL  Docker Hub 镜像加速地址，可重复指定
                           阿里云 ECS 建议使用控制台“容器镜像服务 → 镜像加速器”中的专属地址
    --china                强制使用中国镜像源
    --global               强制使用国际源
    --skip-docker          跳过 Docker 安装（如已安装）
    --skip-caddy           不制作本地 Caddy 镜像（acornfox 启动时从镜像仓库拉 caddy:2.8.4-alpine）
    --public-console 域名   开通公网访问控制台（默认不开通）：先在域名后台把该域名
                           解析到本服务器，并在安全组放行 80、443；安装最后自动验证
                           域名、申请 HTTPS 证书并生成登录密码。也可以以后再用
                           acornfox public-console enable 域名 开通
    --allow-busy-80-443    80/443 已被宝塔、1Panel、nginx 等占用时仍然安装（与它们共存）：
                           应用照常用 http://服务器IP:端口 访问，但不能在 AcornFox 里绑定域名、
                           开 HTTPS 和公网控制台。没有这个参数时 80/443 被占会停下（INSTALL-PORT-02）
    --no-lan-console       不默认打开局域网控制台。不加这个参数时：服务器在家里或办公室的局域网里
                           （地址是 192.168.x.x 这类私网地址，并且不是云主机）、又是第一次安装，
                           装完就在 18802 端口打开控制台，第一次打开时在浏览器里设密码；
                           云主机、公网服务器、重装和升级都不会自动打开。以后开关:
                           acornfox lan-console enable / disable
    --force-os             在不在支持列表里的 Ubuntu/Debian 版本上也继续安装（自行承担风险）；
                           对 CentOS、Alibaba Cloud Linux、OpenCloudOS 等其他系统无效
    --cloud 名称           只用于测试：把本机当作某个云厂商的大陆地域，跳过元数据探测；
                           名称为 aliyun、tencent、huawei、volcengine 或 none
    -y, --yes              可加可不加（安装不询问；与 upgrade.sh 用法一致）
    -h, --help             显示此帮助信息

软件源:
    在阿里云、腾讯云、火山引擎的服务器上（大陆和境外地域都算），APT 和 Docker 优先用
    该云的内网源（不走公网流量、更快）；内网源连不上时自动退回公网源。中国大陆其他
    环境用华为、腾讯等公网镜像站；腾讯云大陆地域的 Docker Hub 镜像加速也走内网。
    识别结果写在 /etc/acornfox/install.env（ACORNFOX_IN_CHINA、ACORNFOX_CLOUD）。

环境变量:
    ACORNFOX_GITHUB_PROXY     GitHub 下载加速前缀，如 https://ghfast.top/（直连失败时使用）
    ACORNFOX_REPO             发布仓库，默认 OrbitMaker/AcornFox
    ACORNFOX_GITEE_REPO       中国大陆优先使用的 Gitee 镜像，默认 VIP13390/AcornFox；设为空则不用
    ACORNFOX_CLOUD_OVERRIDE   同 --cloud，只用于测试
    ACORNFOX_LAN_CONSOLE      设为 0 同 --no-lan-console
    ACORNFOX_DOCKER_CE_MIRROR docker-ce 软件源地址（企业内网镜像，或用 apt 缓存代理时用 http
                              地址，如 http://mirrors.huaweicloud.com/docker-ce），排在所有
                              docker-ce 候选源最前面，先探测，用不了再换默认的源。
                              传法: curl … | sudo ACORNFOX_DOCKER_CE_MIRROR=地址 bash

示例:
    # 自动检测并安装
    sudo bash install.sh

    # 强制使用中国镜像源，并指定阿里云专属镜像加速
    sudo bash install.sh --china --registry-mirror https://xxxx.mirror.aliyuncs.com

    # 服务器上已有宝塔或 1Panel（占着 80/443），与它们共存
    curl -fsSL ${SCRIPT_URL_GITEE}/install.sh | sudo bash -s -- --allow-busy-80-443

    # 跳过已安装的 Docker
    sudo bash install.sh --skip-docker

EOF
}

# 更新软件包列表
# 指向境外官方地址的软件源。Debian 官方云镜像写的是 mirror+file:/etc/apt/mirrors/debian*.list
# （文件里是 deb.debian.org），也算官方源
APT_OFFICIAL_RE='//(archive|security|ports)\.ubuntu\.com|//(deb|security)\.debian\.org|mirror\+file:/*etc/apt/mirrors/debian'

update_apt() {
    log_info "更新软件包列表..."

    # v0.2.1 及更早版本在境外会添加 Caddy 的 cloudsmith 源，该源现在返回 402，留着会让
    # apt update 一直失败；重新安装时先移除
    if [ -f /etc/apt/sources.list.d/caddy-stable.list ] && grep -q 'dl.cloudsmith.io' /etc/apt/sources.list.d/caddy-stable.list; then
        rm -f /etc/apt/sources.list.d/caddy-stable.list
        log_info "已移除失效的 Caddy cloudsmith 软件源"
    fi

    # 只在软件源指向境外官方地址时才替换。云厂商镜像自带的内网源（如阿里云
    # mirrors.cloud.aliyuncs.com）走内网，冷启动 apt update 实测 17 秒，换成公网
    # mirrors.aliyun.com 要 251 秒，所以其他任何源都原样保留。
    # 候选顺序：所在云的内网源（大陆、境外地域都用）→ 中国大陆用华为公网镜像站
    # （2026-11 用全国家庭宽带节点实测，华为、腾讯最快，阿里公网镜像被限在约 0.3 MB/s）。
    # 每个候选先探测能不能连上，apt update 失败就换下一个，都不行就恢复原来的源。
    # 只看生效的源文件（*.list、*.sources），不看换源时留下的 *.bak
    if grep -qsE "$APT_OFFICIAL_RE" /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; then
        local candidates=() intranet base
        intranet=$(cloud_apt_base)
        [ -n "$intranet" ] && candidates+=("$intranet")
        $IN_CHINA && candidates+=("http://mirrors.huaweicloud.com")
        for base in ${candidates[@]+"${candidates[@]}"}; do
            if ! apt_mirror_ok "$base"; then
                log_warn "软件源 ${base} 连不上，换下一个"
                continue
            fi
            log_info "配置 APT 使用镜像 ${base}..."
            write_apt_sources "$base"
            if apt_get update -qq; then
                return 0
            fi
            log_warn "软件源 ${base} 更新失败，换下一个"
        done
        if [ -n "${APT_SOURCES_CHANGED:-}" ]; then
            restore_apt_sources
            log_warn "镜像源都不可用，恢复原来的软件源"
        fi
    elif $IN_CHINA || [ -n "${CLOUD:-}" ]; then
        log_info "APT 已使用非官方境外源（云厂商内网源或国内镜像），保持不变"
    fi

    apt_get update -qq || error_exit "更新软件包列表失败"
}

# apt_mirror_ok BASE：镜像站上有没有本系统的软件包索引
apt_mirror_ok() {
    local base="$1"
    case "$OS" in
        ubuntu) url_ok "${base}/$(ubuntu_dir)/dists/${OS_CODENAME}/Release" ;;
        debian) url_ok "${base}/debian/dists/${OS_CODENAME}/Release" ;;
        *) return 1 ;;
    esac
}

# ubuntu_dir：arm64 等架构的 Ubuntu 软件包在 ubuntu-ports 目录
ubuntu_dir() {
    case "$(dpkg --print-architecture)" in amd64|i386) echo ubuntu ;; *) echo ubuntu-ports ;; esac
}

# write_apt_sources BASE：把软件源换成 BASE，原文件第一次改动前备份为 *.bak
write_apt_sources() {
    local base="$1" f sec
    if [ -z "${APT_SOURCES_CHANGED:-}" ]; then
        cp /etc/apt/sources.list /etc/apt/sources.list.bak 2>/dev/null || true
        # Ubuntu 24.04 / Debian 12 的默认源在 deb822 格式的 *.sources 里，指向官方地址的
        # 一并停用，否则境外源仍会参与 update
        for f in /etc/apt/sources.list.d/*.sources; do
            [ -f "$f" ] || continue
            if grep -qE "$APT_OFFICIAL_RE" "$f"; then
                mv "$f" "$f.bak"
                log_info "已停用境外官方源 ${f}（备份为 $f.bak）"
            fi
        done
        APT_SOURCES_CHANGED=1
    fi
    case "$OS" in
        ubuntu)
            local dir
            dir=$(ubuntu_dir)
            cat > /etc/apt/sources.list << EOF
deb ${base}/${dir}/ ${OS_CODENAME} main restricted universe multiverse
deb ${base}/${dir}/ ${OS_CODENAME}-updates main restricted universe multiverse
deb ${base}/${dir}/ ${OS_CODENAME}-backports main restricted universe multiverse
deb ${base}/${dir}/ ${OS_CODENAME}-security main restricted universe multiverse
EOF
            ;;
        debian)
            # 安全更新单独探测：不是每个镜像站都有 debian-security
            sec="${base}/debian-security"
            if ! url_ok "${sec}/dists/${OS_CODENAME}-security/Release"; then
                if $IN_CHINA && url_ok "http://mirrors.huaweicloud.com/debian-security/dists/${OS_CODENAME}-security/Release"; then
                    sec="http://mirrors.huaweicloud.com/debian-security"
                else
                    sec="http://deb.debian.org/debian-security"
                fi
                log_info "安全更新改用 ${sec}"
            fi
            cat > /etc/apt/sources.list << EOF
deb ${base}/debian/ ${OS_CODENAME} main contrib non-free
deb ${base}/debian/ ${OS_CODENAME}-updates main contrib non-free
deb ${sec} ${OS_CODENAME}-security main contrib non-free
EOF
            ;;
    esac
}

# restore_apt_sources：换源失败时恢复原来的软件源
restore_apt_sources() {
    local f
    if [ -f /etc/apt/sources.list.bak ]; then
        mv -f /etc/apt/sources.list.bak /etc/apt/sources.list
    else
        rm -f /etc/apt/sources.list
    fi
    for f in /etc/apt/sources.list.d/*.sources.bak; do
        [ -f "$f" ] || continue
        mv -f "$f" "${f%.bak}"
    done
    APT_SOURCES_CHANGED=""
}

# 安装基础依赖
install_dependencies() {
    log_info "安装基础依赖..."
    apt_get install -y -qq \
        curl \
        wget \
        ca-certificates \
        gnupg \
        lsb-release \
        apt-transport-https \
        || error_exit "安装基础依赖失败"
}

# 安装 Docker
install_docker() {
    if $SKIP_DOCKER; then
        log_info "跳过 Docker 安装"
        return
    fi

    if command -v docker >/dev/null 2>&1; then
        DOCKER_VERSION=$(docker --version | awk '{print $3}' | tr -d ',')
        log_info "Docker 已安装: $DOCKER_VERSION"
        return
    fi

    log_info "安装 Docker..."

    # 国内镜像站在 Docker 新版本发布后会有一段时间不一致（索引大小不符、包 404、读超时），
    # 依次换源；全部失败时退回发行版仓库自带的 docker.io。
    # 每项是 "软件包地址|GPG 密钥地址"。云厂商内网源只有 http，签名密钥仍从 https 获取，
    # 包的真实性由签名保证。
    # 云主机（大陆、境外地域都算）先用所在云的内网源，连不上自动换下一个。
    local sources=() entry src gpg attempt installed=false intranet custom
    custom=$(custom_docker_source)
    [ -n "$custom" ] && sources+=("$custom")
    intranet=$(cloud_docker_source)
    [ -n "$intranet" ] && sources+=("$intranet")
    if $IN_CHINA; then
        # 公网镜像按 2026-11 家庭宽带实测排序：华为、腾讯最快，阿里被限速，南大和清华（教育网）放最后
        sources+=("https://mirrors.huaweicloud.com/docker-ce|https://mirrors.huaweicloud.com/docker-ce"
                  "https://mirrors.cloud.tencent.com/docker-ce|https://mirrors.cloud.tencent.com/docker-ce"
                  "https://mirrors.aliyun.com/docker-ce|https://mirrors.aliyun.com/docker-ce"
                  "https://mirrors.nju.edu.cn/docker-ce|https://mirrors.nju.edu.cn/docker-ce"
                  "https://mirrors.tuna.tsinghua.edu.cn/docker-ce|https://mirrors.tuna.tsinghua.edu.cn/docker-ce")
    else
        sources+=("https://download.docker.com|https://download.docker.com")
    fi

    for entry in "${sources[@]}"; do
        src="${entry%%|*}"
        gpg="${entry##*|}"
        # 先探测索引能不能取到：内网源在别的网络里连不上，不必等 apt 超时
        if ! url_ok "${src}/linux/${OS}/dists/${OS_CODENAME}/Release"; then
            log_warn "Docker 源 ${src} 连不上，换下一个源"
            continue
        fi
        log_info "使用 Docker 源: ${src}"
        if ! curl -fsSL --connect-timeout 10 --max-time 60 "${gpg}/linux/${OS}/gpg" \
            | gpg --batch --yes --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg; then
            log_warn "无法从 ${gpg} 获取 GPG 密钥，换下一个源"
            continue
        fi
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] ${src}/linux/${OS} ${OS_CODENAME} stable" > /etc/apt/sources.list.d/docker.list
        # 索引不一致（镜像站同步中）时 update 就会失败，重试同一个源也没用，直接换源；
        # update 成功但下载包失败多半是偶发，再试一次
        if ! apt_get update -qq; then
            log_warn "Docker 源 ${src} 索引不可用（镜像站可能正在同步），换下一个源"
            continue
        fi
        for attempt in 1 2; do
            if apt_get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin; then
                installed=true
                break 2
            fi
            log_warn "从 ${src} 安装 Docker 第 ${attempt} 次失败"
            sleep 3
        done
    done

    if ! $installed; then
        # 不留下失效的源，免得之后的 apt-get update 一直报错
        rm -f /etc/apt/sources.list.d/docker.list
        log_warn "Docker 官方源均不可用，改用发行版仓库的 docker.io"
        apt_get update -qq || true
        apt_get install -y -qq docker.io \
            || error_exit "Docker 安装失败：所有 Docker 源都不可用，请稍后重试，或先手动安装 Docker 后用 --skip-docker"
        # runner 通过 Docker API 构建，不依赖 buildx；Debian 11/12 仓库没有这个包，装不上不算失败
        apt_get install -y -qq docker-buildx >/dev/null 2>&1 || true
    fi

    # 启动 Docker
    systemctl enable docker
    systemctl start docker

    # 验证安装
    if docker --version >/dev/null 2>&1; then
        log_success "Docker 安装成功: $(docker --version)"
    else
        error_exit "Docker 安装验证失败"
    fi
}

# 配置 Docker 镜像加速
configure_docker_mirror() {
    log_info "配置 Docker 镜像加速..."

    if [ ${#REGISTRY_MIRRORS[@]} -eq 0 ]; then
        if ! $IN_CHINA; then
            log_info "非中国环境，跳过镜像加速配置"
            return
        fi
        # 公共加速器可用性经常变化；阿里云 ECS 用户应改用控制台里的专属地址。
        # 腾讯云内网加速只在腾讯云服务器上能用（实测 34 MB/s），所以只在腾讯云上放第一位。
        # 先探测：连不上（不在腾讯云内网）时不放进去，免得每次拉镜像先等它超时
        local code=""
        if [ "${CLOUD:-}" = tencent ]; then
            code=$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 3 -m 8 https://mirror.ccs.tencentyun.com/v2/ 2>/dev/null || true)
        fi
        if [ -n "$code" ] && [ "$code" != 000 ]; then
            REGISTRY_MIRRORS=("https://mirror.ccs.tencentyun.com" "https://docker.m.daocloud.io" "https://docker.1panel.live")
        else
            if [ "${CLOUD:-}" = tencent ]; then
                log_warn "腾讯云内网镜像加速 mirror.ccs.tencentyun.com 连不上，只用公网加速"
            fi
            REGISTRY_MIRRORS=("https://docker.m.daocloud.io" "https://docker.1panel.live")
        fi
    fi

    # 已有 daemon.json 时不覆盖，避免破坏用户已有的 Docker 配置
    if [ -s /etc/docker/daemon.json ]; then
        if grep -q '"registry-mirrors"' /etc/docker/daemon.json; then
            log_info "/etc/docker/daemon.json 已配置 registry-mirrors，保持不变"
        else
            log_warn "/etc/docker/daemon.json 已存在，未自动修改；如需镜像加速，请手动加入："
            echo "    \"registry-mirrors\": [$(printf '"%s",' "${REGISTRY_MIRRORS[@]}" | sed 's/,$//')]"
        fi
        return
    fi

    mkdir -p /etc/docker
    cat > /etc/docker/daemon.json << EOF
{
  "registry-mirrors": [$(printf '"%s",' "${REGISTRY_MIRRORS[@]}" | sed 's/,$//')],
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
EOF

    systemctl daemon-reload
    systemctl restart docker

    log_success "Docker 镜像加速配置完成: ${REGISTRY_MIRRORS[*]}"
    if [ "${CLOUD:-}" = aliyun ]; then
        log_info "阿里云 ECS 建议改用专属加速地址：容器镜像服务控制台 → 镜像工具 → 镜像加速器"
    fi
}

# 安装 Caddy
install_caddy() {
    if $SKIP_CADDY; then
        log_info "跳过 Caddy 镜像准备（acornfox 会在首次启动时从镜像仓库拉取 caddy:2.8.4-alpine）"
        return
    fi
    CADDY_VERSION="2.8.4"

    # 三端架构（2026-11-01）：Caddy 改为容器，由 acornfox 管理（容器名 acornfox-caddy，宿主网络）。
    # 这里只把 Caddy 官方二进制做成本地镜像 acornfox/caddy:2.8.4（docker import，不依赖镜像仓库）。
    if docker image inspect "acornfox/caddy:${CADDY_VERSION}" >/dev/null 2>&1; then
        log_info "Caddy 镜像已存在: acornfox/caddy:${CADDY_VERSION}"
        return
    fi
    # v0.3.x 装在宿主机上的 Caddy 2.8.x 可以直接拿来做镜像，免去下载
    if [ -x /usr/bin/caddy ] && /usr/bin/caddy version 2>/dev/null | grep -q "^v${CADDY_VERSION}"; then
        build_caddy_image /usr/bin/caddy
        return
    fi

    log_info "下载 Caddy ${CADDY_VERSION}..."
    # 所有地区都下载 Caddy 官方二进制：官方 apt 仓库 dl.cloudsmith.io 2026-10 实测对匿名请求
    # 返回 402 Payment Required，大陆也不稳定且国内镜像站没有同步；中国大陆先从 Gitee 发行版取同一份安装包（随 AcornFox
    # 发行版附带），失败再走 GitHub 直连和代理。
    ARCH=$(dpkg --print-architecture)
    local caddy_tag="" name
    if $IN_CHINA && [ -n "$ACORNFOX_GITEE_REPO" ]; then
        caddy_tag="$ACORNFOX_VERSION"
        [ "$caddy_tag" = "latest" ] && caddy_tag=$(latest_tag 2>/dev/null || true)
    fi
    local official="https://github.com/caddyserver/caddy/releases/download/v${CADDY_VERSION}"
    local mirror="https://gitee.com/${ACORNFOX_GITEE_REPO}/releases/download/${caddy_tag}"
    name="caddy_${CADDY_VERSION}_linux_${ARCH}.tar.gz"
    if [ -n "$caddy_tag" ] && curl_get "${AFX_WORK}/${name}" "${mirror}/${name}" \
        --connect-timeout 10 --max-time 600 --speed-limit 51200 --speed-time 15; then
        log_info "已从 Gitee 镜像下载 ${name}"
    else
        log_info "从 GitHub 下载 ${name}..."
        github_download "${official}/${name}" "${AFX_WORK}/${name}" \
            || error_exit "下载 Caddy 失败；可设置 ACORNFOX_GITHUB_PROXY 指定可用的 GitHub 加速地址后重试"
    fi
    # 校验值：当前版本 Caddy 官方发布的 SHA-512 直接写在脚本里（来自官方 caddy_${CADDY_VERSION}_checksums.txt，
    # 与安装包的下载来源无关），不用联网取校验文件，国内也就不用连 GitHub。换 Caddy 版本时要一起更新。
    name="caddy_${CADDY_VERSION}_checksums.txt"
    case "${CADDY_VERSION}_${ARCH}" in
        2.8.4_amd64) printf '%s  caddy_2.8.4_linux_amd64.tar.gz\n' "b8bec15d14fb033562af9f207850027bcbaa1f891edc9efe00d38bf39e1bf9944f8b6b8eba041ddd4c171cd70c905174c704d705be2f23bc678fe1eaf37a2485" > "${AFX_WORK}/${name}" ;;
        2.8.4_arm64) printf '%s  caddy_2.8.4_linux_arm64.tar.gz\n' "5466234be3e988071cef937aedbdd94c15b6f75cf7307397e67c2641219ac9bfe2c2bb3b31fc05bc68d3b6398bbe50abfa16ccf3b127318ccac31115ad26507c" > "${AFX_WORK}/${name}" ;;
    esac
    # 没有写死的版本：校验文件只有几 KB，优先取 Caddy 官方的，与镜像上的安装包互相独立；取不到再用镜像副本
    if [ -s "${AFX_WORK}/${name}" ]; then
        :
    elif ! curl -fsSL --connect-timeout 10 --max-time 30 -o "${AFX_WORK}/${name}" "${official}/${name}"; then
        if [ -n "$caddy_tag" ] && curl -fsSL --connect-timeout 10 --max-time 60 -o "${AFX_WORK}/${name}" "${mirror}/${name}"; then
            log_warn "Caddy 官方校验文件取不到，改用 Gitee 镜像上的副本"
        else
            github_download "${official}/${name}" "${AFX_WORK}/${name}" || error_exit "下载 Caddy 校验文件失败"
        fi
    fi
    mv "${AFX_WORK}/caddy_${CADDY_VERSION}_linux_${ARCH}.tar.gz" "${AFX_WORK}/caddy.tar.gz"
    mv "${AFX_WORK}/caddy_${CADDY_VERSION}_checksums.txt" "${AFX_WORK}/caddy_checksums.txt"

    # 无论从哪里下载，都按 Caddy 官方发布的 SHA-512 校验
    CADDY_WANT=$(awk -v a="caddy_${CADDY_VERSION}_linux_${ARCH}.tar.gz" '$2 == a {print $1; exit}' "${AFX_WORK}/caddy_checksums.txt")
    rm -f "${AFX_WORK}/caddy_checksums.txt"
    if [ -z "$CADDY_WANT" ] || [ "$(sha512sum "${AFX_WORK}/caddy.tar.gz" | awk '{print $1}')" != "$CADDY_WANT" ]; then
        rm -f "${AFX_WORK}/caddy.tar.gz"
        error_exit "Caddy 安装包 SHA-512 校验失败"
    fi

    tar -xzf "${AFX_WORK}/caddy.tar.gz" -C "${AFX_WORK}" caddy
    build_caddy_image "${AFX_WORK}/caddy"
    rm -f "${AFX_WORK}/caddy.tar.gz" "${AFX_WORK}/caddy"
}

# build_caddy_image BIN：用 Caddy 二进制和本机 CA 证书做最小镜像 acornfox/caddy:2.8.4
# （没有 shell；acornfox 创建容器时写入 Caddyfile 并指定启动命令）
build_caddy_image() {
    local bin="$1" root
    root=$(mktemp -d)
    mkdir -p "$root/usr/bin" "$root/etc/ssl/certs" "$root/tmp" "$root/data" "$root/config"
    chmod 1777 "$root/tmp"
    install -m 0755 "$bin" "$root/usr/bin/caddy"
    cp /etc/ssl/certs/ca-certificates.crt "$root/etc/ssl/certs/ca-certificates.crt"
    if tar -C "$root" -c . | docker import \
        --change 'ENV XDG_CONFIG_HOME=/config' --change 'ENV XDG_DATA_HOME=/data' \
        --change 'CMD ["/usr/bin/caddy","run","--config","/etc/acornfox/Caddyfile","--adapter","caddyfile"]' \
        - "acornfox/caddy:${CADDY_VERSION}" >/dev/null; then
        log_success "Caddy 镜像已就绪: acornfox/caddy:${CADDY_VERSION}"
    else
        log_warn "制作 Caddy 镜像失败；acornfox 启动时会改从镜像仓库拉取 caddy:${CADDY_VERSION}-alpine"
    fi
    rm -rf "$root"
}

# migrate_host_caddy：把 v0.3.x 装在宿主机上的 Caddy 换成容器。证书和 ACME 账号复制到
# 数据卷 acornfox-caddy-data（宿主机上的原文件保留），停用并禁止 caddy.service 开机启动，
# 留下标记文件 /var/lib/acornfox/.caddy-host-migrated（升级脚本据此知道已经迁移过）。
migrate_host_caddy() {
    [ -f /etc/systemd/system/caddy.service ] || [ -f /lib/systemd/system/caddy.service ] || return 0
    systemctl is-enabled --quiet caddy 2>/dev/null || systemctl is-active --quiet caddy 2>/dev/null || return 0
    log_info "把宿主机上的 Caddy 迁移到容器..."
    local src=/var/lib/caddy/.local/share/caddy vol
    docker volume create acornfox-caddy-data >/dev/null
    vol=$(docker volume inspect -f '{{.Mountpoint}}' acornfox-caddy-data)
    if [ -d "$src" ]; then
        mkdir -p "$vol/caddy"
        cp -a "$src/." "$vol/caddy/"
        log_info "已复制证书和 ACME 账号到数据卷 acornfox-caddy-data（原文件保留在 $src）"
    fi
    systemctl disable --now caddy >/dev/null 2>&1 || true
    mkdir -p /var/lib/acornfox
    date -u +%FT%TZ > /var/lib/acornfox/.caddy-host-migrated
    log_success "宿主机 Caddy 已停用，之后由容器 acornfox-caddy 接管 80、443 和应用端口"
}

# wait_caddy_container：等 acornfox 建好 Caddy 容器、管理网关应答（没有令牌时返回 403）
wait_caddy_container() {
    local i code=""
    for i in $(seq 1 90); do
        code=$(curl -s -o /dev/null -w '%{http_code}' -m 2 http://127.0.0.1:18808/ 2>/dev/null || true)
        [ "$code" = "403" ] && break
        sleep 2
    done
    if [ "$code" = "403" ]; then
        log_success "Caddy 容器 acornfox-caddy 已运行"
    else
        log_warn "Caddy 容器还没就绪；查看: docker logs acornfox-caddy；journalctl -u acornfox-server -n 50"
    fi
}

# 创建系统账号和组
create_accounts() {
    log_info "创建系统账号和组..."

    # 创建 acornfox-ipc 组（用于 socket 通信）
    if ! getent group acornfox-ipc >/dev/null; then
        groupadd -r acornfox-ipc
        log_success "创建组: acornfox-ipc"
    fi

    # 创建 acornfox-users 组（用于 CLI 访问）
    if ! getent group acornfox-users >/dev/null; then
        groupadd -r acornfox-users
        log_success "创建组: acornfox-users"
    fi

    # 创建 acornfox 用户（运行 server）
    if ! id acornfox >/dev/null 2>&1; then
        useradd -r -s /bin/bash -d /var/lib/acornfox -m -G acornfox-ipc,acornfox-users acornfox
        log_success "创建用户: acornfox"
    fi

    # 创建 acornfox-exec 用户（运行 runner，需要 docker 组权限）
    if ! id acornfox-exec >/dev/null 2>&1; then
        useradd -r -s /bin/bash -d /var/lib/acornfox-exec -m -G docker,acornfox-ipc acornfox-exec
        log_success "创建用户: acornfox-exec"
    else
        # 确保 acornfox-exec 在 docker 组中
        usermod -aG docker acornfox-exec
    fi

    # 已存在的账号也确保在 acornfox-ipc 组中（重复安装、旧版本升级）
    usermod -aG acornfox-ipc acornfox
    usermod -aG acornfox-ipc acornfox-exec

}

# 下载 AcornFox 二进制文件
download_acornfox() {
    log_info "下载 AcornFox..."

    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64)
            ARCH="arm64"
            ;;
        *)
            error_exit "不支持的架构: $ARCH"
            ;;
    esac

    if [ -n "$LOCAL_BINARY" ]; then
        [ -f "$LOCAL_BINARY" ] || error_exit "本地二进制文件不存在: $LOCAL_BINARY"
        log_info "使用本地二进制文件: $LOCAL_BINARY"
        install -m 0755 "$LOCAL_BINARY" /usr/local/bin/acornfox.new
        mv -f /usr/local/bin/acornfox.new /usr/local/bin/acornfox
        log_success "AcornFox 安装成功: $(/usr/local/bin/acornfox version)"
        return
    fi

    # 从 GitHub Releases 下载
    # 格式: https://github.com/<repo>/releases/download/v0.2.0/acornfox_linux_amd64
    VERSION="$ACORNFOX_VERSION"

    if [ "$VERSION" = "latest" ]; then
        log_info "获取最新版本号..."
        VERSION=$(latest_tag) || error_exit "无法获取最新版本号；用 --version vX.Y.Z 指定版本，或用 --binary 安装本地文件"
        log_info "最新版本: $VERSION"
    fi

    log_info "下载 AcornFox ${VERSION}（acornfox_linux_${ARCH}）"

    if release_download "$VERSION" "acornfox_linux_${ARCH}" "${AFX_WORK}/acornfox"; then
        log_success "下载完成"
    else
        error_exit "下载 AcornFox 失败，请检查网络或版本号；也可设置 ACORNFOX_GITHUB_PROXY 或使用 --binary"
    fi
    verify_sha256 "${AFX_WORK}/acornfox" "acornfox_linux_${ARCH}" "$VERSION"

    # 安装到 /usr/local/bin
    install -m 0755 "${AFX_WORK}/acornfox" /usr/local/bin/acornfox
    rm -f "${AFX_WORK}/acornfox"

    # 验证安装
    INSTALLED_VERSION=$(/usr/local/bin/acornfox version 2>/dev/null | awk '{print $2}' || echo "unknown")
    if [ "$INSTALLED_VERSION" != "unknown" ]; then
        log_success "AcornFox 安装成功: $INSTALLED_VERSION"
    else
        error_exit "AcornFox 安装验证失败"
    fi
}

# 创建数据目录
create_directories() {
    log_info "创建数据目录..."

    # 数据目录：数据库只有 acornfox 可读；runner（acornfox-exec）只需穿过
    # /var/lib/acornfox 去读 uploads/ 中的上传包，所以组只给执行权限
    mkdir -p /var/lib/acornfox/uploads
    chown acornfox:acornfox-ipc /var/lib/acornfox
    chmod 0710 /var/lib/acornfox
    chown acornfox:acornfox-ipc /var/lib/acornfox/uploads
    chmod 2750 /var/lib/acornfox/uploads

    # /run 是 tmpfs，重启后会清空；用 tmpfiles.d 让 systemd 每次开机重建。
    # setgid 让 server、runner 各自创建的 socket 都归属 acornfox-ipc 组。
    # 其他用户只有穿过权限（o+x），能否连接由各 socket 自身决定：
    # api.sock 归 acornfox-users 组（CLI 用户）。Caddy 已改为容器，管理接口走
    # 127.0.0.1:18808 上带令牌的网关（令牌在 /var/lib/acornfox/caddy-admin.token）。
    cat > /etc/tmpfiles.d/acornfox.conf << 'EOF'
d /run/acornfox 2771 root acornfox-ipc -
EOF
    systemd-tmpfiles --create /etc/tmpfiles.d/acornfox.conf
    # 旧版本安装可能留下其他属主，统一修正
    chown root:acornfox-ipc /run/acornfox
    chmod 2771 /run/acornfox

    log_success "数据目录创建完成"
}

# 初始化数据库
initialize_database() {
    log_info "初始化数据库..."

    # 重复安装时服务还在运行、占着数据库锁，先停下（start_services 会再启动；应用容器不受影响）
    if systemctl is-active --quiet acornfox-server || systemctl is-active --quiet acornfox-runner; then
        log_info "重复安装：先停止正在运行的 acornfox-server、acornfox-runner（应用容器不停）"
        systemctl stop acornfox-server acornfox-runner || true
    fi

    # 使用 acornfox 用户初始化数据库
    su - acornfox -c "/usr/local/bin/acornfox init" || error_exit "数据库初始化失败"

    # 设置数据库文件权限（路径与 server 的 --data-dir 默认值一致）
    chmod 600 /var/lib/acornfox/acornfox.db

    log_success "数据库初始化完成"
}

# 生成管理员令牌
generate_admin_token() {
    log_info "生成管理员令牌..."

    # 生成令牌
    TOKEN=$(su - acornfox -c "/usr/local/bin/acornfox admin-token" 2>&1 | tail -n1)

    if [ -z "$TOKEN" ]; then
        error_exit "生成管理员令牌失败"
    fi

    # 保存令牌到文件（仅 root 可读）
    echo "$TOKEN" > /root/.acornfox-token
    chmod 600 /root/.acornfox-token

    log_success "管理员令牌已生成"
}

# 检测应用访问地址中的主机：优先云厂商元数据里的公网 IP，取不到再用本机首个地址。
# 元数据接口出错时会返回 HTML 页面，所以每个结果都要校验是 IPv4。
detect_public_host() {
    if [ -n "$PUBLIC_HOST" ]; then
        log_info "应用访问地址主机: ${PUBLIC_HOST}（手动指定）"
        return
    fi
    local url ip
    for url in \
        http://100.100.100.200/latest/meta-data/eipv4 \
        http://100.100.100.200/latest/meta-data/public-ipv4 \
        http://metadata.tencentyun.com/latest/meta-data/public-ipv4 \
        http://169.254.169.254/latest/meta-data/public-ipv4; do
        ip=$(curl -fs -m 2 "$url" 2>/dev/null || true)
        if [[ "$ip" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]; then
            PUBLIC_HOST="$ip"
            log_info "应用访问地址主机: ${PUBLIC_HOST}（云厂商元数据）"
            return
        fi
    done
    PUBLIC_HOST=$(hostname -I 2>/dev/null | awk '{print $1}')
    if [ -n "$PUBLIC_HOST" ]; then
        log_warn "未从云厂商元数据取到公网 IP，暂用本机地址 ${PUBLIC_HOST}；如不对，用 --public-host 重新安装或修改 acornfox-server 服务"
    else
        log_warn "无法确定服务器地址，部署输出的访问地址将不含主机名；可用 --public-host 指定"
    fi
}

# 创建 systemd 服务
create_systemd_services() {
    log_info "创建 systemd 服务..."

    # 获取用户 UID（修复 peer credentials 验证问题）
    ACORNFOX_UID=$(id -u acornfox)
    ACORNFOX_EXEC_UID=$(id -u acornfox-exec)

    log_info "配置 UID: acornfox=$ACORNFOX_UID, acornfox-exec=$ACORNFOX_EXEC_UID"

    # acornfox-server.service
    # 重要：添加 --runner-uid 参数，解决 peer credentials 验证问题
    cat > /etc/systemd/system/acornfox-server.service << EOF
[Unit]
Description=AcornFox Server
Documentation=https://github.com/OrbitMaker/AcornFox
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=acornfox
Group=acornfox
ExecStart=/usr/local/bin/acornfox server --runner-uid $ACORNFOX_EXEC_UID --listen-group acornfox-users${PUBLIC_HOST:+ --public-host $PUBLIC_HOST}
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=acornfox-server

# 安全加固
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/acornfox /run/acornfox

[Install]
WantedBy=multi-user.target
EOF

    # acornfox-runner.service
    # 重要：添加 --server-uid 参数，解决 peer credentials 验证问题
    cat > /etc/systemd/system/acornfox-runner.service << EOF
[Unit]
Description=AcornFox Runner
Documentation=https://github.com/OrbitMaker/AcornFox
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=acornfox-exec
Group=acornfox-exec
ExecStart=/usr/local/bin/acornfox runner --server-uid $ACORNFOX_UID
Restart=on-failure
RestartSec=5s
StandardOutput=journal
StandardError=journal
SyslogIdentifier=acornfox-runner

# 安全加固
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

    # 重新加载 systemd
    systemctl daemon-reload

    log_success "systemd 服务创建完成（已配置正确的 UID 参数）"
}

# 启动服务
start_services() {
    log_info "启动 AcornFox 服务..."

    # 启用并启动服务
    # 用 restart：重复安装时让已运行的服务换上新二进制和新组成员
    systemctl enable acornfox-server acornfox-runner
    systemctl restart acornfox-runner
    sleep 2
    systemctl restart acornfox-server

    # 等待服务启动
    sleep 3

    # 检查服务状态
    if systemctl is-active --quiet acornfox-server && systemctl is-active --quiet acornfox-runner; then
        log_success "AcornFox 服务启动成功"
    else
        log_error "服务启动失败，请检查日志:"
        echo "  journalctl -u acornfox-server -n 50"
        echo "  journalctl -u acornfox-runner -n 50"
        exit 1
    fi
}

# 开通公网访问控制台（--public-console）。失败不影响安装结果。
enable_public_console() {
    [ -n "$PUBLIC_CONSOLE_DOMAIN" ] || return 0
    log_info "开通公网访问控制台: ${PUBLIC_CONSOLE_DOMAIN}"
    log_warn "拿到登录密码就能管理整台服务器，请妥善保管"
    local i
    for i in $(seq 1 30); do
        [ -S /run/acornfox/api.sock ] && break
        sleep 1
    done
    # 在服务器本机验证：解析是否指向本机、80 端口能否取回验证令牌，然后申请证书、生成密码
    if /usr/local/bin/acornfox public-console enable "$PUBLIC_CONSOLE_DOMAIN" --local --wait 10m; then
        PUBLIC_CONSOLE_RESULT="已开通：https://${PUBLIC_CONSOLE_DOMAIN}"
    else
        PUBLIC_CONSOLE_RESULT="未开通（按上面的提示处理后执行 acornfox public-console enable ${PUBLIC_CONSOLE_DOMAIN} --local）"
        log_warn "公网访问控制台没有开通，安装本身不受影响"
    fi
}

# private_ipv4 地址：是 10.x、172.16～31.x、192.168.x 这三段私网地址时返回 0。
private_ipv4() {
    case "$1" in
        10.*.*.*|192.168.*.*) return 0 ;;
        172.1[6-9].*.*|172.2[0-9].*.*|172.3[0-1].*.*) return 0 ;;
    esac
    return 1
}

# 局域网服务器全新安装时默认打开局域网控制台（第一次打开时在浏览器里设密码）。
# 条件（都满足才开）：没有 --no-lan-console；这台机器以前没装过（FRESH_INSTALL）；没识别出云厂商；
# 应用访问地址是私网地址。服务端还会自己再判断一次是不是云主机（探测云元数据地址），是就拒绝
# （LAN-CONSOLE-01）；刚启动时还没探测完也会先拒绝，所以这里最多等 30 秒。
# 没开成不影响安装结果，以后可以运行 acornfox lan-console enable。重复运行不会出错（已开着就不变）。
enable_lan_console() {
    LAN_CONSOLE_RESULT=""
    $LAN_CONSOLE || return 0
    $FRESH_INSTALL || return 0
    [ -z "${CLOUD:-}" ] || return 0
    private_ipv4 "${PUBLIC_HOST:-}" || return 0
    log_info "这台服务器在局域网里，打开局域网控制台（端口 18802）..."
    local i out=""
    for i in $(seq 1 15); do
        if [ -S /run/acornfox/api.sock ]; then
            out=$(curl -s --max-time 5 --unix-socket /run/acornfox/api.sock -X POST http://acornfox/v1/lan-console 2>/dev/null || true)
            case "$out" in *'"enabled":true'*) break ;; esac
        fi
        sleep 2
    done
    case "$out" in
        *'"enabled":true'*)
            LAN_CONSOLE_RESULT="http://${PUBLIC_HOST}:18802"
            log_success "局域网控制台已打开: ${LAN_CONSOLE_RESULT}"
            ;;
        *)
            log_warn "局域网控制台没有打开（${out:-后台服务没有应答}）；安装本身不受影响，需要时运行 acornfox lan-console enable"
            ;;
    esac
}

# 显示安装摘要
show_summary() {
    # “下一步”分两种情况，和 AcornFox Skill 第 7 步的情况甲、乙一致：
    #   甲：AI 工作台（或用户自己）就登录在这台服务器上，直接登记本机 unix 套接字；
    #   乙：在自己的电脑上操作，装 CLI 后经 SSH 登记这台服务器。
    # 用 sudo 运行时 SUDO_USER 是登录用户，提示里直接写这个名字（不替用户加组，加组由他确认）。
    local login_user="${SUDO_USER:-}" ssh_user="root" step_a=""
    if [ -n "$login_user" ] && [ "$login_user" != root ]; then
        ssh_user="$login_user"
        step_a="     先做：你是普通用户 ${login_user}，把自己加进 acornfox-users 组（执行一次）:
        ${YELLOW}sudo usermod -aG acornfox-users ${login_user}${NC}
        新加的组在当前登录里还不生效：重新登录服务器后再用；或者这次登录里的每条 acornfox
        命令都包一层 ${YELLOW}sg acornfox-users -c \"…\"${NC}，例如
        ${YELLOW}sg acornfox-users -c \"acornfox target add local --url unix:///run/acornfox/api.sock\"${NC}
"
    fi
    cat << EOF

${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}
${GREEN}✓ AcornFox 安装完成！${NC}
${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}

${BLUE}服务状态:${NC}
  • acornfox-server: $(systemctl is-active acornfox-server)
  • acornfox-runner: $(systemctl is-active acornfox-runner)
  • Caddy 容器 acornfox-caddy: $(docker inspect -f '{{.State.Status}}' acornfox-caddy 2>/dev/null || echo 未创建)

${BLUE}管理员令牌:${NC}
$(cat /root/.acornfox-token)

${BLUE}云服务器安全组（重要）:${NC}
  应用通过 ${YELLOW}http://${PUBLIC_HOST:-服务器IP}:端口${NC} 访问，端口在 18810-18899 之间分配。
  请在云厂商控制台的安全组中放行入方向 TCP ${YELLOW}18810-18899${NC}；
  绑定域名并启用 HTTPS 还需要放行 ${YELLOW}80${NC} 和 ${YELLOW}443${NC}。

${BLUE}下一步（看你或 AI 工作台在哪台机器上操作，二选一）:${NC}
  甲. 就在这台服务器上（AI 工作台登录在这台服务器上，或者你直接在服务器上用）:
${step_a}     1. 登记本机（不用装 CLI，服务器上已经有 acornfox 命令）:
        ${YELLOW}acornfox target add local --url unix:///run/acornfox/api.sock${NC}
     2. 试部署一个示例网页（在新建的空目录里执行；部署自己的项目就在项目目录里 acornfox deploy）:
        ${YELLOW}mkdir -p ~/acornfox-hello && cd ~/acornfox-hello && acornfox deploy --image nginx:1.27-alpine --port 80 --app hello${NC}
     3. 在服务器上检查（端口是 deploy 输出的网址末尾的数字，第一个应用一般是 18810）:
        ${YELLOW}curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18810${NC}   # 200 就是好了

  乙. 在自己的电脑上操作（Mac 或另一台 Linux），通过 SSH 连这台服务器:
     1. 在电脑上安装 CLI（按电脑所在网络选一条，不需要 sudo）:
        国内: ${YELLOW}curl -fsSL ${SCRIPT_URL_GITEE}/install-cli.sh | bash${NC}
        海外: ${YELLOW}curl -fsSL ${SCRIPT_URL_GITHUB}/install-cli.sh | bash${NC}
     2. SSH 用户不是 root 时，在服务器上执行一次: ${YELLOW}sudo usermod -aG acornfox-users <SSH 用户名>${NC}
     3. 在电脑上登记这台服务器（第一次先用 ssh 登录一次确认指纹）:
        ${YELLOW}acornfox target add my-server --ssh ${ssh_user}@${PUBLIC_HOST:-服务器公网IP}${NC}
     4. 在项目目录里部署: ${YELLOW}acornfox deploy${NC}；打开网页控制台: ${YELLOW}acornfox open${NC}

  Windows 电脑：本版本没有 Windows 版命令行，电脑上什么都不用装。用 ssh 登录这台服务器，
     在服务器上按“甲”操作；AI 工作台在 Windows 上时，让它经 ssh 在服务器上运行 acornfox。

  用 AcornFox Skill 的 AI 工作台：接着按 Skill 的自检、登记服务器、部署三步做。
${LAN_CONSOLE_RESULT:+
${BLUE}局域网控制台（已默认打开）:${NC}
  用同一局域网里的电脑或手机的浏览器打开: ${YELLOW}${LAN_CONSOLE_RESULT}${NC}
  ${YELLOW}现在还没有密码：请现在就打开设一个（至少 8 位）。谁先打开谁设，别拖。${NC}
  登录时可以勾“记住这台设备 90 天”。只在可信的网络里用（局域网里不加密）。
  看状态: ${YELLOW}acornfox lan-console status${NC}   忘了密码: ${YELLOW}acornfox lan-console reset-password${NC}
  关闭: ${YELLOW}acornfox lan-console disable${NC}   （安装时不想开: 加 --no-lan-console）
}${PUBLIC_CONSOLE_RESULT:+
${BLUE}公网访问控制台:${NC}
  ${PUBLIC_CONSOLE_RESULT}
  关闭: ${YELLOW}acornfox public-console disable${NC}
}
${BLUE}文档:${NC}
  https://github.com/OrbitMaker/AcornFox/tree/main/docs

${BLUE}故障排查:${NC}
  查看日志: ${YELLOW}journalctl -u acornfox-server -f${NC}
  查看日志: ${YELLOW}journalctl -u acornfox-runner -f${NC}

${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}

EOF
}

# show_plan：开头说明将要做什么（安装不询问，运行即开始）
show_plan() {
    cat << EOF
将要做的事:
  1. 检查系统版本、CPU 架构、磁盘空间和端口占用（不满足就停下，不改动系统）
  2. 识别云厂商和网络，配置软件源（云主机优先用内网源）并安装 Docker
  3. 准备网关组件（Caddy 容器镜像），创建系统账号 acornfox、acornfox-exec 和数据目录 /var/lib/acornfox
  4. 安装 /usr/local/bin/acornfox，注册并启动 acornfox-server、acornfox-runner 两个服务
  5. 生成管理员令牌（保存在 /root/.acornfox-token）
  6. 服务器在局域网里（私网地址、不是云主机）并且是第一次安装时：打开局域网控制台（端口 18802，
     第一次打开时在浏览器里设密码）。不想开加 --no-lan-console；云主机不会开

EOF
}

# 主函数
main() {
    echo ""
    log_info "AcornFox 安装脚本"
    echo ""

    # --help 不需要 root，也不做环境探测
    local arg
    for arg in "$@"; do
        case "$arg" in
            -h|--help) show_help; exit 0 ;;
        esac
    done

    # 解析参数（只记下选项，不改系统）
    parse_args "$@"
    check_root
    # 以前没装过才算全新安装（重装、卸载后保留数据再装都不算）：只有全新安装才默认打开局域网控制台
    FRESH_INSTALL=true
    if [ -x /usr/local/bin/acornfox ] || [ -e /var/lib/acornfox/acornfox.db ] || [ -e "$INSTALLED_VERSION_FILE" ]; then
        FRESH_INSTALL=false
    fi
    show_plan

    # 开始改动系统之前的检查：不满足就带故障编号停下
    detect_os
    check_arch
    check_disk
    check_ports install

    # 检测网络环境（云厂商、是否中国大陆）
    detect_location
    setup_github_proxies
    if [ -z "$LOCAL_BINARY" ]; then
        if [ "$ACORNFOX_VERSION" = "latest" ]; then
            ACORNFOX_VERSION=$(latest_tag) || error_exit "无法获取最新版本号；用 --version vX.Y.Z 指定版本，或用 --binary 安装本地文件"
            log_info "最新版本: $ACORNFOX_VERSION"
        fi
        check_install_downgrade "$ACORNFOX_VERSION"
        follow_release_script install.sh "$ACORNFOX_VERSION" "$@"
    elif [ -f "$LOCAL_BINARY" ]; then
        check_install_downgrade "$({ "$LOCAL_BINARY" version 2>/dev/null || true; } | awk '{print $2}')"
    fi
    # 记下网络环境判断，upgrade.sh 直接沿用，不必再依赖外部 IP 查询
    mkdir -p /etc/acornfox
    {
        echo "ACORNFOX_IN_CHINA=${IN_CHINA}"
        # 所在云厂商（aliyun、tencent、huawei、volcengine 或空）：runner 据此优先用该云的内网镜像加速
        echo "ACORNFOX_CLOUD=${CLOUD:-}"
        echo "ACORNFOX_CLOUD_REGION=${CLOUD_REGION:-}"
    } > /etc/acornfox/install.env

    # 安装流程
    update_apt
    install_dependencies
    install_docker
    configure_docker_mirror
    install_caddy
    migrate_host_caddy
    create_accounts
    create_directories
    download_acornfox
    initialize_database
    record_installed_version
    generate_admin_token
    detect_public_host
    create_systemd_services
    start_services
    wait_caddy_container
    enable_public_console
    enable_lan_console

    # 显示摘要
    show_summary
}

# 执行主函数
main "$@"
