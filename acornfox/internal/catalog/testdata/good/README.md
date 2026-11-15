# 测试材料：按第 3 版清单格式写的目录

- `apps/new-api`：初始化（建管理员、关自助注册）和给别的应用发令牌、收回、核对，都写成清单里的步骤，平台没有为它专门写的代码。
- `apps/hermes-agent`：需要一种服务（`uses`，类型 `llm_gateway`），配置文件里引用 `uses.llm.*`。
- `recipes/`：两个应用各自单独装的配方，和两个一起装、把 New API 接给 Hermes 的配方。
- `service-types/llm_gateway.yaml`：大模型接口的服务类型定义。

这两份清单和目录仓库里的草稿逐字节相同，都还没有重新上架。catalog、appinstall、cli 等包的测试从这里打包目录；`internal/appinstall` 里的实测测试（文件名以 `_real_test.go` 结尾，设了 `AFX_REAL_NEWAPI` 才运行）读 `apps/new-api` 里的步骤，对着真的 New API 镜像执行一遍。
