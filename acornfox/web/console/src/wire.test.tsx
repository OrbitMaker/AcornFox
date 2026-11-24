// 控制台接上新接口：备份恢复、改设置重新生效、改谁能打开的确认、更新和退回到更新前、安装时选谁能打开、
// 落后的应用、数据库这类应用的页面。用假的接口（替换 fetch）把控制台渲染出来；没登记的接口回 404，和旧服务端一样。
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { app, catalog, newApiDetail, resources, task } from '@/test/fixtures'
import type { AppView } from '@/lib/types'

type Reply = unknown | Response
type Handler = (body: any, url: string) => Reply
let routes: Record<string, Handler>
let calls: { method: string; url: string; body?: any }[]
let n8n: AppView

const json = (status: number, body: unknown) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
const sent = (method: string, path: string) => calls.filter((c) => c.method === method && c.url.split('?')[0] === path)

const VARS = {
  app: 'n8n', from_catalog: true, catalog_id: 'n8n', version: '1.0', can_change: true, notes: [],
  vars: [
    { name: 'params.admin_email', layer: 'custom', label: '管理员邮箱', value: 'a@b.com', secret: false, masked: false, origin: 'user', origin_label: '用户填的', can_edit: true },
    { name: 'params.prune_days', layer: 'custom', label: '每周清理旧记录', value: '14', secret: false, masked: false, origin: 'default', can_edit: false },
  ],
}
const PLAN = { app: 'n8n', needed: true, restart: true, vars: [{ name: 'params.admin_email', label: '管理员邮箱', old: 'a@b.com', new: 'c@d.com', from: 'user' }], notes: [], summary: 'x' }
const RUNNING = { id: 'tk1', kind: 'reapply', target: 'n8n', status: 'running', created_at: '', updated_at: '', steps: [{ app: 'n8n', name: 'check', label: '检查', status: 'running' }], apps: [] }
const DONE = { ...RUNNING, status: 'succeeded', steps: [{ app: 'n8n', name: 'check', label: '检查', status: 'done' }, { app: 'n8n', name: 'start', label: '重启', status: 'done' }] }

beforeEach(() => {
  calls = []
  n8n = app({ name: 'n8n', catalog_id: 'n8n', catalog_version: '2.47.5', exposure: 'lan', url: 'http://192.168.1.20:18810' })
  routes = {
    'GET /v1/console/session': () => ({ csrf: 'tok', expires_at: '', server: 'home-server' }),
    'GET /v1/apps': () => ({ apps: [n8n] }),
    'GET /v1/catalog/install': () => ({ tasks: [] }),
    'GET /v1/catalog': () => catalog,
    'GET /v1/resources': () => resources,
    'GET /v1/status': () => ({ runner: 'ok', docker_version: '27.3' }),
    'GET /v1/apps/n8n': () => n8n,
    'GET /v1/apps/n8n/deployments': () => ({ deployments: [] }),
    'GET /v1/apps/n8n/vars': () => VARS,
    'GET /v1/services': () => ({ services: [] }),
  }
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    const method = init?.method || 'GET'
    const url = String(input)
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    const h = routes[method + ' ' + url] || routes[method + ' ' + url.split('?')[0]]
    if (!h) return new Response('404 page not found', { status: 404 })
    const v = h(body, url)
    return v instanceof Response ? v : json(200, v)
  }))
})
afterEach(() => vi.unstubAllGlobals())

const open = async (hash: string, testid: string) => { location.hash = hash; render(<App />); return screen.findByTestId(testid) }

describe('备份和恢复', () => {
  const B1 = { id: 'b1', app: 'n8n', kind: 'manual', created_at: '2026-11-24T10:30:00', bytes: 12 * 1024 * 1024, version: { catalog_version: '2.47.5' } }
  const B0 = { id: 'b0', app: 'n8n', kind: 'pre-upgrade', created_at: '2026-11-23T09:00:00', bytes: 2 * 1024 * 1024 }
  const LIST = { app: 'n8n', installed: true, backups: [B1, B0], tasks: [], keep: { manual: 5, pre_upgrade: 1, pre_restore: 1 } }
  beforeEach(() => { routes['GET /v1/apps/n8n/backups'] = () => LIST })

  it('设置里多一行“备份”，收起一句话', async () => {
    const set = await open('#app-n8n/settings', 'app-settings')
    await waitFor(() => expect(screen.getByTestId('summary-backup').textContent).toMatch(/^最近一份 .*共 2 份$/))
    expect(Array.from(set.querySelectorAll('[data-section]')).map((x) => x.getAttribute('data-section'))).toEqual(['access', 'vars', 'limits', 'data', 'backup', 'delete'])
  })

  it('服务端不支持备份：整行不出现', async () => {
    delete routes['GET /v1/apps/n8n/backups']
    const set = await open('#app-n8n/settings', 'app-settings')
    await waitFor(() => expect(sent('GET', '/v1/apps/n8n/backups').length).toBeGreaterThan(0))
    await waitFor(() => expect(Array.from(set.querySelectorAll('[data-section]')).map((x) => x.getAttribute('data-section'))).toEqual(['access', 'vars', 'limits', 'data', 'delete']))
  })

  it('立即备份：先读计划，写明会停一小会儿；开始后看步骤，做完提示', async () => {
    routes['POST /v1/apps/n8n/backups'] = (b) => (b.plan ? { plan: { app: 'n8n', need_bytes: 20 << 20, enough: true, will_stop: true, keep: 5, say: '' } } : json(202, { task: { id: 'abc123abc123', op: 'backup', status: 'running', steps: [{ key: 'copy', text: '复制数据', status: 'running' }] } }))
    routes['GET /v1/backups/tasks/abc123abc123'] = () => ({ id: 'abc123abc123', op: 'backup', status: 'succeeded', steps: [{ key: 'copy', text: '复制数据', status: 'done' }], result: { bytes: 3 << 20 } })
    await open('#app-n8n/settings/backup', 'backups')
    fireEvent.click(screen.getByTestId('backup-now'))
    const dlg = await screen.findByTestId('op-confirm')
    expect(dlg.textContent).toContain('会停一小会儿')
    fireEvent.click(screen.getByRole('button', { name: '开始备份' }))
    await screen.findByTestId('op-done')
    expect(sent('POST', '/v1/apps/n8n/backups').map((c) => c.body.plan ?? false)).toEqual([true, false])
    expect(screen.getByTestId('op-steps').textContent).toContain('复制数据')
  })

  it('磁盘不够：确认窗里写原因，开始按钮灰着', async () => {
    routes['POST /v1/apps/n8n/backups'] = () => ({ plan: { app: 'n8n', need_bytes: 9 << 30, enough: false, will_stop: false, keep: 5, say: '备份目录只剩 1 GB' } })
    await open('#app-n8n/settings/backup', 'backups')
    fireEvent.click(screen.getByTestId('backup-now'))
    expect((await screen.findByTestId('backup-nospace')).textContent).toContain('只剩 1 GB')
    expect((screen.getByRole('button', { name: '开始备份' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('磁盘放不下但能清自动存的旧备份：确认窗写明会清几份，开始按钮可点', async () => {
    routes['POST /v1/apps/n8n/backups'] = () => ({ plan: { app: 'n8n', need_bytes: 9 << 30, enough: true, will_stop: false, keep: 5, will_prune_for_space: ['a1', 'a2'], say: '' } })
    await open('#app-n8n/settings/backup', 'backups')
    fireEvent.click(screen.getByTestId('backup-now'))
    expect((await screen.findByTestId('backup-evict')).textContent).toContain('先清掉 2 份自动存的旧备份')
    expect((screen.getByRole('button', { name: '开始备份' }) as HTMLButtonElement).disabled).toBe(false)
  })

  it('恢复：确认窗写明会覆盖现在的数据、先自动备份一份；确认后带 confirm 开始', async () => {
    routes['POST /v1/apps/n8n/backups/b1/restore'] = (b) => (b.plan
      ? { plan: { app: 'n8n', backup: B1, intact: true, version_now: '2.47.5', version_then: '2.47.5', changes_version: false, version_available: true, needs_confirm: true, enough: true, will_stop: true, current_has_data: true, say: '' } }
      : json(202, { task: { id: 'rst123rst123', op: 'restore', status: 'running', steps: [{ key: 'x', text: '换上备份的数据', status: 'running' }] } }))
    routes['GET /v1/backups/tasks/rst123rst123'] = () => ({ id: 'rst123rst123', op: 'restore', status: 'succeeded', steps: [{ key: 'x', text: '换上备份的数据', status: 'done' }], result: { pre_restore_id: 'p1' } })
    await open('#app-n8n/settings/backup', 'backups')
    const items = screen.getAllByTestId('backup-item')
    expect(items.map((i) => i.getAttribute('data-kind'))).toEqual(['manual', 'pre-upgrade'])
    fireEvent.click(within(items[0]).getByRole('button', { name: /^恢复/ }))
    const dlg = await screen.findByTestId('op-confirm')
    expect(dlg.textContent).toContain('会被这份备份覆盖')
    expect(dlg.textContent).toContain('先自动备份一份现在的')
    fireEvent.click(screen.getByRole('button', { name: '覆盖并恢复' }))
    await screen.findByTestId('op-done')
    expect(sent('POST', '/v1/apps/n8n/backups/b1/restore').map((c) => c.body)).toEqual([{ plan: true }, { confirm: true, allow_current_version: false }])
  })

  it('恢复时备份的程序这台机器上没有了：写明会继续用现在的版本，并同意用现在的版本', async () => {
    routes['POST /v1/apps/n8n/backups/b1/restore'] = (b) => (b.plan
      ? { plan: { app: 'n8n', backup: B1, intact: true, version_now: '2.48.0', version_then: '2.47.5', changes_version: true, version_available: false, needs_confirm: true, enough: true, will_stop: true, current_has_data: false, say: '' } }
      : json(202, { task: { id: 'rst123rst123', op: 'restore', status: 'running', steps: [] } }))
    routes['GET /v1/backups/tasks/rst123rst123'] = () => ({ id: 'rst123rst123', op: 'restore', status: 'running', steps: [] })
    await open('#app-n8n/settings/backup', 'backups')
    fireEvent.click(within(screen.getAllByTestId('backup-item')[0]).getByRole('button', { name: /^恢复/ }))
    expect((await screen.findByTestId('restore-version')).textContent).toContain('已经没有了')
    fireEvent.click(screen.getByRole('button', { name: '覆盖并恢复' }))
    await waitFor(() => expect(sent('POST', '/v1/apps/n8n/backups/b1/restore')[1].body.allow_current_version).toBe(true))
  })

  it('删除：红色按钮只在确认窗里，带 confirm=true', async () => {
    routes['DELETE /v1/apps/n8n/backups/b0'] = () => ({ deleted: true })
    await open('#app-n8n/settings/backup', 'backups')
    expect(document.querySelectorAll('.btn-danger').length).toBe(0)
    fireEvent.click(within(screen.getAllByTestId('backup-item')[1]).getByRole('button', { name: /^删除/ }))
    await screen.findByTestId('op-confirm')
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '删除' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.url === '/v1/apps/n8n/backups/b0?confirm=true')).toBe(true))
  })
})

describe('改安装设置并让它生效', () => {
  beforeEach(() => {
    routes['PATCH /v1/apps/n8n/vars'] = (b) => (b.plan ? { plan: PLAN } : b.confirm_restart ? json(202, { changed: true, plan: PLAN, task: RUNNING }) : json(409, { error: { code: 'APP-APPLY-05', message: '要同意' }, plan: PLAN }))
    routes['GET /v1/catalog/install/tk1'] = () => DONE
  })

  it('只有服务端说能改的项有“改”；保存先读计划，确认窗列出新旧值，同意后带 confirm_restart', async () => {
    const set = await open('#app-n8n/settings/vars', 'app-settings')
    const row = await within(set).findByTestId('vars-body')
    expect(within(row).getAllByTestId('var-edit')).toHaveLength(1)
    fireEvent.click(within(row).getByRole('button', { name: '改管理员邮箱' }))
    const ed = within(row).getByTestId('var-editor')
    fireEvent.change(within(ed).getByRole('textbox'), { target: { value: 'c@d.com' } })
    fireEvent.click(within(ed).getByRole('button', { name: '保存' }))
    const dlg = await screen.findByTestId('op-confirm')
    expect(dlg.textContent).toContain('会重启一下')
    expect(screen.getByTestId('reapply-changes').textContent).toContain('a@b.com')
    expect(screen.getByTestId('reapply-changes').textContent).toContain('c@d.com')
    fireEvent.click(screen.getByRole('button', { name: '保存并重启' }))
    await screen.findByTestId('op-done')
    expect(sent('PATCH', '/v1/apps/n8n/vars').map((c) => [c.body.plan ?? false, c.body.confirm_restart ?? false, c.body.set])).toEqual([[true, false, { admin_email: 'c@d.com' }], [false, true, { admin_email: 'c@d.com' }]])
  })

  it('没有变化：不弹确认窗，说一声', async () => {
    routes['PATCH /v1/apps/n8n/vars'] = () => ({ plan: { ...PLAN, needed: false, vars: [] } })
    const set = await open('#app-n8n/settings/vars', 'app-settings')
    const row = await within(set).findByTestId('vars-body')
    fireEvent.click(within(row).getByRole('button', { name: '改管理员邮箱' }))
    fireEvent.change(within(row).getByTestId('var-editor').querySelector('input')!, { target: { value: 'x@y.com' } })
    fireEvent.click(within(row).getByRole('button', { name: '保存' }))
    await waitFor(() => expect(sent('PATCH', '/v1/apps/n8n/vars')).toHaveLength(1))
    expect(screen.queryByTestId('op-confirm')).toBeNull()
  })

  it('服务端说这个应用不能改设置：没有“改”', async () => {
    routes['GET /v1/apps/n8n/vars'] = () => ({ ...VARS, can_change: false })
    const set = await open('#app-n8n/settings/vars', 'app-settings')
    await within(set).findByTestId('vars-body')
    expect(screen.queryByTestId('var-edit')).toBeNull()
  })
})

describe('改谁能打开要重启时先问', () => {
  it('409 带计划：弹确认窗，同意后带 confirm_restart 再改；没成之前不给“已改好”', async () => {
    let n = 0
    routes['PATCH /v1/apps/n8n'] = (b) => {
      n++
      if (b.confirm_restart) return { app: n8n, reapply: { plan: PLAN, task: RUNNING } }
      return json(409, { error: { code: 'APP-APPLY-05', message: '要同意' }, plan: { ...PLAN, vars: [{ name: 'platform.app.url', label: '访问地址', old: 'http://192.168.1.20:18810', new: 'http://127.0.0.1:18810', from: 'platform' }] } })
    }
    routes['GET /v1/catalog/install/tk1'] = () => DONE
    await open('#app-n8n', 'ov-access')
    fireEvent.click(screen.getByRole('button', { name: '只本机' }))
    const dlg = await screen.findByTestId('op-confirm')
    expect(dlg.textContent).toContain('打开的地址')
    expect(n).toBe(1)
    fireEvent.click(screen.getByRole('button', { name: '改好并重启' }))
    await screen.findByTestId('op-done')
    expect(sent('PATCH', '/v1/apps/n8n').map((c) => c.body.confirm_restart ?? false)).toEqual([false, true])
  })

  it('“先改，稍后再让应用跟上”：带 reapply: skip', async () => {
    routes['PATCH /v1/apps/n8n'] = (b) => (b.reapply === 'skip' ? { app: n8n } : json(409, { error: { code: 'APP-APPLY-05', message: '要同意' }, plan: PLAN }))
    await open('#app-n8n', 'ov-access')
    fireEvent.click(screen.getByRole('button', { name: '只本机' }))
    await screen.findByTestId('op-confirm')
    fireEvent.click(screen.getByRole('button', { name: '先改，稍后再让应用跟上' }))
    await waitFor(() => expect(sent('PATCH', '/v1/apps/n8n').map((c) => c.body.reapply ?? '')).toEqual(['', 'skip']))
  })

  it('不用重启的应用：直接改好，不弹窗', async () => {
    routes['PATCH /v1/apps/n8n'] = () => ({ app: n8n })
    await open('#app-n8n', 'ov-access')
    fireEvent.click(screen.getByRole('button', { name: '只本机' }))
    await waitFor(() => expect(sent('PATCH', '/v1/apps/n8n')).toHaveLength(1))
    expect(screen.queryByTestId('op-confirm')).toBeNull()
  })

  it('只本机的应用点“让家里的电脑、手机也能打开”：同样先问，窗在应用换了样子以后还在', async () => {
    n8n = { ...n8n, exposure: 'local', url: 'http://127.0.0.1:18810', local_only: true, open_hint: { has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open n8n', lan_url_after_expose: 'http://192.168.1.20:18810' } }
    routes['PATCH /v1/apps/n8n'] = (b) => {
      if (!b.confirm_restart) return json(409, { error: { code: 'APP-APPLY-05', message: '要同意' }, plan: PLAN })
      n8n = { ...n8n, exposure: 'lan', url: 'http://192.168.1.20:18810', local_only: false, open_hint: undefined }
      return { app: n8n, reapply: { plan: PLAN, task: RUNNING } }
    }
    routes['GET /v1/catalog/install/tk1'] = () => DONE
    await open('#app-n8n', 'ov-access')
    fireEvent.click(screen.getByRole('button', { name: '让家里的电脑、手机也能打开' }))
    fireEvent.click(screen.getByRole('button', { name: '好，改' }))
    await screen.findByTestId('op-confirm')
    fireEvent.click(screen.getByRole('button', { name: '改好并重启' }))
    await screen.findByTestId('op-done')
    expect(sent('PATCH', '/v1/apps/n8n').map((c) => c.body)).toEqual([{ exposure: 'lan' }, { exposure: 'lan', confirm_restart: true }])
  })

  it('加域名同样先问：409 后带 confirm_restart', async () => {
    routes['POST /v1/apps/n8n/domains'] = (b) => (b.confirm_restart ? { domain: { name: 'a.example.com' } } : json(409, { error: { code: 'APP-APPLY-05', message: '要同意' }, plan: PLAN }))
    await open('#app-n8n/settings/access', 'domains')
    fireEvent.change(screen.getByLabelText('新域名'), { target: { value: 'a.example.com' } })
    fireEvent.click(screen.getByRole('button', { name: '添加' }))
    await screen.findByTestId('op-confirm')
    fireEvent.click(screen.getByRole('button', { name: '改好并重启' }))
    await waitFor(() => expect(sent('POST', '/v1/apps/n8n/domains').map((c) => c.body.confirm_restart ?? false)).toEqual([false, true]))
  })
})

describe('落在后面的应用', () => {
  const AFFECTED = { var: '', notes: [], apps: [{ app: 'n8n', catalog_id: 'n8n', uses: true, differs: true, restart: true, vars: [{ name: 'platform.tz', label: '时区', old: 'UTC', new: 'Asia/Shanghai', from: 'platform' }], summary: 's' }] }
  beforeEach(() => { routes['GET /v1/platform/affected'] = () => AFFECTED })

  it('概览最上面有提示和“让它跟上”：先读计划，同意后带 confirm_restart', async () => {
    routes['POST /v1/apps/n8n/reapply'] = (b) => (b.plan ? { plan: { ...PLAN, vars: AFFECTED.apps[0].vars } } : json(202, { changed: true, plan: PLAN, task: RUNNING }))
    routes['GET /v1/catalog/install/tk1'] = () => DONE
    const al = await open('#app-n8n', 'behind-alert')
    expect(al.textContent).toContain('时区变了')
    fireEvent.click(screen.getByRole('button', { name: '让它跟上' }))
    await screen.findByTestId('op-confirm')
    fireEvent.click(screen.getByRole('button', { name: '让它跟上并重启' }))
    await screen.findByTestId('op-done')
    expect(sent('POST', '/v1/apps/n8n/reapply').map((c) => c.body)).toEqual([{ plan: true }, { confirm_restart: true }])
  })

  it('我的应用卡片写“设置要跟上”；铃铛里也有一件；服务端没有这个接口时都没有', async () => {
    location.hash = '#apps'
    const first = render(<App />)
    expect((await screen.findByTestId('card-behind')).textContent).toContain('设置要跟上')
    first.unmount()
    delete routes['GET /v1/platform/affected']
    render(<App />)
    await screen.findByTestId('my-app')
    expect(screen.queryByTestId('card-behind')).toBeNull()
  })
})

describe('更新和退回到更新前', () => {
  const upgrade = (over: object = {}) => ({ available: '2.48.0', can_upgrade: true, snapshot: null, can_rollback: false, ...over })
  const UPLAN = { from: '2.47.5', to: '2.48.0', restart: true, snapshot_mb: 64, disk_ok: true, vars_added: ['时区'], notes: [] }

  it('服务端说能更新：有“更新到 2.48.0”，读计划、确认、看步骤；更新前先备份的话写在窗里', async () => {
    n8n = { ...n8n, catalog_update: '2.48.0', upgrade: upgrade() }
    routes['POST /v1/apps/n8n/upgrade'] = (b) => (b.plan ? { plan: UPLAN } : json(202, { task: { ...RUNNING, id: 'up1', kind: 'upgrade', steps: [{ app: 'n8n', name: 'image', label: '下载镜像', status: 'running', progress: { phase: 'download', done_bytes: 50 << 20, total_bytes: 100 << 20, bytes_per_sec: 5 << 20 } }] } }))
    routes['GET /v1/catalog/install/up1'] = () => ({ ...DONE, id: 'up1', kind: 'upgrade' })
    await open('#app-n8n/versions', 'update-available')
    fireEvent.click(screen.getByTestId('update-go'))
    const dlg = await screen.findByTestId('op-confirm')
    expect(dlg.textContent).toContain('从 2.47.5 更新到 2.48.0')
    expect(dlg.textContent).toContain('先把现在的数据备份一份')
    fireEvent.click(screen.getByRole('button', { name: '更新并重启' }))
    expect(await screen.findByTestId('step-progress-text')).toBeTruthy()
    await screen.findByTestId('op-done')
    expect(sent('POST', '/v1/apps/n8n/upgrade').map((c) => c.body)).toEqual([{ plan: true }, { confirm_restart: true }])
  })

  it('磁盘不够：更新按钮灰着，原因写在窗里', async () => {
    n8n = { ...n8n, catalog_update: '2.48.0', upgrade: upgrade() }
    routes['POST /v1/apps/n8n/upgrade'] = () => ({ plan: { ...UPLAN, disk_ok: false } })
    await open('#app-n8n/versions', 'update-available')
    fireEvent.click(screen.getByTestId('update-go'))
    expect((await screen.findByTestId('upgrade-nospace')).textContent).toContain('磁盘空间不够')
    expect((screen.getByRole('button', { name: '更新并重启' }) as HTMLButtonElement).disabled).toBe(true)
  })

  it('服务端说现在不能更新：只写原因，没有按钮；没说 upgrade 时也没有按钮', async () => {
    n8n = { ...n8n, catalog_update: '2.48.0', upgrade: upgrade({ can_upgrade: false, blocked: '磁盘不够', blocked_code: 'UPG-02' }) }
    const first = render(<App />)
    location.hash = '#app-n8n/versions'
    expect((await screen.findByTestId('update-blocked')).textContent).toBe('现在不能更新：磁盘不够')
    expect(screen.queryByTestId('update-go')).toBeNull()
    first.unmount()
  })

  it('更新成功后留着更新前的备份：“退回到更新前”，窗里写明会丢更新后新增的数据，红色按钮', async () => {
    n8n = { ...n8n, upgrade: upgrade({ can_upgrade: false, can_rollback: true, snapshot: { id: 'b9', version: '2.47.5', created_at: '2026-11-24T10:30:00', size_mb: 64 } }) }
    routes['POST /v1/apps/n8n/rollback'] = (b) => (b.plan ? { plan: {} } : json(202, { task: { id: 'rb1rb1rb1rb1', op: 'restore', status: 'running', steps: [] } }))
    routes['GET /v1/backups/tasks/rb1rb1rb1rb1'] = () => ({ id: 'rb1rb1rb1rb1', op: 'restore', status: 'succeeded', steps: [] })
    const card = await open('#app-n8n/versions', 'update-rollback')
    expect(card.textContent).toContain('更新以后新增的数据会丢掉')
    fireEvent.click(within(card).getByRole('button', { name: '退回到更新前' }))
    const dlg = await screen.findByTestId('op-confirm')
    expect(dlg.textContent).toContain('更新以后新增的数据会丢掉')
    fireEvent.click(screen.getByRole('button', { name: '退回并丢掉新数据' }))
    await screen.findByTestId('op-done')
    expect(sent('POST', '/v1/apps/n8n/rollback').map((c) => c.body)).toEqual([{ plan: true }, { confirm: true }])
  })

  it('服务端没有 upgrade：更新页签不提退回', async () => {
    await open('#app-n8n/versions', 'app-update')
    expect(screen.queryByTestId('update-rollback')).toBeNull()
  })
})

describe('装应用时选谁能打开', () => {
  it('默认是服务端给的一档；不改就不发 expose，改了才发', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, exposure: 'lan' })
    routes['POST /v1/catalog/plan'] = () => ({ can_install: true, apps: [] })
    routes['POST /v1/catalog/install'] = () => json(202, task())
    routes['GET /v1/catalog/install/t123'] = () => task()
    location.hash = '#item-new-api'
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: /^安装/ }))
    const choice = await screen.findByTestId('expose-choice')
    expect(within(choice).getByRole('button', { name: '局域网' }).getAttribute('aria-pressed')).toBe('true')
    fireEvent.click(within(choice).getByRole('button', { name: '只本机' }))
    expect(screen.getByTestId('expose-who').textContent).toContain('只有这台机器自己能打开')
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: '安装' }))
    await waitFor(() => expect(sent('POST', '/v1/catalog/install')).toHaveLength(1))
    expect(sent('POST', '/v1/catalog/install')[0].body.expose).toBe('local')
  })

  it('只在内部网络的应用（数据库这类）没有这一项；这台机器不在公网上时“公网”灰着', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, exposure: 'internal' })
    routes['POST /v1/catalog/plan'] = () => ({ can_install: true, apps: [] })
    location.hash = '#item-new-api'
    const first = render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: /^安装/ }))
    await screen.findByRole('dialog')
    await waitFor(() => expect(sent('POST', '/v1/catalog/plan').length).toBeGreaterThan(0))
    expect(screen.queryByTestId('expose-choice')).toBeNull()
    first.unmount()
    routes['GET /v1/catalog/apps/new-api'] = () => ({ ...newApiDetail, exposure: 'lan' })
    routes['GET /v1/status'] = () => ({ runner: 'ok', on_public: false })
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: /^安装/ }))
    const choice = await screen.findByTestId('expose-choice')
    await waitFor(() => expect((within(choice).getByRole('button', { name: '公网' }) as HTMLButtonElement).disabled).toBe(true))
  })

  it('下载应用的进度：已下多少、一共多少、多快、还要多久', async () => {
    routes['GET /v1/catalog/apps/new-api'] = () => newApiDetail
    routes['POST /v1/catalog/plan'] = () => ({ can_install: true, apps: [] })
    routes['POST /v1/catalog/install'] = () => json(202, task())
    const running = task({ steps: [{ app: 'new-api', name: 'image', label: '下载镜像', status: 'running', progress: { phase: 'pull', done_bytes: 120 << 20, total_bytes: 300 << 20, bytes_per_sec: 2 << 20, source: 'docker.m.daocloud.io' } }] })
    routes['GET /v1/catalog/install/t123'] = () => running
    location.hash = '#item-new-api'
    render(<App />)
    fireEvent.click(await screen.findByRole('button', { name: /^安装/ }))
    fireEvent.click(await within(await screen.findByRole('dialog')).findByRole('button', { name: '安装' }))
    const t = await screen.findByTestId('step-progress-text')
    expect(t.textContent).toBe('120 MB / 300 MB · 2 MB/秒 · 剩约 2 分钟')
    expect(screen.getByTestId('step-progress').getAttribute('aria-valuenow')).toBe('40')
  })
})

describe('数据库这类只在内部网络的应用', () => {
  it('设置里没有域名、开发者设置，数据里没有附加服务；有备份', async () => {
    n8n = app({ name: 'n8n', catalog_id: 'postgres16', internal_only: true, exposure: 'internal', url: '', catalog_version: '16.4' })
    routes['GET /v1/apps/n8n/backups'] = () => ({ app: 'n8n', installed: true, backups: [], tasks: [], keep: { manual: 5, pre_upgrade: 1, pre_restore: 1 } })
    routes['GET /v1/apps/n8n/vars'] = () => ({ ...VARS, vars: [] })
    const set = await open('#app-n8n/settings/data', 'app-settings')
    expect(Array.from(set.querySelectorAll('[data-section]')).map((x) => x.getAttribute('data-section'))).toEqual(['vars', 'limits', 'data', 'backup', 'delete'])
    expect(screen.queryByLabelText('附加服务')).toBeNull()
    await waitFor(() => expect(screen.getByTestId('summary-backup').textContent).toBe('还没有备份'))
  })
})

describe('找专人部署和展示内容的同步情况', () => {
  it('读 assist：可见、有 https 链接才显示，价格跟着；链接是空串不显示；旧的 expert 也认', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalog, assist: { visible: true, price_cny: 99, url: 'https://acornfox.com/expert' } })
    location.hash = '#store'
    const first = render(<App />)
    expect((await screen.findAllByRole('button', { name: '找专人部署 · ¥99' })).length).toBeGreaterThan(0)
    first.unmount()
    routes['GET /v1/catalog'] = () => ({ ...catalog, assist: { visible: true, price_cny: 99, url: '' } })
    const second = render(<App />)
    await screen.findAllByText('Hermes 智能体')
    expect(screen.queryByRole('button', { name: /找专人部署/ })).toBeNull()
    second.unmount()
    routes['GET /v1/catalog'] = () => ({ ...catalog, expert: { price_cny: 50, url: 'https://acornfox.com/e' } })
    render(<App />)
    expect((await screen.findAllByRole('button', { name: '找专人部署 · ¥50' })).length).toBeGreaterThan(0)
  })

  it('展示内容没能用上：体检里多一行，算一件要处理的事', async () => {
    routes['GET /v1/catalog'] = () => ({ ...catalog, sync: { ok: true, show: { state: 'failed', code: 'CAT-SHOW-02', message: '签名不对' } } })
    routes['GET /v1/runtime/checks'] = () => ({ os: 'linux', checked_at: '2026-11-24T10:00:00Z', ok: true, items: [{ ok: true, level: 'ok', text: '磁盘还剩 318 GB', key: 'disk' }] })
    location.hash = '#prefs/checkup'
    render(<App />)
    await waitFor(() => expect(screen.getAllByTestId('runtime-check').map((r) => r.textContent).join('|')).toContain('应用中心的介绍这次没更新上，先用上次的'))
    await waitFor(() => expect(screen.getByTestId('checkup-conclusion').textContent).toBe('有 1 件事要处理'))
  })
})
