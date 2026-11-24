// 装应用时选谁能打开、下载进度的字、体检里展示内容这一行。
import { describe, expect, it } from 'vitest'
import { buildRequest, cliCommand, newInstallForm, progressLine } from './catalog'
import { checkupRows, conclusion, showCheck } from '@/pages/runtime/checkup'
import { newApiDetail, hermesDetail } from '@/test/fixtures'
import type { RuntimeChecks } from './types'

describe('安装时的“谁能打开”', () => {
  it('默认是服务端给的；不改不发，改了才发，命令行也带 --expose', () => {
    const f = newInstallForm('app', 'new-api', [{ ...newApiDetail, exposure: 'lan' }], null)
    expect([f.exposeDefault, f.expose]).toEqual(['lan', 'lan'])
    expect(buildRequest(f).expose).toBeUndefined()
    expect(cliCommand(f)).not.toContain('--expose')
    const g = { ...f, expose: 'public' }
    expect(buildRequest(g).expose).toBe('public')
    expect(cliCommand(g)).toContain('--expose public')
  })
  it('只在内部网络的没有这一项；解决方案里跳过内部的和已装好的，取第一个要新装的', () => {
    expect(newInstallForm('app', 'new-api', [{ ...newApiDetail, exposure: 'internal' }], null).exposeDefault).toBe('')
    const r = { id: 'r', title: 'r', summary: '', apps: [{ app: 'new-api' }, { app: 'hermes-agent' }], delivery: { tier: 'auto' }, command: '' }
    const f = newInstallForm('recipe', 'r', [{ ...newApiDetail, exposure: 'internal' }, { ...hermesDetail, exposure: 'lan' }], r)
    expect(f.exposeDefault).toBe('lan')
    const g = newInstallForm('recipe', 'r', [{ ...newApiDetail, exposure: 'public', installed: ['new-api'] }, { ...hermesDetail, exposure: 'local' }], r)
    expect(g.exposeDefault).toBe('local')
  })
})

describe('下载进度的字', () => {
  it('经网关下载和从公共源拉都按字节写：已下、一共、速度、还要多久', () => {
    expect(progressLine({ phase: 'download', done_bytes: 50 << 20, total_bytes: 100 << 20, bytes_per_sec: 5 << 20, eta_seconds: 10 })).toEqual({ pct: 50, text: '50 MB / 100 MB · 5 MB/秒 · 剩约 10 秒', stalled: false })
    expect(progressLine({ phase: 'pull', source: 'x', done_bytes: 120 << 20, total_bytes: 300 << 20, bytes_per_sec: 2 << 20 }).text).toBe('120 MB / 300 MB · 2 MB/秒 · 剩约 2 分钟')
  })
  it('量不出的几种：导入、还在挑线路、Docker 没报进度、一阵子没收到数据', () => {
    expect(progressLine({ phase: 'load', done_bytes: 1, total_bytes: 1 })).toMatchObject({ pct: null, text: '下好了，正在导入' })
    expect(progressLine({ phase: 'pull', done_bytes: 0 })).toMatchObject({ pct: null, text: '正在挑下载最快的线路' })
    const b = progressLine({ phase: 'pull', source: 'x', blind: true, done_bytes: 10 << 20, total_bytes: 100 << 20 })
    expect(b.text).toContain('看不到下到多少')
    expect(b.pct).toBe(10)
    const s = progressLine({ phase: 'download', done_bytes: 1 << 20, total_bytes: 10 << 20, stalled_seconds: 30 })
    expect(s.stalled).toBe(true)
    expect(s.text).toContain('30 秒没收到新数据')
  })
})

describe('体检里应用中心的介绍', () => {
  const c: RuntimeChecks = { os: 'linux', checked_at: '2026-11-24T10:00:00Z', ok: true, items: [{ ok: true, level: 'ok', text: '磁盘还剩 318 GB', key: 'disk' }] }
  it('展示内容没能用上才多一行，算一件要处理的事；取到了或目录里没有都不提', () => {
    expect(checkupRows(c, { show: { state: 'fetched' } }).some((r) => r.kind === 'show')).toBe(false)
    expect(checkupRows(c, { show: { state: 'none' } }).some((r) => r.kind === 'show')).toBe(false)
    const rows = checkupRows(c, { show: { state: 'failed', code: 'CAT-SHOW-02', message: '签名不对' } })
    const r = rows.find((x) => x.kind === 'show')!
    expect(r.state).toBe('这次没更新上，先用上次的')
    expect(conclusion(rows).count).toBe(1)
    expect(showCheck({ state: 'failed', code: 'CAT-SHOW-02' })?.code).toBe('CAT-SHOW-02')
    expect(showCheck({ state: 'fetched' })).toBeNull()
  })
})
