// 改自 shadcn/ui 的 Chart（MIT，许可证见同目录 LICENSE），图表库是 Recharts。
// 和原版不同：原版用 <style> 标签给每个系列写 --color-<key>，被控制台的 CSP（style-src 'self'）拦下；
// 这里把颜色变量写在容器的 style 属性上（React 走脚本写法 CSSOM，CSP 允许）。
import * as React from 'react'
import { ResponsiveContainer } from 'recharts'
import { cn } from '@/lib/utils'

export type ChartConfig = Record<string, { label: string; color: string }>

export function ChartContainer({ config, className, children }: { config: ChartConfig; className?: string; children: React.ReactElement }) {
  const style = React.useMemo(() => {
    const s: Record<string, string> = {}
    for (const [k, v] of Object.entries(config)) s['--color-' + k] = v.color
    return s as React.CSSProperties
  }, [config])
  return (
    <div
      data-chart=""
      style={style}
      className={cn(
        'h-full w-full min-w-0 text-2xs',
        '[&_.recharts-cartesian-axis-tick_text]:fill-muted-foreground [&_.recharts-cartesian-grid_line]:stroke-border',
        '[&_.recharts-curve.recharts-tooltip-cursor]:stroke-muted-foreground [&_.recharts-rectangle.recharts-tooltip-cursor]:fill-muted/60',
        '[&_.recharts-surface]:outline-none [&_.recharts-wrapper]:outline-none',
        className,
      )}
    >
      <ResponsiveContainer width="100%" height="100%" minWidth={0} minHeight={0}>
        {children}
      </ResponsiveContainer>
    </div>
  )
}

export interface TipRow { name: string; value: string; color: string }

// ChartTip：悬停时的小卡片，标题加每个系列一行。
export function ChartTip({ title, rows }: { title?: string; rows: TipRow[] }) {
  if (!rows.length) return null
  return (
    <div className="min-w-[96px] rounded-md border bg-popover px-2 py-1.5 text-xs text-popover-foreground shadow-md">
      {title && <div className="mb-1 text-2xs text-muted-foreground">{title}</div>}
      {rows.map((r) => (
        <div key={r.name} className="flex items-center gap-2">
          <span className="size-2 shrink-0 rounded-sm" style={{ background: r.color }} />
          <span className="flex-1 text-muted-foreground">{r.name}</span>
          <span className="tnum font-medium">{r.value}</span>
        </div>
      ))}
    </div>
  )
}

export function ChartLegend({ items, className }: { items: { name: string; color: string }[]; className?: string }) {
  return (
    <div className={cn('flex flex-wrap items-center gap-x-3 gap-y-1 text-2xs text-muted-foreground', className)}>
      {items.map((s) => (
        <span key={s.name} className="inline-flex items-center gap-1.5">
          <span className="size-2 rounded-sm" style={{ background: s.color }} />
          {s.name}
        </span>
      ))}
    </div>
  )
}
