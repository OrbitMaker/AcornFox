// “我的应用”最下面默认收起的“这台机器上的其他容器”：收起展开、没有时不显示、只看不管理。
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import type { OtherContainer, OtherContainersView } from '@/lib/types'
import { app, catalog, resources } from '@/test/fixtures'
import { OthersGroup } from './others-group'

const c = (p: Partial<OtherContainer>): OtherContainer => ({
  id: 'a1', name: 'redis', image: 'redis:7', state: 'running', running: true, created: 1, ports: [], stats_available: false, cpu_available: false, cpu_percent: 0, memory_usage_mb: 0, ...p,
})
const sample: OtherContainersView = {
  observed_at: '2026-11-25T10:00:00Z', running: 1, stopped: 1, memory_mb: 12.3, cpu_percent: 0.4,
  containers: [
    c({ name: 'redis', stats_available: true, cpu_available: true, cpu_percent: 0.4, memory_usage_mb: 12.3, ports: [{ host: 6379, container: 6379, proto: 'tcp' }] }),
    c({ id: 'b2', name: 'old-db', image: 'mysql:5.7', state: 'exited', running: false }),
  ],
}

let others: () => Response
beforeEach(() => {
  others = () => new Response(JSON.stringify(sample), { status: 200, headers: { 'Content-Type': 'application/json' } })
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    const url = String(input).split('?')[0]
    const ok = (b: unknown) => new Response(JSON.stringify(b), { status: 200, headers: { 'Content-Type': 'application/json' } })
    switch (url) {
      case '/v1/host/containers': return others()
      case '/v1/console/session': return ok({ csrf: 'tok', expires_at: '', server: 'home-server' })
      case '/v1/apps': return ok({ apps: [app({ name: 'blog' })] })
      case '/v1/catalog/install': return ok({ tasks: [] })
      case '/v1/catalog': return ok(catalog)
      case '/v1/resources': return ok(resources)
      case '/v1/status': return ok({ runner: 'ok', docker_version: '27.3' })
    }
    return new Response('404 page not found', { status: 404 })
  }))
})
afterEach(() => vi.unstubAllGlobals())

describe('这台机器上的其他容器', () => {
  it('默认收起，只有一句话；点开一行一个：名字、在跑还是停了、端口、内存，加一句“只显示，不管理”', async () => {
    render(<OthersGroup />)
    const group = await screen.findByTestId('others-group')
    expect(within(group).getByRole('button', { name: /这台机器上的其他容器（2）/ }).getAttribute('aria-expanded')).toBe('false')
    expect(screen.getByTestId('others-summary').textContent).toBe('在跑 1 个，停了 1 个，用了 12 MB 内存')
    expect(screen.queryByTestId('others-list')).toBeNull()

    fireEvent.click(within(group).getByRole('button', { name: /这台机器上的其他容器/ }))
    const rows = screen.getAllByTestId('other-container')
    expect(rows.map((r) => r.textContent)).toEqual(['redis在跑端口 637912 MB', 'old-db停了'])
    expect(screen.getByTestId('others-note').textContent).toContain('这些不是从 AcornFox 装的，这里只显示，不管理')
    expect(screen.getByTestId('others-note').textContent).toMatch(/Windows、Mac.*看不到/)
    // 镜像名这类技术内容不上页面
    expect(group.textContent).not.toMatch(/redis:7|mysql:5\.7|镜像/)

    // 再点一下收起
    fireEvent.click(within(group).getByRole('button', { name: /这台机器上的其他容器/ }))
    expect(screen.queryByTestId('others-list')).toBeNull()
  })

  it('只看不管理：这一组里除了展开收起，没有任何按钮或链接', async () => {
    render(<OthersGroup />)
    const group = await screen.findByTestId('others-group')
    fireEvent.click(within(group).getByRole('button', { name: /这台机器上的其他容器/ }))
    expect(within(group).getAllByRole('button').length).toBe(1)
    expect(within(group).queryAllByRole('link').length).toBe(0)
    expect(within(group).queryByText(/启动|停止|删除|重启|接管|打开/)).toBeNull()
  })

  it('没有这样的容器、旧服务端没有这个接口、Docker 读不到时，整组不显示', async () => {
    others = () => new Response(JSON.stringify({ ...sample, containers: [], running: 0, stopped: 0 }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    const { container, unmount } = render(<OthersGroup />)
    await vi.waitFor(() => expect((fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.some((x) => String(x[0]).startsWith('/v1/host/containers'))).toBe(true))
    expect(container.querySelector('[data-testid="others-group"]')).toBeNull()
    unmount()

    others = () => new Response('404 page not found', { status: 404 })
    const r2 = render(<OthersGroup />)
    await vi.waitFor(() => expect((fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.length).toBeGreaterThan(1))
    expect(r2.container.querySelector('[data-testid="others-group"]')).toBeNull()
    r2.unmount()

    others = () => new Response(JSON.stringify({ error: { code: 'MACHINE-04', message: '读不到' } }), { status: 503, headers: { 'Content-Type': 'application/json' } })
    const r3 = render(<OthersGroup />)
    await vi.waitFor(() => expect((fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls.length).toBeGreaterThan(2))
    expect(r3.container.querySelector('[data-testid="others-group"]')).toBeNull()
  })

  it('在“我的应用”页的最下面（应用卡片后面）', async () => {
    location.hash = '#apps'
    render(<App />)
    const group = await screen.findByTestId('others-group')
    const card = await screen.findByTestId('my-app')
    expect(card.compareDocumentPosition(group) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })
})
