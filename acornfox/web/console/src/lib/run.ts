// 后台任务的统一看法：重新生效、升级、退回到升级前是“安装任务”（GET /v1/catalog/install/{task}），备份和恢复是
// “备份任务”（GET /v1/backups/tasks/{id}）。两种任务的步骤、结果、失败原因长得不一样，页面上都要画成“一列步骤加一个结果”，
// 所以这里先整理成同一个样子（RunView），再由 components/operation-dialog.tsx 画出来。
// 任务只存在服务端的内存和任务记录里：服务重启过就找不到了（404），页面说“找不到这次的进度”，不猜成功还是失败。
import { useEffect, useRef, useState } from 'react'
import { api, ApiError } from './api'
import { plainStep } from './catalog'
import type { StepProgress } from './types'

// 服务端两种任务的并集（只写用到的字段）。备份任务有 op（backup | restore），安装任务有 kind。
export interface TaskLike {
  id: string
  op?: string
  kind?: string
  status?: string
  steps?: unknown[]
  fault?: { code?: string; message?: string; hint?: string } | null
  result?: { backup_id?: string; bytes?: number; rolled_back?: boolean; version?: string; pre_restore_id?: string; pruned?: string[]; warnings?: string[] } | null
  target?: string
}

export interface RunStep { key: string; text: string; status: string; detail?: string; progress?: StepProgress }
export interface RunView {
  id: string
  status: 'running' | 'succeeded' | 'failed'
  steps: RunStep[]
  fault?: { code?: string; message: string; hint?: string }
  result?: TaskLike['result']
  // 备份任务（op 有值）还是安装任务
  backup: boolean
}

// 重新生效任务的步骤，页面上说人话（服务端的名字是 check、files、start、health、init、save、revert）。
const REAPPLY_WORDS: Record<string, string> = {
  check: '检查', files: '写入新设置', start: '重启', health: '等它能用', init: '做收尾的几步', save: '记下新设置', revert: '退回原样',
}

interface InstallStepLike { app?: string; name?: string; label?: string; status?: string; message?: string; progress?: StepProgress }
interface BackupStepLike { key?: string; text?: string; status?: string; detail?: string }

// runView：把一份任务整理成页面要画的样子。
export function runView(t: TaskLike): RunView {
  const backup = !!t.op
  const reapply = t.kind === 'reapply'
  const raw = (t.steps || []) as (InstallStepLike & BackupStepLike)[]
  let steps: RunStep[]
  if (backup) {
    steps = raw.map((s, i) => ({ key: s.key || String(i), text: s.text || '', status: s.status || 'pending', detail: s.detail }))
  } else {
    steps = raw.map((s, i) => ({
      key: (s.app || '') + '/' + (s.name || i),
      text: plainStep(reapply && s.name && REAPPLY_WORDS[s.name] ? REAPPLY_WORDS[s.name] : s.label || ''),
      status: s.status || 'pending',
      detail: s.message ? plainStep(s.message) : undefined,
      progress: s.progress,
    }))
    // “退回原样”只有出了问题才会走到：没轮到它时不列
    if (reapply) steps = steps.filter((s) => !(s.key.endsWith('/revert') && (s.status === 'pending' || s.status === 'skipped')))
  }
  const status = t.status === 'succeeded' || t.status === 'failed' ? t.status : 'running'
  return {
    id: t.id, status, steps, backup, result: t.result,
    fault: t.fault && t.fault.message ? { code: t.fault.code, message: t.fault.message, hint: t.fault.hint } : undefined,
  }
}

// taskPath：这个任务的进度从哪个接口读。
export function taskPath(t: TaskLike): string {
  return (t.op ? '/v1/backups/tasks/' : '/v1/catalog/install/') + encodeURIComponent(t.id)
}

// useRun：每秒读一次任务进度，做完（成功或失败）就停。task 为 null 时什么都不做。
// lost：服务端说没有这个任务（服务重启过）。onFinished 在任务第一次读到“做完了”时调一次。
export function useRun(task: TaskLike | null, onFinished?: (r: RunView) => void): { run: RunView | null; lost: boolean } {
  const [run, setRun] = useState<RunView | null>(task ? runView(task) : null)
  const [lost, setLost] = useState(false)
  const fin = useRef(onFinished)
  fin.current = onFinished
  const id = task?.id
  useEffect(() => {
    if (!task) { setRun(null); setLost(false); return }
    let alive = true
    let timer = 0
    let told = false
    const finish = (r: RunView) => { if (!told && r.status !== 'running') { told = true; fin.current?.(r) } }
    const first = runView(task)
    setRun(first)
    setLost(false)
    if (first.status !== 'running') { finish(first); return }
    const tick = async () => {
      try {
        const t = await api.get<TaskLike>(taskPath(task))
        if (!alive) return
        // 备份任务读出来没有 op 时补上，免得后面按安装任务的样子整理
        const r = runView(task.op && !t.op ? { ...t, op: task.op } : t)
        setRun(r)
        if (r.status === 'running') timer = window.setTimeout(tick, 1000)
        else finish(r)
      } catch (e) {
        if (!alive) return
        const err = e as ApiError
        if (err.status === 401) return
        if (err.status === 404) { setLost(true); return }
        timer = window.setTimeout(tick, 2000)
      }
    }
    timer = window.setTimeout(tick, 600)
    return () => { alive = false; window.clearTimeout(timer) }
    // 只在换了一个任务时重来
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id])
  return { run, lost }
}
