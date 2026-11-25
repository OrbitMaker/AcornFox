# 网页控制台（新版）

`acornfox` 自带的网页控制台：React 18 + Vite + TypeScript + Tailwind，组件照 shadcn/ui 的写法（改过的文件在 `src/components/ui/`，许可证在同目录 `LICENSE`）。页面样子、文案、颜色以控制台样稿为准，主色用“中性”，深浅色都有。

构建产物写到 `../../internal/console/ui/`，**提交进仓库**，由 `internal/console` 用 `go:embed` 嵌进程序，所以编译 `acornfox` 不需要 Node。旧控制台已经去掉，访问 `/legacy/` 会跳回 `/`。

## 这一版有什么

| 页面 | 状态 | 用到的接口 |
| --- | --- | --- |
| 外壳：左侧菜单、面包屑、⌘K 命令面板、深浅色、手机宽度下的抽屉菜单（云版上线前不显示“AFX 云”入口） | 完成 | `GET /v1/console/session`、`GET /v1/status`、`POST /v1/console/logout` |
| 首页看板：可编辑网格（拖动排序、改宽高、前移后移、移除、添加图表、每张图换图表方式），布局存在这个浏览器里（`localStorage` 的 `afx-dash-v1`） | 完成 | `GET /v1/metrics/host`、`GET /v1/metrics/host/history?range=1h\|24h`、`GET /v1/metrics/apps/{app}`、`GET /v1/runtime/checks`、`GET /v1/apps/{app}/deployments?limit=5`，加共享的应用、安装任务、资源 |
| 应用中心：配方卡片、分类、搜索、应用卡片（真图标）、详情页（它需要的服务、它能提供的服务）、已断开提示；“连接私有目录”和来源标签这一版藏起（`src/lib/features.ts` 的 `PRIVATE_CATALOG_UI`，代码保留） | 完成 | `GET /v1/catalog`、`GET /v1/catalog/apps/{id}[?catalog=]`、`POST /v1/catalog/sync`、`POST /v1/catalog/connect`、`GET /v1/resources` |
| 安装弹窗：版本、需要的服务（见下）、更多设置、内存检查、命令行写法（私有目录带 `--catalog`）；进度、失败编号、首次登录；只本机的应用不给打不开的 127.0.0.1 链接，改成“怎么打开”（服务器在局域网、应用有自己的登录时可确认后改成局域网；总是给 `acornfox open 应用名`） | 完成 | `POST /v1/catalog/plan`、`POST /v1/catalog/install`、`GET /v1/catalog/install[/{task}]`、`GET /v1/apps/{app}/first-login[?reveal=true]` |
| 我的应用：卡片和表格、筛选、搜索、只本机的标“只本机”；详情四个标签页（概览、日志、版本、设置；概览里有访问卡片（三档能点、首次登录信息并在里面）、只本机应用的“怎么打开”、用到的服务、旧格式清单装的应用的说明；能改的都在“设置”里，一行一项点开才展开，以前的配置、数据、资源、高级四个标签的地址跳到设置的对应项）；来自私有目录的标目录名，目录断开的标出来；最下面有一组默认收起的“这台机器上的其他容器（N）”（用户自己 docker run 装的，W5-7，只看不管理：名字、在跑还是停了、端口、内存，没有或读不到时整组不显示；首页各应用内存、CPU 图里同一批容器合成一项“其他容器”） | 完成 | `/v1/apps…`、`/v1/metrics/apps/{app}`、`/v1/deployments/{id}/logs`、`GET /v1/host/containers` |
| 运行环境：后台服务、Docker、网关、磁盘、端口的自检，每项带故障编号、原因和怎么办 | 完成 | `GET /v1/runtime/checks`、`GET /v1/setup/faults` |
| 设置：通用、服务、访问、运行环境、存储、下载源、升级、开发者与 AI | 完成 | `GET /v1/services`、`GET /v1/services/kinds`、`POST /v1/services`、`PATCH /v1/services/{name}`、`POST /v1/services/{name}/test`、`DELETE /v1/services/{name}`、`GET/PUT/DELETE /v1/platform/domain`、`POST /v1/platform/domain/check`、`GET /v1/lan-console`、`GET /v1/public-console`、`POST /v1/public-console/enable`、`DELETE /v1/public-console`、`GET /v1/runtime/checks`、`GET /v1/metrics/host`、`GET /v1/status`、`GET /v1/catalog`、`POST /v1/catalog/connect` |

### 接新接口（路 K，W3-5、W3-6、W8、W9、W10）

页面一律先读“计划”（只读）、说清楚后果、用户同意再做、做的时候看一列步骤（`src/components/operation-dialog.tsx`，红色按钮只在确认窗里）；服务端没有对应字段或接口时“有数据才显示”，不猜。

| 功能 | 在哪 | 接口 |
| --- | --- | --- |
| 安装时选谁能打开（W3-5） | 安装弹窗里一行三档，默认是应用详情的 `exposure`，改了才发 `expose` | `GET /v1/catalog/apps/{id}`、`POST /v1/catalog/install` |
| 下载按字节的进度条（W3-6） | 安装、更新、升级的步骤里：已下 / 一共、速度、还要多久、一阵子没收到数据（`src/components/step-bar.tsx`、`lib/catalog.ts` 的 `progressLine`） | 任务步骤的 `progress`（phase download、load、pull） |
| 展示内容、找专人部署（W3-10、W3-11） | `show`、`industries`、`home` 早已接好；`assist`（visible、price_cny、url）控制“找专人部署”；`sync.show.state` 是 `failed` 时体检多一行、算一件要处理的事 | `GET /v1/catalog` |
| 只在内部网络的应用（W5-6） | 设置里没有域名、开发者设置，数据里没有附加服务；开发者设置里健康检查写“在应用里执行命令检查”，没有路径输入框 | `internal_only` |
| 改安装设置、让它生效（W8-1、W8-2） | 设置 › 安装设置里能改的项有“改”（`can_edit`）；保存先读计划，有变化才弹确认窗 | `PATCH /v1/apps/{app}/vars`、`POST /v1/apps/{app}/reapply`、`GET /v1/catalog/install/{task}` |
| 落后的应用要跟上（W8-3） | 概览最上面的提示条、我的应用卡片的“设置要跟上”、铃铛、安装设置里“让它跟上” | `GET /v1/platform/affected` |
| 改谁能打开、加删域名后地址跟着变（W6-3） | 对用到应用地址的应用，服务端 409（`APP-APPLY-05`，体里带计划）：弹确认窗；也可以“先改，稍后再让应用跟上”（`reapply: skip`） | `PATCH /v1/apps/{app}`、`…/domains` |
| 备份、恢复（W10-1、W10-2） | 设置里的“备份”一行：立即备份、一列备份（手动、更新前自动、恢复前自动）、恢复、删除；服务端不支持备份时整行不显示 | `/v1/apps/{app}/backups…`、`GET /v1/backups/tasks/{id}` |
| 更新、退回到更新前（W9-1～W9-3） | “更新”页签：`catalog_update`、`upgrade`（`can_upgrade`、`blocked`、`snapshot`、`can_rollback`）有才显示按钮；“退回到更新前”旁边写明会丢掉更新后新增的数据；我的应用页顶栏写“N 个应用有新版本”（`apps_outdated`） | `POST /v1/apps/{app}/upgrade`、`POST /v1/apps/{app}/rollback`、`GET /v1/status` |

### 首页看板能选的图表

数据只用服务端真有的，图表方式只列适合这类数据的（`src/lib/dashboard.ts` 的 `SOURCES`、`typesFor`）：

| 分组 | 图表 | 数据 | 时间 |
| --- | --- | --- | --- |
| 这台电脑 | CPU 使用率、内存 | `GET /v1/metrics/host/history`：后台服务每 30 秒记一次，**只存在内存里**，最多 24 小时，服务重启后从头记 | 近 1 小时（30 秒一个点）、近 24 小时（5 分钟平均） |
| 这台电脑 | 磁盘、已分配内存 | `GET /v1/metrics/host`、`GET /v1/resources` | 现在 |
| 应用 | 应用状态、各应用内存、各应用 CPU（后两张再加一项“其他容器”：这台机器上不是从 AcornFox 装的容器，在跑的加起来） | `GET /v1/apps`、`GET /v1/metrics/apps/{app}`（只问在跑的应用）、`GET /v1/host/containers` | 现在 |
| 提醒与记录 | 需要你处理 | 部署失败、没在运行、私有目录断开的应用；运行环境自检不正常的项；近一天失败的安装 | — |
| 提醒与记录 | 最近发生的事 | 每个应用最近 5 次部署、安装任务 | — |

样稿里有、这一版**不放进可选列表**的（没有数据来源，不做假数据）：网络流量（服务端没有记网卡流量）、AFX 磁盘占用分类（没有按“程序 / 应用数据 / 回退版本 / 缓存”统计的接口）、一周 CPU 热力图和 7 天范围（历史只存在内存里，没有落盘）、下载源测速、访问量、出错次数、在线率。服务端没有历史时（旧版本），CPU、内存曲线也不能选，时间范围标签不显示。

### 设置里没有的分区

样稿的“提醒”（桌面通知、邮件、群机器人）和“账号”没有后端支撑，这一版不显示。“访问”（`src/pages/prefs/access.tsx`）和应用的“设置”一个样子，一行一项点开才展开（行在 `src/components/expand-row.tsx`）：域名（平台域名，四种样子：正常、还没有、还没生效、这台机器用不了；旧服务端没有接口时不显示这一行）、局域网里打开控制台（开关和清密码只给服务器上的命令，记住的设备能移除）、在外面打开控制台（服务端给 `can_toggle` 时页面上能开、能关，都先确认，`needs_password` 时要再输一次现在的控制台密码，开启后的新密码只显示一次；旧服务端只给命令）。应用“设置 › 谁能打开”的域名分“平台给的”（`platform_domain`）和“自己的域名”两行，两个都有时用自己的。“存储”里的清理没有接口，只给查看占用的命令。

接口没有的东西不显示：
- “找专人部署”：前端读 `GET /v1/catalog` 的 `assist`（经营系统发布的展示内容：`visible`、`price_cny`、`url`），可见且 `url` 是 https 才显示；`url` 是空串不显示。老的 `expert` 写法也认。
- 视频：清单里有 `delivery.video` 才出现。
- 主页、视频、找专人这几个链接只认 `http://`、`https://`，其他写法不显示；目录打包检查也会拦下（`CAT-LINT-15`）。
- 端口由服务端自动分配，弹窗里不能改。

### 服务

“服务”是这台机器上能给应用用的东西：装好的应用提供的，或用户自己登记的。请求、状态文字、表单和计划的整理都在 `src/lib/services.ts`（单元测试 `services.test.ts`），页面测试在 `src/services.test.tsx`。

页面不认识任何一个具体的应用，也不为某种服务写专门的文字或表单：服务类型的中文名、每一项的名字都用接口给的（`kind_label`、`provides_detail`、服务类型定义的 `fields`）。目录里的定义漏写了中文名时，不把程序用的名字直接给用户看：显示“（未命名的服务类型）”“（未命名的项）”，旁边用小字带上原名供排查（`KindName`、`ItemName`，在 `src/pages/prefs/service-form.tsx`）。设置里“服务”分区的说明举的例子也取自目录里定义的服务类型。应用要用的服务只在“需要的服务”里接，安装表单里没有为它单独设的选项，也不提示先装哪个应用；应用自己要的值（例如模型名）是清单里的普通设置项，和别的设置项一样校验（必填、格式），命令行写成 `--set 设置项=值`（配方里 `--set 应用.设置项=值`）。

| 位置 | 显示 |
| --- | --- |
| 设置 → 服务（`#prefs/services`） | 每条服务的名字、服务类型、来源（自己登记的 / 由哪个应用提供）、状态（可用、还差哪几项、提供它的应用停着、提供它的应用已删）、谁在用。点开看各项的值，保密项只有服务端给的后 4 位，页面上没有显示明文；要在应用里做一步的项原样显示那句提示和“填这一项”；按每个使用它的应用单独发的项（`issued_per_consumer`）单独列一行，写“这几项按每个使用它的应用单独发，不在这里填”，不显示成“没填”；“试一下”分三种：通了（带连的是哪，`target`）、没通（带故障编号）、试不了（`skipped`，写原因，不算没通）；谁在用：`used_by` 里 `gone` 为 true 的应用（已删、数据还留着）写成“<应用名>（已删除，数据还留着）”，不算在用。删除先确认（确认的话按谁在用来说：有已装的应用在用就说要先删掉或换掉它们；只有已删的应用接着它，就说它们以后装回来要重新接服务），有应用在用被拒绝时列出是哪些应用（不算已删的）；删除接口可能回 204，也可能回 200 带 `notes`，有 `notes` 时在列表上方原样列出，点“知道了”才消失。应用提供的服务只在提供它的应用已删时给删除，否则写“删掉那个应用它才会消失”。`GET /v1/services` 带 `unreadable`（存的值解不开的服务名字）时，列表上方提示“这几条服务的内容读不出来” |
| 登记一条 | 先选服务类型，再按那种类型的定义（`fields`）生成表单：必填、保密、枚举、默认值、提示都照定义，不为某种类型写专门的表单。值都按字符串发，和命令行 `--set 项=值` 一样。改自己登记的某一项时：必填项不能留空保存；能清掉的项输入框里写明“留空表示清掉这一项”，留空保存先再确认一次 |
| 安装弹窗 | 打开时、换版本或选了用哪条服务时读一次安装前的计划（`POST /v1/catalog/plan`，请求体和安装请求一样）；点“安装”时用当时表单的内容再读一次，按这次的结果决定能不能装（这次读失败不拦安装，写一行小字，交给服务端检查）。计划里有需要的服务才显示这一块，每个需要四种状态：已接上（接的是哪条）、可选的没有（没有时会怎样）、必须的没有（登记一条你已有的：登记命令，也能当场登记一条）、有多条要选（选了写进请求的 `use`）。配方里后装的应用要接的服务由前面的应用装好后登记（已接上且带 `pending_from`）时，写“装好 <应用实例名> 后接上它提供的 <服务类型>”，算已接上，不拦安装。服务端给了不认识的状态时写“状态未知，安装时由服务端检查”，不拦安装。说要选却一条候选都没有时，写“没有可选的服务，登记一条后再试”并给当场登记的入口。计划里不止一个应用（配方）时，`use` 的键一律是“应用实例名.需要的名字”；只装一个应用时就是需要的名字。平台只在这台机器已有的服务里找，不替用户装别的应用，也不提示先装哪个应用。`can_install` 为 false（服务端的原因原样列出）或还有没选的，安装按钮不可点并写原因 |
| 应用详情页 | `GET /v1/catalog/apps/{id}` 的 `uses`（每项带 `kind_label`、`required`、可选的带 `without`）列成“它需要的服务”（必须有 / 可选，可选的写没有时会怎样），`provides_detail` 列成“它能提供的服务”；两样都没有时不显示这一块 |
| 应用概览 | `GET /v1/apps/{app}` 带 `services` 时显示“用到的服务”：需要的、服务类型、接的是哪条、状态；不是可用的在最上面提示，带“去看服务”。用旧格式清单装的应用（照常运行、能启停和删除，但不接服务），详情带字符串字段 `legacy_note` 时原样显示成一条提示，没有就不显示 |

旧服务端没有这些接口（404、405）：设置里写“这个版本的服务端还没有这项功能”，安装弹窗和概览不显示这些块，别的照旧。

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

### 旧格式的清单、空目录

`GET /v1/catalog` 的应用和配方带 `former: true` 时，目录里这个应用（配方：其中某个应用）还是旧格式的清单，这一版装不了（服务端只在不用升级时标它）；`note` 是给用户看的一句话。

| 位置 | 显示 |
| --- | --- |
| 应用卡片、配方卡片 | 原样显示 `note`（没给时用“这是旧格式的清单，这一版装不了”）；“安装”“开始”不可点。已经装过的照常给“已安装 · 管理” |
| 应用详情页 | 详情接口回 409、编号 `CAT-INST-36`：用目录列表里的基本信息顶上，一块说明里是那句话和服务端的原话；“安装”不可点，**不给升级命令**（升级也装不了，要等重新上架） |
| 安装弹窗（命令面板也能打开） | 读详情被拒（`CAT-INST-36`）时服务端的说明原样显示，不能提交，不给升级命令 |

应用中心一个应用都没有、用户也没在搜索时，写“还没有可以安装的应用”，下面一句“应用按新格式上架后会出现在这里”；搜索没结果时仍写“没找到“…””。判断在 `src/lib/catalog.ts`（`formerNote`、`isFormerFault`、`storeEmpty`）。

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

- `npm test`（Vitest）：数据整理（分类、来源、内存检查、安装请求和命令行写法、服务和安装前的计划、任务进度、环境变量改动、看板数据和布局）和页面主要路径（用假接口渲染应用中心、私有目录、安装、首次登录、我的应用、首页看板、运行环境、设置）。
- `go test ./internal/console/`：产物齐全、只引用同源文件、没有内联脚本和样式、没用 `eval`；**`build-info.json` 里的源码哈希和现在的源码对得上**。改了源码忘记重新构建，这个测试会失败。哈希规则见 `scripts/source-hash.mjs`，参与的文件见 `build-inputs.json`（测试文件不算）。
- `go test ./internal/apiserver/ -run Console`：`/` 是控制台、带安全头，`/legacy`、`/legacy/…` 跳到 `/`。
- 冒烟：`npm run smoke`（Playwright，见 `scripts/smoke.mjs` 文件头）打开控制台、进应用中心、看到目录里的应用、打开安装弹窗，深浅色各截一张图。
- 真机走查：`node scripts/e2e-install.mjs`（见文件头）在测试机上从页面按顺序真装几个应用（前面的提供服务，后面的需要它），再看我的应用、首页看板、运行环境、设置，深浅色和手机宽度截图。录屏版是 `scripts/record-progress-reuse.mjs`（下载进度条、接上旧数据重装）。只在测试机上跑。
- 走查查什么由数据定，不看页面上碰巧出现了什么：
  - 目录列表（`GET /v1/catalog`）里这个应用（配方时是配方里的各个应用）声明了需要的服务（`uses`），安装弹窗里就必须在时限内（`E2E_NEEDS_TIMEOUT_MS`）列出每一项，行数对得上，并且都已接上；没出现、行数不对、有没接上的，都算走查失败。没声明的不查。
  - 装好后服务端有这个应用的首次登录信息（`GET /v1/apps/{应用}/first-login`，不带 `reveal`），弹窗里就必须有它的首次登录卡片；里面有保密项的，必须都是 `******`。接口回 404（这个应用没有首次登录信息）的不查；回别的错误（例如会话丢了、服务端解不开）算走查失败。结果里只写遮住了几项，不写读到的值。
  - 这几样判断写在 `scripts/walk-targets.mjs` 里不碰页面的小函数里（`declaredNeeds`、`needsVerdict`、`loginExpectation`，以及读环境变量的 `walkTargets`），单元测试在 `src/lib/walk-targets.test.ts`（随 `npm test` 跑），类型在 `scripts/walk-targets.d.mts`。
- 这三个脚本走查哪些应用、哪个配方、填哪些设置项，从环境变量读，脚本里不写死具体应用（默认值和规则在 `scripts/walk-targets.mjs` 文件头）：

  | 环境变量 | 默认 | 含义 |
  | --- | --- | --- |
  | `E2E_APPS` | `new-api,hermes-agent` | 要走查的应用的目录 id，逗号分隔，按顺序装；后面的应用接上前面的应用登记的服务 |
  | `E2E_RECIPE` | 不设 | 配方的目录 id。设了，`e2e-install.mjs` 用这个配方一次装好，不再一个一个装（另两个脚本不用它） |
  | `E2E_SET` | `hermes-agent.model=qwen-plus` | 安装时要填的设置项，分号分隔，每项写成 `应用id.设置项=值`；空串表示不填 |
  | `E2E_NEEDS_TIMEOUT_MS` | `15000` | 打开安装弹窗后，等“需要的服务”全部列出的时限（毫秒，正整数）；到时还没列全算失败（冒烟脚本不用它） |

  目录里要有这些应用（新格式的清单）；程序自带的目录是空的，要先让测试机用上有它们的目录。

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
