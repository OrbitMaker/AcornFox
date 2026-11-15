# 测试材料：清单里值的写法

只用于测试和验收，不上架。`apps/demo-values` 是一个最小的应用（busybox 的 httpd，配置文件写进 `/www`，健康检查取 `/index.html`），把值的各种写法都用了一遍；`service-types/smtp.yaml` 是它可选需要的那种服务的定义（和 `../services/` 里的一样）。

- 检查工具：`acornfox catalog lint` 这个目录 0 个错误，只有 1 条 CAT-LINT-51 提醒（`LITERAL_BRACES` 里的 `{{ $json.x }}`，故意的）。`internal/catalog` 的测试钉着这一点。
- 安装引擎：`internal/appinstall` 的测试用假的运行环境装它，核对下面每一项。
- 上机：镜像摘要是占位的全 0，上机验收前换成所用 busybox 镜像的真摘要；装好后用 `docker exec <容器> env`、`docker inspect` 看启动命令、`cat /www/<文件>` 核对。

默认设置（`realm` 是 `demo values`，`greeting` 是 `say "hi" \ back`，`flag` 是 true），没接邮箱服务时，应该得到：

| 位置 | 值 |
| --- | --- |
| 启动命令 | `httpd -f -p 8080 -h /www -r "DEMO VALUES"`（最后一项是一个参数，中间有空格） |
| `APP_URL_SLASH` | 应用地址加 `/` |
| `REGION_UPPER` | `GLOBAL`（国内网络是 `CN`） |
| `REGION_IS_CN` | `no`（国内网络是 `yes`） |
| `PIP_ONLY_IN_CN` | 没有这一条（国内网络才有，是国内的 PyPI 镜像地址） |
| `MODE` | `abroad`（国内网络是 `domestic`；同名两条，只写成立的那一条） |
| `FLAG_ON` | `on`（`flag` 设成 false 时没有这一条） |
| `GREETING_B64` | `c2F5ICJoaSIgXCBiYWNr` |
| `GREETING_URL` | `say%20%22hi%22%20%5C%20back` |
| `GREETING_SHA256` | `79f203fa9524fb6efceca47ecfd5f65c3cf03e6e1c493e4517b3fe31dd530c2d` |
| `GREETING_SHELL` | `'say "hi" \ back'` |
| `GREETING_SNAKE` | `say_"hi"_\_back` |
| `MAIL_HOST` | `none`（接了邮箱服务时是它的地址） |
| `MAIL_ONLY_WHEN_THERE` | 没有这一条（接了邮箱服务时才有） |
| `LITERAL_BRACES` | `{{params.greeting}} and {{ $json.x }}`（原样） |
| `ADMIN_DSN` | `admin:<生成的密码>@db`，保密 |
| `/www/index.html` | `<h1>demo values</h1>` |
| `/www/settings.json` | `{"greeting": "say \"hi\" \\ back", "raw": "say \"hi\" \\ back", "region": "global"}` |
| `/www/settings.yaml` | `greeting: "say \"hi\" \\ back"` 和 `raw: "say \"hi\" \\ back"` 两行 |
| `/www/cn-only.txt` | 没有这个文件（国内网络才有） |
| `/www/mail.txt` | 没有这个文件（接了邮箱服务时才有） |
| 首次登录“地址” | 应用地址加 `/index.html` |
