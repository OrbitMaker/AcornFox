# 内置目录快照

这个目录会被编进 acornfox 程序（`go:embed`），没网时应用中心用它浏览目录。

- 内容：`version.txt`、`index.json`、`apps/<id>/<version>.tar.gz`，和发布出去的格式一样，只是没有 `index.json.sig`：快照是程序的一部分，不另外验签。
- 怎么生成：在 `acornfox/` 下运行 `go generate ./internal/catalog/`（默认从 `internal/catalog/testdata/good` 打包，序号 1）。正式发版前改成从目录仓库打包、用当时的发布序号：
  `go run ./internal/catalog/gensnapshot -src <目录仓库> -serial <序号> -out internal/catalog/snapshot`
- 生成器只替换 `version.txt`、`index.json`、`index.json.sig` 和 `apps/`，不动本说明。打包会先校验目录，有错误就停下，快照不变。
- 怎么验证：`go test ./internal/catalog/ -run TestSnapshot`（快照能读、每个包的 sha256 和 index.json 一致）。
- 客户端读目录时，缓存序号不比快照小就用缓存，否则用快照（升级程序后快照可能更新）。
