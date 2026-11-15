# 测试材料：数据库这类服务的写法

- `service-types/demo_sql.yaml`：一种样例数据库服务（地址、端口、库名、账号、密码、连接串）。
- `apps/demo-sql`：提供它的一方。主入口是 `tcp`，健康检查用 `exec` 和 `tcp: true`，访问方式 `internal`；`provides` 用 `issue_vars`（库名、账号按 `issue.id` 起名，密码由平台生成）、`exec` 步骤（`env`、`stdin`、`expect_exit`、`save` 的 JSON 和整段标准输出两种来源、收回里的 `finish_if.exit`），`values` 里的连接串是引用 `issued.*` 的模板并标了保密。
- `apps/demo-sql-user`：使用它的一方。`uses` 写类型名，环境变量逐项引用，`init` 里有一个 `exec` 步骤。

都不是真的应用，命令、镜像摘要都是假的，只用来测清单格式、检查工具、变量报告和步骤的执行。这里用到的写法平台都已经执行，`catalog lint` 没有提醒：`exec` 步骤、`issue_vars`、引用 `issued.*` 的值模板由步骤执行器执行（`internal/appinstall/exec_flow_test.go` 用一个假的数据库把发凭据、核对、收回走一遍），`tcp` 入口、`health` 的 `exec` 和 `tcp`、`internal` 见 `internal/appinstall/internalnet.go`。测试会复制这棵目录再改坏其中一处。
