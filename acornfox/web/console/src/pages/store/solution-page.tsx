// 解决方案的两个页面（样稿第 9 版）。清单和命令里叫配方（recipe），给用户看一律叫“解决方案”。
// SolutionsPage（#solutions）：全部解决方案，一张张大卡；最上面一排行业（“解决方案页要增加分类标签，是行业分类”），
// 点一个只看这个行业的。行业读配方的 show.industries（行业 id，一个解决方案可以挂几个行业），名字和顺序读目录顶层的 industries
// （在经营系统里定）；目录里没有行业这一项时不显示这一排。
// SolutionPage（#solution-<id>）：一个解决方案。左边只有一块：介绍文档（show.doc，一篇 Markdown；小标题固定
// “它能帮你做什么”“它帮你配好了什么”“装好以后”，见 lib/doc.ts）。“介绍性的东西一律内置为一篇文档的形式，不要搞这种多个层级”。
// 右边是一直跟着的安装栏（行业、名字、一句话、主按钮、几行由程序算出来的话、里面的应用——每个能点进应用页）；
// 文档下面是收起的“技术信息”（命令行）；最下面是别的解决方案（同行业的排前面）。
// 样稿第 10 版（评估 K1、K3、X6、X1）：没装是“一起装好”；都装好了是“打开 n8n”（打开里面能直接打开的那个应用），
// 下面小字“去管理”；右边卡片上不再写“解决方案”小标签（顶栏已经有），只留行业；标题和控制台别的页面同一档字号。
// 数据都来自 GET /v1/catalog 里的 recipes，不另外请求。页面上不写技术词。
import { useState } from 'react'
import { Check, ChevronRight, Layers, MemoryStick, Sparkles } from 'lucide-react'
import { cn } from '@/lib/utils'
import { PUBLIC, askInstallText, filterSource, industriesOf, industryNames, orderSolutions, sourceId } from '@/lib/catalog'
import { itemHref } from '@/lib/router'
import type { Recipe } from '@/lib/types'
import { useConsole } from '@/state'
import { Empty } from '@/components/bits'
import { AskAI, EmptyState } from '@/components/fold'
import { buttonVariants } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { SkZone, useSkeleton } from '@/components/ui/skeleton'
import { SkItem, SkStore } from '@/components/skeletons'
import { CopyLine } from '@/components/ui/copy'
import { ExpertButton } from './shared'
import { Doc } from './doc'
import { FormerHint, Installed, OpenButtons, Shelf, SolutionButton, SolutionCard, SourceBadge, StoreIcon, fitText, isSolution, playIntent, solutionParts, useOpenTarget, useSolution } from './shelf'
import { UpgradeNeeded } from './upgrade-note'

export function SolutionsPage() {
  const { catalog, catalogError } = useConsole()
  const [industry, setIndustry] = useState('')
  const sk = useSkeleton(!catalog && !catalogError)
  if (catalogError) return <Empty>{catalogError.message}</Empty>
  if (sk.wait || !catalog) return <SkZone show={sk.show} className="block"><SkStore /></SkZone>
  const all = orderSolutions((catalog.recipes || []).filter(isSolution), catalog.home?.solutions)
  const industries = industriesOf(catalog, all)
  const picked = industries.some((x) => x.id === industry) ? industry : ''
  const list = picked ? all.filter((r) => (r.show?.industries || []).includes(picked)) : all
  const tabs: [string, string, number][] = [['', '全部', all.length], ...industries.map((x) => [x.id, x.name, x.n] as [string, string, number])]
  return (
    <div>
      <div className="mb-3 flex items-end gap-3">
        <h1 className="text-base font-semibold">你想解决什么问题？</h1>
        <span className="tnum pb-0.5 text-xs text-muted-foreground">{list.length} 个</span>
      </div>
      {industries.length > 0 && (
        <div role="tablist" aria-label="行业" className="store-row -mx-1 mb-4 flex items-stretch gap-1.5 overflow-x-auto px-1 py-0.5" data-testid="solution-industry-tabs">
          {tabs.map(([k, label, n]) => (
            <button key={k || 'all'} type="button" role="tab" aria-selected={picked === k} onClick={() => setIndustry(k)}
              className={cn('store-aisle-tab inline-flex h-ctl shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-xs', picked === k && 'on')}>
              {label}<span className="tnum text-2xs opacity-60">{n}</span>
            </button>
          ))}
        </div>
      )}
      {list.length === 0 ? <EmptyState title="还没有解决方案。" action={<a href="#store" className={buttonVariants({ size: 'sm', variant: 'outline' })}>回应用中心</a>} /> : (
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {list.map((r) => <SolutionCard key={sourceId(r) + '/' + r.id} r={r} />)}
        </div>
      )}
    </div>
  )
}

function Fact({ icon, tone, children }: { icon: React.ReactNode; tone?: 'ok' | 'warn'; children: React.ReactNode }) {
  return (
    <li className="flex items-start gap-2 text-xs">
      <span className={cn('mt-px shrink-0 [&_svg]:size-icon', tone === 'warn' ? 'text-warning' : 'text-muted-foreground')}>{icon}</span>
      <span className="min-w-0">{children}</span>
    </li>
  )
}

export function SolutionPage({ id, catalog: src }: { id: string; catalog?: string }) {
  const { catalog, catalogError } = useConsole()
  // 从首页点视频封面进来的，打开就开始播。
  const [autoplay] = useState(() => { const hit = playIntent.id === 'solution:' + id; playIntent.id = ''; return hit })
  const sk = useSkeleton(!catalog && !catalogError)
  if (catalogError) return <Empty>{catalogError.message}</Empty>
  if (sk.wait || !catalog) return <SkZone show={sk.show} className="block"><SkItem /></SkZone>
  const all = filterSource(catalog.recipes || [], src || PUBLIC).filter(isSolution)
  const r = all.find((x) => x.id === id)
  if (!r) return <EmptyState title="没有这个解决方案。" action={<a href="#solutions" className={buttonVariants({ size: 'sm', variant: 'outline' })}>看全部解决方案</a>} />
  // 别的解决方案：同行业的排前面。
  const mine = r.show?.industries || []
  const rest = orderSolutions(all.filter((x) => x.id !== id), catalog.home?.solutions)
  const near = (x: Recipe) => (x.show?.industries || []).some((i) => mine.includes(i))
  return <SolutionBody r={r} others={[...rest.filter(near), ...rest.filter((x) => !near(x))]} autoplay={autoplay} />
}

function SolutionBody({ r, others, autoplay }: { r: Recipe; others: Recipe[]; autoplay: boolean }) {
  const s = useSolution(r)
  const t = useOpenTarget(s.done ? s.main : undefined)
  const minutes = r.delivery.minutes
  const { catalog } = useConsole()
  const industries = industryNames(catalog, r)
  const have = s.members.filter((m) => (m.app?.installed.length || 0) > 0)
  return (
    <div>
      <div className="grid gap-x-6 gap-y-4 lg:grid-cols-3">
        <aside className="min-w-0 lg:col-start-3 lg:row-start-1">
          <div className="flex flex-col gap-3 lg:sticky lg:top-16">
            <Card className="p-4" data-testid="solution-buy">
              {(industries.length > 0 || r.source) && (
                <div className="mb-2 flex flex-wrap items-center gap-1.5 text-2xs font-medium text-muted-foreground">
                  {industries.map((x) => <span key={x} className="rounded bg-muted px-1.5 py-0.5 text-foreground" data-testid="solution-industry">{x}</span>)}
                  <SourceBadge x={r} />
                </div>
              )}
              <h1 className="text-title font-semibold">{r.title}</h1>
              {s.done && <Installed className="mt-1" />}
              <p className="mt-2 text-sm text-muted-foreground">{r.show?.tagline || r.summary}</p>
              <div className="mt-4">
                {s.done
                  ? <OpenButtons t={t} label={s.mainName ? '打开 ' + s.mainName : '打开'} layout="block" />
                  : <SolutionButton s={s} size="lg" className="w-full" />}
              </div>
              {!s.done && !s.needs && !s.former && (
                <AskAI size="default" className="mt-2 w-full" label="让 AI 帮我装" done={['已复制', '粘贴给 AI 工作台，它会帮你装']}
                  text={askInstallText('recipe', { id: r.id, command: r.command, show: r.show, source: r.source }, r.show?.name || r.title)} />
              )}
              {s.needs && <UpgradeNeeded version={s.needs} className="mt-2" />}
              {s.former && <FormerHint note={s.former} className="mt-2" />}
              <ul className="mt-4 flex flex-col gap-2.5 border-t pt-4">
                <Fact icon={<Sparkles />}>免费 · 全自动安装{minutes ? <> · 约 <span className="tnum">{minutes}</span> 分钟装好</> : null}</Fact>
                {!s.done && have.length > 0 && <Fact icon={<Check />}>你已经装了 {have.map((m) => m.app?.name || m.id).join('、')}，直接用，不重装</Fact>}
                {!s.done && (
                  <Fact icon={<MemoryStick />} tone={s.check.tone === 'low' ? 'warn' : 'ok'}>
                    <span title={s.check.text}>{fitText(s.check.tone)}{s.check.tone === 'low' ? '，先删掉一个不用的应用再装' : ''}</span>
                  </Fact>
                )}
              </ul>
              <div className="mt-4 border-t pt-3" data-testid="solution-apps">
                <div className="text-2xs font-medium text-muted-foreground">里面有 {solutionParts(r)}{r.apps.length > 1 ? '，装完自动接上' : '，都配好了'}</div>
                <div className="mt-1 flex flex-col">
                  {s.members.map((m) => (
                    <a key={m.id} href={itemHref(m.id, sourceId(r))} className="group flex min-w-0 items-center gap-2.5 py-1.5">
                      <StoreIcon a={{ id: m.id, name: m.app?.name || m.id, icon: m.app?.icon, icon_type: m.app?.icon_type }} size={24} />
                      <span className="min-w-0 flex-1 truncate text-sm">{m.app?.name || m.id}</span>
                      {(m.app?.installed.length || 0) > 0 && <Installed />}
                      <ChevronRight className="size-icon shrink-0 text-muted-foreground group-hover:text-foreground" />
                    </a>
                  ))}
                </div>
              </div>
            </Card>
            <ExpertButton />
          </div>
        </aside>

        <div className="flex min-w-0 flex-col gap-4 lg:col-span-2 lg:col-start-1 lg:row-start-1">
          <Card className="p-4 sm:p-5">
            {r.show?.doc
              ? <Doc text={r.show.doc} color={r.show.color} autoplay={autoplay} />
              : <p className="text-sm">{r.summary}</p>}
          </Card>
          <details className="rounded-lg border bg-card" data-testid="solution-more">
            <summary className="cursor-pointer select-none px-4 py-3 text-sm font-medium">技术信息</summary>
            <div className="border-t px-4 py-3"><div className="mb-1 text-2xs text-muted-foreground">用命令行装</div><CopyLine text={r.command} /></div>
          </details>
        </div>
      </div>

      {others.length > 0 && (
        <Shelf title="别的解决方案" icon={<Layers />} count={others.length}>
          {others.map((x) => <SolutionCard key={x.id} r={x} className="w-[19rem] shrink-0" />)}
        </Shelf>
      )}
    </div>
  )
}
