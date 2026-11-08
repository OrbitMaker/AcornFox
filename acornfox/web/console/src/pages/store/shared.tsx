import { Headset } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useConsole } from '@/state'
import { cn, safeHref } from '@/lib/utils'

// 找专人部署：GET /v1/catalog 带了 expert（url 是 http/https）才显示，价格也只按它显示。
// 服务端目前还不返回这个字段（目录格式里没有，接到哪里也还没定），所以现在不会出现；这里先按约定的格式读。
export function ExpertButton({ className, variant = 'outline' }: { className?: string; variant?: 'outline' | 'ghost' }) {
  const { catalog } = useConsole()
  const ex = catalog?.expert
  const url = safeHref(ex?.url)
  if (!ex || !url) return null
  const label = (ex.title || '找专人部署') + (ex.price_cny ? ` · ¥${ex.price_cny}` : '')
  return (
    <Button variant={variant} className={cn(className)} onClick={() => window.open(url, '_blank', 'noopener,noreferrer')}>
      <Headset className="size-3.5" />{label}
    </Button>
  )
}
