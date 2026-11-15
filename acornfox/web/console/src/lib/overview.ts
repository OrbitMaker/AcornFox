// 应用概览页图表卡片用的计算（运行条、圆环、版本时间线、访问）。都是纯函数，页面只管画。
// 数据都来自现有接口：GET /v1/apps/{app}、/v1/metrics/apps/{app}、/v1/apps/{app}/deployments。
import { isLoopbackURL, shortAddress } from './appstatus'
import type { AppView, Deployment } from './types'

const DAY = 86400000

export interface UptimeCell { date: string; up: boolean }
export interface Uptime {
  // 一天一格，从早到晚，最后一格是今天。up：这一天里应用在运行（按这次启动的时间推）。
  cells: UptimeCell[]
  // 这次启动以来有几格是亮的
  upDays: number
  // 窗口里每一天都在运行
  full: boolean
}

const p2 = (n: number) => String(n).padStart(2, '0')
const md = (t: number) => { const d = new Date(t); return p2(d.getMonth() + 1) + '-' + p2(d.getDate()) }
const dayStart = (t: number) => { const d = new Date(t); d.setHours(0, 0, 0, 0); return d.getTime() }

// uptimeStrip：近 days 天的运行条。
// 平台现在只知道“这次是什么时候启动的”（容器的启动时间），不记每天有没有中断过：
// 从启动那天到今天算在运行；启动以前没有记录，画成空格。应用没在运行时全是空格。
export function uptimeStrip(startedAt: string | undefined | null, running: boolean, now = Date.now(), days = 30): Uptime {
  const today = dayStart(now)
  const started = running && startedAt ? Date.parse(startedAt) : NaN
  const cells: UptimeCell[] = []
  for (let i = days - 1; i >= 0; i--) {
    const begin = today - i * DAY
    // 这一天结束时应用已经启动了，就算这一天在运行
    cells.push({ date: md(begin), up: Number.isFinite(started) && started < begin + DAY && started <= now })
  }
  const upDays = cells.filter((c) => c.up).length
  return { cells, upDays, full: upDays === days }
}

// ringPct：占上限的百分之几，夹在 0～100；没有上限或没有数时是 null（圆环不画弧）。
export function ringPct(used: number | undefined | null, limit: number | undefined | null): number | null {
  if (used === undefined || used === null || !Number.isFinite(used)) return null
  if (!limit || !Number.isFinite(limit) || limit <= 0) return null
  return Math.max(0, Math.min(100, (used / limit) * 100))
}

// cpuOfLimit：CPU 用了上限的百分之几。接口里的 cpu_percent 是“100% = 用满一个核”，上限 cpu_milli 是千分之一核。
export function cpuOfLimit(cpuPercent: number | undefined | null, cpuMilli: number | undefined | null): number | null {
  if (cpuPercent === undefined || cpuPercent === null) return null
  return ringPct(cpuPercent / 100, (cpuMilli || 0) / 1000)
}

export interface VersionNode { seq: number; live: boolean; failed: boolean; when: string }

// versionTimeline：时间线上的几个版本，从旧到新，最多 max 个，正在服务的那一版一定在里面。
// when：正在服务的写“正在服务”，没上线的写“没上线”，其余写上线那天（月-日）。
export function versionTimeline(list: Deployment[] | undefined | null, liveSeqNow: number, max = 3): VersionNode[] {
  const bySeq = new Map<number, Deployment>()
  for (const d of list || []) if (d && d.seq > 0) bySeq.set(d.seq, d)
  let seqs = Array.from(bySeq.keys()).sort((a, b) => a - b)
  if (liveSeqNow > 0 && !bySeq.has(liveSeqNow)) seqs = [...seqs, liveSeqNow].sort((a, b) => a - b)
  let picked = seqs.slice(-max)
  if (liveSeqNow > 0 && !picked.includes(liveSeqNow)) picked = [liveSeqNow, ...seqs.filter((s) => s > liveSeqNow).slice(-(max - 1))]
  return picked.map((seq) => {
    const d = bySeq.get(seq)
    const live = seq === liveSeqNow
    const failed = !live && d?.status === 'failed'
    const t = d ? Date.parse(d.finished_at || d.created_at) : NaN
    return { seq, live, failed, when: live ? '正在服务' : failed ? '没上线' : Number.isFinite(t) ? md(t) : '' }
  })
}

export type AccessLevel = 'local' | 'lan' | 'public'
export const ACCESS_LEVELS: { key: AccessLevel; text: string; who: string }[] = [
  { key: 'local', text: '只本机', who: '只有这台机器自己能打开' },
  { key: 'lan', text: '局域网', who: '同一个局域网里的电脑、手机都能打开' },
  { key: 'public', text: '公网', who: '能连到这台机器的设备都能打开；机器有公网地址、端口放行了的话，外面也能访问' },
]

export interface Access {
  // 显示的地址（不带 http://）和完整地址
  address: string
  url: string
  // 第二个地址：有域名时另给局域网地址
  lanURL: string
  scheme: string
  domain: string
  level: AccessLevel | ''
  // 用户在自己电脑、手机上能不能直接打开（127.0.0.1 这类不行）
  openable: boolean
}

// accessInfo：访问卡片要的东西。应用没有地址（只在内部网络）时返回 null。
export function accessInfo(app: AppView): Access | null {
  const url = app.url || app.lan_url || ''
  if (!url) return null
  let scheme = 'http'
  try { scheme = new URL(url).protocol.replace(':', '') } catch { /* 地址不是完整网址时按原样显示 */ }
  const domain = (app.domains || []).map((d) => d.name).filter(Boolean)[0] || ''
  const level = (['local', 'lan', 'public'] as const).find((k) => k === app.exposure) || ''
  return {
    address: shortAddress(url), url,
    lanURL: app.lan_url && app.lan_url !== url ? app.lan_url : '',
    scheme, domain, level, openable: !isLoopbackURL(url),
  }
}
