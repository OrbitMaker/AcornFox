# 测试材料：服务的新写法

- `service-types/`：三种服务类型定义（大模型接口、邮箱、只为测试的样例令牌）。
- `apps/demo-provider`：按新写法提供服务（`provides` 里写 `values` 和 `issue`、`revoke`、`check` 步骤；`init` 步骤带 `save`、`headers`）。
- `apps/demo-consumer`：按新写法需要服务（`uses` 写类型名，引用写 `uses.<名>.<项>`；可选的那条写了 `without`）。

都不是真的应用，镜像摘要是假的，只用来测清单格式、检查工具和打包。测试会复制这棵目录再改坏其中一处。
