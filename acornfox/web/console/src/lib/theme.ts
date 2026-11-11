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

// 主色：按钮、开关、进度条这些“主要动作”的颜色。四种，照控制台样稿：neutral（中性，默认）、blue、green、orange。
// 记在本浏览器（afx-accent），只影响这台浏览器看到的样子；读写失败时用中性。颜色值在 index.css 的 [data-accent]。
export type Accent = 'neutral' | 'blue' | 'green' | 'orange'
const ACCENT_KEY = 'afx-accent'
export const ACCENTS: [Accent, string, string][] = [
  ['neutral', '中性', 'hsl(240 5.9% 10%)'], ['blue', '蓝', 'hsl(221 83% 53%)'], ['green', '绿', 'hsl(142 76% 36%)'], ['orange', '橙', 'hsl(24.6 95% 50%)'],
]

export function loadAccent(): Accent {
  try {
    const a = localStorage.getItem(ACCENT_KEY)
    if (a === 'blue' || a === 'green' || a === 'orange') return a
  } catch {
    /* 本机存储不可用 */
  }
  return 'neutral'
}

export function applyAccent(a: Accent) {
  const r = document.documentElement
  if (a === 'neutral') r.removeAttribute('data-accent')
  else r.setAttribute('data-accent', a)
  try {
    if (a === 'neutral') localStorage.removeItem(ACCENT_KEY)
    else localStorage.setItem(ACCENT_KEY, a)
  } catch {
    /* 本机存储不可用 */
  }
}
