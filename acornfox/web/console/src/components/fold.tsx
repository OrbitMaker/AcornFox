// 样稿第 10 版：给小白的页面上不直接摆命令行和故障编号（评估 X4）。全站只有这两种收法，样子一样：
//   <CliFold>：默认收起的“用命令行”，里面才是命令（CopyLine）和给懂的人看的说明；
//   <AskAI>：“复制给 AI 工作台”按钮，故障编号、任务号、诊断都放进它复制的那段文字里，页面上不单独显示。
// “设置 › 开发者与 AI”那一页和应用页的“技术信息”照旧，不用这两个。
import type { ReactNode } from 'react'
import { ChevronRight, Sparkles, Terminal } from 'lucide-react'
import { cn } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'
import { Button } from './ui/button'
import { useToast } from './ui/toast'

export function Fold({ title, icon, children, className, defaultOpen }: { title: ReactNode; icon?: ReactNode; children: ReactNode; className?: string; defaultOpen?: boolean }) {
  return (
    <details className={cn('group/fold text-xs', className)} open={defaultOpen}>
      <summary className="tap-h inline-flex cursor-pointer select-none list-none items-center gap-1 rounded-sm text-2xs text-muted-foreground hover:text-foreground [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-3 transition-transform duration-[var(--dur-sm-in)] ease-out-strong group-open/fold:rotate-90" aria-hidden="true" />
        {icon}
        {title}
      </summary>
      <div className="mt-2 flex flex-col gap-2">{children}</div>
    </details>
  )
}

// CliFold：默认收起的“用命令行”。title 可以换成更具体的一句（例如“我在外面，用命令行打开”）。
export function CliFold({ title = '用命令行', children, className }: { title?: ReactNode; children: ReactNode; className?: string }) {
  return <Fold title={title} icon={<Terminal className="size-3" aria-hidden="true" />} className={className}>{children}</Fold>
}

// AskAI：把一段文字（现象、编号、日志摘录）复制给 AI 工作台。text 可以是现取的（函数）。
// done：复制成功后那条提示的标题和说明，不给就是“已复制 / 粘贴给 AI 工作台，它会接着查”。
export function AskAI({ text, label = '复制给 AI 工作台', size = 'sm', variant = 'outline', className, done }: {
  text: string | (() => string | Promise<string>)
  label?: ReactNode
  done?: [string, string]
  size?: 'sm' | 'default'
  variant?: 'outline' | 'ghost' | 'default'
  className?: string
}) {
  const toast = useToast()
  const run = async () => {
    const t = typeof text === 'function' ? await text() : text
    ;(await copyText(t)) ? toast(done?.[0] ?? '已复制', done?.[1] ?? '粘贴给 AI 工作台，它会接着查') : toast('没能复制', '浏览器不允许，请手动选中复制', 'error')
  }
  return <Button size={size} variant={variant} className={className} onClick={run}><Sparkles className="size-icon" />{label}</Button>
}

// EmptyState：没东西时全站一个样子（评估 X7）：一句话说现在的情况、一个按钮说下一步、不放命令。
// action 给一个 <a className={buttonVariants(...)}> 或 <Button>；实在没有下一步时可以不给。
export function EmptyState({ title, action, className }: { title: ReactNode; action?: ReactNode; className?: string }) {
  return (
    <div className={cn('flex flex-col items-center gap-3 rounded-lg border border-dashed px-6 py-10 text-center', className)} data-testid="empty-state">
      <p className="text-sm text-muted-foreground">{title}</p>
      {action}
    </div>
  )
}
