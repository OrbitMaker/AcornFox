// 我的应用：卡片和表格两种视图，状态、地址、内存、CPU。数据来自 GET /v1/apps 和各应用的 /v1/metrics/apps/{app}。
import { useEffect, useMemo, useState, type MouseEvent } from 'react'
import { ExternalLink, FileText, History, LayoutGrid, List, MoreHorizontal, Play, Plus, RotateCw, Search, Square, Trash2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api } from '@/lib/api'
import { PENDING, appAddress, deriveStatus, failedDeployment, isLocalOnly, liveSeq, openableAddress, shortAddress, type AppStatus } from '@/lib/appstatus'
import { mbText, pctText } from '@/lib/format'
import { appHref, go } from '@/lib/router'
import type { AppMetrics, AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { AppTile, Empty, ErrorLine, PageHead, StatusBadge } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Spinner, Tabs } from '@/components/ui/controls'
import { Input } from '@/components/ui/input'
import { Dropdown, MenuItem, MenuSep } from '@/components/ui/menu'
import { useToast } from '@/components/ui/toast'
import { ACTION_DONE, runAction, type AppAction } from './actions'

type Filter = 'all' | 'running' | 'stopped' | 'problem'
const PROBLEM: AppStatus[] = ['failed', 'crashed']

export function versionLine(a: AppView): string {
  if (a.catalog_id) return (a.catalog_origin ? '来自 ' + (a.catalog_origin_name || a.catalog_origin) : '来自应用中心') + ' · ' + (a.catalog_version || a.catalog_id) + (a.catalog_disconnected ? ' · 目录已断开' : '')
  const n = liveSeq(a)
  if (n) return '第 ' + n + ' 版'
  if (a.latest && PENDING.has(a.latest.status)) return '第 ' + a.latest.seq + ' 版部署中'
  if (a.current_deployment) return '已上线'
  if (a.latest) return '还没有上线的版本'
  return '还没部署'
}

export function filterMyApps(apps: AppView[], installing: Set<string>, f: Filter, q: string): AppView[] {
  const s = q.trim().toLowerCase()
  return apps.filter((a) => {
    const st = deriveStatus(a, installing)
    const okF = f === 'all' || (f === 'problem' ? PROBLEM.includes(st) : st === f)
    return okF && (!s || a.name.toLowerCase().includes(s) || (a.catalog_id || '').includes(s))
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
  const toast = useToast()
  const [busy, setBusy] = useState('')
  const act = async (name: string, action: AppAction) => {
    setBusy(name)
    try {
      await runAction(name, action)
      toast(ACTION_DONE[action], name)
      await refreshApps()
    } catch (e) {
      toast('没能完成', (e as Error).message, 'error')
    } finally {
      setBusy('')
    }
  }
  return { busy, act }
}

function AppMenu({ a, st, act }: { a: AppView; st: AppStatus; act: (n: string, x: AppAction) => void }) {
  const stop = (e: MouseEvent) => e.stopPropagation()
  return (
    <Dropdown width="w-36" trigger={(t) => (
      <Button variant="ghost" size="icon-xs" onClick={(e) => { e.stopPropagation(); t() }} aria-label={a.name + ' 的更多操作'}><MoreHorizontal className="size-3.5" /></Button>
    )}>
      {a.desired === 'stopped' ? (
        <MenuItem icon={<Play />} onClick={(e) => { stop(e); act(a.name, 'start') }}>启动</MenuItem>
      ) : (
        <MenuItem icon={<Square />} onClick={(e) => { stop(e); act(a.name, 'stop') }}>停止</MenuItem>
      )}
      {st === 'running' && <MenuItem icon={<RotateCw />} onClick={(e) => { stop(e); act(a.name, 'restart') }}>重启</MenuItem>}
      <MenuItem icon={<FileText />} href={appHref(a.name, 'logs')} onClick={stop}>日志</MenuItem>
      <MenuItem icon={<History />} href={appHref(a.name, 'versions')} onClick={stop}>版本</MenuItem>
      <MenuSep />
      <MenuItem icon={<Trash2 />} danger href={appHref(a.name, 'advanced')} onClick={stop}>删除</MenuItem>
    </Dropdown>
  )
}

// LocalBadge：只本机能打开的应用标“只本机”。
export function LocalBadge({ a }: { a: AppView }) {
  if (!isLocalOnly(a)) return null
  return <Badge variant="outline" className="shrink-0 font-normal" title="只在服务器本机能打开；点进去看怎么从自己电脑打开">只本机</Badge>
}

// OpenLink：用户在自己电脑上能点开的地址才给“打开”；Linux 服务器上只本机的应用不给（详情里有怎么打开）。
function OpenLink({ a, st, className }: { a: AppView; st: AppStatus; className?: string }) {
  const url = openableAddress(a)
  if (!url) return null
  return (
    <a
      href={url}
      target="_blank"
      rel="noopener noreferrer"
      onClick={(e) => e.stopPropagation()}
      className={cn('inline-flex h-6 items-center gap-1 rounded border bg-background px-2 text-2xs font-medium hover:bg-accent', st !== 'running' && 'pointer-events-none opacity-40', className)}
      aria-disabled={st !== 'running'}
    >
      打开<ExternalLink className="size-3" />
    </a>
  )
}

function AppCard({ a, st, busy, act }: { a: AppView; st: AppStatus; busy: boolean; act: (n: string, x: AppAction) => void }) {
  const failed = failedDeployment(a)
  const url = appAddress(a)
  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => go(appHref(a.name))}
      onKeyDown={(e) => { if (e.key === 'Enter') go(appHref(a.name)) }}
      className="group flex min-w-0 cursor-pointer flex-col gap-3 rounded-lg border bg-card p-3 transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      data-testid="my-app"
    >
      <div className="flex items-center gap-2.5">
        <AppTile a={a} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{a.name}</div>
          <div className="tnum truncate text-2xs text-muted-foreground">{versionLine(a)}</div>
        </div>
        {busy ? <Spinner /> : <StatusBadge s={st} />}
      </div>
      {failed && <div className="truncate rounded border border-danger/25 bg-danger/5 px-2 py-1 text-2xs text-danger">{failed.diagnosis?.message || failed.failure_text || '部署失败'}</div>}
      <div className="mt-auto flex items-center gap-1.5">
        <span className="mono min-w-0 flex-1 truncate text-2xs text-muted-foreground">{url ? shortAddress(url) : '只在容器网络里'}</span>
        <LocalBadge a={a} />
        <OpenLink a={a} st={st} />
        <AppMenu a={a} st={st} act={act} />
      </div>
    </div>
  )
}

function AppTable({ apps, installing, metrics }: { apps: AppView[]; installing: Set<string>; metrics: Record<string, AppMetrics> }) {
  return (
    <table className="w-full min-w-[620px] text-sm">
      <thead>
        <tr className="border-b text-left text-2xs text-muted-foreground">
          <th className="h-8 px-3 font-medium">应用</th>
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
            <tr key={a.name} onClick={() => go(appHref(a.name))} className="cursor-pointer border-b last:border-0 hover:bg-muted/50">
              <td className="px-3 py-2"><div className="flex items-center gap-2"><AppTile a={a} size="xs" /><a href={appHref(a.name)} onClick={(e) => e.stopPropagation()}>{a.name}</a></div></td>
              <td className="px-2"><StatusBadge s={deriveStatus(a, installing)} /></td>
              <td className="mono px-2 text-2xs text-muted-foreground"><div className="flex items-center gap-1.5">{url ? shortAddress(url) : '—'}<LocalBadge a={a} /></div></td>
              <td className="tnum px-2 text-right text-xs">{m?.available ? mbText(m.memory_usage_mb) + ' / ' + mbText(a.memory_mb) : '— / ' + mbText(a.memory_mb)}</td>
              <td className="tnum px-2 text-right text-xs">{m?.available && m.cpu_available !== false ? pctText(m.cpu_percent) : '—'}</td>
              <td className="tnum px-3 text-right text-xs text-muted-foreground">{versionLine(a)}</td>
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
  const { apps, appsError, installing } = useConsole()
  const [view, setViewRaw] = useState<'grid' | 'list'>(loadView)
  const [f, setF] = useState<Filter>('all')
  const [q, setQ] = useState('')
  const { busy, act } = useAct()
  const setView = (v: 'grid' | 'list') => {
    setViewRaw(v)
    try { localStorage.setItem('afx-apps-view', v) } catch { /* 记不住就算了 */ }
  }
  const all = apps || []
  const statuses = useMemo(() => new Map(all.map((a) => [a.name, deriveStatus(a, installing)])), [all, installing])
  const n = (fn: (s: AppStatus) => boolean) => all.filter((a) => fn(statuses.get(a.name)!)).length
  const list = filterMyApps(all, installing, f, q)
  const running = view === 'list' ? list.filter((a) => statuses.get(a.name) === 'running').map((a) => a.name) : []
  const metrics = useMetrics(running)
  const problems = n((s) => PROBLEM.includes(s))

  return (
    <div>
      <PageHead title="我的应用" meta={apps ? all.length + ' 个' : undefined}>
        <a href="#store" className={buttonVariants()}><Plus className="size-3.5" />装新应用</a>
      </PageHead>
      <ErrorLine error={appsError} className="mb-3" />
      {apps && all.length === 0 ? (
        <Empty>
          还没有应用。<a href="#store" className="underline">去应用中心装一个</a>，或者用命令行 <span className="mono">acornfox deploy</span> 部署自己的项目。
        </Empty>
      ) : (
        <>
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <Tabs<Filter>
              value={f}
              onChange={setF}
              label="按状态筛选"
              items={[['all', '全部'], ['running', '运行中 ' + n((s) => s === 'running')], ['stopped', '已停止 ' + n((s) => s === 'stopped')], ['problem', '有问题 ' + problems, problems > 0]]}
            />
            <div className="relative ml-auto w-full sm:w-52">
              <Search className="absolute left-2 top-2 size-3.5 text-muted-foreground" />
              <Input className="pl-7" placeholder="搜索" aria-label="搜索我的应用" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
            <Tabs<'grid' | 'list'> value={view} onChange={setView} label="视图" items={[['grid', <><LayoutGrid className="size-3.5" /><span className="sr-only">卡片</span></>], ['list', <><List className="size-3.5" /><span className="sr-only">表格</span></>]]} />
          </div>
          {!apps && <div className="flex items-center gap-2 text-xs text-muted-foreground"><Spinner />正在读取…</div>}
          {view === 'grid' ? (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {list.map((a) => <AppCard key={a.name} a={a} st={statuses.get(a.name)!} busy={busy === a.name} act={act} />)}
            </div>
          ) : (
            <div className="overflow-x-auto rounded-lg border bg-card">
              <AppTable apps={list} installing={installing} metrics={metrics} />
            </div>
          )}
          {apps && list.length === 0 && <Empty className="mt-3">没有符合条件的应用</Empty>}
        </>
      )}
    </div>
  )
}
