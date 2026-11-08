// 首页看板的数据整理：可选的数据（SOURCES）、每种数据能用的图表方式（typesFor）、布局的读写。
// 写法照控制台样稿，但数据只用服务端真有的接口，没有来源的样稿图表不放进来（见 README“首页看板”）：
//   这台电脑：CPU、内存（GET /v1/metrics/host/history，近 1 小时、24 小时；只存在内存里，没有 7 天）、
//             磁盘（GET /v1/metrics/host）、已分配内存（GET /v1/resources）
//   应用：应用状态（GET /v1/apps）、各应用 CPU / 内存（GET /v1/metrics/apps/{app}）
//   提醒与记录：需要你处理（应用状态、运行环境自检、失败的安装）、最近发生的事（部署记录、安装任务）
// 都是纯函数，页面只负责取数和显示；单元测试见 dashboard.test.ts。
import { deriveStatus, PENDING } from './appstatus'
import type { AppMetrics, AppView, Deployment, HostHistory, HostView, Resources, RuntimeChecks, TaskView } from './types'
import { appHref } from './router'

export type RangeKey = '1h' | '24h'
export const RANGE_NAME: Record<string, string> = { '1h': '近 1 小时', '24h': '近 24 小时' }
export const RANGE_TAB: Record<string, string> = { '1h': '1 小时', '24h': '24 小时' }

export interface Label { s: string; l: string } // s：坐标轴上的短写法；l：提示里的完整写法
export interface Series { key: string; name: string; color: string; data: (number | null)[] }
export interface CatItem { name: string; value: number; color: string }
export interface ListItem { key: string; tone?: 'danger' | 'warning'; icon: string; title: string; sub?: string; time?: string; href?: string; action?: string; task?: { kind: 'app' | 'recipe'; id: string; taskId: string } }

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
  resources: Resources | null
  checks: RuntimeChecks | null
  deployments: Deployment[]
  tasks: TaskView[]
}

export interface SourceDef {
  name: string
  group: '这台电脑' | '应用' | '提醒与记录'
  icon: 'cpu' | 'memory' | 'disk' | 'grid' | 'bell' | 'history'
  fixed?: string // 不随时间范围变的数据：标题旁显示它，例如“现在”
  history?: boolean // 要整机历史（服务端没有历史时不能选）
  gen: (ctx: DashCtx) => Data
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
  if (!ctx.history || ctx.history.ranges.length === 0) return { kind: 'none', text: '这台服务器没有记录历史' }
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
    name: 'CPU 使用率', group: '这台电脑', icon: 'cpu', history: true,
    gen: (ctx) => noHistory(ctx) || {
      kind: 'ts', unit: '%', max: 100,
      labels: ctx.history!.points.map((p) => pointLabel(p.t)),
      series: [{ key: 'cpu', name: 'CPU', color: 'var(--s1)', data: ctx.history!.points.map((p) => (p.cpu_percent === undefined ? null : round(p.cpu_percent))) }],
    },
  },
  mem: {
    name: '内存', group: '这台电脑', icon: 'memory', history: true,
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
    name: '磁盘', group: '这台电脑', icon: 'disk', fixed: '现在',
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
    name: '已分配内存', group: '这台电脑', icon: 'memory', fixed: '现在',
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
    name: '应用状态', group: '应用', icon: 'grid', fixed: '现在',
    gen: (ctx) => {
      const st = ctx.apps.map((a) => deriveStatus(a, ctx.installing))
      const c = (...k: string[]) => st.filter((s) => k.includes(s)).length
      // 和“我的应用”的状态一致：部署中、安装中算“处理中”，读不到状态的单独算，都不算已停止。
      // 这两类平时是 0，有的时候才显示。
      const items = [
        { name: '运行中', value: c('running'), color: 'hsl(var(--success))' },
        { name: '处理中', value: c('deploying', 'installing'), color: 'var(--s1)' },
        { name: '已停止', value: c('stopped', 'empty'), color: 'var(--s-other)' },
        { name: '有问题', value: c('failed', 'crashed'), color: 'hsl(var(--danger))' },
        { name: '读不到状态', value: c('unknown'), color: 'hsl(var(--warning))' },
      ].filter((it) => it.value > 0 || ['运行中', '已停止', '有问题'].includes(it.name))
      return { kind: 'cat', status: true, unit: '个', items }
    },
  },
  appMem: {
    name: '各应用内存', group: '应用', icon: 'grid', fixed: '现在',
    gen: (ctx) => appCat(ctx, (m) => (m.memory_usage_mb === undefined ? null : round(m.memory_usage_mb)), 'MB'),
  },
  appCpu: {
    name: '各应用 CPU', group: '应用', icon: 'grid', fixed: '现在',
    gen: (ctx) => appCat(ctx, (m) => (m.cpu_percent === undefined ? null : round(m.cpu_percent, 2)), '%'),
  },
  todo: { name: '需要你处理', group: '提醒与记录', icon: 'bell', fixed: '', gen: (ctx) => ({ kind: 'list', type: 'todo', items: todoItems(ctx), empty: '都处理好了' }) },
  events: { name: '最近发生的事', group: '提醒与记录', icon: 'history', fixed: '', gen: (ctx) => ({ kind: 'list', type: 'events', items: eventItems(ctx), empty: '还没有记录' }) },
}

function appCat(ctx: DashCtx, pick: (m: AppMetrics) => number | null, unit: string): Data {
  const names = ctx.apps.map((a) => a.name)
  const items: CatItem[] = []
  for (const a of ctx.apps) {
    const m = ctx.metrics[a.name]
    if (!m || !m.available) continue
    const v = pick(m)
    if (v === null) continue
    items.push({ name: a.name, value: v, color: appColor(a.name, names) })
  }
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

export function todoItems(ctx: DashCtx): ListItem[] {
  const out: ListItem[] = []
  for (const a of ctx.apps) {
    const s = deriveStatus(a, ctx.installing)
    const d = a.latest || a.live
    if (s === 'failed') out.push({ key: 'f-' + a.name, tone: 'danger', icon: 'alert', title: `${a.name} 部署失败`, sub: [d?.diagnosis?.code, d?.failure_text || d?.diagnosis?.message].filter(Boolean).join(' '), href: appHref(a.name), action: '查看' })
    else if (s === 'crashed') out.push({ key: 'c-' + a.name, tone: 'danger', icon: 'alert', title: `${a.name} 没在运行`, sub: '容器停了，看日志找原因', href: appHref(a.name, 'logs'), action: '看日志' })
    if (a.catalog_disconnected) out.push({ key: 'd-' + a.name, tone: 'warning', icon: 'unlink', title: `${a.name} 的私有目录已断开`, sub: `${a.catalog_origin_name || a.catalog_origin || ''}：照常运行，不能再从它安装`.replace(/^：/, ''), href: appHref(a.name), action: '查看' })
  }
  for (const c of ctx.checks?.items || []) {
    if (c.level === 'ok') continue
    out.push({ key: 'k-' + c.text, tone: c.level === 'fail' ? 'danger' : 'warning', icon: 'wrench', title: c.text.split(/[：，。]/)[0] + (c.level === 'fail' ? ' 有问题' : ' 要注意'), sub: [c.code, c.text.slice(c.text.indexOf('：') + 1)].filter(Boolean).join(' '), href: '#runtime', action: '查看' })
  }
  for (const t of ctx.tasks) {
    if (t.status !== 'failed') continue
    const at = Date.parse(t.finished_at || t.updated_at)
    if (Number.isFinite(at) && ctx.now.getTime() - at > DAY) continue
    out.push({ key: 't-' + t.id, tone: 'danger', icon: 'package', title: `安装 ${t.target} 失败`, sub: [t.fault?.code, t.fault?.message].filter(Boolean).join(' '), action: '查看', task: { kind: t.kind === 'recipe' ? 'recipe' : 'app', id: t.target, taskId: t.id } })
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

export function eventItems(ctx: DashCtx, max = 20): ListItem[] {
  const rows: { at: number; item: ListItem }[] = []
  for (const d of ctx.deployments) {
    const iso = d.finished_at || d.created_at
    const failed = d.status === 'failed'
    const pending = PENDING.has(d.status)
    const what = failed ? '部署失败' : pending ? '部署中' : d.status === 'live' || d.status === 'retired' ? '上线' : d.status === 'superseded' ? '没上线（有更新的版本）' : d.status_text || d.status
    rows.push({
      at: Date.parse(iso) || 0,
      item: {
        key: 'd-' + d.id, icon: failed ? 'alert' : pending ? 'loader' : 'up', tone: failed ? 'danger' : undefined,
        title: `${d.app} 第 ${d.seq} 版${what}`,
        sub: failed ? d.diagnosis?.code : undefined, time: p2t(iso, ctx.now), href: appHref(d.app, 'versions'),
      },
    })
  }
  for (const t of ctx.tasks) {
    const iso = t.finished_at || t.updated_at || t.created_at
    const r = t.status === 'succeeded' ? '安装完成' : t.status === 'failed' ? '安装失败' : '正在安装'
    rows.push({ at: Date.parse(iso) || 0, item: { key: 't-' + t.id, icon: t.status === 'failed' ? 'alert' : 'package', tone: t.status === 'failed' ? 'danger' : undefined, title: `${t.kind === 'recipe' ? '配方 ' : ''}${t.target} ${r}`, sub: t.fault?.code, time: p2t(iso, ctx.now) } })
  }
  return rows.sort((a, b) => b.at - a.at).slice(0, max).map((r) => r.item)
}

// ---- 布局 ----

export interface WidgetConf { id: string; s: string; t: string; w: number; h: number }

export const WIDTHS: [number, string][] = [[3, '1/4'], [4, '1/3'], [6, '1/2'], [8, '2/3'], [12, '整行']]
export const HEIGHTS: [number, string][] = [[1, '矮'], [2, '中'], [3, '高']]

export const DEFAULT_LAYOUT: WidgetConf[] = [
  { id: 'w1', s: 'appStatus', t: 'kpi', w: 3, h: 1 }, { id: 'w2', s: 'cpu', t: 'stat', w: 3, h: 1 }, { id: 'w3', s: 'mem', t: 'meter', w: 3, h: 1 }, { id: 'w4', s: 'disk', t: 'meter', w: 3, h: 1 },
  { id: 'w5', s: 'cpu', t: 'area', w: 8, h: 2 }, { id: 'w6', s: 'todo', t: 'list', w: 4, h: 2 },
  { id: 'w7', s: 'appMem', t: 'hbar', w: 8, h: 2 }, { id: 'w8', s: 'events', t: 'list', w: 4, h: 2 },
  { id: 'w9', s: 'mem', t: 'area', w: 8, h: 2 }, { id: 'w10', s: 'appCpu', t: 'hbar', w: 4, h: 2 },
]

export const LAYOUT_KEY = 'afx-dash-v1'

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
