#!/usr/bin/env bash
# shellcheck disable=SC1111  # 中文提示里的全角引号是正文，不是 shell 引号
# AcornFox 升级脚本
# 用途: 升级已安装的 AcornFox 服务器端
#
# 用法（国内）: curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/upgrade.sh | sudo bash
# 用法（海外）: curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh | sudo bash
# 确认规则: 在终端里直接运行（sudo bash upgrade.sh）会问一次“确认升级”；通过管道运行
# （curl … | sudo bash，包括 AI 工作台代为执行）时没法问，运行这条命令就视为同意，
# 开头先打印将要做的事。-y 在两种方式下都跳过询问。已是最新版本时，管道运行直接退出
# 不重装，要重装同一版本加 -y。
#
# 只能升级，不能回到旧版本：要装的版本比当前旧时一律拒绝（加 -y 也一样），--rollback 也拒绝，
# 都在停服务之前，什么都不改。应用自己的版本回退（acornfox rollback 应用名）和这里无关，照常可用。
# 升级前自动备份程序和数据库到 /var/lib/acornfox/backups（保留最近 5 份）。数据库迁移失败、
# 新版本的服务起不来、或者升级中途意外出错时，脚本用这份备份自动恢复到升级前：程序和数据库
# 都换回原来的，等于没升上。
#
# 应用会不会断：升级不停应用容器。从 v0.3.x 升级时，网关 Caddy 从宿主机换到容器，应用网址
# 会断几秒（实测 2.5～3 秒；升级没成功、恢复到升级前时换回宿主机，也会断几秒）；v0.4.0 起的
# 版本之间升级一般不断，新版本换了网关镜像或设置、要重建网关容器时也会断几秒。
#
# 故障编号（与 install.sh 共用，脚本停下时打印“故障编号: XXX”）:
#   INSTALL-PORT-01  AcornFox 自己要用的本机端口（18800、18801、18808）被别的程序占用
#   INSTALL-CLOUD-01 ACORNFOX_CLOUD_OVERRIDE 的值不认识
#   UPGRADE-DOWN-01  要装的版本比当前旧（降级），不支持；没停服务、没改动任何东西
#   UPGRADE-ROLLBACK-01 用了 --rollback：不支持回到旧版本；没停服务、没改动任何东西
#   UPGRADE-BACKUP-01 停服务后备份程序或数据库失败，没有升级；已重新启动原来的服务
#   UPGRADE-FAIL-01  新版本的数据库迁移失败，已自动恢复到升级前
#   UPGRADE-FAIL-02  新版本的服务起不来，已自动恢复到升级前
#   UPGRADE-FAIL-03  升级中途意外出错（或被中断），已自动恢复到升级前
#   UPGRADE-RESTORE-01 恢复到升级前时文件没能换回去，或换回去之后服务还是起不来，要人看日志处理
#
# 兜底和恢复的过程另记一份到 /var/log/acornfox-upgrade.log（SSH 断开时只写在这里）。

set -euo pipefail

# 下载的新程序放在本次自己新建的目录里，退出时删掉。不用固定的 /tmp/acornfox.new：别的用户先建了
# 同名文件时，root 也写不进去（fs.protected_regular）。目录设成 0755，迁移时 acornfox 用户要能运行它。
AFX_WORK=$(mktemp -d /tmp/acornfox-upgrade.XXXXXX)
chmod 0755 "$AFX_WORK"
NEW_BIN="$AFX_WORK/acornfox.new"

# 升级进行到哪一步，退出时据此兜底（见 on_exit）：
#   空        还没停服务，或者已经处理完（成功，或已恢复到升级前）
#   stopped   服务已停、备份还没做完：意外退出时重新启动原来的服务
#   changing  备份已做完、新版本还没确认起来：意外退出时用备份恢复到升级前
UPGRADE_PHASE=""

# 兜底和恢复的输出另记一份到这个文件（只追加）。SSH 断开、输出接给的程序先退出时终端写不进去，
# 那时只写这个文件；事后用 sudo tail -n 40 /var/log/acornfox-upgrade.log 看自动处理的结果。
UPGRADE_LOG="/var/log/acornfox-upgrade.log"
UPGRADE_HUP=false
RESTORE_LOGGING=false
RESTORE_TEE_PID=""

# restore_log_start：开始兜底或恢复时调用一次（重复调用不再处理）。关掉 set -e、屏蔽 Ctrl+C、
# kill、SSH 断开和写管道失败的信号，保证恢复不会被打断、也不会因为一句提示写不出去就停下；
# 屏蔽的信号会传给之后启动的 cp、systemctl 等，它们也不会被打断。
# 输出：终端还写得进去时同时写终端和 UPGRADE_LOG（tee -p：一边写不进去不影响另一边）；
# 收到过 SSH 断开（HUP）或终端已写不进去时只写 UPGRADE_LOG。日志文件建不了时照常输出到终端。
restore_log_start() {
    set +e
    trap '' INT TERM HUP PIPE
    $RESTORE_LOGGING && return 0
    RESTORE_LOGGING=true
    if ( : >>"$UPGRADE_LOG" ) 2>/dev/null; then
        chmod 600 "$UPGRADE_LOG" 2>/dev/null
        if ! $UPGRADE_HUP && { echo ""; } 2>/dev/null && command -v tee >/dev/null 2>&1; then
            exec > >(tee -p -a "$UPGRADE_LOG") 2>&1
            RESTORE_TEE_PID=$!
        else
            exec >>"$UPGRADE_LOG" 2>&1
        fi
    fi
    echo "==== $(date '+%F %T') AcornFox 升级没完成，开始自动处理（这一步不响应 Ctrl+C，请等它结束）===="
}

# restore_log_finish：退出前等日志写完（tee 在后台写，脚本先退出时最后几行可能还没进文件）
restore_log_finish() {
    [ -n "$RESTORE_TEE_PID" ] || return 0
    exec >&- 2>&-
    wait "$RESTORE_TEE_PID" 2>/dev/null
    return 0
}

# on_exit：删临时目录；升级中途意外退出（set -e 碰到的错误、Ctrl+C、kill、SSH 断开、输出写不进去）
# 时兜底，不让服务停在半路。明确处理过的失败（迁移失败、服务起不来）会先把 UPGRADE_PHASE 清空。
on_exit() {
    local code=$? phase="$UPGRADE_PHASE"
    # 第一句就关掉 set -e：陷阱里任何一条命令失败（例如往已断开的终端写提示）都不能让兜底停在半路
    set +e
    UPGRADE_PHASE=""
    # 先删临时目录：下面的 fault_exit 会直接退出
    rm -rf "$AFX_WORK"
    if [ "$code" != 0 ] && [ -n "$phase" ]; then
        restore_log_start
        case "$phase" in
            stopped)
                log_error "备份时中途出错或被中断（退出码 ${code}），没有升级，重新启动原来的服务..."
                systemctl start acornfox-runner
                sleep 2
                systemctl start acornfox-server
                fault_exit UPGRADE-BACKUP-01 "备份没做完，没有升级（程序和数据库都没改）" \
                    "原来的服务已重新启动：现在 acornfox-server $(systemctl is-active acornfox-server)，acornfox-runner $(systemctl is-active acornfox-runner)。" \
                    "上面几行是出错原因，常见的是磁盘满了（df -h /var/lib/acornfox）或备份目录 /var/lib/acornfox/backups 写不进去。处理后重新运行升级命令。"
                ;;
            changing)
                log_error "升级中途出错或被中断（退出码 ${code}），开始恢复到升级前..."
                restore_backup "$BACKUP_PATH"
                upgrade_failed UPGRADE-FAIL-03 "升级中途出错或被中断"
                ;;
        esac
    fi
}
trap on_exit EXIT
# Ctrl+C、kill、SSH 断开时也走 on_exit。输出接给的程序先退出（写管道失败）时也一样：
# 不设这一条，bash 会被 SIGPIPE 直接杀掉，EXIT 陷阱不跑
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'UPGRADE_HUP=true; exit 129' HUP
trap 'exit 141' PIPE

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

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
error_exit() { log_error "$1"; exit 1; }

# fault_exit 编号 说明 [处理办法...]：带故障编号停下
fault_exit() {
    local code="$1" msg="$2" line
    shift 2
    log_error "$msg"
    for line in "$@"; do
        echo "    $line"
    done
    echo "故障编号: ${code}"
    if $RESTORE_LOGGING && [ -f "$UPGRADE_LOG" ]; then
        echo "这段记录也写在 ${UPGRADE_LOG}（终端断开时只写在这里）"
    fi
    restore_log_finish
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

# 与 server --data-dir 默认值一致
DB_PATH="/var/lib/acornfox/acornfox.db"
# 升级备份目录：升级没成功时用本次的备份自动恢复到升级前。最近一次备份的路径记在备份目录下的
# .last（隐藏文件，不算进“保留 5 份”），结束提示里显示
BACKUP_DIR="/var/lib/acornfox/backups"
LAST_BACKUP_FILE="${BACKUP_DIR}/.last"
ASSUME_YES=false
TARGET_VERSION="latest"
LOCAL_BINARY=""
WANT_ROLLBACK=false

show_help() {
    cat << EOF
AcornFox 升级脚本（Linux 服务器）

用法:
    curl -fsSL ${SCRIPT_URL_GITEE}/upgrade.sh | sudo bash     （国内）
    curl -fsSL ${SCRIPT_URL_GITHUB}/upgrade.sh | sudo bash   （海外）
    curl -fsSL <上面任一地址> | sudo bash -s -- [选项]
    sudo bash upgrade.sh [选项]

用途:
    把这台服务器上的 AcornFox 服务端升级到新版本。

确认:
    在终端里直接运行时会问一次“确认升级”。通过管道运行（curl … | sudo bash，
    包括 AI 工作台代为执行）时没法询问，运行这条命令就视为同意，开头先打印将要做的事。
    已是最新版本时，管道运行直接退出不重装；要重装同一版本加 -y。

只能升级:
    AcornFox 只能升级，不能回到旧版本。--version 指定比当前旧的版本会被拒绝（加 -y 也一样），
    也没有 --rollback；这两种情况都在停服务之前停下，什么都不改。
    升级没成功时脚本会自动恢复到升级前（见下面“升级做的事”），不需要手动处理。
    应用自己的版本可以用 acornfox rollback 应用名 回退，和这里无关。

升级做的事（副作用）:
    下载并校验新版本 → 停 AcornFox 服务 → 备份程序和数据库到 /var/lib/acornfox/backups
    （保留最近 5 份）→ 数据库迁移 → 换上新程序 /usr/local/bin/acornfox → 启动服务。
    数据库迁移失败、新版本的服务起不来、或者中途出错时，用这份备份自动恢复到升级前：
    程序和数据库都换回原来的，等于没升上。运行中的应用容器不停。升级期间命令行和控制台
    暂时连不上。从 v0.3.x 升级时，网关 Caddy 从宿主机换到容器，应用网址会断几秒；
    v0.4.0 起的版本之间升级一般不断，要重建网关容器时也会断几秒。

例子:
    curl -fsSL ${SCRIPT_URL_GITEE}/upgrade.sh | sudo bash -s -- --version v0.5.0
        升级到指定版本（必须比现在的版本新）
    sudo bash upgrade.sh --binary ./acornfox -y
        用本地的程序升级，不下载（测试候选版本用）

选项:
    -y, --yes          不询问，直接升级（同一版本也重装）
    --version V        升级到指定版本（默认 latest）；比当前旧的版本会被拒绝
    --binary PATH      使用本地 acornfox 二进制文件，不下载（用于测试候选版本）
    -h, --help         显示此帮助信息

环境变量:
    ACORNFOX_GITHUB_PROXY     GitHub 下载加速前缀（直连失败时使用）
    ACORNFOX_REPO             发布仓库，默认 OrbitMaker/AcornFox
    ACORNFOX_GITEE_REPO       中国大陆优先使用的 Gitee 镜像，默认 VIP13390/AcornFox；设为空则不用
    ACORNFOX_CLOUD_OVERRIDE   只用于测试：没有 /etc/acornfox/install.env 时把本机当作某个云厂商
                              的大陆地域（aliyun、tencent、huawei、volcengine 或 none）

网络环境沿用安装时记在 /etc/acornfox/install.env 的判断；没有这个文件时按与 install.sh
相同的规则探测（先认云厂商和地域，认不出再看 IP）。
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -y|--yes) ASSUME_YES=true; shift ;;
            --rollback) WANT_ROLLBACK=true; shift ;;
            --version)
                [[ $# -ge 2 ]] || error_exit "--version 需要一个版本号"
                TARGET_VERSION="$2"; shift 2 ;;
            --binary)
                [[ $# -ge 2 ]] || error_exit "--binary 需要一个本地文件路径"
                LOCAL_BINARY="$2"; shift 2 ;;
            -h|--help) show_help; exit 0 ;;
            *) error_exit "未知参数: $1（可用：-y、--version V、--binary PATH、--help）" ;;
        esac
    done
}

# GitHub 仓库与下载代理（与 install.sh 一致）
ACORNFOX_REPO="${ACORNFOX_REPO:-OrbitMaker/AcornFox}"
GITHUB_PROXIES=()

# ---- 云厂商识别（与 install.sh 中的同名函数保持一致）----
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

setup_github_proxies() {
    GITHUB_PROXIES=("")
    if [ -n "${ACORNFOX_GITHUB_PROXY:-}" ]; then
        GITHUB_PROXIES+=("${ACORNFOX_GITHUB_PROXY%/}/")
    fi
    # 安装时已判断过网络环境并记在 install.env；旧版本安装没有这个文件时再探测
    # 只判断一次（main 和 download_new_version 都会调用本函数）
    if [ -z "${NETWORK_DECIDED:-}" ]; then
        IN_CHINA=false
        if [ -r /etc/acornfox/install.env ]; then
            grep -qx 'ACORNFOX_IN_CHINA=true' /etc/acornfox/install.env && IN_CHINA=true
        else
            detect_location
        fi
        NETWORK_DECIDED=1
    fi
    if $IN_CHINA; then
        GITHUB_PROXIES+=("https://ghfast.top/" "https://gh-proxy.com/")
    fi
}

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

# follow_release_script 脚本名 版本 参数...：版本早于 SCRIPT_MIN_VERSION 时下载该版本的同名脚本并交给它执行
follow_release_script() {
    local name="$1" v="$2" s
    shift 2
    [ -n "${ACORNFOX_SCRIPT_FOLLOWED:-}" ] && return 0
    version_lt "$v" "$SCRIPT_MIN_VERSION" || return 0
    log_warn "要装的 ${v} 早于本脚本支持的 ${SCRIPT_MIN_VERSION}，改用 ${v} 发行版自带的 ${name}"
    s=$(mktemp)
    release_download "$v" "$name" "$s" || error_exit "下载 ${v} 的 ${name} 失败；可以直接运行该版本发行版里的 ${name}"
    # 老版本脚本里写死的是改名前的旧仓库名；把现在的仓库名传给它
    export ACORNFOX_REPO ACORNFOX_GITEE_REPO
    ACORNFOX_SCRIPT_FOLLOWED=1 exec bash "$s" "$@" --version "$v"
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

# 通过管道运行（curl … | sudo bash）时标准输入是脚本本身，没法询问：
# 运行这条命令就视为同意（开头已打印将要做的事）。-y 在任何方式下都跳过询问。
PIPE_CONSENT=false

confirm() {
    $ASSUME_YES && return 0
    $PIPE_CONSENT && return 0
    read -p "$1 (y/N) " -n 1 -r
    echo
    [[ $REPLY =~ ^[Yy]$ ]]
}

# version_older T C：T 比 C 旧时返回 0。去掉开头的 v 再比；同一版本号时正式版比预发布版新
# （0.4.0 比 0.4.0-dev.3 新），两个都是预发布版时按 sort -V 比。任一个为空或 unknown 时
# 判断不了，返回 1（不算更旧）
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

# current_version_now：现在装着的程序的版本号；读不出来时输出“未知”
current_version_now() {
    local v
    v=$(/usr/local/bin/acornfox version 2>/dev/null | awk '{print $2}' || true)
    echo "${v:-未知}"
}

# refuse_rollback：只能升级，不支持 --rollback。在停服务、改动任何东西之前停下
refuse_rollback() {
    fault_exit UPGRADE-ROLLBACK-01 "AcornFox 只能升级，不能回到旧版本，没有 --rollback。服务没有停，什么都没改。" \
        "现在的版本: $(current_version_now)" \
        "升级没成功时（数据库迁移失败、新版本的服务起不来），脚本当时已经自动恢复到升级前，不需要再做什么。" \
        "升级后发现问题：等有更新的版本时重新运行升级命令；或者把问题和 acornfox doctor 的输出反馈给维护者。" \
        "应用自己的版本可以回退，不受影响：acornfox versions 应用名 看有哪些版本，acornfox rollback 应用名 回到上一版。"
}

# refuse_downgrade 目标版本：只能升级，要装的版本比当前旧时带故障编号停下（还没停服务、没改动任何东西）
refuse_downgrade() {
    local t="$1"
    rm -f "$NEW_BIN"
    fault_exit UPGRADE-DOWN-01 "当前是 ${CURRENT_VERSION}，要装的 ${t} 比它旧。AcornFox 只能升级，不能降级（加 -y 也一样）。服务没有停，什么都没改。" \
        "要升级到最新版：去掉 --version 重新运行升级命令。指定版本时，要写比 ${CURRENT_VERSION} 新的版本。" \
        "应用自己的版本可以回退，不受影响：acornfox rollback 应用名。"
}

# check_downgrade 新版本：在下载（或 --binary）之后、停服务之前，按程序自己报的版本号再查一次：
# 比当前旧就拒绝（--binary 只能在这里查）
check_downgrade() {
    if version_older "$1" "$CURRENT_VERSION"; then
        refuse_downgrade "$1"
    fi
    return 0
}

# pipe_version_gate 目标版本：管道运行、没带 -y 时，在下载和停服务之前先比版本号。
# 版本相同直接退出（不白停一次服务）。比当前旧的情况在 main 里已经处理。
pipe_version_gate() {
    local t="${1#v}" c="${CURRENT_VERSION#v}"
    case "$c" in ""|unknown) return 0 ;; esac
    if [ "$t" = "$c" ]; then
        log_success "已是最新版本 ${CURRENT_VERSION}，不用升级（要重装同一版本，加 -y 重新运行）"
        exit 0
    fi
    return 0
}

# show_plan：开头说明升级会做什么
show_plan() {
    cat << EOF
将要做的事:
  1. 下载并校验新版本（失败不影响正在运行的服务）
  2. 停止 acornfox-server、acornfox-runner（应用容器不停。从 v0.3.x 升级时网关 Caddy 从宿主机
     换到容器，应用网址会断几秒；v0.4.0 起的版本之间一般不断，要重建网关容器时也会断几秒）
  3. 备份程序和数据库到 /var/lib/acornfox/backups，执行数据库迁移，换上新程序
  4. 启动服务；数据库迁移失败或新版本的服务起不来时，自动恢复到升级前（等于没升上）
EOF
    if $PIPE_CONSENT; then
        echo "通过管道运行，没法询问，视为已同意升级（在终端里直接运行 sudo bash upgrade.sh 会先询问）。"
    fi
    echo
}

# refresh_install_env_cloud：旧版本安装没有记下云厂商（或当时还不认识这个云）时补上，
# 让 runner 用上该云的内网镜像加速；ACORNFOX_IN_CHINA 沿用安装时的判断，不改
refresh_install_env_cloud() {
    local f=/etc/acornfox/install.env
    [ -f "$f" ] || return 0
    grep -q '^ACORNFOX_CLOUD=..*' "$f" && return 0
    detect_cloud
    [ -n "$CLOUD" ] || return 0
    sed -i '/^ACORNFOX_CLOUD=/d; /^ACORNFOX_CLOUD_REGION=/d' "$f"
    {
        echo "ACORNFOX_CLOUD=${CLOUD}"
        echo "ACORNFOX_CLOUD_REGION=${CLOUD_REGION}"
    } >> "$f"
    log_info "已在 ${f} 记下云厂商: ${CLOUD}（地域 ${CLOUD_REGION}）"
}

# ---- 端口预检（与 install.sh 中的同名函数保持一致）----
# AcornFox 用到的端口：
#   18800 控制台、18801 公网控制台网关、18808 Caddy 管理网关（都只听本机，被占服务就起不来）
#   18810-18899 应用端口（Caddy 容器用宿主网络直接监听；被占的端口一旦分给应用，Caddy 整份配置都加载不了）
#   80、443 绑定域名、HTTPS 和公网控制台时才用
#   18802 局域网控制台，默认不开
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
        log_warn "局域网控制台端口被占用（${lan}）；这个功能默认不开，开启前要先让出 18802"
    fi
    return 0
}



# 检查 root 权限
check_root() {
    if [ "$EUID" -ne 0 ]; then
        error_exit "请使用 sudo 运行此脚本"
    fi
}

# 检查 AcornFox 是否已安装
check_installed() {
    if [ ! -f "/usr/local/bin/acornfox" ]; then
        error_exit "AcornFox 未安装，请先运行 install.sh"
    fi

    CURRENT_VERSION=$(/usr/local/bin/acornfox version 2>/dev/null | awk '{print $2}' || echo "unknown")
    log_info "当前版本: $CURRENT_VERSION"
}

# 备份当前安装
backup_current() {
    TIMESTAMP=$(date +%Y%m%d_%H%M%S)
    BACKUP_PATH="${BACKUP_DIR}/${TIMESTAMP}"

    log_info "创建备份..."
    mkdir -p "$BACKUP_PATH"

    # 备份失败时程序和数据库都还没动：由 on_exit（UPGRADE_PHASE=stopped）重新启动原来的服务
    # 备份二进制文件
    cp /usr/local/bin/acornfox "${BACKUP_PATH}/acornfox.bin" || error_exit "备份二进制文件失败"

    # 备份数据库（服务已停止，连同 WAL 文件一起复制才是一致的快照）
    [ -f "$DB_PATH" ] || error_exit "找不到数据库 ${DB_PATH}，拒绝在没有备份的情况下升级"
    local f
    for f in "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm"; do
        if [ -f "$f" ]; then
            cp -p "$f" "${BACKUP_PATH}/$(basename "$f")" || error_exit "备份数据库失败: $f"
        fi
    done
    log_success "数据库已备份"

    # 记录版本信息
    echo "$CURRENT_VERSION" > "${BACKUP_PATH}/version.txt"

    log_success "备份完成: $BACKUP_PATH"
    echo "$BACKUP_PATH" > "$LAST_BACKUP_FILE"
}

# last_backup：最近一次升级备份的目录；先看备份目录下的 .last，再看最新的备份目录。都没有时输出空
last_backup() {
    local b
    b=$(cat "$LAST_BACKUP_FILE" 2>/dev/null || true)
    if [ -n "$b" ] && [ -d "$b" ]; then
        echo "$b"
        return 0
    fi
    b=$(ls -1td "$BACKUP_DIR"/*/ 2>/dev/null | head -1 || true)
    [ -n "$b" ] && echo "${b%/}"
    return 0
}

# 下载新版本
download_new_version() {
    if [ -n "$LOCAL_BINARY" ]; then
        [ -f "$LOCAL_BINARY" ] || error_exit "本地二进制文件不存在: $LOCAL_BINARY"
        install -m 0755 "$LOCAL_BINARY" "$NEW_BIN"
        NEW_VERSION=$("$NEW_BIN" version 2>/dev/null | awk '{print $2}' || echo "unknown")
        log_info "使用本地二进制文件: ${LOCAL_BINARY}（${NEW_VERSION}）"
        check_downgrade "$NEW_VERSION"
        return
    fi

    log_info "下载新版本..."

    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64) ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
        *) error_exit "不支持的架构: $ARCH" ;;
    esac

    setup_github_proxies
    VERSION="$TARGET_VERSION"

    if [ "$VERSION" = "latest" ]; then
        log_info "获取最新版本号..."
        VERSION=$(latest_tag) || error_exit "无法获取最新版本号；用 --version vX.Y.Z 指定版本，或用 --binary 使用本地文件"
        log_info "最新版本: $VERSION"
    fi

    release_download "$VERSION" "acornfox_linux_${ARCH}" "$NEW_BIN" \
        || error_exit "下载失败；可设置 ACORNFOX_GITHUB_PROXY 或使用 --binary"
    verify_sha256 "$NEW_BIN" "acornfox_linux_${ARCH}" "$VERSION"
    chmod +x "$NEW_BIN"

    # 验证新版本
    NEW_VERSION=$("$NEW_BIN" version 2>/dev/null | awk '{print $2}' || echo "unknown")
    log_info "新版本: $NEW_VERSION"
    check_downgrade "$NEW_VERSION"

    if [ "$NEW_VERSION" = "$CURRENT_VERSION" ]; then
        if ! $ASSUME_YES && $PIPE_CONSENT; then
            # 管道里重复运行升级命令时不白停一次服务
            rm -f "$NEW_BIN"
            log_success "已是最新版本 ${CURRENT_VERSION}，不用升级（要重装同一版本，加 -y 重新运行）"
            exit 0
        fi
        log_warn "新版本与当前版本相同"
        if ! confirm "是否继续?"; then
            rm -f "$NEW_BIN"
            exit 0
        fi
    fi
}

# 停止服务
stop_services() {
    log_info "停止服务..."

    systemctl stop acornfox-server || log_warn "停止 acornfox-server 失败"
    systemctl stop acornfox-runner || log_warn "停止 acornfox-runner 失败"

    # 等待服务完全停止
    sleep 2

    log_success "服务已停止"
}

# 旧版本安装的目录权限会让 runner 读不到上传包、重启后丢失 /run/acornfox，
# 这里补成与 install.sh 一致的布局（幂等）
fix_layout() {
    log_info "校正目录与组权限..."
    getent group acornfox-ipc >/dev/null || groupadd -r acornfox-ipc
    usermod -aG acornfox-ipc acornfox
    usermod -aG acornfox-ipc acornfox-exec
    if id caddy >/dev/null 2>&1; then
        usermod -aG acornfox-ipc caddy
    fi

    mkdir -p /var/lib/acornfox/uploads
    chown acornfox:acornfox-ipc /var/lib/acornfox /var/lib/acornfox/uploads
    chmod 0710 /var/lib/acornfox
    chmod 2750 /var/lib/acornfox/uploads

    echo 'd /run/acornfox 2771 root acornfox-ipc -' > /etc/tmpfiles.d/acornfox.conf
    systemd-tmpfiles --create /etc/tmpfiles.d/acornfox.conf
    chown root:acornfox-ipc /run/acornfox
    chmod 2771 /run/acornfox

    # api.sock 交给 acornfox-users 组（CLI 用户），不再要求 SSH 用户加入 acornfox-ipc
    getent group acornfox-users >/dev/null || groupadd -r acornfox-users
    local unit=/etc/systemd/system/acornfox-server.service
    if [ -f "$unit" ] && ! grep -q -- '--listen-group' "$unit"; then
        sed -i 's|^\(ExecStart=/usr/local/bin/acornfox server .*\)$|\1 --listen-group acornfox-users|' "$unit"
        systemctl daemon-reload
    fi
}

# 执行数据库迁移
run_migrations() {
    log_info "执行数据库迁移..."

    # 使用新版本的 acornfox 执行迁移
    su -s /bin/sh acornfox -c "$NEW_BIN migrate --data-dir /var/lib/acornfox" || {
        log_error "数据库迁移失败"
        return 1
    }

    log_success "数据库迁移完成"
    return 0
}

# 替换二进制文件
replace_binary() {
    log_info "替换二进制文件..."

    mv "$NEW_BIN" /usr/local/bin/acornfox || error_exit "替换失败"
    chown root:root /usr/local/bin/acornfox
    chmod 755 /usr/local/bin/acornfox

    log_success "二进制文件已更新"
}

# 启动服务
start_services() {
    log_info "启动服务..."

    systemctl start acornfox-runner
    sleep 2
    systemctl start acornfox-server
    sleep 3

    # 检查服务状态
    if systemctl is-active --quiet acornfox-server && systemctl is-active --quiet acornfox-runner; then
        log_success "服务启动成功"
        return 0
    else
        log_error "服务启动失败"
        return 1
    fi
}

# 验证升级
verify_upgrade() {
    log_info "验证升级..."

    # 检查版本
    INSTALLED_VERSION=$(/usr/local/bin/acornfox version 2>/dev/null | awk '{print $2}' || echo "unknown")
    log_info "已安装版本: $INSTALLED_VERSION"

    # 检查服务状态
    if systemctl is-active --quiet acornfox-server && systemctl is-active --quiet acornfox-runner; then
        log_success "升级验证通过"
        return 0
    else
        log_error "服务运行异常"
        return 1
    fi
}

# ---- Caddy 改容器（三端架构，2026-11-01；与 install.sh 中同名函数一致）----
CADDY_VERSION="2.8.4"

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
# 留下标记文件 /var/lib/acornfox/.caddy-host-migrated；升级没成功、恢复到升级前时据此换回宿主机 Caddy。
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
    log_success "宿主机 Caddy 已停用（升级没成功时会换回来），之后由容器 acornfox-caddy 接管 80、443 和应用端口"
}

# wait_caddy_container：等 acornfox 建好 Caddy 容器、管理网关应答（没有令牌时返回 403）
wait_caddy_container() {
    local _ code=""
    for _ in $(seq 1 90); do
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

# prepare_caddy_container：升级前准备 Caddy 容器要用的镜像，并迁移宿主机 Caddy。
# 迁移标记同时写进本次备份目录，升级没成功、恢复到升级前时据此换回宿主机 Caddy。
prepare_caddy_container() {
    if ! docker image inspect "acornfox/caddy:${CADDY_VERSION}" >/dev/null 2>&1; then
        if [ -x /usr/bin/caddy ] && /usr/bin/caddy version 2>/dev/null | grep -q "^v${CADDY_VERSION}"; then
            build_caddy_image /usr/bin/caddy
        else
            log_info "宿主机没有 Caddy ${CADDY_VERSION}，acornfox 启动时会从镜像仓库拉取 caddy:${CADDY_VERSION}-alpine"
        fi
    fi
    if [ -f /var/lib/acornfox/.caddy-host-migrated ]; then
        return 0
    fi
    migrate_host_caddy
    if [ -f /var/lib/acornfox/.caddy-host-migrated ]; then
        local backup
        backup="${BACKUP_PATH:-$(last_backup)}"
        [ -n "$backup" ] && [ -d "$backup" ] && cp /var/lib/acornfox/.caddy-host-migrated "$backup/caddy-host-migrated"
    fi
}

# restore_host_caddy BACKUP：从 v0.3.x 升级没成功、恢复到升级前时，删掉 Caddy 容器、换回宿主机 Caddy
restore_host_caddy() {
    local backup="$1"
    [ -f "$backup/caddy-host-migrated" ] || return 0
    log_info "恢复宿主机 Caddy（网关从容器换回宿主机，应用网址会断几秒）..."
    docker rm -f acornfox-caddy >/dev/null 2>&1 || true
    systemctl enable --now caddy >/dev/null 2>&1 || log_warn "宿主机 Caddy 没能启动：journalctl -u caddy -n 50"
    rm -f /var/lib/acornfox/.caddy-host-migrated
    local _
    for _ in $(seq 1 10); do
        [ -S /run/acornfox/caddy-admin.sock ] && break
        sleep 1
    done
    log_success "宿主机 Caddy 已恢复（数据卷 acornfox-caddy-data 保留，可手动删除）"
}

# 数据目录里记下这份数据库最后是哪个版本的程序迁移过的（与 install.sh 中的同名变量和函数一致）。
# 只在升级成功后写：升级没成功、恢复到升级前时数据库是原来的，文件里也还是原来的版本。
# 卸载保留数据后 install.sh 据此拒绝装更旧的版本
INSTALLED_VERSION_FILE="/var/lib/acornfox/installed-version"
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

# backup_version 目录：备份时记下的版本（version.txt 第一行）；没有记录时输出空
backup_version() {
    head -n 1 "$1/version.txt" 2>/dev/null || true
}

# restore_file 源 目标 属主 权限：先拷到目标旁边的临时文件、设好属主和权限，再一步换上（mv 是原子的），
# 拷到一半出问题时目标还是原来的完整文件，不会留下只拷了一半的程序或数据库。失败返回 1
restore_file() {
    local src="$1" dst="$2" owner="$3" mode="$4" tmp="${2}.restore-tmp"
    rm -f "$tmp"
    if cp -p "$src" "$tmp" && chown "$owner" "$tmp" && chmod "$mode" "$tmp" && mv -f "$tmp" "$dst"; then
        return 0
    fi
    rm -f "$tmp"
    log_error "没能把 ${src} 换回 ${dst}"
    return 1
}

# restore_backup 备份目录：升级没成功时，用本次升级前刚做的备份恢复到升级前（程序、数据库，以及
# 从 v0.3.x 升级时换掉的宿主机 Caddy），再启动服务。只在本脚本自己的升级失败时调用，不对外提供
# （没有 --rollback）。恢复后服务起不来、或者文件没能换回去时报 UPGRADE-RESTORE-01。
# 恢复过程不能被打断（restore_log_start 屏蔽 Ctrl+C、kill、SSH 断开），每个文件原子替换；
# 结束时一定以 upgrade_failed 或 fault_exit 退出，所以不用再恢复原来的信号处理。
restore_backup() {
    local backup="$1" v f failed=""
    restore_log_start
    UPGRADE_PHASE=""
    if [ -z "$backup" ] || [ ! -d "$backup" ]; then
        fault_exit UPGRADE-RESTORE-01 "找不到本次升级前的备份（${backup:-没有记录}），没法自动恢复到升级前" \
            "应用容器没有停，已有的应用一般还能打开；AcornFox 服务可能停着，控制台和命令行暂时用不了。" \
            "先看日志：journalctl -u acornfox-server -n 50 --no-pager；journalctl -u acornfox-runner -n 50 --no-pager" \
            "把这段输出和日志反馈给维护者；不要自己删 /var/lib/acornfox 下的任何文件。"
    fi
    v=$(backup_version "$backup")
    log_warn "开始恢复到升级前（${v:-版本未知}），用本次升级前的备份: ${backup}"

    systemctl stop acornfox-server acornfox-runner

    # 恢复程序
    if [ -f "${backup}/acornfox.bin" ]; then
        if restore_file "${backup}/acornfox.bin" /usr/local/bin/acornfox root:root 755; then
            log_success "程序已换回升级前的"
        else
            failed="${failed} 程序"
        fi
    fi

    # 恢复数据库：先删掉迁移后留下的 WAL，避免旧库叠上新 WAL；每个文件原子替换
    if [ -f "${backup}/acornfox.db" ]; then
        rm -f "$DB_PATH-wal" "$DB_PATH-shm"
        for f in acornfox.db acornfox.db-wal acornfox.db-shm; do
            if [ -f "${backup}/$f" ]; then
                restore_file "${backup}/$f" "/var/lib/acornfox/$f" acornfox:acornfox 600 || failed="${failed} $f"
            fi
        done
        [ -n "$failed" ] || log_success "数据库已换回升级前的"
    fi

    if [ -n "$failed" ]; then
        fault_exit UPGRADE-RESTORE-01 "恢复到升级前时，这些文件没能换回去:${failed}；为了不让新旧文件混着运行，没有启动服务" \
            "应用容器没有停，已有的应用一般还能打开；控制台和命令行暂时用不了。" \
            "上面几行是出错原因，常见的是磁盘满了（df -h /var/lib/acornfox /usr/local/bin）。" \
            "升级前的备份在 ${backup}，不要删。处理好磁盘后把这段输出反馈给维护者；不要自己往回拷贝备份里的文件，也不要重装。"
    fi

    restore_host_caddy "$backup"

    systemctl start acornfox-runner
    sleep 2
    systemctl start acornfox-server
    sleep 3

    if systemctl is-active --quiet acornfox-server && systemctl is-active --quiet acornfox-runner; then
        log_success "已恢复到升级前，服务照常运行，现在的版本: $(current_version_now)"
        return 0
    fi
    fault_exit UPGRADE-RESTORE-01 "已把程序和数据库换回升级前的，但服务还是起不来" \
        "应用容器没有停，已有的应用一般还能打开；控制台和命令行暂时用不了。" \
        "先看日志：journalctl -u acornfox-server -n 50 --no-pager；journalctl -u acornfox-runner -n 50 --no-pager" \
        "升级前的备份在 ${backup}，不要删。" \
        "把这段输出和日志反馈给维护者；不要自己往回拷贝备份里的文件，也不要重装。"
}

# show_unit_logs：新版本服务起不来时，先把两个服务最近的日志打出来（恢复之后再看就混在一起了）
show_unit_logs() {
    command -v journalctl >/dev/null 2>&1 || return 0
    local u
    for u in acornfox-server acornfox-runner; do
        echo "    ---- ${u} 最近的日志 ----"
        journalctl -u "$u" -n 8 --no-pager 2>/dev/null | sed 's/^/    /' || true
    done
}

# upgrade_failed 编号 原因：已经恢复到升级前之后，带故障编号说明这次没升上
upgrade_failed() {
    fault_exit "$1" "${2}，没有升上：已自动恢复到升级前的 ${CURRENT_VERSION}，程序和数据库都是原来的，服务照常运行。" \
        "应用容器一直在运行，没有受影响。" \
        "上面几行是失败原因。可以稍后重新运行升级命令再试一次。" \
        "再次失败时，把这段输出和 journalctl -u acornfox-server -n 50 --no-pager 的结果反馈给维护者。"
}

# 清理旧备份（保留最近 5 个）
cleanup_old_backups() {
    if [ -d "$BACKUP_DIR" ]; then
        BACKUP_COUNT=$(ls -1 "$BACKUP_DIR" | wc -l)

        if [ "$BACKUP_COUNT" -gt 5 ]; then
            log_info "清理旧备份..."
            ls -1t "$BACKUP_DIR" | tail -n +6 | xargs -I {} rm -rf "${BACKUP_DIR}/{}"
            log_success "已清理旧备份"
        fi
    fi
}

# 检查应用容器状态
check_apps() {
    log_info "检查应用容器状态..."

    APP_COUNT=$(docker ps --filter "label=acornfox.app" --format "{{.Names}}" 2>/dev/null | wc -l || echo "0")

    if [ "$APP_COUNT" -gt 0 ]; then
        log_success "应用容器运行正常 ($APP_COUNT 个容器)"
    else
        log_info "当前没有运行中的应用"
    fi
}

# 显示升级摘要
show_summary() {
    NEW_VERSION=$(/usr/local/bin/acornfox version 2>/dev/null | awk '{print $2}' || echo "unknown")

    # 标题按实际写：版本号相同是重装（-y），其余算升级（比当前旧的版本在停服务之前就拒绝了）
    local act="升级"
    if [ -n "${CURRENT_VERSION#v}" ] && [ "${NEW_VERSION#v}" = "${CURRENT_VERSION#v}" ]; then
        act="重装"
    fi

    cat << EOF

${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}
${GREEN}✓ ${act}完成！${NC}
${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}

${BLUE}版本:${NC}
  • ${act}前: $CURRENT_VERSION
  • ${act}后: $NEW_VERSION

${BLUE}服务状态:${NC}
  • acornfox-server: $(systemctl is-active acornfox-server)
  • acornfox-runner: $(systemctl is-active acornfox-runner)
  • Caddy 容器 acornfox-caddy: $(docker inspect -f '{{.State.Status}}' acornfox-caddy 2>/dev/null || echo 未创建)

${BLUE}升级前的备份（升级没成功时自动恢复用，保留最近 5 份）:${NC}
  $(last_backup)

${BLUE}说明:${NC}
  AcornFox 只能升级，不能回到旧版本。应用自己的版本可以用 acornfox rollback 应用名 回退。

${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}

EOF
}

# 主函数
main() {
    echo ""
    log_info "AcornFox 升级脚本"
    echo ""

    parse_args "$@"

    # 只能升级：--rollback 在做任何事之前就拒绝
    if $WANT_ROLLBACK; then
        refuse_rollback
    fi

    check_root
    $ASSUME_YES || [ -t 0 ] || PIPE_CONSENT=true
    show_plan
    check_installed

    if [ -z "$LOCAL_BINARY" ]; then
        setup_github_proxies
        TARGET_IS_LATEST=false
        if [ "$TARGET_VERSION" = "latest" ]; then
            TARGET_IS_LATEST=true
            TARGET_VERSION=$(latest_tag) || error_exit "无法获取最新版本号；用 --version vX.Y.Z 指定版本，或用 --binary 使用本地文件"
            log_info "最新版本: $TARGET_VERSION"
        fi
        # 只能升级：要装的版本比当前旧时，不论有没有 -y，都在交给旧版本脚本、下载之前停下。
        # 最新发行版比当前旧（例如装的是还没发布的预发布版）时不算降级请求，直接说不用升级
        if version_older "$TARGET_VERSION" "$CURRENT_VERSION"; then
            if $TARGET_IS_LATEST; then
                log_success "当前版本 ${CURRENT_VERSION} 不比最新发行版 ${TARGET_VERSION} 旧，不用升级"
                exit 0
            fi
            refuse_downgrade "$TARGET_VERSION"
        fi
        if $PIPE_CONSENT; then
            pipe_version_gate "$TARGET_VERSION"
        fi
        # 老版本的 upgrade.sh 在管道里不带 -y 会直接退出；已视为同意（且上面确认了是升级）时替它补上 -y
        if $PIPE_CONSENT; then
            follow_release_script upgrade.sh "$TARGET_VERSION" "$@" -y
        else
            follow_release_script upgrade.sh "$TARGET_VERSION" "$@"
        fi
    fi

    if ! confirm "确认升级 AcornFox?"; then
        log_info "取消升级"
        exit 0
    fi

    # 升级流程：先下载（失败不影响运行中的服务），再停服务、备份、迁移
    download_new_version
    # 停服务前检查端口：本机端口被别的程序占着时新版本起不来，现在停下不影响运行中的服务
    check_ports upgrade
    refresh_install_env_cloud
    UPGRADE_PHASE=stopped
    stop_services
    backup_current
    # 从这里起程序、数据库、目录权限、网关都可能被改：意外退出时 on_exit 用备份恢复到升级前
    UPGRADE_PHASE=changing
    fix_layout

    # 尝试执行迁移
    if ! run_migrations; then
        log_error "新版本的数据库迁移失败，开始恢复到升级前..."
        restore_backup "$BACKUP_PATH"
        upgrade_failed UPGRADE-FAIL-01 "新版本 ${NEW_VERSION} 的数据库迁移失败"
    fi

    replace_binary
    prepare_caddy_container

    # 尝试启动服务
    if ! start_services; then
        log_error "新版本的服务起不来，开始恢复到升级前..."
        show_unit_logs
        restore_backup "$BACKUP_PATH"
        upgrade_failed UPGRADE-FAIL-02 "新版本 ${NEW_VERSION} 的服务起不来"
    fi
    # 新版本已经起来：之后的问题（网关容器没就绪等）只提示，不再恢复到升级前
    UPGRADE_PHASE=""
    record_installed_version

    wait_caddy_container

    # 验证升级
    if ! verify_upgrade; then
        log_warn "升级验证失败，建议检查日志"
    fi

    # 检查应用
    check_apps

    # 清理
    cleanup_old_backups

    # 显示摘要
    show_summary
}

main "$@"
