// 首页看板的图表：面积图、折线图、柱状图、环形图、迷你曲线用 Recharts（shadcn Charts 的写法，见 components/ui/chart.tsx），
// 条形图、分段条、数字、进度条、状态数、表格、列表是普通的页面元素。样子照控制台样稿。
import * as React from 'react'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Cell, Line, LineChart, Pie, PieChart, Tooltip, XAxis, YAxis, type TooltipContentProps } from 'recharts'
import { AlertTriangle, ArrowUpCircle, Bell, CheckCircle2, History, Loader2, Package, Unlink, Wrench } from 'lucide-react'
import { cn } from '@/lib/utils'
import { fmtValue, type CatItem, type Data, type ListItem } from '@/lib/dashboard'
import { go } from '@/lib/router'
import { ChartContainer, ChartLegend, ChartTip, type ChartConfig } from '@/components/ui/chart'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'

type TS = Extract<Data, { kind: 'ts' }>
type Cat = Extract<Data, { kind: 'cat' }>
type Val = Extract<Data, { kind: 'value' }>
type List = Extract<Data, { kind: 'list' }>

const AXIS = { fontSize: 10 }

function niceMax(v: number): number {
  if (!(v > 0)) return 1
  const p = Math.pow(10, Math.floor(Math.log10(v)))
  const n = v / p
  return (n <= 1 ? 1 : n <= 2 ? 2 : n <= 2.5 ? 2.5 : n <= 5 ? 5 : 10) * p
}
const shortNum = (v: number) => (v >= 1000 ? Math.round(v / 100) / 10 + 'k' : String(Math.round(v * 10) / 10))

function tsRows(d: TS) {
  return d.labels.map((lb, i) => {
    const row: Record<string, number | string | null> = { x: lb.s, full: lb.l }
    for (const s of d.series) row[s.key] = s.data[i]
    return row
  })
}

function yMax(d: TS, stacked: boolean): number {
  const vals = d.labels.map((_, i) => (stacked ? d.series.reduce((a, s) => a + (s.data[i] || 0), 0) : Math.max(0, ...d.series.map((s) => s.data[i] || 0))))
  const vmax = Math.max(0, ...vals)
  if (d.max && d.series.length === 1 && vmax > d.max * 0.6) return d.max
  return niceMax(vmax * 1.08)
}

function TimeChart({ d, type }: { d: TS; type: 'area' | 'line' | 'column' }) {
  const multi = d.series.length > 1
  const stacked = type !== 'line' && multi
  const data = React.useMemo(() => tsRows(d), [d])
  const config = React.useMemo<ChartConfig>(() => Object.fromEntries(d.series.map((s) => [s.key, { label: s.name, color: s.color }])), [d.series])
  const max = yMax(d, stacked)
  const tip = (p: TooltipContentProps) => {
    if (!p.active || !p.payload?.length) return null
    const row = (p.payload[0].payload || {}) as Record<string, unknown>
    return <ChartTip title={String(row.full || '')} rows={d.series.map((s) => ({ name: s.name, color: s.color, value: fmtValue(row[s.key] as number | null, d.unit) })).reverse()} />
  }
  const common = { data, margin: { top: 6, right: 4, bottom: 0, left: 0 } }
  const axes = [
    <CartesianGrid key="g" vertical={false} strokeDasharray="0" />,
    <XAxis key="x" dataKey="x" tickLine={false} axisLine={false} tick={AXIS} minTickGap={24} interval="preserveStartEnd" />,
    <YAxis key="y" width={30} tickLine={false} axisLine={false} tick={AXIS} domain={[0, max]} ticks={[0, max / 2, max]} tickFormatter={shortNum} />,
    <Tooltip key="t" content={tip} isAnimationActive={false} />,
  ]
  let chart
  if (type === 'column') {
    chart = (
      <BarChart {...common}>
        {axes}
        {d.series.map((s) => <Bar key={s.key} dataKey={s.key} stackId={stacked ? 'a' : undefined} fill={`var(--color-${s.key})`} radius={stacked ? 0 : [2, 2, 0, 0]} maxBarSize={18} isAnimationActive={false} />)}
      </BarChart>
    )
  } else if (type === 'line') {
    chart = (
      <LineChart {...common}>
        {axes}
        {d.series.map((s) => <Line key={s.key} dataKey={s.key} type="monotone" stroke={`var(--color-${s.key})`} strokeWidth={1.75} dot={false} connectNulls isAnimationActive={false} />)}
      </LineChart>
    )
  } else {
    chart = (
      <AreaChart {...common}>
        {axes}
        {d.series.map((s) => <Area key={s.key} dataKey={s.key} type="monotone" stackId={stacked ? 'a' : undefined} stroke={`var(--color-${s.key})`} fill={`var(--color-${s.key})`} fillOpacity={stacked ? 0.28 : 0.14} strokeWidth={1.75} connectNulls isAnimationActive={false} />)}
      </AreaChart>
    )
  }
  return (
    <div className="flex h-full flex-col gap-1.5">
      {multi && <ChartLegend items={d.series} />}
      <div className="min-h-0 flex-1">
        <ChartContainer config={config}>{chart}</ChartContainer>
      </div>
    </div>
  )
}

function sortCat(d: Cat): CatItem[] {
  return d.status ? d.items : [...d.items].sort((a, b) => b.value - a.value)
}

function CatColumn({ d }: { d: Cat }) {
  const items = sortCat(d)
  const data = items.map((i) => ({ x: i.name.length > 6 ? i.name.slice(0, 5) + '…' : i.name, full: i.name, v: i.value, color: i.color }))
  const max = niceMax(Math.max(0, ...items.map((i) => i.value)) * 1.08)
  const tip = (p: TooltipContentProps) => {
    if (!p.active || !p.payload?.length) return null
    const r = (p.payload[0].payload || {}) as Record<string, unknown>
    return <ChartTip rows={[{ name: String(r.full), color: String(r.color), value: fmtValue(r.v as number, d.unit) }]} />
  }
  return (
    <ChartContainer config={{ v: { label: '数值', color: 'var(--s1)' } }}>
      <BarChart data={data} margin={{ top: 6, right: 4, bottom: 0, left: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="x" tickLine={false} axisLine={false} tick={AXIS} interval={0} />
        <YAxis width={30} tickLine={false} axisLine={false} tick={AXIS} domain={[0, max]} ticks={[0, max / 2, max]} tickFormatter={shortNum} />
        <Tooltip content={tip} isAnimationActive={false} />
        <Bar dataKey="v" radius={[2, 2, 0, 0]} maxBarSize={22} isAnimationActive={false}>
          {data.map((r) => <Cell key={r.full} fill={r.color} />)}
        </Bar>
      </BarChart>
    </ChartContainer>
  )
}

function HBar({ d }: { d: Cat }) {
  const items = sortCat(d)
  const max = Math.max(...items.map((i) => i.value)) || 1
  return (
    <div className="flex h-full flex-col justify-center gap-2 overflow-y-auto">
      {items.map((it) => (
        <div key={it.name} className="group grid grid-cols-[minmax(0,96px)_1fr_auto] items-center gap-2 text-xs" title={it.name + '：' + fmtValue(it.value, d.unit)}>
          <span className="truncate text-muted-foreground">{it.name}</span>
          <div className="h-2.5 rounded-sm bg-muted/60">
            <div className="h-full rounded-sm transition-all group-hover:opacity-80" style={{ width: Math.max(2, (it.value / max) * 100) + '%', background: it.color }} />
          </div>
          <span className="tnum w-16 text-right font-medium">{fmtValue(it.value, d.unit)}</span>
        </div>
      ))}
    </div>
  )
}

function SegBar({ d }: { d: Cat }) {
  const total = d.items.reduce((s, i) => s + i.value, 0)
  const [hi, setHi] = React.useState<number | null>(null)
  return (
    <div className="flex h-full flex-col justify-center gap-2.5">
      <div className="flex items-baseline gap-1.5"><span className="tnum text-xl font-semibold">{fmtValue(total, '')}</span><span className="text-xs text-muted-foreground">{d.unit} 合计</span></div>
      <div className="flex h-7 w-full gap-[2px] overflow-hidden rounded bg-muted/50">
        {total > 0 && d.items.map((it, i) => {
          const p = (it.value / total) * 100
          if (p <= 0) return null
          return (
            <div key={it.name} onMouseEnter={() => setHi(i)} onMouseLeave={() => setHi(null)} title={it.name + '：' + fmtValue(it.value, d.unit)}
              className="flex min-w-[3px] items-center justify-center overflow-hidden text-2xs font-medium text-white transition-opacity"
              style={{ width: p + '%', background: it.color, opacity: hi === null || hi === i ? 1 : 0.5 }}>
              {p > 12 ? Math.round(p) + '%' : ''}
            </div>
          )
        })}
      </div>
      <div className="grid grid-cols-2 gap-x-3 gap-y-1">
        {d.items.map((it, i) => (
          <div key={it.name} className={cn('flex items-center gap-1.5 text-2xs', hi === i && 'text-foreground')}>
            <span className="size-2 shrink-0 rounded-sm" style={{ background: it.color }} />
            <span className="flex-1 truncate text-muted-foreground">{it.name}</span>
            <span className="tnum">{fmtValue(it.value, d.unit)}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function Donut({ d }: { d: Cat }) {
  const total = d.items.reduce((s, i) => s + i.value, 0)
  const [hi, setHi] = React.useState<number | null>(null)
  const items = d.items.filter((i) => i.value > 0)
  const cur = hi !== null ? items[hi] : null
  return (
    <div className="flex h-full items-center gap-4">
      <div className="relative aspect-square h-full max-h-full min-h-0 max-w-[50%]">
        <ChartContainer config={{}}>
          <PieChart>
            <Pie data={items.length ? items : [{ name: '', value: 1, color: 'hsl(var(--muted))' }]} dataKey="value" nameKey="name" innerRadius="62%" outerRadius="100%" paddingAngle={items.length > 1 ? 2 : 0} stroke="none" isAnimationActive={false}
              onMouseEnter={(_: unknown, i: number) => setHi(i)} onMouseLeave={() => setHi(null)}>
              {(items.length ? items : [{ name: '', color: 'hsl(var(--muted))' }]).map((it, i) => <Cell key={i} fill={it.color} opacity={hi === null || hi === i ? 1 : 0.4} />)}
            </Pie>
          </PieChart>
        </ChartContainer>
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
          <span className="tnum text-sm font-semibold">{fmtValue(cur ? cur.value : total, '')}</span>
          <span className="max-w-[80%] truncate text-2xs text-muted-foreground">{cur ? cur.name : d.unit + ' 合计'}</span>
        </div>
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        {items.map((it, i) => (
          <div key={it.name} onMouseEnter={() => setHi(i)} onMouseLeave={() => setHi(null)} className="flex items-center gap-1.5 text-2xs">
            <span className="size-2 shrink-0 rounded-sm" style={{ background: it.color }} />
            <span className="flex-1 truncate text-muted-foreground">{it.name}</span>
            <span className="tnum">{total ? Math.round((it.value / total) * 100) : 0}%</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function Kpi({ d }: { d: Cat }) {
  return (
    <div className="grid h-full items-center gap-2" style={{ gridTemplateColumns: `repeat(${Math.max(1, d.items.length)}, minmax(0, 1fr))` }}>
      {d.items.map((it) => (
        <div key={it.name} className="flex min-w-0 flex-col gap-1">
          <span className="tnum text-xl font-semibold leading-none">{it.value}</span>
          <span className="flex items-center gap-1 truncate text-2xs text-muted-foreground"><span className="size-1.5 shrink-0 rounded-full" style={{ background: it.color }} />{it.name}</span>
        </div>
      ))}
    </div>
  )
}

function lastValue(s: (number | null)[]): number | null {
  for (let i = s.length - 1; i >= 0; i--) if (s[i] !== null) return s[i]
  return null
}

function Spark({ data, color }: { data: (number | null)[]; color: string }) {
  const rows = data.map((v, i) => ({ i, v }))
  return (
    <ChartContainer config={{ v: { label: '', color } }}>
      <AreaChart data={rows} margin={{ top: 2, right: 2, bottom: 2, left: 2 }}>
        <YAxis hide domain={['dataMin - 1', 'dataMax + 1']} />
        <Area dataKey="v" type="monotone" stroke="var(--color-v)" fill="var(--color-v)" fillOpacity={0.12} strokeWidth={1.5} dot={false} connectNulls isAnimationActive={false} />
      </AreaChart>
    </ChartContainer>
  )
}

function Stat({ d }: { d: TS | Val }) {
  if (d.kind === 'value') {
    return (
      <div className="flex h-full flex-col justify-center gap-1">
        <div className="flex items-baseline gap-1"><span className="tnum text-xl font-semibold leading-none">{fmtValue(d.value, '')}</span><span className="text-xs text-muted-foreground">/ {fmtValue(d.max, d.unit)}</span></div>
        <span className="text-2xs text-muted-foreground">{d.sub}</span>
      </div>
    )
  }
  const s = d.series[0].data
  const vals = s.filter((v): v is number => v !== null)
  const last = lastValue(s)
  const avg = vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : null
  return (
    <div className="flex h-full flex-col gap-1">
      <div className="flex items-baseline gap-2">
        <span className="tnum text-xl font-semibold leading-none">{fmtValue(last, d.unit)}</span>
        {avg !== null && last !== null && <span className="tnum text-2xs text-muted-foreground">{last >= avg ? '↑' : '↓'} 均值 {fmtValue(avg, d.unit)}</span>}
      </div>
      <div className="min-h-0 flex-1">{vals.length > 1 && <Spark data={s} color={d.series[0].color} />}</div>
    </div>
  )
}

function Meter({ d }: { d: TS | Val }) {
  let v: number | null, max: number, unit: string, sub: string, tone: string | undefined
  if (d.kind === 'value') {
    v = d.value; max = d.max; unit = d.unit; sub = d.sub; tone = d.tone
  } else {
    v = lastValue(d.series[0].data); max = d.max || 100; unit = d.unit; sub = d.maxLabel || '上限 ' + fmtValue(max, unit)
    tone = v !== null && v / max > 0.85 ? 'warning' : undefined
  }
  const pct = v === null || !max ? 0 : (v / max) * 100
  return (
    <div className="flex h-full flex-col justify-center gap-2">
      <div className="flex items-baseline justify-between gap-2">
        <span><span className="tnum text-xl font-semibold leading-none">{fmtValue(v, '')}</span><span className="text-xs text-muted-foreground"> / {fmtValue(max, unit)}</span></span>
        <span className="tnum text-xs text-muted-foreground">{Math.round(pct)}%</span>
      </div>
      <Progress value={pct} tone={tone as 'warning' | 'danger' | undefined} />
      <span className="truncate text-2xs text-muted-foreground">{sub}</span>
    </div>
  )
}

function DataTable({ d }: { d: Data }) {
  let head: string[] = []
  let rows: string[][] = []
  if (d.kind === 'ts') {
    head = ['时间', ...d.series.map((s) => s.name)]
    rows = d.labels.map((l, i) => [l.l, ...d.series.map((s) => fmtValue(s.data[i], d.unit))]).reverse()
  } else if (d.kind === 'cat') {
    const t = d.items.reduce((s, i) => s + i.value, 0)
    head = ['名称', '数值', '占比']
    rows = sortCat(d).map((i) => [i.name, fmtValue(i.value, d.unit), t ? Math.round((i.value / t) * 100) + '%' : '—'])
  } else if (d.kind === 'value') {
    head = ['项目', '数值']
    rows = [[d.label, fmtValue(d.value, d.unit)], ['总量', fmtValue(d.max, d.unit)]]
  }
  return (
    <div className="h-full overflow-auto">
      <table className="w-full text-xs">
        <thead className="sticky top-0 bg-card"><tr className="border-b text-left text-muted-foreground">{head.map((h, i) => <th key={i} className={cn('py-1 font-medium', i > 0 && 'text-right')}>{h}</th>)}</tr></thead>
        <tbody>{rows.map((r, i) => <tr key={i} className="border-b last:border-0">{r.map((c, j) => <td key={j} className={cn('py-1', j > 0 && 'tnum text-right')}>{c}</td>)}</tr>)}</tbody>
      </table>
    </div>
  )
}

const LIST_ICON: Record<string, React.ComponentType<{ className?: string }>> = {
  alert: AlertTriangle, up: ArrowUpCircle, package: Package, wrench: Wrench, unlink: Unlink, loader: Loader2, bell: Bell, history: History,
}

function ListWidget({ d, onTask }: { d: List; onTask?: (t: NonNullable<ListItem['task']>) => void }) {
  if (!d.items.length) {
    return <div className="flex h-full flex-col items-center justify-center gap-1.5 text-xs text-muted-foreground"><CheckCircle2 className="size-5 text-success" />{d.empty}</div>
  }
  if (d.type === 'events') {
    return (
      <ol className="flex h-full flex-col gap-2.5 overflow-y-auto">
        {d.items.map((it) => {
          const Icon = LIST_ICON[it.icon] || History
          return (
            <li key={it.key} className="flex items-start gap-2 text-xs">
              <Icon className={cn('mt-px size-3.5 shrink-0', it.tone === 'danger' ? 'text-danger' : 'text-muted-foreground')} />
              {it.href ? <a href={it.href} className="min-w-0 flex-1 truncate hover:underline">{it.title}{it.sub && <span className="text-muted-foreground"> · {it.sub}</span>}</a> : <span className="min-w-0 flex-1 truncate">{it.title}{it.sub && <span className="text-muted-foreground"> · {it.sub}</span>}</span>}
              <span className="tnum shrink-0 text-2xs text-muted-foreground">{it.time}</span>
            </li>
          )
        })}
      </ol>
    )
  }
  return (
    <div className="flex h-full flex-col divide-y overflow-y-auto">
      {d.items.map((it) => {
        const Icon = LIST_ICON[it.icon] || Bell
        return (
          <div key={it.key} className="flex items-center gap-2.5 py-2 first:pt-0">
            <div className={cn('flex size-7 shrink-0 items-center justify-center rounded-md border', it.tone === 'danger' && 'border-danger/30 bg-danger/10 text-danger', it.tone === 'warning' && 'border-warning/30 bg-warning/10 text-warning')}>
              <Icon className="size-3.5" />
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-xs font-medium">{it.title}</div>
              {it.sub && <div className="truncate text-2xs text-muted-foreground" title={it.sub}>{it.sub}</div>}
            </div>
            {it.action && (
              <Button variant="outline" size="sm" onClick={() => (it.task && onTask ? onTask(it.task) : it.href && go(it.href))}>{it.action}</Button>
            )}
          </div>
        )
      })}
    </div>
  )
}

export function Viz({ type, d, onTask }: { type: string; d: Data; onTask?: (t: NonNullable<ListItem['task']>) => void }) {
  if (d.kind === 'none') return <div className="flex h-full items-center justify-center text-center text-xs text-muted-foreground">{d.text}</div>
  switch (type) {
    case 'area':
    case 'line':
    case 'column':
      if (d.kind === 'cat') return <CatColumn d={d} />
      if (d.kind === 'ts') return <TimeChart d={d} type={type} />
      break
    case 'hbar': if (d.kind === 'cat') return <HBar d={d} />; break
    case 'segbar': if (d.kind === 'cat') return <SegBar d={d} />; break
    case 'donut': if (d.kind === 'cat') return <Donut d={d} />; break
    case 'kpi': if (d.kind === 'cat') return <Kpi d={d} />; break
    case 'stat': if (d.kind === 'ts' || d.kind === 'value') return <Stat d={d} />; break
    case 'meter': if (d.kind === 'ts' || d.kind === 'value') return <Meter d={d} />; break
    case 'table': return <DataTable d={d} />
    case 'list': if (d.kind === 'list') return <ListWidget d={d} onTask={onTask} />; break
  }
  return null
}
