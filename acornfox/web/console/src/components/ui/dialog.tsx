// 弹窗和侧边抽屉。没有用 Radix：它锁滚动时会往页面里插 <style>，被控制台的 CSP（style-src 'self'）拦下。
// 这里锁滚动只改 body 的 style 属性（脚本写法，CSP 允许），打开时把焦点放进弹窗，关闭时还回去。
// 样稿第 12 版：有离场动画（lib/hooks.ts 的 usePresence）：关的时候里面的内容先留着，背景淡出、面板原路退回，播完才拿掉，
// 这期间不挡点击。宽屏上弹窗从点它的那个按钮的方向长出来（useOrigin：打开那一刻焦点所在的元素），没有就从正中；
// 窄屏从底下升上来；抽屉从它贴着的那一边进出。弹窗圆角 16，关闭钮离边 8、圆角 8。
import * as React from 'react'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useEscape, useLockScroll, usePresence } from '@/lib/hooks'

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

// useOrigin：弹窗从哪儿长出来。打开那一刻记下焦点所在的元素（就是点的那个按钮），面板摆好以后把它的中心换算成面板自己的坐标，
// 写到 --ov-origin 上（style 属性的脚本写法，CSP 允许）。没有按钮（快捷键打开）时不写，从正中。
function useOrigin(open: boolean, ref: React.RefObject<HTMLElement>) {
  const trigger = React.useRef<DOMRect | null>(null)
  if (!open) trigger.current = null
  else if (!trigger.current && typeof document !== 'undefined') {
    const a = document.activeElement
    trigger.current = a && a !== document.body && a instanceof HTMLElement ? a.getBoundingClientRect() : new DOMRect()
  }
  React.useLayoutEffect(() => {
    const el = ref.current
    const t = trigger.current
    if (!open || !el || !t || (!t.width && !t.height)) return
    const r = el.getBoundingClientRect()
    if (!r.width) return
    el.style.setProperty('--ov-origin', Math.round(t.left + t.width / 2 - r.left) + 'px ' + Math.round(t.top + t.height / 2 - r.top) + 'px')
    return () => { el.style.removeProperty('--ov-origin') }
  }, [open, ref])
}

// useKept：关的时候把最后一次打开时的内容留着（很多弹窗关掉的同时内容就变空了），离场动画才有东西可播。
function useKept(open: boolean, children: React.ReactNode) {
  const kept = React.useRef(children)
  if (open) kept.current = children
  return open ? children : kept.current
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
  useLockScroll(open)
  useEscape(open, onClose)
  const p = usePresence(open)
  const ref = p.ref
  useOrigin(open, ref)
  useFocusIn(open, ref)
  const body = useKept(open, children)
  if (!p.mounted) return null
  return (
    <div className={cn('fixed inset-0 z-50 flex items-end justify-center sm:items-center sm:p-4', !open && 'pointer-events-none')}>
      <div data-state={p.state} className="ov ov-fade absolute inset-0 bg-black/50" onClick={onClose} aria-hidden="true" />
      <div
        ref={ref}
        data-state={p.state}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        aria-label={label}
        tabIndex={-1}
        onKeyDown={trapTab}
        className={cn('ov ov-dialog pb-safe relative z-10 max-h-[92vh] w-full overflow-y-auto rounded-t-xl border bg-background p-5 shadow-lg outline-none sm:max-w-md sm:rounded-xl', className)}
      >
        <button type="button" data-close onClick={onClose} className="tap absolute right-2 top-2 inline-flex size-ctl-xs items-center justify-center rounded-md opacity-60 transition-[opacity,background-color] hover:bg-accent hover:opacity-100" aria-label="关闭">
          <X className="size-4" />
        </button>
        {body}
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
  useLockScroll(open)
  useEscape(open, onClose)
  const p = usePresence(open)
  const ref = p.ref
  useFocusIn(open, ref)
  const body = useKept(open, children)
  if (!p.mounted) return null
  return (
    <div className={cn('fixed inset-0 z-50', !open && 'pointer-events-none')}>
      <div data-state={p.state} className="ov ov-fade absolute inset-0 bg-black/50" onClick={onClose} aria-hidden="true" />
      <div
        ref={ref}
        data-state={p.state}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        tabIndex={-1}
        onKeyDown={trapTab}
        className={cn(
          'ov absolute inset-y-0 flex w-[92%] flex-col overflow-y-auto border bg-background shadow-lg outline-none sm:max-w-md',
          side === 'right' ? 'ov-right right-0 border-l' : 'ov-left left-0 border-r',
          className,
        )}
      >
        <button type="button" data-close onClick={onClose} className="tap absolute right-2 top-2 z-10 inline-flex size-ctl-xs items-center justify-center rounded-md opacity-60 transition-[opacity,background-color] hover:bg-accent hover:opacity-100" aria-label="关闭">
          <X className="size-4" />
        </button>
        {body}
      </div>
    </div>
  )
}
