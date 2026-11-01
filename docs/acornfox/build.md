# 构建与发布

本文说明怎样从源码构建 AcornFox，以及发行版里有哪些文件。服务器端依赖的 Docker 与 Caddy 从哪里来，见 [runtime-build.md](runtime-build.md)。

## 产物是什么

AcornFox 只有一个程序 `acornfox`，服务端（`acornfox server`、`acornfox runner`）和用户电脑上的 CLI 是同一个文件。网页控制台的页面、字体、图标通过 `go:embed` 编进程序（`acornfox/internal/console/static/`），没有单独的前端构建步骤。

程序纯 Go 编写，`CGO_ENABLED=0` 构建；SQLite 用的是纯 Go 实现 `modernc.org/sqlite`，不依赖系统库。

## 本地构建

需要 Go，版本与 `acornfox/go.mod` 中的 `go` 一行一致（目前是 1.25.13）。

    cd acornfox
    go build ./...
    go vet ./...
    TMPDIR=/tmp go test ./...           # macOS 需要 TMPDIR=/tmp（Unix socket 路径长度限制）
    node --test internal/console/*.cjs  # 控制台脚本的测试

`internal/runner` 的 socket 测试依赖 Linux 的对端凭据校验，只能在 Linux 上通过；发版前要在 Linux 上跑一次全量测试。

只想得到当前平台的程序：

    go build -o acornfox ./cmd/acornfox

## 发布产物

    scripts/build-release.sh v0.3.0

脚本在仓库根目录的 `dist/` 下生成：

| 文件 | 说明 |
| --- | --- |
| `acornfox_<os>_<arch>[.exe]` | linux、darwin、windows 各 amd64 与 arm64，共 6 个；`-trimpath -s -w`，版本号经 `-ldflags` 写入 `internal/cli.Version` |
| `SHA256SUMS` | 上面 6 个程序的 SHA-256，安装脚本下载后据此校验 |
| `install.sh`、`upgrade.sh`、`install-cli.sh` | 从 `scripts/install/` 原样复制 |
| `caddy_<版本>_linux_{amd64,arm64}.tar.gz`、`caddy_<版本>_checksums.txt`、`caddy_LICENSE` | 随附的 Caddy 官方安装包，见 [runtime-build.md](runtime-build.md) |

## 安装脚本怎样找到版本

`install.sh`、`upgrade.sh`、`install-cli.sh` 不指定 `--version` 时取最新发行版：中国大陆查 Gitee 的 `releases/latest`，其他地区查 GitHub。下载地址固定为 `<发行版>/acornfox_<os>_<arch>`，所以 `dist/` 中的文件名不能改。
