import { describe, expect, it } from 'vitest'
import { envChanges, rowsFrom } from './tab-config'
import { limitOptions } from './tab-resources'
import { lineTone } from './tab-logs'
import { app, resources } from '@/test/fixtures'

describe('环境变量改动', () => {
  const a = app({ env: [{ key: 'TZ', secret: false, value: 'UTC' }, { key: 'TOKEN', secret: true }] })
  it('没改就什么都不发', () => {
    expect(envChanges(rowsFrom(a))).toEqual({ put: [], del: [], problems: [] })
  })
  it('密钥留空不改；改值、改名、删除', () => {
    const rows = rowsFrom(a)
    rows[0].value = 'Asia/Shanghai'
    rows.push({ key: 'NEW_KEY', value: '1', secret: false })
    let c = envChanges(rows)
    expect(c.put.map((r) => r.key)).toEqual(['TZ', 'NEW_KEY'])
    expect(c.del).toEqual([])
    rows[1].key = 'TOKEN2'
    c = envChanges(rows)
    expect(c.problems).toEqual(['密钥 TOKEN2 要填值'])
    rows[1].value = 'xyz'
    c = envChanges(rows)
    expect(c.del).toEqual(['TOKEN'])
    rows[0].removed = true
    expect(envChanges(rows).del).toEqual(['TZ', 'TOKEN'])
  })
  it('变量名不合法、重复', () => {
    const rows = [{ key: '1BAD', value: 'x', secret: false }, { key: 'A', value: '1', secret: false }, { key: 'A', value: '2', secret: false }]
    expect(envChanges(rows).problems.length).toBe(2)
  })
})

describe('资源上限可选项', () => {
  it('内存不超过上限减去别的应用，现在的值总在里面', () => {
    const o = limitOptions(resources, 'new-api', 700, 1000)
    expect(o.memMax).toBe(5120)
    expect(o.mem).toContain(700)
    expect(Math.max(...o.mem)).toBeLessThanOrEqual(5120)
    expect(Math.max(...o.cpu)).toBe(4000)
  })
})

describe('日志着色', () => {
  it('认出错误和警告', () => {
    expect(lineTone('2026/11/06 [ERROR] boom')).toBe('error')
    expect(lineTone('启动失败')).toBe('error')
    expect(lineTone('WARN slow')).toBe('warn')
    expect(lineTone('GET / 200')).toBe('')
  })
})
