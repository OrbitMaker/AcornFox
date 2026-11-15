import { describe, expect, it } from 'vitest'
import { fireEvent, render } from '@testing-library/react'
import { AccessCard, ResourceCard, RunCard } from './overview-cards'
import { app } from '@/test/fixtures'
import type { AppMetrics } from '@/lib/types'

const started = (daysAgo: number) => new Date(Date.now() - daysAgo * 86400000).toISOString()

describe('运行卡片', () => {
  it('在运行：写连续运行多久，运行条 30 格，启动以来的亮着', () => {
    const a = app({ observed: { name: 'c', state: 'running', running: true, restart_count: 0, host_port: 18810, started_at: started(40) } })
    const { container } = render(<RunCard app={a} />)
    const card = container.querySelector('[data-testid="ov-run"]')!
    expect(card.textContent).toContain('40 天')
    expect(card.textContent).toContain('连续运行')
    expect(card.textContent).toContain('重启 0 次')
    expect(card.querySelectorAll('[data-up]').length).toBe(30)
    expect(card.querySelectorAll('[data-up="1"]').length).toBe(30)
  })
  it('没在运行：不亮，写没在运行', () => {
    const a = app({ desired: 'stopped', observed_state: 'stopped', observed: { name: 'c', state: 'exited', running: false, restart_count: 2, host_port: 0 } })
    const { container } = render(<RunCard app={a} />)
    expect(container.textContent).toContain('没在运行')
    expect(container.textContent).toContain('已停止')
    expect(container.querySelectorAll('[data-up="1"]').length).toBe(0)
  })
})

describe('资源卡片', () => {
  const m: AppMetrics = { app: 'web', available: true, observed_state: 'running', cpu_percent: 25, memory_usage_mb: 128, memory_limit_mb: 512, network_rx_mb: 3, network_tx_mb: 1, pids: 13 }
  it('有数时两个圆环写占上限的百分比', () => {
    const { container } = render(<ResourceCard app={app()} metrics={m} />)
    expect(container.querySelector('[data-testid="ov-ring-CPU"]')!.textContent).toContain('25%')
    expect(container.querySelector('[data-testid="ov-ring-内存"]')!.textContent).toContain('25%')
    expect(container.textContent).toContain('13 个进程')
  })
  it('没有数（应用停着、旧服务端）时不出错，写“—”', () => {
    const { container } = render(<ResourceCard app={app()} metrics={null} />)
    expect(container.querySelector('[data-testid="ov-ring-CPU"]')!.textContent).toContain('—')
    expect(container.querySelector('[data-testid="ov-ring-内存"] circle[stroke-dasharray]')).toBeNull()
  })
})

describe('访问卡片', () => {
  const reload = async () => {}
  it('局域网应用：地址、复制、扫码，三档标出局域网、能点；没有“打开”（只在名片上）；点扫码出二维码', () => {
    const { container, getByText, queryByText, baseElement } = render(<AccessCard app={app({ url: 'http://192.168.1.20:18810', exposure: 'lan' })} reload={reload} />)
    const card = container.querySelector('[data-testid="ov-access"]')!
    expect(card.textContent).toContain('192.168.1.20:18810')
    const group = card.querySelector('[role="group"][aria-label="访问方式"]')!
    expect(Array.from(group.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['只本机', '局域网', '公网'])
    expect(group.querySelector('[aria-pressed="true"]')!.textContent).toBe('局域网')
    expect((group.querySelector('[aria-pressed="false"]') as HTMLButtonElement).disabled).toBe(false)
    expect(card.querySelector('[data-testid="ov-access-scheme"]')!.textContent).toBe('http · 没有绑定域名')
    expect(card.querySelector('[data-testid="ov-access-who"]')!.textContent).toContain('同一个局域网')
    expect(queryByText('打开')).toBeNull()
    expect(card.textContent).not.toContain('浏览器')
    getByText('复制地址')
    fireEvent.click(getByText('手机扫码'))
    const qr = baseElement.querySelector('[data-testid="ov-qr"]')!
    expect(qr.querySelectorAll('rect').length).toBeGreaterThan(100)
  })
  it('只本机的 127.0.0.1：没有“扫码”（在别的设备上打不开），只留复制', () => {
    const { container, queryByText } = render(<AccessCard app={app({ url: 'http://127.0.0.1:18810', exposure: 'local' })} reload={reload} />)
    expect(container.querySelector('[aria-pressed="true"]')!.textContent).toBe('只本机')
    expect(queryByText('手机扫码')).toBeNull()
    expect(queryByText('复制地址')).not.toBeNull()
  })
  it('有首次登录信息：下面那一行左边换成登录账号和遮住的密码，带显示和复制', () => {
    const fl = { app: 'web', fields: [{ label: '账号', value: 'admin' }, { label: '密码', value: '******', secret: true }] }
    const { container } = render(<AccessCard app={app({ url: 'http://192.168.1.20:18810', exposure: 'lan' })} reload={reload} login={fl} />)
    const line = container.querySelector('[data-testid="ov-access"] [data-testid="first-login"]')!
    expect(line.textContent).toContain('登录：')
    expect(line.textContent).toContain('账号admin')
    expect(line.querySelector('[data-secret="true"]')!.textContent).toBe('******')
    expect(Array.from(line.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['显示', '复制'])
    expect(container.querySelector('[data-testid="ov-access-scheme"]')).toBeNull()
  })
  it('登录信息没有字段（只在内部网络的这类）时照常写协议和域名', () => {
    const { container } = render(<AccessCard app={app({ url: 'http://192.168.1.20:18810', exposure: 'lan' })} reload={reload} login={{ app: 'web', fields: [] }} />)
    expect(container.querySelector('[data-testid="first-login"]')).toBeNull()
    expect(container.querySelector('[data-testid="ov-access-scheme"]')).not.toBeNull()
  })
  it('经 AFX 云这类不是三档之一的：不显示三档', () => {
    const { container } = render(<AccessCard app={app({ url: 'http://10.0.0.5:18810', exposure: 'afx_cloud' })} reload={reload} />)
    expect(container.querySelector('[data-testid="ov-access"]')).not.toBeNull()
    expect(container.querySelector('[aria-label="访问方式"]')).toBeNull()
  })
  it('没有地址的应用（只在内部网络）：不显示', () => {
    const { container } = render(<AccessCard app={app({ url: '', exposure: 'internal', internal_only: true })} reload={reload} />)
    expect(container.querySelector('[data-testid="ov-access"]')).toBeNull()
  })
})
