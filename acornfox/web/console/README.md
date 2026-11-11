# 网页控制台（新版）

`acornfox` 自带的网页控制台：React 18 + Vite + TypeScript + Tailwind，组件照 shadcn/ui 的写法（改过的文件在 `src/components/ui/`，许可证在同目录 `LICENSE`）。页面样子、文案、颜色以控制台样稿为准，主色用“中性”，深浅色都有。

构建产物写到 `../../internal/console/ui/`，**提交进仓库**，由 `internal/console` 用 `go:embed` 嵌进程序，所以编译 `acornfox` 不需要 Node。旧控制台已经去掉，访问 `/legacy/` 会跳回 `/`。

## 这一版有什么

| 页面 | 状态 | 用到的接口 |
| --- | --- | --- |
| 外壳：左侧菜单、面包屑、⌘K 命令面板、深浅色、手机宽度下的抽屉菜单（云版上线前不显示“AFX 云”入口） | 完成 | `GET /v1/console/session`、`GET /v1/status`、`POST /v1/console/logout` |
| 首页看板：可编辑网格（拖动排序、改宽高、前移后移、移除、添加图表、每张图换图表方式），布局存在这个浏览器里（`localStorage` 的 `afx-dash-v1`） | 完成 | `GET /v1/metrics/host`、`GET /v1/metrics/host/history?range=1h\|24h`、`GET /v1/metrics/apps/{app}`、`GET /v1/runtime/checks`、`GET /v1/apps/{app}/deployments?limit=5`，加共享的应用、安装任务、资源 |
| 应用中心：配方卡片、分类、搜索、应用卡片（真图标）、详情页、已断开提示；“连接私有目录”和来源标签这一版藏起（`src/lib/features.ts` 的 `PRIVATE_CATALOG_UI`，代码保留） | 完成 | `GET /v1/catalog`、`GET /v1/catalog/apps/{id}[?catalog=]`、`POST /v1/catalog/sync`、`POST /v1/catalog/connect`、`GET /v1/resources` |
| 安装弹窗：版本、模型三选一、更多设置、内存检查、命令行写法（私有目录带 `--catalog`）；进度、失败编号、首次登录；只本机的应用不给打不开的 127.0.0.1 链接，改成“怎么打开”（服务器在局域网、应用有自己的登录时可确认后改成局域网；总是给 `acornfox open 应用名`）；用本机 New API 的提示令牌不限额 | 完成 | `POST /v1/catalog/install`、`GET /v1/catalog/install[/{task}]`、`GET /v1/apps/{app}/first-login[?reveal=true]` |
| 我的应用：卡片和表格、筛选、搜索、只本机的标“只本机”；详情七个标签页（概览里有只本机应用的“怎么打开”、不限额令牌的提示）；来自私有目录的标目录名，目录断开的标出来 | 完成 | `/v1/apps…`、`/v1/metrics/apps/{app}`、`/v1/deployments/{id}/logs` |
| 运行环境：后台服务、Docker、网关、磁盘、端口的自检，每项带故障编号、原因和怎么办 | 完成 | `GET /v1/runtime/checks`、`GET /v1/setup/faults` |
| 设置：通用、访问、运行环境、存储、下载源、升级、开发者与 AI | 完成 | `GET /v1/lan-console`、`GET /v1/public-console`（控制台入口只读）、`GET /v1/runtime/checks`、`GET /v1/metrics/host`、`GET /v1/status`、`GET /v1/catalog`、`POST /v1/catalog/connect` |

### 首页看板能选的图表

数据只用服务端真有的，图表方式只列适合这类数据的（`src/lib/dashboard.ts` 的 `SOURCES`、`typesFor`）：

| 分组 | 图表 | 数据 | 时间 |
| --- | --- | --- | --- |
| 这台电脑 | CPU 使用率、内存 | `GET /v1/metrics/host/history`：后台服务每 30 秒记一次，**只存在内存里**，最多 24 小时，服务重启后从头记 | 近 1 小时（30 秒一个点）、近 24 小时（5 分钟平均） |
| 这台电脑 | 磁盘、已分配内存 | `GET /v1/metrics/host`、`GET /v1/resources` | 现在 |
| 应用 | 应用状态、各应用内存、各应用 CPU | `GET /v1/apps`、`GET /v1/metrics/apps/{app}`（只问在跑的应用） | 现在 |
| 提醒与记录 | 需要你处理 | 部署失败、没在运行、私有目录断开的应用；运行环境自检不正常的项；近一天失败的安装 | — |
| 提醒与记录 | 最近发生的事 | 每个应用最近 5 次部署、安装任务 | — |

样稿里有、这一版**不放进可选列表**的（没有数据来源，不做假数据）：网络流量（服务端没有记网卡流量）、AFX 磁盘占用分类（没有按“程序 / 应用数据 / 回退版本 / 缓存”统计的接口）、一周 CPU 热力图和 7 天范围（历史只存在内存里，没有落盘）、下载源测速、访问量、出错次数、在线率。服务端没有历史时（旧版本），CPU、内存曲线也不能选，时间范围标签不显示。

### 设置里没有的分区

样稿的“提醒”（桌面通知、邮件、群机器人）和“账号”没有后端支撑，这一版不显示。“访问”只显示局域网控制台、公网控制台开没开和对应的命令：开关的接口只在受信入口（服务器上的命令行）开放，控制台页面不能把自己开放给更多人。“存储”里的清理没有接口，只给查看占用的命令。

接口没有的东西不显示：
- “找专人部署”：前端只在 `GET /v1/catalog` 返回 `expert`（带 `url`，可选 `price_cny`）时显示。服务端和目录格式**目前都没有这个字段**，所以现在不会出现。
- 视频：清单里有 `delivery.video` 才出现。
- 主页、视频、找专人这几个链接只认 `http://`、`https://`，其他写法不显示；目录打包检查也会拦下（`CAT-LINT-15`）。
- 端口由服务端自动分配，弹窗里不能改。

### 要先升级 AcornFox 才能装的应用

`GET /v1/catalog` 的应用和配方、`GET /v1/catalog/apps/{id}` 的应用详情带 `needs_platform`（字符串，要升到的版本，例如 `"0.6.0"`）时，说明这个应用要更新版本的 AcornFox 才能装。空串或没有这个字段（旧服务端）就是不用升级，页面和以前一样。

| 位置 | 显示 |
| --- | --- |
| 应用卡片、配方卡片 | 一行“要先把 AcornFox 升级到 x.y.z 才能装”和“怎么升级”（进设置的“升级”分区）；“安装”“开始”不可点 |
| 应用详情页 | “安装”不可点；下面一块说明：那句话、这台服务器现在的版本、中国大陆和其他地区两条升级命令 |
| 安装弹窗（配方没有详情页，命令面板也能直接打开弹窗） | 同详情页的说明；不能提交 |
| 提交后被拒绝或失败 | 服务端的说明原样显示。错误里带 `needs_platform` 的（接口错误是 `error.needs_platform`，安装任务是 `fault.needs_platform`；`CAT-INST-08` 一定带，`CAT-INST-29` 只有“要先升级”那种才带），下面加一句“要先升级到 x.y.z”和升级命令；不带的只显示说明。页面只认这个字段，不看错误编号，也不看说明里有没有“升级”两个字 |
| 应用详情读不了（这台 AcornFox 版本不够，连应用包都读不了，接口返回 409 且带 `needs_platform`） | 详情页用目录列表里已有的基本信息（名称、简介、版本、内存、下载大小、支持的 CPU、命令），加一块说明：“要先升级到 x.y.z，才能看这个应用的完整介绍”、服务端的原话，以及和上面一样的升级说明；已经装过的照常能进“管理”。安装弹窗读不了详情时，说明原样显示，下面给升级命令 |

已经装过、正在装、这台机器用不了的应用，按各自原来的说法显示，不再提升级。升级命令只有一处（`src/lib/upgrade.ts`），设置的“升级”分区和这里共用；判断都在同一个文件里，测试见 `src/lib/upgrade.test.ts` 和 `src/app.test.tsx` 的“要先升级 AcornFox 才能装的应用”。

## 怎么构建

前提：Node 22 和 npm。在本目录（`acornfox/web/console`）执行：

```bash
npm ci          # 按 package-lock.json 装依赖
npm test        # Vitest
npm run build   # 类型检查 + 构建，产物写到 ../../internal/console/ui/，同时更新 third-party-licenses.json
```

然后把 `../../internal/console/ui/`、`third-party-licenses.json`（以及改过的 `package.json`、`package-lock.json`）一起提交。加了或升级了依赖，还要在仓库根目录跑 `python3 scripts/gen-licenses.py` 重新生成 `docs/licenses`。

加或升级依赖：`npm install 包@版本`（写死版本号），再按上面重新构建。

## 怎么验证

- `npm test`（Vitest）：数据整理（分类、来源、内存检查、安装请求和命令行写法、任务进度、环境变量改动、看板数据和布局）和页面主要路径（用假接口渲染应用中心、私有目录、安装、首次登录、我的应用、首页看板、运行环境、设置）。
- `go test ./internal/console/`：产物齐全、只引用同源文件、没有内联脚本和样式、没用 `eval`；**`build-info.json` 里的源码哈希和现在的源码对得上**。改了源码忘记重新构建，这个测试会失败。哈希规则见 `scripts/source-hash.mjs`，参与的文件见 `build-inputs.json`（测试文件不算）。
- `go test ./internal/apiserver/ -run Console`：`/` 是控制台、带安全头，`/legacy`、`/legacy/…` 跳到 `/`。
- 冒烟：`npm run smoke`（Playwright，见 `scripts/smoke.mjs` 文件头）打开控制台、进应用中心、看到目录里的应用、打开安装弹窗，深浅色各截一张图。
- 真机走查：`node scripts/e2e-install.mjs`（见文件头）在测试机上从页面真装 New API 和 Hermes，再看我的应用、首页看板、运行环境、设置，深浅色和手机宽度截图。只在测试机上跑。

## 约束（改代码前先看）

- **CSP**：控制台响应头是 `script-src 'self'; style-src 'self'`。不能有内联 `<script>`、`<style>`、`style="…"`，不能 `eval`。React 的 `style={{…}}` 走脚本写法（CSSOM），可以用；需要插 `<style>` 标签的组件库（例如 Radix 的弹窗锁滚动）不能用，弹窗、菜单是自己写的（`src/components/ui/dialog.tsx`、`menu.tsx`）。深浅色的预设放在 `public/theme-init.js`，不内联。
- **图表**：Recharts（MIT），写法照 shadcn Charts（`src/components/ui/chart.tsx`）。shadcn 原版用 `<style>` 标签写系列颜色，被 CSP 拦下，这里改成写在容器的 `style` 属性上（脚本写法）。Recharts 量文字宽度时改的是 `span.style`，也是脚本写法。
- **不连外网**：字体自托管（`@fontsource/geist-*`，OFL-1.1，只带拉丁字符，中文用系统字体），不引用任何 CDN；资源不转成 `data:` 地址（`assetsInlineLimit: 0`）。目录图标按 `icon_type` 显示：`svg` 是 SVG 文字，`png` 是 base64，都用 `<img src="data:…">`，SVG 里的脚本不会执行；不认识的类型退回名称缩写。
- **许可证**：构建时 `scripts/licenses-plugin.mjs` 把打进产物的 npm 包和许可证全文写到 `third-party-licenses.json`；`scripts/gen-licenses.py`（仓库根目录）读它生成 `docs/licenses`。加了依赖要重跑这两步。
- 页面文字用中文；不写服务器地址、密钥。

## 出错先看哪里

| 现象 | 先看 |
| --- | --- |
| `go test ./internal/console/` 报 “ui/ was built from other sources” | 源码改了没重新构建，按上面构建并提交 `ui/` |
| 页面白屏 | 浏览器控制台有没有 CSP 报错（多半是加了内联脚本或样式） |
| 首页 CPU、内存图写“这台服务器没有记录历史” | 服务端是旧版本（没有 `GET /v1/metrics/host/history`），或这是个读不到 /proc 的环境 |
| 首页 CPU、内存图写“刚开始记录” | 后台服务刚启动，等半分钟 |
| 运行环境提示“在服务器上运行 sudo acornfox doctor” | 服务端是旧版本，没有 `GET /v1/runtime/checks` |
| 提示“需要重新登录” | 会话过期，重新 `acornfox open` |
| 应用中心显示“这台服务器没有启用应用中心” | 服务端没接上目录（`/v1/catalog` 回 404） |
