// 一个应用的详情：名片（图标、名字、一句话状态、“打开”和“更多”菜单），下面四个标签页（概览、日志、更新或版本、设置）。
// 样稿第 10 版：
//   名字用应用中心里的名字（lib/display.ts 的 displayName），英文代号只在技术信息、命令行、地址里出现；
//   手机宽度下名字上面有“‹ 我的应用”返回（评估 X10）；
//   状态只有正常、停了、要处理（plainStatus），新版本没换上时写“新版本没换上（旧版本照常用）”，“打开”照样能点（canOpenNow）；
//   第三个页签：从应用中心装的应用叫“更新”（只写现在的版本、有没有新版本），自己部署的项目叫“版本”（版本记录和退回），
//   地址里都还是 versions（#app-<名字>/versions），旧链接照样能用。
// 每件事只放一个地方：地址和谁能打开在概览最上面的“谁能打开”卡片，能改的都在设置；
// 重启、停止、启动收在“更多”菜单里，删除在设置最下面。只本机、点不开的应用，“打开”换成“怎么打开”，点了滚到那张卡片。
// 应用视图和指标每 5 秒刷新。标签写在地址里（#app-<名字>/<标签>），刷新页面还在原处；以前的配置、数据、资源、高级四个
// 标签的地址跳到设置并展开对应的那一项（见 lib/router.ts）。
import { ChevronLeft, ExternalLink, MoreHorizontal } from 'lucide-react'
import { useState } from 'react'
import { api, appPath, type ApiError } from '@/lib/api'
import { deriveStatus, failedDeployment, openableAddress, type AppStatus } from '@/lib/appstatus'
import { canOpenNow, displayName, fromStore, plainStatus, type PlainTone } from '@/lib/display'
import { sinceText } from '@/lib/format'
import { usePoll } from '@/lib/hooks'
import { appHref, go, type AppTab, type SettingsSection } from '@/lib/router'
import type { AppMetrics, AppView } from '@/lib/types'
import { cn } from '@/lib/utils'
import { useConsole } from '@/state'
import { AppTile, ErrorLine, StatusDot, TONE_TEXT, VtName } from '@/components/bits'
import { vtKey } from '@/lib/continuity'
import { EmptyState } from '@/components/fold'
import { Button, buttonVariants } from '@/components/ui/button'
import { Tabs } from '@/components/ui/controls'
import { Loading } from '@/components/ui/skeleton'
import { SkAppHead, SkOverview } from '@/components/skeletons'
import { Dropdown } from '@/components/ui/menu'
import { useToast } from '@/components/ui/toast'
import { ACTION_DONE, ACTION_FAIL, actionToast, runAction, type AppAction } from './actions'
import { AppMenuItems } from './app-menu'
import { OverviewTab } from './tab-overview'
import { VersionsTab } from './tab-versions'
import { LogsTab } from './tab-logs'
import { SettingsTab } from './tab-settings'
import { WHO_CARD_ID } from './who-card'

export interface TabProps {
  app: AppView
  metrics: AppMetrics | null
  // 用量第一次还没读到
  metricsLoading?: boolean
  reload: () => Promise<void>
}


// 名字下面的一句话状态：正常时写“正常 · 已连续运行 X”，别的照 plainStatus（带括号里的补充）。
export function statusLine(a: AppView, st: AppStatus): { text: string; since: string; tone: PlainTone } {
  const p = plainStatus(a, st)
  if (st !== 'running') return { text: p.text + (p.note ? '（' + p.note + '）' : ''), since: '', tone: p.tone }
  const started = a.observed?.started_at
  return { text: p.text, since: started ? '已连续运行 ' + sinceText(started) : '', tone: p.tone }
}

const BACK = 'tap-h inline-flex items-center gap-0.5 text-xs text-muted-foreground hover:text-foreground sm:hidden'

export function AppPage({ name, tab, section }: { name: string; tab: AppTab; section?: SettingsSection }) {
  const { installing, refreshApps, catalog, apps } = useConsole()
  const toast = useToast()
  const [busy, setBusy] = useState<AppAction | ''>('')
  // 启动、停止、重启没做成：出错的话留在名片下面（样稿第 14 版，不弹提示）
  const [actErr, setActErr] = useState<Error | null>(null)
  const view = usePoll((s) => api.get<AppView>(appPath(name), s), 5000, [name])
  const metrics = usePoll((s) => api.get<AppMetrics>('/v1/metrics/apps/' + encodeURIComponent(name), s).catch(() => null), 5000, [name])
  const reload = async () => { await Promise.all([view.refresh(), metrics.refresh(), refreshApps()]) }

  const err = view.error as ApiError | null
  const back = <a href="#apps" className={BACK}><ChevronLeft className="size-icon" />我的应用</a>
  if (err && err.status === 404) {
    return (
      <div className="flex flex-col gap-3">
        {back}
        <EmptyState title="没有这个应用，可能已经删掉了。" action={<a href="#apps" className={buttonVariants({ variant: 'outline', size: 'sm' })}>回我的应用</a>} />
      </div>
    )
  }
  // 样稿第 12 版：名片（图标、名字、状态）先用应用列表里已经有的那一份画出来，详情到了再换成详情；页签一直在；
  // 页签里的内容等详情，等的时候按真版面画占位形状。列表和详情都还没到（直接打开这一页）时名片也是占位形状。
  const full = view.data
  const a = full || (apps || []).find((x) => x.name === name) || null
  if (!a && err) return <EmptyState title={'没能读取：' + err.message} />
  if (!a) {
    return (
      <div>
        {back}
        <Loading when skeleton={<SkAppHead />}>{null}</Loading>
        <Tabs<AppTab> value={tab} onChange={(t) => go(appHref(name, t))} items={[['overview', '概览'], ['logs', '日志'], ['versions', '更新'], ['settings', '设置']]} className="mb-4" label="应用详情" />
        <Loading when skeleton={<SkOverview />}>{null}</Loading>
      </div>
    )
  }

  const label = displayName(catalog, a)
  const st = deriveStatus(a, installing)
  const line = statusLine(a, st)
  const openURL = openableAddress(a)
  const canOpen = canOpenNow(a, st)
  const act = async (x: AppAction) => {
    setBusy(x)
    setActErr(null)
    try {
      await runAction(a.name, x)
      toast(ACTION_DONE[x], label, actionToast(a.name, x, reload))
      await reload()
    } catch (e) {
      setActErr(new Error(ACTION_FAIL[x] + '：' + (e as Error).message))
    } finally {
      setBusy('')
    }
  }
  const tabs: [AppTab, string, boolean?][] = [
    ['overview', '概览', !!failedDeployment(a) || st === 'crashed'], ['logs', '日志'], ['versions', fromStore(a) ? '更新' : '版本'], ['settings', '设置'],
  ]
  const props: TabProps = { app: full || a, metrics: metrics.data, metricsLoading: metrics.loading && !metrics.data, reload }
  return (
    <div>
      {back}
      <div className="mb-4 mt-1 flex flex-wrap items-center gap-3 sm:mt-0">
        <AppTile a={a} size="lg" vt />
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-lg font-semibold"><VtName k={vtKey('app', a.name)}>{label}</VtName></h1>
          <div className="mt-0.5 flex min-w-0 items-center gap-1.5 text-xs" data-testid="app-status-line">
            <StatusDot tone={line.tone} />
            <span className={cn('font-medium', TONE_TEXT[line.tone])}>{line.text}</span>
            {line.since && <span className="truncate text-muted-foreground">· {line.since}</span>}
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          {/* 只本机、在这台机器以外点不开的应用：滚到概览最上面的“谁能打开”卡片。 */}
          {!openURL && a.open_hint && (
            <Button onClick={() => {
              go(appHref(a.name))
              window.setTimeout(() => document.getElementById(WHO_CARD_ID)?.scrollIntoView({ block: 'start', behavior: 'smooth' }), 50)
            }}>怎么打开</Button>
          )}
          {openURL && (
            <a href={openURL} target="_blank" rel="noopener noreferrer" aria-disabled={!canOpen} className={cn(buttonVariants(), !canOpen && 'pointer-events-none opacity-50')}>
              打开<ExternalLink className="size-icon" />
            </a>
          )}
          <Dropdown width="w-36" trigger={(t, open) => (
            <Button variant="outline" size="icon" loading={!!busy} onClick={t} aria-label="更多操作" aria-haspopup="menu" aria-expanded={open}>
              <MoreHorizontal className="size-4" />
            </Button>
          )}>
            <AppMenuItems a={a} st={st} act={act} />
          </Dropdown>
        </div>
      </div>
      <ErrorLine error={actErr} className="-mt-2 mb-3" />
      <Tabs<AppTab> value={tab} onChange={(t) => go(appHref(a.name, t))} items={tabs} className="mb-4" label="应用详情" />
      <Loading when={!full} skeleton={<SkOverview />}>
        {() => (
          <>
            {tab === 'overview' && <OverviewTab {...props} />}
            {tab === 'logs' && <LogsTab {...props} />}
            {tab === 'versions' && <VersionsTab {...props} />}
            {tab === 'settings' && <SettingsTab {...props} section={section} />}
          </>
        )}
      </Loading>
    </div>
  )
}
