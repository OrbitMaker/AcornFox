// 体检（样稿第 11 版起在“设置 › 这台机器”里的一行，不再是单独一页；旧地址 #runtime 跳到 #prefs/checkup）的结论怎么算：
// 把 GET /v1/runtime/checks 的各项合成几行说人话的，每行“名字 + 一句现状”，颜色和“这台机器正常 / 有几件事要处理”都在这里算，
// 页面只负责显示（pages/runtime/checkup-view.tsx）。
//
// 合成规则（按检查项的 key，样稿第 10 版的假字段，要服务端配合）：
//   disk、data_dir → “磁盘”；sources → “下载应用”；docker、service、edge → “后台程序”；public → “在不在公网上”；
//   没有 key 的项（旧服务端）各自一行，用服务端原话。顺序固定：磁盘、下载应用、后台程序、在不在公网上。
// 计数：只数要处理的行（warn、fail）。家里的机器不在公网上是正常情况（评估 X8）：那一行是灰色的说明，不算问题、不计数。
import type { RuntimeCheck, RuntimeChecks, ShowSyncState } from '@/lib/types'

export type RowTone = 'ok' | 'warn' | 'fail' | 'info'
export type RowKind = 'disk' | 'download' | 'backend' | 'public' | 'show' | 'other'

// name：这一行的名字（“磁盘”）；state：一句现状（“还剩 318 GB”）。没有 key 的项 name 是服务端原话、state 是空的。
export interface CheckupRow { id: string; kind: RowKind; name: string; state: string; tone: RowTone; items: RuntimeCheck[] }

const GROUP: Record<string, RowKind> = { disk: 'disk', data_dir: 'disk', sources: 'download', docker: 'backend', service: 'backend', edge: 'backend', public: 'public' }
const ORDER: RowKind[] = ['disk', 'download', 'backend', 'public']
const NAME: Record<RowKind, string> = { disk: '磁盘', download: '下载应用', backend: '后台程序', public: '在不在公网上', show: '应用中心的介绍', other: '' }

function worst(items: RuntimeCheck[]): RowTone {
  if (items.some((i) => i.level === 'fail')) return 'fail'
  if (items.some((i) => i.level !== 'ok')) return 'warn'
  return 'ok'
}

// diskFree：“还剩多少”。优先用主机指标算（GB，一位小数去掉 .0），没有时从服务端那句话里取“还剩 318 GB”。
export function diskFree(c: RuntimeChecks, host?: { disk_total?: number; disk_used?: number } | null): string {
  if (host?.disk_total && host.disk_used !== undefined) {
    const gb = (host.disk_total - host.disk_used) / 1024 ** 3
    return (gb >= 10 ? Math.round(gb) : +gb.toFixed(1)) + ' GB'
  }
  const t = c.items.find((i) => i.key === 'disk')?.text || ''
  const m = t.match(/还剩\s*([\d.]+\s*[KMGT]?B)/)
  return m ? m[1] : ''
}

function stateOf(kind: RowKind, tone: RowTone, items: RuntimeCheck[], c: RuntimeChecks, free: string, home: boolean): string {
  switch (kind) {
    case 'disk': {
      // 磁盘本身没事、是数据目录那一项有事：说保存数据有问题
      const disk = worst(items.filter((i) => i.key === 'disk'))
      if (tone !== 'ok' && disk === 'ok') return '保存数据有问题'
      const left = free ? `还剩 ${free}` : ''
      if (tone === 'ok') return left || '够用'
      return (tone === 'fail' ? '满了' : '快满了') + (left ? '，' + left : '')
    }
    case 'download': {
      const line = c.sources ? (c.sources.china ? '（走国内线路）' : '（走海外线路）') : ''
      return tone === 'ok' ? '能下载' + line : '下载有问题'
    }
    case 'backend': return tone === 'ok' ? '都在运行' : tone === 'fail' ? '没在运行' : '要注意'
    case 'public':
      if (home) return '不在。在家里或公司的网络里'
      return tone === 'ok' ? '在。外面能打开这台机器上的应用' : '外面打不开这台机器上的应用'
    default: return ''
  }
}

// checkupRows：体检的几行。home：这台机器是不是家里（局域网）的，取自 onPublic(status) 取反；
// 状态里没有 on_public（旧服务端）时，看“在不在公网上”那一项带的是不是 NET-LAN-01（没有公网地址）。
//
// show：应用中心的展示内容（一句话、封面、介绍，经营系统签名发布）这次没能用上（sync.show.state 是 failed，W3-11）：多一行琥珀的
// “应用中心的介绍：这次没更新上，先用上次的”，原话和编号放进“怎么办”和复制给 AI 工作台的文字。装应用不受影响，所以只是提醒。
export function checkupRows(c: RuntimeChecks, opts: { home?: boolean; free?: string; show?: ShowSyncState | null } = {}): CheckupRow[] {
  const groups = new Map<RowKind, RuntimeCheck[]>()
  const out: CheckupRow[] = []
  const others: CheckupRow[] = []
  c.items.forEach((it, i) => {
    const kind = it.key ? GROUP[it.key] : undefined
    if (!kind) {
      others.push({ id: 'other-' + i, kind: 'other', name: it.text, state: '', tone: worst([it]), items: [it] })
      return
    }
    groups.set(kind, [...(groups.get(kind) || []), it])
  })
  for (const kind of ORDER) {
    const items = groups.get(kind)
    if (!items) continue
    const home = kind === 'public' && (opts.home ?? items.some((i) => i.code === 'NET-LAN-01'))
    const tone: RowTone = home ? 'info' : worst(items)
    out.push({ id: kind, kind, name: NAME[kind], state: stateOf(kind, tone, items, c, opts.free || diskFree(c), home), tone, items })
  }
  if (opts.show && opts.show.state === 'failed') {
    const it: RuntimeCheck = { ok: false, level: 'warn', text: opts.show.message || '应用中心的展示内容这次没能用上', code: opts.show.code }
    out.push({ id: 'show', kind: 'show', name: NAME.show, state: '这次没更新上，先用上次的', tone: 'warn', items: [it] })
  }
  return [...out, ...others]
}

// showCheck：展示内容这次没能用上，当作一条要注意的体检项（铃铛、左下角的计数、首页和体检里同一个说法）。
export function showCheck(show: ShowSyncState | null | undefined): RuntimeCheck | null {
  if (!show || show.state !== 'failed') return null
  return { ok: false, level: 'warn', text: '应用中心的介绍这次没更新上，先用上次的', code: show.code }
}

export const needsAction = (r: CheckupRow) => r.tone === 'warn' || r.tone === 'fail'

// conclusion：收起时的那一句结论（左下角“这台机器正常 / 有 N 件事要处理”是同一个说法）。
export function conclusion(rows: CheckupRow[]): { text: string; count: number } {
  const n = rows.filter(needsAction).length
  return { text: n ? `有 ${n} 件事要处理` : '这台机器正常', count: n }
}

const LEVEL: Record<string, string> = { ok: '正常', warn: '要注意', fail: '有问题' }

// checkupForAI：“复制给 AI 工作台帮我查”复制的文字：每一项的原话、故障编号和系统信息都放进去（页面上不单独显示编号，评估 X4）。
export function checkupForAI(c: RuntimeChecks, extra: { version?: string; home?: boolean; show?: ShowSyncState | null } = {}): string {
  const lines = [
    '用 acornfox skill 帮我检查这台机器（可以在这台机器上运行 sudo acornfox doctor --json 看详情）。控制台体检的结果：',
    ...c.items.map((i) => `- [${LEVEL[i.level] || i.level}] ${i.code ? i.code + ' ' : ''}${i.text}`),
  ]
  if (extra.show?.state === 'failed') lines.push(`- [要注意] ${extra.show.code ? extra.show.code + ' ' : ''}应用中心的展示内容这次没能用上：${extra.show.message || ''}（运行 acornfox catalog show 应用 看现在用的展示内容）`)
  if (extra.home) lines.push('（这台机器在家里或公司的网络里，不在公网上，这是正常情况。）')
  const sys = [
    '系统 ' + c.os,
    c.docker_version && 'Docker ' + c.docker_version,
    extra.version && 'AcornFox ' + extra.version,
    c.data_dir && '数据目录 ' + c.data_dir,
    c.sources && '下载走' + (c.sources.china ? '国内' : '海外') + '线路',
    '检查时间 ' + c.checked_at,
  ].filter(Boolean)
  lines.push('系统信息：' + sys.join('；'))
  return lines.join('\n')
}
