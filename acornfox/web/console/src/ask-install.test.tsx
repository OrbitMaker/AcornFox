// “让 AI 帮我装”：应用页和解决方案页上的按钮，点了复制一段话给 AI 工作台。
// 有操作手册时话里叫它先取手册（解决方案加 --recipe）；没有时说照通用 Skill 做。页面上不出现手册的内容。
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { askInstallText } from './lib/catalog'
import { catalog, hermesDetail, newApiDetail, resources } from './test/fixtures'
import type { CatalogView } from './lib/types'

type Handler = (url: string, init?: RequestInit) => unknown
let routes: Record<string, Handler>

function json(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
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
    const h = routes[method + ' ' + url] || routes[method + ' ' + url.split('?')[0]]
    if (!h) return json(404, { error: { code: 'not_found', message: '对象不存在' } })
    const v = h(url, init)
    return v instanceof Response ? v : json(200, v)
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
})

function stubClipboard(): string[] {
  const copied: string[] = []
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async (t: string) => { copied.push(t) } } })
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  return copied
}

const skill = { version: '0.23.0', checked: '2026-11-24', size: 1200 }
// 目录里 Hermes 和“Hermes + New API”这个解决方案都有手册。
const withManuals: CatalogView = {
  ...catalog,
  apps: catalog.apps.map((a) => (a.id === 'hermes-agent' ? { ...a, show: { ...a.show, skill } } : a)),
  recipes: catalog.recipes.map((r) => (r.id === 'hermes-with-new-api' ? { ...r, show: { ...r.show, skill } } : r)),
}

describe('askInstallText', () => {
  const app = { id: 'memos', command: 'acornfox install memos', source: { id: 'public', name: '公共目录' } }
  it('有手册：叫 AI 先取手册再照着做，冲突以通用 Skill 为准', () => {
    const t = askInstallText('app', { ...app, show: { skill } }, 'Memos')
    expect(t).toContain('应用“Memos”（编号 memos）')
    expect(t).toContain('先运行 acornfox --json catalog skill memos 取它的操作手册，照着做')
    expect(t).not.toContain('--recipe')
    expect(t).toContain('以通用 Skill 为准')
    expect(t).toContain('acornfox install memos，先加 --plan')
  })
  it('解决方案加 --recipe', () => {
    const t = askInstallText('recipe', { id: 'office', command: 'acornfox install --recipe office', show: { skill } }, '办公套装')
    expect(t).toContain('解决方案“办公套装”（编号 office）')
    expect(t).toContain('先运行 acornfox --json catalog skill office --recipe 取它的操作手册')
  })
  it('没有手册：话里说照通用 Skill 做', () => {
    const t = askInstallText('app', app, 'Memos')
    expect(t).toContain('没有专门的操作手册')
    expect(t).toContain('通用 Skill')
    expect(t).not.toContain('先运行 acornfox --json catalog skill memos 取它的操作手册')
  })
  it('私有目录的应用没有手册，就算同编号的公共应用有也不叫它取', () => {
    const t = askInstallText('app', { ...app, source: { id: 'cat-demo', name: '我的目录' }, show: { skill } }, 'Memos')
    expect(t).toContain('没有专门的操作手册')
  })
})

describe('让 AI 帮我装', () => {
  it('应用页：按钮在安装按钮下面，点了复制话；页面上看不到手册和命令', async () => {
    routes['GET /v1/catalog'] = () => withManuals
    routes['GET /v1/catalog/apps/hermes-agent'] = () => ({ ...hermesDetail, show: { skill } })
    const copied = stubClipboard()
    location.hash = '#item-hermes-agent'
    render(<App />)
    await screen.findByRole('heading', { name: 'Hermes 智能体' })
    const buy = (await screen.findByTestId('item-buy'))
    expect(within(buy).queryByText(/catalog skill/)).toBeNull()
    fireEvent.click(within(buy).getByRole('button', { name: /让 AI 帮我装/ }))
    await screen.findByText('已复制')
    expect(copied.length).toBe(1)
    expect(copied[0]).toContain('应用“Hermes 智能体”（编号 hermes-agent）')
    expect(copied[0]).toContain('先运行 acornfox --json catalog skill hermes-agent 取它的操作手册，照着做')
    expect(copied[0]).toContain('以通用 Skill 为准')
    expect(document.body.textContent).not.toContain('catalog skill')
  })

  it('应用页：没有手册的应用也有按钮，话里说照通用 Skill 做', async () => {
    const copied = stubClipboard()
    location.hash = '#item-new-api'
    render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    fireEvent.click(within((await screen.findByTestId('item-buy'))).getByRole('button', { name: /让 AI 帮我装/ }))
    await screen.findByText('已复制')
    expect(copied[0]).toContain('应用“New API”（编号 new-api）')
    expect(copied[0]).toContain('没有专门的操作手册')
    expect(copied[0]).toContain('通用 Skill')
  })

  it('应用页：装好了就没有这个按钮', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, installed: ['new-api'] })
    location.hash = '#item-new-api'
    render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    expect(within((await screen.findByTestId('item-buy'))).queryByRole('button', { name: /让 AI 帮我装/ })).toBeNull()
  })

  it('应用页：这台机器装不了的不给按钮', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, supported: false })
    location.hash = '#item-new-api'
    render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    expect(within((await screen.findByTestId('item-buy'))).queryByRole('button', { name: /让 AI 帮我装/ })).toBeNull()
  })

  it('解决方案页：有手册，话里加 --recipe', async () => {
    routes['GET /v1/catalog'] = () => withManuals
    const copied = stubClipboard()
    location.hash = '#solution-hermes-with-new-api'
    render(<App />)
    await screen.findByTestId('solution-buy')
    fireEvent.click(within((await screen.findByTestId('solution-buy'))).getByRole('button', { name: /让 AI 帮我装/ }))
    await screen.findByText('已复制')
    expect(copied[0]).toContain('解决方案“Hermes 智能体 + New API”（编号 hermes-with-new-api）')
    expect(copied[0]).toContain('先运行 acornfox --json catalog skill hermes-with-new-api --recipe 取它的操作手册')
    expect(copied[0]).toContain('acornfox install --recipe hermes-with-new-api')
  })

  it('解决方案页：没有手册也有按钮', async () => {
    const copied = stubClipboard()
    location.hash = '#solution-hermes-with-new-api'
    render(<App />)
    await screen.findByTestId('solution-buy')
    fireEvent.click(within((await screen.findByTestId('solution-buy'))).getByRole('button', { name: /让 AI 帮我装/ }))
    await screen.findByText('已复制')
    expect(copied[0]).toContain('没有专门的操作手册')
  })

  it('没能复制时提示手动复制', async () => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    Object.defineProperty(window, 'isSecureContext', { configurable: true, value: false })
    document.execCommand = vi.fn(() => false)
    location.hash = '#item-new-api'
    render(<App />)
    await screen.findByRole('heading', { name: 'New API' })
    fireEvent.click(within((await screen.findByTestId('item-buy'))).getByRole('button', { name: /让 AI 帮我装/ }))
    await screen.findByText('没能复制')
  })
})
