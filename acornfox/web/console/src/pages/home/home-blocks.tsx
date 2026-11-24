// 首页上不是看板的那几块。首页（有应用时）只有图表看板（chart-board.tsx），这里只剩
// 刚装好、还没有应用的机器用的 FirstSteps（“第 1 步　装一个应用 / 第 2 步　打开它”）和 MachineBlock（这台机器三行）。
// “需要你处理”那一块（“首页的这个模块也不要”）、“我的应用”小卡片、“最近发生的事”那一块都拿掉了：
// 要处理的事在铃铛、“我的应用”后面的点、看板“应用状态”的“要处理 N”；最近发生的事是看板里的一张图。
// 每块只负责画，数据由 dashboard-page.tsx 取好传进来：这台机器：GET /v1/metrics/host；
//   第 1 步里的解决方案：GET /v1/catalog，取法和应用中心首页一样（推荐的排前面，取前三个，见 pages/store/store-page.tsx）。
import * as React from 'react'
import { ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { WORDS } from '@/lib/display'
import type { HostView } from '@/lib/types'
import { useConsole } from '@/state'
import { EmptyState } from '@/components/fold'
import { buttonVariants } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Loading } from '@/components/ui/skeleton'
import { SkMachineRows } from '@/components/skeletons'
import { Progress } from '@/components/ui/progress'
import { SolutionCard, isSolution } from '@/pages/store/shelf'
import { orderSolutions } from '@/lib/catalog'

function Head({ title, right }: { title: React.ReactNode; right?: React.ReactNode }) {
  return (
    <div className="mb-2 flex items-center gap-2">
      <h2 className="text-section font-medium">{title}</h2>
      {right && <div className="ml-auto">{right}</div>}
    </div>
  )
}

const GB = 1024 ** 3
const gb = (v: number) => (Math.round((v / GB) * 10) / 10).toString()

interface MachineRow { label: string; pct: number; left: string; total: string }

export function machineRows(h: HostView | null): MachineRow[] | null {
  if (!h || !h.available) return null
  const rows: MachineRow[] = []
  if (h.memory_total && h.memory_used !== undefined) rows.push({ label: '内存', pct: (h.memory_used / h.memory_total) * 100, left: `还剩 ${gb(h.memory_total - h.memory_used)} GB`, total: `共 ${gb(h.memory_total)} GB` })
  if (h.disk_total && h.disk_used !== undefined) rows.push({ label: '磁盘', pct: (h.disk_used / h.disk_total) * 100, left: `还剩 ${gb(h.disk_total - h.disk_used)} GB`, total: `共 ${gb(h.disk_total)} GB` })
  if (h.cpu_percent !== undefined) rows.push({ label: 'CPU', pct: h.cpu_percent, left: `还剩 ${Math.max(0, Math.round(100 - h.cpu_percent))}%`, total: h.cpu_cores ? `${h.cpu_cores} 核` : '' })
  return rows
}

// MachineBlock：这台机器三行：内存、磁盘、CPU，各一行进度条加一句“还剩多少”。
export function MachineBlock({ host, loading, className }: { host: HostView | null; loading?: boolean; className?: string }) {
  const rows = machineRows(host)
  return (
    <section className={className}>
      <Head title={WORDS.machine} />
      <Card className="flex flex-col gap-3.5 p-3.5">
        <Loading when={!!loading} skeleton={<SkMachineRows />}>
        {() => !rows ? (
          <p className="text-xs text-muted-foreground">读不到这台机器的用量</p>
        ) : rows.map((r) => (
          <div key={r.label} className="flex flex-col gap-1.5" data-testid="machine-row">
            <div className="flex items-baseline gap-2 text-sm">
              <span className="font-medium">{r.label}</span>
              <span className="tnum ml-auto text-xs">{r.left}</span>
              {r.total && <span className="tnum text-2xs text-muted-foreground">{r.total}</span>}
            </div>
            <Progress value={r.pct} tone={r.pct > 90 ? 'danger' : r.pct > 80 ? 'warning' : undefined} label={r.label} />
          </div>
        ))}
        </Loading>
      </Card>
    </section>
  )
}

const TOP_SOLUTIONS = 3

function StepNo({ n, on }: { n: number; on: boolean }) {
  return <span className={cn('flex size-5 shrink-0 items-center justify-center rounded-full text-2xs font-semibold', on ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground')}>{n}</span>
}

// FirstSteps：刚装好、还没有应用的机器（评估 C4）。第 1 步摆应用中心首页最前面的三个解决方案；第 2 步灰着。
export function FirstSteps() {
  const { catalog } = useConsole()
  const top = orderSolutions((catalog?.recipes || []).filter(isSolution), catalog?.home?.solutions).slice(0, TOP_SOLUTIONS)
  return (
    <Card className="flex flex-col gap-4 p-4" data-testid="first-steps">
      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-2 text-sm font-medium"><StepNo n={1} on />装一个应用</div>
        {top.length ? (
          <>
            <div className="grid gap-2 lg:grid-cols-3">{top.map((r) => <SolutionCard key={r.id} r={r} size="sm" />)}</div>
            <a href="#store" className="tap-h inline-flex items-center self-start text-xs text-muted-foreground hover:text-foreground">去应用中心看全部<ChevronRight className="size-3" /></a>
          </>
        ) : (
          <EmptyState title="应用中心里有很多现成的应用" action={<a href="#store" className={buttonVariants()}>去应用中心挑一个</a>} />
        )}
      </div>
      <div className="flex items-center gap-2 border-t pt-4 text-sm font-medium text-muted-foreground"><StepNo n={2} on={false} />打开它</div>
    </Card>
  )
}
