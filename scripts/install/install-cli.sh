#!/usr/bin/env bash
# AcornFox CLI 客户端安装脚本
# 支持: Linux、macOS（amd64/arm64）。本版本没有 Windows 版命令行：在 Windows（Git Bash、MSYS、
# Cygwin）上运行时只打印改用 SSH 在服务器上运行 acornfox 的说明，退出码 2，不下载、不改任何东西。
# 用法、参数和环境变量见 --help（show_help）。

set -euo pipefail

show_help() {
    cat << 'EOF'
AcornFox CLI 安装脚本：在你自己的 Linux 或 Mac 电脑上安装 acornfox 命令行（不安装服务端，不需要 sudo）。
本版本没有 Windows 版命令行：Windows 上请让 AI 工作台经 SSH 在服务器上运行 acornfox，例如
  ssh 用户@地址 'acornfox --json status'

用法:
  bash install-cli.sh [选项] [版本]
  curl -fsSL <脚本地址> | bash -s -- [选项] [版本]

  版本      要安装的发行版标签，例如 v0.4.0；不写就装最新版本。
            再次运行即升级或重装，原地覆盖，不会留下两份。

选项:
  --china   强制按中国大陆网络下载：先从 Gitee 发行版下载，失败再走 GitHub 和加速代理
  --global  强制按海外网络下载：从 GitHub 下载（设了 ACORNFOX_GITHUB_PROXY 也会用），
            不用 Gitee 和内置的国内加速代理（不写这两个时自动判断）
  -h, --help  显示本说明

装到哪里（依次选第一个可用的）:
  1. ACORNFOX_INSTALL_DIR 指定的目录
  2. PATH 里已有的 acornfox 所在目录（可写时，就地升级）
  3. /usr/local/bin（当前用户可写时）
  4. ~/.local/bin（不在 PATH 里时，结尾会提示要加的那一行）

环境变量:
  ACORNFOX_INSTALL_DIR   安装目录（默认见上）
  ACORNFOX_GITHUB_PROXY  GitHub 加速地址前缀；下载地址是“前缀 + 完整 GitHub 地址”。
                         测试时也可以指向自己搭的 http 服务，目录结构照
                         <前缀>/https://github.com/<仓库>/releases/download/<版本>/<文件>
  ACORNFOX_REPO          GitHub 仓库（默认 OrbitMaker/AcornFox）
  ACORNFOX_GITEE_REPO    Gitee 仓库（默认 VIP13390/AcornFox；设为空则不用 Gitee）

校验: 下载的程序按同一发行版的 SHA256SUMS 校验，不一致就删除并退出，已安装的旧版本不动。
退出码: 0 安装成功；1 失败（下载失败、版本不存在、校验不通过、新程序无法运行、目录不可写）；
        2 在 Windows 上运行（本版本没有 Windows 版命令行，什么都没装）。
EOF
}

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
error_exit() { log_error "$1"; exit 1; }

# Windows：本版本不发 Windows 版命令行，说明改用 SSH 在服务器上运行，退出码 2
windows_not_supported() {
    log_error "本版本没有 Windows 版命令行，这台电脑上什么都没装。"
    cat << 'EOF'

请让 AI 工作台经 SSH 在服务器上运行 acornfox（在服务器上安装 AcornFox 时已经装好了命令行），例如：

  ssh 用户@地址 'acornfox --json status'

把“用户@地址”换成你登录服务器用的账号和地址。部署你电脑上的项目时，先把项目文件夹传到服务器
（scp -r 或 git），再在服务器上那个目录里运行 acornfox deploy。Windows 本机运行即将支持。

EOF
    exit 2
}

# 检测操作系统和架构
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux)
            OS="linux"
            ;;
        darwin)
            OS="darwin"
            ;;
        mingw*|msys*|cygwin*)
            windows_not_supported
            ;;
        *)
            error_exit "不支持的操作系统: $OS"
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            error_exit "不支持的架构: $ARCH"
            ;;
    esac

    log_info "检测到平台: ${OS}/${ARCH}"
}

# 下载 CLI
download_cli() {
    VERSION="${1:-latest}"

    command -v curl >/dev/null 2>&1 || error_exit "需要 curl"
    setup_github_proxies

    if [ "$VERSION" = "latest" ]; then
        log_info "获取最新版本号..."
        VERSION=$(latest_tag) || error_exit "无法获取最新版本号；用 bash install-cli.sh vX.Y.Z 指定版本"
        log_info "最新版本: $VERSION"
    fi

    BINARY_NAME="acornfox"
    ASSET="acornfox_${OS}_${ARCH}"

    log_info "下载 AcornFox CLI ${VERSION}（${ASSET}）"

    TMP_DIR=$(mktemp -d)
    TMP_FILE="${TMP_DIR}/${BINARY_NAME}"
    local rc=0
    release_download "$VERSION" "$ASSET" "$TMP_FILE" || rc=$?
    if [ "$rc" = 22 ]; then
        error_exit "下载失败：所有下载源都返回 HTTP 错误（多半是版本 ${VERSION} 不存在或没有 ${ASSET}）；核对版本号，或不写版本装最新版"
    elif [ "$rc" != 0 ]; then
        error_exit "下载失败；可设置 ACORNFOX_GITHUB_PROXY 指定 GitHub 加速地址"
    fi
    verify_sha256 "$TMP_FILE" "$ASSET" "$VERSION"
    chmod +x "$TMP_FILE"
}

# 退出时删掉临时目录和临时文件（成功、失败都删）
TMP_DIR=""
SUMS_FILE=""
cleanup() {
    [ -n "$SUMS_FILE" ] && rm -f "$SUMS_FILE"
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR"
    return 0
}
trap cleanup EXIT

# GitHub 仓库与下载代理（与 install.sh 一致）
ACORNFOX_REPO="${ACORNFOX_REPO:-OrbitMaker/AcornFox}"
GITHUB_PROXIES=()

# detect_china：先查阿里云、腾讯云元数据，再问 ip-api（它偶尔要 2 秒以上才响应，放宽超时并试两次）
detect_china() {
    curl -fs -m 2 http://100.100.100.200/latest/meta-data/region-id 2>/dev/null | grep -q '^cn-' && return 0
    curl -fs -m 2 http://metadata.tencentyun.com/latest/meta-data/placement/region 2>/dev/null \
        | grep -qE '^ap-(beijing|shanghai|guangzhou|chengdu|chongqing|nanjing)' && return 0
    local i
    for i in 1 2; do
        curl -s -m 5 "http://ip-api.com/json/?fields=countryCode" 2>/dev/null | grep -q '"countryCode":"CN"' && return 0
    done
    return 1
}

setup_github_proxies() {
    GITHUB_PROXIES=("")
    if [ -n "${ACORNFOX_GITHUB_PROXY:-}" ]; then
        GITHUB_PROXIES+=("${ACORNFOX_GITHUB_PROXY%/}/")
    fi
    IN_CHINA=false
    case "$FORCE_NET" in
        china) IN_CHINA=true; log_info "按中国大陆网络下载（--china）" ;;
        global) log_info "按海外网络下载（--global）" ;;
        *) detect_china && IN_CHINA=true ;;
    esac
    if $IN_CHINA; then
        GITHUB_PROXIES+=("https://ghfast.top/" "https://gh-proxy.com/")
    fi
}

github_download() {
    local url="$1" out="$2" p rc only_http=true
    # 连续 15 秒低于 50KB/s 就放弃当前源换下一个：国内直连 GitHub 常只有十几 KB/s，
    # 不限速时要等满 --max-time 才会轮到代理。所有源都太慢时，最后不限速再试一次。
    # 所有源都是 HTTP 错误（curl 退出码 22，例如 404 版本不存在）时不再重试，返回 22。
    for p in "${GITHUB_PROXIES[@]}"; do
        if [ -n "$p" ]; then
            log_info "尝试通过 ${p} 下载"
        else
            log_info "从 GitHub 下载 ${url##*/}"
        fi
        rc=0
        curl -fL --progress-bar --connect-timeout 10 --max-time 600 --speed-limit 51200 --speed-time 15 -o "$out" "${p}${url}" || rc=$?
        [ "$rc" = 0 ] && return 0
        [ "$rc" = 22 ] || only_http=false
    done
    $only_http && return 22
    log_info "所有下载源都较慢，不限速重试一次"
    curl -fL --progress-bar --connect-timeout 10 --max-time 1800 --retry 2 -o "$out" "${GITHUB_PROXIES[0]}${url}"
}

# 国内镜像：Gitee 发行版与 GitHub Release 的文件相同（同一份 SHA256SUMS 校验）。
# 中国大陆服务器先从 Gitee 下载，失败再走 GitHub 直连和代理；ACORNFOX_GITEE_REPO 设为空可关闭。
ACORNFOX_GITEE_REPO="${ACORNFOX_GITEE_REPO-VIP13390/AcornFox}"

# release_download VERSION ASSET OUT：下载某个版本的 Release 文件
release_download() {
    local version="$1" asset="$2" out="$3"
    if $IN_CHINA && [ -n "$ACORNFOX_GITEE_REPO" ]; then
        log_info "从 Gitee 镜像下载 ${asset}"
        if curl -fL --progress-bar --connect-timeout 10 --max-time 600 --speed-limit 51200 --speed-time 15 -o "$out" \
            "https://gitee.com/${ACORNFOX_GITEE_REPO}/releases/download/${version}/${asset}"; then
            return 0
        fi
        log_warn "Gitee 镜像下载失败，改从 GitHub 下载"
    fi
    github_download "https://github.com/${ACORNFOX_REPO}/releases/download/${version}/${asset}" "$out"
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
    local file="$1" asset="$2" version="$3" want got
    SUMS_FILE=$(mktemp)
    if ! release_download "$version" SHA256SUMS "$SUMS_FILE"; then
        rm -f "$file"
        error_exit "下载 SHA256SUMS 失败，无法校验 ${asset}"
    fi
    want=$(awk -v a="$asset" '$2 == a || $2 == "*" a {print $1; exit}' "$SUMS_FILE")
    rm -f "$SUMS_FILE"
    SUMS_FILE=""
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

# 安装 CLI
install_cli() {
    # Linux/macOS：不使用 sudo。AI 工作台在后台执行时无法输入密码，Apple 芯片的 Mac 上
    # /usr/local/bin 默认也需要 sudo。依次选择：ACORNFOX_INSTALL_DIR、已安装位置（就地升级，
    # 避免新旧两份并存）、可写的 /usr/local/bin、~/.local/bin。
    local existing=""
    existing=$(command -v acornfox 2>/dev/null || true)
    if [ -n "${ACORNFOX_INSTALL_DIR:-}" ]; then
        INSTALL_DIR="$ACORNFOX_INSTALL_DIR"
    elif [ -n "$existing" ] && [ -w "$(dirname "$existing")" ]; then
        INSTALL_DIR=$(dirname "$existing")
    elif [ -w "/usr/local/bin" ]; then
        INSTALL_DIR="/usr/local/bin"
    else
        INSTALL_DIR="${HOME}/.local/bin"
    fi
    mkdir -p "$INSTALL_DIR" || error_exit "无法创建安装目录 ${INSTALL_DIR}；可用 ACORNFOX_INSTALL_DIR 指定可写目录"
    [ -w "$INSTALL_DIR" ] || error_exit "安装目录 ${INSTALL_DIR} 不可写；可用 ACORNFOX_INSTALL_DIR 指定可写目录"
    # 先确认新程序能运行再覆盖，运行不了时已安装的旧版本不动
    "$TMP_FILE" version >/dev/null 2>&1 || error_exit "新下载的程序无法运行（${TMP_FILE}），未覆盖已安装的版本；临时目录所在分区可能挂载为 noexec"
    mv "$TMP_FILE" "${INSTALL_DIR}/acornfox"
    # macOS 会给下载的文件加隔离属性，去掉以免首次运行被拦截
    if [ "$OS" = "darwin" ]; then
        xattr -d com.apple.quarantine "${INSTALL_DIR}/acornfox" 2>/dev/null || true
    fi
    log_success "已安装到: ${INSTALL_DIR}/acornfox"
}

# 验证安装
verify_installation() {
    local bin="${INSTALL_DIR}/acornfox"
    VERSION=$("$bin" version 2>/dev/null) || { log_error "安装验证失败: ${bin} 无法运行"; return 1; }
    log_success "安装成功！版本: ${VERSION}"
    local found
    found=$(command -v acornfox 2>/dev/null || true)
    if [ -z "$found" ]; then
        log_warn "${INSTALL_DIR} 不在 PATH 中。把下面这行加入 ~/.zshrc 或 ~/.bashrc 后重新打开终端，或直接用完整路径 ${bin}："
        echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    elif [ "$found" != "$bin" ]; then
        log_warn "PATH 中优先找到的是另一个 acornfox：${found}（$("$found" version 2>/dev/null || echo 未知版本)），请删除它或调整 PATH 顺序"
    fi
    return 0
}

# 显示使用说明
show_usage() {
    cat << 'EOF'

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✓ AcornFox CLI 安装完成！
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

下一步:

1. 配置服务器连接:
   acornfox target add my-server --ssh user@your-server

2. 部署你的第一个应用:
   cd your-project
   acornfox deploy

3. 安装 AI 工作台 Skill:
   acornfox skill install

文档: https://github.com/OrbitMaker/AcornFox/tree/main/docs

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

EOF
}

FORCE_NET=""

main() {
    local version="latest"
    while [ $# -gt 0 ]; do
        case "$1" in
            -h|--help) show_help; exit 0 ;;
            --china) FORCE_NET=china ;;
            --global) FORCE_NET=global ;;
            -*) show_help >&2; error_exit "未知选项: $1" ;;
            *) version="$1" ;;
        esac
        shift
    done

    echo ""
    log_info "AcornFox CLI 安装脚本"
    echo ""

    detect_platform
    download_cli "$version"
    install_cli

    if verify_installation; then
        show_usage
    else
        error_exit "安装失败"
    fi
}

main "$@"
