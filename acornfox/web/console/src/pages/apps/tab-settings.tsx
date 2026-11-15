// 设置：这个应用能改的都在这一页。一张卡片，一行一项（名字、现状摘要、展开箭头），点一行才展开，同一时间只展开一项：
//   谁能打开（三档、域名）、资源上限（内存、CPU）、数据（数据卷、附加服务）、更多（环境变量、端口和健康检查、命令行），
//   最下面是删除应用（红字，点“删除”走确认框）。
// 从地址进来时展开地址里的那一项：#app-<名字>/settings/<项>，以前的 config、data、resources、advanced 也认（见 lib/router.ts）。
// 每一项展开后的内容在 settings-*.tsx，保存、校验、提示和原来的四个标签页一样。
import { useEffect, useRef, useState, type ReactElement } from 'react'
import { ChevronRight } from 'lucide-react'
import type { SettingsSection } from '@/lib/router'
import type { AppView } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { AccessSection, DeleteAppButton, accessSummary } from './settings-access'
import { LimitsSection, limitsSummary } from './settings-limits'
import { DataSection, dataSummary } from './settings-data'
import { MoreSection, moreSummary } from './settings-more'
import type { TabProps } from './app-page'

type Expandable = Exclude<SettingsSection, 'delete'>

const ROWS: { key: Expandable; name: string; summary: (a: AppView) => string; Body: (p: TabProps) => ReactElement }[] = [
  { key: 'access', name: '谁能打开', summary: accessSummary, Body: AccessSection },
  { key: 'limits', name: '资源上限', summary: limitsSummary, Body: LimitsSection },
  { key: 'data', name: '数据', summary: dataSummary, Body: DataSection },
  { key: 'more', name: '更多', summary: moreSummary, Body: MoreSection },
]

const NAME = 'w-24 shrink-0 text-sm font-medium sm:w-28'

export function SettingsTab({ section, ...props }: TabProps & { section?: SettingsSection }) {
  const a = props.app
  const [open, setOpen] = useState<Expandable | ''>(section && section !== 'delete' ? section : '')
  const rowRefs = useRef<Partial<Record<SettingsSection, HTMLDivElement | null>>>({})
  // 地址里换了一项（例如从别处跳过来）：展开那一项并滚到它
  useEffect(() => {
    if (!section) return
    if (section !== 'delete') setOpen(section)
    window.setTimeout(() => rowRefs.current[section]?.scrollIntoView?.({ block: 'nearest' }), 50)
  }, [section])
  return (
    <Card className="overflow-hidden" data-testid="app-settings">
      {ROWS.map(({ key, name, summary, Body }, i) => {
        const on = open === key
        return (
          <div key={key} ref={(el) => { rowRefs.current[key] = el }} data-section={key} className={cn(i > 0 && 'border-t')}>
            <button type="button" aria-expanded={on} aria-controls={'set-' + key} onClick={() => setOpen(on ? '' : key)}
              className="flex w-full items-center gap-3 px-4 py-3 text-left hover:bg-muted/40">
              <span className={NAME}>{name}</span>
              <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" data-testid={'summary-' + key}>{summary(a)}</span>
              <ChevronRight className={cn('size-4 shrink-0 text-muted-foreground transition-transform', on && 'rotate-90')} aria-hidden="true" />
            </button>
            {on && <div id={'set-' + key} className="px-4 pb-4 pt-0.5 sm:pl-[140px]"><Body {...props} /></div>}
          </div>
        )
      })}
      <div ref={(el) => { rowRefs.current.delete = el }} data-section="delete" className="flex items-center gap-3 border-t px-4 py-2.5">
        <span className={cn(NAME, 'text-danger')}>删除应用</span>
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground">默认保留数据卷</span>
        <DeleteAppButton app={a} />
      </div>
    </Card>
  )
}
