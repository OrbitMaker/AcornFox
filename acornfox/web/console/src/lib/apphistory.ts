// 应用概览页两张小图的数据：把 GET /v1/metrics/apps/{app}/history 的点变成图表要的形状。
// 应用停着的时候后端不记点；相邻两个点隔得比 2.5 个步长还久，就在中间插一个空点，图上留出缺口，不连成一条线。
import { pointLabel, type Data } from './dashboard'
import type { AppHistory } from './types'

type TS = Extract<Data, { kind: 'ts' }>

const round = (v: number, dec = 1) => Math.round(v * 10 ** dec) / 10 ** dec

export interface AppCharts { mem: TS; cpu: TS; count: number }

export function appCharts(h: AppHistory): AppCharts {
  const labels: TS['labels'] = []
  const mem: (number | null)[] = []
  const cpu: (number | null)[] = []
  const gap = (h.step_seconds || 30) * 2.5 * 1000
  let prev = 0
  let limit: number | undefined
  for (const p of h.points) {
    const t = Date.parse(p.t)
    if (prev && t - prev > gap) {
      labels.push(pointLabel(new Date(prev + (t - prev) / 2).toISOString()))
      mem.push(null)
      cpu.push(null)
    }
    prev = t
    labels.push(pointLabel(p.t))
    mem.push(p.memory_usage_mb === undefined ? null : round(p.memory_usage_mb))
    cpu.push(p.cpu_percent === undefined ? null : round(p.cpu_percent, 2))
    if (p.memory_limit_mb) limit = p.memory_limit_mb
  }
  return {
    count: h.points.length,
    mem: { kind: 'ts', unit: 'MB', max: limit, labels, series: [{ key: 'mem', name: '内存', color: 'var(--s1)', data: mem }] },
    cpu: { kind: 'ts', unit: '%', labels, series: [{ key: 'cpu', name: 'CPU', color: 'var(--s2)', data: cpu }] },
  }
}
