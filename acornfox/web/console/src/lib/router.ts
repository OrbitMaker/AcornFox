// 地址栏 # 后面的路由，写法照控制台样稿：
//   #home、#apps、#store、#runtime、#prefs
//   #app-<应用名>[/<标签>]   我的应用里的一个应用；标签是 overview、logs、versions、settings
//   #app-<应用名>/settings/<项>   设置里展开某一项（access、limits、data、more；delete 只滚到删除那一行）
//   以前的四个标签 config、data、resources、advanced 仍然认：跳到设置并展开对应的那一项
//   #item-<目录 id>[?catalog=<私有目录编号>]   应用中心里的一个应用（私有目录的带编号）
//   #prefs/<分区>            设置的一个分区
export type Page = 'home' | 'apps' | 'app' | 'store' | 'item' | 'runtime' | 'prefs'

// section：应用详情“设置”页里要展开的那一项（只在 tab 是 settings 时有）。
export interface Route { page: Page; id?: string; tab?: string; section?: string; query?: Record<string, string> }

export const APP_TABS = ['overview', 'logs', 'versions', 'settings'] as const
export type AppTab = (typeof APP_TABS)[number]

export const SETTINGS_SECTIONS = ['access', 'limits', 'data', 'more', 'delete'] as const
export type SettingsSection = (typeof SETTINGS_SECTIONS)[number]

// 以前的标签 → 设置里的哪一项（旧链接、书签照样能用）。
export const LEGACY_APP_TABS: Record<string, SettingsSection> = { config: 'more', data: 'data', resources: 'limits', advanced: 'access' }

export function parseHash(hash: string): Route {
  const [raw, qs] = (hash || '').replace(/^#\/?/, '').split('?')
  const h = decodeURIComponent(raw)
  if (!h || h === 'home') return { page: 'home' }
  if (h.startsWith('app-')) {
    const [id, tab, sub] = h.slice(4).split('/')
    const query = Object.fromEntries(new URLSearchParams(qs || ''))
    if (tab && Object.prototype.hasOwnProperty.call(LEGACY_APP_TABS, tab)) return { page: 'app', id, tab: 'settings', section: LEGACY_APP_TABS[tab], query }
    if (tab === 'settings' && (SETTINGS_SECTIONS as readonly string[]).includes(sub)) return { page: 'app', id, tab, section: sub, query }
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
