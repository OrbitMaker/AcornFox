# 测试材料：只给一个使用方用的数据库服务

- `service-types/demo_sql.yaml`：和 `../sqlsvc` 里的同一种样例数据库服务。
- `apps/demo-solo-sql`：提供它的一方。`provides` 写了 `max_consumers: 1`（只能给一个使用方用）、`purge`（使用方连数据一起删时，在 `revoke` 之后删掉它的库，带 `finish_if`）、`on_check_fail: stop`（接旧数据重装时旧凭据核对不过就停下，不自动换；`check` 像使用方那样连 `self.host`，不连 127.0.0.1）、`ready`（就绪检查：只读的 `exec` 步骤，引用 `params`、`self.host`、`saved`，不过时原样给用户看 `say`）；主入口的健康检查用 `exec` 加 `expect_stdout`（输出去掉首尾空白后要等于它）。
- `apps/demo-solo-sql-user`：使用它的一方，`uses` 写了 `dedicated: true`（只接没有别的使用方在用的服务）。

都不是真的应用，命令、镜像摘要都是假的，只用来测清单格式、检查工具、变量报告和安装流程（`internal/appinstall` 的测试用假的运行器照这些命令走一遍）。这些写法平台都已执行：`max_consumers`、`dedicated` 在匹配和写接入记录时管，`purge` 在使用方连数据一起删时跑，`on_check_fail: stop` 在核对不过时让安装停下，`ready` 在看服务、看计划、装完、doctor 时现跑；健康检查的 `exec` 加 `expect_stdout` 也执行。测试会复制这棵目录再改坏其中一处。
