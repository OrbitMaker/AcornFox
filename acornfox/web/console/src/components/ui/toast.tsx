// 提示（toast）：回应用户刚做的一件事，出现在内容区底部正中，一次只有一条，新的来了旧的让位。
// （放在内容上方正中会盖住应用详情名片那一行的“打开”，所以在底部正中：页面底下本来就留着空，不挡主要按钮。）
// 分三种（照“提示还是通知”那一期）：
//   状态  toast('已复制')、toast('已保存', 'n8n')：一个对勾加一句话，没有按钮，3～5 秒自己走（字多留得久一点），进场时一道亮带扫过一次。
//   能撤销  toast('已停止', 'n8n', { undo: () => … })：事情已经做了，后面跟一个“撤销”和关闭；图标是一圈在走的倒计时（里面是剩几秒），
//          至少留 6 秒；点了撤销，原地换成“已撤销”，1.5 秒后走。只给真的能原样做回来的操作用。
//   出错  toast('没能复制', '…', 'error')：不自己消失，等人点关闭。能留在出错那一项上的（表单、某一行的保存）不要用提示，
//          用那一项旁边的 <ErrorLine>；这里只收没有固定位置的出错（复制失败、读不到密码）。
// 鼠标停在提示上或焦点在里面时，倒计时暂停。有进场和离场（index.css 的 .toast，时长和曲线用动画档位）。
// 不属于提示的：别人或系统引起的事（应用出问题了、磁盘快满了）是“通知”，在铃铛的“需要你处理”里，一直留到处理完。
import * as React from 'react'
import { Check, CircleAlert, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { usePresence } from '@/lib/hooks'

export interface ToastOpt {
  tone?: 'ok' | 'error'
  // 能撤销：点“撤销”时调它（把刚才那一下原样做回来）；抛错时提示换成“没能撤销”
  undo?: () => void | Promise<void>
  // 撤销以后说的话，默认“已撤销”
  undone?: string
}
type Item = { id: number; title: string; desc?: string; tone: 'ok' | 'error'; undo?: ToastOpt['undo']; undone?: string }
export type ToastFn = (title: string, desc?: string, opt?: 'ok' | 'error' | ToastOpt) => void

const Ctx = React.createContext<ToastFn>(() => {})
export const useToast = () => React.useContext(Ctx)

const UNDO_MS = 6000
const UNDONE_MS = 1500
// 状态提示留多久：3 秒起，字多一点留久一点，最多 5 秒。
export function statusMs(title: string, desc?: string): number {
  return Math.max(3000, Math.min(5000, 2200 + (title.length + (desc?.length || 0)) * 70))
}

export function ToastProvider({ children }: { children: React.ReactNode }) {
  // items 里最多两条：最新的一条，和正在离场的上一条。live 是现在开着的那一条。
  const [items, setItems] = React.useState<Item[]>([])
  const [live, setLive] = React.useState(0)
  const seq = React.useRef(0)
  const toast = React.useCallback<ToastFn>((title, desc, opt) => {
    const o: ToastOpt = typeof opt === 'string' ? { tone: opt } : opt || {}
    const id = ++seq.current
    setItems((x) => [...x.slice(-1), { id, title, desc, tone: o.tone || 'ok', undo: o.undo, undone: o.undone }])
    setLive(id)
  }, [])
  const close = React.useCallback((id: number) => setLive((cur) => (cur === id ? 0 : cur)), [])
  const gone = React.useCallback((id: number) => setItems((x) => x.filter((i) => i.id !== id)), [])
  const cur = items.find((i) => i.id === live)
  return (
    <Ctx.Provider value={toast}>
      {children}
      <div className="toasts pointer-events-none fixed inset-x-0 z-[70] flex justify-center px-4 md:left-48" data-testid="toasts">
        {/* 读屏用的那一块一直在页面里，只换里面的字（每次新插一块带字的区域，常常不会被读出来）。 */}
        <div role="status" aria-live="polite" className="sr-only">{cur && cur.tone !== 'error' ? cur.title + (cur.desc ? '，' + cur.desc : '') : ''}</div>
        {items.map((i) => <ToastView key={i.id} item={i} open={i.id === live} onClose={() => close(i.id)} onGone={() => gone(i.id)} />)}
      </div>
    </Ctx.Provider>
  )
}

function ToastView({ item, open, onClose, onGone }: { item: Item; open: boolean; onClose: () => void; onGone: () => void }) {
  const p = usePresence(open)
  const goneRef = React.useRef(onGone)
  goneRef.current = onGone
  React.useEffect(() => { if (!p.mounted) goneRef.current() }, [p.mounted])
  const closeRef = React.useRef(onClose)
  closeRef.current = onClose

  // phase：idle 平时；undoing 正在撤销；undone 撤销好了；failed 没能撤销
  const [phase, setPhase] = React.useState<'idle' | 'undoing' | 'undone' | 'failed'>('idle')
  const error = item.tone === 'error' || phase === 'failed'
  const canUndo = !!item.undo && phase === 'idle'
  const total = error || phase === 'undoing' ? 0 : phase === 'undone' ? UNDONE_MS : item.undo ? UNDO_MS : statusMs(item.title, item.desc)
  const [left, setLeft] = React.useState(total)
  const paused = React.useRef(false)
  React.useEffect(() => {
    if (!open || !total) return
    let rem = total
    let last = Date.now()
    setLeft(total)
    const t = window.setInterval(() => {
      const now = Date.now()
      if (!paused.current) rem -= now - last
      last = now
      setLeft(Math.max(0, rem))
      if (rem <= 0) closeRef.current()
    }, 200)
    return () => window.clearInterval(t)
  }, [open, total])

  const undo = async () => {
    setPhase('undoing')
    try {
      await item.undo?.()
      setPhase('undone')
    } catch {
      setPhase('failed')
    }
  }
  if (!p.mounted) return null
  const title = phase === 'undone' ? item.undone || '已撤销' : phase === 'failed' ? '没能撤销' : item.title
  const desc = phase === 'idle' || phase === 'undoing' ? item.desc : phase === 'failed' ? '再试一次，或者到那一项上改回来' : undefined
  const secs = Math.ceil(left / 1000)
  return (
    <div
      ref={p.ref}
      data-state={p.state}
      data-sheen={!error && !canUndo && phase !== 'undoing' ? phase : undefined}
      role={error ? 'alert' : undefined}
      onMouseEnter={() => { paused.current = true }}
      onMouseLeave={() => { paused.current = false }}
      onFocus={() => { paused.current = true }}
      onBlur={() => { paused.current = false }}
      onKeyDown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); onClose() } }}
      className="toast pointer-events-auto absolute bottom-0 flex min-h-10 max-w-[min(28rem,calc(100%-2rem))] items-center gap-2 py-[7px] pl-[7px] pr-1.5"
      data-testid="toast"
    >
      <span key={phase} className={cn('toast-swap flex size-6 shrink-0 items-center justify-center rounded-md', error ? 'bg-danger/10 text-danger' : canUndo || phase === 'undoing' ? '' : 'bg-success/10 text-success')}>
        {error ? <CircleAlert className="size-3.5" />
          : canUndo || phase === 'undoing' ? (
            <svg viewBox="0 0 24 24" className="toast-ring size-6" aria-hidden="true">
              <circle cx="12" cy="12" r="9.5" fill="none" strokeWidth="2" className="stroke-muted" />
              <circle cx="12" cy="12" r="9.5" fill="none" strokeWidth="2" strokeLinecap="round" className="stroke-foreground/70" transform="rotate(-90 12 12)" strokeDasharray={59.7} strokeDashoffset={59.7 * (1 - Math.max(0, left - 200) / UNDO_MS)} />
              <text x="12" y="12.5" textAnchor="middle" dominantBaseline="central" className="fill-foreground text-[10px] font-medium">{secs}</text>
            </svg>
          ) : <Check className="size-3.5" strokeWidth={2.5} />}
      </span>
      <span key={'t' + phase} className="toast-swap min-w-0 flex-1 py-px pr-1.5 text-sm [overflow-wrap:anywhere]">
        <span className="font-medium">{title}</span>
        {desc && <span className="text-muted-foreground">　{desc}</span>}
      </span>
      {(canUndo || phase === 'undoing') && (
        <button type="button" disabled={phase === 'undoing'} onClick={undo} className="tap-h inline-flex h-7 shrink-0 items-center rounded-md px-2 text-sm font-medium text-primary transition-colors hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring disabled:opacity-50">撤销</button>
      )}
      {(error || canUndo) && (
        <button type="button" onClick={onClose} aria-label="关闭" className="tap-h inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring">
          <X className="size-3.5" />
        </button>
      )}
    </div>
  )
}
