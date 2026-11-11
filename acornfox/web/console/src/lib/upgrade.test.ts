import { describe, expect, it } from 'vitest'
import { ApiError } from './api'
import { UPGRADE_CN, UPGRADE_GLOBAL, faultNeedsPlatform, higherVersion, installNeedsPlatform, needsPlatform, needsPlatformText, upgradeFirstText } from './upgrade'
import { catalog, hermesDetail, newApiDetail } from '@/test/fixtures'

describe('要先升级 AcornFox 才能装', () => {
  it('有值才算；空串、没有这个字段（旧服务端）、不是字符串都当不用升级', () => {
    expect(needsPlatform({ needs_platform: '0.6.0' })).toBe('0.6.0')
    expect(needsPlatform({ needs_platform: ' 0.6.0 ' })).toBe('0.6.0')
    expect(needsPlatform({ needs_platform: '' })).toBe('')
    expect(needsPlatform({})).toBe('')
    expect(needsPlatform(null)).toBe('')
    expect(needsPlatform({ needs_platform: 1 as unknown as string })).toBe('')
  })
  it('页面上的那句话', () => {
    expect(needsPlatformText('0.6.0')).toBe('要先把 AcornFox 升级到 0.6.0 才能装')
  })
  it('升级命令是公开的那两条，不带 -y', () => {
    expect(UPGRADE_CN).toBe('curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/scripts/install/upgrade.sh | sudo bash')
    expect(UPGRADE_GLOBAL).toBe('curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/upgrade.sh | sudo bash')
  })
  it('两个版本取高的', () => {
    expect(higherVersion('0.6.0', '0.10.0')).toBe('0.10.0')
    expect(higherVersion('1.0.0', '0.9.9')).toBe('1.0.0')
    expect(higherVersion('', '0.6.0')).toBe('0.6.0')
    expect(higherVersion('0.6.0', '')).toBe('0.6.0')
    expect(higherVersion('v0.6.1', '0.6.0')).toBe('v0.6.1')
    expect(higherVersion('0.6', '0.6.0')).toBe('0.6')
  })
  it('弹窗：单个应用看它自己', () => {
    expect(installNeedsPlatform('app', [{ ...newApiDetail, needs_platform: '0.6.0' }], null)).toBe('0.6.0')
    expect(installNeedsPlatform('app', [newApiDetail], null)).toBe('')
  })
  it('弹窗：配方以服务端给配方算的为准；没给时看要新装的应用，会被复用的不算', () => {
    const r = catalog.recipes[1]
    const na = { ...newApiDetail, needs_platform: '0.6.0' }
    const he = { ...hermesDetail(true), needs_platform: '0.7.0' }
    expect(installNeedsPlatform('recipe', [newApiDetail, hermesDetail(true)], { ...r, needs_platform: '0.8.0' })).toBe('0.8.0')
    expect(installNeedsPlatform('recipe', [na, he], r)).toBe('0.7.0')
    expect(installNeedsPlatform('recipe', [na, { ...he, installed: ['hermes-agent'] }], r)).toBe('0.6.0')
    expect(installNeedsPlatform('recipe', [newApiDetail, hermesDetail(true)], r)).toBe('')
  })
  it('哪些失败要给升级命令：只看错误里带没带 needs_platform，不看编号和说明里的字', () => {
    // 任务视图里的 fault
    expect(faultNeedsPlatform({ needs_platform: '0.6.0' })).toBe('0.6.0')
    expect(faultNeedsPlatform({ needs_platform: ' 0.6.0 ' })).toBe('0.6.0')
    // 接口错误
    expect(faultNeedsPlatform(new ApiError(409, 'CAT-INST-08', '要求 0.6.0 以上', { needs_platform: '0.6.0' }))).toBe('0.6.0')
    expect(faultNeedsPlatform(new ApiError(409, 'CAT-INST-29', '清单用了不认识的写法', { needs_platform: '9.9.9' }))).toBe('9.9.9')
    // 没带这个字段：编号是 CAT-INST-08、说明和建议里有“升级”两个字，也不给
    const f08 = { code: 'CAT-INST-08', message: '请先升级 AcornFox 再装' }
    const f29 = { code: 'CAT-INST-29', message: '这份目录有问题', hint: '错误里带 needs_platform 的：先升级 AcornFox 到那个版本再装' }
    expect(faultNeedsPlatform(f08)).toBe('')
    expect(faultNeedsPlatform(f29)).toBe('')
    expect(faultNeedsPlatform(new ApiError(409, 'CAT-INST-29', '这份目录有问题', { hint: '先升级 AcornFox 到那个版本再装' }))).toBe('')
    expect(faultNeedsPlatform({ needs_platform: '' })).toBe('')
    expect(faultNeedsPlatform({ needs_platform: 1 })).toBe('')
    expect(faultNeedsPlatform(new Error('普通错误'))).toBe('')
    expect(faultNeedsPlatform(null)).toBe('')
  })
  it('错误下面的那一句', () => {
    expect(upgradeFirstText('0.6.0')).toBe('要先升级到 0.6.0')
  })
  it('接口错误把 error.needs_platform 带出来；没有或不是字符串时是空串', async () => {
    const res = (body: unknown) => new Response(JSON.stringify(body), { status: 409, headers: { 'Content-Type': 'application/json' } })
    const { toApiError } = await import('./api')
    const e = await toApiError(res({ error: { code: 'CAT-INST-08', message: 'm', hint: 'h', needs_platform: '9.9.9' } }))
    expect([e.status, e.code, e.message, e.hint, e.needsPlatform]).toEqual([409, 'CAT-INST-08', 'm', 'h', '9.9.9'])
    expect((await toApiError(res({ error: { code: 'CAT-INST-29', message: 'm' } }))).needsPlatform).toBe('')
    expect((await toApiError(res({ error: { code: 'CAT-INST-29', message: 'm', needs_platform: 3 } }))).needsPlatform).toBe('')
  })
})
