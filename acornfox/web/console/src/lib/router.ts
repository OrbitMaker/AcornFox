// 地址栏 # 后面的路由，写法照控制台样稿：
//   #home、#apps、#store、#runtime、#prefs
//   #app-<应用名>[/<标签>]   我的应用里的一个应用
//   #item-<目录 id>[?catalog=<私有目录编号>]   应用中心里的一个应用（私有目录的带编号）
//   #prefs/<分区>            设置的一个分区
export type Page = 'home' | 'apps' | 'app' | 'store' | 'item' | 'runtime' | 'prefs'

export interface Route { page: Page; id?: string; tab?: string; query?: Record<string, string> }

export const APP_TABS = ['overview', 'versions', 'logs', 'config', 'data', 'resources', 'advanced'] as const
export type AppTab = (typeof APP_TABS)[number]

export function parseHash(hash: string): Route {
  const [raw, qs] = (hash || '').replace(/^#\/?/, '').split('?')
  const h = decodeURIComponent(raw)
  if (!h || h === 'home') return { page: 'home' }
  if (h.startsWith('app-')) {
    const [id, tab] = h.slice(4).split('/')
    const query = Object.fromEntries(new URLSearchParams(qs || ''))
    return { page: 'app', id, tab: (APP_TABS as readonly string[]).includes(tab) ? tab : 'overview', query }
  }
  if (h.startsWith('item-')) {
    const catalog = new URLSearchParams(qs || '').get('catalog')
    return { page: 'item', id: h.slice(5), ...(catalog && catalog !== 'public' ? { query: { catalog } } : {}) }
  }
  if (h === 'prefs' || h.startsWith('prefs/')) return { page: 'prefs', ...(h.length > 6 ? { id: h.slice(6) } : {}) }
  if (h === 'apps' || h === 'store' || h === 'runtime') return { page: h }
  return { page: 'home' }
}

export function appHref(name: string, tab?: string, query?: Record<string, string>): string {
  const qs = query ? new URLSearchParams(query).toString() : ''
  return '#app-' + encodeURIComponent(name) + (tab && tab !== 'overview' ? '/' + tab : '') + (qs ? '?' + qs : '')
}

export function itemHref(id: string, catalog?: string): string {
  return '#item-' + encodeURIComponent(id) + (catalog && catalog !== 'public' ? '?catalog=' + encodeURIComponent(catalog) : '')
}

export function go(href: string) {
  if (location.hash === href) window.dispatchEvent(new HashChangeEvent('hashchange'))
  else location.hash = href
}
