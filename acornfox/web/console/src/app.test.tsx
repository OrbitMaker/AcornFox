// 页面级测试：用假的接口（替换 fetch）把控制台渲染出来，走应用中心的主要路径。
import { act, fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { Tile } from './components/bits'
import { OpenHintBlock } from './pages/apps/open-hint'
import { ToastProvider } from './components/ui/toast'
import { app, catalog, hermesDetail, newApiDetail, resources, task } from './test/fixtures'
import type { TaskView } from './lib/types'

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
    'GET /v1/catalog/apps/hermes-agent': () => hermesDetail(false),
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
})

async function openStore() {
  render(<App />)
  await screen.findByRole('heading', { name: '应用中心' })
  await screen.findAllByTestId('catalog-app')
}

describe('应用中心', () => {
  it('应用和配方都来自目录接口', async () => {
    await openStore()
    const cards = screen.getAllByTestId('catalog-app')
    expect(cards.length).toBe(2)
    expect(within(cards[0]).getByText('Hermes 智能体')).toBeTruthy()
    expect(within(cards[1]).getByText('New API')).toBeTruthy()
    expect(screen.getByText('Hermes 智能体 + New API')).toBeTruthy()
    expect(screen.getByText('New API 模型网关')).toBeTruthy()
    // 目录没有配“找专人部署”，页面上就没有，也没有价格
    expect(screen.queryByText(/找专人/)).toBeNull()
    expect(screen.queryByText(/¥/)).toBeNull()
  })

  it('目录里只有一个分类时也显示分类标签（全部 + AI）', async () => {
    await openStore()
    const tabs = screen.getByRole('tablist', { name: '分类' })
    expect(within(tabs).getByRole('tab', { name: '全部' })).toBeTruthy()
    expect(within(tabs).getByRole('tab', { name: 'AI' })).toBeTruthy()
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

  it('PNG 图标按 base64 显示', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, icon: 'iVBORw0KGgo=', icon_type: 'png' })
    location.hash = '#item-new-api'
    const { container } = render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    expect(container.querySelector('img')!.getAttribute('src')).toBe('data:image/png;base64,iVBORw0KGgo=')
  })

  it('已安装的应用卡片给“已安装 · 管理”，点了进我的应用', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalog, apps: catalog.apps.map((a) => (a.id === 'new-api' ? { ...a, installed: ['new-api'] } : a)) })
    routes['GET /v1/apps/new-api'] = () => app({ name: 'new-api' })
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[1]).getByRole('button', { name: '已安装 · 管理' }))
    expect(location.hash).toBe('#app-new-api')
  })

  it('搜不到时提示', async () => {
    await openStore()
    fireEvent.change(screen.getByLabelText('搜索应用'), { target: { value: '不存在的东西' } })
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
    await within(dialog).findByText('acornfox install new-api')
    expect(within(dialog).getByTestId('mem-check').textContent).toContain('要 512 MB · 剩 5 GB')
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    await within(dialog).findByText('下载镜像')
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
    expect(within(dialog).getByText(/127.0.0.1:18810（只本机）/)).toBeTruthy()
  })

  it('装好后只本机：服务器在局域网时确认后一键改成局域网，给局域网地址；用本机 New API 的提示不限额令牌', async () => {
    const hint = { lan_address: '192.168.1.20', lan_url: 'http://192.168.1.20:18811', has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open hermes-agent' }
    routes['GET /v1/apps'] = () => ({ apps: [app({ name: 'hermes-agent', catalog_id: 'hermes-agent', url: 'http://127.0.0.1:18811', local_only: true, open_hint: hint })] })
    routes['PATCH /v1/apps/hermes-agent'] = () => app({ name: 'hermes-agent', exposure: 'lan' })
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    routes['GET /v1/catalog/install/t123'] = (): TaskView => task({
      target: 'hermes-agent', status: 'succeeded',
      steps: task().steps.map((s) => ({ ...s, app: 'hermes-agent', status: 'done' })),
      apps: [{ app: 'hermes-agent', catalog_id: 'hermes-agent', version: 'v0.23.0', reused: false, created: true, url: 'http://127.0.0.1:18811', gateway_token: { provider: 'new-api' } }],
    })
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[0]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('tab', { name: '自己的 Key' }))
    fireEvent.change(within(dialog).getByLabelText('接口地址'), { target: { value: 'https://api.example.com/v1' } })
    fireEvent.change(within(dialog).getByLabelText('Key'), { target: { value: 'sk-test' } })
    fireEvent.change(within(dialog).getByLabelText('模型名'), { target: { value: 'm' } })
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    const box = await within(dialog).findByTestId('open-hint', undefined, { timeout: 3000 })
    expect(within(dialog).getByTestId('token-note').textContent).toContain('hermes-agent 用的是 new-api 里一把不限额的令牌')
    expect(within(dialog).queryByRole('link', { name: /打开/ })).toBeNull()
    fireEvent.click(within(box).getByRole('button', { name: '让局域网里的电脑能打开' }))
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false) // 先确认
    expect(within(box).getByText(/要用它自己的账号密码登录/)).toBeTruthy()
    fireEvent.click(within(box).getByRole('button', { name: '确认改成局域网' }))
    const done = await within(box).findByTestId('open-lan-done')
    expect(within(done).getByRole('link').getAttribute('href')).toBe('http://192.168.1.20:18811')
    expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ exposure: 'lan' })
  })

  it('装好后改成局域网、应用列表刷新以后：说明块原位显示完成态和怎么改回，首次登录地址和“打开”都用应用列表里的新地址', async () => {
    const hint = { lan_address: '192.168.1.20', lan_url: 'http://192.168.1.20:18810', has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open new-api' }
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
    expect(within(dialog).getByTestId('first-login-url').textContent).toContain('http://127.0.0.1:18810（只本机）')
    fireEvent.click(await within(box).findByRole('button', { name: '让局域网里的电脑能打开' }))
    expect(within(box).getByText(/所有网卡上开放端口 18810/)).toBeTruthy()
    fireEvent.click(within(box).getByRole('button', { name: '确认改成局域网' }))
    const open = await within(dialog).findByRole('link', { name: /^打开/ }, { timeout: 3000 })
    expect(open.getAttribute('href')).toBe('http://192.168.1.20:18810')
    expect(within(dialog).queryByText(/只在服务器本机能打开/)).toBeNull()
    // 接口不再给 open_hint，说明块不消失，原位显示完成态
    const done = within(dialog).getByTestId('open-lan-done')
    expect(done.textContent).toContain('已改成局域网，同一局域网的电脑用这个地址打开')
    expect(within(done).getByRole('link').getAttribute('href')).toBe('http://192.168.1.20:18810')
    expect(within(dialog).getByTestId('open-hint').textContent).toContain('acornfox app set --app new-api --expose local')
    expect(within(dialog).queryByRole('button', { name: '让局域网里的电脑能打开' })).toBeNull()
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
    const box = await within(dialog).findByTestId('platform-vars')
    expect(box.hasAttribute('open')).toBe(false)
    expect(within(box).getByText('平台自动填的 2 项（一般不用改）')).toBeTruthy()
    expect(within(box).getByText('cn')).toBeTruthy() // 不能改的只显示
    fireEvent.change(within(box).getByLabelText('时区'), { target: { value: 'Asia/Tokyo' } })
    await within(dialog).findByText('acornfox install new-api --set platform.tz=Asia/Tokyo')
    expect(within(box).getByText('平台填的是 Asia/Shanghai')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    await within(dialog).findByText('下载镜像')
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
    await within(dialog).findByText(/正在导入镜像/, undefined, { timeout: 3000 })
    expect(within(dialog).getByTestId('step-progress').getAttribute('aria-valuenow')).toBeNull()
  })

  it('以前同名应用的数据还在（CAT-INST-24）：点“接上旧数据重装”并确认后才带 reuse_data 重新提交', async () => {
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
    await within(dialog).findByText(/CAT-INST-24/)
    fireEvent.click(within(dialog).getByRole('button', { name: '接上旧数据重装' }))
    expect(posts).toBe(1) // 只是展开确认
    fireEvent.click(within(dialog).getByRole('button', { name: '确认接上' }))
    await within(dialog).findByText('下载镜像')
    const sent = calls.filter((c) => c.method === 'POST' && c.url === '/v1/catalog/install').map((c) => c.body)
    expect(sent).toEqual([{ app: 'new-api' }, { app: 'new-api', reuse_data: true }])
  })

  it('安装失败：显示故障编号、建议和“复制诊断给 AI”', async () => {
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
    await within(dialog).findByText(/CAT-INST-12/)
    expect(within(dialog).getByText(/建议：国内网络确认已配镜像加速/)).toBeTruthy()
    expect(within(dialog).getByRole('button', { name: /复制诊断给 AI/ })).toBeTruthy()
  })

  it('Hermes：选平台 Key 提示即将开放，不能提交', async () => {
    await openStore()
    fireEvent.click(within(screen.getAllByTestId('catalog-app')[0]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByRole('tab', { name: '平台 Key' }))
    expect(await screen.findByText('平台 Key 即将开放')).toBeTruthy()
    expect(within(dialog).getByText('登录后可用')).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: '安装' }))
    await within(dialog).findAllByText(/即将开放，请先用自己的 Key/)
    expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/catalog/install')).toBe(false)
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
    expect(within(dialog).getByText(/找不到这个安装任务/)).toBeTruthy()
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
    await within(dialog).findByText('任务 tbusy')
  })
})

describe('我的应用', () => {
  it('来自应用中心的应用显示目录版本；表格视图显示内存和 CPU', async () => {
    routes['GET /v1/apps'] = () => ({ apps: [app({ name: 'new-api', catalog_id: 'new-api', catalog_version: 'v1.0.0-rc.48' }), app({ name: 'web', desired: 'stopped' })] })
    routes['GET /v1/metrics/apps/new-api'] = () => ({ app: 'new-api', available: true, observed_state: 'running', cpu_percent: 3.2, memory_usage_mb: 120, memory_limit_mb: 512 })
    location.hash = '#apps'
    render(<App />)
    await screen.findAllByTestId('my-app')
    expect(screen.getByText('来自应用中心 · v1.0.0-rc.48')).toBeTruthy()
    expect(screen.getByText('已停止')).toBeTruthy()
    fireEvent.click(screen.getByRole('tab', { name: '表格' }))
    await screen.findByText('120 MB / 512 MB')
    expect(screen.getByText('3.2%')).toBeTruthy()
  })

  it('只本机的应用标“只本机”；Linux 服务器上不给打不开的“打开”', async () => {
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
    expect(within(local).getByText('只本机')).toBeTruthy()
    expect(within(local).queryByRole('link', { name: /打开/ })).toBeNull()
    expect(within(pub).queryByText('只本机')).toBeNull()
    expect(within(pub).getByRole('link', { name: /打开/ }).getAttribute('href')).toBe('http://203.0.113.7:18811')
  })

  it('应用详情：只本机的给怎么打开（公网服务器只有 acornfox open），用了本机 New API 的提示令牌不限额', async () => {
    routes['GET /v1/apps/hermes-agent'] = () => app({
      name: 'hermes-agent', catalog_id: 'hermes-agent', url: 'http://127.0.0.1:18811', local_only: true, gateway_token_from: 'new-api',
      open_hint: { has_login: true, can_lan: false, recommend: 'tunnel', command: 'acornfox open hermes-agent' },
    })
    location.hash = '#app-hermes-agent'
    render(<App />)
    const box = await screen.findByTestId('open-hint')
    expect(within(box).getByText('acornfox open hermes-agent')).toBeTruthy()
    expect(within(box).queryByRole('button', { name: '让局域网里的电脑能打开' })).toBeNull()
    expect(screen.getByTestId('token-note').textContent).toContain('在 new-api 后台的令牌里给它设上限')
    expect(screen.queryByRole('link', { name: /打开/ })).toBeNull()
    expect(screen.getByRole('button', { name: '怎么打开' })).toBeTruthy()
  })

  it('我的应用：命令行部署的应用在列表里显示当前是第几版（列表接口没有完整的 live，只有 live_seq）', async () => {
    const d = (id: string, seq: number, status: string) => ({ id, app: 'x', seq, source_kind: 'upload', source_ref: 'x', status, created_at: '2026-11-08T01:00:00Z' })
    routes['GET /v1/apps'] = () => ({ apps: [
      app({ name: 'hello', exposure: 'public', url: 'http://203.0.113.7:18820', live: undefined, live_seq: 7, current_deployment: 'd7', latest: d('d7', 7, 'live') }),
      app({ name: 'shop', exposure: 'public', live: undefined, live_seq: 3, current_deployment: 'd3', latest: d('d4', 4, 'building') }),
      app({ name: 'older', exposure: 'public', live: undefined, current_deployment: 'd2', latest: d('d2', 2, 'live') }),
      app({ name: 'fresh', exposure: 'public', live: undefined, current_deployment: '', observed_state: 'missing', latest: d('d1', 1, 'queued') }),
      app({ name: 'broken', exposure: 'public', live: undefined, current_deployment: '', observed_state: 'missing', latest: d('d1', 1, 'failed') }),
      app({ name: 'blank', exposure: 'public', live: undefined, current_deployment: '', observed_state: 'missing' }),
    ] })
    try { localStorage.removeItem('afx-apps-view') } catch { /* 前一个测试切到了表格视图 */ }
    location.hash = '#apps'
    render(<App />)
    const cards = await screen.findAllByTestId('my-app')
    const card = (n: string) => cards.find((c) => within(c).queryByText(n, { selector: '.text-sm' }))!
    expect(within(card('hello')).getByText('第 7 版')).toBeTruthy()
    expect(within(card('hello')).getByText('运行中')).toBeTruthy()
    expect(within(card('hello')).queryByText('还没部署')).toBeNull()
    expect(within(card('shop')).getByText('第 3 版')).toBeTruthy()
    expect(within(card('older')).getByText('第 2 版')).toBeTruthy()
    expect(within(card('fresh')).getByText('第 1 版部署中')).toBeTruthy()
    expect(within(card('broken')).getByText('还没有上线的版本')).toBeTruthy()
    expect(within(card('blank')).getAllByText('还没部署').length).toBeGreaterThan(0)
  })

  it('应用详情：在概览里改成局域网后说明块显示完成态，首次登录地址跟着变', async () => {
    const hint = { lan_address: '192.168.1.20', lan_url: 'http://192.168.1.20:18810', has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open new-api' }
    let exposure = 'local'
    routes['GET /v1/apps/new-api'] = () => exposure === 'local'
      ? app({ name: 'new-api', catalog_id: 'new-api', url: 'http://127.0.0.1:18810', local_only: true, open_hint: hint })
      : app({ name: 'new-api', catalog_id: 'new-api', url: 'http://192.168.1.20:18810', exposure: 'lan' })
    routes['GET /v1/apps/new-api/first-login'] = () => ({ app: 'new-api', url: 'http://127.0.0.1:18810', fields: [{ label: '用户名', value: 'admin' }] })
    routes['PATCH /v1/apps/new-api'] = () => { exposure = 'lan'; return { app: app({ name: 'new-api', exposure: 'lan' }) } }
    location.hash = '#app-new-api'
    render(<App />)
    const box = await screen.findByTestId('open-hint')
    expect((await screen.findByTestId('first-login-url')).textContent).toContain('127.0.0.1:18810（只本机）')
    fireEvent.click(within(box).getByRole('button', { name: '让局域网里的电脑能打开' }))
    fireEvent.click(within(box).getByRole('button', { name: '确认改成局域网' }))
    const done = await screen.findByTestId('open-lan-done')
    await vi.waitFor(() => expect(screen.getByTestId('first-login-url').textContent).not.toContain('只本机'))
    expect(screen.getByTestId('first-login-url').textContent).toContain('http://192.168.1.20:18810')
    expect(within(done).getByRole('link').getAttribute('href')).toBe('http://192.168.1.20:18810')
    expect(screen.getByTestId('open-hint').textContent).toContain('“高级”里把访问方式改成“只本机”')
    expect(screen.queryByText(/只在服务器本机能打开/)).toBeNull()
  })

  it('说明块：改成局域网后 hint 没了仍显示完成态；应用又被改回只本机时回到“怎么打开”', async () => {
    const hint = { lan_address: '192.168.1.20', lan_url: 'http://192.168.1.20:18810', has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open new-api' }
    routes['PATCH /v1/apps/new-api'] = () => ({ app: app({ name: 'new-api', exposure: 'lan' }) })
    const onChanged = vi.fn()
    const ui = (h?: typeof hint, openURL?: string) => <ToastProvider><OpenHintBlock name="new-api" hint={h} openURL={openURL} onChanged={onChanged} /></ToastProvider>
    const r = render(ui(hint))
    fireEvent.click(screen.getByRole('button', { name: '让局域网里的电脑能打开' }))
    fireEvent.click(screen.getByRole('button', { name: '确认改成局域网' }))
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
    expect(screen.getByRole('button', { name: '让局域网里的电脑能打开' })).toBeTruthy()
    // 从来不是只本机的应用：什么都不显示
    r.rerender(ui(undefined))
    expect(screen.queryByTestId('open-hint')).toBeNull()
  })

  it('高级页：服务端给了平台默认时可以选“跟随默认”，保存发 exposure 空值；默认是公网时先确认', async () => {
    routes['GET /v1/apps/web'] = () => app({ name: 'web', exposure: 'public', exposure_is_default: true, default_exposure: 'public' })
    routes['PATCH /v1/apps/web'] = () => ({ app: app({ name: 'web', exposure: 'local' }) })
    location.hash = '#app-web/advanced'
    const r = render(<App />)
    const sel = (await screen.findByLabelText('访问方式')) as HTMLSelectElement
    expect(sel.value).toBe('default')
    expect(within(sel).getByRole('option', { name: '跟随默认（现在是公网）' })).toBeTruthy()
    expect((screen.getByRole('button', { name: '保存' }) as HTMLButtonElement).disabled).toBe(true)
    fireEvent.change(sel, { target: { value: 'local' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ exposure: 'local' }))
    r.unmount()

    calls = []
    routes['GET /v1/apps/web'] = () => app({ name: 'web', exposure: 'local', exposure_is_default: false, default_exposure: 'public' })
    render(<App />)
    const sel2 = (await screen.findByLabelText('访问方式')) as HTMLSelectElement
    expect(sel2.value).toBe('local')
    fireEvent.change(sel2, { target: { value: 'default' } })
    expect(screen.getByText(/acornfox app set --app web --expose default/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    const dialog = await screen.findByRole('dialog')
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false)
    fireEvent.click(within(dialog).getByRole('button', { name: '改成公网' }))
    await vi.waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ exposure: '' }))
  })

  it('高级页：服务端没给平台默认（旧版本）时只有三个具体值', async () => {
    routes['GET /v1/apps/web'] = () => app({ name: 'web', exposure: 'local' })
    location.hash = '#app-web/advanced'
    render(<App />)
    const sel = await screen.findByLabelText('访问方式')
    expect(within(sel).getAllByRole('option').map((o) => o.textContent)).toEqual(['只本机', '局域网', '公网'])
  })

  it('外壳里没有 AFX 云和旧版入口', async () => {
    location.hash = '#apps'
    render(<App />)
    await screen.findByRole('heading', { name: '我的应用' })
    expect(screen.queryAllByText(/AFX 云/).length).toBe(0)
    expect(screen.queryByText('搬到云上')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '账号菜单' }))
    expect(screen.queryByText('切回旧版')).toBeNull()
  })

  it('高级页：改成公网要先确认；经 AFX 云的应用只显示、不能在页面改', async () => {
    routes['GET /v1/apps/web'] = () => app({ name: 'web', exposure: 'local' })
    routes['PATCH /v1/apps/web'] = () => app({ name: 'web', exposure: 'public' })
    location.hash = '#app-web/advanced'
    const r = render(<App />)
    const sel = await screen.findByLabelText('访问方式')
    fireEvent.change(sel, { target: { value: 'public' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/所有网卡都能打开/)).toBeTruthy()
    expect(calls.some((c) => c.method === 'PATCH')).toBe(false)
    fireEvent.click(within(dialog).getByRole('button', { name: '改成公网' }))
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'PATCH' && (c.body as { exposure: string }).exposure === 'public')).toBe(true))
    r.unmount()

    routes['GET /v1/apps/web'] = () => app({ name: 'web', exposure: 'afx_cloud' })
    render(<App />)
    const ro = await screen.findByTestId('exposure-readonly')
    expect(within(ro).getByText('经 AFX 云')).toBeTruthy()
    expect(screen.queryByLabelText('访问方式')).toBeNull()
  })

  it('会话过期时提示重新执行 acornfox open', async () => {
    routes['GET /v1/console/session'] = () => json(401, { error: { code: 'session_expired', message: '会话已过期' } })
    render(<App />)
    await screen.findByText('需要重新登录')
    expect(screen.getByText('acornfox open')).toBeTruthy()
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

// act 警告来自轮询定时器，测试结束时组件已卸载，不影响结果
void act
