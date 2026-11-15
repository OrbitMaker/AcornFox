import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from './api'
import {
  addCommand, appServiceStatus, applyPlan, buildServiceRequest, candidateNames, checkServiceForm, editPlaceholder, fetchPlan, fieldControl, fieldOptions,
  hasNeeds, issuedItems, listKinds, listServices, needText, newServiceValues, noCandidates, pendingFrom, pendingText, planBlock, planCli, planRows, refusedBy,
  UNNAMED_ITEM, UNNAMED_KIND, canRemove, checkFieldValue, deleteService, goneUsers, itemLabel, kindLabel, removeConfirmText, usedByApps,
  serviceStatus, shownValues, slotsText, sourceText, testText, unchosen, usedByText, useKey, waitingAsk,
} from './services'
import { needOf, planOf, serviceKinds, services } from '@/test/fixtures'

const smtp = serviceKinds.find((k) => k.kind === 'smtp')!
const llm = serviceKinds.find((k) => k.kind === 'llm_gateway')!

afterEach(() => vi.unstubAllGlobals())

describe('服务的状态和来源', () => {
  it('四种状态', () => {
    expect(serviceStatus(services[0])).toEqual({ text: '可用', tone: 'success' })
    expect(serviceStatus(services[1])).toEqual({ text: '还差：Key', tone: 'warning' })
    expect(serviceStatus(services[2])).toEqual({ text: '提供它的应用 other-api 停着', tone: 'warning' })
    expect(serviceStatus(services[3])).toEqual({ text: '提供它的应用 old-api 已删', tone: 'danger' })
    // 不认识的状态用服务端的说法
    expect(serviceStatus({ ...services[0], status: 'later', status_label: '以后的状态' })).toEqual({ text: '以后的状态', tone: 'muted' })
  })
  it('还不能用（就绪检查没过）：提醒，不是错误；没过的那一步放在 detail 里', () => {
    expect(serviceStatus({ ...services[1], status: 'not_ready', ready_say: '还没有上游', ready_detail: '第 1 步：应用返回 HTTP 503' })).toEqual({ text: '还不能用', tone: 'warning', detail: '第 1 步：应用返回 HTTP 503' })
    expect(serviceStatus({ ...services[1], status: 'not_ready' })).toEqual({ text: '还不能用', tone: 'warning' })
    expect(appServiceStatus({ use: 'db', kind: 'demo_sql', service: 'db1', status: 'not_ready' })).toEqual({ text: '还不能用', tone: 'warning' })
  })
  it('还能不能再接：被独占、接满、还能接几个、不限', () => {
    expect(slotsText({ ...services[0], dedicated_to: 'app-a', slots_left: 0 })).toEqual({ text: '被应用 app-a 独占，别的应用接不了', full: true })
    expect(slotsText({ ...services[0], max_consumers: 1, slots_left: 0 })).toEqual({ text: '已经接满（最多 1 个），别的应用接不了', full: true })
    expect(slotsText({ ...services[0], max_consumers: 3, slots_left: 2 })).toEqual({ text: '还能接 2 个（最多 3 个）', full: false })
    expect(slotsText({ ...services[0], dedicated_to: '', max_consumers: 0, slots_left: null })).toEqual({ text: '', full: false })
    expect(slotsText(services[0])).toEqual({ text: '', full: false })
  })
  it('来源、谁在用、要在应用里做一步的项', () => {
    expect(sourceText(services[0])).toBe('自己登记的')
    expect(sourceText(services[1])).toBe('由应用 new-api 提供')
    expect(usedByText(services[0])).toBe('memos 在用')
    expect(usedByText(services[1])).toBe('还没有应用在用')
    expect(waitingAsk(services[1]).map((v) => v.key)).toEqual(['api_key'])
    expect(waitingAsk(services[0])).toEqual([])
  })
  it('按使用方单独发的项：单独拿出来，不当成这条服务的值，也不算“要在应用里做一步”', () => {
    const s = { ...services[1], issued_per_consumer: ['api_key'] }
    expect(issuedItems(s)).toEqual(['api_key'])
    expect(shownValues(s).map((v) => v.key)).toEqual(['base_url'])
    expect(waitingAsk(s)).toEqual([])
    expect(issuedItems(services[0])).toEqual([])
    expect(issuedItems({ ...services[0], issued_per_consumer: null })).toEqual([])
  })
  it('试一下的结果：通了带连的是哪；没通带故障编号；试不了不算没通', () => {
    expect(testText({ ok: true, target: 'mail.example.com:587', detail: '连上了' })).toEqual({ kind: 'ok', tone: 'success', text: '通了（连的是 mail.example.com:587）：连上了' })
    expect(testText({ ok: true })).toEqual({ kind: 'ok', tone: 'success', text: '通了' })
    expect(testText({ ok: false, code: 'CAT-SVC-06', target: 'a:1', detail: '超时' })).toEqual({ kind: 'failed', tone: 'danger', text: '没通 · CAT-SVC-06（连的是 a:1）：超时' })
    expect(testText({ ok: false, skipped: true, detail: '这种服务没有试的办法' })).toEqual({ kind: 'skipped', tone: 'muted', text: '试不了：这种服务没有试的办法' })
    expect(testText({ ok: false, skipped: true })).toEqual({ kind: 'skipped', tone: 'muted', text: '试不了' })
  })
  it('应用用到的服务：没接上、停着、已删', () => {
    expect(appServiceStatus({ use: 'llm', kind: 'llm_gateway', service: 'new-api', status: 'ok' }).tone).toBe('success')
    expect(appServiceStatus({ use: 'llm', kind: 'llm_gateway', service: '', status: '' })).toEqual({ text: '没接上', tone: 'warning' })
    expect(appServiceStatus({ use: 'llm', kind: 'llm_gateway', service: 'x', status: 'provider_stopped', status_label: '提供它的应用停着' }).tone).toBe('warning')
    expect(appServiceStatus({ use: 'llm', kind: 'llm_gateway', service: 'x', status: 'provider_gone' }).tone).toBe('danger')
  })
})

describe('按服务类型定义生成的表单', () => {
  it('默认值、控件、选项都照定义', () => {
    expect(newServiceValues(smtp)).toEqual({ host: '', port: '587', username: '', password: '', tls: 'starttls' })
    expect(smtp.fields.map(fieldControl)).toEqual(['text', 'number', 'text', 'password', 'select'])
    expect(fieldOptions(smtp.fields[4])).toEqual([['starttls', 'starttls'], ['tls', 'tls'], ['none', 'none']])
    // 可以不填、没有默认值的枚举和是否多一个“不填”
    expect(fieldOptions({ key: 'b', label: '开', type: 'bool' })).toEqual([['', '不填'], ['true', '是'], ['false', '否']])
    expect(fieldOptions({ key: 'b', label: '开', type: 'bool', required: true })).toEqual([['true', '是'], ['false', '否']])
  })
  it('名字、必填、整数、地址、枚举', () => {
    const v = newServiceValues(smtp)
    expect(checkServiceForm(smtp, ' ', v)).toEqual([{ key: 'name', message: '请给这条服务起个名字' }, { key: 'host', message: '请填写服务器地址' }])
    expect(checkServiceForm(smtp, '邮箱', { ...v, host: 'mail.example.com', port: '58x' })).toEqual([{ key: 'port', message: '端口要填整数' }])
    expect(checkServiceForm(smtp, '邮箱', { ...v, host: 'h', tls: 'ssl' })[0].message).toBe('加密方式只能选 starttls、tls、none')
    expect(checkServiceForm(llm, '上游', { base_url: 'api.deepseek.com', api_key: 'k', model: '' })).toEqual([{ key: 'base_url', message: '接口地址要以 http:// 或 https:// 开头' }])
    expect(checkServiceForm(llm, '上游', { base_url: 'https://api.deepseek.com/v1', api_key: 'k', model: '' })).toEqual([])
  })
  it('请求只带填了的项，都是字符串；命令行不写保密项的值', () => {
    const v = { ...newServiceValues(smtp), host: ' mail.example.com ', password: 'pw-1a2b' }
    expect(buildServiceRequest(smtp, ' 我的邮箱 ', v)).toEqual({ name: '我的邮箱', kind: 'smtp', values: { host: 'mail.example.com', port: '587', password: 'pw-1a2b', tls: 'starttls' } })
    const cmd = addCommand(smtp, '我的邮箱', v)
    expect(cmd).toBe("acornfox services add smtp --name '我的邮箱' --set host=mail.example.com --set port=587 --set tls=starttls")
    expect(cmd).not.toContain('pw-1a2b')
  })
  it('改一项时的提示：能清掉的写明留空表示清掉，必填的不这么写', () => {
    expect(editPlaceholder(smtp.fields[3])).toBe('留空表示清掉这一项；填了保存后只显示后 4 位')
    expect(editPlaceholder(llm.fields[1])).toBe('新的值（保存后只显示后 4 位）')
    expect(editPlaceholder(smtp.fields[2])).toBe('留空表示清掉这一项')
    expect(editPlaceholder(smtp.fields[0])).toBe('新的值')
  })
})

describe('安装前的计划', () => {
  it('四种需要的说法；不认识的状态不显示原词，也不拦安装', () => {
    expect(['matched', 'optional_missing', 'required_missing', 'choose'].map((s) => needText(needOf({ status: s })).text))
      .toEqual(['已接上', '没有，先跳过', '没有', '要选一条'])
    for (const s of ['will_install', 'later_status', 'constructor', '']) {
      expect(needText(needOf({ status: s }))).toEqual({ text: '状态未知，安装时由服务端检查', tone: 'muted' })
    }
    expect(planBlock(planOf(needOf({ status: 'later_status' })), {})).toEqual([])
  })
  it('配方里由先装的应用装好后登记的服务（pending_from）：算已接上，说清是哪个应用、什么服务，不拦安装', () => {
    const n = needOf({ status: 'matched', service: 'new-api', pending_from: 'new-api' })
    expect(pendingFrom(n)).toBe('new-api')
    expect(pendingText(n)).toBe('装好 new-api 后接上它提供的大模型接口')
    expect(needText(n)).toEqual({ text: '装好后接上', tone: 'success' })
    expect(planBlock(planOf(n), {})).toEqual([])
    expect(unchosen(planOf(n), {})).toEqual([])
    // 别的状态不当成“装好后接上”
    expect(pendingFrom(needOf({ status: 'matched', service: 'x' }))).toBe('')
    expect(pendingFrom(needOf({ status: 'required_missing', pending_from: 'new-api' }))).toBe('')
  })
  it('use 的键：只装一个应用是需要的名字；计划里不止一个应用时一律带应用实例名', () => {
    expect(useKey(false, 'hermes-agent', 'llm')).toBe('llm')
    expect(useKey(true, 'hermes-agent', 'llm')).toBe('hermes-agent.llm')
    expect(planRows(planOf(needOf({})))[0].key).toBe('llm')
    const two = planOf(needOf({}), { apps: [
      { id: 'a', name: 'bot-a', needs: [needOf({ status: 'choose', candidates: ['x', 'y'] })] },
      { id: 'b', name: 'bot-b', needs: [needOf({ status: 'choose', candidates: ['x', 'y'] })] },
    ] })
    expect(planRows(two).map((r) => r.key)).toEqual(['bot-a.llm', 'bot-b.llm'])
    // 选了一个应用的，另一个应用的同名需要还算没选
    expect(planBlock(two, { 'bot-a.llm': 'x' })).toEqual(['先选好 bot-b 的“大模型接口”用哪一条'])
  })
  it('候选两种写法都认，去重', () => {
    expect(candidateNames(needOf({ candidates: ['a', { name: 'b' }, 'a'] }))).toEqual(['a', 'b'])
    expect(candidateNames(needOf({ candidates: null }))).toEqual([])
  })
  it('没有 needs 不显示；请求只带选了的服务，不带别的', () => {
    expect(hasNeeds(null)).toBe(false)
    expect(hasNeeds(planOf(needOf({}), { apps: [{ id: 'x', name: 'x', needs: [] }] }))).toBe(false)
    expect(applyPlan({ app: 'hermes-agent' }, {})).toEqual({ app: 'hermes-agent' })
    expect(applyPlan({ app: 'hermes-agent' }, { llm: '我的上游', mail: '' })).toEqual({ app: 'hermes-agent', use: { llm: '我的上游' } })
    expect(planCli({ llm: '我的上游', mail: '' })).toEqual(["--use 'llm=我的上游'"])
    expect(planCli({})).toEqual([])
  })
  it('不能装的原因：没选的先说，服务端的原话一律列出', () => {
    const choose = planOf(needOf({ status: 'choose', candidates: ['a', 'b'] }), { can_install: false, problems: [{ code: 'CAT-SVC-03', message: '有两条，要选一条' }] })
    expect(planBlock(choose, {})).toEqual(['先选好“大模型接口”用哪一条', 'CAT-SVC-03 · 有两条，要选一条'])
    expect(planBlock(choose, { llm: 'a' })).toEqual(['CAT-SVC-03 · 有两条，要选一条'])
    expect(planBlock(planOf(needOf({ status: 'required_missing' }), { can_install: false, problems: [] }), {})).toEqual(['现在还不能装'])
    expect(planBlock(planOf(needOf({})), {})).toEqual([])
    expect(planBlock(null, {})).toEqual([])
  })
  it('必须的没有、又有多条要选：服务端的原因不因为还有没选的被藏起来', () => {
    const p = planOf(needOf({}), {
      can_install: false,
      problems: [{ code: 'CAT-INST-31', message: 'hermes-agent 需要邮箱（发信），这台机器上没有' }],
      apps: [{ id: 'hermes-agent', name: 'hermes-agent', needs: [
        needOf({ status: 'choose', candidates: ['a', 'b'] }),
        needOf({ use: 'mail', kind: 'smtp', kind_label: '邮箱（发信）', status: 'required_missing' }),
      ] }],
    })
    expect(planBlock(p, {})).toEqual(['先选好“大模型接口”用哪一条', 'CAT-INST-31 · hermes-agent 需要邮箱（发信），这台机器上没有'])
  })
  it('要选却一条候选都没有：不算“没选”，不拦安装按钮（服务端说不能装时照样列出原因）', () => {
    const n = needOf({ status: 'choose', candidates: [] })
    expect(noCandidates(n)).toBe(true)
    expect(noCandidates(needOf({ status: 'choose', candidates: ['a'] }))).toBe(false)
    expect(noCandidates(needOf({ status: 'matched', candidates: [] }))).toBe(false)
    expect(planBlock(planOf(n), {})).toEqual([])
    expect(planBlock(planOf(n, { can_install: false, problems: [{ code: 'CAT-SVC-03', message: '没有能选的' }] }), {})).toEqual(['CAT-SVC-03 · 没有能选的'])
  })
})

describe('接口', () => {
  const respond = (status: number, body: unknown) => vi.stubGlobal('fetch', vi.fn(async () => new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })))
  it('旧服务端没有接口（404、405）：读的函数返回 null', async () => {
    respond(404, '404 page not found')
    expect(await listServices()).toBeNull()
    expect(await listKinds()).toBeNull()
    expect(await fetchPlan({ app: 'x' })).toBeNull()
    respond(405, '')
    expect(await listServices()).toBeNull()
  })
  it('别的错误照常抛出', async () => {
    respond(500, { error: { code: 'internal', message: '坏了' } })
    await expect(listServices()).rejects.toThrow('坏了')
  })
  it('有接口：拿到列表和读不出来的那几条', async () => {
    respond(200, { services })
    expect((await listServices())!.services.length).toBe(4)
    expect((await listServices())!.unreadable).toEqual([])
    respond(200, { services: null })
    expect(await listServices()).toEqual({ services: [], unreadable: [] })
    respond(200, { services: [services[0]], unreadable: ['坏的', '', 3] })
    expect(await listServices()).toEqual({ services: [services[0]], unreadable: ['坏的'] })
  })
  it('删除被拒：错误里带的 used_by 优先，没带时用已知的', () => {
    const e = new ApiError(409, 'CAT-SVC-04', '有应用在用它', { raw: { used_by: [{ app: 'memos', use: 'mail' }, { app: 'n8n', use: 'mail' }, 'memos'] } })
    expect(refusedBy(e)).toEqual(['memos', 'n8n'])
    expect(refusedBy(new ApiError(409, 'CAT-SVC-04', 'x'), [{ app: 'hermes-agent', use: 'llm' }])).toEqual(['hermes-agent'])
  })
})

describe('已删、数据还留着的使用方（used_by 里 gone）', () => {
  const s = { ...services[0], used_by: [{ app: 'memos', use: 'mail' }, { app: 'blog', use: 'mail', gone: true }] }
  const onlyGone = { ...services[0], used_by: [{ app: 'blog', use: 'mail', gone: true }, { app: 'wiki', use: 'mail', gone: true }] }
  it('写成“名字（已删除，数据还留着）”，不算在用', () => {
    expect(usedByApps(s)).toEqual(['memos'])
    expect(goneUsers(s)).toEqual(['blog'])
    expect(usedByText(s)).toBe('memos 在用；blog（已删除，数据还留着）')
    expect(usedByApps(onlyGone)).toEqual([])
    expect(usedByText(onlyGone)).toBe('没有已装的应用在用；blog（已删除，数据还留着）、wiki（已删除，数据还留着）')
    expect(goneUsers({ ...services[0], used_by: null })).toEqual([])
  })
  it('删除前的确认按谁在用来说', () => {
    expect(removeConfirmText(s)).toBe('删除“我的自建邮箱”？现在 memos 在用它，要先把它们删掉或换成别的服务。')
    expect(removeConfirmText(onlyGone)).toBe('删除“我的自建邮箱”？blog（已删除，数据还留着）、wiki（已删除，数据还留着）接着它的记录会一起删掉，以后装回来时要重新接服务。')
    expect(removeConfirmText({ ...services[0], used_by: [] })).toBe('删除“我的自建邮箱”？')
  })
  it('删除被拒时列出的应用不算已删的', () => {
    const e = new ApiError(409, 'CAT-SVC-04', '有应用在用它', { raw: { used_by: [{ app: 'memos', use: 'mail' }, { app: 'blog', use: 'mail', gone: true }] } })
    expect(refusedBy(e)).toEqual(['memos'])
    expect(refusedBy(new ApiError(409, 'CAT-SVC-04', 'x'), s.used_by)).toEqual(['memos'])
  })
  it('能不能在页面上删：自己登记的能；应用提供的只在提供它的应用已删时能', () => {
    expect(canRemove(services[0])).toBe(true)
    expect(canRemove(services[1])).toBe(false)
    expect(canRemove(services[2])).toBe(false)
    expect(canRemove(services[3])).toBe(true)
  })
})

describe('删除服务的应答', () => {
  const reply = (status: number, body: string | null) => vi.stubGlobal('fetch', vi.fn(async () => new Response(body, { status, headers: { 'Content-Type': 'application/json' } })))
  it('204：没有要说的话', async () => {
    reply(204, null)
    expect(await deleteService('我的自建邮箱')).toEqual({ name: '我的自建邮箱', notes: [] })
  })
  it('200 带 notes：原样拿出来，不吞掉；不是字符串的、空的丢掉', async () => {
    reply(200, JSON.stringify({ name: 'old-api', notes: ['blog 以后装回来时要重新接服务', '', 3] }))
    expect(await deleteService('old-api')).toEqual({ name: 'old-api', notes: ['blog 以后装回来时要重新接服务'] })
    reply(200, JSON.stringify({ name: 'old-api', notes: null }))
    expect(await deleteService('old-api')).toEqual({ name: 'old-api', notes: [] })
  })
})

describe('服务类型或项没有中文名', () => {
  it('不把程序用的名字直接给用户看', () => {
    expect(UNNAMED_KIND).toBe('（未命名的服务类型）')
    expect(UNNAMED_ITEM).toBe('（未命名的项）')
    expect(kindLabel('大模型接口')).toBe('大模型接口')
    expect(kindLabel('')).toBe(UNNAMED_KIND)
    expect(kindLabel(undefined)).toBe(UNNAMED_KIND)
    expect(itemLabel(' ')).toBe(UNNAMED_ITEM)
    expect(itemLabel('密码')).toBe('密码')
  })
  it('状态里“还差”的项、计划里的说法、表单校验的提示都用“未命名”，原名只放在给排查看的 detail 里', () => {
    const s = { ...services[1], values: [{ key: 'api_key', label: '', value: '', secret: true, set: false }], missing: ['api_key', 'extra'] }
    expect(serviceStatus(s)).toEqual({ text: '还差：' + UNNAMED_ITEM, tone: 'warning', detail: 'api_key、extra' })
    expect(pendingText(needOf({ status: 'matched', pending_from: 'gw', kind_label: '' }))).toBe('装好 gw 后接上它提供的' + UNNAMED_KIND)
    expect(planBlock(planOf(needOf({ status: 'choose', kind_label: '', candidates: ['a', 'b'] })), {})).toEqual(['先选好“' + UNNAMED_KIND + '”用哪一条'])
    expect(checkFieldValue({ key: 'port', label: '', type: 'int' }, 'x')).toBe(UNNAMED_ITEM + '要填整数')
    expect(checkFieldValue({ key: 'host', label: '', type: 'string', required: true }, '')).toBe('请填写' + UNNAMED_ITEM)
  })
})
