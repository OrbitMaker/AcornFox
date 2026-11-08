// 控制台外壳：读会话 → 左侧菜单 + 顶栏 + 页面；⌘K 命令面板、安装弹窗挂在这一层。
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type ApiError } from './lib/api'
import { parseHash, type AppTab, type Route } from './lib/router'
import type { Session } from './lib/types'
import { ConsoleProvider, type InstallTarget } from './state'
import { Header } from './components/shell/header'
import { Sidebar } from './components/shell/sidebar'
import { CommandPalette } from './components/shell/command-palette'
import { Logo } from './components/bits'
import { Button } from './components/ui/button'
import { Spinner } from './components/ui/controls'
import { Sheet } from './components/ui/dialog'
import { ToastProvider } from './components/ui/toast'
import { DashboardPage } from './pages/home/dashboard-page'
import { RuntimePage } from './pages/runtime/runtime-page'
import { PrefsPage } from './pages/prefs/prefs-page'
import { StorePage } from './pages/store/store-page'
import { ItemPage } from './pages/store/item-page'
import { InstallDialog } from './pages/store/install-dialog'
import { AppsPage } from './pages/apps/apps-page'
import { AppPage } from './pages/apps/app-page'

type Gate = { state: 'loading' } | { state: 'ok'; session: Session } | { state: 'out'; reason: 'expired' | 'logout' | 'error'; message?: string }

export function App() {
  const [gate, setGate] = useState<Gate>({ state: 'loading' })
  useEffect(() => {
    api.onSessionExpired(() => setGate({ state: 'out', reason: 'expired' }))
    api.session()
      .then((s) => setGate({ state: 'ok', session: s }))
      .catch((e: ApiError) => setGate(e.status === 401 ? { state: 'out', reason: 'expired' } : { state: 'out', reason: 'error', message: e.message }))
  }, [])
  if (gate.state === 'loading') return <div className="flex h-full items-center justify-center gap-2 text-xs text-muted-foreground"><Spinner />正在连接…</div>
  if (gate.state === 'out') return <SignedOut reason={gate.reason} message={gate.message} />
  return (
    <ToastProvider>
      <Shell session={gate.session} onLoggedOut={() => setGate({ state: 'out', reason: 'logout' })} />
    </ToastProvider>
  )
}

function SignedOut({ reason, message }: { reason: 'expired' | 'logout' | 'error'; message?: string }) {
  const title = reason === 'logout' ? '已退出登录' : reason === 'expired' ? '需要重新登录' : '连不上控制台'
  return (
    <div className="flex min-h-full items-center justify-center p-6">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <Logo className="size-8" />
        <h1 className="text-base font-semibold">{title}</h1>
        <p className="text-xs text-muted-foreground">
          {reason === 'error' ? message : <>在服务器上运行 <code className="rounded bg-muted px-1">acornfox open</code>，用它给的新链接打开控制台。</>}
        </p>
        {reason === 'error' && <Button variant="outline" size="sm" onClick={() => location.reload()}>重试</Button>}
      </div>
    </div>
  )
}

function Shell({ session, onLoggedOut }: { session: Session; onLoggedOut: () => void }) {
  const [route, setRoute] = useState<Route>(() => parseHash(location.hash))
  const [cmd, setCmd] = useState(false)
  const [nav, setNav] = useState(false)
  const [target, setTarget] = useState<InstallTarget | null>(null)

  const routeRef = useRef(route)
  useEffect(() => {
    const h = () => {
      const prev = routeRef.current
      const next = parseHash(location.hash)
      // 只换标签时不滚回顶部
      if (!(prev.page === 'app' && next.page === 'app' && prev.id === next.id)) window.scrollTo(0, 0)
      routeRef.current = next
      setRoute(next)
    }
    window.addEventListener('hashchange', h)
    return () => window.removeEventListener('hashchange', h)
  }, [])
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setCmd((o) => !o)
      }
    }
    window.addEventListener('keydown', k)
    return () => window.removeEventListener('keydown', k)
  }, [])
  const install = useCallback((t: InstallTarget) => setTarget(t), [])

  let page
  switch (route.page) {
    case 'home': page = <DashboardPage />; break
    case 'apps': page = <AppsPage />; break
    case 'app': page = <AppPage key={route.id} name={route.id || ''} tab={(route.tab || 'overview') as AppTab} />; break
    case 'store': page = <StorePage />; break
    case 'item': page = <ItemPage key={(route.query?.catalog || '') + route.id} id={route.id || ''} catalog={route.query?.catalog} />; break
    case 'runtime': page = <RuntimePage />; break
    default: page = <PrefsPage sec={route.id} />
  }

  return (
    <ConsoleProvider session={session} install={install}>
      <div className="flex min-h-full">
        <aside className="sticky top-0 hidden h-screen w-48 shrink-0 border-r md:block"><Sidebar route={route} /></aside>
        <Sheet open={nav} onClose={() => setNav(false)} side="left" className="w-60 p-0" label="菜单">
          <Sidebar route={route} onNavigate={() => setNav(false)} />
        </Sheet>
        <div className="flex min-w-0 flex-1 flex-col">
          <Header route={route} openCmd={() => setCmd(true)} openNav={() => setNav(true)} onLoggedOut={onLoggedOut} />
          <main className="gutter mx-auto w-full max-w-7xl flex-1 pb-24 pt-5">{page}</main>
        </div>
      </div>
      <CommandPalette open={cmd} onClose={() => setCmd(false)} />
      <InstallDialog target={target} onClose={() => setTarget(null)} />
    </ConsoleProvider>
  )
}
