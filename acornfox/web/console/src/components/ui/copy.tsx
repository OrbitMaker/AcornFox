// 一行命令或文字，右边一个复制按钮。
import * as React from 'react'
import { Check, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'
import { useToast } from './toast'

export function CopyButton({ text, label = '复制', className }: { text: string; label?: string; className?: string }) {
  const [done, setDone] = React.useState(false)
  const toast = useToast()
  const copy = async () => {
    const ok = await copyText(text)
    if (!ok) {
      toast('没能复制', '浏览器不允许，请手动选中复制', 'error')
      return
    }
    setDone(true)
    window.setTimeout(() => setDone(false), 1200)
  }
  return (
    <button type="button" onClick={copy} className={cn('inline-flex size-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground', className)} aria-label={label} title={label}>
      {done ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
    </button>
  )
}

export function CopyLine({ text, className, prompt = true, label = '复制命令' }: { text: string; className?: string; prompt?: boolean; label?: string }) {
  return (
    <div className={cn('flex min-w-0 items-center gap-1.5 rounded-md border bg-muted/40 py-1 pl-2.5 pr-1', className)}>
      <code className="no-scrollbar min-w-0 flex-1 overflow-x-auto whitespace-nowrap text-xs">
        {prompt && <span className="select-none text-muted-foreground">$ </span>}
        {text}
      </code>
      <CopyButton text={text} label={label} />
    </div>
  )
}
