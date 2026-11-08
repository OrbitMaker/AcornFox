// 应用中心列表页：配方卡片、来源和分类标签、搜索、应用小卡片、连接私有目录。数据全部来自 GET /v1/catalog。
// 私有目录的应用标“来自：目录名”，详情和安装都带上目录编号；已断开的私有目录在顶部提示。
// 这一版私有目录先不开放：“连接私有目录”按钮和来源标签由 PRIVATE_CATALOG_UI 控制，默认藏起。
import { useMemo, useState } from 'react'
import { Link2, PlayCircle, RefreshCw, Search, Sparkles, Unlink } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { api } from '@/lib/api'
import { PUBLIC, appKey, appsIn, categoryTabs, disconnectedSources, filterApps, filterSource, freeMB, matchRecipe, memoryCheck, recipeAppNames, recipeMemory, sourceId, sourceTabs } from '@/lib/catalog'
import { mbText } from '@/lib/format'
import { PRIVATE_CATALOG_UI } from '@/lib/features'
import { appHref, go, itemHref } from '@/lib/router'
import type { CatalogApp, Recipe, TaskView } from '@/lib/types'
import { useConsole } from '@/state'
import { Empty, PageHead, TierBadge, Tile } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Alert, Spinner, Tabs } from '@/components/ui/controls'
import { Input } from '@/components/ui/input'
import { useToast } from '@/components/ui/toast'
import { ExpertButton } from './shared'
import { ConnectCatalogDialog } from './connect-dialog'

// runningTaskFor：按“目录来源 + id”找进行中的安装任务。公共目录和私有目录可能有同 id 的应用，
// 只看 id 会把公共版的进度显示到私有那张卡片上。没带 catalog 的旧任务算公共目录。
export function runningTaskFor(tasks: TaskView[], kind: 'app' | 'recipe', id: string, catalog: string = PUBLIC): TaskView | undefined {
  return tasks.find((t) => t.status === 'running' && t.kind === kind && t.target === id && (t.catalog || PUBLIC) === catalog)
}

// SourceBadge：私有目录的应用和配方标“来自：目录名”。
export function SourceBadge({ x }: { x: { source?: { id: string; name: string } } }) {
  if (sourceId(x) === PUBLIC) return null
  return <Badge variant="outline" className="max-w-[10rem] truncate font-normal" title={x.source?.id}>来自：{x.source?.name || x.source?.id}</Badge>
}

function RecipeCard({ r }: { r: Recipe }) {
  const { catalog, resources, tasks, install } = useConsole()
  const mem = recipeMemory(r, appsIn(catalog, sourceId(r)))
  const check = memoryCheck(mem.need, mem.min, freeMB(resources))
  const running = runningTaskFor(tasks, 'recipe', r.id, sourceId(r))
  const minutes = r.delivery.minutes
  const video = safeHref(r.delivery.video)
  return (
    <div className="flex min-w-0 flex-col gap-2.5 rounded-lg border bg-card p-3.5">
      <div className="flex items-center gap-2">
        <Sparkles className="size-3.5 shrink-0" />
        <span className="min-w-0 truncate text-sm font-medium">{r.title}</span>
        <span className="ml-auto flex items-center gap-1"><SourceBadge x={r} /><TierBadge tier={r.delivery.tier} /></span>
      </div>
      {r.summary && <p className="text-xs text-muted-foreground">{r.summary}</p>}
      <div className="flex flex-wrap gap-1">
        {recipeAppNames(r, catalog).map((x) => <Badge key={x} variant="secondary" className="font-normal">{x}</Badge>)}
      </div>
      <div className="mt-auto flex items-center gap-2 pt-1">
        <span className={cn('tnum flex-1 text-2xs', check.tone === 'ok' ? 'text-muted-foreground' : 'text-warning')} title={check.text}>
          {mem.need > 0 ? <>{minutes ? `${minutes} 分钟 · ` : ''}{mbText(mem.need)} 内存</> : '都已装好，不再占内存'}
        </span>
        {video && (
          <a href={video} target="_blank" rel="noopener noreferrer" className="inline-flex size-7 items-center justify-center rounded-md hover:bg-accent" aria-label="看视频" title="看视频">
            <PlayCircle className="size-3.5" />
          </a>
        )}
        <Button size="sm" onClick={() => install({ kind: 'recipe', id: r.id, taskId: running?.id, catalog: sourceId(r) })}>
          {running ? <><Spinner />安装中</> : '开始'}
        </Button>
      </div>
    </div>
  )
}

function AppCard({ a }: { a: CatalogApp }) {
  const { tasks, install } = useConsole()
  const src = sourceId(a)
  const running = runningTaskFor(tasks, 'app', a.id, src)
  const href = itemHref(a.id, src)
  let action
  if (running) action = <Button size="sm" variant="secondary" onClick={(e) => { e.stopPropagation(); install({ kind: 'app', id: a.id, taskId: running.id, catalog: src }) }}><Spinner />安装中</Button>
  else if (a.installed.length > 0) action = <Button size="sm" variant="ghost" className="text-muted-foreground" onClick={(e) => { e.stopPropagation(); go(appHref(a.installed[0])) }}>已安装 · 管理</Button>
  else if (!a.supported) action = <span className="text-2xs text-muted-foreground" title={'只支持 ' + a.platforms.join('、')}>这台机器用不了</span>
  else action = <Button size="sm" variant="secondary" onClick={(e) => { e.stopPropagation(); install({ kind: 'app', id: a.id, catalog: src }) }}>安装</Button>
  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => go(href)}
      onKeyDown={(e) => { if (e.key === 'Enter') go(href) }}
      className="flex min-w-0 cursor-pointer items-center gap-2.5 rounded-lg border bg-card p-2.5 transition-colors hover:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      data-testid="catalog-app"
    >
      <Tile id={a.id} name={a.name} icon={a.icon} iconType={a.icon_type} />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-1.5"><span className="truncate text-sm font-medium">{a.name}</span><SourceBadge x={a} /></div>
        <div className="truncate text-2xs text-muted-foreground">{a.summary}</div>
      </div>
      {action}
    </div>
  )
}

export function StorePage() {
  const { catalog, catalogError, refreshCatalog } = useConsole()
  const toast = useToast()
  const [q, setQ] = useState('')
  const [group, setGroup] = useState('all')
  const [syncing, setSyncing] = useState(false)
  const [src, setSrc] = useState('all')
  const [connect, setConnect] = useState(false)
  const srcTabs = useMemo(() => sourceTabs(catalog), [catalog])
  const source = srcTabs.some(([k]) => k === src) ? src : 'all'
  const apps = useMemo(() => filterSource(catalog?.apps || [], source), [catalog, source])
  const tabs = useMemo(() => categoryTabs(apps), [apps])
  const list = filterApps(apps, group, q)
  const recipes = filterSource(catalog?.recipes || [], source).filter((r) => matchRecipe(r, q))
  const gone = disconnectedSources(catalog)

  const sync = async () => {
    setSyncing(true)
    try {
      const st = await api.post<{ ok: boolean; skipped?: boolean; updated?: boolean; code?: string; message?: string }>('/v1/catalog/sync')
      await refreshCatalog()
      if (!st.ok) toast('目录没能更新', [st.code, st.message].filter(Boolean).join(' '), 'error')
      else if (st.skipped) toast('没有更新目录', st.message || '用的是程序自带的目录')
      else toast(st.updated ? '目录已更新' : '目录已是最新')
    } catch (e) {
      toast('目录没能更新', (e as Error).message, 'error')
    } finally {
      setSyncing(false)
    }
  }

  if (catalogError) {
    return (
      <div>
        <PageHead title="应用中心" />
        <Empty>{catalogError.message}</Empty>
      </div>
    )
  }
  if (!catalog) {
    return (
      <div>
        <PageHead title="应用中心" />
        <div className="flex items-center gap-2 text-xs text-muted-foreground"><Spinner />正在读取目录…</div>
      </div>
    )
  }

  return (
    <div>
      <PageHead title="应用中心" meta={`${apps.length} 个应用`} actionsClassName="w-full flex-nowrap sm:w-auto">
        <div className="relative min-w-0 flex-1 sm:w-56 sm:flex-none">
          <Search className="absolute left-2 top-2 size-3.5 text-muted-foreground" />
          <Input className="pl-7" placeholder="搜索应用" aria-label="搜索应用" value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <Button variant="ghost" size="icon-sm" className="shrink-0" onClick={sync} disabled={syncing} aria-label="更新目录" title="更新目录">
          {syncing ? <Spinner /> : <RefreshCw className="size-3.5" />}
        </Button>
        {PRIVATE_CATALOG_UI && (
          <Button variant="outline" size="sm" className="shrink-0" onClick={() => setConnect(true)} title="输入连接码，连上别人给你准备的私有目录">
            <Link2 className="size-3.5" /><span className="hidden sm:inline">连接私有目录</span>
          </Button>
        )}
      </PageHead>
      {gone.map((g) => (
        <div key={g.id} className="mb-3">
          <Alert tone="warning" icon={<Unlink />} title={`私有目录“${g.name}”已断开`}>
            从它装的应用照常运行，但不能再从它安装或升级；要恢复，向给你连接码的人要一个新码{g.message ? `（${g.message}）` : ''}
          </Alert>
        </div>
      ))}
      {PRIVATE_CATALOG_UI && srcTabs.length > 0 && <Tabs value={source} onChange={setSrc} items={srcTabs} className="mb-3 mr-2" label="来源" />}
      {recipes.length > 0 && (
        <section aria-label="配方" className="mb-5 grid gap-3 md:grid-cols-3">
          {recipes.map((r) => <RecipeCard key={sourceId(r) + '/' + r.id} r={r} />)}
        </section>
      )}
      {tabs.length > 1 && <Tabs value={group} onChange={setGroup} items={tabs} className="mb-3" label="分类" />}
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        {list.map((a) => <AppCard key={appKey(a)} a={a} />)}
      </div>
      {list.length === 0 && (
        <Empty>没找到{q ? `“${q}”` : ''}</Empty>
      )}
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <ExpertButton variant="ghost" />
        <span className="ml-auto text-2xs text-muted-foreground">目录第 {catalog.serial} 版{catalog.origin === 'snapshot' ? ' · 程序自带' : ''}</span>
      </div>
      {PRIVATE_CATALOG_UI && <ConnectCatalogDialog open={connect} onClose={() => setConnect(false)} />}
    </div>
  )
}
