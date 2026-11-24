// 左侧菜单。手机宽度下收进抽屉（见 App.tsx）。
// 样稿第 11 版：菜单只有四项：首页、我的应用、应用中心、设置。体检并进了“设置 › 这台机器”（#prefs/checkup），不再是菜单项。
// 左下角只说“● 这台机器正常”或“● 这台机器有 N 件事要处理”（体检里要处理的项，和首页、铃铛同一个判断：pages/home/todo.tsx），
// 点了去设置里的体检。“要处理”的圆点都是琥珀色（红色只给删除和出错）。
import { useRef } from 'react'
import { House, LayoutGrid, Package, SlidersHorizontal, Stethoscope } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useIndicator } from '@/lib/hooks'
import type { Route } from '@/lib/router'
import { deriveStatus } from '@/lib/appstatus'
import { WORDS, needsYou } from '@/lib/display'
import { useConsole } from '@/state'
import { useTodo } from '@/pages/home/todo'
import { Logo } from '../bits'

export const NAV = [
  { key: 'home', label: '首页', icon: House },
  { key: 'apps', label: '我的应用', icon: LayoutGrid },
  { key: 'store', label: '应用中心', icon: Package },
  { key: 'prefs', label: '设置', icon: SlidersHorizontal },
] as const

// 体检在设置里的地址（左下角、首页、铃铛、搜索都指到这里）。
export const CHECKUP_HREF = '#prefs/checkup'
// 搜索面板“页面”那一组：四个菜单项，加上不在菜单里、但常被搜的体检。
export const PALETTE_PAGES = [...NAV.map(({ key, label, icon }) => ({ href: '#' + key, label, icon })), { href: CHECKUP_HREF, label: '体检', icon: Stethoscope }]

export function Sidebar({ route, onNavigate }: { route: Route; onNavigate?: () => void }) {
  const { apps, installing, status } = useConsole()
  const { machine } = useTodo()
  const bad = (apps || []).filter((a) => needsYou(deriveStatus(a, installing))).length
  const active = (k: string) => route.page === k || (k === 'apps' && route.page === 'app') || (k === 'store' && ['item', 'solutions', 'solution'].includes(route.page))
  const runnerBad = !!status && status.runner !== 'ok'
  const n = machine.length + (runnerBad ? 1 : 0)
  const tone = !status ? 'bg-muted-foreground' : n ? 'bg-warning' : 'bg-success'
  // 选中的那一块在菜单项之间滑过去（样稿第 12 版）
  const navRef = useRef<HTMLElement>(null)
  const ind = useIndicator(navRef, '[aria-current="page"]', [route.page])
  const line = !status ? '正在读取…' : n ? `${WORDS.machine}有 ${n} 件事要处理` : `${WORDS.machine}正常`
  return (
    <div className="flex h-full flex-col gap-1 bg-sidebar p-2.5">
      <a href="#home" onClick={onNavigate} className="mb-3 flex items-center gap-2 px-2 py-1.5">
        <Logo className="size-5" />
        <span className="text-sm font-semibold">AFX</span>
      </a>
      <nav ref={navRef} aria-label="主菜单" className="relative flex flex-col gap-1">
        {ind.box && <span aria-hidden="true" className="nav-pill" data-ready={ind.ready ? '' : undefined} data-dir={ind.dir} style={{ top: ind.box.top, bottom: ind.box.bottom }} />}
        {NAV.map(({ key, label, icon: Icon }) => (
          <a
            key={key}
            href={'#' + key}
            onClick={onNavigate}
            aria-current={active(key) ? 'page' : undefined}
            className={cn('relative flex h-nav items-center gap-2 rounded-md px-2 text-sm transition-colors', active(key) ? 'font-medium text-foreground' : 'text-muted-foreground hover:bg-accent/60 hover:text-foreground')}
          >
            <Icon className="size-icon" />
            <span className="flex-1">{label}</span>
            {key === 'apps' && bad > 0 && <span className="size-1.5 rounded-full bg-warning" aria-label={bad + ' 个应用要处理'} />}
          </a>
        ))}
      </nav>
      <a href={CHECKUP_HREF} onClick={onNavigate} className="mt-auto flex min-h-ctl-sm items-center gap-2 rounded-md px-2 py-1.5 text-2xs text-muted-foreground hover:bg-accent hover:text-foreground" data-testid="machine-health">
        <span className={cn('size-1.5 shrink-0 rounded-full', tone)} />
        <span className="flex-1 truncate">{line}</span>
      </a>
    </div>
  )
}
