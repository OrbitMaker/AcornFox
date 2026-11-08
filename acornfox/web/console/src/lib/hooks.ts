import { useCallback, useEffect, useRef, useState } from 'react'

// usePoll：每 ms 毫秒调一次 fn（页面在后台时暂停），返回最新数据、错误和手动刷新。
export function usePoll<T>(fn: (signal: AbortSignal) => Promise<T>, ms: number, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState<Error | null>(null)
  const [loading, setLoading] = useState(true)
  const fnRef = useRef(fn)
  fnRef.current = fn
  const gen = useRef(0)

  const refresh = useCallback(async () => {
    const my = ++gen.current
    const ctl = new AbortController()
    try {
      const v = await fnRef.current(ctl.signal)
      if (my === gen.current) {
        setData(v)
        setError(null)
      }
    } catch (e) {
      if (my === gen.current && (e as Error).name !== 'AbortError') setError(e as Error)
    } finally {
      if (my === gen.current) setLoading(false)
    }
  }, [])

  useEffect(() => {
    setLoading(true)
    refresh()
    if (ms <= 0) return
    const t = window.setInterval(() => {
      if (document.visibilityState === 'visible') refresh()
    }, ms)
    return () => window.clearInterval(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  return { data, error, loading, refresh, setData }
}

// useLockScroll：弹窗打开时锁住页面滚动。用 style 属性的脚本写法（CSSOM），不插入 <style>，CSP 允许。
export function useLockScroll(active: boolean) {
  useEffect(() => {
    if (!active) return
    const body = document.body
    const prev = body.style.overflow
    body.style.overflow = 'hidden'
    return () => {
      body.style.overflow = prev
    }
  }, [active])
}

export function useEscape(active: boolean, onEscape: () => void) {
  const ref = useRef(onEscape)
  ref.current = onEscape
  useEffect(() => {
    if (!active) return
    const h = (e: KeyboardEvent) => {
      if (e.key === 'Escape') ref.current()
    }
    window.addEventListener('keydown', h)
    return () => window.removeEventListener('keydown', h)
  }, [active])
}
