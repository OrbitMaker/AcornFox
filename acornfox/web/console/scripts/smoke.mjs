// 冒烟测试：用 Playwright 打开真的 acornfox 控制台，走一遍应用中心，深浅色各截一张图。
//
// 做什么：用登录链接进控制台 → 首页 → 应用中心（要看到目录里的应用）→ 打开 New API 的安装弹窗
//         （只看弹窗，不点安装）→ 深色再截一遍 → 手机宽度截一张 → 打开 /legacy/ 确认跳回新控制台（旧版已去掉）。
//         页面有 CSP 报错或脚本错误就算失败。
// 前提：开发机上已装 playwright 的 Chromium（npx playwright install chromium）；有一个在跑的 acornfox 控制台。
// 怎么运行：
//   LOGIN_URL='http://127.0.0.1:18800/console/login?t=<一次性令牌>' OUT_DIR=/tmp/shots npm run smoke
//   令牌用受信入口取：curl --unix-socket <数据目录>/api.sock -X POST http://x/v1/console/tokens
//   PW_CHANNEL=chrome 用系统装的 Chrome（没下载 Playwright 自带的浏览器时）。
// 输出：OUT_DIR 下的截图和 smoke-result.json（每项检查的结果）；全部通过退出码 0，否则 1。
// 副作用：只读，不安装、不改任何东西（登录会建一个控制台会话）。
import { chromium } from 'playwright'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'

const LOGIN_URL = process.env.LOGIN_URL
const OUT = process.env.OUT_DIR || 'smoke-out'
if (!LOGIN_URL) {
  console.error('要设置 LOGIN_URL（控制台登录链接）')
  process.exit(2)
}
mkdirSync(OUT, { recursive: true })
const origin = new URL(LOGIN_URL).origin
const checks = []
const check = (name, ok, detail = '') => {
  checks.push({ name, ok: !!ok, detail })
  console.log((ok ? '通过 ' : '失败 ') + name + (detail ? '：' + detail : ''))
}

const browser = await chromium.launch(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {})
try {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, colorScheme: 'light', locale: 'zh-CN' })
  const page = await ctx.newPage()
  // CSP 拦截和脚本异常算失败；接口回错误（例如这台机器没有 Docker 时读不到资源）只记下来。
  const problems = []
  const notes = []
  page.on('console', (m) => {
    if (m.type() !== 'error') return
    const t = m.text()
    if (/Content Security Policy|Refused to/i.test(t)) problems.push(t)
    else notes.push(t)
  })
  page.on('pageerror', (e) => problems.push(String(e)))

  await page.goto(LOGIN_URL)
  await page.waitForURL(origin + '/')
  await page.getByRole('heading', { name: '首页' }).waitFor()
  check('登录后进到新控制台首页', true, page.url())
  const csp = (await ctx.request.get(origin + '/')).headers()['content-security-policy'] || ''
  check('首页带 CSP，不放开内联脚本', csp.includes("script-src 'self'") && !csp.includes('unsafe-inline'), csp)
  await page.screenshot({ path: join(OUT, 'home-light.png') })

  await page.getByRole('navigation', { name: '主菜单' }).getByRole('link', { name: '应用中心' }).click()
  await page.getByRole('heading', { name: '应用中心' }).waitFor()
  const cards = page.getByTestId('catalog-app')
  await cards.first().waitFor()
  const names = await cards.allInnerTexts()
  check('应用中心列出目录里的应用', names.length >= 2 && names.some((t) => t.includes('New API')) && names.some((t) => t.includes('Hermes')), names.map((t) => t.split('\n')[0]).join('、'))
  await page.screenshot({ path: join(OUT, 'store-light.png'), fullPage: true })

  const card = cards.filter({ hasText: 'New API' }).first()
  await card.getByRole('button', { name: '安装' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByText('acornfox install new-api').waitFor()
  check('安装弹窗给出命令行写法', true)
  const mem = await dialog.getByTestId('mem-check').innerText()
  check('安装弹窗有内存检查', /要 .+/.test(mem), mem)
  await page.screenshot({ path: join(OUT, 'install-dialog-light.png') })

  await page.emulateMedia({ colorScheme: 'dark' })
  await page.waitForTimeout(200)
  const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  check('深色模式背景变深', bg !== 'rgb(255, 255, 255)', bg)
  await page.screenshot({ path: join(OUT, 'install-dialog-dark.png') })
  await page.keyboard.press('Escape')
  await page.screenshot({ path: join(OUT, 'store-dark.png'), fullPage: true })
  await page.emulateMedia({ colorScheme: 'light' })

  await page.setViewportSize({ width: 375, height: 812 })
  await page.waitForTimeout(200)
  const sw = await page.evaluate(() => document.documentElement.scrollWidth)
  check('手机宽度没有横向滚动', sw <= 375, 'scrollWidth=' + sw)
  await page.screenshot({ path: join(OUT, 'mobile-store-light.png') })
  await page.getByRole('button', { name: '打开菜单' }).click()
  await page.getByRole('dialog', { name: '菜单' }).waitFor()
  await page.waitForTimeout(400)
  check('手机宽度下菜单收进抽屉', true)
  await page.screenshot({ path: join(OUT, 'mobile-menu-light.png') })
  await page.keyboard.press('Escape')
  await page.setViewportSize({ width: 1280, height: 800 })

  await page.keyboard.press('Control+k')
  await page.getByRole('dialog', { name: '搜索' }).waitFor()
  await page.keyboard.type('hermes')
  const hit = await page.getByRole('dialog', { name: '搜索' }).getByText('Hermes 智能体').count()
  check('⌘K 能搜到目录里的应用', hit > 0, String(hit))
  await page.keyboard.press('Escape')

  await page.goto(origin + '/legacy/')
  await page.waitForLoadState('networkidle')
  check('/legacy/ 跳回新控制台', new URL(page.url()).pathname === '/', page.url())

  check('没有 CSP 报错和脚本错误', problems.length === 0, problems.slice(0, 5).join(' | '))
  if (notes.length) console.log('接口错误（不算失败）：' + [...new Set(notes)].slice(0, 8).join(' | '))
} finally {
  await browser.close()
}
writeFileSync(join(OUT, 'smoke-result.json'), JSON.stringify({ at: new Date().toISOString(), checks }, null, 2) + '\n')
process.exit(checks.every((c) => c.ok) ? 0 : 1)
