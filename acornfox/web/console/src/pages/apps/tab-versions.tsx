// 版本记录：每次部署一行（第几版、来源、状态、时间、用时），能回退的版本有“回退”，有日志的有“日志”。
// 接口：GET /v1/apps/{app}/deployments?limit=20；POST /v1/apps/{app}/versions/{seq}/rollback；POST /v1/apps/{app}/redeploy。
import { useState } from 'react'
import { RefreshCw, ShieldCheck, TriangleAlert } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api, appPath } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import { PENDING } from '@/lib/appstatus'
import { durationText, timeText } from '@/lib/format'
import { appHref, go } from '@/lib/router'
import type { Deployment } from '@/lib/types'
import { ErrorLine } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Spinner } from '@/components/ui/controls'
import { CopyLine } from '@/components/ui/copy'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { useToast } from '@/components/ui/toast'
import type { TabProps } from './app-page'

const TONE: Record<string, string> = { live: 'text-success', failed: 'text-danger', retired: 'text-muted-foreground', superseded: 'text-muted-foreground' }

export function VersionsTab({ app: a, reload }: TabProps) {
  const toast = useToast()
  const list = usePoll((s) => api.get<{ deployments: Deployment[] }>(appPath(a.name, '/deployments?limit=20'), s).then((b) => b.deployments || []), 5000, [a.name])
  const [target, setTarget] = useState<Deployment | null>(null)
  const [busy, setBusy] = useState('')
  const rows = list.data || []
  const live = rows.find((r) => r.status === 'live')

  const rollback = async (d: Deployment) => {
    setBusy('rb')
    try {
      await api.post(appPath(a.name, '/versions/' + d.seq + '/rollback'), {})
      toast('已提交回退', '健康检查通过才切换，失败继续用现在的版本')
      setTarget(null)
      await Promise.all([list.refresh(), reload()])
    } catch (e) {
      toast('没能回退', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }
  const redeploy = async () => {
    setBusy('re')
    try {
      await api.post(appPath(a.name, '/redeploy'), {})
      toast('已提交重新部署', a.name)
      await Promise.all([list.refresh(), reload()])
    } catch (e) {
      toast('没能重新部署', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <Card className="overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-2 px-3.5 py-2.5">
          <span className="text-sm font-medium">版本记录</span>
          <div className="flex items-center gap-2">
            <span className="text-2xs text-muted-foreground">回退只换程序，数据保持现在的样子</span>
            {a.current_deployment && <Button variant="outline" size="sm" disabled={!!busy} onClick={redeploy}>{busy === 're' ? <Spinner /> : <RefreshCw className="size-3.5" />}重新部署</Button>}
          </div>
        </div>
        <ErrorLine error={list.error} className="px-3.5 pb-2" />
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] text-sm">
            <thead>
              <tr className="border-y bg-muted/30 text-left text-2xs text-muted-foreground">
                <th className="h-8 px-3.5 font-medium">版本</th>
                <th className="px-2 font-medium">状态</th>
                <th className="px-2 font-medium">来源</th>
                <th className="px-2 font-medium">时间</th>
                <th className="px-2 text-right font-medium">用时</th>
                <th className="px-3.5" />
              </tr>
            </thead>
            <tbody>
              {rows.map((d) => (
                <tr key={d.id} className="border-b last:border-0">
                  <td className="tnum px-3.5 py-2">第 {d.seq} 版</td>
                  <td className="px-2">
                    <span className={cn('text-xs', TONE[d.status] || 'text-warning')} title={d.failure_text || ''}>
                      {PENDING.has(d.status) && <Spinner className="mr-1 inline size-3" />}{d.status_text || d.status}
                    </span>
                    {d.failure_text && <div className="max-w-[260px] truncate text-2xs text-muted-foreground" title={d.failure_text}>{d.failure_text}</div>}
                  </td>
                  <td className="px-2 text-xs text-muted-foreground">{d.source_text || d.source_kind}</td>
                  <td className="tnum px-2 text-xs text-muted-foreground">{timeText(d.created_at)}</td>
                  <td className="tnum px-2 text-right text-xs text-muted-foreground">{durationText(d.duration_seconds)}</td>
                  <td className="px-3.5 text-right">
                    <div className="flex items-center justify-end gap-1">
                      {d.can_rollback && d.status !== 'live' && <Button variant="outline" size="sm" disabled={!!busy} onClick={() => setTarget(d)}>回退</Button>}
                      {!d.can_rollback && d.rollback_text && d.status !== 'live' && <span className="text-2xs text-muted-foreground">{d.rollback_text}</span>}
                      {(d.logs?.run || d.logs?.build || d.status === 'live') && <Button variant="ghost" size="sm" onClick={() => go(appHref(a.name, 'logs', { v: String(d.seq) }))}>日志</Button>}
                    </div>
                  </td>
                </tr>
              ))}
              {list.data && rows.length === 0 && <tr><td colSpan={6} className="px-3.5 py-6 text-center text-xs text-muted-foreground">还没有部署记录</td></tr>}
            </tbody>
          </table>
        </div>
      </Card>
      <Dialog open={!!target} onClose={() => setTarget(null)} labelledBy="rb-title">
        {target && (
          <>
            <DialogHead id="rb-title" title={'回退到第 ' + target.seq + ' 版'} desc={(live ? '现在是第 ' + live.seq + ' 版 · ' : '') + '会生成一个新版本，不用重新下载'} />
            <ul className="flex flex-col gap-2 text-xs">
              <li className="flex gap-2"><ShieldCheck className="size-3.5 shrink-0 text-success" />健康检查通过才切换，失败继续用现在的版本</li>
              <li className="flex gap-2"><TriangleAlert className="size-3.5 shrink-0 text-warning" />只换程序，数据保持现在的样子</li>
            </ul>
            <CopyLine className="mt-3" text={'acornfox rollback ' + a.name + ' --to ' + target.seq} />
            <div className="mt-5 flex justify-end gap-2">
              <Button variant="outline" onClick={() => setTarget(null)}>取消</Button>
              <Button disabled={busy === 'rb'} onClick={() => rollback(target)}>{busy === 'rb' && <Spinner />}回退</Button>
            </div>
          </>
        )}
      </Dialog>
    </div>
  )
}
