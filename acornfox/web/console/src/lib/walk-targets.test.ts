// 真机走查脚本共用的小模块（scripts/walk-targets.mjs）里不碰页面的部分：读环境变量、
// 按目录列表判断该不该有“需要的服务”、按服务端的首次登录信息判断该查什么。
import { describe, expect, it } from 'vitest'
import { DEFAULT_APPS, DEFAULT_NEEDS_TIMEOUT_MS, DEFAULT_SET, declaredNeeds, firstLoginResponse, loginExpectation, needsVerdict, recipeApps, walkTargets } from '../../scripts/walk-targets.mjs'

describe('walkTargets：从环境变量读要走查什么', () => {
  it('什么都没设时用默认值', () => {
    const t = walkTargets({})
    expect(t.apps).toEqual(DEFAULT_APPS.split(','))
    expect(t.recipe).toBe('')
    // 默认的设置项按 “应用id.设置项=值” 拆开
    const [left, value] = [DEFAULT_SET.slice(0, DEFAULT_SET.indexOf('=')), DEFAULT_SET.slice(DEFAULT_SET.indexOf('=') + 1)]
    expect(t.sets).toEqual([{ app: left.slice(0, left.indexOf('.')), key: left.slice(left.indexOf('.') + 1), value }])
    expect(t.needsTimeout).toBe(DEFAULT_NEEDS_TIMEOUT_MS)
  })

  it('应用按逗号分开，去掉空白和空项；配方去掉空白', () => {
    const t = walkTargets({ E2E_APPS: ' a , ,b,', E2E_RECIPE: '  bundle ' })
    expect(t.apps).toEqual(['a', 'b'])
    expect(t.recipe).toBe('bundle')
  })

  it('设置项按分号分开；值里可以有等号和点；空串表示什么都不填', () => {
    const t = walkTargets({ E2E_SET: 'x.url=http://a.b/c?d=1 ; y.n=2;' })
    expect(t.sets).toEqual([
      { app: 'x', key: 'url', value: 'http://a.b/c?d=1' },
      { app: 'y', key: 'n', value: '2' },
    ])
    expect(walkTargets({ E2E_SET: '' }).sets).toEqual([])
  })

  it('设置项写法不对时报错', () => {
    expect(() => walkTargets({ E2E_SET: 'novalue' })).toThrow(/E2E_SET/)
    expect(() => walkTargets({ E2E_SET: 'nodot=1' })).toThrow(/E2E_SET/)
    expect(() => walkTargets({ E2E_SET: 'k=a.b' })).toThrow(/E2E_SET/)
  })

  it('应用一个都没有时报错', () => {
    expect(() => walkTargets({ E2E_APPS: ' , ' })).toThrow(/E2E_APPS/)
  })

  it('等“需要的服务”的时限：正整数毫秒，写错报错', () => {
    expect(walkTargets({ E2E_NEEDS_TIMEOUT_MS: ' 3000 ' }).needsTimeout).toBe(3000)
    for (const bad of ['0', '-1', '1.5', 'abc']) expect(() => walkTargets({ E2E_NEEDS_TIMEOUT_MS: bad })).toThrow(/E2E_NEEDS_TIMEOUT_MS/)
  })
})

const cat = {
  apps: [
    { id: 'gw', name: '网关', uses: [] },
    { id: 'bot', name: '机器人', uses: [{ name: 'llm' }, { name: 'mail' }] },
    { id: 'plain', name: '没写需要的' },
  ],
  recipes: [{ id: 'bundle', title: '套装', apps: [{ app: 'gw' }, { app: 'bot' }] }],
}

describe('declaredNeeds：按目录列表判断该不该有“需要的服务”', () => {
  it('应用：列出它声明的每一项；没声明（空数组或没有这个字段）就是空', () => {
    expect(declaredNeeds(cat, 'app', 'bot')).toEqual(['bot.llm', 'bot.mail'])
    expect(declaredNeeds(cat, 'app', 'gw')).toEqual([])
    expect(declaredNeeds(cat, 'app', 'plain')).toEqual([])
  })

  it('配方：配方里各个应用声明的合在一起', () => {
    expect(recipeApps(cat, 'bundle')).toEqual(['gw', 'bot'])
    expect(declaredNeeds(cat, 'recipe', 'bundle')).toEqual(['bot.llm', 'bot.mail'])
  })

  it('目录里没有这个应用或配方时报错，不当成“没声明”悄悄跳过', () => {
    expect(() => declaredNeeds(cat, 'app', 'nope')).toThrow(/nope/)
    expect(() => declaredNeeds(cat, 'recipe', 'nope')).toThrow(/nope/)
    expect(() => declaredNeeds({ ...cat, recipes: [{ id: 'r', apps: [{ app: 'ghost' }] }] }, 'recipe', 'r')).toThrow(/ghost/)
  })
})

describe('needsVerdict：弹窗里读到的和声明的比', () => {
  const declared = ['bot.llm', 'bot.mail']
  it('没声明：不查', () => {
    expect(needsVerdict([], []).ok).toBeNull()
    expect(needsVerdict([], ['matched']).ok).toBeNull()
  })
  it('声明了却没出现：失败', () => {
    const v = needsVerdict(declared, [])
    expect(v.ok).toBe(false)
    expect(v.detail).toContain('bot.llm')
  })
  it('行数对不上：失败', () => {
    expect(needsVerdict(declared, ['matched']).ok).toBe(false)
  })
  it('都已接上才算通过', () => {
    expect(needsVerdict(declared, ['matched', 'matched']).ok).toBe(true)
    expect(needsVerdict(declared, ['matched', 'optional_missing']).ok).toBe(false)
    expect(needsVerdict(declared, ['matched', 'choose']).ok).toBe(false)
  })
})

describe('loginExpectation：按服务端的首次登录信息判断该查什么', () => {
  it('拿不到或没有项：不查', () => {
    expect(loginExpectation(null).shown).toBe(false)
    expect(loginExpectation({ fields: [] }).shown).toBe(false)
    expect(loginExpectation({}).shown).toBe(false)
  })
  it('有项没有保密项：要有卡片，不查遮住', () => {
    expect(loginExpectation({ fields: [{ label: '账号', value: 'root' }] })).toEqual({ shown: true, secret: false, maskedByServer: true })
  })
  it('有保密项：要遮住；服务端自己给了明文也算出来', () => {
    expect(loginExpectation({ fields: [{ label: '账号', value: 'root' }, { label: '密码', value: '******', secret: true }] })).toEqual({ shown: true, secret: true, maskedByServer: true })
    expect(loginExpectation({ fields: [{ label: '密码', value: 'plain-text', secret: true }] }).maskedByServer).toBe(false)
  })
})

describe('firstLoginResponse：只有 404 算没有首次登录信息', () => {
  it('404：没有，不查', () => {
    expect(firstLoginResponse(404, { error: 'x' })).toEqual({ fl: null, error: '' })
  })
  it('200：给出内容', () => {
    const body = { fields: [{ label: '账号', value: 'root' }] }
    expect(firstLoginResponse(200, body)).toEqual({ fl: body, error: '' })
  })
  it('其他状态码都算走查失败，不当成没有', () => {
    for (const status of [401, 403, 500, 502]) {
      const r = firstLoginResponse(status, { error: 'x' })
      expect(r.fl).toBeNull()
      expect(r.error).toContain(String(status))
    }
  })
  it('2xx 却不是 JSON：也算失败', () => {
    expect(firstLoginResponse(200, null).error).not.toBe('')
  })
})
