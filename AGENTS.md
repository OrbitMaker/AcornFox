# AcornFox 开发约定

给参与开发的人和 AI 编码工具看（Claude Code 通过 `CLAUDE.md` 导入本文件，Codex 直接读取）。

## 代码约定

- Go 模块位于 `acornfox/`，所有 go 命令都在该目录执行。
- API 路由在 `internal/apiserver/apiserver.go` 注册，CLI 命令在 `internal/cli/`。
- 数据库结构只能通过在 `internal/state/migrations.go` 末尾追加新迁移来修改；已发布的迁移有校验和保护，不能改动。
- 修改 `Store`、`runner.API`、`client.API` 等接口时，同步补齐所有测试替身（`*_test.go` 中的 fake），并用 `go vet ./...` 确认测试能编译。
- 仅供测试使用的代码，文件名必须以 `_test.go` 结尾，否则会被编进正式程序。

## 验证

改动完成后在 `acornfox/` 执行：

    go build ./...
    go vet ./...
    go test ./...

在 macOS 上请用 `TMPDIR=/tmp go test ./...`，否则 `cmd/acornfox`、`internal/caddyroute` 会因 Unix socket 路径过长而失败（`bind: invalid argument`）。`internal/runner` 的 socket 测试依赖只在 Linux 上实现的对端凭据校验（`internal/peer`），在 macOS 上会报 `runner: unavailable`，以 Linux 开发机的结果为准。
