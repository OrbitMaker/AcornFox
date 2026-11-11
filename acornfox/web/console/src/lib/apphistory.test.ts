// 应用概览小图的数据转换：单位、上限、缺口。
import { describe, expect, it } from 'vitest'
import { appCharts } from './apphistory'
import type { AppHistory } from './types'

const at = (sec: number) => new Date(Date.UTC(2026, 10, 10, 0, 0, sec)).toISOString()

describe('appCharts', () => {
  it('把点变成内存（MB）和 CPU（%）两条线，内存上限取最后一个有上限的点', () => {
    const h: AppHistory = { app: 'x', range: '1h', step_seconds: 30, ranges: ['1h', '24h'], points: [
      { t: at(0), memory_usage_mb: 100.26, memory_limit_mb: 512 },
      { t: at(30), memory_usage_mb: 110, cpu_percent: 1.234, memory_limit_mb: 1024 },
    ] }
    const c = appCharts(h)
    expect(c.count).toBe(2)
    expect(c.mem.unit).toBe('MB')
    expect(c.mem.max).toBe(1024)
    expect(c.mem.series[0].data).toEqual([100.3, 110])
    expect(c.cpu.series[0].data).toEqual([null, 1.23])
    expect(c.cpu.labels.length).toBe(2)
  })

  it('应用停过一段（两个点隔得比 2.5 个步长久）时中间插一个空点，图上留缺口', () => {
    const h: AppHistory = { app: 'x', range: '1h', step_seconds: 30, ranges: ['1h'], points: [
      { t: at(0), memory_usage_mb: 100, cpu_percent: 1 },
      { t: at(60), memory_usage_mb: 100, cpu_percent: 1 }, // 隔 2 个步长：不算缺口
      { t: at(600), memory_usage_mb: 90, cpu_percent: 2 },
    ] }
    const c = appCharts(h)
    expect(c.mem.series[0].data).toEqual([100, 100, null, 90])
    expect(c.cpu.series[0].data).toEqual([1, 1, null, 2])
    expect(c.mem.labels.length).toBe(4)
    expect(c.count).toBe(3)
  })

  it('没有点时两条线都是空的', () => {
    const c = appCharts({ app: 'x', range: '24h', step_seconds: 300, ranges: ['1h', '24h'], points: [] })
    expect(c.count).toBe(0)
    expect(c.mem.series[0].data).toEqual([])
    expect(c.mem.max).toBeUndefined()
  })
})
