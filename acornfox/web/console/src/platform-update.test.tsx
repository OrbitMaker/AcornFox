// 平台新版本提醒（W12-3）的页面测试：设置 › 这台机器 › 升级那一行、“现在查一次”、铃铛里的一条。
// 用假的接口（替换 fetch）渲染整个控制台，和 pages.test.tsx 一样。
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { app, catalog, resources } from './test/fixtures'
import type { PlatformUpdate } from './lib/types'

type Handler = (url: string, init?: RequestInit) => unknown
let routes: Record<string, Handler>
let calls: { method: string; url: string }[]

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

const CMD = 'curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade'
const pu = (o: Partial<PlatformUpdate> = {}): PlatformUpdate => ({
  enabled: true, current: '0.7.0', available: false, ok: true, host_kind: 'linux', source: 'gitee', upgrade_command: CMD, upgrade_where: '在这台服务器上运行', ...o,
})
const NEWER = pu({ latest: '0.7.1', available: true, notes_url: 'https://gitee.com/VIP13390/AcornFox/releases/tag/v0.7.1', checked_at: '2026-11-25T02:00:00Z', code: 'PLAT-UPD-01' })
const status = (p?: PlatformUpdate) => ({ runner: 'ok', docker_version: '27.3.1', version: '0.7.0', ...(p ? { platform_update: p, ...(p.latest ? { latest_version: p.latest } : {}) } : {}) })

beforeEach(() => {
  calls = []
  try { localStorage.clear() } catch { /* 没有 */ }
  location.hash = '#prefs/update'
  routes = {
    'GET /v1/console/session': () => ({ csrf: 'tok', expires_at: '', server: 'home-server' }),
    'GET /v1/apps': () => ({ apps: [app({ name: 'blog' })] }),
    'GET /v1/catalog/install': () => ({ tasks: [] }),
    'GET /v1/catalog': () => catalog,
    'GET /v1/resources': () => resources,
    'GET /v1/status': () => status(NEWER),
    'GET /v1/runtime/checks': () => ({ os: 'linux', checked_at: '2026-11-25T03:00:00Z', ok: true, data_dir: '/var/lib/acornfox', docker_version: '27.3.1', items: [{ ok: true, level: 'ok', text: '后台服务：acornfox-server 运行中' }], sources: { china: true, region_source: 'install.env', docker_hub: [], ghcr: [], gateway: '' } }),
  }
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    const method = init?.method || 'GET'
    const url = String(input)
    calls.push({ method, url })
    const h = routes[method + ' ' + url] || routes[method + ' ' + url.split('?')[0]]
    if (!h) return json(404, { error: { code: 'not_found', message: '对象不存在' } })
    const v = h(url, init)
    return v instanceof Response ? v : json(200, v)
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('设置 › 这台机器 › 升级', () => {
  it('有新版本：收起时写“有新版本 0.7.1”；展开有复制给 AI、发行说明、服务端给的命令和现在查一次；没有一键升级', async () => {
    render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('upgrade-state').textContent).toBe('有新版本 0.7.1，现在是 0.7.0'))
    const body = await screen.findByTestId('prefs-update-body')
    expect(within(body).getByRole('button', { name: /复制给 AI 工作台帮我升级/ })).toBeTruthy()
    const notes = within(body).getByRole('link', { name: '看发行说明' }) as HTMLAnchorElement
    expect(notes.href).toBe('https://gitee.com/VIP13390/AcornFox/releases/tag/v0.7.1')
    expect(notes.target).toBe('_blank')
    expect(within(body).getByText(CMD, { selector: 'code' })).toBeTruthy()
    expect(within(body).getByText(/在这台服务器上运行：/)).toBeTruthy()
    expect(within(body).getByRole('button', { name: '现在查一次' })).toBeTruthy()
    expect(within(body).queryByRole('button', { name: /立即升级|一键升级|升级到/ })).toBeNull()
    // 体检说在国内，也不再另给海外那一条：命令只有服务端给的这一条
    expect(screen.getAllByText(/get\.sh \| sudo bash -s -- upgrade/, { selector: 'code' }).length).toBe(1)
  })

  it('Windows 上服务端给的是 PowerShell 的命令，页面照用', async () => {
    const ps = 'powershell -NoProfile -ExecutionPolicy Bypass -Command "…get.ps1…" -Action upgrade'
    routes['GET /v1/status'] = () => status({ ...NEWER, host_kind: 'windows', upgrade_command: ps, upgrade_where: '在这台电脑的 PowerShell 里运行（普通窗口）' })
    render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('upgrade-state').textContent).toBe('有新版本 0.7.1，现在是 0.7.0'))
    const body = await screen.findByTestId('prefs-update-body')
    expect(within(body).getByText(ps, { selector: 'code' })).toBeTruthy()
    expect(within(body).getByText(/在这台电脑的 PowerShell 里运行（普通窗口）：/)).toBeTruthy()
  })

  it('已是最新：写上次查的时间，有“现在查一次”，没有命令', async () => {
    routes['GET /v1/status'] = () => status(pu({ latest: '0.7.0', checked_at: new Date().toISOString() }))
    render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('upgrade-state').textContent).toMatch(/^现在是 0\.7\.0，已是最新（上次查：今天 \d\d:\d\d）$/))
    const body = await screen.findByTestId('prefs-update-body')
    expect(within(body).getByRole('button', { name: '现在查一次' })).toBeTruthy()
    expect(screen.queryByText(/get\.sh/)).toBeNull()
  })

  it('暂时查不到最新版本：说清楚不影响使用，能再查', async () => {
    routes['GET /v1/status'] = () => status(pu({ ok: false, checked_at: '2026-11-25T02:00:00Z', failures: 3, error: 'gitee：连不上' }))
    render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('upgrade-state').textContent).toBe('现在是 0.7.0（暂时查不到最新版本）'))
    const body = await screen.findByTestId('prefs-update-body')
    expect(within(body).getByText(/不影响使用/)).toBeTruthy()
    expect(within(body).getByRole('button', { name: '现在查一次' })).toBeTruthy()
    expect(screen.queryByText(/连不上/)).toBeNull()
  })

  it('关掉了：没有“现在查一次”', async () => {
    routes['GET /v1/status'] = () => status(pu({ enabled: false }))
    render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('upgrade-state').textContent).toBe('现在是 0.7.0（新版本提醒已关闭）'))
    expect(screen.queryByRole('button', { name: '现在查一次' })).toBeNull()
  })

  it('点“现在查一次”：发 POST，再读一次状态，行上换成新结果', async () => {
    let found = false
    routes['GET /v1/status'] = () => status(found ? NEWER : pu({ ok: false }))
    routes['POST /v1/platform/update/check'] = () => { found = true; return NEWER }
    render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('upgrade-state').textContent).toBe('现在是 0.7.0（还没查过最新版本）'))
    fireEvent.click(await screen.findByRole('button', { name: '现在查一次' }))
    await vi.waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.url === '/v1/platform/update/check')).toBe(true))
    await vi.waitFor(() => expect(screen.getByTestId('upgrade-state').textContent).toBe('有新版本 0.7.1，现在是 0.7.0'))
    expect(await screen.findByText('有新版本 0.7.1')).toBeTruthy() // 提示
  })

  it('旧服务端（没有 platform_update）：只比 latest_version，没有“现在查一次”', async () => {
    routes['GET /v1/status'] = () => ({ runner: 'ok', version: '0.6.3', latest_version: '0.6.4' })
    render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('upgrade-state').textContent).toBe('有新版本 0.6.4，现在是 0.6.3'))
    expect(screen.queryByRole('button', { name: '现在查一次' })).toBeNull()
  })
})

describe('铃铛里的“AcornFox 有新版本”', () => {
  const bell = () => screen.findByRole('button', { name: /需要你处理/ })

  it('有新版本且没看过：铃铛里一条；在设置里展开升级那一行以后，同一个版本不再出', async () => {
    location.hash = '#home'
    render(<App />)
    fireEvent.click(await bell())
    const item = await screen.findByTestId('bell-item')
    expect(item.textContent).toContain('AcornFox 有新版本 0.7.1')
    fireEvent.click(item) // 去 #prefs/update，升级那一行展开
    await vi.waitFor(() => expect(localStorage.getItem('afx-update-seen')).toBe('0.7.1'))
    await vi.waitFor(() => expect(screen.queryByTestId('bell-item')).toBeNull())
  })

  it('线上又出了更新的版本：再提醒一次', async () => {
    localStorage.setItem('afx-update-seen', '0.7.1')
    routes['GET /v1/status'] = () => status({ ...NEWER, latest: '0.7.2' })
    location.hash = '#home'
    render(<App />)
    fireEvent.click(await bell())
    expect((await screen.findByTestId('bell-item')).textContent).toContain('AcornFox 有新版本 0.7.2')
  })

  it('已是最新：铃铛里没有这一条', async () => {
    routes['GET /v1/status'] = () => status(pu({ latest: '0.7.0', checked_at: '2026-11-25T02:00:00Z' }))
    location.hash = '#home'
    render(<App />)
    await screen.findByRole('heading', { name: '首页' })
    expect(screen.queryByRole('button', { name: /需要你处理：/ })).toBeNull()
  })
})
