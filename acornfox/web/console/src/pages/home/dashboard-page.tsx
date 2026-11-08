// 首页看板：可编辑的网格（拖动排序、改宽高、移除、添加图表、每张图换图表方式），布局存在这个浏览器里。
// 数据只来自服务端真有的接口（见 lib/dashboard.ts 文件头）；时间范围只给服务端有的历史（1 小时、24 小时）。
// 接口：GET /v1/metrics/host（10 秒）、GET /v1/metrics/host/history?range=（30 秒）、GET /v1/metrics/apps/{app}（15 秒，只问在跑的应用）、
//       GET /v1/runtime/checks（60 秒）、GET /v1/apps/{app}/deployments?limit=5（60 秒）；应用、安装任务、资源用共享数据。
import * as React from 'react'
import {
  Activity, Bell, ChartArea, ChartBar, ChartColumn, ChartLine, ChartNoAxesGantt, ChartPie, Cpu, Gauge, GripVertical, HardDrive, Hash,
  History, LayoutGrid, List, MemoryStick, MoreHorizontal, Pencil, Plus, Table2, Trash2, ArrowLeft, ArrowRight,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { api, appPath } from '@/lib/api'
import { usePoll } from '@/lib/hooks'
import {
  DEFAULT_LAYOUT, HEIGHTS, RANGE_NAME, RANGE_TAB, SOURCES, TYPE_NAME, WIDTHS, defaultSize, dropWidget, loadLayout, moveWidget, rangeTabs,
  saveLayout, sourceUsable, typesForSource, type DashCtx, type RangeKey, type WidgetConf,
} from '@/lib/dashboard'
import type { AppMetrics, Deployment, HostHistory, HostView, RuntimeChecks } from '@/lib/types'
import { useConsole } from '@/state'
import { Button } from '@/components/ui/button'
import { Tabs } from '@/components/ui/controls'
import { Sheet } from '@/components/ui/dialog'
import { Dropdown, MenuItem, MenuLabel, MenuSep } from '@/components/ui/menu'
import { useToast } from '@/components/ui/toast'
import { Viz } from './viz'

const SOURCE_ICON = { cpu: Cpu, memory: MemoryStick, disk: HardDrive, grid: LayoutGrid, bell: Bell, history: History }
const TYPE_ICON: Record<string, React.ComponentType<{ className?: string }>> = {
  area: ChartArea, line: ChartLine, column: ChartColumn, stat: Hash, meter: Gauge, table: Table2, segbar: ChartNoAxesGantt, hbar: ChartBar, donut: ChartPie, kpi: Activity, list: List,
}

// useDashData：看板要的全部数据。
export function useDashData(range: RangeKey): DashCtx {
  const { apps, installing, resources, tasks } = useConsole()
  const host = usePoll((s) => api.get<HostView>('/v1/metrics/host', s), 10000)
  const history = usePoll((s) => api.get<HostHistory>('/v1/metrics/host/history?range=' + range, s).catch(() => null), 30000, [range])
  const running = (apps || []).filter((a) => a.observed_state === 'running').map((a) => a.name)
  const runningKey = running.join(',')
  const metrics = usePoll(async (s) => {
    const out: Record<string, AppMetrics> = {}
    await Promise.all(running.map((n) => api.get<AppMetrics>('/v1/metrics/apps/' + encodeURIComponent(n), s).then((m) => { out[n] = m }).catch(() => {})))
    return out
  }, 15000, [runningKey])
  const checks = usePoll((s) => api.get<RuntimeChecks>('/v1/runtime/checks', s).catch(() => null), 60000)
  const names = (apps || []).map((a) => a.name)
  const namesKey = names.join(',')
  const deployments = usePoll(async (s) => {
    const all = await Promise.all(names.map((n) => api.get<{ deployments: Deployment[] }>(appPath(n, '/deployments?limit=5'), s).then((b) => b.deployments || []).catch(() => [] as Deployment[])))
    return all.flat()
  }, 60000, [namesKey])
  return React.useMemo(() => ({
    now: new Date(), range, host: host.data, history: history.data, apps: apps || [], installing, metrics: metrics.data || {},
    resources, checks: checks.data, deployments: deployments.data || [], tasks,
  }), [range, host.data, history.data, apps, installing, metrics.data, resources, checks.data, deployments.data, tasks])
}

interface Drag { id: string | null; over: string | null; start: (id: string) => void; overAt: (id: string) => void; end: () => void; drop: (id: string) => void }

function Widget({ wd, ctx, edit, onChange, onRemove, onMove, drag, preview }: {
  wd: WidgetConf
  ctx: DashCtx
  edit?: boolean
  onChange?: (p: Partial<WidgetConf>) => void
  onRemove?: () => void
  onMove?: (dir: number) => void
  drag?: Drag
  preview?: boolean
}) {
  const src = SOURCES[wd.s]
  const { install } = useConsole()
  const d = React.useMemo(() => src.gen(ctx), [src, ctx])
  const types = typesForSource(wd.s, d)
  const t = types.includes(wd.t) ? wd.t : types[0]
  const meta = src.fixed !== undefined ? src.fixed : RANGE_NAME[ctx.range]
  const TypeIcon = TYPE_ICON[t] || ChartArea
  return (
    <div
      className={cn('wd group relative flex min-w-0 flex-col rounded-lg border bg-card p-3.5 transition-shadow', edit && 'border-dashed border-primary/40', drag?.over === wd.id && drag.id !== wd.id && 'ring-2 ring-primary', drag?.id === wd.id && 'opacity-40')}
      data-w={wd.w}
      data-h={wd.h}
      data-testid="widget"
      draggable={!!edit}
      onDragStart={(e) => {
        if (!edit || !drag) return
        e.dataTransfer.effectAllowed = 'move'
        try { e.dataTransfer.setData('text/plain', wd.id) } catch { /* 有的浏览器不允许 */ }
        drag.start(wd.id)
      }}
      onDragOver={(e) => { if (!edit || !drag) return; e.preventDefault(); drag.overAt(wd.id) }}
      onDrop={(e) => { if (!edit || !drag) return; e.preventDefault(); drag.drop(wd.id) }}
      onDragEnd={() => edit && drag?.end()}
    >
      <div className="mb-2 flex h-5 items-center gap-1.5">
        {edit && <span className="-ml-1 cursor-grab text-muted-foreground active:cursor-grabbing" title="拖动排序"><GripVertical className="size-3.5" /></span>}
        <span className="truncate text-xs font-medium">{src.name}</span>
        <span className="truncate text-2xs text-muted-foreground">{meta}</span>
        {!preview && (
          <div className={cn('ml-auto flex items-center gap-0.5', !edit && 'opacity-0 transition-opacity focus-within:opacity-100 group-hover:opacity-100 [@media(hover:none)]:opacity-100')}>
            {types.length > 1 && (
              <Dropdown width="w-36" trigger={(tg) => <Button variant="ghost" size="icon-xs" onClick={tg} aria-label="换图表方式" title="换图表方式"><TypeIcon className="size-3.5" /></Button>}>
                <MenuLabel>图表方式</MenuLabel>
                {types.map((k) => {
                  const I = TYPE_ICON[k] || ChartArea
                  return <MenuItem key={k} icon={<I />} active={k === t} onClick={() => onChange?.({ t: k })}>{TYPE_NAME[k]}</MenuItem>
                })}
              </Dropdown>
            )}
            <Dropdown width="w-44" trigger={(tg) => <Button variant="ghost" size="icon-xs" onClick={tg} aria-label="更多"><MoreHorizontal className="size-3.5" /></Button>}>
              <MenuLabel>宽度</MenuLabel>
              <div className="flex gap-0.5 px-1 pb-1">
                {WIDTHS.map(([w, l]) => <button key={w} type="button" onClick={() => onChange?.({ w })} className={cn('flex-1 rounded px-1 py-0.5 text-2xs hover:bg-accent', wd.w === w && 'bg-accent font-medium')}>{l}</button>)}
              </div>
              <MenuLabel>高度</MenuLabel>
              <div className="flex gap-0.5 px-1 pb-1">
                {HEIGHTS.map(([h, l]) => <button key={h} type="button" onClick={() => onChange?.({ h })} className={cn('flex-1 rounded px-1 py-0.5 text-2xs hover:bg-accent', wd.h === h && 'bg-accent font-medium')}>{l}</button>)}
              </div>
              <MenuSep />
              <MenuItem icon={<ArrowLeft />} onClick={() => onMove?.(-1)}>前移</MenuItem>
              <MenuItem icon={<ArrowRight />} onClick={() => onMove?.(1)}>后移</MenuItem>
              <MenuSep />
              <MenuItem icon={<Trash2 />} danger onClick={onRemove}>移除</MenuItem>
            </Dropdown>
          </div>
        )}
      </div>
      <div className="min-h-0 flex-1"><Viz type={t} d={d} onTask={(x) => install(x)} /></div>
    </div>
  )
}

export function DashboardPage() {
  const toast = useToast()
  const [layout, setLayoutRaw] = React.useState<WidgetConf[]>(() => loadLayout())
  const [range, setRange] = React.useState<RangeKey>('1h')
  const [edit, setEdit] = React.useState(false)
  const [adding, setAdding] = React.useState(false)
  const [dragId, setDragId] = React.useState<string | null>(null)
  const [over, setOver] = React.useState<string | null>(null)
  const ctx = useDashData(range)
  const ranges = rangeTabs(ctx.history)
  const setLayout = React.useCallback((f: (l: WidgetConf[]) => WidgetConf[]) => {
    setLayoutRaw((l) => {
      const n = f(l)
      saveLayout(n)
      return n
    })
  }, [])
  const drag: Drag = {
    id: dragId, over, start: setDragId, overAt: setOver,
    end: () => { setDragId(null); setOver(null) },
    drop: (target) => {
      if (dragId) setLayout((l) => dropWidget(l, dragId, target))
      setDragId(null)
      setOver(null)
    },
  }
  const upd = (id: string, p: Partial<WidgetConf>) => setLayout((l) => l.map((x) => (x.id === id ? { ...x, ...p } : x)))
  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <h1 className="mr-auto text-lg font-semibold">首页</h1>
        {ranges.length > 1 && <Tabs<RangeKey> value={range} onChange={setRange} items={ranges.map((r) => [r, RANGE_TAB[r]])} label="时间范围" />}
        {edit ? (
          <>
            <Button variant="outline" onClick={() => setAdding(true)}><Plus className="size-3.5" />添加图表</Button>
            <Button variant="ghost" onClick={() => { setLayout(() => DEFAULT_LAYOUT); toast('已恢复默认布局') }}>恢复默认</Button>
            <Button onClick={() => setEdit(false)}>完成</Button>
          </>
        ) : (
          <Button variant="outline" onClick={() => setEdit(true)}><Pencil className="size-3.5" />编辑</Button>
        )}
      </div>
      {ctx.history?.since && ranges.length > 0 && (
        <p className="-mt-2 mb-3 text-2xs text-muted-foreground">CPU、内存曲线从 {new Date(ctx.history.since).toLocaleString('zh-CN', { hour12: false })} 开始记录（只存在内存里，后台服务重启后从头记）</p>
      )}
      <div className="dash">
        {layout.map((wd) => (
          <Widget key={wd.id} wd={wd} ctx={ctx} edit={edit} drag={drag}
            onChange={(p) => upd(wd.id, p)}
            onRemove={() => setLayout((l) => l.filter((x) => x.id !== wd.id))}
            onMove={(dir) => setLayout((l) => moveWidget(l, wd.id, dir))} />
        ))}
        {edit && (
          <button type="button" onClick={() => setAdding(true)} className="wd flex flex-col items-center justify-center gap-1.5 rounded-lg border border-dashed text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-foreground" data-w="3" data-h="1">
            <Plus className="size-4" />添加图表
          </button>
        )}
      </div>
      <AddWidget open={adding} onClose={() => setAdding(false)} ctx={ctx}
        onAdd={(wd) => { setLayout((l) => [...l, { ...wd, id: 'w' + Date.now() }]); setAdding(false); setEdit(true); toast('已添加到首页') }} />
    </div>
  )
}

function AddWidget({ open, onClose, ctx, onAdd }: { open: boolean; onClose: () => void; ctx: DashCtx; onAdd: (w: Omit<WidgetConf, 'id'>) => void }) {
  const keys = Object.keys(SOURCES).filter((k) => sourceUsable(k, ctx.history))
  const [s, setS] = React.useState(keys[0] || 'appStatus')
  const [t, setT] = React.useState<string | null>(null)
  const [w, setW] = React.useState(3)
  const [h, setH] = React.useState(1)
  const d = React.useMemo(() => SOURCES[s].gen(ctx), [s, ctx])
  const types = typesForSource(s, d)
  const tt = t && types.includes(t) ? t : types[0]
  const pickS = (k: string) => {
    setS(k)
    setT(null)
    const sz = defaultSize(typesForSource(k, SOURCES[k].gen(ctx))[0])
    setW(sz.w)
    setH(sz.h)
  }
  const pickT = (k: string) => {
    setT(k)
    if (['stat', 'meter', 'kpi'].includes(k)) { setH(1); if (w > 4) setW(3) } else if (h === 1) setH(2)
  }
  React.useEffect(() => { if (open) pickS(keys[0] || 'appStatus') }, [open]) // eslint-disable-line react-hooks/exhaustive-deps
  const groups = [...new Set(keys.map((k) => SOURCES[k].group))]
  return (
    <Sheet open={open} onClose={onClose} className="sm:max-w-3xl" label="添加图表">
      <div className="border-b px-5 py-3"><h2 className="text-base font-semibold">添加图表</h2></div>
      <div className="flex min-h-0 flex-1 flex-col sm:flex-row">
        <div className="no-scrollbar flex gap-1 overflow-x-auto border-b p-2 sm:w-48 sm:shrink-0 sm:flex-col sm:overflow-y-auto sm:border-b-0 sm:border-r">
          {groups.map((g) => (
            <div key={g} className="contents sm:block">
              <div className="hidden px-2 pb-1 pt-2 text-2xs font-medium text-muted-foreground sm:block">{g}</div>
              {keys.filter((k) => SOURCES[k].group === g).map((k) => {
                const I = SOURCE_ICON[SOURCES[k].icon]
                return (
                  <button key={k} type="button" onClick={() => pickS(k)} className={cn('flex shrink-0 items-center gap-2 whitespace-nowrap rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent sm:w-full', s === k && 'bg-accent font-medium')}>
                    <I className="size-3.5 text-muted-foreground" />{SOURCES[k].name}
                  </button>
                )
              })}
            </div>
          ))}
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-4 p-5">
          <div>
            <div className="mb-2 text-xs font-medium">图表方式</div>
            <div className="flex flex-wrap gap-1.5">
              {types.map((k, i) => {
                const I = TYPE_ICON[k] || ChartArea
                return (
                  <button key={k} type="button" onClick={() => pickT(k)} className={cn('flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition-colors hover:bg-accent', tt === k && 'border-primary bg-primary/5 font-medium')}>
                    <I className="size-3.5" />{TYPE_NAME[k]}{i === 0 && <span className="text-2xs font-normal text-muted-foreground">推荐</span>}
                  </button>
                )
              })}
            </div>
          </div>
          <div className="flex flex-wrap gap-6">
            <div><div className="mb-2 text-xs font-medium">宽度</div><Tabs<string> value={String(w)} onChange={(v) => setW(Number(v))} items={WIDTHS.map(([x, l]) => [String(x), l])} label="宽度" /></div>
            <div><div className="mb-2 text-xs font-medium">高度</div><Tabs<string> value={String(h)} onChange={(v) => setH(Number(v))} items={HEIGHTS.map(([x, l]) => [String(x), l])} label="高度" /></div>
          </div>
          <div>
            <div className="mb-2 text-xs font-medium">预览</div>
            <div className="rounded-lg bg-muted/40 p-3">
              <div className="dash pv mx-auto" style={{ maxWidth: Math.max(260, (w / 12) * 560) + 'px' }}>
                <Widget wd={{ id: 'pv', s, t: tt, w, h }} ctx={ctx} preview />
              </div>
            </div>
          </div>
          <div className="mt-auto flex justify-end gap-2">
            <Button variant="outline" onClick={onClose}>取消</Button>
            <Button onClick={() => onAdd({ s, t: tt, w, h })}>添加</Button>
          </div>
        </div>
      </div>
    </Sheet>
  )
}
