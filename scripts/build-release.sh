#!/usr/bin/env bash
# 构建发布产物：dist/acornfox_<os>_<arch>[.exe] 和 SHA256SUMS，并附带安装脚本和 Caddy 安装包
# 文件名与 scripts/install/*.sh 下载的地址一致。
#
# 用法: scripts/build-release.sh vX.Y.Z
#   例: scripts/build-release.sh v0.4.0-rc.1
#
# 编哪些平台：默认 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64。
#   Linux 是服务端加命令行；Mac 只当命令行客户端用（经 SSH 管理服务器）。
#   本版本不编 Windows：Windows 用户经 SSH 在服务器上运行 acornfox。
#   要改就设 ACORNFOX_RELEASE_TARGETS（空格分隔的 os/arch），例如
#     ACORNFOX_RELEASE_TARGETS="linux/amd64" scripts/build-release.sh v0.4.0-rc.1
#     ACORNFOX_RELEASE_TARGETS="linux/amd64 windows/amd64" scripts/build-release.sh v0.4.0-test
#   windows 目标的文件名带 .exe。只认 linux、darwin、windows 和 amd64、arm64。
#
# 输出目录：默认仓库根目录下的 dist/（每次先清空）；设 ACORNFOX_RELEASE_DIST 可改到别处，
#   例如试跑时 ACORNFOX_RELEASE_DIST=$(mktemp -d)/dist。目录会被整个删掉重建，不要指向有别的文件的目录。
# 前提：go（版本见 acornfox/go.mod）、curl、sha256sum 或 shasum；要能连 GitHub 下载 Caddy。
# 验证：最后列出目录内容；SHA256SUMS 里每个 acornfox_* 一行，可用 sha256sum -c SHA256SUMS 核对。
# 出错：某个平台编不过会立刻停下；Caddy 下载或 SHA-512 校验失败会停下并说明是哪个文件。

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
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum acornfox_* > SHA256SUMS
else
    shasum -a 256 acornfox_* > SHA256SUMS
fi
cp "$ROOT/scripts/install/install.sh" "$ROOT/scripts/install/upgrade.sh" "$ROOT/scripts/install/uninstall.sh" "$ROOT/scripts/install/install-cli.sh" "$DIST/"

# install.sh 在中国大陆先从 Gitee 发行版取 Caddy，所以随每个版本附带同一份官方安装包、
# 校验文件与许可证（Apache-2.0）。版本号以 install.sh 为准，下载后按官方 SHA-512 校验。
CADDY_VERSION=$(sed -n 's/^ *CADDY_VERSION="\([0-9.]*\)"$/\1/p' "$ROOT/scripts/install/install.sh" | head -1)
[ -n "$CADDY_VERSION" ] || { echo "无法从 install.sh 读取 CADDY_VERSION" >&2; exit 1; }
CADDY_URL="https://github.com/caddyserver/caddy/releases/download/v${CADDY_VERSION}"
echo "下载 Caddy ${CADDY_VERSION}"
curl -fsSL -o "caddy_${CADDY_VERSION}_checksums.txt" "${CADDY_URL}/caddy_${CADDY_VERSION}_checksums.txt"
curl -fsSL -o caddy_LICENSE "https://raw.githubusercontent.com/caddyserver/caddy/v${CADDY_VERSION}/LICENSE"
for arch in amd64 arm64; do
    f="caddy_${CADDY_VERSION}_linux_${arch}.tar.gz"
    curl -fsSL -o "$f" "${CADDY_URL}/${f}"
    want=$(awk -v a="$f" '$2 == a {print $1; exit}' "caddy_${CADDY_VERSION}_checksums.txt")
    if command -v sha512sum >/dev/null 2>&1; then
        got=$(sha512sum "$f" | awk '{print $1}')
    else
        got=$(shasum -a 512 "$f" | awk '{print $1}')
    fi
    [ -n "$want" ] && [ "$got" = "$want" ] || { echo "$f SHA-512 校验失败" >&2; exit 1; }
done

echo "完成：$DIST"
ls -l "$DIST"
