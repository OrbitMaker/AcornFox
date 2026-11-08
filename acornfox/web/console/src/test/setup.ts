// Vitest 的公共设置：每个测试后清掉渲染的组件；jsdom 没有 scrollTo、scrollIntoView，换成空函数。
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

window.scrollTo = () => {}
Element.prototype.scrollIntoView = () => {}
afterEach(() => cleanup())
