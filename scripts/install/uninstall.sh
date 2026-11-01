#!/usr/bin/env bash
# AcornFox 卸载脚本（Linux 服务器）
#
# 做什么：停止并删除 acornfox-server、acornfox-runner 服务，删除 AcornFox 创建的容器
#         （应用、附加服务、Caddy 容器 acornfox-caddy）和网络，删除 /usr/local/bin/acornfox。
#         默认保留数据：/var/lib/acornfox、数据卷、镜像，重新安装后应用和数据库还在。
# 用法：  sudo bash uninstall.sh            卸载，保留数据
#         sudo bash uninstall.sh --purge    连同数据卷、数据目录、镜像、系统账号一起删除（不可恢复）
#         加 -y 不再询问确认。
# 不动：  Docker 本身、v0.3.x 留在宿主机上的 Caddy 程序文件（/usr/bin/caddy，已停用）。
# 验证：  systemctl status acornfox-server 显示 not found；docker ps -a --filter label=acornfox.managed=1 为空
# 许可：  AGPL-3.0
set -euo pipefail

PURGE=false
ASSUME_YES=false
for a in "$@"; do
    case "$a" in
        --purge) PURGE=true ;;
        -y|--yes) ASSUME_YES=true ;;
        -h|--help) sed -n '2,13p' "$0"; exit 0 ;;
        *) echo "未知参数: $a（可用：--purge、-y）" >&2; exit 2 ;;
    esac
done
[ "$(id -u)" = 0 ] || { echo "请用 root 运行：sudo bash uninstall.sh" >&2; exit 1; }

if ! $ASSUME_YES; then
    if $PURGE; then
        echo "将删除 AcornFox 及其全部应用数据（数据卷、/var/lib/acornfox、镜像），不可恢复。"
    else
        echo "将卸载 AcornFox，保留 /var/lib/acornfox 和数据卷。"
    fi
    read -r -p "输入 yes 继续: " ans < /dev/tty
    [ "$ans" = "yes" ] || { echo "已取消"; exit 1; }
fi

echo "[1/4] 停止服务"
systemctl disable --now acornfox-server acornfox-runner >/dev/null 2>&1 || true
rm -f /etc/systemd/system/acornfox-server.service /etc/systemd/system/acornfox-runner.service
systemctl daemon-reload

echo "[2/4] 删除容器和网络"
if command -v docker >/dev/null 2>&1; then
    ids=$( { docker ps -aq --filter label=acornfox.managed=1; docker ps -aq --filter label=acornfox.edge=1; } | sort -u)
    [ -n "$ids" ] && docker rm -f $ids >/dev/null
    nets=$( { docker network ls -q --filter label=acornfox.managed=1; docker network ls -q --filter label=acornfox.edge=1; } | sort -u)
    [ -n "$nets" ] && docker network rm $nets >/dev/null 2>&1 || true
    echo "  容器 $(echo $ids | wc -w) 个，网络 $(echo $nets | wc -w) 个"
    if $PURGE; then
        vols=$(docker volume ls -q | grep -E '^(af-|acornfox-caddy-)' || true)
        [ -n "$vols" ] && docker volume rm $vols >/dev/null
        imgs=$(docker images --format '{{.Repository}}:{{.Tag}}' | grep -E '^acornfox/' || true)
        [ -n "$imgs" ] && docker rmi -f $imgs >/dev/null 2>&1 || true
        echo "  数据卷 $(echo $vols | wc -w) 个，镜像 $(echo $imgs | wc -w) 个（附加服务用的公共镜像 postgres/mysql/redis/caddy 保留，可用 docker image prune -a 清理）"
    fi
fi

echo "[3/4] 删除程序"
rm -f /usr/local/bin/acornfox /etc/tmpfiles.d/acornfox.conf
rm -rf /run/acornfox

echo "[4/4] 数据"
if $PURGE; then
    rm -rf /var/lib/acornfox /var/lib/acornfox-exec /etc/acornfox /root/.acornfox-token
    for u in acornfox acornfox-exec; do id "$u" >/dev/null 2>&1 && userdel "$u" || true; done
    for g in acornfox-ipc acornfox-users; do getent group "$g" >/dev/null && groupdel "$g" || true; done
    echo "  已删除数据目录和系统账号"
else
    echo "  已保留 /var/lib/acornfox 和数据卷；重新运行 install.sh 后应用会恢复"
fi
echo "卸载完成"
