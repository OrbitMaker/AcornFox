import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from 'react'

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

// usePresence：浮层的“正在关闭”状态（样稿第 12 版；弹窗、抽屉、菜单、提示共用，不要每个组件各写一套）。
// open 变成 false 以后东西先留着播离场动画，播完才真的拿掉。用法：
//   const p = usePresence(open); if (!p.mounted) return null
//   <div ref={p.ref} data-state={p.state} className="ov ov-pop">…</div>      // 进场、离场的样子在 index.css 的 .ov-*
// ref 挂在播离场动画的那个元素上。没有动画时（测试环境、animation: none）立刻拿掉；动画迟迟不报结束（保险用的定时器），最多等动画时长的 6 倍多一点。
// 关到一半又打开：接着从“打开”播，不等。
export function usePresence<T extends HTMLElement = HTMLDivElement>(open: boolean) {
  const [mounted, setMounted] = useState(open)
  const ref = useRef<T>(null)
  if (open && !mounted) setMounted(true)
  useEffect(() => {
    if (open || !mounted) return
    const el = ref.current
    const done = () => setMounted(false)
    if (!el) { done(); return }
    const cs = getComputedStyle(el)
    const dur = parseFloat(cs.animationDuration) * (/ms$/.test(cs.animationDuration.split(',')[0].trim()) ? 1 : 1000)
    if (!cs.animationName || cs.animationName === 'none' || !(dur > 0)) { done(); return }
    const onEnd = (e: AnimationEvent) => { if (e.target === el) done() }
    el.addEventListener('animationend', onEnd)
    el.addEventListener('animationcancel', onEnd)
    const t = window.setTimeout(done, dur * 6 + 200)
    return () => {
      el.removeEventListener('animationend', onEnd)
      el.removeEventListener('animationcancel', onEnd)
      window.clearTimeout(t)
    }
  }, [open, mounted])
  return { mounted: open || mounted, state: (open ? 'open' : 'closed') as 'open' | 'closed', ref }
}

// useDelayedFlag：等待的指示（转圈、占位形状）什么时候显示（样稿第 12 版，全站只用这一个）。
// active 持续超过 delay 毫秒才变 true（很快就来的不闪一下）；一旦显示，至少留 min 毫秒。
export function useDelayedFlag(active: boolean, delay = 300, min = 500): boolean {
  const [shown, setShown] = useState(false)
  const since = useRef(0)
  useEffect(() => {
    if (active) {
      if (shown) return
      const t = window.setTimeout(() => { since.current = Date.now(); setShown(true) }, delay)
      return () => window.clearTimeout(t)
    }
    if (!shown) return
    const left = min - (Date.now() - since.current)
    if (left <= 0) { setShown(false); return }
    const t = window.setTimeout(() => setShown(false), left)
    return () => window.clearTimeout(t)
  }, [active, shown, delay, min])
  return shown
}

// useIndicator：选中块滑过去（样稿第 12 版；分段标签、左侧菜单共用）。量出容器里匹配 selector 的那个元素的位置，
// 返回选中块四边到容器四边的距离（没有选中的是 null）、这次往哪边走（dir）、ready（第一次摆好以后才开过渡，免得刚出来就滑一下）。
// 容器或窗口变了大小会重新量。
export function useIndicator(container: RefObject<HTMLElement>, selector: string, deps: unknown[]) {
  const [box, setBox] = useState<{ left: number; right: number; top: number; bottom: number } | null>(null)
  const [dir, setDir] = useState<'left' | 'right'>('right')
  const [ready, setReady] = useState(false)
  const last = useRef<{ left: number; top: number } | null>(null)
  const measure = useCallback(() => {
    const c = container.current
    const el = c?.querySelector<HTMLElement>(selector)
    if (!c || !el) { setBox(null); last.current = null; return }
    const b = { left: el.offsetLeft, top: el.offsetTop, right: c.scrollWidth - el.offsetLeft - el.offsetWidth, bottom: c.scrollHeight - el.offsetTop - el.offsetHeight }
    const p = last.current
    if (p && (p.left !== b.left || p.top !== b.top)) setDir(b.left > p.left || b.top > p.top ? 'right' : 'left')
    last.current = { left: b.left, top: b.top }
    setBox((o) => (o && o.left === b.left && o.right === b.right && o.top === b.top && o.bottom === b.bottom ? o : b))
  }, [container, selector])
  // eslint-disable-next-line react-hooks/exhaustive-deps
  useLayoutEffect(measure, deps)
  useEffect(() => {
    const c = container.current
    if (!c) return
    const raf = requestAnimationFrame(() => setReady(true))
    const ro = typeof ResizeObserver === 'function' ? new ResizeObserver(() => { setReady(false); measure(); requestAnimationFrame(() => setReady(true)) }) : null
    ro?.observe(c)
    return () => { cancelAnimationFrame(raf); ro?.disconnect() }
  }, [container, measure])
  return { box, dir, ready }
}
