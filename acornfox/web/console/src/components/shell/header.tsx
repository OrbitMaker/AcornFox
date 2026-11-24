// 顶栏：面包屑、⌘K 搜索、需要你处理（铃铛）。
// 样稿第 14 版：二级页面的面包屑和一级页面的页名同一档（text-page）；铃铛里“需要你处理”下面多一段“刚做完的”
// （后台任务做完时人不在那一页才有，规则在 lib/jobs.ts）：有它时铃铛上是一个灰点（要处理的仍是琥珀点），点开又关上以后清掉。
// 样稿第 11 版：右上角的圆形头像去掉了（单机版只有一个管理员）。它菜单里的三样：深浅色在“设置 › 外观”，
// 设置在左侧菜单，退出登录挪到了“设置 › 访问”最下面一行。铃铛上的圆点是琥珀色。一级页面的页名用 text-page 一档。
// 样稿第 10 版（评估 B1、B4、X3、X5）：顶栏只放页面名和这一页唯一的主按钮（页面用 bits.tsx 的 PageHead 送进来），搜索全站只有这一个；
// 应用详情的面包屑用应用中心里的名字；“运行环境”改叫“体检”；铃铛里叫“需要你处理”，和首页那一块同一个来源、同样的话、同样的数量
// （pages/home/todo.tsx 的 useTodo）；账号菜单的标题是“这台机器”。
import { Bell, Check, ChevronRight, Menu, Search } from 'lucide-react'
import { Fragment, useEffect, useRef } from 'react'
import type { Route } from '@/lib/router'
import { findApp } from '@/lib/catalog'
import { WORDS, displayName } from '@/lib/display'
import { doneTime } from '@/lib/jobs'
import { go } from '@/lib/router'
import { useConsole } from '@/state'
import { TodoIcon, useTodo, useTodoGo } from '@/pages/home/todo'
import { Button } from '../ui/button'
import { Kbd } from '../ui/controls'
import { Dropdown, MenuLabel, MenuSep } from '../ui/menu'

export function crumbsFor(route: Route, itemName?: string): [string, string?][] {
  switch (route.page) {
    case 'home': return [['首页']]
    case 'apps': return [['我的应用']]
    case 'app': return [['我的应用', '#apps'], [route.id || '']]
    case 'store': return [['应用中心']]
    case 'item': return [['应用中心', '#store'], [itemName || route.id || '']]
    case 'solutions': return [['应用中心', '#store'], ['解决方案']]
    case 'solution': return [['应用中心', '#store'], ['解决方案', '#solutions'], [itemName || route.id || '']]
    case 'prefs': return [['设置']]
  }
}

// BellButton：铃铛。点开又关上以后把“刚做完的”清掉（seen）。
function BellButton({ open, toggle, todo, fresh, seen }: { open: boolean; toggle: () => void; todo: number; fresh: number; seen: () => void }) {
  const was = useRef(false)
  useEffect(() => {
    if (was.current && !open) seen()
    was.current = open
  }, [open, seen])
  const label = [todo ? `${WORDS.todo}：${todo} 件` : '', fresh ? `刚做完的：${fresh} 件` : ''].filter(Boolean).join('，') || WORDS.todo
  return (
    <Button variant="ghost" size="icon-sm" className="relative" onClick={toggle} aria-label={label} aria-haspopup="menu" aria-expanded={open}>
      <Bell className="size-icon" />
      {todo > 0 ? <span className="absolute right-1 top-1 size-1.5 rounded-full bg-warning" /> : fresh > 0 ? <span className="absolute right-1 top-1 size-1.5 rounded-full bg-muted-foreground" data-testid="bell-fresh" /> : null}
    </Button>
  )
}

// metaSlot、actionsSlot：一级页面把数量、状态和自己的按钮画到顶栏的这两个位置（见 bits.tsx 的 PageHead），窗口窄时这两个位置不显示。
export function Header({ route, openCmd, openNav, metaSlot, actionsSlot }: { route: Route; openCmd: () => void; openNav: () => void; metaSlot?: (el: HTMLElement | null) => void; actionsSlot?: (el: HTMLElement | null) => void }) {
  const { apps, catalog, done, clearDone } = useConsole()
  const mine = route.page === 'app' ? (apps || []).find((a) => a.name === route.id) : undefined
  const itemName = route.page === 'item' ? findApp(catalog, route.id || '', route.query?.catalog || 'public')?.name
    : route.page === 'solution' ? (catalog?.recipes || []).find((r) => r.id === route.id)?.title : undefined
  const crumbs = crumbsFor(route, itemName)
  if (mine) crumbs[crumbs.length - 1] = [displayName(catalog, mine)]
  const { items: todo } = useTodo()
  const openTodo = useTodoGo()
  return (
    <header className="sticky top-0 z-30 border-b bg-background/85 backdrop-blur">
      <div className="gutter flex h-bar items-center gap-2">
        <Button variant="ghost" size="icon-sm" className="-ml-1 md:hidden" onClick={openNav} aria-label="打开菜单"><Menu className="size-4" /></Button>
        <nav aria-label="位置" className="flex min-w-0 items-center gap-1.5 text-page">
          {crumbs.map(([label, href], i) => (
            <Fragment key={i}>
              {i > 0 && <ChevronRight className="hidden size-icon shrink-0 text-muted-foreground/70 sm:block" />}
              {href ? <a href={href} className="hidden text-muted-foreground hover:text-foreground sm:inline">{label}</a> : crumbs.length === 1 ? <h1 className="truncate font-medium">{label}</h1> : <span className="truncate font-medium">{label}</span>}
            </Fragment>
          ))}
        </nav>
        <span ref={metaSlot} className="hidden min-w-0 shrink truncate pl-1 text-xs text-muted-foreground lg:block" />
        <div className="ml-auto flex items-center gap-1.5">
          <div ref={actionsSlot} className="hidden items-center gap-2 pr-1 lg:flex" />
          <button type="button" onClick={openCmd} className="hidden h-ctl-sm w-48 items-center gap-2 rounded-md border bg-muted/40 px-2 text-xs text-muted-foreground transition-colors hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring lg:flex">
            <Search className="size-icon" /><span className="flex-1 text-left">搜索</span><Kbd>⌘K</Kbd>
          </button>
          <Button variant="ghost" size="icon-sm" className="lg:hidden" onClick={openCmd} aria-label="搜索"><Search className="size-icon" /></Button>
          <Dropdown width="w-80 max-w-[calc(100vw-2rem)]" trigger={(t, open) => <BellButton open={open} toggle={t} todo={todo.length} fresh={done.length} seen={clearDone} />}>
            <MenuLabel>{WORDS.todo}{todo.length > 0 && <span className="tnum font-normal"> · {todo.length}</span>}</MenuLabel>
            {todo.length === 0 && <div className="px-2 py-3 text-center text-xs text-muted-foreground">都处理好了</div>}
            {todo.map((it) => (
              <button key={it.key} type="button" onClick={() => openTodo(it)} className="flex min-h-ctl w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-accent" data-testid="bell-item">
                <TodoIcon it={it} small />
                <span className="min-w-0 flex-1 text-sm">{it.title}</span>
                <span className="shrink-0 text-2xs text-muted-foreground">{it.action || '去看看'}</span>
              </button>
            ))}
            {done.length > 0 && (
              <>
                <MenuSep />
                <MenuLabel>刚做完的</MenuLabel>
                {done.map((it) => (
                  <button key={it.key} type="button" onClick={() => it.href && go(it.href)} className="flex min-h-ctl w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition-colors hover:bg-accent" data-testid="bell-done">
                    <span className="flex size-6 shrink-0 items-center justify-center rounded-md border text-success"><Check className="size-icon" /></span>
                    <span className="min-w-0 flex-1 text-sm">{it.title}</span>
                    <span className="tnum shrink-0 text-2xs text-muted-foreground">{doneTime(it.at, Date.now())}</span>
                  </button>
                ))}
              </>
            )}
          </Dropdown>
        </div>
      </div>
    </header>
  )
}
