// 一个应用的详情：顶部状态和操作，下面七个标签页（概览、版本记录、日志、配置、数据、资源、高级）。
// 应用视图和指标每 5 秒刷新。标签写在地址里（#app-<名字>/<标签>），刷新页面还在原处。
import { ExternalLink, Play, RotateCw, Square } from 'lucide-react'
import { useState } from 'react'
import { api, appPath, type ApiError } from '@/lib/api'
import { appAddress, deriveStatus, failedDeployment, isLocalOnly, openableAddress, shortAddress } from '@/lib/appstatus'
import { usePoll } from '@/lib/hooks'
import { appHref, go, type AppTab } from '@/lib/router'
import type { AppMetrics, AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { AppTile, Empty, StatusBadge } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Spinner, Tabs } from '@/components/ui/controls'
import { useToast } from '@/components/ui/toast'
import { ACTION_DONE, runAction, type AppAction } from './actions'
import { versionLine } from './apps-page'
import { OverviewTab } from './tab-overview'
import { VersionsTab } from './tab-versions'
import { LogsTab } from './tab-logs'
import { ConfigTab } from './tab-config'
import { DataTab } from './tab-data'
import { ResourcesTab } from './tab-resources'
import { AdvancedTab } from './tab-advanced'

export interface TabProps {
  app: AppView
  metrics: AppMetrics | null
  reload: () => Promise<void>
}

export function AppPage({ name, tab }: { name: string; tab: AppTab }) {
  const { installing, refreshApps } = useConsole()
  const toast = useToast()
  const [busy, setBusy] = useState<AppAction | ''>('')
  const view = usePoll((s) => api.get<AppView>(appPath(name), s), 5000, [name])
  const metrics = usePoll((s) => api.get<AppMetrics>('/v1/metrics/apps/' + encodeURIComponent(name), s).catch(() => null), 5000, [name])
  const reload = async () => { await Promise.all([view.refresh(), metrics.refresh(), refreshApps()]) }

  const err = view.error as ApiError | null
  if (err && err.status === 404) return <Empty>没有这个应用。<a href="#apps" className="underline">回我的应用</a></Empty>
  const a = view.data
  if (!a) return err ? <Empty>{err.message}</Empty> : <div className="flex items-center gap-2 text-xs text-muted-foreground"><Spinner />正在读取…</div>

  const st = deriveStatus(a, installing)
  const url = appAddress(a)
  const openURL = openableAddress(a)
  const act = async (x: AppAction) => {
    setBusy(x)
    try {
      await runAction(a.name, x)
      toast(ACTION_DONE[x], a.name)
      await reload()
    } catch (e) {
      toast('没能完成', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }
  const tabs: [AppTab, string, boolean?][] = [
    ['overview', '概览', !!failedDeployment(a) || st === 'crashed'], ['versions', '版本记录'], ['logs', '日志'], ['config', '配置'], ['data', '数据'], ['resources', '资源'], ['advanced', '高级'],
  ]
  const props: TabProps = { app: a, metrics: metrics.data, reload }
  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <AppTile a={a} size="lg" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="text-lg font-semibold">{a.name}</h1>
            <StatusBadge s={st} />
            {a.catalog_id && <Badge variant="outline">{versionLine(a)}</Badge>}
          </div>
          <div className="tnum truncate text-xs text-muted-foreground">
            {a.live ? '第 ' + a.live.seq + ' 版' + (a.live.source_text ? ' · ' + a.live.source_text : '') : '还没有上线的版本'}
            {url && <> · <span className="mono">{shortAddress(url)}</span>{isLocalOnly(a) && '（只本机）'}</>}
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          {a.desired === 'stopped' ? (
            <Button variant="outline" disabled={!!busy} onClick={() => act('start')}>{busy === 'start' ? <Spinner /> : <Play className="size-3.5" />}启动</Button>
          ) : (
            <Button variant="outline" disabled={!!busy || st === 'installing'} onClick={() => act('stop')}>{busy === 'stop' ? <Spinner /> : <Square className="size-3.5" />}停止</Button>
          )}
          <Button variant="outline" size="icon" disabled={!!busy || st !== 'running'} onClick={() => act('restart')} aria-label="重启" title="重启">
            {busy === 'restart' ? <Spinner /> : <RotateCw className="size-3.5" />}
          </Button>
          {/* 只本机的应用在 Linux 服务器上点不开：概览里有怎么打开的说明。 */}
          {!openURL && a.open_hint && (
            <Button onClick={() => {
              go(appHref(a.name))
              window.setTimeout(() => document.querySelector('[data-testid="open-hint"]')?.scrollIntoView({ block: 'center' }), 50)
            }}>怎么打开</Button>
          )}
          {openURL && (
            <a href={openURL} target="_blank" rel="noopener noreferrer" aria-disabled={st !== 'running'} className={'inline-flex h-8 items-center gap-1.5 rounded-md bg-primary px-3 text-sm font-medium text-primary-foreground hover:bg-primary/90' + (st !== 'running' ? ' pointer-events-none opacity-50' : '')}>
              打开<ExternalLink className="size-3.5" />
            </a>
          )}
        </div>
      </div>
      <Tabs<AppTab> value={tab} onChange={(t) => go(appHref(a.name, t))} items={tabs} className="mb-4" label="应用详情" />
      {tab === 'overview' && <OverviewTab {...props} />}
      {tab === 'versions' && <VersionsTab {...props} />}
      {tab === 'logs' && <LogsTab {...props} />}
      {tab === 'config' && <ConfigTab {...props} />}
      {tab === 'data' && <DataTab {...props} />}
      {tab === 'resources' && <ResourcesTab {...props} />}
      {tab === 'advanced' && <AdvancedTab {...props} />}
    </div>
  )
}
