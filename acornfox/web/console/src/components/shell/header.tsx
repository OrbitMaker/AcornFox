// 顶栏：面包屑、⌘K 搜索、通知、账号菜单（深浅色、设置、退出）。
import { AlertTriangle, Bell, ChevronRight, LogOut, Menu, Moon, Search, Settings, Sun, User } from 'lucide-react'
import { Fragment } from 'react'
import { api } from '@/lib/api'
import { appHref, type Route } from '@/lib/router'
import { findApp } from '@/lib/catalog'
import { deriveStatus, failedDeployment } from '@/lib/appstatus'
import { effectiveTheme } from '@/lib/theme'
import { useConsole } from '@/state'
import { Button } from '../ui/button'
import { Kbd } from '../ui/controls'
import { Dropdown, MenuItem, MenuLabel, MenuSep } from '../ui/menu'

export function crumbsFor(route: Route, itemName?: string): [string, string?][] {
  switch (route.page) {
    case 'home': return [['首页']]
    case 'apps': return [['我的应用']]
    case 'app': return [['我的应用', '#apps'], [route.id || '']]
    case 'store': return [['应用中心']]
    case 'item': return [['应用中心', '#store'], [itemName || route.id || '']]
    case 'runtime': return [['运行环境']]
    case 'prefs': return [['设置']]
  }
}

export function Header({ route, openCmd, openNav, onLoggedOut }: { route: Route; openCmd: () => void; openNav: () => void; onLoggedOut: () => void }) {
  const { apps, installing, catalog, theme, setTheme } = useConsole()
  const itemName = route.page === 'item' ? findApp(catalog, route.id || '', route.query?.catalog || 'public')?.name : undefined
  const crumbs = crumbsFor(route, itemName)
  const problems = (apps || []).filter((a) => ['failed', 'crashed'].includes(deriveStatus(a, installing)))
  const eff = effectiveTheme(theme)
  const logout = async () => {
    try { await api.post('/v1/console/logout') } catch { /* 会话已经没了也算退出 */ }
    onLoggedOut()
  }
  return (
    <header className="sticky top-0 z-30 border-b bg-background/85 backdrop-blur">
      <div className="gutter flex h-12 items-center gap-2">
        <Button variant="ghost" size="icon-sm" className="-ml-1 md:hidden" onClick={openNav} aria-label="打开菜单"><Menu className="size-4" /></Button>
        <nav aria-label="位置" className="flex min-w-0 items-center gap-1 text-sm">
          {crumbs.map(([label, href], i) => (
            <Fragment key={i}>
              {i > 0 && <ChevronRight className="size-3 shrink-0 text-muted-foreground" />}
              {href ? <a href={href} className="hidden text-muted-foreground hover:text-foreground sm:inline">{label}</a> : <span className="truncate font-medium">{label}</span>}
            </Fragment>
          ))}
        </nav>
        <div className="ml-auto flex items-center gap-1.5">
          <button type="button" onClick={openCmd} className="hidden h-7 w-48 items-center gap-2 rounded-md border bg-muted/40 px-2 text-xs text-muted-foreground transition-colors hover:bg-accent lg:flex">
            <Search className="size-3.5" /><span className="flex-1 text-left">搜索</span><Kbd>⌘K</Kbd>
          </button>
          <Button variant="ghost" size="icon-sm" className="lg:hidden" onClick={openCmd} aria-label="搜索"><Search className="size-3.5" /></Button>
          <Dropdown width="w-72" trigger={(t) => (
            <Button variant="ghost" size="icon-sm" className="relative" onClick={t} aria-label={problems.length ? `通知：${problems.length} 个应用出了问题` : '通知'}>
              <Bell className="size-3.5" />
              {problems.length > 0 && <span className="absolute right-1 top-1 size-1.5 rounded-full bg-danger" />}
            </Button>
          )}>
            <MenuLabel>通知</MenuLabel>
            {problems.length === 0 && <div className="px-2 py-3 text-center text-xs text-muted-foreground">没有要处理的事</div>}
            {problems.map((a) => {
              const d = failedDeployment(a)
              return (
                <a key={a.name} href={appHref(a.name)} className="flex w-full gap-2 rounded-sm px-2 py-1.5 text-left hover:bg-accent">
                  <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-danger" />
                  <div className="min-w-0">
                    <div className="text-sm">{a.name} {d ? '部署失败' : '没在运行'}</div>
                    <div className="truncate text-2xs text-muted-foreground">{d?.diagnosis?.message || d?.failure_text || '容器停了，点进去看日志'}</div>
                  </div>
                </a>
              )
            })}
          </Dropdown>
          <Dropdown trigger={(t) => (
            <button type="button" onClick={t} className="ml-0.5 flex size-7 items-center justify-center rounded-full border bg-muted text-muted-foreground hover:bg-accent" aria-label="账号菜单">
              <User className="size-3.5" />
            </button>
          )}>
            <MenuLabel>本机控制台</MenuLabel>
            <MenuItem icon={eff === 'dark' ? <Sun /> : <Moon />} onClick={() => setTheme(eff === 'dark' ? 'light' : 'dark')}>{eff === 'dark' ? '浅色' : '深色'}</MenuItem>
            <MenuItem icon={<Settings />} href="#prefs">设置</MenuItem>
            <MenuSep />
            <MenuItem icon={<LogOut />} onClick={logout}>退出登录</MenuItem>
          </Dropdown>
        </div>
      </div>
    </header>
  )
}
