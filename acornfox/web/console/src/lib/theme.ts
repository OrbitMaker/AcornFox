// 深浅色：light、dark 或 system（跟随系统）。记在本浏览器，读写失败时跟随系统。
export type Theme = 'light' | 'dark' | 'system'
const KEY = 'afx-theme'

export function loadTheme(): Theme {
  try {
    const t = localStorage.getItem(KEY)
    if (t === 'light' || t === 'dark') return t
  } catch {
    /* 本机存储不可用 */
  }
  return 'system'
}

export function applyTheme(t: Theme) {
  const r = document.documentElement
  if (t === 'system') r.removeAttribute('data-theme')
  else r.setAttribute('data-theme', t)
  try {
    if (t === 'system') localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, t)
  } catch {
    /* 本机存储不可用 */
  }
}

export function effectiveTheme(t: Theme): 'light' | 'dark' {
  if (t !== 'system') return t
  return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}
