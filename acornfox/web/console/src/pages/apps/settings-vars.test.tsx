// 应用设置里的“安装设置”一行：整理（lib/app-vars.ts）和整页里的样子。
// 用假的接口（替换 fetch）把控制台渲染出来；没登记的接口回 404，和旧服务端一样。
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { groupVars, shownNotes, varLabel, varsSummary, type AppVar, type AppVars } from '@/lib/app-vars'
import { parseHash } from '@/lib/router'
import type { ServiceView } from '@/lib/types'
import { app, catalog, resources } from '@/test/fixtures'

const FIX = '要让应用用上现在的值：删了重装（保留数据）'
const STALE_NOTE = 'APP-VARS-01 platform.app.url 装时的值和现在算出来的不一样（value 是装时的、应用现在用的，value_now 是现在的）：' + FIX

const row = (over: Partial<AppVar>): AppVar => ({ name: 'params.x', layer: 'custom', label: '某项', value: 'v', secret: false, masked: false, origin: 'default', origin_label: '清单默认值', ...over })

const url = row({ name: 'platform.app.url', layer: 'system', label: '这个应用的访问地址', value: 'http://127.0.0.1:18810', origin: 'platform', origin_label: '平台算的', value_now: 'http://192.168.31.64:18810', differs: true, code: 'APP-VARS-01', fix: FIX })
const urlSame = { ...url, value: 'http://192.168.31.64:18810', differs: false, code: undefined, fix: undefined }
const VARS: AppVar[] = [
  row({ name: 'params.admin_email', label: '管理员邮箱', value: 'admin@example.com', origin: 'user', origin_label: '用户填的' }),
  row({ name: 'params.encryption_key', label: '加密密钥', value: '******a3f9', secret: true, masked: true, origin: 'generated', origin_label: '平台生成的' }),
  row({ name: 'params.prune_days', label: '每周清理旧记录', value: '14', origin: 'default' }),
  row({ name: 'params.smtp_password', label: '发信密码', value: '******9z8y', secret: true, masked: true, origin: 'user', origin_label: '用户填的' }),
  urlSame,
  row({ name: 'platform.region', layer: 'system', label: '网络区域（cn 或 global）', value: 'cn', origin: 'platform' }),
  row({ name: 'platform.tz', layer: 'system', label: '时区', value: 'Asia/Shanghai', origin: 'platform' }),
]
const REVEALED: Record<string, string> = { 'params.encryption_key': 'k-0123456789a3f9', 'params.smtp_password': 'pw-12345679z8y' }
const COMMON_NOTES = ['改设置的功能还没有：这里只能看，不能改。能改的平台变量只在安装时用 --set platform.名字=值 改', '密码和 Key 只显示后 4 位（不到 12 个字符的全遮）；要看全加 --reveal（接口 ?reveal=true）']

function varsOf(over: Partial<AppVars> = {}, list: AppVar[] = VARS): AppVars {
  return { app: 'n8n', from_catalog: true, catalog_id: 'n8n', version: '1.0', revealed: false, can_change: false, vars: list, notes: [...COMMON_NOTES], ...over }
}
const differ = () => varsOf({ notes: [STALE_NOTE, ...COMMON_NOTES] }, VARS.map((v) => (v.name === url.name ? url : v)))

describe('整理安装设置', () => {
  it('名字去掉括号里的补充，没有中文名时不显示清单里的名字', () => {
    expect(varLabel(row({ label: '网络区域（cn 或 global）' }))).toBe('网络区域')
    expect(varLabel(row({ label: '', name: 'params.secret_x' }))).toBe('secret_x')
    expect(varLabel(row({ label: '', name: '' }))).toBe('（未命名的项）')
    expect(varLabel(row({ label: '这个应用访问地址里的主机（不带端口）', name: 'platform.app.host' }))).toBe('访问地址里的主机')
  })
  it('分组：用户填的默认列出，其余按平台填的、自定义的分两组；不一样的单独挑出来', () => {
    const g = groupVars(differ().vars)
    expect(g.mine.map((v) => v.name)).toEqual(['params.admin_email', 'params.smtp_password'])
    expect(g.platform.map((v) => v.name)).toEqual(['platform.app.url', 'platform.region', 'platform.tz'])
    // 平台生成、用户不用记的密钥（加密密钥）不列
    expect(g.custom.map((v) => v.name)).toEqual(['params.prune_days'])
    expect(g.differs.map((v) => v.name)).toEqual(['platform.app.url'])
    // 安装时改过的平台给的值也算用户填的
    expect(groupVars([row({ layer: 'system', origin: 'platform_user' })]).mine.length).toBe(1)
  })
  it('收起时那一句：都没变、一项不一样、几项不一样', () => {
    expect(varsSummary(varsOf())).toEqual({ text: '都是自动配好的，不用管', tone: '' })
    expect(varsSummary(differ())).toEqual({ text: '打开的地址和现在的不一样', tone: 'warning' })
    const tz = row({ name: 'platform.tz', layer: 'system', label: '时区', value: 'Asia/Shanghai', value_now: 'UTC', differs: true, origin: 'platform' })
    expect(varsSummary(varsOf({}, [tz, url])).text).toBe('打开的地址等 2 项和现在的不一样')
    // 访问地址里的主机、协议并进“打开的地址”，不单独算一项
    expect(varsSummary(varsOf({}, [{ ...url, name: 'platform.app.host', label: '这个应用访问地址里的主机' }, url])).text).toBe('打开的地址和现在的不一样')
  })
  it('服务端的说明：页面上已经有的（不一样那条、只能看、只显示后 4 位）不重复列，其余原样', () => {
    const legacy = '这个应用是用旧格式的清单装的：照常运行'
    expect(shownNotes(varsOf({ notes: [STALE_NOTE, legacy, ...COMMON_NOTES] }, [url]))).toEqual([legacy])
  })
})

let routes: Record<string, () => unknown>
let calls: string[]

beforeEach(() => {
  calls = []
  const pg: ServiceView = { name: 'postgres16', kind: 'postgres', kind_label: '数据库', source: 'app', app: 'postgres16', status: 'ok', values: [], used_by: [{ app: 'n8n', use: 'db' }] }
  routes = {
    'GET /v1/console/session': () => ({ csrf: 'tok', expires_at: '', server: 'home-server' }),
    'GET /v1/apps': () => ({ apps: [] }),
    'GET /v1/catalog/install': () => ({ tasks: [] }),
    'GET /v1/catalog': () => catalog,
    'GET /v1/resources': () => resources,
    'GET /v1/status': () => ({ runner: 'ok', docker_version: '27.3' }),
    'GET /v1/apps/n8n': () => app({ name: 'n8n', catalog_id: 'n8n', exposure: 'lan', url: 'http://192.168.31.64:18810' }),
    'GET /v1/apps/web': () => app({ name: 'web' }),
    'GET /v1/apps/n8n/vars': () => varsOf(),
    'GET /v1/apps/n8n/vars?reveal=true': () => varsOf({ revealed: true }, VARS.map((v) => (REVEALED[v.name] ? { ...v, value: REVEALED[v.name], masked: false } : v))),
    'GET /v1/services': () => ({ services: [pg] }),
  }
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    const u = String(input)
    calls.push(u)
    const h = routes['GET ' + u] || routes['GET ' + u.split('?')[0]]
    if (!h) return new Response('404 page not found', { status: 404 })
    return new Response(JSON.stringify(h()), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
})

afterEach(() => vi.unstubAllGlobals())

async function openSettings(name = 'n8n', sub = '') {
  location.hash = '#app-' + name + '/settings' + (sub ? '/' + sub : '')
  render(<App />)
  return screen.findByTestId('app-settings')
}

const sections = (set: HTMLElement) => Array.from(set.querySelectorAll('[data-section]')).map((x) => x.getAttribute('data-section'))

describe('设置里的安装设置', () => {
  it('地址 #app-名字/settings/vars 认得', () => {
    expect(parseHash('#app-n8n/settings/vars')).toEqual({ page: 'app', id: 'n8n', tab: 'settings', section: 'vars', query: {} })
  })

  it('收起：放在“谁能打开”下面，都没变时一句“都是自动配好的，不用管”；没有“变量”和清单里的名字', async () => {
    const set = await openSettings()
    const sum = await within(set).findByTestId('summary-vars')
    expect(sum.textContent).toBe('都是自动配好的，不用管')
    // 从应用中心装的应用没有“开发者设置”那一行
    expect(sections(set)).toEqual(['access', 'vars', 'limits', 'data', 'delete'])
    expect(within(set).getByRole('button', { name: /安装设置/ }).getAttribute('aria-expanded')).toBe('false')
    expect(set.querySelector('[data-section="vars"]')!.textContent).not.toMatch(/变量|platform\.|params\./)
  })

  it('收起：有一项和现在不一样时带黄点写是哪一项', async () => {
    routes['GET /v1/apps/n8n/vars'] = differ
    const set = await openSettings()
    const sum = await within(set).findByTestId('summary-vars')
    expect(sum.textContent).toBe('打开的地址和现在的不一样')
    expect(sum.parentElement!.querySelector('.bg-warning')).not.toBeNull()
  })

  it('展开：不一样的项一个提示框（装时、现在、服务端的怎么办和编号）；默认只列用户填的和连着谁；其余收在“全部显示”', async () => {
    routes['GET /v1/apps/n8n/vars'] = () => ({ ...differ(), notes: [STALE_NOTE, '这个应用是用旧格式的清单装的', ...COMMON_NOTES] })
    const set = await openSettings('n8n', 'vars')
    const body = await within(set).findByTestId('vars-body')
    const box = within(body).getByTestId('var-differs')
    expect(box.textContent).toContain('打开的地址和现在的不一样：装的时候是 http://127.0.0.1:18810，现在应该是 http://192.168.31.64:18810。')
    expect(box.textContent).toContain('应用里记的还是装的时候的，用到它的地方可能不对。')
    expect(within(box).getByText('怎么办')).toBeTruthy()
    // 故障编号只在“复制给 AI 工作台”的文字里，页面上不写
    expect(box.textContent).not.toContain('APP-VARS-01')
    expect(within(box).getByTestId('var-fix').textContent).toBe(FIX)
    expect(within(body).getAllByTestId('var-item').map((x) => x.textContent)).toEqual(['管理员邮箱admin@example.com', '发信密码******9z8y'])
    expect((await within(body).findByTestId('var-link')).textContent).toBe('数据库用的是postgres16')
    expect(within(body).getByTestId('vars-notes').textContent).toBe('这个应用是用旧格式的清单装的')
    expect(within(body).getByText('还有 4 项是自动填好的')).toBeTruthy()
    expect(within(body).queryByText('时区')).toBeNull()
    expect(body.textContent).not.toMatch(/变量|platform\.|params\.|--set|--reveal/)
  })

  it('全部显示：分“平台自动填的”“安装设置”两组，每项中文名和值', async () => {
    const set = await openSettings('n8n', 'vars')
    const body = await within(set).findByTestId('vars-body')
    fireEvent.click(within(body).getByRole('button', { name: '全部显示' }))
    const plat = within(body).getByTestId('vars-group-平台自动填的')
    expect(within(plat).getAllByTestId('var-item').map((x) => x.textContent)).toEqual(['打开的地址http://192.168.31.64:18810', '网络区域cn', '时区Asia/Shanghai'])
    const custom = within(body).getByTestId('vars-group-安装设置')
    expect(within(custom).getAllByTestId('var-item').map((x) => x.textContent)).toEqual(['每周清理旧记录14'])
    // 不讲还没做的功能（改设置）
    expect(within(body).queryByText(/还不能在这里改/)).toBeNull()
    expect(within(body).queryByRole('button', { name: '全部显示' })).toBeNull()
  })

  it('保密项点“显示”才取完整值（带 reveal=true），再点遮住；收起这一行就没了', async () => {
    const set = await openSettings('n8n', 'vars')
    const body = await within(set).findByTestId('vars-body')
    expect(calls.some((c) => c.includes('reveal=true'))).toBe(false)
    fireEvent.click(within(body).getByRole('button', { name: '显示发信密码' }))
    expect(await within(body).findByText('pw-12345679z8y')).toBeTruthy()
    expect(calls.filter((c) => c === '/v1/apps/n8n/vars?reveal=true').length).toBe(1)
    // 平台生成、用户不用记的密钥一直不列，也不会取到完整值
    fireEvent.click(within(body).getByRole('button', { name: '全部显示' }))
    expect(within(body).queryByText('******a3f9')).toBeNull()
    expect(within(body).queryByText('k-0123456789a3f9')).toBeNull()
    fireEvent.click(within(body).getByRole('button', { name: '遮住发信密码' }))
    expect(within(body).queryByText('pw-12345679z8y')).toBeNull()
    fireEvent.click(within(body).getByRole('button', { name: '显示发信密码' }))
    expect(await within(body).findByText('pw-12345679z8y')).toBeTruthy()
    // 收起再展开：完整值没了
    fireEvent.click(within(set).getByRole('button', { name: /安装设置/ }))
    expect(within(set).queryByTestId('vars-body')).toBeNull()
    fireEvent.click(within(set).getByRole('button', { name: /安装设置/ }))
    const again = await within(set).findByTestId('vars-body')
    expect(within(again).queryByText('pw-12345679z8y')).toBeNull()
    expect(within(again).getByText('******9z8y')).toBeTruthy()
    expect(location.hash).not.toContain('reveal')
  })

  it('不是从应用中心装的应用：不读这个接口、不显示', async () => {
    const set = await openSettings('web')
    await within(set).findByTestId('summary-access')
    expect(sections(set)).not.toContain('vars')
    expect(calls.some((c) => c.includes('/vars'))).toBe(false)
  })

  it('旧服务端没有这个接口（404）：不显示', async () => {
    delete routes['GET /v1/apps/n8n/vars']
    const set = await openSettings()
    await within(set).findByTestId('summary-access')
    await vi.waitFor(() => expect(calls.some((c) => c.includes('/v1/apps/n8n/vars'))).toBe(true))
    await new Promise((r) => setTimeout(r, 20))
    expect(sections(set)).not.toContain('vars')
  })

  it('from_catalog 为 false：不显示', async () => {
    routes['GET /v1/apps/n8n/vars'] = () => varsOf({ from_catalog: false, vars: [] })
    const set = await openSettings()
    await vi.waitFor(() => expect(calls.some((c) => c.includes('/v1/apps/n8n/vars'))).toBe(true))
    await new Promise((r) => setTimeout(r, 20))
    expect(sections(set)).not.toContain('vars')
  })
  it('几项和现在不一样时只有一个提示框：先说一句，再一项一行，怎么办只出现一次', async () => {
    const host = row({ name: 'platform.tz', layer: 'system', label: '时区', value: 'Asia/Shanghai', value_now: 'UTC', differs: true, origin: 'platform', code: 'APP-VARS-01', fix: FIX })
    routes['GET /v1/apps/n8n/vars'] = () => varsOf({}, [host, url])
    location.hash = '#app-n8n/settings/vars'
    render(<App />)
    const set = await screen.findByTestId('app-settings')
    await vi.waitFor(() => expect(within(set).getByTestId('summary-vars').textContent).toBe('打开的地址等 2 项和现在的不一样'))
    const body = await within(set).findByTestId('vars-body')
    expect(within(body).getAllByTestId('var-differs').length).toBe(1)
    const box = within(body).getByTestId('var-differs')
    expect(box.textContent).toContain('有 2 项和现在的不一样。应用里记的还是装的时候的，用到它们的地方可能不对。')
    expect(within(box).getAllByTestId('var-differs-item').map((x) => x.textContent)).toEqual([
      '时区：装的时候是 Asia/Shanghai，现在应该是 UTC',
      '打开的地址：装的时候是 http://127.0.0.1:18810，现在应该是 http://192.168.31.64:18810',
    ])
    expect(within(box).getAllByTestId('var-fix').length).toBe(1)
  })

})
