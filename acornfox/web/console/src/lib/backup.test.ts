// 备份和恢复：一行怎么写、收起那一句、哪些错误说明服务端不支持备份。
import { describe, expect, it } from 'vitest'
import { ApiError } from './api'
import { backupForAI, backupLine, backupSummary, backupUnsupported, kindText, type BackupList, type BackupMeta } from './backup'

const NOW = new Date('2026-11-24T12:00:00')
const meta = (over: Partial<BackupMeta> = {}): BackupMeta => ({ id: 'b1', app: 'n8n', kind: 'manual', created_at: '2026-11-24T10:30:00', bytes: 12 * 1024 * 1024, ...over })
const list = (over: Partial<BackupList> = {}): BackupList => ({ app: 'n8n', installed: true, backups: [], tasks: [], keep: { manual: 5, pre_upgrade: 1, pre_restore: 1 }, ...over })

describe('备份的说法', () => {
  it('三种备份叫手动备份、更新前自动备份、恢复前自动备份', () => {
    expect(kindText('manual')).toBe('手动备份')
    expect(kindText('pre-upgrade')).toBe('更新前自动备份')
    expect(kindText('pre-restore')).toBe('恢复前自动备份')
    expect(kindText('???')).toBe('备份')
  })
  it('列表里一行：时间、哪一种、多大，带版本时写版本', () => {
    expect(backupLine(meta(), NOW)).toBe('11-24 10:30 · 手动备份 · 12 MB')
    expect(backupLine(meta({ kind: 'pre-upgrade', version: { catalog_version: '2.47.5' } }), NOW)).toBe('11-24 10:30 · 更新前自动备份 · 12 MB · 版本 2.47.5')
  })
  it('收起时那一句：没有、最近一份加份数、正在做', () => {
    expect(backupSummary(list(), NOW)).toBe('还没有备份')
    expect(backupSummary(list({ backups: [meta(), meta({ id: 'b0' })] }), NOW)).toBe('最近一份 11-24 10:30，共 2 份')
    expect(backupSummary(list({ busy: 'abc123abc123' }), NOW)).toBe('正在备份或恢复')
    expect(backupSummary(null, NOW)).toBe('')
  })
  it('服务端不支持：404、405 或 BKP-11；别的错误不算', () => {
    expect(backupUnsupported(new ApiError(404, 'not_found', ''))).toBe(true)
    expect(backupUnsupported(new ApiError(409, 'BKP-11', ''))).toBe(true)
    expect(backupUnsupported(new ApiError(500, 'internal', ''))).toBe(false)
  })
  it('复制给 AI 的文字带编号、任务号和命令', () => {
    const t = backupForAI('n8n', 'n8n', '备份', { code: 'BKP-01', message: '磁盘不够' }, 'abc')
    expect(t).toContain('BKP-01')
    expect(t).toContain('acornfox backup list n8n')
    expect(t).toContain('abc')
  })
})
