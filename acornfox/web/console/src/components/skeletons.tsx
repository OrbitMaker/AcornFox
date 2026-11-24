// 各页等数据时的占位形状（样稿第 12 版）。每一个都照着真内容的版面画：同样的格子、同样的卡片内边距、同样大的图标块，
// 这样数据来了真内容落在同一个位置，版面不跳。用法：<Loading when={!data} skeleton={<SkAppCards />}>{() => 真内容}</Loading>
// （components/ui/skeleton.tsx）。改了哪一页的版面，记得把这里对应的形状也改了。
import { cn } from '@/lib/utils'
import { Sk } from './ui/skeleton'

const times = (n: number) => Array.from({ length: n }, (_, i) => i)
// 一行字的长短错开，看着不像一堵墙。
const W = ['w-24', 'w-32', 'w-20', 'w-28', 'w-36', 'w-16']
const WIDE = ['w-11/12', 'w-4/5', 'w-full', 'w-3/4', 'w-5/6', 'w-2/3']
const HALF = ['w-2/5', 'w-1/3', 'w-1/2', 'w-1/4', 'w-2/5', 'w-1/3']

// 我的应用：卡片（图标、名字、状态；底下一行来源和“打开”）
export function SkAppCards({ n = 6 }: { n?: number }) {
  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      {times(n).map((i) => (
        <div key={i} className="flex min-w-0 flex-col gap-3 rounded-lg border bg-card p-3">
          <div className="flex items-center gap-2.5">
            <Sk className="size-tile-md rounded-md" />
            <div className="flex min-w-0 flex-1 flex-col gap-1.5 py-0.5"><Sk className={cn('h-3.5', W[i % 6])} /><Sk className="h-2.5 w-12" /></div>
          </div>
          <div className="flex min-h-ctl-sm items-center gap-1.5"><Sk className="mr-auto h-2.5 w-20" /><Sk className="h-ctl-sm w-14 rounded-md" /></div>
        </div>
      ))}
    </div>
  )
}

// 这台机器三行：名字、还剩多少、一条进度条
export function SkMachineRows() {
  return (
    <>
      {times(3).map((i) => (
        <div key={i} className="flex flex-col gap-1.5">
          <div className="flex h-[var(--lh-sm)] items-center gap-2"><Sk className="h-3 w-8" /><Sk className="ml-auto h-2.5 w-20" /><Sk className="h-2.5 w-12" /></div>
          <Sk round className="h-1.5 w-full" />
        </div>
      ))}
    </>
  )
}

// 卡片里的几行字（最近发生的事、技术信息）
export function SkLines({ n = 4, className }: { n?: number; className?: string }) {
  return (
    <div className={cn('flex flex-col gap-2.5', className)}>
      {times(n).map((i) => <div key={i} className="flex h-[var(--lh-sm)] items-center gap-2"><Sk className="size-icon rounded" /><span className="flex min-w-0 flex-1"><Sk className={cn('h-3', HALF[i % 6])} /></span><Sk className="h-2.5 w-10" /></div>)}
    </div>
  )
}

// 首页看板里一张图的里面（卡片的标题不在这里）。照图表方式画：状态数是三个数；进度条是一个数、右边一句、一条；
// 条形图是几行；列表是几行字；曲线、柱状图是一整块。compact：矮卡片。样稿第 14 版。
export function SkViz({ type, compact }: { type: string; compact?: boolean }) {
  if (type === 'kpi') {
    return <div className="grid h-full grid-cols-3 items-center gap-2">{times(3).map((i) => <div key={i} className="flex flex-col gap-2"><Sk className="h-5 w-6" /><Sk className="h-2.5 w-10" /></div>)}</div>
  }
  if (type === 'meter' || type === 'stat') {
    return (
      <div className="flex h-full flex-col justify-center gap-2.5">
        <div className="flex items-center justify-between gap-2"><Sk className="h-5 w-24" /><Sk className="h-2.5 w-14" /></div>
        <Sk round className="h-1.5 w-full" />
        {!compact && <Sk className="h-2.5 w-20" />}
      </div>
    )
  }
  if (type === 'hbar' || type === 'segbar' || type === 'donut' || type === 'table') {
    return (
      <div className="flex h-full flex-col justify-center gap-2.5">
        {times(compact ? 2 : 5).map((i) => <div key={i} className="grid grid-cols-[minmax(0,7.5rem)_1fr_4.5rem] items-center gap-2.5"><span className="flex h-[var(--lh-xs)] min-w-0 items-center"><Sk className={cn('h-2.5 max-w-full', W[(i + 1) % 6])} /></span><Sk className="h-2.5 rounded-sm" /><Sk className="ml-auto h-2.5 w-10" /></div>)}
      </div>
    )
  }
  if (type === 'list') {
    return <div className="flex h-full flex-col gap-2.5 overflow-hidden">{times(compact ? 2 : 6).map((i) => <div key={i} className="flex h-[var(--lh-xs)] items-center gap-2"><Sk className="size-icon rounded" /><span className="flex min-w-0 flex-1"><Sk className={cn('h-2.5', HALF[i % 6])} /></span><Sk className="h-2.5 w-10" /></div>)}</div>
  }
  return <Sk className="h-full w-full rounded-md" />
}

// 首页整页（应用列表还没到，不知道是摆看板还是“第 1 步”）：照默认看板的样子画——
// 时间范围和“编辑”一排，四张矮卡，一张宽的曲线加一张列表，两张条形图。格子用的就是看板自己的（.dash、.wd）。
export function SkHome() {
  const card = (w: number, h: number, type: string) => (
    <div className="wd flex min-w-0 flex-col rounded-lg border bg-card p-3.5" data-w={w} data-h={h}>
      <div className="mb-2 flex h-5 items-center gap-1.5"><Sk className="h-3 w-14" /><Sk className="h-2.5 w-8" /></div>
      <div className="min-h-0 flex-1"><SkViz type={type} compact={h === 1} /></div>
    </div>
  )
  return (
    <div className="flex flex-col">
      <div className="mb-3 flex items-center"><Sk className="h-tabs w-36 rounded-md" /><Sk className="ml-auto h-ctl w-20 rounded-md" /></div>
      <div className="dash">
        {card(3, 1, 'kpi')}{card(3, 1, 'meter')}{card(3, 1, 'meter')}{card(3, 1, 'meter')}
        {card(8, 2, 'area')}{card(4, 2, 'list')}
        {card(6, 2, 'hbar')}{card(6, 2, 'hbar')}
      </div>
    </div>
  )
}

// 应用详情的名片：大图标、名字、一句状态，右边“打开”和“更多”
export function SkAppHead() {
  return (
    <div className="mb-4 mt-1 flex flex-wrap items-center gap-3 sm:mt-0">
      <Sk className="size-tile-lg rounded-lg" />
      <div className="flex min-w-0 flex-1 flex-col gap-2"><Sk className="h-4 w-28" /><Sk className="h-3 w-40" /></div>
      <div className="flex items-center gap-1.5"><Sk className="h-ctl w-[4.5rem] rounded-md" /><Sk className="size-ctl rounded-md" /></div>
    </div>
  )
}

// 概览里小卡片的标题那一行（“运行　近 30 天”）
function SkCardHead({ note }: { note?: boolean }) {
  return <div className="box-content flex h-[var(--lh-xs)] items-center justify-between pt-1"><Sk className="h-3 w-8" />{note && <Sk className="h-2.5 w-10" />}</div>
}

// 概览“资源”卡片里的两个圆环（CPU、内存；内存下面多一行“用了多少 / 最多多少”）。样稿第 14 版。
export function SkRings() {
  return (
    <div className="mt-2.5 flex">
      {times(2).map((i) => (
        <div key={i} className="flex flex-1 flex-col items-center">
          <Sk round className="size-donut" />
          <span className="mt-0.5 flex h-[var(--lh-xs)] items-center"><Sk className="h-2.5 w-8" /></span>
          {i === 1 && <span className="flex h-[var(--lh-2xs)] items-center"><Sk className="h-2.5 w-20" /></span>}
        </div>
      ))}
    </div>
  )
}

// 概览“版本”卡片里：自己部署的项目的时间线（几个圆点，下面两行字）
export function SkTimeline({ n = 3 }: { n?: number }) {
  return (
    <div className="mt-5 flex justify-between px-2">
      {times(n).map((i) => <div key={i} className="flex flex-col items-center"><Sk round className="size-5" /><span className="mt-2 flex h-[var(--lh-xs)] items-center"><Sk className="h-2.5 w-10" /></span><span className="flex h-[var(--lh-2xs)] items-center"><Sk className="h-2.5 w-8" /></span></div>)}
    </div>
  )
}

// 概览“它用到的服务”卡片：标题，下面每条服务一行（小图块、一句话、右边“已接上”）
export function SkLinks({ n = 1 }: { n?: number }) {
  return (
    <div className="rounded-lg border bg-card px-3.5 pb-2 pt-2">
      <SkCardHead />
      <div className="mt-0.5">
        {times(n).map((i) => <div key={i} className="flex items-center gap-2.5 py-1.5"><Sk className="size-tile-xs rounded" /><span className="flex h-[var(--lh-sm)] flex-1 items-center"><Sk className={cn('h-3', W[(i + 4) % 6])} /></span><Sk className="h-5 w-14 rounded" /></div>)}
      </div>
    </div>
  )
}

// 应用详情 › 概览：“谁能打开”卡片、三张小卡（运行：大字和 30 天的一条；资源：两个圆环；版本：大字和一行）
export function SkOverview() {
  return (
    <div className="flex flex-col gap-3">
      <div className="rounded-lg border bg-card px-3.5 pb-3 pt-2">
        <SkCardHead />
        <div className="mt-2 flex flex-wrap items-center gap-2.5"><span className="flex h-7 items-center"><Sk className="h-5 w-48" /></span><Sk className="h-ctl-sm w-24 rounded-md" /><Sk className="h-ctl-sm w-24 rounded-md" /><Sk className="ml-auto h-tabs w-44 rounded-md" /></div>
        <div className="mt-2 flex h-[var(--lh-2xs)] items-center justify-end"><Sk className="h-2.5 w-56" /></div>
      </div>
      <div className="grid gap-3 md:grid-cols-3">
        <div className="flex flex-col rounded-lg border bg-card px-3.5 pb-3 pt-2">
          <SkCardHead note />
          <div className="mt-2 flex h-[var(--lh-2xl)] items-center"><Sk className="h-6 w-20" /></div>
          <Sk className="mt-2.5 h-7 w-full rounded-sm" />
          <div className="mt-1 flex h-[var(--lh-2xs)] items-center justify-between"><Sk className="h-2.5 w-10" /><Sk className="h-2.5 w-8" /></div>
        </div>
        <div className="flex flex-col rounded-lg border bg-card px-3.5 pb-3 pt-2"><SkCardHead note /><SkRings /></div>
        <div className="flex flex-col rounded-lg border bg-card px-3.5 pb-3 pt-2">
          <SkCardHead />
          <div className="mt-2 flex flex-1 flex-col justify-center"><span className="flex h-[var(--lh-2xl)] items-center"><Sk className="h-6 w-24" /></span><span className="mt-1 flex h-[var(--lh-xs)] items-center"><Sk className="h-2.5 w-20" /></span></div>
          <div className="mt-2.5 flex h-[var(--lh-2xs)] items-center"><Sk className="h-2.5 w-14" /></div>
        </div>
      </div>
    </div>
  )
}

// 应用详情 › 版本：版本记录（表头一条，下面每版一行：第几版、状态、来源、时间，右边按钮）。样稿第 14 版。
export function SkVersions({ n = 4 }: { n?: number }) {
  return (
    <div>
      <div className="hidden h-ctl border-y bg-muted/30 sm:block" />
      <div className="border-t sm:border-t-0">
        {times(n).map((i) => (
          <div key={i} className="flex items-center gap-4 border-b px-3.5 py-2 last:border-0">
            <span className="flex h-ctl-sm items-center"><Sk className="h-3 w-12" /></span>
            <Sk className="h-2.5 w-14" /><Sk className={cn('hidden h-2.5 sm:block', W[(i + 1) % 6])} /><Sk className="hidden h-2.5 w-20 sm:block" />
            <Sk className="ml-auto h-ctl-sm w-20 rounded-md" />
          </div>
        ))}
      </div>
    </div>
  )
}

// 日志：等宽的一行一行
export function SkLogLines({ n = 12 }: { n?: number }) {
  return <div className="flex flex-col gap-[7px] py-0.5">{times(n).map((i) => <div key={i} className="flex gap-3"><Sk className="h-2.5 w-28 rounded" /><Sk className={cn('h-2.5 rounded', WIDE[(i * 5) % 6].replace('w-full', 'w-1/2'))} /></div>)}</div>
}

// 一行一项的设置行（设置 › 服务、访问；应用的版本记录）
export function SkRows({ n = 3, className }: { n?: number; className?: string }) {
  return (
    <div className={cn('divide-y', className)}>
      {times(n).map((i) => (
        <div key={i} className="flex items-center gap-3 px-4 py-row">
          <div className="flex h-[var(--lh-sm)] flex-1 items-center gap-3"><Sk className="h-3 w-20 sm:w-24" /><Sk className={cn('h-2.5', W[(i + 3) % 6])} /></div>
          <Sk className="size-4 rounded" />
        </div>
      ))}
    </div>
  )
}

// 应用中心首页：三张解决方案大卡、一面应用
export function SkStore() {
  return (
    <div>
      <div className="mb-3 flex h-[var(--lh-base)] items-center"><Sk className="h-4 w-36" /></div>
      <div className="mb-8 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {times(3).map((i) => (
          <div key={i} className="overflow-hidden rounded-lg border bg-card">
            <Sk className="aspect-[16/9] w-full rounded-none" />
            <div className="flex flex-col gap-2 p-3.5"><Sk className={cn('h-4', W[i + 3])} /><Sk className="h-2.5 w-4/5" /><Sk className="mt-1 h-2.5 w-24" /></div>
          </div>
        ))}
      </div>
      <div className="mb-3 flex h-[var(--lh-base)] items-center"><Sk className="h-4 w-20" /></div>
      <SkProductCards n={10} />
    </div>
  )
}

// 应用中心的商品卡：上面封面，下面图标、名字、一句话、按钮
export function SkProductCards({ n = 8 }: { n?: number }) {
  return (
    <div className="grid gap-3 [grid-template-columns:repeat(auto-fill,minmax(13rem,1fr))]">
      {times(n).map((i) => (
        <div key={i} className="flex min-w-0 flex-col overflow-hidden rounded-lg border bg-card">
          <Sk className="aspect-[16/10] w-full rounded-none" />
          <div className="flex flex-col gap-2 p-3">
            <div className="flex items-center gap-2"><Sk className="size-7 rounded-md" /><Sk className={cn('h-3.5', W[i % 6])} /></div>
            <Sk className="h-2.5 w-11/12" /><Sk className="h-2.5 w-2/3" />
            <div className="flex items-center pt-1"><Sk className="h-2.5 w-16" /><Sk className="ml-auto h-ctl-sm w-12 rounded-md" /></div>
          </div>
        </div>
      ))}
    </div>
  )
}

// 应用页、解决方案页：右边安装栏，左边一篇介绍
export function SkItem() {
  return (
    <div className="grid gap-x-6 gap-y-4 lg:grid-cols-3">
      <aside className="min-w-0 lg:col-start-3 lg:row-start-1">
        <div className="flex flex-col gap-3 rounded-lg border bg-card p-4">
          <div className="flex items-center gap-3"><Sk className="size-11 rounded-lg" /><div className="flex flex-col gap-2"><Sk className="h-4 w-24" /><Sk className="h-2.5 w-16" /></div></div>
          <Sk className="h-3 w-11/12" /><Sk className="h-3 w-2/3" />
          <Sk className="mt-1 h-ctl-lg w-full rounded-md" />
          <Sk className="h-2.5 w-32" /><Sk className="h-2.5 w-40" />
        </div>
      </aside>
      <div className="min-w-0 lg:col-span-2 lg:col-start-1 lg:row-start-1">
        <div className="flex flex-col gap-3 rounded-lg border bg-card p-4 sm:p-5">
          <Sk className="aspect-video max-h-80 w-full rounded-lg" />
          <Sk className="mt-2 h-4 w-32" />
          {times(6).map((i) => <Sk key={i} className={cn('h-3', WIDE[i])} />)}
        </div>
      </div>
    </div>
  )
}

// 弹窗、卡片里的一个表单：几组“名字 + 输入框”
export function SkForm({ n = 3 }: { n?: number }) {
  return <div className="flex flex-col gap-4">{times(n).map((i) => <div key={i} className="flex flex-col gap-2"><Sk className={cn('h-2.5', W[i % 6])} /><Sk className="h-ctl w-full rounded-md" /></div>)}</div>
}
