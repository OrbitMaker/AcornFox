// 我的应用：卡片和表格两种视图。数据来自 GET /v1/apps 和各应用的 /v1/metrics/apps/{app}（只在表格视图里读）。
// 样稿第 10 版（评估 X3、X5、Y1～Y5）：
//   卡片用应用中心里的名字和图标，版本号、地址不上卡片；
//   样稿第 11 版：每张卡片两行、一样高。第一行图标、名字，名字下面是状态（和首页的应用卡片同一个位置；要处理时后面跟一句
//   “没启动成功”这类小字，不再另起一个带底色的框）；第二行左边一句灰字（“来自应用中心”“自己部署的项目”，数据库这类写
//   “给别的应用用，不用打开”），右边“打开”或“怎么打开”（去详情）和“更多”；“新版本没换上、旧版本照常用”时“打开”能点；
//   应用少于 8 个时筛选、视图切换整排不显示（只用卡片）；搜索只用顶栏那一个（⌘K），页面里不再放第二个搜索框；
//   空的时候一句话加一个按钮，不放命令（部署自己的项目在“设置 › 开发者与 AI”里）。
//   最下面有一组默认收起的“这台机器上的其他容器”（用户自己装的，只看不管理，others-group.tsx）；没有就不显示。
//   表格是给应用多的人看的：仍有地址、内存、CPU、版本几列。
//   状态后面可以跟一句小字：“设置要跟上”（琥珀，平台自动填的值变了、应用还没跟上，GET /v1/platform/affected）、
//   “有新版本”（应用中心里有比现在新的，catalog_update）；都是服务端说了才写。
import { useEffect, useMemo, useState } from 'react'
import { ExternalLink, LayoutGrid, List, MoreHorizontal, Plus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api } from '@/lib/api'
import { PENDING, appAddress, deriveStatus, isLocalOnly, liveSeq, openableAddress, shortAddress, type AppStatus } from '@/lib/appstatus'
import { WORDS, canOpenNow, displayName, fromStore, plainStatus, useAppNames } from '@/lib/display'
import { mbText, pctText } from '@/lib/format'
import { appHref, go } from '@/lib/router'
import { outdatedCount } from '@/lib/app-upgrade'
import type { AppMetrics, AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { AppPlainBadge, AppTile, ErrorLine, PageHead, PlainBadge, VtName } from '@/components/bits'
import { vtKey } from '@/lib/continuity'
import { EmptyState } from '@/components/fold'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Spinner, Tabs } from '@/components/ui/controls'
import { Loading } from '@/components/ui/skeleton'
import { SkAppCards } from '@/components/skeletons'
import { Dropdown } from '@/components/ui/menu'
import { useToast } from '@/components/ui/toast'
import { ACTION_DONE, ACTION_FAIL, actionToast, runAction, type AppAction } from './actions'
import { AppMenuItems } from './app-menu'
import { OthersGroup } from './others-group'

type Filter = 'all' | 'running' | 'stopped' | 'problem'
const PROBLEM: AppStatus[] = ['failed', 'crashed']

// 应用少于这个数时，筛选和视图切换整排不显示（评估 Y4）。
export const FEW_APPS = 8

// originLine：卡片第二行，应用从哪来（评估 Y1）。
export function originLine(a: AppView): string {
  return fromStore(a) ? WORDS.fromStore : '自己部署的项目'
}

// versionLine：表格里的“版本”一列。应用中心装的只写应用自己的版本号（2.47.5）；“第几版”只给自己部署的项目。
export function versionLine(a: AppView): string {
  if (fromStore(a)) return a.catalog_version || ''
  const n = liveSeq(a)
  if (n) return '第 ' + n + ' 版'
  if (a.latest && PENDING.has(a.latest.status)) return '第 ' + a.latest.seq + ' 版更新中'
  if (a.current_deployment) return '已装好'
  if (a.latest) return '还没有装好的版本'
  return '还没装好'
}

// filterMyApps：q 也比对显示名（页面里不再有搜索框，留着给测试和以后用）。
export function filterMyApps(apps: AppView[], installing: Set<string>, f: Filter, q: string, nameOf?: (a: AppView) => string): AppView[] {
  const s = q.trim().toLowerCase()
  return apps.filter((a) => {
    const st = deriveStatus(a, installing)
    const okF = f === 'all' || (f === 'problem' ? PROBLEM.includes(st) : st === f)
    return okF && (!s || a.name.toLowerCase().includes(s) || (a.catalog_id || '').includes(s) || (nameOf ? nameOf(a).toLowerCase().includes(s) : false))
  })
}

// useMetrics：给在运行的应用每 10 秒读一次内存和 CPU。
function useMetrics(names: string[]) {
  const [m, setM] = useState<Record<string, AppMetrics>>({})
  const key = names.join(',')
  useEffect(() => {
    if (!key) return
    let alive = true
    const load = async () => {
      if (document.visibilityState !== 'visible') return
      const out: Record<string, AppMetrics> = {}
      await Promise.all(key.split(',').map(async (n) => {
        try { out[n] = await api.get<AppMetrics>('/v1/metrics/apps/' + encodeURIComponent(n)) } catch { /* 读不到就不显示 */ }
      }))
      if (alive) setM(out)
    }
    load()
    const t = window.setInterval(load, 10000)
    return () => { alive = false; window.clearInterval(t) }
  }, [key])
  return m
}

function useAct() {
  const { refreshApps } = useConsole()
  const nameOf = useAppNames()
  const toast = useToast()
  const [busy, setBusy] = useState('')
  // 没做成：出错的话留在那个应用的卡片上（样稿第 14 版，不弹提示）
  const [failed, setFailed] = useState<{ name: string; error: Error } | null>(null)
  const act = async (name: string, action: AppAction) => {
    setBusy(name)
    setFailed(null)
    try {
      await runAction(name, action)
      toast(ACTION_DONE[action], nameOf(name), actionToast(name, action, refreshApps))
      await refreshApps()
    } catch (e) {
      setFailed({ name, error: new Error(ACTION_FAIL[action] + '：' + (e as Error).message) })
    } finally {
      setBusy('')
    }
  }
  return { busy, act, failed }
}

function AppMenu({ a, st, act, label }: { a: AppView; st: AppStatus; act: (n: string, x: AppAction) => void; label: string }) {
  return (
    <Dropdown width="w-36" trigger={(t) => (
      <Button variant="ghost" size="icon-xs" onClick={(e) => { e.stopPropagation(); t() }} aria-label={label + ' 的更多操作'}><MoreHorizontal className="size-icon" /></Button>
    )}>
      <AppMenuItems a={a} st={st} act={(x) => act(a.name, x)} />
    </Dropdown>
  )
}

// LocalBadge：表格里只本机能打开的应用标“只本机”。
export function LocalBadge({ a }: { a: AppView }) {
  if (!isLocalOnly(a)) return null
  return <Badge variant="outline" className="shrink-0 font-normal" title="现在只有这台机器自己能打开；点进去看怎么打开">只本机</Badge>
}

// openKind：卡片底部给什么（评估 Y2）：open 能直接打开；how 只本机，去详情看怎么打开；internal 给别的应用用；none 什么都不给。
export function openKind(a: AppView): 'open' | 'how' | 'internal' | 'none' {
  if (a.internal_only) return 'internal'
  if (openableAddress(a)) return 'open'
  if (isLocalOnly(a) || a.open_hint) return 'how'
  return 'none'
}

// OpenAction：卡片底部那一个动作（首页“我的应用”也用它）。样稿第 14 版：和别处的小号按钮同一档（以前自己小一号）。“打开”只在现在打得开时能点（canOpenNow：新版本没换上、旧版本照常用时也能点）。
export function OpenAction({ a, st, className }: { a: AppView; st: AppStatus; className?: string }) {
  const k = openKind(a)
  if (k === 'internal') return <span className={cn('text-2xs text-muted-foreground', className)}>{WORDS.internalApp}</span>
  if (k === 'how') {
    return <a href={appHref(a.name)} onClick={(e) => e.stopPropagation()} className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), className)}>怎么打开</a>
  }
  if (k === 'open') {
    const can = canOpenNow(a, st)
    return (
      <a
        href={openableAddress(a)}
        target="_blank"
        rel="noopener noreferrer"
        onClick={(e) => e.stopPropagation()}
        className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), !can && 'pointer-events-none opacity-40', className)}
        aria-disabled={!can}
      >
        打开<ExternalLink className="size-icon" />
      </a>
    )
  }
  return null
}

function AppCard({ a, st, busy, act, error }: { a: AppView; st: AppStatus; busy: boolean; act: (n: string, x: AppAction) => void; error?: Error | null }) {
  const { catalog, behind } = useConsole()
  const name = displayName(catalog, a)
  const p = plainStatus(a, st)
  const internal = openKind(a) === 'internal'
  const late = behind.some((x) => x.app === a.name)
  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => go(appHref(a.name))}
      onKeyDown={(e) => { if (e.key === 'Enter') go(appHref(a.name)) }}
      className="group flex min-w-0 cursor-pointer flex-col gap-3 rounded-lg border bg-card p-3 transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      data-testid="my-app"
      data-vt-scope=""
    >
      <div className="flex items-center gap-2.5">
        <AppTile a={a} vt />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium"><VtName k={vtKey('app', a.name)}>{name}</VtName></div>
          <div className="flex min-w-0 items-center">
            {busy ? <Spinner className="size-2.5" /> : <PlainBadge p={p} withNote className="min-w-0" />}
            {late ? <span className="ml-1.5 shrink-0 text-2xs text-warning" data-testid="card-behind">· 设置要跟上</span>
              : fromStore(a) && a.catalog_update ? <span className="ml-1.5 shrink-0 text-2xs text-primary" data-testid="card-update">· 有新版本</span> : null}
          </div>
        </div>
      </div>
      <div className="mt-auto flex min-h-ctl-sm items-center gap-1.5">
        <span className="mr-auto min-w-0 truncate text-2xs text-muted-foreground">{internal ? WORDS.internalApp : originLine(a)}</span>
        {!internal && <OpenAction a={a} st={st} />}
        <AppMenu a={a} st={st} act={act} label={name} />
      </div>
      {error && <ErrorLine error={error} />}
    </div>
  )
}

function AppTable({ apps, installing, metrics }: { apps: AppView[]; installing: Set<string>; metrics: Record<string, AppMetrics> }) {
  const { catalog } = useConsole()
  return (
    <table className="w-full min-w-[620px] text-sm">
      <thead>
        <tr className="border-b text-left text-2xs text-muted-foreground">
          <th className="h-ctl px-3 font-medium">应用</th>
          <th className="px-2 font-medium">状态</th>
          <th className="px-2 font-medium">地址</th>
          <th className="px-2 text-right font-medium">内存</th>
          <th className="px-2 text-right font-medium">CPU</th>
          <th className="px-3 text-right font-medium">版本</th>
        </tr>
      </thead>
      <tbody>
        {apps.map((a) => {
          const m = metrics[a.name]
          const url = appAddress(a)
          return (
            <tr key={a.name} onClick={() => go(appHref(a.name))} className="cursor-pointer border-b last:border-0 hover:bg-muted/50" data-vt-scope="">
              <td className="px-3 py-2"><div className="flex items-center gap-2"><AppTile a={a} size="xs" vt /><a href={appHref(a.name)} onClick={(e) => e.stopPropagation()}><VtName k={vtKey('app', a.name)}>{displayName(catalog, a)}</VtName></a></div></td>
              <td className="px-2"><AppPlainBadge a={a} st={deriveStatus(a, installing)} /></td>
              <td className="px-2 text-2xs text-muted-foreground">{a.internal_only ? WORDS.internalApp : <div className="mono flex items-center gap-1.5">{url ? shortAddress(url) : '—'}<LocalBadge a={a} /></div>}</td>
              <td className="tnum px-2 text-right text-xs">{m?.available ? mbText(m.memory_usage_mb) + ' / ' + mbText(a.memory_mb) : '— / ' + mbText(a.memory_mb)}</td>
              <td className="tnum px-2 text-right text-xs">{m?.available && m.cpu_available !== false ? pctText(m.cpu_percent) : '—'}</td>
              <td className="tnum px-3 text-right text-xs text-muted-foreground">{versionLine(a)}{fromStore(a) && a.catalog_update ? <span className="ml-1.5 text-primary">· 有新版本</span> : null}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

function loadView(): 'grid' | 'list' {
  try { return localStorage.getItem('afx-apps-view') === 'list' ? 'list' : 'grid' } catch { return 'grid' }
}

export function AppsPage() {
  const { apps, appsError, installing, status } = useConsole()
  const [viewRaw, setViewRaw] = useState<'grid' | 'list'>(loadView)
  const [fRaw, setF] = useState<Filter>('all')
  const { busy, act, failed } = useAct()
  const setView = (v: 'grid' | 'list') => {
    setViewRaw(v)
    try { localStorage.setItem('afx-apps-view', v) } catch { /* 记不住就算了 */ }
  }
  const all = apps || []
  // 应用少于 FEW_APPS 个时整排不显示：只用卡片、不筛选。
  const many = all.length >= FEW_APPS
  const view = many ? viewRaw : 'grid'
  const f = many ? fRaw : 'all'
  const statuses = useMemo(() => new Map(all.map((a) => [a.name, deriveStatus(a, installing)])), [all, installing])
  const n = (fn: (s: AppStatus) => boolean) => all.filter((a) => fn(statuses.get(a.name)!)).length
  const list = filterMyApps(all, installing, f, '')
  const running = view === 'list' ? list.filter((a) => statuses.get(a.name) === 'running').map((a) => a.name) : []
  const metrics = useMetrics(running)
  const problems = n((s) => PROBLEM.includes(s))
  // 有新版本的应用有几个：服务端的 apps_outdated 优先，没有就数应用里带 catalog_update 的
  const outdated = outdatedCount(status) || all.filter((a) => fromStore(a) && !!a.catalog_update).length

  return (
    <div>
      <PageHead meta={outdated > 0 ? `${outdated} 个应用有新版本` : undefined}>
        <a href="#store" className={buttonVariants()}><Plus className="size-icon" />装新应用</a>
      </PageHead>
      <ErrorLine error={appsError} className="mb-3" />
      {apps && all.length === 0 ? (
        <EmptyState title="还没有应用" action={<a href="#store" className={buttonVariants()}>去应用中心挑一个</a>} />
      ) : (
        <>
          {many && (
            <div className="mb-3 flex flex-wrap items-center gap-2">
              <Tabs<Filter>
                value={f}
                onChange={setF}
                label="按状态筛选"
                items={[['all', '全部'], ['running', '正常 ' + n((s) => s === 'running')], ['stopped', '停了 ' + n((s) => s === 'stopped')], ['problem', '要处理 ' + problems, problems > 0]]}
              />
              <Tabs<'grid' | 'list'> className="ml-auto" value={view} onChange={setView} label="视图" items={[['grid', <><LayoutGrid className="size-icon" /><span className="sr-only">卡片</span></>], ['list', <><List className="size-icon" /><span className="sr-only">表格</span></>]]} />
            </div>
          )}
          <Loading when={!apps} skeleton={<SkAppCards />}>
            {view === 'grid' ? (
              <div className="sk-kids grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {list.map((a) => <AppCard key={a.name} a={a} st={statuses.get(a.name)!} busy={busy === a.name} act={act} error={failed?.name === a.name ? failed.error : null} />)}
              </div>
            ) : (
              <div className="overflow-x-auto rounded-lg border bg-card">
                <AppTable apps={list} installing={installing} metrics={metrics} />
              </div>
            )}
          </Loading>
          {apps && list.length === 0 && <EmptyState className="mt-3" title="没有这种状态的应用" action={<Button variant="outline" size="sm" onClick={() => setF('all')}>看全部</Button>} />}
        </>
      )}
      <OthersGroup />
    </div>
  )
}
