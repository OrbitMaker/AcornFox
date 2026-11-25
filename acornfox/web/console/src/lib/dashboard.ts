// 首页的数据整理。两处在用：
//   ① 首页上面几块（pages/home/）：“需要你处理”（todoItems）、“最近发生的事”（eventItems）——铃铛（顶栏）也用 todoItems，
//      所以首页、铃铛、菜单红点同一个来源、同一句话；
//   ② 首页最下面默认收起的“图表”（pages/home/chart-board.tsx）：可选的数据（SOURCES）、每种数据能用的图表方式（typesFor）、布局的读写。
// 数据只用服务端真有的接口，没有来源的样稿图表不放进来（见 README“首页看板”）：
//   这台机器：CPU、内存（GET /v1/metrics/host/history，近 1 小时、24 小时；只存在内存里，没有 7 天）、
//             磁盘（GET /v1/metrics/host）、已分配内存（GET /v1/resources）
//   应用：应用状态（GET /v1/apps）、各应用 CPU / 内存（GET /v1/metrics/apps/{app}；再加一项“其他容器”：这台机器上不是从 AcornFox 装的容器在跑的加起来，GET /v1/host/containers）
//   提醒与记录：需要你处理（应用状态、体检、失败的安装）、最近发生的事（安装和更新记录、安装任务）
// 样稿第 10 版（评估 C2、C5、C6、X3、X4、X8）：页面上只写人话和应用中心里的名字；故障编号、任务号、诊断只放进 ListItem.ask
// （“复制给 AI 工作台”复制的那段）；不写“第几版”；家里的机器不在公网上（NET-LAN-01）不算要处理的事。
// 都是纯函数，页面只负责取数和显示；单元测试见 dashboard.test.ts（第 10 版改了文字，测试还没跟上）。
import { deployDiagnosisForAI, deriveStatus, PENDING } from './appstatus'
import { problemLine } from './display'
import type { AppMetrics, AppView, Deployment, HostHistory, HostView, OtherContainersView, PlatformUpdate, Resources, RuntimeCheck, RuntimeChecks, TaskView } from './types'
import { appHref } from './router'
import { OTHERS_NAME, othersForChart } from './others'

export type RangeKey = '1h' | '24h'
export const RANGE_NAME: Record<string, string> = { '1h': '近 1 小时', '24h': '近 24 小时' }
export const RANGE_TAB: Record<string, string> = { '1h': '1 小时', '24h': '24 小时' }

export interface Label { s: string; l: string } // s：坐标轴上的短写法；l：提示里的完整写法
export interface Series { key: string; name: string; color: string; data: (number | null)[] }
// href：这一项能点（状态数里的“要处理”点了去“我的应用”）；只在数不是 0 时做成链接。
export interface CatItem { name: string; value: number; color: string; href?: string }
// ask：给“复制给 AI 工作台”的那段文字（故障编号、诊断都在这里，页面上不单独显示）。
export interface ListItem { key: string; tone?: 'danger' | 'warning'; icon: string; title: string; sub?: string; time?: string; href?: string; action?: string; ask?: string; task?: { kind: 'app' | 'recipe'; id: string; taskId: string } }

export type Data =
  | { kind: 'ts'; unit: string; max?: number; maxLabel?: string; labels: Label[]; series: Series[] }
  | { kind: 'cat'; unit: string; status?: boolean; items: CatItem[] }
  | { kind: 'value'; unit: string; value: number; max: number; label: string; sub: string; tone?: 'warning' | 'danger' }
  | { kind: 'list'; type: 'todo' | 'events'; items: ListItem[]; empty: string }
  | { kind: 'none'; text: string }

// DashCtx：看板用到的全部接口数据；没取到的是 null。
export interface DashCtx {
  now: Date
  range: RangeKey
  host: HostView | null
  history: HostHistory | null
  apps: AppView[]
  installing: Set<string>
  metrics: Record<string, AppMetrics>
  // others：这台机器上不是从 AcornFox 装的容器（GET /v1/host/containers）；读不到（旧服务端、Docker 没回应）是 null。各应用内存、CPU 图里合成一项“其他容器”，总数才对得上。没给当作 null。
  others?: OtherContainersView | null
  resources: Resources | null
  checks: RuntimeChecks | null
  deployments: Deployment[]
  tasks: TaskView[]
  // nameOf：应用代号（或应用中心里的应用、解决方案编号）→ 页面上显示的名字。没给时原样显示代号。
  nameOf?: (id: string) => string
  // behind：设置落在后面的应用（平台自动填的值变了、应用里用的还是以前的，GET /v1/platform/affected）。没给当作没有。
  behind?: { app: string; blocked?: string }[]
  // platformUpdate：服务端查到的 AcornFox 新版本（GET /v1/status 的 platform_update）；updateSeen：用户已经看过提醒的那个版本（lib/update-seen.ts）。
  // 有新版本、而且这个版本用户还没看过，才在“需要你处理”里出一条。没给当作没有。
  platformUpdate?: PlatformUpdate | null
  updateSeen?: string
  // loading：哪几样数据第一次还没读到（样稿第 14 版）。用到它的图表这时画占位形状，不先写“读不到磁盘”这类话。没给当作都读到了。
  loading?: Partial<Record<Need, boolean>>
}

// 图表要的数据分这几样（和 chart-board.tsx 的 useDashData 里的几个请求一一对应）。
export type Need = 'host' | 'history' | 'apps' | 'metrics' | 'resources' | 'checks' | 'deployments' | 'others'

const nameIn = (ctx: { nameOf?: (id: string) => string }, id: string) => (ctx.nameOf ? ctx.nameOf(id) : id)

export interface SourceDef {
  name: string
  group: '这台机器' | '应用' | '提醒与记录'
  icon: 'cpu' | 'memory' | 'disk' | 'grid' | 'bell' | 'history'
  fixed?: string // 不随时间范围变的数据：标题旁显示它，例如“现在”
  history?: boolean // 要整机历史（服务端没有历史时不能选）
  needs: Need[] // 用到哪几样数据（还没读到时画占位形状）
  gen: (ctx: DashCtx) => Data
}

// sourceLoading：这张图要的数据是不是还有没读到的。
export function sourceLoading(key: string, ctx: DashCtx): boolean {
  const s = SOURCES[key]
  return !!s && !!ctx.loading && s.needs.some((n) => ctx.loading![n])
}

const p2 = (n: number) => String(n).padStart(2, '0')
const GB = 1024 ** 3

export function pointLabel(iso: string): Label {
  const d = new Date(iso)
  const hm = `${p2(d.getHours())}:${p2(d.getMinutes())}`
  return { s: hm, l: `${p2(d.getMonth() + 1)}-${p2(d.getDate())} ${hm}` }
}

const round = (v: number, dec = 1) => Math.round(v * 10 ** dec) / 10 ** dec

function noHistory(ctx: DashCtx): Data | null {
  if (!ctx.history || ctx.history.ranges.length === 0) return { kind: 'none', text: '这台机器没有记录历史' }
  if (ctx.history.points.length === 0) return { kind: 'none', text: '刚开始记录，半分钟后再看' }
  return null
}

// 应用的固定颜色：按名字分到 8 个颜色里，同一个应用在各张图上颜色一样。
export function appColor(name: string, all: string[]): string {
  const i = [...all].sort().indexOf(name)
  return i < 0 ? 'var(--s-other)' : `var(--s${(i % 8) + 1})`
}

export const SOURCES: Record<string, SourceDef> = {
  cpu: {
    name: 'CPU 使用率', group: '这台机器', icon: 'cpu', history: true, needs: ['history', 'host'],
    gen: (ctx) => noHistory(ctx) || {
      kind: 'ts', unit: '%', max: 100, maxLabel: ctx.host?.cpu_cores ? ctx.host.cpu_cores + ' 核' : undefined,
      labels: ctx.history!.points.map((p) => pointLabel(p.t)),
      series: [{ key: 'cpu', name: 'CPU', color: 'var(--s1)', data: ctx.history!.points.map((p) => (p.cpu_percent === undefined ? null : round(p.cpu_percent))) }],
    },
  },
  mem: {
    name: '内存', group: '这台机器', icon: 'memory', history: true, needs: ['history'],
    gen: (ctx) => {
      const none = noHistory(ctx)
      if (none) return none
      const pts = ctx.history!.points
      const last = [...pts].reverse().find((p) => p.memory_total && p.memory_used !== undefined)
      const total = last?.memory_total
      return {
        kind: 'ts', unit: 'GB', max: total ? round(total / GB, 2) : undefined,
        maxLabel: total ? `还能用 ${round((total - (last!.memory_used || 0)) / GB, 1)} GB` : undefined,
        labels: pts.map((p) => pointLabel(p.t)),
        series: [{ key: 'mem', name: '已用', color: 'var(--s1)', data: pts.map((p) => (p.memory_used === undefined ? null : round(p.memory_used / GB, 2))) }],
      }
    },
  },
  disk: {
    name: '磁盘', group: '这台机器', icon: 'disk', fixed: '现在', needs: ['host'],
    gen: (ctx) => {
      const h = ctx.host
      if (!h || !h.available || !h.disk_total || h.disk_used === undefined) return { kind: 'none', text: '读不到磁盘' }
      const used = round(h.disk_used / GB)
      const total = round(h.disk_total / GB)
      const free = round(total - used)
      return { kind: 'value', unit: 'GB', value: used, max: total, label: '已用', sub: `剩 ${free} GB`, tone: free < 3 ? 'danger' : free < 10 ? 'warning' : undefined }
    },
  },
  alloc: {
    name: '已分配内存', group: '这台机器', icon: 'memory', fixed: '现在', needs: ['resources'],
    gen: (ctx) => {
      const m = ctx.resources?.memory
      const cap = m ? m.cap_mb || m.host_mb : 0
      if (!m || !cap) return { kind: 'none', text: '读不到内存上限' }
      const used = round(m.allocated_mb / 1024)
      const max = round(cap / 1024)
      return { kind: 'value', unit: 'GB', value: used, max, label: '已分配', sub: `应用和数据库的内存上限加起来，还能分 ${round(Math.max(0, max - used))} GB`, tone: used / max > 0.9 ? 'warning' : undefined }
    },
  },
  appStatus: {
    name: '应用状态', group: '应用', icon: 'grid', fixed: '现在', needs: ['apps'],
    gen: (ctx) => {
      const st = ctx.apps.map((a) => deriveStatus(a, ctx.installing))
      const c = (...k: string[]) => st.filter((s) => k.includes(s)).length
      // 和“我的应用”的状态一致：更新中、安装中算“处理中”，读不到状态的单独算，都不算停了。
      // 这两类平时是 0，有的时候才显示。
      const items = [
        { name: '正常', value: c('running'), color: 'hsl(var(--success))' },
        { name: '处理中', value: c('deploying', 'installing'), color: 'var(--s1)' },
        { name: '停了', value: c('stopped', 'empty'), color: 'var(--s-other)' },
        { name: '要处理', value: c('failed', 'crashed'), color: 'hsl(var(--warning))', href: '#apps' },
        { name: '看不到状态', value: c('unknown'), color: 'hsl(var(--muted-foreground))' },
      ].filter((it) => it.value > 0 || ['正常', '停了', '要处理'].includes(it.name))
      return { kind: 'cat', status: true, unit: '个', items }
    },
  },
  appMem: {
    name: '各应用内存', group: '应用', icon: 'grid', fixed: '现在', needs: ['apps', 'metrics', 'others'],
    gen: (ctx) => appCat(ctx, (m) => (m.memory_usage_mb === undefined ? null : round(m.memory_usage_mb)), 'MB', othersForChart(ctx.others, 'memory')),
  },
  appCpu: {
    name: '各应用 CPU', group: '应用', icon: 'grid', fixed: '现在', needs: ['apps', 'metrics', 'others'],
    gen: (ctx) => appCat(ctx, (m) => (m.cpu_percent === undefined ? null : round(m.cpu_percent, 2)), '%', othersForChart(ctx.others, 'cpu')),
  },
  todo: { name: '需要你处理', group: '提醒与记录', icon: 'bell', fixed: '', needs: ['apps', 'checks'], gen: (ctx) => ({ kind: 'list', type: 'todo', items: todoItems(ctx), empty: '都处理好了' }) },
  events: { name: '最近发生的事', group: '提醒与记录', icon: 'history', fixed: '', needs: ['apps', 'deployments'], gen: (ctx) => ({ kind: 'list', type: 'events', items: eventItems(ctx), empty: '还没有记录' }) },
}

// others：“其他容器”合成的一项（在跑的加起来；没有就是 null、不加这一项），放在最后、用灰色。
function appCat(ctx: DashCtx, pick: (m: AppMetrics) => number | null, unit: string, others: number | null = null): Data {
  const names = ctx.apps.map((a) => a.name)
  const items: CatItem[] = []
  for (const a of ctx.apps) {
    const m = ctx.metrics[a.name]
    if (!m || !m.available) continue
    const v = pick(m)
    if (v === null) continue
    items.push({ name: nameIn(ctx, a.name), value: v, color: appColor(a.name, names) })
  }
  if (others !== null) items.push({ name: OTHERS_NAME, value: others, color: 'var(--s-other)' })
  if (items.length === 0) {
    const running = ctx.apps.filter((a) => a.observed_state === 'running')
    if (running.some((a) => !ctx.metrics[a.name])) return { kind: 'none', text: '正在读取…' }
    return { kind: 'none', text: ctx.apps.length ? '没有在运行的应用' : '还没有应用' }
  }
  return { kind: 'cat', unit, items }
}

// sourceUsable：服务端没有历史时，CPU、内存曲线不能选。
export function sourceUsable(key: string, history: HostHistory | null): boolean {
  const s = SOURCES[key]
  if (!s) return false
  return !s.history || !!(history && history.ranges.length > 0)
}

export const TYPE_NAME: Record<string, string> = {
  area: '面积图', line: '折线图', column: '柱状图', stat: '数字', meter: '进度条', table: '表格',
  segbar: '分段条', hbar: '条形图', donut: '环形图', kpi: '状态数', list: '列表',
}

// typesFor：这类数据适合的图表方式，第一个是推荐的。
export function typesFor(d: Data): string[] {
  if (d.kind === 'ts') return d.series.length > 1 ? ['line', 'area', 'column', 'table'] : ['area', 'line', 'column', 'stat', ...(d.max ? ['meter'] : []), 'table']
  if (d.kind === 'cat') return d.status ? ['kpi', 'segbar', 'hbar', 'donut', 'table'] : ['hbar', 'column', 'segbar', 'donut', 'table']
  if (d.kind === 'value') return ['meter', 'stat']
  if (d.kind === 'list') return ['list']
  return []
}

// typesForSource：数据还没取到时按数据的种类给，图表方式菜单不会空着。
export function typesForSource(key: string, d: Data): string[] {
  const t = typesFor(d)
  if (t.length) return t
  const kind = { cpu: 'ts', mem: 'ts', disk: 'value', alloc: 'value', appStatus: 'status', appMem: 'cat', appCpu: 'cat', todo: 'list', events: 'list' }[key]
  if (kind === 'ts') return ['area', 'line', 'column', 'stat', 'meter', 'table']
  if (kind === 'value') return ['meter', 'stat']
  if (kind === 'status') return ['kpi', 'segbar', 'hbar', 'donut', 'table']
  if (kind === 'cat') return ['hbar', 'column', 'segbar', 'donut', 'table']
  return ['list']
}

// ---- 需要你处理、最近发生的事 ----

const DAY = 24 * 3600 * 1000

// isQuietCheck：不算问题的检查项。家里、公司局域网里的机器没有公网地址是正常情况（评估 X8），不进“需要你处理”、不计数。
export function isQuietCheck(c: { code?: string }): boolean {
  return c.code === 'NET-LAN-01'
}

// machineTodo：体检里要处理的项（首页、铃铛、左下角“这台机器有 N 件事要处理”用同一个判断）。
export function machineTodo(checks: RuntimeChecks | null): RuntimeCheck[] {
  return (checks?.items || []).filter((c) => c.level !== 'ok' && !isQuietCheck(c))
}

// todoItems：需要你处理的事。title 是一句人话（problemLine），action 一律“去看看”；编号和诊断放进 ask。
// 颜色一律琥珀（样稿第 11 版：“要处理”用琥珀，红色只给删除和出错）。
export function todoItems(ctx: Pick<DashCtx, 'apps' | 'installing' | 'checks' | 'tasks' | 'now' | 'nameOf' | 'behind' | 'platformUpdate' | 'updateSeen'>): ListItem[] {
  const out: ListItem[] = []
  for (const a of ctx.apps) {
    const s = deriveStatus(a, ctx.installing)
    const name = nameIn(ctx, a.name)
    const d = a.latest || a.live
    if (s === 'failed') {
      const ask = deployDiagnosisForAI(a) || `AcornFox 上的应用 ${name}（${a.name}）没启动成功：${[d?.diagnosis?.code, d?.failure_text || d?.diagnosis?.message].filter(Boolean).join(' ')}`
      out.push({ key: 'f-' + a.name, tone: 'warning', icon: 'alert', title: problemLine(name, a, s), href: appHref(a.name), action: '去看看', ask })
    } else if (s === 'crashed') {
      out.push({ key: 'c-' + a.name, tone: 'warning', icon: 'alert', title: problemLine(name, a, s), href: appHref(a.name, 'logs'), action: '去看看', ask: `AcornFox 上的应用 ${name}（${a.name}）停了，容器状态 ${a.observed_state}。请帮我查原因（acornfox logs ${a.name}）。` })
    }
    if (a.catalog_disconnected) {
      const from = a.catalog_origin_name || a.catalog_origin || '私有目录'
      out.push({ key: 'd-' + a.name, tone: 'warning', icon: 'unlink', title: `${name} 来自的“${from}”连不上了，应用照常用`, href: appHref(a.name), action: '去看看', ask: `AcornFox 上的应用 ${name}（${a.name}）来自私有目录 ${from}，这个目录已断开：应用照常运行，但不能再从它安装或更新。` })
    }
  }
  for (const b of ctx.behind || []) {
    const name = nameIn(ctx, b.app)
    out.push({ key: 'b-' + b.app, tone: 'warning', icon: 'wrench', title: `${name} 的设置还没跟上`, href: appHref(b.app), action: '去看看', ask: `AcornFox 上的应用 ${name}（${b.app}）的设置落后了：平台自动填的值变了，应用里用的还是以前的。请帮我处理，可以先运行 acornfox apps reapply ${b.app} --plan。` })
  }
  for (const c of machineTodo(ctx.checks)) {
    const head = c.text.split(/[：，。]/)[0]
    out.push({ key: 'k-' + c.text, tone: 'warning', icon: 'wrench', title: head, href: '#prefs/checkup', action: '去看看', ask: `AcornFox 体检有一项${c.level === 'fail' ? '不通过' : '要注意'}：${[c.code, c.text].filter(Boolean).join(' ')}` })
  }
  // AcornFox 自己有新版本：不是故障，同一个版本只提醒一次（用户在“设置 › 这台机器”看过就不再出，lib/update-seen.ts）。
  const pu = ctx.platformUpdate
  if (pu?.enabled && pu.available && pu.latest && ctx.updateSeen !== pu.latest) {
    out.push({ key: 'u-' + pu.latest, tone: 'warning', icon: 'up', title: `AcornFox 有新版本 ${pu.latest}`, href: '#prefs/update', action: '去看看', ask: `AcornFox 有新版本 ${pu.latest}（现在是 ${pu.current}）。请先把发行说明${pu.notes_url ? '（' + pu.notes_url + '）' : ''}和新版本告诉我，问我同意以后再升级：acornfox update 能看到该用的升级命令。` })
  }
  for (const t of ctx.tasks) {
    if (t.status !== 'failed') continue
    const at = Date.parse(t.finished_at || t.updated_at)
    if (Number.isFinite(at) && ctx.now.getTime() - at > DAY) continue
    const name = nameIn(ctx, t.target)
    out.push({ key: 't-' + t.id, tone: 'warning', icon: 'package', title: `${name} 没装成功`, action: '去看看', ask: `AcornFox 安装 ${t.kind === 'recipe' ? '解决方案' : '应用'} ${name}（${t.target}）没成功，任务 ${t.id}：${[t.fault?.code, t.fault?.message].filter(Boolean).join(' ')}`, task: { kind: t.kind === 'recipe' ? 'recipe' : 'app', id: t.target, taskId: t.id } })
  }
  return out
}

const p2t = (iso: string | undefined, now: Date): string => {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const hm = `${p2(d.getHours())}:${p2(d.getMinutes())}`
  if (d.toDateString() === now.toDateString()) return hm
  const y = new Date(now.getTime() - DAY)
  if (d.toDateString() === y.toDateString()) return '昨天 ' + hm
  return `${p2(d.getMonth() + 1)}-${p2(d.getDate())}`
}

// eventItems：最近发生的事，一句人话（“n8n 更新好了”“my-site 更新没成功”）。不写第几版、不写编号（评估 C5）。
// 被新版本顶掉、没上线的那几版不列（对用户没有意义）；应用中心装好一个应用和它的第一次上线是同一件事，只列上线那条。
export function eventItems(ctx: Pick<DashCtx, 'deployments' | 'tasks' | 'now' | 'nameOf'>, max = 20): ListItem[] {
  const rows: { at: number; item: ListItem }[] = []
  for (const d of ctx.deployments) {
    const iso = d.finished_at || d.created_at
    const first = d.seq <= 1
    const failed = d.status === 'failed'
    const pending = PENDING.has(d.status)
    const what = failed ? (first ? '没装成功' : '更新没成功') : pending ? (first ? '正在安装' : '正在更新') : d.status === 'live' || d.status === 'retired' ? (first ? '装好了' : '更新好了') : ''
    if (!what) continue
    rows.push({
      at: Date.parse(iso) || 0,
      item: { key: 'd-' + d.id, icon: failed ? 'alert' : pending ? 'loader' : 'up', tone: failed ? 'danger' : undefined, title: `${nameIn(ctx, d.app)} ${what}`, time: p2t(iso, ctx.now), href: appHref(d.app) },
    })
  }
  for (const t of ctx.tasks) {
    if (t.kind !== 'recipe' && t.status === 'succeeded') continue
    const iso = t.finished_at || t.updated_at || t.created_at
    const name = nameIn(ctx, t.target)
    const title = t.status === 'succeeded' ? `${name} 一起装好了` : t.status === 'failed' ? `${name} 没装成功` : `正在安装 ${name}`
    rows.push({ at: Date.parse(iso) || 0, item: { key: 't-' + t.id, icon: t.status === 'failed' ? 'alert' : 'package', tone: t.status === 'failed' ? 'danger' : undefined, title, time: p2t(iso, ctx.now) } })
  }
  return rows.sort((a, b) => b.at - a.at).slice(0, max).map((r) => r.item)
}

// ---- 布局 ----

export interface WidgetConf { id: string; s: string; t: string; w: number; h: number }

export const WIDTHS: [number, string][] = [[3, '1/4'], [4, '1/3'], [6, '1/2'], [8, '2/3'], [12, '整行']]
export const HEIGHTS: [number, string][] = [[1, '矮'], [2, '中'], [3, '高']]

// 默认布局（样稿第 14 版重排：首页只显示图表和状态）。三层，从上到下越来越细：
//   第一行四张矮卡，一眼看完“应用都好吗、机器还够用吗”：应用状态；CPU、内存、磁盘是同一个样子（数字、进度条、右边一句还剩多少）；
//   第二行是这一页唯一的一条曲线（CPU，宽的那一张）和最近发生的事；
//   第三行每个应用各用了多少内存、CPU。
// 内存的曲线平时是一条平线，默认不摆（要看可以“编辑 › 添加图表”，或把内存那张换成面积图）。
// 手机上一张一行，顺序就是这个顺序。“需要你处理”默认不摆（首页不要这一块；它在铃铛里），
// “应用状态”那张的“要处理 N”能点，去“我的应用”。
export const DEFAULT_LAYOUT: WidgetConf[] = [
  { id: 'w5', s: 'appStatus', t: 'kpi', w: 3, h: 1 }, { id: 'w8', s: 'cpu', t: 'meter', w: 3, h: 1 },
  { id: 'w2', s: 'mem', t: 'meter', w: 3, h: 1 }, { id: 'w6', s: 'disk', t: 'meter', w: 3, h: 1 },
  { id: 'w1', s: 'cpu', t: 'area', w: 8, h: 2 }, { id: 'w7', s: 'events', t: 'list', w: 4, h: 2 },
  { id: 'w3', s: 'appMem', t: 'hbar', w: 6, h: 2 }, { id: 'w4', s: 'appCpu', t: 'hbar', w: 6, h: 2 },
]

// v4：第 14 版重排了默认布局，旧的 v1～v3 不再读。
export const LAYOUT_KEY = 'afx-dash-v4'

// 布局只存在这个浏览器里（样稿的做法）；读不到或格式不对就用默认布局。
export function loadLayout(storage: Pick<Storage, 'getItem'> | null = safeStorage()): WidgetConf[] {
  try {
    const v = JSON.parse(storage?.getItem(LAYOUT_KEY) || 'null')
    if (Array.isArray(v) && v.length && v.every((x) => x && typeof x.id === 'string' && SOURCES[x.s] && WIDTHS.some(([w]) => w === x.w) && HEIGHTS.some(([h]) => h === x.h))) return v
  } catch {
    /* 坏数据：用默认 */
  }
  return DEFAULT_LAYOUT
}

export function saveLayout(l: WidgetConf[], storage: Pick<Storage, 'setItem'> | null = safeStorage()) {
  try {
    storage?.setItem(LAYOUT_KEY, JSON.stringify(l))
  } catch {
    /* 隐私模式等存不了：只在这次打开时有效 */
  }
}

function safeStorage(): Storage | null {
  try {
    return window.localStorage
  } catch {
    return null
  }
}

export function moveWidget(l: WidgetConf[], id: string, dir: number): WidgetConf[] {
  const a = [...l]
  const i = a.findIndex((x) => x.id === id)
  if (i < 0) return l
  const j = Math.max(0, Math.min(a.length - 1, i + dir))
  const [it] = a.splice(i, 1)
  a.splice(j, 0, it)
  return a
}

export function dropWidget(l: WidgetConf[], from: string, to: string): WidgetConf[] {
  if (from === to) return l
  const a = [...l]
  const i = a.findIndex((x) => x.id === from)
  if (i < 0) return l
  const [it] = a.splice(i, 1)
  const j = a.findIndex((x) => x.id === to)
  a.splice(j < 0 ? a.length : j, 0, it)
  return a
}

// 新加的图表默认大小：数字、进度条、状态数是矮的 1/4，其余是中等的一半宽。
export function defaultSize(t: string): { w: number; h: number } {
  return ['stat', 'meter', 'kpi'].includes(t) ? { w: 3, h: 1 } : { w: 6, h: 2 }
}

// fmtValue：图表上的数字写法（照样稿 fv）。
export function fmtValue(v: number | null | undefined, unit: string): string {
  if (v === null || v === undefined || !Number.isFinite(v)) return '—'
  const a = Math.abs(v)
  const n = a >= 1000 ? Math.round(v).toLocaleString('en-US') : a >= 100 ? String(Math.round(v)) : a >= 10 ? String(Math.round(v * 10) / 10) : String(Math.round(v * 100) / 100)
  return unit === '%' ? n + '%' : unit ? n + ' ' + unit : n
}

// hasRange：时间范围标签只给服务端有的历史。
export function rangeTabs(history: HostHistory | null): RangeKey[] {
  return ((history?.ranges || []) as string[]).filter((r): r is RangeKey => r === '1h' || r === '24h')
}
