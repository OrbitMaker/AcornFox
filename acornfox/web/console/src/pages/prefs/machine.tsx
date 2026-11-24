// 设置 › 这台机器（样稿第 11 版：“把那个体检合并到设置里。”）。一行一项，同一时间只展开一项：
//   升级：收起时一句“现在是 0.6.3，已是最新”或“有新版本 0.6.4”；展开是“复制给 AI 工作台帮我升级”和默认收起的命令。
//         平台只能升级、不能退回（升级没成功时脚本自动恢复原样）。
//   体检：收起时一个圆点加一句结论（“这台机器正常”或“有 1 件事要处理”）；展开是原来体检页的那几行
//         （磁盘、下载应用、后台程序、在不在公网上），内容和算法都在 pages/runtime/（checkup.ts、checkup-view.tsx），这里只摆进来。
// 地址：#prefs/machine 两行都收着；#prefs/update、#prefs/checkup 直接展开对应的那一行（左下角“这台机器正常”、首页和铃铛里
// 体检的事、搜索里的“体检”都指到 #prefs/checkup）。以前的 #runtime 和更早的 #prefs/engine、storage、mirror 也跳到 #prefs/checkup。
// 接口：GET /v1/status（版本、线上最新的版本 latest_version，样稿第 10 版的假字段）、
//       GET /v1/runtime/checks（体检；升级只取 sources.china：这台机器在国内还是海外，只给该用的那一条命令）。
import * as React from 'react'
import { api } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { upgradeCommand, upgradeForAI, upgradeState } from '@/lib/upgrade'
import type { RuntimeChecks } from '@/lib/types'
import { useConsole } from '@/state'
import { ExpandRow } from '@/components/expand-row'
import { AskAI, CliFold } from '@/components/fold'
import { CopyLine } from '@/components/ui/copy'
import { CheckupBody, checkupSummary, useCheckup } from '@/pages/runtime/checkup-view'

export type MachineRow = 'update' | 'checkup'

export function Machine({ want }: { want?: MachineRow }) {
  const { status } = useConsole()
  const [open, setOpen] = React.useState<MachineRow | ''>(want || '')
  React.useEffect(() => { if (want) setOpen(want) }, [want])
  const toggle = (k: MachineRow) => setOpen((o) => (o === k ? '' : k))

  const checks = usePoll((s) => api.get<RuntimeChecks>('/v1/runtime/checks', s).catch(() => null), 0)
  const version = status?.version
  const latest = status?.latest_version
  const state = upgradeState(version, latest)
  const china = checks.data?.sources ? checks.data.sources.china : null
  const cmds = upgradeCommand(china)
  const line = !version ? '正在读取版本…' : state === 'new' ? `有新版本 ${latest}，现在是 ${version}` : state === 'latest' ? `现在是 ${version}，已是最新` : `现在是 ${version}`

  const checkup = useCheckup()
  const sum = checkupSummary(checkup)

  return (
    <>
      <ExpandRow className="-mx-4" id="machine-update" name="升级" summary={<span data-testid="upgrade-state">{line}</span>} open={open === 'update'} onToggle={() => toggle('update')}>
        <div className="flex flex-col items-start gap-2.5" data-testid="prefs-update-body">
          {state === 'latest' || !version ? (
            <p className="text-xs text-muted-foreground">{version ? '有新版本时会写在这一行。' : '正在读取版本…'}</p>
          ) : (
            <>
              <AskAI text={upgradeForAI(version, latest, cmds)} label="复制给 AI 工作台帮我升级" variant={state === 'new' ? 'default' : 'outline'} />
              <CliFold className="w-full">
                {cmds.length === 1 ? <CopyLine text={cmds[0]} /> : (
                  <>
                    <div className="text-2xs text-muted-foreground">这台机器在国内用：</div><CopyLine text={cmds[0]} />
                    <div className="text-2xs text-muted-foreground">在海外用：</div><CopyLine text={cmds[1]} />
                  </>
                )}
                <p className="text-2xs text-muted-foreground">在这台机器上运行，会先打印要做的事；升级没成功会自动恢复原样。</p>
              </CliFold>
            </>
          )}
        </div>
      </ExpandRow>
      <ExpandRow className="-mx-4" id="machine-checkup" name="体检" tone={sum.tone} summary={sum.text} summaryTestId="checkup-conclusion" open={open === 'checkup'} onToggle={() => toggle('checkup')}>
        <CheckupBody k={checkup} />
      </ExpandRow>
    </>
  )
}
