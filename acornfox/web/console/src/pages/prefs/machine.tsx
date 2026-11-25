// 设置 › 这台机器（样稿第 11 版：“把那个体检合并到设置里。”）。一行一项，同一时间只展开一项：
//   升级：收起时一句“现在是 0.7.0，已是最新（上次查：今天 10:00）”或“有新版本 0.7.1，现在是 0.7.0”；展开是“复制给 AI 工作台帮我升级”、
//         发行说明链接、默认收起的命令（可复制）和“现在查一次”。平台只能升级、不能退回（升级没成功时脚本自动恢复原样）。
//         没有“一键升级”按钮：升级要在这台机器上运行命令，由用户同意后做。服务端每 6 小时自己查一次（W12-3，platform_update），
//         旧服务端没有这一项时只比版本号和 latest_version。同一个版本的新版本提醒，用户在这里看过（展开升级这一行）就不再进铃铛。
//   体检：收起时一个圆点加一句结论（“这台机器正常”或“有 1 件事要处理”）；展开是原来体检页的那几行
//         （磁盘、下载应用、后台程序、在不在公网上），内容和算法都在 pages/runtime/（checkup.ts、checkup-view.tsx），这里只摆进来。
// 地址：#prefs/machine 两行都收着；#prefs/update、#prefs/checkup 直接展开对应的那一行（左下角“这台机器正常”、首页和铃铛里
// 体检的事、搜索里的“体检”都指到 #prefs/checkup）。以前的 #runtime 和更早的 #prefs/engine、storage、mirror 也跳到 #prefs/checkup。
// 接口：GET /v1/status（版本、platform_update：线上最新的版本、该用的升级命令、发行说明；POST /v1/platform/update/check 现在就查一次）、
//       GET /v1/runtime/checks（体检；升级只取 sources.china：这台机器在国内还是海外，旧服务端没给升级命令时只给该用的那一条）。
import * as React from 'react'
import { api } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { upgradeForAI, upgradeView } from '@/lib/upgrade'
import { markUpdateSeen } from '@/lib/update-seen'
import type { PlatformUpdate, RuntimeChecks } from '@/lib/types'
import { useConsole } from '@/state'
import { ExpandRow } from '@/components/expand-row'
import { AskAI, CliFold } from '@/components/fold'
import { Button } from '@/components/ui/button'
import { CopyLine } from '@/components/ui/copy'
import { useToast } from '@/components/ui/toast'
import { CheckupBody, checkupSummary, useCheckup } from '@/pages/runtime/checkup-view'

export type MachineRow = 'update' | 'checkup'

export function Machine({ want }: { want?: MachineRow }) {
  const { status, refreshStatus } = useConsole()
  const toast = useToast()
  const [open, setOpen] = React.useState<MachineRow | ''>(want || '')
  React.useEffect(() => { if (want) setOpen(want) }, [want])
  const toggle = (k: MachineRow) => setOpen((o) => (o === k ? '' : k))
  const [checking, setChecking] = React.useState(false)

  const checks = usePoll((s) => api.get<RuntimeChecks>('/v1/runtime/checks', s).catch(() => null), 0)
  const china = checks.data?.sources ? checks.data.sources.china : null
  const view = upgradeView(status, china, new Date())
  const { kind, version, latest, commands: cmds } = view

  // 用户展开了升级这一行，就算看过这个版本的提醒（铃铛里不再出这一条）
  React.useEffect(() => {
    if (open === 'update' && kind === 'new' && latest) markUpdateSeen(latest)
  }, [open, kind, latest])

  const checkNow = async () => {
    setChecking(true)
    try {
      const r = await api.post<PlatformUpdate>('/v1/platform/update/check')
      await refreshStatus()
      if (r.available && r.latest) toast('有新版本 ' + r.latest, '展开升级这一行看怎么升')
      else if (r.ok) toast('已是最新', '现在是 ' + r.current)
      else toast('暂时查不到最新版本', '过一会儿再试，不影响现在使用')
    } catch (e) {
      toast('没能查', (e as Error).message || '请稍后再试', 'error')
    } finally {
      setChecking(false)
    }
  }
  const checkButton = view.canCheck && kind !== 'off' ? <Button size="sm" variant="outline" loading={checking} onClick={checkNow}>现在查一次</Button> : null

  const checkup = useCheckup()
  const sum = checkupSummary(checkup)

  return (
    <>
      <ExpandRow className="-mx-4" id="machine-update" name="升级" summary={<span data-testid="upgrade-state">{view.line}</span>} open={open === 'update'} onToggle={() => toggle('update')}>
        <div className="flex flex-col items-start gap-2.5" data-testid="prefs-update-body">
          {!version ? (
            <p className="text-xs text-muted-foreground">正在读取版本…</p>
          ) : kind === 'new' ? (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <AskAI text={upgradeForAI(version, latest, cmds, view.where)} label="复制给 AI 工作台帮我升级" variant="default" />
                {view.notesUrl && <a href={view.notesUrl} target="_blank" rel="noreferrer" className="text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground">看发行说明</a>}
                {checkButton}
              </div>
              <CliFold className="w-full">
                {view.where && <div className="text-2xs text-muted-foreground">{view.where}：</div>}
                {cmds.length === 1 ? <CopyLine text={cmds[0]} /> : (
                  <>
                    <div className="text-2xs text-muted-foreground">这台机器在国内用：</div><CopyLine text={cmds[0]} />
                    <div className="text-2xs text-muted-foreground">在海外用：</div><CopyLine text={cmds[1]} />
                  </>
                )}
                <p className="text-2xs text-muted-foreground">会先打印要做的事，升级会短暂停一下服务；升级没成功会自动恢复原样。</p>
              </CliFold>
            </>
          ) : kind === 'unknown' ? (
            <>
              <p className="text-xs text-muted-foreground">有新版本时会写在这一行。</p>
              <AskAI text={upgradeForAI(version, latest, cmds)} label="复制给 AI 工作台帮我升级" variant="outline" />
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
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              <p className="text-xs text-muted-foreground">
                {kind === 'off' ? '这台机器不去查新版本（服务的设置里关掉了）。'
                  : kind === 'unreachable' ? '暂时查不到最新版本，不影响使用。服务器每 6 小时会自己再试。'
                  : kind === 'pending' ? '服务器每 6 小时查一次，开机后几分钟内会查第一次。'
                  : view.canCheck ? '有新版本时会写在这一行，服务器每 6 小时查一次。'
                  : '有新版本时会写在这一行。'}
              </p>
              {checkButton}
            </div>
          )}
        </div>
      </ExpandRow>
      <ExpandRow className="-mx-4" id="machine-checkup" name="体检" tone={sum.tone} summary={sum.text} summaryTestId="checkup-conclusion" open={open === 'checkup'} onToggle={() => toggle('checkup')}>
        <CheckupBody k={checkup} />
      </ExpandRow>
    </>
  )
}
