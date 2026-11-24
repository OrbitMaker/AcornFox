// 一步里的进度条：下载应用程序（经下载网关或从公共下载源拉）时按字节画，下面一行字写已下多少、多快、还要多久（lib/catalog.ts 的 progressLine）。
// 量不出进度时（下完在导入、还在挑下载线路、Docker 这会儿没报进度）条是来回走的。安装、更新、升级共用。
import { cn } from '@/lib/utils'
import { progressLine } from '@/lib/catalog'
import type { StepProgress } from '@/lib/types'

export function StepBar({ p, className }: { p: StepProgress; className?: string }) {
  const l = progressLine(p)
  return (
    <div className={cn('mt-1 flex flex-col gap-1', className)}>
      <div className="h-1.5 w-56 max-w-full overflow-hidden rounded-full bg-muted" role="progressbar" aria-label="下载进度"
        aria-valuemin={0} aria-valuemax={100} aria-valuenow={l.pct ?? undefined} data-testid="step-progress">
        {l.pct !== null
          ? <div className="h-full rounded-full bg-primary transition-[width] duration-700 ease-linear" style={{ width: l.pct + '%' }} />
          : <div className="h-full w-1/3 animate-pulse rounded-full bg-primary/70" />}
      </div>
      {l.text && <div className={cn('text-2xs', l.stalled ? 'text-warning' : 'text-muted-foreground')} data-testid="step-progress-text">{l.text}</div>}
    </div>
  )
}
