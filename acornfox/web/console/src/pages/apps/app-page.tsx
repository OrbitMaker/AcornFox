// 一个应用的详情：名片（图标、名字、一句话状态、“打开”和“更多”菜单），下面四个标签页（概览、日志、版本、设置）。
// 每件事只放一个地方：地址和谁能打开在概览的访问卡片，版本在版本卡片和版本页签，能改的都在设置；
// 重启、停止、启动收在“更多”菜单里，删除在设置最下面。
// 应用视图和指标每 5 秒刷新。标签写在地址里（#app-<名字>/<标签>），刷新页面还在原处；以前的配置、数据、资源、高级四个
// 标签的地址跳到设置并展开对应的那一项（见 lib/router.ts）。
import { ExternalLink, MoreHorizontal, Play, RotateCw, Square } from 'lucide-react'
import { useState } from 'react'
import { api, appPath, type ApiError } from '@/lib/api'
import { STATUS, deriveStatus, failedDeployment, openableAddress, type AppStatus } from '@/lib/appstatus'
import { sinceText } from '@/lib/format'
import { usePoll } from '@/lib/hooks'
import { appHref, go, type AppTab, type SettingsSection } from '@/lib/router'
import type { AppMetrics, AppView } from '@/lib/types'
import { cn } from '@/lib/utils'
import { useConsole } from '@/state'
import { AppTile, Empty } from '@/components/bits'
import { Button } from '@/components/ui/button'
import { Spinner, Tabs } from '@/components/ui/controls'
import { Dropdown, MenuItem } from '@/components/ui/menu'
import { useToast } from '@/components/ui/toast'
import { ACTION_DONE, runAction, type AppAction } from './actions'
import { OverviewTab } from './tab-overview'
import { VersionsTab } from './tab-versions'
import { LogsTab } from './tab-logs'
import { SettingsTab } from './tab-settings'

export interface TabProps {
  app: AppView
  metrics: AppMetrics | null
  reload: () => Promise<void>
}

const TONE_TEXT = { success: 'text-success', danger: 'text-danger', warning: 'text-warning', muted: 'text-muted-foreground' } as const
const TONE_DOT = { success: 'bg-success', danger: 'bg-danger', warning: 'animate-pulse bg-warning', muted: 'bg-muted-foreground' } as const

// 名字下面的一句话状态：运行中写“运行正常 · 已连续运行 X”，别的状态照状态判断的文字。
export function statusLine(a: AppView, st: AppStatus): { text: string; since: string; tone: keyof typeof TONE_TEXT } {
  const { text, tone } = STATUS[st]
  if (st !== 'running') return { text, since: '', tone }
  const started = a.observed?.started_at
  return { text: '运行正常', since: started ? '已连续运行 ' + sinceText(started) : '', tone }
}

export function AppPage({ name, tab, section }: { name: string; tab: AppTab; section?: SettingsSection }) {
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
  const line = statusLine(a, st)
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
    ['overview', '概览', !!failedDeployment(a) || st === 'crashed'], ['logs', '日志'], ['versions', '版本'], ['settings', '设置'],
  ]
  const canStop = a.desired !== 'stopped' && st !== 'installing'
  const canRestart = st === 'running'
  const props: TabProps = { app: a, metrics: metrics.data, reload }
  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <AppTile a={a} size="lg" />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-lg font-semibold leading-6">{a.name}</h1>
          <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs" data-testid="app-status-line">
            <span className={cn('size-1.5 shrink-0 rounded-full', TONE_DOT[line.tone])} />
            <span className={cn('font-medium', TONE_TEXT[line.tone])}>{line.text}</span>
            {line.since && <span className="truncate text-muted-foreground">· {line.since}</span>}
          </div>
        </div>
        <div className="flex items-center gap-1.5">
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
          {(a.desired === 'stopped' || canStop || canRestart) && (
            <Dropdown width="w-32" trigger={(t, open) => (
              <Button variant="outline" size="icon" disabled={!!busy} onClick={t} aria-label="更多操作" aria-haspopup="menu" aria-expanded={open}>
                {busy ? <Spinner /> : <MoreHorizontal className="size-4" />}
              </Button>
            )}>
              {canRestart && <MenuItem icon={<RotateCw />} onClick={() => act('restart')}>重启</MenuItem>}
              {a.desired === 'stopped'
                ? <MenuItem icon={<Play />} onClick={() => act('start')}>启动</MenuItem>
                : canStop && <MenuItem icon={<Square />} onClick={() => act('stop')}>停止</MenuItem>}
            </Dropdown>
          )}
        </div>
      </div>
      <Tabs<AppTab> value={tab} onChange={(t) => go(appHref(a.name, t))} items={tabs} className="mb-4" label="应用详情" />
      {tab === 'overview' && <OverviewTab {...props} />}
      {tab === 'logs' && <LogsTab {...props} />}
      {tab === 'versions' && <VersionsTab {...props} />}
      {tab === 'settings' && <SettingsTab {...props} section={section} />}
    </div>
  )
}
