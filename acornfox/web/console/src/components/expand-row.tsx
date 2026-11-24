// 一行一项、点开才展开的设置行（应用的“设置”页、“设置 › 访问”“设置 › 服务”“设置 › 这台机器”共用）：
//   行的上下留白用“大小档位”里的 py-row；
//   左边名字，中间一句现状（可带一个小圆点表示正常或出错），右边可以放一个按钮，最右是展开箭头；展开后的内容缩进到现状那一列下面。
//   可选的 lead（名字前面的图标）和 fluid（名字不占固定宽度，整句话就是名字，现状跟在后面）是体检页那种“一句话一行”用的；
//   不给这两个时样子和原来一样。
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
  // 名字前面的图标（例如体检页每行的对勾）；展开内容跟着缩进到图标后面
  lead?: ReactNode
  // 名字不占固定宽度：整句话当名字，现状（可以为空）跟在后面
  fluid?: boolean
  // 手机宽度下名字在上、现状在下（名字长短不一、一列放不下的，例如“设置 › 服务”里的“PostgreSQL 数据库”）；电脑上照旧并排。样稿第 14 版
  stack?: boolean
  className?: string
}

export function ExpandRow({ id, name, summary, summaryTestId, tone = '', right, open, onToggle, children, wide, lead, fluid, stack, className }: ExpandRowProps) {
  return (
    <div className={className}>
      <div className="flex cursor-pointer items-center gap-3 px-4 py-row hover:bg-muted/40" onClick={onToggle}>
        <button type="button" aria-expanded={open} aria-controls={id} className={cn('flex min-w-0 flex-1 rounded-sm text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring', stack ? 'flex-col gap-0.5 sm:flex-row sm:items-center sm:gap-3' : 'items-center gap-3')}>
          {lead && <span className="flex size-4 shrink-0 items-center justify-center">{lead}</span>}
          <span className={cn('text-sm font-medium', fluid ? 'min-w-0 shrink truncate' : cn('shrink-0', stack ? (wide ? 'sm:w-36' : 'sm:w-28') : wide ? 'w-28 sm:w-36' : 'w-24 sm:w-28'))}>{name}</span>
          <span className={cn('flex min-w-0 items-center gap-1.5', stack ? 'w-full sm:w-auto sm:flex-1' : 'flex-1')}>
            {tone && <span className={cn('size-1.5 shrink-0 rounded-full', DOT[tone])} aria-hidden="true" />}
            <span className={cn('min-w-0 flex-1 truncate text-xs', tone === 'danger' ? 'text-danger' : 'text-muted-foreground')} data-testid={summaryTestId}>{summary}</span>
          </span>
        </button>
        {right && <div className="shrink-0" onClick={(e) => e.stopPropagation()}>{right}</div>}
        <ChevronRight className={cn('size-4 shrink-0 text-muted-foreground transition-transform duration-[var(--dur-sm-in)] ease-out-strong', open && 'rotate-90')} aria-hidden="true" />
      </div>
      {open && children && <div id={id} className={cn('px-4 pb-4 pt-0.5', fluid ? (lead ? 'pl-11' : '') : wide ? 'sm:pl-[172px]' : 'sm:pl-[140px]')}>{children}</div>}
    </div>
  )
}
