// 页面级测试：用假的接口（替换 fetch）把控制台渲染出来，走应用中心的主要路径。
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { Tile } from './components/bits'
import { OpenHintBlock } from './pages/apps/open-hint'
import { ConsoleProvider } from './state'
import { ToastProvider } from './components/ui/toast'
import { FORMER_NOTE, NEEDS, app, catalog, catalogFormer, catalogNeedsUpgrade, hermesDetail, hermesWithModel, newApiDetail, resources, task } from './test/fixtures'
import type { CatalogView, TaskView } from './lib/types'

type Handler = (url: string, init?: RequestInit) => unknown
let routes: Record<string, Handler>
let calls: { method: string; url: string; body?: unknown; csrf?: string }[]

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  calls = []
  routes = {
    'GET /v1/console/session': () => ({ csrf: 'tok', expires_at: '', server: 'home-server' }),
    'GET /v1/apps': () => ({ apps: [] }),
    'GET /v1/catalog/install': () => ({ tasks: [] }),
    'GET /v1/catalog': () => catalog,
    'GET /v1/resources': () => resources,
    'GET /v1/status': () => ({ runner: 'ok', docker_version: '27.3' }),
    'GET /v1/catalog/apps/new-api': () => newApiDetail,
    'GET /v1/catalog/apps/hermes-agent': () => hermesDetail,
  }
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    const method = init?.method || 'GET'
    const url = String(input)
    const headers = (init?.headers || {}) as Record<string, string>
    calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined, csrf: headers['X-AcornFox-CSRF'] })
    const h = routes[method + ' ' + url] || routes[method + ' ' + url.split('?')[0]]
    if (!h) return json(404, { error: { code: 'not_found', message: '对象不存在' } })
    const v = h(url, init)
    return v instanceof Response ? v : json(200, v)
  }))
  location.hash = '#store'
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
})

// stubClipboard：把“复制”接到一个数组上，看复制出去的文字里有什么（故障编号、任务号只放在复制给 AI 工作台的文字里）。
function stubClipboard(): string[] {
  const copied: string[] = []
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (t: string) => { copied.push(t) } } })
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  return copied
}

async function openStore() {
  render(<App />)
  await screen.findByRole('heading', { name: '应用中心' })
  await screen.findAllByTestId('catalog-app')
  // 占位形状换成真内容时页面会重画一次；等占位都没了、卡片稳定在页面上再往下查（开发机忙时偶发查到半截）
  await waitFor(() => {
    expect(screen.queryAllByTestId('skeleton')).toHaveLength(0)
    expect(screen.getAllByTestId('catalog-app').length).toBeGreaterThan(0)
  })
}

describe('应用中心', () => {
  it('应用和配方都来自目录接口', async () => {
    await openStore()
    const cards = screen.getAllByTestId('catalog-app')
    expect(cards.length).toBe(2)
    expect(within(cards[0]).getByText('Hermes 智能体')).toBeTruthy()
    expect(within(cards[1]).getByText('New API')).toBeTruthy()
    // 解决方案在上：只有几个应用串起来的配方算（只装一个应用的配方和直接装那个应用是一回事，不摆）
    const sol = screen.getAllByTestId('catalog-recipe')
    expect(sol.length).toBe(1)
    expect(within(sol[0]).getByText('Hermes 智能体 + New API')).toBeTruthy()
    expect(screen.queryByText('New API 模型网关')).toBeNull()
    // 目录没有配“找专人部署”，页面上就没有，也没有价格
    expect(screen.queryByText(/找专人/)).toBeNull()
    expect(screen.queryByText(/¥/)).toBeNull()
  })

  it('目录里只有一个分类时也显示货架标签（全部 + AI）', async () => {
    await openStore()
    const tabs = screen.getByRole('tablist', { name: '货架' })
    // 标签后面带这一格里有几个应用
    expect(within(tabs).getByRole('tab', { name: /^全部/ })).toBeTruthy()
    expect(within(tabs).getByRole('tab', { name: /^AI/ })).toBeTruthy()
  })

  it('详情页不把 javascript: 之类的主页、视频地址做成链接', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, homepage: 'javascript:alert(1)', delivery: { ...newApiDetail.delivery, video: 'javascript:alert(2)' } })
    location.hash = '#item-new-api'
    render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    expect(document.querySelector('a[href^="javascript"]')).toBeNull()
    expect(screen.queryByText('看视频（新窗口打开）')).toBeNull()
    expect(screen.queryByText('主页')).toBeNull()
  })

  it('详情页：它需要的服务、它能提供的服务都用接口给的中文名，可选的写没有时会怎样；不提示先装哪个应用', async () => {
    routes['GET /v1/catalog/apps/hermes-agent'] = () => ({
      ...hermesDetail,
      uses: [
        ...(hermesDetail.uses || []),
        { name: 'mail', kind: 'smtp', optional: true, without: '能用，但发不了通知邮件', use: 'mail', kind_label: '邮箱（发信）', required: false },
      ],
    })
    location.hash = '#item-hermes-agent'
    render(<App />)
    await screen.findByRole('heading', { name: 'Hermes 智能体' })
    const uses = screen.getByTestId('item-uses')
    expect(within(uses).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['大模型接口必须有', '邮箱（发信）可选没有时：能用，但发不了通知邮件'])
    expect(screen.queryByTestId('item-provides')).toBeNull()
    expect(screen.getByTestId('item-services').textContent).not.toMatch(/llm_gateway|smtp|先装|New API/)
  })

  it('详情页：能提供服务的应用列出它能提供的服务；两样都没有时不显示这一块', async () => {
    location.hash = '#item-new-api'
    const { unmount } = render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    expect(within(screen.getByTestId('item-provides')).getByText('大模型接口')).toBeTruthy()
    expect(screen.queryByTestId('item-uses')).toBeNull()
    unmount()
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, provides: [], provides_detail: [] })
    render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    expect(screen.queryByTestId('item-services')).toBeNull()
  })

  it('PNG 图标按 base64 显示', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, icon: 'iVBORw0KGgo=', icon_type: 'png' })
    location.hash = '#item-new-api'
    const { container } = render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    expect(container.querySelector('img')!.getAttribute('src')).toBe('data:image/png;base64,iVBORw0KGgo=')
  })

  it('已装好的应用卡片标“已装好”，按钮是“去管理”，点了进我的应用', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalog, apps: catalog.apps.map((a) => (a.id === 'new-api' ? { ...a, installed: ['new-api'] } : a)) })
    routes['GET /v1/apps/new-api'] = () => app({ name: 'new-api' })
    await openStore()
    const card = screen.getAllByTestId('catalog-app')[1]
    expect(within(card).getByText('已装好')).toBeTruthy()
    // 这个应用在“我的应用”里还没读到地址时，主按钮就是“去管理”；没有“安装”按钮
    expect(within(card).getByRole('link', { name: '去管理' }).getAttribute('href')).toBe('#app-new-api')
    expect(within(card).queryByRole('button', { name: '安装' })).toBeNull()
    fireEvent.click(card)
    expect(location.hash).toBe('#app-new-api')
  })

  it('搜不到时提示', async () => {
    await openStore()
    fireEvent.change(screen.getByLabelText('找应用'), { target: { value: '不存在的东西' } })
    expect(screen.getByText(/没找到/)).toBeTruthy()
  })

  it('装 New API：弹窗给命令行写法，提交后按任务步骤显示进度，装好显示首次登录（密码遮住，点了才显示）', async () => {
    let poll = 0
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = (): TaskView => {
      poll++
      return poll < 2 ? task() : task({
        status: 'succeeded',
        steps: task().steps.map((s) => ({ ...s, status: 'done' })),
        apps: [{ app: 'new-api', catalog_id: 'new-api', version: 'v1.0.0-rc.48', reused: false, created: true, url: 'http://127.0.0.1:18810' }],
        first_login: [{ app: 'new-api', url: 'http://127.0.0.1:18810', fields: [{ label: '用户名', value: 'admin' }, { label: '密码', value: '******', secret: true }] }],
        user_steps: ['登录后新建一个渠道'],
      })
    }
    routes['GET /v1/apps/new-api/first-login'] = () => ({ app: 'new-api', url: 'http://127.0.0.1:18810', fields: [{ label: '用户名', value: 'admin' }, { label: '密码', value: 'S3cret-Generated', secret: true }] })
    await openStore()
    const card = screen.getAllByTestId('catalog-app')[1]
    fireEvent.click(within(card).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByTestId('mem-check')
    // 弹窗里不放命令行（命令在应用页的“技术信息”里）；够不够内存只说一句，具体数字在悬停提示里
    expect(within(dialog).queryByText(/acornfox install/)).toBeNull()
    expect(within(dialog).getByTestId('mem-check').textContent).toContain('这台机器装得下')
    expect(within(dialog).getByTitle('要 512 MB · 剩 5 GB')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    await within(dialog).findByText('下载应用')
    const post = calls.find((c) => c.method === 'POST' && c.url === '/v1/catalog/install')!
    expect(post.body).toEqual({ app: 'new-api' })
    expect(post.csrf).toBe('tok')
    await within(dialog).findByText('New API 已装好', undefined, { timeout: 3000 })
    expect(within(dialog).getByText('******')).toBeTruthy()
    expect(within(dialog).queryByText('S3cret-Generated')).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: /显示密码/ }))
    await within(dialog).findByText('S3cret-Generated')
    expect(calls.some((c) => c.url === '/v1/apps/new-api/first-login?reveal=true')).toBe(true)
    expect(within(dialog).getByText('登录后新建一个渠道')).toBeTruthy()
    // 只本机（127.0.0.1）：不给打不开的链接，给在自己电脑上运行的 acornfox open
    expect(within(dialog).queryByRole('link', { name: /打开/ })).toBeNull()
    expect(within(dialog).getByTestId('open-hint').textContent).toContain('acornfox open new-api')
    expect(within(dialog).getByText(/127.0.0.1:18810（只这台机器）/)).toBeTruthy()
  })

  it('装好后只本机：服务器在局域网时确认后一键改成局域网，给局域网地址', async () => {
    const hint = { lan_address: '192.168.1.20', lan_url_after_expose: 'http://192.168.1.20:18811', has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open hermes-agent',
      tunnel_command: 'ssh -N -L 18811:127.0.0.1:18811 用户@服务器地址', tunnel_url: 'http://127.0.0.1:18811' }
    routes['GET /v1/apps'] = () => ({ apps: [app({ name: 'hermes-agent', catalog_id: 'hermes-agent', url: 'http://127.0.0.1:18811', local_only: true, open_hint: hint })] })
    routes['PATCH /v1/apps/hermes-agent'] = () => app({ name: 'hermes-agent', exposure: 'lan' })
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    routes['GET /v1/catalog/install/t123'] = (): TaskView => task({
      target: 'hermes-agent', status: 'succeeded',
      steps: task().steps.map((s) => ({ ...s, app: 'hermes-agent', status: 'done' })),
      apps: [{ app: 'hermes-agent', catalog_id: 'hermes-agent', version: 'v0.23.0', reused: false, created: true, url: 'http://127.0.0.1:18811' }],
    })
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[0]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('button', { name: '安装' }))
    const box = await within(dialog).findByTestId('open-hint', undefined, { timeout: 3000 })
    expect(within(dialog).queryByRole('link', { name: /打开/ })).toBeNull()
    // 改之前局域网地址打不开，不当成地址给出来；没有命令行时给 SSH 隧道那一行
    expect(box.textContent).not.toContain('http://192.168.1.20:18811')
    expect(within(box).getByTestId('open-ssh').textContent).toContain('ssh -N -L 18811:127.0.0.1:18811 用户@服务器地址')
    expect(within(box).getByTestId('open-ssh').textContent).toContain('http://127.0.0.1:18811')
    fireEvent.click(within(box).getByRole('button', { name: '让家里的电脑、手机也能打开' }))
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false) // 先确认
    expect(within(box).getByText(/要用它自己的密码登录/)).toBeTruthy()
    fireEvent.click(within(box).getByRole('button', { name: '好，改' }))
    const done = await within(box).findByTestId('open-lan-done')
    expect(within(done).getByRole('link').getAttribute('href')).toBe('http://192.168.1.20:18811')
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ exposure: 'lan' })
  })

  it('装好后改成局域网、应用列表刷新以后：说明块原位显示完成态和怎么改回，首次登录地址和“打开”都用应用列表里的新地址', async () => {
    const hint = { lan_address: '192.168.1.20', lan_url_after_expose: 'http://192.168.1.20:18810', has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open new-api' }
    let exposure = 'local'
    routes['GET /v1/apps'] = () => ({
      apps: [exposure === 'local'
        ? app({ name: 'new-api', catalog_id: 'new-api', url: 'http://127.0.0.1:18810', local_only: true, open_hint: hint })
        : app({ name: 'new-api', catalog_id: 'new-api', url: 'http://192.168.1.20:18810', exposure: 'lan' })],
    })
    routes['PATCH /v1/apps/new-api'] = () => { exposure = 'lan'; return app({ name: 'new-api', exposure: 'lan' }) }
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = (): TaskView => task({
      status: 'succeeded',
      steps: task().steps.map((s) => ({ ...s, status: 'done' })),
      apps: [{ app: 'new-api', catalog_id: 'new-api', version: 'v1.0.0-rc.48', reused: false, created: true, url: 'http://127.0.0.1:18810' }],
      first_login: [{ app: 'new-api', url: 'http://127.0.0.1:18810', fields: [{ label: '用户名', value: 'admin' }] }],
    })
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('button', { name: '安装' }))
    const box = await within(dialog).findByTestId('open-hint', undefined, { timeout: 3000 })
    expect(within(dialog).getByTestId('first-login-url').textContent).toContain('http://127.0.0.1:18810（只这台机器）')
    fireEvent.click(await within(box).findByRole('button', { name: '让家里的电脑、手机也能打开' }))
    expect(within(box).getByText(/要用它自己的密码登录/)).toBeTruthy()
    fireEvent.click(within(box).getByRole('button', { name: '好，改' }))
    const open = await within(dialog).findByRole('link', { name: /^打开/ }, { timeout: 3000 })
    expect(open.getAttribute('href')).toBe('http://192.168.1.20:18810')
    expect(within(dialog).queryByText(/只在服务器本机能打开/)).toBeNull()
    // 接口不再给 open_hint，说明块不消失，原位显示完成态
    const done = within(dialog).getByTestId('open-lan-done')
    expect(done.textContent).toContain('已改好，家里的电脑、手机用这个地址打开')
    expect(within(done).getByRole('link').getAttribute('href')).toBe('http://192.168.1.20:18810')
    // 完成态里不再写命令，只留新地址
    expect(within(dialog).getByTestId('open-hint').textContent).not.toContain('acornfox')
    expect(within(dialog).queryByRole('button', { name: '让家里的电脑、手机也能打开' })).toBeNull()
    // 首次登录的地址跟着变，不再写只本机
    const login = within(dialog).getByTestId('first-login-url')
    expect(login.textContent).toContain('http://192.168.1.20:18810')
    expect(login.textContent).not.toContain('只本机')
    expect(within(login).getByRole('link').getAttribute('href')).toBe('http://192.168.1.20:18810')
    // 再过几轮刷新也不消失
    await act(async () => { await new Promise((r) => setTimeout(r, 1200)) })
    expect(within(dialog).getByTestId('open-lan-done')).toBeTruthy()
  })

  it('平台自动填的：默认收起，只把改过的时区发给服务端，命令里也带上', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({
      ...newApiDetail,
      platform: [
        { name: 'tz', label: '时区', standalone: '本机时区', cloud: '账号时区', user_can_set: true, value: 'Asia/Shanghai', available: true },
        { name: 'region', label: '网络区域（cn 或 global）', standalone: '安装时的选择', cloud: '按集群', user_can_set: false, value: 'cn', available: true },
      ],
    })
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = () => task()
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    // 平台自动填的收在默认收起的“技术设置”里，弹窗里不放命令行
    const tech = await within(dialog).findByTestId('install-tech')
    expect(tech.hasAttribute('open')).toBe(false)
    const box = within(dialog).getByTestId('platform-vars')
    expect(within(box).getByText('平台自动填的')).toBeTruthy()
    expect(within(box).getByText('cn')).toBeTruthy() // 不能改的只显示
    fireEvent.change(within(box).getByLabelText('时区'), { target: { value: 'Asia/Tokyo' } })
    expect(within(box).getByText('平台填的是 Asia/Shanghai')).toBeTruthy()
    expect(within(dialog).queryByText(/acornfox install/)).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    await within(dialog).findByText('下载应用')
    const post = calls.find((c) => c.method === 'POST' && c.url === '/v1/catalog/install')!
    expect(post.body).toEqual({ app: 'new-api', platform: { tz: 'Asia/Tokyo' } })
  })

  it('下载镜像：经网关下载时显示进度条和已下多少，下完显示正在导入', async () => {
    let poll = 0
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = (): TaskView => {
      poll++
      const steps = task().steps.map((s) => (s.name === 'image'
        ? (poll < 2
          ? { ...s, status: 'running', message: '从下载网关下载 312 MB / 1.0 GB（29%，25 MB/s，还要约 30 秒）', progress: { phase: 'download', done_bytes: 312 << 20, total_bytes: 1120227840, bytes_per_sec: 25 << 20, eta_seconds: 30 } }
          : { ...s, status: 'running', message: '已从下载网关下载 1.0 GB，正在导入镜像（大镜像要几十秒）', progress: { phase: 'load', done_bytes: 1120227840, total_bytes: 1120227840 } })
        : s))
      return task({ steps })
    }
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('button', { name: '安装' }))
    await within(dialog).findByText(/从下载网关下载 312 MB/)
    const bar = within(dialog).getByTestId('step-progress')
    expect(bar.getAttribute('aria-valuenow')).toBe('29')
    await within(dialog).findByText(/正在导入应用程序/, undefined, { timeout: 3000 })
    expect(within(dialog).getByTestId('step-progress').getAttribute('aria-valuenow')).toBeNull()
  })

  it('以前同名应用的数据还在（CAT-INST-24）：点“接上以前留下的数据重装”并确认后才带 reuse_data 重新提交', async () => {
    let posts = 0
    routes['POST /v1/catalog/install'] = () => {
      posts++
      return posts === 1
        ? json(409, { error: { code: 'CAT-INST-24', message: '这台机器上还留着以前叫 new-api 的应用的数据卷（af-new-api-1）', hint: '要接着用就加 --reuse-data 重装' } })
        : json(202, task())
    }
    routes['GET /v1/catalog/install/t123'] = () => task()
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('button', { name: '安装' }))
    await within(dialog).findByText(/还留着以前叫 new-api 的应用的数据卷/)
    // 故障编号不写在页面上
    expect(within(dialog).queryByText(/CAT-INST-24/)).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: '接上以前留下的数据重装' }))
    expect(posts).toBe(1) // 只是展开确认
    fireEvent.click(within(dialog).getByRole('button', { name: '确认接上' }))
    await within(dialog).findByText('下载应用')
    const sent = calls.filter((c) => c.method === 'POST' && c.url === '/v1/catalog/install').map((c) => c.body)
    expect(sent).toEqual([{ app: 'new-api' }, { app: 'new-api', reuse_data: true }])
  })

  it('安装失败：页面上写原因和建议，故障编号和任务号只在“复制给 AI 工作台”复制出去的文字里', async () => {
    const copied = stubClipboard()
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = () => task({
      status: 'failed',
      steps: [{ app: 'new-api', name: 'image', label: '下载镜像', status: 'failed', message: '所有来源都失败' }],
      fault: { code: 'CAT-INST-12', message: '镜像从所有来源都没拿到', hint: '国内网络确认已配镜像加速；稍后重试' },
    })
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('button', { name: '安装' }))
    await within(dialog).findByText('镜像从所有来源都没拿到')
    expect(within(dialog).queryByText(/CAT-INST-12/)).toBeNull()
    expect(within(dialog).getByText(/建议：国内网络确认已配镜像加速/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: /复制给 AI 工作台/ }))
    await vi.waitFor(() => expect(copied.length).toBe(1))
    expect(copied[0]).toContain('CAT-INST-12')
    expect(copied[0]).toContain('t123')
  })

  it('需要大模型接口的应用：弹窗里没有大模型来源的选择，也不提示先装别的应用；请求和命令都只有应用名', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[0]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByTestId('mem-check')
    expect(within(dialog).queryByRole('tab')).toBeNull()
    expect(within(dialog).queryByText(/平台 Key|自己的 Key|先装|模型名|--llm/)).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    await vi.waitFor(() => expect(calls.filter((c) => c.method === 'POST' && c.url === '/v1/catalog/install').map((c) => c.body)).toEqual([{ app: 'hermes-agent' }]))
  })

  it('安装任务找不到（404）时停止轮询，提示去我的应用看', async () => {
    let polls = 0
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = () => { polls++; return json(404, { error: { code: 'CAT-INST-05', message: '没有这个安装任务' } }) }
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('button', { name: '安装' }))
    await within(dialog).findByTestId('task-lost')
    expect(within(dialog).getByText(/找不到这次安装的进度了/)).toBeTruthy()
    expect(within(dialog).getByRole('button', { name: '去我的应用' })).toBeTruthy()
    const n = polls
    await new Promise((r) => setTimeout(r, 2500))
    expect(polls).toBe(n)
  })

  it('已经在装同一个应用时接上那个任务的进度', async () => {
    routes['POST /v1/catalog/install'] = () => json(409, { error: { code: 'CAT-INST-04', message: '正在安装', task_id: 'tbusy' } })
    routes['GET /v1/catalog/install/tbusy'] = () => task({ id: 'tbusy' })
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('button', { name: '安装' }))
    // 接上那个任务的进度（不显示任务号）：按它的步骤显示
    await within(dialog).findByText('下载应用')
    expect(within(dialog).queryByText(/tbusy/)).toBeNull()
    expect(calls.some((c) => c.url === '/v1/catalog/install/tbusy')).toBe(true)
  })
})

// 经营系统签名发布的展示内容（show.json）并进 GET /v1/catalog：每个应用和配方多一个 show，顶层多 industries、home。
// 格式见 LW1 接口约定；没有展示内容时页面退回清单里的名字和简介（上面的用例用的 catalog 就没有 show）。
describe('应用中心的展示内容', () => {
  const pair = (id: string, over: Partial<CatalogView['recipes'][number]> = {}) => ({
    id, title: '解决方案 ' + id, summary: '清单里的简介 ' + id, apps: [{ app: 'new-api' }, { app: 'memos' }],
    delivery: { tier: 'auto', minutes: 2, memory_mb: 768 }, command: 'acornfox install --recipe ' + id, ...over,
  })
  const shown: CatalogView = {
    ...catalog,
    industries: [{ id: 'office', name: '通用办公' }, { id: 'retail', name: '电商零售' }],
    // 首页按这个顺序摆（取前三个）
    home: { solutions: ['pair-c', 'pair-a', 'pair-b'] },
    apps: catalog.apps.map((a) => (a.id === 'hermes-agent'
      ? { ...a, show: { name: '智能小助手', tagline: '管家式的 AI 助手', flag: 'hot', color: '#3b5bdb', keywords: ['小秘书'], order: 2 } }
      : { ...a, show: { order: 1 } })),
    recipes: [
      ...catalog.recipes,
      pair('pair-a', { show: { name: '开网店', tagline: '一句话讲清楚', industries: ['retail', 'office'] } }),
      pair('pair-b', { show: { industries: ['office'] } }),
      pair('pair-c', { show: { industries: ['ghost'] } }),
    ],
  }

  it('应用卡片：用展示内容里的名字、一句话、角标；同一格里按 order 排；搜索认展示内容里的词', async () => {
    routes['GET /v1/catalog'] = () => shown
    await openStore()
    const cards = screen.getAllByTestId('catalog-app')
    // New API 的 order 是 1，排在 order 为 2 的 Hermes 前面
    expect(within(cards[0]).getByText('New API')).toBeTruthy()
    expect(within(cards[1]).getByText('智能小助手')).toBeTruthy()
    expect(within(cards[1]).getByText('管家式的 AI 助手')).toBeTruthy()
    expect(within(cards[1]).getByText('热门')).toBeTruthy()
    expect(within(cards[1]).queryByText('Hermes 智能体')).toBeNull()
    fireEvent.change(screen.getByLabelText('找应用'), { target: { value: '小秘书' } })
    const found = screen.getAllByTestId('catalog-app')
    expect(found.length).toBe(1)
    expect(within(found[0]).getByText('智能小助手')).toBeTruthy()
  })

  it('解决方案在上：首页按 home.solutions 的顺序摆前三个，行业按 id 换成名字，认不得的行业不显示；其余进“全部解决方案”', async () => {
    routes['GET /v1/catalog'] = () => shown
    await openStore()
    const sol = screen.getByTestId('store-solutions')
    const cards = within(sol).getAllByTestId('catalog-recipe')
    expect(cards.length).toBe(3)
    expect(within(cards[0]).getByText('解决方案 pair-c')).toBeTruthy()
    expect(within(cards[1]).getByText('开网店')).toBeTruthy() // 展示内容里的名字
    expect(within(cards[1]).getByText('一句话讲清楚')).toBeTruthy()
    expect(within(cards[2]).getByText('解决方案 pair-b')).toBeTruthy()
    // 行业：id 换成名字，按目录里的顺序没有的（ghost）不写出来
    expect(within(cards[1]).getByTestId('solution-industries').textContent).toBe('电商零售通用办公')
    expect(within(cards[0]).queryByTestId('solution-industries')).toBeNull()
    expect(sol.textContent).not.toContain('ghost')
    // 第四个（Hermes + New API）不在首页，进“全部解决方案”
    expect(within(sol).queryByText('Hermes 智能体 + New API')).toBeNull()
    expect(within(sol).getByRole('link', { name: /全部解决方案 4 个/ }).getAttribute('href')).toBe('#solutions')
    // 搜索认行业名
    fireEvent.change(screen.getByLabelText('找应用'), { target: { value: '电商零售' } })
    expect(within(screen.getByTestId('store-solutions')).getAllByTestId('catalog-recipe').length).toBe(1)
  })

  it('全部解决方案：行业筛选读目录顶层 industries 的名字和顺序，只列有解决方案的行业', async () => {
    routes['GET /v1/catalog'] = () => shown
    location.hash = '#solutions'
    render(<App />)
    const tabs = await screen.findByTestId('solution-industry-tabs')
    expect(within(tabs).getAllByRole('tab').map((t) => t.textContent)).toEqual(['全部4', '通用办公2', '电商零售1'])
    expect(screen.getAllByTestId('catalog-recipe').length).toBe(4)
    fireEvent.click(within(tabs).getByRole('tab', { name: /^电商零售/ }))
    const list = screen.getAllByTestId('catalog-recipe')
    expect(list.length).toBe(1)
    expect(within(list[0]).getByText('开网店')).toBeTruthy()
  })

  it('目录没有行业这一项（没有展示内容）：不显示行业这一排，也不把行业 id 写出来', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalog, recipes: [...catalog.recipes, pair('pair-a', { show: { industries: ['office'] } })] })
    location.hash = '#solutions'
    render(<App />)
    await screen.findAllByTestId('catalog-recipe')
    expect(screen.queryByTestId('solution-industry-tabs')).toBeNull()
    expect(screen.queryByText('office')).toBeNull()
    // 名字和简介退回清单里的
    expect(screen.getByText('解决方案 pair-a')).toBeTruthy()
  })

  it('没有视频的介绍文档不画播放键；图的链接是空的不显示占位画面', async () => {
    const doc = '## 它能帮你做什么\n\n帮你收发消息。\n\n[演示](https://www.bilibili.com/video/)\n\n![截图]()\n'
    routes['GET /v1/catalog'] = () => ({ ...shown, apps: shown.apps.map((a) => (a.id === 'new-api' ? { ...a, show: { ...a.show, doc } } : a)) })
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, show: { doc } })
    location.hash = '#item-new-api'
    render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    const article = await screen.findByTestId('store-doc')
    expect(within(article).getByText('帮你收发消息。')).toBeTruthy()
    expect(within(article).queryByTestId('doc-video')).toBeNull()
    expect(article.querySelector('figure')).toBeNull()
  })
})

describe('要先升级 AcornFox 才能装的应用', () => {
  const SENTENCE = '要先把 AcornFox 升级到 0.6.0 才能装'
  const upgradeLines = (root: HTMLElement) => within(root).queryAllByText(/get\.sh \| sudo bash -s -- upgrade/, { selector: 'code' })

  it('应用卡片：说明要升到哪个版本，安装按钮不可点，“怎么升级”进设置的升级分区；别的应用不受影响', async () => {
    routes['GET /v1/catalog'] = () => catalogNeedsUpgrade
    await openStore()
    const [hermes, newApi] = screen.getAllByTestId('catalog-app')
    expect(within(hermes).getByText(SENTENCE)).toBeTruthy()
    expect((within(hermes).getByRole('button', { name: '安装' }) as HTMLButtonElement).disabled).toBe(true)
    // 不用升级的（空串）照常能装，没有提示
    expect(within(newApi).queryByTestId('needs-platform')).toBeNull()
    expect((within(newApi).getByRole('button', { name: '安装' }) as HTMLButtonElement).disabled).toBe(false)
    fireEvent.click(within(hermes).getByRole('button', { name: '怎么升级' }))
    expect(location.hash).toBe('#prefs/update')
  })

  it('解决方案卡片：自己或其中的应用要升级时“一起装好”不可点并说明；别的解决方案不受影响', async () => {
    // 再加一个不用 Hermes 的解决方案（两个应用串起来），看它不受影响
    routes['GET /v1/catalog'] = () => ({
      ...catalogNeedsUpgrade,
      recipes: [...catalogNeedsUpgrade.recipes, { id: 'pair', title: '两个一起用', summary: '', apps: [{ app: 'new-api' }, { app: 'memos' }], delivery: { tier: 'auto', minutes: 2, memory_mb: 768 }, command: 'acornfox install --recipe pair' }],
    })
    await openStore()
    const cards = screen.getAllByTestId('catalog-recipe')
    // 只装一个应用的配方不摆，所以只有两张
    expect(cards.length).toBe(2)
    const [needy, ok] = cards
    expect(within(needy).getByText('Hermes 智能体 + New API')).toBeTruthy()
    expect(within(needy).getByText(SENTENCE)).toBeTruthy()
    expect((within(needy).getByRole('button', { name: '一起装好' }) as HTMLButtonElement).disabled).toBe(true)
    expect(within(needy).getByRole('button', { name: '怎么升级' })).toBeTruthy()
    expect(within(ok).getByText('两个一起用')).toBeTruthy()
    expect(within(ok).queryByTestId('needs-platform')).toBeNull()
    expect((within(ok).getByRole('button', { name: '一起装好' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('旧服务端没有这个字段时，卡片和详情都没有提示，按钮能点', async () => {
    await openStore()
    expect(screen.queryByTestId('needs-platform')).toBeNull()
    for (const b of screen.getAllByRole('button', { name: /^(安装|一起装好)$/ })) expect((b as HTMLButtonElement).disabled).toBe(false)
  })

  it('详情页：安装按钮不可点，说明要升到哪个版本、现在是哪个版本，给国内和海外两条升级命令', async () => {
    routes['GET /v1/catalog'] = () => catalogNeedsUpgrade
    routes['GET /v1/status'] = () => ({ runner: 'ok', version: '0.5.1' })
    routes['GET /v1/catalog/apps/hermes-agent'] = () => ({ ...hermesDetail, needs_platform: NEEDS })
    location.hash = '#item-hermes-agent'
    render(<App />)
    await screen.findByRole('heading', { name: 'Hermes 智能体' })
    // 下面“再逛逛”的卡片上也有升级提示，这里只看右边安装栏那一张
    const buy = screen.getByTestId('item-buy')
    const note = within(buy).getByTestId('needs-platform')
    expect(within(note).getByText(SENTENCE)).toBeTruthy()
    await within(note).findByText('0.5.1')
    const lines = upgradeLines(note).map((x) => x.textContent || '')
    expect(lines.length).toBe(2)
    expect(lines[0]).toContain('gitee.com/VIP13390/AcornFox/raw/main/get.sh')
    expect(lines[1]).toContain('github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh')
    expect((within(buy).getByRole('button', { name: '安装' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('详情页：不用升级的应用没有提示，安装能点', async () => {
    routes['GET /v1/catalog'] = () => catalogNeedsUpgrade
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, needs_platform: '' })
    location.hash = '#item-new-api'
    render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    const buy = screen.getByTestId('item-buy')
    expect(within(buy).queryByTestId('needs-platform')).toBeNull()
    expect((within(buy).getByRole('button', { name: '安装' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('安装弹窗：列表是旧的、详情说要升级时，弹窗里说明并给升级命令，不能提交', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, needs_platform: NEEDS })
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText(SENTENCE)).toBeTruthy()
    expect(upgradeLines(dialog).length).toBe(2)
    const submit = within(dialog).getByRole('button', { name: '安装' }) as HTMLButtonElement
    expect(submit.disabled).toBe(true)
    fireEvent.click(submit)
    expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/catalog/install')).toBe(false)
  })

  it('解决方案的弹窗：列表是旧的、其中一个应用的详情说要升级时，弹窗里说明并给升级命令，不能提交', async () => {
    // 弹窗自己也要拦：列表上没标要升级，点开后读到的应用详情说要升级
    routes['GET /v1/catalog/apps/hermes-agent'] = () => ({ ...hermesDetail, needs_platform: NEEDS })
    await openStore()
    fireEvent.click(within(screen.getByTestId('catalog-recipe')).getByRole('button', { name: '一起装好' }))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText(SENTENCE)).toBeTruthy()
    expect(upgradeLines(dialog).length).toBe(2)
    const submit = within(dialog).getByRole('button', { name: '开始安装' }) as HTMLButtonElement
    expect(submit.disabled).toBe(true)
    fireEvent.click(submit)
    expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/catalog/install')).toBe(false)
  })

  // 下面几条的说明和建议照服务端的原文写。CAT-INST-29 的建议两种情况共用一句，里面有“升级”两个字，页面不能按字猜。
  const MSG08 = 'new-api 要求 AcornFox 0.6.0 以上，这台是 0.5.1，请先升级 AcornFox 再装'
  const HINT08 = '先升级 AcornFox 再装（错误里的 needs_platform 是要升到的版本）'
  const MSG29_UPGRADE = 'new-api v1.0.0-rc.48 的清单用了这个版本的 AcornFox 不认识的写法，它要求 AcornFox 0.6.0 以上，这台是 0.5.1，请先升级 AcornFox 再装（/ additional properties \'future_feature\' not allowed）'
  const MSG29_BROKEN = 'new-api v1.0.0-rc.48 的清单过不了检查，没有安装。这是这份目录有问题，不是你的操作不对：稍后再试，还不行请把这段话反馈给我们（/ additional properties \'future_feature\' not allowed）'
  const HINT29 = '错误里带 needs_platform 的：先升级 AcornFox 到那个版本再装。不带的：是这份目录有问题，不是操作不对，过一会儿 acornfox catalog sync 后再试，还不行就把 message 反馈给我们；不要想办法绕过'
  const FIRST = '要先升级到 0.6.0'

  async function submitNewApi() {
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('button', { name: '安装' }))
    return dialog
  }

  it('提交后被拒绝（CAT-INST-08，带 needs_platform）：服务端的说明原样显示，下面说要先升级到哪个版本并给升级命令', async () => {
    routes['POST /v1/catalog/install'] = () => json(409, { error: { code: 'CAT-INST-08', message: MSG08, hint: HINT08, needs_platform: NEEDS } })
    const dialog = await submitNewApi()
    await within(dialog).findByText(MSG08)
    expect(within(dialog).getByText(HINT08)).toBeTruthy()
    expect(within(within(dialog).getByTestId('upgrade-first')).getByText(FIRST)).toBeTruthy()
    expect(upgradeLines(dialog).length).toBe(2)
  })

  it('提交后被拒绝（CAT-INST-29，带 needs_platform）：说明原样显示，下面说要先升级并给升级命令', async () => {
    routes['POST /v1/catalog/install'] = () => json(409, { error: { code: 'CAT-INST-29', message: MSG29_UPGRADE, hint: HINT29, needs_platform: NEEDS } })
    const dialog = await submitNewApi()
    await within(dialog).findByText(MSG29_UPGRADE)
    expect(within(within(dialog).getByTestId('upgrade-first')).getByText(FIRST)).toBeTruthy()
    expect(upgradeLines(dialog).length).toBe(2)
  })

  it('提交后被拒绝（CAT-INST-29，不带 needs_platform）：是目录有问题，只原样显示说明，不提升级、不给升级命令', async () => {
    // 建议里有“升级”两个字（两种情况共用一句），不能因此给升级命令。
    routes['POST /v1/catalog/install'] = () => json(409, { error: { code: 'CAT-INST-29', message: MSG29_BROKEN, hint: HINT29 } })
    const dialog = await submitNewApi()
    await within(dialog).findByText(MSG29_BROKEN)
    expect(within(dialog).getByText(HINT29)).toBeTruthy()
    expect(within(dialog).queryByTestId('upgrade-first')).toBeNull()
    expect(upgradeLines(dialog).length).toBe(0)
  })

  it('没带 needs_platform 的 CAT-INST-08 也只原样显示说明（只认这个字段，不按编号猜）', async () => {
    routes['POST /v1/catalog/install'] = () => json(409, { error: { code: 'CAT-INST-08', message: MSG08, hint: HINT08 } })
    const dialog = await submitNewApi()
    await within(dialog).findByText(MSG08)
    expect(within(dialog).queryByTestId('upgrade-first')).toBeNull()
    expect(upgradeLines(dialog).length).toBe(0)
  })

  const fail29 = (message: string, needs?: string) => task({
    status: 'failed',
    steps: [{ app: 'new-api', name: 'check', label: '检查', status: 'failed', message }],
    fault: { code: 'CAT-INST-29', message, hint: HINT29, ...(needs ? { needs_platform: needs } : {}) },
  })

  it('任务失败，fault 带 needs_platform：说明原样显示，下面说要先升级并给升级命令', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = () => fail29(MSG29_UPGRADE, NEEDS)
    const dialog = await submitNewApi()
    expect((await within(dialog).findAllByText(MSG29_UPGRADE)).length).toBeGreaterThan(0) // 失败的那一步和下面的说明各写一遍
    expect(within(dialog).getByText('建议：' + HINT29)).toBeTruthy()
    expect(within(dialog).getByText(/已经清理掉，升级后再装。/)).toBeTruthy()
    expect(within(within(dialog).getByTestId('upgrade-first')).getByText(FIRST)).toBeTruthy()
    expect(upgradeLines(dialog).length).toBe(2)
  })

  it('任务失败，fault 不带 needs_platform：说明原样显示，不给升级命令', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = () => fail29(MSG29_BROKEN)
    const dialog = await submitNewApi()
    expect((await within(dialog).findAllByText(MSG29_BROKEN)).length).toBeGreaterThan(0)
    expect(within(dialog).getByText('建议：' + HINT29)).toBeTruthy()
    expect(within(dialog).getByText(/已经清理掉，可以直接重装。/)).toBeTruthy()
    expect(within(dialog).queryByTestId('upgrade-first')).toBeNull()
    expect(upgradeLines(dialog).length).toBe(0)
  })

  // 给更新版本的 AcornFox 做的应用包，这台读不了：详情接口返回 409，错误里带 needs_platform。
  const MSG_DETAIL = 'hermes-agent 要求 AcornFox 0.6.0 以上，这台是 0.5.1，请先升级 AcornFox 再装'
  const tooOld = () => json(409, { error: { code: 'CAT-INST-08', message: MSG_DETAIL, hint: HINT08, needs_platform: NEEDS } })

  it('详情页：应用包读不了（409，带 needs_platform）时用列表里的基本信息，说明要先升级并给升级命令，不是加载失败', async () => {
    routes['GET /v1/catalog'] = () => catalogNeedsUpgrade
    routes['GET /v1/status'] = () => ({ runner: 'ok', version: '0.5.1' })
    routes['GET /v1/catalog/apps/hermes-agent'] = tooOld
    location.hash = '#item-hermes-agent'
    render(<App />)
    await screen.findByRole('heading', { name: 'Hermes 智能体' })
    const page = screen.getByTestId('item-limited')
    // 列表里已有的：简介、版本、内存、下载大小、支持的 CPU、命令
    expect(within(page).getByText(catalog.apps[0].summary)).toBeTruthy()
    expect(within(page).getByText('v0.23.0')).toBeTruthy()
    expect(within(page).getByText('amd64、arm64')).toBeTruthy()
    expect(within(page).getByText('acornfox install hermes-agent')).toBeTruthy()
    // 说明：要升到哪个版本、为什么只有基本信息、服务端的原话
    expect(within(page).getByText('要先升级到 0.6.0，才能看这个应用的完整介绍')).toBeTruthy()
    expect(within(page).getByText(/读不了这个应用现在的安装包，所以这一页只有基本信息。升级后再打开，就能看到完整介绍并安装。/)).toBeTruthy()
    expect(within(page).getByTestId('item-limited-message').textContent).toBe(MSG_DETAIL)
    // 和能读详情时一样的位置：安装不可点，下面是那句话、现在的版本、两条升级命令
    const note = within(page).getByTestId('needs-platform')
    expect(within(note).getByText(SENTENCE)).toBeTruthy()
    await within(note).findByText('0.5.1')
    expect(upgradeLines(page).length).toBe(2)
    expect((within(page).getByRole('button', { name: '安装' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.queryByText(/回应用中心/)).toBeNull()
  })

  it('详情页：应用包读不了、但这个应用已经装过，照常能进管理，说明里给升级命令', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalogNeedsUpgrade, apps: catalogNeedsUpgrade.apps.map((a) => (a.id === 'hermes-agent' ? { ...a, installed: ['hermes-agent'] } : a)) })
    routes['GET /v1/catalog/apps/hermes-agent'] = tooOld
    location.hash = '#item-hermes-agent'
    render(<App />)
    await screen.findByRole('heading', { name: 'Hermes 智能体' })
    const page = screen.getByTestId('item-limited')
    // 已经装好的：主按钮是“去管理”，不是“安装”
    fireEvent.click(within(page).getByRole('button', { name: '去管理' }))
    expect(location.hash).toBe('#app-hermes-agent')
    expect(within(page).queryByRole('button', { name: '安装' })).toBeNull()
    expect(within(page).getByText(/已经装好的照常运行，不受影响。/)).toBeTruthy()
    expect(within(page).queryByTestId('needs-platform')).toBeNull()
    expect(upgradeLines(page).length).toBe(2)
  })

  it('详情页：列表里没有这个应用时，仍说明要先升级并给升级命令', async () => {
    routes['GET /v1/catalog/apps/ghost'] = () => json(409, { error: { code: 'CAT-INST-08', message: 'ghost 要求 AcornFox 0.6.0 以上，这台是 0.5.1，请先升级 AcornFox 再装', hint: HINT08, needs_platform: NEEDS } })
    location.hash = '#item-ghost'
    render(<App />)
    await screen.findByRole('heading', { name: 'ghost' })
    const page = screen.getByTestId('item-limited')
    expect(within(page).getByText(SENTENCE)).toBeTruthy()
    expect(upgradeLines(page).length).toBe(2)
  })

  it('详情页：别的错误（不带 needs_platform）照旧显示服务端的说明', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => json(500, { error: { code: 'CAT-INST-19', message: '应用包和目录对不上' } })
    location.hash = '#item-new-api'
    render(<App />)
    await screen.findByText(/应用包和目录对不上/)
    expect(screen.queryByTestId('item-limited')).toBeNull()
    expect(upgradeLines(document.body).length).toBe(0)
  })

  it('安装弹窗：表单要读的应用详情读不了（409，带 needs_platform）时，说明原样显示并给升级命令', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => json(409, { error: { code: 'CAT-INST-08', message: MSG08, hint: HINT08, needs_platform: NEEDS } })
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByText(MSG08)
    expect(within(within(dialog).getByTestId('upgrade-first')).getByText(FIRST)).toBeTruthy()
    expect(upgradeLines(dialog).length).toBe(2)
  })
})

describe('我的应用', () => {
  // 应用少于 8 个时只有卡片、没有筛选和视图切换；表格给应用多的人看
  const fillers = Array.from({ length: 6 }, (_, i) => app({ name: 'extra' + i, desired: 'stopped' }))

  it('卡片：应用中心的应用写“来自应用中心”，状态写“正常”“停了”；应用多时表格视图显示版本、内存和 CPU', async () => {
    try { localStorage.removeItem('afx-apps-view') } catch { /* 前一个测试切到了表格视图 */ }
    routes['GET /v1/apps'] = () => ({ apps: [app({ name: 'new-api', catalog_id: 'new-api', catalog_version: 'v1.0.0-rc.48' }), app({ name: 'web', desired: 'stopped' })] })
    location.hash = '#apps'
    const r = render(<App />)
    const cards = await screen.findAllByTestId('my-app')
    expect(within(cards[0]).getByText('来自应用中心')).toBeTruthy()
    expect(within(cards[0]).getByText('New API')).toBeTruthy() // 用应用中心里的名字
    expect(within(cards[0]).getByText('正常')).toBeTruthy()
    expect(within(cards[1]).getByText('自己部署的项目')).toBeTruthy()
    expect(within(cards[1]).getByText('停了')).toBeTruthy()
    // 少于 8 个：没有筛选和视图切换
    expect(screen.queryByRole('tab', { name: '表格' })).toBeNull()
    r.unmount()

    routes['GET /v1/apps'] = () => ({ apps: [app({ name: 'new-api', catalog_id: 'new-api', catalog_version: 'v1.0.0-rc.48' }), app({ name: 'web', desired: 'stopped' }), ...fillers] })
    routes['GET /v1/metrics/apps/new-api'] = () => ({ app: 'new-api', available: true, observed_state: 'running', cpu_percent: 3.2, memory_usage_mb: 120, memory_limit_mb: 512 })
    render(<App />)
    await screen.findAllByTestId('my-app')
    fireEvent.click(await screen.findByRole('tab', { name: '表格' }))
    await screen.findByText('120 MB / 512 MB')
    expect(screen.getByText('3.2%')).toBeTruthy()
    expect(screen.getByText('v1.0.0-rc.48')).toBeTruthy()
    try { localStorage.removeItem('afx-apps-view') } catch { /* 还原 */ }
  })

  it('只本机的应用给“怎么打开”；Linux 服务器上不给打不开的“打开”', async () => {
    routes['GET /v1/apps'] = () => ({ apps: [
      app({ name: 'new-api', url: 'http://127.0.0.1:18810', local_only: true, open_hint: { has_login: true, can_lan: false, recommend: 'tunnel', command: 'acornfox open new-api' } }),
      app({ name: 'blog', exposure: 'public', url: 'http://203.0.113.7:18811' }),
    ] })
    try { localStorage.removeItem('afx-apps-view') } catch { /* 前一个测试切到了表格视图 */ }
    location.hash = '#apps'
    render(<App />)
    const cards = await screen.findAllByTestId('my-app')
    const local = cards.find((c) => c.textContent?.includes('new-api'))!
    const pub = cards.find((c) => c.textContent?.includes('blog'))!
    // 卡片上不写“只本机”，只本机的给“怎么打开”（去详情看），不给打不开的“打开”
    expect(within(local).getByRole('link', { name: '怎么打开' }).getAttribute('href')).toBe('#app-new-api')
    expect(within(local).queryByRole('link', { name: /^打开/ })).toBeNull()
    expect(within(pub).queryByRole('link', { name: '怎么打开' })).toBeNull()
    expect(within(pub).getByRole('link', { name: /^打开/ }).getAttribute('href')).toBe('http://203.0.113.7:18811')
  })

  it('应用详情：只本机的给怎么打开（公网服务器只有 acornfox open）', async () => {
    routes['GET /v1/apps/hermes-agent'] = () => app({
      name: 'hermes-agent', catalog_id: 'hermes-agent', url: 'http://127.0.0.1:18811', local_only: true,
      open_hint: { has_login: true, can_lan: false, recommend: 'tunnel', command: 'acornfox open hermes-agent' },
    })
    location.hash = '#app-hermes-agent'
    render(<App />)
    const box = await screen.findByTestId('open-hint')
    expect(within(box).getByText('acornfox open hermes-agent')).toBeTruthy()
    expect(within(box).queryByRole('button', { name: '让家里的电脑、手机也能打开' })).toBeNull()
    expect(screen.queryByRole('link', { name: /打开/ })).toBeNull()
    expect(screen.getByRole('button', { name: '怎么打开' })).toBeTruthy()
  })

  it('应用详情：用旧格式清单装的应用，详情带 legacy_note 时原样显示成一条提示；没有或不是字符串时不显示', async () => {
    const NOTE = '这个应用是用旧格式的清单装的：照常运行，能启停和删除，但不接服务'
    routes['GET /v1/apps/old-app'] = () => app({ name: 'old-app', catalog_id: 'old-app', legacy_note: NOTE })
    location.hash = '#app-old-app'
    const { unmount } = render(<App />)
    expect((await screen.findByTestId('legacy-note')).textContent).toBe(NOTE)
    unmount()
    routes['GET /v1/apps/old-app'] = () => ({ ...app({ name: 'old-app', catalog_id: 'old-app' }), legacy_note: true })
    render(<App />)
    await screen.findByTestId('ov-run')
    expect(screen.queryByTestId('legacy-note')).toBeNull()
  })

  it('我的应用：命令行部署的应用在表格里显示当前是第几版（列表接口没有完整的 live，只有 live_seq）；卡片上不写版本', async () => {
    const d = (id: string, seq: number, status: string) => ({ id, app: 'x', seq, source_kind: 'upload', source_ref: 'x', status, created_at: '2026-11-08T01:00:00Z' })
    routes['GET /v1/apps'] = () => ({ apps: [
      app({ name: 'hello', exposure: 'public', url: 'http://203.0.113.7:18820', live: undefined, live_seq: 7, current_deployment: 'd7', latest: d('d7', 7, 'live') }),
      app({ name: 'shop', exposure: 'public', live: undefined, live_seq: 3, current_deployment: 'd3', latest: d('d4', 4, 'building') }),
      app({ name: 'older', exposure: 'public', live: undefined, current_deployment: 'd2', latest: d('d2', 2, 'live') }),
      app({ name: 'fresh', exposure: 'public', live: undefined, current_deployment: '', observed_state: 'missing', latest: d('d1', 1, 'queued') }),
      app({ name: 'broken', exposure: 'public', live: undefined, current_deployment: '', observed_state: 'missing', latest: d('d1', 1, 'failed') }),
      app({ name: 'blank', exposure: 'public', live: undefined, current_deployment: '', observed_state: 'missing' }),
      app({ name: 'extra0', exposure: 'public' }), app({ name: 'extra1', exposure: 'public' }),
    ] })
    try { localStorage.removeItem('afx-apps-view') } catch { /* 前一个测试切到了表格视图 */ }
    location.hash = '#apps'
    render(<App />)
    const cards = await screen.findAllByTestId('my-app')
    expect(cards.some((c) => /第 \d+ 版/.test(c.textContent || ''))).toBe(false)
    fireEvent.click(await screen.findByRole('tab', { name: '表格' }))
    const row = (n: string) => screen.getAllByRole('row').find((r) => within(r).queryByRole('link', { name: n }))!
    expect(within(row('hello')).getByText('第 7 版')).toBeTruthy()
    expect(within(row('hello')).getByText('正常')).toBeTruthy()
    expect(within(row('shop')).getByText('第 3 版')).toBeTruthy()
    expect(within(row('older')).getByText('第 2 版')).toBeTruthy()
    expect(within(row('fresh')).getByText('第 1 版更新中')).toBeTruthy()
    expect(within(row('broken')).getByText('还没有装好的版本')).toBeTruthy()
    expect(within(row('blank')).getAllByText('还没装好').length).toBeGreaterThan(0)
    try { localStorage.removeItem('afx-apps-view') } catch { /* 还原 */ }
  })

  it('应用详情：在“谁能打开”卡片里改成局域网后，卡片换成地址和三档；第一次登录那一行一直在卡片里，只有一处', async () => {
    const hint = { lan_address: '192.168.1.20', lan_url_after_expose: 'http://192.168.1.20:18810', has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open new-api' }
    let exposure = 'local'
    routes['GET /v1/apps/new-api'] = () => exposure === 'local'
      ? app({ name: 'new-api', catalog_id: 'new-api', url: 'http://127.0.0.1:18810', local_only: true, open_hint: hint })
      : app({ name: 'new-api', catalog_id: 'new-api', url: 'http://192.168.1.20:18810', exposure: 'lan' })
    routes['GET /v1/apps/new-api/first-login'] = () => ({ app: 'new-api', url: 'http://127.0.0.1:18810', fields: [{ label: '用户名', value: 'admin' }] })
    routes['PATCH /v1/apps/new-api'] = () => { exposure = 'lan'; return { app: app({ name: 'new-api', exposure: 'lan' }) } }
    location.hash = '#app-new-api'
    render(<App />)
    const box = await screen.findByTestId('open-hint')
    // 只本机、要按说明打开：卡片里是说明，没有地址和三档，登录那一行放在说明下面
    const card = screen.getByTestId('ov-access')
    expect(within(card).getByTestId('open-hint')).toBe(box)
    expect(within(card).queryByRole('group', { name: /谁能打开/ })).toBeNull()
    const alone = await screen.findByTestId('ov-login')
    expect(within(alone).getByText('admin')).toBeTruthy()
    fireEvent.click(within(box).getByRole('button', { name: '让家里的电脑、手机也能打开' }))
    fireEvent.click(within(box).getByRole('button', { name: '好，改' }))
    // 改好后应用的信息刷新，卡片换成地址和三档（局域网亮着）
    await vi.waitFor(() => expect(screen.getByTestId('ov-access').textContent).toContain('192.168.1.20:18810'))
    expect(screen.queryByTestId('open-hint')).toBeNull()
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ exposure: 'lan' })
    const access = screen.getByTestId('ov-access')
    expect(within(access).getByRole('button', { name: '局域网' }).getAttribute('aria-pressed')).toBe('true')
    expect(within(access).getByTestId('first-login').textContent).toContain('admin')
    expect(screen.getAllByTestId('first-login').length).toBe(1)
    expect(screen.queryByText(/只在服务器本机能打开/)).toBeNull()
  })

  it('说明块：改成局域网后 hint 没了仍显示完成态；应用又被改回只本机时回到“怎么打开”', async () => {
    // 旧服务端把改成局域网以后的地址放在 open_hint.lan_url 里，照样认
    const hint = { lan_address: '192.168.1.20', lan_url: 'http://192.168.1.20:18810', has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open new-api' }
    routes['PATCH /v1/apps/new-api'] = () => ({ app: app({ name: 'new-api', exposure: 'lan' }) })
    const onChanged = vi.fn()
    const ui = (h?: typeof hint, openURL?: string) => (
      <ToastProvider>
        <ConsoleProvider session={{ csrf: 'tok', expires_at: '', server: 'home-server' }} install={() => {}} logout={async () => {}}>
          <OpenHintBlock name="new-api" hint={h} openURL={openURL} onChanged={onChanged} />
        </ConsoleProvider>
      </ToastProvider>
    )
    const r = render(ui(hint))
    fireEvent.click(screen.getByRole('button', { name: '让家里的电脑、手机也能打开' }))
    fireEvent.click(screen.getByRole('button', { name: '好，改' }))
    const done = await screen.findByTestId('open-lan-done')
    expect(onChanged).toHaveBeenCalled()
    // 列表刷新前：用说明里的局域网地址
    expect(within(done).getByRole('link').getAttribute('href')).toBe('http://192.168.1.20:18810')
    // 列表刷新后 hint 没了：完成态还在，地址用应用现在的地址
    r.rerender(ui(undefined, 'http://192.168.1.20:18810/'))
    expect(within(screen.getByTestId('open-lan-done')).getByRole('link').getAttribute('href')).toBe('http://192.168.1.20:18810/')
    // 又改回只本机：回到说明
    r.rerender(ui(hint))
    expect(screen.queryByTestId('open-lan-done')).toBeNull()
    expect(screen.getByRole('button', { name: '让家里的电脑、手机也能打开' })).toBeTruthy()
    // 从来不是只本机的应用：什么都不显示
    r.rerender(ui(undefined))
    expect(screen.queryByTestId('open-hint')).toBeNull()
  })

  it('从旧的 #…/advanced 进来：展开设置里的“域名”那一行；三档只在概览卡片里，设置里没有，也没有“跟随默认”', async () => {
    routes['GET /v1/apps/web'] = () => app({ name: 'web', exposure: 'public', exposure_is_default: true, default_exposure: 'public' })
    location.hash = '#app-web/advanced'
    render(<App />)
    const set = await screen.findByTestId('app-settings')
    expect(within(set).getByRole('button', { name: /^域名/ }).getAttribute('aria-expanded')).toBe('true')
    expect(screen.queryByLabelText(/跟随默认/)).toBeNull()
    expect(screen.queryByTestId('exposure-default')).toBeNull()
    expect(screen.queryByRole('group', { name: /谁能打开/ })).toBeNull()
  })

  it('概览的“谁能打开”卡片：三档能点，局域网直接改；改成公网要先确认；点亮着的那一档什么都不做', async () => {
    routes['GET /v1/apps/web'] = () => app({ name: 'web', exposure: 'local', url: 'http://192.168.1.20:18810' })
    routes['PATCH /v1/apps/web'] = () => app({ name: 'web', exposure: 'public' })
    location.hash = '#app-web'
    const r = render(<App />)
    const card = await screen.findByTestId('ov-access')
    const group = within(card).getByRole('group', { name: /谁能打开/ })
    expect(within(group).getAllByRole('button').map((o) => o.textContent)).toEqual(['只本机', '局域网', '公网'])
    fireEvent.click(within(card).getByRole('button', { name: '公网' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/互联网上任何人都能打开它的登录页/)).toBeTruthy()
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false)
    fireEvent.click(within(dialog).getByRole('button', { name: '改成公网' }))
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'PATCH' && (c.body as { exposure: string }).exposure === 'public')).toBe(true))
    await vi.waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    calls = []
    const lan = within(card).getByRole('button', { name: '局域网' }) as HTMLButtonElement
    await vi.waitFor(() => expect(lan.disabled).toBe(false))
    fireEvent.click(lan)
    expect(screen.queryByRole('dialog')).toBeNull()
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ exposure: 'lan' }))
    // 点亮着的那一档什么都不做
    expect((within(card).getByRole('button', { name: '只本机' }) as HTMLButtonElement).disabled).toBe(true)
    r.unmount()

    // 经 AFX 云这类不是三档之一的：卡片只给地址，不显示三档，设置里也改不了
    routes['GET /v1/apps/web'] = () => app({ name: 'web', exposure: 'afx_cloud', url: 'http://10.0.0.5:18810' })
    location.hash = '#app-web'
    render(<App />)
    await screen.findByTestId('ov-access')
    expect(screen.queryByRole('group', { name: /谁能打开/ })).toBeNull()
  })

  it('外壳里没有 AFX 云和旧版入口，也没有账号菜单', async () => {
    location.hash = '#apps'
    render(<App />)
    await screen.findByRole('heading', { name: '我的应用' })
    expect(screen.queryAllByText(/AFX 云/).length).toBe(0)
    expect(screen.queryByText('搬到云上')).toBeNull()
    expect(screen.queryByRole('button', { name: '账号菜单' })).toBeNull()
    expect(screen.queryByText('切回旧版')).toBeNull()
    // 左侧菜单只有四项
    const nav = screen.getByRole('navigation', { name: '主菜单' })
    expect(within(nav).getAllByRole('link').map((l) => l.textContent?.replace(/\d+$/, '').trim())).toEqual(['首页', '我的应用', '应用中心', '设置'])
  })

  it('会话过期时提示重新执行 acornfox open', async () => {
    routes['GET /v1/console/session'] = () => json(401, { error: { code: 'session_expired', message: '会话已过期' } })
    render(<App />)
    await screen.findByText('需要重新登录')
    expect(screen.getByText('acornfox open')).toBeTruthy()
  })
})

describe('旧格式的清单和空目录', () => {
  const MSG36 = 'hermes-agent v0.23.0 是旧格式（第 2 版）的清单，这一版平台装不了：要等这个应用按新格式重新上架。不是你的操作不对；什么都没建、没下载'
  const former = () => json(409, { error: { code: 'CAT-INST-36', message: MSG36, hint: '要等重新上架后才能装' } })
  const upgradeLines = (root: HTMLElement) => within(root).queryAllByText(/get\.sh \| sudo bash -s -- upgrade/, { selector: 'code' })

  it('应用卡片：原样显示服务端的那句话，“安装”不可点；别的应用照常能装', async () => {
    routes['GET /v1/catalog'] = () => catalogFormer
    await openStore()
    const [hermes, newApi] = screen.getAllByTestId('catalog-app')
    expect(within(hermes).getByTestId('former-note').textContent).toBe(FORMER_NOTE)
    expect((within(hermes).getByRole('button', { name: '安装' }) as HTMLButtonElement).disabled).toBe(true)
    expect(within(hermes).queryByTestId('needs-platform')).toBeNull()
    expect(within(newApi).queryByTestId('former-note')).toBeNull()
    expect((within(newApi).getByRole('button', { name: '安装' }) as HTMLButtonElement).disabled).toBe(false)
    // 点了不可点的按钮不会开弹窗
    fireEvent.click(within(hermes).getByRole('button', { name: '安装' }))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('应用卡片：已经装过的旧应用照常给“已安装 · 管理”，不显示那句话', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalogFormer, apps: catalogFormer.apps.map((a) => (a.id === 'hermes-agent' ? { ...a, installed: ['hermes-agent'] } : a)) })
    await openStore()
    const hermes = screen.getAllByTestId('catalog-app')[0]
    expect(within(hermes).getByText('已装好')).toBeTruthy()
    expect(within(hermes).getByRole('link', { name: '去管理' })).toBeTruthy()
    expect(within(hermes).queryByTestId('former-note')).toBeNull()
  })

  it('解决方案卡片：解决方案里有旧格式的应用时原样显示那句话，“一起装好”不可点；别的解决方案不受影响', async () => {
    routes['GET /v1/catalog'] = () => ({
      ...catalogFormer,
      recipes: [...catalogFormer.recipes, { id: 'pair', title: '两个一起用', summary: '', apps: [{ app: 'new-api' }, { app: 'memos' }], delivery: { tier: 'auto', minutes: 2, memory_mb: 768 }, command: 'acornfox install --recipe pair' }],
    })
    await openStore()
    const cards = screen.getAllByTestId('catalog-recipe')
    // 只装一个应用的配方不摆
    expect(cards.length).toBe(2)
    const [bad, ok] = cards
    expect(within(bad).getByText('Hermes 智能体 + New API')).toBeTruthy()
    expect(within(bad).getByTestId('former-note').textContent).toBe(FORMER_NOTE)
    expect((within(bad).getByRole('button', { name: '一起装好' }) as HTMLButtonElement).disabled).toBe(true)
    expect(within(ok).queryByTestId('former-note')).toBeNull()
    expect((within(ok).getByRole('button', { name: '一起装好' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('详情页：409 CAT-INST-36 时用列表里的基本信息，服务端的话原样显示，安装不可点，不给升级命令和安装命令', async () => {
    routes['GET /v1/catalog'] = () => catalogFormer
    routes['GET /v1/catalog/apps/hermes-agent'] = former
    location.hash = '#item-hermes-agent'
    render(<App />)
    await screen.findByRole('heading', { name: 'Hermes 智能体' })
    const page = screen.getByTestId('item-limited-former')
    expect(within(page).getByTestId('item-former').textContent).toContain(FORMER_NOTE)
    expect(within(page).getByTestId('item-former-message').textContent).toBe(MSG36)
    // 照着跑只会被拒，所以不给可复制的安装命令
    expect(within(page).queryByText(/acornfox install/)).toBeNull()
    expect(within(page).queryByRole('button', { name: '复制命令' })).toBeNull()
    expect((within(page).getByRole('button', { name: '安装' }) as HTMLButtonElement).disabled).toBe(true)
    expect(within(page).queryByTestId('needs-platform')).toBeNull()
    expect(within(page).queryByTestId('upgrade-commands')).toBeNull()
    expect(upgradeLines(page).length).toBe(0)
    expect(page.textContent).not.toMatch(/升级/)
    expect(screen.queryByTestId('item-limited')).toBeNull()
  })

  it('详情页：列表里没标 former、读了应用包才知道是旧格式时，也照样说明，不当成加载失败', async () => {
    routes['GET /v1/catalog/apps/hermes-agent'] = former
    location.hash = '#item-hermes-agent'
    render(<App />)
    await screen.findByRole('heading', { name: 'Hermes 智能体' })
    expect(screen.getByTestId('item-former').textContent).toContain('这是旧格式的清单，这一版装不了')
    expect(screen.getByTestId('item-former-message').textContent).toBe(MSG36)
    expect(upgradeLines(document.body).length).toBe(0)
    expect(screen.queryByText(/acornfox install/)).toBeNull()
  })

  it('安装弹窗：读详情被拒（CAT-INST-36）时服务端的说明原样显示，不给升级命令，没有安装按钮', async () => {
    routes['GET /v1/catalog/apps/hermes-agent'] = former
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[0]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByText(MSG36)
    expect(within(dialog).queryByTestId('upgrade-first')).toBeNull()
    expect(upgradeLines(dialog).length).toBe(0)
    expect(within(dialog).queryByRole('button', { name: '安装' })).toBeNull()
    expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/catalog/install')).toBe(false)
  })

  it('提交后被拒（CAT-INST-36）：说明原样显示，不提升级、不给升级命令', async () => {
    routes['POST /v1/catalog/install'] = former
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByTestId('mem-check')
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    await within(dialog).findByText(MSG36)
    expect(within(dialog).queryByTestId('upgrade-first')).toBeNull()
    expect(upgradeLines(dialog).length).toBe(0)
  })

  it('目录里一个应用都没有、没在搜索：说“还没有可以安装的应用”，不说“没找到”', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalog, apps: [], recipes: [] })
    render(<App />)
    await screen.findByRole('heading', { name: '应用中心' })
    const empty = await screen.findByTestId('store-empty')
    expect(empty.getAttribute('data-searching')).toBe('false')
    expect(within(empty).getByText('还没有可以安装的应用')).toBeTruthy()
    expect(screen.queryByText(/没找到/)).toBeNull()
    // 空目录里搜东西：仍说没找到
    fireEvent.change(screen.getByLabelText('找应用'), { target: { value: 'abc' } })
    expect(screen.getByTestId('store-empty').getAttribute('data-searching')).toBe('true')
    expect(screen.getByText(/没找到“abc”/)).toBeTruthy()
    expect(screen.getByRole('button', { name: '看全部应用' })).toBeTruthy()
    expect(screen.queryByText('还没有可以安装的应用')).toBeNull()
  })

  it('有应用时搜不到：仍是原来的“没找到”说法', async () => {
    await openStore()
    fireEvent.change(screen.getByLabelText('找应用'), { target: { value: '不存在的东西' } })
    expect(screen.getByText(/没找到“不存在的东西”/)).toBeTruthy()
    expect(screen.queryByText('还没有可以安装的应用')).toBeNull()
  })
})

describe('应用自己的必填设置项（例如模型名）', () => {
  it('和别的设置项一样：不填点安装在页面上拦下并提示；填了进请求的 params，命令行跟着变', async () => {
    routes['GET /v1/catalog/apps/hermes-agent'] = () => hermesWithModel
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[0]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    await within(dialog).findByTestId('mem-check')
    // 必填的放在外面，不收进“更多设置”；有星号和提示
    const input = within(dialog).getByLabelText('模型名 *') as HTMLInputElement
    expect(input.closest('details')).toBeNull()
    expect(within(dialog).getByText('要调用的模型，上下文要不少于 64K，例如 qwen-plus')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    expect(await within(dialog).findByText('请填写模型名')).toBeTruthy()
    expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/catalog/install')).toBe(false)
    fireEvent.change(input, { target: { value: 'qwen-plus' } })
    expect(within(dialog).queryByText(/acornfox install/)).toBeNull()
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    await vi.waitFor(() => expect(calls.filter((c) => c.method === 'POST' && c.url === '/v1/catalog/install').map((c) => c.body)).toEqual([{ app: 'hermes-agent', params: { model: 'qwen-plus' } }]))
  })
})

describe('图标', () => {
  it('目录图标用 <img> 显示（里面的脚本不会执行），没有图标用缩写', () => {
    const { container } = render(<ToastProvider><Tile id="new-api" name="New API" icon={'<svg><script>alert(1)</script></svg>'} /><Tile id="x" name="Hermes 智能体" /></ToastProvider>)
    const img = container.querySelector('img')!
    expect(img.getAttribute('src')!.startsWith('data:image/svg+xml')).toBe(true)
    expect(container.querySelector('script')).toBeNull()
    expect(screen.getByText('He')).toBeTruthy()
  })
})

describe('应用详情：名片、四个页签、设置', () => {
  const started = new Date(Date.now() - 3 * 86400000 - 3600000).toISOString()
  const web = (over: Parameters<typeof app>[0] = {}) => app({
    name: 'web', exposure: 'lan', url: 'http://192.168.1.20:18810', memory_mb: 640, cpu_milli: 2000,
    observed: { name: 'af-web-a', state: 'running', running: true, restart_count: 0, host_port: 18810, started_at: started },
    env: [{ key: 'TZ', secret: false, value: 'UTC' }],
    volumes: [{ app: 'web', path: '/data', volume_name: 'af-web-data', auto: false, created_at: '' }],
    addons: [{ kind: 'postgres', image: 'postgres:16', env_var: 'DATABASE_URL', host: 'af-web-postgres', port: 5432, volume_name: 'v', observed_state: 'running', created_at: '2026-11-06T01:00:00Z' }],
    ...over,
  })
  beforeEach(() => {
    routes['GET /v1/apps/web'] = () => web()
    routes['GET /v1/apps/web/deployments'] = () => ({ deployments: [] })
  })

  it('名片：名字、一句话状态、“打开”和“更多”菜单（重启、停止）；副标题里没有版本和地址；页签是概览、日志、版本、设置', async () => {
    routes['POST /v1/apps/web/stop'] = () => ({})
    location.hash = '#app-web'
    render(<App />)
    const h1 = await screen.findByRole('heading', { name: 'web' })
    const head = h1.closest('.mb-4') as HTMLElement
    expect(within(head).getByTestId('app-status-line').textContent).toBe('正常· 已连续运行 3 天')
    expect(head.textContent).not.toContain('192.168.1.20')
    expect(head.textContent).not.toContain('第 1 版')
    expect(within(head).getByRole('link', { name: /打开/ }).getAttribute('href')).toBe('http://192.168.1.20:18810')
    // “打开”只在名片上，访问卡片里没有
    expect(screen.getAllByRole('link', { name: /打开/ }).length).toBe(1)
    expect(within(head).queryByRole('button', { name: '重启' })).toBeNull()
    fireEvent.click(within(head).getByRole('button', { name: '更多操作' }))
    const menu = within(head).getByRole('menu')
    // 菜单里的几项：停止、重启、日志、版本（自己部署的项目叫“版本”）、删除
    expect(within(menu).getAllByRole('menuitem').map((m) => m.textContent)).toEqual(['停止', '重启', '日志', '版本', '删除'])
    fireEvent.click(within(menu).getByRole('menuitem', { name: '停止' }))
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/apps/web/stop')).toBe(true))
    const tabs = screen.getByRole('tablist', { name: '应用详情' })
    expect(within(tabs).getAllByRole('tab').map((t) => t.textContent)).toEqual(['概览', '日志', '版本', '设置'])
    fireEvent.click(within(tabs).getByRole('tab', { name: '设置' }))
    expect(location.hash).toBe('#app-web/settings')
    expect(await screen.findByTestId('app-settings')).toBeTruthy()
  })

  it('名片：停着的应用写“停了”，“更多”里只有启动', async () => {
    routes['GET /v1/apps/web'] = () => web({ desired: 'stopped', observed_state: 'stopped', observed: { name: 'c', state: 'exited', running: false, restart_count: 0, host_port: 0 } })
    location.hash = '#app-web'
    render(<App />)
    expect((await screen.findByTestId('app-status-line')).textContent).toBe('停了')
    fireEvent.click(screen.getByRole('button', { name: '更多操作' }))
    // 停着的：只有启动，没有停止和重启
    expect(within(screen.getByRole('menu')).getAllByRole('menuitem').map((m) => m.textContent)).toEqual(['启动', '日志', '版本', '删除'])
  })

  it('概览：访问卡片在最上面，三张卡片在下面；概览里不再有单独的首次登录卡片', async () => {
    routes['GET /v1/apps/web'] = () => web({ catalog_id: 'web' })
    routes['GET /v1/apps/web/first-login'] = () => ({ app: 'web', url: 'http://192.168.1.20:18810', fields: [{ label: '账号', value: 'admin' }, { label: '密码', value: '******', secret: true }] })
    routes['GET /v1/apps/web/first-login?reveal=true'] = () => ({ app: 'web', fields: [{ label: '账号', value: 'admin' }, { label: '密码', value: 'S3cret', secret: true }] })
    location.hash = '#app-web'
    render(<App />)
    const access = await screen.findByTestId('ov-access')
    const run = await screen.findByTestId('ov-run')
    expect(access.compareDocumentPosition(run) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    const login = await within(access).findByTestId('first-login')
    expect(screen.getAllByTestId('first-login').length).toBe(1)
    expect(screen.queryByText('首次登录')).toBeNull()
    expect(within(login).getByText('******')).toBeTruthy()
    fireEvent.click(within(login).getByRole('button', { name: '显示' }))
    await within(login).findByText('S3cret')
    expect(calls.some((c) => c.url === '/v1/apps/web/first-login?reveal=true')).toBe(true)
  })

  it('设置：一行一项，摘要写现状；点一行才展开，同一时间只展开一项', async () => {
    routes['GET /v1/apps/web'] = () => web({ domains: [{ app: 'web', name: 'demo.example.com', status: 'ready', created_at: '' }] })
    location.hash = '#app-web/settings'
    render(<App />)
    const set = await screen.findByTestId('app-settings')
    const sum = (k: string) => within(set).getByTestId('summary-' + k).textContent
    expect(sum('access')).toBe('demo.example.com')
    expect(sum('limits')).toBe('内存 640 MB · CPU 2 核')
    expect(sum('data')).toBe('存在这台机器上，删应用时默认留着 · 附加服务 PostgreSQL')
    expect(sum('more')).toBe('环境变量 1 个 · 端口和健康检查')
    expect(within(set).getByText('删除应用')).toBeTruthy()
    expect(within(set).getByText('默认留着数据')).toBeTruthy()
    const row = (n: RegExp) => within(set).getByRole('button', { name: n })
    expect(set.querySelectorAll('[aria-expanded="true"]').length).toBe(0)
    fireEvent.click(row(/最多能用多少内存/))
    expect(row(/最多能用多少内存/).getAttribute('aria-expanded')).toBe('true')
    expect(screen.getByLabelText('最多用的内存')).toBeTruthy()
    fireEvent.click(row(/^数据/))
    expect(row(/最多能用多少内存/).getAttribute('aria-expanded')).toBe('false')
    expect(screen.queryByLabelText('最多用的内存')).toBeNull()
    expect(screen.getByText('af-web-data')).toBeTruthy()
    fireEvent.click(row(/^数据/))
    expect(set.querySelectorAll('[aria-expanded="true"]').length).toBe(0)
  })

  it('以前的标签地址：#…/config、/data、/resources 跳到设置并展开开发者设置、数据、最多能用多少内存', async () => {
    const cases: [string, RegExp, string][] = [['config', /^开发者设置/, '程序端口'], ['data', /^数据/, '附加服务'], ['resources', /最多能用多少内存/, '最多用的内存']]
    for (const [old, name, label] of cases) {
      location.hash = '#app-web/' + old
      const r = render(<App />)
      const set = await screen.findByTestId('app-settings')
      expect(within(screen.getByRole('tablist', { name: '应用详情' })).getByRole('tab', { name: '设置' }).getAttribute('aria-selected')).toBe('true')
      expect(within(set).getByRole('button', { name }).getAttribute('aria-expanded')).toBe('true')
      expect(set.querySelectorAll('[aria-expanded="true"]').length).toBe(1)
      expect(within(set).getAllByText(label).length).toBeGreaterThan(0)
      r.unmount()
    }
  })

  it('设置 → 域名：添加、删掉域名', async () => {
    routes['GET /v1/apps/web'] = () => web({ domains: [{ app: 'web', name: 'old.example.com', status: 'pending', created_at: '' }] })
    routes['POST /v1/apps/web/domains'] = () => ({})
    routes['DELETE /v1/apps/web/domains/old.example.com'] = () => ({})
    location.hash = '#app-web/settings/access'
    render(<App />)
    const doms = await screen.findByTestId('domains')
    expect(within(doms).getByText('正在生效')).toBeTruthy()
    fireEvent.change(within(doms).getByLabelText('新域名'), { target: { value: ' New.Example.com ' } })
    fireEvent.click(within(doms).getByRole('button', { name: '添加' }))
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'POST' && c.url === '/v1/apps/web/domains')?.body).toEqual({ name: 'new.example.com' }))
    const rm = within(doms).getByRole('button', { name: '删掉域名 old.example.com' }) as HTMLButtonElement
    await vi.waitFor(() => expect(rm.disabled).toBe(false))
    fireEvent.click(rm)
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/v1/apps/web/domains/old.example.com')).toBe(true))
  })

  it('设置 → 域名：自动给的域名一行——生效、没生效、没有平台域名时不显示；两个都有时写明用自己的', async () => {
    routes['GET /v1/apps/web'] = () => web({ exposure: 'public', url: 'https://web.apps.example.com', platform_domain: { host: 'web.apps.example.com', active: true, note: '' } })
    location.hash = '#app-web/settings/access'
    let r = render(<App />)
    let row = await screen.findByTestId('platform-domain')
    expect(within(row).getByText('自动给的')).toBeTruthy()
    expect(within(row).getByText('web.apps.example.com')).toBeTruthy()
    expect(within(row).getByText('已生效')).toBeTruthy()
    expect(screen.getByTestId('summary-access').textContent).toBe('web.apps.example.com（自动给的）')
    // 只有自动给的、自己一个都没加：不写“两个都有”
    expect(screen.queryByTestId('domain-precedence')).toBeNull()
    expect(within(screen.getByTestId('own-domains')).getByText('没有')).toBeTruthy()
    r.unmount()

    routes['GET /v1/apps/web'] = () => web({ platform_domain: { host: 'web.apps.example.com', active: false, note: '改成公网后自动生效' } })
    r = render(<App />)
    row = await screen.findByTestId('platform-domain')
    expect(within(row).getByText('没生效')).toBeTruthy()
    expect(within(row).getByText('改成公网后自动生效')).toBeTruthy()
    expect(screen.getByTestId('summary-access').textContent).toBe('没有，用地址打开')
    r.unmount()

    routes['GET /v1/apps/web'] = () => web({ exposure: 'public', url: 'https://mine.example.com', platform_domain: { host: 'web.apps.example.com', active: true, note: '' }, domains: [{ app: 'web', name: 'mine.example.com', status: 'ready', created_at: '' }] })
    r = render(<App />)
    await screen.findByTestId('platform-domain')
    expect(screen.getByTestId('domain-precedence').textContent).toBe('两个都有，现在用你自己的。')
    expect(screen.getByTestId('summary-access').textContent).toBe('mine.example.com')
    r.unmount()

    routes['GET /v1/apps/web'] = () => web()
    render(<App />)
    await screen.findByTestId('own-domains')
    expect(screen.queryByTestId('platform-domain')).toBeNull()
    expect(screen.queryByTestId('domain-precedence')).toBeNull()
  })

  it('概览的“谁能打开”卡片：平台给的域名生效时地址是这个域名、没有端口，下面写局域网也能用的地址', async () => {
    routes['GET /v1/apps/web'] = () => web({ exposure: 'public', url: 'https://web.apps.example.com', lan_url: 'http://192.168.1.20:18810', platform_domain: { host: 'web.apps.example.com', active: true, note: '' } })
    location.hash = '#app-web'
    render(<App />)
    const card = await screen.findByTestId('ov-access')
    expect(within(card).getByTitle('https://web.apps.example.com').textContent).toBe('web.apps.example.com')
    expect(within(card).getByText(/同一个网络里也能用 192\.168\.1\.20:18810/)).toBeTruthy()
  })

  it('设置 → 开发者设置：改环境变量后保存并重装一遍；改端口要合法；有常用命令', async () => {
    routes['PUT /v1/apps/web/env/TZ'] = () => ({})
    routes['POST /v1/apps/web/redeploy'] = () => ({})
    routes['PATCH /v1/apps/web'] = () => ({})
    location.hash = '#app-web/settings/more'
    render(<App />)
    const tz = await screen.findByLabelText('变量 TZ 的值')
    fireEvent.change(tz, { target: { value: 'Asia/Shanghai' } })
    fireEvent.click(screen.getByRole('button', { name: '保存并重装一遍' }))
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/apps/web/redeploy')).toBe(true))
    expect(calls.find((c) => c.method === 'PUT')?.body).toEqual({ value: 'Asia/Shanghai', secret: false })
    fireEvent.change(screen.getByLabelText('程序端口'), { target: { value: '70000' } })
    fireEvent.click(within(screen.getByRole('region', { name: '运行设置' })).getByRole('button', { name: '保存' }))
    await screen.findByText(/端口不对/)
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false)
    fireEvent.change(screen.getByLabelText('程序端口'), { target: { value: '8080' } })
    fireEvent.click(within(screen.getByRole('region', { name: '运行设置' })).getByRole('button', { name: '保存' }))
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ port: 8080, health_path: '/' }))
    expect(screen.getByText('acornfox status web')).toBeTruthy()
    expect(screen.getByText('acornfox logs web -f')).toBeTruthy()
  })

  it('设置 → 最多能用多少内存：改内存上限后保存发 PATCH；不再显示“现在”的用量', async () => {
    routes['PATCH /v1/apps/web'] = () => ({ applied_now: true })
    location.hash = '#app-web/settings/limits'
    render(<App />)
    const mem = (await screen.findByLabelText('最多用的内存')) as HTMLSelectElement
    expect(screen.queryByText('每 5 秒更新')).toBeNull()
    const save = within(screen.getByTestId('app-settings')).getByRole('button', { name: '保存' }) as HTMLButtonElement
    expect(save.disabled).toBe(true)
    fireEvent.change(mem, { target: { value: '1024' } })
    fireEvent.click(save)
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ memory_mb: 1024, cpu_milli: 2000 }))
  })

  it('设置 → 数据：加一个附加服务；关掉一个时可以选同时删数据', async () => {
    routes['POST /v1/apps/web/addons'] = () => ({})
    routes['DELETE /v1/apps/web/addons/postgres'] = () => ({})
    location.hash = '#app-web/settings/data'
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: 'Redis' }))
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'POST' && c.url === '/v1/apps/web/addons')?.body).toEqual({ kind: 'redis' }))
    const off = screen.getByRole('button', { name: '关闭' }) as HTMLButtonElement
    await vi.waitFor(() => expect(off.disabled).toBe(false))
    fireEvent.click(off)
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByLabelText('数据也删掉（找不回来）'))
    // 弹窗右上角的 × 也叫“关闭”，点的是底下那个文字按钮
    fireEvent.click(within(dialog).getAllByRole('button', { name: '关闭' }).find((b) => b.textContent === '关闭')!)
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/v1/apps/web/addons/postgres?volumes=true')).toBe(true))
  })

  it('设置 → 删除应用：默认留着数据；不留要输入应用名才能删', async () => {
    routes['DELETE /v1/apps/web'] = () => ({})
    location.hash = '#app-web/settings/delete'
    const r = render(<App />)
    const set = await screen.findByTestId('app-settings')
    expect(set.querySelectorAll('[aria-expanded="true"]').length).toBe(0)
    fireEvent.click(within(set).getByRole('button', { name: '删除' }))
    let dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: '删除' }))
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/v1/apps/web?volumes=false')).toBe(true))
    await vi.waitFor(() => expect(location.hash).toBe('#apps'))
    r.unmount()

    calls = []
    location.hash = '#app-web/settings'
    render(<App />)
    fireEvent.click(within(await screen.findByTestId('app-settings')).getByRole('button', { name: '删除' }))
    dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByLabelText(/留着它的数据/))
    const del = within(dialog).getByRole('button', { name: '删除' }) as HTMLButtonElement
    expect(del.disabled).toBe(true)
    fireEvent.change(within(dialog).getByLabelText(/输入.*确认/), { target: { value: 'web' } })
    expect(del.disabled).toBe(false)
    fireEvent.click(del)
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/v1/apps/web?volumes=true')).toBe(true))
  })
})

// act 警告来自轮询定时器，测试结束时组件已卸载，不影响结果
void act
