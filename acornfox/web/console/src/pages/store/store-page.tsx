// 应用中心首页（样稿第 6 版： 从三个方向里选的“B 一页到底”）。
// 应用是单个安装；解决方案是几个应用串起来，或一个应用加了插件、改了设置（只装一个应用的配方不算，见 shelf.tsx 的 isSolution）。
// 从上到下只有两块：“你想解决什么问题？”（几个解决方案的大卡，其余进“全部解决方案”页）→ “全部应用”
// （货架导航加一整面商品卡）。热门、新上架、推荐只是卡片上的角标（由经营系统标）。
// 标题旁边不放解释性的话（“不需要这种说明性的文字”），最多一个数量。
// 给不懂技术的用户看：页面上不写内存、版本、目录这些词，只说它能解决什么问题、几分钟装好、这台机器装不装得下。
//
// 样稿第 10 版（评估 X5、I3、I4、I5、X1）：
// - 页面里不再有自己的大搜索框，搜索用顶栏那一个：顶栏搜应用时跳到 #store?q=…，这一页读地址里的 q 来过滤；
//   货架那一排右边留一个小的筛选框（和“我的应用”的筛选框一样大），改它时只改地址里的 q，不新增浏览记录。
// - “全部”里不摆基础服务（数据库、缓存这类只给别的应用用的），只在“基础服务”那一格和搜索结果里看得到。
// - 搜不到：一句话加“看全部应用”按钮（EmptyState）。
// - 这台机器一个应用都没装时，“你想解决什么问题？”上面加一句“第一次用？从这三个里挑一个”。
// - 标题和卡片跟控制台别的页面同一档字号、同一种卡片。
// 数据：GET /v1/catalog（应用和解决方案，清单里解决方案叫配方 recipes），资源、我的应用用共享数据。零件在 shelf.tsx。
// 要更新版本的 AcornFox 才能装的（needs_platform）、旧格式清单（former）、这台机器用不了的（supported 为 false）照旧：按钮不可点、写原因。
// 私有目录先不开放：“连接私有目录”按钮和来源标签由 PRIVATE_CATALOG_UI 控制，默认藏起。
import { useEffect, useMemo, useState } from 'react'
import { Activity, Bot, ChevronRight, Clapperboard, Code2, Database, FolderOpen, Globe, LayoutGrid, Link2, Package, RefreshCw, Search, Unlink, Users, Workflow } from 'lucide-react'
import { cn } from '@/lib/utils'
import { api } from '@/lib/api'
import { GROUPS, appKey, disconnectedSources, filterApps, filterSource, freeMB, groupOf, isBasicService, matchRecipe, orderSolutions, sortByShowOrder, sourceId, sourceTabs, storeEmpty } from '@/lib/catalog'
import { PRIVATE_CATALOG_UI } from '@/lib/features'
import { useConsole } from '@/state'
import { Empty, ErrorLine, PageHead } from '@/components/bits'
import { EmptyState } from '@/components/fold'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, Tabs } from '@/components/ui/controls'
import { SkZone, useSkeleton } from '@/components/ui/skeleton'
import { SkStore } from '@/components/skeletons'
import { useToast } from '@/components/ui/toast'
import { ExpertButton } from './shared'
import { ConnectCatalogDialog } from './connect-dialog'
import { ProductCard, SolutionCard, isSolution } from './shelf'

export { FormerHint, SourceBadge, runningTaskFor } from './shelf'

// 货架的牌子：每个分类一个图标、一句“这排卖什么”。
export const AISLE: Record<string, [React.ReactNode, string]> = {
  all: [<LayoutGrid key="i" />, ''],
  ai: [<Bot key="i" />, '聊天助手、AI 工具'],
  auto: [<Workflow key="i" />, '重复的活交给它'],
  site: [<Globe key="i" />, '博客、官网、访问统计'],
  media: [<Clapperboard key="i" />, '电影、剧集、照片'],
  team: [<Users key="i" />, '聊天、文档'],
  files: [<FolderOpen key="i" />, '网盘、同步、密码'],
  ops: [<Activity key="i" />, '看数字、盯网站'],
  dev: [<Code2 key="i" />, '存代码、画图'],
  db: [<Database key="i" />, '别的应用要用到的服务，一般跟着解决方案一起装'],
  other: [<Package key="i" />, ''],
}

// 首页最多摆几个解决方案；多出来的进“全部解决方案”页。
const TOP_SOLUTIONS = 3

// hashQuery：地址里 #store?q=… 的 q（顶栏搜索跳过来时带着）。
function hashQuery(): string {
  return new URLSearchParams((location.hash.split('?')[1]) || '').get('q') || ''
}

export function StorePage() {
  const { catalog, catalogError, refreshCatalog, resources, apps: mine } = useConsole()
  const toast = useToast()
  // 等目录时按真版面画占位形状（样稿第 12 版）
  const sk = useSkeleton(!catalog && !catalogError)
  const [q, setQState] = useState(hashQuery)
  const [group, setGroup] = useState('all')
  const [syncing, setSyncing] = useState(false)
  // 没能看到新上架的：出错的话留在“全部应用”那一排下面（样稿第 14 版，不弹提示）
  const [syncErr, setSyncErr] = useState<Error | null>(null)
  const [src, setSrc] = useState('all')
  const [connect, setConnect] = useState(false)
  // 顶栏搜索再跳过来（地址里的 q 变了）时跟着变。
  useEffect(() => {
    const on = () => setQState(hashQuery())
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  const setQ = (v: string) => {
    setQState(v)
    try { history.replaceState(null, '', '#store' + (v.trim() ? '?q=' + encodeURIComponent(v.trim()) : '')) } catch { /* 地址改不了不影响过滤 */ }
  }
  const srcTabs = useMemo(() => sourceTabs(catalog), [catalog])
  const source = srcTabs.some(([k]) => k === src) ? src : 'all'
  const apps = useMemo(() => filterSource(catalog?.apps || [], source), [catalog, source])
  const counts = useMemo(() => {
    const c = new Map<string, number>()
    for (const a of apps) c.set(groupOf(a.category), (c.get(groupOf(a.category)) || 0) + 1)
    return c
  }, [apps])
  const aisles = GROUPS.filter(([k]) => counts.has(k))
  // “全部”里不摆基础服务（评估 I4）。
  const shelfApps = apps.filter((a) => !isBasicService(a))
  const groups: [string, string, number][] = [['all', '全部', shelfApps.length], ...aisles.map(([k, l]) => [k, l, counts.get(k) || 0] as [string, string, number])]
  const searching = !!q.trim()
  // 不搜索、看“全部”时按货架的顺序排（不含基础服务）；选了货架或在搜索时照目录的顺序（搜索时基础服务也找）。
  const byAisle = aisles.flatMap(([k]) => sortByShowOrder(shelfApps.filter((a) => groupOf(a.category) === k)))
  const list = !searching && group === 'all' ? byAisle : filterApps(apps, group, q)
  const solutions = orderSolutions(filterSource(catalog?.recipes || [], source).filter(isSolution), catalog?.home?.solutions)
  const shownSolutions = searching ? solutions.filter((r) => matchRecipe(r, q)) : solutions.slice(0, TOP_SOLUTIONS)
  const empty = storeEmpty(apps.length, list.length, q)
  const gone = disconnectedSources(catalog)
  const free = freeMB(resources)
  // 第一次来：这台机器上一个应用都还没装（评估 I5）。
  const first = !!mine && mine.length === 0 && !searching

  const sync = async () => {
    setSyncing(true)
    setSyncErr(null)
    try {
      const st = await api.post<{ ok: boolean; skipped?: boolean; updated?: boolean; code?: string; message?: string }>('/v1/catalog/sync')
      await refreshCatalog()
      if (!st.ok) setSyncErr(new Error('没能看到新上架的' + (st.message ? '：' + st.message : '')))
      else if (st.skipped) toast('没有新上架的', st.message || '')
      else toast(st.updated ? '有新上架的，已经摆上来了' : '没有新上架的')
    } catch (e) {
      setSyncErr(new Error('没能看到新上架的：' + (e as Error).message))
    } finally {
      setSyncing(false)
    }
  }

  if (catalogError) return <div><Empty>{catalogError.message}</Empty></div>
  if (sk.wait || !catalog) return <SkZone show={sk.show} className="block"><SkStore /></SkZone>

  return (
    <div className={cn(sk.reveal && 'sk-reveal')}>
      <PageHead>
        {PRIVATE_CATALOG_UI && (
          <Button variant="outline" size="sm" className="shrink-0" onClick={() => setConnect(true)} title="输入连接码，连上别人给你准备的私有目录">
            <Link2 className="size-icon" /><span className="hidden sm:inline">连接私有目录</span>
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

      {shownSolutions.length > 0 && (
        <section className="mb-8" aria-label="解决方案" data-testid="store-solutions">
          {first && <p className="mb-1 text-xs text-muted-foreground" data-testid="store-first">第一次用？从这三个里挑一个</p>}
          <div className="mb-3 flex flex-wrap items-end gap-x-3 gap-y-1">
            <h2 className="text-base font-semibold">{searching ? '找到的解决方案' : '你想解决什么问题？'}</h2>
            {!searching && solutions.length > TOP_SOLUTIONS && (
              <a href="#solutions" className="tap-h ml-auto inline-flex items-center pb-0.5 text-xs text-muted-foreground hover:text-foreground">全部解决方案 {solutions.length} 个<ChevronRight className="size-icon" /></a>
            )}
          </div>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {shownSolutions.map((r) => <SolutionCard key={sourceId(r) + '/' + r.id} r={r} />)}
          </div>
        </section>
      )}

      <section aria-label="全部应用">
        <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1">
          <h2 className="text-base font-semibold">{searching ? '找到的应用' : '全部应用'}</h2>
          {(searching || group !== 'all') && <span className="tnum pb-0.5 text-xs text-muted-foreground">{list.length} 个</span>}
          {/* 小的“找应用”和刷新放在标题这一行的右边，分类那一排整行留给分类（放不下横向滑）。 */}
          <div className="ml-auto flex items-center gap-1">
          <div className="relative w-28 shrink-0 sm:w-40">
              <Search className="pointer-events-none absolute left-2 top-1/2 size-icon -translate-y-1/2 text-muted-foreground" />
              <Input className="pl-7" placeholder="找应用" aria-label="找应用" value={q} onChange={(e) => setQ(e.target.value)} />
            </div>
            <Button variant="ghost" size="icon-sm" className="hidden shrink-0 sm:inline-flex" onClick={sync} loading={syncing} turn aria-label="看看有没有新上架的" title="看看有没有新上架的">
              <RefreshCw className="size-icon" />
            </Button>
          </div>
          <ErrorLine error={syncErr} className="basis-full text-right" />
        </div>
        <div className="mb-3 flex items-center gap-2">
          <div role="tablist" aria-label="货架" className="store-row -mx-1 flex min-w-0 flex-1 items-stretch gap-1.5 overflow-x-auto px-1 py-0.5 lg:flex-wrap lg:overflow-visible">
            {groups.map(([k, label, n]) => (
              <button
                key={k}
                type="button"
                role="tab"
                aria-selected={group === k}
                onClick={() => setGroup(k)}
                className={cn('store-aisle-tab inline-flex h-ctl shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-xs', group === k && 'on')}
              >
                <span className="[&_svg]:size-icon">{AISLE[k]?.[0]}</span>{label}<span className="tnum text-2xs opacity-60">{n}</span>
              </button>
            ))}
          </div>
        </div>
        {list.length > 0 && (
          <div className="grid gap-3 [grid-template-columns:repeat(auto-fill,minmax(13rem,1fr))]">
            {list.map((a) => <ProductCard key={appKey(a)} a={a} free={free} />)}
          </div>
        )}
        {empty && !(searching && shownSolutions.length > 0) && (
          <div data-testid="store-empty" data-searching={empty.searching ? 'true' : 'false'}>
            {empty.searching ? (
              <EmptyState
                title={q.trim() ? `没找到“${q.trim()}”。换个说法试试，或者` : '这一格里还没有应用。'}
                action={<Button size="sm" variant="outline" onClick={() => { setQ(''); setGroup('all') }}>看全部应用</Button>}
              />
            ) : (
              <EmptyState title={empty.title} />
            )}
          </div>
        )}
        {empty && searching && shownSolutions.length > 0 && <p className="text-xs text-muted-foreground">没有单个应用叫这个名字，看看上面的解决方案。</p>}
      </section>

      <div className="mt-6 flex flex-wrap items-center gap-2">
        <ExpertButton variant="ghost" />
      </div>
      {PRIVATE_CATALOG_UI && <ConnectCatalogDialog open={connect} onClose={() => setConnect(false)} />}
    </div>
  )
}
