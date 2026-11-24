// 让应用用上新的设置：页面上的几处入口共用这一套（lib/reapply.ts 是接口和说法）。
//   ① 设置 › 安装设置里改一项（settings-vars.tsx）：先读计划，有变化才弹确认窗，同意了再做；
//   ② 平台自动填的值变了、应用落在后面（概览最上面的提示条、安装设置里的“和现在不一样”）：一键“让它跟上”；
//   ③ 改了谁能打开、加或删了域名（access.tsx、settings-access.tsx）：对用到应用地址的应用，服务端不带“同意重启”会 409，
//      体里带计划，这里拿它问用户；同意了带上同意再来；也可以“先改，稍后再让应用跟上”（reapply: skip，之后落在后面，概览会提示）。
// 确认窗和进度都是 components/operation-dialog.tsx：确认 → 一列步骤 → 做成了或没做成（失败会自动退回原样，页面照实说）。
import { useEffect, useState, useSyncExternalStore } from 'react'
import { AlertTriangle } from 'lucide-react'
import { useAppNames } from '@/lib/display'
import {
  applyReapply, applyVars, behindSummary, changeName, confirmPlan, planFacts, planReapply, planVars, reapplyForAI, visibleChanges,
  type AffectedApp, type ReapplyAnswer, type ReapplyPlan,
} from '@/lib/reapply'
import type { TaskLike } from '@/lib/run'
import { useConsole } from '@/state'
import { Alert } from '@/components/ui/controls'
import { Button } from '@/components/ui/button'
import { useToast } from '@/components/ui/toast'
import { OperationDialog, type OpSpec } from '@/components/operation-dialog'

// PlanBody：确认窗里的内容：几句话，加改动的几项（名字：原来的 → 新的）。
export function PlanBody({ plan, label }: { plan: ReapplyPlan; label: string }) {
  const name = useAppNames()
  const changes = visibleChanges(plan.vars)
  return (
    <>
      {planFacts(plan, label).map((t, i) => <p key={i}>{t}</p>)}
      {changes.length > 0 && (
        <ul className="flex flex-col gap-1 rounded-md bg-muted/50 p-2.5 text-xs" data-testid="reapply-changes">
          {changes.map((v) => (
            <li key={v.name} className="flex flex-wrap items-baseline gap-x-2">
              <span className="w-24 shrink-0 text-muted-foreground">{changeName(v)}</span>
              <span className="mono min-w-0 break-all">{v.old || '没有'}<span className="px-1.5 text-muted-foreground">→</span>{v.new || '没有'}</span>
            </li>
          ))}
        </ul>
      )}
      {plan.dependents && plan.dependents.length > 0 && <p className="text-xs text-muted-foreground" data-testid="reapply-dependents">{plan.dependents.map(name).join('、')} 用着它给的服务。</p>}
    </>
  )
}

// 地址那类改动的一次尝试：mode 是 try（不带同意，服务端要同意会 409）、confirm（用户同意重启）、skip（先改，不让应用跟着变）。
export type AddressAttempt = (mode: 'try' | 'confirm' | 'skip') => Promise<unknown>

// 确认窗和进度只有一个，放在外壳里（ReapplyHost，App.tsx 挂一次）：改谁能打开以后卡片会从“三档”换成“怎么打开”这类别的样子，
// 发起的那个小组件跟着没了，窗不能跟着没（应用正在重启，用户要看到结果）。同一时间只做一件事。
let current: OpSpec | null = null
const subs = new Set<() => void>()
const setCurrent = (s: OpSpec | null) => { current = s; subs.forEach((f) => f()) }
const subscribe = (f: () => void) => { subs.add(f); return () => { subs.delete(f) } }

export function ReapplyHost() {
  const spec = useSyncExternalStore(subscribe, () => current)
  // 外壳卸掉（退出登录、测试里换了一次渲染）时不把上一件事带到下一次
  useEffect(() => () => setCurrent(null), [])
  return <OperationDialog spec={spec} onClose={() => setCurrent(null)} />
}

interface Flow {
  // 改一项或几项安装设置：先读计划；没有变化时提示一声，有变化弹确认窗。读计划出错时抛出来，调用方写在那一行上。
  changeVars: (set: Record<string, unknown>, reset?: string[]) => Promise<void>
  // 让应用跟上现在的平台设置和服务
  catchUp: () => Promise<void>
  // 改了谁能打开、加或删域名：尝试一次；服务端要用户同意重启时弹确认窗。返回 true 表示已经有了结果（做成了或弹了窗）。
  // 返回 plain：改好了，也没有后续要做的（可以给“撤销”）；followed：弹了确认窗，或者应用正在跟着变（不给撤销，别再来回改）。
  address: (what: string, attempt: AddressAttempt) => Promise<'plain' | 'followed'>
}

// useReapply：这个应用的“让它用上新设置”流程。after 在做完（成败都算）以后调：刷新应用和落后的列表。
export function useReapply(app: string, label: string, after: () => Promise<unknown> | void): Flow {
  const toast = useToast()
  const { refreshAffected, refreshApps } = useConsole()
  const done = async () => { await Promise.all([after(), refreshAffected(), refreshApps()]) }

  const base = (title: string): Pick<OpSpec, 'title' | 'runTitle' | 'doneTitle' | 'failTitle' | 'onFinished' | 'askAI'> => ({
    title,
    runTitle: label + ' 正在用上新设置',
    doneTitle: label + ' 已用上新设置',
    failTitle: label + ' 没能用上新设置',
    onFinished: () => { void done() },
    askAI: (r) => reapplyForAI(app, label, '用上新设置时没成功', (r.fault?.message || '') + (r.fault?.code ? '（' + r.fault.code + '）' : '') + '，任务 ' + r.id),
  })
  const open = (s: OpSpec) => setCurrent({ ...s, key: s.key + ':' + Date.now() })
  const taskOf = (a: ReapplyAnswer | undefined): TaskLike | null => (a?.task as TaskLike | undefined) || null

  const confirmSpec = (title: string, plan: ReapplyPlan, go: (confirm: boolean) => Promise<TaskLike | null>): OpSpec => ({
    ...base(title),
    key: 'reapply',
    confirm: { body: <PlanBody plan={plan} label={label} />, button: plan.restart ? '保存并重启' : '保存', onConfirm: () => go(true) },
  })

  const changeVars: Flow['changeVars'] = async (set, reset = []) => {
    const plan = await planVars(app, set, reset)
    if (!plan.needed) { toast('没有变化', '现在的设置就是这样'); return }
    open(confirmSpec('改 ' + label + ' 的设置', plan, async (confirm) => {
      const a = await applyVars(app, set, reset, confirm)
      if (!a.task) void done()
      return taskOf(a)
    }))
  }

  const catchUp: Flow['catchUp'] = async () => {
    const plan = await planReapply(app)
    if (!plan.needed) { toast('已经是最新的', label + ' 用的设置和现在的一致'); void done(); return }
    const s = confirmSpec('让 ' + label + ' 跟上', plan, async (confirm) => {
      const a = await applyReapply(app, confirm)
      if (!a.task) void done()
      return taskOf(a)
    })
    s.confirm!.button = plan.restart ? '让它跟上并重启' : '让它跟上'
    open(s)
  }

  const address: Flow['address'] = async (what, attempt) => {
    const finish = (r: unknown) => {
      const re = (r as { reapply?: ReapplyAnswer & { error?: { message?: string }; note?: string } } | undefined)?.reapply
      if (re?.error) toast('改好了，但应用没能跟上', re.note || re.error.message || '', 'error')
      return taskOf(re)
    }
    try {
      const r = await attempt('try')
      const t = finish(r)
      if (t) { open({ ...base(what), key: 'address-run', task: t }); return 'followed' }
      await done()
      return 'plain'
    } catch (e) {
      const plan = confirmPlan(e)
      if (!plan) throw e
      open({
        ...base(what),
        key: 'address',
        confirm: {
          body: <PlanBody plan={plan} label={label} />,
          button: '改好并重启',
          onConfirm: async () => { const t = finish(await attempt('confirm')); if (!t) void done(); return t },
          later: { label: '先改，稍后再让应用跟上', onConfirm: async () => { await attempt('skip'); await done(); return null } },
        },
      })
      return 'followed'
    }
  }

  return { changeVars, catchUp, address }
}

// BehindAlert：这个应用落在后面时，概览最上面的提示条（琥珀）：一句说是哪几项，一个“让它跟上”。
export function BehindAlert({ behind, label, onCatchUp }: { behind: AffectedApp; label: string; onCatchUp: () => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<Error | null>(null)
  const run = async () => {
    setBusy(true)
    setErr(null)
    try { await onCatchUp() } catch (e) { setErr(new Error('没能让它跟上：' + (e as Error).message)) } finally { setBusy(false) }
  }
  return (
    <Alert
      tone="warning"
      icon={<AlertTriangle />}
      title={label + ' 的设置还没跟上'}
      action={behind.blocked ? undefined : <Button size="sm" variant="outline" loading={busy} onClick={run}>让它跟上</Button>}
    >
      <span data-testid="behind-alert">
        {behind.blocked ? behind.blocked : `${behindSummary(behind)}变了，应用里用的还是以前的。`}
        {err && <span className="ml-1 text-danger" role="alert">{err.message}</span>}
      </span>
    </Alert>
  )
}
