// 让应用用上新设置：409 里的计划怎么认、计划里哪几项给用户看、说法。
import { describe, expect, it } from 'vitest'
import { ApiError } from './api'
import { behindApps, behindSummary, changeName, confirmPlan, planFacts, visibleChanges, type AffectedApp, type ReapplyPlan, type ReapplyVarChange } from './reapply'

const v = (over: Partial<ReapplyVarChange>): ReapplyVarChange => ({ name: 'params.x', old: 'a', new: 'b', from: 'user', ...over })
const plan = (over: Partial<ReapplyPlan> = {}): ReapplyPlan => ({ app: 'n8n', needed: true, restart: true, vars: [], notes: [], summary: 'n8n：1 项设置变了，改写 1 个环境变量；会重启 n8n', ...over })

describe('409 里的计划', () => {
  it('APP-APPLY-05 并且体里有计划才算要用户同意', () => {
    const p = plan()
    expect(confirmPlan(new ApiError(409, 'APP-APPLY-05', '要用户同意才做', { raw: { plan: p } }))).toEqual(p)
    expect(confirmPlan(new ApiError(409, 'APP-APPLY-05', '没有计划'))).toBeNull()
    expect(confirmPlan(new ApiError(409, 'domain_taken', '域名被占', { raw: { plan: p } }))).toBeNull()
    expect(confirmPlan(new Error('x'))).toBeNull()
  })
})

describe('计划里给用户看的', () => {
  it('访问地址里的协议、主机并进“打开的地址”，只有它们时照列', () => {
    const url = v({ name: 'platform.app.url', label: '这个应用的访问地址', old: 'http://127.0.0.1:18810', new: 'http://192.168.1.20:18810', from: 'platform' })
    const host = v({ name: 'platform.app.host', label: '访问地址里的主机', from: 'platform' })
    expect(visibleChanges([url, host]).map((x) => x.name)).toEqual(['platform.app.url'])
    expect(visibleChanges([host]).map((x) => x.name)).toEqual(['platform.app.host'])
    expect(changeName(url)).toBe('打开的地址')
    expect(changeName(v({ label: '这个应用的时区（IANA）', name: 'platform.tz' }))).toBe('时区')
    expect(changeName(v({ label: '', name: 'params.smtp_host' }))).toBe('smtp_host')
  })

  it('说人话：重启的说清楚数据不动、失败会退回；不重启的说不用重启；不出现“环境变量”', () => {
    expect(planFacts(plan(), 'n8n')[0]).toContain('n8n 会重启一下')
    expect(planFacts(plan(), 'n8n')[0]).toContain('数据不动')
    expect(planFacts(plan({ restart: false }), 'n8n')[0]).toBe('n8n 不用重启，改完马上生效。')
    expect(planFacts(plan(), 'n8n').join('')).not.toMatch(/环境变量|变量|配置文件/)
    expect(planFacts(plan({ dependents: ['memos'] }), 'n8n')).toHaveLength(2)
  })

  it('落在后面的：只算和现在不一样的；一句话说是哪几项', () => {
    const a = (over: Partial<AffectedApp>): AffectedApp => ({ app: 'n8n', differs: true, vars: [], restart: true, ...over })
    expect(behindApps({ var: '', apps: [a({}), a({ app: 'memos', differs: false })], notes: [] }).map((x) => x.app)).toEqual(['n8n'])
    expect(behindApps(null)).toEqual([])
    expect(behindSummary(a({ vars: [v({ name: 'platform.app.url', label: '访问地址' }), v({ name: 'platform.tz', label: '时区' })] }))).toBe('打开的地址、时区')
    expect(behindSummary(a({ vars: [] }))).toBe('设置')
  })
})
