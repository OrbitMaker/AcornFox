// 改自 shadcn/ui 的 Progress（MIT，许可证见同目录 LICENSE）：加了警告、危险两种颜色；宽度用 style 属性（脚本写法，CSP 允许）。
import { cn } from '@/lib/utils'

export function Progress({ value, tone, className, label }: { value: number; tone?: 'warning' | 'danger'; className?: string; label?: string }) {
  const v = Math.max(0, Math.min(100, Number.isFinite(value) ? value : 0))
  return (
    <div role="progressbar" aria-valuenow={Math.round(v)} aria-valuemin={0} aria-valuemax={100} aria-label={label} className={cn('relative h-1.5 w-full overflow-hidden rounded-full bg-primary/10', className)}>
      <div className={cn('h-full rounded-full transition-all', tone === 'warning' ? 'bg-warning' : tone === 'danger' ? 'bg-danger' : 'bg-primary')} style={{ width: v + '%' }} />
    </div>
  )
}
