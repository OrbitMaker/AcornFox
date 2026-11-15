#!/usr/bin/env bash
# install.sh 选软件源（rank_sources）和装 Docker 失败时报 INSTALL-DOCKER-01 的不动系统测试。
#
# 做什么：把 install.sh 当函数库读进来（AFX_INSTALL_LIB=1，只定义函数、不执行），PATH 最前面放一个假的 curl：
#   按 URL 返回事先写好的结果（状态码、下载了多少字节、首字节时间、总用时），记下每次调用的参数。
#   核对：最快的被选中、都够快时照候选表的先后、慢的排后面、坏的跳过并写明原因、自己指定的源固定第一、
#   取的是最新的 docker-ce 包、系统软件源取样的地址对、全都不通时不输出任何源、探测有时间上限，
#   以及 docker-ce 源和 docker.io 都装不上时停在 INSTALL-DOCKER-01 并列出每个源的结果。
# 什么时候用：改了 install.sh 里选软件源、装 Docker 的部分之后。scripts/get.test.sh 最后会调用它（测试闸门的 oss.scripts）。
# 前提：bash、awk、sort -V。不需要 root，不联网（curl 是假的）。
# 怎么运行：bash scripts/install/sources.test.sh
# 怎么验证：最后一行“通过 N/N”，退出码 0；不对的项打印“不对”和实际输出。
# 副作用：只在 mktemp 建的临时目录里写文件，退出时删掉。不改系统（apt、gpg、systemctl 都换成了假的）。
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
INSTALL="$HERE/install.sh"
T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
pass=0; total=0
ok() { total=$((total + 1)); pass=$((pass + 1)); echo "对    $1"; }
bad() { total=$((total + 1)); echo "不对  $1"; echo "$2" | sed 's/^/        /' | head -20; }
check() {  # 名称 期望的（grep -F） 实际
    if grep -qF -- "$2" <<<"$3"; then ok "$1"; else bad "$1（没有：$2）" "$3"; fi
}
check_not() {
    if grep -qF -- "$2" <<<"$3"; then bad "$1（不该有：$2）" "$3"; else ok "$1"; fi
}

# ---------- 假的 curl ----------
# $T/curl.conf 每行：URL 里含的字符串 状态码 字节数 首字节秒数 总秒数 [要睡几秒] [-o 时写进去的文件]
mkdir -p "$T/bin"
cat > "$T/bin/curl" <<'EOF'
#!/usr/bin/env bash
url="" out="" fmt="" max=0 conf="${FAKE_CURL_CONF}"
while [ $# -gt 0 ]; do
    case "$1" in
        -o) out="$2"; shift ;;
        -w) fmt="$2"; shift ;;
        -m) max="$2"; shift ;;
        -r|--connect-timeout) shift ;;
        http*) url="$1" ;;
    esac
    shift
done
echo "$url max=$max" >> "${FAKE_CURL_LOG}"
line=$(awk -v u="$url" 'index(u, $1) { print; exit }' "$conf")
if [ -z "$line" ]; then
    [ -n "$fmt" ] && printf '000 0 0.000 0.001'
    exit 7
fi
read -r _ code size tfb ttot sleep_s content <<<"$line"
rc=0
if [ -n "${sleep_s:-}" ] && [ "$sleep_s" != - ]; then
    if [ "$max" != 0 ] && [ "$sleep_s" -ge "$max" ]; then sleep "$max"; ttot="$max"; rc=28; else sleep "$sleep_s"; fi
fi
if [ -n "$out" ] && [ "$out" != /dev/null ]; then
    if [ -n "${content:-}" ]; then cp "$content" "$out"; else : > "$out"; fi
fi
[ -n "$fmt" ] && printf '%s %s %s %s' "$code" "$size" "$tfb" "$ttot"
exit "$rc"
EOF
chmod +x "$T/bin/curl"

# 一份 docker-ce 的 Packages：最新的 docker-ce 是 29.8.2（故意不按顺序写，还混着别的包）
cat > "$T/Packages" <<'EOF'
Package: docker-ce
Version: 5:29.1.0-1~ubuntu.24.04~noble
Filename: pool/stable/amd64/docker-ce_29.1.0-1~ubuntu.24.04~noble_amd64.deb

Package: docker-ce-cli
Version: 5:29.9.9-1~ubuntu.24.04~noble
Filename: pool/stable/amd64/docker-ce-cli_29.9.9-1~ubuntu.24.04~noble_amd64.deb

Package: docker-ce
Version: 5:29.8.2-1~ubuntu.24.04~noble
Filename: pool/stable/amd64/docker-ce_29.8.2-1~ubuntu.24.04~noble_amd64.deb

Package: docker-ce
Version: 5:28.5.1-1~ubuntu.24.04~noble
Filename: pool/stable/amd64/docker-ce_28.5.1-1~ubuntu.24.04~noble_amd64.deb
EOF

# run 配置 命令 参数…：在子进程里读入 install.sh 的函数后执行命令（命令里用 "$1" "$2" … 引用参数）；
# 标准输出和标准错误都收进来
run() {
    local conf="$1"; shift
    printf '%s\n' "$conf" > "$T/curl.conf"
    : > "$T/curl.log"
    FAKE_CURL_CONF="$T/curl.conf" FAKE_CURL_LOG="$T/curl.log" PATH="$T/bin:$PATH" AFX_INSTALL_LIB=1 \
        bash -c 'source "$1"; cmd="$2"; shift 2; OS=ubuntu; OS_CODENAME=noble; dpkg() { echo amd64; }; eval "$cmd"' _ "$INSTALL" "$@" 2>&1
}

A="阿里云|https://a.example/docker-ce|https://a.example/docker-ce"
B="华为云|https://b.example/docker-ce|https://b.example/docker-ce"
C="腾讯云|https://c.example/docker-ce|https://c.example/docker-ce"
D="南京大学|https://d.example/docker-ce|https://d.example/docker-ce"
E="清华大学|https://e.example/docker-ce|https://e.example/docker-ce"
PKG="/docker-ce/linux/ubuntu/dists/noble/stable/binary-amd64/Packages"
DEB="/docker-ce/linux/ubuntu/pool/stable/amd64/docker-ce_29.8.2"

# ---------- 选 docker-ce 源 ----------
conf="a.example$PKG 200 300000 0.2 0.5 - $T/Packages
a.example$DEB 206 4194304 0.4 0.6
b.example$PKG 200 300000 0.5 2.0 - $T/Packages
b.example$DEB 206 1200000 1.0 6.0
c.example$PKG 200 300000 0.2 0.4 - $T/Packages
c.example$DEB 206 0 0 6.0"
out=$(run "$conf" 'rank_sources docker 0 "$1" "$2" "$3" "$4"' "$B" "$C" "$D" "$A")
check "最快的排第一（阿里云 20 MB/s，表里在最后）" "$(printf '%s\n%s' "$A" "$B")" "$out"
check "慢的排在后面，日志写明慢" "华为云 234 KB/s（慢）" "$out"
check "取样 0 字节的不用，写明原因" "腾讯云（取不到数据，不用）" "$out"
check "连不上的不用，写明原因" "南京大学（连不上或取不到索引，不用）" "$out"
check "日志写出先用哪个" "；先用 阿里云" "$out"
check_not "坏的源不在结果里" "$C" "$out"
log=$(cat "$T/curl.log")
check "取的是最新的 docker-ce 包（29.8.2，不是 docker-ce-cli 的 29.9.9）" "b.example${DEB}-1~ubuntu.24.04~noble_amd64.deb" "$log"
check_not "没有去取不通的源的包" "d.example$DEB" "$log"

conf="a.example$PKG 200 300000 0.2 0.5 - $T/Packages
a.example$DEB 206 4194304 0.2 0.4
e.example$PKG 200 300000 0.2 0.5 - $T/Packages
e.example$DEB 206 4194304 0.1 0.2"
out=$(run "$conf" 'rank_sources docker 0 "$1" "$2"' "$A" "$E")
check "都够快时照候选表的先后（不是谁快谁先）" "$(printf '%s\n%s' "$A" "$E")" "$out"

conf="b.example$PKG 200 300000 0.5 2.0 - $T/Packages
b.example$DEB 206 400000 1.0 6.0
a.example$PKG 200 300000 0.2 0.5 - $T/Packages
a.example$DEB 206 4194304 0.2 0.4"
out=$(run "$conf" 'rank_sources docker 1 "$1" "$2"' "自己指定的源|https://b.example/docker-ce|https://b.example/docker-ce" "$A")
check "自己指定的源只要能用就排第一（哪怕慢）" "$(printf '%s\n%s' "自己指定的源|https://b.example/docker-ce|https://b.example/docker-ce" "$A")" "$out"

conf="a.example$PKG 200 300000 0.2 0.5 - $T/Packages
a.example$DEB 404 0 0.1 0.1
e.example$PKG 200 300000 0.2 0.5 - $T/Packages
e.example$DEB 206 400000 0.2 6.0"
out=$(run "$conf" 'rank_sources docker 0 "$1" "$2"' "$A" "$E")
check "取样 404（没同步到最新的包）排在慢的后面，还会试" "$(printf '%s\n%s' "$E" "$A")" "$out"

out=$(run "" 'rank_sources docker 0 "$1" "$2"; echo "行数=$(rank_sources docker 0 "$1" "$2" 2>/dev/null | wc -l)"; cat "$AFX_WORK/probe-docker.txt"' "$A" "$B")
check "全都不通：不输出任何源" "行数=0" "$out"
check "全都不通：日志写“都不能用”" "；都不能用" "$out"
check "全都不通：每个源的结果另存（报错时用）" "阿里云（连不上或取不到索引，不用）、华为云（连不上或取不到索引，不用）" "$out"

out=$(run "" 'rank_sources docker 0 "$1"' "$A")
check "只有一个候选时不测，原样输出" "$A" "$out"
total=$((total + 1)); if [ ! -s "$T/curl.log" ]; then pass=$((pass + 1)); echo "对    只有一个候选时不发请求"; else echo "不对  只有一个候选时发了请求：$(cat "$T/curl.log")"; fi

# ---------- 系统软件源 ----------
conf="m1.example/ubuntu/dists/noble/Release 200 120000 0.1 0.2
m1.example/ubuntu/dists/noble/main/binary-amd64/Packages.gz 200 1800000 0.3 4.0
m2.example/ubuntu/dists/noble/Release 200 120000 0.1 0.2
m2.example/ubuntu/dists/noble/main/binary-amd64/Packages.gz 200 1800000 0.3 0.7"
out=$(run "$conf" 'rank_sources apt 0 "$1" "$2"' "华为云|http://m1.example" "阿里云|http://m2.example")
check "系统软件源：快的排第一" "$(printf '%s\n%s' "阿里云|http://m2.example" "华为云|http://m1.example")" "$out"
check "系统软件源：日志标题" "系统软件源实测" "$out"

# ---------- 时间上限 ----------
out=$(run "" 'echo "默认上限 索引=$PROBE_INDEX_MAX 取样=$PROBE_SAMPLE_MAX 连接=$PROBE_CONNECT"')
check "默认时限：取索引 8 秒、取样 6 秒、连接 3 秒（整个最多约 15 秒）" "默认上限 索引=8 取样=6 连接=3" "$out"
conf="a.example$PKG 200 300000 0.2 30 30 $T/Packages
b.example$PKG 200 300000 0.2 30 30 $T/Packages
c.example$PKG 200 300000 0.2 30 30 $T/Packages"
t0=$(date +%s)
out=$(AFX_PROBE_INDEX_MAX=1 AFX_PROBE_SAMPLE_MAX=1 run "$conf" 'rank_sources docker 0 "$1" "$2" "$3"' "$A" "$B" "$C")
el=$(( $(date +%s) - t0 ))
total=$((total + 1)); if [ "$el" -le 5 ]; then pass=$((pass + 1)); echo "对    探测有时间上限：三个都卡住的源同时测，${el} 秒结束（上限各 1 秒）"; else echo "不对  三个都卡住的源测了 ${el} 秒"; fi
check "卡住的源按给的上限取（-m 1）" "max=1" "$(cat "$T/curl.log")"

# ---------- 装不上 Docker：INSTALL-DOCKER-01 ----------
out=$(run "" 'command() { if [ "${2:-}" = docker ]; then return 1; fi; builtin command "$@"; }
    apt_get() { return 1; }; gpg() { return 1; }; systemctl() { :; }
    SKIP_DOCKER=false IN_CHINA=true CLOUD=""
    DOCKER_CE_SOURCES_CN=("$1" "$2")
    install_docker; echo "rc=$?"' "$A" "$B"; echo "退出码=$?")
check "都装不上：故障编号 INSTALL-DOCKER-01" "故障编号: INSTALL-DOCKER-01" "$out"
check "都装不上：列出每个源的实测结果" "各个 docker-ce 源的实测结果：阿里云（连不上或取不到索引，不用）" "$out"
check "都装不上：给出三条处理办法" "处理办法 3：先自己装好 Docker，再加 --skip-docker 重新运行" "$out"
check "都装不上：退出码 1" "退出码=1" "$out"
check_not "都装不上：没有接着往下走" "rc=" "$out"

# ---------- 测速一个都没测出来：照候选的先后再试 ----------
conf="a.example/docker-ce/linux/ubuntu/dists/noble/Release 200 100 0.1 0.1
b.example/docker-ce/linux/ubuntu/dists/noble/Release 200 100 0.1 0.1"
out=$(run "$conf" 'command() { if [ "${2:-}" = docker ]; then return 1; fi; builtin command "$@"; }
    apt_get() { return 1; }; gpg() { return 1; }; systemctl() { :; }
    SKIP_DOCKER=false IN_CHINA=true CLOUD=""
    DOCKER_CE_SOURCES_CN=("$1" "$2")
    install_docker' "$A" "$B")
check "测速都不行时照候选的先后再试：有提示" "照候选的先后逐个再试一次" "$out"
check "测速都不行时照候选的先后再试：第一个源真的去探了" "a.example/docker-ce/linux/ubuntu/dists/noble/Release" "$(cat "$T/curl.log")"
check "测速都不行时照候选的先后再试：第二个源也探了" "b.example/docker-ce/linux/ubuntu/dists/noble/Release" "$(cat "$T/curl.log")"

# ---------- 签名密钥必须是 Docker 官方的那一把 ----------
FPR=9DC858229FC7DD38854AE2D88D81803C0EBFCD88
OFFICIAL="pub:-:4096:1:8D81803C0EBFCD88:1487788586:::-:::scESA::::::23::0:
fpr:::::::::${FPR}:
uid:-::::1487792064::B5A08F01796E7F521861B449372D9F8E2B5B8B9E::Docker Release (CE deb) <docker@docker.com>::::::::::0:
sub:-:4096:1:7EA0A9C3F273FCD8:1487788586::::::s::::::23:
fpr:::::::::D3306A018370199E527AE7997EA0A9C3F273FCD8:"
OTHER="pub:-:4096:1:1111111111111111:1487788586:::-:::scESA::::::23::0:
fpr:::::::::AAAA1111BBBB2222CCCC3333DDDD4444EEEE5555:"
printf '%s\n' "$OFFICIAL" > "$T/k.official"
printf '%s\n%s\n' "$OFFICIAL" "$OTHER" > "$T/k.two"
printf '%s\n' "$OTHER" > "$T/k.other"
out=$(run "" 'gpg() { cat "${@: -1}"; }
    for f in official two other; do if docker_key_ok "'"$T"'/k.$f"; then echo "$f=ok"; else echo "$f=no"; fi; done')
check "官方密钥：认" "official=ok" "$out"
check "官方密钥之外多一把别的主密钥：不认" "two=no" "$out"
check "别的密钥：不认" "other=no" "$out"

conf="a.example/docker-ce/linux/ubuntu/dists/noble/Release 200 100 0.1 0.1
a.example/docker-ce/linux/ubuntu/gpg 200 100 0.1 0.1"
out=$(AFX_DOCKER_KEYRING="$T/keyring.gpg" run "$conf" 'command() { if [ "${2:-}" = docker ]; then return 1; fi; builtin command "$@"; }
    apt_get() { echo "APT $*"; return 1; }; systemctl() { :; }
    gpg() { case "$*" in *--dearmor*) cat > /dev/null; echo fake > "$AFX_DOCKER_KEYRING" ;; *--show-keys*) cat "'"$T"'/k.other" ;; esac; }
    SKIP_DOCKER=false IN_CHINA=true CLOUD=""
    DOCKER_CE_SOURCES_CN=("$1")
    install_docker' "$A")
check "密钥不是官方的：不用这个源，写明原因" "不是 Docker 官方的那一把" "$out"
first_apt=$(grep -n '^APT ' <<<"$out" | head -1 | cut -d: -f1)
fallback=$(grep -n '改用发行版仓库的 docker.io' <<<"$out" | head -1 | cut -d: -f1)
total=$((total + 1)); if [ -n "$fallback" ] && [ -n "$first_apt" ] && [ "$first_apt" -gt "$fallback" ]; then pass=$((pass + 1)); echo "对    密钥不是官方的：没有拿它去 apt update（第一次 apt 在退回 docker.io 之后）"; else echo "不对  密钥不是官方的：apt 在第 ${first_apt:-?} 行，退回 docker.io 在第 ${fallback:-?} 行"; echo "$out" | head -20 | sed 's/^/        /'; fi
total=$((total + 1)); if [ ! -e "$T/keyring.gpg" ]; then pass=$((pass + 1)); echo "对    密钥不是官方的：取到的密钥文件删掉了"; else echo "不对  密钥不是官方的：密钥文件还在"; fi

# ---------- 表只在一处 ----------
n=$(grep -c 'mirrors.tuna.tsinghua.edu.cn/docker-ce' "$INSTALL")
total=$((total + 1)); if [ "$n" = 1 ]; then pass=$((pass + 1)); echo "对    国内 docker-ce 源只写在候选表里（清华只出现在 1 行）"; else echo "不对  mirrors.tuna.tsinghua.edu.cn/docker-ce 出现在 $n 行"; fi
n=$(grep -cE 'docker\.m\.daocloud\.io|docker\.1panel\.live' "$INSTALL")
total=$((total + 1)); if [ "$n" = 0 ]; then pass=$((pass + 1)); echo "对    脚本里不再写死 Docker 镜像加速地址（取自 acornfox sources --daemon-mirrors）"; else echo "不对  脚本里还写着镜像加速地址 $n 处"; fi

echo "通过 $pass/$total"
[ "$pass" = "$total" ]
