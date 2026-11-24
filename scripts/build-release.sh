#!/usr/bin/env bash
# 构建发布产物：dist/acornfox_<os>_<arch>[.exe] 和 SHA256SUMS，并附带引导脚本 get.sh、get.ps1（Windows）、打了版本戳的安装脚本（含 install-windows.ps1）、
# 入口镜像包 acornfox-edge_<入口版本>_linux_<amd64|arm64>.tar.gz（里面带上游 Caddy，上游安装包本身不再单独附带）
# 文件名与 scripts/install/*.sh 下载的地址一致。
#
# 用法: scripts/build-release.sh vX.Y.Z
#   例: scripts/build-release.sh v0.4.0-rc.1
#
# 编哪些平台：默认 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64。
#   Linux 是服务端加命令行；Mac 的程序（darwin/arm64）既是命令行，也是 Mac 本机安装用的 acornfox mac …（装进苹果 container 开的虚拟机，
#   虚拟机里是 linux/arm64 的程序，见 install-mac.sh）。
#   不编 Windows 的 exe：Windows 上的 AcornFox 装在 WSL 里（get.ps1 + install-windows.ps1），命令行经 wsl 调用。
#   要改就设 ACORNFOX_RELEASE_TARGETS（空格分隔的 os/arch），例如
#     ACORNFOX_RELEASE_TARGETS="linux/amd64" scripts/build-release.sh v0.4.0-rc.1
#     ACORNFOX_RELEASE_TARGETS="linux/amd64 windows/amd64" scripts/build-release.sh v0.4.0-test
#   windows 目标的文件名带 .exe。只认 linux、darwin、windows 和 amd64、arm64。
#
# 输出目录：默认仓库根目录下的 dist/（每次先清空）；设 ACORNFOX_RELEASE_DIST 可改到别处，
#   例如试跑时 ACORNFOX_RELEASE_DIST=$(mktemp -d)/dist。目录会被整个删掉重建，不要指向有别的文件的目录。
# 前提：go（版本见 acornfox/go.mod）、curl、tar、sha256sum 或 shasum；要能连 GitHub 下载 Caddy；
#   构建机上要有 CA 证书包（Debian、Ubuntu 的 /etc/ssl/certs/ca-certificates.crt，或用 ACORNFOX_EDGE_CA_BUNDLE 指定）。
# 验证：最后列出目录内容；SHA256SUMS 覆盖目录里的全部文件，可用 sha256sum -c SHA256SUMS 核对；
#   install.sh、upgrade.sh、install-cli.sh 里的 SCRIPT_VERSION 是这次的版本号（不是 dev）。
# 出错：某个平台编不过会立刻停下；Caddy 下载或 SHA-512 校验失败会停下并说明是哪个文件；
#   入口镜像包里缺文件会停下并说缺哪个。验证入口镜像包：docker import acornfox-edge_*_linux_amd64.tar.gz acornfox/edge:<入口版本>
#   后 docker run --rm --entrypoint /usr/bin/afx-edge acornfox/edge:<入口版本> version 应打印入口版本。

set -euo pipefail

VERSION="${1:?用法: $0 vX.Y.Z}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="${ACORNFOX_RELEASE_DIST:-$ROOT/dist}"
TARGETS="${ACORNFOX_RELEASE_TARGETS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64}"
for target in $TARGETS; do
    case "$target" in
        linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64|windows/arm64) ;;
        *) echo "ACORNFOX_RELEASE_TARGETS 里有不认识的平台：$target（写成 os/arch，例如 linux/amd64）" >&2; exit 1 ;;
    esac
done
[ -n "${TARGETS// /}" ] || { echo "ACORNFOX_RELEASE_TARGETS 是空的" >&2; exit 1; }
case "$DIST" in
    ""|/|"$HOME"|"$ROOT") echo "ACORNFOX_RELEASE_DIST 不能是 ${DIST:-空}（会被整个删掉）" >&2; exit 1 ;;
esac
LDFLAGS="-s -w -X github.com/acornfox/acornfox/internal/cli.Version=${VERSION#v}"

rm -rf "$DIST"
mkdir -p "$DIST"

cd "$ROOT/acornfox"
for target in $TARGETS; do
    os="${target%/*}"
    arch="${target#*/}"
    out="$DIST/acornfox_${os}_${arch}"
    [ "$os" = "windows" ] && out="${out}.exe"
    echo "构建 ${os}/${arch}"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "$LDFLAGS" -o "$out" ./cmd/acornfox
done

cd "$DIST"
cp "$ROOT/scripts/install/install.sh" "$ROOT/scripts/install/upgrade.sh" "$ROOT/scripts/install/uninstall.sh" "$ROOT/scripts/install/install-cli.sh" "$ROOT/scripts/install/install-mac.sh" "$DIST/"
# 引导脚本 get.sh 是用户的一行命令读的入口（海外读“最新正式版”的这个附件）。它不含版本，原样带上。
cp "$ROOT/get.sh" "$DIST/"
# Windows：引导脚本 get.ps1（不含版本，原样带上）和打了版本戳、拼进下载网关 PowerShell 客户端的 install-windows.ps1。
# 两个都是带 BOM 的 UTF-8（英文 Windows 的 PowerShell 5.1 读无 BOM 的中文会解析错）；Windows 上不发 exe。
cp "$ROOT/get.ps1" "$DIST/"
bash "$ROOT/scripts/install/windows/assemble.sh" "$DIST/install-windows.ps1" "$VERSION" >/dev/null
for f in get.ps1 install-windows.ps1; do
    [ "$(head -c 3 "$DIST/$f" | od -An -tx1 | tr -d ' \n')" = "efbbbf" ] || { echo "$f 没有 BOM（要带 BOM 的 UTF-8）" >&2; exit 1; }
done

# 给安装、升级、装命令行、Mac 本机安装的脚本打版本戳：发行版里的脚本只处理这个版本的程序（源码里是 dev，不能直接用）。
# 每个脚本里必须恰好有一行 SCRIPT_VERSION="dev"，改不到就停下，不发没有戳的脚本。
for f in install.sh upgrade.sh install-cli.sh install-mac.sh; do
    n=$(grep -c '^SCRIPT_VERSION="dev"$' "$f" || true)
    [ "$n" = 1 ] || { echo "$f 里要恰好有一行 SCRIPT_VERSION=\"dev\"，现在有 ${n} 行，没法打版本戳" >&2; exit 1; }
    sed "s/^SCRIPT_VERSION=\"dev\"\$/SCRIPT_VERSION=\"${VERSION}\"/" "$f" > "$f.tmp" && mv "$f.tmp" "$f"
    chmod 0755 "$f"
    grep -q "^SCRIPT_VERSION=\"${VERSION}\"\$" "$f" || { echo "$f 的版本戳没写进去" >&2; exit 1; }
done
chmod 0755 uninstall.sh get.sh

# 入口镜像包 acornfox-edge_<入口版本>_linux_<架构>.tar.gz：一份根文件系统，安装、升级脚本用
# docker import 导入成 acornfox/edge:<入口版本>，主程序只从本机这个镜像建入口容器（不从镜像仓库拉）。
# 里面是上游原版 Caddy、适配程序 afx-edge（容器的 1 号进程，管 Caddy）、CA 证书包和许可证。
# 入口版本“Caddy 版本-适配版本”只在 acornfox/internal/edge/layout.go 的 ImageVersion 一处写；
# Caddy 版本取它“-”前面那段。
EDGE_VERSION=$(sed -n 's/^[[:space:]]*ImageVersion[[:space:]]*=[[:space:]]*"\([0-9.]*-[0-9]*\)"$/\1/p' "$ROOT/acornfox/internal/edge/layout.go" | head -1)
[ -n "$EDGE_VERSION" ] || { echo "无法从 acornfox/internal/edge/layout.go 读取 ImageVersion" >&2; exit 1; }
CADDY_VERSION="${EDGE_VERSION%-*}"

# 上游 Caddy 安装包的 SHA-512：官方 caddy_<版本>_checksums.txt 里的值，写死在这里，与下载来源无关。
# 换 Caddy 版本时改 layout.go 的 ImageVersion，并在这里加上新版本两个架构的值。
caddy_sha512() {
    case "$1" in
        2.8.4_amd64) echo "b8bec15d14fb033562af9f207850027bcbaa1f891edc9efe00d38bf39e1bf9944f8b6b8eba041ddd4c171cd70c905174c704d705be2f23bc678fe1eaf37a2485" ;;
        2.8.4_arm64) echo "5466234be3e988071cef937aedbdd94c15b6f75cf7307397e67c2641219ac9bfe2c2bb3b31fc05bc68d3b6398bbe50abfa16ccf3b127318ccac31115ad26507c" ;;
        *) return 1 ;;
    esac
}
sha512_of() {
    if command -v sha512sum >/dev/null 2>&1; then sha512sum "$1" | awk '{print $1}'; else shasum -a 512 "$1" | awk '{print $1}'; fi
}
# 容器里 Caddy 访问 ACME 等要用的 CA 证书包：默认取构建机自己的；可用 ACORNFOX_EDGE_CA_BUNDLE 指定。
CA_BUNDLE="${ACORNFOX_EDGE_CA_BUNDLE:-}"
if [ -z "$CA_BUNDLE" ]; then
    for f in /etc/ssl/certs/ca-certificates.crt /etc/pki/tls/certs/ca-bundle.crt /etc/ssl/cert.pem; do
        [ -s "$f" ] && { CA_BUNDLE="$f"; break; }
    done
fi
[ -n "$CA_BUNDLE" ] && [ -s "$CA_BUNDLE" ] || { echo "找不到 CA 证书包；设 ACORNFOX_EDGE_CA_BUNDLE 指向一个 PEM 文件" >&2; exit 1; }
# 打包时文件属主一律写成 root（0:0），GNU tar 和 macOS 自带的 bsdtar 写法不同
if tar --version 2>/dev/null | grep -q 'GNU tar'; then
    TAR_ROOT=(--numeric-owner --owner=0 --group=0 --sort=name)
else
    TAR_ROOT=(--uid 0 --gid 0 --uname root --gname root)
fi

CADDY_URL="https://github.com/caddyserver/caddy/releases/download/v${CADDY_VERSION}"
EDGE_WORK=$(mktemp -d)
trap 'rm -rf "$EDGE_WORK"' EXIT
echo "下载 Caddy ${CADDY_VERSION}（只放进入口镜像包，不单独附带）"
curl -fsSL -o "$EDGE_WORK/caddy_LICENSE" "https://raw.githubusercontent.com/caddyserver/caddy/v${CADDY_VERSION}/LICENSE"
umask 022
for arch in amd64 arm64; do
    f="$EDGE_WORK/caddy_${CADDY_VERSION}_linux_${arch}.tar.gz"
    want=$(caddy_sha512 "${CADDY_VERSION}_${arch}") || { echo "build-release.sh 里没有 Caddy ${CADDY_VERSION} ${arch} 的 SHA-512，先补上" >&2; exit 1; }
    curl -fsSL -o "$f" "${CADDY_URL}/$(basename "$f")"
    [ "$(sha512_of "$f")" = "$want" ] || { echo "$(basename "$f") SHA-512 校验失败" >&2; exit 1; }

    echo "打包入口镜像 ${EDGE_VERSION} linux/${arch}"
    root="$EDGE_WORK/root-${arch}"
    mkdir -p "$root/usr/bin" "$root/etc/ssl/certs" "$root/etc/afx-edge" "$root/tmp" "$root/data" "$root/config" \
        "$root/usr/share/licenses/caddy" "$root/usr/share/licenses/afx-edge"
    chmod 1777 "$root/tmp"
    chmod 0700 "$root/etc/afx-edge"
    tar -xzf "$f" -C "$EDGE_WORK" caddy
    install -m 0755 "$EDGE_WORK/caddy" "$root/usr/bin/caddy"
    rm -f "$EDGE_WORK/caddy"
    (cd "$ROOT/acornfox" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "-s -w" -o "$root/usr/bin/afx-edge" ./cmd/afx-edge)
    install -m 0644 "$CA_BUNDLE" "$root/etc/ssl/certs/ca-certificates.crt"
    install -m 0644 "$EDGE_WORK/caddy_LICENSE" "$root/usr/share/licenses/caddy/LICENSE"
    install -m 0644 "$ROOT/LICENSE" "$root/usr/share/licenses/afx-edge/LICENSE"
    pkg="acornfox-edge_${EDGE_VERSION}_linux_${arch}.tar.gz"
    tar "${TAR_ROOT[@]}" -C "$root" -czf "$pkg" .
    # 先把清单存下来再查（grep -q 提前退出会让管道里的 tar 报错，配 pipefail 就误判成缺文件）
    tar -tzf "$pkg" > "$EDGE_WORK/list-${arch}"
    for want_path in ./usr/bin/caddy ./usr/bin/afx-edge ./etc/ssl/certs/ca-certificates.crt ./tmp/; do
        grep -qx "$want_path" "$EDGE_WORK/list-${arch}" || { echo "$pkg 里缺 $want_path" >&2; exit 1; }
    done
done

# 校验文件覆盖发行版里的全部文件（以前只有程序文件）：get.sh 下载脚本后先按它校验再执行。
# 放在最后生成，入口镜像包也算进去。
rm -f SHA256SUMS
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum $(ls | grep -v '^SHA256SUMS$') > SHA256SUMS
else
    shasum -a 256 $(ls | grep -v '^SHA256SUMS$') > SHA256SUMS
fi

echo "完成：$DIST"
ls -l "$DIST"
