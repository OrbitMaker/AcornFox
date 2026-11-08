// 小控件：分段标签、开关、下拉选择、键帽、提示条，样子照控制台样稿。
import * as React from 'react'
import { ChevronDown, Info } from 'lucide-react'
import { cn } from '@/lib/utils'

export function Tabs<K extends string>({ value, onChange, items, className, label }: {
  value: K
  onChange: (k: K) => void
  items: [K, React.ReactNode, boolean?][]
  className?: string
  label?: string
}) {
  return (
    <div role="tablist" aria-label={label} className={cn('no-scrollbar inline-flex h-8 max-w-full items-center overflow-x-auto rounded-md bg-muted p-0.5 text-muted-foreground', className)}>
      {items.map(([k, l, dot]) => (
        <button
          key={k}
          type="button"
          role="tab"
          aria-selected={value === k}
          onClick={() => onChange(k)}
          className={cn(
            'inline-flex h-7 items-center justify-center gap-1.5 whitespace-nowrap rounded px-2.5 text-xs font-medium transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
            value === k ? 'bg-background text-foreground shadow-sm' : 'hover:text-foreground',
          )}
        >
          {l}
          {dot && <span className="size-1.5 rounded-full bg-danger" />}
        </button>
      ))}
    </div>
  )
}

export function Switch({ checked, onChange, id, disabled, label }: { checked: boolean; onChange?: (v: boolean) => void; id?: string; disabled?: boolean; label?: string }) {
  return (
    <button
      id={id}
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange?.(!checked)}
      className={cn(
        'inline-flex h-[18px] w-8 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50',
        checked ? 'bg-primary' : 'bg-input',
      )}
    >
      <span className={cn('pointer-events-none block h-3.5 w-3.5 rounded-full bg-background shadow transition-transform', checked ? 'translate-x-3.5' : 'translate-x-0')} />
    </button>
  )
}

export function Select({ className, options, ...p }: React.SelectHTMLAttributes<HTMLSelectElement> & { options: [string, string][] }) {
  return (
    <div className={cn('relative', className)}>
      <select
        className="flex h-8 w-full appearance-none rounded-md border border-input bg-background pl-2.5 pr-7 text-sm focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:opacity-50"
        {...p}
      >
        {options.map(([v, l]) => (
          <option key={v} value={v}>{l}</option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2 top-2 size-4 text-muted-foreground" />
    </div>
  )
}

export function Kbd({ children }: { children: React.ReactNode }) {
  return <kbd className="pointer-events-none inline-flex h-[18px] select-none items-center rounded border bg-muted px-1 font-mono text-2xs text-muted-foreground">{children}</kbd>
}

export function Alert({ tone = 'default', icon, title, children, action }: {
  tone?: 'default' | 'danger' | 'warning'
  icon?: React.ReactNode
  title: React.ReactNode
  children?: React.ReactNode
  action?: React.ReactNode
}) {
  return (
    <div
      role={tone === 'default' ? 'status' : 'alert'}
      className={cn(
        'flex w-full flex-wrap items-center gap-3 rounded-lg border px-3.5 py-2.5 text-sm [&>svg]:size-4 [&>svg]:shrink-0',
        tone === 'danger' && 'border-danger/40 [&>svg]:text-danger',
        tone === 'warning' && 'border-warning/40 [&>svg]:text-warning',
      )}
    >
      {icon || <Info />}
      <div className="min-w-0 flex-1">
        <span className="font-medium">{title}</span>
        {children && <span className="text-muted-foreground"> · {children}</span>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  )
}

export function Spinner({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className={cn('size-3.5 shrink-0 animate-spin', className)} aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-6.22-8.56" />
    </svg>
  )
}
