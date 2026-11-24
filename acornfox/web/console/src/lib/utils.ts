import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

// 大小档位里自己起的档名（tailwind.config.ts 的 fontSize、spacing）：合并类名时要认得它们，
// 不然 text-page 会被当成文字颜色、h-ctl 和 h-6 同时留下。加了新档名这里也要加。
const twMerge = extendTailwindMerge({
  extend: {
    theme: { spacing: ['ctl', 'ctl-sm', 'ctl-xs', 'ctl-lg', 'nav', 'bar', 'icon', 'row', 'tabs', 'tile-xs', 'tile-sm', 'tile-md', 'tile-lg', 'donut'] },
    classGroups: { 'font-size': [{ text: ['page', 'title', 'section', 'field'] }] },
  },
})

// cn 合并 Tailwind 类名（shadcn 的同名工具）。
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// safeHref 只放行 http:// 和 https:// 的绝对地址，其他（javascript:、data:、相对地址、写错的）一律返回 undefined，
// 调用方据此不显示链接。目录里的主页、视频、找专人地址来自网络同步，不能只靠 CSP 兜底。
export function safeHref(u?: string | null): string | undefined {
  if (!u) return undefined
  const s = u.trim()
  if (!/^https?:\/\//i.test(s)) return undefined
  try {
    const p = new URL(s)
    return (p.protocol === 'http:' || p.protocol === 'https:') && p.host ? s : undefined
  } catch {
    return undefined
  }
}
