// 下拉菜单：点按钮打开，点外面或按 Esc 关闭，点菜单项后自动关闭。
import * as React from 'react'
import { Check } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useEscape } from '@/lib/hooks'

export function Dropdown({ trigger, children, align = 'end', width = 'w-48' }: {
  trigger: (toggle: () => void, open: boolean) => React.ReactNode
  children: React.ReactNode
  align?: 'start' | 'end'
  width?: string
}) {
  const [open, setOpen] = React.useState(false)
  const ref = React.useRef<HTMLDivElement>(null)
  useEscape(open, () => setOpen(false))
  React.useEffect(() => {
    if (!open) return
    const h = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [open])
  return (
    <div className="relative" ref={ref}>
      {trigger(() => setOpen((o) => !o), open)}
      {open && (
        <div
          role="menu"
          className={cn('absolute top-full z-40 mt-1 animate-in overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md', align === 'end' ? 'right-0' : 'left-0', width)}
          onClick={() => setOpen(false)}
        >
          {children}
        </div>
      )}
    </div>
  )
}

export function MenuItem({ icon, children, onClick, danger, hint, active, href, external }: {
  icon?: React.ReactNode
  children: React.ReactNode
  onClick?: (e: React.MouseEvent) => void
  danger?: boolean
  hint?: string
  active?: boolean
  href?: string
  external?: boolean
}) {
  const cls = cn(
    'flex w-full select-none items-center gap-2 rounded-sm px-2 py-1 text-left text-sm outline-none transition-colors hover:bg-accent focus-visible:bg-accent [&_svg]:size-3.5 [&_svg]:text-muted-foreground',
    danger && 'text-danger [&_svg]:text-danger',
    active && 'font-medium',
  )
  const inner = (
    <>
      {icon}
      <span className="flex-1">{children}</span>
      {active && <Check />}
      {hint && <span className="text-2xs text-muted-foreground">{hint}</span>}
    </>
  )
  if (href) {
    return (
      <a role="menuitem" href={href} className={cls} onClick={onClick} {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>
        {inner}
      </a>
    )
  }
  return (
    <button role="menuitem" type="button" className={cls} onClick={onClick}>
      {inner}
    </button>
  )
}

export function MenuLabel({ children }: { children: React.ReactNode }) {
  return <div className="px-2 py-1 text-2xs font-medium text-muted-foreground">{children}</div>
}

export function MenuSep() {
  return <div className="-mx-1 my-1 h-px bg-border" />
}
