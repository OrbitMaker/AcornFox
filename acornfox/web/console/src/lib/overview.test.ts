import { describe, expect, it } from 'vitest'
import { accessInfo, cpuOfLimit, ringPct, uptimeStrip, versionTimeline } from './overview'
import { app } from '@/test/fixtures'
import type { Deployment } from './types'

const NOW = new Date(2026, 10, 15, 22, 0, 0).getTime() // 2026-11-15 22:00（本地时间）
const ago = (days: number, hours = 0) => new Date(NOW - days * 86400000 - hours * 3600000).toISOString()

describe('运行条 uptimeStrip', () => {
  it('30 天以前就启动了：30 格全亮', () => {
    const u = uptimeStrip(ago(45), true, NOW)
    expect(u.cells.length).toBe(30)
    expect(u.upDays).toBe(30)
    expect(u.full).toBe(true)
    expect(u.cells[29].date).toBe('11-15')
    expect(u.cells[0].date).toBe('10-17')
  })
  it('3 天前启动：只亮启动那天到今天，更早的是空格（没有记录，不写成中断）', () => {
    const u = uptimeStrip(ago(3), true, NOW)
    expect(u.cells.map((c) => (c.up ? 1 : 0)).join('').slice(-5)).toBe('01111')
    expect(u.upDays).toBe(4)
    expect(u.full).toBe(false)
  })
  it('今天刚启动：只亮今天', () => {
    expect(uptimeStrip(ago(0, 1), true, NOW).upDays).toBe(1)
  })
  it('没在运行、没有启动时间、时间不对：全是空格', () => {
    expect(uptimeStrip(ago(10), false, NOW).upDays).toBe(0)
    expect(uptimeStrip(undefined, true, NOW).upDays).toBe(0)
    expect(uptimeStrip('不是时间', true, NOW).upDays).toBe(0)
  })
})

describe('圆环的百分比', () => {
  it('占上限的多少，夹在 0～100', () => {
    expect(ringPct(8, 640)).toBeCloseTo(1.25)
    expect(ringPct(700, 640)).toBe(100)
    expect(ringPct(0, 640)).toBe(0)
  })
  it('没有数或没有上限时是 null', () => {
    expect(ringPct(undefined, 640)).toBeNull()
    expect(ringPct(8, 0)).toBeNull()
    expect(ringPct(NaN, 640)).toBeNull()
  })
  it('CPU：接口里 100% 是一个核，上限按千分之一核', () => {
    expect(cpuOfLimit(50, 1000)).toBe(50)
    expect(cpuOfLimit(50, 500)).toBe(100)
    expect(cpuOfLimit(50, 2000)).toBe(25)
    expect(cpuOfLimit(undefined, 1000)).toBeNull()
    expect(cpuOfLimit(50, 0)).toBeNull()
  })
})

describe('版本时间线 versionTimeline', () => {
  const d = (seq: number, status: string, at = '2026-10-15T08:00:00Z'): Deployment => ({ id: 'd' + seq, app: 'web', seq, source_kind: 'upload', source_ref: '', status, created_at: at })
  it('从旧到新，正在用的标出来，别的写上线那天', () => {
    const n = versionTimeline([d(3, 'live'), d(2, 'superseded'), d(1, 'superseded')], 3)
    expect(n.map((x) => x.seq)).toEqual([1, 2, 3])
    expect(n[2]).toMatchObject({ live: true, when: '正在用' })
    expect(n[0].when).toMatch(/^10-1[56]$/)
  })
  it('版本多时只留最近 3 个；没上线的写“没换上”', () => {
    const n = versionTimeline([d(6, 'failed'), d(5, 'live'), d(4, 'superseded'), d(3, 'superseded'), d(2, 'superseded')], 5)
    expect(n.map((x) => x.seq)).toEqual([4, 5, 6])
    expect(n[2]).toMatchObject({ failed: true, when: '没换上' })
  })
  it('正在用的那一版很旧时也一定在里面', () => {
    const n = versionTimeline([d(9, 'failed'), d(8, 'failed'), d(7, 'failed'), d(6, 'failed'), d(2, 'live')], 2)
    expect(n.map((x) => x.seq)).toEqual([2, 8, 9])
    expect(n[0].live).toBe(true)
  })
  it('读不到版本列表时只画正在用的那一版；什么都没有时是空的', () => {
    expect(versionTimeline(null, 4)).toEqual([{ seq: 4, live: true, failed: false, when: '正在用' }])
    expect(versionTimeline(null, 0)).toEqual([])
  })
})

describe('访问 accessInfo', () => {
  it('局域网地址：地址、端口、档位，能直接打开', () => {
    const x = accessInfo(app({ url: 'http://192.168.1.20:18810', exposure: 'lan' }))
    expect(x).toMatchObject({ address: '192.168.1.20:18810', scheme: 'http', level: 'lan', openable: true, domain: '', lanURL: '' })
  })
  it('绑了域名：地址写域名，另给局域网地址', () => {
    const x = accessInfo(app({ url: 'https://demo.example.com', lan_url: 'http://192.168.1.20:18810', exposure: 'public', domains: [{ app: 'web', name: 'demo.example.com', status: 'active', created_at: '' }] }))
    expect(x).toMatchObject({ address: 'demo.example.com', scheme: 'https', domain: 'demo.example.com', level: 'public', lanURL: 'http://192.168.1.20:18810' })
  })
  it('平台给的域名生效时：地址是 https、没有端口，下面写这个域名；自己的域名优先；没生效时不算', () => {
    const pd = { host: 'web.apps.example.com', active: true, note: '' }
    const x = accessInfo(app({ url: 'https://web.apps.example.com', lan_url: 'http://192.168.1.20:18810', exposure: 'public', platform_domain: pd }))
    expect(x).toMatchObject({ address: 'web.apps.example.com', scheme: 'https', domain: 'web.apps.example.com', lanURL: 'http://192.168.1.20:18810' })
    const own = accessInfo(app({ url: 'https://demo.example.com', exposure: 'public', platform_domain: pd, domains: [{ app: 'web', name: 'demo.example.com', status: 'ready', created_at: '' }] }))
    expect(own?.domain).toBe('demo.example.com')
    const off = accessInfo(app({ url: 'http://192.168.1.20:18810', exposure: 'lan', platform_domain: { ...pd, active: false, note: '改成公网后自动生效' } }))
    expect(off).toMatchObject({ domain: '', scheme: 'http' })
  })
  it('只本机的 127.0.0.1 不能直接打开；没有地址时是 null；不认识的访问方式不标档位', () => {
    expect(accessInfo(app({ url: 'http://127.0.0.1:18810', exposure: 'local' }))?.openable).toBe(false)
    expect(accessInfo(app({ url: '', exposure: 'internal' }))).toBeNull()
    expect(accessInfo(app({ url: 'http://10.0.0.5:18810', exposure: 'afx_cloud' }))?.level).toBe('')
  })
})
