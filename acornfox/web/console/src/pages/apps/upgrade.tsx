// 更新到新版本、退回到更新前（W9）：应用“更新”页签里的两个动作（lib/app-upgrade.ts 是接口和说法）。
//   更新：点“更新到 x”先读计划（只读）：从哪一版到哪一版、要不要重启、更新前先备份多少数据、新版本多了或少了哪几项设置、磁盘够不够
//     （不够不让点，原因写在窗里）；同意了开始，做的时候一列步骤（下载新版本带进度条），没成功会自动换回原样，页面照实说。
//   退回到更新前：更新成功后留着更新前的数据备份，页面给“退回到更新前”；确认窗写明会丢掉更新后新增的数据（红色按钮，只在确认窗里）。
// 确认窗和进度是 components/operation-dialog.tsx。更新是新装一版程序，进度里的“新版本上线”算在后台任务里：人去了别的页，做完进铃铛的“刚做完的”。
import { useState, type ReactNode } from 'react'
import { itemsText, planRollbackToSnapshot, planUpgrade, rollbackFacts, startRollbackToSnapshot, startUpgrade, upgradeFacts, upgradeForAI, type UpgradeInfo } from '@/lib/app-upgrade'
import type { AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { OperationDialog, type OpSpec } from '@/components/operation-dialog'

interface Upgrade {
  dialog: ReactNode
  // 读计划并弹确认窗；出错时抛出来，调用方写在按钮旁边
  askUpgrade: () => Promise<void>
  askRollback: () => Promise<void>
}

// useUpgrade：这个应用的更新和退回到更新前。after 在做完（成败都算）以后调。
export function useUpgrade(a: AppView, label: string, info: UpgradeInfo | null, after: () => Promise<unknown> | void): Upgrade {
  const { trackJob, refreshApps } = useConsole()
  const [spec, setSpec] = useState<OpSpec | null>(null)
  const open = (s: OpSpec) => setSpec({ ...s, key: s.key + ':' + Date.now() })
  const finished = () => { void Promise.all([after(), refreshApps()]) }

  const askUpgrade = async () => {
    const p = await planUpgrade(a.name)
    const notes = [...(p.notes || []), ...(info?.notes || [])].filter(Boolean)
    open({
      key: 'upgrade', title: `把 ${label} 更新到 ${p.to || info?.next || '新版本'}？`,
      runTitle: '正在更新 ' + label, doneTitle: label + ' 已更新到 ' + (p.to || info?.next || '新版本'), failTitle: label + ' 没能更新',
      onFinished: finished,
      askAI: (r) => upgradeForAI(a.name, label, '更新', r.fault, r.id),
      confirm: {
        disabled: p.disk_ok === false,
        button: p.restart ? '更新并重启' : '更新',
        body: (
          <>
            {upgradeFacts(p, label).map((t, i) => <p key={i}>{t}</p>)}
            {p.disk_ok === false && <p className="text-warning" role="alert" data-testid="upgrade-nospace">磁盘空间不够，先清理一下再更新。</p>}
            {itemsText(p.uses_changed).length === 0 && notes.length > 0 && <ul className="list-disc pl-5 text-xs text-muted-foreground">{notes.map((n, i) => <li key={i}>{n}</li>)}</ul>}
          </>
        ),
        onConfirm: async () => {
          trackJob(a.name, 'update', '')
          const t = await startUpgrade(a.name)
          if (!t) finished()
          return t
        },
      },
    })
  }

  const askRollback = async () => {
    const s = info?.snapshot
    if (!s) return
    const p = await planRollbackToSnapshot(a.name)
    open({
      key: 'rollback', title: `把 ${label} 退回到更新前？`,
      runTitle: '正在退回 ' + label, doneTitle: label + ' 已退回到更新前', failTitle: label + ' 没能退回',
      onFinished: finished,
      askAI: (r) => upgradeForAI(a.name, label, '退回到更新前', r.fault, r.id),
      confirm: {
        destructive: true,
        disabled: !!p.blocked,
        button: '退回并丢掉新数据',
        body: (
          <>
            {rollbackFacts(s, label).map((t, i) => <p key={i}>{t}</p>)}
            {p.blocked && <p className="text-warning" role="alert">{p.blocked}</p>}
          </>
        ),
        onConfirm: async () => {
          trackJob(a.name, 'rollback', '更新前')
          const t = await startRollbackToSnapshot(a.name)
          if (!t) finished()
          return t
        },
      },
    })
  }

  return { askUpgrade, askRollback, dialog: <OperationDialog spec={spec} onClose={() => setSpec(null)} /> }
}
