import { describe, expect, it } from 'vitest'
import { ApiError } from './api'
import { UPGRADE_CN, UPGRADE_GLOBAL, clockText, faultNeedsPlatform, higherVersion, installNeedsPlatform, needsPlatform, needsPlatformText, safeNotesUrl, upgradeFirstText, upgradeForAI, upgradeView } from './upgrade'
import type { PlatformUpdate } from './types'
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
    expect(UPGRADE_CN).toBe('curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade')
    expect(UPGRADE_GLOBAL).toBe('curl -fsSL https://github.com/OrbitMaker/AcornFox/releases/latest/download/get.sh | sudo bash -s -- upgrade')
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
    const he = { ...hermesDetail, needs_platform: '0.7.0' }
    expect(installNeedsPlatform('recipe', [newApiDetail, hermesDetail], { ...r, needs_platform: '0.8.0' })).toBe('0.8.0')
    expect(installNeedsPlatform('recipe', [na, he], r)).toBe('0.7.0')
    expect(installNeedsPlatform('recipe', [na, { ...he, installed: ['hermes-agent'] }], r)).toBe('0.6.0')
    expect(installNeedsPlatform('recipe', [newApiDetail, hermesDetail], r)).toBe('')
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

// ---- 平台新版本提醒（W12-3） ----

const NOW = new Date(2026, 10, 25, 12, 0, 0) // 本地时间 2026-11-25 12:00
const pu = (o: Partial<PlatformUpdate> = {}): PlatformUpdate => ({
  enabled: true, current: '0.7.0', available: false, ok: true, host_kind: 'linux',
  upgrade_command: 'curl -fsSL https://gitee.com/VIP13390/AcornFox/raw/main/get.sh | sudo bash -s -- upgrade', upgrade_where: '在这台服务器上运行', ...o,
})

describe('升级那一行：服务端的 platform_update', () => {
  it('有新版本：写新版本号和现在的版本，命令、在哪运行、发行说明都用服务端给的', () => {
    const v = upgradeView({ version: '0.7.0', platform_update: pu({ latest: '0.7.1', available: true, notes_url: 'https://gitee.com/x/releases/tag/v0.7.1', checked_at: new Date(2026, 10, 25, 10, 0).toISOString() }) }, true, NOW)
    expect(v).toMatchObject({ kind: 'new', line: '有新版本 0.7.1，现在是 0.7.0', latest: '0.7.1', where: '在这台服务器上运行', notesUrl: 'https://gitee.com/x/releases/tag/v0.7.1', canCheck: true })
    expect(v.commands).toEqual([pu().upgrade_command])
  })
  it('已是最新：带上次查的时间', () => {
    const v = upgradeView({ version: '0.7.0', platform_update: pu({ latest: '0.7.0', checked_at: new Date(2026, 10, 25, 10, 5).toISOString() }) }, null, NOW)
    expect(v.kind).toBe('latest')
    expect(v.line).toBe('现在是 0.7.0，已是最新（上次查：今天 10:05）')
  })
  it('已是最新但好几天没查到新的：说一句', () => {
    const v = upgradeView({ version: '0.7.0', platform_update: pu({ latest: '0.7.0', unreachable: true, ok: false, failures: 20 }) }, null, NOW)
    expect(v).toMatchObject({ kind: 'latest', stale: true, line: '现在是 0.7.0，已是最新（好几天没查到新的了）' })
  })
  it('还没查过、暂时查不到、已关闭', () => {
    expect(upgradeView({ version: '0.7.0', platform_update: pu({ ok: false }) }, null, NOW)).toMatchObject({ kind: 'pending', line: '现在是 0.7.0（还没查过最新版本）', canCheck: true })
    expect(upgradeView({ version: '0.7.0', platform_update: pu({ ok: false, checked_at: '2026-11-25T01:00:00Z', failures: 2, error: '连不上' }) }, null, NOW)).toMatchObject({ kind: 'unreachable', line: '现在是 0.7.0（暂时查不到最新版本）' })
    expect(upgradeView({ version: '0.7.0', platform_update: pu({ enabled: false }) }, null, NOW)).toMatchObject({ kind: 'off', line: '现在是 0.7.0（新版本提醒已关闭）' })
  })
  it('旧服务端没有 platform_update：只比 latest_version，说法和以前一样，命令按体检里的国内海外选', () => {
    expect(upgradeView({ version: '0.6.3', latest_version: '0.6.4' }, true, NOW)).toMatchObject({ kind: 'new', line: '有新版本 0.6.4，现在是 0.6.3', canCheck: false, commands: [UPGRADE_CN] })
    expect(upgradeView({ version: '0.6.3', latest_version: '0.6.3' }, false, NOW)).toMatchObject({ kind: 'latest', line: '现在是 0.6.3，已是最新', commands: [UPGRADE_GLOBAL] })
    expect(upgradeView({ version: '0.6.3' }, null, NOW)).toMatchObject({ kind: 'unknown', line: '现在是 0.6.3', commands: [UPGRADE_CN, UPGRADE_GLOBAL] })
    expect(upgradeView(null, null, NOW)).toMatchObject({ kind: 'unknown', line: '正在读取版本…' })
  })
  it('发行说明只认 https', () => {
    expect(safeNotesUrl('https://github.com/x')).toBe('https://github.com/x')
    expect(safeNotesUrl('javascript:alert(1)')).toBe('')
    expect(safeNotesUrl('http://x')).toBe('')
    expect(safeNotesUrl(undefined)).toBe('')
  })
  it('时间：今天、昨天、更早', () => {
    expect(clockText(new Date(2026, 10, 25, 9, 3).toISOString(), NOW)).toBe('今天 09:03')
    expect(clockText(new Date(2026, 10, 24, 23, 59).toISOString(), NOW)).toBe('昨天 23:59')
    expect(clockText(new Date(2026, 10, 20, 8, 0).toISOString(), NOW)).toBe('11-20 08:00')
    expect(clockText('', NOW)).toBe('')
    expect(clockText('坏的', NOW)).toBe('')
  })
  it('复制给 AI 的话：给了“在哪运行”就按它写，并说先问我', () => {
    const t = upgradeForAI('0.7.0', '0.7.1', ['powershell -NoProfile … -Action upgrade'], '在这台电脑的 PowerShell 里运行')
    expect(t).toContain('从 0.7.0 升级到 0.7.1')
    expect(t).toContain('问我同意以后，在这台电脑的 PowerShell 里运行')
    expect(t).toContain('-Action upgrade')
    // 不给 where 时和以前一样
    expect(upgradeForAI('0.6.3', '0.6.4', ['a'])).toContain('在这台机器上运行（会先打印要做的事')
  })
})
