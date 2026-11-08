// 左侧菜单。手机宽度下收进抽屉（见 App.tsx）。
import { LayoutDashboard, LayoutGrid, Package, SlidersHorizontal, Wrench } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Route } from '@/lib/router'
import { deriveStatus } from '@/lib/appstatus'
import { useConsole } from '@/state'
import { Logo } from '../bits'

export const NAV = [
  { key: 'home', label: '首页', icon: LayoutDashboard },
  { key: 'apps', label: '我的应用', icon: LayoutGrid },
  { key: 'store', label: '应用中心', icon: Package },
  { key: 'runtime', label: '运行环境', icon: Wrench },
  { key: 'prefs', label: '设置', icon: SlidersHorizontal },
] as const

export function Sidebar({ route, onNavigate }: { route: Route; onNavigate?: () => void }) {
  const { apps, installing, session, status } = useConsole()
  const bad = (apps || []).filter((a) => ['failed', 'crashed'].includes(deriveStatus(a, installing))).length
  const active = (k: string) => route.page === k || (k === 'apps' && route.page === 'app') || (k === 'store' && route.page === 'item')
  const runnerOK = status?.runner === 'ok'
  return (
    <div className="flex h-full flex-col gap-1 bg-sidebar p-2.5">
      <a href="#home" onClick={onNavigate} className="mb-3 flex items-center gap-2 px-2 py-1.5">
        <Logo className="size-5" />
        <span className="text-sm font-semibold">AFX Console</span>
      </a>
      <nav aria-label="主菜单" className="flex flex-col gap-1">
        {NAV.map(({ key, label, icon: Icon }) => (
          <a
            key={key}
            href={'#' + key}
            onClick={onNavigate}
            aria-current={active(key) ? 'page' : undefined}
            className={cn('flex h-7 items-center gap-2 rounded-md px-2 text-sm transition-colors hover:bg-accent', active(key) ? 'bg-accent font-medium text-foreground' : 'text-muted-foreground hover:text-foreground')}
          >
            <Icon className="size-3.5" />
            <span className="flex-1">{label}</span>
            {key === 'apps' && bad > 0 && <span className="size-1.5 rounded-full bg-danger" aria-label={bad + ' 个应用出了问题'} />}
          </a>
        ))}
      </nav>
      <a href="#runtime" onClick={onNavigate} className="mt-auto flex items-center gap-2 rounded-md px-2 py-1.5 text-2xs text-muted-foreground hover:bg-accent" title={runnerOK ? 'Docker 正常' : '读不到 Docker'}>
        <span className={cn('size-1.5 rounded-full', status ? (runnerOK ? 'bg-success' : 'bg-danger') : 'bg-muted-foreground')} />
        <span className="flex-1 truncate">{session.server || '本机'}</span>
        {status?.docker_version && <span className="tnum">Docker {status.docker_version}</span>}
      </a>
    </div>
  )
}
