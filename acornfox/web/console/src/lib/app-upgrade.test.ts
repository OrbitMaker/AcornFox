// 应用更新：服务端有没有说、能不能更新、计划怎么说成人话。
import { describe, expect, it } from 'vitest'
import { itemsText, outdatedCount, rollbackFacts, upgradeFacts, upgradeInfo, type UpgradePlan } from './app-upgrade'
import type { AppView } from './types'
import { app } from '@/test/fixtures'

const store = (over: Partial<AppView> = {}) => app({ name: 'n8n', catalog_id: 'n8n', catalog_version: '2.47.5', ...over })
const plan = (over: Partial<UpgradePlan> = {}): UpgradePlan => ({ from: '2.47.5', to: '2.48.0', restart: true, snapshot_mb: 64, disk_ok: true, ...over })

describe('更新情况', () => {
  it('服务端什么都没说：没有更新的事（不猜“已是最新”）', () => {
    expect(upgradeInfo(store())).toBeNull()
  })
  it('只有新版本号：能看到有新版本，但没有“更新”按钮', () => {
    const i = upgradeInfo(store({ catalog_update: '2.48.0' }))!
    expect(i.next).toBe('2.48.0')
    expect(i.canUpgrade).toBe(false)
  })
  it('服务端说能更新才有按钮；不能时带原因，原因的编号不进页面', () => {
    expect(upgradeInfo(store({ catalog_update: '2.48.0', upgrade: { available: '2.48.0', can_upgrade: true } }))!.canUpgrade).toBe(true)
    const b = upgradeInfo(store({ catalog_update: '2.48.0', upgrade: { available: '2.48.0', can_upgrade: false, blocked: '磁盘不够', blocked_code: 'UPG-02' } }))!
    expect(b.canUpgrade).toBe(false)
    expect(b.blocked).toBe('磁盘不够')
    expect(b.blockedCode).toBe('UPG-02')
  })
  it('退回到更新前：要有备份、服务端说能退回', () => {
    const snap = { id: 'b9', version: '2.47.5', created_at: '2026-11-24T10:30:00', size_mb: 64 }
    expect(upgradeInfo(store({ upgrade: { can_rollback: true, snapshot: snap } }))!.canRollback).toBe(true)
    expect(upgradeInfo(store({ upgrade: { can_rollback: true, snapshot: null } }))!.canRollback).toBe(false)
    expect(upgradeInfo(store({ upgrade: { can_rollback: false, snapshot: snap } }))!.canRollback).toBe(false)
  })
  it('有新版本的应用数：数量或列表都认，没说是 0', () => {
    expect(outdatedCount({ apps_outdated: 2 })).toBe(2)
    expect(outdatedCount({ apps_outdated: ['n8n', 'memos'] })).toBe(2)
    expect(outdatedCount({})).toBe(0)
    expect(outdatedCount(null)).toBe(0)
  })
})

describe('确认窗里的话', () => {
  it('更新：从哪到哪、重启、更新前先备份、多了哪几项设置', () => {
    const t = upgradeFacts(plan({ vars_added: [{ label: '加密密钥' }, '时区'], vars_removed: ['旧项'] }), 'n8n')
    expect(t[0]).toBe('n8n 从 2.47.5 更新到 2.48.0。')
    expect(t[1]).toContain('重启')
    expect(t[2]).toContain('先把现在的数据备份一份')
    expect(t[2]).toContain('64 MB')
    expect(t.join('')).toContain('多了 2 项设置（加密密钥、时区）')
    expect(t.join('')).toContain('1 项设置新版本不用了')
    expect(upgradeFacts(plan({ restart: false }), 'n8n')[1]).toBe('更新时不用重启。')
    expect(itemsText([{ name: 'a' }, 3, null, 'b'])).toEqual(['a', 'b'])
  })
  it('退回到更新前：写明版本、备份时间和会丢掉的数据', () => {
    const t = rollbackFacts({ id: 'b9', version: '2.47.5', created_at: '2026-11-24T10:30:00' }, 'n8n')
    expect(t[0]).toContain('2.47.5')
    expect(t[0]).toContain('11-24 10:30')
    expect(t[1]).toBe('更新以后新增的数据会丢掉。')
  })
})
