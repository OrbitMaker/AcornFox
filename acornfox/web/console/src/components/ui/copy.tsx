// 一行命令或文字，右边一个复制按钮。
// 样稿第 14 版：没能复制时，出错的话留在那一行下面（那一行的字就在眼前，可以手动选中）；
// 单独用 CopyButton、旁边没有地方写的，仍是一条提示。
import * as React from 'react'
import { Check, Copy } from 'lucide-react'
import { cn } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'
import { useToast } from './toast'

// onFail：没能复制时调（使用方把出错的话写在这一项旁边）；没给就弹一条提示。onOk：复制成功时调（好把上一次的出错收掉）。
export function CopyButton({ text, label = '复制', className, onFail, onOk }: { text: string; label?: string; className?: string; onFail?: () => void; onOk?: () => void }) {
  const [done, setDone] = React.useState(false)
  const toast = useToast()
  const copy = async () => {
    const ok = await copyText(text)
    if (!ok) {
      if (onFail) onFail()
      else toast('没能复制', '浏览器不允许，请手动选中复制', 'error')
      return
    }
    onOk?.()
    setDone(true)
    window.setTimeout(() => setDone(false), 1200)
  }
  return (
    <button type="button" onClick={copy} className={cn('tap inline-flex size-ctl-xs shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground', className)} aria-label={label} title={label}>
      {done ? <Check className="size-icon" /> : <Copy className="size-icon" />}
    </button>
  )
}

export function CopyLine({ text, className, prompt = true, label = '复制命令' }: { text: string; className?: string; prompt?: boolean; label?: string }) {
  const [failed, setFailed] = React.useState(false)
  return (
    <div className={cn('min-w-0', className)}>
      <div className="flex min-w-0 items-center gap-1.5 rounded-md border bg-muted/40 py-1 pl-2.5 pr-1">
        <code className="no-scrollbar min-w-0 flex-1 select-all overflow-x-auto whitespace-nowrap text-xs">
          {prompt && <span className="select-none text-muted-foreground">$ </span>}
          {text}
        </code>
        <CopyButton text={text} label={label} onFail={() => setFailed(true)} onOk={() => setFailed(false)} />
      </div>
      {failed && <p className="mt-1 text-xs text-danger" role="alert">没能复制，请手动选中这一行</p>}
    </div>
  )
}
