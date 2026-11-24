// 选“谁能打开”：只本机、局域网、公网三档（叫法和每档的一句话在 lib/overview.ts 的 ACCESS_LEVELS，全站一处）。
// 装应用时用它（安装弹窗里，W3-5）；已装好的应用在概览的“谁能打开”卡片里改（pages/apps/access.tsx，要先确认、有撤销）。
// 这台机器不在公网上（家里的机器）：“公网”那一档灰着；选了公网，下面一行提醒先给应用设自己的密码。
import { useRef } from 'react'
import { onPublic } from '@/lib/display'
import { useIndicator } from '@/lib/hooks'
import { ACCESS_LEVELS } from '@/lib/overview'
import { cn } from '@/lib/utils'
import { useConsole } from '@/state'

export function ExposureChoice({ value, onChange, name }: { value: string; onChange: (v: string) => void; name: string }) {
  const { status } = useConsole()
  const inner = useRef<HTMLSpanElement>(null)
  const ind = useIndicator(inner, '[aria-pressed="true"]', [value])
  const publicOK = onPublic(status)
  const level = ACCESS_LEVELS.find((l) => l.key === value)
  return (
    <div className="flex flex-col gap-1.5" data-testid="expose-choice">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="text-xs font-medium leading-none">谁能打开</span>
        <span className="seg inline-flex h-ctl-sm shrink-0 rounded-md bg-muted p-0.5 text-xs" role="group" aria-label={'谁能打开 ' + name}>
          <span ref={inner} className="relative inline-flex h-full items-center">
            {ind.box && <span aria-hidden="true" className="seg-pill" data-ready={ind.ready ? '' : undefined} data-dir={ind.dir} style={{ left: ind.box.left, right: ind.box.right }} />}
            {ACCESS_LEVELS.map((l) => {
              const on = l.key === value
              const off = l.key === 'public' && !publicOK
              return (
                <button key={l.key} type="button" aria-pressed={on} data-on={on ? '1' : '0'} disabled={off}
                  title={off ? '这台机器不在公网上' : undefined}
                  onClick={() => onChange(l.key)}
                  className={cn('tap relative inline-flex h-full items-center whitespace-nowrap rounded-[6px] px-3 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-default',
                    on ? 'font-medium text-foreground' : off ? 'text-muted-foreground/50' : 'text-muted-foreground hover:text-foreground')}>
                  {l.text}
                </button>
              )
            })}
          </span>
        </span>
      </div>
      {level && <span className="text-2xs text-muted-foreground" data-testid="expose-who">{level.who}</span>}
      {value === 'public' && <span className="text-2xs text-warning" data-testid="expose-public-note">先确认已经给它设了自己的密码。</span>}
      {!publicOK && <span className="text-2xs text-muted-foreground">这台机器不在公网上</span>}
    </div>
  )
}
