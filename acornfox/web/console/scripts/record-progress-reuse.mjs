// 录屏：下载镜像的进度条、接上旧数据重装。
//
// 要走查哪些应用、填哪些设置项，从环境变量读（见 walk-targets.mjs）：E2E_APPS（默认 new-api,hermes-agent，按顺序装）、
// E2E_SET（默认 hermes-agent.model=qwen-plus）、E2E_NEEDS_TIMEOUT_MS（等“需要的服务”出现，默认 15000）。这个脚本一个一个装，不用 E2E_RECIPE。
// 目录列表里声明了需要的服务的应用，安装弹窗里必须在时限内列出每一项、都已接上，没出现算失败；没声明的不查。
// 做什么：用 Playwright 在真页面上操作并录屏（webm），分两段，由 PHASE 选：
//   PHASE=progress：应用中心按顺序装 E2E_APPS 里的应用（后面的应用“需要的服务”接上前面的应用登记的那条），
//                   录下最后一个应用“下载镜像”的进度条从 0 走到头、“正在导入镜像”、装好（最后一个应用要经下载网关下载镜像包）。
//   PHASE=reuse   ：这几个应用都已删掉（保留数据）之后，按顺序：装 → 报 CAT-INST-24 → 点“接上旧数据重装”→“确认接上”→ 装好；
//                   第一个应用要跳过初始化，后面的应用“需要的服务”都已接上。
//   动作之间故意停顿（slowMo 和 pause），方便人看。不点“显示密码”，录屏里没有明文密码。
// 前提：一台装好 acornfox 的测试机，控制台经 SSH 隧道转到本机端口；Playwright 能用的浏览器（PW_CHANNEL=chrome 用系统 Chrome）；
//       录屏要 ffmpeg（Playwright 的 ffmpeg-<版本>/ffmpeg-linux，可以链接到系统 ffmpeg）。
// 怎么运行：
//   LOGIN_URL='http://127.0.0.1:28820/console/login?t=<一次性令牌>' PHASE=progress VIDEO_DIR=/tmp/v OUT_DIR=/tmp/v PW_CHANNEL=chrome node scripts/record-progress-reuse.mjs
//   令牌在测试机上取：sudo curl -s --unix-socket /run/acornfox/api.sock -X POST http://x/v1/console/tokens
// 输出：VIDEO_DIR 下的 webm，OUT_DIR 下几张截图和 record-<PHASE>.json（每项检查）；全部通过退出码 0。
// 副作用：在测试机上真装应用（只在测试机上用）。
import { chromium } from 'playwright'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { catalogNames, fillSettings, needsVerdict, readNeedStatuses, walkTargets } from './walk-targets.mjs'

const LOGIN_URL = process.env.LOGIN_URL
const PHASE = process.env.PHASE || 'progress'
const OUT = process.env.OUT_DIR || 'record-out'
const VIDEO = process.env.VIDEO_DIR || OUT
if (!LOGIN_URL) {
  console.error('要设置 LOGIN_URL')
  process.exit(2)
}
const { apps: APPS, sets: SETS, needsTimeout: NEEDS_TIMEOUT } = walkTargets()
mkdirSync(OUT, { recursive: true })
mkdirSync(VIDEO, { recursive: true })
const origin = new URL(LOGIN_URL).origin
const checks = []
const t0 = Date.now()
const check = (name, ok, detail = '') => {
  checks.push({ name, ok: !!ok, detail, at_s: Math.round((Date.now() - t0) / 1000) })
  console.log((ok ? '通过 ' : '失败 ') + name + (detail ? '：' + detail : ''))
}
const pause = (page, ms = 1500) => page.waitForTimeout(ms)

const browser = await chromium.launch({ ...(process.env.PW_CHANNEL ? { channel: process.env.PW_CHANNEL } : {}), slowMo: 250 })
const ctx = await browser.newContext({
  viewport: { width: 1280, height: 860 }, colorScheme: 'light', locale: 'zh-CN',
  recordVideo: { dir: VIDEO, size: { width: 1280, height: 860 } },
})
const page = await ctx.newPage()
const shot = (name) => page.screenshot({ path: join(OUT, name + '.png') })

// openInstall：打开 id 这个应用的安装弹窗（卡片按名字找），填好 E2E_SET 里给它的设置项；
// 目录列表里它声明了需要的服务（declared 不为空）时，弹窗里必须在时限内列出每一项，并且都已接上（这台机器上已登记的那条）。
async function openInstall(id, name, declared) {
  await page.goto(origin + '/#store')
  const card = page.getByTestId('catalog-app').filter({ has: page.getByText(name, { exact: true }) }).first()
  await card.waitFor()
  await pause(page)
  await card.getByRole('button', { name: '安装' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByText(/acornfox install/).first().waitFor()
  await fillSettings(dialog, SETS, id)
  if (declared.length > 0) {
    const v = needsVerdict(declared, await readNeedStatuses(dialog, declared.length, NEEDS_TIMEOUT))
    check('装 ' + id + ' 之前需要的服务都已接上', v.ok, v.detail)
  }
  await pause(page)
  return dialog
}

try {
  await page.goto(LOGIN_URL)
  await page.getByRole('heading', { name: '首页' }).waitFor({ timeout: 20000 })
  await pause(page)

  const names = await catalogNames(page)
  if (PHASE === 'progress') {
    // 前面的应用照常装；最后一个录进度条。
    for (const id of APPS.slice(0, -1)) {
      const d = await openInstall(id, names.app(id), names.needs('app', id))
      await d.getByRole('button', { name: '安装', exact: true }).click()
      await d.getByText(names.app(id) + ' 已装好').waitFor({ timeout: 5 * 60 * 1000 })
      check('装 ' + id, true)
      await pause(page, 2500)
      await page.keyboard.press('Escape')
    }
    const last = APPS[APPS.length - 1]
    const d = await openInstall(last, names.app(last), names.needs('app', last))
    await d.getByRole('button', { name: '安装', exact: true }).click()
    // 进度条：等它出现，记下几次读数
    const bar = d.getByTestId('step-progress')
    await bar.waitFor({ timeout: 2 * 60 * 1000 })
    const seen = []
    const deadline = Date.now() + 10 * 60 * 1000
    let shotMid = false
    while (Date.now() < deadline) {
      const txt = (await d.getByText(/从下载网关下载 \d+ MB|正在导入镜像/).first().innerText().catch(() => '')) || ''
      const now = await bar.getAttribute('aria-valuenow').catch(() => null)
      if (txt && (seen.length === 0 || seen[seen.length - 1].txt !== txt)) seen.push({ txt, now })
      if (!shotMid && now && Number(now) >= 40) { await shot('progress-mid'); shotMid = true }
      if (/正在导入镜像/.test(txt)) break
      await page.waitForTimeout(1000)
    }
    const pcts = seen.map((s) => s.now).filter(Boolean).map(Number)
    check('进度条从小到大走（读数）', pcts.length >= 3 && pcts[pcts.length - 1] > pcts[0], pcts.join(' → '))
    check('下完显示正在导入镜像', seen.some((s) => /正在导入镜像/.test(s.txt)))
    await shot('progress-load')
    await d.getByText(names.app(last) + ' 已装好').waitFor({ timeout: 5 * 60 * 1000 })
    check(last + ' 装好', true)
    await pause(page, 3000)
    await shot('progress-done')
  } else {
    // 接上旧数据：按顺序一个一个装回来
    for (const [i, id] of APPS.entries()) {
      const d = await openInstall(id, names.app(id), names.needs('app', id))
      await d.getByRole('button', { name: '安装', exact: true }).click()
      await d.getByText(/CAT-INST-24/).waitFor({ timeout: 60000 })
      if (i === 0) check('同名旧数据还在：报 CAT-INST-24', true)
      await pause(page, 2500)
      if (i === 0) await shot('reuse-refused')
      await d.getByRole('button', { name: '接上旧数据重装' }).click()
      await pause(page, 2500)
      if (i === 0) await shot('reuse-confirm')
      await d.getByRole('button', { name: '确认接上' }).click()
      await d.getByText(names.app(id) + ' 已装好').waitFor({ timeout: 5 * 60 * 1000 })
      if (i === 0) {
        const initMsg = await d.getByText(/接上了旧数据|已经初始化过/).first().innerText().catch(() => '')
        check(id + ' 接上旧数据装好，跳过初始化', /接上了旧数据/.test(initMsg), initMsg)
      } else {
        check(id + ' 接上旧数据、接上本机的服务装好', true)
      }
      await pause(page, 3000)
      await shot('reuse-' + id + '-done')
      await page.keyboard.press('Escape')
    }
    await page.goto(origin + '/#apps')
    await page.getByTestId('my-app').nth(APPS.length - 1).waitFor({ timeout: 30000 })
    await pause(page, 3000)
  }
} catch (e) {
  check('录屏中途出错', false, String(e && e.message ? e.message : e).slice(0, 400))
  await shot('error').catch(() => {})
} finally {
  await ctx.close()
  await browser.close()
}
writeFileSync(join(OUT, 'record-' + PHASE + '.json'), JSON.stringify({ checks }, null, 2))
const failed = checks.filter((c) => !c.ok).length
console.log(failed ? `有 ${failed} 项没通过` : `全部 ${checks.length} 项通过`)
process.exit(failed ? 1 : 0)
