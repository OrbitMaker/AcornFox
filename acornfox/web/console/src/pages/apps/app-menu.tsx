// 一个应用的“更多”菜单里的几项：我的应用的卡片和应用详情的名片用同一份，内容和顺序一样（评估 Y6）。
//   启动或停止、重启、日志、更新（自己部署的项目叫“版本”）、删除。
// 点菜单项不会顺带触发外面卡片的点击。启停和重启走 actions.ts，其余是跳到详情里对应的页签。
import type { MouseEvent } from 'react'
import { FileText, History, Play, RotateCw, Square, Trash2 } from 'lucide-react'
import type { AppStatus } from '@/lib/appstatus'
import { fromStore } from '@/lib/display'
import { appHref } from '@/lib/router'
import type { AppView } from '@/lib/types'
import { MenuItem, MenuSep } from '@/components/ui/menu'
import type { AppAction } from './actions'

export function AppMenuItems({ a, st, act }: { a: AppView; st: AppStatus; act: (x: AppAction) => void }) {
  const stop = (e: MouseEvent) => e.stopPropagation()
  const run = (x: AppAction) => (e: MouseEvent) => { stop(e); act(x) }
  return (
    <>
      {a.desired === 'stopped'
        ? <MenuItem icon={<Play />} onClick={run('start')}>启动</MenuItem>
        : st !== 'installing' && <MenuItem icon={<Square />} onClick={run('stop')}>停止</MenuItem>}
      {st === 'running' && <MenuItem icon={<RotateCw />} onClick={run('restart')}>重启</MenuItem>}
      <MenuItem icon={<FileText />} href={appHref(a.name, 'logs')} onClick={stop}>日志</MenuItem>
      <MenuItem icon={<History />} href={appHref(a.name, 'versions')} onClick={stop}>{fromStore(a) ? '更新' : '版本'}</MenuItem>
      <MenuSep />
      <MenuItem icon={<Trash2 />} danger href={appHref(a.name, 'settings/delete')} onClick={stop}>删除</MenuItem>
    </>
  )
}
