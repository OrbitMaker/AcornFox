// 第三个页签，按两种应用分开（评估 G1～G4、R6）：
//   从应用中心装的应用：叫“更新”。只写“现在是 n8n 2.47.5，11-20 装好”；服务端在 AppView.catalog_update 里说有新版本时多一句“有新版本 2.48.0”
//     （没说就什么都不写，不猜“已是最新”）；AppView.upgrade 说能更新（can_upgrade）时多一个“更新到 2.48.0”，点了先读计划、确认、再做，
//     不能更新时写原因（pages/apps/upgrade.tsx，W9）；更新成功后留着更新前的数据备份时，下面多一张“退回到更新前”（旁边写明会丢掉更新后新增的数据）。
//     “重装一遍程序（数据不动）”收在右上角“更多”菜单里（就是 POST /v1/apps/{app}/redeploy），点了先确认。
//   开发者自己部署的项目：叫“版本”。版本记录（第几版、状态、来源、时间、用时），能退回的有“退回这一版”（上一版写“退回上一版”），
//     有日志的有“日志”；右上“重装一遍”。手机宽度下一版一张卡片，不出横向滚动的表格。
//     状态只用四种说法：正在用、以前的版本、没换上、更新中。确认窗里不摆命令。
// 重装、退回是后台任务——点了以后这一页原位变（“正在重装…”→“重装好了”；版本记录里多一行“更新中”→“正在用”），
//   不再弹“正在重装一遍”的提示；人去了别的页，做完时进铃铛的“刚做完的”（lib/jobs.ts）。没点成（请求出错）留在确认窗里那个按钮上面。
//   读版本记录时有占位形状。
// 接口：GET /v1/apps/{app}/deployments?limit=20；POST /v1/apps/{app}/versions/{seq}/rollback；POST /v1/apps/{app}/redeploy。
import { useState } from 'react'
import { ArrowUpCircle, CheckCircle2, MoreHorizontal, RefreshCw, ShieldCheck, TriangleAlert } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api, appPath } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { PENDING } from '@/lib/appstatus'
import { displayName, fromStore } from '@/lib/display'
import { durationText } from '@/lib/format'
import { installedAt, plainWords } from '@/lib/overview'
import { appHref, go } from '@/lib/router'
import { upgradeInfo } from '@/lib/app-upgrade'
import { timeText } from '@/lib/format'
import type { AppView, Deployment } from '@/lib/types'
import { useConsole } from '@/state'
import { ErrorLine } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Spinner } from '@/components/ui/controls'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { Dropdown, MenuItem } from '@/components/ui/menu'
import { Loading } from '@/components/ui/skeleton'
import { SkVersions } from '@/components/skeletons'
import { useUpgrade } from './upgrade'
import type { TabProps } from './app-page'

const useDeployments = (app: string) =>
  usePoll((s) => api.get<{ deployments: Deployment[] }>(appPath(app, '/deployments?limit=20'), s).then((b) => b.deployments || []), 5000, [app])

export function VersionsTab(props: TabProps) {
  return fromStore(props.app) ? <UpdatePanel {...props} /> : <VersionsPanel {...props} />
}

// useJob：这一页上点过的后台任务（重装、退回）现在到哪了。'' 没点过；running 在做；done 做成了；failed 没换上。
// begin() 在发请求前调（记下那一刻最新一次部署的编号，新的一次出现以后才算）；cancel() 请求没发成时调。
function useJob(a: AppView) {
  const [from, setFrom] = useState<string | null>(null)
  const d = a.latest
  const pending = !!d && PENDING.has(d.status)
  const state: '' | 'running' | 'done' | 'failed' = from === null ? (pending ? 'running' : '') : !d || d.id === from || pending ? 'running' : d.status === 'live' ? 'done' : 'failed'
  return { state, begin: () => setFrom(a.latest?.id || ''), cancel: () => setFrom(null) }
}

// useRedeploy：“重装一遍”（数据不动），带确认窗。没点成时出错的话留在确认窗里（不弹提示）。
function useRedeploy(a: AppView, label: string, after: () => Promise<unknown>) {
  const { trackJob } = useConsole()
  const job = useJob(a)
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<Error | null>(null)
  const run = async () => {
    setBusy(true)
    setErr(null)
    job.begin()
    trackJob(a.name, 'redeploy')
    try {
      await api.post(appPath(a.name, '/redeploy'), {})
      setOpen(false)
      await after()
    } catch (e) {
      job.cancel()
      setErr(new Error('没能重装：' + (e as Error).message))
    } finally {
      setBusy(false)
    }
  }
  const dialog = (
    <Dialog open={open} onClose={() => setOpen(false)} labelledBy="re-title">
      <DialogHead id="re-title" title={'重装一遍 ' + label + ' 的程序？'} />
      <p className="text-sm">数据不动。新装的启动正常才换过去，不成功就继续用现在的。</p>
      <ErrorLine error={err} className="mt-3" />
      <div className="mt-5 flex justify-end gap-2">
        <Button variant="outline" onClick={() => setOpen(false)}>取消</Button>
        <Button onClick={run} loading={busy}>重装一遍</Button>
      </div>
    </Dialog>
  )
  return { ask: () => { setErr(null); setOpen(true) }, busy, state: job.state, dialog }
}

// ---- 应用中心装的：更新 ----

function UpdatePanel({ app: a, reload }: TabProps) {
  const { catalog } = useConsole()
  const label = displayName(catalog, a)
  const list = useDeployments(a.name)
  const re = useRedeploy(a, label, () => Promise.all([list.refresh(), reload()]))
  const when = installedAt(list.data, a.live)
  const info = upgradeInfo(a)
  const next = info?.next || ''
  const up = useUpgrade(a, label, info, () => Promise.all([list.refresh(), reload()]))
  // 读计划、读不到：出错的话留在“更新到”按钮旁边（不弹提示）
  const [upBusy, setUpBusy] = useState('')
  const [upErr, setUpErr] = useState<Error | null>(null)
  const ask = (kind: 'up' | 'back') => async () => {
    setUpBusy(kind)
    setUpErr(null)
    try { await (kind === 'up' ? up.askUpgrade() : up.askRollback()) } catch (e) { setUpErr(new Error('没能开始：' + (e as Error).message)) } finally { setUpBusy('') }
  }
  const working = re.state === 'running'
  return (
    <div className="flex flex-col gap-3">
      <Card className="px-4 py-3.5" data-testid="app-update">
        <div className="flex items-start gap-3">
          <div className="min-w-0 flex-1">
            <div className="text-sm">现在是 <span className="font-medium">{label} {a.catalog_version || ''}</span>{when && <span className="text-muted-foreground">，{when} 装好</span>}</div>
            {re.state === 'running' ? (
              <div className="mt-2.5 inline-flex items-center gap-1.5 text-sm text-muted-foreground" role="status" data-testid="update-running"><Spinner />正在重装，数据不动</div>
            ) : re.state === 'done' ? (
              <div className="mt-2.5 inline-flex items-center gap-1.5 text-sm text-success" role="status" data-testid="update-done"><CheckCircle2 className="size-4" />重装好了</div>
            ) : re.state === 'failed' ? (
              <div className="mt-2.5 text-sm text-danger" role="alert" data-testid="update-failed">这次没装上，原来的照常用</div>
            ) : next || info?.canUpgrade ? (
              <div className="mt-2.5 flex flex-wrap items-center gap-2.5" data-testid="update-available">
                <span className="inline-flex items-center gap-1.5 text-sm text-primary"><ArrowUpCircle className="size-4" />有新版本 {next}</span>
                {info?.canUpgrade && <Button size="sm" loading={upBusy === 'up'} disabled={!!upBusy} onClick={ask('up')} data-testid="update-go">更新{next ? '到 ' + next : ''}</Button>}
                {!info?.canUpgrade && info?.blocked && <span className="text-xs text-muted-foreground" data-testid="update-blocked">现在不能更新：{info.blocked}</span>}
              </div>
            ) : null}
            <ErrorLine error={upErr} className="mt-2" />
          </div>
          <Dropdown width="w-52" trigger={(t, open) => (
            <Button variant="ghost" size="icon-sm" disabled={!a.current_deployment || working} loading={re.busy} onClick={t} aria-label="更多" aria-haspopup="menu" aria-expanded={open}>
              <MoreHorizontal className="size-4" />
            </Button>
          )}>
            <MenuItem icon={<RefreshCw />} onClick={re.ask}>重装一遍程序（数据不动）</MenuItem>
          </Dropdown>
        </div>
        <ErrorLine error={list.error} className="mt-2" />
        {re.dialog}
      </Card>
      {info?.canRollback && info.snapshot && (
        <Card className="flex flex-wrap items-center gap-3 px-4 py-3" data-testid="update-rollback">
          <div className="min-w-0 flex-1 text-sm">
            <div>更新前的数据备份还留着{info.snapshot.created_at ? '（' + timeText(info.snapshot.created_at) + '）' : ''}</div>
            <div className="text-xs text-muted-foreground">退回到更新前，更新以后新增的数据会丢掉。</div>
          </div>
          <Button variant="outline" size="sm" loading={upBusy === 'back'} disabled={!!upBusy || working} onClick={ask('back')}>退回到更新前</Button>
        </Card>
      )}
      {up.dialog}
    </div>
  )
}

// ---- 自己部署的项目：版本记录 ----

// statusWord：版本记录里的状态，只用四种说法。
function statusWord(d: Deployment): { text: string; tone: string } {
  if (d.status === 'live') return { text: '正在用', tone: 'text-success' }
  if (d.status === 'failed') return { text: '没换上', tone: 'text-danger' }
  if (PENDING.has(d.status)) return { text: '更新中', tone: 'text-muted-foreground' }
  return { text: '以前的版本', tone: 'text-muted-foreground' }
}

// rollbackNote：不能一键退回时右边那句（服务端给的话里的“回退”换成“退回”）。
const rollbackNote = (d: Deployment) => (!d.can_rollback && d.rollback_text && d.status !== 'live' ? plainWords(d.rollback_text) : '')

function VersionsPanel({ app: a, reload }: TabProps) {
  const { catalog, trackJob } = useConsole()
  const label = displayName(catalog, a)
  const list = useDeployments(a.name)
  const [target, setTarget] = useState<Deployment | null>(null)
  const [busy, setBusy] = useState(false)
  const [backErr, setBackErr] = useState<Error | null>(null)
  const back = useJob(a)
  const re = useRedeploy(a, label, () => Promise.all([list.refresh(), reload()]))
  const rows = list.data || []
  const live = rows.find((r) => r.status === 'live')
  // 上一版：正在用的那一版之前、能退回的最近一版
  const prevSeq = live ? Math.max(0, ...rows.filter((r) => r.seq < live.seq && r.can_rollback).map((r) => r.seq)) : 0
  const backWord = (d: Deployment) => (d.seq === prevSeq ? '退回上一版' : '退回这一版')

  const rollback = async (d: Deployment) => {
    setBusy(true)
    setBackErr(null)
    back.begin()
    trackJob(a.name, 'rollback', '第 ' + d.seq + ' 版')
    try {
      await api.post(appPath(a.name, '/versions/' + d.seq + '/rollback'), {})
      setTarget(null)
      await Promise.all([list.refresh(), reload()])
    } catch (e) {
      back.cancel()
      setBackErr(new Error('没能退回：' + (e as Error).message))
    } finally {
      setBusy(false)
    }
  }
  // 有一次重装或退回正在做：别的“退回”先不能点，“重装一遍”的图标转着。
  const working = re.state === 'running' || back.state === 'running'
  const actions = (d: Deployment) => (
    <div className="flex items-center justify-end gap-1">
      {d.can_rollback && d.status !== 'live' && <Button variant="outline" size="sm" disabled={busy || working} onClick={() => { setBackErr(null); setTarget(d) }}>{backWord(d)}</Button>}
      {rollbackNote(d) && <span className="text-2xs text-muted-foreground">{rollbackNote(d)}</span>}
      {(d.logs?.run || d.logs?.build || d.status === 'live') && <Button variant="ghost" size="sm" onClick={() => go(appHref(a.name, 'logs', { v: String(d.seq) }))}>日志</Button>}
    </div>
  )

  return (
    <div className="flex flex-col gap-3">
      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-2 px-3.5 py-2.5">
          <span className="text-sm font-medium">版本记录</span>
          {a.current_deployment && <Button variant="outline" size="sm" onClick={re.ask} loading={re.busy || working} turn><RefreshCw className="size-icon" />重装一遍</Button>}
        </div>
        <ErrorLine error={list.error} className="px-3.5 pb-2" />
        <Loading when={!list.data && !list.error} skeleton={<SkVersions />}>
        {/* 电脑上是表格 */}
        <table className="hidden w-full text-sm sm:table">
          <thead>
            <tr className="border-y bg-muted/30 text-left text-2xs text-muted-foreground">
              <th className="h-ctl px-3.5 font-medium">版本</th>
              <th className="px-2 font-medium">状态</th>
              <th className="px-2 font-medium">来源</th>
              <th className="px-2 font-medium">时间</th>
              <th className="px-2 text-right font-medium">用时</th>
              <th className="px-3.5" />
            </tr>
          </thead>
          <tbody>
            {rows.map((d) => {
              const w = statusWord(d)
              return (
                <tr key={d.id} className="border-b last:border-0">
                  <td className="tnum whitespace-nowrap px-3.5 py-2">第 {d.seq} 版</td>
                  <td className="px-2">
                    <span className={cn('whitespace-nowrap text-xs', w.tone)}>{PENDING.has(d.status) && <Spinner className="mr-1 inline size-3" />}{w.text}</span>
                    {d.failure_text && <div className="max-w-[260px] truncate text-2xs text-muted-foreground" title={d.failure_text}>{d.failure_text}</div>}
                  </td>
                  <td className="px-2 text-xs text-muted-foreground">{plainWords(d.source_text || d.source_kind)}</td>
                  <td className="tnum whitespace-nowrap px-2 text-xs text-muted-foreground">{timeText(d.created_at)}</td>
                  <td className="tnum px-2 text-right text-xs text-muted-foreground">{durationText(d.duration_seconds)}</td>
                  <td className="px-3.5">{actions(d)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
        {/* 手机上一版一张卡片（评估 X10、G4） */}
        <div className="flex flex-col border-t sm:hidden" data-testid="versions-cards">
          {rows.map((d) => {
            const w = statusWord(d)
            return (
              <div key={d.id} className="flex flex-col gap-1 border-b px-3.5 py-2.5 last:border-0">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="tnum text-sm font-medium">第 {d.seq} 版</span>
                  <span className={cn('text-xs', w.tone)}>{PENDING.has(d.status) && <Spinner className="mr-1 inline size-3" />}{w.text}</span>
                </div>
                <div className="tnum text-2xs text-muted-foreground">{timeText(d.created_at)} · {plainWords(d.source_text || d.source_kind)}{d.duration_seconds != null ? ' · 用时 ' + durationText(d.duration_seconds) : ''}</div>
                {d.failure_text && <div className="text-2xs text-muted-foreground">{d.failure_text}</div>}
                <div className="flex justify-start">{actions(d)}</div>
              </div>
            )
          })}
        </div>
        {list.data && rows.length === 0 && <div className="border-t px-3.5 py-6 text-center text-xs text-muted-foreground">还没有版本记录</div>}
        </Loading>
      </Card>
      <Dialog open={!!target} onClose={() => setTarget(null)} labelledBy="rb-title">
        {target && (
          <>
            <DialogHead id="rb-title" title={backWord(target)} desc={(live ? '现在用的是第 ' + live.seq + ' 版，' : '') + '换回第 ' + target.seq + ' 版。不用重新下载。'} />
            <ul className="flex flex-col gap-2 text-xs">
              <li className="flex gap-2"><ShieldCheck className="size-icon shrink-0 text-success" />启动正常才换过去，不成功就继续用现在的版本</li>
              <li className="flex gap-2"><TriangleAlert className="size-icon shrink-0 text-warning" />只换程序，数据保持现在的样子</li>
            </ul>
            <ErrorLine error={backErr} className="mt-3" />
            <div className="mt-5 flex justify-end gap-2">
              <Button variant="outline" onClick={() => setTarget(null)}>取消</Button>
              <Button loading={busy} onClick={() => rollback(target)}>{backWord(target)}</Button>
            </div>
          </>
        )}
      </Dialog>
      {re.dialog}
    </div>
  )
}
