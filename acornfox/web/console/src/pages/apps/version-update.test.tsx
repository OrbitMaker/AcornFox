// 从应用中心装的应用：概览的“版本”卡片和“更新”页签。服务端没说有新版本时什么都不猜（不写“已是最新”），
// 说了才写“有新版本 x”；应用升级还没做，所以没有“更新”按钮。
import { render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { app, catalog, resources } from '@/test/fixtures'
import type { AppView } from '@/lib/types'

let n8n: AppView

beforeEach(() => {
  n8n = app({ name: 'n8n', catalog_id: 'n8n', catalog_version: '2.47.5', exposure: 'lan', url: 'http://192.168.1.20:18810' })
  const routes: Record<string, () => unknown> = {
    'GET /v1/console/session': () => ({ csrf: 'tok', expires_at: '', server: 'home-server' }),
    'GET /v1/apps': () => ({ apps: [n8n] }),
    'GET /v1/catalog/install': () => ({ tasks: [] }),
    'GET /v1/catalog': () => catalog,
    'GET /v1/resources': () => resources,
    'GET /v1/status': () => ({ runner: 'ok', docker_version: '27.3' }),
    'GET /v1/apps/n8n': () => n8n,
    'GET /v1/apps/n8n/deployments': () => ({ deployments: [] }),
    'GET /v1/services': () => ({ services: [] }),
  }
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    const h = routes['GET ' + String(input).split('?')[0]]
    if (!h) return new Response('404 page not found', { status: 404 })
    return new Response(JSON.stringify(h()), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }))
})
afterEach(() => vi.unstubAllGlobals())

describe('版本卡片和更新页签', () => {
  it('服务端没说有没有新版本：版本卡片只写现在的版本，更新页签不写“已是最新”', async () => {
    location.hash = '#app-n8n'
    const first = render(<App />)
    const card = await screen.findByTestId('ov-ver')
    expect(card.textContent).toContain('2.47.5')
    expect(card.textContent).not.toContain('已是最新')
    expect(screen.queryByTestId('ov-ver-update')).toBeNull()
    first.unmount()
    location.hash = '#app-n8n/versions'
    render(<App />)
    const panel = await screen.findByTestId('app-update')
    expect(panel.textContent).toContain('现在是')
    expect(panel.textContent).not.toContain('已是最新')
    expect(within(panel).queryByRole('button', { name: '更新' })).toBeNull()
  })

  it('服务端说有新版本：版本卡片写“有新版本 2.48.0”能点去更新页签；更新页签写同一句，但没有“更新”按钮', async () => {
    n8n = { ...n8n, catalog_update: '2.48.0' }
    location.hash = '#app-n8n'
    const first = render(<App />)
    const link = await screen.findByTestId('ov-ver-update')
    expect(link.textContent).toBe('有新版本 2.48.0')
    first.unmount()
    location.hash = '#app-n8n/versions'
    render(<App />)
    const panel = await screen.findByTestId('app-update')
    expect(within(panel).getByTestId('update-available').textContent).toContain('有新版本 2.48.0')
    expect(within(panel).queryByRole('button', { name: '更新' })).toBeNull()
  })
})
