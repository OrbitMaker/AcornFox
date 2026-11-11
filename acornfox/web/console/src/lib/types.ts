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
  // 详情才有：给这个应用发了一把不限额令牌的本机模型网关（New API 的应用名）。
  gateway_token_from?: string
}

// OpenHint：只本机的应用怎么从用户自己的电脑打开。
// recommend 为 lan：服务器在用户的局域网里、应用有自己的登录，可以一键改成局域网（要确认）；
// 为 tunnel：在自己电脑上运行 command（acornfox open 应用名，经 SSH 隧道）。
export interface OpenHint {
  lan_address?: string
  lan_url?: string
  has_login: boolean
  can_lan: boolean
  recommend: 'lan' | 'tunnel' | string
  command: string
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

export interface ServerStatus { runner?: string; docker_version?: string; apps?: number; version?: string; setup?: { state: string }; edge?: { state: string; message?: string } }

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
// GET /v1/metrics/apps/{app}/history?range=1h|24h：一个应用的 CPU、内存；规则同上，应用停着的时候不记。
export interface AppPoint { t: string; cpu_percent?: number; memory_usage_mb?: number; memory_limit_mb?: number }
export interface AppHistory { app: string; range: string; step_seconds: number; ranges: string[]; since?: string; points: AppPoint[] }
export interface HostHistory { range: string; step_seconds: number; ranges: string[]; since?: string; points: HostPoint[] }

// GET /v1/runtime/checks：和 acornfox doctor 同一套故障编号。
export interface RuntimeCheck { ok: boolean; level: 'ok' | 'warn' | 'fail' | string; text: string; code?: string }
export interface DownloadSources { china: boolean; region_source: string; docker_hub: string[]; ghcr?: string[]; gateway?: string; download_base?: string }
export interface RuntimeChecks { os: string; checked_at: string; ok: boolean; items: RuntimeCheck[]; data_dir?: string; docker_version?: string; sources?: DownloadSources }

// GET /v1/setup/faults：故障编号的说明（单一来源在服务端）。
export interface FaultDef { code: string; title: string; causes: string[]; next: string; needs_user: boolean }

// GET /v1/lan-console。password_set 为 false：开着但还没人设密码（局域网里谁先打开谁设）；
// password_set_at / password_set_from：密码是什么时候、从哪个地址设的；generated_password：旧版本开的，还在用生成的密码。
export interface LANConsoleStatus { enabled: boolean; url?: string; port: number; password_set?: boolean; password_set_at?: string; password_set_from?: string; generated_password?: boolean; devices?: number }
// GET /v1/lan-console/devices：登录时勾了“记住这台设备 90 天”的浏览器。
export interface LANDevice { id: string; name: string; created_at: string; last_used_at: string; last_ip?: string; expires_at: string }
export interface PublicConsoleStatus { enabled: boolean; domain?: string; url?: string; enabled_at?: string; verifying?: string }

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
export interface Use { name: string; kind: string; for_param?: string; optional?: boolean }

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
}

export interface CatalogSource { id: string; name: string }

export interface Recipe {
  id: string
  title: string
  summary: string
  keywords?: string[]
  apps: { app: string; reuse_installed?: boolean }[]
  links?: { from: string; to: string; uses: string }[]
  delivery: Delivery
  command: string
  source?: CatalogSource
  // 配方自己或其中任何一个应用要先升级 AcornFox 时有值，含义同应用的 needs_platform。
  needs_platform?: string
}

// 找专人部署：目录的运营数据里有才显示（现在的目录还没有这一项）。
export interface ExpertService { title?: string; price_cny?: number; url?: string }

export interface CatalogView {
  serial: number
  origin: string
  sync: Record<string, unknown>
  apps: CatalogApp[]
  recipes: Recipe[]
  sources?: CatalogSourceView[]
  expert?: ExpertService
}

export interface SourceState { available: boolean; code?: string; message?: string }
export interface LLMSources { min_context?: number; own: SourceState; local_gateway: SourceState & { apps: string[] }; platform: SourceState }

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
  llm?: LLMSources
}

// PlatformVarView：平台变量（平台按这台机器自动填给应用的模型地址、时区、下载源等）。
// 表在服务端 internal/catalog/platformvars.go；user_can_set 的可以在安装时改。
export interface PlatformVarView {
  name: string
  label: string
  standalone: string
  cloud: string
  user_can_set: boolean
  secret?: boolean
  issued?: boolean
  via_param?: boolean
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
  uses: Use[] | null
  installed: string[]
  command: string
  source?: CatalogSource
  // 要先把 AcornFox 升到这个版本才能装（例如 "0.6.0"）；不用升级是空串，旧服务端没有这一项。
  needs_platform?: string
}

// needs_platform：原因是“AcornFox 版本不够”时有，值是要升到的版本（例如 "0.6.0"）；别的错误没有这个键。
export interface Fault { code: string; message: string; hint?: string; diagnosis?: unknown; task_id?: string; needs_platform?: string }

// StepProgress：正在进行的一步能自己量出进度时有（经下载网关下载镜像包：download；下完导入：load）。
export interface StepProgress { phase: 'download' | 'load' | string; done_bytes: number; total_bytes?: number; bytes_per_sec?: number; eta_seconds?: number }
export interface StepView { app: string; name: string; label: string; status: string; message?: string; started_at?: string; finished_at?: string; progress?: StepProgress }
export interface AppResult { app: string; catalog_id: string; version: string; reused: boolean; created: boolean; url?: string; memory_mb?: number; volumes?: string[]; gateway_token?: { provider: string } }
export interface LoginField { label: string; value: string; secret?: boolean }
export interface FirstLogin { app: string; url?: string; fields: LoginField[] }

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
