// 应用升级和退回到升级前（W9-1～W9-3）。页面上叫“更新”（小白页面不说“升级”“回滚”，见设计规范 5.2）。
// 后端另一路按约定写，页面先接好，服务端没有这些字段时“有数据才显示”，不猜：
//   GET  /v1/apps/{app}            catalog_update（应用中心里的新版本号）和 upgrade：
//                                  {available, can_upgrade, blocked, blocked_code, notes[], snapshot:{id,version,created_at,size_mb}|null, can_rollback}
//   POST /v1/apps/{app}/upgrade    {"plan": true} → {plan:{from,to,restart,snapshot_mb,disk_ok,vars_added[],vars_removed[],uses_changed[],notes[],summary}}
//                                  {"confirm_restart": true} → 202 {task}（安装任务，进度读 /v1/catalog/install/{task}）
//   POST /v1/apps/{app}/rollback   {"plan": true} → {plan}；{"confirm": true} → 202 {task}：退回到升级前（用升级前自动备份的数据换回去）
//   GET  /v1/status                apps_outdated：有新版本的应用（数量，或应用代号的列表）
// 更新前服务端先把现在的数据备份一份（升级前快照），没更新成功会自动退回；更新成功后留着，页面给“退回到更新前”，
// 旁边写明会丢掉更新后新增的数据。
import { api, appPath } from './api'
import { bytesText, timeText } from './format'
import type { TaskLike } from './run'
import type { AppView, ServerStatus } from './types'

export interface UpgradePlan {
  from: string
  to: string
  restart: boolean
  snapshot_mb?: number
  disk_ok?: boolean
  vars_added?: unknown[]
  vars_removed?: unknown[]
  uses_changed?: unknown[]
  notes?: string[]
  summary?: string
}

export interface UpgradeInfo {
  // 应用中心里的新版本号（没有新版本是空串）
  next: string
  // 页面上有没有“更新”按钮：服务端说能升才有
  canUpgrade: boolean
  // 现在不能更新的原因（一句话）；blockedCode 只放进复制给 AI 工作台的文字
  blocked: string
  blockedCode: string
  notes: string[]
  // 更新前备份的数据还在，而且能退回到更新前
  snapshot: { id: string; version?: string; created_at?: string; size_mb?: number } | null
  canRollback: boolean
}

// upgradeInfo：这个应用的更新情况；什么都没有（旧服务端）返回 null。
export function upgradeInfo(a: AppView): UpgradeInfo | null {
  const u = a.upgrade
  const next = a.catalog_update || ''
  if (!u && !next) return null
  const snapshot = u?.snapshot && u.snapshot.id ? u.snapshot : null
  return {
    next,
    canUpgrade: !!u?.can_upgrade && !!(next || u?.available),
    blocked: u?.blocked || '',
    blockedCode: u?.blocked_code || '',
    notes: u?.notes || [],
    snapshot,
    canRollback: !!u?.can_rollback && !!snapshot,
  }
}

// outdatedCount：有新版本的应用有几个（服务端没说就是 0，不猜）。
export function outdatedCount(s: ServerStatus | null | undefined): number {
  const o = s?.apps_outdated
  return Array.isArray(o) ? o.length : typeof o === 'number' && o > 0 ? o : 0
}

// itemText：计划里“多了哪几项”这类列表，每一项可能是文字，也可能是带名字的对象。
function itemText(x: unknown): string {
  if (typeof x === 'string') return x
  if (x && typeof x === 'object') {
    const o = x as Record<string, unknown>
    const t = o.label || o.name || o.use || o.kind
    if (typeof t === 'string') return t
  }
  return ''
}
export const itemsText = (xs: unknown[] | null | undefined): string[] => (xs || []).map(itemText).filter(Boolean)

// upgradeFacts：确认窗里的几句话。label 是应用的名字。
export function upgradeFacts(p: UpgradePlan, label: string): string[] {
  const out = [`${label} 从 ${p.from || '现在的版本'} 更新到 ${p.to || '新版本'}。`]
  out.push(p.restart ? '更新时它会重启一下，中间有一小段时间用不了。' : '更新时不用重启。')
  out.push('更新前先把现在的数据备份一份' + (p.snapshot_mb ? '（约 ' + bytesText(p.snapshot_mb * 1024 * 1024) + '）' : '') + '；没更新成功会自动换回原样，更新好了不满意也能退回。')
  const added = itemsText(p.vars_added)
  const gone = itemsText(p.vars_removed)
  if (added.length) out.push(`新版本多了 ${added.length} 项设置（${added.join('、')}），已经按默认值填好。`)
  if (gone.length) out.push(`有 ${gone.length} 项设置新版本不用了（${gone.join('、')}）。`)
  const uses = itemsText(p.uses_changed)
  if (uses.length) out.push('用到的服务有变化：' + uses.join('、') + '。')
  return out
}

// 退回到更新前：窗里写的是哪一份备份、会丢什么。
export function rollbackFacts(s: NonNullable<UpgradeInfo['snapshot']>, label: string): string[] {
  const when = s.created_at ? timeText(s.created_at) : ''
  return [
    `${label} 会退回到更新前的样子${s.version ? '（' + s.version + '）' : ''}，数据也换回${when ? ' ' + when : '更新前'}备份的。`,
    '更新以后新增的数据会丢掉。',
  ]
}

export async function planUpgrade(app: string): Promise<UpgradePlan> {
  return (await api.post<{ plan: UpgradePlan }>(appPath(app, '/upgrade'), { plan: true })).plan
}

export async function startUpgrade(app: string): Promise<TaskLike | null> {
  return (await api.post<{ task?: TaskLike }>(appPath(app, '/upgrade'), { confirm_restart: true })).task || null
}

export async function planRollbackToSnapshot(app: string): Promise<{ summary?: string; blocked?: string }> {
  const r = await api.post<{ plan?: { summary?: string; blocked?: string } }>(appPath(app, '/rollback'), { plan: true })
  return r.plan || {}
}

export async function startRollbackToSnapshot(app: string): Promise<TaskLike | null> {
  return (await api.post<{ task?: TaskLike }>(appPath(app, '/rollback'), { confirm: true })).task || null
}

// upgradeForAI：更新没成功时复制给 AI 工作台的文字。
export function upgradeForAI(app: string, label: string, what: string, fault?: { code?: string; message?: string }, task?: string): string {
  return `AcornFox 上的应用 ${label}（${app}）${what}没成功：${fault?.code ? fault.code + ' ' : ''}${fault?.message || ''}${task ? '（任务 ' + task + '）' : ''}\n请帮我查原因，可以先运行 acornfox status ${app} 和 acornfox backup list ${app}。`
}
