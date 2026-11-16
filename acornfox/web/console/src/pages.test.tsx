// 页面级测试（第二轮）：首页看板、运行环境、设置、应用中心的私有目录。用假的接口（替换 fetch）渲染整个控制台。
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { ConsoleProvider } from './state'
import { ToastProvider } from './components/ui/toast'
import { ConnectCatalogDialog } from './pages/store/connect-dialog'
import { certPoll } from './pages/prefs/cert-wait'
import { app, catalog, newApiDetail, resources, task } from './test/fixtures'
import { DEFAULT_LAYOUT, LAYOUT_KEY } from './lib/dashboard'
import type { CatalogView } from './lib/types'

type Handler = (url: string, init?: RequestInit) => unknown
let routes: Record<string, Handler>
let calls: { method: string; url: string; body?: unknown }[]

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

const GB = 1024 ** 3
const history = (ranges: string[]) => ({
  range: '1h', step_seconds: 30, ranges, since: ranges.length ? '2026-11-06T00:00:00Z' : undefined,
  points: ranges.length ? Array.from({ length: 6 }, (_, i) => ({ t: new Date(Date.UTC(2026, 10, 6, 0, 0, 30 * i)).toISOString(), cpu_percent: 5 + i, memory_used: 2 * GB, memory_total: 8 * GB })) : [],
})

const checks = {
  os: 'linux', checked_at: '2026-11-06T03:00:00Z', ok: false, data_dir: '/var/lib/acornfox', docker_version: '27.3.1',
  items: [
    { ok: true, level: 'ok', text: '后台服务：acornfox-server 运行中（2 个应用）' },
    { ok: true, level: 'ok', text: 'Docker：27.3.1 运行中（由执行器访问）' },
    { ok: false, level: 'fail', text: '磁盘：数据目录 /var/lib/acornfox、Docker 数据目录 /var/lib/docker 所在分区只剩 2.0 GB，不到 3.0 GB，拉镜像和构建会失败', code: 'SETUP-DISK-01' },
    { ok: true, level: 'warn', text: '端口：18832 有程序在监听，不是 root，看不到是谁占用；用 sudo acornfox doctor 可确认', code: 'SETUP-PORT-05' },
  ],
  sources: { china: true, region_source: 'install.env', docker_hub: ['docker.m.daocloud.io', 'docker.1panel.live', 'registry-1.docker.io'], ghcr: ['ghcr.nju.edu.cn'], gateway: 'https://dl.acornfox.com' },
}

const priv = { id: 'cat-demo', name: '演示客户专用' }
const withPrivate: CatalogView = {
  ...catalog,
  apps: [...catalog.apps.map((a) => ({ ...a, source: { id: 'public', name: '公共目录' } })), { ...catalog.apps[1], name: 'New API 定制版', source: priv }],
  recipes: catalog.recipes,
  sources: [
    { id: 'public', kind: 'public', name: '公共目录', serial: 1, origin: 'snapshot', apps: 2, recipes: 3, disconnected: false },
    { id: 'cat-demo', kind: 'private', name: '演示客户专用', serial: 2, apps: 1, recipes: 0, disconnected: false },
    { id: 'cat-old', kind: 'private', name: '旧客户', serial: 0, apps: 0, recipes: 0, disconnected: true },
  ],
}

beforeEach(() => {
  calls = []
  try { localStorage.clear() } catch { /* 没有 */ }
  routes = {
    'GET /v1/console/session': () => ({ csrf: 'tok', expires_at: '', server: 'home-server' }),
    'GET /v1/apps': () => ({ apps: [app({ name: 'new-api', catalog_id: 'new-api' }), app({ name: 'blog', observed_state: 'missing' })] }),
    'GET /v1/catalog/install': () => ({ tasks: [] }),
    'GET /v1/catalog': () => catalog,
    'GET /v1/resources': () => resources,
    'GET /v1/status': () => ({ runner: 'ok', docker_version: '27.3.1', version: '0.5.0-dev.2' }),
    'GET /v1/metrics/host': () => ({ available: true, cpu_percent: 9, memory_used: 2 * GB, memory_total: 8 * GB, disk_used: 30 * GB, disk_total: 40 * GB }),
    'GET /v1/metrics/host/history': () => history(['1h', '24h']),
    'GET /v1/metrics/apps/new-api': () => ({ app: 'new-api', available: true, observed_state: 'running', cpu_percent: 1.5, memory_usage_mb: 210, memory_limit_mb: 512 }),
    'GET /v1/apps/new-api/deployments': () => ({ deployments: [{ id: 'd1', app: 'new-api', seq: 1, source_kind: 'image', source_ref: 'x', status: 'live', created_at: '2026-11-06T02:00:00Z', finished_at: '2026-11-06T02:01:00Z' }] }),
    'GET /v1/apps/blog/deployments': () => ({ deployments: [] }),
    'GET /v1/runtime/checks': () => checks,
    'GET /v1/setup/faults': () => ({ faults: [{ code: 'SETUP-DISK-01', title: '磁盘剩余空间不够', causes: ['旧的镜像占了空间'], next: '先运行 docker system df 看占用', needs_user: false }] }),
    'GET /v1/lan-console': () => ({ enabled: true, url: 'http://192.168.1.10:18802', port: 18802 }),
    'GET /v1/public-console': () => ({ enabled: false }),
    'GET /v1/catalog/apps/new-api': () => newApiDetail,
  }
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    const method = init?.method || 'GET'
    const url = String(input)
    calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    const h = routes[method + ' ' + url] || routes[method + ' ' + url.split('?')[0]]
    if (!h) return json(404, { error: { code: 'not_found', message: '对象不存在' } })
    const v = h(url, init)
    return v instanceof Response ? v : json(200, v)
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('首页看板', () => {
  it('按默认布局显示，数据来自真接口；时间范围只给服务端有的', async () => {
    location.hash = '#home'
    render(<App />)
    await screen.findByRole('heading', { name: '首页' })
    expect(screen.getAllByTestId('widget').length).toBe(DEFAULT_LAYOUT.length)
    const tabs = await screen.findByRole('tablist', { name: '时间范围' })
    expect(within(tabs).getAllByRole('tab').map((t) => t.textContent)).toEqual(['1 小时', '24 小时'])
    expect(screen.queryByText('7 天')).toBeNull()
    await screen.findByText('blog 没在运行')
    await screen.findByText(/磁盘 有问题/)
    await screen.findByText('new-api 第 1 版上线')
    fireEvent.click(within(tabs).getByRole('tab', { name: '24 小时' }))
    await vi.waitFor(() => expect(calls.some((c) => c.url === '/v1/metrics/host/history?range=24h')).toBe(true))
  })

  it('服务端没有历史：不显示时间范围，CPU 曲线说明没有记录，也不能再添加', async () => {
    routes['GET /v1/metrics/host/history'] = () => history([])
    location.hash = '#home'
    render(<App />)
    await screen.findByRole('heading', { name: '首页' })
    expect(screen.queryByRole('tablist', { name: '时间范围' })).toBeNull()
    expect((await screen.findAllByText('这台服务器没有记录历史')).length).toBeGreaterThan(0)
    fireEvent.click(screen.getByRole('button', { name: /编辑/ }))
    fireEvent.click(screen.getAllByRole('button', { name: /添加图表/ })[0])
    const sheet = await screen.findByRole('dialog', { name: '添加图表' })
    expect(within(sheet).queryByRole('button', { name: 'CPU 使用率' })).toBeNull()
    expect(within(sheet).getByRole('button', { name: '各应用内存' })).toBeTruthy()
  })

  it('编辑：添加、换图表方式、移除，布局存在这个浏览器里', async () => {
    location.hash = '#home'
    render(<App />)
    await screen.findByRole('heading', { name: '首页' })
    fireEvent.click(screen.getByRole('button', { name: /编辑/ }))
    fireEvent.click(screen.getAllByRole('button', { name: /添加图表/ })[0])
    const sheet = await screen.findByRole('dialog', { name: '添加图表' })
    fireEvent.click(within(sheet).getByRole('button', { name: '各应用 CPU' }))
    fireEvent.click(within(sheet).getByRole('button', { name: /环形图/ }))
    fireEvent.click(within(sheet).getByRole('button', { name: '添加' }))
    expect(screen.getAllByTestId('widget').length).toBe(DEFAULT_LAYOUT.length + 1)
    let saved = JSON.parse(localStorage.getItem(LAYOUT_KEY) || '[]')
    expect(saved[saved.length - 1]).toMatchObject({ s: 'appCpu', t: 'donut', w: 6, h: 2 })

    const first = screen.getAllByTestId('widget')[0]
    fireEvent.click(within(first).getByRole('button', { name: '换图表方式' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '表格' }))
    saved = JSON.parse(localStorage.getItem(LAYOUT_KEY) || '[]')
    expect(saved[0]).toMatchObject({ s: 'appStatus', t: 'table' })

    fireEvent.click(within(screen.getAllByTestId('widget')[0]).getByRole('button', { name: '更多' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '移除' }))
    expect(screen.getAllByTestId('widget').length).toBe(DEFAULT_LAYOUT.length)
    fireEvent.click(screen.getByRole('button', { name: '恢复默认' }))
    expect(JSON.parse(localStorage.getItem(LAYOUT_KEY) || '[]')).toEqual(DEFAULT_LAYOUT)
  })
})

describe('运行环境', () => {
  it('每项带故障编号，点“怎么办”看建议；不显示安装步骤', async () => {
    location.hash = '#runtime'
    render(<App />)
    await screen.findByRole('heading', { name: '运行环境' })
    const rows = await screen.findAllByTestId('runtime-check')
    expect(rows.length).toBe(4)
    expect(screen.getByText('2 项要处理')).toBeTruthy()
    expect(screen.getByText('SETUP-DISK-01')).toBeTruthy()
    expect(screen.getByText('先运行 docker system df 看占用')).toBeTruthy() // 不通过的默认展开
    expect(screen.getByText('SETUP-PORT-05')).toBeTruthy()
    expect(screen.getByText('sudo acornfox doctor')).toBeTruthy()
    expect(screen.queryByText(/安装 Docker|WSL|OrbStack/)).toBeNull()
    expect(screen.getByText(/SETUP-DISK-01 磁盘/)).toBeTruthy() // 发给 AI 的文字
  })

  it('旧服务端没有自检接口时提示用 doctor', async () => {
    delete routes['GET /v1/runtime/checks']
    location.hash = '#runtime'
    render(<App />)
    await screen.findByText(/在服务器上运行 sudo acornfox doctor/)
  })

  it('自检接口出错（带编号）和连不上后台服务分开说，都指向 doctor', async () => {
    routes['GET /v1/runtime/checks'] = () => json(500, { error: { code: 'internal', message: '读应用列表失败' } })
    location.hash = '#runtime'
    const { unmount } = render(<App />)
    await screen.findByText('自检没做完：internal 读应用列表失败。可以在服务器上运行 sudo acornfox doctor。')
    unmount()
    routes['GET /v1/runtime/checks'] = () => { throw new TypeError('Failed to fetch') }
    render(<App />)
    await screen.findByText('连不上后台服务，没能自检。请在服务器上运行 sudo acornfox doctor。')
    expect(screen.queryByText(/Failed to fetch/)).toBeNull()
  })
})

describe('设置', () => {
  it('通用：主色四选一，选了就换 data-accent 并记在浏览器里，选回中性就去掉', async () => {
    location.hash = '#prefs'
    render(<App />)
    const group = await screen.findByRole('radiogroup', { name: '主色' })
    const radios = within(group).getAllByRole('radio')
    expect(radios.map((r) => r.getAttribute('aria-label'))).toEqual(['主色：中性', '主色：蓝', '主色：绿', '主色：橙'])
    expect(radios[0].getAttribute('aria-checked')).toBe('true')
    expect(document.documentElement.hasAttribute('data-accent')).toBe(false)
    fireEvent.click(radios[1])
    expect(document.documentElement.getAttribute('data-accent')).toBe('blue')
    expect(localStorage.getItem('afx-accent')).toBe('blue')
    expect(within(group).getByRole('radio', { name: '主色：蓝' }).getAttribute('aria-checked')).toBe('true')
    fireEvent.click(within(group).getByRole('radio', { name: '主色：中性' }))
    expect(document.documentElement.hasAttribute('data-accent')).toBe(false)
    expect(localStorage.getItem('afx-accent')).toBeNull()
    expect(calls.some((c) => c.method !== 'GET')).toBe(false)
  })

  // 设置 › 访问：一行一项、点开才展开。row(名字) 找到那一行能点的按钮（名字加现状）。
  const row = (name: string) => screen.getByRole('button', { name: new RegExp('^' + name) })
  const DOMAIN_OK = {
    domain: 'apps.example.com', state: 'ok', supported: true, public_ip: '203.0.113.10',
    record: { name: '*.apps', type: 'A', value: '203.0.113.10' },
    checks: [{ key: 'dns', ok: true, text: '域名指过来了' }, { key: 'ports', ok: true, text: '外面能连上' }, { key: 'cert', ok: true, text: '证书自动申请' }],
    console_host: 'console.apps.example.com',
  }
  const DOMAIN_UNSET = { state: 'unset', supported: true, public_ip: '203.0.113.10', record: { type: 'A', value: '203.0.113.10' }, checks: [] }
  const DOMAIN_PROBLEM = {
    ...DOMAIN_OK, state: 'problem',
    checks: [{ key: 'dns', ok: false, text: '域名没有指到这台机器' }, { key: 'ports', ok: false, text: '等上一项' }, { key: 'cert', ok: false, text: '等上一项' }],
    problem: { code: 'PDOMAIN-01', text: '域名没有指到这台机器', detail: '现在指向 198.51.100.7，应该指向 203.0.113.10', next: '到你买域名的地方，把 *.apps 这条记录改成 203.0.113.10，等几分钟再检查' },
  }

  it('访问：一行一项，都收起；点开才展开、同一时间只展开一项；旧服务端没有平台域名接口时不显示域名这一行，公网控制台只给命令', async () => {
    location.hash = '#prefs/access'
    render(<App />)
    expect((await screen.findByTestId('summary-lan')).textContent).toBe('已开启 · 192.168.1.10:18802')
    expect(screen.getByTestId('summary-pub').textContent).toBe('没开启。开启后人在外面也能用浏览器打开')
    await vi.waitFor(() => expect(calls.some((c) => c.url === '/v1/platform/domain')).toBe(true))
    expect(screen.queryByTestId('summary-domain')).toBeNull()
    expect(screen.queryByRole('button', { name: '开启' })).toBeNull()
    expect(screen.queryByText('acornfox lan-console disable')).toBeNull()
    fireEvent.click(row('在外面打开控制台'))
    expect(row('在外面打开控制台').getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByText('acornfox public-console enable 你的域名')).toBeTruthy()
    expect(screen.getByText('acornfox open')).toBeTruthy()
    expect(screen.queryByLabelText('公网控制台的域名')).toBeNull()
    fireEvent.click(row('局域网里打开控制台'))
    expect(row('在外面打开控制台').getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByText('acornfox open')).toBeNull()
    expect(screen.getByText('acornfox lan-console disable')).toBeTruthy()
    fireEvent.click(row('局域网里打开控制台'))
    expect(screen.queryByText('acornfox lan-console disable')).toBeNull()
    expect(calls.some((c) => c.method !== 'GET')).toBe(false)
  })

  it('访问：局域网控制台显示密码是何时从哪设的、记住的设备，能移除一台；清密码只给命令', async () => {
    routes['GET /v1/lan-console'] = () => ({ enabled: true, url: 'http://192.168.1.10:18802', port: 18802, password_set: true, password_set_at: '2026-11-10T03:00:00Z', password_set_from: '192.168.1.23', devices: 2 })
    let devices = [
      { id: 'd1', name: 'Mac · Chrome', created_at: '2026-11-10T03:00:00Z', last_used_at: '2026-11-10T03:05:00Z', last_ip: '192.168.1.23', expires_at: '2027-02-08T03:00:00Z' },
      { id: 'd2', name: 'iPhone · Safari', created_at: '2026-11-10T04:00:00Z', last_used_at: '2026-11-10T04:00:00Z', last_ip: '192.168.1.40', expires_at: '2027-02-08T04:00:00Z' },
    ]
    routes['GET /v1/lan-console/devices'] = () => ({ devices })
    routes['DELETE /v1/lan-console/devices/d2'] = () => { devices = devices.filter((d) => d.id !== 'd2'); return { ok: true, removed: true } }
    location.hash = '#prefs/access'
    render(<App />)
    expect((await screen.findByTestId('summary-lan')).textContent).toBe('已开启 · 192.168.1.10:18802 · 记住了 2 台设备')
    fireEvent.click(row('局域网里打开控制台'))
    expect(await screen.findByText(/从 192\.168\.1\.23 设的/)).toBeTruthy()
    expect(screen.getByText('acornfox lan-console reset-password')).toBeTruthy()
    expect(screen.getByText('acornfox lan-console disable')).toBeTruthy()
    const box = await screen.findByTestId('lan-devices')
    expect(within(box).getByText('Mac · Chrome')).toBeTruthy()
    expect(within(box).getByText(/192\.168\.1\.40 · 2027-02-08 到期/)).toBeTruthy()
    fireEvent.click(within(box).getByRole('button', { name: '移除 iPhone · Safari' }))
    await vi.waitFor(() => expect(within(box).queryByText('iPhone · Safari')).toBeNull())
    const del = calls.find((c) => c.method === 'DELETE')
    expect(del?.url).toBe('/v1/lan-console/devices/d2')
    expect(calls.filter((c) => c.method !== 'GET').length).toBe(1)
  })

  it('访问：局域网控制台开着但还没设密码时提醒现在就去设；没开时不显示设备和清密码', async () => {
    routes['GET /v1/lan-console'] = () => ({ enabled: true, url: 'http://192.168.1.10:18802', port: 18802, password_set: false, devices: 0 })
    routes['GET /v1/lan-console/devices'] = () => ({ devices: [] })
    location.hash = '#prefs/access'
    const first = render(<App />)
    expect((await screen.findByTestId('summary-lan')).textContent).toBe('已开启 · 192.168.1.10:18802 · 还没设密码')
    fireEvent.click(row('局域网里打开控制台'))
    expect(await screen.findByText(/还没设密码：请现在就用局域网里的电脑打开它设一个/)).toBeTruthy()
    first.unmount()
    routes['GET /v1/lan-console'] = () => ({ enabled: false, port: 18802 })
    render(<App />)
    expect((await screen.findByTestId('summary-lan')).textContent).toBe('没开启。局域网里的电脑、手机打不开控制台')
    fireEvent.click(row('局域网里打开控制台'))
    await screen.findByText('acornfox lan-console enable')
    expect(screen.queryByTestId('lan-devices')).toBeNull()
    expect(screen.queryByText('acornfox lan-console reset-password')).toBeNull()
  })

  it('域名·正常：收起一句话；展开有说明和三个勾；“不用域名了”先确认再调 DELETE，回到“还没有”', async () => {
    routes['GET /v1/platform/domain'] = () => DOMAIN_OK
    routes['DELETE /v1/platform/domain'] = () => DOMAIN_UNSET
    location.hash = '#prefs/access'
    render(<App />)
    expect((await screen.findByTestId('summary-domain')).textContent).toBe('apps.example.com · 正常')
    expect(screen.queryByText('域名指过来了')).toBeNull()
    expect(screen.queryByRole('button', { name: '配置' })).toBeNull()
    fireEvent.click(row('域名'))
    expect(screen.getByText('应用名.apps.example.com')).toBeTruthy()
    for (const t of ['域名指过来了', '外面能连上', '证书自动申请']) expect(screen.getByText(t)).toBeTruthy()
    expect(screen.queryByRole('button', { name: '重新检查' })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '不用域名了' }))
    const dialog = await screen.findByRole('dialog', { name: '不用域名了？' })
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    fireEvent.click(within(dialog).getByRole('button', { name: '不用了' }))
    // 服务端要求带上“已确认”，不带会被拒（确认框就是页面上的确认）
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/v1/platform/domain?confirm=true')).toBe(true))
    await vi.waitFor(() => expect(screen.getByTestId('summary-domain').textContent).toBe('还没有。配一个域名，应用就不用记数字地址了'))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('域名·正常：“换一个域名”在弹窗里填新域名，调 PUT；记录的名字跟着填的域名变', async () => {
    routes['GET /v1/platform/domain'] = () => DOMAIN_OK
    routes['PUT /v1/platform/domain'] = (_u, init) => ({ ...DOMAIN_OK, domain: JSON.parse(String(init?.body)).domain })
    location.hash = '#prefs/access'
    render(<App />)
    await screen.findByTestId('summary-domain')
    fireEvent.click(row('域名'))
    fireEvent.click(screen.getByRole('button', { name: '换一个域名' }))
    const dialog = await screen.findByRole('dialog', { name: '换一个域名' })
    const save = within(dialog).getByRole('button', { name: '换成这个' }) as HTMLButtonElement
    expect(save.disabled).toBe(true)
    fireEvent.change(within(dialog).getByLabelText('平台域名'), { target: { value: ' https://Home.MyCo.cn/ ' } })
    expect(within(within(dialog).getByTestId('domain-record')).getByText('*.home')).toBeTruthy()
    fireEvent.click(save)
    // 换掉已有的域名要带“已确认”；第一次设不带（见下面“还没有”那一条）
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'PUT')?.body).toEqual({ domain: 'home.myco.cn', confirm: true }))
    await vi.waitFor(() => expect(screen.getByTestId('summary-domain').textContent).toBe('home.myco.cn · 正常'))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('域名·还没有：一句话加“配置”按钮；展开是两步（记录的名字、类型、指向直接给出来），保存调 PUT；没生效时变成红字', async () => {
    routes['GET /v1/platform/domain'] = () => DOMAIN_UNSET
    routes['PUT /v1/platform/domain'] = () => DOMAIN_PROBLEM
    location.hash = '#prefs/access'
    render(<App />)
    expect((await screen.findByTestId('summary-domain')).textContent).toBe('还没有。配一个域名，应用就不用记数字地址了')
    fireEvent.click(screen.getByRole('button', { name: '配置' }))
    expect(row('域名').getAttribute('aria-expanded')).toBe('true')
    expect(screen.queryByRole('button', { name: '配置' })).toBeNull()
    expect(screen.getByText('第一步')).toBeTruthy()
    const rec = screen.getByTestId('domain-record')
    expect(within(rec).getByText('*.apps')).toBeTruthy()
    expect(within(rec).getByText('A')).toBeTruthy()
    expect(within(rec).getByText('203.0.113.10')).toBeTruthy()
    expect(screen.getByText(/以 apps\.example\.com 为例/)).toBeTruthy()
    fireEvent.change(screen.getByLabelText('平台域名'), { target: { value: 'my.home.example.com' } })
    expect(within(rec).getByText('*.my.home')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'PUT' && c.url === '/v1/platform/domain')?.body).toEqual({ domain: 'my.home.example.com' }))
    const sum = screen.getByTestId('summary-domain')
    await vi.waitFor(() => expect(sum.textContent).toBe('apps.example.com · 还没生效：域名没有指到这台机器'))
    expect(sum.className).toMatch(/text-danger/)
  })

  it('域名·没生效：展开写现在指向哪、该改成什么、故障编号；“重新检查”调 check，过了变成正常', async () => {
    routes['GET /v1/platform/domain'] = () => DOMAIN_PROBLEM
    routes['POST /v1/platform/domain/check'] = () => DOMAIN_OK
    location.hash = '#prefs/access'
    render(<App />)
    expect((await screen.findByTestId('summary-domain')).textContent).toBe('apps.example.com · 还没生效：域名没有指到这台机器')
    fireEvent.click(row('域名'))
    expect(screen.getByText('现在指向 198.51.100.7，应该指向 203.0.113.10')).toBeTruthy()
    expect(screen.getByText(/把 \*\.apps 这条记录改成 203\.0\.113\.10/)).toBeTruthy()
    expect(screen.getByText('PDOMAIN-01')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '重新检查' }))
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/platform/domain/check')).toBe(true))
    await vi.waitFor(() => expect(screen.getByTestId('summary-domain').textContent).toBe('apps.example.com · 正常'))
  })

  it('域名·这台机器用不了（局域网机器）：一句话，展开说明原因和编号，不给输入框', async () => {
    routes['GET /v1/platform/domain'] = () => ({ state: 'unset', supported: false, unsupported: { code: 'PDOMAIN-02', text: '这台机器在局域网里，没有公网地址，用不了域名' } })
    location.hash = '#prefs/access'
    render(<App />)
    expect((await screen.findByTestId('summary-domain')).textContent).toBe('这台机器在局域网里，没有公网地址，用不了域名')
    expect(screen.queryByRole('button', { name: '配置' })).toBeNull()
    fireEvent.click(row('域名'))
    expect(screen.getByText('PDOMAIN-02')).toBeTruthy()
    expect(screen.getByText(/证书要从公网验证/)).toBeTruthy()
    expect(screen.queryByLabelText('平台域名')).toBeNull()
  })

  it('在外面打开控制台·开启（要再输密码）：确认框三句后果；密码不对留在框里；成功后新密码显示一次', async () => {
    let enabled = false
    let tries = 0
    routes['GET /v1/public-console'] = () => (enabled
      ? { enabled: true, domain: 'console.apps.example.com', url: 'https://console.apps.example.com', can_toggle: true, needs_password: true }
      : { enabled: false, can_toggle: true, needs_password: true, suggested_domain: 'console.apps.example.com' })
    routes['POST /v1/public-console/enable'] = () => {
      tries++
      if (tries === 1) return json(403, { error: { code: 'PUBCON-01', message: '控制台密码不对' } })
      enabled = true
      return { domain: 'console.apps.example.com', url: 'https://console.apps.example.com', password: 'N3w-Pass-123' }
    }
    location.hash = '#prefs/access'
    render(<App />)
    await screen.findByTestId('summary-pub')
    fireEvent.click(screen.getByRole('button', { name: '开启' }))
    expect(row('在外面打开控制台').getAttribute('aria-expanded')).toBe('true')
    expect((screen.getByLabelText('公网控制台的域名') as HTMLInputElement).value).toBe('console.apps.example.com')
    expect(screen.getByText('已经按上面的域名填好了，也可以换成自己的域名。')).toBeTruthy()
    expect(screen.getByText('acornfox open')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '开启公网控制台' }))
    const dialog = await screen.findByRole('dialog', { name: '开启公网控制台？' })
    expect(within(dialog).getByText('地址：https://console.apps.example.com')).toBeTruthy()
    expect(within(dialog).getByText('互联网上任何人都能打开登录页，只靠密码挡着。')).toBeTruthy()
    expect(within(dialog).getByText('会生成一个新密码，只显示这一次，请马上记下。')).toBeTruthy()
    expect(within(dialog).getByText('随时可以回到这里关闭。')).toBeTruthy()
    const ok = within(dialog).getByRole('button', { name: '开启' }) as HTMLButtonElement
    expect(ok.disabled).toBe(true)
    fireEvent.change(within(dialog).getByLabelText('再输一次现在的控制台密码'), { target: { value: 'wrong' } })
    fireEvent.click(ok)
    expect((await within(dialog).findByRole('alert')).textContent).toBe('控制台密码不对（PUBCON-01）')
    expect(screen.queryByTestId('pub-new-password')).toBeNull()
    fireEvent.change(within(dialog).getByLabelText('再输一次现在的控制台密码'), { target: { value: 'old-pass' } })
    fireEvent.click(ok)
    const pw = await screen.findByTestId('pub-new-password')
    expect(pw.textContent).toContain('N3w-Pass-123')
    expect(screen.getByText(/只显示这一次，请马上记下来/)).toBeTruthy()
    const posts = calls.filter((c) => c.method === 'POST' && c.url === '/v1/public-console/enable')
    expect(posts.map((c) => c.body)).toEqual([{ domain: 'console.apps.example.com', password: 'wrong' }, { domain: 'console.apps.example.com', password: 'old-pass' }])
    expect(screen.queryByRole('dialog', { name: '开启公网控制台？' })).toBeNull()
    await vi.waitFor(() => expect(screen.getByTestId('summary-pub').textContent).toBe('已开启 · console.apps.example.com · 要密码登录'))
    fireEvent.click(screen.getByRole('button', { name: '我记下了' }))
    expect(screen.queryByTestId('pub-new-password')).toBeNull()
    expect(screen.queryByText('N3w-Pass-123')).toBeNull()
  })

  it('在外面打开控制台·开启（不用再输密码）：没有平台域名时自己填，发的请求不带密码；新密码也认 password_once', async () => {
    routes['GET /v1/public-console'] = () => ({ enabled: false, can_toggle: true, needs_password: false, suggested_domain: '' })
    routes['POST /v1/public-console/enable'] = () => ({ domain: 'c.mine.com', url: 'https://c.mine.com', password_once: 'Once-999' })
    location.hash = '#prefs/access'
    render(<App />)
    await screen.findByTestId('summary-pub')
    fireEvent.click(row('在外面打开控制台'))
    expect(screen.getByText(/在上面“域名”那一行配好域名，这里会自动填上/)).toBeTruthy()
    const btn = screen.getByRole('button', { name: '开启公网控制台' }) as HTMLButtonElement
    expect(btn.disabled).toBe(true)
    fireEvent.change(screen.getByLabelText('公网控制台的域名'), { target: { value: 'C.Mine.com' } })
    fireEvent.click(btn)
    const dialog = await screen.findByRole('dialog', { name: '开启公网控制台？' })
    expect(within(dialog).queryByLabelText('再输一次现在的控制台密码')).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: '开启' }))
    expect((await screen.findByTestId('pub-new-password')).textContent).toContain('Once-999')
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ domain: 'c.mine.com' })
  })

  it('在外面打开控制台·刚开启证书还在申请：弹窗写“正在申请证书”，好了改成“可以打开了”，关掉弹窗后那一行也说一声', async () => {
    certPoll.pending = 5
    let enabled = false
    let ready = false
    const on = { enabled: true, domain: 'c.mine.com', url: 'https://c.mine.com', enabled_at: '2026-11-16T01:00:00Z', can_toggle: true, needs_password: false }
    routes['GET /v1/public-console'] = () => (enabled ? on : { enabled: false, can_toggle: true, needs_password: false, suggested_domain: 'c.mine.com' })
    routes['GET /v1/public-console?check=https'] = () => (enabled
      ? { ...on, https: ready ? { code: 'ok', message: 'https://c.mine.com 已可访问，证书有效' } : { code: 'cert_pending', message: 'c.mine.com 的 443 端口已通，但证书还没有就绪', hint: '入口代理正在自动申请证书，通常一两分钟。' } }
      : { enabled: false })
    routes['POST /v1/public-console/enable'] = () => { enabled = true; return { domain: 'c.mine.com', url: 'https://c.mine.com', password: 'Pw-1' } }
    location.hash = '#prefs/access'
    render(<App />)
    await screen.findByTestId('summary-pub')
    fireEvent.click(row('在外面打开控制台'))
    expect(screen.queryByTestId('pub-cert')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '开启公网控制台' }))
    fireEvent.click(within(await screen.findByRole('dialog', { name: '开启公网控制台？' })).getByRole('button', { name: '开启' }))
    const done = await screen.findByRole('dialog', { name: '公网控制台已开启' })
    await vi.waitFor(() => expect(within(done).getByTestId('pub-cert').getAttribute('data-phase')).toBe('pending'))
    expect(within(done).getByTestId('pub-cert').textContent).toContain('正在申请证书，通常一两分钟')
    expect(within(done).getByTestId('pub-cert').textContent).toContain('现在打开，浏览器会报“连接不安全”')
    expect(within(done).queryByRole('link', { name: '打开' })).toBeNull()
    // 一直在问
    const asked = () => calls.filter((c) => c.url === '/v1/public-console?check=https').length
    const n = asked()
    await vi.waitFor(() => expect(asked()).toBeGreaterThan(n + 1))
    ready = true
    await vi.waitFor(() => expect(within(done).getByTestId('pub-cert').getAttribute('data-phase')).toBe('ready'))
    expect(within(done).getByTestId('pub-cert').textContent).toContain('证书好了，可以打开了')
    expect((within(done).getByRole('link', { name: '打开' }) as HTMLAnchorElement).getAttribute('href')).toBe('https://c.mine.com')
    // 好了以后不再问
    const m = asked()
    await new Promise((r) => setTimeout(r, 40))
    expect(asked()).toBe(m)
    fireEvent.click(within(done).getByRole('button', { name: '我记下了' }))
    expect(screen.getByTestId('pub-cert').textContent).toContain('证书好了，可以打开了')
    certPoll.pending = 3000
  })

  it('在外面打开控制台·证书状态的另外几种：没签下来给原因；服务器确认不了时说明；一直是好的、旧服务端都不多说', async () => {
    const on = { enabled: true, domain: 'c.mine.com', url: 'https://c.mine.com', enabled_at: '2026-11-16T01:00:00Z', can_toggle: true, needs_password: false }
    routes['GET /v1/public-console'] = () => on
    routes['GET /v1/public-console?check=https'] = () => ({ ...on, https: { code: 'cert_failed', message: '等待后 c.mine.com 的 HTTPS 证书仍未签发', hint: '常见原因：80 端口没有放行。' } })
    location.hash = '#prefs/access'
    const first = render(<App />)
    await screen.findByTestId('summary-pub')
    fireEvent.click(row('在外面打开控制台'))
    await vi.waitFor(() => expect(screen.getByTestId('pub-cert').getAttribute('data-phase')).toBe('failed'))
    expect(screen.getByTestId('pub-cert').textContent).toContain('证书还没签下来，地址现在打不开。服务器还在自己重试，好了这里会提示。')
    expect(screen.getByTestId('pub-cert').textContent).toContain('常见原因：80 端口没有放行。')
    first.unmount()

    routes['GET /v1/public-console?check=https'] = () => ({ ...on, https: { code: 'port_unreachable', message: '连接 c.mine.com 的 443 端口超时' } })
    const second = render(<App />)
    await screen.findByTestId('summary-pub')
    fireEvent.click(row('在外面打开控制台'))
    await vi.waitFor(() => expect(screen.getByTestId('pub-cert').getAttribute('data-phase')).toBe('unknown'))
    expect(screen.getByTestId('pub-cert').textContent).toContain('这台服务器自己确认不了证书好了没有（连接 c.mine.com 的 443 端口超时）')
    expect(screen.getByTestId('pub-cert').textContent).toContain('直接打开地址试试')
    second.unmount()

    for (const https of [{ code: 'ok' }, undefined]) {
      routes['GET /v1/public-console?check=https'] = () => ({ ...on, https })
      const before = calls.length
      const r = render(<App />)
      await screen.findByTestId('summary-pub')
      fireEvent.click(row('在外面打开控制台'))
      await vi.waitFor(() => expect(calls.slice(before).some((c) => c.url === '/v1/public-console?check=https')).toBe(true))
      await screen.findByTestId('pub-url')
      await new Promise((res) => setTimeout(res, 20))
      expect(screen.queryByTestId('pub-cert')).toBeNull()
      r.unmount()
    }
  })

  it('在外面打开控制台·已开启：地址、复制、关闭；关闭先确认，要密码时带上密码调 DELETE', async () => {
    let enabled = true
    routes['GET /v1/public-console'] = () => (enabled
      ? { enabled: true, domain: 'console.apps.example.com', url: 'https://console.apps.example.com', enabled_at: '2026-11-16T01:00:00Z', can_toggle: true, needs_password: true }
      : { enabled: false, can_toggle: true, needs_password: true, suggested_domain: 'console.apps.example.com' })
    routes['DELETE /v1/public-console'] = () => { enabled = false; return { disabled: true, was_enabled: true } }
    location.hash = '#prefs/access'
    render(<App />)
    expect((await screen.findByTestId('summary-pub')).textContent).toBe('已开启 · console.apps.example.com · 要密码登录')
    expect(screen.queryByRole('button', { name: '开启' })).toBeNull()
    fireEvent.click(row('在外面打开控制台'))
    expect(screen.getByTestId('pub-url').textContent).toBe('https://console.apps.example.com')
    expect(screen.getByRole('button', { name: '复制地址' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '关闭公网控制台' }))
    const dialog = await screen.findByRole('dialog', { name: '关闭公网控制台？' })
    const off = within(dialog).getByRole('button', { name: '关闭公网控制台' }) as HTMLButtonElement
    expect(off.disabled).toBe(true)
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
    fireEvent.change(within(dialog).getByLabelText('再输一次现在的控制台密码'), { target: { value: 'pw-now' } })
    fireEvent.click(off)
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'DELETE')).toMatchObject({ url: '/v1/public-console', body: { password: 'pw-now' } }))
    await vi.waitFor(() => expect(screen.getByTestId('summary-pub').textContent).toBe('没开启。开启后人在外面也能用浏览器打开'))
  })

  it('在外面打开控制台·已开启、不用再输密码：DELETE 不带请求体', async () => {
    routes['GET /v1/public-console'] = () => ({ enabled: true, domain: 'c.mine.com', can_toggle: true, needs_password: false })
    routes['DELETE /v1/public-console'] = () => ({ disabled: true })
    location.hash = '#prefs/access'
    render(<App />)
    await screen.findByTestId('summary-pub')
    fireEvent.click(row('在外面打开控制台'))
    expect(screen.getByTestId('pub-url').textContent).toBe('https://c.mine.com')
    fireEvent.click(screen.getByRole('button', { name: '关闭公网控制台' }))
    const dialog = await screen.findByRole('dialog', { name: '关闭公网控制台？' })
    expect(within(dialog).queryByLabelText('再输一次现在的控制台密码')).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: '关闭公网控制台' }))
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/v1/public-console')).toBe(true))
    expect(calls.find((c) => c.method === 'DELETE')?.body).toBeUndefined()
  })

  it('在外面打开控制台·纯局域网机器或这次登录不能开关：不给按钮，写原因', async () => {
    routes['GET /v1/public-console'] = () => ({ enabled: false, can_toggle: false, unsupported: { code: 'PDOMAIN-02', text: '这台机器在局域网里，开不了公网控制台' } })
    location.hash = '#prefs/access'
    const r = render(<App />)
    expect((await screen.findByTestId('summary-pub')).textContent).toBe('这台机器在局域网里，开不了公网控制台')
    expect(screen.queryByRole('button', { name: '开启' })).toBeNull()
    fireEvent.click(row('在外面打开控制台'))
    expect(screen.getByText('PDOMAIN-02')).toBeTruthy()
    expect(screen.getByText('acornfox open')).toBeTruthy()
    expect(screen.queryByRole('button', { name: '开启公网控制台' })).toBeNull()
    r.unmount()

    routes['GET /v1/public-console'] = () => ({ enabled: false, can_toggle: false, needs_password: false })
    render(<App />)
    await screen.findByTestId('summary-pub')
    expect(screen.queryByRole('button', { name: '开启' })).toBeNull()
    fireEvent.click(row('在外面打开控制台'))
    expect(screen.getByText(/这次登录不能在页面上开关公网控制台/)).toBeTruthy()
    expect(screen.getByText('acornfox public-console enable 你的域名')).toBeTruthy()
  })

  it('没有后端支撑的分区（提醒、账号）不显示；升级给当前版本和命令', async () => {
    location.hash = '#prefs/update'
    render(<App />)
    await screen.findByText('0.5.0-dev.2')
    const nav = screen.getByRole('navigation', { name: '设置分区' })
    expect(within(nav).queryByText('提醒')).toBeNull()
    expect(within(nav).queryByText('账号')).toBeNull()
    expect(screen.getAllByText(/get\.sh \| sudo bash -s -- upgrade/, { selector: 'code' }).length).toBe(2)
  })

  it('下载源：换源顺序、目录来源（含已断开的）；这一版不显示“连接私有目录”', async () => {
    routes['GET /v1/catalog'] = () => withPrivate
    location.hash = '#prefs/mirror'
    render(<App />)
    const order = await screen.findByTestId('mirror-order')
    expect(within(order).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['1docker.m.daocloud.io', '2docker.1panel.live', '3Docker Hub 官方'])
    const cats = await screen.findByTestId('catalog-sources')
    expect(within(cats).getByText('演示客户专用')).toBeTruthy()
    expect(within(cats).getByText('已断开')).toBeTruthy()
    expect(screen.queryByRole('button', { name: /连接私有目录/ })).toBeNull()
  })

  it('通用：没有“切回旧版”', async () => {
    location.hash = '#prefs/general'
    render(<App />)
    await screen.findByTestId('prefs-general')
    expect(screen.queryByText(/旧版/)).toBeNull()
  })
})

// 连接私有目录的弹窗：页面上的入口这一版藏起（PRIVATE_CATALOG_UI），弹窗本身照常测。
describe('连接私有目录', () => {
  async function openDialog(code = 'AFXC-ABCD-EFGH-IJKL-MNOP') {
    const onClose = vi.fn()
    render(
      <ToastProvider>
        <ConsoleProvider session={{ csrf: 'tok', expires_at: '', server: 'home-server' }} install={() => {}}>
          <ConnectCatalogDialog open onClose={onClose} />
        </ConsoleProvider>
      </ToastProvider>,
    )
    const dialog = await screen.findByRole('dialog')
    fireEvent.change(within(dialog).getByLabelText('连接码'), { target: { value: code } })
    fireEvent.click(within(dialog).getByRole('button', { name: /连接/ }))
    return { dialog, onClose }
  }

  it('格式不对的不发给服务端', async () => {
    const { dialog } = await openDialog('AFXC-1234')
    await within(dialog).findByText(/CAT-CONN-01/)
    expect(calls.some((c) => c.method === 'POST')).toBe(false)
  })

  it('服务端拒绝时弹窗里显示编号、原因和提示，弹窗不关', async () => {
    routes['POST /v1/catalog/connect'] = () => json(409, { error: { code: 'CAT-CONN-03', message: '这个连接码已经用过了', hint: '找给你连接码的人要一个新的' } })
    const { dialog, onClose } = await openDialog()
    const alert = await within(dialog).findByRole('alert')
    expect(alert.textContent).toContain('CAT-CONN-03')
    expect(alert.textContent).toContain('这个连接码已经用过了')
    expect(alert.textContent).toContain('找给你连接码的人要一个新的')
    expect(onClose).not.toHaveBeenCalled()
  })

  it('连不上后台服务时说中文，不显示浏览器的英文原因', async () => {
    routes['POST /v1/catalog/connect'] = () => { throw new TypeError('Failed to fetch') }
    const { dialog } = await openDialog()
    const alert = await within(dialog).findByRole('alert')
    expect(alert.textContent).toContain('连不上后台服务')
    expect(alert.textContent).not.toContain('Failed to fetch')
  })

  it('成功后重新读目录、关掉弹窗', async () => {
    routes['POST /v1/catalog/connect'] = () => ({ catalog: priv, again: false, sync: { ok: true } })
    const { onClose } = await openDialog()
    await vi.waitFor(() => expect(onClose).toHaveBeenCalled())
    const post = calls.findIndex((c) => c.method === 'POST' && c.url === '/v1/catalog/connect')
    expect(post).toBeGreaterThanOrEqual(0)
    expect(calls[post].body).toEqual({ code: 'AFXC-ABCD-EFGH-IJKL-MNOP' })
    expect(calls.slice(post + 1).some((c) => c.method === 'GET' && c.url === '/v1/catalog')).toBe(true)
  })
})

describe('应用中心的私有目录', () => {
  it('公共版在装时，私有目录里同 id 的卡片不显示“安装中”', async () => {
    routes['GET /v1/catalog'] = () => withPrivate
    routes['GET /v1/catalog/install'] = () => ({ tasks: [task({ target: 'new-api', catalog: 'public' })] })
    location.hash = '#store'
    render(<App />)
    const cards = await screen.findAllByTestId('catalog-app')
    const privCard = cards.find((c) => c.textContent?.includes('New API 定制版'))!
    const pubCard = cards.find((c) => c.textContent?.includes('New API') && !c.textContent?.includes('定制版'))!
    await within(pubCard).findByText('安装中')
    expect(within(privCard).queryByText('安装中')).toBeNull()
    expect(within(privCard).getByRole('button', { name: '安装' })).toBeTruthy()
  })

  it('私有应用标来源；断开的目录有提示；详情和安装带目录编号；这一版不显示来源标签和“连接私有目录”', async () => {
    routes['GET /v1/catalog'] = () => withPrivate
    routes['GET /v1/catalog/apps/new-api'] = (url) => (url.includes('catalog=cat-demo') ? { ...newApiDetail, name: 'New API 定制版', source: priv } : newApiDetail)
    location.hash = '#store'
    render(<App />)
    await screen.findByRole('heading', { name: '应用中心' })
    expect((await screen.findAllByTestId('catalog-app')).length).toBe(3)
    expect(screen.getByText('来自：演示客户专用')).toBeTruthy()
    expect(screen.getByText(/私有目录“旧客户”已断开/)).toBeTruthy()
    expect(screen.queryByRole('tablist', { name: '来源' })).toBeNull()
    expect(screen.queryByRole('button', { name: /连接私有目录/ })).toBeNull()
    const card = screen.getAllByTestId('catalog-app').find((c) => c.textContent?.includes('定制版'))!
    fireEvent.click(within(card).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByText('acornfox install new-api --catalog cat-demo')
    expect(calls.some((c) => c.url === '/v1/catalog/apps/new-api?catalog=cat-demo')).toBe(true)
  })

  it('列表里有图标就显示真图标', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalog, apps: catalog.apps.map((a) => ({ ...a, icon: '<svg xmlns="http://www.w3.org/2000/svg"/>', icon_type: 'svg' })) })
    location.hash = '#store'
    const { container } = render(<App />)
    await screen.findAllByTestId('catalog-app')
    expect(container.querySelectorAll('[data-testid="catalog-app"] img').length).toBe(2)
  })
})

describe('应用概览不再放内存、CPU 的曲线图', () => {
  it('只有四张图表卡片，不读历史接口', async () => {
    routes['GET /v1/apps/new-api'] = () => app({ name: 'new-api', catalog_id: 'new-api' })
    routes['GET /v1/metrics/apps/new-api/history'] = () => ({ app: 'new-api', range: '1h', step_seconds: 30, ranges: ['1h', '24h'], points: [] })
    location.hash = '#app-new-api'
    render(<App />)
    await screen.findByTestId('ov-run')
    await screen.findByTestId('ov-res')
    await screen.findByTestId('ov-ver')
    expect(screen.queryByTestId('app-history')).toBeNull()
    expect(calls.some((c) => c.url.startsWith('/v1/metrics/apps/new-api/history'))).toBe(false)
  })
})
