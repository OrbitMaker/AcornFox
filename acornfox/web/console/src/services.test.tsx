// 页面级测试：服务。设置里的“服务”分区、安装弹窗里的“需要的服务”、应用概览里用到的服务。
// 用假的接口（替换 fetch）把控制台渲染出来；接口不存在时回 404，和旧服务端一样。
import { fireEvent, render, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { app, catalog, hermesDetail, needOf, newApiDetail, planOf, resources, serviceKinds, services, task } from './test/fixtures'
import type { InstallPlan } from './lib/types'

type Handler = (url: string, init?: RequestInit) => unknown
let routes: Record<string, Handler>
let calls: { method: string; url: string; body?: unknown }[]

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
    'GET /v1/status': () => ({ runner: 'ok', docker_version: '27.3', version: '0.6.0-dev' }),
    'GET /v1/catalog/apps/new-api': () => newApiDetail,
    'GET /v1/catalog/apps/hermes-agent': () => hermesDetail,
    'GET /v1/services': () => ({ services }),
    'GET /v1/services/kinds': () => ({ kinds: serviceKinds }),
  }
  vi.stubGlobal('fetch', vi.fn(async (input: string, init?: RequestInit) => {
    const method = init?.method || 'GET'
    const url = String(input)
    calls.push({ method, url, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    const h = routes[method + ' ' + url] || routes[method + ' ' + url.split('?')[0]]
    if (!h) return new Response('404 page not found', { status: 404 })
    const v = h(url, init)
    return v instanceof Response ? v : json(200, v)
  }))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

const sent = (method: string, url: string) => calls.filter((c) => c.method === method && c.url === url).map((c) => c.body)

async function openServices() {
  location.hash = '#prefs/services'
  render(<App />)
  await screen.findAllByTestId('service')
}

function item(name: string) {
  return screen.getAllByTestId('service').find((x) => x.getAttribute('data-name') === name)!
}

describe('设置里的服务', () => {
  it('一行一项：名字是服务类型，一句话说谁提供、谁在用；状态不是可用时状态放最前面；分区在设置的分区表里', async () => {
    await openServices()
    const nav = screen.getByRole('navigation', { name: '设置分区' })
    expect(within(nav).getByText('服务').closest('a')!.getAttribute('aria-current')).toBe('page')
    expect(screen.getByText('这台机器上的服务')).toBeTruthy()
    expect(screen.getByTestId('services-intro').textContent).toBe('应用之间互相借用的东西。装应用时需要哪种，自动接上。')
    expect(screen.getAllByTestId('service-summary').map((x) => x.textContent)).toEqual([
      '你登记的“我的自建邮箱” · memos 在用',
      '还差：Key · new-api 提供 · 还没有应用在用',
      '提供它的应用 other-api 停着 · other-api 提供 · hermes-agent 在用',
      '提供它的应用 old-api 已删 · old-api 提供 · 还没有应用在用',
    ])
    // 能用的没有状态字；不能用的状态字单独标出来
    expect(screen.getAllByTestId('service').map((x) => within(x).queryByTestId('service-status')?.textContent ?? ''))
      .toEqual(['', '还差：Key', '提供它的应用 other-api 停着', '提供它的应用 old-api 已删'])
    // 小圆点：能用是绿的，缺项、停着是黄的，已删是红的
    const dot = (name: string) => ['success', 'warning', 'danger'].find((t) => item(name).querySelector('.bg-' + t))
    expect(['我的自建邮箱', 'new-api', 'other-api', 'old-api'].map(dot)).toEqual(['success', 'warning', 'warning', 'danger'])
    // 名字是服务类型的中文名
    expect(within(item('我的自建邮箱')).getByText('邮箱（发信）')).toBeTruthy()
    expect(within(item('new-api')).getByText('大模型接口')).toBeTruthy()
    // 都收起；最下面不再有命令行那一条
    expect(screen.queryByTestId('service-values')).toBeNull()
    expect(screen.queryByText('acornfox services list')).toBeNull()
    // 页面上没有“服务类型”这类英文写法，也没有“变量”两个字
    expect(screen.queryByText(/llm_gateway|provides|uses/)).toBeNull()
    expect(screen.getByTestId('prefs-services').textContent).not.toContain('变量')
  })

  it('同一时间只展开一条：点开另一条，前一条收起；再点一下收起', async () => {
    await openServices()
    fireEvent.click(within(item('我的自建邮箱')).getByRole('button', { name: /我的自建邮箱/ }))
    expect(within(item('我的自建邮箱')).getByTestId('service-values')).toBeTruthy()
    fireEvent.click(within(item('other-api')).getByRole('button', { name: /other-api/ }))
    expect(within(item('我的自建邮箱')).queryByTestId('service-values')).toBeNull()
    const other = item('other-api')
    expect(within(other).getByTestId('service-users').textContent).toBe('谁在用hermes-agent')
    fireEvent.click(within(other).getByRole('button', { name: /other-api/ }))
    expect(screen.queryByTestId('service-values')).toBeNull()
  })

  it('展开后：名额有限时写还能接几个；应用提供的服务名字和应用名不一样时写出名字；还不能用时写原因', async () => {
    routes['GET /v1/services'] = () => ({ services: [
      { ...services[0], status: 'ok', max_consumers: 3, slots_left: 2 },
      { ...services[1], name: 'gw-main', status: 'not_ready', ready_say: '还没有上游', ready_detail: '第 1 步：应用返回 HTTP 503', used_by: [], dedicated_to: 'hermes-agent', slots_left: 0 },
    ] })
    await openServices()
    fireEvent.click(within(item('我的自建邮箱')).getByRole('button', { name: /我的自建邮箱/ }))
    expect(within(item('我的自建邮箱')).getByTestId('service-slots').textContent).toBe('还能不能接还能接 2 个（最多 3 个）')
    expect(within(item('我的自建邮箱')).queryByTestId('service-name')).toBeNull()
    const gw = item('gw-main')
    expect(within(gw).getByTestId('service-summary').textContent).toBe('还不能用 · new-api 提供 · 还没有应用在用')
    fireEvent.click(within(gw).getByRole('button', { name: /gw-main|new-api/ }))
    expect(within(gw).getByTestId('service-name').textContent).toBe('名字gw-main')
    expect(within(gw).getByTestId('service-not-ready').textContent).toContain('还不能用：还没有上游')
    expect(within(gw).getByTestId('service-not-ready').textContent).toContain('没过的那一步：第 1 步：应用返回 HTTP 503')
    expect(within(gw).getByTestId('service-slots').textContent).toBe('还能不能接被应用 hermes-agent 独占，别的应用接不了')
  })

  it('点开看各项的值：保密项只有后 4 位，没有“显示明文”', async () => {
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    const vals = within(mail).getByTestId('service-values')
    expect(within(vals).getByText('mail.example.com')).toBeTruthy()
    expect(within(vals).getByText('••••1a2b')).toBeTruthy()
    expect(within(mail).queryByRole('button', { name: /显示/ })).toBeNull()
  })

  it('还差一项：原样显示提示，点“填这一项”填好后只发这一项，显示服务端的说明', async () => {
    routes['PATCH /v1/services/new-api'] = () => ({ ...services[1], status: 'ok', notes: ['有 1 个应用在用它，重新应用设置后才生效：hermes-agent'] })
    await openServices()
    const s = item('new-api')
    // 收起时只有一句：还差哪一项；那句提示点开才看到
    expect(within(s).getByTestId('service-summary').textContent).toContain('还差：Key')
    expect(within(s).queryByText('登录 New API，在“令牌”里新建一把，贴进来')).toBeNull()
    fireEvent.click(within(s).getByRole('button', { name: /new-api/ }))
    expect(within(s).getByTestId('service-ask').textContent).toBe('登录 New API，在“令牌”里新建一把，贴进来')
    // 应用提供的：没有删除，说明删掉那个应用它才会消失
    expect(within(s).queryByRole('button', { name: '删除' })).toBeNull()
    expect(within(s).getByText('这条由应用 new-api 提供，删掉那个应用它才会消失')).toBeTruthy()
    fireEvent.click(within(s).getByRole('button', { name: '填Key' }))
    const edit = within(s).getByTestId('service-edit')
    const input = within(edit).getByLabelText('Key *')
    expect(input.getAttribute('type')).toBe('password')
    // 必填的保密项不能清掉，提示里也不说“留空表示清掉”
    expect(input.getAttribute('placeholder')).toBe('新的值（保存后只显示后 4 位）')
    fireEvent.click(within(edit).getByRole('button', { name: '保存' }))
    expect(within(edit).getByText('请填写Key')).toBeTruthy()
    expect(sent('PATCH', '/v1/services/new-api')).toEqual([])
    fireEvent.change(input, { target: { value: 'sk-abcd1234' } })
    fireEvent.click(within(edit).getByRole('button', { name: '保存' }))
    expect(await within(s).findByText('有 1 个应用在用它，重新应用设置后才生效：hermes-agent')).toBeTruthy()
    expect(sent('PATCH', '/v1/services/new-api')).toEqual([{ values: { api_key: 'sk-abcd1234' } }])
  })

  it('改自己登记的：必填项不能留空保存；保密项写明留空表示清掉，清掉前再确认一次', async () => {
    const path = '/v1/services/' + encodeURIComponent('我的自建邮箱')
    routes['PATCH ' + path] = () => services[0]
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    // 必填项留空：页面上拦下，不发请求
    fireEvent.click(within(mail).getByRole('button', { name: '改服务器地址' }))
    let edit = within(mail).getByTestId('service-edit')
    expect(within(edit).getByLabelText('服务器地址 *').getAttribute('placeholder')).toBe('新的值')
    fireEvent.click(within(edit).getByRole('button', { name: '保存' }))
    expect(within(edit).getByText('请填写服务器地址')).toBeTruthy()
    expect(within(edit).queryByTestId('service-clear-confirm')).toBeNull()
    fireEvent.click(within(edit).getByRole('button', { name: '取消' }))
    // 保密项：提示里写明留空表示清掉；留空保存先确认，确认前不发请求
    fireEvent.click(within(mail).getByRole('button', { name: '改密码' }))
    edit = within(mail).getByTestId('service-edit')
    const pw = within(edit).getByLabelText('密码')
    expect(pw.getAttribute('type')).toBe('password')
    expect(pw.getAttribute('placeholder')).toContain('留空表示清掉这一项')
    fireEvent.click(within(edit).getByRole('button', { name: '保存' }))
    const confirm = within(edit).getByTestId('service-clear-confirm')
    expect(confirm.textContent).toContain('保存后会清掉“密码”')
    expect(sent('PATCH', path)).toEqual([])
    // 不清：回到原样，还是没发
    fireEvent.click(within(confirm).getByRole('button', { name: '不清' }))
    expect(within(edit).queryByTestId('service-clear-confirm')).toBeNull()
    expect(sent('PATCH', path)).toEqual([])
    fireEvent.click(within(edit).getByRole('button', { name: '保存' }))
    fireEvent.click(within(within(edit).getByTestId('service-clear-confirm')).getByRole('button', { name: '确认清掉' }))
    await vi.waitFor(() => expect(sent('PATCH', path)).toEqual([{ values: { password: '' } }]))
  })

  it('试一下：结果原样显示', async () => {
    routes['POST /v1/services/' + encodeURIComponent('我的自建邮箱') + '/test'] = () => ({ ok: false, code: 'CAT-SVC-06', detail: '连不上 mail.example.com:587' })
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    fireEvent.click(within(mail).getByRole('button', { name: '试一下' }))
    const r = await within(mail).findByTestId('service-test')
    expect(r.textContent).toBe('没通 · CAT-SVC-06：连不上 mail.example.com:587')
    expect(r.getAttribute('data-result')).toBe('failed')
  })

  it('试一下：通了时写连的是哪；试不了（skipped）写“试不了”和原因，不说成没通', async () => {
    const path = 'POST /v1/services/' + encodeURIComponent('我的自建邮箱') + '/test'
    routes[path] = () => ({ ok: true, target: 'mail.example.com:587', detail: '连上了' })
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    fireEvent.click(within(mail).getByRole('button', { name: '试一下' }))
    let r = await within(mail).findByTestId('service-test')
    expect(r.textContent).toBe('通了（连的是 mail.example.com:587）：连上了')
    expect(r.getAttribute('data-result')).toBe('ok')
    routes[path] = () => ({ ok: false, skipped: true, detail: '服务器地址还没填，没法试' })
    fireEvent.click(within(mail).getByRole('button', { name: '试一下' }))
    await vi.waitFor(() => expect(within(mail).getByTestId('service-test').getAttribute('data-result')).toBe('skipped'))
    r = within(mail).getByTestId('service-test')
    expect(r.textContent).toBe('试不了：服务器地址还没填，没法试')
    expect(r.textContent).not.toContain('没通')
  })

  it('按每个使用它的应用单独发的项：单独列出、写明不在这里填，不显示成“没填”，也不给“填”', async () => {
    routes['GET /v1/services'] = () => ({ services: [{
      ...services[1], status: 'ok', status_label: '可用', missing: [],
      values: [{ key: 'base_url', label: '接口地址', value: 'http://new-api:3000/v1', secret: false, set: true, ask: '' }],
      issued_per_consumer: ['api_key'],
    }] })
    await openServices()
    const s = item('new-api')
    fireEvent.click(within(s).getByRole('button', { name: /new-api/ }))
    // 项的中文名取服务类型的定义（类型列表和服务列表分开读，等它到）
    await vi.waitFor(() => expect(within(s).getByTestId('service-issued').textContent).toBe('Key这几项按每个使用它的应用单独发，不在这里填'))
    expect(within(within(s).getByTestId('service-values')).queryByText('没填')).toBeNull()
    expect(within(s).queryByRole('button', { name: '填Key' })).toBeNull()
  })

  it('有服务的值读不出来（unreadable）：列表上方提示是哪几条，别的照常列出', async () => {
    routes['GET /v1/services'] = () => ({ services: [services[0]], unreadable: ['旧的上游', '坏掉的邮箱'] })
    await openServices()
    expect(screen.getByTestId('services-unreadable').textContent).toContain('这几条服务的内容读不出来：旧的上游、坏掉的邮箱')
    expect(screen.getAllByTestId('service').length).toBe(1)
  })

  it('只有读不出来的服务时：只提示读不出来，不说“还没有服务”', async () => {
    routes['GET /v1/services'] = () => ({ services: [], unreadable: ['旧的上游'] })
    location.hash = '#prefs/services'
    render(<App />)
    expect((await screen.findByTestId('services-unreadable')).textContent).toContain('旧的上游')
    expect(screen.queryByTestId('services-empty')).toBeNull()
  })

  it('删除：先确认；有应用在用被拒绝时列出是哪些应用', async () => {
    const path = '/v1/services/' + encodeURIComponent('我的自建邮箱')
    routes['DELETE ' + path] = () => json(409, { error: { code: 'CAT-SVC-04', message: '有应用在用这条服务，不能删', used_by: [{ app: 'memos', use: 'mail' }, { app: 'n8n', use: 'mail' }] } })
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    fireEvent.click(within(mail).getByRole('button', { name: '删除' }))
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false) // 先确认
    fireEvent.click(within(mail).getByRole('button', { name: '确认删除' }))
    const refused = await within(mail).findByTestId('service-refused')
    expect(refused.textContent).toContain('memos、n8n')
    expect(within(mail).getByRole('alert').textContent).toContain('CAT-SVC-04 · 有应用在用这条服务，不能删')
    expect(sent('DELETE', path).length).toBe(1)
  })

  it('登记一条：先选类型，按那种类型的定义生成表单（必填、保密、枚举、默认值、提示）', async () => {
    routes['POST /v1/services'] = () => json(201, { ...services[0], name: '公司邮箱' })
    await openServices()
    fireEvent.click(screen.getByRole('button', { name: '登记一条' }))
    const dialog = await screen.findByRole('dialog')
    const kinds = await within(dialog).findByRole('list', { name: '服务类型' })
    // 只说是什么服务，不提示去装哪个应用
    expect(within(kinds).getByText('给应用用的 OpenAI 兼容接口')).toBeTruthy()
    expect(within(dialog).queryByText(/也可以装|装好后自动出现|New API/)).toBeNull()
    fireEvent.click(within(kinds).getByText('邮箱（发信）'))
    const form = within(dialog).getByTestId('service-form')
    expect(within(form).getByText('服务器地址 *')).toBeTruthy()
    expect((within(form).getByLabelText('端口') as HTMLInputElement).value).toBe('587')
    expect(within(form).getByLabelText('密码').getAttribute('type')).toBe('password')
    const tls = within(form).getByLabelText('加密方式') as HTMLSelectElement
    expect(tls.value).toBe('starttls')
    expect([...tls.options].map((o) => o.value)).toEqual(['starttls', 'tls', 'none'])
    expect(within(form).getByText('一般用 starttls')).toBeTruthy()
    // 没填必填的：页面上拦下，不发请求
    fireEvent.click(within(form).getByRole('button', { name: '登记' }))
    expect(within(form).getByText('请填写服务器地址')).toBeTruthy()
    expect(within(form).getByText('请给这条服务起个名字')).toBeTruthy()
    expect(sent('POST', '/v1/services')).toEqual([])
    fireEvent.change(within(form).getByLabelText('名字 *'), { target: { value: '公司邮箱' } })
    fireEvent.change(within(form).getByLabelText('服务器地址 *'), { target: { value: 'smtp.example.com' } })
    fireEvent.change(within(form).getByLabelText('密码'), { target: { value: 'pw-9z8y' } })
    // 命令行写法不带保密项的值
    expect(within(form).getByText(/acornfox services add smtp/).textContent).not.toContain('pw-9z8y')
    fireEvent.click(within(form).getByRole('button', { name: '登记' }))
    await vi.waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(sent('POST', '/v1/services')).toEqual([{ name: '公司邮箱', kind: 'smtp', values: { host: 'smtp.example.com', port: '587', password: 'pw-9z8y', tls: 'starttls' } }])
  })

  it('另一种类型生成另一张表单（没为某种类型写专门的表单）', async () => {
    await openServices()
    fireEvent.click(screen.getByRole('button', { name: '登记一条' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(await within(dialog).findByText('演示令牌'))
    const form = within(dialog).getByTestId('service-form')
    expect(within(form).getByLabelText('地址 *')).toBeTruthy()
    expect(within(form).getByLabelText('令牌 *').getAttribute('type')).toBe('password')
    expect(within(form).queryByLabelText('服务器地址 *')).toBeNull()
  })

  it('旧服务端没有服务接口：说这个版本还没有这项功能，不白屏、不能登记', async () => {
    delete routes['GET /v1/services']
    delete routes['GET /v1/services/kinds']
    location.hash = '#prefs/services'
    render(<App />)
    expect((await screen.findByTestId('services-unsupported')).textContent).toContain('这个版本的服务端还没有这项功能')
    expect(screen.queryByRole('button', { name: '登记一条' })).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('一条都没有时说明会从哪里来', async () => {
    routes['GET /v1/services'] = () => ({ services: [] })
    location.hash = '#prefs/services'
    render(<App />)
    const empty = await screen.findByTestId('services-empty')
    expect(empty.textContent).toContain('还没有。装一个能提供服务的应用，它的服务会自己出现在这里；你手里已有的')
    expect(empty.textContent).toContain('也可以登记进来。')
    expect(screen.getByRole('button', { name: '登记一条' })).toBeTruthy()
  })
})

describe('设置里的服务：平台自动填的', () => {
  const pv = (name: string, label: string, value: string) => ({ name, label, standalone: '', cloud: '', user_can_set: true, value, available: !!value })
  const VARS = [
    pv('tz', '时区', 'Asia/Shanghai'),
    pv('region', '网络区域（cn 或 global）', 'global'),
    pv('mirror.pypi', 'Python 包下载源', 'https://pypi.org/simple/'),
    pv('mirror.npm', 'Node 包下载源', ''),
    pv('app.url', '这个应用的访问地址', 'http://127.0.0.1:8080'),
    pv('app.host', '这个应用访问地址里的主机（不带端口）', ''),
  ]

  it('列表最后固定一行：收起时一句话，项数只算列出来的；点开列出有值的（不列每个应用各算各的），名字去掉括号里的补充', async () => {
    routes['GET /v1/catalog/platform-vars'] = () => ({ vars: VARS })
    await openServices()
    const row = await screen.findByTestId('platform-auto')
    // 在最后一条服务的后面
    const all = screen.getByTestId('prefs-services').querySelectorAll('[data-testid="service"], [data-testid="platform-auto"]')
    expect(all[all.length - 1]).toBe(row)
    expect(within(row).getByText('平台自动填的')).toBeTruthy()
    expect(within(row).getByTestId('platform-auto-summary').textContent).toBe('时区、下载源等 3 项。装应用时平台替你填好，不用管')
    expect(within(row).queryByTestId('platform-auto-values')).toBeNull()
    // 先展开一条服务，再展开这一行：服务那条收起（同一时间只展开一项）
    fireEvent.click(within(item('我的自建邮箱')).getByRole('button', { name: /我的自建邮箱/ }))
    fireEvent.click(within(row).getByRole('button', { name: /平台自动填的/ }))
    expect(within(item('我的自建邮箱')).queryByTestId('service-values')).toBeNull()
    const vals = within(row).getByTestId('platform-auto-values')
    expect([...vals.querySelectorAll('dt')].map((x) => x.textContent)).toEqual(['时区', '网络区域', 'Python 包下载源'])
    expect([...vals.querySelectorAll('dd')].map((x) => x.textContent)).toEqual(['Asia/Shanghai', 'global', 'https://pypi.org/simple/'])
    expect(row.textContent).toContain('想给某个应用用别的值，装它的时候可以改。')
    // 不显示清单里的名字，也不写“变量”
    expect(row.textContent).not.toMatch(/app\.|mirror\.|tz|变量/)
  })

  it('还没有服务时也显示这一行', async () => {
    routes['GET /v1/services'] = () => ({ services: [] })
    routes['GET /v1/catalog/platform-vars'] = () => ({ vars: VARS })
    location.hash = '#prefs/services'
    render(<App />)
    await screen.findByTestId('services-empty')
    expect((await screen.findByTestId('platform-auto-summary')).textContent).toBe('时区、下载源等 3 项。装应用时平台替你填好，不用管')
  })

  it('旧服务端没有这个接口（404）：不显示这一行，也不报错', async () => {
    await openServices()
    await vi.waitFor(() => expect(calls.some((c) => c.url === '/v1/catalog/platform-vars')).toBe(true))
    expect(screen.queryByTestId('platform-auto')).toBeNull()
    expect(screen.queryByText('平台自动填的')).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
  })
})

describe('安装弹窗里需要的服务', () => {
  async function openInstall(plan: InstallPlan | ((url: string, init?: RequestInit) => unknown)) {
    routes['POST /v1/catalog/plan'] = typeof plan === 'function' ? plan : () => plan
    location.hash = '#store'
    render(<App />)
    const cards = await screen.findAllByTestId('catalog-app')
    fireEvent.click(within(cards[0]).getByRole('button', { name: '安装' }))
    const dialog = await screen.findByRole('dialog')
    return dialog
  }

  it('已接上：显示接的是哪条；计划请求和安装请求一样', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    const d = await openInstall(planOf(needOf({ status: 'matched', service: 'new-api' })))
    const need = await within(d).findByTestId('need')
    expect(need.getAttribute('data-status')).toBe('matched')
    expect(within(need).getByText('已接上')).toBeTruthy()
    expect(within(need).getByText('接的是“new-api”')).toBeTruthy()
    expect(within(need).getByText('大模型接口')).toBeTruthy()
    expect(sent('POST', '/v1/catalog/plan')[0]).toEqual({ app: 'hermes-agent' })
    expect(within(d).queryByText(/一并/)).toBeNull()
    fireEvent.click(within(d).getByRole('button', { name: '安装' }))
    await vi.waitFor(() => expect(sent('POST', '/v1/catalog/install')).toEqual([{ app: 'hermes-agent' }]))
  })

  it('不认识的状态：不把原词给用户看，写“状态未知”，不拦安装，请求里也不多带东西', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    const d = await openInstall(planOf(needOf({ status: 'will_install' }), { say: '' }))
    const need = await within(d).findByTestId('need')
    expect(within(need).getByText('状态未知，安装时由服务端检查')).toBeTruthy()
    expect(d.textContent).not.toContain('will_install')
    expect(within(d).queryByText(/一并|先装/)).toBeNull()
    expect(within(d).queryByTestId('plan-blocked')).toBeNull()
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(false)
    expect(within(d).queryByText(/--with-providers/)).toBeNull()
    fireEvent.click(within(d).getByRole('button', { name: '安装' }))
    await vi.waitFor(() => expect(sent('POST', '/v1/catalog/install')).toEqual([{ app: 'hermes-agent' }]))
  })

  it('可选的没有：显示没有时会怎样，照常能装', async () => {
    const d = await openInstall(planOf(needOf({ use: 'mail', kind: 'smtp', kind_label: '邮箱（发信）', required: false, status: 'optional_missing', without: '能用，但发不了通知邮件' })))
    const need = await within(d).findByTestId('need')
    expect(within(need).getByText('没有，先跳过')).toBeTruthy()
    expect(within(need).getByText('可选')).toBeTruthy()
    expect(within(need).getByText('能用，但发不了通知邮件')).toBeTruthy()
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(false)
  })

  it('必须的没有：显示原因和登记命令，安装按钮不可点；当场登记一条后重新读计划', async () => {
    let n = 0
    routes['POST /v1/services'] = () => json(201, { ...services[0], name: '我的上游', kind: 'llm_gateway' })
    const d = await openInstall(() => (++n === 1
      ? planOf(needOf({ status: 'required_missing', how: ['acornfox services add llm_gateway --name 名字 --set base_url=…'] }), {
        can_install: false, problems: [{ code: 'CAT-INST-31', message: 'hermes-agent 需要大模型接口，这台机器上没有' }],
      })
      : planOf(needOf({ status: 'matched', service: '我的上游' }))))
    const need = await within(d).findByTestId('need')
    expect(within(need).getByText('没有')).toBeTruthy()
    expect(within(need).getByText('这台机器上没有大模型接口。登记一条你已有的，再装：')).toBeTruthy()
    expect(within(need).getByText('acornfox services add llm_gateway --name 名字 --set base_url=…')).toBeTruthy()
    expect(within(d).getByTestId('plan-blocked').textContent).toContain('CAT-INST-31 · hermes-agent 需要大模型接口，这台机器上没有')
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(true)
    // 不提示先装哪个应用，也不提应用目录里谁能提供
    expect(within(d).queryByText(/先装|一并|应用目录里|New API/)).toBeNull()
    // 当场登记：表单照大模型接口的定义生成
    fireEvent.click(within(need).getByRole('button', { name: '登记一条你已有的大模型接口' }))
    const form = await within(d).findByTestId('service-form')
    fireEvent.change(within(form).getByLabelText('名字 *'), { target: { value: '我的上游' } })
    fireEvent.change(within(form).getByLabelText('接口地址 *'), { target: { value: 'https://api.deepseek.com/v1' } })
    fireEvent.change(within(form).getByLabelText('Key *'), { target: { value: 'sk-1234abcd' } })
    fireEvent.click(within(form).getByRole('button', { name: '登记' }))
    await within(d).findByText('接的是“我的上游”')
    expect(sent('POST', '/v1/services')).toEqual([{ name: '我的上游', kind: 'llm_gateway', values: { base_url: 'https://api.deepseek.com/v1', api_key: 'sk-1234abcd' } }])
    expect(sent('POST', '/v1/catalog/plan').length).toBe(2)
    expect(within(d).queryByTestId('plan-blocked')).toBeNull()
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(false)
  })

  it('有多条要选：没选不能装；选了写进安装请求的 use', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    const d = await openInstall((_u: string, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body || '{}'))
      return body.use?.llm ? planOf(needOf({ status: 'matched', service: body.use.llm })) : planOf(needOf({ status: 'choose', candidates: ['new-api', '我的上游'] }), { can_install: false, problems: [{ code: 'CAT-SVC-03', message: '有两条大模型接口' }] })
    })
    const need = await within(d).findByTestId('need')
    expect(within(need).getByText('要选一条')).toBeTruthy()
    expect(within(d).getByTestId('plan-blocked').textContent).toContain('先选好“大模型接口”用哪一条')
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(true)
    const sel = within(need).getByLabelText('大模型接口用哪一条') as HTMLSelectElement
    expect([...sel.options].map((o) => o.value)).toEqual(['', 'new-api', '我的上游'])
    fireEvent.change(sel, { target: { value: '我的上游' } })
    await vi.waitFor(() => expect(within(d).getByTestId('need').getAttribute('data-status')).toBe('matched'))
    expect(sent('POST', '/v1/catalog/plan')[1]).toEqual({ app: 'hermes-agent', use: { llm: '我的上游' } })
    // 选过以后还能换
    expect((within(d).getByLabelText('大模型接口用哪一条') as HTMLSelectElement).value).toBe('我的上游')
    expect(within(d).getByText(/--use 'llm=我的上游'/)).toBeTruthy()
    fireEvent.click(within(d).getByRole('button', { name: '安装' }))
    await vi.waitFor(() => expect(sent('POST', '/v1/catalog/install')).toEqual([{ app: 'hermes-agent', use: { llm: '我的上游' } }]))
  })

  it('必须的没有、又有多条要选：服务端的原因照样列出，不因为还有没选的被藏起来', async () => {
    const d = await openInstall(planOf(needOf({}), {
      can_install: false,
      problems: [{ code: 'CAT-INST-31', message: 'hermes-agent 需要邮箱（发信），这台机器上没有' }],
      apps: [{ id: 'hermes-agent', name: 'hermes-agent', needs: [
        needOf({ status: 'choose', candidates: ['new-api', '我的上游'] }),
        needOf({ use: 'mail', kind: 'smtp', kind_label: '邮箱（发信）', status: 'required_missing', how: ['acornfox services add smtp --name 名字 --set host=…'] }),
      ] }],
    }))
    const blocked = await within(d).findByTestId('plan-blocked')
    expect(within(blocked).getAllByRole('listitem').map((li) => li.textContent))
      .toEqual(['先选好“大模型接口”用哪一条', 'CAT-INST-31 · hermes-agent 需要邮箱（发信），这台机器上没有'])
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(true)
  })

  it('要选却一条候选都没有：说没有可选的、给当场登记的入口，不把安装按钮锁死', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    const d = await openInstall(planOf(needOf({ status: 'choose', candidates: [] })))
    const need = await within(d).findByTestId('need')
    expect(within(need).getByText('没有可选的服务，登记一条后再试')).toBeTruthy()
    expect(within(need).queryByRole('combobox')).toBeNull()
    expect(within(d).queryByTestId('plan-blocked')).toBeNull()
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(false)
    fireEvent.click(within(need).getByRole('button', { name: '登记一条你已有的大模型接口' }))
    expect(await within(d).findByTestId('service-form')).toBeTruthy()
  })

  it('配方里两个应用有同名的需要：各自一行，键带应用实例名，不串', async () => {
    const pick = (body: { use?: Record<string, string> }, app: string) => body.use?.[app + '.llm']
    routes['POST /v1/catalog/plan'] = (_u: string, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body || '{}'))
      const needFor = (app: string) => (pick(body, app) ? needOf({ status: 'matched', service: pick(body, app) }) : needOf({ status: 'choose', candidates: ['上游甲', '上游乙'] }))
      return planOf(needOf({}), { apps: [
        { id: 'new-api', name: 'new-api', needs: [needFor('new-api')] },
        { id: 'hermes-agent', name: 'hermes-agent', needs: [needFor('hermes-agent')] },
      ] })
    }
    location.hash = '#store'
    render(<App />)
    const cards = await screen.findAllByTestId('catalog-recipe')
    fireEvent.click(within(cards[1]).getByRole('button', { name: '开始' }))
    const d = await screen.findByRole('dialog')
    await vi.waitFor(() => expect(within(d).getAllByTestId('need').length).toBe(2))
    expect(within(d).getByTestId('plan-blocked').textContent).toContain('先选好 new-api 的“大模型接口”用哪一条')
    expect(within(d).getByTestId('plan-blocked').textContent).toContain('先选好 hermes-agent 的“大模型接口”用哪一条')
    fireEvent.change(within(d).getByLabelText('hermes-agent 的大模型接口用哪一条'), { target: { value: '上游乙' } })
    await vi.waitFor(() => expect(within(d).getAllByTestId('need').map((x) => x.getAttribute('data-status'))).toEqual(['choose', 'matched']))
    expect(sent('POST', '/v1/catalog/plan').at(-1)).toEqual({ recipe: 'hermes-with-new-api', use: { 'hermes-agent.llm': '上游乙' } })
    // new-api 那一行还是没选
    expect((within(d).getByLabelText('new-api 的大模型接口用哪一条') as HTMLSelectElement).value).toBe('')
    expect(within(d).getByTestId('plan-blocked').textContent).toContain('先选好 new-api 的“大模型接口”用哪一条')
    expect(within(d).getByTestId('plan-blocked').textContent).not.toContain('hermes-agent 的')
    expect(within(d).getByText(/--use 'hermes-agent\.llm=上游乙'/)).toBeTruthy()
  })

  it('点安装时用当前表单再读一次计划：这次说不能装就不装，并列出原因', async () => {
    let n = 0
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    const d = await openInstall(() => (++n === 1
      ? planOf(needOf({ status: 'matched', service: 'new-api' }))
      : planOf(needOf({ status: 'required_missing' }), { can_install: false, problems: [{ code: 'CAT-INST-31', message: 'hermes-agent 需要大模型接口，这台机器上没有' }] })))
    await within(d).findByTestId('need')
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(false)
    fireEvent.click(within(d).getByRole('button', { name: '安装' }))
    expect((await within(d).findByTestId('plan-blocked')).textContent).toContain('CAT-INST-31 · hermes-agent 需要大模型接口，这台机器上没有')
    expect(sent('POST', '/v1/catalog/plan').length).toBe(2)
    expect(sent('POST', '/v1/catalog/install')).toEqual([])
    expect(within(d).getByRole('button', { name: '安装' }).hasAttribute('disabled')).toBe(true)
  })

  it('点安装时读计划带的是当时填的设置；这次读失败不拦安装', async () => {
    let n = 0
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'new-api' }))
    routes['POST /v1/catalog/plan'] = () => (++n === 1 ? planOf(needOf({}), { apps: [{ id: 'new-api', name: 'new-api', needs: [] }] }) : json(500, { error: { code: 'internal', message: '坏了' } }))
    location.hash = '#store'
    render(<App />)
    const cards = await screen.findAllByTestId('catalog-app')
    fireEvent.click(within(cards[1]).getByRole('button', { name: '安装' }))
    const d = await screen.findByRole('dialog')
    await vi.waitFor(() => expect(n).toBe(1))
    fireEvent.change(within(d).getByLabelText('管理员用户名'), { target: { value: 'boss' } })
    fireEvent.click(within(d).getByRole('button', { name: '安装' }))
    await vi.waitFor(() => expect(sent('POST', '/v1/catalog/install')).toEqual([{ app: 'new-api', params: { admin_username: 'boss' } }]))
    expect(sent('POST', '/v1/catalog/plan')).toEqual([{ app: 'new-api' }, { app: 'new-api', params: { admin_username: 'boss' } }])
  })

  it('配方里后装的应用要接先装的应用提供的服务（pending_from）：写“装好 X 后接上它提供的 …”，算已接上，不拦安装', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task({ kind: 'recipe', target: 'hermes-with-new-api' }))
    routes['POST /v1/catalog/plan'] = () => planOf(needOf({}), { apps: [
      { id: 'new-api', name: 'new-api', needs: [], provides: [{ kind: 'llm_gateway', kind_label: '大模型接口' }] },
      { id: 'hermes-agent', name: 'hermes-agent', needs: [needOf({ status: 'matched', service: 'new-api', pending_from: 'new-api' })] },
    ] })
    location.hash = '#store'
    render(<App />)
    const cards = await screen.findAllByTestId('catalog-recipe')
    fireEvent.click(within(cards[1]).getByRole('button', { name: '开始' }))
    const d = await screen.findByRole('dialog')
    const need = await within(d).findByTestId('need')
    expect(within(need).getByTestId('need-pending').textContent).toBe('装好 new-api 后接上它提供的大模型接口')
    expect(within(need).getByText('装好后接上')).toBeTruthy()
    expect(within(need).queryByText(/接的是/)).toBeNull()
    expect(within(d).queryByTestId('plan-blocked')).toBeNull()
    expect(within(d).getByRole('button', { name: '开始' }).hasAttribute('disabled')).toBe(false)
    fireEvent.click(within(d).getByRole('button', { name: '开始' }))
    await vi.waitFor(() => expect(sent('POST', '/v1/catalog/install')).toEqual([{ recipe: 'hermes-with-new-api' }]))
  })

  it('旧服务端没有计划接口：不显示这一块，也没有别的服务选择，照常能装', async () => {
    routes['POST /v1/catalog/install'] = () => json(202, task({ target: 'hermes-agent' }))
    location.hash = '#store'
    render(<App />)
    const cards = await screen.findAllByTestId('catalog-app')
    fireEvent.click(within(cards[0]).getByRole('button', { name: '安装' }))
    const d = await screen.findByRole('dialog')
    await vi.waitFor(() => expect(calls.some((c) => c.url === '/v1/catalog/plan')).toBe(true))
    expect(within(d).queryByTestId('plan-needs')).toBeNull()
    expect(within(d).queryByTestId('plan-error')).toBeNull()
    expect(within(d).queryByRole('alert')).toBeNull()
    expect(within(d).queryByRole('tab')).toBeNull()
    fireEvent.click(within(d).getByRole('button', { name: '安装' }))
    await vi.waitFor(() => expect(sent('POST', '/v1/catalog/install')).toEqual([{ app: 'hermes-agent' }]))
  })
})

describe('应用概览里用到的服务', () => {
  it('不能用的在最上面提示，能点去看服务；不再有单独的“用到的服务”卡片', async () => {
    routes['GET /v1/apps/hermes-agent'] = () => app({
      name: 'hermes-agent',
      services: [
        { use: 'llm', kind: 'llm_gateway', kind_label: '大模型接口', service: 'other-api', status: 'provider_stopped', status_label: '提供它的应用停着' },
        { use: 'mail', kind: 'smtp', kind_label: '邮箱（发信）', service: '我的自建邮箱', status: 'ok', status_label: '可用' },
      ],
    })
    location.hash = '#app-hermes-agent'
    render(<App />)
    expect((await screen.findByTestId('services-alert')).textContent).toBe('大模型接口（other-api）：提供它的应用停着')
    // 原来单独的“用到的服务”卡片去掉了，它说的事在“连接”卡片里
    expect(screen.queryByTestId('app-services')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '去看服务' }))
    expect(location.hash).toBe('#prefs/services')
  })

  it('没有这个字段（旧服务端）时不显示', async () => {
    routes['GET /v1/apps/web'] = () => app()
    location.hash = '#app-web'
    render(<App />)
    await screen.findByTestId('ov-run')
    expect(screen.queryByTestId('app-services')).toBeNull()
    expect(screen.queryByTestId('services-alert')).toBeNull()
  })
})

describe('已删、数据还留着的使用方和删除后的说明', () => {
  const mailPath = '/v1/services/' + encodeURIComponent('我的自建邮箱')
  const withGone = { ...services[0], used_by: [{ app: 'memos', use: 'mail' }, { app: 'blog', use: 'mail', gone: true }] }
  const onlyGone = { ...services[0], used_by: [{ app: 'blog', use: 'mail', gone: true }] }

  it('谁在用：已删的写成“名字（已删除，数据还留着）”，不算在用', async () => {
    routes['GET /v1/services'] = () => ({ services: [withGone, { ...services[1], name: 'gw2', used_by: [{ app: 'blog', use: 'llm', gone: true }] }] })
    await openServices()
    expect(within(item('我的自建邮箱')).getByTestId('service-summary').textContent).toBe('你登记的“我的自建邮箱” · memos 在用；blog（已删除，数据还留着）')
    expect(within(item('gw2')).getByTestId('service-summary').textContent).toBe('还差：Key · new-api 提供 · 没有已装的应用在用；blog（已删除，数据还留着）')
    fireEvent.click(within(item('我的自建邮箱')).getByRole('button', { name: /我的自建邮箱/ }))
    expect(within(item('我的自建邮箱')).getByTestId('service-users').textContent).toBe('谁在用memos、blog（已删除，数据还留着）')
  })

  it('只有已删的应用接着它：确认时说它们以后装回来要重新接服务；200 带 notes 时删完在列表上方原样列出，关掉才消失', async () => {
    let gone = false
    routes['GET /v1/services'] = () => ({ services: gone ? [services[1]] : [onlyGone, services[1]] })
    const NOTES = ['blog 接着它的记录已经删了：以后装回来时要重新接服务', '另一句要告诉用户的话']
    routes['DELETE ' + mailPath] = () => { gone = true; return { name: '我的自建邮箱', notes: NOTES } }
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    fireEvent.click(within(mail).getByRole('button', { name: '删除' }))
    expect(within(mail).getByTestId('service-remove-confirm').textContent).toBe('删除“我的自建邮箱”？blog（已删除，数据还留着）接着它的记录会一起删掉，以后装回来时要重新接服务。')
    fireEvent.click(within(mail).getByRole('button', { name: '确认删除' }))
    const box = await screen.findByTestId('service-removed-notes')
    await vi.waitFor(() => expect(screen.getAllByTestId('service').length).toBe(1))
    expect(within(box).getByText('已删除“我的自建邮箱”')).toBeTruthy()
    expect(within(box).getAllByRole('listitem').map((li) => li.textContent)).toEqual(NOTES)
    expect(sent('DELETE', mailPath).length).toBe(1)
    fireEvent.click(within(box).getByRole('button', { name: '知道了' }))
    expect(screen.queryByTestId('service-removed-notes')).toBeNull()
  })

  it('两句一样的说明照样列两行、不报重复的 key；再删一条没有说明的服务时，上一次的说明框清掉', async () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    const removedNames: string[] = []
    const oldPath = '/v1/services/old-api'
    routes['GET /v1/services'] = () => ({ services: [onlyGone, services[3]].filter((s) => !removedNames.includes(s.name)) })
    const SAME = 'blog 接着它的记录已经删了：以后装回来时要重新接服务'
    routes['DELETE ' + mailPath] = () => { removedNames.push('我的自建邮箱'); return { name: '我的自建邮箱', notes: [SAME, SAME] } }
    routes['DELETE ' + oldPath] = () => { removedNames.push('old-api'); return new Response(null, { status: 204 }) }
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    fireEvent.click(within(mail).getByRole('button', { name: '删除' }))
    fireEvent.click(within(mail).getByRole('button', { name: '确认删除' }))
    const box = await screen.findByTestId('service-removed-notes')
    expect(within(box).getAllByRole('listitem').map((li) => li.textContent)).toEqual([SAME, SAME])
    expect(errors.mock.calls.some((c) => c.map(String).join(' ').includes('same key'))).toBe(false)
    await vi.waitFor(() => expect(screen.getAllByTestId('service').length).toBe(1))
    const old = item('old-api')
    fireEvent.click(within(old).getByRole('button', { name: /old-api/ }))
    fireEvent.click(within(old).getByRole('button', { name: '删除' }))
    fireEvent.click(within(old).getByRole('button', { name: '确认删除' }))
    await vi.waitFor(() => expect(sent('DELETE', oldPath).length).toBe(1))
    await vi.waitFor(() => expect(screen.queryByTestId('service-removed-notes')).toBeNull())
    errors.mockRestore()
  })

  it('改设置后的说明：两句一样的照样列两行、不报重复的 key', async () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    const SAME = '有 1 个应用在用它，重新应用设置后才生效：hermes-agent'
    routes['PATCH /v1/services/new-api'] = () => ({ ...services[1], status: 'ok', notes: [SAME, SAME] })
    await openServices()
    const s = item('new-api')
    fireEvent.click(within(s).getByRole('button', { name: /new-api/ }))
    fireEvent.click(within(s).getByRole('button', { name: '填Key' }))
    const edit = within(s).getByTestId('service-edit')
    fireEvent.change(within(edit).getByLabelText('Key *'), { target: { value: 'sk-abcd1234' } })
    fireEvent.click(within(edit).getByRole('button', { name: '保存' }))
    const notes = await within(s).findByTestId('service-notes')
    expect(within(notes).getAllByRole('listitem').map((li) => li.textContent)).toEqual([SAME, SAME])
    expect(errors.mock.calls.some((c) => c.map(String).join(' ').includes('same key'))).toBe(false)
    errors.mockRestore()
  })

  it('204 没有内容：删掉就好，不出空的说明', async () => {
    let gone = false
    routes['GET /v1/services'] = () => ({ services: gone ? [services[1]] : [services[0], services[1]] })
    routes['DELETE ' + mailPath] = () => { gone = true; return new Response(null, { status: 204 }) }
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    fireEvent.click(within(mail).getByRole('button', { name: '删除' }))
    expect(within(mail).getByTestId('service-remove-confirm').textContent).toBe('删除“我的自建邮箱”？现在 memos 在用它，要先把它们删掉或换成别的服务。')
    fireEvent.click(within(mail).getByRole('button', { name: '确认删除' }))
    await vi.waitFor(() => expect(screen.getAllByTestId('service').length).toBe(1))
    expect(screen.queryByTestId('service-removed-notes')).toBeNull()
  })

  it('删除被拒时列出的在用应用不含已删的', async () => {
    routes['GET /v1/services'] = () => ({ services: [withGone] })
    routes['DELETE ' + mailPath] = () => json(409, { error: { code: 'CAT-SVC-04', message: '还有应用在用 我的自建邮箱，不能删：memos（mail）', used_by: [{ app: 'memos', use: 'mail' }] } })
    await openServices()
    const mail = item('我的自建邮箱')
    fireEvent.click(within(mail).getByRole('button', { name: /我的自建邮箱/ }))
    fireEvent.click(within(mail).getByRole('button', { name: '删除' }))
    fireEvent.click(within(mail).getByRole('button', { name: '确认删除' }))
    expect((await within(mail).findByTestId('service-refused')).textContent).toBe('这些应用在用它：memos。先把它们删掉或换成别的服务，再删这一条。')
  })

  it('应用提供的服务：提供它的应用已删时能删；还在时不给删', async () => {
    await openServices()
    const old = item('old-api')
    fireEvent.click(within(old).getByRole('button', { name: /old-api/ }))
    expect(within(old).getByRole('button', { name: '删除' })).toBeTruthy()
    const gw = item('new-api')
    fireEvent.click(within(gw).getByRole('button', { name: /new-api/ }))
    expect(within(gw).queryByRole('button', { name: '删除' })).toBeNull()
    expect(within(gw).getByText('这条由应用 new-api 提供，删掉那个应用它才会消失')).toBeTruthy()
  })
})

describe('服务类型或项没有中文名', () => {
  const nameless = { kind: 'x_kind', label: '', summary: '', fields: [{ key: 'x_item', label: '', type: 'string', required: true }], has_test: false }
  const svc = {
    name: 'svc1', kind: 'x_kind', kind_label: '', source: 'user', app: '', status: 'incomplete', missing: ['x_item'],
    values: [{ key: 'x_item', label: '', value: '', secret: false, set: false, ask: '' }], used_by: [],
  }

  it('设置里：显示“（未命名的服务类型）”“（未命名的项）”，原名只在旁边的小字里', async () => {
    routes['GET /v1/services/kinds'] = () => ({ kinds: [...serviceKinds, nameless] })
    routes['GET /v1/services'] = () => ({ services: [svc] })
    await openServices()
    const s = item('svc1')
    const unnamed = within(s).getByTestId('unnamed')
    expect(unnamed.textContent).toBe('（未命名的服务类型）x_kind')
    expect(within(unnamed).getByText('x_kind').getAttribute('title')).toBe('程序里用的名字，排查时用')
    expect(within(s).getByTestId('service-status').textContent).toBe('还差：（未命名的项）')
    expect(within(s).getByTestId('service-status').getAttribute('title')).toBe('x_item')
    fireEvent.click(within(s).getByRole('button', { name: /svc1/ }))
    expect(within(within(s).getByTestId('service-values')).getAllByTestId('unnamed').map((x) => x.textContent)).toEqual(['（未命名的项）x_item'])
    // 登记时选类型、填表单也一样
    fireEvent.click(screen.getByRole('button', { name: '登记一条' }))
    const d = await screen.findByRole('dialog')
    const pick = await within(d).findByRole('list', { name: '服务类型' })
    fireEvent.click(within(pick).getByText('（未命名的服务类型）').closest('button')!)
    expect(within(d).getByRole('heading').textContent).toBe('登记：（未命名的服务类型）x_kind')
    expect(within(within(d).getByTestId('service-form')).getAllByTestId('unnamed').map((x) => x.textContent)).toEqual(['（未命名的项）x_item'])
  })

  it('安装弹窗、目录详情、应用概览：同样不直接显示程序用的名字', async () => {
    routes['POST /v1/catalog/plan'] = () => planOf(needOf({ status: 'required_missing', kind: 'x_kind', kind_label: '', how: ['acornfox services add x_kind --name 名字'] }))
    location.hash = '#store'
    const { unmount } = render(<App />)
    fireEvent.click(within((await screen.findAllByTestId('catalog-app'))[0]).getByRole('button', { name: '安装' }))
    const d = await screen.findByRole('dialog')
    const need = await within(d).findByTestId('need')
    expect(within(need).getByTestId('unnamed').textContent).toBe('（未命名的服务类型）x_kind')
    expect(within(need).getByText(/这台机器上没有（未命名的服务类型）。/)).toBeTruthy()
    unmount()

    routes['GET /v1/catalog/apps/hermes-agent'] = () => ({ ...hermesDetail, uses: [{ name: 'llm', kind: 'x_kind', use: 'llm', kind_label: '', required: true }], provides_detail: [{ kind: 'y_kind' }] })
    location.hash = '#item-hermes-agent'
    const r2 = render(<App />)
    await screen.findByRole('heading', { name: 'Hermes 智能体' })
    expect(within(screen.getByTestId('item-uses')).getByTestId('unnamed').textContent).toBe('（未命名的服务类型）x_kind')
    expect(within(screen.getByTestId('item-provides')).getByTestId('unnamed').textContent).toBe('（未命名的服务类型）y_kind')
    r2.unmount()

    routes['GET /v1/apps/hermes-agent'] = () => app({ name: 'hermes-agent', services: [{ use: 'llm', kind: 'x_kind', kind_label: '', service: '', status: 'not_connected', status_label: '没接上' }] })
    location.hash = '#app-hermes-agent'
    render(<App />)
    expect((await screen.findByTestId('services-alert')).textContent).toBe('（未命名的服务类型）：没接上')
  })

  it('还没有服务时说明里举的例子取自目录里定义的服务类型，不写死；取不到时不举例', async () => {
    routes['GET /v1/services'] = () => ({ services: [] })
    location.hash = '#prefs/services'
    const r = render(<App />)
    await screen.findByTestId('services-empty')
    await vi.waitFor(() => expect(screen.getByTestId('services-empty').textContent).toContain('你手里已有的（例如大模型接口、邮箱（发信））也可以登记进来'))
    r.unmount()
    routes['GET /v1/services/kinds'] = () => ({ kinds: [{ kind: 'a', label: '甲服务', fields: [] }] })
    const r2 = render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('services-empty').textContent).toContain('（例如甲服务）'))
    expect(screen.getByTestId('services-empty').textContent).not.toMatch(/大模型|邮箱/)
    r2.unmount()
    routes['GET /v1/services/kinds'] = () => ({ kinds: [] })
    render(<App />)
    await vi.waitFor(() => expect(screen.getByTestId('services-empty').textContent).toContain('你手里已有的也可以登记进来'))
    expect(screen.getByTestId('services-empty').textContent).not.toContain('例如')
  })
})
