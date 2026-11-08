// 弹窗和侧边抽屉。没有用 Radix：它锁滚动时会往页面里插 <style>，被控制台的 CSP（style-src 'self'）拦下。
// 这里锁滚动只改 body 的 style 属性（脚本写法，CSP 允许），打开时把焦点放进弹窗，关闭时还回去。
import * as React from 'react'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useEscape, useLockScroll } from '@/lib/hooks'

function useFocusIn(open: boolean, ref: React.RefObject<HTMLElement>) {
  React.useEffect(() => {
    if (!open) return
    const before = document.activeElement as HTMLElement | null
    const el = ref.current
    const first = el?.querySelector<HTMLElement>('[data-autofocus], input:not([disabled]), select, textarea, button:not([data-close])')
    ;(first || el)?.focus({ preventScroll: true })
    return () => before?.focus?.({ preventScroll: true })
  }, [open, ref])
}

// 焦点在弹窗里循环，Tab 不会跑到后面的页面上。
function trapTab(e: React.KeyboardEvent<HTMLElement>) {
  if (e.key !== 'Tab') return
  const items = Array.from(e.currentTarget.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'))
  if (items.length === 0) return
  const first = items[0]
  const last = items[items.length - 1]
  if (e.shiftKey && document.activeElement === first) {
    e.preventDefault()
    last.focus()
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault()
    first.focus()
  }
}

export interface DialogProps {
  open: boolean
  onClose: () => void
  children: React.ReactNode
  className?: string
  labelledBy?: string
  label?: string
}

export function Dialog({ open, onClose, children, className, labelledBy, label }: DialogProps) {
  const ref = React.useRef<HTMLDivElement>(null)
  useLockScroll(open)
  useEscape(open, onClose)
  useFocusIn(open, ref)
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center sm:items-center sm:p-4">
      <div className="absolute inset-0 animate-fade bg-black/50" onClick={onClose} aria-hidden="true" />
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        aria-label={label}
        tabIndex={-1}
        onKeyDown={trapTab}
        className={cn('pb-safe relative z-10 max-h-[92vh] w-full animate-in overflow-y-auto rounded-t-lg border bg-background p-5 shadow-lg outline-none sm:max-w-md sm:rounded-lg', className)}
      >
        <button type="button" data-close onClick={onClose} className="absolute right-3.5 top-3.5 rounded-sm opacity-60 hover:opacity-100" aria-label="关闭">
          <X className="size-4" />
        </button>
        {children}
      </div>
    </div>
  )
}

export function DialogHead({ id, title, desc }: { id?: string; title: React.ReactNode; desc?: React.ReactNode }) {
  return (
    <div className="mb-4 flex flex-col gap-1 pr-6">
      <h2 id={id} className="text-base font-semibold">{title}</h2>
      {desc && <p className="text-xs text-muted-foreground">{desc}</p>}
    </div>
  )
}

export function Sheet({ open, onClose, side = 'right', children, className, label }: { open: boolean; onClose: () => void; side?: 'left' | 'right'; children: React.ReactNode; className?: string; label?: string }) {
  const ref = React.useRef<HTMLDivElement>(null)
  useLockScroll(open)
  useEscape(open, onClose)
  useFocusIn(open, ref)
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50">
      <div className="absolute inset-0 animate-fade bg-black/50" onClick={onClose} aria-hidden="true" />
      <div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        tabIndex={-1}
        onKeyDown={trapTab}
        className={cn(
          'absolute inset-y-0 flex w-[92%] flex-col overflow-y-auto border bg-background shadow-lg outline-none sm:max-w-md',
          side === 'right' ? 'right-0 animate-slide border-l' : 'left-0 animate-slide-left border-r',
          className,
        )}
      >
        <button type="button" data-close onClick={onClose} className="absolute right-3.5 top-3.5 z-10 rounded-sm opacity-60 hover:opacity-100" aria-label="关闭">
          <X className="size-4" />
        </button>
        {children}
      </div>
    </div>
  )
}
