// 测试数据：照程序自带目录（internal/catalog/snapshot）里的两个应用、三个配方写的接口返回。
import type { AppDetail, AppView, CatalogView, Resources, TaskView } from '@/lib/types'

export const catalog: CatalogView = {
  serial: 1,
  origin: 'snapshot',
  sync: { ok: true, skipped: true, message: '' },
  apps: [
    {
      id: 'hermes-agent', version: 'v0.23.0', name: 'Hermes 智能体', summary: '常驻的 AI 智能体，可以接飞书、钉钉、企业微信、微信等做机器人',
      category: 'ai-agent', keywords: ['hermes', '爱马仕'], tags: ['ai', 'agent'], memory_mb: { min: 1024, recommended: 2048 },
      platforms: ['linux/amd64', 'linux/arm64'], size_mb: 1150, delivery: { tier: 'auto', minutes: 3, user_steps: ['打开控制台，用应用详情里的密码登录'] },
      uses: [{ name: 'llm_gateway', kind: 'llm_gateway', for_param: 'llm', optional: true }], provides: [], supported: true, installed: [], command: 'acornfox install hermes-agent',
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
      links: [{ from: 'new-api', to: 'hermes-agent', uses: 'llm_gateway' }], delivery: { tier: 'auto', minutes: 4, memory_mb: 2560, user_steps: ['在 New API 里新建一个渠道'] },
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
  provides: ['llm_gateway'], uses: [], installed: [], command: 'acornfox install new-api',
}

export function hermesDetail(localAvailable: boolean): AppDetail {
  return {
    id: 'hermes-agent', version: 'v0.23.0', versions: ['v0.23.0'], name: 'Hermes 智能体', summary: catalog.apps[0].summary, category: 'ai-agent',
    homepage: '', license: 'MIT', memory_mb: { min: 1024, recommended: 2048 }, disk_mb: { min: 1024 }, platforms: ['linux/amd64', 'linux/arm64'],
    supported: true, size_mb: 1150, image: 'nousresearch/hermes-agent:v0.23.0', port: 9119, exposure: 'local', delivery: catalog.apps[0].delivery,
    params: [{
      key: 'llm', type: 'llm_provider', label: '大模型', hint: '上下文要不少于 64K', required: true, generated: false,
      llm: {
        min_context: 65536, own: { available: true },
        local_gateway: localAvailable ? { available: true, apps: ['new-api'] } : { available: false, code: 'CAT-INST-10', message: '先装 New API', apps: [] },
        platform: { available: false, code: 'CAT-INST-09', message: '即将开放' },
      },
    }],
    provides: [], uses: [{ name: 'llm_gateway', kind: 'llm_gateway', for_param: 'llm', optional: true }], installed: [], command: 'acornfox install hermes-agent',
  }
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
