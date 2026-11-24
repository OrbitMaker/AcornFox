// Vitest 的公共设置：每个测试后清掉渲染的组件；jsdom 没有 scrollTo、scrollIntoView，换成空函数。
import { afterEach } from 'vitest'
import { cleanup, configure } from '@testing-library/react'

// 开发机忙的时候（几路测试同时跑）页面渲染慢，findBy/waitFor 默认只等 1 秒会偶发超时；放宽到 5 秒。
configure({ asyncUtilTimeout: 5000 })

window.scrollTo = () => {}
Element.prototype.scrollIntoView = () => {}
afterEach(() => cleanup())
