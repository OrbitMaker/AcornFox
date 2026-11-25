// 接口返回的数据结构，字段名和 Go 端 JSON 一致。
// 只写控制台用到的字段；可选字段在旧版本服务端上可能没有。

export interface Diagnosis {
  stage: string
  code: string
  message: string
  log_excerpt?: string
  hint?: string
}

export interface Deployment {
  id: string
  app: string
  seq: number
  source_kind: string
  source_ref: string
  status: string
  diagnosis?: Diagnosis
  created_at: string
  finished_at?: string
  based_on_seq?: number
  origin_kind?: string
  reason?: string
  status_text?: string
  source_text?: string
  duration_seconds?: number | null
  can_rollback?: boolean
  rollback_blocked?: string
  same_image_as?: number
  rollback_text?: string
  failure_text?: string
  logs?: { build: boolean; run: boolean }
}

export interface EnvVar { key: string; secret: boolean; value?: string }
export interface Volume { app: string; path: string; volume_name: string; auto: boolean; created_at: string }
export interface Domain { app: string; name: string; status: string; diagnosis?: Diagnosis; created_at: string }
export interface Addon {
  kind: string
  image: string
  env_var: string
  host: string
  port: number
  volume_name: string
  observed_state: string
  created_at: string
}

export interface AppView {
  name: string
  desired: string
  port: number
  health_path: string
  public_port: number
  current_deployment: string
  memory_mb: number
  cpu_milli: number
  url: string
  lan_url?: string
  exposure: string
  // 应用没有单独设对外方式、跟着平台默认走（default_exposure）；PATCH exposure "" 回到默认。
  exposure_is_default?: boolean
  default_exposure?: string
  observed_state: string
  observed?: { name: string; state: string; running: boolean; restart_count: number; host_port: number; started_at?: string }
  live?: Deployment
  latest?: Deployment
  // 正在服务的版本号（第 N 版），列表里也有；详情另有完整的 live。
  live_seq?: number
  rollback_target_seq: number
  env?: EnvVar[]
  volumes?: Volume[]
  domains?: Domain[]
  // 平台给的域名（<应用名>.<平台域名>）。没设平台域名时没有；应用不是公网时 active 为 false，note 写怎样才生效。
  platform_domain?: { host: string; active: boolean; note?: string }
  addons?: Addon[]
  catalog_id?: string
  catalog_version?: string
  catalog_origin?: string
  catalog_origin_name?: string
  catalog_disconnected?: boolean
  // 只本机能打开（exposure local）：Linux 上 url 是 127.0.0.1，用户在自己电脑上点不开。
  local_only?: boolean
  // 只本机的应用在 Linux 服务器上怎么打开（服务端给，桌面版没有）。
  open_hint?: OpenHint
  // 只在内部网络（exposure internal，数据库、缓存这类给别的应用用的）：没有 url、没有 open_hint、不能改访问方式。
  internal_only?: boolean
  internal_note?: string
  // 详情才有：这个应用用了哪些服务（旧服务端没有这一项）。
  services?: AppServiceUse[] | null
  // 详情才有：用旧格式清单装的应用的说明（照常运行、能启停和删除，但不接服务）。有就原样显示成一条提示。
  legacy_note?: string
  // 从应用中心装的应用，应用中心里有比现在新的版本时是那个版本号（例如 2.48.0），没有新版本时没有这一项。
  // 应用“更新”页签和概览的版本卡片有它才写“有新版本”，不猜“已是最新”。
  catalog_update?: string
  // 应用升级（W9）的现状：服务端有这一项才显示“更新”按钮和“退回到升级前”（见 lib/app-upgrade.ts），没有时页面不提升级。
  upgrade?: AppUpgrade
}

// GET /v1/apps/{app} 的 upgrade：能不能升、为什么不能、升级前备份的数据还在不在、能不能退回到升级前。
export interface AppUpgrade {
  available?: string // 新版本号（服务端给的是字符串）
  can_upgrade?: boolean
  // 不能升的原因（一句话）和故障编号（编号只放进“复制给 AI 工作台”）
  blocked?: string
  blocked_code?: string
  notes?: string[]
  snapshot?: { id: string; version?: string; created_at?: string; size_mb?: number } | null
  can_rollback?: boolean
}

// OpenHint：只本机的应用怎么从用户自己的电脑打开。
// recommend 为 lan：服务器在用户的局域网里、应用有自己的登录，可以一键改成局域网（要确认）；
// 为 tunnel：在自己电脑上运行 command（acornfox open 应用名，经 SSH 隧道）。
// lan_url 只在已经开放到局域网、现在就打得开时才有；lan_url_after_expose 是改成局域网以后的地址，改之前打不开。
export interface OpenHint {
  lan_address?: string
  lan_url?: string
  lan_url_after_expose?: string
  has_login: boolean
  can_lan: boolean
  recommend: 'lan' | 'tunnel' | string
  command: string
  // 要原样问用户的那句话（recommend 是 lan、还没开放时）和用户同意后由 AI 运行的命令。
  ask?: string
  ask_yes_command?: string
  // 用户电脑上没有 acornfox 命令行时的 SSH 隧道（“用户@服务器地址”由用户换成自己的）和之后打开的地址。
  tunnel_command?: string
  tunnel_url?: string
}

// GET /v1/host/containers：这台机器上不是从 AcornFox 装的容器（用户自己 docker run 装的），只读。
// memory_mb、cpu_percent：读到用量的在跑容器加起来；旧服务端没有这个接口（404），读不到时是 503（MACHINE-04）。
export interface OtherPort { host: number; container: number; proto: string }
export interface OtherContainer {
  id: string
  name: string
  image: string
  state: string
  running: boolean
  created: number
  ports: OtherPort[]
  host_network?: boolean
  stats_available: boolean
  cpu_available: boolean
  cpu_percent: number
  memory_usage_mb: number
}
export interface OtherContainersView {
  observed_at: string
  containers: OtherContainer[]
  running: number
  stopped: number
  memory_mb: number
  cpu_percent: number
  truncated?: boolean
}

export interface AppMetrics {
  app: string
  available: boolean
  observed_state: string
  cpu_percent?: number
  cpu_available?: boolean
  memory_usage_mb?: number
  memory_limit_mb?: number
  network_rx_mb?: number
  network_tx_mb?: number
  pids?: number
}

export interface Resources {
  memory: { host_mb: number; cap_mb: number; auto: boolean; allocated_mb: number }
  cpu: { host_milli: number; cap_milli: number; auto: boolean; max_app_milli: number }
  items: { app: string; kind: string; memory_mb: number; cpu_milli: number }[]
  min_app_memory_mb: number
  min_app_cpu_milli: number
}

export interface Session { csrf: string; expires_at: string; server: string }

// on_public、latest_version：服务端还没有给这两项（要服务端配合），没给时页面照没有这回事处理。on_public：这台机器在不在公网上
// （家里的机器不在，是正常情况）；latest_version：线上最新的版本号（和 version 一样就是“已是最新”）。
// apps_outdated：有新版本的应用（数量，或应用代号的列表）；服务端没给时不提。
// platform_update（W12-3，服务端每 6 小时查一次线上最新的正式版）：GET /v1/status 带上；旧服务端没有，页面照旧只看 latest_version。
export interface PlatformUpdate {
  enabled: boolean // false：服务的环境里设了 ACORNFOX_UPDATE_CHECK=off，不联网查
  current: string
  latest?: string // 最近一次查到的最新正式版；还没查到过时没有
  available: boolean // latest 比 current 新
  checked_at?: string // 上次查的时间（不管成没成），RFC 3339
  ok: boolean // 上次查到了
  source?: string // gitee、github
  notes_url?: string // 发行说明
  upgrade_command?: string // 这台机器该用的那一条升级命令（Linux、Windows、Mac 不一样，国内用 Gitee、海外用 GitHub）
  upgrade_where?: string // 在哪里运行它
  host_kind?: string // linux、windows、mac
  last_ok_at?: string
  failures?: number
  unreachable?: boolean // 好几天一直查不到
  error?: string
  code?: string // PLAT-UPD-01 有新版本、PLAT-UPD-02 一直查不到
}
export interface ServerStatus { runner?: string; docker_version?: string; apps?: number; version?: string; setup?: { state: string }; edge?: { state: string; message?: string }; on_public?: boolean; latest_version?: string; platform_update?: PlatformUpdate; apps_outdated?: number | string[] }

// ---- 首页看板、运行环境、设置 ----

// GET /v1/metrics/host：整机此刻的资源（读不到的项没有）。
export interface HostView {
  available: boolean
  cpu_percent?: number
  cpu_cores?: number
  memory_used?: number
  memory_total?: number
  memory_percent?: number
  disk_used?: number
  disk_total?: number
  disk_percent?: number
  load1?: number
  uptime_seconds?: number
}

// GET /v1/metrics/host/history?range=1h|24h：只存在内存里，服务重启后从头记。
export interface HostPoint { t: string; cpu_percent?: number; memory_used?: number; memory_total?: number; disk_used?: number; disk_total?: number }
export interface HostHistory { range: string; step_seconds: number; ranges: string[]; since?: string; points: HostPoint[] }

// GET /v1/runtime/checks：和 acornfox doctor 同一套故障编号。
// key：服务端还没有给（要服务端配合）：每项检查一个固定的键，“体检”页按它合成说人话的行，没给时每项检查各占一行、用服务端原话，
//   docker、service、edge（后台程序）、data_dir（保存数据）、disk（磁盘）、sources（下载）、public（在不在公网上）；没有 key 的项单独成一行。
export interface RuntimeCheck { ok: boolean; level: 'ok' | 'warn' | 'fail' | string; text: string; code?: string; key?: string }
export interface DownloadSources { china: boolean; region_source: string; docker_hub: string[]; ghcr?: string[]; gateway?: string; download_base?: string }
// app_disk、keep_versions：服务端还没有给（要服务端配合），没给时体检里不写这两样。app_disk：每个应用占了多少磁盘（应用程序加数据，字节）；
// keep_versions：每个应用除了正在用的，还留着几个旧版本用来退回上一版。
export interface AppDiskUse { app: string; bytes: number }
export interface RuntimeChecks { os: string; checked_at: string; ok: boolean; items: RuntimeCheck[]; data_dir?: string; docker_version?: string; sources?: DownloadSources; app_disk?: AppDiskUse[]; keep_versions?: number }

// GET /v1/setup/faults：故障编号的说明（单一来源在服务端）。
export interface FaultDef { code: string; title: string; causes: string[]; next: string; needs_user: boolean }

// GET /v1/lan-console。password_set 为 false：开着但还没人设密码（局域网里谁先打开谁设）；
// password_set_at / password_set_from：密码是什么时候、从哪个地址设的；generated_password：旧版本开的，还在用生成的密码。
export interface LANConsoleStatus { enabled: boolean; url?: string; port: number; password_set?: boolean; password_set_at?: string; password_set_from?: string; generated_password?: boolean; devices?: number }
// GET /v1/lan-console/devices：登录时勾了“记住这台设备 90 天”的浏览器。
export interface LANDevice { id: string; name: string; created_at: string; last_used_at: string; last_ip?: string; expires_at: string }
// GET /v1/public-console[?check=https]。can_toggle、needs_password、suggested_domain、unsupported 是能在页面上开关以后才有的
// （旧服务端没有：页面只给服务器上的命令）。suggested_domain：有平台域名时是 console.<平台域名>。
export interface PublicConsoleStatus {
  enabled: boolean; domain?: string; url?: string; enabled_at?: string; verifying?: string
  can_toggle?: boolean; needs_password?: boolean; suggested_domain?: string; unsupported?: CodeText
  // 只在带 ?check=https 问、而且开着时才有：地址的证书好了没有（服务器自己查的）。
  // code：ok 可以打开；cert_pending 正在申请；cert_failed 等了几分钟还没签下来；别的是这台服务器确认不了。
  https?: HTTPSCheck
}
export interface HTTPSCheck { code: string; message?: string; hint?: string }
// POST /v1/public-console/enable 的应答：新密码只在这一次给（password；也认 password_once）。
export interface PublicConsoleEnableResult { domain?: string; url?: string; password?: string; password_once?: string; already_enabled?: boolean }

export interface CodeText { code: string; text: string }
// GET/PUT/DELETE /v1/platform/domain、POST /v1/platform/domain/check（四个返回同一个形状）：这台机器的平台域名。
// 公网应用自动得到 <应用名>.<domain>。旧服务端没有这个接口（404）。
export interface PlatformDomainView {
  domain?: string
  state: 'unset' | 'ok' | 'problem' | string
  supported: boolean
  unsupported?: CodeText
  public_ip?: string
  // 到买域名的地方要加的那一条记录：名字（例如 *.apps）、类型、指向。还没设域名时可能只有 type、value。
  record?: { name?: string; type?: string; value?: string }
  // waiting：这一项还没法检查（前一项没过，或者还没有开放到公网的应用），不算出问题
  checks?: { key: string; ok: boolean; text: string; waiting?: boolean }[]
  // 只在 state 是 problem 时有：第一项没过的检查
  problem?: { code: string; text: string; detail?: string; next?: string }
  console_host?: string
}

// GET /v1/catalog/sources：公共目录和连过的私有目录（已断开的也在）。
export interface CatalogSourceView {
  id: string
  kind: 'public' | 'private' | string
  name: string
  serial: number
  origin?: string
  apps: number
  recipes: number
  connected_at?: string
  last_sync?: string
  code?: string
  message?: string
  disconnected: boolean
  disconnected_at?: string
}

// ---- 应用中心 ----

export interface MinRec { min: number; recommended?: number }
export interface Delivery { tier: 'auto' | 'steps' | string; minutes?: number; memory_mb?: number; video?: string; user_steps?: string[] }
// Use：清单里的一个“需要的服务”。kind 是服务类型的名字（只给程序用，页面上显示 kind_label）。
// 目录列表里只有清单原样的几项；应用详情（GET /v1/catalog/apps/{id}）另带 use、kind_label、required。
export interface Use {
  name: string
  kind: string
  optional?: boolean
  without?: string // 可选的服务没有时会怎样（一句话）
  dedicated?: boolean
  use?: string
  kind_label?: string
  required?: boolean
}

// ProvideView：应用能提供的一种服务。
export interface ProvideView { kind: string; kind_label?: string }

// ---- 应用和解决方案的展示内容 ----
// 介绍只有一个字段 doc：一篇 Markdown 文档（认哪些写法见 lib/doc.ts），演示视频、截图、能做什么、装好后做什么都写在这一篇里。
// 卡片上另外要的只有：主色、一句话、角标、封面图。这些来自经营系统签名发布的展示内容（GET /v1/catalog 里每个应用和配方的 show），
// 格式见经营系统和单机版的展示内容约定（show.json）；没有 show（旧目录、验签没过、没编辑过）时页面退回清单里的 name 和 summary。
export interface AppShow {
  name?: string // 应用中心里显示的名字，空表示用清单里的 name（lib/catalog.ts 的 applyShow 在读到目录时就换好）
  color?: string // 主色（#rrggbb），封面和图标底色用；空时用灰色
  tagline?: string // 一句话卖点，比 summary 短
  flag?: 'hot' | 'new' | 'pick' | string // 热门、新上架、推荐（经营系统里定）
  cover?: string // 卡片封面图的链接；没有时封面是应用图标放在主色上
  doc?: string // 介绍：一篇 Markdown 文档
  order?: number // 同一货架里小的排前面；0 或没有表示没排
  keywords?: string[] // 搜索用的词，和清单的 keywords 合并
  // 有一份可用的操作手册（给 AI 工作台看的，页面上不显示它的内容）：对哪个版本验过、哪天验的。没有就不写这一项。
  skill?: { version: string; checked: string; size?: number }
  // 只用在解决方案上：属于哪些行业，写行业的 id（行业的名字和顺序在 CatalogView.industries 里）。可以挂几个。
  industries?: string[]
}

// 行业：id 是稳定的，name 是给用户看的，顺序由负责人定。
export interface Industry { id: string; name: string }

export interface CatalogApp {
  id: string
  version: string
  name: string
  summary: string
  category: string
  keywords: string[] | null
  tags: string[] | null
  memory_mb: MinRec
  platforms: string[]
  size_mb: number
  delivery: Delivery
  uses: Use[] | null
  provides: string[] | null
  supported: boolean
  installed: string[]
  command: string
  // 来源：public 或私有目录编号；旧服务器没有这一项，当作 public。
  source?: CatalogSource
  // 安装包已在本机时才有（列表不下载安装包），没有就显示名称缩写。
  icon?: string
  icon_type?: string
  // 要先把 AcornFox 升到这个版本才能装（例如 "0.6.0"）；不用升级是空串，旧服务端没有这一项。
  needs_platform?: string
  // former：目录里这个应用还是旧格式的清单，这一版装不了（详情和安装都报 CAT-INST-36）；note 是给用户看的一句话。
  // 服务端只在不用升级（needs_platform 为空）时标它。
  former?: boolean
  note?: string
  show?: AppShow
}

export interface CatalogSource { id: string; name: string }

export interface Recipe {
  id: string
  title: string
  summary: string
  keywords?: string[]
  apps: { app: string; reuse_installed?: boolean }[]
  // 给用户看叫“解决方案”。title 按问题起名；show.doc 是介绍（一篇文档），show.industries 是行业（id）。
  // 只装一个应用的配方不算解决方案（见 shelf.tsx 的 isSolution）。
  show?: AppShow
  // 配方里谁接谁：to 这个应用名叫 uses 的需要，接 from 这个应用登记的服务。
  links?: { from: string; to: string; uses: string }[]
  delivery: Delivery
  command: string
  source?: CatalogSource
  // 配方自己或其中任何一个应用要先升级 AcornFox 时有值，含义同应用的 needs_platform。
  needs_platform?: string
  // 配方里有应用还是旧格式的清单：这个配方这一版装不了；note 是给用户看的一句话（同应用的 former、note）。
  former?: boolean
  note?: string
}

// 找专人部署：目录的运营数据里有才显示（现在的目录还没有这一项）。
export interface ExpertService { title?: string; price_cny?: number; url?: string }

// GET /v1/catalog 的 sync.show：这次同步里展示内容（一句话、封面、介绍）的情况。state：fetched 取到了；none 目录里没有；
// failed 验签没过或和目录对不上（这次不用它，应用中心退回用清单里的名字和简介，装应用不受影响），code 是故障编号，message 是一句话。
export interface ShowSyncState { state: 'fetched' | 'none' | 'failed' | string; code?: string; message?: string; serial?: number }

export interface CatalogView {
  serial: number
  origin: string
  sync: Record<string, unknown> & { show?: ShowSyncState }
  apps: CatalogApp[]
  recipes: Recipe[]
  sources?: CatalogSourceView[]
  expert?: ExpertService
  // 展示内容里和整个目录有关的几项（没有展示内容时都没有）：
  industries?: Industry[] // 行业，按排好的顺序
  home?: { solutions: string[] } // 首页“你想解决什么问题？”按这个顺序摆的解决方案（配方 id）
  // 找专人部署（经营系统发布的展示内容）：visible 为 true 且 url 是 http/https 才显示按钮；价格只按它显示。url 是空串就不显示按钮。
  assist?: { visible?: boolean; price_cny?: number; url?: string }
}

export interface ParamView {
  key: string
  type: string
  label?: string
  hint?: string
  default?: unknown
  options?: unknown[]
  pattern?: string
  required: boolean
  generated: boolean
  editable?: string
  // hidden：服务端还没有给（要服务端配合），没给时都照常显示。安装时自动生成、用户又不用记的（会话密钥、加密密钥这类），安装弹窗不问、不列（评估 L3）。
  hidden?: boolean
}

// PlatformVarView：平台变量（平台按这台机器自动填给应用的时区、下载源等）。
// 表在服务端 internal/catalog/platformvars.go；user_can_set 的可以在安装时改。
export interface PlatformVarView {
  name: string
  label: string
  standalone: string
  cloud: string
  user_can_set: boolean
  secret?: boolean
  value?: string
  available: boolean
  note?: string
}

export interface AppDetail {
  id: string
  version: string
  versions: string[]
  name: string
  summary: string
  category: string
  homepage: string
  license: string
  source_code?: string
  icon?: string
  icon_type?: 'svg' | 'png' | string // svg：icon 是 SVG 文字；png：icon 是 PNG 的 base64
  memory_mb: MinRec
  disk_mb: MinRec
  platforms: string[]
  supported: boolean
  size_mb: number
  image: string
  port: number
  exposure: string
  delivery: Delivery
  params: ParamView[]
  platform?: PlatformVarView[] // 旧服务端没有
  provides: string[] | null
  // 能提供的服务，每种带中文名；需要的服务见 uses（带 use、kind_label、required）。
  provides_detail?: ProvideView[] | null
  uses: Use[] | null
  installed: string[]
  command: string
  source?: CatalogSource
  // 要先把 AcornFox 升到这个版本才能装（例如 "0.6.0"）；不用升级是空串，旧服务端没有这一项。
  needs_platform?: string
  show?: AppShow
}

// needs_platform：原因是“AcornFox 版本不够”时有，值是要升到的版本（例如 "0.6.0"）；别的错误没有这个键。
export interface Fault { code: string; message: string; hint?: string; diagnosis?: unknown; task_id?: string; needs_platform?: string }

// StepProgress：正在进行的一步能自己量出进度时有（经下载网关下载镜像包：download；下完导入：load）。
// phase：download 经下载网关下载应用程序；load 下完导入；pull 从公共下载源拉（Docker 报多少算多少，blind 为 true 是它这会儿没报进度）。
export interface StepProgress {
  phase: 'download' | 'load' | 'pull' | string
  done_bytes: number
  total_bytes?: number
  bytes_per_sec?: number
  eta_seconds?: number
  stalled_seconds?: number
  source?: string
  blind?: boolean
  pull_seconds?: number
}
export interface StepView { app: string; name: string; label: string; status: string; message?: string; started_at?: string; finished_at?: string; progress?: StepProgress }
export interface AppResult { app: string; catalog_id: string; version: string; reused: boolean; created: boolean; url?: string; memory_mb?: number; volumes?: string[] }
export interface LoginField { label: string; value: string; secret?: boolean }
// url_note：url 只有服务器本机能打开时的说明（从自己电脑打开的办法看应用的 open_hint）。
export interface FirstLogin { app: string; url?: string; url_note?: string; fields: LoginField[] }

export interface TaskView {
  id: string
  kind: 'app' | 'recipe' | string
  target: string
  // 目录来源：public 或私有目录编号；旧版服务端没有这一项。
  catalog?: string
  status: 'running' | 'succeeded' | 'failed' | string
  step?: string
  keep_volumes_on_failure?: boolean
  steps: StepView[]
  apps: AppResult[]
  fault?: Fault
  first_login?: FirstLogin[]
  user_steps?: string[]
  created_at: string
  updated_at: string
  finished_at?: string
}

// ---- 服务（这台机器上能给应用用的东西：装好的应用提供的，或用户自己登记的） ----

// 服务类型里的一项。type：string、int、bool、url、enum；secret 的值在所有输出里只留后 4 位。
export interface ServiceField {
  key: string
  label: string
  type: 'string' | 'int' | 'bool' | 'url' | 'enum' | string
  required?: boolean
  secret?: boolean
  default?: string | number | boolean | null
  options?: string[] | null
  hint?: string
}

// GET /v1/services/kinds：目录里定义的服务类型。
export interface ServiceKind {
  kind: string
  label: string
  summary?: string
  fields: ServiceField[]
  has_test?: boolean
}

// 服务的一项值。保密项的 value 永远是遮盖后的样子；ask 不为空：这一项要用户在应用里做一步再填进来。
export interface ServiceValue { key: string; label: string; value: string; secret: boolean; set: boolean; ask?: string }

// status：ok 可用；incomplete 还差 missing 里的项；provider_stopped 提供它的应用停着；provider_gone 提供它的应用已删；
// not_ready 还不能用：提供它的应用的就绪检查现在没过（ready_say 是它清单写的原因），不挡安装，只是用到它的功能要等它好了。
export type ServiceStatus = 'ok' | 'incomplete' | 'provider_stopped' | 'provider_gone' | 'not_ready'

// GET /v1/services、GET /v1/services/{name}。source：user 用户登记；app 由应用 app 提供。
export interface ServiceView {
  name: string
  kind: string
  kind_label?: string
  source: 'user' | 'app' | string
  app?: string
  status: ServiceStatus | string
  status_label?: string
  missing?: string[] | null
  values?: ServiceValue[] | null
  used_by?: ServiceUse[] | null
  // 按每个使用它的应用单独发的项（项的名字）：不在 values 里，也不由用户在这里填。
  issued_per_consumer?: string[] | null
  // dedicated_to：独占这条服务的应用（空串：没有）；max_consumers：提供它的应用让同时接几个（0：不限）；
  // slots_left：现在还能接几个（被独占或接满是 0，不限是 null；删了但数据还留着的应用也占位）。
  dedicated_to?: string
  max_consumers?: number
  slots_left?: number | null
  // status 是 not_ready 时：ready_say 是提供它的应用清单写的那句话，原样给用户看；ready_detail 是检查哪一步没过。
  ready_say?: string
  ready_detail?: string
  created_at?: string
  updated_at?: string
  // PATCH 才有：例如“有 2 个应用在用它，重新应用设置后才生效”。
  notes?: string[] | null
}

// ServiceUse：一个接着这条服务的应用（实例名）和它清单里需要的名字。
// gone：这个应用已经删了、数据还留着（以后用旧数据装回来时接上原来的）。它不算“在用”：不拦删除这条服务。
// dedicated：这个应用独占这条服务（别的应用接不了）。
export interface ServiceUse { app: string; use: string; gone?: boolean; dedicated?: boolean }

// DELETE /v1/services/{name}：204 没有内容；或 200 带 notes（删掉的同时要告诉用户的话，例如哪些已删的应用以后装回来要重新接服务）。
export interface RemovedService { name?: string; notes: string[] }

// GET /v1/services。unreadable：存的值解不开的服务名字（不在 services 里）；没有时可能不带这一项。
export interface ServiceList { services: ServiceView[]; unreadable: string[] }

// POST /v1/services/{name}/test。没通时 ok 为 false，code 是故障编号。
// skipped 为 true：这次试不了（没法试，不是没通），detail 说为什么；target：连的是哪。
export interface ServiceTestResult { ok: boolean; detail?: string; code?: string; message?: string; target?: string; skipped?: boolean }

// ---- 安装前的计划（POST /v1/catalog/plan，只读，请求体和安装请求一样） ----

// status：matched 已接上（service）；optional_missing 可选的，没有，会跳过（without）；
// required_missing 必须的，没有（how 是登记一条的命令）；choose 有多条，在 candidates 里选一条。
// pending_from 有值：配方里后装的应用要接的服务现在还没有，由同一次安装里先装的那个应用（实例名）装好后登记，算已接上。
// 平台只在这台机器已有的服务里找，不替用户装别的应用。不认识的状态页面写“状态未知”，交给服务端在安装时检查。
export type NeedStatus = 'matched' | 'optional_missing' | 'required_missing' | 'choose'

export interface PlanNeed {
  use: string
  kind: string
  kind_label?: string
  required: boolean
  dedicated?: boolean
  status: NeedStatus | string
  service?: string
  pending_from?: string
  // 能选的服务名；旧写法或以后的写法可能是对象，页面两种都认（见 lib/services.ts 的 candidateNames）。
  candidates?: (string | { name: string; status?: string; status_label?: string })[] | null
  without?: string
  how?: string[] | null
}

export interface PlanApp {
  id: string
  name: string
  version?: string
  needs?: PlanNeed[] | null
  params_missing?: { key: string; label?: string }[] | null
  provides?: { kind: string; kind_label?: string }[] | null
}

export interface InstallPlan {
  can_install: boolean
  say?: string
  apps?: PlanApp[] | null
  problems?: { code: string; message: string }[] | null
}

// GET /v1/apps/{app} 的 services：这个应用用了哪些服务。没接上时 service 为空（status 是 not_connected）。
export interface AppServiceUse { use: string; kind: string; kind_label?: string; required?: boolean; service?: string; status: string; status_label?: string }

// ---- 设置 › 服务里的“平台自动填的”一行 ----

// GET /v1/catalog/platform-vars：平台按这台机器自动填给应用的那几项和现在的值（每一项的形状同 PlatformVarView）。
// 服务端没启用应用中心或是旧版本时回 404。
export interface PlatformVarList { vars?: PlatformVarView[] | null }
