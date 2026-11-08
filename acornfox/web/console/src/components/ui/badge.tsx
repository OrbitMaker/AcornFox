// 改自 shadcn/ui 的 Badge（MIT，许可证见同目录 LICENSE）：加了成功、警告、危险三种颜色。
import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

export const badgeVariants = cva('inline-flex items-center gap-1 whitespace-nowrap rounded border px-1.5 py-px text-2xs font-medium', {
  variants: {
    variant: {
      default: 'border-transparent bg-primary text-primary-foreground',
      secondary: 'border-transparent bg-secondary text-secondary-foreground',
      outline: 'text-muted-foreground',
      success: 'border-success/25 bg-success/10 text-success',
      warning: 'border-warning/25 bg-warning/10 text-warning',
      danger: 'border-danger/25 bg-danger/10 text-danger',
    },
  },
  defaultVariants: { variant: 'secondary' },
})

export interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement>, VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />
}
