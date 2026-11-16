// 一行一项、点开才展开的设置行（应用的“设置”页和“设置 › 访问”共用）：
//   左边名字，中间一句现状（可带一个小圆点表示正常或出错），右边可以放一个按钮，最右是展开箭头；展开后的内容缩进到现状那一列下面。
// 整行都能点（点按钮、点箭头都算）；键盘用名字和现状那一块（一个真的 button，带 aria-expanded）。
// 右边的按钮自己处理点击，不会顺带把这一行展开或收起。同一时间只展开一项由使用方管（传 open、onToggle）。
import type { ReactNode } from 'react'
import { ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'

export type RowTone = 'success' | 'warning' | 'danger' | ''

const DOT: Record<Exclude<RowTone, ''>, string> = { success: 'bg-success', warning: 'bg-warning', danger: 'bg-danger' }

export interface ExpandRowProps {
  // 展开内容的 id（按钮的 aria-controls）
  id: string
  name: ReactNode
  summary: ReactNode
  summaryTestId?: string
  tone?: RowTone
  // 现状右边的按钮（例如“开启”“配置”）；展开时一般不再给
  right?: ReactNode
  open: boolean
  onToggle: () => void
  children?: ReactNode
  // 名字一列宽一些（名字长的，例如“局域网里打开控制台”）
  wide?: boolean
  className?: string
}

export function ExpandRow({ id, name, summary, summaryTestId, tone = '', right, open, onToggle, children, wide, className }: ExpandRowProps) {
  return (
    <div className={className}>
      <div className="flex cursor-pointer items-center gap-3 px-4 py-3 hover:bg-muted/40" onClick={onToggle}>
        <button type="button" aria-expanded={open} aria-controls={id} className="flex min-w-0 flex-1 items-center gap-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded-sm">
          <span className={cn('shrink-0 text-sm font-medium', wide ? 'w-28 sm:w-36' : 'w-24 sm:w-28')}>{name}</span>
          <span className="flex min-w-0 flex-1 items-center gap-1.5">
            {tone && <span className={cn('size-1.5 shrink-0 rounded-full', DOT[tone])} aria-hidden="true" />}
            <span className={cn('min-w-0 flex-1 truncate text-xs', tone === 'danger' ? 'text-danger' : 'text-muted-foreground')} data-testid={summaryTestId}>{summary}</span>
          </span>
        </button>
        {right && <div className="shrink-0" onClick={(e) => e.stopPropagation()}>{right}</div>}
        <ChevronRight className={cn('size-4 shrink-0 text-muted-foreground transition-transform', open && 'rotate-90')} aria-hidden="true" />
      </div>
      {open && children && <div id={id} className={cn('px-4 pb-4 pt-0.5', wide ? 'sm:pl-[172px]' : 'sm:pl-[140px]')}>{children}</div>}
    </div>
  )
}
