// 测试数据：照第 3 版清单写的两个应用（一个需要大模型接口、一个能提供它）和三个配方的接口返回。
// 应用要用的服务只出现在“需要的服务”（uses）里，没有原来的大模型设置项。
import type { AppDetail, AppView, CatalogView, InstallPlan, PlanNeed, Resources, ServiceKind, ServiceView, TaskView } from '@/lib/types'

export const catalog: CatalogView = {
  serial: 1,
  origin: 'snapshot',
  sync: { ok: true, skipped: true, message: '' },
  apps: [
    {
      id: 'hermes-agent', version: 'v0.23.0', name: 'Hermes 智能体', summary: '常驻的 AI 智能体，可以接飞书、钉钉、企业微信、微信等做机器人',
      category: 'ai-agent', keywords: ['hermes', '爱马仕'], tags: ['ai', 'agent'], memory_mb: { min: 1024, recommended: 2048 },
      platforms: ['linux/amd64', 'linux/arm64'], size_mb: 1150, delivery: { tier: 'auto', minutes: 3, user_steps: ['打开 Hermes 的页面，用 AcornFox 控制台应用详情里的密码登录'] },
      uses: [{ name: 'llm', kind: 'llm_gateway' }], provides: [], supported: true, installed: [], command: 'acornfox install hermes-agent',
    },
    {
      id: 'new-api', version: 'v1.0.0-rc.48', name: 'New API', summary: '把多家大模型合成一个 OpenAI 兼容的接口，统一管理 Key、令牌和额度',
      category: 'ai-gateway', keywords: [], tags: ['ai', 'llm', 'gateway'], memory_mb: { min: 256, recommended: 512 },
      platforms: ['linux/amd64', 'linux/arm64'], size_mb: 78, delivery: { tier: 'auto', minutes: 1, user_steps: ['登录后在“渠道”里新建一个渠道'] },
      uses: [], provides: ['llm_gateway'], supported: true, installed: [], command: 'acornfox install new-api',
    },
  ],
  recipes: [
    { id: 'hermes-agent', title: 'Hermes 智能体', summary: '常驻的 AI 智能体', apps: [{ app: 'hermes-agent' }], delivery: { tier: 'auto', minutes: 3, memory_mb: 2048 }, command: 'acornfox install --recipe hermes-agent' },
    {
      id: 'hermes-with-new-api', title: 'Hermes 智能体 + New API', summary: '装好智能体和模型网关并连起来', apps: [{ app: 'new-api' }, { app: 'hermes-agent' }],
      links: [{ from: 'new-api', to: 'hermes-agent', uses: 'llm' }], delivery: { tier: 'auto', minutes: 4, memory_mb: 2560, user_steps: ['在 New API 里新建一个渠道'] },
      command: 'acornfox install --recipe hermes-with-new-api',
    },
    { id: 'new-api', title: 'New API 模型网关', summary: '', apps: [{ app: 'new-api' }], delivery: { tier: 'auto', minutes: 1, memory_mb: 512 }, command: 'acornfox install --recipe new-api' },
  ],
}

// 目录里有“要更新版本的 AcornFox 才能装”的应用时的样子：Hermes 要 0.6.0，带它的两个配方跟着要；New API 不受影响。
// 服务端不用升级时给空串，旧服务端没有这个字段，两种都留着。
export const NEEDS = '0.6.0'
export const catalogNeedsUpgrade: CatalogView = {
  ...catalog,
  apps: catalog.apps.map((a) => (a.id === 'hermes-agent' ? { ...a, needs_platform: NEEDS } : { ...a, needs_platform: '' })),
  recipes: catalog.recipes.map((r) => (r.apps.some((x) => x.app === 'hermes-agent') ? { ...r, needs_platform: NEEDS } : r)),
}

export const newApiDetail: AppDetail = {
  id: 'new-api', version: 'v1.0.0-rc.48', versions: ['v1.0.0-rc.48'], name: 'New API', summary: catalog.apps[1].summary, category: 'ai-gateway',
  homepage: 'https://github.com/QuantumNous/new-api', license: 'AGPL-3.0', icon: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>',
  memory_mb: { min: 256, recommended: 512 }, disk_mb: { min: 512 }, platforms: ['linux/amd64', 'linux/arm64'], supported: true, size_mb: 78,
  image: 'calciumion/new-api:v1.0.0-rc.48', port: 3000, exposure: 'local', delivery: catalog.apps[1].delivery,
  params: [
    { key: 'admin_username', type: 'string', label: '管理员用户名', default: 'admin', pattern: '^[A-Za-z0-9_]{1,12}$', required: false, generated: false, editable: 'never' },
    { key: 'admin_password', type: 'secret', label: '管理员密码', hint: '留空就自动生成', required: false, generated: true, editable: 'never' },
  ],
  provides: ['llm_gateway'], provides_detail: [{ kind: 'llm_gateway', kind_label: '大模型接口' }], uses: [], installed: [], command: 'acornfox install new-api',
}

// hermesDetail：需要大模型接口的应用。要接哪条由安装前的计划决定，表单里没有它的设置项。
export const hermesDetail: AppDetail = {
  id: 'hermes-agent', version: 'v0.23.0', versions: ['v0.23.0'], name: 'Hermes 智能体', summary: catalog.apps[0].summary, category: 'ai-agent',
  homepage: '', license: 'MIT', memory_mb: { min: 1024, recommended: 2048 }, disk_mb: { min: 1024 }, platforms: ['linux/amd64', 'linux/arm64'],
  supported: true, size_mb: 1150, image: 'nousresearch/hermes-agent:v0.23.0', port: 9119, exposure: 'local', delivery: catalog.apps[0].delivery,
  params: [],
  provides: [], provides_detail: [],
  uses: [{ name: 'llm', kind: 'llm_gateway', use: 'llm', kind_label: '大模型接口', required: true }],
  installed: [], command: 'acornfox install hermes-agent',
}

// hermesWithModel：照第 3 版清单草稿，模型名是这个应用自己的一个普通必填设置项（没有默认值、不自动生成），
// 和别的设置项一样填；需要的服务仍只在“需要的服务”里接。
export const modelParam = { key: 'model', type: 'string', label: '模型名', hint: '要调用的模型，上下文要不少于 64K，例如 qwen-plus', required: true, generated: false }
export const hermesWithModel: AppDetail = { ...hermesDetail, params: [modelParam] }
// 配方（先装提供服务的应用，再装要它的）里每个应用的详情。
export const recipeDetailsWithModel: AppDetail[] = [newApiDetail, hermesWithModel]

// 目录里还是旧格式清单的应用和配方（former）：服务端给的那句话原样显示。
export const FORMER_NOTE = '这是旧格式的清单，这一版装不了，等这个应用按新格式重新上架'
export const catalogFormer: CatalogView = {
  ...catalog,
  apps: catalog.apps.map((a) => (a.id === 'hermes-agent' ? { ...a, former: true, note: FORMER_NOTE } : a)),
  recipes: catalog.recipes.map((r) => (r.apps.some((x) => x.app === 'hermes-agent') ? { ...r, former: true, note: FORMER_NOTE } : r)),
}

export const resources: Resources = {
  memory: { host_mb: 8192, cap_mb: 6144, auto: true, allocated_mb: 1024 },
  cpu: { host_milli: 4000, cap_milli: 4000, auto: true, max_app_milli: 1000 },
  items: [{ app: 'web', kind: 'app', memory_mb: 1024, cpu_milli: 1000 }],
  min_app_memory_mb: 256,
  min_app_cpu_milli: 250,
}

export function app(over: Partial<AppView> = {}): AppView {
  return {
    name: 'web', desired: 'running', port: 3000, health_path: '/', public_port: 18810, current_deployment: 'aaaaaaaaaaaa', memory_mb: 512, cpu_milli: 1000,
    url: 'http://localhost:18810', exposure: 'local', observed_state: 'running', rollback_target_seq: 0,
    observed: { name: 'af-web-aaaaaaaaaaaa', state: 'running', running: true, restart_count: 0, host_port: 18810 },
    live: { id: 'aaaaaaaaaaaa', app: 'web', seq: 1, source_kind: 'image', source_ref: 'nginx', status: 'live', created_at: '2026-11-06T01:00:00Z' },
    ...over,
  }
}

export function task(over: Partial<TaskView> = {}): TaskView {
  return {
    id: 't123', kind: 'app', target: 'new-api', status: 'running', created_at: '2026-11-06T01:00:00Z', updated_at: '2026-11-06T01:00:05Z',
    steps: [
      { app: 'new-api', name: 'check', label: '检查', status: 'done' },
      { app: 'new-api', name: 'image', label: '下载镜像', status: 'running', message: '从国内源下载' },
      { app: 'new-api', name: 'start', label: '启动', status: 'pending' },
    ],
    apps: [{ app: 'new-api', catalog_id: 'new-api', version: 'v1.0.0-rc.48', reused: false, created: true }],
    ...over,
  }
}

// ---- 服务（格式照接口约定写：GET /v1/services/kinds、GET /v1/services、POST /v1/catalog/plan） ----

export const serviceKinds: ServiceKind[] = [
  {
    kind: 'llm_gateway', label: '大模型接口', summary: '给应用用的 OpenAI 兼容接口',
    fields: [
      { key: 'base_url', label: '接口地址', type: 'url', required: true, secret: false, default: '', options: [], hint: '例如 https://api.deepseek.com/v1' },
      { key: 'api_key', label: 'Key', type: 'string', required: true, secret: true, default: '', options: [], hint: '' },
      { key: 'model', label: '模型', type: 'string', required: false, secret: false, default: '', options: [], hint: '' },
    ],
    has_test: true,
  },
  {
    kind: 'smtp', label: '邮箱（发信）', summary: '给应用发通知邮件用的发信服务器',
    fields: [
      { key: 'host', label: '服务器地址', type: 'string', required: true, secret: false, default: '', options: [], hint: '' },
      { key: 'port', label: '端口', type: 'int', required: false, secret: false, default: 587, options: [], hint: '' },
      { key: 'username', label: '账号', type: 'string', required: false, secret: false, default: '', options: [], hint: '' },
      { key: 'password', label: '密码', type: 'string', required: false, secret: true, default: '', options: [], hint: '' },
      { key: 'tls', label: '加密方式', type: 'enum', required: false, secret: false, default: 'starttls', options: ['starttls', 'tls', 'none'], hint: '一般用 starttls' },
    ],
    has_test: true,
  },
  {
    kind: 'demo_token', label: '演示令牌', summary: '只为测试',
    fields: [
      { key: 'url', label: '地址', type: 'url', required: true, secret: false, default: '', options: [], hint: '' },
      { key: 'token', label: '令牌', type: 'string', required: true, secret: true, default: '', options: [], hint: '' },
    ],
    has_test: false,
  },
]

// 四种状态各一条：自己登记的邮箱（可用、有应用在用）；New API 提供的大模型接口（还差 Key，要在应用里做一步）；
// 提供它的应用停着；提供它的应用已删。
export const services: ServiceView[] = [
  {
    name: '我的自建邮箱', kind: 'smtp', kind_label: '邮箱（发信）', source: 'user', app: '', status: 'ok', status_label: '可用', missing: [],
    values: [
      { key: 'host', label: '服务器地址', value: 'mail.example.com', secret: false, set: true, ask: '' },
      { key: 'port', label: '端口', value: '587', secret: false, set: true, ask: '' },
      { key: 'password', label: '密码', value: '••••1a2b', secret: true, set: true, ask: '' },
    ],
    used_by: [{ app: 'memos', use: 'mail' }], created_at: '2026-11-12T01:00:00Z', updated_at: '2026-11-12T01:00:00Z',
  },
  {
    name: 'new-api', kind: 'llm_gateway', kind_label: '大模型接口', source: 'app', app: 'new-api', status: 'incomplete', status_label: '还差 1 项', missing: ['api_key'],
    values: [
      { key: 'base_url', label: '接口地址', value: 'http://new-api:3000/v1', secret: false, set: true, ask: '' },
      { key: 'api_key', label: 'Key', value: '', secret: true, set: false, ask: '登录 New API，在“令牌”里新建一把，贴进来' },
    ],
    used_by: [], created_at: '2026-11-12T01:00:00Z', updated_at: '2026-11-12T01:00:00Z',
  },
  {
    name: 'other-api', kind: 'llm_gateway', kind_label: '大模型接口', source: 'app', app: 'other-api', status: 'provider_stopped', status_label: '提供它的应用停着', missing: [],
    values: [{ key: 'base_url', label: '接口地址', value: 'http://other-api:3000/v1', secret: false, set: true, ask: '' }],
    used_by: [{ app: 'hermes-agent', use: 'llm' }],
  },
  {
    name: 'old-api', kind: 'llm_gateway', kind_label: '大模型接口', source: 'app', app: 'old-api', status: 'provider_gone', status_label: '提供它的应用已删', missing: [],
    values: [], used_by: [],
  },
]

// needOf：计划里的一个需要（默认是 Hermes 的大模型接口）。
export function needOf(over: Partial<PlanNeed>): PlanNeed {
  return { use: 'llm', kind: 'llm_gateway', kind_label: '大模型接口', required: true, status: 'matched', service: '', candidates: [], without: '', how: [], ...over }
}

// planOf：装 Hermes 的计划，只有一个需要。
export function planOf(need: PlanNeed, over: Partial<InstallPlan> = {}): InstallPlan {
  return {
    can_install: true, say: '',
    apps: [{ id: 'hermes-agent', name: 'hermes-agent', version: 'v0.23.0', needs: [need], params_missing: [], provides: [] }],
    problems: [],
    ...over,
  }
}
