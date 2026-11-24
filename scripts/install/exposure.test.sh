#!/usr/bin/env bash
# install.sh 定“新应用默认的对外方式”（choose_default_exposure）的不动系统测试。
#
# 做什么：把 install.sh 当函数库读进来（AFX_INSTALL_LIB=1，只定义函数、不执行），给定
#   全新安装与否、云厂商、应用访问地址、--default-exposure、现有服务文件，核对：
#   局域网服务器全新安装给 lan；云主机、公网地址、取不到地址不写（应用中心装的应用照旧 local）；
#   重装沿用现有服务文件里的值、没有就不写；参数优先；写错的参数在改系统之前就停下；
#   服务文件的 ExecStart 带上 --default-exposure；upgrade.sh 不碰这一项（升级上来的机器不变）。
# 什么时候用：改了 install.sh 里局域网判断、默认对外方式、服务文件的部分之后。
#   scripts/get.test.sh 最后会调用它（测试闸门的 oss.scripts）。
# 前提：bash、sed、grep。不需要 root，不联网。
# 怎么运行：bash scripts/install/exposure.test.sh
# 怎么验证：最后一行“通过 N/N”，退出码 0；不对的项打印“不对”和实际输出。
# 副作用：只在 mktemp 建的临时目录里写文件，退出时删掉。不改系统。
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
INSTALL="$HERE/install.sh"
UPGRADE="$HERE/upgrade.sh"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
pass=0; total=0
ok() { total=$((total + 1)); pass=$((pass + 1)); echo "对    $1"; }
bad() { total=$((total + 1)); echo "不对  $1"; echo "$2" | sed 's/^/        /' | head -20; }
check() {  # 名称 期望的（grep -F） 实际
    if grep -qF -- "$2" <<<"$3"; then ok "$1"; else bad "$1（没有：$2）" "$3"; fi
}

# choose 全新安装 云厂商 应用访问地址 参数 [服务文件]：读入函数后运行 choose_default_exposure，
# 输出 “DEFAULT_EXPOSURE=… APPS_EXPOSURE=…”
choose() {
    AFX_INSTALL_LIB=1 AFX_SERVER_UNIT="${5:-$T/no-such.service}" \
        bash -c 'source "$1"; FRESH_INSTALL=$2 CLOUD=$3 PUBLIC_HOST=$4 DEFAULT_EXPOSURE_ARG=$5
                 choose_default_exposure >/dev/null 2>&1
                 echo "DEFAULT_EXPOSURE=${DEFAULT_EXPOSURE} APPS_EXPOSURE=${APPS_EXPOSURE}"' \
        _ "$INSTALL" "$1" "$2" "$3" "$4" 2>&1
}

# ---------- 全新安装 ----------
check "局域网服务器（192.168.x，不是云主机）全新安装：lan" "DEFAULT_EXPOSURE=lan APPS_EXPOSURE=lan" "$(choose true '' 192.168.1.20 '')"
check "局域网服务器（10.x）全新安装：lan" "DEFAULT_EXPOSURE=lan APPS_EXPOSURE=lan" "$(choose true '' 10.0.0.5 '')"
check "局域网服务器（172.16～31.x）全新安装：lan" "DEFAULT_EXPOSURE=lan APPS_EXPOSURE=lan" "$(choose true '' 172.20.3.4 '')"
check "云主机（私网地址也算云主机）：不写，应用中心装的应用 local" "DEFAULT_EXPOSURE= APPS_EXPOSURE=local" "$(choose true aliyun 172.20.3.4 '')"
check "云主机（公网地址）：不写，local" "DEFAULT_EXPOSURE= APPS_EXPOSURE=local" "$(choose true tencent 203.0.113.10 '')"
check "没识别出云厂商、公网地址：不写，local" "DEFAULT_EXPOSURE= APPS_EXPOSURE=local" "$(choose true '' 203.0.113.10 '')"
check "取不到应用访问地址：不写，local" "DEFAULT_EXPOSURE= APPS_EXPOSURE=local" "$(choose true '' '' '')"
check "局域网服务器加 --default-exposure local：local" "DEFAULT_EXPOSURE=local APPS_EXPOSURE=local" "$(choose true '' 192.168.1.20 local)"
check "云主机加 --default-exposure lan：照参数" "DEFAULT_EXPOSURE=lan APPS_EXPOSURE=lan" "$(choose true aliyun 172.20.3.4 lan)"

# ---------- 重装（不是全新安装） ----------
unit_lan="$T/lan.service"
printf '[Service]\nExecStart=/usr/local/bin/acornfox server --runner-uid 998 --listen-group acornfox-users --public-host 192.168.1.20 --default-exposure lan\n' > "$unit_lan"
unit_old="$T/old.service"
printf '[Service]\nExecStart=/usr/local/bin/acornfox server --runner-uid 998 --listen-group acornfox-users --public-host 192.168.1.20\n' > "$unit_old"
check "重装：沿用服务文件里的 lan" "DEFAULT_EXPOSURE=lan APPS_EXPOSURE=lan" "$(choose false '' 192.168.1.20 '' "$unit_lan")"
check "重装以前装的机器（服务文件里没有）：局域网里也不写，保持原样" "DEFAULT_EXPOSURE= APPS_EXPOSURE=local" "$(choose false '' 192.168.1.20 '' "$unit_old")"
check "重装、没有服务文件：不写" "DEFAULT_EXPOSURE= APPS_EXPOSURE=local" "$(choose false '' 192.168.1.20 '')"
check "重装加 --default-exposure local：照参数" "DEFAULT_EXPOSURE=local APPS_EXPOSURE=local" "$(choose false '' 192.168.1.20 local "$unit_lan")"

# ---------- 参数检查（在改系统之前） ----------
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --default-exposure public; echo "到不了这里"' _ "$INSTALL" 2>&1); rc=$?
check "--default-exposure public 不认：报错" "只能是 lan（本机和局域网）或 local（只本机）" "$out"
check "--default-exposure public 不认：停下" "rc=1" "rc=$rc"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --default-exposure local; echo "参数=${DEFAULT_EXPOSURE_ARG}"' _ "$INSTALL" 2>&1)
check "--default-exposure local：记下" "参数=local" "$out"
out=$(AFX_INSTALL_LIB=1 ACORNFOX_DEFAULT_EXPOSURE=lan bash -c 'source "$1"; parse_args; echo "参数=${DEFAULT_EXPOSURE_ARG}"' _ "$INSTALL" 2>&1)
check "环境变量 ACORNFOX_DEFAULT_EXPOSURE=lan：记下" "参数=lan" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; show_help' _ "$INSTALL" 2>&1)
check "帮助里有 --default-exposure" "--default-exposure 方式" "$out"

# ---------- 服务文件 ----------
line=$(grep '^ExecStart=/usr/local/bin/acornfox server ' "$INSTALL")
render() {  # DEFAULT_EXPOSURE PUBLIC_HOST：按 install.sh 里那一行展开
    ACORNFOX_EXEC_UID=998 DEFAULT_EXPOSURE="$1" PUBLIC_HOST="$2" bash -c "echo \"$line\""
}
check "服务文件：lan 时带 --default-exposure lan" "--public-host 192.168.1.20 --default-exposure lan" "$(render lan 192.168.1.20)"
out=$(render '' 203.0.113.10)
if grep -q -- '--default-exposure' <<<"$out"; then bad "服务文件：不写时没有 --default-exposure" "$out"; else ok "服务文件：不写时没有 --default-exposure"; fi
check "服务文件写出来的值 choose 能读回" "DEFAULT_EXPOSURE=lan" "$(printf '[Service]\n%s\n' "$(render lan 192.168.1.20)" > "$T/r.service"; choose false '' 192.168.1.20 '' "$T/r.service")"

# ---------- 局域网控制台用同一个判断 ----------
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; for x in "|192.168.1.20" "aliyun|192.168.1.20" "|203.0.113.10"; do CLOUD=${x%%|*} PUBLIC_HOST=${x#*|}; if lan_server; then echo "${x}=是"; else echo "${x}=不是"; fi; done' _ "$INSTALL" 2>&1)
check "lan_server：私网地址、不是云主机" "|192.168.1.20=是" "$out"
check "lan_server：云主机不算" "aliyun|192.168.1.20=不是" "$out"
check "lan_server：公网地址不算" "|203.0.113.10=不是" "$out"
check "enable_lan_console 用 lan_server" "lan_server || return 0" "$(sed -n '/^enable_lan_console()/,/^}/p' "$INSTALL")"

# ---------- WSL（Windows 里的 Linux，install.sh --wsl） ----------
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --wsl --public-host 192.168.1.9; echo "wsl=$WSL_MODE host=$PUBLIC_HOST"' _ "$INSTALL" 2>&1)
check "--wsl：记下，--public-host 照常" "wsl=true host=192.168.1.9" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args; echo "wsl=$WSL_MODE"' _ "$INSTALL" 2>&1)
check "不加 --wsl：不是 WSL" "wsl=false" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --wsl; for x in "|127.0.0.1" "aliyun|192.168.1.20" "|"; do CLOUD=${x%%|*} PUBLIC_HOST=${x#*|}; if lan_server; then echo "${x}=是"; else echo "${x}=不是"; fi; done' _ "$INSTALL" 2>&1)
check "WSL：地址是 127.0.0.1 也算局域网机器（默认打开局域网控制台）" "|127.0.0.1=是" "$out"
check "WSL：识别出云厂商的不算" "aliyun|192.168.1.20=不是" "$out"
check "WSL：没传地址也算局域网机器" "|=是" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --wsl; PUBLIC_HOST=""; log_info() { :; }; detect_public_host; echo "host=$PUBLIC_HOST"' _ "$INSTALL" 2>&1)
check "WSL 没传 --public-host：用 127.0.0.1，不去读 WSL 内部地址" "host=127.0.0.1" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --wsl --public-host 192.168.1.9; log_info() { :; }; detect_public_host; echo "host=$PUBLIC_HOST"' _ "$INSTALL" 2>&1)
check "WSL 传了 --public-host：用它" "host=192.168.1.9" "$out"
check "choose_default_exposure：WSL 全新安装是 lan" "DEFAULT_EXPOSURE=lan APPS_EXPOSURE=lan" "$(AFX_INSTALL_LIB=1 AFX_SERVER_UNIT="$T/no-such.service" bash -c 'source "$1"; parse_args --wsl; FRESH_INSTALL=true CLOUD="" PUBLIC_HOST=127.0.0.1 DEFAULT_EXPOSURE_ARG=""; choose_default_exposure >/dev/null 2>&1; echo "DEFAULT_EXPOSURE=${DEFAULT_EXPOSURE} APPS_EXPOSURE=${APPS_EXPOSURE}"' _ "$INSTALL" 2>&1)"
check "帮助里有 --wsl" "--wsl                  这台机器是 Windows 里的 WSL 发行版" "$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; show_help' _ "$INSTALL" 2>&1)"
check "WSL 摘要不讲云服务器安全组和 SSH" "Windows 上用" "$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --wsl; show_summary' _ "$INSTALL" 2>&1)"

# ---------- Mac 里的虚拟机（install.sh --mac-vm）：网络上同 WSL，摘要写 Mac 上怎么用 ----------
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --mac-vm --public-host 192.168.1.9; echo "wsl=$WSL_MODE mac=$MAC_VM_MODE host=$PUBLIC_HOST"' _ "$INSTALL" 2>&1)
check "--mac-vm：按 WSL 的网络规矩处理，另记是 Mac 虚拟机" "wsl=true mac=true host=192.168.1.9" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --wsl; echo "wsl=$WSL_MODE mac=$MAC_VM_MODE"' _ "$INSTALL" 2>&1)
check "--wsl 不是 Mac 虚拟机" "wsl=true mac=false" "$out"
out=$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --mac-vm; for x in "|127.0.0.1" "aliyun|192.168.1.20"; do CLOUD=${x%%|*} PUBLIC_HOST=${x#*|}; if lan_server; then echo "${x}=是"; else echo "${x}=不是"; fi; done' _ "$INSTALL" 2>&1)
check "Mac 虚拟机：算局域网机器（默认打开局域网控制台）" "|127.0.0.1=是" "$out"
check "choose_default_exposure：Mac 虚拟机全新安装是 lan" "DEFAULT_EXPOSURE=lan APPS_EXPOSURE=lan" "$(AFX_INSTALL_LIB=1 AFX_SERVER_UNIT="$T/no-such.service" bash -c 'source "$1"; parse_args --mac-vm; FRESH_INSTALL=true CLOUD="" PUBLIC_HOST=192.168.1.9 DEFAULT_EXPOSURE_ARG=""; choose_default_exposure >/dev/null 2>&1; echo "DEFAULT_EXPOSURE=${DEFAULT_EXPOSURE} APPS_EXPOSURE=${APPS_EXPOSURE}"' _ "$INSTALL" 2>&1)"
check "帮助里有 --mac-vm" "--mac-vm               这台机器是 Mac 上苹果 container 开的 Debian 虚拟机" "$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; show_help' _ "$INSTALL" 2>&1)"
check "Mac 虚拟机的摘要讲 Mac 上怎么用" "在 Mac 上用" "$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --mac-vm; show_summary' _ "$INSTALL" 2>&1)"
check "Mac 虚拟机的摘要不讲 Windows" "http://127.0.0.1:18802/" "$(AFX_INSTALL_LIB=1 bash -c 'source "$1"; parse_args --mac-vm; show_summary' _ "$INSTALL" 2>&1)"
check "服务文件：--mac-vm 也写 --wsl（只本机的应用监听虚拟机所有网卡）" '${WSL_FLAG:+ --wsl}' "$line"

# ---------- 升级上来的机器不变 ----------
out=$(grep -c -- '--default-exposure' "$UPGRADE" || true)
check "upgrade.sh 不写 --default-exposure" "0" "$out"
out=$(grep -n 'ExecStart=' "$UPGRADE" | grep -v 'listen-group' || true)
if [ -z "$out" ]; then ok "upgrade.sh 改服务文件只有追加 --listen-group 那一处（不重写 ExecStart）"; else bad "upgrade.sh 还有别处改 ExecStart" "$out"; fi

echo "通过 $pass/$total"
[ "$pass" = "$total" ]
