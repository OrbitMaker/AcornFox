// 设置 → 备份（W10-1、W10-2）：这个应用的数据备份和恢复，一行一项：收起时一句“最近一份 11-24 10:30，共 2 份”或“还没有备份”。
//   展开：“立即备份”和一列备份（时间、哪一种、多大、哪一版），每份有“恢复”和“删除”：
//   - 立即备份：先读计划（只读）。应用在运行就先说一句“会停一小会儿，做完自动开”再开始；磁盘不够不让点，原因写在窗里；
//   - 恢复：确认窗写明会覆盖现在的数据、覆盖前自动备份一份现在的（做错了还能恢复回来）、当时那一版的程序有没有；
//     做的时候一列步骤，恢复失败会自动换回恢复前的样子（页面照实说）；
//   - 删除：红色按钮，只在确认窗里。
//   正在做的备份或恢复（服务端说 busy）在列表上面写一行“正在…”，点“看进度”接着看。
// 更新前自动备份（更新到新版本时服务端先做的）和恢复前自动备份也在列表里，标明是哪一种。
// 服务端不支持备份（旧版本、这台机器读不了数据）时整行不显示（lib/backup.ts 的 backupUnsupported）。
// 接口都在 lib/backup.ts；确认窗和进度是 components/operation-dialog.tsx。
import { useState } from 'react'
import { DatabaseBackup } from 'lucide-react'
import { usePoll } from '@/lib/hooks'
import { bytesText, timeText } from '@/lib/format'
import {
  backupForAI, backupLine, backupSummary, deleteBackup, fetchBackups, kindText, planBackup, planRestore, startBackup, startRestore,
  type BackupList, type BackupMeta,
} from '@/lib/backup'
import type { TaskLike } from '@/lib/run'
import { ErrorLine } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { Sk } from '@/components/ui/skeleton'
import { OperationDialog, type OpSpec } from '@/components/operation-dialog'
import { ExpandRow } from '@/components/expand-row'
import { useToast } from '@/components/ui/toast'
import type { TabProps } from './app-page'

// useBackups：读这个应用的备份（15 秒一次，做完备份和恢复后马上再读）。null：不支持备份，不显示这一行。
export function useBackups(app: string) {
  return usePoll((s) => fetchBackups(app, s), 15000, [app])
}

export { backupSummary }

// BackupRow：设置页里的这一行（自带上面的分隔线和 data-section；不支持备份时什么都不画）。rowRef 让设置页能滚到它。
export function BackupRow({ app: a, label, reload, open, onToggle, wide, rowRef }: { app: TabProps['app']; label: string; reload: () => Promise<void>; open: boolean; onToggle: () => void; wide?: boolean; rowRef?: (el: HTMLDivElement | null) => void }) {
  const b = useBackups(a.name)
  if (b.data === null && !b.loading) return null
  if (!b.data) {
    return (
      <div ref={rowRef} data-section="backup" className="border-t">
        <div className="flex items-center gap-3 px-4 py-row" aria-busy="true" data-testid="backup-loading">
          <span className="w-28 shrink-0 text-sm font-medium sm:w-36">备份</span>
          <span className="flex h-[var(--lh-xs)] min-w-0 flex-1 items-center"><Sk className="h-2.5 w-40" /></span>
        </div>
      </div>
    )
  }
  const l = b.data
  return (
    <div ref={rowRef} data-section="backup" className="border-t">
      <ExpandRow id="set-backup" name="备份" summary={backupSummary(l)} summaryTestId="summary-backup" open={open} onToggle={onToggle} wide={wide}>
        <BackupBody app={a.name} label={label} list={l} refresh={async () => { await Promise.all([b.refresh(), reload()]) }} />
      </ExpandRow>
    </div>
  )
}

function BackupBody({ app, label, list, refresh }: { app: string; label: string; list: BackupList; refresh: () => Promise<void> }) {
  const toast = useToast()
  const [spec, setSpec] = useState<OpSpec | null>(null)
  const [busy, setBusy] = useState('')
  // 读计划没成：留在“立即备份”那一块下面
  const [err, setErr] = useState<Error | null>(null)
  const running = list.busy ? list.tasks.find((t) => t.id === list.busy) || ({ id: list.busy, op: 'backup', status: 'running', steps: [] } as TaskLike) : null
  const open = (s: OpSpec) => setSpec({ ...s, key: s.key + ':' + Date.now() })
  const finished = () => { void refresh() }
  const ask = (what: string) => (r: { fault?: { code?: string; message: string }; id: string }) => backupForAI(app, label, what, r.fault, r.id)

  const backupNow = async () => {
    setBusy('backup')
    setErr(null)
    try {
      const plan = await planBackup(app)
      open({
        key: 'backup', title: '备份 ' + label, runTitle: '正在备份 ' + label, doneTitle: label + ' 备份好了', failTitle: label + ' 没能备份',
        onFinished: finished, askAI: ask('备份'),
        doneBody: (r) => <p className="text-xs text-muted-foreground">{r.result?.bytes ? '大小 ' + bytesText(r.result.bytes) + '。' : ''}{r.result?.pruned?.length ? '更早的 ' + r.result.pruned.length + ' 份已按留存的份数清掉。' : ''}</p>,
        confirm: {
          disabled: !plan.enough,
          body: (
            <>
              <p>{plan.will_stop ? `备份时 ${label} 会停一小会儿，做完自动开。` : `${label} 现在是停着的，直接备份，不会把它开起来。`}</p>
              <p className="text-xs text-muted-foreground">要用 {bytesText(plan.need_bytes)} 左右的磁盘{plan.will_prune?.length ? `；手动备份最多留 ${plan.keep} 份，最早的 ${plan.will_prune.length} 份会清掉` : ''}。</p>
              {!!plan.will_prune_for_space?.length && <p className="text-xs text-warning" data-testid="backup-evict">磁盘放不下，会先清掉 {plan.will_prune_for_space.length} 份自动存的旧备份（升级前、恢复前自动存的），清了找不回来。</p>}
              {!plan.enough && <p className="text-warning" role="alert" data-testid="backup-nospace">{plan.say || '磁盘空间不够，先清理一下再备份。'}</p>}
            </>
          ),
          button: '开始备份',
          onConfirm: async () => startBackup(app),
        },
      })
    } catch (e) {
      setErr(new Error('没能开始备份：' + (e as Error).message))
    } finally {
      setBusy('')
    }
  }

  const restore = async (b: BackupMeta) => {
    setBusy('restore-' + b.id)
    setErr(null)
    try {
      const plan = await planRestore(app, b.id)
      const when = timeText(b.created_at)
      const bad = !plan.intact || !plan.enough
      open({
        key: 'restore-' + b.id, title: `恢复到 ${when} 的备份？`, runTitle: '正在恢复 ' + label, doneTitle: label + ' 恢复好了', failTitle: label + ' 没能恢复',
        onFinished: finished, askAI: ask('恢复'),
        doneBody: (r) => <p className="text-xs text-muted-foreground">{r.result?.pre_restore_id ? '恢复前的数据也留了一份，在备份列表里。' : ''}</p>,
        failBody: (r) => (r.result?.rolled_back ? <p className="mt-1 text-muted-foreground">已经换回恢复前的样子。</p> : null),
        confirm: {
          disabled: bad,
          button: '覆盖并恢复',
          body: (
            <>
              <p>{label} 现在的数据会被这份备份覆盖{plan.will_stop ? '，恢复时它会停一会儿' : ''}。</p>
              {plan.current_has_data && <p>覆盖前会先自动备份一份现在的，做错了还能恢复回来。</p>}
              {!!plan.will_prune_for_space?.length && <p className="text-warning" data-testid="restore-evict">备份的地方放不下，会先清掉 {plan.will_prune_for_space.length} 份自动存的旧备份（升级前、恢复前自动存的），清了找不回来。</p>}
              {plan.changes_version && (
                <p data-testid="restore-version">
                  这份备份是 {plan.version_then} 时做的，恢复后 {label} 也会换回这一版（现在是 {plan.version_now}）。
                  {!plan.version_available && '当时那一版的程序这台机器上已经没有了，会继续用现在的版本，数据和它可能对不上。'}
                </p>
              )}
              {!plan.intact && <p className="text-danger" role="alert">这份备份的文件不完整，不能恢复。</p>}
              {plan.intact && !plan.enough && <p className="text-warning" role="alert">磁盘空间不够，先清理一下再恢复。</p>}
            </>
          ),
          onConfirm: async () => startRestore(app, b.id, plan.changes_version && !plan.version_available),
        },
      })
    } catch (e) {
      setErr(new Error('没能读到这份备份的情况：' + (e as Error).message))
    } finally {
      setBusy('')
    }
  }

  const remove = (b: BackupMeta) => open({
    key: 'rm-' + b.id, title: '删除这份备份',
    confirm: {
      destructive: true, button: '删除',
      body: <p>会删掉 {backupLine(b)}。删掉就找不回来了。</p>,
      onConfirm: async () => { await deleteBackup(app, b.id); toast('已删掉这份备份'); await refresh() },
    },
  })

  return (
    <div className="flex flex-col gap-3" data-testid="backups">
      <div className="flex flex-wrap items-center gap-2.5">
        <Button variant="outline" onClick={backupNow} loading={busy === 'backup'} disabled={!!running} data-testid="backup-now"><DatabaseBackup className="size-icon" />立即备份</Button>
        {running && (
          <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground" role="status" data-testid="backup-busy">
            <Spinner className="size-3" />正在备份或恢复
            <Button variant="link" size="sm" className="h-auto p-0 text-xs" onClick={() => open({ key: 'run-' + running.id, title: label, task: running, runTitle: '正在备份或恢复 ' + label, doneTitle: '做好了', failTitle: '没做成', onFinished: finished, askAI: ask('备份或恢复') })}>看进度</Button>
          </span>
        )}
      </div>
      <ErrorLine error={err} />
      {list.backups.length === 0 ? (
        <p className="text-sm text-muted-foreground" data-testid="backup-empty">还没有备份。</p>
      ) : (
        <ul className="flex flex-col" data-testid="backup-list">
          {list.backups.map((b) => (
            <li key={b.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t py-2 first:border-t-0" data-testid="backup-item" data-kind={b.kind}>
              <span className="min-w-0 flex-1 text-xs" title={kindText(b.kind)}>{backupLine(b)}</span>
              <Button variant="outline" size="sm" disabled={!!running || !list.installed || !!busy} loading={busy === 'restore-' + b.id} onClick={() => restore(b)} aria-label={'恢复 ' + timeText(b.created_at)}>恢复</Button>
              <Button variant="ghost" size="sm" className="text-danger hover:text-danger" disabled={!!running || !!busy} onClick={() => remove(b)} aria-label={'删除 ' + timeText(b.created_at)}>删除</Button>
            </li>
          ))}
        </ul>
      )}
      <OperationDialog spec={spec} onClose={() => setSpec(null)} />
    </div>
  )
}
