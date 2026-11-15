# 内置目录快照

这个目录会被编进 acornfox 程序（`go:embed`），没网时应用中心用它浏览目录。

- 内容：`version.txt`、`index.json`、`apps/<id>/<version>.tar.gz`，和发布出去的格式一样，只是没有 `index.json.sig`：快照是程序的一部分，不另外验签。
- **现在是一份没有应用的空目录**：只有服务类型定义（`llm_gateway`、`mysql`、`postgres`、`redis`、`smtp`，来自 `internal/catalog/snapshotsrc/`），序号 4。原因：
  - 这一版平台只装第 3 版格式的清单，线上序号 3 的目录还是第 2 版的，装不了；按第 3 版重新上架的应用还没有发布。
  - 序号 4 比线上序号 3 大，线上那份旧目录就不会被同步下来盖掉它；应用中心显示“还没有可以安装的应用”，不报错。
- **正式发版前必须重新生成**：用按第 3 版重新上架后的目录（目录仓库）打包，那次发布的序号必须大于 4（否则已经装了这一版的机器不会同步它）：
  `go run ./gensnapshot -src <目录仓库> -serial <序号> -out snapshot`（在 `acornfox/internal/catalog/` 下运行）
- 怎么生成现在这份：在 `acornfox/internal/catalog/` 下运行 `go generate ./`（等于 `go run ./gensnapshot -src snapshotsrc -serial 4 -out snapshot`）。
- 生成器只替换 `version.txt`、`index.json`、`index.json.sig` 和 `apps/`，不动本说明。打包会先校验目录，有错误就停下，快照不变。
- 怎么验证：`go test ./internal/catalog/ -run TestSnapshot`（快照能读、序号和服务类型对、每个包的 sha256 和 index.json 一致）。
- 客户端读目录时，缓存序号不比快照小就用缓存，否则用快照（升级程序后快照可能更新）。
