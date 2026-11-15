import { describe, expect, it } from 'vitest'
import { render } from '@testing-library/react'
import { FirstLoginCard, FirstLogins } from './install-dialog'
import type { FirstLogin } from '../../lib/types'

// 没有登录信息的应用（只在内部网络、没有网页的数据库这类）：服务端给的字段可能是空列表，
// 旧的服务端给的是 null。两种都不能让页面出错，也不显示“首次登录”这一块。
describe('FirstLoginCard：没有登录信息的应用', () => {
  it('fields 是 null 时不出错、不显示', () => {
    const fl = { app: 'demo-sql', fields: null } as unknown as FirstLogin
    const { container } = render(<FirstLoginCard fl={fl} title="首次登录" />)
    expect(container.querySelector('[data-testid="first-login"]')).toBeNull()
  })
  it('fields 是空列表时不显示；有字段的照常显示', () => {
    const logins: FirstLogin[] = [
      { app: 'demo-sql', fields: [] },
      { app: 'demo-web', url: 'http://127.0.0.1:18810', fields: [{ label: '用户名', value: 'admin' }] },
    ]
    const { container } = render(<FirstLogins logins={logins} />)
    const cards = container.querySelectorAll('[data-testid="first-login"]')
    expect(cards.length).toBe(1)
    expect(cards[0].textContent).toContain('demo-web')
  })
})
