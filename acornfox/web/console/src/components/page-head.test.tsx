import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render } from '@testing-library/react'
import { PageHead, PageSlots } from './bits'
import { crumbsFor } from './shell/header'

// 一级页面的名字只在顶栏写一次；PageHead 只管数量、状态和页面自己的按钮放在哪。
describe('PageHead：页面里不再有大标题', () => {
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); document.body.innerHTML = '' })

  const wide = (matches: boolean) => vi.stubGlobal('matchMedia', () => ({ matches, addEventListener: () => {}, removeEventListener: () => {} }))
  const slots = () => {
    const meta = document.createElement('span'); meta.id = 'slot-meta'
    const actions = document.createElement('div'); actions.id = 'slot-actions'
    document.body.append(meta, actions)
    return { meta, actions }
  }

  it('没有顶栏（单独渲染这一页）：数量和按钮在页面里排成一行，没有 h1', () => {
    const { container } = render(<PageHead meta="3 个应用"><button>更新目录</button></PageHead>)
    expect(container.querySelector('h1')).toBeNull()
    expect(container.textContent).toContain('3 个应用')
    expect(container.querySelector('button')!.textContent).toBe('更新目录')
  })

  it('窗口够宽、有顶栏：数量和按钮画进顶栏的两个位置，页面里不占地方', () => {
    wide(true)
    const s = slots()
    const { container } = render(<PageSlots.Provider value={s}><PageHead meta="3 个应用"><button>更新目录</button></PageHead></PageSlots.Provider>)
    expect(container.textContent).toBe('')
    expect(s.meta.textContent).toBe('3 个应用')
    expect(s.actions.querySelector('button')!.textContent).toBe('更新目录')
  })

  it('窗口窄：即使有顶栏也留在页面里（顶栏放不下）', () => {
    wide(false)
    const s = slots()
    const { container } = render(<PageSlots.Provider value={s}><PageHead meta="全部正常"><button>重新检查</button></PageHead></PageSlots.Provider>)
    expect(container.textContent).toContain('全部正常')
    expect(s.meta.textContent).toBe('')
    expect(s.actions.children.length).toBe(0)
  })

  it('什么都没有时不画', () => {
    const { container } = render(<PageHead />)
    expect(container.innerHTML).toBe('')
  })

  it('顶栏的位置：一级页面只有页面名一项，第二层才有路径', () => {
    expect(crumbsFor({ page: 'home' })).toEqual([['首页']])
    expect(crumbsFor({ page: 'app', id: 'hello' })).toEqual([['我的应用', '#apps'], ['hello']])
  })
})
