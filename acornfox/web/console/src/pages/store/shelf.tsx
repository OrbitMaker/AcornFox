// 应用中心的“货架”零件（样稿第 4 版，首页和详情页共用）：
// 图标、封面、商品卡、一排货架、解决方案卡，以及一个应用的按钮和这台机器上的服务有没有。
// 展示内容（主色、一句话卖点、角标、封面图、介绍文档）读应用的 show 字段；清单格式里还没有这些字段，没有时退回简介。
//
// 样稿第 10 版（评估 X6、I1、I2、J1、K1、J2、L2）：
// - 按钮全站一个说法：装之前“安装”，解决方案“一起装好”；装好以后标“✓ 已装好”，主按钮“打开”（直接开应用现在的地址），
//   旁边小字“去管理”；没法直接打开的（只这台机器能开、数据库这类、停了）主按钮就是“去管理”（OpenButtons）。
// - 封面：有视频才画播放键；没视频、没封面图的，封面是应用的图标加主色（Cover）。
// - 缺服务：应用要用到的服务这台机器上没有时，找一个包含它、又能补上那个服务的解决方案（solutionFor），
//   应用页大按钮变成“和 MySQL 一起装好”，安装弹窗也引到那里。平台不提示“先装某某”，一起装靠解决方案。
import { useMemo, useRef, useState } from 'react'
import { Ban, Check, ChevronLeft, ChevronRight, Play } from 'lucide-react'
import { cn, safeHref } from '@/lib/utils'
import { GROUPS, PUBLIC, appMemory, appsIn, formerNote, freeMB, groupOf, industryNames, memoryCheck, recipeMemory, sourceId } from '@/lib/catalog'
import { usePoll } from '@/lib/hooks'
import { docHasVideo } from '@/lib/doc'
import { listKinds, listServices } from '@/lib/services'
import { needsPlatform, needsPlatformText } from '@/lib/upgrade'
import { deriveStatus, openableAddress, type AppStatus } from '@/lib/appstatus'
import { WORDS, canOpenNow, plainStatus } from '@/lib/display'
import { appHref, go, itemHref } from '@/lib/router'
import type { AppShow, AppView, CatalogApp, CatalogView, Recipe, ServiceView, TaskView } from '@/lib/types'
import { useConsole } from '@/state'
import { Tile, iconSrc } from '@/components/bits'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Spinner } from '@/components/ui/controls'
import { UpgradeHint } from './upgrade-note'

// runningTaskFor：按“目录来源 + id”找进行中的安装任务。公共目录和私有目录可能有同 id 的应用，
// 只看 id 会把公共版的进度显示到私有那张卡片上。没带 catalog 的旧任务算公共目录。
export function runningTaskFor(tasks: TaskView[], kind: 'app' | 'recipe', id: string, catalog: string = PUBLIC): TaskView | undefined {
  return tasks.find((t) => t.status === 'running' && t.kind === kind && t.target === id && (t.catalog || PUBLIC) === catalog)
}

// FormerHint：卡片上的一行，旧格式的清单装不了时服务端给的那句话（原样）。
export function FormerHint({ note, className }: { note: string; className?: string }) {
  return (
    <div className={cn('flex min-w-0 items-start gap-1.5 text-2xs text-muted-foreground', className)} data-testid="former-note">
      <Ban className="mt-px size-3 shrink-0" /><span className="min-w-0 break-words">{note}</span>
    </div>
  )
}

// SourceBadge：私有目录的应用和配方标“来自：目录名”。
export function SourceBadge({ x }: { x: { source?: { id: string; name: string } } }) {
  if (sourceId(x) === PUBLIC) return null
  return <Badge variant="outline" className="max-w-[10rem] truncate font-normal" title={x.source?.id}>来自：{x.source?.name || x.source?.id}</Badge>
}

export function groupName(category: string): string {
  return GROUPS.find(([k]) => k === groupOf(category))?.[1] || '其他'
}

export function clock(seconds: number): string {
  const s = Math.max(0, Math.round(seconds))
  return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0')
}

// fitText：这台机器装不装得下，说成用户的话；具体数字（要多少、还剩多少）只放在悬停提示里。
// 应用中心是给不懂技术的用户看的（“一定不要有技术词汇”），卡片和安装栏上不写内存、版本这些词。
export function fitText(tone: string): string {
  return tone === 'ok' ? '这台机器装得下' : tone === 'warn' ? '这台机器有点紧，也能装' : '这台机器装不下了'
}

// playIntent：从卡片的封面点进详情页时，记下是哪个应用，详情页打开就播介绍文档里的第一段视频。
export const playIntent = { id: '' }

// ---- 这台机器上的服务：中文名、有没有 ----

export interface ServiceInfo { label: (kind: string) => string; have: (kind: string) => boolean | null }

export function useServiceInfo(): ServiceInfo {
  const kinds = usePoll((s) => listKinds(s).catch(() => null), 0)
  const svcs = usePoll((s) => listServices(s).catch(() => null), 15000)
  return useMemo(() => {
    const names = new Map((kinds.data || []).map((k) => [k.kind, k.label]))
    const list: ServiceView[] | null = svcs.data ? svcs.data.services : null
    return {
      label: (k: string) => names.get(k) || k,
      // null：读不到服务列表（旧服务端），不说有没有。
      have: (k: string) => (list ? list.some((s) => s.kind === k && s.status !== 'provider_gone') : null),
    }
  }, [kinds.data, svcs.data])
}

// ---- 装好的应用：打开、去管理 ----

// useOpenTarget：装好的一个应用（name 是它在这台机器上的代号）现在能不能直接打开、打开哪个地址、去哪里管理。
// 能打开：在运行（或新版本没换上、旧版本照常用），而且有用户在自己电脑上点得开的地址。
export function useOpenTarget(name: string | undefined) {
  const { apps, installing } = useConsole()
  const view: AppView | undefined = name ? (apps || []).find((x) => x.name === name) : undefined
  const status: AppStatus | null = view ? deriveStatus(view, installing) : null
  const url = view && status && canOpenNow(view, status) ? safeHref(openableAddress(view)) || '' : ''
  return { name: name || '', view, status, url, manage: name ? appHref(name) : '#apps' }
}

export type OpenTarget = ReturnType<typeof useOpenTarget>

// OpenButtons：装好以后的按钮。能打开时主按钮“打开”（新窗口开应用的地址），旁边小字“去管理”；
// 打不开时主按钮“去管理”，数据库这类在下面带一句“给别的应用用，不用打开”。
// layout：row 是卡片上一行（小按钮）；block 是应用页、解决方案页右边那一栏（整宽按钮，“去管理”在下面一行）。
export function OpenButtons({ t, label = '打开', layout = 'row', className }: { t: OpenTarget; label?: string; layout?: 'row' | 'block'; className?: string }) {
  const block = layout === 'block'
  const stop = (e: React.MouseEvent) => e.stopPropagation()
  if (t.url) {
    const open = (
      <a href={t.url} target="_blank" rel="noopener noreferrer" onClick={stop} className={cn(buttonVariants({ size: block ? 'lg' : 'sm', variant: block ? 'default' : 'outline' }), block && 'w-full')} data-testid="open-app">{label}</a>
    )
    const manage = <a href={t.manage} onClick={stop} className={cn('shrink-0 text-muted-foreground underline-offset-4 hover:text-foreground hover:underline', block ? 'text-xs' : 'text-2xs')}>去管理</a>
    return block
      ? <div className={cn('flex flex-col items-center gap-2', className)}>{open}{manage}</div>
      : <span className={cn('flex shrink-0 items-center gap-2', className)}>{manage}{open}</span>
  }
  const manage = <a href={t.manage} onClick={stop} className={cn(buttonVariants({ size: block ? 'lg' : 'sm', variant: block ? 'default' : 'outline' }), block && 'w-full')}>去管理</a>
  if (!block) return <span className={cn('shrink-0', className)}>{manage}</span>
  return (
    <div className={cn('flex flex-col items-center gap-2', className)}>
      {manage}
      {t.view?.internal_only && <span className="text-2xs text-muted-foreground">{WORDS.internalApp}</span>}
    </div>
  )
}

// Installed：“✓ 已装好”。
export function Installed({ className }: { className?: string }) {
  return <span className={cn('inline-flex shrink-0 items-center gap-0.5 text-2xs font-medium text-success', className)}><Check className="size-3" />{WORDS.installed}</span>
}

// NotNormal：装好的应用不正常时的一句（正常时不显示，免得和“已装好”说两遍）。
export function NotNormal({ t, className }: { t: OpenTarget; className?: string }) {
  if (!t.view || !t.status || t.status === 'running') return null
  const p = plainStatus(t.view, t.status)
  return <span className={cn('truncate text-2xs', p.tone === 'warning' ? 'text-warning' : 'text-muted-foreground', className)}>{p.text}{p.note ? '，' + p.note : ''}</span>
}

// ---- 一个应用的按钮 ----

export function useAppAction(a: CatalogApp, size: 'sm' | 'lg' = 'sm') {
  // 卡片上（sm）一屏有二十多个按钮，用白键；主按钮只留给应用页右栏（lg）。
  const variant = size === 'lg' ? 'default' : 'outline'
  const { tasks, install } = useConsole()
  const src = sourceId(a)
  const running = runningTaskFor(tasks, 'app', a.id, src)
  const installed = a.installed.length > 0
  const t = useOpenTarget(installed ? a.installed[0] : undefined)
  // 装过的、正在装的、这台机器用不了的，各有各的说法，不再提升级。
  const needs = !running && !installed && a.supported ? needsPlatform(a) : ''
  const former = !running && !installed ? formerNote(a) : ''
  let button
  if (running) button = <Button size={size} variant="secondary" onClick={(e) => { e.stopPropagation(); install({ kind: 'app', id: a.id, taskId: running.id, catalog: src }) }}><Spinner />安装中</Button>
  else if (installed) button = <OpenButtons t={t} />
  else if (former) button = <Button size={size} variant={variant} disabled title={former}>安装</Button>
  else if (!a.supported) button = <span className="text-2xs text-muted-foreground" title={'只支持 ' + a.platforms.join('、')}>这台机器用不了</span>
  else if (needs) button = <Button size={size} variant={variant} disabled title={needsPlatformText(needs)}>安装</Button>
  else button = <Button size={size} variant={variant} onClick={(e) => { e.stopPropagation(); install({ kind: 'app', id: a.id, catalog: src }) }}>安装</Button>
  return { button, needs, former, open: t, installed, running: !!running }
}

// ---- 图标、封面 ----

interface Iconed { id: string; name: string; icon?: string; icon_type?: string }

// StoreIcon：目录图标本身就是带底色的圆角方块，整块显示；没有图标时退回名称缩写的图块。
export function StoreIcon({ a, size = 28 }: { a: Iconed; size?: number }) {
  const src = iconSrc(a.icon, a.icon_type)
  if (!src) return <Tile id={a.id} name={a.name} size={size >= 40 ? 'lg' : size >= 30 ? 'md' : 'sm'} />
  return <img alt="" src={src} className="store-icon shrink-0" style={{ width: size, height: size }} />
}

interface Shown { id: string; show?: AppShow }

// Cover：卡片封面。有封面图（show.cover）用它，没有时是图标（icon）放在主色上；介绍里有视频才画播放键（评估 I1）。
export function Cover({ a, icon, className, play = 'md' }: { a: Shown; icon?: React.ReactNode; className?: string; play?: 'sm' | 'md' | 'lg' | 'none' }) {
  const video = docHasVideo(a.show?.doc)
  const cover = safeHref(a.show?.cover)
  // 封面图加载不出来（链接失效、浏览器的安全策略不让加载）时退回图标，不留一个破图
  const [broken, setBroken] = useState(false)
  return (
    <div className={cn('store-cover relative', className)} style={{ '--c': a.show?.color || '#64748b' } as React.CSSProperties}>
      {cover && !broken ? <img alt="" src={cover} className="absolute inset-0 size-full object-cover" onError={() => setBroken(true)} />
        : icon ? <span className="absolute inset-0 flex items-center justify-center">{icon}</span> : null}
      {video && play !== 'none' && (
        <span className={cn('store-play absolute left-1/2 top-1/2 flex -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full', play === 'lg' ? 'size-16' : play === 'md' ? 'size-10' : 'size-6')}>
          <Play className={cn('translate-x-[1px] fill-current', play === 'lg' ? 'size-6' : play === 'md' ? 'size-4' : 'size-2.5')} />
        </span>
      )}
    </div>
  )
}

const FLAG: Record<string, [string, string]> = { hot: ['热门', 'store-flag-hot'], new: ['新上架', 'store-flag-new'], pick: ['推荐', 'store-flag-pick'] }

export function Flag({ flag, className }: { flag?: string; className?: string }) {
  const f = flag ? FLAG[flag] : undefined
  if (!f) return null
  return <span className={cn('rounded px-1.5 py-0.5 text-2xs font-medium', f[1], className)}>{f[0]}</span>
}

// ---- 商品卡：上面封面，下面图标、名字、一句话、几分钟装好（装不下时改成提醒）、按钮 ----
// 装好的：封面右上“✓ 已装好”，按钮“打开”和小字“去管理”，点卡片别处也去管理（评估 I2）；不正常时左下一句状态。

export function ProductCard({ a, free, className }: { a: CatalogApp; free: number | null; className?: string }) {
  const act = useAppAction(a)
  const item = itemHref(a.id, sourceId(a))
  const m = appMemory(a)
  const check = memoryCheck(m.need, m.min, free)
  const minutes = a.delivery?.minutes
  const hasVideo = docHasVideo(a.show?.doc)
  const open = (play?: boolean) => {
    if (play && hasVideo) { playIntent.id = a.id; go(item); return }
    go(act.installed ? act.open.manage : item)
  }
  return (
    <div
      role="link"
      tabIndex={0}
      onClick={() => open()}
      onKeyDown={(e) => { if (e.key === 'Enter') open() }}
      className={cn('store-card flex min-w-0 cursor-pointer flex-col overflow-hidden rounded-lg border bg-card focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', className)}
      data-testid="catalog-app"
    >
      <div className="relative" onClick={(e) => { e.stopPropagation(); open(true) }} title={hasVideo ? '看演示视频' : undefined}>
        <Cover a={a} icon={<StoreIcon a={a} size={48} />} className="aspect-[16/10]" />
        <Flag flag={a.show?.flag} className="absolute left-2 top-2" />
        {act.installed && <span className="store-have absolute right-2 top-2 inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-2xs font-medium"><Check className="size-3" />{WORDS.installed}</span>}
      </div>
      <div className="flex flex-1 flex-col gap-1.5 p-3">
        <div className="flex min-w-0 items-center gap-2">
          <StoreIcon a={a} size={28} />
          <span className="min-w-0 flex-1 truncate text-sm font-medium">{a.name}</span>
          <SourceBadge x={a} />
        </div>
        <p className="line-clamp-2 min-h-[calc(var(--lh-xs)*2)] text-xs text-muted-foreground">{a.show?.tagline || a.summary}</p>
        {act.needs && <UpgradeHint version={act.needs} />}
        {act.former && <FormerHint note={act.former} />}
        <div className="mt-auto flex items-center gap-2 pt-1">
          <span className="min-w-0 flex-1 truncate" title={act.installed ? undefined : check.text}>
            {act.installed ? <NotNormal t={act.open} />
              : act.running ? null
              : check.tone === 'low' ? <span className="text-2xs text-warning">{fitText('low')}</span>
              : <span className="tnum text-2xs text-muted-foreground">{minutes ? `约 ${minutes} 分钟装好` : ''}</span>}
          </span>
          {act.button}
        </div>
      </div>
    </div>
  )
}

// ---- 一排货架：标题、看全部、左右翻；内容横着排，放不下就横向滑 ----

export function Shelf({ title, sub, icon, count, onAll, children }: { title: string; sub?: string; icon?: React.ReactNode; count?: number; onAll?: () => void; children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const move = (dir: number) => { const el = ref.current; if (el) el.scrollBy({ left: dir * el.clientWidth * 0.8, behavior: 'smooth' }) }
  return (
    <section className="mt-8" aria-label={title}>
      <div className="mb-2.5 flex items-center gap-2">
        {icon && <span className="store-aisle flex size-7 shrink-0 items-center justify-center rounded-lg [&_svg]:size-4">{icon}</span>}
        <h3 className="shrink-0 text-sm font-medium">{title}</h3>
        {sub && <span className="hidden min-w-0 truncate text-xs text-muted-foreground sm:inline">{sub}</span>}
        <span className="ml-auto flex shrink-0 items-center gap-1">
          {onAll && (
            <button type="button" onClick={onAll} className="inline-flex items-center rounded-md px-1.5 py-1 text-xs text-muted-foreground hover:text-foreground">
              看全部{count ? ` ${count} 个` : ''}<ChevronRight className="size-icon" />
            </button>
          )}
          {(count || 0) > 4 && (
            <>
              <Button variant="outline" size="icon-sm" className="hidden md:inline-flex" aria-label={title + '：往前'} onClick={() => move(-1)}><ChevronLeft className="size-icon" /></Button>
              <Button variant="outline" size="icon-sm" className="hidden md:inline-flex" aria-label={title + '：往后'} onClick={() => move(1)}><ChevronRight className="size-icon" /></Button>
            </>
          )}
        </span>
      </div>
      <div ref={ref} className="store-row -mx-1 flex gap-3 overflow-x-auto px-1 pb-3 pt-0.5">{children}</div>
    </section>
  )
}

// ---- 解决方案（清单和命令里叫配方 recipe；给用户看一律叫“解决方案”： 定）----
// 应用是应用，解决方案是解决方案：应用是单个安装；解决方案是把几个应用串起来，
// 或者在一个应用上加插件、改设置。名字按用户的问题起（“让 AI 同事进飞书”）。

// isSolution：这个配方算不算解决方案——几个应用串起来的算。
// 只装一个应用、什么都没加的配方，和直接装那个应用是一回事，不当解决方案摆出来。
// （“给一个应用加插件、改设置”的配方，现在的配方格式还写不出来，格式支持以后在这里加一条。）
export function isSolution(r: Recipe): boolean {
  return r.apps.length > 1
}

// solutionParts：卡片上的一句“里面有什么”：几个应用。
export function solutionParts(r: Recipe): string {
  return `${r.apps.length} 个应用`
}

export function solutionHref(id: string, catalog?: string): string {
  return '#solution-' + encodeURIComponent(id) + (catalog && catalog !== 'public' ? '?catalog=' + encodeURIComponent(catalog) : '')
}

// useSolution：一个解决方案在这台机器上的情况——里面有哪些应用、装不装得下、是不是在装、是不是都装好了，
// 都装好以后“打开”打开哪一个（main：里面能直接打开的那个；都打不开时是最后一个不是“给别的应用用”的）。
export function useSolution(r: Recipe) {
  const { catalog, resources, tasks, install, apps: mine } = useConsole()
  const apps = appsIn(catalog, sourceId(r))
  const mem = recipeMemory(r, apps)
  const check = memoryCheck(mem.need, mem.min, freeMB(resources))
  const running = runningTaskFor(tasks, 'recipe', r.id, sourceId(r))
  const needs = running ? '' : needsPlatform(r)
  const former = running || needs ? '' : formerNote(r)
  const members = r.apps.map((ra) => ({ id: ra.app, app: apps.find((x) => x.id === ra.app) }))
  // 里面的应用都已经装了，就不用再装。
  const done = mem.need === 0
  const views = members
    .map((m) => (m.app?.installed[0] ? (mine || []).find((x) => x.name === m.app!.installed[0]) : undefined))
    .filter((x): x is AppView => !!x)
  const visible = views.filter((v) => !v.internal_only)
  const main = [...visible].reverse().find((v) => !!openableAddress(v)) || visible[visible.length - 1] || views[views.length - 1]
  // mainName：按钮上写“打开 n8n”用的名字（应用中心里的名字）。
  const mainName = main ? members.find((m) => m.app?.installed[0] === main.name)?.app?.name || main.name : ''
  const start = () => install({ kind: 'recipe', id: r.id, taskId: running?.id, catalog: sourceId(r) })
  return { members, check, running, needs, former, done, start, main: main?.name, mainName }
}

export function SolutionButton({ s, size = 'sm', className }: { s: ReturnType<typeof useSolution>; size?: 'sm' | 'lg'; className?: string }) {
  if (s.done) return <Installed className={className} />
  return (
    <Button size={size} variant={size === 'lg' ? 'default' : 'outline'} className={className} disabled={!!s.needs || !!s.former} title={s.needs ? needsPlatformText(s.needs) : s.former || undefined} onClick={(e) => { e.stopPropagation(); s.start() }}>
      {s.running ? <><Spinner />安装中</> : '一起装好'}
    </Button>
  )
}

// SolutionIcons：里面几个应用的图标，叠着排。
export function SolutionIcons({ s, size = 24 }: { s: ReturnType<typeof useSolution>; size?: number }) {
  return (
    <span className="flex shrink-0 items-center">
      {s.members.map((m, i) => (
        <span key={m.id} title={m.app?.name || m.id} style={{ marginLeft: i ? -size * 0.22 : 0 }} className="rounded-[26%] ring-2 ring-card">
          <StoreIcon a={{ id: m.id, name: m.app?.name || m.id, icon: m.app?.icon, icon_type: m.app?.icon_type }} size={size} />
        </span>
      ))}
    </span>
  )
}

// SolutionCard：lg 是首页和“全部解决方案”里的大卡（封面、行业、按问题起的名字、一句话、里面的应用、一起装好）；
// sm 是一行（应用页“用到它的解决方案”里用）。点卡片进解决方案页，点封面进去就播。
// 都装好了：封面右上“✓ 已装好”，按钮“打开”（打开里面能直接打开的那个应用），小字“去管理”。
export function SolutionCard({ r, size = 'lg', className }: { r: Recipe; size?: 'lg' | 'sm'; className?: string }) {
  const s = useSolution(r)
  const t = useOpenTarget(s.done ? s.main : undefined)
  const href = solutionHref(r.id, sourceId(r))
  const hasVideo = docHasVideo(r.show?.doc)
  const { catalog } = useConsole()
  const industries = industryNames(catalog, r)
  const open = (play?: boolean) => { if (play && hasVideo) playIntent.id = 'solution:' + r.id; go(href) }
  const minutes = r.delivery.minutes
  const meta = s.done ? solutionParts(r) : s.check.tone === 'low' ? fitText('low') : `${solutionParts(r)}${minutes ? ` · 约 ${minutes} 分钟装好` : ''}`
  const action = s.done ? <OpenButtons t={t} /> : <SolutionButton s={s} />
  if (size === 'sm') {
    return (
      <div role="link" tabIndex={0} onClick={() => open()} onKeyDown={(e) => { if (e.key === 'Enter') open() }}
        className={cn('store-card flex min-w-0 cursor-pointer items-center gap-3 rounded-lg border bg-card p-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', className)} data-testid="catalog-recipe">
        <SolutionIcons s={s} size={28} />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{r.title}</div>
          <div className={cn('tnum truncate text-2xs', !s.done && s.check.tone === 'low' ? 'text-warning' : 'text-muted-foreground')}>{meta}</div>
        </div>
        {action}
      </div>
    )
  }
  return (
    <div role="link" tabIndex={0} onClick={() => open()} onKeyDown={(e) => { if (e.key === 'Enter') open() }}
      className={cn('store-card flex min-w-0 cursor-pointer flex-col overflow-hidden rounded-lg border bg-card focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', className)} data-testid="catalog-recipe">
      <div className="relative" onClick={(e) => { e.stopPropagation(); open(true) }} title={hasVideo ? '看演示视频' : undefined}>
        <Cover a={{ id: r.id, show: r.show }} icon={<SolutionIcons s={s} size={44} />} className="aspect-[16/9]" />
        <Flag flag={r.show?.flag} className="absolute left-2.5 top-2.5" />
        {s.done && <span className="store-have absolute right-2.5 top-2.5 inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-2xs font-medium"><Check className="size-3" />{WORDS.installed}</span>}
        {industries.length > 0 && (
          <span className="absolute bottom-2.5 left-2.5 flex flex-wrap gap-1" data-testid="solution-industries">
            {industries.map((x) => <span key={x} className="store-time rounded px-1.5 py-0.5 text-2xs font-medium">{x}</span>)}
          </span>
        )}
      </div>
      <div className="flex flex-1 flex-col gap-1.5 p-3.5">
        <div className="flex min-w-0 items-center gap-2"><span className="min-w-0 flex-1 text-sm font-semibold leading-snug">{r.title}</span><SourceBadge x={r} /></div>
        <p className="line-clamp-2 min-h-[calc(var(--lh-xs)*2)] text-xs text-muted-foreground">{r.show?.tagline || r.summary}</p>
        {s.needs && <UpgradeHint version={s.needs} />}
        {s.former && <FormerHint note={s.former} />}
        <div className="mt-auto flex items-center gap-2.5 pt-2">
          <SolutionIcons s={s} />
          <span className={cn('tnum min-w-0 flex-1 truncate text-2xs', !s.done && s.check.tone === 'low' ? 'text-warning' : 'text-muted-foreground')} title={s.done ? undefined : s.check.text}>{meta}</span>
          {action}
        </div>
      </div>
    </div>
  )
}

// ---- 缺服务时用哪个解决方案一起装（样稿第 10 版，评估 L2、J2、R8）----
// solutionFor：包含这个应用、里面又有别的应用能提供 kinds 这几种服务的解决方案；几个都行时，挑应用最少的那个。
// provider：那个解决方案里提供服务的应用（按钮上写“和 MySQL 一起装好”用它的名字）。没有时返回 null。
// 只按目录里的数据算（应用的 provides、配方的 apps），不为哪种服务、哪个应用写专门的规则。
export function solutionFor(cat: CatalogView | null | undefined, appId: string, kinds: string[], src: string = PUBLIC): { r: Recipe; provider?: CatalogApp } | null {
  if (kinds.length === 0) return null
  const apps = appsIn(cat, src)
  const provides = (id: string, k: string) => !!apps.find((a) => a.id === id)?.provides?.includes(k)
  const fits = (cat?.recipes || [])
    .filter((r) => sourceId(r) === src && isSolution(r) && !r.former && r.apps.some((x) => x.app === appId))
    .filter((r) => kinds.every((k) => r.apps.some((x) => x.app !== appId && provides(x.app, k))))
    .sort((x, y) => x.apps.length - y.apps.length)
  const r = fits[0]
  if (!r) return null
  const pid = r.apps.find((x) => x.app !== appId && kinds.some((k) => provides(x.app, k)))?.app
  return { r, provider: apps.find((a) => a.id === pid) }
}
