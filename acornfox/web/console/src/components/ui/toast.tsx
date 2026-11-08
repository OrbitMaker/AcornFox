// 页面底部的短提示，2.8 秒后自动消失。
import * as React from 'react'
import { CircleAlert, CircleCheck } from 'lucide-react'

type Item = { id: number; title: string; desc?: string; tone: 'ok' | 'error' }
type ToastFn = (title: string, desc?: string, tone?: 'ok' | 'error') => void

const Ctx = React.createContext<ToastFn>(() => {})
export const useToast = () => React.useContext(Ctx)

export function ToastProvider({ children }: { children: React.ReactNode }) {
  const [items, setItems] = React.useState<Item[]>([])
  const seq = React.useRef(0)
  const toast = React.useCallback<ToastFn>((title, desc, tone = 'ok') => {
    const id = ++seq.current
    setItems((x) => [...x, { id, title, desc, tone }])
    window.setTimeout(() => setItems((x) => x.filter((i) => i.id !== id)), tone === 'error' ? 5000 : 2800)
  }, [])
  return (
    <Ctx.Provider value={toast}>
      {children}
      <div className="pointer-events-none fixed inset-x-0 bottom-0 z-[70] flex flex-col items-center gap-2 p-4" role="status" aria-live="polite">
        {items.map((i) => (
          <div key={i.id} className="pointer-events-auto flex w-full max-w-xs animate-in items-start gap-2.5 rounded-md border bg-background px-3 py-2.5 shadow-lg">
            {i.tone === 'error' ? <CircleAlert className="mt-0.5 size-4 shrink-0 text-danger" /> : <CircleCheck className="mt-0.5 size-4 shrink-0 text-success" />}
            <div className="min-w-0">
              <div className="text-sm font-medium">{i.title}</div>
              {i.desc && <div className="break-words text-xs text-muted-foreground">{i.desc}</div>}
            </div>
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}
