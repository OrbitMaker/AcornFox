// 真机走查：用 Playwright 在页面上从应用中心真装几个应用（前面的应用登记的服务，后面的应用装之前自动接上），
// 然后看我的应用、首页看板、运行环境、设置，深浅色和手机宽度各截图。
//
// 要走查哪些应用、哪个配方、填哪些设置项，从环境变量读（见 walk-targets.mjs）：
//   E2E_APPS（默认 new-api,hermes-agent，按顺序装）、E2E_RECIPE（设了就用这个配方一次装好）、
//   E2E_SET（默认 hermes-agent.model=qwen-plus，安装时要填的设置项）、E2E_NEEDS_TIMEOUT_MS（等“需要的服务”出现，默认 15000）。
// 做什么：登录 → 应用中心按顺序装 E2E_APPS 里的应用（看到进度、成功；
//         目录列表里声明了需要的服务的应用，弹窗里必须在时限内列出每一项、都已接上，没出现算失败；
//         服务端有首次登录信息的应用，装好后弹窗里必须有它的首次登录卡片，密码默认遮住）→ 我的应用里有这几个应用 → 首页看板有真数据（各应用内存里有它们）
//         → 运行环境、设置的各分区能打开 → 深色、手机宽度（390）再截一遍，手机宽度不能横向滚动。页面有 CSP 报错或脚本错误就算失败。
// 前提：一台装好 acornfox 的测试机，控制台经 SSH 隧道转到本机端口；有 Playwright 能用的浏览器（PW_CHANNEL=chrome 用系统 Chrome）。
//       测试机上还没装这几个应用（脚本会真装，要几分钟，镜像要现拉）；目录里有它们（新格式的清单，不是旧格式）。
// 怎么运行：
//   LOGIN_URL='http://127.0.0.1:28800/console/login?t=<一次性令牌>' OUT_DIR=/tmp/shots PW_CHANNEL=chrome node scripts/e2e-install.mjs
//   换应用：E2E_APPS=提供服务的应用id,要用它的应用id E2E_SET='要用它的应用id.设置项=值' node scripts/e2e-install.mjs
//   令牌在测试机上取：sudo curl -s --unix-socket /run/acornfox/api.sock -X POST http://x/v1/console/tokens
//   SKIP_INSTALL=1：应用已经装好时跳过安装，只看页面、重拍截图（安装那几张不重拍）。
//   VIDEO_DIR=目录：把主窗口的操作录成 webm 放进去（验收要录屏时用）。
// 输出：OUT_DIR 下的截图和 e2e-result.json（每项检查的结果）；全部通过退出码 0，否则 1。
// 副作用：在测试机上真装应用（只在测试机上用）；不显示、不截图明文密码。
import { chromium } from 'playwright'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { catalogNames, checkFirstLogin, fillSettings, needsVerdict, readNeedStatuses, walkTargets } from './walk-targets.mjs'

const LOGIN_URL = process.env.LOGIN_URL
const OUT = process.env.OUT_DIR || 'e2e-out'
const INSTALL_TIMEOUT = Number(process.env.INSTALL_TIMEOUT_MS || 10 * 60 * 1000)
if (!LOGIN_URL) {
  console.error('要设置 LOGIN_URL（控制台登录链接）')
  process.exit(2)
}
const { apps: APPS, recipe: RECIPE, sets: SETS, needsTimeout: NEEDS_TIMEOUT } = walkTargets()
mkdirSync(OUT, { recursive: true })
const origin = new URL(LOGIN_URL).origin
const checks = []
const t0 = Date.now()
const check = (name, ok, detail = '') => {
  checks.push({ name, ok: !!ok, detail, at_s: Math.round((Date.now() - t0) / 1000) })
  console.log((ok ? '通过 ' : '失败 ') + name + (detail ? '：' + detail : ''))
}

const browser = await chromium.launch(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {})
const problems = []
const watch = (page) => {
  page.on('console', (m) => {
    if (m.type() !== 'error') return
    const t = m.text()
    if (/Content Security Policy|Refused to/i.test(t)) problems.push('CSP：' + t)
  })
  page.on('pageerror', (e) => problems.push('脚本错误：' + e.message))
}
const shot = (page, name) => page.screenshot({ path: join(OUT, name + '.png'), fullPage: true })

// checkNeeds：目录列表里声明了需要的服务（declared 不为空）时，弹窗里必须在时限内列出每一项，并且都已接上
// （这台机器上已登记的，或配方里先装的应用登记的）。没声明的不查。
async function checkNeeds(page, dialog, what, declared, shotName) {
  if (declared.length === 0) return
  const statuses = await readNeedStatuses(dialog, declared.length, NEEDS_TIMEOUT)
  const v = needsVerdict(declared, statuses)
  check('装 ' + what + ' 前：需要的服务都已接上', v.ok, v.detail)
  await shot(page, shotName)
}

// checkLogins：装好后，服务端有首次登录信息的应用，弹窗里必须有它的卡片；有密码这类保密项的必须默认遮住。
async function checkLogins(page, dialog, apps) {
  for (const app of apps) {
    const r = await checkFirstLogin(page, dialog, app)
    if (!r) continue
    check(app + ' 装好后给首次登录信息', r.shown.ok, r.shown.detail)
    if (r.masked) check(app + ' 首次登录的密码默认遮住', r.masked.ok, r.masked.detail)
  }
}

// installFromStore：kind 是 app 或 recipe，id 是目录 id，name 是卡片上的名字（应用名或配方标题），
// declared 是目录列表里声明了的需要（catalogNames 的 needs），apps 是装好后要查首次登录的应用实例名。
async function installFromStore(page, kind, id, name, declared, apps) {
  await page.goto(origin + '/#store')
  const dialog = page.getByRole('dialog')
  if (kind === 'recipe') {
    const card = page.getByTestId('catalog-recipe').filter({ has: page.getByText(name, { exact: true }) }).first()
    await card.getByRole('button', { name: '开始' }).click()
  } else {
    const card = page.getByTestId('catalog-app').filter({ has: page.getByText(name, { exact: true }) }).first()
    await card.getByRole('button', { name: '安装' }).click()
  }
  await dialog.getByText(/acornfox install/).first().waitFor()
  await fillSettings(dialog, SETS, kind === 'app' ? id : '')
  await checkNeeds(page, dialog, name, declared, 'install-' + id + '-form')
  await dialog.getByRole('button', { name: kind === 'recipe' ? '开始' : '安装', exact: true }).click()
  const started = Date.now()
  // 进度：任务的步骤一出现就截图。
  await dialog.locator('li').first().waitFor({ timeout: 60000 })
  await page.waitForTimeout(1500)
  await shot(page, 'install-' + id + '-progress')
  await dialog.getByText(name + ' 已装好').waitFor({ timeout: INSTALL_TIMEOUT })
  const secs = Math.round((Date.now() - started) / 1000)
  await shot(page, 'install-' + id + '-done')
  await checkLogins(page, dialog, apps)
  return { dialog, secs }
}

try {
  const ctx = await browser.newContext({
    viewport: { width: 1280, height: 860 }, colorScheme: 'light', locale: 'zh-CN',
    ...(process.env.VIDEO_DIR ? { recordVideo: { dir: process.env.VIDEO_DIR, size: { width: 1280, height: 860 } } } : {}),
  })
  const page = await ctx.newPage()
  watch(page)
  await page.goto(LOGIN_URL)
  await page.getByRole('heading', { name: '首页' }).waitFor({ timeout: 20000 })
  check('登录进新控制台首页', true)

  const skip = process.env.SKIP_INSTALL === '1'
  const names = await catalogNames(page)
  // 1. 安装：配方一次装好，或按顺序一个一个装（后面的应用接上前面的应用登记的服务）
  if (!skip && RECIPE) {
    const r = await installFromStore(page, 'recipe', RECIPE, names.recipe(RECIPE), names.needs('recipe', RECIPE), names.recipeApps(RECIPE))
    check('应用中心用配方 ' + RECIPE + ' 装好：有进度', true, `${r.secs} 秒`)
    await page.keyboard.press('Escape')
  } else if (!skip) {
    for (const id of APPS) {
      const r = await installFromStore(page, 'app', id, names.app(id), names.needs('app', id), [id])
      check('应用中心装 ' + id + '：有进度，装好', true, `${r.secs} 秒`)
      await page.keyboard.press('Escape')
    }
  }

  // 2. 我的应用
  await page.goto(origin + '/#apps')
  await page.getByTestId('my-app').nth(APPS.length - 1).waitFor({ timeout: 30000 })
  const mine = await page.getByTestId('my-app').allInnerTexts()
  check('我的应用里有 ' + APPS.join('、'), APPS.every((id) => mine.some((t) => t.includes(id))), mine.map((t) => t.split('\n')[0]).join('、'))
  await shot(page, 'apps-light')

  // 3. 首页看板：等各应用内存出现这几个应用
  await page.goto(origin + '/#home')
  await page.getByRole('heading', { name: '首页' }).waitFor()
  await page.getByTestId('widget').filter({ hasText: '各应用内存' }).getByText(APPS[APPS.length - 1]).waitFor({ timeout: 60000 })
  const memText = await page.getByTestId('widget').filter({ hasText: '各应用内存' }).innerText()
  check('首页看板：各应用内存是真数据', APPS.every((id) => memText.includes(id)) && /MB/.test(memText), memText.replace(/\s+/g, ' ').slice(0, 120))
  const ranges = await page.getByRole('tablist', { name: '时间范围' }).getByRole('tab').allInnerTexts().catch(() => [])
  check('时间范围只给服务端有的（1 小时、24 小时）', ranges.join(',') === '1 小时,24 小时', ranges.join(','))
  const cpuText = await page.getByTestId('widget').filter({ hasText: 'CPU 使用率' }).first().innerText()
  check('CPU 来自整机历史（有数字）', /\d+(\.\d+)?%/.test(cpuText), cpuText.replace(/\s+/g, ' ').slice(0, 80))
  await page.waitForTimeout(1000)
  await shot(page, 'home-light')

  // 4. 运行环境、设置
  await page.goto(origin + '/#runtime')
  await page.getByTestId('runtime-check').first().waitFor({ timeout: 20000 })
  const rows = await page.getByTestId('runtime-check').allInnerTexts()
  check('运行环境：自检有后台服务、Docker、网关、磁盘、端口', ['后台服务', 'Docker', '网关', '磁盘', '端口'].every((k) => rows.some((r) => r.includes(k))), rows.map((r) => r.split('\n')[0]).join(' | ').slice(0, 300))
  await shot(page, 'runtime-light')
  for (const sec of ['general', 'access', 'engine', 'storage', 'mirror', 'update', 'dev']) {
    await page.goto(origin + '/#prefs/' + sec)
    await page.getByTestId('prefs-' + sec).waitFor()
    await page.waitForTimeout(500)
    if (['access', 'mirror', 'storage'].includes(sec)) await shot(page, 'prefs-' + sec + '-light')
  }
  const order = await page.goto(origin + '/#prefs/mirror').then(() => page.getByTestId('mirror-order').innerText({ timeout: 15000 }))
  check('设置·下载源显示换源顺序', /daocloud/.test(order), order.replace(/\s+/g, ' '))
  check('设置七个分区都能打开', true)

  // 5. 深色
  await page.emulateMedia({ colorScheme: 'dark' })
  for (const [hash, name, wait] of [['#home', 'home-dark', 'widget'], ['#store', 'store-dark', 'catalog-app'], ['#runtime', 'runtime-dark', 'runtime-check'], ['#prefs/mirror', 'prefs-mirror-dark', 'mirror-order']]) {
    await page.goto(origin + '/' + hash)
    await page.getByTestId(wait).first().waitFor()
    await page.waitForTimeout(800)
    await shot(page, name)
  }
  const dark = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  check('深色生效', dark !== 'rgb(255, 255, 255)', dark)

  // 6. 手机宽度
  const m = await browser.newContext({ viewport: { width: 390, height: 844 }, colorScheme: 'light', locale: 'zh-CN', storageState: await ctx.storageState(), isMobile: true, hasTouch: true })
  const mp = await m.newPage()
  watch(mp)
  for (const [hash, name, wait] of [['#home', 'mobile-home', 'widget'], ['#runtime', 'mobile-runtime', 'runtime-check'], ['#prefs/mirror', 'mobile-prefs', 'mirror-order'], ['#store', 'mobile-store', 'catalog-app']]) {
    await mp.goto(origin + '/' + hash)
    await mp.getByTestId(wait).first().waitFor()
    await mp.waitForTimeout(800)
    const over = await mp.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    check('手机宽度 ' + hash + ' 没有横向滚动', over <= 0, 'scrollWidth - innerWidth = ' + over)
    await shot(mp, name)
  }
  await m.close()

  check('没有 CSP 报错和脚本错误', problems.length === 0, problems.join(' | ').slice(0, 400))
} catch (e) {
  check('走查中途出错', false, String(e && e.message ? e.message : e).slice(0, 400))
} finally {
  // 先关窗口，录屏文件才会写完。
  for (const c of browser.contexts()) await c.close().catch(() => {})
  await browser.close()
}

writeFileSync(join(OUT, 'e2e-result.json'), JSON.stringify({ checks, problems }, null, 2))
const failed = checks.filter((c) => !c.ok).length
console.log(failed ? `有 ${failed} 项没通过` : `全部 ${checks.length} 项通过`)
process.exit(failed ? 1 : 0)
