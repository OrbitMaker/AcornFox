#!/usr/bin/env bash
# uninstall.sh 的不动系统测试：没有终端、又没加 -y 时，要说清楚加 -y 并报 UNINSTALL-TTY-01，什么都不改。
#
# 做什么：在临时目录放假的 id（假装是 root）、systemctl、docker、rm、userdel 等命令，用 setsid 在没有
#   控制终端的会话里运行 uninstall.sh（和经 ssh 执行、不分配终端时一样），核对退出码、输出和有没有
#   碰到这些命令；另查加了 -y 时会往下走（只调假命令）。
# 什么时候用：改了 uninstall.sh 的确认逻辑之后。
# 前提条件：Linux（要 setsid，util-linux 自带）、bash。不需要 root，不联网。
# 怎么运行：bash scripts/install/uninstall.test.sh
# 怎么验证结果：最后一行“通过 N/N”，退出码 0；不对的项打印“不对”和实际输出。
# 副作用：只在 mktemp 建的临时目录里写文件，退出时删掉。假命令放在 PATH 最前面，真的 systemctl、
#   docker、rm 不会被调用；脚本里写死的 /usr/local/bin/acornfox 只在测试里被假 rm 记下，不会删。
set -uo pipefail
HERE=$(cd "$(dirname "$0")" && pwd)
SCRIPT="$HERE/uninstall.sh"
command -v setsid >/dev/null || { echo "要 setsid（Linux util-linux）"; exit 2; }
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/bin"
for c in systemctl docker rm userdel groupdel getent; do
    printf '#!/bin/sh\necho "%s $*" >> "%s/calls"\nexit 0\n' "$c" "$T" > "$T/bin/$c"
done
printf '#!/bin/sh\n[ "$1" = "-u" ] && { echo 0; exit 0; }\nexit 1\n' > "$T/bin/id"
chmod +x "$T/bin/"*
pass=0 total=0
check() { # 名称 条件结果(0/1) 输出
    total=$((total + 1))
    if [ "$2" = 0 ]; then pass=$((pass + 1)); else echo "不对：$1"; echo "$3" | sed 's/^/    /'; fi
}

# 1. 没有终端、不加 -y：报编号退出 1，不调任何命令
: > "$T/calls"
out=$(PATH="$T/bin:$PATH" setsid -w bash "$SCRIPT" </dev/null 2>&1); rc=$?
ok=1; [ "$rc" = 1 ] && echo "$out" | grep -q "故障编号: UNINSTALL-TTY-01" && echo "$out" | grep -q "sudo bash uninstall.sh -y" \
    && echo "$out" | grep -q "默认保留数据" && ! echo "$out" | grep -q "/dev/tty: No such" && [ ! -s "$T/calls" ] && ok=0
check "没有终端时说明要加 -y（rc=$rc）" $ok "$out"

# 1b. 只看标准输出（例如 2>/dev/null，或工具只采集标准输出）也拿得到处理办法和编号，最后一行是编号
out=$(PATH="$T/bin:$PATH" setsid -w bash "$SCRIPT" </dev/null 2>/dev/null); rc=$?
ok=1; [ "$rc" = 1 ] && [ "$(echo "$out" | tail -n 1)" = "故障编号: UNINSTALL-TTY-01" ] && echo "$out" | grep -q "sudo bash uninstall.sh -y" && ok=0
check "处理办法和编号在标准输出（rc=$rc）" $ok "$out"

# 2. --purge 时说明会删数据、命令带 --purge -y
: > "$T/calls"
out=$(PATH="$T/bin:$PATH" setsid -w bash "$SCRIPT" --purge </dev/null 2>&1); rc=$?
ok=1; [ "$rc" = 1 ] && echo "$out" | grep -q "UNINSTALL-TTY-01" && echo "$out" | grep -q "uninstall.sh --purge -y" \
    && echo "$out" | grep -q "不可恢复" && [ ! -s "$T/calls" ] && ok=0
check "--purge 没有终端时说明会删数据（rc=$rc）" $ok "$out"

# 3. 加 -y：不问，往下走（只调了假命令）
: > "$T/calls"
out=$(PATH="$T/bin:$PATH" setsid -w bash "$SCRIPT" -y </dev/null 2>&1); rc=$?
ok=1; [ "$rc" = 0 ] && echo "$out" | grep -q "卸载完成" && grep -q "^systemctl disable --now acornfox-server" "$T/calls" \
    && ! echo "$out" | grep -q "UNINSTALL-TTY-01" && ok=0
check "加 -y 时照常卸载（rc=$rc）" $ok "$out"

# 4. -h 里写了这个编号
out=$(bash "$SCRIPT" -h 2>&1)
ok=1; echo "$out" | grep -q "UNINSTALL-TTY-01" && ok=0
check "帮助里有故障编号" $ok "$out"

echo "通过 $pass/$total"
[ "$pass" = "$total" ]
