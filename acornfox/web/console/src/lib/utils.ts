import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

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
