// 真机走查脚本（e2e-install.mjs、record-progress-reuse.mjs）共用：要走查哪些应用、哪个配方，安装时填哪些设置项。
// 都从环境变量读，脚本里不写死；没设时用下面的默认值（现在目录里的两个应用和它们的配方，换了目录就改环境变量）。
//
//   E2E_APPS    要装的应用的目录 id，逗号分隔，按顺序一个一个装。默认 new-api,hermes-agent。
//               前面装的应用登记的服务，后面的应用装之前应该都已接上（脚本检查“需要的服务”每一项都是已接上）。
//   E2E_RECIPE  配方的目录 id。设了就用这个配方一次装好（点配方卡片的“开始”），不再一个一个装 E2E_APPS；
//               E2E_APPS 仍用来核对装好的应用。默认不设。
//   E2E_SET     安装时要填的设置项，分号分隔，每项写成 “应用id.设置项=值”（和命令行装配方时 --set 的写法一样）。
//               默认 hermes-agent.model=qwen-plus（模型名是那个应用自己的必填设置项）。设成空串表示什么都不填。
//
//   E2E_NEEDS_TIMEOUT_MS  打开安装弹窗后，等“需要的服务”出现的时限（毫秒），默认 15000。
//
// 页面上按目录里的名字找卡片（id 到名字从 GET /v1/catalog 读）；应用实例名就用目录 id（应用中心装的默认名）。
// 该查什么由数据定，不看页面上碰巧出现了什么：
//   - 目录列表里这个应用（配方时是配方里的各个应用）声明了需要的服务（uses），弹窗里就必须在时限内列出每一项，
//     并且都已接上；没出现算走查失败。没声明的不查。
//   - 装好后服务端有这个应用的首次登录信息（GET /v1/apps/{应用}/first-login，不带 reveal），弹窗里就必须有它的
//     首次登录卡片；里面有密码这类保密项的，必须默认遮住。没有首次登录信息的不查。
// 这几样判断写成下面不碰页面的小函数，单元测试在 src/lib/walk-targets.test.ts。
export const DEFAULT_APPS = 'new-api,hermes-agent'
export const DEFAULT_SET = 'hermes-agent.model=qwen-plus'

export const DEFAULT_NEEDS_TIMEOUT_MS = 15000
export const MASKED = '******'

export function walkTargets(env = process.env) {
  const apps = (env.E2E_APPS ?? DEFAULT_APPS).split(',').map((x) => x.trim()).filter(Boolean)
  const recipe = (env.E2E_RECIPE || '').trim()
  const rawTimeout = (env.E2E_NEEDS_TIMEOUT_MS || '').trim()
  const needsTimeout = rawTimeout === '' ? DEFAULT_NEEDS_TIMEOUT_MS : Number(rawTimeout)
  if (!Number.isInteger(needsTimeout) || needsTimeout <= 0) throw new Error('E2E_NEEDS_TIMEOUT_MS 要写成正整数（毫秒）：' + rawTimeout)
  const sets = []
  for (const part of (env.E2E_SET ?? DEFAULT_SET).split(';')) {
    const s = part.trim()
    if (!s) continue
    const eq = s.indexOf('=')
    const dot = s.indexOf('.')
    if (eq < 0 || dot < 0 || dot > eq) throw new Error('E2E_SET 每项要写成 应用id.设置项=值：' + s)
    sets.push({ app: s.slice(0, dot), key: s.slice(dot + 1, eq), value: s.slice(eq + 1) })
  }
  if (apps.length === 0) throw new Error('E2E_APPS 至少要有一个应用 id')
  return { apps, recipe, sets, needsTimeout }
}

// recipeApps：配方里按顺序装的应用的目录 id（cat 是 GET /v1/catalog 的应答）。
export function recipeApps(cat, id) {
  const r = (cat?.recipes || []).find((x) => x.id === id)
  if (!r) throw new Error('目录里没有配方 ' + id + '（看 E2E_RECIPE）')
  return (r.apps || []).map((x) => x.app).filter(Boolean)
}

// declaredNeeds：目录列表里声明了的需要，每项写成 “应用id.需要名”。kind 是 app 时只看这个应用，
// 是 recipe 时看配方里的各个应用。空数组表示没声明，弹窗里不该有“需要的服务”，不查。
export function declaredNeeds(cat, kind, id) {
  const byId = new Map((cat?.apps || []).map((a) => [a.id, a]))
  const ids = kind === 'recipe' ? recipeApps(cat, id) : [id]
  const out = []
  for (const appId of ids) {
    const a = byId.get(appId)
    if (!a) throw new Error('目录里没有应用 ' + appId + (kind === 'recipe' ? '（配方 ' + id + ' 里的）' : '（看 E2E_APPS）'))
    for (const u of a.uses || []) if (u && u.name) out.push(appId + '.' + u.name)
  }
  return out
}

// needsVerdict：弹窗里读到的每一行“需要的服务”的状态（data-status）和目录里声明的需要比。
// 声明了：行数要对上，并且每一行都是已接上（matched）；没声明：不查（ok 为 null）。
export function needsVerdict(declared, statuses) {
  if (declared.length === 0) return { ok: null, detail: '目录里没有声明需要的服务，不查' }
  if (statuses.length === 0) return { ok: false, detail: '目录里声明了 ' + declared.join('、') + '，弹窗里没有出现“需要的服务”' }
  if (statuses.length !== declared.length) return { ok: false, detail: '声明了 ' + declared.length + ' 项（' + declared.join('、') + '），弹窗里是 ' + statuses.length + ' 项：' + statuses.join('、') }
  const ok = statuses.every((x) => x === 'matched')
  return { ok, detail: statuses.join('、') }
}

// loginExpectation：服务端的首次登录信息（GET /v1/apps/{应用}/first-login 不带 reveal 的应答；拿不到时传 null）
// 说明弹窗里该有什么。shown：要有这个应用的首次登录卡片；secret：里面有保密项，页面上要遮住；
// maskedByServer：服务端自己没给明文（保密项的值都是 ******）。
export function loginExpectation(fl) {
  const fields = Array.isArray(fl?.fields) ? fl.fields.filter((f) => f && typeof f === 'object') : []
  const secrets = fields.filter((f) => f.secret)
  return { shown: fields.length > 0, secret: secrets.length > 0, maskedByServer: secrets.every((f) => f.value === MASKED) }
}

// catalogNames：目录里应用 id → 名字、配方 id → 标题（页面上的卡片按名字找），以及目录里声明了哪些需要（needs）、
// 配方里有哪几个应用（recipeApps）。要在登录之后调用（带会话 Cookie）。
export async function catalogNames(page) {
  const cat = await page.evaluate(async () => {
    const r = await fetch('/v1/catalog', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
    return r.json()
  })
  const apps = new Map((cat.apps || []).map((a) => [a.id, a.name]))
  const recipes = new Map((cat.recipes || []).map((r) => [r.id, r.title]))
  return {
    needs: (kind, id) => declaredNeeds(cat, kind, id),
    recipeApps: (id) => recipeApps(cat, id),
    app: (id) => {
      if (!apps.has(id)) throw new Error('目录里没有应用 ' + id + '（看 E2E_APPS）')
      return apps.get(id)
    },
    recipe: (id) => {
      if (!recipes.has(id)) throw new Error('目录里没有配方 ' + id + '（看 E2E_RECIPE）')
      return recipes.get(id)
    },
  }
}

// fillSettings：在安装弹窗里填设置项。装单个应用（forApp 是它的 id）只填这个应用的，输入框按设置项名找；
// 装配方（forApp 为空）填全部，输入框按“应用id.设置项”找。收在“更多设置”里的先展开。
export async function fillSettings(dialog, sets, forApp) {
  for (const s of sets) {
    if (forApp && s.app !== forApp) continue
    const key = forApp ? s.key : s.app + '.' + s.key
    const input = dialog.locator('#prm-' + key.replace(/[^A-Za-z0-9_-]/g, '-'))
    if (!(await input.isVisible().catch(() => false))) await dialog.getByText('更多设置（可不填）').click().catch(() => {})
    // 有选项的设置项是下拉框。
    if ((await input.evaluate((el) => el.tagName)) === 'SELECT') await input.selectOption(s.value)
    else await input.fill(s.value)
  }
}

// readNeedStatuses：等弹窗里“需要的服务”列出 want 行（最多等 timeout 毫秒），返回每一行的状态。
// 计划还在读或只列出一部分时接着等；到时限照实返回读到的（可能是空数组）。
export async function readNeedStatuses(dialog, want, timeout) {
  const rows = dialog.getByTestId('need')
  const deadline = Date.now() + timeout
  let statuses = []
  for (;;) {
    statuses = await rows.evaluateAll((els) => els.map((e) => e.getAttribute('data-status') || ''))
    if ((statuses.length >= want && statuses.every((x) => x !== '')) || Date.now() >= deadline) return statuses
    await new Promise((r) => setTimeout(r, 250))
  }
}

// firstLoginResponse：把 GET /v1/apps/{应用}/first-login 的应答（状态码和解析后的内容）换成走查要用的结果。
// 只有 404 算“这个应用没有首次登录信息”（fl 为 null、error 为空）；2xx 给出内容；其他状态码（会话丢了、
// 服务端解不开等）和 2xx 却不是 JSON 的，都写进 error，走查要算失败，不能当成“没有”悄悄跳过。
export function firstLoginResponse(status, body) {
  if (status === 404) return { fl: null, error: '' }
  if (status >= 200 && status < 300) {
    if (body && typeof body === 'object') return { fl: body, error: '' }
    return { fl: null, error: '首次登录信息的应答不是 JSON（HTTP ' + status + '）' }
  }
  return { fl: null, error: '读首次登录信息失败：HTTP ' + status }
}

// fetchFirstLogin：服务端这个应用的首次登录信息（不带 reveal，不会拿到明文），按 firstLoginResponse 给出结果。
export async function fetchFirstLogin(page, app) {
  const r = await page.evaluate(async (name) => {
    const res = await fetch('/v1/apps/' + encodeURIComponent(name) + '/first-login', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
    let body = null
    try {
      body = await res.json()
    } catch {
      body = null
    }
    return { status: res.status, body }
  }, app)
  return firstLoginResponse(r.status, r.body)
}

// checkFirstLogin：照 loginExpectation 查弹窗里这个应用的首次登录卡片。返回 null 表示这个应用没有首次登录信息（接口 404）、不查；
// 否则返回 { shown, masked }（ok 为 false 时 detail 写原因；接口出错时 shown.ok 为 false）。
export async function checkFirstLogin(page, dialog, app) {
  const got = await fetchFirstLogin(page, app)
  if (got.error) return { shown: { ok: false, detail: app + ' ' + got.error }, masked: null }
  const exp = loginExpectation(got.fl)
  if (!exp.shown) return null
  const card = dialog.getByTestId('first-login').filter({ hasText: '首次登录 · ' + app })
  const has = (await card.count()) > 0
  const shown = { ok: has, detail: has ? '' : '服务端有 ' + app + ' 的首次登录信息，弹窗里没有它的卡片' }
  if (!exp.secret) return { shown, masked: null }
  const values = has ? await card.first().locator('[data-secret="true"]').allInnerTexts() : []
  const hidden = values.filter((v) => v.trim() === MASKED).length
  const ok = exp.maskedByServer && values.length > 0 && hidden === values.length
  // 说明里只写遮住了几项，不写读到的值（没遮住时那就是明文）。
  const detail = !exp.maskedByServer ? '服务端不带 reveal 时给了明文' : values.length === 0 ? '卡片里没有保密项' : `${hidden}/${values.length} 项遮住`
  return { shown, masked: { ok, detail } }
}
