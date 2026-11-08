// 页面级测试（第二轮）：首页看板、运行环境、设置、应用中心的私有目录。用假的接口（替换 fetch）渲染整个控制台。
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { ConsoleProvider } from './state'
import { ToastProvider } from './components/ui/toast'
import { ConnectCatalogDialog } from './pages/store/connect-dialog'
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
  it('访问：只显示局域网、公网控制台的状态和命令，开关不能在页面上点', async () => {
    location.hash = '#prefs/access'
    render(<App />)
    await screen.findByText('局域网控制台')
    const sw = screen.getAllByRole('switch')
    expect(sw.length).toBe(2)
    expect(sw.every((s) => (s as HTMLButtonElement).disabled)).toBe(true)
    expect(sw[0].getAttribute('aria-checked')).toBe('true')
    expect(screen.getByText('acornfox lan-console disable')).toBeTruthy()
    expect(screen.getByText('acornfox public-console enable 你的域名')).toBeTruthy()
    expect(calls.some((c) => c.method !== 'GET')).toBe(false)
  })

  it('没有后端支撑的分区（提醒、账号）不显示；升级给当前版本和命令', async () => {
    location.hash = '#prefs/update'
    render(<App />)
    await screen.findByText('0.5.0-dev.2')
    const nav = screen.getByRole('navigation', { name: '设置分区' })
    expect(within(nav).queryByText('提醒')).toBeNull()
    expect(within(nav).queryByText('账号')).toBeNull()
    expect(screen.getAllByText(/upgrade.sh \| sudo bash/, { selector: 'code' }).length).toBe(2)
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
