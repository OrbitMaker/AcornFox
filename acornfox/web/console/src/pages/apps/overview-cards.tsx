// 应用概览页的四张卡片：访问（地址、三档、登录信息）、运行（近 30 天运行条）、资源（CPU、内存圆环）、版本（时间线）。
// 计算都在 lib/overview.ts；这里只画。数据来自现有接口，没有新接口：
//   运行条按“这次启动的时间”推（平台还不记每天有没有中断过），所以写的是“连续运行”，启动以前的格子是空的。
//   版本时间线读 GET /v1/apps/{app}/deployments（30 秒一次）；读不到时只画正在服务的那一版。
import { useMemo, useState } from 'react'
import { encode } from 'uqr'
import { ArrowDown, ArrowUp, Check, Copy, QrCode } from 'lucide-react'
import { api } from '@/lib/api'
import { liveSeq } from '@/lib/appstatus'
import { copyText } from '@/lib/clipboard'
import { coresText, mbText, sinceText } from '@/lib/format'
import { usePoll } from '@/lib/hooks'
import { ACCESS_LEVELS, accessInfo, cpuOfLimit, ringPct, uptimeStrip, versionTimeline } from '@/lib/overview'
import { appHref, go } from '@/lib/router'
import type { AppMetrics, AppView, Deployment, FirstLogin } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Dialog, DialogHead } from '@/components/ui/dialog'
import { useToast } from '@/components/ui/toast'
import { ExposureSwitch } from './access'
import { LoginLine, hasLoginFields } from './login-line'

function Head({ title, note }: { title: string; note?: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-2 pt-1">
      <span className="text-xs font-medium">{title}</span>
      {note && <span className="text-2xs text-muted-foreground">{note}</span>}
    </div>
  )
}

function Chip({ tone, children }: { tone: 'success' | 'warning' | 'muted'; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full border px-2 py-px text-2xs leading-[18px]">
      <span className={cn('size-1.5 rounded-full', tone === 'success' ? 'bg-success' : tone === 'warning' ? 'bg-warning' : 'bg-muted-foreground/50')} />
      {children}
    </span>
  )
}

const FOOT = 'mt-2.5 border-t pt-2'

// 运行：这次启动以来连续运行了多久，加近 30 天一天一格的运行条。
export function RunCard({ app: a }: { app: AppView }) {
  const running = !!a.observed?.running
  const up = useMemo(() => uptimeStrip(a.observed?.started_at, running), [a.observed?.started_at, running])
  const restarts = a.observed ? a.observed.restart_count : null
  return (
    <Card data-testid="ov-run" className="flex flex-col px-3.5 pb-3 pt-2">
      <Head title="运行" note="近 30 天" />
      <div className="mt-2 flex items-baseline gap-2">
        <span className="tnum text-[22px] font-semibold leading-7 tracking-tight">{running ? sinceText(a.observed?.started_at) : '没在运行'}</span>
        {running && <span className="text-xs text-muted-foreground">连续运行</span>}
      </div>
      <div className="mt-2.5 flex h-7 gap-[3px]" role="img" aria-label={running ? '近 30 天里有 ' + up.upDays + ' 天在运行' : '没在运行'}>
        {up.cells.map((c) => (
          <span key={c.date} data-up={c.up ? '1' : '0'} title={c.date + (c.up ? ' 在运行' : ' 没有记录')}
            className={cn('min-w-0 flex-1 rounded-sm', c.up ? 'bg-success' : 'bg-muted')} />
        ))}
      </div>
      <div className="tnum mt-1 flex justify-between text-2xs text-muted-foreground">
        <span>{up.cells[0]?.date}</span><span>今天</span>
      </div>
      <div className="flex-1" />
      <div className={cn('flex flex-wrap gap-1.5', FOOT)}>
        {running && !!a.live && <Chip tone="success">上线检查通过</Chip>}
        {restarts !== null && <Chip tone={restarts > 0 ? 'warning' : 'success'}>重启 {restarts} 次</Chip>}
        {!running && <Chip tone="muted">{a.desired === 'stopped' ? '已停止' : '没在运行'}</Chip>}
      </div>
    </Card>
  )
}

const R = 30
const CIRC = 2 * Math.PI * R

// 圆环：pct 是 null 时只画底圈、中间写“—”。
function Ring({ pct, color, label, sub }: { pct: number | null; color: string; label: string; sub: string }) {
  const arc = pct && pct > 0 ? Math.max(4, (CIRC * pct) / 100) : 0
  const text = pct === null ? '—' : (pct > 0 && pct < 1 ? '<1' : Math.round(pct)) + '%'
  return (
    <div className="flex flex-1 flex-col items-center" data-testid={'ov-ring-' + label}>
      <svg viewBox="0 0 76 76" className="size-[76px]" role="img" aria-label={label + ' ' + text}>
        <circle cx="38" cy="38" r={R} fill="none" strokeWidth="7" className="stroke-muted" />
        {arc > 0 && <circle cx="38" cy="38" r={R} fill="none" stroke={color} strokeWidth="7" strokeLinecap="round" strokeDasharray={arc.toFixed(2) + ' ' + CIRC.toFixed(2)} transform="rotate(-90 38 38)" />}
        <text x="38" y="43" textAnchor="middle" fontSize="15" fontWeight="600" className="tnum fill-foreground">{text}</text>
      </svg>
      <div className="mt-0.5 text-xs">{label}</div>
      <div className="tnum text-2xs text-muted-foreground">{sub}</div>
    </div>
  )
}

// 资源：CPU、内存各用了上限的多少（颜色和上面两张曲线图一样），底下一行进程数和网络收发。
export function ResourceCard({ app: a, metrics: m }: { app: AppView; metrics: AppMetrics | null }) {
  const ok = !!m?.available
  const cpu = ok && m.cpu_available !== false ? cpuOfLimit(m.cpu_percent, a.cpu_milli) : null
  const mem = ok ? ringPct(m.memory_usage_mb, a.memory_mb) : null
  return (
    <Card data-testid="ov-res" className="flex flex-col px-3.5 pb-3 pt-2">
      <Head title="资源" note="现在" />
      <div className="mt-2.5 flex">
        <Ring pct={cpu} color="var(--s2)" label="CPU" sub={a.cpu_milli ? '上限 ' + coresText(a.cpu_milli) : '没有设上限'} />
        <Ring pct={mem} color="var(--s1)" label="内存" sub={(ok ? mbText(m.memory_usage_mb) : '—') + ' / ' + mbText(a.memory_mb)} />
      </div>
      <div className="flex-1" />
      <div className={cn('tnum flex justify-between gap-2 text-2xs text-muted-foreground', FOOT)}>
        <span>{ok && m.pids !== undefined ? m.pids + ' 个进程' : '进程 —'}</span>
        <span className="inline-flex items-center gap-1">网络
          <ArrowDown className="size-3" aria-label="收" />{ok ? mbText(m.network_rx_mb) : '—'}
          <ArrowUp className="size-3" aria-label="发" />{ok ? mbText(m.network_tx_mb) : '—'}
        </span>
      </div>
    </Card>
  )
}

// 版本：最近几版的时间线，正在服务的那一版加粗；底下一行这一版从哪来。点卡片上的“共 N 版”去版本页签。
export function VersionCard({ app: a }: { app: AppView }) {
  const list = usePoll((s) => api.get<{ deployments: Deployment[] }>('/v1/apps/' + encodeURIComponent(a.name) + '/deployments?limit=20', s).then((b) => b.deployments || []).catch(() => null), 30000, [a.name])
  const seq = liveSeq(a)
  const nodes = useMemo(() => versionTimeline(list.data, seq), [list.data, seq])
  const total = Math.max(seq, ...(list.data || []).map((d) => d.seq), 0)
  const from = a.catalog_id
    ? (a.catalog_origin ? a.catalog_origin_name || a.catalog_origin : '应用中心') + ' · ' + a.catalog_id + (a.catalog_version ? ' · ' + a.catalog_version : '')
    : a.live?.source_text || ''
  return (
    <Card data-testid="ov-ver" className="flex flex-col px-3.5 pb-3 pt-2">
      <Head title="版本" note={total > 0 ? <button type="button" className="hover:text-foreground hover:underline" onClick={() => go(appHref(a.name, 'versions'))}>共 {total} 版</button> : undefined} />
      {nodes.length === 0 ? (
        <div className="flex flex-1 items-center justify-center py-6 text-xs text-muted-foreground">还没有上线的版本</div>
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
      <div className={cn('truncate text-2xs text-muted-foreground', FOOT)} title={from}>
        {from || '—'}
        {a.catalog_disconnected && <span className="ml-1.5 text-warning" title="照常运行，但不能再从这个私有目录安装或升级">目录已断开</span>}
      </div>
    </Card>
  )
}

// 二维码：在页面里自己画成 SVG，不联网、不用图片。
function QR({ text }: { text: string }) {
  // border 0：四周的留白自己画（pad），方便控制大小
  const cells = useMemo(() => encode(text, { ecc: 'M', border: 0 }).data, [text])
  const n = cells.length
  const pad = 2
  return (
    <svg data-testid="ov-qr" viewBox={`0 0 ${n + pad * 2} ${n + pad * 2}`} className="size-52 rounded-md" shapeRendering="crispEdges" role="img" aria-label={'二维码：' + text}>
      <rect width={n + pad * 2} height={n + pad * 2} fill="#fff" />
      {cells.map((row, r) => row.map((dark, c) => (dark ? <rect key={r + '-' + c} x={c + pad} y={r + pad} width="1" height="1" fill="#000" /> : null)))}
    </svg>
  )
}

// 访问：谁能打开这个应用。一行地址、复制、扫码和三档（能点，见 access.tsx）；下面一行左边是协议和域名
// （有首次登录信息时换成登录账号密码），右边是这一档谁能打开。“打开”只在名片上。
// 应用没有地址（只在内部网络）时不显示；只本机、要按说明打开的应用仍由 OpenHintBlock 负责。
export function AccessCard({ app: a, reload, login }: { app: AppView; reload: () => Promise<void>; login?: FirstLogin | null }) {
  const toast = useToast()
  const [qr, setQr] = useState(false)
  const [copied, setCopied] = useState(false)
  const x = accessInfo(a)
  if (!x) return null
  const level = ACCESS_LEVELS.find((l) => l.key === x.level)
  const copy = async () => {
    if (!(await copyText(x.url))) { toast('没能复制', '浏览器不允许，请手动选中复制', 'error'); return }
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1200)
  }
  return (
    <Card data-testid="ov-access" className="px-3.5 pb-3 pt-2">
      <Head title="访问" note="谁能打开这个应用" />
      <div className="mt-2 flex flex-wrap items-center gap-2.5">
        <span className="mono tnum mr-1 min-w-0 max-w-full truncate text-[17px] font-medium leading-7" title={x.url}>{x.address}</span>
        <Button size="sm" variant="outline" onClick={copy}>{copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}复制地址</Button>
        {x.openable && <Button size="sm" variant="outline" onClick={() => setQr(true)}><QrCode className="size-3.5" />手机扫码</Button>}
        <span className="flex-1" />
        {level && <ExposureSwitch app={a} reload={reload} />}
      </div>
      <div className="mt-2 flex flex-wrap justify-between gap-x-3 gap-y-1 text-2xs text-muted-foreground">
        {hasLoginFields(login) ? <LoginLine fl={login} /> : <span data-testid="ov-access-scheme">{x.scheme} · {x.domain ? '域名 ' + x.domain : '没有绑定域名'}</span>}
        {level && <span data-testid="ov-access-who">{level.who}</span>}
      </div>
      {x.lanURL && <div className="mono mt-0.5 truncate text-2xs text-muted-foreground" title={x.lanURL}>局域网 {x.lanURL.replace(/^https?:\/\//, '')}</div>}
      <Dialog open={qr} onClose={() => setQr(false)} labelledBy="ov-qr-title" className="sm:max-w-xs">
        <DialogHead id="ov-qr-title" title="用手机扫码打开" desc={x.level === 'public' ? '手机能连到这台机器就能打开' : '手机要和这台机器在同一个局域网里'} />
        <div className="flex flex-col items-center gap-2">
          {qr && <QR text={x.url} />}
          <code className="mono max-w-full truncate text-xs text-muted-foreground">{x.address}</code>
        </div>
      </Dialog>
    </Card>
  )
}
