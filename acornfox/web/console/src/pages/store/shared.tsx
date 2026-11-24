import { Headset } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useConsole } from '@/state'
import { cn, safeHref } from '@/lib/utils'

// 找专人部署：GET /v1/catalog 的 assist（经营系统发布的展示内容）里 visible 为 true、url 是 http/https 才显示；价格也只按它显示。
// url 是空串（经营系统还没填链接）就不显示按钮。老的 expert 写法（title、price_cny、url）也认。
export function ExpertButton({ className, variant = 'outline' }: { className?: string; variant?: 'outline' | 'ghost' }) {
  const { catalog } = useConsole()
  const assist = catalog?.assist
  const ex = assist ? (assist.visible ? { title: '', price_cny: assist.price_cny, url: assist.url } : null) : catalog?.expert
  const url = safeHref(ex?.url)
  if (!ex || !url) return null
  const label = (ex.title || '找专人部署') + (ex.price_cny ? ` · ¥${ex.price_cny}` : '')
  return (
    <Button variant={variant} className={cn(className)} onClick={() => window.open(url, '_blank', 'noopener,noreferrer')}>
      <Headset className="size-icon" />{label}
    </Button>
  )
}
