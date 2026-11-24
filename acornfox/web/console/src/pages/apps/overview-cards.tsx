// 应用概览页下面并排的三张卡片：运行（近 30 天运行条）、资源（CPU、内存圆环）、版本。“谁能打开”卡片在 who-card.tsx。
// 版式是 11-15 定的，样稿第 10 版只去掉了小白用不上的小字（评估 E3）：上线检查、重启次数、进程数、网络收发、CPU 上限、来源。
// 计算都在 lib/overview.ts；这里只画。数据来自现有接口，没有新接口：
//   运行条按“这次启动的时间”推（平台还不记每天有没有中断过），所以写的是“连续运行”，启动以前的格子是空的。
//   版本：从应用中心装的应用只写应用自己的版本号和哪天装好（有新版本时多一句，点了去“更新”页签）；
//         自己部署的项目画最近几版的时间线（GET /v1/apps/{app}/deployments，30 秒一次；读不到时只画正在用的那一版）。
import { useMemo } from 'react'
import { api } from '@/lib/api'
import { liveSeq } from '@/lib/appstatus'
import { fromStore } from '@/lib/display'
import { mbText, sinceText } from '@/lib/format'
import { usePoll } from '@/lib/hooks'
import { cpuOfLimit, installedAt, plainWords, ringPct, uptimeStrip, versionTimeline } from '@/lib/overview'
import { appHref, go } from '@/lib/router'
import type { AppMetrics, AppView, Deployment } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { Sk, Loading } from '@/components/ui/skeleton'
import { SkRings, SkTimeline } from '@/components/skeletons'

function Head({ title, note }: { title: string; note?: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-2 pt-1">
      <span className="text-xs font-medium">{title}</span>
      {note && <span className="text-2xs text-muted-foreground">{note}</span>}
    </div>
  )
}

// 运行：这次启动以来连续运行了多久，加近 30 天一天一格的运行条。
export function RunCard({ app: a }: { app: AppView }) {
  const running = !!a.observed?.running
  const up = useMemo(() => uptimeStrip(a.observed?.started_at, running), [a.observed?.started_at, running])
  return (
    <Card data-testid="ov-run" className="flex flex-col px-3.5 pb-3 pt-2">
      <Head title="运行" note="近 30 天" />
      <div className="mt-2 flex items-baseline gap-2">
        <span className="tnum text-2xl font-semibold tracking-tight">{running ? sinceText(a.observed?.started_at) : '停了'}</span>
        {running && <span className="text-xs text-muted-foreground">连续运行</span>}
      </div>
      <div className="mt-2.5 flex h-7 gap-[3px]" role="img" aria-label={running ? '近 30 天里有 ' + up.upDays + ' 天在运行' : '停了'}>
        {up.cells.map((c) => (
          <span key={c.date} data-up={c.up ? '1' : '0'} title={c.date + (c.up ? ' 在运行' : ' 没有记录')}
            className={cn('min-w-0 flex-1 rounded-sm', c.up ? 'bg-success' : 'bg-muted')} />
        ))}
      </div>
      <div className="tnum mt-1 flex justify-between text-2xs text-muted-foreground">
        <span>{up.cells[0]?.date}</span><span>今天</span>
      </div>
    </Card>
  )
}

const R = 30
const CIRC = 2 * Math.PI * R

// 用到多少算“快满了”：圆环超过这个数才上琥珀色，平时是中性色（样稿第 11 版：正常的东西不上色）。
const RING_WARN = 80

// 圆环：pct 是 null 时只画底圈、中间写“—”。
function Ring({ pct, label, sub }: { pct: number | null; label: string; sub?: string }) {
  const color = pct !== null && pct > RING_WARN ? 'hsl(var(--warning))' : 'hsl(var(--foreground))'
  const arc = pct && pct > 0 ? Math.max(4, (CIRC * pct) / 100) : 0
  const text = pct === null ? '—' : (pct > 0 && pct < 1 ? '<1' : Math.round(pct)) + '%'
  return (
    <div className="flex flex-1 flex-col items-center" data-testid={'ov-ring-' + label}>
      <svg viewBox="0 0 76 76" className="size-donut" role="img" aria-label={label + ' ' + text}>
        <circle cx="38" cy="38" r={R} fill="none" strokeWidth="7" className="stroke-muted" />
        {arc > 0 && <circle cx="38" cy="38" r={R} fill="none" stroke={color} strokeWidth="7" strokeLinecap="round" strokeDasharray={arc.toFixed(2) + ' ' + CIRC.toFixed(2)} transform="rotate(-90 38 38)" />}
        <text x="38" y="43" textAnchor="middle" fontSize="15" fontWeight="600" className="tnum fill-foreground">{text}</text>
      </svg>
      <div className="mt-0.5 text-xs">{label}</div>
      {sub && <div className="tnum text-2xs text-muted-foreground">{sub}</div>}
    </div>
  )
}

// 资源：CPU、内存各用了最多能用的多少。内存下面一行“用了多少 / 最多多少”，CPU 不再写上限。
// loading：用量还没读到（第一次）时两个圆环是占位形状，不先画两个“—”（样稿第 14 版）。
export function ResourceCard({ app: a, metrics: m, loading }: { app: AppView; metrics: AppMetrics | null; loading?: boolean }) {
  const ok = !!m?.available
  const cpu = ok && m.cpu_available !== false ? cpuOfLimit(m.cpu_percent, a.cpu_milli) : null
  const mem = ok ? ringPct(m.memory_usage_mb, a.memory_mb) : null
  return (
    <Card data-testid="ov-res" className="flex flex-col px-3.5 pb-3 pt-2">
      <Head title="资源" note="现在" />
      <Loading when={!!loading} skeleton={<SkRings />}>
        {() => (
          <div className="mt-2.5 flex">
            <Ring pct={cpu} label="CPU" />
            <Ring pct={mem} label="内存" sub={(ok ? mbText(m.memory_usage_mb) : '—') + ' / ' + mbText(a.memory_mb)} />
          </div>
        )}
      </Loading>
    </Card>
  )
}

// 版本：从应用中心装的应用和自己部署的项目分开画（评估 E3、G1）。
export function VersionCard({ app: a }: { app: AppView }) {
  const list = usePoll((s) => api.get<{ deployments: Deployment[] }>('/v1/apps/' + encodeURIComponent(a.name) + '/deployments?limit=20', s).then((b) => b.deployments || []).catch(() => null), 30000, [a.name])
  const loading = list.loading && !list.data
  if (fromStore(a)) return <StoreVersion app={a} list={list.data} loading={loading} />
  return <DevVersions app={a} list={list.data} loading={loading} />
}

// 应用中心装的：大字写应用自己的版本号，下面“11-20 装好”；服务端告诉有新版本时多一句，点了去“更新”页签（没告诉就不写，不猜“已是最新”）。
function StoreVersion({ app: a, list, loading }: { app: AppView; list: Deployment[] | null | undefined; loading?: boolean }) {
  const when = installedAt(list, a.live)
  return (
    <Card data-testid="ov-ver" className="flex flex-col px-3.5 pb-3 pt-2">
      <Head title="版本" />
      <div className="mt-2 flex flex-1 flex-col justify-center">
        <span className="tnum text-2xl font-semibold tracking-tight">{a.catalog_version || '—'}</span>
        <span className="mt-1 flex h-[var(--lh-xs)] items-center text-xs text-muted-foreground">{loading ? <Sk className="h-2.5 w-20" /> : when ? when + ' 装好' : ''}</span>
      </div>
      {a.catalog_update && (
        <button type="button" className="tap-h mt-2.5 self-start text-2xs text-primary hover:underline" onClick={() => go(appHref(a.name, 'versions'))} data-testid="ov-ver-update">
          有新版本 {a.catalog_update}
        </button>
      )}
    </Card>
  )
}

// 自己部署的项目：最近几版的时间线，正在用的那一版加粗；点“共 N 版”去版本页签。底下一行这一版从哪来。
function DevVersions({ app: a, list, loading }: { app: AppView; list: Deployment[] | null | undefined; loading?: boolean }) {
  const seq = liveSeq(a)
  const nodes = useMemo(() => versionTimeline(list, seq), [list, seq])
  const total = Math.max(seq, ...(list || []).map((d) => d.seq), 0)
  const from = plainWords(a.live?.source_text)
  return (
    <Card data-testid="ov-ver" className="flex flex-col px-3.5 pb-3 pt-2">
      <Head title="版本" note={total > 0 ? <button type="button" className="hover:text-foreground hover:underline" onClick={() => go(appHref(a.name, 'versions'))}>共 {total} 版</button> : undefined} />
      {loading && nodes.length <= 1 ? (
        <SkTimeline />
      ) : nodes.length === 0 ? (
        <div className="flex flex-1 items-center justify-center py-6 text-xs text-muted-foreground">还没有装好的版本</div>
      ) : (
        <div className="relative mt-5 flex justify-between px-2">
          {nodes.length > 1 && <div className="absolute left-6 right-6 top-[9px] h-0.5 bg-border" aria-hidden="true" />}
          {nodes.map((n) => (
            <div key={n.seq} data-live={n.live ? '1' : '0'} className={cn('relative flex min-w-0 flex-col items-center', nodes.length === 1 && 'mx-auto')}>
              <span className="flex size-5 items-center justify-center">
                {n.live
                  ? <span className="flex size-[18px] items-center justify-center rounded-full bg-foreground"><span className="size-[7px] rounded-full bg-card" /></span>
                  : <span className={cn('size-3 rounded-full border-2 bg-card', n.failed ? 'border-danger' : 'border-muted-foreground')} />}
              </span>
              <span className={cn('tnum mt-2 text-xs', n.live ? 'font-semibold' : 'text-muted-foreground')}>第 {n.seq} 版</span>
              <span className={cn('tnum text-2xs', n.failed ? 'text-danger' : 'text-muted-foreground')}>{n.when}</span>
            </div>
          ))}
        </div>
      )}
      <div className="flex-1" />
      {from && <div className="mt-2.5 truncate border-t pt-2 text-2xs text-muted-foreground" title={from}>现在这一版的来源：{from}</div>}
    </Card>
  )
}
