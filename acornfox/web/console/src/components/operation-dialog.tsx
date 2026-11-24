// 一件要走完的事的弹窗：确认（说清楚会发生什么）→ 做的时候一列步骤 → 做成了或没做成。
// 更新到新版本、退回到升级前、让应用用上新设置、备份、恢复都用它（样稿规范 4.5：一件要走完的事用弹窗，红色按钮只在这里出现）。
//   确认：标题写要做的事，一句人话说后果，右下角“取消”（描边）和这个窗唯一的主按钮；没做成的留在按钮上面写一行红字。
//   做的时候：步骤点加一句人话，下载那一步带按字节的进度条（components/step-bar.tsx）；可以关掉窗口，任务接着做。
//   做完：成功写一句结果；失败写一句人话和“复制给 AI 工作台”（故障编号、任务号只进复制的文字）。
// 任务有两种（lib/run.ts）：安装任务（重新生效、升级）和备份任务（备份、恢复），这里不管是哪一种，只画。
// 用法：父组件持有一个 OpSpec | null，传给 <OperationDialog spec={…} onClose={…} />；key 变了就是新的一件事（从头开始）。
import { useState, type ReactNode } from 'react'
import { CircleCheck } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useRun, type RunView, type TaskLike } from '@/lib/run'
import { StepBar } from '@/components/step-bar'
import { StepDot, ErrorLine } from '@/components/bits'
import { AskAI } from '@/components/fold'
import { Button } from '@/components/ui/button'
import { Dialog, DialogHead } from '@/components/ui/dialog'

export type OpResult = TaskLike | null | void

export interface OpConfirm {
  body: ReactNode
  button: string
  // 红色按钮：会丢数据、做不回来的事
  destructive?: boolean
  // 不让点（例如磁盘不够）；原因写在 body 里
  disabled?: boolean
  // 做这件事。返回任务就接着看进度；返回空就关掉窗口（调用方自己说结果）；抛错就把原因写在按钮上面。
  onConfirm: () => Promise<OpResult>
  // 第二条路（幽灵小按钮），例如“先改，稍后再让应用跟上”
  later?: { label: string; onConfirm: () => Promise<OpResult> }
}

export interface OpSpec {
  key: string
  title: string
  confirm?: OpConfirm
  // 任务已经在跑（没有确认这一步）
  task?: TaskLike | null
  runTitle?: string
  doneTitle?: string
  failTitle?: string
  doneBody?: (r: RunView) => ReactNode
  failBody?: (r: RunView) => ReactNode
  // 做完（成功或失败）时调一次：页面用它刷新
  onFinished?: (r: RunView) => void
  // 失败时复制给 AI 工作台的文字
  askAI?: (r: RunView) => string
}

const TITLE_ID = 'op-title'

export function OperationDialog({ spec, onClose }: { spec: OpSpec | null; onClose: () => void }) {
  return (
    <Dialog open={!!spec} onClose={onClose} labelledBy={TITLE_ID} className="sm:max-w-lg">
      {spec && <OpBody key={spec.key} spec={spec} onClose={onClose} />}
    </Dialog>
  )
}

function OpBody({ spec, onClose }: { spec: OpSpec; onClose: () => void }) {
  const [task, setTask] = useState<TaskLike | null>(spec.task || null)
  const [busy, setBusy] = useState<'' | 'main' | 'later'>('')
  const [err, setErr] = useState<Error | null>(null)
  const { run, lost } = useRun(task, spec.onFinished)

  if (!task) {
    const c = spec.confirm
    if (!c) return <><DialogHead id={TITLE_ID} title={spec.title} /><div className="mt-4 flex justify-end"><Button variant="outline" onClick={onClose}>关闭</Button></div></>
    const go = (which: 'main' | 'later', fn: () => Promise<OpResult>) => async () => {
      setBusy(which)
      setErr(null)
      try {
        const t = await fn()
        if (t && typeof t === 'object') setTask(t)
        else onClose()
      } catch (e) {
        setErr(e as Error)
      } finally {
        setBusy('')
      }
    }
    return (
      <>
        <DialogHead id={TITLE_ID} title={spec.title} />
        <div className="flex flex-col gap-3 text-sm" data-testid="op-confirm">{c.body}</div>
        <ErrorLine error={err} className="mt-3" />
        <div className="mt-5 flex flex-wrap items-center justify-end gap-2">
          {c.later && <Button variant="ghost" size="sm" disabled={!!busy} loading={busy === 'later'} onClick={go('later', c.later.onConfirm)}>{c.later.label}</Button>}
          <Button variant="outline" onClick={onClose} disabled={!!busy}>取消</Button>
          <Button variant={c.destructive ? 'destructive' : 'default'} disabled={c.disabled || (!!busy && busy !== 'main')} loading={busy === 'main'} onClick={go('main', c.onConfirm)}>{c.button}</Button>
        </div>
      </>
    )
  }

  if (lost) {
    return (
      <>
        <DialogHead id={TITLE_ID} title={spec.title} />
        <p className="text-sm" role="alert" data-testid="op-lost">找不到这次的进度了（这台机器上的 AcornFox 重启过）。去看看应用现在是什么样子。</p>
        <div className="mt-5 flex justify-end"><Button variant="outline" onClick={() => { spec.onFinished?.({ id: task.id, status: 'failed', steps: [], backup: !!task.op }); onClose() }}>关闭</Button></div>
      </>
    )
  }
  const r = run || { id: task.id, status: 'running' as const, steps: [], backup: !!task.op }
  const heading = r.status === 'succeeded' ? spec.doneTitle || spec.title : r.status === 'failed' ? spec.failTitle || spec.title : spec.runTitle || spec.title
  return (
    <>
      <DialogHead id={TITLE_ID} title={heading} />
      <ol className="flex flex-col gap-2" data-testid="op-steps">
        {r.steps.map((s) => (
          <li key={s.key} className="flex items-start gap-2.5 text-sm" data-status={s.status}>
            <span className="mt-0.5"><StepDot status={s.status} /></span>
            <div className="min-w-0">
              <span className={cn(s.status === 'pending' && 'text-muted-foreground', s.status === 'failed' && 'text-danger')}>{s.text}</span>
              {s.detail && <div className="break-words text-2xs text-muted-foreground">{s.detail}</div>}
              {s.status === 'running' && s.progress && <StepBar p={s.progress} />}
            </div>
          </li>
        ))}
      </ol>
      {r.status === 'running' && <p className="mt-3 text-2xs text-muted-foreground">可以关掉这个窗口，会接着做。</p>}
      {r.status === 'succeeded' && (
        <div className="mt-4 flex flex-col gap-1.5 text-sm" data-testid="op-done">
          <div className="flex items-center gap-1.5 text-success"><CircleCheck className="size-icon" />做好了</div>
          {spec.doneBody?.(r)}
        </div>
      )}
      {r.status === 'failed' && (
        <div className="mt-4 rounded-md border border-danger/40 bg-danger/5 px-3 py-2.5 text-xs" role="alert" data-testid="op-failed">
          <div className="font-medium text-danger">{r.fault?.message || '这次没做成'}</div>
          {r.fault?.hint && <div className="mt-1 text-muted-foreground">建议：{r.fault.hint}</div>}
          {spec.failBody?.(r)}
        </div>
      )}
      <div className="mt-5 flex flex-wrap items-center justify-end gap-2">
        {r.status === 'failed' && spec.askAI && <AskAI text={() => spec.askAI!(r)} />}
        <Button variant={r.status === 'running' ? 'outline' : 'default'} onClick={onClose}>{r.status === 'running' ? '先关掉' : r.status === 'succeeded' ? '完成' : '关闭'}</Button>
      </div>
    </>
  )
}
