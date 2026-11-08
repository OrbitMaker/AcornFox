import { describe, expect, it } from 'vitest'
import { deriveStatus, liveSeq, loginAddress, oldVersionServing } from './appstatus'
import { durationText, mbText, coresText } from './format'
import { appHref, parseHash } from './router'
import { app } from '@/test/fixtures'

describe('应用状态', () => {
  it('各种情况', () => {
    expect(deriveStatus(app())).toBe('running')
    expect(deriveStatus(app({ desired: 'stopped' }))).toBe('stopped')
    expect(deriveStatus(app({ observed_state: 'stopped' }))).toBe('crashed')
    expect(deriveStatus(app({ observed_state: 'unavailable' }))).toBe('unknown')
    expect(deriveStatus(app({ latest: { ...app().live!, id: 'bbbbbbbbbbbb', seq: 2, status: 'building' } }))).toBe('deploying')
    expect(deriveStatus(app(), new Set(['web']))).toBe('installing')
    expect(deriveStatus(app({ live: undefined, current_deployment: '' }))).toBe('empty')
  })
  it('新版本失败、旧版本还在服务', () => {
    const a = app({ latest: { ...app().live!, id: 'bbbbbbbbbbbb', seq: 2, status: 'failed' } })
    expect(deriveStatus(a)).toBe('failed')
    expect(oldVersionServing(a)).toBe(true)
  })
})

describe('显示格式', () => {
  it('大小、时长、核数', () => {
    expect(mbText(512)).toBe('512 MB')
    expect(mbText(1536)).toBe('1.5 GB')
    expect(mbText(2048)).toBe('2 GB')
    expect(durationText(63)).toBe('1 分 03 秒')
    expect(durationText(9)).toBe('9 秒')
    expect(coresText(500)).toBe('0.5 核')
    expect(coresText(2000)).toBe('2 核')
  })
})

describe('路由', () => {
  it('解析和生成', () => {
    expect(parseHash('')).toEqual({ page: 'home' })
    expect(parseHash('#store')).toEqual({ page: 'store' })
    expect(parseHash('#item-new-api')).toEqual({ page: 'item', id: 'new-api' })
    expect(parseHash('#app-new-api/logs?v=3')).toEqual({ page: 'app', id: 'new-api', tab: 'logs', query: { v: '3' } })
    expect(parseHash('#app-web/nope').tab).toBe('overview')
    expect(appHref('new-api', 'logs', { v: '3' })).toBe('#app-new-api/logs?v=3')
    expect(appHref('web', 'overview')).toBe('#app-web')
  })
})

describe('链接和图标', () => {
  it('safeHref 只放行 http 和 https', async () => {
    const { safeHref } = await import('./utils')
    expect(safeHref('https://github.com/x')).toBe('https://github.com/x')
    expect(safeHref('HTTP://example.com')).toBe('HTTP://example.com')
    expect(safeHref('http://127.0.0.1:18810')).toBe('http://127.0.0.1:18810')
    expect(safeHref('javascript:alert(1)')).toBeUndefined()
    expect(safeHref(' javascript:alert(1)')).toBeUndefined()
    expect(safeHref('data:text/html,x')).toBeUndefined()
    expect(safeHref('//evil.example')).toBeUndefined()
    expect(safeHref('#store')).toBeUndefined()
    expect(safeHref('')).toBeUndefined()
    expect(safeHref(undefined)).toBeUndefined()
  })

  it('iconSrc 按类型生成 data: 地址，不认识的不显示', async () => {
    const { iconSrc } = await import('@/components/bits')
    expect(iconSrc('<svg/>', 'svg')!.startsWith('data:image/svg+xml')).toBe(true)
    expect(iconSrc('<svg/>')!.startsWith('data:image/svg+xml')).toBe(true)
    expect(iconSrc('iVBORw0KGgo=', 'png')).toBe('data:image/png;base64,iVBORw0KGgo=')
    expect(iconSrc('not base64!', 'png')).toBeUndefined()
    expect(iconSrc('R0lGOD', 'gif')).toBeUndefined()
    expect(iconSrc('iVBORw0KGgo=')).toBeUndefined()
    expect(iconSrc(undefined, 'svg')).toBeUndefined()
  })
})

describe('版本行和首次登录地址', () => {
  it('liveSeq：详情的 live、列表的 live_seq、旧服务端只有 latest 和 current_deployment', () => {
    const d = (id: string, seq: number, status: string) => ({ id, app: 'web', seq, source_kind: 'image', source_ref: 'x', status, created_at: '' })
    expect(liveSeq(app())).toBe(1)
    expect(liveSeq(app({ live: undefined, live_seq: 7 }))).toBe(7)
    expect(liveSeq(app({ live: undefined, current_deployment: 'd2', latest: d('d2', 2, 'live') }))).toBe(2)
    expect(liveSeq(app({ live: undefined, current_deployment: 'd2', latest: d('d3', 3, 'failed') }))).toBe(0)
    expect(liveSeq(app({ live: undefined, current_deployment: '' }))).toBe(0)
  })
  it('loginAddress：按应用现在的地址，保留路径；只本机仍是 127.0.0.1；不知道应用时用原地址', () => {
    const lan = app({ exposure: 'lan', url: 'http://192.168.1.20:18810' })
    const local = app({ url: 'http://127.0.0.1:18810', local_only: true, open_hint: { has_login: true, can_lan: true, recommend: 'lan', command: 'acornfox open web' } })
    expect(loginAddress('http://127.0.0.1:18810', lan)).toBe('http://192.168.1.20:18810')
    expect(loginAddress('http://127.0.0.1:18810/admin?x=1', lan)).toBe('http://192.168.1.20:18810/admin?x=1')
    expect(loginAddress('http://127.0.0.1:18810', local)).toBe('http://127.0.0.1:18810')
    expect(loginAddress('http://127.0.0.1:18810', undefined)).toBe('http://127.0.0.1:18810')
    expect(loginAddress('', lan)).toBe('http://192.168.1.20:18810')
    expect(loginAddress('http://127.0.0.1:18810', app({ url: '', lan_url: '' }))).toBe('http://127.0.0.1:18810')
  })
})
