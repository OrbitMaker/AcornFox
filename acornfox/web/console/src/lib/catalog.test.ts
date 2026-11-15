import { describe, expect, it } from 'vitest'
import {
  abbr, buildRequest, categoryTabs, changedPlatform, checkForm, cliCommand, faultForAI, filterApps, freeMB, groupSteps, installingApps, memoryCheck,
  newInstallForm, recipeMemory, taskProgress, appKey, catalogAppOf, disconnectedSources, filterSource, findApp, sourceId, sourceTabs,
  FORMER_CODE, formerNote, isFormerFault, storeEmpty,
} from './catalog'
import { ApiError } from './api'
import { FORMER_NOTE, catalog, catalogFormer, hermesDetail, hermesWithModel, newApiDetail, recipeDetailsWithModel, resources, task } from '@/test/fixtures'
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
  it('需要服务的应用：服务不是表单里的设置项，什么都不填时请求只有应用名，命令里也没有别的写法', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermesDetail], null)
    expect(f.plain).toEqual([])
    expect(checkForm(f)).toEqual([])
    expect(buildRequest(f)).toEqual({ app: 'hermes-agent' })
    expect(cliCommand(f)).toBe('acornfox install hermes-agent')
  })
  it('配方：参数带应用前缀，请求和命令行都只写配方名和填了的设置', () => {
    const r = catalog.recipes[1]
    const f = newInstallForm('recipe', r.id, [newApiDetail, hermesDetail], r)
    expect(f.plain.map((p) => p.key)).toEqual(['new-api.admin_username', 'new-api.admin_password'])
    expect(checkForm(f)).toEqual([])
    expect(buildRequest(f)).toEqual({ recipe: 'hermes-with-new-api' })
    expect(cliCommand(f)).toBe('acornfox install --recipe hermes-with-new-api')
    f.plain[0].value = 'root'
    expect(buildRequest(f).params).toEqual({ 'new-api.admin_username': 'root' })
    expect(cliCommand(f)).toBe('acornfox install --recipe hermes-with-new-api --set new-api.admin_username=root')
  })
  it('配方里已经装过的应用不再填参数', () => {
    const r = catalog.recipes[1]
    const f = newInstallForm('recipe', r.id, [{ ...newApiDetail, installed: ['new-api'] }, hermesDetail], r)
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
    expect(s).not.toContain('needs_platform')
  })
  it('原因是版本不够时，给 AI 的诊断带上要升到的版本', () => {
    const t = task({ status: 'failed', fault: { code: 'CAT-INST-08', message: '要求 AcornFox 0.6.0 以上', hint: '先升级 AcornFox 再装', needs_platform: '0.6.0' } })
    expect(faultForAI(t)).toContain('"needs_platform": "0.6.0"')
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
  const hermes = () => withPlatform(hermesDetail, [
    pv('mirror.npm', 'https://registry.npmmirror.com/', true), pv('mirror.pypi', 'https://mirrors.aliyun.com/pypi/simple/', true), pv('tz', 'Asia/Shanghai', true),
  ])
  it('不改就不发，命令也不多写', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermes()], null)
    expect(f.platform.map((p) => p.view.name)).toEqual(['mirror.npm', 'mirror.pypi', 'tz'])
    expect(changedPlatform(f)).toEqual({})
    expect(buildRequest(f).platform).toBeUndefined()
    expect(cliCommand(f)).toBe('acornfox install hermes-agent')
  })
  it('改了的进请求和命令；改回原值就不发；只多了空格不算改', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermes()], null)
    f.platform[2].value = 'Asia/Tokyo'
    expect(buildRequest(f).platform).toEqual({ tz: 'Asia/Tokyo' })
    expect(cliCommand(f)).toBe('acornfox install hermes-agent --set platform.tz=Asia/Tokyo')
    f.platform[2].value = 'Asia/Shanghai'
    expect(buildRequest(f).platform).toBeUndefined()
    f.platform[0].value = ' https://registry.npmmirror.com/ '
    expect(buildRequest(f).platform).toBeUndefined()
  })
  it('格式不对在页面上拦下；不能改的不查、不发', () => {
    const f = newInstallForm('app', 'hermes-agent', [withPlatform(hermesDetail, [pv('mirror.pypi', 'https://a/', true), pv('tz', 'UTC', true), pv('region', 'cn', false)])], null)
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
    expect(f.platform.map((p) => p.view.name)).toEqual(['tz', 'mirror.npm', 'mirror.pypi'])
    const reused = newInstallForm('recipe', r.id, [{ ...na, installed: ['new-api'] }, withPlatform(hermesDetail, [])], r)
    expect(reused.platform).toEqual([])
  })
  it('旧服务端没有平台变量：表单照旧，没有平台变量这一块', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermesDetail], null)
    expect(f.platform).toEqual([])
    expect(buildRequest(f)).toEqual({ app: 'hermes-agent' })
  })
})

describe('旧格式的清单和空目录', () => {
  it('former 为 true 时给服务端的那句话；没给时用一句通用的；不是 former 时为空', () => {
    expect(formerNote(catalogFormer.apps[0])).toBe(FORMER_NOTE)
    expect(formerNote(catalogFormer.recipes[0])).toBe(FORMER_NOTE)
    expect(formerNote({ former: true })).toBe('这是旧格式的清单，这一版装不了')
    expect(formerNote({ former: true, note: '  ' })).toBe('这是旧格式的清单，这一版装不了')
    expect(formerNote(catalogFormer.apps[1])).toBe('')
    expect(formerNote({ note: '有话但不是 former' })).toBe('')
    expect(formerNote(null)).toBe('')
  })
  it('读详情或安装被拒绝的原因是不是旧格式的清单：只认编号', () => {
    expect(FORMER_CODE).toBe('CAT-INST-36')
    expect(isFormerFault(new ApiError(409, 'CAT-INST-36', 'x 是旧格式的清单'))).toBe(true)
    expect(isFormerFault({ code: 'CAT-INST-36', message: '' })).toBe(true)
    expect(isFormerFault(new ApiError(409, 'CAT-INST-08', '要先升级'))).toBe(false)
    expect(isFormerFault(null)).toBe(false)
  })
  it('列表空时怎么说：没在搜索、目录里一个应用都没有 → 还没有可以安装的应用；搜索没结果 → 没找到', () => {
    expect(storeEmpty(0, 0, '')).toEqual({ title: '还没有可以安装的应用', sub: '应用按新格式上架后会出现在这里', searching: false })
    expect(storeEmpty(0, 0, '   ')).toEqual({ title: '还没有可以安装的应用', sub: '应用按新格式上架后会出现在这里', searching: false })
    expect(storeEmpty(0, 0, 'abc')).toEqual({ title: '没找到“abc”', searching: true })
    expect(storeEmpty(2, 0, 'abc')).toEqual({ title: '没找到“abc”', searching: true })
    expect(storeEmpty(2, 2, '')).toBeNull()
  })
})

describe('应用自己的必填设置项（例如模型名）', () => {
  it('和别的设置项一样：不填在页面上拦下，提示用中文名；填了进请求的 params，命令行写成 --set 设置项=值', () => {
    const f = newInstallForm('app', 'hermes-agent', [hermesWithModel], null)
    expect(f.plain.map((p) => p.key)).toEqual(['model'])
    expect(checkForm(f)).toEqual([{ key: 'model', message: '请填写模型名' }])
    f.plain[0].value = '  qwen-plus '
    expect(checkForm(f)).toEqual([])
    expect(buildRequest(f)).toEqual({ app: 'hermes-agent', params: { model: 'qwen-plus' } })
    expect(cliCommand(f)).toBe('acornfox install hermes-agent --set model=qwen-plus')
    // 值里有空格等特殊字符时命令行加引号
    f.plain[0].value = 'my model'
    expect(cliCommand(f)).toBe("acornfox install hermes-agent --set model='my model'")
  })
  it('配方里：键带应用名前缀，命令行写成 --set 应用.设置项=值', () => {
    const r = catalog.recipes[1]
    const f = newInstallForm('recipe', r.id, recipeDetailsWithModel, r)
    expect(checkForm(f)).toEqual([{ key: 'hermes-agent.model', message: '请填写模型名' }])
    f.plain.find((p) => p.key === 'hermes-agent.model')!.value = 'qwen-plus'
    expect(checkForm(f)).toEqual([])
    expect(buildRequest(f)).toEqual({ recipe: r.id, params: { 'hermes-agent.model': 'qwen-plus' } })
    expect(cliCommand(f)).toBe('acornfox install --recipe ' + r.id + ' --set hermes-agent.model=qwen-plus')
  })
})
