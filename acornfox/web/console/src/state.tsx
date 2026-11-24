// 控制台的共享数据：会话、应用列表、目录、正在跑的安装任务、整机资源。
// 应用列表和安装任务每 5 秒刷新一次（页面在后台时暂停）；目录在打开应用中心和装完应用后刷新。
// 样稿第 14 版：后台任务（安装、重装、退回）做完时人已经不在那一页的，记成“刚做完的”（done），给铃铛用；规则在 lib/jobs.ts。
import * as React from 'react'
import { api } from './lib/api'
import { usePoll } from './lib/hooks'
import { applyShow, catalogAppOf, installingApps } from './lib/catalog'
import { behindApps, fetchAffected, type AffectedApp } from './lib/reapply'
import { jobDone, jobStep, lookingAtApp, lookingAtItem, taskDone, type DoneItem, type Job, type JobKind } from './lib/jobs'
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
  // 落在后面的应用（平台自动填的值或用到的服务变了，应用里用的还是以前的；GET /v1/platform/affected）；旧服务端没有这个接口时是空的
  behind: AffectedApp[]
  refreshAffected: () => Promise<void>
  theme: Theme
  setTheme: (t: Theme) => void
  accent: Accent
  setAccent: (a: Accent) => void
  install: (t: InstallTarget) => void
  // 刚做完的后台任务（人不在那一页时才记），铃铛里显示；clearDone 在铃铛点开又关上以后调
  done: DoneItem[]
  clearDone: () => void
  // 点了重装、退回以后调：记下这件事，做成时按上面的规则告知。detail 是“第 3 版”这类补充
  trackJob: (app: string, kind: JobKind, detail?: string) => void
  // 退出登录（“设置 › 访问”最下面一行用）
  logout: () => Promise<void>
}

const Ctx = React.createContext<ConsoleState | null>(null)

export function useConsole(): ConsoleState {
  const v = React.useContext(Ctx)
  if (!v) throw new Error('useConsole 只能在 ConsoleProvider 里用')
  return v
}

export function ConsoleProvider({ session, install, installTarget, logout, children }: {
  session: Session
  install: (t: InstallTarget) => void
  // 安装弹窗现在开着的那一个（开着就算人还看着这次安装）
  installTarget?: InstallTarget | null
  logout: () => Promise<void>
  children: React.ReactNode
}) {
  const apps = usePoll((s) => api.get<{ apps: AppView[] }>('/v1/apps', s).then((b) => b.apps || []), 5000)
  // 没启用应用中心的服务器回 404：当作没有任务。
  const tasks = usePoll((s) => api.get<{ tasks: TaskView[] }>('/v1/catalog/install', s).then((b) => b.tasks || []).catch(() => [] as TaskView[]), 5000)
  // 公共目录和连上的私有目录合在一起；每个应用、配方带 source，详情和安装按来源 + id 走。
  const catalog = usePoll((s) => api.get<CatalogView>('/v1/catalog', s).then(applyShow), 0)
  const resources = usePoll((s) => api.get<Resources>('/v1/resources', s), 15000)
  const status = usePoll((s) => api.get<ServerStatus>('/v1/status', s), 15000)
  // 一分钟查一次（服务端要逐个应用算一遍）；改完设置、让应用跟上以后马上再查
  const affected = usePoll((s) => fetchAffected(s).then(behindApps).catch(() => [] as AffectedApp[]), 60000)
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

  const behind = React.useMemo(() => affected.data || [], [affected.data])
  const taskList = tasks.data || []
  const installing = React.useMemo(() => installingApps(taskList), [taskList])

  // ---- 刚做完的后台任务 ----
  const [done, setDone] = React.useState<DoneItem[]>([])
  const clearDone = React.useCallback(() => setDone((x) => (x.length ? [] : x)), [])
  const addDone = React.useCallback((it: DoneItem) => setDone((x) => (x.some((y) => y.key === it.key) ? x : [it, ...x].slice(0, 6))), [])
  const jobs = React.useRef(new Map<string, Job>())
  const appsRef = React.useRef<AppView[] | null>(null)
  appsRef.current = apps.data
  const trackJob = React.useCallback((app: string, kind: JobKind, detail?: string) => {
    const a = (appsRef.current || []).find((x) => x.name === app)
    jobs.current.set(app, { kind, detail, from: a?.latest?.id || a?.current_deployment || '', at: Date.now() })
  }, [])
  const targetRef = React.useRef(installTarget)
  targetRef.current = installTarget
  const catalogRef = React.useRef<CatalogView | null>(null)
  catalogRef.current = catalog.data
  // 重装、退回：新的一次部署上线了就算做成。
  React.useEffect(() => {
    const now = Date.now()
    for (const [app, job] of jobs.current) {
      const a = (apps.data || []).find((x) => x.name === app)
      const step = apps.data ? jobStep(job, a, now) : 'wait'
      if (step === 'wait') continue
      jobs.current.delete(app)
      if (step === 'ok' && a && !lookingAtApp(location.hash, app)) addDone(jobDone(job, app, (a.catalog_id && catalogAppOf(catalogRef.current, a)?.name) || a.name, a.latest?.id || '', now))
    }
  }, [apps.data, addDone])
  // 安装：这次打开页面以后看见它在装、现在装好了。
  const taskSeen = React.useRef(new Map<string, string>())
  React.useEffect(() => {
    for (const t of tasks.data || []) {
      const prev = taskSeen.current.get(t.id)
      taskSeen.current.set(t.id, t.status)
      if (prev !== 'running' || t.status !== 'succeeded') continue
      const tg = targetRef.current
      if (tg && (tg.taskId === t.id || tg.id === t.target)) continue
      if (lookingAtItem(location.hash, t.kind, t.target)) continue
      const cat = catalogRef.current
      const name = t.kind === 'recipe' ? (cat?.recipes || []).find((r) => r.id === t.target)?.title : (cat?.apps || []).find((c) => c.id === t.target)?.name
      addDone(taskDone(t, name || t.target, Date.now()))
    }
  }, [tasks.data, addDone])

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
    behind,
    refreshAffected: affected.refresh,
    theme,
    setTheme,
    accent,
    setAccent,
    install,
    done,
    clearDone,
    trackJob,
    logout,
  }
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}
