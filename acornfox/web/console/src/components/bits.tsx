// 控制台里反复用到的小块：标志、应用图块、状态、交付方式、页头、键值行、步骤列表。
// 样稿第 10 版：AppTile 的缩写用显示名；新加 PlainBadge（给小白的状态：正常、停了、要处理，带“旧版本照常用”这类小字）。
import * as React from 'react'
import { createPortal } from 'react-dom'
import { Check, CircleDashed, CircleX, PlayCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { abbr, catalogAppOf, hue } from '@/lib/catalog'
import type { AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { STATUS, type AppStatus } from '@/lib/appstatus'
import { displayName, plainStatus, type PlainStatus } from '@/lib/display'
import { vtKey } from '@/lib/continuity'
import { Badge } from './ui/badge'
import { Spinner } from './ui/controls'

const LOGO_BODY = [[12, 34], [32, 34], [52, 34], [72, 34], [92, 34], [12, 54], [32, 54], [52, 54], [72, 54], [92, 54], [32, 74], [52, 74], [72, 74]]
const LOGO_AMBER = [[12, 14], [92, 14], [52, 94]]

export function Logo({ className = 'size-6' }: { className?: string }) {
  return (
    <svg viewBox="0 0 120 120" className={className} role="img" aria-label="AcornFox">
      {LOGO_BODY.map(([x, y]) => <rect key={x + '-' + y} x={x} y={y} width="16" height="16" rx="3" className="fill-[hsl(var(--logo))]" />)}
      {LOGO_AMBER.map(([x, y]) => <rect key={'a' + x + '-' + y} x={x} y={y} width="16" height="16" rx="3" fill="#E08A2E" />)}
    </svg>
  )
}

// 图块的大小跟着“大小档位”走（index.css 的 --tile-*）。
const TILE_SIZE = { xs: 'size-tile-xs text-2xs rounded', sm: 'size-tile-sm text-2xs rounded-md', md: 'size-tile-md text-xs rounded-md', lg: 'size-tile-lg text-sm rounded-lg' }

// VtName：能在列表和详情之间带过去的名字（界面连续性）。包一层贴着字的小块，过渡时只放大这一块。
export function VtName({ k, children }: { k: string; children: React.ReactNode }) {
  return <span data-vt-name={k} className="inline-block max-w-full truncate align-bottom">{children}</span>
}

// iconSrc 把目录图标转成 <img> 用的 data: 地址。svg 是 SVG 文字，png 是 base64；
// 没写类型时只认 SVG 文字（以 < 开头），其他不认识的返回 undefined，图块退回名称缩写。
export function iconSrc(icon?: string, type?: string): string | undefined {
  if (!icon) return undefined
  if (type === 'png') return /^[A-Za-z0-9+/=\s]+$/.test(icon) ? 'data:image/png;base64,' + icon.replace(/\s+/g, '') : undefined
  if (type === 'svg' || (!type && icon.trimStart().startsWith('<'))) return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(icon)
  return undefined
}

// Tile：应用图块。有目录图标时用 <img> 显示（SVG 里的脚本不会执行）；没有或不认识就显示名称缩写。
// vt：界面连续性的键（lib/continuity.ts 的 vtKey）；给了就能在列表和详情之间带过去。
export function Tile({ id, name, icon, iconType, size = 'md', vt }: { id: string; name: string; icon?: string; iconType?: string; size?: keyof typeof TILE_SIZE; vt?: string }) {
  const src = iconSrc(icon, iconType)
  if (src) {
    return (
      <div data-vt-icon={vt} className={cn('flex shrink-0 items-center justify-center overflow-hidden border bg-background', TILE_SIZE[size])}>
        <img alt="" className="size-[72%]" src={src} />
      </div>
    )
  }
  return (
    <div data-vt-icon={vt} className={cn('tile flex shrink-0 items-center justify-center border font-semibold', TILE_SIZE[size])} style={{ '--tile-h': String(hue(id)) } as React.CSSProperties} aria-hidden="true">
      {abbr(name)}
    </div>
  )
}

// AppTile：我的应用的图块，从应用中心装的用目录里的图标。
export function AppTile({ a, size = 'md', vt }: { a: AppView; size?: keyof typeof TILE_SIZE; vt?: boolean }) {
  const { catalog } = useConsole()
  const c = catalogAppOf(catalog, a)
  return <Tile id={a.catalog_id || a.name} name={displayName(catalog, a)} icon={c?.icon} iconType={c?.icon_type} size={size} vt={vt ? vtKey('app', a.name) : undefined} />
}

// 状态的颜色（样稿第 11 版）：正常绿、停了灰、要处理琥珀；busy（安装中、更新中）是灰色，圆点换成转圈。
export type StatusTone = 'success' | 'muted' | 'warning' | 'danger' | 'busy'
export const TONE_TEXT: Record<StatusTone, string> = { success: 'text-success', danger: 'text-danger', warning: 'text-warning', muted: 'text-muted-foreground', busy: 'text-muted-foreground' }
const TONE_DOT: Record<StatusTone, string> = { success: 'bg-success', danger: 'bg-danger', warning: 'bg-warning', muted: 'bg-muted-foreground', busy: '' }

// StatusDot：状态前面的小圆点；busy 时是一个转圈。
export function StatusDot({ tone }: { tone: StatusTone }) {
  if (tone === 'busy') return <Spinner className="size-2.5" />
  return <span className={cn('size-1.5 shrink-0 rounded-full', TONE_DOT[tone])} />
}

export function StatusBadge({ s }: { s: AppStatus }) {
  const { text, tone } = STATUS[s]
  return (
    <span className={cn('inline-flex items-center gap-1 whitespace-nowrap text-2xs font-medium', TONE_TEXT[tone])}>
      <StatusDot tone={tone} />
      {text}
    </span>
  )
}

// PlainBadge：给小白看的状态（lib/display.ts 的 plainStatus 算出来的）。withNote 时把“旧版本照常用”这类小字接在后面。
export function PlainBadge({ p, withNote, className }: { p: PlainStatus; withNote?: boolean; className?: string }) {
  const { text, tone, note } = p
  return (
    <span className={cn('inline-flex items-center gap-1 whitespace-nowrap text-2xs font-medium', TONE_TEXT[tone], className)}>
      <StatusDot tone={tone} />
      {text}
      {withNote && note && <span className="truncate font-normal text-muted-foreground">· {note}</span>}
    </span>
  )
}

// AppPlainBadge：一个应用的状态，直接从应用和推出来的状态算。
export function AppPlainBadge({ a, st, withNote, className }: { a: AppView; st: AppStatus; withNote?: boolean; className?: string }) {
  return <PlainBadge p={plainStatus(a, st)} withNote={withNote} className={className} />
}

export function TierBadge({ tier }: { tier: string }) {
  return tier === 'steps' ? (
    <Badge variant="outline"><PlayCircle className="size-3" />几步 · 有视频</Badge>
  ) : (
    <Badge variant="success">全自动</Badge>
  )
}

// 一级页面不再有自己的大标题：页面名只在顶栏写一次（顶栏的面包屑）。
// PageHead 把这一页的数量、状态（meta）和页面自己的按钮（children）送到顶栏里：
//   窗口够宽（≥1024px）而且外面有顶栏时，用传送门画进顶栏的两个位置（PageSlots 由外壳提供）；
//   窗口窄，或者单独渲染这一页（测试）时，就在页面最上面画成一行。两种情况下都不再画页面名。
// actionsClassName：按钮区的额外类名（只在页面里画成一行时用），例如手机宽度下让按钮占满一行。
export const PageSlots = React.createContext<{ meta: HTMLElement | null; actions: HTMLElement | null }>({ meta: null, actions: null })

const WIDE = '(min-width: 1024px)'
function useWide(): boolean {
  const can = typeof window !== 'undefined' && typeof window.matchMedia === 'function'
  const [wide, setWide] = React.useState(() => can && window.matchMedia(WIDE).matches)
  React.useEffect(() => {
    if (!can) return
    const m = window.matchMedia(WIDE)
    const h = () => setWide(m.matches)
    h()
    m.addEventListener('change', h)
    return () => m.removeEventListener('change', h)
  }, [can])
  return wide
}

export function PageHead({ meta, children, actionsClassName }: { meta?: React.ReactNode; children?: React.ReactNode; actionsClassName?: string }) {
  const slots = React.useContext(PageSlots)
  const wide = useWide()
  const hasMeta = meta !== undefined && meta !== null && meta !== false
  if (wide && slots.meta && slots.actions) {
    return (
      <>
        {hasMeta && createPortal(<span data-testid="page-meta">{meta}</span>, slots.meta)}
        {children && createPortal(children, slots.actions)}
      </>
    )
  }
  if (!hasMeta && !children) return null
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2">
      {hasMeta && <span data-testid="page-meta" className="text-xs text-muted-foreground">{meta}</span>}
      <div className={cn('ml-auto flex flex-wrap items-center gap-2', actionsClassName)}>{children}</div>
    </div>
  )
}

export function KV({ k, children }: { k: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 py-1.5 text-sm">
      <span className="shrink-0 text-muted-foreground">{k}</span>
      <span className="min-w-0 truncate text-right">{children}</span>
    </div>
  )
}

export type StepState = 'pending' | 'running' | 'done' | 'failed' | 'skipped'

export function StepDot({ status }: { status: string }) {
  return (
    <span
      className={cn(
        'flex size-4 shrink-0 items-center justify-center rounded-full border text-2xs',
        status === 'done' && 'border-success bg-success text-background',
        status === 'running' && 'border-primary',
        status === 'failed' && 'border-danger bg-danger text-background',
      )}
    >
      {status === 'done' && <Check className="size-2.5" />}
      {status === 'running' && <Spinner className="size-2.5" />}
      {status === 'failed' && <CircleX className="size-2.5" />}
      {status === 'skipped' && <CircleDashed className="size-2.5 text-muted-foreground" />}
    </span>
  )
}

export function Empty({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn('rounded-lg border border-dashed p-8 text-center text-xs text-muted-foreground', className)}>{children}</div>
}

export function ErrorLine({ error, className }: { error: Error | null; className?: string }) {
  if (!error) return null
  return <p className={cn('text-xs text-danger', className)} role="alert">{error.message}</p>
}
