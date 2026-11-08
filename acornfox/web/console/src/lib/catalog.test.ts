import { describe, expect, it } from 'vitest'
import {
  abbr, buildRequest, categoryTabs, changedPlatform, checkForm, cliCommand, defaultModel, faultForAI, filterApps, freeMB, groupSteps, installingApps, memoryCheck,
  newInstallForm, recipeMemory, taskProgress, appKey, catalogAppOf, disconnectedSources, filterSource, findApp, sourceId, sourceTabs,
} from './catalog'
import { catalog, hermesDetail, newApiDetail, resources, task } from '@/test/fixtures'
import type { AppDetail, PlatformVarView } from './types'

describe('分类和搜索', () => {
  it('只列目录里有的分类，两个 AI 子类归到 AI', () => {
    expect(categoryTabs(catalog.apps)).toEqual([['all', '全部'], ['ai', 'AI']])
  })
  it('按名称、关键词搜索', () => {
    expect(filterApps(catalog.apps, 'all', '爱马仕').map((a) => a.id)).toEqual(['hermes-agent'])
    expect(filterApps(catalog.apps, 'all', 'openai').map((a) => a.id)).toEqual(['new-api'])
    expect(filterApps(catalog.apps, 'ai', '').length).toBe(2)
    expect(filterApps(catalog.apps, 'db', '').length).toBe(0)
  })
  it('缩写', () => {
    expect(abbr('New API')).toBe('NA')
    expect(abbr('Hermes 智能体')).toBe('He')
    expect(abbr('n8n')).toBe('N8')
  })
})

describe('内存检查', () => {
  it('剩余 = 整机给应用的上限 − 已分配', () => {
    expect(freeMB(resources)).toBe(5120)
    expect(freeMB({ ...resources, memory: { ...resources.memory, cap_mb: 0 } })).toBe(7168)
    expect(freeMB(null)).toBeNull()
  })
  it('够、只够最少值、不够三种', () => {
    expect(memoryCheck(2048, 1024, 5120)).toEqual({ tone: 'ok', text: '要 2 GB · 剩 5 GB' })
    expect(memoryCheck(2048, 1024, 1536).tone).toBe('warn')
    expect(memoryCheck(2048, 1024, 512)).toEqual({ tone: 'low', text: '要 2 GB · 剩 512 MB，内存不够' })
    expect(memoryCheck(512, 256, null).text).toBe('要 512 MB')
  })
  it('配方：全部新装用配方写的合计，有复用的按实际新装的算', () => {
    const r = catalog.recipes[1]
    expect(recipeMemory(r, catalog.apps).need).toBe(2560)
    const withGateway = catalog.apps.map((a) => (a.id === 'new-api' ? { ...a, installed: ['new-api'] } : a))
    expect(recipeMemory(r, withGateway)).toEqual({ need: 2048, min: 1024 })
  })
})

describe('安装表单', () => {
  it('New API：什么都不填，请求只有应用名，命令是 acornfox install new-api', () => {
    const f = newInstallForm('app', 'new-api', [newApiDetail], null)
    expect(f.llm).toEqual([])
    expect(f.plain.map((p) => p.key)).toEqual(['admin_username', 'admin_password'])
    expect(checkForm(f)).toEqual([])
    expect(buildRequest(f)).toEqual({ app: 'new-api' })
    expect(cliCommand(f)).toBe('acornfox install new-api')
  })
  it('填了的参数进请求；命令行里不写出密码', () => {
    const f = newInstallForm('app', 'new-api', [newApiDetail], null)
    f.plain[0].value = 'root'
    f.plain[1].value = 'secret-pass-1'
    expect(buildRequest(f)).toEqual({ app: 'new-api', params: { admin_username: 'root', admin_password: 'secret-pass-1' } })
    expect(cliCommand(f)).toBe('acornfox install new-api --set admin_username=root --set admin_password=<密码>')
  })
  it('格式不对和密码太短在页面上就拦下', () => {
    const f = newInstallForm('app', 'new-api', [newApiDetail], null)
    f.plain[0].value = 'bad name!'
    f.plain[1].value = 'short'
    expect(checkForm(f).map((p) => p.key)).toEqual(['admin_username', 'admin_password'])
  })
  it('Hermes：本机有 New API 时默认用它', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermesDetail(true)], null)
    expect(f.llm[0].source).toBe('local_gateway')
    expect(buildRequest(f)).toEqual({ app: 'hermes-agent', params: { llm: { source: 'local_gateway', app: 'new-api' } } })
    expect(cliCommand(f)).toBe('acornfox install hermes-agent --llm local')
  })
  it('Hermes：没有 New API 时默认自己的 Key，三项都要填，Key 不进命令行', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermesDetail(false)], null)
    expect(f.llm[0].source).toBe('own')
    expect(checkForm(f).length).toBe(3)
    f.llm[0] = { ...f.llm[0], baseURL: 'https://api.deepseek.com/v1', apiKey: 'sk-abc', model: 'deepseek-chat' }
    expect(checkForm(f)).toEqual([])
    expect(buildRequest(f).params).toEqual({ llm: { source: 'own', base_url: 'https://api.deepseek.com/v1', api_key: 'sk-abc', model: 'deepseek-chat' } })
    const cmd = cliCommand(f)
    expect(cmd).toContain('--llm own')
    expect(cmd).toContain('--set llm.api_key=<你的 Key>')
    expect(cmd).not.toContain('sk-abc')
  })
  it('平台 Key 和本机没网关时不能提交', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermesDetail(false)], null)
    f.llm[0].source = 'platform'
    expect(checkForm(f)[0].message).toContain('即将开放')
    f.llm[0].source = 'local_gateway'
    expect(checkForm(f)[0].message).toBe('先装 New API')
  })
  it('配方 Hermes + New API：Hermes 的模型固定连本机 New API，参数带应用前缀', () => {
    const r = catalog.recipes[1]
    const f = newInstallForm('recipe', r.id, [newApiDetail, hermesDetail(false)], r)
    expect(f.llm[0]).toMatchObject({ key: 'hermes-agent.llm', fixed: 'new-api' })
    expect(f.plain.map((p) => p.key)).toEqual(['new-api.admin_username', 'new-api.admin_password'])
    expect(checkForm(f)).toEqual([])
    expect(buildRequest(f)).toEqual({ recipe: 'hermes-with-new-api' })
    expect(cliCommand(f)).toBe('acornfox install --recipe hermes-with-new-api')
    f.llm[0].model = 'qwen-max'
    expect(buildRequest(f).params).toEqual({ 'hermes-agent.llm': { source: 'local_gateway', model: 'qwen-max' } })
    expect(cliCommand(f)).toBe('acornfox install --recipe hermes-with-new-api --llm local --set hermes-agent.llm.model=qwen-max')
  })
  it('配方里已经装过的应用不再填参数', () => {
    const r = catalog.recipes[1]
    const f = newInstallForm('recipe', r.id, [{ ...newApiDetail, installed: ['new-api'] }, hermesDetail(true)], r)
    expect(f.plain).toEqual([])
  })
})

describe('安装任务', () => {
  it('按应用分组、数进度', () => {
    const t = task()
    expect(groupSteps(t).map((g) => g.app)).toEqual(['new-api'])
    expect(taskProgress(t)).toEqual({ done: 1, total: 3 })
  })
  it('失败时给 AI 的诊断带故障编号、失败的步骤和查询命令', () => {
    const t = task({
      status: 'failed',
      steps: [{ app: 'new-api', name: 'image', label: '下载镜像', status: 'failed', message: '所有来源都失败' }],
      fault: { code: 'CAT-INST-12', message: '镜像从所有来源都没拿到', hint: '稍后重试' },
    })
    const s = faultForAI(t)
    expect(s).toContain('acornfox install status t123')
    expect(s).toContain('"code": "CAT-INST-12"')
    expect(s).toContain('new-api · 下载镜像：所有来源都失败')
  })
  it('正在装的应用名（复用的不算）', () => {
    const t = task({ apps: [{ app: 'new-api', catalog_id: 'new-api', version: 'x', reused: true, created: false }, { app: 'hermes-agent', catalog_id: 'hermes-agent', version: 'y', reused: false, created: true }] })
    expect([...installingApps([t, task({ id: 'done', status: 'succeeded' })])]).toEqual(['hermes-agent'])
  })
})

describe('私有目录', () => {
  it('公共、私有同 id 各算一份：按来源分标签、按来源找应用、安装请求和命令行带目录编号', () => {
    const priv = { id: 'cat-demo', name: '演示' }
    const c = {
      ...catalog,
      apps: [
        ...catalog.apps.map((a) => ({ ...a, source: { id: 'public', name: '公共目录' } })),
        ...catalog.apps.map((a) => ({ ...a, name: a.name + '（定制）', source: priv })),
      ],
      recipes: [...catalog.recipes, ...catalog.recipes.map((r) => ({ ...r, source: priv }))],
      sources: [
        { id: 'public', kind: 'public', name: '公共目录', serial: 1, apps: 2, recipes: 3, disconnected: false },
        { id: 'cat-demo', kind: 'private', name: '演示', serial: 3, apps: 2, recipes: 3, disconnected: false },
        { id: 'cat-old', kind: 'private', name: '旧客户', serial: 0, apps: 0, recipes: 0, disconnected: true },
      ],
    }
    expect(sourceTabs(c)).toEqual([['all', '全部'], ['public', '公共目录'], ['cat-demo', '演示']])
    expect(sourceTabs(catalog)).toEqual([]) // 只有公共目录时不分来源
    expect(new Set(c.apps.map(appKey)).size).toBe(4)
    expect(filterSource(c.apps, 'cat-demo').every((a) => sourceId(a) === 'cat-demo')).toBe(true)
    expect(filterSource(c.recipes, 'public').length).toBe(3) // 没写来源的当作公共目录
    expect(findApp(c, 'new-api', 'cat-demo')?.name).toBe('New API（定制）')
    expect(findApp(c, 'new-api')?.name).toBe('New API')
    expect(disconnectedSources(c).map((x) => x.id)).toEqual(['cat-old'])
    const f = newInstallForm('app', 'new-api', [newApiDetail], null, 'cat-demo')
    expect(buildRequest(f)).toEqual({ app: 'new-api', catalog: 'cat-demo' })
    expect(cliCommand(f)).toBe('acornfox install new-api --catalog cat-demo')
    expect(buildRequest(newInstallForm('app', 'new-api', [newApiDetail], null)).catalog).toBeUndefined()
    expect(catalogAppOf(c, { catalog_id: 'new-api', catalog_origin: 'cat-demo' })?.name).toBe('New API（定制）')
    expect(catalogAppOf(c, { catalog_id: 'new-api' })?.name).toBe('New API')
    expect(catalogAppOf(c, {})).toBeUndefined()
    // 已断开的私有目录装的应用：找不到就不借公共版的图标。
    expect(catalogAppOf(c, { catalog_id: 'new-api', catalog_origin: 'cat-old' })).toBeUndefined()
  })
})

describe('平台变量', () => {
  const pv = (name: string, value: string, canSet: boolean): PlatformVarView => ({
    name, label: name, standalone: '本机', cloud: '集群', user_can_set: canSet, value, available: true,
  })
  const withPlatform = (d: AppDetail, vars: PlatformVarView[]): AppDetail => ({ ...d, platform: vars })
  const hermes = () => withPlatform(hermesDetail(true), [
    pv('llm.model.chat', 'qwen-plus', true), pv('mirror.pypi', 'https://mirrors.aliyun.com/pypi/simple/', true), pv('tz', 'Asia/Shanghai', true),
  ])
  it('不改就不发，命令也不多写；默认模型取平台的', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermes()], null)
    expect(f.platform.map((p) => p.view.name)).toEqual(['llm.model.chat', 'mirror.pypi', 'tz'])
    expect(changedPlatform(f)).toEqual({})
    expect(buildRequest(f).platform).toBeUndefined()
    expect(cliCommand(f)).toBe('acornfox install hermes-agent --llm local')
    expect(defaultModel(f)).toBe('qwen-plus')
  })
  it('改了的进请求和命令；改回原值就不发', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermes()], null)
    f.platform[2].value = 'Asia/Tokyo'
    expect(buildRequest(f).platform).toEqual({ tz: 'Asia/Tokyo' })
    expect(cliCommand(f)).toBe('acornfox install hermes-agent --llm local --set platform.tz=Asia/Tokyo')
    f.platform[2].value = 'Asia/Shanghai'
    expect(buildRequest(f).platform).toBeUndefined()
    f.platform[0].value = 'glm-4'
    expect(defaultModel(f)).toBe('glm-4') // “留空用 …”跟着改
    f.platform[0].value = ' '
    expect(defaultModel(f)).toBe('qwen-plus')
  })
  it('格式不对在页面上拦下；不能改的不查、不发', () => {
    const f = newInstallForm('app', 'hermes-agent', [withPlatform(hermesDetail(true), [pv('mirror.pypi', 'https://a/', true), pv('tz', 'UTC', true), pv('region', 'cn', false)])], null)
    f.platform[0].value = 'ftp://x'
    f.platform[1].value = '../etc'
    f.platform[2].value = 'global'
    expect(checkForm(f).map((p) => p.key)).toEqual(['platform.mirror.pypi', 'platform.tz'])
    f.platform[0].value = ''
    expect(checkForm(f)[0].message).toContain('不能为空')
    f.platform[0].value = 'https://a/'
    f.platform[1].value = 'UTC'
    expect(buildRequest(f).platform).toBeUndefined()
  })
  it('配方：几个应用同名的只列一次，复用的应用不列', () => {
    const r = catalog.recipes[1]
    const na = withPlatform(newApiDetail, [pv('tz', 'Asia/Shanghai', true)])
    const f = newInstallForm('recipe', r.id, [na, hermes()], r)
    expect(f.platform.map((p) => p.view.name)).toEqual(['tz', 'llm.model.chat', 'mirror.pypi'])
    const reused = newInstallForm('recipe', r.id, [{ ...na, installed: ['new-api'] }, withPlatform(hermesDetail(true), [])], r)
    expect(reused.platform).toEqual([])
  })
  it('旧服务端没有平台变量：表单照旧，默认模型 deepseek-chat', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermesDetail(true)], null)
    expect(f.platform).toEqual([])
    expect(defaultModel(f)).toBe('deepseek-chat')
  })
})
