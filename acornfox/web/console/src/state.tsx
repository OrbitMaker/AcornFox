// 控制台的共享数据：会话、应用列表、目录、正在跑的安装任务、整机资源。
// 应用列表和安装任务每 5 秒刷新一次（页面在后台时暂停）；目录在打开应用中心和装完应用后刷新。
import * as React from 'react'
import { api } from './lib/api'
import { usePoll } from './lib/hooks'
import { installingApps } from './lib/catalog'
import type { AppView, CatalogView, Resources, ServerStatus, Session, TaskView } from './lib/types'
import { applyAccent, applyTheme, loadAccent, loadTheme, type Accent, type Theme } from './lib/theme'

// catalog：私有目录的编号；没有或 public 是公共目录。
export type InstallTarget = { kind: 'app' | 'recipe'; id: string; taskId?: string; catalog?: string }

export interface ConsoleState {
  session: Session
  apps: AppView[] | null
  appsError: Error | null
  refreshApps: () => Promise<void>
  catalog: CatalogView | null
  catalogError: Error | null
  refreshCatalog: () => Promise<void>
  tasks: TaskView[]
  refreshTasks: () => Promise<void>
  installing: Set<string>
  resources: Resources | null
  refreshResources: () => Promise<void>
  status: ServerStatus | null
  theme: Theme
  setTheme: (t: Theme) => void
  accent: Accent
  setAccent: (a: Accent) => void
  install: (t: InstallTarget) => void
}

const Ctx = React.createContext<ConsoleState | null>(null)

export function useConsole(): ConsoleState {
  const v = React.useContext(Ctx)
  if (!v) throw new Error('useConsole 只能在 ConsoleProvider 里用')
  return v
}

export function ConsoleProvider({ session, install, children }: {
  session: Session
  install: (t: InstallTarget) => void
  children: React.ReactNode
}) {
  const apps = usePoll((s) => api.get<{ apps: AppView[] }>('/v1/apps', s).then((b) => b.apps || []), 5000)
  // 没启用应用中心的服务器回 404：当作没有任务。
  const tasks = usePoll((s) => api.get<{ tasks: TaskView[] }>('/v1/catalog/install', s).then((b) => b.tasks || []).catch(() => [] as TaskView[]), 5000)
  // 公共目录和连上的私有目录合在一起；每个应用、配方带 source，详情和安装按来源 + id 走。
  const catalog = usePoll((s) => api.get<CatalogView>('/v1/catalog', s), 0)
  const resources = usePoll((s) => api.get<Resources>('/v1/resources', s), 15000)
  const status = usePoll((s) => api.get<ServerStatus>('/v1/status', s), 15000)
  const [theme, setThemeRaw] = React.useState<Theme>(loadTheme)
  const setTheme = React.useCallback((t: Theme) => {
    applyTheme(t)
    setThemeRaw(t)
  }, [])
  const [accent, setAccentRaw] = React.useState<Accent>(loadAccent)
  const setAccent = React.useCallback((a: Accent) => {
    applyAccent(a)
    setAccentRaw(a)
  }, [])

  const taskList = tasks.data || []
  const installing = React.useMemo(() => installingApps(taskList), [taskList])

  const value: ConsoleState = {
    session,
    apps: apps.data,
    appsError: apps.error,
    refreshApps: apps.refresh,
    catalog: catalog.data,
    catalogError: catalog.error,
    refreshCatalog: catalog.refresh,
    tasks: taskList,
    refreshTasks: tasks.refresh,
    installing,
    resources: resources.data,
    refreshResources: resources.refresh,
    status: status.data,
    theme,
    setTheme,
    accent,
    setAccent,
    install,
  }
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}
