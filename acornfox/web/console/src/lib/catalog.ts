// 应用中心的数据整理：分类标签、搜索、内存检查、安装请求和对应的命令行写法。
// 都是纯函数，页面只负责显示；单元测试见 catalog.test.ts。
// 应用要用的服务不是表单里的设置项：它们由安装前的计划给出（lib/services.ts），
// 选了哪条写进请求的 use。这里不认识任何一种服务，也不认识任何一个具体的应用。
import type { AppDetail, CatalogApp, CatalogView, ParamView, PlatformVarView, Recipe, Resources, StepProgress, TaskView } from './types'
import { bytesText, etaText, mbText } from './format'

// 目录里的分类（清单 schema 的 category 枚举）归到页面上的标签。
const GROUP_OF: Record<string, string> = {
  ai: 'ai', 'ai-agent': 'ai', 'ai-gateway': 'ai',
  automation: 'auto', site: 'site', database: 'db', monitor: 'ops', dev: 'dev', files: 'files',
  media: 'media', team: 'team', // 影音照片、团队协作这两排：目录里有这类应用才会出现
}
export const GROUPS: [string, string][] = [
  ['ai', 'AI'], ['auto', '自动化'], ['site', '建站'], ['media', '影音照片'], ['team', '团队协作'], ['files', '文件'], ['ops', '数据看板'], ['dev', '开发工具'], ['db', '基础服务'], ['other', '其他'],
]

// isBasicService：数据库、缓存这类只给别的应用用的（货架“基础服务”）。小白单独装它没用，
// 应用中心的“全部”里不摆，只在“基础服务”那一格和搜索结果里看得到（样稿第 10 版，评估 I4）。
export function isBasicService(a: { category: string }): boolean {
  return groupOf(a.category) === 'db'
}

// shortName：去掉名字末尾的大版本号，按钮上用（“MySQL 8.4” → “MySQL”，“PostgreSQL 16” → “PostgreSQL”）。
export function shortName(name: string): string {
  return name.replace(/\s+v?\d+(\.\d+)*$/i, '') || name
}

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

// ---- 展示内容（经营系统签名发布，GET /v1/catalog 里每个应用和配方的 show）----

// applyShow：读到目录时过一遍，把展示内容里的名字和搜索词并进应用和配方本身，页面别的地方照旧用 name、title、keywords。
// 解决方案挂的行业名也并进它的搜索词（show.industries 里是行业 id，名字在目录顶层的 industries）。没有展示内容的原样放过。
export function applyShow(v: CatalogView): CatalogView {
  const names = new Map((v.industries || []).map((i) => [i.id, i.name]))
  const join = <K extends string[] | null | undefined>(kw: K, extra: string[]): K | string[] => (extra.length ? [...new Set([...(kw || []), ...extra])] : kw)
  return {
    ...v,
    apps: (v.apps || []).map((a) => (a.show ? { ...a, name: a.show.name || a.name, keywords: join(a.keywords, a.show.keywords || []) } : a)),
    recipes: (v.recipes || []).map((r) => (r.show
      ? { ...r, title: r.show.name || r.title, keywords: join(r.keywords, [...(r.show.keywords || []), ...(r.show.industries || []).map((i) => names.get(i) || '').filter(Boolean)]) }
      : r)),
  }
}

// industryName：行业 id 换成给人看的名字；目录里没有这个行业（没有展示内容、已停用）时是空串，页面不显示它。
export function industryName(cat: CatalogView | null | undefined, id: string): string {
  return cat?.industries?.find((i) => i.id === id)?.name || ''
}

// industryNames：一个解决方案挂的行业名（按 id 顺序，认不得的略过）。
export function industryNames(cat: CatalogView | null | undefined, r: Recipe): string[] {
  return (r.show?.industries || []).map((id) => industryName(cat, id)).filter(Boolean)
}

// industriesOf：这些解决方案里出现过的行业和各有几个，按排好的顺序（目录顶层 industries 的顺序），一个都没有的不列。
export function industriesOf(cat: CatalogView | null | undefined, list: Recipe[]): { id: string; name: string; n: number }[] {
  return (cat?.industries || [])
    .map((i) => ({ id: i.id, name: i.name, n: list.filter((r) => (r.show?.industries || []).includes(i.id)).length }))
    .filter((x) => x.n > 0)
}

// sortByShowOrder：同一排里按展示内容的 order 排，小的在前，没排（0 或没有）的在后面，各自保持原来的顺序。
export function sortByShowOrder<T extends { show?: { order?: number } }>(list: T[]): T[] {
  const key = (x: T) => (x.show?.order && x.show.order > 0 ? x.show.order : Infinity)
  return list.map((x, i) => [x, i] as const).sort(([a, i], [b, j]) => (key(a) < key(b) ? -1 : key(a) > key(b) ? 1 : i - j)).map(([x]) => x)
}

// orderSolutions：首页和“全部解决方案”里解决方案的顺序。目录说了首页摆哪几个（home.solutions）就按它的顺序排在最前面；
// 没说时标了“推荐”的在前；剩下的按展示内容的 order，再按目录里的顺序。
export function orderSolutions(list: Recipe[], home?: string[]): Recipe[] {
  const head: Recipe[] = home && home.length > 0
    ? home.map((id) => list.find((r) => r.id === id)).filter((r): r is Recipe => !!r)
    : sortByShowOrder(list.filter((r) => r.show?.flag === 'pick'))
  return [...head, ...sortByShowOrder(list.filter((r) => !head.includes(r)))]
}

export function filterApps(apps: CatalogApp[], group: string, q: string): CatalogApp[] {
  return apps.filter((a) => (group === 'all' || groupOf(a.category) === group) && matchApp(a, q))
}

// licenseText：许可证写成人话（评估 J3），原名放在悬停提示里。上架的规矩是“允许用户自己部署”，
// 所以都写“可以自己部署”；应用自己定的许可（LicenseRef-…）写“某某自己的许可”。
export function licenseText(license: string, appName: string): string {
  if (!license) return ''
  if (/LicenseRef-/i.test(license)) return `${shortName(appName)} 自己的许可（可以自己部署）`
  return '开源许可（可以自己部署）'
}

// ---- 旧格式的清单、空目录 ----

// 目录里还是旧格式清单的应用（列表里 former 为 true）：这一版装不了，详情和安装都报这个编号（HTTP 409）。
export const FORMER_CODE = 'CAT-INST-36'
const FORMER_FALLBACK = '这是旧格式的清单，这一版装不了'

// formerNote：应用或配方是旧格式的清单时给用户看的那句话（服务端的 note 原样用；没给时用一句通用的）；不是时为空串。
export function formerNote(x: { former?: boolean; note?: string } | null | undefined): string {
  if (x?.former !== true) return ''
  return (typeof x.note === 'string' && x.note.trim()) || FORMER_FALLBACK
}

// isFormerFault：读详情或安装被拒绝的原因是“旧格式的清单”。只认编号：这个编号只有这一种意思，也不带 needs_platform。
export function isFormerFault(e: unknown): boolean {
  return !!e && typeof e === 'object' && (e as { code?: unknown }).code === FORMER_CODE
}

// storeEmpty：应用中心列表一个都没有时怎么说。用户没在搜索、目录里一个应用都没有：还没有可以安装的应用；
// 搜索没结果：没找到（页面再加“换个说法试试”和“看全部应用”按钮，评估 I3）。列表里有东西时为 null。
export function storeEmpty(total: number, shown: number, q: string): { title: string; sub?: string; searching: boolean } | null {
  if (shown > 0) return null
  const s = q.trim()
  if (!s && total === 0) return { title: '还没有可以安装的应用', sub: '应用按新格式上架后会出现在这里', searching: false }
  return { title: s ? `没找到“${s}”` : '没找到', searching: true }
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
  platform: PlatformField[] // 这次装的应用用到的平台变量（配方里几个应用合并，同名只列一次）
  // 谁能打开（W3-5）：expose 是用户选的（local、lan、public），exposeDefault 是服务端说这次装会得到的（应用详情的 exposure）。
  // 两个一样就不发给服务端；exposeDefault 是空串：这次装的都是只在内部网络的（数据库这类），没有这一项可选。
  expose: string
  exposeDefault: string
}

// 能选的三档（internal、afx_cloud 不在这里）。
const CHOOSABLE = ['local', 'lan', 'public']

// newInstallForm：按应用详情（配方就是其中每个应用的详情）建表单的初始值。
export function newInstallForm(kind: 'app' | 'recipe', id: string, details: AppDetail[], recipe: Recipe | null, catalog = PUBLIC): InstallForm {
  const prefix = (d: AppDetail) => (kind === 'recipe' ? d.id + '.' : '')
  const form: InstallForm = { kind, id, catalog, version: '', defaultVersion: kind === 'app' && details[0] ? details[0].version : '', plain: [], platform: [], expose: '', exposeDefault: '' }
  for (const d of details) {
    // 配方里已经装过、会被复用的应用不再填参数。
    if (kind === 'recipe' && d.installed.length > 0) {
      const ra = recipe?.apps.find((x) => x.app === d.id)
      if (!ra || ra.reuse_installed !== false) continue
    }
    // 谁能打开：以第一个要新装、又不是只在内部网络的应用的默认为准
    if (!form.exposeDefault && CHOOSABLE.includes(d.exposure)) form.exposeDefault = d.exposure
    // 复用的应用不重装，平台变量也不会再写进去，所以只合并要新装的。
    for (const v of d.platform || []) {
      if (form.platform.some((p) => p.view.name === v.name)) continue
      form.platform.push({ view: v, base: v.value || '', value: v.value || '' })
    }
    for (const p of d.params) {
      form.plain.push({ appId: d.id, key: prefix(d) + p.key, param: p, value: '' })
    }
  }
  form.expose = form.exposeDefault
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
  use?: Record<string, string> // 某个需要接哪条服务（计划里不止一个应用时，键是“应用实例名.需要的名字”）
  expose?: string // 谁能打开：local、lan、public；不写就是这台机器给新装应用的默认（只在用户改了时才发）
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
  const req: InstallRequest = f.kind === 'app' ? { app: f.id } : { recipe: f.id }
  if (f.kind === 'app' && f.version && f.version !== f.defaultVersion) req.version = f.version
  if (f.catalog && f.catalog !== PUBLIC) req.catalog = f.catalog
  if (Object.keys(params).length) req.params = params
  const platform = changedPlatform(f)
  if (Object.keys(platform).length) req.platform = platform
  if (f.exposeDefault && f.expose && f.expose !== f.exposeDefault) req.expose = f.expose
  return req
}

// 命令行里不放密码和 Key 本身，用占位写法提醒自己填。
export const quote = (v: string) => (/^[A-Za-z0-9._:/@%+-]+$/.test(v) ? v : "'" + v.replace(/'/g, "'\\''") + "'")

export function cliCommand(f: InstallForm): string {
  const parts = ['acornfox install', f.kind === 'app' ? f.id : '--recipe ' + f.id]
  if (f.catalog && f.catalog !== PUBLIC) parts.push('--catalog ' + quote(f.catalog))
  if (f.kind === 'app' && f.version && f.version !== f.defaultVersion) parts.push('--version ' + quote(f.version))
  for (const p of f.plain) {
    const v = p.value.trim()
    if (!v) continue
    parts.push('--set ' + p.key + '=' + (p.param.type === 'secret' ? '<密码>' : quote(v)))
  }
  for (const [k, v] of Object.entries(changedPlatform(f))) parts.push('--set platform.' + k + '=' + quote(v))
  if (f.exposeDefault && f.expose && f.expose !== f.exposeDefault) parts.push('--expose ' + f.expose)
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

// plainStep：安装步骤给小白看的说法（样稿第 10 版，评估 L6）。服务端的步骤名和说明里有“镜像”这类词，页面上换成用户的话；
// 不认识的原样显示。
const STEP_WORDS: [RegExp, string][] = [[/下载镜像/g, '下载应用'], [/国内镜像源/g, '国内线路'], [/镜像/g, '应用程序'], [/容器/g, '应用']]
export function plainStep(text: string | undefined): string {
  let s = text || ''
  for (const [re, to] of STEP_WORDS) s = s.replace(re, to)
  return s
}

// progressLine：正在进行的一步能量出进度时，画一条进度条和下面一行字（W3-6）。
// pct 是 null：量不出多少（下完在导入、还在挑下载线路、Docker 这会儿没报进度）时条是来回走的。
// 字只写用户看得懂的：已下多少 / 一共多少、现在多快、还要多久；一阵子没收到数据时说一声。
// 下载应用程序有两条路：经下载网关（download，下完还要导入 load），或从公共下载源拉（pull）；都按字节算。
export function progressLine(p: StepProgress): { pct: number | null; text: string; stalled: boolean } {
  const total = p.total_bytes || 0
  const done = Math.max(0, p.done_bytes || 0)
  const rate = p.bytes_per_sec || 0
  if (p.phase === 'load') return { pct: null, text: '下好了，正在导入', stalled: false }
  if (p.phase === 'pull' && !p.source && !total) return { pct: null, text: '正在挑下载最快的线路', stalled: false }
  const pct = total > 0 ? Math.min(100, Math.floor((done * 100) / total)) : null
  const parts: string[] = []
  if (total > 0) parts.push(bytesText(done) + ' / ' + bytesText(total))
  else if (done > 0) parts.push('已下 ' + bytesText(done))
  if (p.blind) {
    parts.push('这会儿看不到下到多少，还在下')
    return { pct, text: parts.join(' · '), stalled: false }
  }
  if (rate > 0) {
    parts.push(bytesText(rate) + '/秒')
    const eta = p.eta_seconds || (total > done ? Math.round((total - done) / rate) : 0)
    if (eta > 0) parts.push('剩约 ' + etaText(eta))
  }
  const stalled = (p.stalled_seconds || 0) >= 15
  if (stalled) parts.push('已有 ' + p.stalled_seconds + ' 秒没收到新数据')
  return { pct, text: parts.join(' · '), stalled }
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
    // 原因是版本不够时才有：要升到的版本（建议里说的 needs_platform 就是它）。
    ...(t.fault?.needs_platform ? { needs_platform: t.fault.needs_platform } : {}),
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

// askInstallText：“让 AI 帮我装”复制出去的那段话，粘给用户的 AI 工作台。
// 有操作手册（show.skill）：叫它先取手册再照着做；没有（或来自私有目录，私有目录没有手册）：说明照通用 Skill 做，
// 只让它顺手看一眼有没有。手册和通用规则冲突时以通用规则为准（写死在这段话里）。
// 命令里的 acornfox 是服务器上的命令行，AI 工作台通过它连到这台服务器。
export function askInstallText(kind: 'app' | 'recipe', x: { id: string; command: string; show?: { skill?: unknown }; source?: { id: string; name?: string } }, name: string): string {
  const what = kind === 'recipe' ? '解决方案' : '应用'
  const flag = kind === 'recipe' ? ' --recipe' : ''
  const fetch = `acornfox --json catalog skill ${x.id}${flag}`
  const head = `请帮我在 AcornFox 应用中心里安装${what}“${name}”（编号 ${x.id}）。安装命令是 ${x.command}，先加 --plan 看计划，我确认后再装。`
  if (x.show?.skill && sourceId(x) === PUBLIC) {
    return `${head}\n先运行 ${fetch} 取它的操作手册，照着做；手册和通用 Skill 的规则冲突时，以通用 Skill 为准。`
  }
  return `${head}\n这个${what}目前没有专门的操作手册，请照通用 Skill 里“从应用中心装应用”那一节做（也可以先运行 ${fetch} 看一眼，说没有就照通用的做）。`
}
