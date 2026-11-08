import { describe, expect, it } from 'vitest'
import {
  DEFAULT_LAYOUT, LAYOUT_KEY, SOURCES, dropWidget, eventItems, fmtValue, loadLayout, moveWidget, rangeTabs, saveLayout, sourceUsable,
  todoItems, typesFor, typesForSource, type DashCtx,
} from './dashboard'
import { app, task } from '@/test/fixtures'
import type { AppView, HostHistory } from './types'

const GB = 1024 ** 3
const hist = (n: number): HostHistory => ({
  range: '1h', step_seconds: 30, ranges: ['1h', '24h'], since: '2026-11-06T00:00:00Z',
  points: Array.from({ length: n }, (_, i) => ({ t: new Date(Date.UTC(2026, 10, 6, 0, 0, 30 * i)).toISOString(), cpu_percent: 10 + i, memory_used: 2 * GB, memory_total: 8 * GB })),
})

function ctx(p: Partial<DashCtx> = {}): DashCtx {
  return {
    now: new Date('2026-11-06T12:00:00Z'), range: '1h', host: null, history: null, apps: [], installing: new Set(), metrics: {},
    resources: null, checks: null, deployments: [], tasks: [], ...p,
  }
}

describe('看板的数据', () => {
  it('CPU、内存来自整机历史；没有历史时说清楚，不画假数据', () => {
    expect(SOURCES.cpu.gen(ctx())).toEqual({ kind: 'none', text: '这台服务器没有记录历史' })
    expect(SOURCES.cpu.gen(ctx({ history: { ...hist(0) } }))).toMatchObject({ kind: 'none' })
    const d = SOURCES.cpu.gen(ctx({ history: hist(3) }))
    expect(d.kind).toBe('ts')
    if (d.kind !== 'ts') return
    expect(d.series[0].data).toEqual([10, 11, 12])
    expect(d.max).toBe(100)
    const m = SOURCES.mem.gen(ctx({ history: hist(2) }))
    expect(m).toMatchObject({ kind: 'ts', unit: 'GB', max: 8, maxLabel: '还能用 6 GB' })
  })

  it('没有历史的服务器上 CPU、内存曲线不能选，时间范围只给服务端有的', () => {
    expect(sourceUsable('cpu', null)).toBe(false)
    expect(sourceUsable('cpu', { ...hist(0), ranges: [] })).toBe(false)
    expect(sourceUsable('cpu', hist(1))).toBe(true)
    expect(sourceUsable('appStatus', null)).toBe(true)
    expect(rangeTabs(hist(1))).toEqual(['1h', '24h'])
    expect(rangeTabs(null)).toEqual([])
    // 样稿里没有数据来源的图表不在可选列表里
    for (const k of ['net', 'heat', 'mirror', 'req', 'errors', 'uptime']) expect(SOURCES[k]).toBeUndefined()
  })

  it('磁盘按剩余空间提醒，已分配内存来自资源接口', () => {
    const d = SOURCES.disk.gen(ctx({ host: { available: true, disk_used: 38 * GB, disk_total: 40 * GB } }))
    expect(d).toMatchObject({ kind: 'value', value: 38, max: 40, sub: '剩 2 GB', tone: 'danger' })
    expect(SOURCES.disk.gen(ctx({ host: { available: false } }))).toMatchObject({ kind: 'none' })
    const a = SOURCES.alloc.gen(ctx({ resources: { memory: { host_mb: 8192, cap_mb: 6964, auto: true, allocated_mb: 3072 }, cpu: { host_milli: 4000, cap_milli: 4000, auto: true, max_app_milli: 1000 }, items: [], min_app_memory_mb: 64, min_app_cpu_milli: 100 } }))
    expect(a).toMatchObject({ kind: 'value', value: 3, max: 6.8 })
  })

  it('应用状态数和各应用资源只算在跑的', () => {
    const run = app({ name: 'web' })
    const stopped = app({ name: 'blog', desired: 'stopped', observed_state: 'stopped' })
    const st = SOURCES.appStatus.gen(ctx({ apps: [run, stopped] }))
    expect(st).toMatchObject({ kind: 'cat', status: true })
    if (st.kind === 'cat') expect(st.items.map((i) => i.value)).toEqual([1, 1, 0])
    // 部署中的应用（旧版本还在服务）不算已停止，单独算“处理中”；读不到状态的也单独算。
    const deploying = app({ name: 'api', latest: { id: 'd2', app: 'api', status: 'building' } as never, current_deployment: 'd1', observed_state: 'running' })
    const unknown = app({ name: 'db', current_deployment: 'd1', observed_state: 'unavailable' })
    const st2 = SOURCES.appStatus.gen(ctx({ apps: [run, stopped, deploying, unknown] }))
    if (st2.kind !== 'cat') throw new Error('kind')
    expect(st2.items.map((i) => [i.name, i.value])).toEqual([['运行中', 1], ['处理中', 1], ['已停止', 1], ['有问题', 0], ['读不到状态', 1]])
    const mem = SOURCES.appMem.gen(ctx({ apps: [run, stopped], metrics: { web: { app: 'web', available: true, observed_state: 'running', memory_usage_mb: 300.4 } } }))
    expect(mem).toMatchObject({ kind: 'cat', unit: 'MB', items: [{ name: 'web', value: 300.4 }] })
    expect(SOURCES.appCpu.gen(ctx({ apps: [] }))).toEqual({ kind: 'none', text: '还没有应用' })
    expect(SOURCES.appCpu.gen(ctx({ apps: [run] }))).toEqual({ kind: 'none', text: '正在读取…' }) // 指标还没取到
    expect(SOURCES.appCpu.gen(ctx({ apps: [stopped] }))).toEqual({ kind: 'none', text: '没有在运行的应用' })
  })

  it('图表方式只列适合这类数据的', () => {
    expect(typesFor({ kind: 'ts', unit: '%', max: 100, labels: [], series: [{ key: 'a', name: 'a', color: '', data: [] }] })).toEqual(['area', 'line', 'column', 'stat', 'meter', 'table'])
    expect(typesFor({ kind: 'cat', unit: '个', status: true, items: [] })[0]).toBe('kpi')
    expect(typesFor({ kind: 'value', unit: 'GB', value: 1, max: 2, label: '', sub: '' })).toEqual(['meter', 'stat'])
    expect(typesForSource('cpu', { kind: 'none', text: '' })).toContain('area') // 数据没到时菜单也不空
    expect(typesForSource('todo', { kind: 'none', text: '' })).toEqual(['list'])
  })
})

describe('需要你处理、最近发生的事', () => {
  it('部署失败、没在运行、目录断开、自检不通过、近一天失败的安装都列出来', () => {
    const failed = app({ name: 'shop', latest: { id: 'd2', app: 'shop', seq: 2, source_kind: 'image', source_ref: 'x', status: 'failed', created_at: '2026-11-06T11:00:00Z', diagnosis: { stage: 'health', code: 'DEP-HEALTH-01', message: '健康检查没通过' } } })
    const crashed = app({ name: 'blog', observed_state: 'missing' })
    const gone: AppView = { ...app({ name: 'agent' }), catalog_disconnected: true, catalog_origin: 'cat-demo', catalog_origin_name: '演示' }
    const t = task({ id: 'tk1', status: 'failed', target: 'hermes-agent', finished_at: '2026-11-06T10:00:00Z', fault: { code: 'CAT-INST-12', message: '镜像拉不下来' } })
    const old = task({ ...t, id: 'tk0', finished_at: '2026-11-01T10:00:00Z' })
    const items = todoItems(ctx({
      apps: [failed, crashed, gone], tasks: [t, old],
      checks: { os: 'linux', checked_at: '', ok: false, items: [{ ok: false, level: 'fail', text: '磁盘：数据目录 /var/lib/acornfox 所在分区只剩 2.0 GB', code: 'SETUP-DISK-01' }, { ok: true, level: 'ok', text: 'Docker：27.3 运行中' }] },
    }))
    expect(items.map((i) => i.title)).toEqual(['shop 部署失败', 'blog 没在运行', 'agent 的私有目录已断开', '磁盘 有问题', '安装 hermes-agent 失败'])
    expect(items[0].sub).toContain('DEP-HEALTH-01')
    expect(items[3]).toMatchObject({ href: '#runtime', sub: expect.stringContaining('SETUP-DISK-01') })
    expect(items[4].task).toEqual({ kind: 'app', id: 'hermes-agent', taskId: 'tk1' })
    expect(todoItems(ctx({ apps: [app({ name: 'web' })] }))).toEqual([])
  })

  it('部署记录和安装任务按时间倒序', () => {
    const items = eventItems(ctx({
      deployments: [
        { id: 'd1', app: 'web', seq: 1, source_kind: 'image', source_ref: 'nginx', status: 'retired', created_at: '2026-11-05T08:00:00Z', finished_at: '2026-11-05T08:01:00Z' },
        { id: 'd2', app: 'web', seq: 2, source_kind: 'image', source_ref: 'nginx', status: 'live', created_at: '2026-11-06T09:00:00Z', finished_at: '2026-11-06T09:01:00Z' },
      ],
      tasks: [task({ id: 'tk', status: 'succeeded', target: 'new-api', finished_at: '2026-11-06T10:00:00Z' })],
    }))
    expect(items.map((i) => i.title)).toEqual(['new-api 安装完成', 'web 第 2 版上线', 'web 第 1 版上线'])
    expect(items[2].time).toBe('昨天 ' + new Date('2026-11-05T08:01:00Z').toTimeString().slice(0, 5))
  })
})

describe('布局', () => {
  it('存在这个浏览器里，读到坏数据就用默认布局', () => {
    const mem: Record<string, string> = {}
    const st = { getItem: (k: string) => mem[k] ?? null, setItem: (k: string, v: string) => { mem[k] = v } }
    expect(loadLayout(st)).toBe(DEFAULT_LAYOUT)
    const l = [{ id: 'a', s: 'disk', t: 'meter', w: 3, h: 1 }]
    saveLayout(l, st)
    expect(JSON.parse(mem[LAYOUT_KEY])).toEqual(l)
    expect(loadLayout(st)).toEqual(l)
    mem[LAYOUT_KEY] = JSON.stringify([{ id: 'x', s: 'heat', t: 'heatmap', w: 8, h: 2 }]) // 样稿里有、这里没有数据来源的
    expect(loadLayout(st)).toBe(DEFAULT_LAYOUT)
    mem[LAYOUT_KEY] = '{坏'
    expect(loadLayout(st)).toBe(DEFAULT_LAYOUT)
    expect(loadLayout(null)).toBe(DEFAULT_LAYOUT)
    expect(() => saveLayout(l, { setItem: () => { throw new Error('quota') } })).not.toThrow()
  })

  it('前移、后移、拖到别的图表前面', () => {
    const l = [{ id: 'a', s: 'disk', t: 'meter', w: 3, h: 1 }, { id: 'b', s: 'cpu', t: 'area', w: 6, h: 2 }, { id: 'c', s: 'todo', t: 'list', w: 4, h: 2 }]
    expect(moveWidget(l, 'a', 1).map((x) => x.id)).toEqual(['b', 'a', 'c'])
    expect(moveWidget(l, 'a', -1).map((x) => x.id)).toEqual(['a', 'b', 'c'])
    expect(dropWidget(l, 'c', 'a').map((x) => x.id)).toEqual(['c', 'a', 'b'])
    expect(dropWidget(l, 'a', 'a')).toBe(l)
  })

  it('数字写法', () => {
    expect(fmtValue(12.345, '%')).toBe('12.3%')
    expect(fmtValue(1234, 'MB')).toBe('1,234 MB')
    expect(fmtValue(null, 'GB')).toBe('—')
  })
})
