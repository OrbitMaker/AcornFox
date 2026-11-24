// 界面连续性（样稿第 12 版）：从“我的应用”点进一个应用时，它的图标和名字从卡片的位置挪到详情页页头，返回时原路回去。
// 用浏览器自带的视图过渡（View Transitions），不支持的浏览器照旧硬切，不引入动画库。
// 样稿第 14 版：只有“我的应用 ↔ 应用详情”带图标和名字；应用中心的卡片点进应用页、解决方案页（和别处进这几种详情页）
// 只淡出淡入，不飞图标（原来那样偏花）。首页已经没有应用卡片，那一处的标记也拿掉了。
//
// 怎么认“同一个东西”：列表里的卡片和详情页页头，各自在图标上写 data-vt-icon="<键>"、在名字上写 data-vt-name="<键>"
// （键是 vtKey 算的：app:<应用名>）；卡片最外层写 data-vt-scope，同一页有几处同一个应用时，
// 用的是刚点的那一张。过渡前给旧页面上的那两个元素标上 view-transition-name（vt-icon、vt-name），页面换好以后给新页面上的标上，
// 过渡完都擦掉。名字是 style 属性的脚本写法，控制台的 CSP 允许。过渡的时长、曲线在 index.css（::view-transition-*）。
//
// 只在“列表 ↔ 详情”之间做；旧页面上找不到可带的元素（例如从搜索面板、应用中心进详情）就只淡出淡入。
// 详情页的数据还没到、页头还没画出来时最多等 WAIT_MS，等不到就只让旧的那一个淡出，不卡住页面。
// “减少动效”时不带元素，只淡入淡出（index.css）。新的一次导航会打断上一次没放完的过渡。
import type { Route } from './router'

const WAIT_MS = 250
const NAMES = { icon: 'vt-icon', name: 'vt-name' } as const
type Kind = keyof typeof NAMES

export function vtKey(kind: 'app', id: string): string {
  return kind + ':' + id
}

// 能带图标和名字的只有应用详情。
function keyOf(r: Route): string | null {
  if (r.page === 'app' && r.id) return vtKey('app', r.id)
  return null
}

// 详情页（应用详情、应用中心的应用页、解决方案页）：进出这几种页面至少有一次淡出淡入。
function detailOf(r: Route): string | null {
  if ((r.page === 'app' || r.page === 'item' || r.page === 'solution') && r.id) return r.page + ':' + r.id
  return null
}

// 刚按下的那个元素：同一页有几张同一个应用的卡片时，带的是点的那一张。
let pressed: Element | null = null
if (typeof document !== 'undefined') {
  document.addEventListener('pointerdown', (e) => { pressed = e.target as Element }, true)
  document.addEventListener('keydown', (e) => { if (e.key === 'Enter') pressed = document.activeElement }, true)
}

function find(key: string): Partial<Record<Kind, HTMLElement>> {
  const out: Partial<Record<Kind, HTMLElement>> = {}
  const scope = pressed && pressed.isConnected ? pressed.closest('[data-vt-scope]') : null
  for (const kind of Object.keys(NAMES) as Kind[]) {
    const sel = '[data-vt-' + kind + '="' + CSS.escape(key) + '"]'
    const el = (scope && scope.querySelector<HTMLElement>(sel)) || document.querySelector<HTMLElement>('main ' + sel)
    if (el && el.getClientRects().length) out[kind] = el
  }
  return out
}

function tag(els: Partial<Record<Kind, HTMLElement>>): () => void {
  const done: HTMLElement[] = []
  for (const kind of Object.keys(NAMES) as Kind[]) {
    const el = els[kind]
    if (!el) continue
    el.style.setProperty('view-transition-name', NAMES[kind])
    done.push(el)
  }
  return () => done.forEach((el) => el.style.removeProperty('view-transition-name'))
}

function waitFor(key: string, ms: number): Promise<Partial<Record<Kind, HTMLElement>>> {
  return new Promise((resolve) => {
    const t0 = performance.now()
    // 用定时器轮询，不用 requestAnimationFrame：过渡等新页面的这段时间浏览器不画帧，rAF 不会被调到。
    const tick = () => {
      const got = find(key)
      if (got.icon || got.name || performance.now() - t0 > ms) resolve(got)
      else window.setTimeout(tick, 16)
    }
    tick()
  })
}

type Doc = Document & { startViewTransition?: (cb: () => void | Promise<void>) => { finished: Promise<void>; ready: Promise<void>; updateCallbackDone: Promise<void> } }

// carry：换页。apply(sync) 是真正换页的那一步（sync 为 true 时要同步把新页面画出来，用 flushSync）。
export function carry(prev: Route, next: Route, apply: (sync: boolean) => void) {
  const doc = document as Doc
  const pk = keyOf(prev)
  const nk = keyOf(next)
  // 进详情用新页面的键；从详情回列表用旧页面的键；同一个详情里换页签不算换页。
  const key = nk && nk !== pk ? nk : pk && !nk ? pk : null
  // 进出详情页（同一个详情里换页签不算）：没有可带的元素时只淡出淡入。
  const pd = detailOf(prev)
  const nd = detailOf(next)
  const fade = pd !== nd && !!(pd || nd)
  if ((!key && !fade) || typeof doc.startViewTransition !== 'function') { apply(false); return }
  const reduced = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches
  const from = reduced || !key ? {} : find(key)
  if (!key || (!reduced && !from.icon && !from.name)) {
    const t = doc.startViewTransition(() => { apply(true); pressed = null })
    t.ready.catch(() => {})
    t.updateCallbackDone.catch(() => {})
    t.finished.catch(() => {})
    return
  }
  const untagOld = tag(from)
  let untagNew = () => {}
  const t = doc.startViewTransition(async () => {
    apply(true)
    untagOld()
    if (reduced) return
    pressed = null
    let to = find(key)
    if (!to.icon && !to.name) to = await waitFor(key, WAIT_MS)
    untagNew = tag(to)
  })
  // 被下一次导航打断时这几个 Promise 会被拒绝，属于正常情况，不让它变成页面报错。
  t.ready.catch(() => {})
  t.updateCallbackDone.catch(() => {})
  t.finished.catch(() => {}).finally(() => { untagOld(); untagNew() })
}
