// 设置：这个应用能改的都在这一页。一张卡片，一行一项（名字、现状一句话、展开箭头），点一行才展开，同一时间只展开一项。
// 按两种应用分开（样稿第 10 版，评估 R5、H2、H7、H8）：
//   从应用中心装的应用（lib/display.ts 的 fromStore）：域名、安装设置、最多能用多少内存、数据、备份、删除；
//   开发者自己部署的项目：域名、最多能用多少内存、数据（另有附加服务）、备份、开发者设置（环境变量、端口和健康检查、命令行）、删除。
//   “备份”一行服务端不支持备份时不显示（pages/apps/settings-backup.tsx）。
//   数据库这类只在内部网络的应用没有“域名”那一行。“谁能打开”的三档只在概览最上面的卡片里，这里不再有一套。
//   删除：被别的应用用着时（lib/app-links.ts 的 usedBy）按钮灰着，旁边写“n8n 正在用它”。
// 地址里的项名没改（#app-<名字>/settings/<项>：access 是域名、vars、limits、data、more 是开发者设置、delete），
// 以前的 config、data、resources、advanced 也认（见 lib/router.ts）。
// 行的样子在 components/expand-row.tsx（“设置 › 访问”也用它），名字一列用宽的那一档（“最多能用多少内存”放得下）；每一项展开后的内容在 settings-*.tsx。
import { Fragment, useEffect, useRef, useState, type ReactElement } from 'react'
import { useAppLinks } from '@/lib/app-links'
import { displayName, fromStore, useAppNames } from '@/lib/display'
import type { SettingsSection } from '@/lib/router'
import type { AppView } from '@/lib/types'
import { useConsole } from '@/state'
import { cn } from '@/lib/utils'
import { Card } from '@/components/ui/card'
import { ExpandRow } from '@/components/expand-row'
import { AccessSection, DeleteAppButton, accessSummary } from './settings-access'
import { LimitsSection, limitsSummary } from './settings-limits'
import { DataSection, dataSummary } from './settings-data'
import { MoreSection, moreSummary } from './settings-more'
import { VarsRow, useAppVars } from './settings-vars'
import { BackupRow } from './settings-backup'
import { useReapply } from './reapply'
import type { TabProps } from './app-page'

type Expandable = Exclude<SettingsSection, 'delete'>
type RowKey = Exclude<Expandable, 'vars' | 'backup'>

interface Row { key: RowKey; name: string; summary: (a: AppView) => string; Body: (p: TabProps) => ReactElement }

const ROW: Record<RowKey, Row> = {
  access: { key: 'access', name: '域名', summary: accessSummary, Body: AccessSection },
  limits: { key: 'limits', name: '最多能用多少内存', summary: limitsSummary, Body: LimitsSection },
  data: { key: 'data', name: '数据', summary: dataSummary, Body: DataSection },
  more: { key: 'more', name: '开发者设置', summary: moreSummary, Body: MoreSection },
}

// rowsFor：这个应用显示哪几行（“安装设置”不在这里，它的现状要先读接口，单独画在第一行下面）。
export function rowsFor(a: AppView): Row[] {
  const keys: RowKey[] = [...(a.internal_only ? [] : ['access' as const]), 'limits', 'data', ...(fromStore(a) || a.internal_only ? [] : ['more' as const])]
  return keys.map((k) => ROW[k])
}

export function SettingsTab({ section, ...props }: TabProps & { section?: SettingsSection }) {
  const a = props.app
  const [open, setOpen] = useState<Expandable | ''>(section && section !== 'delete' ? section : '')
  const rowRefs = useRef<Partial<Record<SettingsSection, HTMLDivElement | null>>>({})
  const { catalog } = useConsole()
  const label = displayName(catalog, a)
  const vars = useAppVars(a)
  // 改安装设置、让它跟上：先读计划，同意了再做；做完（成败）重新读一遍安装设置和应用
  const flow = useReapply(a.name, label, async () => { vars.refresh(); await props.reload() })
  const links = useAppLinks(a.name)
  const name = useAppNames()
  const usedBy = [...new Set((links?.usedBy || []).map((b) => name(b.app)))]
  const rows = rowsFor(a)
  // 地址里换了一项（例如从别处跳过来）：展开那一项并滚到它
  useEffect(() => {
    if (!section) return
    if (section !== 'delete') setOpen(section)
    window.setTimeout(() => rowRefs.current[section]?.scrollIntoView?.({ block: 'nearest' }), 50)
  }, [section])
  // “安装设置”这一行要等接口回来才画出来：从地址直接进来时，画出来以后再滚到它
  const varsShown = !!vars.vars
  useEffect(() => {
    if (section === 'vars' && varsShown) rowRefs.current.vars?.scrollIntoView?.({ block: 'nearest' })
  }, [section, varsShown])
  const varsRow = (vars.vars || vars.error || vars.loading) && (
    <div ref={(el) => { rowRefs.current.vars = el }} data-section="vars" className={cn(rows[0]?.key === 'access' && 'border-t')}>
      <VarsRow wide app={a.name} vars={vars.vars} error={vars.error} loading={vars.loading} open={open === 'vars'} onToggle={() => setOpen(open === 'vars' ? '' : 'vars')} flow={flow} />
    </div>
  )
  return (
    <Card className="overflow-hidden" data-testid="app-settings">
      {/* 没有“域名”那一行（数据库这类）时“安装设置”是第一行 */}
      {rows[0]?.key !== 'access' && varsRow}
      {rows.map(({ key, name: rowName, summary, Body }, i) => {
        const on = open === key
        return (
          <Fragment key={key}>
            <div ref={(el) => { rowRefs.current[key] = el }} data-section={key} className={cn((i > 0 || (rows[0]?.key !== 'access' && !!varsRow)) && 'border-t')}>
              <ExpandRow id={'set-' + key} name={rowName} summary={summary(a)} summaryTestId={'summary-' + key} open={on} onToggle={() => setOpen(on ? '' : key)} wide>
                <Body {...props} />
              </ExpandRow>
            </div>
            {key === 'access' && varsRow}
          </Fragment>
        )
      })}
      <BackupRow app={a} label={label} reload={props.reload} open={open === 'backup'} onToggle={() => setOpen(open === 'backup' ? '' : 'backup')} wide rowRef={(el) => { rowRefs.current.backup = el }} />
      <div ref={(el) => { rowRefs.current.delete = el }} data-section="delete" className="flex items-center gap-3 border-t px-4 py-2.5">
        <span className="w-28 shrink-0 text-sm font-medium text-danger sm:w-36">删除应用</span>
        <span className="min-w-0 flex-1 truncate text-xs text-muted-foreground" data-testid="delete-note">{usedBy.length ? usedBy.join('、') + ' 正在用它' : '默认留着数据'}</span>
        <DeleteAppButton app={a} blockedBy={usedBy} />
      </div>
    </Card>
  )
}
