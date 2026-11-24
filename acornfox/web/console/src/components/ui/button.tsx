// 改自 shadcn/ui 的 Button（MIT，许可证见同目录 LICENSE）：尺寸按控制台样稿收紧。
// 高度用“大小档位”里的 ctl 一组（index.css）：默认 h-ctl、小号 h-ctl-sm、最小 h-ctl-xs、大号 h-ctl-lg，页面里不另写高度。
//
// 样稿第 12 版：有厚度的按钮。接口没变（variant、size 还是原来那几种），样子落到 index.css 的 .btn 一组类上：
//   default 主要     → .btn-face .btn-primary   深色面，面从主色（--primary）推出来，换主色不用改
//   destructive 危险 → .btn-face .btn-danger    同一套深色面，底色是 --destructive
//   outline 描边     → .btn-face .btn-key       白键
//   secondary 灰     → .btn-face .btn-key2      灰键
//   ghost 幽灵       → .btn-ghost               平时没有面，悬停淡灰，按下是凹下去的槽
//   link 链接        → .btn-text                一行字带细下划线；加 data-arrow 时后面有个箭头，悬停往右挪一点
// 四种状态：悬停（面亮一档、托起来的投影变大）、按下（下陷一条线的高度、面上下颠倒、高光变成内阴影）、
// 键盘聚焦（2px 的描边，outline，不占 box-shadow）、禁用（半透明、没有投影）。材质随字号缩放，“现在 / 放大一档”都对。
//
// 等待：loading 时字和宽度不变、不变灰、挡住第二次点击。前面有图标的，图标换成转圈；没有图标的，字先隐去、转圈在正中。
//   转圈在按下 300 毫秒后才出现、至少留 500 毫秒。turn：图标本身就表示这个动作（刷新、同步）时，从按下起转图标，至少转满一圈、停在整圈上。
import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'
import { useDelayedFlag } from '@/lib/hooks'

export const buttonVariants = cva(
  'btn inline-flex select-none items-center justify-center gap-1.5 whitespace-nowrap rounded-md text-sm font-medium [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        default: 'btn-face btn-primary',
        destructive: 'btn-face btn-danger',
        outline: 'btn-face btn-key',
        secondary: 'btn-face btn-key2',
        ghost: 'btn-ghost',
        link: 'btn-text',
      },
      size: {
        default: 'h-ctl px-3 [--btn-pl:0.75rem]',
        sm: 'h-ctl-sm px-2.5 text-xs [--btn-pl:0.625rem]',
        lg: 'h-ctl-lg px-4 [--btn-pl:1rem]',
        icon: 'size-ctl',
        'icon-sm': 'size-ctl-sm',
        'icon-xs': 'tap size-ctl-xs rounded',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  // 正在等这一下的结果：不变灰、不能再点；转圈 300 毫秒后才出现
  loading?: boolean
  // 图标本身就表示这个动作（刷新、同步）：loading 时转图标，不换成转圈
  turn?: boolean
}

function Spin() {
  return (
    <span className="btn-spin" aria-hidden="true">
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="size-icon animate-spin"><path d="M21 12a9 9 0 1 1-6.22-8.56" /></svg>
    </span>
  )
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(({ className, variant, size, type = 'button', loading = false, turn = false, children, onClick, onAnimationIteration, ...props }, ref) => {
  const spin = useDelayedFlag(loading && !turn, 300, 500)
  // turn：从按下起转；loading 结束后等这一圈转完（animationiteration）再停。
  const [turning, setTurning] = React.useState(false)
  if (turn && loading && !turning) setTurning(true)
  return (
    <button
      ref={ref}
      type={type}
      className={cn(buttonVariants({ variant, size }), className)}
      aria-busy={loading || undefined}
      data-loading={spin ? '' : undefined}
      data-turning={turning ? '' : undefined}
      onClick={loading ? undefined : onClick}
      onAnimationIteration={(e) => { if (turning && !loading) setTurning(false); onAnimationIteration?.(e) }}
      {...props}
    >
      {children}
      {spin && <Spin />}
    </button>
  )
})
Button.displayName = 'Button'
