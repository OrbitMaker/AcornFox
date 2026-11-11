// 样稿和实际控制台的尺寸、字号、颜色对比：同一个窗口大小下打开两边的同一个页面，量同一批元素，截图并排放。
//
// 做什么：对首页、我的应用、应用中心三个页面，量正文字号和字体、页面标题、侧栏宽度和菜单项、
//         主按钮（高度、字号、底色）、第一张卡片、搜索框，输出两边的数字和差值；每个页面各截一张图。只读，不点任何按钮。
// 什么时候用：有人觉得实际页面和样稿“大小不一样、颜色不一样”时，先跑这个拿到数字再改。
// 前提：样稿的 HTML 文件（要能上网取它引用的 CDN 脚本和字体）；
//       一台装好 acornfox 的测试机，控制台经 SSH 隧道转到本机端口；Playwright 能用的浏览器（PW_CHANNEL=chrome 用系统 Chrome）。
// 怎么运行：
//   PROTO=/path/样稿.html LOGIN_URL='http://127.0.0.1:28830/console/login?t=<一次性令牌>' \
//     OUT_DIR=/tmp/cmp PW_CHANNEL=chrome node scripts/compare-prototype.mjs
//   ACCENT=blue|green|orange：样稿用哪个主色打开（默认 neutral，样稿的默认值）。VIEW=1440x900 改窗口大小。
// 输出：OUT_DIR/compare.json（每个页面每个元素两边的数字，differs=true 的是不一致的）和 proto-*.png、actual-*.png。
//       退出码 0 表示量完了（不代表一致），看 compare.json 里的 differs。
// 出错先看哪里：样稿一直停在“正在加载样稿…”是 CDN 取不到；实际页面量不到元素多半是登录链接过期（一次性，几分钟有效）。
import { chromium } from 'playwright'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'

const { PROTO, LOGIN_URL } = process.env
const OUT = process.env.OUT_DIR || 'cmp-out'
const ACCENT = process.env.ACCENT || 'neutral'
const [W, H] = (process.env.VIEW || '1440x900').split('x').map(Number)
if (!PROTO || !LOGIN_URL) { console.error('要设置 PROTO（样稿 HTML 路径）和 LOGIN_URL（控制台登录链接）'); process.exit(2) }
mkdirSync(OUT, { recursive: true })

const PAGES = [['home', '#'], ['apps', '#apps'], ['store', '#store']]

// 在页面里量：两边用同一段代码，按文字和结构找元素，不依赖各自的 class 名。
const measure = () => {
  const cs = (e, extra = []) => {
    if (!e) return null
    const s = getComputedStyle(e); const r = e.getBoundingClientRect()
    const o = { w: Math.round(r.width * 10) / 10, h: Math.round(r.height * 10) / 10, font: s.fontSize, weight: s.fontWeight, line: s.lineHeight }
    for (const k of extra) o[k] = s[k]
    return o
  }
  const byText = (sel, t) => [...document.querySelectorAll(sel)].find((e) => e.textContent.trim().includes(t) && e.getBoundingClientRect().width > 0)
  const main = document.querySelector('main') || document.body
  const out = {}
  out.body = { font: getComputedStyle(document.body).fontSize, line: getComputedStyle(document.body).lineHeight, family: [...document.fonts].filter((f) => f.status === 'loaded').map((f) => f.family.replace(/"/g, '')).filter((v, i, a) => a.indexOf(v) === i).sort().join(',') }
  out.h1 = cs(main.querySelector('h1'))
  const nav = byText('aside a, aside button, nav a, nav button', '我的应用')
  out.navItem = cs(nav, ['paddingLeft'])
  const aside = nav && nav.closest('aside')
  out.sidebar = aside ? { w: Math.round(aside.getBoundingClientRect().width) } : null
  out.mainPad = { left: getComputedStyle(main).paddingLeft, top: getComputedStyle(main).paddingTop, contentW: Math.round((main.querySelector('h1')?.parentElement || main).getBoundingClientRect().width) }
  const primary = byText('main a, main button', '装新应用') || [...main.querySelectorAll('button, a')].find((e) => { const b = getComputedStyle(e).backgroundColor; return e.textContent.trim() === '安装' && b !== 'rgba(0, 0, 0, 0)' })
  out.primaryButton = cs(primary, ['backgroundColor', 'color', 'borderRadius', 'paddingLeft'])
  const card = [...main.querySelectorAll('div')].find((e) => { const s = getComputedStyle(e); return s.borderTopWidth === '1px' && parseFloat(s.borderTopLeftRadius) >= 6 && e.getBoundingClientRect().width > 150 })
  out.firstCard = cs(card, ['borderRadius', 'paddingLeft', 'paddingTop'])
  const cardTitle = card && [...card.querySelectorAll('div,span,h3')].find((e) => e.children.length === 0 && e.textContent.trim())
  out.firstCardText = cs(cardTitle)
  out.search = cs(document.querySelector('header input, header button[aria-label*="搜索"], main input'))
  out.headerH = (() => { const h = document.querySelector('header'); return h ? Math.round(h.getBoundingClientRect().height) : null })()
  return out
}

const browser = await chromium.launch({ channel: process.env.PW_CHANNEL || undefined })
const result = { view: `${W}x${H}`, accent: ACCENT, pages: {} }
async function side(name, open) {
  const ctx = await browser.newContext({ viewport: { width: W, height: H }, deviceScaleFactor: 1, colorScheme: 'light' })
  const page = await ctx.newPage()
  const base = await open(page)
  for (const [key, hash] of PAGES) {
    await page.goto(base + hash)
    await page.waitForSelector('h1', { timeout: 30000 })
    await page.evaluate(() => document.fonts.ready)
    await page.waitForTimeout(1500)
    result.pages[key] = result.pages[key] || {}
    result.pages[key][name] = await page.evaluate(measure)
    await page.screenshot({ path: join(OUT, `${name}-${key}.png`) })
  }
  await ctx.close()
}
await side('proto', async (page) => {
  const url = 'file://' + resolve(PROTO)
  await page.addInitScript((a) => { try { localStorage.setItem('afx-accent', a); localStorage.setItem('afx-theme', 'light') } catch { /* 没有 */ } }, ACCENT)
  await page.goto(url)
  return url
})
await side('actual', async (page) => {
  await page.goto(LOGIN_URL)
  await page.waitForSelector('h1', { timeout: 30000 })
  return new URL(LOGIN_URL).origin + '/'
})
await browser.close()

// 两边逐项比，数字差超过 0.5 或文字不同就标出来。
const diffs = []
for (const [pg, v] of Object.entries(result.pages)) {
  for (const el of Object.keys(v.proto || {})) {
    const a = v.proto[el], b = v.actual?.[el]
    if (a === null || b === null || b === undefined) { if (a !== b) diffs.push({ page: pg, el, proto: a, actual: b ?? null }); continue }
    if (typeof a !== 'object') { if (a !== b) diffs.push({ page: pg, el, proto: a, actual: b }); continue }
    for (const k of Object.keys(a)) {
      const x = a[k], y = b[k]
      const same = typeof x === 'number' && typeof y === 'number' ? Math.abs(x - y) <= 0.5 : x === y
      if (!same) diffs.push({ page: pg, el: el + '.' + k, proto: x, actual: y })
    }
  }
}
result.diffs = diffs
writeFileSync(join(OUT, 'compare.json'), JSON.stringify(result, null, 2))
console.log(JSON.stringify({ view: result.view, accent: ACCENT, diffs }, null, 1))
