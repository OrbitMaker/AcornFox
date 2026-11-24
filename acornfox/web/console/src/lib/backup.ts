// 应用数据的备份和恢复（W10-1、W10-2）。接口（acornfox/internal/apiserver/backups.go）：
//   GET    /v1/apps/{app}/backups                  这个应用的备份（新的在前）、正在做的任务、每种留几份、busy（正在做的任务号）
//   POST   /v1/apps/{app}/backups                  {"plan": true} 只说会怎样（200 {plan}）；不带 plan 开始备份（202 {task}）
//   POST   /v1/apps/{app}/backups/{id}/restore     {"plan": true} 只说会怎样；{"confirm": true, "allow_current_version"} 开始恢复（202 {task}）
//   DELETE /v1/apps/{app}/backups/{id}?confirm=true 删掉这份备份的文件
//   GET    /v1/backups/tasks/{id}                  备份或恢复任务的进度（步骤、结果、失败原因）
// 备份时应用会停一小会儿（做完自动开）；恢复要把现在的数据换成备份里的，换之前先自动备份一份“恢复前”，做错了还能恢复回来。
// 服务端不支持备份（404、BKP-11：老版本，或这台机器读不了数据）时页面不显示备份这一行。
// 页面上不写“数据卷”“卷”，故障编号只进复制给 AI 工作台的文字。
import { api, ApiError, appPath } from './api'
import { bytesText, timeText } from './format'
import type { TaskLike } from './run'

export interface BackupMeta {
  id: string
  app: string
  // manual 手动备份；pre-upgrade 升级前自动备份；pre-restore 恢复前自动备份
  kind: 'manual' | 'pre-upgrade' | 'pre-restore' | string
  note?: string
  created_at: string
  was_running?: boolean
  version?: { catalog_version?: string; seq?: number }
  bytes: number
}

export interface BackupList {
  app: string
  // 应用还在不在（应用删了，备份还留着，但恢复要应用在）
  installed: boolean
  backups: BackupMeta[]
  tasks: TaskLike[]
  keep: { manual: number; pre_upgrade: number; pre_restore: number }
  // 正在做的备份或恢复的任务号（没有就没有这一项）
  busy?: string
}

export interface BackupPlan {
  app: string
  need_bytes: number
  free_bytes: number
  free_known: boolean
  enough: boolean
  // 应用在运行，备份时会停一小会儿
  will_stop: boolean
  keep: number
  will_prune?: string[]
  // 磁盘放不下，会先清掉的自动另存的旧备份
  will_prune_for_space?: string[]
  say: string
}

export interface RestorePlan {
  app: string
  backup: BackupMeta
  intact: boolean
  version_now: string
  version_then: string
  changes_version: boolean
  // 当时那一版的程序这台机器上还有没有
  version_available: boolean
  needs_confirm: boolean
  backup_need_bytes: number
  docker_need_bytes: number
  enough: boolean
  will_stop: boolean
  current_has_data: boolean
  // 磁盘放不下恢复前的备份时，会先清掉的自动另存的旧备份
  will_prune_for_space?: string[]
  say: string
}

export const KIND_TEXT: Record<string, string> = { manual: '手动备份', 'pre-upgrade': '更新前自动备份', 'pre-restore': '恢复前自动备份' }
export const kindText = (k: string) => KIND_TEXT[k] || '备份'

// 旧服务端、这台机器读不了数据（BKP-11）：不显示备份这一行
export function backupUnsupported(e: unknown): boolean {
  return e instanceof ApiError && (e.status === 404 || e.status === 405 || e.code === 'BKP-11')
}

export const backupsPath = (app: string, rest = '') => appPath(app, '/backups' + rest)

export async function fetchBackups(app: string, signal?: AbortSignal): Promise<BackupList | null> {
  try {
    const l = await api.get<BackupList>(backupsPath(app), signal)
    return { ...l, backups: l.backups || [], tasks: l.tasks || [] }
  } catch (e) {
    if (backupUnsupported(e)) return null
    throw e
  }
}

export async function planBackup(app: string): Promise<BackupPlan> {
  return (await api.post<{ plan: BackupPlan }>(backupsPath(app), { plan: true })).plan
}

export async function startBackup(app: string): Promise<TaskLike> {
  return (await api.post<{ task: TaskLike }>(backupsPath(app), { kind: 'manual' })).task
}

export async function planRestore(app: string, id: string): Promise<RestorePlan> {
  return (await api.post<{ plan: RestorePlan }>(backupsPath(app, '/' + encodeURIComponent(id) + '/restore'), { plan: true })).plan
}

export async function startRestore(app: string, id: string, allowCurrentVersion: boolean): Promise<TaskLike> {
  return (await api.post<{ task: TaskLike }>(backupsPath(app, '/' + encodeURIComponent(id) + '/restore'), { confirm: true, allow_current_version: allowCurrentVersion })).task
}

export function deleteBackup(app: string, id: string): Promise<unknown> {
  return api.del(backupsPath(app, '/' + encodeURIComponent(id) + '?confirm=true'))
}

// ---- 说人话 ----

// backupLine：列表里一份备份的一行：“11-24 10:30 · 手动备份 · 12 MB”，来自哪一版的写在后面。
export function backupLine(b: BackupMeta, now = new Date()): string {
  const v = b.version?.catalog_version
  return [timeText(b.created_at, now), kindText(b.kind), bytesText(b.bytes), v ? '版本 ' + v : ''].filter(Boolean).join(' · ')
}

// backupSummary：设置里“备份”那一行收起时的一句。
export function backupSummary(l: BackupList | null | undefined, now = new Date()): string {
  if (!l) return ''
  if (l.busy) return '正在备份或恢复'
  const latest = l.backups[0]
  if (!latest) return '还没有备份'
  return `最近一份 ${timeText(latest.created_at, now)}，共 ${l.backups.length} 份`
}

// backupForAI：做不成时复制给 AI 工作台的文字。
export function backupForAI(app: string, label: string, what: string, fault?: { code?: string; message?: string }, task?: string): string {
  return `AcornFox 上的应用 ${label}（${app}）${what}没做成：${fault?.code ? fault.code + ' ' : ''}${fault?.message || ''}${task ? '（任务 ' + task + '）' : ''}\n请帮我查原因，可以先运行 acornfox backup list ${app} 和 acornfox faults ${fault?.code || 'BKP-01'}。`
}
