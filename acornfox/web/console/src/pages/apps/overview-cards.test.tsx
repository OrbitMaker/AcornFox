import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render } from '@testing-library/react'
import { ResourceCard, RunCard } from './overview-cards'
import { WhoCard } from './who-card'
import { app } from '@/test/fixtures'
import { ToastProvider } from '@/components/ui/toast'
import type { AppMetrics } from '@/lib/types'

// “谁能打开”卡片里的三档要读这台机器在不在公网上；这里只给它一个最小的状态，不起整个控制台。
vi.mock('@/state', () => ({ useConsole: () => ({ status: { on_public: true } }) }))

const started = (daysAgo: number) => new Date(Date.now() - daysAgo * 86400000).toISOString()

describe('运行卡片', () => {
  it('在运行：写连续运行多久，运行条 30 格，启动以来的亮着', () => {
    const a = app({ observed: { name: 'c', state: 'running', running: true, restart_count: 0, host_port: 18810, started_at: started(40) } })
    const { container } = render(<RunCard app={a} />)
    const card = container.querySelector('[data-testid="ov-run"]')!
    expect(card.textContent).toContain('40 天')
    expect(card.textContent).toContain('连续运行')
    expect(card.querySelectorAll('[data-up]').length).toBe(30)
    expect(card.querySelectorAll('[data-up="1"]').length).toBe(30)
  })
  it('没在运行：不亮，写没在运行', () => {
    const a = app({ desired: 'stopped', observed_state: 'stopped', observed: { name: 'c', state: 'exited', running: false, restart_count: 2, host_port: 0 } })
    const { container } = render(<RunCard app={a} />)
    expect(container.textContent).toContain('停了')
    expect(container.querySelectorAll('[data-up="1"]').length).toBe(0)
  })
})

describe('资源卡片', () => {
  const m: AppMetrics = { app: 'web', available: true, observed_state: 'running', cpu_percent: 25, memory_usage_mb: 128, memory_limit_mb: 512, network_rx_mb: 3, network_tx_mb: 1, pids: 13 }
  it('有数时两个圆环写占上限的百分比', () => {
    const { container } = render(<ResourceCard app={app()} metrics={m} />)
    expect(container.querySelector('[data-testid="ov-ring-CPU"]')!.textContent).toContain('25%')
    expect(container.querySelector('[data-testid="ov-ring-内存"]')!.textContent).toContain('25%')
  })
  it('没有数（应用停着、旧服务端）时不出错，写“—”', () => {
    const { container } = render(<ResourceCard app={app()} metrics={null} />)
    expect(container.querySelector('[data-testid="ov-ring-CPU"]')!.textContent).toContain('—')
    expect(container.querySelector('[data-testid="ov-ring-内存"] circle[stroke-dasharray]')).toBeNull()
  })
})

describe('“谁能打开”卡片', () => {
  const reload = async () => {}
  const withToast = (el: React.ReactElement) => <ToastProvider>{el}</ToastProvider>
  it('局域网应用：地址、复制、扫码，三档标出局域网、能点；没有“打开”（只在名片上）；点扫码出二维码', () => {
    const { container, getByText, queryByText, baseElement } = render(withToast(<WhoCard name="web" app={app({ url: 'http://192.168.1.20:18810', exposure: 'lan' })} reload={reload} />))
    const card = container.querySelector('[data-testid="ov-access"]')!
    expect(card.textContent).toContain('192.168.1.20:18810')
    const group = card.querySelector('[role="group"][aria-label^="谁能打开"]')!
    expect(Array.from(group.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['只本机', '局域网', '公网'])
    expect(group.querySelector('[aria-pressed="true"]')!.textContent).toBe('局域网')
    expect((group.querySelector('[aria-pressed="false"]') as HTMLButtonElement).disabled).toBe(false)
    expect(card.querySelector('[data-testid="ov-access-who"]')!.textContent).toContain('同一个网络里的电脑、手机都能打开')
    expect(queryByText('打开')).toBeNull()
    expect(card.textContent).not.toContain('浏览器')
    getByText('复制地址')
    fireEvent.click(getByText('手机扫码'))
    const qr = baseElement.querySelector('[data-testid="ov-qr"]')!
    expect(qr.querySelectorAll('rect').length).toBeGreaterThan(100)
  })
  it('只本机的 127.0.0.1：没有“扫码”（在别的设备上打不开），只留复制', () => {
    const { container, queryByText } = render(withToast(<WhoCard name="web" app={app({ url: 'http://127.0.0.1:18810', exposure: 'local' })} reload={reload} />))
    expect(container.querySelector('[aria-pressed="true"]')!.textContent).toBe('只本机')
    expect(queryByText('手机扫码')).toBeNull()
    expect(queryByText('复制地址')).not.toBeNull()
  })
  it('有首次登录信息：地址下面固定一行“第一次登录用”，账号和遮住的密码，带显示和复制', () => {
    const fl = { app: 'web', fields: [{ label: '账号', value: 'admin' }, { label: '密码', value: '******', secret: true }] }
    const { container } = render(withToast(<WhoCard name="web" app={app({ url: 'http://192.168.1.20:18810', exposure: 'lan' })} reload={reload} login={fl} />))
    const line = container.querySelector('[data-testid="ov-access"] [data-testid="first-login"]')!
    expect(line.textContent).toContain('第一次登录用：')
    expect(line.textContent).toContain('账号admin')
    expect(line.querySelector('[data-secret="true"]')!.textContent).toBe('******')
    expect(Array.from(line.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['显示', '复制'])
  })
  it('登录信息没有字段时不写“第一次登录用”这一行，别的照常', () => {
    const { container } = render(withToast(<WhoCard name="web" app={app({ url: 'http://192.168.1.20:18810', exposure: 'lan' })} reload={reload} login={{ app: 'web', fields: [] }} />))
    expect(container.querySelector('[data-testid="first-login"]')).toBeNull()
    expect(container.querySelector('[data-testid="ov-access-who"]')).not.toBeNull()
  })
  it('经 AFX 云这类不是三档之一的：不显示三档', () => {
    const { container } = render(withToast(<WhoCard name="web" app={app({ url: 'http://10.0.0.5:18810', exposure: 'afx_cloud' })} reload={reload} />))
    expect(container.querySelector('[data-testid="ov-access"]')).not.toBeNull()
    expect(container.querySelector('[aria-label^="谁能打开"]')).toBeNull()
  })
  it('没有网页的应用（给别的应用用的）：卡片只写一句它没有网页可以打开', () => {
    const { container } = render(withToast(<WhoCard name="web" app={app({ url: '', exposure: 'internal', internal_only: true })} reload={reload} />))
    expect(container.querySelector('[data-testid="ov-internal"]')!.textContent).toContain('没有网页可以打开')
  })
})
