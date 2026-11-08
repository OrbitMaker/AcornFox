// 应用中心的数据整理：分类标签、搜索、内存检查、安装请求和对应的命令行写法。
// 都是纯函数，页面只负责显示；单元测试见 catalog.test.ts。
import type { AppDetail, CatalogApp, CatalogView, ParamView, PlatformVarView, Recipe, Resources, TaskView } from './types'
import { mbText } from './format'

// 目录里的分类（清单 schema 的 category 枚举）归到页面上的标签。
const GROUP_OF: Record<string, string> = {
  ai: 'ai', 'ai-agent': 'ai', 'ai-gateway': 'ai',
  automation: 'auto', site: 'site', database: 'db', monitor: 'ops', dev: 'dev', files: 'files',
}
export const GROUPS: [string, string][] = [
  ['ai', 'AI'], ['auto', '自动化'], ['site', '建站'], ['db', '数据库'], ['ops', '监控'], ['dev', '开发'], ['files', '文件'], ['other', '其他'],
]

export function groupOf(category: string): string {
  return GROUP_OF[category] || 'other'
}

// categoryTabs：“全部”加上目录里实际有应用的分类，顺序固定。
export function categoryTabs(apps: CatalogApp[]): [string, string][] {
  const have = new Set(apps.map((a) => groupOf(a.category)))
  return [['all', '全部'], ...GROUPS.filter(([k]) => have.has(k))]
}

export function matchApp(a: CatalogApp, q: string): boolean {
  const s = q.trim().toLowerCase()
  if (!s) return true
  return [a.id, a.name, a.summary, ...(a.keywords || []), ...(a.tags || [])].some((x) => (x || '').toLowerCase().includes(s))
}

export function matchRecipe(r: Recipe, q: string): boolean {
  const s = q.trim().toLowerCase()
  if (!s) return true
  return [r.id, r.title, r.summary, ...(r.keywords || [])].some((x) => (x || '').toLowerCase().includes(s))
}

export function filterApps(apps: CatalogApp[], group: string, q: string): CatalogApp[] {
  return apps.filter((a) => (group === 'all' || groupOf(a.category) === group) && matchApp(a, q))
}

// 应用名的缩写和色相，用于没有图标时的图块。
export function abbr(name: string): string {
  const words: string[] = name.match(/[A-Za-z0-9]+/g) || []
  const [a, b] = words
  if (a && b) return (a.charAt(0) + b.charAt(0)).toUpperCase()
  if (a) return a.charAt(0).toUpperCase() + a.charAt(1)
  return name.slice(0, 1)
}

export function hue(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0
  return h % 360
}

// ---- 内存检查 ----

export interface MemCheck { tone: 'ok' | 'warn' | 'low'; text: string }

// freeMB：整机给应用的内存上限减去已分配的；没有上限时按整机内存算；都不知道时为 null。
export function freeMB(res: Resources | null | undefined): number | null {
  if (!res || !res.memory) return null
  const m = res.memory
  const total = m.cap_mb > 0 ? m.cap_mb : m.host_mb
  if (!total) return null
  return Math.max(0, total - (m.allocated_mb || 0))
}

// memoryCheck：安装时先按建议值留内存，不够就退到最少值，再不够安装会被拒绝（CAT-INST-05）。
export function memoryCheck(needMB: number, minMB: number, free: number | null): MemCheck {
  if (free === null) return { tone: 'ok', text: `要 ${mbText(needMB)}` }
  const head = `要 ${mbText(needMB)} · 剩 ${mbText(free)}`
  if (free >= needMB) return { tone: 'ok', text: head }
  if (free >= minMB) return { tone: 'warn', text: `${head}，按最少 ${mbText(minMB)} 装` }
  return { tone: 'low', text: `${head}，内存不够` }
}

export function appMemory(a: { memory_mb: { min: number; recommended?: number } }): { need: number; min: number } {
  return { need: a.memory_mb.recommended || a.memory_mb.min, min: a.memory_mb.min }
}

// recipeMemory：配方写了合计内存就用它，否则把要新装的应用加起来。
export function recipeMemory(r: Recipe, apps: CatalogApp[]): { need: number; min: number } {
  let need = 0
  let min = 0
  let reused = false
  for (const ra of r.apps) {
    const a = apps.find((x) => x.id === ra.app)
    if (!a) continue
    if (a.installed.length > 0 && ra.reuse_installed !== false) {
      reused = true
      continue
    }
    need += a.memory_mb.recommended || a.memory_mb.min
    min += a.memory_mb.min
  }
  // 配方写的合计内存只在全部新装时用；有复用的应用时按实际要新装的算。
  if (r.delivery.memory_mb && !reused) need = r.delivery.memory_mb
  return { need, min: Math.min(min || need, need) }
}

// ---- 安装表单 ----

export type LLMSource = 'own' | 'local_gateway' | 'platform'

export interface LLMField {
  appId: string
  key: string // 请求里的参数名：单个应用是 llm，配方里是 <应用>.llm
  label: string
  hint?: string
  fixed: string // 配方已经把它连到本机的这个网关（只能改模型）；空表示用户自己选
  sources?: ParamView['llm']
  source: LLMSource
  baseURL: string
  apiKey: string
  model: string
  gatewayApp: string
}

export interface PlainField {
  appId: string
  key: string // 请求里的参数名
  param: ParamView
  value: string
}

// PlatformField：一个平台变量。base 是平台按这台机器填的值；value 是表单里的值（改了才发给服务端）。
export interface PlatformField {
  view: PlatformVarView
  base: string
  value: string
}

export interface InstallForm {
  kind: 'app' | 'recipe'
  id: string
  catalog: string // public 或私有目录编号
  version: string // 空 = 目录里的当前版本
  defaultVersion: string
  plain: PlainField[]
  llm: LLMField[]
  platform: PlatformField[] // 这次装的应用用到的平台变量（配方里几个应用合并，同名只列一次）
}

// defaultModel：没填模型名时用的默认模型（平台变量 llm.model.chat，用户在“平台自动填的”里改过就用改的；
// 旧服务端没有就是 deepseek-chat）。
export function defaultModel(f: InstallForm): string {
  const p = f.platform.find((x) => x.view.name === 'llm.model.chat')
  return p?.value.trim() || p?.base || 'deepseek-chat'
}

// recipeLinkTarget：配方里 app 的大模型参数被连到哪个应用（没有连就是空）。
function recipeLinkTarget(r: Recipe | null, appId: string, d: AppDetail, key: string): string {
  if (!r || !r.links) return ''
  const viaUse = (d.uses || []).some((u) => u.kind === 'llm_gateway' && u.for_param === key)
  if (!viaUse) return ''
  const l = r.links.find((x) => x.to === appId && x.uses === 'llm_gateway')
  return l ? l.from : ''
}

// newInstallForm：按应用详情（配方就是其中每个应用的详情）建表单的初始值。
export function newInstallForm(kind: 'app' | 'recipe', id: string, details: AppDetail[], recipe: Recipe | null, catalog = PUBLIC): InstallForm {
  const prefix = (d: AppDetail) => (kind === 'recipe' ? d.id + '.' : '')
  const form: InstallForm = { kind, id, catalog, version: '', defaultVersion: kind === 'app' && details[0] ? details[0].version : '', plain: [], llm: [], platform: [] }
  for (const d of details) {
    // 配方里已经装过、会被复用的应用不再填参数。
    if (kind === 'recipe' && d.installed.length > 0) {
      const ra = recipe?.apps.find((x) => x.app === d.id)
      if (!ra || ra.reuse_installed !== false) continue
    }
    // 复用的应用不重装，平台变量也不会再写进去，所以只合并要新装的。
    for (const v of d.platform || []) {
      if (form.platform.some((p) => p.view.name === v.name)) continue
      form.platform.push({ view: v, base: v.value || '', value: v.value || '' })
    }
    for (const p of d.params) {
      if (p.type === 'llm_provider') {
        const fixed = recipeLinkTarget(recipe, d.id, d, p.key)
        const localOK = !!p.llm?.local_gateway.available
        form.llm.push({
          appId: d.id, key: prefix(d) + p.key, label: p.label || '大模型', hint: p.hint, fixed, sources: p.llm,
          source: fixed || localOK ? 'local_gateway' : 'own',
          baseURL: '', apiKey: '', model: '', gatewayApp: p.llm?.local_gateway.apps?.[0] || '',
        })
        continue
      }
      form.plain.push({ appId: d.id, key: prefix(d) + p.key, param: p, value: '' })
    }
  }
  return form
}

export interface FormProblem { key: string; message: string }

// checkForm：提交前在页面上能查的（必填、格式）；其余以服务端为准。
export function checkForm(f: InstallForm): FormProblem[] {
  const out: FormProblem[] = []
  for (const p of f.plain) {
    const v = p.value.trim()
    const hasDefault = p.param.default !== undefined && p.param.default !== null && p.param.default !== ''
    if (!v) {
      if (p.param.required && !hasDefault && !p.param.generated) out.push({ key: p.key, message: `请填写${p.param.label || p.param.key}` })
      continue
    }
    if (p.param.pattern) {
      try {
        if (!new RegExp(p.param.pattern).test(v)) out.push({ key: p.key, message: `${p.param.label || p.param.key}的格式不对` })
      } catch {
        /* 目录里的正则浏览器不认就交给服务端查 */
      }
    }
    if (p.param.type === 'secret' && v.length < 8) out.push({ key: p.key, message: `${p.param.label || p.param.key}至少 8 个字符` })
  }
  for (const p of f.platform) {
    if (!p.view.user_can_set) continue
    const v = p.value.trim()
    const key = 'platform.' + p.view.name
    if (!v) out.push({ key, message: `${p.view.label}不能为空（不想改就点“恢复”）` })
    else if (p.view.name.startsWith('mirror.') && !/^https?:\/\/[^\s@]+$/.test(v)) out.push({ key, message: `${p.view.label}要以 http:// 或 https:// 开头` })
    else if (p.view.name === 'tz' && !/^[A-Za-z][A-Za-z0-9_+-]*(\/[A-Za-z0-9_+-]+){0,2}$/.test(v)) out.push({ key, message: '时区要写成 Asia/Shanghai 这样的名字' })
  }
  for (const l of f.llm) {
    if (l.fixed) continue
    if (l.source === 'platform') out.push({ key: l.key, message: '平台 Key 即将开放，请先用自己的 Key 或本机 New API' })
    if (l.source === 'local_gateway' && !l.sources?.local_gateway.available) out.push({ key: l.key, message: l.sources?.local_gateway.message || '本机还没有 New API' })
    if (l.source === 'own') {
      if (!/^https?:\/\/\S+$/.test(l.baseURL.trim())) out.push({ key: l.key + '.base_url', message: '接口地址要以 http:// 或 https:// 开头' })
      if (!l.apiKey.trim()) out.push({ key: l.key + '.api_key', message: '请填写 Key' })
      if (!l.model.trim()) out.push({ key: l.key + '.model', message: '请填写模型名' })
    }
  }
  return out
}

export interface InstallRequest {
  app?: string
  recipe?: string
  version?: string
  catalog?: string
  params?: Record<string, unknown>
  platform?: Record<string, string> // 只放用户改过的平台变量
  reuse_data?: boolean // 接上以前同名应用留下的数据卷（CAT-INST-24 时用户确认后才发）
}

// changedPlatform：用户改过的平台变量（和平台填的值不一样的）。
export function changedPlatform(f: InstallForm): Record<string, string> {
  const out: Record<string, string> = {}
  for (const p of f.platform) {
    const v = p.value.trim()
    if (p.view.user_can_set && v && v !== p.base) out[p.view.name] = v
  }
  return out
}

export function buildRequest(f: InstallForm): InstallRequest {
  const params: Record<string, unknown> = {}
  for (const p of f.plain) {
    const v = p.value.trim()
    if (v) params[p.key] = v
  }
  for (const l of f.llm) {
    const model = l.model.trim()
    if (l.fixed) {
      if (model) params[l.key] = { source: 'local_gateway', model }
      continue
    }
    if (l.source === 'own') params[l.key] = { source: 'own', base_url: l.baseURL.trim(), api_key: l.apiKey.trim(), model }
    else if (l.source === 'local_gateway') {
      const c: Record<string, string> = { source: 'local_gateway' }
      if (l.gatewayApp) c.app = l.gatewayApp
      if (model) c.model = model
      params[l.key] = c
    }
  }
  const req: InstallRequest = f.kind === 'app' ? { app: f.id } : { recipe: f.id }
  if (f.kind === 'app' && f.version && f.version !== f.defaultVersion) req.version = f.version
  if (f.catalog && f.catalog !== PUBLIC) req.catalog = f.catalog
  if (Object.keys(params).length) req.params = params
  const platform = changedPlatform(f)
  if (Object.keys(platform).length) req.platform = platform
  return req
}

// 命令行里不放密码和 Key 本身，用占位写法提醒自己填。
const quote = (v: string) => (/^[A-Za-z0-9._:/@%+-]+$/.test(v) ? v : "'" + v.replace(/'/g, "'\\''") + "'")

export function cliCommand(f: InstallForm): string {
  const parts = ['acornfox install', f.kind === 'app' ? f.id : '--recipe ' + f.id]
  if (f.catalog && f.catalog !== PUBLIC) parts.push('--catalog ' + quote(f.catalog))
  if (f.kind === 'app' && f.version && f.version !== f.defaultVersion) parts.push('--version ' + quote(f.version))
  const llmSources = new Set(f.llm.filter((l) => !l.fixed || l.model.trim()).map((l) => (l.fixed ? 'local_gateway' : l.source)))
  if (llmSources.size === 1) {
    const s = [...llmSources][0]
    parts.push('--llm ' + (s === 'local_gateway' ? 'local' : s))
  }
  for (const p of f.plain) {
    const v = p.value.trim()
    if (!v) continue
    parts.push('--set ' + p.key + '=' + (p.param.type === 'secret' ? '<密码>' : quote(v)))
  }
  for (const l of f.llm) {
    const model = l.model.trim()
    if (!l.fixed && l.source === 'own') {
      parts.push('--set ' + l.key + '.base_url=' + quote(l.baseURL.trim() || '<接口地址>'))
      parts.push('--set ' + l.key + '.api_key=<你的 Key>')
    }
    if (!l.fixed && l.source === 'local_gateway' && l.gatewayApp && (l.sources?.local_gateway.apps?.length || 0) > 1) {
      parts.push('--set ' + l.key + '.app=' + quote(l.gatewayApp))
    }
    if (model) parts.push('--set ' + l.key + '.model=' + quote(model))
  }
  for (const [k, v] of Object.entries(changedPlatform(f))) parts.push('--set platform.' + k + '=' + quote(v))
  return parts.join(' ')
}

// ---- 安装任务 ----

export interface TaskGroup { app: string; steps: TaskView['steps'] }

// groupSteps：按应用分组；配方装几个应用时每组一个小标题。
export function groupSteps(t: TaskView): TaskGroup[] {
  const out: TaskGroup[] = []
  for (const s of t.steps || []) {
    let g = out.find((x) => x.app === s.app)
    if (!g) {
      g = { app: s.app, steps: [] }
      out.push(g)
    }
    g.steps.push(s)
  }
  return out
}

export function taskProgress(t: TaskView): { done: number; total: number } {
  const steps = t.steps || []
  return { done: steps.filter((s) => s.status === 'done' || s.status === 'skipped').length, total: steps.length }
}

// faultForAI：安装失败时交给 AI 的诊断文字，带任务编号、故障编号、失败的那一步和建议。
export function faultForAI(t: TaskView): string {
  const failed = (t.steps || []).find((s) => s.status === 'failed')
  const payload = {
    task_id: t.id,
    target: (t.kind === 'recipe' ? '配方 ' : '应用 ') + t.target,
    code: t.fault?.code || '',
    message: t.fault?.message || '',
    hint: t.fault?.hint || '',
    failed_step: failed ? `${failed.app} · ${failed.label}${failed.message ? '：' + failed.message : ''}` : '',
    diagnosis: t.fault?.diagnosis ?? null,
    kept_volumes: t.keep_volumes_on_failure ? t.apps.flatMap((a) => a.volumes || []) : [],
  }
  return (
    '我在 AcornFox 应用中心安装失败了，请按故障编号帮我排查（可以运行 acornfox install status ' + t.id + ' --json 看详情）：\n' +
    JSON.stringify(payload, null, 2)
  )
}

// installingApps：正在安装的任务涉及的应用名，我的应用列表里标“安装中”。
export function installingApps(tasks: TaskView[]): Set<string> {
  const s = new Set<string>()
  for (const t of tasks) if (t.status === 'running') for (const a of t.apps || []) if (!a.reused) s.add(a.app)
  return s
}

export function recipeAppNames(r: Recipe, cat: CatalogView | null): string[] {
  return r.apps.map((ra) => findApp(cat, ra.app, sourceId(r))?.name || ra.app)
}

// ---- 目录来源 ----
// 同一个 id 可能公共、私有各有一份：页面上用“来源 + id”区分，详情和安装都带上来源（私有的带 catalog 参数）。

export const PUBLIC = 'public'

export function sourceId(x: { source?: { id: string } }): string {
  return x.source?.id || PUBLIC
}

export function appKey(a: CatalogApp): string {
  return sourceId(a) + '/' + a.id
}

// appsIn：某个目录里的应用（配方只用自己目录里的应用）。
export function appsIn(cat: CatalogView | null | undefined, src: string): CatalogApp[] {
  return (cat?.apps || []).filter((a) => sourceId(a) === src)
}

export function findApp(cat: CatalogView | null | undefined, id: string, src = PUBLIC): CatalogApp | undefined {
  return appsIn(cat, src).find((a) => a.id === id)
}

// sourceTabs：有私有目录的应用时才分来源：“全部”“公共目录”和每个私有目录。
export function sourceTabs(cat: CatalogView | null | undefined): [string, string][] {
  const ids = new Set([...(cat?.apps || []), ...(cat?.recipes || [])].map(sourceId))
  if (![...ids].some((x) => x !== PUBLIC)) return []
  const names = new Map<string, string>()
  for (const x of [...(cat?.apps || []), ...(cat?.recipes || [])]) if (x.source) names.set(x.source.id, x.source.name)
  for (const s of cat?.sources || []) if (!names.has(s.id)) names.set(s.id, s.name)
  const priv = [...ids].filter((x) => x !== PUBLIC).sort()
  return [['all', '全部'], [PUBLIC, '公共目录'], ...priv.map((x): [string, string] => [x, names.get(x) || x])]
}

export function filterSource<T extends { source?: { id: string } }>(xs: T[], src: string): T[] {
  return src === 'all' ? xs : xs.filter((x) => sourceId(x) === src)
}

// disconnectedSources：连过、后来被撤销或停用的私有目录（应用中心顶部提示）。
export function disconnectedSources(cat: CatalogView | null | undefined) {
  return (cat?.sources || []).filter((s) => s.kind === 'private' && s.disconnected)
}

// catalogAppOf：我的应用里一个应用对应的目录应用（取图标用）；不是从应用中心装的返回 undefined。
// 从私有目录装的只在那个目录里找：目录断开后找不到就显示首字母图块，不借公共目录同 id 应用的图标。
export function catalogAppOf(cat: CatalogView | null | undefined, a: { catalog_id?: string; catalog_origin?: string }): CatalogApp | undefined {
  if (!a.catalog_id) return undefined
  const origin = a.catalog_origin || PUBLIC
  return findApp(cat, a.catalog_id, origin)
}
