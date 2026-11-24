// 后台任务的统一看法：两种任务整理成同一个样子；任务进度读哪个接口。
import { describe, expect, it } from 'vitest'
import { runView, taskPath } from './run'

describe('备份任务', () => {
  const t = {
    id: 'abc123abc123', op: 'backup', status: 'running',
    steps: [{ key: 'plan', text: '检查磁盘', status: 'done' }, { key: 'copy', text: '复制数据', status: 'running', detail: '1 个存放数据的地方' }],
  }
  it('步骤原样用服务端的人话，读 /v1/backups/tasks', () => {
    const r = runView(t)
    expect(r.backup).toBe(true)
    expect(r.status).toBe('running')
    expect(r.steps.map((s) => [s.text, s.status, s.detail])).toEqual([['检查磁盘', 'done', undefined], ['复制数据', 'running', '1 个存放数据的地方']])
    expect(taskPath(t)).toBe('/v1/backups/tasks/abc123abc123')
  })
  it('失败带原因和结果；没见过的状态当作还在做', () => {
    const r = runView({ ...t, status: 'failed', fault: { code: 'BKP-01', message: '磁盘不够', hint: '先清理' }, result: { rolled_back: true } })
    expect(r.status).toBe('failed')
    expect(r.fault).toEqual({ code: 'BKP-01', message: '磁盘不够', hint: '先清理' })
    expect(r.result?.rolled_back).toBe(true)
    expect(runView({ ...t, status: 'weird' }).status).toBe('running')
  })
})

describe('安装任务（重新生效、升级）', () => {
  const steps = [
    { app: 'n8n', name: 'check', label: '检查', status: 'done' },
    { app: 'n8n', name: 'files', label: '写设置', status: 'done' },
    { app: 'n8n', name: 'start', label: '重启', status: 'running' },
    { app: 'n8n', name: 'health', label: '健康检查', status: 'pending' },
    { app: 'n8n', name: 'revert', label: '退回', status: 'pending' },
  ]
  it('重新生效：步骤说人话，没轮到的“退回原样”不列', () => {
    const r = runView({ id: 't1', kind: 'reapply', status: 'running', steps })
    expect(r.backup).toBe(false)
    expect(r.steps.map((s) => s.text)).toEqual(['检查', '写入新设置', '重启', '等它能用'])
    expect(taskPath({ id: 't1', kind: 'reapply' })).toBe('/v1/catalog/install/t1')
  })
  it('走到退回原样时要列出来', () => {
    const r = runView({ id: 't1', kind: 'reapply', status: 'failed', steps: steps.map((s) => (s.name === 'revert' ? { ...s, status: 'done' } : s)), fault: { code: 'APP-APPLY-03', message: '没成功，已退回' } })
    expect(r.steps.map((s) => s.text)).toContain('退回原样')
    expect(r.fault?.message).toBe('没成功，已退回')
  })
  it('升级（kind 不是 reapply）：用服务端的步骤名，下载应用的“镜像”换成“应用程序”，带进度', () => {
    const r = runView({ id: 't2', kind: 'upgrade', status: 'running', steps: [{ app: 'n8n', name: 'image', label: '下载镜像', status: 'running', progress: { phase: 'download', done_bytes: 10, total_bytes: 100 } }] })
    expect(r.steps[0].text).toBe('下载应用')
    expect(r.steps[0].progress?.total_bytes).toBe(100)
  })
})
