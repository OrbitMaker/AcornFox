// 控制台里反复用到的小块：标志、应用图块、状态、交付方式、页头、键值行、步骤列表。
import * as React from 'react'
import { createPortal } from 'react-dom'
import { Check, CircleDashed, CircleX, PlayCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { abbr, catalogAppOf, hue } from '@/lib/catalog'
import type { AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { STATUS, type AppStatus } from '@/lib/appstatus'
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

const TILE_SIZE = { xs: 'size-6 text-2xs rounded', sm: 'size-7 text-2xs rounded-md', md: 'size-8 text-xs rounded-md', lg: 'size-11 text-sm rounded-lg' }

// iconSrc 把目录图标转成 <img> 用的 data: 地址。svg 是 SVG 文字，png 是 base64；
// 没写类型时只认 SVG 文字（以 < 开头），其他不认识的返回 undefined，图块退回名称缩写。
export function iconSrc(icon?: string, type?: string): string | undefined {
  if (!icon) return undefined
  if (type === 'png') return /^[A-Za-z0-9+/=\s]+$/.test(icon) ? 'data:image/png;base64,' + icon.replace(/\s+/g, '') : undefined
  if (type === 'svg' || (!type && icon.trimStart().startsWith('<'))) return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(icon)
  return undefined
}

// Tile：应用图块。有目录图标时用 <img> 显示（SVG 里的脚本不会执行）；没有或不认识就显示名称缩写。
export function Tile({ id, name, icon, iconType, size = 'md' }: { id: string; name: string; icon?: string; iconType?: string; size?: keyof typeof TILE_SIZE }) {
  const src = iconSrc(icon, iconType)
  if (src) {
    return (
      <div className={cn('flex shrink-0 items-center justify-center overflow-hidden border bg-background', TILE_SIZE[size])}>
        <img alt="" className="size-[72%]" src={src} />
      </div>
    )
  }
  return (
    <div className={cn('tile flex shrink-0 items-center justify-center border font-semibold', TILE_SIZE[size])} style={{ '--tile-h': String(hue(id)) } as React.CSSProperties} aria-hidden="true">
      {abbr(name)}
    </div>
  )
}

// AppTile：我的应用的图块，从应用中心装的用目录里的图标。
export function AppTile({ a, size = 'md' }: { a: AppView; size?: keyof typeof TILE_SIZE }) {
  const { catalog } = useConsole()
  const c = catalogAppOf(catalog, a)
  return <Tile id={a.catalog_id || a.name} name={a.name} icon={c?.icon} iconType={c?.icon_type} size={size} />
}

export function StatusBadge({ s }: { s: AppStatus }) {
  const { text, tone } = STATUS[s]
  return (
    <span className={cn('inline-flex items-center gap-1 whitespace-nowrap text-2xs font-medium', tone === 'success' && 'text-success', tone === 'danger' && 'text-danger', tone === 'warning' && 'text-warning', tone === 'muted' && 'text-muted-foreground')}>
      <span className={cn('size-1.5 rounded-full', tone === 'success' && 'bg-success', tone === 'danger' && 'bg-danger', tone === 'warning' && 'animate-pulse bg-warning', tone === 'muted' && 'bg-muted-foreground')} />
      {text}
    </span>
  )
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
