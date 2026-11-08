// 改自 shadcn/ui 的 Card（MIT，许可证见同目录 LICENSE）。
import * as React from 'react'
import { cn } from '@/lib/utils'

export const Card = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(({ className, ...props }, ref) => (
  <div ref={ref} className={cn('rounded-lg border bg-card text-card-foreground', className)} {...props} />
))
Card.displayName = 'Card'
