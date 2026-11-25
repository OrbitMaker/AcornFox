// 这台机器上不是从 AcornFox 装的容器：显示的整理（lib/others.ts）和首页“各应用内存、CPU”里合成的一项。
import { describe, expect, it } from 'vitest'
import { SOURCES, type DashCtx } from './dashboard'
import { othersForChart, otherPorts, otherState, othersSummary, otherUsage } from './others'
import { app } from '@/test/fixtures'
import type { OtherContainer, OtherContainersView } from './types'

const c = (p: Partial<OtherContainer>): OtherContainer => ({
  id: 'a1', name: 'redis', image: 'redis:7', state: 'running', running: true, created: 1, ports: [], stats_available: false, cpu_available: false, cpu_percent: 0, memory_usage_mb: 0, ...p,
})
const view = (p: Partial<OtherContainersView> = {}): OtherContainersView => ({
  observed_at: '2026-11-25T10:00:00Z', running: 2, stopped: 1, memory_mb: 120.5, cpu_percent: 1.3,
  containers: [
    c({ name: 'redis', stats_available: true, cpu_available: true, cpu_percent: 0.8, memory_usage_mb: 20.5, ports: [{ host: 6379, container: 6379, proto: 'tcp' }] }),
    c({ id: 'b2', name: 'grafana', stats_available: true, memory_usage_mb: 100 }),
    c({ id: 'c3', name: 'old-db', state: 'exited', running: false }),
  ],
  ...p,
})

describe('其他容器的整理', () => {
  it('状态用人话：在跑、停了、暂停、在反复重启；停了不算要处理', () => {
    expect(otherState({ state: 'running', running: true })).toEqual({ text: '在跑', tone: 'success' })
    expect(otherState({ state: 'exited', running: false })).toEqual({ text: '停了', tone: 'muted' })
    expect(otherState({ state: 'dead', running: false }).text).toBe('停了')
    expect(otherState({ state: 'paused', running: false }).text).toBe('暂停了')
    expect(otherState({ state: 'restarting', running: false })).toEqual({ text: '在反复重启', tone: 'warning' })
    expect(otherState({ state: 'created', running: false }).text).toBe('没启动过')
  })

  it('端口：同一个端口只写一次；用本机网络的和没有端口的', () => {
    expect(otherPorts({ ports: [{ host: 80, container: 8080, proto: 'tcp' }, { host: 80, container: 8080, proto: 'udp' }, { host: 443, container: 443, proto: 'tcp' }] })).toBe('端口 80、443')
    expect(otherPorts({ ports: [] })).toBe('')
    expect(otherPorts({ ports: [], host_network: true })).toBe('用本机的网络')
  })

  it('内存只在读到用量时写；收起时的一句话', () => {
    expect(otherUsage({ stats_available: false, memory_usage_mb: 0 })).toBe('')
    expect(otherUsage({ stats_available: true, memory_usage_mb: 20.5 })).toBe('21 MB')
    expect(othersSummary(view())).toBe('在跑 2 个，停了 1 个，用了 121 MB 内存')
    expect(othersSummary(view({ running: 0, stopped: 3, memory_mb: 0, containers: [c({ state: 'exited', running: false })] }))).toBe('停了 3 个')
    expect(othersSummary(view({ stopped: 0 }))).toBe('在跑 2 个，用了 121 MB 内存')
  })
})

describe('首页各应用内存、CPU：合成一项“其他容器”', () => {
  const ctx = (p: Partial<DashCtx> = {}): DashCtx => ({
    now: new Date('2026-11-25T12:00:00Z'), range: '1h', host: null, history: null, apps: [], installing: new Set(), metrics: {},
    resources: null, checks: null, deployments: [], tasks: [], ...p,
  })
  const web = app({ name: 'web' })
  const wm = { web: { app: 'web', available: true, observed_state: 'running', memory_usage_mb: 300, cpu_percent: 2 } }

  it('在跑的加起来成一项，放在最后，总数才对得上', () => {
    const mem = SOURCES.appMem.gen(ctx({ apps: [web], metrics: wm, others: view() }))
    if (mem.kind !== 'cat') throw new Error('kind')
    expect(mem.items.map((i) => [i.name, i.value])).toEqual([['web', 300], ['其他容器', 120.5]])
    const cpu = SOURCES.appCpu.gen(ctx({ apps: [web], metrics: wm, others: view() }))
    if (cpu.kind !== 'cat') throw new Error('kind')
    expect(cpu.items.map((i) => [i.name, i.value])).toEqual([['web', 2], ['其他容器', 1.3]])
  })

  it('没有、读不到、没有在跑的、没读到用量时不加这一项；这时图表照原来的样子', () => {
    for (const others of [null, undefined, view({ running: 0, stopped: 3, memory_mb: 0, containers: [c({ state: 'exited', running: false })] }), view({ containers: [c({}), c({ id: 'z' })] })]) {
      const mem = SOURCES.appMem.gen(ctx({ apps: [web], metrics: wm, others }))
      if (mem.kind !== 'cat') throw new Error('kind')
      expect(mem.items.map((i) => i.name)).toEqual(['web'])
    }
    // 在跑的都没读到 CPU 时，CPU 图不加；内存图照加
    const noCpu = view({ containers: [c({ stats_available: true, cpu_available: false, memory_usage_mb: 50 })], running: 1, stopped: 0, memory_mb: 50, cpu_percent: 0 })
    expect(othersForChart(noCpu, 'cpu')).toBeNull()
    expect(othersForChart(noCpu, 'memory')).toBe(50)
  })

  it('没有应用在跑、只有其他容器时，图上只有这一项', () => {
    const mem = SOURCES.appMem.gen(ctx({ apps: [], others: view() }))
    expect(mem).toMatchObject({ kind: 'cat', unit: 'MB', items: [{ name: '其他容器', value: 120.5 }] })
  })

  it('这两张图要等其他容器的数据读到第一次', () => {
    expect(SOURCES.appMem.needs).toContain('others')
    expect(SOURCES.appCpu.needs).toContain('others')
  })
})
