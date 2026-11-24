// 应用概览的“它用到的服务”卡片：整理（lib/app-links.ts）和画法，加整页里放在哪、旧服务端（404）时不显示。
import { render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { appLinks, hasLinks } from '@/lib/app-links'
import type { ServiceView } from '@/lib/types'
import { app, catalog, resources } from '@/test/fixtures'
import { LinksCard } from './links-card'

const pg: ServiceView = {
  name: 'postgres16', kind: 'postgres', kind_label: '数据库', source: 'app', app: 'postgres16', status: 'ok', status_label: '可用',
  values: [], used_by: [{ app: 'n8n', use: 'db' }, { app: 'blog', use: 'db', gone: true }],
}
const myLLM: ServiceView = {
  name: '我的 DeepSeek', kind: 'llm_gateway', kind_label: '大模型接口', source: 'user', app: '', status: 'ok',
  values: [], used_by: [{ app: 'n8n', use: 'llm' }],
}
const redis: ServiceView = {
  name: 'redis7', kind: 'redis', kind_label: '缓存', source: 'app', app: 'redis7', status: 'provider_stopped',
  values: [], used_by: [{ app: 'n8n', use: 'cache' }],
}
const idle: ServiceView = { name: 'mysql84', kind: 'mysql', kind_label: '', source: 'app', app: 'mysql84', status: 'ok', values: [], used_by: [] }

describe('整理连接', () => {
  it('用着的：used_by 里有它；提供的：每个在用的应用一行；已删、数据还留着的不算', () => {
    const n8n = appLinks([pg, myLLM, redis, idle], 'n8n')
    expect(n8n.uses.map((u) => [u.kind, u.provider, u.service, u.status.tone])).toEqual([
      ['数据库', 'postgres16', 'postgres16', 'success'],
      ['大模型接口', '', '我的 DeepSeek', 'success'],
      ['缓存', 'redis7', 'redis7', 'warning'],
    ])
    expect(n8n.usedBy).toEqual([])
    const p = appLinks([pg, myLLM, redis, idle], 'postgres16')
    expect(p.uses).toEqual([])
    expect(p.usedBy).toEqual([{ kind: '数据库', service: 'postgres16', app: 'n8n' }])
    expect(appLinks([pg], 'blog').uses).toEqual([])
    expect(hasLinks(appLinks([pg, idle], 'mysql84'))).toBe(false)
    expect(hasLinks(appLinks(null, 'n8n'))).toBe(false)
  })
})

describe('“它用到的服务”卡片', () => {
  it('用别人的：一行一句，提供方的应用名、“已接上”；自己添加的写“你添加的”；不能用时换成服务的状态', () => {
    const { container } = render(<LinksCard links={appLinks([pg, myLLM, redis], 'n8n')} />)
    const rows = screen.getAllByTestId('ov-link-uses')
    expect(rows.map((r) => within(r).getByTestId('link-text').textContent)).toEqual([
      '数据库用的是：postgres16',
      '大模型接口用的是：你添加的“我的 DeepSeek”',
      '缓存用的是：redis7',
    ])
    expect(within(rows[0]).getByText('已接上')).toBeTruthy()
    expect(within(rows[1]).getByText('已接上')).toBeTruthy()
    expect(within(rows[2]).getByText('redis7 停了，暂时用不了')).toBeTruthy()
    expect(screen.queryByTestId('ov-link-usedby')).toBeNull()
    expect(container.textContent).not.toMatch(/变量|used_by/)
  })

  it('被别人用：每个在用的应用一行，右边“所以现在不能删它”', () => {
    render(<LinksCard links={appLinks([pg], 'postgres16')} />)
    const row = screen.getByTestId('ov-link-usedby')
    expect(within(row).getByTestId('link-text').textContent).toBe('n8n 正在用它，所以现在不能删')
    expect(screen.queryByTestId('ov-link-uses')).toBeNull()
  })

  it('一行都没有、或没读到（旧服务端）时不显示卡片', () => {
    const { container, rerender } = render(<LinksCard links={appLinks([idle, pg], 'mysql84')} />)
    expect(container.querySelector('[data-testid="ov-links"]')).toBeNull()
    rerender(<LinksCard links={null} />)
    expect(container.querySelector('[data-testid="ov-links"]')).toBeNull()
  })
})

describe('概览里的“它用到的服务”卡片', () => {
  let routes: Record<string, () => unknown>
  beforeEach(() => {
    routes = {
      'GET /v1/console/session': () => ({ csrf: 'tok', expires_at: '', server: 'home-server' }),
      'GET /v1/apps': () => ({ apps: [app({ name: 'n8n' }), app({ name: 'postgres16' })] }),
      'GET /v1/catalog/install': () => ({ tasks: [] }),
      'GET /v1/catalog': () => catalog,
      'GET /v1/resources': () => resources,
      'GET /v1/status': () => ({ runner: 'ok', docker_version: '27.3' }),
      'GET /v1/apps/n8n': () => app({ name: 'n8n', url: 'http://192.168.1.20:18810', exposure: 'lan' }),
      'GET /v1/services': () => ({ services: [pg] }),
    }
    vi.stubGlobal('fetch', vi.fn(async (input: string) => {
      const h = routes['GET ' + String(input).split('?')[0]]
      if (!h) return new Response('404 page not found', { status: 404 })
      return new Response(JSON.stringify(h()), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }))
  })
  afterEach(() => vi.unstubAllGlobals())

  it('放在“谁能打开”卡片下面', async () => {
    location.hash = '#app-n8n'
    render(<App />)
    const card = await screen.findByTestId('ov-links')
    expect(within(card).getByText('它用到的服务')).toBeTruthy()
    const access = screen.getByTestId('ov-access')
    expect(access.compareDocumentPosition(card) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(access.nextElementSibling).toBe(card)
  })

  it('旧服务端没有服务接口（404）：不显示，概览照常', async () => {
    delete routes['GET /v1/services']
    location.hash = '#app-n8n'
    render(<App />)
    await screen.findByTestId('ov-run')
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByTestId('ov-links')).toBeNull()
  })
})
